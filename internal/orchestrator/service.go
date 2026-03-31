package orchestrator

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"tuku/internal/adapters/adapter_contract"
	"tuku/internal/domain/benchmark"
	"tuku/internal/domain/brief"
	"tuku/internal/domain/capsule"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	contextdomain "tuku/internal/domain/context"
	"tuku/internal/domain/conversation"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/intent"
	"tuku/internal/domain/operatorstep"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/recoveryaction"
	rundomain "tuku/internal/domain/run"
	"tuku/internal/domain/shellsession"
	"tuku/internal/domain/taskmemory"
	anchorgit "tuku/internal/git/anchor"
	"tuku/internal/response/canonical"
	"tuku/internal/storage"
)

type StartTaskResult struct {
	TaskID            common.TaskID
	ConversationID    common.ConversationID
	Phase             phase.Phase
	CanonicalResponse string
	RepoAnchor        anchorgit.Snapshot
}

type MessageTaskResult struct {
	TaskID            common.TaskID
	Phase             phase.Phase
	IntentClass       intent.Class
	BriefID           common.BriefID
	BriefHash         string
	CanonicalResponse string
	RepoAnchor        anchorgit.Snapshot
}

type RunTaskRequest struct {
	TaskID             string
	Action             string // start|complete|interrupt
	Mode               string // real|noop
	RunID              common.RunID
	ShellSessionID     string
	SimulateInterrupt  bool
	InterruptionReason string
}

type RunTaskResult struct {
	TaskID            common.TaskID
	RunID             common.RunID
	RunStatus         rundomain.Status
	Phase             phase.Phase
	CanonicalResponse string
}

type ContinueOutcome string

const (
	ContinueOutcomeSafe                ContinueOutcome = "SAFE_RESUME_AVAILABLE"
	ContinueOutcomeRunInProgress       ContinueOutcome = "ACTIVE_RUN_IN_PROGRESS"
	ContinueOutcomeStaleReconciled     ContinueOutcome = "STALE_RUN_RECONCILED"
	ContinueOutcomeNeedsDecision       ContinueOutcome = "RESUME_DECISION_REQUIRED"
	ContinueOutcomeBlockedDrift        ContinueOutcome = "RESUME_BLOCKED_DRIFT"
	ContinueOutcomeBlockedInconsistent ContinueOutcome = "RESUME_BLOCKED_INCONSISTENT_STATE"
)

type ContinueTaskResult struct {
	TaskID                common.TaskID
	Outcome               ContinueOutcome
	DriftClass            checkpoint.DriftClass
	Phase                 phase.Phase
	RunID                 common.RunID
	CheckpointID          common.CheckpointID
	ResumeDescriptor      string
	RecoveryClass         RecoveryClass
	RecommendedAction     RecoveryAction
	ReadyForNextRun       bool
	ReadyForHandoffLaunch bool
	RecoveryReason        string
	CanonicalResponse     string
}

type CreateCheckpointResult struct {
	TaskID            common.TaskID
	CheckpointID      common.CheckpointID
	Trigger           checkpoint.Trigger
	IsResumable       bool
	CanonicalResponse string
}

type StatusTaskResult struct {
	TaskID                                      common.TaskID
	ConversationID                              common.ConversationID
	Goal                                        string
	Phase                                       phase.Phase
	Status                                      string
	CurrentIntentID                             common.IntentID
	CurrentIntentClass                          intent.Class
	CurrentIntentSummary                        string
	CompiledIntent                              *CompiledIntentSummary
	CurrentBriefID                              common.BriefID
	CurrentBriefHash                            string
	CompiledBrief                               *CompiledBriefSummary
	LatestRunID                                 common.RunID
	LatestRunStatus                             rundomain.Status
	LatestRunSummary                            string
	LatestRunWorkerRunID                        string
	LatestRunShellSessionID                     string
	LatestRunCommand                            string
	LatestRunArgs                               []string
	LatestRunExitCode                           *int
	LatestRunChangedFiles                       []string
	LatestRunChangedFilesSemantics              string
	LatestRunRepoDiffSummary                    string
	LatestRunWorktreeSummary                    string
	LatestRunValidationSignals                  []string
	LatestRunOutputArtifactRef                  string
	LatestRunStructuredSummary                  string
	CurrentContextPackID                        common.ContextPackID
	CurrentContextPackMode                      contextdomain.Mode
	CurrentContextPackFileCount                 int
	CurrentContextPackHash                      string
	CurrentTaskMemoryID                         common.MemoryID
	CurrentTaskMemorySource                     string
	CurrentTaskMemorySummary                    string
	CurrentTaskMemoryFullHistoryTokens          int
	CurrentTaskMemoryResumePromptTokens         int
	CurrentTaskMemoryCompactionRatio            float64
	CurrentBenchmarkID                          common.BenchmarkID
	CurrentBenchmarkSource                      string
	CurrentBenchmarkSummary                     string
	CurrentBenchmarkRawPromptTokens             int
	CurrentBenchmarkDispatchPromptTokens        int
	CurrentBenchmarkStructuredPromptTokens      int
	CurrentBenchmarkSelectedContextTokens       int
	CurrentBenchmarkEstimatedTokenSavings       int
	CurrentBenchmarkFilesScanned                int
	CurrentBenchmarkRankedTargetCount           int
	CurrentBenchmarkCandidateRecallAt3          float64
	CurrentBenchmarkDefaultSerializer           string
	CurrentBenchmarkStructuredCheaper           bool
	CurrentBenchmarkConfidenceValue             float64
	CurrentBenchmarkConfidenceLevel             string
	LatestPolicyDecisionID                      common.DecisionID
	LatestPolicyDecisionStatus                  string
	LatestPolicyDecisionRiskLevel               string
	LatestPolicyDecisionReason                  string
	RepoAnchor                                  anchorgit.Snapshot
	LatestShellSessionID                        string
	LatestShellSessionClass                     ShellSessionClass
	LatestShellSessionReason                    string
	LatestShellSessionGuidance                  string
	LatestShellSessionWorkerSessionID           string
	LatestShellSessionWorkerSessionIDSource     shellsession.WorkerSessionIDSource
	LatestShellTranscriptState                  shellsession.TranscriptState
	LatestShellTranscriptRetainedChunks         int
	LatestShellTranscriptDroppedChunks          int
	LatestShellTranscriptRetentionLimit         int
	LatestShellTranscriptOldestSequence         int64
	LatestShellTranscriptNewestSequence         int64
	LatestShellTranscriptLastChunkAt            time.Time
	LatestShellTranscriptReviewID               common.EventID
	LatestShellTranscriptReviewSource           shellsession.TranscriptSource
	LatestShellTranscriptReviewedUpTo           int64
	LatestShellTranscriptReviewSummary          string
	LatestShellTranscriptReviewAt               time.Time
	LatestShellTranscriptReviewStale            bool
	LatestShellTranscriptReviewNewer            int
	LatestShellTranscriptReviewClosureState     shellsession.TranscriptReviewClosureState
	LatestShellTranscriptReviewOldestUnreviewed int64
	LatestShellSessionState                     string
	LatestShellSessionUpdatedAt                 time.Time
	LatestShellEventID                          common.EventID
	LatestShellEventKind                        string
	LatestShellEventSessionID                   string
	LatestShellEventAt                          time.Time
	LatestShellEventNote                        string
	LatestCheckpointID                          common.CheckpointID
	LatestCheckpointAt                          time.Time
	LatestCheckpointTrigger                     checkpoint.Trigger
	CheckpointResumable                         bool
	ResumeDescriptor                            string
	LatestLaunchAttemptID                       string
	LatestLaunchID                              string
	LatestLaunchStatus                          handoff.LaunchStatus
	LatestAcknowledgmentID                      string
	LatestAcknowledgmentStatus                  handoff.AcknowledgmentStatus
	LatestAcknowledgmentSummary                 string
	LatestFollowThroughID                       string
	LatestFollowThroughKind                     handoff.FollowThroughKind
	LatestFollowThroughSummary                  string
	LatestResolutionID                          string
	LatestResolutionKind                        handoff.ResolutionKind
	LatestResolutionSummary                     string
	LatestResolutionAt                          time.Time
	LaunchControlState                          LaunchControlState
	LaunchRetryDisposition                      LaunchRetryDisposition
	LaunchControlReason                         string
	HandoffContinuityState                      HandoffContinuityState
	HandoffContinuityReason                     string
	HandoffContinuationProven                   bool
	ActiveBranchClass                           ActiveBranchClass
	ActiveBranchRef                             string
	ActiveBranchAnchorKind                      ActiveBranchAnchorKind
	ActiveBranchAnchorRef                       string
	ActiveBranchReason                          string
	LocalRunFinalizationState                   LocalRunFinalizationState
	LocalRunFinalizationRunID                   common.RunID
	LocalRunFinalizationStatus                  rundomain.Status
	LocalRunFinalizationCheckpointID            common.CheckpointID
	LocalRunFinalizationReason                  string
	LocalResumeAuthorityState                   LocalResumeAuthorityState
	LocalResumeMode                             LocalResumeMode
	LocalResumeCheckpointID                     common.CheckpointID
	LocalResumeRunID                            common.RunID
	LocalResumeReason                           string
	RequiredNextOperatorAction                  OperatorAction
	ActionAuthority                             []OperatorActionAuthority
	OperatorDecision                            *OperatorDecisionSummary
	OperatorExecutionPlan                       *OperatorExecutionPlan
	LatestOperatorStepReceipt                   *operatorstep.Receipt
	RecentOperatorStepReceipts                  []operatorstep.Receipt
	LatestContinuityTransitionReceipt           *ContinuityTransitionReceiptSummary
	RecentContinuityTransitionReceipts          []ContinuityTransitionReceiptSummary
	ContinuityTransitionRiskSummary             *ContinuityTransitionRiskSummary
	ContinuityIncidentSummary                   *ContinuityIncidentRiskSummary
	LatestContinuityIncidentTriageReceipt       *ContinuityIncidentTriageReceiptSummary
	RecentContinuityIncidentTriageReceipts      []ContinuityIncidentTriageReceiptSummary
	ContinuityIncidentTriageHistoryRollup       *ContinuityIncidentTriageHistoryRollupSummary
	LatestContinuityIncidentFollowUpReceipt     *ContinuityIncidentFollowUpReceiptSummary
	RecentContinuityIncidentFollowUpReceipts    []ContinuityIncidentFollowUpReceiptSummary
	ContinuityIncidentFollowUpHistoryRollup     *ContinuityIncidentFollowUpHistoryRollupSummary
	ContinuityIncidentFollowUp                  *ContinuityIncidentFollowUpSummary
	ContinuityIncidentTaskRisk                  *ContinuityIncidentTaskRiskSummary
	LatestTranscriptReviewGapAcknowledgment     *TranscriptReviewGapAcknowledgmentSummary
	RecentTranscriptReviewGapAcknowledgments    []TranscriptReviewGapAcknowledgmentSummary
	IsResumable                                 bool
	RecoveryClass                               RecoveryClass
	RecommendedAction                           RecoveryAction
	ReadyForNextRun                             bool
	ReadyForHandoffLaunch                       bool
	RecoveryReason                              string
	LatestRecoveryAction                        *recoveryaction.Record
	LastEventID                                 common.EventID
	LastEventType                               proof.EventType
	LastEventAt                                 time.Time
}

type InspectTaskResult struct {
	TaskID                                   common.TaskID
	Intent                                   *intent.State
	CompiledIntent                           *CompiledIntentSummary
	Brief                                    *brief.ExecutionBrief
	CompiledBrief                            *CompiledBriefSummary
	TaskMemory                               *taskmemory.Snapshot
	Benchmark                                *benchmark.Run
	Run                                      *rundomain.ExecutionRun
	Checkpoint                               *checkpoint.Checkpoint
	Handoff                                  *handoff.Packet
	Launch                                   *handoff.Launch
	Acknowledgment                           *handoff.Acknowledgment
	FollowThrough                            *handoff.FollowThrough
	Resolution                               *handoff.Resolution
	ActiveBranch                             *ActiveBranchProvenance
	LocalRunFinalization                     *LocalRunFinalization
	LocalResumeAuthority                     *LocalResumeAuthority
	ActionAuthority                          *OperatorActionAuthoritySet
	OperatorDecision                         *OperatorDecisionSummary
	OperatorExecutionPlan                    *OperatorExecutionPlan
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
	LaunchControl                            *LaunchControl
	HandoffContinuity                        *HandoffContinuity
	Recovery                                 *RecoveryAssessment
	LatestRecoveryAction                     *recoveryaction.Record
	RecentRecoveryActions                    []recoveryaction.Record
	ShellSessions                            []ShellSessionView
	RecentShellEvents                        []shellsession.Event
	RecentShellTranscript                    []shellsession.TranscriptChunk
	RepoAnchor                               anchorgit.Snapshot
}

type Dependencies struct {
	Store                  storage.Store
	IntentCompiler         intent.Compiler
	BriefBuilder           brief.Builder
	WorkerAdapter          adapter_contract.WorkerAdapter
	HandoffLauncher        adapter_contract.HandoffLauncher
	Synthesizer            canonical.Synthesizer
	AnchorProvider         anchorgit.Provider
	ShellSessions          ShellSessionRegistry
	ShellSessionStaleAfter time.Duration
	Clock                  func() time.Time
	IDGenerator            func(prefix string) string
}

type Coordinator struct {
	store                  storage.Store
	intentCompiler         intent.Compiler
	briefBuilder           brief.Builder
	workerAdapter          adapter_contract.WorkerAdapter
	handoffLauncher        adapter_contract.HandoffLauncher
	synthesizer            canonical.Synthesizer
	anchorProvider         anchorgit.Provider
	shellSessions          ShellSessionRegistry
	shellSessionStaleAfter time.Duration
	clock                  func() time.Time
	idGenerator            func(prefix string) string
	activeRunsMu           sync.RWMutex
	activeRuns             map[common.TaskID]common.RunID
}

func NewCoordinator(deps Dependencies) (*Coordinator, error) {
	if deps.Store == nil {
		return nil, errors.New("store is required")
	}
	if deps.IntentCompiler == nil {
		return nil, errors.New("intent compiler is required")
	}
	if deps.BriefBuilder == nil {
		return nil, errors.New("brief builder is required")
	}
	if deps.Synthesizer == nil {
		return nil, errors.New("canonical synthesizer is required")
	}
	if deps.ShellSessions == nil {
		return nil, errors.New("shell session registry is required")
	}
	if deps.AnchorProvider == nil {
		deps.AnchorProvider = anchorgit.NewGitProvider()
	}
	if deps.ShellSessionStaleAfter <= 0 {
		deps.ShellSessionStaleAfter = DefaultShellSessionStaleAfter
	}
	if deps.Clock == nil {
		deps.Clock = func() time.Time { return time.Now().UTC() }
	}
	if deps.IDGenerator == nil {
		deps.IDGenerator = newID
	}
	return &Coordinator{
		store:                  deps.Store,
		intentCompiler:         deps.IntentCompiler,
		briefBuilder:           deps.BriefBuilder,
		workerAdapter:          deps.WorkerAdapter,
		handoffLauncher:        deps.HandoffLauncher,
		synthesizer:            deps.Synthesizer,
		anchorProvider:         deps.AnchorProvider,
		shellSessions:          deps.ShellSessions,
		shellSessionStaleAfter: deps.ShellSessionStaleAfter,
		clock:                  deps.Clock,
		idGenerator:            deps.IDGenerator,
		activeRuns:             map[common.TaskID]common.RunID{},
	}, nil
}

func (c *Coordinator) StartTask(ctx context.Context, goal string, repoRoot string) (StartTaskResult, error) {
	var result StartTaskResult
	err := c.withTx(func(txc *Coordinator) error {
		now := txc.clock()
		taskID := common.TaskID(txc.idGenerator("tsk"))
		conversationID := common.ConversationID(txc.idGenerator("conv"))
		repo := strings.TrimSpace(repoRoot)
		if repo == "" {
			repo = "."
		}
		repo = filepath.Clean(repo)
		anchor := txc.anchorProvider.Capture(ctx, repo)

		caps := capsule.WorkCapsule{
			TaskID:             taskID,
			ConversationID:     conversationID,
			Version:            1,
			CreatedAt:          now,
			UpdatedAt:          now,
			Goal:               strings.TrimSpace(goal),
			AcceptanceCriteria: []string{},
			Constraints:        []string{},
			RepoRoot:           anchor.RepoRoot,
			WorktreePath:       anchor.RepoRoot,
			BranchName:         anchor.Branch,
			HeadSHA:            anchor.HeadSHA,
			WorkingTreeDirty:   anchor.WorkingTreeDirty,
			AnchorCapturedAt:   anchor.CapturedAt,
			CurrentPhase:       phase.PhaseIntake,
			Status:             "ACTIVE",
			CurrentIntentID:    "",
			CurrentBriefID:     "",
			TouchedFiles:       []string{},
			Blockers:           []string{},
			NextAction:         "Await user message for intent interpretation",
			ParentTaskID:       nil,
			ChildTaskIDs:       []common.TaskID{},
			EdgeRefs:           []string{},
		}
		if err := txc.store.Capsules().Create(caps); err != nil {
			return err
		}
		if err := txc.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": string(phase.PhaseIntake), "reason": "task created"}, nil); err != nil {
			return err
		}

		canonicalText := "Tuku task initialized. Repo anchor captured. I am tracking canonical task state and evidence. Send your first implementation instruction to generate an execution brief."
		if err := txc.emitCanonicalConversation(caps, canonicalText, map[string]any{"summary": "task initialized"}, nil); err != nil {
			return err
		}

		result = StartTaskResult{
			TaskID:            taskID,
			ConversationID:    conversationID,
			Phase:             caps.CurrentPhase,
			CanonicalResponse: canonicalText,
			RepoAnchor:        anchor,
		}
		return nil
	})
	if err != nil {
		return StartTaskResult{}, err
	}
	return result, nil
}

func (c *Coordinator) markRunActive(taskID common.TaskID, runID common.RunID) {
	c.activeRunsMu.Lock()
	defer c.activeRunsMu.Unlock()
	c.activeRuns[taskID] = runID
}

