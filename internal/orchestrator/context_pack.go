package orchestrator

import (
	"bufio"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"tuku/internal/domain/brief"
	"tuku/internal/domain/capsule"
	"tuku/internal/domain/common"
	contextdomain "tuku/internal/domain/context"
)

const (
	contextPackMaxFiles         = 6
	contextPackCompactMaxLines  = 40
	contextPackStandardMaxLines = 80
	contextPackVerboseMaxLines  = 140
	contextPackSnippetMaxBytes  = 16 * 1024
	contextPackCompactBudget    = 1200
	contextPackStandardBudget   = 2400
	contextPackVerboseBudget    = 4800
)

func (c *Coordinator) buildContextPack(caps capsule.WorkCapsule, scopeHints []string, mode contextdomain.Mode, tokenBudget int) (contextdomain.Pack, error) {
	if mode == "" {
		mode = contextdomain.ModeCompact
	}
	if tokenBudget <= 0 {
		tokenBudget = tokenBudgetForContextMode(mode)
	}

	repoRoot := strings.TrimSpace(caps.RepoRoot)
	if repoRoot == "" {
		repoRoot = "."
	}
	repoRoot = filepath.Clean(repoRoot)

	candidates := orderedContextCandidates(repoRoot, caps.TouchedFiles, scopeHints)
	includedFiles := make([]string, 0, len(candidates))
	includedSnippets := make([]contextdomain.Snippet, 0, len(candidates))
	for _, relPath := range candidates {
		if len(includedFiles) >= contextPackMaxFiles {
			break
		}
		snippet, ok := readContextSnippet(filepath.Join(repoRoot, relPath), relPath, maxLinesForContextMode(mode))
		if !ok {
			continue
		}
		includedFiles = append(includedFiles, relPath)
		includedSnippets = append(includedSnippets, snippet)
	}

	selectionRationale := []string{
		fmt.Sprintf("selected up to %d repo-local files for bounded worker context", contextPackMaxFiles),
		fmt.Sprintf("prioritized touched files (%d) and scope hints (%d)", len(caps.TouchedFiles), len(scopeHints)),
		fmt.Sprintf("captured %d snippet(s) in %s mode", len(includedSnippets), mode),
	}
	if len(includedFiles) == 0 {
		selectionRationale = append(selectionRationale, "no readable repo-local files matched the current bounded scope")
	}

	pack := contextdomain.Pack{
		ContextPackID:      common.ContextPackID(c.idGenerator("ctx")),
		TaskID:             caps.TaskID,
		Mode:               mode,
		TokenBudget:        tokenBudget,
		RepoAnchorHash:     caps.HeadSHA,
		FreshnessState:     "current",
		IncludedFiles:      includedFiles,
		IncludedSnippets:   includedSnippets,
		SelectionRationale: selectionRationale,
		CreatedAt:          c.clock(),
	}
	pack.PackHash = hashContextPack(pack)
	return pack, nil
}

func contextModeForVerbosity(v brief.Verbosity) contextdomain.Mode {
	switch v {
	case brief.VerbosityVerbose:
		return contextdomain.ModeVerbose
	case brief.VerbosityStandard:
		return contextdomain.ModeStandard
	default:
		return contextdomain.ModeCompact
	}
}

func tokenBudgetForContextMode(mode contextdomain.Mode) int {
	switch mode {
	case contextdomain.ModeVerbose:
		return contextPackVerboseBudget
	case contextdomain.ModeStandard:
		return contextPackStandardBudget
	default:
		return contextPackCompactBudget
	}
}

func maxLinesForContextMode(mode contextdomain.Mode) int {
	switch mode {
	case contextdomain.ModeVerbose:
		return contextPackVerboseMaxLines
	case contextdomain.ModeStandard:
		return contextPackStandardMaxLines
	default:
		return contextPackCompactMaxLines
	}
}

func orderedContextCandidates(repoRoot string, touchedFiles []string, scopeHints []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(touchedFiles)+len(scopeHints)+2)
	prefixCount := 0
	add := func(path string) {
		rel, ok := normalizeContextCandidate(repoRoot, path)
		if !ok {
			return
		}
		if _, exists := seen[rel]; exists {
			return
		}
		seen[rel] = struct{}{}
		out = append(out, rel)
	}

	for _, path := range touchedFiles {
		before := len(out)
		add(path)
		if len(out) > before {
			prefixCount++
		}
	}
	for _, path := range scopeHints {
		add(path)
	}
	add("AGENTS.md")
	add("README.md")

	sort.Strings(out[prefixCount:])
	return out
}

func normalizeContextCandidate(repoRoot string, path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false
	}

	absPath := path
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(repoRoot, path)
	}
	absPath = filepath.Clean(absPath)
	rel, err := filepath.Rel(repoRoot, absPath)
	if err != nil {
		return "", false
	}
	if strings.HasPrefix(rel, "..") || rel == "." {
		return "", false
	}
	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func readContextSnippet(absPath string, relPath string, maxLines int) (contextdomain.Snippet, bool) {
	file, err := os.Open(absPath)
	if err != nil {
		return contextdomain.Snippet{}, false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var (
		lines []string
		size  int
	)
	for scanner.Scan() {
		line := scanner.Text()
		size += len(line) + 1
		if size > contextPackSnippetMaxBytes {
			break
		}
		lines = append(lines, line)
		if maxLines > 0 && len(lines) >= maxLines {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return contextdomain.Snippet{}, false
	}
	if len(lines) == 0 {
		return contextdomain.Snippet{}, false
	}

	return contextdomain.Snippet{
		Path:      filepath.ToSlash(relPath),
		StartLine: 1,
		EndLine:   len(lines),
		Content:   strings.Join(lines, "\n"),
	}, true
}

func hashContextPack(pack contextdomain.Pack) string {
	payload := struct {
		TaskID             common.TaskID           `json:"task_id"`
		Mode               contextdomain.Mode      `json:"mode"`
		TokenBudget        int                     `json:"token_budget"`
		RepoAnchorHash     string                  `json:"repo_anchor_hash"`
		FreshnessState     string                  `json:"freshness_state"`
		IncludedFiles      []string                `json:"included_files"`
		IncludedSnippets   []contextdomain.Snippet `json:"included_snippets"`
		SelectionRationale []string                `json:"selection_rationale"`
	}{
		TaskID:             pack.TaskID,
		Mode:               pack.Mode,
		TokenBudget:        pack.TokenBudget,
		RepoAnchorHash:     pack.RepoAnchorHash,
		FreshnessState:     pack.FreshnessState,
		IncludedFiles:      append([]string{}, pack.IncludedFiles...),
		IncludedSnippets:   append([]contextdomain.Snippet{}, pack.IncludedSnippets...),
		SelectionRationale: append([]string{}, pack.SelectionRationale...),
	}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (c *Coordinator) resolveExecutionContextPack(caps capsule.WorkCapsule, b brief.ExecutionBrief) (contextdomain.Pack, error) {
	if b.ContextPackID != "" {
		pack, err := c.store.ContextPacks().Get(b.ContextPackID)
		if err == nil {
			return pack, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return contextdomain.Pack{}, err
		}
	}

	mode := contextModeForVerbosity(b.Verbosity)
	pack, err := c.buildContextPack(caps, b.ScopeIn, mode, tokenBudgetForContextMode(mode))
	if err != nil {
		return contextdomain.Pack{}, err
	}
	if err := c.store.ContextPacks().Save(pack); err != nil {
		return contextdomain.Pack{}, err
	}
	return pack, nil
}
