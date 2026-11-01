package tools

import (
	"bufio"
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	snapshotMaxFiles      = 1000
	snapshotMaxFileBytes  = 1 << 20
	snapshotMaxTotalBytes = 16 << 20
)

type snapshotEntry struct{ name, hash string }

// SnapshotPinnedRepository returns bounded regular-file bytes directly from the
// pinned Git tree. It rejects symlinks, submodules and unsafe paths; it does
// not run checkout scripts or repository code. It does not attest sandboxing.
func SnapshotPinnedRepository(ctx context.Context, workdir, repository, commit string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := VerifyGitRepository(ctx, workdir, repository, commit); err != nil {
		return nil, err
	}
	tree, err := readGitBounded(ctx, workdir, 2<<20, "", "ls-tree", "-r", "--full-tree", "-z", commit)
	if err != nil {
		return nil, fmt.Errorf("snapshot tree: %w", err)
	}
	if tree == "" || !strings.HasSuffix(tree, "\x00") {
		return nil, fmt.Errorf("snapshot tree empty or malformed")
	}
	records := strings.Split(strings.TrimSuffix(tree, "\x00"), "\x00")
	if len(records) > snapshotMaxFiles {
		return nil, fmt.Errorf("snapshot file count exceeds limit")
	}
	entries := make([]snapshotEntry, 0, len(records))
	var input strings.Builder
	for _, record := range records {
		parts := strings.SplitN(record, "\t", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("snapshot tree entry malformed")
		}
		fields := strings.Fields(parts[0])
		name := parts[1]
		if len(fields) != 3 || (fields[0] != "100644" && fields[0] != "100755") || fields[1] != "blob" || !gitCommitSHA.MatchString(fields[2]) {
			return nil, fmt.Errorf("snapshot contains unsupported entry")
		}
		if name == "" || len(name) > 1024 || !utf8.ValidString(name) || path.IsAbs(name) || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "\\") || strings.IndexFunc(name, unicode.IsControl) >= 0 {
			return nil, fmt.Errorf("snapshot contains unsafe path")
		}
		entries = append(entries, snapshotEntry{name: name, hash: fields[2]})
		input.WriteString(fields[2])
		input.WriteByte('\n')
	}
	batch, err := readGitBounded(ctx, workdir, 17<<20, input.String(), "cat-file", "--batch")
	if err != nil {
		return nil, fmt.Errorf("snapshot blobs: %w", err)
	}
	r := bufio.NewReader(strings.NewReader(batch))
	files := make(map[string]string, len(entries))
	total := 0
	for _, entry := range entries {
		header, err := r.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("snapshot blob header: %w", err)
		}
		fields := strings.Fields(header)
		if len(fields) != 3 || fields[0] != entry.hash || fields[1] != "blob" {
			return nil, fmt.Errorf("snapshot blob identity mismatch")
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || size < 0 || size > snapshotMaxFileBytes {
			return nil, fmt.Errorf("snapshot blob exceeds limit")
		}
		total += int(size)
		if total > snapshotMaxTotalBytes {
			return nil, fmt.Errorf("snapshot exceeds total limit")
		}
		content := make([]byte, int(size))
		if _, err := io.ReadFull(r, content); err != nil {
			return nil, fmt.Errorf("snapshot blob truncated: %w", err)
		}
		if delimiter, err := r.ReadByte(); err != nil || delimiter != '\n' {
			return nil, fmt.Errorf("snapshot blob delimiter invalid")
		}
		h := sha1.New()
		fmt.Fprintf(h, "blob %d\x00", len(content))
		_, _ = h.Write(content)
		if fmt.Sprintf("%x", h.Sum(nil)) != entry.hash {
			return nil, fmt.Errorf("snapshot blob content mismatch")
		}
		if _, exists := files[entry.name]; exists {
			return nil, fmt.Errorf("snapshot duplicate path")
		}
		files[entry.name] = string(content)
	}
	if _, err := r.ReadByte(); err != io.EOF {
		return nil, fmt.Errorf("snapshot contains unexpected trailing data")
	}
	if err := VerifyGitRepository(ctx, workdir, repository, commit); err != nil {
		return nil, err
	}
	return files, nil
}
