package tools

import "testing"

func hasSignature(sigs []sigLite, name string) bool {
	for _, s := range sigs {
		if s.name == name {
			return true
		}
	}
	return false
}

type sigLite struct{ name, kind string }

func TestParseGo(t *testing.T) {
	src := `package sample

import (
	"database/sql"
	"fmt"
)

type Store struct {
	db *sql.DB
}

func (s *Store) FindUser(name string) (*string, error) {
	query := fmt.Sprintf("SELECT email FROM users WHERE name = '%s'", name)
	row := s.db.QueryRow(query)
	return &email, nil
}

func Divide(a, b int) int {
	return a / b
}
`
	p := newHeuristicParser()
	pf, err := p.Parse("store.go", src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if pf.Language != "go" {
		t.Errorf("language = %q, want go", pf.Language)
	}
	var lite []sigLite
	for _, s := range pf.Signatures {
		lite = append(lite, sigLite{s.Name, s.Kind})
	}
	for _, want := range []string{"FindUser", "Divide", "Store"} {
		if !hasSignature(lite, want) {
			t.Errorf("missing signature %q; got %+v", want, lite)
		}
	}
	if len(pf.Imports) == 0 {
		t.Errorf("expected imports, got none")
	}
	foundFmt := false
	for _, imp := range pf.Imports {
		if imp == "fmt" || imp == "database/sql" {
			foundFmt = true
		}
	}
	if !foundFmt {
		t.Errorf("expected fmt/database/sql import, got %v", pf.Imports)
	}
	if len(pf.Calls) == 0 {
		t.Errorf("expected at least one call edge")
	}
	// Verify a real call is attributed and there's no self-edge.
	foundCall := false
	for _, c := range pf.Calls {
		if c.Caller == "FindUser" && c.Callee == "FindUser" {
			t.Errorf("self-edge recorded for FindUser")
		}
		if c.Callee == "Sprintf" || c.Callee == "QueryRow" {
			foundCall = true
		}
	}
	if !foundCall {
		t.Errorf("expected Sprintf/QueryRow call edge, got %+v", pf.Calls)
	}
}

func TestParsePython(t *testing.T) {
	src := `import sqlite3
import hashlib


def get_user(db_path, username):
    conn = sqlite3.connect(db_path)
    cur = conn.cursor()
    cur.execute("SELECT 1")
    return cur.fetchone()


class Account:
    def verify(self, password):
        return hashlib.sha256(password.encode()).hexdigest()
`
	p := newHeuristicParser()
	pf, err := p.Parse("auth.py", src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if pf.Language != "python" {
		t.Errorf("language = %q, want python", pf.Language)
	}
	var lite []sigLite
	for _, s := range pf.Signatures {
		lite = append(lite, sigLite{s.Name, s.Kind})
	}
	for _, want := range []string{"get_user", "Account", "verify"} {
		if !hasSignature(lite, want) {
			t.Errorf("missing signature %q; got %+v", want, lite)
		}
	}
	if len(pf.Imports) == 0 {
		t.Errorf("expected imports, got none")
	}
	if len(pf.Calls) == 0 {
		t.Errorf("expected at least one call edge")
	}
	// connect/cursor/execute should be attributed to get_user.
	found := false
	for _, c := range pf.Calls {
		if c.Caller == "get_user" && (c.Callee == "connect" || c.Callee == "execute") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected get_user call edge, got %+v", pf.Calls)
	}
}

func TestSupportsAndLanguage(t *testing.T) {
	p := newHeuristicParser()
	if !p.Supports(".go") || !p.Supports(".PY") {
		t.Error("expected .go and .PY supported")
	}
	if p.Supports(".md") {
		t.Error(".md should not be supported")
	}
	if p.Language(".ts") != "typescript" {
		t.Errorf("Language(.ts) = %q", p.Language(".ts"))
	}
}

func TestParseUnsupported(t *testing.T) {
	p := newHeuristicParser()
	pf, err := p.Parse("README.md", "# hello")
	if err != nil {
		t.Fatalf("Parse should not fatally error: %v", err)
	}
	if pf.ParseError == "" {
		t.Error("expected ParseError set for unsupported extension")
	}
}
