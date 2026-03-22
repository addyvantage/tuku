package context

import (
	"time"

	"tuku/internal/domain/common"
)

type Mode string

const (
	ModeCompact  Mode = "compact"
	ModeStandard Mode = "standard"
	ModeVerbose  Mode = "verbose"
)

type Snippet struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Content   string `json:"content"`
}

type Pack struct {
	ContextPackID      common.ContextPackID `json:"context_pack_id"`
	TaskID             common.TaskID        `json:"task_id"`
	Mode               Mode                 `json:"mode"`
	TokenBudget        int                  `json:"token_budget"`
	RepoAnchorHash     string               `json:"repo_anchor_hash"`
	FreshnessState     string               `json:"freshness_state"`
	IncludedFiles      []string             `json:"included_files"`
	IncludedSnippets   []Snippet            `json:"included_snippets"`
	SelectionRationale []string             `json:"selection_rationale"`
	PackHash           string               `json:"pack_hash"`
	CreatedAt          time.Time            `json:"created_at"`
}

type Provider interface {
	Build(input BuildInput) (Pack, error)
}

type BuildInput struct {
	TaskID           common.TaskID
	Mode             Mode
	TokenBudget      int
	ScopeHints       []string
	TouchedFiles     []string
	FailingTestHints []string
	RepoRoot         string
	HeadSHA          string
}
