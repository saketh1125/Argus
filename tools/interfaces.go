// Package tools provides the I/O building blocks the agents compose: GitHub
// operations, source parsing/navigation, and patch/diff utilities. Per the
// design principle "LLMs reason, tools only do I/O" — there is no decision
// logic here, only mechanical operations.
//
// Dependency direction: tools -> {models, services, config}.
package tools

import (
	"context"

	"github.com/saketh1125/argus/models"
)

// ---------------------------------------------------------------------------
// GitHub
// ---------------------------------------------------------------------------

// PRSpec describes a pull request to create.
type PRSpec struct {
	Title string
	Body  string
	Head  string // source branch
	Base  string // target branch (usually default)
}

// CommitSpec describes a single-file commit onto a branch.
type CommitSpec struct {
	Branch  string
	Path    string
	Content string
	Message string
}

// GitHubClient performs all GitHub-side operations. The real adapter wraps
// google/go-github; the mock records calls in memory and returns deterministic
// URLs so the pipeline runs end-to-end without a token.
type GitHubClient interface {
	// ParseURL extracts owner/repo from a GitHub URL.
	ParseURL(url string) (owner, repo string, err error)
	// Clone shallow-clones the repo to a local directory and returns its path.
	Clone(ctx context.Context, url, dest string) (string, error)
	// DefaultBranch returns the repo's default branch name.
	DefaultBranch(ctx context.Context, owner, repo string) (string, error)
	// CreateBranch creates branch `name` from `base`.
	CreateBranch(ctx context.Context, owner, repo, base, name string) error
	// CommitFile commits a single file change to a branch.
	CommitFile(ctx context.Context, owner, repo string, spec CommitSpec) error
	// CreatePR opens a pull request and returns its number and URL.
	CreatePR(ctx context.Context, owner, repo string, spec PRSpec) (number int, url string, err error)
	// CreateIssue opens an issue and returns its number and URL.
	CreateIssue(ctx context.Context, owner, repo, title, body string) (number int, url string, err error)
	Name() string
	Live() bool
}

// ---------------------------------------------------------------------------
// Source parsing & navigation
// ---------------------------------------------------------------------------

// CodeParser extracts signatures, imports, and call edges from source code.
// The primary implementation uses tree-sitter (CGO); a regex/heuristic fallback
// keeps the pipeline working if the CGO build is unavailable.
type CodeParser interface {
	// Supports reports whether the parser handles a file with this extension.
	Supports(ext string) bool
	// Language returns the language name for an extension (e.g. ".go" -> "go").
	Language(ext string) string
	// Parse analyzes one file's content into a ParsedFile.
	Parse(path, content string) (*models.ParsedFile, error)
	Name() string
}
