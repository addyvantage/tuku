package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tuku/internal/adapters/adapter_contract"
	"tuku/internal/domain/brief"
	"tuku/internal/domain/capsule"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/operatorstep"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/recoveryaction"
	rundomain "tuku/internal/domain/run"
	"tuku/internal/domain/shellsession"
	anchorgit "tuku/internal/git/anchor"
	"tuku/internal/response/canonical"
	"tuku/internal/storage"
	"tuku/internal/storage/sqlite"
)

func testActionAuthority(t *testing.T, actions []OperatorActionAuthority, action OperatorAction) OperatorActionAuthority {
	t.Helper()
	for _, candidate := range actions {
		if candidate.Action == action {
			return candidate
		}
	}
	t.Fatalf("missing action authority for %s", action)
	return OperatorActionAuthority{}
}

func requireOperatorDecision(t *testing.T, decision *OperatorDecisionSummary) *OperatorDecisionSummary {
	t.Helper()
	if decision == nil {
		t.Fatal("expected operator decision summary")
	}
	return decision
}

func requireExecutionPlan(t *testing.T, plan *OperatorExecutionPlan) *OperatorExecutionPlan {
	t.Helper()
	if plan == nil {
		t.Fatal("expected operator execution plan")
	}
	if plan.PrimaryStep == nil {
		t.Fatal("expected operator execution primary step")
	}
	return plan
}

func TestStartTaskCreatesCapsuleWithAnchorAndProof(t *testing.T) {
	store := newTestStore(t)
	provider := &staticAnchorProvider{snapshot: anchorgit.Snapshot{RepoRoot: "/tmp/repo", Branch: "main", HeadSHA: "abc123", WorkingTreeDirty: true, CapturedAt: time.Unix(1700000000, 0).UTC()}}
	coord := newTestCoordinator(t, store, provider, newFakeAdapterSuccess())

	res, err := coord.StartTask(context.Background(), "Build milestone four", "/tmp/repo")
	if err != nil {
		t.Fatalf("start task: %v", err)
	}

	caps, err := store.Capsules().Get(res.TaskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	if caps.BranchName != "main" || caps.HeadSHA != "abc123" || !caps.WorkingTreeDirty {
		t.Fatalf("expected anchor persisted in capsule: %+v", caps)
	}
}

func TestMessageCreatesIntentAndBriefAndProof(t *testing.T) {
	store := newTestStore(t)
	provider := &staticAnchorProvider{snapshot: anchorgit.Snapshot{RepoRoot: "/tmp/repo", Branch: "main", HeadSHA: "head-1", WorkingTreeDirty: false, CapturedAt: time.Unix(1700001000, 0).UTC()}}
	coord := newTestCoordinator(t, store, provider, newFakeAdapterSuccess())

	start, err := coord.StartTask(context.Background(), "Implement parser", "/tmp/repo")
	if err != nil {
		t.Fatalf("start task: %v", err)
	}

	msgRes, err := coord.MessageTask(context.Background(), string(start.TaskID), "continue and prepare implementation")
	if err != nil {
		t.Fatalf("message task: %v", err)
	}
	if msgRes.BriefID == "" || msgRes.BriefHash == "" {
		t.Fatal("expected brief id and hash")
	}

	events, err := store.Proofs().ListByTask(start.TaskID, 30)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	if !hasEvent(events, proof.EventBriefCreated) {
		t.Fatal("expected brief created event")
	}
}

func TestMessageTaskPromptTriageSharpensVagueBugRequest(t *testing.T) {
	repoRoot := t.TempDir()
	files := map[string]string{
		"web/src/components/ProfileCard.tsx": "export function ProfileCard() {\n  return <section className=\"profile-card\">UI bug target</section>\n}\n",
		"web/src/pages/Dashboard.tsx":        "export default function Dashboard() {\n  return <div className=\"dashboard-screen\">dashboard ui layout</div>\n}\n",
		"internal/server/router.go":          "package server\n\nfunc RegisterRoutes() {}\n",
	}
	for rel, content := range files {
		abs := filepath.Join(repoRoot, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", abs, err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", abs, err)
		}
	}

	store := newTestStore(t)
	provider := &staticAnchorProvider{snapshot: anchorgit.Snapshot{RepoRoot: repoRoot, Branch: "main", HeadSHA: "head-ui", WorkingTreeDirty: false, CapturedAt: time.Unix(1700001100, 0).UTC()}}
	coord := newTestCoordinator(t, store, provider, newFakeAdapterSuccess())

	start, err := coord.StartTask(context.Background(), "Fix frontend defect", repoRoot)
	if err != nil {
		t.Fatalf("start task: %v", err)
	}
	if _, err := coord.MessageTask(context.Background(), string(start.TaskID), "fix the UI bug"); err != nil {
		t.Fatalf("message task: %v", err)
	}

	briefID := mustCurrentBriefID(t, store, start.TaskID)
	gotBrief, err := store.Briefs().Get(briefID)
	if err != nil {
		t.Fatalf("get brief: %v", err)
	}
	if !gotBrief.PromptTriage.Applied {
		t.Fatalf("expected prompt triage to apply, got %+v", gotBrief.PromptTriage)
	}
	if gotBrief.PromptTriage.FilesScanned < 2 {
		t.Fatalf("expected files scanned metric, got %+v", gotBrief.PromptTriage)
	}
	if len(gotBrief.PromptTriage.CandidateFiles) == 0 {
		t.Fatalf("expected ranked candidate files, got %+v", gotBrief.PromptTriage)
	}
	if !containsLine(gotBrief.PromptTriage.CandidateFiles, "web/src/components/ProfileCard.tsx") && !containsLine(gotBrief.PromptTriage.CandidateFiles, "web/src/pages/Dashboard.tsx") {
		t.Fatalf("expected frontend candidate file, got %+v", gotBrief.PromptTriage.CandidateFiles)
	}
	if !strings.Contains(gotBrief.ScopeSummary, "repo-local triage") {
		t.Fatalf("expected sharpened scope summary, got %q", gotBrief.ScopeSummary)
	}
	if containsLine(gotBrief.AmbiguityFlags, "scope_not_explicit") {
		t.Fatalf("expected scope ambiguity to be cleared, got %+v", gotBrief.AmbiguityFlags)
	}
	if gotBrief.PromptTriage.SearchSpaceTokenEstimate <= 0 || gotBrief.PromptTriage.SelectedContextTokenEstimate <= 0 {
		t.Fatalf("expected token estimates, got %+v", gotBrief.PromptTriage)
	}
	if gotBrief.TaskMemoryID == "" {
		t.Fatalf("expected task memory id on brief, got %+v", gotBrief)
	}
	if !gotBrief.MemoryCompression.Applied || gotBrief.MemoryCompression.FullHistoryTokenEstimate <= 0 || gotBrief.MemoryCompression.ResumePromptTokenEstimate <= 0 {
		t.Fatalf("expected brief memory compression metrics, got %+v", gotBrief.MemoryCompression)
	}
	if gotBrief.BenchmarkID == "" {
		t.Fatalf("expected benchmark id on brief, got %+v", gotBrief)
	}
	if gotBrief.PromptIR.NormalizedTaskType == "" || len(gotBrief.PromptIR.RankedTargets) == 0 || strings.TrimSpace(gotBrief.PromptIR.Confidence.Level) == "" {
		t.Fatalf("expected prompt ir packet on brief, got %+v", gotBrief.PromptIR)
	}

	taskMemory, err := store.TaskMemories().Get(gotBrief.TaskMemoryID)
	if err != nil {
		t.Fatalf("get task memory: %v", err)
	}
	if taskMemory.BriefID != gotBrief.BriefID || taskMemory.TaskID != start.TaskID {
		t.Fatalf("unexpected task memory linkage: %+v", taskMemory)
	}
	if taskMemory.Summary == "" || len(taskMemory.CandidateFiles) == 0 || taskMemory.ResumePromptTokenEstimate <= 0 {
		t.Fatalf("expected populated task memory snapshot, got %+v", taskMemory)
	}
	if !containsLine(taskMemory.CandidateFiles, "web/src/components/ProfileCard.tsx") && !containsLine(taskMemory.CandidateFiles, "web/src/pages/Dashboard.tsx") {
		t.Fatalf("expected task memory candidate file from triage, got %+v", taskMemory.CandidateFiles)
	}
	benchmarkRecord, err := store.Benchmarks().Get(gotBrief.BenchmarkID)
	if err != nil {
		t.Fatalf("get benchmark: %v", err)
	}
	if benchmarkRecord.BriefID != gotBrief.BriefID || benchmarkRecord.DispatchPromptTokenEstimate <= 0 || benchmarkRecord.FilesScanned <= 0 {
		t.Fatalf("expected populated benchmark record, got %+v", benchmarkRecord)
	}
	if benchmarkRecord.RankedTargetCount < len(gotBrief.PromptIR.RankedTargets) {
		t.Fatalf("expected ranked target count to cover prompt ir, got %+v", benchmarkRecord)
	}

	readBrief, err := coord.ReadGeneratedBrief(context.Background(), ReadGeneratedBriefRequest{TaskID: string(start.TaskID)})
	if err != nil {
		t.Fatalf("read generated brief: %v", err)
	}
	if readBrief.CompiledBrief == nil || readBrief.CompiledBrief.PromptTriage == nil || !readBrief.CompiledBrief.PromptTriage.Applied {
		t.Fatalf("expected compiled brief prompt triage projection, got %+v", readBrief.CompiledBrief)
	}
	if readBrief.CompiledBrief.MemoryCompression == nil || !readBrief.CompiledBrief.MemoryCompression.Applied || readBrief.CompiledBrief.MemoryCompression.ResumePromptTokenEstimate <= 0 {
		t.Fatalf("expected compiled brief memory compression projection, got %+v", readBrief.CompiledBrief)
	}
	if readBrief.CompiledBrief.PromptIR == nil || len(readBrief.CompiledBrief.PromptIR.RankedTargets) == 0 {
		t.Fatalf("expected compiled brief prompt ir projection, got %+v", readBrief.CompiledBrief)
	}

	benchmarkView, err := coord.ReadBenchmark(context.Background(), ReadBenchmarkRequest{TaskID: string(start.TaskID)})
	if err != nil {
		t.Fatalf("read benchmark: %v", err)
	}
	if benchmarkView.Benchmark == nil || benchmarkView.Benchmark.BenchmarkID != gotBrief.BenchmarkID {
		t.Fatalf("expected benchmark read projection, got %+v", benchmarkView)
	}
	if benchmarkView.CompiledBrief == nil || benchmarkView.CompiledBrief.PromptIR == nil {
		t.Fatalf("expected compiled brief in benchmark view, got %+v", benchmarkView)
	}

	events, err := store.Proofs().ListByTask(start.TaskID, 40)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	if !hasEvent(events, proof.EventTaskMemoryUpdated) {
		t.Fatalf("expected task memory updated proof event, got %+v", events)
	}
}

func TestMessageTaskBlockedWhileAcceptedClaudeHandoffIsActiveBranch(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.CreateCheckpoint(context.Background(), string(taskID)); err != nil {
		t.Fatalf("seed resumable checkpoint: %v", err)
	}

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "active handoff mutation gate",
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
	capsBefore, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule before blocked message: %v", err)
	}
	convBefore, err := store.Conversations().ListRecent(capsBefore.ConversationID, 100)
	if err != nil {
		t.Fatalf("list conversation before blocked message: %v", err)
	}

	_, err = coord.MessageTask(context.Background(), string(taskID), "change the execution brief locally")
	if err == nil || !strings.Contains(err.Error(), "active continuity branch") {
		t.Fatalf("expected active-handoff mutation gate error, got %v", err)
	}

	capsAfter, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule after blocked message: %v", err)
	}
	if capsAfter.CurrentBriefID != capsBefore.CurrentBriefID {
		t.Fatalf("blocked message must not replace current brief: before=%s after=%s", capsBefore.CurrentBriefID, capsAfter.CurrentBriefID)
	}
	convAfter, err := store.Conversations().ListRecent(capsAfter.ConversationID, 100)
	if err != nil {
		t.Fatalf("list conversation after blocked message: %v", err)
	}
	if len(convAfter) != len(convBefore) {
		t.Fatalf("blocked message must not append conversation entries: before=%d after=%d", len(convBefore), len(convAfter))
	}
}

func TestStatusTaskDefaultTaskReportsLocalActiveBranchOwner(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.ActiveBranchClass != ActiveBranchClassLocal {
		t.Fatalf("expected local active branch class, got %s", status.ActiveBranchClass)
	}
	if status.ActiveBranchRef != string(taskID) {
		t.Fatalf("expected local branch ref %s, got %s", taskID, status.ActiveBranchRef)
	}
	if status.ActiveBranchAnchorKind != ActiveBranchAnchorKindBrief {
		t.Fatalf("expected local branch to be anchored by current brief, got %s", status.ActiveBranchAnchorKind)
	}
	if status.ActiveBranchAnchorRef == "" {
		t.Fatal("expected local branch anchor ref")
	}
	if status.LocalResumeAuthorityState != LocalResumeAuthorityNotApplicable {
		t.Fatalf("expected no interrupted-lineage resume authority by default, got %s", status.LocalResumeAuthorityState)
	}
	if status.LocalResumeMode != LocalResumeModeStartFreshNextRun {
		t.Fatalf("expected default local mode to be fresh next run, got %s", status.LocalResumeMode)
	}
	decision := requireOperatorDecision(t, status.OperatorDecision)
	if decision.Headline != "Local fresh run ready" || decision.RequiredNextAction != OperatorActionStartLocalRun {
		t.Fatalf("unexpected default operator decision summary: %+v", decision)
	}
	if !strings.Contains(strings.ToLower(decision.Guidance), "start the next bounded local run") {
		t.Fatalf("expected fresh-run guidance, got %+v", decision)
	}
	plan := requireExecutionPlan(t, status.OperatorExecutionPlan)
	if plan.PrimaryStep.Action != OperatorActionStartLocalRun || plan.PrimaryStep.Status != OperatorActionAuthorityRequiredNext {
		t.Fatalf("unexpected default operator execution plan: %+v", plan)
	}
	if plan.PrimaryStep.CommandHint != "tuku run --task "+string(taskID)+" --action start" {
		t.Fatalf("expected fresh-run command hint, got %+v", plan.PrimaryStep)
	}
}

