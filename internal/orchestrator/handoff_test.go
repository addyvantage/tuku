package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"tuku/internal/adapters/adapter_contract"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/proof"
	rundomain "tuku/internal/domain/run"
	"tuku/internal/domain/shellsession"
	"tuku/internal/domain/transition"
	anchorgit "tuku/internal/git/anchor"
	"tuku/internal/response/canonical"
	"tuku/internal/storage"
	"tuku/internal/storage/sqlite"
)

func TestCreateHandoffFromSafeResumableState(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	seed, err := coord.CreateCheckpoint(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}
	capsBefore, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule before handoff: %v", err)
	}

	out, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "codex quota exhausted",
		Mode:         handoff.ModeResume,
		Notes:        []string{"prefer minimal diff follow-up"},
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if out.Status != handoff.StatusCreated {
		t.Fatalf("expected created status, got %s", out.Status)
	}
	if out.TargetWorker != rundomain.WorkerKindClaude {
		t.Fatalf("expected claude target worker, got %s", out.TargetWorker)
	}
	if out.Packet == nil {
		t.Fatal("expected handoff packet")
	}
	if out.Packet.CheckpointID != seed.CheckpointID {
		t.Fatalf("expected reused checkpoint %s, got %s", seed.CheckpointID, out.Packet.CheckpointID)
	}
	if out.Packet.TargetWorker != rundomain.WorkerKindClaude {
		t.Fatalf("expected packet target worker claude, got %s", out.Packet.TargetWorker)
	}
	if out.Packet.RepoAnchor.HeadSHA == "" {
		t.Fatal("expected repo anchor in handoff packet")
	}
	if out.Packet.BriefID == "" || out.Packet.IntentID == "" {
		t.Fatalf("expected brief and intent references in packet, got brief=%s intent=%s", out.Packet.BriefID, out.Packet.IntentID)
	}

	persisted, err := store.Handoffs().Get(out.HandoffID)
	if err != nil {
		t.Fatalf("load persisted handoff: %v", err)
	}
	if persisted.HandoffID != out.HandoffID {
		t.Fatalf("expected persisted handoff %s, got %s", out.HandoffID, persisted.HandoffID)
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if !hasEvent(events, proof.EventHandoffCreated) {
		t.Fatal("expected HANDOFF_CREATED proof event")
	}

	capsAfter, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule after handoff: %v", err)
	}
	if capsAfter.Version != capsBefore.Version {
		t.Fatalf("handoff should not mutate capsule version in reuse case: before=%d after=%d", capsBefore.Version, capsAfter.Version)
	}
}

func TestCreateHandoffBlockedOnInconsistentContinuity(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	lastEvent, err := latestEventID(store, taskID)
	if err != nil {
		t.Fatalf("latest event: %v", err)
	}
	bad := checkpoint.Checkpoint{
		Version:            1,
		CheckpointID:       common.CheckpointID("chk_bad_handoff_missing_brief"),
		TaskID:             taskID,
		RunID:              "",
		CreatedAt:          time.Now().UTC().Add(9 * time.Second),
		Trigger:            checkpoint.TriggerManual,
		CapsuleVersion:     caps.Version,
		Phase:              caps.CurrentPhase,
		Anchor:             anchorFromCapsule(caps),
		IntentID:           caps.CurrentIntentID,
		BriefID:            common.BriefID("brf_missing_for_handoff"),
		ContextPackID:      "",
		LastEventID:        lastEvent,
		PendingDecisionIDs: []common.DecisionID{},
		ResumeDescriptor:   "bad checkpoint for handoff consistency test",
		IsResumable:        true,
	}
	if err := store.Checkpoints().Create(bad); err != nil {
		t.Fatalf("create bad checkpoint: %v", err)
	}

	out, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "handoff despite broken continuity",
	})
	if err != nil {
		t.Fatalf("create blocked handoff: %v", err)
	}
	if out.Status != handoff.StatusBlocked {
		t.Fatalf("expected blocked status, got %s", out.Status)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "blocked") {
		t.Fatalf("expected blocked canonical response, got %q", out.CanonicalResponse)
	}
	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if !hasEvent(events, proof.EventHandoffBlocked) {
		t.Fatal("expected HANDOFF_BLOCKED proof event")
	}
}

func TestCreateHandoffCreatesCheckpointWhenReuseNotPossible(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	out, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "handoff without existing checkpoint",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if out.Status != handoff.StatusCreated {
		t.Fatalf("expected created status, got %s", out.Status)
	}
	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint: %v", err)
	}
	if latestCheckpoint.Trigger != checkpoint.TriggerHandoff {
		t.Fatalf("expected handoff trigger on handoff-created checkpoint, got %s", latestCheckpoint.Trigger)
	}
}

func TestCreateHandoffCreatesNewCheckpointWhenLatestCheckpointNotReusable(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	lastEvent, err := latestEventID(store, taskID)
	if err != nil {
		t.Fatalf("latest event: %v", err)
	}
	nonReusable := checkpoint.Checkpoint{
		Version:            1,
		CheckpointID:       common.CheckpointID("chk_non_reusable_for_handoff"),
		TaskID:             taskID,
		RunID:              "",
		CreatedAt:          time.Now().UTC().Add(-1 * time.Second),
		Trigger:            checkpoint.TriggerAwaitingDecision,
		CapsuleVersion:     caps.Version,
		Phase:              caps.CurrentPhase,
		Anchor:             anchorFromCapsule(caps),
		IntentID:           caps.CurrentIntentID,
		BriefID:            caps.CurrentBriefID,
		ContextPackID:      "",
		LastEventID:        lastEvent,
		PendingDecisionIDs: []common.DecisionID{},
		ResumeDescriptor:   "non-resumable checkpoint for reuse guard test",
		IsResumable:        false,
	}
	if err := store.Checkpoints().Create(nonReusable); err != nil {
		t.Fatalf("create non-reusable checkpoint: %v", err)
	}

	out, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "handoff requiring fresh resumable checkpoint",
		Mode:         handoff.ModeResume,
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if out.Status != handoff.StatusCreated {
		t.Fatalf("expected created status, got %s", out.Status)
	}
	if out.Packet == nil {
		t.Fatal("expected packet")
	}
	if out.Packet.CheckpointID == nonReusable.CheckpointID {
		t.Fatalf("expected fresh handoff checkpoint, got reused non-resumable checkpoint %s", nonReusable.CheckpointID)
	}

	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint: %v", err)
	}
	if latestCheckpoint.Trigger != checkpoint.TriggerHandoff {
		t.Fatalf("expected handoff trigger for newly created checkpoint, got %s", latestCheckpoint.Trigger)
	}
	if latestCheckpoint.CheckpointID != out.Packet.CheckpointID {
		t.Fatalf("expected packet checkpoint %s to match latest %s", out.Packet.CheckpointID, latestCheckpoint.CheckpointID)
	}
}

func TestCreateHandoffReusesMatchingLatestPacket(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	req := CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "reuse existing handoff packet",
		Mode:         handoff.ModeResume,
		Notes:        []string{"preserve prior packet"},
	}
	first, err := coord.CreateHandoff(context.Background(), req)
	if err != nil {
		t.Fatalf("create first handoff: %v", err)
	}
	second, err := coord.CreateHandoff(context.Background(), req)
	if err != nil {
		t.Fatalf("create second handoff: %v", err)
	}
	if first.HandoffID != second.HandoffID {
		t.Fatalf("expected handoff reuse, got first=%s second=%s", first.HandoffID, second.HandoffID)
	}
	if first.CheckpointID != second.CheckpointID {
		t.Fatalf("expected checkpoint reuse, got first=%s second=%s", first.CheckpointID, second.CheckpointID)
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if countEvents(events, proof.EventHandoffCreated) != 1 {
		t.Fatalf("expected exactly one HANDOFF_CREATED event, got %d", countEvents(events, proof.EventHandoffCreated))
	}
}

func TestAcceptHandoffRecordsCompletion(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "handoff to claude",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	acceptOut, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  createOut.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
		Notes:      []string{"accepted for follow-up implementation"},
	})
	if err != nil {
		t.Fatalf("accept handoff: %v", err)
	}
	if acceptOut.Status != handoff.StatusAccepted {
		t.Fatalf("expected accepted status, got %s", acceptOut.Status)
	}
	persisted, err := store.Handoffs().Get(createOut.HandoffID)
	if err != nil {
		t.Fatalf("load persisted handoff: %v", err)
	}
	if persisted.Status != handoff.StatusAccepted {
		t.Fatalf("expected persisted accepted status, got %s", persisted.Status)
	}
	if persisted.AcceptedBy != rundomain.WorkerKindClaude {
		t.Fatalf("expected persisted accepted_by %s, got %s", rundomain.WorkerKindClaude, persisted.AcceptedBy)
	}
	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if !hasEvent(events, proof.EventHandoffAccepted) {
		t.Fatal("expected HANDOFF_ACCEPTED proof event")
	}
}

