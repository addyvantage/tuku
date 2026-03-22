package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/ipc"
)

type IPCSnapshotSource struct {
	SocketPath string
	Timeout    time.Duration
}

func NewIPCSnapshotSource(socketPath string) *IPCSnapshotSource {
	return &IPCSnapshotSource{
		SocketPath: socketPath,
		Timeout:    5 * time.Second,
	}
}

func (s *IPCSnapshotSource) Load(taskID string) (Snapshot, error) {
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	payload, err := json.Marshal(ipc.TaskShellSnapshotRequest{TaskID: ipcTaskID(taskID)})
	if err != nil {
		return Snapshot{}, err
	}
	resp, err := ipc.CallUnix(ctx, s.SocketPath, ipc.Request{
		RequestID: fmt.Sprintf("shell_%d", time.Now().UTC().UnixNano()),
		Method:    ipc.MethodTaskShellSnapshot,
		Payload:   payload,
	})
	if err != nil {
		return Snapshot{}, err
	}
	var raw ipc.TaskShellSnapshotResponse
	if err := json.Unmarshal(resp.Payload, &raw); err != nil {
		return Snapshot{}, err
	}
	return snapshotFromIPC(raw), nil
}

func snapshotFromIPC(raw ipc.TaskShellSnapshotResponse) Snapshot {
	out := Snapshot{
		TaskID:        string(raw.TaskID),
		Goal:          raw.Goal,
		Phase:         raw.Phase,
		Status:        raw.Status,
		IntentClass:   raw.IntentClass,
		IntentSummary: raw.IntentSummary,
		Repo: RepoAnchor{
			RepoRoot:         raw.RepoAnchor.RepoRoot,
			Branch:           raw.RepoAnchor.Branch,
			HeadSHA:          raw.RepoAnchor.HeadSHA,
			WorkingTreeDirty: raw.RepoAnchor.WorkingTreeDirty,
			CapturedAt:       raw.RepoAnchor.CapturedAt,
		},
		LatestCanonicalResponse: raw.LatestCanonicalResponse,
	}
	if raw.Brief != nil {
		out.Brief = &BriefSummary{
			ID:               string(raw.Brief.BriefID),
			Objective:        raw.Brief.Objective,
			NormalizedAction: raw.Brief.NormalizedAction,
			Constraints:      append([]string{}, raw.Brief.Constraints...),
			DoneCriteria:     append([]string{}, raw.Brief.DoneCriteria...),
		}
	}
	if raw.Run != nil {
		out.Run = &RunSummary{
			ID:                 string(raw.Run.RunID),
			WorkerKind:         string(raw.Run.WorkerKind),
			Status:             string(raw.Run.Status),
			LastKnownSummary:   raw.Run.LastKnownSummary,
			StartedAt:          raw.Run.StartedAt,
			EndedAt:            raw.Run.EndedAt,
			InterruptionReason: raw.Run.InterruptionReason,
		}
	}
	if raw.Checkpoint != nil {
		out.Checkpoint = &CheckpointSummary{
			ID:               string(raw.Checkpoint.CheckpointID),
			Trigger:          string(raw.Checkpoint.Trigger),
			CreatedAt:        raw.Checkpoint.CreatedAt,
			ResumeDescriptor: raw.Checkpoint.ResumeDescriptor,
			IsResumable:      raw.Checkpoint.IsResumable,
		}
	}
	if raw.Handoff != nil {
		out.Handoff = &HandoffSummary{
			ID:           raw.Handoff.HandoffID,
			Status:       raw.Handoff.Status,
			SourceWorker: string(raw.Handoff.SourceWorker),
			TargetWorker: string(raw.Handoff.TargetWorker),
			Mode:         raw.Handoff.Mode,
			Reason:       raw.Handoff.Reason,
			AcceptedBy:   string(raw.Handoff.AcceptedBy),
			CreatedAt:    raw.Handoff.CreatedAt,
		}
	}
	if raw.Launch != nil {
		out.Launch = &LaunchSummary{
			AttemptID:         raw.Launch.AttemptID,
			LaunchID:          raw.Launch.LaunchID,
			Status:            raw.Launch.Status,
			RequestedAt:       raw.Launch.RequestedAt,
			StartedAt:         raw.Launch.StartedAt,
			EndedAt:           raw.Launch.EndedAt,
			Summary:           raw.Launch.Summary,
			ErrorMessage:      raw.Launch.ErrorMessage,
			OutputArtifactRef: raw.Launch.OutputArtifactRef,
		}
	}
	if raw.LaunchControl != nil {
		out.LaunchControl = &LaunchControlSummary{
			State:            raw.LaunchControl.State,
			RetryDisposition: raw.LaunchControl.RetryDisposition,
			Reason:           raw.LaunchControl.Reason,
			HandoffID:        raw.LaunchControl.HandoffID,
			AttemptID:        raw.LaunchControl.AttemptID,
			LaunchID:         raw.LaunchControl.LaunchID,
			TargetWorker:     string(raw.LaunchControl.TargetWorker),
			RequestedAt:      raw.LaunchControl.RequestedAt,
			CompletedAt:      raw.LaunchControl.CompletedAt,
			FailedAt:         raw.LaunchControl.FailedAt,
		}
	}
	if raw.Acknowledgment != nil {
		out.Acknowledgment = &AcknowledgmentSummary{
			Status:    raw.Acknowledgment.Status,
			Summary:   raw.Acknowledgment.Summary,
			CreatedAt: raw.Acknowledgment.CreatedAt,
		}
	}
	if raw.FollowThrough != nil {
		out.FollowThrough = &FollowThroughSummary{
			RecordID:        raw.FollowThrough.RecordID,
			Kind:            raw.FollowThrough.Kind,
			Summary:         raw.FollowThrough.Summary,
			LaunchAttemptID: raw.FollowThrough.LaunchAttemptID,
			LaunchID:        raw.FollowThrough.LaunchID,
			CreatedAt:       raw.FollowThrough.CreatedAt,
		}
	}
	if raw.Resolution != nil {
		out.Resolution = &ResolutionSummary{
			ResolutionID:    raw.Resolution.ResolutionID,
			Kind:            raw.Resolution.Kind,
			Summary:         raw.Resolution.Summary,
			LaunchAttemptID: raw.Resolution.LaunchAttemptID,
			LaunchID:        raw.Resolution.LaunchID,
			CreatedAt:       raw.Resolution.CreatedAt,
		}
	}
	if raw.ActiveBranch != nil {
		out.ActiveBranch = &ActiveBranchSummary{
			Class:                  raw.ActiveBranch.Class,
			BranchRef:              raw.ActiveBranch.BranchRef,
			ActionabilityAnchor:    raw.ActiveBranch.ActionabilityAnchor,
			ActionabilityAnchorRef: raw.ActiveBranch.ActionabilityAnchorRef,
			Reason:                 raw.ActiveBranch.Reason,
		}
	}
	if raw.LocalRunFinalization != nil {
		out.LocalRunFinalization = &LocalRunFinalizationSummary{
			State:        raw.LocalRunFinalization.State,
			RunID:        string(raw.LocalRunFinalization.RunID),
			RunStatus:    string(raw.LocalRunFinalization.RunStatus),
			CheckpointID: string(raw.LocalRunFinalization.CheckpointID),
			Reason:       raw.LocalRunFinalization.Reason,
		}
	}
	if raw.LocalResume != nil {
		out.LocalResume = &LocalResumeAuthoritySummary{
			State:               raw.LocalResume.State,
			Mode:                raw.LocalResume.Mode,
			CheckpointID:        string(raw.LocalResume.CheckpointID),
			RunID:               string(raw.LocalResume.RunID),
			BlockingBranchClass: raw.LocalResume.BlockingBranchClass,
			BlockingBranchRef:   raw.LocalResume.BlockingBranchRef,
			Reason:              raw.LocalResume.Reason,
		}
	}
	if raw.ActionAuthority != nil {
		out.ActionAuthority = &OperatorActionAuthoritySet{
			RequiredNextAction: raw.ActionAuthority.RequiredNextAction,
		}
		if len(raw.ActionAuthority.Actions) > 0 {
			out.ActionAuthority.Actions = make([]OperatorActionAuthority, 0, len(raw.ActionAuthority.Actions))
			for _, action := range raw.ActionAuthority.Actions {
				out.ActionAuthority.Actions = append(out.ActionAuthority.Actions, OperatorActionAuthority{
					Action:              action.Action,
					State:               action.State,
					Reason:              action.Reason,
					BlockingBranchClass: action.BlockingBranchClass,
					BlockingBranchRef:   action.BlockingBranchRef,
					AnchorKind:          action.AnchorKind,
					AnchorRef:           action.AnchorRef,
				})
			}
		}
	}
	if raw.OperatorDecision != nil {
		out.OperatorDecision = &OperatorDecisionSummary{
			ActiveOwnerClass:   raw.OperatorDecision.ActiveOwnerClass,
			ActiveOwnerRef:     raw.OperatorDecision.ActiveOwnerRef,
			Headline:           raw.OperatorDecision.Headline,
			RequiredNextAction: raw.OperatorDecision.RequiredNextAction,
			PrimaryReason:      raw.OperatorDecision.PrimaryReason,
			Guidance:           raw.OperatorDecision.Guidance,
			IntegrityNote:      raw.OperatorDecision.IntegrityNote,
		}
		if len(raw.OperatorDecision.BlockedActions) > 0 {
			out.OperatorDecision.BlockedActions = make([]OperatorDecisionBlockedAction, 0, len(raw.OperatorDecision.BlockedActions))
			for _, blocked := range raw.OperatorDecision.BlockedActions {
				out.OperatorDecision.BlockedActions = append(out.OperatorDecision.BlockedActions, OperatorDecisionBlockedAction{
					Action: blocked.Action,
					Reason: blocked.Reason,
				})
			}
		}
	}
	if raw.OperatorExecutionPlan != nil {
		out.OperatorExecutionPlan = &OperatorExecutionPlan{
			MandatoryBeforeProgress: raw.OperatorExecutionPlan.MandatoryBeforeProgress,
		}
		if raw.OperatorExecutionPlan.PrimaryStep != nil {
			out.OperatorExecutionPlan.PrimaryStep = &OperatorExecutionStep{
				Action:         raw.OperatorExecutionPlan.PrimaryStep.Action,
				Status:         raw.OperatorExecutionPlan.PrimaryStep.Status,
				Domain:         raw.OperatorExecutionPlan.PrimaryStep.Domain,
				CommandSurface: raw.OperatorExecutionPlan.PrimaryStep.CommandSurface,
				CommandHint:    raw.OperatorExecutionPlan.PrimaryStep.CommandHint,
				Reason:         raw.OperatorExecutionPlan.PrimaryStep.Reason,
			}
		}
		if len(raw.OperatorExecutionPlan.SecondarySteps) > 0 {
			out.OperatorExecutionPlan.SecondarySteps = make([]OperatorExecutionStep, 0, len(raw.OperatorExecutionPlan.SecondarySteps))
			for _, step := range raw.OperatorExecutionPlan.SecondarySteps {
				out.OperatorExecutionPlan.SecondarySteps = append(out.OperatorExecutionPlan.SecondarySteps, OperatorExecutionStep{
					Action:         step.Action,
					Status:         step.Status,
					Domain:         step.Domain,
					CommandSurface: step.CommandSurface,
					CommandHint:    step.CommandHint,
					Reason:         step.Reason,
				})
			}
		}
		if len(raw.OperatorExecutionPlan.BlockedSteps) > 0 {
			out.OperatorExecutionPlan.BlockedSteps = make([]OperatorExecutionStep, 0, len(raw.OperatorExecutionPlan.BlockedSteps))
			for _, step := range raw.OperatorExecutionPlan.BlockedSteps {
				out.OperatorExecutionPlan.BlockedSteps = append(out.OperatorExecutionPlan.BlockedSteps, OperatorExecutionStep{
					Action:         step.Action,
					Status:         step.Status,
					Domain:         step.Domain,
					CommandSurface: step.CommandSurface,
					CommandHint:    step.CommandHint,
					Reason:         step.Reason,
				})
			}
		}
	}
	if raw.LatestOperatorStepReceipt != nil {
		out.LatestOperatorStepReceipt = &OperatorStepReceiptSummary{
			ReceiptID:    raw.LatestOperatorStepReceipt.ReceiptID,
			ActionHandle: raw.LatestOperatorStepReceipt.ActionHandle,
			ResultClass:  raw.LatestOperatorStepReceipt.ResultClass,
			Summary:      raw.LatestOperatorStepReceipt.Summary,
			Reason:       raw.LatestOperatorStepReceipt.Reason,
			CreatedAt:    raw.LatestOperatorStepReceipt.CreatedAt,
		}
	}
	if len(raw.RecentOperatorStepReceipts) > 0 {
		out.RecentOperatorStepReceipts = make([]OperatorStepReceiptSummary, 0, len(raw.RecentOperatorStepReceipts))
		for _, item := range raw.RecentOperatorStepReceipts {
			out.RecentOperatorStepReceipts = append(out.RecentOperatorStepReceipts, OperatorStepReceiptSummary{
				ReceiptID:    item.ReceiptID,
				ActionHandle: item.ActionHandle,
				ResultClass:  item.ResultClass,
				Summary:      item.Summary,
				Reason:       item.Reason,
				CreatedAt:    item.CreatedAt,
			})
		}
	}
	if raw.HandoffContinuity != nil {
		out.HandoffContinuity = &HandoffContinuitySummary{
			State:                        raw.HandoffContinuity.State,
			Reason:                       raw.HandoffContinuity.Reason,
			LaunchAttemptID:              raw.HandoffContinuity.LaunchAttemptID,
			LaunchID:                     raw.HandoffContinuity.LaunchID,
			AcknowledgmentID:             raw.HandoffContinuity.AcknowledgmentID,
			AcknowledgmentStatus:         raw.HandoffContinuity.AcknowledgmentStatus,
			AcknowledgmentSummary:        raw.HandoffContinuity.AcknowledgmentSummary,
			FollowThroughID:              raw.HandoffContinuity.FollowThroughID,
			FollowThroughKind:            raw.HandoffContinuity.FollowThroughKind,
			FollowThroughSummary:         raw.HandoffContinuity.FollowThroughSummary,
			ResolutionID:                 raw.HandoffContinuity.ResolutionID,
			ResolutionKind:               raw.HandoffContinuity.ResolutionKind,
			ResolutionSummary:            raw.HandoffContinuity.ResolutionSummary,
			DownstreamContinuationProven: raw.HandoffContinuity.DownstreamContinuationProven,
		}
	}
	if raw.Recovery != nil {
		out.Recovery = &RecoverySummary{
			ContinuityOutcome:      raw.Recovery.ContinuityOutcome,
			Class:                  raw.Recovery.RecoveryClass,
			Action:                 raw.Recovery.RecommendedAction,
			ReadyForNextRun:        raw.Recovery.ReadyForNextRun,
			ReadyForHandoffLaunch:  raw.Recovery.ReadyForHandoffLaunch,
			RequiresDecision:       raw.Recovery.RequiresDecision,
			RequiresRepair:         raw.Recovery.RequiresRepair,
			RequiresReview:         raw.Recovery.RequiresReview,
			RequiresReconciliation: raw.Recovery.RequiresReconciliation,
			DriftClass:             string(raw.Recovery.DriftClass),
			Reason:                 raw.Recovery.Reason,
			CheckpointID:           string(raw.Recovery.CheckpointID),
			RunID:                  string(raw.Recovery.RunID),
			HandoffID:              raw.Recovery.HandoffID,
			HandoffStatus:          raw.Recovery.HandoffStatus,
		}
		if len(raw.Recovery.Issues) > 0 {
			out.Recovery.Issues = make([]RecoveryIssue, 0, len(raw.Recovery.Issues))
			for _, issue := range raw.Recovery.Issues {
				out.Recovery.Issues = append(out.Recovery.Issues, RecoveryIssue{
					Code:    issue.Code,
					Message: issue.Message,
				})
			}
		}
	}
	if len(raw.RecentProofs) > 0 {
		out.RecentProofs = make([]ProofItem, 0, len(raw.RecentProofs))
		for _, evt := range raw.RecentProofs {
			out.RecentProofs = append(out.RecentProofs, ProofItem{
				ID:        string(evt.EventID),
				Type:      evt.Type,
				Summary:   evt.Summary,
				Timestamp: evt.Timestamp,
			})
		}
	}
	if len(raw.RecentConversation) > 0 {
		out.RecentConversation = make([]ConversationItem, 0, len(raw.RecentConversation))
		for _, msg := range raw.RecentConversation {
			out.RecentConversation = append(out.RecentConversation, ConversationItem{
				Role:      msg.Role,
				Body:      msg.Body,
				CreatedAt: msg.CreatedAt,
			})
		}
	}
	return out
}

func ipcTaskID(taskID string) common.TaskID {
	return common.TaskID(taskID)
}