func TestStatusTaskAcceptedClaudeHandoffReportsClaudeActiveBranchOwner(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "accepted handoff should own continuity",
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

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.ActiveBranchClass != ActiveBranchClassHandoffClaude {
		t.Fatalf("expected Claude handoff active branch class, got %s", status.ActiveBranchClass)
	}
	if status.ActiveBranchRef != createOut.HandoffID {
		t.Fatalf("expected active branch ref %s, got %s", createOut.HandoffID, status.ActiveBranchRef)
	}
	if status.ActiveBranchAnchorKind != ActiveBranchAnchorKindHandoff {
		t.Fatalf("expected handoff anchor kind, got %s", status.ActiveBranchAnchorKind)
	}
	if status.LocalResumeAuthorityState != LocalResumeAuthorityBlocked {
		t.Fatalf("expected accepted handoff to block local resume authority, got %s", status.LocalResumeAuthorityState)
	}
	if status.RequiredNextOperatorAction != OperatorActionLaunchAcceptedHandoff {
		t.Fatalf("expected accepted handoff launch to be required next, got %s", status.RequiredNextOperatorAction)
	}
	messageAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionLocalMessageMutation)
	if messageAuthority.State != OperatorActionAuthorityBlocked || !strings.Contains(messageAuthority.Reason, "active continuity branch") {
		t.Fatalf("expected local message mutation block under Claude ownership, got %+v", messageAuthority)
	}
	checkpointAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionCreateCheckpoint)
	if checkpointAuthority.State != OperatorActionAuthorityBlocked || !strings.Contains(checkpointAuthority.Reason, "active continuity branch") {
		t.Fatalf("expected checkpoint creation block under Claude ownership, got %+v", checkpointAuthority)
	}
	launchAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionLaunchAcceptedHandoff)
	if launchAuthority.State != OperatorActionAuthorityRequiredNext {
		t.Fatalf("expected launch-accepted-handoff to be required next, got %+v", launchAuthority)
	}
	decision := requireOperatorDecision(t, status.OperatorDecision)
	if decision.Headline != "Accepted Claude handoff launch ready" || decision.RequiredNextAction != OperatorActionLaunchAcceptedHandoff {
		t.Fatalf("unexpected accepted-handoff operator decision summary: %+v", decision)
	}
	if !strings.Contains(strings.ToLower(decision.Guidance), "launch the accepted claude handoff") {
		t.Fatalf("expected launch-handoff guidance, got %+v", decision)
	}
	plan := requireExecutionPlan(t, status.OperatorExecutionPlan)
	if plan.PrimaryStep.Action != OperatorActionLaunchAcceptedHandoff || plan.PrimaryStep.Status != OperatorActionAuthorityRequiredNext || !plan.MandatoryBeforeProgress {
		t.Fatalf("unexpected accepted-handoff execution plan: %+v", plan)
	}
	if plan.PrimaryStep.CommandHint != "tuku handoff-launch --task "+string(taskID)+" --handoff "+createOut.HandoffID {
		t.Fatalf("expected truthful accepted-handoff launch command, got %+v", plan.PrimaryStep)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	inspectPlan := requireExecutionPlan(t, inspectOut.OperatorExecutionPlan)
	if inspectPlan.PrimaryStep.CommandHint != plan.PrimaryStep.CommandHint {
		t.Fatalf("expected inspect execution plan to use same canonical launch hint, status=%q inspect=%q", plan.PrimaryStep.CommandHint, inspectPlan.PrimaryStep.CommandHint)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.OperatorExecutionPlan == nil || snapshot.OperatorExecutionPlan.PrimaryStep == nil {
		t.Fatalf("expected shell operator execution plan")
	}
	if snapshot.OperatorExecutionPlan.PrimaryStep.CommandHint != plan.PrimaryStep.CommandHint {
		t.Fatalf("expected shell execution plan to use same canonical launch hint, status=%q shell=%q", plan.PrimaryStep.CommandHint, snapshot.OperatorExecutionPlan.PrimaryStep.CommandHint)
	}
}

func TestStatusTaskLaunchedClaudeHandoffReportsClaudeActiveBranchOwner(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "launched handoff should own continuity",
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
	if status.ActiveBranchClass != ActiveBranchClassHandoffClaude {
		t.Fatalf("expected launched Claude handoff to own continuity, got %s", status.ActiveBranchClass)
	}
	if status.ActiveBranchRef != createOut.HandoffID {
		t.Fatalf("expected launched active branch ref %s, got %s", createOut.HandoffID, status.ActiveBranchRef)
	}
	if status.LocalResumeAuthorityState != LocalResumeAuthorityBlocked {
		t.Fatalf("expected launched Claude handoff to block local resume authority, got %s", status.LocalResumeAuthorityState)
	}
}

func TestFinalizedLocalRunDoesNotReportStaleReconciliation(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{
		TaskID: string(taskID),
		Action: "complete",
		RunID:  startOut.RunID,
	}); err != nil {
		t.Fatalf("complete noop run: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.LocalRunFinalizationState != LocalRunFinalizationFinalized {
		t.Fatalf("expected finalized local run state, got %s", status.LocalRunFinalizationState)
	}
	if status.RecoveryClass == RecoveryClassStaleRunReconciliationRequired {
		t.Fatalf("finalized run must not report stale reconciliation: %+v", status)
	}
}

func TestStartTaskRollsBackOnProofAppendFailure(t *testing.T) {
	base := newTestStore(t)
	injected := &faultInjectedStore{base: base, failProofAppend: true}
	coord, err := NewCoordinator(Dependencies{
		Store:          injected,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    canonical.NewSimpleSynthesizer(),
		AnchorProvider: defaultAnchor(),
		ShellSessions:  NewMemoryShellSessionRegistry(),
		IDGenerator: func(prefix string) string {
			return prefix + "_fixed"
		},
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}

	if _, err := coord.StartTask(context.Background(), "tx rollback start", "/tmp/repo"); err == nil {
		t.Fatal("expected start task failure")
	}

	if _, err := base.Capsules().Get(common.TaskID("tsk_fixed")); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected no persisted capsule after rollback, got err=%v", err)
	}
	events, err := base.Proofs().ListByTask(common.TaskID("tsk_fixed"), 20)
	if err != nil {
		t.Fatalf("list proofs after rollback: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected no proof events for rolled-back start, got %d", len(events))
	}
}

func TestMessageTaskRollsBackOnSynthesisFailure(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	start := setupTaskWithBrief(t, coord)

	capsBefore, err := store.Capsules().Get(start)
	if err != nil {
		t.Fatalf("get capsule before: %v", err)
	}
	convBefore, err := store.Conversations().ListRecent(capsBefore.ConversationID, 100)
	if err != nil {
		t.Fatalf("list conversations before: %v", err)
	}
	eventsBefore, err := store.Proofs().ListByTask(start, 200)
	if err != nil {
		t.Fatalf("list proofs before: %v", err)
	}

	failCoord, err := NewCoordinator(Dependencies{
		Store:          store,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    &failingSynthesizer{err: errors.New("synth failure")},
		AnchorProvider: defaultAnchor(),
		ShellSessions:  NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new failing coordinator: %v", err)
	}

	if _, err := failCoord.MessageTask(context.Background(), string(start), "this write should rollback"); err == nil {
		t.Fatal("expected message task failure")
	}

	capsAfter, err := store.Capsules().Get(start)
	if err != nil {
		t.Fatalf("get capsule after: %v", err)
	}
	if capsAfter.CurrentIntentID != capsBefore.CurrentIntentID {
		t.Fatalf("capsule intent pointer changed despite rollback: before=%s after=%s", capsBefore.CurrentIntentID, capsAfter.CurrentIntentID)
	}
	if capsAfter.CurrentBriefID != capsBefore.CurrentBriefID {
		t.Fatalf("capsule brief pointer changed despite rollback: before=%s after=%s", capsBefore.CurrentBriefID, capsAfter.CurrentBriefID)
	}

	convAfter, err := store.Conversations().ListRecent(capsAfter.ConversationID, 100)
	if err != nil {
		t.Fatalf("list conversations after: %v", err)
	}
	if len(convAfter) != len(convBefore) {
		t.Fatalf("conversation count changed despite rollback: before=%d after=%d", len(convBefore), len(convAfter))
	}
	eventsAfter, err := store.Proofs().ListByTask(start, 200)
	if err != nil {
		t.Fatalf("list proofs after: %v", err)
	}
	if len(eventsAfter) != len(eventsBefore) {
		t.Fatalf("proof event count changed despite rollback: before=%d after=%d", len(eventsBefore), len(eventsAfter))
	}
}

func TestRunRealSuccessCompletesAndRecordsEvidence(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:                string(taskID),
		SessionID:             "shs_run_success",
		WorkerPreference:      "codex",
		ResolvedWorker:        "codex",
		WorkerSessionID:       "wks_run_success",
		WorkerSessionIDSource: shellsession.WorkerSessionIDSourceAuthoritative,
		AttachCapability:      shellsession.AttachCapabilityAttachable,
		HostMode:              "codex-pty",
		HostState:             "live",
		StartedAt:             time.Unix(1710000000, 0).UTC(),
		Active:                true,
	}); err != nil {
		t.Fatalf("report shell session before run: %v", err)
	}

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start real: %v", err)
	}
	if res.RunID == "" {
		t.Fatal("expected run id")
	}
	if res.RunStatus != rundomain.StatusCompleted {
		t.Fatalf("expected completed status, got %s", res.RunStatus)
	}
	if res.Phase != phase.PhaseValidating {
		t.Fatalf("expected %s phase, got %s", phase.PhaseValidating, res.Phase)
	}
	if !strings.Contains(strings.ToLower(res.CanonicalResponse), "completed") {
		t.Fatalf("expected canonical completion response, got %q", res.CanonicalResponse)
	}

	runRec, err := store.Runs().Get(res.RunID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if runRec.Status != rundomain.StatusCompleted {
		t.Fatalf("expected run status completed, got %s", runRec.Status)
	}
	if runRec.WorkerRunID == "" {
		t.Fatal("expected durable worker run id")
	}
	if runRec.ShellSessionID != "shs_run_success" {
		t.Fatalf("expected durable shell session linkage, got %q", runRec.ShellSessionID)
	}
	if runRec.Command == "" {
		t.Fatal("expected durable command evidence")
	}
	if runRec.ExitCode == nil || *runRec.ExitCode != 0 {
		t.Fatalf("expected durable exit code 0, got %+v", runRec.ExitCode)
	}
	if runRec.Stdout == "" {
		t.Fatal("expected durable stdout evidence")
	}
	if len(runRec.ChangedFiles) == 0 {
		t.Fatal("expected durable changed file evidence")
	}
	if len(runRec.ValidationSignals) == 0 {
		t.Fatal("expected durable validation signal evidence")
	}
	briefRec, err := store.Briefs().Get(runRec.BriefID)
	if err != nil {
		t.Fatalf("get brief for benchmark: %v", err)
	}
	benchmarkRecord, err := store.Benchmarks().Get(briefRec.BenchmarkID)
	if err != nil {
		t.Fatalf("get benchmark after run: %v", err)
	}
	if benchmarkRecord.RunID != res.RunID || len(benchmarkRecord.ChangedFiles) == 0 {
		t.Fatalf("expected benchmark run linkage and changed files, got %+v", benchmarkRecord)
	}
	if benchmarkRecord.CandidateRecallAt3 < 0 || benchmarkRecord.CandidateRecallAt3 > 1 {
		t.Fatalf("expected candidate recall in [0,1], got %+v", benchmarkRecord)
	}

	events, err := store.Proofs().ListByTask(taskID, 80)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	if !hasEvent(events, proof.EventWorkerRunStarted) {
		t.Fatal("expected worker run started")
	}
	if !hasEvent(events, proof.EventWorkerOutputCaptured) {
		t.Fatal("expected worker output captured")
	}
	if !hasEvent(events, proof.EventFileChangeDetected) {
		t.Fatal("expected file change detected event")
	}
	if !hasEvent(events, proof.EventWorkerRunCompleted) {
		t.Fatal("expected worker run completed")
	}
	for _, e := range events {
		switch e.Type {
		case proof.EventWorkerRunStarted, proof.EventWorkerOutputCaptured, proof.EventFileChangeDetected, proof.EventWorkerRunCompleted, proof.EventWorkerRunFailed, proof.EventRunInterrupted:
			if e.RunID == nil {
				t.Fatalf("expected run_id for run-related event %s", e.Type)
			}
		}
	}
}

func TestRunRealFailureMarksBlocked(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start real failure path: %v", err)
	}
	if res.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected failed status, got %s", res.RunStatus)
	}
	if res.Phase != phase.PhaseBlocked {
		t.Fatalf("expected %s phase, got %s", phase.PhaseBlocked, res.Phase)
	}

	events, err := store.Proofs().ListByTask(taskID, 80)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	if !hasEvent(events, proof.EventWorkerRunFailed) {
		t.Fatal("expected worker run failed")
	}
}

func TestStatusInspectAndShellSnapshotSurfaceDurableExecutionAndSessionEvidence(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:                string(taskID),
		SessionID:             "shs_surface",
		WorkerPreference:      "codex",
		ResolvedWorker:        "codex",
		WorkerSessionID:       "wks_surface",
		WorkerSessionIDSource: shellsession.WorkerSessionIDSourceAuthoritative,
		AttachCapability:      shellsession.AttachCapabilityAttachable,
		HostMode:              "codex-pty",
		HostState:             "live",
		StartedAt:             time.Unix(1710000100, 0).UTC(),
		Active:                true,
	}); err != nil {
		t.Fatalf("report shell session: %v", err)
	}
	runRes, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start real: %v", err)
	}

	statusOut, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if statusOut.LatestRunID != runRes.RunID || statusOut.LatestRunWorkerRunID == "" {
		t.Fatalf("expected status to surface run evidence, got %+v", statusOut)
	}
	if statusOut.LatestRunExitCode == nil || *statusOut.LatestRunExitCode != 0 {
		t.Fatalf("expected status exit code 0, got %+v", statusOut.LatestRunExitCode)
	}
	if statusOut.LatestRunShellSessionID != "shs_surface" || statusOut.LatestShellSessionID == "" {
		t.Fatalf("expected status shell linkage, got latest_run_shell=%q latest_shell=%q", statusOut.LatestRunShellSessionID, statusOut.LatestShellSessionID)
	}
	if statusOut.LatestShellEventKind == "" {
		t.Fatal("expected status to surface latest shell event kind")
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.Run == nil || inspectOut.Run.WorkerRunID == "" || inspectOut.Run.Stdout == "" {
		t.Fatalf("expected inspect to surface durable run evidence, got %+v", inspectOut.Run)
	}
	if len(inspectOut.ShellSessions) == 0 {
		t.Fatal("expected inspect to surface durable shell sessions")
	}
	if len(inspectOut.RecentShellEvents) == 0 {
		t.Fatal("expected inspect to surface durable shell events")
	}

	shellOut, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot task: %v", err)
	}
	if shellOut.Run == nil || shellOut.Run.WorkerRunID == "" || shellOut.Run.Stdout == "" {
		t.Fatalf("expected shell snapshot run evidence, got %+v", shellOut.Run)
	}
	if len(shellOut.ShellSessions) == 0 {
		t.Fatal("expected shell snapshot to surface shell sessions")
	}
	if len(shellOut.RecentShellEvents) == 0 {
		t.Fatal("expected shell snapshot to surface shell session events")
	}
}

func TestRunRealAdapterErrorMarksBlocked(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterError(errors.New("codex missing")))
	taskID := setupTaskWithBrief(t, coord)

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start real adapter error should map to canonical failure, got: %v", err)
	}
	if res.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected failed status, got %s", res.RunStatus)
	}
	latestRun, err := store.Runs().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest run by task: %v", err)
	}
	if latestRun.Status != rundomain.StatusFailed {
		t.Fatalf("expected durable failed run status, got %s", latestRun.Status)
	}
	if _, err := store.Runs().LatestRunningByTask(taskID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected no stale RUNNING run after adapter error, got err=%v", err)
	}
}

func TestRunRealPassesBoundedExecutionEnvelopeToAdapter(t *testing.T) {
	store := newTestStore(t)
	adapter := newFakeAdapterSuccess()
	coord := newTestCoordinator(t, store, defaultAnchor(), adapter)
	taskID := setupTaskWithBrief(t, coord)

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start real: %v", err)
	}
	if !adapter.called {
		t.Fatal("expected adapter execute to be called")
	}
	if adapter.lastReq.TaskID != taskID {
		t.Fatalf("expected adapter task id %s, got %s", taskID, adapter.lastReq.TaskID)
	}
	if adapter.lastReq.RunID != res.RunID {
		t.Fatalf("expected adapter run id %s, got %s", res.RunID, adapter.lastReq.RunID)
	}
	if adapter.lastReq.Brief.BriefID == "" {
		t.Fatal("expected adapter brief id to be populated")
	}
	if adapter.lastReq.Brief.NormalizedAction == "" {
		t.Fatal("expected adapter normalized action to be populated")
	}
	if adapter.lastReq.RepoAnchor.RepoRoot == "" {
		t.Fatal("expected adapter repo root to be populated")
	}
	if adapter.lastReq.ContextSummary == "" {
		t.Fatal("expected adapter context summary to be populated")
	}
	if adapter.lastReq.PolicyProfileID == "" {
		t.Fatal("expected adapter policy profile to be populated")
	}
}

func TestRunDurablyRunningBeforeWorkerExecute(t *testing.T) {
	store := newTestStore(t)
	adapter := newFakeAdapterSuccess()
	var observedRunStatus rundomain.Status
	var observedCapsulePhase phase.Phase
	adapter.onExecute = func(req adapter_contract.ExecutionRequest) {
		runRec, err := store.Runs().Get(req.RunID)
		if err != nil {
			t.Fatalf("expected run to exist before execute: %v", err)
		}
		observedRunStatus = runRec.Status

		caps, err := store.Capsules().Get(req.TaskID)
		if err != nil {
			t.Fatalf("expected capsule to exist before execute: %v", err)
		}
		observedCapsulePhase = caps.CurrentPhase
	}
	coord := newTestCoordinator(t, store, defaultAnchor(), adapter)
	taskID := setupTaskWithBrief(t, coord)

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start real: %v", err)
	}
	if observedRunStatus != rundomain.StatusRunning {
		t.Fatalf("expected RUNNING before execute, got %s", observedRunStatus)
	}
	if observedCapsulePhase != phase.PhaseExecuting {
		t.Fatalf("expected EXECUTING before execute, got %s", observedCapsulePhase)
	}
	if res.RunStatus != rundomain.StatusCompleted {
		t.Fatalf("expected completed final status, got %s", res.RunStatus)
	}
}

func TestStatusShowsActiveExecutionWhileWorkerIsStillRunning(t *testing.T) {
	store := newTestStore(t)
	adapter := newFakeAdapterSuccess()
	var coord *Coordinator
	var observedStatus StatusTaskResult
	adapter.onExecute = func(req adapter_contract.ExecutionRequest) {
		status, err := coord.StatusTask(context.Background(), string(req.TaskID))
		if err != nil {
			t.Fatalf("status during execute: %v", err)
		}
		observedStatus = status
	}
	coord = newTestCoordinator(t, store, defaultAnchor(), adapter)
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("run start real: %v", err)
	}
	if observedStatus.RequiredNextOperatorAction != OperatorActionWaitForLocalRun {
		t.Fatalf("expected wait-for-run required-next action, got %s", observedStatus.RequiredNextOperatorAction)
	}
	if observedStatus.LocalRunFinalizationState != LocalRunFinalizationActiveExecution {
		t.Fatalf("expected active execution local run state, got %s", observedStatus.LocalRunFinalizationState)
	}
	if observedStatus.RecoveryClass != RecoveryClassRunInProgress {
		t.Fatalf("expected run-in-progress recovery class, got %s", observedStatus.RecoveryClass)
	}
}

func TestCanonicalResponseNotRawWorkerText(t *testing.T) {
	store := newTestStore(t)
	adapter := &fakeWorkerAdapter{kind: adapter_contract.WorkerCodex, result: adapter_contract.ExecutionResult{
		ExitCode:  0,
		Stdout:    "RAW_WORKER_OUTPUT_TOKEN_12345",
		Stderr:    "",
		Summary:   "completed summary",
		StartedAt: time.Now().UTC(),
		EndedAt:   time.Now().UTC(),
	}}
	coord := newTestCoordinator(t, store, defaultAnchor(), adapter)
	taskID := setupTaskWithBrief(t, coord)

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start: %v", err)
	}
	if res.CanonicalResponse == adapter.result.Stdout {
		t.Fatal("canonical response must not equal raw worker stdout")
	}
	if strings.Contains(res.CanonicalResponse, "RAW_WORKER_OUTPUT_TOKEN_12345") {
		t.Fatal("canonical response leaked raw worker token")
	}
}

func TestRunNoBriefBlocked(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())

	start, err := coord.StartTask(context.Background(), "No brief case", "/tmp/repo")
	if err != nil {
		t.Fatalf("start task: %v", err)
	}

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(start.TaskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run should return canonical blocked response, got error: %v", err)
	}
	if res.RunID != "" {
		t.Fatalf("expected empty run id when blocked, got %s", res.RunID)
	}
	if !strings.Contains(strings.ToLower(res.CanonicalResponse), "cannot start") {
		t.Fatalf("unexpected canonical response: %s", res.CanonicalResponse)
	}
}

func TestRunNoopModeManualLifecycleStillWorks(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startRes, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("noop start: %v", err)
	}
	if startRes.RunStatus != rundomain.StatusRunning {
		t.Fatalf("expected running noop run, got %s", startRes.RunStatus)
	}
	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule after noop start: %v", err)
	}
	if caps.CurrentPhase != phase.PhaseExecuting {
		t.Fatalf("running invariant broken: expected phase %s, got %s", phase.PhaseExecuting, caps.CurrentPhase)
	}
	completeRes, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "complete", RunID: startRes.RunID})
	if err != nil {
		t.Fatalf("noop complete: %v", err)
	}
	if completeRes.RunStatus != rundomain.StatusCompleted {
		t.Fatalf("expected completed noop run, got %s", completeRes.RunStatus)
	}
}

