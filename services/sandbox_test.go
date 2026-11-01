package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/saketh1125/argus/config"
)

func TestSandboxUnavailableCannotExecuteOrWrite(t *testing.T) {
	for _, cfg := range []*config.Config{{ForceMock: true}, {E2BAPIKey: "present"}, {}} {
		marker := filepath.Join(t.TempDir(), "marker")
		s := selectSandbox(cfg)
		res, err := s.Run(context.Background(), SandboxSpec{
			Files:    map[string]string{"../../escape": "bad"},
			Commands: [][]string{{"touch", marker + "; echo injected"}},
		})
		if !errors.Is(err, ErrSandboxUnavailable) || res != nil || s.Live() || s.Name() != "unavailable" {
			t.Fatalf("expected unavailable sandbox, got %v %v %s", res, err, s.Name())
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("command ran or marker inaccessible: %v", err)
		}
	}
}
