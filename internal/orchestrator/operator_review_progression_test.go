package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"tuku/internal/domain/handoff"
	"tuku/internal/domain/proof"
	rundomain "tuku/internal/domain/run"
	"tuku/internal/domain/shellsession"
)

func TestReviewAwareProgressionStaleAdvisoryConsistentAcrossSurfaces(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:    string(taskID),
		SessionID: "shs_review_stale",
		HostMode:  "transcript",
		HostState: "transcript-only",
		StartedAt: time.Unix(1712000000, 0).UTC(),
		Active:    true,
	}); err != nil {
		t.Fatalf("report shell session: %v", err)
	}
	if _, err := coord.RecordShellTranscript(context.Background(), RecordShellTranscriptRequest{
		TaskID:    string(taskID),
		SessionID: "shs_review_stale",
		Chunks: []RecordShellTranscriptChunk{
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "line 1", CreatedAt: time.Unix(1712000001, 0).UTC()},
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "line 2", CreatedAt: time.Unix(1712000002, 0).UTC()},
		},
	}); err != nil {
		t.Fatalf("record transcript: %v", err)
	}
	if _, err := coord.RecordShellTranscriptReview(context.Background(), RecordShellTranscriptReviewRequest{
		TaskID:          string(taskID),
		SessionID:       "shs_review_stale",
		ReviewedUpToSeq: 1,
		Summary:         "reviewed initial retained evidence",
	}); err != nil {
		t.Fatalf("record transcript review: %v", err)
	}

	statusOut, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	shellOut, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}

	statusDecision := requireOperatorDecision(t, statusOut.OperatorDecision)
	inspectDecision := requireOperatorDecision(t, inspectOut.OperatorDecision)
	shellDecision := requireShellDecision(t, shellOut.OperatorDecision)
	if !strings.Contains(statusDecision.Guidance, "Transcript review is stale for shell session shs_review_stale") {
		t.Fatalf("expected stale transcript advisory in status guidance, got %q", statusDecision.Guidance)
	}
	if !strings.Contains(statusDecision.Guidance, "sequence 2") {
		t.Fatalf("expected stale sequence boundary in status guidance, got %q", statusDecision.Guidance)
	}
	if statusDecision.Guidance != inspectDecision.Guidance || statusDecision.Guidance != shellDecision.Guidance {
		t.Fatalf("expected consistent stale guidance across status/inspect/shell, got status=%q inspect=%q shell=%q", statusDecision.Guidance, inspectDecision.Guidance, shellDecision.Guidance)
	}
	if !strings.Contains(statusDecision.IntegrityNote, "oldest unreviewed sequence 2") {
		t.Fatalf("expected stale integrity note, got %q", statusDecision.IntegrityNote)
	}

	statusPlan := requireExecutionPlan(t, statusOut.OperatorExecutionPlan)
	inspectPlan := requireExecutionPlan(t, inspectOut.OperatorExecutionPlan)
	shellPlan := requireShellExecutionPlan(t, shellOut.OperatorExecutionPlan)
	if statusPlan.PrimaryStep.Action != OperatorActionStartLocalRun {
		t.Fatalf("expected required action unchanged, got %+v", statusPlan.PrimaryStep)
	}
	if !strings.Contains(statusPlan.PrimaryStep.Reason, "starting at sequence 2") {
		t.Fatalf("expected stale-aware primary-step reason, got %q", statusPlan.PrimaryStep.Reason)
	}
	if statusPlan.PrimaryStep.Reason != inspectPlan.PrimaryStep.Reason || statusPlan.PrimaryStep.Reason != shellPlan.PrimaryStep.Reason {
		t.Fatalf("expected consistent stale plan reason across status/inspect/shell, got status=%q inspect=%q shell=%q", statusPlan.PrimaryStep.Reason, inspectPlan.PrimaryStep.Reason, shellPlan.PrimaryStep.Reason)
	}
}

