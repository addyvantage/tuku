package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/recoveryaction"
	rundomain "tuku/internal/domain/run"
	"tuku/internal/domain/shellsession"
	"tuku/internal/domain/transition"
)

func TestReadContinuityIncidentSliceAnchoredCorrelatesBoundedEvidence(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	briefID := mustCurrentBriefID(t, store, taskID)

	base := time.Unix(1716100000, 0).UTC()
	sessionID := "shs_incident"
	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:         string(taskID),
		SessionID:      sessionID,
		HostMode:       "codex-pty",
		HostState:      "running",
		ResolvedWorker: "codex",
		StartedAt:      base.Add(-1 * time.Minute),
		Active:         true,
	}); err != nil {
		t.Fatalf("report shell session: %v", err)
	}
	if _, err := coord.RecordShellTranscript(context.Background(), RecordShellTranscriptRequest{
		TaskID:    string(taskID),
		SessionID: sessionID,
		Chunks: []RecordShellTranscriptChunk{
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "chunk 1", CreatedAt: base.Add(1 * time.Second)},
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "chunk 2", CreatedAt: base.Add(2 * time.Second)},
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "chunk 3", CreatedAt: base.Add(3 * time.Second)},
		},
	}); err != nil {
		t.Fatalf("record shell transcript: %v", err)
	}
	reviewOut, err := coord.RecordShellTranscriptReview(context.Background(), RecordShellTranscriptReviewRequest{
		TaskID:          string(taskID),
		SessionID:       sessionID,
		ReviewedUpToSeq: 2,
		Summary:         "reviewed up to seq 2",
	})
	if err != nil {
		t.Fatalf("record shell transcript review: %v", err)
	}
	registry, ok := coord.shellSessions.(*MemoryShellSessionRegistry)
	if !ok {
		t.Fatalf("expected memory shell session registry, got %T", coord.shellSessions)
	}
	if _, err := registry.AppendTranscriptReviewGapAcknowledgment(shellsession.TranscriptReviewGapAcknowledgment{
		AcknowledgmentID:         "sack_incident",
		TaskID:                   taskID,
		SessionID:                sessionID,
		Class:                    shellsession.TranscriptReviewGapAckStaleReview,
		ReviewState:              string(reviewOut.Closure.State),
		ReviewedUpToSequence:     reviewOut.LatestReview.ReviewedUpToSequence,
		OldestUnreviewedSequence: reviewOut.Closure.OldestUnreviewedSequence,
		NewestRetainedSequence:   reviewOut.Closure.NewestRetainedSequence,
		UnreviewedRetainedCount:  reviewOut.Closure.UnreviewedRetainedCount,
		TranscriptState:          reviewOut.LatestReview.TranscriptState,
		RetentionLimit:           reviewOut.LatestReview.RetentionLimit,
		RetainedChunks:           reviewOut.LatestReview.RetainedChunks,
		DroppedChunks:            reviewOut.LatestReview.DroppedChunks,
		ActionContext:            "incident test setup",
		Summary:                  "explicit stale-review acknowledgment",
		CreatedAt:                base.Add(4 * time.Second),
	}); err != nil {
		t.Fatalf("append transcript review gap acknowledgment: %v", err)
	}

	receipts := []transition.Receipt{
		{
			Version:                 1,
			ReceiptID:               "ctr_incident_old2",
			TaskID:                  taskID,
			TransitionKind:          transition.KindHandoffLaunch,
			BranchClassBefore:       string(ActiveBranchClassLocal),
			BranchClassAfter:        string(ActiveBranchClassHandoffClaude),
			HandoffContinuityBefore: string(HandoffContinuityStateAcceptedNotLaunched),
			HandoffContinuityAfter:  string(HandoffContinuityStateLaunchCompletedAckSeen),
			CreatedAt:               base.Add(1 * time.Second),
		},
		{
			Version:                 1,
			ReceiptID:               "ctr_incident_old1",
			TaskID:                  taskID,
			TransitionKind:          transition.KindHandoffLaunch,
			BranchClassBefore:       string(ActiveBranchClassLocal),
			BranchClassAfter:        string(ActiveBranchClassHandoffClaude),
			HandoffContinuityBefore: string(HandoffContinuityStateAcceptedNotLaunched),
			HandoffContinuityAfter:  string(HandoffContinuityStateLaunchCompletedAckSeen),
			CreatedAt:               base.Add(2 * time.Second),
		},
		{
			Version:                 1,
			ReceiptID:               "ctr_incident_anchor",
			TaskID:                  taskID,
			ShellSessionID:          sessionID,
			TransitionKind:          transition.KindHandoffLaunch,
			BranchClassBefore:       string(ActiveBranchClassLocal),
			BranchClassAfter:        string(ActiveBranchClassHandoffClaude),
			HandoffContinuityBefore: string(HandoffContinuityStateAcceptedNotLaunched),
			HandoffContinuityAfter:  string(HandoffContinuityStateLaunchCompletedAckSeen),
			ReviewGapPresent:        true,
			ReviewPosture:           transition.ReviewPostureGlobalReviewStale,
			LatestReviewID:          reviewOut.LatestReview.ReviewID,
			LatestReviewAckID:       "sack_incident",
			AcknowledgmentPresent:   true,
			AcknowledgmentClass:     shellsession.TranscriptReviewGapAckStaleReview,
			Summary:                 "anchor handoff launch recorded with stale transcript review posture",
			CreatedAt:               base.Add(3 * time.Second),
		},
		{
			Version:                 1,
			ReceiptID:               "ctr_incident_new1",
			TaskID:                  taskID,
			TransitionKind:          transition.KindHandoffResolution,
			BranchClassBefore:       string(ActiveBranchClassHandoffClaude),
			BranchClassAfter:        string(ActiveBranchClassLocal),
			HandoffContinuityBefore: string(HandoffContinuityStateFollowThroughStalled),
			HandoffContinuityAfter:  string(HandoffContinuityStateResolved),
			CreatedAt:               base.Add(4 * time.Second),
		},
		{
			Version:                 1,
			ReceiptID:               "ctr_incident_new2",
			TaskID:                  taskID,
			TransitionKind:          transition.KindHandoffResolution,
			BranchClassBefore:       string(ActiveBranchClassHandoffClaude),
			BranchClassAfter:        string(ActiveBranchClassLocal),
			HandoffContinuityBefore: string(HandoffContinuityStateFollowThroughStalled),
			HandoffContinuityAfter:  string(HandoffContinuityStateResolved),
			CreatedAt:               base.Add(5 * time.Second),
		},
	}
	for _, record := range receipts {
		if err := store.TransitionReceipts().Create(record); err != nil {
			t.Fatalf("create transition receipt %s: %v", record.ReceiptID, err)
		}
	}

	endedNear := base.Add(3*time.Second + 200*time.Millisecond)
	if err := store.Runs().Create(rundomain.ExecutionRun{
		RunID:            "run_near",
		TaskID:           taskID,
		BriefID:          briefID,
		WorkerKind:       rundomain.WorkerKindCodex,
		Status:           rundomain.StatusFailed,
		CreatedFromPhase: phase.PhaseExecuting,
		StartedAt:        base.Add(3 * time.Second),
		EndedAt:          &endedNear,
		CreatedAt:        base.Add(3 * time.Second),
		UpdatedAt:        endedNear,
		LastKnownSummary: "near failed run",
	}); err != nil {
		t.Fatalf("create near run: %v", err)
	}
	endedFar := base.Add(25 * time.Second)
	if err := store.Runs().Create(rundomain.ExecutionRun{
		RunID:            "run_far",
		TaskID:           taskID,
		BriefID:          briefID,
		WorkerKind:       rundomain.WorkerKindCodex,
		Status:           rundomain.StatusCompleted,
		CreatedFromPhase: phase.PhaseExecuting,
		StartedAt:        base.Add(20 * time.Second),
		EndedAt:          &endedFar,
		CreatedAt:        base.Add(20 * time.Second),
		UpdatedAt:        endedFar,
		LastKnownSummary: "far completed run",
	}); err != nil {
		t.Fatalf("create far run: %v", err)
	}

	if err := store.RecoveryActions().Create(recoveryaction.Record{
		Version:   1,
		ActionID:  "act_near",
		TaskID:    taskID,
		Kind:      recoveryaction.KindFailedRunReviewed,
		RunID:     "run_near",
		Summary:   "reviewed failed run near anchor",
		CreatedAt: base.Add(3*time.Second + 500*time.Millisecond),
	}); err != nil {
		t.Fatalf("create recovery action: %v", err)
	}
	if err := store.RecoveryActions().Create(recoveryaction.Record{
		Version:   1,
		ActionID:  "act_far",
		TaskID:    taskID,
		Kind:      recoveryaction.KindContinueExecuted,
		RunID:     "run_far",
		Summary:   "continue far from anchor",
		CreatedAt: base.Add(40 * time.Second),
	}); err != nil {
		t.Fatalf("create far recovery action: %v", err)
	}

	if err := store.Proofs().Append(proof.Event{
		EventID:        "evt_near",
		TaskID:         taskID,
		Timestamp:      base.Add(3 * time.Second),
		Type:           proof.EventWorkerRunFailed,
		ActorType:      proof.ActorSystem,
		ActorID:        "tuku",
		PayloadJSON:    `{"summary":"near failure"}`,
		CapsuleVersion: 1,
	}); err != nil {
		t.Fatalf("append near proof: %v", err)
	}
	if err := store.Proofs().Append(proof.Event{
		EventID:        "evt_far",
		TaskID:         taskID,
		Timestamp:      base.Add(45 * time.Second),
		Type:           proof.EventCanonicalResponseEmitted,
		ActorType:      proof.ActorSystem,
		ActorID:        "tuku",
		PayloadJSON:    `{"summary":"far event"}`,
		CapsuleVersion: 1,
	}); err != nil {
		t.Fatalf("append far proof: %v", err)
	}

	out, err := coord.ReadContinuityIncidentSlice(context.Background(), ReadContinuityIncidentSliceRequest{
		TaskID:                    string(taskID),
		AnchorTransitionReceiptID: "ctr_incident_anchor",
		TransitionNeighborLimit:   1,
		RunLimit:                  1,
		RecoveryLimit:             1,
		ProofLimit:                1,
		AckLimit:                  2,
	})
	if err != nil {
		t.Fatalf("read continuity incident slice: %v", err)
	}

	if out.AnchorMode != ContinuityIncidentAnchorTransitionID || out.Anchor.ReceiptID != "ctr_incident_anchor" {
		t.Fatalf("unexpected incident anchor selection: %+v", out)
	}
	if !out.Bounded || out.TransitionNeighborLimit != 1 || out.RunLimit != 1 || out.RecoveryLimit != 1 || out.ProofLimit != 1 || out.AckLimit != 2 {
		t.Fatalf("unexpected incident bounded limits metadata: %+v", out)
	}
	if !out.HasOlderTransitionsOutsideWindow || !out.HasNewerTransitionsOutsideWindow {
		t.Fatalf("expected bounded older/newer overflow metadata, got older=%v newer=%v", out.HasOlderTransitionsOutsideWindow, out.HasNewerTransitionsOutsideWindow)
	}
	if len(out.Transitions) != 3 ||
		out.Transitions[0].ReceiptID != "ctr_incident_old1" ||
		out.Transitions[1].ReceiptID != "ctr_incident_anchor" ||
		out.Transitions[2].ReceiptID != "ctr_incident_new1" {
		t.Fatalf("unexpected bounded transition neighborhood: %+v", out.Transitions)
	}
	if len(out.Runs) != 1 || out.Runs[0].RunID != "run_near" {
		t.Fatalf("expected nearest run only, got %+v", out.Runs)
	}
	if len(out.RecoveryActions) != 1 || out.RecoveryActions[0].ActionID != "act_near" {
		t.Fatalf("expected nearest recovery action only, got %+v", out.RecoveryActions)
	}
	if len(out.ProofEvents) != 1 || out.ProofEvents[0].EventID != "evt_near" {
		t.Fatalf("expected nearest proof event only, got %+v", out.ProofEvents)
	}
	if out.LatestTranscriptReview == nil || out.LatestTranscriptReview.ReviewID != reviewOut.LatestReview.ReviewID {
		t.Fatalf("expected latest transcript review evidence in incident slice, got %+v", out.LatestTranscriptReview)
	}
	if out.LatestTranscriptReviewGapAck == nil || out.LatestTranscriptReviewGapAck.AcknowledgmentID != "sack_incident" {
		t.Fatalf("expected latest transcript review-gap acknowledgment evidence in incident slice, got %+v", out.LatestTranscriptReviewGapAck)
	}
	if len(out.RecentTranscriptReviewGapAcks) != 1 || out.RecentTranscriptReviewGapAcks[0].AcknowledgmentID != "sack_incident" {
		t.Fatalf("expected bounded recent acknowledgment history in incident slice, got %+v", out.RecentTranscriptReviewGapAcks)
	}
	if !out.RiskSummary.ReviewGapPresent || !out.RiskSummary.AcknowledgmentPresent || !out.RiskSummary.StaleOrUnreviewedReviewPosture || !out.RiskSummary.IntoClaudeOwnershipTransition {
		t.Fatalf("unexpected incident risk summary flags: %+v", out.RiskSummary)
	}
	if !strings.Contains(out.Caveat, "do not prove causality") {
		t.Fatalf("expected bounded truth caveat in incident slice output, got %q", out.Caveat)
	}
}

