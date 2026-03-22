package anchor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Snapshot is a minimal repo identity snapshot for milestone 2.
type Snapshot struct {
	RepoRoot         string    `json:"repo_root"`
	Branch           string    `json:"branch"`
	HeadSHA          string    `json:"head_sha"`
	WorkingTreeDirty bool      `json:"working_tree_dirty"`
	CapturedAt       time.Time `json:"captured_at"`
}

type Provider interface {
	Capture(ctx context.Context, repoRoot string) Snapshot
}

type GitProvider struct{}

func NewGitProvider() *GitProvider {
	return &GitProvider{}
}

func ResolveRepoRoot(ctx context.Context, startDir string) (string, error) {
	root := filepath.Clean(strings.TrimSpace(startDir))
	if root == "" {
		root = "."
	}
	resolved, ok := gitLine(ctx, root, "rev-parse", "--show-toplevel")
	if !ok || strings.TrimSpace(resolved) == "" {
		return "", fmt.Errorf("git repo root not found from %s", root)
	}
	return filepath.Clean(resolved), nil
}

func (p *GitProvider) Capture(ctx context.Context, repoRoot string) Snapshot {
	root := filepath.Clean(strings.TrimSpace(repoRoot))
	if root == "" {
		root = "."
	}

	snap := Snapshot{RepoRoot: root, CapturedAt: time.Now().UTC()}

	if branch, ok := gitLine(ctx, root, "rev-parse", "--abbrev-ref", "HEAD"); ok {
		snap.Branch = branch
	}
	if head, ok := gitLine(ctx, root, "rev-parse", "HEAD"); ok {
		snap.HeadSHA = head
	}
	if status, ok := gitLine(ctx, root, "status", "--porcelain"); ok {
		snap.WorkingTreeDirty = strings.TrimSpace(status) != ""
	}

	return snap
}

func gitLine(ctx context.Context, repoRoot string, args ...string) (string, bool) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repoRoot}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	line := strings.TrimSpace(string(bytes.TrimSpace(out)))
	return line, true
}
