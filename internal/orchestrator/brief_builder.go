package orchestrator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"tuku/internal/domain/brief"
	"tuku/internal/domain/common"
)

// BriefBuilderV1 produces deterministic execution briefs for milestone 2.
type BriefBuilderV1 struct {
	idGenerator func(prefix string) string
	clock       func() time.Time
}

func NewBriefBuilderV1(idGenerator func(prefix string) string, clock func() time.Time) *BriefBuilderV1 {
	if idGenerator == nil {
		idGenerator = newID
	}
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &BriefBuilderV1{idGenerator: idGenerator, clock: clock}
}

func (b *BriefBuilderV1) Build(input brief.BuildInput) (brief.ExecutionBrief, error) {
	payload := struct {
		Version          int                 `json:"version"`
		TaskID           common.TaskID       `json:"task_id"`
		IntentID         common.IntentID     `json:"intent_id"`
		CapsuleVersion   common.CapsuleVersion `json:"capsule_version"`
		Goal             string              `json:"goal"`
		NormalizedAction string              `json:"normalized_action"`
		ScopeIn          []string            `json:"scope_in"`
		ScopeOut         []string            `json:"scope_out"`
		Constraints      []string            `json:"constraints"`
		DoneCriteria     []string            `json:"done_criteria"`
		ContextPackID    common.ContextPackID `json:"context_pack_id"`
		Verbosity        brief.Verbosity     `json:"verbosity"`
		PolicyProfileID  string              `json:"policy_profile_id"`
	}{
		Version:          1,
		TaskID:           input.TaskID,
		IntentID:         input.IntentID,
		CapsuleVersion:   input.CapsuleVersion,
		Goal:             input.Goal,
		NormalizedAction: input.NormalizedAction,
		ScopeIn:          append([]string{}, input.ScopeHints...),
		ScopeOut:         append([]string{}, input.ScopeOutHints...),
		Constraints:      append([]string{}, input.Constraints...),
		DoneCriteria:     append([]string{}, input.DoneCriteria...),
		ContextPackID:    input.ContextPackID,
		Verbosity:        input.Verbosity,
		PolicyProfileID:  input.PolicyProfileID,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return brief.ExecutionBrief{}, fmt.Errorf("marshal brief hash payload: %w", err)
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])

	objective := input.Goal
	if input.NormalizedAction != "" {
		objective = fmt.Sprintf("%s; action: %s", input.Goal, input.NormalizedAction)
	}

	out := brief.ExecutionBrief{
		Version:          1,
		BriefID:          common.BriefID(b.idGenerator("brf")),
		TaskID:           input.TaskID,
		IntentID:         input.IntentID,
		CapsuleVersion:   input.CapsuleVersion,
		CreatedAt:        b.clock(),
		Objective:        objective,
		NormalizedAction: input.NormalizedAction,
		ScopeIn:          append([]string{}, input.ScopeHints...),
		ScopeOut:         append([]string{}, input.ScopeOutHints...),
		Constraints:      append([]string{}, input.Constraints...),
		DoneCriteria:     append([]string{}, input.DoneCriteria...),
		ContextPackID:    input.ContextPackID,
		Verbosity:        input.Verbosity,
		PolicyProfileID:  input.PolicyProfileID,
		BriefHash:        hash,
	}
	return out, nil
}
