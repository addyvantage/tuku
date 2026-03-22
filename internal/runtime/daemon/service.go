package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"tuku/internal/domain/handoff"
	"tuku/internal/domain/operatorstep"
	"tuku/internal/domain/recoveryaction"
	"tuku/internal/domain/shellsession"
	"tuku/internal/ipc"
	"tuku/internal/orchestrator"
)

func ipcRecoveryActionRecord(in *recoveryaction.Record) *ipc.TaskRecoveryActionRecord {
	if in == nil {
		return nil
	}
	createdAt := int64(0)
	if !in.CreatedAt.IsZero() {
		createdAt = in.CreatedAt.UnixMilli()
	}
	return &ipc.TaskRecoveryActionRecord{
		ActionID:        in.ActionID,
		TaskID:          in.TaskID,
		Kind:            string(in.Kind),
		RunID:           in.RunID,
		CheckpointID:    in.CheckpointID,
		HandoffID:       in.HandoffID,
		LaunchAttemptID: in.LaunchAttemptID,
		Summary:         in.Summary,
		Notes:           append([]string{}, in.Notes...),
		CreatedAtUnixMs: createdAt,
	}
}

func ipcRecoveryAssessment(in *orchestrator.RecoveryAssessment) *ipc.TaskRecoveryAssessment {
	if in == nil {
		return nil
	}
	out := &ipc.TaskRecoveryAssessment{
		TaskID:                 in.TaskID,
		ContinuityOutcome:      string(in.ContinuityOutcome),
		RecoveryClass:          string(in.RecoveryClass),
		RecommendedAction:      string(in.RecommendedAction),
		ReadyForNextRun:        in.ReadyForNextRun,
		ReadyForHandoffLaunch:  in.ReadyForHandoffLaunch,
		RequiresDecision:       in.RequiresDecision,
		RequiresRepair:         in.RequiresRepair,
		RequiresReview:         in.RequiresReview,
		RequiresReconciliation: in.RequiresReconciliation,
		DriftClass:             in.DriftClass,
		Reason:                 in.Reason,
		CheckpointID:           in.CheckpointID,
		RunID:                  in.RunID,
		HandoffID:              in.HandoffID,
		HandoffStatus:          string(in.HandoffStatus),
		LatestAction:           ipcRecoveryActionRecord(in.LatestAction),
	}
	if len(in.Issues) > 0 {
		out.Issues = make([]ipc.TaskRecoveryIssue, 0, len(in.Issues))
		for _, issue := range in.Issues {
			out.Issues = append(out.Issues, ipc.TaskRecoveryIssue{
				Code:    issue.Code,
				Message: issue.Message,
			})
		}
	}
	return out
}

func ipcLaunchControl(in *orchestrator.LaunchControl) *ipc.TaskLaunchControl {
	if in == nil {
		return nil
	}
	return &ipc.TaskLaunchControl{
		TaskID:           in.TaskID,
		HandoffID:        in.HandoffID,
		AttemptID:        in.AttemptID,
		LaunchID:         in.LaunchID,
		State:            string(in.State),
		RetryDisposition: string(in.RetryDisposition),
		Reason:           in.Reason,
		TargetWorker:     in.TargetWorker,
		RequestedAt:      in.RequestedAt,
		CompletedAt:      in.CompletedAt,
		FailedAt:         in.FailedAt,
	}
}

func ipcShellRecovery(in *orchestrator.ShellRecoverySummary) *ipc.TaskShellRecovery {
	if in == nil {
		return nil
	}
	out := &ipc.TaskShellRecovery{
		ContinuityOutcome:      string(in.ContinuityOutcome),
		RecoveryClass:          string(in.RecoveryClass),
		RecommendedAction:      string(in.RecommendedAction),
		ReadyForNextRun:        in.ReadyForNextRun,
		ReadyForHandoffLaunch:  in.ReadyForHandoffLaunch,
		RequiresDecision:       in.RequiresDecision,
		RequiresRepair:         in.RequiresRepair,
		RequiresReview:         in.RequiresReview,
		RequiresReconciliation: in.RequiresReconciliation,
		DriftClass:             in.DriftClass,
		Reason:                 in.Reason,
		CheckpointID:           in.CheckpointID,
		RunID:                  in.RunID,
		HandoffID:              in.HandoffID,
		HandoffStatus:          string(in.HandoffStatus),
	}
	if len(in.Issues) > 0 {
		out.Issues = make([]ipc.TaskShellRecoveryIssue, 0, len(in.Issues))
		for _, issue := range in.Issues {
			out.Issues = append(out.Issues, ipc.TaskShellRecoveryIssue{
				Code:    issue.Code,
				Message: issue.Message,
			})
		}
	}
	return out
}

func ipcShellLaunchControl(in *orchestrator.ShellLaunchControlSummary) *ipc.TaskShellLaunchControl {
	if in == nil {
		return nil
	}
	return &ipc.TaskShellLaunchControl{
		State:            string(in.State),
		RetryDisposition: string(in.RetryDisposition),
		Reason:           in.Reason,
		HandoffID:        in.HandoffID,
		AttemptID:        in.AttemptID,
		LaunchID:         in.LaunchID,
		TargetWorker:     in.TargetWorker,
		RequestedAt:      in.RequestedAt,
		CompletedAt:      in.CompletedAt,
		FailedAt:         in.FailedAt,
	}
}

func ipcHandoffContinuity(in *orchestrator.HandoffContinuity) *ipc.TaskHandoffContinuity {
	if in == nil {
		return nil
	}
	return &ipc.TaskHandoffContinuity{
		TaskID:                       in.TaskID,
		HandoffID:                    in.HandoffID,
		TargetWorker:                 in.TargetWorker,
		State:                        string(in.State),
		LaunchAttemptID:              in.LaunchAttemptID,
		LaunchID:                     in.LaunchID,
		LaunchStatus:                 string(in.LaunchStatus),
		AcknowledgmentID:             in.AcknowledgmentID,
		AcknowledgmentStatus:         string(in.AcknowledgmentStatus),
		AcknowledgmentSummary:        in.AcknowledgmentSummary,
		FollowThroughID:              in.FollowThroughID,
		FollowThroughKind:            string(in.FollowThroughKind),
		FollowThroughSummary:         in.FollowThroughSummary,
		ResolutionID:                 in.ResolutionID,
		ResolutionKind:               string(in.ResolutionKind),
		ResolutionSummary:            in.ResolutionSummary,
		DownstreamContinuationProven: in.DownstreamContinuationProven,
		Reason:                       in.Reason,
	}
}

func ipcActiveBranch(in *orchestrator.ActiveBranchProvenance) *ipc.TaskActiveBranch {
	if in == nil {
		return nil
	}
	return &ipc.TaskActiveBranch{
		TaskID:                 in.TaskID,
		Class:                  string(in.Class),
		BranchRef:              in.BranchRef,
		ActionabilityAnchor:    string(in.ActionabilityAnchor),
		ActionabilityAnchorRef: in.ActionabilityAnchorRef,
		Reason:                 in.Reason,
	}
}

func ipcLocalResumeAuthority(in *orchestrator.LocalResumeAuthority) *ipc.TaskLocalResumeAuthority {
	if in == nil {
		return nil
	}
	return &ipc.TaskLocalResumeAuthority{
		TaskID:              in.TaskID,
		State:               string(in.State),
		Mode:                string(in.Mode),
		CheckpointID:        in.CheckpointID,
		RunID:               in.RunID,
		BlockingBranchClass: string(in.BlockingBranchClass),
		BlockingBranchRef:   in.BlockingBranchRef,
		Reason:              in.Reason,
	}
}

