package orchestrator

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"tuku/internal/domain/brief"
	"tuku/internal/domain/capsule"
	"tuku/internal/domain/common"
	"tuku/internal/domain/intent"
)

type CompiledBriefSummary struct {
	BriefID                 common.BriefID  `json:"brief_id"`
	IntentID                common.IntentID `json:"intent_id"`
	Posture                 brief.Posture   `json:"posture"`
	Objective               string          `json:"objective,omitempty"`
	RequestedOutcome        string          `json:"requested_outcome,omitempty"`
	NormalizedAction        string          `json:"normalized_action,omitempty"`
	ScopeSummary            string          `json:"scope_summary,omitempty"`
	Constraints             []string        `json:"constraints,omitempty"`
	DoneCriteria            []string        `json:"done_criteria,omitempty"`
	AmbiguityFlags          []string        `json:"ambiguity_flags,omitempty"`
	ClarificationQuestions  []string        `json:"clarification_questions,omitempty"`
	RequiresClarification   bool            `json:"requires_clarification"`
	WorkerFraming           string          `json:"worker_framing,omitempty"`
	BoundedEvidenceMessages int             `json:"bounded_evidence_messages"`
	CreatedAt               time.Time       `json:"created_at,omitempty"`
	Digest                  string          `json:"digest,omitempty"`
	Advisory                string          `json:"advisory,omitempty"`
}

type ReadGeneratedBriefRequest struct {
	TaskID string
}

type ReadGeneratedBriefResult struct {
	TaskID         common.TaskID
	CurrentBriefID common.BriefID
	Bounded        bool
	Brief          *brief.ExecutionBrief
	CompiledBrief  *CompiledBriefSummary
}

func (c *Coordinator) ReadGeneratedBrief(ctx context.Context, req ReadGeneratedBriefRequest) (ReadGeneratedBriefResult, error) {
	_ = ctx
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return ReadGeneratedBriefResult{}, fmt.Errorf("task id is required")
	}
	caps, err := c.store.Capsules().Get(taskID)
	if err != nil {
		return ReadGeneratedBriefResult{}, err
	}
	out := ReadGeneratedBriefResult{
		TaskID:         taskID,
		CurrentBriefID: caps.CurrentBriefID,
		Bounded:        true,
	}
	if caps.CurrentBriefID == "" {
		return out, nil
	}
	b, err := c.store.Briefs().Get(caps.CurrentBriefID)
	if err != nil {
		if err == sql.ErrNoRows {
			return out, nil
		}
		return ReadGeneratedBriefResult{}, err
	}
	bCopy := b
	out.Brief = &bCopy
	out.CompiledBrief = compiledBriefSummaryFromBrief(bCopy)
	return out, nil
}

