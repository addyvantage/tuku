package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/recoveryaction"
	rundomain "tuku/internal/domain/run"
)

func testShellActionAuthority(t *testing.T, actions []ShellOperatorActionAuthority, action OperatorAction) ShellOperatorActionAuthority {
	t.Helper()
	for _, candidate := range actions {
		if candidate.Action == action {
			return candidate
		}
	}
	t.Fatalf("missing shell action authority for %s", action)
	return ShellOperatorActionAuthority{}
}

func requireShellDecision(t *testing.T, decision *ShellOperatorDecisionSummary) *ShellOperatorDecisionSummary {
	t.Helper()
	if decision == nil {
		t.Fatal("expected shell operator decision summary")
	}
	return decision
}

func requireShellExecutionPlan(t *testing.T, plan *ShellOperatorExecutionPlan) *ShellOperatorExecutionPlan {
	t.Helper()
	if plan == nil {
		t.Fatal("expected shell operator execution plan")
	}
	if plan.PrimaryStep == nil {
		t.Fatal("expected shell operator execution primary step")
	}
	return plan
}

func TestShellSnapshotTaskBuildsShellStateFromPersistedTaskState(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	runRes, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start: %v", err)
	}
	if _, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Mode:         handoff.ModeResume,
		Reason:       "shell test",
	}); err != nil {
		t.Fatalf("create handoff: %v", err)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.TaskID != taskID {
		t.Fatalf("expected task id %s, got %s", taskID, snapshot.TaskID)
	}
	if snapshot.Brief == nil || snapshot.Brief.BriefID == "" {
		t.Fatal("expected brief summary in shell snapshot")
	}
	if snapshot.Run == nil || snapshot.Run.RunID != runRes.RunID {
		t.Fatalf("expected latest run summary, got %+v", snapshot.Run)
	}
	if snapshot.Checkpoint == nil || snapshot.Checkpoint.CheckpointID == "" {
		t.Fatal("expected checkpoint summary in shell snapshot")
	}
	if snapshot.Handoff == nil || snapshot.Handoff.TargetWorker != rundomain.WorkerKindClaude {
		t.Fatalf("expected latest handoff summary, got %+v", snapshot.Handoff)
	}
	if snapshot.ActiveBranch == nil || snapshot.ActiveBranch.Class != ActiveBranchClassLocal {
		t.Fatalf("expected default local branch owner in shell snapshot, got %+v", snapshot.ActiveBranch)
	}
	if snapshot.LocalRunFinalization == nil || snapshot.LocalRunFinalization.State != LocalRunFinalizationFinalized {
		t.Fatalf("expected finalized local run finalization in shell snapshot, got %+v", snapshot.LocalRunFinalization)
	}
	if snapshot.Recovery == nil {
		t.Fatal("expected recovery summary in shell snapshot")
	}
	if snapshot.LaunchControl == nil {
		t.Fatal("expected launch control summary in shell snapshot")
	}
	if len(snapshot.RecentProofs) == 0 {
		t.Fatal("expected proof highlights")
	}
	if len(snapshot.RecentConversation) == 0 {
		t.Fatal("expected recent conversation")
	}
	if snapshot.LatestCanonicalResponse == "" {
		t.Fatal("expected latest canonical response")
	}
}