func ipcLocalRunFinalization(in *orchestrator.LocalRunFinalization) *ipc.TaskLocalRunFinalization {
	if in == nil {
		return nil
	}
	return &ipc.TaskLocalRunFinalization{
		TaskID:       in.TaskID,
		State:        string(in.State),
		RunID:        in.RunID,
		RunStatus:    in.RunStatus,
		CheckpointID: in.CheckpointID,
		Reason:       in.Reason,
	}
}

func ipcOperatorActionAuthoritySet(in *orchestrator.OperatorActionAuthoritySet) *ipc.TaskOperatorActionAuthoritySet {
	if in == nil {
		return nil
	}
	out := &ipc.TaskOperatorActionAuthoritySet{RequiredNextAction: string(in.RequiredNextAction)}
	if len(in.Actions) > 0 {
		out.Actions = make([]ipc.TaskOperatorActionAuthority, 0, len(in.Actions))
		for _, action := range in.Actions {
			out.Actions = append(out.Actions, ipc.TaskOperatorActionAuthority{
				Action:              string(action.Action),
				State:               string(action.State),
				Reason:              action.Reason,
				BlockingBranchClass: string(action.BlockingBranchClass),
				BlockingBranchRef:   action.BlockingBranchRef,
				AnchorKind:          string(action.AnchorKind),
				AnchorRef:           action.AnchorRef,
			})
		}
	}
	return out
}

func ipcOperatorActionAuthorities(in []orchestrator.OperatorActionAuthority) []ipc.TaskOperatorActionAuthority {
	if len(in) == 0 {
		return nil
	}
	out := make([]ipc.TaskOperatorActionAuthority, 0, len(in))
	for _, action := range in {
		out = append(out, ipc.TaskOperatorActionAuthority{
			Action:              string(action.Action),
			State:               string(action.State),
			Reason:              action.Reason,
			BlockingBranchClass: string(action.BlockingBranchClass),
			BlockingBranchRef:   action.BlockingBranchRef,
			AnchorKind:          string(action.AnchorKind),
			AnchorRef:           action.AnchorRef,
		})
	}
	return out
}

func ipcOperatorDecisionSummary(in *orchestrator.OperatorDecisionSummary) *ipc.TaskOperatorDecisionSummary {
	if in == nil {
		return nil
	}
	out := &ipc.TaskOperatorDecisionSummary{
		ActiveOwnerClass:   string(in.ActiveOwnerClass),
		ActiveOwnerRef:     in.ActiveOwnerRef,
		Headline:           in.Headline,
		RequiredNextAction: string(in.RequiredNextAction),
		PrimaryReason:      in.PrimaryReason,
		Guidance:           in.Guidance,
		IntegrityNote:      in.IntegrityNote,
	}
	if len(in.BlockedActions) > 0 {
		out.BlockedActions = make([]ipc.TaskOperatorDecisionBlockedAction, 0, len(in.BlockedActions))
		for _, blocked := range in.BlockedActions {
			out.BlockedActions = append(out.BlockedActions, ipc.TaskOperatorDecisionBlockedAction{
				Action: string(blocked.Action),
				Reason: blocked.Reason,
			})
		}
	}
	return out
}

func ipcOperatorStepReceipt(in *operatorstep.Receipt) *ipc.TaskOperatorStepReceipt {
	if in == nil {
		return nil
	}
	out := &ipc.TaskOperatorStepReceipt{
		ReceiptID:          in.ReceiptID,
		TaskID:             in.TaskID,
		ActionHandle:       in.ActionHandle,
		ExecutionDomain:    in.ExecutionDomain,
		CommandSurfaceKind: in.CommandSurfaceKind,
		ExecutionAttempted: in.ExecutionAttempted,
		ResultClass:        string(in.ResultClass),
		Summary:            in.Summary,
		Reason:             in.Reason,
		RunID:              in.RunID,
		CheckpointID:       in.CheckpointID,
		BriefID:            in.BriefID,
		HandoffID:          in.HandoffID,
		LaunchAttemptID:    in.LaunchAttemptID,
		LaunchID:           in.LaunchID,
		CreatedAt:          in.CreatedAt,
	}
	if in.CompletedAt != nil {
		out.CompletedAt = *in.CompletedAt
	}
	return out
}

func ipcOperatorStepReceipts(in []operatorstep.Receipt) []ipc.TaskOperatorStepReceipt {
	if len(in) == 0 {
		return nil
	}
	out := make([]ipc.TaskOperatorStepReceipt, 0, len(in))
	for i := range in {
		if mapped := ipcOperatorStepReceipt(&in[i]); mapped != nil {
			out = append(out, *mapped)
		}
	}
	return out
}

func ipcOperatorExecutionPlan(in *orchestrator.OperatorExecutionPlan) *ipc.TaskOperatorExecutionPlan {
	if in == nil {
		return nil
	}
	out := &ipc.TaskOperatorExecutionPlan{
		MandatoryBeforeProgress: in.MandatoryBeforeProgress,
	}
	if in.PrimaryStep != nil {
		out.PrimaryStep = &ipc.TaskOperatorExecutionStep{
			Action:         string(in.PrimaryStep.Action),
			Status:         string(in.PrimaryStep.Status),
			Domain:         string(in.PrimaryStep.Domain),
			CommandSurface: string(in.PrimaryStep.CommandSurface),
			CommandHint:    in.PrimaryStep.CommandHint,
			Reason:         in.PrimaryStep.Reason,
		}
	}
	if len(in.SecondarySteps) > 0 {
		out.SecondarySteps = make([]ipc.TaskOperatorExecutionStep, 0, len(in.SecondarySteps))
		for _, step := range in.SecondarySteps {
			out.SecondarySteps = append(out.SecondarySteps, ipc.TaskOperatorExecutionStep{
				Action:         string(step.Action),
				Status:         string(step.Status),
				Domain:         string(step.Domain),
				CommandSurface: string(step.CommandSurface),
				CommandHint:    step.CommandHint,
				Reason:         step.Reason,
			})
		}
	}
	if len(in.BlockedSteps) > 0 {
		out.BlockedSteps = make([]ipc.TaskOperatorExecutionStep, 0, len(in.BlockedSteps))
		for _, step := range in.BlockedSteps {
			out.BlockedSteps = append(out.BlockedSteps, ipc.TaskOperatorExecutionStep{
				Action:         string(step.Action),
				Status:         string(step.Status),
				Domain:         string(step.Domain),
				CommandSurface: string(step.CommandSurface),
				CommandHint:    step.CommandHint,
				Reason:         step.Reason,
			})
		}
	}
	return out
}

func ipcShellHandoffContinuity(in *orchestrator.ShellHandoffContinuitySummary) *ipc.TaskShellHandoffContinuity {
	if in == nil {
		return nil
	}
	return &ipc.TaskShellHandoffContinuity{
		State:                        string(in.State),
		Reason:                       in.Reason,
		LaunchAttemptID:              in.LaunchAttemptID,
		LaunchID:                     in.LaunchID,
		AcknowledgmentID:             in.AcknowledgmentID,
		AcknowledgmentStatus:         string(in.AcknowledgmentStatus),
		AcknowledgmentSummary:        in.AcknowledgmentSummary,
		FollowThroughID:              in.FollowThroughID,
		FollowThroughKind:            string(in.FollowThroughKind),
		FollowThroughSummary:         in.FollowThroughSummary,
		ResolutionID:                 in.ResolutionID,
		ResolutionKind:               string(in.ResolutionKind),
		ResolutionSummary:            in.ResolutionSummary,
		DownstreamContinuationProven: in.DownstreamContinuationProven,
	}
}

