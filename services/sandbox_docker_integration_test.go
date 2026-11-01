//go:build sandbox_integration

package services

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// These are host capability probes, not tests of an ARGUS sandbox provider.
// The caller must supply a locally available, trusted redis image by immutable ID.
func TestDockerBoundaryProbes(t *testing.T) {
	image := os.Getenv("ARGUS_DOCKER_TEST_IMAGE")
	if !strings.HasPrefix(image, "sha256:") || len(image) != len("sha256:")+64 {
		t.Skip("set ARGUS_DOCKER_TEST_IMAGE to a trusted local image ID")
	}
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Skip("docker unavailable")
	}
	run := func(t *testing.T, entrypoint string, args ...string) (string, error) {
		t.Helper()
		name := fmt.Sprintf("argus-boundary-test-%d", time.Now().UnixNano())
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if out, err := exec.CommandContext(ctx, docker, "rm", "-f", name).CombinedOutput(); err != nil {
				t.Errorf("container cleanup failed: %s: %v", out, err)
				return
			}
			if err := exec.CommandContext(ctx, docker, "inspect", name).Run(); err == nil {
				t.Error("container still exists after cleanup")
			}
		})
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		argv := []string{"run", "--name", name, "--pull=never", "--network=none", "--read-only", "--user=65534:65534", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=32", "--memory=64m", "--memory-swap=64m", "--cpus=0.5", "--tmpfs", "/tmp:rw,noexec,nosuid,nodev,size=4m", "--entrypoint", entrypoint, image}
		argv = append(argv, args...)
		out, err := exec.CommandContext(ctx, docker, argv...).CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("docker command timed out: %v", ctx.Err())
		}
		return string(out), err
	}
	t.Run("transfer bounded archive", func(t *testing.T) {
		archive, err := sandboxArchive(map[string]string{"nested/input": "argus-transfer-check"})
		if err != nil {
			t.Fatal(err)
		}
		name := fmt.Sprintf("argus-boundary-transfer-%d", time.Now().UnixNano())
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if out, err := exec.CommandContext(ctx, docker, "rm", "-f", name).CombinedOutput(); err != nil {
				t.Errorf("cleanup: %s %v", out, err)
			}
			if err := exec.CommandContext(ctx, docker, "inspect", name).Run(); err == nil {
				t.Error("container survived cleanup")
			}
		})
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		create := exec.CommandContext(ctx, docker, "create", "--name", name, "--pull=never", "--network=none", "--read-only", "--user=65534:65534", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=32", "--memory=64m", "--memory-swap=64m", "--cpus=0.5", "--tmpfs", "/tmp:rw,noexec,nosuid,nodev,size=4m", "--entrypoint", "sleep", image, "30")
		if out, err := create.CombinedOutput(); err != nil {
			t.Fatalf("create: %s %v", out, err)
		}
		if out, err := exec.CommandContext(ctx, docker, "start", name).CombinedOutput(); err != nil {
			t.Fatalf("start: %s %v", out, err)
		}
		extract := exec.CommandContext(ctx, docker, "exec", "-i", "--user=65534:65534", name, "tar", "-xf", "-", "-C", "/tmp")
		extract.Stdin = bytes.NewReader(archive)
		if out, err := extract.CombinedOutput(); err != nil {
			t.Fatalf("extract: %s %v", out, err)
		}
		out, err := exec.CommandContext(ctx, docker, "exec", "--user=65534:65534", name, "cat", "/tmp/nested/input").CombinedOutput()
		if err != nil || string(out) != "argus-transfer-check" {
			t.Fatalf("transfer: %q %v", out, err)
		}
	})
	t.Run("timeout explicitly removes container", func(t *testing.T) {
		name := fmt.Sprintf("argus-boundary-timeout-%d", time.Now().UnixNano())
		cleanup := func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = exec.CommandContext(ctx, docker, "rm", "-f", name).Run()
		}
		t.Cleanup(cleanup)
		createCtx, createCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer createCancel()
		create := exec.CommandContext(createCtx, docker, "create", "--name", name, "--pull=never", "--network=none", "--read-only", "--user=65534:65534", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=32", "--memory=64m", "--memory-swap=64m", "--cpus=0.5", "--entrypoint", "sleep", image, "30")
		if out, err := create.CombinedOutput(); err != nil {
			t.Fatalf("create: %s %v", out, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, docker, "start", "-a", name).Run()
		if ctx.Err() != context.DeadlineExceeded {
			t.Fatalf("expected client deadline, got %v", ctx.Err())
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if out, err := exec.CommandContext(cleanupCtx, docker, "rm", "-f", name).CombinedOutput(); err != nil {
			t.Fatalf("cleanup: %s %v", out, err)
		}
		if err := exec.CommandContext(cleanupCtx, docker, "inspect", name).Run(); err == nil {
			t.Fatal("timed-out container survived cleanup")
		}
	})
	t.Run("runs as non-root", func(t *testing.T) {
		out, err := run(t, "id", "-u")
		if err != nil || strings.TrimSpace(out) != "65534" {
			t.Fatalf("unexpected identity: %q %v", out, err)
		}
	})
	t.Run("capabilities dropped", func(t *testing.T) {
		out, err := run(t, "cat", "/proc/self/status")
		if err != nil || !strings.Contains(out, "CapEff:\t0000000000000000") || !strings.Contains(out, "NoNewPrivs:\t1") {
			t.Fatalf("unexpected process security state: %q %v", out, err)
		}
	})
	t.Run("root filesystem readonly", func(t *testing.T) {
		out, err := run(t, "cat", "/proc/mounts")
		if err != nil {
			t.Fatalf("mounts: %v", err)
		}
		readonly := false
		for _, line := range strings.Split(out, "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 4 && fields[1] == "/" {
				for _, option := range strings.Split(fields[3], ",") {
					if option == "ro" {
						readonly = true
					}
				}
			}
		}
		if !readonly {
			t.Fatal("root filesystem not reported read-only")
		}
	})
	t.Run("host temporary file absent", func(t *testing.T) {
		file, err := os.CreateTemp("", "argus-host-boundary-")
		if err != nil {
			t.Fatal(err)
		}
		name := file.Name()
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Remove(name) })
		out, err := run(t, "test", "!", "-e", name)
		if err != nil {
			t.Fatalf("host file visible in container: %q %v", out, err)
		}
	})
	t.Run("docker socket absent", func(t *testing.T) {
		out, err := run(t, "test", "!", "-e", "/var/run/docker.sock")
		if err != nil {
			t.Fatalf("docker socket accessible: %q %v", out, err)
		}
	})
	t.Run("cpu quota configured", func(t *testing.T) {
		out, err := run(t, "cat", "/sys/fs/cgroup/cpu.max")
		if err != nil || strings.TrimSpace(out) != "50000 100000" {
			t.Fatalf("unexpected CPU quota: %q %v", out, err)
		}
	})
	t.Run("network disabled", func(t *testing.T) {
		out, err := run(t, "redis-cli", "-h", "1.1.1.1", "-p", "53", "ping")
		if err == nil || !strings.Contains(out, "Network unreachable") {
			t.Fatalf("unexpected network result: %q %v", out, err)
		}
	})
	t.Run("memory limit enforced", func(t *testing.T) {
		name := fmt.Sprintf("argus-boundary-memory-%d", time.Now().UnixNano())
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if out, err := exec.CommandContext(ctx, docker, "rm", "-f", name).CombinedOutput(); err != nil {
				t.Errorf("cleanup: %s %v", out, err)
				return
			}
			if err := exec.CommandContext(ctx, docker, "inspect", name).Run(); err == nil {
				t.Error("container survived cleanup")
			}
		})
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		create := exec.CommandContext(ctx, docker, "create", "--name", name, "--pull=never", "--network=none", "--read-only", "--user=65534:65534", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=16", "--memory=32m", "--memory-swap=32m", "--cpus=0.5", "--tmpfs", "/tmp:rw,noexec,nosuid,nodev,size=128m", "--entrypoint", "dd", image, "if=/dev/zero", "of=/tmp/over", "bs=1048576", "count=128")
		if out, err := create.CombinedOutput(); err != nil {
			t.Fatalf("create: %s %v", out, err)
		}
		_ = exec.CommandContext(ctx, docker, "start", "-a", name).Run()
		out, err := exec.CommandContext(ctx, docker, "inspect", "--format", "{{.State.OOMKilled}} {{.State.ExitCode}}", name).CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != "true 137" {
			t.Fatalf("expected cgroup OOM kill: %q %v", out, err)
		}
	})
	t.Run("process limit enforced", func(t *testing.T) {
		// This fixed test program creates only short-lived sleeps; it is not repository or model input.
		out, err := run(t, "sh", "-c", `i=0; while [ "$i" -lt 48 ]; do sleep 2 & i=$((i+1)); done; wait`)
		if err == nil || !strings.Contains(out, "can't fork") {
			t.Fatalf("unexpected process-limit result: %q %v", out, err)
		}
	})
	t.Run("tmpfs size enforced", func(t *testing.T) {
		out, err := run(t, "dd", "if=/dev/zero", "of=/tmp/over", "bs=1048576", "count=8")
		if err == nil || !strings.Contains(out, "No space left on device") {
			t.Fatalf("unexpected disk result: %q %v", out, err)
		}
	})
}