func TestAcceptHandoffIsIdempotent(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "idempotent accept test",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	first, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  createOut.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
		Notes:      []string{"accept once"},
	})
	if err != nil {
		t.Fatalf("accept handoff first: %v", err)
	}
	second, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  createOut.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
		Notes:      []string{"accept once"},
	})
	if err != nil {
		t.Fatalf("accept handoff second: %v", err)
	}
	if first.Status != handoff.StatusAccepted || second.Status != handoff.StatusAccepted {
		t.Fatalf("expected accepted status on idempotent path, got first=%s second=%s", first.Status, second.Status)
	}

	persisted, err := store.Handoffs().Get(createOut.HandoffID)
	if err != nil {
		t.Fatalf("load handoff: %v", err)
	}
	if len(persisted.HandoffNotes) != 1 {
		t.Fatalf("expected exactly one persisted note after idempotent accept, got %+v", persisted.HandoffNotes)
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if countEvents(events, proof.EventHandoffAccepted) != 1 {
		t.Fatalf("expected exactly one HANDOFF_ACCEPTED event, got %d", countEvents(events, proof.EventHandoffAccepted))
	}
}

func TestAcceptedHandoffRecoveryIsLaunchReady(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "accepted handoff recovery readiness",
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

	continueOut, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if continueOut.RecoveryClass != RecoveryClassAcceptedHandoffLaunchReady {
		t.Fatalf("expected accepted handoff recovery class, got %s", continueOut.RecoveryClass)
	}
	if continueOut.RecommendedAction != RecoveryActionLaunchAcceptedHandoff {
		t.Fatalf("expected launch accepted handoff action, got %s", continueOut.RecommendedAction)
	}
	if continueOut.ReadyForNextRun {
		t.Fatal("accepted handoff recovery should not claim local next-run readiness")
	}
	if !continueOut.ReadyForHandoffLaunch {
		t.Fatal("accepted handoff recovery should be ready for handoff launch")
	}
}

func TestCreateHandoffBuildsPacketFromPersistedContinuityState(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startRes, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "complete", RunID: startRes.RunID}); err != nil {
		t.Fatalf("complete noop run: %v", err)
	}

	runRec, err := store.Runs().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest run: %v", err)
	}
	runRec.LastKnownSummary = "persisted-summary-for-handoff-trust"
	runRec.UpdatedAt = time.Now().UTC()
	if err := store.Runs().Update(runRec); err != nil {
		t.Fatalf("update latest run summary: %v", err)
	}

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	caps.Version++
	caps.UpdatedAt = time.Now().UTC()
	caps.WorkingTreeDirty = true
	caps.TouchedFiles = append(caps.TouchedFiles, "persisted/worker_state.go")
	caps.Blockers = []string{"persisted blocker for trust test"}
	caps.NextAction = "persisted next action for handoff"
	if err := store.Capsules().Update(caps); err != nil {
		t.Fatalf("update capsule: %v", err)
	}

	briefRec, err := store.Briefs().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest brief: %v", err)
	}
	lastEvent, err := latestEventID(store, taskID)
	if err != nil {
		t.Fatalf("latest event: %v", err)
	}
	persistedCheckpoint := checkpoint.Checkpoint{
		Version:            1,
		CheckpointID:       common.CheckpointID("chk_persisted_trust_anchor"),
		TaskID:             taskID,
		RunID:              runRec.RunID,
		CreatedAt:          time.Now().UTC().Add(15 * time.Second),
		Trigger:            checkpoint.TriggerManual,
		CapsuleVersion:     caps.Version,
		Phase:              caps.CurrentPhase,
		Anchor:             anchorFromCapsule(caps),
		IntentID:           caps.CurrentIntentID,
		BriefID:            briefRec.BriefID,
		ContextPackID:      "",
		LastEventID:        lastEvent,
		PendingDecisionIDs: []common.DecisionID{},
		ResumeDescriptor:   "persisted resume descriptor for trust test",
		IsResumable:        true,
	}
	if err := store.Checkpoints().Create(persistedCheckpoint); err != nil {
		t.Fatalf("create persisted checkpoint: %v", err)
	}

	out, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "trust test handoff",
		Mode:         handoff.ModeTakeover,
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if out.Status != handoff.StatusCreated {
		t.Fatalf("expected created status, got %s", out.Status)
	}
	if out.Packet == nil {
		t.Fatal("expected packet")
	}
	if out.Packet.CheckpointID != persistedCheckpoint.CheckpointID {
		t.Fatalf("expected packet checkpoint from persisted state %s, got %s", persistedCheckpoint.CheckpointID, out.Packet.CheckpointID)
	}
	if out.Packet.ResumeDescriptor != persistedCheckpoint.ResumeDescriptor {
		t.Fatalf("expected persisted resume descriptor %q, got %q", persistedCheckpoint.ResumeDescriptor, out.Packet.ResumeDescriptor)
	}
	if out.Packet.LatestRunID != runRec.RunID {
		t.Fatalf("expected packet latest run %s, got %s", runRec.RunID, out.Packet.LatestRunID)
	}
	if out.Packet.BriefID != briefRec.BriefID {
		t.Fatalf("expected packet brief %s, got %s", briefRec.BriefID, out.Packet.BriefID)
	}
	if out.Packet.HandoffMode != handoff.ModeTakeover {
		t.Fatalf("expected typed handoff mode %s, got %s", handoff.ModeTakeover, out.Packet.HandoffMode)
	}
	if !containsString(out.Packet.TouchedFiles, "persisted/worker_state.go") {
		t.Fatalf("expected touched files to reflect persisted capsule update: %+v", out.Packet.TouchedFiles)
	}
	if !containsString(out.Packet.Blockers, "persisted blocker for trust test") {
		t.Fatalf("expected blockers to reflect persisted capsule update: %+v", out.Packet.Blockers)
	}
}

func TestLaunchHandoffClaudeSuccessFlow(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherSuccess()
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "handoff launch test",
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

	launchOut, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("launch handoff: %v", err)
	}
	if launchOut.LaunchStatus != HandoffLaunchStatusCompleted {
		t.Fatalf("expected completed launch status, got %s", launchOut.LaunchStatus)
	}
	if launchOut.Payload == nil {
		t.Fatal("expected launch payload")
	}
	if launchOut.Payload.HandoffID != createOut.HandoffID {
		t.Fatalf("expected payload handoff id %s, got %s", createOut.HandoffID, launchOut.Payload.HandoffID)
	}
	if launchOut.Payload.BriefID != createOut.Packet.BriefID {
		t.Fatalf("expected payload brief id %s, got %s", createOut.Packet.BriefID, launchOut.Payload.BriefID)
	}
	if launchOut.TransitionReceiptID == "" {
		t.Fatal("expected continuity transition receipt id on launch result")
	}
	if !strings.Contains(strings.ToLower(launchOut.CanonicalResponse), "acknowledgment") {
		t.Fatalf("expected canonical response to mention acknowledgment, got %q", launchOut.CanonicalResponse)
	}
	if !strings.Contains(strings.ToLower(launchOut.CanonicalResponse), "not yet proven complete") {
		t.Fatalf("expected canonical response to avoid downstream completion claims, got %q", launchOut.CanonicalResponse)
	}
	if !launcher.called {
		t.Fatal("expected handoff launcher to be called")
	}
	if launcher.lastReq.Payload.CheckpointID != createOut.Packet.CheckpointID {
		t.Fatalf("expected launcher payload checkpoint %s, got %s", createOut.Packet.CheckpointID, launcher.lastReq.Payload.CheckpointID)
	}
	ack, err := store.Handoffs().LatestAcknowledgment(createOut.HandoffID)
	if err != nil {
		t.Fatalf("expected persisted launch acknowledgment: %v", err)
	}
	if ack.Status != handoff.AcknowledgmentCaptured {
		t.Fatalf("expected captured acknowledgment status, got %s", ack.Status)
	}
	if ack.Summary == "" {
		t.Fatal("expected non-empty acknowledgment summary")
	}
	transitionRec, err := store.TransitionReceipts().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest continuity transition receipt: %v", err)
	}
	if transitionRec.ReceiptID != launchOut.TransitionReceiptID {
		t.Fatalf("expected transition receipt id %s, got %s", launchOut.TransitionReceiptID, transitionRec.ReceiptID)
	}
	if transitionRec.TransitionKind != transition.KindHandoffLaunch {
		t.Fatalf("expected handoff launch transition kind, got %s", transitionRec.TransitionKind)
	}
	if transitionRec.HandoffContinuityBefore != string(HandoffContinuityStateLaunchPendingOutcome) {
		t.Fatalf("expected launch-pending continuity before launch completion, got %s", transitionRec.HandoffContinuityBefore)
	}
	if transitionRec.HandoffContinuityAfter == transitionRec.HandoffContinuityBefore {
		t.Fatalf("expected continuity transition after launch, got before=%s after=%s", transitionRec.HandoffContinuityBefore, transitionRec.HandoffContinuityAfter)
	}
	if transitionRec.ReviewPosture != transition.ReviewPostureNone {
		t.Fatalf("expected no-session review posture NONE in launch transition, got %s", transitionRec.ReviewPosture)
	}
	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.LatestContinuityTransitionReceipt == nil || status.LatestContinuityTransitionReceipt.ReceiptID != launchOut.TransitionReceiptID {
		t.Fatalf("expected status latest transition receipt %s, got %+v", launchOut.TransitionReceiptID, status.LatestContinuityTransitionReceipt)
	}
	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.LatestContinuityTransitionReceipt == nil || inspectOut.LatestContinuityTransitionReceipt.ReceiptID != launchOut.TransitionReceiptID {
		t.Fatalf("expected inspect latest transition receipt %s, got %+v", launchOut.TransitionReceiptID, inspectOut.LatestContinuityTransitionReceipt)
	}
	shellOut, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if shellOut.LatestContinuityTransitionReceipt == nil || shellOut.LatestContinuityTransitionReceipt.ReceiptID != launchOut.TransitionReceiptID {
		t.Fatalf("expected shell snapshot latest transition receipt %s, got %+v", launchOut.TransitionReceiptID, shellOut.LatestContinuityTransitionReceipt)
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if !hasEvent(events, proof.EventHandoffLaunchRequested) {
		t.Fatal("expected HANDOFF_LAUNCH_REQUESTED proof event")
	}
	if !hasEvent(events, proof.EventHandoffLaunchCompleted) {
		t.Fatal("expected HANDOFF_LAUNCH_COMPLETED proof event")
	}
	if !hasEvent(events, proof.EventHandoffAcknowledgmentCaptured) {
		t.Fatal("expected HANDOFF_ACKNOWLEDGMENT_CAPTURED proof event")
	}
	if !hasEvent(events, proof.EventCanonicalResponseEmitted) {
		t.Fatal("expected canonical response proof event")
	}
	if !hasEvent(events, proof.EventBranchHandoffTransitionRecorded) {
		t.Fatal("expected branch/handoff transition proof event")
	}
}