func TestReviewAwareProgressionUnreviewedAdvisoryPreservesRequiredNextAction(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:    string(taskID),
		SessionID: "shs_review_unreviewed",
		HostMode:  "transcript",
		HostState: "transcript-only",
		StartedAt: time.Unix(1712100000, 0).UTC(),
		Active:    true,
	}); err != nil {
		t.Fatalf("report shell session: %v", err)
	}
	if _, err := coord.RecordShellTranscript(context.Background(), RecordShellTranscriptRequest{
		TaskID:    string(taskID),
		SessionID: "shs_review_unreviewed",
		Chunks: []RecordShellTranscriptChunk{
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "line 1", CreatedAt: time.Unix(1712100001, 0).UTC()},
			{Source: shellsession.TranscriptSourceSystemNote, Content: "line 2", CreatedAt: time.Unix(1712100002, 0).UTC()},
		},
	}); err != nil {
		t.Fatalf("record transcript: %v", err)
	}

	statusOut, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	decision := requireOperatorDecision(t, statusOut.OperatorDecision)
	if decision.RequiredNextAction != OperatorActionStartLocalRun {
		t.Fatalf("expected required-next action to remain start local run, got %+v", decision)
	}
	if !strings.Contains(decision.Guidance, "has not been operator-reviewed yet") {
		t.Fatalf("expected unreviewed transcript advisory in guidance, got %q", decision.Guidance)
	}
	if !strings.Contains(decision.IntegrityNote, "has no review marker yet") {
		t.Fatalf("expected unreviewed transcript integrity note, got %q", decision.IntegrityNote)
	}

	plan := requireExecutionPlan(t, statusOut.OperatorExecutionPlan)
	if plan.PrimaryStep.Action != OperatorActionStartLocalRun {
		t.Fatalf("expected primary action unchanged, got %+v", plan.PrimaryStep)
	}
	if !strings.Contains(plan.PrimaryStep.Reason, "Retained transcript evidence is unreviewed") {
		t.Fatalf("expected unreviewed plan reason, got %q", plan.PrimaryStep.Reason)
	}
}

func TestReviewAwareProgressionCurrentReviewKeepsGuidanceConservative(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:    string(taskID),
		SessionID: "shs_review_current",
		HostMode:  "transcript",
		HostState: "transcript-only",
		StartedAt: time.Unix(1712200000, 0).UTC(),
		Active:    true,
	}); err != nil {
		t.Fatalf("report shell session: %v", err)
	}
	if _, err := coord.RecordShellTranscript(context.Background(), RecordShellTranscriptRequest{
		TaskID:    string(taskID),
		SessionID: "shs_review_current",
		Chunks: []RecordShellTranscriptChunk{
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "line 1", CreatedAt: time.Unix(1712200001, 0).UTC()},
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "line 2", CreatedAt: time.Unix(1712200002, 0).UTC()},
		},
	}); err != nil {
		t.Fatalf("record transcript: %v", err)
	}
	if _, err := coord.RecordShellTranscriptReview(context.Background(), RecordShellTranscriptReviewRequest{
		TaskID:          string(taskID),
		SessionID:       "shs_review_current",
		ReviewedUpToSeq: 2,
		Summary:         "reviewed latest retained evidence",
	}); err != nil {
		t.Fatalf("record transcript review: %v", err)
	}

	statusOut, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	decision := requireOperatorDecision(t, statusOut.OperatorDecision)
	if strings.Contains(decision.Guidance, "has not been operator-reviewed yet") || strings.Contains(decision.Guidance, "Transcript review is stale") {
		t.Fatalf("expected no stale/unreviewed transcript advisory when current, got %q", decision.Guidance)
	}
}

