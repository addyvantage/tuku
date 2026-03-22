package shell

import (
	"context"
	"strings"
	"testing"
)

func TestClaudePTYHostStartRequiresRepoRoot(t *testing.T) {
	host := NewDefaultClaudePTYHost()
	err := host.Start(context.Background(), Snapshot{})
	if err == nil {
		t.Fatal("expected missing repo root to block PTY host start")
	}
	if !strings.Contains(err.Error(), "repo root is required") {
		t.Fatalf("unexpected start error %q", err)
	}
	if host.Status().State != HostStateFailed {
		t.Fatalf("expected failed host state, got %s", host.Status().State)
	}
}
