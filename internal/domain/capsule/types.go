package capsule

import (
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/phase"
)

// WorkCapsule is the v1 durable task object.
// Design note: IDs and references are graph-friendly for later Task Graph evolution.
type WorkCapsule struct {
	TaskID         common.TaskID         `json:"task_id"`
	ConversationID common.ConversationID `json:"conversation_id"`
	Version        common.CapsuleVersion `json:"version"`
	CreatedAt      time.Time             `json:"created_at"`
	UpdatedAt      time.Time             `json:"updated_at"`

	Goal               string   `json:"goal"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	Constraints        []string `json:"constraints"`

	RepoRoot         string    `json:"repo_root"`
	WorktreePath     string    `json:"worktree_path"`
	BranchName       string    `json:"branch_name"`
	HeadSHA          string    `json:"head_sha"`
	WorkingTreeDirty bool      `json:"working_tree_dirty"`
	AnchorCapturedAt time.Time `json:"anchor_captured_at"`

	CurrentPhase phase.Phase `json:"current_phase"`
	Status       string      `json:"status"`

	CurrentIntentID common.IntentID `json:"current_intent_id"`
	CurrentBriefID  common.BriefID  `json:"current_brief_id"`

	TouchedFiles []string `json:"touched_files"`
	Blockers     []string `json:"blockers"`
	NextAction   string   `json:"next_action"`

	// Future graph-friendly references (not active graph runtime in v1).
	ParentTaskID *common.TaskID  `json:"parent_task_id,omitempty"`
	ChildTaskIDs []common.TaskID `json:"child_task_ids,omitempty"`
	EdgeRefs     []string        `json:"edge_refs,omitempty"`
}

// LayeredStateRefs explicitly mirrors Tuku layered state model in v1.
type LayeredStateRefs struct {
	RawConversationMessageIDs []common.MessageID    `json:"raw_conversation_message_ids"`
	TaskMemoryVersion         common.CapsuleVersion `json:"task_memory_version"`
	CurrentIntentID           common.IntentID       `json:"current_intent_id"`
	CurrentBriefID            common.BriefID        `json:"current_brief_id"`
	LastProofEventID          common.EventID        `json:"last_proof_event_id"`
}

type Repository interface {
	Create(c WorkCapsule) error
	Get(taskID common.TaskID) (WorkCapsule, error)
	LatestByRepoRoot(repoRoot string) (WorkCapsule, error)
	Update(c WorkCapsule) error
}