func (c *Coordinator) clearRunActive(taskID common.TaskID, runID common.RunID) {
	c.activeRunsMu.Lock()
	defer c.activeRunsMu.Unlock()
	current, ok := c.activeRuns[taskID]
	if ok && current == runID {
		delete(c.activeRuns, taskID)
	}
}

func (c *Coordinator) isRunActive(taskID common.TaskID, runID common.RunID) bool {
	c.activeRunsMu.RLock()
	defer c.activeRunsMu.RUnlock()
	current, ok := c.activeRuns[taskID]
	return ok && current == runID
}

func (c *Coordinator) MessageTask(ctx context.Context, taskID string, message string) (MessageTaskResult, error) {
	if blocked, err := c.localMutationBlockedByClaudeHandoff(ctx, common.TaskID(taskID), "compile a new local execution brief"); err != nil {
		return MessageTaskResult{}, err
	} else if blocked != "" {
		return MessageTaskResult{}, fmt.Errorf(blocked)
	}
	var result MessageTaskResult
	err := c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(common.TaskID(taskID))
		if err != nil {
			return err
		}
		now := txc.clock()

		anchor := txc.anchorProvider.Capture(ctx, caps.RepoRoot)
		caps.BranchName = anchor.Branch
		caps.HeadSHA = anchor.HeadSHA
		caps.WorkingTreeDirty = anchor.WorkingTreeDirty
		caps.AnchorCapturedAt = anchor.CapturedAt

		userMsg := conversation.Message{
			MessageID:      common.MessageID(txc.idGenerator("msg")),
			ConversationID: caps.ConversationID,
			TaskID:         caps.TaskID,
			Role:           conversation.RoleUser,
			Body:           message,
			CreatedAt:      now,
		}
		if err := txc.store.Conversations().Append(userMsg); err != nil {
			return err
		}
		if err := txc.appendProof(caps, proof.EventUserMessageReceived, proof.ActorUser, "user", map[string]any{"message_id": userMsg.MessageID}, nil); err != nil {
			return err
		}

		recent, err := txc.store.Conversations().ListRecent(caps.ConversationID, 12)
		if err != nil {
			return err
		}
		recentBodies := make([]string, 0, len(recent))
		for _, m := range recent {
			recentBodies = append(recentBodies, m.Body)
		}

		intentState, err := txc.intentCompiler.Compile(intent.CompileInput{
			TaskID:            caps.TaskID,
			LatestMessage:     message,
			RecentMessages:    recentBodies,
			CurrentPhase:      caps.CurrentPhase,
			CurrentBlockers:   caps.Blockers,
			CurrentGoal:       caps.Goal,
			RepoAnchorSummary: fmt.Sprintf("repo=%s branch=%s head=%s dirty=%t", caps.RepoRoot, caps.BranchName, caps.HeadSHA, caps.WorkingTreeDirty),
		})
		if err != nil {
			return err
		}
		intentState.SourceMessageIDs = []common.MessageID{userMsg.MessageID}
		triage, err := txc.sharpenPromptTriage(caps, intentState, message)
		if err != nil {
			return err
		}
		intentState = triage.IntentState
		if err := txc.store.Intents().Save(intentState); err != nil {
			return err
		}

		caps.Version++
		caps.UpdatedAt = now
		caps.CurrentIntentID = intentState.IntentID
		caps.CurrentPhase = intentState.ProposedPhase
		if err := txc.appendProof(caps, proof.EventIntentCompiled, proof.ActorSystem, "tuku-intent-stub", map[string]any{
			"intent_id": intentState.IntentID, "class": intentState.Class,
			"normalized_action": intentState.NormalizedAction, "confidence": intentState.Confidence,
			"prompt_triage_applied":         triage.PromptTriage.Applied,
			"prompt_triage_files_scanned":   triage.PromptTriage.FilesScanned,
			"prompt_triage_candidate_count": len(triage.PromptTriage.CandidateFiles),
		}, nil); err != nil {
			return err
		}

		briefInput := buildBriefInputV2(caps, intentState, nil, caps.Version)
		if len(triage.ScopeHints) > 0 {
			briefInput.ScopeHints = dedupeNonEmpty(append(append([]string{}, triage.ScopeHints...), briefInput.ScopeHints...), 16)
		}
		if strings.TrimSpace(triage.WorkerFraming) != "" {
			briefInput.WorkerFraming = triage.WorkerFraming
		}
		contextMode := contextModeForVerbosity(briefInput.Verbosity)
		contextPack, err := txc.buildContextPack(caps, briefInput.ScopeHints, contextMode, tokenBudgetForContextMode(contextMode))
		if err != nil {
			return err
		}
		if err := txc.store.ContextPacks().Save(contextPack); err != nil {
			return err
		}
		briefInput.PromptTriage = finalizePromptTriageTelemetry(triage.PromptTriage, briefInput, contextPack)
		briefInput.ContextPackID = contextPack.ContextPackID
		provisionalBrief, err := txc.briefBuilder.Build(briefInput)
		if err != nil {
			return err
		}
		taskMemorySnapshot, err := txc.buildTaskMemorySnapshot(caps, provisionalBrief, contextPack, nil, "brief_compiled")
		if err != nil {
			return err
		}
		briefInput.TaskMemoryID = taskMemorySnapshot.MemoryID
		briefInput.MemoryCompression = memoryCompressionFromSnapshot(taskMemorySnapshot)
		promptPacket, _, benchmarkRun, err := txc.buildPromptIRAndBenchmark(caps, intentState, briefInput, contextPack, taskMemorySnapshot, message)
		if err != nil {
			return err
		}
		briefInput.PromptIR = promptPacket
		briefInput.BenchmarkID = benchmarkRun.BenchmarkID
		briefArtifact, err := txc.briefBuilder.Build(briefInput)
		if err != nil {
			return err
		}
		taskMemorySnapshot.BriefID = briefArtifact.BriefID
		if err := txc.store.TaskMemories().Save(taskMemorySnapshot); err != nil {
			return err
		}
		if err := txc.store.Briefs().Save(briefArtifact); err != nil {
			return err
		}
		benchmarkRun.BriefID = briefArtifact.BriefID
		if err := txc.store.Benchmarks().Save(benchmarkRun); err != nil {
			return err
		}

		caps.CurrentBriefID = briefArtifact.BriefID
		caps.CurrentPhase = phase.PhaseBriefReady
		switch briefArtifact.Posture {
		case brief.PostureClarificationNeeded:
			caps.NextAction = "Execution brief is clarification-needed. Refine scope/objective or continue with bounded planning before execution."
		case brief.PosturePlanningOriented:
			caps.NextAction = "Execution brief is planning-oriented. Validate scope/constraints, then execute the next bounded step."
		case brief.PostureValidationOriented:
			caps.NextAction = "Execution brief is validation-oriented. Run bounded validation and record evidence."
		case brief.PostureRepairOriented:
			caps.NextAction = "Execution brief is repair-oriented. Execute bounded repair work and record evidence."
		default:
			caps.NextAction = "Execution brief is ready. Start a run with `tuku run --task <id>`."
		}
		if err := txc.store.Capsules().Update(caps); err != nil {
			return err
		}

		if err := txc.appendProof(caps, proof.EventBriefCreated, proof.ActorSystem, "tuku-brief-builder", map[string]any{
			"brief_id":                               briefArtifact.BriefID,
			"brief_hash":                             briefArtifact.BriefHash,
			"intent_id":                              intentState.IntentID,
			"brief_posture":                          briefArtifact.Posture,
			"requires_clarification":                 briefArtifact.RequiresClarification,
			"context_pack_id":                        briefArtifact.ContextPackID,
			"context_pack_hash":                      contextPack.PackHash,
			"context_pack_file_count":                len(contextPack.IncludedFiles),
			"context_pack_snippet_count":             len(contextPack.IncludedSnippets),
			"task_memory_id":                         taskMemorySnapshot.MemoryID,
			"task_memory_resume_prompt_tokens":       taskMemorySnapshot.ResumePromptTokenEstimate,
			"task_memory_full_history_tokens":        taskMemorySnapshot.FullHistoryTokenEstimate,
			"task_memory_compaction_ratio":           taskMemorySnapshot.MemoryCompactionRatio,
			"benchmark_id":                           benchmarkRun.BenchmarkID,
			"prompt_ir_target_count":                 len(briefArtifact.PromptIR.RankedTargets),
			"prompt_ir_validator_count":              len(briefArtifact.PromptIR.ValidatorPlan.Commands),
			"prompt_ir_confidence":                   briefArtifact.PromptIR.Confidence.Value,
			"prompt_ir_confidence_level":             briefArtifact.PromptIR.Confidence.Level,
			"benchmark_dispatch_prompt_tokens":       benchmarkRun.DispatchPromptTokenEstimate,
			"benchmark_structured_prompt_tokens":     benchmarkRun.StructuredPromptTokenEstimate,
			"benchmark_estimated_token_savings":      benchmarkRun.EstimatedTokenSavings,
			"benchmark_default_serializer":           benchmarkRun.DefaultSerializer,
			"bounded_evidence_messages":              briefArtifact.BoundedEvidenceMessages,
			"prompt_triage_applied":                  briefArtifact.PromptTriage.Applied,
			"prompt_triage_reason":                   briefArtifact.PromptTriage.Reason,
			"prompt_triage_files_scanned":            briefArtifact.PromptTriage.FilesScanned,
			"prompt_triage_candidate_count":          len(briefArtifact.PromptTriage.CandidateFiles),
			"prompt_triage_context_savings_estimate": briefArtifact.PromptTriage.ContextTokenSavingsEstimate,
		}, nil); err != nil {
			return err
		}
		if err := txc.appendProof(caps, proof.EventTaskMemoryUpdated, proof.ActorSystem, "tuku-task-memory", map[string]any{
			"memory_id":               taskMemorySnapshot.MemoryID,
			"source":                  taskMemorySnapshot.Source,
			"brief_id":                briefArtifact.BriefID,
			"full_history_tokens":     taskMemorySnapshot.FullHistoryTokenEstimate,
			"resume_prompt_tokens":    taskMemorySnapshot.ResumePromptTokenEstimate,
			"memory_compaction_ratio": taskMemorySnapshot.MemoryCompactionRatio,
			"confirmed_facts_count":   len(taskMemorySnapshot.ConfirmedFacts),
			"validators_run_count":    len(taskMemorySnapshot.ValidatorsRun),
			"candidate_files_count":   len(taskMemorySnapshot.CandidateFiles),
		}, nil); err != nil {
			return err
		}
		if err := txc.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": caps.CurrentPhase, "reason": "intent and brief prepared"}, nil); err != nil {
			return err
		}

		recentEvents, err := txc.store.Proofs().ListByTask(caps.TaskID, 10)
		if err != nil {
			return err
		}
		canonicalText, err := txc.synthesizer.Synthesize(ctx, caps, recentEvents)
		if err != nil {
			return err
		}
		if err := txc.emitCanonicalConversation(caps, canonicalText, map[string]any{"intent_id": intentState.IntentID, "brief_id": briefArtifact.BriefID}, nil); err != nil {
			return err
		}

		result = MessageTaskResult{
			TaskID:            caps.TaskID,
			Phase:             caps.CurrentPhase,
			IntentClass:       intentState.Class,
			BriefID:           briefArtifact.BriefID,
			BriefHash:         briefArtifact.BriefHash,
			CanonicalResponse: canonicalText,
			RepoAnchor:        anchor,
		}
		return nil
	})
	if err != nil {
		return MessageTaskResult{}, err
	}
	return result, nil
}

func (c *Coordinator) RunTask(ctx context.Context, req RunTaskRequest) (RunTaskResult, error) {
	action := strings.TrimSpace(strings.ToLower(req.Action))
	if action == "" {
		action = "start"
	}
	mode := strings.TrimSpace(strings.ToLower(req.Mode))
	if mode == "" {
		mode = "real"
	}

	switch action {
	case "start":
		if mode == "real" {
			return c.startRunRealStaged(ctx, req)
		}
		var result RunTaskResult
		err := c.withTx(func(txc *Coordinator) error {
			caps, err := txc.store.Capsules().Get(common.TaskID(req.TaskID))
			if err != nil {
				return err
			}
			result, err = txc.startRunNoop(ctx, caps, req)
			return err
		})
		if err != nil {
			return RunTaskResult{}, err
		}
		return result, nil
	case "complete":
		var result RunTaskResult
		err := c.withTx(func(txc *Coordinator) error {
			caps, err := txc.store.Capsules().Get(common.TaskID(req.TaskID))
			if err != nil {
				return err
			}
			result, err = txc.completeRun(ctx, caps, req)
			return err
		})
		if err != nil {
			return RunTaskResult{}, err
		}
		return result, nil
	case "interrupt":
		var result RunTaskResult
		err := c.withTx(func(txc *Coordinator) error {
			caps, err := txc.store.Capsules().Get(common.TaskID(req.TaskID))
			if err != nil {
				return err
			}
			result, err = txc.interruptRun(ctx, caps, req)
			return err
		})
		if err != nil {
			return RunTaskResult{}, err
		}
		return result, nil
	default:
		return RunTaskResult{}, fmt.Errorf("unsupported run action: %s", req.Action)
	}
}

func (c *Coordinator) CreateCheckpoint(ctx context.Context, taskID string) (CreateCheckpointResult, error) {
	if blocked, err := c.localMutationBlockedByClaudeHandoff(ctx, common.TaskID(taskID), "capture a new local checkpoint"); err != nil {
		return CreateCheckpointResult{}, err
	} else if blocked != "" {
		return CreateCheckpointResult{}, fmt.Errorf(blocked)
	}
	var result CreateCheckpointResult
	err := c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(common.TaskID(taskID))
		if err != nil {
			return err
		}
		anchor := txc.anchorProvider.Capture(ctx, caps.RepoRoot)
		caps.BranchName = anchor.Branch
		caps.HeadSHA = anchor.HeadSHA
		caps.WorkingTreeDirty = anchor.WorkingTreeDirty
		caps.AnchorCapturedAt = anchor.CapturedAt
		caps.Version++
		caps.UpdatedAt = txc.clock()
		if err := txc.store.Capsules().Update(caps); err != nil {
			return err
		}

		resumable := caps.CurrentBriefID != "" && caps.CurrentPhase != phase.PhaseBlocked && caps.CurrentPhase != phase.PhaseAwaitingDecision
		descriptor := "Manual checkpoint captured for deterministic continue."
		if !resumable {
			descriptor = "Manual checkpoint captured for recovery inspection; direct resume is not currently ready."
		}
		cp, err := txc.createCheckpoint(caps, "", checkpoint.TriggerManual, resumable, descriptor)
		if err != nil {
			return err
		}
		canonical := fmt.Sprintf(
			"Manual checkpoint %s captured. Task is resumable from branch %s (head %s).",
			cp.CheckpointID,
			caps.BranchName,
			caps.HeadSHA,
		)
		if !resumable {
			canonical = fmt.Sprintf(
				"Manual checkpoint %s captured on branch %s (head %s), but direct resume is not currently ready.",
				cp.CheckpointID,
				caps.BranchName,
				caps.HeadSHA,
			)
		}
		if err := txc.emitCanonicalConversation(caps, canonical, map[string]any{
			"checkpoint_id": cp.CheckpointID,
			"trigger":       cp.Trigger,
			"is_resumable":  cp.IsResumable,
		}, nil); err != nil {
			return err
		}

		result = CreateCheckpointResult{
			TaskID:            caps.TaskID,
			CheckpointID:      cp.CheckpointID,
			Trigger:           cp.Trigger,
			IsResumable:       cp.IsResumable,
			CanonicalResponse: canonical,
		}
		return nil
	})
	if err != nil {
		return CreateCheckpointResult{}, err
	}
	return result, nil
}

func (c *Coordinator) ContinueTask(ctx context.Context, taskID string) (ContinueTaskResult, error) {
	assessment, err := c.assessContinue(ctx, common.TaskID(taskID))
	if err != nil {
		return ContinueTaskResult{}, err
	}
	recovery := c.recoveryFromContinueAssessment(assessment)
	if assessment.Outcome == ContinueOutcomeSafe && !recovery.ReadyForNextRun {
		assessment.RequiresMutation = false
	}
	if !assessment.RequiresMutation {
		return c.recordNoMutationContinueOutcome(ctx, assessment, recovery)
	}

	var result ContinueTaskResult
	err = c.withTx(func(txc *Coordinator) error {
		return txc.finalizeContinue(ctx, assessment, recovery, &result)
	})
	if err != nil {
		return ContinueTaskResult{}, err
	}
	return result, nil
}

func (c *Coordinator) recordNoMutationContinueOutcome(_ context.Context, assessment continueAssessment, recovery RecoveryAssessment) (ContinueTaskResult, error) {
	result := c.noMutationContinueResult(assessment, recovery)
	err := c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(assessment.TaskID)
		if err != nil {
			return err
		}
		runID := runIDPointer(result.RunID)
		payload := map[string]any{
			"outcome":           result.Outcome,
			"drift_class":       result.DriftClass,
			"checkpoint_id":     result.CheckpointID,
			"resume_descriptor": result.ResumeDescriptor,
			"no_state_mutation": true,
			"checkpoint_reused": result.CheckpointID != "",
			"assessment_reason": assessment.Reason,
		}
		payload["recovery_class"] = result.RecoveryClass
		payload["recommended_action"] = result.RecommendedAction
		payload["ready_for_next_run"] = result.ReadyForNextRun
		payload["ready_for_handoff_launch"] = result.ReadyForHandoffLaunch
		payload["recovery_reason"] = result.RecoveryReason
		if err := txc.appendProof(caps, proof.EventContinueAssessed, proof.ActorSystem, "tuku-daemon", payload, runID); err != nil {
			return err
		}
		if err := txc.emitCanonicalConversation(caps, result.CanonicalResponse, payload, runID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return ContinueTaskResult{}, err
	}
	return result, nil
}

type continueAssessment struct {
	TaskID               common.TaskID
	Capsule              capsule.WorkCapsule
	LatestRun            *rundomain.ExecutionRun
	LatestCheckpoint     *checkpoint.Checkpoint
	LatestHandoff        *handoff.Packet
	LatestLaunch         *handoff.Launch
	LatestAck            *handoff.Acknowledgment
	LatestFollowThrough  *handoff.FollowThrough
	LatestResolution     *handoff.Resolution
	LatestRecoveryAction *recoveryaction.Record
	FreshAnchor          anchorgit.Snapshot
	DriftClass           checkpoint.DriftClass
	Outcome              ContinueOutcome
	Reason               string
	Issues               []continuityViolation
	RequiresMutation     bool
	ReuseCheckpointID    common.CheckpointID
}

