package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/incidenttriage"
	"tuku/internal/domain/proof"
	rundomain "tuku/internal/domain/run"
	"tuku/internal/domain/transition"
)

const (
	defaultContinuityIncidentTriageHistoryLimit = 5
	maxContinuityIncidentTriageHistoryLimit     = 50
	maxContinuityIncidentTriageSummaryChars     = 400
)

type ContinuityIncidentFollowUpState string

const (
	ContinuityIncidentFollowUpNone               ContinuityIncidentFollowUpState = "NONE"
	ContinuityIncidentFollowUpUntriaged          ContinuityIncidentFollowUpState = "UNTRIAGED"
	ContinuityIncidentFollowUpTriagedCurrent     ContinuityIncidentFollowUpState = "TRIAGED_CURRENT"
	ContinuityIncidentFollowUpNeedsFollowUp      ContinuityIncidentFollowUpState = "NEEDS_FOLLOW_UP"
	ContinuityIncidentFollowUpDeferred           ContinuityIncidentFollowUpState = "DEFERRED"
	ContinuityIncidentFollowUpTriageBehindLatest ContinuityIncidentFollowUpState = "TRIAGE_BEHIND_LATEST"
	ContinuityIncidentFollowUpPending            ContinuityIncidentFollowUpState = "FOLLOW_UP_PENDING"
	ContinuityIncidentFollowUpProgressed         ContinuityIncidentFollowUpState = "FOLLOW_UP_PROGRESSED"
	ContinuityIncidentFollowUpClosed             ContinuityIncidentFollowUpState = "FOLLOW_UP_CLOSED"
	ContinuityIncidentFollowUpReopened           ContinuityIncidentFollowUpState = "FOLLOW_UP_REOPENED"
)

type ContinuityIncidentTriageReceiptSummary struct {
	ReceiptID common.EventID `json:"receipt_id"`
	TaskID    common.TaskID  `json:"task_id"`

	AnchorMode                incidenttriage.AnchorMode `json:"anchor_mode"`
	AnchorTransitionReceiptID common.EventID            `json:"anchor_transition_receipt_id"`
	AnchorTransitionKind      transition.Kind           `json:"anchor_transition_kind"`
	AnchorHandoffID           string                    `json:"anchor_handoff_id,omitempty"`
	AnchorShellSessionID      string                    `json:"anchor_shell_session_id,omitempty"`

	Posture         incidenttriage.Posture         `json:"posture"`
	FollowUpPosture incidenttriage.FollowUpPosture `json:"follow_up_posture"`
	Summary         string                         `json:"summary,omitempty"`

	ReviewGapPresent         bool                          `json:"review_gap_present,omitempty"`
	ReviewPosture            transition.ReviewPosture      `json:"review_posture,omitempty"`
	ReviewState              string                        `json:"review_state,omitempty"`
	ReviewScope              string                        `json:"review_scope,omitempty"`
	ReviewedUpToSequence     int64                         `json:"reviewed_up_to_sequence,omitempty"`
	OldestUnreviewedSequence int64                         `json:"oldest_unreviewed_sequence,omitempty"`
	NewestRetainedSequence   int64                         `json:"newest_retained_sequence,omitempty"`
	UnreviewedRetainedCount  int                           `json:"unreviewed_retained_count,omitempty"`
	LatestReviewID           common.EventID                `json:"latest_review_id,omitempty"`
	LatestReviewGapAckID     common.EventID                `json:"latest_review_gap_acknowledgment_id,omitempty"`
	AcknowledgmentPresent    bool                          `json:"acknowledgment_present,omitempty"`
	AcknowledgmentClass      string                        `json:"acknowledgment_class,omitempty"`
	RiskSummary              ContinuityIncidentRiskSummary `json:"risk_summary"`
	CreatedAt                time.Time                     `json:"created_at"`
}

type ContinuityIncidentFollowUpSummary struct {
	State                     ContinuityIncidentFollowUpState   `json:"state"`
	Digest                    string                            `json:"digest,omitempty"`
	WindowAdvisory            string                            `json:"window_advisory,omitempty"`
	Advisory                  string                            `json:"advisory,omitempty"`
	ClosureIntelligence       *ContinuityIncidentClosureSummary `json:"closure_intelligence,omitempty"`
	FollowUpAdvised           bool                              `json:"follow_up_advised"`
	NeedsFollowUp             bool                              `json:"needs_follow_up"`
	Deferred                  bool                              `json:"deferred"`
	TriageBehindLatest        bool                              `json:"triage_behind_latest"`
	TriagedUnderReviewRisk    bool                              `json:"triaged_under_review_risk"`
	LatestTransitionReceiptID common.EventID                    `json:"latest_transition_receipt_id,omitempty"`
	LatestTriageReceiptID     common.EventID                    `json:"latest_triage_receipt_id,omitempty"`
	TriageAnchorReceiptID     common.EventID                    `json:"triage_anchor_receipt_id,omitempty"`
	TriagePosture             incidenttriage.Posture            `json:"triage_posture,omitempty"`
	LatestFollowUpReceiptID   common.EventID                    `json:"latest_follow_up_receipt_id,omitempty"`
	LatestFollowUpActionKind  incidenttriage.FollowUpActionKind `json:"latest_follow_up_action_kind,omitempty"`
	LatestFollowUpSummary     string                            `json:"latest_follow_up_summary,omitempty"`
	LatestFollowUpAt          time.Time                         `json:"latest_follow_up_at,omitempty"`
	FollowUpReceiptPresent    bool                              `json:"follow_up_receipt_present,omitempty"`
	FollowUpOpen              bool                              `json:"follow_up_open,omitempty"`
	FollowUpClosed            bool                              `json:"follow_up_closed,omitempty"`
	FollowUpReopened          bool                              `json:"follow_up_reopened,omitempty"`
	FollowUpProgressed        bool                              `json:"follow_up_progressed,omitempty"`
}

