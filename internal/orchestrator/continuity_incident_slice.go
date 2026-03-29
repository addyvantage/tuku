package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/recoveryaction"
	rundomain "tuku/internal/domain/run"
	"tuku/internal/domain/transition"
)

const (
	defaultContinuityIncidentTransitionNeighborLimit = 2
	maxContinuityIncidentTransitionNeighborLimit     = 20
	defaultContinuityIncidentRunLimit                = 3
	maxContinuityIncidentRunLimit                    = 20
	defaultContinuityIncidentRecoveryLimit           = 3
	maxContinuityIncidentRecoveryLimit               = 20
	defaultContinuityIncidentProofLimit              = 8
	maxContinuityIncidentProofLimit                  = 50
	defaultContinuityIncidentAckLimit                = 3
	maxContinuityIncidentAckLimit                    = 20
)

type ContinuityIncidentAnchorMode string

const (
	ContinuityIncidentAnchorLatestTransition ContinuityIncidentAnchorMode = "LATEST_TRANSITION"
	ContinuityIncidentAnchorTransitionID     ContinuityIncidentAnchorMode = "TRANSITION_RECEIPT_ID"
)

type ContinuityIncidentRunSummary struct {
	RunID          common.RunID         `json:"run_id"`
	WorkerKind     rundomain.WorkerKind `json:"worker_kind"`
	Status         rundomain.Status     `json:"status"`
	ShellSessionID string               `json:"shell_session_id,omitempty"`
	ExitCode       *int                 `json:"exit_code,omitempty"`
	OccurredAt     time.Time            `json:"occurred_at"`
	StartedAt      time.Time            `json:"started_at"`
	EndedAt        *time.Time           `json:"ended_at,omitempty"`
	Summary        string               `json:"summary,omitempty"`
}

type ContinuityIncidentRecoveryActionSummary struct {
	ActionID        string              `json:"action_id"`
	Kind            recoveryaction.Kind `json:"kind"`
	RunID           common.RunID        `json:"run_id,omitempty"`
	CheckpointID    common.CheckpointID `json:"checkpoint_id,omitempty"`
	HandoffID       string              `json:"handoff_id,omitempty"`
	LaunchAttemptID string              `json:"launch_attempt_id,omitempty"`
	Summary         string              `json:"summary,omitempty"`
	CreatedAt       time.Time           `json:"created_at"`
}

type ContinuityIncidentProofSummary struct {
	EventID    common.EventID  `json:"event_id"`
	Type       proof.EventType `json:"type"`
	ActorType  proof.ActorType `json:"actor_type"`
	ActorID    string          `json:"actor_id"`
	Timestamp  time.Time       `json:"timestamp"`
	Summary    string          `json:"summary,omitempty"`
	SequenceNo int64           `json:"sequence_no"`
}

type ContinuityIncidentRiskSummary struct {
	ReviewGapPresent                bool   `json:"review_gap_present"`
	AcknowledgmentPresent           bool   `json:"acknowledgment_present"`
	StaleOrUnreviewedReviewPosture  bool   `json:"stale_or_unreviewed_review_posture"`
	SourceScopedReviewPosture       bool   `json:"source_scoped_review_posture"`
	IntoClaudeOwnershipTransition   bool   `json:"into_claude_ownership_transition"`
	BackToLocalOwnershipTransition  bool   `json:"back_to_local_ownership_transition"`
	UnresolvedContinuityAmbiguity   bool   `json:"unresolved_continuity_ambiguity"`
	NearbyFailedOrInterruptedRuns   int    `json:"nearby_failed_or_interrupted_runs"`
	NearbyRecoveryActions           int    `json:"nearby_recovery_actions"`
	RecentFailureOrRecoveryActivity bool   `json:"recent_failure_or_recovery_activity"`
	OperationallyNotable            bool   `json:"operationally_notable"`
	Summary                         string `json:"summary,omitempty"`
}

type ReadContinuityIncidentSliceRequest struct {
	TaskID                    string
	AnchorTransitionReceiptID string
	TransitionNeighborLimit   int
	RunLimit                  int
	RecoveryLimit             int
	ProofLimit                int
	AckLimit                  int
}

