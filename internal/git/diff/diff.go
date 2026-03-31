package diff

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type FileStat struct {
	Path       string `json:"path"`
	Insertions int    `json:"insertions"`
	Deletions  int    `json:"deletions"`
	Binary     bool   `json:"binary,omitempty"`
}

type Summary struct {
	FilesChanged int        `json:"files_changed"`
	Insertions   int        `json:"insertions"`
	Deletions    int        `json:"deletions"`
	Files        []FileStat `json:"files,omitempty"`
	Headline     string     `json:"headline,omitempty"`
}

func Capture(repoRoot string) (Summary, error) {
	repoRoot = cleanRepoRoot(repoRoot)
	if repoRoot == "" {
		repoRoot = "."
	}
	if _, err := exec.LookPath("git"); err != nil {
		return Summary{}, err
	}

	headVerified := exec.Command("git", "-C", repoRoot, "rev-parse", "--verify", "HEAD")
	if err := headVerified.Run(); err != nil {
		return Summary{Headline: "git diff unavailable: repository HEAD is not initialized yet"}, nil
	}

	cmd := exec.Command("git", "-C", repoRoot, "diff", "--numstat", "HEAD", "--")
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return Summary{}, fmt.Errorf("git diff --numstat: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return Summary{}, err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	stats := make([]FileStat, 0, len(lines))
	var insertions, deletions int
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		stat := FileStat{Path: filepath.ToSlash(strings.TrimSpace(parts[2]))}
		if parts[0] == "-" || parts[1] == "-" {
			stat.Binary = true
		} else {
			adds, err := strconv.Atoi(parts[0])
			if err == nil {
				stat.Insertions = adds
				insertions += adds
			}
			dels, err := strconv.Atoi(parts[1])
			if err == nil {
				stat.Deletions = dels
				deletions += dels
			}
		}
		stats = append(stats, stat)
	}

	summary := Summary{
		FilesChanged: len(stats),
		Insertions:   insertions,
		Deletions:    deletions,
		Files:        stats,
	}
	if len(stats) == 0 {
		summary.Headline = "git diff reports no tracked changes relative to HEAD"
	} else {
		summary.Headline = fmt.Sprintf("git diff relative to HEAD: %d file(s), +%d/-%d", len(stats), insertions, deletions)
	}
	return summary, nil
}

func Render(summary Summary) string {
	if strings.TrimSpace(summary.Headline) != "" {
		return summary.Headline
	}
	return fmt.Sprintf("git diff relative to HEAD: %d file(s), +%d/-%d", summary.FilesChanged, summary.Insertions, summary.Deletions)
}

func cleanRepoRoot(repoRoot string) string {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		return ""
	}
	return filepath.Clean(repoRoot)
}

func Bytes(summary Summary) []byte {
	var buf bytes.Buffer
	buf.WriteString(Render(summary))
	buf.WriteByte('\n')
	for _, file := range summary.Files {
		if file.Binary {
			fmt.Fprintf(&buf, "binary\t%s\n", file.Path)
			continue
		}
		fmt.Fprintf(&buf, "+%d\t-%d\t%s\n", file.Insertions, file.Deletions, file.Path)
	}
	return buf.Bytes()
}