type RecordContinuityIncidentTriageRequest struct {
	TaskID                    string
	AnchorMode                string
	AnchorTransitionReceiptID string
	Posture                   string
	Summary                   string
}

type RecordContinuityIncidentTriageResult struct {
	TaskID                          common.TaskID
	AnchorMode                      incidenttriage.AnchorMode
	AnchorTransitionReceiptID       common.EventID
	Posture                         incidenttriage.Posture
	Reused                          bool
	Receipt                         ContinuityIncidentTriageReceiptSummary
	LatestContinuityTransition      *ContinuityTransitionReceiptSummary
	RecentContinuityIncidentTriages []ContinuityIncidentTriageReceiptSummary
	FollowUp                        *ContinuityIncidentFollowUpSummary
}

type ContinuityIncidentFollowUpReceiptSummary struct {
	ReceiptID common.EventID `json:"receipt_id"`
	TaskID    common.TaskID  `json:"task_id"`

	AnchorMode                incidenttriage.AnchorMode `json:"anchor_mode"`
	AnchorTransitionReceiptID common.EventID            `json:"anchor_transition_receipt_id"`
	AnchorTransitionKind      transition.Kind           `json:"anchor_transition_kind"`
	AnchorHandoffID           string                    `json:"anchor_handoff_id,omitempty"`
	AnchorShellSessionID      string                    `json:"anchor_shell_session_id,omitempty"`

	TriageReceiptID     common.EventID                    `json:"triage_receipt_id"`
	TriagePosture       incidenttriage.Posture            `json:"triage_posture"`
	TriageFollowUpState incidenttriage.FollowUpPosture    `json:"triage_follow_up_posture"`
	ActionKind          incidenttriage.FollowUpActionKind `json:"action_kind"`
	Summary             string                            `json:"summary,omitempty"`

	ReviewGapPresent         bool                     `json:"review_gap_present,omitempty"`
	ReviewPosture            transition.ReviewPosture `json:"review_posture,omitempty"`
	ReviewState              string                   `json:"review_state,omitempty"`
	ReviewScope              string                   `json:"review_scope,omitempty"`
	ReviewedUpToSequence     int64                    `json:"reviewed_up_to_sequence,omitempty"`
	OldestUnreviewedSequence int64                    `json:"oldest_unreviewed_sequence,omitempty"`
	NewestRetainedSequence   int64                    `json:"newest_retained_sequence,omitempty"`
	UnreviewedRetainedCount  int                      `json:"unreviewed_retained_count,omitempty"`
	LatestReviewID           common.EventID           `json:"latest_review_id,omitempty"`
	LatestReviewGapAckID     common.EventID           `json:"latest_review_gap_acknowledgment_id,omitempty"`
	AcknowledgmentPresent    bool                     `json:"acknowledgment_present,omitempty"`
	AcknowledgmentClass      string                   `json:"acknowledgment_class,omitempty"`
	TriagedUnderReviewRisk   bool                     `json:"triaged_under_review_risk,omitempty"`
	CreatedAt                time.Time                `json:"created_at"`
}