func (c *Coordinator) assessContinue(ctx context.Context, taskID common.TaskID) (continueAssessment, error) {
	snapshot, err := c.loadContinuitySnapshot(taskID)
	if err != nil {
		return continueAssessment{}, err
	}
	caps := snapshot.Capsule
	anchor := c.anchorProvider.Capture(ctx, caps.RepoRoot)
	issues, err := c.validateContinuitySnapshot(snapshot)
	if err != nil {
		return continueAssessment{}, err
	}
	issue := firstContinuityViolationMessage(issues)
	if issue != "" {
		reuse := c.canReuseInconsistencyCheckpoint(caps, snapshot.LatestCheckpoint, anchor, issue)
		return continueAssessment{
			TaskID:               taskID,
			Capsule:              caps,
			LatestRun:            snapshot.LatestRun,
			LatestCheckpoint:     snapshot.LatestCheckpoint,
			LatestHandoff:        snapshot.ActiveHandoff,
			LatestLaunch:         snapshot.ActiveLaunch,
			LatestAck:            snapshot.ActiveAcknowledgment,
			LatestFollowThrough:  snapshot.ActiveFollowThrough,
			LatestResolution:     snapshot.LatestResolution,
			LatestRecoveryAction: snapshot.LatestRecoveryAction,
			FreshAnchor:          anchor,
			Outcome:              ContinueOutcomeBlockedInconsistent,
			Reason:               issue,
			Issues:               issues,
			DriftClass:           checkpoint.DriftNone,
			RequiresMutation:     !reuse,
			ReuseCheckpointID:    reusableCheckpointID(snapshot.LatestCheckpoint, reuse),
		}, nil
	}

	if snapshot.LatestRun != nil && snapshot.LatestRun.Status == rundomain.StatusRunning {
		if c.isRunActive(taskID, snapshot.LatestRun.RunID) {
			return continueAssessment{
				TaskID:               taskID,
				Capsule:              caps,
				LatestRun:            snapshot.LatestRun,
				LatestCheckpoint:     snapshot.LatestCheckpoint,
				LatestHandoff:        snapshot.ActiveHandoff,
				LatestLaunch:         snapshot.ActiveLaunch,
				LatestAck:            snapshot.ActiveAcknowledgment,
				LatestFollowThrough:  snapshot.ActiveFollowThrough,
				LatestResolution:     snapshot.LatestResolution,
				LatestRecoveryAction: snapshot.LatestRecoveryAction,
				FreshAnchor:          anchor,
				Outcome:              ContinueOutcomeRunInProgress,
				Reason:               fmt.Sprintf("latest run %s is actively executing in the local runtime", snapshot.LatestRun.RunID),
				Issues:               issues,
				DriftClass:           checkpoint.DriftNone,
				RequiresMutation:     false,
			}, nil
		}
		return continueAssessment{
			TaskID:               taskID,
			Capsule:              caps,
			LatestRun:            snapshot.LatestRun,
			LatestCheckpoint:     snapshot.LatestCheckpoint,
			LatestHandoff:        snapshot.ActiveHandoff,
			LatestLaunch:         snapshot.ActiveLaunch,
			LatestAck:            snapshot.ActiveAcknowledgment,
			LatestFollowThrough:  snapshot.ActiveFollowThrough,
			LatestResolution:     snapshot.LatestResolution,
			LatestRecoveryAction: snapshot.LatestRecoveryAction,
			FreshAnchor:          anchor,
			Outcome:              ContinueOutcomeStaleReconciled,
			Reason:               "latest run is durably RUNNING and requires explicit stale reconciliation",
			Issues:               issues,
			DriftClass:           checkpoint.DriftNone,
			RequiresMutation:     true,
		}, nil
	}

	baseline := anchorFromCapsule(caps)
	if snapshot.LatestCheckpoint != nil {
		baseline = snapshot.LatestCheckpoint.Anchor
	}
	drift := classifyAnchorDrift(baseline, anchor)

	if caps.CurrentPhase == phase.PhaseAwaitingDecision {
		reuse := c.canReuseDecisionCheckpoint(caps, snapshot.LatestCheckpoint, anchor)
		outcome := ContinueOutcomeNeedsDecision
		reason := "task is already in decision-gated resume state"
		if drift == checkpoint.DriftMajor {
			outcome = ContinueOutcomeBlockedDrift
			reason = "task remains decision-gated with major repo drift"
		}
		return continueAssessment{
			TaskID:               taskID,
			Capsule:              caps,
			LatestRun:            snapshot.LatestRun,
			LatestCheckpoint:     snapshot.LatestCheckpoint,
			LatestHandoff:        snapshot.ActiveHandoff,
			LatestLaunch:         snapshot.ActiveLaunch,
			LatestAck:            snapshot.ActiveAcknowledgment,
			LatestFollowThrough:  snapshot.ActiveFollowThrough,
			LatestResolution:     snapshot.LatestResolution,
			LatestRecoveryAction: snapshot.LatestRecoveryAction,
			FreshAnchor:          anchor,
			Outcome:              outcome,
			Reason:               reason,
			Issues:               issues,
			DriftClass:           drift,
			RequiresMutation:     !reuse,
			ReuseCheckpointID:    reusableCheckpointID(snapshot.LatestCheckpoint, reuse),
		}, nil
	}

	if drift == checkpoint.DriftMajor {
		reuse := c.canReuseDecisionCheckpoint(caps, snapshot.LatestCheckpoint, anchor)
		return continueAssessment{
			TaskID:               taskID,
			Capsule:              caps,
			LatestRun:            snapshot.LatestRun,
			LatestCheckpoint:     snapshot.LatestCheckpoint,
			LatestHandoff:        snapshot.ActiveHandoff,
			LatestLaunch:         snapshot.ActiveLaunch,
			LatestAck:            snapshot.ActiveAcknowledgment,
			LatestFollowThrough:  snapshot.ActiveFollowThrough,
			LatestResolution:     snapshot.LatestResolution,
			LatestRecoveryAction: snapshot.LatestRecoveryAction,
			FreshAnchor:          anchor,
			Outcome:              ContinueOutcomeBlockedDrift,
			Reason:               "major repo drift blocks direct resume",
			Issues:               issues,
			DriftClass:           drift,
			RequiresMutation:     !reuse,
			ReuseCheckpointID:    reusableCheckpointID(snapshot.LatestCheckpoint, reuse),
		}, nil
	}
	if drift == checkpoint.DriftMinor {
		reuse := c.canReuseDecisionCheckpoint(caps, snapshot.LatestCheckpoint, anchor)
		return continueAssessment{
			TaskID:               taskID,
			Capsule:              caps,
			LatestRun:            snapshot.LatestRun,
			LatestCheckpoint:     snapshot.LatestCheckpoint,
			LatestHandoff:        snapshot.ActiveHandoff,
			LatestLaunch:         snapshot.ActiveLaunch,
			LatestAck:            snapshot.ActiveAcknowledgment,
			LatestFollowThrough:  snapshot.ActiveFollowThrough,
			LatestResolution:     snapshot.LatestResolution,
			LatestRecoveryAction: snapshot.LatestRecoveryAction,
			FreshAnchor:          anchor,
			Outcome:              ContinueOutcomeNeedsDecision,
			Reason:               "minor repo drift requires explicit decision",
			Issues:               issues,
			DriftClass:           drift,
			RequiresMutation:     !reuse,
			ReuseCheckpointID:    reusableCheckpointID(snapshot.LatestCheckpoint, reuse),
		}, nil
	}

	reuseSafe := c.canReuseSafeCheckpoint(caps, snapshot.LatestRun, snapshot.LatestCheckpoint, anchor)
	return continueAssessment{
		TaskID:               taskID,
		Capsule:              caps,
		LatestRun:            snapshot.LatestRun,
		LatestCheckpoint:     snapshot.LatestCheckpoint,
		LatestHandoff:        snapshot.ActiveHandoff,
		LatestLaunch:         snapshot.ActiveLaunch,
		LatestAck:            snapshot.ActiveAcknowledgment,
		LatestFollowThrough:  snapshot.ActiveFollowThrough,
		LatestResolution:     snapshot.LatestResolution,
		LatestRecoveryAction: snapshot.LatestRecoveryAction,
		FreshAnchor:          anchor,
		Outcome:              ContinueOutcomeSafe,
		Reason:               "safe resume is available from continuity state",
		Issues:               issues,
		DriftClass:           checkpoint.DriftNone,
		RequiresMutation:     !reuseSafe,
		ReuseCheckpointID:    reusableCheckpointID(snapshot.LatestCheckpoint, reuseSafe),
	}, nil
}

func reusableCheckpointID(cp *checkpoint.Checkpoint, ok bool) common.CheckpointID {
	if !ok || cp == nil {
		return ""
	}
	return cp.CheckpointID
}

func (c *Coordinator) finalizeContinue(ctx context.Context, assessment continueAssessment, recovery RecoveryAssessment, out *ContinueTaskResult) error {
	caps, err := c.store.Capsules().Get(assessment.TaskID)
	if err != nil {
		return err
	}
	if caps.Version != assessment.Capsule.Version {
		return c.blockedContinueByInconsistency(ctx, caps, fmt.Sprintf("task state changed during continue assessment (capsule version %d -> %d)", assessment.Capsule.Version, caps.Version), out)
	}
	caps.BranchName = assessment.FreshAnchor.Branch
	caps.HeadSHA = assessment.FreshAnchor.HeadSHA
	caps.WorkingTreeDirty = assessment.FreshAnchor.WorkingTreeDirty
	caps.AnchorCapturedAt = assessment.FreshAnchor.CapturedAt

	switch assessment.Outcome {
	case ContinueOutcomeStaleReconciled:
		if assessment.LatestRun == nil {
			return c.blockedContinueByInconsistency(ctx, caps, "stale reconciliation requested without latest run", out)
		}
		runRec, err := c.store.Runs().Get(assessment.LatestRun.RunID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return c.blockedContinueByInconsistency(ctx, caps, "latest run referenced by assessment is missing", out)
			}
			return err
		}
		if runRec.Status != rundomain.StatusRunning {
			return c.blockedContinueByInconsistency(ctx, caps, fmt.Sprintf("latest run %s is not RUNNING (status=%s)", runRec.RunID, runRec.Status), out)
		}
		return c.reconcileStaleRun(ctx, caps, runRec, out)

	case ContinueOutcomeBlockedDrift:
		return c.blockedContinueByDrift(ctx, caps, assessment.DriftClass, out)

	case ContinueOutcomeNeedsDecision:
		return c.awaitDecisionOnContinue(ctx, caps, assessment.DriftClass, out)

	case ContinueOutcomeBlockedInconsistent:
		return c.blockedContinueByInconsistency(ctx, caps, assessment.Reason, out)

	case ContinueOutcomeSafe:
		var hasCheckpoint bool
		var cp checkpoint.Checkpoint
		if assessment.LatestCheckpoint != nil {
			hasCheckpoint = true
			cp = *assessment.LatestCheckpoint
		}
		var hasRun bool
		var runRec rundomain.ExecutionRun
		if assessment.LatestRun != nil {
			hasRun = true
			runRec = *assessment.LatestRun
		}
		return c.safeContinue(ctx, caps, hasCheckpoint, cp, hasRun, runRec, recovery, out)

	default:
		return c.blockedContinueByInconsistency(ctx, caps, fmt.Sprintf("unsupported continue outcome: %s", assessment.Outcome), out)
	}
}

func (c *Coordinator) noMutationContinueResult(assessment continueAssessment, recovery RecoveryAssessment) ContinueTaskResult {
	caps := assessment.Capsule
	checkpointID := assessment.ReuseCheckpointID
	resumeDescriptor := ""
	if assessment.LatestCheckpoint != nil {
		resumeDescriptor = assessment.LatestCheckpoint.ResumeDescriptor
	}
	runID := common.RunID("")
	if assessment.LatestRun != nil {
		runID = assessment.LatestRun.RunID
	}
	base := ContinueTaskResult{
		TaskID:           caps.TaskID,
		Outcome:          assessment.Outcome,
		DriftClass:       assessment.DriftClass,
		Phase:            caps.CurrentPhase,
		RunID:            runID,
		CheckpointID:     checkpointID,
		ResumeDescriptor: resumeDescriptor,
	}
	applyRecoveryAssessmentToContinueResult(&base, recovery)
	switch assessment.Outcome {
	case ContinueOutcomeSafe:
		switch recovery.RecoveryClass {
		case RecoveryClassInterruptedRunRecoverable:
			base.CanonicalResponse = fmt.Sprintf(
				"Interrupted execution is already recoverable from checkpoint %s using brief %s on branch %s (head %s). No new checkpoint was created because the interrupted recovery state is unchanged; resume the interrupted execution path from that checkpoint.",
				checkpointID,
				caps.CurrentBriefID,
				assessment.FreshAnchor.Branch,
				assessment.FreshAnchor.HeadSHA,
			)
		case RecoveryClassAcceptedHandoffLaunchReady:
			base.CanonicalResponse = fmt.Sprintf(
				"Continuity is intact and accepted handoff %s is ready to launch. No new checkpoint was created because the handoff-based recovery state is unchanged.",
				recovery.HandoffID,
			)
		case RecoveryClassHandoffLaunchPendingOutcome:
			base.CanonicalResponse = fmt.Sprintf(
				"Continuity is intact, but handoff launch is not retryable yet: %s. No new checkpoint was created.",
				recovery.Reason,
			)
		case RecoveryClassHandoffLaunchCompleted:
			base.CanonicalResponse = fmt.Sprintf(
				"Continuity is intact, and the latest handoff launch step is already complete: %s. No new checkpoint was created.",
				recovery.Reason,
			)
		case RecoveryClassFailedRunReviewRequired:
			base.CanonicalResponse = fmt.Sprintf(
				"Continuity is intact, but the next run is not ready because latest run %s failed. Review failure evidence before retrying or regenerating the brief. No new checkpoint was created.",
				runID,
			)
		case RecoveryClassContinueExecutionRequired:
			base.CanonicalResponse = "Continuity is intact, but the current brief is not yet cleared for execution. Explicit continue finalization must happen before the next bounded run. No new checkpoint was created."
		case RecoveryClassValidationReviewRequired:
			base.CanonicalResponse = fmt.Sprintf(
				"Continuity is intact, but the task is still in validation review after run %s. Review validation state before starting another bounded run. No new checkpoint was created.",
				runID,
			)
		case RecoveryClassCompletedNoAction:
			base.CanonicalResponse = "Continuity is intact, and the task is already completed. No recovery action was taken."
		case RecoveryClassRepairRequired:
			base.CanonicalResponse = fmt.Sprintf(
				"Continuity facts are present, but deterministic recovery is not ready: %s. No new checkpoint was created.",
				recovery.Reason,
			)
		case RecoveryClassRebriefRequired:
			base.CanonicalResponse = "Continuity is intact, but the next run is blocked until the execution brief is regenerated or replaced. No new checkpoint was created."
		case RecoveryClassReadyNextRun:
			base.CanonicalResponse = fmt.Sprintf(
				"Fresh next bounded run is already ready from checkpoint %s using brief %s on branch %s (head %s). No new checkpoint was created because the local recovery boundary is unchanged.",
				checkpointID,
				caps.CurrentBriefID,
				assessment.FreshAnchor.Branch,
				assessment.FreshAnchor.HeadSHA,
			)
		default:
			base.CanonicalResponse = "Continuity is intact. No new checkpoint was created because recovery state is unchanged."
		}
		return base
	case ContinueOutcomeRunInProgress:
		base.CanonicalResponse = fmt.Sprintf(
			"Local run %s is still actively executing. No recovery mutation was applied; wait for the current run to finish before reconciling, validating, or starting another bounded run.",
			runID,
		)
		return base
	case ContinueOutcomeNeedsDecision:
		base.CanonicalResponse = fmt.Sprintf(
			"Resume still requires a decision. I reused checkpoint %s and did not create a new one because the decision-gated continuity state is unchanged.",
			checkpointID,
		)
		return base
	case ContinueOutcomeBlockedDrift:
		base.CanonicalResponse = fmt.Sprintf(
			"Direct resume is still blocked by major repo drift. I reused checkpoint %s and did not create a new continuity record because state is unchanged.",
			checkpointID,
		)
		return base
	case ContinueOutcomeBlockedInconsistent:
		base.CanonicalResponse = fmt.Sprintf(
			"Resume remains blocked due to inconsistent continuity state. I reused checkpoint %s and did not create a new one because the blocked state is unchanged.",
			checkpointID,
		)
		return base
	default:
		base.CanonicalResponse = "Continue assessment completed with no state mutation."
		return base
	}
}

func (c *Coordinator) validateContinueConsistency(snapshot continuitySnapshot) (string, error) {
	violations, err := c.validateContinuitySnapshot(snapshot)
	if err != nil {
		return "", err
	}
	return firstContinuityViolationMessage(violations), nil
}

func (c *Coordinator) canReuseSafeCheckpoint(caps capsule.WorkCapsule, latestRun *rundomain.ExecutionRun, latestCheckpoint *checkpoint.Checkpoint, currentAnchor anchorgit.Snapshot) bool {
	if latestCheckpoint == nil || !latestCheckpoint.IsResumable {
		return false
	}
	if latestCheckpoint.TaskID != caps.TaskID {
		return false
	}
	if latestCheckpoint.BriefID != caps.CurrentBriefID {
		return false
	}
	if latestCheckpoint.IntentID != caps.CurrentIntentID {
		return false
	}
	if latestCheckpoint.Phase != caps.CurrentPhase {
		return false
	}
	if latestCheckpoint.CapsuleVersion != caps.Version {
		return false
	}
	if latestCheckpoint.Anchor.RepoRoot != currentAnchor.RepoRoot {
		return false
	}
	if latestCheckpoint.Anchor.BranchName != currentAnchor.Branch {
		return false
	}
	if latestCheckpoint.Anchor.HeadSHA != currentAnchor.HeadSHA {
		return false
	}
	if latestCheckpoint.Anchor.DirtyHash != boolString(currentAnchor.WorkingTreeDirty) {
		return false
	}
	if latestRun != nil && latestCheckpoint.RunID != "" && latestCheckpoint.RunID != latestRun.RunID {
		return false
	}
	return true
}

