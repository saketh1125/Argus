package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var dockerImageID = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// dockerSandbox is an experimental Python syntax-check prototype. The factory
// intentionally never selects it: its image trust, isolation and validation
// policy are not yet certified for real workloads.
type dockerSandbox struct {
	docker, image string
	// callOverride is used only by deterministic lifecycle tests.
	callOverride func(context.Context, []byte, ...string) (string, string, error)
}

func newDockerSandbox(image string) (*dockerSandbox, error) {
	if !dockerImageID.MatchString(image) {
		return nil, fmt.Errorf("docker prototype: immutable image ID required")
	}
	binary, err := exec.LookPath("docker")
	if err != nil {
		return nil, fmt.Errorf("docker prototype: docker unavailable: %w", err)
	}
	return &dockerSandbox{docker: binary, image: image}, nil
}
func (*dockerSandbox) Name() string            { return "docker-experimental-unverified" }
func (*dockerSandbox) Live() bool              { return false }
func (*dockerSandbox) IsolationVerified() bool { return false }

type boundedDockerOutput struct{ bytes.Buffer }

func (b *boundedDockerOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 64<<10 {
		return 0, fmt.Errorf("docker output exceeds limit")
	}
	return b.Buffer.Write(p)
}
func (s *dockerSandbox) call(ctx context.Context, input []byte, args ...string) (string, string, error) {
	if s.callOverride != nil {
		return s.callOverride(ctx, input, args...)
	}
	cmd := exec.CommandContext(ctx, s.docker, args...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=/nonexistent", "DOCKER_CONFIG=/nonexistent", "LANG=C"}
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	var out, stderr boundedDockerOutput
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err := cmd.Run()
	return out.String(), stderr.String(), err
}

func (s *dockerSandbox) cleanup(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _, removeErr := s.call(ctx, nil, "rm", "-f", name)
	_, inspectErrText, inspectErr := s.call(ctx, nil, "inspect", name)
	if inspectErr == nil || !strings.Contains(strings.ToLower(inspectErrText), "no such object") {
		return fmt.Errorf("docker prototype: cleanup could not be verified")
	}
	// Removing a container that was never created can fail; absence was verified.
	_ = removeErr
	return nil
}

// Run accepts only fixed Python syntax checks and never interpolates shell text.
// It is not production validation and does not execute repository tests.
func (s *dockerSandbox) Run(ctx context.Context, spec SandboxSpec) (result *SandboxResult, err error) {
	if s == nil || !dockerImageID.MatchString(s.image) || spec.RepositoryContext || spec.Language != "python" || spec.TimeoutSec < 1 || spec.TimeoutSec > 60 || len(spec.Commands) == 0 || len(spec.Commands) > 10 {
		return nil, fmt.Errorf("docker prototype: invalid specification")
	}
	archive, err := sandboxArchive(spec.Files)
	if err != nil {
		return nil, fmt.Errorf("docker prototype: invalid files: %w", err)
	}
	for _, command := range spec.Commands {
		if len(command) != 4 || command[0] != "python3" || command[1] != "-m" || command[2] != "py_compile" {
			return nil, fmt.Errorf("docker prototype: command not allowlisted")
		}
		if _, ok := spec.Files[command[3]]; !ok {
			return nil, fmt.Errorf("docker prototype: command file unavailable")
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, fmt.Errorf("docker prototype: generate name: %w", err)
	}
	name := "argus-experimental-" + hex.EncodeToString(random[:])
	defer func() {
		if cleanupErr := s.cleanup(name); cleanupErr != nil {
			result = nil
			err = errors.Join(err, cleanupErr)
		}
	}()
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(spec.TimeoutSec)*time.Second)
	defer cancel()
	_, _, err = s.call(runCtx, nil, "create", "--name", name, "--pull=never", "--network=none", "--read-only", "--user=65534:65534", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=32", "--memory=64m", "--memory-swap=64m", "--cpus=0.5", "--tmpfs", "/workspace:rw,noexec,nosuid,nodev,size=16m,uid=65534,gid=65534,mode=0700", "--tmpfs", "/tmp:rw,noexec,nosuid,nodev,size=4m", "--log-driver=none", "--entrypoint", "sleep", s.image, "120")
	if err != nil {
		return nil, fmt.Errorf("docker prototype: create: %w", err)
	}
	if _, _, err = s.call(runCtx, nil, "start", name); err != nil {
		return nil, fmt.Errorf("docker prototype: start: %w", err)
	}
	if _, _, err = s.call(runCtx, archive, "exec", "-i", "--user=65534:65534", name, "tar", "-xf", "-", "-C", "/workspace"); err != nil {
		return nil, fmt.Errorf("docker prototype: transfer: %w", err)
	}
	var stdout, stderr strings.Builder
	for _, command := range spec.Commands {
		out, errout, runErr := s.call(runCtx, nil, "exec", "--user=65534:65534", "--workdir=/workspace", name, "python3", "-m", "py_compile", "/workspace/"+command[3])
		stdout.WriteString(out)
		stderr.WriteString(errout)
		if runErr != nil {
			if runCtx.Err() != nil {
				return nil, fmt.Errorf("docker prototype: execution deadline: %w", runCtx.Err())
			}
			var exitErr *exec.ExitError
			if !errors.As(runErr, &exitErr) || exitErr.ExitCode() < 0 || exitErr.ExitCode() >= 125 {
				return nil, fmt.Errorf("docker prototype: execution unavailable: %w", runErr)
			}
			return &SandboxResult{Level: ValidationSyntax, RepositoryContext: false, Success: false, ExitCode: exitErr.ExitCode(), Stdout: stdout.String(), Stderr: stderr.String(), Logs: "experimental Python syntax check failed"}, nil
		}
	}
	if runCtx.Err() != nil {
		return nil, runCtx.Err()
	}
	return &SandboxResult{Level: ValidationSyntax, RepositoryContext: false, Success: true, ExitCode: 0, Stdout: stdout.String(), Stderr: stderr.String(), Logs: "experimental Python syntax check only"}, nil
}
