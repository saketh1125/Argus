package main

import "testing"

func TestLiveGitHubRequiresIsolatedSandbox(t *testing.T) {
	if err := requireIsolatedValidationBeforeLivePR(true, false); err == nil {
		t.Fatal("live GitHub with local sandbox must be rejected")
	}
	if err := requireIsolatedValidationBeforeLivePR(true, true); err != nil {
		t.Fatalf("live GitHub with isolated sandbox should pass: %v", err)
	}
	if err := requireIsolatedValidationBeforeLivePR(false, false); err != nil {
		t.Fatalf("mock GitHub offline run should remain available: %v", err)
	}
}