func TestReadContinuityIncidentSliceUsesLatestTransitionWhenAnchorNotProvided(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	base := time.Unix(1716200000, 0).UTC()
	records := []transition.Receipt{
		{
			Version:        1,
			ReceiptID:      "ctr_latest_a",
			TaskID:         taskID,
			TransitionKind: transition.KindHandoffLaunch,
			CreatedAt:      base,
		},
		{
			Version:        1,
			ReceiptID:      "ctr_latest_b",
			TaskID:         taskID,
			TransitionKind: transition.KindHandoffLaunch,
			CreatedAt:      base,
		},
	}
	for _, record := range records {
		if err := store.TransitionReceipts().Create(record); err != nil {
			t.Fatalf("create transition receipt %s: %v", record.ReceiptID, err)
		}
	}

	out, err := coord.ReadContinuityIncidentSlice(context.Background(), ReadContinuityIncidentSliceRequest{
		TaskID:                  string(taskID),
		TransitionNeighborLimit: 1,
	})
	if err != nil {
		t.Fatalf("read continuity incident slice latest anchor: %v", err)
	}
	if out.AnchorMode != ContinuityIncidentAnchorLatestTransition || out.RequestedAnchorTransitionReceiptID != "" {
		t.Fatalf("expected latest-transition anchor mode metadata, got %+v", out)
	}
	if out.Anchor.ReceiptID != "ctr_latest_b" {
		t.Fatalf("expected deterministic latest transition anchor ctr_latest_b, got %+v", out.Anchor)
	}
}

func TestReadContinuityIncidentSliceRejectsUnknownTaskAndAnchor(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	if err := store.TransitionReceipts().Create(transition.Receipt{
		Version:        1,
		ReceiptID:      common.EventID("ctr_existing"),
		TaskID:         taskID,
		TransitionKind: transition.KindHandoffLaunch,
		CreatedAt:      time.Unix(1716300000, 0).UTC(),
	}); err != nil {
		t.Fatalf("create transition receipt: %v", err)
	}

	if _, err := coord.ReadContinuityIncidentSlice(context.Background(), ReadContinuityIncidentSliceRequest{
		TaskID: "tsk_missing",
	}); err == nil {
		t.Fatalf("expected unknown task rejection")
	}

	if _, err := coord.ReadContinuityIncidentSlice(context.Background(), ReadContinuityIncidentSliceRequest{
		TaskID:                    string(taskID),
		AnchorTransitionReceiptID: "ctr_missing",
	}); err == nil || !strings.Contains(err.Error(), "was not found for task") {
		t.Fatalf("expected unknown anchor rejection, got %v", err)
	}
}
