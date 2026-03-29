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

func ipcCompiledIntentSummary(in *orchestrator.CompiledIntentSummary) *ipc.TaskCompiledIntentSummary {
	if in == nil {
		return nil
	}
	createdAt := int64(0)
	if !in.CreatedAt.IsZero() {
		createdAt = in.CreatedAt.UnixMilli()
	}
	return &ipc.TaskCompiledIntentSummary{
		IntentID:                in.IntentID,
		Class:                   string(in.Class),
		Posture:                 string(in.Posture),
		ExecutionReadiness:      string(in.ExecutionReadiness),
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
		CreatedAtUnixMs:         createdAt,
	}
}

func ipcCompiledBriefSummary(in *orchestrator.CompiledBriefSummary) *ipc.TaskCompiledBriefSummary {
	if in == nil {
		return nil
	}
	createdAt := int64(0)
	if !in.CreatedAt.IsZero() {
		createdAt = in.CreatedAt.UnixMilli()
	}
	return &ipc.TaskCompiledBriefSummary{
		BriefID:                 in.BriefID,
		IntentID:                in.IntentID,
		Posture:                 string(in.Posture),
		Objective:               in.Objective,
		RequestedOutcome:        in.RequestedOutcome,
		NormalizedAction:        in.NormalizedAction,
		ScopeSummary:            in.ScopeSummary,
		Constraints:             append([]string{}, in.Constraints...),
		DoneCriteria:            append([]string{}, in.DoneCriteria...),
		AmbiguityFlags:          append([]string{}, in.AmbiguityFlags...),
		ClarificationQuestions:  append([]string{}, in.ClarificationQuestions...),
		RequiresClarification:   in.RequiresClarification,
		WorkerFraming:           in.WorkerFraming,
		BoundedEvidenceMessages: in.BoundedEvidenceMessages,
		Digest:                  in.Digest,
		Advisory:                in.Advisory,
		CreatedAtUnixMs:         createdAt,
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

func ipcTranscriptReviewMarker(in orchestrator.ShellTranscriptReviewSummary) ipc.TaskShellTranscriptReviewMarker {
	return ipc.TaskShellTranscriptReviewMarker{
		ReviewID:                 in.ReviewID,
		SourceFilter:             string(in.SourceFilter),
		ReviewedUpToSequence:     in.ReviewedUpToSequence,
		Summary:                  in.Summary,
		CreatedAt:                in.CreatedAt,
		TranscriptState:          string(in.TranscriptState),
		RetentionLimit:           in.RetentionLimit,
		RetainedChunks:           in.RetainedChunks,
		DroppedChunks:            in.DroppedChunks,
		OldestRetainedSequence:   in.OldestRetainedSequence,
		NewestRetainedSequence:   in.NewestRetainedSequence,
		StaleBehindLatest:        in.StaleBehindLatest,
		NewerRetainedCount:       in.NewerRetainedCount,
		OldestUnreviewedSequence: in.OldestUnreviewedSequence,
		ClosureState:             string(in.ClosureState),
	}
}

func ipcTranscriptReviewClosure(in orchestrator.ShellTranscriptReviewClosure) ipc.TaskShellTranscriptReviewClosure {
	return ipc.TaskShellTranscriptReviewClosure{
		State:                    string(in.State),
		Scope:                    string(in.Scope),
		HasReview:                in.HasReview,
		HasUnreadNewerEvidence:   in.HasUnreadNewerEvidence,
		ReviewedUpToSequence:     in.ReviewedUpToSequence,
		OldestUnreviewedSequence: in.OldestUnreviewedSequence,
		NewestRetainedSequence:   in.NewestRetainedSequence,
		UnreviewedRetainedCount:  in.UnreviewedRetainedCount,
		RetentionLimit:           in.RetentionLimit,
		RetainedChunkCount:       in.RetainedChunkCount,
		DroppedChunkCount:        in.DroppedChunkCount,
	}
}

func ipcTaskShellSessionRecord(in orchestrator.ShellSessionView) ipc.TaskShellSessionRecord {
	out := ipc.TaskShellSessionRecord{
		SessionID:                        in.SessionID,
		TaskID:                           in.TaskID,
		WorkerPreference:                 in.WorkerPreference,
		ResolvedWorker:                   in.ResolvedWorker,
		WorkerSessionID:                  in.WorkerSessionID,
		WorkerSessionIDSource:            string(in.WorkerSessionIDSource),
		AttachCapability:                 string(in.AttachCapability),
		HostMode:                         in.HostMode,
		HostState:                        in.HostState,
		SessionClass:                     string(in.SessionClass),
		SessionClassReason:               in.SessionClassReason,
		ReattachGuidance:                 in.ReattachGuidance,
		OperatorSummary:                  in.OperatorSummary,
		TranscriptState:                  string(in.TranscriptState),
		TranscriptRetainedChunks:         in.TranscriptRetainedChunks,
		TranscriptDroppedChunks:          in.TranscriptDroppedChunks,
		TranscriptRetentionLimit:         in.TranscriptRetentionLimit,
		TranscriptOldestSequence:         in.TranscriptOldestSequence,
		TranscriptNewestSequence:         in.TranscriptNewestSequence,
		TranscriptLastChunkAt:            in.TranscriptLastChunkAt,
		TranscriptReviewID:               in.TranscriptReviewID,
		TranscriptReviewSource:           string(in.TranscriptReviewSource),
		TranscriptReviewedUpTo:           in.TranscriptReviewedUpTo,
		TranscriptReviewSummary:          in.TranscriptReviewSummary,
		TranscriptReviewAt:               in.TranscriptReviewAt,
		TranscriptReviewStale:            in.TranscriptReviewStale,
		TranscriptReviewNewer:            in.TranscriptReviewNewer,
		TranscriptReviewClosureState:     string(in.TranscriptReviewClosureState),
		TranscriptReviewOldestUnreviewed: in.TranscriptReviewOldestUnreviewed,
		StartedAt:                        in.StartedAt,
		LastUpdatedAt:                    in.LastUpdatedAt,
		Active:                           in.Active,
		Note:                             in.Note,
		LatestEventID:                    in.LatestEventID,
		LatestEventKind:                  in.LatestEventKind,
		LatestEventAt:                    in.LatestEventAt,
		LatestEventNote:                  in.LatestEventNote,
	}
	if len(in.TranscriptRecentReviews) > 0 {
		out.TranscriptRecentReviews = make([]ipc.TaskShellTranscriptReviewMarker, 0, len(in.TranscriptRecentReviews))
		for _, review := range in.TranscriptRecentReviews {
			out.TranscriptRecentReviews = append(out.TranscriptRecentReviews, ipcTranscriptReviewMarker(review))
		}
	}
	return out
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
		ReceiptID:                        in.ReceiptID,
		TaskID:                           in.TaskID,
		ActionHandle:                     in.ActionHandle,
		ExecutionDomain:                  in.ExecutionDomain,
		CommandSurfaceKind:               in.CommandSurfaceKind,
		ExecutionAttempted:               in.ExecutionAttempted,
		ResultClass:                      string(in.ResultClass),
		Summary:                          in.Summary,
		Reason:                           in.Reason,
		RunID:                            in.RunID,
		CheckpointID:                     in.CheckpointID,
		BriefID:                          in.BriefID,
		HandoffID:                        in.HandoffID,
		LaunchAttemptID:                  in.LaunchAttemptID,
		LaunchID:                         in.LaunchID,
		ReviewGapState:                   in.ReviewGapState,
		ReviewGapSessionID:               in.ReviewGapSessionID,
		ReviewGapClass:                   in.ReviewGapClass,
		ReviewGapPresent:                 in.ReviewGapPresent,
		ReviewGapReviewedUpTo:            in.ReviewGapReviewedUpTo,
		ReviewGapOldestUnreviewed:        in.ReviewGapOldestUnreviewed,
		ReviewGapNewestRetained:          in.ReviewGapNewestRetained,
		ReviewGapUnreviewedRetainedCount: in.ReviewGapUnreviewedRetainedCount,
		ReviewGapAcknowledged:            in.ReviewGapAcknowledged,
		ReviewGapAcknowledgmentID:        in.ReviewGapAcknowledgmentID,
		ReviewGapAcknowledgmentClass:     in.ReviewGapAcknowledgmentClass,
		TransitionReceiptID:              in.TransitionReceiptID,
		TransitionKind:                   in.TransitionKind,
		CreatedAt:                        in.CreatedAt,
	}
	if in.CompletedAt != nil {
		out.CompletedAt = *in.CompletedAt
	}
	return out
}

func ipcTranscriptReviewGapAcknowledgment(in *orchestrator.TranscriptReviewGapAcknowledgmentSummary) *ipc.TaskTranscriptReviewGapAcknowledgment {
	if in == nil {
		return nil
	}
	return &ipc.TaskTranscriptReviewGapAcknowledgment{
		AcknowledgmentID:         in.AcknowledgmentID,
		TaskID:                   in.TaskID,
		SessionID:                in.SessionID,
		Class:                    string(in.Class),
		ReviewState:              in.ReviewState,
		ReviewScope:              string(in.ReviewScope),
		ReviewedUpToSequence:     in.ReviewedUpToSequence,
		OldestUnreviewedSequence: in.OldestUnreviewedSequence,
		NewestRetainedSequence:   in.NewestRetainedSequence,
		UnreviewedRetainedCount:  in.UnreviewedRetainedCount,
		TranscriptState:          string(in.TranscriptState),
		RetentionLimit:           in.RetentionLimit,
		RetainedChunks:           in.RetainedChunks,
		DroppedChunks:            in.DroppedChunks,
		ActionContext:            in.ActionContext,
		Summary:                  in.Summary,
		CreatedAt:                in.CreatedAt,
		StaleBehindCurrent:       in.StaleBehindCurrent,
		NewerRetainedCount:       in.NewerRetainedCount,
	}
}

func ipcTranscriptReviewGapAcknowledgments(in []orchestrator.TranscriptReviewGapAcknowledgmentSummary) []ipc.TaskTranscriptReviewGapAcknowledgment {
	if len(in) == 0 {
		return nil
	}
	out := make([]ipc.TaskTranscriptReviewGapAcknowledgment, 0, len(in))
	for i := range in {
		if mapped := ipcTranscriptReviewGapAcknowledgment(&in[i]); mapped != nil {
			out = append(out, *mapped)
		}
	}
	return out
}

func ipcContinuityTransitionReceipt(in *orchestrator.ContinuityTransitionReceiptSummary) *ipc.TaskContinuityTransitionReceipt {
	if in == nil {
		return nil
	}
	return &ipc.TaskContinuityTransitionReceipt{
		ReceiptID:                in.ReceiptID,
		TaskID:                   in.TaskID,
		ShellSessionID:           in.ShellSessionID,
		TransitionKind:           string(in.TransitionKind),
		TransitionHandle:         in.TransitionHandle,
		TriggerAction:            in.TriggerAction,
		TriggerSource:            in.TriggerSource,
		HandoffID:                in.HandoffID,
		LaunchAttemptID:          in.LaunchAttemptID,
		LaunchID:                 in.LaunchID,
		ResolutionID:             in.ResolutionID,
		BranchClassBefore:        string(in.BranchClassBefore),
		BranchRefBefore:          in.BranchRefBefore,
		BranchClassAfter:         string(in.BranchClassAfter),
		BranchRefAfter:           in.BranchRefAfter,
		HandoffStateBefore:       string(in.HandoffStateBefore),
		HandoffStateAfter:        string(in.HandoffStateAfter),
		LaunchControlBefore:      string(in.LaunchControlBefore),
		LaunchControlAfter:       string(in.LaunchControlAfter),
		ReviewGapPresent:         in.ReviewGapPresent,
		ReviewPosture:            string(in.ReviewPosture),
		ReviewState:              in.ReviewState,
		ReviewScope:              string(in.ReviewScope),
		ReviewedUpToSequence:     in.ReviewedUpToSequence,
		OldestUnreviewedSequence: in.OldestUnreviewedSequence,
		NewestRetainedSequence:   in.NewestRetainedSequence,
		UnreviewedRetainedCount:  in.UnreviewedRetainedCount,
		LatestReviewID:           in.LatestReviewID,
		LatestReviewGapAckID:     in.LatestReviewGapAckID,
		AcknowledgmentPresent:    in.AcknowledgmentPresent,
		AcknowledgmentClass:      string(in.AcknowledgmentClass),
		Summary:                  in.Summary,
		CreatedAt:                in.CreatedAt,
	}
}

func ipcContinuityTransitionReceipts(in []orchestrator.ContinuityTransitionReceiptSummary) []ipc.TaskContinuityTransitionReceipt {
	if len(in) == 0 {
		return nil
	}
	out := make([]ipc.TaskContinuityTransitionReceipt, 0, len(in))
	for i := range in {
		if mapped := ipcContinuityTransitionReceipt(&in[i]); mapped != nil {
			out = append(out, *mapped)
		}
	}
	return out
}

func ipcContinuityTransitionRiskSummary(in *orchestrator.ContinuityTransitionRiskSummary) *ipc.TaskContinuityTransitionRiskSummary {
	if in == nil {
		return nil
	}
	return &ipc.TaskContinuityTransitionRiskSummary{
		WindowSize:                           in.WindowSize,
		ReviewGapTransitions:                 in.ReviewGapTransitions,
		AcknowledgedReviewGapTransitions:     in.AcknowledgedReviewGapTransitions,
		UnacknowledgedReviewGapTransitions:   in.UnacknowledgedReviewGapTransitions,
		StaleReviewPostureTransitions:        in.StaleReviewPostureTransitions,
		SourceScopedReviewPostureTransitions: in.SourceScopedReviewPostureTransitions,
		IntoClaudeOwnershipTransitions:       in.IntoClaudeOwnershipTransitions,
		BackToLocalOwnershipTransitions:      in.BackToLocalOwnershipTransitions,
		OperationallyNotable:                 in.OperationallyNotable,
		Summary:                              in.Summary,
	}
}

func ipcContinuityIncidentRiskSummary(in *orchestrator.ContinuityIncidentRiskSummary) *ipc.TaskContinuityIncidentRiskSummary {
	if in == nil {
		return nil
	}
	return &ipc.TaskContinuityIncidentRiskSummary{
		ReviewGapPresent:                in.ReviewGapPresent,
		AcknowledgmentPresent:           in.AcknowledgmentPresent,
		StaleOrUnreviewedReviewPosture:  in.StaleOrUnreviewedReviewPosture,
		SourceScopedReviewPosture:       in.SourceScopedReviewPosture,
		IntoClaudeOwnershipTransition:   in.IntoClaudeOwnershipTransition,
		BackToLocalOwnershipTransition:  in.BackToLocalOwnershipTransition,
		UnresolvedContinuityAmbiguity:   in.UnresolvedContinuityAmbiguity,
		NearbyFailedOrInterruptedRuns:   in.NearbyFailedOrInterruptedRuns,
		NearbyRecoveryActions:           in.NearbyRecoveryActions,
		RecentFailureOrRecoveryActivity: in.RecentFailureOrRecoveryActivity,
		OperationallyNotable:            in.OperationallyNotable,
		Summary:                         in.Summary,
	}
}

func ipcContinuityIncidentTriageReceipt(in *orchestrator.ContinuityIncidentTriageReceiptSummary) *ipc.TaskContinuityIncidentTriageReceipt {
	if in == nil {
		return nil
	}
	return &ipc.TaskContinuityIncidentTriageReceipt{
		ReceiptID:                 in.ReceiptID,
		TaskID:                    in.TaskID,
		AnchorMode:                string(in.AnchorMode),
		AnchorTransitionReceiptID: in.AnchorTransitionReceiptID,
		AnchorTransitionKind:      string(in.AnchorTransitionKind),
		AnchorHandoffID:           in.AnchorHandoffID,
		AnchorShellSessionID:      in.AnchorShellSessionID,
		Posture:                   string(in.Posture),
		FollowUpPosture:           string(in.FollowUpPosture),
		Summary:                   in.Summary,
		ReviewGapPresent:          in.ReviewGapPresent,
		ReviewPosture:             string(in.ReviewPosture),
		ReviewState:               in.ReviewState,
		ReviewScope:               in.ReviewScope,
		ReviewedUpToSequence:      in.ReviewedUpToSequence,
		OldestUnreviewedSequence:  in.OldestUnreviewedSequence,
		NewestRetainedSequence:    in.NewestRetainedSequence,
		UnreviewedRetainedCount:   in.UnreviewedRetainedCount,
		LatestReviewID:            in.LatestReviewID,
		LatestReviewGapAckID:      in.LatestReviewGapAckID,
		AcknowledgmentPresent:     in.AcknowledgmentPresent,
		AcknowledgmentClass:       in.AcknowledgmentClass,
		RiskSummary:               *ipcContinuityIncidentRiskSummary(&in.RiskSummary),
		CreatedAt:                 in.CreatedAt,
	}
}

func ipcContinuityIncidentTriageReceipts(in []orchestrator.ContinuityIncidentTriageReceiptSummary) []ipc.TaskContinuityIncidentTriageReceipt {
	if len(in) == 0 {
		return nil
	}
	out := make([]ipc.TaskContinuityIncidentTriageReceipt, 0, len(in))
	for i := range in {
		if mapped := ipcContinuityIncidentTriageReceipt(&in[i]); mapped != nil {
			out = append(out, *mapped)
		}
	}
	return out
}

func ipcContinuityIncidentFollowUpSummary(in *orchestrator.ContinuityIncidentFollowUpSummary) *ipc.TaskContinuityIncidentFollowUpSummary {
	if in == nil {
		return nil
	}
	return &ipc.TaskContinuityIncidentFollowUpSummary{
		State:                     string(in.State),
		Digest:                    in.Digest,
		WindowAdvisory:            in.WindowAdvisory,
		Advisory:                  in.Advisory,
		FollowUpAdvised:           in.FollowUpAdvised,
		NeedsFollowUp:             in.NeedsFollowUp,
		Deferred:                  in.Deferred,
		TriageBehindLatest:        in.TriageBehindLatest,
		TriagedUnderReviewRisk:    in.TriagedUnderReviewRisk,
		LatestTransitionReceiptID: in.LatestTransitionReceiptID,
		LatestTriageReceiptID:     in.LatestTriageReceiptID,
		TriageAnchorReceiptID:     in.TriageAnchorReceiptID,
		TriagePosture:             string(in.TriagePosture),
		LatestFollowUpReceiptID:   in.LatestFollowUpReceiptID,
		LatestFollowUpActionKind:  string(in.LatestFollowUpActionKind),
		LatestFollowUpSummary:     in.LatestFollowUpSummary,
		LatestFollowUpAt:          in.LatestFollowUpAt,
		FollowUpReceiptPresent:    in.FollowUpReceiptPresent,
		FollowUpOpen:              in.FollowUpOpen,
		FollowUpClosed:            in.FollowUpClosed,
		FollowUpReopened:          in.FollowUpReopened,
		FollowUpProgressed:        in.FollowUpProgressed,
		ClosureIntelligence:       ipcContinuityIncidentClosureSummary(in.ClosureIntelligence),
	}
}

func ipcContinuityIncidentClosureSummary(in *orchestrator.ContinuityIncidentClosureSummary) *ipc.TaskContinuityIncidentClosureSummary {
	if in == nil {
		return nil
	}
	out := &ipc.TaskContinuityIncidentClosureSummary{
		Class:                             string(in.Class),
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
		out.RecentAnchors = make([]ipc.TaskContinuityIncidentClosureAnchorItem, 0, len(in.RecentAnchors))
		for _, item := range in.RecentAnchors {
			out.RecentAnchors = append(out.RecentAnchors, ipc.TaskContinuityIncidentClosureAnchorItem{
				AnchorTransitionReceiptID: item.AnchorTransitionReceiptID,
				Class:                     string(item.Class),
				Digest:                    item.Digest,
				Explanation:               item.Explanation,
				LatestFollowUpReceiptID:   item.LatestFollowUpReceiptID,
				LatestFollowUpActionKind:  string(item.LatestFollowUpActionKind),
				LatestFollowUpAt:          item.LatestFollowUpAt,
			})
		}
	}
	return out
}

func ipcContinuityIncidentTaskRiskSummary(in *orchestrator.ContinuityIncidentTaskRiskSummary) *ipc.TaskContinuityIncidentTaskRiskSummary {
	if in == nil {
		return nil
	}
	out := &ipc.TaskContinuityIncidentTaskRiskSummary{
		Class:                               string(in.Class),
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
		out.RecentAnchorClasses = make([]string, 0, len(in.RecentAnchorClasses))
		for _, class := range in.RecentAnchorClasses {
			out.RecentAnchorClasses = append(out.RecentAnchorClasses, string(class))
		}
	}
	return out
}

func ipcContinuityIncidentTriageHistoryRollup(in *orchestrator.ContinuityIncidentTriageHistoryRollupSummary) *ipc.TaskContinuityIncidentTriageHistoryRollup {
	if in == nil {
		return nil
	}
	return &ipc.TaskContinuityIncidentTriageHistoryRollup{
		WindowSize:                        in.WindowSize,
		BoundedWindow:                     in.BoundedWindow,
		DistinctAnchors:                   in.DistinctAnchors,
		AnchorsTriagedCurrent:             in.AnchorsTriagedCurrent,
		AnchorsNeedsFollowUp:              in.AnchorsNeedsFollowUp,
		AnchorsDeferred:                   in.AnchorsDeferred,
		AnchorsBehindLatestTransition:     in.AnchorsBehindLatestTransition,
		AnchorsWithOpenFollowUp:           in.AnchorsWithOpenFollowUp,
		AnchorsRepeatedWithoutProgression: in.AnchorsRepeatedWithoutProgression,
		ReviewRiskReceipts:                in.ReviewRiskReceipts,
		AcknowledgedReviewGapReceipts:     in.AcknowledgedReviewGapReceipts,
		OperationallyNotable:              in.OperationallyNotable,
		Summary:                           in.Summary,
	}
}

func ipcContinuityIncidentFollowUpReceipt(in *orchestrator.ContinuityIncidentFollowUpReceiptSummary) *ipc.TaskContinuityIncidentFollowUpReceipt {
	if in == nil {
		return nil
	}
	return &ipc.TaskContinuityIncidentFollowUpReceipt{
		ReceiptID:                 in.ReceiptID,
		TaskID:                    in.TaskID,
		AnchorMode:                string(in.AnchorMode),
		AnchorTransitionReceiptID: in.AnchorTransitionReceiptID,
		AnchorTransitionKind:      string(in.AnchorTransitionKind),
		AnchorHandoffID:           in.AnchorHandoffID,
		AnchorShellSessionID:      in.AnchorShellSessionID,
		TriageReceiptID:           in.TriageReceiptID,
		TriagePosture:             string(in.TriagePosture),
		TriageFollowUpPosture:     string(in.TriageFollowUpState),
		ActionKind:                string(in.ActionKind),
		Summary:                   in.Summary,
		ReviewGapPresent:          in.ReviewGapPresent,
		ReviewPosture:             string(in.ReviewPosture),
		ReviewState:               in.ReviewState,
		ReviewScope:               in.ReviewScope,
		ReviewedUpToSequence:      in.ReviewedUpToSequence,
		OldestUnreviewedSequence:  in.OldestUnreviewedSequence,
		NewestRetainedSequence:    in.NewestRetainedSequence,
		UnreviewedRetainedCount:   in.UnreviewedRetainedCount,
		LatestReviewID:            in.LatestReviewID,
		LatestReviewGapAckID:      in.LatestReviewGapAckID,
		AcknowledgmentPresent:     in.AcknowledgmentPresent,
		AcknowledgmentClass:       in.AcknowledgmentClass,
		TriagedUnderReviewRisk:    in.TriagedUnderReviewRisk,
		CreatedAt:                 in.CreatedAt,
	}
}

func ipcContinuityIncidentFollowUpReceipts(in []orchestrator.ContinuityIncidentFollowUpReceiptSummary) []ipc.TaskContinuityIncidentFollowUpReceipt {
	if len(in) == 0 {
		return nil
	}
	out := make([]ipc.TaskContinuityIncidentFollowUpReceipt, 0, len(in))
	for i := range in {
		if mapped := ipcContinuityIncidentFollowUpReceipt(&in[i]); mapped != nil {
			out = append(out, *mapped)
		}
	}
	return out
}

func ipcContinuityIncidentFollowUpHistoryRollup(in *orchestrator.ContinuityIncidentFollowUpHistoryRollupSummary) *ipc.TaskContinuityIncidentFollowUpHistoryRollup {
	if in == nil {
		return nil
	}
	return &ipc.TaskContinuityIncidentFollowUpHistoryRollup{
		WindowSize:                        in.WindowSize,
		BoundedWindow:                     in.BoundedWindow,
		DistinctAnchors:                   in.DistinctAnchors,
		ReceiptsRecordedPending:           in.ReceiptsRecordedPending,
		ReceiptsProgressed:                in.ReceiptsProgressed,
		ReceiptsClosed:                    in.ReceiptsClosed,
		ReceiptsReopened:                  in.ReceiptsReopened,
		AnchorsWithOpenFollowUp:           in.AnchorsWithOpenFollowUp,
		AnchorsClosed:                     in.AnchorsClosed,
		AnchorsReopened:                   in.AnchorsReopened,
		OpenAnchorsBehindLatestTransition: in.OpenAnchorsBehindLatestTransition,
		AnchorsRepeatedWithoutProgression: in.AnchorsRepeatedWithoutProgression,
		AnchorsTriagedWithoutFollowUp:     in.AnchorsTriagedWithoutFollowUp,
		OperationallyNotable:              in.OperationallyNotable,
		Summary:                           in.Summary,
	}
}

func ipcContinuityIncidentRuns(in []orchestrator.ContinuityIncidentRunSummary) []ipc.TaskContinuityIncidentRun {
	if len(in) == 0 {
		return nil
	}
	out := make([]ipc.TaskContinuityIncidentRun, 0, len(in))
	for _, item := range in {
		out = append(out, ipc.TaskContinuityIncidentRun{
			RunID:          item.RunID,
			WorkerKind:     item.WorkerKind,
			Status:         item.Status,
			ShellSessionID: item.ShellSessionID,
			ExitCode:       item.ExitCode,
			OccurredAt:     item.OccurredAt,
			StartedAt:      item.StartedAt,
			EndedAt:        item.EndedAt,
			Summary:        item.Summary,
		})
	}
	return out
}

func ipcContinuityIncidentProofEvents(in []orchestrator.ContinuityIncidentProofSummary) []ipc.TaskContinuityIncidentProof {
	if len(in) == 0 {
		return nil
	}
	out := make([]ipc.TaskContinuityIncidentProof, 0, len(in))
	for _, item := range in {
		out = append(out, ipc.TaskContinuityIncidentProof{
			EventID:    item.EventID,
			Type:       string(item.Type),
			ActorType:  string(item.ActorType),
			ActorID:    item.ActorID,
			Timestamp:  item.Timestamp,
			Summary:    item.Summary,
			SequenceNo: item.SequenceNo,
		})
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
		ReceiptID:                        in.ReceiptID,
		ActionHandle:                     in.ActionHandle,
		ResultClass:                      string(in.ResultClass),
		Summary:                          in.Summary,
		Reason:                           in.Reason,
		ReviewGapState:                   in.ReviewGapState,
		ReviewGapSessionID:               in.ReviewGapSessionID,
		ReviewGapClass:                   in.ReviewGapClass,
		ReviewGapPresent:                 in.ReviewGapPresent,
		ReviewGapReviewedUpTo:            in.ReviewGapReviewedUpTo,
		ReviewGapOldestUnreviewed:        in.ReviewGapOldestUnreviewed,
		ReviewGapNewestRetained:          in.ReviewGapNewestRetained,
		ReviewGapUnreviewedRetainedCount: in.ReviewGapUnreviewedRetainedCount,
		ReviewGapAcknowledged:            in.ReviewGapAcknowledged,
		ReviewGapAcknowledgmentID:        in.ReviewGapAcknowledgmentID,
		ReviewGapAcknowledgmentClass:     in.ReviewGapAcknowledgmentClass,
		TransitionReceiptID:              in.TransitionReceiptID,
		TransitionKind:                   in.TransitionKind,
		CreatedAt:                        in.CreatedAt,
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
		latestShellSessionUpdatedAt := int64(0)
		if !out.LatestShellSessionUpdatedAt.IsZero() {
			latestShellSessionUpdatedAt = out.LatestShellSessionUpdatedAt.UnixMilli()
		}
		latestShellEventAt := int64(0)
		if !out.LatestShellEventAt.IsZero() {
			latestShellEventAt = out.LatestShellEventAt.UnixMilli()
		}
		latestShellTranscriptLastChunkAt := int64(0)
		if !out.LatestShellTranscriptLastChunkAt.IsZero() {
			latestShellTranscriptLastChunkAt = out.LatestShellTranscriptLastChunkAt.UnixMilli()
		}
		latestShellTranscriptReviewAt := int64(0)
		if !out.LatestShellTranscriptReviewAt.IsZero() {
			latestShellTranscriptReviewAt = out.LatestShellTranscriptReviewAt.UnixMilli()
		}
		return respondOK(ipc.TaskStatusResponse{
			TaskID:                                      out.TaskID,
			ConversationID:                              out.ConversationID,
			Goal:                                        out.Goal,
			Phase:                                       out.Phase,
			Status:                                      out.Status,
			CurrentIntentID:                             out.CurrentIntentID,
			CurrentIntentClass:                          string(out.CurrentIntentClass),
			CurrentIntentSummary:                        out.CurrentIntentSummary,
			CompiledIntent:                              ipcCompiledIntentSummary(out.CompiledIntent),
			CurrentBriefID:                              out.CurrentBriefID,
			CurrentBriefHash:                            out.CurrentBriefHash,
			CompiledBrief:                               ipcCompiledBriefSummary(out.CompiledBrief),
			LatestRunID:                                 out.LatestRunID,
			LatestRunStatus:                             out.LatestRunStatus,
			LatestRunSummary:                            out.LatestRunSummary,
			LatestRunWorkerRunID:                        out.LatestRunWorkerRunID,
			LatestRunShellSessionID:                     out.LatestRunShellSessionID,
			LatestRunCommand:                            out.LatestRunCommand,
			LatestRunArgs:                               append([]string{}, out.LatestRunArgs...),
			LatestRunExitCode:                           out.LatestRunExitCode,
			LatestRunChangedFiles:                       append([]string{}, out.LatestRunChangedFiles...),
			LatestRunValidationSignals:                  append([]string{}, out.LatestRunValidationSignals...),
			LatestRunOutputArtifactRef:                  out.LatestRunOutputArtifactRef,
			LatestRunStructuredSummary:                  out.LatestRunStructuredSummary,
			LatestShellSessionID:                        out.LatestShellSessionID,
			LatestShellSessionClass:                     string(out.LatestShellSessionClass),
			LatestShellSessionReason:                    out.LatestShellSessionReason,
			LatestShellSessionGuidance:                  out.LatestShellSessionGuidance,
			LatestShellSessionWorkerSessionID:           out.LatestShellSessionWorkerSessionID,
			LatestShellSessionWorkerSessionIDSource:     string(out.LatestShellSessionWorkerSessionIDSource),
			LatestShellTranscriptState:                  string(out.LatestShellTranscriptState),
			LatestShellTranscriptRetainedChunks:         out.LatestShellTranscriptRetainedChunks,
			LatestShellTranscriptDroppedChunks:          out.LatestShellTranscriptDroppedChunks,
			LatestShellTranscriptRetentionLimit:         out.LatestShellTranscriptRetentionLimit,
			LatestShellTranscriptOldestSequence:         out.LatestShellTranscriptOldestSequence,
			LatestShellTranscriptNewestSequence:         out.LatestShellTranscriptNewestSequence,
			LatestShellTranscriptLastChunkAtUnixMs:      latestShellTranscriptLastChunkAt,
			LatestShellTranscriptReviewID:               out.LatestShellTranscriptReviewID,
			LatestShellTranscriptReviewSource:           string(out.LatestShellTranscriptReviewSource),
			LatestShellTranscriptReviewedUpTo:           out.LatestShellTranscriptReviewedUpTo,
			LatestShellTranscriptReviewSummary:          out.LatestShellTranscriptReviewSummary,
			LatestShellTranscriptReviewAtUnixMs:         latestShellTranscriptReviewAt,
			LatestShellTranscriptReviewStale:            out.LatestShellTranscriptReviewStale,
			LatestShellTranscriptReviewNewer:            out.LatestShellTranscriptReviewNewer,
			LatestShellTranscriptReviewClosureState:     string(out.LatestShellTranscriptReviewClosureState),
			LatestShellTranscriptReviewOldestUnreviewed: out.LatestShellTranscriptReviewOldestUnreviewed,
			LatestShellSessionState:                     out.LatestShellSessionState,
			LatestShellSessionUpdatedAtUnixMs:           latestShellSessionUpdatedAt,
			LatestShellEventID:                          out.LatestShellEventID,
			LatestShellEventKind:                        out.LatestShellEventKind,
			LatestShellEventSessionID:                   out.LatestShellEventSessionID,
			LatestShellEventAtUnixMs:                    latestShellEventAt,
			LatestShellEventNote:                        out.LatestShellEventNote,
			RepoAnchor: ipc.RepoAnchor{
				RepoRoot:         out.RepoAnchor.RepoRoot,
				Branch:           out.RepoAnchor.Branch,
				HeadSHA:          out.RepoAnchor.HeadSHA,
				WorkingTreeDirty: out.RepoAnchor.WorkingTreeDirty,
				CapturedAt:       out.RepoAnchor.CapturedAt,
			},
			LatestCheckpointID:                       out.LatestCheckpointID,
			LatestCheckpointAtUnixMs:                 latestCheckpointAt,
			LatestCheckpointTrigger:                  string(out.LatestCheckpointTrigger),
			CheckpointResumable:                      out.CheckpointResumable,
			ResumeDescriptor:                         out.ResumeDescriptor,
			LatestLaunchAttemptID:                    out.LatestLaunchAttemptID,
			LatestLaunchID:                           out.LatestLaunchID,
			LatestLaunchStatus:                       string(out.LatestLaunchStatus),
			LatestAcknowledgmentID:                   out.LatestAcknowledgmentID,
			LatestAcknowledgmentStatus:               string(out.LatestAcknowledgmentStatus),
			LatestAcknowledgmentSummary:              out.LatestAcknowledgmentSummary,
			LatestFollowThroughID:                    out.LatestFollowThroughID,
			LatestFollowThroughKind:                  string(out.LatestFollowThroughKind),
			LatestFollowThroughSummary:               out.LatestFollowThroughSummary,
			LatestResolutionID:                       out.LatestResolutionID,
			LatestResolutionKind:                     string(out.LatestResolutionKind),
			LatestResolutionSummary:                  out.LatestResolutionSummary,
			LatestResolutionAtUnixMs:                 latestResolutionAt,
			LaunchControlState:                       string(out.LaunchControlState),
			LaunchRetryDisposition:                   string(out.LaunchRetryDisposition),
			LaunchControlReason:                      out.LaunchControlReason,
			HandoffContinuityState:                   string(out.HandoffContinuityState),
			HandoffContinuityReason:                  out.HandoffContinuityReason,
			HandoffContinuationProven:                out.HandoffContinuationProven,
			ActiveBranchClass:                        string(out.ActiveBranchClass),
			ActiveBranchRef:                          out.ActiveBranchRef,
			ActiveBranchAnchorKind:                   string(out.ActiveBranchAnchorKind),
			ActiveBranchAnchorRef:                    out.ActiveBranchAnchorRef,
			ActiveBranchReason:                       out.ActiveBranchReason,
			LocalRunFinalizationState:                string(out.LocalRunFinalizationState),
			LocalRunFinalizationRunID:                out.LocalRunFinalizationRunID,
			LocalRunFinalizationStatus:               out.LocalRunFinalizationStatus,
			LocalRunFinalizationCheckpointID:         out.LocalRunFinalizationCheckpointID,
			LocalRunFinalizationReason:               out.LocalRunFinalizationReason,
			LocalResumeAuthorityState:                string(out.LocalResumeAuthorityState),
			LocalResumeMode:                          string(out.LocalResumeMode),
			LocalResumeCheckpointID:                  out.LocalResumeCheckpointID,
			LocalResumeRunID:                         out.LocalResumeRunID,
			LocalResumeReason:                        out.LocalResumeReason,
			RequiredNextOperatorAction:               string(out.RequiredNextOperatorAction),
			ActionAuthority:                          ipcOperatorActionAuthorities(out.ActionAuthority),
			OperatorDecision:                         ipcOperatorDecisionSummary(out.OperatorDecision),
			OperatorExecutionPlan:                    ipcOperatorExecutionPlan(out.OperatorExecutionPlan),
			LatestOperatorStepReceipt:                ipcOperatorStepReceipt(out.LatestOperatorStepReceipt),
			RecentOperatorStepReceipts:               ipcOperatorStepReceipts(out.RecentOperatorStepReceipts),
			LatestContinuityTransitionReceipt:        ipcContinuityTransitionReceipt(out.LatestContinuityTransitionReceipt),
			RecentContinuityTransitionReceipts:       ipcContinuityTransitionReceipts(out.RecentContinuityTransitionReceipts),
			ContinuityTransitionRiskSummary:          ipcContinuityTransitionRiskSummary(out.ContinuityTransitionRiskSummary),
			ContinuityIncidentSummary:                ipcContinuityIncidentRiskSummary(out.ContinuityIncidentSummary),
			LatestContinuityIncidentTriageReceipt:    ipcContinuityIncidentTriageReceipt(out.LatestContinuityIncidentTriageReceipt),
			RecentContinuityIncidentTriageReceipts:   ipcContinuityIncidentTriageReceipts(out.RecentContinuityIncidentTriageReceipts),
			ContinuityIncidentTriageHistoryRollup:    ipcContinuityIncidentTriageHistoryRollup(out.ContinuityIncidentTriageHistoryRollup),
			LatestContinuityIncidentFollowUpReceipt:  ipcContinuityIncidentFollowUpReceipt(out.LatestContinuityIncidentFollowUpReceipt),
			RecentContinuityIncidentFollowUpReceipts: ipcContinuityIncidentFollowUpReceipts(out.RecentContinuityIncidentFollowUpReceipts),
			ContinuityIncidentFollowUpHistoryRollup:  ipcContinuityIncidentFollowUpHistoryRollup(out.ContinuityIncidentFollowUpHistoryRollup),
			ContinuityIncidentFollowUp:               ipcContinuityIncidentFollowUpSummary(out.ContinuityIncidentFollowUp),
			ContinuityIncidentTaskRisk:               ipcContinuityIncidentTaskRiskSummary(out.ContinuityIncidentTaskRisk),
			LatestTranscriptReviewGapAcknowledgment:  ipcTranscriptReviewGapAcknowledgment(out.LatestTranscriptReviewGapAcknowledgment),
			RecentTranscriptReviewGapAcknowledgments: ipcTranscriptReviewGapAcknowledgments(out.RecentTranscriptReviewGapAcknowledgments),
			IsResumable:                              out.IsResumable,
			RecoveryClass:                            string(out.RecoveryClass),
			RecommendedAction:                        string(out.RecommendedAction),
			ReadyForNextRun:                          out.ReadyForNextRun,
			ReadyForHandoffLaunch:                    out.ReadyForHandoffLaunch,
			RecoveryReason:                           out.RecoveryReason,
			LatestRecoveryAction:                     ipcRecoveryActionRecord(out.LatestRecoveryAction),
			LastEventType:                            string(out.LastEventType),
			LastEventID:                              out.LastEventID,
			LastEventAtUnixMs:                        lastEventAt,
		})
	case ipc.MethodTaskIntent:
		var p ipc.TaskIntentRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ReadCompiledIntent(ctx, orchestrator.ReadCompiledIntentRequest{TaskID: string(p.TaskID)})
		if err != nil {
			return respondErr("INTENT_READ_FAILED", err.Error())
		}
		return respondOK(ipc.TaskIntentResponse{
			TaskID:          out.TaskID,
			CurrentIntentID: out.CurrentIntentID,
			Bounded:         out.Bounded,
			Intent:          out.Intent,
			CompiledIntent:  ipcCompiledIntentSummary(out.CompiledIntent),
		})
	case ipc.MethodTaskBrief:
		var p ipc.TaskBriefRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ReadGeneratedBrief(ctx, orchestrator.ReadGeneratedBriefRequest{TaskID: string(p.TaskID)})
		if err != nil {
			return respondErr("BRIEF_READ_FAILED", err.Error())
		}
		return respondOK(ipc.TaskBriefResponse{
			TaskID:         out.TaskID,
			CurrentBriefID: out.CurrentBriefID,
			Bounded:        out.Bounded,
			Brief:          out.Brief,
			CompiledBrief:  ipcCompiledBriefSummary(out.CompiledBrief),
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
		out, err := s.Handler.ExecutePrimaryOperatorStep(ctx, orchestrator.ExecutePrimaryOperatorStepRequest{
			TaskID:                      string(p.TaskID),
			AcknowledgeReviewGap:        p.AcknowledgeReviewGap,
			ReviewGapSessionID:          p.ReviewGapSessionID,
			ReviewGapAcknowledgmentKind: p.ReviewGapAcknowledgmentKind,
			ReviewGapSummary:            p.ReviewGapSummary,
		})
		if err != nil {
			return respondErr("OPERATOR_NEXT_FAILED", err.Error())
		}
		receipt := ipcOperatorStepReceipt(&out.Receipt)
		if receipt == nil {
			return respondErr("OPERATOR_NEXT_FAILED", "missing operator step receipt")
		}
		return respondOK(ipc.TaskExecutePrimaryOperatorStepResponse{
			TaskID:                                   out.TaskID,
			Receipt:                                  *receipt,
			ActiveBranch:                             ipcActiveBranch(&out.ActiveBranch),
			OperatorDecision:                         ipcOperatorDecisionSummary(&out.OperatorDecision),
			OperatorExecutionPlan:                    ipcOperatorExecutionPlan(&out.OperatorExecutionPlan),
			RecoveryClass:                            string(out.RecoveryClass),
			RecommendedAction:                        string(out.RecommendedAction),
			ReadyForNextRun:                          out.ReadyForNextRun,
			ReadyForHandoffLaunch:                    out.ReadyForHandoffLaunch,
			RecoveryReason:                           out.RecoveryReason,
			CanonicalResponse:                        out.CanonicalResponse,
			RecentOperatorStepReceipts:               ipcOperatorStepReceipts(out.RecentOperatorStepReceipts),
			LatestContinuityTransitionReceipt:        ipcContinuityTransitionReceipt(out.LatestContinuityTransitionReceipt),
			RecentContinuityTransitionReceipts:       ipcContinuityTransitionReceipts(out.RecentContinuityTransitionReceipts),
			LatestContinuityIncidentTriageReceipt:    ipcContinuityIncidentTriageReceipt(out.LatestContinuityIncidentTriageReceipt),
			RecentContinuityIncidentTriageReceipts:   ipcContinuityIncidentTriageReceipts(out.RecentContinuityIncidentTriageReceipts),
			ContinuityIncidentTriageHistoryRollup:    ipcContinuityIncidentTriageHistoryRollup(out.ContinuityIncidentTriageHistoryRollup),
			LatestContinuityIncidentFollowUpReceipt:  ipcContinuityIncidentFollowUpReceipt(out.LatestContinuityIncidentFollowUpReceipt),
			RecentContinuityIncidentFollowUpReceipts: ipcContinuityIncidentFollowUpReceipts(out.RecentContinuityIncidentFollowUpReceipts),
			ContinuityIncidentFollowUpHistoryRollup:  ipcContinuityIncidentFollowUpHistoryRollup(out.ContinuityIncidentFollowUpHistoryRollup),
			ContinuityIncidentFollowUp:               ipcContinuityIncidentFollowUpSummary(out.ContinuityIncidentFollowUp),
			LatestTranscriptReviewGapAcknowledgment:  ipcTranscriptReviewGapAcknowledgment(out.LatestTranscriptReviewGapAcknowledgment),
			RecentTranscriptReviewGapAcknowledgments: ipcTranscriptReviewGapAcknowledgments(out.RecentTranscriptReviewGapAcknowledgments),
		})
	case ipc.MethodOperatorAcknowledgeReviewGap:
		var p ipc.TaskOperatorAcknowledgeReviewGapRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.RecordOperatorReviewGapAcknowledgment(ctx, orchestrator.RecordOperatorReviewGapAcknowledgmentRequest{
			TaskID:        string(p.TaskID),
			SessionID:     p.SessionID,
			Kind:          p.Kind,
			Summary:       p.Summary,
			ActionContext: p.ActionContext,
		})
		if err != nil {
			return respondErr("OPERATOR_REVIEW_GAP_ACK_FAILED", err.Error())
		}
		ack := ipcTranscriptReviewGapAcknowledgment(&out.Acknowledgment)
		if ack == nil {
			return respondErr("OPERATOR_REVIEW_GAP_ACK_FAILED", "missing transcript review-gap acknowledgment payload")
		}
		return respondOK(ipc.TaskOperatorAcknowledgeReviewGapResponse{
			TaskID:                 out.TaskID,
			SessionID:              out.SessionID,
			Acknowledgment:         *ack,
			ReviewGapState:         out.ReviewGapState,
			ReviewGapClass:         string(out.ReviewGapClass),
			ReviewScope:            string(out.ReviewScope),
			ReviewedUpToSequence:   out.ReviewedUpToSequence,
			OldestUnreviewedSeq:    out.OldestUnreviewedSeq,
			NewestRetainedSequence: out.NewestRetainedSequence,
			UnreviewedRetained:     out.UnreviewedRetained,
			Advisory:               out.Advisory,
			RecentAcknowledgments:  ipcTranscriptReviewGapAcknowledgments(out.RecentAcknowledgments),
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
			ShellSessionID:     p.ShellSessionID,
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
			Intent:                                   out.Intent,
			CompiledIntent:                           ipcCompiledIntentSummary(out.CompiledIntent),
			Brief:                                    out.Brief,
			CompiledBrief:                            ipcCompiledBriefSummary(out.CompiledBrief),
			Run:                                      out.Run,
			Checkpoint:                               out.Checkpoint,
			Handoff:                                  out.Handoff,
			Launch:                                   out.Launch,
			Acknowledgment:                           out.Acknowledgment,
			FollowThrough:                            out.FollowThrough,
			Resolution:                               out.Resolution,
			ActiveBranch:                             ipcActiveBranch(out.ActiveBranch),
			LocalRunFinalization:                     ipcLocalRunFinalization(out.LocalRunFinalization),
			LocalResumeAuthority:                     ipcLocalResumeAuthority(out.LocalResumeAuthority),
			ActionAuthority:                          ipcOperatorActionAuthoritySet(out.ActionAuthority),
			OperatorDecision:                         ipcOperatorDecisionSummary(out.OperatorDecision),
			OperatorExecutionPlan:                    ipcOperatorExecutionPlan(out.OperatorExecutionPlan),
			LatestOperatorStepReceipt:                ipcOperatorStepReceipt(out.LatestOperatorStepReceipt),
			RecentOperatorStepReceipts:               ipcOperatorStepReceipts(out.RecentOperatorStepReceipts),
			LatestContinuityTransitionReceipt:        ipcContinuityTransitionReceipt(out.LatestContinuityTransitionReceipt),
			RecentContinuityTransitionReceipts:       ipcContinuityTransitionReceipts(out.RecentContinuityTransitionReceipts),
			ContinuityTransitionRiskSummary:          ipcContinuityTransitionRiskSummary(out.ContinuityTransitionRiskSummary),
			ContinuityIncidentSummary:                ipcContinuityIncidentRiskSummary(out.ContinuityIncidentSummary),
			LatestContinuityIncidentTriageReceipt:    ipcContinuityIncidentTriageReceipt(out.LatestContinuityIncidentTriageReceipt),
			RecentContinuityIncidentTriageReceipts:   ipcContinuityIncidentTriageReceipts(out.RecentContinuityIncidentTriageReceipts),
			ContinuityIncidentTriageHistoryRollup:    ipcContinuityIncidentTriageHistoryRollup(out.ContinuityIncidentTriageHistoryRollup),
			LatestContinuityIncidentFollowUpReceipt:  ipcContinuityIncidentFollowUpReceipt(out.LatestContinuityIncidentFollowUpReceipt),
			RecentContinuityIncidentFollowUpReceipts: ipcContinuityIncidentFollowUpReceipts(out.RecentContinuityIncidentFollowUpReceipts),
			ContinuityIncidentFollowUpHistoryRollup:  ipcContinuityIncidentFollowUpHistoryRollup(out.ContinuityIncidentFollowUpHistoryRollup),
			ContinuityIncidentFollowUp:               ipcContinuityIncidentFollowUpSummary(out.ContinuityIncidentFollowUp),
			ContinuityIncidentTaskRisk:               ipcContinuityIncidentTaskRiskSummary(out.ContinuityIncidentTaskRisk),
			LatestTranscriptReviewGapAcknowledgment:  ipcTranscriptReviewGapAcknowledgment(out.LatestTranscriptReviewGapAcknowledgment),
			RecentTranscriptReviewGapAcknowledgments: ipcTranscriptReviewGapAcknowledgments(out.RecentTranscriptReviewGapAcknowledgments),
			LaunchControl:                            ipcLaunchControl(out.LaunchControl),
			HandoffContinuity:                        ipcHandoffContinuity(out.HandoffContinuity),
			Recovery:                                 ipcRecoveryAssessment(out.Recovery),
			LatestRecoveryAction:                     ipcRecoveryActionRecord(out.LatestRecoveryAction),
		}
		if len(out.RecentRecoveryActions) > 0 {
			resp.RecentRecoveryActions = make([]ipc.TaskRecoveryActionRecord, 0, len(out.RecentRecoveryActions))
			for i := range out.RecentRecoveryActions {
				if mapped := ipcRecoveryActionRecord(&out.RecentRecoveryActions[i]); mapped != nil {
					resp.RecentRecoveryActions = append(resp.RecentRecoveryActions, *mapped)
				}
			}
		}
		if len(out.ShellSessions) > 0 {
			resp.ShellSessions = make([]ipc.TaskShellSessionRecord, 0, len(out.ShellSessions))
			for _, session := range out.ShellSessions {
				resp.ShellSessions = append(resp.ShellSessions, ipcTaskShellSessionRecord(session))
			}
		}
		if len(out.RecentShellEvents) > 0 {
			resp.RecentShellEvents = make([]ipc.TaskShellSessionEventRecord, 0, len(out.RecentShellEvents))
			for _, event := range out.RecentShellEvents {
				resp.RecentShellEvents = append(resp.RecentShellEvents, ipc.TaskShellSessionEventRecord{
					EventID:               event.EventID,
					TaskID:                event.TaskID,
					SessionID:             event.SessionID,
					Kind:                  string(event.Kind),
					HostMode:              event.HostMode,
					HostState:             event.HostState,
					WorkerSessionID:       event.WorkerSessionID,
					WorkerSessionIDSource: string(event.WorkerSessionIDSource),
					AttachCapability:      string(event.AttachCapability),
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
		if len(out.RecentShellTranscript) > 0 {
			resp.RecentShellTranscript = make([]ipc.TaskShellTranscriptChunk, 0, len(out.RecentShellTranscript))
			for _, chunk := range out.RecentShellTranscript {
				resp.RecentShellTranscript = append(resp.RecentShellTranscript, ipc.TaskShellTranscriptChunk{
					ChunkID:    chunk.ChunkID,
					TaskID:     chunk.TaskID,
					SessionID:  chunk.SessionID,
					SequenceNo: chunk.SequenceNo,
					Source:     string(chunk.Source),
					Content:    chunk.Content,
					CreatedAt:  chunk.CreatedAt,
				})
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
			TaskID:         out.TaskID,
			Goal:           out.Goal,
			Phase:          out.Phase,
			Status:         out.Status,
			IntentClass:    out.IntentClass,
			IntentSummary:  out.IntentSummary,
			CompiledIntent: ipcCompiledIntentSummary(out.CompiledIntent),
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
				BriefID:                 out.Brief.BriefID,
				Posture:                 string(out.Brief.Posture),
				Objective:               out.Brief.Objective,
				RequestedOutcome:        out.Brief.RequestedOutcome,
				NormalizedAction:        out.Brief.NormalizedAction,
				ScopeSummary:            out.Brief.ScopeSummary,
				Constraints:             append([]string{}, out.Brief.Constraints...),
				DoneCriteria:            append([]string{}, out.Brief.DoneCriteria...),
				AmbiguityFlags:          append([]string{}, out.Brief.AmbiguityFlags...),
				ClarificationQuestions:  append([]string{}, out.Brief.ClarificationQuestions...),
				RequiresClarification:   out.Brief.RequiresClarification,
				WorkerFraming:           out.Brief.WorkerFraming,
				BoundedEvidenceMessages: out.Brief.BoundedEvidenceMessages,
			}
		}
		if out.Run != nil {
			resp.Run = &ipc.TaskShellRun{
				RunID:              out.Run.RunID,
				WorkerKind:         out.Run.WorkerKind,
				Status:             out.Run.Status,
				WorkerRunID:        out.Run.WorkerRunID,
				ShellSessionID:     out.Run.ShellSessionID,
				Command:            out.Run.Command,
				Args:               append([]string{}, out.Run.Args...),
				ExitCode:           out.Run.ExitCode,
				Stdout:             out.Run.Stdout,
				Stderr:             out.Run.Stderr,
				ChangedFiles:       append([]string{}, out.Run.ChangedFiles...),
				ValidationSignals:  append([]string{}, out.Run.ValidationSignals...),
				OutputArtifactRef:  out.Run.OutputArtifactRef,
				StructuredSummary:  out.Run.StructuredSummary,
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
		resp.LatestContinuityTransitionReceipt = ipcContinuityTransitionReceipt(out.LatestContinuityTransitionReceipt)
		resp.RecentContinuityTransitionReceipts = ipcContinuityTransitionReceipts(out.RecentContinuityTransitionReceipts)
		resp.ContinuityTransitionRiskSummary = ipcContinuityTransitionRiskSummary(out.ContinuityTransitionRiskSummary)
		resp.ContinuityIncidentSummary = ipcContinuityIncidentRiskSummary(out.ContinuityIncidentSummary)
		resp.LatestContinuityIncidentTriageReceipt = ipcContinuityIncidentTriageReceipt(out.LatestContinuityIncidentTriageReceipt)
		resp.RecentContinuityIncidentTriageReceipts = ipcContinuityIncidentTriageReceipts(out.RecentContinuityIncidentTriageReceipts)
		resp.ContinuityIncidentTriageHistoryRollup = ipcContinuityIncidentTriageHistoryRollup(out.ContinuityIncidentTriageHistoryRollup)
		resp.LatestContinuityIncidentFollowUpReceipt = ipcContinuityIncidentFollowUpReceipt(out.LatestContinuityIncidentFollowUpReceipt)
		resp.RecentContinuityIncidentFollowUpReceipts = ipcContinuityIncidentFollowUpReceipts(out.RecentContinuityIncidentFollowUpReceipts)
		resp.ContinuityIncidentFollowUpHistoryRollup = ipcContinuityIncidentFollowUpHistoryRollup(out.ContinuityIncidentFollowUpHistoryRollup)
		resp.ContinuityIncidentFollowUp = ipcContinuityIncidentFollowUpSummary(out.ContinuityIncidentFollowUp)
		resp.ContinuityIncidentTaskRisk = ipcContinuityIncidentTaskRiskSummary(out.ContinuityIncidentTaskRisk)
		resp.LatestTranscriptReviewGapAcknowledgment = ipcTranscriptReviewGapAcknowledgment(out.LatestTranscriptReviewGapAcknowledgment)
		resp.RecentTranscriptReviewGapAcknowledgments = ipcTranscriptReviewGapAcknowledgments(out.RecentTranscriptReviewGapAcknowledgments)
		resp.HandoffContinuity = ipcShellHandoffContinuity(out.HandoffContinuity)
		resp.Recovery = ipcShellRecovery(out.Recovery)
		if len(out.ShellSessions) > 0 {
			resp.ShellSessions = make([]ipc.TaskShellSessionRecord, 0, len(out.ShellSessions))
			for _, session := range out.ShellSessions {
				resp.ShellSessions = append(resp.ShellSessions, ipcTaskShellSessionRecord(session))
			}
		}
		if len(out.RecentShellEvents) > 0 {
			resp.RecentShellEvents = make([]ipc.TaskShellSessionEventRecord, 0, len(out.RecentShellEvents))
			for _, event := range out.RecentShellEvents {
				resp.RecentShellEvents = append(resp.RecentShellEvents, ipc.TaskShellSessionEventRecord{
					EventID:               event.EventID,
					TaskID:                out.TaskID,
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
		if len(out.RecentShellTranscript) > 0 {
			resp.RecentShellTranscript = make([]ipc.TaskShellTranscriptChunk, 0, len(out.RecentShellTranscript))
			for _, chunk := range out.RecentShellTranscript {
				resp.RecentShellTranscript = append(resp.RecentShellTranscript, ipc.TaskShellTranscriptChunk{
					ChunkID:    chunk.ChunkID,
					TaskID:     out.TaskID,
					SessionID:  chunk.SessionID,
					SequenceNo: chunk.SequenceNo,
					Source:     chunk.Source,
					Content:    chunk.Content,
					CreatedAt:  chunk.CreatedAt,
				})
			}
		}
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
			TaskID:                string(p.TaskID),
			SessionID:             p.SessionID,
			Kind:                  orchestrator.ShellLifecycleKind(p.Kind),
			HostMode:              p.HostMode,
			HostState:             p.HostState,
			WorkerSessionID:       p.WorkerSessionID,
			WorkerSessionIDSource: shellsession.WorkerSessionIDSource(p.WorkerSessionIDSource),
			AttachCapability:      shellsession.AttachCapability(p.AttachCapability),
			Note:                  p.Note,
			InputLive:             p.InputLive,
			ExitCode:              p.ExitCode,
			PaneWidth:             p.PaneWidth,
			PaneHeight:            p.PaneHeight,
		})
		if err != nil {
			return respondErr("SHELL_LIFECYCLE_FAILED", err.Error())
		}
		return respondOK(ipc.TaskShellLifecycleResponse{TaskID: out.TaskID})
	case ipc.MethodTaskShellTranscriptAppend:
		var p ipc.TaskShellTranscriptAppendRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		chunks := make([]orchestrator.RecordShellTranscriptChunk, 0, len(p.Chunks))
		for _, chunk := range p.Chunks {
			chunks = append(chunks, orchestrator.RecordShellTranscriptChunk{
				Source:    shellsession.TranscriptSource(chunk.Source),
				Content:   chunk.Content,
				CreatedAt: chunk.CreatedAt,
			})
		}
		out, err := s.Handler.RecordShellTranscript(ctx, orchestrator.RecordShellTranscriptRequest{
			TaskID:    string(p.TaskID),
			SessionID: p.SessionID,
			Chunks:    chunks,
		})
		if err != nil {
			return respondErr("SHELL_TRANSCRIPT_APPEND_FAILED", err.Error())
		}
		return respondOK(ipc.TaskShellTranscriptAppendResponse{
			TaskID:         out.TaskID,
			SessionID:      out.SessionID,
			RetainedChunks: out.Summary.RetainedChunks,
			DroppedChunks:  out.Summary.DroppedChunks,
			RetentionLimit: out.Summary.RetentionLimit,
			LastSequenceNo: out.Summary.LastSequenceNo,
			LastChunkAt:    out.Summary.LastChunkAt,
		})
	case ipc.MethodTaskShellTranscriptRead:
		var p ipc.TaskShellTranscriptReadRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ReadShellTranscript(ctx, orchestrator.ReadShellTranscriptRequest{
			TaskID:         string(p.TaskID),
			SessionID:      p.SessionID,
			Limit:          p.Limit,
			BeforeSequence: p.BeforeSequence,
			Source:         p.Source,
		})
		if err != nil {
			return respondErr("SHELL_TRANSCRIPT_READ_FAILED", err.Error())
		}
		resp := ipc.TaskShellTranscriptReadResponse{
			TaskID:                 out.TaskID,
			SessionID:              out.SessionID,
			TranscriptState:        string(out.TranscriptState),
			TranscriptOnly:         out.TranscriptOnly,
			Bounded:                out.Bounded,
			Partial:                out.Partial,
			RetentionLimit:         out.RetentionLimit,
			RetainedChunkCount:     out.RetainedChunkCount,
			DroppedChunkCount:      out.DroppedChunkCount,
			LastSequence:           out.LastSequence,
			LastChunkAt:            out.LastChunkAt,
			OldestRetainedSequence: out.OldestRetainedSequence,
			NewestRetainedSequence: out.NewestRetainedSequence,
			OldestRetainedChunkAt:  out.OldestRetainedChunkAt,
			NewestRetainedChunkAt:  out.NewestRetainedChunkAt,
			RequestedLimit:         out.RequestedLimit,
			RequestedSource:        string(out.RequestedSource),
			PageOldestSequence:     out.PageOldestSequence,
			PageNewestSequence:     out.PageNewestSequence,
			PageChunkCount:         out.PageChunkCount,
			HasMoreOlder:           out.HasMoreOlder,
			HasUnreadNewerEvidence: out.HasUnreadNewerEvidence,
			PageFullyReviewed:      out.PageFullyReviewed,
			PageCrossesReview:      out.PageCrossesReview,
			PageHasUnreviewed:      out.PageHasUnreviewed,
			Closure:                ipcTranscriptReviewClosure(out.Closure),
		}
		if out.RequestedBeforeSequence != nil {
			resp.RequestedBeforeSequence = *out.RequestedBeforeSequence
		}
		if out.NextBeforeSequence != nil {
			resp.NextBeforeSequence = *out.NextBeforeSequence
		}
		if out.LatestReview != nil {
			marker := ipcTranscriptReviewMarker(*out.LatestReview)
			resp.LatestReview = &marker
		}
		if len(out.SourceSummary) > 0 {
			resp.SourceSummary = make([]ipc.TaskShellTranscriptSourceCount, 0, len(out.SourceSummary))
			for _, source := range out.SourceSummary {
				resp.SourceSummary = append(resp.SourceSummary, ipc.TaskShellTranscriptSourceCount{
					Source: string(source.Source),
					Chunks: source.Chunks,
				})
			}
		}
		if len(out.Chunks) > 0 {
			resp.Chunks = make([]ipc.TaskShellTranscriptChunk, 0, len(out.Chunks))
			for _, chunk := range out.Chunks {
				resp.Chunks = append(resp.Chunks, ipc.TaskShellTranscriptChunk{
					ChunkID:    chunk.ChunkID,
					TaskID:     chunk.TaskID,
					SessionID:  chunk.SessionID,
					SequenceNo: chunk.SequenceNo,
					Source:     string(chunk.Source),
					Content:    chunk.Content,
					CreatedAt:  chunk.CreatedAt,
				})
			}
		}
		return respondOK(resp)
	case ipc.MethodTaskShellTranscriptReview:
		var p ipc.TaskShellTranscriptReviewRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.RecordShellTranscriptReview(ctx, orchestrator.RecordShellTranscriptReviewRequest{
			TaskID:          string(p.TaskID),
			SessionID:       p.SessionID,
			ReviewedUpToSeq: p.ReviewedUpToSeq,
			Source:          p.Source,
			Summary:         p.Summary,
		})
		if err != nil {
			return respondErr("SHELL_TRANSCRIPT_REVIEW_FAILED", err.Error())
		}
		return respondOK(ipc.TaskShellTranscriptReviewResponse{
			TaskID:                 out.TaskID,
			SessionID:              out.SessionID,
			TranscriptState:        string(out.TranscriptState),
			RetentionLimit:         out.RetentionLimit,
			RetainedChunkCount:     out.RetainedChunkCount,
			DroppedChunkCount:      out.DroppedChunkCount,
			OldestRetainedSequence: out.OldestRetainedSequence,
			NewestRetainedSequence: out.NewestRetainedSequence,
			LatestReview:           ipcTranscriptReviewMarker(out.LatestReview),
			HasUnreadNewerEvidence: out.HasUnreadNewerEvidence,
			Closure:                ipcTranscriptReviewClosure(out.Closure),
		})
	case ipc.MethodTaskShellTranscriptHistory:
		var p ipc.TaskShellTranscriptHistoryRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ReadShellTranscriptReviewHistory(ctx, orchestrator.ReadShellTranscriptReviewHistoryRequest{
			TaskID:    string(p.TaskID),
			SessionID: p.SessionID,
			Source:    p.Source,
			Limit:     p.Limit,
		})
		if err != nil {
			return respondErr("SHELL_TRANSCRIPT_HISTORY_FAILED", err.Error())
		}
		resp := ipc.TaskShellTranscriptHistoryResponse{
			TaskID:                 out.TaskID,
			SessionID:              out.SessionID,
			TranscriptState:        string(out.TranscriptState),
			TranscriptOnly:         out.TranscriptOnly,
			Bounded:                out.Bounded,
			Partial:                out.Partial,
			RetentionLimit:         out.RetentionLimit,
			RetainedChunkCount:     out.RetainedChunkCount,
			DroppedChunkCount:      out.DroppedChunkCount,
			OldestRetainedSequence: out.OldestRetainedSequence,
			NewestRetainedSequence: out.NewestRetainedSequence,
			RequestedLimit:         out.RequestedLimit,
			RequestedSource:        string(out.RequestedSource),
			Closure:                ipcTranscriptReviewClosure(out.Closure),
		}
		if out.LatestReview != nil {
			latest := ipcTranscriptReviewMarker(*out.LatestReview)
			resp.LatestReview = &latest
		}
		if len(out.Reviews) > 0 {
			resp.Reviews = make([]ipc.TaskShellTranscriptReviewMarker, 0, len(out.Reviews))
			for _, review := range out.Reviews {
				resp.Reviews = append(resp.Reviews, ipcTranscriptReviewMarker(review))
			}
		}
		return respondOK(resp)
	case ipc.MethodTaskTransitionHistory:
		var p ipc.TaskTransitionHistoryRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ReadContinuityTransitionHistory(ctx, orchestrator.ReadContinuityTransitionHistoryRequest{
			TaskID:          string(p.TaskID),
			Limit:           p.Limit,
			BeforeReceiptID: string(p.BeforeReceiptID),
			TransitionKind:  p.TransitionKind,
			HandoffID:       p.HandoffID,
		})
		if err != nil {
			return respondErr("TRANSITION_HISTORY_FAILED", err.Error())
		}
		resp := ipc.TaskTransitionHistoryResponse{
			TaskID:                   out.TaskID,
			Bounded:                  out.Bounded,
			RequestedLimit:           out.RequestedLimit,
			RequestedBeforeReceiptID: out.RequestedBeforeReceiptID,
			RequestedTransitionKind:  string(out.RequestedTransitionKind),
			RequestedHandoffID:       out.RequestedHandoffID,
			HasMoreOlder:             out.HasMoreOlder,
			NextBeforeReceiptID:      out.NextBeforeReceiptID,
			RiskSummary:              *ipcContinuityTransitionRiskSummary(&out.RiskSummary),
		}
		resp.Latest = ipcContinuityTransitionReceipt(out.Latest)
		resp.Receipts = ipcContinuityTransitionReceipts(out.Receipts)
		return respondOK(resp)
	case ipc.MethodTaskContinuityIncidentSlice:
		var p ipc.TaskContinuityIncidentSliceRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ReadContinuityIncidentSlice(ctx, orchestrator.ReadContinuityIncidentSliceRequest{
			TaskID:                    string(p.TaskID),
			AnchorTransitionReceiptID: string(p.AnchorTransitionReceiptID),
			TransitionNeighborLimit:   p.TransitionNeighborLimit,
			RunLimit:                  p.RunLimit,
			RecoveryLimit:             p.RecoveryLimit,
			ProofLimit:                p.ProofLimit,
			AckLimit:                  p.AckLimit,
		})
		if err != nil {
			return respondErr("CONTINUITY_INCIDENT_SLICE_FAILED", err.Error())
		}
		resp := ipc.TaskContinuityIncidentSliceResponse{
			TaskID:                             out.TaskID,
			Bounded:                            out.Bounded,
			AnchorMode:                         string(out.AnchorMode),
			RequestedAnchorTransitionReceiptID: out.RequestedAnchorTransitionReceiptID,
			Anchor:                             *ipcContinuityTransitionReceipt(&out.Anchor),
			TransitionNeighborLimit:            out.TransitionNeighborLimit,
			RunLimit:                           out.RunLimit,
			RecoveryLimit:                      out.RecoveryLimit,
			ProofLimit:                         out.ProofLimit,
			AckLimit:                           out.AckLimit,
			HasOlderTransitionsOutsideWindow:   out.HasOlderTransitionsOutsideWindow,
			HasNewerTransitionsOutsideWindow:   out.HasNewerTransitionsOutsideWindow,
			WindowStartAt:                      out.WindowStartAt,
			WindowEndAt:                        out.WindowEndAt,
			Transitions:                        ipcContinuityTransitionReceipts(out.Transitions),
			Runs:                               ipcContinuityIncidentRuns(out.Runs),
			ProofEvents:                        ipcContinuityIncidentProofEvents(out.ProofEvents),
			LatestTranscriptReviewGapAck:       ipcTranscriptReviewGapAcknowledgment(out.LatestTranscriptReviewGapAck),
			RecentTranscriptReviewGapAcks:      ipcTranscriptReviewGapAcknowledgments(out.RecentTranscriptReviewGapAcks),
			RiskSummary:                        *ipcContinuityIncidentRiskSummary(&out.RiskSummary),
			Caveat:                             out.Caveat,
		}
		if out.LatestTranscriptReview != nil {
			review := ipcTranscriptReviewMarker(*out.LatestTranscriptReview)
			resp.LatestTranscriptReview = &review
		}
		if len(out.RecoveryActions) > 0 {
			resp.RecoveryActions = make([]ipc.TaskRecoveryActionRecord, 0, len(out.RecoveryActions))
			for _, action := range out.RecoveryActions {
				record := recoveryaction.Record{
					ActionID:        action.ActionID,
					TaskID:          out.TaskID,
					Kind:            action.Kind,
					RunID:           action.RunID,
					CheckpointID:    action.CheckpointID,
					HandoffID:       action.HandoffID,
					LaunchAttemptID: action.LaunchAttemptID,
					Summary:         action.Summary,
					CreatedAt:       action.CreatedAt,
				}
				if mapped := ipcRecoveryActionRecord(&record); mapped != nil {
					resp.RecoveryActions = append(resp.RecoveryActions, *mapped)
				}
			}
		}
		return respondOK(resp)
	case ipc.MethodTaskContinuityIncidentTriage:
		var p ipc.TaskContinuityIncidentTriageRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.RecordContinuityIncidentTriage(ctx, orchestrator.RecordContinuityIncidentTriageRequest{
			TaskID:                    string(p.TaskID),
			AnchorMode:                p.AnchorMode,
			AnchorTransitionReceiptID: string(p.AnchorTransitionReceiptID),
			Posture:                   p.Posture,
			Summary:                   p.Summary,
		})
		if err != nil {
			return respondErr("CONTINUITY_INCIDENT_TRIAGE_FAILED", err.Error())
		}
		receipt := ipcContinuityIncidentTriageReceipt(&out.Receipt)
		if receipt == nil {
			return respondErr("CONTINUITY_INCIDENT_TRIAGE_FAILED", "missing continuity incident triage receipt")
		}
		return respondOK(ipc.TaskContinuityIncidentTriageResponse{
			TaskID:                            out.TaskID,
			AnchorMode:                        string(out.AnchorMode),
			AnchorTransitionReceiptID:         out.AnchorTransitionReceiptID,
			Posture:                           string(out.Posture),
			Reused:                            out.Reused,
			Receipt:                           *receipt,
			LatestContinuityTransitionReceipt: ipcContinuityTransitionReceipt(out.LatestContinuityTransition),
			RecentContinuityIncidentTriages:   ipcContinuityIncidentTriageReceipts(out.RecentContinuityIncidentTriages),
			ContinuityIncidentFollowUp:        ipcContinuityIncidentFollowUpSummary(out.FollowUp),
		})
	case ipc.MethodTaskContinuityIncidentTriageHistory:
		var p ipc.TaskContinuityIncidentTriageHistoryRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ReadContinuityIncidentTriageHistory(ctx, orchestrator.ReadContinuityIncidentTriageHistoryRequest{
			TaskID:                    string(p.TaskID),
			Limit:                     p.Limit,
			BeforeReceiptID:           string(p.BeforeReceiptID),
			AnchorTransitionReceiptID: string(p.AnchorTransitionReceiptID),
			Posture:                   p.Posture,
		})
		if err != nil {
			return respondErr("CONTINUITY_INCIDENT_TRIAGE_HISTORY_FAILED", err.Error())
		}
		resp := ipc.TaskContinuityIncidentTriageHistoryResponse{
			TaskID:                             out.TaskID,
			Bounded:                            out.Bounded,
			RequestedLimit:                     out.RequestedLimit,
			RequestedBeforeReceiptID:           out.RequestedBeforeReceiptID,
			RequestedAnchorTransitionReceiptID: out.RequestedAnchorTransitionReceiptID,
			RequestedPosture:                   string(out.RequestedPosture),
			HasMoreOlder:                       out.HasMoreOlder,
			NextBeforeReceiptID:                out.NextBeforeReceiptID,
			LatestTransitionReceiptID:          out.LatestTransitionReceiptID,
			Rollup:                             *ipcContinuityIncidentTriageHistoryRollup(&out.Rollup),
		}
		resp.Latest = ipcContinuityIncidentTriageReceipt(out.Latest)
		resp.Receipts = ipcContinuityIncidentTriageReceipts(out.Receipts)
		return respondOK(resp)
	case ipc.MethodTaskContinuityIncidentFollowUp:
		var p ipc.TaskContinuityIncidentFollowUpRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.RecordContinuityIncidentFollowUp(ctx, orchestrator.RecordContinuityIncidentFollowUpRequest{
			TaskID:                    string(p.TaskID),
			AnchorMode:                p.AnchorMode,
			AnchorTransitionReceiptID: string(p.AnchorTransitionReceiptID),
			TriageReceiptID:           string(p.TriageReceiptID),
			ActionKind:                p.ActionKind,
			Summary:                   p.Summary,
		})
		if err != nil {
			return respondErr("CONTINUITY_INCIDENT_FOLLOW_UP_FAILED", err.Error())
		}
		receipt := ipcContinuityIncidentFollowUpReceipt(&out.Receipt)
		if receipt == nil {
			return respondErr("CONTINUITY_INCIDENT_FOLLOW_UP_FAILED", "missing continuity incident follow-up receipt")
		}
		return respondOK(ipc.TaskContinuityIncidentFollowUpResponse{
			TaskID:                                  out.TaskID,
			AnchorMode:                              string(out.AnchorMode),
			AnchorTransitionReceiptID:               out.AnchorTransitionReceiptID,
			TriageReceiptID:                         out.TriageReceiptID,
			ActionKind:                              string(out.ActionKind),
			Reused:                                  out.Reused,
			Receipt:                                 *receipt,
			LatestContinuityTransitionReceipt:       ipcContinuityTransitionReceipt(out.LatestContinuityTransition),
			LatestContinuityIncidentTriageReceipt:   ipcContinuityIncidentTriageReceipt(out.LatestContinuityIncidentTriage),
			RecentContinuityIncidentTriages:         ipcContinuityIncidentTriageReceipts(out.RecentContinuityIncidentTriages),
			LatestContinuityIncidentFollowUpReceipt: ipcContinuityIncidentFollowUpReceipt(out.LatestContinuityIncidentFollowUp),
			RecentContinuityIncidentFollowUps:       ipcContinuityIncidentFollowUpReceipts(out.RecentContinuityIncidentFollowUps),
			ContinuityIncidentFollowUpHistoryRollup: ipcContinuityIncidentFollowUpHistoryRollup(out.ContinuityIncidentFollowUpHistoryRollup),
			ContinuityIncidentFollowUp:              ipcContinuityIncidentFollowUpSummary(out.FollowUp),
		})
	case ipc.MethodTaskContinuityIncidentFollowUpHistory:
		var p ipc.TaskContinuityIncidentFollowUpHistoryRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ReadContinuityIncidentFollowUpHistory(ctx, orchestrator.ReadContinuityIncidentFollowUpHistoryRequest{
			TaskID:                    string(p.TaskID),
			Limit:                     p.Limit,
			BeforeReceiptID:           string(p.BeforeReceiptID),
			AnchorTransitionReceiptID: string(p.AnchorTransitionReceiptID),
			TriageReceiptID:           string(p.TriageReceiptID),
			ActionKind:                p.ActionKind,
		})
		if err != nil {
			return respondErr("CONTINUITY_INCIDENT_FOLLOW_UP_HISTORY_FAILED", err.Error())
		}
		resp := ipc.TaskContinuityIncidentFollowUpHistoryResponse{
			TaskID:                             out.TaskID,
			Bounded:                            out.Bounded,
			RequestedLimit:                     out.RequestedLimit,
			RequestedBeforeReceiptID:           out.RequestedBeforeReceiptID,
			RequestedAnchorTransitionReceiptID: out.RequestedAnchorTransitionReceiptID,
			RequestedTriageReceiptID:           out.RequestedTriageReceiptID,
			RequestedActionKind:                string(out.RequestedActionKind),
			HasMoreOlder:                       out.HasMoreOlder,
			NextBeforeReceiptID:                out.NextBeforeReceiptID,
			LatestTransitionReceiptID:          out.LatestTransitionReceiptID,
			Rollup:                             *ipcContinuityIncidentFollowUpHistoryRollup(&out.Rollup),
		}
		resp.Latest = ipcContinuityIncidentFollowUpReceipt(out.Latest)
		resp.Receipts = ipcContinuityIncidentFollowUpReceipts(out.Receipts)
		return respondOK(resp)
	case ipc.MethodTaskContinuityIncidentClosure:
		var p ipc.TaskContinuityIncidentClosureRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ReadContinuityIncidentClosure(ctx, orchestrator.ReadContinuityIncidentClosureRequest{
			TaskID:          string(p.TaskID),
			Limit:           p.Limit,
			BeforeReceiptID: string(p.BeforeReceiptID),
		})
		if err != nil {
			return respondErr("CONTINUITY_INCIDENT_CLOSURE_FAILED", err.Error())
		}
		resp := ipc.TaskContinuityIncidentClosureResponse{
			TaskID:                    out.TaskID,
			Bounded:                   out.Bounded,
			RequestedLimit:            out.RequestedLimit,
			RequestedBeforeReceiptID:  out.RequestedBeforeReceiptID,
			HasMoreOlder:              out.HasMoreOlder,
			NextBeforeReceiptID:       out.NextBeforeReceiptID,
			LatestTransitionReceiptID: out.LatestTransitionReceiptID,
			Rollup:                    *ipcContinuityIncidentFollowUpHistoryRollup(&out.Rollup),
			FollowUp:                  ipcContinuityIncidentFollowUpSummary(out.FollowUp),
			Closure:                   ipcContinuityIncidentClosureSummary(out.Closure),
		}
		resp.Latest = ipcContinuityIncidentFollowUpReceipt(out.Latest)
		resp.Receipts = ipcContinuityIncidentFollowUpReceipts(out.Receipts)
		return respondOK(resp)
	case ipc.MethodTaskContinuityIncidentRisk:
		var p ipc.TaskContinuityIncidentTaskRiskRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ReadContinuityIncidentTaskRisk(ctx, orchestrator.ReadContinuityIncidentTaskRiskRequest{
			TaskID:          string(p.TaskID),
			Limit:           p.Limit,
			BeforeReceiptID: string(p.BeforeReceiptID),
		})
		if err != nil {
			return respondErr("CONTINUITY_INCIDENT_TASK_RISK_FAILED", err.Error())
		}
		return respondOK(ipc.TaskContinuityIncidentTaskRiskResponse{
			TaskID:                   out.TaskID,
			Bounded:                  out.Bounded,
			RequestedLimit:           out.RequestedLimit,
			RequestedBeforeReceiptID: out.RequestedBeforeReceiptID,
			HasMoreOlder:             out.HasMoreOlder,
			NextBeforeReceiptID:      out.NextBeforeReceiptID,
			Summary:                  ipcContinuityIncidentTaskRiskSummary(out.Summary),
			Closure:                  ipcContinuityIncidentClosureSummary(out.Closure),
		})
	case ipc.MethodTaskShellSessionReport:
		var p ipc.TaskShellSessionReportRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ReportShellSession(ctx, orchestrator.ReportShellSessionRequest{
			TaskID:                string(p.TaskID),
			SessionID:             p.SessionID,
			WorkerPreference:      p.WorkerPreference,
			ResolvedWorker:        p.ResolvedWorker,
			WorkerSessionID:       p.WorkerSessionID,
			WorkerSessionIDSource: shellsession.WorkerSessionIDSource(p.WorkerSessionIDSource),
			AttachCapability:      shellsession.AttachCapability(p.AttachCapability),
			HostMode:              p.HostMode,
			HostState:             p.HostState,
			StartedAt:             p.StartedAt,
			Active:                p.Active,
			Note:                  p.Note,
		})
		if err != nil {
			return respondErr("SHELL_SESSION_REPORT_FAILED", err.Error())
		}
		return respondOK(ipc.TaskShellSessionReportResponse{
			TaskID:  out.TaskID,
			Session: ipcTaskShellSessionRecord(out.Session),
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
				resp.Sessions = append(resp.Sessions, ipcTaskShellSessionRecord(session))
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
			TaskID:              out.TaskID,
			HandoffID:           out.HandoffID,
			TargetWorker:        out.TargetWorker,
			LaunchStatus:        string(out.LaunchStatus),
			LaunchID:            out.LaunchID,
			TransitionReceiptID: out.TransitionReceiptID,
			CanonicalResponse:   out.CanonicalResponse,
			Payload:             out.Payload,
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
			TransitionReceiptID:   out.TransitionReceiptID,
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
