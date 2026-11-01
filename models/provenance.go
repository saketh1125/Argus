package models

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

// PatchProvenance identifies the exact source and patch under review.
// Consumers must independently compute and verify the hashes against the actual
// source, resulting file and patch; this type does not establish validation.
type PatchProvenance struct {
	Repository   string `json:"repository"`
	BaseCommit   string `json:"base_commit"`
	Path         string `json:"path"`
	OriginalHash string `json:"original_hash"`
	ResultHash   string `json:"result_hash"`
	PatchDigest  string `json:"patch_digest"`
}

func validLowerHex(s string, n int) bool {
	if len(s) != n || s != strings.ToLower(s) {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func (p PatchProvenance) digest() (string, error) {
	parts := strings.Split(p.Repository, "/")
	if len(parts) != 3 || parts[0] != "github.com" || parts[1] == "" || parts[2] == "" || parts[1] == "." || parts[1] == ".." || parts[2] == "." || parts[2] == ".." || p.Repository != strings.ToLower(p.Repository) || !validLowerHex(p.BaseCommit, 40) || !validLowerHex(p.OriginalHash, 64) || !validLowerHex(p.ResultHash, 64) || !validLowerHex(p.PatchDigest, 64) || p.Path == "" || path.IsAbs(p.Path) || path.Clean(p.Path) != p.Path || p.Path == ".." || strings.HasPrefix(p.Path, "../") {
		return "", fmt.Errorf("invalid patch provenance")
	}
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	d := sha256.Sum256(b)
	return hex.EncodeToString(d[:]), nil
}

// PatchApproval binds a decision to a provenance digest; it is not an
// authenticated identity or permission to publish by itself.
type PatchApproval struct {
	ProvenanceDigest string `json:"provenance_digest"`
}

// NewPatchApproval constructs a binding after a caller obtains explicit consent.
func NewPatchApproval(p PatchProvenance) (PatchApproval, error) {
	d, err := p.digest()
	return PatchApproval{ProvenanceDigest: d}, err
}

// Matches reports whether the exact provenance remains unchanged.
func (a PatchApproval) Matches(p PatchProvenance) bool {
	d, err := p.digest()
	return err == nil && a.ProvenanceDigest != "" && a.ProvenanceDigest == d
}

// NewPatchProvenance hashes the actual inputs rather than trusting supplied hashes.
// It does not establish that the base commit or patch application is valid.
func NewPatchProvenance(repo, commit, file string, original, result, patch []byte) (PatchProvenance, error) {
	if len(original) == 0 || len(result) == 0 || len(patch) == 0 {
		return PatchProvenance{}, fmt.Errorf("empty patch input")
	}
	p := PatchProvenance{Repository: repo, BaseCommit: commit, Path: file, OriginalHash: hashBytes(original), ResultHash: hashBytes(result), PatchDigest: hashBytes(patch)}
	if _, err := p.digest(); err != nil {
		return PatchProvenance{}, err
	}
	return p, nil
}

func hashBytes(b []byte) string {
	d := sha256.Sum256(b)
	return hex.EncodeToString(d[:])
}

// VerifyBytes rechecks all recorded hashes against actual bytes.
func (p PatchProvenance) VerifyBytes(original, result, patch []byte) error {
	if _, err := p.digest(); err != nil {
		return err
	}
	if len(original) == 0 || len(result) == 0 || len(patch) == 0 || p.OriginalHash != hashBytes(original) || p.ResultHash != hashBytes(result) || p.PatchDigest != hashBytes(patch) {
		return fmt.Errorf("patch provenance byte mismatch")
	}
	return nil
}