func TestLaunchHandoffTransitionReceiptSnapshotsSourceScopedStaleReviewAndAcknowledgment(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherSuccess()
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)
	base := time.Unix(1713100000, 0).UTC()

	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:    string(taskID),
		SessionID: "shs_launch_scope_stale",
		HostMode:  "transcript",
		HostState: "transcript-only",
		StartedAt: base,
		Active:    true,
	}); err != nil {
		t.Fatalf("report shell session: %v", err)
	}
	if _, err := coord.RecordShellTranscript(context.Background(), RecordShellTranscriptRequest{
		TaskID:    string(taskID),
		SessionID: "shs_launch_scope_stale",
		Chunks: []RecordShellTranscriptChunk{
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "worker line 1", CreatedAt: base.Add(1 * time.Second)},
			{Source: shellsession.TranscriptSourceSystemNote, Content: "system note", CreatedAt: base.Add(2 * time.Second)},
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "worker line 2", CreatedAt: base.Add(3 * time.Second)},
		},
	}); err != nil {
		t.Fatalf("record shell transcript: %v", err)
	}
	if _, err := coord.RecordShellTranscriptReview(context.Background(), RecordShellTranscriptReviewRequest{
		TaskID:          string(taskID),
		SessionID:       "shs_launch_scope_stale",
		Source:          string(shellsession.TranscriptSourceWorkerOutput),
		ReviewedUpToSeq: 1,
		Summary:         "reviewed worker output through first retained sequence",
	}); err != nil {
		t.Fatalf("record source-scoped transcript review: %v", err)
	}
	ackOut, err := coord.RecordOperatorReviewGapAcknowledgment(context.Background(), RecordOperatorReviewGapAcknowledgmentRequest{
		TaskID:    string(taskID),
		SessionID: "shs_launch_scope_stale",
		Summary:   "launching handoff with source-scoped stale transcript review awareness",
	})
	if err != nil {
		t.Fatalf("record operator review-gap acknowledgment: %v", err)
	}
	if ackOut.Acknowledgment.Class != shellsession.TranscriptReviewGapAckSourceScopedStale {
		t.Fatalf("expected source-scoped stale acknowledgment class, got %+v", ackOut.Acknowledgment)
	}

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "launch under source-scoped stale review posture",
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
	launchOut, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("launch handoff: %v", err)
	}
	if launchOut.TransitionReceiptID == "" {
		t.Fatal("expected launch transition receipt id")
	}
	transitionRec, err := store.TransitionReceipts().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest transition receipt: %v", err)
	}
	if transitionRec.ReceiptID != launchOut.TransitionReceiptID {
		t.Fatalf("expected transition receipt id %s, got %s", launchOut.TransitionReceiptID, transitionRec.ReceiptID)
	}
	if transitionRec.ReviewPosture != transition.ReviewPostureSourceScopedReviewStale {
		t.Fatalf("expected source-scoped stale review posture, got %s", transitionRec.ReviewPosture)
	}
	if transitionRec.ReviewScope != shellsession.TranscriptSourceWorkerOutput {
		t.Fatalf("expected worker-output review scope snapshot, got %s", transitionRec.ReviewScope)
	}
	if !transitionRec.ReviewGapPresent {
		t.Fatal("expected review-gap present for source-scoped stale posture")
	}
	if !transitionRec.AcknowledgmentPresent {
		t.Fatalf("expected acknowledgment-present transition receipt, got %+v", transitionRec)
	}
	if transitionRec.AcknowledgmentClass != shellsession.TranscriptReviewGapAckSourceScopedStale {
		t.Fatalf("expected source-scoped stale acknowledgment class in transition receipt, got %s", transitionRec.AcknowledgmentClass)
	}
	if transitionRec.LatestReviewAckID != ackOut.Acknowledgment.AcknowledgmentID {
		t.Fatalf("expected linked latest review-gap acknowledgment id %s, got %s", ackOut.Acknowledgment.AcknowledgmentID, transitionRec.LatestReviewAckID)
	}
	if transitionRec.OldestUnreviewedSequence == 0 || transitionRec.UnreviewedRetainedCount == 0 {
		t.Fatalf("expected retained unreviewed snapshot evidence, got %+v", transitionRec)
	}
}

func TestLaunchHandoffReplayCompletedDoesNotDuplicateTransitionReceipt(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherSuccess()
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "transition receipt replay guard",
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

	first, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("first launch handoff: %v", err)
	}
	second, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("replayed launch handoff: %v", err)
	}
	if first.TransitionReceiptID == "" {
		t.Fatalf("expected first launch transition receipt id, got %+v", first)
	}
	if second.TransitionReceiptID != "" {
		t.Fatalf("expected replayed launch to reuse durable outcome without new transition receipt id, got %+v", second)
	}
	history, err := store.TransitionReceipts().ListByTask(taskID, 10)
	if err != nil {
		t.Fatalf("list transition receipts: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected exactly one transition receipt after replayed launch, got %+v", history)
	}
	if history[0].ReceiptID != first.TransitionReceiptID {
		t.Fatalf("expected persisted transition receipt %s, got %+v", first.TransitionReceiptID, history[0])
	}
}

func TestCreateHandoffBlockedAfterFailedRunRecovery(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("run start: %v", err)
	}

	out, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "should block after failed run",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if out.Status != handoff.StatusBlocked {
		t.Fatalf("expected blocked handoff after failed run recovery state, got %s", out.Status)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "blocked") {
		t.Fatalf("expected blocked canonical response, got %q", out.CanonicalResponse)
	}
}

func TestLaunchHandoffSuccessWithUnusableOutputPersistsUnavailableAcknowledgment(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherUnusableOutput()
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "handoff unusable-ack test",
		Mode:         handoff.ModeResume,
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	launchOut, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("launch handoff: %v", err)
	}
	if launchOut.LaunchStatus != HandoffLaunchStatusCompleted {
		t.Fatalf("expected completed launch status, got %s", launchOut.LaunchStatus)
	}
	if !strings.Contains(strings.ToLower(launchOut.CanonicalResponse), "no usable initial acknowledgment") {
		t.Fatalf("expected canonical fallback wording, got %q", launchOut.CanonicalResponse)
	}
	if !strings.Contains(strings.ToLower(launchOut.CanonicalResponse), "not yet proven complete") {
		t.Fatalf("expected explicit uncertainty in canonical response, got %q", launchOut.CanonicalResponse)
	}

	ack, err := store.Handoffs().LatestAcknowledgment(createOut.HandoffID)
	if err != nil {
		t.Fatalf("load latest acknowledgment: %v", err)
	}
	if ack.Status != handoff.AcknowledgmentUnavailable {
		t.Fatalf("expected unavailable acknowledgment status, got %s", ack.Status)
	}
	if len(ack.Unknowns) == 0 {
		t.Fatal("expected unknowns for unavailable acknowledgment")
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if !hasEvent(events, proof.EventHandoffAcknowledgmentUnavailable) {
		t.Fatal("expected HANDOFF_ACKNOWLEDGMENT_UNAVAILABLE proof event")
	}
	if hasEvent(events, proof.EventHandoffAcknowledgmentCaptured) {
		t.Fatal("did not expect HANDOFF_ACKNOWLEDGMENT_CAPTURED for unusable output")
	}
}