func TestRunInterruptSetsPausedInvariant(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startRes, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("noop start: %v", err)
	}
	interruptRes, err := coord.RunTask(context.Background(), RunTaskRequest{
		TaskID:             string(taskID),
		Action:             "interrupt",
		RunID:              startRes.RunID,
		InterruptionReason: "test interruption",
	})
	if err != nil {
		t.Fatalf("interrupt run: %v", err)
	}
	if interruptRes.RunStatus != rundomain.StatusInterrupted {
		t.Fatalf("expected interrupted status, got %s", interruptRes.RunStatus)
	}
	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule after interrupt: %v", err)
	}
	if caps.CurrentPhase != phase.PhasePaused {
		t.Fatalf("interrupt invariant broken: expected phase %s, got %s", phase.PhasePaused, caps.CurrentPhase)
	}
}

func TestRunStartBlockedWhenRecoveryClassDecisionRequired(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("seed failed run: %v", err)
	}
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindFailedRunReviewed,
	}); err != nil {
		t.Fatalf("record failed-run review: %v", err)
	}

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start should return canonical blocked response, got error: %v", err)
	}
	if res.RunID != "" || res.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected blocked run start result, got %+v", res)
	}
	if !strings.Contains(strings.ToLower(res.CanonicalResponse), "decision") {
		t.Fatalf("expected decision-required canonical response, got %q", res.CanonicalResponse)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.RecoveryClass != RecoveryClassDecisionRequired {
		t.Fatalf("expected decision-required status recovery class, got %s", status.RecoveryClass)
	}
}

func TestRunStartBlockedWhenRecoveryClassFailedRunReviewRequired(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("seed failed run: %v", err)
	}

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start should return canonical blocked response, got error: %v", err)
	}
	if res.RunID != "" || res.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected blocked run start result, got %+v", res)
	}
	if !strings.Contains(strings.ToLower(res.CanonicalResponse), "failed run") {
		t.Fatalf("expected failed-run review canonical response, got %q", res.CanonicalResponse)
	}
}

func TestRunStartBlockedWhenRecoveryClassRebriefRequired(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("seed failed run: %v", err)
	}
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindFailedRunReviewed,
	}); err != nil {
		t.Fatalf("record failed-run review: %v", err)
	}
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionRegenerateBrief,
	}); err != nil {
		t.Fatalf("record regenerate-brief decision: %v", err)
	}

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start should return canonical blocked response, got error: %v", err)
	}
	if res.RunID != "" || res.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected blocked run start result, got %+v", res)
	}
	if !strings.Contains(strings.ToLower(res.CanonicalResponse), "brief") {
		t.Fatalf("expected rebrief canonical response, got %q", res.CanonicalResponse)
	}
}

func TestRunStartBlockedWhenRecoveryClassBlockedDrift(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.CreateCheckpoint(context.Background(), string(taskID)); err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}

	driftCoord := newTestCoordinator(t, store, &staticAnchorProvider{
		snapshot: anchorgit.Snapshot{
			RepoRoot:         "/tmp/repo",
			Branch:           "feature/drift",
			HeadSHA:          "head-drift",
			WorkingTreeDirty: false,
			CapturedAt:       time.Unix(1700006000, 0).UTC(),
		},
	}, newFakeAdapterSuccess())

	res, err := driftCoord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start should return canonical blocked response, got error: %v", err)
	}
	if res.RunID != "" || res.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected blocked drift run start result, got %+v", res)
	}
	if !strings.Contains(strings.ToLower(res.CanonicalResponse), "drift") {
		t.Fatalf("expected drift canonical response, got %q", res.CanonicalResponse)
	}
}

func TestRunStartBlockedWhenAcceptedClaudeHandoffIsActiveRecoveryPath(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "handoff branch active for launch",
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

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start should return canonical blocked response, got error: %v", err)
	}
	if res.RunID != "" || res.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected blocked handoff-path run start result, got %+v", res)
	}
	if !strings.Contains(strings.ToLower(res.CanonicalResponse), "handoff") {
		t.Fatalf("expected handoff canonical response, got %q", res.CanonicalResponse)
	}
}

func TestRunStartStrictContinuePathRequiresExecutedContinueRecovery(t *testing.T) {
	store := newTestStore(t)
	failCoord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, failCoord)

	if _, err := failCoord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("seed failed run: %v", err)
	}
	if _, err := failCoord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindFailedRunReviewed,
	}); err != nil {
		t.Fatalf("record failed-run review: %v", err)
	}
	if _, err := failCoord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionContinue,
	}); err != nil {
		t.Fatalf("record continue decision: %v", err)
	}

	statusBefore, err := failCoord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status before continue execution: %v", err)
	}
	if statusBefore.RecoveryClass != RecoveryClassContinueExecutionRequired {
		t.Fatalf("expected continue-execution-required before execute, got %s", statusBefore.RecoveryClass)
	}
	if statusBefore.ReadyForNextRun {
		t.Fatal("status must not claim ready-for-next-run before continue execution")
	}

	blocked, err := failCoord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start before continue execution should return canonical blocked response, got error: %v", err)
	}
	if blocked.RunID != "" || blocked.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected blocked run start before continue execution, got %+v", blocked)
	}
	if !strings.Contains(strings.ToLower(blocked.CanonicalResponse), "continue") {
		t.Fatalf("expected continue-finalization canonical response, got %q", blocked.CanonicalResponse)
	}

	successCoord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	if _, err := successCoord.ExecuteContinueRecovery(context.Background(), ExecuteContinueRecoveryRequest{TaskID: string(taskID)}); err != nil {
		t.Fatalf("execute continue recovery: %v", err)
	}

	statusAfter, err := successCoord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status after continue execution: %v", err)
	}
	if statusAfter.RecoveryClass != RecoveryClassReadyNextRun || !statusAfter.ReadyForNextRun {
		t.Fatalf("expected ready-next-run after continue execution, got %+v", statusAfter)
	}
	if statusAfter.LatestRecoveryAction == nil || statusAfter.LatestRecoveryAction.Kind != recoveryaction.KindContinueExecuted {
		t.Fatalf("expected latest continue-executed action after continue execution, got %+v", statusAfter.LatestRecoveryAction)
	}

	inspectAfter, err := successCoord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect after continue execution: %v", err)
	}
	if inspectAfter.Recovery == nil || inspectAfter.Recovery.RecoveryClass != RecoveryClassReadyNextRun {
		t.Fatalf("expected inspect ready-next-run after continue execution, got %+v", inspectAfter.Recovery)
	}

	allowed, err := successCoord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start after continue execution: %v", err)
	}
	if allowed.RunID == "" {
		t.Fatalf("expected run id after continue execution, got %+v", allowed)
	}
}

func TestRunStartNoopAlsoUsesRecoveryGate(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("seed failed run: %v", err)
	}

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("noop run start should return canonical blocked response, got error: %v", err)
	}
	if res.RunID != "" || res.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected noop run start to be blocked by recovery gate, got %+v", res)
	}
	if !strings.Contains(strings.ToLower(res.CanonicalResponse), "failed run") {
		t.Fatalf("expected noop recovery-gate canonical response, got %q", res.CanonicalResponse)
	}
}

func TestStatusAndInspectExposeLatestRun(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	runRes, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.LatestRunID != runRes.RunID {
		t.Fatalf("status missing latest run id: %+v", status)
	}

	ins, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if ins.Run == nil || ins.Run.RunID != runRes.RunID {
		t.Fatalf("inspect missing latest run: %+v", ins)
	}
}

func TestBriefBuilderDeterministicHash(t *testing.T) {
	builder := NewBriefBuilderV1(func(_ string) string { return "brf_fixed" }, func() time.Time {
		return time.Unix(1700003000, 0).UTC()
	})

	input := brief.BuildInput{
		TaskID:           "tsk_1",
		IntentID:         "int_1",
		CapsuleVersion:   2,
		Goal:             "Implement feature X",
		NormalizedAction: "continue from current state",
		Constraints:      []string{"do not execute workers"},
		ScopeHints:       []string{"internal/orchestrator"},
		ScopeOutHints:    []string{"web"},
		DoneCriteria:     []string{"brief is generated"},
		Verbosity:        brief.VerbosityStandard,
		PolicyProfileID:  "default-safe-v1",
	}

	b1, err := builder.Build(input)
	if err != nil {
		t.Fatalf("build brief 1: %v", err)
	}
	b2, err := builder.Build(input)
	if err != nil {
		t.Fatalf("build brief 2: %v", err)
	}
	if b1.BriefHash != b2.BriefHash {
		t.Fatalf("expected deterministic hash, got %s vs %s", b1.BriefHash, b2.BriefHash)
	}
}

func TestRunTaskKeepsDurableRunningStateWhenFinalizationFails(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	capsBefore, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule before: %v", err)
	}
	convBefore, err := store.Conversations().ListRecent(capsBefore.ConversationID, 200)
	if err != nil {
		t.Fatalf("list conversations before: %v", err)
	}
	eventsBefore, err := store.Proofs().ListByTask(taskID, 200)
	if err != nil {
		t.Fatalf("list proofs before: %v", err)
	}

	failCoord, err := NewCoordinator(Dependencies{
		Store:          store,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    &failingSynthesizer{err: errors.New("run synth failure")},
		AnchorProvider: defaultAnchor(),
		ShellSessions:  NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new failing coordinator: %v", err)
	}

	if _, err := failCoord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err == nil {
		t.Fatal("expected run task failure")
	}

	runRec, err := store.Runs().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("expected persisted running run after stage-1 commit, got err=%v", err)
	}
	if runRec.Status != rundomain.StatusRunning {
		t.Fatalf("expected run to remain RUNNING when finalization fails, got %s", runRec.Status)
	}

	capsAfter, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule after: %v", err)
	}
	if capsAfter.CurrentPhase != phase.PhaseExecuting {
		t.Fatalf("expected capsule to remain EXECUTING when finalization fails, got %s", capsAfter.CurrentPhase)
	}
	convAfter, err := store.Conversations().ListRecent(capsAfter.ConversationID, 200)
	if err != nil {
		t.Fatalf("list conversations after: %v", err)
	}
	if len(convAfter) != len(convBefore) {
		t.Fatalf("conversation count changed despite rollback: before=%d after=%d", len(convBefore), len(convAfter))
	}

	eventsAfter, err := store.Proofs().ListByTask(taskID, 200)
	if err != nil {
		t.Fatalf("list proofs after: %v", err)
	}
	if len(eventsAfter) != len(eventsBefore)+4 {
		t.Fatalf("expected stage-1 run start and policy events to persist: before=%d after=%d", len(eventsBefore), len(eventsAfter))
	}
	if !hasEvent(eventsAfter, proof.EventWorkerRunStarted) {
		t.Fatal("expected durable worker run started event from stage-1 commit")
	}
	if !hasEvent(eventsAfter, proof.EventPolicyDecisionRequested) || !hasEvent(eventsAfter, proof.EventPolicyDecisionResolved) {
		t.Fatal("expected durable policy decision proof from stage-1 commit")
	}
	if hasEvent(eventsAfter, proof.EventWorkerOutputCaptured) {
		t.Fatal("worker output captured should rollback when finalization transaction fails")
	}
	if hasEvent(eventsAfter, proof.EventWorkerRunCompleted) || hasEvent(eventsAfter, proof.EventWorkerRunFailed) {
		t.Fatal("terminal run events should not persist when finalization transaction fails")
	}

	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint after failed finalization: %v", err)
	}
	if latestCheckpoint.Trigger != checkpoint.TriggerBeforeExecution {
		t.Fatalf("expected before-execution checkpoint from prepare stage, got %s", latestCheckpoint.Trigger)
	}
	if latestCheckpoint.RunID != runRec.RunID {
		t.Fatalf("expected checkpoint run id %s, got %s", runRec.RunID, latestCheckpoint.RunID)
	}
}

func TestRunRealSuccessCreatesAfterExecutionCheckpoint(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	runRes, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start real: %v", err)
	}

	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint: %v", err)
	}
	if latestCheckpoint.Trigger != checkpoint.TriggerAfterExecution {
		t.Fatalf("expected after-execution checkpoint, got %s", latestCheckpoint.Trigger)
	}
	if latestCheckpoint.RunID != runRes.RunID {
		t.Fatalf("expected checkpoint run id %s, got %s", runRes.RunID, latestCheckpoint.RunID)
	}
	if !latestCheckpoint.IsResumable {
		t.Fatal("expected checkpoint to be resumable")
	}
}

func TestCreateCheckpointManual(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	out, err := coord.CreateCheckpoint(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("create checkpoint: %v", err)
	}
	if out.Trigger != checkpoint.TriggerManual {
		t.Fatalf("expected manual trigger, got %s", out.Trigger)
	}
	if out.CheckpointID == "" {
		t.Fatal("expected checkpoint id")
	}

	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint: %v", err)
	}
	if latestCheckpoint.CheckpointID != out.CheckpointID {
		t.Fatalf("expected latest checkpoint %s, got %s", out.CheckpointID, latestCheckpoint.CheckpointID)
	}
	if !hasEventMust(t, store, taskID, proof.EventCheckpointCreated) {
		t.Fatal("expected checkpoint created proof event")
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status after checkpoint: %v", err)
	}
	if status.LatestCheckpointID != out.CheckpointID {
		t.Fatalf("status missing latest checkpoint id: expected %s got %s", out.CheckpointID, status.LatestCheckpointID)
	}
	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect after checkpoint: %v", err)
	}
	if inspectOut.Checkpoint == nil || inspectOut.Checkpoint.CheckpointID != out.CheckpointID {
		t.Fatalf("inspect missing checkpoint: %+v", inspectOut.Checkpoint)
	}
}

func TestCreateCheckpointBlockedWhileLaunchedClaudeHandoffIsActiveBranch(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "checkpoint mutation gate",
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
	checkpointBefore, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint before blocked manual checkpoint: %v", err)
	}

	_, err = coord.CreateCheckpoint(context.Background(), string(taskID))
	if err == nil || !strings.Contains(err.Error(), "local checkpoint") {
		t.Fatalf("expected launched-handoff checkpoint gate error, got %v", err)
	}
	checkpointAfter, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint after blocked manual checkpoint: %v", err)
	}
	if checkpointAfter.CheckpointID != checkpointBefore.CheckpointID {
		t.Fatalf("blocked checkpoint must not create a newer checkpoint: before=%s after=%s", checkpointBefore.CheckpointID, checkpointAfter.CheckpointID)
	}
}

func TestMessageTaskBlockedWhileLaunchedClaudeHandoffRemainsActiveBranch(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "launched handoff message gate",
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

	_, err = coord.MessageTask(context.Background(), string(taskID), "rewrite the local brief after launching Claude")
	if err == nil || !strings.Contains(err.Error(), "launched Claude handoff") {
		t.Fatalf("expected launched-handoff message gate error, got %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status after blocked message: %v", err)
	}
	if status.RecoveryClass != RecoveryClassHandoffLaunchCompleted {
		t.Fatalf("blocked message must leave launched-handoff recovery intact, got %s", status.RecoveryClass)
	}
	if status.ReadyForNextRun {
		t.Fatal("blocked message must not make launched handoff fresh-run ready")
	}
}

func TestMessageTaskAllowedAfterExplicitClaudeHandoffResolution(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "resolve and return to local control",
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

	out, err := coord.MessageTask(context.Background(), string(taskID), "continue local implementation after resolving Claude branch")
	if err != nil {
		t.Fatalf("message task after resolution: %v", err)
	}
	if out.BriefID == "" {
		t.Fatalf("expected message task to persist a new local brief after resolution, got %+v", out)
	}
}

func TestCreateCheckpointAllowedAfterExplicitClaudeHandoffResolution(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "resolve launched Claude branch",
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
		Kind:    handoff.ResolutionClosedUnproven,
		Summary: "close launched Claude branch without completion proof",
	}); err != nil {
		t.Fatalf("record handoff resolution: %v", err)
	}

	out, err := coord.CreateCheckpoint(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("create checkpoint after resolution: %v", err)
	}
	if out.CheckpointID == "" {
		t.Fatalf("expected checkpoint after resolution, got %+v", out)
	}
}

func TestRunStartUsesLocalTruthAfterExplicitClaudeHandoffResolution(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "resolve handoff before local run",
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
		Summary: "return local control before starting a fresh run",
	}); err != nil {
		t.Fatalf("record handoff resolution: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.HandoffContinuityState != HandoffContinuityStateNotApplicable {
		t.Fatalf("expected no active handoff continuity after resolution, got %s", status.HandoffContinuityState)
	}
	if status.ActiveBranchClass != ActiveBranchClassLocal {
		t.Fatalf("expected local branch owner after resolution, got %s", status.ActiveBranchClass)
	}
	if !status.ReadyForNextRun || status.RecoveryClass != RecoveryClassReadyNextRun {
		t.Fatalf("expected local next-run truth after resolution, got %+v", status)
	}
	if status.LocalResumeAuthorityState != LocalResumeAuthorityNotApplicable || status.LocalResumeMode != LocalResumeModeStartFreshNextRun {
		t.Fatalf("expected fresh-next-run local resume summary after resolution, got %+v", status)
	}

	runOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run after resolution: %v", err)
	}
	if runOut.RunStatus != rundomain.StatusRunning {
		t.Fatalf("expected noop run to start after resolution, got %+v", runOut)
	}
}

func TestGatingFollowsExplicitActiveBranchOwnerTruth(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "active branch gating truth",
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

	statusBefore, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status before: %v", err)
	}
	if statusBefore.ActiveBranchClass != ActiveBranchClassHandoffClaude {
		t.Fatalf("expected Claude handoff owner before resolution, got %s", statusBefore.ActiveBranchClass)
	}
	blocked, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("run start while handoff owns continuity: %v", err)
	}
	if blocked.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected run start to be blocked while Claude branch owns continuity, got %+v", blocked)
	}

	if _, err := coord.RecordHandoffResolution(context.Background(), RecordHandoffResolutionRequest{
		TaskID:  string(taskID),
		Kind:    handoff.ResolutionSupersededByLocal,
		Summary: "return local control after review",
	}); err != nil {
		t.Fatalf("resolve handoff: %v", err)
	}

	statusAfter, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status after: %v", err)
	}
	if statusAfter.ActiveBranchClass != ActiveBranchClassLocal {
		t.Fatalf("expected local owner after resolution, got %s", statusAfter.ActiveBranchClass)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"}); err != nil {
		t.Fatalf("expected run start to use local truth after resolution, got %v", err)
	}
}