type ReadContinuityIncidentSliceResult struct {
	TaskID                             common.TaskID
	Bounded                            bool
	AnchorMode                         ContinuityIncidentAnchorMode
	RequestedAnchorTransitionReceiptID common.EventID
	Anchor                             ContinuityTransitionReceiptSummary
	TransitionNeighborLimit            int
	RunLimit                           int
	RecoveryLimit                      int
	ProofLimit                         int
	AckLimit                           int
	HasOlderTransitionsOutsideWindow   bool
	HasNewerTransitionsOutsideWindow   bool
	WindowStartAt                      time.Time
	WindowEndAt                        time.Time
	Transitions                        []ContinuityTransitionReceiptSummary
	Runs                               []ContinuityIncidentRunSummary
	RecoveryActions                    []ContinuityIncidentRecoveryActionSummary
	ProofEvents                        []ContinuityIncidentProofSummary
	LatestTranscriptReview             *ShellTranscriptReviewSummary
	LatestTranscriptReviewGapAck       *TranscriptReviewGapAcknowledgmentSummary
	RecentTranscriptReviewGapAcks      []TranscriptReviewGapAcknowledgmentSummary
	RiskSummary                        ContinuityIncidentRiskSummary
	Caveat                             string
}

func (c *Coordinator) ReadContinuityIncidentSlice(ctx context.Context, req ReadContinuityIncidentSliceRequest) (ReadContinuityIncidentSliceResult, error) {
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return ReadContinuityIncidentSliceResult{}, fmt.Errorf("task id is required")
	}
	if _, err := c.store.Capsules().Get(taskID); err != nil {
		return ReadContinuityIncidentSliceResult{}, err
	}

	transitionNeighborLimit := clampPositive(req.TransitionNeighborLimit, defaultContinuityIncidentTransitionNeighborLimit, maxContinuityIncidentTransitionNeighborLimit)
	runLimit := clampPositive(req.RunLimit, defaultContinuityIncidentRunLimit, maxContinuityIncidentRunLimit)
	recoveryLimit := clampPositive(req.RecoveryLimit, defaultContinuityIncidentRecoveryLimit, maxContinuityIncidentRecoveryLimit)
	proofLimit := clampPositive(req.ProofLimit, defaultContinuityIncidentProofLimit, maxContinuityIncidentProofLimit)
	ackLimit := clampPositive(req.AckLimit, defaultContinuityIncidentAckLimit, maxContinuityIncidentAckLimit)

	requestedAnchor := common.EventID(strings.TrimSpace(req.AnchorTransitionReceiptID))
	anchorMode := ContinuityIncidentAnchorLatestTransition
	var anchorRecord transition.Receipt
	var err error
	if requestedAnchor != "" {
		anchorMode = ContinuityIncidentAnchorTransitionID
		anchorRecord, err = c.store.TransitionReceipts().GetByTaskReceipt(taskID, requestedAnchor)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ReadContinuityIncidentSliceResult{}, fmt.Errorf("anchor transition receipt %s was not found for task %s", requestedAnchor, taskID)
			}
			return ReadContinuityIncidentSliceResult{}, err
		}
	} else {
		anchorRecord, err = c.store.TransitionReceipts().LatestByTask(taskID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ReadContinuityIncidentSliceResult{}, fmt.Errorf("no continuity transition receipt exists for task %s", taskID)
			}
			return ReadContinuityIncidentSliceResult{}, err
		}
	}
	anchor := continuityTransitionReceiptSummary(anchorRecord)

	olderRecords, err := c.store.TransitionReceipts().ListByTaskFiltered(taskID, transition.ReceiptListFilter{
		Limit:           transitionNeighborLimit + 1,
		BeforeReceiptID: anchor.ReceiptID,
		BeforeCreatedAt: anchor.CreatedAt,
	})
	if err != nil {
		return ReadContinuityIncidentSliceResult{}, err
	}
	hasOlder := len(olderRecords) > transitionNeighborLimit
	if hasOlder {
		olderRecords = olderRecords[:transitionNeighborLimit]
	}

	newerRecords, err := c.store.TransitionReceipts().ListByTaskAfter(taskID, anchor.ReceiptID, anchor.CreatedAt, transitionNeighborLimit+1)
	if err != nil {
		return ReadContinuityIncidentSliceResult{}, err
	}
	hasNewer := len(newerRecords) > transitionNeighborLimit
	if hasNewer {
		newerRecords = newerRecords[:transitionNeighborLimit]
	}

	transitions := make([]ContinuityTransitionReceiptSummary, 0, len(olderRecords)+1+len(newerRecords))
	for i := len(olderRecords) - 1; i >= 0; i-- {
		transitions = append(transitions, continuityTransitionReceiptSummary(olderRecords[i]))
	}
	transitions = append(transitions, anchor)
	for _, record := range newerRecords {
		transitions = append(transitions, continuityTransitionReceiptSummary(record))
	}

	windowStart := anchor.CreatedAt
	windowEnd := anchor.CreatedAt
	if len(transitions) > 0 {
		windowStart = transitions[0].CreatedAt
		windowEnd = transitions[len(transitions)-1].CreatedAt
	}

	runs, err := c.incidentRunsNearAnchor(taskID, anchor.CreatedAt, runLimit)
	if err != nil {
		return ReadContinuityIncidentSliceResult{}, err
	}
	recoveryActions, err := c.incidentRecoveryActionsNearAnchor(taskID, anchor.CreatedAt, recoveryLimit)
	if err != nil {
		return ReadContinuityIncidentSliceResult{}, err
	}
	proofEvents, err := c.incidentProofEventsNearAnchor(taskID, anchor.CreatedAt, proofLimit)
	if err != nil {
		return ReadContinuityIncidentSliceResult{}, err
	}
	latestReview, latestAck, recentAcks, err := c.incidentTranscriptReviewEvidence(ctx, taskID, anchor, ackLimit)
	if err != nil {
		return ReadContinuityIncidentSliceResult{}, err
	}

	failedOrInterrupted := 0
	for _, run := range runs {
		if run.Status == rundomain.StatusFailed || run.Status == rundomain.StatusInterrupted {
			failedOrInterrupted++
		}
	}
	risk := continuityIncidentRiskSummaryFromAnchor(anchor, failedOrInterrupted, len(recoveryActions))

	result := ReadContinuityIncidentSliceResult{
		TaskID:                             taskID,
		Bounded:                            true,
		AnchorMode:                         anchorMode,
		RequestedAnchorTransitionReceiptID: requestedAnchor,
		Anchor:                             anchor,
		TransitionNeighborLimit:            transitionNeighborLimit,
		RunLimit:                           runLimit,
		RecoveryLimit:                      recoveryLimit,
		ProofLimit:                         proofLimit,
		AckLimit:                           ackLimit,
		HasOlderTransitionsOutsideWindow:   hasOlder,
		HasNewerTransitionsOutsideWindow:   hasNewer,
		WindowStartAt:                      windowStart,
		WindowEndAt:                        windowEnd,
		Transitions:                        transitions,
		Runs:                               runs,
		RecoveryActions:                    recoveryActions,
		ProofEvents:                        proofEvents,
		LatestTranscriptReview:             latestReview,
		LatestTranscriptReviewGapAck:       latestAck,
		RecentTranscriptReviewGapAcks:      recentAcks,
		RiskSummary:                        risk,
		Caveat:                             "continuity incident slices correlate nearby durable evidence in a bounded window and do not prove causality, completion, correctness, resumability, or full transcript completeness",
	}
	return result, nil
}

