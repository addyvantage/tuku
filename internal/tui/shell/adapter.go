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

func continuityIncidentClosureSummaryFromIPC(in *ipc.TaskContinuityIncidentClosureSummary) *ContinuityIncidentClosureSummary {
	if in == nil {
		return nil
	}
	out := &ContinuityIncidentClosureSummary{
		Class:                             in.Class,
		Digest:                            in.Digest,
		WindowAdvisory:                    in.WindowAdvisory,
		Detail:                            in.Detail,
		BoundedWindow:                     in.BoundedWindow,
		WindowSize:                        in.WindowSize,
		DistinctAnchors:                   in.DistinctAnchors,
		OperationallyUnresolved:           in.OperationallyUnresolved,
		ClosureAppearsWeak:                in.ClosureAppearsWeak,
		ReopenedAfterClosure:              in.ReopenedAfterClosure,
		RepeatedReopenLoop:                in.RepeatedReopenLoop,
		StagnantProgression:               in.StagnantProgression,
		TriagedWithoutFollowUp:            in.TriagedWithoutFollowUp,
		AnchorsWithOpenFollowUp:           in.AnchorsWithOpenFollowUp,
		AnchorsClosed:                     in.AnchorsClosed,
		AnchorsReopened:                   in.AnchorsReopened,
		AnchorsBehindLatestTransition:     in.AnchorsBehindLatestTransition,
		AnchorsRepeatedWithoutProgression: in.AnchorsRepeatedWithoutProgression,
		AnchorsTriagedWithoutFollowUp:     in.AnchorsTriagedWithoutFollowUp,
		ReopenedAfterClosureAnchors:       in.ReopenedAfterClosureAnchors,
		RepeatedReopenLoopAnchors:         in.RepeatedReopenLoopAnchors,
		StagnantProgressionAnchors:        in.StagnantProgressionAnchors,
	}
	if len(in.RecentAnchors) > 0 {
		out.RecentAnchors = make([]ContinuityIncidentClosureAnchorItem, 0, len(in.RecentAnchors))
		for _, item := range in.RecentAnchors {
			out.RecentAnchors = append(out.RecentAnchors, ContinuityIncidentClosureAnchorItem{
				AnchorTransitionReceiptID: item.AnchorTransitionReceiptID,
				Class:                     item.Class,
				Digest:                    item.Digest,
				Explanation:               item.Explanation,
				LatestFollowUpReceiptID:   string(item.LatestFollowUpReceiptID),
				LatestFollowUpActionKind:  item.LatestFollowUpActionKind,
				LatestFollowUpAt:          item.LatestFollowUpAt,
			})
		}
	}
	return out
}

func continuityIncidentTaskRiskSummaryFromIPC(in *ipc.TaskContinuityIncidentTaskRiskSummary) *ContinuityIncidentTaskRiskSummary {
	if in == nil {
		return nil
	}
	out := &ContinuityIncidentTaskRiskSummary{
		Class:                               in.Class,
		Digest:                              in.Digest,
		WindowAdvisory:                      in.WindowAdvisory,
		Detail:                              in.Detail,
		BoundedWindow:                       in.BoundedWindow,
		WindowSize:                          in.WindowSize,
		DistinctAnchors:                     in.DistinctAnchors,
		RecurringWeakClosure:                in.RecurringWeakClosure,
		RecurringUnresolved:                 in.RecurringUnresolved,
		RecurringStagnantFollowUp:           in.RecurringStagnantFollowUp,
		RecurringTriagedWithoutFollowUp:     in.RecurringTriagedWithoutFollowUp,
		ReopenedAfterClosureAnchors:         in.ReopenedAfterClosureAnchors,
		RepeatedReopenLoopAnchors:           in.RepeatedReopenLoopAnchors,
		StagnantProgressionAnchors:          in.StagnantProgressionAnchors,
		AnchorsTriagedWithoutFollowUp:       in.AnchorsTriagedWithoutFollowUp,
		AnchorsWithOpenFollowUp:             in.AnchorsWithOpenFollowUp,
		AnchorsReopened:                     in.AnchorsReopened,
		OperationallyUnresolvedAnchorSignal: in.OperationallyUnresolvedAnchorSignal,
	}
	if len(in.RecentAnchorClasses) > 0 {
		out.RecentAnchorClasses = append([]string{}, in.RecentAnchorClasses...)
	}
	return out
}

