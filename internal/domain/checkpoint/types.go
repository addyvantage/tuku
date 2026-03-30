package checkpoint

import (
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/phase"
)

type Trigger string

const (
	TriggerBeforeExecution  Trigger = "BEFORE_EXECUTION"
	TriggerAfterExecution   Trigger = "AFTER_EXECUTION"
	TriggerInterruption     Trigger = "INTERRUPTION"
	TriggerManual           Trigger = "MANUAL"
	TriggerContinue         Trigger = "CONTINUE"
	TriggerHandoff          Trigger = "HANDOFF"
	TriggerAwaitingDecision Trigger = "AWAITING_DECISION"
)

type DriftClass string

const (
	DriftNone  DriftClass = "NONE"
	DriftMinor DriftClass = "MINOR"
	DriftMajor DriftClass = "MAJOR"
)

type RepoAnchor struct {
	RepoRoot      string `json:"repo_root"`
	WorktreePath  string `json:"worktree_path"`
	BranchName    string `json:"branch_name"`
	HeadSHA       string `json:"head_sha"`
	DirtyHash     string `json:"dirty_hash"`
	UntrackedHash string `json:"untracked_hash"`
}

type Checkpoint struct {
	Version            int                   `json:"version"`
	CheckpointID       common.CheckpointID   `json:"checkpoint_id"`
	TaskID             common.TaskID         `json:"task_id"`
	RunID              common.RunID          `json:"run_id,omitempty"`
	CreatedAt          time.Time             `json:"created_at"`
	Trigger            Trigger               `json:"trigger"`
	CapsuleVersion     common.CapsuleVersion `json:"capsule_version"`
	Phase              phase.Phase           `json:"phase"`
	Anchor             RepoAnchor            `json:"anchor"`
	IntentID           common.IntentID       `json:"intent_id"`
	BriefID            common.BriefID        `json:"brief_id"`
	ContextPackID      common.ContextPackID  `json:"context_pack_id"`
	LastEventID        common.EventID        `json:"last_event_id"`
	PendingDecisionIDs []common.DecisionID   `json:"pending_decision_ids"`
	ResumeDescriptor   string                `json:"resume_descriptor"`
	IsResumable        bool                  `json:"is_resumable"`
}

type Repository interface {
	Create(c Checkpoint) error
	Get(checkpointID common.CheckpointID) (Checkpoint, error)
	LatestByTask(taskID common.TaskID) (Checkpoint, error)
}