func TestLaunchHandoffBlockedCases(t *testing.T) {
	t.Run("missing handoff", func(t *testing.T) {
		store := newTestStore(t)
		launcher := newFakeHandoffLauncherSuccess()
		coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
		taskID := setupTaskWithBrief(t, coord)

		out, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
			TaskID:    string(taskID),
			HandoffID: "hnd_missing",
		})
		if err != nil {
			t.Fatalf("launch handoff missing: %v", err)
		}
		if out.LaunchStatus != HandoffLaunchStatusBlocked {
			t.Fatalf("expected blocked launch status, got %s", out.LaunchStatus)
		}
		if launcher.called {
			t.Fatal("launcher should not be called on blocked path")
		}
		events, err := store.Proofs().ListByTask(taskID, 500)
		if err != nil {
			t.Fatalf("list proofs: %v", err)
		}
		if !hasEvent(events, proof.EventHandoffLaunchBlocked) {
			t.Fatal("expected HANDOFF_LAUNCH_BLOCKED proof event")
		}
	})

	t.Run("wrong status", func(t *testing.T) {
		store := newTestStore(t)
		launcher := newFakeHandoffLauncherSuccess()
		coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
		taskID := setupTaskWithBrief(t, coord)

		createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
			TaskID:       string(taskID),
			TargetWorker: rundomain.WorkerKindClaude,
			Reason:       "status block test",
		})
		if err != nil {
			t.Fatalf("create handoff: %v", err)
		}
		if err := store.Handoffs().UpdateStatus(taskID, createOut.HandoffID, handoff.StatusBlocked, rundomain.WorkerKindUnknown, []string{"blocked for test"}, time.Now().UTC()); err != nil {
			t.Fatalf("force blocked status: %v", err)
		}

		out, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
			TaskID:    string(taskID),
			HandoffID: createOut.HandoffID,
		})
		if err != nil {
			t.Fatalf("launch handoff wrong status: %v", err)
		}
		if out.LaunchStatus != HandoffLaunchStatusBlocked {
			t.Fatalf("expected blocked status, got %s", out.LaunchStatus)
		}
		if launcher.called {
			t.Fatal("launcher should not be called on wrong-status blocked path")
		}
	})

	t.Run("wrong target", func(t *testing.T) {
		store := newTestStore(t)
		launcher := newFakeHandoffLauncherSuccess()
		coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
		taskID := setupTaskWithBrief(t, coord)

		createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
			TaskID:       string(taskID),
			TargetWorker: rundomain.WorkerKindClaude,
			Reason:       "target mismatch test",
		})
		if err != nil {
			t.Fatalf("create handoff: %v", err)
		}

		out, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
			TaskID:       string(taskID),
			HandoffID:    createOut.HandoffID,
			TargetWorker: rundomain.WorkerKindCodex,
		})
		if err != nil {
			t.Fatalf("launch handoff wrong target: %v", err)
		}
		if out.LaunchStatus != HandoffLaunchStatusBlocked {
			t.Fatalf("expected blocked status, got %s", out.LaunchStatus)
		}
		if launcher.called {
			t.Fatal("launcher should not be called on wrong-target blocked path")
		}
	})
}

func TestLaunchHandoffFailureRecordsProofAndCanonical(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherError(errors.New("claude launch unavailable"))
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "failure path test",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}

	out, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("launch handoff failure path should return canonical result, got err: %v", err)
	}
	if out.LaunchStatus != HandoffLaunchStatusFailed {
		t.Fatalf("expected failed launch status, got %s", out.LaunchStatus)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "failed") {
		t.Fatalf("expected failed canonical response, got %q", out.CanonicalResponse)
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if !hasEvent(events, proof.EventHandoffLaunchRequested) {
		t.Fatal("expected HANDOFF_LAUNCH_REQUESTED proof event")
	}
	if !hasEvent(events, proof.EventHandoffLaunchFailed) {
		t.Fatal("expected HANDOFF_LAUNCH_FAILED proof event")
	}
	if !hasEvent(events, proof.EventCanonicalResponseEmitted) {
		t.Fatal("expected canonical response proof event")
	}
}

func TestLaunchHandoffReusesDurableSuccessWithoutRelaunch(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherSuccess()
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "replay durable success",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	first, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("launch handoff first: %v", err)
	}
	second, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("launch handoff second: %v", err)
	}
	if launcher.callCount != 1 {
		t.Fatalf("expected launcher to run once, got %d", launcher.callCount)
	}
	if first.LaunchID == "" || second.LaunchID != first.LaunchID {
		t.Fatalf("expected durable launch id reuse, got first=%s second=%s", first.LaunchID, second.LaunchID)
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if countEvents(events, proof.EventHandoffLaunchCompleted) != 1 {
		t.Fatalf("expected exactly one HANDOFF_LAUNCH_COMPLETED event, got %d", countEvents(events, proof.EventHandoffLaunchCompleted))
	}
	if countEvents(events, proof.EventHandoffAcknowledgmentCaptured) != 1 {
		t.Fatalf("expected exactly one HANDOFF_ACKNOWLEDGMENT_CAPTURED event, got %d", countEvents(events, proof.EventHandoffAcknowledgmentCaptured))
	}
}

func TestLaunchHandoffBlockedWhenLauncherMissing(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "missing launcher guard",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}

	out, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("launch handoff with missing launcher should return canonical blocked result, got err: %v", err)
	}
	if out.LaunchStatus != HandoffLaunchStatusBlocked {
		t.Fatalf("expected blocked launch status, got %s", out.LaunchStatus)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "blocked") {
		t.Fatalf("expected blocked canonical response, got %q", out.CanonicalResponse)
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if !hasEvent(events, proof.EventHandoffLaunchBlocked) {
		t.Fatal("expected HANDOFF_LAUNCH_BLOCKED proof event")
	}
	if hasEvent(events, proof.EventHandoffLaunchRequested) {
		t.Fatal("should not emit HANDOFF_LAUNCH_REQUESTED when launcher is missing")
	}
}

func TestLaunchHandoffBlocksRetryWhenPriorRequestOutcomeUnknown(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherSuccess()
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "unknown launch replay guard",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}

	now := time.Now().UTC()
	if err := store.Handoffs().CreateLaunch(handoff.Launch{
		Version:      1,
		AttemptID:    "hlc_requested_unknown",
		HandoffID:    createOut.HandoffID,
		TaskID:       taskID,
		TargetWorker: createOut.TargetWorker,
		Status:       handoff.LaunchStatusRequested,
		PayloadHash:  "hash_requested_unknown",
		RequestedAt:  now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("create requested launch record: %v", err)
	}

	out, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("launch handoff retry guard: %v", err)
	}
	if out.LaunchStatus != HandoffLaunchStatusBlocked {
		t.Fatalf("expected blocked replay status, got %s", out.LaunchStatus)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "unknown") {
		t.Fatalf("expected unknown-outcome canonical response, got %q", out.CanonicalResponse)
	}
	if launcher.callCount != 0 {
		t.Fatalf("expected launcher not to run, got %d calls", launcher.callCount)
	}
}

func TestLaunchHandoffAllowsRetryAfterDurableFailure(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherError(errors.New("claude launch unavailable"))
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "retry after durable failure",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}

	first, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("first failed launch: %v", err)
	}
	if first.LaunchStatus != HandoffLaunchStatusFailed {
		t.Fatalf("expected failed first launch, got %s", first.LaunchStatus)
	}

	launcher.err = nil
	launcher.result = newFakeHandoffLauncherSuccess().result
	second, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("second retry launch: %v", err)
	}
	if second.LaunchStatus != HandoffLaunchStatusCompleted {
		t.Fatalf("expected completed second launch, got %s", second.LaunchStatus)
	}
	if launcher.callCount != 2 {
		t.Fatalf("expected launcher to run twice across retry, got %d", launcher.callCount)
	}

	latestLaunch, err := store.Handoffs().LatestLaunchByHandoff(createOut.HandoffID)
	if err != nil {
		t.Fatalf("latest launch by handoff: %v", err)
	}
	if latestLaunch.Status != handoff.LaunchStatusCompleted {
		t.Fatalf("expected latest durable launch to be completed, got %s", latestLaunch.Status)
	}
	if latestLaunch.AttemptID == "" {
		t.Fatal("expected latest durable launch attempt id")
	}
}