func TestShellSnapshotInterruptedRunExposesRecoverableState(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{
		TaskID:             string(taskID),
		Action:             "interrupt",
		RunID:              startOut.RunID,
		InterruptionReason: "operator interrupted",
	}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Recovery == nil {
		t.Fatal("expected recovery summary")
	}
	if snapshot.Recovery.RecoveryClass != RecoveryClassInterruptedRunRecoverable {
		t.Fatalf("expected interrupted recoverable class, got %s", snapshot.Recovery.RecoveryClass)
	}
	if snapshot.Recovery.RecommendedAction != RecoveryActionResumeInterrupted {
		t.Fatalf("expected interrupted recovery action, got %s", snapshot.Recovery.RecommendedAction)
	}
	if snapshot.Recovery.ReadyForNextRun {
		t.Fatal("interrupted recovery must not appear fresh-start ready in shell snapshot")
	}
	if snapshot.LocalRunFinalization == nil || snapshot.LocalRunFinalization.State != LocalRunFinalizationInterruptedRecoverable {
		t.Fatalf("expected interrupted local run finalization in shell snapshot, got %+v", snapshot.LocalRunFinalization)
	}
	if snapshot.LocalResume == nil || snapshot.LocalResume.State != LocalResumeAuthorityAllowed || snapshot.LocalResume.Mode != LocalResumeModeResumeInterruptedLineage {
		t.Fatalf("expected shell local resume authority for interrupted lineage, got %+v", snapshot.LocalResume)
	}
	decision := requireShellDecision(t, snapshot.OperatorDecision)
	if decision.Headline != "Interrupted local lineage recoverable" || decision.RequiredNextAction != OperatorActionResumeInterruptedLineage {
		t.Fatalf("unexpected shell interrupted decision summary: %+v", decision)
	}
	plan := requireShellExecutionPlan(t, snapshot.OperatorExecutionPlan)
	if plan.PrimaryStep.Action != OperatorActionResumeInterruptedLineage || plan.PrimaryStep.Status != OperatorActionAuthorityRequiredNext {
		t.Fatalf("unexpected shell interrupted execution plan: %+v", plan)
	}
	if snapshot.LaunchControl == nil || snapshot.LaunchControl.State != LaunchControlStateNotApplicable {
		t.Fatalf("expected non-applicable launch control, got %+v", snapshot.LaunchControl)
	}
}

