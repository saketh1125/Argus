package services

import (
	"context"
	"testing"
)

func TestUnavailableSandboxRefusesCommands(t *testing.T) {
	sandbox := newUnavailableSandbox("test: isolation is not configured")
	result, err := sandbox.Run(context.Background(), SandboxSpec{
		Files:    map[string]string{"payload.txt": "must not be written"},
		Commands: []string{"echo unsafe"},
	})
	if err == nil || result != nil {
		t.Fatalf("unavailable sandbox must refuse execution, result=%+v err=%v", result, err)
	}
	if got := err.Error(); got != "validation unavailable: test: isolation is not configured" {
		t.Fatalf("unexpected failure reason: %q", got)
	}
}