func TestStatusAndInspectSurfaceDurableLaunchControl(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "launch control inspectability",
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

	now := time.Now().UTC()
	if err := store.Handoffs().CreateLaunch(handoff.Launch{
		Version:      1,
		AttemptID:    "hlc_control_pending",
		HandoffID:    createOut.HandoffID,
		TaskID:       taskID,
		TargetWorker: rundomain.WorkerKindClaude,
		Status:       handoff.LaunchStatusRequested,
		PayloadHash:  "launch_control_pending",
		RequestedAt:  now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("create launch control record: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.LaunchControlState != LaunchControlStateRequestedOutcomeUnknown {
		t.Fatalf("expected requested-unknown launch control state, got %s", status.LaunchControlState)
	}
	if status.LaunchRetryDisposition != LaunchRetryDispositionBlocked {
		t.Fatalf("expected blocked retry disposition, got %s", status.LaunchRetryDisposition)
	}
	if status.RecoveryClass != RecoveryClassHandoffLaunchPendingOutcome {
		t.Fatalf("expected pending launch recovery class, got %s", status.RecoveryClass)
	}
	if status.HandoffContinuityState != HandoffContinuityStateLaunchPendingOutcome {
		t.Fatalf("expected pending handoff continuity state, got %s", status.HandoffContinuityState)
	}
	if status.HandoffContinuationProven {
		t.Fatal("pending launch must not claim downstream continuation proven")
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.Launch == nil {
		t.Fatal("expected persisted launch in inspect output")
	}
	if inspectOut.LaunchControl == nil {
		t.Fatal("expected launch control in inspect output")
	}
	if inspectOut.LaunchControl.State != LaunchControlStateRequestedOutcomeUnknown {
		t.Fatalf("expected inspect launch control state requested-unknown, got %s", inspectOut.LaunchControl.State)
	}
	if inspectOut.Recovery == nil || inspectOut.Recovery.RecoveryClass != RecoveryClassHandoffLaunchPendingOutcome {
		t.Fatalf("expected inspect recovery class %s, got %+v", RecoveryClassHandoffLaunchPendingOutcome, inspectOut.Recovery)
	}
	if inspectOut.HandoffContinuity == nil || inspectOut.HandoffContinuity.State != HandoffContinuityStateLaunchPendingOutcome {
		t.Fatalf("expected inspect handoff continuity pending outcome, got %+v", inspectOut.HandoffContinuity)
	}
}

func TestStatusAndInspectSurfaceCompletedClaudeLaunchWithCapturedAcknowledgment(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "captured acknowledgment inspectability",
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
	if _, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	}); err != nil {
		t.Fatalf("launch handoff: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.RecoveryClass != RecoveryClassHandoffLaunchCompleted {
		t.Fatalf("expected launched recovery class, got %s", status.RecoveryClass)
	}
	if status.ReadyForNextRun {
		t.Fatal("completed Claude launch must not imply fresh next-run readiness")
	}
	if status.HandoffContinuityState != HandoffContinuityStateLaunchCompletedAckSeen {
		t.Fatalf("expected captured-ack handoff continuity state, got %s", status.HandoffContinuityState)
	}
	if status.LatestAcknowledgmentStatus != handoff.AcknowledgmentCaptured {
		t.Fatalf("expected captured acknowledgment status, got %s", status.LatestAcknowledgmentStatus)
	}
	if status.HandoffContinuationProven {
		t.Fatal("captured acknowledgment must not prove downstream continuation")
	}
	if !strings.Contains(strings.ToLower(status.HandoffContinuityReason), "unproven") {
		t.Fatalf("expected explicit downstream-unproven reason, got %q", status.HandoffContinuityReason)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.HandoffContinuity == nil || inspectOut.HandoffContinuity.State != HandoffContinuityStateLaunchCompletedAckSeen {
		t.Fatalf("expected inspect captured-ack handoff continuity, got %+v", inspectOut.HandoffContinuity)
	}
	if inspectOut.HandoffContinuity.DownstreamContinuationProven {
		t.Fatal("inspect handoff continuity must not claim downstream continuation proven")
	}
	if inspectOut.Acknowledgment == nil || inspectOut.Acknowledgment.Status != handoff.AcknowledgmentCaptured {
		t.Fatalf("expected inspect acknowledgment captured, got %+v", inspectOut.Acknowledgment)
	}
}

func TestStatusAndInspectSurfaceCompletedClaudeLaunchWithUnavailableAcknowledgment(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherUnusableOutput())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "unavailable acknowledgment inspectability",
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
	if _, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	}); err != nil {
		t.Fatalf("launch handoff: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.HandoffContinuityState != HandoffContinuityStateLaunchCompletedAckEmpty {
		t.Fatalf("expected unavailable-ack handoff continuity state, got %s", status.HandoffContinuityState)
	}
	if status.LatestAcknowledgmentStatus != handoff.AcknowledgmentUnavailable {
		t.Fatalf("expected unavailable acknowledgment status, got %s", status.LatestAcknowledgmentStatus)
	}
	if !strings.Contains(strings.ToLower(status.HandoffContinuityReason), "no usable initial acknowledgment") &&
		!strings.Contains(strings.ToLower(status.HandoffContinuityReason), "acknowledgment") {
		t.Fatalf("expected acknowledgment-unavailable reason, got %q", status.HandoffContinuityReason)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.HandoffContinuity == nil || inspectOut.HandoffContinuity.State != HandoffContinuityStateLaunchCompletedAckEmpty {
		t.Fatalf("expected inspect unavailable-ack handoff continuity, got %+v", inspectOut.HandoffContinuity)
	}
}

func TestRecordHandoffFollowThroughProofOfLifeStrengthensLaunchedClaudeContinuity(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "record downstream proof of life",
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
	if _, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	}); err != nil {
		t.Fatalf("launch handoff: %v", err)
	}

	recorded, err := coord.RecordHandoffFollowThrough(context.Background(), RecordHandoffFollowThroughRequest{
		TaskID:  string(taskID),
		Kind:    handoff.FollowThroughProofOfLifeObserved,
		Summary: "later Claude proof of life observed",
		Notes:   []string{"operator observed downstream ping"},
	})
	if err != nil {
		t.Fatalf("record follow-through: %v", err)
	}
	if recorded.HandoffContinuity.State != HandoffContinuityStateFollowThroughProofOfLife {
		t.Fatalf("expected proof-of-life continuity state, got %+v", recorded.HandoffContinuity)
	}
	if !recorded.HandoffContinuity.DownstreamContinuationProven {
		t.Fatal("proof-of-life continuity should mark downstream continuation proven without implying completion")
	}
	if recorded.ReadyForNextRun {
		t.Fatal("proof-of-life follow-through must not make local next run ready")
	}

	persisted, err := store.Handoffs().LatestFollowThrough(createOut.HandoffID)
	if err != nil {
		t.Fatalf("load persisted follow-through: %v", err)
	}
	if persisted.Kind != handoff.FollowThroughProofOfLifeObserved {
		t.Fatalf("expected persisted proof-of-life kind, got %s", persisted.Kind)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.HandoffContinuityState != HandoffContinuityStateFollowThroughProofOfLife {
		t.Fatalf("expected proof-of-life handoff continuity state, got %s", status.HandoffContinuityState)
	}
	if status.LatestFollowThroughKind != handoff.FollowThroughProofOfLifeObserved {
		t.Fatalf("expected latest follow-through kind in status, got %s", status.LatestFollowThroughKind)
	}
	if !status.HandoffContinuationProven {
		t.Fatal("status should reflect downstream continuation proven for proof-of-life evidence")
	}
	if status.ReadyForNextRun {
		t.Fatal("status must not imply fresh next-run readiness after handoff follow-through proof of life")
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.FollowThrough == nil || inspectOut.FollowThrough.Kind != handoff.FollowThroughProofOfLifeObserved {
		t.Fatalf("expected inspect follow-through payload, got %+v", inspectOut.FollowThrough)
	}
	if inspectOut.HandoffContinuity == nil || inspectOut.HandoffContinuity.State != HandoffContinuityStateFollowThroughProofOfLife {
		t.Fatalf("expected inspect proof-of-life continuity, got %+v", inspectOut.HandoffContinuity)
	}
	if inspectOut.Recovery == nil || inspectOut.Recovery.RecoveryClass != RecoveryClassHandoffLaunchCompleted {
		t.Fatalf("expected launched-handoff recovery after proof-of-life evidence, got %+v", inspectOut.Recovery)
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if !hasEvent(events, proof.EventHandoffFollowThroughRecorded) {
		t.Fatal("expected HANDOFF_FOLLOW_THROUGH_RECORDED proof event")
	}
}

func TestRecordHandoffFollowThroughStalledRequiresReview(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "record stalled downstream follow-through",
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
	if _, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	}); err != nil {
		t.Fatalf("launch handoff: %v", err)
	}

	recorded, err := coord.RecordHandoffFollowThrough(context.Background(), RecordHandoffFollowThroughRequest{
		TaskID:  string(taskID),
		Kind:    handoff.FollowThroughStalledReviewRequired,
		Summary: "Claude follow-through appears stalled",
	})
	if err != nil {
		t.Fatalf("record stalled follow-through: %v", err)
	}
	if recorded.HandoffContinuity.State != HandoffContinuityStateFollowThroughStalled {
		t.Fatalf("expected stalled follow-through continuity, got %+v", recorded.HandoffContinuity)
	}
	if recorded.RecoveryClass != RecoveryClassHandoffFollowThroughReviewRequired {
		t.Fatalf("expected handoff follow-through review-required recovery, got %s", recorded.RecoveryClass)
	}
	if recorded.RecommendedAction != RecoveryActionReviewHandoffFollowThrough {
		t.Fatalf("expected review-handoff-follow-through action, got %s", recorded.RecommendedAction)
	}
	if recorded.ReadyForNextRun {
		t.Fatal("stalled follow-through must not make local next run ready")
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.RecoveryClass != RecoveryClassHandoffFollowThroughReviewRequired {
		t.Fatalf("expected stalled follow-through recovery in status, got %s", status.RecoveryClass)
	}
	if status.RecommendedAction != RecoveryActionReviewHandoffFollowThrough {
		t.Fatalf("expected stalled follow-through action in status, got %s", status.RecommendedAction)
	}
	if status.HandoffContinuityState != HandoffContinuityStateFollowThroughStalled {
		t.Fatalf("expected stalled handoff continuity in status, got %s", status.HandoffContinuityState)
	}
}

func TestRecordHandoffFollowThroughRejectsInvalidPostureAndReplaysWithinSameLaunch(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "follow-through posture validation",
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

	if _, err := coord.RecordHandoffFollowThrough(context.Background(), RecordHandoffFollowThroughRequest{
		TaskID:  string(taskID),
		Kind:    handoff.FollowThroughContinuationUnknown,
		Summary: "still unknown before launch",
	}); err == nil || !strings.Contains(err.Error(), "launched Claude follow-through posture") {
		t.Fatalf("expected invalid-posture rejection before launch completion, got %v", err)
	}

	launcher := newFakeHandoffLauncherSuccess()
	coord = newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	if _, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	}); err != nil {
		t.Fatalf("launch handoff: %v", err)
	}

	first, err := coord.RecordHandoffFollowThrough(context.Background(), RecordHandoffFollowThroughRequest{
		TaskID:  string(taskID),
		Kind:    handoff.FollowThroughContinuationUnknown,
		Summary: "downstream follow-through remains unknown",
	})
	if err != nil {
		t.Fatalf("record first unknown follow-through: %v", err)
	}
	second, err := coord.RecordHandoffFollowThrough(context.Background(), RecordHandoffFollowThroughRequest{
		TaskID:  string(taskID),
		Kind:    handoff.FollowThroughContinuationUnknown,
		Summary: "downstream follow-through remains unknown",
	})
	if err != nil {
		t.Fatalf("record replayed unknown follow-through: %v", err)
	}
	if first.Record.RecordID != second.Record.RecordID {
		t.Fatalf("expected replay reuse of follow-through record, got first=%s second=%s", first.Record.RecordID, second.Record.RecordID)
	}
	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if countEvents(events, proof.EventHandoffFollowThroughRecorded) != 1 {
		t.Fatalf("expected exactly one HANDOFF_FOLLOW_THROUGH_RECORDED event, got %d", countEvents(events, proof.EventHandoffFollowThroughRecorded))
	}
}

