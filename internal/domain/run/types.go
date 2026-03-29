package run

import (
	"time"

	"tuku/internal/domain/brief"
	"tuku/internal/domain/common"
	"tuku/internal/domain/phase"
)

type WorkerKind string

const (
	WorkerKindUnknown WorkerKind = "unknown"
	WorkerKindCodex   WorkerKind = "codex"
	WorkerKindClaude  WorkerKind = "claude"
	WorkerKindNoop    WorkerKind = "noop"
)

type Status string

const (
	StatusCreated     Status = "CREATED"
	StatusRunning     Status = "RUNNING"
	StatusInterrupted Status = "INTERRUPTED"
	StatusCompleted   Status = "COMPLETED"
	StatusFailed      Status = "FAILED"
)

// ExecutionRun is the explicit run lifecycle record for Execute/Record stages.
type ExecutionRun struct {
	RunID                 common.RunID   `json:"run_id"`
	TaskID                common.TaskID  `json:"task_id"`
	BriefID               common.BriefID `json:"brief_id"`
	WorkerKind            WorkerKind     `json:"worker_kind"`
	WorkerRunID           string         `json:"worker_run_id,omitempty"`
	ShellSessionID        string         `json:"shell_session_id,omitempty"`
	Status                Status         `json:"status"`
	Command               string         `json:"command,omitempty"`
	Args                  []string       `json:"args,omitempty"`
	ExitCode              *int           `json:"exit_code,omitempty"`
	Stdout                string         `json:"stdout,omitempty"`
	Stderr                string         `json:"stderr,omitempty"`
	ChangedFiles          []string       `json:"changed_files,omitempty"`
	ChangedFilesSemantics string         `json:"changed_files_semantics,omitempty"`
	ValidationSignals     []string       `json:"validation_signals,omitempty"`
	OutputArtifactRef     string         `json:"output_artifact_ref,omitempty"`
	StructuredSummary     string         `json:"structured_summary,omitempty"`
	StartedAt             time.Time      `json:"started_at"`
	EndedAt               *time.Time     `json:"ended_at,omitempty"`
	InterruptionReason    string         `json:"interruption_reason,omitempty"`
	CreatedFromPhase      phase.Phase    `json:"created_from_phase"`
	LastKnownSummary      string         `json:"last_known_summary,omitempty"`
	CreatedAt             time.Time      `json:"created_at"`
	UpdatedAt             time.Time      `json:"updated_at"`
}

type Repository interface {
	Create(run ExecutionRun) error
	Get(runID common.RunID) (ExecutionRun, error)
	LatestByTask(taskID common.TaskID) (ExecutionRun, error)
	LatestRunningByTask(taskID common.TaskID) (ExecutionRun, error)
	Update(run ExecutionRun) error
}

// StartInput keeps run initiation deterministic and scoped to latest brief.
type StartInput struct {
	TaskID      common.TaskID
	Brief       brief.ExecutionBrief
	WorkerKind  WorkerKind
	SourcePhase phase.Phase
	Now         time.Time
	RunID       common.RunID
}
