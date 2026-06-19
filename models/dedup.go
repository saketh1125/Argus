package models

import (
	"encoding/json"
	"os"
	"sync"
)

// DedupStore persists the set of Issue.DedupKey() values for which a PR has
// already been raised. On each run the PR agent checks the store before
// creating a branch; after a successful PR the key is written back to disk.
// This prevents the same finding from generating a new PR on every run.
//
// The backing file is a JSON array of strings written atomically (write to a
// temp file, then rename). If the file is missing it is created on first
// write. If it is unreadable the store starts empty and continues without
// persistence — a non-fatal degradation.
type DedupStore struct {
	path string
	mu   sync.Mutex
	seen map[string]struct{}
}

// LoadDedupStore opens or creates the dedup file at path. The returned store
// is always usable even if the file does not exist yet.
func LoadDedupStore(path string) *DedupStore {
	ds := &DedupStore{path: path, seen: make(map[string]struct{})}
	data, err := os.ReadFile(path)
	if err != nil {
		return ds // missing file is fine; starts empty
	}
	var keys []string
	if err := json.Unmarshal(data, &keys); err != nil {
		return ds // corrupt file; start fresh
	}
	for _, k := range keys {
		ds.seen[k] = struct{}{}
	}
	return ds
}

// Has reports whether key has already been recorded in the store.
func (d *DedupStore) Has(key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.seen[key]
	return ok
}

// Record marks key as seen in memory and atomically persists the full set to
// the backing file. Errors are silently discarded — the in-memory set is
// always updated even when the disk write fails.
func (d *DedupStore) Record(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seen[key] = struct{}{}
	_ = d.flush()
}

// flush writes the current seen-set to disk. Must be called with d.mu held.
func (d *DedupStore) flush() error {
	keys := make([]string, 0, len(d.seen))
	for k := range d.seen {
		keys = append(keys, k)
	}
	data, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	// Write to a sibling temp file then rename for atomicity.
	tmp := d.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, d.path)
}

// Len returns the number of recorded keys (useful for logging).
func (d *DedupStore) Len() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.seen)
}
