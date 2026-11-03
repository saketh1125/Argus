package services

import (
	"context"
	"runtime"
	"testing"
)

func TestLocalSandboxMissingValidatorFailsClosed(t *testing.T) {
	result, err := newLocalSandbox().Run(context.Background(), SandboxSpec{
		Commands: []string{"codesentinel-command-that-does-not-exist --version"}, TimeoutSec: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Success || result.ExitCode != 127 {
		t.Fatalf("missing validation tool must fail, got success=%v exit=%d", result.Success, result.ExitCode)
	}
}

func TestLocalSandboxUsesNativeWindowsShell(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows shell selection only")
	}
	result, err := newLocalSandbox().Run(context.Background(), SandboxSpec{
		Commands: []string{"python --version"}, TimeoutSec: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Fatalf("native PowerShell syntax command failed: %s", result.Logs)
	}
}
