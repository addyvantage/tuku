package brief

import (
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/promptir"
)

type Verbosity string

const (
	VerbosityCompact  Verbosity = "compact"
	VerbosityStandard Verbosity = "standard"
	VerbosityVerbose  Verbosity = "verbose"
)

type Posture string

const (
	PostureExecutionReady      Posture = "EXECUTION_READY"
	PostureClarificationNeeded Posture = "CLARIFICATION_NEEDED"
	PosturePlanningOriented    Posture = "PLANNING_ORIENTED"
	PostureValidationOriented  Posture = "VALIDATION_ORIENTED"
	PostureRepairOriented      Posture = "REPAIR_ORIENTED"
)

type ExecutionBrief struct {
	Version                 int                   `json:"version"`
	BriefID                 common.BriefID        `json:"brief_id"`
	TaskID                  common.TaskID         `json:"task_id"`
	IntentID                common.IntentID       `json:"intent_id"`
	CapsuleVersion          common.CapsuleVersion `json:"capsule_version"`
	CreatedAt               time.Time             `json:"created_at"`
	Posture                 Posture               `json:"posture,omitempty"`
	Objective               string                `json:"objective"`
	RequestedOutcome        string                `json:"requested_outcome,omitempty"`
	NormalizedAction        string                `json:"normalized_action"`
	ScopeSummary            string                `json:"scope_summary,omitempty"`
	ScopeIn                 []string              `json:"scope_in"`
	ScopeOut                []string              `json:"scope_out"`
	Constraints             []string              `json:"constraints"`
	DoneCriteria            []string              `json:"done_criteria"`
	AmbiguityFlags          []string              `json:"ambiguity_flags,omitempty"`
	ClarificationQuestions  []string              `json:"clarification_questions,omitempty"`
	RequiresClarification   bool                  `json:"requires_clarification"`
	WorkerFraming           string                `json:"worker_framing,omitempty"`
	BoundedEvidenceMessages int                   `json:"bounded_evidence_messages,omitempty"`
	PromptTriage            PromptTriage          `json:"prompt_triage,omitempty"`
	ContextPackID           common.ContextPackID  `json:"context_pack_id"`
	TaskMemoryID            common.MemoryID       `json:"task_memory_id,omitempty"`
	MemoryCompression       MemoryCompression     `json:"memory_compression,omitempty"`
	PromptIR                promptir.Packet       `json:"prompt_ir,omitempty"`
	BenchmarkID             common.BenchmarkID    `json:"benchmark_id,omitempty"`
	Verbosity               Verbosity             `json:"verbosity"`
	PolicyProfileID         string                `json:"policy_profile_id"`
	BriefHash               string                `json:"brief_hash"`
}

type PromptTriage struct {
	Applied                      bool     `json:"applied,omitempty"`
	Reason                       string   `json:"reason,omitempty"`
	Summary                      string   `json:"summary,omitempty"`
	SearchTerms                  []string `json:"search_terms,omitempty"`
	CandidateFiles               []string `json:"candidate_files,omitempty"`
	FilesScanned                 int      `json:"files_scanned,omitempty"`
	RawPromptTokenEstimate       int      `json:"raw_prompt_token_estimate,omitempty"`
	RewrittenPromptTokenEstimate int      `json:"rewritten_prompt_token_estimate,omitempty"`
	SearchSpaceTokenEstimate     int      `json:"search_space_token_estimate,omitempty"`
	SelectedContextTokenEstimate int      `json:"selected_context_token_estimate,omitempty"`
	ContextTokenSavingsEstimate  int      `json:"context_token_savings_estimate,omitempty"`
}

type MemoryCompression struct {
	Applied                   bool    `json:"applied,omitempty"`
	Summary                   string  `json:"summary,omitempty"`
	FullHistoryTokenEstimate  int     `json:"full_history_token_estimate,omitempty"`
	ResumePromptTokenEstimate int     `json:"resume_prompt_token_estimate,omitempty"`
	MemoryCompactionRatio     float64 `json:"memory_compaction_ratio,omitempty"`
	ConfirmedFactsCount       int     `json:"confirmed_facts_count,omitempty"`
	TouchedFilesCount         int     `json:"touched_files_count,omitempty"`
	ValidatorsRunCount        int     `json:"validators_run_count,omitempty"`
	CandidateFilesCount       int     `json:"candidate_files_count,omitempty"`
	RejectedHypothesesCount   int     `json:"rejected_hypotheses_count,omitempty"`
	UnknownsCount             int     `json:"unknowns_count,omitempty"`
}

type Builder interface {
	Build(input BuildInput) (ExecutionBrief, error)
}

type BuildInput struct {
	TaskID                  common.TaskID
	IntentID                common.IntentID
	CapsuleVersion          common.CapsuleVersion
	Posture                 Posture
	Goal                    string
	RequestedOutcome        string
	NormalizedAction        string
	ScopeSummary            string
	Constraints             []string
	ScopeHints              []string
	ScopeOutHints           []string
	DoneCriteria            []string
	AmbiguityFlags          []string
	ClarificationQuestions  []string
	RequiresClarification   bool
	WorkerFraming           string
	BoundedEvidenceMessages int
	PromptTriage            PromptTriage
	ContextPackID           common.ContextPackID
	TaskMemoryID            common.MemoryID
	MemoryCompression       MemoryCompression
	PromptIR                promptir.Packet
	BenchmarkID             common.BenchmarkID
	Verbosity               Verbosity
	PolicyProfileID         string
}