func continuityIncidentSummaryProjection(anchor *ContinuityTransitionReceiptSummary, latestRun *rundomain.ExecutionRun, latestRecoveryAction *recoveryaction.Record) *ContinuityIncidentRiskSummary {
	if anchor == nil {
		return nil
	}
	failedOrInterrupted := 0
	if latestRun != nil && (latestRun.Status == rundomain.StatusFailed || latestRun.Status == rundomain.StatusInterrupted) {
		failedOrInterrupted = 1
	}
	recoveryCount := 0
	if latestRecoveryAction != nil {
		recoveryCount = 1
	}
	summary := continuityIncidentRiskSummaryFromAnchor(*anchor, failedOrInterrupted, recoveryCount)
	return &summary
}

func continuityIncidentRiskSummaryFromAnchor(anchor ContinuityTransitionReceiptSummary, nearbyFailedOrInterruptedRuns int, nearbyRecoveryActions int) ContinuityIncidentRiskSummary {
	staleOrUnreviewed := continuityTransitionPostureIsStale(anchor.ReviewPosture)
	sourceScoped := continuityTransitionPostureIsSourceScoped(anchor.ReviewPosture)
	intoClaude := anchor.BranchClassAfter == ActiveBranchClassHandoffClaude && anchor.BranchClassBefore != ActiveBranchClassHandoffClaude
	backToLocal := anchor.BranchClassBefore == ActiveBranchClassHandoffClaude && anchor.BranchClassAfter == ActiveBranchClassLocal
	unresolved := continuityIncidentAmbiguityFromHandoffState(anchor.HandoffStateAfter)
	recentFailureOrRecovery := nearbyFailedOrInterruptedRuns > 0 || nearbyRecoveryActions > 0

	summary := ContinuityIncidentRiskSummary{
		ReviewGapPresent:                anchor.ReviewGapPresent,
		AcknowledgmentPresent:           anchor.AcknowledgmentPresent,
		StaleOrUnreviewedReviewPosture:  staleOrUnreviewed,
		SourceScopedReviewPosture:       sourceScoped,
		IntoClaudeOwnershipTransition:   intoClaude,
		BackToLocalOwnershipTransition:  backToLocal,
		UnresolvedContinuityAmbiguity:   unresolved,
		NearbyFailedOrInterruptedRuns:   nearbyFailedOrInterruptedRuns,
		NearbyRecoveryActions:           nearbyRecoveryActions,
		RecentFailureOrRecoveryActivity: recentFailureOrRecovery,
	}
	summary.OperationallyNotable =
		summary.ReviewGapPresent ||
			summary.StaleOrUnreviewedReviewPosture ||
			summary.SourceScopedReviewPosture ||
			summary.IntoClaudeOwnershipTransition ||
			summary.BackToLocalOwnershipTransition ||
			summary.UnresolvedContinuityAmbiguity ||
			summary.RecentFailureOrRecoveryActivity

	notes := make([]string, 0, 6)
	if summary.ReviewGapPresent {
		if summary.AcknowledgmentPresent {
			notes = append(notes, "anchor transition recorded under explicit transcript review-gap acknowledgment")
		} else {
			notes = append(notes, "anchor transition recorded with unacknowledged transcript review gap")
		}
	}
	if summary.StaleOrUnreviewedReviewPosture {
		notes = append(notes, "anchor transition carried stale or unreviewed retained transcript posture")
	}
	if summary.SourceScopedReviewPosture {
		notes = append(notes, "anchor transition used source-scoped transcript review posture")
	}
	if summary.IntoClaudeOwnershipTransition {
		notes = append(notes, "anchor transition moved continuity ownership into Claude handoff branch")
	}
	if summary.BackToLocalOwnershipTransition {
		notes = append(notes, "anchor transition moved continuity ownership back to local lineage")
	}
	if summary.UnresolvedContinuityAmbiguity {
		notes = append(notes, "anchor transition ended in a continuity state that remains operationally unproven")
	}
	if summary.RecentFailureOrRecoveryActivity {
		notes = append(notes, fmt.Sprintf("bounded incident slice includes %d failed/interrupted run(s) and %d recovery action(s)", nearbyFailedOrInterruptedRuns, nearbyRecoveryActions))
	}
	if len(notes) == 0 {
		summary.Summary = "Bounded incident slice shows no explicit review-gap or failure/recovery risk flags around the anchor transition."
		return summary
	}
	summary.Summary = strings.Join(notes, "; ")
	return summary
}

