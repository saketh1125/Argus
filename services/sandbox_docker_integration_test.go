//go:build sandbox_integration

package services

import (
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
	t.Run("runs as non-root", func(t *testing.T) {
		out, err := run(t, "id", "-u")
		if err != nil || strings.TrimSpace(out) != "65534" {
			t.Fatalf("unexpected identity: %q %v", out, err)
		}
	})
	t.Run("network disabled", func(t *testing.T) {
		out, err := run(t, "redis-cli", "-h", "1.1.1.1", "-p", "53", "ping")
		if err == nil || !strings.Contains(out, "Network unreachable") {
			t.Fatalf("unexpected network result: %q %v", out, err)
		}
	})
	t.Run("tmpfs size enforced", func(t *testing.T) {
		out, err := run(t, "dd", "if=/dev/zero", "of=/tmp/over", "bs=1048576", "count=8")
		if err == nil || !strings.Contains(out, "No space left on device") {
			t.Fatalf("unexpected disk result: %q %v", out, err)
		}
	})
}
