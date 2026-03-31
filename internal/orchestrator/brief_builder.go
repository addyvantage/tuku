package orchestrator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"tuku/internal/domain/brief"
	"tuku/internal/domain/common"
	"tuku/internal/domain/promptir"
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
	posture := input.Posture
	if posture == "" {
		posture = brief.PostureClarificationNeeded
	}
	scopeSummary := strings.TrimSpace(input.ScopeSummary)
	if scopeSummary == "" {
		scopeSummary = "bounded scope not explicitly provided"
	}
	workerFraming := strings.TrimSpace(input.WorkerFraming)
	if workerFraming == "" {
		workerFraming = defaultWorkerFramingForPosture(posture, input.RequiresClarification)
	}

	payload := struct {
		Version                 int                     `json:"version"`
		TaskID                  common.TaskID           `json:"task_id"`
		IntentID                common.IntentID         `json:"intent_id"`
		CapsuleVersion          common.CapsuleVersion   `json:"capsule_version"`
		Posture                 brief.Posture           `json:"posture"`
		Goal                    string                  `json:"goal"`
		RequestedOutcome        string                  `json:"requested_outcome"`
		NormalizedAction        string                  `json:"normalized_action"`
		ScopeSummary            string                  `json:"scope_summary"`
		ScopeIn                 []string                `json:"scope_in"`
		ScopeOut                []string                `json:"scope_out"`
		Constraints             []string                `json:"constraints"`
		DoneCriteria            []string                `json:"done_criteria"`
		AmbiguityFlags          []string                `json:"ambiguity_flags"`
		ClarificationQuestions  []string                `json:"clarification_questions"`
		RequiresClarification   bool                    `json:"requires_clarification"`
		WorkerFraming           string                  `json:"worker_framing"`
		BoundedEvidenceMessages int                     `json:"bounded_evidence_messages"`
		PromptTriage            brief.PromptTriage      `json:"prompt_triage"`
		ContextPackID           common.ContextPackID    `json:"context_pack_id"`
		TaskMemoryID            common.MemoryID         `json:"task_memory_id"`
		MemoryCompression       brief.MemoryCompression `json:"memory_compression"`
		PromptIR                promptir.Packet         `json:"prompt_ir"`
		BenchmarkID             common.BenchmarkID      `json:"benchmark_id"`
		Verbosity               brief.Verbosity         `json:"verbosity"`
		PolicyProfileID         string                  `json:"policy_profile_id"`
	}{
		Version:                 2,
		TaskID:                  input.TaskID,
		IntentID:                input.IntentID,
		CapsuleVersion:          input.CapsuleVersion,
		Posture:                 posture,
		Goal:                    input.Goal,
		RequestedOutcome:        input.RequestedOutcome,
		NormalizedAction:        input.NormalizedAction,
		ScopeSummary:            scopeSummary,
		ScopeIn:                 append([]string{}, input.ScopeHints...),
		ScopeOut:                append([]string{}, input.ScopeOutHints...),
		Constraints:             append([]string{}, input.Constraints...),
		DoneCriteria:            append([]string{}, input.DoneCriteria...),
		AmbiguityFlags:          append([]string{}, input.AmbiguityFlags...),
		ClarificationQuestions:  append([]string{}, input.ClarificationQuestions...),
		RequiresClarification:   input.RequiresClarification,
		WorkerFraming:           workerFraming,
		BoundedEvidenceMessages: input.BoundedEvidenceMessages,
		PromptTriage:            clonePromptTriage(input.PromptTriage),
		ContextPackID:           input.ContextPackID,
		TaskMemoryID:            input.TaskMemoryID,
		MemoryCompression:       cloneMemoryCompression(input.MemoryCompression),
		PromptIR:                clonePromptIR(input.PromptIR),
		BenchmarkID:             input.BenchmarkID,
		Verbosity:               input.Verbosity,
		PolicyProfileID:         input.PolicyProfileID,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return brief.ExecutionBrief{}, fmt.Errorf("marshal brief hash payload: %w", err)
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])

	objective := strings.TrimSpace(input.Goal)
	if objective == "" {
		objective = strings.TrimSpace(input.NormalizedAction)
	}
	if objective == "" {
		objective = "Clarify and bound the next execution step from current task intent."
	}

	out := brief.ExecutionBrief{
		Version:                 2,
		BriefID:                 common.BriefID(b.idGenerator("brf")),
		TaskID:                  input.TaskID,
		IntentID:                input.IntentID,
		CapsuleVersion:          input.CapsuleVersion,
		CreatedAt:               b.clock(),
		Posture:                 posture,
		Objective:               objective,
		RequestedOutcome:        strings.TrimSpace(input.RequestedOutcome),
		NormalizedAction:        input.NormalizedAction,
		ScopeSummary:            scopeSummary,
		ScopeIn:                 append([]string{}, input.ScopeHints...),
		ScopeOut:                append([]string{}, input.ScopeOutHints...),
		Constraints:             append([]string{}, input.Constraints...),
		DoneCriteria:            append([]string{}, input.DoneCriteria...),
		AmbiguityFlags:          append([]string{}, input.AmbiguityFlags...),
		ClarificationQuestions:  append([]string{}, input.ClarificationQuestions...),
		RequiresClarification:   input.RequiresClarification,
		WorkerFraming:           workerFraming,
		BoundedEvidenceMessages: input.BoundedEvidenceMessages,
		PromptTriage:            clonePromptTriage(input.PromptTriage),
		ContextPackID:           input.ContextPackID,
		TaskMemoryID:            input.TaskMemoryID,
		MemoryCompression:       cloneMemoryCompression(input.MemoryCompression),
		PromptIR:                clonePromptIR(input.PromptIR),
		BenchmarkID:             input.BenchmarkID,
		Verbosity:               input.Verbosity,
		PolicyProfileID:         input.PolicyProfileID,
		BriefHash:               hash,
	}
	return out, nil
}