func TestContinueReconcilesStaleRunningRun(t *testing.T) {
	store := newTestStore(t)
	taskID := setupTaskWithBrief(t, newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess()))

	failCoord, err := NewCoordinator(Dependencies{
		Store:          store,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    &failingSynthesizer{err: errors.New("run synth failure")},
		AnchorProvider: defaultAnchor(),
		ShellSessions:  NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new failing coordinator: %v", err)
	}
	if _, err := failCoord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err == nil {
		t.Fatal("expected staged finalization failure to leave stale running state")
	}
	beforeCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint before continue reconciliation: %v", err)
	}

	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if out.Outcome != ContinueOutcomeStaleReconciled {
		t.Fatalf("expected stale reconciliation outcome, got %s", out.Outcome)
	}
	if out.CheckpointID == "" {
		t.Fatal("expected reconciliation checkpoint id")
	}

	runRec, err := store.Runs().Get(out.RunID)
	if err != nil {
		t.Fatalf("get reconciled run: %v", err)
	}
	if runRec.Status != rundomain.StatusInterrupted {
		t.Fatalf("expected run interrupted after reconciliation, got %s", runRec.Status)
	}
	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	if caps.CurrentPhase != phase.PhasePaused {
		t.Fatalf("expected paused phase after stale reconciliation, got %s", caps.CurrentPhase)
	}
	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint after reconciliation: %v", err)
	}
	if latestCheckpoint.CheckpointID == beforeCheckpoint.CheckpointID {
		t.Fatalf("expected new checkpoint for reconciliation, got same id %s", latestCheckpoint.CheckpointID)
	}
	if latestCheckpoint.Trigger != checkpoint.TriggerInterruption {
		t.Fatalf("expected interruption checkpoint after stale reconciliation, got %s", latestCheckpoint.Trigger)
	}
	if out.RecoveryClass != RecoveryClassInterruptedRunRecoverable {
		t.Fatalf("expected interrupted recoverable class after stale reconciliation, got %s", out.RecoveryClass)
	}
	if out.RecommendedAction != RecoveryActionResumeInterrupted {
		t.Fatalf("expected resume interrupted action after stale reconciliation, got %s", out.RecommendedAction)
	}
	if out.ReadyForNextRun {
		t.Fatal("stale-run reconciliation must not claim fresh next-run readiness")
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "resume the interrupted execution path") {
		t.Fatalf("expected stale-run reconciliation canonical response to describe interrupted resume, got %q", out.CanonicalResponse)
	}
}

func TestStatusTaskExposesStaleRunFinalizationDistinctFromLocalResumeAuthority(t *testing.T) {
	store := newTestStore(t)
	taskID := setupTaskWithBrief(t, newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess()))

	failCoord, err := NewCoordinator(Dependencies{
		Store:          store,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    &failingSynthesizer{err: errors.New("leave stale run")},
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
	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.LocalRunFinalizationState != LocalRunFinalizationStaleReconciliationNeeded {
		t.Fatalf("expected stale run finalization state, got %s", status.LocalRunFinalizationState)
	}
	if status.LocalResumeAuthorityState != LocalResumeAuthorityNotApplicable {
		t.Fatalf("stale run should not claim interrupted resume authority, got %s", status.LocalResumeAuthorityState)
	}
	if status.RecoveryClass != RecoveryClassStaleRunReconciliationRequired {
		t.Fatalf("expected stale-run reconciliation recovery, got %s", status.RecoveryClass)
	}
	if status.RequiredNextOperatorAction != OperatorActionReconcileStaleRun {
		t.Fatalf("expected reconcile-stale-run to be required next, got %s", status.RequiredNextOperatorAction)
	}
	startAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionStartLocalRun)
	if startAuthority.State != OperatorActionAuthorityBlocked || !strings.Contains(strings.ToLower(startAuthority.Reason), "stale run") {
		t.Fatalf("expected stale run to block fresh start explicitly, got %+v", startAuthority)
	}
	reconcileAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionReconcileStaleRun)
	if reconcileAuthority.State != OperatorActionAuthorityRequiredNext {
		t.Fatalf("expected stale reconciliation authority to be required next, got %+v", reconcileAuthority)
	}
	decision := requireOperatorDecision(t, status.OperatorDecision)
	if decision.Headline != "Stale local run reconciliation required" || decision.RequiredNextAction != OperatorActionReconcileStaleRun {
		t.Fatalf("unexpected stale-reconciliation operator decision summary: %+v", decision)
	}
	if !strings.Contains(strings.ToLower(decision.Guidance), "reconcile stale run state") {
		t.Fatalf("expected stale-reconciliation guidance, got %+v", decision)
	}
	plan := requireExecutionPlan(t, status.OperatorExecutionPlan)
	if plan.PrimaryStep.Action != OperatorActionReconcileStaleRun || plan.PrimaryStep.Status != OperatorActionAuthorityRequiredNext || !plan.MandatoryBeforeProgress {
		t.Fatalf("unexpected stale-reconciliation execution plan: %+v", plan)
	}
	if plan.PrimaryStep.CommandHint != "tuku continue --task "+string(taskID) {
		t.Fatalf("expected stale-reconciliation command hint, got %+v", plan.PrimaryStep)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.LocalRunFinalization == nil || inspectOut.LocalRunFinalization.State != LocalRunFinalizationStaleReconciliationNeeded {
		t.Fatalf("expected inspect stale run finalization summary, got %+v", inspectOut.LocalRunFinalization)
	}
	if inspectOut.LocalResumeAuthority == nil || inspectOut.LocalResumeAuthority.State != LocalResumeAuthorityNotApplicable {
		t.Fatalf("expected inspect local resume authority to remain not-applicable for stale run, got %+v", inspectOut.LocalResumeAuthority)
	}
	if inspectOut.ActionAuthority == nil || inspectOut.ActionAuthority.RequiredNextAction != OperatorActionReconcileStaleRun {
		t.Fatalf("expected inspect action authority to expose stale reconciliation distinctly, got %+v", inspectOut.ActionAuthority)
	}
	inspectPlan := requireExecutionPlan(t, inspectOut.OperatorExecutionPlan)
	if inspectPlan.PrimaryStep.Action != OperatorActionReconcileStaleRun {
		t.Fatalf("expected inspect execution plan to expose stale reconciliation distinctly, got %+v", inspectPlan)
	}
}

func TestStatusTaskFailedRunFinalizationRemainsDistinctFromStaleReconciliation(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	runOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start: %v", err)
	}
	if runOut.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected failed run status, got %s", runOut.RunStatus)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.LocalRunFinalizationState != LocalRunFinalizationFailedReviewRequired {
		t.Fatalf("expected failed-review local run finalization, got %s", status.LocalRunFinalizationState)
	}
	if status.RecoveryClass != RecoveryClassFailedRunReviewRequired {
		t.Fatalf("expected failed-run review recovery, got %s", status.RecoveryClass)
	}
	if status.LocalResumeAuthorityState != LocalResumeAuthorityNotApplicable {
		t.Fatalf("failed run should not claim interrupted resume authority, got %s", status.LocalResumeAuthorityState)
	}
}

func TestAcceptedClaudeOwnershipStillBlocksLocalActionabilityWhenStaleRunExists(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "active Claude branch should outrank stale local run actionability",
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

	runID := common.RunID("run_stale_branch")
	now := time.Now().UTC()
	if err := store.Runs().Create(rundomain.ExecutionRun{
		RunID:            runID,
		TaskID:           taskID,
		BriefID:          mustCurrentBriefID(t, store, taskID),
		WorkerKind:       rundomain.WorkerKindCodex,
		Status:           rundomain.StatusRunning,
		LastKnownSummary: "synthetic stale run for branch ownership test",
		StartedAt:        now,
		CreatedFromPhase: phase.PhaseExecuting,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("create stale run: %v", err)
	}
	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	caps.CurrentPhase = phase.PhaseExecuting
	caps.Version++
	caps.UpdatedAt = now
	if err := store.Capsules().Update(caps); err != nil {
		t.Fatalf("update capsule: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.ActiveBranchClass != ActiveBranchClassHandoffClaude {
		t.Fatalf("expected accepted Claude handoff to remain active owner, got %s", status.ActiveBranchClass)
	}
	if status.LocalRunFinalizationState != LocalRunFinalizationStaleReconciliationNeeded {
		t.Fatalf("expected stale local run finalization truth, got %s", status.LocalRunFinalizationState)
	}
	if status.LocalResumeAuthorityState != LocalResumeAuthorityBlocked {
		t.Fatalf("expected active Claude ownership to block local actionability, got %s", status.LocalResumeAuthorityState)
	}
}

func TestResolutionLetsLocalOwnershipExposeStaleRunTruthAgain(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.CreateCheckpoint(context.Background(), string(taskID)); err != nil {
		t.Fatalf("seed resumable checkpoint: %v", err)
	}

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "resolution should return stale local truth",
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

	runID := common.RunID("run_stale_after_resolution")
	now := time.Now().UTC()
	if err := store.Runs().Create(rundomain.ExecutionRun{
		RunID:            runID,
		TaskID:           taskID,
		BriefID:          mustCurrentBriefID(t, store, taskID),
		WorkerKind:       rundomain.WorkerKindCodex,
		Status:           rundomain.StatusRunning,
		LastKnownSummary: "synthetic stale run before resolution",
		StartedAt:        now,
		CreatedFromPhase: phase.PhaseExecuting,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("create stale run: %v", err)
	}
	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	caps.CurrentPhase = phase.PhaseExecuting
	caps.Version++
	caps.UpdatedAt = now
	if err := store.Capsules().Update(caps); err != nil {
		t.Fatalf("update capsule: %v", err)
	}

	if _, err := coord.RecordHandoffResolution(context.Background(), RecordHandoffResolutionRequest{
		TaskID:  string(taskID),
		Kind:    handoff.ResolutionSupersededByLocal,
		Summary: "return local control despite stale run",
	}); err != nil {
		t.Fatalf("resolve handoff: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.ActiveBranchClass != ActiveBranchClassLocal {
		t.Fatalf("expected local branch owner after resolution, got %s", status.ActiveBranchClass)
	}
	if status.LocalRunFinalizationState != LocalRunFinalizationStaleReconciliationNeeded {
		t.Fatalf("expected stale run truth to reappear after resolution, got %s", status.LocalRunFinalizationState)
	}
	if status.LocalResumeAuthorityState != LocalResumeAuthorityNotApplicable {
		t.Fatalf("expected stale reconciliation to remain distinct from resume after resolution, got %s", status.LocalResumeAuthorityState)
	}
}

func TestContinueBlockedOnMajorDrift(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.CreateCheckpoint(context.Background(), string(taskID)); err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}

	driftAnchor := &staticAnchorProvider{
		snapshot: anchorgit.Snapshot{
			RepoRoot:         "/tmp/repo",
			Branch:           "feature/drift",
			HeadSHA:          "head-x",
			WorkingTreeDirty: false,
			CapturedAt:       time.Unix(1700005000, 0).UTC(),
		},
	}
	driftCoord := newTestCoordinator(t, store, driftAnchor, newFakeAdapterSuccess())
	out, err := driftCoord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue with drift: %v", err)
	}
	if out.Outcome != ContinueOutcomeBlockedDrift {
		t.Fatalf("expected blocked drift outcome, got %s", out.Outcome)
	}
	if out.DriftClass != checkpoint.DriftMajor {
		t.Fatalf("expected major drift class, got %s", out.DriftClass)
	}
	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	if caps.CurrentPhase != phase.PhaseAwaitingDecision {
		t.Fatalf("expected awaiting decision phase, got %s", caps.CurrentPhase)
	}
}

func TestContinueSafeFromCheckpoint(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	seed, err := coord.CreateCheckpoint(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}
	eventsBefore, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("events before safe continue: %v", err)
	}

	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue safe: %v", err)
	}
	if out.Outcome != ContinueOutcomeSafe {
		t.Fatalf("expected safe outcome, got %s", out.Outcome)
	}
	if out.CheckpointID == "" {
		t.Fatal("expected continuation checkpoint")
	}
	if out.CheckpointID != seed.CheckpointID {
		t.Fatalf("expected safe continue to reuse checkpoint %s, got %s", seed.CheckpointID, out.CheckpointID)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "fresh next bounded run is already ready") {
		t.Fatalf("expected canonical fresh-next-run response, got %q", out.CanonicalResponse)
	}
	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint after safe continue: %v", err)
	}
	if latestCheckpoint.CheckpointID != seed.CheckpointID {
		t.Fatalf("expected no new checkpoint to be created on safe continue")
	}
	eventsAfter, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("events after safe continue: %v", err)
	}
	if len(eventsAfter) <= len(eventsBefore) {
		t.Fatalf("expected durable proof records for no-op safe continue")
	}
	if !hasEvent(eventsAfter, proof.EventContinueAssessed) {
		t.Fatalf("expected continue-assessed proof event for no-op safe continue")
	}
}

func TestContinueInterruptedRunReportsRecoveryReady(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startRes, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{
		TaskID:             string(taskID),
		Action:             "interrupt",
		RunID:              startRes.RunID,
		InterruptionReason: "phase 2 interrupted recovery test",
	}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}

	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if out.RecoveryClass != RecoveryClassInterruptedRunRecoverable {
		t.Fatalf("expected interrupted recovery class, got %s", out.RecoveryClass)
	}
	if out.RecommendedAction != RecoveryActionResumeInterrupted {
		t.Fatalf("expected resume interrupted action, got %s", out.RecommendedAction)
	}
	if out.ReadyForNextRun {
		t.Fatal("interrupted recovery must not claim fresh next-run readiness")
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "resume the interrupted execution path") {
		t.Fatalf("expected interrupted recovery canonical response to describe resume, got %q", out.CanonicalResponse)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.RecoveryClass != RecoveryClassInterruptedRunRecoverable {
		t.Fatalf("expected interrupted recovery class in status, got %s", status.RecoveryClass)
	}
	if status.RequiredNextOperatorAction != OperatorActionResumeInterruptedLineage {
		t.Fatalf("expected interrupted resume to be required next, got %s", status.RequiredNextOperatorAction)
	}
	if status.ReadyForNextRun {
		t.Fatal("status must not claim fresh next-run readiness for interrupted recovery")
	}
	if status.LocalResumeAuthorityState != LocalResumeAuthorityAllowed {
		t.Fatalf("expected interrupted local resume authority to be allowed, got %s", status.LocalResumeAuthorityState)
	}
	if status.LocalResumeMode != LocalResumeModeResumeInterruptedLineage {
		t.Fatalf("expected interrupted local resume mode, got %s", status.LocalResumeMode)
	}
	if status.LocalResumeCheckpointID == "" {
		t.Fatal("expected interrupted local resume checkpoint id in status")
	}
	resumeAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionResumeInterruptedLineage)
	if resumeAuthority.State != OperatorActionAuthorityRequiredNext {
		t.Fatalf("expected interrupted resume authority to be required next, got %+v", resumeAuthority)
	}
	startAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionStartLocalRun)
	if startAuthority.State != OperatorActionAuthorityBlocked {
		t.Fatalf("expected fresh local run start to remain blocked during interrupted recovery, got %+v", startAuthority)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.Recovery == nil {
		t.Fatal("expected inspect recovery assessment")
	}
	if inspectOut.Recovery.RecoveryClass != RecoveryClassInterruptedRunRecoverable {
		t.Fatalf("expected interrupted recovery class in inspect, got %+v", inspectOut.Recovery)
	}
	if inspectOut.Recovery.ReadyForNextRun {
		t.Fatal("inspect must not claim fresh next-run readiness for interrupted recovery")
	}
	if inspectOut.LocalResumeAuthority == nil {
		t.Fatal("expected inspect local resume authority")
	}
	if inspectOut.LocalResumeAuthority.State != LocalResumeAuthorityAllowed || inspectOut.LocalResumeAuthority.Mode != LocalResumeModeResumeInterruptedLineage {
		t.Fatalf("unexpected inspect local resume authority: %+v", inspectOut.LocalResumeAuthority)
	}
	decision := requireOperatorDecision(t, status.OperatorDecision)
	if decision.Headline != "Interrupted local lineage recoverable" || decision.RequiredNextAction != OperatorActionResumeInterruptedLineage {
		t.Fatalf("unexpected interrupted operator decision summary: %+v", decision)
	}
	if !strings.Contains(strings.ToLower(decision.Guidance), "resume the interrupted local lineage") {
		t.Fatalf("expected interrupted-lineage guidance, got %+v", decision)
	}
	if !strings.Contains(strings.ToLower(decision.IntegrityNote), "checkpoint resumability") {
		t.Fatalf("expected checkpoint caution note for interrupted recovery, got %+v", decision)
	}
	if inspectDecision := requireOperatorDecision(t, inspectOut.OperatorDecision); inspectDecision.RequiredNextAction != OperatorActionResumeInterruptedLineage {
		t.Fatalf("expected inspect operator decision to match interrupted resume truth, got %+v", inspectDecision)
	}
	plan := requireExecutionPlan(t, status.OperatorExecutionPlan)
	if plan.PrimaryStep.Action != OperatorActionResumeInterruptedLineage || plan.PrimaryStep.Status != OperatorActionAuthorityRequiredNext || !plan.MandatoryBeforeProgress {
		t.Fatalf("unexpected interrupted execution plan: %+v", plan)
	}
	if plan.PrimaryStep.CommandHint != "tuku recovery resume-interrupted --task "+string(taskID) {
		t.Fatalf("expected interrupted-resume command hint, got %+v", plan.PrimaryStep)
	}
	inspectPlan := requireExecutionPlan(t, inspectOut.OperatorExecutionPlan)
	if inspectPlan.PrimaryStep.Action != OperatorActionResumeInterruptedLineage {
		t.Fatalf("expected inspect execution plan to preserve interrupted resume truth, got %+v", inspectPlan)
	}
}