func (c *Coordinator) canReuseDecisionCheckpoint(caps capsule.WorkCapsule, latestCheckpoint *checkpoint.Checkpoint, currentAnchor anchorgit.Snapshot) bool {
	if latestCheckpoint == nil {
		return false
	}
	if latestCheckpoint.TaskID != caps.TaskID {
		return false
	}
	if latestCheckpoint.IsResumable {
		return false
	}
	if latestCheckpoint.Phase != phase.PhaseAwaitingDecision || caps.CurrentPhase != phase.PhaseAwaitingDecision {
		return false
	}
	if latestCheckpoint.Anchor.RepoRoot != currentAnchor.RepoRoot {
		return false
	}
	if latestCheckpoint.Anchor.BranchName != currentAnchor.Branch {
		return false
	}
	if latestCheckpoint.Anchor.HeadSHA != currentAnchor.HeadSHA {
		return false
	}
	if latestCheckpoint.Anchor.DirtyHash != boolString(currentAnchor.WorkingTreeDirty) {
		return false
	}
	return true
}

func (c *Coordinator) canReuseInconsistencyCheckpoint(caps capsule.WorkCapsule, latestCheckpoint *checkpoint.Checkpoint, currentAnchor anchorgit.Snapshot, reason string) bool {
	if latestCheckpoint == nil {
		return false
	}
	if latestCheckpoint.TaskID != caps.TaskID {
		return false
	}
	if latestCheckpoint.IsResumable {
		return false
	}
	if latestCheckpoint.Phase != phase.PhaseBlocked || caps.CurrentPhase != phase.PhaseBlocked {
		return false
	}
	if latestCheckpoint.Anchor.RepoRoot != currentAnchor.RepoRoot {
		return false
	}
	if latestCheckpoint.Anchor.BranchName != currentAnchor.Branch {
		return false
	}
	if latestCheckpoint.Anchor.HeadSHA != currentAnchor.HeadSHA {
		return false
	}
	if latestCheckpoint.Anchor.DirtyHash != boolString(currentAnchor.WorkingTreeDirty) {
		return false
	}
	return strings.Contains(strings.ToLower(latestCheckpoint.ResumeDescriptor), strings.ToLower(reason))
}

type preparedRealRun struct {
	TaskID      common.TaskID
	RunID       common.RunID
	Brief       brief.ExecutionBrief
	ContextPack contextdomain.Pack
	TaskMemory  taskmemory.Snapshot
	Capsule     capsule.WorkCapsule
}

func (c *Coordinator) assessRunStartRecovery(ctx context.Context, taskID common.TaskID) (RecoveryAssessment, bool, string, error) {
	assessment, err := c.assessContinue(ctx, taskID)
	if err != nil {
		return RecoveryAssessment{}, false, "", err
	}
	recovery := c.recoveryFromContinueAssessment(assessment)
	branch := deriveActiveBranchProvenanceFromAssessment(assessment, recovery)
	runFinalization := deriveLocalRunFinalization(assessment, recovery)
	localResume := deriveLocalResumeAuthority(assessment, recovery)
	actions := deriveOperatorActionAuthoritySet(assessment, recovery, branch, runFinalization, localResume)
	allowed, canonical := runStartEligibility(recovery, actions)
	return recovery, allowed, canonical, nil
}

func (c *Coordinator) startRunRealStaged(ctx context.Context, req RunTaskRequest) (RunTaskResult, error) {
	prepared, immediateResult, err := c.prepareRealRun(ctx, req)
	if err != nil {
		return RunTaskResult{}, err
	}
	if immediateResult != nil {
		return *immediateResult, nil
	}

	c.markRunActive(prepared.TaskID, prepared.RunID)
	defer c.clearRunActive(prepared.TaskID, prepared.RunID)

	execReq := c.buildExecutionRequest(prepared)
	execResult, execErr := c.workerAdapter.Execute(ctx, execReq, nil)
	if execErr == nil && execResult.ExitCode == 0 {
		execResult = c.enrichExecutionResultWithValidation(ctx, prepared, execResult)
	}

	finalResult, finalizeErr := c.finalizeRealRun(ctx, prepared, execResult, execErr)
	if finalizeErr != nil {
		return RunTaskResult{}, fmt.Errorf("finalize run %s after worker execution: %w", prepared.RunID, finalizeErr)
	}
	return finalResult, nil
}

func (c *Coordinator) prepareRealRun(ctx context.Context, req RunTaskRequest) (*preparedRealRun, *RunTaskResult, error) {
	var prepared preparedRealRun
	var immediate *RunTaskResult
	err := c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(common.TaskID(req.TaskID))
		if err != nil {
			return err
		}
		if caps.CurrentBriefID == "" {
			canonical := "Execution cannot start yet because no execution brief is available. Send a task message first so Tuku can compile intent and create a brief."
			if err := txc.emitCanonicalConversation(caps, canonical, map[string]any{"reason": "missing_brief"}, nil); err != nil {
				return err
			}
			out := RunTaskResult{TaskID: caps.TaskID, Phase: caps.CurrentPhase, RunStatus: rundomain.StatusFailed, CanonicalResponse: canonical}
			immediate = &out
			return nil
		}
		recovery, allowed, canonical, err := txc.assessRunStartRecovery(ctx, caps.TaskID)
		if err != nil {
			return err
		}
		if !allowed {
			payload := map[string]any{
				"reason":                   "recovery_gate_blocked",
				"recovery_class":           recovery.RecoveryClass,
				"recommended_action":       recovery.RecommendedAction,
				"ready_for_next_run":       recovery.ReadyForNextRun,
				"ready_for_handoff_launch": recovery.ReadyForHandoffLaunch,
				"recovery_reason":          recovery.Reason,
			}
			if err := txc.emitCanonicalConversation(caps, canonical, payload, nil); err != nil {
				return err
			}
			out := RunTaskResult{TaskID: caps.TaskID, Phase: caps.CurrentPhase, RunStatus: rundomain.StatusFailed, CanonicalResponse: canonical}
			immediate = &out
			return nil
		}
		if txc.workerAdapter == nil {
			canonical := "Execution adapter is not configured. Tuku cannot run Codex in real mode yet."
			if err := txc.emitCanonicalConversation(caps, canonical, map[string]any{"reason": "missing_worker_adapter"}, nil); err != nil {
				return err
			}
			out := RunTaskResult{TaskID: caps.TaskID, Phase: caps.CurrentPhase, RunStatus: rundomain.StatusFailed, CanonicalResponse: canonical}
			immediate = &out
			return nil
		}

		b, err := txc.store.Briefs().Get(caps.CurrentBriefID)
		if err != nil {
			return err
		}
		contextPack, err := txc.resolveExecutionContextPack(caps, b)
		if err != nil {
			return err
		}
		if err := txc.recordRunStartPolicyDecision(caps, b, contextPack); err != nil {
			return err
		}
		anchor := txc.anchorProvider.Capture(ctx, caps.RepoRoot)
		caps.BranchName = anchor.Branch
		caps.HeadSHA = anchor.HeadSHA
		caps.WorkingTreeDirty = anchor.WorkingTreeDirty
		caps.AnchorCapturedAt = anchor.CapturedAt

		now := txc.clock()
		runID := req.RunID
		if runID == "" {
			runID = common.RunID(txc.idGenerator("run"))
		}
		shellSessionID := strings.TrimSpace(req.ShellSessionID)
		if shellSessionID == "" {
			shellSessionID = txc.inferActiveShellSessionID(caps.TaskID)
		}
		initialExitCode := -1
		runRec := rundomain.ExecutionRun{
			RunID:              runID,
			TaskID:             caps.TaskID,
			BriefID:            b.BriefID,
			WorkerKind:         rundomain.WorkerKindCodex,
			ShellSessionID:     shellSessionID,
			Status:             rundomain.StatusRunning,
			ExitCode:           &initialExitCode,
			StartedAt:          now,
			CreatedFromPhase:   caps.CurrentPhase,
			LastKnownSummary:   "Codex execution started",
			CreatedAt:          now,
			UpdatedAt:          now,
			InterruptionReason: "",
		}
		if err := txc.store.Runs().Create(runRec); err != nil {
			return err
		}

		caps.Version++
		caps.UpdatedAt = txc.clock()
		caps.CurrentPhase = phase.PhaseExecuting
		caps.NextAction = "Real execution run is in progress."
		if err := txc.store.Capsules().Update(caps); err != nil {
			return err
		}
		if err := txc.appendProof(caps, proof.EventWorkerRunStarted, proof.ActorSystem, "tuku-runner", map[string]any{
			"run_id":      runID,
			"brief_id":    b.BriefID,
			"worker_kind": runRec.WorkerKind,
			"mode":        "real",
		}, &runID); err != nil {
			return err
		}
		if err := txc.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": caps.CurrentPhase, "reason": "real codex run started"}, &runID); err != nil {
			return err
		}
		if _, err := txc.createCheckpointWithOptions(caps, runID, checkpoint.TriggerBeforeExecution, true, "Run started and durably marked RUNNING before worker execution.", false); err != nil {
			return err
		}

		taskMemorySnapshot, err := txc.resolveExecutionTaskMemory(caps, b, contextPack)
		if err != nil {
			return err
		}
		prepared = preparedRealRun{
			TaskID:      caps.TaskID,
			RunID:       runID,
			Brief:       b,
			ContextPack: contextPack,
			TaskMemory:  taskMemorySnapshot,
			Capsule:     caps,
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if immediate != nil {
		return nil, immediate, nil
	}
	return &prepared, nil, nil
}

func (c *Coordinator) buildExecutionRequest(prepared *preparedRealRun) adapter_contract.ExecutionRequest {
	agentsChecksum, agentsInstructions := agentsMetadata(prepared.Capsule.RepoRoot)
	return adapter_contract.ExecutionRequest{
		RunID:       prepared.RunID,
		TaskID:      prepared.TaskID,
		Worker:      adapter_contract.WorkerCodex,
		Brief:       prepared.Brief,
		ContextPack: prepared.ContextPack,
		TaskMemory:  prepared.TaskMemory,
		RepoAnchor: checkpoint.RepoAnchor{
			RepoRoot:      prepared.Capsule.RepoRoot,
			WorktreePath:  prepared.Capsule.WorktreePath,
			BranchName:    prepared.Capsule.BranchName,
			HeadSHA:       prepared.Capsule.HeadSHA,
			DirtyHash:     boolString(prepared.Capsule.WorkingTreeDirty),
			UntrackedHash: "",
		},
		PolicyProfileID:    prepared.Brief.PolicyProfileID,
		AgentsChecksum:     agentsChecksum,
		AgentsInstructions: agentsInstructions,
		ContextSummary:     fmt.Sprintf("phase=%s context_files=%d context_snippets=%d memory_ratio=%.2fx history_tokens=%d resume_tokens=%d", prepared.Capsule.CurrentPhase, len(prepared.ContextPack.IncludedFiles), len(prepared.ContextPack.IncludedSnippets), prepared.TaskMemory.MemoryCompactionRatio, prepared.TaskMemory.FullHistoryTokenEstimate, prepared.TaskMemory.ResumePromptTokenEstimate),
	}
}

func (c *Coordinator) finalizeRealRun(ctx context.Context, prepared *preparedRealRun, execResult adapter_contract.ExecutionResult, execErr error) (RunTaskResult, error) {
	var result RunTaskResult
	err := c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(prepared.TaskID)
		if err != nil {
			return err
		}
		runRec, err := txc.store.Runs().Get(prepared.RunID)
		if err != nil {
			return err
		}
		if runRec.Status != rundomain.StatusRunning {
			return fmt.Errorf("run %s is not RUNNING during finalization (status=%s)", runRec.RunID, runRec.Status)
		}

		if err := txc.appendProof(caps, proof.EventWorkerOutputCaptured, proof.ActorSystem, "tuku-runner", map[string]any{
			"run_id":                  prepared.RunID,
			"exit_code":               execResult.ExitCode,
			"started_at_unix_ms":      execResult.StartedAt.UnixMilli(),
			"ended_at_unix_ms":        execResult.EndedAt.UnixMilli(),
			"stdout_excerpt":          truncate(execResult.Stdout, 2000),
			"stderr_excerpt":          truncate(execResult.Stderr, 2000),
			"changed_files":           execResult.ChangedFiles,
			"changed_files_semantics": execResult.ChangedFilesSemantics,
			"repo_diff_summary":       repoDiffSummaryFromExecution(execResult),
			"worktree_summary":        worktreeSummaryFromExecution(execResult),
			"validation_signals":      execResult.ValidationSignals,
			"summary":                 execResult.Summary,
			"error_message":           execResult.ErrorMessage,
		}, &prepared.RunID); err != nil {
			return err
		}
		if len(execResult.ChangedFiles) > 0 {
			if err := txc.appendProof(caps, proof.EventFileChangeDetected, proof.ActorSystem, "tuku-runner", map[string]any{
				"run_id":                  prepared.RunID,
				"changed_files":           execResult.ChangedFiles,
				"changed_files_semantics": execResult.ChangedFilesSemantics,
				"repo_diff_summary":       repoDiffSummaryFromExecution(execResult),
				"worktree_summary":        worktreeSummaryFromExecution(execResult),
				"count":                   len(execResult.ChangedFiles),
			}, &prepared.RunID); err != nil {
				return err
			}
		}

		if execErr != nil {
			result, err = txc.markRunFailed(ctx, caps, runRec, execResult, execErr)
			if err != nil {
				return err
			}
			finalRun, err := txc.store.Runs().Get(prepared.RunID)
			if err != nil {
				return err
			}
			return txc.updateBenchmarkOutcome(prepared.Brief, finalRun)
		}
		if execResult.ExitCode != 0 {
			result, err = txc.markRunFailed(ctx, caps, runRec, execResult, fmt.Errorf("codex exit code %d", execResult.ExitCode))
			if err != nil {
				return err
			}
			finalRun, err := txc.store.Runs().Get(prepared.RunID)
			if err != nil {
				return err
			}
			return txc.updateBenchmarkOutcome(prepared.Brief, finalRun)
		}
		result, err = txc.markRunCompleted(ctx, caps, runRec, execResult)
		if err != nil {
			return err
		}
		finalRun, err := txc.store.Runs().Get(prepared.RunID)
		if err != nil {
			return err
		}
		return txc.updateBenchmarkOutcome(prepared.Brief, finalRun)
	})
	if err != nil {
		return RunTaskResult{}, err
	}
	return result, nil
}

