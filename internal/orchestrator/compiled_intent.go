package orchestrator

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/intent"
)

type CompiledIntentSummary struct {
	IntentID                common.IntentID  `json:"intent_id"`
	Class                   intent.Class     `json:"class"`
	Posture                 intent.Posture   `json:"posture"`
	ExecutionReadiness      intent.Readiness `json:"execution_readiness"`
	Objective               string           `json:"objective,omitempty"`
	RequestedOutcome        string           `json:"requested_outcome,omitempty"`
	NormalizedAction        string           `json:"normalized_action,omitempty"`
	ScopeSummary            string           `json:"scope_summary,omitempty"`
	ExplicitConstraints     []string         `json:"explicit_constraints,omitempty"`
	DoneCriteria            []string         `json:"done_criteria,omitempty"`
	AmbiguityFlags          []string         `json:"ambiguity_flags,omitempty"`
	ClarificationQuestions  []string         `json:"clarification_questions,omitempty"`
	RequiresClarification   bool             `json:"requires_clarification"`
	BoundedEvidenceMessages int              `json:"bounded_evidence_messages"`
	ReadinessReason         string           `json:"readiness_reason,omitempty"`
	CompilationNotes        string           `json:"compilation_notes,omitempty"`
	CreatedAt               time.Time        `json:"created_at,omitempty"`
	Digest                  string           `json:"digest,omitempty"`
	Advisory                string           `json:"advisory,omitempty"`
}

type ReadCompiledIntentRequest struct {
	TaskID string
}

type ReadCompiledIntentResult struct {
	TaskID          common.TaskID
	CurrentIntentID common.IntentID
	Bounded         bool
	Intent          *intent.State
	CompiledIntent  *CompiledIntentSummary
}

func (c *Coordinator) ReadCompiledIntent(ctx context.Context, req ReadCompiledIntentRequest) (ReadCompiledIntentResult, error) {
	_ = ctx
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return ReadCompiledIntentResult{}, fmt.Errorf("task id is required")
	}
	caps, err := c.store.Capsules().Get(taskID)
	if err != nil {
		return ReadCompiledIntentResult{}, err
	}
	out := ReadCompiledIntentResult{
		TaskID:          taskID,
		CurrentIntentID: caps.CurrentIntentID,
		Bounded:         true,
	}
	latest, err := c.store.Intents().LatestByTask(taskID)
	if err != nil {
		if err == sql.ErrNoRows {
			return out, nil
		}
		return ReadCompiledIntentResult{}, err
	}
	latestCopy := latest
	out.Intent = &latestCopy
	out.CompiledIntent = compiledIntentSummaryFromState(latestCopy)
	return out, nil
}

func (c *Coordinator) compiledIntentProjection(taskID common.TaskID) (*CompiledIntentSummary, error) {
	in, err := c.store.Intents().LatestByTask(taskID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return compiledIntentSummaryFromState(in), nil
}

func compiledIntentSummaryFromState(st intent.State) *CompiledIntentSummary {
	out := &CompiledIntentSummary{
		IntentID:                st.IntentID,
		Class:                   st.Class,
		Posture:                 st.Posture,
		ExecutionReadiness:      st.ExecutionReadiness,
		Objective:               st.Objective,
		RequestedOutcome:        st.RequestedOutcome,
		NormalizedAction:        st.NormalizedAction,
		ScopeSummary:            st.ScopeSummary,
		ExplicitConstraints:     append([]string{}, st.ExplicitConstraints...),
		DoneCriteria:            append([]string{}, st.DoneCriteria...),
		AmbiguityFlags:          append([]string{}, st.AmbiguityFlags...),
		ClarificationQuestions:  append([]string{}, st.ClarificationQuestions...),
		RequiresClarification:   st.RequiresClarification,
		BoundedEvidenceMessages: st.BoundedEvidenceMessages,
		ReadinessReason:         st.ReadinessReason,
		CompilationNotes:        st.CompilationNotes,
		CreatedAt:               st.CreatedAt,
	}
	switch st.ExecutionReadiness {
	case intent.ReadinessExecutionReady:
		out.Digest = "execution-ready intent in bounded recent evidence"
		out.Advisory = "Intent appears execution-ready within bounded recent evidence."
	case intent.ReadinessPlanningInProgress:
		out.Digest = "planning intent posture in bounded recent evidence"
		out.Advisory = "Intent remains planning-focused in bounded recent evidence."
	case intent.ReadinessValidationFocused:
		out.Digest = "validation-focused intent posture in bounded recent evidence"
		out.Advisory = "Intent is validation-focused in bounded recent evidence."
	case intent.ReadinessRepairRecovery:
		out.Digest = "repair/recovery-focused intent posture in bounded recent evidence"
		out.Advisory = "Intent is repair/recovery-focused in bounded recent evidence."
	default:
		out.Digest = "clarification-needed intent posture in bounded recent evidence"
		out.Advisory = "Intent remains clarification-needed in bounded recent evidence."
	}
	if out.RequiresClarification && len(out.ClarificationQuestions) > 0 {
		out.Advisory = "Intent remains clarification-needed in bounded recent evidence; unresolved clarification questions remain explicit."
	}
	return out
}