func (c *Coordinator) RecordContinuityIncidentTriage(ctx context.Context, req RecordContinuityIncidentTriageRequest) (RecordContinuityIncidentTriageResult, error) {
	_ = ctx
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return RecordContinuityIncidentTriageResult{}, fmt.Errorf("task id is required")
	}
	if _, err := c.store.Capsules().Get(taskID); err != nil {
		return RecordContinuityIncidentTriageResult{}, err
	}

	anchor, anchorMode, err := c.resolveContinuityIncidentTriageAnchor(taskID, req.AnchorMode, req.AnchorTransitionReceiptID)
	if err != nil {
		return RecordContinuityIncidentTriageResult{}, err
	}
	posture, err := parseContinuityIncidentTriagePosture(req.Posture)
	if err != nil {
		return RecordContinuityIncidentTriageResult{}, err
	}
	summary := normalizeContinuityIncidentTriageSummary(req.Summary)
	followUpPosture := followUpPostureFromTriagePosture(posture)
	risk, err := c.continuityIncidentRiskSnapshotForAnchor(taskID, anchor)
	if err != nil {
		return RecordContinuityIncidentTriageResult{}, err
	}

	if latestForAnchor, err := c.store.IncidentTriages().LatestByTaskAnchor(taskID, anchor.ReceiptID); err == nil {
		if continuityIncidentTriageEquivalent(latestForAnchor, anchorMode, posture, followUpPosture, summary) {
			latestTransition, recentTriages, followUp, _, projectionErr := c.continuityIncidentTriageReadProjection(taskID)
			if projectionErr != nil {
				return RecordContinuityIncidentTriageResult{}, projectionErr
			}
			return RecordContinuityIncidentTriageResult{
				TaskID:                          taskID,
				AnchorMode:                      anchorMode,
				AnchorTransitionReceiptID:       anchor.ReceiptID,
				Posture:                         posture,
				Reused:                          true,
				Receipt:                         continuityIncidentTriageReceiptSummary(latestForAnchor),
				LatestContinuityTransition:      latestTransition,
				RecentContinuityIncidentTriages: recentTriages,
				FollowUp:                        followUp,
			}, nil
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return RecordContinuityIncidentTriageResult{}, err
	}

	record := incidenttriage.Receipt{
		Version:                             1,
		ReceiptID:                           common.EventID(c.idGenerator("citr")),
		TaskID:                              taskID,
		AnchorMode:                          anchorMode,
		AnchorTransitionReceiptID:           anchor.ReceiptID,
		AnchorTransitionKind:                anchor.TransitionKind,
		AnchorHandoffID:                     anchor.HandoffID,
		AnchorShellSessionID:                anchor.ShellSessionID,
		Posture:                             posture,
		FollowUpPosture:                     followUpPosture,
		Summary:                             summary,
		ReviewGapPresent:                    anchor.ReviewGapPresent,
		ReviewPosture:                       anchor.ReviewPosture,
		ReviewState:                         anchor.ReviewState,
		ReviewScope:                         anchor.ReviewScope,
		ReviewedUpToSequence:                anchor.ReviewedUpToSequence,
		OldestUnreviewedSequence:            anchor.OldestUnreviewedSequence,
		NewestRetainedSequence:              anchor.NewestRetainedSequence,
		UnreviewedRetainedCount:             anchor.UnreviewedRetainedCount,
		LatestReviewID:                      anchor.LatestReviewID,
		LatestReviewGapAckID:                anchor.LatestReviewGapAckID,
		AcknowledgmentPresent:               anchor.AcknowledgmentPresent,
		AcknowledgmentClass:                 anchor.AcknowledgmentClass,
		RiskReviewGapPresent:                risk.ReviewGapPresent,
		RiskAcknowledgmentPresent:           risk.AcknowledgmentPresent,
		RiskStaleOrUnreviewedReviewPosture:  risk.StaleOrUnreviewedReviewPosture,
		RiskSourceScopedReviewPosture:       risk.SourceScopedReviewPosture,
		RiskIntoClaudeOwnershipTransition:   risk.IntoClaudeOwnershipTransition,
		RiskBackToLocalOwnershipTransition:  risk.BackToLocalOwnershipTransition,
		RiskUnresolvedContinuityAmbiguity:   risk.UnresolvedContinuityAmbiguity,
		RiskNearbyFailedOrInterruptedRuns:   risk.NearbyFailedOrInterruptedRuns,
		RiskNearbyRecoveryActions:           risk.NearbyRecoveryActions,
		RiskRecentFailureOrRecoveryActivity: risk.RecentFailureOrRecoveryActivity,
		RiskOperationallyNotable:            risk.OperationallyNotable,
		RiskSummary:                         risk.Summary,
		CreatedAt:                           c.clock(),
	}
	if record.Summary == "" {
		record.Summary = fmt.Sprintf("continuity incident %s at transition %s", strings.ToLower(string(posture)), anchor.ReceiptID)
	}

	if err := c.withTx(func(txc *Coordinator) error {
		if err := txc.store.IncidentTriages().Create(record); err != nil {
			return err
		}
		caps, err := txc.store.Capsules().Get(taskID)
		if err != nil {
			return err
		}
		payload := map[string]any{
			"incident_triage_receipt_id":      record.ReceiptID,
			"anchor_mode":                     record.AnchorMode,
			"anchor_transition_receipt_id":    record.AnchorTransitionReceiptID,
			"anchor_transition_kind":          record.AnchorTransitionKind,
			"anchor_handoff_id":               record.AnchorHandoffID,
			"anchor_shell_session_id":         record.AnchorShellSessionID,
			"posture":                         record.Posture,
			"follow_up_posture":               record.FollowUpPosture,
			"summary":                         record.Summary,
			"review_gap_present":              record.ReviewGapPresent,
			"review_posture":                  record.ReviewPosture,
			"review_state":                    record.ReviewState,
			"review_scope":                    record.ReviewScope,
			"reviewed_up_to_sequence":         record.ReviewedUpToSequence,
			"oldest_unreviewed_sequence":      record.OldestUnreviewedSequence,
			"newest_retained_sequence":        record.NewestRetainedSequence,
			"unreviewed_retained_count":       record.UnreviewedRetainedCount,
			"latest_review_id":                record.LatestReviewID,
			"latest_review_gap_ack_id":        record.LatestReviewGapAckID,
			"acknowledgment_present":          record.AcknowledgmentPresent,
			"acknowledgment_class":            record.AcknowledgmentClass,
			"risk_review_gap_present":         record.RiskReviewGapPresent,
			"risk_acknowledgment_present":     record.RiskAcknowledgmentPresent,
			"risk_stale_or_unreviewed":        record.RiskStaleOrUnreviewedReviewPosture,
			"risk_source_scoped":              record.RiskSourceScopedReviewPosture,
			"risk_into_claude":                record.RiskIntoClaudeOwnershipTransition,
			"risk_back_to_local":              record.RiskBackToLocalOwnershipTransition,
			"risk_unresolved_continuity":      record.RiskUnresolvedContinuityAmbiguity,
			"risk_nearby_failed_runs":         record.RiskNearbyFailedOrInterruptedRuns,
			"risk_nearby_recovery_actions":    record.RiskNearbyRecoveryActions,
			"risk_recent_failure_or_recovery": record.RiskRecentFailureOrRecoveryActivity,
			"risk_operationally_notable":      record.RiskOperationallyNotable,
			"risk_summary":                    record.RiskSummary,
		}
		return txc.appendProof(caps, proof.EventContinuityIncidentTriaged, proof.ActorUser, "user", payload, nil)
	}); err != nil {
		return RecordContinuityIncidentTriageResult{}, err
	}

	latestTransition, recentTriages, followUp, _, err := c.continuityIncidentTriageReadProjection(taskID)
	if err != nil {
		return RecordContinuityIncidentTriageResult{}, err
	}
	return RecordContinuityIncidentTriageResult{
		TaskID:                          taskID,
		AnchorMode:                      anchorMode,
		AnchorTransitionReceiptID:       anchor.ReceiptID,
		Posture:                         posture,
		Receipt:                         continuityIncidentTriageReceiptSummary(record),
		LatestContinuityTransition:      latestTransition,
		RecentContinuityIncidentTriages: recentTriages,
		FollowUp:                        followUp,
	}, nil
}

