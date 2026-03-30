package shell

import (
	"context"
	"time"
)

type Snapshot struct {
	TaskID                                   string
	Goal                                     string
	Phase                                    string
	Status                                   string
	Repo                                     RepoAnchor
	LocalScratch                             *LocalScratchContext
	IntentClass                              string
	IntentSummary                            string
	CompiledIntent                           *CompiledIntentSummary
	Brief                                    *BriefSummary
	Run                                      *RunSummary
	Checkpoint                               *CheckpointSummary
	Handoff                                  *HandoffSummary
	Launch                                   *LaunchSummary
	LaunchControl                            *LaunchControlSummary
	Acknowledgment                           *AcknowledgmentSummary
	FollowThrough                            *FollowThroughSummary
	Resolution                               *ResolutionSummary
	ActiveBranch                             *ActiveBranchSummary
	LocalRunFinalization                     *LocalRunFinalizationSummary
	LocalResume                              *LocalResumeAuthoritySummary
	ActionAuthority                          *OperatorActionAuthoritySet
	OperatorDecision                         *OperatorDecisionSummary
	OperatorExecutionPlan                    *OperatorExecutionPlan
	LatestOperatorStepReceipt                *OperatorStepReceiptSummary
	RecentOperatorStepReceipts               []OperatorStepReceiptSummary
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
	LatestTranscriptReviewGapAcknowledgment  *TranscriptReviewGapAcknowledgment
	RecentTranscriptReviewGapAcknowledgments []TranscriptReviewGapAcknowledgment
	HandoffContinuity                        *HandoffContinuitySummary
	Recovery                                 *RecoverySummary
	ShellSessions                            []KnownShellSession
	RecentShellEvents                        []ShellSessionEventSummary
	RecentShellTranscript                    []ShellTranscriptChunkSummary
	RecentProofs                             []ProofItem
	RecentConversation                       []ConversationItem
	LatestCanonicalResponse                  string
}

type RepoAnchor struct {
	RepoRoot         string
	Branch           string
	HeadSHA          string
	WorkingTreeDirty bool
	CapturedAt       time.Time
}

type LocalScratchContext struct {
	RepoRoot string
	Notes    []ConversationItem
}

type CompiledIntentSummary struct {
	IntentID                string
	Class                   string
	Posture                 string
	ExecutionReadiness      string
	Objective               string
	RequestedOutcome        string
	NormalizedAction        string
	ScopeSummary            string
	ExplicitConstraints     []string
	DoneCriteria            []string
	AmbiguityFlags          []string
	ClarificationQuestions  []string
	RequiresClarification   bool
	BoundedEvidenceMessages int
	ReadinessReason         string
	CompilationNotes        string
	Digest                  string
	Advisory                string
	CreatedAt               time.Time
}

