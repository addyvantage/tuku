package brief

import (
	"time"

	"tuku/internal/domain/common"
)

type Verbosity string

const (
	VerbosityCompact  Verbosity = "compact"
	VerbosityStandard Verbosity = "standard"
	VerbosityVerbose  Verbosity = "verbose"
)

type ExecutionBrief struct {
	Version          int                 `json:"version"`
	BriefID          common.BriefID      `json:"brief_id"`
	TaskID           common.TaskID       `json:"task_id"`
	IntentID         common.IntentID     `json:"intent_id"`
	CapsuleVersion   common.CapsuleVersion `json:"capsule_version"`
	CreatedAt        time.Time           `json:"created_at"`
	Objective        string              `json:"objective"`
	NormalizedAction string              `json:"normalized_action"`
	ScopeIn          []string            `json:"scope_in"`
	ScopeOut         []string            `json:"scope_out"`
	Constraints      []string            `json:"constraints"`
	DoneCriteria     []string            `json:"done_criteria"`
	ContextPackID    common.ContextPackID `json:"context_pack_id"`
	Verbosity        Verbosity           `json:"verbosity"`
	PolicyProfileID  string              `json:"policy_profile_id"`
	BriefHash        string              `json:"brief_hash"`
}

type Builder interface {
	Build(input BuildInput) (ExecutionBrief, error)
}

type BuildInput struct {
	TaskID          common.TaskID
	IntentID        common.IntentID
	CapsuleVersion  common.CapsuleVersion
	Goal            string
	NormalizedAction string
	Constraints     []string
	ScopeHints      []string
	ScopeOutHints   []string
	DoneCriteria    []string
	ContextPackID   common.ContextPackID
	Verbosity       Verbosity
	PolicyProfileID string
}
