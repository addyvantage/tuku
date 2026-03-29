package orchestrator

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"tuku/internal/domain/brief"
	"tuku/internal/domain/capsule"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/conversation"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/intent"
	"tuku/internal/domain/operatorstep"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/recoveryaction"
	rundomain "tuku/internal/domain/run"
	anchorgit "tuku/internal/git/anchor"
)

type ShellSnapshotResult struct {
	TaskID                                   common.TaskID
	Goal                                     string
	Phase                                    string
	Status                                   string
	RepoAnchor                               anchorgit.Snapshot
	IntentClass                              string
	IntentSummary                            string
	CompiledIntent                           *CompiledIntentSummary
	Brief                                    *ShellBriefSummary
	Run                                      *ShellRunSummary
	Checkpoint                               *ShellCheckpointSummary
	Handoff                                  *ShellHandoffSummary
	Launch                                   *ShellLaunchSummary
	LaunchControl                            *ShellLaunchControlSummary
	Acknowledgment                           *ShellAcknowledgmentSummary
	FollowThrough                            *ShellFollowThroughSummary
	Resolution                               *ShellResolutionSummary
	ActiveBranch                             *ShellActiveBranchSummary
	LocalRunFinalization                     *ShellLocalRunFinalizationSummary
	LocalResume                              *ShellLocalResumeAuthoritySummary
	ActionAuthority                          *ShellOperatorActionAuthoritySet
	OperatorDecision                         *ShellOperatorDecisionSummary
	OperatorExecutionPlan                    *ShellOperatorExecutionPlan
	LatestOperatorStepReceipt                *operatorstep.Receipt
	RecentOperatorStepReceipts               []operatorstep.Receipt
	LatestContinuityTransitionReceipt        *ContinuityTransitionReceiptSummary
	RecentContinuityTransitionReceipts       []ContinuityTransitionReceiptSummary
	ContinuityTransitionRiskSummary          *ContinuityTransitionRiskSummary
	ContinuityIncidentSummary                *ContinuityIncidentRiskSummary
	LatestContinuityIncidentTriageReceipt    *ContinuityIncidentTriageReceiptSummary
	RecentContinuityIncidentTriageReceipts   []ContinuityIncidentTriageReceiptSummary
	ContinuityIncidentTriageHistoryRollup    *ContinuityIncidentTriageHistoryRollupSummary
	LatestContinuityIncidentFollowUpReceipt  *ContinuityIncidentFollowUpReceiptSummary
	RecentContinuityIncidentFollowUpReceipts []ContinuityIncidentFollowUpReceiptSummary
	ContinuityIncidentFollowUpHistoryRollup  *ContinuityIncidentFollowUpHistoryRollupSummary
	ContinuityIncidentFollowUp               *ContinuityIncidentFollowUpSummary
	ContinuityIncidentTaskRisk               *ContinuityIncidentTaskRiskSummary
	LatestTranscriptReviewGapAcknowledgment  *TranscriptReviewGapAcknowledgmentSummary
	RecentTranscriptReviewGapAcknowledgments []TranscriptReviewGapAcknowledgmentSummary
	HandoffContinuity                        *ShellHandoffContinuitySummary
	Recovery                                 *ShellRecoverySummary
	ShellSessions                            []ShellSessionView
	RecentShellEvents                        []ShellSessionEventSummary
	RecentShellTranscript                    []ShellTranscriptChunkSummary
	RecentProofs                             []ShellProofSummary
	RecentConversation                       []ShellConversationSummary
	LatestCanonicalResponse                  string
}

type ShellBriefSummary struct {
	BriefID                 common.BriefID
	Posture                 brief.Posture
	Objective               string
	RequestedOutcome        string
	NormalizedAction        string
	ScopeSummary            string
	Constraints             []string
	DoneCriteria            []string
	AmbiguityFlags          []string
	ClarificationQuestions  []string
	RequiresClarification   bool
	WorkerFraming           string
	BoundedEvidenceMessages int
}

type ShellRunSummary struct {
	RunID              common.RunID
	WorkerKind         rundomain.WorkerKind
	Status             rundomain.Status
	WorkerRunID        string
	ShellSessionID     string
	Command            string
	Args               []string
	ExitCode           *int
	Stdout             string
	Stderr             string
	ChangedFiles       []string
	ValidationSignals  []string
	OutputArtifactRef  string
	StructuredSummary  string
	LastKnownSummary   string
	StartedAt          time.Time
	EndedAt            *time.Time
	InterruptionReason string
}

