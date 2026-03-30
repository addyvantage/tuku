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

type ResolutionKind string

const (
	ResolutionAbandoned         ResolutionKind = "ABANDONED"
	ResolutionSupersededByLocal ResolutionKind = "SUPERSEDED_BY_LOCAL"
	ResolutionClosedUnproven    ResolutionKind = "CLOSED_UNPROVEN"
	ResolutionReviewedStale     ResolutionKind = "REVIEWED_STALE"
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

// Resolution is a bounded durable artifact closing an active Claude handoff branch without claiming downstream completion.
type Resolution struct {
	Version int `json:"version"`

	ResolutionID    string         `json:"resolution_id"`
	HandoffID       string         `json:"handoff_id"`
	LaunchAttemptID string         `json:"launch_attempt_id,omitempty"`
	LaunchID        string         `json:"launch_id,omitempty"`
	TaskID          common.TaskID  `json:"task_id"`
	TargetWorker    run.WorkerKind `json:"target_worker"`
	Kind            ResolutionKind `json:"kind"`
	Summary         string         `json:"summary"`
	Notes           []string       `json:"notes,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
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
	ListByTask(taskID common.TaskID, limit int) ([]Packet, error)
	UpdateStatus(taskID common.TaskID, handoffID string, status Status, acceptedBy run.WorkerKind, notes []string, at time.Time) error
	CreateLaunch(launch Launch) error
	GetLaunch(attemptID string) (Launch, error)
	LatestLaunchByHandoff(handoffID string) (Launch, error)
	UpdateLaunch(launch Launch) error
	SaveAcknowledgment(ack Acknowledgment) error
	LatestAcknowledgment(handoffID string) (Acknowledgment, error)
	SaveFollowThrough(record FollowThrough) error
	LatestFollowThrough(handoffID string) (FollowThrough, error)
	SaveResolution(record Resolution) error
	LatestResolution(handoffID string) (Resolution, error)
	LatestResolutionByTask(taskID common.TaskID) (Resolution, error)
}
