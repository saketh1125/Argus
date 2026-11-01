package services

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestSandboxArchiveRejectsUnsafePathsAndLimits(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "a/../../x", "a//b", "./a", "", "a\\b", "a/../b"} {
		if _, err := sandboxArchive(map[string]string{name: "x"}); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
	if _, err := sandboxArchive(map[string]string{"file": strings.Repeat("x", sandboxMaxFileBytes+1)}); err == nil {
		t.Error("accepted oversized file")
	}
}

func TestSandboxArchiveDeterministicRegularFiles(t *testing.T) {
	files := map[string]string{"b/file": "two", "a": "one"}
	first, err := sandboxArchive(files)
	if err != nil {
		t.Fatal(err)
	}
	second, err := sandboxArchive(files)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("archive is nondeterministic")
	}
	r := tar.NewReader(bytes.NewReader(first))
	for _, expected := range []string{"a", "b/file"} {
		h, err := r.Next()
		if err != nil {
			t.Fatal(err)
		}
		if h.Name != expected || h.Typeflag != tar.TypeReg {
			t.Fatalf("unexpected entry: %+v", h)
		}
		got, err := io.ReadAll(r)
		if err != nil || string(got) != files[expected] {
			t.Fatalf("bad content: %q %v", got, err)
		}
	}
	if _, err := r.Next(); err != io.EOF {
		t.Fatalf("unexpected extra entry: %v", err)
	}
}

func TestSandboxArchiveRejectsDirectoryConflictAndCount(t *testing.T) {
	if _, err := sandboxArchive(map[string]string{"a": "file", "a/b": "nested"}); err == nil {
		t.Fatal("accepted file/directory collision")
	}
	files := make(map[string]string)
	for i := 0; i <= sandboxMaxFiles; i++ {
		files[fmt.Sprintf("f%d", i)] = "x"
	}
	if _, err := sandboxArchive(files); err == nil {
		t.Fatal("accepted excessive file count")
	}
}