func TestShellSnapshotInterruptedRunReviewedStillShowsInterruptedRecoverableState(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{
		TaskID:             string(taskID),
		Action:             "interrupt",
		RunID:              startOut.RunID,
		InterruptionReason: "operator interrupted",
	}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID:  string(taskID),
		Kind:    recoveryaction.KindInterruptedRunReviewed,
		Summary: "interrupted lineage reviewed",
	}); err != nil {
		t.Fatalf("record interrupted review: %v", err)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Recovery == nil || snapshot.Recovery.RecoveryClass != RecoveryClassInterruptedRunRecoverable {
		t.Fatalf("expected interrupted recoverable class after review, got %+v", snapshot.Recovery)
	}
	if snapshot.Recovery.ReadyForNextRun {
		t.Fatal("interrupted review must not make shell snapshot fresh-start ready")
	}
	if !strings.Contains(strings.ToLower(snapshot.Recovery.Reason), "reviewed") {
		t.Fatalf("expected reviewed interrupted-lineage reason, got %q", snapshot.Recovery.Reason)
	}
	found := false
	for _, evt := range snapshot.RecentProofs {
		if evt.Type == proof.EventInterruptedRunReviewed {
			found = true
			if evt.Summary != "Interrupted run reviewed" {
				t.Fatalf("unexpected interrupted review proof summary %q", evt.Summary)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected interrupted review proof highlight in shell snapshot")
	}
}

func TestShellSnapshotInterruptedResumeShowsContinueExecutionRequiredState(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{
		TaskID:             string(taskID),
		Action:             "interrupt",
		RunID:              startOut.RunID,
		InterruptionReason: "operator interrupted",
	}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}
	if _, err := coord.ExecuteInterruptedResume(context.Background(), ExecuteInterruptedResumeRequest{
		TaskID:  string(taskID),
		Summary: "operator resumed interrupted lineage",
	}); err != nil {
		t.Fatalf("execute interrupted resume: %v", err)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Phase != string(phase.PhaseBriefReady) {
		t.Fatalf("expected shell snapshot phase %s after interrupted resume, got %s", phase.PhaseBriefReady, snapshot.Phase)
	}
	if snapshot.Recovery == nil || snapshot.Recovery.RecoveryClass != RecoveryClassContinueExecutionRequired {
		t.Fatalf("expected continue-execution-required shell recovery, got %+v", snapshot.Recovery)
	}
	if snapshot.Recovery.RecommendedAction != RecoveryActionExecuteContinueRecovery {
		t.Fatalf("expected execute-continue-recovery shell action, got %+v", snapshot.Recovery)
	}
	if snapshot.Recovery.ReadyForNextRun {
		t.Fatal("interrupted resume must not make shell snapshot fresh-start ready")
	}
	if snapshot.LocalRunFinalization == nil || snapshot.LocalRunFinalization.State != LocalRunFinalizationInterruptedRecoverable {
		t.Fatalf("expected interrupted finalization summary to remain distinct after interrupted resume, got %+v", snapshot.LocalRunFinalization)
	}
	if snapshot.LocalResume == nil || snapshot.LocalResume.State != LocalResumeAuthorityNotApplicable || snapshot.LocalResume.Mode != LocalResumeModeFinalizeContinueRecovery {
		t.Fatalf("expected shell local resume summary to require continue finalization, got %+v", snapshot.LocalResume)
	}
	if !strings.Contains(strings.ToLower(snapshot.Recovery.Reason), "interrupted") {
		t.Fatalf("expected interrupted-lineage reason in shell snapshot, got %q", snapshot.Recovery.Reason)
	}
	found := false
	for _, evt := range snapshot.RecentProofs {
		if evt.Type == proof.EventInterruptedRunResumeExecuted {
			found = true
			if evt.Summary != "Interrupted lineage continuation selected" {
				t.Fatalf("unexpected interrupted resume proof summary %q", evt.Summary)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected interrupted resume proof highlight in shell snapshot")
	}
}

func TestShellSnapshotStaleRunShowsReconciliationDistinctFromResume(t *testing.T) {
	store := newTestStore(t)
	taskID := setupTaskWithBrief(t, newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess()))

	failCoord, err := NewCoordinator(Dependencies{
		Store:          store,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    &failingSynthesizer{err: context.DeadlineExceeded},
		AnchorProvider: defaultAnchor(),
		ShellSessions:  NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new failing coordinator: %v", err)
	}
	if _, err := failCoord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err == nil {
		t.Fatal("expected staged finalization failure to leave stale running state")
	}

	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.LocalRunFinalization == nil || snapshot.LocalRunFinalization.State != LocalRunFinalizationStaleReconciliationNeeded {
		t.Fatalf("expected stale local run finalization in shell snapshot, got %+v", snapshot.LocalRunFinalization)
	}
	if snapshot.LocalResume == nil || snapshot.LocalResume.State != LocalResumeAuthorityNotApplicable {
		t.Fatalf("expected stale run to remain distinct from interrupted resume authority, got %+v", snapshot.LocalResume)
	}
	if snapshot.Recovery == nil || snapshot.Recovery.RecoveryClass != RecoveryClassStaleRunReconciliationRequired {
		t.Fatalf("expected stale-run reconciliation recovery in shell snapshot, got %+v", snapshot.Recovery)
	}
	if snapshot.ActionAuthority == nil || snapshot.ActionAuthority.RequiredNextAction != OperatorActionReconcileStaleRun {
		t.Fatalf("expected shell action authority to require stale reconciliation, got %+v", snapshot.ActionAuthority)
	}
	startAuthority := testShellActionAuthority(t, snapshot.ActionAuthority.Actions, OperatorActionStartLocalRun)
	if startAuthority.State != OperatorActionAuthorityBlocked {
		t.Fatalf("expected stale run to block fresh local run authority in shell snapshot, got %+v", startAuthority)
	}
	decision := requireShellDecision(t, snapshot.OperatorDecision)
	if decision.Headline != "Stale local run reconciliation required" {
		t.Fatalf("unexpected shell stale decision summary: %+v", decision)
	}
	plan := requireShellExecutionPlan(t, snapshot.OperatorExecutionPlan)
	if plan.PrimaryStep.Action != OperatorActionReconcileStaleRun || plan.PrimaryStep.CommandHint != "tuku continue --task "+string(taskID) {
		t.Fatalf("unexpected shell stale execution plan: %+v", plan)
	}
}

func TestShellSnapshotFailedRunDoesNotOverclaimReadiness(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("run start: %v", err)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Checkpoint == nil {
		t.Fatal("expected checkpoint summary")
	}
	if snapshot.Checkpoint.IsResumable {
		t.Fatalf("failed run checkpoint should not look resumable: %+v", snapshot.Checkpoint)
	}
	if snapshot.Recovery == nil {
		t.Fatal("expected recovery summary")
	}
	if snapshot.Recovery.RecoveryClass != RecoveryClassFailedRunReviewRequired {
		t.Fatalf("expected failed-run recovery class, got %s", snapshot.Recovery.RecoveryClass)
	}
	if snapshot.Recovery.ReadyForNextRun {
		t.Fatal("failed run should not look ready for next run")
	}
	if snapshot.Recovery.RecommendedAction != RecoveryActionInspectFailedRun {
		t.Fatalf("expected inspect-failed-run action, got %s", snapshot.Recovery.RecommendedAction)
	}
}

func TestShellSnapshotAcceptedHandoffLaunchReadyState(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "launch ready shell snapshot",
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

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Recovery == nil || snapshot.Recovery.RecoveryClass != RecoveryClassAcceptedHandoffLaunchReady {
		t.Fatalf("expected accepted handoff launch-ready recovery, got %+v", snapshot.Recovery)
	}
	if !snapshot.Recovery.ReadyForHandoffLaunch {
		t.Fatal("expected handoff launch readiness")
	}
	if snapshot.HandoffContinuity == nil || snapshot.HandoffContinuity.State != HandoffContinuityStateAcceptedNotLaunched {
		t.Fatalf("expected accepted-not-launched handoff continuity, got %+v", snapshot.HandoffContinuity)
	}
	if snapshot.ActiveBranch == nil || snapshot.ActiveBranch.Class != ActiveBranchClassHandoffClaude || snapshot.ActiveBranch.BranchRef != createOut.HandoffID {
		t.Fatalf("expected accepted Claude handoff to own active branch, got %+v", snapshot.ActiveBranch)
	}
	if snapshot.LocalRunFinalization == nil || snapshot.LocalRunFinalization.State != LocalRunFinalizationNoRelevantRun {
		t.Fatalf("expected no local run finalization anchor for accepted handoff case, got %+v", snapshot.LocalRunFinalization)
	}
	if snapshot.LocalResume == nil || snapshot.LocalResume.State != LocalResumeAuthorityBlocked {
		t.Fatalf("expected accepted Claude handoff to block shell local resume authority, got %+v", snapshot.LocalResume)
	}
	if snapshot.ActionAuthority == nil || snapshot.ActionAuthority.RequiredNextAction != OperatorActionLaunchAcceptedHandoff {
		t.Fatalf("expected shell action authority to require handoff launch, got %+v", snapshot.ActionAuthority)
	}
	messageAuthority := testShellActionAuthority(t, snapshot.ActionAuthority.Actions, OperatorActionLocalMessageMutation)
	if messageAuthority.State != OperatorActionAuthorityBlocked {
		t.Fatalf("expected shell action authority to block local message mutation, got %+v", messageAuthority)
	}
	launchAuthority := testShellActionAuthority(t, snapshot.ActionAuthority.Actions, OperatorActionLaunchAcceptedHandoff)
	if launchAuthority.State != OperatorActionAuthorityRequiredNext {
		t.Fatalf("expected shell action authority to require launch, got %+v", launchAuthority)
	}
	decision := requireShellDecision(t, snapshot.OperatorDecision)
	if decision.Headline != "Accepted Claude handoff launch ready" {
		t.Fatalf("unexpected shell accepted-handoff decision summary: %+v", decision)
	}
	plan := requireShellExecutionPlan(t, snapshot.OperatorExecutionPlan)
	if plan.PrimaryStep.Action != OperatorActionLaunchAcceptedHandoff || plan.PrimaryStep.CommandHint != "tuku handoff-launch --task "+string(taskID)+" --handoff "+createOut.HandoffID {
		t.Fatalf("unexpected shell accepted-handoff execution plan: %+v", plan)
	}
	if snapshot.LaunchControl == nil || snapshot.LaunchControl.State != LaunchControlStateNotRequested {
		t.Fatalf("expected not-requested launch control state, got %+v", snapshot.LaunchControl)
	}
	if snapshot.LaunchControl.RetryDisposition != LaunchRetryDispositionAllowed {
		t.Fatalf("expected allowed launch retry disposition, got %+v", snapshot.LaunchControl)
	}
}

func TestShellSnapshotResolvedClaudeHandoffShowsExplicitResolutionWithoutBlocking(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "resolve accepted handoff in shell snapshot",
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
		Kind:    handoff.ResolutionSupersededByLocal,
		Summary: "operator returned local control",
	}); err != nil {
		t.Fatalf("record handoff resolution: %v", err)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Handoff == nil || snapshot.Handoff.HandoffID != createOut.HandoffID {
		t.Fatalf("expected historical handoff summary after explicit resolution, got %+v", snapshot.Handoff)
	}
	if snapshot.HandoffContinuity == nil || snapshot.HandoffContinuity.State != HandoffContinuityStateNotApplicable {
		t.Fatalf("expected no active handoff continuity after resolution, got %+v", snapshot.HandoffContinuity)
	}
	if snapshot.ActiveBranch == nil || snapshot.ActiveBranch.Class != ActiveBranchClassLocal {
		t.Fatalf("expected local branch owner after resolution, got %+v", snapshot.ActiveBranch)
	}
	if snapshot.Resolution == nil || snapshot.Resolution.Kind != handoff.ResolutionSupersededByLocal {
		t.Fatalf("expected shell resolution summary, got %+v", snapshot.Resolution)
	}
	if snapshot.Recovery == nil || !snapshot.Recovery.ReadyForNextRun {
		t.Fatalf("expected local next-run readiness after resolution, got %+v", snapshot.Recovery)
	}
	if snapshot.LocalResume == nil || snapshot.LocalResume.State != LocalResumeAuthorityNotApplicable || snapshot.LocalResume.Mode != LocalResumeModeStartFreshNextRun {
		t.Fatalf("expected shell local resume summary to return to fresh-next-run truth after resolution, got %+v", snapshot.LocalResume)
	}
	found := false
	for _, evt := range snapshot.RecentProofs {
		if evt.Type == proof.EventHandoffResolutionRecorded {
			found = true
			if evt.Summary != "Handoff resolution recorded" {
				t.Fatalf("unexpected handoff resolution proof summary %q", evt.Summary)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected handoff resolution proof highlight in shell snapshot")
	}
}

func TestShellSnapshotNewActiveClaudeHandoffOutranksOlderResolvedHistory(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	first, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "older branch to resolve",
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
		Summary: "return local control before issuing a fresh handoff",
	}); err != nil {
		t.Fatalf("resolve first handoff: %v", err)
	}

	second, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "fresh active branch",
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

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Handoff == nil || snapshot.Handoff.HandoffID != second.HandoffID {
		t.Fatalf("expected shell snapshot to surface new active handoff, got %+v", snapshot.Handoff)
	}
	if snapshot.ActiveBranch == nil || snapshot.ActiveBranch.Class != ActiveBranchClassHandoffClaude || snapshot.ActiveBranch.BranchRef != second.HandoffID {
		t.Fatalf("expected new active handoff to own shell branch provenance, got %+v", snapshot.ActiveBranch)
	}
	if snapshot.HandoffContinuity == nil || snapshot.HandoffContinuity.State != HandoffContinuityStateAcceptedNotLaunched {
		t.Fatalf("expected new active accepted handoff continuity, got %+v", snapshot.HandoffContinuity)
	}
	if snapshot.Resolution == nil || snapshot.Resolution.Kind != handoff.ResolutionSupersededByLocal {
		t.Fatalf("expected historical resolution to remain visible, got %+v", snapshot.Resolution)
	}
}

func TestShellSnapshotPendingLaunchUnknownOutcomeState(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "pending launch shell snapshot",
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
		AttemptID:    "hlc_shell_pending",
		HandoffID:    createOut.HandoffID,
		TaskID:       taskID,
		TargetWorker: rundomain.WorkerKindClaude,
		Status:       handoff.LaunchStatusRequested,
		PayloadHash:  "shell_pending_hash",
		RequestedAt:  now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("create launch record: %v", err)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Recovery == nil || snapshot.Recovery.RecoveryClass != RecoveryClassHandoffLaunchPendingOutcome {
		t.Fatalf("expected pending launch recovery class, got %+v", snapshot.Recovery)
	}
	if snapshot.Recovery.ReadyForNextRun {
		t.Fatal("pending launch should not look ready for next run")
	}
	if snapshot.LaunchControl == nil || snapshot.LaunchControl.State != LaunchControlStateRequestedOutcomeUnknown {
		t.Fatalf("expected requested-outcome-unknown launch control, got %+v", snapshot.LaunchControl)
	}
	if snapshot.LaunchControl.RetryDisposition != LaunchRetryDispositionBlocked {
		t.Fatalf("expected blocked launch retry, got %+v", snapshot.LaunchControl)
	}
	if snapshot.Launch == nil || snapshot.Launch.Status != handoff.LaunchStatusRequested {
		t.Fatalf("expected latest launch summary, got %+v", snapshot.Launch)
	}
}

func TestShellSnapshotCompletedLaunchStateDoesNotOverclaimDownstreamCompletion(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherSuccess()
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "completed launch shell snapshot",
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

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Recovery == nil || snapshot.Recovery.RecoveryClass != RecoveryClassHandoffLaunchCompleted {
		t.Fatalf("expected completed launch recovery class, got %+v", snapshot.Recovery)
	}
	if snapshot.Recovery.RecommendedAction != RecoveryActionMonitorLaunchedHandoff {
		t.Fatalf("expected monitor launched handoff action, got %+v", snapshot.Recovery)
	}
	if snapshot.LaunchControl == nil || snapshot.LaunchControl.State != LaunchControlStateCompleted {
		t.Fatalf("expected completed launch control, got %+v", snapshot.LaunchControl)
	}
	if snapshot.LaunchControl.RetryDisposition != LaunchRetryDispositionBlocked {
		t.Fatalf("expected blocked retry after durable completion, got %+v", snapshot.LaunchControl)
	}
	if snapshot.Acknowledgment == nil {
		t.Fatal("expected persisted acknowledgment after completed launch")
	}
	if snapshot.HandoffContinuity == nil || snapshot.HandoffContinuity.State != HandoffContinuityStateLaunchCompletedAckSeen {
		t.Fatalf("expected captured-ack handoff continuity, got %+v", snapshot.HandoffContinuity)
	}
	if snapshot.ActionAuthority == nil {
		t.Fatal("expected shell action authority")
	}
	resolveAuthority := testShellActionAuthority(t, snapshot.ActionAuthority.Actions, OperatorActionResolveActiveHandoff)
	if resolveAuthority.State != OperatorActionAuthorityAllowed {
		t.Fatalf("expected launched handoff to allow explicit resolution in shell snapshot, got %+v", resolveAuthority)
	}
	messageAuthority := testShellActionAuthority(t, snapshot.ActionAuthority.Actions, OperatorActionLocalMessageMutation)
	if messageAuthority.State != OperatorActionAuthorityBlocked {
		t.Fatalf("expected launched handoff to block local mutation in shell snapshot, got %+v", messageAuthority)
	}
	decision := requireShellDecision(t, snapshot.OperatorDecision)
	if decision.Headline != "Claude handoff branch active" {
		t.Fatalf("unexpected shell launched-handoff decision summary: %+v", decision)
	}
	plan := requireShellExecutionPlan(t, snapshot.OperatorExecutionPlan)
	if plan.PrimaryStep.Action != OperatorActionResolveActiveHandoff || plan.PrimaryStep.Status != OperatorActionAuthorityAllowed {
		t.Fatalf("unexpected shell launched-handoff execution plan: %+v", plan)
	}
	if snapshot.HandoffContinuity.DownstreamContinuationProven {
		t.Fatal("completed launch shell snapshot must not claim downstream continuation proven")
	}
	if snapshot.Recovery.Reason == "" || !strings.Contains(strings.ToLower(snapshot.Recovery.Reason), "launch") {
		t.Fatalf("expected launch-specific recovery reason, got %+v", snapshot.Recovery)
	}
}

func TestShellSnapshotCompletedLaunchWithUnavailableAcknowledgmentShowsUnprovenContinuity(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherUnusableOutput())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "unavailable acknowledgment shell snapshot",
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

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.HandoffContinuity == nil || snapshot.HandoffContinuity.State != HandoffContinuityStateLaunchCompletedAckEmpty {
		t.Fatalf("expected unavailable-ack handoff continuity in shell snapshot, got %+v", snapshot.HandoffContinuity)
	}
	if snapshot.HandoffContinuity.DownstreamContinuationProven {
		t.Fatal("unavailable acknowledgment shell snapshot must not claim downstream continuation proven")
	}
}

func TestShellSnapshotProofOfLifeFollowThroughShowsStrongerButIncompleteClaudeContinuity(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "proof-of-life shell snapshot",
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
	if _, err := coord.RecordHandoffFollowThrough(context.Background(), RecordHandoffFollowThroughRequest{
		TaskID:  string(taskID),
		Kind:    handoff.FollowThroughProofOfLifeObserved,
		Summary: "later Claude proof of life observed",
	}); err != nil {
		t.Fatalf("record handoff follow-through: %v", err)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.HandoffContinuity == nil || snapshot.HandoffContinuity.State != HandoffContinuityStateFollowThroughProofOfLife {
		t.Fatalf("expected proof-of-life handoff continuity in shell snapshot, got %+v", snapshot.HandoffContinuity)
	}
	if !snapshot.HandoffContinuity.DownstreamContinuationProven {
		t.Fatal("proof-of-life shell snapshot should reflect downstream continuation proven")
	}
	if snapshot.FollowThrough == nil || snapshot.FollowThrough.Kind != handoff.FollowThroughProofOfLifeObserved {
		t.Fatalf("expected shell follow-through summary, got %+v", snapshot.FollowThrough)
	}
	if snapshot.Recovery == nil || snapshot.Recovery.RecoveryClass != RecoveryClassHandoffLaunchCompleted {
		t.Fatalf("expected launched-handoff recovery after proof-of-life evidence, got %+v", snapshot.Recovery)
	}
	if snapshot.Recovery.ReadyForNextRun {
		t.Fatal("proof-of-life shell snapshot must not imply fresh next-run readiness")
	}
}

func TestShellSnapshotStalledFollowThroughRequiresReview(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "stalled follow-through shell snapshot",
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
	if _, err := coord.RecordHandoffFollowThrough(context.Background(), RecordHandoffFollowThroughRequest{
		TaskID:  string(taskID),
		Kind:    handoff.FollowThroughStalledReviewRequired,
		Summary: "Claude follow-through appears stalled",
	}); err != nil {
		t.Fatalf("record stalled handoff follow-through: %v", err)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Recovery == nil || snapshot.Recovery.RecoveryClass != RecoveryClassHandoffFollowThroughReviewRequired {
		t.Fatalf("expected stalled follow-through review-required recovery, got %+v", snapshot.Recovery)
	}
	if snapshot.Recovery.RecommendedAction != RecoveryActionReviewHandoffFollowThrough {
		t.Fatalf("expected review-handoff-follow-through action, got %+v", snapshot.Recovery)
	}
	if snapshot.HandoffContinuity == nil || snapshot.HandoffContinuity.State != HandoffContinuityStateFollowThroughStalled {
		t.Fatalf("expected stalled follow-through handoff continuity, got %+v", snapshot.HandoffContinuity)
	}
}

func TestShellSnapshotBrokenContinuityExposesRepairIssues(t *testing.T) {
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
		CheckpointID:       common.CheckpointID("chk_shell_bad_brief"),
		TaskID:             taskID,
		CreatedAt:          time.Now().UTC().Add(5 * time.Second),
		Trigger:            checkpoint.TriggerManual,
		CapsuleVersion:     caps.Version,
		Phase:              caps.CurrentPhase,
		Anchor:             anchorFromCapsule(caps),
		IntentID:           caps.CurrentIntentID,
		BriefID:            common.BriefID("brf_missing_shell"),
		ContextPackID:      "",
		LastEventID:        lastEvent,
		PendingDecisionIDs: []common.DecisionID{},
		ResumeDescriptor:   "broken shell continuity test",
		IsResumable:        true,
	}
	if err := store.Checkpoints().Create(bad); err != nil {
		t.Fatalf("create bad checkpoint: %v", err)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Recovery == nil || snapshot.Recovery.RecoveryClass != RecoveryClassRepairRequired {
		t.Fatalf("expected repair-required recovery class, got %+v", snapshot.Recovery)
	}
	if len(snapshot.Recovery.Issues) == 0 {
		t.Fatal("expected continuity issues in shell snapshot")
	}
	if snapshot.LaunchControl == nil || snapshot.LaunchControl.State != LaunchControlStateNotApplicable {
		t.Fatalf("expected non-applicable launch control in broken continuity state, got %+v", snapshot.LaunchControl)
	}
}