func continuityIncidentTriageEquivalent(existing incidenttriage.Receipt, anchorMode incidenttriage.AnchorMode, posture incidenttriage.Posture, followUpPosture incidenttriage.FollowUpPosture, summary string) bool {
	return existing.AnchorMode == anchorMode &&
		existing.Posture == posture &&
		existing.FollowUpPosture == followUpPosture &&
		strings.TrimSpace(existing.Summary) == summary
}

func (c *Coordinator) resolveContinuityIncidentTriageAnchor(taskID common.TaskID, rawMode string, rawReceiptID string) (ContinuityTransitionReceiptSummary, incidenttriage.AnchorMode, error) {
	modeRaw := strings.TrimSpace(rawMode)
	receiptID := common.EventID(strings.TrimSpace(rawReceiptID))
	mode := incidenttriage.AnchorMode("")
	if modeRaw == "" {
		if receiptID != "" {
			mode = incidenttriage.AnchorModeTransitionID
		} else {
			mode = incidenttriage.AnchorModeLatestTransition
		}
	} else {
		switch strings.ToLower(modeRaw) {
		case "latest", "latest_transition":
			mode = incidenttriage.AnchorModeLatestTransition
		case "receipt", "transition_receipt_id", "transition_receipt":
			mode = incidenttriage.AnchorModeTransitionID
		default:
			return ContinuityTransitionReceiptSummary{}, "", fmt.Errorf("unsupported anchor mode %q (expected latest|receipt)", rawMode)
		}
	}
	switch mode {
	case incidenttriage.AnchorModeLatestTransition:
		if receiptID != "" {
			return ContinuityTransitionReceiptSummary{}, "", fmt.Errorf("anchor receipt id cannot be combined with anchor mode latest")
		}
		record, err := c.store.TransitionReceipts().LatestByTask(taskID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ContinuityTransitionReceiptSummary{}, "", fmt.Errorf("no continuity transition receipt exists for task %s", taskID)
			}
			return ContinuityTransitionReceiptSummary{}, "", err
		}
		return continuityTransitionReceiptSummary(record), mode, nil
	case incidenttriage.AnchorModeTransitionID:
		if receiptID == "" {
			return ContinuityTransitionReceiptSummary{}, "", fmt.Errorf("anchor receipt id is required when anchor mode is receipt")
		}
		record, err := c.store.TransitionReceipts().GetByTaskReceipt(taskID, receiptID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ContinuityTransitionReceiptSummary{}, "", fmt.Errorf("anchor transition receipt %s was not found for task %s", receiptID, taskID)
			}
			return ContinuityTransitionReceiptSummary{}, "", err
		}
		return continuityTransitionReceiptSummary(record), mode, nil
	default:
		return ContinuityTransitionReceiptSummary{}, "", fmt.Errorf("unsupported anchor mode %q", rawMode)
	}
}

func parseContinuityIncidentTriagePosture(raw string) (incidenttriage.Posture, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	switch normalized {
	case "triaged":
		return incidenttriage.PostureTriaged, nil
	case "needs_follow_up":
		return incidenttriage.PostureNeedsFollowUp, nil
	case "deferred":
		return incidenttriage.PostureDeferred, nil
	default:
		return "", fmt.Errorf("invalid posture %q (expected triaged|needs_follow_up|deferred)", raw)
	}
}

func normalizeContinuityIncidentTriageSummary(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) > maxContinuityIncidentTriageSummaryChars {
		return trimmed[:maxContinuityIncidentTriageSummaryChars]
	}
	return trimmed
}

func followUpPostureFromTriagePosture(posture incidenttriage.Posture) incidenttriage.FollowUpPosture {
	switch posture {
	case incidenttriage.PostureNeedsFollowUp:
		return incidenttriage.FollowUpPostureAdvisory
	case incidenttriage.PostureDeferred:
		return incidenttriage.FollowUpPostureDeferred
	default:
		return incidenttriage.FollowUpPostureNone
	}
}