func (c *Coordinator) startRunNoop(ctx context.Context, caps capsule.WorkCapsule, req RunTaskRequest) (RunTaskResult, error) {
	if caps.CurrentBriefID == "" {
		canonical := "Execution cannot start yet because no execution brief is available. Send a task message first so Tuku can compile intent and create a brief."
		if err := c.emitCanonicalConversation(caps, canonical, map[string]any{"reason": "missing_brief"}, nil); err != nil {
			return RunTaskResult{}, err
		}
		return RunTaskResult{TaskID: caps.TaskID, Phase: caps.CurrentPhase, RunStatus: rundomain.StatusFailed, CanonicalResponse: canonical}, nil
	}
	recovery, allowed, canonical, err := c.assessRunStartRecovery(ctx, caps.TaskID)
	if err != nil {
		return RunTaskResult{}, err
	}
	if !allowed {
		payload := map[string]any{
			"reason":                   "recovery_gate_blocked",
			"recovery_class":           recovery.RecoveryClass,
			"recommended_action":       recovery.RecommendedAction,
			"ready_for_next_run":       recovery.ReadyForNextRun,
			"ready_for_handoff_launch": recovery.ReadyForHandoffLaunch,
			"recovery_reason":          recovery.Reason,
		}
		if err := c.emitCanonicalConversation(caps, canonical, payload, nil); err != nil {
			return RunTaskResult{}, err
		}
		return RunTaskResult{TaskID: caps.TaskID, Phase: caps.CurrentPhase, RunStatus: rundomain.StatusFailed, CanonicalResponse: canonical}, nil
	}

	b, err := c.store.Briefs().Get(caps.CurrentBriefID)
	if err != nil {
		return RunTaskResult{}, err
	}

	now := c.clock()
	runID := req.RunID
	if runID == "" {
		runID = common.RunID(c.idGenerator("run"))
	}
	shellSessionID := strings.TrimSpace(req.ShellSessionID)
	if shellSessionID == "" {
		shellSessionID = c.inferActiveShellSessionID(caps.TaskID)
	}
	initialExitCode := -1
	r := rundomain.ExecutionRun{
		RunID:              runID,
		TaskID:             caps.TaskID,
		BriefID:            b.BriefID,
		WorkerKind:         rundomain.WorkerKindNoop,
		ShellSessionID:     shellSessionID,
		Status:             rundomain.StatusCreated,
		ExitCode:           &initialExitCode,
		StartedAt:          now,
		CreatedFromPhase:   caps.CurrentPhase,
		LastKnownSummary:   "No-op run created and awaiting placeholder execution",
		CreatedAt:          now,
		UpdatedAt:          now,
		InterruptionReason: "",
	}
	if err := c.store.Runs().Create(r); err != nil {
		return RunTaskResult{}, err
	}

	r.Status = rundomain.StatusRunning
	r.LastKnownSummary = "No-op execution placeholder started"
	r.UpdatedAt = c.clock()
	if err := c.store.Runs().Update(r); err != nil {
		return RunTaskResult{}, err
	}
	if b, err := c.store.Briefs().Get(r.BriefID); err == nil {
		if err := c.updateBenchmarkOutcome(b, r); err != nil {
			return RunTaskResult{}, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return RunTaskResult{}, err
	}

	caps.Version++
	caps.UpdatedAt = c.clock()
	caps.CurrentPhase = phase.PhaseExecuting
	caps.NextAction = "No-op run is active. Complete with `tuku run --task <id> --action complete` or interrupt with `--action interrupt`."
	if err := c.store.Capsules().Update(caps); err != nil {
		return RunTaskResult{}, err
	}

	if err := c.appendProof(caps, proof.EventWorkerRunStarted, proof.ActorSystem, "tuku-runner", map[string]any{"run_id": runID, "brief_id": b.BriefID, "worker_kind": r.WorkerKind, "mode": "noop"}, &runID); err != nil {
		return RunTaskResult{}, err
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": caps.CurrentPhase, "reason": "execution run started"}, &runID); err != nil {
		return RunTaskResult{}, err
	}
	if _, err := c.createCheckpoint(caps, runID, checkpoint.TriggerBeforeExecution, true, "No-op run entered RUNNING state."); err != nil {
		return RunTaskResult{}, err
	}

	if req.SimulateInterrupt {
		interruptReq := RunTaskRequest{TaskID: string(caps.TaskID), Action: "interrupt", RunID: runID, InterruptionReason: "simulated interruption"}
		return c.interruptRun(ctx, caps, interruptReq)
	}

	recentEvents, err := c.store.Proofs().ListByTask(caps.TaskID, 12)
	if err != nil {
		return RunTaskResult{}, err
	}
	canonicalText, err := c.synthesizer.Synthesize(ctx, caps, recentEvents)
	if err != nil {
		return RunTaskResult{}, err
	}
	if err := c.emitCanonicalConversation(caps, canonicalText, map[string]any{"run_id": runID, "status": r.Status}, &runID); err != nil {
		return RunTaskResult{}, err
	}

	return RunTaskResult{TaskID: caps.TaskID, RunID: runID, RunStatus: r.Status, Phase: caps.CurrentPhase, CanonicalResponse: canonicalText}, nil
}

func (c *Coordinator) completeRun(ctx context.Context, caps capsule.WorkCapsule, req RunTaskRequest) (RunTaskResult, error) {
	r, err := c.resolveRunForAction(caps.TaskID, req.RunID)
	if err != nil {
		canonical := "Execution cannot complete because there is no active run for this task."
		if emitErr := c.emitCanonicalConversation(caps, canonical, map[string]any{"reason": "missing_running_run"}, nil); emitErr != nil {
			return RunTaskResult{}, emitErr
		}
		return RunTaskResult{TaskID: caps.TaskID, Phase: caps.CurrentPhase, RunStatus: rundomain.StatusFailed, CanonicalResponse: canonical}, nil
	}

	now := c.clock()
	r.Status = rundomain.StatusCompleted
	r.LastKnownSummary = "Execution placeholder completed"
	r.EndedAt = &now
	r.UpdatedAt = now
	if err := c.store.Runs().Update(r); err != nil {
		return RunTaskResult{}, err
	}
	if b, err := c.store.Briefs().Get(r.BriefID); err == nil {
		if err := c.updateBenchmarkOutcome(b, r); err != nil {
			return RunTaskResult{}, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return RunTaskResult{}, err
	}

	caps.Version++
	caps.UpdatedAt = now
	caps.CurrentPhase = phase.PhaseValidating
	caps.NextAction = "Execution placeholder completed. Validation logic is deferred to the next milestone."
	if err := c.store.Capsules().Update(caps); err != nil {
		return RunTaskResult{}, err
	}
	taskMemorySnapshot, err := c.refreshTaskMemoryForCurrentState(caps, &r, "run_completed_noop")
	if err != nil {
		return RunTaskResult{}, err
	}

	if err := c.appendProof(caps, proof.EventWorkerRunCompleted, proof.ActorSystem, "tuku-runner", map[string]any{"run_id": r.RunID, "status": r.Status}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	if taskMemorySnapshot != nil {
		if err := c.appendProof(caps, proof.EventTaskMemoryUpdated, proof.ActorSystem, "tuku-task-memory", map[string]any{
			"memory_id":               taskMemorySnapshot.MemoryID,
			"source":                  taskMemorySnapshot.Source,
			"run_id":                  r.RunID,
			"full_history_tokens":     taskMemorySnapshot.FullHistoryTokenEstimate,
			"resume_prompt_tokens":    taskMemorySnapshot.ResumePromptTokenEstimate,
			"memory_compaction_ratio": taskMemorySnapshot.MemoryCompactionRatio,
		}, &r.RunID); err != nil {
			return RunTaskResult{}, err
		}
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": caps.CurrentPhase, "reason": "run completed"}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	if _, err := c.createCheckpoint(caps, r.RunID, checkpoint.TriggerAfterExecution, true, "Run completed and task moved to validation."); err != nil {
		return RunTaskResult{}, err
	}

	recentEvents, err := c.store.Proofs().ListByTask(caps.TaskID, 12)
	if err != nil {
		return RunTaskResult{}, err
	}
	canonicalText, err := c.synthesizer.Synthesize(ctx, caps, recentEvents)
	if err != nil {
		return RunTaskResult{}, err
	}
	if err := c.emitCanonicalConversation(caps, canonicalText, map[string]any{"run_id": r.RunID, "status": r.Status}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}

	return RunTaskResult{TaskID: caps.TaskID, RunID: r.RunID, RunStatus: r.Status, Phase: caps.CurrentPhase, CanonicalResponse: canonicalText}, nil
}

func (c *Coordinator) interruptRun(ctx context.Context, caps capsule.WorkCapsule, req RunTaskRequest) (RunTaskResult, error) {
	r, err := c.resolveRunForAction(caps.TaskID, req.RunID)
	if err != nil {
		canonical := "Execution cannot be interrupted because there is no active run for this task."
		if emitErr := c.emitCanonicalConversation(caps, canonical, map[string]any{"reason": "missing_running_run"}, nil); emitErr != nil {
			return RunTaskResult{}, emitErr
		}
		return RunTaskResult{TaskID: caps.TaskID, Phase: caps.CurrentPhase, RunStatus: rundomain.StatusFailed, CanonicalResponse: canonical}, nil
	}

	now := c.clock()
	reason := strings.TrimSpace(req.InterruptionReason)
	if reason == "" {
		reason = "manual interruption"
	}
	r.Status = rundomain.StatusInterrupted
	r.InterruptionReason = reason
	r.LastKnownSummary = "Execution placeholder interrupted"
	r.EndedAt = &now
	r.UpdatedAt = now
	if err := c.store.Runs().Update(r); err != nil {
		return RunTaskResult{}, err
	}

	caps.Version++
	caps.UpdatedAt = now
	caps.CurrentPhase = phase.PhasePaused
	caps.NextAction = "Run interrupted. Use `tuku continue --task <id>` to reconcile and resume safely."
	if err := c.store.Capsules().Update(caps); err != nil {
		return RunTaskResult{}, err
	}
	taskMemorySnapshot, err := c.refreshTaskMemoryForCurrentState(caps, &r, "run_interrupted_noop")
	if err != nil {
		return RunTaskResult{}, err
	}

	if err := c.appendProof(caps, proof.EventRunInterrupted, proof.ActorSystem, "tuku-runner", map[string]any{"run_id": r.RunID, "reason": reason}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	if taskMemorySnapshot != nil {
		if err := c.appendProof(caps, proof.EventTaskMemoryUpdated, proof.ActorSystem, "tuku-task-memory", map[string]any{
			"memory_id":               taskMemorySnapshot.MemoryID,
			"source":                  taskMemorySnapshot.Source,
			"run_id":                  r.RunID,
			"full_history_tokens":     taskMemorySnapshot.FullHistoryTokenEstimate,
			"resume_prompt_tokens":    taskMemorySnapshot.ResumePromptTokenEstimate,
			"memory_compaction_ratio": taskMemorySnapshot.MemoryCompactionRatio,
		}, &r.RunID); err != nil {
			return RunTaskResult{}, err
		}
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": caps.CurrentPhase, "reason": "run interrupted"}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	if _, err := c.createCheckpoint(caps, r.RunID, checkpoint.TriggerInterruption, true, "Run interrupted and task is resumable from paused state."); err != nil {
		return RunTaskResult{}, err
	}

	recentEvents, err := c.store.Proofs().ListByTask(caps.TaskID, 12)
	if err != nil {
		return RunTaskResult{}, err
	}
	canonicalText, err := c.synthesizer.Synthesize(ctx, caps, recentEvents)
	if err != nil {
		return RunTaskResult{}, err
	}
	if err := c.emitCanonicalConversation(caps, canonicalText, map[string]any{"run_id": r.RunID, "status": r.Status, "reason": reason}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}

	return RunTaskResult{TaskID: caps.TaskID, RunID: r.RunID, RunStatus: r.Status, Phase: caps.CurrentPhase, CanonicalResponse: canonicalText}, nil
}

func (c *Coordinator) resolveRunForAction(taskID common.TaskID, preferredRunID common.RunID) (rundomain.ExecutionRun, error) {
	var runRecord rundomain.ExecutionRun
	var err error
	if preferredRunID != "" {
		runRecord, err = c.store.Runs().Get(preferredRunID)
	} else {
		runRecord, err = c.store.Runs().LatestRunningByTask(taskID)
	}
	if err != nil {
		return rundomain.ExecutionRun{}, err
	}
	if runRecord.Status != rundomain.StatusRunning {
		return rundomain.ExecutionRun{}, fmt.Errorf("run %s is not RUNNING (status=%s)", runRecord.RunID, runRecord.Status)
	}
	return runRecord, nil
}

func (c *Coordinator) inferActiveShellSessionID(taskID common.TaskID) string {
	records, err := c.shellSessions.ListByTask(taskID)
	if err != nil || len(records) == 0 {
		return ""
	}
	views := classifyShellSessions(records, c.clock(), c.shellSessionStaleAfter)
	for _, view := range views {
		if !view.Active {
			continue
		}
		if view.SessionClass == ShellSessionClassStale {
			continue
		}
		return view.SessionID
	}
	return ""
}

func applyExecutionEvidence(runRec *rundomain.ExecutionRun, execResult adapter_contract.ExecutionResult) {
	if runRec == nil {
		return
	}
	runRec.WorkerRunID = strings.TrimSpace(string(execResult.WorkerRunID))
	runRec.Command = strings.TrimSpace(execResult.Command)
	runRec.Args = append([]string{}, execResult.Args...)
	runRec.Stdout = execResult.Stdout
	runRec.Stderr = execResult.Stderr
	runRec.ChangedFiles = append([]string{}, execResult.ChangedFiles...)
	runRec.ChangedFilesSemantics = strings.TrimSpace(execResult.ChangedFilesSemantics)
	runRec.RepoDiffSummary = strings.TrimSpace(repoDiffSummaryFromExecution(execResult))
	runRec.WorktreeSummary = strings.TrimSpace(worktreeSummaryFromExecution(execResult))
	runRec.ValidationSignals = append([]string{}, execResult.ValidationSignals...)
	runRec.OutputArtifactRef = strings.TrimSpace(execResult.OutputArtifactRef)
	runRec.StructuredSummary = strings.TrimSpace(execResult.StructuredSummary)
	exitCode := execResult.ExitCode
	runRec.ExitCode = &exitCode
}

func repoDiffSummaryFromExecution(execResult adapter_contract.ExecutionResult) string {
	if summary, ok := extractExecutionStructuredField(execResult.StructuredSummary, "repo_diff_summary"); ok {
		return summary
	}
	return ""
}

func worktreeSummaryFromExecution(execResult adapter_contract.ExecutionResult) string {
	if summary, ok := extractExecutionStructuredField(execResult.StructuredSummary, "worktree_summary"); ok {
		return summary
	}
	return ""
}

func extractExecutionStructuredField(raw string, field string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", false
	}
	value, ok := payload[field]
	if !ok {
		return "", false
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return "", false
	}
	return text, true
}

func (c *Coordinator) markRunCompleted(ctx context.Context, caps capsule.WorkCapsule, r rundomain.ExecutionRun, execResult adapter_contract.ExecutionResult) (RunTaskResult, error) {
	now := c.clock()
	applyExecutionEvidence(&r, execResult)
	r.Status = rundomain.StatusCompleted
	r.LastKnownSummary = execResult.Summary
	r.EndedAt = &now
	r.UpdatedAt = now
	if err := c.store.Runs().Update(r); err != nil {
		return RunTaskResult{}, err
	}

	caps.Version++
	caps.UpdatedAt = now
	caps.CurrentPhase = phase.PhaseValidating
	caps.NextAction = "Codex run completed. Review evidence and decide validation/follow-up."
	if err := c.store.Capsules().Update(caps); err != nil {
		return RunTaskResult{}, err
	}
	taskMemorySnapshot, err := c.refreshTaskMemoryForCurrentState(caps, &r, "run_completed")
	if err != nil {
		return RunTaskResult{}, err
	}

	if err := c.appendProof(caps, proof.EventWorkerRunCompleted, proof.ActorSystem, "tuku-runner", map[string]any{
		"run_id":                  r.RunID,
		"status":                  r.Status,
		"exit_code":               execResult.ExitCode,
		"changed_files":           execResult.ChangedFiles,
		"changed_files_semantics": execResult.ChangedFilesSemantics,
		"repo_diff_summary":       repoDiffSummaryFromExecution(execResult),
		"worktree_summary":        worktreeSummaryFromExecution(execResult),
		"summary":                 execResult.Summary,
		"validation_hints":        execResult.ValidationSignals,
	}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	if taskMemorySnapshot != nil {
		if err := c.appendProof(caps, proof.EventTaskMemoryUpdated, proof.ActorSystem, "tuku-task-memory", map[string]any{
			"memory_id":               taskMemorySnapshot.MemoryID,
			"source":                  taskMemorySnapshot.Source,
			"run_id":                  r.RunID,
			"full_history_tokens":     taskMemorySnapshot.FullHistoryTokenEstimate,
			"resume_prompt_tokens":    taskMemorySnapshot.ResumePromptTokenEstimate,
			"memory_compaction_ratio": taskMemorySnapshot.MemoryCompactionRatio,
		}, &r.RunID); err != nil {
			return RunTaskResult{}, err
		}
	}
	if len(execResult.ValidationSignals) > 0 {
		if err := c.appendProof(caps, proof.EventValidationResult, proof.ActorSystem, "tuku-validator", map[string]any{
			"run_id":              r.RunID,
			"signals":             execResult.ValidationSignals,
			"output_artifact_ref": execResult.OutputArtifactRef,
			"passed":              validationPassed(execResult.ValidationSignals),
		}, &r.RunID); err != nil {
			return RunTaskResult{}, err
		}
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": caps.CurrentPhase, "reason": "run completed"}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	if _, err := c.createCheckpoint(caps, r.RunID, checkpoint.TriggerAfterExecution, true, "Run completed with captured evidence; ready for validation follow-up."); err != nil {
		return RunTaskResult{}, err
	}

	recentEvents, err := c.store.Proofs().ListByTask(caps.TaskID, 20)
	if err != nil {
		return RunTaskResult{}, err
	}
	canonicalText, err := c.synthesizer.Synthesize(ctx, caps, recentEvents)
	if err != nil {
		return RunTaskResult{}, err
	}
	if err := c.emitCanonicalConversation(caps, canonicalText, map[string]any{"run_id": r.RunID, "status": r.Status, "exit_code": execResult.ExitCode}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	return RunTaskResult{TaskID: caps.TaskID, RunID: r.RunID, RunStatus: r.Status, Phase: caps.CurrentPhase, CanonicalResponse: canonicalText}, nil
}

func (c *Coordinator) markRunFailed(ctx context.Context, caps capsule.WorkCapsule, r rundomain.ExecutionRun, execResult adapter_contract.ExecutionResult, runErr error) (RunTaskResult, error) {
	now := c.clock()
	applyExecutionEvidence(&r, execResult)
	if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
		r.Status = rundomain.StatusInterrupted
		r.InterruptionReason = runErr.Error()
		r.LastKnownSummary = "Codex run interrupted"
		r.EndedAt = &now
		r.UpdatedAt = now
		if err := c.store.Runs().Update(r); err != nil {
			return RunTaskResult{}, err
		}
		if b, err := c.store.Briefs().Get(r.BriefID); err == nil {
			if err := c.updateBenchmarkOutcome(b, r); err != nil {
				return RunTaskResult{}, err
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return RunTaskResult{}, err
		}
		caps.Version++
		caps.UpdatedAt = now
		caps.CurrentPhase = phase.PhasePaused
		caps.NextAction = "Codex run was interrupted. Check execution evidence and retry."
		if err := c.store.Capsules().Update(caps); err != nil {
			return RunTaskResult{}, err
		}
		taskMemorySnapshot, err := c.refreshTaskMemoryForCurrentState(caps, &r, "run_interrupted")
		if err != nil {
			return RunTaskResult{}, err
		}
		if err := c.appendProof(caps, proof.EventRunInterrupted, proof.ActorSystem, "tuku-runner", map[string]any{"run_id": r.RunID, "reason": runErr.Error()}, &r.RunID); err != nil {
			return RunTaskResult{}, err
		}
		if taskMemorySnapshot != nil {
			if err := c.appendProof(caps, proof.EventTaskMemoryUpdated, proof.ActorSystem, "tuku-task-memory", map[string]any{
				"memory_id":               taskMemorySnapshot.MemoryID,
				"source":                  taskMemorySnapshot.Source,
				"run_id":                  r.RunID,
				"full_history_tokens":     taskMemorySnapshot.FullHistoryTokenEstimate,
				"resume_prompt_tokens":    taskMemorySnapshot.ResumePromptTokenEstimate,
				"memory_compaction_ratio": taskMemorySnapshot.MemoryCompactionRatio,
			}, &r.RunID); err != nil {
				return RunTaskResult{}, err
			}
		}
		if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": caps.CurrentPhase, "reason": "run interrupted"}, &r.RunID); err != nil {
			return RunTaskResult{}, err
		}
		if _, err := c.createCheckpoint(caps, r.RunID, checkpoint.TriggerInterruption, true, "Run interrupted during execution; resumable from paused phase."); err != nil {
			return RunTaskResult{}, err
		}
		recentEvents, err := c.store.Proofs().ListByTask(caps.TaskID, 20)
		if err != nil {
			return RunTaskResult{}, err
		}
		canonicalText, err := c.synthesizer.Synthesize(ctx, caps, recentEvents)
		if err != nil {
			return RunTaskResult{}, err
		}
		if err := c.emitCanonicalConversation(caps, canonicalText, map[string]any{"run_id": r.RunID, "status": r.Status, "reason": runErr.Error()}, &r.RunID); err != nil {
			return RunTaskResult{}, err
		}
		return RunTaskResult{TaskID: caps.TaskID, RunID: r.RunID, RunStatus: r.Status, Phase: caps.CurrentPhase, CanonicalResponse: canonicalText}, nil
	}

	r.Status = rundomain.StatusFailed
	r.LastKnownSummary = fmt.Sprintf("Codex run failed: %s", execResult.Summary)
	r.EndedAt = &now
	r.UpdatedAt = now
	if err := c.store.Runs().Update(r); err != nil {
		return RunTaskResult{}, err
	}

	caps.Version++
	caps.UpdatedAt = now
	caps.CurrentPhase = phase.PhaseBlocked
	caps.NextAction = "Codex run failed. Inspect proof evidence and adjust brief or constraints."
	if err := c.store.Capsules().Update(caps); err != nil {
		return RunTaskResult{}, err
	}
	taskMemorySnapshot, err := c.refreshTaskMemoryForCurrentState(caps, &r, "run_failed")
	if err != nil {
		return RunTaskResult{}, err
	}

	if err := c.appendProof(caps, proof.EventWorkerRunFailed, proof.ActorSystem, "tuku-runner", map[string]any{
		"run_id":                  r.RunID,
		"error":                   runErr.Error(),
		"exit_code":               execResult.ExitCode,
		"summary":                 execResult.Summary,
		"stderr_excerpt":          truncate(execResult.Stderr, 2000),
		"changed_files":           execResult.ChangedFiles,
		"changed_files_semantics": execResult.ChangedFilesSemantics,
		"repo_diff_summary":       repoDiffSummaryFromExecution(execResult),
		"worktree_summary":        worktreeSummaryFromExecution(execResult),
	}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	if taskMemorySnapshot != nil {
		if err := c.appendProof(caps, proof.EventTaskMemoryUpdated, proof.ActorSystem, "tuku-task-memory", map[string]any{
			"memory_id":               taskMemorySnapshot.MemoryID,
			"source":                  taskMemorySnapshot.Source,
			"run_id":                  r.RunID,
			"full_history_tokens":     taskMemorySnapshot.FullHistoryTokenEstimate,
			"resume_prompt_tokens":    taskMemorySnapshot.ResumePromptTokenEstimate,
			"memory_compaction_ratio": taskMemorySnapshot.MemoryCompactionRatio,
		}, &r.RunID); err != nil {
			return RunTaskResult{}, err
		}
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": caps.CurrentPhase, "reason": "run failed"}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	if _, err := c.createCheckpoint(caps, r.RunID, checkpoint.TriggerAfterExecution, false, "Run failed with evidence captured; inspect failure evidence before retrying or regenerating the brief."); err != nil {
		return RunTaskResult{}, err
	}

	recentEvents, err := c.store.Proofs().ListByTask(caps.TaskID, 20)
	if err != nil {
		return RunTaskResult{}, err
	}
	canonicalText, err := c.synthesizer.Synthesize(ctx, caps, recentEvents)
	if err != nil {
		return RunTaskResult{}, err
	}
	if err := c.emitCanonicalConversation(caps, canonicalText, map[string]any{"run_id": r.RunID, "status": r.Status, "error": runErr.Error()}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	return RunTaskResult{TaskID: caps.TaskID, RunID: r.RunID, RunStatus: r.Status, Phase: caps.CurrentPhase, CanonicalResponse: canonicalText}, nil
}

func (c *Coordinator) StatusTask(ctx context.Context, taskID string) (StatusTaskResult, error) {
	caps, err := c.store.Capsules().Get(common.TaskID(taskID))
	if err != nil {
		return StatusTaskResult{}, err
	}

	status := StatusTaskResult{
		TaskID:          caps.TaskID,
		ConversationID:  caps.ConversationID,
		Goal:            caps.Goal,
		Phase:           caps.CurrentPhase,
		Status:          caps.Status,
		CurrentIntentID: caps.CurrentIntentID,
		CurrentBriefID:  caps.CurrentBriefID,
		RepoAnchor: anchorgit.Snapshot{
			RepoRoot:         caps.RepoRoot,
			Branch:           caps.BranchName,
			HeadSHA:          caps.HeadSHA,
			WorkingTreeDirty: caps.WorkingTreeDirty,
			CapturedAt:       caps.AnchorCapturedAt,
		},
	}

	intentState, err := c.store.Intents().LatestByTask(caps.TaskID)
	if err == nil {
		status.CurrentIntentClass = intentState.Class
		status.CurrentIntentSummary = intentState.NormalizedAction
		status.CompiledIntent = compiledIntentSummaryFromState(intentState)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return StatusTaskResult{}, err
	}

	if caps.CurrentBriefID != "" {
		b, err := c.store.Briefs().Get(caps.CurrentBriefID)
		if err == nil {
			status.CurrentBriefHash = b.BriefHash
			status.CurrentContextPackID = b.ContextPackID
			status.CompiledBrief = compiledBriefSummaryFromBrief(b)
			if benchmarkRecord, err := c.loadBenchmarkForTask(caps.TaskID, b.BenchmarkID); err == nil && benchmarkRecord != nil {
				status.CurrentBenchmarkID = benchmarkRecord.BenchmarkID
				status.CurrentBenchmarkSource = benchmarkRecord.Source
				status.CurrentBenchmarkSummary = benchmarkRecord.Summary
				status.CurrentBenchmarkRawPromptTokens = benchmarkRecord.RawPromptTokenEstimate
				status.CurrentBenchmarkDispatchPromptTokens = benchmarkRecord.DispatchPromptTokenEstimate
				status.CurrentBenchmarkStructuredPromptTokens = benchmarkRecord.StructuredPromptTokenEstimate
				status.CurrentBenchmarkSelectedContextTokens = benchmarkRecord.SelectedContextTokenEstimate
				status.CurrentBenchmarkEstimatedTokenSavings = benchmarkRecord.EstimatedTokenSavings
				status.CurrentBenchmarkFilesScanned = benchmarkRecord.FilesScanned
				status.CurrentBenchmarkRankedTargetCount = benchmarkRecord.RankedTargetCount
				status.CurrentBenchmarkCandidateRecallAt3 = benchmarkRecord.CandidateRecallAt3
				status.CurrentBenchmarkDefaultSerializer = benchmarkRecord.DefaultSerializer
				status.CurrentBenchmarkStructuredCheaper = benchmarkRecord.StructuredCheaper
				status.CurrentBenchmarkConfidenceValue = benchmarkRecord.ConfidenceValue
				status.CurrentBenchmarkConfidenceLevel = benchmarkRecord.ConfidenceLevel
			} else if err != nil {
				return StatusTaskResult{}, err
			}
			if b.ContextPackID != "" {
				if pack, err := c.store.ContextPacks().Get(b.ContextPackID); err == nil {
					status.CurrentContextPackMode = pack.Mode
					status.CurrentContextPackFileCount = len(pack.IncludedFiles)
					status.CurrentContextPackHash = pack.PackHash
				} else if !errors.Is(err, sql.ErrNoRows) {
					return StatusTaskResult{}, err
				}
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return StatusTaskResult{}, err
		}
	}
	if snapshot, err := c.store.TaskMemories().LatestByTask(caps.TaskID); err == nil {
		status.CurrentTaskMemoryID = snapshot.MemoryID
		status.CurrentTaskMemorySource = snapshot.Source
		status.CurrentTaskMemorySummary = snapshot.Summary
		status.CurrentTaskMemoryFullHistoryTokens = snapshot.FullHistoryTokenEstimate
		status.CurrentTaskMemoryResumePromptTokens = snapshot.ResumePromptTokenEstimate
		status.CurrentTaskMemoryCompactionRatio = snapshot.MemoryCompactionRatio
	} else if !errors.Is(err, sql.ErrNoRows) {
		return StatusTaskResult{}, err
	}

	var latestRunForIncident *rundomain.ExecutionRun
	if latestRun, err := c.store.Runs().LatestByTask(caps.TaskID); err == nil {
		status.LatestRunID = latestRun.RunID
		status.LatestRunStatus = latestRun.Status
		status.LatestRunSummary = latestRun.LastKnownSummary
		status.LatestRunWorkerRunID = latestRun.WorkerRunID
		status.LatestRunShellSessionID = latestRun.ShellSessionID
		status.LatestRunCommand = latestRun.Command
		status.LatestRunArgs = append([]string{}, latestRun.Args...)
		if latestRun.ExitCode != nil {
			code := *latestRun.ExitCode
			status.LatestRunExitCode = &code
		}
		status.LatestRunChangedFiles = append([]string{}, latestRun.ChangedFiles...)
		status.LatestRunChangedFilesSemantics = latestRun.ChangedFilesSemantics
		status.LatestRunRepoDiffSummary = latestRun.RepoDiffSummary
		status.LatestRunWorktreeSummary = latestRun.WorktreeSummary
		status.LatestRunValidationSignals = append([]string{}, latestRun.ValidationSignals...)
		status.LatestRunOutputArtifactRef = latestRun.OutputArtifactRef
		status.LatestRunStructuredSummary = latestRun.StructuredSummary
		runCopy := latestRun
		latestRunForIncident = &runCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return StatusTaskResult{}, err
	}
	var shellViews []ShellSessionView
	if views, err := c.classifiedShellSessions(caps.TaskID); err != nil {
		return StatusTaskResult{}, err
	} else if len(views) > 0 {
		shellViews = append([]ShellSessionView{}, views...)
		if len(views) > 0 {
			latestSession := views[0]
			for _, session := range views[1:] {
				if session.LastUpdatedAt.After(latestSession.LastUpdatedAt) {
					latestSession = session
				}
			}
			status.LatestShellSessionID = latestSession.SessionID
			status.LatestShellSessionClass = latestSession.SessionClass
			status.LatestShellSessionReason = latestSession.SessionClassReason
			status.LatestShellSessionGuidance = latestSession.ReattachGuidance
			status.LatestShellSessionWorkerSessionID = latestSession.WorkerSessionID
			status.LatestShellSessionWorkerSessionIDSource = latestSession.WorkerSessionIDSource
			status.LatestShellTranscriptState = latestSession.TranscriptState
			status.LatestShellTranscriptRetainedChunks = latestSession.TranscriptRetainedChunks
			status.LatestShellTranscriptDroppedChunks = latestSession.TranscriptDroppedChunks
			status.LatestShellTranscriptRetentionLimit = latestSession.TranscriptRetentionLimit
			status.LatestShellTranscriptOldestSequence = latestSession.TranscriptOldestSequence
			status.LatestShellTranscriptNewestSequence = latestSession.TranscriptNewestSequence
			status.LatestShellTranscriptLastChunkAt = latestSession.TranscriptLastChunkAt
			status.LatestShellTranscriptReviewID = latestSession.TranscriptReviewID
			status.LatestShellTranscriptReviewSource = latestSession.TranscriptReviewSource
			status.LatestShellTranscriptReviewedUpTo = latestSession.TranscriptReviewedUpTo
			status.LatestShellTranscriptReviewSummary = latestSession.TranscriptReviewSummary
			status.LatestShellTranscriptReviewAt = latestSession.TranscriptReviewAt
			status.LatestShellTranscriptReviewStale = latestSession.TranscriptReviewStale
			status.LatestShellTranscriptReviewNewer = latestSession.TranscriptReviewNewer
			status.LatestShellTranscriptReviewClosureState = latestSession.TranscriptReviewClosureState
			status.LatestShellTranscriptReviewOldestUnreviewed = latestSession.TranscriptReviewOldestUnreviewed
			status.LatestShellSessionState = string(latestSession.HostState)
			status.LatestShellSessionUpdatedAt = latestSession.LastUpdatedAt
		}
	} else {
		shellViews = views
	}
	if shellEvents, err := c.listShellSessionEvents(caps.TaskID, "", 1); err != nil {
		return StatusTaskResult{}, err
	} else if len(shellEvents) > 0 {
		status.LatestShellEventID = shellEvents[0].EventID
		status.LatestShellEventKind = string(shellEvents[0].Kind)
		status.LatestShellEventSessionID = shellEvents[0].SessionID
		status.LatestShellEventAt = shellEvents[0].CreatedAt
		status.LatestShellEventNote = shellEvents[0].Note
	}

	checkpointResumable := false
	if latestCheckpoint, err := c.store.Checkpoints().LatestByTask(caps.TaskID); err == nil {
		status.LatestCheckpointID = latestCheckpoint.CheckpointID
		status.LatestCheckpointAt = latestCheckpoint.CreatedAt
		status.LatestCheckpointTrigger = latestCheckpoint.Trigger
		status.ResumeDescriptor = latestCheckpoint.ResumeDescriptor
		checkpointResumable = latestCheckpoint.IsResumable
	} else if !errors.Is(err, sql.ErrNoRows) {
		return StatusTaskResult{}, err
	}

	var latestPacket *handoff.Packet
	var latestLaunch *handoff.Launch
	var latestAck *handoff.Acknowledgment
	var latestFollowThrough *handoff.FollowThrough
	if record, err := c.store.Handoffs().LatestResolutionByTask(caps.TaskID); err == nil {
		status.LatestResolutionID = record.ResolutionID
		status.LatestResolutionKind = record.Kind
		status.LatestResolutionSummary = record.Summary
		status.LatestResolutionAt = record.CreatedAt
	} else if !errors.Is(err, sql.ErrNoRows) {
		return StatusTaskResult{}, err
	}
	if packet, launch, ack, followThrough, err := c.loadActiveClaudeHandoffBranch(caps.TaskID); err != nil {
		return StatusTaskResult{}, err
	} else {
		latestPacket = packet
		latestLaunch = launch
		latestAck = ack
		latestFollowThrough = followThrough
		if latestPacket != nil {
			if latestLaunch != nil {
				status.LatestLaunchAttemptID = latestLaunch.AttemptID
				status.LatestLaunchID = latestLaunch.LaunchID
				status.LatestLaunchStatus = latestLaunch.Status
			}
			control := assessLaunchControl(caps.TaskID, latestPacket, latestLaunch)
			status.LaunchControlState = control.State
			status.LaunchRetryDisposition = control.RetryDisposition
			status.LaunchControlReason = control.Reason
			if latestAck != nil {
				status.LatestAcknowledgmentID = latestAck.AckID
				status.LatestAcknowledgmentStatus = latestAck.Status
				status.LatestAcknowledgmentSummary = latestAck.Summary
			}
			if latestFollowThrough != nil {
				status.LatestFollowThroughID = latestFollowThrough.RecordID
				status.LatestFollowThroughKind = latestFollowThrough.Kind
				status.LatestFollowThroughSummary = latestFollowThrough.Summary
			}
		}
		handoffContinuity := assessHandoffContinuity(caps.TaskID, latestPacket, latestLaunch, latestAck, latestFollowThrough, nil)
		status.HandoffContinuityState = handoffContinuity.State
		status.HandoffContinuityReason = handoffContinuity.Reason
		status.HandoffContinuationProven = handoffContinuity.DownstreamContinuationProven
	}

	if assessment, err := c.assessContinue(ctx, caps.TaskID); err != nil {
		return StatusTaskResult{}, err
	} else {
		recovery, branch, actions, decision, plan, _, runFinalization, localResume := c.operatorTruthForAssessment(assessment)
		reviewProgression := deriveOperatorReviewProgressionFromSessions(shellViews)
		applyReviewProgressionToOperatorDecision(&decision, reviewProgression)
		applyReviewProgressionToOperatorExecutionPlan(&plan, reviewProgression)
		applyRecoveryAssessmentToStatus(&status, recovery, checkpointResumable)
		status.ActiveBranchClass = branch.Class
		status.ActiveBranchRef = branch.BranchRef
		status.ActiveBranchAnchorKind = branch.ActionabilityAnchor
		status.ActiveBranchAnchorRef = branch.ActionabilityAnchorRef
		status.ActiveBranchReason = branch.Reason
		status.LocalRunFinalizationState = runFinalization.State
		status.LocalRunFinalizationRunID = runFinalization.RunID
		status.LocalRunFinalizationStatus = runFinalization.RunStatus
		status.LocalRunFinalizationCheckpointID = runFinalization.CheckpointID
		status.LocalRunFinalizationReason = runFinalization.Reason
		status.LocalResumeAuthorityState = localResume.State
		status.LocalResumeMode = localResume.Mode
		status.LocalResumeCheckpointID = localResume.CheckpointID
		status.LocalResumeRunID = localResume.RunID
		status.LocalResumeReason = localResume.Reason
		status.RequiredNextOperatorAction = actions.RequiredNextAction
		status.ActionAuthority = append([]OperatorActionAuthority{}, actions.Actions...)
		status.OperatorDecision = &decision
		status.OperatorExecutionPlan = &plan
	}
	if latestReceipt, err := c.store.OperatorStepReceipts().LatestByTask(caps.TaskID); err == nil {
		receiptCopy := latestReceipt
		status.LatestOperatorStepReceipt = &receiptCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return StatusTaskResult{}, err
	}
	if recentReceipts, err := c.store.OperatorStepReceipts().ListByTask(caps.TaskID, 3); err == nil {
		status.RecentOperatorStepReceipts = append([]operatorstep.Receipt{}, recentReceipts...)
	} else {
		return StatusTaskResult{}, err
	}
	latestGapAck, recentGapAcks, err := c.reviewGapAcknowledgmentProjection(caps.TaskID, shellViews, 3)
	if err != nil {
		return StatusTaskResult{}, err
	}
	status.LatestTranscriptReviewGapAcknowledgment = latestGapAck
	status.RecentTranscriptReviewGapAcknowledgments = append([]TranscriptReviewGapAcknowledgmentSummary{}, recentGapAcks...)
	latestTransition, recentTransitions, err := c.continuityTransitionReceiptProjection(caps.TaskID, 3)
	if err != nil {
		return StatusTaskResult{}, err
	}
	status.LatestContinuityTransitionReceipt = latestTransition
	status.RecentContinuityTransitionReceipts = append([]ContinuityTransitionReceiptSummary{}, recentTransitions...)
	transitionRisk := deriveContinuityTransitionRiskSummary(status.RecentContinuityTransitionReceipts)
	status.ContinuityTransitionRiskSummary = &transitionRisk
	status.ContinuityIncidentSummary = continuityIncidentSummaryProjection(status.LatestContinuityTransitionReceipt, latestRunForIncident, status.LatestRecoveryAction)
	latestTriage, recentTriages, triageRollup, err := c.continuityIncidentTriageHistoryProjection(caps.TaskID, 3)
	if err != nil {
		return StatusTaskResult{}, err
	}
	latestFollowUp, recentFollowUps, followUpRollup, err := c.continuityIncidentFollowUpHistoryProjection(caps.TaskID, 3)
	if err != nil {
		return StatusTaskResult{}, err
	}
	status.LatestContinuityIncidentTriageReceipt = latestTriage
	status.RecentContinuityIncidentTriageReceipts = append([]ContinuityIncidentTriageReceiptSummary{}, recentTriages...)
	status.ContinuityIncidentTriageHistoryRollup = triageRollup
	status.LatestContinuityIncidentFollowUpReceipt = latestFollowUp
	status.RecentContinuityIncidentFollowUpReceipts = append([]ContinuityIncidentFollowUpReceiptSummary{}, recentFollowUps...)
	status.ContinuityIncidentFollowUpHistoryRollup = followUpRollup
	status.ContinuityIncidentFollowUp = deriveFollowUpAwareAdvisory(
		deriveContinuityIncidentFollowUpSummary(status.LatestContinuityTransitionReceipt, latestTriage, latestFollowUp),
		followUpRollup,
		status.RecentContinuityIncidentFollowUpReceipts,
	)
	status.ContinuityIncidentTaskRisk, err = c.continuityIncidentTaskRiskProjection(ctx, caps.TaskID, defaultContinuityIncidentTaskRiskReadLimit)
	if err != nil {
		return StatusTaskResult{}, err
	}
	if status.OperatorDecision != nil {
		applyContinuityIncidentFollowUpToOperatorDecision(status.OperatorDecision, status.ContinuityIncidentFollowUp)
	}
	if status.OperatorExecutionPlan != nil {
		applyContinuityIncidentFollowUpToOperatorExecutionPlan(status.OperatorExecutionPlan, status.ContinuityIncidentFollowUp)
	}

	events, err := c.store.Proofs().ListByTask(caps.TaskID, 24)
	if err == nil && len(events) > 0 {
		last := events[len(events)-1]
		status.LastEventID = last.EventID
		status.LastEventType = last.Type
		status.LastEventAt = last.Timestamp
		if policySnapshot := latestPolicyDecisionSnapshot(events); policySnapshot != nil {
			status.LatestPolicyDecisionID = policySnapshot.DecisionID
			status.LatestPolicyDecisionStatus = policySnapshot.Status
			status.LatestPolicyDecisionRiskLevel = policySnapshot.RiskLevel
			status.LatestPolicyDecisionReason = policySnapshot.Reason
		}
	} else if err != nil {
		return StatusTaskResult{}, err
	}

	return status, nil
}

func (c *Coordinator) InspectTask(ctx context.Context, taskID string) (InspectTaskResult, error) {
	caps, err := c.store.Capsules().Get(common.TaskID(taskID))
	if err != nil {
		return InspectTaskResult{}, err
	}
	out := InspectTaskResult{
		TaskID: caps.TaskID,
		RepoAnchor: anchorgit.Snapshot{
			RepoRoot:         caps.RepoRoot,
			Branch:           caps.BranchName,
			HeadSHA:          caps.HeadSHA,
			WorkingTreeDirty: caps.WorkingTreeDirty,
			CapturedAt:       caps.AnchorCapturedAt,
		},
	}

	if in, err := c.store.Intents().LatestByTask(caps.TaskID); err == nil {
		inCopy := in
		out.Intent = &inCopy
		out.CompiledIntent = compiledIntentSummaryFromState(inCopy)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return InspectTaskResult{}, err
	}

	if caps.CurrentBriefID != "" {
		b, err := c.store.Briefs().Get(caps.CurrentBriefID)
		if err == nil {
			briefCopy := b
			out.Brief = &briefCopy
			out.CompiledBrief = compiledBriefSummaryFromBrief(briefCopy)
			if benchmarkRecord, err := c.loadBenchmarkForTask(caps.TaskID, b.BenchmarkID); err == nil && benchmarkRecord != nil {
				out.Benchmark = benchmarkRecord
			} else if err != nil {
				return InspectTaskResult{}, err
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return InspectTaskResult{}, err
		}
	}
	if snapshot, err := c.store.TaskMemories().LatestByTask(caps.TaskID); err == nil {
		snapshotCopy := snapshot
		out.TaskMemory = &snapshotCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return InspectTaskResult{}, err
	}

	if latestRun, err := c.store.Runs().LatestByTask(caps.TaskID); err == nil {
		runCopy := latestRun
		out.Run = &runCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return InspectTaskResult{}, err
	}

	if latestCheckpoint, err := c.store.Checkpoints().LatestByTask(caps.TaskID); err == nil {
		cpCopy := latestCheckpoint
		out.Checkpoint = &cpCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return InspectTaskResult{}, err
	}
	if latestHandoff, err := c.store.Handoffs().LatestByTask(caps.TaskID); err == nil {
		packetCopy := latestHandoff
		out.Handoff = &packetCopy
		if latestLaunch, err := c.store.Handoffs().LatestLaunchByHandoff(latestHandoff.HandoffID); err == nil {
			launchCopy := latestLaunch
			out.Launch = &launchCopy
		} else if !errors.Is(err, sql.ErrNoRows) {
			return InspectTaskResult{}, err
		}
		if latestAck, err := c.store.Handoffs().LatestAcknowledgment(latestHandoff.HandoffID); err == nil {
			ackCopy := latestAck
			out.Acknowledgment = &ackCopy
		} else if !errors.Is(err, sql.ErrNoRows) {
			return InspectTaskResult{}, err
		}
		if latestFollowThrough, err := c.store.Handoffs().LatestFollowThrough(latestHandoff.HandoffID); err == nil {
			recordCopy := latestFollowThrough
			out.FollowThrough = &recordCopy
		} else if !errors.Is(err, sql.ErrNoRows) {
			return InspectTaskResult{}, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return InspectTaskResult{}, err
	}
	if latestResolution, err := c.store.Handoffs().LatestResolutionByTask(caps.TaskID); err == nil {
		recordCopy := latestResolution
		out.Resolution = &recordCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return InspectTaskResult{}, err
	}
	if latestHandoff, latestLaunch, latestAck, latestFollowThrough, err := c.loadActiveClaudeHandoffBranch(caps.TaskID); err != nil {
		return InspectTaskResult{}, err
	} else {
		if latestHandoff != nil {
			control := assessLaunchControl(caps.TaskID, out.Handoff, out.Launch)
			if out.Handoff == nil || out.Handoff.HandoffID != latestHandoff.HandoffID {
				control = assessLaunchControl(caps.TaskID, latestHandoff, latestLaunch)
			}
			out.LaunchControl = &control
		}
		continuity := assessHandoffContinuity(caps.TaskID, latestHandoff, latestLaunch, latestAck, latestFollowThrough, nil)
		out.HandoffContinuity = &continuity
	}
	if latestAction, err := c.store.RecoveryActions().LatestByTask(caps.TaskID); err == nil {
		actionCopy := latestAction
		out.LatestRecoveryAction = &actionCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return InspectTaskResult{}, err
	}
	if actions, err := c.store.RecoveryActions().ListByTask(caps.TaskID, 5); err == nil {
		out.RecentRecoveryActions = append([]recoveryaction.Record{}, actions...)
	} else {
		return InspectTaskResult{}, err
	}
	if assessment, err := c.assessContinue(ctx, caps.TaskID); err != nil {
		return InspectTaskResult{}, err
	} else {
		recovery, branch, actions, decision, plan, _, runFinalization, localResume := c.operatorTruthForAssessment(assessment)
		out.Recovery = &recovery
		out.ActiveBranch = &branch
		out.LocalRunFinalization = &runFinalization
		out.LocalResumeAuthority = &localResume
		out.ActionAuthority = &actions
		out.OperatorDecision = &decision
		out.OperatorExecutionPlan = &plan
		if recovery.LatestAction != nil {
			actionCopy := *recovery.LatestAction
			out.LatestRecoveryAction = &actionCopy
		}
	}
	if latestReceipt, err := c.store.OperatorStepReceipts().LatestByTask(caps.TaskID); err == nil {
		receiptCopy := latestReceipt
		out.LatestOperatorStepReceipt = &receiptCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return InspectTaskResult{}, err
	}
	if recentReceipts, err := c.store.OperatorStepReceipts().ListByTask(caps.TaskID, 5); err == nil {
		out.RecentOperatorStepReceipts = append([]operatorstep.Receipt{}, recentReceipts...)
	} else {
		return InspectTaskResult{}, err
	}
	if sessions, err := c.classifiedShellSessions(caps.TaskID); err == nil {
		out.ShellSessions = sessions
	} else {
		return InspectTaskResult{}, err
	}
	reviewProgression := deriveOperatorReviewProgressionFromSessions(out.ShellSessions)
	if out.OperatorDecision != nil {
		applyReviewProgressionToOperatorDecision(out.OperatorDecision, reviewProgression)
	}
	if out.OperatorExecutionPlan != nil {
		applyReviewProgressionToOperatorExecutionPlan(out.OperatorExecutionPlan, reviewProgression)
	}
	latestGapAck, recentGapAcks, err := c.reviewGapAcknowledgmentProjection(caps.TaskID, out.ShellSessions, 5)
	if err != nil {
		return InspectTaskResult{}, err
	}
	out.LatestTranscriptReviewGapAcknowledgment = latestGapAck
	out.RecentTranscriptReviewGapAcknowledgments = append([]TranscriptReviewGapAcknowledgmentSummary{}, recentGapAcks...)
	latestTransition, recentTransitions, err := c.continuityTransitionReceiptProjection(caps.TaskID, 5)
	if err != nil {
		return InspectTaskResult{}, err
	}
	out.LatestContinuityTransitionReceipt = latestTransition
	out.RecentContinuityTransitionReceipts = append([]ContinuityTransitionReceiptSummary{}, recentTransitions...)
	transitionRisk := deriveContinuityTransitionRiskSummary(out.RecentContinuityTransitionReceipts)
	out.ContinuityTransitionRiskSummary = &transitionRisk
	out.ContinuityIncidentSummary = continuityIncidentSummaryProjection(out.LatestContinuityTransitionReceipt, out.Run, out.LatestRecoveryAction)
	latestTriage, recentTriages, triageRollup, err := c.continuityIncidentTriageHistoryProjection(caps.TaskID, 5)
	if err != nil {
		return InspectTaskResult{}, err
	}
	latestFollowUp, recentFollowUps, followUpRollup, err := c.continuityIncidentFollowUpHistoryProjection(caps.TaskID, 5)
	if err != nil {
		return InspectTaskResult{}, err
	}
	out.LatestContinuityIncidentTriageReceipt = latestTriage
	out.RecentContinuityIncidentTriageReceipts = append([]ContinuityIncidentTriageReceiptSummary{}, recentTriages...)
	out.ContinuityIncidentTriageHistoryRollup = triageRollup
	out.LatestContinuityIncidentFollowUpReceipt = latestFollowUp
	out.RecentContinuityIncidentFollowUpReceipts = append([]ContinuityIncidentFollowUpReceiptSummary{}, recentFollowUps...)
	out.ContinuityIncidentFollowUpHistoryRollup = followUpRollup
	out.ContinuityIncidentFollowUp = deriveFollowUpAwareAdvisory(
		deriveContinuityIncidentFollowUpSummary(out.LatestContinuityTransitionReceipt, latestTriage, latestFollowUp),
		followUpRollup,
		out.RecentContinuityIncidentFollowUpReceipts,
	)
	out.ContinuityIncidentTaskRisk, err = c.continuityIncidentTaskRiskProjection(ctx, caps.TaskID, defaultContinuityIncidentTaskRiskReadLimit)
	if err != nil {
		return InspectTaskResult{}, err
	}
	if out.OperatorDecision != nil {
		applyContinuityIncidentFollowUpToOperatorDecision(out.OperatorDecision, out.ContinuityIncidentFollowUp)
	}
	if out.OperatorExecutionPlan != nil {
		applyContinuityIncidentFollowUpToOperatorExecutionPlan(out.OperatorExecutionPlan, out.ContinuityIncidentFollowUp)
	}
	if shellEvents, err := c.listShellSessionEvents(caps.TaskID, "", 20); err == nil {
		out.RecentShellEvents = append([]shellsession.Event{}, shellEvents...)
	} else {
		return InspectTaskResult{}, err
	}
	latestSessionID := ""
	latestUpdated := time.Time{}
	for _, session := range out.ShellSessions {
		if latestSessionID == "" || session.LastUpdatedAt.After(latestUpdated) {
			latestSessionID = session.SessionID
			latestUpdated = session.LastUpdatedAt
		}
	}
	if latestSessionID != "" {
		if transcript, err := c.listShellTranscript(caps.TaskID, latestSessionID, 40); err == nil {
			out.RecentShellTranscript = append([]shellsession.TranscriptChunk{}, transcript...)
		} else {
			return InspectTaskResult{}, err
		}
	}

	return out, nil
}

func (c *Coordinator) reconcileStaleRun(ctx context.Context, caps capsule.WorkCapsule, latestRun rundomain.ExecutionRun, out *ContinueTaskResult) error {
	now := c.clock()
	latestRun.Status = rundomain.StatusInterrupted
	latestRun.InterruptionReason = "stale RUNNING reconciled during continue: no active execution handle"
	latestRun.LastKnownSummary = "Reconciled stale RUNNING run as INTERRUPTED"
	latestRun.EndedAt = &now
	latestRun.UpdatedAt = now
	if err := c.store.Runs().Update(latestRun); err != nil {
		return err
	}

	caps.Version++
	caps.UpdatedAt = now
	caps.CurrentPhase = phase.PhasePaused
	caps.NextAction = "A stale RUNNING run was reconciled to INTERRUPTED. Review evidence and restart execution when ready."
	if err := c.store.Capsules().Update(caps); err != nil {
		return err
	}
	if err := c.appendProof(caps, proof.EventRunInterrupted, proof.ActorSystem, "tuku-daemon", map[string]any{
		"run_id":              latestRun.RunID,
		"reason":              latestRun.InterruptionReason,
		"reconciliation":      true,
		"previous_run_status": rundomain.StatusRunning,
	}, &latestRun.RunID); err != nil {
		return err
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{
		"phase":  caps.CurrentPhase,
		"reason": "stale running run reconciled on continue",
	}, &latestRun.RunID); err != nil {
		return err
	}
	cp, err := c.createCheckpoint(caps, latestRun.RunID, checkpoint.TriggerInterruption, true, "Stale RUNNING run reconciled to INTERRUPTED; resumable from paused state.")
	if err != nil {
		return err
	}

	canonical := fmt.Sprintf(
		"I found run %s still marked RUNNING but no active execution handle was present. I reconciled it as INTERRUPTED and created resumable checkpoint %s. Resume the interrupted execution path from brief %s.",
		latestRun.RunID,
		cp.CheckpointID,
		caps.CurrentBriefID,
	)
	if err := c.emitCanonicalConversation(caps, canonical, map[string]any{
		"outcome":       ContinueOutcomeStaleReconciled,
		"run_id":        latestRun.RunID,
		"checkpoint_id": cp.CheckpointID,
	}, &latestRun.RunID); err != nil {
		return err
	}

	*out = ContinueTaskResult{
		TaskID:            caps.TaskID,
		Outcome:           ContinueOutcomeStaleReconciled,
		DriftClass:        checkpoint.DriftNone,
		Phase:             caps.CurrentPhase,
		RunID:             latestRun.RunID,
		CheckpointID:      cp.CheckpointID,
		ResumeDescriptor:  cp.ResumeDescriptor,
		CanonicalResponse: canonical,
	}
	applyRecoveryAssessmentToContinueResult(out, RecoveryAssessment{
		TaskID:            caps.TaskID,
		ContinuityOutcome: ContinueOutcomeSafe,
		RecoveryClass:     RecoveryClassInterruptedRunRecoverable,
		RecommendedAction: RecoveryActionResumeInterrupted,
		ReadyForNextRun:   false,
		Reason:            fmt.Sprintf("stale run %s was reconciled and is now recoverable from checkpoint %s", latestRun.RunID, cp.CheckpointID),
		CheckpointID:      cp.CheckpointID,
		RunID:             latestRun.RunID,
	})
	return nil
}

func (c *Coordinator) blockedContinueByDrift(_ context.Context, caps capsule.WorkCapsule, drift checkpoint.DriftClass, out *ContinueTaskResult) error {
	now := c.clock()
	caps.Version++
	caps.UpdatedAt = now
	caps.CurrentPhase = phase.PhaseAwaitingDecision
	caps.NextAction = "Direct resume is blocked by major repo drift. Re-anchor or create a new brief before executing."
	if err := c.store.Capsules().Update(caps); err != nil {
		return err
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{
		"phase":  caps.CurrentPhase,
		"reason": "major anchor drift blocked resume",
	}, nil); err != nil {
		return err
	}
	cp, err := c.createCheckpoint(caps, "", checkpoint.TriggerAwaitingDecision, false, "Major repo drift detected. Direct resume blocked pending user decision.")
	if err != nil {
		return err
	}
	canonical := fmt.Sprintf(
		"Direct resume is not safe. I detected major repo drift versus the last continuity anchor, so I blocked automatic resume and recorded checkpoint %s for decision review.",
		cp.CheckpointID,
	)
	if err := c.emitCanonicalConversation(caps, canonical, map[string]any{
		"outcome":       ContinueOutcomeBlockedDrift,
		"drift_class":   drift,
		"checkpoint_id": cp.CheckpointID,
	}, nil); err != nil {
		return err
	}
	*out = ContinueTaskResult{
		TaskID:            caps.TaskID,
		Outcome:           ContinueOutcomeBlockedDrift,
		DriftClass:        drift,
		Phase:             caps.CurrentPhase,
		CheckpointID:      cp.CheckpointID,
		ResumeDescriptor:  cp.ResumeDescriptor,
		CanonicalResponse: canonical,
	}
	applyRecoveryAssessmentToContinueResult(out, RecoveryAssessment{
		TaskID:            caps.TaskID,
		ContinuityOutcome: ContinueOutcomeBlockedDrift,
		RecoveryClass:     RecoveryClassBlockedDrift,
		RecommendedAction: RecoveryActionMakeResumeDecision,
		DriftClass:        drift,
		RequiresDecision:  true,
		Reason:            "repository drift blocks automatic recovery",
		CheckpointID:      cp.CheckpointID,
	})
	return nil
}

func (c *Coordinator) blockedContinueByInconsistency(_ context.Context, caps capsule.WorkCapsule, reason string, out *ContinueTaskResult) error {
	now := c.clock()
	caps.Version++
	caps.UpdatedAt = now
	caps.CurrentPhase = phase.PhaseBlocked
	caps.NextAction = "Continuity state is inconsistent. Re-anchor state or regenerate intent/brief before continuing."
	if err := c.store.Capsules().Update(caps); err != nil {
		return err
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{
		"phase":  caps.CurrentPhase,
		"reason": "continue blocked by inconsistent continuity state",
	}, nil); err != nil {
		return err
	}
	cp, err := c.createCheckpoint(caps, "", checkpoint.TriggerAwaitingDecision, false, fmt.Sprintf("Continue blocked by inconsistent continuity state: %s", reason))
	if err != nil {
		return err
	}
	canonical := fmt.Sprintf(
		"Resume is blocked because continuity state is inconsistent: %s. I recorded checkpoint %s for explicit recovery decisions.",
		reason,
		cp.CheckpointID,
	)
	if err := c.emitCanonicalConversation(caps, canonical, map[string]any{
		"outcome":       ContinueOutcomeBlockedInconsistent,
		"reason":        reason,
		"checkpoint_id": cp.CheckpointID,
	}, nil); err != nil {
		return err
	}
	*out = ContinueTaskResult{
		TaskID:            caps.TaskID,
		Outcome:           ContinueOutcomeBlockedInconsistent,
		DriftClass:        checkpoint.DriftNone,
		Phase:             caps.CurrentPhase,
		CheckpointID:      cp.CheckpointID,
		ResumeDescriptor:  cp.ResumeDescriptor,
		CanonicalResponse: canonical,
	}
	applyRecoveryAssessmentToContinueResult(out, RecoveryAssessment{
		TaskID:            caps.TaskID,
		ContinuityOutcome: ContinueOutcomeBlockedInconsistent,
		RecoveryClass:     RecoveryClassRepairRequired,
		RecommendedAction: RecoveryActionRepairContinuity,
		RequiresRepair:    true,
		Reason:            reason,
		CheckpointID:      cp.CheckpointID,
	})
	return nil
}

func (c *Coordinator) awaitDecisionOnContinue(_ context.Context, caps capsule.WorkCapsule, drift checkpoint.DriftClass, out *ContinueTaskResult) error {
	now := c.clock()
	caps.Version++
	caps.UpdatedAt = now
	caps.CurrentPhase = phase.PhaseAwaitingDecision
	caps.NextAction = "Minor repo drift detected. Confirm whether to continue with the existing brief or regenerate intent/brief."
	if err := c.store.Capsules().Update(caps); err != nil {
		return err
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{
		"phase":  caps.CurrentPhase,
		"reason": "minor anchor drift requires decision",
	}, nil); err != nil {
		return err
	}
	cp, err := c.createCheckpoint(caps, "", checkpoint.TriggerAwaitingDecision, false, "Minor repo drift detected. Awaiting explicit decision before resume.")
	if err != nil {
		return err
	}
	canonical := fmt.Sprintf(
		"I found minor repo drift since the last checkpoint. I paused at decision state and created checkpoint %s. Confirm whether to continue with brief %s or regenerate the brief.",
		cp.CheckpointID,
		caps.CurrentBriefID,
	)
	if err := c.emitCanonicalConversation(caps, canonical, map[string]any{
		"outcome":       ContinueOutcomeNeedsDecision,
		"drift_class":   drift,
		"checkpoint_id": cp.CheckpointID,
	}, nil); err != nil {
		return err
	}
	*out = ContinueTaskResult{
		TaskID:            caps.TaskID,
		Outcome:           ContinueOutcomeNeedsDecision,
		DriftClass:        drift,
		Phase:             caps.CurrentPhase,
		CheckpointID:      cp.CheckpointID,
		ResumeDescriptor:  cp.ResumeDescriptor,
		CanonicalResponse: canonical,
	}
	applyRecoveryAssessmentToContinueResult(out, RecoveryAssessment{
		TaskID:            caps.TaskID,
		ContinuityOutcome: ContinueOutcomeNeedsDecision,
		RecoveryClass:     RecoveryClassDecisionRequired,
		RecommendedAction: RecoveryActionMakeResumeDecision,
		DriftClass:        drift,
		RequiresDecision:  true,
		Reason:            "resume requires an explicit operator decision",
		CheckpointID:      cp.CheckpointID,
	})
	return nil
}

func (c *Coordinator) safeContinue(_ context.Context, caps capsule.WorkCapsule, hasCheckpoint bool, latestCheckpoint checkpoint.Checkpoint, hasRun bool, latestRun rundomain.ExecutionRun, recovery RecoveryAssessment, out *ContinueTaskResult) error {
	now := c.clock()
	if caps.CurrentBriefID == "" {
		canonical := "Resume is blocked because no execution brief exists for this task. Send a task message to compile intent and generate a brief first."
		if err := c.emitCanonicalConversation(caps, canonical, map[string]any{
			"outcome": ContinueOutcomeBlockedInconsistent,
			"reason":  "missing_brief",
		}, nil); err != nil {
			return err
		}
		*out = ContinueTaskResult{
			TaskID:            caps.TaskID,
			Outcome:           ContinueOutcomeBlockedInconsistent,
			DriftClass:        checkpoint.DriftNone,
			Phase:             caps.CurrentPhase,
			CanonicalResponse: canonical,
		}
		return nil
	}
	if _, err := c.store.Briefs().Get(caps.CurrentBriefID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			canonical := "Resume is blocked because capsule state references a missing execution brief. Recompile intent to restore continuity."
			if emitErr := c.emitCanonicalConversation(caps, canonical, map[string]any{
				"outcome": ContinueOutcomeBlockedInconsistent,
				"reason":  "brief_pointer_missing",
			}, nil); emitErr != nil {
				return emitErr
			}
			*out = ContinueTaskResult{
				TaskID:            caps.TaskID,
				Outcome:           ContinueOutcomeBlockedInconsistent,
				DriftClass:        checkpoint.DriftNone,
				Phase:             caps.CurrentPhase,
				CanonicalResponse: canonical,
			}
			return nil
		}
		return err
	}

	runID := common.RunID("")
	if hasRun {
		runID = latestRun.RunID
	}

	caps.Version++
	caps.UpdatedAt = now
	caps.NextAction = "Fresh next bounded run is ready. Start the next bounded run when local execution should proceed."
	if err := c.store.Capsules().Update(caps); err != nil {
		return err
	}

	descriptor := localResumeDescriptorForReadyNextRun("")
	trigger := checkpoint.TriggerContinue
	if recovery.RecoveryClass == RecoveryClassInterruptedRunRecoverable {
		descriptor = localResumeDescriptorForInterrupted("")
	}
	if hasCheckpoint {
		if recovery.RecoveryClass == RecoveryClassInterruptedRunRecoverable {
			descriptor = localResumeDescriptorForInterrupted(latestCheckpoint.CheckpointID)
		} else {
			descriptor = localResumeDescriptorForReadyNextRun(latestCheckpoint.CheckpointID)
		}
	}
	cp, err := c.createCheckpoint(caps, runID, trigger, true, descriptor)
	if err != nil {
		return err
	}
	canonical := fmt.Sprintf(
		"Fresh next bounded run is ready. Checkpoint %s captures the current local recovery boundary for brief %s on branch %s (head %s).",
		cp.CheckpointID,
		caps.CurrentBriefID,
		caps.BranchName,
		caps.HeadSHA,
	)
	if recovery.RecoveryClass == RecoveryClassInterruptedRunRecoverable {
		caps.NextAction = "Interrupted recovery is available. Resume the interrupted execution path from the recoverable checkpoint."
		if err := c.store.Capsules().Update(caps); err != nil {
			return err
		}
		canonical = fmt.Sprintf(
			"Interrupted execution is recoverable. Use checkpoint %s with brief %s on branch %s (head %s) to resume the interrupted execution path.",
			cp.CheckpointID,
			caps.CurrentBriefID,
			caps.BranchName,
			caps.HeadSHA,
		)
	}
	if err := c.emitCanonicalConversation(caps, canonical, map[string]any{
		"outcome":            ContinueOutcomeSafe,
		"checkpoint_id":      cp.CheckpointID,
		"brief_id":           caps.CurrentBriefID,
		"recovery_class":     recovery.RecoveryClass,
		"recommended_action": recovery.RecommendedAction,
	}, runIDPointer(runID)); err != nil {
		return err
	}

	*out = ContinueTaskResult{
		TaskID:            caps.TaskID,
		Outcome:           ContinueOutcomeSafe,
		DriftClass:        checkpoint.DriftNone,
		Phase:             caps.CurrentPhase,
		RunID:             runID,
		CheckpointID:      cp.CheckpointID,
		ResumeDescriptor:  cp.ResumeDescriptor,
		CanonicalResponse: canonical,
	}
	applyRecoveryAssessmentToContinueResult(out, recovery)
	return nil
}

func (c *Coordinator) createCheckpoint(caps capsule.WorkCapsule, runID common.RunID, trigger checkpoint.Trigger, resumable bool, descriptor string) (checkpoint.Checkpoint, error) {
	return c.createCheckpointWithOptions(caps, runID, trigger, resumable, descriptor, true)
}

func (c *Coordinator) createCheckpointWithOptions(caps capsule.WorkCapsule, runID common.RunID, trigger checkpoint.Trigger, resumable bool, descriptor string, emitProof bool) (checkpoint.Checkpoint, error) {
	lastEventID, err := c.latestProofEventID(caps.TaskID)
	if err != nil {
		return checkpoint.Checkpoint{}, err
	}
	if strings.TrimSpace(descriptor) == "" {
		descriptor = "Checkpoint captured for continuity."
	}
	cp := checkpoint.Checkpoint{
		Version:            1,
		CheckpointID:       common.CheckpointID(c.idGenerator("chk")),
		TaskID:             caps.TaskID,
		RunID:              runID,
		CreatedAt:          c.clock(),
		Trigger:            trigger,
		CapsuleVersion:     caps.Version,
		Phase:              caps.CurrentPhase,
		Anchor:             anchorFromCapsule(caps),
		IntentID:           caps.CurrentIntentID,
		BriefID:            caps.CurrentBriefID,
		ContextPackID:      c.contextPackIDForCheckpoint(caps),
		LastEventID:        lastEventID,
		PendingDecisionIDs: []common.DecisionID{},
		ResumeDescriptor:   descriptor,
		IsResumable:        resumable,
	}
	if err := c.store.Checkpoints().Create(cp); err != nil {
		return checkpoint.Checkpoint{}, err
	}
	if emitProof {
		if err := c.appendCheckpointCreatedProof(caps, cp, runIDPointer(runID)); err != nil {
			return checkpoint.Checkpoint{}, err
		}
	}
	return cp, nil
}

func (c *Coordinator) appendCheckpointCreatedProof(caps capsule.WorkCapsule, cp checkpoint.Checkpoint, runID *common.RunID) error {
	checkpointID := cp.CheckpointID
	diffSummary, worktreeSummary := captureRepoEvidence(caps.RepoRoot)
	event := proof.Event{
		EventID:      common.EventID(c.idGenerator("evt")),
		TaskID:       caps.TaskID,
		RunID:        runID,
		CheckpointID: &checkpointID,
		Timestamp:    c.clock(),
		Type:         proof.EventCheckpointCreated,
		ActorType:    proof.ActorSystem,
		ActorID:      "tuku-daemon",
		PayloadJSON: mustJSON(map[string]any{
			"checkpoint_id":     cp.CheckpointID,
			"trigger":           cp.Trigger,
			"resumable":         cp.IsResumable,
			"descriptor":        cp.ResumeDescriptor,
			"context_pack_id":   cp.ContextPackID,
			"repo_diff_summary": diffSummary,
			"worktree_summary":  worktreeSummary,
		}),
		CapsuleVersion: caps.Version,
	}
	return c.store.Proofs().Append(event)
}

func (c *Coordinator) latestProofEventID(taskID common.TaskID) (common.EventID, error) {
	events, err := c.store.Proofs().ListByTask(taskID, 1)
	if err != nil {
		return "", err
	}
	if len(events) == 0 {
		return "", nil
	}
	return events[len(events)-1].EventID, nil
}

func anchorFromCapsule(caps capsule.WorkCapsule) checkpoint.RepoAnchor {
	return checkpoint.RepoAnchor{
		RepoRoot:      caps.RepoRoot,
		WorktreePath:  caps.WorktreePath,
		BranchName:    caps.BranchName,
		HeadSHA:       caps.HeadSHA,
		DirtyHash:     boolString(caps.WorkingTreeDirty),
		UntrackedHash: "",
	}
}

func (c *Coordinator) contextPackIDForCheckpoint(caps capsule.WorkCapsule) common.ContextPackID {
	if caps.CurrentBriefID == "" {
		return ""
	}
	briefRec, err := c.store.Briefs().Get(caps.CurrentBriefID)
	if err != nil {
		return ""
	}
	return briefRec.ContextPackID
}

func classifyAnchorDrift(baseline checkpoint.RepoAnchor, current anchorgit.Snapshot) checkpoint.DriftClass {
	if strings.TrimSpace(current.RepoRoot) == "" {
		return checkpoint.DriftMajor
	}
	if strings.TrimSpace(baseline.RepoRoot) != "" && filepath.Clean(baseline.RepoRoot) != filepath.Clean(current.RepoRoot) {
		return checkpoint.DriftMajor
	}
	if strings.TrimSpace(baseline.WorktreePath) != "" && filepath.Clean(baseline.WorktreePath) != filepath.Clean(current.RepoRoot) {
		return checkpoint.DriftMajor
	}
	if strings.TrimSpace(baseline.BranchName) != "" && strings.TrimSpace(current.Branch) != "" && baseline.BranchName != current.Branch {
		return checkpoint.DriftMajor
	}
	if strings.TrimSpace(baseline.HeadSHA) != "" && strings.TrimSpace(current.HeadSHA) != "" && baseline.HeadSHA != current.HeadSHA {
		return checkpoint.DriftMinor
	}
	if strings.TrimSpace(baseline.DirtyHash) != "" && baseline.DirtyHash != boolString(current.WorkingTreeDirty) {
		return checkpoint.DriftMinor
	}
	return checkpoint.DriftNone
}

func runIDPointer(runID common.RunID) *common.RunID {
	if runID == "" {
		return nil
	}
	id := runID
	return &id
}

type policyDecisionSnapshot struct {
	DecisionID common.DecisionID
	Status     string
	RiskLevel  string
	Reason     string
}

func latestPolicyDecisionSnapshot(events []proof.Event) *policyDecisionSnapshot {
	out := &policyDecisionSnapshot{}
	set := false
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		switch event.Type {
		case proof.EventPolicyDecisionResolved:
			var payload struct {
				DecisionID common.DecisionID `json:"decision_id"`
				Status     string            `json:"status"`
				Reason     string            `json:"reason"`
			}
			if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err == nil {
				out.DecisionID = payload.DecisionID
				out.Status = payload.Status
				out.Reason = payload.Reason
				set = true
			}
		case proof.EventPolicyDecisionRequested:
			var payload struct {
				DecisionID common.DecisionID `json:"decision_id"`
				RiskLevel  string            `json:"risk_level"`
			}
			if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err == nil {
				if out.DecisionID == "" {
					out.DecisionID = payload.DecisionID
				}
				if out.RiskLevel == "" {
					out.RiskLevel = payload.RiskLevel
				}
				set = true
			}
		}
		if set && out.DecisionID != "" && out.Status != "" && out.RiskLevel != "" {
			break
		}
	}
	if !set {
		return nil
	}
	return out
}

func (c *Coordinator) withTx(fn func(txc *Coordinator) error) error {
	return c.store.WithTx(func(txStore storage.Store) error {
		txc := *c
		txc.store = txStore
		return fn(&txc)
	})
}

func (c *Coordinator) appendProof(caps capsule.WorkCapsule, eventType proof.EventType, actorType proof.ActorType, actorID string, payload map[string]any, runID *common.RunID) error {
	e := proof.Event{
		EventID:        common.EventID(c.idGenerator("evt")),
		TaskID:         caps.TaskID,
		RunID:          runID,
		Timestamp:      c.clock(),
		Type:           eventType,
		ActorType:      actorType,
		ActorID:        actorID,
		PayloadJSON:    mustJSON(payload),
		CapsuleVersion: caps.Version,
	}
	return c.store.Proofs().Append(e)
}

func (c *Coordinator) emitCanonicalConversation(caps capsule.WorkCapsule, canonicalText string, payload map[string]any, runID *common.RunID) error {
	systemMsg := conversation.Message{
		MessageID:      common.MessageID(c.idGenerator("msg")),
		ConversationID: caps.ConversationID,
		TaskID:         caps.TaskID,
		Role:           conversation.RoleSystem,
		Body:           canonicalText,
		CreatedAt:      c.clock(),
	}
	if err := c.store.Conversations().Append(systemMsg); err != nil {
		return err
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["message_id"] = systemMsg.MessageID
	return c.appendProof(caps, proof.EventCanonicalResponseEmitted, proof.ActorSystem, "tuku-daemon", payload, runID)
}

func agentsMetadata(repoRoot string) (checksum string, instructions string) {
	path := filepath.Join(repoRoot, "AGENTS.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	sum := sha256.Sum256(data)
	checksum = hexString(sum[:])
	lines := strings.Split(string(data), "\n")
	selected := make([]string, 0, 6)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		selected = append(selected, line)
		if len(selected) >= 6 {
			break
		}
	}
	return checksum, strings.Join(selected, " | ")
}

func boolString(v bool) string {
	if v {
		return "dirty"
	}
	return "clean"
}

func truncate(value string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max]) + "...(truncated)"
}

func hexString(bytes []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(bytes)*2)
	for i, b := range bytes {
		out[i*2] = hexdigits[b>>4]
		out[i*2+1] = hexdigits[b&0x0f]
	}
	return string(out)
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func newID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UTC().UnixNano())
}
