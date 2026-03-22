package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/conversation"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/operatorstep"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/recoveryaction"
	"tuku/internal/domain/run"
	"tuku/internal/domain/shellsession"
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
				RecentProofs: []orchestrator.ShellProofSummary{
					{EventID: "evt_1", Type: proof.EventBriefCreated, Summary: "Execution brief updated"},
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
				TaskID:                      common.TaskID("tsk_status"),
				Phase:                       phase.PhaseBlocked,
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
				OperatorExecutionPlan: &orchestrator.OperatorExecutionPlan{
					PrimaryStep: &orchestrator.OperatorExecutionStep{
						Action:      orchestrator.OperatorActionReviewHandoffFollowUp,
						Status:      orchestrator.OperatorActionAuthorityRequiredNext,
						Domain:      orchestrator.OperatorExecutionDomainReview,
						CommandHint: "tuku inspect --task tsk_status",
						Reason:      "launch completed and acknowledgment captured, but downstream follow-through appears stalled",
					},
					MandatoryBeforeProgress: true,
				},
				RecoveryClass:        orchestrator.RecoveryClassHandoffFollowThroughReviewRequired,
				RecommendedAction:    orchestrator.RecoveryActionReviewHandoffFollowThrough,
				LatestRecoveryAction: action,
			}, nil
		},
		inspectFn: func(_ context.Context, _ string) (orchestrator.InspectTaskResult, error) {
			return orchestrator.InspectTaskResult{
				TaskID:                common.TaskID("tsk_status"),
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
				ActionAuthority: &orchestrator.OperatorActionAuthoritySet{
					RequiredNextAction: orchestrator.OperatorActionReviewHandoffFollowUp,
					Actions: []orchestrator.OperatorActionAuthority{
						{Action: orchestrator.OperatorActionLocalMessageMutation, State: orchestrator.OperatorActionAuthorityBlocked, BlockingBranchClass: orchestrator.ActiveBranchClassHandoffClaude, BlockingBranchRef: "hnd_status", Reason: "Cannot send a local task message while launched Claude handoff hnd_status remains the active continuity branch."},
						{Action: orchestrator.OperatorActionReviewHandoffFollowUp, State: orchestrator.OperatorActionAuthorityRequiredNext, Reason: "launch completed and acknowledgment captured, but downstream follow-through appears stalled"},
					},
				},
				OperatorExecutionPlan: &orchestrator.OperatorExecutionPlan{
					PrimaryStep: &orchestrator.OperatorExecutionStep{
						Action:      orchestrator.OperatorActionReviewHandoffFollowUp,
						Status:      orchestrator.OperatorActionAuthorityRequiredNext,
						Domain:      orchestrator.OperatorExecutionDomainReview,
						CommandHint: "tuku inspect --task tsk_status",
						Reason:      "launch completed and acknowledgment captured, but downstream follow-through appears stalled",
					},
					MandatoryBeforeProgress: true,
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
	if inspectOut.OperatorExecutionPlan == nil || inspectOut.OperatorExecutionPlan.PrimaryStep == nil {
		t.Fatalf("expected inspect execution plan mapping, got %+v", inspectOut.OperatorExecutionPlan)
	}
	if inspectOut.OperatorExecutionPlan.PrimaryStep.Action != string(orchestrator.OperatorActionReviewHandoffFollowUp) || inspectOut.OperatorExecutionPlan.PrimaryStep.CommandHint != "tuku inspect --task tsk_status" {
		t.Fatalf("expected inspect execution plan primary step mapping, got %+v", inspectOut.OperatorExecutionPlan.PrimaryStep)
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
					TaskID:           common.TaskID(req.TaskID),
					SessionID:        req.SessionID,
					WorkerPreference: req.WorkerPreference,
					ResolvedWorker:   req.ResolvedWorker,
					WorkerSessionID:  req.WorkerSessionID,
					AttachCapability: req.AttachCapability,
					HostMode:         req.HostMode,
					HostState:        req.HostState,
					StartedAt:        req.StartedAt,
					Active:           req.Active,
					Note:             req.Note,
					SessionClass:     orchestrator.ShellSessionClassAttachable,
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskShellSessionReportRequest{
		TaskID:           common.TaskID("tsk_shell"),
		SessionID:        "shs_456",
		WorkerPreference: "auto",
		ResolvedWorker:   "claude",
		WorkerSessionID:  "wks_456",
		AttachCapability: "attachable",
		HostMode:         "claude-pty",
		HostState:        "starting",
		Active:           true,
		Note:             "shell session registered",
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
	var out ipc.TaskShellSessionReportResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal shell session report response: %v", err)
	}
	if out.Session.SessionClass != "attachable" || out.Session.WorkerSessionID != "wks_456" || out.Session.AttachCapability != "attachable" {
		t.Fatalf("expected active session class, got %+v", out.Session)
	}
}

func TestHandleRequestShellSessionsRoute(t *testing.T) {
	handler := &fakeOrchestratorService{
		listShellSessionsFn: func(_ context.Context, taskID string) (orchestrator.ListShellSessionsResult, error) {
			return orchestrator.ListShellSessionsResult{
				TaskID: common.TaskID(taskID),
				Sessions: []orchestrator.ShellSessionView{
					{
						TaskID:           common.TaskID(taskID),
						SessionID:        "shs_1",
						WorkerPreference: "auto",
						ResolvedWorker:   "codex",
						WorkerSessionID:  "wks_1",
						AttachCapability: shellsession.AttachCapabilityNone,
						HostMode:         "codex-pty",
						HostState:        "live",
						Active:           true,
						SessionClass:     orchestrator.ShellSessionClassStale,
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
		TaskID:           string(start.TaskID),
		SessionID:        "shs_durable",
		WorkerPreference: "auto",
		ResolvedWorker:   "codex",
		WorkerSessionID:  "wks_durable",
		AttachCapability: shellsession.AttachCapabilityAttachable,
		HostMode:         "codex-pty",
		HostState:        "live",
		StartedAt:        time.Unix(1710000000, 0).UTC(),
		Active:           true,
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
	resolveShellTaskForRepoFn    func(context.Context, string, string) (orchestrator.ResolveShellTaskResult, error)
	createHandoffFn              func(context.Context, orchestrator.CreateHandoffRequest) (orchestrator.CreateHandoffResult, error)
	acceptHandoffFn              func(context.Context, orchestrator.AcceptHandoffRequest) (orchestrator.AcceptHandoffResult, error)
	recordHandoffFollowThroughFn func(context.Context, orchestrator.RecordHandoffFollowThroughRequest) (orchestrator.RecordHandoffFollowThroughResult, error)
	recordHandoffResolutionFn    func(context.Context, orchestrator.RecordHandoffResolutionRequest) (orchestrator.RecordHandoffResolutionResult, error)
	recordRecoveryActionFn       func(context.Context, orchestrator.RecordRecoveryActionRequest) (orchestrator.RecordRecoveryActionResult, error)
	executeRebriefFn             func(context.Context, orchestrator.ExecuteRebriefRequest) (orchestrator.ExecuteRebriefResult, error)
	executeInterruptedResumeFn   func(context.Context, orchestrator.ExecuteInterruptedResumeRequest) (orchestrator.ExecuteInterruptedResumeResult, error)
	executeContinueRecoveryFn    func(context.Context, orchestrator.ExecuteContinueRecoveryRequest) (orchestrator.ExecuteContinueRecoveryResult, error)
	executePrimaryOperatorStepFn func(context.Context, orchestrator.ExecutePrimaryOperatorStepRequest) (orchestrator.ExecutePrimaryOperatorStepResult, error)
	statusFn                     func(context.Context, string) (orchestrator.StatusTaskResult, error)
	inspectFn                    func(context.Context, string) (orchestrator.InspectTaskResult, error)
	shellSnapshotFn              func(context.Context, string) (orchestrator.ShellSnapshotResult, error)
	recordShellLifecycleFn       func(context.Context, orchestrator.RecordShellLifecycleRequest) (orchestrator.RecordShellLifecycleResult, error)
	reportShellSessionFn         func(context.Context, orchestrator.ReportShellSessionRequest) (orchestrator.ReportShellSessionResult, error)
	listShellSessionsFn          func(context.Context, string) (orchestrator.ListShellSessionsResult, error)
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
					ReceiptID:    "orec_123",
					TaskID:       common.TaskID(req.TaskID),
					ActionHandle: string(orchestrator.OperatorActionLaunchAcceptedHandoff),
					ResultClass:  operatorstep.ResultSucceeded,
					Summary:      "launched accepted handoff hnd_123",
					CreatedAt:    time.Unix(1710000000, 0).UTC(),
				},
				ActiveBranch:               orchestrator.ActiveBranchProvenance{TaskID: common.TaskID(req.TaskID), Class: orchestrator.ActiveBranchClassHandoffClaude, BranchRef: "hnd_123"},
				OperatorDecision:           orchestrator.OperatorDecisionSummary{Headline: "Active Claude handoff pending", RequiredNextAction: orchestrator.OperatorActionResolveActiveHandoff},
				OperatorExecutionPlan:      orchestrator.OperatorExecutionPlan{PrimaryStep: &orchestrator.OperatorExecutionStep{Action: orchestrator.OperatorActionResolveActiveHandoff, Status: orchestrator.OperatorActionAuthorityAllowed}},
				RecentOperatorStepReceipts: []operatorstep.Receipt{{ReceiptID: "orec_123", TaskID: common.TaskID(req.TaskID), ActionHandle: string(orchestrator.OperatorActionLaunchAcceptedHandoff), ResultClass: operatorstep.ResultSucceeded, Summary: "launched accepted handoff hnd_123", CreatedAt: time.Unix(1710000000, 0).UTC()}},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)
	payload, _ := json.Marshal(ipc.TaskExecutePrimaryOperatorStepRequest{TaskID: common.TaskID("tsk_123")})
	resp := svc.handleRequest(context.Background(), ipc.Request{RequestID: "req_next", Method: ipc.MethodExecutePrimaryOperatorStep, Payload: payload})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_123" {
		t.Fatalf("unexpected execute-primary request: %+v", captured)
	}
	var out ipc.TaskExecutePrimaryOperatorStepResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.Receipt.ReceiptID != "orec_123" || out.Receipt.ActionHandle != string(orchestrator.OperatorActionLaunchAcceptedHandoff) {
		t.Fatalf("unexpected operator-next response: %+v", out)
	}
	if len(out.RecentOperatorStepReceipts) != 1 || out.RecentOperatorStepReceipts[0].ReceiptID != "orec_123" {
		t.Fatalf("expected recent receipt history in response, got %+v", out.RecentOperatorStepReceipts)
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