func TestRecordHandoffFollowThroughRejectsResolvedClaudeBranch(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "resolved branches must reject new follow-through receipts",
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
	if _, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	}); err != nil {
		t.Fatalf("launch handoff: %v", err)
	}
	if _, err := coord.RecordHandoffResolution(context.Background(), RecordHandoffResolutionRequest{
		TaskID:  string(taskID),
		Kind:    handoff.ResolutionAbandoned,
		Summary: "downstream launch abandoned during test",
	}); err != nil {
		t.Fatalf("record handoff resolution: %v", err)
	}

	if _, err := coord.RecordHandoffFollowThrough(context.Background(), RecordHandoffFollowThroughRequest{
		TaskID:  string(taskID),
		Kind:    handoff.FollowThroughContinuationUnknown,
		Summary: "should not be accepted after resolution",
	}); err == nil || !strings.Contains(err.Error(), "already resolved") {
		t.Fatalf("expected resolved-branch rejection, got %v", err)
	}
}

func TestRecordHandoffResolutionSupersededByLocalUnblocksAcceptedClaudeBranch(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "return local control after accepted handoff",
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

	recorded, err := coord.RecordHandoffResolution(context.Background(), RecordHandoffResolutionRequest{
		TaskID:  string(taskID),
		Kind:    handoff.ResolutionSupersededByLocal,
		Summary: "operator returned canonical control to the local branch",
		Notes:   []string{"Claude branch closed explicitly"},
	})
	if err != nil {
		t.Fatalf("record handoff resolution: %v", err)
	}
	if recorded.Record.Kind != handoff.ResolutionSupersededByLocal {
		t.Fatalf("expected superseded-by-local resolution, got %+v", recorded.Record)
	}
	if recorded.HandoffContinuity.State != HandoffContinuityStateResolved {
		t.Fatalf("expected resolved handoff continuity, got %+v", recorded.HandoffContinuity)
	}
	if recorded.TransitionReceiptID == "" {
		t.Fatal("expected transition receipt id on handoff resolution result")
	}
	if recorded.RecoveryClass != RecoveryClassReadyNextRun {
		t.Fatalf("expected local next-run recovery after resolution, got %s", recorded.RecoveryClass)
	}
	if !recorded.ReadyForNextRun {
		t.Fatal("resolved accepted handoff should unblock local next-run readiness")
	}
	if strings.Contains(strings.ToLower(recorded.CanonicalResponse), "completed") {
		t.Fatalf("resolution must not claim downstream completion, got %q", recorded.CanonicalResponse)
	}

	persisted, err := store.Handoffs().LatestResolution(createOut.HandoffID)
	if err != nil {
		t.Fatalf("latest resolution: %v", err)
	}
	if persisted.ResolutionID != recorded.Record.ResolutionID {
		t.Fatalf("expected persisted resolution %s, got %s", recorded.Record.ResolutionID, persisted.ResolutionID)
	}
	transitionRec, err := store.TransitionReceipts().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest transition receipt: %v", err)
	}
	if transitionRec.ReceiptID != recorded.TransitionReceiptID {
		t.Fatalf("expected transition receipt id %s, got %s", recorded.TransitionReceiptID, transitionRec.ReceiptID)
	}
	if transitionRec.TransitionKind != transition.KindHandoffResolution {
		t.Fatalf("expected handoff-resolution transition kind, got %s", transitionRec.TransitionKind)
	}
	if transitionRec.BranchClassBefore != string(ActiveBranchClassHandoffClaude) || transitionRec.BranchClassAfter != string(ActiveBranchClassLocal) {
		t.Fatalf("expected branch ownership to transition handoff->local, got before=%s after=%s", transitionRec.BranchClassBefore, transitionRec.BranchClassAfter)
	}
	if transitionRec.HandoffContinuityBefore != string(HandoffContinuityStateAcceptedNotLaunched) || transitionRec.HandoffContinuityAfter != string(HandoffContinuityStateNotApplicable) {
		t.Fatalf("expected continuity transition accepted-not-launched->not-applicable after resolution, got before=%s after=%s", transitionRec.HandoffContinuityBefore, transitionRec.HandoffContinuityAfter)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.HandoffContinuityState != HandoffContinuityStateNotApplicable {
		t.Fatalf("expected no active handoff continuity in status after resolution, got %s", status.HandoffContinuityState)
	}
	if status.LatestResolutionKind != handoff.ResolutionSupersededByLocal {
		t.Fatalf("expected resolution kind in status, got %s", status.LatestResolutionKind)
	}
	if status.LatestContinuityTransitionReceipt == nil || status.LatestContinuityTransitionReceipt.ReceiptID != recorded.TransitionReceiptID {
		t.Fatalf("expected status transition receipt %s, got %+v", recorded.TransitionReceiptID, status.LatestContinuityTransitionReceipt)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.Resolution == nil || inspectOut.Resolution.Kind != handoff.ResolutionSupersededByLocal {
		t.Fatalf("expected inspect resolution payload, got %+v", inspectOut.Resolution)
	}
	if inspectOut.Handoff == nil || inspectOut.Handoff.HandoffID != createOut.HandoffID {
		t.Fatalf("expected inspect to retain historical handoff packet, got %+v", inspectOut.Handoff)
	}
	if inspectOut.HandoffContinuity == nil || inspectOut.HandoffContinuity.State != HandoffContinuityStateNotApplicable {
		t.Fatalf("expected no active handoff continuity in inspect after resolution, got %+v", inspectOut.HandoffContinuity)
	}
	if inspectOut.LatestContinuityTransitionReceipt == nil || inspectOut.LatestContinuityTransitionReceipt.ReceiptID != recorded.TransitionReceiptID {
		t.Fatalf("expected inspect transition receipt %s, got %+v", recorded.TransitionReceiptID, inspectOut.LatestContinuityTransitionReceipt)
	}
	shellOut, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot task: %v", err)
	}
	if shellOut.LatestContinuityTransitionReceipt == nil || shellOut.LatestContinuityTransitionReceipt.ReceiptID != recorded.TransitionReceiptID {
		t.Fatalf("expected shell transition receipt %s, got %+v", recorded.TransitionReceiptID, shellOut.LatestContinuityTransitionReceipt)
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if countEvents(events, proof.EventHandoffResolutionRecorded) != 1 {
		t.Fatalf("expected exactly one HANDOFF_RESOLUTION_RECORDED event, got %d", countEvents(events, proof.EventHandoffResolutionRecorded))
	}
	if countEvents(events, proof.EventBranchHandoffTransitionRecorded) < 1 {
		t.Fatalf("expected transition proof event for resolution, got %d", countEvents(events, proof.EventBranchHandoffTransitionRecorded))
	}
}

func TestRecordHandoffResolutionClosedUnprovenClosesLaunchedClaudeBranchWithoutCompletionClaim(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "close launched branch as unproven",
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
	if _, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	}); err != nil {
		t.Fatalf("launch handoff: %v", err)
	}

	recorded, err := coord.RecordHandoffResolution(context.Background(), RecordHandoffResolutionRequest{
		TaskID:  string(taskID),
		Kind:    handoff.ResolutionClosedUnproven,
		Summary: "launch completed but downstream completion remains unproven",
	})
	if err != nil {
		t.Fatalf("record launched handoff resolution: %v", err)
	}
	if recorded.HandoffContinuity.State != HandoffContinuityStateResolved {
		t.Fatalf("expected resolved launched continuity, got %+v", recorded.HandoffContinuity)
	}
	if recorded.ReadyForNextRun != true {
		t.Fatal("resolved launched handoff should unblock local next-run readiness when the local brief is still valid")
	}
	if strings.Contains(strings.ToLower(recorded.CanonicalResponse), "Claude has continued coding") {
		t.Fatalf("resolution canonical must not claim downstream coding, got %q", recorded.CanonicalResponse)
	}
	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.ActiveBranchClass != ActiveBranchClassLocal {
		t.Fatalf("expected branch ownership to return to local after resolution, got %s", status.ActiveBranchClass)
	}
}