func continuityIncidentAmbiguityFromHandoffState(state HandoffContinuityState) bool {
	switch state {
	case HandoffContinuityStateLaunchPendingOutcome,
		HandoffContinuityStateLaunchCompletedAckSeen,
		HandoffContinuityStateLaunchCompletedAckEmpty,
		HandoffContinuityStateLaunchCompletedAckLost,
		HandoffContinuityStateFollowThroughProofOfLife,
		HandoffContinuityStateFollowThroughConfirmed,
		HandoffContinuityStateFollowThroughUnknown,
		HandoffContinuityStateFollowThroughStalled:
		return true
	default:
		return false
	}
}

func (c *Coordinator) incidentRunsNearAnchor(taskID common.TaskID, anchorAt time.Time, limit int) ([]ContinuityIncidentRunSummary, error) {
	if limit <= 0 {
		return nil, nil
	}
	scanLimit := limit * 12
	if scanLimit < 24 {
		scanLimit = 24
	}
	if scanLimit > 300 {
		scanLimit = 300
	}
	runs, err := c.store.Runs().ListByTask(taskID, scanLimit)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, nil
	}
	type runCandidate struct {
		summary ContinuityIncidentRunSummary
		delta   time.Duration
	}
	candidates := make([]runCandidate, 0, len(runs))
	for _, record := range runs {
		summary := continuityIncidentRunSummary(record)
		candidates = append(candidates, runCandidate{
			summary: summary,
			delta:   absoluteDuration(summary.OccurredAt.Sub(anchorAt)),
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].delta != candidates[j].delta {
			return candidates[i].delta < candidates[j].delta
		}
		if !candidates[i].summary.OccurredAt.Equal(candidates[j].summary.OccurredAt) {
			return candidates[i].summary.OccurredAt.After(candidates[j].summary.OccurredAt)
		}
		return candidates[i].summary.RunID > candidates[j].summary.RunID
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	out := make([]ContinuityIncidentRunSummary, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.summary)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].OccurredAt.Equal(out[j].OccurredAt) {
			return out[i].OccurredAt.Before(out[j].OccurredAt)
		}
		return out[i].RunID < out[j].RunID
	})
	return out, nil
}