// TestDockerPythonSyntaxProbe tests controlled fixture code only. This does not
// enable an ARGUS provider or certify the image as safe for untrusted code.
func TestDockerPythonSyntaxProbe(t *testing.T) {
	image := os.Getenv("ARGUS_DOCKER_PYTHON_IMAGE")
	if !strings.HasPrefix(image, "sha256:") || len(image) != len("sha256:")+64 {
		t.Skip("set ARGUS_DOCKER_PYTHON_IMAGE to a trusted local image ID")
	}
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Skip("docker unavailable")
	}
	archive, err := sandboxArchive(map[string]string{"nested/valid.py": "def valid():\n    return 1\n", "nested/invalid.py": "def broken(:\n"})
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("argus-python-probe-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, docker, "rm", "-f", name).CombinedOutput(); err != nil {
			t.Errorf("cleanup: %s %v", out, err)
			return
		}
		if err := exec.CommandContext(ctx, docker, "inspect", name).Run(); err == nil {
			t.Error("container survived cleanup")
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	create := exec.CommandContext(ctx, docker, "create", "--name", name, "--pull=never", "--network=none", "--read-only", "--user=65534:65534", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=32", "--memory=64m", "--memory-swap=64m", "--cpus=0.5", "--tmpfs", "/tmp:rw,noexec,nosuid,nodev,size=4m", "--entrypoint", "sleep", image, "30")
	if out, err := create.CombinedOutput(); err != nil {
		t.Fatalf("create: %s %v", out, err)
	}
	if out, err := exec.CommandContext(ctx, docker, "start", name).CombinedOutput(); err != nil {
		t.Fatalf("start: %s %v", out, err)
	}
	extract := exec.CommandContext(ctx, docker, "exec", "-i", "--user=65534:65534", name, "tar", "-xf", "-", "-C", "/tmp")
	extract.Stdin = bytes.NewReader(archive)
	if out, err := extract.CombinedOutput(); err != nil {
		t.Fatalf("extract: %s %v", out, err)
	}
	if out, err := exec.CommandContext(ctx, docker, "exec", "--user=65534:65534", name, "python3", "-m", "py_compile", "/tmp/nested/valid.py").CombinedOutput(); err != nil {
		t.Fatalf("valid syntax: %s %v", out, err)
	}
	if out, err := exec.CommandContext(ctx, docker, "exec", "--user=65534:65534", name, "python3", "-m", "py_compile", "/tmp/nested/invalid.py").CombinedOutput(); err == nil || !strings.Contains(string(out), "SyntaxError") {
		t.Fatalf("invalid syntax accepted: %s %v", out, err)
	}
	if out, err := exec.CommandContext(ctx, docker, "exec", "--user=65534:65534", name, "argus_missing_interpreter").CombinedOutput(); err == nil || !strings.Contains(string(out), "argus_missing_interpreter") {
		t.Fatalf("unexpected missing interpreter result: %s %v", out, err)
	}
	if ctx.Err() != nil {
		t.Fatalf("probe deadline: %v", ctx.Err())
	}
}