func TestRecordHandoffResolutionRejectsNoActiveBranchAndReplaysDeterministically(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RecordHandoffResolution(context.Background(), RecordHandoffResolutionRequest{
		TaskID: string(taskID),
		Kind:   handoff.ResolutionAbandoned,
	}); err == nil || !strings.Contains(err.Error(), "active Claude handoff branch") {
		t.Fatalf("expected no-active-handoff rejection, got %v", err)
	}

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "replay resolution test",
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

	first, err := coord.RecordHandoffResolution(context.Background(), RecordHandoffResolutionRequest{
		TaskID:  string(taskID),
		Kind:    handoff.ResolutionReviewedStale,
		Summary: "reviewed stale Claude branch",
	})
	if err != nil {
		t.Fatalf("record first resolution: %v", err)
	}
	second, err := coord.RecordHandoffResolution(context.Background(), RecordHandoffResolutionRequest{
		TaskID:  string(taskID),
		Kind:    handoff.ResolutionReviewedStale,
		Summary: "reviewed stale Claude branch",
	})
	if err != nil {
		t.Fatalf("record replayed resolution: %v", err)
	}
	if first.Record.ResolutionID != second.Record.ResolutionID {
		t.Fatalf("expected replay reuse of resolution, got first=%s second=%s", first.Record.ResolutionID, second.Record.ResolutionID)
	}
	transitionHistory, err := store.TransitionReceipts().ListByTask(taskID, 10)
	if err != nil {
		t.Fatalf("list transition receipts: %v", err)
	}
	if len(transitionHistory) != 1 {
		t.Fatalf("expected exactly one transition receipt for replayed resolution, got %+v", transitionHistory)
	}
	if first.TransitionReceiptID == "" || transitionHistory[0].ReceiptID != first.TransitionReceiptID {
		t.Fatalf("expected first resolution transition receipt %s to remain latest, got %+v", first.TransitionReceiptID, transitionHistory[0])
	}
	if second.TransitionReceiptID != "" {
		t.Fatalf("expected replayed resolution to avoid creating a new transition receipt id, got %+v", second)
	}
	if _, err := coord.RecordHandoffResolution(context.Background(), RecordHandoffResolutionRequest{
		TaskID:  string(taskID),
		Kind:    handoff.ResolutionAbandoned,
		Summary: "try different resolution after closure",
	}); err == nil || !strings.Contains(err.Error(), "already resolved") {
		t.Fatalf("expected already-resolved rejection for different resolution, got %v", err)
	}
}

func TestCreateHandoffDoesNotReuseResolvedClaudeBranch(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	first, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "resolved packet must not be reused",
		Mode:         handoff.ModeResume,
		Notes:        []string{"first packet"},
	})
	if err != nil {
		t.Fatalf("create first handoff: %v", err)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  first.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
	}); err != nil {
		t.Fatalf("accept first handoff: %v", err)
	}
	if _, err := coord.RecordHandoffResolution(context.Background(), RecordHandoffResolutionRequest{
		TaskID:  string(taskID),
		Kind:    handoff.ResolutionSupersededByLocal,
		Summary: "return local control before creating a new handoff",
	}); err != nil {
		t.Fatalf("resolve first handoff: %v", err)
	}

	second, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "resolved packet must not be reused",
		Mode:         handoff.ModeResume,
		Notes:        []string{"first packet"},
	})
	if err != nil {
		t.Fatalf("create second handoff: %v", err)
	}
	if second.HandoffID == first.HandoffID {
		t.Fatalf("expected a new handoff packet after resolution, got reused id %s", second.HandoffID)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  second.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
	}); err != nil {
		t.Fatalf("accept second handoff: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.HandoffContinuityState != HandoffContinuityStateAcceptedNotLaunched {
		t.Fatalf("expected new accepted handoff to become active continuity, got %s", status.HandoffContinuityState)
	}
	if status.ActiveBranchClass != ActiveBranchClassHandoffClaude {
		t.Fatalf("expected fresh new Claude handoff to become active branch owner, got %s", status.ActiveBranchClass)
	}
	if status.ActiveBranchRef != second.HandoffID {
		t.Fatalf("expected fresh active branch ref %s, got %s", second.HandoffID, status.ActiveBranchRef)
	}
	if status.LatestResolutionKind != handoff.ResolutionSupersededByLocal {
		t.Fatalf("expected historical resolution to remain visible, got %s", status.LatestResolutionKind)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.Handoff == nil || inspectOut.Handoff.HandoffID != second.HandoffID {
		t.Fatalf("expected inspect to surface new active handoff, got %+v", inspectOut.Handoff)
	}
	if inspectOut.ActiveBranch == nil || inspectOut.ActiveBranch.Class != ActiveBranchClassHandoffClaude || inspectOut.ActiveBranch.BranchRef != second.HandoffID {
		t.Fatalf("expected inspect to surface new active branch owner, got %+v", inspectOut.ActiveBranch)
	}
	if inspectOut.Resolution == nil || inspectOut.Resolution.HandoffID != first.HandoffID {
		t.Fatalf("expected inspect to retain historical resolution for first handoff, got %+v", inspectOut.Resolution)
	}
}

func TestLaunchHandoffDefaultsToNewestActiveBranchAfterOlderResolution(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherSuccess()
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	first, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "older branch that will be resolved",
		Mode:         handoff.ModeResume,
	})
	if err != nil {
		t.Fatalf("create first handoff: %v", err)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  first.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
	}); err != nil {
		t.Fatalf("accept first handoff: %v", err)
	}
	if _, err := coord.RecordHandoffResolution(context.Background(), RecordHandoffResolutionRequest{
		TaskID:  string(taskID),
		Kind:    handoff.ResolutionSupersededByLocal,
		Summary: "first branch explicitly closed",
	}); err != nil {
		t.Fatalf("resolve first handoff: %v", err)
	}

	second, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "new active branch",
		Mode:         handoff.ModeResume,
	})
	if err != nil {
		t.Fatalf("create second handoff: %v", err)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  second.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
	}); err != nil {
		t.Fatalf("accept second handoff: %v", err)
	}

	launchOut, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID: string(taskID),
	})
	if err != nil {
		t.Fatalf("launch default active handoff: %v", err)
	}
	if launchOut.HandoffID != second.HandoffID {
		t.Fatalf("expected default launch selection to use newest active handoff %s, got %s", second.HandoffID, launchOut.HandoffID)
	}
}