func ipcShellActiveBranch(in *orchestrator.ShellActiveBranchSummary) *ipc.TaskShellActiveBranch {
	if in == nil {
		return nil
	}
	return &ipc.TaskShellActiveBranch{
		Class:                  string(in.Class),
		BranchRef:              in.BranchRef,
		ActionabilityAnchor:    string(in.ActionabilityAnchor),
		ActionabilityAnchorRef: in.ActionabilityAnchorRef,
		Reason:                 in.Reason,
	}
}

func ipcShellLocalResumeAuthority(in *orchestrator.ShellLocalResumeAuthoritySummary) *ipc.TaskShellLocalResumeAuthority {
	if in == nil {
		return nil
	}
	return &ipc.TaskShellLocalResumeAuthority{
		State:               string(in.State),
		Mode:                string(in.Mode),
		CheckpointID:        in.CheckpointID,
		RunID:               in.RunID,
		BlockingBranchClass: string(in.BlockingBranchClass),
		BlockingBranchRef:   in.BlockingBranchRef,
		Reason:              in.Reason,
	}
}

func ipcShellLocalRunFinalization(in *orchestrator.ShellLocalRunFinalizationSummary) *ipc.TaskShellLocalRunFinalization {
	if in == nil {
		return nil
	}
	return &ipc.TaskShellLocalRunFinalization{
		State:        string(in.State),
		RunID:        in.RunID,
		RunStatus:    in.RunStatus,
		CheckpointID: in.CheckpointID,
		Reason:       in.Reason,
	}
}

func ipcShellOperatorActionAuthoritySet(in *orchestrator.ShellOperatorActionAuthoritySet) *ipc.TaskShellOperatorActionAuthoritySet {
	if in == nil {
		return nil
	}
	out := &ipc.TaskShellOperatorActionAuthoritySet{RequiredNextAction: string(in.RequiredNextAction)}
	if len(in.Actions) > 0 {
		out.Actions = make([]ipc.TaskShellOperatorActionAuthority, 0, len(in.Actions))
		for _, action := range in.Actions {
			out.Actions = append(out.Actions, ipc.TaskShellOperatorActionAuthority{
				Action:              string(action.Action),
				State:               string(action.State),
				Reason:              action.Reason,
				BlockingBranchClass: string(action.BlockingBranchClass),
				BlockingBranchRef:   action.BlockingBranchRef,
				AnchorKind:          string(action.AnchorKind),
				AnchorRef:           action.AnchorRef,
			})
		}
	}
	return out
}

func ipcShellOperatorDecisionSummary(in *orchestrator.ShellOperatorDecisionSummary) *ipc.TaskShellOperatorDecisionSummary {
	if in == nil {
		return nil
	}
	out := &ipc.TaskShellOperatorDecisionSummary{
		ActiveOwnerClass:   string(in.ActiveOwnerClass),
		ActiveOwnerRef:     in.ActiveOwnerRef,
		Headline:           in.Headline,
		RequiredNextAction: string(in.RequiredNextAction),
		PrimaryReason:      in.PrimaryReason,
		Guidance:           in.Guidance,
		IntegrityNote:      in.IntegrityNote,
	}
	if len(in.BlockedActions) > 0 {
		out.BlockedActions = make([]ipc.TaskShellOperatorDecisionBlockedAction, 0, len(in.BlockedActions))
		for _, blocked := range in.BlockedActions {
			out.BlockedActions = append(out.BlockedActions, ipc.TaskShellOperatorDecisionBlockedAction{
				Action: string(blocked.Action),
				Reason: blocked.Reason,
			})
		}
	}
	return out
}

func ipcShellOperatorStepReceipt(in *operatorstep.Receipt) *ipc.TaskShellOperatorStepReceipt {
	if in == nil {
		return nil
	}
	return &ipc.TaskShellOperatorStepReceipt{
		ReceiptID:    in.ReceiptID,
		ActionHandle: in.ActionHandle,
		ResultClass:  string(in.ResultClass),
		Summary:      in.Summary,
		Reason:       in.Reason,
		CreatedAt:    in.CreatedAt,
	}
}

func ipcShellOperatorStepReceipts(in []operatorstep.Receipt) []ipc.TaskShellOperatorStepReceipt {
	if len(in) == 0 {
		return nil
	}
	out := make([]ipc.TaskShellOperatorStepReceipt, 0, len(in))
	for i := range in {
		if mapped := ipcShellOperatorStepReceipt(&in[i]); mapped != nil {
			out = append(out, *mapped)
		}
	}
	return out
}

func ipcShellOperatorExecutionPlan(in *orchestrator.ShellOperatorExecutionPlan) *ipc.TaskShellOperatorExecutionPlan {
	if in == nil {
		return nil
	}
	out := &ipc.TaskShellOperatorExecutionPlan{
		MandatoryBeforeProgress: in.MandatoryBeforeProgress,
	}
	if in.PrimaryStep != nil {
		out.PrimaryStep = &ipc.TaskShellOperatorExecutionStep{
			Action:         string(in.PrimaryStep.Action),
			Status:         string(in.PrimaryStep.Status),
			Domain:         string(in.PrimaryStep.Domain),
			CommandSurface: string(in.PrimaryStep.CommandSurface),
			CommandHint:    in.PrimaryStep.CommandHint,
			Reason:         in.PrimaryStep.Reason,
		}
	}
	if len(in.SecondarySteps) > 0 {
		out.SecondarySteps = make([]ipc.TaskShellOperatorExecutionStep, 0, len(in.SecondarySteps))
		for _, step := range in.SecondarySteps {
			out.SecondarySteps = append(out.SecondarySteps, ipc.TaskShellOperatorExecutionStep{
				Action:         string(step.Action),
				Status:         string(step.Status),
				Domain:         string(step.Domain),
				CommandSurface: string(step.CommandSurface),
				CommandHint:    step.CommandHint,
				Reason:         step.Reason,
			})
		}
	}
	if len(in.BlockedSteps) > 0 {
		out.BlockedSteps = make([]ipc.TaskShellOperatorExecutionStep, 0, len(in.BlockedSteps))
		for _, step := range in.BlockedSteps {
			out.BlockedSteps = append(out.BlockedSteps, ipc.TaskShellOperatorExecutionStep{
				Action:         string(step.Action),
				Status:         string(step.Status),
				Domain:         string(step.Domain),
				CommandSurface: string(step.CommandSurface),
				CommandHint:    step.CommandHint,
				Reason:         step.Reason,
			})
		}
	}
	return out
}

type Service struct {
	SocketPath string
	Handler    orchestrator.Service
}

func NewService(socketPath string, handler orchestrator.Service) *Service {
	return &Service{SocketPath: socketPath, Handler: handler}
}