type BriefSummary struct {
	ID                      string
	Posture                 string
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

type RunSummary struct {
	ID                 string
	WorkerKind         string
	Status             string
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

type CheckpointSummary struct {
	ID               string
	Trigger          string
	CreatedAt        time.Time
	ResumeDescriptor string
	IsResumable      bool
}

type HandoffSummary struct {
	ID           string
	Status       string
	SourceWorker string
	TargetWorker string
	Mode         string
	Reason       string
	AcceptedBy   string
	CreatedAt    time.Time
}

type LaunchSummary struct {
	AttemptID         string
	LaunchID          string
	Status            string
	RequestedAt       time.Time
	StartedAt         time.Time
	EndedAt           time.Time
	Summary           string
	ErrorMessage      string
	OutputArtifactRef string
}

type LaunchControlSummary struct {
	State            string
	RetryDisposition string
	Reason           string
	HandoffID        string
	AttemptID        string
	LaunchID         string
	TargetWorker     string
	RequestedAt      time.Time
	CompletedAt      time.Time
	FailedAt         time.Time
}

type AcknowledgmentSummary struct {
	Status    string
	Summary   string
	CreatedAt time.Time
}

type FollowThroughSummary struct {
	RecordID        string
	Kind            string
	Summary         string
	LaunchAttemptID string
	LaunchID        string
	CreatedAt       time.Time
}

type ResolutionSummary struct {
	ResolutionID    string
	Kind            string
	Summary         string
	LaunchAttemptID string
	LaunchID        string
	CreatedAt       time.Time
}

type ActiveBranchSummary struct {
	Class                  string
	BranchRef              string
	ActionabilityAnchor    string
	ActionabilityAnchorRef string
	Reason                 string
}

type LocalRunFinalizationSummary struct {
	State        string
	RunID        string
	RunStatus    string
	CheckpointID string
	Reason       string
}

type LocalResumeAuthoritySummary struct {
	State               string
	Mode                string
	CheckpointID        string
	RunID               string
	BlockingBranchClass string
	BlockingBranchRef   string
	Reason              string
}

type OperatorActionAuthority struct {
	Action              string
	State               string
	Reason              string
	BlockingBranchClass string
	BlockingBranchRef   string
	AnchorKind          string
	AnchorRef           string
}

type OperatorActionAuthoritySet struct {
	RequiredNextAction string
	Actions            []OperatorActionAuthority
}

type OperatorDecisionBlockedAction struct {
	Action string
	Reason string
}

type OperatorDecisionSummary struct {
	ActiveOwnerClass   string
	ActiveOwnerRef     string
	Headline           string
	RequiredNextAction string
	PrimaryReason      string
	Guidance           string
	IntegrityNote      string
	BlockedActions     []OperatorDecisionBlockedAction
}

type OperatorExecutionStep struct {
	Action         string
	Status         string
	Domain         string
	CommandSurface string
	CommandHint    string
	Reason         string
}

type OperatorExecutionPlan struct {
	PrimaryStep             *OperatorExecutionStep
	MandatoryBeforeProgress bool
	SecondarySteps          []OperatorExecutionStep
	BlockedSteps            []OperatorExecutionStep
}

type OperatorStepReceiptSummary struct {
	ReceiptID                        string
	TaskID                           string
	ActionHandle                     string
	ExecutionDomain                  string
	CommandSurfaceKind               string
	ExecutionAttempted               bool
	ResultClass                      string
	Summary                          string
	Reason                           string
	RunID                            string
	CheckpointID                     string
	BriefID                          string
	HandoffID                        string
	LaunchAttemptID                  string
	LaunchID                         string
	ReviewGapState                   string
	ReviewGapSessionID               string
	ReviewGapClass                   string
	ReviewGapPresent                 bool
	ReviewGapReviewedUpTo            int64
	ReviewGapOldestUnreviewed        int64
	ReviewGapNewestRetained          int64
	ReviewGapUnreviewedRetainedCount int
	ReviewGapAcknowledged            bool
	ReviewGapAcknowledgmentID        string
	ReviewGapAcknowledgmentClass     string
	TransitionReceiptID              string
	TransitionKind                   string
	CreatedAt                        time.Time
	CompletedAt                      time.Time
}

type ContinuityTransitionReceiptSummary struct {
	ReceiptID                string
	TaskID                   string
	ShellSessionID           string
	TransitionKind           string
	TransitionHandle         string
	TriggerAction            string
	TriggerSource            string
	HandoffID                string
	LaunchAttemptID          string
	LaunchID                 string
	ResolutionID             string
	BranchClassBefore        string
	BranchRefBefore          string
	BranchClassAfter         string
	BranchRefAfter           string
	HandoffStateBefore       string
	HandoffStateAfter        string
	LaunchControlBefore      string
	LaunchControlAfter       string
	ReviewGapPresent         bool
	ReviewPosture            string
	ReviewState              string
	ReviewScope              string
	ReviewedUpToSequence     int64
	OldestUnreviewedSequence int64
	NewestRetainedSequence   int64
	UnreviewedRetainedCount  int
	LatestReviewID           string
	LatestReviewGapAckID     string
	AcknowledgmentPresent    bool
	AcknowledgmentClass      string
	Summary                  string
	CreatedAt                time.Time
}

type ContinuityTransitionRiskSummary struct {
	WindowSize                           int
	ReviewGapTransitions                 int
	AcknowledgedReviewGapTransitions     int
	UnacknowledgedReviewGapTransitions   int
	StaleReviewPostureTransitions        int
	SourceScopedReviewPostureTransitions int
	IntoClaudeOwnershipTransitions       int
	BackToLocalOwnershipTransitions      int
	OperationallyNotable                 bool
	Summary                              string
}

type ContinuityIncidentRiskSummary struct {
	ReviewGapPresent                bool
	AcknowledgmentPresent           bool
	StaleOrUnreviewedReviewPosture  bool
	SourceScopedReviewPosture       bool
	IntoClaudeOwnershipTransition   bool
	BackToLocalOwnershipTransition  bool
	UnresolvedContinuityAmbiguity   bool
	NearbyFailedOrInterruptedRuns   int
	NearbyRecoveryActions           int
	RecentFailureOrRecoveryActivity bool
	OperationallyNotable            bool
	Summary                         string
}

type ContinuityIncidentTriageReceiptSummary struct {
	ReceiptID                 string
	TaskID                    string
	AnchorMode                string
	AnchorTransitionReceiptID string
	AnchorTransitionKind      string
	AnchorHandoffID           string
	AnchorShellSessionID      string
	Posture                   string
	FollowUpPosture           string
	Summary                   string
	ReviewGapPresent          bool
	ReviewPosture             string
	ReviewState               string
	ReviewScope               string
	ReviewedUpToSequence      int64
	OldestUnreviewedSequence  int64
	NewestRetainedSequence    int64
	UnreviewedRetainedCount   int
	LatestReviewID            string
	LatestReviewGapAckID      string
	AcknowledgmentPresent     bool
	AcknowledgmentClass       string
	RiskSummary               ContinuityIncidentRiskSummary
	CreatedAt                 time.Time
}

type ContinuityIncidentFollowUpSummary struct {
	State                     string
	Digest                    string
	WindowAdvisory            string
	Advisory                  string
	ClosureIntelligence       *ContinuityIncidentClosureSummary
	FollowUpAdvised           bool
	NeedsFollowUp             bool
	Deferred                  bool
	TriageBehindLatest        bool
	TriagedUnderReviewRisk    bool
	LatestTransitionReceiptID string
	LatestTriageReceiptID     string
	TriageAnchorReceiptID     string
	TriagePosture             string
	LatestFollowUpReceiptID   string
	LatestFollowUpActionKind  string
	LatestFollowUpSummary     string
	LatestFollowUpAt          time.Time
	FollowUpReceiptPresent    bool
	FollowUpOpen              bool
	FollowUpClosed            bool
	FollowUpReopened          bool
	FollowUpProgressed        bool
}

type ContinuityIncidentClosureSummary struct {
	Class                             string
	Digest                            string
	WindowAdvisory                    string
	Detail                            string
	BoundedWindow                     bool
	WindowSize                        int
	DistinctAnchors                   int
	OperationallyUnresolved           bool
	ClosureAppearsWeak                bool
	ReopenedAfterClosure              bool
	RepeatedReopenLoop                bool
	StagnantProgression               bool
	TriagedWithoutFollowUp            bool
	AnchorsWithOpenFollowUp           int
	AnchorsClosed                     int
	AnchorsReopened                   int
	AnchorsBehindLatestTransition     int
	AnchorsRepeatedWithoutProgression int
	AnchorsTriagedWithoutFollowUp     int
	ReopenedAfterClosureAnchors       int
	RepeatedReopenLoopAnchors         int
	StagnantProgressionAnchors        int
	RecentAnchors                     []ContinuityIncidentClosureAnchorItem
}

type ContinuityIncidentTaskRiskSummary struct {
	Class                               string
	Digest                              string
	WindowAdvisory                      string
	Detail                              string
	BoundedWindow                       bool
	WindowSize                          int
	DistinctAnchors                     int
	RecurringWeakClosure                bool
	RecurringUnresolved                 bool
	RecurringStagnantFollowUp           bool
	RecurringTriagedWithoutFollowUp     bool
	ReopenedAfterClosureAnchors         int
	RepeatedReopenLoopAnchors           int
	StagnantProgressionAnchors          int
	AnchorsTriagedWithoutFollowUp       int
	AnchorsWithOpenFollowUp             int
	AnchorsReopened                     int
	OperationallyUnresolvedAnchorSignal int
	RecentAnchorClasses                 []string
}

type ContinuityIncidentClosureAnchorItem struct {
	AnchorTransitionReceiptID string
	Class                     string
	Digest                    string
	Explanation               string
	LatestFollowUpReceiptID   string
	LatestFollowUpActionKind  string
	LatestFollowUpAt          time.Time
}

type ContinuityIncidentTriageHistoryRollupSummary struct {
	WindowSize                        int
	BoundedWindow                     bool
	DistinctAnchors                   int
	AnchorsTriagedCurrent             int
	AnchorsNeedsFollowUp              int
	AnchorsDeferred                   int
	AnchorsBehindLatestTransition     int
	AnchorsWithOpenFollowUp           int
	AnchorsRepeatedWithoutProgression int
	ReviewRiskReceipts                int
	AcknowledgedReviewGapReceipts     int
	OperationallyNotable              bool
	Summary                           string
}

type ContinuityIncidentFollowUpReceiptSummary struct {
	ReceiptID                 string
	TaskID                    string
	AnchorMode                string
	AnchorTransitionReceiptID string
	AnchorTransitionKind      string
	AnchorHandoffID           string
	AnchorShellSessionID      string
	TriageReceiptID           string
	TriagePosture             string
	TriageFollowUpPosture     string
	ActionKind                string
	Summary                   string
	ReviewGapPresent          bool
	ReviewPosture             string
	ReviewState               string
	ReviewScope               string
	ReviewedUpToSequence      int64
	OldestUnreviewedSequence  int64
	NewestRetainedSequence    int64
	UnreviewedRetainedCount   int
	LatestReviewID            string
	LatestReviewGapAckID      string
	AcknowledgmentPresent     bool
	AcknowledgmentClass       string
	TriagedUnderReviewRisk    bool
	CreatedAt                 time.Time
}

type ContinuityIncidentFollowUpHistoryRollupSummary struct {
	WindowSize                        int
	BoundedWindow                     bool
	DistinctAnchors                   int
	ReceiptsRecordedPending           int
	ReceiptsProgressed                int
	ReceiptsClosed                    int
	ReceiptsReopened                  int
	AnchorsWithOpenFollowUp           int
	AnchorsClosed                     int
	AnchorsReopened                   int
	OpenAnchorsBehindLatestTransition int
	AnchorsRepeatedWithoutProgression int
	AnchorsTriagedWithoutFollowUp     int
	OperationallyNotable              bool
	Summary                           string
}

type TranscriptReviewGapAcknowledgment struct {
	AcknowledgmentID         string
	TaskID                   string
	SessionID                string
	Class                    string
	ReviewState              string
	ReviewScope              string
	ReviewedUpToSequence     int64
	OldestUnreviewedSequence int64
	NewestRetainedSequence   int64
	UnreviewedRetainedCount  int
	TranscriptState          string
	RetentionLimit           int
	RetainedChunks           int
	DroppedChunks            int
	ActionContext            string
	Summary                  string
	CreatedAt                time.Time
	StaleBehindCurrent       bool
	NewerRetainedCount       int
}

type HandoffContinuitySummary struct {
	State                        string
	Reason                       string
	LaunchAttemptID              string
	LaunchID                     string
	AcknowledgmentID             string
	AcknowledgmentStatus         string
	AcknowledgmentSummary        string
	FollowThroughID              string
	FollowThroughKind            string
	FollowThroughSummary         string
	ResolutionID                 string
	ResolutionKind               string
	ResolutionSummary            string
	DownstreamContinuationProven bool
}

type RecoveryIssue struct {
	Code    string
	Message string
}

type RecoverySummary struct {
	ContinuityOutcome      string
	Class                  string
	Action                 string
	ReadyForNextRun        bool
	ReadyForHandoffLaunch  bool
	RequiresDecision       bool
	RequiresRepair         bool
	RequiresReview         bool
	RequiresReconciliation bool
	DriftClass             string
	Reason                 string
	CheckpointID           string
	RunID                  string
	HandoffID              string
	HandoffStatus          string
	Issues                 []RecoveryIssue
}

type ProofItem struct {
	ID        string
	Type      string
	Summary   string
	Timestamp time.Time
}

type ConversationItem struct {
	Role      string
	Body      string
	CreatedAt time.Time
}

type HostMode string

const (
	HostModeCodexPTY   HostMode = "codex-pty"
	HostModeClaudePTY  HostMode = "claude-pty"
	HostModeTranscript HostMode = "transcript"
)

type HostState string

const (
	HostStateStarting       HostState = "starting"
	HostStateLive           HostState = "live"
	HostStateExited         HostState = "exited"
	HostStateFailed         HostState = "failed"
	HostStateFallback       HostState = "fallback"
	HostStateTranscriptOnly HostState = "transcript-only"
)

type HostStatus struct {
	Mode                  HostMode
	State                 HostState
	Label                 string
	Note                  string
	WorkerSessionID       string
	WorkerSessionIDSource WorkerSessionIDSource
	InputLive             bool
	ExitCode              *int
	Width                 int
	Height                int
	LastOutputAt          time.Time
	StateChangedAt        time.Time
}

type SessionEventType string

const (
	SessionEventShellStarted                  SessionEventType = "shell_started"
	SessionEventHostStartupAttempted          SessionEventType = "host_startup_attempted"
	SessionEventHostLive                      SessionEventType = "host_live"
	SessionEventResizeApplied                 SessionEventType = "resize_applied"
	SessionEventHostExited                    SessionEventType = "host_exited"
	SessionEventHostFailed                    SessionEventType = "host_failed"
	SessionEventFallbackActivated             SessionEventType = "fallback_activated"
	SessionEventManualRefresh                 SessionEventType = "manual_refresh"
	SessionEventPendingMessageStaged          SessionEventType = "pending_message_staged"
	SessionEventPendingMessageEditStarted     SessionEventType = "pending_message_edit_started"
	SessionEventPendingMessageEditSaved       SessionEventType = "pending_message_edit_saved"
	SessionEventPendingMessageEditCanceled    SessionEventType = "pending_message_edit_canceled"
	SessionEventPendingMessageSent            SessionEventType = "pending_message_sent"
	SessionEventPendingMessageCleared         SessionEventType = "pending_message_cleared"
	SessionEventPrimaryOperatorActionStarted  SessionEventType = "primary_operator_action_started"
	SessionEventPrimaryOperatorActionExecuted SessionEventType = "primary_operator_action_executed"
	SessionEventPrimaryOperatorActionFailed   SessionEventType = "primary_operator_action_failed"
	SessionEventPriorPersistedProof           SessionEventType = "prior_persisted_proof"
)

type SessionEvent struct {
	Type      SessionEventType
	Summary   string
	CreatedAt time.Time
}

type SessionState struct {
	SessionID             string
	StartedAt             time.Time
	WorkerPreference      WorkerPreference
	ResolvedWorker        WorkerPreference
	WorkerSessionID       string
	WorkerSessionIDSource WorkerSessionIDSource
	AttachCapability      WorkerAttachCapability
	Journal               []SessionEvent
	KnownSessions         []KnownShellSession
	PriorPersistedSummary string
}

type WorkerAttachCapability string

const (
	WorkerAttachCapabilityNone       WorkerAttachCapability = "none"
	WorkerAttachCapabilityAttachable WorkerAttachCapability = "attachable"
)

type WorkerSessionIDSource string

const (
	WorkerSessionIDSourceNone          WorkerSessionIDSource = "none"
	WorkerSessionIDSourceAuthoritative WorkerSessionIDSource = "authoritative"
	WorkerSessionIDSourceHeuristic     WorkerSessionIDSource = "heuristic"
	WorkerSessionIDSourceUnknown       WorkerSessionIDSource = "unknown"
)

type KnownShellSessionClass string

const (
	KnownShellSessionClassAttachable         KnownShellSessionClass = "attachable"
	KnownShellSessionClassActiveUnattachable KnownShellSessionClass = "active_unattachable"
	KnownShellSessionClassStale              KnownShellSessionClass = "stale"
	KnownShellSessionClassEnded              KnownShellSessionClass = "ended"
)

type KnownShellSession struct {
	SessionID                        string
	TaskID                           string
	WorkerPreference                 WorkerPreference
	ResolvedWorker                   WorkerPreference
	WorkerSessionID                  string
	WorkerSessionIDSource            WorkerSessionIDSource
	AttachCapability                 WorkerAttachCapability
	HostMode                         HostMode
	HostState                        HostState
	SessionClass                     KnownShellSessionClass
	SessionClassReason               string
	ReattachGuidance                 string
	OperatorSummary                  string
	TranscriptState                  string
	TranscriptRetainedChunks         int
	TranscriptDroppedChunks          int
	TranscriptRetentionLimit         int
	TranscriptOldestSequence         int64
	TranscriptNewestSequence         int64
	TranscriptLastChunkAt            time.Time
	TranscriptReviewID               string
	TranscriptReviewSource           string
	TranscriptReviewedUpTo           int64
	TranscriptReviewSummary          string
	TranscriptReviewAt               time.Time
	TranscriptReviewStale            bool
	TranscriptReviewNewer            int
	TranscriptReviewClosureState     string
	TranscriptReviewOldestUnreviewed int64
	TranscriptRecentReviews          []TranscriptReviewMarker
	StartedAt                        time.Time
	LastUpdatedAt                    time.Time
	Active                           bool
	Note                             string
	LatestEventID                    string
	LatestEventKind                  string
	LatestEventAt                    time.Time
	LatestEventNote                  string
}

type TranscriptReviewMarker struct {
	ReviewID                 string
	SourceFilter             string
	ReviewedUpToSequence     int64
	Summary                  string
	CreatedAt                time.Time
	TranscriptState          string
	RetentionLimit           int
	RetainedChunks           int
	DroppedChunks            int
	OldestRetainedSequence   int64
	NewestRetainedSequence   int64
	StaleBehindLatest        bool
	NewerRetainedCount       int
	OldestUnreviewedSequence int64
	ClosureState             string
}

type ShellSessionEventSummary struct {
	EventID               string
	TaskID                string
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
	ChunkID    string
	TaskID     string
	SessionID  string
	SequenceNo int64
	Source     string
	Content    string
	CreatedAt  time.Time
}

type FocusPane int

const (
	FocusWorker FocusPane = iota
	FocusInspector
	FocusActivity
)

type UIState struct {
	ShowInspector                  bool
	ShowProof                      bool
	ShowHelp                       bool
	ShowStatus                     bool
	Focus                          FocusPane
	EscapePrefix                   bool
	PendingTaskMessage             string
	PendingTaskMessageSource       string
	PendingTaskMessageEditMode     bool
	PendingTaskMessageEditBuffer   string
	PendingTaskMessageEditOriginal string
	Session                        SessionState
	LastRefresh                    time.Time
	ObservedAt                     time.Time
	LastError                      string
	PrimaryActionInFlight          *PrimaryActionInFlightSummary
	LastPrimaryActionResult        *PrimaryActionResultSummary
}

type PrimaryActionInFlightSummary struct {
	Action    string
	StartedAt time.Time
}

type PrimaryActionResultSummary struct {
	Action      string
	Outcome     string
	Summary     string
	Deltas      []string
	NextStep    string
	ErrorText   string
	ReceiptID   string
	ResultClass string
	CreatedAt   time.Time
}

type ViewModel struct {
	Header     HeaderView
	WorkerPane PaneView
	Inspector  *InspectorView
	ProofStrip *StripView
	InputDock  InputDockView
	Footer     string
	Overlay    *OverlayView
	Layout     shellLayout
}

type HeaderView struct {
	Title       string
	TaskLabel   string
	Phase       string
	Worker      string
	Repo        string
	Continuity  string
	WorkerState string
	RepoState   string
	NextAction  string
	SessionID   string
}

type PaneView struct {
	Title   string
	Lines   []string
	Focused bool
}

type InspectorView struct {
	Title    string
	Sections []SectionView
	Focused  bool
}

type SectionView struct {
	Title string
	Lines []string
}

type StripView struct {
	Title   string
	Lines   []string
	Focused bool
}

type OverlayView struct {
	Title string
	Lines []string
}

type InputDockView struct {
	Title       string
	Status      string
	PromptLabel string
	Preview     []string
	Placeholder string
	Hint        string
	Focused     bool
	ReadOnly    bool
}

type SnapshotSource interface {
	Load(taskID string) (Snapshot, error)
}

type WorkerHost interface {
	Start(ctx context.Context, snapshot Snapshot) error
	Stop() error
	UpdateSnapshot(snapshot Snapshot)
	Resize(width int, height int) bool
	CanAcceptInput() bool
	WriteInput(data []byte) bool
	Status() HostStatus
	Title() string
	WorkerLabel() string
	Lines(height int, width int) []string
	ActivityLines(limit int) []string
}

func (s Snapshot) RunWorkerKind() string {
	if s.Run == nil {
		return ""
	}
	return s.Run.WorkerKind
}

func (s Snapshot) HandoffTargetWorker() string {
	if s.Handoff == nil {
		return ""
	}
	return s.Handoff.TargetWorker
}

func (s Snapshot) HasLocalScratchAdoption() bool {
	return s.LocalScratch != nil && len(s.LocalScratch.Notes) > 0
}
