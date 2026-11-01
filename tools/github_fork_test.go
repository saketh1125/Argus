package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInspectExistingForkVerifiesIdentityWithoutWrites(t *testing.T) {
	for _, tc := range []struct {
		name                string
		forkOwner, parentID int
		wantErr             bool
	}{
		{"valid", 10, 20, false}, {"wrong owner", 11, 20, true}, {"wrong parent", 10, 21, true}, {"missing parent identity", 10, 0, true}, {"missing owner identity", 0, 20, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("unexpected write: %s", r.Method)
					w.WriteHeader(405)
					return
				}
				switch r.URL.Path {
				case "/user":
					fmt.Fprint(w, `{"id":10,"login":"contributor"}`)
				case "/repos/upstream/project":
					fmt.Fprint(w, `{"id":20,"name":"project","full_name":"upstream/project","owner":{"id":30,"login":"upstream"}}`)
				case "/repos/contributor/project":
					fmt.Fprintf(w, `{"id":40,"name":"project","full_name":"contributor/project","fork":true,"owner":{"id":%d,"login":"contributor"},"parent":{"id":%d,"full_name":"upstream/project"}}`, tc.forkOwner, tc.parentID)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			g := &githubREST{token: "test-only", http: server.Client(), baseURL: server.URL}
			fork, err := g.inspectExistingFork(context.Background(), "upstream", "project")
			if tc.wantErr {
				if err == nil {
					t.Fatal("invalid fork accepted")
				}
				return
			}
			if err != nil || fork.ForkID != 40 || fork.UpstreamID != 20 || fork.OwnerID != 10 {
				t.Fatalf("unexpected fork: %+v %v", fork, err)
			}
		})
	}
}

func TestForkMetadataDoesNotFollowRedirects(t *testing.T) {
	contacted := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { contacted = true; w.WriteHeader(http.StatusOK) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer server.Close()
	g := &githubREST{token: "test-only", http: server.Client(), baseURL: server.URL}
	if _, err := g.inspectExistingFork(context.Background(), "upstream", "project"); err == nil {
		t.Fatal("redirect accepted")
	}
	if contacted {
		t.Fatal("followed a redirect with authenticated request")
	}
}

func TestForkMetadataRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(strings.Repeat(" ", (1<<20)+1))) }))
	defer server.Close()
	g := &githubREST{token: "test-only", http: server.Client(), baseURL: server.URL}
	if _, err := g.inspectExistingFork(context.Background(), "upstream", "project"); err == nil {
		t.Fatal("oversized response accepted")
	}
}

func TestGitHubReadRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(strings.Repeat("x", (1<<20)+1))) }))
	defer server.Close()
	g := &githubREST{token: "test-only", http: server.Client()}
	if err := g.do(context.Background(), http.MethodGet, server.URL, nil, nil); err == nil {
		t.Fatal("oversized response accepted")
	}
}

func TestGitHubReadDoesNotFollowRedirects(t *testing.T) {
	contacted := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { contacted = true }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer server.Close()
	g := &githubREST{token: "test-only", http: server.Client()}
	if err := g.do(context.Background(), http.MethodGet, server.URL, nil, nil); err == nil {
		t.Fatal("redirect accepted")
	}
	if contacted {
		t.Fatal("followed authenticated redirect")
	}
}

func TestGitHubGenericRequestRejectsWrites(t *testing.T) {
	contacted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { contacted = true; w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	g := &githubREST{token: "test-only", http: server.Client()}
	if err := g.do(context.Background(), http.MethodPost, server.URL, map[string]string{"x": "y"}, nil); err == nil {
		t.Fatal("generic write accepted")
	}
	if contacted {
		t.Fatal("write reached server")
	}
}

func TestGitHubReadCancellationInterruptsRetry(t *testing.T) {
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	g := &githubREST{token: "test-only", http: server.Client()}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- g.do(ctx, http.MethodGet, server.URL, nil, nil) }()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("retry did not honor cancellation")
	}
}