func TestStatusTaskContinueExecutionRequiredDistinguishesLocalResumeAuthority(t *testing.T) {
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
		InterruptionReason: "continue recovery distinction test",
	}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}
	if _, err := coord.ExecuteInterruptedResume(context.Background(), ExecuteInterruptedResumeRequest{
		TaskID:  string(taskID),
		Summary: "operator resumed interrupted lineage",
	}); err != nil {
		t.Fatalf("execute interrupted resume: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.RecoveryClass != RecoveryClassContinueExecutionRequired {
		t.Fatalf("expected continue-execution-required recovery, got %s", status.RecoveryClass)
	}
	if status.LocalResumeAuthorityState != LocalResumeAuthorityNotApplicable {
		t.Fatalf("expected interrupted resume authority to be not applicable after interrupted-resume execution, got %s", status.LocalResumeAuthorityState)
	}
	if status.LocalResumeMode != LocalResumeModeFinalizeContinueRecovery {
		t.Fatalf("expected finalize-continue local mode, got %s", status.LocalResumeMode)
	}
	if status.RequiredNextOperatorAction != OperatorActionFinalizeContinueRecovery {
		t.Fatalf("expected finalize-continue to be required next, got %s", status.RequiredNextOperatorAction)
	}
	finalizeAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionFinalizeContinueRecovery)
	if finalizeAuthority.State != OperatorActionAuthorityRequiredNext {
		t.Fatalf("expected finalize-continue authority to be required next, got %+v", finalizeAuthority)
	}
	resumeAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionResumeInterruptedLineage)
	if resumeAuthority.State != OperatorActionAuthorityNotApplicable {
		t.Fatalf("expected interrupted resume to remain distinct after continue confirmation, got %+v", resumeAuthority)
	}
	decision := requireOperatorDecision(t, status.OperatorDecision)
	if decision.Headline != "Continue finalization required" || decision.RequiredNextAction != OperatorActionFinalizeContinueRecovery {
		t.Fatalf("unexpected continue-confirmation operator decision summary: %+v", decision)
	}
	if !strings.Contains(strings.ToLower(decision.Guidance), "finalize continue recovery") {
		t.Fatalf("expected finalize-continue guidance, got %+v", decision)
	}
	plan := requireExecutionPlan(t, status.OperatorExecutionPlan)
	if plan.PrimaryStep.Action != OperatorActionFinalizeContinueRecovery || plan.PrimaryStep.Status != OperatorActionAuthorityRequiredNext {
		t.Fatalf("unexpected continue-finalization execution plan: %+v", plan)
	}
	if plan.PrimaryStep.CommandHint != "tuku recovery continue --task "+string(taskID) {
		t.Fatalf("expected continue-recovery command hint, got %+v", plan.PrimaryStep)
	}
}

func TestBlockingClaudeBranchDoesNotLetRawCheckpointOverrideLocalResumeAuthority(t *testing.T) {
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
		InterruptionReason: "raw checkpoint authority test",
	}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "accepted handoff should block local resume authority",
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

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if !status.CheckpointResumable {
		t.Fatal("expected raw checkpoint resumability to remain visible")
	}
	if status.LocalResumeAuthorityState != LocalResumeAuthorityBlocked {
		t.Fatalf("expected blocked local resume authority under active Claude handoff, got %s", status.LocalResumeAuthorityState)
	}
	if status.LocalResumeMode != LocalResumeModeNone {
		t.Fatalf("expected no authorized local resume mode while handoff owns continuity, got %s", status.LocalResumeMode)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.LocalResumeAuthority == nil {
		t.Fatal("expected inspect local resume authority")
	}
	if inspectOut.LocalResumeAuthority.State != LocalResumeAuthorityBlocked {
		t.Fatalf("expected inspect local resume authority to be blocked, got %+v", inspectOut.LocalResumeAuthority)
	}
	startAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionStartLocalRun)
	if startAuthority.State != OperatorActionAuthorityBlocked || !strings.Contains(strings.ToLower(startAuthority.Reason), "claude handoff") {
		t.Fatalf("expected active Claude ownership to block fresh run authority, got %+v", startAuthority)
	}
}

func TestResolutionCanRestoreInterruptedLocalResumeAuthority(t *testing.T) {
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
		InterruptionReason: "resolution resume authority test",
	}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}
	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "resolve back to interrupted local lineage",
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
		Summary: "return local control to interrupted lineage",
	}); err != nil {
		t.Fatalf("resolve handoff: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.ActiveBranchClass != ActiveBranchClassLocal {
		t.Fatalf("expected local branch owner after resolution, got %s", status.ActiveBranchClass)
	}
	if status.LocalResumeAuthorityState != LocalResumeAuthorityAllowed || status.LocalResumeMode != LocalResumeModeResumeInterruptedLineage {
		t.Fatalf("expected interrupted local resume authority to return after resolution, got %+v", status)
	}
	if status.RequiredNextOperatorAction != OperatorActionResumeInterruptedLineage {
		t.Fatalf("expected interrupted resume to become required next after resolution, got %s", status.RequiredNextOperatorAction)
	}
	messageAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionLocalMessageMutation)
	if messageAuthority.State != OperatorActionAuthorityAllowed {
		t.Fatalf("expected local message mutation to return after resolution, got %+v", messageAuthority)
	}
	checkpointAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionCreateCheckpoint)
	if checkpointAuthority.State != OperatorActionAuthorityAllowed {
		t.Fatalf("expected checkpoint creation to return after resolution, got %+v", checkpointAuthority)
	}
	decision := requireOperatorDecision(t, status.OperatorDecision)
	if decision.ActiveOwnerClass != ActiveBranchClassLocal {
		t.Fatalf("expected local owner in post-resolution decision summary, got %+v", decision)
	}
	if !strings.Contains(strings.ToLower(decision.IntegrityNote), "historical claude branch resolution") {
		t.Fatalf("expected historical-resolution caution note without completion claim, got %+v", decision)
	}
	plan := requireExecutionPlan(t, status.OperatorExecutionPlan)
	if plan.PrimaryStep.Action != OperatorActionResumeInterruptedLineage || plan.PrimaryStep.Status != OperatorActionAuthorityRequiredNext {
		t.Fatalf("unexpected post-resolution execution plan: %+v", plan)
	}
	if plan.PrimaryStep.CommandHint != "tuku recovery resume-interrupted --task "+string(taskID) {
		t.Fatalf("expected post-resolution interrupted-resume command hint, got %+v", plan.PrimaryStep)
	}
}

func TestReadyNextRunActionAuthorityAllowsFreshRunWithoutResume(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.RequiredNextOperatorAction != OperatorActionStartLocalRun {
		t.Fatalf("expected start-local-run to be required next, got %s", status.RequiredNextOperatorAction)
	}
	startAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionStartLocalRun)
	if startAuthority.State != OperatorActionAuthorityRequiredNext {
		t.Fatalf("expected fresh local run to be required next, got %+v", startAuthority)
	}
	resumeAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionResumeInterruptedLineage)
	if resumeAuthority.State != OperatorActionAuthorityNotApplicable {
		t.Fatalf("expected interrupted resume to remain distinct from fresh-next-run readiness, got %+v", resumeAuthority)
	}
}

func TestLaunchedClaudeHandoffActionAuthorityAllowsResolutionWhileBlockingUnsafeMutation(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.CreateCheckpoint(context.Background(), string(taskID)); err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "launched handoff authority test",
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
	resolveAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionResolveActiveHandoff)
	if resolveAuthority.State != OperatorActionAuthorityAllowed {
		t.Fatalf("expected launched handoff to allow explicit resolution, got %+v", resolveAuthority)
	}
	messageAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionLocalMessageMutation)
	if messageAuthority.State != OperatorActionAuthorityBlocked {
		t.Fatalf("expected launched handoff to block local mutation, got %+v", messageAuthority)
	}
	decision := requireOperatorDecision(t, status.OperatorDecision)
	if decision.Headline != "Claude handoff branch active" || decision.ActiveOwnerClass != ActiveBranchClassHandoffClaude {
		t.Fatalf("unexpected launched handoff operator decision summary: %+v", decision)
	}
	if !strings.Contains(strings.ToLower(decision.Guidance), "monitor or explicitly resolve") {
		t.Fatalf("expected launched handoff guidance, got %+v", decision)
	}
	if !strings.Contains(strings.ToLower(decision.IntegrityNote), "downstream claude completion remains unproven") {
		t.Fatalf("expected launched-handoff caution note, got %+v", decision)
	}
	plan := requireExecutionPlan(t, status.OperatorExecutionPlan)
	if plan.PrimaryStep.Action != OperatorActionResolveActiveHandoff || plan.PrimaryStep.Status != OperatorActionAuthorityAllowed || !plan.MandatoryBeforeProgress {
		t.Fatalf("unexpected launched-handoff execution plan: %+v", plan)
	}
	if plan.PrimaryStep.CommandHint != "tuku handoff-resolve --task "+string(taskID)+" --handoff "+createOut.HandoffID+" --kind <abandoned|superseded-by-local|closed-unproven|reviewed-stale>" {
		t.Fatalf("expected truthful launched-handoff resolution command hint, got %+v", plan.PrimaryStep)
	}
}

func TestStalledFollowThroughDecisionSummaryRequiresReview(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "stalled follow-through decision summary",
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
	if _, err := coord.RecordHandoffFollowThrough(context.Background(), RecordHandoffFollowThroughRequest{
		TaskID:  string(taskID),
		Kind:    handoff.FollowThroughStalledReviewRequired,
		Summary: "Claude follow-through appears stalled",
	}); err != nil {
		t.Fatalf("record handoff follow-through: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	decision := requireOperatorDecision(t, status.OperatorDecision)
	if decision.Headline != "Claude follow-through review required" || decision.RequiredNextAction != OperatorActionReviewHandoffFollowUp {
		t.Fatalf("unexpected stalled-follow-through operator decision summary: %+v", decision)
	}
	if !strings.Contains(strings.ToLower(decision.Guidance), "review the stalled claude follow-through") {
		t.Fatalf("expected stalled-follow-through guidance, got %+v", decision)
	}
	plan := requireExecutionPlan(t, status.OperatorExecutionPlan)
	if plan.PrimaryStep.Action != OperatorActionReviewHandoffFollowUp || plan.PrimaryStep.Status != OperatorActionAuthorityRequiredNext || !plan.MandatoryBeforeProgress {
		t.Fatalf("unexpected stalled-follow-through execution plan: %+v", plan)
	}
	if plan.PrimaryStep.CommandHint != "tuku inspect --task "+string(taskID) {
		t.Fatalf("expected truthful stalled-follow-through review command hint, got %+v", plan.PrimaryStep)
	}
}

func TestRecordRecoveryActionInterruptedRunReviewedKeepsInterruptedPosture(t *testing.T) {
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

	out, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID:  string(taskID),
		Kind:    recoveryaction.KindInterruptedRunReviewed,
		Summary: "interrupted lineage reviewed",
		Notes:   []string{"preserve interrupted lineage"},
	})
	if err != nil {
		t.Fatalf("record interrupted-run review: %v", err)
	}
	if out.RecoveryClass != RecoveryClassInterruptedRunRecoverable {
		t.Fatalf("expected interrupted recoverable class after review, got %s", out.RecoveryClass)
	}
	if out.RecommendedAction != RecoveryActionResumeInterrupted {
		t.Fatalf("expected resume-interrupted action after review, got %s", out.RecommendedAction)
	}
	if out.ReadyForNextRun {
		t.Fatal("interrupted review must not make the task fresh-start ready")
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "interrupted") || strings.Contains(strings.ToLower(out.CanonicalResponse), "start a run") {
		t.Fatalf("expected interrupted-lineage canonical response, got %q", out.CanonicalResponse)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.RecoveryClass != RecoveryClassInterruptedRunRecoverable || status.ReadyForNextRun {
		t.Fatalf("unexpected status recovery after interrupted review: %+v", status)
	}
	if status.LatestRecoveryAction == nil || status.LatestRecoveryAction.Kind != recoveryaction.KindInterruptedRunReviewed {
		t.Fatalf("expected latest recovery action to be interrupted review, got %+v", status.LatestRecoveryAction)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.Recovery == nil || inspectOut.Recovery.RecoveryClass != RecoveryClassInterruptedRunRecoverable || inspectOut.Recovery.ReadyForNextRun {
		t.Fatalf("unexpected inspect recovery after interrupted review: %+v", inspectOut.Recovery)
	}
	if inspectOut.LatestRecoveryAction == nil || inspectOut.LatestRecoveryAction.Kind != recoveryaction.KindInterruptedRunReviewed {
		t.Fatalf("expected inspect latest recovery action to be interrupted review, got %+v", inspectOut.LatestRecoveryAction)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Recovery == nil || snapshot.Recovery.RecoveryClass != RecoveryClassInterruptedRunRecoverable || snapshot.Recovery.ReadyForNextRun {
		t.Fatalf("unexpected shell recovery after interrupted review: %+v", snapshot.Recovery)
	}
	if !strings.Contains(strings.ToLower(snapshot.Recovery.Reason), "reviewed") {
		t.Fatalf("expected reviewed interrupted-lineage reason in shell snapshot, got %q", snapshot.Recovery.Reason)
	}

	runStart, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start should return canonical blocked response, got error: %v", err)
	}
	if runStart.RunID != "" || runStart.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected interrupted-review run start to remain blocked, got %+v", runStart)
	}
	if !strings.Contains(strings.ToLower(runStart.CanonicalResponse), "interrupted") {
		t.Fatalf("expected interrupted recovery blocked response, got %q", runStart.CanonicalResponse)
	}

	events, err := store.Proofs().ListByTask(taskID, 100)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	if !hasEvent(events, proof.EventInterruptedRunReviewed) {
		t.Fatal("expected interrupted-run-reviewed proof event")
	}
	if hasEvent(events, proof.EventRecoveryActionRecorded) {
		t.Fatal("interrupted review should emit only the specific interrupted-review proof event")
	}
}

func TestRecordRecoveryActionInterruptedRunReviewedRejectsInvalidPostureAndReplaysIdempotently(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindInterruptedRunReviewed,
	}); err == nil || !strings.Contains(err.Error(), string(RecoveryClassInterruptedRunRecoverable)) {
		t.Fatalf("expected interrupted-review invalid-posture rejection, got %v", err)
	}

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

	first, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID:  string(taskID),
		Kind:    recoveryaction.KindInterruptedRunReviewed,
		Summary: "interrupted lineage reviewed",
	})
	if err != nil {
		t.Fatalf("first interrupted review: %v", err)
	}
	second, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID:  string(taskID),
		Kind:    recoveryaction.KindInterruptedRunReviewed,
		Summary: "interrupted lineage reviewed",
	})
	if err != nil {
		t.Fatalf("second interrupted review: %v", err)
	}
	if first.Action.ActionID != second.Action.ActionID {
		t.Fatalf("expected interrupted-review replay to reuse latest action, got %s then %s", first.Action.ActionID, second.Action.ActionID)
	}
}

func TestRecordRecoveryActionInterruptedRunReviewedReplayRejectedAfterPostureChanges(t *testing.T) {
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

	runRec, err := store.Runs().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest run: %v", err)
	}
	runRec.Status = rundomain.StatusCompleted
	now := time.Now().UTC()
	runRec.EndedAt = &now
	runRec.LastKnownSummary = "execution completed after out-of-band reconciliation"
	if err := store.Runs().Update(runRec); err != nil {
		t.Fatalf("update run: %v", err)
	}

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	caps.CurrentPhase = phase.PhaseValidating
	caps.NextAction = "Validation review is required before another run."
	caps.Version++
	caps.UpdatedAt = now
	if err := store.Capsules().Update(caps); err != nil {
		t.Fatalf("update capsule: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.RecoveryClass != RecoveryClassValidationReviewRequired {
		t.Fatalf("expected validation-review posture after mutation, got %s", status.RecoveryClass)
	}

	_, err = coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID:  string(taskID),
		Kind:    recoveryaction.KindInterruptedRunReviewed,
		Summary: "interrupted lineage reviewed",
	})
	if err == nil || !strings.Contains(err.Error(), string(RecoveryClassInterruptedRunRecoverable)) {
		t.Fatalf("expected interrupted-review replay to reject outside interrupted posture, got %v", err)
	}
}

func TestExecuteInterruptedResumeTransitionsToContinueExecutionRequired(t *testing.T) {
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

	out, err := coord.ExecuteInterruptedResume(context.Background(), ExecuteInterruptedResumeRequest{
		TaskID:  string(taskID),
		Summary: "operator resumed interrupted lineage",
		Notes:   []string{"maintain interrupted lineage semantics"},
	})
	if err != nil {
		t.Fatalf("execute interrupted resume: %v", err)
	}
	if out.Action.Kind != recoveryaction.KindInterruptedResumeExecuted {
		t.Fatalf("expected interrupted-resume action kind, got %s", out.Action.Kind)
	}
	if out.RecoveryClass != RecoveryClassContinueExecutionRequired {
		t.Fatalf("expected continue-execution-required recovery class, got %s", out.RecoveryClass)
	}
	if out.RecommendedAction != RecoveryActionExecuteContinueRecovery {
		t.Fatalf("expected execute-continue-recovery action, got %s", out.RecommendedAction)
	}
	if out.ReadyForNextRun {
		t.Fatal("interrupted resume must not claim fresh next-run readiness")
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "not claiming fresh-run readiness") {
		t.Fatalf("expected honest interrupted resume canonical response, got %q", out.CanonicalResponse)
	}

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	if caps.CurrentPhase != phase.PhaseBriefReady {
		t.Fatalf("expected capsule phase %s, got %s", phase.PhaseBriefReady, caps.CurrentPhase)
	}
	if !strings.Contains(strings.ToLower(caps.NextAction), "execute continue recovery") {
		t.Fatalf("expected next action to require continue recovery, got %q", caps.NextAction)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.Phase != phase.PhaseBriefReady {
		t.Fatalf("expected status phase %s after interrupted resume, got %s", phase.PhaseBriefReady, status.Phase)
	}
	if status.RecoveryClass != RecoveryClassContinueExecutionRequired || status.ReadyForNextRun {
		t.Fatalf("unexpected status recovery after interrupted resume: %+v", status)
	}
	if status.LatestRecoveryAction == nil || status.LatestRecoveryAction.Kind != recoveryaction.KindInterruptedResumeExecuted {
		t.Fatalf("expected latest recovery action to be interrupted resume, got %+v", status.LatestRecoveryAction)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.Recovery == nil || inspectOut.Recovery.RecoveryClass != RecoveryClassContinueExecutionRequired || inspectOut.Recovery.ReadyForNextRun {
		t.Fatalf("unexpected inspect recovery after interrupted resume: %+v", inspectOut.Recovery)
	}
	if inspectOut.LatestRecoveryAction == nil || inspectOut.LatestRecoveryAction.Kind != recoveryaction.KindInterruptedResumeExecuted {
		t.Fatalf("expected inspect latest recovery action to be interrupted resume, got %+v", inspectOut.LatestRecoveryAction)
	}

	events, err := store.Proofs().ListByTask(taskID, 100)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	if !hasEvent(events, proof.EventInterruptedRunResumeExecuted) {
		t.Fatal("expected interrupted-run-resume-executed proof event")
	}
	if hasEvent(events, proof.EventRecoveryActionRecorded) {
		t.Fatal("interrupted resume should emit only the dedicated interrupted resume proof event")
	}
}

func TestExecuteInterruptedResumeBlocksFreshRunUntilContinueRecovery(t *testing.T) {
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
	if _, err := coord.ExecuteInterruptedResume(context.Background(), ExecuteInterruptedResumeRequest{TaskID: string(taskID)}); err != nil {
		t.Fatalf("execute interrupted resume: %v", err)
	}

	runStart, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start should return canonical blocked response, got error: %v", err)
	}
	if runStart.RunID != "" || runStart.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected fresh run start to remain blocked, got %+v", runStart)
	}
	if !strings.Contains(strings.ToLower(runStart.CanonicalResponse), "continue finalization") {
		t.Fatalf("expected continue-finalization block reason, got %q", runStart.CanonicalResponse)
	}
}

