package adapter_contract

import (
	"context"
	"time"

	"tuku/internal/domain/brief"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	contextdomain "tuku/internal/domain/context"
	"tuku/internal/domain/handoff"
)

type WorkerKind string

const (
	WorkerUnknown WorkerKind = "unknown"
	WorkerCodex   WorkerKind = "codex"
	WorkerClaude  WorkerKind = "claude"
)

type ExecutionRequest struct {
	RunID              common.RunID          `json:"run_id"`
	TaskID             common.TaskID         `json:"task_id"`
	Worker             WorkerKind            `json:"worker"`
	Brief              brief.ExecutionBrief  `json:"brief"`
	ContextPack        contextdomain.Pack    `json:"context_pack"`
	RepoAnchor         checkpoint.RepoAnchor `json:"repo_anchor"`
	PolicyProfileID    string                `json:"policy_profile_id"`
	AgentsChecksum     string                `json:"agents_checksum"`
	AgentsInstructions string                `json:"agents_instructions,omitempty"`
	ContextSummary     string                `json:"context_summary,omitempty"`
}

type ExecutionResult struct {
	WorkerRunID           common.WorkerRunID `json:"worker_run_id"`
	ExitCode              int                `json:"exit_code"`
	StartedAt             time.Time          `json:"started_at"`
	EndedAt               time.Time          `json:"ended_at"`
	Command               string             `json:"command"`
	Args                  []string           `json:"args"`
	Stdout                string             `json:"stdout"`
	Stderr                string             `json:"stderr"`
	ChangedFiles          []string           `json:"changed_files,omitempty"`
	ChangedFilesSemantics string             `json:"changed_files_semantics,omitempty"`
	ValidationSignals     []string           `json:"validation_signals,omitempty"`
	OutputArtifactRef     string             `json:"output_artifact_ref"`
	Summary               string             `json:"summary"`
	StructuredSummary     string             `json:"structured_summary,omitempty"`
	ErrorMessage          string             `json:"error_message,omitempty"`
}

type WorkerEventType string

const (
	WorkerEventStarted   WorkerEventType = "STARTED"
	WorkerEventOutput    WorkerEventType = "OUTPUT"
	WorkerEventCommand   WorkerEventType = "COMMAND"
	WorkerEventCompleted WorkerEventType = "COMPLETED"
	WorkerEventFailed    WorkerEventType = "FAILED"
)

type WorkerEvent struct {
	Type    WorkerEventType `json:"type"`
	RunID   common.RunID    `json:"run_id"`
	Payload string          `json:"payload"`
}

type WorkerEventSink interface {
	OnWorkerEvent(ctx context.Context, event WorkerEvent) error
}

type WorkerAdapter interface {
	Name() WorkerKind
	Execute(ctx context.Context, req ExecutionRequest, sink WorkerEventSink) (ExecutionResult, error)
}

type HandoffLaunchRequest struct {
	TaskID       common.TaskID         `json:"task_id"`
	HandoffID    string                `json:"handoff_id"`
	SourceWorker WorkerKind            `json:"source_worker"`
	TargetWorker WorkerKind            `json:"target_worker"`
	Payload      handoff.LaunchPayload `json:"payload"`
}

type HandoffLaunchResult struct {
	LaunchID          string     `json:"launch_id"`
	TargetWorker      WorkerKind `json:"target_worker"`
	StartedAt         time.Time  `json:"started_at"`
	EndedAt           time.Time  `json:"ended_at"`
	Command           string     `json:"command"`
	Args              []string   `json:"args"`
	ExitCode          int        `json:"exit_code"`
	Stdout            string     `json:"stdout"`
	Stderr            string     `json:"stderr"`
	OutputArtifactRef string     `json:"output_artifact_ref,omitempty"`
	Summary           string     `json:"summary"`
	ErrorMessage      string     `json:"error_message,omitempty"`
}

type HandoffLauncher interface {
	Name() WorkerKind
	LaunchHandoff(ctx context.Context, req HandoffLaunchRequest) (HandoffLaunchResult, error)
}
