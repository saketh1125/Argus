package services

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
)

var dockerImageID = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// dockerSandbox is an experimental Python syntax-check prototype. The factory
// intentionally never selects it: its image trust, isolation and validation
// policy are not yet certified for real workloads.
type dockerSandbox struct {
	docker, image string
	ownershipDir  string
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

// Run accepts only fixed Python syntax checks and never interpolates shell text.
// It is not production validation and does not execute repository tests.
func (s *dockerSandbox) Run(ctx context.Context, spec SandboxSpec) (result *SandboxResult, err error) {
	if s == nil || !dockerImageID.MatchString(s.image) || spec.RepositoryContext || spec.Language != "python" || spec.TimeoutSec < 1 || spec.TimeoutSec > 60 || len(spec.Commands) == 0 || len(spec.Commands) > 10 {
		return nil, fmt.Errorf("docker prototype: invalid specification")
	}
	_, err = sandboxArchive(spec.Files)
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
	// The current record primitive cannot atomically persist lifecycle changes
	// or reconcile an uncertain create. Refuse to contact Docker until those
	// guarantees exist; a record alone is not sufficient ownership proof.
	if s.ownershipDir == "" {
		return nil, fmt.Errorf("docker prototype: ownership store unavailable")
	}
	if _, err := prepareDockerOwnership(s.ownershipDir); err != nil {
		return nil, fmt.Errorf("docker prototype: prepare ownership: %w", err)
	}
	return nil, fmt.Errorf("docker prototype: ownership reconciliation unavailable")
}
