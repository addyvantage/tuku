package phase

import "fmt"

type Phase string

const (
	PhaseIntake           Phase = "INTAKE"
	PhaseInterpreting     Phase = "INTERPRETING"
	PhaseBriefReady       Phase = "BRIEF_READY"
	PhaseExecuting        Phase = "EXECUTING"
	PhaseValidating       Phase = "VALIDATING"
	PhaseAwaitingDecision Phase = "AWAITING_DECISION"
	PhasePaused           Phase = "PAUSED"
	PhaseBlocked          Phase = "BLOCKED"
	PhaseCompleted        Phase = "COMPLETED"
	PhaseFailed           Phase = "FAILED"
)

type TransitionTrigger string

const (
	TriggerUserMessage    TransitionTrigger = "USER_MESSAGE"
	TriggerIntentCompiled TransitionTrigger = "INTENT_COMPILED"
	TriggerBriefGenerated TransitionTrigger = "BRIEF_GENERATED"
	TriggerWorkerStarted  TransitionTrigger = "WORKER_STARTED"
	TriggerWorkerFinished TransitionTrigger = "WORKER_FINISHED"
	TriggerValidationDone TransitionTrigger = "VALIDATION_DONE"
	TriggerNeedsDecision  TransitionTrigger = "NEEDS_DECISION"
	TriggerPaused         TransitionTrigger = "PAUSED"
	TriggerResumed        TransitionTrigger = "RESUMED"
	TriggerError          TransitionTrigger = "ERROR"
)

type Snapshot struct {
	Current            Phase          `json:"current"`
	EnteredAtUnixMs    int64          `json:"entered_at_unix_ms"`
	Reason             string         `json:"reason"`
	AwaitingDecision   bool           `json:"awaiting_decision"`
	AllowedTransitions map[Phase]bool `json:"allowed_transitions"`
}

func Validate(p Phase) error {
	switch p {
	case PhaseIntake, PhaseInterpreting, PhaseBriefReady, PhaseExecuting, PhaseValidating,
		PhaseAwaitingDecision, PhasePaused, PhaseBlocked, PhaseCompleted, PhaseFailed:
		return nil
	default:
		return fmt.Errorf("unknown phase: %s", p)
	}
}
