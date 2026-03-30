package brief

import (
	"time"

	"tuku/internal/domain/common"
)

type Verbosity string

const (
	VerbosityCompact  Verbosity = "compact"
	VerbosityStandard Verbosity = "standard"
	VerbosityVerbose  Verbosity = "verbose"
)

type Posture string

const (
	PostureExecutionReady     Posture = "EXECUTION_READY"
	PostureClarificationNeeded Posture = "CLARIFICATION_NEEDED"
	PosturePlanningOriented   Posture = "PLANNING_ORIENTED"
	PostureValidationOriented Posture = "VALIDATION_ORIENTED"
	PostureRepairOriented     Posture = "REPAIR_ORIENTED"
)

type ExecutionBrief struct {
	Version                int                  `json:"version"`
	BriefID                common.BriefID       `json:"brief_id"`
	TaskID                 common.TaskID        `json:"task_id"`
	IntentID               common.IntentID      `json:"intent_id"`
	CapsuleVersion         common.CapsuleVersion `json:"capsule_version"`
	CreatedAt              time.Time            `json:"created_at"`
	Posture                Posture              `json:"posture,omitempty"`
	Objective              string               `json:"objective"`
	RequestedOutcome       string               `json:"requested_outcome,omitempty"`
	NormalizedAction       string               `json:"normalized_action"`
	ScopeSummary           string               `json:"scope_summary,omitempty"`
	ScopeIn                []string             `json:"scope_in"`
	ScopeOut               []string             `json:"scope_out"`
	Constraints            []string             `json:"constraints"`
	DoneCriteria           []string             `json:"done_criteria"`
	AmbiguityFlags         []string             `json:"ambiguity_flags,omitempty"`
	ClarificationQuestions []string             `json:"clarification_questions,omitempty"`
	RequiresClarification  bool                 `json:"requires_clarification"`
	WorkerFraming          string               `json:"worker_framing,omitempty"`
	BoundedEvidenceMessages int                 `json:"bounded_evidence_messages,omitempty"`
	ContextPackID          common.ContextPackID `json:"context_pack_id"`
	Verbosity              Verbosity            `json:"verbosity"`
	PolicyProfileID        string               `json:"policy_profile_id"`
	BriefHash              string               `json:"brief_hash"`
}

type Builder interface {
	Build(input BuildInput) (ExecutionBrief, error)
}

type BuildInput struct {
	TaskID                 common.TaskID
	IntentID               common.IntentID
	CapsuleVersion         common.CapsuleVersion
	Posture                Posture
	Goal                   string
	RequestedOutcome       string
	NormalizedAction       string
	ScopeSummary           string
	Constraints            []string
	ScopeHints             []string
	ScopeOutHints          []string
	DoneCriteria           []string
	AmbiguityFlags         []string
	ClarificationQuestions []string
	RequiresClarification  bool
	WorkerFraming          string
	BoundedEvidenceMessages int
	ContextPackID          common.ContextPackID
	Verbosity              Verbosity
	PolicyProfileID        string
}
