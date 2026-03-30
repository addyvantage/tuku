1. Concise diagnosis
- Claude handoff truth stopped at launch + initial acknowledgment. Tuku could say a Claude handoff was accepted, launched, and initially acknowledged, but it had no durable way to record later downstream proof-of-life, manual continuation confirmation, persistent unknown state, or an operator-marked stalled handoff.
- That left post-launch continuity too shallow. Status / inspect / shell could describe launched-but-unproven handoffs, but they could not represent stronger or worse downstream evidence without overclaiming.

2. Implementation summary
- Added a small durable handoff follow-through record in the handoff domain and SQLite storage.
- Added typed follow-through kinds:
  - `PROOF_OF_LIFE_OBSERVED`
  - `CONTINUATION_CONFIRMED`
  - `CONTINUATION_UNKNOWN`
  - `STALLED_REVIEW_REQUIRED`
- Added orchestrator mutation `RecordHandoffFollowThrough`, plus CLI / IPC / daemon path `tuku handoff-followthrough ...` -> `task.handoff.followthrough.record`.
- Extended `HandoffContinuity` to consume latest durable follow-through evidence and distinguish launched Claude handoffs more precisely.
- Extended continuity validation so persisted follow-through evidence must match the latest completed launch attempt and latest handoff.
- Extended recovery projection so stalled follow-through becomes a first-class review posture (`HANDOFF_FOLLOW_THROUGH_REVIEW_REQUIRED`), while proof-of-life / confirmed / unknown remain launched-handoff monitoring states and never become fresh-run ready.
- Extended status / inspect / shell snapshot / shell viewmodel to surface latest follow-through truth.
- Added proof event `HANDOFF_FOLLOW_THROUGH_RECORDED` and proof/shell summaries.

3. Files changed
- /Users/kagaya/Desktop/Tuku/internal/domain/handoff/types.go
- /Users/kagaya/Desktop/Tuku/internal/domain/proof/types.go
- /Users/kagaya/Desktop/Tuku/internal/storage/contracts.go
- /Users/kagaya/Desktop/Tuku/internal/storage/sqlite/handoff_repo.go
- /Users/kagaya/Desktop/Tuku/internal/orchestrator/orchestrator.go
- /Users/kagaya/Desktop/Tuku/internal/orchestrator/handoff_continuity.go
- /Users/kagaya/Desktop/Tuku/internal/orchestrator/handoff_followthrough.go
- /Users/kagaya/Desktop/Tuku/internal/orchestrator/continuity.go
- /Users/kagaya/Desktop/Tuku/internal/orchestrator/recovery.go
- /Users/kagaya/Desktop/Tuku/internal/orchestrator/service.go
- /Users/kagaya/Desktop/Tuku/internal/orchestrator/shell.go
- /Users/kagaya/Desktop/Tuku/internal/orchestrator/handoff_test.go
- /Users/kagaya/Desktop/Tuku/internal/orchestrator/shell_test.go
- /Users/kagaya/Desktop/Tuku/internal/ipc/types.go
- /Users/kagaya/Desktop/Tuku/internal/ipc/payloads.go
- /Users/kagaya/Desktop/Tuku/internal/runtime/daemon/service.go
- /Users/kagaya/Desktop/Tuku/internal/runtime/daemon/service_test.go
- /Users/kagaya/Desktop/Tuku/internal/app/bootstrap.go
- /Users/kagaya/Desktop/Tuku/internal/app/bootstrap_test.go
- /Users/kagaya/Desktop/Tuku/internal/tui/shell/types.go
- /Users/kagaya/Desktop/Tuku/internal/tui/shell/adapter.go
- /Users/kagaya/Desktop/Tuku/internal/tui/shell/viewmodel.go
- /Users/kagaya/Desktop/Tuku/internal/tui/shell/viewmodel_test.go

4. Behavior before vs after
- Before: launched Claude handoffs stopped at `launch completed + acknowledgment captured/unavailable` with downstream continuation still unproven.
- After: launched Claude handoffs can additionally record bounded downstream follow-through evidence without pretending Tuku has transcript/session visibility.
- Before: there was no durable way to say “later proof of life observed”, “operator confirmed continuation”, “still unknown”, or “stalled and needs review”.
- After: those are durable typed facts, surfaced through continuity, recovery, status, inspect, and shell.
- Before: stalled launched Claude handoffs stayed in the generic monitor-launched-handoff posture.
- After: stalled follow-through becomes an explicit review-required recovery posture.
- Before: operator surfaces could not distinguish stronger post-launch evidence from initial acknowledgment.
- After: operator surfaces carry follow-through IDs, kinds, summaries, continuity state, and recovery semantics while still not claiming downstream completion.

5. Tests added/updated
- `internal/orchestrator/handoff_test.go`
  - added proof-of-life follow-through lifecycle test
  - added stalled follow-through review-required test
  - added invalid-posture rejection + replay reuse test
  - proves durable storage, proof emission, status/inspect alignment, and non-readiness for local next run
- `internal/orchestrator/shell_test.go`
  - added shell snapshot proof-of-life continuity test
  - added shell snapshot stalled follow-through review test
- `internal/runtime/daemon/service_test.go`
  - extended shell snapshot mapping test for follow-through fields
  - extended status/inspect mapping test for latest follow-through and stalled continuity
  - added route test for `task.handoff.followthrough.record`
- `internal/app/bootstrap_test.go`
  - added CLI parser test for handoff follow-through kind
  - added CLI route test for `handoff-followthrough`
  - added invalid kind rejection test
  - added daemon-error propagation test
- `internal/tui/shell/viewmodel_test.go`
  - added stalled follow-through operator surfacing regression

6. Commands run
```bash
gofmt -w internal/domain/handoff/types.go internal/domain/proof/types.go internal/storage/contracts.go internal/storage/sqlite/handoff_repo.go internal/orchestrator/handoff_continuity.go internal/orchestrator/handoff_followthrough.go internal/orchestrator/continuity.go internal/orchestrator/recovery.go internal/orchestrator/service.go internal/orchestrator/shell.go internal/orchestrator/handoff_test.go internal/orchestrator/shell_test.go internal/orchestrator/orchestrator.go internal/ipc/types.go internal/ipc/payloads.go internal/runtime/daemon/service.go internal/runtime/daemon/service_test.go internal/app/bootstrap.go internal/app/bootstrap_test.go internal/tui/shell/types.go internal/tui/shell/adapter.go internal/tui/shell/viewmodel.go internal/tui/shell/viewmodel_test.go

go test ./internal/orchestrator ./internal/runtime/daemon ./internal/tui/shell ./internal/app -count=1
```

7. Remaining limitations
- This is still not Claude runtime/session attachment, transcript replay, or downstream completion proof.
- `PROOF_OF_LIFE_OBSERVED` and `CONTINUATION_CONFIRMED` intentionally prove only bounded post-launch downstream follow-through, not task completion.
- Handoff follow-through remains operator-recorded evidence. Tuku still does not observe full downstream Claude execution directly.
- Handoff continuity and follow-through are still projected on read from durable records rather than persisted as one consolidated state object.

8. Full diff
- The workspace is not a git checkout, so a true unified diff against a repo base revision is not available here.
- Full current contents of every changed file are included below.


## internal/domain/handoff/types.go

```go
package handoff

import (
	"time"

	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/run"
)

type Status string

const (
	StatusCreated  Status = "CREATED"
	StatusAccepted Status = "ACCEPTED"
	StatusBlocked  Status = "BLOCKED"
)

type Mode string

const (
	ModeResume   Mode = "resume"
	ModeReview   Mode = "review"
	ModeTakeover Mode = "takeover"
)

type AcknowledgmentStatus string

const (
	AcknowledgmentCaptured    AcknowledgmentStatus = "CAPTURED"
	AcknowledgmentUnavailable AcknowledgmentStatus = "UNAVAILABLE"
)

type FollowThroughKind string

const (
	FollowThroughProofOfLifeObserved   FollowThroughKind = "PROOF_OF_LIFE_OBSERVED"
	FollowThroughContinuationConfirmed FollowThroughKind = "CONTINUATION_CONFIRMED"
	FollowThroughContinuationUnknown   FollowThroughKind = "CONTINUATION_UNKNOWN"
	FollowThroughStalledReviewRequired FollowThroughKind = "STALLED_REVIEW_REQUIRED"
)

type LaunchStatus string

const (
	LaunchStatusRequested LaunchStatus = "REQUESTED"
	LaunchStatusCompleted LaunchStatus = "COMPLETED"
	LaunchStatusFailed    LaunchStatus = "FAILED"
)

// Packet is a durable cross-worker continuation artifact.
type Packet struct {
	Version int `json:"version"`

	HandoffID string        `json:"handoff_id"`
	TaskID    common.TaskID `json:"task_id"`
	Status    Status        `json:"status"`

	SourceWorker run.WorkerKind `json:"source_worker"`
	TargetWorker run.WorkerKind `json:"target_worker"`
	HandoffMode  Mode           `json:"handoff_mode"`
	Reason       string         `json:"reason"`

	CurrentPhase   phase.Phase           `json:"current_phase"`
	CheckpointID   common.CheckpointID   `json:"checkpoint_id"`
	BriefID        common.BriefID        `json:"brief_id"`
	IntentID       common.IntentID       `json:"intent_id"`
	CapsuleVersion common.CapsuleVersion `json:"capsule_version"`
	RepoAnchor     checkpoint.RepoAnchor `json:"repo_anchor"`

	IsResumable      bool   `json:"is_resumable"`
	ResumeDescriptor string `json:"resume_descriptor"`

	LatestRunID     common.RunID `json:"latest_run_id,omitempty"`
	LatestRunStatus run.Status   `json:"latest_run_status,omitempty"`

	Goal             string   `json:"goal"`
	BriefObjective   string   `json:"brief_objective"`
	NormalizedAction string   `json:"normalized_action"`
	Constraints      []string `json:"constraints"`
	DoneCriteria     []string `json:"done_criteria"`
	TouchedFiles     []string `json:"touched_files"`
	Blockers         []string `json:"blockers"`
	NextAction       string   `json:"next_action"`
	Unknowns         []string `json:"unknowns"`
	HandoffNotes     []string `json:"handoff_notes"`

	CreatedAt  time.Time      `json:"created_at"`
	AcceptedAt *time.Time     `json:"accepted_at,omitempty"`
	AcceptedBy run.WorkerKind `json:"accepted_by,omitempty"`
}

// LaunchPayload is the deterministic cross-worker payload materialized from persisted continuity state.
type LaunchPayload struct {
	Version int `json:"version"`

	TaskID       common.TaskID  `json:"task_id"`
	HandoffID    string         `json:"handoff_id"`
	SourceWorker run.WorkerKind `json:"source_worker"`
	TargetWorker run.WorkerKind `json:"target_worker"`
	HandoffMode  Mode           `json:"handoff_mode"`

	CurrentPhase     phase.Phase           `json:"current_phase"`
	CheckpointID     common.CheckpointID   `json:"checkpoint_id"`
	BriefID          common.BriefID        `json:"brief_id"`
	IntentID         common.IntentID       `json:"intent_id"`
	CapsuleVersion   common.CapsuleVersion `json:"capsule_version"`
	RepoAnchor       checkpoint.RepoAnchor `json:"repo_anchor"`
	IsResumable      bool                  `json:"is_resumable"`
	ResumeDescriptor string                `json:"resume_descriptor"`

	LatestRunID      common.RunID `json:"latest_run_id,omitempty"`
	LatestRunStatus  run.Status   `json:"latest_run_status,omitempty"`
	LatestRunSummary string       `json:"latest_run_summary,omitempty"`

	Goal             string   `json:"goal"`
	BriefObjective   string   `json:"brief_objective"`
	NormalizedAction string   `json:"normalized_action"`
	Constraints      []string `json:"constraints"`
	DoneCriteria     []string `json:"done_criteria"`
	TouchedFiles     []string `json:"touched_files"`
	Blockers         []string `json:"blockers"`
	NextAction       string   `json:"next_action"`
	Unknowns         []string `json:"unknowns"`
	HandoffNotes     []string `json:"handoff_notes"`

	GeneratedAt time.Time `json:"generated_at"`
}

// Acknowledgment is a bounded durable artifact proving initial post-launch worker acknowledgement state.
type Acknowledgment struct {
	Version      int                  `json:"version"`
	AckID        string               `json:"ack_id"`
	HandoffID    string               `json:"handoff_id"`
	LaunchID     string               `json:"launch_id"`
	TaskID       common.TaskID        `json:"task_id"`
	TargetWorker run.WorkerKind       `json:"target_worker"`
	Status       AcknowledgmentStatus `json:"status"`
	Summary      string               `json:"summary"`
	Unknowns     []string             `json:"unknowns"`
	CreatedAt    time.Time            `json:"created_at"`
}

// FollowThrough is a bounded durable artifact for post-launch downstream handoff evidence.
type FollowThrough struct {
	Version int `json:"version"`

	RecordID        string            `json:"record_id"`
	HandoffID       string            `json:"handoff_id"`
	LaunchAttemptID string            `json:"launch_attempt_id,omitempty"`
	LaunchID        string            `json:"launch_id,omitempty"`
	TaskID          common.TaskID     `json:"task_id"`
	TargetWorker    run.WorkerKind    `json:"target_worker"`
	Kind            FollowThroughKind `json:"kind"`
	Summary         string            `json:"summary"`
	Notes           []string          `json:"notes,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
}

// Launch is the durable control-plane record for a handoff launch attempt.
type Launch struct {
	Version int `json:"version"`

	AttemptID    string         `json:"attempt_id"`
	HandoffID    string         `json:"handoff_id"`
	TaskID       common.TaskID  `json:"task_id"`
	TargetWorker run.WorkerKind `json:"target_worker"`
	Status       LaunchStatus   `json:"status"`

	LaunchID          string    `json:"launch_id,omitempty"`
	PayloadHash       string    `json:"payload_hash,omitempty"`
	RequestedAt       time.Time `json:"requested_at"`
	StartedAt         time.Time `json:"started_at,omitempty"`
	EndedAt           time.Time `json:"ended_at,omitempty"`
	Command           string    `json:"command,omitempty"`
	Args              []string  `json:"args,omitempty"`
	ExitCode          *int      `json:"exit_code,omitempty"`
	Summary           string    `json:"summary,omitempty"`
	ErrorMessage      string    `json:"error_message,omitempty"`
	OutputArtifactRef string    `json:"output_artifact_ref,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type Repository interface {
	Create(packet Packet) error
	Get(handoffID string) (Packet, error)
	LatestByTask(taskID common.TaskID) (Packet, error)
	UpdateStatus(taskID common.TaskID, handoffID string, status Status, acceptedBy run.WorkerKind, notes []string, at time.Time) error
	CreateLaunch(launch Launch) error
	GetLaunch(attemptID string) (Launch, error)
	LatestLaunchByHandoff(handoffID string) (Launch, error)
	UpdateLaunch(launch Launch) error
	SaveAcknowledgment(ack Acknowledgment) error
	LatestAcknowledgment(handoffID string) (Acknowledgment, error)
	SaveFollowThrough(record FollowThrough) error
	LatestFollowThrough(handoffID string) (FollowThrough, error)
}

```


## internal/domain/proof/types.go

```go
package proof

import (
	"time"

	"tuku/internal/domain/common"
)

type EventType string

const (
	EventUserMessageReceived              EventType = "USER_MESSAGE_RECEIVED"
	EventIntentCompiled                   EventType = "INTENT_COMPILED"
	EventBriefCreated                     EventType = "BRIEF_CREATED"
	EventWorkerRunStarted                 EventType = "WORKER_RUN_STARTED"
	EventWorkerRunCompleted               EventType = "WORKER_RUN_COMPLETED"
	EventWorkerRunFailed                  EventType = "WORKER_RUN_FAILED"
	EventWorkerOutputCaptured             EventType = "WORKER_OUTPUT_CAPTURED"
	EventWorkerCommandExecuted            EventType = "WORKER_COMMAND_EXECUTED"
	EventFileChangeDetected               EventType = "FILE_CHANGE_DETECTED"
	EventValidationResult                 EventType = "VALIDATION_RESULT"
	EventPolicyDecisionRequested          EventType = "POLICY_DECISION_REQUESTED"
	EventPolicyDecisionResolved           EventType = "POLICY_DECISION_RESOLVED"
	EventCheckpointCreated                EventType = "CHECKPOINT_CREATED"
	EventContinueAssessed                 EventType = "CONTINUE_ASSESSED"
	EventHandoffCreated                   EventType = "HANDOFF_CREATED"
	EventHandoffAccepted                  EventType = "HANDOFF_ACCEPTED"
	EventHandoffBlocked                   EventType = "HANDOFF_BLOCKED"
	EventHandoffLaunchRequested           EventType = "HANDOFF_LAUNCH_REQUESTED"
	EventHandoffLaunchCompleted           EventType = "HANDOFF_LAUNCH_COMPLETED"
	EventHandoffLaunchFailed              EventType = "HANDOFF_LAUNCH_FAILED"
	EventHandoffLaunchBlocked             EventType = "HANDOFF_LAUNCH_BLOCKED"
	EventHandoffAcknowledgmentCaptured    EventType = "HANDOFF_ACKNOWLEDGMENT_CAPTURED"
	EventHandoffAcknowledgmentUnavailable EventType = "HANDOFF_ACKNOWLEDGMENT_UNAVAILABLE"
	EventHandoffFollowThroughRecorded     EventType = "HANDOFF_FOLLOW_THROUGH_RECORDED"
	EventRecoveryActionRecorded           EventType = "RECOVERY_ACTION_RECORDED"
	EventInterruptedRunReviewed           EventType = "INTERRUPTED_RUN_REVIEWED"
	EventInterruptedRunResumeExecuted     EventType = "INTERRUPTED_RUN_RESUME_EXECUTED"
	EventRecoveryContinueExecuted         EventType = "RECOVERY_CONTINUE_EXECUTED"
	EventBriefRegenerated                 EventType = "BRIEF_REGENERATED"
	EventRunInterrupted                   EventType = "RUN_INTERRUPTED"
	EventRunResumed                       EventType = "RUN_RESUMED"
	EventShellHostStarted                 EventType = "SHELL_HOST_STARTED"
	EventShellHostExited                  EventType = "SHELL_HOST_EXITED"
	EventShellFallbackActivated           EventType = "SHELL_FALLBACK_ACTIVATED"
	EventCanonicalResponseEmitted         EventType = "CANONICAL_RESPONSE_EMITTED"
	EventTaskPhaseTransitioned            EventType = "TASK_PHASE_TRANSITIONED"
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

```


## internal/storage/contracts.go

```go
package storage

import (
	"time"

	"tuku/internal/domain/brief"
	"tuku/internal/domain/capsule"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	contextdomain "tuku/internal/domain/context"
	"tuku/internal/domain/conversation"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/intent"
	"tuku/internal/domain/policy"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/recoveryaction"
	"tuku/internal/domain/run"
)

type CapsuleStore interface {
	Create(c capsule.WorkCapsule) error
	Get(taskID common.TaskID) (capsule.WorkCapsule, error)
	LatestByRepoRoot(repoRoot string) (capsule.WorkCapsule, error)
	Update(c capsule.WorkCapsule) error
}

type ConversationStore interface {
	Append(message conversation.Message) error
	ListRecent(conversationID common.ConversationID, limit int) ([]conversation.Message, error)
}

type IntentStore interface {
	Save(state intent.State) error
	LatestByTask(taskID common.TaskID) (intent.State, error)
}

type BriefStore interface {
	Save(b brief.ExecutionBrief) error
	Get(briefID common.BriefID) (brief.ExecutionBrief, error)
	LatestByTask(taskID common.TaskID) (brief.ExecutionBrief, error)
}

type ProofStore interface {
	Append(event proof.Event) error
	ListByTask(taskID common.TaskID, limit int) ([]proof.Event, error)
}

type RunStore interface {
	Create(run run.ExecutionRun) error
	Get(runID common.RunID) (run.ExecutionRun, error)
	LatestByTask(taskID common.TaskID) (run.ExecutionRun, error)
	LatestRunningByTask(taskID common.TaskID) (run.ExecutionRun, error)
	Update(run run.ExecutionRun) error
}

type CheckpointStore interface {
	Create(c checkpoint.Checkpoint) error
	Get(checkpointID common.CheckpointID) (checkpoint.Checkpoint, error)
	LatestByTask(taskID common.TaskID) (checkpoint.Checkpoint, error)
}

type HandoffStore interface {
	Create(packet handoff.Packet) error
	Get(handoffID string) (handoff.Packet, error)
	LatestByTask(taskID common.TaskID) (handoff.Packet, error)
	UpdateStatus(taskID common.TaskID, handoffID string, status handoff.Status, acceptedBy run.WorkerKind, notes []string, at time.Time) error
	CreateLaunch(launch handoff.Launch) error
	GetLaunch(attemptID string) (handoff.Launch, error)
	LatestLaunchByHandoff(handoffID string) (handoff.Launch, error)
	UpdateLaunch(launch handoff.Launch) error
	SaveAcknowledgment(ack handoff.Acknowledgment) error
	LatestAcknowledgment(handoffID string) (handoff.Acknowledgment, error)
	SaveFollowThrough(record handoff.FollowThrough) error
	LatestFollowThrough(handoffID string) (handoff.FollowThrough, error)
}

type RecoveryActionStore interface {
	Create(record recoveryaction.Record) error
	LatestByTask(taskID common.TaskID) (recoveryaction.Record, error)
	ListByTask(taskID common.TaskID, limit int) ([]recoveryaction.Record, error)
}

type ContextPackStore interface {
	Save(pack contextdomain.Pack) error
	Get(id common.ContextPackID) (contextdomain.Pack, error)
}

type PolicyDecisionStore interface {
	Save(decision policy.Decision) error
	Get(decisionID common.DecisionID) (policy.Decision, error)
}

type Store interface {
	Capsules() CapsuleStore
	Conversations() ConversationStore
	Intents() IntentStore
	Briefs() BriefStore
	Proofs() ProofStore
	Runs() RunStore
	Checkpoints() CheckpointStore
	Handoffs() HandoffStore
	RecoveryActions() RecoveryActionStore
	ContextPacks() ContextPackStore
	PolicyDecisions() PolicyDecisionStore
	WithTx(fn func(Store) error) error
}

```


## internal/storage/sqlite/handoff_repo.go

```go
package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/run"
)

type handoffRepo struct{ q queryable }

func ensureHandoffTables(q queryable) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS handoff_packets (
	handoff_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	status TEXT NOT NULL,
	source_worker TEXT NOT NULL,
	target_worker TEXT NOT NULL,
	checkpoint_id TEXT NOT NULL,
	brief_id TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	accepted_at TEXT,
	accepted_by TEXT,
	packet_json TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_handoff_packets_task_created
	ON handoff_packets(task_id, created_at DESC);

CREATE TABLE IF NOT EXISTS handoff_acknowledgments (
	ack_id TEXT PRIMARY KEY,
	handoff_id TEXT NOT NULL,
	launch_id TEXT NOT NULL,
	task_id TEXT NOT NULL,
	target_worker TEXT NOT NULL,
	status TEXT NOT NULL,
	summary TEXT NOT NULL,
	unknowns_json TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_handoff_acknowledgments_handoff_created
	ON handoff_acknowledgments(handoff_id, created_at DESC, ack_id DESC);

CREATE TABLE IF NOT EXISTS handoff_follow_throughs (
	record_id TEXT PRIMARY KEY,
	handoff_id TEXT NOT NULL,
	launch_attempt_id TEXT NOT NULL,
	launch_id TEXT NOT NULL,
	task_id TEXT NOT NULL,
	target_worker TEXT NOT NULL,
	kind TEXT NOT NULL,
	summary TEXT NOT NULL,
	notes_json TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_handoff_follow_throughs_handoff_created
	ON handoff_follow_throughs(handoff_id, created_at DESC, record_id DESC);

CREATE TABLE IF NOT EXISTS handoff_launches (
	attempt_id TEXT PRIMARY KEY,
	handoff_id TEXT NOT NULL,
	task_id TEXT NOT NULL,
	target_worker TEXT NOT NULL,
	status TEXT NOT NULL,
	launch_id TEXT NOT NULL,
	payload_hash TEXT NOT NULL,
	requested_at TEXT NOT NULL,
	started_at TEXT,
	ended_at TEXT,
	command TEXT NOT NULL,
	args_json TEXT NOT NULL,
	exit_code INTEGER,
	summary TEXT NOT NULL,
	error_message TEXT NOT NULL,
	output_artifact_ref TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_handoff_launches_handoff_requested
	ON handoff_launches(handoff_id, requested_at DESC, attempt_id DESC);
`
	if _, err := q.Exec(ddl); err != nil {
		return fmt.Errorf("ensure handoff tables: %w", err)
	}
	return nil
}

func (r *handoffRepo) Create(packet handoff.Packet) error {
	if err := ensureHandoffTables(r.q); err != nil {
		return err
	}
	packetJSON, err := json.Marshal(packet)
	if err != nil {
		return err
	}
	now := packet.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	_, err = r.q.Exec(`
INSERT INTO handoff_packets(
	handoff_id, task_id, status, source_worker, target_worker, checkpoint_id, brief_id,
	created_at, updated_at, accepted_at, accepted_by, packet_json
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
`,
		packet.HandoffID,
		string(packet.TaskID),
		string(packet.Status),
		string(packet.SourceWorker),
		string(packet.TargetWorker),
		string(packet.CheckpointID),
		string(packet.BriefID),
		now.Format(sqliteTimestampLayout),
		now.Format(sqliteTimestampLayout),
		nilIfTime(packet.AcceptedAt),
		nilIfString(string(packet.AcceptedBy)),
		string(packetJSON),
	)
	if err != nil {
		return fmt.Errorf("insert handoff packet: %w", err)
	}
	return nil
}

func (r *handoffRepo) Get(handoffID string) (handoff.Packet, error) {
	if err := ensureHandoffTables(r.q); err != nil {
		return handoff.Packet{}, err
	}
	row := r.q.QueryRow(`
SELECT packet_json
FROM handoff_packets
WHERE handoff_id = ?
`, handoffID)
	return scanHandoffPacket(row)
}

func (r *handoffRepo) LatestByTask(taskID common.TaskID) (handoff.Packet, error) {
	if err := ensureHandoffTables(r.q); err != nil {
		return handoff.Packet{}, err
	}
	row := r.q.QueryRow(`
SELECT packet_json
FROM handoff_packets
WHERE task_id = ?
ORDER BY created_at DESC, handoff_id DESC
LIMIT 1
`, string(taskID))
	return scanHandoffPacket(row)
}

func (r *handoffRepo) UpdateStatus(taskID common.TaskID, handoffID string, status handoff.Status, acceptedBy run.WorkerKind, notes []string, at time.Time) error {
	if err := ensureHandoffTables(r.q); err != nil {
		return err
	}
	packet, err := r.Get(handoffID)
	if err != nil {
		return err
	}
	if packet.TaskID != taskID {
		return fmt.Errorf("handoff %s task mismatch: got %s expected %s", handoffID, packet.TaskID, taskID)
	}
	packet.Status = status
	if status == handoff.StatusAccepted {
		packet.AcceptedAt = &at
		packet.AcceptedBy = acceptedBy
	}
	if len(notes) > 0 {
		packet.HandoffNotes = append(packet.HandoffNotes, notes...)
	}
	packetJSON, err := json.Marshal(packet)
	if err != nil {
		return err
	}

	res, err := r.q.Exec(`
UPDATE handoff_packets SET
	status=?, updated_at=?, accepted_at=?, accepted_by=?, packet_json=?
WHERE handoff_id = ? AND task_id = ?
`,
		string(status),
		at.Format(sqliteTimestampLayout),
		nilIfTime(packet.AcceptedAt),
		nilIfString(string(acceptedBy)),
		string(packetJSON),
		handoffID,
		string(taskID),
	)
	if err != nil {
		return fmt.Errorf("update handoff status: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *handoffRepo) SaveAcknowledgment(ack handoff.Acknowledgment) error {
	if err := ensureHandoffTables(r.q); err != nil {
		return err
	}
	unknownsJSON, err := marshalStringSlice(ack.Unknowns)
	if err != nil {
		return err
	}
	_, err = r.q.Exec(`
INSERT INTO handoff_acknowledgments(
	ack_id, handoff_id, launch_id, task_id, target_worker, status, summary, unknowns_json, created_at
) VALUES(?,?,?,?,?,?,?,?,?)
`,
		ack.AckID,
		ack.HandoffID,
		ack.LaunchID,
		string(ack.TaskID),
		string(ack.TargetWorker),
		string(ack.Status),
		ack.Summary,
		unknownsJSON,
		ack.CreatedAt.Format(sqliteTimestampLayout),
	)
	if err != nil {
		return fmt.Errorf("insert handoff acknowledgment: %w", err)
	}
	return nil
}

func (r *handoffRepo) CreateLaunch(launch handoff.Launch) error {
	if err := ensureHandoffTables(r.q); err != nil {
		return err
	}
	argsJSON, err := marshalStringSlice(launch.Args)
	if err != nil {
		return err
	}
	_, err = r.q.Exec(`
INSERT INTO handoff_launches(
	attempt_id, handoff_id, task_id, target_worker, status, launch_id, payload_hash,
	requested_at, started_at, ended_at, command, args_json, exit_code, summary,
	error_message, output_artifact_ref, created_at, updated_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
`,
		launch.AttemptID,
		launch.HandoffID,
		string(launch.TaskID),
		string(launch.TargetWorker),
		string(launch.Status),
		launch.LaunchID,
		launch.PayloadHash,
		launch.RequestedAt.Format(sqliteTimestampLayout),
		nilIfZeroTime(launch.StartedAt),
		nilIfZeroTime(launch.EndedAt),
		launch.Command,
		argsJSON,
		launch.ExitCode,
		launch.Summary,
		launch.ErrorMessage,
		launch.OutputArtifactRef,
		launch.CreatedAt.Format(sqliteTimestampLayout),
		launch.UpdatedAt.Format(sqliteTimestampLayout),
	)
	if err != nil {
		return fmt.Errorf("insert handoff launch: %w", err)
	}
	return nil
}

func (r *handoffRepo) GetLaunch(attemptID string) (handoff.Launch, error) {
	if err := ensureHandoffTables(r.q); err != nil {
		return handoff.Launch{}, err
	}
	row := r.q.QueryRow(`
SELECT attempt_id, handoff_id, task_id, target_worker, status, launch_id, payload_hash,
	requested_at, started_at, ended_at, command, args_json, exit_code, summary,
	error_message, output_artifact_ref, created_at, updated_at
FROM handoff_launches
WHERE attempt_id = ?
`, attemptID)
	return scanHandoffLaunch(row)
}

func (r *handoffRepo) LatestLaunchByHandoff(handoffID string) (handoff.Launch, error) {
	if err := ensureHandoffTables(r.q); err != nil {
		return handoff.Launch{}, err
	}
	row := r.q.QueryRow(`
SELECT attempt_id, handoff_id, task_id, target_worker, status, launch_id, payload_hash,
	requested_at, started_at, ended_at, command, args_json, exit_code, summary,
	error_message, output_artifact_ref, created_at, updated_at
FROM handoff_launches
WHERE handoff_id = ?
ORDER BY requested_at DESC, attempt_id DESC
LIMIT 1
`, handoffID)
	return scanHandoffLaunch(row)
}

func (r *handoffRepo) UpdateLaunch(launch handoff.Launch) error {
	if err := ensureHandoffTables(r.q); err != nil {
		return err
	}
	argsJSON, err := marshalStringSlice(launch.Args)
	if err != nil {
		return err
	}
	res, err := r.q.Exec(`
UPDATE handoff_launches SET
	status=?, launch_id=?, payload_hash=?, requested_at=?, started_at=?, ended_at=?, command=?,
	args_json=?, exit_code=?, summary=?, error_message=?, output_artifact_ref=?, updated_at=?
WHERE attempt_id = ?
`,
		string(launch.Status),
		launch.LaunchID,
		launch.PayloadHash,
		launch.RequestedAt.Format(sqliteTimestampLayout),
		nilIfZeroTime(launch.StartedAt),
		nilIfZeroTime(launch.EndedAt),
		launch.Command,
		argsJSON,
		launch.ExitCode,
		launch.Summary,
		launch.ErrorMessage,
		launch.OutputArtifactRef,
		launch.UpdatedAt.Format(sqliteTimestampLayout),
		launch.AttemptID,
	)
	if err != nil {
		return fmt.Errorf("update handoff launch: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *handoffRepo) LatestAcknowledgment(handoffID string) (handoff.Acknowledgment, error) {
	if err := ensureHandoffTables(r.q); err != nil {
		return handoff.Acknowledgment{}, err
	}
	row := r.q.QueryRow(`
SELECT ack_id, handoff_id, launch_id, task_id, target_worker, status, summary, unknowns_json, created_at
FROM handoff_acknowledgments
WHERE handoff_id = ?
ORDER BY created_at DESC, ack_id DESC
LIMIT 1
`, handoffID)
	var (
		ackID        string
		persistedHID string
		launchID     string
		taskID       string
		targetWorker string
		status       string
		summary      string
		unknownsJSON string
		createdAtRaw string
	)
	if err := row.Scan(&ackID, &persistedHID, &launchID, &taskID, &targetWorker, &status, &summary, &unknownsJSON, &createdAtRaw); err != nil {
		return handoff.Acknowledgment{}, err
	}
	createdAt, err := time.Parse(sqliteTimestampLayout, createdAtRaw)
	if err != nil {
		return handoff.Acknowledgment{}, err
	}
	unknowns, err := unmarshalStringSlice(unknownsJSON)
	if err != nil {
		return handoff.Acknowledgment{}, err
	}
	return handoff.Acknowledgment{
		Version:      1,
		AckID:        ackID,
		HandoffID:    persistedHID,
		LaunchID:     launchID,
		TaskID:       common.TaskID(taskID),
		TargetWorker: run.WorkerKind(targetWorker),
		Status:       handoff.AcknowledgmentStatus(status),
		Summary:      summary,
		Unknowns:     unknowns,
		CreatedAt:    createdAt,
	}, nil
}

func (r *handoffRepo) SaveFollowThrough(record handoff.FollowThrough) error {
	if err := ensureHandoffTables(r.q); err != nil {
		return err
	}
	notesJSON, err := marshalStringSlice(record.Notes)
	if err != nil {
		return err
	}
	_, err = r.q.Exec(`
INSERT INTO handoff_follow_throughs(
	record_id, handoff_id, launch_attempt_id, launch_id, task_id, target_worker, kind, summary, notes_json, created_at
) VALUES(?,?,?,?,?,?,?,?,?,?)
`,
		record.RecordID,
		record.HandoffID,
		record.LaunchAttemptID,
		record.LaunchID,
		string(record.TaskID),
		string(record.TargetWorker),
		string(record.Kind),
		record.Summary,
		notesJSON,
		record.CreatedAt.Format(sqliteTimestampLayout),
	)
	if err != nil {
		return fmt.Errorf("insert handoff follow-through: %w", err)
	}
	return nil
}

func (r *handoffRepo) LatestFollowThrough(handoffID string) (handoff.FollowThrough, error) {
	if err := ensureHandoffTables(r.q); err != nil {
		return handoff.FollowThrough{}, err
	}
	row := r.q.QueryRow(`
SELECT record_id, handoff_id, launch_attempt_id, launch_id, task_id, target_worker, kind, summary, notes_json, created_at
FROM handoff_follow_throughs
WHERE handoff_id = ?
ORDER BY created_at DESC, record_id DESC
LIMIT 1
`, handoffID)
	var (
		recordID        string
		persistedHID    string
		launchAttemptID string
		launchID        string
		taskID          string
		targetWorker    string
		kind            string
		summary         string
		notesJSON       string
		createdAtRaw    string
	)
	if err := row.Scan(&recordID, &persistedHID, &launchAttemptID, &launchID, &taskID, &targetWorker, &kind, &summary, &notesJSON, &createdAtRaw); err != nil {
		return handoff.FollowThrough{}, err
	}
	createdAt, err := time.Parse(sqliteTimestampLayout, createdAtRaw)
	if err != nil {
		return handoff.FollowThrough{}, err
	}
	notes, err := unmarshalStringSlice(notesJSON)
	if err != nil {
		return handoff.FollowThrough{}, err
	}
	return handoff.FollowThrough{
		Version:         1,
		RecordID:        recordID,
		HandoffID:       persistedHID,
		LaunchAttemptID: launchAttemptID,
		LaunchID:        launchID,
		TaskID:          common.TaskID(taskID),
		TargetWorker:    run.WorkerKind(targetWorker),
		Kind:            handoff.FollowThroughKind(kind),
		Summary:         summary,
		Notes:           notes,
		CreatedAt:       createdAt,
	}, nil
}

func scanHandoffPacket(row *sql.Row) (handoff.Packet, error) {
	var packetJSON string
	if err := row.Scan(&packetJSON); err != nil {
		return handoff.Packet{}, err
	}
	var packet handoff.Packet
	if err := json.Unmarshal([]byte(packetJSON), &packet); err != nil {
		return handoff.Packet{}, err
	}
	return packet, nil
}

func scanHandoffLaunch(row *sql.Row) (handoff.Launch, error) {
	var (
		attemptID         string
		handoffID         string
		taskID            string
		targetWorker      string
		status            string
		launchID          string
		payloadHash       string
		requestedAtRaw    string
		startedAtRaw      sql.NullString
		endedAtRaw        sql.NullString
		command           string
		argsJSON          string
		exitCode          sql.NullInt64
		summary           string
		errorMessage      string
		outputArtifactRef string
		createdAtRaw      string
		updatedAtRaw      string
	)
	if err := row.Scan(
		&attemptID,
		&handoffID,
		&taskID,
		&targetWorker,
		&status,
		&launchID,
		&payloadHash,
		&requestedAtRaw,
		&startedAtRaw,
		&endedAtRaw,
		&command,
		&argsJSON,
		&exitCode,
		&summary,
		&errorMessage,
		&outputArtifactRef,
		&createdAtRaw,
		&updatedAtRaw,
	); err != nil {
		return handoff.Launch{}, err
	}
	requestedAt, err := time.Parse(sqliteTimestampLayout, requestedAtRaw)
	if err != nil {
		return handoff.Launch{}, err
	}
	createdAt, err := time.Parse(sqliteTimestampLayout, createdAtRaw)
	if err != nil {
		return handoff.Launch{}, err
	}
	updatedAt, err := time.Parse(sqliteTimestampLayout, updatedAtRaw)
	if err != nil {
		return handoff.Launch{}, err
	}
	startedAt, err := parseNullableTime(startedAtRaw)
	if err != nil {
		return handoff.Launch{}, err
	}
	endedAt, err := parseNullableTime(endedAtRaw)
	if err != nil {
		return handoff.Launch{}, err
	}
	args, err := unmarshalStringSlice(argsJSON)
	if err != nil {
		return handoff.Launch{}, err
	}
	var exitCodePtr *int
	if exitCode.Valid {
		value := int(exitCode.Int64)
		exitCodePtr = &value
	}
	return handoff.Launch{
		Version:           1,
		AttemptID:         attemptID,
		HandoffID:         handoffID,
		TaskID:            common.TaskID(taskID),
		TargetWorker:      run.WorkerKind(targetWorker),
		Status:            handoff.LaunchStatus(status),
		LaunchID:          launchID,
		PayloadHash:       payloadHash,
		RequestedAt:       requestedAt,
		StartedAt:         startedAt,
		EndedAt:           endedAt,
		Command:           command,
		Args:              args,
		ExitCode:          exitCodePtr,
		Summary:           summary,
		ErrorMessage:      errorMessage,
		OutputArtifactRef: outputArtifactRef,
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
	}, nil
}

```


## internal/orchestrator/orchestrator.go

```go
package orchestrator

import "context"

type Service interface {
	StartTask(ctx context.Context, goal string, repoRoot string) (StartTaskResult, error)
	ResolveShellTaskForRepo(ctx context.Context, repoRoot string, defaultGoal string) (ResolveShellTaskResult, error)
	MessageTask(ctx context.Context, taskID string, message string) (MessageTaskResult, error)
	RunTask(ctx context.Context, req RunTaskRequest) (RunTaskResult, error)
	ContinueTask(ctx context.Context, taskID string) (ContinueTaskResult, error)
	CreateCheckpoint(ctx context.Context, taskID string) (CreateCheckpointResult, error)
	CreateHandoff(ctx context.Context, req CreateHandoffRequest) (CreateHandoffResult, error)
	AcceptHandoff(ctx context.Context, req AcceptHandoffRequest) (AcceptHandoffResult, error)
	LaunchHandoff(ctx context.Context, req LaunchHandoffRequest) (LaunchHandoffResult, error)
	RecordHandoffFollowThrough(ctx context.Context, req RecordHandoffFollowThroughRequest) (RecordHandoffFollowThroughResult, error)
	RecordRecoveryAction(ctx context.Context, req RecordRecoveryActionRequest) (RecordRecoveryActionResult, error)
	ExecuteRebrief(ctx context.Context, req ExecuteRebriefRequest) (ExecuteRebriefResult, error)
	ExecuteInterruptedResume(ctx context.Context, req ExecuteInterruptedResumeRequest) (ExecuteInterruptedResumeResult, error)
	ExecuteContinueRecovery(ctx context.Context, req ExecuteContinueRecoveryRequest) (ExecuteContinueRecoveryResult, error)
	StatusTask(ctx context.Context, taskID string) (StatusTaskResult, error)
	InspectTask(ctx context.Context, taskID string) (InspectTaskResult, error)
	ShellSnapshotTask(ctx context.Context, taskID string) (ShellSnapshotResult, error)
	RecordShellLifecycle(ctx context.Context, req RecordShellLifecycleRequest) (RecordShellLifecycleResult, error)
	ReportShellSession(ctx context.Context, req ReportShellSessionRequest) (ReportShellSessionResult, error)
	ListShellSessions(ctx context.Context, taskID string) (ListShellSessionsResult, error)
}

```


## internal/orchestrator/handoff_continuity.go

```go
package orchestrator

import (
	"fmt"

	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	rundomain "tuku/internal/domain/run"
)

type HandoffContinuityState string

const (
	HandoffContinuityStateNotApplicable            HandoffContinuityState = "NOT_APPLICABLE"
	HandoffContinuityStateAcceptedNotLaunched      HandoffContinuityState = "ACCEPTED_NOT_LAUNCHED"
	HandoffContinuityStateLaunchPendingOutcome     HandoffContinuityState = "LAUNCH_PENDING_OUTCOME"
	HandoffContinuityStateLaunchFailedRetryable    HandoffContinuityState = "LAUNCH_FAILED_RETRYABLE"
	HandoffContinuityStateLaunchCompletedAckSeen   HandoffContinuityState = "LAUNCH_COMPLETED_ACK_CAPTURED"
	HandoffContinuityStateLaunchCompletedAckEmpty  HandoffContinuityState = "LAUNCH_COMPLETED_ACK_UNAVAILABLE"
	HandoffContinuityStateLaunchCompletedAckLost   HandoffContinuityState = "LAUNCH_COMPLETED_ACK_MISSING"
	HandoffContinuityStateFollowThroughProofOfLife HandoffContinuityState = "FOLLOW_THROUGH_PROOF_OF_LIFE"
	HandoffContinuityStateFollowThroughConfirmed   HandoffContinuityState = "FOLLOW_THROUGH_CONFIRMED"
	HandoffContinuityStateFollowThroughUnknown     HandoffContinuityState = "FOLLOW_THROUGH_UNKNOWN"
	HandoffContinuityStateFollowThroughStalled     HandoffContinuityState = "FOLLOW_THROUGH_STALLED"
)

type HandoffContinuity struct {
	TaskID                       common.TaskID                `json:"task_id"`
	HandoffID                    string                       `json:"handoff_id,omitempty"`
	TargetWorker                 rundomain.WorkerKind         `json:"target_worker,omitempty"`
	State                        HandoffContinuityState       `json:"state"`
	LaunchAttemptID              string                       `json:"launch_attempt_id,omitempty"`
	LaunchID                     string                       `json:"launch_id,omitempty"`
	LaunchStatus                 handoff.LaunchStatus         `json:"launch_status,omitempty"`
	AcknowledgmentID             string                       `json:"acknowledgment_id,omitempty"`
	AcknowledgmentStatus         handoff.AcknowledgmentStatus `json:"acknowledgment_status,omitempty"`
	AcknowledgmentSummary        string                       `json:"acknowledgment_summary,omitempty"`
	FollowThroughID              string                       `json:"follow_through_id,omitempty"`
	FollowThroughKind            handoff.FollowThroughKind    `json:"follow_through_kind,omitempty"`
	FollowThroughSummary         string                       `json:"follow_through_summary,omitempty"`
	DownstreamContinuationProven bool                         `json:"downstream_continuation_proven"`
	Reason                       string                       `json:"reason,omitempty"`
}

func assessHandoffContinuity(taskID common.TaskID, packet *handoff.Packet, launch *handoff.Launch, ack *handoff.Acknowledgment, followThrough *handoff.FollowThrough) HandoffContinuity {
	out := HandoffContinuity{
		TaskID:                       taskID,
		State:                        HandoffContinuityStateNotApplicable,
		DownstreamContinuationProven: false,
	}
	if packet == nil {
		out.Reason = "no Claude handoff continuity is active"
		return out
	}

	out.HandoffID = packet.HandoffID
	out.TargetWorker = packet.TargetWorker
	if packet.TargetWorker != rundomain.WorkerKindClaude {
		out.Reason = fmt.Sprintf("latest handoff target %s is not Claude", packet.TargetWorker)
		return out
	}
	if packet.Status != handoff.StatusAccepted {
		out.Reason = fmt.Sprintf("latest Claude handoff %s is in status %s and is not yet launch-active", packet.HandoffID, packet.Status)
		return out
	}
	if !packet.IsResumable {
		out.Reason = fmt.Sprintf("accepted Claude handoff %s is not launchable because its checkpoint is not resumable", packet.HandoffID)
		return out
	}

	control := assessLaunchControl(taskID, packet, launch)
	out.LaunchAttemptID = control.AttemptID
	out.LaunchID = control.LaunchID
	if launch != nil {
		out.LaunchStatus = launch.Status
	}
	if ack != nil {
		out.AcknowledgmentID = ack.AckID
		out.AcknowledgmentStatus = ack.Status
		out.AcknowledgmentSummary = ack.Summary
	}
	if followThrough != nil {
		out.FollowThroughID = followThrough.RecordID
		out.FollowThroughKind = followThrough.Kind
		out.FollowThroughSummary = followThrough.Summary
	}

	switch control.State {
	case LaunchControlStateNotRequested:
		out.State = HandoffContinuityStateAcceptedNotLaunched
		out.Reason = fmt.Sprintf("accepted Claude handoff %s is ready to launch, but no durable launch attempt exists yet", packet.HandoffID)
	case LaunchControlStateRequestedOutcomeUnknown:
		out.State = HandoffContinuityStateLaunchPendingOutcome
		out.Reason = fmt.Sprintf("Claude handoff launch attempt %s is durably recorded as requested, but completion and acknowledgment are still unproven", control.AttemptID)
	case LaunchControlStateFailed:
		out.State = HandoffContinuityStateLaunchFailedRetryable
		out.Reason = fmt.Sprintf("Claude handoff launch failed durably for attempt %s. Retry is allowed, but downstream continuation is still unproven", control.AttemptID)
	case LaunchControlStateCompleted:
		switch {
		case ack == nil:
			out.State = HandoffContinuityStateLaunchCompletedAckLost
			out.Reason = fmt.Sprintf("Claude handoff launch %s completed durably, but no persisted acknowledgment is available. Downstream continuation remains unproven and continuity repair is required", nonEmpty(control.LaunchID, control.AttemptID))
		default:
			if followThrough != nil {
				switch followThrough.Kind {
				case handoff.FollowThroughProofOfLifeObserved:
					out.State = HandoffContinuityStateFollowThroughProofOfLife
					out.DownstreamContinuationProven = true
					out.Reason = fmt.Sprintf("Claude handoff launch %s has downstream proof-of-life evidence: %s. This proves some downstream follow-through occurred, but not downstream task completion", nonEmpty(control.LaunchID, control.AttemptID), followThrough.Summary)
					return out
				case handoff.FollowThroughContinuationConfirmed:
					out.State = HandoffContinuityStateFollowThroughConfirmed
					out.DownstreamContinuationProven = true
					out.Reason = fmt.Sprintf("Claude handoff launch %s has operator-confirmed downstream continuation: %s. This does not prove downstream task completion or full transcript visibility", nonEmpty(control.LaunchID, control.AttemptID), followThrough.Summary)
					return out
				case handoff.FollowThroughContinuationUnknown:
					out.State = HandoffContinuityStateFollowThroughUnknown
					out.Reason = fmt.Sprintf("Claude handoff launch %s still has unknown downstream follow-through: %s", nonEmpty(control.LaunchID, control.AttemptID), followThrough.Summary)
					return out
				case handoff.FollowThroughStalledReviewRequired:
					out.State = HandoffContinuityStateFollowThroughStalled
					out.Reason = fmt.Sprintf("Claude handoff launch %s appears stalled and needs review: %s", nonEmpty(control.LaunchID, control.AttemptID), followThrough.Summary)
					return out
				}
			}
			if ack.Status == handoff.AcknowledgmentCaptured {
				out.State = HandoffContinuityStateLaunchCompletedAckSeen
				out.Reason = fmt.Sprintf("Claude handoff launch %s completed and an initial acknowledgment was captured. This proves launcher invocation and initial worker acknowledgment only; downstream continuation remains unproven", nonEmpty(control.LaunchID, control.AttemptID))
			} else {
				out.State = HandoffContinuityStateLaunchCompletedAckEmpty
				out.Reason = fmt.Sprintf("Claude handoff launch %s completed, but no usable initial acknowledgment was captured. Downstream continuation remains unproven", nonEmpty(control.LaunchID, control.AttemptID))
			}
		}
	default:
		out.Reason = control.Reason
	}
	return out
}

```


## internal/orchestrator/handoff_followthrough.go

```go
package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/proof"
	rundomain "tuku/internal/domain/run"
)

type RecordHandoffFollowThroughRequest struct {
	TaskID  string
	Kind    handoff.FollowThroughKind
	Summary string
	Notes   []string
}

type RecordHandoffFollowThroughResult struct {
	TaskID                common.TaskID
	Record                handoff.FollowThrough
	HandoffContinuity     HandoffContinuity
	RecoveryClass         RecoveryClass
	RecommendedAction     RecoveryAction
	ReadyForNextRun       bool
	ReadyForHandoffLaunch bool
	RecoveryReason        string
	CanonicalResponse     string
}

func (c *Coordinator) RecordHandoffFollowThrough(ctx context.Context, req RecordHandoffFollowThroughRequest) (RecordHandoffFollowThroughResult, error) {
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return RecordHandoffFollowThroughResult{}, fmt.Errorf("task id is required")
	}
	if req.Kind == "" {
		return RecordHandoffFollowThroughResult{}, fmt.Errorf("follow-through kind is required")
	}

	assessment, err := c.assessContinue(ctx, taskID)
	if err != nil {
		return RecordHandoffFollowThroughResult{}, err
	}
	continuity := assessHandoffContinuity(taskID, assessment.LatestHandoff, assessment.LatestLaunch, assessment.LatestAck, assessment.LatestFollowThrough)
	if err := validateHandoffFollowThroughPosture(continuity, req.Kind); err != nil {
		return RecordHandoffFollowThroughResult{}, err
	}
	if replayableHandoffFollowThrough(req, assessment.LatestFollowThrough, continuity) {
		recovery := c.recoveryFromContinueAssessment(assessment)
		return RecordHandoffFollowThroughResult{
			TaskID:                taskID,
			Record:                *assessment.LatestFollowThrough,
			HandoffContinuity:     continuity,
			RecoveryClass:         recovery.RecoveryClass,
			RecommendedAction:     recovery.RecommendedAction,
			ReadyForNextRun:       recovery.ReadyForNextRun,
			ReadyForHandoffLaunch: recovery.ReadyForHandoffLaunch,
			RecoveryReason:        recovery.Reason,
			CanonicalResponse:     buildHandoffFollowThroughCanonical(continuity, *assessment.LatestFollowThrough),
		}, nil
	}

	var result RecordHandoffFollowThroughResult
	err = c.withTx(func(txc *Coordinator) error {
		current, err := txc.assessContinue(ctx, taskID)
		if err != nil {
			return err
		}
		continuity := assessHandoffContinuity(taskID, current.LatestHandoff, current.LatestLaunch, current.LatestAck, current.LatestFollowThrough)
		if err := validateHandoffFollowThroughPosture(continuity, req.Kind); err != nil {
			return err
		}

		caps, err := txc.store.Capsules().Get(taskID)
		if err != nil {
			return err
		}

		record := handoff.FollowThrough{
			Version:         1,
			RecordID:        txc.idGenerator("hft"),
			HandoffID:       current.LatestHandoff.HandoffID,
			LaunchAttemptID: current.LatestLaunch.AttemptID,
			LaunchID:        current.LatestLaunch.LaunchID,
			TaskID:          taskID,
			TargetWorker:    rundomain.WorkerKindClaude,
			Kind:            req.Kind,
			Summary:         normalizeHandoffFollowThroughSummary(req.Kind, req.Summary),
			Notes:           append([]string{}, req.Notes...),
			CreatedAt:       txc.clock(),
		}
		if err := txc.store.Handoffs().SaveFollowThrough(record); err != nil {
			return err
		}

		payload := map[string]any{
			"record_id":                    record.RecordID,
			"handoff_id":                   record.HandoffID,
			"launch_attempt_id":            record.LaunchAttemptID,
			"launch_id":                    record.LaunchID,
			"target_worker":                record.TargetWorker,
			"kind":                         record.Kind,
			"summary":                      record.Summary,
			"notes":                        append([]string{}, record.Notes...),
			"downstream_completion_proven": false,
		}
		runID := runIDPointer(common.RunID(""))
		if current.LatestRun != nil {
			runID = runIDPointer(current.LatestRun.RunID)
		}
		if err := txc.appendProof(caps, proof.EventHandoffFollowThroughRecorded, proof.ActorSystem, "tuku-daemon", payload, runID); err != nil {
			return err
		}

		current.LatestFollowThrough = &record
		updatedContinuity := assessHandoffContinuity(taskID, current.LatestHandoff, current.LatestLaunch, current.LatestAck, &record)
		currentRecovery := txc.recoveryFromContinueAssessment(current)
		canonical := buildHandoffFollowThroughCanonical(updatedContinuity, record)
		if err := txc.emitCanonicalConversation(caps, canonical, payload, runID); err != nil {
			return err
		}

		result = RecordHandoffFollowThroughResult{
			TaskID:                taskID,
			Record:                record,
			HandoffContinuity:     updatedContinuity,
			RecoveryClass:         currentRecovery.RecoveryClass,
			RecommendedAction:     currentRecovery.RecommendedAction,
			ReadyForNextRun:       currentRecovery.ReadyForNextRun,
			ReadyForHandoffLaunch: currentRecovery.ReadyForHandoffLaunch,
			RecoveryReason:        currentRecovery.Reason,
			CanonicalResponse:     canonical,
		}
		return nil
	})
	if err != nil {
		return RecordHandoffFollowThroughResult{}, err
	}
	return result, nil
}

func validateHandoffFollowThroughPosture(continuity HandoffContinuity, kind handoff.FollowThroughKind) error {
	if continuity.TargetWorker != rundomain.WorkerKindClaude {
		return fmt.Errorf("handoff follow-through can only be recorded for Claude handoffs")
	}
	switch continuity.State {
	case HandoffContinuityStateLaunchCompletedAckSeen,
		HandoffContinuityStateLaunchCompletedAckEmpty,
		HandoffContinuityStateFollowThroughProofOfLife,
		HandoffContinuityStateFollowThroughConfirmed,
		HandoffContinuityStateFollowThroughUnknown,
		HandoffContinuityStateFollowThroughStalled:
	default:
		return fmt.Errorf("handoff follow-through kind %s can only be recorded while handoff continuity state is a launched Claude follow-through posture, got %s", kind, continuity.State)
	}
	return nil
}

func replayableHandoffFollowThrough(req RecordHandoffFollowThroughRequest, latest *handoff.FollowThrough, continuity HandoffContinuity) bool {
	if latest == nil {
		return false
	}
	if err := validateHandoffFollowThroughPosture(continuity, req.Kind); err != nil {
		return false
	}
	if latest.Kind != req.Kind {
		return false
	}
	if latest.HandoffID != continuity.HandoffID || latest.LaunchAttemptID != continuity.LaunchAttemptID {
		return false
	}
	summary := strings.TrimSpace(req.Summary)
	return summary == "" || summary == strings.TrimSpace(latest.Summary)
}

func normalizeHandoffFollowThroughSummary(kind handoff.FollowThroughKind, requested string) string {
	if summary := strings.TrimSpace(requested); summary != "" {
		return summary
	}
	switch kind {
	case handoff.FollowThroughProofOfLifeObserved:
		return "post-launch downstream proof of life observed"
	case handoff.FollowThroughContinuationConfirmed:
		return "operator confirmed downstream continuation occurred"
	case handoff.FollowThroughContinuationUnknown:
		return "downstream follow-through remains unknown"
	case handoff.FollowThroughStalledReviewRequired:
		return "downstream follow-through appears stalled and needs review"
	default:
		return "handoff follow-through recorded"
	}
}

func buildHandoffFollowThroughCanonical(continuity HandoffContinuity, record handoff.FollowThrough) string {
	switch record.Kind {
	case handoff.FollowThroughProofOfLifeObserved:
		return fmt.Sprintf("I recorded downstream proof-of-life evidence for Claude handoff %s: %s. This is stronger than initial launch acknowledgment, but it still does not prove downstream task completion.", continuity.HandoffID, record.Summary)
	case handoff.FollowThroughContinuationConfirmed:
		return fmt.Sprintf("I recorded operator confirmation that downstream Claude continuation occurred for handoff %s: %s. This does not prove downstream task completion or full transcript visibility.", continuity.HandoffID, record.Summary)
	case handoff.FollowThroughContinuationUnknown:
		return fmt.Sprintf("I recorded that downstream Claude follow-through remains unknown for handoff %s: %s. Tuku still does not have proof of downstream task completion.", continuity.HandoffID, record.Summary)
	case handoff.FollowThroughStalledReviewRequired:
		return fmt.Sprintf("I recorded that downstream Claude follow-through appears stalled for handoff %s: %s. Local fresh-run readiness remains blocked while this launched handoff needs review.", continuity.HandoffID, record.Summary)
	default:
		return fmt.Sprintf("I recorded downstream follow-through evidence for Claude handoff %s.", continuity.HandoffID)
	}
}

```


## internal/orchestrator/continuity.go

```go
package orchestrator

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"tuku/internal/domain/capsule"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/recoveryaction"
	rundomain "tuku/internal/domain/run"
)

type continuityViolationCode string

const (
	continuityViolationCapsuleBriefMissing             continuityViolationCode = "CAPSULE_BRIEF_MISSING"
	continuityViolationCapsuleIntentMissing            continuityViolationCode = "CAPSULE_INTENT_MISSING"
	continuityViolationCheckpointTaskMismatch          continuityViolationCode = "CHECKPOINT_TASK_MISMATCH"
	continuityViolationCheckpointRunMissing            continuityViolationCode = "CHECKPOINT_RUN_MISSING"
	continuityViolationCheckpointBriefMissing          continuityViolationCode = "CHECKPOINT_BRIEF_MISSING"
	continuityViolationCheckpointResumablePhase        continuityViolationCode = "CHECKPOINT_RESUMABLE_PHASE_INVALID"
	continuityViolationRunTaskMismatch                 continuityViolationCode = "RUN_TASK_MISMATCH"
	continuityViolationRunBriefMissing                 continuityViolationCode = "RUN_BRIEF_MISSING"
	continuityViolationRunPhaseMismatch                continuityViolationCode = "RUN_PHASE_MISMATCH"
	continuityViolationRunningRunCheckpointMismatch    continuityViolationCode = "RUNNING_RUN_CHECKPOINT_MISMATCH"
	continuityViolationLatestHandoffTaskMismatch       continuityViolationCode = "LATEST_HANDOFF_TASK_MISMATCH"
	continuityViolationLatestHandoffBriefMissing       continuityViolationCode = "LATEST_HANDOFF_BRIEF_MISSING"
	continuityViolationLatestHandoffCheckpointMissing  continuityViolationCode = "LATEST_HANDOFF_CHECKPOINT_MISSING"
	continuityViolationLatestHandoffCheckpointMismatch continuityViolationCode = "LATEST_HANDOFF_CHECKPOINT_MISMATCH"
	continuityViolationLatestHandoffAcceptedInvalid    continuityViolationCode = "LATEST_HANDOFF_ACCEPTED_INVALID"
	continuityViolationLatestLaunchInvalid             continuityViolationCode = "LATEST_LAUNCH_INVALID"
	continuityViolationLatestAckInvalid                continuityViolationCode = "LATEST_ACK_INVALID"
	continuityViolationLatestFollowThroughInvalid      continuityViolationCode = "LATEST_FOLLOW_THROUGH_INVALID"
	continuityViolationLatestRecoveryActionInvalid     continuityViolationCode = "LATEST_RECOVERY_ACTION_INVALID"
)

type continuityViolation struct {
	Code    continuityViolationCode
	Message string
}

type continuitySnapshot struct {
	Capsule              capsule.WorkCapsule
	LatestRun            *rundomain.ExecutionRun
	LatestCheckpoint     *checkpoint.Checkpoint
	LatestHandoff        *handoff.Packet
	LatestLaunch         *handoff.Launch
	LatestAcknowledgment *handoff.Acknowledgment
	LatestFollowThrough  *handoff.FollowThrough
	LatestRecoveryAction *recoveryaction.Record
}

func (c *Coordinator) loadContinuitySnapshot(taskID common.TaskID) (continuitySnapshot, error) {
	caps, err := c.store.Capsules().Get(taskID)
	if err != nil {
		return continuitySnapshot{}, err
	}

	snapshot := continuitySnapshot{Capsule: caps}
	if latestRun, err := c.store.Runs().LatestByTask(taskID); err == nil {
		runCopy := latestRun
		snapshot.LatestRun = &runCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return continuitySnapshot{}, err
	}

	if latestCheckpoint, err := c.store.Checkpoints().LatestByTask(taskID); err == nil {
		cpCopy := latestCheckpoint
		snapshot.LatestCheckpoint = &cpCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return continuitySnapshot{}, err
	}

	if latestHandoff, err := c.store.Handoffs().LatestByTask(taskID); err == nil {
		packetCopy := latestHandoff
		snapshot.LatestHandoff = &packetCopy
		if latestLaunch, err := c.store.Handoffs().LatestLaunchByHandoff(latestHandoff.HandoffID); err == nil {
			launchCopy := latestLaunch
			snapshot.LatestLaunch = &launchCopy
		} else if !errors.Is(err, sql.ErrNoRows) {
			return continuitySnapshot{}, err
		}
		if latestAck, err := c.store.Handoffs().LatestAcknowledgment(latestHandoff.HandoffID); err == nil {
			ackCopy := latestAck
			snapshot.LatestAcknowledgment = &ackCopy
		} else if !errors.Is(err, sql.ErrNoRows) {
			return continuitySnapshot{}, err
		}
		if latestFollowThrough, err := c.store.Handoffs().LatestFollowThrough(latestHandoff.HandoffID); err == nil {
			recordCopy := latestFollowThrough
			snapshot.LatestFollowThrough = &recordCopy
		} else if !errors.Is(err, sql.ErrNoRows) {
			return continuitySnapshot{}, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return continuitySnapshot{}, err
	}

	if latestAction, err := c.store.RecoveryActions().LatestByTask(taskID); err == nil {
		actionCopy := latestAction
		snapshot.LatestRecoveryAction = &actionCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return continuitySnapshot{}, err
	}

	return snapshot, nil
}

func (c *Coordinator) validateContinuitySnapshot(snapshot continuitySnapshot) ([]continuityViolation, error) {
	violations := make([]continuityViolation, 0, 8)
	caps := snapshot.Capsule

	if caps.CurrentBriefID == "" {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationCapsuleBriefMissing,
			Message: "capsule has no current brief reference",
		})
	} else if _, err := c.store.Briefs().Get(caps.CurrentBriefID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationCapsuleBriefMissing,
				Message: fmt.Sprintf("capsule references missing brief %s", caps.CurrentBriefID),
			})
		} else {
			return nil, err
		}
	}

	if caps.CurrentIntentID != "" {
		latestIntent, err := c.store.Intents().LatestByTask(caps.TaskID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationCapsuleIntentMissing,
					Message: fmt.Sprintf("capsule references missing intent %s", caps.CurrentIntentID),
				})
			} else {
				return nil, err
			}
		} else if latestIntent.IntentID != caps.CurrentIntentID {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationCapsuleIntentMissing,
				Message: fmt.Sprintf("capsule current intent %s does not match latest intent %s", caps.CurrentIntentID, latestIntent.IntentID),
			})
		}
	}

	if snapshot.LatestCheckpoint != nil {
		cpViolations, err := c.validateCheckpointContinuity(snapshot)
		if err != nil {
			return nil, err
		}
		violations = append(violations, cpViolations...)
	}

	if snapshot.LatestRun != nil {
		runViolations, err := c.validateRunContinuity(snapshot)
		if err != nil {
			return nil, err
		}
		violations = append(violations, runViolations...)
	} else if caps.CurrentPhase == phase.PhaseExecuting {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationRunPhaseMismatch,
			Message: "capsule phase is EXECUTING but no run exists",
		})
	}

	if snapshot.LatestHandoff != nil {
		handoffViolations, err := c.validateHandoffContinuity(snapshot)
		if err != nil {
			return nil, err
		}
		violations = append(violations, handoffViolations...)
	}
	if snapshot.LatestRecoveryAction != nil {
		actionViolations, err := c.validateRecoveryActionContinuity(snapshot)
		if err != nil {
			return nil, err
		}
		violations = append(violations, actionViolations...)
	}

	return dedupeContinuityViolations(violations), nil
}

func (c *Coordinator) validateCheckpointContinuity(snapshot continuitySnapshot) ([]continuityViolation, error) {
	cp := snapshot.LatestCheckpoint
	caps := snapshot.Capsule
	violations := make([]continuityViolation, 0, 4)

	if cp.TaskID != caps.TaskID {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationCheckpointTaskMismatch,
			Message: fmt.Sprintf("latest checkpoint task mismatch: checkpoint task=%s capsule task=%s", cp.TaskID, caps.TaskID),
		})
	}
	if cp.BriefID != "" {
		if _, err := c.store.Briefs().Get(cp.BriefID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationCheckpointBriefMissing,
					Message: fmt.Sprintf("latest checkpoint references missing brief %s", cp.BriefID),
				})
			} else {
				return nil, err
			}
		}
	}
	if cp.RunID != "" {
		runRec, err := c.store.Runs().Get(cp.RunID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationCheckpointRunMissing,
					Message: fmt.Sprintf("latest checkpoint references missing run %s", cp.RunID),
				})
			} else {
				return nil, err
			}
		} else if runRec.TaskID != caps.TaskID {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationCheckpointTaskMismatch,
				Message: fmt.Sprintf("latest checkpoint run task mismatch: run task=%s capsule task=%s", runRec.TaskID, caps.TaskID),
			})
		}
	}
	if cp.IsResumable && (cp.Phase == phase.PhaseAwaitingDecision || cp.Phase == phase.PhaseBlocked) {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationCheckpointResumablePhase,
			Message: fmt.Sprintf("latest checkpoint %s is marked resumable in incompatible phase %s", cp.CheckpointID, cp.Phase),
		})
	}

	return violations, nil
}

func (c *Coordinator) validateRunContinuity(snapshot continuitySnapshot) ([]continuityViolation, error) {
	runRec := snapshot.LatestRun
	caps := snapshot.Capsule
	violations := make([]continuityViolation, 0, 4)

	if runRec.TaskID != caps.TaskID {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationRunTaskMismatch,
			Message: fmt.Sprintf("latest run task mismatch: run task=%s capsule task=%s", runRec.TaskID, caps.TaskID),
		})
	}
	if runRec.BriefID == "" {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationRunBriefMissing,
			Message: "latest run has empty brief reference",
		})
	} else if _, err := c.store.Briefs().Get(runRec.BriefID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationRunBriefMissing,
				Message: fmt.Sprintf("latest run references missing brief %s", runRec.BriefID),
			})
		} else {
			return nil, err
		}
	}

	if runRec.Status == rundomain.StatusRunning {
		if caps.CurrentPhase != phase.PhaseExecuting {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationRunPhaseMismatch,
				Message: fmt.Sprintf("latest run %s is RUNNING but capsule phase is %s", runRec.RunID, caps.CurrentPhase),
			})
		}
		if caps.CurrentBriefID != "" && caps.CurrentBriefID != runRec.BriefID {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationRunPhaseMismatch,
				Message: fmt.Sprintf("RUNNING run brief %s does not match capsule brief %s", runRec.BriefID, caps.CurrentBriefID),
			})
		}
		if snapshot.LatestCheckpoint != nil {
			if snapshot.LatestCheckpoint.RunID == "" {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationRunningRunCheckpointMismatch,
					Message: fmt.Sprintf("RUNNING run %s has checkpoint linkage without run_id", runRec.RunID),
				})
			} else if snapshot.LatestCheckpoint.RunID != runRec.RunID {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationRunningRunCheckpointMismatch,
					Message: fmt.Sprintf("RUNNING run %s does not match checkpoint run linkage %s", runRec.RunID, snapshot.LatestCheckpoint.RunID),
				})
			}
		}
	} else if caps.CurrentPhase == phase.PhaseExecuting {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationRunPhaseMismatch,
			Message: fmt.Sprintf("capsule phase EXECUTING is inconsistent with latest run terminal status %s", runRec.Status),
		})
	}

	return violations, nil
}

func (c *Coordinator) validateHandoffContinuity(snapshot continuitySnapshot) ([]continuityViolation, error) {
	packet := snapshot.LatestHandoff
	caps := snapshot.Capsule
	violations := make([]continuityViolation, 0, 8)

	if packet.TaskID != caps.TaskID {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationLatestHandoffTaskMismatch,
			Message: fmt.Sprintf("latest handoff task mismatch: handoff task=%s capsule task=%s", packet.TaskID, caps.TaskID),
		})
	}
	if packet.BriefID == "" {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationLatestHandoffBriefMissing,
			Message: fmt.Sprintf("latest handoff %s has empty brief reference", packet.HandoffID),
		})
	} else if _, err := c.store.Briefs().Get(packet.BriefID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestHandoffBriefMissing,
				Message: fmt.Sprintf("latest handoff references missing brief %s", packet.BriefID),
			})
		} else {
			return nil, err
		}
	}
	if packet.CheckpointID == "" {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationLatestHandoffCheckpointMissing,
			Message: fmt.Sprintf("latest handoff %s has empty checkpoint reference", packet.HandoffID),
		})
	} else {
		cp, err := c.store.Checkpoints().Get(packet.CheckpointID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestHandoffCheckpointMissing,
					Message: fmt.Sprintf("latest handoff references missing checkpoint %s", packet.CheckpointID),
				})
			} else {
				return nil, err
			}
		} else {
			if cp.TaskID != caps.TaskID {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestHandoffTaskMismatch,
					Message: fmt.Sprintf("latest handoff checkpoint task mismatch: checkpoint task=%s capsule task=%s", cp.TaskID, caps.TaskID),
				})
			}
			if packet.IsResumable && !cp.IsResumable {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestHandoffCheckpointMismatch,
					Message: fmt.Sprintf("latest handoff %s claims resumable continuity but checkpoint %s is not resumable", packet.HandoffID, packet.CheckpointID),
				})
			}
			if packet.BriefID != "" && cp.BriefID != "" && packet.BriefID != cp.BriefID {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestHandoffCheckpointMismatch,
					Message: fmt.Sprintf("latest handoff brief %s does not match checkpoint brief %s", packet.BriefID, cp.BriefID),
				})
			}
			if packet.IntentID != "" && cp.IntentID != "" && packet.IntentID != cp.IntentID {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestHandoffCheckpointMismatch,
					Message: fmt.Sprintf("latest handoff intent %s does not match checkpoint intent %s", packet.IntentID, cp.IntentID),
				})
			}
		}
	}

	switch packet.Status {
	case handoff.StatusAccepted:
		if packet.AcceptedBy == "" || packet.AcceptedAt == nil {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestHandoffAcceptedInvalid,
				Message: fmt.Sprintf("latest handoff %s is ACCEPTED without accepted_by and accepted_at", packet.HandoffID),
			})
		}
	case handoff.StatusCreated:
		if packet.AcceptedBy != "" || packet.AcceptedAt != nil {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestHandoffAcceptedInvalid,
				Message: fmt.Sprintf("latest handoff %s is CREATED but carries acceptance fields", packet.HandoffID),
			})
		}
	case handoff.StatusBlocked:
		if packet.AcceptedBy != "" || packet.AcceptedAt != nil {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestHandoffAcceptedInvalid,
				Message: fmt.Sprintf("latest handoff %s is BLOCKED but carries acceptance fields", packet.HandoffID),
			})
		}
	}

	if snapshot.LatestLaunch != nil {
		launch := snapshot.LatestLaunch
		if launch.TaskID != caps.TaskID {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestLaunchInvalid,
				Message: fmt.Sprintf("latest launch task mismatch: launch task=%s capsule task=%s", launch.TaskID, caps.TaskID),
			})
		}
		if launch.HandoffID != packet.HandoffID {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestLaunchInvalid,
				Message: fmt.Sprintf("latest launch handoff mismatch: launch handoff=%s latest handoff=%s", launch.HandoffID, packet.HandoffID),
			})
		}
		if launch.TargetWorker != packet.TargetWorker {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestLaunchInvalid,
				Message: fmt.Sprintf("latest launch target %s does not match latest handoff target %s", launch.TargetWorker, packet.TargetWorker),
			})
		}
		if packet.Status == handoff.StatusBlocked {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestLaunchInvalid,
				Message: fmt.Sprintf("latest handoff %s is BLOCKED but has a persisted launch attempt", packet.HandoffID),
			})
		}
		switch launch.Status {
		case handoff.LaunchStatusRequested:
		case handoff.LaunchStatusCompleted:
			if launch.EndedAt.IsZero() {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestLaunchInvalid,
					Message: fmt.Sprintf("latest launch %s is COMPLETED without ended_at", launch.AttemptID),
				})
			}
		case handoff.LaunchStatusFailed:
			if launch.EndedAt.IsZero() {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestLaunchInvalid,
					Message: fmt.Sprintf("latest launch %s is FAILED without ended_at", launch.AttemptID),
				})
			}
		default:
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestLaunchInvalid,
				Message: fmt.Sprintf("latest launch %s has unsupported status %s", launch.AttemptID, launch.Status),
			})
		}
		if launch.Status == handoff.LaunchStatusCompleted && snapshot.LatestAcknowledgment == nil {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestAckInvalid,
				Message: fmt.Sprintf("latest completed launch %s for handoff %s has no persisted acknowledgment", launch.AttemptID, packet.HandoffID),
			})
		}
	}

	if snapshot.LatestAcknowledgment != nil {
		ack := snapshot.LatestAcknowledgment
		if ack.TaskID != caps.TaskID {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestAckInvalid,
				Message: fmt.Sprintf("latest acknowledgment task mismatch: ack task=%s capsule task=%s", ack.TaskID, caps.TaskID),
			})
		}
		if ack.HandoffID != packet.HandoffID {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestAckInvalid,
				Message: fmt.Sprintf("latest acknowledgment handoff mismatch: ack handoff=%s latest handoff=%s", ack.HandoffID, packet.HandoffID),
			})
		}
		if ack.TargetWorker != packet.TargetWorker {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestAckInvalid,
				Message: fmt.Sprintf("latest acknowledgment target %s does not match latest handoff target %s", ack.TargetWorker, packet.TargetWorker),
			})
		}
		if packet.Status == handoff.StatusBlocked {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestAckInvalid,
				Message: fmt.Sprintf("latest handoff %s is BLOCKED but has a persisted launch acknowledgment", packet.HandoffID),
			})
		}
		if strings.TrimSpace(ack.LaunchID) == "" {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestAckInvalid,
				Message: fmt.Sprintf("latest acknowledgment for handoff %s has empty launch id", packet.HandoffID),
			})
		}
		if snapshot.LatestLaunch != nil {
			launch := snapshot.LatestLaunch
			switch launch.Status {
			case handoff.LaunchStatusCompleted:
				if strings.TrimSpace(launch.LaunchID) != "" && ack.LaunchID != launch.LaunchID {
					violations = append(violations, continuityViolation{
						Code:    continuityViolationLatestAckInvalid,
						Message: fmt.Sprintf("latest acknowledgment launch %s does not match latest completed launch %s", ack.LaunchID, launch.LaunchID),
					})
				}
			case handoff.LaunchStatusFailed:
				if strings.TrimSpace(ack.LaunchID) != "" && (ack.LaunchID == launch.LaunchID || launch.LaunchID == "") {
					violations = append(violations, continuityViolation{
						Code:    continuityViolationLatestAckInvalid,
						Message: fmt.Sprintf("latest failed launch %s should not have acknowledgment state for the same attempt", launch.AttemptID),
					})
				}
			}
		}
	}

	if snapshot.LatestFollowThrough != nil {
		record := snapshot.LatestFollowThrough
		if record.TaskID != caps.TaskID {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestFollowThroughInvalid,
				Message: fmt.Sprintf("latest follow-through task mismatch: record task=%s capsule task=%s", record.TaskID, caps.TaskID),
			})
		}
		if record.HandoffID != packet.HandoffID {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestFollowThroughInvalid,
				Message: fmt.Sprintf("latest follow-through handoff mismatch: record handoff=%s latest handoff=%s", record.HandoffID, packet.HandoffID),
			})
		}
		if record.TargetWorker != packet.TargetWorker {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestFollowThroughInvalid,
				Message: fmt.Sprintf("latest follow-through target %s does not match latest handoff target %s", record.TargetWorker, packet.TargetWorker),
			})
		}
		if snapshot.LatestLaunch == nil {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestFollowThroughInvalid,
				Message: fmt.Sprintf("latest follow-through for handoff %s exists without a persisted launch attempt", packet.HandoffID),
			})
		} else {
			launch := snapshot.LatestLaunch
			if launch.Status != handoff.LaunchStatusCompleted {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestFollowThroughInvalid,
					Message: fmt.Sprintf("latest follow-through for handoff %s exists but latest launch %s is not COMPLETED", packet.HandoffID, launch.AttemptID),
				})
			}
			if strings.TrimSpace(record.LaunchAttemptID) == "" {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestFollowThroughInvalid,
					Message: fmt.Sprintf("latest follow-through for handoff %s has empty launch attempt id", packet.HandoffID),
				})
			} else if record.LaunchAttemptID != launch.AttemptID {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestFollowThroughInvalid,
					Message: fmt.Sprintf("latest follow-through launch attempt %s does not match latest launch %s", record.LaunchAttemptID, launch.AttemptID),
				})
			}
			if strings.TrimSpace(record.LaunchID) != "" && strings.TrimSpace(launch.LaunchID) != "" && record.LaunchID != launch.LaunchID {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestFollowThroughInvalid,
					Message: fmt.Sprintf("latest follow-through launch id %s does not match latest launch id %s", record.LaunchID, launch.LaunchID),
				})
			}
		}
	}

	return violations, nil
}

func dedupeContinuityViolations(values []continuityViolation) []continuityViolation {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]continuityViolation, 0, len(values))
	for _, value := range values {
		key := string(value.Code) + "|" + value.Message
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func firstContinuityViolationMessage(values []continuityViolation) string {
	if len(values) == 0 {
		return ""
	}
	return values[0].Message
}

func stringSlicesEqual(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func repoAnchorsEqual(a checkpoint.RepoAnchor, b checkpoint.RepoAnchor) bool {
	return a.RepoRoot == b.RepoRoot &&
		a.WorktreePath == b.WorktreePath &&
		a.BranchName == b.BranchName &&
		a.HeadSHA == b.HeadSHA &&
		a.DirtyHash == b.DirtyHash &&
		a.UntrackedHash == b.UntrackedHash
}

func buildReplayBlockedLaunchResponse(packet handoff.Packet) LaunchHandoffResult {
	canonical := fmt.Sprintf(
		"Launch for handoff %s was previously requested, but Tuku does not have a durable completion or failure record for that request. The outcome is unknown, so automatic retry is blocked to avoid duplicate worker launch.",
		packet.HandoffID,
	)
	return LaunchHandoffResult{
		TaskID:            packet.TaskID,
		HandoffID:         packet.HandoffID,
		TargetWorker:      packet.TargetWorker,
		LaunchStatus:      HandoffLaunchStatusBlocked,
		CanonicalResponse: canonical,
	}
}

func (c *Coordinator) validateRecoveryActionContinuity(snapshot continuitySnapshot) ([]continuityViolation, error) {
	action := snapshot.LatestRecoveryAction
	violations := make([]continuityViolation, 0, 4)
	if action == nil {
		return violations, nil
	}
	if action.TaskID != snapshot.Capsule.TaskID {
		violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action task mismatch: action task=%s capsule task=%s", action.TaskID, snapshot.Capsule.TaskID)})
	}
	if action.RunID != "" {
		runRec, err := c.store.Runs().Get(action.RunID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action references missing run %s", action.RunID)})
			} else {
				return nil, err
			}
		} else if runRec.TaskID != snapshot.Capsule.TaskID {
			violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action run task mismatch: run task=%s capsule task=%s", runRec.TaskID, snapshot.Capsule.TaskID)})
		}
	}
	if action.CheckpointID != "" {
		cp, err := c.store.Checkpoints().Get(action.CheckpointID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action references missing checkpoint %s", action.CheckpointID)})
			} else {
				return nil, err
			}
		} else if cp.TaskID != snapshot.Capsule.TaskID {
			violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action checkpoint task mismatch: checkpoint task=%s capsule task=%s", cp.TaskID, snapshot.Capsule.TaskID)})
		}
	}
	if action.HandoffID != "" {
		packet, err := c.store.Handoffs().Get(action.HandoffID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action references missing handoff %s", action.HandoffID)})
			} else {
				return nil, err
			}
		} else if packet.TaskID != snapshot.Capsule.TaskID {
			violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action handoff task mismatch: handoff task=%s capsule task=%s", packet.TaskID, snapshot.Capsule.TaskID)})
		}
	}
	if action.LaunchAttemptID != "" {
		launch, err := c.store.Handoffs().GetLaunch(action.LaunchAttemptID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action references missing launch attempt %s", action.LaunchAttemptID)})
			} else {
				return nil, err
			}
		} else if launch.TaskID != snapshot.Capsule.TaskID {
			violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action launch task mismatch: launch task=%s capsule task=%s", launch.TaskID, snapshot.Capsule.TaskID)})
		}
	}
	return violations, nil
}

```


## internal/orchestrator/recovery.go

```go
package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/recoveryaction"
	rundomain "tuku/internal/domain/run"
)

type RecoveryClass string

const (
	RecoveryClassReadyNextRun                       RecoveryClass = "READY_NEXT_RUN"
	RecoveryClassInterruptedRunRecoverable          RecoveryClass = "INTERRUPTED_RUN_RECOVERABLE"
	RecoveryClassAcceptedHandoffLaunchReady         RecoveryClass = "ACCEPTED_HANDOFF_LAUNCH_READY"
	RecoveryClassHandoffLaunchPendingOutcome        RecoveryClass = "HANDOFF_LAUNCH_PENDING_OUTCOME"
	RecoveryClassHandoffLaunchCompleted             RecoveryClass = "HANDOFF_LAUNCH_COMPLETED"
	RecoveryClassHandoffFollowThroughReviewRequired RecoveryClass = "HANDOFF_FOLLOW_THROUGH_REVIEW_REQUIRED"
	RecoveryClassFailedRunReviewRequired            RecoveryClass = "FAILED_RUN_REVIEW_REQUIRED"
	RecoveryClassValidationReviewRequired           RecoveryClass = "VALIDATION_REVIEW_REQUIRED"
	RecoveryClassStaleRunReconciliationRequired     RecoveryClass = "STALE_RUN_RECONCILIATION_REQUIRED"
	RecoveryClassDecisionRequired                   RecoveryClass = "DECISION_REQUIRED"
	RecoveryClassContinueExecutionRequired          RecoveryClass = "CONTINUE_EXECUTION_REQUIRED"
	RecoveryClassBlockedDrift                       RecoveryClass = "BLOCKED_DRIFT"
	RecoveryClassRepairRequired                     RecoveryClass = "REPAIR_REQUIRED"
	RecoveryClassRebriefRequired                    RecoveryClass = "REBRIEF_REQUIRED"
	RecoveryClassCompletedNoAction                  RecoveryClass = "COMPLETED_NO_ACTION"
)

type RecoveryAction string

const (
	RecoveryActionNone                       RecoveryAction = "NONE"
	RecoveryActionStartNextRun               RecoveryAction = "START_NEXT_RUN"
	RecoveryActionResumeInterrupted          RecoveryAction = "RESUME_INTERRUPTED_RUN"
	RecoveryActionLaunchAcceptedHandoff      RecoveryAction = "LAUNCH_ACCEPTED_HANDOFF"
	RecoveryActionWaitForLaunchOutcome       RecoveryAction = "WAIT_FOR_LAUNCH_OUTCOME"
	RecoveryActionMonitorLaunchedHandoff     RecoveryAction = "MONITOR_LAUNCHED_HANDOFF"
	RecoveryActionReviewHandoffFollowThrough RecoveryAction = "REVIEW_HANDOFF_FOLLOW_THROUGH"
	RecoveryActionInspectFailedRun           RecoveryAction = "INSPECT_FAILED_RUN"
	RecoveryActionReviewValidation           RecoveryAction = "REVIEW_VALIDATION_STATE"
	RecoveryActionReconcileStaleRun          RecoveryAction = "RECONCILE_STALE_RUN"
	RecoveryActionMakeResumeDecision         RecoveryAction = "MAKE_RESUME_DECISION"
	RecoveryActionExecuteContinueRecovery    RecoveryAction = "EXECUTE_CONTINUE_RECOVERY"
	RecoveryActionRepairContinuity           RecoveryAction = "REPAIR_CONTINUITY"
	RecoveryActionRegenerateBrief            RecoveryAction = "REGENERATE_BRIEF"
)

type RecoveryIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type RecoveryAssessment struct {
	TaskID                 common.TaskID          `json:"task_id"`
	ContinuityOutcome      ContinueOutcome        `json:"continuity_outcome"`
	RecoveryClass          RecoveryClass          `json:"recovery_class"`
	RecommendedAction      RecoveryAction         `json:"recommended_action"`
	ReadyForNextRun        bool                   `json:"ready_for_next_run"`
	ReadyForHandoffLaunch  bool                   `json:"ready_for_handoff_launch"`
	RequiresDecision       bool                   `json:"requires_decision,omitempty"`
	RequiresRepair         bool                   `json:"requires_repair,omitempty"`
	RequiresReview         bool                   `json:"requires_review,omitempty"`
	RequiresReconciliation bool                   `json:"requires_reconciliation,omitempty"`
	DriftClass             checkpoint.DriftClass  `json:"drift_class,omitempty"`
	Reason                 string                 `json:"reason,omitempty"`
	CheckpointID           common.CheckpointID    `json:"checkpoint_id,omitempty"`
	RunID                  common.RunID           `json:"run_id,omitempty"`
	HandoffID              string                 `json:"handoff_id,omitempty"`
	HandoffStatus          handoff.Status         `json:"handoff_status,omitempty"`
	LatestAction           *recoveryaction.Record `json:"latest_action,omitempty"`
	Issues                 []RecoveryIssue        `json:"issues,omitempty"`
}

func (c *Coordinator) AssessRecovery(ctx context.Context, taskID string) (RecoveryAssessment, error) {
	assessment, err := c.assessContinue(ctx, common.TaskID(strings.TrimSpace(taskID)))
	if err != nil {
		return RecoveryAssessment{}, err
	}
	return c.recoveryFromContinueAssessment(assessment), nil
}

func (c *Coordinator) recoveryFromContinueAssessment(assessment continueAssessment) RecoveryAssessment {
	recovery := RecoveryAssessment{
		TaskID:            assessment.TaskID,
		ContinuityOutcome: assessment.Outcome,
		DriftClass:        assessment.DriftClass,
		Reason:            assessment.Reason,
		CheckpointID:      assessment.ReuseCheckpointID,
		Issues:            recoveryIssuesFromContinuity(assessment.Issues),
	}
	if assessment.LatestCheckpoint != nil {
		recovery.CheckpointID = assessment.LatestCheckpoint.CheckpointID
	}
	if assessment.LatestRun != nil {
		recovery.RunID = assessment.LatestRun.RunID
	}
	if assessment.LatestHandoff != nil {
		recovery.HandoffID = assessment.LatestHandoff.HandoffID
		recovery.HandoffStatus = assessment.LatestHandoff.Status
	}
	if assessment.LatestRecoveryAction != nil {
		actionCopy := *assessment.LatestRecoveryAction
		recovery.LatestAction = &actionCopy
	}

	switch assessment.Outcome {
	case ContinueOutcomeBlockedInconsistent:
		recovery.RecoveryClass = RecoveryClassRepairRequired
		recovery.RecommendedAction = RecoveryActionRepairContinuity
		recovery.RequiresRepair = true
		if recovery.Reason == "" {
			recovery.Reason = "continuity state is inconsistent and must be repaired before recovery"
		}
		return applyRecoveryActionProgression(recovery)
	case ContinueOutcomeBlockedDrift:
		recovery.RecoveryClass = RecoveryClassBlockedDrift
		recovery.RecommendedAction = RecoveryActionMakeResumeDecision
		recovery.RequiresDecision = true
		if recovery.Reason == "" {
			recovery.Reason = "repository drift blocks automatic recovery"
		}
		return applyRecoveryActionProgression(recovery)
	case ContinueOutcomeNeedsDecision:
		recovery.RecoveryClass = RecoveryClassDecisionRequired
		recovery.RecommendedAction = RecoveryActionMakeResumeDecision
		recovery.RequiresDecision = true
		if recovery.Reason == "" {
			recovery.Reason = "resume requires an explicit operator decision"
		}
		return applyRecoveryActionProgression(recovery)
	case ContinueOutcomeStaleReconciled:
		recovery.RecoveryClass = RecoveryClassStaleRunReconciliationRequired
		recovery.RecommendedAction = RecoveryActionReconcileStaleRun
		recovery.RequiresReconciliation = true
		if recovery.Reason == "" {
			recovery.Reason = "latest run is still durably RUNNING and must be reconciled before recovery"
		}
		return applyRecoveryActionProgression(recovery)
	case ContinueOutcomeSafe:
		// Continue with operational recovery classification below.
	default:
		recovery.RecoveryClass = RecoveryClassRepairRequired
		recovery.RecommendedAction = RecoveryActionRepairContinuity
		recovery.RequiresRepair = true
		if recovery.Reason == "" {
			recovery.Reason = fmt.Sprintf("unsupported continuity outcome: %s", assessment.Outcome)
		}
		return applyRecoveryActionProgression(recovery)
	}

	if packet := assessment.LatestHandoff; packet != nil && packet.Status == handoff.StatusAccepted && packet.TargetWorker == rundomain.WorkerKindClaude && packet.IsResumable {
		handoffContinuity := assessHandoffContinuity(assessment.TaskID, packet, assessment.LatestLaunch, assessment.LatestAck, assessment.LatestFollowThrough)
		switch handoffContinuity.State {
		case HandoffContinuityStateAcceptedNotLaunched, HandoffContinuityStateLaunchFailedRetryable:
			recovery.RecoveryClass = RecoveryClassAcceptedHandoffLaunchReady
			recovery.RecommendedAction = RecoveryActionLaunchAcceptedHandoff
			recovery.ReadyForHandoffLaunch = true
			recovery.Reason = handoffContinuity.Reason
			return applyRecoveryActionProgression(recovery)
		case HandoffContinuityStateLaunchPendingOutcome:
			recovery.RecoveryClass = RecoveryClassHandoffLaunchPendingOutcome
			recovery.RecommendedAction = RecoveryActionWaitForLaunchOutcome
			recovery.Reason = handoffContinuity.Reason
			return applyRecoveryActionProgression(recovery)
		case HandoffContinuityStateLaunchCompletedAckSeen, HandoffContinuityStateLaunchCompletedAckEmpty, HandoffContinuityStateFollowThroughProofOfLife, HandoffContinuityStateFollowThroughConfirmed, HandoffContinuityStateFollowThroughUnknown:
			recovery.RecoveryClass = RecoveryClassHandoffLaunchCompleted
			recovery.RecommendedAction = RecoveryActionMonitorLaunchedHandoff
			recovery.Reason = handoffContinuity.Reason
			return applyRecoveryActionProgression(recovery)
		case HandoffContinuityStateFollowThroughStalled:
			recovery.RecoveryClass = RecoveryClassHandoffFollowThroughReviewRequired
			recovery.RecommendedAction = RecoveryActionReviewHandoffFollowThrough
			recovery.RequiresReview = true
			recovery.Reason = handoffContinuity.Reason
			return applyRecoveryActionProgression(recovery)
		case HandoffContinuityStateLaunchCompletedAckLost:
			recovery.RecoveryClass = RecoveryClassRepairRequired
			recovery.RecommendedAction = RecoveryActionRepairContinuity
			recovery.RequiresRepair = true
			recovery.Reason = handoffContinuity.Reason
			return applyRecoveryActionProgression(recovery)
		}
	}

	if assessment.Capsule.CurrentPhase == phase.PhaseBriefReady {
		if override, ok := briefReadyRecoveryOverride(recovery); ok {
			return override
		}
		recovery.RecoveryClass = RecoveryClassReadyNextRun
		recovery.RecommendedAction = RecoveryActionStartNextRun
		recovery.ReadyForNextRun = true
		recovery.Reason = fmt.Sprintf("task is ready for the next bounded run with brief %s", assessment.Capsule.CurrentBriefID)
		return applyRecoveryActionProgression(recovery)
	}

	if runRec := assessment.LatestRun; runRec != nil {
		switch runRec.Status {
		case rundomain.StatusInterrupted:
			if assessment.LatestCheckpoint != nil && assessment.LatestCheckpoint.IsResumable {
				recovery.RecoveryClass = RecoveryClassInterruptedRunRecoverable
				recovery.RecommendedAction = RecoveryActionResumeInterrupted
				recovery.ReadyForNextRun = false
				recovery.Reason = fmt.Sprintf("interrupted run %s is recoverable from checkpoint %s", runRec.RunID, assessment.LatestCheckpoint.CheckpointID)
				return applyRecoveryActionProgression(recovery)
			}
			recovery.RecoveryClass = RecoveryClassRepairRequired
			recovery.RecommendedAction = RecoveryActionRepairContinuity
			recovery.RequiresRepair = true
			recovery.Reason = fmt.Sprintf("interrupted run %s has no resumable checkpoint for recovery", runRec.RunID)
			return applyRecoveryActionProgression(recovery)
		case rundomain.StatusFailed:
			recovery.RecoveryClass = RecoveryClassFailedRunReviewRequired
			recovery.RecommendedAction = RecoveryActionInspectFailedRun
			recovery.RequiresReview = true
			recovery.Reason = fmt.Sprintf("latest run %s failed; inspect failure evidence before retrying or regenerating the brief", runRec.RunID)
			return applyRecoveryActionProgression(recovery)
		case rundomain.StatusCompleted:
			switch assessment.Capsule.CurrentPhase {
			case phase.PhaseValidating:
				recovery.RecoveryClass = RecoveryClassValidationReviewRequired
				recovery.RecommendedAction = RecoveryActionReviewValidation
				recovery.RequiresReview = true
				recovery.Reason = fmt.Sprintf("latest run %s completed and task is awaiting validation review", runRec.RunID)
				return applyRecoveryActionProgression(recovery)
			case phase.PhaseCompleted:
				recovery.RecoveryClass = RecoveryClassCompletedNoAction
				recovery.RecommendedAction = RecoveryActionNone
				recovery.Reason = "task is already completed; no recovery action is required"
				return applyRecoveryActionProgression(recovery)
			case phase.PhaseBriefReady:
				if override, ok := briefReadyRecoveryOverride(recovery); ok {
					return override
				}
				recovery.RecoveryClass = RecoveryClassReadyNextRun
				recovery.RecommendedAction = RecoveryActionStartNextRun
				recovery.ReadyForNextRun = true
				recovery.Reason = fmt.Sprintf("task is ready for the next bounded run with brief %s", assessment.Capsule.CurrentBriefID)
				return applyRecoveryActionProgression(recovery)
			}
		}
	}

	switch assessment.Capsule.CurrentPhase {
	case phase.PhasePaused:
		recovery.RecoveryClass = RecoveryClassInterruptedRunRecoverable
		recovery.RecommendedAction = RecoveryActionResumeInterrupted
		if assessment.LatestCheckpoint != nil && assessment.LatestCheckpoint.IsResumable {
			recovery.ReadyForNextRun = false
			recovery.Reason = fmt.Sprintf("paused task is recoverable from checkpoint %s", assessment.LatestCheckpoint.CheckpointID)
		} else {
			recovery.RecoveryClass = RecoveryClassRepairRequired
			recovery.RecommendedAction = RecoveryActionRepairContinuity
			recovery.RequiresRepair = true
			recovery.Reason = "paused task has no resumable checkpoint for recovery"
		}
	case phase.PhaseValidating:
		recovery.RecoveryClass = RecoveryClassValidationReviewRequired
		recovery.RecommendedAction = RecoveryActionReviewValidation
		recovery.RequiresReview = true
		recovery.Reason = "task is awaiting validation review before another run"
	case phase.PhaseCompleted:
		recovery.RecoveryClass = RecoveryClassCompletedNoAction
		recovery.RecommendedAction = RecoveryActionNone
		recovery.Reason = "task is already completed; no recovery action is required"
	default:
		recovery.RecoveryClass = RecoveryClassRepairRequired
		recovery.RecommendedAction = RecoveryActionRepairContinuity
		recovery.RequiresRepair = true
		if recovery.Reason == "" {
			recovery.Reason = fmt.Sprintf("task phase %s does not support deterministic recovery", assessment.Capsule.CurrentPhase)
		}
	}

	return applyRecoveryActionProgression(recovery)
}

func briefReadyRecoveryOverride(recovery RecoveryAssessment) (RecoveryAssessment, bool) {
	if recovery.LatestAction == nil {
		return recovery, false
	}
	switch recovery.LatestAction.Kind {
	case recoveryaction.KindDecisionContinue:
		recovery.RecoveryClass = RecoveryClassContinueExecutionRequired
		recovery.RecommendedAction = RecoveryActionExecuteContinueRecovery
		recovery.ReadyForNextRun = false
		recovery.ReadyForHandoffLaunch = false
		recovery.RequiresDecision = false
		recovery.RequiresReview = false
		recovery.RequiresRepair = false
		recovery.RequiresReconciliation = false
		recovery.Reason = fmt.Sprintf("operator chose to continue with the current brief, but explicit continue finalization is still required before the next bounded run: %s", recovery.LatestAction.Summary)
		return recovery, true
	case recoveryaction.KindInterruptedResumeExecuted:
		recovery.RecoveryClass = RecoveryClassContinueExecutionRequired
		recovery.RecommendedAction = RecoveryActionExecuteContinueRecovery
		recovery.ReadyForNextRun = false
		recovery.ReadyForHandoffLaunch = false
		recovery.RequiresDecision = false
		recovery.RequiresReview = false
		recovery.RequiresRepair = false
		recovery.RequiresReconciliation = false
		recovery.Reason = fmt.Sprintf("operator explicitly resumed interrupted execution lineage; continue recovery still must be executed before the next bounded run: %s", recovery.LatestAction.Summary)
		return recovery, true
	default:
		return recovery, false
	}
}

func applyRecoveryActionProgression(recovery RecoveryAssessment) RecoveryAssessment {
	if recovery.LatestAction == nil {
		return recovery
	}
	action := recovery.LatestAction
	switch action.Kind {
	case recoveryaction.KindFailedRunReviewed:
		if recovery.RecoveryClass == RecoveryClassFailedRunReviewRequired && action.RunID == recovery.RunID {
			recovery.RecoveryClass = RecoveryClassDecisionRequired
			recovery.RecommendedAction = RecoveryActionMakeResumeDecision
			recovery.RequiresReview = false
			recovery.RequiresDecision = true
			recovery.Reason = fmt.Sprintf("failed run %s was reviewed; choose whether to continue with the current brief or regenerate it", recovery.RunID)
		}
	case recoveryaction.KindInterruptedRunReviewed:
		if recovery.RecoveryClass == RecoveryClassInterruptedRunRecoverable && (recovery.RunID == "" || action.RunID == recovery.RunID) {
			recovery.RecommendedAction = RecoveryActionResumeInterrupted
			recovery.ReadyForNextRun = false
			recovery.ReadyForHandoffLaunch = false
			recovery.RequiresDecision = false
			recovery.RequiresReview = false
			recovery.RequiresRepair = false
			recovery.RequiresReconciliation = false
			recovery.Reason = fmt.Sprintf("interrupted execution lineage was reviewed and remains recoverable from checkpoint %s: %s", nonEmpty(string(recovery.CheckpointID), "unknown"), action.Summary)
		}
	case recoveryaction.KindInterruptedResumeExecuted:
		switch recovery.RecoveryClass {
		case RecoveryClassInterruptedRunRecoverable, RecoveryClassReadyNextRun, RecoveryClassContinueExecutionRequired:
			recovery.RecoveryClass = RecoveryClassContinueExecutionRequired
			recovery.RecommendedAction = RecoveryActionExecuteContinueRecovery
			recovery.ReadyForNextRun = false
			recovery.ReadyForHandoffLaunch = false
			recovery.RequiresDecision = false
			recovery.RequiresReview = false
			recovery.RequiresRepair = false
			recovery.RequiresReconciliation = false
			recovery.Reason = fmt.Sprintf("operator explicitly resumed interrupted execution lineage; continue recovery still must be executed before the next bounded run: %s", action.Summary)
		}
	case recoveryaction.KindValidationReviewed:
		if recovery.RecoveryClass == RecoveryClassValidationReviewRequired && (recovery.RunID == "" || action.RunID == recovery.RunID) {
			recovery.RecoveryClass = RecoveryClassDecisionRequired
			recovery.RecommendedAction = RecoveryActionMakeResumeDecision
			recovery.RequiresReview = false
			recovery.RequiresDecision = true
			recovery.Reason = fmt.Sprintf("validation state for run %s was reviewed; choose whether to continue with the current brief or regenerate it", nonEmpty(string(recovery.RunID), "unknown"))
		}
	case recoveryaction.KindRepairIntentRecorded:
		if recovery.RecoveryClass == RecoveryClassRepairRequired || recovery.RecoveryClass == RecoveryClassBlockedDrift {
			recovery.Reason = fmt.Sprintf("repair intent recorded: %s", action.Summary)
		}
	case recoveryaction.KindPendingLaunchReviewed:
		if recovery.RecoveryClass == RecoveryClassHandoffLaunchPendingOutcome {
			recovery.Reason = fmt.Sprintf("pending handoff launch was reviewed: %s", action.Summary)
		}
	case recoveryaction.KindDecisionContinue:
		switch recovery.RecoveryClass {
		case RecoveryClassDecisionRequired, RecoveryClassFailedRunReviewRequired, RecoveryClassValidationReviewRequired, RecoveryClassReadyNextRun:
			recovery.RecoveryClass = RecoveryClassContinueExecutionRequired
			recovery.RecommendedAction = RecoveryActionExecuteContinueRecovery
			recovery.ReadyForNextRun = false
			recovery.ReadyForHandoffLaunch = false
			recovery.RequiresDecision = false
			recovery.RequiresReview = false
			recovery.RequiresRepair = false
			recovery.RequiresReconciliation = false
			recovery.Reason = fmt.Sprintf("operator chose to continue with the current brief, but explicit continue finalization is still required before the next bounded run: %s", action.Summary)
		}
	case recoveryaction.KindContinueExecuted:
		if recovery.RecoveryClass == RecoveryClassReadyNextRun {
			recovery.RecommendedAction = RecoveryActionStartNextRun
			recovery.ReadyForNextRun = true
			recovery.ReadyForHandoffLaunch = false
			recovery.RequiresDecision = false
			recovery.RequiresReview = false
			recovery.RequiresRepair = false
			recovery.RequiresReconciliation = false
			recovery.Reason = fmt.Sprintf("operator explicitly confirmed the current brief for the next bounded run: %s", action.Summary)
		}
	case recoveryaction.KindDecisionRegenerateBrief:
		if recovery.RecoveryClass == RecoveryClassReadyNextRun {
			recovery.Reason = fmt.Sprintf("execution brief was regenerated after operator decision: %s", action.Summary)
			return recovery
		}
		recovery.RecoveryClass = RecoveryClassRebriefRequired
		recovery.RecommendedAction = RecoveryActionRegenerateBrief
		recovery.ReadyForNextRun = false
		recovery.ReadyForHandoffLaunch = false
		recovery.RequiresDecision = false
		recovery.RequiresReview = false
		recovery.RequiresRepair = false
		recovery.RequiresReconciliation = false
		recovery.Reason = fmt.Sprintf("operator chose to regenerate the execution brief before another run: %s", action.Summary)
	}
	return recovery
}

func recoveryIssuesFromContinuity(values []continuityViolation) []RecoveryIssue {
	if len(values) == 0 {
		return nil
	}
	issues := make([]RecoveryIssue, 0, len(values))
	for _, value := range values {
		issues = append(issues, RecoveryIssue{Code: string(value.Code), Message: value.Message})
	}
	return issues
}

func applyRecoveryAssessmentToContinueResult(result *ContinueTaskResult, recovery RecoveryAssessment) {
	if result == nil {
		return
	}
	result.RecoveryClass = recovery.RecoveryClass
	result.RecommendedAction = recovery.RecommendedAction
	result.ReadyForNextRun = recovery.ReadyForNextRun
	result.ReadyForHandoffLaunch = recovery.ReadyForHandoffLaunch
	result.RecoveryReason = recovery.Reason
}

func applyRecoveryAssessmentToStatus(status *StatusTaskResult, recovery RecoveryAssessment, checkpointResumable bool) {
	if status == nil {
		return
	}
	status.CheckpointResumable = checkpointResumable
	status.IsResumable = recovery.ReadyForNextRun
	status.RecoveryClass = recovery.RecoveryClass
	status.RecommendedAction = recovery.RecommendedAction
	status.ReadyForNextRun = recovery.ReadyForNextRun
	status.ReadyForHandoffLaunch = recovery.ReadyForHandoffLaunch
	status.RecoveryReason = recovery.Reason
	if recovery.LatestAction != nil {
		actionCopy := *recovery.LatestAction
		status.LatestRecoveryAction = &actionCopy
	} else {
		status.LatestRecoveryAction = nil
	}
	if recovery.Reason != "" {
		status.ResumeDescriptor = recovery.Reason
	}
}

func runStartBlockedCanonical(recovery RecoveryAssessment) string {
	switch recovery.RecoveryClass {
	case RecoveryClassContinueExecutionRequired:
		return "Execution cannot start yet because operator continue finalization is still required. Execute continue recovery first so Tuku can durably clear the current brief for the next bounded run."
	case RecoveryClassDecisionRequired:
		return "Execution cannot start yet because recovery still requires an explicit operator decision."
	case RecoveryClassFailedRunReviewRequired:
		return "Execution cannot start yet because the latest failed run still requires review."
	case RecoveryClassValidationReviewRequired:
		return "Execution cannot start yet because validation review is still required."
	case RecoveryClassRebriefRequired:
		return "Execution cannot start yet because the execution brief must be regenerated or replaced first."
	case RecoveryClassBlockedDrift:
		return "Execution cannot start yet because repository drift is blocking deterministic recovery."
	case RecoveryClassRepairRequired:
		return "Execution cannot start yet because continuity repair is still required."
	case RecoveryClassAcceptedHandoffLaunchReady:
		return "Execution cannot start yet because the active recovery path is accepted handoff launch, not a new local run."
	case RecoveryClassHandoffLaunchPendingOutcome:
		return "Execution cannot start yet because the latest handoff launch outcome is still pending."
	case RecoveryClassHandoffLaunchCompleted:
		return "Execution cannot start yet because the latest handoff launch step is already complete and should be monitored rather than replaced by a new local run."
	case RecoveryClassInterruptedRunRecoverable:
		return "Execution cannot start yet because the task is in interrupted-run recovery, not cleared for a fresh bounded run."
	case RecoveryClassStaleRunReconciliationRequired:
		return "Execution cannot start yet because stale run reconciliation is still required."
	case RecoveryClassCompletedNoAction:
		return "Execution cannot start because the task is already completed."
	default:
		return fmt.Sprintf("Execution cannot start yet because recovery posture is %s.", recovery.RecoveryClass)
	}
}

func runStartEligibility(recovery RecoveryAssessment) (bool, string) {
	if recovery.RecoveryClass == RecoveryClassReadyNextRun && recovery.RecommendedAction == RecoveryActionStartNextRun && recovery.ReadyForNextRun {
		return true, ""
	}
	return false, runStartBlockedCanonical(recovery)
}

```


## internal/orchestrator/service.go

```go
package orchestrator

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tuku/internal/adapters/adapter_contract"
	"tuku/internal/domain/brief"
	"tuku/internal/domain/capsule"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	contextdomain "tuku/internal/domain/context"
	"tuku/internal/domain/conversation"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/intent"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/recoveryaction"
	rundomain "tuku/internal/domain/run"
	anchorgit "tuku/internal/git/anchor"
	"tuku/internal/response/canonical"
	"tuku/internal/storage"
)

type StartTaskResult struct {
	TaskID            common.TaskID
	ConversationID    common.ConversationID
	Phase             phase.Phase
	CanonicalResponse string
	RepoAnchor        anchorgit.Snapshot
}

type MessageTaskResult struct {
	TaskID            common.TaskID
	Phase             phase.Phase
	IntentClass       intent.Class
	BriefID           common.BriefID
	BriefHash         string
	CanonicalResponse string
	RepoAnchor        anchorgit.Snapshot
}

type RunTaskRequest struct {
	TaskID             string
	Action             string // start|complete|interrupt
	Mode               string // real|noop
	RunID              common.RunID
	SimulateInterrupt  bool
	InterruptionReason string
}

type RunTaskResult struct {
	TaskID            common.TaskID
	RunID             common.RunID
	RunStatus         rundomain.Status
	Phase             phase.Phase
	CanonicalResponse string
}

type ContinueOutcome string

const (
	ContinueOutcomeSafe                ContinueOutcome = "SAFE_RESUME_AVAILABLE"
	ContinueOutcomeStaleReconciled     ContinueOutcome = "STALE_RUN_RECONCILED"
	ContinueOutcomeNeedsDecision       ContinueOutcome = "RESUME_DECISION_REQUIRED"
	ContinueOutcomeBlockedDrift        ContinueOutcome = "RESUME_BLOCKED_DRIFT"
	ContinueOutcomeBlockedInconsistent ContinueOutcome = "RESUME_BLOCKED_INCONSISTENT_STATE"
)

type ContinueTaskResult struct {
	TaskID                common.TaskID
	Outcome               ContinueOutcome
	DriftClass            checkpoint.DriftClass
	Phase                 phase.Phase
	RunID                 common.RunID
	CheckpointID          common.CheckpointID
	ResumeDescriptor      string
	RecoveryClass         RecoveryClass
	RecommendedAction     RecoveryAction
	ReadyForNextRun       bool
	ReadyForHandoffLaunch bool
	RecoveryReason        string
	CanonicalResponse     string
}

type CreateCheckpointResult struct {
	TaskID            common.TaskID
	CheckpointID      common.CheckpointID
	Trigger           checkpoint.Trigger
	IsResumable       bool
	CanonicalResponse string
}

type StatusTaskResult struct {
	TaskID                      common.TaskID
	ConversationID              common.ConversationID
	Goal                        string
	Phase                       phase.Phase
	Status                      string
	CurrentIntentID             common.IntentID
	CurrentIntentClass          intent.Class
	CurrentIntentSummary        string
	CurrentBriefID              common.BriefID
	CurrentBriefHash            string
	LatestRunID                 common.RunID
	LatestRunStatus             rundomain.Status
	LatestRunSummary            string
	RepoAnchor                  anchorgit.Snapshot
	LatestCheckpointID          common.CheckpointID
	LatestCheckpointAt          time.Time
	LatestCheckpointTrigger     checkpoint.Trigger
	CheckpointResumable         bool
	ResumeDescriptor            string
	LatestLaunchAttemptID       string
	LatestLaunchID              string
	LatestLaunchStatus          handoff.LaunchStatus
	LatestAcknowledgmentID      string
	LatestAcknowledgmentStatus  handoff.AcknowledgmentStatus
	LatestAcknowledgmentSummary string
	LatestFollowThroughID       string
	LatestFollowThroughKind     handoff.FollowThroughKind
	LatestFollowThroughSummary  string
	LaunchControlState          LaunchControlState
	LaunchRetryDisposition      LaunchRetryDisposition
	LaunchControlReason         string
	HandoffContinuityState      HandoffContinuityState
	HandoffContinuityReason     string
	HandoffContinuationProven   bool
	IsResumable                 bool
	RecoveryClass               RecoveryClass
	RecommendedAction           RecoveryAction
	ReadyForNextRun             bool
	ReadyForHandoffLaunch       bool
	RecoveryReason              string
	LatestRecoveryAction        *recoveryaction.Record
	LastEventID                 common.EventID
	LastEventType               proof.EventType
	LastEventAt                 time.Time
}

type InspectTaskResult struct {
	TaskID                common.TaskID
	Intent                *intent.State
	Brief                 *brief.ExecutionBrief
	Run                   *rundomain.ExecutionRun
	Checkpoint            *checkpoint.Checkpoint
	Handoff               *handoff.Packet
	Launch                *handoff.Launch
	Acknowledgment        *handoff.Acknowledgment
	FollowThrough         *handoff.FollowThrough
	LaunchControl         *LaunchControl
	HandoffContinuity     *HandoffContinuity
	Recovery              *RecoveryAssessment
	LatestRecoveryAction  *recoveryaction.Record
	RecentRecoveryActions []recoveryaction.Record
	RepoAnchor            anchorgit.Snapshot
}

type Dependencies struct {
	Store                  storage.Store
	IntentCompiler         intent.Compiler
	BriefBuilder           brief.Builder
	WorkerAdapter          adapter_contract.WorkerAdapter
	HandoffLauncher        adapter_contract.HandoffLauncher
	Synthesizer            canonical.Synthesizer
	AnchorProvider         anchorgit.Provider
	ShellSessions          ShellSessionRegistry
	ShellSessionStaleAfter time.Duration
	Clock                  func() time.Time
	IDGenerator            func(prefix string) string
}

type Coordinator struct {
	store                  storage.Store
	intentCompiler         intent.Compiler
	briefBuilder           brief.Builder
	workerAdapter          adapter_contract.WorkerAdapter
	handoffLauncher        adapter_contract.HandoffLauncher
	synthesizer            canonical.Synthesizer
	anchorProvider         anchorgit.Provider
	shellSessions          ShellSessionRegistry
	shellSessionStaleAfter time.Duration
	clock                  func() time.Time
	idGenerator            func(prefix string) string
}

func NewCoordinator(deps Dependencies) (*Coordinator, error) {
	if deps.Store == nil {
		return nil, errors.New("store is required")
	}
	if deps.IntentCompiler == nil {
		return nil, errors.New("intent compiler is required")
	}
	if deps.BriefBuilder == nil {
		return nil, errors.New("brief builder is required")
	}
	if deps.Synthesizer == nil {
		return nil, errors.New("canonical synthesizer is required")
	}
	if deps.ShellSessions == nil {
		return nil, errors.New("shell session registry is required")
	}
	if deps.AnchorProvider == nil {
		deps.AnchorProvider = anchorgit.NewGitProvider()
	}
	if deps.ShellSessionStaleAfter <= 0 {
		deps.ShellSessionStaleAfter = DefaultShellSessionStaleAfter
	}
	if deps.Clock == nil {
		deps.Clock = func() time.Time { return time.Now().UTC() }
	}
	if deps.IDGenerator == nil {
		deps.IDGenerator = newID
	}
	return &Coordinator{
		store:                  deps.Store,
		intentCompiler:         deps.IntentCompiler,
		briefBuilder:           deps.BriefBuilder,
		workerAdapter:          deps.WorkerAdapter,
		handoffLauncher:        deps.HandoffLauncher,
		synthesizer:            deps.Synthesizer,
		anchorProvider:         deps.AnchorProvider,
		shellSessions:          deps.ShellSessions,
		shellSessionStaleAfter: deps.ShellSessionStaleAfter,
		clock:                  deps.Clock,
		idGenerator:            deps.IDGenerator,
	}, nil
}

func (c *Coordinator) StartTask(ctx context.Context, goal string, repoRoot string) (StartTaskResult, error) {
	var result StartTaskResult
	err := c.withTx(func(txc *Coordinator) error {
		now := txc.clock()
		taskID := common.TaskID(txc.idGenerator("tsk"))
		conversationID := common.ConversationID(txc.idGenerator("conv"))
		repo := strings.TrimSpace(repoRoot)
		if repo == "" {
			repo = "."
		}
		repo = filepath.Clean(repo)
		anchor := txc.anchorProvider.Capture(ctx, repo)

		caps := capsule.WorkCapsule{
			TaskID:             taskID,
			ConversationID:     conversationID,
			Version:            1,
			CreatedAt:          now,
			UpdatedAt:          now,
			Goal:               strings.TrimSpace(goal),
			AcceptanceCriteria: []string{},
			Constraints:        []string{},
			RepoRoot:           anchor.RepoRoot,
			WorktreePath:       anchor.RepoRoot,
			BranchName:         anchor.Branch,
			HeadSHA:            anchor.HeadSHA,
			WorkingTreeDirty:   anchor.WorkingTreeDirty,
			AnchorCapturedAt:   anchor.CapturedAt,
			CurrentPhase:       phase.PhaseIntake,
			Status:             "ACTIVE",
			CurrentIntentID:    "",
			CurrentBriefID:     "",
			TouchedFiles:       []string{},
			Blockers:           []string{},
			NextAction:         "Await user message for intent interpretation",
			ParentTaskID:       nil,
			ChildTaskIDs:       []common.TaskID{},
			EdgeRefs:           []string{},
		}
		if err := txc.store.Capsules().Create(caps); err != nil {
			return err
		}
		if err := txc.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": string(phase.PhaseIntake), "reason": "task created"}, nil); err != nil {
			return err
		}

		canonicalText := "Tuku task initialized. Repo anchor captured. I am tracking canonical task state and evidence. Send your first implementation instruction to generate an execution brief."
		if err := txc.emitCanonicalConversation(caps, canonicalText, map[string]any{"summary": "task initialized"}, nil); err != nil {
			return err
		}

		result = StartTaskResult{
			TaskID:            taskID,
			ConversationID:    conversationID,
			Phase:             caps.CurrentPhase,
			CanonicalResponse: canonicalText,
			RepoAnchor:        anchor,
		}
		return nil
	})
	if err != nil {
		return StartTaskResult{}, err
	}
	return result, nil
}

func (c *Coordinator) MessageTask(ctx context.Context, taskID string, message string) (MessageTaskResult, error) {
	var result MessageTaskResult
	err := c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(common.TaskID(taskID))
		if err != nil {
			return err
		}
		now := txc.clock()

		anchor := txc.anchorProvider.Capture(ctx, caps.RepoRoot)
		caps.BranchName = anchor.Branch
		caps.HeadSHA = anchor.HeadSHA
		caps.WorkingTreeDirty = anchor.WorkingTreeDirty
		caps.AnchorCapturedAt = anchor.CapturedAt

		userMsg := conversation.Message{
			MessageID:      common.MessageID(txc.idGenerator("msg")),
			ConversationID: caps.ConversationID,
			TaskID:         caps.TaskID,
			Role:           conversation.RoleUser,
			Body:           message,
			CreatedAt:      now,
		}
		if err := txc.store.Conversations().Append(userMsg); err != nil {
			return err
		}
		if err := txc.appendProof(caps, proof.EventUserMessageReceived, proof.ActorUser, "user", map[string]any{"message_id": userMsg.MessageID}, nil); err != nil {
			return err
		}

		recent, err := txc.store.Conversations().ListRecent(caps.ConversationID, 12)
		if err != nil {
			return err
		}
		recentBodies := make([]string, 0, len(recent))
		for _, m := range recent {
			recentBodies = append(recentBodies, m.Body)
		}

		intentState, err := txc.intentCompiler.Compile(intent.CompileInput{
			TaskID:            caps.TaskID,
			LatestMessage:     message,
			RecentMessages:    recentBodies,
			CurrentPhase:      caps.CurrentPhase,
			CurrentBlockers:   caps.Blockers,
			CurrentGoal:       caps.Goal,
			RepoAnchorSummary: fmt.Sprintf("repo=%s branch=%s head=%s dirty=%t", caps.RepoRoot, caps.BranchName, caps.HeadSHA, caps.WorkingTreeDirty),
		})
		if err != nil {
			return err
		}
		intentState.SourceMessageIDs = []common.MessageID{userMsg.MessageID}
		if err := txc.store.Intents().Save(intentState); err != nil {
			return err
		}

		caps.Version++
		caps.UpdatedAt = now
		caps.CurrentIntentID = intentState.IntentID
		caps.CurrentPhase = intentState.ProposedPhase
		if err := txc.appendProof(caps, proof.EventIntentCompiled, proof.ActorSystem, "tuku-intent-stub", map[string]any{
			"intent_id": intentState.IntentID, "class": intentState.Class,
			"normalized_action": intentState.NormalizedAction, "confidence": intentState.Confidence,
		}, nil); err != nil {
			return err
		}

		briefArtifact, err := txc.briefBuilder.Build(brief.BuildInput{
			TaskID:           caps.TaskID,
			IntentID:         intentState.IntentID,
			CapsuleVersion:   caps.Version,
			Goal:             caps.Goal,
			NormalizedAction: intentState.NormalizedAction,
			Constraints:      caps.Constraints,
			ScopeHints:       caps.TouchedFiles,
			ScopeOutHints:    []string{},
			DoneCriteria:     []string{"Execution plan is prepared and ready for worker dispatch"},
			ContextPackID:    "",
			Verbosity:        brief.VerbosityStandard,
			PolicyProfileID:  "default-safe-v1",
		})
		if err != nil {
			return err
		}
		if err := txc.store.Briefs().Save(briefArtifact); err != nil {
			return err
		}

		caps.CurrentBriefID = briefArtifact.BriefID
		caps.CurrentPhase = phase.PhaseBriefReady
		caps.NextAction = "Execution brief is ready. Start a run with `tuku run --task <id>`."
		if err := txc.store.Capsules().Update(caps); err != nil {
			return err
		}

		if err := txc.appendProof(caps, proof.EventBriefCreated, proof.ActorSystem, "tuku-brief-builder", map[string]any{"brief_id": briefArtifact.BriefID, "brief_hash": briefArtifact.BriefHash, "intent_id": intentState.IntentID}, nil); err != nil {
			return err
		}
		if err := txc.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": caps.CurrentPhase, "reason": "intent and brief prepared"}, nil); err != nil {
			return err
		}

		recentEvents, err := txc.store.Proofs().ListByTask(caps.TaskID, 10)
		if err != nil {
			return err
		}
		canonicalText, err := txc.synthesizer.Synthesize(ctx, caps, recentEvents)
		if err != nil {
			return err
		}
		if err := txc.emitCanonicalConversation(caps, canonicalText, map[string]any{"intent_id": intentState.IntentID, "brief_id": briefArtifact.BriefID}, nil); err != nil {
			return err
		}

		result = MessageTaskResult{
			TaskID:            caps.TaskID,
			Phase:             caps.CurrentPhase,
			IntentClass:       intentState.Class,
			BriefID:           briefArtifact.BriefID,
			BriefHash:         briefArtifact.BriefHash,
			CanonicalResponse: canonicalText,
			RepoAnchor:        anchor,
		}
		return nil
	})
	if err != nil {
		return MessageTaskResult{}, err
	}
	return result, nil
}

func (c *Coordinator) RunTask(ctx context.Context, req RunTaskRequest) (RunTaskResult, error) {
	action := strings.TrimSpace(strings.ToLower(req.Action))
	if action == "" {
		action = "start"
	}
	mode := strings.TrimSpace(strings.ToLower(req.Mode))
	if mode == "" {
		mode = "real"
	}

	switch action {
	case "start":
		if mode == "real" {
			return c.startRunRealStaged(ctx, req)
		}
		var result RunTaskResult
		err := c.withTx(func(txc *Coordinator) error {
			caps, err := txc.store.Capsules().Get(common.TaskID(req.TaskID))
			if err != nil {
				return err
			}
			result, err = txc.startRunNoop(ctx, caps, req)
			return err
		})
		if err != nil {
			return RunTaskResult{}, err
		}
		return result, nil
	case "complete":
		var result RunTaskResult
		err := c.withTx(func(txc *Coordinator) error {
			caps, err := txc.store.Capsules().Get(common.TaskID(req.TaskID))
			if err != nil {
				return err
			}
			result, err = txc.completeRun(ctx, caps, req)
			return err
		})
		if err != nil {
			return RunTaskResult{}, err
		}
		return result, nil
	case "interrupt":
		var result RunTaskResult
		err := c.withTx(func(txc *Coordinator) error {
			caps, err := txc.store.Capsules().Get(common.TaskID(req.TaskID))
			if err != nil {
				return err
			}
			result, err = txc.interruptRun(ctx, caps, req)
			return err
		})
		if err != nil {
			return RunTaskResult{}, err
		}
		return result, nil
	default:
		return RunTaskResult{}, fmt.Errorf("unsupported run action: %s", req.Action)
	}
}

func (c *Coordinator) CreateCheckpoint(ctx context.Context, taskID string) (CreateCheckpointResult, error) {
	var result CreateCheckpointResult
	err := c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(common.TaskID(taskID))
		if err != nil {
			return err
		}
		anchor := txc.anchorProvider.Capture(ctx, caps.RepoRoot)
		caps.BranchName = anchor.Branch
		caps.HeadSHA = anchor.HeadSHA
		caps.WorkingTreeDirty = anchor.WorkingTreeDirty
		caps.AnchorCapturedAt = anchor.CapturedAt
		caps.Version++
		caps.UpdatedAt = txc.clock()
		if err := txc.store.Capsules().Update(caps); err != nil {
			return err
		}

		resumable := caps.CurrentBriefID != "" && caps.CurrentPhase != phase.PhaseBlocked && caps.CurrentPhase != phase.PhaseAwaitingDecision
		descriptor := "Manual checkpoint captured for deterministic continue."
		if !resumable {
			descriptor = "Manual checkpoint captured for recovery inspection; direct resume is not currently ready."
		}
		cp, err := txc.createCheckpoint(caps, "", checkpoint.TriggerManual, resumable, descriptor)
		if err != nil {
			return err
		}
		canonical := fmt.Sprintf(
			"Manual checkpoint %s captured. Task is resumable from branch %s (head %s).",
			cp.CheckpointID,
			caps.BranchName,
			caps.HeadSHA,
		)
		if !resumable {
			canonical = fmt.Sprintf(
				"Manual checkpoint %s captured on branch %s (head %s), but direct resume is not currently ready.",
				cp.CheckpointID,
				caps.BranchName,
				caps.HeadSHA,
			)
		}
		if err := txc.emitCanonicalConversation(caps, canonical, map[string]any{
			"checkpoint_id": cp.CheckpointID,
			"trigger":       cp.Trigger,
			"is_resumable":  cp.IsResumable,
		}, nil); err != nil {
			return err
		}

		result = CreateCheckpointResult{
			TaskID:            caps.TaskID,
			CheckpointID:      cp.CheckpointID,
			Trigger:           cp.Trigger,
			IsResumable:       cp.IsResumable,
			CanonicalResponse: canonical,
		}
		return nil
	})
	if err != nil {
		return CreateCheckpointResult{}, err
	}
	return result, nil
}

func (c *Coordinator) ContinueTask(ctx context.Context, taskID string) (ContinueTaskResult, error) {
	assessment, err := c.assessContinue(ctx, common.TaskID(taskID))
	if err != nil {
		return ContinueTaskResult{}, err
	}
	recovery := c.recoveryFromContinueAssessment(assessment)
	if assessment.Outcome == ContinueOutcomeSafe && !recovery.ReadyForNextRun {
		assessment.RequiresMutation = false
	}
	if !assessment.RequiresMutation {
		return c.recordNoMutationContinueOutcome(ctx, assessment, recovery)
	}

	var result ContinueTaskResult
	err = c.withTx(func(txc *Coordinator) error {
		return txc.finalizeContinue(ctx, assessment, recovery, &result)
	})
	if err != nil {
		return ContinueTaskResult{}, err
	}
	return result, nil
}

func (c *Coordinator) recordNoMutationContinueOutcome(_ context.Context, assessment continueAssessment, recovery RecoveryAssessment) (ContinueTaskResult, error) {
	result := c.noMutationContinueResult(assessment, recovery)
	err := c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(assessment.TaskID)
		if err != nil {
			return err
		}
		runID := runIDPointer(result.RunID)
		payload := map[string]any{
			"outcome":           result.Outcome,
			"drift_class":       result.DriftClass,
			"checkpoint_id":     result.CheckpointID,
			"resume_descriptor": result.ResumeDescriptor,
			"no_state_mutation": true,
			"checkpoint_reused": result.CheckpointID != "",
			"assessment_reason": assessment.Reason,
		}
		payload["recovery_class"] = result.RecoveryClass
		payload["recommended_action"] = result.RecommendedAction
		payload["ready_for_next_run"] = result.ReadyForNextRun
		payload["ready_for_handoff_launch"] = result.ReadyForHandoffLaunch
		payload["recovery_reason"] = result.RecoveryReason
		if err := txc.appendProof(caps, proof.EventContinueAssessed, proof.ActorSystem, "tuku-daemon", payload, runID); err != nil {
			return err
		}
		if err := txc.emitCanonicalConversation(caps, result.CanonicalResponse, payload, runID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return ContinueTaskResult{}, err
	}
	return result, nil
}

type continueAssessment struct {
	TaskID               common.TaskID
	Capsule              capsule.WorkCapsule
	LatestRun            *rundomain.ExecutionRun
	LatestCheckpoint     *checkpoint.Checkpoint
	LatestHandoff        *handoff.Packet
	LatestLaunch         *handoff.Launch
	LatestAck            *handoff.Acknowledgment
	LatestFollowThrough  *handoff.FollowThrough
	LatestRecoveryAction *recoveryaction.Record
	FreshAnchor          anchorgit.Snapshot
	DriftClass           checkpoint.DriftClass
	Outcome              ContinueOutcome
	Reason               string
	Issues               []continuityViolation
	RequiresMutation     bool
	ReuseCheckpointID    common.CheckpointID
}

func (c *Coordinator) assessContinue(ctx context.Context, taskID common.TaskID) (continueAssessment, error) {
	snapshot, err := c.loadContinuitySnapshot(taskID)
	if err != nil {
		return continueAssessment{}, err
	}
	caps := snapshot.Capsule
	anchor := c.anchorProvider.Capture(ctx, caps.RepoRoot)
	issues, err := c.validateContinuitySnapshot(snapshot)
	if err != nil {
		return continueAssessment{}, err
	}
	issue := firstContinuityViolationMessage(issues)
	if issue != "" {
		reuse := c.canReuseInconsistencyCheckpoint(caps, snapshot.LatestCheckpoint, anchor, issue)
		return continueAssessment{
			TaskID:               taskID,
			Capsule:              caps,
			LatestRun:            snapshot.LatestRun,
			LatestCheckpoint:     snapshot.LatestCheckpoint,
			LatestHandoff:        snapshot.LatestHandoff,
			LatestLaunch:         snapshot.LatestLaunch,
			LatestAck:            snapshot.LatestAcknowledgment,
			LatestFollowThrough:  snapshot.LatestFollowThrough,
			LatestRecoveryAction: snapshot.LatestRecoveryAction,
			FreshAnchor:          anchor,
			Outcome:              ContinueOutcomeBlockedInconsistent,
			Reason:               issue,
			Issues:               issues,
			DriftClass:           checkpoint.DriftNone,
			RequiresMutation:     !reuse,
			ReuseCheckpointID:    reusableCheckpointID(snapshot.LatestCheckpoint, reuse),
		}, nil
	}

	if snapshot.LatestRun != nil && snapshot.LatestRun.Status == rundomain.StatusRunning {
		return continueAssessment{
			TaskID:               taskID,
			Capsule:              caps,
			LatestRun:            snapshot.LatestRun,
			LatestCheckpoint:     snapshot.LatestCheckpoint,
			LatestHandoff:        snapshot.LatestHandoff,
			LatestLaunch:         snapshot.LatestLaunch,
			LatestAck:            snapshot.LatestAcknowledgment,
			LatestFollowThrough:  snapshot.LatestFollowThrough,
			LatestRecoveryAction: snapshot.LatestRecoveryAction,
			FreshAnchor:          anchor,
			Outcome:              ContinueOutcomeStaleReconciled,
			Reason:               "latest run is durably RUNNING and requires explicit stale reconciliation",
			Issues:               issues,
			DriftClass:           checkpoint.DriftNone,
			RequiresMutation:     true,
		}, nil
	}

	baseline := anchorFromCapsule(caps)
	if snapshot.LatestCheckpoint != nil {
		baseline = snapshot.LatestCheckpoint.Anchor
	}
	drift := classifyAnchorDrift(baseline, anchor)

	if caps.CurrentPhase == phase.PhaseAwaitingDecision {
		reuse := c.canReuseDecisionCheckpoint(caps, snapshot.LatestCheckpoint, anchor)
		outcome := ContinueOutcomeNeedsDecision
		reason := "task is already in decision-gated resume state"
		if drift == checkpoint.DriftMajor {
			outcome = ContinueOutcomeBlockedDrift
			reason = "task remains decision-gated with major repo drift"
		}
		return continueAssessment{
			TaskID:               taskID,
			Capsule:              caps,
			LatestRun:            snapshot.LatestRun,
			LatestCheckpoint:     snapshot.LatestCheckpoint,
			LatestHandoff:        snapshot.LatestHandoff,
			LatestLaunch:         snapshot.LatestLaunch,
			LatestAck:            snapshot.LatestAcknowledgment,
			LatestFollowThrough:  snapshot.LatestFollowThrough,
			LatestRecoveryAction: snapshot.LatestRecoveryAction,
			FreshAnchor:          anchor,
			Outcome:              outcome,
			Reason:               reason,
			Issues:               issues,
			DriftClass:           drift,
			RequiresMutation:     !reuse,
			ReuseCheckpointID:    reusableCheckpointID(snapshot.LatestCheckpoint, reuse),
		}, nil
	}

	if drift == checkpoint.DriftMajor {
		reuse := c.canReuseDecisionCheckpoint(caps, snapshot.LatestCheckpoint, anchor)
		return continueAssessment{
			TaskID:               taskID,
			Capsule:              caps,
			LatestRun:            snapshot.LatestRun,
			LatestCheckpoint:     snapshot.LatestCheckpoint,
			LatestHandoff:        snapshot.LatestHandoff,
			LatestLaunch:         snapshot.LatestLaunch,
			LatestAck:            snapshot.LatestAcknowledgment,
			LatestFollowThrough:  snapshot.LatestFollowThrough,
			LatestRecoveryAction: snapshot.LatestRecoveryAction,
			FreshAnchor:          anchor,
			Outcome:              ContinueOutcomeBlockedDrift,
			Reason:               "major repo drift blocks direct resume",
			Issues:               issues,
			DriftClass:           drift,
			RequiresMutation:     !reuse,
			ReuseCheckpointID:    reusableCheckpointID(snapshot.LatestCheckpoint, reuse),
		}, nil
	}
	if drift == checkpoint.DriftMinor {
		reuse := c.canReuseDecisionCheckpoint(caps, snapshot.LatestCheckpoint, anchor)
		return continueAssessment{
			TaskID:               taskID,
			Capsule:              caps,
			LatestRun:            snapshot.LatestRun,
			LatestCheckpoint:     snapshot.LatestCheckpoint,
			LatestHandoff:        snapshot.LatestHandoff,
			LatestLaunch:         snapshot.LatestLaunch,
			LatestAck:            snapshot.LatestAcknowledgment,
			LatestFollowThrough:  snapshot.LatestFollowThrough,
			LatestRecoveryAction: snapshot.LatestRecoveryAction,
			FreshAnchor:          anchor,
			Outcome:              ContinueOutcomeNeedsDecision,
			Reason:               "minor repo drift requires explicit decision",
			Issues:               issues,
			DriftClass:           drift,
			RequiresMutation:     !reuse,
			ReuseCheckpointID:    reusableCheckpointID(snapshot.LatestCheckpoint, reuse),
		}, nil
	}

	reuseSafe := c.canReuseSafeCheckpoint(caps, snapshot.LatestRun, snapshot.LatestCheckpoint, anchor)
	return continueAssessment{
		TaskID:               taskID,
		Capsule:              caps,
		LatestRun:            snapshot.LatestRun,
		LatestCheckpoint:     snapshot.LatestCheckpoint,
		LatestHandoff:        snapshot.LatestHandoff,
		LatestLaunch:         snapshot.LatestLaunch,
		LatestAck:            snapshot.LatestAcknowledgment,
		LatestFollowThrough:  snapshot.LatestFollowThrough,
		LatestRecoveryAction: snapshot.LatestRecoveryAction,
		FreshAnchor:          anchor,
		Outcome:              ContinueOutcomeSafe,
		Reason:               "safe resume is available from continuity state",
		Issues:               issues,
		DriftClass:           checkpoint.DriftNone,
		RequiresMutation:     !reuseSafe,
		ReuseCheckpointID:    reusableCheckpointID(snapshot.LatestCheckpoint, reuseSafe),
	}, nil
}

func reusableCheckpointID(cp *checkpoint.Checkpoint, ok bool) common.CheckpointID {
	if !ok || cp == nil {
		return ""
	}
	return cp.CheckpointID
}

func (c *Coordinator) finalizeContinue(ctx context.Context, assessment continueAssessment, recovery RecoveryAssessment, out *ContinueTaskResult) error {
	caps, err := c.store.Capsules().Get(assessment.TaskID)
	if err != nil {
		return err
	}
	if caps.Version != assessment.Capsule.Version {
		return c.blockedContinueByInconsistency(ctx, caps, fmt.Sprintf("task state changed during continue assessment (capsule version %d -> %d)", assessment.Capsule.Version, caps.Version), out)
	}
	caps.BranchName = assessment.FreshAnchor.Branch
	caps.HeadSHA = assessment.FreshAnchor.HeadSHA
	caps.WorkingTreeDirty = assessment.FreshAnchor.WorkingTreeDirty
	caps.AnchorCapturedAt = assessment.FreshAnchor.CapturedAt

	switch assessment.Outcome {
	case ContinueOutcomeStaleReconciled:
		if assessment.LatestRun == nil {
			return c.blockedContinueByInconsistency(ctx, caps, "stale reconciliation requested without latest run", out)
		}
		runRec, err := c.store.Runs().Get(assessment.LatestRun.RunID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return c.blockedContinueByInconsistency(ctx, caps, "latest run referenced by assessment is missing", out)
			}
			return err
		}
		if runRec.Status != rundomain.StatusRunning {
			return c.blockedContinueByInconsistency(ctx, caps, fmt.Sprintf("latest run %s is not RUNNING (status=%s)", runRec.RunID, runRec.Status), out)
		}
		return c.reconcileStaleRun(ctx, caps, runRec, out)

	case ContinueOutcomeBlockedDrift:
		return c.blockedContinueByDrift(ctx, caps, assessment.DriftClass, out)

	case ContinueOutcomeNeedsDecision:
		return c.awaitDecisionOnContinue(ctx, caps, assessment.DriftClass, out)

	case ContinueOutcomeBlockedInconsistent:
		return c.blockedContinueByInconsistency(ctx, caps, assessment.Reason, out)

	case ContinueOutcomeSafe:
		var hasCheckpoint bool
		var cp checkpoint.Checkpoint
		if assessment.LatestCheckpoint != nil {
			hasCheckpoint = true
			cp = *assessment.LatestCheckpoint
		}
		var hasRun bool
		var runRec rundomain.ExecutionRun
		if assessment.LatestRun != nil {
			hasRun = true
			runRec = *assessment.LatestRun
		}
		return c.safeContinue(ctx, caps, hasCheckpoint, cp, hasRun, runRec, recovery, out)

	default:
		return c.blockedContinueByInconsistency(ctx, caps, fmt.Sprintf("unsupported continue outcome: %s", assessment.Outcome), out)
	}
}

func (c *Coordinator) noMutationContinueResult(assessment continueAssessment, recovery RecoveryAssessment) ContinueTaskResult {
	caps := assessment.Capsule
	checkpointID := assessment.ReuseCheckpointID
	resumeDescriptor := ""
	if assessment.LatestCheckpoint != nil {
		resumeDescriptor = assessment.LatestCheckpoint.ResumeDescriptor
	}
	runID := common.RunID("")
	if assessment.LatestRun != nil {
		runID = assessment.LatestRun.RunID
	}
	base := ContinueTaskResult{
		TaskID:           caps.TaskID,
		Outcome:          assessment.Outcome,
		DriftClass:       assessment.DriftClass,
		Phase:            caps.CurrentPhase,
		RunID:            runID,
		CheckpointID:     checkpointID,
		ResumeDescriptor: resumeDescriptor,
	}
	applyRecoveryAssessmentToContinueResult(&base, recovery)
	switch assessment.Outcome {
	case ContinueOutcomeSafe:
		switch recovery.RecoveryClass {
		case RecoveryClassInterruptedRunRecoverable:
			base.CanonicalResponse = fmt.Sprintf(
				"Interrupted execution is already recoverable from checkpoint %s using brief %s on branch %s (head %s). No new checkpoint was created because the interrupted recovery state is unchanged; resume the interrupted execution path from that checkpoint.",
				checkpointID,
				caps.CurrentBriefID,
				assessment.FreshAnchor.Branch,
				assessment.FreshAnchor.HeadSHA,
			)
		case RecoveryClassAcceptedHandoffLaunchReady:
			base.CanonicalResponse = fmt.Sprintf(
				"Continuity is intact and accepted handoff %s is ready to launch. No new checkpoint was created because the handoff-based recovery state is unchanged.",
				recovery.HandoffID,
			)
		case RecoveryClassHandoffLaunchPendingOutcome:
			base.CanonicalResponse = fmt.Sprintf(
				"Continuity is intact, but handoff launch is not retryable yet: %s. No new checkpoint was created.",
				recovery.Reason,
			)
		case RecoveryClassHandoffLaunchCompleted:
			base.CanonicalResponse = fmt.Sprintf(
				"Continuity is intact, and the latest handoff launch step is already complete: %s. No new checkpoint was created.",
				recovery.Reason,
			)
		case RecoveryClassFailedRunReviewRequired:
			base.CanonicalResponse = fmt.Sprintf(
				"Continuity is intact, but the next run is not ready because latest run %s failed. Review failure evidence before retrying or regenerating the brief. No new checkpoint was created.",
				runID,
			)
		case RecoveryClassContinueExecutionRequired:
			base.CanonicalResponse = "Continuity is intact, but the current brief is not yet cleared for execution. Explicit continue finalization must happen before the next bounded run. No new checkpoint was created."
		case RecoveryClassValidationReviewRequired:
			base.CanonicalResponse = fmt.Sprintf(
				"Continuity is intact, but the task is still in validation review after run %s. Review validation state before starting another bounded run. No new checkpoint was created.",
				runID,
			)
		case RecoveryClassCompletedNoAction:
			base.CanonicalResponse = "Continuity is intact, and the task is already completed. No recovery action was taken."
		case RecoveryClassRepairRequired:
			base.CanonicalResponse = fmt.Sprintf(
				"Continuity facts are present, but deterministic recovery is not ready: %s. No new checkpoint was created.",
				recovery.Reason,
			)
		case RecoveryClassRebriefRequired:
			base.CanonicalResponse = "Continuity is intact, but the next run is blocked until the execution brief is regenerated or replaced. No new checkpoint was created."
		case RecoveryClassReadyNextRun:
			base.CanonicalResponse = fmt.Sprintf(
				"Safe resume is already available from checkpoint %s using brief %s on branch %s (head %s). No new checkpoint was created because continuity state is unchanged.",
				checkpointID,
				caps.CurrentBriefID,
				assessment.FreshAnchor.Branch,
				assessment.FreshAnchor.HeadSHA,
			)
		default:
			base.CanonicalResponse = "Continuity is intact. No new checkpoint was created because recovery state is unchanged."
		}
		return base
	case ContinueOutcomeNeedsDecision:
		base.CanonicalResponse = fmt.Sprintf(
			"Resume still requires a decision. I reused checkpoint %s and did not create a new one because the decision-gated continuity state is unchanged.",
			checkpointID,
		)
		return base
	case ContinueOutcomeBlockedDrift:
		base.CanonicalResponse = fmt.Sprintf(
			"Direct resume is still blocked by major repo drift. I reused checkpoint %s and did not create a new continuity record because state is unchanged.",
			checkpointID,
		)
		return base
	case ContinueOutcomeBlockedInconsistent:
		base.CanonicalResponse = fmt.Sprintf(
			"Resume remains blocked due to inconsistent continuity state. I reused checkpoint %s and did not create a new one because the blocked state is unchanged.",
			checkpointID,
		)
		return base
	default:
		base.CanonicalResponse = "Continue assessment completed with no state mutation."
		return base
	}
}

func (c *Coordinator) validateContinueConsistency(snapshot continuitySnapshot) (string, error) {
	violations, err := c.validateContinuitySnapshot(snapshot)
	if err != nil {
		return "", err
	}
	return firstContinuityViolationMessage(violations), nil
}

func (c *Coordinator) canReuseSafeCheckpoint(caps capsule.WorkCapsule, latestRun *rundomain.ExecutionRun, latestCheckpoint *checkpoint.Checkpoint, currentAnchor anchorgit.Snapshot) bool {
	if latestCheckpoint == nil || !latestCheckpoint.IsResumable {
		return false
	}
	if latestCheckpoint.TaskID != caps.TaskID {
		return false
	}
	if latestCheckpoint.BriefID != caps.CurrentBriefID {
		return false
	}
	if latestCheckpoint.IntentID != caps.CurrentIntentID {
		return false
	}
	if latestCheckpoint.Phase != caps.CurrentPhase {
		return false
	}
	if latestCheckpoint.CapsuleVersion != caps.Version {
		return false
	}
	if latestCheckpoint.Anchor.RepoRoot != currentAnchor.RepoRoot {
		return false
	}
	if latestCheckpoint.Anchor.BranchName != currentAnchor.Branch {
		return false
	}
	if latestCheckpoint.Anchor.HeadSHA != currentAnchor.HeadSHA {
		return false
	}
	if latestCheckpoint.Anchor.DirtyHash != boolString(currentAnchor.WorkingTreeDirty) {
		return false
	}
	if latestRun != nil && latestCheckpoint.RunID != "" && latestCheckpoint.RunID != latestRun.RunID {
		return false
	}
	return true
}

func (c *Coordinator) canReuseDecisionCheckpoint(caps capsule.WorkCapsule, latestCheckpoint *checkpoint.Checkpoint, currentAnchor anchorgit.Snapshot) bool {
	if latestCheckpoint == nil {
		return false
	}
	if latestCheckpoint.TaskID != caps.TaskID {
		return false
	}
	if latestCheckpoint.IsResumable {
		return false
	}
	if latestCheckpoint.Phase != phase.PhaseAwaitingDecision || caps.CurrentPhase != phase.PhaseAwaitingDecision {
		return false
	}
	if latestCheckpoint.Anchor.RepoRoot != currentAnchor.RepoRoot {
		return false
	}
	if latestCheckpoint.Anchor.BranchName != currentAnchor.Branch {
		return false
	}
	if latestCheckpoint.Anchor.HeadSHA != currentAnchor.HeadSHA {
		return false
	}
	if latestCheckpoint.Anchor.DirtyHash != boolString(currentAnchor.WorkingTreeDirty) {
		return false
	}
	return true
}

func (c *Coordinator) canReuseInconsistencyCheckpoint(caps capsule.WorkCapsule, latestCheckpoint *checkpoint.Checkpoint, currentAnchor anchorgit.Snapshot, reason string) bool {
	if latestCheckpoint == nil {
		return false
	}
	if latestCheckpoint.TaskID != caps.TaskID {
		return false
	}
	if latestCheckpoint.IsResumable {
		return false
	}
	if latestCheckpoint.Phase != phase.PhaseBlocked || caps.CurrentPhase != phase.PhaseBlocked {
		return false
	}
	if latestCheckpoint.Anchor.RepoRoot != currentAnchor.RepoRoot {
		return false
	}
	if latestCheckpoint.Anchor.BranchName != currentAnchor.Branch {
		return false
	}
	if latestCheckpoint.Anchor.HeadSHA != currentAnchor.HeadSHA {
		return false
	}
	if latestCheckpoint.Anchor.DirtyHash != boolString(currentAnchor.WorkingTreeDirty) {
		return false
	}
	return strings.Contains(strings.ToLower(latestCheckpoint.ResumeDescriptor), strings.ToLower(reason))
}

type preparedRealRun struct {
	TaskID  common.TaskID
	RunID   common.RunID
	Brief   brief.ExecutionBrief
	Capsule capsule.WorkCapsule
}

func (c *Coordinator) assessRunStartRecovery(ctx context.Context, taskID common.TaskID) (RecoveryAssessment, bool, string, error) {
	assessment, err := c.assessContinue(ctx, taskID)
	if err != nil {
		return RecoveryAssessment{}, false, "", err
	}
	recovery := c.recoveryFromContinueAssessment(assessment)
	allowed, canonical := runStartEligibility(recovery)
	return recovery, allowed, canonical, nil
}

func (c *Coordinator) startRunRealStaged(ctx context.Context, req RunTaskRequest) (RunTaskResult, error) {
	prepared, immediateResult, err := c.prepareRealRun(ctx, req)
	if err != nil {
		return RunTaskResult{}, err
	}
	if immediateResult != nil {
		return *immediateResult, nil
	}

	execReq := c.buildExecutionRequest(prepared)
	execResult, execErr := c.workerAdapter.Execute(ctx, execReq, nil)

	finalResult, finalizeErr := c.finalizeRealRun(ctx, prepared, execResult, execErr)
	if finalizeErr != nil {
		return RunTaskResult{}, fmt.Errorf("finalize run %s after worker execution: %w", prepared.RunID, finalizeErr)
	}
	return finalResult, nil
}

func (c *Coordinator) prepareRealRun(ctx context.Context, req RunTaskRequest) (*preparedRealRun, *RunTaskResult, error) {
	var prepared preparedRealRun
	var immediate *RunTaskResult
	err := c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(common.TaskID(req.TaskID))
		if err != nil {
			return err
		}
		if caps.CurrentBriefID == "" {
			canonical := "Execution cannot start yet because no execution brief is available. Send a task message first so Tuku can compile intent and create a brief."
			if err := txc.emitCanonicalConversation(caps, canonical, map[string]any{"reason": "missing_brief"}, nil); err != nil {
				return err
			}
			out := RunTaskResult{TaskID: caps.TaskID, Phase: caps.CurrentPhase, RunStatus: rundomain.StatusFailed, CanonicalResponse: canonical}
			immediate = &out
			return nil
		}
		recovery, allowed, canonical, err := txc.assessRunStartRecovery(ctx, caps.TaskID)
		if err != nil {
			return err
		}
		if !allowed {
			payload := map[string]any{
				"reason":                   "recovery_gate_blocked",
				"recovery_class":           recovery.RecoveryClass,
				"recommended_action":       recovery.RecommendedAction,
				"ready_for_next_run":       recovery.ReadyForNextRun,
				"ready_for_handoff_launch": recovery.ReadyForHandoffLaunch,
				"recovery_reason":          recovery.Reason,
			}
			if err := txc.emitCanonicalConversation(caps, canonical, payload, nil); err != nil {
				return err
			}
			out := RunTaskResult{TaskID: caps.TaskID, Phase: caps.CurrentPhase, RunStatus: rundomain.StatusFailed, CanonicalResponse: canonical}
			immediate = &out
			return nil
		}
		if txc.workerAdapter == nil {
			canonical := "Execution adapter is not configured. Tuku cannot run Codex in real mode yet."
			if err := txc.emitCanonicalConversation(caps, canonical, map[string]any{"reason": "missing_worker_adapter"}, nil); err != nil {
				return err
			}
			out := RunTaskResult{TaskID: caps.TaskID, Phase: caps.CurrentPhase, RunStatus: rundomain.StatusFailed, CanonicalResponse: canonical}
			immediate = &out
			return nil
		}

		b, err := txc.store.Briefs().Get(caps.CurrentBriefID)
		if err != nil {
			return err
		}
		anchor := txc.anchorProvider.Capture(ctx, caps.RepoRoot)
		caps.BranchName = anchor.Branch
		caps.HeadSHA = anchor.HeadSHA
		caps.WorkingTreeDirty = anchor.WorkingTreeDirty
		caps.AnchorCapturedAt = anchor.CapturedAt

		now := txc.clock()
		runID := req.RunID
		if runID == "" {
			runID = common.RunID(txc.idGenerator("run"))
		}
		runRec := rundomain.ExecutionRun{
			RunID:              runID,
			TaskID:             caps.TaskID,
			BriefID:            b.BriefID,
			WorkerKind:         rundomain.WorkerKindCodex,
			Status:             rundomain.StatusRunning,
			StartedAt:          now,
			CreatedFromPhase:   caps.CurrentPhase,
			LastKnownSummary:   "Codex execution started",
			CreatedAt:          now,
			UpdatedAt:          now,
			InterruptionReason: "",
		}
		if err := txc.store.Runs().Create(runRec); err != nil {
			return err
		}

		caps.Version++
		caps.UpdatedAt = txc.clock()
		caps.CurrentPhase = phase.PhaseExecuting
		caps.NextAction = "Real execution run is in progress."
		if err := txc.store.Capsules().Update(caps); err != nil {
			return err
		}
		if err := txc.appendProof(caps, proof.EventWorkerRunStarted, proof.ActorSystem, "tuku-runner", map[string]any{
			"run_id":      runID,
			"brief_id":    b.BriefID,
			"worker_kind": runRec.WorkerKind,
			"mode":        "real",
		}, &runID); err != nil {
			return err
		}
		if err := txc.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": caps.CurrentPhase, "reason": "real codex run started"}, &runID); err != nil {
			return err
		}
		if _, err := txc.createCheckpointWithOptions(caps, runID, checkpoint.TriggerBeforeExecution, true, "Run started and durably marked RUNNING before worker execution.", false); err != nil {
			return err
		}

		prepared = preparedRealRun{
			TaskID:  caps.TaskID,
			RunID:   runID,
			Brief:   b,
			Capsule: caps,
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if immediate != nil {
		return nil, immediate, nil
	}
	return &prepared, nil, nil
}

func (c *Coordinator) buildExecutionRequest(prepared *preparedRealRun) adapter_contract.ExecutionRequest {
	agentsChecksum, agentsInstructions := agentsMetadata(prepared.Capsule.RepoRoot)
	return adapter_contract.ExecutionRequest{
		RunID:  prepared.RunID,
		TaskID: prepared.TaskID,
		Worker: adapter_contract.WorkerCodex,
		Brief:  prepared.Brief,
		ContextPack: contextdomain.Pack{
			ContextPackID:      "",
			TaskID:             prepared.TaskID,
			Mode:               contextdomain.ModeCompact,
			TokenBudget:        0,
			RepoAnchorHash:     prepared.Capsule.HeadSHA,
			FreshnessState:     "current",
			IncludedFiles:      prepared.Capsule.TouchedFiles,
			IncludedSnippets:   []contextdomain.Snippet{},
			SelectionRationale: []string{"placeholder context pack for bounded milestone 4 execution"},
			PackHash:           "",
			CreatedAt:          c.clock(),
		},
		RepoAnchor: checkpoint.RepoAnchor{
			RepoRoot:      prepared.Capsule.RepoRoot,
			WorktreePath:  prepared.Capsule.WorktreePath,
			BranchName:    prepared.Capsule.BranchName,
			HeadSHA:       prepared.Capsule.HeadSHA,
			DirtyHash:     boolString(prepared.Capsule.WorkingTreeDirty),
			UntrackedHash: "",
		},
		PolicyProfileID:    prepared.Brief.PolicyProfileID,
		AgentsChecksum:     agentsChecksum,
		AgentsInstructions: agentsInstructions,
		ContextSummary:     fmt.Sprintf("phase=%s touched_files=%d", prepared.Capsule.CurrentPhase, len(prepared.Capsule.TouchedFiles)),
	}
}

func (c *Coordinator) finalizeRealRun(ctx context.Context, prepared *preparedRealRun, execResult adapter_contract.ExecutionResult, execErr error) (RunTaskResult, error) {
	var result RunTaskResult
	err := c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(prepared.TaskID)
		if err != nil {
			return err
		}
		runRec, err := txc.store.Runs().Get(prepared.RunID)
		if err != nil {
			return err
		}
		if runRec.Status != rundomain.StatusRunning {
			return fmt.Errorf("run %s is not RUNNING during finalization (status=%s)", runRec.RunID, runRec.Status)
		}

		if err := txc.appendProof(caps, proof.EventWorkerOutputCaptured, proof.ActorSystem, "tuku-runner", map[string]any{
			"run_id":                  prepared.RunID,
			"exit_code":               execResult.ExitCode,
			"started_at_unix_ms":      execResult.StartedAt.UnixMilli(),
			"ended_at_unix_ms":        execResult.EndedAt.UnixMilli(),
			"stdout_excerpt":          truncate(execResult.Stdout, 2000),
			"stderr_excerpt":          truncate(execResult.Stderr, 2000),
			"changed_files":           execResult.ChangedFiles,
			"changed_files_semantics": execResult.ChangedFilesSemantics,
			"validation_signals":      execResult.ValidationSignals,
			"summary":                 execResult.Summary,
			"error_message":           execResult.ErrorMessage,
		}, &prepared.RunID); err != nil {
			return err
		}
		if len(execResult.ChangedFiles) > 0 {
			if err := txc.appendProof(caps, proof.EventFileChangeDetected, proof.ActorSystem, "tuku-runner", map[string]any{
				"run_id":                  prepared.RunID,
				"changed_files":           execResult.ChangedFiles,
				"changed_files_semantics": execResult.ChangedFilesSemantics,
				"count":                   len(execResult.ChangedFiles),
			}, &prepared.RunID); err != nil {
				return err
			}
		}

		if execErr != nil {
			result, err = txc.markRunFailed(ctx, caps, runRec, execResult, execErr)
			return err
		}
		if execResult.ExitCode != 0 {
			result, err = txc.markRunFailed(ctx, caps, runRec, execResult, fmt.Errorf("codex exit code %d", execResult.ExitCode))
			return err
		}
		result, err = txc.markRunCompleted(ctx, caps, runRec, execResult)
		return err
	})
	if err != nil {
		return RunTaskResult{}, err
	}
	return result, nil
}

func (c *Coordinator) startRunNoop(ctx context.Context, caps capsule.WorkCapsule, req RunTaskRequest) (RunTaskResult, error) {
	if caps.CurrentBriefID == "" {
		canonical := "Execution cannot start yet because no execution brief is available. Send a task message first so Tuku can compile intent and create a brief."
		if err := c.emitCanonicalConversation(caps, canonical, map[string]any{"reason": "missing_brief"}, nil); err != nil {
			return RunTaskResult{}, err
		}
		return RunTaskResult{TaskID: caps.TaskID, Phase: caps.CurrentPhase, RunStatus: rundomain.StatusFailed, CanonicalResponse: canonical}, nil
	}
	recovery, allowed, canonical, err := c.assessRunStartRecovery(ctx, caps.TaskID)
	if err != nil {
		return RunTaskResult{}, err
	}
	if !allowed {
		payload := map[string]any{
			"reason":                   "recovery_gate_blocked",
			"recovery_class":           recovery.RecoveryClass,
			"recommended_action":       recovery.RecommendedAction,
			"ready_for_next_run":       recovery.ReadyForNextRun,
			"ready_for_handoff_launch": recovery.ReadyForHandoffLaunch,
			"recovery_reason":          recovery.Reason,
		}
		if err := c.emitCanonicalConversation(caps, canonical, payload, nil); err != nil {
			return RunTaskResult{}, err
		}
		return RunTaskResult{TaskID: caps.TaskID, Phase: caps.CurrentPhase, RunStatus: rundomain.StatusFailed, CanonicalResponse: canonical}, nil
	}

	b, err := c.store.Briefs().Get(caps.CurrentBriefID)
	if err != nil {
		return RunTaskResult{}, err
	}

	now := c.clock()
	runID := req.RunID
	if runID == "" {
		runID = common.RunID(c.idGenerator("run"))
	}
	r := rundomain.ExecutionRun{
		RunID:              runID,
		TaskID:             caps.TaskID,
		BriefID:            b.BriefID,
		WorkerKind:         rundomain.WorkerKindNoop,
		Status:             rundomain.StatusCreated,
		StartedAt:          now,
		CreatedFromPhase:   caps.CurrentPhase,
		LastKnownSummary:   "No-op run created and awaiting placeholder execution",
		CreatedAt:          now,
		UpdatedAt:          now,
		InterruptionReason: "",
	}
	if err := c.store.Runs().Create(r); err != nil {
		return RunTaskResult{}, err
	}

	r.Status = rundomain.StatusRunning
	r.LastKnownSummary = "No-op execution placeholder started"
	r.UpdatedAt = c.clock()
	if err := c.store.Runs().Update(r); err != nil {
		return RunTaskResult{}, err
	}

	caps.Version++
	caps.UpdatedAt = c.clock()
	caps.CurrentPhase = phase.PhaseExecuting
	caps.NextAction = "No-op run is active. Complete with `tuku run --task <id> --action complete` or interrupt with `--action interrupt`."
	if err := c.store.Capsules().Update(caps); err != nil {
		return RunTaskResult{}, err
	}

	if err := c.appendProof(caps, proof.EventWorkerRunStarted, proof.ActorSystem, "tuku-runner", map[string]any{"run_id": runID, "brief_id": b.BriefID, "worker_kind": r.WorkerKind, "mode": "noop"}, &runID); err != nil {
		return RunTaskResult{}, err
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": caps.CurrentPhase, "reason": "execution run started"}, &runID); err != nil {
		return RunTaskResult{}, err
	}
	if _, err := c.createCheckpoint(caps, runID, checkpoint.TriggerBeforeExecution, true, "No-op run entered RUNNING state."); err != nil {
		return RunTaskResult{}, err
	}

	if req.SimulateInterrupt {
		interruptReq := RunTaskRequest{TaskID: string(caps.TaskID), Action: "interrupt", RunID: runID, InterruptionReason: "simulated interruption"}
		return c.interruptRun(ctx, caps, interruptReq)
	}

	recentEvents, err := c.store.Proofs().ListByTask(caps.TaskID, 12)
	if err != nil {
		return RunTaskResult{}, err
	}
	canonicalText, err := c.synthesizer.Synthesize(ctx, caps, recentEvents)
	if err != nil {
		return RunTaskResult{}, err
	}
	if err := c.emitCanonicalConversation(caps, canonicalText, map[string]any{"run_id": runID, "status": r.Status}, &runID); err != nil {
		return RunTaskResult{}, err
	}

	return RunTaskResult{TaskID: caps.TaskID, RunID: runID, RunStatus: r.Status, Phase: caps.CurrentPhase, CanonicalResponse: canonicalText}, nil
}

func (c *Coordinator) completeRun(ctx context.Context, caps capsule.WorkCapsule, req RunTaskRequest) (RunTaskResult, error) {
	r, err := c.resolveRunForAction(caps.TaskID, req.RunID)
	if err != nil {
		canonical := "Execution cannot complete because there is no active run for this task."
		if emitErr := c.emitCanonicalConversation(caps, canonical, map[string]any{"reason": "missing_running_run"}, nil); emitErr != nil {
			return RunTaskResult{}, emitErr
		}
		return RunTaskResult{TaskID: caps.TaskID, Phase: caps.CurrentPhase, RunStatus: rundomain.StatusFailed, CanonicalResponse: canonical}, nil
	}

	now := c.clock()
	r.Status = rundomain.StatusCompleted
	r.LastKnownSummary = "Execution placeholder completed"
	r.EndedAt = &now
	r.UpdatedAt = now
	if err := c.store.Runs().Update(r); err != nil {
		return RunTaskResult{}, err
	}

	caps.Version++
	caps.UpdatedAt = now
	caps.CurrentPhase = phase.PhaseValidating
	caps.NextAction = "Execution placeholder completed. Validation logic is deferred to the next milestone."
	if err := c.store.Capsules().Update(caps); err != nil {
		return RunTaskResult{}, err
	}

	if err := c.appendProof(caps, proof.EventWorkerRunCompleted, proof.ActorSystem, "tuku-runner", map[string]any{"run_id": r.RunID, "status": r.Status}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": caps.CurrentPhase, "reason": "run completed"}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	if _, err := c.createCheckpoint(caps, r.RunID, checkpoint.TriggerAfterExecution, true, "Run completed and task moved to validation."); err != nil {
		return RunTaskResult{}, err
	}

	recentEvents, err := c.store.Proofs().ListByTask(caps.TaskID, 12)
	if err != nil {
		return RunTaskResult{}, err
	}
	canonicalText, err := c.synthesizer.Synthesize(ctx, caps, recentEvents)
	if err != nil {
		return RunTaskResult{}, err
	}
	if err := c.emitCanonicalConversation(caps, canonicalText, map[string]any{"run_id": r.RunID, "status": r.Status}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}

	return RunTaskResult{TaskID: caps.TaskID, RunID: r.RunID, RunStatus: r.Status, Phase: caps.CurrentPhase, CanonicalResponse: canonicalText}, nil
}

func (c *Coordinator) interruptRun(ctx context.Context, caps capsule.WorkCapsule, req RunTaskRequest) (RunTaskResult, error) {
	r, err := c.resolveRunForAction(caps.TaskID, req.RunID)
	if err != nil {
		canonical := "Execution cannot be interrupted because there is no active run for this task."
		if emitErr := c.emitCanonicalConversation(caps, canonical, map[string]any{"reason": "missing_running_run"}, nil); emitErr != nil {
			return RunTaskResult{}, emitErr
		}
		return RunTaskResult{TaskID: caps.TaskID, Phase: caps.CurrentPhase, RunStatus: rundomain.StatusFailed, CanonicalResponse: canonical}, nil
	}

	now := c.clock()
	reason := strings.TrimSpace(req.InterruptionReason)
	if reason == "" {
		reason = "manual interruption"
	}
	r.Status = rundomain.StatusInterrupted
	r.InterruptionReason = reason
	r.LastKnownSummary = "Execution placeholder interrupted"
	r.EndedAt = &now
	r.UpdatedAt = now
	if err := c.store.Runs().Update(r); err != nil {
		return RunTaskResult{}, err
	}

	caps.Version++
	caps.UpdatedAt = now
	caps.CurrentPhase = phase.PhasePaused
	caps.NextAction = "Run interrupted. Use `tuku continue --task <id>` to reconcile and resume safely."
	if err := c.store.Capsules().Update(caps); err != nil {
		return RunTaskResult{}, err
	}

	if err := c.appendProof(caps, proof.EventRunInterrupted, proof.ActorSystem, "tuku-runner", map[string]any{"run_id": r.RunID, "reason": reason}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": caps.CurrentPhase, "reason": "run interrupted"}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	if _, err := c.createCheckpoint(caps, r.RunID, checkpoint.TriggerInterruption, true, "Run interrupted and task is resumable from paused state."); err != nil {
		return RunTaskResult{}, err
	}

	recentEvents, err := c.store.Proofs().ListByTask(caps.TaskID, 12)
	if err != nil {
		return RunTaskResult{}, err
	}
	canonicalText, err := c.synthesizer.Synthesize(ctx, caps, recentEvents)
	if err != nil {
		return RunTaskResult{}, err
	}
	if err := c.emitCanonicalConversation(caps, canonicalText, map[string]any{"run_id": r.RunID, "status": r.Status, "reason": reason}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}

	return RunTaskResult{TaskID: caps.TaskID, RunID: r.RunID, RunStatus: r.Status, Phase: caps.CurrentPhase, CanonicalResponse: canonicalText}, nil
}

func (c *Coordinator) resolveRunForAction(taskID common.TaskID, preferredRunID common.RunID) (rundomain.ExecutionRun, error) {
	var runRecord rundomain.ExecutionRun
	var err error
	if preferredRunID != "" {
		runRecord, err = c.store.Runs().Get(preferredRunID)
	} else {
		runRecord, err = c.store.Runs().LatestRunningByTask(taskID)
	}
	if err != nil {
		return rundomain.ExecutionRun{}, err
	}
	if runRecord.Status != rundomain.StatusRunning {
		return rundomain.ExecutionRun{}, fmt.Errorf("run %s is not RUNNING (status=%s)", runRecord.RunID, runRecord.Status)
	}
	return runRecord, nil
}

func (c *Coordinator) markRunCompleted(ctx context.Context, caps capsule.WorkCapsule, r rundomain.ExecutionRun, execResult adapter_contract.ExecutionResult) (RunTaskResult, error) {
	now := c.clock()
	r.Status = rundomain.StatusCompleted
	r.LastKnownSummary = execResult.Summary
	r.EndedAt = &now
	r.UpdatedAt = now
	if err := c.store.Runs().Update(r); err != nil {
		return RunTaskResult{}, err
	}

	caps.Version++
	caps.UpdatedAt = now
	caps.CurrentPhase = phase.PhaseValidating
	caps.NextAction = "Codex run completed. Review evidence and decide validation/follow-up."
	if err := c.store.Capsules().Update(caps); err != nil {
		return RunTaskResult{}, err
	}

	if err := c.appendProof(caps, proof.EventWorkerRunCompleted, proof.ActorSystem, "tuku-runner", map[string]any{
		"run_id":                  r.RunID,
		"status":                  r.Status,
		"exit_code":               execResult.ExitCode,
		"changed_files":           execResult.ChangedFiles,
		"changed_files_semantics": execResult.ChangedFilesSemantics,
		"summary":                 execResult.Summary,
		"validation_hints":        execResult.ValidationSignals,
	}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": caps.CurrentPhase, "reason": "run completed"}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	if _, err := c.createCheckpoint(caps, r.RunID, checkpoint.TriggerAfterExecution, true, "Run completed with captured evidence; ready for validation follow-up."); err != nil {
		return RunTaskResult{}, err
	}

	recentEvents, err := c.store.Proofs().ListByTask(caps.TaskID, 20)
	if err != nil {
		return RunTaskResult{}, err
	}
	canonicalText, err := c.synthesizer.Synthesize(ctx, caps, recentEvents)
	if err != nil {
		return RunTaskResult{}, err
	}
	if err := c.emitCanonicalConversation(caps, canonicalText, map[string]any{"run_id": r.RunID, "status": r.Status, "exit_code": execResult.ExitCode}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	return RunTaskResult{TaskID: caps.TaskID, RunID: r.RunID, RunStatus: r.Status, Phase: caps.CurrentPhase, CanonicalResponse: canonicalText}, nil
}

func (c *Coordinator) markRunFailed(ctx context.Context, caps capsule.WorkCapsule, r rundomain.ExecutionRun, execResult adapter_contract.ExecutionResult, runErr error) (RunTaskResult, error) {
	now := c.clock()
	if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
		r.Status = rundomain.StatusInterrupted
		r.InterruptionReason = runErr.Error()
		r.LastKnownSummary = "Codex run interrupted"
		r.EndedAt = &now
		r.UpdatedAt = now
		if err := c.store.Runs().Update(r); err != nil {
			return RunTaskResult{}, err
		}
		caps.Version++
		caps.UpdatedAt = now
		caps.CurrentPhase = phase.PhasePaused
		caps.NextAction = "Codex run was interrupted. Check execution evidence and retry."
		if err := c.store.Capsules().Update(caps); err != nil {
			return RunTaskResult{}, err
		}
		if err := c.appendProof(caps, proof.EventRunInterrupted, proof.ActorSystem, "tuku-runner", map[string]any{"run_id": r.RunID, "reason": runErr.Error()}, &r.RunID); err != nil {
			return RunTaskResult{}, err
		}
		if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": caps.CurrentPhase, "reason": "run interrupted"}, &r.RunID); err != nil {
			return RunTaskResult{}, err
		}
		if _, err := c.createCheckpoint(caps, r.RunID, checkpoint.TriggerInterruption, true, "Run interrupted during execution; resumable from paused phase."); err != nil {
			return RunTaskResult{}, err
		}
		recentEvents, err := c.store.Proofs().ListByTask(caps.TaskID, 20)
		if err != nil {
			return RunTaskResult{}, err
		}
		canonicalText, err := c.synthesizer.Synthesize(ctx, caps, recentEvents)
		if err != nil {
			return RunTaskResult{}, err
		}
		if err := c.emitCanonicalConversation(caps, canonicalText, map[string]any{"run_id": r.RunID, "status": r.Status, "reason": runErr.Error()}, &r.RunID); err != nil {
			return RunTaskResult{}, err
		}
		return RunTaskResult{TaskID: caps.TaskID, RunID: r.RunID, RunStatus: r.Status, Phase: caps.CurrentPhase, CanonicalResponse: canonicalText}, nil
	}

	r.Status = rundomain.StatusFailed
	r.LastKnownSummary = fmt.Sprintf("Codex run failed: %s", execResult.Summary)
	r.EndedAt = &now
	r.UpdatedAt = now
	if err := c.store.Runs().Update(r); err != nil {
		return RunTaskResult{}, err
	}

	caps.Version++
	caps.UpdatedAt = now
	caps.CurrentPhase = phase.PhaseBlocked
	caps.NextAction = "Codex run failed. Inspect proof evidence and adjust brief or constraints."
	if err := c.store.Capsules().Update(caps); err != nil {
		return RunTaskResult{}, err
	}

	if err := c.appendProof(caps, proof.EventWorkerRunFailed, proof.ActorSystem, "tuku-runner", map[string]any{
		"run_id":                  r.RunID,
		"error":                   runErr.Error(),
		"exit_code":               execResult.ExitCode,
		"summary":                 execResult.Summary,
		"stderr_excerpt":          truncate(execResult.Stderr, 2000),
		"changed_files":           execResult.ChangedFiles,
		"changed_files_semantics": execResult.ChangedFilesSemantics,
	}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": caps.CurrentPhase, "reason": "run failed"}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	if _, err := c.createCheckpoint(caps, r.RunID, checkpoint.TriggerAfterExecution, false, "Run failed with evidence captured; inspect failure evidence before retrying or regenerating the brief."); err != nil {
		return RunTaskResult{}, err
	}

	recentEvents, err := c.store.Proofs().ListByTask(caps.TaskID, 20)
	if err != nil {
		return RunTaskResult{}, err
	}
	canonicalText, err := c.synthesizer.Synthesize(ctx, caps, recentEvents)
	if err != nil {
		return RunTaskResult{}, err
	}
	if err := c.emitCanonicalConversation(caps, canonicalText, map[string]any{"run_id": r.RunID, "status": r.Status, "error": runErr.Error()}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	return RunTaskResult{TaskID: caps.TaskID, RunID: r.RunID, RunStatus: r.Status, Phase: caps.CurrentPhase, CanonicalResponse: canonicalText}, nil
}

func (c *Coordinator) StatusTask(ctx context.Context, taskID string) (StatusTaskResult, error) {
	caps, err := c.store.Capsules().Get(common.TaskID(taskID))
	if err != nil {
		return StatusTaskResult{}, err
	}

	status := StatusTaskResult{
		TaskID:          caps.TaskID,
		ConversationID:  caps.ConversationID,
		Goal:            caps.Goal,
		Phase:           caps.CurrentPhase,
		Status:          caps.Status,
		CurrentIntentID: caps.CurrentIntentID,
		CurrentBriefID:  caps.CurrentBriefID,
		RepoAnchor: anchorgit.Snapshot{
			RepoRoot:         caps.RepoRoot,
			Branch:           caps.BranchName,
			HeadSHA:          caps.HeadSHA,
			WorkingTreeDirty: caps.WorkingTreeDirty,
			CapturedAt:       caps.AnchorCapturedAt,
		},
	}

	intentState, err := c.store.Intents().LatestByTask(caps.TaskID)
	if err == nil {
		status.CurrentIntentClass = intentState.Class
		status.CurrentIntentSummary = intentState.NormalizedAction
	} else if !errors.Is(err, sql.ErrNoRows) {
		return StatusTaskResult{}, err
	}

	if caps.CurrentBriefID != "" {
		b, err := c.store.Briefs().Get(caps.CurrentBriefID)
		if err == nil {
			status.CurrentBriefHash = b.BriefHash
		} else if !errors.Is(err, sql.ErrNoRows) {
			return StatusTaskResult{}, err
		}
	}

	if latestRun, err := c.store.Runs().LatestByTask(caps.TaskID); err == nil {
		status.LatestRunID = latestRun.RunID
		status.LatestRunStatus = latestRun.Status
		status.LatestRunSummary = latestRun.LastKnownSummary
	} else if !errors.Is(err, sql.ErrNoRows) {
		return StatusTaskResult{}, err
	}

	checkpointResumable := false
	if latestCheckpoint, err := c.store.Checkpoints().LatestByTask(caps.TaskID); err == nil {
		status.LatestCheckpointID = latestCheckpoint.CheckpointID
		status.LatestCheckpointAt = latestCheckpoint.CreatedAt
		status.LatestCheckpointTrigger = latestCheckpoint.Trigger
		status.ResumeDescriptor = latestCheckpoint.ResumeDescriptor
		checkpointResumable = latestCheckpoint.IsResumable
	} else if !errors.Is(err, sql.ErrNoRows) {
		return StatusTaskResult{}, err
	}

	var latestPacket *handoff.Packet
	var latestLaunch *handoff.Launch
	var latestAck *handoff.Acknowledgment
	var latestFollowThrough *handoff.FollowThrough
	if packet, err := c.store.Handoffs().LatestByTask(caps.TaskID); err == nil {
		packetCopy := packet
		latestPacket = &packetCopy
		if launch, err := c.store.Handoffs().LatestLaunchByHandoff(packet.HandoffID); err == nil {
			launchCopy := launch
			latestLaunch = &launchCopy
			status.LatestLaunchAttemptID = launch.AttemptID
			status.LatestLaunchID = launch.LaunchID
			status.LatestLaunchStatus = launch.Status
			control := assessLaunchControl(caps.TaskID, latestPacket, latestLaunch)
			status.LaunchControlState = control.State
			status.LaunchRetryDisposition = control.RetryDisposition
			status.LaunchControlReason = control.Reason
		} else if !errors.Is(err, sql.ErrNoRows) {
			return StatusTaskResult{}, err
		} else {
			control := assessLaunchControl(caps.TaskID, latestPacket, nil)
			status.LaunchControlState = control.State
			status.LaunchRetryDisposition = control.RetryDisposition
			status.LaunchControlReason = control.Reason
		}
		if ack, err := c.store.Handoffs().LatestAcknowledgment(packet.HandoffID); err == nil {
			ackCopy := ack
			latestAck = &ackCopy
			status.LatestAcknowledgmentID = ack.AckID
			status.LatestAcknowledgmentStatus = ack.Status
			status.LatestAcknowledgmentSummary = ack.Summary
		} else if !errors.Is(err, sql.ErrNoRows) {
			return StatusTaskResult{}, err
		}
		if record, err := c.store.Handoffs().LatestFollowThrough(packet.HandoffID); err == nil {
			recordCopy := record
			latestFollowThrough = &recordCopy
			status.LatestFollowThroughID = record.RecordID
			status.LatestFollowThroughKind = record.Kind
			status.LatestFollowThroughSummary = record.Summary
		} else if !errors.Is(err, sql.ErrNoRows) {
			return StatusTaskResult{}, err
		}
		handoffContinuity := assessHandoffContinuity(caps.TaskID, latestPacket, latestLaunch, latestAck, latestFollowThrough)
		status.HandoffContinuityState = handoffContinuity.State
		status.HandoffContinuityReason = handoffContinuity.Reason
		status.HandoffContinuationProven = handoffContinuity.DownstreamContinuationProven
	} else if !errors.Is(err, sql.ErrNoRows) {
		return StatusTaskResult{}, err
	}

	if assessment, err := c.assessContinue(ctx, caps.TaskID); err != nil {
		return StatusTaskResult{}, err
	} else {
		recovery := c.recoveryFromContinueAssessment(assessment)
		applyRecoveryAssessmentToStatus(&status, recovery, checkpointResumable)
	}

	events, err := c.store.Proofs().ListByTask(caps.TaskID, 1)
	if err == nil && len(events) > 0 {
		last := events[len(events)-1]
		status.LastEventID = last.EventID
		status.LastEventType = last.Type
		status.LastEventAt = last.Timestamp
	} else if err != nil {
		return StatusTaskResult{}, err
	}

	return status, nil
}

func (c *Coordinator) InspectTask(ctx context.Context, taskID string) (InspectTaskResult, error) {
	caps, err := c.store.Capsules().Get(common.TaskID(taskID))
	if err != nil {
		return InspectTaskResult{}, err
	}
	out := InspectTaskResult{
		TaskID: caps.TaskID,
		RepoAnchor: anchorgit.Snapshot{
			RepoRoot:         caps.RepoRoot,
			Branch:           caps.BranchName,
			HeadSHA:          caps.HeadSHA,
			WorkingTreeDirty: caps.WorkingTreeDirty,
			CapturedAt:       caps.AnchorCapturedAt,
		},
	}

	if in, err := c.store.Intents().LatestByTask(caps.TaskID); err == nil {
		inCopy := in
		out.Intent = &inCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return InspectTaskResult{}, err
	}

	if caps.CurrentBriefID != "" {
		b, err := c.store.Briefs().Get(caps.CurrentBriefID)
		if err == nil {
			briefCopy := b
			out.Brief = &briefCopy
		} else if !errors.Is(err, sql.ErrNoRows) {
			return InspectTaskResult{}, err
		}
	}

	if latestRun, err := c.store.Runs().LatestByTask(caps.TaskID); err == nil {
		runCopy := latestRun
		out.Run = &runCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return InspectTaskResult{}, err
	}

	if latestCheckpoint, err := c.store.Checkpoints().LatestByTask(caps.TaskID); err == nil {
		cpCopy := latestCheckpoint
		out.Checkpoint = &cpCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return InspectTaskResult{}, err
	}
	if latestHandoff, err := c.store.Handoffs().LatestByTask(caps.TaskID); err == nil {
		packetCopy := latestHandoff
		out.Handoff = &packetCopy
		if latestLaunch, err := c.store.Handoffs().LatestLaunchByHandoff(latestHandoff.HandoffID); err == nil {
			launchCopy := latestLaunch
			out.Launch = &launchCopy
			control := assessLaunchControl(caps.TaskID, out.Handoff, out.Launch)
			out.LaunchControl = &control
		} else if !errors.Is(err, sql.ErrNoRows) {
			return InspectTaskResult{}, err
		} else {
			control := assessLaunchControl(caps.TaskID, out.Handoff, nil)
			out.LaunchControl = &control
		}
		if latestAck, err := c.store.Handoffs().LatestAcknowledgment(latestHandoff.HandoffID); err == nil {
			ackCopy := latestAck
			out.Acknowledgment = &ackCopy
		} else if !errors.Is(err, sql.ErrNoRows) {
			return InspectTaskResult{}, err
		}
		if latestFollowThrough, err := c.store.Handoffs().LatestFollowThrough(latestHandoff.HandoffID); err == nil {
			recordCopy := latestFollowThrough
			out.FollowThrough = &recordCopy
		} else if !errors.Is(err, sql.ErrNoRows) {
			return InspectTaskResult{}, err
		}
		continuity := assessHandoffContinuity(caps.TaskID, out.Handoff, out.Launch, out.Acknowledgment, out.FollowThrough)
		out.HandoffContinuity = &continuity
	} else if !errors.Is(err, sql.ErrNoRows) {
		return InspectTaskResult{}, err
	}
	if latestAction, err := c.store.RecoveryActions().LatestByTask(caps.TaskID); err == nil {
		actionCopy := latestAction
		out.LatestRecoveryAction = &actionCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return InspectTaskResult{}, err
	}
	if actions, err := c.store.RecoveryActions().ListByTask(caps.TaskID, 5); err == nil {
		out.RecentRecoveryActions = append([]recoveryaction.Record{}, actions...)
	} else {
		return InspectTaskResult{}, err
	}
	if assessment, err := c.assessContinue(ctx, caps.TaskID); err != nil {
		return InspectTaskResult{}, err
	} else {
		recovery := c.recoveryFromContinueAssessment(assessment)
		out.Recovery = &recovery
		if recovery.LatestAction != nil {
			actionCopy := *recovery.LatestAction
			out.LatestRecoveryAction = &actionCopy
		}
	}

	return out, nil
}

func (c *Coordinator) reconcileStaleRun(ctx context.Context, caps capsule.WorkCapsule, latestRun rundomain.ExecutionRun, out *ContinueTaskResult) error {
	now := c.clock()
	latestRun.Status = rundomain.StatusInterrupted
	latestRun.InterruptionReason = "stale RUNNING reconciled during continue: no active execution handle"
	latestRun.LastKnownSummary = "Reconciled stale RUNNING run as INTERRUPTED"
	latestRun.EndedAt = &now
	latestRun.UpdatedAt = now
	if err := c.store.Runs().Update(latestRun); err != nil {
		return err
	}

	caps.Version++
	caps.UpdatedAt = now
	caps.CurrentPhase = phase.PhasePaused
	caps.NextAction = "A stale RUNNING run was reconciled to INTERRUPTED. Review evidence and restart execution when ready."
	if err := c.store.Capsules().Update(caps); err != nil {
		return err
	}
	if err := c.appendProof(caps, proof.EventRunInterrupted, proof.ActorSystem, "tuku-daemon", map[string]any{
		"run_id":              latestRun.RunID,
		"reason":              latestRun.InterruptionReason,
		"reconciliation":      true,
		"previous_run_status": rundomain.StatusRunning,
	}, &latestRun.RunID); err != nil {
		return err
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{
		"phase":  caps.CurrentPhase,
		"reason": "stale running run reconciled on continue",
	}, &latestRun.RunID); err != nil {
		return err
	}
	cp, err := c.createCheckpoint(caps, latestRun.RunID, checkpoint.TriggerInterruption, true, "Stale RUNNING run reconciled to INTERRUPTED; resumable from paused state.")
	if err != nil {
		return err
	}

	canonical := fmt.Sprintf(
		"I found run %s still marked RUNNING but no active execution handle was present. I reconciled it as INTERRUPTED and created resumable checkpoint %s. Resume the interrupted execution path from brief %s.",
		latestRun.RunID,
		cp.CheckpointID,
		caps.CurrentBriefID,
	)
	if err := c.emitCanonicalConversation(caps, canonical, map[string]any{
		"outcome":       ContinueOutcomeStaleReconciled,
		"run_id":        latestRun.RunID,
		"checkpoint_id": cp.CheckpointID,
	}, &latestRun.RunID); err != nil {
		return err
	}

	*out = ContinueTaskResult{
		TaskID:            caps.TaskID,
		Outcome:           ContinueOutcomeStaleReconciled,
		DriftClass:        checkpoint.DriftNone,
		Phase:             caps.CurrentPhase,
		RunID:             latestRun.RunID,
		CheckpointID:      cp.CheckpointID,
		ResumeDescriptor:  cp.ResumeDescriptor,
		CanonicalResponse: canonical,
	}
	applyRecoveryAssessmentToContinueResult(out, RecoveryAssessment{
		TaskID:            caps.TaskID,
		ContinuityOutcome: ContinueOutcomeSafe,
		RecoveryClass:     RecoveryClassInterruptedRunRecoverable,
		RecommendedAction: RecoveryActionResumeInterrupted,
		ReadyForNextRun:   false,
		Reason:            fmt.Sprintf("stale run %s was reconciled and is now recoverable from checkpoint %s", latestRun.RunID, cp.CheckpointID),
		CheckpointID:      cp.CheckpointID,
		RunID:             latestRun.RunID,
	})
	return nil
}

func (c *Coordinator) blockedContinueByDrift(_ context.Context, caps capsule.WorkCapsule, drift checkpoint.DriftClass, out *ContinueTaskResult) error {
	now := c.clock()
	caps.Version++
	caps.UpdatedAt = now
	caps.CurrentPhase = phase.PhaseAwaitingDecision
	caps.NextAction = "Direct resume is blocked by major repo drift. Re-anchor or create a new brief before executing."
	if err := c.store.Capsules().Update(caps); err != nil {
		return err
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{
		"phase":  caps.CurrentPhase,
		"reason": "major anchor drift blocked resume",
	}, nil); err != nil {
		return err
	}
	cp, err := c.createCheckpoint(caps, "", checkpoint.TriggerAwaitingDecision, false, "Major repo drift detected. Direct resume blocked pending user decision.")
	if err != nil {
		return err
	}
	canonical := fmt.Sprintf(
		"Direct resume is not safe. I detected major repo drift versus the last continuity anchor, so I blocked automatic resume and recorded checkpoint %s for decision review.",
		cp.CheckpointID,
	)
	if err := c.emitCanonicalConversation(caps, canonical, map[string]any{
		"outcome":       ContinueOutcomeBlockedDrift,
		"drift_class":   drift,
		"checkpoint_id": cp.CheckpointID,
	}, nil); err != nil {
		return err
	}
	*out = ContinueTaskResult{
		TaskID:            caps.TaskID,
		Outcome:           ContinueOutcomeBlockedDrift,
		DriftClass:        drift,
		Phase:             caps.CurrentPhase,
		CheckpointID:      cp.CheckpointID,
		ResumeDescriptor:  cp.ResumeDescriptor,
		CanonicalResponse: canonical,
	}
	applyRecoveryAssessmentToContinueResult(out, RecoveryAssessment{
		TaskID:            caps.TaskID,
		ContinuityOutcome: ContinueOutcomeBlockedDrift,
		RecoveryClass:     RecoveryClassBlockedDrift,
		RecommendedAction: RecoveryActionMakeResumeDecision,
		DriftClass:        drift,
		RequiresDecision:  true,
		Reason:            "repository drift blocks automatic recovery",
		CheckpointID:      cp.CheckpointID,
	})
	return nil
}

func (c *Coordinator) blockedContinueByInconsistency(_ context.Context, caps capsule.WorkCapsule, reason string, out *ContinueTaskResult) error {
	now := c.clock()
	caps.Version++
	caps.UpdatedAt = now
	caps.CurrentPhase = phase.PhaseBlocked
	caps.NextAction = "Continuity state is inconsistent. Re-anchor state or regenerate intent/brief before continuing."
	if err := c.store.Capsules().Update(caps); err != nil {
		return err
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{
		"phase":  caps.CurrentPhase,
		"reason": "continue blocked by inconsistent continuity state",
	}, nil); err != nil {
		return err
	}
	cp, err := c.createCheckpoint(caps, "", checkpoint.TriggerAwaitingDecision, false, fmt.Sprintf("Continue blocked by inconsistent continuity state: %s", reason))
	if err != nil {
		return err
	}
	canonical := fmt.Sprintf(
		"Resume is blocked because continuity state is inconsistent: %s. I recorded checkpoint %s for explicit recovery decisions.",
		reason,
		cp.CheckpointID,
	)
	if err := c.emitCanonicalConversation(caps, canonical, map[string]any{
		"outcome":       ContinueOutcomeBlockedInconsistent,
		"reason":        reason,
		"checkpoint_id": cp.CheckpointID,
	}, nil); err != nil {
		return err
	}
	*out = ContinueTaskResult{
		TaskID:            caps.TaskID,
		Outcome:           ContinueOutcomeBlockedInconsistent,
		DriftClass:        checkpoint.DriftNone,
		Phase:             caps.CurrentPhase,
		CheckpointID:      cp.CheckpointID,
		ResumeDescriptor:  cp.ResumeDescriptor,
		CanonicalResponse: canonical,
	}
	applyRecoveryAssessmentToContinueResult(out, RecoveryAssessment{
		TaskID:            caps.TaskID,
		ContinuityOutcome: ContinueOutcomeBlockedInconsistent,
		RecoveryClass:     RecoveryClassRepairRequired,
		RecommendedAction: RecoveryActionRepairContinuity,
		RequiresRepair:    true,
		Reason:            reason,
		CheckpointID:      cp.CheckpointID,
	})
	return nil
}

func (c *Coordinator) awaitDecisionOnContinue(_ context.Context, caps capsule.WorkCapsule, drift checkpoint.DriftClass, out *ContinueTaskResult) error {
	now := c.clock()
	caps.Version++
	caps.UpdatedAt = now
	caps.CurrentPhase = phase.PhaseAwaitingDecision
	caps.NextAction = "Minor repo drift detected. Confirm whether to continue with the existing brief or regenerate intent/brief."
	if err := c.store.Capsules().Update(caps); err != nil {
		return err
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{
		"phase":  caps.CurrentPhase,
		"reason": "minor anchor drift requires decision",
	}, nil); err != nil {
		return err
	}
	cp, err := c.createCheckpoint(caps, "", checkpoint.TriggerAwaitingDecision, false, "Minor repo drift detected. Awaiting explicit decision before resume.")
	if err != nil {
		return err
	}
	canonical := fmt.Sprintf(
		"I found minor repo drift since the last checkpoint. I paused at decision state and created checkpoint %s. Confirm whether to continue with brief %s or regenerate the brief.",
		cp.CheckpointID,
		caps.CurrentBriefID,
	)
	if err := c.emitCanonicalConversation(caps, canonical, map[string]any{
		"outcome":       ContinueOutcomeNeedsDecision,
		"drift_class":   drift,
		"checkpoint_id": cp.CheckpointID,
	}, nil); err != nil {
		return err
	}
	*out = ContinueTaskResult{
		TaskID:            caps.TaskID,
		Outcome:           ContinueOutcomeNeedsDecision,
		DriftClass:        drift,
		Phase:             caps.CurrentPhase,
		CheckpointID:      cp.CheckpointID,
		ResumeDescriptor:  cp.ResumeDescriptor,
		CanonicalResponse: canonical,
	}
	applyRecoveryAssessmentToContinueResult(out, RecoveryAssessment{
		TaskID:            caps.TaskID,
		ContinuityOutcome: ContinueOutcomeNeedsDecision,
		RecoveryClass:     RecoveryClassDecisionRequired,
		RecommendedAction: RecoveryActionMakeResumeDecision,
		DriftClass:        drift,
		RequiresDecision:  true,
		Reason:            "resume requires an explicit operator decision",
		CheckpointID:      cp.CheckpointID,
	})
	return nil
}

func (c *Coordinator) safeContinue(_ context.Context, caps capsule.WorkCapsule, hasCheckpoint bool, latestCheckpoint checkpoint.Checkpoint, hasRun bool, latestRun rundomain.ExecutionRun, recovery RecoveryAssessment, out *ContinueTaskResult) error {
	now := c.clock()
	if caps.CurrentBriefID == "" {
		canonical := "Resume is blocked because no execution brief exists for this task. Send a task message to compile intent and generate a brief first."
		if err := c.emitCanonicalConversation(caps, canonical, map[string]any{
			"outcome": ContinueOutcomeBlockedInconsistent,
			"reason":  "missing_brief",
		}, nil); err != nil {
			return err
		}
		*out = ContinueTaskResult{
			TaskID:            caps.TaskID,
			Outcome:           ContinueOutcomeBlockedInconsistent,
			DriftClass:        checkpoint.DriftNone,
			Phase:             caps.CurrentPhase,
			CanonicalResponse: canonical,
		}
		return nil
	}
	if _, err := c.store.Briefs().Get(caps.CurrentBriefID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			canonical := "Resume is blocked because capsule state references a missing execution brief. Recompile intent to restore continuity."
			if emitErr := c.emitCanonicalConversation(caps, canonical, map[string]any{
				"outcome": ContinueOutcomeBlockedInconsistent,
				"reason":  "brief_pointer_missing",
			}, nil); emitErr != nil {
				return emitErr
			}
			*out = ContinueTaskResult{
				TaskID:            caps.TaskID,
				Outcome:           ContinueOutcomeBlockedInconsistent,
				DriftClass:        checkpoint.DriftNone,
				Phase:             caps.CurrentPhase,
				CanonicalResponse: canonical,
			}
			return nil
		}
		return err
	}

	runID := common.RunID("")
	if hasRun {
		runID = latestRun.RunID
	}

	caps.Version++
	caps.UpdatedAt = now
	caps.NextAction = "Resume is safe. Follow the current recovery posture before starting another bounded run."
	if err := c.store.Capsules().Update(caps); err != nil {
		return err
	}

	descriptor := "Safe resume available from current capsule and checkpoint state."
	trigger := checkpoint.TriggerContinue
	if hasCheckpoint {
		descriptor = fmt.Sprintf("Safe resume confirmed from checkpoint %s.", latestCheckpoint.CheckpointID)
	}
	cp, err := c.createCheckpoint(caps, runID, trigger, true, descriptor)
	if err != nil {
		return err
	}
	canonical := fmt.Sprintf(
		"Safe resume is available. Use checkpoint %s with brief %s on branch %s (head %s) to continue with a bounded run.",
		cp.CheckpointID,
		caps.CurrentBriefID,
		caps.BranchName,
		caps.HeadSHA,
	)
	if recovery.RecoveryClass == RecoveryClassInterruptedRunRecoverable {
		caps.NextAction = "Interrupted recovery is available. Resume the interrupted execution path from the recoverable checkpoint."
		if err := c.store.Capsules().Update(caps); err != nil {
			return err
		}
		canonical = fmt.Sprintf(
			"Interrupted execution is recoverable. Use checkpoint %s with brief %s on branch %s (head %s) to resume the interrupted execution path.",
			cp.CheckpointID,
			caps.CurrentBriefID,
			caps.BranchName,
			caps.HeadSHA,
		)
	}
	if err := c.emitCanonicalConversation(caps, canonical, map[string]any{
		"outcome":            ContinueOutcomeSafe,
		"checkpoint_id":      cp.CheckpointID,
		"brief_id":           caps.CurrentBriefID,
		"recovery_class":     recovery.RecoveryClass,
		"recommended_action": recovery.RecommendedAction,
	}, runIDPointer(runID)); err != nil {
		return err
	}

	*out = ContinueTaskResult{
		TaskID:            caps.TaskID,
		Outcome:           ContinueOutcomeSafe,
		DriftClass:        checkpoint.DriftNone,
		Phase:             caps.CurrentPhase,
		RunID:             runID,
		CheckpointID:      cp.CheckpointID,
		ResumeDescriptor:  cp.ResumeDescriptor,
		CanonicalResponse: canonical,
	}
	applyRecoveryAssessmentToContinueResult(out, recovery)
	return nil
}

func (c *Coordinator) createCheckpoint(caps capsule.WorkCapsule, runID common.RunID, trigger checkpoint.Trigger, resumable bool, descriptor string) (checkpoint.Checkpoint, error) {
	return c.createCheckpointWithOptions(caps, runID, trigger, resumable, descriptor, true)
}

func (c *Coordinator) createCheckpointWithOptions(caps capsule.WorkCapsule, runID common.RunID, trigger checkpoint.Trigger, resumable bool, descriptor string, emitProof bool) (checkpoint.Checkpoint, error) {
	lastEventID, err := c.latestProofEventID(caps.TaskID)
	if err != nil {
		return checkpoint.Checkpoint{}, err
	}
	if strings.TrimSpace(descriptor) == "" {
		descriptor = "Checkpoint captured for continuity."
	}
	cp := checkpoint.Checkpoint{
		Version:            1,
		CheckpointID:       common.CheckpointID(c.idGenerator("chk")),
		TaskID:             caps.TaskID,
		RunID:              runID,
		CreatedAt:          c.clock(),
		Trigger:            trigger,
		CapsuleVersion:     caps.Version,
		Phase:              caps.CurrentPhase,
		Anchor:             anchorFromCapsule(caps),
		IntentID:           caps.CurrentIntentID,
		BriefID:            caps.CurrentBriefID,
		ContextPackID:      "",
		LastEventID:        lastEventID,
		PendingDecisionIDs: []common.DecisionID{},
		ResumeDescriptor:   descriptor,
		IsResumable:        resumable,
	}
	if err := c.store.Checkpoints().Create(cp); err != nil {
		return checkpoint.Checkpoint{}, err
	}
	if emitProof {
		if err := c.appendCheckpointCreatedProof(caps, cp, runIDPointer(runID)); err != nil {
			return checkpoint.Checkpoint{}, err
		}
	}
	return cp, nil
}

func (c *Coordinator) appendCheckpointCreatedProof(caps capsule.WorkCapsule, cp checkpoint.Checkpoint, runID *common.RunID) error {
	checkpointID := cp.CheckpointID
	event := proof.Event{
		EventID:        common.EventID(c.idGenerator("evt")),
		TaskID:         caps.TaskID,
		RunID:          runID,
		CheckpointID:   &checkpointID,
		Timestamp:      c.clock(),
		Type:           proof.EventCheckpointCreated,
		ActorType:      proof.ActorSystem,
		ActorID:        "tuku-daemon",
		PayloadJSON:    mustJSON(map[string]any{"checkpoint_id": cp.CheckpointID, "trigger": cp.Trigger, "resumable": cp.IsResumable, "descriptor": cp.ResumeDescriptor}),
		CapsuleVersion: caps.Version,
	}
	return c.store.Proofs().Append(event)
}

func (c *Coordinator) latestProofEventID(taskID common.TaskID) (common.EventID, error) {
	events, err := c.store.Proofs().ListByTask(taskID, 1)
	if err != nil {
		return "", err
	}
	if len(events) == 0 {
		return "", nil
	}
	return events[len(events)-1].EventID, nil
}

func anchorFromCapsule(caps capsule.WorkCapsule) checkpoint.RepoAnchor {
	return checkpoint.RepoAnchor{
		RepoRoot:      caps.RepoRoot,
		WorktreePath:  caps.WorktreePath,
		BranchName:    caps.BranchName,
		HeadSHA:       caps.HeadSHA,
		DirtyHash:     boolString(caps.WorkingTreeDirty),
		UntrackedHash: "",
	}
}

func classifyAnchorDrift(baseline checkpoint.RepoAnchor, current anchorgit.Snapshot) checkpoint.DriftClass {
	if strings.TrimSpace(current.RepoRoot) == "" {
		return checkpoint.DriftMajor
	}
	if strings.TrimSpace(baseline.RepoRoot) != "" && filepath.Clean(baseline.RepoRoot) != filepath.Clean(current.RepoRoot) {
		return checkpoint.DriftMajor
	}
	if strings.TrimSpace(baseline.WorktreePath) != "" && filepath.Clean(baseline.WorktreePath) != filepath.Clean(current.RepoRoot) {
		return checkpoint.DriftMajor
	}
	if strings.TrimSpace(baseline.BranchName) != "" && strings.TrimSpace(current.Branch) != "" && baseline.BranchName != current.Branch {
		return checkpoint.DriftMajor
	}
	if strings.TrimSpace(baseline.HeadSHA) != "" && strings.TrimSpace(current.HeadSHA) != "" && baseline.HeadSHA != current.HeadSHA {
		return checkpoint.DriftMinor
	}
	if strings.TrimSpace(baseline.DirtyHash) != "" && baseline.DirtyHash != boolString(current.WorkingTreeDirty) {
		return checkpoint.DriftMinor
	}
	return checkpoint.DriftNone
}

func runIDPointer(runID common.RunID) *common.RunID {
	if runID == "" {
		return nil
	}
	id := runID
	return &id
}

func (c *Coordinator) withTx(fn func(txc *Coordinator) error) error {
	return c.store.WithTx(func(txStore storage.Store) error {
		txc := *c
		txc.store = txStore
		return fn(&txc)
	})
}

func (c *Coordinator) appendProof(caps capsule.WorkCapsule, eventType proof.EventType, actorType proof.ActorType, actorID string, payload map[string]any, runID *common.RunID) error {
	e := proof.Event{
		EventID:        common.EventID(c.idGenerator("evt")),
		TaskID:         caps.TaskID,
		RunID:          runID,
		Timestamp:      c.clock(),
		Type:           eventType,
		ActorType:      actorType,
		ActorID:        actorID,
		PayloadJSON:    mustJSON(payload),
		CapsuleVersion: caps.Version,
	}
	return c.store.Proofs().Append(e)
}

func (c *Coordinator) emitCanonicalConversation(caps capsule.WorkCapsule, canonicalText string, payload map[string]any, runID *common.RunID) error {
	systemMsg := conversation.Message{
		MessageID:      common.MessageID(c.idGenerator("msg")),
		ConversationID: caps.ConversationID,
		TaskID:         caps.TaskID,
		Role:           conversation.RoleSystem,
		Body:           canonicalText,
		CreatedAt:      c.clock(),
	}
	if err := c.store.Conversations().Append(systemMsg); err != nil {
		return err
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["message_id"] = systemMsg.MessageID
	return c.appendProof(caps, proof.EventCanonicalResponseEmitted, proof.ActorSystem, "tuku-daemon", payload, runID)
}

func agentsMetadata(repoRoot string) (checksum string, instructions string) {
	path := filepath.Join(repoRoot, "AGENTS.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	sum := sha256.Sum256(data)
	checksum = hexString(sum[:])
	lines := strings.Split(string(data), "\n")
	selected := make([]string, 0, 6)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		selected = append(selected, line)
		if len(selected) >= 6 {
			break
		}
	}
	return checksum, strings.Join(selected, " | ")
}

func boolString(v bool) string {
	if v {
		return "dirty"
	}
	return "clean"
}

func truncate(value string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max]) + "...(truncated)"
}

func hexString(bytes []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(bytes)*2)
	for i, b := range bytes {
		out[i*2] = hexdigits[b>>4]
		out[i*2+1] = hexdigits[b&0x0f]
	}
	return string(out)
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func newID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UTC().UnixNano())
}

```


## internal/orchestrator/shell.go

```go
package orchestrator

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"tuku/internal/domain/brief"
	"tuku/internal/domain/capsule"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/conversation"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/intent"
	"tuku/internal/domain/proof"
	rundomain "tuku/internal/domain/run"
	anchorgit "tuku/internal/git/anchor"
)

type ShellSnapshotResult struct {
	TaskID                  common.TaskID
	Goal                    string
	Phase                   string
	Status                  string
	RepoAnchor              anchorgit.Snapshot
	IntentClass             string
	IntentSummary           string
	Brief                   *ShellBriefSummary
	Run                     *ShellRunSummary
	Checkpoint              *ShellCheckpointSummary
	Handoff                 *ShellHandoffSummary
	Launch                  *ShellLaunchSummary
	LaunchControl           *ShellLaunchControlSummary
	Acknowledgment          *ShellAcknowledgmentSummary
	FollowThrough           *ShellFollowThroughSummary
	HandoffContinuity       *ShellHandoffContinuitySummary
	Recovery                *ShellRecoverySummary
	RecentProofs            []ShellProofSummary
	RecentConversation      []ShellConversationSummary
	LatestCanonicalResponse string
}

type ShellBriefSummary struct {
	BriefID          common.BriefID
	Objective        string
	NormalizedAction string
	Constraints      []string
	DoneCriteria     []string
}

type ShellRunSummary struct {
	RunID              common.RunID
	WorkerKind         rundomain.WorkerKind
	Status             rundomain.Status
	LastKnownSummary   string
	StartedAt          time.Time
	EndedAt            *time.Time
	InterruptionReason string
}

type ShellCheckpointSummary struct {
	CheckpointID     common.CheckpointID
	Trigger          checkpoint.Trigger
	CreatedAt        time.Time
	ResumeDescriptor string
	IsResumable      bool
}

type ShellHandoffSummary struct {
	HandoffID    string
	Status       handoff.Status
	SourceWorker rundomain.WorkerKind
	TargetWorker rundomain.WorkerKind
	Mode         handoff.Mode
	Reason       string
	AcceptedBy   rundomain.WorkerKind
	CreatedAt    time.Time
}

type ShellLaunchSummary struct {
	AttemptID         string
	LaunchID          string
	Status            handoff.LaunchStatus
	RequestedAt       time.Time
	StartedAt         time.Time
	EndedAt           time.Time
	Summary           string
	ErrorMessage      string
	OutputArtifactRef string
}

type ShellLaunchControlSummary struct {
	State            LaunchControlState
	RetryDisposition LaunchRetryDisposition
	Reason           string
	HandoffID        string
	AttemptID        string
	LaunchID         string
	TargetWorker     rundomain.WorkerKind
	RequestedAt      time.Time
	CompletedAt      time.Time
	FailedAt         time.Time
}

type ShellAcknowledgmentSummary struct {
	Status    handoff.AcknowledgmentStatus
	Summary   string
	CreatedAt time.Time
}

type ShellFollowThroughSummary struct {
	RecordID        string
	Kind            handoff.FollowThroughKind
	Summary         string
	LaunchAttemptID string
	LaunchID        string
	CreatedAt       time.Time
}

type ShellHandoffContinuitySummary struct {
	State                        HandoffContinuityState
	Reason                       string
	LaunchAttemptID              string
	LaunchID                     string
	AcknowledgmentID             string
	AcknowledgmentStatus         handoff.AcknowledgmentStatus
	AcknowledgmentSummary        string
	FollowThroughID              string
	FollowThroughKind            handoff.FollowThroughKind
	FollowThroughSummary         string
	DownstreamContinuationProven bool
}

type ShellRecoveryIssue struct {
	Code    string
	Message string
}

type ShellRecoverySummary struct {
	ContinuityOutcome      ContinueOutcome
	RecoveryClass          RecoveryClass
	RecommendedAction      RecoveryAction
	ReadyForNextRun        bool
	ReadyForHandoffLaunch  bool
	RequiresDecision       bool
	RequiresRepair         bool
	RequiresReview         bool
	RequiresReconciliation bool
	DriftClass             checkpoint.DriftClass
	Reason                 string
	CheckpointID           common.CheckpointID
	RunID                  common.RunID
	HandoffID              string
	HandoffStatus          handoff.Status
	Issues                 []ShellRecoveryIssue
}

type ShellProofSummary struct {
	EventID   common.EventID
	Type      proof.EventType
	Summary   string
	Timestamp time.Time
}

type ShellConversationSummary struct {
	Role      conversation.Role
	Body      string
	CreatedAt time.Time
}

func (c *Coordinator) ShellSnapshotTask(ctx context.Context, taskID string) (ShellSnapshotResult, error) {
	id := common.TaskID(strings.TrimSpace(taskID))
	if id == "" {
		return ShellSnapshotResult{}, fmt.Errorf("task id is required")
	}

	caps, err := c.store.Capsules().Get(id)
	if err != nil {
		return ShellSnapshotResult{}, err
	}

	result := ShellSnapshotResult{
		TaskID:     caps.TaskID,
		Goal:       caps.Goal,
		Phase:      string(caps.CurrentPhase),
		Status:     caps.Status,
		RepoAnchor: capsuleAnchorSnapshot(caps),
	}

	if st, ok, err := c.shellIntent(id, caps.CurrentIntentID); err != nil {
		return ShellSnapshotResult{}, err
	} else if ok {
		result.IntentClass = string(st.Class)
		result.IntentSummary = shellIntentSummary(st)
	}

	if b, ok, err := c.shellBrief(id, caps.CurrentBriefID); err != nil {
		return ShellSnapshotResult{}, err
	} else if ok {
		result.Brief = &ShellBriefSummary{
			BriefID:          b.BriefID,
			Objective:        b.Objective,
			NormalizedAction: b.NormalizedAction,
			Constraints:      append([]string{}, b.Constraints...),
			DoneCriteria:     append([]string{}, b.DoneCriteria...),
		}
	}

	if runRec, err := c.store.Runs().LatestByTask(id); err != nil {
		if err != sql.ErrNoRows {
			return ShellSnapshotResult{}, err
		}
	} else {
		result.Run = &ShellRunSummary{
			RunID:              runRec.RunID,
			WorkerKind:         runRec.WorkerKind,
			Status:             runRec.Status,
			LastKnownSummary:   runRec.LastKnownSummary,
			StartedAt:          runRec.StartedAt,
			EndedAt:            runRec.EndedAt,
			InterruptionReason: runRec.InterruptionReason,
		}
	}

	if cp, err := c.store.Checkpoints().LatestByTask(id); err != nil {
		if err != sql.ErrNoRows {
			return ShellSnapshotResult{}, err
		}
	} else {
		result.Checkpoint = &ShellCheckpointSummary{
			CheckpointID:     cp.CheckpointID,
			Trigger:          cp.Trigger,
			CreatedAt:        cp.CreatedAt,
			ResumeDescriptor: cp.ResumeDescriptor,
			IsResumable:      cp.IsResumable,
		}
	}

	if packet, err := c.store.Handoffs().LatestByTask(id); err != nil {
		if err != sql.ErrNoRows {
			return ShellSnapshotResult{}, err
		}
	} else {
		var latestLaunch *handoff.Launch
		var latestAck *handoff.Acknowledgment
		var latestFollowThrough *handoff.FollowThrough
		result.Handoff = &ShellHandoffSummary{
			HandoffID:    packet.HandoffID,
			Status:       packet.Status,
			SourceWorker: packet.SourceWorker,
			TargetWorker: packet.TargetWorker,
			Mode:         packet.HandoffMode,
			Reason:       packet.Reason,
			AcceptedBy:   packet.AcceptedBy,
			CreatedAt:    packet.CreatedAt,
		}
		if launch, err := c.store.Handoffs().LatestLaunchByHandoff(packet.HandoffID); err != nil {
			if err != sql.ErrNoRows {
				return ShellSnapshotResult{}, err
			}
		} else {
			launchCopy := launch
			latestLaunch = &launchCopy
			result.Launch = &ShellLaunchSummary{
				AttemptID:         launch.AttemptID,
				LaunchID:          launch.LaunchID,
				Status:            launch.Status,
				RequestedAt:       launch.RequestedAt,
				StartedAt:         launch.StartedAt,
				EndedAt:           launch.EndedAt,
				Summary:           launch.Summary,
				ErrorMessage:      launch.ErrorMessage,
				OutputArtifactRef: launch.OutputArtifactRef,
			}
		}
		if ack, err := c.store.Handoffs().LatestAcknowledgment(packet.HandoffID); err != nil {
			if err != sql.ErrNoRows {
				return ShellSnapshotResult{}, err
			}
		} else {
			ackCopy := ack
			latestAck = &ackCopy
			result.Acknowledgment = &ShellAcknowledgmentSummary{
				Status:    ack.Status,
				Summary:   ack.Summary,
				CreatedAt: ack.CreatedAt,
			}
		}
		if record, err := c.store.Handoffs().LatestFollowThrough(packet.HandoffID); err != nil {
			if err != sql.ErrNoRows {
				return ShellSnapshotResult{}, err
			}
		} else {
			recordCopy := record
			latestFollowThrough = &recordCopy
			result.FollowThrough = &ShellFollowThroughSummary{
				RecordID:        record.RecordID,
				Kind:            record.Kind,
				Summary:         record.Summary,
				LaunchAttemptID: record.LaunchAttemptID,
				LaunchID:        record.LaunchID,
				CreatedAt:       record.CreatedAt,
			}
		}
		continuity := assessHandoffContinuity(id, &packet, latestLaunch, latestAck, latestFollowThrough)
		result.HandoffContinuity = &ShellHandoffContinuitySummary{
			State:                        continuity.State,
			Reason:                       continuity.Reason,
			LaunchAttemptID:              continuity.LaunchAttemptID,
			LaunchID:                     continuity.LaunchID,
			AcknowledgmentID:             continuity.AcknowledgmentID,
			AcknowledgmentStatus:         continuity.AcknowledgmentStatus,
			AcknowledgmentSummary:        continuity.AcknowledgmentSummary,
			FollowThroughID:              continuity.FollowThroughID,
			FollowThroughKind:            continuity.FollowThroughKind,
			FollowThroughSummary:         continuity.FollowThroughSummary,
			DownstreamContinuationProven: continuity.DownstreamContinuationProven,
		}
	}

	if events, err := c.store.Proofs().ListByTask(id, 8); err != nil {
		return ShellSnapshotResult{}, err
	} else {
		result.RecentProofs = make([]ShellProofSummary, 0, len(events))
		for _, evt := range events {
			result.RecentProofs = append(result.RecentProofs, ShellProofSummary{
				EventID:   evt.EventID,
				Type:      evt.Type,
				Summary:   summarizeProofEvent(evt),
				Timestamp: evt.Timestamp,
			})
		}
	}

	if messages, err := c.store.Conversations().ListRecent(caps.ConversationID, 18); err != nil {
		return ShellSnapshotResult{}, err
	} else {
		result.RecentConversation = make([]ShellConversationSummary, 0, len(messages))
		for _, msg := range messages {
			result.RecentConversation = append(result.RecentConversation, ShellConversationSummary{
				Role:      msg.Role,
				Body:      msg.Body,
				CreatedAt: msg.CreatedAt,
			})
			if msg.Role == conversation.RoleSystem {
				result.LatestCanonicalResponse = msg.Body
			}
		}
	}

	if assessment, err := c.assessContinue(ctx, id); err != nil {
		return ShellSnapshotResult{}, err
	} else {
		recovery := c.recoveryFromContinueAssessment(assessment)
		result.Recovery = shellRecoverySummary(recovery)
		control := assessLaunchControl(id, assessment.LatestHandoff, assessment.LatestLaunch)
		result.LaunchControl = &ShellLaunchControlSummary{
			State:            control.State,
			RetryDisposition: control.RetryDisposition,
			Reason:           control.Reason,
			HandoffID:        control.HandoffID,
			AttemptID:        control.AttemptID,
			LaunchID:         control.LaunchID,
			TargetWorker:     control.TargetWorker,
			RequestedAt:      control.RequestedAt,
			CompletedAt:      control.CompletedAt,
			FailedAt:         control.FailedAt,
		}
	}

	return result, nil
}

func shellRecoverySummary(in RecoveryAssessment) *ShellRecoverySummary {
	out := &ShellRecoverySummary{
		ContinuityOutcome:      in.ContinuityOutcome,
		RecoveryClass:          in.RecoveryClass,
		RecommendedAction:      in.RecommendedAction,
		ReadyForNextRun:        in.ReadyForNextRun,
		ReadyForHandoffLaunch:  in.ReadyForHandoffLaunch,
		RequiresDecision:       in.RequiresDecision,
		RequiresRepair:         in.RequiresRepair,
		RequiresReview:         in.RequiresReview,
		RequiresReconciliation: in.RequiresReconciliation,
		DriftClass:             in.DriftClass,
		Reason:                 in.Reason,
		CheckpointID:           in.CheckpointID,
		RunID:                  in.RunID,
		HandoffID:              in.HandoffID,
		HandoffStatus:          in.HandoffStatus,
	}
	if len(in.Issues) > 0 {
		out.Issues = make([]ShellRecoveryIssue, 0, len(in.Issues))
		for _, issue := range in.Issues {
			out.Issues = append(out.Issues, ShellRecoveryIssue{
				Code:    issue.Code,
				Message: issue.Message,
			})
		}
	}
	return out
}

func (c *Coordinator) shellIntent(taskID common.TaskID, currentID common.IntentID) (intent.State, bool, error) {
	if currentID != "" {
		st, err := c.store.Intents().LatestByTask(taskID)
		if err == nil && st.IntentID == currentID {
			return st, true, nil
		}
		if err != nil && err != sql.ErrNoRows {
			return intent.State{}, false, err
		}
	}
	st, err := c.store.Intents().LatestByTask(taskID)
	if err != nil {
		if err == sql.ErrNoRows {
			return intent.State{}, false, nil
		}
		return intent.State{}, false, err
	}
	return st, true, nil
}

func (c *Coordinator) shellBrief(taskID common.TaskID, currentID common.BriefID) (brief.ExecutionBrief, bool, error) {
	if currentID != "" {
		b, err := c.store.Briefs().Get(currentID)
		if err == nil {
			return b, true, nil
		}
		if err != sql.ErrNoRows {
			return brief.ExecutionBrief{}, false, err
		}
	}
	b, err := c.store.Briefs().LatestByTask(taskID)
	if err != nil {
		if err == sql.ErrNoRows {
			return brief.ExecutionBrief{}, false, nil
		}
		return brief.ExecutionBrief{}, false, err
	}
	return b, true, nil
}

func capsuleAnchorSnapshot(caps capsule.WorkCapsule) anchorgit.Snapshot {
	return anchorgit.Snapshot{
		RepoRoot:         caps.RepoRoot,
		Branch:           caps.BranchName,
		HeadSHA:          caps.HeadSHA,
		WorkingTreeDirty: caps.WorkingTreeDirty,
		CapturedAt:       caps.AnchorCapturedAt,
	}
}

func shellIntentSummary(st intent.State) string {
	if strings.TrimSpace(st.NormalizedAction) == "" {
		return string(st.Class)
	}
	return fmt.Sprintf("%s: %s", st.Class, st.NormalizedAction)
}

func summarizeProofEvent(evt proof.Event) string {
	switch evt.Type {
	case proof.EventUserMessageReceived:
		return "User message recorded"
	case proof.EventIntentCompiled:
		return "Intent compiled"
	case proof.EventBriefCreated:
		return "Execution brief updated"
	case proof.EventBriefRegenerated:
		return "Execution brief regenerated"
	case proof.EventWorkerRunStarted:
		return "Worker run started"
	case proof.EventWorkerRunCompleted:
		return "Worker run completed"
	case proof.EventWorkerRunFailed:
		return "Worker run failed"
	case proof.EventRunInterrupted:
		return "Run interrupted"
	case proof.EventCheckpointCreated:
		return "Checkpoint created"
	case proof.EventContinueAssessed:
		return "Continuity assessed"
	case proof.EventHandoffCreated:
		return "Handoff packet created"
	case proof.EventHandoffAccepted:
		return "Handoff accepted"
	case proof.EventHandoffLaunchRequested:
		return "Handoff launch prepared"
	case proof.EventHandoffLaunchCompleted:
		return "Handoff launch invoked"
	case proof.EventHandoffLaunchFailed:
		return "Handoff launch failed"
	case proof.EventHandoffLaunchBlocked:
		return "Handoff launch blocked"
	case proof.EventHandoffAcknowledgmentCaptured:
		return "Worker acknowledgment captured"
	case proof.EventHandoffAcknowledgmentUnavailable:
		return "Worker acknowledgment unavailable"
	case proof.EventHandoffFollowThroughRecorded:
		return "Downstream follow-through recorded"
	case proof.EventRecoveryActionRecorded:
		return "Recovery action recorded"
	case proof.EventInterruptedRunReviewed:
		return "Interrupted run reviewed"
	case proof.EventInterruptedRunResumeExecuted:
		return "Interrupted lineage continuation selected"
	case proof.EventRecoveryContinueExecuted:
		return "Continue recovery executed"
	case proof.EventShellHostStarted:
		return "Shell live host started"
	case proof.EventShellHostExited:
		return "Shell live host ended"
	case proof.EventShellFallbackActivated:
		return "Shell transcript fallback activated"
	case proof.EventCanonicalResponseEmitted:
		return "Canonical response emitted"
	default:
		return strings.ReplaceAll(strings.ToLower(string(evt.Type)), "_", " ")
	}
}

```


## internal/orchestrator/handoff_test.go

```go
package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"tuku/internal/adapters/adapter_contract"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/proof"
	rundomain "tuku/internal/domain/run"
	anchorgit "tuku/internal/git/anchor"
	"tuku/internal/response/canonical"
	"tuku/internal/storage"
	"tuku/internal/storage/sqlite"
)

func TestCreateHandoffFromSafeResumableState(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	seed, err := coord.CreateCheckpoint(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}
	capsBefore, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule before handoff: %v", err)
	}

	out, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "codex quota exhausted",
		Mode:         handoff.ModeResume,
		Notes:        []string{"prefer minimal diff follow-up"},
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if out.Status != handoff.StatusCreated {
		t.Fatalf("expected created status, got %s", out.Status)
	}
	if out.TargetWorker != rundomain.WorkerKindClaude {
		t.Fatalf("expected claude target worker, got %s", out.TargetWorker)
	}
	if out.Packet == nil {
		t.Fatal("expected handoff packet")
	}
	if out.Packet.CheckpointID != seed.CheckpointID {
		t.Fatalf("expected reused checkpoint %s, got %s", seed.CheckpointID, out.Packet.CheckpointID)
	}
	if out.Packet.TargetWorker != rundomain.WorkerKindClaude {
		t.Fatalf("expected packet target worker claude, got %s", out.Packet.TargetWorker)
	}
	if out.Packet.RepoAnchor.HeadSHA == "" {
		t.Fatal("expected repo anchor in handoff packet")
	}
	if out.Packet.BriefID == "" || out.Packet.IntentID == "" {
		t.Fatalf("expected brief and intent references in packet, got brief=%s intent=%s", out.Packet.BriefID, out.Packet.IntentID)
	}

	persisted, err := store.Handoffs().Get(out.HandoffID)
	if err != nil {
		t.Fatalf("load persisted handoff: %v", err)
	}
	if persisted.HandoffID != out.HandoffID {
		t.Fatalf("expected persisted handoff %s, got %s", out.HandoffID, persisted.HandoffID)
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if !hasEvent(events, proof.EventHandoffCreated) {
		t.Fatal("expected HANDOFF_CREATED proof event")
	}

	capsAfter, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule after handoff: %v", err)
	}
	if capsAfter.Version != capsBefore.Version {
		t.Fatalf("handoff should not mutate capsule version in reuse case: before=%d after=%d", capsBefore.Version, capsAfter.Version)
	}
}

func TestCreateHandoffBlockedOnInconsistentContinuity(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	lastEvent, err := latestEventID(store, taskID)
	if err != nil {
		t.Fatalf("latest event: %v", err)
	}
	bad := checkpoint.Checkpoint{
		Version:            1,
		CheckpointID:       common.CheckpointID("chk_bad_handoff_missing_brief"),
		TaskID:             taskID,
		RunID:              "",
		CreatedAt:          time.Now().UTC().Add(9 * time.Second),
		Trigger:            checkpoint.TriggerManual,
		CapsuleVersion:     caps.Version,
		Phase:              caps.CurrentPhase,
		Anchor:             anchorFromCapsule(caps),
		IntentID:           caps.CurrentIntentID,
		BriefID:            common.BriefID("brf_missing_for_handoff"),
		ContextPackID:      "",
		LastEventID:        lastEvent,
		PendingDecisionIDs: []common.DecisionID{},
		ResumeDescriptor:   "bad checkpoint for handoff consistency test",
		IsResumable:        true,
	}
	if err := store.Checkpoints().Create(bad); err != nil {
		t.Fatalf("create bad checkpoint: %v", err)
	}

	out, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "handoff despite broken continuity",
	})
	if err != nil {
		t.Fatalf("create blocked handoff: %v", err)
	}
	if out.Status != handoff.StatusBlocked {
		t.Fatalf("expected blocked status, got %s", out.Status)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "blocked") {
		t.Fatalf("expected blocked canonical response, got %q", out.CanonicalResponse)
	}
	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if !hasEvent(events, proof.EventHandoffBlocked) {
		t.Fatal("expected HANDOFF_BLOCKED proof event")
	}
}

func TestCreateHandoffCreatesCheckpointWhenReuseNotPossible(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	out, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "handoff without existing checkpoint",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if out.Status != handoff.StatusCreated {
		t.Fatalf("expected created status, got %s", out.Status)
	}
	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint: %v", err)
	}
	if latestCheckpoint.Trigger != checkpoint.TriggerHandoff {
		t.Fatalf("expected handoff trigger on handoff-created checkpoint, got %s", latestCheckpoint.Trigger)
	}
}

func TestCreateHandoffCreatesNewCheckpointWhenLatestCheckpointNotReusable(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	lastEvent, err := latestEventID(store, taskID)
	if err != nil {
		t.Fatalf("latest event: %v", err)
	}
	nonReusable := checkpoint.Checkpoint{
		Version:            1,
		CheckpointID:       common.CheckpointID("chk_non_reusable_for_handoff"),
		TaskID:             taskID,
		RunID:              "",
		CreatedAt:          time.Now().UTC().Add(-1 * time.Second),
		Trigger:            checkpoint.TriggerAwaitingDecision,
		CapsuleVersion:     caps.Version,
		Phase:              caps.CurrentPhase,
		Anchor:             anchorFromCapsule(caps),
		IntentID:           caps.CurrentIntentID,
		BriefID:            caps.CurrentBriefID,
		ContextPackID:      "",
		LastEventID:        lastEvent,
		PendingDecisionIDs: []common.DecisionID{},
		ResumeDescriptor:   "non-resumable checkpoint for reuse guard test",
		IsResumable:        false,
	}
	if err := store.Checkpoints().Create(nonReusable); err != nil {
		t.Fatalf("create non-reusable checkpoint: %v", err)
	}

	out, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "handoff requiring fresh resumable checkpoint",
		Mode:         handoff.ModeResume,
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if out.Status != handoff.StatusCreated {
		t.Fatalf("expected created status, got %s", out.Status)
	}
	if out.Packet == nil {
		t.Fatal("expected packet")
	}
	if out.Packet.CheckpointID == nonReusable.CheckpointID {
		t.Fatalf("expected fresh handoff checkpoint, got reused non-resumable checkpoint %s", nonReusable.CheckpointID)
	}

	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint: %v", err)
	}
	if latestCheckpoint.Trigger != checkpoint.TriggerHandoff {
		t.Fatalf("expected handoff trigger for newly created checkpoint, got %s", latestCheckpoint.Trigger)
	}
	if latestCheckpoint.CheckpointID != out.Packet.CheckpointID {
		t.Fatalf("expected packet checkpoint %s to match latest %s", out.Packet.CheckpointID, latestCheckpoint.CheckpointID)
	}
}

func TestCreateHandoffReusesMatchingLatestPacket(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	req := CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "reuse existing handoff packet",
		Mode:         handoff.ModeResume,
		Notes:        []string{"preserve prior packet"},
	}
	first, err := coord.CreateHandoff(context.Background(), req)
	if err != nil {
		t.Fatalf("create first handoff: %v", err)
	}
	second, err := coord.CreateHandoff(context.Background(), req)
	if err != nil {
		t.Fatalf("create second handoff: %v", err)
	}
	if first.HandoffID != second.HandoffID {
		t.Fatalf("expected handoff reuse, got first=%s second=%s", first.HandoffID, second.HandoffID)
	}
	if first.CheckpointID != second.CheckpointID {
		t.Fatalf("expected checkpoint reuse, got first=%s second=%s", first.CheckpointID, second.CheckpointID)
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if countEvents(events, proof.EventHandoffCreated) != 1 {
		t.Fatalf("expected exactly one HANDOFF_CREATED event, got %d", countEvents(events, proof.EventHandoffCreated))
	}
}

func TestAcceptHandoffRecordsCompletion(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "handoff to claude",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	acceptOut, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  createOut.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
		Notes:      []string{"accepted for follow-up implementation"},
	})
	if err != nil {
		t.Fatalf("accept handoff: %v", err)
	}
	if acceptOut.Status != handoff.StatusAccepted {
		t.Fatalf("expected accepted status, got %s", acceptOut.Status)
	}
	persisted, err := store.Handoffs().Get(createOut.HandoffID)
	if err != nil {
		t.Fatalf("load persisted handoff: %v", err)
	}
	if persisted.Status != handoff.StatusAccepted {
		t.Fatalf("expected persisted accepted status, got %s", persisted.Status)
	}
	if persisted.AcceptedBy != rundomain.WorkerKindClaude {
		t.Fatalf("expected persisted accepted_by %s, got %s", rundomain.WorkerKindClaude, persisted.AcceptedBy)
	}
	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if !hasEvent(events, proof.EventHandoffAccepted) {
		t.Fatal("expected HANDOFF_ACCEPTED proof event")
	}
}

func TestAcceptHandoffIsIdempotent(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "idempotent accept test",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	first, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  createOut.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
		Notes:      []string{"accept once"},
	})
	if err != nil {
		t.Fatalf("accept handoff first: %v", err)
	}
	second, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  createOut.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
		Notes:      []string{"accept once"},
	})
	if err != nil {
		t.Fatalf("accept handoff second: %v", err)
	}
	if first.Status != handoff.StatusAccepted || second.Status != handoff.StatusAccepted {
		t.Fatalf("expected accepted status on idempotent path, got first=%s second=%s", first.Status, second.Status)
	}

	persisted, err := store.Handoffs().Get(createOut.HandoffID)
	if err != nil {
		t.Fatalf("load handoff: %v", err)
	}
	if len(persisted.HandoffNotes) != 1 {
		t.Fatalf("expected exactly one persisted note after idempotent accept, got %+v", persisted.HandoffNotes)
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if countEvents(events, proof.EventHandoffAccepted) != 1 {
		t.Fatalf("expected exactly one HANDOFF_ACCEPTED event, got %d", countEvents(events, proof.EventHandoffAccepted))
	}
}

func TestAcceptedHandoffRecoveryIsLaunchReady(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "accepted handoff recovery readiness",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  createOut.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
	}); err != nil {
		t.Fatalf("accept handoff: %v", err)
	}

	continueOut, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if continueOut.RecoveryClass != RecoveryClassAcceptedHandoffLaunchReady {
		t.Fatalf("expected accepted handoff recovery class, got %s", continueOut.RecoveryClass)
	}
	if continueOut.RecommendedAction != RecoveryActionLaunchAcceptedHandoff {
		t.Fatalf("expected launch accepted handoff action, got %s", continueOut.RecommendedAction)
	}
	if continueOut.ReadyForNextRun {
		t.Fatal("accepted handoff recovery should not claim local next-run readiness")
	}
	if !continueOut.ReadyForHandoffLaunch {
		t.Fatal("accepted handoff recovery should be ready for handoff launch")
	}
}

func TestCreateHandoffBuildsPacketFromPersistedContinuityState(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startRes, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "complete", RunID: startRes.RunID}); err != nil {
		t.Fatalf("complete noop run: %v", err)
	}

	runRec, err := store.Runs().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest run: %v", err)
	}
	runRec.LastKnownSummary = "persisted-summary-for-handoff-trust"
	runRec.UpdatedAt = time.Now().UTC()
	if err := store.Runs().Update(runRec); err != nil {
		t.Fatalf("update latest run summary: %v", err)
	}

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	caps.Version++
	caps.UpdatedAt = time.Now().UTC()
	caps.WorkingTreeDirty = true
	caps.TouchedFiles = append(caps.TouchedFiles, "persisted/worker_state.go")
	caps.Blockers = []string{"persisted blocker for trust test"}
	caps.NextAction = "persisted next action for handoff"
	if err := store.Capsules().Update(caps); err != nil {
		t.Fatalf("update capsule: %v", err)
	}

	briefRec, err := store.Briefs().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest brief: %v", err)
	}
	lastEvent, err := latestEventID(store, taskID)
	if err != nil {
		t.Fatalf("latest event: %v", err)
	}
	persistedCheckpoint := checkpoint.Checkpoint{
		Version:            1,
		CheckpointID:       common.CheckpointID("chk_persisted_trust_anchor"),
		TaskID:             taskID,
		RunID:              runRec.RunID,
		CreatedAt:          time.Now().UTC().Add(15 * time.Second),
		Trigger:            checkpoint.TriggerManual,
		CapsuleVersion:     caps.Version,
		Phase:              caps.CurrentPhase,
		Anchor:             anchorFromCapsule(caps),
		IntentID:           caps.CurrentIntentID,
		BriefID:            briefRec.BriefID,
		ContextPackID:      "",
		LastEventID:        lastEvent,
		PendingDecisionIDs: []common.DecisionID{},
		ResumeDescriptor:   "persisted resume descriptor for trust test",
		IsResumable:        true,
	}
	if err := store.Checkpoints().Create(persistedCheckpoint); err != nil {
		t.Fatalf("create persisted checkpoint: %v", err)
	}

	out, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "trust test handoff",
		Mode:         handoff.ModeTakeover,
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if out.Status != handoff.StatusCreated {
		t.Fatalf("expected created status, got %s", out.Status)
	}
	if out.Packet == nil {
		t.Fatal("expected packet")
	}
	if out.Packet.CheckpointID != persistedCheckpoint.CheckpointID {
		t.Fatalf("expected packet checkpoint from persisted state %s, got %s", persistedCheckpoint.CheckpointID, out.Packet.CheckpointID)
	}
	if out.Packet.ResumeDescriptor != persistedCheckpoint.ResumeDescriptor {
		t.Fatalf("expected persisted resume descriptor %q, got %q", persistedCheckpoint.ResumeDescriptor, out.Packet.ResumeDescriptor)
	}
	if out.Packet.LatestRunID != runRec.RunID {
		t.Fatalf("expected packet latest run %s, got %s", runRec.RunID, out.Packet.LatestRunID)
	}
	if out.Packet.BriefID != briefRec.BriefID {
		t.Fatalf("expected packet brief %s, got %s", briefRec.BriefID, out.Packet.BriefID)
	}
	if out.Packet.HandoffMode != handoff.ModeTakeover {
		t.Fatalf("expected typed handoff mode %s, got %s", handoff.ModeTakeover, out.Packet.HandoffMode)
	}
	if !containsString(out.Packet.TouchedFiles, "persisted/worker_state.go") {
		t.Fatalf("expected touched files to reflect persisted capsule update: %+v", out.Packet.TouchedFiles)
	}
	if !containsString(out.Packet.Blockers, "persisted blocker for trust test") {
		t.Fatalf("expected blockers to reflect persisted capsule update: %+v", out.Packet.Blockers)
	}
}

func TestLaunchHandoffClaudeSuccessFlow(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherSuccess()
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "handoff launch test",
		Mode:         handoff.ModeResume,
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}

	launchOut, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("launch handoff: %v", err)
	}
	if launchOut.LaunchStatus != HandoffLaunchStatusCompleted {
		t.Fatalf("expected completed launch status, got %s", launchOut.LaunchStatus)
	}
	if launchOut.Payload == nil {
		t.Fatal("expected launch payload")
	}
	if launchOut.Payload.HandoffID != createOut.HandoffID {
		t.Fatalf("expected payload handoff id %s, got %s", createOut.HandoffID, launchOut.Payload.HandoffID)
	}
	if launchOut.Payload.BriefID != createOut.Packet.BriefID {
		t.Fatalf("expected payload brief id %s, got %s", createOut.Packet.BriefID, launchOut.Payload.BriefID)
	}
	if !strings.Contains(strings.ToLower(launchOut.CanonicalResponse), "acknowledgment") {
		t.Fatalf("expected canonical response to mention acknowledgment, got %q", launchOut.CanonicalResponse)
	}
	if !strings.Contains(strings.ToLower(launchOut.CanonicalResponse), "not yet proven complete") {
		t.Fatalf("expected canonical response to avoid downstream completion claims, got %q", launchOut.CanonicalResponse)
	}
	if !launcher.called {
		t.Fatal("expected handoff launcher to be called")
	}
	if launcher.lastReq.Payload.CheckpointID != createOut.Packet.CheckpointID {
		t.Fatalf("expected launcher payload checkpoint %s, got %s", createOut.Packet.CheckpointID, launcher.lastReq.Payload.CheckpointID)
	}
	ack, err := store.Handoffs().LatestAcknowledgment(createOut.HandoffID)
	if err != nil {
		t.Fatalf("expected persisted launch acknowledgment: %v", err)
	}
	if ack.Status != handoff.AcknowledgmentCaptured {
		t.Fatalf("expected captured acknowledgment status, got %s", ack.Status)
	}
	if ack.Summary == "" {
		t.Fatal("expected non-empty acknowledgment summary")
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if !hasEvent(events, proof.EventHandoffLaunchRequested) {
		t.Fatal("expected HANDOFF_LAUNCH_REQUESTED proof event")
	}
	if !hasEvent(events, proof.EventHandoffLaunchCompleted) {
		t.Fatal("expected HANDOFF_LAUNCH_COMPLETED proof event")
	}
	if !hasEvent(events, proof.EventHandoffAcknowledgmentCaptured) {
		t.Fatal("expected HANDOFF_ACKNOWLEDGMENT_CAPTURED proof event")
	}
	if !hasEvent(events, proof.EventCanonicalResponseEmitted) {
		t.Fatal("expected canonical response proof event")
	}
}

func TestCreateHandoffBlockedAfterFailedRunRecovery(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("run start: %v", err)
	}

	out, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "should block after failed run",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if out.Status != handoff.StatusBlocked {
		t.Fatalf("expected blocked handoff after failed run recovery state, got %s", out.Status)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "blocked") {
		t.Fatalf("expected blocked canonical response, got %q", out.CanonicalResponse)
	}
}

func TestLaunchHandoffSuccessWithUnusableOutputPersistsUnavailableAcknowledgment(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherUnusableOutput()
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "handoff unusable-ack test",
		Mode:         handoff.ModeResume,
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	launchOut, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("launch handoff: %v", err)
	}
	if launchOut.LaunchStatus != HandoffLaunchStatusCompleted {
		t.Fatalf("expected completed launch status, got %s", launchOut.LaunchStatus)
	}
	if !strings.Contains(strings.ToLower(launchOut.CanonicalResponse), "no usable initial acknowledgment") {
		t.Fatalf("expected canonical fallback wording, got %q", launchOut.CanonicalResponse)
	}
	if !strings.Contains(strings.ToLower(launchOut.CanonicalResponse), "not yet proven complete") {
		t.Fatalf("expected explicit uncertainty in canonical response, got %q", launchOut.CanonicalResponse)
	}

	ack, err := store.Handoffs().LatestAcknowledgment(createOut.HandoffID)
	if err != nil {
		t.Fatalf("load latest acknowledgment: %v", err)
	}
	if ack.Status != handoff.AcknowledgmentUnavailable {
		t.Fatalf("expected unavailable acknowledgment status, got %s", ack.Status)
	}
	if len(ack.Unknowns) == 0 {
		t.Fatal("expected unknowns for unavailable acknowledgment")
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if !hasEvent(events, proof.EventHandoffAcknowledgmentUnavailable) {
		t.Fatal("expected HANDOFF_ACKNOWLEDGMENT_UNAVAILABLE proof event")
	}
	if hasEvent(events, proof.EventHandoffAcknowledgmentCaptured) {
		t.Fatal("did not expect HANDOFF_ACKNOWLEDGMENT_CAPTURED for unusable output")
	}
}

func TestLaunchHandoffBlockedCases(t *testing.T) {
	t.Run("missing handoff", func(t *testing.T) {
		store := newTestStore(t)
		launcher := newFakeHandoffLauncherSuccess()
		coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
		taskID := setupTaskWithBrief(t, coord)

		out, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
			TaskID:    string(taskID),
			HandoffID: "hnd_missing",
		})
		if err != nil {
			t.Fatalf("launch handoff missing: %v", err)
		}
		if out.LaunchStatus != HandoffLaunchStatusBlocked {
			t.Fatalf("expected blocked launch status, got %s", out.LaunchStatus)
		}
		if launcher.called {
			t.Fatal("launcher should not be called on blocked path")
		}
		events, err := store.Proofs().ListByTask(taskID, 500)
		if err != nil {
			t.Fatalf("list proofs: %v", err)
		}
		if !hasEvent(events, proof.EventHandoffLaunchBlocked) {
			t.Fatal("expected HANDOFF_LAUNCH_BLOCKED proof event")
		}
	})

	t.Run("wrong status", func(t *testing.T) {
		store := newTestStore(t)
		launcher := newFakeHandoffLauncherSuccess()
		coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
		taskID := setupTaskWithBrief(t, coord)

		createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
			TaskID:       string(taskID),
			TargetWorker: rundomain.WorkerKindClaude,
			Reason:       "status block test",
		})
		if err != nil {
			t.Fatalf("create handoff: %v", err)
		}
		if err := store.Handoffs().UpdateStatus(taskID, createOut.HandoffID, handoff.StatusBlocked, rundomain.WorkerKindUnknown, []string{"blocked for test"}, time.Now().UTC()); err != nil {
			t.Fatalf("force blocked status: %v", err)
		}

		out, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
			TaskID:    string(taskID),
			HandoffID: createOut.HandoffID,
		})
		if err != nil {
			t.Fatalf("launch handoff wrong status: %v", err)
		}
		if out.LaunchStatus != HandoffLaunchStatusBlocked {
			t.Fatalf("expected blocked status, got %s", out.LaunchStatus)
		}
		if launcher.called {
			t.Fatal("launcher should not be called on wrong-status blocked path")
		}
	})

	t.Run("wrong target", func(t *testing.T) {
		store := newTestStore(t)
		launcher := newFakeHandoffLauncherSuccess()
		coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
		taskID := setupTaskWithBrief(t, coord)

		createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
			TaskID:       string(taskID),
			TargetWorker: rundomain.WorkerKindClaude,
			Reason:       "target mismatch test",
		})
		if err != nil {
			t.Fatalf("create handoff: %v", err)
		}

		out, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
			TaskID:       string(taskID),
			HandoffID:    createOut.HandoffID,
			TargetWorker: rundomain.WorkerKindCodex,
		})
		if err != nil {
			t.Fatalf("launch handoff wrong target: %v", err)
		}
		if out.LaunchStatus != HandoffLaunchStatusBlocked {
			t.Fatalf("expected blocked status, got %s", out.LaunchStatus)
		}
		if launcher.called {
			t.Fatal("launcher should not be called on wrong-target blocked path")
		}
	})
}

func TestLaunchHandoffFailureRecordsProofAndCanonical(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherError(errors.New("claude launch unavailable"))
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "failure path test",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}

	out, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("launch handoff failure path should return canonical result, got err: %v", err)
	}
	if out.LaunchStatus != HandoffLaunchStatusFailed {
		t.Fatalf("expected failed launch status, got %s", out.LaunchStatus)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "failed") {
		t.Fatalf("expected failed canonical response, got %q", out.CanonicalResponse)
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if !hasEvent(events, proof.EventHandoffLaunchRequested) {
		t.Fatal("expected HANDOFF_LAUNCH_REQUESTED proof event")
	}
	if !hasEvent(events, proof.EventHandoffLaunchFailed) {
		t.Fatal("expected HANDOFF_LAUNCH_FAILED proof event")
	}
	if !hasEvent(events, proof.EventCanonicalResponseEmitted) {
		t.Fatal("expected canonical response proof event")
	}
}

func TestLaunchHandoffReusesDurableSuccessWithoutRelaunch(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherSuccess()
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "replay durable success",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	first, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("launch handoff first: %v", err)
	}
	second, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("launch handoff second: %v", err)
	}
	if launcher.callCount != 1 {
		t.Fatalf("expected launcher to run once, got %d", launcher.callCount)
	}
	if first.LaunchID == "" || second.LaunchID != first.LaunchID {
		t.Fatalf("expected durable launch id reuse, got first=%s second=%s", first.LaunchID, second.LaunchID)
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if countEvents(events, proof.EventHandoffLaunchCompleted) != 1 {
		t.Fatalf("expected exactly one HANDOFF_LAUNCH_COMPLETED event, got %d", countEvents(events, proof.EventHandoffLaunchCompleted))
	}
	if countEvents(events, proof.EventHandoffAcknowledgmentCaptured) != 1 {
		t.Fatalf("expected exactly one HANDOFF_ACKNOWLEDGMENT_CAPTURED event, got %d", countEvents(events, proof.EventHandoffAcknowledgmentCaptured))
	}
}

func TestLaunchHandoffBlockedWhenLauncherMissing(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "missing launcher guard",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}

	out, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("launch handoff with missing launcher should return canonical blocked result, got err: %v", err)
	}
	if out.LaunchStatus != HandoffLaunchStatusBlocked {
		t.Fatalf("expected blocked launch status, got %s", out.LaunchStatus)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "blocked") {
		t.Fatalf("expected blocked canonical response, got %q", out.CanonicalResponse)
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if !hasEvent(events, proof.EventHandoffLaunchBlocked) {
		t.Fatal("expected HANDOFF_LAUNCH_BLOCKED proof event")
	}
	if hasEvent(events, proof.EventHandoffLaunchRequested) {
		t.Fatal("should not emit HANDOFF_LAUNCH_REQUESTED when launcher is missing")
	}
}

func TestLaunchHandoffBlocksRetryWhenPriorRequestOutcomeUnknown(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherSuccess()
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "unknown launch replay guard",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}

	now := time.Now().UTC()
	if err := store.Handoffs().CreateLaunch(handoff.Launch{
		Version:      1,
		AttemptID:    "hlc_requested_unknown",
		HandoffID:    createOut.HandoffID,
		TaskID:       taskID,
		TargetWorker: createOut.TargetWorker,
		Status:       handoff.LaunchStatusRequested,
		PayloadHash:  "hash_requested_unknown",
		RequestedAt:  now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("create requested launch record: %v", err)
	}

	out, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("launch handoff retry guard: %v", err)
	}
	if out.LaunchStatus != HandoffLaunchStatusBlocked {
		t.Fatalf("expected blocked replay status, got %s", out.LaunchStatus)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "unknown") {
		t.Fatalf("expected unknown-outcome canonical response, got %q", out.CanonicalResponse)
	}
	if launcher.callCount != 0 {
		t.Fatalf("expected launcher not to run, got %d calls", launcher.callCount)
	}
}

func TestLaunchHandoffAllowsRetryAfterDurableFailure(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherError(errors.New("claude launch unavailable"))
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "retry after durable failure",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}

	first, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("first failed launch: %v", err)
	}
	if first.LaunchStatus != HandoffLaunchStatusFailed {
		t.Fatalf("expected failed first launch, got %s", first.LaunchStatus)
	}

	launcher.err = nil
	launcher.result = newFakeHandoffLauncherSuccess().result
	second, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("second retry launch: %v", err)
	}
	if second.LaunchStatus != HandoffLaunchStatusCompleted {
		t.Fatalf("expected completed second launch, got %s", second.LaunchStatus)
	}
	if launcher.callCount != 2 {
		t.Fatalf("expected launcher to run twice across retry, got %d", launcher.callCount)
	}

	latestLaunch, err := store.Handoffs().LatestLaunchByHandoff(createOut.HandoffID)
	if err != nil {
		t.Fatalf("latest launch by handoff: %v", err)
	}
	if latestLaunch.Status != handoff.LaunchStatusCompleted {
		t.Fatalf("expected latest durable launch to be completed, got %s", latestLaunch.Status)
	}
	if latestLaunch.AttemptID == "" {
		t.Fatal("expected latest durable launch attempt id")
	}
}

func TestStatusAndInspectSurfaceDurableLaunchControl(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "launch control inspectability",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  createOut.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
	}); err != nil {
		t.Fatalf("accept handoff: %v", err)
	}

	now := time.Now().UTC()
	if err := store.Handoffs().CreateLaunch(handoff.Launch{
		Version:      1,
		AttemptID:    "hlc_control_pending",
		HandoffID:    createOut.HandoffID,
		TaskID:       taskID,
		TargetWorker: rundomain.WorkerKindClaude,
		Status:       handoff.LaunchStatusRequested,
		PayloadHash:  "launch_control_pending",
		RequestedAt:  now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("create launch control record: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.LaunchControlState != LaunchControlStateRequestedOutcomeUnknown {
		t.Fatalf("expected requested-unknown launch control state, got %s", status.LaunchControlState)
	}
	if status.LaunchRetryDisposition != LaunchRetryDispositionBlocked {
		t.Fatalf("expected blocked retry disposition, got %s", status.LaunchRetryDisposition)
	}
	if status.RecoveryClass != RecoveryClassHandoffLaunchPendingOutcome {
		t.Fatalf("expected pending launch recovery class, got %s", status.RecoveryClass)
	}
	if status.HandoffContinuityState != HandoffContinuityStateLaunchPendingOutcome {
		t.Fatalf("expected pending handoff continuity state, got %s", status.HandoffContinuityState)
	}
	if status.HandoffContinuationProven {
		t.Fatal("pending launch must not claim downstream continuation proven")
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.Launch == nil {
		t.Fatal("expected persisted launch in inspect output")
	}
	if inspectOut.LaunchControl == nil {
		t.Fatal("expected launch control in inspect output")
	}
	if inspectOut.LaunchControl.State != LaunchControlStateRequestedOutcomeUnknown {
		t.Fatalf("expected inspect launch control state requested-unknown, got %s", inspectOut.LaunchControl.State)
	}
	if inspectOut.Recovery == nil || inspectOut.Recovery.RecoveryClass != RecoveryClassHandoffLaunchPendingOutcome {
		t.Fatalf("expected inspect recovery class %s, got %+v", RecoveryClassHandoffLaunchPendingOutcome, inspectOut.Recovery)
	}
	if inspectOut.HandoffContinuity == nil || inspectOut.HandoffContinuity.State != HandoffContinuityStateLaunchPendingOutcome {
		t.Fatalf("expected inspect handoff continuity pending outcome, got %+v", inspectOut.HandoffContinuity)
	}
}

func TestStatusAndInspectSurfaceCompletedClaudeLaunchWithCapturedAcknowledgment(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "captured acknowledgment inspectability",
		Mode:         handoff.ModeResume,
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  createOut.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
	}); err != nil {
		t.Fatalf("accept handoff: %v", err)
	}
	if _, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	}); err != nil {
		t.Fatalf("launch handoff: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.RecoveryClass != RecoveryClassHandoffLaunchCompleted {
		t.Fatalf("expected launched recovery class, got %s", status.RecoveryClass)
	}
	if status.ReadyForNextRun {
		t.Fatal("completed Claude launch must not imply fresh next-run readiness")
	}
	if status.HandoffContinuityState != HandoffContinuityStateLaunchCompletedAckSeen {
		t.Fatalf("expected captured-ack handoff continuity state, got %s", status.HandoffContinuityState)
	}
	if status.LatestAcknowledgmentStatus != handoff.AcknowledgmentCaptured {
		t.Fatalf("expected captured acknowledgment status, got %s", status.LatestAcknowledgmentStatus)
	}
	if status.HandoffContinuationProven {
		t.Fatal("captured acknowledgment must not prove downstream continuation")
	}
	if !strings.Contains(strings.ToLower(status.HandoffContinuityReason), "unproven") {
		t.Fatalf("expected explicit downstream-unproven reason, got %q", status.HandoffContinuityReason)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.HandoffContinuity == nil || inspectOut.HandoffContinuity.State != HandoffContinuityStateLaunchCompletedAckSeen {
		t.Fatalf("expected inspect captured-ack handoff continuity, got %+v", inspectOut.HandoffContinuity)
	}
	if inspectOut.HandoffContinuity.DownstreamContinuationProven {
		t.Fatal("inspect handoff continuity must not claim downstream continuation proven")
	}
	if inspectOut.Acknowledgment == nil || inspectOut.Acknowledgment.Status != handoff.AcknowledgmentCaptured {
		t.Fatalf("expected inspect acknowledgment captured, got %+v", inspectOut.Acknowledgment)
	}
}

func TestStatusAndInspectSurfaceCompletedClaudeLaunchWithUnavailableAcknowledgment(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherUnusableOutput())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "unavailable acknowledgment inspectability",
		Mode:         handoff.ModeResume,
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  createOut.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
	}); err != nil {
		t.Fatalf("accept handoff: %v", err)
	}
	if _, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	}); err != nil {
		t.Fatalf("launch handoff: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.HandoffContinuityState != HandoffContinuityStateLaunchCompletedAckEmpty {
		t.Fatalf("expected unavailable-ack handoff continuity state, got %s", status.HandoffContinuityState)
	}
	if status.LatestAcknowledgmentStatus != handoff.AcknowledgmentUnavailable {
		t.Fatalf("expected unavailable acknowledgment status, got %s", status.LatestAcknowledgmentStatus)
	}
	if !strings.Contains(strings.ToLower(status.HandoffContinuityReason), "no usable initial acknowledgment") &&
		!strings.Contains(strings.ToLower(status.HandoffContinuityReason), "acknowledgment") {
		t.Fatalf("expected acknowledgment-unavailable reason, got %q", status.HandoffContinuityReason)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.HandoffContinuity == nil || inspectOut.HandoffContinuity.State != HandoffContinuityStateLaunchCompletedAckEmpty {
		t.Fatalf("expected inspect unavailable-ack handoff continuity, got %+v", inspectOut.HandoffContinuity)
	}
}

func TestRecordHandoffFollowThroughProofOfLifeStrengthensLaunchedClaudeContinuity(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "record downstream proof of life",
		Mode:         handoff.ModeResume,
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  createOut.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
	}); err != nil {
		t.Fatalf("accept handoff: %v", err)
	}
	if _, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	}); err != nil {
		t.Fatalf("launch handoff: %v", err)
	}

	recorded, err := coord.RecordHandoffFollowThrough(context.Background(), RecordHandoffFollowThroughRequest{
		TaskID:  string(taskID),
		Kind:    handoff.FollowThroughProofOfLifeObserved,
		Summary: "later Claude proof of life observed",
		Notes:   []string{"operator observed downstream ping"},
	})
	if err != nil {
		t.Fatalf("record follow-through: %v", err)
	}
	if recorded.HandoffContinuity.State != HandoffContinuityStateFollowThroughProofOfLife {
		t.Fatalf("expected proof-of-life continuity state, got %+v", recorded.HandoffContinuity)
	}
	if !recorded.HandoffContinuity.DownstreamContinuationProven {
		t.Fatal("proof-of-life continuity should mark downstream continuation proven without implying completion")
	}
	if recorded.ReadyForNextRun {
		t.Fatal("proof-of-life follow-through must not make local next run ready")
	}

	persisted, err := store.Handoffs().LatestFollowThrough(createOut.HandoffID)
	if err != nil {
		t.Fatalf("load persisted follow-through: %v", err)
	}
	if persisted.Kind != handoff.FollowThroughProofOfLifeObserved {
		t.Fatalf("expected persisted proof-of-life kind, got %s", persisted.Kind)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.HandoffContinuityState != HandoffContinuityStateFollowThroughProofOfLife {
		t.Fatalf("expected proof-of-life handoff continuity state, got %s", status.HandoffContinuityState)
	}
	if status.LatestFollowThroughKind != handoff.FollowThroughProofOfLifeObserved {
		t.Fatalf("expected latest follow-through kind in status, got %s", status.LatestFollowThroughKind)
	}
	if !status.HandoffContinuationProven {
		t.Fatal("status should reflect downstream continuation proven for proof-of-life evidence")
	}
	if status.ReadyForNextRun {
		t.Fatal("status must not imply fresh next-run readiness after handoff follow-through proof of life")
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.FollowThrough == nil || inspectOut.FollowThrough.Kind != handoff.FollowThroughProofOfLifeObserved {
		t.Fatalf("expected inspect follow-through payload, got %+v", inspectOut.FollowThrough)
	}
	if inspectOut.HandoffContinuity == nil || inspectOut.HandoffContinuity.State != HandoffContinuityStateFollowThroughProofOfLife {
		t.Fatalf("expected inspect proof-of-life continuity, got %+v", inspectOut.HandoffContinuity)
	}
	if inspectOut.Recovery == nil || inspectOut.Recovery.RecoveryClass != RecoveryClassHandoffLaunchCompleted {
		t.Fatalf("expected launched-handoff recovery after proof-of-life evidence, got %+v", inspectOut.Recovery)
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if !hasEvent(events, proof.EventHandoffFollowThroughRecorded) {
		t.Fatal("expected HANDOFF_FOLLOW_THROUGH_RECORDED proof event")
	}
}

func TestRecordHandoffFollowThroughStalledRequiresReview(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "record stalled downstream follow-through",
		Mode:         handoff.ModeResume,
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  createOut.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
	}); err != nil {
		t.Fatalf("accept handoff: %v", err)
	}
	if _, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	}); err != nil {
		t.Fatalf("launch handoff: %v", err)
	}

	recorded, err := coord.RecordHandoffFollowThrough(context.Background(), RecordHandoffFollowThroughRequest{
		TaskID:  string(taskID),
		Kind:    handoff.FollowThroughStalledReviewRequired,
		Summary: "Claude follow-through appears stalled",
	})
	if err != nil {
		t.Fatalf("record stalled follow-through: %v", err)
	}
	if recorded.HandoffContinuity.State != HandoffContinuityStateFollowThroughStalled {
		t.Fatalf("expected stalled follow-through continuity, got %+v", recorded.HandoffContinuity)
	}
	if recorded.RecoveryClass != RecoveryClassHandoffFollowThroughReviewRequired {
		t.Fatalf("expected handoff follow-through review-required recovery, got %s", recorded.RecoveryClass)
	}
	if recorded.RecommendedAction != RecoveryActionReviewHandoffFollowThrough {
		t.Fatalf("expected review-handoff-follow-through action, got %s", recorded.RecommendedAction)
	}
	if recorded.ReadyForNextRun {
		t.Fatal("stalled follow-through must not make local next run ready")
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.RecoveryClass != RecoveryClassHandoffFollowThroughReviewRequired {
		t.Fatalf("expected stalled follow-through recovery in status, got %s", status.RecoveryClass)
	}
	if status.RecommendedAction != RecoveryActionReviewHandoffFollowThrough {
		t.Fatalf("expected stalled follow-through action in status, got %s", status.RecommendedAction)
	}
	if status.HandoffContinuityState != HandoffContinuityStateFollowThroughStalled {
		t.Fatalf("expected stalled handoff continuity in status, got %s", status.HandoffContinuityState)
	}
}

func TestRecordHandoffFollowThroughRejectsInvalidPostureAndReplaysWithinSameLaunch(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "follow-through posture validation",
		Mode:         handoff.ModeResume,
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  createOut.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
	}); err != nil {
		t.Fatalf("accept handoff: %v", err)
	}

	if _, err := coord.RecordHandoffFollowThrough(context.Background(), RecordHandoffFollowThroughRequest{
		TaskID:  string(taskID),
		Kind:    handoff.FollowThroughContinuationUnknown,
		Summary: "still unknown before launch",
	}); err == nil || !strings.Contains(err.Error(), "launched Claude follow-through posture") {
		t.Fatalf("expected invalid-posture rejection before launch completion, got %v", err)
	}

	launcher := newFakeHandoffLauncherSuccess()
	coord = newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	if _, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	}); err != nil {
		t.Fatalf("launch handoff: %v", err)
	}

	first, err := coord.RecordHandoffFollowThrough(context.Background(), RecordHandoffFollowThroughRequest{
		TaskID:  string(taskID),
		Kind:    handoff.FollowThroughContinuationUnknown,
		Summary: "downstream follow-through remains unknown",
	})
	if err != nil {
		t.Fatalf("record first unknown follow-through: %v", err)
	}
	second, err := coord.RecordHandoffFollowThrough(context.Background(), RecordHandoffFollowThroughRequest{
		TaskID:  string(taskID),
		Kind:    handoff.FollowThroughContinuationUnknown,
		Summary: "downstream follow-through remains unknown",
	})
	if err != nil {
		t.Fatalf("record replayed unknown follow-through: %v", err)
	}
	if first.Record.RecordID != second.Record.RecordID {
		t.Fatalf("expected replay reuse of follow-through record, got first=%s second=%s", first.Record.RecordID, second.Record.RecordID)
	}
	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if countEvents(events, proof.EventHandoffFollowThroughRecorded) != 1 {
		t.Fatalf("expected exactly one HANDOFF_FOLLOW_THROUGH_RECORDED event, got %d", countEvents(events, proof.EventHandoffFollowThroughRecorded))
	}
}

func TestCompletedClaudeLaunchWithoutPersistedAcknowledgmentRequiresRepair(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "missing acknowledgment continuity invariant",
		Mode:         handoff.ModeResume,
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  createOut.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
	}); err != nil {
		t.Fatalf("accept handoff: %v", err)
	}

	now := time.Now().UTC()
	if err := store.Handoffs().CreateLaunch(handoff.Launch{
		Version:      1,
		AttemptID:    "hlc_completed_missing_ack",
		HandoffID:    createOut.HandoffID,
		TaskID:       taskID,
		TargetWorker: rundomain.WorkerKindClaude,
		Status:       handoff.LaunchStatusCompleted,
		LaunchID:     "launch_missing_ack",
		PayloadHash:  "payload_missing_ack",
		RequestedAt:  now,
		StartedAt:    now,
		EndedAt:      now.Add(50 * time.Millisecond),
		Summary:      "launch completed without persisted ack for invariant test",
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("create completed launch without ack: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.RecoveryClass != RecoveryClassRepairRequired {
		t.Fatalf("expected repair-required recovery for missing ack continuity break, got %s", status.RecoveryClass)
	}
	if status.HandoffContinuityState != HandoffContinuityStateLaunchCompletedAckLost {
		t.Fatalf("expected missing-ack handoff continuity state, got %s", status.HandoffContinuityState)
	}
	if !strings.Contains(strings.ToLower(status.HandoffContinuityReason), "no persisted acknowledgment") {
		t.Fatalf("expected missing-ack continuity reason, got %q", status.HandoffContinuityReason)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.HandoffContinuity == nil || inspectOut.HandoffContinuity.State != HandoffContinuityStateLaunchCompletedAckLost {
		t.Fatalf("expected inspect missing-ack handoff continuity, got %+v", inspectOut.HandoffContinuity)
	}
}

func TestLaunchHandoffBlockedUsesPacketTargetWhenRequestTargetEmpty(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherSuccess()
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	cpOut, err := coord.CreateCheckpoint(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("create checkpoint: %v", err)
	}
	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	briefRec, err := store.Briefs().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("get latest brief: %v", err)
	}
	cp, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("get latest checkpoint: %v", err)
	}
	if cp.CheckpointID != cpOut.CheckpointID {
		t.Fatalf("expected checkpoint %s, got %s", cpOut.CheckpointID, cp.CheckpointID)
	}

	packet := handoff.Packet{
		Version:          1,
		HandoffID:        "hnd_codex_target_for_block_test",
		TaskID:           taskID,
		Status:           handoff.StatusCreated,
		SourceWorker:     rundomain.WorkerKindUnknown,
		TargetWorker:     rundomain.WorkerKindCodex,
		HandoffMode:      handoff.ModeResume,
		Reason:           "seed unsupported target launch packet",
		CurrentPhase:     caps.CurrentPhase,
		CheckpointID:     cp.CheckpointID,
		BriefID:          briefRec.BriefID,
		IntentID:         caps.CurrentIntentID,
		CapsuleVersion:   caps.Version,
		RepoAnchor:       cp.Anchor,
		IsResumable:      true,
		ResumeDescriptor: cp.ResumeDescriptor,
		Goal:             caps.Goal,
		BriefObjective:   briefRec.Objective,
		NormalizedAction: briefRec.NormalizedAction,
		Constraints:      append([]string{}, briefRec.Constraints...),
		DoneCriteria:     append([]string{}, briefRec.DoneCriteria...),
		TouchedFiles:     append([]string{}, caps.TouchedFiles...),
		Blockers:         append([]string{}, caps.Blockers...),
		NextAction:       caps.NextAction,
		CreatedAt:        time.Now().UTC(),
	}
	if err := store.Handoffs().Create(packet); err != nil {
		t.Fatalf("create handoff packet: %v", err)
	}

	out, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: packet.HandoffID,
	})
	if err != nil {
		t.Fatalf("launch handoff: %v", err)
	}
	if out.LaunchStatus != HandoffLaunchStatusBlocked {
		t.Fatalf("expected blocked status, got %s", out.LaunchStatus)
	}
	if out.TargetWorker != rundomain.WorkerKindCodex {
		t.Fatalf("expected blocked target worker to match packet target %s, got %s", rundomain.WorkerKindCodex, out.TargetWorker)
	}
	if launcher.called {
		t.Fatal("launcher should not be called for unsupported target")
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func newTestCoordinatorWithLauncher(t *testing.T, store *sqlite.Store, anchorProvider anchorgit.Provider, adapter adapter_contract.WorkerAdapter, launcher adapter_contract.HandoffLauncher) *Coordinator {
	t.Helper()
	coord, err := NewCoordinator(Dependencies{
		Store:           store,
		IntentCompiler:  NewIntentStubCompiler(),
		BriefBuilder:    NewBriefBuilderV1(nil, nil),
		WorkerAdapter:   adapter,
		HandoffLauncher: launcher,
		Synthesizer:     canonical.NewSimpleSynthesizer(),
		AnchorProvider:  anchorProvider,
		ShellSessions:   NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	return coord
}

type fakeHandoffLauncher struct {
	kind      adapter_contract.WorkerKind
	result    adapter_contract.HandoffLaunchResult
	err       error
	called    bool
	callCount int
	lastReq   adapter_contract.HandoffLaunchRequest
}

func newFakeHandoffLauncherSuccess() *fakeHandoffLauncher {
	now := time.Now().UTC()
	return &fakeHandoffLauncher{
		kind: adapter_contract.WorkerClaude,
		result: adapter_contract.HandoffLaunchResult{
			LaunchID:     "hlc_test",
			TargetWorker: adapter_contract.WorkerClaude,
			StartedAt:    now,
			EndedAt:      now.Add(150 * time.Millisecond),
			Command:      "claude",
			Args:         []string{"--print"},
			ExitCode:     0,
			Stdout:       "claude handoff launch accepted",
			Summary:      "handoff launch accepted",
		},
	}
}

func newFakeHandoffLauncherError(err error) *fakeHandoffLauncher {
	now := time.Now().UTC()
	return &fakeHandoffLauncher{
		kind: adapter_contract.WorkerClaude,
		result: adapter_contract.HandoffLaunchResult{
			LaunchID:     "hlc_err",
			TargetWorker: adapter_contract.WorkerClaude,
			StartedAt:    now,
			EndedAt:      now.Add(50 * time.Millisecond),
			Command:      "claude",
			Args:         []string{},
			ExitCode:     1,
			Stderr:       "launcher failed",
			Summary:      "handoff launch failed",
		},
		err: err,
	}
}

func newFakeHandoffLauncherUnusableOutput() *fakeHandoffLauncher {
	now := time.Now().UTC()
	return &fakeHandoffLauncher{
		kind: adapter_contract.WorkerClaude,
		result: adapter_contract.HandoffLaunchResult{
			LaunchID:     "hlc_unusable",
			TargetWorker: adapter_contract.WorkerClaude,
			StartedAt:    now,
			EndedAt:      now.Add(80 * time.Millisecond),
			Command:      "claude",
			Args:         []string{"--print"},
			ExitCode:     0,
			Stdout:       "   \n  ",
			Stderr:       "",
			Summary:      "",
		},
	}
}

func (f *fakeHandoffLauncher) Name() adapter_contract.WorkerKind {
	return f.kind
}

func (f *fakeHandoffLauncher) LaunchHandoff(_ context.Context, req adapter_contract.HandoffLaunchRequest) (adapter_contract.HandoffLaunchResult, error) {
	f.called = true
	f.callCount++
	f.lastReq = req
	out := f.result
	if out.TargetWorker == "" {
		out.TargetWorker = req.TargetWorker
	}
	if out.LaunchID == "" {
		out.LaunchID = "hlc_generated"
	}
	if out.StartedAt.IsZero() {
		out.StartedAt = time.Now().UTC()
	}
	if out.EndedAt.IsZero() {
		out.EndedAt = out.StartedAt.Add(100 * time.Millisecond)
	}
	return out, f.err
}

var _ adapter_contract.HandoffLauncher = (*fakeHandoffLauncher)(nil)

func (s *faultInjectedStore) Handoffs() storage.HandoffStore {
	return s.base.Handoffs()
}

func (s *txCountingStore) Handoffs() storage.HandoffStore {
	return s.base.Handoffs()
}

var _ storage.Store = (*faultInjectedStore)(nil)
var _ storage.Store = (*txCountingStore)(nil)

```


## internal/orchestrator/shell_test.go

```go
package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/recoveryaction"
	rundomain "tuku/internal/domain/run"
)

func TestShellSnapshotTaskBuildsShellStateFromPersistedTaskState(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	runRes, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start: %v", err)
	}
	if _, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Mode:         handoff.ModeResume,
		Reason:       "shell test",
	}); err != nil {
		t.Fatalf("create handoff: %v", err)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.TaskID != taskID {
		t.Fatalf("expected task id %s, got %s", taskID, snapshot.TaskID)
	}
	if snapshot.Brief == nil || snapshot.Brief.BriefID == "" {
		t.Fatal("expected brief summary in shell snapshot")
	}
	if snapshot.Run == nil || snapshot.Run.RunID != runRes.RunID {
		t.Fatalf("expected latest run summary, got %+v", snapshot.Run)
	}
	if snapshot.Checkpoint == nil || snapshot.Checkpoint.CheckpointID == "" {
		t.Fatal("expected checkpoint summary in shell snapshot")
	}
	if snapshot.Handoff == nil || snapshot.Handoff.TargetWorker != rundomain.WorkerKindClaude {
		t.Fatalf("expected latest handoff summary, got %+v", snapshot.Handoff)
	}
	if snapshot.Recovery == nil {
		t.Fatal("expected recovery summary in shell snapshot")
	}
	if snapshot.LaunchControl == nil {
		t.Fatal("expected launch control summary in shell snapshot")
	}
	if len(snapshot.RecentProofs) == 0 {
		t.Fatal("expected proof highlights")
	}
	if len(snapshot.RecentConversation) == 0 {
		t.Fatal("expected recent conversation")
	}
	if snapshot.LatestCanonicalResponse == "" {
		t.Fatal("expected latest canonical response")
	}
}

func TestShellSnapshotInterruptedRunExposesRecoverableState(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{
		TaskID:             string(taskID),
		Action:             "interrupt",
		RunID:              startOut.RunID,
		InterruptionReason: "operator interrupted",
	}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Recovery == nil {
		t.Fatal("expected recovery summary")
	}
	if snapshot.Recovery.RecoveryClass != RecoveryClassInterruptedRunRecoverable {
		t.Fatalf("expected interrupted recoverable class, got %s", snapshot.Recovery.RecoveryClass)
	}
	if snapshot.Recovery.RecommendedAction != RecoveryActionResumeInterrupted {
		t.Fatalf("expected interrupted recovery action, got %s", snapshot.Recovery.RecommendedAction)
	}
	if snapshot.Recovery.ReadyForNextRun {
		t.Fatal("interrupted recovery must not appear fresh-start ready in shell snapshot")
	}
	if snapshot.LaunchControl == nil || snapshot.LaunchControl.State != LaunchControlStateNotApplicable {
		t.Fatalf("expected non-applicable launch control, got %+v", snapshot.LaunchControl)
	}
}

func TestShellSnapshotInterruptedRunReviewedStillShowsInterruptedRecoverableState(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{
		TaskID:             string(taskID),
		Action:             "interrupt",
		RunID:              startOut.RunID,
		InterruptionReason: "operator interrupted",
	}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID:  string(taskID),
		Kind:    recoveryaction.KindInterruptedRunReviewed,
		Summary: "interrupted lineage reviewed",
	}); err != nil {
		t.Fatalf("record interrupted review: %v", err)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Recovery == nil || snapshot.Recovery.RecoveryClass != RecoveryClassInterruptedRunRecoverable {
		t.Fatalf("expected interrupted recoverable class after review, got %+v", snapshot.Recovery)
	}
	if snapshot.Recovery.ReadyForNextRun {
		t.Fatal("interrupted review must not make shell snapshot fresh-start ready")
	}
	if !strings.Contains(strings.ToLower(snapshot.Recovery.Reason), "reviewed") {
		t.Fatalf("expected reviewed interrupted-lineage reason, got %q", snapshot.Recovery.Reason)
	}
	found := false
	for _, evt := range snapshot.RecentProofs {
		if evt.Type == proof.EventInterruptedRunReviewed {
			found = true
			if evt.Summary != "Interrupted run reviewed" {
				t.Fatalf("unexpected interrupted review proof summary %q", evt.Summary)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected interrupted review proof highlight in shell snapshot")
	}
}

func TestShellSnapshotInterruptedResumeShowsContinueExecutionRequiredState(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{
		TaskID:             string(taskID),
		Action:             "interrupt",
		RunID:              startOut.RunID,
		InterruptionReason: "operator interrupted",
	}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}
	if _, err := coord.ExecuteInterruptedResume(context.Background(), ExecuteInterruptedResumeRequest{
		TaskID:  string(taskID),
		Summary: "operator resumed interrupted lineage",
	}); err != nil {
		t.Fatalf("execute interrupted resume: %v", err)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Phase != string(phase.PhaseBriefReady) {
		t.Fatalf("expected shell snapshot phase %s after interrupted resume, got %s", phase.PhaseBriefReady, snapshot.Phase)
	}
	if snapshot.Recovery == nil || snapshot.Recovery.RecoveryClass != RecoveryClassContinueExecutionRequired {
		t.Fatalf("expected continue-execution-required shell recovery, got %+v", snapshot.Recovery)
	}
	if snapshot.Recovery.RecommendedAction != RecoveryActionExecuteContinueRecovery {
		t.Fatalf("expected execute-continue-recovery shell action, got %+v", snapshot.Recovery)
	}
	if snapshot.Recovery.ReadyForNextRun {
		t.Fatal("interrupted resume must not make shell snapshot fresh-start ready")
	}
	if !strings.Contains(strings.ToLower(snapshot.Recovery.Reason), "interrupted") {
		t.Fatalf("expected interrupted-lineage reason in shell snapshot, got %q", snapshot.Recovery.Reason)
	}
	found := false
	for _, evt := range snapshot.RecentProofs {
		if evt.Type == proof.EventInterruptedRunResumeExecuted {
			found = true
			if evt.Summary != "Interrupted lineage continuation selected" {
				t.Fatalf("unexpected interrupted resume proof summary %q", evt.Summary)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected interrupted resume proof highlight in shell snapshot")
	}
}

func TestShellSnapshotFailedRunDoesNotOverclaimReadiness(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("run start: %v", err)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Checkpoint == nil {
		t.Fatal("expected checkpoint summary")
	}
	if snapshot.Checkpoint.IsResumable {
		t.Fatalf("failed run checkpoint should not look resumable: %+v", snapshot.Checkpoint)
	}
	if snapshot.Recovery == nil {
		t.Fatal("expected recovery summary")
	}
	if snapshot.Recovery.RecoveryClass != RecoveryClassFailedRunReviewRequired {
		t.Fatalf("expected failed-run recovery class, got %s", snapshot.Recovery.RecoveryClass)
	}
	if snapshot.Recovery.ReadyForNextRun {
		t.Fatal("failed run should not look ready for next run")
	}
	if snapshot.Recovery.RecommendedAction != RecoveryActionInspectFailedRun {
		t.Fatalf("expected inspect-failed-run action, got %s", snapshot.Recovery.RecommendedAction)
	}
}

func TestShellSnapshotAcceptedHandoffLaunchReadyState(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "launch ready shell snapshot",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  createOut.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
	}); err != nil {
		t.Fatalf("accept handoff: %v", err)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Recovery == nil || snapshot.Recovery.RecoveryClass != RecoveryClassAcceptedHandoffLaunchReady {
		t.Fatalf("expected accepted handoff launch-ready recovery, got %+v", snapshot.Recovery)
	}
	if !snapshot.Recovery.ReadyForHandoffLaunch {
		t.Fatal("expected handoff launch readiness")
	}
	if snapshot.HandoffContinuity == nil || snapshot.HandoffContinuity.State != HandoffContinuityStateAcceptedNotLaunched {
		t.Fatalf("expected accepted-not-launched handoff continuity, got %+v", snapshot.HandoffContinuity)
	}
	if snapshot.LaunchControl == nil || snapshot.LaunchControl.State != LaunchControlStateNotRequested {
		t.Fatalf("expected not-requested launch control state, got %+v", snapshot.LaunchControl)
	}
	if snapshot.LaunchControl.RetryDisposition != LaunchRetryDispositionAllowed {
		t.Fatalf("expected allowed launch retry disposition, got %+v", snapshot.LaunchControl)
	}
}

func TestShellSnapshotPendingLaunchUnknownOutcomeState(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "pending launch shell snapshot",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  createOut.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
	}); err != nil {
		t.Fatalf("accept handoff: %v", err)
	}

	now := time.Now().UTC()
	if err := store.Handoffs().CreateLaunch(handoff.Launch{
		Version:      1,
		AttemptID:    "hlc_shell_pending",
		HandoffID:    createOut.HandoffID,
		TaskID:       taskID,
		TargetWorker: rundomain.WorkerKindClaude,
		Status:       handoff.LaunchStatusRequested,
		PayloadHash:  "shell_pending_hash",
		RequestedAt:  now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("create launch record: %v", err)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Recovery == nil || snapshot.Recovery.RecoveryClass != RecoveryClassHandoffLaunchPendingOutcome {
		t.Fatalf("expected pending launch recovery class, got %+v", snapshot.Recovery)
	}
	if snapshot.Recovery.ReadyForNextRun {
		t.Fatal("pending launch should not look ready for next run")
	}
	if snapshot.LaunchControl == nil || snapshot.LaunchControl.State != LaunchControlStateRequestedOutcomeUnknown {
		t.Fatalf("expected requested-outcome-unknown launch control, got %+v", snapshot.LaunchControl)
	}
	if snapshot.LaunchControl.RetryDisposition != LaunchRetryDispositionBlocked {
		t.Fatalf("expected blocked launch retry, got %+v", snapshot.LaunchControl)
	}
	if snapshot.Launch == nil || snapshot.Launch.Status != handoff.LaunchStatusRequested {
		t.Fatalf("expected latest launch summary, got %+v", snapshot.Launch)
	}
}

func TestShellSnapshotCompletedLaunchStateDoesNotOverclaimDownstreamCompletion(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherSuccess()
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "completed launch shell snapshot",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  createOut.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
	}); err != nil {
		t.Fatalf("accept handoff: %v", err)
	}
	if _, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	}); err != nil {
		t.Fatalf("launch handoff: %v", err)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Recovery == nil || snapshot.Recovery.RecoveryClass != RecoveryClassHandoffLaunchCompleted {
		t.Fatalf("expected completed launch recovery class, got %+v", snapshot.Recovery)
	}
	if snapshot.Recovery.RecommendedAction != RecoveryActionMonitorLaunchedHandoff {
		t.Fatalf("expected monitor launched handoff action, got %+v", snapshot.Recovery)
	}
	if snapshot.LaunchControl == nil || snapshot.LaunchControl.State != LaunchControlStateCompleted {
		t.Fatalf("expected completed launch control, got %+v", snapshot.LaunchControl)
	}
	if snapshot.LaunchControl.RetryDisposition != LaunchRetryDispositionBlocked {
		t.Fatalf("expected blocked retry after durable completion, got %+v", snapshot.LaunchControl)
	}
	if snapshot.Acknowledgment == nil {
		t.Fatal("expected persisted acknowledgment after completed launch")
	}
	if snapshot.HandoffContinuity == nil || snapshot.HandoffContinuity.State != HandoffContinuityStateLaunchCompletedAckSeen {
		t.Fatalf("expected captured-ack handoff continuity, got %+v", snapshot.HandoffContinuity)
	}
	if snapshot.HandoffContinuity.DownstreamContinuationProven {
		t.Fatal("completed launch shell snapshot must not claim downstream continuation proven")
	}
	if snapshot.Recovery.Reason == "" || !strings.Contains(strings.ToLower(snapshot.Recovery.Reason), "launch") {
		t.Fatalf("expected launch-specific recovery reason, got %+v", snapshot.Recovery)
	}
}

func TestShellSnapshotCompletedLaunchWithUnavailableAcknowledgmentShowsUnprovenContinuity(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherUnusableOutput())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "unavailable acknowledgment shell snapshot",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  createOut.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
	}); err != nil {
		t.Fatalf("accept handoff: %v", err)
	}
	if _, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	}); err != nil {
		t.Fatalf("launch handoff: %v", err)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.HandoffContinuity == nil || snapshot.HandoffContinuity.State != HandoffContinuityStateLaunchCompletedAckEmpty {
		t.Fatalf("expected unavailable-ack handoff continuity in shell snapshot, got %+v", snapshot.HandoffContinuity)
	}
	if snapshot.HandoffContinuity.DownstreamContinuationProven {
		t.Fatal("unavailable acknowledgment shell snapshot must not claim downstream continuation proven")
	}
}

func TestShellSnapshotProofOfLifeFollowThroughShowsStrongerButIncompleteClaudeContinuity(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "proof-of-life shell snapshot",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  createOut.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
	}); err != nil {
		t.Fatalf("accept handoff: %v", err)
	}
	if _, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	}); err != nil {
		t.Fatalf("launch handoff: %v", err)
	}
	if _, err := coord.RecordHandoffFollowThrough(context.Background(), RecordHandoffFollowThroughRequest{
		TaskID:  string(taskID),
		Kind:    handoff.FollowThroughProofOfLifeObserved,
		Summary: "later Claude proof of life observed",
	}); err != nil {
		t.Fatalf("record handoff follow-through: %v", err)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.HandoffContinuity == nil || snapshot.HandoffContinuity.State != HandoffContinuityStateFollowThroughProofOfLife {
		t.Fatalf("expected proof-of-life handoff continuity in shell snapshot, got %+v", snapshot.HandoffContinuity)
	}
	if !snapshot.HandoffContinuity.DownstreamContinuationProven {
		t.Fatal("proof-of-life shell snapshot should reflect downstream continuation proven")
	}
	if snapshot.FollowThrough == nil || snapshot.FollowThrough.Kind != handoff.FollowThroughProofOfLifeObserved {
		t.Fatalf("expected shell follow-through summary, got %+v", snapshot.FollowThrough)
	}
	if snapshot.Recovery == nil || snapshot.Recovery.RecoveryClass != RecoveryClassHandoffLaunchCompleted {
		t.Fatalf("expected launched-handoff recovery after proof-of-life evidence, got %+v", snapshot.Recovery)
	}
	if snapshot.Recovery.ReadyForNextRun {
		t.Fatal("proof-of-life shell snapshot must not imply fresh next-run readiness")
	}
}

func TestShellSnapshotStalledFollowThroughRequiresReview(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "stalled follow-through shell snapshot",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  createOut.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
	}); err != nil {
		t.Fatalf("accept handoff: %v", err)
	}
	if _, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	}); err != nil {
		t.Fatalf("launch handoff: %v", err)
	}
	if _, err := coord.RecordHandoffFollowThrough(context.Background(), RecordHandoffFollowThroughRequest{
		TaskID:  string(taskID),
		Kind:    handoff.FollowThroughStalledReviewRequired,
		Summary: "Claude follow-through appears stalled",
	}); err != nil {
		t.Fatalf("record stalled handoff follow-through: %v", err)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Recovery == nil || snapshot.Recovery.RecoveryClass != RecoveryClassHandoffFollowThroughReviewRequired {
		t.Fatalf("expected stalled follow-through review-required recovery, got %+v", snapshot.Recovery)
	}
	if snapshot.Recovery.RecommendedAction != RecoveryActionReviewHandoffFollowThrough {
		t.Fatalf("expected review-handoff-follow-through action, got %+v", snapshot.Recovery)
	}
	if snapshot.HandoffContinuity == nil || snapshot.HandoffContinuity.State != HandoffContinuityStateFollowThroughStalled {
		t.Fatalf("expected stalled follow-through handoff continuity, got %+v", snapshot.HandoffContinuity)
	}
}

func TestShellSnapshotBrokenContinuityExposesRepairIssues(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	lastEvent, err := latestEventID(store, taskID)
	if err != nil {
		t.Fatalf("latest event: %v", err)
	}
	bad := checkpoint.Checkpoint{
		Version:            1,
		CheckpointID:       common.CheckpointID("chk_shell_bad_brief"),
		TaskID:             taskID,
		CreatedAt:          time.Now().UTC().Add(5 * time.Second),
		Trigger:            checkpoint.TriggerManual,
		CapsuleVersion:     caps.Version,
		Phase:              caps.CurrentPhase,
		Anchor:             anchorFromCapsule(caps),
		IntentID:           caps.CurrentIntentID,
		BriefID:            common.BriefID("brf_missing_shell"),
		ContextPackID:      "",
		LastEventID:        lastEvent,
		PendingDecisionIDs: []common.DecisionID{},
		ResumeDescriptor:   "broken shell continuity test",
		IsResumable:        true,
	}
	if err := store.Checkpoints().Create(bad); err != nil {
		t.Fatalf("create bad checkpoint: %v", err)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Recovery == nil || snapshot.Recovery.RecoveryClass != RecoveryClassRepairRequired {
		t.Fatalf("expected repair-required recovery class, got %+v", snapshot.Recovery)
	}
	if len(snapshot.Recovery.Issues) == 0 {
		t.Fatal("expected continuity issues in shell snapshot")
	}
	if snapshot.LaunchControl == nil || snapshot.LaunchControl.State != LaunchControlStateNotApplicable {
		t.Fatalf("expected non-applicable launch control in broken continuity state, got %+v", snapshot.LaunchControl)
	}
}

```


## internal/ipc/types.go

```go
package ipc

import "encoding/json"

type Method string

const (
	MethodStartTask                  Method = "task.start"
	MethodResolveShellTaskForRepo    Method = "task.shell.resolve"
	MethodSendMessage                Method = "task.message"
	MethodContinueTask               Method = "task.continue"
	MethodRecordRecoveryAction       Method = "task.recovery.record"
	MethodReviewInterruptedRun       Method = "task.recovery.review_interrupted"
	MethodExecuteRebrief             Method = "task.recovery.rebrief"
	MethodExecuteInterruptedResume   Method = "task.recovery.resume_interrupted"
	MethodExecuteContinueRecovery    Method = "task.recovery.continue"
	MethodTaskRun                    Method = "task.run"
	MethodTaskStatus                 Method = "task.status"
	MethodTaskInspect                Method = "task.inspect"
	MethodTaskShellSnapshot          Method = "task.shell.snapshot"
	MethodTaskShellLifecycle         Method = "task.shell.lifecycle"
	MethodTaskShellSessionReport     Method = "task.shell.session.report"
	MethodTaskShellSessions          Method = "task.shell.sessions"
	MethodCreateCheckpoint           Method = "task.checkpoint"
	MethodCreateHandoff              Method = "task.handoff.create"
	MethodAcceptHandoff              Method = "task.handoff.accept"
	MethodLaunchHandoff              Method = "task.handoff.launch"
	MethodRecordHandoffFollowThrough Method = "task.handoff.followthrough.record"
	MethodApproveDecision            Method = "task.approve"
	MethodRejectDecision             Method = "task.reject"
)

type Request struct {
	RequestID string          `json:"request_id"`
	Method    Method          `json:"method"`
	Payload   json.RawMessage `json:"payload"`
}

type Response struct {
	RequestID string          `json:"request_id"`
	OK        bool            `json:"ok"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Error     *ErrorPayload   `json:"error,omitempty"`
}

type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

```


## internal/ipc/payloads.go

```go
package ipc

import (
	"time"

	"tuku/internal/domain/brief"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/intent"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/run"
)

type RepoAnchor struct {
	RepoRoot         string    `json:"repo_root"`
	Branch           string    `json:"branch"`
	HeadSHA          string    `json:"head_sha"`
	WorkingTreeDirty bool      `json:"working_tree_dirty"`
	CapturedAt       time.Time `json:"captured_at"`
}

type StartTaskRequest struct {
	Goal     string `json:"goal"`
	RepoRoot string `json:"repo_root"`
}

type StartTaskResponse struct {
	TaskID            common.TaskID         `json:"task_id"`
	ConversationID    common.ConversationID `json:"conversation_id"`
	Phase             phase.Phase           `json:"phase"`
	RepoAnchor        RepoAnchor            `json:"repo_anchor"`
	CanonicalResponse string                `json:"canonical_response"`
}

type ResolveShellTaskForRepoRequest struct {
	RepoRoot    string `json:"repo_root"`
	DefaultGoal string `json:"default_goal,omitempty"`
}

type ResolveShellTaskForRepoResponse struct {
	TaskID   common.TaskID `json:"task_id"`
	RepoRoot string        `json:"repo_root"`
	Created  bool          `json:"created"`
}

type TaskMessageRequest struct {
	TaskID  common.TaskID `json:"task_id"`
	Message string        `json:"message"`
}

type TaskMessageResponse struct {
	TaskID            common.TaskID  `json:"task_id"`
	Phase             phase.Phase    `json:"phase"`
	IntentClass       string         `json:"intent_class"`
	BriefID           common.BriefID `json:"brief_id"`
	BriefHash         string         `json:"brief_hash"`
	RepoAnchor        RepoAnchor     `json:"repo_anchor"`
	CanonicalResponse string         `json:"canonical_response"`
}

type TaskStatusRequest struct {
	TaskID common.TaskID `json:"task_id"`
}

type TaskRecoveryActionRecord struct {
	ActionID        string              `json:"action_id"`
	TaskID          common.TaskID       `json:"task_id"`
	Kind            string              `json:"kind"`
	RunID           common.RunID        `json:"run_id,omitempty"`
	CheckpointID    common.CheckpointID `json:"checkpoint_id,omitempty"`
	HandoffID       string              `json:"handoff_id,omitempty"`
	LaunchAttemptID string              `json:"launch_attempt_id,omitempty"`
	Summary         string              `json:"summary,omitempty"`
	Notes           []string            `json:"notes,omitempty"`
	CreatedAtUnixMs int64               `json:"created_at_unix_ms,omitempty"`
}

type TaskStatusResponse struct {
	TaskID                      common.TaskID             `json:"task_id"`
	ConversationID              common.ConversationID     `json:"conversation_id"`
	Goal                        string                    `json:"goal"`
	Phase                       phase.Phase               `json:"phase"`
	Status                      string                    `json:"status"`
	CurrentIntentID             common.IntentID           `json:"current_intent_id"`
	CurrentIntentClass          string                    `json:"current_intent_class,omitempty"`
	CurrentIntentSummary        string                    `json:"current_intent_summary,omitempty"`
	CurrentBriefID              common.BriefID            `json:"current_brief_id,omitempty"`
	CurrentBriefHash            string                    `json:"current_brief_hash,omitempty"`
	LatestRunID                 common.RunID              `json:"latest_run_id,omitempty"`
	LatestRunStatus             run.Status                `json:"latest_run_status,omitempty"`
	LatestRunSummary            string                    `json:"latest_run_summary,omitempty"`
	RepoAnchor                  RepoAnchor                `json:"repo_anchor"`
	LatestCheckpointID          common.CheckpointID       `json:"latest_checkpoint_id,omitempty"`
	LatestCheckpointAtUnixMs    int64                     `json:"latest_checkpoint_at_unix_ms,omitempty"`
	LatestCheckpointTrigger     string                    `json:"latest_checkpoint_trigger,omitempty"`
	CheckpointResumable         bool                      `json:"checkpoint_resumable,omitempty"`
	ResumeDescriptor            string                    `json:"resume_descriptor,omitempty"`
	LatestLaunchAttemptID       string                    `json:"latest_launch_attempt_id,omitempty"`
	LatestLaunchID              string                    `json:"latest_launch_id,omitempty"`
	LatestLaunchStatus          string                    `json:"latest_launch_status,omitempty"`
	LatestAcknowledgmentID      string                    `json:"latest_acknowledgment_id,omitempty"`
	LatestAcknowledgmentStatus  string                    `json:"latest_acknowledgment_status,omitempty"`
	LatestAcknowledgmentSummary string                    `json:"latest_acknowledgment_summary,omitempty"`
	LatestFollowThroughID       string                    `json:"latest_follow_through_id,omitempty"`
	LatestFollowThroughKind     string                    `json:"latest_follow_through_kind,omitempty"`
	LatestFollowThroughSummary  string                    `json:"latest_follow_through_summary,omitempty"`
	LaunchControlState          string                    `json:"launch_control_state,omitempty"`
	LaunchRetryDisposition      string                    `json:"launch_retry_disposition,omitempty"`
	LaunchControlReason         string                    `json:"launch_control_reason,omitempty"`
	HandoffContinuityState      string                    `json:"handoff_continuity_state,omitempty"`
	HandoffContinuityReason     string                    `json:"handoff_continuity_reason,omitempty"`
	HandoffContinuationProven   bool                      `json:"handoff_continuation_proven"`
	IsResumable                 bool                      `json:"is_resumable,omitempty"`
	RecoveryClass               string                    `json:"recovery_class,omitempty"`
	RecommendedAction           string                    `json:"recommended_action,omitempty"`
	ReadyForNextRun             bool                      `json:"ready_for_next_run,omitempty"`
	ReadyForHandoffLaunch       bool                      `json:"ready_for_handoff_launch,omitempty"`
	RecoveryReason              string                    `json:"recovery_reason,omitempty"`
	LatestRecoveryAction        *TaskRecoveryActionRecord `json:"latest_recovery_action,omitempty"`
	LastEventType               string                    `json:"last_event_type,omitempty"`
	LastEventID                 common.EventID            `json:"last_event_id,omitempty"`
	LastEventAtUnixMs           int64                     `json:"last_event_at_unix_ms,omitempty"`
}

type TaskInspectRequest struct {
	TaskID common.TaskID `json:"task_id"`
}

type TaskRecoveryIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type TaskRecoveryAssessment struct {
	TaskID                 common.TaskID             `json:"task_id"`
	ContinuityOutcome      string                    `json:"continuity_outcome"`
	RecoveryClass          string                    `json:"recovery_class"`
	RecommendedAction      string                    `json:"recommended_action"`
	ReadyForNextRun        bool                      `json:"ready_for_next_run"`
	ReadyForHandoffLaunch  bool                      `json:"ready_for_handoff_launch"`
	RequiresDecision       bool                      `json:"requires_decision,omitempty"`
	RequiresRepair         bool                      `json:"requires_repair,omitempty"`
	RequiresReview         bool                      `json:"requires_review,omitempty"`
	RequiresReconciliation bool                      `json:"requires_reconciliation,omitempty"`
	DriftClass             checkpoint.DriftClass     `json:"drift_class,omitempty"`
	Reason                 string                    `json:"reason,omitempty"`
	CheckpointID           common.CheckpointID       `json:"checkpoint_id,omitempty"`
	RunID                  common.RunID              `json:"run_id,omitempty"`
	HandoffID              string                    `json:"handoff_id,omitempty"`
	HandoffStatus          string                    `json:"handoff_status,omitempty"`
	LatestAction           *TaskRecoveryActionRecord `json:"latest_action,omitempty"`
	Issues                 []TaskRecoveryIssue       `json:"issues,omitempty"`
}

type TaskLaunchControl struct {
	TaskID           common.TaskID  `json:"task_id"`
	HandoffID        string         `json:"handoff_id,omitempty"`
	AttemptID        string         `json:"attempt_id,omitempty"`
	LaunchID         string         `json:"launch_id,omitempty"`
	State            string         `json:"state"`
	RetryDisposition string         `json:"retry_disposition"`
	Reason           string         `json:"reason,omitempty"`
	TargetWorker     run.WorkerKind `json:"target_worker,omitempty"`
	RequestedAt      time.Time      `json:"requested_at,omitempty"`
	CompletedAt      time.Time      `json:"completed_at,omitempty"`
	FailedAt         time.Time      `json:"failed_at,omitempty"`
}

type TaskHandoffContinuity struct {
	TaskID                       common.TaskID  `json:"task_id"`
	HandoffID                    string         `json:"handoff_id,omitempty"`
	TargetWorker                 run.WorkerKind `json:"target_worker,omitempty"`
	State                        string         `json:"state"`
	LaunchAttemptID              string         `json:"launch_attempt_id,omitempty"`
	LaunchID                     string         `json:"launch_id,omitempty"`
	LaunchStatus                 string         `json:"launch_status,omitempty"`
	AcknowledgmentID             string         `json:"acknowledgment_id,omitempty"`
	AcknowledgmentStatus         string         `json:"acknowledgment_status,omitempty"`
	AcknowledgmentSummary        string         `json:"acknowledgment_summary,omitempty"`
	FollowThroughID              string         `json:"follow_through_id,omitempty"`
	FollowThroughKind            string         `json:"follow_through_kind,omitempty"`
	FollowThroughSummary         string         `json:"follow_through_summary,omitempty"`
	DownstreamContinuationProven bool           `json:"downstream_continuation_proven"`
	Reason                       string         `json:"reason,omitempty"`
}

type TaskInspectResponse struct {
	TaskID                common.TaskID              `json:"task_id"`
	RepoAnchor            RepoAnchor                 `json:"repo_anchor"`
	Intent                *intent.State              `json:"intent,omitempty"`
	Brief                 *brief.ExecutionBrief      `json:"brief,omitempty"`
	Run                   *run.ExecutionRun          `json:"run,omitempty"`
	Checkpoint            *checkpoint.Checkpoint     `json:"checkpoint,omitempty"`
	Handoff               *handoff.Packet            `json:"handoff,omitempty"`
	Launch                *handoff.Launch            `json:"launch,omitempty"`
	Acknowledgment        *handoff.Acknowledgment    `json:"acknowledgment,omitempty"`
	FollowThrough         *handoff.FollowThrough     `json:"follow_through,omitempty"`
	LaunchControl         *TaskLaunchControl         `json:"launch_control,omitempty"`
	HandoffContinuity     *TaskHandoffContinuity     `json:"handoff_continuity,omitempty"`
	Recovery              *TaskRecoveryAssessment    `json:"recovery,omitempty"`
	LatestRecoveryAction  *TaskRecoveryActionRecord  `json:"latest_recovery_action,omitempty"`
	RecentRecoveryActions []TaskRecoveryActionRecord `json:"recent_recovery_actions,omitempty"`
}

type TaskRecordRecoveryActionRequest struct {
	TaskID  common.TaskID `json:"task_id"`
	Kind    string        `json:"kind"`
	Summary string        `json:"summary,omitempty"`
	Notes   []string      `json:"notes,omitempty"`
}

type TaskRecordRecoveryActionResponse struct {
	TaskID                common.TaskID            `json:"task_id"`
	Action                TaskRecoveryActionRecord `json:"action"`
	RecoveryClass         string                   `json:"recovery_class"`
	RecommendedAction     string                   `json:"recommended_action"`
	ReadyForNextRun       bool                     `json:"ready_for_next_run"`
	ReadyForHandoffLaunch bool                     `json:"ready_for_handoff_launch"`
	RecoveryReason        string                   `json:"recovery_reason,omitempty"`
	CanonicalResponse     string                   `json:"canonical_response"`
}

type TaskReviewInterruptedRunRequest struct {
	TaskID  common.TaskID `json:"task_id"`
	Summary string        `json:"summary,omitempty"`
	Notes   []string      `json:"notes,omitempty"`
}

type TaskRebriefRequest struct {
	TaskID common.TaskID `json:"task_id"`
}

type TaskInterruptedResumeRequest struct {
	TaskID  common.TaskID `json:"task_id"`
	Summary string        `json:"summary,omitempty"`
	Notes   []string      `json:"notes,omitempty"`
}

type TaskInterruptedResumeResponse struct {
	TaskID                common.TaskID            `json:"task_id"`
	BriefID               common.BriefID           `json:"brief_id"`
	BriefHash             string                   `json:"brief_hash"`
	Action                TaskRecoveryActionRecord `json:"action"`
	RecoveryClass         string                   `json:"recovery_class"`
	RecommendedAction     string                   `json:"recommended_action"`
	ReadyForNextRun       bool                     `json:"ready_for_next_run"`
	ReadyForHandoffLaunch bool                     `json:"ready_for_handoff_launch"`
	RecoveryReason        string                   `json:"recovery_reason,omitempty"`
	CanonicalResponse     string                   `json:"canonical_response"`
}

type TaskRebriefResponse struct {
	TaskID                common.TaskID  `json:"task_id"`
	PreviousBriefID       common.BriefID `json:"previous_brief_id"`
	BriefID               common.BriefID `json:"brief_id"`
	BriefHash             string         `json:"brief_hash"`
	RecoveryClass         string         `json:"recovery_class"`
	RecommendedAction     string         `json:"recommended_action"`
	ReadyForNextRun       bool           `json:"ready_for_next_run"`
	ReadyForHandoffLaunch bool           `json:"ready_for_handoff_launch"`
	RecoveryReason        string         `json:"recovery_reason,omitempty"`
	CanonicalResponse     string         `json:"canonical_response"`
}

type TaskContinueRecoveryRequest struct {
	TaskID common.TaskID `json:"task_id"`
}

type TaskContinueRecoveryResponse struct {
	TaskID                common.TaskID            `json:"task_id"`
	BriefID               common.BriefID           `json:"brief_id"`
	BriefHash             string                   `json:"brief_hash"`
	Action                TaskRecoveryActionRecord `json:"action"`
	RecoveryClass         string                   `json:"recovery_class"`
	RecommendedAction     string                   `json:"recommended_action"`
	ReadyForNextRun       bool                     `json:"ready_for_next_run"`
	ReadyForHandoffLaunch bool                     `json:"ready_for_handoff_launch"`
	RecoveryReason        string                   `json:"recovery_reason,omitempty"`
	CanonicalResponse     string                   `json:"canonical_response"`
}

type TaskHandoffFollowThroughRecordRequest struct {
	TaskID  common.TaskID `json:"task_id"`
	Kind    string        `json:"kind"`
	Summary string        `json:"summary,omitempty"`
	Notes   []string      `json:"notes,omitempty"`
}

type TaskHandoffFollowThroughRecordResponse struct {
	TaskID                common.TaskID          `json:"task_id"`
	Record                *handoff.FollowThrough `json:"record"`
	HandoffContinuity     *TaskHandoffContinuity `json:"handoff_continuity,omitempty"`
	RecoveryClass         string                 `json:"recovery_class,omitempty"`
	RecommendedAction     string                 `json:"recommended_action,omitempty"`
	ReadyForNextRun       bool                   `json:"ready_for_next_run"`
	ReadyForHandoffLaunch bool                   `json:"ready_for_handoff_launch"`
	RecoveryReason        string                 `json:"recovery_reason,omitempty"`
	CanonicalResponse     string                 `json:"canonical_response"`
}

type TaskShellSnapshotRequest struct {
	TaskID common.TaskID `json:"task_id"`
}

type TaskShellBrief struct {
	BriefID          common.BriefID `json:"brief_id"`
	Objective        string         `json:"objective"`
	NormalizedAction string         `json:"normalized_action"`
	Constraints      []string       `json:"constraints,omitempty"`
	DoneCriteria     []string       `json:"done_criteria,omitempty"`
}

type TaskShellRun struct {
	RunID              common.RunID   `json:"run_id"`
	WorkerKind         run.WorkerKind `json:"worker_kind"`
	Status             run.Status     `json:"status"`
	LastKnownSummary   string         `json:"last_known_summary,omitempty"`
	StartedAt          time.Time      `json:"started_at"`
	EndedAt            *time.Time     `json:"ended_at,omitempty"`
	InterruptionReason string         `json:"interruption_reason,omitempty"`
}

type TaskShellCheckpoint struct {
	CheckpointID     common.CheckpointID `json:"checkpoint_id"`
	Trigger          checkpoint.Trigger  `json:"trigger"`
	CreatedAt        time.Time           `json:"created_at"`
	ResumeDescriptor string              `json:"resume_descriptor,omitempty"`
	IsResumable      bool                `json:"is_resumable"`
}

type TaskShellHandoff struct {
	HandoffID    string         `json:"handoff_id"`
	Status       string         `json:"status"`
	SourceWorker run.WorkerKind `json:"source_worker"`
	TargetWorker run.WorkerKind `json:"target_worker"`
	Mode         string         `json:"mode"`
	Reason       string         `json:"reason,omitempty"`
	AcceptedBy   run.WorkerKind `json:"accepted_by,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}

type TaskShellLaunch struct {
	AttemptID         string    `json:"attempt_id,omitempty"`
	LaunchID          string    `json:"launch_id,omitempty"`
	Status            string    `json:"status,omitempty"`
	RequestedAt       time.Time `json:"requested_at,omitempty"`
	StartedAt         time.Time `json:"started_at,omitempty"`
	EndedAt           time.Time `json:"ended_at,omitempty"`
	Summary           string    `json:"summary,omitempty"`
	ErrorMessage      string    `json:"error_message,omitempty"`
	OutputArtifactRef string    `json:"output_artifact_ref,omitempty"`
}

type TaskShellAcknowledgment struct {
	Status    string    `json:"status"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

type TaskShellFollowThrough struct {
	RecordID        string    `json:"record_id,omitempty"`
	Kind            string    `json:"kind,omitempty"`
	Summary         string    `json:"summary,omitempty"`
	LaunchAttemptID string    `json:"launch_attempt_id,omitempty"`
	LaunchID        string    `json:"launch_id,omitempty"`
	CreatedAt       time.Time `json:"created_at,omitempty"`
}

type TaskShellHandoffContinuity struct {
	State                        string `json:"state"`
	Reason                       string `json:"reason,omitempty"`
	LaunchAttemptID              string `json:"launch_attempt_id,omitempty"`
	LaunchID                     string `json:"launch_id,omitempty"`
	AcknowledgmentID             string `json:"acknowledgment_id,omitempty"`
	AcknowledgmentStatus         string `json:"acknowledgment_status,omitempty"`
	AcknowledgmentSummary        string `json:"acknowledgment_summary,omitempty"`
	FollowThroughID              string `json:"follow_through_id,omitempty"`
	FollowThroughKind            string `json:"follow_through_kind,omitempty"`
	FollowThroughSummary         string `json:"follow_through_summary,omitempty"`
	DownstreamContinuationProven bool   `json:"downstream_continuation_proven"`
}

type TaskShellRecoveryIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type TaskShellRecovery struct {
	ContinuityOutcome      string                   `json:"continuity_outcome"`
	RecoveryClass          string                   `json:"recovery_class"`
	RecommendedAction      string                   `json:"recommended_action"`
	ReadyForNextRun        bool                     `json:"ready_for_next_run"`
	ReadyForHandoffLaunch  bool                     `json:"ready_for_handoff_launch"`
	RequiresDecision       bool                     `json:"requires_decision,omitempty"`
	RequiresRepair         bool                     `json:"requires_repair,omitempty"`
	RequiresReview         bool                     `json:"requires_review,omitempty"`
	RequiresReconciliation bool                     `json:"requires_reconciliation,omitempty"`
	DriftClass             checkpoint.DriftClass    `json:"drift_class,omitempty"`
	Reason                 string                   `json:"reason,omitempty"`
	CheckpointID           common.CheckpointID      `json:"checkpoint_id,omitempty"`
	RunID                  common.RunID             `json:"run_id,omitempty"`
	HandoffID              string                   `json:"handoff_id,omitempty"`
	HandoffStatus          string                   `json:"handoff_status,omitempty"`
	Issues                 []TaskShellRecoveryIssue `json:"issues,omitempty"`
}

type TaskShellLaunchControl struct {
	State            string         `json:"state"`
	RetryDisposition string         `json:"retry_disposition"`
	Reason           string         `json:"reason,omitempty"`
	HandoffID        string         `json:"handoff_id,omitempty"`
	AttemptID        string         `json:"attempt_id,omitempty"`
	LaunchID         string         `json:"launch_id,omitempty"`
	TargetWorker     run.WorkerKind `json:"target_worker,omitempty"`
	RequestedAt      time.Time      `json:"requested_at,omitempty"`
	CompletedAt      time.Time      `json:"completed_at,omitempty"`
	FailedAt         time.Time      `json:"failed_at,omitempty"`
}

type TaskShellProof struct {
	EventID   common.EventID `json:"event_id"`
	Type      string         `json:"type"`
	Summary   string         `json:"summary"`
	Timestamp time.Time      `json:"timestamp"`
}

type TaskShellConversation struct {
	Role      string    `json:"role"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type TaskShellSnapshotResponse struct {
	TaskID                  common.TaskID               `json:"task_id"`
	Goal                    string                      `json:"goal"`
	Phase                   string                      `json:"phase"`
	Status                  string                      `json:"status"`
	RepoAnchor              RepoAnchor                  `json:"repo_anchor"`
	IntentClass             string                      `json:"intent_class,omitempty"`
	IntentSummary           string                      `json:"intent_summary,omitempty"`
	Brief                   *TaskShellBrief             `json:"brief,omitempty"`
	Run                     *TaskShellRun               `json:"run,omitempty"`
	Checkpoint              *TaskShellCheckpoint        `json:"checkpoint,omitempty"`
	Handoff                 *TaskShellHandoff           `json:"handoff,omitempty"`
	Launch                  *TaskShellLaunch            `json:"launch,omitempty"`
	LaunchControl           *TaskShellLaunchControl     `json:"launch_control,omitempty"`
	Acknowledgment          *TaskShellAcknowledgment    `json:"acknowledgment,omitempty"`
	FollowThrough           *TaskShellFollowThrough     `json:"follow_through,omitempty"`
	HandoffContinuity       *TaskShellHandoffContinuity `json:"handoff_continuity,omitempty"`
	Recovery                *TaskShellRecovery          `json:"recovery,omitempty"`
	RecentProofs            []TaskShellProof            `json:"recent_proofs,omitempty"`
	RecentConversation      []TaskShellConversation     `json:"recent_conversation,omitempty"`
	LatestCanonicalResponse string                      `json:"latest_canonical_response,omitempty"`
}

type TaskShellLifecycleRequest struct {
	TaskID     common.TaskID `json:"task_id"`
	SessionID  string        `json:"session_id"`
	Kind       string        `json:"kind"`
	HostMode   string        `json:"host_mode"`
	HostState  string        `json:"host_state"`
	Note       string        `json:"note,omitempty"`
	InputLive  bool          `json:"input_live"`
	ExitCode   *int          `json:"exit_code,omitempty"`
	PaneWidth  int           `json:"pane_width,omitempty"`
	PaneHeight int           `json:"pane_height,omitempty"`
}

type TaskShellLifecycleResponse struct {
	TaskID common.TaskID `json:"task_id"`
}

type TaskShellSessionRecord struct {
	SessionID        string        `json:"session_id"`
	TaskID           common.TaskID `json:"task_id"`
	WorkerPreference string        `json:"worker_preference,omitempty"`
	ResolvedWorker   string        `json:"resolved_worker,omitempty"`
	WorkerSessionID  string        `json:"worker_session_id,omitempty"`
	AttachCapability string        `json:"attach_capability,omitempty"`
	HostMode         string        `json:"host_mode,omitempty"`
	HostState        string        `json:"host_state,omitempty"`
	SessionClass     string        `json:"session_class,omitempty"`
	StartedAt        time.Time     `json:"started_at"`
	LastUpdatedAt    time.Time     `json:"last_updated_at"`
	Active           bool          `json:"active"`
	Note             string        `json:"note,omitempty"`
}

type TaskShellSessionReportRequest struct {
	TaskID           common.TaskID `json:"task_id"`
	SessionID        string        `json:"session_id"`
	WorkerPreference string        `json:"worker_preference,omitempty"`
	ResolvedWorker   string        `json:"resolved_worker,omitempty"`
	WorkerSessionID  string        `json:"worker_session_id,omitempty"`
	AttachCapability string        `json:"attach_capability,omitempty"`
	HostMode         string        `json:"host_mode,omitempty"`
	HostState        string        `json:"host_state,omitempty"`
	StartedAt        time.Time     `json:"started_at"`
	Active           bool          `json:"active"`
	Note             string        `json:"note,omitempty"`
}

type TaskShellSessionReportResponse struct {
	TaskID  common.TaskID          `json:"task_id"`
	Session TaskShellSessionRecord `json:"session"`
}

type TaskShellSessionsRequest struct {
	TaskID common.TaskID `json:"task_id"`
}

type TaskShellSessionsResponse struct {
	TaskID   common.TaskID            `json:"task_id"`
	Sessions []TaskShellSessionRecord `json:"sessions,omitempty"`
}

type TaskRunRequest struct {
	TaskID             common.TaskID `json:"task_id"`
	Action             string        `json:"action,omitempty"` // start|complete|interrupt
	Mode               string        `json:"mode,omitempty"`   // real|noop
	RunID              common.RunID  `json:"run_id,omitempty"`
	SimulateInterrupt  bool          `json:"simulate_interrupt,omitempty"`
	InterruptionReason string        `json:"interruption_reason,omitempty"`
}

type TaskRunResponse struct {
	TaskID            common.TaskID `json:"task_id"`
	RunID             common.RunID  `json:"run_id"`
	RunStatus         run.Status    `json:"run_status"`
	Phase             phase.Phase   `json:"phase"`
	CanonicalResponse string        `json:"canonical_response"`
}

type TaskContinueRequest struct {
	TaskID common.TaskID `json:"task_id"`
}

type TaskContinueResponse struct {
	TaskID                common.TaskID         `json:"task_id"`
	Outcome               string                `json:"outcome"`
	DriftClass            checkpoint.DriftClass `json:"drift_class"`
	Phase                 phase.Phase           `json:"phase"`
	RunID                 common.RunID          `json:"run_id,omitempty"`
	CheckpointID          common.CheckpointID   `json:"checkpoint_id,omitempty"`
	ResumeDescriptor      string                `json:"resume_descriptor,omitempty"`
	RecoveryClass         string                `json:"recovery_class,omitempty"`
	RecommendedAction     string                `json:"recommended_action,omitempty"`
	ReadyForNextRun       bool                  `json:"ready_for_next_run,omitempty"`
	ReadyForHandoffLaunch bool                  `json:"ready_for_handoff_launch,omitempty"`
	RecoveryReason        string                `json:"recovery_reason,omitempty"`
	CanonicalResponse     string                `json:"canonical_response"`
}

type TaskCheckpointRequest struct {
	TaskID common.TaskID `json:"task_id"`
}

type TaskCheckpointResponse struct {
	TaskID            common.TaskID       `json:"task_id"`
	CheckpointID      common.CheckpointID `json:"checkpoint_id"`
	Trigger           checkpoint.Trigger  `json:"trigger"`
	IsResumable       bool                `json:"is_resumable"`
	CanonicalResponse string              `json:"canonical_response"`
}

type TaskHandoffCreateRequest struct {
	TaskID       common.TaskID  `json:"task_id"`
	TargetWorker run.WorkerKind `json:"target_worker,omitempty"`
	Reason       string         `json:"reason,omitempty"`
	Mode         handoff.Mode   `json:"mode,omitempty"`
	Notes        []string       `json:"notes,omitempty"`
}

type TaskHandoffCreateResponse struct {
	TaskID            common.TaskID       `json:"task_id"`
	HandoffID         string              `json:"handoff_id"`
	SourceWorker      run.WorkerKind      `json:"source_worker"`
	TargetWorker      run.WorkerKind      `json:"target_worker"`
	Status            string              `json:"status"`
	CheckpointID      common.CheckpointID `json:"checkpoint_id,omitempty"`
	BriefID           common.BriefID      `json:"brief_id,omitempty"`
	CanonicalResponse string              `json:"canonical_response"`
	Packet            *handoff.Packet     `json:"packet,omitempty"`
}

type TaskHandoffAcceptRequest struct {
	TaskID     common.TaskID  `json:"task_id"`
	HandoffID  string         `json:"handoff_id"`
	AcceptedBy run.WorkerKind `json:"accepted_by,omitempty"`
	Notes      []string       `json:"notes,omitempty"`
}

type TaskHandoffAcceptResponse struct {
	TaskID            common.TaskID `json:"task_id"`
	HandoffID         string        `json:"handoff_id"`
	Status            string        `json:"status"`
	CanonicalResponse string        `json:"canonical_response"`
}

type TaskHandoffLaunchRequest struct {
	TaskID       common.TaskID  `json:"task_id"`
	HandoffID    string         `json:"handoff_id,omitempty"`
	TargetWorker run.WorkerKind `json:"target_worker,omitempty"`
}

type TaskHandoffLaunchResponse struct {
	TaskID            common.TaskID          `json:"task_id"`
	HandoffID         string                 `json:"handoff_id"`
	TargetWorker      run.WorkerKind         `json:"target_worker"`
	LaunchStatus      string                 `json:"launch_status"`
	LaunchID          string                 `json:"launch_id,omitempty"`
	CanonicalResponse string                 `json:"canonical_response"`
	Payload           *handoff.LaunchPayload `json:"payload,omitempty"`
}

```


## internal/runtime/daemon/service.go

```go
package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"tuku/internal/domain/handoff"
	"tuku/internal/domain/recoveryaction"
	"tuku/internal/domain/shellsession"
	"tuku/internal/ipc"
	"tuku/internal/orchestrator"
)

func ipcRecoveryActionRecord(in *recoveryaction.Record) *ipc.TaskRecoveryActionRecord {
	if in == nil {
		return nil
	}
	createdAt := int64(0)
	if !in.CreatedAt.IsZero() {
		createdAt = in.CreatedAt.UnixMilli()
	}
	return &ipc.TaskRecoveryActionRecord{
		ActionID:        in.ActionID,
		TaskID:          in.TaskID,
		Kind:            string(in.Kind),
		RunID:           in.RunID,
		CheckpointID:    in.CheckpointID,
		HandoffID:       in.HandoffID,
		LaunchAttemptID: in.LaunchAttemptID,
		Summary:         in.Summary,
		Notes:           append([]string{}, in.Notes...),
		CreatedAtUnixMs: createdAt,
	}
}

func ipcRecoveryAssessment(in *orchestrator.RecoveryAssessment) *ipc.TaskRecoveryAssessment {
	if in == nil {
		return nil
	}
	out := &ipc.TaskRecoveryAssessment{
		TaskID:                 in.TaskID,
		ContinuityOutcome:      string(in.ContinuityOutcome),
		RecoveryClass:          string(in.RecoveryClass),
		RecommendedAction:      string(in.RecommendedAction),
		ReadyForNextRun:        in.ReadyForNextRun,
		ReadyForHandoffLaunch:  in.ReadyForHandoffLaunch,
		RequiresDecision:       in.RequiresDecision,
		RequiresRepair:         in.RequiresRepair,
		RequiresReview:         in.RequiresReview,
		RequiresReconciliation: in.RequiresReconciliation,
		DriftClass:             in.DriftClass,
		Reason:                 in.Reason,
		CheckpointID:           in.CheckpointID,
		RunID:                  in.RunID,
		HandoffID:              in.HandoffID,
		HandoffStatus:          string(in.HandoffStatus),
		LatestAction:           ipcRecoveryActionRecord(in.LatestAction),
	}
	if len(in.Issues) > 0 {
		out.Issues = make([]ipc.TaskRecoveryIssue, 0, len(in.Issues))
		for _, issue := range in.Issues {
			out.Issues = append(out.Issues, ipc.TaskRecoveryIssue{
				Code:    issue.Code,
				Message: issue.Message,
			})
		}
	}
	return out
}

func ipcLaunchControl(in *orchestrator.LaunchControl) *ipc.TaskLaunchControl {
	if in == nil {
		return nil
	}
	return &ipc.TaskLaunchControl{
		TaskID:           in.TaskID,
		HandoffID:        in.HandoffID,
		AttemptID:        in.AttemptID,
		LaunchID:         in.LaunchID,
		State:            string(in.State),
		RetryDisposition: string(in.RetryDisposition),
		Reason:           in.Reason,
		TargetWorker:     in.TargetWorker,
		RequestedAt:      in.RequestedAt,
		CompletedAt:      in.CompletedAt,
		FailedAt:         in.FailedAt,
	}
}

func ipcShellRecovery(in *orchestrator.ShellRecoverySummary) *ipc.TaskShellRecovery {
	if in == nil {
		return nil
	}
	out := &ipc.TaskShellRecovery{
		ContinuityOutcome:      string(in.ContinuityOutcome),
		RecoveryClass:          string(in.RecoveryClass),
		RecommendedAction:      string(in.RecommendedAction),
		ReadyForNextRun:        in.ReadyForNextRun,
		ReadyForHandoffLaunch:  in.ReadyForHandoffLaunch,
		RequiresDecision:       in.RequiresDecision,
		RequiresRepair:         in.RequiresRepair,
		RequiresReview:         in.RequiresReview,
		RequiresReconciliation: in.RequiresReconciliation,
		DriftClass:             in.DriftClass,
		Reason:                 in.Reason,
		CheckpointID:           in.CheckpointID,
		RunID:                  in.RunID,
		HandoffID:              in.HandoffID,
		HandoffStatus:          string(in.HandoffStatus),
	}
	if len(in.Issues) > 0 {
		out.Issues = make([]ipc.TaskShellRecoveryIssue, 0, len(in.Issues))
		for _, issue := range in.Issues {
			out.Issues = append(out.Issues, ipc.TaskShellRecoveryIssue{
				Code:    issue.Code,
				Message: issue.Message,
			})
		}
	}
	return out
}

func ipcShellLaunchControl(in *orchestrator.ShellLaunchControlSummary) *ipc.TaskShellLaunchControl {
	if in == nil {
		return nil
	}
	return &ipc.TaskShellLaunchControl{
		State:            string(in.State),
		RetryDisposition: string(in.RetryDisposition),
		Reason:           in.Reason,
		HandoffID:        in.HandoffID,
		AttemptID:        in.AttemptID,
		LaunchID:         in.LaunchID,
		TargetWorker:     in.TargetWorker,
		RequestedAt:      in.RequestedAt,
		CompletedAt:      in.CompletedAt,
		FailedAt:         in.FailedAt,
	}
}

func ipcHandoffContinuity(in *orchestrator.HandoffContinuity) *ipc.TaskHandoffContinuity {
	if in == nil {
		return nil
	}
	return &ipc.TaskHandoffContinuity{
		TaskID:                       in.TaskID,
		HandoffID:                    in.HandoffID,
		TargetWorker:                 in.TargetWorker,
		State:                        string(in.State),
		LaunchAttemptID:              in.LaunchAttemptID,
		LaunchID:                     in.LaunchID,
		LaunchStatus:                 string(in.LaunchStatus),
		AcknowledgmentID:             in.AcknowledgmentID,
		AcknowledgmentStatus:         string(in.AcknowledgmentStatus),
		AcknowledgmentSummary:        in.AcknowledgmentSummary,
		FollowThroughID:              in.FollowThroughID,
		FollowThroughKind:            string(in.FollowThroughKind),
		FollowThroughSummary:         in.FollowThroughSummary,
		DownstreamContinuationProven: in.DownstreamContinuationProven,
		Reason:                       in.Reason,
	}
}

func ipcShellHandoffContinuity(in *orchestrator.ShellHandoffContinuitySummary) *ipc.TaskShellHandoffContinuity {
	if in == nil {
		return nil
	}
	return &ipc.TaskShellHandoffContinuity{
		State:                        string(in.State),
		Reason:                       in.Reason,
		LaunchAttemptID:              in.LaunchAttemptID,
		LaunchID:                     in.LaunchID,
		AcknowledgmentID:             in.AcknowledgmentID,
		AcknowledgmentStatus:         string(in.AcknowledgmentStatus),
		AcknowledgmentSummary:        in.AcknowledgmentSummary,
		FollowThroughID:              in.FollowThroughID,
		FollowThroughKind:            string(in.FollowThroughKind),
		FollowThroughSummary:         in.FollowThroughSummary,
		DownstreamContinuationProven: in.DownstreamContinuationProven,
	}
}

type Service struct {
	SocketPath string
	Handler    orchestrator.Service
}

func NewService(socketPath string, handler orchestrator.Service) *Service {
	return &Service{SocketPath: socketPath, Handler: handler}
}

func (s *Service) Run(ctx context.Context) error {
	if s.Handler == nil {
		return errors.New("daemon handler is required")
	}
	if s.SocketPath == "" {
		return errors.New("daemon socket path is required")
	}

	if err := os.MkdirAll(filepath.Dir(s.SocketPath), 0o755); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}
	_ = os.Remove(s.SocketPath)

	ln, err := net.Listen("unix", s.SocketPath)
	if err != nil {
		return fmt.Errorf("listen unix socket: %w", err)
	}
	defer func() {
		_ = ln.Close()
		_ = os.Remove(s.SocketPath)
	}()

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}
			return fmt.Errorf("accept connection: %w", err)
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *Service) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	decoder := json.NewDecoder(bufio.NewReader(conn))
	encoder := json.NewEncoder(conn)

	var req ipc.Request
	if err := decoder.Decode(&req); err != nil {
		_ = encoder.Encode(ipc.Response{RequestID: req.RequestID, OK: false, Error: &ipc.ErrorPayload{Code: "BAD_REQUEST", Message: err.Error()}})
		return
	}

	resp := s.handleRequest(ctx, req)
	_ = encoder.Encode(resp)
}

func (s *Service) handleRequest(ctx context.Context, req ipc.Request) ipc.Response {
	respondErr := func(code, msg string) ipc.Response {
		return ipc.Response{RequestID: req.RequestID, OK: false, Error: &ipc.ErrorPayload{Code: code, Message: msg}}
	}
	respondOK := func(payload any) ipc.Response {
		b, err := json.Marshal(payload)
		if err != nil {
			return respondErr("ENCODE_ERROR", err.Error())
		}
		return ipc.Response{RequestID: req.RequestID, OK: true, Payload: b}
	}

	switch req.Method {
	case ipc.MethodResolveShellTaskForRepo:
		var p ipc.ResolveShellTaskForRepoRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ResolveShellTaskForRepo(ctx, p.RepoRoot, p.DefaultGoal)
		if err != nil {
			return respondErr("SHELL_TASK_RESOLVE_FAILED", err.Error())
		}
		return respondOK(ipc.ResolveShellTaskForRepoResponse{
			TaskID:   out.TaskID,
			RepoRoot: out.RepoRoot,
			Created:  out.Created,
		})
	case ipc.MethodStartTask:
		var p ipc.StartTaskRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.StartTask(ctx, p.Goal, p.RepoRoot)
		if err != nil {
			return respondErr("START_FAILED", err.Error())
		}
		return respondOK(ipc.StartTaskResponse{
			TaskID:         out.TaskID,
			ConversationID: out.ConversationID,
			Phase:          out.Phase,
			RepoAnchor: ipc.RepoAnchor{
				RepoRoot:         out.RepoAnchor.RepoRoot,
				Branch:           out.RepoAnchor.Branch,
				HeadSHA:          out.RepoAnchor.HeadSHA,
				WorkingTreeDirty: out.RepoAnchor.WorkingTreeDirty,
				CapturedAt:       out.RepoAnchor.CapturedAt,
			},
			CanonicalResponse: out.CanonicalResponse,
		})

	case ipc.MethodSendMessage:
		var p ipc.TaskMessageRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.MessageTask(ctx, string(p.TaskID), p.Message)
		if err != nil {
			return respondErr("MESSAGE_FAILED", err.Error())
		}
		return respondOK(ipc.TaskMessageResponse{
			TaskID:      out.TaskID,
			Phase:       out.Phase,
			IntentClass: string(out.IntentClass),
			BriefID:     out.BriefID,
			BriefHash:   out.BriefHash,
			RepoAnchor: ipc.RepoAnchor{
				RepoRoot:         out.RepoAnchor.RepoRoot,
				Branch:           out.RepoAnchor.Branch,
				HeadSHA:          out.RepoAnchor.HeadSHA,
				WorkingTreeDirty: out.RepoAnchor.WorkingTreeDirty,
				CapturedAt:       out.RepoAnchor.CapturedAt,
			},
			CanonicalResponse: out.CanonicalResponse,
		})

	case ipc.MethodTaskStatus:
		var p ipc.TaskStatusRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.StatusTask(ctx, string(p.TaskID))
		if err != nil {
			return respondErr("STATUS_FAILED", err.Error())
		}
		latestCheckpointAt := int64(0)
		if !out.LatestCheckpointAt.IsZero() {
			latestCheckpointAt = out.LatestCheckpointAt.UnixMilli()
		}
		lastEventAt := int64(0)
		if !out.LastEventAt.IsZero() {
			lastEventAt = out.LastEventAt.UnixMilli()
		}
		return respondOK(ipc.TaskStatusResponse{
			TaskID:               out.TaskID,
			ConversationID:       out.ConversationID,
			Goal:                 out.Goal,
			Phase:                out.Phase,
			Status:               out.Status,
			CurrentIntentID:      out.CurrentIntentID,
			CurrentIntentClass:   string(out.CurrentIntentClass),
			CurrentIntentSummary: out.CurrentIntentSummary,
			CurrentBriefID:       out.CurrentBriefID,
			CurrentBriefHash:     out.CurrentBriefHash,
			LatestRunID:          out.LatestRunID,
			LatestRunStatus:      out.LatestRunStatus,
			LatestRunSummary:     out.LatestRunSummary,
			RepoAnchor: ipc.RepoAnchor{
				RepoRoot:         out.RepoAnchor.RepoRoot,
				Branch:           out.RepoAnchor.Branch,
				HeadSHA:          out.RepoAnchor.HeadSHA,
				WorkingTreeDirty: out.RepoAnchor.WorkingTreeDirty,
				CapturedAt:       out.RepoAnchor.CapturedAt,
			},
			LatestCheckpointID:          out.LatestCheckpointID,
			LatestCheckpointAtUnixMs:    latestCheckpointAt,
			LatestCheckpointTrigger:     string(out.LatestCheckpointTrigger),
			CheckpointResumable:         out.CheckpointResumable,
			ResumeDescriptor:            out.ResumeDescriptor,
			LatestLaunchAttemptID:       out.LatestLaunchAttemptID,
			LatestLaunchID:              out.LatestLaunchID,
			LatestLaunchStatus:          string(out.LatestLaunchStatus),
			LatestAcknowledgmentID:      out.LatestAcknowledgmentID,
			LatestAcknowledgmentStatus:  string(out.LatestAcknowledgmentStatus),
			LatestAcknowledgmentSummary: out.LatestAcknowledgmentSummary,
			LatestFollowThroughID:       out.LatestFollowThroughID,
			LatestFollowThroughKind:     string(out.LatestFollowThroughKind),
			LatestFollowThroughSummary:  out.LatestFollowThroughSummary,
			LaunchControlState:          string(out.LaunchControlState),
			LaunchRetryDisposition:      string(out.LaunchRetryDisposition),
			LaunchControlReason:         out.LaunchControlReason,
			HandoffContinuityState:      string(out.HandoffContinuityState),
			HandoffContinuityReason:     out.HandoffContinuityReason,
			HandoffContinuationProven:   out.HandoffContinuationProven,
			IsResumable:                 out.IsResumable,
			RecoveryClass:               string(out.RecoveryClass),
			RecommendedAction:           string(out.RecommendedAction),
			ReadyForNextRun:             out.ReadyForNextRun,
			ReadyForHandoffLaunch:       out.ReadyForHandoffLaunch,
			RecoveryReason:              out.RecoveryReason,
			LatestRecoveryAction:        ipcRecoveryActionRecord(out.LatestRecoveryAction),
			LastEventType:               string(out.LastEventType),
			LastEventID:                 out.LastEventID,
			LastEventAtUnixMs:           lastEventAt,
		})
	case ipc.MethodRecordRecoveryAction:
		var p ipc.TaskRecordRecoveryActionRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.RecordRecoveryAction(ctx, orchestrator.RecordRecoveryActionRequest{
			TaskID:  string(p.TaskID),
			Kind:    recoveryaction.Kind(p.Kind),
			Summary: p.Summary,
			Notes:   append([]string{}, p.Notes...),
		})
		if err != nil {
			return respondErr("RECOVERY_ACTION_FAILED", err.Error())
		}
		action := ipcRecoveryActionRecord(&out.Action)
		if action == nil {
			return respondErr("RECOVERY_ACTION_FAILED", "missing recovery action payload")
		}
		return respondOK(ipc.TaskRecordRecoveryActionResponse{
			TaskID:                out.TaskID,
			Action:                *action,
			RecoveryClass:         string(out.RecoveryClass),
			RecommendedAction:     string(out.RecommendedAction),
			ReadyForNextRun:       out.ReadyForNextRun,
			ReadyForHandoffLaunch: out.ReadyForHandoffLaunch,
			RecoveryReason:        out.RecoveryReason,
			CanonicalResponse:     out.CanonicalResponse,
		})
	case ipc.MethodReviewInterruptedRun:
		var p ipc.TaskReviewInterruptedRunRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.RecordRecoveryAction(ctx, orchestrator.RecordRecoveryActionRequest{
			TaskID:  string(p.TaskID),
			Kind:    recoveryaction.KindInterruptedRunReviewed,
			Summary: p.Summary,
			Notes:   append([]string{}, p.Notes...),
		})
		if err != nil {
			return respondErr("INTERRUPTED_REVIEW_FAILED", err.Error())
		}
		action := ipcRecoveryActionRecord(&out.Action)
		if action == nil {
			return respondErr("INTERRUPTED_REVIEW_FAILED", "missing interrupted review action payload")
		}
		return respondOK(ipc.TaskRecordRecoveryActionResponse{
			TaskID:                out.TaskID,
			Action:                *action,
			RecoveryClass:         string(out.RecoveryClass),
			RecommendedAction:     string(out.RecommendedAction),
			ReadyForNextRun:       out.ReadyForNextRun,
			ReadyForHandoffLaunch: out.ReadyForHandoffLaunch,
			RecoveryReason:        out.RecoveryReason,
			CanonicalResponse:     out.CanonicalResponse,
		})
	case ipc.MethodExecuteRebrief:
		var p ipc.TaskRebriefRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ExecuteRebrief(ctx, orchestrator.ExecuteRebriefRequest{TaskID: string(p.TaskID)})
		if err != nil {
			return respondErr("REBRIEF_FAILED", err.Error())
		}
		return respondOK(ipc.TaskRebriefResponse{
			TaskID:                out.TaskID,
			PreviousBriefID:       out.PreviousBriefID,
			BriefID:               out.BriefID,
			BriefHash:             out.BriefHash,
			RecoveryClass:         string(out.RecoveryClass),
			RecommendedAction:     string(out.RecommendedAction),
			ReadyForNextRun:       out.ReadyForNextRun,
			ReadyForHandoffLaunch: out.ReadyForHandoffLaunch,
			RecoveryReason:        out.RecoveryReason,
			CanonicalResponse:     out.CanonicalResponse,
		})
	case ipc.MethodExecuteInterruptedResume:
		var p ipc.TaskInterruptedResumeRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ExecuteInterruptedResume(ctx, orchestrator.ExecuteInterruptedResumeRequest{
			TaskID:  string(p.TaskID),
			Summary: p.Summary,
			Notes:   append([]string{}, p.Notes...),
		})
		if err != nil {
			return respondErr("INTERRUPTED_RESUME_FAILED", err.Error())
		}
		action := ipcRecoveryActionRecord(&out.Action)
		if action == nil {
			return respondErr("INTERRUPTED_RESUME_FAILED", "missing interrupted resume action payload")
		}
		return respondOK(ipc.TaskInterruptedResumeResponse{
			TaskID:                out.TaskID,
			BriefID:               out.BriefID,
			BriefHash:             out.BriefHash,
			Action:                *action,
			RecoveryClass:         string(out.RecoveryClass),
			RecommendedAction:     string(out.RecommendedAction),
			ReadyForNextRun:       out.ReadyForNextRun,
			ReadyForHandoffLaunch: out.ReadyForHandoffLaunch,
			RecoveryReason:        out.RecoveryReason,
			CanonicalResponse:     out.CanonicalResponse,
		})
	case ipc.MethodExecuteContinueRecovery:
		var p ipc.TaskContinueRecoveryRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ExecuteContinueRecovery(ctx, orchestrator.ExecuteContinueRecoveryRequest{TaskID: string(p.TaskID)})
		if err != nil {
			return respondErr("CONTINUE_RECOVERY_FAILED", err.Error())
		}
		action := ipcRecoveryActionRecord(&out.Action)
		if action == nil {
			return respondErr("CONTINUE_RECOVERY_FAILED", "missing continue recovery action payload")
		}
		return respondOK(ipc.TaskContinueRecoveryResponse{
			TaskID:                out.TaskID,
			BriefID:               out.BriefID,
			BriefHash:             out.BriefHash,
			Action:                *action,
			RecoveryClass:         string(out.RecoveryClass),
			RecommendedAction:     string(out.RecommendedAction),
			ReadyForNextRun:       out.ReadyForNextRun,
			ReadyForHandoffLaunch: out.ReadyForHandoffLaunch,
			RecoveryReason:        out.RecoveryReason,
			CanonicalResponse:     out.CanonicalResponse,
		})
	case ipc.MethodTaskRun:
		var p ipc.TaskRunRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.RunTask(ctx, orchestrator.RunTaskRequest{
			TaskID:             string(p.TaskID),
			Action:             p.Action,
			Mode:               p.Mode,
			RunID:              p.RunID,
			SimulateInterrupt:  p.SimulateInterrupt,
			InterruptionReason: p.InterruptionReason,
		})
		if err != nil {
			return respondErr("RUN_FAILED", err.Error())
		}
		return respondOK(ipc.TaskRunResponse{
			TaskID:            out.TaskID,
			RunID:             out.RunID,
			RunStatus:         out.RunStatus,
			Phase:             out.Phase,
			CanonicalResponse: out.CanonicalResponse,
		})
	case ipc.MethodTaskInspect:
		var p ipc.TaskInspectRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.InspectTask(ctx, string(p.TaskID))
		if err != nil {
			return respondErr("INSPECT_FAILED", err.Error())
		}
		resp := ipc.TaskInspectResponse{
			TaskID: out.TaskID,
			RepoAnchor: ipc.RepoAnchor{
				RepoRoot:         out.RepoAnchor.RepoRoot,
				Branch:           out.RepoAnchor.Branch,
				HeadSHA:          out.RepoAnchor.HeadSHA,
				WorkingTreeDirty: out.RepoAnchor.WorkingTreeDirty,
				CapturedAt:       out.RepoAnchor.CapturedAt,
			},
			Intent:               out.Intent,
			Brief:                out.Brief,
			Run:                  out.Run,
			Checkpoint:           out.Checkpoint,
			Handoff:              out.Handoff,
			Launch:               out.Launch,
			Acknowledgment:       out.Acknowledgment,
			FollowThrough:        out.FollowThrough,
			LaunchControl:        ipcLaunchControl(out.LaunchControl),
			HandoffContinuity:    ipcHandoffContinuity(out.HandoffContinuity),
			Recovery:             ipcRecoveryAssessment(out.Recovery),
			LatestRecoveryAction: ipcRecoveryActionRecord(out.LatestRecoveryAction),
		}
		if len(out.RecentRecoveryActions) > 0 {
			resp.RecentRecoveryActions = make([]ipc.TaskRecoveryActionRecord, 0, len(out.RecentRecoveryActions))
			for i := range out.RecentRecoveryActions {
				if mapped := ipcRecoveryActionRecord(&out.RecentRecoveryActions[i]); mapped != nil {
					resp.RecentRecoveryActions = append(resp.RecentRecoveryActions, *mapped)
				}
			}
		}
		return respondOK(resp)
	case ipc.MethodTaskShellSnapshot:
		var p ipc.TaskShellSnapshotRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ShellSnapshotTask(ctx, string(p.TaskID))
		if err != nil {
			return respondErr("SHELL_SNAPSHOT_FAILED", err.Error())
		}
		resp := ipc.TaskShellSnapshotResponse{
			TaskID:        out.TaskID,
			Goal:          out.Goal,
			Phase:         out.Phase,
			Status:        out.Status,
			IntentClass:   out.IntentClass,
			IntentSummary: out.IntentSummary,
			RepoAnchor: ipc.RepoAnchor{
				RepoRoot:         out.RepoAnchor.RepoRoot,
				Branch:           out.RepoAnchor.Branch,
				HeadSHA:          out.RepoAnchor.HeadSHA,
				WorkingTreeDirty: out.RepoAnchor.WorkingTreeDirty,
				CapturedAt:       out.RepoAnchor.CapturedAt,
			},
			LatestCanonicalResponse: out.LatestCanonicalResponse,
		}
		if out.Brief != nil {
			resp.Brief = &ipc.TaskShellBrief{
				BriefID:          out.Brief.BriefID,
				Objective:        out.Brief.Objective,
				NormalizedAction: out.Brief.NormalizedAction,
				Constraints:      append([]string{}, out.Brief.Constraints...),
				DoneCriteria:     append([]string{}, out.Brief.DoneCriteria...),
			}
		}
		if out.Run != nil {
			resp.Run = &ipc.TaskShellRun{
				RunID:              out.Run.RunID,
				WorkerKind:         out.Run.WorkerKind,
				Status:             out.Run.Status,
				LastKnownSummary:   out.Run.LastKnownSummary,
				StartedAt:          out.Run.StartedAt,
				EndedAt:            out.Run.EndedAt,
				InterruptionReason: out.Run.InterruptionReason,
			}
		}
		if out.Checkpoint != nil {
			resp.Checkpoint = &ipc.TaskShellCheckpoint{
				CheckpointID:     out.Checkpoint.CheckpointID,
				Trigger:          out.Checkpoint.Trigger,
				CreatedAt:        out.Checkpoint.CreatedAt,
				ResumeDescriptor: out.Checkpoint.ResumeDescriptor,
				IsResumable:      out.Checkpoint.IsResumable,
			}
		}
		if out.Handoff != nil {
			resp.Handoff = &ipc.TaskShellHandoff{
				HandoffID:    out.Handoff.HandoffID,
				Status:       string(out.Handoff.Status),
				SourceWorker: out.Handoff.SourceWorker,
				TargetWorker: out.Handoff.TargetWorker,
				Mode:         string(out.Handoff.Mode),
				Reason:       out.Handoff.Reason,
				AcceptedBy:   out.Handoff.AcceptedBy,
				CreatedAt:    out.Handoff.CreatedAt,
			}
		}
		if out.Launch != nil {
			resp.Launch = &ipc.TaskShellLaunch{
				AttemptID:         out.Launch.AttemptID,
				LaunchID:          out.Launch.LaunchID,
				Status:            string(out.Launch.Status),
				RequestedAt:       out.Launch.RequestedAt,
				StartedAt:         out.Launch.StartedAt,
				EndedAt:           out.Launch.EndedAt,
				Summary:           out.Launch.Summary,
				ErrorMessage:      out.Launch.ErrorMessage,
				OutputArtifactRef: out.Launch.OutputArtifactRef,
			}
		}
		resp.LaunchControl = ipcShellLaunchControl(out.LaunchControl)
		if out.Acknowledgment != nil {
			resp.Acknowledgment = &ipc.TaskShellAcknowledgment{
				Status:    string(out.Acknowledgment.Status),
				Summary:   out.Acknowledgment.Summary,
				CreatedAt: out.Acknowledgment.CreatedAt,
			}
		}
		if out.FollowThrough != nil {
			resp.FollowThrough = &ipc.TaskShellFollowThrough{
				RecordID:        out.FollowThrough.RecordID,
				Kind:            string(out.FollowThrough.Kind),
				Summary:         out.FollowThrough.Summary,
				LaunchAttemptID: out.FollowThrough.LaunchAttemptID,
				LaunchID:        out.FollowThrough.LaunchID,
				CreatedAt:       out.FollowThrough.CreatedAt,
			}
		}
		resp.HandoffContinuity = ipcShellHandoffContinuity(out.HandoffContinuity)
		resp.Recovery = ipcShellRecovery(out.Recovery)
		if len(out.RecentProofs) > 0 {
			resp.RecentProofs = make([]ipc.TaskShellProof, 0, len(out.RecentProofs))
			for _, evt := range out.RecentProofs {
				resp.RecentProofs = append(resp.RecentProofs, ipc.TaskShellProof{
					EventID:   evt.EventID,
					Type:      string(evt.Type),
					Summary:   evt.Summary,
					Timestamp: evt.Timestamp,
				})
			}
		}
		if len(out.RecentConversation) > 0 {
			resp.RecentConversation = make([]ipc.TaskShellConversation, 0, len(out.RecentConversation))
			for _, msg := range out.RecentConversation {
				resp.RecentConversation = append(resp.RecentConversation, ipc.TaskShellConversation{
					Role:      string(msg.Role),
					Body:      msg.Body,
					CreatedAt: msg.CreatedAt,
				})
			}
		}
		return respondOK(resp)
	case ipc.MethodTaskShellLifecycle:
		var p ipc.TaskShellLifecycleRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.RecordShellLifecycle(ctx, orchestrator.RecordShellLifecycleRequest{
			TaskID:     string(p.TaskID),
			SessionID:  p.SessionID,
			Kind:       orchestrator.ShellLifecycleKind(p.Kind),
			HostMode:   p.HostMode,
			HostState:  p.HostState,
			Note:       p.Note,
			InputLive:  p.InputLive,
			ExitCode:   p.ExitCode,
			PaneWidth:  p.PaneWidth,
			PaneHeight: p.PaneHeight,
		})
		if err != nil {
			return respondErr("SHELL_LIFECYCLE_FAILED", err.Error())
		}
		return respondOK(ipc.TaskShellLifecycleResponse{TaskID: out.TaskID})
	case ipc.MethodTaskShellSessionReport:
		var p ipc.TaskShellSessionReportRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ReportShellSession(ctx, orchestrator.ReportShellSessionRequest{
			TaskID:           string(p.TaskID),
			SessionID:        p.SessionID,
			WorkerPreference: p.WorkerPreference,
			ResolvedWorker:   p.ResolvedWorker,
			WorkerSessionID:  p.WorkerSessionID,
			AttachCapability: shellsession.AttachCapability(p.AttachCapability),
			HostMode:         p.HostMode,
			HostState:        p.HostState,
			StartedAt:        p.StartedAt,
			Active:           p.Active,
			Note:             p.Note,
		})
		if err != nil {
			return respondErr("SHELL_SESSION_REPORT_FAILED", err.Error())
		}
		return respondOK(ipc.TaskShellSessionReportResponse{
			TaskID: out.TaskID,
			Session: ipc.TaskShellSessionRecord{
				SessionID:        out.Session.SessionID,
				TaskID:           out.Session.TaskID,
				WorkerPreference: out.Session.WorkerPreference,
				ResolvedWorker:   out.Session.ResolvedWorker,
				WorkerSessionID:  out.Session.WorkerSessionID,
				AttachCapability: string(out.Session.AttachCapability),
				HostMode:         out.Session.HostMode,
				HostState:        out.Session.HostState,
				SessionClass:     string(out.Session.SessionClass),
				StartedAt:        out.Session.StartedAt,
				LastUpdatedAt:    out.Session.LastUpdatedAt,
				Active:           out.Session.Active,
				Note:             out.Session.Note,
			},
		})
	case ipc.MethodTaskShellSessions:
		var p ipc.TaskShellSessionsRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ListShellSessions(ctx, string(p.TaskID))
		if err != nil {
			return respondErr("SHELL_SESSIONS_FAILED", err.Error())
		}
		resp := ipc.TaskShellSessionsResponse{TaskID: out.TaskID}
		if len(out.Sessions) > 0 {
			resp.Sessions = make([]ipc.TaskShellSessionRecord, 0, len(out.Sessions))
			for _, session := range out.Sessions {
				resp.Sessions = append(resp.Sessions, ipc.TaskShellSessionRecord{
					SessionID:        session.SessionID,
					TaskID:           session.TaskID,
					WorkerPreference: session.WorkerPreference,
					ResolvedWorker:   session.ResolvedWorker,
					WorkerSessionID:  session.WorkerSessionID,
					AttachCapability: string(session.AttachCapability),
					HostMode:         session.HostMode,
					HostState:        session.HostState,
					SessionClass:     string(session.SessionClass),
					StartedAt:        session.StartedAt,
					LastUpdatedAt:    session.LastUpdatedAt,
					Active:           session.Active,
					Note:             session.Note,
				})
			}
		}
		return respondOK(resp)
	case ipc.MethodContinueTask:
		var p ipc.TaskContinueRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ContinueTask(ctx, string(p.TaskID))
		if err != nil {
			return respondErr("CONTINUE_FAILED", err.Error())
		}
		return respondOK(ipc.TaskContinueResponse{
			TaskID:                out.TaskID,
			Outcome:               string(out.Outcome),
			DriftClass:            out.DriftClass,
			Phase:                 out.Phase,
			RunID:                 out.RunID,
			CheckpointID:          out.CheckpointID,
			ResumeDescriptor:      out.ResumeDescriptor,
			RecoveryClass:         string(out.RecoveryClass),
			RecommendedAction:     string(out.RecommendedAction),
			ReadyForNextRun:       out.ReadyForNextRun,
			ReadyForHandoffLaunch: out.ReadyForHandoffLaunch,
			RecoveryReason:        out.RecoveryReason,
			CanonicalResponse:     out.CanonicalResponse,
		})
	case ipc.MethodCreateCheckpoint:
		var p ipc.TaskCheckpointRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.CreateCheckpoint(ctx, string(p.TaskID))
		if err != nil {
			return respondErr("CHECKPOINT_FAILED", err.Error())
		}
		return respondOK(ipc.TaskCheckpointResponse{
			TaskID:            out.TaskID,
			CheckpointID:      out.CheckpointID,
			Trigger:           out.Trigger,
			IsResumable:       out.IsResumable,
			CanonicalResponse: out.CanonicalResponse,
		})
	case ipc.MethodCreateHandoff:
		var p ipc.TaskHandoffCreateRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.CreateHandoff(ctx, orchestrator.CreateHandoffRequest{
			TaskID:       string(p.TaskID),
			TargetWorker: p.TargetWorker,
			Reason:       p.Reason,
			Mode:         p.Mode,
			Notes:        append([]string{}, p.Notes...),
		})
		if err != nil {
			return respondErr("HANDOFF_CREATE_FAILED", err.Error())
		}
		return respondOK(ipc.TaskHandoffCreateResponse{
			TaskID:            out.TaskID,
			HandoffID:         out.HandoffID,
			SourceWorker:      out.SourceWorker,
			TargetWorker:      out.TargetWorker,
			Status:            string(out.Status),
			CheckpointID:      out.CheckpointID,
			BriefID:           out.BriefID,
			CanonicalResponse: out.CanonicalResponse,
			Packet:            out.Packet,
		})
	case ipc.MethodAcceptHandoff:
		var p ipc.TaskHandoffAcceptRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.AcceptHandoff(ctx, orchestrator.AcceptHandoffRequest{
			TaskID:     string(p.TaskID),
			HandoffID:  p.HandoffID,
			AcceptedBy: p.AcceptedBy,
			Notes:      append([]string{}, p.Notes...),
		})
		if err != nil {
			return respondErr("HANDOFF_ACCEPT_FAILED", err.Error())
		}
		return respondOK(ipc.TaskHandoffAcceptResponse{
			TaskID:            out.TaskID,
			HandoffID:         out.HandoffID,
			Status:            string(out.Status),
			CanonicalResponse: out.CanonicalResponse,
		})
	case ipc.MethodLaunchHandoff:
		var p ipc.TaskHandoffLaunchRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.LaunchHandoff(ctx, orchestrator.LaunchHandoffRequest{
			TaskID:       string(p.TaskID),
			HandoffID:    p.HandoffID,
			TargetWorker: p.TargetWorker,
		})
		if err != nil {
			return respondErr("HANDOFF_LAUNCH_FAILED", err.Error())
		}
		return respondOK(ipc.TaskHandoffLaunchResponse{
			TaskID:            out.TaskID,
			HandoffID:         out.HandoffID,
			TargetWorker:      out.TargetWorker,
			LaunchStatus:      string(out.LaunchStatus),
			LaunchID:          out.LaunchID,
			CanonicalResponse: out.CanonicalResponse,
			Payload:           out.Payload,
		})
	case ipc.MethodRecordHandoffFollowThrough:
		var p ipc.TaskHandoffFollowThroughRecordRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.RecordHandoffFollowThrough(ctx, orchestrator.RecordHandoffFollowThroughRequest{
			TaskID:  string(p.TaskID),
			Kind:    handoff.FollowThroughKind(p.Kind),
			Summary: p.Summary,
			Notes:   append([]string{}, p.Notes...),
		})
		if err != nil {
			return respondErr("HANDOFF_FOLLOW_THROUGH_FAILED", err.Error())
		}
		return respondOK(ipc.TaskHandoffFollowThroughRecordResponse{
			TaskID:                out.TaskID,
			Record:                &out.Record,
			HandoffContinuity:     ipcHandoffContinuity(&out.HandoffContinuity),
			RecoveryClass:         string(out.RecoveryClass),
			RecommendedAction:     string(out.RecommendedAction),
			ReadyForNextRun:       out.ReadyForNextRun,
			ReadyForHandoffLaunch: out.ReadyForHandoffLaunch,
			RecoveryReason:        out.RecoveryReason,
			CanonicalResponse:     out.CanonicalResponse,
		})
	default:
		return respondErr("UNSUPPORTED_METHOD", fmt.Sprintf("unsupported method: %s", req.Method))
	}
}

```


## internal/runtime/daemon/service_test.go

```go
package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/conversation"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/recoveryaction"
	"tuku/internal/domain/run"
	"tuku/internal/domain/shellsession"
	anchorgit "tuku/internal/git/anchor"
	"tuku/internal/ipc"
	"tuku/internal/orchestrator"
	"tuku/internal/response/canonical"
	"tuku/internal/storage/sqlite"
)

func TestHandleRequestCreateHandoffRoute(t *testing.T) {
	var captured orchestrator.CreateHandoffRequest
	handler := &fakeOrchestratorService{
		createHandoffFn: func(_ context.Context, req orchestrator.CreateHandoffRequest) (orchestrator.CreateHandoffResult, error) {
			captured = req
			return orchestrator.CreateHandoffResult{
				TaskID:            common.TaskID(req.TaskID),
				HandoffID:         "hnd_test",
				SourceWorker:      run.WorkerKindCodex,
				TargetWorker:      req.TargetWorker,
				Status:            handoff.StatusCreated,
				CheckpointID:      common.CheckpointID("chk_test"),
				BriefID:           common.BriefID("brf_test"),
				CanonicalResponse: "handoff created",
				Packet: &handoff.Packet{
					Version:      1,
					HandoffID:    "hnd_test",
					TaskID:       common.TaskID(req.TaskID),
					Status:       handoff.StatusCreated,
					TargetWorker: req.TargetWorker,
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskHandoffCreateRequest{
		TaskID:       common.TaskID("tsk_123"),
		TargetWorker: run.WorkerKindClaude,
		Reason:       "manual test",
		Mode:         handoff.ModeResume,
		Notes:        []string{"note"},
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_1",
		Method:    ipc.MethodCreateHandoff,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_123" {
		t.Fatalf("expected captured task id tsk_123, got %s", captured.TaskID)
	}
	if captured.TargetWorker != run.WorkerKindClaude {
		t.Fatalf("expected target worker claude, got %s", captured.TargetWorker)
	}
	var out ipc.TaskHandoffCreateResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.HandoffID != "hnd_test" {
		t.Fatalf("expected handoff id hnd_test, got %s", out.HandoffID)
	}
	if out.Status != string(handoff.StatusCreated) {
		t.Fatalf("expected status CREATED, got %s", out.Status)
	}
}

func TestHandleRequestResolveShellTaskForRepoRoute(t *testing.T) {
	var capturedRepoRoot string
	var capturedGoal string
	handler := &fakeOrchestratorService{
		resolveShellTaskForRepoFn: func(_ context.Context, repoRoot string, defaultGoal string) (orchestrator.ResolveShellTaskResult, error) {
			capturedRepoRoot = repoRoot
			capturedGoal = defaultGoal
			return orchestrator.ResolveShellTaskResult{
				TaskID:   common.TaskID("tsk_repo"),
				RepoRoot: repoRoot,
				Created:  true,
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.ResolveShellTaskForRepoRequest{
		RepoRoot:    "/tmp/repo",
		DefaultGoal: "Continue work in this repository",
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_repo",
		Method:    ipc.MethodResolveShellTaskForRepo,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if capturedRepoRoot != "/tmp/repo" || capturedGoal != "Continue work in this repository" {
		t.Fatalf("unexpected resolve-shell-task request: repo=%q goal=%q", capturedRepoRoot, capturedGoal)
	}
	var out ipc.ResolveShellTaskForRepoResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal resolve shell task response: %v", err)
	}
	if out.TaskID != "tsk_repo" || !out.Created {
		t.Fatalf("unexpected resolve shell task response: %+v", out)
	}
}

func TestHandleRequestAcceptHandoffRoute(t *testing.T) {
	var captured orchestrator.AcceptHandoffRequest
	handler := &fakeOrchestratorService{
		acceptHandoffFn: func(_ context.Context, req orchestrator.AcceptHandoffRequest) (orchestrator.AcceptHandoffResult, error) {
			captured = req
			return orchestrator.AcceptHandoffResult{
				TaskID:            common.TaskID(req.TaskID),
				HandoffID:         req.HandoffID,
				Status:            handoff.StatusAccepted,
				CanonicalResponse: "handoff accepted",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskHandoffAcceptRequest{
		TaskID:     common.TaskID("tsk_123"),
		HandoffID:  "hnd_abc",
		AcceptedBy: run.WorkerKindClaude,
		Notes:      []string{"accepted"},
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_2",
		Method:    ipc.MethodAcceptHandoff,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_123" || captured.HandoffID != "hnd_abc" {
		t.Fatalf("unexpected captured accept request: %+v", captured)
	}
	var out ipc.TaskHandoffAcceptResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.Status != string(handoff.StatusAccepted) {
		t.Fatalf("expected status ACCEPTED, got %s", out.Status)
	}
}

func TestHandleRequestShellSnapshotRoute(t *testing.T) {
	handler := &fakeOrchestratorService{
		shellSnapshotFn: func(_ context.Context, taskID string) (orchestrator.ShellSnapshotResult, error) {
			if taskID != "tsk_123" {
				t.Fatalf("unexpected task id %s", taskID)
			}
			return orchestrator.ShellSnapshotResult{
				TaskID:        common.TaskID(taskID),
				Goal:          "Shell milestone",
				Phase:         "BRIEF_READY",
				Status:        "ACTIVE",
				IntentClass:   "implement",
				IntentSummary: "implement: wire shell",
				LaunchControl: &orchestrator.ShellLaunchControlSummary{
					State:            orchestrator.LaunchControlStateCompleted,
					RetryDisposition: orchestrator.LaunchRetryDispositionBlocked,
					Reason:           "launcher invocation completed; downstream continuation remains unproven",
					HandoffID:        "hnd_1",
					AttemptID:        "hlc_1",
					LaunchID:         "launch_1",
				},
				HandoffContinuity: &orchestrator.ShellHandoffContinuitySummary{
					State:                        orchestrator.HandoffContinuityStateFollowThroughProofOfLife,
					Reason:                       "Claude handoff launch has downstream proof-of-life evidence, but downstream completion remains unproven",
					LaunchAttemptID:              "hlc_1",
					FollowThroughID:              "hft_1",
					FollowThroughKind:            handoff.FollowThroughProofOfLifeObserved,
					FollowThroughSummary:         "later Claude proof of life observed",
					DownstreamContinuationProven: true,
				},
				FollowThrough: &orchestrator.ShellFollowThroughSummary{
					RecordID:        "hft_1",
					Kind:            handoff.FollowThroughProofOfLifeObserved,
					Summary:         "later Claude proof of life observed",
					LaunchAttemptID: "hlc_1",
					LaunchID:        "launch_1",
					CreatedAt:       time.Unix(1710000300, 0).UTC(),
				},
				Recovery: &orchestrator.ShellRecoverySummary{
					ContinuityOutcome: orchestrator.ContinueOutcomeSafe,
					RecoveryClass:     orchestrator.RecoveryClassHandoffLaunchCompleted,
					RecommendedAction: orchestrator.RecoveryActionMonitorLaunchedHandoff,
					ReadyForNextRun:   false,
					Issues: []orchestrator.ShellRecoveryIssue{
						{Code: "HANDOFF_MONITORING", Message: "downstream completion remains unproven"},
					},
				},
				RecentProofs: []orchestrator.ShellProofSummary{
					{EventID: "evt_1", Type: proof.EventBriefCreated, Summary: "Execution brief updated"},
				},
				RecentConversation: []orchestrator.ShellConversationSummary{
					{Role: conversation.RoleSystem, Body: "Canonical shell response"},
				},
				LatestCanonicalResponse: "Canonical shell response",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskShellSnapshotRequest{TaskID: common.TaskID("tsk_123")})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_3",
		Method:    ipc.MethodTaskShellSnapshot,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	var out ipc.TaskShellSnapshotResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal shell snapshot response: %v", err)
	}
	if out.TaskID != "tsk_123" {
		t.Fatalf("expected task id tsk_123, got %s", out.TaskID)
	}
	if out.LatestCanonicalResponse != "Canonical shell response" {
		t.Fatalf("unexpected canonical response %q", out.LatestCanonicalResponse)
	}
	if len(out.RecentProofs) != 1 {
		t.Fatalf("expected one proof item, got %d", len(out.RecentProofs))
	}
	if out.LaunchControl == nil || out.LaunchControl.State != string(orchestrator.LaunchControlStateCompleted) {
		t.Fatalf("expected launch control state mapping, got %+v", out.LaunchControl)
	}
	if out.HandoffContinuity == nil || out.HandoffContinuity.State != string(orchestrator.HandoffContinuityStateFollowThroughProofOfLife) {
		t.Fatalf("expected handoff continuity mapping, got %+v", out.HandoffContinuity)
	}
	if out.HandoffContinuity.FollowThroughKind != string(handoff.FollowThroughProofOfLifeObserved) {
		t.Fatalf("expected follow-through continuity mapping, got %+v", out.HandoffContinuity)
	}
	if out.FollowThrough == nil || out.FollowThrough.Kind != string(handoff.FollowThroughProofOfLifeObserved) {
		t.Fatalf("expected shell follow-through payload mapping, got %+v", out.FollowThrough)
	}
	if out.Recovery == nil || out.Recovery.RecoveryClass != string(orchestrator.RecoveryClassHandoffLaunchCompleted) {
		t.Fatalf("expected recovery mapping, got %+v", out.Recovery)
	}
	if len(out.Recovery.Issues) != 1 {
		t.Fatalf("expected one recovery issue, got %+v", out.Recovery)
	}
}

func TestHandleRequestRecordRecoveryActionRoute(t *testing.T) {
	var captured orchestrator.RecordRecoveryActionRequest
	handler := &fakeOrchestratorService{
		recordRecoveryActionFn: func(_ context.Context, req orchestrator.RecordRecoveryActionRequest) (orchestrator.RecordRecoveryActionResult, error) {
			captured = req
			return orchestrator.RecordRecoveryActionResult{
				TaskID: common.TaskID(req.TaskID),
				Action: recoveryaction.Record{
					Version:   1,
					ActionID:  "ract_1",
					TaskID:    common.TaskID(req.TaskID),
					Kind:      req.Kind,
					Summary:   req.Summary,
					Notes:     append([]string{}, req.Notes...),
					CreatedAt: time.Unix(1710000000, 0).UTC(),
				},
				RecoveryClass:         orchestrator.RecoveryClassDecisionRequired,
				RecommendedAction:     orchestrator.RecoveryActionMakeResumeDecision,
				ReadyForNextRun:       false,
				ReadyForHandoffLaunch: false,
				RecoveryReason:        "failed run reviewed; choose next step",
				CanonicalResponse:     "recovery action recorded",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskRecordRecoveryActionRequest{
		TaskID:  common.TaskID("tsk_123"),
		Kind:    string(recoveryaction.KindFailedRunReviewed),
		Summary: "reviewed failed run",
		Notes:   []string{"operator reviewed logs"},
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_recovery_1",
		Method:    ipc.MethodRecordRecoveryAction,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_123" || captured.Kind != recoveryaction.KindFailedRunReviewed {
		t.Fatalf("unexpected recovery action request: %+v", captured)
	}
	var out ipc.TaskRecordRecoveryActionResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal recovery action response: %v", err)
	}
	if out.Action.ActionID != "ract_1" || out.RecoveryClass != string(orchestrator.RecoveryClassDecisionRequired) {
		t.Fatalf("unexpected recovery action response: %+v", out)
	}
}

func TestHandleRequestReviewInterruptedRunRoute(t *testing.T) {
	var captured orchestrator.RecordRecoveryActionRequest
	handler := &fakeOrchestratorService{
		recordRecoveryActionFn: func(_ context.Context, req orchestrator.RecordRecoveryActionRequest) (orchestrator.RecordRecoveryActionResult, error) {
			captured = req
			return orchestrator.RecordRecoveryActionResult{
				TaskID: common.TaskID(req.TaskID),
				Action: recoveryaction.Record{
					Version:      1,
					ActionID:     "ract_interrupt_1",
					TaskID:       common.TaskID(req.TaskID),
					Kind:         req.Kind,
					RunID:        common.RunID("run_123"),
					CheckpointID: common.CheckpointID("chk_123"),
					Summary:      req.Summary,
					Notes:        append([]string{}, req.Notes...),
					CreatedAt:    time.Unix(1710000001, 0).UTC(),
				},
				RecoveryClass:         orchestrator.RecoveryClassInterruptedRunRecoverable,
				RecommendedAction:     orchestrator.RecoveryActionResumeInterrupted,
				ReadyForNextRun:       false,
				ReadyForHandoffLaunch: false,
				RecoveryReason:        "interrupted execution lineage was reviewed and remains recoverable",
				CanonicalResponse:     "interrupted run reviewed",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskReviewInterruptedRunRequest{
		TaskID:  common.TaskID("tsk_123"),
		Summary: "interrupted lineage reviewed",
		Notes:   []string{"preserve interrupted lineage"},
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_review_interrupt",
		Method:    ipc.MethodReviewInterruptedRun,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_123" || captured.Kind != recoveryaction.KindInterruptedRunReviewed {
		t.Fatalf("unexpected captured interrupted review request: %+v", captured)
	}
	if len(captured.Notes) != 1 || captured.Notes[0] != "preserve interrupted lineage" {
		t.Fatalf("unexpected captured interrupted review notes: %+v", captured.Notes)
	}
	var out ipc.TaskRecordRecoveryActionResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.Action.Kind != string(recoveryaction.KindInterruptedRunReviewed) {
		t.Fatalf("expected interrupted review action kind, got %s", out.Action.Kind)
	}
	if out.ReadyForNextRun {
		t.Fatal("interrupted review response must not claim next-run readiness")
	}
}

func TestHandleRequestExecuteRebriefRoute(t *testing.T) {
	var captured orchestrator.ExecuteRebriefRequest
	handler := &fakeOrchestratorService{
		executeRebriefFn: func(_ context.Context, req orchestrator.ExecuteRebriefRequest) (orchestrator.ExecuteRebriefResult, error) {
			captured = req
			return orchestrator.ExecuteRebriefResult{
				TaskID:                common.TaskID(req.TaskID),
				PreviousBriefID:       common.BriefID("brf_old"),
				BriefID:               common.BriefID("brf_new"),
				BriefHash:             "hash_new",
				RecoveryClass:         orchestrator.RecoveryClassReadyNextRun,
				RecommendedAction:     orchestrator.RecoveryActionStartNextRun,
				ReadyForNextRun:       true,
				ReadyForHandoffLaunch: false,
				RecoveryReason:        "execution brief was regenerated after operator decision",
				CanonicalResponse:     "rebrief executed",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskRebriefRequest{TaskID: common.TaskID("tsk_456")})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_rebrief_1",
		Method:    ipc.MethodExecuteRebrief,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_456" {
		t.Fatalf("unexpected rebrief request: %+v", captured)
	}
	var out ipc.TaskRebriefResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal rebrief response: %v", err)
	}
	if out.BriefID != "brf_new" || !out.ReadyForNextRun {
		t.Fatalf("unexpected rebrief response: %+v", out)
	}
}

func TestHandleRequestExecuteInterruptedResumeRoute(t *testing.T) {
	var captured orchestrator.ExecuteInterruptedResumeRequest
	handler := &fakeOrchestratorService{
		executeInterruptedResumeFn: func(_ context.Context, req orchestrator.ExecuteInterruptedResumeRequest) (orchestrator.ExecuteInterruptedResumeResult, error) {
			captured = req
			return orchestrator.ExecuteInterruptedResumeResult{
				TaskID:                common.TaskID(req.TaskID),
				BriefID:               common.BriefID("brf_current"),
				BriefHash:             "hash_current",
				Action:                recoveryaction.Record{ActionID: "ract_resume_interrupt_1", TaskID: common.TaskID(req.TaskID), Kind: recoveryaction.KindInterruptedResumeExecuted, Summary: "operator resumed interrupted lineage"},
				RecoveryClass:         orchestrator.RecoveryClassContinueExecutionRequired,
				RecommendedAction:     orchestrator.RecoveryActionExecuteContinueRecovery,
				ReadyForNextRun:       false,
				ReadyForHandoffLaunch: false,
				RecoveryReason:        "operator explicitly resumed interrupted execution lineage; continue recovery still must be executed before the next bounded run",
				CanonicalResponse:     "interrupted resume executed",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskInterruptedResumeRequest{
		TaskID:  common.TaskID("tsk_interrupt_resume"),
		Summary: "operator resumed interrupted lineage",
		Notes:   []string{"maintain interrupted lineage semantics"},
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_interrupt_resume_1",
		Method:    ipc.MethodExecuteInterruptedResume,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_interrupt_resume" || captured.Summary != "operator resumed interrupted lineage" {
		t.Fatalf("unexpected interrupted resume request: %+v", captured)
	}
	if len(captured.Notes) != 1 || captured.Notes[0] != "maintain interrupted lineage semantics" {
		t.Fatalf("unexpected interrupted resume notes: %+v", captured.Notes)
	}
	var out ipc.TaskInterruptedResumeResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal interrupted resume response: %v", err)
	}
	if out.BriefID != "brf_current" || out.Action.Kind != string(recoveryaction.KindInterruptedResumeExecuted) {
		t.Fatalf("unexpected interrupted resume response: %+v", out)
	}
	if out.ReadyForNextRun {
		t.Fatal("interrupted resume response must not claim fresh next-run readiness")
	}
}

func TestHandleRequestExecuteContinueRecoveryRoute(t *testing.T) {
	var captured orchestrator.ExecuteContinueRecoveryRequest
	handler := &fakeOrchestratorService{
		executeContinueRecoveryFn: func(_ context.Context, req orchestrator.ExecuteContinueRecoveryRequest) (orchestrator.ExecuteContinueRecoveryResult, error) {
			captured = req
			return orchestrator.ExecuteContinueRecoveryResult{
				TaskID:                common.TaskID(req.TaskID),
				BriefID:               common.BriefID("brf_current"),
				BriefHash:             "hash_current",
				Action:                recoveryaction.Record{ActionID: "ract_continue_1", TaskID: common.TaskID(req.TaskID), Kind: recoveryaction.KindContinueExecuted, Summary: "operator confirmed current brief"},
				RecoveryClass:         orchestrator.RecoveryClassReadyNextRun,
				RecommendedAction:     orchestrator.RecoveryActionStartNextRun,
				ReadyForNextRun:       true,
				ReadyForHandoffLaunch: false,
				RecoveryReason:        "operator explicitly confirmed the current brief for the next bounded run",
				CanonicalResponse:     "continue recovery executed",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskContinueRecoveryRequest{TaskID: common.TaskID("tsk_789")})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_continue_recovery_1",
		Method:    ipc.MethodExecuteContinueRecovery,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_789" {
		t.Fatalf("unexpected continue recovery request: %+v", captured)
	}
	var out ipc.TaskContinueRecoveryResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal continue recovery response: %v", err)
	}
	if out.BriefID != "brf_current" || out.Action.Kind != string(recoveryaction.KindContinueExecuted) || !out.ReadyForNextRun {
		t.Fatalf("unexpected continue recovery response: %+v", out)
	}
}

func TestHandleRequestStatusAndInspectRouteMapRecoveryActions(t *testing.T) {
	action := &recoveryaction.Record{
		Version:   1,
		ActionID:  "ract_status",
		TaskID:    common.TaskID("tsk_status"),
		Kind:      recoveryaction.KindRepairIntentRecorded,
		Summary:   "repair intent recorded",
		CreatedAt: time.Unix(1710000100, 0).UTC(),
	}
	handler := &fakeOrchestratorService{
		statusFn: func(_ context.Context, _ string) (orchestrator.StatusTaskResult, error) {
			return orchestrator.StatusTaskResult{
				TaskID:                      common.TaskID("tsk_status"),
				Phase:                       phase.PhaseBlocked,
				LatestCheckpointTrigger:     checkpoint.TriggerManual,
				HandoffContinuityState:      orchestrator.HandoffContinuityStateFollowThroughStalled,
				HandoffContinuityReason:     "launch completed and acknowledgment captured, but downstream follow-through appears stalled",
				HandoffContinuationProven:   false,
				LatestAcknowledgmentID:      "hak_1",
				LatestAcknowledgmentStatus:  handoff.AcknowledgmentCaptured,
				LatestAcknowledgmentSummary: "Claude acknowledged the handoff packet.",
				LatestFollowThroughID:       "hft_status",
				LatestFollowThroughKind:     handoff.FollowThroughStalledReviewRequired,
				LatestFollowThroughSummary:  "Claude follow-through looks stalled",
				RecoveryClass:               orchestrator.RecoveryClassHandoffFollowThroughReviewRequired,
				RecommendedAction:           orchestrator.RecoveryActionReviewHandoffFollowThrough,
				LatestRecoveryAction:        action,
			}, nil
		},
		inspectFn: func(_ context.Context, _ string) (orchestrator.InspectTaskResult, error) {
			return orchestrator.InspectTaskResult{
				TaskID:                common.TaskID("tsk_status"),
				LatestRecoveryAction:  action,
				RecentRecoveryActions: []recoveryaction.Record{*action},
				Recovery: &orchestrator.RecoveryAssessment{
					TaskID:            common.TaskID("tsk_status"),
					RecoveryClass:     orchestrator.RecoveryClassHandoffFollowThroughReviewRequired,
					RecommendedAction: orchestrator.RecoveryActionReviewHandoffFollowThrough,
					LatestAction:      action,
				},
				HandoffContinuity: &orchestrator.HandoffContinuity{
					TaskID:                       common.TaskID("tsk_status"),
					HandoffID:                    "hnd_status",
					State:                        orchestrator.HandoffContinuityStateFollowThroughStalled,
					AcknowledgmentID:             "hak_1",
					AcknowledgmentStatus:         handoff.AcknowledgmentCaptured,
					AcknowledgmentSummary:        "Claude acknowledged the handoff packet.",
					FollowThroughID:              "hft_status",
					FollowThroughKind:            handoff.FollowThroughStalledReviewRequired,
					FollowThroughSummary:         "Claude follow-through looks stalled",
					DownstreamContinuationProven: false,
					Reason:                       "launch completed and acknowledgment captured, but downstream follow-through appears stalled",
				},
				FollowThrough: &handoff.FollowThrough{
					Version:         1,
					RecordID:        "hft_status",
					HandoffID:       "hnd_status",
					LaunchAttemptID: "hlc_status",
					LaunchID:        "launch_status",
					TaskID:          common.TaskID("tsk_status"),
					TargetWorker:    run.WorkerKindClaude,
					Kind:            handoff.FollowThroughStalledReviewRequired,
					Summary:         "Claude follow-through looks stalled",
					CreatedAt:       time.Unix(1710000300, 0).UTC(),
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	statusPayload, _ := json.Marshal(ipc.TaskStatusRequest{TaskID: common.TaskID("tsk_status")})
	statusResp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_status_recovery",
		Method:    ipc.MethodTaskStatus,
		Payload:   statusPayload,
	})
	if !statusResp.OK {
		t.Fatalf("expected OK status response, got %+v", statusResp.Error)
	}
	var statusOut ipc.TaskStatusResponse
	if err := json.Unmarshal(statusResp.Payload, &statusOut); err != nil {
		t.Fatalf("unmarshal status response: %v", err)
	}
	if statusOut.LatestRecoveryAction == nil || statusOut.LatestRecoveryAction.ActionID != action.ActionID {
		t.Fatalf("expected latest recovery action in status response, got %+v", statusOut.LatestRecoveryAction)
	}
	if statusOut.HandoffContinuityState != string(orchestrator.HandoffContinuityStateFollowThroughStalled) {
		t.Fatalf("expected handoff continuity state in status response, got %+v", statusOut)
	}
	if statusOut.LatestAcknowledgmentStatus != string(handoff.AcknowledgmentCaptured) {
		t.Fatalf("expected acknowledgment status in status response, got %+v", statusOut)
	}
	if statusOut.LatestFollowThroughKind != string(handoff.FollowThroughStalledReviewRequired) {
		t.Fatalf("expected follow-through status mapping, got %+v", statusOut)
	}

	inspectPayload, _ := json.Marshal(ipc.TaskInspectRequest{TaskID: common.TaskID("tsk_status")})
	inspectResp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_inspect_recovery",
		Method:    ipc.MethodTaskInspect,
		Payload:   inspectPayload,
	})
	if !inspectResp.OK {
		t.Fatalf("expected OK inspect response, got %+v", inspectResp.Error)
	}
	var inspectOut ipc.TaskInspectResponse
	if err := json.Unmarshal(inspectResp.Payload, &inspectOut); err != nil {
		t.Fatalf("unmarshal inspect response: %v", err)
	}
	if inspectOut.LatestRecoveryAction == nil || inspectOut.LatestRecoveryAction.ActionID != action.ActionID {
		t.Fatalf("expected latest recovery action in inspect response, got %+v", inspectOut.LatestRecoveryAction)
	}
	if len(inspectOut.RecentRecoveryActions) != 1 || inspectOut.RecentRecoveryActions[0].ActionID != action.ActionID {
		t.Fatalf("expected recent recovery action in inspect response, got %+v", inspectOut.RecentRecoveryActions)
	}
	if inspectOut.Recovery == nil || inspectOut.Recovery.LatestAction == nil || inspectOut.Recovery.LatestAction.ActionID != action.ActionID {
		t.Fatalf("expected recovery latest action mapping, got %+v", inspectOut.Recovery)
	}
	if inspectOut.HandoffContinuity == nil || inspectOut.HandoffContinuity.State != string(orchestrator.HandoffContinuityStateFollowThroughStalled) {
		t.Fatalf("expected handoff continuity in inspect response, got %+v", inspectOut.HandoffContinuity)
	}
	if inspectOut.FollowThrough == nil || inspectOut.FollowThrough.Kind != handoff.FollowThroughStalledReviewRequired {
		t.Fatalf("expected inspect follow-through mapping, got %+v", inspectOut.FollowThrough)
	}
}

func TestHandleRequestRecordHandoffFollowThroughRoute(t *testing.T) {
	var captured orchestrator.RecordHandoffFollowThroughRequest
	handler := &fakeOrchestratorService{
		recordHandoffFollowThroughFn: func(_ context.Context, req orchestrator.RecordHandoffFollowThroughRequest) (orchestrator.RecordHandoffFollowThroughResult, error) {
			captured = req
			return orchestrator.RecordHandoffFollowThroughResult{
				TaskID: common.TaskID(req.TaskID),
				Record: handoff.FollowThrough{
					Version:         1,
					RecordID:        "hft_1",
					HandoffID:       "hnd_1",
					LaunchAttemptID: "hlc_1",
					LaunchID:        "launch_1",
					TaskID:          common.TaskID(req.TaskID),
					TargetWorker:    run.WorkerKindClaude,
					Kind:            req.Kind,
					Summary:         req.Summary,
					CreatedAt:       time.Unix(1710000200, 0).UTC(),
				},
				HandoffContinuity: orchestrator.HandoffContinuity{
					TaskID:                       common.TaskID(req.TaskID),
					HandoffID:                    "hnd_1",
					State:                        orchestrator.HandoffContinuityStateFollowThroughProofOfLife,
					LaunchAttemptID:              "hlc_1",
					LaunchID:                     "launch_1",
					FollowThroughID:              "hft_1",
					FollowThroughKind:            req.Kind,
					FollowThroughSummary:         req.Summary,
					DownstreamContinuationProven: true,
					Reason:                       "downstream proof of life observed",
				},
				RecoveryClass:         orchestrator.RecoveryClassHandoffLaunchCompleted,
				RecommendedAction:     orchestrator.RecoveryActionMonitorLaunchedHandoff,
				ReadyForNextRun:       false,
				ReadyForHandoffLaunch: false,
				RecoveryReason:        "launched handoff remains monitor-only",
				CanonicalResponse:     "follow-through recorded",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskHandoffFollowThroughRecordRequest{
		TaskID:  common.TaskID("tsk_follow"),
		Kind:    string(handoff.FollowThroughProofOfLifeObserved),
		Summary: "later Claude proof of life observed",
		Notes:   []string{"operator confirmed downstream ping"},
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_followthrough",
		Method:    ipc.MethodRecordHandoffFollowThrough,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got %+v", resp.Error)
	}
	if captured.TaskID != "tsk_follow" || captured.Kind != handoff.FollowThroughProofOfLifeObserved {
		t.Fatalf("unexpected captured follow-through request: %+v", captured)
	}
	var out ipc.TaskHandoffFollowThroughRecordResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal follow-through response: %v", err)
	}
	if out.Record == nil || out.Record.Kind != handoff.FollowThroughProofOfLifeObserved {
		t.Fatalf("expected recorded follow-through payload, got %+v", out.Record)
	}
	if out.HandoffContinuity == nil || out.HandoffContinuity.State != string(orchestrator.HandoffContinuityStateFollowThroughProofOfLife) {
		t.Fatalf("expected follow-through continuity mapping, got %+v", out.HandoffContinuity)
	}
	if out.RecoveryClass != string(orchestrator.RecoveryClassHandoffLaunchCompleted) {
		t.Fatalf("expected monitor recovery mapping, got %+v", out)
	}
}

func TestHandleRequestShellLifecycleRoute(t *testing.T) {
	var captured orchestrator.RecordShellLifecycleRequest
	handler := &fakeOrchestratorService{
		recordShellLifecycleFn: func(_ context.Context, req orchestrator.RecordShellLifecycleRequest) (orchestrator.RecordShellLifecycleResult, error) {
			captured = req
			return orchestrator.RecordShellLifecycleResult{TaskID: common.TaskID(req.TaskID)}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	exitCode := 9
	payload, _ := json.Marshal(ipc.TaskShellLifecycleRequest{
		TaskID:     common.TaskID("tsk_shell"),
		SessionID:  "shs_123",
		Kind:       "host_exited",
		HostMode:   "codex-pty",
		HostState:  "exited",
		Note:       "codex exited with code 9",
		InputLive:  false,
		ExitCode:   &exitCode,
		PaneWidth:  80,
		PaneHeight: 24,
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_4",
		Method:    ipc.MethodTaskShellLifecycle,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.SessionID != "shs_123" || captured.Kind != orchestrator.ShellLifecycleHostExited {
		t.Fatalf("unexpected shell lifecycle request: %+v", captured)
	}
}

func TestHandleRequestShellSessionReportRoute(t *testing.T) {
	var captured orchestrator.ReportShellSessionRequest
	handler := &fakeOrchestratorService{
		reportShellSessionFn: func(_ context.Context, req orchestrator.ReportShellSessionRequest) (orchestrator.ReportShellSessionResult, error) {
			captured = req
			return orchestrator.ReportShellSessionResult{
				TaskID: common.TaskID(req.TaskID),
				Session: orchestrator.ShellSessionView{
					TaskID:           common.TaskID(req.TaskID),
					SessionID:        req.SessionID,
					WorkerPreference: req.WorkerPreference,
					ResolvedWorker:   req.ResolvedWorker,
					WorkerSessionID:  req.WorkerSessionID,
					AttachCapability: req.AttachCapability,
					HostMode:         req.HostMode,
					HostState:        req.HostState,
					StartedAt:        req.StartedAt,
					Active:           req.Active,
					Note:             req.Note,
					SessionClass:     orchestrator.ShellSessionClassAttachable,
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskShellSessionReportRequest{
		TaskID:           common.TaskID("tsk_shell"),
		SessionID:        "shs_456",
		WorkerPreference: "auto",
		ResolvedWorker:   "claude",
		WorkerSessionID:  "wks_456",
		AttachCapability: "attachable",
		HostMode:         "claude-pty",
		HostState:        "starting",
		Active:           true,
		Note:             "shell session registered",
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_5",
		Method:    ipc.MethodTaskShellSessionReport,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.SessionID != "shs_456" || captured.ResolvedWorker != "claude" {
		t.Fatalf("unexpected shell session report request: %+v", captured)
	}
	var out ipc.TaskShellSessionReportResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal shell session report response: %v", err)
	}
	if out.Session.SessionClass != "attachable" || out.Session.WorkerSessionID != "wks_456" || out.Session.AttachCapability != "attachable" {
		t.Fatalf("expected active session class, got %+v", out.Session)
	}
}

func TestHandleRequestShellSessionsRoute(t *testing.T) {
	handler := &fakeOrchestratorService{
		listShellSessionsFn: func(_ context.Context, taskID string) (orchestrator.ListShellSessionsResult, error) {
			return orchestrator.ListShellSessionsResult{
				TaskID: common.TaskID(taskID),
				Sessions: []orchestrator.ShellSessionView{
					{
						TaskID:           common.TaskID(taskID),
						SessionID:        "shs_1",
						WorkerPreference: "auto",
						ResolvedWorker:   "codex",
						WorkerSessionID:  "wks_1",
						AttachCapability: shellsession.AttachCapabilityNone,
						HostMode:         "codex-pty",
						HostState:        "live",
						Active:           true,
						SessionClass:     orchestrator.ShellSessionClassStale,
					},
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskShellSessionsRequest{TaskID: common.TaskID("tsk_123")})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_6",
		Method:    ipc.MethodTaskShellSessions,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	var out ipc.TaskShellSessionsResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal shell sessions response: %v", err)
	}
	if len(out.Sessions) != 1 || out.Sessions[0].ResolvedWorker != "codex" {
		t.Fatalf("unexpected shell sessions payload: %+v", out)
	}
	if out.Sessions[0].SessionClass != "stale" || out.Sessions[0].WorkerSessionID != "wks_1" || out.Sessions[0].AttachCapability != "none" {
		t.Fatalf("expected stale session class, got %+v", out.Sessions[0])
	}
}

func TestHandleRequestShellSessionsRouteReadsDurableRecordsAfterCoordinatorRecreation(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tuku-shell-route.db")
	store, err := sqlite.NewStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}

	coord, err := orchestrator.NewCoordinator(orchestrator.Dependencies{
		Store:          store,
		IntentCompiler: orchestrator.NewIntentStubCompiler(),
		BriefBuilder:   orchestrator.NewBriefBuilderV1(nil, nil),
		Synthesizer:    canonical.NewSimpleSynthesizer(),
		AnchorProvider: anchorgit.NewGitProvider(),
		ShellSessions:  store.ShellSessions(),
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	repoRoot := t.TempDir()
	start, err := coord.StartTask(context.Background(), "Shell route durability", repoRoot)
	if err != nil {
		t.Fatalf("start task: %v", err)
	}
	if _, err := coord.MessageTask(context.Background(), string(start.TaskID), "prepare shell session route"); err != nil {
		t.Fatalf("message task: %v", err)
	}
	if _, err := coord.ReportShellSession(context.Background(), orchestrator.ReportShellSessionRequest{
		TaskID:           string(start.TaskID),
		SessionID:        "shs_durable",
		WorkerPreference: "auto",
		ResolvedWorker:   "codex",
		WorkerSessionID:  "wks_durable",
		AttachCapability: shellsession.AttachCapabilityAttachable,
		HostMode:         "codex-pty",
		HostState:        "live",
		StartedAt:        time.Unix(1710000000, 0).UTC(),
		Active:           true,
	}); err != nil {
		t.Fatalf("report shell session: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite store: %v", err)
	}

	reopened, err := sqlite.NewStore(dbPath)
	if err != nil {
		t.Fatalf("reopen sqlite store: %v", err)
	}
	defer reopened.Close()
	coord2, err := orchestrator.NewCoordinator(orchestrator.Dependencies{
		Store:          reopened,
		IntentCompiler: orchestrator.NewIntentStubCompiler(),
		BriefBuilder:   orchestrator.NewBriefBuilderV1(nil, nil),
		Synthesizer:    canonical.NewSimpleSynthesizer(),
		AnchorProvider: anchorgit.NewGitProvider(),
		ShellSessions:  reopened.ShellSessions(),
	})
	if err != nil {
		t.Fatalf("new reopened coordinator: %v", err)
	}

	svc := NewService("/tmp/unused.sock", coord2)
	payload, _ := json.Marshal(ipc.TaskShellSessionsRequest{TaskID: start.TaskID})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_durable",
		Method:    ipc.MethodTaskShellSessions,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	var out ipc.TaskShellSessionsResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal durable shell sessions response: %v", err)
	}
	if len(out.Sessions) != 1 {
		t.Fatalf("expected one durable shell session in route response, got %d", len(out.Sessions))
	}
	if out.Sessions[0].SessionID != "shs_durable" || out.Sessions[0].WorkerSessionID != "wks_durable" || out.Sessions[0].AttachCapability != "attachable" {
		t.Fatalf("unexpected durable shell session payload: %+v", out.Sessions[0])
	}
}

type fakeOrchestratorService struct {
	resolveShellTaskForRepoFn    func(context.Context, string, string) (orchestrator.ResolveShellTaskResult, error)
	createHandoffFn              func(context.Context, orchestrator.CreateHandoffRequest) (orchestrator.CreateHandoffResult, error)
	acceptHandoffFn              func(context.Context, orchestrator.AcceptHandoffRequest) (orchestrator.AcceptHandoffResult, error)
	recordHandoffFollowThroughFn func(context.Context, orchestrator.RecordHandoffFollowThroughRequest) (orchestrator.RecordHandoffFollowThroughResult, error)
	recordRecoveryActionFn       func(context.Context, orchestrator.RecordRecoveryActionRequest) (orchestrator.RecordRecoveryActionResult, error)
	executeRebriefFn             func(context.Context, orchestrator.ExecuteRebriefRequest) (orchestrator.ExecuteRebriefResult, error)
	executeInterruptedResumeFn   func(context.Context, orchestrator.ExecuteInterruptedResumeRequest) (orchestrator.ExecuteInterruptedResumeResult, error)
	executeContinueRecoveryFn    func(context.Context, orchestrator.ExecuteContinueRecoveryRequest) (orchestrator.ExecuteContinueRecoveryResult, error)
	statusFn                     func(context.Context, string) (orchestrator.StatusTaskResult, error)
	inspectFn                    func(context.Context, string) (orchestrator.InspectTaskResult, error)
	shellSnapshotFn              func(context.Context, string) (orchestrator.ShellSnapshotResult, error)
	recordShellLifecycleFn       func(context.Context, orchestrator.RecordShellLifecycleRequest) (orchestrator.RecordShellLifecycleResult, error)
	reportShellSessionFn         func(context.Context, orchestrator.ReportShellSessionRequest) (orchestrator.ReportShellSessionResult, error)
	listShellSessionsFn          func(context.Context, string) (orchestrator.ListShellSessionsResult, error)
}

func (f *fakeOrchestratorService) ResolveShellTaskForRepo(ctx context.Context, repoRoot string, defaultGoal string) (orchestrator.ResolveShellTaskResult, error) {
	if f.resolveShellTaskForRepoFn != nil {
		return f.resolveShellTaskForRepoFn(ctx, repoRoot, defaultGoal)
	}
	return orchestrator.ResolveShellTaskResult{}, nil
}

func (f *fakeOrchestratorService) StartTask(_ context.Context, _ string, _ string) (orchestrator.StartTaskResult, error) {
	return orchestrator.StartTaskResult{}, nil
}

func (f *fakeOrchestratorService) MessageTask(_ context.Context, _, _ string) (orchestrator.MessageTaskResult, error) {
	return orchestrator.MessageTaskResult{}, nil
}

func (f *fakeOrchestratorService) RunTask(_ context.Context, _ orchestrator.RunTaskRequest) (orchestrator.RunTaskResult, error) {
	return orchestrator.RunTaskResult{}, nil
}

func (f *fakeOrchestratorService) ContinueTask(_ context.Context, _ string) (orchestrator.ContinueTaskResult, error) {
	return orchestrator.ContinueTaskResult{}, nil
}

func (f *fakeOrchestratorService) RecordRecoveryAction(ctx context.Context, req orchestrator.RecordRecoveryActionRequest) (orchestrator.RecordRecoveryActionResult, error) {
	if f.recordRecoveryActionFn != nil {
		return f.recordRecoveryActionFn(ctx, req)
	}
	return orchestrator.RecordRecoveryActionResult{}, nil
}

func (f *fakeOrchestratorService) ExecuteRebrief(ctx context.Context, req orchestrator.ExecuteRebriefRequest) (orchestrator.ExecuteRebriefResult, error) {
	if f.executeRebriefFn != nil {
		return f.executeRebriefFn(ctx, req)
	}
	return orchestrator.ExecuteRebriefResult{}, nil
}

func (f *fakeOrchestratorService) ExecuteInterruptedResume(ctx context.Context, req orchestrator.ExecuteInterruptedResumeRequest) (orchestrator.ExecuteInterruptedResumeResult, error) {
	if f.executeInterruptedResumeFn != nil {
		return f.executeInterruptedResumeFn(ctx, req)
	}
	return orchestrator.ExecuteInterruptedResumeResult{}, nil
}

func (f *fakeOrchestratorService) ExecuteContinueRecovery(ctx context.Context, req orchestrator.ExecuteContinueRecoveryRequest) (orchestrator.ExecuteContinueRecoveryResult, error) {
	if f.executeContinueRecoveryFn != nil {
		return f.executeContinueRecoveryFn(ctx, req)
	}
	return orchestrator.ExecuteContinueRecoveryResult{}, nil
}

func (f *fakeOrchestratorService) CreateCheckpoint(_ context.Context, _ string) (orchestrator.CreateCheckpointResult, error) {
	return orchestrator.CreateCheckpointResult{}, nil
}

func (f *fakeOrchestratorService) CreateHandoff(ctx context.Context, req orchestrator.CreateHandoffRequest) (orchestrator.CreateHandoffResult, error) {
	if f.createHandoffFn != nil {
		return f.createHandoffFn(ctx, req)
	}
	return orchestrator.CreateHandoffResult{}, nil
}

func (f *fakeOrchestratorService) AcceptHandoff(ctx context.Context, req orchestrator.AcceptHandoffRequest) (orchestrator.AcceptHandoffResult, error) {
	if f.acceptHandoffFn != nil {
		return f.acceptHandoffFn(ctx, req)
	}
	return orchestrator.AcceptHandoffResult{}, nil
}

func (f *fakeOrchestratorService) LaunchHandoff(_ context.Context, _ orchestrator.LaunchHandoffRequest) (orchestrator.LaunchHandoffResult, error) {
	return orchestrator.LaunchHandoffResult{}, nil
}

func (f *fakeOrchestratorService) RecordHandoffFollowThrough(ctx context.Context, req orchestrator.RecordHandoffFollowThroughRequest) (orchestrator.RecordHandoffFollowThroughResult, error) {
	if f.recordHandoffFollowThroughFn != nil {
		return f.recordHandoffFollowThroughFn(ctx, req)
	}
	return orchestrator.RecordHandoffFollowThroughResult{}, nil
}

func (f *fakeOrchestratorService) StatusTask(ctx context.Context, taskID string) (orchestrator.StatusTaskResult, error) {
	if f.statusFn != nil {
		return f.statusFn(ctx, taskID)
	}
	return orchestrator.StatusTaskResult{
		Phase:                   phase.PhaseIntake,
		LatestCheckpointTrigger: checkpoint.TriggerManual,
	}, nil
}

func (f *fakeOrchestratorService) InspectTask(ctx context.Context, taskID string) (orchestrator.InspectTaskResult, error) {
	if f.inspectFn != nil {
		return f.inspectFn(ctx, taskID)
	}
	return orchestrator.InspectTaskResult{}, nil
}

func (f *fakeOrchestratorService) ShellSnapshotTask(ctx context.Context, taskID string) (orchestrator.ShellSnapshotResult, error) {
	if f.shellSnapshotFn != nil {
		return f.shellSnapshotFn(ctx, taskID)
	}
	return orchestrator.ShellSnapshotResult{}, nil
}

func (f *fakeOrchestratorService) RecordShellLifecycle(ctx context.Context, req orchestrator.RecordShellLifecycleRequest) (orchestrator.RecordShellLifecycleResult, error) {
	if f.recordShellLifecycleFn != nil {
		return f.recordShellLifecycleFn(ctx, req)
	}
	return orchestrator.RecordShellLifecycleResult{}, nil
}

func (f *fakeOrchestratorService) ReportShellSession(ctx context.Context, req orchestrator.ReportShellSessionRequest) (orchestrator.ReportShellSessionResult, error) {
	if f.reportShellSessionFn != nil {
		return f.reportShellSessionFn(ctx, req)
	}
	return orchestrator.ReportShellSessionResult{}, nil
}

func (f *fakeOrchestratorService) ListShellSessions(ctx context.Context, taskID string) (orchestrator.ListShellSessionsResult, error) {
	if f.listShellSessionsFn != nil {
		return f.listShellSessionsFn(ctx, taskID)
	}
	return orchestrator.ListShellSessionsResult{}, nil
}

```


## internal/app/bootstrap.go

```go
package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"tuku/internal/adapters/claude"
	"tuku/internal/adapters/codex"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/recoveryaction"
	rundomain "tuku/internal/domain/run"
	anchorgit "tuku/internal/git/anchor"
	"tuku/internal/ipc"
	"tuku/internal/orchestrator"
	"tuku/internal/response/canonical"
	daemonruntime "tuku/internal/runtime/daemon"
	"tuku/internal/storage/sqlite"
	tukushell "tuku/internal/tui/shell"
)

// CLIApplication is the top-level command host for the user-facing Tuku CLI.
type CLIApplication struct {
	openShellFn         func(ctx context.Context, socketPath string, taskID string, preference tukushell.WorkerPreference) error
	openFallbackShellFn func(ctx context.Context, cwd string, preference tukushell.WorkerPreference) error
}

type repoShellTaskResolution struct {
	TaskID   common.TaskID
	RepoRoot string
	Created  bool
}

// DaemonApplication is the top-level process host for the local Tuku daemon.
type DaemonApplication struct{}

func NewCLIApplication() *CLIApplication {
	return &CLIApplication{}
}

func NewDaemonApplication() *DaemonApplication {
	return &DaemonApplication{}
}

var (
	getWorkingDir          = os.Getwd
	resolveRepoRootFromDir = anchorgit.ResolveRepoRoot
	ipcCall                = ipc.CallUnix
	startLocalDaemon       = launchLocalDaemonProcess
	resolveScratchPath     = defaultScratchSessionPath
	daemonReadyTimeout     = 5 * time.Second
	daemonRetryInterval    = 150 * time.Millisecond
)

func (a *CLIApplication) Run(ctx context.Context, args []string) error {
	if len(args) > 0 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		_, _ = fmt.Fprintln(os.Stdout, cliUsage())
		return nil
	}

	socketPath, err := defaultSocketPath()
	if err != nil {
		return err
	}

	if len(args) == 0 {
		return a.runPrimaryEntry(ctx, socketPath, nil)
	}

	switch args[0] {
	case "chat":
		return a.runPrimaryEntry(ctx, socketPath, args[1:])
	case "start":
		fs := flag.NewFlagSet("start", flag.ContinueOnError)
		goal := fs.String("goal", "", "task goal")
		repo := fs.String("repo", ".", "repo root")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload, _ := json.Marshal(ipc.StartTaskRequest{Goal: *goal, RepoRoot: *repo})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodStartTask, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.StartTaskResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "message":
		fs := flag.NewFlagSet("message", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		message := fs.String("text", "", "user message")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		if *message == "" {
			return errors.New("--text is required")
		}
		payload, _ := json.Marshal(map[string]any{"task_id": *task, "message": *message})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodSendMessage, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskMessageResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "status":
		fs := flag.NewFlagSet("status", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(map[string]any{"task_id": *task})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodTaskStatus, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskStatusResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "shell":
		fs := flag.NewFlagSet("shell", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		worker := fs.String("worker", "auto", "worker preference: auto|codex|claude")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		preference, err := parseShellWorkerPreference(*worker)
		if err != nil {
			return err
		}
		return a.openShell(ctx, socketPath, *task, preference)

	case "shell-sessions":
		fs := flag.NewFlagSet("shell-sessions", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(ipc.TaskShellSessionsRequest{TaskID: common.TaskID(*task)})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodTaskShellSessions, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskShellSessionsResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "run":
		fs := flag.NewFlagSet("run", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		action := fs.String("action", "start", "run action: start|complete|interrupt")
		mode := fs.String("mode", "real", "run mode: real|noop")
		runID := fs.String("run-id", "", "run id for complete/interrupt actions")
		simInterrupt := fs.Bool("simulate-interrupt", false, "start then immediately interrupt")
		reason := fs.String("reason", "", "interruption reason")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(map[string]any{
			"task_id":             *task,
			"action":              *action,
			"mode":                *mode,
			"run_id":              *runID,
			"simulate_interrupt":  *simInterrupt,
			"interruption_reason": *reason,
		})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodTaskRun, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskRunResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "continue":
		fs := flag.NewFlagSet("continue", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(ipc.TaskContinueRequest{TaskID: common.TaskID(*task)})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodContinueTask, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskContinueResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "checkpoint":
		fs := flag.NewFlagSet("checkpoint", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(ipc.TaskCheckpointRequest{TaskID: common.TaskID(*task)})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodCreateCheckpoint, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskCheckpointResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "recovery":
		if len(args) < 2 {
			return errors.New("usage: tuku recovery <record|review-interrupted|resume-interrupted|rebrief|continue> ...")
		}
		switch args[1] {
		case "record":
			fs := flag.NewFlagSet("recovery record", flag.ContinueOnError)
			task := fs.String("task", "", "task id")
			action := fs.String("action", "", "recovery action kind")
			summary := fs.String("summary", "", "optional recovery action summary")
			note := fs.String("note", "", "optional recovery action note")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			if *task == "" {
				return errors.New("--task is required")
			}
			if *action == "" {
				return errors.New("--action is required")
			}
			kind, err := parseRecoveryActionKind(*action)
			if err != nil {
				return err
			}
			notes := []string{}
			if trimmed := strings.TrimSpace(*note); trimmed != "" {
				notes = append(notes, trimmed)
			}
			payload, _ := json.Marshal(ipc.TaskRecordRecoveryActionRequest{
				TaskID:  common.TaskID(*task),
				Kind:    string(kind),
				Summary: strings.TrimSpace(*summary),
				Notes:   notes,
			})
			resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodRecordRecoveryAction, Payload: payload})
			if err != nil {
				return err
			}
			var out ipc.TaskRecordRecoveryActionResponse
			if err := json.Unmarshal(resp.Payload, &out); err != nil {
				return err
			}
			return writeJSON(os.Stdout, out)
		case "review-interrupted":
			fs := flag.NewFlagSet("recovery review-interrupted", flag.ContinueOnError)
			task := fs.String("task", "", "task id")
			summary := fs.String("summary", "", "optional interrupted recovery summary")
			note := fs.String("note", "", "optional interrupted recovery note")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			if *task == "" {
				return errors.New("--task is required")
			}
			notes := []string{}
			if trimmed := strings.TrimSpace(*note); trimmed != "" {
				notes = append(notes, trimmed)
			}
			payload, _ := json.Marshal(ipc.TaskReviewInterruptedRunRequest{
				TaskID:  common.TaskID(*task),
				Summary: strings.TrimSpace(*summary),
				Notes:   notes,
			})
			resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodReviewInterruptedRun, Payload: payload})
			if err != nil {
				return err
			}
			var out ipc.TaskRecordRecoveryActionResponse
			if err := json.Unmarshal(resp.Payload, &out); err != nil {
				return err
			}
			return writeJSON(os.Stdout, out)
		case "continue":
			fs := flag.NewFlagSet("recovery continue", flag.ContinueOnError)
			task := fs.String("task", "", "task id")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			if *task == "" {
				return errors.New("--task is required")
			}
			payload, _ := json.Marshal(ipc.TaskContinueRecoveryRequest{TaskID: common.TaskID(*task)})
			resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodExecuteContinueRecovery, Payload: payload})
			if err != nil {
				return err
			}
			var out ipc.TaskContinueRecoveryResponse
			if err := json.Unmarshal(resp.Payload, &out); err != nil {
				return err
			}
			return writeJSON(os.Stdout, out)
		case "rebrief":
			fs := flag.NewFlagSet("recovery rebrief", flag.ContinueOnError)
			task := fs.String("task", "", "task id")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			if *task == "" {
				return errors.New("--task is required")
			}
			payload, _ := json.Marshal(ipc.TaskRebriefRequest{TaskID: common.TaskID(*task)})
			resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodExecuteRebrief, Payload: payload})
			if err != nil {
				return err
			}
			var out ipc.TaskRebriefResponse
			if err := json.Unmarshal(resp.Payload, &out); err != nil {
				return err
			}
			return writeJSON(os.Stdout, out)
		case "resume-interrupted":
			fs := flag.NewFlagSet("recovery resume-interrupted", flag.ContinueOnError)
			task := fs.String("task", "", "task id")
			summary := fs.String("summary", "", "optional interrupted resume summary")
			note := fs.String("note", "", "optional interrupted resume note")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			if *task == "" {
				return errors.New("--task is required")
			}
			notes := []string{}
			if trimmed := strings.TrimSpace(*note); trimmed != "" {
				notes = append(notes, trimmed)
			}
			payload, _ := json.Marshal(ipc.TaskInterruptedResumeRequest{
				TaskID:  common.TaskID(*task),
				Summary: strings.TrimSpace(*summary),
				Notes:   notes,
			})
			resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodExecuteInterruptedResume, Payload: payload})
			if err != nil {
				return err
			}
			var out ipc.TaskInterruptedResumeResponse
			if err := json.Unmarshal(resp.Payload, &out); err != nil {
				return err
			}
			return writeJSON(os.Stdout, out)
		default:
			return fmt.Errorf("unknown recovery command: %s", args[1])
		}

	case "handoff-followthrough":
		fs := flag.NewFlagSet("handoff-followthrough", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		kindValue := fs.String("kind", "", "follow-through kind")
		summary := fs.String("summary", "", "optional follow-through summary")
		note := fs.String("note", "", "optional follow-through note")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		if *kindValue == "" {
			return errors.New("--kind is required")
		}
		kind, err := parseHandoffFollowThroughKind(*kindValue)
		if err != nil {
			return err
		}
		notes := []string{}
		if trimmed := strings.TrimSpace(*note); trimmed != "" {
			notes = append(notes, trimmed)
		}
		payload, _ := json.Marshal(ipc.TaskHandoffFollowThroughRecordRequest{
			TaskID:  common.TaskID(*task),
			Kind:    string(kind),
			Summary: strings.TrimSpace(*summary),
			Notes:   notes,
		})
		resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodRecordHandoffFollowThrough, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskHandoffFollowThroughRecordResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "handoff-create":
		fs := flag.NewFlagSet("handoff-create", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		target := fs.String("target", string(rundomain.WorkerKindClaude), "target worker (claude)")
		mode := fs.String("mode", string(handoff.ModeResume), "handoff mode: resume|review|takeover")
		reason := fs.String("reason", "", "handoff reason")
		note := fs.String("note", "", "optional handoff note")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		notes := []string{}
		if trimmed := strings.TrimSpace(*note); trimmed != "" {
			notes = append(notes, trimmed)
		}
		payload, _ := json.Marshal(ipc.TaskHandoffCreateRequest{
			TaskID:       common.TaskID(*task),
			TargetWorker: rundomain.WorkerKind(*target),
			Reason:       *reason,
			Mode:         handoff.Mode(*mode),
			Notes:        notes,
		})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodCreateHandoff, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskHandoffCreateResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "handoff-accept":
		fs := flag.NewFlagSet("handoff-accept", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		handoffID := fs.String("handoff", "", "handoff packet id")
		acceptedBy := fs.String("by", string(rundomain.WorkerKindClaude), "accepted-by worker")
		note := fs.String("note", "", "optional acceptance note")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		if *handoffID == "" {
			return errors.New("--handoff is required")
		}
		notes := []string{}
		if trimmed := strings.TrimSpace(*note); trimmed != "" {
			notes = append(notes, trimmed)
		}
		payload, _ := json.Marshal(ipc.TaskHandoffAcceptRequest{
			TaskID:     common.TaskID(*task),
			HandoffID:  *handoffID,
			AcceptedBy: rundomain.WorkerKind(*acceptedBy),
			Notes:      notes,
		})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodAcceptHandoff, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskHandoffAcceptResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "handoff-launch":
		fs := flag.NewFlagSet("handoff-launch", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		handoffID := fs.String("handoff", "", "handoff packet id (optional; defaults to latest for task)")
		target := fs.String("target", "", "target worker override (optional; must match packet target if set)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(ipc.TaskHandoffLaunchRequest{
			TaskID:       common.TaskID(*task),
			HandoffID:    *handoffID,
			TargetWorker: rundomain.WorkerKind(*target),
		})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodLaunchHandoff, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskHandoffLaunchResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "inspect":
		fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(map[string]any{"task_id": *task})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodTaskInspect, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskInspectResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	default:
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func cliUsage() string {
	return "usage: tuku [chat] | tuku <start|message|shell|shell-sessions|run|continue|checkpoint|recovery|handoff-create|handoff-accept|handoff-launch|handoff-followthrough|status|inspect|help> [flags]"
}

func parseRecoveryActionKind(value string) (recoveryaction.Kind, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	switch normalized {
	case "failed-run-reviewed":
		return recoveryaction.KindFailedRunReviewed, nil
	case "interrupted-run-reviewed":
		return recoveryaction.KindInterruptedRunReviewed, nil
	case "validation-reviewed":
		return recoveryaction.KindValidationReviewed, nil
	case "decision-continue":
		return recoveryaction.KindDecisionContinue, nil
	case "decision-regenerate-brief":
		return recoveryaction.KindDecisionRegenerateBrief, nil
	case "repair-intent-recorded":
		return recoveryaction.KindRepairIntentRecorded, nil
	case "pending-launch-reviewed":
		return recoveryaction.KindPendingLaunchReviewed, nil
	default:
		return "", fmt.Errorf("unsupported recovery action %q", value)
	}
}

func parseHandoffFollowThroughKind(value string) (handoff.FollowThroughKind, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	switch normalized {
	case "proof-of-life-observed":
		return handoff.FollowThroughProofOfLifeObserved, nil
	case "continuation-confirmed":
		return handoff.FollowThroughContinuationConfirmed, nil
	case "continuation-unknown":
		return handoff.FollowThroughContinuationUnknown, nil
	case "stalled-review-required":
		return handoff.FollowThroughStalledReviewRequired, nil
	default:
		return "", fmt.Errorf("unsupported handoff follow-through kind %q", value)
	}
}

func (a *DaemonApplication) Run(ctx context.Context) error {
	dbPath, err := defaultDBPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return err
	}

	store, err := sqlite.NewStore(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	coord, err := orchestrator.NewCoordinator(orchestrator.Dependencies{
		Store:           store,
		IntentCompiler:  orchestrator.NewIntentStubCompiler(),
		BriefBuilder:    orchestrator.NewBriefBuilderV1(nil, nil),
		WorkerAdapter:   codex.NewAdapter(),
		HandoffLauncher: claude.NewLauncher(),
		Synthesizer:     canonical.NewSimpleSynthesizer(),
		AnchorProvider:  anchorgit.NewGitProvider(),
		ShellSessions:   store.ShellSessions(),
	})
	if err != nil {
		return err
	}

	socketPath, err := defaultSocketPath()
	if err != nil {
		return err
	}
	service := daemonruntime.NewService(socketPath, coord)
	return service.Run(ctx)
}

func defaultDataRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", "Tuku"), nil
}

func defaultDBPath() (string, error) {
	root, err := defaultDataRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "tuku.db"), nil
}

func defaultSocketPath() (string, error) {
	root, err := defaultDataRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "run", "tukud.sock"), nil
}

func requestID() string {
	return fmt.Sprintf("req_%d", time.Now().UTC().UnixNano())
}

func (a *CLIApplication) runPrimaryEntry(ctx context.Context, socketPath string, args []string) error {
	fs := flag.NewFlagSet("chat", flag.ContinueOnError)
	worker := fs.String("worker", "auto", "worker preference: auto|codex|claude")
	if err := fs.Parse(args); err != nil {
		return err
	}
	preference, err := parseShellWorkerPreference(*worker)
	if err != nil {
		return err
	}
	cwd, repoRoot, repoDetected, err := resolvePrimaryEntryContext(ctx)
	if err != nil {
		return err
	}
	if !repoDetected {
		openFallback := a.openPrimaryFallbackShell
		if a.openFallbackShellFn != nil {
			openFallback = a.openFallbackShellFn
		}
		return openFallback(ctx, cwd, preference)
	}
	resolution, err := resolveShellTaskForRepoWithDaemonBootstrap(ctx, socketPath, repoRoot, orchestrator.DefaultRepoContinueGoal)
	if err != nil {
		return err
	}
	if a.openShellFn != nil {
		return a.openShellFn(ctx, socketPath, string(resolution.TaskID), preference)
	}
	source, err := newPrimaryRepoSnapshotSource(socketPath, repoRoot, resolution.Created)
	if err != nil {
		return err
	}
	return a.openShellWithSource(ctx, string(resolution.TaskID), preference, source)
}

func (a *CLIApplication) openPrimaryFallbackShell(ctx context.Context, cwd string, _ tukushell.WorkerPreference) error {
	scratchPath, err := resolveScratchPath(cwd)
	if err != nil {
		return err
	}
	return newPrimaryScratchIntake(cwd, scratchPath).Run(ctx)
}

func (a *CLIApplication) openShell(ctx context.Context, socketPath string, taskID string, preference tukushell.WorkerPreference) error {
	return a.openShellWithSource(ctx, taskID, preference, tukushell.NewIPCSnapshotSource(socketPath))
}

func (a *CLIApplication) openShellWithSource(ctx context.Context, taskID string, preference tukushell.WorkerPreference, source tukushell.SnapshotSource) error {
	shellApp := tukushell.NewApp(taskID, source)
	shellApp.WorkerPreference = preference
	if socketPath := snapshotSourceSocketPath(source); socketPath != "" {
		shellApp.MessageSender = tukushell.NewIPCTaskMessageSender(socketPath)
		shellApp.LifecycleSink = tukushell.NewIPCLifecycleSink(socketPath)
		shellApp.RegistrySink = tukushell.NewIPCSessionRegistryClient(socketPath)
		shellApp.RegistrySource = tukushell.NewIPCSessionRegistryClient(socketPath)
	}
	return shellApp.Run(ctx)
}

func resolvePrimaryEntryContext(ctx context.Context) (string, string, bool, error) {
	cwd, err := getWorkingDir()
	if err != nil {
		return "", "", false, err
	}
	root, err := resolveRepoRootFromDir(ctx, cwd)
	if err != nil {
		return cwd, "", false, nil
	}
	return cwd, root, true, nil
}

func resolveCurrentRepoRoot(ctx context.Context) (string, error) {
	_, root, repoDetected, err := resolvePrimaryEntryContext(ctx)
	if err != nil {
		return "", err
	}
	if !repoDetected {
		return "", fmt.Errorf("tuku needs a git repository for the primary entry path; current directory is not inside one")
	}
	return root, nil
}

func resolveShellTaskForRepo(ctx context.Context, socketPath string, repoRoot string, defaultGoal string) (repoShellTaskResolution, error) {
	payload, err := json.Marshal(ipc.ResolveShellTaskForRepoRequest{
		RepoRoot:    repoRoot,
		DefaultGoal: defaultGoal,
	})
	if err != nil {
		return repoShellTaskResolution{}, err
	}
	resp, err := ipcCall(ctx, socketPath, ipc.Request{
		RequestID: requestID(),
		Method:    ipc.MethodResolveShellTaskForRepo,
		Payload:   payload,
	})
	if err != nil {
		return repoShellTaskResolution{}, err
	}
	var out ipc.ResolveShellTaskForRepoResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return repoShellTaskResolution{}, err
	}
	return repoShellTaskResolution{
		TaskID:   out.TaskID,
		RepoRoot: out.RepoRoot,
		Created:  out.Created,
	}, nil
}

func resolveShellTaskForRepoWithDaemonBootstrap(ctx context.Context, socketPath string, repoRoot string, defaultGoal string) (repoShellTaskResolution, error) {
	resolution, err := resolveShellTaskForRepo(ctx, socketPath, repoRoot, defaultGoal)
	if err == nil {
		return resolution, nil
	}
	if !isDaemonUnavailableError(err) {
		return repoShellTaskResolution{}, err
	}

	waitCh, err := startLocalDaemon()
	if err != nil {
		return repoShellTaskResolution{}, fmt.Errorf("could not start the local Tuku daemon automatically: %w", err)
	}

	deadline := time.Now().Add(daemonReadyTimeout)
	for {
		resolution, err = resolveShellTaskForRepo(ctx, socketPath, repoRoot, defaultGoal)
		if err == nil {
			return resolution, nil
		}
		if !isDaemonUnavailableError(err) {
			return repoShellTaskResolution{}, err
		}
		select {
		case waitErr, ok := <-waitCh:
			if ok && waitErr != nil {
				return repoShellTaskResolution{}, fmt.Errorf("local Tuku daemon failed to start: %w", waitErr)
			}
			return repoShellTaskResolution{}, errors.New("local Tuku daemon exited before becoming ready")
		default:
		}
		if time.Now().After(deadline) {
			return repoShellTaskResolution{}, fmt.Errorf("local Tuku daemon did not become ready within %s", daemonReadyTimeout)
		}
		if err := sleepWithContext(ctx, daemonRetryInterval); err != nil {
			return repoShellTaskResolution{}, err
		}
	}
}

func isDaemonUnavailableError(err error) bool {
	return errors.Is(err, os.ErrNotExist) ||
		errors.Is(err, syscall.ENOENT) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ENOTCONN)
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func launchLocalDaemonProcess() (<-chan error, error) {
	spec, err := resolveDaemonLaunchSpec()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(spec.Command, spec.Args...)
	if spec.WorkingDir != "" {
		cmd.Dir = spec.WorkingDir
	}
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("launch %s: %w", spec.Label, err)
	}

	waitCh := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		if err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg != "" {
				err = fmt.Errorf("%w: %s", err, msg)
			}
		}
		waitCh <- err
		close(waitCh)
	}()
	return waitCh, nil
}

type daemonLaunchSpec struct {
	Command    string
	Args       []string
	WorkingDir string
	Label      string
}

func resolveDaemonLaunchSpec() (daemonLaunchSpec, error) {
	if path, err := exec.LookPath("tukud"); err == nil {
		return daemonLaunchSpec{
			Command: path,
			Label:   path,
		}, nil
	}
	if exe, err := os.Executable(); err == nil {
		sibling := filepath.Join(filepath.Dir(exe), "tukud")
		if fileExists(sibling) {
			return daemonLaunchSpec{
				Command: sibling,
				Label:   sibling,
			}, nil
		}
	}
	if root, ok := sourceTreeRoot(); ok {
		goBin, err := exec.LookPath("go")
		if err == nil {
			return daemonLaunchSpec{
				Command:    goBin,
				Args:       []string{"run", "./cmd/tukud"},
				WorkingDir: root,
				Label:      "go run ./cmd/tukud",
			}, nil
		}
	}
	return daemonLaunchSpec{}, errors.New("could not locate `tukud`; build or install it, or continue starting `tukud` manually")
}

func sourceTreeRoot() (string, bool) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", false
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	if !fileExists(filepath.Join(root, "go.mod")) {
		return "", false
	}
	if !fileExists(filepath.Join(root, "cmd", "tukud", "main.go")) {
		return "", false
	}
	return root, true
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func defaultScratchSessionPath(cwd string) (string, error) {
	root, err := defaultDataRoot()
	if err != nil {
		return "", err
	}
	normalized := filepath.Clean(strings.TrimSpace(cwd))
	sum := sha256.Sum256([]byte(normalized))
	return filepath.Join(root, "scratch", fmt.Sprintf("%x.json", sum[:])), nil
}

type primaryRepoScratchBridge struct {
	RepoRoot string
	Notes    []tukushell.ConversationItem
}

type primaryRepoScratchBridgeSource struct {
	base   tukushell.SnapshotSource
	bridge *primaryRepoScratchBridge
}

func snapshotSourceSocketPath(source tukushell.SnapshotSource) string {
	switch src := source.(type) {
	case *tukushell.IPCSnapshotSource:
		return src.SocketPath
	case *primaryRepoScratchBridgeSource:
		return snapshotSourceSocketPath(src.base)
	default:
		return ""
	}
}

func newPrimaryRepoSnapshotSource(socketPath string, repoRoot string, created bool) (tukushell.SnapshotSource, error) {
	base := tukushell.NewIPCSnapshotSource(socketPath)
	if !created {
		return base, nil
	}
	bridge, err := loadPrimaryRepoScratchBridge(repoRoot)
	if err != nil {
		return nil, err
	}
	if bridge == nil {
		return base, nil
	}
	return &primaryRepoScratchBridgeSource{
		base:   base,
		bridge: bridge,
	}, nil
}

func loadPrimaryRepoScratchBridge(repoRoot string) (*primaryRepoScratchBridge, error) {
	scratchPath, err := resolveScratchPath(repoRoot)
	if err != nil {
		return nil, err
	}
	notes, err := tukushell.LoadLocalScratchNotes(scratchPath)
	if err != nil {
		return nil, err
	}
	if len(notes) == 0 {
		return nil, nil
	}
	return &primaryRepoScratchBridge{
		RepoRoot: filepath.Clean(strings.TrimSpace(repoRoot)),
		Notes:    notes,
	}, nil
}

func (s *primaryRepoScratchBridgeSource) Load(taskID string) (tukushell.Snapshot, error) {
	snapshot, err := s.base.Load(taskID)
	if err != nil {
		return tukushell.Snapshot{}, err
	}
	return applyPrimaryRepoScratchBridge(snapshot, s.bridge), nil
}

func applyPrimaryRepoScratchBridge(snapshot tukushell.Snapshot, bridge *primaryRepoScratchBridge) tukushell.Snapshot {
	if bridge == nil || len(bridge.Notes) == 0 {
		return snapshot
	}
	surfacedNotes := surfacedScratchBridgeNotes(bridge.Notes, 3)
	out := snapshot
	out.LocalScratch = &tukushell.LocalScratchContext{
		RepoRoot: bridge.RepoRoot,
		Notes:    surfacedNotes,
	}
	out.RecentConversation = append([]tukushell.ConversationItem{}, snapshot.RecentConversation...)
	out.RecentConversation = append(out.RecentConversation, tukushell.ConversationItem{
		Role: "system",
		Body: "Local scratch notes were found for this repo root when this task was first created. They have not been imported into canonical task state.",
	})
	out.RecentConversation = append(out.RecentConversation, tukushell.ConversationItem{
		Role: "system",
		Body: "Use the shell adopt command to stage them into a pending task message. Sending that pending message is the explicit adoption step into real Tuku continuity.",
	})
	out.RecentConversation = append(out.RecentConversation, tukushell.ConversationItem{
		Role: "system",
		Body: "Shell commands: stage local scratch with `a`, send the pending task message with `m`, clear it with `x`. When worker input is live, press Ctrl-G before the command key.",
	})
	for _, note := range surfacedNotes {
		body := strings.TrimSpace(note.Body)
		if body == "" {
			continue
		}
		out.RecentConversation = append(out.RecentConversation, tukushell.ConversationItem{
			Role:      "system",
			Body:      "local scratch note: " + body,
			CreatedAt: note.CreatedAt,
		})
	}
	return out
}

func surfacedScratchBridgeNotes(notes []tukushell.ConversationItem, limit int) []tukushell.ConversationItem {
	if limit <= 0 || len(notes) <= limit {
		return append([]tukushell.ConversationItem{}, notes...)
	}
	start := len(notes) - limit
	return append([]tukushell.ConversationItem{}, notes[start:]...)
}

func parseShellWorkerPreference(raw string) (tukushell.WorkerPreference, error) {
	preference, err := tukushell.ParseWorkerPreference(raw)
	if err != nil {
		return "", fmt.Errorf("invalid --worker: %w", err)
	}
	return preference, nil
}

func writeJSON(out *os.File, value any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func primaryEntryScratchSnapshot(cwd string) tukushell.Snapshot {
	message := "No git repository was detected. Tuku opened a local scratch and intake session instead of repo-backed continuity."
	return tukushell.Snapshot{
		Goal:                    "Local scratch and intake session",
		Phase:                   "SCRATCH_INTAKE",
		Status:                  "LOCAL_ONLY",
		IntentClass:             "scratch",
		IntentSummary:           fmt.Sprintf("Use this local scratch session to plan work, sketch a new project, or prepare to clone or initialize a repository. Current directory: %s", cwd),
		LatestCanonicalResponse: message,
		RecentConversation: []tukushell.ConversationItem{
			{
				Role: "system",
				Body: message,
			},
			{
				Role: "system",
				Body: "This session is local-only. Tuku is not starting the daemon, not creating a task, and not claiming repo-backed continuity here.",
			},
			{
				Role: "system",
				Body: "Good uses for this mode: outline a new project, define milestones, list requirements, or prepare the next step before a repository exists.",
			},
			{
				Role: "system",
				Body: "Type one line and press Enter to save a local scratch note on this machine. Use /help, /list, or /quit as needed. This is scratch history only, not a Tuku task.",
			},
			{
				Role: "system",
				Body: fmt.Sprintf("Current directory: %s", cwd),
			},
		},
	}
}

```


## internal/app/bootstrap_test.go

```go
package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/recoveryaction"
	"tuku/internal/ipc"
	tukushell "tuku/internal/tui/shell"
)

func TestParseShellWorkerPreference(t *testing.T) {
	preference, err := parseShellWorkerPreference("claude")
	if err != nil {
		t.Fatalf("parse claude worker preference: %v", err)
	}
	if preference != "claude" {
		t.Fatalf("expected claude preference, got %q", preference)
	}
}

func TestParseShellWorkerPreferenceRejectsInvalidWorker(t *testing.T) {
	if _, err := parseShellWorkerPreference("invalid-worker"); err == nil {
		t.Fatal("expected invalid worker error")
	}
}

func TestCLIUsageMentionsChat(t *testing.T) {
	if !strings.Contains(cliUsage(), "chat") {
		t.Fatalf("expected cli usage to mention chat, got %q", cliUsage())
	}
}

func TestCLIUsageMentionsRecovery(t *testing.T) {
	if !strings.Contains(cliUsage(), "recovery") {
		t.Fatalf("expected cli usage to mention recovery, got %q", cliUsage())
	}
}

func TestParseRecoveryActionKind(t *testing.T) {
	kind, err := parseRecoveryActionKind("decision-regenerate-brief")
	if err != nil {
		t.Fatalf("parse recovery action kind: %v", err)
	}
	if kind != "DECISION_REGENERATE_BRIEF" {
		t.Fatalf("expected DECISION_REGENERATE_BRIEF, got %s", kind)
	}
}

func TestParseHandoffFollowThroughKind(t *testing.T) {
	kind, err := parseHandoffFollowThroughKind("proof-of-life-observed")
	if err != nil {
		t.Fatalf("parse handoff follow-through kind: %v", err)
	}
	if kind != handoff.FollowThroughProofOfLifeObserved {
		t.Fatalf("expected %s, got %s", handoff.FollowThroughProofOfLifeObserved, kind)
	}
}

func TestCLIRecoveryRecordCommandRoutesRequest(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskRecordRecoveryActionResponse{
			TaskID:                common.TaskID("tsk_123"),
			Action:                ipc.TaskRecoveryActionRecord{ActionID: "ract_123", Kind: "FAILED_RUN_REVIEWED"},
			RecoveryClass:         "DECISION_REQUIRED",
			RecommendedAction:     "MAKE_RESUME_DECISION",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: false,
			RecoveryReason:        "failed run reviewed; choose next step",
			CanonicalResponse:     "recovery action recorded",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	defer stdout.restore()
	if err := app.Run(context.Background(), []string{
		"recovery", "record",
		"--task", "tsk_123",
		"--action", "failed-run-reviewed",
		"--summary", "reviewed failed run",
		"--note", "operator reviewed logs",
	}); err != nil {
		t.Fatalf("run recovery command: %v", err)
	}
	if captured.Method != ipc.MethodRecordRecoveryAction {
		t.Fatalf("expected recovery record method, got %s", captured.Method)
	}
	var req ipc.TaskRecordRecoveryActionRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal recovery record request: %v", err)
	}
	if req.TaskID != "tsk_123" || req.Kind != "FAILED_RUN_REVIEWED" {
		t.Fatalf("unexpected recovery record request: %+v", req)
	}
	if len(req.Notes) != 1 || req.Notes[0] != "operator reviewed logs" {
		t.Fatalf("unexpected recovery record notes: %+v", req.Notes)
	}
}

func TestCLIRecoveryRecordCommandRejectsUnsupportedAction(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	called := false
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		called = true
		return ipc.Response{}, nil
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"recovery", "record", "--task", "tsk_123", "--action", "not-a-real-action"})
	if err == nil || !strings.Contains(err.Error(), "unsupported recovery action") {
		t.Fatalf("expected unsupported recovery action error, got %v", err)
	}
	if called {
		t.Fatal("ipc should not be called for unsupported recovery action")
	}
}

func TestCLIRecoveryRecordCommandReturnsDaemonError(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, errors.New("daemon error [RECOVERY_ACTION_FAILED]: continue decision can only be recorded while recovery class is DECISION_REQUIRED")
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"recovery", "record", "--task", "tsk_123", "--action", "decision-continue"})
	if err == nil || !strings.Contains(err.Error(), "DECISION_REQUIRED") {
		t.Fatalf("expected daemon rejection to surface, got %v", err)
	}
}

func TestCLIRecoveryRebriefCommandRoutesRequest(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskRebriefResponse{
			TaskID:                common.TaskID("tsk_456"),
			PreviousBriefID:       common.BriefID("brf_old"),
			BriefID:               common.BriefID("brf_new"),
			BriefHash:             "hash_new",
			RecoveryClass:         "READY_NEXT_RUN",
			RecommendedAction:     "START_NEXT_RUN",
			ReadyForNextRun:       true,
			ReadyForHandoffLaunch: false,
			RecoveryReason:        "execution brief was regenerated after operator decision",
			CanonicalResponse:     "rebrief executed",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	defer stdout.restore()
	if err := app.Run(context.Background(), []string{"recovery", "rebrief", "--task", "tsk_456"}); err != nil {
		t.Fatalf("run recovery rebrief command: %v", err)
	}
	if captured.Method != ipc.MethodExecuteRebrief {
		t.Fatalf("expected rebrief method, got %s", captured.Method)
	}
	var req ipc.TaskRebriefRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal rebrief request: %v", err)
	}
	if req.TaskID != "tsk_456" {
		t.Fatalf("unexpected rebrief request: %+v", req)
	}
}

func TestCLIRecoveryRebriefCommandReturnsDaemonError(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, errors.New("daemon error [REBRIEF_FAILED]: rebrief can only be executed while recovery class is REBRIEF_REQUIRED")
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"recovery", "rebrief", "--task", "tsk_456"})
	if err == nil || !strings.Contains(err.Error(), "REBRIEF_REQUIRED") {
		t.Fatalf("expected daemon rebrief rejection to surface, got %v", err)
	}
}

func TestCLIRecoveryResumeInterruptedCommandRoutesRequest(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskInterruptedResumeResponse{
			TaskID:                common.TaskID("tsk_resume_interrupt"),
			BriefID:               common.BriefID("brf_current"),
			BriefHash:             "hash_current",
			Action:                ipc.TaskRecoveryActionRecord{ActionID: "ract_interrupt_resume_1", Kind: string(recoveryaction.KindInterruptedResumeExecuted), Summary: "operator resumed interrupted lineage"},
			RecoveryClass:         "CONTINUE_EXECUTION_REQUIRED",
			RecommendedAction:     "EXECUTE_CONTINUE_RECOVERY",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: false,
			RecoveryReason:        "operator explicitly resumed interrupted execution lineage; continue recovery still must be executed before the next bounded run",
			CanonicalResponse:     "interrupted resume executed",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	defer stdout.restore()
	if err := app.Run(context.Background(), []string{
		"recovery", "resume-interrupted",
		"--task", "tsk_resume_interrupt",
		"--summary", "operator resumed interrupted lineage",
		"--note", "maintain interrupted lineage semantics",
	}); err != nil {
		t.Fatalf("run recovery resume-interrupted command: %v", err)
	}
	if captured.Method != ipc.MethodExecuteInterruptedResume {
		t.Fatalf("expected interrupted-resume method, got %s", captured.Method)
	}
	var req ipc.TaskInterruptedResumeRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal interrupted resume request: %v", err)
	}
	if req.TaskID != "tsk_resume_interrupt" || req.Summary != "operator resumed interrupted lineage" {
		t.Fatalf("unexpected interrupted resume request: %+v", req)
	}
	if len(req.Notes) != 1 || req.Notes[0] != "maintain interrupted lineage semantics" {
		t.Fatalf("unexpected interrupted resume notes: %+v", req.Notes)
	}
}

func TestCLIRecoveryResumeInterruptedCommandReturnsDaemonError(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, errors.New("daemon error [INTERRUPTED_RESUME_FAILED]: interrupted resume can only be executed while recovery class is INTERRUPTED_RUN_RECOVERABLE and recommended action is RESUME_INTERRUPTED_RUN")
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"recovery", "resume-interrupted", "--task", "tsk_resume_interrupt"})
	if err == nil || !strings.Contains(err.Error(), "INTERRUPTED_RUN_RECOVERABLE") {
		t.Fatalf("expected daemon interrupted-resume rejection to surface, got %v", err)
	}
}

func TestCLIRecoveryContinueCommandRoutesRequest(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskContinueRecoveryResponse{
			TaskID:                common.TaskID("tsk_789"),
			BriefID:               common.BriefID("brf_current"),
			BriefHash:             "hash_current",
			Action:                ipc.TaskRecoveryActionRecord{ActionID: "ract_continue_1", TaskID: common.TaskID("tsk_789"), Kind: string(recoveryaction.KindContinueExecuted), Summary: "operator confirmed current brief"},
			RecoveryClass:         "READY_NEXT_RUN",
			RecommendedAction:     "START_NEXT_RUN",
			ReadyForNextRun:       true,
			ReadyForHandoffLaunch: false,
			RecoveryReason:        "operator explicitly confirmed the current brief for the next bounded run",
			CanonicalResponse:     "continue recovery executed",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	defer stdout.restore()
	if err := app.Run(context.Background(), []string{"recovery", "continue", "--task", "tsk_789"}); err != nil {
		t.Fatalf("run recovery continue command: %v", err)
	}
	if captured.Method != ipc.MethodExecuteContinueRecovery {
		t.Fatalf("expected continue-recovery method, got %s", captured.Method)
	}
	var req ipc.TaskContinueRecoveryRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal continue recovery request: %v", err)
	}
	if req.TaskID != "tsk_789" {
		t.Fatalf("unexpected continue recovery request: %+v", req)
	}
}

func TestCLIHandoffFollowThroughCommandRoutesRequest(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskHandoffFollowThroughRecordResponse{
			TaskID: common.TaskID("tsk_follow"),
			Record: &handoff.FollowThrough{
				Version:         1,
				RecordID:        "hft_1",
				HandoffID:       "hnd_1",
				LaunchAttemptID: "hlc_1",
				LaunchID:        "launch_1",
				TaskID:          common.TaskID("tsk_follow"),
				TargetWorker:    "claude",
				Kind:            handoff.FollowThroughProofOfLifeObserved,
				Summary:         "later Claude proof of life observed",
				CreatedAt:       time.Unix(1710000200, 0).UTC(),
			},
			RecoveryClass:         "HANDOFF_LAUNCH_COMPLETED",
			RecommendedAction:     "MONITOR_LAUNCHED_HANDOFF",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: false,
			RecoveryReason:        "launched handoff remains monitor-only",
			CanonicalResponse:     "follow-through recorded",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	defer stdout.restore()
	if err := app.Run(context.Background(), []string{
		"handoff-followthrough",
		"--task", "tsk_follow",
		"--kind", "proof-of-life-observed",
		"--summary", "later Claude proof of life observed",
		"--note", "operator confirmed downstream ping",
	}); err != nil {
		t.Fatalf("run handoff-followthrough command: %v", err)
	}
	if captured.Method != ipc.MethodRecordHandoffFollowThrough {
		t.Fatalf("expected handoff follow-through method, got %s", captured.Method)
	}
	var req ipc.TaskHandoffFollowThroughRecordRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal handoff follow-through request: %v", err)
	}
	if req.TaskID != "tsk_follow" || req.Kind != string(handoff.FollowThroughProofOfLifeObserved) {
		t.Fatalf("unexpected handoff follow-through request: %+v", req)
	}
	if len(req.Notes) != 1 || req.Notes[0] != "operator confirmed downstream ping" {
		t.Fatalf("unexpected handoff follow-through notes: %+v", req.Notes)
	}
}

func TestCLIHandoffFollowThroughCommandRejectsUnsupportedKind(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	called := false
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		called = true
		return ipc.Response{}, nil
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"handoff-followthrough", "--task", "tsk_follow", "--kind", "not-a-real-kind"})
	if err == nil || !strings.Contains(err.Error(), "unsupported handoff follow-through kind") {
		t.Fatalf("expected unsupported follow-through kind error, got %v", err)
	}
	if called {
		t.Fatal("ipc should not be called for unsupported handoff follow-through kind")
	}
}

func TestCLIHandoffFollowThroughCommandReturnsDaemonError(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, errors.New("daemon error [HANDOFF_FOLLOW_THROUGH_FAILED]: handoff follow-through kind PROOF_OF_LIFE_OBSERVED can only be recorded while handoff continuity state is a launched Claude follow-through posture")
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"handoff-followthrough", "--task", "tsk_follow", "--kind", "proof-of-life-observed"})
	if err == nil || !strings.Contains(err.Error(), "launched Claude follow-through posture") {
		t.Fatalf("expected daemon follow-through rejection to surface, got %v", err)
	}
}

func TestCLIRecoveryContinueCommandReturnsDaemonError(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, errors.New("daemon error [CONTINUE_RECOVERY_FAILED]: continue recovery can only be executed while recovery class is CONTINUE_EXECUTION_REQUIRED and latest action is DECISION_CONTINUE")
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"recovery", "continue", "--task", "tsk_789"})
	if err == nil || !strings.Contains(err.Error(), "CONTINUE_EXECUTION_REQUIRED") {
		t.Fatalf("expected daemon continue-recovery rejection to surface, got %v", err)
	}
}

func TestCLIRecoveryReviewInterruptedCommandRoutesRequest(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskRecordRecoveryActionResponse{
			TaskID:                common.TaskID("tsk_interrupt"),
			Action:                ipc.TaskRecoveryActionRecord{ActionID: "ract_interrupt_1", Kind: string(recoveryaction.KindInterruptedRunReviewed)},
			RecoveryClass:         "INTERRUPTED_RUN_RECOVERABLE",
			RecommendedAction:     "RESUME_INTERRUPTED_RUN",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: false,
			RecoveryReason:        "interrupted execution lineage was reviewed and remains recoverable",
			CanonicalResponse:     "interrupted run reviewed",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	defer stdout.restore()
	if err := app.Run(context.Background(), []string{
		"recovery", "review-interrupted",
		"--task", "tsk_interrupt",
		"--summary", "interrupted lineage reviewed",
		"--note", "preserve interrupted lineage",
	}); err != nil {
		t.Fatalf("run recovery review-interrupted command: %v", err)
	}
	if captured.Method != ipc.MethodReviewInterruptedRun {
		t.Fatalf("expected review-interrupted method, got %s", captured.Method)
	}
	var req ipc.TaskReviewInterruptedRunRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal review-interrupted request: %v", err)
	}
	if req.TaskID != "tsk_interrupt" || req.Summary != "interrupted lineage reviewed" {
		t.Fatalf("unexpected review-interrupted request: %+v", req)
	}
	if len(req.Notes) != 1 || req.Notes[0] != "preserve interrupted lineage" {
		t.Fatalf("unexpected review-interrupted notes: %+v", req.Notes)
	}
}

func TestCLIRecoveryReviewInterruptedCommandReturnsDaemonError(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, errors.New("daemon error [INTERRUPTED_REVIEW_FAILED]: interrupted-run review can only be recorded while recovery class is INTERRUPTED_RUN_RECOVERABLE")
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"recovery", "review-interrupted", "--task", "tsk_interrupt"})
	if err == nil || !strings.Contains(err.Error(), "INTERRUPTED_RUN_RECOVERABLE") {
		t.Fatalf("expected daemon interrupted-review rejection to surface, got %v", err)
	}
}

func TestResolveShellTaskForRepoWithDaemonBootstrapStartsDaemonOnUnavailable(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	origTimeout := daemonReadyTimeout
	origInterval := daemonRetryInterval
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
		daemonReadyTimeout = origTimeout
		daemonRetryInterval = origInterval
	}()

	var calls int
	var launched int
	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		calls++
		if calls == 1 || calls == 2 {
			return ipc.Response{}, daemonUnavailableErr()
		}
		return mustResolveShellTaskResponse(t, "tsk_bootstrap"), nil
	}
	startLocalDaemon = func() (<-chan error, error) {
		launched++
		ch := make(chan error)
		return ch, nil
	}
	daemonReadyTimeout = 50 * time.Millisecond
	daemonRetryInterval = 0

	resolution, err := resolveShellTaskForRepoWithDaemonBootstrap(context.Background(), "/tmp/tukud.sock", "/tmp/repo", "Continue work in this repository")
	if err != nil {
		t.Fatalf("resolve shell task with bootstrap: %v", err)
	}
	if resolution.TaskID != common.TaskID("tsk_bootstrap") {
		t.Fatalf("expected task id tsk_bootstrap, got %s", resolution.TaskID)
	}
	if launched != 1 {
		t.Fatalf("expected daemon to be launched once, got %d", launched)
	}
}

func TestResolveShellTaskForRepoWithDaemonBootstrapDoesNotStartDaemonOnUnexpectedError(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
	}()

	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, errors.New("daemon error [BAD_PAYLOAD]: broken request")
	}
	startLocalDaemon = func() (<-chan error, error) {
		t.Fatal("daemon should not be launched for unexpected IPC errors")
		return nil, nil
	}

	if _, err := resolveShellTaskForRepoWithDaemonBootstrap(context.Background(), "/tmp/tukud.sock", "/tmp/repo", "Continue work in this repository"); err == nil {
		t.Fatal("expected unexpected IPC error to be returned")
	}
}

func TestResolveShellTaskForRepoWithDaemonBootstrapReturnsStartupFailure(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
	}()

	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, daemonUnavailableErr()
	}
	startLocalDaemon = func() (<-chan error, error) {
		return nil, errors.New("launch failed")
	}

	_, err := resolveShellTaskForRepoWithDaemonBootstrap(context.Background(), "/tmp/tukud.sock", "/tmp/repo", "Continue work in this repository")
	if err == nil || !strings.Contains(err.Error(), "could not start the local Tuku daemon automatically") {
		t.Fatalf("expected daemon startup failure, got %v", err)
	}
}

func TestResolveShellTaskForRepoWithDaemonBootstrapReturnsProcessExit(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	origTimeout := daemonReadyTimeout
	origInterval := daemonRetryInterval
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
		daemonReadyTimeout = origTimeout
		daemonRetryInterval = origInterval
	}()

	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, daemonUnavailableErr()
	}
	startLocalDaemon = func() (<-chan error, error) {
		ch := make(chan error, 1)
		ch <- errors.New("exit status 1")
		close(ch)
		return ch, nil
	}
	daemonReadyTimeout = 50 * time.Millisecond
	daemonRetryInterval = 0

	_, err := resolveShellTaskForRepoWithDaemonBootstrap(context.Background(), "/tmp/tukud.sock", "/tmp/repo", "Continue work in this repository")
	if err == nil || !strings.Contains(err.Error(), "local Tuku daemon failed to start") {
		t.Fatalf("expected daemon process exit failure, got %v", err)
	}
}

func TestRunPrimaryEntryStartsDaemonAndOpensShell(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	origGetwd := getWorkingDir
	origResolveRepo := resolveRepoRootFromDir
	origTimeout := daemonReadyTimeout
	origInterval := daemonRetryInterval
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
		getWorkingDir = origGetwd
		resolveRepoRootFromDir = origResolveRepo
		daemonReadyTimeout = origTimeout
		daemonRetryInterval = origInterval
	}()

	getWorkingDir = func() (string, error) { return "/tmp/repo", nil }
	resolveRepoRootFromDir = func(_ context.Context, dir string) (string, error) { return dir, nil }
	daemonReadyTimeout = 50 * time.Millisecond
	daemonRetryInterval = 0

	var calls int
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		calls++
		if calls <= 2 {
			return ipc.Response{}, daemonUnavailableErr()
		}
		if req.Method != ipc.MethodResolveShellTaskForRepo {
			t.Fatalf("expected resolve shell task request, got %s", req.Method)
		}
		return mustResolveShellTaskResponse(t, "tsk_primary"), nil
	}
	startLocalDaemon = func() (<-chan error, error) {
		ch := make(chan error)
		return ch, nil
	}

	var openedTaskID string
	app := &CLIApplication{
		openShellFn: func(_ context.Context, _ string, taskID string, _ tukushell.WorkerPreference) error {
			openedTaskID = taskID
			return nil
		},
	}
	if err := app.runPrimaryEntry(context.Background(), "/tmp/tukud.sock", nil); err != nil {
		t.Fatalf("run primary entry: %v", err)
	}
	if openedTaskID != "tsk_primary" {
		t.Fatalf("expected shell to open task tsk_primary, got %q", openedTaskID)
	}
}

func TestRunPrimaryEntryOutsideRepoOpensFallbackShell(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	origGetwd := getWorkingDir
	origResolveRepo := resolveRepoRootFromDir
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
		getWorkingDir = origGetwd
		resolveRepoRootFromDir = origResolveRepo
	}()

	getWorkingDir = func() (string, error) { return "/tmp/no-repo", nil }
	resolveRepoRootFromDir = func(_ context.Context, _ string) (string, error) {
		return "", errors.New("git repo root not found")
	}
	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		t.Fatal("daemon IPC should not be used outside repo fallback mode")
		return ipc.Response{}, nil
	}
	startLocalDaemon = func() (<-chan error, error) {
		t.Fatal("daemon should not be auto-started outside repo fallback mode")
		return nil, nil
	}

	var fallbackCWD string
	app := &CLIApplication{
		openFallbackShellFn: func(_ context.Context, cwd string, _ tukushell.WorkerPreference) error {
			fallbackCWD = cwd
			return nil
		},
		openShellFn: func(_ context.Context, _ string, _ string, _ tukushell.WorkerPreference) error {
			t.Fatal("task-backed shell should not open outside repo fallback mode")
			return nil
		},
	}
	if err := app.runPrimaryEntry(context.Background(), "/tmp/tukud.sock", nil); err != nil {
		t.Fatalf("run primary entry outside repo: %v", err)
	}
	if fallbackCWD != "/tmp/no-repo" {
		t.Fatalf("expected fallback cwd /tmp/no-repo, got %q", fallbackCWD)
	}
}

func TestResolveCurrentRepoRootReturnsPrimaryEntryMessage(t *testing.T) {
	origGetwd := getWorkingDir
	origResolveRepo := resolveRepoRootFromDir
	defer func() {
		getWorkingDir = origGetwd
		resolveRepoRootFromDir = origResolveRepo
	}()

	getWorkingDir = func() (string, error) { return "/tmp/not-repo", nil }
	resolveRepoRootFromDir = func(_ context.Context, _ string) (string, error) {
		return "", errors.New("git repo root not found")
	}

	_, err := resolveCurrentRepoRoot(context.Background())
	if err == nil || !strings.Contains(err.Error(), "tuku needs a git repository for the primary entry path") {
		t.Fatalf("expected primary-entry repo error, got %v", err)
	}
}

func TestPrimaryEntryScratchSnapshotExplainsNoRepoMode(t *testing.T) {
	snapshot := primaryEntryScratchSnapshot("/tmp/no-repo")
	if snapshot.Status != "LOCAL_ONLY" || snapshot.Phase != "SCRATCH_INTAKE" {
		t.Fatalf("expected scratch intake snapshot, got %+v", snapshot)
	}
	if snapshot.Repo.RepoRoot != "" {
		t.Fatalf("expected no repo anchor in scratch mode, got %+v", snapshot.Repo)
	}
	if snapshot.IntentClass != "scratch" {
		t.Fatalf("expected scratch intent class, got %q", snapshot.IntentClass)
	}
	if !strings.Contains(snapshot.LatestCanonicalResponse, "local scratch and intake session") {
		t.Fatalf("expected scratch explanation, got %q", snapshot.LatestCanonicalResponse)
	}
	if !strings.Contains(snapshot.IntentSummary, "/tmp/no-repo") {
		t.Fatalf("expected cwd in scratch intent summary, got %q", snapshot.IntentSummary)
	}
	if len(snapshot.RecentConversation) < 3 {
		t.Fatal("expected scratch intake guidance conversation")
	}
}

func TestLoadPrimaryRepoScratchBridgeLoadsExactRepoScratchNotes(t *testing.T) {
	origResolveScratchPath := resolveScratchPath
	defer func() {
		resolveScratchPath = origResolveScratchPath
	}()

	path := filepath.Join(t.TempDir(), "scratch.json")
	resolveScratchPath = func(string) (string, error) {
		return path, nil
	}
	if err := os.WriteFile(path, []byte(`{
  "version": 1,
  "kind": "local_scratch_intake",
  "cwd": "/tmp/repo",
  "created_at": "2026-03-19T00:00:00Z",
  "updated_at": "2026-03-19T00:00:00Z",
  "notes": [
    {"role": "user", "body": "Draft the first milestone list", "created_at": "2026-03-19T00:00:00Z"}
  ]
}`), 0o644); err != nil {
		t.Fatalf("write scratch file: %v", err)
	}

	bridge, err := loadPrimaryRepoScratchBridge("/tmp/repo")
	if err != nil {
		t.Fatalf("load primary repo scratch bridge: %v", err)
	}
	if bridge == nil || len(bridge.Notes) != 1 {
		t.Fatalf("expected one bridged scratch note, got %+v", bridge)
	}
	if bridge.Notes[0].Body != "Draft the first milestone list" {
		t.Fatalf("expected bridged note body, got %+v", bridge.Notes[0])
	}
}

func TestApplyPrimaryRepoScratchBridgeAppendsExplicitLocalOnlyMessages(t *testing.T) {
	snapshot := applyPrimaryRepoScratchBridge(tukushell.Snapshot{
		TaskID:                  "tsk_repo",
		Phase:                   "INTAKE",
		Status:                  "ACTIVE",
		LatestCanonicalResponse: "Canonical repo-backed response.",
		RecentConversation: []tukushell.ConversationItem{
			{Role: "system", Body: "Repo-backed task created."},
		},
	}, &primaryRepoScratchBridge{
		RepoRoot: "/tmp/repo",
		Notes: []tukushell.ConversationItem{
			{Role: "user", Body: "Plan project structure"},
			{Role: "user", Body: "List initial requirements"},
		},
	})

	if snapshot.LatestCanonicalResponse != "Canonical repo-backed response." {
		t.Fatalf("expected canonical response to remain unchanged, got %q", snapshot.LatestCanonicalResponse)
	}
	if snapshot.LocalScratch == nil || len(snapshot.LocalScratch.Notes) != 2 {
		t.Fatalf("expected surfaced local scratch context, got %+v", snapshot.LocalScratch)
	}
	all := make([]string, 0, len(snapshot.RecentConversation))
	for _, msg := range snapshot.RecentConversation {
		all = append(all, msg.Body)
	}
	joined := strings.Join(all, "\n")
	if !strings.Contains(joined, "have not been imported into canonical task state") {
		t.Fatalf("expected explicit local-only boundary, got %q", joined)
	}
	if !strings.Contains(joined, "Sending that pending message is the explicit adoption step") {
		t.Fatalf("expected explicit adoption step, got %q", joined)
	}
	if !strings.Contains(joined, "Shell commands: stage local scratch with `a`") {
		t.Fatalf("expected shell-local adoption command copy, got %q", joined)
	}
	if !strings.Contains(joined, "local scratch note: Plan project structure") {
		t.Fatalf("expected bridged scratch note, got %q", joined)
	}
}

func mustResolveShellTaskResponse(t *testing.T, taskID common.TaskID) ipc.Response {
	t.Helper()
	payload, err := json.Marshal(ipc.ResolveShellTaskForRepoResponse{
		TaskID:   taskID,
		RepoRoot: "/tmp/repo",
		Created:  false,
	})
	if err != nil {
		t.Fatalf("marshal resolve shell task response: %v", err)
	}
	return ipc.Response{OK: true, Payload: payload}
}

func daemonUnavailableErr() error {
	return &net.OpError{Op: "dial", Net: "unix", Err: syscall.ECONNREFUSED}
}

type capturedStdout struct {
	previous *os.File
	reader   *os.File
	writer   *os.File
	buffer   bytes.Buffer
}

func captureCLIStdout(t *testing.T) *capturedStdout {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	captured := &capturedStdout{
		previous: os.Stdout,
		reader:   reader,
		writer:   writer,
	}
	os.Stdout = writer
	return captured
}

func (c *capturedStdout) restore() {
	if c == nil {
		return
	}
	if c.previous != nil {
		os.Stdout = c.previous
	}
	if c.writer != nil {
		_ = c.writer.Close()
	}
	if c.reader != nil {
		_, _ = c.buffer.ReadFrom(c.reader)
		_ = c.reader.Close()
	}
}

```


## internal/tui/shell/types.go

```go
package shell

import (
	"context"
	"time"
)

type Snapshot struct {
	TaskID                  string
	Goal                    string
	Phase                   string
	Status                  string
	Repo                    RepoAnchor
	LocalScratch            *LocalScratchContext
	IntentClass             string
	IntentSummary           string
	Brief                   *BriefSummary
	Run                     *RunSummary
	Checkpoint              *CheckpointSummary
	Handoff                 *HandoffSummary
	Launch                  *LaunchSummary
	LaunchControl           *LaunchControlSummary
	Acknowledgment          *AcknowledgmentSummary
	FollowThrough           *FollowThroughSummary
	HandoffContinuity       *HandoffContinuitySummary
	Recovery                *RecoverySummary
	RecentProofs            []ProofItem
	RecentConversation      []ConversationItem
	LatestCanonicalResponse string
}

type RepoAnchor struct {
	RepoRoot         string
	Branch           string
	HeadSHA          string
	WorkingTreeDirty bool
	CapturedAt       time.Time
}

type LocalScratchContext struct {
	RepoRoot string
	Notes    []ConversationItem
}

type BriefSummary struct {
	ID               string
	Objective        string
	NormalizedAction string
	Constraints      []string
	DoneCriteria     []string
}

type RunSummary struct {
	ID                 string
	WorkerKind         string
	Status             string
	LastKnownSummary   string
	StartedAt          time.Time
	EndedAt            *time.Time
	InterruptionReason string
}

type CheckpointSummary struct {
	ID               string
	Trigger          string
	CreatedAt        time.Time
	ResumeDescriptor string
	IsResumable      bool
}

type HandoffSummary struct {
	ID           string
	Status       string
	SourceWorker string
	TargetWorker string
	Mode         string
	Reason       string
	AcceptedBy   string
	CreatedAt    time.Time
}

type LaunchSummary struct {
	AttemptID         string
	LaunchID          string
	Status            string
	RequestedAt       time.Time
	StartedAt         time.Time
	EndedAt           time.Time
	Summary           string
	ErrorMessage      string
	OutputArtifactRef string
}

type LaunchControlSummary struct {
	State            string
	RetryDisposition string
	Reason           string
	HandoffID        string
	AttemptID        string
	LaunchID         string
	TargetWorker     string
	RequestedAt      time.Time
	CompletedAt      time.Time
	FailedAt         time.Time
}

type AcknowledgmentSummary struct {
	Status    string
	Summary   string
	CreatedAt time.Time
}

type FollowThroughSummary struct {
	RecordID        string
	Kind            string
	Summary         string
	LaunchAttemptID string
	LaunchID        string
	CreatedAt       time.Time
}

type HandoffContinuitySummary struct {
	State                        string
	Reason                       string
	LaunchAttemptID              string
	LaunchID                     string
	AcknowledgmentID             string
	AcknowledgmentStatus         string
	AcknowledgmentSummary        string
	FollowThroughID              string
	FollowThroughKind            string
	FollowThroughSummary         string
	DownstreamContinuationProven bool
}

type RecoveryIssue struct {
	Code    string
	Message string
}

type RecoverySummary struct {
	ContinuityOutcome      string
	Class                  string
	Action                 string
	ReadyForNextRun        bool
	ReadyForHandoffLaunch  bool
	RequiresDecision       bool
	RequiresRepair         bool
	RequiresReview         bool
	RequiresReconciliation bool
	DriftClass             string
	Reason                 string
	CheckpointID           string
	RunID                  string
	HandoffID              string
	HandoffStatus          string
	Issues                 []RecoveryIssue
}

type ProofItem struct {
	ID        string
	Type      string
	Summary   string
	Timestamp time.Time
}

type ConversationItem struct {
	Role      string
	Body      string
	CreatedAt time.Time
}

type HostMode string

const (
	HostModeCodexPTY   HostMode = "codex-pty"
	HostModeClaudePTY  HostMode = "claude-pty"
	HostModeTranscript HostMode = "transcript"
)

type HostState string

const (
	HostStateStarting       HostState = "starting"
	HostStateLive           HostState = "live"
	HostStateExited         HostState = "exited"
	HostStateFailed         HostState = "failed"
	HostStateFallback       HostState = "fallback"
	HostStateTranscriptOnly HostState = "transcript-only"
)

type HostStatus struct {
	Mode           HostMode
	State          HostState
	Label          string
	Note           string
	InputLive      bool
	ExitCode       *int
	Width          int
	Height         int
	LastOutputAt   time.Time
	StateChangedAt time.Time
}

type SessionEventType string

const (
	SessionEventShellStarted               SessionEventType = "shell_started"
	SessionEventHostStartupAttempted       SessionEventType = "host_startup_attempted"
	SessionEventHostLive                   SessionEventType = "host_live"
	SessionEventResizeApplied              SessionEventType = "resize_applied"
	SessionEventHostExited                 SessionEventType = "host_exited"
	SessionEventHostFailed                 SessionEventType = "host_failed"
	SessionEventFallbackActivated          SessionEventType = "fallback_activated"
	SessionEventManualRefresh              SessionEventType = "manual_refresh"
	SessionEventPendingMessageStaged       SessionEventType = "pending_message_staged"
	SessionEventPendingMessageEditStarted  SessionEventType = "pending_message_edit_started"
	SessionEventPendingMessageEditSaved    SessionEventType = "pending_message_edit_saved"
	SessionEventPendingMessageEditCanceled SessionEventType = "pending_message_edit_canceled"
	SessionEventPendingMessageSent         SessionEventType = "pending_message_sent"
	SessionEventPendingMessageCleared      SessionEventType = "pending_message_cleared"
	SessionEventPriorPersistedProof        SessionEventType = "prior_persisted_proof"
)

type SessionEvent struct {
	Type      SessionEventType
	Summary   string
	CreatedAt time.Time
}

type SessionState struct {
	SessionID             string
	StartedAt             time.Time
	WorkerPreference      WorkerPreference
	ResolvedWorker        WorkerPreference
	WorkerSessionID       string
	AttachCapability      WorkerAttachCapability
	Journal               []SessionEvent
	KnownSessions         []KnownShellSession
	PriorPersistedSummary string
}

type WorkerAttachCapability string

const (
	WorkerAttachCapabilityNone       WorkerAttachCapability = "none"
	WorkerAttachCapabilityAttachable WorkerAttachCapability = "attachable"
)

type KnownShellSessionClass string

const (
	KnownShellSessionClassAttachable         KnownShellSessionClass = "attachable"
	KnownShellSessionClassActiveUnattachable KnownShellSessionClass = "active_unattachable"
	KnownShellSessionClassStale              KnownShellSessionClass = "stale"
	KnownShellSessionClassEnded              KnownShellSessionClass = "ended"
)

type KnownShellSession struct {
	SessionID        string
	TaskID           string
	WorkerPreference WorkerPreference
	ResolvedWorker   WorkerPreference
	WorkerSessionID  string
	AttachCapability WorkerAttachCapability
	HostMode         HostMode
	HostState        HostState
	SessionClass     KnownShellSessionClass
	StartedAt        time.Time
	LastUpdatedAt    time.Time
	Active           bool
	Note             string
}

type FocusPane int

const (
	FocusWorker FocusPane = iota
	FocusInspector
	FocusActivity
)

type UIState struct {
	ShowInspector                  bool
	ShowProof                      bool
	ShowHelp                       bool
	ShowStatus                     bool
	Focus                          FocusPane
	EscapePrefix                   bool
	PendingTaskMessage             string
	PendingTaskMessageSource       string
	PendingTaskMessageEditMode     bool
	PendingTaskMessageEditBuffer   string
	PendingTaskMessageEditOriginal string
	Session                        SessionState
	LastRefresh                    time.Time
	ObservedAt                     time.Time
	LastError                      string
}

type ViewModel struct {
	Header     HeaderView
	WorkerPane PaneView
	Inspector  *InspectorView
	ProofStrip *StripView
	Footer     string
	Overlay    *OverlayView
	Layout     shellLayout
}

type HeaderView struct {
	Title      string
	TaskLabel  string
	Phase      string
	Worker     string
	Repo       string
	Continuity string
}

type PaneView struct {
	Title   string
	Lines   []string
	Focused bool
}

type InspectorView struct {
	Title    string
	Sections []SectionView
	Focused  bool
}

type SectionView struct {
	Title string
	Lines []string
}

type StripView struct {
	Title   string
	Lines   []string
	Focused bool
}

type OverlayView struct {
	Title string
	Lines []string
}

type SnapshotSource interface {
	Load(taskID string) (Snapshot, error)
}

type WorkerHost interface {
	Start(ctx context.Context, snapshot Snapshot) error
	Stop() error
	UpdateSnapshot(snapshot Snapshot)
	Resize(width int, height int) bool
	CanAcceptInput() bool
	WriteInput(data []byte) bool
	Status() HostStatus
	Title() string
	WorkerLabel() string
	Lines(height int, width int) []string
	ActivityLines(limit int) []string
}

func (s Snapshot) RunWorkerKind() string {
	if s.Run == nil {
		return ""
	}
	return s.Run.WorkerKind
}

func (s Snapshot) HandoffTargetWorker() string {
	if s.Handoff == nil {
		return ""
	}
	return s.Handoff.TargetWorker
}

func (s Snapshot) HasLocalScratchAdoption() bool {
	return s.LocalScratch != nil && len(s.LocalScratch.Notes) > 0
}

```


## internal/tui/shell/adapter.go

```go
package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/ipc"
)

type IPCSnapshotSource struct {
	SocketPath string
	Timeout    time.Duration
}

func NewIPCSnapshotSource(socketPath string) *IPCSnapshotSource {
	return &IPCSnapshotSource{
		SocketPath: socketPath,
		Timeout:    5 * time.Second,
	}
}

func (s *IPCSnapshotSource) Load(taskID string) (Snapshot, error) {
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	payload, err := json.Marshal(ipc.TaskShellSnapshotRequest{TaskID: ipcTaskID(taskID)})
	if err != nil {
		return Snapshot{}, err
	}
	resp, err := ipc.CallUnix(ctx, s.SocketPath, ipc.Request{
		RequestID: fmt.Sprintf("shell_%d", time.Now().UTC().UnixNano()),
		Method:    ipc.MethodTaskShellSnapshot,
		Payload:   payload,
	})
	if err != nil {
		return Snapshot{}, err
	}
	var raw ipc.TaskShellSnapshotResponse
	if err := json.Unmarshal(resp.Payload, &raw); err != nil {
		return Snapshot{}, err
	}
	return snapshotFromIPC(raw), nil
}

func snapshotFromIPC(raw ipc.TaskShellSnapshotResponse) Snapshot {
	out := Snapshot{
		TaskID:        string(raw.TaskID),
		Goal:          raw.Goal,
		Phase:         raw.Phase,
		Status:        raw.Status,
		IntentClass:   raw.IntentClass,
		IntentSummary: raw.IntentSummary,
		Repo: RepoAnchor{
			RepoRoot:         raw.RepoAnchor.RepoRoot,
			Branch:           raw.RepoAnchor.Branch,
			HeadSHA:          raw.RepoAnchor.HeadSHA,
			WorkingTreeDirty: raw.RepoAnchor.WorkingTreeDirty,
			CapturedAt:       raw.RepoAnchor.CapturedAt,
		},
		LatestCanonicalResponse: raw.LatestCanonicalResponse,
	}
	if raw.Brief != nil {
		out.Brief = &BriefSummary{
			ID:               string(raw.Brief.BriefID),
			Objective:        raw.Brief.Objective,
			NormalizedAction: raw.Brief.NormalizedAction,
			Constraints:      append([]string{}, raw.Brief.Constraints...),
			DoneCriteria:     append([]string{}, raw.Brief.DoneCriteria...),
		}
	}
	if raw.Run != nil {
		out.Run = &RunSummary{
			ID:                 string(raw.Run.RunID),
			WorkerKind:         string(raw.Run.WorkerKind),
			Status:             string(raw.Run.Status),
			LastKnownSummary:   raw.Run.LastKnownSummary,
			StartedAt:          raw.Run.StartedAt,
			EndedAt:            raw.Run.EndedAt,
			InterruptionReason: raw.Run.InterruptionReason,
		}
	}
	if raw.Checkpoint != nil {
		out.Checkpoint = &CheckpointSummary{
			ID:               string(raw.Checkpoint.CheckpointID),
			Trigger:          string(raw.Checkpoint.Trigger),
			CreatedAt:        raw.Checkpoint.CreatedAt,
			ResumeDescriptor: raw.Checkpoint.ResumeDescriptor,
			IsResumable:      raw.Checkpoint.IsResumable,
		}
	}
	if raw.Handoff != nil {
		out.Handoff = &HandoffSummary{
			ID:           raw.Handoff.HandoffID,
			Status:       raw.Handoff.Status,
			SourceWorker: string(raw.Handoff.SourceWorker),
			TargetWorker: string(raw.Handoff.TargetWorker),
			Mode:         raw.Handoff.Mode,
			Reason:       raw.Handoff.Reason,
			AcceptedBy:   string(raw.Handoff.AcceptedBy),
			CreatedAt:    raw.Handoff.CreatedAt,
		}
	}
	if raw.Launch != nil {
		out.Launch = &LaunchSummary{
			AttemptID:         raw.Launch.AttemptID,
			LaunchID:          raw.Launch.LaunchID,
			Status:            raw.Launch.Status,
			RequestedAt:       raw.Launch.RequestedAt,
			StartedAt:         raw.Launch.StartedAt,
			EndedAt:           raw.Launch.EndedAt,
			Summary:           raw.Launch.Summary,
			ErrorMessage:      raw.Launch.ErrorMessage,
			OutputArtifactRef: raw.Launch.OutputArtifactRef,
		}
	}
	if raw.LaunchControl != nil {
		out.LaunchControl = &LaunchControlSummary{
			State:            raw.LaunchControl.State,
			RetryDisposition: raw.LaunchControl.RetryDisposition,
			Reason:           raw.LaunchControl.Reason,
			HandoffID:        raw.LaunchControl.HandoffID,
			AttemptID:        raw.LaunchControl.AttemptID,
			LaunchID:         raw.LaunchControl.LaunchID,
			TargetWorker:     string(raw.LaunchControl.TargetWorker),
			RequestedAt:      raw.LaunchControl.RequestedAt,
			CompletedAt:      raw.LaunchControl.CompletedAt,
			FailedAt:         raw.LaunchControl.FailedAt,
		}
	}
	if raw.Acknowledgment != nil {
		out.Acknowledgment = &AcknowledgmentSummary{
			Status:    raw.Acknowledgment.Status,
			Summary:   raw.Acknowledgment.Summary,
			CreatedAt: raw.Acknowledgment.CreatedAt,
		}
	}
	if raw.FollowThrough != nil {
		out.FollowThrough = &FollowThroughSummary{
			RecordID:        raw.FollowThrough.RecordID,
			Kind:            raw.FollowThrough.Kind,
			Summary:         raw.FollowThrough.Summary,
			LaunchAttemptID: raw.FollowThrough.LaunchAttemptID,
			LaunchID:        raw.FollowThrough.LaunchID,
			CreatedAt:       raw.FollowThrough.CreatedAt,
		}
	}
	if raw.HandoffContinuity != nil {
		out.HandoffContinuity = &HandoffContinuitySummary{
			State:                        raw.HandoffContinuity.State,
			Reason:                       raw.HandoffContinuity.Reason,
			LaunchAttemptID:              raw.HandoffContinuity.LaunchAttemptID,
			LaunchID:                     raw.HandoffContinuity.LaunchID,
			AcknowledgmentID:             raw.HandoffContinuity.AcknowledgmentID,
			AcknowledgmentStatus:         raw.HandoffContinuity.AcknowledgmentStatus,
			AcknowledgmentSummary:        raw.HandoffContinuity.AcknowledgmentSummary,
			FollowThroughID:              raw.HandoffContinuity.FollowThroughID,
			FollowThroughKind:            raw.HandoffContinuity.FollowThroughKind,
			FollowThroughSummary:         raw.HandoffContinuity.FollowThroughSummary,
			DownstreamContinuationProven: raw.HandoffContinuity.DownstreamContinuationProven,
		}
	}
	if raw.Recovery != nil {
		out.Recovery = &RecoverySummary{
			ContinuityOutcome:      raw.Recovery.ContinuityOutcome,
			Class:                  raw.Recovery.RecoveryClass,
			Action:                 raw.Recovery.RecommendedAction,
			ReadyForNextRun:        raw.Recovery.ReadyForNextRun,
			ReadyForHandoffLaunch:  raw.Recovery.ReadyForHandoffLaunch,
			RequiresDecision:       raw.Recovery.RequiresDecision,
			RequiresRepair:         raw.Recovery.RequiresRepair,
			RequiresReview:         raw.Recovery.RequiresReview,
			RequiresReconciliation: raw.Recovery.RequiresReconciliation,
			DriftClass:             string(raw.Recovery.DriftClass),
			Reason:                 raw.Recovery.Reason,
			CheckpointID:           string(raw.Recovery.CheckpointID),
			RunID:                  string(raw.Recovery.RunID),
			HandoffID:              raw.Recovery.HandoffID,
			HandoffStatus:          raw.Recovery.HandoffStatus,
		}
		if len(raw.Recovery.Issues) > 0 {
			out.Recovery.Issues = make([]RecoveryIssue, 0, len(raw.Recovery.Issues))
			for _, issue := range raw.Recovery.Issues {
				out.Recovery.Issues = append(out.Recovery.Issues, RecoveryIssue{
					Code:    issue.Code,
					Message: issue.Message,
				})
			}
		}
	}
	if len(raw.RecentProofs) > 0 {
		out.RecentProofs = make([]ProofItem, 0, len(raw.RecentProofs))
		for _, evt := range raw.RecentProofs {
			out.RecentProofs = append(out.RecentProofs, ProofItem{
				ID:        string(evt.EventID),
				Type:      evt.Type,
				Summary:   evt.Summary,
				Timestamp: evt.Timestamp,
			})
		}
	}
	if len(raw.RecentConversation) > 0 {
		out.RecentConversation = make([]ConversationItem, 0, len(raw.RecentConversation))
		for _, msg := range raw.RecentConversation {
			out.RecentConversation = append(out.RecentConversation, ConversationItem{
				Role:      msg.Role,
				Body:      msg.Body,
				CreatedAt: msg.CreatedAt,
			})
		}
	}
	return out
}

func ipcTaskID(taskID string) common.TaskID {
	return common.TaskID(taskID)
}

```


## internal/tui/shell/viewmodel.go

```go
package shell

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func BuildViewModel(snapshot Snapshot, ui UIState, host WorkerHost, width int, height int) ViewModel {
	if width <= 0 {
		width = 120
	}
	if height <= 0 {
		height = 32
	}
	if host == nil {
		host = NewTranscriptHost()
		host.UpdateSnapshot(snapshot)
	}

	header := HeaderView{
		Title:      "Tuku",
		TaskLabel:  displayTaskLabel(snapshot.TaskID),
		Phase:      nonEmpty(snapshot.Phase, "UNKNOWN"),
		Worker:     effectiveWorkerLabel(snapshot, host),
		Repo:       repoLabel(snapshot.Repo),
		Continuity: continuityLabel(snapshot),
	}

	layout := computeShellLayout(width, height, ui)
	bodyHeight := layout.bodyHeight
	workerWidth := layout.workerWidth
	inspectorWidth := layout.inspectorWidth
	if !layout.showInspector && ui.Focus == FocusInspector {
		ui.Focus = FocusWorker
	}
	if !layout.showProof && ui.Focus == FocusActivity {
		ui.Focus = FocusWorker
	}

	workerPane := buildWorkerPane(snapshot, ui, host, bodyHeight-1, workerWidth)

	var inspector *InspectorView
	if layout.showInspector && inspectorWidth > 0 {
		inspector = &InspectorView{
			Title:   "inspector",
			Focused: ui.Focus == FocusInspector,
			Sections: []SectionView{
				{Title: "operator", Lines: inspectorOperator(snapshot)},
				{Title: "worker session", Lines: inspectorWorkerSession(host, ui.Session)},
				{Title: "brief", Lines: inspectorBrief(snapshot)},
				{Title: "intent", Lines: inspectorIntent(snapshot)},
				{Title: "pending message", Lines: inspectorPendingMessage(snapshot, ui)},
				{Title: "checkpoint", Lines: inspectorCheckpoint(snapshot)},
				{Title: "handoff", Lines: inspectorHandoff(snapshot)},
				{Title: "launch", Lines: inspectorLaunch(snapshot)},
				{Title: "run", Lines: inspectorRun(snapshot)},
				{Title: "proof", Lines: inspectorProof(snapshot)},
			},
		}
	}

	var strip *StripView
	if layout.showProof {
		strip = &StripView{
			Title:   "activity",
			Focused: ui.Focus == FocusActivity,
			Lines:   buildActivityLines(snapshot, host, ui.Session),
		}
	}

	vm := ViewModel{
		Header:     header,
		WorkerPane: workerPane,
		Inspector:  inspector,
		ProofStrip: strip,
		Footer:     footerText(snapshot, ui, host),
		Layout:     layout,
	}

	if ui.ShowHelp {
		vm.Overlay = &OverlayView{
			Title: "help",
			Lines: []string{
				"q quit shell",
				"i toggle inspector",
				"p toggle activity strip",
				"r refresh shell state",
				"s toggle compact status card",
				"h toggle help",
				"tab cycle focus",
				"a stage a local draft from surfaced scratch",
				"e edit the staged local draft",
				"m send the current draft through Tuku",
				"x clear the local draft",
				"while editing: type in the worker pane",
				"ctrl-g s save and leave edit mode",
				"ctrl-g c cancel edits and restore the staged draft",
				"ctrl-g next-key when the live worker pane is focused",
				"",
				"Scratch stays local-only. The staged draft stays shell-local until you explicitly send it with m.",
			},
		}
	} else if ui.ShowStatus {
		vm.Overlay = &OverlayView{
			Title: "status",
			Lines: []string{
				fmt.Sprintf("task %s", displayTaskLabel(snapshot.TaskID)),
				fmt.Sprintf("new shell session %s", ui.Session.SessionID),
				fmt.Sprintf("phase %s", nonEmpty(snapshot.Phase, "UNKNOWN")),
				fmt.Sprintf("worker %s", effectiveWorkerLabel(snapshot, host)),
				fmt.Sprintf("host %s", hostStatusLine(snapshot, ui, host)),
				fmt.Sprintf("repo %s", repoLabel(snapshot.Repo)),
				fmt.Sprintf("continuity %s", continuityLabel(snapshot)),
				fmt.Sprintf("recovery %s", operatorStateLabel(snapshot)),
				fmt.Sprintf("next %s", operatorActionLabel(snapshot)),
				fmt.Sprintf("readiness %s", operatorReadinessLine(snapshot)),
				fmt.Sprintf("launch %s", launchControlLine(snapshot)),
				fmt.Sprintf("reason %s", strongestOperatorReason(snapshot)),
				fmt.Sprintf("registry %s", sessionRegistrySummary(ui.Session)),
				fmt.Sprintf("draft %s", pendingMessageSummary(snapshot, ui)),
				fmt.Sprintf("checkpoint %s", checkpointLine(snapshot)),
				fmt.Sprintf("handoff %s", handoffLine(snapshot)),
				sessionPriorLine(ui.Session),
				"",
				latestCanonicalLine(snapshot),
			},
		}
	}

	return vm
}

func buildWorkerPane(snapshot Snapshot, ui UIState, host WorkerHost, height int, width int) PaneView {
	if ui.PendingTaskMessageEditMode {
		return PaneView{
			Title:   "worker pane | pending message editor",
			Lines:   pendingTaskMessageEditorLines(ui, height, width),
			Focused: ui.Focus == FocusWorker,
		}
	}
	hostHeight := height
	lines := []string(nil)
	if summary := workerPaneSummaryLine(snapshot, ui, host); summary != "" && height >= 5 {
		hostHeight = max(1, height-1)
		lines = append(lines, summary)
	}
	lines = append(lines, host.Lines(hostHeight, width)...)
	return PaneView{
		Title:   host.Title(),
		Lines:   lines,
		Focused: ui.Focus == FocusWorker,
	}
}

func shortTaskID(taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if len(taskID) <= 10 {
		return taskID
	}
	return taskID[:10]
}

func displayTaskLabel(taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "no-task"
	}
	return shortTaskID(taskID)
}

func workerLabel(snapshot Snapshot) string {
	return snapshotWorkerLabel(snapshot)
}

func effectiveWorkerLabel(snapshot Snapshot, host WorkerHost) string {
	if isScratchIntakeSnapshot(snapshot) {
		return snapshotWorkerLabel(snapshot)
	}
	if host != nil {
		if label := strings.TrimSpace(host.WorkerLabel()); label != "" {
			return label
		}
		status := host.Status()
		if label := strings.TrimSpace(status.Label); label != "" {
			return label
		}
	}
	return snapshotWorkerLabel(snapshot)
}

func snapshotWorkerLabel(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "scratch intake"
	}
	if snapshot.Run != nil {
		if snapshot.Run.Status == "RUNNING" {
			return fmt.Sprintf("%s active", nonEmpty(snapshot.Run.WorkerKind, "worker"))
		}
		return fmt.Sprintf("%s last", nonEmpty(snapshot.Run.WorkerKind, "worker"))
	}
	if snapshot.Handoff != nil && snapshot.Handoff.TargetWorker != "" {
		return fmt.Sprintf("%s handoff", snapshot.Handoff.TargetWorker)
	}
	return "none"
}

func repoLabel(anchor RepoAnchor) string {
	if strings.TrimSpace(anchor.RepoRoot) == "" {
		return "no-repo"
	}
	name := filepath.Base(anchor.RepoRoot)
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = anchor.RepoRoot
	}
	branch := nonEmpty(anchor.Branch, "detached")
	dirty := ""
	if anchor.WorkingTreeDirty {
		dirty = " dirty"
	}
	return fmt.Sprintf("%s@%s%s", name, branch, dirty)
}

func continuityLabel(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "local-only"
	}
	if snapshot.Recovery != nil {
		switch snapshot.Recovery.Class {
		case "READY_NEXT_RUN":
			if snapshot.Recovery.ReadyForNextRun {
				return "ready"
			}
		case "CONTINUE_EXECUTION_REQUIRED":
			return "continue-pending"
		case "INTERRUPTED_RUN_RECOVERABLE":
			return "recoverable"
		case "ACCEPTED_HANDOFF_LAUNCH_READY":
			if snapshot.LaunchControl != nil && snapshot.LaunchControl.State == "FAILED" && snapshot.LaunchControl.RetryDisposition == "ALLOWED" {
				return "launch-retry"
			}
			return "handoff-ready"
		case "HANDOFF_LAUNCH_PENDING_OUTCOME":
			return "launch-pending"
		case "HANDOFF_LAUNCH_COMPLETED":
			return "launched"
		case "HANDOFF_FOLLOW_THROUGH_REVIEW_REQUIRED":
			return "review"
		case "FAILED_RUN_REVIEW_REQUIRED", "VALIDATION_REVIEW_REQUIRED":
			return "review"
		case "DECISION_REQUIRED", "BLOCKED_DRIFT":
			return "decision"
		case "REBRIEF_REQUIRED":
			return "rebrief"
		case "REPAIR_REQUIRED":
			return "repair"
		case "COMPLETED_NO_ACTION":
			return "complete"
		}
	}
	if snapshot.Checkpoint != nil && snapshot.Checkpoint.IsResumable {
		return "resumable"
	}
	switch snapshot.Phase {
	case "BLOCKED", "FAILED":
		return "blocked"
	case "VALIDATING":
		return "validating"
	default:
		return strings.ToLower(nonEmpty(snapshot.Status, "active"))
	}
}

func inspectorBrief(snapshot Snapshot) []string {
	if isScratchIntakeSnapshot(snapshot) {
		return []string{
			"No repo-backed brief exists in scratch intake mode.",
			"Use this session to frame the project, scope milestones, and prepare for repository setup.",
		}
	}
	if snapshot.Brief == nil {
		return []string{"No brief persisted yet."}
	}
	lines := []string{
		truncateWithEllipsis(snapshot.Brief.Objective, 48),
		fmt.Sprintf("action %s", nonEmpty(snapshot.Brief.NormalizedAction, "n/a")),
	}
	if len(snapshot.Brief.Constraints) > 0 {
		lines = append(lines, fmt.Sprintf("constraints %s", strings.Join(snapshot.Brief.Constraints, ", ")))
	}
	if len(snapshot.Brief.DoneCriteria) > 0 {
		lines = append(lines, fmt.Sprintf("done %s", strings.Join(snapshot.Brief.DoneCriteria, ", ")))
	}
	return lines
}

func inspectorIntent(snapshot Snapshot) []string {
	if isScratchIntakeSnapshot(snapshot) {
		return []string{
			"Local scratch intake session.",
			"Plan the work here before cloning or initializing a repository.",
		}
	}
	if snapshot.IntentSummary == "" {
		return []string{"No intent summary."}
	}
	return []string{snapshot.IntentSummary}
}

func inspectorCheckpoint(snapshot Snapshot) []string {
	if isScratchIntakeSnapshot(snapshot) {
		return []string{"No checkpoint exists because this session is not repo-backed."}
	}
	if snapshot.Checkpoint == nil {
		return []string{"No checkpoint yet."}
	}
	lines := []string{
		fmt.Sprintf("%s | %s", shortTaskID(snapshot.Checkpoint.ID), strings.ToLower(snapshot.Checkpoint.Trigger)),
	}
	lines = append(lines, fmt.Sprintf("raw resumable %s", yesNo(snapshot.Checkpoint.IsResumable)))
	if snapshot.Checkpoint.ResumeDescriptor != "" {
		lines = append(lines, snapshot.Checkpoint.ResumeDescriptor)
	}
	return lines
}

func inspectorHandoff(snapshot Snapshot) []string {
	if isScratchIntakeSnapshot(snapshot) {
		return []string{"No handoff packet exists in local scratch intake mode."}
	}
	if snapshot.Handoff == nil {
		return []string{"No handoff packet."}
	}
	lines := []string{
		fmt.Sprintf("%s -> %s (%s)", nonEmpty(snapshot.Handoff.SourceWorker, "unknown"), nonEmpty(snapshot.Handoff.TargetWorker, "unknown"), nonEmpty(snapshot.Handoff.Status, "unknown")),
	}
	if snapshot.Handoff.Mode != "" {
		lines = append(lines, fmt.Sprintf("mode %s", snapshot.Handoff.Mode))
	}
	if snapshot.Handoff.Reason != "" {
		lines = append(lines, snapshot.Handoff.Reason)
	}
	if continuity := handoffContinuityLine(snapshot); continuity != "n/a" {
		lines = append(lines, "continuity "+continuity)
	}
	if snapshot.Acknowledgment != nil {
		lines = append(lines, fmt.Sprintf("ack %s", strings.ToLower(snapshot.Acknowledgment.Status)))
		lines = append(lines, truncateWithEllipsis(snapshot.Acknowledgment.Summary, 48))
	}
	if snapshot.FollowThrough != nil {
		lines = append(lines, fmt.Sprintf("follow-through %s", strings.ToLower(strings.ReplaceAll(snapshot.FollowThrough.Kind, "_", "-"))))
		lines = append(lines, truncateWithEllipsis(snapshot.FollowThrough.Summary, 48))
	}
	if snapshot.LaunchControl != nil && snapshot.LaunchControl.State != "NOT_APPLICABLE" {
		lines = append(lines, "launch "+launchControlLine(snapshot))
	}
	if continuity := handoffContinuityLine(snapshot); continuity != "n/a" {
		lines = append(lines, "continuity "+continuity)
	}
	return lines
}

func inspectorLaunch(snapshot Snapshot) []string {
	if isScratchIntakeSnapshot(snapshot) {
		return []string{"No launch state exists in local scratch intake mode."}
	}
	if snapshot.Launch == nil && (snapshot.LaunchControl == nil || snapshot.LaunchControl.State == "NOT_APPLICABLE") {
		return []string{"No launch state."}
	}
	lines := []string{launchControlLine(snapshot)}
	if snapshot.Launch != nil {
		lines = append(lines, fmt.Sprintf("attempt %s | %s", shortTaskID(snapshot.Launch.AttemptID), strings.ToLower(nonEmpty(snapshot.Launch.Status, "unknown"))))
		if snapshot.Launch.LaunchID != "" {
			lines = append(lines, "launch id "+snapshot.Launch.LaunchID)
		}
		if snapshot.Launch.Summary != "" {
			lines = append(lines, truncateWithEllipsis(snapshot.Launch.Summary, 48))
		}
		if snapshot.Launch.ErrorMessage != "" {
			lines = append(lines, truncateWithEllipsis("error "+snapshot.Launch.ErrorMessage, 48))
		}
	}
	if snapshot.LaunchControl != nil && snapshot.LaunchControl.State == "COMPLETED" {
		lines = append(lines, "launcher invocation completed; downstream work not proven")
	}
	if continuity := handoffContinuityLine(snapshot); continuity != "n/a" {
		lines = append(lines, continuity)
	}
	if snapshot.HandoffContinuity != nil && snapshot.HandoffContinuity.State == "LAUNCH_COMPLETED_ACK_UNAVAILABLE" {
		lines = append(lines, "no usable acknowledgment captured; downstream work not proven")
	}
	return lines
}

func inspectorRun(snapshot Snapshot) []string {
	if isScratchIntakeSnapshot(snapshot) {
		return []string{"No execution run exists because this session has no task-backed orchestration state."}
	}
	if snapshot.Run == nil {
		return []string{"No run recorded."}
	}
	lines := []string{
		fmt.Sprintf("%s | %s", nonEmpty(snapshot.Run.WorkerKind, "worker"), snapshot.Run.Status),
	}
	if snapshot.Run.LastKnownSummary != "" {
		lines = append(lines, truncateWithEllipsis(snapshot.Run.LastKnownSummary, 48))
	}
	if snapshot.Run.InterruptionReason != "" {
		lines = append(lines, fmt.Sprintf("interrupt %s", snapshot.Run.InterruptionReason))
	}
	return lines
}

func inspectorWorkerSession(host WorkerHost, session SessionState) []string {
	if host == nil {
		return []string{"No worker host."}
	}
	status := host.Status()
	lines := []string{
		fmt.Sprintf("new shell session %s", session.SessionID),
		sessionRegistrySummary(session),
		fmt.Sprintf("preferred %s", nonEmpty(string(session.WorkerPreference), "auto")),
		fmt.Sprintf("resolved %s", nonEmpty(string(session.ResolvedWorker), "unknown")),
		fmt.Sprintf("worker session %s", nonEmpty(session.WorkerSessionID, "none")),
		fmt.Sprintf("attach %s", nonEmpty(string(session.AttachCapability), "none")),
		fmt.Sprintf("mode %s", nonEmpty(string(status.Mode), "unknown")),
		fmt.Sprintf("state %s", nonEmpty(string(status.State), "unknown")),
	}
	if !session.StartedAt.IsZero() {
		lines = append(lines, fmt.Sprintf("started %s", session.StartedAt.Format("15:04:05")))
	}
	if status.InputLive {
		lines = append(lines, "input live")
	} else {
		lines = append(lines, "input disabled")
	}
	if status.Width > 0 && status.Height > 0 {
		lines = append(lines, fmt.Sprintf("pane %dx%d", status.Width, status.Height))
	}
	if status.ExitCode != nil {
		lines = append(lines, fmt.Sprintf("exit code %d", *status.ExitCode))
	}
	if note := strings.TrimSpace(status.Note); note != "" {
		lines = append(lines, truncateWithEllipsis(note, 64))
	}
	if session.PriorPersistedSummary != "" {
		lines = append(lines, truncateWithEllipsis("previous persisted shell outcome "+session.PriorPersistedSummary, 64))
	}
	for _, evt := range recentSessionEvents(session, 2) {
		lines = append(lines, fmt.Sprintf("%s %s", evt.CreatedAt.Format("15:04"), truncateWithEllipsis(evt.Summary, 48)))
	}
	return lines
}

func inspectorProof(snapshot Snapshot) []string {
	if len(snapshot.RecentProofs) == 0 {
		return []string{"No proof events yet."}
	}
	lines := make([]string, 0, min(4, len(snapshot.RecentProofs)))
	limit := min(4, len(snapshot.RecentProofs))
	for _, evt := range snapshot.RecentProofs[:limit] {
		lines = append(lines, fmt.Sprintf("%s %s", evt.Timestamp.Format("15:04"), evt.Summary))
	}
	return lines
}

func inspectorOperator(snapshot Snapshot) []string {
	if isScratchIntakeSnapshot(snapshot) {
		return []string{
			"Local-only scratch intake session.",
			"No task-backed recovery or launch-control state exists here.",
		}
	}
	lines := []string{
		"state " + operatorStateLabel(snapshot),
		"next " + operatorActionLabel(snapshot),
		"readiness " + operatorReadinessLine(snapshot),
	}
	if launch := launchControlLine(snapshot); launch != "n/a" {
		lines = append(lines, "launch "+launch)
	}
	if reason := strongestOperatorReason(snapshot); reason != "none" {
		lines = append(lines, "reason "+truncateWithEllipsis(reason, 64))
	}
	return lines
}

func inspectorPendingMessage(snapshot Snapshot, ui UIState) []string {
	if ui.PendingTaskMessageEditMode {
		lines := []string{
			"Editing the staged local draft.",
			pendingMessageSummary(snapshot, ui),
			"Typing changes only the shell-local draft. Nothing here is canonical until you explicitly send it with m.",
		}
		for _, line := range wrapText(truncateWithEllipsis(currentPendingTaskMessage(ui), 160), 48) {
			lines = append(lines, line)
		}
		lines = append(lines, "save with ctrl-g then s", "cancel with ctrl-g then c", "send with ctrl-g then m")
		return lines
	}
	if strings.TrimSpace(ui.PendingTaskMessage) != "" {
		lines := []string{
			"Local draft is staged and ready for review.",
			pendingMessageSummary(snapshot, ui),
			"Editing and clearing stay shell-local. Sending with m is the explicit step that makes this canonical.",
		}
		for _, line := range wrapText(truncateWithEllipsis(ui.PendingTaskMessage, 160), 48) {
			lines = append(lines, line)
		}
		lines = append(lines, "edit with e", "send with m", "clear with x")
		return lines
	}
	if snapshot.HasLocalScratchAdoption() {
		return []string{
			"Local scratch is available for explicit adoption.",
			"Stage a shell-local draft with a.",
			"Nothing becomes canonical until you explicitly send that draft with m.",
		}
	}
	return []string{"No pending task message."}
}

func buildActivityLines(snapshot Snapshot, host WorkerHost, session SessionState) []string {
	lines := []string{latestCanonicalLine(snapshot)}
	if host != nil {
		for _, line := range host.ActivityLines(3) {
			lines = append(lines, line)
		}
	}
	for _, evt := range recentSessionEvents(session, 3) {
		lines = append(lines, fmt.Sprintf("%s  %s", evt.CreatedAt.Format("15:04:05"), evt.Summary))
	}
	if len(snapshot.RecentProofs) > 0 {
		lines = append(lines, "")
		limit := min(3, len(snapshot.RecentProofs))
		for _, evt := range snapshot.RecentProofs[:limit] {
			lines = append(lines, fmt.Sprintf("%s  %s", evt.Timestamp.Format("15:04:05"), evt.Summary))
		}
	}
	return lines
}

func checkpointLine(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "local-only"
	}
	if snapshot.Checkpoint == nil {
		return "none"
	}
	label := shortTaskID(snapshot.Checkpoint.ID)
	if snapshot.Checkpoint.IsResumable {
		return label + " raw-resumable"
	}
	return label + " raw-not-resumable"
}

func handoffLine(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "local-only"
	}
	if snapshot.Handoff == nil {
		return "none"
	}
	return fmt.Sprintf("%s %s->%s", snapshot.Handoff.Status, nonEmpty(snapshot.Handoff.SourceWorker, "unknown"), nonEmpty(snapshot.Handoff.TargetWorker, "unknown"))
}

func latestCanonicalLine(snapshot Snapshot) string {
	if strings.TrimSpace(snapshot.LatestCanonicalResponse) == "" {
		return "No canonical Tuku response persisted yet."
	}
	return truncateWithEllipsis(snapshot.LatestCanonicalResponse, 160)
}

func operatorStateLabel(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "local-only"
	}
	if snapshot.Recovery == nil || strings.TrimSpace(snapshot.Recovery.Class) == "" {
		return continuityLabel(snapshot)
	}
	switch snapshot.Recovery.Class {
	case "READY_NEXT_RUN":
		return "ready next run"
	case "INTERRUPTED_RUN_RECOVERABLE":
		return "interrupted recoverable"
	case "ACCEPTED_HANDOFF_LAUNCH_READY":
		if snapshot.LaunchControl != nil && snapshot.LaunchControl.State == "FAILED" && snapshot.LaunchControl.RetryDisposition == "ALLOWED" {
			return "launch retry ready"
		}
		return "accepted handoff launch ready"
	case "HANDOFF_LAUNCH_PENDING_OUTCOME":
		return "launch pending"
	case "HANDOFF_LAUNCH_COMPLETED":
		return "launch completed"
	case "HANDOFF_FOLLOW_THROUGH_REVIEW_REQUIRED":
		return "handoff follow-through review required"
	case "FAILED_RUN_REVIEW_REQUIRED":
		return "failed run review required"
	case "VALIDATION_REVIEW_REQUIRED":
		return "validation review required"
	case "STALE_RUN_RECONCILIATION_REQUIRED":
		return "stale run reconciliation required"
	case "DECISION_REQUIRED":
		return "decision required"
	case "CONTINUE_EXECUTION_REQUIRED":
		return "continue confirmation required"
	case "BLOCKED_DRIFT":
		return "drift blocked"
	case "REBRIEF_REQUIRED":
		return "rebrief required"
	case "REPAIR_REQUIRED":
		return "repair required"
	case "COMPLETED_NO_ACTION":
		return "completed"
	default:
		return humanizeConstant(snapshot.Recovery.Class)
	}
}

func operatorActionLabel(snapshot Snapshot) string {
	if snapshot.Recovery == nil || strings.TrimSpace(snapshot.Recovery.Action) == "" {
		return "none"
	}
	switch snapshot.Recovery.Action {
	case "START_NEXT_RUN":
		return "start next run"
	case "RESUME_INTERRUPTED_RUN":
		return "resume interrupted run"
	case "LAUNCH_ACCEPTED_HANDOFF":
		return "launch accepted handoff"
	case "WAIT_FOR_LAUNCH_OUTCOME":
		return "wait for launch outcome"
	case "MONITOR_LAUNCHED_HANDOFF":
		return "monitor launched handoff"
	case "REVIEW_HANDOFF_FOLLOW_THROUGH":
		return "review handoff follow-through"
	case "INSPECT_FAILED_RUN":
		return "inspect failed run"
	case "REVIEW_VALIDATION_STATE":
		return "review validation state"
	case "RECONCILE_STALE_RUN":
		return "reconcile stale run"
	case "MAKE_RESUME_DECISION":
		return "make resume decision"
	case "EXECUTE_CONTINUE_RECOVERY":
		return "finalize continue"
	case "REPAIR_CONTINUITY":
		return "repair continuity"
	case "REGENERATE_BRIEF":
		return "regenerate brief"
	case "NONE":
		return "none"
	default:
		return humanizeConstant(snapshot.Recovery.Action)
	}
}

func operatorReadinessLine(snapshot Snapshot) string {
	nextRun := false
	handoffLaunch := false
	if snapshot.Recovery != nil {
		nextRun = snapshot.Recovery.ReadyForNextRun
		handoffLaunch = snapshot.Recovery.ReadyForHandoffLaunch
	}
	return fmt.Sprintf("next-run %s | handoff-launch %s", yesNo(nextRun), yesNo(handoffLaunch))
}

func strongestOperatorReason(snapshot Snapshot) string {
	if snapshot.Recovery != nil {
		if reason := strings.TrimSpace(snapshot.Recovery.Reason); reason != "" {
			return reason
		}
		if len(snapshot.Recovery.Issues) > 0 {
			if msg := strings.TrimSpace(snapshot.Recovery.Issues[0].Message); msg != "" {
				return msg
			}
		}
	}
	if snapshot.LaunchControl != nil {
		if reason := strings.TrimSpace(snapshot.LaunchControl.Reason); reason != "" {
			return reason
		}
	}
	return "none"
}

func launchControlLine(snapshot Snapshot) string {
	if snapshot.LaunchControl == nil || snapshot.LaunchControl.State == "" || snapshot.LaunchControl.State == "NOT_APPLICABLE" {
		return "n/a"
	}
	state := ""
	switch snapshot.LaunchControl.State {
	case "NOT_REQUESTED":
		state = "not requested"
	case "REQUESTED_OUTCOME_UNKNOWN":
		state = "pending outcome unknown"
	case "COMPLETED":
		state = "completed (invocation only)"
	case "FAILED":
		state = "failed"
	default:
		state = humanizeConstant(snapshot.LaunchControl.State)
	}
	retry := "retry " + strings.ToLower(nonEmpty(snapshot.LaunchControl.RetryDisposition, "unknown"))
	return state + " | " + retry
}

func handoffContinuityLine(snapshot Snapshot) string {
	if snapshot.HandoffContinuity == nil || snapshot.HandoffContinuity.State == "" || snapshot.HandoffContinuity.State == "NOT_APPLICABLE" {
		return "n/a"
	}
	switch snapshot.HandoffContinuity.State {
	case "ACCEPTED_NOT_LAUNCHED":
		return "accepted, not launched"
	case "LAUNCH_PENDING_OUTCOME":
		return "launch pending, downstream outcome unknown"
	case "LAUNCH_FAILED_RETRYABLE":
		return "launch failed, retry allowed"
	case "LAUNCH_COMPLETED_ACK_CAPTURED":
		return "launch completed, acknowledgment captured, downstream unproven"
	case "LAUNCH_COMPLETED_ACK_UNAVAILABLE":
		return "launch completed, acknowledgment unavailable, downstream unproven"
	case "LAUNCH_COMPLETED_ACK_MISSING":
		return "launch completed, acknowledgment missing, continuity inconsistent"
	case "FOLLOW_THROUGH_PROOF_OF_LIFE":
		return "proof of life observed, completion unproven"
	case "FOLLOW_THROUGH_CONFIRMED":
		return "continuation confirmed, completion unproven"
	case "FOLLOW_THROUGH_UNKNOWN":
		return "follow-through still unknown"
	case "FOLLOW_THROUGH_STALLED":
		return "follow-through stalled, review required"
	default:
		return humanizeConstant(snapshot.HandoffContinuity.State)
	}
}

func operatorPaneCue(snapshot Snapshot) string {
	state := operatorStateLabel(snapshot)
	action := operatorActionLabel(snapshot)
	if state == "" || state == "local-only" {
		return state
	}
	if action == "" || action == "none" {
		return state
	}
	return state + " | next " + action
}

func pendingMessageSummary(snapshot Snapshot, ui UIState) string {
	if ui.PendingTaskMessageEditMode {
		switch ui.PendingTaskMessageSource {
		case "local_scratch_adoption":
			return "editing staged draft from local scratch"
		default:
			return "editing staged local draft"
		}
	}
	if strings.TrimSpace(ui.PendingTaskMessage) != "" {
		switch ui.PendingTaskMessageSource {
		case "local_scratch_adoption":
			return "staged draft from local scratch"
		default:
			return "staged local draft"
		}
	}
	if snapshot.HasLocalScratchAdoption() {
		return "local scratch available"
	}
	return "none"
}

func isScratchIntakeSnapshot(snapshot Snapshot) bool {
	return strings.TrimSpace(snapshot.TaskID) == "" &&
		strings.EqualFold(strings.TrimSpace(snapshot.Phase), "SCRATCH_INTAKE")
}

func footerText(snapshot Snapshot, ui UIState, host WorkerHost) string {
	parts := make([]string, 0, 12)
	if ui.Session.SessionID != "" {
		parts = append(parts, "session "+shortTaskID(ui.Session.SessionID))
	}
	if host != nil {
		status := host.Status()
		if status.InputLive {
			parts = append(parts, "worker live input")
		} else {
			parts = append(parts, "worker read-only")
		}
		if cue := footerHostCue(snapshot, ui, status); cue != "" {
			parts = append(parts, cue)
		}
	}
	if operator := footerOperatorCue(snapshot); operator != "" {
		parts = append(parts, operator)
	}
	parts = append(parts, "q quit", "h help", "i inspector", "p activity", "r refresh", "s status")
	if host != nil && ui.Focus == FocusWorker && host.CanAcceptInput() {
		parts = append(parts, "ctrl-g shell commands")
	}
	if ui.EscapePrefix {
		parts = append(parts, "shell command armed")
	}
	if ui.PendingTaskMessageEditMode {
		parts = append(parts, "editing staged draft")
	} else if pending := strings.TrimSpace(ui.PendingTaskMessage); pending != "" {
		parts = append(parts, "staged local draft")
	} else if snapshot.HasLocalScratchAdoption() {
		parts = append(parts, "local scratch available")
	}
	if !ui.LastRefresh.IsZero() {
		parts = append(parts, "refreshed "+ui.LastRefresh.Format("15:04:05"))
	}
	if ui.LastError != "" {
		parts = append(parts, truncateWithEllipsis(ui.LastError, 80))
	} else if host != nil {
		if note := strings.TrimSpace(host.Status().Note); note != "" {
			parts = append(parts, truncateWithEllipsis(note, 80))
		}
	}
	return strings.Join(parts, " | ")
}

func hostStatusLine(snapshot Snapshot, ui UIState, host WorkerHost) string {
	if host == nil {
		return "none"
	}
	status := host.Status()
	line := fmt.Sprintf("%s / %s", nonEmpty(string(status.Mode), "unknown"), nonEmpty(string(status.State), "unknown"))
	if status.InputLive {
		line += " / input live"
	} else {
		line += " / input off"
	}
	if status.ExitCode != nil {
		line += fmt.Sprintf(" / exit %d", *status.ExitCode)
	}
	if temporal := hostTemporalStatus(snapshot, ui, status); temporal != "" {
		line += " / " + temporal
	}
	if note := strings.TrimSpace(status.Note); note != "" {
		line += " / " + truncateWithEllipsis(note, 48)
	}
	return line
}

func workerPaneSummaryLine(snapshot Snapshot, ui UIState, host WorkerHost) string {
	if host == nil {
		return ""
	}
	status := host.Status()
	label := nonEmpty(strings.TrimSpace(status.Label), strings.TrimSpace(string(status.Mode)))
	now := observedAt(ui)
	cue := workerPanePrimaryCue(snapshot, status, now)
	operatorCue := operatorPaneCue(snapshot)
	if operatorCue == "" {
		if cue == "" {
			return label
		}
		return label + " | " + cue
	}
	if cue == "" {
		return operatorCue + " | " + label
	}
	return operatorCue + " | " + label + " | " + cue
}

func workerPanePrimaryCue(snapshot Snapshot, status HostStatus, now time.Time) string {
	switch status.State {
	case HostStateLive:
		if status.LastOutputAt.IsZero() {
			return "awaiting visible output"
		}
		return livePaneCue(status, now)
	case HostStateStarting:
		return "starting up"
	case HostStateExited, HostStateFailed:
		return inactivePaneCue(status)
	case HostStateFallback:
		return "historical transcript below | fallback active"
	case HostStateTranscriptOnly:
		if savedAt := latestTranscriptTimestamp(snapshot); !savedAt.IsZero() {
			return "historical transcript below | saved transcript " + savedAt.Format("15:04:05")
		}
		return "historical transcript below"
	}
	return ""
}

func livePaneCue(status HostStatus, now time.Time) string {
	since := elapsedSince(now, status.LastOutputAt)
	switch {
	case since <= 0:
		return "newest output at bottom"
	case since < 60*time.Second:
		return "newest output at bottom"
	case since < 2*time.Minute:
		return "newest output at bottom | quiet"
	default:
		return "newest output at bottom | quiet a while"
	}
}

func inactivePaneCue(status HostStatus) string {
	switch status.State {
	case HostStateFailed:
		return "newest captured output at bottom | worker failed"
	default:
		return "newest captured output at bottom | worker exited"
	}
}

func footerHostCue(snapshot Snapshot, ui UIState, status HostStatus) string {
	now := observedAt(ui)
	switch status.State {
	case HostStateLive:
		if status.LastOutputAt.IsZero() {
			return "awaiting output"
		}
		since := elapsedSince(now, status.LastOutputAt)
		switch {
		case since <= 0:
			return "recent output"
		case since < 60*time.Second:
			return "recent output"
		case since < 2*time.Minute:
			return "quiet"
		default:
			return "quiet a while"
		}
	case HostStateStarting:
		return "starting"
	case HostStateExited:
		if elapsedSince(now, status.StateChangedAt) < 30*time.Second {
			return "recent exit"
		}
		return "exited"
	case HostStateFailed:
		if elapsedSince(now, status.StateChangedAt) < 30*time.Second {
			return "recent failure"
		}
		return "failed"
	case HostStateFallback:
		return "fallback active"
	case HostStateTranscriptOnly:
		if !latestTranscriptTimestamp(snapshot).IsZero() {
			return "historical transcript"
		}
		return "read-only transcript"
	}
	return ""
}

func hostTemporalStatus(snapshot Snapshot, ui UIState, status HostStatus) string {
	now := observedAt(ui)
	switch status.State {
	case HostStateLive:
		if status.LastOutputAt.IsZero() {
			return describeAwaitingVisibleOutput(status, now)
		}
		return describeLiveOutputAssessment(status, now)
	case HostStateStarting:
		return describeAwaitingVisibleOutput(status, now)
	case HostStateExited, HostStateFailed:
		return describeInactiveState(status, now)
	case HostStateFallback:
		return describeFallbackState(status, now)
	case HostStateTranscriptOnly:
		if savedAt := latestTranscriptTimestamp(snapshot); !savedAt.IsZero() {
			return "latest transcript " + savedAt.Format("15:04:05")
		}
	}
	return ""
}

func latestTranscriptTimestamp(snapshot Snapshot) time.Time {
	var latest time.Time
	for _, item := range snapshot.RecentConversation {
		if item.CreatedAt.After(latest) {
			latest = item.CreatedAt
		}
	}
	return latest
}

func observedAt(ui UIState) time.Time {
	if !ui.ObservedAt.IsZero() {
		return ui.ObservedAt
	}
	if !ui.LastRefresh.IsZero() {
		return ui.LastRefresh
	}
	return time.Now().UTC()
}

func describeAwaitingVisibleOutput(status HostStatus, now time.Time) string {
	since := elapsedSince(now, status.StateChangedAt)
	if since <= 0 {
		return "awaiting first visible output"
	}
	return "awaiting first visible output for " + formatElapsed(since)
}

func describeLiveOutputAssessment(status HostStatus, now time.Time) string {
	since := elapsedSince(now, status.LastOutputAt)
	if since <= 0 {
		return "quiet with recent visible output"
	}
	if since >= 60*time.Second {
		return "quiet for " + formatElapsed(since) + "; possibly waiting for input or stalled"
	}
	return "quiet for " + formatElapsed(since)
}

func describeInactiveState(status HostStatus, now time.Time) string {
	sinceChange := elapsedSince(now, status.StateChangedAt)
	switch status.State {
	case HostStateFailed:
		if sinceChange > 0 && sinceChange < 30*time.Second {
			return "recently failed " + formatElapsed(sinceChange) + " ago"
		}
		if sinceChange > 0 {
			return "failed " + formatElapsed(sinceChange) + " ago"
		}
		return "worker failed"
	default:
		if sinceChange > 0 && sinceChange < 30*time.Second {
			return "recently exited " + formatElapsed(sinceChange) + " ago"
		}
		if sinceChange > 0 {
			return "exited " + formatElapsed(sinceChange) + " ago"
		}
		return "worker exited"
	}
}

func describeFallbackState(status HostStatus, now time.Time) string {
	since := elapsedSince(now, status.StateChangedAt)
	if since <= 0 {
		return "fallback active"
	}
	return "fallback activated " + formatElapsed(since) + " ago"
}

func describeInactiveBody(status HostStatus) string {
	if status.LastOutputAt.IsZero() {
		return "The session ended before any visible output arrived."
	}
	return "No newer worker output arrived after the session ended."
}

func elapsedSince(now time.Time, then time.Time) time.Duration {
	if now.IsZero() || then.IsZero() {
		return 0
	}
	if then.After(now) {
		return 0
	}
	return now.Sub(then)
}

func formatElapsed(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Round(time.Second)/time.Second))
	}
	if d < 10*time.Minute {
		seconds := int(d.Round(time.Second) / time.Second)
		minutes := seconds / 60
		remain := seconds % 60
		if remain == 0 {
			return fmt.Sprintf("%dm", minutes)
		}
		return fmt.Sprintf("%dm%ds", minutes, remain)
	}
	minutes := int(d.Round(time.Minute) / time.Minute)
	return fmt.Sprintf("%dm", minutes)
}

func sessionPriorLine(session SessionState) string {
	if strings.TrimSpace(session.PriorPersistedSummary) == "" {
		return "previous shell outcome none"
	}
	return "previous shell outcome " + truncateWithEllipsis(session.PriorPersistedSummary, 48)
}

func nonEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func humanizeConstant(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	return strings.ReplaceAll(value, "_", " ")
}

func footerOperatorCue(snapshot Snapshot) string {
	if snapshot.Recovery == nil || isScratchIntakeSnapshot(snapshot) {
		return ""
	}
	action := operatorActionLabel(snapshot)
	if action == "" || action == "none" {
		return ""
	}
	return "next " + action
}

func truncateWithEllipsis(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func pendingTaskMessageEditorLines(ui UIState, height int, width int) []string {
	if height < 1 {
		return nil
	}
	lines := []string{
		"editing staged local draft",
		"this draft stays shell-local until you explicitly send it",
		"ctrl-g s save edit | ctrl-g c cancel edit | ctrl-g m send | ctrl-g x clear",
		"",
	}
	buffer := currentPendingTaskMessage(ui)
	editorLines := strings.Split(buffer, "\n")
	if len(editorLines) == 0 {
		editorLines = []string{""}
	}
	for idx, line := range editorLines {
		prefix := "draft> "
		if idx > 0 {
			prefix = "       "
		}
		lines = append(lines, wrapText(prefix+line, width)...)
	}
	return fitBottom(lines, height)
}

```


## internal/tui/shell/viewmodel_test.go

```go
package shell

import (
	"strings"
	"testing"
	"time"

	"tuku/internal/ipc"
)

func TestBuildViewModelReflectsSnapshotState(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	host := &stubHost{
		title:    "worker pane | codex live session",
		worker:   "codex live",
		lines:    []string{"codex> hello"},
		activity: []string{"12:00:00  worker host started"},
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateLive,
			Label:     "codex live",
			InputLive: true,
			Width:     80,
			Height:    20,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_1234567890",
		Goal:   "Implement shell",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
		Repo: RepoAnchor{
			RepoRoot:         "/Users/kagaya/Desktop/Tuku",
			Branch:           "main",
			HeadSHA:          "abc123",
			WorkingTreeDirty: true,
			CapturedAt:       now,
		},
		IntentSummary: "implement: build shell",
		Brief: &BriefSummary{
			ID:               "brf_1",
			Objective:        "Build worker-native shell",
			NormalizedAction: "build-shell",
			Constraints:      []string{"keep it narrow"},
			DoneCriteria:     []string{"full-screen shell"},
		},
		Run: &RunSummary{
			ID:               "run_1",
			WorkerKind:       "codex",
			Status:           "RUNNING",
			LastKnownSummary: "applying shell patch",
			StartedAt:        now,
		},
		Checkpoint: &CheckpointSummary{
			ID:               "chk_1",
			Trigger:          "CONTINUE",
			CreatedAt:        now,
			ResumeDescriptor: "resume from shell-ready checkpoint",
			IsResumable:      true,
		},
		Recovery: &RecoverySummary{
			Class:           "READY_NEXT_RUN",
			Action:          "START_NEXT_RUN",
			ReadyForNextRun: true,
			Reason:          "task is ready for the next bounded run with brief brf_1",
		},
		Handoff: &HandoffSummary{
			ID:           "hnd_1",
			Status:       "ACCEPTED",
			SourceWorker: "codex",
			TargetWorker: "claude",
			Mode:         "resume",
			CreatedAt:    now,
		},
		Acknowledgment: &AcknowledgmentSummary{
			Status:    "CAPTURED",
			Summary:   "Claude acknowledged the handoff packet.",
			CreatedAt: now,
		},
		HandoffContinuity: &HandoffContinuitySummary{
			State:                        "LAUNCH_COMPLETED_ACK_CAPTURED",
			Reason:                       "Claude handoff launch completed and initial acknowledgment was captured; downstream continuation remains unproven",
			LaunchID:                     "hlc_1",
			AcknowledgmentStatus:         "CAPTURED",
			AcknowledgmentSummary:        "Claude acknowledged the handoff packet.",
			DownstreamContinuationProven: false,
		},
		RecentProofs: []ProofItem{
			{ID: "evt_1", Type: "BRIEF_CREATED", Summary: "Execution brief updated", Timestamp: now},
			{ID: "evt_2", Type: "HANDOFF_CREATED", Summary: "Handoff packet created", Timestamp: now},
		},
		RecentConversation: []ConversationItem{
			{Role: "user", Body: "Start implementation.", CreatedAt: now},
			{Role: "system", Body: "I prepared the shell state.", CreatedAt: now},
		},
		LatestCanonicalResponse: "I prepared the shell state.",
	}, UIState{
		ShowInspector: true,
		ShowProof:     true,
		Focus:         FocusWorker,
		Session: SessionState{
			SessionID: "shs_1234567890",
			StartedAt: now,
			Journal: []SessionEvent{
				{Type: SessionEventShellStarted, Summary: "Shell session shs_1234567890 started.", CreatedAt: now},
				{Type: SessionEventHostLive, Summary: "Live worker host is active.", CreatedAt: now},
			},
			PriorPersistedSummary: "Shell live host ended",
		},
		LastRefresh: now,
	}, host, 120, 32)

	if vm.Header.Worker != "codex live" {
		t.Fatalf("expected active worker label, got %q", vm.Header.Worker)
	}
	if vm.Header.Continuity != "ready" {
		t.Fatalf("expected ready continuity, got %q", vm.Header.Continuity)
	}
	if vm.Inspector == nil {
		t.Fatal("expected inspector pane")
	}
	if vm.ProofStrip == nil {
		t.Fatal("expected proof strip")
	}
	if vm.Overlay != nil {
		t.Fatal("expected no overlay")
	}
	if len(vm.WorkerPane.Lines) == 0 {
		t.Fatal("expected worker pane lines")
	}
	if len(vm.ProofStrip.Lines) < 2 {
		t.Fatal("expected activity lines merged into proof strip")
	}
	if vm.Inspector.Sections[0].Title != "operator" {
		t.Fatalf("expected operator section first, got %q", vm.Inspector.Sections[0].Title)
	}
	foundOperatorNext := false
	for _, line := range vm.Inspector.Sections[0].Lines {
		if strings.Contains(line, "next start next run") {
			foundOperatorNext = true
		}
	}
	if !foundOperatorNext {
		t.Fatalf("expected operator section to include next action, got %#v", vm.Inspector.Sections[0].Lines)
	}
	if vm.Inspector.Sections[1].Title != "worker session" {
		t.Fatalf("expected worker session section second, got %q", vm.Inspector.Sections[1].Title)
	}
	foundSessionLine := false
	for _, line := range vm.Inspector.Sections[1].Lines {
		if strings.Contains(line, "new shell session shs_1234567890") {
			foundSessionLine = true
		}
	}
	if !foundSessionLine {
		t.Fatalf("expected worker-session inspector to include session id, got %#v", vm.Inspector.Sections[1].Lines)
	}
	foundHandoffContinuity := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "handoff" {
			continue
		}
		for _, line := range section.Lines {
			if strings.Contains(line, "continuity launch completed, acknowledgment captured, downstream unproven") {
				foundHandoffContinuity = true
				break
			}
		}
	}
	if !foundHandoffContinuity {
		t.Fatalf("expected handoff continuity line in inspector, got %#v", vm.Inspector.Sections)
	}
}

func TestBuildViewModelSurfacesRebriefRequiredState(t *testing.T) {
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_rebrief",
		Phase:  "BLOCKED",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:           "REBRIEF_REQUIRED",
			Action:          "REGENERATE_BRIEF",
			ReadyForNextRun: false,
			Reason:          "operator chose to regenerate the execution brief before another run",
		},
		Checkpoint: &CheckpointSummary{
			ID:          "chk_rebrief",
			IsResumable: true,
		},
	}, UIState{ShowStatus: true}, NewTranscriptHost(), 120, 32)

	if vm.Header.Continuity != "rebrief" {
		t.Fatalf("expected rebrief continuity label, got %q", vm.Header.Continuity)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	status := strings.Join(vm.Overlay.Lines, "\n")
	if !strings.Contains(status, "recovery rebrief required") {
		t.Fatalf("expected rebrief operator state, got %q", status)
	}
	if !strings.Contains(status, "next regenerate brief") {
		t.Fatalf("expected regenerate-brief operator action, got %q", status)
	}
}

func TestBuildViewModelSurfacesContinueExecutionRequiredState(t *testing.T) {
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_continue_pending",
		Phase:  "BRIEF_READY",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:           "CONTINUE_EXECUTION_REQUIRED",
			Action:          "EXECUTE_CONTINUE_RECOVERY",
			ReadyForNextRun: false,
			Reason:          "operator chose to continue with the current brief, but explicit continue finalization is still required",
		},
		Checkpoint: &CheckpointSummary{
			ID:          "chk_continue_pending",
			IsResumable: true,
		},
	}, UIState{ShowStatus: true}, NewTranscriptHost(), 120, 32)

	if vm.Header.Continuity != "continue-pending" {
		t.Fatalf("expected continue-pending continuity label, got %q", vm.Header.Continuity)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	status := strings.Join(vm.Overlay.Lines, "\n")
	if !strings.Contains(status, "recovery continue confirmation required") {
		t.Fatalf("expected continue-confirmation operator state, got %q", status)
	}
	if !strings.Contains(status, "next finalize continue") {
		t.Fatalf("expected finalize-continue operator action, got %q", status)
	}
}

func TestBuildViewModelSurfacesStalledClaudeFollowThroughState(t *testing.T) {
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_handoff_stalled",
		Phase:  "BLOCKED",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:           "HANDOFF_FOLLOW_THROUGH_REVIEW_REQUIRED",
			Action:          "REVIEW_HANDOFF_FOLLOW_THROUGH",
			ReadyForNextRun: false,
			Reason:          "Claude handoff follow-through appears stalled and needs review",
		},
		HandoffContinuity: &HandoffContinuitySummary{
			State:                "FOLLOW_THROUGH_STALLED",
			Reason:               "Claude handoff launch appears stalled and needs review",
			FollowThroughKind:    "STALLED_REVIEW_REQUIRED",
			FollowThroughSummary: "Claude follow-through appears stalled",
		},
		Handoff: &HandoffSummary{
			ID:           "hnd_1",
			Status:       "ACCEPTED",
			SourceWorker: "codex",
			TargetWorker: "claude",
			Mode:         "resume",
		},
		FollowThrough: &FollowThroughSummary{
			RecordID: "hft_1",
			Kind:     "STALLED_REVIEW_REQUIRED",
			Summary:  "Claude follow-through appears stalled",
		},
	}, UIState{ShowStatus: true, ShowInspector: true}, NewTranscriptHost(), 120, 32)

	if vm.Header.Continuity != "review" {
		t.Fatalf("expected review continuity label, got %q", vm.Header.Continuity)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	status := strings.Join(vm.Overlay.Lines, "\n")
	if !strings.Contains(status, "recovery handoff follow-through review required") {
		t.Fatalf("expected stalled handoff operator state, got %q", status)
	}
	if !strings.Contains(status, "next review handoff follow-through") {
		t.Fatalf("expected stalled handoff operator action, got %q", status)
	}
	found := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "handoff" {
			continue
		}
		for _, line := range section.Lines {
			if strings.Contains(line, "continuity follow-through stalled, review required") {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatalf("expected stalled follow-through continuity in inspector, got %#v", vm.Inspector.Sections)
	}
}

func TestBuildViewModelAddsLiveWorkerPaneRecencySummary(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	host := &stubHost{
		title:  "worker pane | codex live | input to worker",
		worker: "codex live",
		lines:  []string{"codex> hello"},
		status: HostStatus{
			Mode:         HostModeCodexPTY,
			State:        HostStateLive,
			Label:        "codex live",
			InputLive:    true,
			LastOutputAt: now,
		},
	}

	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_live_summary",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
	}, UIState{
		Focus:      FocusWorker,
		ObservedAt: now.Add(18 * time.Second),
		Session: SessionState{
			SessionID: "shs_live_summary",
		},
	}, host, 120, 20)

	if len(vm.WorkerPane.Lines) == 0 {
		t.Fatal("expected worker pane lines")
	}
	if !strings.Contains(vm.WorkerPane.Lines[0], "codex live | newest output at bottom") {
		t.Fatalf("expected live recency summary, got %#v", vm.WorkerPane.Lines)
	}
	if strings.Contains(vm.WorkerPane.Lines[0], "quiet for 18s") {
		t.Fatalf("expected pane summary to stay concise, got %#v", vm.WorkerPane.Lines)
	}
}

func TestSnapshotFromIPCMapsShellState(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	raw := ipc.TaskShellSnapshotResponse{
		TaskID:        "tsk_1",
		Goal:          "Goal",
		Phase:         "BRIEF_READY",
		Status:        "ACTIVE",
		IntentClass:   "implement",
		IntentSummary: "implement: wire shell",
		RepoAnchor: ipc.RepoAnchor{
			RepoRoot:         "/tmp/repo",
			Branch:           "main",
			HeadSHA:          "sha",
			WorkingTreeDirty: true,
			CapturedAt:       now,
		},
		Brief: &ipc.TaskShellBrief{
			BriefID:          "brf_1",
			Objective:        "Objective",
			NormalizedAction: "act",
			Constraints:      []string{"c1"},
			DoneCriteria:     []string{"d1"},
		},
		Run: &ipc.TaskShellRun{
			RunID:            "run_1",
			WorkerKind:       "codex",
			Status:           "COMPLETED",
			LastKnownSummary: "done",
			StartedAt:        now,
		},
		Launch: &ipc.TaskShellLaunch{
			AttemptID:   "hlc_1",
			LaunchID:    "launch_1",
			Status:      "FAILED",
			RequestedAt: now,
			EndedAt:     now.Add(2 * time.Second),
			Summary:     "launcher failed",
		},
		LaunchControl: &ipc.TaskShellLaunchControl{
			State:            "FAILED",
			RetryDisposition: "ALLOWED",
			Reason:           "durable failure may be retried",
			HandoffID:        "hnd_1",
			AttemptID:        "hlc_1",
			LaunchID:         "launch_1",
			TargetWorker:     "claude",
			RequestedAt:      now,
			FailedAt:         now.Add(2 * time.Second),
		},
		Recovery: &ipc.TaskShellRecovery{
			ContinuityOutcome:     "SAFE_RESUME_AVAILABLE",
			RecoveryClass:         "FAILED_RUN_REVIEW_REQUIRED",
			RecommendedAction:     "INSPECT_FAILED_RUN",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: false,
			Reason:                "latest run failed",
			Issues: []ipc.TaskShellRecoveryIssue{
				{Code: "RUN_BRIEF_MISSING", Message: "run references missing brief"},
			},
		},
		RecentProofs: []ipc.TaskShellProof{
			{EventID: "evt_1", Type: "CHECKPOINT_CREATED", Summary: "Checkpoint created", Timestamp: now},
		},
		RecentConversation: []ipc.TaskShellConversation{
			{Role: "system", Body: "Canonical response.", CreatedAt: now},
		},
		LatestCanonicalResponse: "Canonical response.",
	}

	snapshot := snapshotFromIPC(raw)
	if snapshot.TaskID != "tsk_1" {
		t.Fatalf("expected task id tsk_1, got %q", snapshot.TaskID)
	}
	if snapshot.Brief == nil || snapshot.Brief.ID != "brf_1" {
		t.Fatal("expected brief mapping")
	}
	if snapshot.Run == nil || snapshot.Run.WorkerKind != "codex" {
		t.Fatal("expected run mapping")
	}
	if snapshot.Launch == nil || snapshot.Launch.AttemptID != "hlc_1" {
		t.Fatalf("expected launch mapping, got %+v", snapshot.Launch)
	}
	if snapshot.LaunchControl == nil || snapshot.LaunchControl.RetryDisposition != "ALLOWED" {
		t.Fatalf("expected launch control mapping, got %+v", snapshot.LaunchControl)
	}
	if snapshot.Recovery == nil || snapshot.Recovery.Class != "FAILED_RUN_REVIEW_REQUIRED" {
		t.Fatalf("expected recovery mapping, got %+v", snapshot.Recovery)
	}
	if len(snapshot.RecentProofs) != 1 || snapshot.RecentProofs[0].Summary != "Checkpoint created" {
		t.Fatal("expected proof mapping")
	}
}

func TestContinuityLabelUsesRecoveryTruthOverRawCheckpointResumability(t *testing.T) {
	snapshot := Snapshot{
		Status: "ACTIVE",
		Checkpoint: &CheckpointSummary{
			ID:          "chk_1",
			IsResumable: true,
		},
		Recovery: &RecoverySummary{
			Class:           "FAILED_RUN_REVIEW_REQUIRED",
			ReadyForNextRun: false,
		},
	}

	if got := continuityLabel(snapshot); got != "review" {
		t.Fatalf("expected recovery-driven continuity label, got %q", got)
	}
}

func TestContinuityLabelDistinguishesReadyRecoverableAndLaunchStates(t *testing.T) {
	cases := []struct {
		name     string
		snapshot Snapshot
		want     string
	}{
		{
			name: "ready next run",
			snapshot: Snapshot{
				Recovery: &RecoverySummary{Class: "READY_NEXT_RUN", ReadyForNextRun: true},
			},
			want: "ready",
		},
		{
			name: "interrupted recoverable",
			snapshot: Snapshot{
				Recovery: &RecoverySummary{Class: "INTERRUPTED_RUN_RECOVERABLE", ReadyForNextRun: true},
			},
			want: "recoverable",
		},
		{
			name: "launch retry",
			snapshot: Snapshot{
				Recovery:      &RecoverySummary{Class: "ACCEPTED_HANDOFF_LAUNCH_READY", ReadyForHandoffLaunch: true},
				LaunchControl: &LaunchControlSummary{State: "FAILED", RetryDisposition: "ALLOWED"},
			},
			want: "launch-retry",
		},
		{
			name: "completed",
			snapshot: Snapshot{
				Recovery: &RecoverySummary{Class: "COMPLETED_NO_ACTION"},
			},
			want: "complete",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := continuityLabel(tc.snapshot); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestBuildViewModelStatusOverlayReflectsHostState(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	host := &stubHost{
		status: HostStatus{
			Mode:           HostModeTranscript,
			State:          HostStateFallback,
			Label:          "transcript fallback",
			InputLive:      false,
			Note:           "live worker exited; switched to transcript fallback",
			StateChangedAt: now,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_overlay",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:           "FAILED_RUN_REVIEW_REQUIRED",
			Action:          "INSPECT_FAILED_RUN",
			ReadyForNextRun: false,
			Reason:          "latest run run_1 failed; inspect failure evidence before retrying or regenerating the brief",
		},
		LatestCanonicalResponse: "Tuku is ready to continue from transcript mode.",
	}, UIState{
		ShowStatus: true,
		ObservedAt: now.Add(6 * time.Second),
		Session: SessionState{
			SessionID:             "shs_overlay",
			PriorPersistedSummary: "Shell transcript fallback activated",
		},
	}, host, 120, 32)

	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundHostLine := false
	foundVerboseFallbackTiming := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "host transcript / fallback / input off") {
			foundHostLine = true
		}
		if strings.Contains(line, "fallback activated 6s ago") {
			foundVerboseFallbackTiming = true
		}
	}
	if !foundHostLine {
		t.Fatalf("expected host status line in overlay, got %#v", vm.Overlay.Lines)
	}
	if !foundVerboseFallbackTiming {
		t.Fatalf("expected overlay to retain verbose fallback timing, got %#v", vm.Overlay.Lines)
	}
	foundSessionLine := false
	foundPriorLine := false
	foundRecoveryLine := false
	foundNextLine := false
	foundReasonLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "new shell session shs_overlay") {
			foundSessionLine = true
		}
		if strings.Contains(line, "previous shell outcome Shell transcript fallback activated") {
			foundPriorLine = true
		}
		if strings.Contains(line, "recovery failed run review required") {
			foundRecoveryLine = true
		}
		if strings.Contains(line, "next inspect failed run") {
			foundNextLine = true
		}
		if strings.Contains(line, "reason latest run run_1 failed") {
			foundReasonLine = true
		}
	}
	if !foundSessionLine {
		t.Fatalf("expected session id in overlay, got %#v", vm.Overlay.Lines)
	}
	if !foundPriorLine {
		t.Fatalf("expected previous shell outcome in overlay, got %#v", vm.Overlay.Lines)
	}
	if !foundRecoveryLine || !foundNextLine || !foundReasonLine {
		t.Fatalf("expected operator truth lines in overlay, got %#v", vm.Overlay.Lines)
	}
	if !strings.Contains(vm.Footer, "read-only") {
		t.Fatalf("expected footer to clarify read-only fallback, got %q", vm.Footer)
	}
	if !strings.Contains(vm.Footer, "fallback active") {
		t.Fatalf("expected footer to include short fallback cue, got %q", vm.Footer)
	}
	if !strings.Contains(vm.Footer, "next inspect failed run") {
		t.Fatalf("expected footer to include operator next-action cue, got %q", vm.Footer)
	}
	if strings.Contains(vm.Footer, "fallback activated 6s ago") {
		t.Fatalf("expected footer to avoid duplicating verbose fallback timing, got %q", vm.Footer)
	}
}

func TestBuildViewModelAddsFallbackWorkerPaneSummary(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	host := NewTranscriptHost()
	host.markFallback("live worker exited; switched to transcript fallback")
	host.status.StateChangedAt = now
	host.UpdateSnapshot(Snapshot{
		RecentConversation: []ConversationItem{
			{Role: "system", Body: "Canonical response.", CreatedAt: now},
		},
	})

	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_fallback_summary",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:           "HANDOFF_LAUNCH_PENDING_OUTCOME",
			Action:          "WAIT_FOR_LAUNCH_OUTCOME",
			ReadyForNextRun: false,
		},
		RecentConversation: []ConversationItem{
			{Role: "system", Body: "Canonical response.", CreatedAt: now},
		},
	}, UIState{
		Focus:      FocusWorker,
		ObservedAt: now.Add(6 * time.Second),
		Session: SessionState{
			SessionID: "shs_fallback_summary",
		},
	}, host, 120, 20)

	if len(vm.WorkerPane.Lines) == 0 {
		t.Fatal("expected worker pane lines")
	}
	if !strings.Contains(vm.WorkerPane.Lines[0], "launch pending | next wait for launch outcome | transcript fallback | historical transcript below | fallback active") {
		t.Fatalf("expected fallback summary line, got %#v", vm.WorkerPane.Lines)
	}
}

func TestBuildViewModelShowsLongQuietLiveInferenceCarefully(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	host := &stubHost{
		title:  "worker pane | codex live | input to worker",
		worker: "codex live",
		lines:  []string{"codex> hello"},
		status: HostStatus{
			Mode:         HostModeCodexPTY,
			State:        HostStateLive,
			Label:        "codex live",
			InputLive:    true,
			LastOutputAt: now,
		},
	}

	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_live_quiet",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
	}, UIState{
		Focus:      FocusWorker,
		ObservedAt: now.Add(2 * time.Minute),
		Session: SessionState{
			SessionID: "shs_live_quiet",
		},
	}, host, 120, 20)

	if !strings.Contains(vm.WorkerPane.Lines[0], "quiet a while") {
		t.Fatalf("expected concise long-quiet pane cue, got %#v", vm.WorkerPane.Lines)
	}
	if !strings.Contains(vm.Footer, "quiet a while") {
		t.Fatalf("expected footer to carry a short quiet-state cue, got %q", vm.Footer)
	}
	if strings.Contains(vm.Footer, "possibly waiting for input or stalled") {
		t.Fatalf("expected footer to avoid duplicating verbose quiet inference, got %q", vm.Footer)
	}
}

func TestBuildViewModelReflectsClaudeHostState(t *testing.T) {
	host := &stubHost{
		title:    "worker pane | claude live session",
		worker:   "claude live",
		lines:    []string{"claude> hello"},
		canInput: true,
		status: HostStatus{
			Mode:  HostModeClaudePTY,
			State: HostStateLive,
			Label: "claude live",
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_claude",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
	}, UIState{
		ShowStatus: true,
		Session: SessionState{
			SessionID: "shs_claude",
		},
	}, host, 120, 32)

	if vm.Header.Worker != "claude live" {
		t.Fatalf("expected claude worker label, got %q", vm.Header.Worker)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundHostLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "host claude-pty / live / input live") {
			foundHostLine = true
		}
	}
	if !foundHostLine {
		t.Fatalf("expected claude host line, got %#v", vm.Overlay.Lines)
	}
}

func TestBuildViewModelSurfacesInterruptedRecoverableState(t *testing.T) {
	host := &stubHost{
		title:  "worker pane | codex transcript",
		worker: "codex last",
		status: HostStatus{
			Mode:      HostModeTranscript,
			State:     HostStateTranscriptOnly,
			Label:     "codex transcript",
			InputLive: false,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_interrupt",
		Phase:  "PAUSED",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:                 "INTERRUPTED_RUN_RECOVERABLE",
			Action:                "RESUME_INTERRUPTED_RUN",
			ReadyForNextRun:       true,
			ReadyForHandoffLaunch: false,
			Reason:                "latest run run_1 was interrupted and can be resumed safely",
		},
	}, UIState{
		ShowInspector: true,
		Session:       SessionState{SessionID: "shs_interrupt"},
	}, host, 120, 32)

	if vm.Header.Continuity != "recoverable" {
		t.Fatalf("expected recoverable continuity, got %q", vm.Header.Continuity)
	}
	if !strings.Contains(vm.WorkerPane.Lines[0], "interrupted recoverable | next resume interrupted run") {
		t.Fatalf("expected interrupted operator cue, got %#v", vm.WorkerPane.Lines)
	}
	if vm.Inspector == nil {
		t.Fatal("expected inspector")
	}
	joined := strings.Join(vm.Inspector.Sections[0].Lines, "\n")
	if !strings.Contains(joined, "readiness next-run yes | handoff-launch no") || !strings.Contains(joined, "reason latest run run_1 was interrupted") {
		t.Fatalf("expected interrupted recovery truth in operator section, got %q", joined)
	}
}

func TestBuildViewModelSurfacesAcceptedHandoffLaunchReadyState(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	host := &stubHost{
		title:  "worker pane | transcript",
		worker: "claude handoff",
		status: HostStatus{
			Mode:      HostModeTranscript,
			State:     HostStateTranscriptOnly,
			Label:     "transcript",
			InputLive: false,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_launch_ready",
		Phase:  "PAUSED",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:                 "ACCEPTED_HANDOFF_LAUNCH_READY",
			Action:                "LAUNCH_ACCEPTED_HANDOFF",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: true,
			Reason:                "accepted handoff hnd_1 is ready to launch for claude",
		},
		LaunchControl: &LaunchControlSummary{
			State:            "NOT_REQUESTED",
			RetryDisposition: "ALLOWED",
			Reason:           "accepted handoff hnd_1 is ready to launch for claude",
			HandoffID:        "hnd_1",
			TargetWorker:     "claude",
		},
		Handoff: &HandoffSummary{
			ID:           "hnd_1",
			Status:       "ACCEPTED",
			SourceWorker: "codex",
			TargetWorker: "claude",
			Mode:         "resume",
			CreatedAt:    now,
		},
	}, UIState{
		ShowInspector: true,
		ShowStatus:    true,
		Session:       SessionState{SessionID: "shs_launch_ready"},
	}, host, 120, 32)

	if vm.Header.Continuity != "handoff-ready" {
		t.Fatalf("expected handoff-ready continuity, got %q", vm.Header.Continuity)
	}
	if !strings.Contains(vm.WorkerPane.Lines[0], "accepted handoff launch ready | next launch accepted handoff") {
		t.Fatalf("expected worker pane operator cue, got %#v", vm.WorkerPane.Lines)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundReadiness := false
	foundLaunch := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "readiness next-run no | handoff-launch yes") {
			foundReadiness = true
		}
		if strings.Contains(line, "launch not requested | retry allowed") {
			foundLaunch = true
		}
	}
	if !foundReadiness || !foundLaunch {
		t.Fatalf("expected launch-ready operator lines, got %#v", vm.Overlay.Lines)
	}
}

func TestBuildViewModelSurfacesRepairRequiredReason(t *testing.T) {
	host := &stubHost{
		status: HostStatus{
			Mode:      HostModeTranscript,
			State:     HostStateTranscriptOnly,
			Label:     "transcript",
			InputLive: false,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_repair",
		Phase:  "BLOCKED",
		Status: "ACTIVE",
		Checkpoint: &CheckpointSummary{
			ID:          "chk_repair",
			IsResumable: true,
		},
		Recovery: &RecoverySummary{
			Class:                 "REPAIR_REQUIRED",
			Action:                "REPAIR_CONTINUITY",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: false,
			Reason:                "capsule references missing brief brf_missing",
			Issues:                []RecoveryIssue{{Code: "MISSING_BRIEF", Message: "capsule references missing brief brf_missing"}},
		},
	}, UIState{
		ShowInspector: true,
		Session:       SessionState{SessionID: "shs_repair"},
	}, host, 120, 32)

	if vm.Header.Continuity != "repair" {
		t.Fatalf("expected repair continuity, got %q", vm.Header.Continuity)
	}
	if !strings.Contains(vm.WorkerPane.Lines[0], "repair required | next repair continuity") {
		t.Fatalf("expected repair operator cue, got %#v", vm.WorkerPane.Lines)
	}
	if vm.Inspector == nil {
		t.Fatal("expected inspector")
	}
	if vm.Inspector.Sections[0].Title != "operator" {
		t.Fatalf("expected operator section first, got %q", vm.Inspector.Sections[0].Title)
	}
	joined := strings.Join(vm.Inspector.Sections[0].Lines, "\n")
	if !strings.Contains(joined, "state repair required") || !strings.Contains(joined, "reason capsule references missing brief brf_missing") {
		t.Fatalf("expected repair reason in operator section, got %q", joined)
	}
	checkpointJoined := ""
	for _, section := range vm.Inspector.Sections {
		if section.Title == "checkpoint" {
			checkpointJoined = strings.Join(section.Lines, "\n")
			break
		}
	}
	if !strings.Contains(checkpointJoined, "raw resumable yes") {
		t.Fatalf("expected checkpoint section to preserve raw resumable truth, got %q", checkpointJoined)
	}
}

func TestBuildViewModelSurfacesPendingLaunchBlockedRetry(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	host := NewTranscriptHost()
	host.markFallback("live worker exited; switched to transcript fallback")
	host.status.StateChangedAt = now
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_launch_pending",
		Phase:  "PAUSED",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:                 "HANDOFF_LAUNCH_PENDING_OUTCOME",
			Action:                "WAIT_FOR_LAUNCH_OUTCOME",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: false,
			Reason:                "handoff launch hlc_1 is still pending durable outcome",
		},
		Launch: &LaunchSummary{
			AttemptID:   "hlc_1",
			Status:      "REQUESTED",
			RequestedAt: now,
			Summary:     "launch requested",
		},
		LaunchControl: &LaunchControlSummary{
			State:            "REQUESTED_OUTCOME_UNKNOWN",
			RetryDisposition: "BLOCKED",
			Reason:           "handoff launch hlc_1 is still pending durable outcome",
			HandoffID:        "hnd_1",
			AttemptID:        "hlc_1",
			TargetWorker:     "claude",
			RequestedAt:      now,
		},
	}, UIState{
		ShowStatus: true,
		ObservedAt: now.Add(6 * time.Second),
		Session:    SessionState{SessionID: "shs_launch_pending"},
	}, host, 120, 32)

	if vm.Header.Continuity != "launch-pending" {
		t.Fatalf("expected launch-pending continuity, got %q", vm.Header.Continuity)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundRecovery := false
	foundNext := false
	foundLaunch := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "recovery launch pending") {
			foundRecovery = true
		}
		if strings.Contains(line, "next wait for launch outcome") {
			foundNext = true
		}
		if strings.Contains(line, "launch pending outcome unknown | retry blocked") {
			foundLaunch = true
		}
	}
	if !foundRecovery || !foundNext || !foundLaunch {
		t.Fatalf("expected pending-launch operator truth in overlay, got %#v", vm.Overlay.Lines)
	}
}

func TestBuildViewModelInspectorLaunchSectionUsesLaunchControlTruth(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	host := &stubHost{
		title:  "worker pane | transcript",
		worker: "claude handoff",
		status: HostStatus{
			Mode:      HostModeTranscript,
			State:     HostStateTranscriptOnly,
			Label:     "transcript",
			InputLive: false,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_launch",
		Phase:  "PAUSED",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:                 "HANDOFF_LAUNCH_COMPLETED",
			Action:                "MONITOR_LAUNCHED_HANDOFF",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: false,
			Reason:                "handoff hnd_1 already has durable completed launch launch_1",
		},
		Launch: &LaunchSummary{
			AttemptID:   "hlc_1",
			LaunchID:    "launch_1",
			Status:      "COMPLETED",
			RequestedAt: now,
			EndedAt:     now.Add(2 * time.Second),
		},
		LaunchControl: &LaunchControlSummary{
			State:            "COMPLETED",
			RetryDisposition: "BLOCKED",
			Reason:           "handoff hnd_1 already has durable completed launch launch_1",
			HandoffID:        "hnd_1",
			AttemptID:        "hlc_1",
			LaunchID:         "launch_1",
			TargetWorker:     "claude",
			RequestedAt:      now,
			CompletedAt:      now.Add(2 * time.Second),
		},
	}, UIState{
		ShowInspector: true,
	}, host, 120, 32)

	if vm.Header.Continuity != "launched" {
		t.Fatalf("expected launched continuity label, got %q", vm.Header.Continuity)
	}
	if vm.Inspector == nil {
		t.Fatal("expected inspector")
	}
	foundLaunchSection := false
	foundInvocationOnly := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "launch" {
			continue
		}
		foundLaunchSection = true
		for _, line := range section.Lines {
			if strings.Contains(line, "completed (invocation only) | retry blocked") {
				foundInvocationOnly = true
			}
		}
	}
	if !foundLaunchSection || !foundInvocationOnly {
		t.Fatalf("expected launch inspector section with invocation-only truth, got %#v", vm.Inspector.Sections)
	}
}

func TestBuildViewModelShowsAnotherKnownSession(t *testing.T) {
	host := &stubHost{
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateLive,
			Label:     "codex live",
			InputLive: true,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_known",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
	}, UIState{
		ShowStatus: true,
		Session: SessionState{
			SessionID: "shs_current",
			KnownSessions: []KnownShellSession{
				{SessionID: "shs_current", SessionClass: KnownShellSessionClassAttachable, Active: true},
				{SessionID: "shs_other", SessionClass: KnownShellSessionClassAttachable, Active: true, ResolvedWorker: WorkerPreferenceClaude, HostState: HostStateLive, LastUpdatedAt: time.Unix(1710000001, 0).UTC()},
			},
		},
	}, host, 120, 32)

	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundRegistryLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "another attachable shell session is known") {
			foundRegistryLine = true
		}
	}
	if !foundRegistryLine {
		t.Fatalf("expected registry summary line, got %#v", vm.Overlay.Lines)
	}
}

func TestBuildViewModelShowsStaleKnownSession(t *testing.T) {
	host := &stubHost{
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateLive,
			Label:     "codex live",
			InputLive: true,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_stale",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
	}, UIState{
		ShowStatus: true,
		Session: SessionState{
			SessionID: "shs_current",
			KnownSessions: []KnownShellSession{
				{SessionID: "shs_current", SessionClass: KnownShellSessionClassAttachable, Active: true},
				{SessionID: "shs_stale", SessionClass: KnownShellSessionClassStale, Active: true, ResolvedWorker: WorkerPreferenceClaude, HostState: HostStateLive, LastUpdatedAt: time.Unix(1710000001, 0).UTC()},
			},
		},
	}, host, 120, 32)

	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundRegistryLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "stale shell session is known") {
			foundRegistryLine = true
		}
	}
	if !foundRegistryLine {
		t.Fatalf("expected stale registry summary line, got %#v", vm.Overlay.Lines)
	}
}

func TestBuildViewModelShowsActiveUnattachableKnownSession(t *testing.T) {
	host := &stubHost{
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateLive,
			Label:     "codex live",
			InputLive: true,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_unattachable",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
	}, UIState{
		ShowStatus: true,
		Session: SessionState{
			SessionID: "shs_current",
			KnownSessions: []KnownShellSession{
				{SessionID: "shs_current", SessionClass: KnownShellSessionClassAttachable, Active: true},
				{SessionID: "shs_other", SessionClass: KnownShellSessionClassActiveUnattachable, Active: true, ResolvedWorker: WorkerPreferenceClaude, HostState: HostStateFallback, LastUpdatedAt: time.Unix(1710000001, 0).UTC()},
			},
		},
	}, host, 120, 32)

	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundRegistryLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "another active but non-attachable shell session is known") {
			foundRegistryLine = true
		}
	}
	if !foundRegistryLine {
		t.Fatalf("expected active-unattachable registry summary line, got %#v", vm.Overlay.Lines)
	}
}

func TestBuildViewModelShowsNoRepoLabels(t *testing.T) {
	host := &stubHost{
		status: HostStatus{
			Mode:      HostModeTranscript,
			State:     HostStateTranscriptOnly,
			Label:     "transcript",
			InputLive: false,
		},
	}
	vm := BuildViewModel(Snapshot{
		Phase:                   "SCRATCH_INTAKE",
		Status:                  "LOCAL_ONLY",
		IntentClass:             "scratch",
		IntentSummary:           "Use this local scratch session to plan work before cloning or initializing a repository.",
		LatestCanonicalResponse: "No git repository was detected. Tuku opened a local scratch and intake session instead of repo-backed continuity.",
	}, UIState{
		ShowStatus: true,
		Session: SessionState{
			SessionID: "shs_no_repo",
		},
	}, host, 120, 32)

	if vm.Header.TaskLabel != "no-task" {
		t.Fatalf("expected no-task header label, got %q", vm.Header.TaskLabel)
	}
	if vm.Header.Repo != "no-repo" {
		t.Fatalf("expected no-repo header label, got %q", vm.Header.Repo)
	}
	if vm.Header.Continuity != "local-only" {
		t.Fatalf("expected local-only continuity, got %q", vm.Header.Continuity)
	}
	if vm.Header.Worker != "scratch intake" {
		t.Fatalf("expected scratch intake worker label, got %q", vm.Header.Worker)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundTaskLine := false
	foundRepoLine := false
	foundContinuityLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "task no-task") {
			foundTaskLine = true
		}
		if strings.Contains(line, "repo no-repo") {
			foundRepoLine = true
		}
		if strings.Contains(line, "continuity local-only") {
			foundContinuityLine = true
		}
	}
	if !foundTaskLine || !foundRepoLine || !foundContinuityLine {
		t.Fatalf("expected no-repo overlay lines, got %#v", vm.Overlay.Lines)
	}
}

func TestBuildViewModelShowsPendingScratchAdoptionDraft(t *testing.T) {
	host := &stubHost{
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateLive,
			Label:     "codex live",
			InputLive: true,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_pending",
		Phase:  "INTAKE",
		Status: "ACTIVE",
		LocalScratch: &LocalScratchContext{
			RepoRoot: "/tmp/repo",
			Notes: []ConversationItem{
				{Role: "user", Body: "Plan project structure"},
			},
		},
	}, UIState{
		ShowInspector:            true,
		ShowStatus:               true,
		PendingTaskMessage:       "Explicitly adopt these local scratch intake notes into this repo-backed Tuku task:\n\n- Plan project structure",
		PendingTaskMessageSource: "local_scratch_adoption",
		Session: SessionState{
			SessionID: "shs_pending",
		},
	}, host, 120, 32)

	if vm.Inspector == nil {
		t.Fatal("expected inspector")
	}
	foundPendingSection := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "pending message" {
			continue
		}
		foundPendingSection = true
		joined := strings.Join(section.Lines, "\n")
		if !strings.Contains(joined, "Local draft is staged and ready for review.") {
			t.Fatalf("expected staged draft guidance, got %q", joined)
		}
	}
	if !foundPendingSection {
		t.Fatalf("expected pending message section, got %#v", vm.Inspector.Sections)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundPendingOverlayLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "draft staged draft from local scratch") {
			foundPendingOverlayLine = true
		}
	}
	if !foundPendingOverlayLine {
		t.Fatalf("expected pending message overlay line, got %#v", vm.Overlay.Lines)
	}
}

func TestBuildViewModelShowsPendingDraftEditMode(t *testing.T) {
	host := &stubHost{
		title:  "worker pane | codex live session",
		worker: "codex live",
		lines:  []string{"codex> hello"},
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateLive,
			Label:     "codex live",
			InputLive: true,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_editing",
		Phase:  "INTAKE",
		Status: "ACTIVE",
		LocalScratch: &LocalScratchContext{
			RepoRoot: "/tmp/repo",
			Notes: []ConversationItem{
				{Role: "user", Body: "Plan project structure"},
			},
		},
	}, UIState{
		ShowInspector:                true,
		ShowStatus:                   true,
		Focus:                        FocusWorker,
		PendingTaskMessage:           "Saved draft",
		PendingTaskMessageSource:     "local_scratch_adoption",
		PendingTaskMessageEditMode:   true,
		PendingTaskMessageEditBuffer: "Edited draft",
		Session: SessionState{
			SessionID: "shs_editing",
		},
	}, host, 120, 32)

	if vm.WorkerPane.Title != "worker pane | pending message editor" {
		t.Fatalf("expected worker pane edit title, got %q", vm.WorkerPane.Title)
	}
	joinedPane := strings.Join(vm.WorkerPane.Lines, "\n")
	if !strings.Contains(joinedPane, "Edited draft") {
		t.Fatalf("expected editor lines to show edited draft, got %q", joinedPane)
	}
	if vm.Inspector == nil {
		t.Fatal("expected inspector")
	}
	foundPendingSection := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "pending message" {
			continue
		}
		foundPendingSection = true
		joined := strings.Join(section.Lines, "\n")
		if !strings.Contains(joined, "Editing the staged local draft.") {
			t.Fatalf("expected edit-mode copy, got %q", joined)
		}
		if !strings.Contains(joined, "Nothing here is canonical until you explicitly send it with m.") {
			t.Fatalf("expected explicit local-only boundary, got %q", joined)
		}
	}
	if !foundPendingSection {
		t.Fatalf("expected pending message section, got %#v", vm.Inspector.Sections)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundPendingOverlayLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "draft editing staged draft from local scratch") {
			foundPendingOverlayLine = true
		}
	}
	if !foundPendingOverlayLine {
		t.Fatalf("expected edit-mode overlay line, got %#v", vm.Overlay.Lines)
	}
	if !strings.Contains(vm.Footer, "editing staged draft") {
		t.Fatalf("expected footer edit-mode hint, got %q", vm.Footer)
	}
}

func TestBuildViewModelShowsLocalScratchAvailableState(t *testing.T) {
	host := &stubHost{
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateLive,
			Label:     "codex live",
			InputLive: true,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_local_scratch",
		Phase:  "INTAKE",
		Status: "ACTIVE",
		LocalScratch: &LocalScratchContext{
			RepoRoot: "/tmp/repo",
			Notes: []ConversationItem{
				{Role: "user", Body: "Plan project structure"},
			},
		},
	}, UIState{
		ShowInspector: true,
		ShowStatus:    true,
		Session: SessionState{
			SessionID: "shs_local_scratch",
		},
	}, host, 120, 32)

	if vm.Inspector == nil {
		t.Fatal("expected inspector")
	}
	foundPendingSection := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "pending message" {
			continue
		}
		foundPendingSection = true
		joined := strings.Join(section.Lines, "\n")
		if !strings.Contains(joined, "Local scratch is available for explicit adoption.") {
			t.Fatalf("expected local scratch available copy, got %q", joined)
		}
	}
	if !foundPendingSection {
		t.Fatalf("expected pending message section, got %#v", vm.Inspector.Sections)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundPendingOverlayLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "draft local scratch available") {
			foundPendingOverlayLine = true
		}
	}
	if !foundPendingOverlayLine {
		t.Fatalf("expected local scratch available overlay line, got %#v", vm.Overlay.Lines)
	}
	if !strings.Contains(vm.Footer, "local scratch available") {
		t.Fatalf("expected footer local scratch hint, got %q", vm.Footer)
	}
}

func TestBuildViewModelCollapsesSecondaryChromeInNarrowTerminal(t *testing.T) {
	host := &stubHost{
		title:  "worker pane | codex live session",
		worker: "codex live",
		lines:  []string{"codex> hello"},
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateLive,
			Label:     "codex live",
			InputLive: true,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_narrow",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
	}, UIState{
		ShowInspector: true,
		ShowProof:     true,
		Focus:         FocusWorker,
		Session: SessionState{
			SessionID: "shs_narrow",
		},
	}, host, 100, 16)

	if vm.Inspector != nil {
		t.Fatalf("expected inspector to auto-collapse in narrow terminal, got %#v", vm.Inspector)
	}
	if vm.ProofStrip != nil {
		t.Fatalf("expected activity strip to auto-collapse in narrow terminal, got %#v", vm.ProofStrip)
	}
	if vm.Layout.showInspector || vm.Layout.showProof {
		t.Fatalf("expected collapsed layout flags, got %+v", vm.Layout)
	}
}

```