func TestLaunchHandoffRejectedAfterExplicitResolution(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherSuccess()
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "resolved handoff must not relaunch",
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
	if _, err := coord.RecordHandoffResolution(context.Background(), RecordHandoffResolutionRequest{
		TaskID:  string(taskID),
		Kind:    handoff.ResolutionAbandoned,
		Summary: "explicitly retire Claude branch",
	}); err != nil {
		t.Fatalf("resolve handoff: %v", err)
	}

	out, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("launch resolved handoff: %v", err)
	}
	if out.LaunchStatus != HandoffLaunchStatusBlocked {
		t.Fatalf("expected blocked launch after resolution, got %+v", out)
	}
	if launcher.called {
		t.Fatal("launcher should not be called for an explicitly resolved handoff branch")
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "explicitly resolved") {
		t.Fatalf("expected resolved-branch launch block canonical response, got %q", out.CanonicalResponse)
	}
}

func TestCompletedClaudeLaunchWithoutPersistedAcknowledgmentRequiresRepair(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "missing acknowledgment continuity invariant",
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

	now := time.Now().UTC()
	if err := store.Handoffs().CreateLaunch(handoff.Launch{
		Version:      1,
		AttemptID:    "hlc_completed_missing_ack",
		HandoffID:    createOut.HandoffID,
		TaskID:       taskID,
		TargetWorker: rundomain.WorkerKindClaude,
		Status:       handoff.LaunchStatusCompleted,
		LaunchID:     "launch_missing_ack",
		PayloadHash:  "payload_missing_ack",
		RequestedAt:  now,
		StartedAt:    now,
		EndedAt:      now.Add(50 * time.Millisecond),
		Summary:      "launch completed without persisted ack for invariant test",
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("create completed launch without ack: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.RecoveryClass != RecoveryClassRepairRequired {
		t.Fatalf("expected repair-required recovery for missing ack continuity break, got %s", status.RecoveryClass)
	}
	if status.HandoffContinuityState != HandoffContinuityStateLaunchCompletedAckLost {
		t.Fatalf("expected missing-ack handoff continuity state, got %s", status.HandoffContinuityState)
	}
	if !strings.Contains(strings.ToLower(status.HandoffContinuityReason), "no persisted acknowledgment") {
		t.Fatalf("expected missing-ack continuity reason, got %q", status.HandoffContinuityReason)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.HandoffContinuity == nil || inspectOut.HandoffContinuity.State != HandoffContinuityStateLaunchCompletedAckLost {
		t.Fatalf("expected inspect missing-ack handoff continuity, got %+v", inspectOut.HandoffContinuity)
	}
}

func TestLaunchHandoffBlockedUsesPacketTargetWhenRequestTargetEmpty(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherSuccess()
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	cpOut, err := coord.CreateCheckpoint(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("create checkpoint: %v", err)
	}
	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	briefRec, err := store.Briefs().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("get latest brief: %v", err)
	}
	cp, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("get latest checkpoint: %v", err)
	}
	if cp.CheckpointID != cpOut.CheckpointID {
		t.Fatalf("expected checkpoint %s, got %s", cpOut.CheckpointID, cp.CheckpointID)
	}

	packet := handoff.Packet{
		Version:          1,
		HandoffID:        "hnd_codex_target_for_block_test",
		TaskID:           taskID,
		Status:           handoff.StatusCreated,
		SourceWorker:     rundomain.WorkerKindUnknown,
		TargetWorker:     rundomain.WorkerKindCodex,
		HandoffMode:      handoff.ModeResume,
		Reason:           "seed unsupported target launch packet",
		CurrentPhase:     caps.CurrentPhase,
		CheckpointID:     cp.CheckpointID,
		BriefID:          briefRec.BriefID,
		IntentID:         caps.CurrentIntentID,
		CapsuleVersion:   caps.Version,
		RepoAnchor:       cp.Anchor,
		IsResumable:      true,
		ResumeDescriptor: cp.ResumeDescriptor,
		Goal:             caps.Goal,
		BriefObjective:   briefRec.Objective,
		NormalizedAction: briefRec.NormalizedAction,
		Constraints:      append([]string{}, briefRec.Constraints...),
		DoneCriteria:     append([]string{}, briefRec.DoneCriteria...),
		TouchedFiles:     append([]string{}, caps.TouchedFiles...),
		Blockers:         append([]string{}, caps.Blockers...),
		NextAction:       caps.NextAction,
		CreatedAt:        time.Now().UTC(),
	}
	if err := store.Handoffs().Create(packet); err != nil {
		t.Fatalf("create handoff packet: %v", err)
	}

	out, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: packet.HandoffID,
	})
	if err != nil {
		t.Fatalf("launch handoff: %v", err)
	}
	if out.LaunchStatus != HandoffLaunchStatusBlocked {
		t.Fatalf("expected blocked status, got %s", out.LaunchStatus)
	}
	if out.TargetWorker != rundomain.WorkerKindCodex {
		t.Fatalf("expected blocked target worker to match packet target %s, got %s", rundomain.WorkerKindCodex, out.TargetWorker)
	}
	if launcher.called {
		t.Fatal("launcher should not be called for unsupported target")
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func newTestCoordinatorWithLauncher(t *testing.T, store *sqlite.Store, anchorProvider anchorgit.Provider, adapter adapter_contract.WorkerAdapter, launcher adapter_contract.HandoffLauncher) *Coordinator {
	t.Helper()
	coord, err := NewCoordinator(Dependencies{
		Store:           store,
		IntentCompiler:  NewIntentStubCompiler(),
		BriefBuilder:    NewBriefBuilderV1(nil, nil),
		WorkerAdapter:   adapter,
		HandoffLauncher: launcher,
		Synthesizer:     canonical.NewSimpleSynthesizer(),
		AnchorProvider:  anchorProvider,
		ShellSessions:   NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	return coord
}

type fakeHandoffLauncher struct {
	kind      adapter_contract.WorkerKind
	result    adapter_contract.HandoffLaunchResult
	err       error
	called    bool
	callCount int
	lastReq   adapter_contract.HandoffLaunchRequest
}

func newFakeHandoffLauncherSuccess() *fakeHandoffLauncher {
	now := time.Now().UTC()
	return &fakeHandoffLauncher{
		kind: adapter_contract.WorkerClaude,
		result: adapter_contract.HandoffLaunchResult{
			LaunchID:     "hlc_test",
			TargetWorker: adapter_contract.WorkerClaude,
			StartedAt:    now,
			EndedAt:      now.Add(150 * time.Millisecond),
			Command:      "claude",
			Args:         []string{"--print"},
			ExitCode:     0,
			Stdout:       "claude handoff launch accepted",
			Summary:      "handoff launch accepted",
		},
	}
}

func newFakeHandoffLauncherError(err error) *fakeHandoffLauncher {
	now := time.Now().UTC()
	return &fakeHandoffLauncher{
		kind: adapter_contract.WorkerClaude,
		result: adapter_contract.HandoffLaunchResult{
			LaunchID:     "hlc_err",
			TargetWorker: adapter_contract.WorkerClaude,
			StartedAt:    now,
			EndedAt:      now.Add(50 * time.Millisecond),
			Command:      "claude",
			Args:         []string{},
			ExitCode:     1,
			Stderr:       "launcher failed",
			Summary:      "handoff launch failed",
		},
		err: err,
	}
}

func newFakeHandoffLauncherUnusableOutput() *fakeHandoffLauncher {
	now := time.Now().UTC()
	return &fakeHandoffLauncher{
		kind: adapter_contract.WorkerClaude,
		result: adapter_contract.HandoffLaunchResult{
			LaunchID:     "hlc_unusable",
			TargetWorker: adapter_contract.WorkerClaude,
			StartedAt:    now,
			EndedAt:      now.Add(80 * time.Millisecond),
			Command:      "claude",
			Args:         []string{"--print"},
			ExitCode:     0,
			Stdout:       "   \n  ",
			Stderr:       "",
			Summary:      "",
		},
	}
}

func (f *fakeHandoffLauncher) Name() adapter_contract.WorkerKind {
	return f.kind
}

func (f *fakeHandoffLauncher) LaunchHandoff(_ context.Context, req adapter_contract.HandoffLaunchRequest) (adapter_contract.HandoffLaunchResult, error) {
	f.called = true
	f.callCount++
	f.lastReq = req
	out := f.result
	if out.TargetWorker == "" {
		out.TargetWorker = req.TargetWorker
	}
	if out.LaunchID == "" {
		out.LaunchID = "hlc_generated"
	}
	if out.StartedAt.IsZero() {
		out.StartedAt = time.Now().UTC()
	}
	if out.EndedAt.IsZero() {
		out.EndedAt = out.StartedAt.Add(100 * time.Millisecond)
	}
	return out, f.err
}

var _ adapter_contract.HandoffLauncher = (*fakeHandoffLauncher)(nil)

func (s *faultInjectedStore) Handoffs() storage.HandoffStore {
	return s.base.Handoffs()
}

func (s *txCountingStore) Handoffs() storage.HandoffStore {
	return s.base.Handoffs()
}

var _ storage.Store = (*faultInjectedStore)(nil)
var _ storage.Store = (*txCountingStore)(nil)
