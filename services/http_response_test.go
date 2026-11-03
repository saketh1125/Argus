package services

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type fixedResponseRoundTripper struct{ body string }

func (r fixedResponseRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode:    http.StatusOK,
		Header:        make(http.Header),
		Body:          io.NopCloser(strings.NewReader(r.body)),
		ContentLength: -1,
		Request:       req,
	}, nil
}

type countingResponseReader struct {
	remaining int
	read      int
}

func (r *countingResponseReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := len(p)
	if n > r.remaining {
		n = r.remaining
	}
	for i := 0; i < n; i++ {
		p[i] = 'x'
	}
	r.remaining -= n
	r.read += n
	return n, nil
}

func TestReadServiceResponseBoundsUnknownLengthBodies(t *testing.T) {
	source := &countingResponseReader{remaining: maxServiceResponseBytes * 10}
	resp := &http.Response{Body: io.NopCloser(source), ContentLength: -1}
	if _, err := readServiceResponse(resp); err == nil || !strings.Contains(err.Error(), "response exceeds") {
		t.Fatalf("readServiceResponse error = %v, want response-size error", err)
	}
	if source.read != maxServiceResponseBytes+1 {
		t.Fatalf("bytes read = %d, want at most limit+1 (%d)", source.read, maxServiceResponseBytes+1)
	}
}

func TestLiveServiceAdaptersRejectOversizedResponses(t *testing.T) {
	largeBody := strings.Repeat("x", maxServiceResponseBytes+1)
	httpClient := &http.Client{Transport: fixedResponseRoundTripper{body: largeBody}}
	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "NIM",
			run: func() error {
				client := newNIMClient("https://provider.invalid/v1", "test-key", "test-model", NewRateLimiter(60))
				client.http = httpClient
				_, err := client.Complete(context.Background(), CompletionRequest{User: "x"})
				return err
			},
		},
		{
			name: "Ollama",
			run: func() error {
				client := &ollamaEmbed{host: "https://provider.invalid", model: "test-model", http: httpClient}
				_, err := client.Embed(context.Background(), []string{"x"})
				return err
			},
		},
		{
			name: "Jina",
			run: func() error {
				client := &jinaEmbed{apiKey: "test-key", model: "test-model", http: httpClient}
				_, err := client.Embed(context.Background(), []string{"x"})
				return err
			},
		},
		{
			name: "Gemini",
			run: func() error {
				client := &geminiEmbed{apiKey: "test-key", model: "models/test", http: httpClient}
				_, err := client.Embed(context.Background(), []string{"x"})
				return err
			},
		},
		{
			name: "Qdrant",
			run: func() error {
				client := &qdrantStore{baseURL: "https://provider.invalid", http: httpClient}
				_, _, err := client.doJSON(context.Background(), http.MethodGet, "/collections", nil)
				return err
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%d-byte limit", maxServiceResponseBytes)) {
				t.Fatalf("adapter error = %v, want bounded-response error", err)
			}
		})
	}
}

func TestNIMRetryAfterIsCappedAndCancellationAware(t *testing.T) {
	tests := []struct {
		value string
		want  time.Duration
	}{
		{value: "", want: 10 * time.Second},
		{value: "2", want: 2 * time.Second},
		{value: "3600", want: nimMaxRetryAfter},
		{value: "-1", want: 10 * time.Second},
		{value: "tomorrow", want: 10 * time.Second},
	}
	for _, tc := range tests {
		if got := nimRetryAfterDelay(tc.value); got != tc.want {
			t.Errorf("nimRetryAfterDelay(%q) = %s, want %s", tc.value, got, tc.want)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := waitForNIMRetry(ctx, nimMaxRetryAfter); err != context.Canceled {
		t.Fatalf("waitForNIMRetry error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("canceled retry wait took %s, want prompt cancellation", elapsed)
	}
}

type cancelOnRetryResponse struct{ cancel context.CancelFunc }

func (r cancelOnRetryResponse) RoundTrip(req *http.Request) (*http.Response, error) {
	r.cancel()
	return &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Retry-After": []string{"86400"}},
		Body:       io.NopCloser(strings.NewReader("retry later")),
		Request:    req,
	}, nil
}

func TestNIMRetryAfterCancellationDoesNotSleep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := newNIMClient("https://provider.invalid/v1", "test-key", "test-model", NewRateLimiter(60))
	client.http = &http.Client{Transport: cancelOnRetryResponse{cancel: cancel}}

	start := time.Now()
	_, err := client.Complete(ctx, CompletionRequest{User: "x"})
	if err != context.Canceled {
		t.Fatalf("Complete error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("canceled 24-hour Retry-After took %s, want prompt cancellation", elapsed)
	}
}
