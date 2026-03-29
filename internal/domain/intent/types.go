package intent

import (
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/phase"
)

type Class string

const (
	ClassStartTask    Class = "START_TASK"
	ClassContinueTask Class = "CONTINUE_TASK"
	ClassImplement    Class = "IMPLEMENT_CHANGE"
	ClassDebug        Class = "DEBUG_FIX"
	ClassValidate     Class = "RUN_VALIDATION"
	ClassExplain      Class = "STATUS_OR_EXPLAIN"
	ClassApproval     Class = "APPROVAL_RESPONSE"
	ClassReplan       Class = "REPLAN_SCOPE"
	ClassPause        Class = "PAUSE_OR_STOP"
	ClassComplete     Class = "MARK_COMPLETE"
)

type Posture string

const (
	PostureExploratoryAmbiguous Posture = "EXPLORATORY_AMBIGUOUS"
	PosturePlanning             Posture = "PLANNING"
	PostureExecutionReady       Posture = "EXECUTION_READY"
	PostureValidationFocused    Posture = "VALIDATION_FOCUSED"
	PostureRepairRecovery       Posture = "REPAIR_RECOVERY"
	PostureClarificationNeeded  Posture = "CLARIFICATION_NEEDED"
)

type Readiness string

const (
	ReadinessExecutionReady      Readiness = "EXECUTION_READY"
	ReadinessClarificationNeeded Readiness = "CLARIFICATION_NEEDED"
	ReadinessPlanningInProgress  Readiness = "PLANNING_IN_PROGRESS"
	ReadinessValidationFocused   Readiness = "VALIDATION_FOCUSED"
	ReadinessRepairRecovery      Readiness = "REPAIR_RECOVERY_FOCUSED"
)

type State struct {
	Version                 int                `json:"version"`
	IntentID                common.IntentID    `json:"intent_id"`
	TaskID                  common.TaskID      `json:"task_id"`
	Class                   Class              `json:"class"`
	Posture                 Posture            `json:"posture,omitempty"`
	ExecutionReadiness      Readiness          `json:"execution_readiness,omitempty"`
	Objective               string             `json:"objective,omitempty"`
	RequestedOutcome        string             `json:"requested_outcome,omitempty"`
	NormalizedAction        string             `json:"normalized_action"`
	ScopeSummary            string             `json:"scope_summary,omitempty"`
	ExplicitConstraints     []string           `json:"explicit_constraints,omitempty"`
	DoneCriteria            []string           `json:"done_criteria,omitempty"`
	AmbiguityFlags          []string           `json:"ambiguity_flags"`
	ClarificationQuestions  []string           `json:"clarification_questions,omitempty"`
	RequiresClarification   bool               `json:"requires_clarification"`
	ReadinessReason         string             `json:"readiness_reason,omitempty"`
	CompilationNotes        string             `json:"compilation_notes,omitempty"`
	BoundedEvidenceMessages int                `json:"bounded_evidence_messages,omitempty"`
	Confidence              float64            `json:"confidence"`
	SourceMessageIDs        []common.MessageID `json:"source_message_ids"`
	ProposedPhase           phase.Phase        `json:"proposed_phase"`
	CreatedAt               time.Time          `json:"created_at"`
}

type Compiler interface {
	Compile(input CompileInput) (State, error)
}

type CompileInput struct {
	TaskID            common.TaskID
	LatestMessage     string
	RecentMessages    []string
	CurrentPhase      phase.Phase
	CurrentBlockers   []string
	CurrentGoal       string
	RepoAnchorSummary string
}