func compiledIntentSummaryFromIPC(in *ipc.TaskCompiledIntentSummary) *CompiledIntentSummary {
	if in == nil {
		return nil
	}
	out := &CompiledIntentSummary{
		IntentID:                string(in.IntentID),
		Class:                   in.Class,
		Posture:                 in.Posture,
		ExecutionReadiness:      in.ExecutionReadiness,
		Objective:               in.Objective,
		RequestedOutcome:        in.RequestedOutcome,
		NormalizedAction:        in.NormalizedAction,
		ScopeSummary:            in.ScopeSummary,
		ExplicitConstraints:     append([]string{}, in.ExplicitConstraints...),
		DoneCriteria:            append([]string{}, in.DoneCriteria...),
		AmbiguityFlags:          append([]string{}, in.AmbiguityFlags...),
		ClarificationQuestions:  append([]string{}, in.ClarificationQuestions...),
		RequiresClarification:   in.RequiresClarification,
		BoundedEvidenceMessages: in.BoundedEvidenceMessages,
		ReadinessReason:         in.ReadinessReason,
		CompilationNotes:        in.CompilationNotes,
		Digest:                  in.Digest,
		Advisory:                in.Advisory,
	}
	if in.CreatedAtUnixMs > 0 {
		out.CreatedAt = time.UnixMilli(in.CreatedAtUnixMs).UTC()
	}
	return out
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
		TaskID:         string(raw.TaskID),
		Goal:           raw.Goal,
		Phase:          raw.Phase,
		Status:         raw.Status,
		IntentClass:    raw.IntentClass,
		IntentSummary:  raw.IntentSummary,
		CompiledIntent: compiledIntentSummaryFromIPC(raw.CompiledIntent),
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
			ID:                     string(raw.Brief.BriefID),
			Posture:                raw.Brief.Posture,
			Objective:              raw.Brief.Objective,
			RequestedOutcome:       raw.Brief.RequestedOutcome,
			NormalizedAction:       raw.Brief.NormalizedAction,
			ScopeSummary:           raw.Brief.ScopeSummary,
			Constraints:            append([]string{}, raw.Brief.Constraints...),
			DoneCriteria:           append([]string{}, raw.Brief.DoneCriteria...),
			AmbiguityFlags:         append([]string{}, raw.Brief.AmbiguityFlags...),
			ClarificationQuestions: append([]string{}, raw.Brief.ClarificationQuestions...),
			RequiresClarification:  raw.Brief.RequiresClarification,
			WorkerFraming:          raw.Brief.WorkerFraming,
			BoundedEvidenceMessages: raw.Brief.BoundedEvidenceMessages,
		}
	}
	if raw.Run != nil {
		out.Run = &RunSummary{
			ID:                 string(raw.Run.RunID),
			WorkerKind:         string(raw.Run.WorkerKind),
			Status:             string(raw.Run.Status),
			WorkerRunID:        raw.Run.WorkerRunID,
			ShellSessionID:     raw.Run.ShellSessionID,
			Command:            raw.Run.Command,
			Args:               append([]string{}, raw.Run.Args...),
			ExitCode:           raw.Run.ExitCode,
			Stdout:             raw.Run.Stdout,
			Stderr:             raw.Run.Stderr,
			ChangedFiles:       append([]string{}, raw.Run.ChangedFiles...),
			ValidationSignals:  append([]string{}, raw.Run.ValidationSignals...),
			OutputArtifactRef:  raw.Run.OutputArtifactRef,
			StructuredSummary:  raw.Run.StructuredSummary,
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
			ReceiptID:                        raw.LatestOperatorStepReceipt.ReceiptID,
			ActionHandle:                     raw.LatestOperatorStepReceipt.ActionHandle,
			ResultClass:                      raw.LatestOperatorStepReceipt.ResultClass,
			Summary:                          raw.LatestOperatorStepReceipt.Summary,
			Reason:                           raw.LatestOperatorStepReceipt.Reason,
			ReviewGapState:                   raw.LatestOperatorStepReceipt.ReviewGapState,
			ReviewGapSessionID:               raw.LatestOperatorStepReceipt.ReviewGapSessionID,
			ReviewGapClass:                   raw.LatestOperatorStepReceipt.ReviewGapClass,
			ReviewGapPresent:                 raw.LatestOperatorStepReceipt.ReviewGapPresent,
			ReviewGapReviewedUpTo:            raw.LatestOperatorStepReceipt.ReviewGapReviewedUpTo,
			ReviewGapOldestUnreviewed:        raw.LatestOperatorStepReceipt.ReviewGapOldestUnreviewed,
			ReviewGapNewestRetained:          raw.LatestOperatorStepReceipt.ReviewGapNewestRetained,
			ReviewGapUnreviewedRetainedCount: raw.LatestOperatorStepReceipt.ReviewGapUnreviewedRetainedCount,
			ReviewGapAcknowledged:            raw.LatestOperatorStepReceipt.ReviewGapAcknowledged,
			ReviewGapAcknowledgmentID:        string(raw.LatestOperatorStepReceipt.ReviewGapAcknowledgmentID),
			ReviewGapAcknowledgmentClass:     raw.LatestOperatorStepReceipt.ReviewGapAcknowledgmentClass,
			TransitionReceiptID:              string(raw.LatestOperatorStepReceipt.TransitionReceiptID),
			TransitionKind:                   raw.LatestOperatorStepReceipt.TransitionKind,
			CreatedAt:                        raw.LatestOperatorStepReceipt.CreatedAt,
		}
	}
	if len(raw.RecentOperatorStepReceipts) > 0 {
		out.RecentOperatorStepReceipts = make([]OperatorStepReceiptSummary, 0, len(raw.RecentOperatorStepReceipts))
		for _, item := range raw.RecentOperatorStepReceipts {
			out.RecentOperatorStepReceipts = append(out.RecentOperatorStepReceipts, OperatorStepReceiptSummary{
				ReceiptID:                        item.ReceiptID,
				ActionHandle:                     item.ActionHandle,
				ResultClass:                      item.ResultClass,
				Summary:                          item.Summary,
				Reason:                           item.Reason,
				ReviewGapState:                   item.ReviewGapState,
				ReviewGapSessionID:               item.ReviewGapSessionID,
				ReviewGapClass:                   item.ReviewGapClass,
				ReviewGapPresent:                 item.ReviewGapPresent,
				ReviewGapReviewedUpTo:            item.ReviewGapReviewedUpTo,
				ReviewGapOldestUnreviewed:        item.ReviewGapOldestUnreviewed,
				ReviewGapNewestRetained:          item.ReviewGapNewestRetained,
				ReviewGapUnreviewedRetainedCount: item.ReviewGapUnreviewedRetainedCount,
				ReviewGapAcknowledged:            item.ReviewGapAcknowledged,
				ReviewGapAcknowledgmentID:        string(item.ReviewGapAcknowledgmentID),
				ReviewGapAcknowledgmentClass:     item.ReviewGapAcknowledgmentClass,
				TransitionReceiptID:              string(item.TransitionReceiptID),
				TransitionKind:                   item.TransitionKind,
				CreatedAt:                        item.CreatedAt,
			})
		}
	}
	if raw.LatestContinuityTransitionReceipt != nil {
		out.LatestContinuityTransitionReceipt = &ContinuityTransitionReceiptSummary{
			ReceiptID:                string(raw.LatestContinuityTransitionReceipt.ReceiptID),
			TaskID:                   string(raw.LatestContinuityTransitionReceipt.TaskID),
			ShellSessionID:           raw.LatestContinuityTransitionReceipt.ShellSessionID,
			TransitionKind:           raw.LatestContinuityTransitionReceipt.TransitionKind,
			TransitionHandle:         raw.LatestContinuityTransitionReceipt.TransitionHandle,
			TriggerAction:            raw.LatestContinuityTransitionReceipt.TriggerAction,
			TriggerSource:            raw.LatestContinuityTransitionReceipt.TriggerSource,
			HandoffID:                raw.LatestContinuityTransitionReceipt.HandoffID,
			LaunchAttemptID:          raw.LatestContinuityTransitionReceipt.LaunchAttemptID,
			LaunchID:                 raw.LatestContinuityTransitionReceipt.LaunchID,
			ResolutionID:             raw.LatestContinuityTransitionReceipt.ResolutionID,
			BranchClassBefore:        raw.LatestContinuityTransitionReceipt.BranchClassBefore,
			BranchRefBefore:          raw.LatestContinuityTransitionReceipt.BranchRefBefore,
			BranchClassAfter:         raw.LatestContinuityTransitionReceipt.BranchClassAfter,
			BranchRefAfter:           raw.LatestContinuityTransitionReceipt.BranchRefAfter,
			HandoffStateBefore:       raw.LatestContinuityTransitionReceipt.HandoffStateBefore,
			HandoffStateAfter:        raw.LatestContinuityTransitionReceipt.HandoffStateAfter,
			LaunchControlBefore:      raw.LatestContinuityTransitionReceipt.LaunchControlBefore,
			LaunchControlAfter:       raw.LatestContinuityTransitionReceipt.LaunchControlAfter,
			ReviewGapPresent:         raw.LatestContinuityTransitionReceipt.ReviewGapPresent,
			ReviewPosture:            raw.LatestContinuityTransitionReceipt.ReviewPosture,
			ReviewState:              raw.LatestContinuityTransitionReceipt.ReviewState,
			ReviewScope:              raw.LatestContinuityTransitionReceipt.ReviewScope,
			ReviewedUpToSequence:     raw.LatestContinuityTransitionReceipt.ReviewedUpToSequence,
			OldestUnreviewedSequence: raw.LatestContinuityTransitionReceipt.OldestUnreviewedSequence,
			NewestRetainedSequence:   raw.LatestContinuityTransitionReceipt.NewestRetainedSequence,
			UnreviewedRetainedCount:  raw.LatestContinuityTransitionReceipt.UnreviewedRetainedCount,
			LatestReviewID:           string(raw.LatestContinuityTransitionReceipt.LatestReviewID),
			LatestReviewGapAckID:     string(raw.LatestContinuityTransitionReceipt.LatestReviewGapAckID),
			AcknowledgmentPresent:    raw.LatestContinuityTransitionReceipt.AcknowledgmentPresent,
			AcknowledgmentClass:      raw.LatestContinuityTransitionReceipt.AcknowledgmentClass,
			Summary:                  raw.LatestContinuityTransitionReceipt.Summary,
			CreatedAt:                raw.LatestContinuityTransitionReceipt.CreatedAt,
		}
	}
	if len(raw.RecentContinuityTransitionReceipts) > 0 {
		out.RecentContinuityTransitionReceipts = make([]ContinuityTransitionReceiptSummary, 0, len(raw.RecentContinuityTransitionReceipts))
		for _, item := range raw.RecentContinuityTransitionReceipts {
			out.RecentContinuityTransitionReceipts = append(out.RecentContinuityTransitionReceipts, ContinuityTransitionReceiptSummary{
				ReceiptID:                string(item.ReceiptID),
				TaskID:                   string(item.TaskID),
				ShellSessionID:           item.ShellSessionID,
				TransitionKind:           item.TransitionKind,
				TransitionHandle:         item.TransitionHandle,
				TriggerAction:            item.TriggerAction,
				TriggerSource:            item.TriggerSource,
				HandoffID:                item.HandoffID,
				LaunchAttemptID:          item.LaunchAttemptID,
				LaunchID:                 item.LaunchID,
				ResolutionID:             item.ResolutionID,
				BranchClassBefore:        item.BranchClassBefore,
				BranchRefBefore:          item.BranchRefBefore,
				BranchClassAfter:         item.BranchClassAfter,
				BranchRefAfter:           item.BranchRefAfter,
				HandoffStateBefore:       item.HandoffStateBefore,
				HandoffStateAfter:        item.HandoffStateAfter,
				LaunchControlBefore:      item.LaunchControlBefore,
				LaunchControlAfter:       item.LaunchControlAfter,
				ReviewGapPresent:         item.ReviewGapPresent,
				ReviewPosture:            item.ReviewPosture,
				ReviewState:              item.ReviewState,
				ReviewScope:              item.ReviewScope,
				ReviewedUpToSequence:     item.ReviewedUpToSequence,
				OldestUnreviewedSequence: item.OldestUnreviewedSequence,
				NewestRetainedSequence:   item.NewestRetainedSequence,
				UnreviewedRetainedCount:  item.UnreviewedRetainedCount,
				LatestReviewID:           string(item.LatestReviewID),
				LatestReviewGapAckID:     string(item.LatestReviewGapAckID),
				AcknowledgmentPresent:    item.AcknowledgmentPresent,
				AcknowledgmentClass:      item.AcknowledgmentClass,
				Summary:                  item.Summary,
				CreatedAt:                item.CreatedAt,
			})
		}
	}
	if raw.ContinuityTransitionRiskSummary != nil {
		out.ContinuityTransitionRiskSummary = &ContinuityTransitionRiskSummary{
			WindowSize:                           raw.ContinuityTransitionRiskSummary.WindowSize,
			ReviewGapTransitions:                 raw.ContinuityTransitionRiskSummary.ReviewGapTransitions,
			AcknowledgedReviewGapTransitions:     raw.ContinuityTransitionRiskSummary.AcknowledgedReviewGapTransitions,
			UnacknowledgedReviewGapTransitions:   raw.ContinuityTransitionRiskSummary.UnacknowledgedReviewGapTransitions,
			StaleReviewPostureTransitions:        raw.ContinuityTransitionRiskSummary.StaleReviewPostureTransitions,
			SourceScopedReviewPostureTransitions: raw.ContinuityTransitionRiskSummary.SourceScopedReviewPostureTransitions,
			IntoClaudeOwnershipTransitions:       raw.ContinuityTransitionRiskSummary.IntoClaudeOwnershipTransitions,
			BackToLocalOwnershipTransitions:      raw.ContinuityTransitionRiskSummary.BackToLocalOwnershipTransitions,
			OperationallyNotable:                 raw.ContinuityTransitionRiskSummary.OperationallyNotable,
			Summary:                              raw.ContinuityTransitionRiskSummary.Summary,
		}
	}
	if raw.ContinuityIncidentSummary != nil {
		out.ContinuityIncidentSummary = &ContinuityIncidentRiskSummary{
			ReviewGapPresent:                raw.ContinuityIncidentSummary.ReviewGapPresent,
			AcknowledgmentPresent:           raw.ContinuityIncidentSummary.AcknowledgmentPresent,
			StaleOrUnreviewedReviewPosture:  raw.ContinuityIncidentSummary.StaleOrUnreviewedReviewPosture,
			SourceScopedReviewPosture:       raw.ContinuityIncidentSummary.SourceScopedReviewPosture,
			IntoClaudeOwnershipTransition:   raw.ContinuityIncidentSummary.IntoClaudeOwnershipTransition,
			BackToLocalOwnershipTransition:  raw.ContinuityIncidentSummary.BackToLocalOwnershipTransition,
			UnresolvedContinuityAmbiguity:   raw.ContinuityIncidentSummary.UnresolvedContinuityAmbiguity,
			NearbyFailedOrInterruptedRuns:   raw.ContinuityIncidentSummary.NearbyFailedOrInterruptedRuns,
			NearbyRecoveryActions:           raw.ContinuityIncidentSummary.NearbyRecoveryActions,
			RecentFailureOrRecoveryActivity: raw.ContinuityIncidentSummary.RecentFailureOrRecoveryActivity,
			OperationallyNotable:            raw.ContinuityIncidentSummary.OperationallyNotable,
			Summary:                         raw.ContinuityIncidentSummary.Summary,
		}
	}
	if raw.LatestContinuityIncidentTriageReceipt != nil {
		out.LatestContinuityIncidentTriageReceipt = &ContinuityIncidentTriageReceiptSummary{
			ReceiptID:                 string(raw.LatestContinuityIncidentTriageReceipt.ReceiptID),
			TaskID:                    string(raw.LatestContinuityIncidentTriageReceipt.TaskID),
			AnchorMode:                raw.LatestContinuityIncidentTriageReceipt.AnchorMode,
			AnchorTransitionReceiptID: string(raw.LatestContinuityIncidentTriageReceipt.AnchorTransitionReceiptID),
			AnchorTransitionKind:      raw.LatestContinuityIncidentTriageReceipt.AnchorTransitionKind,
			AnchorHandoffID:           raw.LatestContinuityIncidentTriageReceipt.AnchorHandoffID,
			AnchorShellSessionID:      raw.LatestContinuityIncidentTriageReceipt.AnchorShellSessionID,
			Posture:                   raw.LatestContinuityIncidentTriageReceipt.Posture,
			FollowUpPosture:           raw.LatestContinuityIncidentTriageReceipt.FollowUpPosture,
			Summary:                   raw.LatestContinuityIncidentTriageReceipt.Summary,
			ReviewGapPresent:          raw.LatestContinuityIncidentTriageReceipt.ReviewGapPresent,
			ReviewPosture:             raw.LatestContinuityIncidentTriageReceipt.ReviewPosture,
			ReviewState:               raw.LatestContinuityIncidentTriageReceipt.ReviewState,
			ReviewScope:               raw.LatestContinuityIncidentTriageReceipt.ReviewScope,
			ReviewedUpToSequence:      raw.LatestContinuityIncidentTriageReceipt.ReviewedUpToSequence,
			OldestUnreviewedSequence:  raw.LatestContinuityIncidentTriageReceipt.OldestUnreviewedSequence,
			NewestRetainedSequence:    raw.LatestContinuityIncidentTriageReceipt.NewestRetainedSequence,
			UnreviewedRetainedCount:   raw.LatestContinuityIncidentTriageReceipt.UnreviewedRetainedCount,
			LatestReviewID:            string(raw.LatestContinuityIncidentTriageReceipt.LatestReviewID),
			LatestReviewGapAckID:      string(raw.LatestContinuityIncidentTriageReceipt.LatestReviewGapAckID),
			AcknowledgmentPresent:     raw.LatestContinuityIncidentTriageReceipt.AcknowledgmentPresent,
			AcknowledgmentClass:       raw.LatestContinuityIncidentTriageReceipt.AcknowledgmentClass,
			RiskSummary: ContinuityIncidentRiskSummary{
				ReviewGapPresent:                raw.LatestContinuityIncidentTriageReceipt.RiskSummary.ReviewGapPresent,
				AcknowledgmentPresent:           raw.LatestContinuityIncidentTriageReceipt.RiskSummary.AcknowledgmentPresent,
				StaleOrUnreviewedReviewPosture:  raw.LatestContinuityIncidentTriageReceipt.RiskSummary.StaleOrUnreviewedReviewPosture,
				SourceScopedReviewPosture:       raw.LatestContinuityIncidentTriageReceipt.RiskSummary.SourceScopedReviewPosture,
				IntoClaudeOwnershipTransition:   raw.LatestContinuityIncidentTriageReceipt.RiskSummary.IntoClaudeOwnershipTransition,
				BackToLocalOwnershipTransition:  raw.LatestContinuityIncidentTriageReceipt.RiskSummary.BackToLocalOwnershipTransition,
				UnresolvedContinuityAmbiguity:   raw.LatestContinuityIncidentTriageReceipt.RiskSummary.UnresolvedContinuityAmbiguity,
				NearbyFailedOrInterruptedRuns:   raw.LatestContinuityIncidentTriageReceipt.RiskSummary.NearbyFailedOrInterruptedRuns,
				NearbyRecoveryActions:           raw.LatestContinuityIncidentTriageReceipt.RiskSummary.NearbyRecoveryActions,
				RecentFailureOrRecoveryActivity: raw.LatestContinuityIncidentTriageReceipt.RiskSummary.RecentFailureOrRecoveryActivity,
				OperationallyNotable:            raw.LatestContinuityIncidentTriageReceipt.RiskSummary.OperationallyNotable,
				Summary:                         raw.LatestContinuityIncidentTriageReceipt.RiskSummary.Summary,
			},
			CreatedAt: raw.LatestContinuityIncidentTriageReceipt.CreatedAt,
		}
	}
	if len(raw.RecentContinuityIncidentTriageReceipts) > 0 {
		out.RecentContinuityIncidentTriageReceipts = make([]ContinuityIncidentTriageReceiptSummary, 0, len(raw.RecentContinuityIncidentTriageReceipts))
		for _, item := range raw.RecentContinuityIncidentTriageReceipts {
			out.RecentContinuityIncidentTriageReceipts = append(out.RecentContinuityIncidentTriageReceipts, ContinuityIncidentTriageReceiptSummary{
				ReceiptID:                 string(item.ReceiptID),
				TaskID:                    string(item.TaskID),
				AnchorMode:                item.AnchorMode,
				AnchorTransitionReceiptID: string(item.AnchorTransitionReceiptID),
				AnchorTransitionKind:      item.AnchorTransitionKind,
				AnchorHandoffID:           item.AnchorHandoffID,
				AnchorShellSessionID:      item.AnchorShellSessionID,
				Posture:                   item.Posture,
				FollowUpPosture:           item.FollowUpPosture,
				Summary:                   item.Summary,
				ReviewGapPresent:          item.ReviewGapPresent,
				ReviewPosture:             item.ReviewPosture,
				ReviewState:               item.ReviewState,
				ReviewScope:               item.ReviewScope,
				ReviewedUpToSequence:      item.ReviewedUpToSequence,
				OldestUnreviewedSequence:  item.OldestUnreviewedSequence,
				NewestRetainedSequence:    item.NewestRetainedSequence,
				UnreviewedRetainedCount:   item.UnreviewedRetainedCount,
				LatestReviewID:            string(item.LatestReviewID),
				LatestReviewGapAckID:      string(item.LatestReviewGapAckID),
				AcknowledgmentPresent:     item.AcknowledgmentPresent,
				AcknowledgmentClass:       item.AcknowledgmentClass,
				RiskSummary: ContinuityIncidentRiskSummary{
					ReviewGapPresent:                item.RiskSummary.ReviewGapPresent,
					AcknowledgmentPresent:           item.RiskSummary.AcknowledgmentPresent,
					StaleOrUnreviewedReviewPosture:  item.RiskSummary.StaleOrUnreviewedReviewPosture,
					SourceScopedReviewPosture:       item.RiskSummary.SourceScopedReviewPosture,
					IntoClaudeOwnershipTransition:   item.RiskSummary.IntoClaudeOwnershipTransition,
					BackToLocalOwnershipTransition:  item.RiskSummary.BackToLocalOwnershipTransition,
					UnresolvedContinuityAmbiguity:   item.RiskSummary.UnresolvedContinuityAmbiguity,
					NearbyFailedOrInterruptedRuns:   item.RiskSummary.NearbyFailedOrInterruptedRuns,
					NearbyRecoveryActions:           item.RiskSummary.NearbyRecoveryActions,
					RecentFailureOrRecoveryActivity: item.RiskSummary.RecentFailureOrRecoveryActivity,
					OperationallyNotable:            item.RiskSummary.OperationallyNotable,
					Summary:                         item.RiskSummary.Summary,
				},
				CreatedAt: item.CreatedAt,
			})
		}
	}
	if raw.ContinuityIncidentFollowUp != nil {
		out.ContinuityIncidentFollowUp = &ContinuityIncidentFollowUpSummary{
			State:                     raw.ContinuityIncidentFollowUp.State,
			Digest:                    raw.ContinuityIncidentFollowUp.Digest,
			WindowAdvisory:            raw.ContinuityIncidentFollowUp.WindowAdvisory,
			Advisory:                  raw.ContinuityIncidentFollowUp.Advisory,
			FollowUpAdvised:           raw.ContinuityIncidentFollowUp.FollowUpAdvised,
			NeedsFollowUp:             raw.ContinuityIncidentFollowUp.NeedsFollowUp,
			Deferred:                  raw.ContinuityIncidentFollowUp.Deferred,
			TriageBehindLatest:        raw.ContinuityIncidentFollowUp.TriageBehindLatest,
			TriagedUnderReviewRisk:    raw.ContinuityIncidentFollowUp.TriagedUnderReviewRisk,
			LatestTransitionReceiptID: string(raw.ContinuityIncidentFollowUp.LatestTransitionReceiptID),
			LatestTriageReceiptID:     string(raw.ContinuityIncidentFollowUp.LatestTriageReceiptID),
			TriageAnchorReceiptID:     string(raw.ContinuityIncidentFollowUp.TriageAnchorReceiptID),
			TriagePosture:             raw.ContinuityIncidentFollowUp.TriagePosture,
			LatestFollowUpReceiptID:   string(raw.ContinuityIncidentFollowUp.LatestFollowUpReceiptID),
			LatestFollowUpActionKind:  raw.ContinuityIncidentFollowUp.LatestFollowUpActionKind,
			LatestFollowUpSummary:     raw.ContinuityIncidentFollowUp.LatestFollowUpSummary,
			LatestFollowUpAt:          raw.ContinuityIncidentFollowUp.LatestFollowUpAt,
			FollowUpReceiptPresent:    raw.ContinuityIncidentFollowUp.FollowUpReceiptPresent,
			FollowUpOpen:              raw.ContinuityIncidentFollowUp.FollowUpOpen,
			FollowUpClosed:            raw.ContinuityIncidentFollowUp.FollowUpClosed,
			FollowUpReopened:          raw.ContinuityIncidentFollowUp.FollowUpReopened,
			FollowUpProgressed:        raw.ContinuityIncidentFollowUp.FollowUpProgressed,
			ClosureIntelligence:       continuityIncidentClosureSummaryFromIPC(raw.ContinuityIncidentFollowUp.ClosureIntelligence),
		}
	}
	out.ContinuityIncidentTaskRisk = continuityIncidentTaskRiskSummaryFromIPC(raw.ContinuityIncidentTaskRisk)
	if raw.ContinuityIncidentTriageHistoryRollup != nil {
		out.ContinuityIncidentTriageHistoryRollup = &ContinuityIncidentTriageHistoryRollupSummary{
			WindowSize:                        raw.ContinuityIncidentTriageHistoryRollup.WindowSize,
			BoundedWindow:                     raw.ContinuityIncidentTriageHistoryRollup.BoundedWindow,
			DistinctAnchors:                   raw.ContinuityIncidentTriageHistoryRollup.DistinctAnchors,
			AnchorsTriagedCurrent:             raw.ContinuityIncidentTriageHistoryRollup.AnchorsTriagedCurrent,
			AnchorsNeedsFollowUp:              raw.ContinuityIncidentTriageHistoryRollup.AnchorsNeedsFollowUp,
			AnchorsDeferred:                   raw.ContinuityIncidentTriageHistoryRollup.AnchorsDeferred,
			AnchorsBehindLatestTransition:     raw.ContinuityIncidentTriageHistoryRollup.AnchorsBehindLatestTransition,
			AnchorsWithOpenFollowUp:           raw.ContinuityIncidentTriageHistoryRollup.AnchorsWithOpenFollowUp,
			AnchorsRepeatedWithoutProgression: raw.ContinuityIncidentTriageHistoryRollup.AnchorsRepeatedWithoutProgression,
			ReviewRiskReceipts:                raw.ContinuityIncidentTriageHistoryRollup.ReviewRiskReceipts,
			AcknowledgedReviewGapReceipts:     raw.ContinuityIncidentTriageHistoryRollup.AcknowledgedReviewGapReceipts,
			OperationallyNotable:              raw.ContinuityIncidentTriageHistoryRollup.OperationallyNotable,
			Summary:                           raw.ContinuityIncidentTriageHistoryRollup.Summary,
		}
	}
	if raw.LatestContinuityIncidentFollowUpReceipt != nil {
		out.LatestContinuityIncidentFollowUpReceipt = &ContinuityIncidentFollowUpReceiptSummary{
			ReceiptID:                 string(raw.LatestContinuityIncidentFollowUpReceipt.ReceiptID),
			TaskID:                    string(raw.LatestContinuityIncidentFollowUpReceipt.TaskID),
			AnchorMode:                raw.LatestContinuityIncidentFollowUpReceipt.AnchorMode,
			AnchorTransitionReceiptID: string(raw.LatestContinuityIncidentFollowUpReceipt.AnchorTransitionReceiptID),
			AnchorTransitionKind:      raw.LatestContinuityIncidentFollowUpReceipt.AnchorTransitionKind,
			AnchorHandoffID:           raw.LatestContinuityIncidentFollowUpReceipt.AnchorHandoffID,
			AnchorShellSessionID:      raw.LatestContinuityIncidentFollowUpReceipt.AnchorShellSessionID,
			TriageReceiptID:           string(raw.LatestContinuityIncidentFollowUpReceipt.TriageReceiptID),
			TriagePosture:             raw.LatestContinuityIncidentFollowUpReceipt.TriagePosture,
			TriageFollowUpPosture:     raw.LatestContinuityIncidentFollowUpReceipt.TriageFollowUpPosture,
			ActionKind:                raw.LatestContinuityIncidentFollowUpReceipt.ActionKind,
			Summary:                   raw.LatestContinuityIncidentFollowUpReceipt.Summary,
			ReviewGapPresent:          raw.LatestContinuityIncidentFollowUpReceipt.ReviewGapPresent,
			ReviewPosture:             raw.LatestContinuityIncidentFollowUpReceipt.ReviewPosture,
			ReviewState:               raw.LatestContinuityIncidentFollowUpReceipt.ReviewState,
			ReviewScope:               raw.LatestContinuityIncidentFollowUpReceipt.ReviewScope,
			ReviewedUpToSequence:      raw.LatestContinuityIncidentFollowUpReceipt.ReviewedUpToSequence,
			OldestUnreviewedSequence:  raw.LatestContinuityIncidentFollowUpReceipt.OldestUnreviewedSequence,
			NewestRetainedSequence:    raw.LatestContinuityIncidentFollowUpReceipt.NewestRetainedSequence,
			UnreviewedRetainedCount:   raw.LatestContinuityIncidentFollowUpReceipt.UnreviewedRetainedCount,
			LatestReviewID:            string(raw.LatestContinuityIncidentFollowUpReceipt.LatestReviewID),
			LatestReviewGapAckID:      string(raw.LatestContinuityIncidentFollowUpReceipt.LatestReviewGapAckID),
			AcknowledgmentPresent:     raw.LatestContinuityIncidentFollowUpReceipt.AcknowledgmentPresent,
			AcknowledgmentClass:       raw.LatestContinuityIncidentFollowUpReceipt.AcknowledgmentClass,
			TriagedUnderReviewRisk:    raw.LatestContinuityIncidentFollowUpReceipt.TriagedUnderReviewRisk,
			CreatedAt:                 raw.LatestContinuityIncidentFollowUpReceipt.CreatedAt,
		}
	}
	if len(raw.RecentContinuityIncidentFollowUpReceipts) > 0 {
		out.RecentContinuityIncidentFollowUpReceipts = make([]ContinuityIncidentFollowUpReceiptSummary, 0, len(raw.RecentContinuityIncidentFollowUpReceipts))
		for _, item := range raw.RecentContinuityIncidentFollowUpReceipts {
			out.RecentContinuityIncidentFollowUpReceipts = append(out.RecentContinuityIncidentFollowUpReceipts, ContinuityIncidentFollowUpReceiptSummary{
				ReceiptID:                 string(item.ReceiptID),
				TaskID:                    string(item.TaskID),
				AnchorMode:                item.AnchorMode,
				AnchorTransitionReceiptID: string(item.AnchorTransitionReceiptID),
				AnchorTransitionKind:      item.AnchorTransitionKind,
				AnchorHandoffID:           item.AnchorHandoffID,
				AnchorShellSessionID:      item.AnchorShellSessionID,
				TriageReceiptID:           string(item.TriageReceiptID),
				TriagePosture:             item.TriagePosture,
				TriageFollowUpPosture:     item.TriageFollowUpPosture,
				ActionKind:                item.ActionKind,
				Summary:                   item.Summary,
				ReviewGapPresent:          item.ReviewGapPresent,
				ReviewPosture:             item.ReviewPosture,
				ReviewState:               item.ReviewState,
				ReviewScope:               item.ReviewScope,
				ReviewedUpToSequence:      item.ReviewedUpToSequence,
				OldestUnreviewedSequence:  item.OldestUnreviewedSequence,
				NewestRetainedSequence:    item.NewestRetainedSequence,
				UnreviewedRetainedCount:   item.UnreviewedRetainedCount,
				LatestReviewID:            string(item.LatestReviewID),
				LatestReviewGapAckID:      string(item.LatestReviewGapAckID),
				AcknowledgmentPresent:     item.AcknowledgmentPresent,
				AcknowledgmentClass:       item.AcknowledgmentClass,
				TriagedUnderReviewRisk:    item.TriagedUnderReviewRisk,
				CreatedAt:                 item.CreatedAt,
			})
		}
	}
	if raw.ContinuityIncidentFollowUpHistoryRollup != nil {
		out.ContinuityIncidentFollowUpHistoryRollup = &ContinuityIncidentFollowUpHistoryRollupSummary{
			WindowSize:                        raw.ContinuityIncidentFollowUpHistoryRollup.WindowSize,
			BoundedWindow:                     raw.ContinuityIncidentFollowUpHistoryRollup.BoundedWindow,
			DistinctAnchors:                   raw.ContinuityIncidentFollowUpHistoryRollup.DistinctAnchors,
			ReceiptsRecordedPending:           raw.ContinuityIncidentFollowUpHistoryRollup.ReceiptsRecordedPending,
			ReceiptsProgressed:                raw.ContinuityIncidentFollowUpHistoryRollup.ReceiptsProgressed,
			ReceiptsClosed:                    raw.ContinuityIncidentFollowUpHistoryRollup.ReceiptsClosed,
			ReceiptsReopened:                  raw.ContinuityIncidentFollowUpHistoryRollup.ReceiptsReopened,
			AnchorsWithOpenFollowUp:           raw.ContinuityIncidentFollowUpHistoryRollup.AnchorsWithOpenFollowUp,
			AnchorsClosed:                     raw.ContinuityIncidentFollowUpHistoryRollup.AnchorsClosed,
			AnchorsReopened:                   raw.ContinuityIncidentFollowUpHistoryRollup.AnchorsReopened,
			OpenAnchorsBehindLatestTransition: raw.ContinuityIncidentFollowUpHistoryRollup.OpenAnchorsBehindLatestTransition,
			AnchorsRepeatedWithoutProgression: raw.ContinuityIncidentFollowUpHistoryRollup.AnchorsRepeatedWithoutProgression,
			AnchorsTriagedWithoutFollowUp:     raw.ContinuityIncidentFollowUpHistoryRollup.AnchorsTriagedWithoutFollowUp,
			OperationallyNotable:              raw.ContinuityIncidentFollowUpHistoryRollup.OperationallyNotable,
			Summary:                           raw.ContinuityIncidentFollowUpHistoryRollup.Summary,
		}
	}
	if raw.LatestTranscriptReviewGapAcknowledgment != nil {
		out.LatestTranscriptReviewGapAcknowledgment = &TranscriptReviewGapAcknowledgment{
			AcknowledgmentID:         string(raw.LatestTranscriptReviewGapAcknowledgment.AcknowledgmentID),
			TaskID:                   string(raw.LatestTranscriptReviewGapAcknowledgment.TaskID),
			SessionID:                raw.LatestTranscriptReviewGapAcknowledgment.SessionID,
			Class:                    raw.LatestTranscriptReviewGapAcknowledgment.Class,
			ReviewState:              raw.LatestTranscriptReviewGapAcknowledgment.ReviewState,
			ReviewScope:              raw.LatestTranscriptReviewGapAcknowledgment.ReviewScope,
			ReviewedUpToSequence:     raw.LatestTranscriptReviewGapAcknowledgment.ReviewedUpToSequence,
			OldestUnreviewedSequence: raw.LatestTranscriptReviewGapAcknowledgment.OldestUnreviewedSequence,
			NewestRetainedSequence:   raw.LatestTranscriptReviewGapAcknowledgment.NewestRetainedSequence,
			UnreviewedRetainedCount:  raw.LatestTranscriptReviewGapAcknowledgment.UnreviewedRetainedCount,
			TranscriptState:          raw.LatestTranscriptReviewGapAcknowledgment.TranscriptState,
			RetentionLimit:           raw.LatestTranscriptReviewGapAcknowledgment.RetentionLimit,
			RetainedChunks:           raw.LatestTranscriptReviewGapAcknowledgment.RetainedChunks,
			DroppedChunks:            raw.LatestTranscriptReviewGapAcknowledgment.DroppedChunks,
			ActionContext:            raw.LatestTranscriptReviewGapAcknowledgment.ActionContext,
			Summary:                  raw.LatestTranscriptReviewGapAcknowledgment.Summary,
			CreatedAt:                raw.LatestTranscriptReviewGapAcknowledgment.CreatedAt,
			StaleBehindCurrent:       raw.LatestTranscriptReviewGapAcknowledgment.StaleBehindCurrent,
			NewerRetainedCount:       raw.LatestTranscriptReviewGapAcknowledgment.NewerRetainedCount,
		}
	}
	if len(raw.RecentTranscriptReviewGapAcknowledgments) > 0 {
		out.RecentTranscriptReviewGapAcknowledgments = make([]TranscriptReviewGapAcknowledgment, 0, len(raw.RecentTranscriptReviewGapAcknowledgments))
		for _, item := range raw.RecentTranscriptReviewGapAcknowledgments {
			out.RecentTranscriptReviewGapAcknowledgments = append(out.RecentTranscriptReviewGapAcknowledgments, TranscriptReviewGapAcknowledgment{
				AcknowledgmentID:         string(item.AcknowledgmentID),
				TaskID:                   string(item.TaskID),
				SessionID:                item.SessionID,
				Class:                    item.Class,
				ReviewState:              item.ReviewState,
				ReviewScope:              item.ReviewScope,
				ReviewedUpToSequence:     item.ReviewedUpToSequence,
				OldestUnreviewedSequence: item.OldestUnreviewedSequence,
				NewestRetainedSequence:   item.NewestRetainedSequence,
				UnreviewedRetainedCount:  item.UnreviewedRetainedCount,
				TranscriptState:          item.TranscriptState,
				RetentionLimit:           item.RetentionLimit,
				RetainedChunks:           item.RetainedChunks,
				DroppedChunks:            item.DroppedChunks,
				ActionContext:            item.ActionContext,
				Summary:                  item.Summary,
				CreatedAt:                item.CreatedAt,
				StaleBehindCurrent:       item.StaleBehindCurrent,
				NewerRetainedCount:       item.NewerRetainedCount,
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
	if len(raw.ShellSessions) > 0 {
		out.ShellSessions = make([]KnownShellSession, 0, len(raw.ShellSessions))
		for _, session := range raw.ShellSessions {
			mapped := KnownShellSession{
				SessionID:                        session.SessionID,
				TaskID:                           string(session.TaskID),
				WorkerPreference:                 WorkerPreference(session.WorkerPreference),
				ResolvedWorker:                   WorkerPreference(session.ResolvedWorker),
				WorkerSessionID:                  session.WorkerSessionID,
				WorkerSessionIDSource:            normalizeWorkerSessionIDSource(WorkerSessionIDSource(session.WorkerSessionIDSource), session.WorkerSessionID),
				AttachCapability:                 WorkerAttachCapability(session.AttachCapability),
				HostMode:                         HostMode(session.HostMode),
				HostState:                        HostState(session.HostState),
				SessionClass:                     KnownShellSessionClass(session.SessionClass),
				SessionClassReason:               session.SessionClassReason,
				ReattachGuidance:                 session.ReattachGuidance,
				OperatorSummary:                  session.OperatorSummary,
				TranscriptState:                  session.TranscriptState,
				TranscriptRetainedChunks:         session.TranscriptRetainedChunks,
				TranscriptDroppedChunks:          session.TranscriptDroppedChunks,
				TranscriptRetentionLimit:         session.TranscriptRetentionLimit,
				TranscriptOldestSequence:         session.TranscriptOldestSequence,
				TranscriptNewestSequence:         session.TranscriptNewestSequence,
				TranscriptLastChunkAt:            session.TranscriptLastChunkAt,
				TranscriptReviewID:               string(session.TranscriptReviewID),
				TranscriptReviewSource:           session.TranscriptReviewSource,
				TranscriptReviewedUpTo:           session.TranscriptReviewedUpTo,
				TranscriptReviewSummary:          session.TranscriptReviewSummary,
				TranscriptReviewAt:               session.TranscriptReviewAt,
				TranscriptReviewStale:            session.TranscriptReviewStale,
				TranscriptReviewNewer:            session.TranscriptReviewNewer,
				TranscriptReviewClosureState:     session.TranscriptReviewClosureState,
				TranscriptReviewOldestUnreviewed: session.TranscriptReviewOldestUnreviewed,
				StartedAt:                        session.StartedAt,
				LastUpdatedAt:                    session.LastUpdatedAt,
				Active:                           session.Active,
				Note:                             session.Note,
				LatestEventID:                    string(session.LatestEventID),
				LatestEventKind:                  session.LatestEventKind,
				LatestEventAt:                    session.LatestEventAt,
				LatestEventNote:                  session.LatestEventNote,
			}
			if len(session.TranscriptRecentReviews) > 0 {
				mapped.TranscriptRecentReviews = make([]TranscriptReviewMarker, 0, len(session.TranscriptRecentReviews))
				for _, review := range session.TranscriptRecentReviews {
					mapped.TranscriptRecentReviews = append(mapped.TranscriptRecentReviews, TranscriptReviewMarker{
						ReviewID:                 string(review.ReviewID),
						SourceFilter:             review.SourceFilter,
						ReviewedUpToSequence:     review.ReviewedUpToSequence,
						Summary:                  review.Summary,
						CreatedAt:                review.CreatedAt,
						TranscriptState:          review.TranscriptState,
						RetentionLimit:           review.RetentionLimit,
						RetainedChunks:           review.RetainedChunks,
						DroppedChunks:            review.DroppedChunks,
						OldestRetainedSequence:   review.OldestRetainedSequence,
						NewestRetainedSequence:   review.NewestRetainedSequence,
						StaleBehindLatest:        review.StaleBehindLatest,
						NewerRetainedCount:       review.NewerRetainedCount,
						OldestUnreviewedSequence: review.OldestUnreviewedSequence,
						ClosureState:             review.ClosureState,
					})
				}
			}
			out.ShellSessions = append(out.ShellSessions, mapped)
		}
	}
	if len(raw.RecentShellEvents) > 0 {
		out.RecentShellEvents = make([]ShellSessionEventSummary, 0, len(raw.RecentShellEvents))
		for _, event := range raw.RecentShellEvents {
			out.RecentShellEvents = append(out.RecentShellEvents, ShellSessionEventSummary{
				EventID:               string(event.EventID),
				TaskID:                string(event.TaskID),
				SessionID:             event.SessionID,
				Kind:                  event.Kind,
				HostMode:              event.HostMode,
				HostState:             event.HostState,
				WorkerSessionID:       event.WorkerSessionID,
				WorkerSessionIDSource: event.WorkerSessionIDSource,
				AttachCapability:      event.AttachCapability,
				Active:                event.Active,
				InputLive:             event.InputLive,
				ExitCode:              event.ExitCode,
				PaneWidth:             event.PaneWidth,
				PaneHeight:            event.PaneHeight,
				Note:                  event.Note,
				CreatedAt:             event.CreatedAt,
			})
		}
	}
	if len(raw.RecentShellTranscript) > 0 {
		out.RecentShellTranscript = make([]ShellTranscriptChunkSummary, 0, len(raw.RecentShellTranscript))
		for _, chunk := range raw.RecentShellTranscript {
			out.RecentShellTranscript = append(out.RecentShellTranscript, ShellTranscriptChunkSummary{
				ChunkID:    string(chunk.ChunkID),
				TaskID:     string(chunk.TaskID),
				SessionID:  chunk.SessionID,
				SequenceNo: chunk.SequenceNo,
				Source:     chunk.Source,
				Content:    chunk.Content,
				CreatedAt:  chunk.CreatedAt,
			})
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