func TestExecuteInterruptedResumeRejectsInvalidPostureAndReplayAfterSuccess(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.ExecuteInterruptedResume(context.Background(), ExecuteInterruptedResumeRequest{TaskID: string(taskID)}); err == nil || !strings.Contains(err.Error(), string(RecoveryClassInterruptedRunRecoverable)) {
		t.Fatalf("expected interrupted resume invalid-posture rejection, got %v", err)
	}

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
	if _, err := coord.ExecuteInterruptedResume(context.Background(), ExecuteInterruptedResumeRequest{TaskID: string(taskID)}); err != nil {
		t.Fatalf("execute interrupted resume: %v", err)
	}
	_, err = coord.ExecuteInterruptedResume(context.Background(), ExecuteInterruptedResumeRequest{TaskID: string(taskID)})
	if err == nil {
		t.Fatal("expected interrupted resume replay rejection after success")
	}
	if !strings.Contains(err.Error(), string(RecoveryClassInterruptedRunRecoverable)) && !strings.Contains(strings.ToLower(err.Error()), "already been executed") {
		t.Fatalf("expected interrupted resume replay to reject on posture change or prior execution, got %v", err)
	}
}

func TestExecuteContinueRecoveryAcceptsInterruptedResumeTrigger(t *testing.T) {
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
	if _, err := coord.ExecuteInterruptedResume(context.Background(), ExecuteInterruptedResumeRequest{TaskID: string(taskID)}); err != nil {
		t.Fatalf("execute interrupted resume: %v", err)
	}

	out, err := coord.ExecuteContinueRecovery(context.Background(), ExecuteContinueRecoveryRequest{TaskID: string(taskID)})
	if err != nil {
		t.Fatalf("execute continue recovery after interrupted resume: %v", err)
	}
	if out.RecoveryClass != RecoveryClassReadyNextRun || !out.ReadyForNextRun {
		t.Fatalf("expected ready-next-run after interrupted resume + continue recovery, got %+v", out)
	}
	if out.Action.Kind != recoveryaction.KindContinueExecuted {
		t.Fatalf("expected continue-executed action after interrupted resume trigger, got %+v", out.Action)
	}
}

func TestFailedRunRecoveryRequiresReviewNotNextRunReady(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	runOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start: %v", err)
	}
	if runOut.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected failed run status, got %s", runOut.RunStatus)
	}

	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint: %v", err)
	}
	if latestCheckpoint.IsResumable {
		t.Fatal("failed run checkpoint must not claim resumable recovery")
	}

	continueOut, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if continueOut.RecoveryClass != RecoveryClassFailedRunReviewRequired {
		t.Fatalf("expected failed-run review recovery class, got %s", continueOut.RecoveryClass)
	}
	if continueOut.RecommendedAction != RecoveryActionInspectFailedRun {
		t.Fatalf("expected inspect failed run action, got %s", continueOut.RecommendedAction)
	}
	if continueOut.ReadyForNextRun {
		t.Fatal("failed run recovery must not be ready for next run")
	}
	if !strings.Contains(strings.ToLower(continueOut.CanonicalResponse), "not ready") {
		t.Fatalf("expected failed recovery canonical response to avoid ready claim, got %q", continueOut.CanonicalResponse)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.CheckpointResumable {
		t.Fatal("status should report failed checkpoint as non-resumable")
	}
	if status.RecoveryClass != RecoveryClassFailedRunReviewRequired {
		t.Fatalf("expected failed recovery class in status, got %s", status.RecoveryClass)
	}
	if status.ReadyForNextRun {
		t.Fatal("status must not claim ready-for-next-run after failed execution")
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.Recovery == nil {
		t.Fatal("expected inspect recovery assessment")
	}
	if inspectOut.Recovery.RecoveryClass != RecoveryClassFailedRunReviewRequired {
		t.Fatalf("expected inspect failed recovery class, got %s", inspectOut.Recovery.RecoveryClass)
	}
	if inspectOut.Recovery.ReadyForNextRun {
		t.Fatal("inspect recovery must not claim ready-for-next-run after failed execution")
	}
}

func TestRecordRecoveryActionFailedRunReviewedPromotesDecisionRequired(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("run start: %v", err)
	}

	out, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindFailedRunReviewed,
		Notes:  []string{"reviewed failure evidence"},
	})
	if err != nil {
		t.Fatalf("record failed-run review: %v", err)
	}
	if out.RecoveryClass != RecoveryClassDecisionRequired {
		t.Fatalf("expected decision-required recovery class, got %s", out.RecoveryClass)
	}
	if out.RecommendedAction != RecoveryActionMakeResumeDecision {
		t.Fatalf("expected make-resume-decision action, got %s", out.RecommendedAction)
	}
	if out.ReadyForNextRun {
		t.Fatal("failed-run review should not make the task ready yet")
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.LatestRecoveryAction == nil || status.LatestRecoveryAction.Kind != recoveryaction.KindFailedRunReviewed {
		t.Fatalf("expected latest recovery action in status, got %+v", status.LatestRecoveryAction)
	}
	if status.RecoveryClass != RecoveryClassDecisionRequired {
		t.Fatalf("expected status decision-required class, got %s", status.RecoveryClass)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.LatestRecoveryAction == nil || inspectOut.LatestRecoveryAction.Kind != recoveryaction.KindFailedRunReviewed {
		t.Fatalf("expected latest inspect recovery action, got %+v", inspectOut.LatestRecoveryAction)
	}
	if len(inspectOut.RecentRecoveryActions) != 1 {
		t.Fatalf("expected one persisted recovery action, got %d", len(inspectOut.RecentRecoveryActions))
	}
	events, err := store.Proofs().ListByTask(taskID, 100)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	if !hasEvent(events, proof.EventRecoveryActionRecorded) {
		t.Fatal("expected recovery-action-recorded proof event")
	}
}

func TestRecordRecoveryActionDecisionContinueRequiresExplicitContinueExecution(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("run start: %v", err)
	}
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindFailedRunReviewed,
	}); err != nil {
		t.Fatalf("record failed-run review: %v", err)
	}

	out, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionContinue,
	})
	if err != nil {
		t.Fatalf("record decision continue: %v", err)
	}
	if out.RecoveryClass != RecoveryClassContinueExecutionRequired {
		t.Fatalf("expected continue-execution-required recovery class, got %s", out.RecoveryClass)
	}
	if out.RecommendedAction != RecoveryActionExecuteContinueRecovery {
		t.Fatalf("expected execute-continue-recovery action, got %s", out.RecommendedAction)
	}
	if out.ReadyForNextRun {
		t.Fatal("continue decision must not claim ready-for-next-run before continue execution")
	}

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	if caps.CurrentPhase != phase.PhaseBriefReady {
		t.Fatalf("expected capsule phase %s, got %s", phase.PhaseBriefReady, caps.CurrentPhase)
	}
	if !strings.Contains(strings.ToLower(caps.NextAction), "execute continue recovery") {
		t.Fatalf("expected capsule next action to require continue execution, got %q", caps.NextAction)
	}
}

func TestRecordRecoveryActionDecisionRegenerateBriefRequiresRebrief(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("run start: %v", err)
	}
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindFailedRunReviewed,
	}); err != nil {
		t.Fatalf("record failed-run review: %v", err)
	}

	out, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionRegenerateBrief,
	})
	if err != nil {
		t.Fatalf("record regenerate-brief decision: %v", err)
	}
	if out.RecoveryClass != RecoveryClassRebriefRequired {
		t.Fatalf("expected rebrief-required class, got %s", out.RecoveryClass)
	}
	if out.RecommendedAction != RecoveryActionRegenerateBrief {
		t.Fatalf("expected regenerate-brief action, got %s", out.RecommendedAction)
	}
	if out.ReadyForNextRun {
		t.Fatal("regenerate-brief decision must not claim next-run readiness")
	}

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	if caps.CurrentPhase != phase.PhaseBlocked {
		t.Fatalf("expected capsule phase %s, got %s", phase.PhaseBlocked, caps.CurrentPhase)
	}
}

func TestRecordRecoveryActionIdempotentReplayReusesLatestAction(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("run start: %v", err)
	}

	first, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID:  string(taskID),
		Kind:    recoveryaction.KindFailedRunReviewed,
		Summary: "reviewed failed run",
		Notes:   []string{"same-note"},
	})
	if err != nil {
		t.Fatalf("first record recovery action: %v", err)
	}
	second, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID:  string(taskID),
		Kind:    recoveryaction.KindFailedRunReviewed,
		Summary: "reviewed failed run",
		Notes:   []string{"same-note"},
	})
	if err != nil {
		t.Fatalf("second record recovery action: %v", err)
	}
	if first.Action.ActionID != second.Action.ActionID {
		t.Fatalf("expected idempotent recovery action replay, got %s and %s", first.Action.ActionID, second.Action.ActionID)
	}
	actions, err := store.RecoveryActions().ListByTask(taskID, 10)
	if err != nil {
		t.Fatalf("list recovery actions: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("expected one persisted recovery action, got %d", len(actions))
	}
}

func TestRecordRecoveryActionDecisionContinueReplayReusesLatestAction(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("run start: %v", err)
	}
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindFailedRunReviewed,
	}); err != nil {
		t.Fatalf("record failed-run review: %v", err)
	}

	first, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionContinue,
	})
	if err != nil {
		t.Fatalf("first decision continue: %v", err)
	}
	second, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionContinue,
	})
	if err != nil {
		t.Fatalf("second decision continue: %v", err)
	}
	if first.Action.ActionID != second.Action.ActionID {
		t.Fatalf("expected decision-continue replay to reuse latest action, got %s and %s", first.Action.ActionID, second.Action.ActionID)
	}
	if second.RecoveryClass != RecoveryClassContinueExecutionRequired || second.ReadyForNextRun {
		t.Fatalf("expected continue-execution-required after decision continue replay, got %+v", second)
	}
	actions, err := store.RecoveryActions().ListByTask(taskID, 10)
	if err != nil {
		t.Fatalf("list recovery actions: %v", err)
	}
	if len(actions) != 2 {
		t.Fatalf("expected exactly two persisted recovery actions (review + decision), got %d", len(actions))
	}
}

func TestRecordRecoveryActionRepairIntentPersistsWhileStillBlocked(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	briefRec, err := store.Briefs().Get(caps.CurrentBriefID)
	if err != nil {
		t.Fatalf("get brief: %v", err)
	}
	packet := handoff.Packet{
		Version:          1,
		HandoffID:        "hnd_broken_repair_intent",
		TaskID:           taskID,
		Status:           handoff.StatusCreated,
		SourceWorker:     rundomain.WorkerKindCodex,
		TargetWorker:     rundomain.WorkerKindClaude,
		HandoffMode:      handoff.ModeResume,
		Reason:           "broken handoff for repair intent test",
		CurrentPhase:     caps.CurrentPhase,
		CheckpointID:     common.CheckpointID("chk_missing_repair_intent"),
		BriefID:          briefRec.BriefID,
		IntentID:         caps.CurrentIntentID,
		CapsuleVersion:   caps.Version,
		RepoAnchor:       anchorFromCapsule(caps),
		IsResumable:      true,
		ResumeDescriptor: "broken repair handoff",
		Goal:             caps.Goal,
		BriefObjective:   briefRec.Objective,
		NormalizedAction: briefRec.NormalizedAction,
		Constraints:      append([]string{}, briefRec.Constraints...),
		DoneCriteria:     append([]string{}, briefRec.DoneCriteria...),
		CreatedAt:        time.Now().UTC(),
	}
	if err := store.Handoffs().Create(packet); err != nil {
		t.Fatalf("create broken handoff: %v", err)
	}

	out, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID:  string(taskID),
		Kind:    recoveryaction.KindRepairIntentRecorded,
		Summary: "repair broken checkpoint reference",
	})
	if err != nil {
		t.Fatalf("record repair intent: %v", err)
	}
	if out.RecoveryClass != RecoveryClassRepairRequired {
		t.Fatalf("expected repair-required class, got %s", out.RecoveryClass)
	}
	if out.ReadyForNextRun {
		t.Fatal("repair intent must not claim next-run readiness")
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.LatestRecoveryAction == nil || inspectOut.LatestRecoveryAction.Kind != recoveryaction.KindRepairIntentRecorded {
		t.Fatalf("expected repair intent action in inspect output, got %+v", inspectOut.LatestRecoveryAction)
	}
	if inspectOut.Recovery == nil || !strings.Contains(strings.ToLower(inspectOut.Recovery.Reason), "repair intent recorded") {
		t.Fatalf("expected recovery reason to reflect repair intent, got %+v", inspectOut.Recovery)
	}
}

func TestRecordRecoveryActionRejectsInvalidPosture(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	_, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionContinue,
	})
	if err == nil || !strings.Contains(err.Error(), string(RecoveryClassDecisionRequired)) {
		t.Fatalf("expected decision-required posture rejection, got %v", err)
	}
}

func TestExecuteRebriefRegeneratesBriefAndReadiesTask(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("run start: %v", err)
	}
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindFailedRunReviewed,
	}); err != nil {
		t.Fatalf("record failed-run review: %v", err)
	}
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionRegenerateBrief,
	}); err != nil {
		t.Fatalf("record regenerate-brief decision: %v", err)
	}

	beforeCaps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule before rebrief: %v", err)
	}
	beforeBriefID := beforeCaps.CurrentBriefID

	out, err := coord.ExecuteRebrief(context.Background(), ExecuteRebriefRequest{TaskID: string(taskID)})
	if err != nil {
		t.Fatalf("execute rebrief: %v", err)
	}
	if out.PreviousBriefID != beforeBriefID {
		t.Fatalf("expected previous brief %s, got %s", beforeBriefID, out.PreviousBriefID)
	}
	if out.BriefID == "" || out.BriefID == beforeBriefID {
		t.Fatalf("expected new brief id, got %s", out.BriefID)
	}
	if out.RecoveryClass != RecoveryClassReadyNextRun {
		t.Fatalf("expected ready-next-run recovery class, got %s", out.RecoveryClass)
	}
	if out.RecommendedAction != RecoveryActionStartNextRun {
		t.Fatalf("expected start-next-run action, got %s", out.RecommendedAction)
	}
	if !out.ReadyForNextRun {
		t.Fatal("expected ready-for-next-run after rebrief")
	}

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule after rebrief: %v", err)
	}
	if caps.CurrentBriefID != out.BriefID {
		t.Fatalf("expected capsule current brief %s, got %s", out.BriefID, caps.CurrentBriefID)
	}
	if caps.CurrentPhase != phase.PhaseBriefReady {
		t.Fatalf("expected phase %s, got %s", phase.PhaseBriefReady, caps.CurrentPhase)
	}

	briefRec, err := store.Briefs().Get(out.BriefID)
	if err != nil {
		t.Fatalf("get regenerated brief: %v", err)
	}
	if briefRec.BriefHash != out.BriefHash {
		t.Fatalf("expected brief hash %s, got %s", out.BriefHash, briefRec.BriefHash)
	}

	events, err := store.Proofs().ListByTask(taskID, 200)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	if !hasEvent(events, proof.EventBriefRegenerated) {
		t.Fatal("expected brief-regenerated proof event")
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.CurrentBriefID != out.BriefID {
		t.Fatalf("expected status current brief %s, got %s", out.BriefID, status.CurrentBriefID)
	}
	if status.RecoveryClass != RecoveryClassReadyNextRun || !status.ReadyForNextRun {
		t.Fatalf("expected ready-next-run status after rebrief, got %+v", status)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.Brief == nil || inspectOut.Brief.BriefID != out.BriefID {
		t.Fatalf("expected inspect current brief %s, got %+v", out.BriefID, inspectOut.Brief)
	}
	if inspectOut.Recovery == nil || inspectOut.Recovery.RecoveryClass != RecoveryClassReadyNextRun {
		t.Fatalf("expected inspect ready-next-run recovery, got %+v", inspectOut.Recovery)
	}
}

func TestExecuteRebriefRejectsInvalidPosture(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	_, err := coord.ExecuteRebrief(context.Background(), ExecuteRebriefRequest{TaskID: string(taskID)})
	if err == nil || !strings.Contains(err.Error(), string(RecoveryClassRebriefRequired)) {
		t.Fatalf("expected rebrief-required rejection, got %v", err)
	}
}

func TestExecuteRebriefReplayRejectedAfterSuccessfulExecution(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("run start: %v", err)
	}
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindFailedRunReviewed,
	}); err != nil {
		t.Fatalf("record failed-run review: %v", err)
	}
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionRegenerateBrief,
	}); err != nil {
		t.Fatalf("record regenerate-brief decision: %v", err)
	}
	if _, err := coord.ExecuteRebrief(context.Background(), ExecuteRebriefRequest{TaskID: string(taskID)}); err != nil {
		t.Fatalf("first execute rebrief: %v", err)
	}

	_, err := coord.ExecuteRebrief(context.Background(), ExecuteRebriefRequest{TaskID: string(taskID)})
	if err == nil || !strings.Contains(err.Error(), string(RecoveryClassRebriefRequired)) {
		t.Fatalf("expected replay rebrief rejection after success, got %v", err)
	}
}

