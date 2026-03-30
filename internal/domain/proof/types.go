package proof

import (
	"time"

	"tuku/internal/domain/common"
)

type EventType string

const (
	EventUserMessageReceived                EventType = "USER_MESSAGE_RECEIVED"
	EventIntentCompiled                     EventType = "INTENT_COMPILED"
	EventBriefCreated                       EventType = "BRIEF_CREATED"
	EventWorkerRunStarted                   EventType = "WORKER_RUN_STARTED"
	EventWorkerRunCompleted                 EventType = "WORKER_RUN_COMPLETED"
	EventWorkerRunFailed                    EventType = "WORKER_RUN_FAILED"
	EventWorkerOutputCaptured               EventType = "WORKER_OUTPUT_CAPTURED"
	EventWorkerCommandExecuted              EventType = "WORKER_COMMAND_EXECUTED"
	EventFileChangeDetected                 EventType = "FILE_CHANGE_DETECTED"
	EventValidationResult                   EventType = "VALIDATION_RESULT"
	EventPolicyDecisionRequested            EventType = "POLICY_DECISION_REQUESTED"
	EventPolicyDecisionResolved             EventType = "POLICY_DECISION_RESOLVED"
	EventCheckpointCreated                  EventType = "CHECKPOINT_CREATED"
	EventContinueAssessed                   EventType = "CONTINUE_ASSESSED"
	EventHandoffCreated                     EventType = "HANDOFF_CREATED"
	EventHandoffAccepted                    EventType = "HANDOFF_ACCEPTED"
	EventHandoffBlocked                     EventType = "HANDOFF_BLOCKED"
	EventHandoffLaunchRequested             EventType = "HANDOFF_LAUNCH_REQUESTED"
	EventHandoffLaunchCompleted             EventType = "HANDOFF_LAUNCH_COMPLETED"
	EventHandoffLaunchFailed                EventType = "HANDOFF_LAUNCH_FAILED"
	EventHandoffLaunchBlocked               EventType = "HANDOFF_LAUNCH_BLOCKED"
	EventHandoffAcknowledgmentCaptured      EventType = "HANDOFF_ACKNOWLEDGMENT_CAPTURED"
	EventHandoffAcknowledgmentUnavailable   EventType = "HANDOFF_ACKNOWLEDGMENT_UNAVAILABLE"
	EventHandoffFollowThroughRecorded       EventType = "HANDOFF_FOLLOW_THROUGH_RECORDED"
	EventHandoffResolutionRecorded          EventType = "HANDOFF_RESOLUTION_RECORDED"
	EventOperatorStepExecutionRecorded      EventType = "OPERATOR_STEP_EXECUTION_RECORDED"
	EventRecoveryActionRecorded             EventType = "RECOVERY_ACTION_RECORDED"
	EventInterruptedRunReviewed             EventType = "INTERRUPTED_RUN_REVIEWED"
	EventInterruptedRunResumeExecuted       EventType = "INTERRUPTED_RUN_RESUME_EXECUTED"
	EventRecoveryContinueExecuted           EventType = "RECOVERY_CONTINUE_EXECUTED"
	EventBriefRegenerated                   EventType = "BRIEF_REGENERATED"
	EventRunInterrupted                     EventType = "RUN_INTERRUPTED"
	EventRunResumed                         EventType = "RUN_RESUMED"
	EventShellHostStarted                   EventType = "SHELL_HOST_STARTED"
	EventShellHostExited                    EventType = "SHELL_HOST_EXITED"
	EventShellFallbackActivated             EventType = "SHELL_FALLBACK_ACTIVATED"
	EventShellReattachRequested             EventType = "SHELL_REATTACH_REQUESTED"
	EventShellReattachFailed                EventType = "SHELL_REATTACH_FAILED"
	EventTranscriptEvidenceReviewed         EventType = "TRANSCRIPT_EVIDENCE_REVIEWED"
	EventTranscriptReviewGapAcknowledged    EventType = "TRANSCRIPT_REVIEW_GAP_ACKNOWLEDGED"
	EventBranchHandoffTransitionRecorded    EventType = "BRANCH_HANDOFF_TRANSITION_RECORDED"
	EventContinuityIncidentTriaged          EventType = "CONTINUITY_INCIDENT_TRIAGED"
	EventContinuityIncidentFollowUpRecorded EventType = "CONTINUITY_INCIDENT_FOLLOW_UP_RECORDED"
	EventCanonicalResponseEmitted           EventType = "CANONICAL_RESPONSE_EMITTED"
	EventTaskPhaseTransitioned              EventType = "TASK_PHASE_TRANSITIONED"
)

type ActorType string

const (
	ActorUser   ActorType = "USER"
	ActorSystem ActorType = "SYSTEM"
	ActorWorker ActorType = "WORKER"
)

type Event struct {
	EventID             common.EventID        `json:"event_id"`
	TaskID              common.TaskID         `json:"task_id"`
	RunID               *common.RunID         `json:"run_id,omitempty"`
	CheckpointID        *common.CheckpointID  `json:"checkpoint_id,omitempty"`
	SequenceNo          int64                 `json:"sequence_no"`
	Timestamp           time.Time             `json:"timestamp"`
	Type                EventType             `json:"type"`
	ActorType           ActorType             `json:"actor_type"`
	ActorID             string                `json:"actor_id"`
	PayloadJSON         string                `json:"payload_json"`
	CausalParentEventID *common.EventID       `json:"causal_parent_event_id,omitempty"`
	CapsuleVersion      common.CapsuleVersion `json:"capsule_version"`
}

type Ledger interface {
	Append(event Event) error
	ListByTask(taskID common.TaskID, limit int) ([]Event, error)
}

// ProofCard is evidence plus a decision interface artifact.
type ProofCard struct {
	Version           int                 `json:"version"`
	CardID            string              `json:"card_id"`
	TaskID            common.TaskID       `json:"task_id"`
	CheckpointID      common.CheckpointID `json:"checkpoint_id"`
	EventRangeStart   common.EventID      `json:"event_range_start"`
	EventRangeEnd     common.EventID      `json:"event_range_end"`
	WhatChanged       []string            `json:"what_changed"`
	WhatVerified      []string            `json:"what_verified"`
	WhatFailed        []string            `json:"what_failed"`
	Unknowns          []string            `json:"unknowns"`
	ConfidenceNotes   []string            `json:"confidence_notes"`
	RiskNotes         []string            `json:"risk_notes"`
	DecisionRequired  bool                `json:"decision_required"`
	DecisionPrompt    string              `json:"decision_prompt"`
	DecisionOptions   []string            `json:"decision_options"`
	RecommendedOption string              `json:"recommended_option"`
}