type ShellSessionEventSummary struct {
	EventID               common.EventID
	SessionID             string
	Kind                  string
	HostMode              string
	HostState             string
	WorkerSessionID       string
	WorkerSessionIDSource string
	AttachCapability      string
	Active                bool
	InputLive             bool
	ExitCode              *int
	PaneWidth             int
	PaneHeight            int
	Note                  string
	CreatedAt             time.Time
}

type ShellTranscriptChunkSummary struct {
	ChunkID    common.EventID
	SessionID  string
	SequenceNo int64
	Source     string
	Content    string
	CreatedAt  time.Time
}

type ShellCheckpointSummary struct {
	CheckpointID     common.CheckpointID
	Trigger          checkpoint.Trigger
	CreatedAt        time.Time
	ResumeDescriptor string
	IsResumable      bool
}

type ShellHandoffSummary struct {
	HandoffID    string
	Status       handoff.Status
	SourceWorker rundomain.WorkerKind
	TargetWorker rundomain.WorkerKind
	Mode         handoff.Mode
	Reason       string
	AcceptedBy   rundomain.WorkerKind
	CreatedAt    time.Time
}

type ShellLaunchSummary struct {
	AttemptID         string
	LaunchID          string
	Status            handoff.LaunchStatus
	RequestedAt       time.Time
	StartedAt         time.Time
	EndedAt           time.Time
	Summary           string
	ErrorMessage      string
	OutputArtifactRef string
}

type ShellLaunchControlSummary struct {
	State            LaunchControlState
	RetryDisposition LaunchRetryDisposition
	Reason           string
	HandoffID        string
	AttemptID        string
	LaunchID         string
	TargetWorker     rundomain.WorkerKind
	RequestedAt      time.Time
	CompletedAt      time.Time
	FailedAt         time.Time
}

type ShellAcknowledgmentSummary struct {
	Status    handoff.AcknowledgmentStatus
	Summary   string
	CreatedAt time.Time
}

type ShellFollowThroughSummary struct {
	RecordID        string
	Kind            handoff.FollowThroughKind
	Summary         string
	LaunchAttemptID string
	LaunchID        string
	CreatedAt       time.Time
}

type ShellResolutionSummary struct {
	ResolutionID    string
	Kind            handoff.ResolutionKind
	Summary         string
	LaunchAttemptID string
	LaunchID        string
	CreatedAt       time.Time
}

type ShellActiveBranchSummary struct {
	Class                  ActiveBranchClass
	BranchRef              string
	ActionabilityAnchor    ActiveBranchAnchorKind
	ActionabilityAnchorRef string
	Reason                 string
}

type ShellLocalRunFinalizationSummary struct {
	State        LocalRunFinalizationState
	RunID        common.RunID
	RunStatus    rundomain.Status
	CheckpointID common.CheckpointID
	Reason       string
}

type ShellLocalResumeAuthoritySummary struct {
	State               LocalResumeAuthorityState
	Mode                LocalResumeMode
	CheckpointID        common.CheckpointID
	RunID               common.RunID
	BlockingBranchClass ActiveBranchClass
	BlockingBranchRef   string
	Reason              string
}

type ShellOperatorActionAuthority struct {
	Action              OperatorAction
	State               OperatorActionAuthorityState
	Reason              string
	BlockingBranchClass ActiveBranchClass
	BlockingBranchRef   string
	AnchorKind          ActiveBranchAnchorKind
	AnchorRef           string
}

type ShellOperatorActionAuthoritySet struct {
	RequiredNextAction OperatorAction
	Actions            []ShellOperatorActionAuthority
}

type ShellOperatorDecisionBlockedAction struct {
	Action OperatorAction
	Reason string
}

type ShellOperatorDecisionSummary struct {
	ActiveOwnerClass   ActiveBranchClass
	ActiveOwnerRef     string
	Headline           string
	RequiredNextAction OperatorAction
	PrimaryReason      string
	Guidance           string
	IntegrityNote      string
	BlockedActions     []ShellOperatorDecisionBlockedAction
}

type ShellOperatorExecutionStep struct {
	Action         OperatorAction
	Status         OperatorActionAuthorityState
	Domain         OperatorExecutionDomain
	CommandSurface OperatorCommandSurfaceType
	CommandHint    string
	Reason         string
}

type ShellOperatorExecutionPlan struct {
	PrimaryStep             *ShellOperatorExecutionStep
	MandatoryBeforeProgress bool
	SecondarySteps          []ShellOperatorExecutionStep
	BlockedSteps            []ShellOperatorExecutionStep
}

