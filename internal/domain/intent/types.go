package intent

import (
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/phase"
)

type Class string

const (
	ClassStartTask      Class = "START_TASK"
	ClassContinueTask   Class = "CONTINUE_TASK"
	ClassImplement      Class = "IMPLEMENT_CHANGE"
	ClassDebug          Class = "DEBUG_FIX"
	ClassValidate       Class = "RUN_VALIDATION"
	ClassExplain        Class = "STATUS_OR_EXPLAIN"
	ClassApproval       Class = "APPROVAL_RESPONSE"
	ClassReplan         Class = "REPLAN_SCOPE"
	ClassPause          Class = "PAUSE_OR_STOP"
	ClassComplete       Class = "MARK_COMPLETE"
)

type State struct {
	Version         int             `json:"version"`
	IntentID        common.IntentID `json:"intent_id"`
	TaskID          common.TaskID   `json:"task_id"`
	Class           Class           `json:"class"`
	NormalizedAction string         `json:"normalized_action"`
	Confidence      float64         `json:"confidence"`
	AmbiguityFlags  []string        `json:"ambiguity_flags"`
	RequiresClarification bool      `json:"requires_clarification"`
	SourceMessageIDs []common.MessageID `json:"source_message_ids"`
	ProposedPhase   phase.Phase     `json:"proposed_phase"`
	CreatedAt       time.Time       `json:"created_at"`
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
