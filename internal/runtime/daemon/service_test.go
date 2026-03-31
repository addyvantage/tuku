package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tuku/internal/domain/benchmark"
	"tuku/internal/domain/brief"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/conversation"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/incidenttriage"
	"tuku/internal/domain/operatorstep"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/promptir"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/recoveryaction"
	"tuku/internal/domain/run"
	"tuku/internal/domain/shellsession"
	"tuku/internal/domain/taskmemory"
	"tuku/internal/domain/transition"
	anchorgit "tuku/internal/git/anchor"
	"tuku/internal/ipc"
	"tuku/internal/orchestrator"
	"tuku/internal/response/canonical"
	"tuku/internal/storage/sqlite"
)

func TestHandleRequestCreateHandoffRoute(t *testing.T) {
	var captured orchestrator.CreateHandoffRequest
	handler := &fakeOrchestratorService{
		createHandoffFn: func(_ context.Context, req orchestrator.CreateHandoffRequest) (orchestrator.CreateHandoffResult, error) {
			captured = req
			return orchestrator.CreateHandoffResult{
				TaskID:            common.TaskID(req.TaskID),
				HandoffID:         "hnd_test",
				SourceWorker:      run.WorkerKindCodex,
				TargetWorker:      req.TargetWorker,
				Status:            handoff.StatusCreated,
				CheckpointID:      common.CheckpointID("chk_test"),
				BriefID:           common.BriefID("brf_test"),
				CanonicalResponse: "handoff created",
				Packet: &handoff.Packet{
					Version:      1,
					HandoffID:    "hnd_test",
					TaskID:       common.TaskID(req.TaskID),
					Status:       handoff.StatusCreated,
					TargetWorker: req.TargetWorker,
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskHandoffCreateRequest{
		TaskID:       common.TaskID("tsk_123"),
		TargetWorker: run.WorkerKindClaude,
		Reason:       "manual test",
		Mode:         handoff.ModeResume,
		Notes:        []string{"note"},
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_1",
		Method:    ipc.MethodCreateHandoff,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_123" {
		t.Fatalf("expected captured task id tsk_123, got %s", captured.TaskID)
	}
	if captured.TargetWorker != run.WorkerKindClaude {
		t.Fatalf("expected target worker claude, got %s", captured.TargetWorker)
	}
	var out ipc.TaskHandoffCreateResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.HandoffID != "hnd_test" {
		t.Fatalf("expected handoff id hnd_test, got %s", out.HandoffID)
	}
	if out.Status != string(handoff.StatusCreated) {
		t.Fatalf("expected status CREATED, got %s", out.Status)
	}
}

func TestHandleRequestResolveShellTaskForRepoRoute(t *testing.T) {
	var capturedRepoRoot string
	var capturedGoal string
	handler := &fakeOrchestratorService{
		resolveShellTaskForRepoFn: func(_ context.Context, repoRoot string, defaultGoal string) (orchestrator.ResolveShellTaskResult, error) {
			capturedRepoRoot = repoRoot
			capturedGoal = defaultGoal
			return orchestrator.ResolveShellTaskResult{
				TaskID:   common.TaskID("tsk_repo"),
				RepoRoot: repoRoot,
				Created:  true,
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.ResolveShellTaskForRepoRequest{
		RepoRoot:    "/tmp/repo",
		DefaultGoal: "Continue work in this repository",
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_repo",
		Method:    ipc.MethodResolveShellTaskForRepo,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if capturedRepoRoot != "/tmp/repo" || capturedGoal != "Continue work in this repository" {
		t.Fatalf("unexpected resolve-shell-task request: repo=%q goal=%q", capturedRepoRoot, capturedGoal)
	}
	var out ipc.ResolveShellTaskForRepoResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal resolve shell task response: %v", err)
	}
	if out.TaskID != "tsk_repo" || !out.Created {
		t.Fatalf("unexpected resolve shell task response: %+v", out)
	}
}

func TestHandleRequestAcceptHandoffRoute(t *testing.T) {
	var captured orchestrator.AcceptHandoffRequest
	handler := &fakeOrchestratorService{
		acceptHandoffFn: func(_ context.Context, req orchestrator.AcceptHandoffRequest) (orchestrator.AcceptHandoffResult, error) {
			captured = req
			return orchestrator.AcceptHandoffResult{
				TaskID:            common.TaskID(req.TaskID),
				HandoffID:         req.HandoffID,
				Status:            handoff.StatusAccepted,
				CanonicalResponse: "handoff accepted",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskHandoffAcceptRequest{
		TaskID:     common.TaskID("tsk_123"),
		HandoffID:  "hnd_abc",
		AcceptedBy: run.WorkerKindClaude,
		Notes:      []string{"accepted"},
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_2",
		Method:    ipc.MethodAcceptHandoff,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_123" || captured.HandoffID != "hnd_abc" {
		t.Fatalf("unexpected captured accept request: %+v", captured)
	}
	var out ipc.TaskHandoffAcceptResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.Status != string(handoff.StatusAccepted) {
		t.Fatalf("expected status ACCEPTED, got %s", out.Status)
	}
}

func TestHandleRequestShellSnapshotRoute(t *testing.T) {
	handler := &fakeOrchestratorService{
		shellSnapshotFn: func(_ context.Context, taskID string) (orchestrator.ShellSnapshotResult, error) {
			if taskID != "tsk_123" {
				t.Fatalf("unexpected task id %s", taskID)
			}
			return orchestrator.ShellSnapshotResult{
				TaskID:        common.TaskID(taskID),
				Goal:          "Shell milestone",
				Phase:         "BRIEF_READY",
				Status:        "ACTIVE",
				IntentClass:   "implement",
				IntentSummary: "implement: wire shell",
				CompiledIntent: &orchestrator.CompiledIntentSummary{
					IntentID:                "int_shell_1",
					Class:                   "IMPLEMENT_CHANGE",
					Posture:                 "EXECUTION_READY",
					ExecutionReadiness:      "EXECUTION_READY",
					Objective:               "Wire shell snapshot parity",
					RequiresClarification:   false,
					BoundedEvidenceMessages: 4,
					Digest:                  "execution-ready intent in bounded recent evidence",
					Advisory:                "Intent appears execution-ready within bounded recent evidence.",
				},
				ActiveBranch: &orchestrator.ShellActiveBranchSummary{
					Class:                  orchestrator.ActiveBranchClassHandoffClaude,
					BranchRef:              "hnd_1",
					ActionabilityAnchor:    orchestrator.ActiveBranchAnchorKindHandoff,
					ActionabilityAnchorRef: "hnd_1",
					Reason:                 "accepted Claude handoff branch currently owns continuity",
				},
				LocalRunFinalization: &orchestrator.ShellLocalRunFinalizationSummary{
					State:     orchestrator.LocalRunFinalizationStaleReconciliationNeeded,
					RunID:     common.RunID("run_1"),
					RunStatus: run.StatusRunning,
					Reason:    "latest run is still durably RUNNING and requires explicit stale reconciliation",
				},
				LocalResume: &orchestrator.ShellLocalResumeAuthoritySummary{
					State:               orchestrator.LocalResumeAuthorityBlocked,
					Mode:                orchestrator.LocalResumeModeNone,
					BlockingBranchClass: orchestrator.ActiveBranchClassHandoffClaude,
					BlockingBranchRef:   "hnd_1",
					Reason:              "local interrupted-lineage resume is blocked while Claude handoff branch hnd_1 owns continuity",
				},
				ActionAuthority: &orchestrator.ShellOperatorActionAuthoritySet{
					RequiredNextAction: orchestrator.OperatorActionReviewHandoffFollowUp,
					Actions: []orchestrator.ShellOperatorActionAuthority{
						{Action: orchestrator.OperatorActionLocalMessageMutation, State: orchestrator.OperatorActionAuthorityBlocked, BlockingBranchClass: orchestrator.ActiveBranchClassHandoffClaude, BlockingBranchRef: "hnd_1", Reason: "Cannot send a local task message while launched Claude handoff hnd_1 remains the active continuity branch."},
						{Action: orchestrator.OperatorActionReviewHandoffFollowUp, State: orchestrator.OperatorActionAuthorityRequiredNext, Reason: "launched handoff follow-through appears stalled and requires review"},
					},
				},
				OperatorExecutionPlan: &orchestrator.ShellOperatorExecutionPlan{
					PrimaryStep: &orchestrator.ShellOperatorExecutionStep{
						Action:      orchestrator.OperatorActionReviewHandoffFollowUp,
						Status:      orchestrator.OperatorActionAuthorityRequiredNext,
						Domain:      orchestrator.OperatorExecutionDomainReview,
						CommandHint: "tuku inspect --task tsk_123",
						Reason:      "launched handoff follow-through appears stalled and requires review",
					},
					MandatoryBeforeProgress: true,
					BlockedSteps: []orchestrator.ShellOperatorExecutionStep{
						{Action: orchestrator.OperatorActionLocalMessageMutation, Status: orchestrator.OperatorActionAuthorityBlocked, Domain: orchestrator.OperatorExecutionDomainLocal, CommandHint: "tuku message --task tsk_123 --text \"<message>\"", Reason: "Cannot send a local task message while launched Claude handoff hnd_1 remains the active continuity branch."},
					},
				},
				LaunchControl: &orchestrator.ShellLaunchControlSummary{
					State:            orchestrator.LaunchControlStateCompleted,
					RetryDisposition: orchestrator.LaunchRetryDispositionBlocked,
					Reason:           "launcher invocation completed; downstream continuation remains unproven",
					HandoffID:        "hnd_1",
					AttemptID:        "hlc_1",
					LaunchID:         "launch_1",
				},
				HandoffContinuity: &orchestrator.ShellHandoffContinuitySummary{
					State:                        orchestrator.HandoffContinuityStateFollowThroughProofOfLife,
					Reason:                       "Claude handoff launch has downstream proof-of-life evidence, but downstream completion remains unproven",
					LaunchAttemptID:              "hlc_1",
					FollowThroughID:              "hft_1",
					FollowThroughKind:            handoff.FollowThroughProofOfLifeObserved,
					FollowThroughSummary:         "later Claude proof of life observed",
					DownstreamContinuationProven: true,
				},
				FollowThrough: &orchestrator.ShellFollowThroughSummary{
					RecordID:        "hft_1",
					Kind:            handoff.FollowThroughProofOfLifeObserved,
					Summary:         "later Claude proof of life observed",
					LaunchAttemptID: "hlc_1",
					LaunchID:        "launch_1",
					CreatedAt:       time.Unix(1710000300, 0).UTC(),
				},
				Resolution: &orchestrator.ShellResolutionSummary{
					ResolutionID:    "hrs_1",
					Kind:            handoff.ResolutionSupersededByLocal,
					Summary:         "operator returned local control",
					LaunchAttemptID: "hlc_1",
					LaunchID:        "launch_1",
					CreatedAt:       time.Unix(1710000400, 0).UTC(),
				},
				Recovery: &orchestrator.ShellRecoverySummary{
					ContinuityOutcome: orchestrator.ContinueOutcomeSafe,
					RecoveryClass:     orchestrator.RecoveryClassHandoffLaunchCompleted,
					RecommendedAction: orchestrator.RecoveryActionMonitorLaunchedHandoff,
					ReadyForNextRun:   false,
					Issues: []orchestrator.ShellRecoveryIssue{
						{Code: "HANDOFF_MONITORING", Message: "downstream completion remains unproven"},
					},
				},
				LatestContinuityTransitionReceipt: &orchestrator.ContinuityTransitionReceiptSummary{
					ReceiptID:             "ctr_shell_1",
					TaskID:                common.TaskID(taskID),
					TransitionKind:        transition.KindHandoffLaunch,
					HandoffStateBefore:    orchestrator.HandoffContinuityStateAcceptedNotLaunched,
					HandoffStateAfter:     orchestrator.HandoffContinuityStateLaunchCompletedAckSeen,
					ReviewGapPresent:      true,
					ReviewPosture:         transition.ReviewPostureGlobalReviewStale,
					AcknowledgmentPresent: false,
					Summary:               "handoff launch occurred while transcript review remained stale",
					CreatedAt:             time.Unix(1710000500, 0).UTC(),
				},
				RecentContinuityTransitionReceipts: []orchestrator.ContinuityTransitionReceiptSummary{
					{
						ReceiptID:             "ctr_shell_1",
						TaskID:                common.TaskID(taskID),
						TransitionKind:        transition.KindHandoffLaunch,
						HandoffStateBefore:    orchestrator.HandoffContinuityStateAcceptedNotLaunched,
						HandoffStateAfter:     orchestrator.HandoffContinuityStateLaunchCompletedAckSeen,
						ReviewGapPresent:      true,
						ReviewPosture:         transition.ReviewPostureGlobalReviewStale,
						AcknowledgmentPresent: false,
						Summary:               "handoff launch occurred while transcript review remained stale",
						CreatedAt:             time.Unix(1710000500, 0).UTC(),
					},
					{
						ReceiptID:             "ctr_shell_0",
						TaskID:                common.TaskID(taskID),
						TransitionKind:        transition.KindHandoffResolution,
						HandoffStateBefore:    orchestrator.HandoffContinuityStateFollowThroughStalled,
						HandoffStateAfter:     orchestrator.HandoffContinuityStateResolved,
						ReviewGapPresent:      false,
						ReviewPosture:         transition.ReviewPostureGlobalReviewCurrent,
						AcknowledgmentPresent: false,
						Summary:               "handoff resolution recorded with current retained transcript review",
						CreatedAt:             time.Unix(1710000600, 0).UTC(),
					},
				},
				RecentProofs: []orchestrator.ShellProofSummary{
					{EventID: "evt_1", Type: proof.EventBriefCreated, Summary: "Execution brief updated"},
				},
				ContinuityIncidentTaskRisk: &orchestrator.ContinuityIncidentTaskRiskSummary{
					Class:                               orchestrator.ContinuityIncidentTaskRiskRecurringWeakClosure,
					Digest:                              "recurring continuity weakness in recent bounded evidence",
					WindowAdvisory:                      "bounded incident window anchors=3 open=1 reopened=2 triaged-without-follow-up=0 stagnant=0",
					Detail:                              "Recent bounded evidence suggests recurring weak closure posture across incidents.",
					DistinctAnchors:                     3,
					RecurringWeakClosure:                true,
					ReopenedAfterClosureAnchors:         2,
					RepeatedReopenLoopAnchors:           1,
					OperationallyUnresolvedAnchorSignal: 3,
					RecentAnchorClasses: []orchestrator.ContinuityIncidentClosureClass{
						orchestrator.ContinuityIncidentClosureWeakReopened,
						orchestrator.ContinuityIncidentClosureWeakLoop,
					},
				},
				RecentConversation: []orchestrator.ShellConversationSummary{
					{Role: conversation.RoleSystem, Body: "Canonical shell response"},
				},
				LatestCanonicalResponse: "Canonical shell response",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskShellSnapshotRequest{TaskID: common.TaskID("tsk_123")})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_3",
		Method:    ipc.MethodTaskShellSnapshot,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	var out ipc.TaskShellSnapshotResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal shell snapshot response: %v", err)
	}
	if out.TaskID != "tsk_123" {
		t.Fatalf("expected task id tsk_123, got %s", out.TaskID)
	}
	if out.CompiledIntent == nil || out.CompiledIntent.IntentID != "int_shell_1" || out.CompiledIntent.ExecutionReadiness != "EXECUTION_READY" {
		t.Fatalf("expected compiled intent mapping in shell snapshot response, got %+v", out.CompiledIntent)
	}
	if out.LatestCanonicalResponse != "Canonical shell response" {
		t.Fatalf("unexpected canonical response %q", out.LatestCanonicalResponse)
	}
	if len(out.RecentProofs) != 1 {
		t.Fatalf("expected one proof item, got %d", len(out.RecentProofs))
	}
	if out.LaunchControl == nil || out.LaunchControl.State != string(orchestrator.LaunchControlStateCompleted) {
		t.Fatalf("expected launch control state mapping, got %+v", out.LaunchControl)
	}
	if out.HandoffContinuity == nil || out.HandoffContinuity.State != string(orchestrator.HandoffContinuityStateFollowThroughProofOfLife) {
		t.Fatalf("expected handoff continuity mapping, got %+v", out.HandoffContinuity)
	}
	if out.HandoffContinuity.FollowThroughKind != string(handoff.FollowThroughProofOfLifeObserved) {
		t.Fatalf("expected follow-through continuity mapping, got %+v", out.HandoffContinuity)
	}
	if out.ActiveBranch == nil || out.ActiveBranch.Class != string(orchestrator.ActiveBranchClassHandoffClaude) || out.ActiveBranch.BranchRef != "hnd_1" {
		t.Fatalf("expected shell active branch mapping, got %+v", out.ActiveBranch)
	}
	if out.LocalRunFinalization == nil || out.LocalRunFinalization.State != string(orchestrator.LocalRunFinalizationStaleReconciliationNeeded) {
		t.Fatalf("expected shell local run finalization mapping, got %+v", out.LocalRunFinalization)
	}
	if out.LocalResume == nil || out.LocalResume.State != string(orchestrator.LocalResumeAuthorityBlocked) || out.LocalResume.BlockingBranchRef != "hnd_1" {
		t.Fatalf("expected shell local resume mapping, got %+v", out.LocalResume)
	}
	if out.ActionAuthority == nil || out.ActionAuthority.RequiredNextAction != string(orchestrator.OperatorActionReviewHandoffFollowUp) {
		t.Fatalf("expected shell action authority mapping, got %+v", out.ActionAuthority)
	}
	if out.OperatorExecutionPlan == nil || out.OperatorExecutionPlan.PrimaryStep == nil {
		t.Fatalf("expected shell execution plan mapping, got %+v", out.OperatorExecutionPlan)
	}
	if out.OperatorExecutionPlan.PrimaryStep.Action != string(orchestrator.OperatorActionReviewHandoffFollowUp) || out.OperatorExecutionPlan.PrimaryStep.CommandHint != "tuku inspect --task tsk_123" {
		t.Fatalf("expected shell execution plan primary step mapping, got %+v", out.OperatorExecutionPlan.PrimaryStep)
	}
	if out.FollowThrough == nil || out.FollowThrough.Kind != string(handoff.FollowThroughProofOfLifeObserved) {
		t.Fatalf("expected shell follow-through payload mapping, got %+v", out.FollowThrough)
	}
	if out.Resolution == nil || out.Resolution.Kind != string(handoff.ResolutionSupersededByLocal) {
		t.Fatalf("expected shell resolution payload mapping, got %+v", out.Resolution)
	}
	if out.Recovery == nil || out.Recovery.RecoveryClass != string(orchestrator.RecoveryClassHandoffLaunchCompleted) {
		t.Fatalf("expected recovery mapping, got %+v", out.Recovery)
	}
	if len(out.Recovery.Issues) != 1 {
		t.Fatalf("expected one recovery issue, got %+v", out.Recovery)
	}
	if out.LatestContinuityTransitionReceipt == nil || out.LatestContinuityTransitionReceipt.ReceiptID != "ctr_shell_1" {
		t.Fatalf("expected latest continuity transition receipt mapping, got %+v", out.LatestContinuityTransitionReceipt)
	}
	if out.LatestContinuityTransitionReceipt.TransitionKind != string(transition.KindHandoffLaunch) || out.LatestContinuityTransitionReceipt.ReviewPosture != string(transition.ReviewPostureGlobalReviewStale) {
		t.Fatalf("expected latest transition review posture mapping, got %+v", out.LatestContinuityTransitionReceipt)
	}
	if len(out.RecentContinuityTransitionReceipts) != 2 {
		t.Fatalf("expected recent continuity transition receipt history mapping, got %+v", out.RecentContinuityTransitionReceipts)
	}
	if out.ContinuityIncidentTaskRisk == nil || out.ContinuityIncidentTaskRisk.Class != string(orchestrator.ContinuityIncidentTaskRiskRecurringWeakClosure) {
		t.Fatalf("expected shell incident task-risk mapping, got %+v", out.ContinuityIncidentTaskRisk)
	}
}

func TestHandleRequestRecordRecoveryActionRoute(t *testing.T) {
	var captured orchestrator.RecordRecoveryActionRequest
	handler := &fakeOrchestratorService{
		recordRecoveryActionFn: func(_ context.Context, req orchestrator.RecordRecoveryActionRequest) (orchestrator.RecordRecoveryActionResult, error) {
			captured = req
			return orchestrator.RecordRecoveryActionResult{
				TaskID: common.TaskID(req.TaskID),
				Action: recoveryaction.Record{
					Version:   1,
					ActionID:  "ract_1",
					TaskID:    common.TaskID(req.TaskID),
					Kind:      req.Kind,
					Summary:   req.Summary,
					Notes:     append([]string{}, req.Notes...),
					CreatedAt: time.Unix(1710000000, 0).UTC(),
				},
				RecoveryClass:         orchestrator.RecoveryClassDecisionRequired,
				RecommendedAction:     orchestrator.RecoveryActionMakeResumeDecision,
				ReadyForNextRun:       false,
				ReadyForHandoffLaunch: false,
				RecoveryReason:        "failed run reviewed; choose next step",
				CanonicalResponse:     "recovery action recorded",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskRecordRecoveryActionRequest{
		TaskID:  common.TaskID("tsk_123"),
		Kind:    string(recoveryaction.KindFailedRunReviewed),
		Summary: "reviewed failed run",
		Notes:   []string{"operator reviewed logs"},
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_recovery_1",
		Method:    ipc.MethodRecordRecoveryAction,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_123" || captured.Kind != recoveryaction.KindFailedRunReviewed {
		t.Fatalf("unexpected recovery action request: %+v", captured)
	}
	var out ipc.TaskRecordRecoveryActionResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal recovery action response: %v", err)
	}
	if out.Action.ActionID != "ract_1" || out.RecoveryClass != string(orchestrator.RecoveryClassDecisionRequired) {
		t.Fatalf("unexpected recovery action response: %+v", out)
	}
}

func TestHandleRequestReviewInterruptedRunRoute(t *testing.T) {
	var captured orchestrator.RecordRecoveryActionRequest
	handler := &fakeOrchestratorService{
		recordRecoveryActionFn: func(_ context.Context, req orchestrator.RecordRecoveryActionRequest) (orchestrator.RecordRecoveryActionResult, error) {
			captured = req
			return orchestrator.RecordRecoveryActionResult{
				TaskID: common.TaskID(req.TaskID),
				Action: recoveryaction.Record{
					Version:      1,
					ActionID:     "ract_interrupt_1",
					TaskID:       common.TaskID(req.TaskID),
					Kind:         req.Kind,
					RunID:        common.RunID("run_123"),
					CheckpointID: common.CheckpointID("chk_123"),
					Summary:      req.Summary,
					Notes:        append([]string{}, req.Notes...),
					CreatedAt:    time.Unix(1710000001, 0).UTC(),
				},
				RecoveryClass:         orchestrator.RecoveryClassInterruptedRunRecoverable,
				RecommendedAction:     orchestrator.RecoveryActionResumeInterrupted,
				ReadyForNextRun:       false,
				ReadyForHandoffLaunch: false,
				RecoveryReason:        "interrupted execution lineage was reviewed and remains recoverable",
				CanonicalResponse:     "interrupted run reviewed",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskReviewInterruptedRunRequest{
		TaskID:  common.TaskID("tsk_123"),
		Summary: "interrupted lineage reviewed",
		Notes:   []string{"preserve interrupted lineage"},
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_review_interrupt",
		Method:    ipc.MethodReviewInterruptedRun,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_123" || captured.Kind != recoveryaction.KindInterruptedRunReviewed {
		t.Fatalf("unexpected captured interrupted review request: %+v", captured)
	}
	if len(captured.Notes) != 1 || captured.Notes[0] != "preserve interrupted lineage" {
		t.Fatalf("unexpected captured interrupted review notes: %+v", captured.Notes)
	}
	var out ipc.TaskRecordRecoveryActionResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.Action.Kind != string(recoveryaction.KindInterruptedRunReviewed) {
		t.Fatalf("expected interrupted review action kind, got %s", out.Action.Kind)
	}
	if out.ReadyForNextRun {
		t.Fatal("interrupted review response must not claim next-run readiness")
	}
}

func TestHandleRequestExecuteRebriefRoute(t *testing.T) {
	var captured orchestrator.ExecuteRebriefRequest
	handler := &fakeOrchestratorService{
		executeRebriefFn: func(_ context.Context, req orchestrator.ExecuteRebriefRequest) (orchestrator.ExecuteRebriefResult, error) {
			captured = req
			return orchestrator.ExecuteRebriefResult{
				TaskID:                common.TaskID(req.TaskID),
				PreviousBriefID:       common.BriefID("brf_old"),
				BriefID:               common.BriefID("brf_new"),
				BriefHash:             "hash_new",
				RecoveryClass:         orchestrator.RecoveryClassReadyNextRun,
				RecommendedAction:     orchestrator.RecoveryActionStartNextRun,
				ReadyForNextRun:       true,
				ReadyForHandoffLaunch: false,
				RecoveryReason:        "execution brief was regenerated after operator decision",
				CanonicalResponse:     "rebrief executed",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskRebriefRequest{TaskID: common.TaskID("tsk_456")})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_rebrief_1",
		Method:    ipc.MethodExecuteRebrief,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_456" {
		t.Fatalf("unexpected rebrief request: %+v", captured)
	}
	var out ipc.TaskRebriefResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal rebrief response: %v", err)
	}
	if out.BriefID != "brf_new" || !out.ReadyForNextRun {
		t.Fatalf("unexpected rebrief response: %+v", out)
	}
}

func TestHandleRequestExecuteInterruptedResumeRoute(t *testing.T) {
	var captured orchestrator.ExecuteInterruptedResumeRequest
	handler := &fakeOrchestratorService{
		executeInterruptedResumeFn: func(_ context.Context, req orchestrator.ExecuteInterruptedResumeRequest) (orchestrator.ExecuteInterruptedResumeResult, error) {
			captured = req
			return orchestrator.ExecuteInterruptedResumeResult{
				TaskID:                common.TaskID(req.TaskID),
				BriefID:               common.BriefID("brf_current"),
				BriefHash:             "hash_current",
				Action:                recoveryaction.Record{ActionID: "ract_resume_interrupt_1", TaskID: common.TaskID(req.TaskID), Kind: recoveryaction.KindInterruptedResumeExecuted, Summary: "operator resumed interrupted lineage"},
				RecoveryClass:         orchestrator.RecoveryClassContinueExecutionRequired,
				RecommendedAction:     orchestrator.RecoveryActionExecuteContinueRecovery,
				ReadyForNextRun:       false,
				ReadyForHandoffLaunch: false,
				RecoveryReason:        "operator explicitly resumed interrupted execution lineage; continue recovery still must be executed before the next bounded run",
				CanonicalResponse:     "interrupted resume executed",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskInterruptedResumeRequest{
		TaskID:  common.TaskID("tsk_interrupt_resume"),
		Summary: "operator resumed interrupted lineage",
		Notes:   []string{"maintain interrupted lineage semantics"},
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_interrupt_resume_1",
		Method:    ipc.MethodExecuteInterruptedResume,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_interrupt_resume" || captured.Summary != "operator resumed interrupted lineage" {
		t.Fatalf("unexpected interrupted resume request: %+v", captured)
	}
	if len(captured.Notes) != 1 || captured.Notes[0] != "maintain interrupted lineage semantics" {
		t.Fatalf("unexpected interrupted resume notes: %+v", captured.Notes)
	}
	var out ipc.TaskInterruptedResumeResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal interrupted resume response: %v", err)
	}
	if out.BriefID != "brf_current" || out.Action.Kind != string(recoveryaction.KindInterruptedResumeExecuted) {
		t.Fatalf("unexpected interrupted resume response: %+v", out)
	}
	if out.ReadyForNextRun {
		t.Fatal("interrupted resume response must not claim fresh next-run readiness")
	}
}

func TestHandleRequestExecuteContinueRecoveryRoute(t *testing.T) {
	var captured orchestrator.ExecuteContinueRecoveryRequest
	handler := &fakeOrchestratorService{
		executeContinueRecoveryFn: func(_ context.Context, req orchestrator.ExecuteContinueRecoveryRequest) (orchestrator.ExecuteContinueRecoveryResult, error) {
			captured = req
			return orchestrator.ExecuteContinueRecoveryResult{
				TaskID:                common.TaskID(req.TaskID),
				BriefID:               common.BriefID("brf_current"),
				BriefHash:             "hash_current",
				Action:                recoveryaction.Record{ActionID: "ract_continue_1", TaskID: common.TaskID(req.TaskID), Kind: recoveryaction.KindContinueExecuted, Summary: "operator confirmed current brief"},
				RecoveryClass:         orchestrator.RecoveryClassReadyNextRun,
				RecommendedAction:     orchestrator.RecoveryActionStartNextRun,
				ReadyForNextRun:       true,
				ReadyForHandoffLaunch: false,
				RecoveryReason:        "operator explicitly confirmed the current brief for the next bounded run",
				CanonicalResponse:     "continue recovery executed",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskContinueRecoveryRequest{TaskID: common.TaskID("tsk_789")})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_continue_recovery_1",
		Method:    ipc.MethodExecuteContinueRecovery,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_789" {
		t.Fatalf("unexpected continue recovery request: %+v", captured)
	}
	var out ipc.TaskContinueRecoveryResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal continue recovery response: %v", err)
	}
	if out.BriefID != "brf_current" || out.Action.Kind != string(recoveryaction.KindContinueExecuted) || !out.ReadyForNextRun {
		t.Fatalf("unexpected continue recovery response: %+v", out)
	}
}

func TestHandleRequestStatusAndInspectRouteMapRecoveryActions(t *testing.T) {
	action := &recoveryaction.Record{
		Version:   1,
		ActionID:  "ract_status",
		TaskID:    common.TaskID("tsk_status"),
		Kind:      recoveryaction.KindRepairIntentRecorded,
		Summary:   "repair intent recorded",
		CreatedAt: time.Unix(1710000100, 0).UTC(),
	}
	handler := &fakeOrchestratorService{
		statusFn: func(_ context.Context, _ string) (orchestrator.StatusTaskResult, error) {
			return orchestrator.StatusTaskResult{
				TaskID: common.TaskID("tsk_status"),
				Phase:  phase.PhaseBlocked,
				CompiledIntent: &orchestrator.CompiledIntentSummary{
					IntentID:                "int_status_1",
					Class:                   "DEBUG_FIX",
					Posture:                 "REPAIR_RECOVERY",
					ExecutionReadiness:      "REPAIR_RECOVERY_FOCUSED",
					Objective:               "Repair stalled handoff continuity posture",
					RequiresClarification:   false,
					BoundedEvidenceMessages: 6,
					Digest:                  "repair/recovery-focused intent posture in bounded recent evidence",
					Advisory:                "Intent is repair/recovery-focused in bounded recent evidence.",
				},
				CompiledBrief: &orchestrator.CompiledBriefSummary{
					BriefID:                 "brf_status_1",
					IntentID:                "int_status_1",
					Posture:                 brief.PostureRepairOriented,
					Objective:               "Repair stalled handoff continuity posture",
					NormalizedAction:        "inspect and repair stalled handoff follow-through posture",
					ScopeSummary:            "bounded scope signals: handoff continuity and review posture",
					Constraints:             []string{"no authority changes"},
					DoneCriteria:            []string{"repair evidence captured conservatively"},
					Digest:                  "repair-oriented brief posture in bounded recent evidence",
					Advisory:                "Brief is repair-oriented in bounded recent evidence.",
					BoundedEvidenceMessages: 6,
				},
				LatestCheckpointTrigger:     checkpoint.TriggerManual,
				HandoffContinuityState:      orchestrator.HandoffContinuityStateFollowThroughStalled,
				HandoffContinuityReason:     "launch completed and acknowledgment captured, but downstream follow-through appears stalled",
				HandoffContinuationProven:   false,
				LatestAcknowledgmentID:      "hak_1",
				LatestAcknowledgmentStatus:  handoff.AcknowledgmentCaptured,
				LatestAcknowledgmentSummary: "Claude acknowledged the handoff packet.",
				LatestFollowThroughID:       "hft_status",
				LatestFollowThroughKind:     handoff.FollowThroughStalledReviewRequired,
				LatestFollowThroughSummary:  "Claude follow-through looks stalled",
				LatestResolutionID:          "hrs_status",
				LatestResolutionKind:        handoff.ResolutionReviewedStale,
				LatestResolutionSummary:     "reviewed stale after operator follow-up",
				LatestResolutionAt:          time.Unix(1710000800, 0).UTC(),
				ActiveBranchClass:           orchestrator.ActiveBranchClassHandoffClaude,
				ActiveBranchRef:             "hnd_status",
				ActiveBranchAnchorKind:      orchestrator.ActiveBranchAnchorKindHandoff,
				ActiveBranchAnchorRef:       "hnd_status",
				ActiveBranchReason:          "accepted Claude handoff branch currently owns continuity",
				LocalRunFinalizationState:   orchestrator.LocalRunFinalizationStaleReconciliationNeeded,
				LocalRunFinalizationRunID:   common.RunID("run_123"),
				LocalRunFinalizationStatus:  run.StatusRunning,
				LocalRunFinalizationReason:  "latest run is still durably RUNNING and requires explicit stale reconciliation",
				LocalResumeAuthorityState:   orchestrator.LocalResumeAuthorityBlocked,
				LocalResumeMode:             orchestrator.LocalResumeModeNone,
				LocalResumeReason:           "local interrupted-lineage resume is blocked while Claude handoff branch hnd_status owns continuity",
				RequiredNextOperatorAction:  orchestrator.OperatorActionReviewHandoffFollowUp,
				ActionAuthority: []orchestrator.OperatorActionAuthority{
					{Action: orchestrator.OperatorActionLocalMessageMutation, State: orchestrator.OperatorActionAuthorityBlocked, BlockingBranchClass: orchestrator.ActiveBranchClassHandoffClaude, BlockingBranchRef: "hnd_status", Reason: "Cannot send a local task message while launched Claude handoff hnd_status remains the active continuity branch."},
					{Action: orchestrator.OperatorActionReviewHandoffFollowUp, State: orchestrator.OperatorActionAuthorityRequiredNext, Reason: "launch completed and acknowledgment captured, but downstream follow-through appears stalled"},
				},
				OperatorDecision: &orchestrator.OperatorDecisionSummary{
					Headline:           "Claude follow-through review required",
					RequiredNextAction: orchestrator.OperatorActionReviewHandoffFollowUp,
					PrimaryReason:      "launch completed and acknowledgment captured, but downstream follow-through appears stalled",
					Guidance:           "Review the stalled Claude follow-through before resuming local work. Transcript review is stale for shell session shs_review_status; newer retained evidence starts at sequence 42.",
					IntegrityNote:      "Downstream Claude completion remains unproven. Transcript review is behind retained evidence (oldest unreviewed sequence 42).",
				},
				OperatorExecutionPlan: &orchestrator.OperatorExecutionPlan{
					PrimaryStep: &orchestrator.OperatorExecutionStep{
						Action:      orchestrator.OperatorActionReviewHandoffFollowUp,
						Status:      orchestrator.OperatorActionAuthorityRequiredNext,
						Domain:      orchestrator.OperatorExecutionDomainReview,
						CommandHint: "tuku inspect --task tsk_status",
						Reason:      "launch completed and acknowledgment captured, but downstream follow-through appears stalled. Newer retained transcript evidence exists starting at sequence 42; review awareness is recommended while progressing.",
					},
					MandatoryBeforeProgress: true,
				},
				LatestContinuityTransitionReceipt: &orchestrator.ContinuityTransitionReceiptSummary{
					ReceiptID:             "ctr_status_1",
					TaskID:                common.TaskID("tsk_status"),
					TransitionKind:        transition.KindHandoffLaunch,
					HandoffStateBefore:    orchestrator.HandoffContinuityStateAcceptedNotLaunched,
					HandoffStateAfter:     orchestrator.HandoffContinuityStateLaunchCompletedAckSeen,
					ReviewGapPresent:      true,
					ReviewPosture:         transition.ReviewPostureGlobalReviewStale,
					AcknowledgmentPresent: false,
					Summary:               "handoff launch was recorded while transcript review remained stale",
					CreatedAt:             time.Unix(1710000700, 0).UTC(),
				},
				RecentContinuityTransitionReceipts: []orchestrator.ContinuityTransitionReceiptSummary{
					{
						ReceiptID:             "ctr_status_1",
						TaskID:                common.TaskID("tsk_status"),
						TransitionKind:        transition.KindHandoffLaunch,
						HandoffStateBefore:    orchestrator.HandoffContinuityStateAcceptedNotLaunched,
						HandoffStateAfter:     orchestrator.HandoffContinuityStateLaunchCompletedAckSeen,
						ReviewGapPresent:      true,
						ReviewPosture:         transition.ReviewPostureGlobalReviewStale,
						AcknowledgmentPresent: false,
						Summary:               "handoff launch was recorded while transcript review remained stale",
						CreatedAt:             time.Unix(1710000700, 0).UTC(),
					},
				},
				ContinuityIncidentTaskRisk: &orchestrator.ContinuityIncidentTaskRiskSummary{
					Class:                               orchestrator.ContinuityIncidentTaskRiskRecurringWeakClosure,
					Digest:                              "recurring continuity weakness in recent bounded evidence",
					WindowAdvisory:                      "bounded incident window anchors=3 open=1 reopened=2 triaged-without-follow-up=0 stagnant=0",
					Detail:                              "Recent bounded evidence suggests recurring weak closure posture across incidents.",
					DistinctAnchors:                     3,
					RecurringWeakClosure:                true,
					ReopenedAfterClosureAnchors:         2,
					RepeatedReopenLoopAnchors:           1,
					OperationallyUnresolvedAnchorSignal: 3,
					RecentAnchorClasses: []orchestrator.ContinuityIncidentClosureClass{
						orchestrator.ContinuityIncidentClosureWeakReopened,
						orchestrator.ContinuityIncidentClosureWeakLoop,
					},
				},
				CurrentTaskMemoryID:                 common.MemoryID("mem_status_1"),
				CurrentTaskMemorySource:             "run_completed",
				CurrentTaskMemorySummary:            "phase=blocked; action=inspect and repair stalled handoff follow-through posture; next=review follow-through",
				CurrentTaskMemoryFullHistoryTokens:  480,
				CurrentTaskMemoryResumePromptTokens: 140,
				CurrentTaskMemoryCompactionRatio:    3.43,
				RecoveryClass:                       orchestrator.RecoveryClassHandoffFollowThroughReviewRequired,
				RecommendedAction:                   orchestrator.RecoveryActionReviewHandoffFollowThrough,
				LatestRecoveryAction:                action,
			}, nil
		},
		inspectFn: func(_ context.Context, _ string) (orchestrator.InspectTaskResult, error) {
			return orchestrator.InspectTaskResult{
				TaskID: common.TaskID("tsk_status"),
				CompiledIntent: &orchestrator.CompiledIntentSummary{
					IntentID:                "int_status_1",
					Class:                   "DEBUG_FIX",
					Posture:                 "REPAIR_RECOVERY",
					ExecutionReadiness:      "REPAIR_RECOVERY_FOCUSED",
					Objective:               "Repair stalled handoff continuity posture",
					RequiresClarification:   false,
					BoundedEvidenceMessages: 6,
					Digest:                  "repair/recovery-focused intent posture in bounded recent evidence",
					Advisory:                "Intent is repair/recovery-focused in bounded recent evidence.",
				},
				CompiledBrief: &orchestrator.CompiledBriefSummary{
					BriefID:                 "brf_status_1",
					IntentID:                "int_status_1",
					Posture:                 brief.PostureRepairOriented,
					Objective:               "Repair stalled handoff continuity posture",
					NormalizedAction:        "inspect and repair stalled handoff follow-through posture",
					ScopeSummary:            "bounded scope signals: handoff continuity and review posture",
					Constraints:             []string{"no authority changes"},
					DoneCriteria:            []string{"repair evidence captured conservatively"},
					Digest:                  "repair-oriented brief posture in bounded recent evidence",
					Advisory:                "Brief is repair-oriented in bounded recent evidence.",
					BoundedEvidenceMessages: 6,
				},
				LatestRecoveryAction:  action,
				RecentRecoveryActions: []recoveryaction.Record{*action},
				Recovery: &orchestrator.RecoveryAssessment{
					TaskID:            common.TaskID("tsk_status"),
					RecoveryClass:     orchestrator.RecoveryClassHandoffFollowThroughReviewRequired,
					RecommendedAction: orchestrator.RecoveryActionReviewHandoffFollowThrough,
					LatestAction:      action,
				},
				HandoffContinuity: &orchestrator.HandoffContinuity{
					TaskID:                       common.TaskID("tsk_status"),
					HandoffID:                    "hnd_status",
					State:                        orchestrator.HandoffContinuityStateFollowThroughStalled,
					AcknowledgmentID:             "hak_1",
					AcknowledgmentStatus:         handoff.AcknowledgmentCaptured,
					AcknowledgmentSummary:        "Claude acknowledged the handoff packet.",
					FollowThroughID:              "hft_status",
					FollowThroughKind:            handoff.FollowThroughStalledReviewRequired,
					FollowThroughSummary:         "Claude follow-through looks stalled",
					ResolutionID:                 "hrs_status",
					ResolutionKind:               handoff.ResolutionReviewedStale,
					ResolutionSummary:            "reviewed stale after operator follow-up",
					DownstreamContinuationProven: false,
					Reason:                       "launch completed and acknowledgment captured, but downstream follow-through appears stalled",
				},
				FollowThrough: &handoff.FollowThrough{
					Version:         1,
					RecordID:        "hft_status",
					HandoffID:       "hnd_status",
					LaunchAttemptID: "hlc_status",
					LaunchID:        "launch_status",
					TaskID:          common.TaskID("tsk_status"),
					TargetWorker:    run.WorkerKindClaude,
					Kind:            handoff.FollowThroughStalledReviewRequired,
					Summary:         "Claude follow-through looks stalled",
					CreatedAt:       time.Unix(1710000300, 0).UTC(),
				},
				Resolution: &handoff.Resolution{
					Version:         1,
					ResolutionID:    "hrs_status",
					HandoffID:       "hnd_status",
					LaunchAttemptID: "hlc_status",
					LaunchID:        "launch_status",
					TaskID:          common.TaskID("tsk_status"),
					TargetWorker:    run.WorkerKindClaude,
					Kind:            handoff.ResolutionReviewedStale,
					Summary:         "reviewed stale after operator follow-up",
					CreatedAt:       time.Unix(1710000800, 0).UTC(),
				},
				ActiveBranch: &orchestrator.ActiveBranchProvenance{
					TaskID:                 common.TaskID("tsk_status"),
					Class:                  orchestrator.ActiveBranchClassHandoffClaude,
					BranchRef:              "hnd_status",
					ActionabilityAnchor:    orchestrator.ActiveBranchAnchorKindHandoff,
					ActionabilityAnchorRef: "hnd_status",
					Reason:                 "accepted Claude handoff branch currently owns continuity",
				},
				LocalRunFinalization: &orchestrator.LocalRunFinalization{
					TaskID:    common.TaskID("tsk_status"),
					State:     orchestrator.LocalRunFinalizationStaleReconciliationNeeded,
					RunID:     common.RunID("run_123"),
					RunStatus: run.StatusRunning,
					Reason:    "latest run is still durably RUNNING and requires explicit stale reconciliation",
				},
				LocalResumeAuthority: &orchestrator.LocalResumeAuthority{
					TaskID:              common.TaskID("tsk_status"),
					State:               orchestrator.LocalResumeAuthorityBlocked,
					Mode:                orchestrator.LocalResumeModeNone,
					BlockingBranchClass: orchestrator.ActiveBranchClassHandoffClaude,
					BlockingBranchRef:   "hnd_status",
					Reason:              "local interrupted-lineage resume is blocked while Claude handoff branch hnd_status owns continuity",
				},
				TaskMemory: &taskmemory.Snapshot{
					MemoryID:                  common.MemoryID("mem_status_1"),
					Source:                    "run_completed",
					Summary:                   "phase=blocked; action=inspect and repair stalled handoff follow-through posture; next=review follow-through",
					FullHistoryTokenEstimate:  480,
					ResumePromptTokenEstimate: 140,
					MemoryCompactionRatio:     3.43,
				},
				ActionAuthority: &orchestrator.OperatorActionAuthoritySet{
					RequiredNextAction: orchestrator.OperatorActionReviewHandoffFollowUp,
					Actions: []orchestrator.OperatorActionAuthority{
						{Action: orchestrator.OperatorActionLocalMessageMutation, State: orchestrator.OperatorActionAuthorityBlocked, BlockingBranchClass: orchestrator.ActiveBranchClassHandoffClaude, BlockingBranchRef: "hnd_status", Reason: "Cannot send a local task message while launched Claude handoff hnd_status remains the active continuity branch."},
						{Action: orchestrator.OperatorActionReviewHandoffFollowUp, State: orchestrator.OperatorActionAuthorityRequiredNext, Reason: "launch completed and acknowledgment captured, but downstream follow-through appears stalled"},
					},
				},
				OperatorDecision: &orchestrator.OperatorDecisionSummary{
					Headline:           "Claude follow-through review required",
					RequiredNextAction: orchestrator.OperatorActionReviewHandoffFollowUp,
					PrimaryReason:      "launch completed and acknowledgment captured, but downstream follow-through appears stalled",
					Guidance:           "Review the stalled Claude follow-through before resuming local work. Transcript review is stale for shell session shs_review_status; newer retained evidence starts at sequence 42.",
					IntegrityNote:      "Downstream Claude completion remains unproven. Transcript review is behind retained evidence (oldest unreviewed sequence 42).",
				},
				OperatorExecutionPlan: &orchestrator.OperatorExecutionPlan{
					PrimaryStep: &orchestrator.OperatorExecutionStep{
						Action:      orchestrator.OperatorActionReviewHandoffFollowUp,
						Status:      orchestrator.OperatorActionAuthorityRequiredNext,
						Domain:      orchestrator.OperatorExecutionDomainReview,
						CommandHint: "tuku inspect --task tsk_status",
						Reason:      "launch completed and acknowledgment captured, but downstream follow-through appears stalled. Newer retained transcript evidence exists starting at sequence 42; review awareness is recommended while progressing.",
					},
					MandatoryBeforeProgress: true,
				},
				LatestContinuityTransitionReceipt: &orchestrator.ContinuityTransitionReceiptSummary{
					ReceiptID:             "ctr_status_1",
					TaskID:                common.TaskID("tsk_status"),
					TransitionKind:        transition.KindHandoffLaunch,
					HandoffStateBefore:    orchestrator.HandoffContinuityStateAcceptedNotLaunched,
					HandoffStateAfter:     orchestrator.HandoffContinuityStateLaunchCompletedAckSeen,
					ReviewGapPresent:      true,
					ReviewPosture:         transition.ReviewPostureGlobalReviewStale,
					AcknowledgmentPresent: false,
					Summary:               "handoff launch was recorded while transcript review remained stale",
					CreatedAt:             time.Unix(1710000700, 0).UTC(),
				},
				RecentContinuityTransitionReceipts: []orchestrator.ContinuityTransitionReceiptSummary{
					{
						ReceiptID:             "ctr_status_1",
						TaskID:                common.TaskID("tsk_status"),
						TransitionKind:        transition.KindHandoffLaunch,
						HandoffStateBefore:    orchestrator.HandoffContinuityStateAcceptedNotLaunched,
						HandoffStateAfter:     orchestrator.HandoffContinuityStateLaunchCompletedAckSeen,
						ReviewGapPresent:      true,
						ReviewPosture:         transition.ReviewPostureGlobalReviewStale,
						AcknowledgmentPresent: false,
						Summary:               "handoff launch was recorded while transcript review remained stale",
						CreatedAt:             time.Unix(1710000700, 0).UTC(),
					},
				},
				ContinuityIncidentTaskRisk: &orchestrator.ContinuityIncidentTaskRiskSummary{
					Class:                               orchestrator.ContinuityIncidentTaskRiskRecurringWeakClosure,
					Digest:                              "recurring continuity weakness in recent bounded evidence",
					WindowAdvisory:                      "bounded incident window anchors=3 open=1 reopened=2 triaged-without-follow-up=0 stagnant=0",
					Detail:                              "Recent bounded evidence suggests recurring weak closure posture across incidents.",
					DistinctAnchors:                     3,
					RecurringWeakClosure:                true,
					ReopenedAfterClosureAnchors:         2,
					RepeatedReopenLoopAnchors:           1,
					OperationallyUnresolvedAnchorSignal: 3,
					RecentAnchorClasses: []orchestrator.ContinuityIncidentClosureClass{
						orchestrator.ContinuityIncidentClosureWeakReopened,
						orchestrator.ContinuityIncidentClosureWeakLoop,
					},
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	statusPayload, _ := json.Marshal(ipc.TaskStatusRequest{TaskID: common.TaskID("tsk_status")})
	statusResp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_status_recovery",
		Method:    ipc.MethodTaskStatus,
		Payload:   statusPayload,
	})
	if !statusResp.OK {
		t.Fatalf("expected OK status response, got %+v", statusResp.Error)
	}
	var statusOut ipc.TaskStatusResponse
	if err := json.Unmarshal(statusResp.Payload, &statusOut); err != nil {
		t.Fatalf("unmarshal status response: %v", err)
	}
	if statusOut.LatestRecoveryAction == nil || statusOut.LatestRecoveryAction.ActionID != action.ActionID {
		t.Fatalf("expected latest recovery action in status response, got %+v", statusOut.LatestRecoveryAction)
	}
	if statusOut.CompiledIntent == nil || statusOut.CompiledIntent.IntentID != "int_status_1" || statusOut.CompiledIntent.ExecutionReadiness != "REPAIR_RECOVERY_FOCUSED" {
		t.Fatalf("expected status compiled intent mapping, got %+v", statusOut.CompiledIntent)
	}
	if statusOut.CompiledBrief == nil || statusOut.CompiledBrief.BriefID != "brf_status_1" || statusOut.CompiledBrief.Posture != string(brief.PostureRepairOriented) {
		t.Fatalf("expected status compiled brief mapping, got %+v", statusOut.CompiledBrief)
	}
	if statusOut.HandoffContinuityState != string(orchestrator.HandoffContinuityStateFollowThroughStalled) {
		t.Fatalf("expected handoff continuity state in status response, got %+v", statusOut)
	}
	if statusOut.LatestAcknowledgmentStatus != string(handoff.AcknowledgmentCaptured) {
		t.Fatalf("expected acknowledgment status in status response, got %+v", statusOut)
	}
	if statusOut.LatestFollowThroughKind != string(handoff.FollowThroughStalledReviewRequired) {
		t.Fatalf("expected follow-through status mapping, got %+v", statusOut)
	}
	if statusOut.LatestResolutionKind != string(handoff.ResolutionReviewedStale) {
		t.Fatalf("expected resolution status mapping, got %+v", statusOut)
	}
	if statusOut.ActiveBranchClass != string(orchestrator.ActiveBranchClassHandoffClaude) || statusOut.ActiveBranchRef != "hnd_status" {
		t.Fatalf("expected active branch in status response, got %+v", statusOut)
	}
	if statusOut.LocalRunFinalizationState != string(orchestrator.LocalRunFinalizationStaleReconciliationNeeded) || statusOut.LocalRunFinalizationRunID != "run_123" {
		t.Fatalf("expected local run finalization in status response, got %+v", statusOut)
	}
	if statusOut.LocalResumeAuthorityState != string(orchestrator.LocalResumeAuthorityBlocked) || statusOut.LocalResumeReason == "" {
		t.Fatalf("expected local resume authority in status response, got %+v", statusOut)
	}
	if statusOut.RequiredNextOperatorAction != string(orchestrator.OperatorActionReviewHandoffFollowUp) || len(statusOut.ActionAuthority) != 2 {
		t.Fatalf("expected status action authority mapping, got %+v", statusOut)
	}
	if statusOut.OperatorExecutionPlan == nil || statusOut.OperatorExecutionPlan.PrimaryStep == nil {
		t.Fatalf("expected status execution plan mapping, got %+v", statusOut.OperatorExecutionPlan)
	}
	if statusOut.OperatorExecutionPlan.PrimaryStep.Action != string(orchestrator.OperatorActionReviewHandoffFollowUp) || statusOut.OperatorExecutionPlan.PrimaryStep.CommandHint != "tuku inspect --task tsk_status" {
		t.Fatalf("expected status execution plan primary step mapping, got %+v", statusOut.OperatorExecutionPlan.PrimaryStep)
	}
	if statusOut.OperatorDecision == nil || !strings.Contains(statusOut.OperatorDecision.Guidance, "Transcript review is stale for shell session shs_review_status") {
		t.Fatalf("expected status review-aware decision guidance mapping, got %+v", statusOut.OperatorDecision)
	}
	if !strings.Contains(statusOut.OperatorExecutionPlan.PrimaryStep.Reason, "starting at sequence 42") {
		t.Fatalf("expected status review-aware plan reason mapping, got %+v", statusOut.OperatorExecutionPlan.PrimaryStep)
	}
	if statusOut.LatestContinuityTransitionReceipt == nil || statusOut.LatestContinuityTransitionReceipt.ReceiptID != "ctr_status_1" {
		t.Fatalf("expected status transition receipt mapping, got %+v", statusOut.LatestContinuityTransitionReceipt)
	}
	if len(statusOut.RecentContinuityTransitionReceipts) != 1 || statusOut.RecentContinuityTransitionReceipts[0].ReviewPosture != string(transition.ReviewPostureGlobalReviewStale) {
		t.Fatalf("expected status transition receipt history mapping, got %+v", statusOut.RecentContinuityTransitionReceipts)
	}
	if statusOut.ContinuityIncidentTaskRisk == nil || statusOut.ContinuityIncidentTaskRisk.Class != string(orchestrator.ContinuityIncidentTaskRiskRecurringWeakClosure) {
		t.Fatalf("expected status incident task-risk mapping, got %+v", statusOut.ContinuityIncidentTaskRisk)
	}
	if len(statusOut.ContinuityIncidentTaskRisk.RecentAnchorClasses) != 2 {
		t.Fatalf("expected status task-risk recent classes mapping, got %+v", statusOut.ContinuityIncidentTaskRisk)
	}
	if statusOut.CurrentTaskMemoryID != "mem_status_1" || statusOut.CurrentTaskMemoryResumePromptTokens != 140 || statusOut.CurrentTaskMemoryCompactionRatio != 3.43 {
		t.Fatalf("expected status task memory mapping, got %+v", statusOut)
	}

	inspectPayload, _ := json.Marshal(ipc.TaskInspectRequest{TaskID: common.TaskID("tsk_status")})
	inspectResp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_inspect_recovery",
		Method:    ipc.MethodTaskInspect,
		Payload:   inspectPayload,
	})
	if !inspectResp.OK {
		t.Fatalf("expected OK inspect response, got %+v", inspectResp.Error)
	}
	var inspectOut ipc.TaskInspectResponse
	if err := json.Unmarshal(inspectResp.Payload, &inspectOut); err != nil {
		t.Fatalf("unmarshal inspect response: %v", err)
	}
	if inspectOut.LatestRecoveryAction == nil || inspectOut.LatestRecoveryAction.ActionID != action.ActionID {
		t.Fatalf("expected latest recovery action in inspect response, got %+v", inspectOut.LatestRecoveryAction)
	}
	if inspectOut.CompiledIntent == nil || inspectOut.CompiledIntent.IntentID != "int_status_1" || inspectOut.CompiledIntent.Posture != "REPAIR_RECOVERY" {
		t.Fatalf("expected inspect compiled intent mapping, got %+v", inspectOut.CompiledIntent)
	}
	if inspectOut.CompiledBrief == nil || inspectOut.CompiledBrief.BriefID != "brf_status_1" || inspectOut.CompiledBrief.Posture != string(brief.PostureRepairOriented) {
		t.Fatalf("expected inspect compiled brief mapping, got %+v", inspectOut.CompiledBrief)
	}
	if len(inspectOut.RecentRecoveryActions) != 1 || inspectOut.RecentRecoveryActions[0].ActionID != action.ActionID {
		t.Fatalf("expected recent recovery action in inspect response, got %+v", inspectOut.RecentRecoveryActions)
	}
	if inspectOut.Recovery == nil || inspectOut.Recovery.LatestAction == nil || inspectOut.Recovery.LatestAction.ActionID != action.ActionID {
		t.Fatalf("expected recovery latest action mapping, got %+v", inspectOut.Recovery)
	}
	if inspectOut.HandoffContinuity == nil || inspectOut.HandoffContinuity.State != string(orchestrator.HandoffContinuityStateFollowThroughStalled) {
		t.Fatalf("expected handoff continuity in inspect response, got %+v", inspectOut.HandoffContinuity)
	}
	if inspectOut.FollowThrough == nil || inspectOut.FollowThrough.Kind != handoff.FollowThroughStalledReviewRequired {
		t.Fatalf("expected inspect follow-through mapping, got %+v", inspectOut.FollowThrough)
	}
	if inspectOut.Resolution == nil || inspectOut.Resolution.Kind != handoff.ResolutionReviewedStale {
		t.Fatalf("expected inspect resolution mapping, got %+v", inspectOut.Resolution)
	}
	if inspectOut.ActiveBranch == nil || inspectOut.ActiveBranch.Class != string(orchestrator.ActiveBranchClassHandoffClaude) || inspectOut.ActiveBranch.BranchRef != "hnd_status" {
		t.Fatalf("expected active branch in inspect response, got %+v", inspectOut.ActiveBranch)
	}
	if inspectOut.LocalRunFinalization == nil || inspectOut.LocalRunFinalization.State != string(orchestrator.LocalRunFinalizationStaleReconciliationNeeded) {
		t.Fatalf("expected inspect local run finalization, got %+v", inspectOut.LocalRunFinalization)
	}
	if inspectOut.LocalResumeAuthority == nil || inspectOut.LocalResumeAuthority.State != string(orchestrator.LocalResumeAuthorityBlocked) {
		t.Fatalf("expected inspect local resume authority, got %+v", inspectOut.LocalResumeAuthority)
	}
	if inspectOut.ActionAuthority == nil || inspectOut.ActionAuthority.RequiredNextAction != string(orchestrator.OperatorActionReviewHandoffFollowUp) {
		t.Fatalf("expected inspect action authority mapping, got %+v", inspectOut.ActionAuthority)
	}
	if inspectOut.OperatorDecision == nil || !strings.Contains(inspectOut.OperatorDecision.Guidance, "Transcript review is stale for shell session shs_review_status") {
		t.Fatalf("expected inspect review-aware decision guidance mapping, got %+v", inspectOut.OperatorDecision)
	}
	if inspectOut.OperatorExecutionPlan == nil || inspectOut.OperatorExecutionPlan.PrimaryStep == nil {
		t.Fatalf("expected inspect execution plan mapping, got %+v", inspectOut.OperatorExecutionPlan)
	}
	if inspectOut.OperatorExecutionPlan.PrimaryStep.Action != string(orchestrator.OperatorActionReviewHandoffFollowUp) || inspectOut.OperatorExecutionPlan.PrimaryStep.CommandHint != "tuku inspect --task tsk_status" {
		t.Fatalf("expected inspect execution plan primary step mapping, got %+v", inspectOut.OperatorExecutionPlan.PrimaryStep)
	}
	if inspectOut.LatestContinuityTransitionReceipt == nil || inspectOut.LatestContinuityTransitionReceipt.ReceiptID != "ctr_status_1" {
		t.Fatalf("expected inspect transition receipt mapping, got %+v", inspectOut.LatestContinuityTransitionReceipt)
	}
	if len(inspectOut.RecentContinuityTransitionReceipts) != 1 || inspectOut.RecentContinuityTransitionReceipts[0].TransitionKind != string(transition.KindHandoffLaunch) {
		t.Fatalf("expected inspect transition receipt history mapping, got %+v", inspectOut.RecentContinuityTransitionReceipts)
	}
	if inspectOut.ContinuityIncidentTaskRisk == nil || inspectOut.ContinuityIncidentTaskRisk.Class != string(orchestrator.ContinuityIncidentTaskRiskRecurringWeakClosure) {
		t.Fatalf("expected inspect incident task-risk mapping, got %+v", inspectOut.ContinuityIncidentTaskRisk)
	}
	if inspectOut.TaskMemory == nil || inspectOut.TaskMemory.MemoryID != "mem_status_1" || inspectOut.TaskMemory.ResumePromptTokenEstimate != 140 {
		t.Fatalf("expected inspect task memory mapping, got %+v", inspectOut.TaskMemory)
	}
}

func TestHandleRequestTaskIntentRoute(t *testing.T) {
	var captured orchestrator.ReadCompiledIntentRequest
	handler := &fakeOrchestratorService{
		readCompiledIntentFn: func(_ context.Context, req orchestrator.ReadCompiledIntentRequest) (orchestrator.ReadCompiledIntentResult, error) {
			captured = req
			return orchestrator.ReadCompiledIntentResult{
				TaskID:          common.TaskID(req.TaskID),
				CurrentIntentID: common.IntentID("int_123"),
				Bounded:         true,
				CompiledIntent: &orchestrator.CompiledIntentSummary{
					IntentID:                common.IntentID("int_123"),
					Class:                   "IMPLEMENT_CHANGE",
					Posture:                 "PLANNING",
					ExecutionReadiness:      "PLANNING_IN_PROGRESS",
					Objective:               "Prepare intent compiler rollout",
					ScopeSummary:            "bounded scope signals: internal/orchestrator/service.go",
					RequiresClarification:   true,
					ClarificationQuestions:  []string{"Which task slice should be executed first?"},
					BoundedEvidenceMessages: 5,
					Digest:                  "planning intent posture in bounded recent evidence",
					Advisory:                "Intent remains planning-focused in bounded recent evidence.",
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskIntentRequest{TaskID: common.TaskID("tsk_intent")})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_intent",
		Method:    ipc.MethodTaskIntent,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_intent" {
		t.Fatalf("unexpected read intent request: %+v", captured)
	}
	var out ipc.TaskIntentResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal task intent response: %v", err)
	}
	if out.TaskID != "tsk_intent" || out.CurrentIntentID != "int_123" || !out.Bounded {
		t.Fatalf("unexpected task intent response envelope: %+v", out)
	}
	if out.CompiledIntent == nil || out.CompiledIntent.ExecutionReadiness != "PLANNING_IN_PROGRESS" || out.CompiledIntent.Digest == "" {
		t.Fatalf("expected mapped compiled intent payload, got %+v", out.CompiledIntent)
	}
	if len(out.CompiledIntent.ClarificationQuestions) != 1 || !out.CompiledIntent.RequiresClarification {
		t.Fatalf("expected clarification cues in task intent response, got %+v", out.CompiledIntent)
	}
}

func TestHandleRequestTaskBriefRoute(t *testing.T) {
	var captured orchestrator.ReadGeneratedBriefRequest
	handler := &fakeOrchestratorService{
		readGeneratedBriefFn: func(_ context.Context, req orchestrator.ReadGeneratedBriefRequest) (orchestrator.ReadGeneratedBriefResult, error) {
			captured = req
			return orchestrator.ReadGeneratedBriefResult{
				TaskID:         common.TaskID(req.TaskID),
				CurrentBriefID: common.BriefID("brf_123"),
				Bounded:        true,
				Brief: &brief.ExecutionBrief{
					BriefID:                 common.BriefID("brf_123"),
					TaskID:                  common.TaskID(req.TaskID),
					IntentID:                common.IntentID("int_123"),
					Posture:                 brief.PostureClarificationNeeded,
					Objective:               "Clarify and bound next execution step",
					RequestedOutcome:        "Produce explicit bounded implementation brief",
					NormalizedAction:        "prepare bounded execution brief",
					ScopeSummary:            "scope remains underspecified in recent bounded evidence",
					Constraints:             []string{"no authority changes"},
					DoneCriteria:            []string{"clarification questions remain explicit"},
					AmbiguityFlags:          []string{"scope_underspecified"},
					ClarificationQuestions:  []string{"Which module should be changed first?"},
					RequiresClarification:   true,
					WorkerFraming:           "Clarification-focused brief: do not fabricate missing requirements; surface unresolved questions before bounded execution.",
					BoundedEvidenceMessages: 4,
				},
				CompiledBrief: &orchestrator.CompiledBriefSummary{
					BriefID:                 common.BriefID("brf_123"),
					IntentID:                common.IntentID("int_123"),
					Posture:                 brief.PostureClarificationNeeded,
					Objective:               "Clarify and bound next execution step",
					RequestedOutcome:        "Produce explicit bounded implementation brief",
					NormalizedAction:        "prepare bounded execution brief",
					ScopeSummary:            "scope remains underspecified in recent bounded evidence",
					Constraints:             []string{"no authority changes"},
					DoneCriteria:            []string{"clarification questions remain explicit"},
					AmbiguityFlags:          []string{"scope_underspecified"},
					ClarificationQuestions:  []string{"Which module should be changed first?"},
					RequiresClarification:   true,
					WorkerFraming:           "Clarification-focused brief: do not fabricate missing requirements; surface unresolved questions before bounded execution.",
					BoundedEvidenceMessages: 4,
					MemoryCompression: &brief.MemoryCompression{
						Applied:                   true,
						Summary:                   "phase=planning; action=prepare bounded execution brief",
						FullHistoryTokenEstimate:  360,
						ResumePromptTokenEstimate: 110,
						MemoryCompactionRatio:     3.27,
						ConfirmedFactsCount:       3,
					},
					Digest:   "clarification-needed brief posture in bounded recent evidence",
					Advisory: "Brief remains clarification-needed in bounded recent evidence; unresolved clarification questions remain explicit.",
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskBriefRequest{TaskID: common.TaskID("tsk_brief")})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_brief",
		Method:    ipc.MethodTaskBrief,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_brief" {
		t.Fatalf("unexpected task brief request: %+v", captured)
	}
	var out ipc.TaskBriefResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal task brief response: %v", err)
	}
	if out.TaskID != "tsk_brief" || out.CurrentBriefID != "brf_123" || !out.Bounded {
		t.Fatalf("unexpected task brief envelope: %+v", out)
	}
	if out.CompiledBrief == nil || out.CompiledBrief.Posture != string(brief.PostureClarificationNeeded) {
		t.Fatalf("expected compiled brief mapping, got %+v", out.CompiledBrief)
	}
	if out.CompiledBrief.Digest != "clarification-needed brief posture in bounded recent evidence" {
		t.Fatalf("unexpected compiled brief digest mapping: %+v", out.CompiledBrief)
	}
	if out.CompiledBrief.MemoryCompression == nil || !out.CompiledBrief.MemoryCompression.Applied || out.CompiledBrief.MemoryCompression.ResumePromptTokenEstimate != 110 {
		t.Fatalf("expected memory compression in compiled brief mapping, got %+v", out.CompiledBrief)
	}
	if out.Brief == nil || !out.Brief.RequiresClarification || len(out.Brief.ClarificationQuestions) != 1 {
		t.Fatalf("expected brief clarification cues, got %+v", out.Brief)
	}
}

func TestHandleRequestTaskBenchmarkRoute(t *testing.T) {
	var captured orchestrator.ReadBenchmarkRequest
	handler := &fakeOrchestratorService{
		readBenchmarkFn: func(_ context.Context, req orchestrator.ReadBenchmarkRequest) (orchestrator.BenchmarkTaskResult, error) {
			captured = req
			return orchestrator.BenchmarkTaskResult{
				TaskID: common.TaskID(req.TaskID),
				Benchmark: &benchmark.Run{
					BenchmarkID:                   common.BenchmarkID("bmk_123"),
					TaskID:                        common.TaskID(req.TaskID),
					Source:                        "brief_compiled",
					RawPromptTokenEstimate:        5,
					DispatchPromptTokenEstimate:   118,
					StructuredPromptTokenEstimate: 99,
					EstimatedTokenSavings:         380,
					ConfidenceLevel:               "high",
					ConfidenceValue:               0.82,
				},
				CompiledBrief: &orchestrator.CompiledBriefSummary{
					BriefID: common.BriefID("brf_123"),
					PromptIR: &promptir.Packet{
						NormalizedTaskType: "BUG_FIX",
						ValidatorPlan:      promptir.ValidatorPlan{Commands: []string{"go test ./internal/orchestrator"}},
					},
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskBenchmarkRequest{TaskID: common.TaskID("tsk_benchmark")})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_benchmark",
		Method:    ipc.MethodTaskBenchmark,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_benchmark" {
		t.Fatalf("unexpected benchmark request: %+v", captured)
	}
	var out ipc.TaskBenchmarkResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal task benchmark response: %v", err)
	}
	if out.TaskID != "tsk_benchmark" || out.Benchmark == nil || out.Benchmark.BenchmarkID != "bmk_123" {
		t.Fatalf("unexpected benchmark response envelope: %+v", out)
	}
	if out.CompiledBrief == nil || out.CompiledBrief.PromptIR == nil || len(out.CompiledBrief.PromptIR.ValidatorPlan.Commands) != 1 {
		t.Fatalf("expected compiled brief prompt ir mapping, got %+v", out.CompiledBrief)
	}
}

func TestHandleRequestRecordHandoffFollowThroughRoute(t *testing.T) {
	var captured orchestrator.RecordHandoffFollowThroughRequest
	handler := &fakeOrchestratorService{
		recordHandoffFollowThroughFn: func(_ context.Context, req orchestrator.RecordHandoffFollowThroughRequest) (orchestrator.RecordHandoffFollowThroughResult, error) {
			captured = req
			return orchestrator.RecordHandoffFollowThroughResult{
				TaskID: common.TaskID(req.TaskID),
				Record: handoff.FollowThrough{
					Version:         1,
					RecordID:        "hft_1",
					HandoffID:       "hnd_1",
					LaunchAttemptID: "hlc_1",
					LaunchID:        "launch_1",
					TaskID:          common.TaskID(req.TaskID),
					TargetWorker:    run.WorkerKindClaude,
					Kind:            req.Kind,
					Summary:         req.Summary,
					CreatedAt:       time.Unix(1710000200, 0).UTC(),
				},
				HandoffContinuity: orchestrator.HandoffContinuity{
					TaskID:                       common.TaskID(req.TaskID),
					HandoffID:                    "hnd_1",
					State:                        orchestrator.HandoffContinuityStateFollowThroughProofOfLife,
					LaunchAttemptID:              "hlc_1",
					LaunchID:                     "launch_1",
					FollowThroughID:              "hft_1",
					FollowThroughKind:            req.Kind,
					FollowThroughSummary:         req.Summary,
					DownstreamContinuationProven: true,
					Reason:                       "downstream proof of life observed",
				},
				RecoveryClass:         orchestrator.RecoveryClassHandoffLaunchCompleted,
				RecommendedAction:     orchestrator.RecoveryActionMonitorLaunchedHandoff,
				ReadyForNextRun:       false,
				ReadyForHandoffLaunch: false,
				RecoveryReason:        "launched handoff remains monitor-only",
				CanonicalResponse:     "follow-through recorded",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskHandoffFollowThroughRecordRequest{
		TaskID:  common.TaskID("tsk_follow"),
		Kind:    string(handoff.FollowThroughProofOfLifeObserved),
		Summary: "later Claude proof of life observed",
		Notes:   []string{"operator confirmed downstream ping"},
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_followthrough",
		Method:    ipc.MethodRecordHandoffFollowThrough,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got %+v", resp.Error)
	}
	if captured.TaskID != "tsk_follow" || captured.Kind != handoff.FollowThroughProofOfLifeObserved {
		t.Fatalf("unexpected captured follow-through request: %+v", captured)
	}
	var out ipc.TaskHandoffFollowThroughRecordResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal follow-through response: %v", err)
	}
	if out.Record == nil || out.Record.Kind != handoff.FollowThroughProofOfLifeObserved {
		t.Fatalf("expected recorded follow-through payload, got %+v", out.Record)
	}
	if out.HandoffContinuity == nil || out.HandoffContinuity.State != string(orchestrator.HandoffContinuityStateFollowThroughProofOfLife) {
		t.Fatalf("expected follow-through continuity mapping, got %+v", out.HandoffContinuity)
	}
	if out.RecoveryClass != string(orchestrator.RecoveryClassHandoffLaunchCompleted) {
		t.Fatalf("expected monitor recovery mapping, got %+v", out)
	}
}

func TestHandleRequestRecordHandoffResolutionRoute(t *testing.T) {
	var captured orchestrator.RecordHandoffResolutionRequest
	handler := &fakeOrchestratorService{
		recordHandoffResolutionFn: func(_ context.Context, req orchestrator.RecordHandoffResolutionRequest) (orchestrator.RecordHandoffResolutionResult, error) {
			captured = req
			return orchestrator.RecordHandoffResolutionResult{
				TaskID: common.TaskID(req.TaskID),
				Record: handoff.Resolution{
					Version:         1,
					ResolutionID:    "hrs_1",
					HandoffID:       "hnd_1",
					LaunchAttemptID: "hlc_1",
					LaunchID:        "launch_1",
					TaskID:          common.TaskID(req.TaskID),
					TargetWorker:    run.WorkerKindClaude,
					Kind:            req.Kind,
					Summary:         req.Summary,
					CreatedAt:       time.Unix(1710000500, 0).UTC(),
				},
				HandoffContinuity: orchestrator.HandoffContinuity{
					TaskID:            common.TaskID(req.TaskID),
					HandoffID:         "hnd_1",
					State:             orchestrator.HandoffContinuityStateResolved,
					LaunchAttemptID:   "hlc_1",
					LaunchID:          "launch_1",
					ResolutionID:      "hrs_1",
					ResolutionKind:    req.Kind,
					ResolutionSummary: req.Summary,
					Reason:            "Claude handoff branch was explicitly resolved without claiming downstream completion",
				},
				RecoveryClass:         orchestrator.RecoveryClassReadyNextRun,
				RecommendedAction:     orchestrator.RecoveryActionStartNextRun,
				ReadyForNextRun:       true,
				ReadyForHandoffLaunch: false,
				RecoveryReason:        "resolved handoff no longer blocks local mutation",
				CanonicalResponse:     "handoff resolution recorded",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskHandoffResolutionRecordRequest{
		TaskID:  common.TaskID("tsk_resolve"),
		Kind:    string(handoff.ResolutionSupersededByLocal),
		Summary: "operator returned local control",
		Notes:   []string{"close Claude branch"},
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_resolve",
		Method:    ipc.MethodRecordHandoffResolution,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got %+v", resp.Error)
	}
	if captured.TaskID != "tsk_resolve" || captured.Kind != handoff.ResolutionSupersededByLocal {
		t.Fatalf("unexpected captured handoff resolution request: %+v", captured)
	}
	var out ipc.TaskHandoffResolutionRecordResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal handoff resolution response: %v", err)
	}
	if out.Record == nil || out.Record.Kind != handoff.ResolutionSupersededByLocal {
		t.Fatalf("expected recorded resolution payload, got %+v", out.Record)
	}
	if out.HandoffContinuity == nil || out.HandoffContinuity.State != string(orchestrator.HandoffContinuityStateResolved) {
		t.Fatalf("expected resolved handoff continuity mapping, got %+v", out.HandoffContinuity)
	}
}

func TestHandleRequestShellLifecycleRoute(t *testing.T) {
	var captured orchestrator.RecordShellLifecycleRequest
	handler := &fakeOrchestratorService{
		recordShellLifecycleFn: func(_ context.Context, req orchestrator.RecordShellLifecycleRequest) (orchestrator.RecordShellLifecycleResult, error) {
			captured = req
			return orchestrator.RecordShellLifecycleResult{TaskID: common.TaskID(req.TaskID)}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	exitCode := 9
	payload, _ := json.Marshal(ipc.TaskShellLifecycleRequest{
		TaskID:     common.TaskID("tsk_shell"),
		SessionID:  "shs_123",
		Kind:       "host_exited",
		HostMode:   "codex-pty",
		HostState:  "exited",
		Note:       "codex exited with code 9",
		InputLive:  false,
		ExitCode:   &exitCode,
		PaneWidth:  80,
		PaneHeight: 24,
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_4",
		Method:    ipc.MethodTaskShellLifecycle,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.SessionID != "shs_123" || captured.Kind != orchestrator.ShellLifecycleHostExited {
		t.Fatalf("unexpected shell lifecycle request: %+v", captured)
	}
}

func TestHandleRequestShellSessionReportRoute(t *testing.T) {
	var captured orchestrator.ReportShellSessionRequest
	handler := &fakeOrchestratorService{
		reportShellSessionFn: func(_ context.Context, req orchestrator.ReportShellSessionRequest) (orchestrator.ReportShellSessionResult, error) {
			captured = req
			return orchestrator.ReportShellSessionResult{
				TaskID: common.TaskID(req.TaskID),
				Session: orchestrator.ShellSessionView{
					TaskID:                common.TaskID(req.TaskID),
					SessionID:             req.SessionID,
					WorkerPreference:      req.WorkerPreference,
					ResolvedWorker:        req.ResolvedWorker,
					WorkerSessionID:       req.WorkerSessionID,
					WorkerSessionIDSource: req.WorkerSessionIDSource,
					AttachCapability:      req.AttachCapability,
					HostMode:              req.HostMode,
					HostState:             req.HostState,
					StartedAt:             req.StartedAt,
					Active:                req.Active,
					Note:                  req.Note,
					SessionClass:          orchestrator.ShellSessionClassAttachable,
					SessionClassReason:    "active PTY session with authoritative worker session id and attach capability",
					ReattachGuidance:      "reattach with `tuku shell --task tsk_shell --reattach shs_456`",
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskShellSessionReportRequest{
		TaskID:                common.TaskID("tsk_shell"),
		SessionID:             "shs_456",
		WorkerPreference:      "auto",
		ResolvedWorker:        "claude",
		WorkerSessionID:       "wks_456",
		WorkerSessionIDSource: "authoritative",
		AttachCapability:      "attachable",
		HostMode:              "claude-pty",
		HostState:             "starting",
		Active:                true,
		Note:                  "shell session registered",
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_5",
		Method:    ipc.MethodTaskShellSessionReport,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.SessionID != "shs_456" || captured.ResolvedWorker != "claude" {
		t.Fatalf("unexpected shell session report request: %+v", captured)
	}
	if captured.WorkerSessionIDSource != "authoritative" {
		t.Fatalf("expected authoritative session-id source, got %+v", captured)
	}
	var out ipc.TaskShellSessionReportResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal shell session report response: %v", err)
	}
	if out.Session.SessionClass != "attachable" || out.Session.WorkerSessionID != "wks_456" || out.Session.AttachCapability != "attachable" {
		t.Fatalf("expected active session class, got %+v", out.Session)
	}
	if out.Session.WorkerSessionIDSource != "authoritative" {
		t.Fatalf("expected authoritative session-id source in response, got %+v", out.Session)
	}
}

func TestHandleRequestShellTranscriptAppendRoute(t *testing.T) {
	var captured orchestrator.RecordShellTranscriptRequest
	handler := &fakeOrchestratorService{
		recordShellTranscriptFn: func(_ context.Context, req orchestrator.RecordShellTranscriptRequest) (orchestrator.RecordShellTranscriptResult, error) {
			captured = req
			return orchestrator.RecordShellTranscriptResult{
				TaskID:    common.TaskID(req.TaskID),
				SessionID: req.SessionID,
				Summary: shellsession.TranscriptSummary{
					TaskID:         common.TaskID(req.TaskID),
					SessionID:      req.SessionID,
					RetainedChunks: 3,
					DroppedChunks:  1,
					RetentionLimit: 200,
					LastSequenceNo: 42,
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)
	payload, _ := json.Marshal(ipc.TaskShellTranscriptAppendRequest{
		TaskID:    common.TaskID("tsk_shell"),
		SessionID: "shs_1",
		Chunks: []ipc.TaskShellTranscriptChunkAppend{
			{Source: "worker_output", Content: "line 1"},
			{Source: "worker_output", Content: "line 2"},
		},
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_transcript_append",
		Method:    ipc.MethodTaskShellTranscriptAppend,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_shell" || captured.SessionID != "shs_1" || len(captured.Chunks) != 2 {
		t.Fatalf("unexpected transcript append request: %+v", captured)
	}
	var out ipc.TaskShellTranscriptAppendResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal transcript append response: %v", err)
	}
	if out.RetainedChunks != 3 || out.DroppedChunks != 1 || out.RetentionLimit != 200 || out.LastSequenceNo != 42 {
		t.Fatalf("unexpected transcript append response payload: %+v", out)
	}
}

func TestHandleRequestShellTranscriptReadRoute(t *testing.T) {
	var captured orchestrator.ReadShellTranscriptRequest
	handler := &fakeOrchestratorService{
		readShellTranscriptFn: func(_ context.Context, req orchestrator.ReadShellTranscriptRequest) (orchestrator.ReadShellTranscriptResult, error) {
			captured = req
			next := int64(81)
			before := int64(121)
			return orchestrator.ReadShellTranscriptResult{
				TaskID:                  common.TaskID(req.TaskID),
				SessionID:               req.SessionID,
				TranscriptState:         shellsession.TranscriptStateTranscriptOnlyPartial,
				TranscriptOnly:          true,
				Bounded:                 true,
				Partial:                 true,
				RetentionLimit:          200,
				RetainedChunkCount:      200,
				DroppedChunkCount:       57,
				LastSequence:            280,
				OldestRetainedSequence:  81,
				NewestRetainedSequence:  280,
				RequestedLimit:          40,
				RequestedBeforeSequence: &before,
				RequestedSource:         shellsession.TranscriptSourceWorkerOutput,
				PageOldestSequence:      81,
				PageNewestSequence:      120,
				PageChunkCount:          40,
				HasMoreOlder:            true,
				NextBeforeSequence:      &next,
				LatestReview: &orchestrator.ShellTranscriptReviewSummary{
					ReviewID:               "srev_123",
					SourceFilter:           shellsession.TranscriptSourceWorkerOutput,
					ReviewedUpToSequence:   110,
					Summary:                "reviewed bounded worker output",
					CreatedAt:              time.Unix(1710000060, 0).UTC(),
					TranscriptState:        shellsession.TranscriptStateTranscriptOnlyPartial,
					RetentionLimit:         200,
					RetainedChunks:         200,
					DroppedChunks:          57,
					OldestRetainedSequence: 81,
					NewestRetainedSequence: 280,
					StaleBehindLatest:      true,
					NewerRetainedCount:     170,
				},
				HasUnreadNewerEvidence: true,
				PageFullyReviewed:      false,
				PageCrossesReview:      true,
				PageHasUnreviewed:      true,
				Closure: orchestrator.ShellTranscriptReviewClosure{
					State:                    shellsession.TranscriptReviewClosureSourceScopedStale,
					Scope:                    shellsession.TranscriptSourceWorkerOutput,
					HasReview:                true,
					HasUnreadNewerEvidence:   true,
					ReviewedUpToSequence:     110,
					OldestUnreviewedSequence: 111,
					NewestRetainedSequence:   280,
					UnreviewedRetainedCount:  170,
					RetentionLimit:           200,
					RetainedChunkCount:       200,
					DroppedChunkCount:        57,
				},
				SourceSummary: []orchestrator.ShellTranscriptSourceSummary{
					{Source: shellsession.TranscriptSourceFallback, Chunks: 12},
					{Source: shellsession.TranscriptSourceWorkerOutput, Chunks: 188},
				},
				Chunks: []shellsession.TranscriptChunk{
					{ChunkID: "sst_81", TaskID: common.TaskID(req.TaskID), SessionID: req.SessionID, SequenceNo: 81, Source: shellsession.TranscriptSourceWorkerOutput, Content: "line 81", CreatedAt: time.Unix(1710000000, 0).UTC()},
					{ChunkID: "sst_120", TaskID: common.TaskID(req.TaskID), SessionID: req.SessionID, SequenceNo: 120, Source: shellsession.TranscriptSourceWorkerOutput, Content: "line 120", CreatedAt: time.Unix(1710000040, 0).UTC()},
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)
	payload, _ := json.Marshal(ipc.TaskShellTranscriptReadRequest{
		TaskID:         common.TaskID("tsk_shell"),
		SessionID:      "shs_1",
		Limit:          40,
		BeforeSequence: 121,
		Source:         "worker_output",
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_transcript_read",
		Method:    ipc.MethodTaskShellTranscriptRead,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_shell" || captured.SessionID != "shs_1" || captured.Limit != 40 || captured.BeforeSequence != 121 || captured.Source != "worker_output" {
		t.Fatalf("unexpected transcript read request: %+v", captured)
	}
	var out ipc.TaskShellTranscriptReadResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal transcript read response: %v", err)
	}
	if out.TranscriptState != "transcript_only_bounded_partial" || !out.Partial || !out.Bounded || !out.TranscriptOnly {
		t.Fatalf("unexpected transcript state payload: %+v", out)
	}
	if out.RetainedChunkCount != 200 || out.DroppedChunkCount != 57 || out.PageChunkCount != 40 || !out.HasMoreOlder || out.NextBeforeSequence != 81 {
		t.Fatalf("unexpected transcript pagination payload: %+v", out)
	}
	if out.LatestReview == nil || out.LatestReview.ReviewID != "srev_123" || !out.HasUnreadNewerEvidence || !out.PageCrossesReview {
		t.Fatalf("expected transcript review metadata in read payload, got %+v", out)
	}
	if out.Closure.State != string(shellsession.TranscriptReviewClosureSourceScopedStale) || out.Closure.OldestUnreviewedSequence != 111 || out.Closure.UnreviewedRetainedCount != 170 {
		t.Fatalf("expected closure metadata in read payload, got %+v", out.Closure)
	}
	if len(out.SourceSummary) != 2 || len(out.Chunks) != 2 {
		t.Fatalf("expected source summary and chunk payload, got %+v", out)
	}
}

func TestHandleRequestShellTranscriptReviewRoute(t *testing.T) {
	var captured orchestrator.RecordShellTranscriptReviewRequest
	handler := &fakeOrchestratorService{
		recordShellTranscriptReviewFn: func(_ context.Context, req orchestrator.RecordShellTranscriptReviewRequest) (orchestrator.RecordShellTranscriptReviewResult, error) {
			captured = req
			return orchestrator.RecordShellTranscriptReviewResult{
				TaskID:                 common.TaskID(req.TaskID),
				SessionID:              req.SessionID,
				TranscriptState:        shellsession.TranscriptStateTranscriptOnlyPartial,
				RetentionLimit:         200,
				RetainedChunkCount:     200,
				DroppedChunkCount:      57,
				OldestRetainedSequence: 81,
				NewestRetainedSequence: 280,
				LatestReview: orchestrator.ShellTranscriptReviewSummary{
					ReviewID:                 "srev_123",
					SourceFilter:             shellsession.TranscriptSourceWorkerOutput,
					ReviewedUpToSequence:     req.ReviewedUpToSeq,
					Summary:                  req.Summary,
					CreatedAt:                time.Unix(1710000100, 0).UTC(),
					TranscriptState:          shellsession.TranscriptStateTranscriptOnlyPartial,
					RetentionLimit:           200,
					RetainedChunks:           200,
					DroppedChunks:            57,
					OldestRetainedSequence:   81,
					NewestRetainedSequence:   280,
					StaleBehindLatest:        true,
					NewerRetainedCount:       100,
					OldestUnreviewedSequence: 181,
					ClosureState:             shellsession.TranscriptReviewClosureSourceScopedStale,
				},
				HasUnreadNewerEvidence: true,
				Closure: orchestrator.ShellTranscriptReviewClosure{
					State:                    shellsession.TranscriptReviewClosureSourceScopedStale,
					Scope:                    shellsession.TranscriptSourceWorkerOutput,
					HasReview:                true,
					HasUnreadNewerEvidence:   true,
					ReviewedUpToSequence:     req.ReviewedUpToSeq,
					OldestUnreviewedSequence: 181,
					NewestRetainedSequence:   280,
					UnreviewedRetainedCount:  100,
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)
	payload, _ := json.Marshal(ipc.TaskShellTranscriptReviewRequest{
		TaskID:          common.TaskID("tsk_shell"),
		SessionID:       "shs_1",
		ReviewedUpToSeq: 180,
		Source:          "worker_output",
		Summary:         "reviewed up to sequence 180",
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_transcript_review",
		Method:    ipc.MethodTaskShellTranscriptReview,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_shell" || captured.SessionID != "shs_1" || captured.ReviewedUpToSeq != 180 || captured.Source != "worker_output" {
		t.Fatalf("unexpected transcript review request: %+v", captured)
	}
	var out ipc.TaskShellTranscriptReviewResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal transcript review response: %v", err)
	}
	if out.LatestReview.ReviewID != "srev_123" || out.LatestReview.ReviewedUpToSequence != 180 || !out.HasUnreadNewerEvidence {
		t.Fatalf("unexpected transcript review payload: %+v", out)
	}
	if out.Closure.State != string(shellsession.TranscriptReviewClosureSourceScopedStale) || out.Closure.UnreviewedRetainedCount != 100 {
		t.Fatalf("unexpected transcript review closure payload: %+v", out.Closure)
	}
}

func TestHandleRequestShellTranscriptHistoryRoute(t *testing.T) {
	var captured orchestrator.ReadShellTranscriptReviewHistoryRequest
	handler := &fakeOrchestratorService{
		readShellTranscriptReviewHistoryFn: func(_ context.Context, req orchestrator.ReadShellTranscriptReviewHistoryRequest) (orchestrator.ReadShellTranscriptReviewHistoryResult, error) {
			captured = req
			latest := orchestrator.ShellTranscriptReviewSummary{
				ReviewID:                 "srev_200",
				SourceFilter:             shellsession.TranscriptSourceWorkerOutput,
				ReviewedUpToSequence:     210,
				Summary:                  "reviewed worker output boundary",
				CreatedAt:                time.Unix(1710000200, 0).UTC(),
				StaleBehindLatest:        true,
				NewerRetainedCount:       15,
				OldestUnreviewedSequence: 211,
				ClosureState:             shellsession.TranscriptReviewClosureSourceScopedStale,
			}
			return orchestrator.ReadShellTranscriptReviewHistoryResult{
				TaskID:                 common.TaskID(req.TaskID),
				SessionID:              req.SessionID,
				TranscriptState:        shellsession.TranscriptStateTranscriptOnlyPartial,
				TranscriptOnly:         true,
				Bounded:                true,
				Partial:                true,
				RetentionLimit:         200,
				RetainedChunkCount:     200,
				DroppedChunkCount:      20,
				OldestRetainedSequence: 26,
				NewestRetainedSequence: 225,
				RequestedLimit:         5,
				RequestedSource:        shellsession.TranscriptSourceWorkerOutput,
				Closure: orchestrator.ShellTranscriptReviewClosure{
					State:                    shellsession.TranscriptReviewClosureSourceScopedStale,
					Scope:                    shellsession.TranscriptSourceWorkerOutput,
					HasReview:                true,
					HasUnreadNewerEvidence:   true,
					ReviewedUpToSequence:     210,
					OldestUnreviewedSequence: 211,
					NewestRetainedSequence:   225,
					UnreviewedRetainedCount:  15,
				},
				LatestReview: &latest,
				Reviews: []orchestrator.ShellTranscriptReviewSummary{
					latest,
					{
						ReviewID:             "srev_199",
						SourceFilter:         "",
						ReviewedUpToSequence: 190,
						Summary:              "global retained review",
						CreatedAt:            time.Unix(1710000100, 0).UTC(),
						StaleBehindLatest:    true,
						NewerRetainedCount:   35,
						ClosureState:         shellsession.TranscriptReviewClosureGlobalStale,
					},
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)
	payload, _ := json.Marshal(ipc.TaskShellTranscriptHistoryRequest{
		TaskID:    common.TaskID("tsk_shell"),
		SessionID: "shs_1",
		Source:    "worker_output",
		Limit:     5,
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_transcript_history",
		Method:    ipc.MethodTaskShellTranscriptHistory,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_shell" || captured.SessionID != "shs_1" || captured.Source != "worker_output" || captured.Limit != 5 {
		t.Fatalf("unexpected transcript history request: %+v", captured)
	}
	var out ipc.TaskShellTranscriptHistoryResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal transcript history response: %v", err)
	}
	if out.Closure.State != string(shellsession.TranscriptReviewClosureSourceScopedStale) || out.Closure.OldestUnreviewedSequence != 211 {
		t.Fatalf("unexpected transcript history closure payload: %+v", out.Closure)
	}
	if out.LatestReview == nil || out.LatestReview.ReviewID != "srev_200" || len(out.Reviews) != 2 {
		t.Fatalf("unexpected transcript history payload: %+v", out)
	}
}

func TestHandleRequestContinuityTransitionHistoryRoute(t *testing.T) {
	var captured orchestrator.ReadContinuityTransitionHistoryRequest
	handler := &fakeOrchestratorService{
		readContinuityTransitionHistoryFn: func(_ context.Context, req orchestrator.ReadContinuityTransitionHistoryRequest) (orchestrator.ReadContinuityTransitionHistoryResult, error) {
			captured = req
			latest := orchestrator.ContinuityTransitionReceiptSummary{
				ReceiptID:             "ctr_300",
				TaskID:                common.TaskID(req.TaskID),
				TransitionKind:        transition.KindHandoffLaunch,
				HandoffID:             "hnd_1",
				HandoffStateBefore:    orchestrator.HandoffContinuityStateAcceptedNotLaunched,
				HandoffStateAfter:     orchestrator.HandoffContinuityStateLaunchCompletedAckSeen,
				ReviewGapPresent:      true,
				ReviewPosture:         transition.ReviewPostureGlobalReviewStale,
				AcknowledgmentPresent: true,
				Summary:               "handoff launch transition recorded under explicit review-gap acknowledgment",
				CreatedAt:             time.Unix(1710001200, 0).UTC(),
			}
			return orchestrator.ReadContinuityTransitionHistoryResult{
				TaskID:                   common.TaskID(req.TaskID),
				Bounded:                  true,
				RequestedLimit:           5,
				RequestedBeforeReceiptID: "ctr_250",
				RequestedTransitionKind:  transition.KindHandoffLaunch,
				RequestedHandoffID:       "hnd_1",
				HasMoreOlder:             true,
				NextBeforeReceiptID:      "ctr_280",
				Latest:                   &latest,
				Receipts: []orchestrator.ContinuityTransitionReceiptSummary{
					latest,
					{
						ReceiptID:             "ctr_280",
						TaskID:                common.TaskID(req.TaskID),
						TransitionKind:        transition.KindHandoffLaunch,
						HandoffID:             "hnd_1",
						HandoffStateBefore:    orchestrator.HandoffContinuityStateAcceptedNotLaunched,
						HandoffStateAfter:     orchestrator.HandoffContinuityStateLaunchCompletedAckSeen,
						ReviewGapPresent:      true,
						ReviewPosture:         transition.ReviewPostureGlobalReviewStale,
						AcknowledgmentPresent: false,
						CreatedAt:             time.Unix(1710001100, 0).UTC(),
					},
				},
				RiskSummary: orchestrator.ContinuityTransitionRiskSummary{
					WindowSize:                           2,
					ReviewGapTransitions:                 2,
					AcknowledgedReviewGapTransitions:     1,
					UnacknowledgedReviewGapTransitions:   1,
					StaleReviewPostureTransitions:        2,
					SourceScopedReviewPostureTransitions: 0,
					IntoClaudeOwnershipTransitions:       1,
					BackToLocalOwnershipTransitions:      0,
					OperationallyNotable:                 true,
					Summary:                              "2 transition(s) recorded with unacknowledged transcript review gaps",
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)
	payload, _ := json.Marshal(ipc.TaskTransitionHistoryRequest{
		TaskID:          common.TaskID("tsk_transitions"),
		Limit:           5,
		BeforeReceiptID: "ctr_250",
		TransitionKind:  "HANDOFF_LAUNCH",
		HandoffID:       "hnd_1",
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_transition_history",
		Method:    ipc.MethodTaskTransitionHistory,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_transitions" || captured.Limit != 5 || captured.BeforeReceiptID != "ctr_250" || captured.TransitionKind != "HANDOFF_LAUNCH" || captured.HandoffID != "hnd_1" {
		t.Fatalf("unexpected transition history request: %+v", captured)
	}
	var out ipc.TaskTransitionHistoryResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal transition history response: %v", err)
	}
	if out.RequestedTransitionKind != "HANDOFF_LAUNCH" || out.NextBeforeReceiptID != "ctr_280" || !out.HasMoreOlder {
		t.Fatalf("unexpected transition history response metadata: %+v", out)
	}
	if out.Latest == nil || out.Latest.ReceiptID != "ctr_300" || len(out.Receipts) != 2 {
		t.Fatalf("unexpected transition history response receipts: %+v", out)
	}
	if out.RiskSummary.UnacknowledgedReviewGapTransitions != 1 || !out.RiskSummary.OperationallyNotable {
		t.Fatalf("unexpected transition history risk summary mapping: %+v", out.RiskSummary)
	}
}

func TestHandleRequestContinuityIncidentSliceRoute(t *testing.T) {
	var captured orchestrator.ReadContinuityIncidentSliceRequest
	handler := &fakeOrchestratorService{
		readContinuityIncidentSliceFn: func(_ context.Context, req orchestrator.ReadContinuityIncidentSliceRequest) (orchestrator.ReadContinuityIncidentSliceResult, error) {
			captured = req
			anchor := orchestrator.ContinuityTransitionReceiptSummary{
				ReceiptID:             "ctr_anchor",
				TaskID:                common.TaskID(req.TaskID),
				TransitionKind:        transition.KindHandoffLaunch,
				HandoffStateBefore:    orchestrator.HandoffContinuityStateAcceptedNotLaunched,
				HandoffStateAfter:     orchestrator.HandoffContinuityStateLaunchCompletedAckSeen,
				ReviewGapPresent:      true,
				ReviewPosture:         transition.ReviewPostureGlobalReviewStale,
				AcknowledgmentPresent: true,
				CreatedAt:             time.Unix(1711000000, 0).UTC(),
			}
			latestReview := orchestrator.ShellTranscriptReviewSummary{
				ReviewID:             "srev_300",
				ReviewedUpToSequence: 220,
				ClosureState:         shellsession.TranscriptReviewClosureGlobalStale,
				CreatedAt:            time.Unix(1711000020, 0).UTC(),
			}
			latestAck := orchestrator.TranscriptReviewGapAcknowledgmentSummary{
				AcknowledgmentID: "sack_300",
				TaskID:           common.TaskID(req.TaskID),
				SessionID:        "shs_incident",
				Class:            shellsession.TranscriptReviewGapAckStaleReview,
				ReviewState:      "global_review_stale",
				CreatedAt:        time.Unix(1711000030, 0).UTC(),
			}
			return orchestrator.ReadContinuityIncidentSliceResult{
				TaskID:                             common.TaskID(req.TaskID),
				Bounded:                            true,
				AnchorMode:                         orchestrator.ContinuityIncidentAnchorTransitionID,
				RequestedAnchorTransitionReceiptID: "ctr_anchor",
				Anchor:                             anchor,
				TransitionNeighborLimit:            2,
				RunLimit:                           3,
				RecoveryLimit:                      3,
				ProofLimit:                         6,
				AckLimit:                           2,
				HasOlderTransitionsOutsideWindow:   true,
				HasNewerTransitionsOutsideWindow:   false,
				WindowStartAt:                      time.Unix(1710999980, 0).UTC(),
				WindowEndAt:                        time.Unix(1711000060, 0).UTC(),
				Transitions: []orchestrator.ContinuityTransitionReceiptSummary{
					anchor,
				},
				Runs: []orchestrator.ContinuityIncidentRunSummary{
					{
						RunID:      common.RunID("run_300"),
						WorkerKind: run.WorkerKindCodex,
						Status:     run.StatusFailed,
						OccurredAt: time.Unix(1711000010, 0).UTC(),
						StartedAt:  time.Unix(1711000000, 0).UTC(),
						Summary:    "run failed with validation issues",
					},
				},
				RecoveryActions: []orchestrator.ContinuityIncidentRecoveryActionSummary{
					{
						ActionID:  "ract_300",
						Kind:      recoveryaction.KindFailedRunReviewed,
						Summary:   "reviewed failed run evidence",
						CreatedAt: time.Unix(1711000040, 0).UTC(),
					},
				},
				ProofEvents: []orchestrator.ContinuityIncidentProofSummary{
					{
						EventID:    "evt_300",
						Type:       proof.EventBranchHandoffTransitionRecorded,
						ActorType:  proof.ActorSystem,
						ActorID:    "tuku-daemon",
						Timestamp:  time.Unix(1711000005, 0).UTC(),
						Summary:    "Branch/handoff transition receipt recorded",
						SequenceNo: 300,
					},
				},
				LatestTranscriptReview:        &latestReview,
				LatestTranscriptReviewGapAck:  &latestAck,
				RecentTranscriptReviewGapAcks: []orchestrator.TranscriptReviewGapAcknowledgmentSummary{latestAck},
				RiskSummary: orchestrator.ContinuityIncidentRiskSummary{
					ReviewGapPresent:                true,
					AcknowledgmentPresent:           true,
					StaleOrUnreviewedReviewPosture:  true,
					SourceScopedReviewPosture:       false,
					IntoClaudeOwnershipTransition:   true,
					BackToLocalOwnershipTransition:  false,
					UnresolvedContinuityAmbiguity:   true,
					NearbyFailedOrInterruptedRuns:   1,
					NearbyRecoveryActions:           1,
					RecentFailureOrRecoveryActivity: true,
					OperationallyNotable:            true,
					Summary:                         "anchor transition carried stale retained transcript posture with nearby failed run evidence",
				},
				Caveat: "bounded incident slice caveat",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)
	payload, _ := json.Marshal(ipc.TaskContinuityIncidentSliceRequest{
		TaskID:                    common.TaskID("tsk_incident"),
		AnchorTransitionReceiptID: "ctr_anchor",
		TransitionNeighborLimit:   2,
		RunLimit:                  3,
		RecoveryLimit:             3,
		ProofLimit:                6,
		AckLimit:                  2,
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_incident_slice",
		Method:    ipc.MethodTaskContinuityIncidentSlice,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_incident" || captured.AnchorTransitionReceiptID != "ctr_anchor" || captured.TransitionNeighborLimit != 2 || captured.RunLimit != 3 || captured.RecoveryLimit != 3 || captured.ProofLimit != 6 || captured.AckLimit != 2 {
		t.Fatalf("unexpected incident slice request: %+v", captured)
	}
	var out ipc.TaskContinuityIncidentSliceResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal incident slice response: %v", err)
	}
	if out.Anchor.ReceiptID != "ctr_anchor" || out.AnchorMode != string(orchestrator.ContinuityIncidentAnchorTransitionID) {
		t.Fatalf("unexpected incident anchor payload: %+v", out)
	}
	if len(out.Transitions) != 1 || len(out.Runs) != 1 || len(out.RecoveryActions) != 1 || len(out.ProofEvents) != 1 {
		t.Fatalf("unexpected incident evidence payload counts: %+v", out)
	}
	if out.RiskSummary.NearbyFailedOrInterruptedRuns != 1 || !out.RiskSummary.OperationallyNotable {
		t.Fatalf("unexpected incident risk summary payload: %+v", out.RiskSummary)
	}
	if out.LatestTranscriptReviewGapAck == nil || out.LatestTranscriptReviewGapAck.AcknowledgmentID != "sack_300" {
		t.Fatalf("unexpected latest transcript review-gap acknowledgment payload: %+v", out.LatestTranscriptReviewGapAck)
	}
}

func TestHandleRequestContinuityIncidentTriageRoute(t *testing.T) {
	var captured orchestrator.RecordContinuityIncidentTriageRequest
	handler := &fakeOrchestratorService{
		recordContinuityIncidentTriageFn: func(_ context.Context, req orchestrator.RecordContinuityIncidentTriageRequest) (orchestrator.RecordContinuityIncidentTriageResult, error) {
			captured = req
			latestTransition := orchestrator.ContinuityTransitionReceiptSummary{
				ReceiptID:      "ctr_triage_anchor",
				TaskID:         common.TaskID(req.TaskID),
				TransitionKind: transition.KindHandoffLaunch,
				CreatedAt:      time.Unix(1711000200, 0).UTC(),
			}
			receipt := orchestrator.ContinuityIncidentTriageReceiptSummary{
				ReceiptID:                 "citr_500",
				TaskID:                    common.TaskID(req.TaskID),
				AnchorMode:                "LATEST_TRANSITION",
				AnchorTransitionReceiptID: "ctr_triage_anchor",
				AnchorTransitionKind:      transition.KindHandoffLaunch,
				Posture:                   "NEEDS_FOLLOW_UP",
				FollowUpPosture:           "ADVISORY_OPEN",
				Summary:                   "triaged incident with follow-up advisory open",
				RiskSummary: orchestrator.ContinuityIncidentRiskSummary{
					ReviewGapPresent:               true,
					StaleOrUnreviewedReviewPosture: true,
					OperationallyNotable:           true,
					Summary:                        "anchor transition carried stale retained transcript posture",
				},
				CreatedAt: time.Unix(1711000210, 0).UTC(),
			}
			followUp := orchestrator.ContinuityIncidentFollowUpSummary{
				State:                     orchestrator.ContinuityIncidentFollowUpNeedsFollowUp,
				Advisory:                  "Latest continuity incident triage is marked NEEDS_FOLLOW_UP; operator follow-up is still advised.",
				FollowUpAdvised:           true,
				NeedsFollowUp:             true,
				LatestTransitionReceiptID: latestTransition.ReceiptID,
				LatestTriageReceiptID:     receipt.ReceiptID,
				TriageAnchorReceiptID:     receipt.AnchorTransitionReceiptID,
				TriagePosture:             receipt.Posture,
			}
			return orchestrator.RecordContinuityIncidentTriageResult{
				TaskID:                          common.TaskID(req.TaskID),
				AnchorMode:                      "LATEST_TRANSITION",
				AnchorTransitionReceiptID:       "ctr_triage_anchor",
				Posture:                         "NEEDS_FOLLOW_UP",
				Reused:                          false,
				Receipt:                         receipt,
				LatestContinuityTransition:      &latestTransition,
				RecentContinuityIncidentTriages: []orchestrator.ContinuityIncidentTriageReceiptSummary{receipt},
				FollowUp:                        &followUp,
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)
	payload, _ := json.Marshal(ipc.TaskContinuityIncidentTriageRequest{
		TaskID:                    common.TaskID("tsk_triage"),
		AnchorMode:                "latest",
		AnchorTransitionReceiptID: "",
		Posture:                   "NEEDS_FOLLOW_UP",
		Summary:                   "triaged incident with follow-up advisory open",
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_incident_triage",
		Method:    ipc.MethodTaskContinuityIncidentTriage,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_triage" || captured.AnchorMode != "latest" || captured.Posture != "NEEDS_FOLLOW_UP" {
		t.Fatalf("unexpected incident triage request payload mapping: %+v", captured)
	}
	var out ipc.TaskContinuityIncidentTriageResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal continuity incident triage response: %v", err)
	}
	if out.Receipt.ReceiptID != "citr_500" || out.AnchorTransitionReceiptID != "ctr_triage_anchor" {
		t.Fatalf("unexpected continuity incident triage response receipt mapping: %+v", out)
	}
	if out.ContinuityIncidentFollowUp == nil || out.ContinuityIncidentFollowUp.State != string(orchestrator.ContinuityIncidentFollowUpNeedsFollowUp) {
		t.Fatalf("unexpected continuity incident follow-up mapping: %+v", out.ContinuityIncidentFollowUp)
	}
	if len(out.RecentContinuityIncidentTriages) != 1 {
		t.Fatalf("expected bounded recent triage history in response, got %+v", out.RecentContinuityIncidentTriages)
	}
}

func TestHandleRequestContinuityIncidentTriageHistoryRoute(t *testing.T) {
	var captured orchestrator.ReadContinuityIncidentTriageHistoryRequest
	handler := &fakeOrchestratorService{
		readContinuityIncidentTriageHistoryFn: func(_ context.Context, req orchestrator.ReadContinuityIncidentTriageHistoryRequest) (orchestrator.ReadContinuityIncidentTriageHistoryResult, error) {
			captured = req
			latest := orchestrator.ContinuityIncidentTriageReceiptSummary{
				ReceiptID:                 "citr_610",
				TaskID:                    common.TaskID(req.TaskID),
				AnchorMode:                "LATEST_TRANSITION",
				AnchorTransitionReceiptID: "ctr_600",
				AnchorTransitionKind:      transition.KindHandoffLaunch,
				Posture:                   "NEEDS_FOLLOW_UP",
				FollowUpPosture:           "ADVISORY_OPEN",
				Summary:                   "follow-up remains open for anchor ctr_600",
				CreatedAt:                 time.Unix(1711000600, 0).UTC(),
			}
			return orchestrator.ReadContinuityIncidentTriageHistoryResult{
				TaskID:                             common.TaskID(req.TaskID),
				Bounded:                            true,
				RequestedLimit:                     4,
				RequestedBeforeReceiptID:           "citr_590",
				RequestedAnchorTransitionReceiptID: "ctr_600",
				RequestedPosture:                   "NEEDS_FOLLOW_UP",
				HasMoreOlder:                       true,
				NextBeforeReceiptID:                "citr_605",
				LatestTransitionReceiptID:          "ctr_620",
				Latest:                             &latest,
				Receipts: []orchestrator.ContinuityIncidentTriageReceiptSummary{
					latest,
					{
						ReceiptID:                 "citr_605",
						TaskID:                    common.TaskID(req.TaskID),
						AnchorMode:                "TRANSITION_RECEIPT_ID",
						AnchorTransitionReceiptID: "ctr_600",
						AnchorTransitionKind:      transition.KindHandoffLaunch,
						Posture:                   "NEEDS_FOLLOW_UP",
						FollowUpPosture:           "ADVISORY_OPEN",
						CreatedAt:                 time.Unix(1711000500, 0).UTC(),
					},
				},
				Rollup: orchestrator.ContinuityIncidentTriageHistoryRollupSummary{
					WindowSize:                        2,
					BoundedWindow:                     true,
					DistinctAnchors:                   1,
					AnchorsNeedsFollowUp:              1,
					AnchorsWithOpenFollowUp:           1,
					AnchorsBehindLatestTransition:     1,
					AnchorsRepeatedWithoutProgression: 1,
					ReviewRiskReceipts:                1,
					OperationallyNotable:              true,
					Summary:                           "1 anchor(s) remain in open follow-up posture",
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)
	payload, _ := json.Marshal(ipc.TaskContinuityIncidentTriageHistoryRequest{
		TaskID:                    common.TaskID("tsk_triage_history"),
		Limit:                     4,
		BeforeReceiptID:           "citr_590",
		AnchorTransitionReceiptID: "ctr_600",
		Posture:                   "NEEDS_FOLLOW_UP",
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_incident_triage_history",
		Method:    ipc.MethodTaskContinuityIncidentTriageHistory,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_triage_history" || captured.Limit != 4 || captured.BeforeReceiptID != "citr_590" || captured.AnchorTransitionReceiptID != "ctr_600" || captured.Posture != "NEEDS_FOLLOW_UP" {
		t.Fatalf("unexpected incident triage history request payload mapping: %+v", captured)
	}
	var out ipc.TaskContinuityIncidentTriageHistoryResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal continuity incident triage history response: %v", err)
	}
	if out.RequestedPosture != "NEEDS_FOLLOW_UP" || out.NextBeforeReceiptID != "citr_605" || !out.HasMoreOlder {
		t.Fatalf("unexpected continuity incident triage history metadata mapping: %+v", out)
	}
	if out.Latest == nil || out.Latest.ReceiptID != "citr_610" || len(out.Receipts) != 2 {
		t.Fatalf("unexpected continuity incident triage history receipt mapping: %+v", out)
	}
	if out.Rollup.AnchorsWithOpenFollowUp != 1 || !out.Rollup.OperationallyNotable {
		t.Fatalf("unexpected continuity incident triage history rollup mapping: %+v", out.Rollup)
	}
}

func TestHandleRequestContinuityIncidentFollowUpRoute(t *testing.T) {
	var captured orchestrator.RecordContinuityIncidentFollowUpRequest
	handler := &fakeOrchestratorService{
		recordContinuityIncidentFollowUpFn: func(_ context.Context, req orchestrator.RecordContinuityIncidentFollowUpRequest) (orchestrator.RecordContinuityIncidentFollowUpResult, error) {
			captured = req
			latestTransition := orchestrator.ContinuityTransitionReceiptSummary{
				ReceiptID:      "ctr_followup_anchor",
				TaskID:         common.TaskID(req.TaskID),
				TransitionKind: transition.KindHandoffLaunch,
				CreatedAt:      time.Unix(1712000200, 0).UTC(),
			}
			latestTriage := orchestrator.ContinuityIncidentTriageReceiptSummary{
				ReceiptID:                 "citr_followup_anchor",
				TaskID:                    common.TaskID(req.TaskID),
				AnchorMode:                incidenttriage.AnchorModeLatestTransition,
				AnchorTransitionReceiptID: "ctr_followup_anchor",
				AnchorTransitionKind:      transition.KindHandoffLaunch,
				Posture:                   incidenttriage.PostureNeedsFollowUp,
				FollowUpPosture:           incidenttriage.FollowUpPostureAdvisory,
				CreatedAt:                 time.Unix(1712000210, 0).UTC(),
			}
			receipt := orchestrator.ContinuityIncidentFollowUpReceiptSummary{
				ReceiptID:                 "cifr_700",
				TaskID:                    common.TaskID(req.TaskID),
				AnchorMode:                incidenttriage.AnchorModeLatestTransition,
				AnchorTransitionReceiptID: "ctr_followup_anchor",
				AnchorTransitionKind:      transition.KindHandoffLaunch,
				TriageReceiptID:           "citr_followup_anchor",
				TriagePosture:             incidenttriage.PostureNeedsFollowUp,
				TriageFollowUpState:       incidenttriage.FollowUpPostureAdvisory,
				ActionKind:                incidenttriage.FollowUpActionProgressed,
				Summary:                   "validated downstream handoff context and advanced follow-up",
				ReviewGapPresent:          true,
				ReviewPosture:             transition.ReviewPostureGlobalReviewStale,
				AcknowledgmentPresent:     true,
				TriagedUnderReviewRisk:    true,
				CreatedAt:                 time.Unix(1712000220, 0).UTC(),
			}
			followUp := orchestrator.ContinuityIncidentFollowUpSummary{
				State:                     orchestrator.ContinuityIncidentFollowUpProgressed,
				Digest:                    "follow-up open",
				WindowAdvisory:            "bounded window open=1",
				Advisory:                  "Latest incident follow-up receipt is PROGRESSED; explicit closure remains open.",
				FollowUpAdvised:           true,
				NeedsFollowUp:             true,
				TriagedUnderReviewRisk:    true,
				LatestTransitionReceiptID: latestTransition.ReceiptID,
				LatestTriageReceiptID:     latestTriage.ReceiptID,
				TriageAnchorReceiptID:     latestTriage.AnchorTransitionReceiptID,
				TriagePosture:             latestTriage.Posture,
				LatestFollowUpReceiptID:   receipt.ReceiptID,
				LatestFollowUpActionKind:  receipt.ActionKind,
				LatestFollowUpSummary:     receipt.Summary,
				LatestFollowUpAt:          receipt.CreatedAt,
				FollowUpReceiptPresent:    true,
				FollowUpOpen:              true,
				FollowUpProgressed:        true,
			}
			rollup := orchestrator.ContinuityIncidentFollowUpHistoryRollupSummary{
				WindowSize:              1,
				BoundedWindow:           true,
				DistinctAnchors:         1,
				ReceiptsProgressed:      1,
				AnchorsWithOpenFollowUp: 1,
				OperationallyNotable:    true,
				Summary:                 "1 anchor(s) have open follow-up receipts",
			}
			return orchestrator.RecordContinuityIncidentFollowUpResult{
				TaskID:                                  common.TaskID(req.TaskID),
				AnchorMode:                              incidenttriage.AnchorModeLatestTransition,
				AnchorTransitionReceiptID:               "ctr_followup_anchor",
				TriageReceiptID:                         "citr_followup_anchor",
				ActionKind:                              incidenttriage.FollowUpActionProgressed,
				Receipt:                                 receipt,
				LatestContinuityTransition:              &latestTransition,
				LatestContinuityIncidentTriage:          &latestTriage,
				RecentContinuityIncidentTriages:         []orchestrator.ContinuityIncidentTriageReceiptSummary{latestTriage},
				LatestContinuityIncidentFollowUp:        &receipt,
				RecentContinuityIncidentFollowUps:       []orchestrator.ContinuityIncidentFollowUpReceiptSummary{receipt},
				ContinuityIncidentFollowUpHistoryRollup: &rollup,
				FollowUp:                                &followUp,
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)
	payload, _ := json.Marshal(ipc.TaskContinuityIncidentFollowUpRequest{
		TaskID:                    common.TaskID("tsk_followup"),
		AnchorMode:                "latest",
		TriageReceiptID:           "citr_followup_anchor",
		ActionKind:                "PROGRESSED",
		Summary:                   "validated downstream handoff context and advanced follow-up",
		AnchorTransitionReceiptID: "",
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_incident_followup",
		Method:    ipc.MethodTaskContinuityIncidentFollowUp,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_followup" || captured.AnchorMode != "latest" || captured.TriageReceiptID != "citr_followup_anchor" || captured.ActionKind != "PROGRESSED" {
		t.Fatalf("unexpected incident follow-up request mapping: %+v", captured)
	}
	var out ipc.TaskContinuityIncidentFollowUpResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal continuity incident follow-up response: %v", err)
	}
	if out.Receipt.ReceiptID != "cifr_700" || out.ActionKind != "PROGRESSED" || out.AnchorTransitionReceiptID != "ctr_followup_anchor" {
		t.Fatalf("unexpected continuity incident follow-up response receipt mapping: %+v", out)
	}
	if out.ContinuityIncidentFollowUp == nil || !out.ContinuityIncidentFollowUp.FollowUpProgressed || out.ContinuityIncidentFollowUp.LatestFollowUpReceiptID != "cifr_700" {
		t.Fatalf("unexpected continuity incident follow-up summary mapping: %+v", out.ContinuityIncidentFollowUp)
	}
	if out.ContinuityIncidentFollowUp.Digest != "follow-up open" || out.ContinuityIncidentFollowUp.WindowAdvisory != "bounded window open=1" {
		t.Fatalf("expected follow-up digest/window mapping, got %+v", out.ContinuityIncidentFollowUp)
	}
	if out.ContinuityIncidentFollowUpHistoryRollup == nil || out.ContinuityIncidentFollowUpHistoryRollup.ReceiptsProgressed != 1 {
		t.Fatalf("unexpected continuity incident follow-up rollup mapping: %+v", out.ContinuityIncidentFollowUpHistoryRollup)
	}
}

func TestHandleRequestContinuityIncidentFollowUpHistoryRoute(t *testing.T) {
	var captured orchestrator.ReadContinuityIncidentFollowUpHistoryRequest
	handler := &fakeOrchestratorService{
		readContinuityIncidentFollowUpHistoryFn: func(_ context.Context, req orchestrator.ReadContinuityIncidentFollowUpHistoryRequest) (orchestrator.ReadContinuityIncidentFollowUpHistoryResult, error) {
			captured = req
			latest := orchestrator.ContinuityIncidentFollowUpReceiptSummary{
				ReceiptID:                 "cifr_810",
				TaskID:                    common.TaskID(req.TaskID),
				AnchorMode:                incidenttriage.AnchorModeLatestTransition,
				AnchorTransitionReceiptID: "ctr_800",
				AnchorTransitionKind:      transition.KindHandoffResolution,
				TriageReceiptID:           "citr_805",
				TriagePosture:             incidenttriage.PostureNeedsFollowUp,
				TriageFollowUpState:       incidenttriage.FollowUpPostureAdvisory,
				ActionKind:                incidenttriage.FollowUpActionClosed,
				Summary:                   "closure recorded after explicit operator review",
				AcknowledgmentPresent:     true,
				CreatedAt:                 time.Unix(1712000800, 0).UTC(),
			}
			return orchestrator.ReadContinuityIncidentFollowUpHistoryResult{
				TaskID:                             common.TaskID(req.TaskID),
				Bounded:                            true,
				RequestedLimit:                     3,
				RequestedBeforeReceiptID:           "cifr_790",
				RequestedAnchorTransitionReceiptID: "ctr_800",
				RequestedTriageReceiptID:           "citr_805",
				RequestedActionKind:                incidenttriage.FollowUpActionClosed,
				HasMoreOlder:                       true,
				NextBeforeReceiptID:                "cifr_805",
				LatestTransitionReceiptID:          "ctr_820",
				Latest:                             &latest,
				Receipts: []orchestrator.ContinuityIncidentFollowUpReceiptSummary{
					latest,
					{
						ReceiptID:                 "cifr_805",
						TaskID:                    common.TaskID(req.TaskID),
						AnchorMode:                incidenttriage.AnchorModeTransitionID,
						AnchorTransitionReceiptID: "ctr_800",
						AnchorTransitionKind:      transition.KindHandoffResolution,
						TriageReceiptID:           "citr_805",
						ActionKind:                incidenttriage.FollowUpActionProgressed,
						CreatedAt:                 time.Unix(1712000700, 0).UTC(),
					},
				},
				Rollup: orchestrator.ContinuityIncidentFollowUpHistoryRollupSummary{
					WindowSize:                        2,
					BoundedWindow:                     true,
					DistinctAnchors:                   1,
					ReceiptsProgressed:                1,
					ReceiptsClosed:                    1,
					AnchorsClosed:                     1,
					OpenAnchorsBehindLatestTransition: 1,
					OperationallyNotable:              true,
					Summary:                           "1 open anchor(s) are behind the latest transition anchor",
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)
	payload, _ := json.Marshal(ipc.TaskContinuityIncidentFollowUpHistoryRequest{
		TaskID:                    common.TaskID("tsk_followup_history"),
		Limit:                     3,
		BeforeReceiptID:           "cifr_790",
		AnchorTransitionReceiptID: "ctr_800",
		TriageReceiptID:           "citr_805",
		ActionKind:                "CLOSED",
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_incident_followup_history",
		Method:    ipc.MethodTaskContinuityIncidentFollowUpHistory,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_followup_history" || captured.Limit != 3 || captured.BeforeReceiptID != "cifr_790" || captured.AnchorTransitionReceiptID != "ctr_800" || captured.TriageReceiptID != "citr_805" || captured.ActionKind != "CLOSED" {
		t.Fatalf("unexpected incident follow-up history request payload mapping: %+v", captured)
	}
	var out ipc.TaskContinuityIncidentFollowUpHistoryResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal continuity incident follow-up history response: %v", err)
	}
	if out.RequestedActionKind != "CLOSED" || out.NextBeforeReceiptID != "cifr_805" || !out.HasMoreOlder {
		t.Fatalf("unexpected continuity incident follow-up history metadata mapping: %+v", out)
	}
	if out.Latest == nil || out.Latest.ReceiptID != "cifr_810" || len(out.Receipts) != 2 {
		t.Fatalf("unexpected continuity incident follow-up history receipt mapping: %+v", out)
	}
	if out.Rollup.ReceiptsClosed != 1 || !out.Rollup.OperationallyNotable {
		t.Fatalf("unexpected continuity incident follow-up history rollup mapping: %+v", out.Rollup)
	}
}

func TestHandleRequestContinuityIncidentClosureRoute(t *testing.T) {
	var captured orchestrator.ReadContinuityIncidentClosureRequest
	handler := &fakeOrchestratorService{
		readContinuityIncidentClosureFn: func(_ context.Context, req orchestrator.ReadContinuityIncidentClosureRequest) (orchestrator.ReadContinuityIncidentClosureResult, error) {
			captured = req
			latest := orchestrator.ContinuityIncidentFollowUpReceiptSummary{
				ReceiptID:                 "cifr_910",
				TaskID:                    common.TaskID(req.TaskID),
				AnchorMode:                incidenttriage.AnchorModeLatestTransition,
				AnchorTransitionReceiptID: "ctr_900",
				ActionKind:                incidenttriage.FollowUpActionReopened,
				CreatedAt:                 time.Unix(1713000910, 0).UTC(),
			}
			return orchestrator.ReadContinuityIncidentClosureResult{
				TaskID:                    common.TaskID(req.TaskID),
				Bounded:                   true,
				RequestedLimit:            5,
				RequestedBeforeReceiptID:  "cifr_880",
				HasMoreOlder:              true,
				NextBeforeReceiptID:       "cifr_905",
				LatestTransitionReceiptID: "ctr_920",
				Latest:                    &latest,
				Receipts:                  []orchestrator.ContinuityIncidentFollowUpReceiptSummary{latest},
				Rollup: orchestrator.ContinuityIncidentFollowUpHistoryRollupSummary{
					WindowSize:              1,
					BoundedWindow:           true,
					DistinctAnchors:         1,
					ReceiptsReopened:        1,
					AnchorsReopened:         1,
					AnchorsWithOpenFollowUp: 1,
					OperationallyNotable:    true,
					Summary:                 "bounded evidence includes reopened follow-up posture",
				},
				FollowUp: &orchestrator.ContinuityIncidentFollowUpSummary{
					State:          orchestrator.ContinuityIncidentFollowUpReopened,
					Digest:         "follow-up reopened",
					WindowAdvisory: "bounded window open=1 reopened=1",
					Advisory:       "Latest incident follow-up receipt is REOPENED; follow-up remains explicitly open.",
					FollowUpOpen:   true,
					ClosureIntelligence: &orchestrator.ContinuityIncidentClosureSummary{
						Class:                   orchestrator.ContinuityIncidentClosureWeakReopened,
						Digest:                  "closure reopened after close",
						WindowAdvisory:          "bounded window anchors=1 open=1 closed=0 reopened=1 triaged-without-follow-up=0 repeated=0 behind-latest=0",
						Detail:                  "Recent bounded evidence suggests incident closure remains operationally unresolved.",
						BoundedWindow:           true,
						WindowSize:              1,
						DistinctAnchors:         1,
						OperationallyUnresolved: true,
						ClosureAppearsWeak:      true,
						ReopenedAfterClosure:    true,
						RecentAnchors: []orchestrator.ContinuityIncidentClosureAnchorItem{
							{
								AnchorTransitionReceiptID: "ctr_900",
								Class:                     orchestrator.ContinuityIncidentClosureWeakReopened,
								Digest:                    "closure reopened after close",
								Explanation:               "reopened after closure in recent bounded evidence",
								LatestFollowUpReceiptID:   "cifr_910",
								LatestFollowUpActionKind:  incidenttriage.FollowUpActionReopened,
								LatestFollowUpAt:          time.Unix(1713000910, 0).UTC(),
							},
						},
					},
				},
				Closure: &orchestrator.ContinuityIncidentClosureSummary{
					Class:                   orchestrator.ContinuityIncidentClosureWeakReopened,
					Digest:                  "closure reopened after close",
					WindowAdvisory:          "bounded window anchors=1 open=1 closed=0 reopened=1 triaged-without-follow-up=0 repeated=0 behind-latest=0",
					Detail:                  "Recent bounded evidence suggests incident closure remains operationally unresolved.",
					BoundedWindow:           true,
					WindowSize:              1,
					DistinctAnchors:         1,
					OperationallyUnresolved: true,
					ClosureAppearsWeak:      true,
					ReopenedAfterClosure:    true,
					RecentAnchors: []orchestrator.ContinuityIncidentClosureAnchorItem{
						{
							AnchorTransitionReceiptID: "ctr_900",
							Class:                     orchestrator.ContinuityIncidentClosureWeakReopened,
							Digest:                    "closure reopened after close",
							Explanation:               "reopened after closure in recent bounded evidence",
							LatestFollowUpReceiptID:   "cifr_910",
							LatestFollowUpActionKind:  incidenttriage.FollowUpActionReopened,
							LatestFollowUpAt:          time.Unix(1713000910, 0).UTC(),
						},
					},
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)
	payload, _ := json.Marshal(ipc.TaskContinuityIncidentClosureRequest{
		TaskID:          common.TaskID("tsk_closure"),
		Limit:           5,
		BeforeReceiptID: "cifr_880",
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_incident_closure",
		Method:    ipc.MethodTaskContinuityIncidentClosure,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_closure" || captured.Limit != 5 || captured.BeforeReceiptID != "cifr_880" {
		t.Fatalf("unexpected continuity incident closure request mapping: %+v", captured)
	}
	var out ipc.TaskContinuityIncidentClosureResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal continuity incident closure response: %v", err)
	}
	if out.RequestedLimit != 5 || out.NextBeforeReceiptID != "cifr_905" || !out.HasMoreOlder {
		t.Fatalf("unexpected continuity incident closure metadata mapping: %+v", out)
	}
	if out.Closure == nil || out.Closure.Class != string(orchestrator.ContinuityIncidentClosureWeakReopened) || !out.Closure.OperationallyUnresolved {
		t.Fatalf("unexpected continuity incident closure summary mapping: %+v", out.Closure)
	}
	if len(out.Closure.RecentAnchors) != 1 || out.Closure.RecentAnchors[0].Class != string(orchestrator.ContinuityIncidentClosureWeakReopened) {
		t.Fatalf("expected closure recent-anchor timeline mapping, got %+v", out.Closure.RecentAnchors)
	}
	if out.FollowUp == nil || out.FollowUp.ClosureIntelligence == nil || out.FollowUp.ClosureIntelligence.Class != string(orchestrator.ContinuityIncidentClosureWeakReopened) {
		t.Fatalf("expected follow-up nested closure intelligence mapping, got %+v", out.FollowUp)
	}
	if len(out.FollowUp.ClosureIntelligence.RecentAnchors) != 1 || out.FollowUp.ClosureIntelligence.RecentAnchors[0].LatestFollowUpActionKind != "REOPENED" {
		t.Fatalf("expected nested follow-up closure anchor mapping, got %+v", out.FollowUp.ClosureIntelligence.RecentAnchors)
	}
	if out.Latest == nil || out.Latest.ReceiptID != "cifr_910" || len(out.Receipts) != 1 {
		t.Fatalf("unexpected continuity incident closure receipts mapping: %+v", out)
	}
}

func TestHandleRequestContinuityIncidentTaskRiskRoute(t *testing.T) {
	var captured orchestrator.ReadContinuityIncidentTaskRiskRequest
	handler := &fakeOrchestratorService{
		readContinuityIncidentTaskRiskFn: func(_ context.Context, req orchestrator.ReadContinuityIncidentTaskRiskRequest) (orchestrator.ReadContinuityIncidentTaskRiskResult, error) {
			captured = req
			return orchestrator.ReadContinuityIncidentTaskRiskResult{
				TaskID:                   common.TaskID(req.TaskID),
				Bounded:                  true,
				RequestedLimit:           7,
				RequestedBeforeReceiptID: "cifr_700",
				HasMoreOlder:             true,
				NextBeforeReceiptID:      "cifr_690",
				Summary: &orchestrator.ContinuityIncidentTaskRiskSummary{
					Class:                               orchestrator.ContinuityIncidentTaskRiskRecurringWeakClosure,
					Digest:                              "recurring continuity weakness in recent bounded evidence",
					WindowAdvisory:                      "bounded incident window anchors=4 open=2 reopened=2 triaged-without-follow-up=1 stagnant=1",
					Detail:                              "Recent bounded evidence suggests recurring weak closure posture across incidents.",
					BoundedWindow:                       true,
					WindowSize:                          7,
					DistinctAnchors:                     4,
					RecurringWeakClosure:                true,
					RecurringUnresolved:                 true,
					RecurringStagnantFollowUp:           true,
					RecurringTriagedWithoutFollowUp:     true,
					ReopenedAfterClosureAnchors:         2,
					RepeatedReopenLoopAnchors:           1,
					StagnantProgressionAnchors:          2,
					AnchorsTriagedWithoutFollowUp:       1,
					AnchorsWithOpenFollowUp:             2,
					AnchorsReopened:                     2,
					OperationallyUnresolvedAnchorSignal: 5,
					RecentAnchorClasses: []orchestrator.ContinuityIncidentClosureClass{
						orchestrator.ContinuityIncidentClosureWeakReopened,
						orchestrator.ContinuityIncidentClosureWeakLoop,
						orchestrator.ContinuityIncidentClosureWeakStagnant,
					},
				},
				Closure: &orchestrator.ContinuityIncidentClosureSummary{
					Class:                   orchestrator.ContinuityIncidentClosureWeakLoop,
					Digest:                  "closure loop signals",
					WindowAdvisory:          "bounded window anchors=4 open=2 closed=2 reopened=2 triaged-without-follow-up=1 repeated=2 behind-latest=1",
					Detail:                  "Recent bounded evidence suggests incident closure remains operationally unresolved.",
					BoundedWindow:           true,
					WindowSize:              7,
					DistinctAnchors:         4,
					OperationallyUnresolved: true,
					ClosureAppearsWeak:      true,
					RepeatedReopenLoop:      true,
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)
	payload, _ := json.Marshal(ipc.TaskContinuityIncidentTaskRiskRequest{
		TaskID:          common.TaskID("tsk_risk"),
		Limit:           7,
		BeforeReceiptID: "cifr_700",
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_incident_task_risk",
		Method:    ipc.MethodTaskContinuityIncidentRisk,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_risk" || captured.Limit != 7 || captured.BeforeReceiptID != "cifr_700" {
		t.Fatalf("unexpected continuity incident task-risk request mapping: %+v", captured)
	}
	var out ipc.TaskContinuityIncidentTaskRiskResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal continuity incident task-risk response: %v", err)
	}
	if out.Summary == nil || out.Summary.Class != string(orchestrator.ContinuityIncidentTaskRiskRecurringWeakClosure) {
		t.Fatalf("unexpected task-risk summary mapping: %+v", out.Summary)
	}
	if len(out.Summary.RecentAnchorClasses) != 3 || out.Summary.RecentAnchorClasses[0] != string(orchestrator.ContinuityIncidentClosureWeakReopened) {
		t.Fatalf("unexpected task-risk recent class mapping: %+v", out.Summary)
	}
	if out.Closure == nil || out.Closure.Class != string(orchestrator.ContinuityIncidentClosureWeakLoop) {
		t.Fatalf("unexpected nested closure mapping: %+v", out.Closure)
	}
	if out.NextBeforeReceiptID != "cifr_690" || !out.HasMoreOlder {
		t.Fatalf("unexpected task-risk paging mapping: %+v", out)
	}
}

func TestHandleRequestShellSessionsRoute(t *testing.T) {
	handler := &fakeOrchestratorService{
		listShellSessionsFn: func(_ context.Context, taskID string) (orchestrator.ListShellSessionsResult, error) {
			return orchestrator.ListShellSessionsResult{
				TaskID: common.TaskID(taskID),
				Sessions: []orchestrator.ShellSessionView{
					{
						TaskID:             common.TaskID(taskID),
						SessionID:          "shs_1",
						WorkerPreference:   "auto",
						ResolvedWorker:     "codex",
						WorkerSessionID:    "wks_1",
						AttachCapability:   shellsession.AttachCapabilityNone,
						HostMode:           "codex-pty",
						HostState:          "live",
						Active:             true,
						SessionClass:       orchestrator.ShellSessionClassStale,
						SessionClassReason: "session is stale; last update is older than 1m0s",
						ReattachGuidance:   "open a new shell session; stale sessions are not trusted as attach targets",
						OperatorSummary:    "stale session; live continuity is unproven",
					},
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskShellSessionsRequest{TaskID: common.TaskID("tsk_123")})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_6",
		Method:    ipc.MethodTaskShellSessions,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	var out ipc.TaskShellSessionsResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal shell sessions response: %v", err)
	}
	if len(out.Sessions) != 1 || out.Sessions[0].ResolvedWorker != "codex" {
		t.Fatalf("unexpected shell sessions payload: %+v", out)
	}
	if out.Sessions[0].SessionClass != "stale" || out.Sessions[0].WorkerSessionID != "wks_1" || out.Sessions[0].AttachCapability != "none" {
		t.Fatalf("expected stale session class, got %+v", out.Sessions[0])
	}
	if out.Sessions[0].SessionClassReason == "" || out.Sessions[0].ReattachGuidance == "" {
		t.Fatalf("expected reattach reason/guidance mapping, got %+v", out.Sessions[0])
	}
}

func TestHandleRequestShellSessionsRouteReadsDurableRecordsAfterCoordinatorRecreation(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tuku-shell-route.db")
	store, err := sqlite.NewStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}

	coord, err := orchestrator.NewCoordinator(orchestrator.Dependencies{
		Store:          store,
		IntentCompiler: orchestrator.NewIntentStubCompiler(),
		BriefBuilder:   orchestrator.NewBriefBuilderV1(nil, nil),
		Synthesizer:    canonical.NewSimpleSynthesizer(),
		AnchorProvider: anchorgit.NewGitProvider(),
		ShellSessions:  store.ShellSessions(),
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	repoRoot := t.TempDir()
	start, err := coord.StartTask(context.Background(), "Shell route durability", repoRoot)
	if err != nil {
		t.Fatalf("start task: %v", err)
	}
	if _, err := coord.MessageTask(context.Background(), string(start.TaskID), "prepare shell session route"); err != nil {
		t.Fatalf("message task: %v", err)
	}
	if _, err := coord.ReportShellSession(context.Background(), orchestrator.ReportShellSessionRequest{
		TaskID:                string(start.TaskID),
		SessionID:             "shs_durable",
		WorkerPreference:      "auto",
		ResolvedWorker:        "codex",
		WorkerSessionID:       "wks_durable",
		WorkerSessionIDSource: shellsession.WorkerSessionIDSourceAuthoritative,
		AttachCapability:      shellsession.AttachCapabilityAttachable,
		HostMode:              "codex-pty",
		HostState:             "live",
		StartedAt:             time.Unix(1710000000, 0).UTC(),
		Active:                true,
	}); err != nil {
		t.Fatalf("report shell session: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite store: %v", err)
	}

	reopened, err := sqlite.NewStore(dbPath)
	if err != nil {
		t.Fatalf("reopen sqlite store: %v", err)
	}
	defer reopened.Close()
	coord2, err := orchestrator.NewCoordinator(orchestrator.Dependencies{
		Store:          reopened,
		IntentCompiler: orchestrator.NewIntentStubCompiler(),
		BriefBuilder:   orchestrator.NewBriefBuilderV1(nil, nil),
		Synthesizer:    canonical.NewSimpleSynthesizer(),
		AnchorProvider: anchorgit.NewGitProvider(),
		ShellSessions:  reopened.ShellSessions(),
	})
	if err != nil {
		t.Fatalf("new reopened coordinator: %v", err)
	}

	svc := NewService("/tmp/unused.sock", coord2)
	payload, _ := json.Marshal(ipc.TaskShellSessionsRequest{TaskID: start.TaskID})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_durable",
		Method:    ipc.MethodTaskShellSessions,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	var out ipc.TaskShellSessionsResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal durable shell sessions response: %v", err)
	}
	if len(out.Sessions) != 1 {
		t.Fatalf("expected one durable shell session in route response, got %d", len(out.Sessions))
	}
	if out.Sessions[0].SessionID != "shs_durable" || out.Sessions[0].WorkerSessionID != "wks_durable" || out.Sessions[0].AttachCapability != "attachable" {
		t.Fatalf("unexpected durable shell session payload: %+v", out.Sessions[0])
	}
}

type fakeOrchestratorService struct {
	resolveShellTaskForRepoFn               func(context.Context, string, string) (orchestrator.ResolveShellTaskResult, error)
	createHandoffFn                         func(context.Context, orchestrator.CreateHandoffRequest) (orchestrator.CreateHandoffResult, error)
	acceptHandoffFn                         func(context.Context, orchestrator.AcceptHandoffRequest) (orchestrator.AcceptHandoffResult, error)
	recordHandoffFollowThroughFn            func(context.Context, orchestrator.RecordHandoffFollowThroughRequest) (orchestrator.RecordHandoffFollowThroughResult, error)
	recordHandoffResolutionFn               func(context.Context, orchestrator.RecordHandoffResolutionRequest) (orchestrator.RecordHandoffResolutionResult, error)
	recordRecoveryActionFn                  func(context.Context, orchestrator.RecordRecoveryActionRequest) (orchestrator.RecordRecoveryActionResult, error)
	executeRebriefFn                        func(context.Context, orchestrator.ExecuteRebriefRequest) (orchestrator.ExecuteRebriefResult, error)
	executeInterruptedResumeFn              func(context.Context, orchestrator.ExecuteInterruptedResumeRequest) (orchestrator.ExecuteInterruptedResumeResult, error)
	executeContinueRecoveryFn               func(context.Context, orchestrator.ExecuteContinueRecoveryRequest) (orchestrator.ExecuteContinueRecoveryResult, error)
	executePrimaryOperatorStepFn            func(context.Context, orchestrator.ExecutePrimaryOperatorStepRequest) (orchestrator.ExecutePrimaryOperatorStepResult, error)
	recordOperatorReviewGapAckFn            func(context.Context, orchestrator.RecordOperatorReviewGapAcknowledgmentRequest) (orchestrator.RecordOperatorReviewGapAcknowledgmentResult, error)
	recordContinuityIncidentTriageFn        func(context.Context, orchestrator.RecordContinuityIncidentTriageRequest) (orchestrator.RecordContinuityIncidentTriageResult, error)
	recordContinuityIncidentFollowUpFn      func(context.Context, orchestrator.RecordContinuityIncidentFollowUpRequest) (orchestrator.RecordContinuityIncidentFollowUpResult, error)
	readCompiledIntentFn                    func(context.Context, orchestrator.ReadCompiledIntentRequest) (orchestrator.ReadCompiledIntentResult, error)
	readGeneratedBriefFn                    func(context.Context, orchestrator.ReadGeneratedBriefRequest) (orchestrator.ReadGeneratedBriefResult, error)
	readBenchmarkFn                         func(context.Context, orchestrator.ReadBenchmarkRequest) (orchestrator.BenchmarkTaskResult, error)
	statusFn                                func(context.Context, string) (orchestrator.StatusTaskResult, error)
	inspectFn                               func(context.Context, string) (orchestrator.InspectTaskResult, error)
	shellSnapshotFn                         func(context.Context, string) (orchestrator.ShellSnapshotResult, error)
	recordShellLifecycleFn                  func(context.Context, orchestrator.RecordShellLifecycleRequest) (orchestrator.RecordShellLifecycleResult, error)
	recordShellTranscriptFn                 func(context.Context, orchestrator.RecordShellTranscriptRequest) (orchestrator.RecordShellTranscriptResult, error)
	readShellTranscriptFn                   func(context.Context, orchestrator.ReadShellTranscriptRequest) (orchestrator.ReadShellTranscriptResult, error)
	recordShellTranscriptReviewFn           func(context.Context, orchestrator.RecordShellTranscriptReviewRequest) (orchestrator.RecordShellTranscriptReviewResult, error)
	readShellTranscriptReviewHistoryFn      func(context.Context, orchestrator.ReadShellTranscriptReviewHistoryRequest) (orchestrator.ReadShellTranscriptReviewHistoryResult, error)
	readContinuityTransitionHistoryFn       func(context.Context, orchestrator.ReadContinuityTransitionHistoryRequest) (orchestrator.ReadContinuityTransitionHistoryResult, error)
	readContinuityIncidentSliceFn           func(context.Context, orchestrator.ReadContinuityIncidentSliceRequest) (orchestrator.ReadContinuityIncidentSliceResult, error)
	readContinuityIncidentTriageHistoryFn   func(context.Context, orchestrator.ReadContinuityIncidentTriageHistoryRequest) (orchestrator.ReadContinuityIncidentTriageHistoryResult, error)
	readContinuityIncidentFollowUpHistoryFn func(context.Context, orchestrator.ReadContinuityIncidentFollowUpHistoryRequest) (orchestrator.ReadContinuityIncidentFollowUpHistoryResult, error)
	readContinuityIncidentClosureFn         func(context.Context, orchestrator.ReadContinuityIncidentClosureRequest) (orchestrator.ReadContinuityIncidentClosureResult, error)
	readContinuityIncidentTaskRiskFn        func(context.Context, orchestrator.ReadContinuityIncidentTaskRiskRequest) (orchestrator.ReadContinuityIncidentTaskRiskResult, error)
	reportShellSessionFn                    func(context.Context, orchestrator.ReportShellSessionRequest) (orchestrator.ReportShellSessionResult, error)
	listShellSessionsFn                     func(context.Context, string) (orchestrator.ListShellSessionsResult, error)
}

func (f *fakeOrchestratorService) ResolveShellTaskForRepo(ctx context.Context, repoRoot string, defaultGoal string) (orchestrator.ResolveShellTaskResult, error) {
	if f.resolveShellTaskForRepoFn != nil {
		return f.resolveShellTaskForRepoFn(ctx, repoRoot, defaultGoal)
	}
	return orchestrator.ResolveShellTaskResult{}, nil
}

func (f *fakeOrchestratorService) StartTask(_ context.Context, _ string, _ string) (orchestrator.StartTaskResult, error) {
	return orchestrator.StartTaskResult{}, nil
}

func (f *fakeOrchestratorService) MessageTask(_ context.Context, _, _ string) (orchestrator.MessageTaskResult, error) {
	return orchestrator.MessageTaskResult{}, nil
}

func (f *fakeOrchestratorService) RunTask(_ context.Context, _ orchestrator.RunTaskRequest) (orchestrator.RunTaskResult, error) {
	return orchestrator.RunTaskResult{}, nil
}

func (f *fakeOrchestratorService) ContinueTask(_ context.Context, _ string) (orchestrator.ContinueTaskResult, error) {
	return orchestrator.ContinueTaskResult{}, nil
}

func (f *fakeOrchestratorService) RecordRecoveryAction(ctx context.Context, req orchestrator.RecordRecoveryActionRequest) (orchestrator.RecordRecoveryActionResult, error) {
	if f.recordRecoveryActionFn != nil {
		return f.recordRecoveryActionFn(ctx, req)
	}
	return orchestrator.RecordRecoveryActionResult{}, nil
}

func (f *fakeOrchestratorService) ExecuteRebrief(ctx context.Context, req orchestrator.ExecuteRebriefRequest) (orchestrator.ExecuteRebriefResult, error) {
	if f.executeRebriefFn != nil {
		return f.executeRebriefFn(ctx, req)
	}
	return orchestrator.ExecuteRebriefResult{}, nil
}

func (f *fakeOrchestratorService) ExecuteInterruptedResume(ctx context.Context, req orchestrator.ExecuteInterruptedResumeRequest) (orchestrator.ExecuteInterruptedResumeResult, error) {
	if f.executeInterruptedResumeFn != nil {
		return f.executeInterruptedResumeFn(ctx, req)
	}
	return orchestrator.ExecuteInterruptedResumeResult{}, nil
}

func (f *fakeOrchestratorService) ExecuteContinueRecovery(ctx context.Context, req orchestrator.ExecuteContinueRecoveryRequest) (orchestrator.ExecuteContinueRecoveryResult, error) {
	if f.executeContinueRecoveryFn != nil {
		return f.executeContinueRecoveryFn(ctx, req)
	}
	return orchestrator.ExecuteContinueRecoveryResult{}, nil
}

func (f *fakeOrchestratorService) ExecutePrimaryOperatorStep(ctx context.Context, req orchestrator.ExecutePrimaryOperatorStepRequest) (orchestrator.ExecutePrimaryOperatorStepResult, error) {
	if f.executePrimaryOperatorStepFn != nil {
		return f.executePrimaryOperatorStepFn(ctx, req)
	}
	return orchestrator.ExecutePrimaryOperatorStepResult{}, nil
}

func (f *fakeOrchestratorService) RecordOperatorReviewGapAcknowledgment(ctx context.Context, req orchestrator.RecordOperatorReviewGapAcknowledgmentRequest) (orchestrator.RecordOperatorReviewGapAcknowledgmentResult, error) {
	if f.recordOperatorReviewGapAckFn != nil {
		return f.recordOperatorReviewGapAckFn(ctx, req)
	}
	return orchestrator.RecordOperatorReviewGapAcknowledgmentResult{}, nil
}

func (f *fakeOrchestratorService) RecordContinuityIncidentTriage(ctx context.Context, req orchestrator.RecordContinuityIncidentTriageRequest) (orchestrator.RecordContinuityIncidentTriageResult, error) {
	if f.recordContinuityIncidentTriageFn != nil {
		return f.recordContinuityIncidentTriageFn(ctx, req)
	}
	return orchestrator.RecordContinuityIncidentTriageResult{}, nil
}

func (f *fakeOrchestratorService) RecordContinuityIncidentFollowUp(ctx context.Context, req orchestrator.RecordContinuityIncidentFollowUpRequest) (orchestrator.RecordContinuityIncidentFollowUpResult, error) {
	if f.recordContinuityIncidentFollowUpFn != nil {
		return f.recordContinuityIncidentFollowUpFn(ctx, req)
	}
	return orchestrator.RecordContinuityIncidentFollowUpResult{}, nil
}

func (f *fakeOrchestratorService) ReadCompiledIntent(ctx context.Context, req orchestrator.ReadCompiledIntentRequest) (orchestrator.ReadCompiledIntentResult, error) {
	if f.readCompiledIntentFn != nil {
		return f.readCompiledIntentFn(ctx, req)
	}
	return orchestrator.ReadCompiledIntentResult{}, nil
}

func (f *fakeOrchestratorService) ReadGeneratedBrief(ctx context.Context, req orchestrator.ReadGeneratedBriefRequest) (orchestrator.ReadGeneratedBriefResult, error) {
	if f.readGeneratedBriefFn != nil {
		return f.readGeneratedBriefFn(ctx, req)
	}
	return orchestrator.ReadGeneratedBriefResult{}, nil
}

func (f *fakeOrchestratorService) ReadBenchmark(ctx context.Context, req orchestrator.ReadBenchmarkRequest) (orchestrator.BenchmarkTaskResult, error) {
	if f.readBenchmarkFn != nil {
		return f.readBenchmarkFn(ctx, req)
	}
	return orchestrator.BenchmarkTaskResult{}, nil
}

func (f *fakeOrchestratorService) CreateCheckpoint(_ context.Context, _ string) (orchestrator.CreateCheckpointResult, error) {
	return orchestrator.CreateCheckpointResult{}, nil
}

func (f *fakeOrchestratorService) CreateHandoff(ctx context.Context, req orchestrator.CreateHandoffRequest) (orchestrator.CreateHandoffResult, error) {
	if f.createHandoffFn != nil {
		return f.createHandoffFn(ctx, req)
	}
	return orchestrator.CreateHandoffResult{}, nil
}

func (f *fakeOrchestratorService) AcceptHandoff(ctx context.Context, req orchestrator.AcceptHandoffRequest) (orchestrator.AcceptHandoffResult, error) {
	if f.acceptHandoffFn != nil {
		return f.acceptHandoffFn(ctx, req)
	}
	return orchestrator.AcceptHandoffResult{}, nil
}

func (f *fakeOrchestratorService) LaunchHandoff(_ context.Context, _ orchestrator.LaunchHandoffRequest) (orchestrator.LaunchHandoffResult, error) {
	return orchestrator.LaunchHandoffResult{}, nil
}

func (f *fakeOrchestratorService) RecordHandoffFollowThrough(ctx context.Context, req orchestrator.RecordHandoffFollowThroughRequest) (orchestrator.RecordHandoffFollowThroughResult, error) {
	if f.recordHandoffFollowThroughFn != nil {
		return f.recordHandoffFollowThroughFn(ctx, req)
	}
	return orchestrator.RecordHandoffFollowThroughResult{}, nil
}

func (f *fakeOrchestratorService) RecordHandoffResolution(ctx context.Context, req orchestrator.RecordHandoffResolutionRequest) (orchestrator.RecordHandoffResolutionResult, error) {
	if f.recordHandoffResolutionFn != nil {
		return f.recordHandoffResolutionFn(ctx, req)
	}
	return orchestrator.RecordHandoffResolutionResult{}, nil
}

func (f *fakeOrchestratorService) StatusTask(ctx context.Context, taskID string) (orchestrator.StatusTaskResult, error) {
	if f.statusFn != nil {
		return f.statusFn(ctx, taskID)
	}
	return orchestrator.StatusTaskResult{
		Phase:                   phase.PhaseIntake,
		LatestCheckpointTrigger: checkpoint.TriggerManual,
	}, nil
}

func (f *fakeOrchestratorService) InspectTask(ctx context.Context, taskID string) (orchestrator.InspectTaskResult, error) {
	if f.inspectFn != nil {
		return f.inspectFn(ctx, taskID)
	}
	return orchestrator.InspectTaskResult{}, nil
}

func (f *fakeOrchestratorService) ShellSnapshotTask(ctx context.Context, taskID string) (orchestrator.ShellSnapshotResult, error) {
	if f.shellSnapshotFn != nil {
		return f.shellSnapshotFn(ctx, taskID)
	}
	return orchestrator.ShellSnapshotResult{}, nil
}

func (f *fakeOrchestratorService) RecordShellLifecycle(ctx context.Context, req orchestrator.RecordShellLifecycleRequest) (orchestrator.RecordShellLifecycleResult, error) {
	if f.recordShellLifecycleFn != nil {
		return f.recordShellLifecycleFn(ctx, req)
	}
	return orchestrator.RecordShellLifecycleResult{}, nil
}

func (f *fakeOrchestratorService) RecordShellTranscript(ctx context.Context, req orchestrator.RecordShellTranscriptRequest) (orchestrator.RecordShellTranscriptResult, error) {
	if f.recordShellTranscriptFn != nil {
		return f.recordShellTranscriptFn(ctx, req)
	}
	return orchestrator.RecordShellTranscriptResult{}, nil
}

func (f *fakeOrchestratorService) ReadShellTranscript(ctx context.Context, req orchestrator.ReadShellTranscriptRequest) (orchestrator.ReadShellTranscriptResult, error) {
	if f.readShellTranscriptFn != nil {
		return f.readShellTranscriptFn(ctx, req)
	}
	return orchestrator.ReadShellTranscriptResult{}, nil
}

func (f *fakeOrchestratorService) RecordShellTranscriptReview(ctx context.Context, req orchestrator.RecordShellTranscriptReviewRequest) (orchestrator.RecordShellTranscriptReviewResult, error) {
	if f.recordShellTranscriptReviewFn != nil {
		return f.recordShellTranscriptReviewFn(ctx, req)
	}
	return orchestrator.RecordShellTranscriptReviewResult{}, nil
}

func (f *fakeOrchestratorService) ReadShellTranscriptReviewHistory(ctx context.Context, req orchestrator.ReadShellTranscriptReviewHistoryRequest) (orchestrator.ReadShellTranscriptReviewHistoryResult, error) {
	if f.readShellTranscriptReviewHistoryFn != nil {
		return f.readShellTranscriptReviewHistoryFn(ctx, req)
	}
	return orchestrator.ReadShellTranscriptReviewHistoryResult{}, nil
}

func (f *fakeOrchestratorService) ReadContinuityTransitionHistory(ctx context.Context, req orchestrator.ReadContinuityTransitionHistoryRequest) (orchestrator.ReadContinuityTransitionHistoryResult, error) {
	if f.readContinuityTransitionHistoryFn != nil {
		return f.readContinuityTransitionHistoryFn(ctx, req)
	}
	return orchestrator.ReadContinuityTransitionHistoryResult{}, nil
}

func (f *fakeOrchestratorService) ReadContinuityIncidentSlice(ctx context.Context, req orchestrator.ReadContinuityIncidentSliceRequest) (orchestrator.ReadContinuityIncidentSliceResult, error) {
	if f.readContinuityIncidentSliceFn != nil {
		return f.readContinuityIncidentSliceFn(ctx, req)
	}
	return orchestrator.ReadContinuityIncidentSliceResult{}, nil
}

func (f *fakeOrchestratorService) ReadContinuityIncidentTriageHistory(ctx context.Context, req orchestrator.ReadContinuityIncidentTriageHistoryRequest) (orchestrator.ReadContinuityIncidentTriageHistoryResult, error) {
	if f.readContinuityIncidentTriageHistoryFn != nil {
		return f.readContinuityIncidentTriageHistoryFn(ctx, req)
	}
	return orchestrator.ReadContinuityIncidentTriageHistoryResult{}, nil
}

func (f *fakeOrchestratorService) ReadContinuityIncidentFollowUpHistory(ctx context.Context, req orchestrator.ReadContinuityIncidentFollowUpHistoryRequest) (orchestrator.ReadContinuityIncidentFollowUpHistoryResult, error) {
	if f.readContinuityIncidentFollowUpHistoryFn != nil {
		return f.readContinuityIncidentFollowUpHistoryFn(ctx, req)
	}
	return orchestrator.ReadContinuityIncidentFollowUpHistoryResult{}, nil
}

func (f *fakeOrchestratorService) ReadContinuityIncidentClosure(ctx context.Context, req orchestrator.ReadContinuityIncidentClosureRequest) (orchestrator.ReadContinuityIncidentClosureResult, error) {
	if f.readContinuityIncidentClosureFn != nil {
		return f.readContinuityIncidentClosureFn(ctx, req)
	}
	return orchestrator.ReadContinuityIncidentClosureResult{}, nil
}

func (f *fakeOrchestratorService) ReadContinuityIncidentTaskRisk(ctx context.Context, req orchestrator.ReadContinuityIncidentTaskRiskRequest) (orchestrator.ReadContinuityIncidentTaskRiskResult, error) {
	if f.readContinuityIncidentTaskRiskFn != nil {
		return f.readContinuityIncidentTaskRiskFn(ctx, req)
	}
	return orchestrator.ReadContinuityIncidentTaskRiskResult{}, nil
}

func (f *fakeOrchestratorService) ReportShellSession(ctx context.Context, req orchestrator.ReportShellSessionRequest) (orchestrator.ReportShellSessionResult, error) {
	if f.reportShellSessionFn != nil {
		return f.reportShellSessionFn(ctx, req)
	}
	return orchestrator.ReportShellSessionResult{}, nil
}

func (f *fakeOrchestratorService) ListShellSessions(ctx context.Context, taskID string) (orchestrator.ListShellSessionsResult, error) {
	if f.listShellSessionsFn != nil {
		return f.listShellSessionsFn(ctx, taskID)
	}
	return orchestrator.ListShellSessionsResult{}, nil
}

func TestHandleRequestExecutePrimaryOperatorStepRoute(t *testing.T) {
	var captured orchestrator.ExecutePrimaryOperatorStepRequest
	handler := &fakeOrchestratorService{
		executePrimaryOperatorStepFn: func(_ context.Context, req orchestrator.ExecutePrimaryOperatorStepRequest) (orchestrator.ExecutePrimaryOperatorStepResult, error) {
			captured = req
			return orchestrator.ExecutePrimaryOperatorStepResult{
				TaskID: common.TaskID(req.TaskID),
				Receipt: operatorstep.Receipt{
					ReceiptID:           "orec_123",
					TaskID:              common.TaskID(req.TaskID),
					ActionHandle:        string(orchestrator.OperatorActionLaunchAcceptedHandoff),
					ResultClass:         operatorstep.ResultSucceeded,
					TransitionReceiptID: "ctr_step_1",
					TransitionKind:      string(transition.KindHandoffLaunch),
					Summary:             "launched accepted handoff hnd_123",
					CreatedAt:           time.Unix(1710000000, 0).UTC(),
				},
				ActiveBranch:               orchestrator.ActiveBranchProvenance{TaskID: common.TaskID(req.TaskID), Class: orchestrator.ActiveBranchClassHandoffClaude, BranchRef: "hnd_123"},
				OperatorDecision:           orchestrator.OperatorDecisionSummary{Headline: "Active Claude handoff pending", RequiredNextAction: orchestrator.OperatorActionResolveActiveHandoff},
				OperatorExecutionPlan:      orchestrator.OperatorExecutionPlan{PrimaryStep: &orchestrator.OperatorExecutionStep{Action: orchestrator.OperatorActionResolveActiveHandoff, Status: orchestrator.OperatorActionAuthorityAllowed}},
				RecentOperatorStepReceipts: []operatorstep.Receipt{{ReceiptID: "orec_123", TaskID: common.TaskID(req.TaskID), ActionHandle: string(orchestrator.OperatorActionLaunchAcceptedHandoff), ResultClass: operatorstep.ResultSucceeded, Summary: "launched accepted handoff hnd_123", CreatedAt: time.Unix(1710000000, 0).UTC()}},
				LatestContinuityTransitionReceipt: &orchestrator.ContinuityTransitionReceiptSummary{
					ReceiptID:             "ctr_step_1",
					TaskID:                common.TaskID(req.TaskID),
					TransitionKind:        transition.KindHandoffLaunch,
					HandoffStateBefore:    orchestrator.HandoffContinuityStateAcceptedNotLaunched,
					HandoffStateAfter:     orchestrator.HandoffContinuityStateLaunchCompletedAckSeen,
					ReviewGapPresent:      true,
					ReviewPosture:         transition.ReviewPostureGlobalReviewStale,
					AcknowledgmentPresent: true,
					Summary:               "handoff launch transition recorded under explicit review-gap acknowledgment",
					CreatedAt:             time.Unix(1710000001, 0).UTC(),
				},
				RecentContinuityTransitionReceipts: []orchestrator.ContinuityTransitionReceiptSummary{
					{
						ReceiptID:             "ctr_step_1",
						TaskID:                common.TaskID(req.TaskID),
						TransitionKind:        transition.KindHandoffLaunch,
						HandoffStateBefore:    orchestrator.HandoffContinuityStateAcceptedNotLaunched,
						HandoffStateAfter:     orchestrator.HandoffContinuityStateLaunchCompletedAckSeen,
						ReviewGapPresent:      true,
						ReviewPosture:         transition.ReviewPostureGlobalReviewStale,
						AcknowledgmentPresent: true,
						Summary:               "handoff launch transition recorded under explicit review-gap acknowledgment",
						CreatedAt:             time.Unix(1710000001, 0).UTC(),
					},
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)
	payload, _ := json.Marshal(ipc.TaskExecutePrimaryOperatorStepRequest{
		TaskID:                      common.TaskID("tsk_123"),
		AcknowledgeReviewGap:        true,
		ReviewGapSessionID:          "shs_123",
		ReviewGapAcknowledgmentKind: "stale_review",
		ReviewGapSummary:            "proceed with explicit awareness of stale transcript evidence",
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{RequestID: "req_next", Method: ipc.MethodExecutePrimaryOperatorStep, Payload: payload})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_123" || !captured.AcknowledgeReviewGap || captured.ReviewGapSessionID != "shs_123" || captured.ReviewGapAcknowledgmentKind != "stale_review" {
		t.Fatalf("unexpected execute-primary request: %+v", captured)
	}
	var out ipc.TaskExecutePrimaryOperatorStepResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.Receipt.ReceiptID != "orec_123" || out.Receipt.ActionHandle != string(orchestrator.OperatorActionLaunchAcceptedHandoff) {
		t.Fatalf("unexpected operator-next response: %+v", out)
	}
	if out.Receipt.TransitionReceiptID != "ctr_step_1" || out.Receipt.TransitionKind != string(transition.KindHandoffLaunch) {
		t.Fatalf("expected transition linkage on operator receipt mapping, got %+v", out.Receipt)
	}
	if len(out.RecentOperatorStepReceipts) != 1 || out.RecentOperatorStepReceipts[0].ReceiptID != "orec_123" {
		t.Fatalf("expected recent receipt history in response, got %+v", out.RecentOperatorStepReceipts)
	}
	if out.LatestContinuityTransitionReceipt == nil || out.LatestContinuityTransitionReceipt.ReceiptID != "ctr_step_1" {
		t.Fatalf("expected latest continuity transition receipt mapping, got %+v", out.LatestContinuityTransitionReceipt)
	}
	if len(out.RecentContinuityTransitionReceipts) != 1 || out.RecentContinuityTransitionReceipts[0].TransitionKind != string(transition.KindHandoffLaunch) {
		t.Fatalf("expected continuity transition receipt history mapping, got %+v", out.RecentContinuityTransitionReceipts)
	}
}

func TestHandleRequestOperatorAcknowledgeReviewGapRoute(t *testing.T) {
	var captured orchestrator.RecordOperatorReviewGapAcknowledgmentRequest
	handler := &fakeOrchestratorService{
		recordOperatorReviewGapAckFn: func(_ context.Context, req orchestrator.RecordOperatorReviewGapAcknowledgmentRequest) (orchestrator.RecordOperatorReviewGapAcknowledgmentResult, error) {
			captured = req
			return orchestrator.RecordOperatorReviewGapAcknowledgmentResult{
				TaskID:    common.TaskID(req.TaskID),
				SessionID: "shs_123",
				Acknowledgment: orchestrator.TranscriptReviewGapAcknowledgmentSummary{
					AcknowledgmentID:         "sack_123",
					TaskID:                   common.TaskID(req.TaskID),
					SessionID:                "shs_123",
					Class:                    shellsession.TranscriptReviewGapAckStaleReview,
					ReviewState:              "global_review_stale",
					ReviewedUpToSequence:     120,
					OldestUnreviewedSequence: 121,
					NewestRetainedSequence:   160,
					UnreviewedRetainedCount:  40,
					Summary:                  "proceed with explicit awareness",
					CreatedAt:                time.Unix(1710001000, 0).UTC(),
				},
				ReviewGapState:         "global_review_stale",
				ReviewGapClass:         shellsession.TranscriptReviewGapAckStaleReview,
				ReviewedUpToSequence:   120,
				OldestUnreviewedSeq:    121,
				NewestRetainedSequence: 160,
				UnreviewedRetained:     40,
				Advisory:               "review awareness recommended while progressing",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)
	payload, _ := json.Marshal(ipc.TaskOperatorAcknowledgeReviewGapRequest{
		TaskID:    common.TaskID("tsk_123"),
		SessionID: "shs_123",
		Kind:      "stale_review",
		Summary:   "proceed with explicit awareness",
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_ack_gap",
		Method:    ipc.MethodOperatorAcknowledgeReviewGap,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_123" || captured.SessionID != "shs_123" || captured.Kind != "stale_review" {
		t.Fatalf("unexpected operator review-gap acknowledgment request: %+v", captured)
	}
	var out ipc.TaskOperatorAcknowledgeReviewGapResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.Acknowledgment.AcknowledgmentID != "sack_123" || out.ReviewGapClass != "stale_review" {
		t.Fatalf("unexpected operator review-gap acknowledgment response: %+v", out)
	}
}

func TestShellSnapshotResponseIncludesOperatorReceiptHistory(t *testing.T) {
	handler := &fakeOrchestratorService{
		shellSnapshotFn: func(_ context.Context, _ string) (orchestrator.ShellSnapshotResult, error) {
			return orchestrator.ShellSnapshotResult{
				TaskID:                    "tsk_123",
				Goal:                      "test",
				Phase:                     "BRIEF_READY",
				Status:                    "ACTIVE",
				LatestOperatorStepReceipt: &operatorstep.Receipt{ReceiptID: "orec_latest", TaskID: "tsk_123", ActionHandle: string(orchestrator.OperatorActionFinalizeContinueRecovery), ResultClass: operatorstep.ResultSucceeded, Summary: "finalized continue recovery", CreatedAt: time.Unix(1710000100, 0).UTC()},
				RecentOperatorStepReceipts: []operatorstep.Receipt{
					{ReceiptID: "orec_latest", TaskID: "tsk_123", ActionHandle: string(orchestrator.OperatorActionFinalizeContinueRecovery), ResultClass: operatorstep.ResultSucceeded, Summary: "finalized continue recovery", CreatedAt: time.Unix(1710000100, 0).UTC()},
					{ReceiptID: "orec_prev", TaskID: "tsk_123", ActionHandle: string(orchestrator.OperatorActionResumeInterruptedLineage), ResultClass: operatorstep.ResultSucceeded, Summary: "resumed interrupted lineage", CreatedAt: time.Unix(1710000000, 0).UTC()},
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)
	payload, _ := json.Marshal(ipc.TaskShellSnapshotRequest{TaskID: common.TaskID("tsk_123")})
	resp := svc.handleRequest(context.Background(), ipc.Request{RequestID: "req_shell", Method: ipc.MethodTaskShellSnapshot, Payload: payload})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	var out ipc.TaskShellSnapshotResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.LatestOperatorStepReceipt == nil || out.LatestOperatorStepReceipt.ReceiptID != "orec_latest" {
		t.Fatalf("expected latest operator receipt in shell snapshot, got %+v", out.LatestOperatorStepReceipt)
	}
	if len(out.RecentOperatorStepReceipts) != 2 || out.RecentOperatorStepReceipts[1].ReceiptID != "orec_prev" {
		t.Fatalf("expected recent shell receipt history, got %+v", out.RecentOperatorStepReceipts)
	}
}
