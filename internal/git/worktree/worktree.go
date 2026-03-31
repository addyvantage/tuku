package worktree

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

type Summary struct {
	DirtyPaths     int      `json:"dirty_paths"`
	ModifiedPaths  int      `json:"modified_paths"`
	AddedPaths     int      `json:"added_paths"`
	DeletedPaths   int      `json:"deleted_paths"`
	RenamedPaths   int      `json:"renamed_paths"`
	UntrackedPaths int      `json:"untracked_paths"`
	Paths          []string `json:"paths,omitempty"`
	Headline       string   `json:"headline,omitempty"`
}

func Capture(repoRoot string) (Summary, error) {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		repoRoot = "."
	}
	repoRoot = filepath.Clean(repoRoot)
	if _, err := exec.LookPath("git"); err != nil {
		return Summary{}, err
	}

	cmd := exec.Command("git", "-C", repoRoot, "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return Summary{}, err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	summary := Summary{}
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		path := strings.TrimSpace(line)
		if len(path) > 3 {
			path = strings.TrimSpace(path[3:])
		}
		path = filepath.ToSlash(path)
		if path != "" {
			summary.Paths = append(summary.Paths, path)
		}
		summary.DirtyPaths++

		code := "??"
		if len(line) >= 2 {
			code = line[:2]
		}
		switch {
		case strings.Contains(code, "R"):
			summary.RenamedPaths++
		case code == "??":
			summary.UntrackedPaths++
		case strings.Contains(code, "D"):
			summary.DeletedPaths++
		case strings.Contains(code, "A"):
			summary.AddedPaths++
		default:
			summary.ModifiedPaths++
		}
	}

	if summary.DirtyPaths == 0 {
		summary.Headline = "worktree clean"
	} else {
		summary.Headline = fmt.Sprintf(
			"worktree dirty: %d path(s) [modified=%d added=%d deleted=%d renamed=%d untracked=%d]",
			summary.DirtyPaths,
			summary.ModifiedPaths,
			summary.AddedPaths,
			summary.DeletedPaths,
			summary.RenamedPaths,
			summary.UntrackedPaths,
		)
	}
	return summary, nil
}

func Render(summary Summary) string {
	if strings.TrimSpace(summary.Headline) != "" {
		return summary.Headline
	}
	return fmt.Sprintf(
		"worktree dirty: %d path(s) [modified=%d added=%d deleted=%d renamed=%d untracked=%d]",
		summary.DirtyPaths,
		summary.ModifiedPaths,
		summary.AddedPaths,
		summary.DeletedPaths,
		summary.RenamedPaths,
		summary.UntrackedPaths,
	)
}
