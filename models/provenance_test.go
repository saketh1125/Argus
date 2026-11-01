package models

import (
	"strings"
	"testing"
)

func TestApprovalRejectsChangedCommitAndPatch(t *testing.T) {
	p := PatchProvenance{Repository: "github.com/owner/repo", BaseCommit: strings.Repeat("a", 40), Path: "file.go", OriginalHash: strings.Repeat("b", 64), ResultHash: strings.Repeat("c", 64), PatchDigest: strings.Repeat("d", 64)}
	a, err := NewPatchApproval(p)
	if err != nil || !a.Matches(p) {
		t.Fatalf("valid approval rejected: %v", err)
	}
	changedCommit := p
	changedCommit.BaseCommit = strings.Repeat("e", 40)
	if a.Matches(changedCommit) {
		t.Fatal("approval survived base commit change")
	}
	changedPatch := p
	changedPatch.PatchDigest = strings.Repeat("f", 64)
	if a.Matches(changedPatch) {
		t.Fatal("approval survived patch mutation")
	}
	changedFile := p
	changedFile.ResultHash = strings.Repeat("1", 64)
	if a.Matches(changedFile) {
		t.Fatal("approval survived file mutation")
	}
}

func TestApprovalRejectsIncompleteProvenance(t *testing.T) {
	if _, err := NewPatchApproval(PatchProvenance{}); err == nil {
		t.Fatal("empty provenance approved")
	}
}

func TestProvenanceVerifiesActualBytes(t *testing.T) {
	original := []byte("original")
	result := []byte("result")
	patch := []byte("diff")
	p, err := NewPatchProvenance("github.com/owner/repo", strings.Repeat("a", 40), "file.go", original, result, patch)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.VerifyBytes(original, result, patch); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ a, b, c []byte }{
		{[]byte("stale"), result, patch}, {original, []byte("changed"), patch}, {original, result, []byte("modified")},
	} {
		if err := p.VerifyBytes(tc.a, tc.b, tc.c); err == nil {
			t.Fatal("modified bytes accepted")
		}
	}
}
