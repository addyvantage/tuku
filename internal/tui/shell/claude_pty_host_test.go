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

func TestClaudePTYHostStartBlocksWhenAuthIsMissing(t *testing.T) {
	origLookPath := workerPrereqLookPath
	defer func() { workerPrereqLookPath = origLookPath }()

	bin := writeWorkerProbeScript(t, `#!/bin/sh
if [ "$1" = "auth" ] && [ "$2" = "status" ]; then
  echo '{"loggedIn":false}'
  exit 1
fi
echo "unexpected"
exit 0
`)
	workerPrereqLookPath = func(string) (string, error) { return bin, nil }

	host := NewDefaultClaudePTYHost()
	err := host.Start(context.Background(), Snapshot{Repo: RepoAnchor{RepoRoot: t.TempDir()}})
	if err == nil {
		t.Fatal("expected unauthenticated claude to block PTY host start")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "sign") && !strings.Contains(strings.ToLower(err.Error()), "logged") {
		t.Fatalf("expected auth-related error, got %q", err)
	}
	if host.Status().State != HostStateFailed {
		t.Fatalf("expected failed host state, got %s", host.Status().State)
	}
}