func TestReviewAwareProgressionDoesNotOverrideHandoffBranchRequiredAction(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "review-aware progression should stay advisory",
		Mode:         handoff.ModeResume,
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  createOut.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
	}); err != nil {
		t.Fatalf("accept handoff: %v", err)
	}

	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:    string(taskID),
		SessionID: "shs_handoff_review",
		HostMode:  "transcript",
		HostState: "transcript-only",
		StartedAt: time.Unix(1712300000, 0).UTC(),
		Active:    true,
	}); err != nil {
		t.Fatalf("report shell session: %v", err)
	}
	if _, err := coord.RecordShellTranscript(context.Background(), RecordShellTranscriptRequest{
		TaskID:    string(taskID),
		SessionID: "shs_handoff_review",
		Chunks: []RecordShellTranscriptChunk{
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "line 1", CreatedAt: time.Unix(1712300001, 0).UTC()},
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "line 2", CreatedAt: time.Unix(1712300002, 0).UTC()},
		},
	}); err != nil {
		t.Fatalf("record transcript: %v", err)
	}
	if _, err := coord.RecordShellTranscriptReview(context.Background(), RecordShellTranscriptReviewRequest{
		TaskID:          string(taskID),
		SessionID:       "shs_handoff_review",
		ReviewedUpToSeq: 1,
		Source:          string(shellsession.TranscriptSourceWorkerOutput),
		Summary:         "reviewed worker output slice",
	}); err != nil {
		t.Fatalf("record transcript review: %v", err)
	}

	statusOut, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	decision := requireOperatorDecision(t, statusOut.OperatorDecision)
	plan := requireExecutionPlan(t, statusOut.OperatorExecutionPlan)
	if decision.RequiredNextAction != OperatorActionLaunchAcceptedHandoff {
		t.Fatalf("expected handoff required-next action to remain dominant, got %+v", decision)
	}
	if plan.PrimaryStep.Action != OperatorActionLaunchAcceptedHandoff {
		t.Fatalf("expected handoff launch primary step to remain dominant, got %+v", plan.PrimaryStep)
	}
	if !strings.Contains(decision.Guidance, "source-scoped (worker_output) and stale") {
		t.Fatalf("expected source-scoped stale advisory in guidance, got %q", decision.Guidance)
	}
	if !strings.Contains(decision.IntegrityNote, "source-scoped to worker_output") {
		t.Fatalf("expected source-scope integrity note, got %q", decision.IntegrityNote)
	}
}

func TestRecordOperatorReviewGapAcknowledgmentPersistsAndSurfacesAcrossReadModels(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	base := time.Unix(1712400000, 0).UTC()

	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:    string(taskID),
		SessionID: "shs_ack_gap",
		HostMode:  "transcript",
		HostState: "transcript-only",
		StartedAt: base,
		Active:    true,
	}); err != nil {
		t.Fatalf("report shell session: %v", err)
	}
	if _, err := coord.RecordShellTranscript(context.Background(), RecordShellTranscriptRequest{
		TaskID:    string(taskID),
		SessionID: "shs_ack_gap",
		Chunks: []RecordShellTranscriptChunk{
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "line 1", CreatedAt: base.Add(1 * time.Second)},
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "line 2", CreatedAt: base.Add(2 * time.Second)},
		},
	}); err != nil {
		t.Fatalf("record shell transcript: %v", err)
	}

	out, err := coord.RecordOperatorReviewGapAcknowledgment(context.Background(), RecordOperatorReviewGapAcknowledgmentRequest{
		TaskID:    string(taskID),
		SessionID: "shs_ack_gap",
		Summary:   "proceeding with unreviewed retained transcript evidence",
	})
	if err != nil {
		t.Fatalf("record operator review-gap acknowledgment: %v", err)
	}
	if out.Acknowledgment.AcknowledgmentID == "" || out.Acknowledgment.Class != shellsession.TranscriptReviewGapAckMissingReviewMarker {
		t.Fatalf("unexpected acknowledgment result: %+v", out)
	}
	if out.ReviewGapState == "" || !strings.Contains(out.ReviewGapState, "unreviewed") {
		t.Fatalf("expected unreviewed review-gap state, got %+v", out)
	}

	statusOut, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if statusOut.LatestTranscriptReviewGapAcknowledgment == nil || statusOut.LatestTranscriptReviewGapAcknowledgment.AcknowledgmentID != out.Acknowledgment.AcknowledgmentID {
		t.Fatalf("expected status to surface latest review-gap acknowledgment, got %+v", statusOut.LatestTranscriptReviewGapAcknowledgment)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.LatestTranscriptReviewGapAcknowledgment == nil || inspectOut.LatestTranscriptReviewGapAcknowledgment.AcknowledgmentID != out.Acknowledgment.AcknowledgmentID {
		t.Fatalf("expected inspect to surface latest review-gap acknowledgment, got %+v", inspectOut.LatestTranscriptReviewGapAcknowledgment)
	}

	shellOut, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if shellOut.LatestTranscriptReviewGapAcknowledgment == nil || shellOut.LatestTranscriptReviewGapAcknowledgment.AcknowledgmentID != out.Acknowledgment.AcknowledgmentID {
		t.Fatalf("expected shell snapshot to surface latest review-gap acknowledgment, got %+v", shellOut.LatestTranscriptReviewGapAcknowledgment)
	}

	events, err := store.Proofs().ListByTask(taskID, 200)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	found := false
	for _, evt := range events {
		if evt.Type == proof.EventTranscriptReviewGapAcknowledged {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected transcript review-gap acknowledgment proof event, got %+v", events)
	}
}

func TestExecutePrimaryOperatorStepAcknowledgesReviewGapWhenExplicitlyRequested(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	base := time.Unix(1712500000, 0).UTC()

	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:    string(taskID),
		SessionID: "shs_step_ack",
		HostMode:  "transcript",
		HostState: "transcript-only",
		StartedAt: base,
		Active:    true,
	}); err != nil {
		t.Fatalf("report shell session: %v", err)
	}
	if _, err := coord.RecordShellTranscript(context.Background(), RecordShellTranscriptRequest{
		TaskID:    string(taskID),
		SessionID: "shs_step_ack",
		Chunks: []RecordShellTranscriptChunk{
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "line 1", CreatedAt: base.Add(1 * time.Second)},
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "line 2", CreatedAt: base.Add(2 * time.Second)},
		},
	}); err != nil {
		t.Fatalf("record shell transcript: %v", err)
	}

	out, err := coord.ExecutePrimaryOperatorStep(context.Background(), ExecutePrimaryOperatorStepRequest{
		TaskID:               string(taskID),
		AcknowledgeReviewGap: true,
		ReviewGapSessionID:   "shs_step_ack",
		ReviewGapSummary:     "continue despite unreviewed retained transcript evidence",
	})
	if err != nil {
		t.Fatalf("execute primary operator step with review-gap acknowledgment: %v", err)
	}
	if !out.Receipt.ReviewGapPresent || !out.Receipt.ReviewGapAcknowledged || out.Receipt.ReviewGapAcknowledgmentID == "" {
		t.Fatalf("expected receipt to capture explicit review-gap acknowledgment linkage, got %+v", out.Receipt)
	}
	if out.LatestTranscriptReviewGapAcknowledgment == nil || out.LatestTranscriptReviewGapAcknowledgment.AcknowledgmentID == "" {
		t.Fatalf("expected primary-step result to include latest review-gap acknowledgment summary, got %+v", out.LatestTranscriptReviewGapAcknowledgment)
	}
}

