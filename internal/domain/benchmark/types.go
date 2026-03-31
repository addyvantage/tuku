package benchmark

import (
	"time"

	"tuku/internal/domain/common"
)

type Run struct {
	Version int `json:"version"`

	BenchmarkID common.BenchmarkID `json:"benchmark_id"`
	TaskID      common.TaskID      `json:"task_id"`
	BriefID     common.BriefID     `json:"brief_id,omitempty"`
	RunID       common.RunID       `json:"run_id,omitempty"`
	Source      string             `json:"source,omitempty"`

	RawPromptTokenEstimate        int      `json:"raw_prompt_token_estimate,omitempty"`
	DispatchPromptTokenEstimate   int      `json:"dispatch_prompt_token_estimate,omitempty"`
	StructuredPromptTokenEstimate int      `json:"structured_prompt_token_estimate,omitempty"`
	SelectedContextTokenEstimate  int      `json:"selected_context_token_estimate,omitempty"`
	EstimatedTokenSavings         int      `json:"estimated_token_savings,omitempty"`
	FilesScanned                  int      `json:"files_scanned,omitempty"`
	RankedTargetCount             int      `json:"ranked_target_count,omitempty"`
	CandidateRecallAt3            float64  `json:"candidate_recall_at_3,omitempty"`
	StructuredCheaper             bool     `json:"structured_cheaper,omitempty"`
	DefaultSerializer             string   `json:"default_serializer,omitempty"`
	ConfidenceValue               float64  `json:"confidence_value,omitempty"`
	ConfidenceLevel               string   `json:"confidence_level,omitempty"`
	Summary                       string   `json:"summary,omitempty"`
	ChangedFiles                  []string `json:"changed_files,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Repository interface {
	Save(run Run) error
	Get(benchmarkID common.BenchmarkID) (Run, error)
	LatestByTask(taskID common.TaskID) (Run, error)
}