type ShellHandoffContinuitySummary struct {
	State                        HandoffContinuityState
	Reason                       string
	LaunchAttemptID              string
	LaunchID                     string
	AcknowledgmentID             string
	AcknowledgmentStatus         handoff.AcknowledgmentStatus
	AcknowledgmentSummary        string
	FollowThroughID              string
	FollowThroughKind            handoff.FollowThroughKind
	FollowThroughSummary         string
	ResolutionID                 string
	ResolutionKind               handoff.ResolutionKind
	ResolutionSummary            string
	DownstreamContinuationProven bool
}

type ShellRecoveryIssue struct {
	Code    string
	Message string
}

type ShellRecoverySummary struct {
	ContinuityOutcome      ContinueOutcome
	RecoveryClass          RecoveryClass
	RecommendedAction      RecoveryAction
	ReadyForNextRun        bool
	ReadyForHandoffLaunch  bool
	RequiresDecision       bool
	RequiresRepair         bool
	RequiresReview         bool
	RequiresReconciliation bool
	DriftClass             checkpoint.DriftClass
	Reason                 string
	CheckpointID           common.CheckpointID
	RunID                  common.RunID
	HandoffID              string
	HandoffStatus          handoff.Status
	Issues                 []ShellRecoveryIssue
}

type ShellProofSummary struct {
	EventID   common.EventID
	Type      proof.EventType
	Summary   string
	Timestamp time.Time
}

type ShellConversationSummary struct {
	Role      conversation.Role
	Body      string
	CreatedAt time.Time
}