func clonePromptIR(in promptir.Packet) promptir.Packet {
	out := in
	out.RankedTargets = append([]promptir.Target{}, in.RankedTargets...)
	out.OperationPlan = append([]string{}, in.OperationPlan...)
	out.Constraints = append([]string{}, in.Constraints...)
	out.NonGoals = append([]string{}, in.NonGoals...)
	out.OutputContract = append([]string{}, in.OutputContract...)
	out.EvidenceRequirements = append([]string{}, in.EvidenceRequirements...)
	out.ValidatorPlan.Commands = append([]string{}, in.ValidatorPlan.Commands...)
	out.ValidatorPlan.Evidence = append([]string{}, in.ValidatorPlan.Evidence...)
	for i := range out.RankedTargets {
		out.RankedTargets[i].Reasons = append([]string{}, in.RankedTargets[i].Reasons...)
	}
	return out
}

func cloneMemoryCompression(in brief.MemoryCompression) brief.MemoryCompression {
	return brief.MemoryCompression{
		Applied:                   in.Applied,
		Summary:                   in.Summary,
		FullHistoryTokenEstimate:  in.FullHistoryTokenEstimate,
		ResumePromptTokenEstimate: in.ResumePromptTokenEstimate,
		MemoryCompactionRatio:     in.MemoryCompactionRatio,
		ConfirmedFactsCount:       in.ConfirmedFactsCount,
		TouchedFilesCount:         in.TouchedFilesCount,
		ValidatorsRunCount:        in.ValidatorsRunCount,
		CandidateFilesCount:       in.CandidateFilesCount,
		RejectedHypothesesCount:   in.RejectedHypothesesCount,
		UnknownsCount:             in.UnknownsCount,
	}
}

func defaultWorkerFramingForPosture(posture brief.Posture, requiresClarification bool) string {
	if requiresClarification || posture == brief.PostureClarificationNeeded {
		return "Clarification-focused brief: do not fabricate missing requirements; surface unresolved questions before bounded execution."
	}
	switch posture {
	case brief.PosturePlanningOriented:
		return "Planning-oriented brief: produce a bounded execution plan and explicit scope/constraint framing."
	case brief.PostureValidationOriented:
		return "Validation-oriented brief: validate current state and report bounded evidence without overclaiming completion."
	case brief.PostureRepairOriented:
		return "Repair-oriented brief: perform bounded repair/debug work and report concrete evidence."
	default:
		return "Execution-ready brief: execute the bounded task scope using explicit constraints and done criteria."
	}
}
