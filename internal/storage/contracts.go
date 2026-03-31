package storage

import (
	"time"

	"tuku/internal/domain/benchmark"
	"tuku/internal/domain/brief"
	"tuku/internal/domain/capsule"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	contextdomain "tuku/internal/domain/context"
	"tuku/internal/domain/conversation"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/incidenttriage"
	"tuku/internal/domain/intent"
	"tuku/internal/domain/operatorstep"
	"tuku/internal/domain/policy"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/recoveryaction"
	"tuku/internal/domain/run"
	"tuku/internal/domain/taskmemory"
	"tuku/internal/domain/transition"
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
	ListByTask(taskID common.TaskID, limit int) ([]run.ExecutionRun, error)
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
	ListByTask(taskID common.TaskID, limit int) ([]handoff.Packet, error)
	UpdateStatus(taskID common.TaskID, handoffID string, status handoff.Status, acceptedBy run.WorkerKind, notes []string, at time.Time) error
	CreateLaunch(launch handoff.Launch) error
	GetLaunch(attemptID string) (handoff.Launch, error)
	LatestLaunchByHandoff(handoffID string) (handoff.Launch, error)
	UpdateLaunch(launch handoff.Launch) error
	SaveAcknowledgment(ack handoff.Acknowledgment) error
	LatestAcknowledgment(handoffID string) (handoff.Acknowledgment, error)
	SaveFollowThrough(record handoff.FollowThrough) error
	LatestFollowThrough(handoffID string) (handoff.FollowThrough, error)
	SaveResolution(record handoff.Resolution) error
	LatestResolution(handoffID string) (handoff.Resolution, error)
	LatestResolutionByTask(taskID common.TaskID) (handoff.Resolution, error)
}

type RecoveryActionStore interface {
	Create(record recoveryaction.Record) error
	LatestByTask(taskID common.TaskID) (recoveryaction.Record, error)
	ListByTask(taskID common.TaskID, limit int) ([]recoveryaction.Record, error)
}

type OperatorStepReceiptStore interface {
	Create(record operatorstep.Receipt) error
	LatestByTask(taskID common.TaskID) (operatorstep.Receipt, error)
	ListByTask(taskID common.TaskID, limit int) ([]operatorstep.Receipt, error)
}

type TransitionReceiptStore interface {
	Create(record transition.Receipt) error
	GetByTaskReceipt(taskID common.TaskID, receiptID common.EventID) (transition.Receipt, error)
	LatestByTask(taskID common.TaskID) (transition.Receipt, error)
	ListByTask(taskID common.TaskID, limit int) ([]transition.Receipt, error)
	ListByTaskFiltered(taskID common.TaskID, filter transition.ReceiptListFilter) ([]transition.Receipt, error)
	ListByTaskAfter(taskID common.TaskID, afterReceiptID common.EventID, afterCreatedAt time.Time, limit int) ([]transition.Receipt, error)
}

type IncidentTriageStore interface {
	Create(record incidenttriage.Receipt) error
	GetByTaskReceipt(taskID common.TaskID, receiptID common.EventID) (incidenttriage.Receipt, error)
	LatestByTask(taskID common.TaskID) (incidenttriage.Receipt, error)
	LatestByTaskAnchor(taskID common.TaskID, anchorTransitionReceiptID common.EventID) (incidenttriage.Receipt, error)
	ListByTask(taskID common.TaskID, limit int) ([]incidenttriage.Receipt, error)
	ListByTaskFiltered(taskID common.TaskID, filter incidenttriage.ReceiptListFilter) ([]incidenttriage.Receipt, error)
}

type IncidentFollowUpStore interface {
	Create(record incidenttriage.FollowUpReceipt) error
	GetByTaskReceipt(taskID common.TaskID, receiptID common.EventID) (incidenttriage.FollowUpReceipt, error)
	LatestByTask(taskID common.TaskID) (incidenttriage.FollowUpReceipt, error)
	LatestByTaskAnchor(taskID common.TaskID, anchorTransitionReceiptID common.EventID) (incidenttriage.FollowUpReceipt, error)
	ListByTask(taskID common.TaskID, limit int) ([]incidenttriage.FollowUpReceipt, error)
	ListByTaskFiltered(taskID common.TaskID, filter incidenttriage.FollowUpReceiptListFilter) ([]incidenttriage.FollowUpReceipt, error)
}

type ContextPackStore interface {
	Save(pack contextdomain.Pack) error
	Get(id common.ContextPackID) (contextdomain.Pack, error)
}

type TaskMemoryStore interface {
	Save(snapshot taskmemory.Snapshot) error
	Get(memoryID common.MemoryID) (taskmemory.Snapshot, error)
	LatestByTask(taskID common.TaskID) (taskmemory.Snapshot, error)
}

type BenchmarkStore interface {
	Save(run benchmark.Run) error
	Get(benchmarkID common.BenchmarkID) (benchmark.Run, error)
	LatestByTask(taskID common.TaskID) (benchmark.Run, error)
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
	OperatorStepReceipts() OperatorStepReceiptStore
	TransitionReceipts() TransitionReceiptStore
	IncidentTriages() IncidentTriageStore
	IncidentFollowUps() IncidentFollowUpStore
	ContextPacks() ContextPackStore
	TaskMemories() TaskMemoryStore
	Benchmarks() BenchmarkStore
	PolicyDecisions() PolicyDecisionStore
	WithTx(fn func(Store) error) error
}