func (c *Coordinator) ShellSnapshotTask(ctx context.Context, taskID string) (ShellSnapshotResult, error) {
	id := common.TaskID(strings.TrimSpace(taskID))
	if id == "" {
		return ShellSnapshotResult{}, fmt.Errorf("task id is required")
	}

	caps, err := c.store.Capsules().Get(id)
	if err != nil {
		return ShellSnapshotResult{}, err
	}

	result := ShellSnapshotResult{
		TaskID:     caps.TaskID,
		Goal:       caps.Goal,
		Phase:      string(caps.CurrentPhase),
		Status:     caps.Status,
		RepoAnchor: capsuleAnchorSnapshot(caps),
	}

	if st, ok, err := c.shellIntent(id, caps.CurrentIntentID); err != nil {
		return ShellSnapshotResult{}, err
	} else if ok {
		result.IntentClass = string(st.Class)
		result.IntentSummary = shellIntentSummary(st)
		result.CompiledIntent = compiledIntentSummaryFromState(st)
	}

	if b, ok, err := c.shellBrief(id, caps.CurrentBriefID); err != nil {
		return ShellSnapshotResult{}, err
	} else if ok {
		result.Brief = &ShellBriefSummary{
			BriefID:                 b.BriefID,
			Posture:                 b.Posture,
			Objective:               b.Objective,
			RequestedOutcome:        b.RequestedOutcome,
			NormalizedAction:        b.NormalizedAction,
			ScopeSummary:            b.ScopeSummary,
			Constraints:             append([]string{}, b.Constraints...),
			DoneCriteria:            append([]string{}, b.DoneCriteria...),
			AmbiguityFlags:          append([]string{}, b.AmbiguityFlags...),
			ClarificationQuestions:  append([]string{}, b.ClarificationQuestions...),
			RequiresClarification:   b.RequiresClarification,
			WorkerFraming:           b.WorkerFraming,
			BoundedEvidenceMessages: b.BoundedEvidenceMessages,
		}
	}

	var latestRunForIncident *rundomain.ExecutionRun
	if runRec, err := c.store.Runs().LatestByTask(id); err != nil {
		if err != sql.ErrNoRows {
			return ShellSnapshotResult{}, err
		}
	} else {
		runCopy := runRec
		latestRunForIncident = &runCopy
		result.Run = &ShellRunSummary{
			RunID:              runRec.RunID,
			WorkerKind:         runRec.WorkerKind,
			Status:             runRec.Status,
			WorkerRunID:        runRec.WorkerRunID,
			ShellSessionID:     runRec.ShellSessionID,
			Command:            runRec.Command,
			Args:               append([]string{}, runRec.Args...),
			ExitCode:           runRec.ExitCode,
			Stdout:             runRec.Stdout,
			Stderr:             runRec.Stderr,
			ChangedFiles:       append([]string{}, runRec.ChangedFiles...),
			ValidationSignals:  append([]string{}, runRec.ValidationSignals...),
			OutputArtifactRef:  runRec.OutputArtifactRef,
			StructuredSummary:  runRec.StructuredSummary,
			LastKnownSummary:   runRec.LastKnownSummary,
			StartedAt:          runRec.StartedAt,
			EndedAt:            runRec.EndedAt,
			InterruptionReason: runRec.InterruptionReason,
		}
	}
	if sessions, err := c.classifiedShellSessions(id); err != nil {
		return ShellSnapshotResult{}, err
	} else if len(sessions) > 0 {
		result.ShellSessions = sessions
		latestSessionID := sessions[0].SessionID
		latestUpdated := sessions[0].LastUpdatedAt
		for _, session := range sessions[1:] {
			if session.LastUpdatedAt.After(latestUpdated) {
				latestSessionID = session.SessionID
				latestUpdated = session.LastUpdatedAt
			}
		}
		if transcript, err := c.listShellTranscript(id, latestSessionID, 40); err != nil {
			return ShellSnapshotResult{}, err
		} else if len(transcript) > 0 {
			result.RecentShellTranscript = make([]ShellTranscriptChunkSummary, 0, len(transcript))
			for _, chunk := range transcript {
				result.RecentShellTranscript = append(result.RecentShellTranscript, ShellTranscriptChunkSummary{
					ChunkID:    chunk.ChunkID,
					SessionID:  chunk.SessionID,
					SequenceNo: chunk.SequenceNo,
					Source:     string(chunk.Source),
					Content:    chunk.Content,
					CreatedAt:  chunk.CreatedAt,
				})
			}
		}
	}
	if events, err := c.listShellSessionEvents(id, "", 20); err != nil {
		return ShellSnapshotResult{}, err
	} else if len(events) > 0 {
		result.RecentShellEvents = make([]ShellSessionEventSummary, 0, len(events))
		for _, event := range events {
			result.RecentShellEvents = append(result.RecentShellEvents, ShellSessionEventSummary{
				EventID:               event.EventID,
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

	if cp, err := c.store.Checkpoints().LatestByTask(id); err != nil {
		if err != sql.ErrNoRows {
			return ShellSnapshotResult{}, err
		}
	} else {
		result.Checkpoint = &ShellCheckpointSummary{
			CheckpointID:     cp.CheckpointID,
			Trigger:          cp.Trigger,
			CreatedAt:        cp.CreatedAt,
			ResumeDescriptor: cp.ResumeDescriptor,
			IsResumable:      cp.IsResumable,
		}
	}

	if packet, err := c.store.Handoffs().LatestByTask(id); err != nil {
		if err != sql.ErrNoRows {
			return ShellSnapshotResult{}, err
		}
	} else {
		result.Handoff = &ShellHandoffSummary{
			HandoffID:    packet.HandoffID,
			Status:       packet.Status,
			SourceWorker: packet.SourceWorker,
			TargetWorker: packet.TargetWorker,
			Mode:         packet.HandoffMode,
			Reason:       packet.Reason,
			AcceptedBy:   packet.AcceptedBy,
			CreatedAt:    packet.CreatedAt,
		}
		if latestLaunch, err := c.store.Handoffs().LatestLaunchByHandoff(packet.HandoffID); err != nil {
			if err != sql.ErrNoRows {
				return ShellSnapshotResult{}, err
			}
		} else {
			result.Launch = &ShellLaunchSummary{
				AttemptID:         latestLaunch.AttemptID,
				LaunchID:          latestLaunch.LaunchID,
				Status:            latestLaunch.Status,
				RequestedAt:       latestLaunch.RequestedAt,
				StartedAt:         latestLaunch.StartedAt,
				EndedAt:           latestLaunch.EndedAt,
				Summary:           latestLaunch.Summary,
				ErrorMessage:      latestLaunch.ErrorMessage,
				OutputArtifactRef: latestLaunch.OutputArtifactRef,
			}
		}
		if latestAck, err := c.store.Handoffs().LatestAcknowledgment(packet.HandoffID); err != nil {
			if err != sql.ErrNoRows {
				return ShellSnapshotResult{}, err
			}
		} else {
			result.Acknowledgment = &ShellAcknowledgmentSummary{
				Status:    latestAck.Status,
				Summary:   latestAck.Summary,
				CreatedAt: latestAck.CreatedAt,
			}
		}
		if latestFollowThrough, err := c.store.Handoffs().LatestFollowThrough(packet.HandoffID); err != nil {
			if err != sql.ErrNoRows {
				return ShellSnapshotResult{}, err
			}
		} else {
			result.FollowThrough = &ShellFollowThroughSummary{
				RecordID:        latestFollowThrough.RecordID,
				Kind:            latestFollowThrough.Kind,
				Summary:         latestFollowThrough.Summary,
				LaunchAttemptID: latestFollowThrough.LaunchAttemptID,
				LaunchID:        latestFollowThrough.LaunchID,
				CreatedAt:       latestFollowThrough.CreatedAt,
			}
		}
	}

	if latestResolution, err := c.store.Handoffs().LatestResolutionByTask(id); err != nil {
		if err != sql.ErrNoRows {
			return ShellSnapshotResult{}, err
		}
	} else {
		result.Resolution = &ShellResolutionSummary{
			ResolutionID:    latestResolution.ResolutionID,
			Kind:            latestResolution.Kind,
			Summary:         latestResolution.Summary,
			LaunchAttemptID: latestResolution.LaunchAttemptID,
			LaunchID:        latestResolution.LaunchID,
			CreatedAt:       latestResolution.CreatedAt,
		}
	}
	if packet, latestLaunch, latestAck, latestFollowThrough, err := c.loadActiveClaudeHandoffBranch(id); err != nil {
		return ShellSnapshotResult{}, err
	} else {
		continuity := assessHandoffContinuity(id, packet, latestLaunch, latestAck, latestFollowThrough, nil)
		result.HandoffContinuity = &ShellHandoffContinuitySummary{
			State:                        continuity.State,
			Reason:                       continuity.Reason,
			LaunchAttemptID:              continuity.LaunchAttemptID,
			LaunchID:                     continuity.LaunchID,
			AcknowledgmentID:             continuity.AcknowledgmentID,
			AcknowledgmentStatus:         continuity.AcknowledgmentStatus,
			AcknowledgmentSummary:        continuity.AcknowledgmentSummary,
			FollowThroughID:              continuity.FollowThroughID,
			FollowThroughKind:            continuity.FollowThroughKind,
			FollowThroughSummary:         continuity.FollowThroughSummary,
			ResolutionID:                 continuity.ResolutionID,
			ResolutionKind:               continuity.ResolutionKind,
			ResolutionSummary:            continuity.ResolutionSummary,
			DownstreamContinuationProven: continuity.DownstreamContinuationProven,
		}
	}

	if events, err := c.store.Proofs().ListByTask(id, 8); err != nil {
		return ShellSnapshotResult{}, err
	} else {
		result.RecentProofs = make([]ShellProofSummary, 0, len(events))
		for _, evt := range events {
			result.RecentProofs = append(result.RecentProofs, ShellProofSummary{
				EventID:   evt.EventID,
				Type:      evt.Type,
				Summary:   summarizeProofEvent(evt),
				Timestamp: evt.Timestamp,
			})
		}
	}
	if latestReceipt, err := c.store.OperatorStepReceipts().LatestByTask(id); err == nil {
		receiptCopy := latestReceipt
		result.LatestOperatorStepReceipt = &receiptCopy
	} else if err != sql.ErrNoRows {
		return ShellSnapshotResult{}, err
	}
	if recentReceipts, err := c.store.OperatorStepReceipts().ListByTask(id, 5); err != nil {
		return ShellSnapshotResult{}, err
	} else {
		result.RecentOperatorStepReceipts = append([]operatorstep.Receipt{}, recentReceipts...)
	}
	latestGapAck, recentGapAcks, err := c.reviewGapAcknowledgmentProjection(id, result.ShellSessions, 5)
	if err != nil {
		return ShellSnapshotResult{}, err
	}
	result.LatestTranscriptReviewGapAcknowledgment = latestGapAck
	result.RecentTranscriptReviewGapAcknowledgments = append([]TranscriptReviewGapAcknowledgmentSummary{}, recentGapAcks...)
	latestTransition, recentTransitions, err := c.continuityTransitionReceiptProjection(id, 5)
	if err != nil {
		return ShellSnapshotResult{}, err
	}
	result.LatestContinuityTransitionReceipt = latestTransition
	result.RecentContinuityTransitionReceipts = append([]ContinuityTransitionReceiptSummary{}, recentTransitions...)
	transitionRisk := deriveContinuityTransitionRiskSummary(result.RecentContinuityTransitionReceipts)
	result.ContinuityTransitionRiskSummary = &transitionRisk
	latestTriage, recentTriages, triageRollup, err := c.continuityIncidentTriageHistoryProjection(id, 5)
	if err != nil {
		return ShellSnapshotResult{}, err
	}
	latestFollowUp, recentFollowUps, followUpRollup, err := c.continuityIncidentFollowUpHistoryProjection(id, 5)
	if err != nil {
		return ShellSnapshotResult{}, err
	}
	result.LatestContinuityIncidentTriageReceipt = latestTriage
	result.RecentContinuityIncidentTriageReceipts = append([]ContinuityIncidentTriageReceiptSummary{}, recentTriages...)
	result.ContinuityIncidentTriageHistoryRollup = triageRollup
	result.LatestContinuityIncidentFollowUpReceipt = latestFollowUp
	result.RecentContinuityIncidentFollowUpReceipts = append([]ContinuityIncidentFollowUpReceiptSummary{}, recentFollowUps...)
	result.ContinuityIncidentFollowUpHistoryRollup = followUpRollup
	result.ContinuityIncidentFollowUp = deriveFollowUpAwareAdvisory(
		deriveContinuityIncidentFollowUpSummary(result.LatestContinuityTransitionReceipt, latestTriage, latestFollowUp),
		followUpRollup,
		result.RecentContinuityIncidentFollowUpReceipts,
	)
	result.ContinuityIncidentTaskRisk, err = c.continuityIncidentTaskRiskProjection(ctx, id, defaultContinuityIncidentTaskRiskReadLimit)
	if err != nil {
		return ShellSnapshotResult{}, err
	}
	var latestRecoveryAction *recoveryaction.Record
	if action, err := c.store.RecoveryActions().LatestByTask(id); err == nil {
		actionCopy := action
		latestRecoveryAction = &actionCopy
	} else if err != sql.ErrNoRows {
		return ShellSnapshotResult{}, err
	}
	result.ContinuityIncidentSummary = continuityIncidentSummaryProjection(result.LatestContinuityTransitionReceipt, latestRunForIncident, latestRecoveryAction)

	if messages, err := c.store.Conversations().ListRecent(caps.ConversationID, 18); err != nil {
		return ShellSnapshotResult{}, err
	} else {
		result.RecentConversation = make([]ShellConversationSummary, 0, len(messages))
		for _, msg := range messages {
			result.RecentConversation = append(result.RecentConversation, ShellConversationSummary{
				Role:      msg.Role,
				Body:      msg.Body,
				CreatedAt: msg.CreatedAt,
			})
			if msg.Role == conversation.RoleSystem {
				result.LatestCanonicalResponse = msg.Body
			}
		}
	}

	if assessment, err := c.assessContinue(ctx, id); err != nil {
		return ShellSnapshotResult{}, err
	} else {
		recovery, branch, authorities, decision, plan, _, runFinalization, localResume := c.operatorTruthForAssessment(assessment)
		reviewProgression := deriveOperatorReviewProgressionFromSessions(result.ShellSessions)
		applyReviewProgressionToOperatorDecision(&decision, reviewProgression)
		applyReviewProgressionToOperatorExecutionPlan(&plan, reviewProgression)
		applyContinuityIncidentFollowUpToOperatorDecision(&decision, result.ContinuityIncidentFollowUp)
		applyContinuityIncidentFollowUpToOperatorExecutionPlan(&plan, result.ContinuityIncidentFollowUp)
		result.Recovery = shellRecoverySummary(recovery)
		result.ActiveBranch = &ShellActiveBranchSummary{
			Class:                  branch.Class,
			BranchRef:              branch.BranchRef,
			ActionabilityAnchor:    branch.ActionabilityAnchor,
			ActionabilityAnchorRef: branch.ActionabilityAnchorRef,
			Reason:                 branch.Reason,
		}
		result.LocalRunFinalization = &ShellLocalRunFinalizationSummary{
			State:        runFinalization.State,
			RunID:        runFinalization.RunID,
			RunStatus:    runFinalization.RunStatus,
			CheckpointID: runFinalization.CheckpointID,
			Reason:       runFinalization.Reason,
		}
		result.LocalResume = &ShellLocalResumeAuthoritySummary{
			State:               localResume.State,
			Mode:                localResume.Mode,
			CheckpointID:        localResume.CheckpointID,
			RunID:               localResume.RunID,
			BlockingBranchClass: localResume.BlockingBranchClass,
			BlockingBranchRef:   localResume.BlockingBranchRef,
			Reason:              localResume.Reason,
		}
		result.ActionAuthority = shellOperatorActionAuthoritySet(authorities)
		result.OperatorDecision = shellOperatorDecisionSummary(decision)
		result.OperatorExecutionPlan = shellOperatorExecutionPlan(plan)
		control := assessLaunchControl(id, assessment.LatestHandoff, assessment.LatestLaunch)
		result.LaunchControl = &ShellLaunchControlSummary{
			State:            control.State,
			RetryDisposition: control.RetryDisposition,
			Reason:           control.Reason,
			HandoffID:        control.HandoffID,
			AttemptID:        control.AttemptID,
			LaunchID:         control.LaunchID,
			TargetWorker:     control.TargetWorker,
			RequestedAt:      control.RequestedAt,
			CompletedAt:      control.CompletedAt,
			FailedAt:         control.FailedAt,
		}
	}

	return result, nil
}

func shellOperatorActionAuthoritySet(in OperatorActionAuthoritySet) *ShellOperatorActionAuthoritySet {
	out := &ShellOperatorActionAuthoritySet{
		RequiredNextAction: in.RequiredNextAction,
	}
	if len(in.Actions) > 0 {
		out.Actions = make([]ShellOperatorActionAuthority, 0, len(in.Actions))
		for _, action := range in.Actions {
			out.Actions = append(out.Actions, ShellOperatorActionAuthority{
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
	return out
}

func shellOperatorDecisionSummary(in OperatorDecisionSummary) *ShellOperatorDecisionSummary {
	out := &ShellOperatorDecisionSummary{
		ActiveOwnerClass:   in.ActiveOwnerClass,
		ActiveOwnerRef:     in.ActiveOwnerRef,
		Headline:           in.Headline,
		RequiredNextAction: in.RequiredNextAction,
		PrimaryReason:      in.PrimaryReason,
		Guidance:           in.Guidance,
		IntegrityNote:      in.IntegrityNote,
	}
	if len(in.BlockedActions) > 0 {
		out.BlockedActions = make([]ShellOperatorDecisionBlockedAction, 0, len(in.BlockedActions))
		for _, blocked := range in.BlockedActions {
			out.BlockedActions = append(out.BlockedActions, ShellOperatorDecisionBlockedAction{
				Action: blocked.Action,
				Reason: blocked.Reason,
			})
		}
	}
	return out
}

func shellOperatorExecutionPlan(in OperatorExecutionPlan) *ShellOperatorExecutionPlan {
	out := &ShellOperatorExecutionPlan{
		MandatoryBeforeProgress: in.MandatoryBeforeProgress,
	}
	if in.PrimaryStep != nil {
		out.PrimaryStep = &ShellOperatorExecutionStep{
			Action:         in.PrimaryStep.Action,
			Status:         in.PrimaryStep.Status,
			Domain:         in.PrimaryStep.Domain,
			CommandSurface: in.PrimaryStep.CommandSurface,
			CommandHint:    in.PrimaryStep.CommandHint,
			Reason:         in.PrimaryStep.Reason,
		}
	}
	if len(in.SecondarySteps) > 0 {
		out.SecondarySteps = make([]ShellOperatorExecutionStep, 0, len(in.SecondarySteps))
		for _, step := range in.SecondarySteps {
			out.SecondarySteps = append(out.SecondarySteps, ShellOperatorExecutionStep{
				Action:         step.Action,
				Status:         step.Status,
				Domain:         step.Domain,
				CommandSurface: step.CommandSurface,
				CommandHint:    step.CommandHint,
				Reason:         step.Reason,
			})
		}
	}
	if len(in.BlockedSteps) > 0 {
		out.BlockedSteps = make([]ShellOperatorExecutionStep, 0, len(in.BlockedSteps))
		for _, step := range in.BlockedSteps {
			out.BlockedSteps = append(out.BlockedSteps, ShellOperatorExecutionStep{
				Action:         step.Action,
				Status:         step.Status,
				Domain:         step.Domain,
				CommandSurface: step.CommandSurface,
				CommandHint:    step.CommandHint,
				Reason:         step.Reason,
			})
		}
	}
	return out
}

func shellRecoverySummary(in RecoveryAssessment) *ShellRecoverySummary {
	out := &ShellRecoverySummary{
		ContinuityOutcome:      in.ContinuityOutcome,
		RecoveryClass:          in.RecoveryClass,
		RecommendedAction:      in.RecommendedAction,
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
		HandoffStatus:          in.HandoffStatus,
	}
	if len(in.Issues) > 0 {
		out.Issues = make([]ShellRecoveryIssue, 0, len(in.Issues))
		for _, issue := range in.Issues {
			out.Issues = append(out.Issues, ShellRecoveryIssue{
				Code:    issue.Code,
				Message: issue.Message,
			})
		}
	}
	return out
}

func (c *Coordinator) shellIntent(taskID common.TaskID, currentID common.IntentID) (intent.State, bool, error) {
	if currentID != "" {
		st, err := c.store.Intents().LatestByTask(taskID)
		if err == nil && st.IntentID == currentID {
			return st, true, nil
		}
		if err != nil && err != sql.ErrNoRows {
			return intent.State{}, false, err
		}
	}
	st, err := c.store.Intents().LatestByTask(taskID)
	if err != nil {
		if err == sql.ErrNoRows {
			return intent.State{}, false, nil
		}
		return intent.State{}, false, err
	}
	return st, true, nil
}

func (c *Coordinator) shellBrief(taskID common.TaskID, currentID common.BriefID) (brief.ExecutionBrief, bool, error) {
	if currentID != "" {
		b, err := c.store.Briefs().Get(currentID)
		if err == nil {
			return b, true, nil
		}
		if err != sql.ErrNoRows {
			return brief.ExecutionBrief{}, false, err
		}
	}
	b, err := c.store.Briefs().LatestByTask(taskID)
	if err != nil {
		if err == sql.ErrNoRows {
			return brief.ExecutionBrief{}, false, nil
		}
		return brief.ExecutionBrief{}, false, err
	}
	return b, true, nil
}

func capsuleAnchorSnapshot(caps capsule.WorkCapsule) anchorgit.Snapshot {
	return anchorgit.Snapshot{
		RepoRoot:         caps.RepoRoot,
		Branch:           caps.BranchName,
		HeadSHA:          caps.HeadSHA,
		WorkingTreeDirty: caps.WorkingTreeDirty,
		CapturedAt:       caps.AnchorCapturedAt,
	}
}

func shellIntentSummary(st intent.State) string {
	if strings.TrimSpace(st.NormalizedAction) == "" {
		return string(st.Class)
	}
	return fmt.Sprintf("%s: %s", st.Class, st.NormalizedAction)
}

func summarizeProofEvent(evt proof.Event) string {
	switch evt.Type {
	case proof.EventUserMessageReceived:
		return "User message recorded"
	case proof.EventIntentCompiled:
		return "Intent compiled"
	case proof.EventBriefCreated:
		return "Execution brief updated"
	case proof.EventBriefRegenerated:
		return "Execution brief regenerated"
	case proof.EventWorkerRunStarted:
		return "Worker run started"
	case proof.EventWorkerRunCompleted:
		return "Worker run completed"
	case proof.EventWorkerRunFailed:
		return "Worker run failed"
	case proof.EventRunInterrupted:
		return "Run interrupted"
	case proof.EventCheckpointCreated:
		return "Checkpoint created"
	case proof.EventContinueAssessed:
		return "Continuity assessed"
	case proof.EventHandoffCreated:
		return "Handoff packet created"
	case proof.EventHandoffAccepted:
		return "Handoff accepted"
	case proof.EventHandoffLaunchRequested:
		return "Handoff launch prepared"
	case proof.EventHandoffLaunchCompleted:
		return "Handoff launch invoked"
	case proof.EventHandoffLaunchFailed:
		return "Handoff launch failed"
	case proof.EventHandoffLaunchBlocked:
		return "Handoff launch blocked"
	case proof.EventHandoffAcknowledgmentCaptured:
		return "Worker acknowledgment captured"
	case proof.EventHandoffAcknowledgmentUnavailable:
		return "Worker acknowledgment unavailable"
	case proof.EventHandoffFollowThroughRecorded:
		return "Downstream follow-through recorded"
	case proof.EventHandoffResolutionRecorded:
		return "Handoff resolution recorded"
	case proof.EventOperatorStepExecutionRecorded:
		return "Operator step receipt recorded"
	case proof.EventRecoveryActionRecorded:
		return "Recovery action recorded"
	case proof.EventInterruptedRunReviewed:
		return "Interrupted run reviewed"
	case proof.EventInterruptedRunResumeExecuted:
		return "Interrupted lineage continuation selected"
	case proof.EventRecoveryContinueExecuted:
		return "Continue recovery executed"
	case proof.EventShellHostStarted:
		return "Shell live host started"
	case proof.EventShellHostExited:
		return "Shell live host ended"
	case proof.EventShellFallbackActivated:
		return "Shell transcript fallback activated"
	case proof.EventTranscriptEvidenceReviewed:
		return "Transcript evidence review recorded"
	case proof.EventTranscriptReviewGapAcknowledged:
		return "Transcript review-gap acknowledgment recorded"
	case proof.EventBranchHandoffTransitionRecorded:
		return "Branch/handoff transition receipt recorded"
	case proof.EventCanonicalResponseEmitted:
		return "Canonical response emitted"
	default:
		return strings.ReplaceAll(strings.ToLower(string(evt.Type)), "_", " ")
	}
}