func (c *Coordinator) continuityIncidentRiskSnapshotForAnchor(taskID common.TaskID, anchor ContinuityTransitionReceiptSummary) (ContinuityIncidentRiskSummary, error) {
	runs, err := c.incidentRunsNearAnchor(taskID, anchor.CreatedAt, defaultContinuityIncidentRunLimit)
	if err != nil {
		return ContinuityIncidentRiskSummary{}, err
	}
	recoveryActions, err := c.incidentRecoveryActionsNearAnchor(taskID, anchor.CreatedAt, defaultContinuityIncidentRecoveryLimit)
	if err != nil {
		return ContinuityIncidentRiskSummary{}, err
	}
	failedOrInterrupted := 0
	for _, run := range runs {
		if run.Status == rundomain.StatusFailed || run.Status == rundomain.StatusInterrupted {
			failedOrInterrupted++
		}
	}
	return continuityIncidentRiskSummaryFromAnchor(anchor, failedOrInterrupted, len(recoveryActions)), nil
}

func continuityIncidentTriageReceiptSummary(record incidenttriage.Receipt) ContinuityIncidentTriageReceiptSummary {
	return ContinuityIncidentTriageReceiptSummary{
		ReceiptID:                 record.ReceiptID,
		TaskID:                    record.TaskID,
		AnchorMode:                record.AnchorMode,
		AnchorTransitionReceiptID: record.AnchorTransitionReceiptID,
		AnchorTransitionKind:      record.AnchorTransitionKind,
		AnchorHandoffID:           record.AnchorHandoffID,
		AnchorShellSessionID:      record.AnchorShellSessionID,
		Posture:                   record.Posture,
		FollowUpPosture:           record.FollowUpPosture,
		Summary:                   record.Summary,
		ReviewGapPresent:          record.ReviewGapPresent,
		ReviewPosture:             record.ReviewPosture,
		ReviewState:               record.ReviewState,
		ReviewScope:               string(record.ReviewScope),
		ReviewedUpToSequence:      record.ReviewedUpToSequence,
		OldestUnreviewedSequence:  record.OldestUnreviewedSequence,
		NewestRetainedSequence:    record.NewestRetainedSequence,
		UnreviewedRetainedCount:   record.UnreviewedRetainedCount,
		LatestReviewID:            record.LatestReviewID,
		LatestReviewGapAckID:      record.LatestReviewGapAckID,
		AcknowledgmentPresent:     record.AcknowledgmentPresent,
		AcknowledgmentClass:       string(record.AcknowledgmentClass),
		RiskSummary: ContinuityIncidentRiskSummary{
			ReviewGapPresent:                record.RiskReviewGapPresent,
			AcknowledgmentPresent:           record.RiskAcknowledgmentPresent,
			StaleOrUnreviewedReviewPosture:  record.RiskStaleOrUnreviewedReviewPosture,
			SourceScopedReviewPosture:       record.RiskSourceScopedReviewPosture,
			IntoClaudeOwnershipTransition:   record.RiskIntoClaudeOwnershipTransition,
			BackToLocalOwnershipTransition:  record.RiskBackToLocalOwnershipTransition,
			UnresolvedContinuityAmbiguity:   record.RiskUnresolvedContinuityAmbiguity,
			NearbyFailedOrInterruptedRuns:   record.RiskNearbyFailedOrInterruptedRuns,
			NearbyRecoveryActions:           record.RiskNearbyRecoveryActions,
			RecentFailureOrRecoveryActivity: record.RiskRecentFailureOrRecoveryActivity,
			OperationallyNotable:            record.RiskOperationallyNotable,
			Summary:                         record.RiskSummary,
		},
		CreatedAt: record.CreatedAt,
	}
}

func (c *Coordinator) continuityIncidentTriageProjection(taskID common.TaskID, limit int) (*ContinuityIncidentTriageReceiptSummary, []ContinuityIncidentTriageReceiptSummary, error) {
	if limit <= 0 {
		limit = defaultContinuityIncidentTriageHistoryLimit
	}
	if limit > maxContinuityIncidentTriageHistoryLimit {
		limit = maxContinuityIncidentTriageHistoryLimit
	}
	records, err := c.store.IncidentTriages().ListByTask(taskID, limit)
	if err != nil {
		return nil, nil, err
	}
	if len(records) == 0 {
		return nil, nil, nil
	}
	out := make([]ContinuityIncidentTriageReceiptSummary, 0, len(records))
	for _, record := range records {
		out = append(out, continuityIncidentTriageReceiptSummary(record))
	}
	latest := out[0]
	return &latest, out, nil
}

