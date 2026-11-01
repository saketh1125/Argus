package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareDockerOwnershipPersistsPrivateDistinctRecords(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a, err := prepareDockerOwnership(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := prepareDockerOwnership(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a.RunID == b.RunID || a.OwnershipDigest == b.OwnershipDigest {
		t.Fatal("run identities must differ")
	}
	path := filepath.Join(dir, a.RunID+".json")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("record permissions: %v %v", info, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got dockerOwnershipRecord
	if err := json.Unmarshal(data, &got); err != nil || got != a {
		t.Fatalf("record mismatch: %+v %v", got, err)
	}
}

func TestPrepareDockerOwnershipRejectsNonPrivateDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareDockerOwnership(dir); err == nil {
		t.Fatal("accepted public directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unexpected records: %v %v", entries, err)
	}
}

func TestDockerOwnershipTransitions(t *testing.T) {
	for _, tc := range []struct {
		from, to string
		want     bool
	}{
		{"prepared", "create_uncertain", true},
		{"create_uncertain", "owned", true},
		{"owned", "cleanup_pending", true},
		{"cleanup_pending", "removed", true},
		{"create_uncertain", "ownership_conflict", true},
		{"create_uncertain", "removed", false},
		{"prepared", "owned", false},
		{"removed", "owned", false},
		{"ownership_conflict", "removed", false},
	} {
		if got := validDockerOwnershipTransition(tc.from, tc.to); got != tc.want {
			t.Errorf("transition %s -> %s = %t, want %t", tc.from, tc.to, got, tc.want)
		}
	}
}