func TestExecuteContinueRecoveryFinalizesReadyState(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("run start: %v", err)
	}
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindFailedRunReviewed,
	}); err != nil {
		t.Fatalf("record failed-run review: %v", err)
	}
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionContinue,
	}); err != nil {
		t.Fatalf("record continue decision: %v", err)
	}

	beforeCaps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule before continue execution: %v", err)
	}
	beforeBrief, err := store.Briefs().Get(beforeCaps.CurrentBriefID)
	if err != nil {
		t.Fatalf("get current brief before continue execution: %v", err)
	}

	out, err := coord.ExecuteContinueRecovery(context.Background(), ExecuteContinueRecoveryRequest{TaskID: string(taskID)})
	if err != nil {
		t.Fatalf("execute continue recovery: %v", err)
	}
	if out.BriefID != beforeBrief.BriefID {
		t.Fatalf("expected continue recovery to keep brief %s, got %s", beforeBrief.BriefID, out.BriefID)
	}
	if out.BriefHash != beforeBrief.BriefHash {
		t.Fatalf("expected continue recovery to keep brief hash %s, got %s", beforeBrief.BriefHash, out.BriefHash)
	}
	if out.Action.Kind != recoveryaction.KindContinueExecuted {
		t.Fatalf("expected continue-executed action, got %s", out.Action.Kind)
	}
	if out.RecoveryClass != RecoveryClassReadyNextRun {
		t.Fatalf("expected ready-next-run recovery class, got %s", out.RecoveryClass)
	}
	if out.RecommendedAction != RecoveryActionStartNextRun {
		t.Fatalf("expected start-next-run action, got %s", out.RecommendedAction)
	}
	if !out.ReadyForNextRun {
		t.Fatal("expected ready-for-next-run after continue recovery execution")
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "confirmed continuation") {
		t.Fatalf("expected canonical response to describe continue confirmation, got %q", out.CanonicalResponse)
	}

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule after continue execution: %v", err)
	}
	if caps.CurrentBriefID != beforeBrief.BriefID {
		t.Fatalf("expected capsule current brief %s, got %s", beforeBrief.BriefID, caps.CurrentBriefID)
	}
	if caps.CurrentPhase != phase.PhaseBriefReady {
		t.Fatalf("expected phase %s, got %s", phase.PhaseBriefReady, caps.CurrentPhase)
	}

	events, err := store.Proofs().ListByTask(taskID, 200)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	if !hasEvent(events, proof.EventRecoveryContinueExecuted) {
		t.Fatal("expected recovery-continue-executed proof event")
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.CurrentBriefID != beforeBrief.BriefID {
		t.Fatalf("expected status current brief %s, got %s", beforeBrief.BriefID, status.CurrentBriefID)
	}
	if status.RecoveryClass != RecoveryClassReadyNextRun || !status.ReadyForNextRun {
		t.Fatalf("expected ready-next-run status after continue execution, got %+v", status)
	}
	if status.LatestRecoveryAction == nil || status.LatestRecoveryAction.Kind != recoveryaction.KindContinueExecuted {
		t.Fatalf("expected latest continue-executed action in status, got %+v", status.LatestRecoveryAction)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.Brief == nil || inspectOut.Brief.BriefID != beforeBrief.BriefID {
		t.Fatalf("expected inspect current brief %s, got %+v", beforeBrief.BriefID, inspectOut.Brief)
	}
	if inspectOut.Recovery == nil || inspectOut.Recovery.RecoveryClass != RecoveryClassReadyNextRun {
		t.Fatalf("expected inspect ready-next-run recovery, got %+v", inspectOut.Recovery)
	}
	if inspectOut.LatestRecoveryAction == nil || inspectOut.LatestRecoveryAction.Kind != recoveryaction.KindContinueExecuted {
		t.Fatalf("expected inspect latest continue-executed action, got %+v", inspectOut.LatestRecoveryAction)
	}
}

func TestExecuteContinueRecoveryRejectsInvalidPosture(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	_, err := coord.ExecuteContinueRecovery(context.Background(), ExecuteContinueRecoveryRequest{TaskID: string(taskID)})
	if err == nil || !strings.Contains(err.Error(), string(RecoveryClassContinueExecutionRequired)) || !strings.Contains(err.Error(), string(recoveryaction.KindDecisionContinue)) || !strings.Contains(err.Error(), string(recoveryaction.KindInterruptedResumeExecuted)) {
		t.Fatalf("expected continue-execution-required trigger rejection, got %v", err)
	}
}

func TestExecuteContinueRecoveryReplayRejectedAfterSuccessfulExecution(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("run start: %v", err)
	}
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindFailedRunReviewed,
	}); err != nil {
		t.Fatalf("record failed-run review: %v", err)
	}
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionContinue,
	}); err != nil {
		t.Fatalf("record continue decision: %v", err)
	}
	if _, err := coord.ExecuteContinueRecovery(context.Background(), ExecuteContinueRecoveryRequest{TaskID: string(taskID)}); err != nil {
		t.Fatalf("first execute continue recovery: %v", err)
	}

	_, err := coord.ExecuteContinueRecovery(context.Background(), ExecuteContinueRecoveryRequest{TaskID: string(taskID)})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "already been executed") {
		t.Fatalf("expected replay continue recovery rejection after success, got %v", err)
	}
}

func TestInspectTaskSurfacesRecoveryIssuesForBrokenHandoffState(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	briefRec, err := store.Briefs().Get(caps.CurrentBriefID)
	if err != nil {
		t.Fatalf("get brief: %v", err)
	}
	packet := handoff.Packet{
		Version:          1,
		HandoffID:        "hnd_broken_inspect_recovery",
		TaskID:           taskID,
		Status:           handoff.StatusCreated,
		SourceWorker:     rundomain.WorkerKindCodex,
		TargetWorker:     rundomain.WorkerKindClaude,
		HandoffMode:      handoff.ModeResume,
		Reason:           "broken handoff for inspect recovery test",
		CurrentPhase:     caps.CurrentPhase,
		CheckpointID:     common.CheckpointID("chk_missing_for_inspect_recovery"),
		BriefID:          briefRec.BriefID,
		IntentID:         caps.CurrentIntentID,
		CapsuleVersion:   caps.Version,
		RepoAnchor:       anchorFromCapsule(caps),
		IsResumable:      true,
		ResumeDescriptor: "broken inspect handoff",
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
		t.Fatalf("create broken handoff packet: %v", err)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.Handoff == nil || inspectOut.Handoff.HandoffID != packet.HandoffID {
		t.Fatalf("expected inspect handoff %s, got %+v", packet.HandoffID, inspectOut.Handoff)
	}
	if inspectOut.Recovery == nil {
		t.Fatal("expected inspect recovery assessment")
	}
	if inspectOut.Recovery.RecoveryClass != RecoveryClassRepairRequired {
		t.Fatalf("expected repair-required recovery class, got %s", inspectOut.Recovery.RecoveryClass)
	}
	if len(inspectOut.Recovery.Issues) == 0 {
		t.Fatal("expected inspect recovery issues for broken handoff state")
	}
	foundCheckpointIssue := false
	for _, issue := range inspectOut.Recovery.Issues {
		if strings.Contains(strings.ToLower(issue.Message), "missing checkpoint") {
			foundCheckpointIssue = true
			break
		}
	}
	if !foundCheckpointIssue {
		t.Fatalf("expected missing-checkpoint issue, got %+v", inspectOut.Recovery.Issues)
	}
}

func TestContinueBlockedWhenBriefMissing(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	start, err := coord.StartTask(context.Background(), "No brief continue", "/tmp/repo")
	if err != nil {
		t.Fatalf("start task: %v", err)
	}

	out, err := coord.ContinueTask(context.Background(), string(start.TaskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if out.Outcome != ContinueOutcomeBlockedInconsistent {
		t.Fatalf("expected blocked inconsistent outcome, got %s", out.Outcome)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "inconsistent") {
		t.Fatalf("expected canonical inconsistent response, got %q", out.CanonicalResponse)
	}
}

func TestContinueBlockedWhenCheckpointBriefMissing(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	lastEvent, err := latestEventID(store, taskID)
	if err != nil {
		t.Fatalf("latest event id: %v", err)
	}
	bad := checkpoint.Checkpoint{
		Version:            1,
		CheckpointID:       common.CheckpointID("chk_bad_missing_brief"),
		TaskID:             taskID,
		RunID:              "",
		CreatedAt:          time.Now().UTC().Add(5 * time.Second),
		Trigger:            checkpoint.TriggerManual,
		CapsuleVersion:     caps.Version,
		Phase:              caps.CurrentPhase,
		Anchor:             anchorFromCapsule(caps),
		IntentID:           caps.CurrentIntentID,
		BriefID:            common.BriefID("brf_missing_checkpoint"),
		ContextPackID:      "",
		LastEventID:        lastEvent,
		PendingDecisionIDs: []common.DecisionID{},
		ResumeDescriptor:   "bad checkpoint for test",
		IsResumable:        true,
	}
	if err := store.Checkpoints().Create(bad); err != nil {
		t.Fatalf("create bad checkpoint: %v", err)
	}

	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue with bad checkpoint brief: %v", err)
	}
	if out.Outcome != ContinueOutcomeBlockedInconsistent {
		t.Fatalf("expected blocked inconsistent outcome, got %s", out.Outcome)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "missing brief") {
		t.Fatalf("expected canonical missing-brief message, got %q", out.CanonicalResponse)
	}
}

func TestContinueBlockedWhenCheckpointRunMissing(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	lastEvent, err := latestEventID(store, taskID)
	if err != nil {
		t.Fatalf("latest event id: %v", err)
	}
	bad := checkpoint.Checkpoint{
		Version:            1,
		CheckpointID:       common.CheckpointID("chk_bad_missing_run"),
		TaskID:             taskID,
		RunID:              common.RunID("run_missing_for_checkpoint"),
		CreatedAt:          time.Now().UTC().Add(5 * time.Second),
		Trigger:            checkpoint.TriggerManual,
		CapsuleVersion:     caps.Version,
		Phase:              caps.CurrentPhase,
		Anchor:             anchorFromCapsule(caps),
		IntentID:           caps.CurrentIntentID,
		BriefID:            caps.CurrentBriefID,
		ContextPackID:      "",
		LastEventID:        lastEvent,
		PendingDecisionIDs: []common.DecisionID{},
		ResumeDescriptor:   "bad checkpoint for missing run test",
		IsResumable:        true,
	}
	if err := store.Checkpoints().Create(bad); err != nil {
		t.Fatalf("create bad checkpoint: %v", err)
	}

	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue with bad checkpoint run: %v", err)
	}
	if out.Outcome != ContinueOutcomeBlockedInconsistent {
		t.Fatalf("expected blocked inconsistent outcome, got %s", out.Outcome)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "missing run") {
		t.Fatalf("expected canonical missing-run message, got %q", out.CanonicalResponse)
	}
}

func TestContinueBlockedWhenRunningCheckpointLinkageBroken(t *testing.T) {
	store := newTestStore(t)
	taskID := setupTaskWithBrief(t, newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess()))

	failCoord, err := NewCoordinator(Dependencies{
		Store:          store,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    &failingSynthesizer{err: errors.New("run synth failure")},
		AnchorProvider: defaultAnchor(),
		ShellSessions:  NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new failing coordinator: %v", err)
	}
	if _, err := failCoord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err == nil {
		t.Fatal("expected staged finalization failure to leave RUNNING state")
	}

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	lastEvent, err := latestEventID(store, taskID)
	if err != nil {
		t.Fatalf("latest event id: %v", err)
	}
	bad := checkpoint.Checkpoint{
		Version:            1,
		CheckpointID:       common.CheckpointID("chk_bad_running_linkage"),
		TaskID:             taskID,
		RunID:              "",
		CreatedAt:          time.Now().UTC().Add(10 * time.Second),
		Trigger:            checkpoint.TriggerManual,
		CapsuleVersion:     caps.Version,
		Phase:              caps.CurrentPhase,
		Anchor:             anchorFromCapsule(caps),
		IntentID:           caps.CurrentIntentID,
		BriefID:            caps.CurrentBriefID,
		ContextPackID:      "",
		LastEventID:        lastEvent,
		PendingDecisionIDs: []common.DecisionID{},
		ResumeDescriptor:   "broken checkpoint linkage for test",
		IsResumable:        true,
	}
	if err := store.Checkpoints().Create(bad); err != nil {
		t.Fatalf("create bad checkpoint: %v", err)
	}

	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if out.Outcome != ContinueOutcomeBlockedInconsistent {
		t.Fatalf("expected blocked inconsistent outcome, got %s", out.Outcome)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "inconsistent") {
		t.Fatalf("expected inconsistent canonical response, got %q", out.CanonicalResponse)
	}
}

func TestContinueBlockedWhenLatestHandoffCheckpointMissing(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	briefRec, err := store.Briefs().Get(caps.CurrentBriefID)
	if err != nil {
		t.Fatalf("get brief: %v", err)
	}

	packet := handoff.Packet{
		Version:          1,
		HandoffID:        "hnd_missing_checkpoint_for_continue",
		TaskID:           taskID,
		Status:           handoff.StatusCreated,
		SourceWorker:     rundomain.WorkerKindCodex,
		TargetWorker:     rundomain.WorkerKindClaude,
		HandoffMode:      handoff.ModeResume,
		Reason:           "broken handoff state",
		CurrentPhase:     caps.CurrentPhase,
		CheckpointID:     common.CheckpointID("chk_missing_for_handoff"),
		BriefID:          briefRec.BriefID,
		IntentID:         caps.CurrentIntentID,
		CapsuleVersion:   caps.Version,
		RepoAnchor:       anchorFromCapsule(caps),
		IsResumable:      true,
		ResumeDescriptor: "broken handoff packet for continue validation",
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
		t.Fatalf("create broken handoff packet: %v", err)
	}

	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if out.Outcome != ContinueOutcomeBlockedInconsistent {
		t.Fatalf("expected blocked inconsistent outcome, got %s", out.Outcome)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "handoff") {
		t.Fatalf("expected handoff-related inconsistency, got %q", out.CanonicalResponse)
	}
}

func TestContinueSafeAssessmentDoesNotRequireWriteTransaction(t *testing.T) {
	base := newTestStore(t)
	baseCoord := newTestCoordinator(t, base, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, baseCoord)
	seed, err := baseCoord.CreateCheckpoint(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}

	counting := &txCountingStore{base: base}
	coord, err := NewCoordinator(Dependencies{
		Store:          counting,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    canonical.NewSimpleSynthesizer(),
		AnchorProvider: defaultAnchor(),
		ShellSessions:  NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}

	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if out.Outcome != ContinueOutcomeSafe {
		t.Fatalf("expected safe outcome, got %s", out.Outcome)
	}
	if out.CheckpointID != seed.CheckpointID {
		t.Fatalf("expected checkpoint reuse %s, got %s", seed.CheckpointID, out.CheckpointID)
	}
	if counting.withTxCount < 1 {
		t.Fatalf("expected lightweight durable write path for no-op safe continue")
	}
}

func TestContinueSafeReuseDoesNotCreateCheckpointChurn(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	seed, err := coord.CreateCheckpoint(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}

	first, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("first continue: %v", err)
	}
	second, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("second continue: %v", err)
	}
	if first.CheckpointID != seed.CheckpointID || second.CheckpointID != seed.CheckpointID {
		t.Fatalf("expected checkpoint reuse across continues, got first=%s second=%s seed=%s", first.CheckpointID, second.CheckpointID, seed.CheckpointID)
	}
	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint: %v", err)
	}
	if latestCheckpoint.CheckpointID != seed.CheckpointID {
		t.Fatalf("expected no checkpoint churn, latest=%s seed=%s", latestCheckpoint.CheckpointID, seed.CheckpointID)
	}
}

func TestSafeContinueCreatesCheckpointWithContinueTrigger(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if out.Outcome != ContinueOutcomeSafe {
		t.Fatalf("expected safe continue, got %s", out.Outcome)
	}
	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint: %v", err)
	}
	if latestCheckpoint.Trigger != checkpoint.TriggerContinue {
		t.Fatalf("expected continue trigger, got %s", latestCheckpoint.Trigger)
	}
}

func setupTaskWithBrief(t *testing.T, coord *Coordinator) common.TaskID {
	t.Helper()
	start, err := coord.StartTask(context.Background(), "Run lifecycle test", "/tmp/repo")
	if err != nil {
		t.Fatalf("start task: %v", err)
	}
	if _, err := coord.MessageTask(context.Background(), string(start.TaskID), "start implementation process"); err != nil {
		t.Fatalf("message task: %v", err)
	}
	return start.TaskID
}

func mustCurrentBriefID(t *testing.T, store storage.Store, taskID common.TaskID) common.BriefID {
	t.Helper()
	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule for brief id: %v", err)
	}
	if caps.CurrentBriefID == "" {
		t.Fatal("expected current brief id")
	}
	return caps.CurrentBriefID
}

func hasEvent(events []proof.Event, typ proof.EventType) bool {
	for _, e := range events {
		if e.Type == typ {
			return true
		}
	}
	return false
}

func countEvents(events []proof.Event, typ proof.EventType) int {
	count := 0
	for _, e := range events {
		if e.Type == typ {
			count++
		}
	}
	return count
}

func hasEventMust(t *testing.T, store storage.Store, taskID common.TaskID, typ proof.EventType) bool {
	t.Helper()
	events, err := store.Proofs().ListByTask(taskID, 200)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	return hasEvent(events, typ)
}

func latestEventID(store storage.Store, taskID common.TaskID) (common.EventID, error) {
	events, err := store.Proofs().ListByTask(taskID, 1)
	if err != nil {
		return "", err
	}
	if len(events) == 0 {
		return "", nil
	}
	return events[len(events)-1].EventID, nil
}