func TestRecordOperatorReviewGapAcknowledgmentRejectsWhenGapIsNotPresent(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	base := time.Unix(1712600000, 0).UTC()

	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:    string(taskID),
		SessionID: "shs_ack_current",
		HostMode:  "transcript",
		HostState: "transcript-only",
		StartedAt: base,
		Active:    true,
	}); err != nil {
		t.Fatalf("report shell session: %v", err)
	}
	if _, err := coord.RecordShellTranscript(context.Background(), RecordShellTranscriptRequest{
		TaskID:    string(taskID),
		SessionID: "shs_ack_current",
		Chunks: []RecordShellTranscriptChunk{
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "line 1", CreatedAt: base.Add(1 * time.Second)},
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "line 2", CreatedAt: base.Add(2 * time.Second)},
		},
	}); err != nil {
		t.Fatalf("record shell transcript: %v", err)
	}
	if _, err := coord.RecordShellTranscriptReview(context.Background(), RecordShellTranscriptReviewRequest{
		TaskID:          string(taskID),
		SessionID:       "shs_ack_current",
		ReviewedUpToSeq: 2,
		Summary:         "reviewed latest retained evidence",
	}); err != nil {
		t.Fatalf("record transcript review: %v", err)
	}

	_, err := coord.RecordOperatorReviewGapAcknowledgment(context.Background(), RecordOperatorReviewGapAcknowledgmentRequest{
		TaskID:    string(taskID),
		SessionID: "shs_ack_current",
		Summary:   "should be rejected",
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "no transcript review gap acknowledgment is required") {
		t.Fatalf("expected no-gap rejection, got %v", err)
	}
}