func (c *Coordinator) continuityIncidentTriageHistoryProjection(taskID common.TaskID, limit int) (*ContinuityIncidentTriageReceiptSummary, []ContinuityIncidentTriageReceiptSummary, *ContinuityIncidentTriageHistoryRollupSummary, error) {
	readOut, err := c.ReadContinuityIncidentTriageHistory(context.Background(), ReadContinuityIncidentTriageHistoryRequest{
		TaskID: string(taskID),
		Limit:  limit,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	if len(readOut.Receipts) == 0 {
		return nil, nil, nil, nil
	}
	rollup := readOut.Rollup
	return readOut.Latest, append([]ContinuityIncidentTriageReceiptSummary{}, readOut.Receipts...), &rollup, nil
}

func continuityIncidentFollowUpReceiptSummary(record incidenttriage.FollowUpReceipt) ContinuityIncidentFollowUpReceiptSummary {
	return ContinuityIncidentFollowUpReceiptSummary{
		ReceiptID:                 record.ReceiptID,
		TaskID:                    record.TaskID,
		AnchorMode:                record.AnchorMode,
		AnchorTransitionReceiptID: record.AnchorTransitionReceiptID,
		AnchorTransitionKind:      record.AnchorTransitionKind,
		AnchorHandoffID:           record.AnchorHandoffID,
		AnchorShellSessionID:      record.AnchorShellSessionID,
		TriageReceiptID:           record.TriageReceiptID,
		TriagePosture:             record.TriagePosture,
		TriageFollowUpState:       record.TriageFollowUpState,
		ActionKind:                record.ActionKind,
		Summary:                   record.Summary,
		ReviewGapPresent:          record.ReviewGapPresent,
		ReviewPosture:             record.ReviewPosture,
		ReviewState:               record.ReviewState,
		ReviewScope:               string(record.ReviewScope),
		ReviewedUpToSequence:      record.ReviewedUpToSequence,
		OldestUnreviewedSequence:  record.OldestUnreviewedSequence,
		NewestRetainedSequence:    record.NewestRetainedSequence,
		UnreviewedRetainedCount:   record.UnreviewedRetainedCount,
		LatestReviewID:            record.LatestReviewID,
		LatestReviewGapAckID:      record.LatestReviewGapAckID,
		AcknowledgmentPresent:     record.AcknowledgmentPresent,
		AcknowledgmentClass:       string(record.AcknowledgmentClass),
		TriagedUnderReviewRisk:    record.TriagedUnderReviewRisk,
		CreatedAt:                 record.CreatedAt,
	}
}

func deriveContinuityIncidentFollowUpSummary(latestTransition *ContinuityTransitionReceiptSummary, latestTriage *ContinuityIncidentTriageReceiptSummary, latestFollowUp *ContinuityIncidentFollowUpReceiptSummary) *ContinuityIncidentFollowUpSummary {
	if latestTransition == nil && latestTriage == nil {
		return nil
	}
	out := &ContinuityIncidentFollowUpSummary{}
	if latestTransition != nil {
		out.LatestTransitionReceiptID = latestTransition.ReceiptID
	}
	if latestTriage != nil {
		out.LatestTriageReceiptID = latestTriage.ReceiptID
		out.TriageAnchorReceiptID = latestTriage.AnchorTransitionReceiptID
		out.TriagePosture = latestTriage.Posture
	}
	if latestFollowUp != nil {
		out.LatestFollowUpReceiptID = latestFollowUp.ReceiptID
		out.LatestFollowUpActionKind = latestFollowUp.ActionKind
		out.LatestFollowUpSummary = latestFollowUp.Summary
		out.LatestFollowUpAt = latestFollowUp.CreatedAt
		out.FollowUpReceiptPresent = true
	}
	if latestTransition == nil {
		out.State = ContinuityIncidentFollowUpNone
		out.Advisory = "Continuity incident triage receipts are historical evidence only until a latest continuity transition anchor exists."
		return out
	}
	if latestTriage == nil {
		out.State = ContinuityIncidentFollowUpUntriaged
		out.FollowUpAdvised = true
		out.NeedsFollowUp = true
		out.Advisory = "Latest continuity incident has not been triaged yet; operator triage is advised before progression."
		return out
	}
	if latestTriage.AnchorTransitionReceiptID != latestTransition.ReceiptID {
		out.State = ContinuityIncidentFollowUpTriageBehindLatest
		out.FollowUpAdvised = true
		out.NeedsFollowUp = true
		out.TriageBehindLatest = true
		out.Advisory = fmt.Sprintf(
			"Latest continuity incident triage is anchored to %s while newest transition is %s; triage the latest incident anchor for current audit closure.",
			latestTriage.AnchorTransitionReceiptID,
			latestTransition.ReceiptID,
		)
		return out
	}
	switch latestTriage.Posture {
	case incidenttriage.PostureNeedsFollowUp:
		out.State = ContinuityIncidentFollowUpNeedsFollowUp
		out.FollowUpAdvised = true
		out.NeedsFollowUp = true
		out.FollowUpOpen = true
		out.Advisory = "Latest continuity incident triage is marked NEEDS_FOLLOW_UP; operator follow-up is still advised."
	case incidenttriage.PostureDeferred:
		out.State = ContinuityIncidentFollowUpDeferred
		out.FollowUpAdvised = true
		out.Deferred = true
		out.FollowUpOpen = true
		out.Advisory = "Latest continuity incident triage is DEFERRED; follow-up remains intentionally open."
	default:
		out.State = ContinuityIncidentFollowUpTriagedCurrent
		if latestTriage.RiskSummary.StaleOrUnreviewedReviewPosture || latestTriage.RiskSummary.SourceScopedReviewPosture {
			out.TriagedUnderReviewRisk = true
			out.Advisory = "Latest continuity incident was triaged under stale or source-scoped retained transcript review posture."
		} else {
			out.Advisory = "Latest continuity incident triage is current within retained evidence bounds."
		}
	}
	if latestFollowUp != nil && latestFollowUp.AnchorTransitionReceiptID == latestTriage.AnchorTransitionReceiptID {
		switch latestFollowUp.ActionKind {
		case incidenttriage.FollowUpActionRecordedPending:
			out.State = ContinuityIncidentFollowUpPending
			out.FollowUpAdvised = true
			out.NeedsFollowUp = true
			out.FollowUpOpen = true
			out.Advisory = "Latest incident follow-up receipt is RECORDED_PENDING; follow-up remains open."
		case incidenttriage.FollowUpActionProgressed:
			out.State = ContinuityIncidentFollowUpProgressed
			out.FollowUpAdvised = true
			out.NeedsFollowUp = true
			out.FollowUpProgressed = true
			out.FollowUpOpen = true
			out.Advisory = "Latest incident follow-up receipt is PROGRESSED; explicit closure remains open."
		case incidenttriage.FollowUpActionClosed:
			out.State = ContinuityIncidentFollowUpClosed
			out.FollowUpAdvised = false
			out.NeedsFollowUp = false
			out.Deferred = false
			out.FollowUpClosed = true
			out.FollowUpOpen = false
			out.Advisory = "Latest incident follow-up receipt is CLOSED within this bounded audit workflow."
		case incidenttriage.FollowUpActionReopened:
			out.State = ContinuityIncidentFollowUpReopened
			out.FollowUpAdvised = true
			out.NeedsFollowUp = true
			out.FollowUpReopened = true
			out.FollowUpOpen = true
			out.Advisory = "Latest incident follow-up receipt is REOPENED; follow-up remains explicitly open."
		}
		if note := strings.TrimSpace(latestFollowUp.Summary); note != "" {
			out.Advisory = appendOperatorSentence(out.Advisory, note)
		}
	}
	return out
}

func deriveFollowUpAwareAdvisory(summary *ContinuityIncidentFollowUpSummary, rollup *ContinuityIncidentFollowUpHistoryRollupSummary, receipts []ContinuityIncidentFollowUpReceiptSummary) *ContinuityIncidentFollowUpSummary {
	if summary == nil {
		return nil
	}
	if followUpTriagedWithoutReceipt(summary) {
		summary.FollowUpAdvised = true
		summary.NeedsFollowUp = true
		summary.FollowUpOpen = true
		summary.Advisory = appendOperatorSentence(
			summary.Advisory,
			"Latest continuity incident anchor is triaged, but no follow-up receipt is recorded yet.",
		)
	}
	if rollup != nil {
		if note := continuityIncidentFollowUpRollupAdvisory(rollup); note != "" {
			summary.Advisory = appendOperatorSentence(summary.Advisory, note)
		}
	}
	closure := deriveContinuityIncidentClosureSummary(rollup, receipts)
	summary.ClosureIntelligence = closure
	if closure != nil {
		if note := strings.TrimSpace(closure.Detail); note != "" {
			summary.Advisory = appendOperatorSentence(summary.Advisory, note)
		}
		if closure.OperationallyUnresolved {
			summary.FollowUpAdvised = true
			summary.NeedsFollowUp = true
		}
	}
	if summary.FollowUpClosed {
		summary.Advisory = appendOperatorSentence(
			summary.Advisory,
			"Follow-up closure is an audit marker only and does not certify correctness, completion, or resumability.",
		)
	}
	summary.Digest = deriveContinuityIncidentFollowUpDigest(summary)
	summary.WindowAdvisory = deriveContinuityIncidentFollowUpWindowAdvisory(rollup)
	return summary
}

func followUpTriagedWithoutReceipt(summary *ContinuityIncidentFollowUpSummary) bool {
	if summary == nil {
		return false
	}
	if summary.FollowUpReceiptPresent {
		return false
	}
	if summary.LatestTransitionReceiptID == "" || summary.LatestTriageReceiptID == "" || summary.TriageAnchorReceiptID == "" {
		return false
	}
	return summary.TriageAnchorReceiptID == summary.LatestTransitionReceiptID
}

func continuityIncidentFollowUpRollupAdvisory(rollup *ContinuityIncidentFollowUpHistoryRollupSummary) string {
	if rollup == nil || rollup.WindowSize <= 0 {
		return ""
	}
	notes := make([]string, 0, 4)
	if rollup.AnchorsTriagedWithoutFollowUp > 0 {
		notes = append(notes, fmt.Sprintf("%d triaged anchor(s) have no follow-up receipt in this bounded window", rollup.AnchorsTriagedWithoutFollowUp))
	}
	if rollup.AnchorsWithOpenFollowUp > 0 {
		notes = append(notes, fmt.Sprintf("%d anchor(s) remain in open follow-up posture in this bounded window", rollup.AnchorsWithOpenFollowUp))
	}
	if rollup.AnchorsReopened > 0 {
		notes = append(notes, fmt.Sprintf("%d anchor(s) were reopened in this bounded window", rollup.AnchorsReopened))
	}
	if rollup.AnchorsRepeatedWithoutProgression > 0 {
		notes = append(notes, fmt.Sprintf("%d anchor(s) show repeated follow-up progression without closure in this bounded window", rollup.AnchorsRepeatedWithoutProgression))
	}
	if len(notes) == 0 {
		return ""
	}
	return strings.Join(notes, "; ") + "."
}

func deriveContinuityIncidentFollowUpDigest(summary *ContinuityIncidentFollowUpSummary) string {
	if summary == nil {
		return ""
	}
	if followUpTriagedWithoutReceipt(summary) {
		return "triaged without follow-up"
	}
	switch summary.State {
	case ContinuityIncidentFollowUpUntriaged:
		return "latest anchor untriaged"
	case ContinuityIncidentFollowUpTriageBehindLatest:
		return "triage behind latest anchor"
	case ContinuityIncidentFollowUpNeedsFollowUp, ContinuityIncidentFollowUpDeferred, ContinuityIncidentFollowUpPending, ContinuityIncidentFollowUpProgressed:
		return "follow-up open"
	case ContinuityIncidentFollowUpReopened:
		return "follow-up reopened"
	case ContinuityIncidentFollowUpClosed:
		return "follow-up closed (audit only)"
	case ContinuityIncidentFollowUpTriagedCurrent:
		return "follow-up posture current"
	default:
		return ""
	}
}

func deriveContinuityIncidentFollowUpWindowAdvisory(rollup *ContinuityIncidentFollowUpHistoryRollupSummary) string {
	if rollup == nil || rollup.WindowSize <= 0 {
		return ""
	}
	parts := make([]string, 0, 4)
	if rollup.AnchorsTriagedWithoutFollowUp > 0 {
		parts = append(parts, fmt.Sprintf("triaged-without-follow-up=%d", rollup.AnchorsTriagedWithoutFollowUp))
	}
	if rollup.AnchorsWithOpenFollowUp > 0 {
		parts = append(parts, fmt.Sprintf("open=%d", rollup.AnchorsWithOpenFollowUp))
	}
	if rollup.AnchorsReopened > 0 {
		parts = append(parts, fmt.Sprintf("reopened=%d", rollup.AnchorsReopened))
	}
	if rollup.AnchorsRepeatedWithoutProgression > 0 {
		parts = append(parts, fmt.Sprintf("repeated-progression=%d", rollup.AnchorsRepeatedWithoutProgression))
	}
	if len(parts) == 0 {
		return ""
	}
	return "bounded window " + strings.Join(parts, " ")
}

func (c *Coordinator) continuityIncidentTriageReadProjection(taskID common.TaskID) (*ContinuityTransitionReceiptSummary, []ContinuityIncidentTriageReceiptSummary, *ContinuityIncidentFollowUpSummary, *ContinuityIncidentFollowUpReceiptSummary, error) {
	latestTransition, _, err := c.continuityTransitionReceiptProjection(taskID, defaultContinuityTransitionReceiptHistoryLimit)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	latestTriage, recentTriages, err := c.continuityIncidentTriageProjection(taskID, defaultContinuityIncidentTriageHistoryLimit)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	var latestFollowUp *ContinuityIncidentFollowUpReceiptSummary
	if latestTriage != nil {
		record, followErr := c.store.IncidentFollowUps().LatestByTaskAnchor(taskID, latestTriage.AnchorTransitionReceiptID)
		if followErr == nil {
			mapped := continuityIncidentFollowUpReceiptSummary(record)
			latestFollowUp = &mapped
		} else if !errors.Is(followErr, sql.ErrNoRows) {
			return nil, nil, nil, nil, followErr
		}
	}
	followUp := deriveContinuityIncidentFollowUpSummary(latestTransition, latestTriage, latestFollowUp)
	followUp = deriveFollowUpAwareAdvisory(followUp, nil, nil)
	return latestTransition, append([]ContinuityIncidentTriageReceiptSummary{}, recentTriages...), followUp, latestFollowUp, nil
}

func applyContinuityIncidentFollowUpToOperatorDecision(summary *OperatorDecisionSummary, followUp *ContinuityIncidentFollowUpSummary) {
	if summary == nil || followUp == nil {
		return
	}
	if note := strings.TrimSpace(followUp.Digest); note != "" {
		summary.Guidance = appendOperatorSentence(summary.Guidance, "Follow-up advisory: "+note)
	}
	if note := strings.TrimSpace(followUp.WindowAdvisory); note != "" {
		summary.Guidance = appendOperatorSentence(summary.Guidance, note)
	}
	if note := strings.TrimSpace(followUp.Advisory); note != "" {
		summary.Guidance = appendOperatorSentence(summary.Guidance, note)
	}
	if followUp.FollowUpAdvised {
		summary.IntegrityNote = appendOperatorSentence(summary.IntegrityNote, "Continuity incident triage follow-up remains open; guidance is advisory and does not override authority constraints.")
	}
	if followUp.FollowUpClosed {
		summary.IntegrityNote = appendOperatorSentence(summary.IntegrityNote, "Incident follow-up closure remains bounded audit evidence and does not imply correctness or completion.")
	}
}

func applyContinuityIncidentFollowUpToOperatorExecutionPlan(plan *OperatorExecutionPlan, followUp *ContinuityIncidentFollowUpSummary) {
	if plan == nil || plan.PrimaryStep == nil || followUp == nil {
		return
	}
	if note := strings.TrimSpace(followUp.Digest); note != "" {
		plan.PrimaryStep.Reason = appendOperatorSentence(plan.PrimaryStep.Reason, "Follow-up advisory: "+note)
	}
	if note := strings.TrimSpace(followUp.WindowAdvisory); note != "" {
		plan.PrimaryStep.Reason = appendOperatorSentence(plan.PrimaryStep.Reason, note)
	}
	if note := strings.TrimSpace(followUp.Advisory); note != "" {
		plan.PrimaryStep.Reason = appendOperatorSentence(plan.PrimaryStep.Reason, note)
	}
}
