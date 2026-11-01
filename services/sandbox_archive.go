package services

import (
	"archive/tar"
	"bytes"
	"fmt"
	"path"
	"sort"
	"strings"
)

const (
	sandboxMaxFiles        = 1000
	sandboxMaxFileBytes    = 1 << 20
	sandboxMaxArchiveBytes = 16 << 20
)

// sandboxArchive creates a bounded archive containing regular files only. It
// does not establish that a provider extracts archives safely or is isolated.
func sandboxArchive(files map[string]string) ([]byte, error) {
	if len(files) == 0 || len(files) > sandboxMaxFiles {
		return nil, fmt.Errorf("invalid sandbox file count")
	}
	names := make([]string, 0, len(files))
	total := 0
	for name, content := range files {
		if name == "" || name == "." || len(name) > 1024 || path.IsAbs(name) || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") || strings.ContainsAny(name, "\\\x00") {
			return nil, fmt.Errorf("invalid sandbox file path %q", name)
		}
		if len(content) > sandboxMaxFileBytes {
			return nil, fmt.Errorf("sandbox file exceeds size limit")
		}
		total += len(content)
		if total > sandboxMaxArchiveBytes {
			return nil, fmt.Errorf("sandbox input exceeds size limit")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if _, exists := files[parent]; exists {
				return nil, fmt.Errorf("sandbox file path conflicts with directory")
			}
		}
	}
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	for _, name := range names {
		content := files[name]
		if err := w.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(content)); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
