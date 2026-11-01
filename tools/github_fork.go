package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type githubUserIdentity struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}
type githubRepositoryIdentity struct {
	ID       int64              `json:"id"`
	Name     string             `json:"name"`
	FullName string             `json:"full_name"`
	Fork     bool               `json:"fork"`
	Owner    githubUserIdentity `json:"owner"`
	Parent   *struct {
		ID       int64  `json:"id"`
		FullName string `json:"full_name"`
	} `json:"parent"`
}

// forkSnapshot is read-only identity evidence; it is not permission to write or
// proof that a fork is current, approved, or safe for publication.
type forkSnapshot struct {
	OwnerID, UpstreamID, ForkID int64
	Owner, Upstream, Fork       string
}

func (g *githubREST) readForkMetadata(ctx context.Context, endpoint string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.baseURL+endpoint, nil)
	if err != nil {
		return fmt.Errorf("fork metadata: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	if g.http == nil {
		return fmt.Errorf("fork metadata: HTTP client unavailable")
	}
	client := *g.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fork metadata: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fork metadata: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return fmt.Errorf("fork metadata: response unavailable or too large")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("fork metadata: invalid response: %w", err)
	}
	return nil
}

// inspectExistingFork reads and cross-checks GitHub identity metadata. It never
// creates a fork, branch, commit, issue, or PR; publishing remains disabled.
func (g *githubREST) inspectExistingFork(ctx context.Context, owner, repo string) (forkSnapshot, error) {
	if _, _, err := parseGitHubURL("https://github.com/" + owner + "/" + repo); err != nil {
		return forkSnapshot{}, fmt.Errorf("invalid upstream identity")
	}
	var user githubUserIdentity
	if err := g.readForkMetadata(ctx, "/user", &user); err != nil {
		return forkSnapshot{}, err
	}
	if user.ID <= 0 {
		return forkSnapshot{}, fmt.Errorf("authenticated identity unavailable")
	}
	if _, _, err := parseGitHubURL("https://github.com/" + user.Login + "/" + repo); err != nil {
		return forkSnapshot{}, fmt.Errorf("authenticated identity invalid")
	}
	var upstream, fork githubRepositoryIdentity
	if err := g.readForkMetadata(ctx, "/repos/"+owner+"/"+repo, &upstream); err != nil {
		return forkSnapshot{}, err
	}
	if upstream.ID <= 0 || upstream.Owner.ID <= 0 || !strings.EqualFold(upstream.FullName, owner+"/"+repo) || !strings.EqualFold(upstream.Owner.Login, owner) || !strings.EqualFold(upstream.Name, repo) {
		return forkSnapshot{}, fmt.Errorf("upstream identity mismatch")
	}
	if err := g.readForkMetadata(ctx, "/repos/"+user.Login+"/"+repo, &fork); err != nil {
		return forkSnapshot{}, err
	}
	if fork.ID <= 0 || fork.ID == upstream.ID || !fork.Fork || fork.Owner.ID != user.ID || !strings.EqualFold(fork.Owner.Login, user.Login) || !strings.EqualFold(fork.Name, repo) || !strings.EqualFold(fork.FullName, user.Login+"/"+repo) || fork.Parent == nil || fork.Parent.ID != upstream.ID || !strings.EqualFold(fork.Parent.FullName, upstream.FullName) {
		return forkSnapshot{}, fmt.Errorf("fork identity mismatch")
	}
	return forkSnapshot{OwnerID: user.ID, UpstreamID: upstream.ID, ForkID: fork.ID, Owner: user.Login, Upstream: upstream.FullName, Fork: fork.FullName}, nil
}
