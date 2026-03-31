package taskmemory

import (
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/phase"
)

type Snapshot struct {
	Version int `json:"version"`

	MemoryID common.MemoryID `json:"memory_id"`
	TaskID   common.TaskID   `json:"task_id"`
	BriefID  common.BriefID  `json:"brief_id,omitempty"`
	RunID    common.RunID    `json:"run_id,omitempty"`

	Phase  phase.Phase `json:"phase,omitempty"`
	Source string      `json:"source,omitempty"`

	Summary            string   `json:"summary,omitempty"`
	ConfirmedFacts     []string `json:"confirmed_facts,omitempty"`
	RejectedHypotheses []string `json:"rejected_hypotheses,omitempty"`
	Unknowns           []string `json:"unknowns,omitempty"`
	UserConstraints    []string `json:"user_constraints,omitempty"`
	TouchedFiles       []string `json:"touched_files,omitempty"`
	ValidatorsRun      []string `json:"validators_run,omitempty"`
	CandidateFiles     []string `json:"candidate_files,omitempty"`
	LastBlocker        string   `json:"last_blocker,omitempty"`
	NextSuggestedStep  string   `json:"next_suggested_step,omitempty"`

	FullHistoryTokenEstimate  int     `json:"full_history_token_estimate,omitempty"`
	ResumePromptTokenEstimate int     `json:"resume_prompt_token_estimate,omitempty"`
	MemoryCompactionRatio     float64 `json:"memory_compaction_ratio,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

type Repository interface {
	Save(snapshot Snapshot) error
	Get(memoryID common.MemoryID) (Snapshot, error)
	LatestByTask(taskID common.TaskID) (Snapshot, error)
}