func continuityIncidentRunSummary(record rundomain.ExecutionRun) ContinuityIncidentRunSummary {
	reference := record.CreatedAt
	if !record.StartedAt.IsZero() {
		reference = record.StartedAt
	}
	if !record.UpdatedAt.IsZero() {
		reference = record.UpdatedAt
	}
	if record.EndedAt != nil && !record.EndedAt.IsZero() {
		reference = *record.EndedAt
	}
	summary := strings.TrimSpace(record.StructuredSummary)
	if summary == "" {
		summary = strings.TrimSpace(record.LastKnownSummary)
	}
	if summary == "" {
		summary = strings.ToLower(string(record.Status))
	}
	return ContinuityIncidentRunSummary{
		RunID:          record.RunID,
		WorkerKind:     record.WorkerKind,
		Status:         record.Status,
		ShellSessionID: record.ShellSessionID,
		ExitCode:       record.ExitCode,
		OccurredAt:     reference,
		StartedAt:      record.StartedAt,
		EndedAt:        record.EndedAt,
		Summary:        summary,
	}
}

func (c *Coordinator) incidentRecoveryActionsNearAnchor(taskID common.TaskID, anchorAt time.Time, limit int) ([]ContinuityIncidentRecoveryActionSummary, error) {
	if limit <= 0 {
		return nil, nil
	}
	scanLimit := limit * 12
	if scanLimit < 24 {
		scanLimit = 24
	}
	if scanLimit > 300 {
		scanLimit = 300
	}
	records, err := c.store.RecoveryActions().ListByTask(taskID, scanLimit)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}
	type recoveryCandidate struct {
		record recoveryaction.Record
		delta  time.Duration
	}
	candidates := make([]recoveryCandidate, 0, len(records))
	for _, record := range records {
		candidates = append(candidates, recoveryCandidate{
			record: record,
			delta:  absoluteDuration(record.CreatedAt.Sub(anchorAt)),
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].delta != candidates[j].delta {
			return candidates[i].delta < candidates[j].delta
		}
		if !candidates[i].record.CreatedAt.Equal(candidates[j].record.CreatedAt) {
			return candidates[i].record.CreatedAt.After(candidates[j].record.CreatedAt)
		}
		return candidates[i].record.ActionID > candidates[j].record.ActionID
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	out := make([]ContinuityIncidentRecoveryActionSummary, 0, len(candidates))
	for _, candidate := range candidates {
		record := candidate.record
		out = append(out, ContinuityIncidentRecoveryActionSummary{
			ActionID:        record.ActionID,
			Kind:            record.Kind,
			RunID:           record.RunID,
			CheckpointID:    record.CheckpointID,
			HandoffID:       record.HandoffID,
			LaunchAttemptID: record.LaunchAttemptID,
			Summary:         record.Summary,
			CreatedAt:       record.CreatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ActionID < out[j].ActionID
	})
	return out, nil
}

func (c *Coordinator) incidentProofEventsNearAnchor(taskID common.TaskID, anchorAt time.Time, limit int) ([]ContinuityIncidentProofSummary, error) {
	if limit <= 0 {
		return nil, nil
	}
	scanLimit := limit * 20
	if scanLimit < 40 {
		scanLimit = 40
	}
	if scanLimit > 500 {
		scanLimit = 500
	}
	events, err := c.store.Proofs().ListByTask(taskID, scanLimit)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, nil
	}
	type proofCandidate struct {
		event proof.Event
		delta time.Duration
	}
	candidates := make([]proofCandidate, 0, len(events))
	for _, event := range events {
		candidates = append(candidates, proofCandidate{
			event: event,
			delta: absoluteDuration(event.Timestamp.Sub(anchorAt)),
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].delta != candidates[j].delta {
			return candidates[i].delta < candidates[j].delta
		}
		if candidates[i].event.SequenceNo != candidates[j].event.SequenceNo {
			return candidates[i].event.SequenceNo > candidates[j].event.SequenceNo
		}
		return candidates[i].event.EventID > candidates[j].event.EventID
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	out := make([]ContinuityIncidentProofSummary, 0, len(candidates))
	for _, candidate := range candidates {
		event := candidate.event
		out = append(out, ContinuityIncidentProofSummary{
			EventID:    event.EventID,
			Type:       event.Type,
			ActorType:  event.ActorType,
			ActorID:    event.ActorID,
			Timestamp:  event.Timestamp,
			Summary:    summarizeProofEvent(event),
			SequenceNo: event.SequenceNo,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SequenceNo != out[j].SequenceNo {
			return out[i].SequenceNo < out[j].SequenceNo
		}
		return out[i].EventID < out[j].EventID
	})
	return out, nil
}

func (c *Coordinator) incidentTranscriptReviewEvidence(ctx context.Context, taskID common.TaskID, anchor ContinuityTransitionReceiptSummary, ackLimit int) (*ShellTranscriptReviewSummary, *TranscriptReviewGapAcknowledgmentSummary, []TranscriptReviewGapAcknowledgmentSummary, error) {
	sessionID := strings.TrimSpace(anchor.ShellSessionID)
	if sessionID == "" {
		sessions, err := c.classifiedShellSessions(taskID)
		if err != nil {
			return nil, nil, nil, err
		}
		latest, recent, err := c.reviewGapAcknowledgmentProjection(taskID, sessions, ackLimit)
		if err != nil {
			return nil, nil, nil, err
		}
		return nil, latest, append([]TranscriptReviewGapAcknowledgmentSummary{}, recent...), nil
	}

	reviewHistory, reviewErr := c.ReadShellTranscriptReviewHistory(ctx, ReadShellTranscriptReviewHistoryRequest{
		TaskID:    string(taskID),
		SessionID: sessionID,
		Limit:     1,
	})
	var latestReview *ShellTranscriptReviewSummary
	if reviewErr == nil && reviewHistory.LatestReview != nil {
		copyReview := *reviewHistory.LatestReview
		latestReview = &copyReview
	}

	sessions, err := c.classifiedShellSessions(taskID)
	if err != nil {
		return nil, nil, nil, err
	}
	currentNewestBySession := make(map[string]int64, len(sessions))
	for _, session := range sessions {
		currentNewestBySession[strings.TrimSpace(session.SessionID)] = session.TranscriptNewestSequence
	}

	records, err := c.listShellTranscriptReviewGapAcknowledgments(taskID, sessionID, ackLimit)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(records) == 0 {
		return latestReview, nil, nil, nil
	}
	recent := make([]TranscriptReviewGapAcknowledgmentSummary, 0, len(records))
	for _, record := range records {
		currentNewest := currentNewestBySession[strings.TrimSpace(record.SessionID)]
		recent = append(recent, transcriptReviewGapAcknowledgmentSummary(record, currentNewest))
	}
	latest := recent[0]
	if anchor.LatestReviewGapAckID != "" {
		for _, item := range recent {
			if item.AcknowledgmentID == anchor.LatestReviewGapAckID {
				latest = item
				break
			}
		}
	}
	return latestReview, &latest, recent, nil
}

func absoluteDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}

func clampPositive(value int, fallback int, max int) int {
	if value <= 0 {
		value = fallback
	}
	if value > max {
		value = max
	}
	return value
}
