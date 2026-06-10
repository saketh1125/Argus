package models

import "sync"

// RepoIndex is the in-memory knowledge base produced by the preprocessor and
// read by the localizer, analysts, and aggregator. It is concurrency-safe: the
// preprocessor populates it (single writer) and the analyst goroutines read
// from it in parallel, so every access goes through the RWMutex.
type RepoIndex struct {
	mu        sync.RWMutex
	files     map[string]*ParsedFile
	callGraph map[string]*CallGraphNode
	sigs      []Signature
}

// NewRepoIndex returns an empty, ready-to-use index.
func NewRepoIndex() *RepoIndex {
	return &RepoIndex{
		files:     make(map[string]*ParsedFile),
		callGraph: make(map[string]*CallGraphNode),
	}
}

// AddFile registers a parsed file and its signatures. Call BuildCallGraph once
// all files have been added.
func (r *RepoIndex) AddFile(f *ParsedFile) {
	if f == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.files[f.Path] = f
	r.sigs = append(r.sigs, f.Signatures...)
}

// BuildCallGraph derives the bidirectional call graph from every file's call
// edges. Safe to call once after all files are added.
func (r *RepoIndex) BuildCallGraph() {
	r.mu.Lock()
	defer r.mu.Unlock()

	node := func(fn, file string) *CallGraphNode {
		n, ok := r.callGraph[fn]
		if !ok {
			n = &CallGraphNode{Function: fn, File: file}
			r.callGraph[fn] = n
		}
		if n.File == "" {
			n.File = file
		}
		return n
	}

	// Seed nodes from declared signatures so even uncalled functions appear.
	for _, s := range r.sigs {
		node(s.Name, s.File)
	}

	for _, f := range r.files {
		for _, e := range f.Calls {
			caller := node(e.Caller, e.File)
			callee := node(e.Callee, "")
			if !contains(caller.Calls, e.Callee) {
				caller.Calls = append(caller.Calls, e.Callee)
			}
			if !contains(callee.CalledBy, e.Caller) {
				callee.CalledBy = append(callee.CalledBy, e.Caller)
			}
		}
	}
}

// GetCallGraph returns the call graph node for a function, or nil if unknown.
// The returned node is a defensive copy so callers cannot mutate shared state.
func (r *RepoIndex) GetCallGraph(fn string) *CallGraphNode {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n, ok := r.callGraph[fn]
	if !ok {
		return nil
	}
	cp := *n
	cp.Calls = append([]string(nil), n.Calls...)
	cp.CalledBy = append([]string(nil), n.CalledBy...)
	return &cp
}

// DependentFiles returns the set of files involved in a function's call graph
// neighborhood (callers and callees), used to enrich an issue's fix context.
func (r *RepoIndex) DependentFiles(fn string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n, ok := r.callGraph[fn]
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if dep, ok := r.callGraph[name]; ok && dep.File != "" && !seen[dep.File] {
			seen[dep.File] = true
			out = append(out, dep.File)
		}
	}
	for _, c := range n.Calls {
		add(c)
	}
	for _, c := range n.CalledBy {
		add(c)
	}
	return out
}

// GetFile returns a parsed file by path.
func (r *RepoIndex) GetFile(path string) (*ParsedFile, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.files[path]
	return f, ok
}

// Files returns all parsed files (snapshot slice).
func (r *RepoIndex) Files() []*ParsedFile {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*ParsedFile, 0, len(r.files))
	for _, f := range r.files {
		out = append(out, f)
	}
	return out
}

// Signatures returns a snapshot of all signatures across the repo.
func (r *RepoIndex) Signatures() []Signature {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Signature(nil), r.sigs...)
}

// FileCount returns the number of indexed files.
func (r *RepoIndex) FileCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.files)
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
