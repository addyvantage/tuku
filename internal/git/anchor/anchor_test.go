package anchor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCaptureNonGitDirectoryReturnsMinimalSnapshot(t *testing.T) {
	provider := NewGitProvider()
	snap := provider.Capture(context.Background(), t.TempDir())
	if snap.RepoRoot == "" {
		t.Fatal("expected repo root to be set")
	}
	// Branch/head may be empty in non-git directory; this is expected in milestone 2.
}

func TestResolveRepoRootReturnsGitTopLevel(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repoRoot := t.TempDir()
	cmd := exec.Command("git", "init", repoRoot)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v (%s)", err, string(out))
	}
	nested := filepath.Join(repoRoot, "nested", "deeper")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested repo dir: %v", err)
	}

	resolved, err := ResolveRepoRoot(context.Background(), nested)
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	expected, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		expected = filepath.Clean(repoRoot)
	}
	if resolvedExpected, err := filepath.EvalSymlinks(resolved); err == nil {
		resolved = resolvedExpected
	}
	if resolved != filepath.Clean(expected) {
		t.Fatalf("expected repo root %q, got %q", filepath.Clean(expected), resolved)
	}
}

func TestResolveRepoRootRejectsNonGitDirectory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	if _, err := ResolveRepoRoot(context.Background(), t.TempDir()); err == nil {
		t.Fatal("expected non-git directory to fail repo root resolution")
	}
}