func (s *Service) Run(ctx context.Context) error {
	if s.Handler == nil {
		return errors.New("daemon handler is required")
	}
	if s.SocketPath == "" {
		return errors.New("daemon socket path is required")
	}

	if err := os.MkdirAll(filepath.Dir(s.SocketPath), 0o755); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}
	_ = os.Remove(s.SocketPath)

	ln, err := net.Listen("unix", s.SocketPath)
	if err != nil {
		return fmt.Errorf("listen unix socket: %w", err)
	}
	defer func() {
		_ = ln.Close()
		_ = os.Remove(s.SocketPath)
	}()

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}
			return fmt.Errorf("accept connection: %w", err)
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *Service) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	decoder := json.NewDecoder(bufio.NewReader(conn))
	encoder := json.NewEncoder(conn)

	var req ipc.Request
	if err := decoder.Decode(&req); err != nil {
		_ = encoder.Encode(ipc.Response{RequestID: req.RequestID, OK: false, Error: &ipc.ErrorPayload{Code: "BAD_REQUEST", Message: err.Error()}})
		return
	}

	resp := s.handleRequest(ctx, req)
	_ = encoder.Encode(resp)
}

func (s *Service) handleRequest(ctx context.Context, req ipc.Request) ipc.Response {
	respondErr := func(code, msg string) ipc.Response {
		return ipc.Response{RequestID: req.RequestID, OK: false, Error: &ipc.ErrorPayload{Code: code, Message: msg}}
	}
	respondOK := func(payload any) ipc.Response {
		b, err := json.Marshal(payload)
		if err != nil {
			return respondErr("ENCODE_ERROR", err.Error())
		}
		return ipc.Response{RequestID: req.RequestID, OK: true, Payload: b}
	}

	switch req.Method {
	case ipc.MethodResolveShellTaskForRepo:
		var p ipc.ResolveShellTaskForRepoRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ResolveShellTaskForRepo(ctx, p.RepoRoot, p.DefaultGoal)
		if err != nil {
			return respondErr("SHELL_TASK_RESOLVE_FAILED", err.Error())
		}
		return respondOK(ipc.ResolveShellTaskForRepoResponse{
			TaskID:   out.TaskID,
			RepoRoot: out.RepoRoot,
			Created:  out.Created,
		})
	case ipc.MethodStartTask:
		var p ipc.StartTaskRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.StartTask(ctx, p.Goal, p.RepoRoot)
		if err != nil {
			return respondErr("START_FAILED", err.Error())
		}
		return respondOK(ipc.StartTaskResponse{
			TaskID:         out.TaskID,
			ConversationID: out.ConversationID,
			Phase:          out.Phase,
			RepoAnchor: ipc.RepoAnchor{
				RepoRoot:         out.RepoAnchor.RepoRoot,
				Branch:           out.RepoAnchor.Branch,
				HeadSHA:          out.RepoAnchor.HeadSHA,
				WorkingTreeDirty: out.RepoAnchor.WorkingTreeDirty,
				CapturedAt:       out.RepoAnchor.CapturedAt,
			},
			CanonicalResponse: out.CanonicalResponse,
		})

	case ipc.MethodSendMessage:
		var p ipc.TaskMessageRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.MessageTask(ctx, string(p.TaskID), p.Message)
		if err != nil {
			return respondErr("MESSAGE_FAILED", err.Error())
		}
		return respondOK(ipc.TaskMessageResponse{
			TaskID:      out.TaskID,
			Phase:       out.Phase,
			IntentClass: string(out.IntentClass),
			BriefID:     out.BriefID,
			BriefHash:   out.BriefHash,
			RepoAnchor: ipc.RepoAnchor{
				RepoRoot:         out.RepoAnchor.RepoRoot,
				Branch:           out.RepoAnchor.Branch,
				HeadSHA:          out.RepoAnchor.HeadSHA,
				WorkingTreeDirty: out.RepoAnchor.WorkingTreeDirty,
				CapturedAt:       out.RepoAnchor.CapturedAt,
			},
			CanonicalResponse: out.CanonicalResponse,
		})

	case ipc.MethodTaskStatus:
		var p ipc.TaskStatusRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.StatusTask(ctx, string(p.TaskID))
		if err != nil {
			return respondErr("STATUS_FAILED", err.Error())
		}
		latestCheckpointAt := int64(0)
		if !out.LatestCheckpointAt.IsZero() {
			latestCheckpointAt = out.LatestCheckpointAt.UnixMilli()
		}
		lastEventAt := int64(0)
		if !out.LastEventAt.IsZero() {
			lastEventAt = out.LastEventAt.UnixMilli()
		}
		latestResolutionAt := int64(0)
		if !out.LatestResolutionAt.IsZero() {
			latestResolutionAt = out.LatestResolutionAt.UnixMilli()
		}
		return respondOK(ipc.TaskStatusResponse{
			TaskID:               out.TaskID,
			ConversationID:       out.ConversationID,
			Goal:                 out.Goal,
			Phase:                out.Phase,
			Status:               out.Status,
			CurrentIntentID:      out.CurrentIntentID,
			CurrentIntentClass:   string(out.CurrentIntentClass),
			CurrentIntentSummary: out.CurrentIntentSummary,
			CurrentBriefID:       out.CurrentBriefID,
			CurrentBriefHash:     out.CurrentBriefHash,
			LatestRunID:          out.LatestRunID,
			LatestRunStatus:      out.LatestRunStatus,
			LatestRunSummary:     out.LatestRunSummary,
			RepoAnchor: ipc.RepoAnchor{
				RepoRoot:         out.RepoAnchor.RepoRoot,
				Branch:           out.RepoAnchor.Branch,
				HeadSHA:          out.RepoAnchor.HeadSHA,
				WorkingTreeDirty: out.RepoAnchor.WorkingTreeDirty,
				CapturedAt:       out.RepoAnchor.CapturedAt,
			},
			LatestCheckpointID:               out.LatestCheckpointID,
			LatestCheckpointAtUnixMs:         latestCheckpointAt,
			LatestCheckpointTrigger:          string(out.LatestCheckpointTrigger),
			CheckpointResumable:              out.CheckpointResumable,
			ResumeDescriptor:                 out.ResumeDescriptor,
			LatestLaunchAttemptID:            out.LatestLaunchAttemptID,
			LatestLaunchID:                   out.LatestLaunchID,
			LatestLaunchStatus:               string(out.LatestLaunchStatus),
			LatestAcknowledgmentID:           out.LatestAcknowledgmentID,
			LatestAcknowledgmentStatus:       string(out.LatestAcknowledgmentStatus),
			LatestAcknowledgmentSummary:      out.LatestAcknowledgmentSummary,
			LatestFollowThroughID:            out.LatestFollowThroughID,
			LatestFollowThroughKind:          string(out.LatestFollowThroughKind),
			LatestFollowThroughSummary:       out.LatestFollowThroughSummary,
			LatestResolutionID:               out.LatestResolutionID,
			LatestResolutionKind:             string(out.LatestResolutionKind),
			LatestResolutionSummary:          out.LatestResolutionSummary,
			LatestResolutionAtUnixMs:         latestResolutionAt,
			LaunchControlState:               string(out.LaunchControlState),
			LaunchRetryDisposition:           string(out.LaunchRetryDisposition),
			LaunchControlReason:              out.LaunchControlReason,
			HandoffContinuityState:           string(out.HandoffContinuityState),
			HandoffContinuityReason:          out.HandoffContinuityReason,
			HandoffContinuationProven:        out.HandoffContinuationProven,
			ActiveBranchClass:                string(out.ActiveBranchClass),
			ActiveBranchRef:                  out.ActiveBranchRef,
			ActiveBranchAnchorKind:           string(out.ActiveBranchAnchorKind),
			ActiveBranchAnchorRef:            out.ActiveBranchAnchorRef,
			ActiveBranchReason:               out.ActiveBranchReason,
			LocalRunFinalizationState:        string(out.LocalRunFinalizationState),
			LocalRunFinalizationRunID:        out.LocalRunFinalizationRunID,
			LocalRunFinalizationStatus:       out.LocalRunFinalizationStatus,
			LocalRunFinalizationCheckpointID: out.LocalRunFinalizationCheckpointID,
			LocalRunFinalizationReason:       out.LocalRunFinalizationReason,
			LocalResumeAuthorityState:        string(out.LocalResumeAuthorityState),
			LocalResumeMode:                  string(out.LocalResumeMode),
			LocalResumeCheckpointID:          out.LocalResumeCheckpointID,
			LocalResumeRunID:                 out.LocalResumeRunID,
			LocalResumeReason:                out.LocalResumeReason,
			RequiredNextOperatorAction:       string(out.RequiredNextOperatorAction),
			ActionAuthority:                  ipcOperatorActionAuthorities(out.ActionAuthority),
			OperatorDecision:                 ipcOperatorDecisionSummary(out.OperatorDecision),
			OperatorExecutionPlan:            ipcOperatorExecutionPlan(out.OperatorExecutionPlan),
			LatestOperatorStepReceipt:        ipcOperatorStepReceipt(out.LatestOperatorStepReceipt),
			RecentOperatorStepReceipts:       ipcOperatorStepReceipts(out.RecentOperatorStepReceipts),
			IsResumable:                      out.IsResumable,
			RecoveryClass:                    string(out.RecoveryClass),
			RecommendedAction:                string(out.RecommendedAction),
			ReadyForNextRun:                  out.ReadyForNextRun,
			ReadyForHandoffLaunch:            out.ReadyForHandoffLaunch,
			RecoveryReason:                   out.RecoveryReason,
			LatestRecoveryAction:             ipcRecoveryActionRecord(out.LatestRecoveryAction),
			LastEventType:                    string(out.LastEventType),
			LastEventID:                      out.LastEventID,
			LastEventAtUnixMs:                lastEventAt,
		})
	case ipc.MethodRecordRecoveryAction:
		var p ipc.TaskRecordRecoveryActionRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.RecordRecoveryAction(ctx, orchestrator.RecordRecoveryActionRequest{
			TaskID:  string(p.TaskID),
			Kind:    recoveryaction.Kind(p.Kind),
			Summary: p.Summary,
			Notes:   append([]string{}, p.Notes...),
		})
		if err != nil {
			return respondErr("RECOVERY_ACTION_FAILED", err.Error())
		}
		action := ipcRecoveryActionRecord(&out.Action)
		if action == nil {
			return respondErr("RECOVERY_ACTION_FAILED", "missing recovery action payload")
		}
		return respondOK(ipc.TaskRecordRecoveryActionResponse{
			TaskID:                out.TaskID,
			Action:                *action,
			RecoveryClass:         string(out.RecoveryClass),
			RecommendedAction:     string(out.RecommendedAction),
			ReadyForNextRun:       out.ReadyForNextRun,
			ReadyForHandoffLaunch: out.ReadyForHandoffLaunch,
			RecoveryReason:        out.RecoveryReason,
			CanonicalResponse:     out.CanonicalResponse,
		})
	case ipc.MethodReviewInterruptedRun:
		var p ipc.TaskReviewInterruptedRunRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.RecordRecoveryAction(ctx, orchestrator.RecordRecoveryActionRequest{
			TaskID:  string(p.TaskID),
			Kind:    recoveryaction.KindInterruptedRunReviewed,
			Summary: p.Summary,
			Notes:   append([]string{}, p.Notes...),
		})
		if err != nil {
			return respondErr("INTERRUPTED_REVIEW_FAILED", err.Error())
		}
		action := ipcRecoveryActionRecord(&out.Action)
		if action == nil {
			return respondErr("INTERRUPTED_REVIEW_FAILED", "missing interrupted review action payload")
		}
		return respondOK(ipc.TaskRecordRecoveryActionResponse{
			TaskID:                out.TaskID,
			Action:                *action,
			RecoveryClass:         string(out.RecoveryClass),
			RecommendedAction:     string(out.RecommendedAction),
			ReadyForNextRun:       out.ReadyForNextRun,
			ReadyForHandoffLaunch: out.ReadyForHandoffLaunch,
			RecoveryReason:        out.RecoveryReason,
			CanonicalResponse:     out.CanonicalResponse,
		})
	case ipc.MethodExecuteRebrief:
		var p ipc.TaskRebriefRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ExecuteRebrief(ctx, orchestrator.ExecuteRebriefRequest{TaskID: string(p.TaskID)})
		if err != nil {
			return respondErr("REBRIEF_FAILED", err.Error())
		}
		return respondOK(ipc.TaskRebriefResponse{
			TaskID:                out.TaskID,
			PreviousBriefID:       out.PreviousBriefID,
			BriefID:               out.BriefID,
			BriefHash:             out.BriefHash,
			RecoveryClass:         string(out.RecoveryClass),
			RecommendedAction:     string(out.RecommendedAction),
			ReadyForNextRun:       out.ReadyForNextRun,
			ReadyForHandoffLaunch: out.ReadyForHandoffLaunch,
			RecoveryReason:        out.RecoveryReason,
			CanonicalResponse:     out.CanonicalResponse,
		})
	case ipc.MethodExecuteInterruptedResume:
		var p ipc.TaskInterruptedResumeRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ExecuteInterruptedResume(ctx, orchestrator.ExecuteInterruptedResumeRequest{
			TaskID:  string(p.TaskID),
			Summary: p.Summary,
			Notes:   append([]string{}, p.Notes...),
		})
		if err != nil {
			return respondErr("INTERRUPTED_RESUME_FAILED", err.Error())
		}
		action := ipcRecoveryActionRecord(&out.Action)
		if action == nil {
			return respondErr("INTERRUPTED_RESUME_FAILED", "missing interrupted resume action payload")
		}
		return respondOK(ipc.TaskInterruptedResumeResponse{
			TaskID:                out.TaskID,
			BriefID:               out.BriefID,
			BriefHash:             out.BriefHash,
			Action:                *action,
			RecoveryClass:         string(out.RecoveryClass),
			RecommendedAction:     string(out.RecommendedAction),
			ReadyForNextRun:       out.ReadyForNextRun,
			ReadyForHandoffLaunch: out.ReadyForHandoffLaunch,
			RecoveryReason:        out.RecoveryReason,
			CanonicalResponse:     out.CanonicalResponse,
		})
	case ipc.MethodExecuteContinueRecovery:
		var p ipc.TaskContinueRecoveryRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ExecuteContinueRecovery(ctx, orchestrator.ExecuteContinueRecoveryRequest{TaskID: string(p.TaskID)})
		if err != nil {
			return respondErr("CONTINUE_RECOVERY_FAILED", err.Error())
		}
		action := ipcRecoveryActionRecord(&out.Action)
		if action == nil {
			return respondErr("CONTINUE_RECOVERY_FAILED", "missing continue recovery action payload")
		}
		return respondOK(ipc.TaskContinueRecoveryResponse{
			TaskID:                out.TaskID,
			BriefID:               out.BriefID,
			BriefHash:             out.BriefHash,
			Action:                *action,
			RecoveryClass:         string(out.RecoveryClass),
			RecommendedAction:     string(out.RecommendedAction),
			ReadyForNextRun:       out.ReadyForNextRun,
			ReadyForHandoffLaunch: out.ReadyForHandoffLaunch,
			RecoveryReason:        out.RecoveryReason,
			CanonicalResponse:     out.CanonicalResponse,
		})
	case ipc.MethodExecutePrimaryOperatorStep:
		var p ipc.TaskExecutePrimaryOperatorStepRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ExecutePrimaryOperatorStep(ctx, orchestrator.ExecutePrimaryOperatorStepRequest{TaskID: string(p.TaskID)})
		if err != nil {
			return respondErr("OPERATOR_NEXT_FAILED", err.Error())
		}
		receipt := ipcOperatorStepReceipt(&out.Receipt)
		if receipt == nil {
			return respondErr("OPERATOR_NEXT_FAILED", "missing operator step receipt")
		}
		return respondOK(ipc.TaskExecutePrimaryOperatorStepResponse{
			TaskID:                     out.TaskID,
			Receipt:                    *receipt,
			ActiveBranch:               ipcActiveBranch(&out.ActiveBranch),
			OperatorDecision:           ipcOperatorDecisionSummary(&out.OperatorDecision),
			OperatorExecutionPlan:      ipcOperatorExecutionPlan(&out.OperatorExecutionPlan),
			RecoveryClass:              string(out.RecoveryClass),
			RecommendedAction:          string(out.RecommendedAction),
			ReadyForNextRun:            out.ReadyForNextRun,
			ReadyForHandoffLaunch:      out.ReadyForHandoffLaunch,
			RecoveryReason:             out.RecoveryReason,
			CanonicalResponse:          out.CanonicalResponse,
			RecentOperatorStepReceipts: ipcOperatorStepReceipts(out.RecentOperatorStepReceipts),
		})
	case ipc.MethodTaskRun:
		var p ipc.TaskRunRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.RunTask(ctx, orchestrator.RunTaskRequest{
			TaskID:             string(p.TaskID),
			Action:             p.Action,
			Mode:               p.Mode,
			RunID:              p.RunID,
			SimulateInterrupt:  p.SimulateInterrupt,
			InterruptionReason: p.InterruptionReason,
		})
		if err != nil {
			return respondErr("RUN_FAILED", err.Error())
		}
		return respondOK(ipc.TaskRunResponse{
			TaskID:            out.TaskID,
			RunID:             out.RunID,
			RunStatus:         out.RunStatus,
			Phase:             out.Phase,
			CanonicalResponse: out.CanonicalResponse,
		})
	case ipc.MethodTaskInspect:
		var p ipc.TaskInspectRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.InspectTask(ctx, string(p.TaskID))
		if err != nil {
			return respondErr("INSPECT_FAILED", err.Error())
		}
		resp := ipc.TaskInspectResponse{
			TaskID: out.TaskID,
			RepoAnchor: ipc.RepoAnchor{
				RepoRoot:         out.RepoAnchor.RepoRoot,
				Branch:           out.RepoAnchor.Branch,
				HeadSHA:          out.RepoAnchor.HeadSHA,
				WorkingTreeDirty: out.RepoAnchor.WorkingTreeDirty,
				CapturedAt:       out.RepoAnchor.CapturedAt,
			},
			Intent:                     out.Intent,
			Brief:                      out.Brief,
			Run:                        out.Run,
			Checkpoint:                 out.Checkpoint,
			Handoff:                    out.Handoff,
			Launch:                     out.Launch,
			Acknowledgment:             out.Acknowledgment,
			FollowThrough:              out.FollowThrough,
			Resolution:                 out.Resolution,
			ActiveBranch:               ipcActiveBranch(out.ActiveBranch),
			LocalRunFinalization:       ipcLocalRunFinalization(out.LocalRunFinalization),
			LocalResumeAuthority:       ipcLocalResumeAuthority(out.LocalResumeAuthority),
			ActionAuthority:            ipcOperatorActionAuthoritySet(out.ActionAuthority),
			OperatorDecision:           ipcOperatorDecisionSummary(out.OperatorDecision),
			OperatorExecutionPlan:      ipcOperatorExecutionPlan(out.OperatorExecutionPlan),
			LatestOperatorStepReceipt:  ipcOperatorStepReceipt(out.LatestOperatorStepReceipt),
			RecentOperatorStepReceipts: ipcOperatorStepReceipts(out.RecentOperatorStepReceipts),
			LaunchControl:              ipcLaunchControl(out.LaunchControl),
			HandoffContinuity:          ipcHandoffContinuity(out.HandoffContinuity),
			Recovery:                   ipcRecoveryAssessment(out.Recovery),
			LatestRecoveryAction:       ipcRecoveryActionRecord(out.LatestRecoveryAction),
		}
		if len(out.RecentRecoveryActions) > 0 {
			resp.RecentRecoveryActions = make([]ipc.TaskRecoveryActionRecord, 0, len(out.RecentRecoveryActions))
			for i := range out.RecentRecoveryActions {
				if mapped := ipcRecoveryActionRecord(&out.RecentRecoveryActions[i]); mapped != nil {
					resp.RecentRecoveryActions = append(resp.RecentRecoveryActions, *mapped)
				}
			}
		}
		return respondOK(resp)
	case ipc.MethodTaskShellSnapshot:
		var p ipc.TaskShellSnapshotRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ShellSnapshotTask(ctx, string(p.TaskID))
		if err != nil {
			return respondErr("SHELL_SNAPSHOT_FAILED", err.Error())
		}
		resp := ipc.TaskShellSnapshotResponse{
			TaskID:        out.TaskID,
			Goal:          out.Goal,
			Phase:         out.Phase,
			Status:        out.Status,
			IntentClass:   out.IntentClass,
			IntentSummary: out.IntentSummary,
			RepoAnchor: ipc.RepoAnchor{
				RepoRoot:         out.RepoAnchor.RepoRoot,
				Branch:           out.RepoAnchor.Branch,
				HeadSHA:          out.RepoAnchor.HeadSHA,
				WorkingTreeDirty: out.RepoAnchor.WorkingTreeDirty,
				CapturedAt:       out.RepoAnchor.CapturedAt,
			},
			LatestCanonicalResponse: out.LatestCanonicalResponse,
		}
		if out.Brief != nil {
			resp.Brief = &ipc.TaskShellBrief{
				BriefID:          out.Brief.BriefID,
				Objective:        out.Brief.Objective,
				NormalizedAction: out.Brief.NormalizedAction,
				Constraints:      append([]string{}, out.Brief.Constraints...),
				DoneCriteria:     append([]string{}, out.Brief.DoneCriteria...),
			}
		}
		if out.Run != nil {
			resp.Run = &ipc.TaskShellRun{
				RunID:              out.Run.RunID,
				WorkerKind:         out.Run.WorkerKind,
				Status:             out.Run.Status,
				LastKnownSummary:   out.Run.LastKnownSummary,
				StartedAt:          out.Run.StartedAt,
				EndedAt:            out.Run.EndedAt,
				InterruptionReason: out.Run.InterruptionReason,
			}
		}
		if out.Checkpoint != nil {
			resp.Checkpoint = &ipc.TaskShellCheckpoint{
				CheckpointID:     out.Checkpoint.CheckpointID,
				Trigger:          out.Checkpoint.Trigger,
				CreatedAt:        out.Checkpoint.CreatedAt,
				ResumeDescriptor: out.Checkpoint.ResumeDescriptor,
				IsResumable:      out.Checkpoint.IsResumable,
			}
		}
		if out.Handoff != nil {
			resp.Handoff = &ipc.TaskShellHandoff{
				HandoffID:    out.Handoff.HandoffID,
				Status:       string(out.Handoff.Status),
				SourceWorker: out.Handoff.SourceWorker,
				TargetWorker: out.Handoff.TargetWorker,
				Mode:         string(out.Handoff.Mode),
				Reason:       out.Handoff.Reason,
				AcceptedBy:   out.Handoff.AcceptedBy,
				CreatedAt:    out.Handoff.CreatedAt,
			}
		}
		if out.Launch != nil {
			resp.Launch = &ipc.TaskShellLaunch{
				AttemptID:         out.Launch.AttemptID,
				LaunchID:          out.Launch.LaunchID,
				Status:            string(out.Launch.Status),
				RequestedAt:       out.Launch.RequestedAt,
				StartedAt:         out.Launch.StartedAt,
				EndedAt:           out.Launch.EndedAt,
				Summary:           out.Launch.Summary,
				ErrorMessage:      out.Launch.ErrorMessage,
				OutputArtifactRef: out.Launch.OutputArtifactRef,
			}
		}
		resp.LaunchControl = ipcShellLaunchControl(out.LaunchControl)
		if out.Acknowledgment != nil {
			resp.Acknowledgment = &ipc.TaskShellAcknowledgment{
				Status:    string(out.Acknowledgment.Status),
				Summary:   out.Acknowledgment.Summary,
				CreatedAt: out.Acknowledgment.CreatedAt,
			}
		}
		if out.FollowThrough != nil {
			resp.FollowThrough = &ipc.TaskShellFollowThrough{
				RecordID:        out.FollowThrough.RecordID,
				Kind:            string(out.FollowThrough.Kind),
				Summary:         out.FollowThrough.Summary,
				LaunchAttemptID: out.FollowThrough.LaunchAttemptID,
				LaunchID:        out.FollowThrough.LaunchID,
				CreatedAt:       out.FollowThrough.CreatedAt,
			}
		}
		if out.Resolution != nil {
			resp.Resolution = &ipc.TaskShellResolution{
				ResolutionID:    out.Resolution.ResolutionID,
				Kind:            string(out.Resolution.Kind),
				Summary:         out.Resolution.Summary,
				LaunchAttemptID: out.Resolution.LaunchAttemptID,
				LaunchID:        out.Resolution.LaunchID,
				CreatedAt:       out.Resolution.CreatedAt,
			}
		}
		resp.ActiveBranch = ipcShellActiveBranch(out.ActiveBranch)
		resp.LocalRunFinalization = ipcShellLocalRunFinalization(out.LocalRunFinalization)
		resp.LocalResume = ipcShellLocalResumeAuthority(out.LocalResume)
		resp.ActionAuthority = ipcShellOperatorActionAuthoritySet(out.ActionAuthority)
		resp.OperatorDecision = ipcShellOperatorDecisionSummary(out.OperatorDecision)
		resp.OperatorExecutionPlan = ipcShellOperatorExecutionPlan(out.OperatorExecutionPlan)
		resp.LatestOperatorStepReceipt = ipcShellOperatorStepReceipt(out.LatestOperatorStepReceipt)
		resp.RecentOperatorStepReceipts = ipcShellOperatorStepReceipts(out.RecentOperatorStepReceipts)
		resp.HandoffContinuity = ipcShellHandoffContinuity(out.HandoffContinuity)
		resp.Recovery = ipcShellRecovery(out.Recovery)
		if len(out.RecentProofs) > 0 {
			resp.RecentProofs = make([]ipc.TaskShellProof, 0, len(out.RecentProofs))
			for _, evt := range out.RecentProofs {
				resp.RecentProofs = append(resp.RecentProofs, ipc.TaskShellProof{
					EventID:   evt.EventID,
					Type:      string(evt.Type),
					Summary:   evt.Summary,
					Timestamp: evt.Timestamp,
				})
			}
		}
		if len(out.RecentConversation) > 0 {
			resp.RecentConversation = make([]ipc.TaskShellConversation, 0, len(out.RecentConversation))
			for _, msg := range out.RecentConversation {
				resp.RecentConversation = append(resp.RecentConversation, ipc.TaskShellConversation{
					Role:      string(msg.Role),
					Body:      msg.Body,
					CreatedAt: msg.CreatedAt,
				})
			}
		}
		return respondOK(resp)
	case ipc.MethodTaskShellLifecycle:
		var p ipc.TaskShellLifecycleRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.RecordShellLifecycle(ctx, orchestrator.RecordShellLifecycleRequest{
			TaskID:     string(p.TaskID),
			SessionID:  p.SessionID,
			Kind:       orchestrator.ShellLifecycleKind(p.Kind),
			HostMode:   p.HostMode,
			HostState:  p.HostState,
			Note:       p.Note,
			InputLive:  p.InputLive,
			ExitCode:   p.ExitCode,
			PaneWidth:  p.PaneWidth,
			PaneHeight: p.PaneHeight,
		})
		if err != nil {
			return respondErr("SHELL_LIFECYCLE_FAILED", err.Error())
		}
		return respondOK(ipc.TaskShellLifecycleResponse{TaskID: out.TaskID})
	case ipc.MethodTaskShellSessionReport:
		var p ipc.TaskShellSessionReportRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ReportShellSession(ctx, orchestrator.ReportShellSessionRequest{
			TaskID:           string(p.TaskID),
			SessionID:        p.SessionID,
			WorkerPreference: p.WorkerPreference,
			ResolvedWorker:   p.ResolvedWorker,
			WorkerSessionID:  p.WorkerSessionID,
			AttachCapability: shellsession.AttachCapability(p.AttachCapability),
			HostMode:         p.HostMode,
			HostState:        p.HostState,
			StartedAt:        p.StartedAt,
			Active:           p.Active,
			Note:             p.Note,
		})
		if err != nil {
			return respondErr("SHELL_SESSION_REPORT_FAILED", err.Error())
		}
		return respondOK(ipc.TaskShellSessionReportResponse{
			TaskID: out.TaskID,
			Session: ipc.TaskShellSessionRecord{
				SessionID:        out.Session.SessionID,
				TaskID:           out.Session.TaskID,
				WorkerPreference: out.Session.WorkerPreference,
				ResolvedWorker:   out.Session.ResolvedWorker,
				WorkerSessionID:  out.Session.WorkerSessionID,
				AttachCapability: string(out.Session.AttachCapability),
				HostMode:         out.Session.HostMode,
				HostState:        out.Session.HostState,
				SessionClass:     string(out.Session.SessionClass),
				StartedAt:        out.Session.StartedAt,
				LastUpdatedAt:    out.Session.LastUpdatedAt,
				Active:           out.Session.Active,
				Note:             out.Session.Note,
			},
		})
	case ipc.MethodTaskShellSessions:
		var p ipc.TaskShellSessionsRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ListShellSessions(ctx, string(p.TaskID))
		if err != nil {
			return respondErr("SHELL_SESSIONS_FAILED", err.Error())
		}
		resp := ipc.TaskShellSessionsResponse{TaskID: out.TaskID}
		if len(out.Sessions) > 0 {
			resp.Sessions = make([]ipc.TaskShellSessionRecord, 0, len(out.Sessions))
			for _, session := range out.Sessions {
				resp.Sessions = append(resp.Sessions, ipc.TaskShellSessionRecord{
					SessionID:        session.SessionID,
					TaskID:           session.TaskID,
					WorkerPreference: session.WorkerPreference,
					ResolvedWorker:   session.ResolvedWorker,
					WorkerSessionID:  session.WorkerSessionID,
					AttachCapability: string(session.AttachCapability),
					HostMode:         session.HostMode,
					HostState:        session.HostState,
					SessionClass:     string(session.SessionClass),
					StartedAt:        session.StartedAt,
					LastUpdatedAt:    session.LastUpdatedAt,
					Active:           session.Active,
					Note:             session.Note,
				})
			}
		}
		return respondOK(resp)
	case ipc.MethodContinueTask:
		var p ipc.TaskContinueRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ContinueTask(ctx, string(p.TaskID))
		if err != nil {
			return respondErr("CONTINUE_FAILED", err.Error())
		}
		return respondOK(ipc.TaskContinueResponse{
			TaskID:                out.TaskID,
			Outcome:               string(out.Outcome),
			DriftClass:            out.DriftClass,
			Phase:                 out.Phase,
			RunID:                 out.RunID,
			CheckpointID:          out.CheckpointID,
			ResumeDescriptor:      out.ResumeDescriptor,
			RecoveryClass:         string(out.RecoveryClass),
			RecommendedAction:     string(out.RecommendedAction),
			ReadyForNextRun:       out.ReadyForNextRun,
			ReadyForHandoffLaunch: out.ReadyForHandoffLaunch,
			RecoveryReason:        out.RecoveryReason,
			CanonicalResponse:     out.CanonicalResponse,
		})
	case ipc.MethodCreateCheckpoint:
		var p ipc.TaskCheckpointRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.CreateCheckpoint(ctx, string(p.TaskID))
		if err != nil {
			return respondErr("CHECKPOINT_FAILED", err.Error())
		}
		return respondOK(ipc.TaskCheckpointResponse{
			TaskID:            out.TaskID,
			CheckpointID:      out.CheckpointID,
			Trigger:           out.Trigger,
			IsResumable:       out.IsResumable,
			CanonicalResponse: out.CanonicalResponse,
		})
	case ipc.MethodCreateHandoff:
		var p ipc.TaskHandoffCreateRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.CreateHandoff(ctx, orchestrator.CreateHandoffRequest{
			TaskID:       string(p.TaskID),
			TargetWorker: p.TargetWorker,
			Reason:       p.Reason,
			Mode:         p.Mode,
			Notes:        append([]string{}, p.Notes...),
		})
		if err != nil {
			return respondErr("HANDOFF_CREATE_FAILED", err.Error())
		}
		return respondOK(ipc.TaskHandoffCreateResponse{
			TaskID:            out.TaskID,
			HandoffID:         out.HandoffID,
			SourceWorker:      out.SourceWorker,
			TargetWorker:      out.TargetWorker,
			Status:            string(out.Status),
			CheckpointID:      out.CheckpointID,
			BriefID:           out.BriefID,
			CanonicalResponse: out.CanonicalResponse,
			Packet:            out.Packet,
		})
	case ipc.MethodAcceptHandoff:
		var p ipc.TaskHandoffAcceptRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.AcceptHandoff(ctx, orchestrator.AcceptHandoffRequest{
			TaskID:     string(p.TaskID),
			HandoffID:  p.HandoffID,
			AcceptedBy: p.AcceptedBy,
			Notes:      append([]string{}, p.Notes...),
		})
		if err != nil {
			return respondErr("HANDOFF_ACCEPT_FAILED", err.Error())
		}
		return respondOK(ipc.TaskHandoffAcceptResponse{
			TaskID:            out.TaskID,
			HandoffID:         out.HandoffID,
			Status:            string(out.Status),
			CanonicalResponse: out.CanonicalResponse,
		})
	case ipc.MethodLaunchHandoff:
		var p ipc.TaskHandoffLaunchRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.LaunchHandoff(ctx, orchestrator.LaunchHandoffRequest{
			TaskID:       string(p.TaskID),
			HandoffID:    p.HandoffID,
			TargetWorker: p.TargetWorker,
		})
		if err != nil {
			return respondErr("HANDOFF_LAUNCH_FAILED", err.Error())
		}
		return respondOK(ipc.TaskHandoffLaunchResponse{
			TaskID:            out.TaskID,
			HandoffID:         out.HandoffID,
			TargetWorker:      out.TargetWorker,
			LaunchStatus:      string(out.LaunchStatus),
			LaunchID:          out.LaunchID,
			CanonicalResponse: out.CanonicalResponse,
			Payload:           out.Payload,
		})
	case ipc.MethodRecordHandoffFollowThrough:
		var p ipc.TaskHandoffFollowThroughRecordRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.RecordHandoffFollowThrough(ctx, orchestrator.RecordHandoffFollowThroughRequest{
			TaskID:  string(p.TaskID),
			Kind:    handoff.FollowThroughKind(p.Kind),
			Summary: p.Summary,
			Notes:   append([]string{}, p.Notes...),
		})
		if err != nil {
			return respondErr("HANDOFF_FOLLOW_THROUGH_FAILED", err.Error())
		}
		return respondOK(ipc.TaskHandoffFollowThroughRecordResponse{
			TaskID:                out.TaskID,
			Record:                &out.Record,
			HandoffContinuity:     ipcHandoffContinuity(&out.HandoffContinuity),
			RecoveryClass:         string(out.RecoveryClass),
			RecommendedAction:     string(out.RecommendedAction),
			ReadyForNextRun:       out.ReadyForNextRun,
			ReadyForHandoffLaunch: out.ReadyForHandoffLaunch,
			RecoveryReason:        out.RecoveryReason,
			CanonicalResponse:     out.CanonicalResponse,
		})
	case ipc.MethodRecordHandoffResolution:
		var p ipc.TaskHandoffResolutionRecordRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.RecordHandoffResolution(ctx, orchestrator.RecordHandoffResolutionRequest{
			TaskID:    string(p.TaskID),
			HandoffID: p.HandoffID,
			Kind:      handoff.ResolutionKind(p.Kind),
			Summary:   p.Summary,
			Notes:     append([]string{}, p.Notes...),
		})
		if err != nil {
			return respondErr("HANDOFF_RESOLUTION_FAILED", err.Error())
		}
		return respondOK(ipc.TaskHandoffResolutionRecordResponse{
			TaskID:                out.TaskID,
			Record:                &out.Record,
			HandoffContinuity:     ipcHandoffContinuity(&out.HandoffContinuity),
			RecoveryClass:         string(out.RecoveryClass),
			RecommendedAction:     string(out.RecommendedAction),
			ReadyForNextRun:       out.ReadyForNextRun,
			ReadyForHandoffLaunch: out.ReadyForHandoffLaunch,
			RecoveryReason:        out.RecoveryReason,
			CanonicalResponse:     out.CanonicalResponse,
		})
	default:
		return respondErr("UNSUPPORTED_METHOD", fmt.Sprintf("unsupported method: %s", req.Method))
	}
}