func newTestCoordinator(t *testing.T, store *sqlite.Store, anchorProvider anchorgit.Provider, adapter adapter_contract.WorkerAdapter) *Coordinator {
	t.Helper()
	coord, err := NewCoordinator(Dependencies{
		Store:          store,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  adapter,
		Synthesizer:    canonical.NewSimpleSynthesizer(),
		AnchorProvider: anchorProvider,
		ShellSessions:  NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	return coord
}

func newTestStore(t *testing.T) *sqlite.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "tuku-test.db")
	store, err := sqlite.NewStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

type staticAnchorProvider struct {
	snapshot anchorgit.Snapshot
}

func (p *staticAnchorProvider) Capture(_ context.Context, repoRoot string) anchorgit.Snapshot {
	out := p.snapshot
	if out.RepoRoot == "" {
		out.RepoRoot = repoRoot
	}
	if out.CapturedAt.IsZero() {
		out.CapturedAt = time.Now().UTC()
	}
	return out
}

func defaultAnchor() anchorgit.Provider {
	return &staticAnchorProvider{snapshot: anchorgit.Snapshot{RepoRoot: "/tmp/repo", Branch: "main", HeadSHA: "head-x", WorkingTreeDirty: false, CapturedAt: time.Unix(1700004000, 0).UTC()}}
}

type fakeWorkerAdapter struct {
	kind      adapter_contract.WorkerKind
	result    adapter_contract.ExecutionResult
	err       error
	called    bool
	lastReq   adapter_contract.ExecutionRequest
	onExecute func(req adapter_contract.ExecutionRequest)
}

func newFakeAdapterSuccess() *fakeWorkerAdapter {
	now := time.Now().UTC()
	return &fakeWorkerAdapter{
		kind: adapter_contract.WorkerCodex,
		result: adapter_contract.ExecutionResult{
			ExitCode:          0,
			StartedAt:         now,
			EndedAt:           now.Add(200 * time.Millisecond),
			Stdout:            "implemented bounded step",
			Stderr:            "",
			ChangedFiles:      []string{"internal/orchestrator/service.go"},
			ValidationSignals: []string{"worker mentioned test activity"},
			Summary:           "bounded codex step complete",
		},
	}
}

func newFakeAdapterExitFailure() *fakeWorkerAdapter {
	now := time.Now().UTC()
	return &fakeWorkerAdapter{
		kind: adapter_contract.WorkerCodex,
		result: adapter_contract.ExecutionResult{
			ExitCode:  1,
			StartedAt: now,
			EndedAt:   now.Add(100 * time.Millisecond),
			Stdout:    "attempted change",
			Stderr:    "test failed",
			Summary:   "run failed",
		},
	}
}

func newFakeAdapterError(err error) *fakeWorkerAdapter {
	return &fakeWorkerAdapter{
		kind: adapter_contract.WorkerCodex,
		result: adapter_contract.ExecutionResult{
			ExitCode: -1,
			Summary:  "adapter error",
		},
		err: err,
	}
}

func (f *fakeWorkerAdapter) Name() adapter_contract.WorkerKind {
	return f.kind
}

func (f *fakeWorkerAdapter) Execute(_ context.Context, req adapter_contract.ExecutionRequest, _ adapter_contract.WorkerEventSink) (adapter_contract.ExecutionResult, error) {
	f.called = true
	f.lastReq = req
	if f.onExecute != nil {
		f.onExecute(req)
	}
	out := f.result
	if out.WorkerRunID == "" {
		out.WorkerRunID = common.WorkerRunID("wrk_" + string(req.RunID))
	}
	if out.Command == "" {
		out.Command = "codex"
	}
	if out.StartedAt.IsZero() {
		out.StartedAt = time.Now().UTC()
	}
	if out.EndedAt.IsZero() {
		out.EndedAt = out.StartedAt.Add(100 * time.Millisecond)
	}
	return out, f.err
}

var _ adapter_contract.WorkerAdapter = (*fakeWorkerAdapter)(nil)

type failingSynthesizer struct {
	err error
}

func (s *failingSynthesizer) Synthesize(_ context.Context, _ capsule.WorkCapsule, _ []proof.Event) (string, error) {
	return "", s.err
}

type faultInjectedStore struct {
	base            storage.Store
	failProofAppend bool
}

func (s *faultInjectedStore) Capsules() storage.CapsuleStore {
	return s.base.Capsules()
}

func (s *faultInjectedStore) Conversations() storage.ConversationStore {
	return s.base.Conversations()
}

func (s *faultInjectedStore) Intents() storage.IntentStore {
	return s.base.Intents()
}

func (s *faultInjectedStore) Briefs() storage.BriefStore {
	return s.base.Briefs()
}

func (s *faultInjectedStore) Proofs() storage.ProofStore {
	if !s.failProofAppend {
		return s.base.Proofs()
	}
	return &faultProofStore{base: s.base.Proofs()}
}

func (s *faultInjectedStore) Runs() storage.RunStore {
	return s.base.Runs()
}

func (s *faultInjectedStore) Checkpoints() storage.CheckpointStore {
	return s.base.Checkpoints()
}

func (s *faultInjectedStore) RecoveryActions() storage.RecoveryActionStore {
	return s.base.RecoveryActions()
}

func (s *faultInjectedStore) OperatorStepReceipts() storage.OperatorStepReceiptStore {
	return s.base.OperatorStepReceipts()
}

func (s *faultInjectedStore) TransitionReceipts() storage.TransitionReceiptStore {
	return s.base.TransitionReceipts()
}

func (s *faultInjectedStore) IncidentTriages() storage.IncidentTriageStore {
	return s.base.IncidentTriages()
}

func (s *faultInjectedStore) IncidentFollowUps() storage.IncidentFollowUpStore {
	return s.base.IncidentFollowUps()
}

func (s *faultInjectedStore) ContextPacks() storage.ContextPackStore {
	return s.base.ContextPacks()
}

func (s *faultInjectedStore) TaskMemories() storage.TaskMemoryStore {
	return s.base.TaskMemories()
}

func (s *faultInjectedStore) Benchmarks() storage.BenchmarkStore {
	return s.base.Benchmarks()
}

func (s *faultInjectedStore) PolicyDecisions() storage.PolicyDecisionStore {
	return s.base.PolicyDecisions()
}

func (s *faultInjectedStore) WithTx(fn func(storage.Store) error) error {
	return s.base.WithTx(func(txStore storage.Store) error {
		wrapped := &faultInjectedStore{
			base:            txStore,
			failProofAppend: s.failProofAppend,
		}
		return fn(wrapped)
	})
}

type txCountingStore struct {
	base        storage.Store
	withTxCount int
}

func (s *txCountingStore) Capsules() storage.CapsuleStore {
	return s.base.Capsules()
}

func (s *txCountingStore) Conversations() storage.ConversationStore {
	return s.base.Conversations()
}

func (s *txCountingStore) Intents() storage.IntentStore {
	return s.base.Intents()
}

func (s *txCountingStore) Briefs() storage.BriefStore {
	return s.base.Briefs()
}

func (s *txCountingStore) OperatorStepReceipts() storage.OperatorStepReceiptStore {
	return s.base.OperatorStepReceipts()
}

func (s *txCountingStore) TransitionReceipts() storage.TransitionReceiptStore {
	return s.base.TransitionReceipts()
}

func (s *txCountingStore) IncidentTriages() storage.IncidentTriageStore {
	return s.base.IncidentTriages()
}

func (s *txCountingStore) IncidentFollowUps() storage.IncidentFollowUpStore {
	return s.base.IncidentFollowUps()
}

func (s *txCountingStore) Proofs() storage.ProofStore {
	return s.base.Proofs()
}

func (s *txCountingStore) Runs() storage.RunStore {
	return s.base.Runs()
}

func (s *txCountingStore) Checkpoints() storage.CheckpointStore {
	return s.base.Checkpoints()
}

func (s *txCountingStore) RecoveryActions() storage.RecoveryActionStore {
	return s.base.RecoveryActions()
}

func (s *txCountingStore) ContextPacks() storage.ContextPackStore {
	return s.base.ContextPacks()
}

func (s *txCountingStore) TaskMemories() storage.TaskMemoryStore {
	return s.base.TaskMemories()
}

func (s *txCountingStore) Benchmarks() storage.BenchmarkStore {
	return s.base.Benchmarks()
}

func (s *txCountingStore) PolicyDecisions() storage.PolicyDecisionStore {
	return s.base.PolicyDecisions()
}

func (s *txCountingStore) WithTx(fn func(storage.Store) error) error {
	s.withTxCount++
	return s.base.WithTx(fn)
}

type faultProofStore struct {
	base storage.ProofStore
}

func (s *faultProofStore) Append(event proof.Event) error {
	return errors.New("forced proof append failure")
}

func (s *faultProofStore) ListByTask(taskID common.TaskID, limit int) ([]proof.Event, error) {
	return s.base.ListByTask(taskID, limit)
}

func TestExecutePrimaryOperatorStepStartRunRecordsDurableReceipt(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	out, err := coord.ExecutePrimaryOperatorStep(context.Background(), ExecutePrimaryOperatorStepRequest{TaskID: string(taskID)})
	if err != nil {
		t.Fatalf("execute primary operator step: %v", err)
	}
	if out.Receipt.ActionHandle != string(OperatorActionStartLocalRun) || out.Receipt.ResultClass != operatorstep.ResultSucceeded {
		t.Fatalf("unexpected receipt: %+v", out.Receipt)
	}
	if out.Receipt.RunID == "" {
		t.Fatalf("expected run target on receipt, got %+v", out.Receipt)
	}
	if len(out.RecentOperatorStepReceipts) == 0 || out.RecentOperatorStepReceipts[0].ReceiptID != out.Receipt.ReceiptID {
		t.Fatalf("expected latest receipt in recent history, got %+v", out.RecentOperatorStepReceipts)
	}
}

func TestExecutePrimaryOperatorStepInterruptedResumeAdvancesToContinueRecovery(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	startOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "interrupt", RunID: startOut.RunID, InterruptionReason: "operator next test"}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}

	out, err := coord.ExecutePrimaryOperatorStep(context.Background(), ExecutePrimaryOperatorStepRequest{TaskID: string(taskID)})
	if err != nil {
		t.Fatalf("execute primary operator step: %v", err)
	}
	if out.Receipt.ActionHandle != string(OperatorActionResumeInterruptedLineage) || out.Receipt.ResultClass != operatorstep.ResultSucceeded {
		t.Fatalf("unexpected receipt: %+v", out.Receipt)
	}
	if out.OperatorExecutionPlan.PrimaryStep == nil || out.OperatorExecutionPlan.PrimaryStep.Action != OperatorActionFinalizeContinueRecovery {
		t.Fatalf("expected continue recovery next, got %+v", out.OperatorExecutionPlan)
	}
}

func TestExecutePrimaryOperatorStepContinueRecoveryAdvancesToFreshRun(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	startOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "interrupt", RunID: startOut.RunID, InterruptionReason: "operator next test"}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}
	if _, err := coord.ExecuteInterruptedResume(context.Background(), ExecuteInterruptedResumeRequest{TaskID: string(taskID)}); err != nil {
		t.Fatalf("resume interrupted lineage: %v", err)
	}

	out, err := coord.ExecutePrimaryOperatorStep(context.Background(), ExecutePrimaryOperatorStepRequest{TaskID: string(taskID)})
	if err != nil {
		t.Fatalf("execute primary operator step: %v", err)
	}
	if out.Receipt.ActionHandle != string(OperatorActionFinalizeContinueRecovery) || out.Receipt.ResultClass != operatorstep.ResultSucceeded {
		t.Fatalf("unexpected receipt: %+v", out.Receipt)
	}
	if out.OperatorExecutionPlan.PrimaryStep == nil || out.OperatorExecutionPlan.PrimaryStep.Action != OperatorActionStartLocalRun {
		t.Fatalf("expected start-local-run next, got %+v", out.OperatorExecutionPlan)
	}
}

func TestExecutePrimaryOperatorStepAcceptedHandoffLaunchRecordsReceipt(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.CreateCheckpoint(context.Background(), string(taskID)); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{TaskID: string(taskID), TargetWorker: rundomain.WorkerKindClaude, Reason: "operator next launch", Mode: handoff.ModeResume})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{TaskID: string(taskID), HandoffID: createOut.HandoffID, AcceptedBy: rundomain.WorkerKindClaude}); err != nil {
		t.Fatalf("accept handoff: %v", err)
	}

	out, err := coord.ExecutePrimaryOperatorStep(context.Background(), ExecutePrimaryOperatorStepRequest{TaskID: string(taskID)})
	if err != nil {
		t.Fatalf("execute primary operator step: %v", err)
	}
	if out.Receipt.ActionHandle != string(OperatorActionLaunchAcceptedHandoff) || out.Receipt.ResultClass != operatorstep.ResultSucceeded {
		t.Fatalf("unexpected receipt: %+v", out.Receipt)
	}
	if out.ActiveBranch.Class != ActiveBranchClassHandoffClaude {
		t.Fatalf("expected launched Claude branch to remain active, got %+v", out.ActiveBranch)
	}
	if strings.Contains(strings.ToLower(out.CanonicalResponse), "completed coding") {
		t.Fatalf("launch response overclaimed downstream completion: %q", out.CanonicalResponse)
	}
}

func TestExecutePrimaryOperatorStepResolveActiveHandoffReturnsToLocalOwnership(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.CreateCheckpoint(context.Background(), string(taskID)); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{TaskID: string(taskID), TargetWorker: rundomain.WorkerKindClaude, Reason: "operator next resolve", Mode: handoff.ModeResume})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{TaskID: string(taskID), HandoffID: createOut.HandoffID, AcceptedBy: rundomain.WorkerKindClaude}); err != nil {
		t.Fatalf("accept handoff: %v", err)
	}
	if _, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{TaskID: string(taskID), HandoffID: createOut.HandoffID}); err != nil {
		t.Fatalf("launch handoff: %v", err)
	}

	out, err := coord.ExecutePrimaryOperatorStep(context.Background(), ExecutePrimaryOperatorStepRequest{TaskID: string(taskID)})
	if err != nil {
		t.Fatalf("execute primary operator step: %v", err)
	}
	if out.Receipt.ActionHandle != string(OperatorActionResolveActiveHandoff) || out.Receipt.ResultClass != operatorstep.ResultSucceeded {
		t.Fatalf("unexpected receipt: %+v", out.Receipt)
	}
	if out.ActiveBranch.Class != ActiveBranchClassLocal {
		t.Fatalf("expected local branch ownership after resolution, got %+v", out.ActiveBranch)
	}
}

func TestExecutePrimaryOperatorStepInspectFallbackRecordsRejectedReceipt(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.CreateCheckpoint(context.Background(), string(taskID)); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{TaskID: string(taskID), TargetWorker: rundomain.WorkerKindClaude, Reason: "operator next inspect fallback", Mode: handoff.ModeResume})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{TaskID: string(taskID), HandoffID: createOut.HandoffID, AcceptedBy: rundomain.WorkerKindClaude}); err != nil {
		t.Fatalf("accept handoff: %v", err)
	}
	if _, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{TaskID: string(taskID), HandoffID: createOut.HandoffID}); err != nil {
		t.Fatalf("launch handoff: %v", err)
	}
	if _, err := coord.RecordHandoffFollowThrough(context.Background(), RecordHandoffFollowThroughRequest{TaskID: string(taskID), Kind: handoff.FollowThroughStalledReviewRequired}); err != nil {
		t.Fatalf("record stalled follow-through: %v", err)
	}

	out, err := coord.ExecutePrimaryOperatorStep(context.Background(), ExecutePrimaryOperatorStepRequest{TaskID: string(taskID)})
	if err != nil {
		t.Fatalf("execute primary operator step: %v", err)
	}
	if out.Receipt.ActionHandle != string(OperatorActionReviewHandoffFollowUp) || out.Receipt.ResultClass != operatorstep.ResultRejected || out.Receipt.ExecutionAttempted {
		t.Fatalf("expected rejected non-executable receipt, got %+v", out.Receipt)
	}
}

func TestOperatorStepReceiptHistorySurfacesInInspectAndShellSnapshot(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	startOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "interrupt", RunID: startOut.RunID, InterruptionReason: "history transport"}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}
	first, err := coord.ExecutePrimaryOperatorStep(context.Background(), ExecutePrimaryOperatorStepRequest{TaskID: string(taskID)})
	if err != nil {
		t.Fatalf("execute interrupted resume: %v", err)
	}
	second, err := coord.ExecutePrimaryOperatorStep(context.Background(), ExecutePrimaryOperatorStepRequest{TaskID: string(taskID)})
	if err != nil {
		t.Fatalf("execute continue recovery: %v", err)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.LatestOperatorStepReceipt == nil || inspectOut.LatestOperatorStepReceipt.ReceiptID != second.Receipt.ReceiptID {
		t.Fatalf("expected latest inspect receipt %s, got %+v", second.Receipt.ReceiptID, inspectOut.LatestOperatorStepReceipt)
	}
	if len(inspectOut.RecentOperatorStepReceipts) < 2 || inspectOut.RecentOperatorStepReceipts[1].ReceiptID != first.Receipt.ReceiptID {
		t.Fatalf("expected inspect recent receipt history, got %+v", inspectOut.RecentOperatorStepReceipts)
	}

	shellOut, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if shellOut.LatestOperatorStepReceipt == nil || shellOut.LatestOperatorStepReceipt.ReceiptID != second.Receipt.ReceiptID {
		t.Fatalf("expected latest shell receipt %s, got %+v", second.Receipt.ReceiptID, shellOut.LatestOperatorStepReceipt)
	}
	if len(shellOut.RecentOperatorStepReceipts) < 2 || shellOut.RecentOperatorStepReceipts[1].ReceiptID != first.Receipt.ReceiptID {
		t.Fatalf("expected shell receipt history, got %+v", shellOut.RecentOperatorStepReceipts)
	}
}

func TestLaunchDispatchFromResultMarksReusedLaunchAsNoop(t *testing.T) {
	continuity := HandoffContinuity{HandoffID: "hnd_123", LaunchID: "launch_123"}
	dispatch := launchDispatchFromResult(continuity, LaunchHandoffResult{HandoffID: "hnd_123", LaunchID: "launch_123", LaunchStatus: HandoffLaunchStatusCompleted, CanonicalResponse: "reused launch"})
	if dispatch.resultClass != operatorstep.ResultNoopReused {
		t.Fatalf("expected noop reused launch classification, got %+v", dispatch)
	}
}

func TestStatusTaskSurfacesLatestOperatorStepReceipt(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	result, err := coord.ExecutePrimaryOperatorStep(context.Background(), ExecutePrimaryOperatorStepRequest{TaskID: string(taskID)})
	if err != nil {
		t.Fatalf("execute primary operator step: %v", err)
	}
	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.LatestOperatorStepReceipt == nil || status.LatestOperatorStepReceipt.ReceiptID != result.Receipt.ReceiptID {
		t.Fatalf("expected latest status receipt %s, got %+v", result.Receipt.ReceiptID, status.LatestOperatorStepReceipt)
	}
}