func (c *Coordinator) compiledBriefProjection(taskID common.TaskID, currentBriefID common.BriefID) (*CompiledBriefSummary, error) {
	_ = taskID
	if strings.TrimSpace(string(currentBriefID)) == "" {
		return nil, nil
	}
	b, err := c.store.Briefs().Get(currentBriefID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return compiledBriefSummaryFromBrief(b), nil
}

func compiledBriefSummaryFromBrief(b brief.ExecutionBrief) *CompiledBriefSummary {
	out := &CompiledBriefSummary{
		BriefID:                 b.BriefID,
		IntentID:                b.IntentID,
		Posture:                 b.Posture,
		Objective:               b.Objective,
		RequestedOutcome:        b.RequestedOutcome,
		NormalizedAction:        b.NormalizedAction,
		ScopeSummary:            b.ScopeSummary,
		Constraints:             append([]string{}, b.Constraints...),
		DoneCriteria:            append([]string{}, b.DoneCriteria...),
		AmbiguityFlags:          append([]string{}, b.AmbiguityFlags...),
		ClarificationQuestions:  append([]string{}, b.ClarificationQuestions...),
		RequiresClarification:   b.RequiresClarification,
		WorkerFraming:           b.WorkerFraming,
		BoundedEvidenceMessages: b.BoundedEvidenceMessages,
		CreatedAt:               b.CreatedAt,
	}
	posture := normalizedBriefPosture(b.Posture, b.RequiresClarification)
	out.Posture = posture
	switch posture {
	case brief.PostureExecutionReady:
		out.Digest = "execution-ready brief posture in bounded recent evidence"
		out.Advisory = "Brief is execution-ready within bounded recent evidence."
	case brief.PosturePlanningOriented:
		out.Digest = "planning-oriented brief posture in bounded recent evidence"
		out.Advisory = "Brief remains planning-oriented in bounded recent evidence."
	case brief.PostureValidationOriented:
		out.Digest = "validation-oriented brief posture in bounded recent evidence"
		out.Advisory = "Brief is validation-oriented in bounded recent evidence."
	case brief.PostureRepairOriented:
		out.Digest = "repair-oriented brief posture in bounded recent evidence"
		out.Advisory = "Brief is repair-oriented in bounded recent evidence."
	default:
		out.Digest = "clarification-needed brief posture in bounded recent evidence"
		out.Advisory = "Brief remains clarification-needed in bounded recent evidence."
	}
	if out.RequiresClarification && len(out.ClarificationQuestions) > 0 {
		out.Advisory = "Brief remains clarification-needed in bounded recent evidence; unresolved clarification questions remain explicit."
	}
	return out
}

func buildBriefInputV2(caps capsule.WorkCapsule, in intent.State, previous *brief.ExecutionBrief, nextCapsuleVersion common.CapsuleVersion) brief.BuildInput {
	posture := deriveBriefPostureFromIntent(in)
	goal := strings.TrimSpace(in.Objective)
	if goal == "" {
		goal = strings.TrimSpace(caps.Goal)
	}
	if goal == "" && previous != nil {
		goal = strings.TrimSpace(previous.Objective)
	}

	normalizedAction := strings.TrimSpace(in.NormalizedAction)
	if normalizedAction == "" && previous != nil {
		normalizedAction = strings.TrimSpace(previous.NormalizedAction)
	}
	if normalizedAction == "" {
		normalizedAction = "prepare bounded next step"
	}

	requestedOutcome := strings.TrimSpace(in.RequestedOutcome)
	if requestedOutcome == "" && previous != nil {
		requestedOutcome = strings.TrimSpace(previous.RequestedOutcome)
	}
	scopeSummary := strings.TrimSpace(in.ScopeSummary)
	if scopeSummary == "" && previous != nil {
		scopeSummary = strings.TrimSpace(previous.ScopeSummary)
	}
	if scopeSummary == "" {
		scopeSummary = "bounded scope not explicitly provided; operator clarification may improve execution targeting"
	}

	constraints := dedupeNonEmpty(append(append(append([]string{}, in.ExplicitConstraints...), caps.Constraints...), previousStringSlice(previous, func(b *brief.ExecutionBrief) []string {
		return b.Constraints
	})...), 16)
	doneCriteria := dedupeNonEmpty(append(append([]string{}, in.DoneCriteria...), previousStringSlice(previous, func(b *brief.ExecutionBrief) []string {
		return b.DoneCriteria
	})...), 8)
	if len(doneCriteria) == 0 {
		doneCriteria = defaultDoneCriteriaForBriefPosture(posture)
	}

	scopeHints := append([]string{}, caps.TouchedFiles...)
	if len(scopeHints) == 0 && previous != nil {
		scopeHints = append(scopeHints, previous.ScopeIn...)
	}
	scopeOutHints := []string{}
	if previous != nil {
		scopeOutHints = append(scopeOutHints, previous.ScopeOut...)
	}

	requiresClarification := in.RequiresClarification || posture == brief.PostureClarificationNeeded
	ambiguityFlags := dedupeNonEmpty(in.AmbiguityFlags, 8)
	clarificationQuestions := dedupeNonEmpty(in.ClarificationQuestions, 6)
	if requiresClarification && len(clarificationQuestions) == 0 {
		clarificationQuestions = []string{"What concrete bounded next step should this brief target?"}
	}

	contextPackID := common.ContextPackID("")
	verbosity := brief.VerbosityStandard
	policyProfileID := "default-safe-v1"
	if previous != nil {
		contextPackID = previous.ContextPackID
		if previous.Verbosity != "" {
			verbosity = previous.Verbosity
		}
		if strings.TrimSpace(previous.PolicyProfileID) != "" {
			policyProfileID = previous.PolicyProfileID
		}
	}

	return brief.BuildInput{
		TaskID:                  caps.TaskID,
		IntentID:                in.IntentID,
		CapsuleVersion:          nextCapsuleVersion,
		Posture:                 posture,
		Goal:                    goal,
		RequestedOutcome:        requestedOutcome,
		NormalizedAction:        normalizedAction,
		ScopeSummary:            scopeSummary,
		Constraints:             constraints,
		ScopeHints:              scopeHints,
		ScopeOutHints:           scopeOutHints,
		DoneCriteria:            doneCriteria,
		AmbiguityFlags:          ambiguityFlags,
		ClarificationQuestions:  clarificationQuestions,
		RequiresClarification:   requiresClarification,
		WorkerFraming:           defaultWorkerFramingForPosture(posture, requiresClarification),
		BoundedEvidenceMessages: in.BoundedEvidenceMessages,
		ContextPackID:           contextPackID,
		Verbosity:               verbosity,
		PolicyProfileID:         policyProfileID,
	}
}

func deriveBriefPostureFromIntent(in intent.State) brief.Posture {
	if in.RequiresClarification || in.ExecutionReadiness == intent.ReadinessClarificationNeeded || in.Posture == intent.PostureClarificationNeeded || in.Posture == intent.PostureExploratoryAmbiguous {
		return brief.PostureClarificationNeeded
	}
	switch in.ExecutionReadiness {
	case intent.ReadinessPlanningInProgress:
		return brief.PosturePlanningOriented
	case intent.ReadinessValidationFocused:
		return brief.PostureValidationOriented
	case intent.ReadinessRepairRecovery:
		return brief.PostureRepairOriented
	default:
		return brief.PostureExecutionReady
	}
}

func normalizedBriefPosture(p brief.Posture, requiresClarification bool) brief.Posture {
	if requiresClarification {
		return brief.PostureClarificationNeeded
	}
	switch p {
	case brief.PostureExecutionReady, brief.PosturePlanningOriented, brief.PostureValidationOriented, brief.PostureRepairOriented:
		return p
	default:
		return brief.PostureClarificationNeeded
	}
}

func defaultDoneCriteriaForBriefPosture(posture brief.Posture) []string {
	switch posture {
	case brief.PosturePlanningOriented:
		return []string{"Bounded execution plan is explicit and scoped for the immediate next step."}
	case brief.PostureValidationOriented:
		return []string{"Validation findings are recorded with bounded evidence and no overclaiming."}
	case brief.PostureRepairOriented:
		return []string{"Repair step and bounded evidence summary are produced for the targeted issue."}
	case brief.PostureClarificationNeeded:
		return []string{"Clarification questions are captured before execution claims are made."}
	default:
		return []string{"Execution step is bounded by explicit constraints and done criteria."}
	}
}

func previousStringSlice(previous *brief.ExecutionBrief, sel func(*brief.ExecutionBrief) []string) []string {
	if previous == nil {
		return nil
	}
	return append([]string{}, sel(previous)...)
}
