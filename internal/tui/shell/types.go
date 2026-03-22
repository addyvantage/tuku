package shell

import (
	"context"
	"time"
)

type Snapshot struct {
	TaskID                     string
	Goal                       string
	Phase                      string
	Status                     string
	Repo                       RepoAnchor
	LocalScratch               *LocalScratchContext
	IntentClass                string
	IntentSummary              string
	Brief                      *BriefSummary
	Run                        *RunSummary
	Checkpoint                 *CheckpointSummary
	Handoff                    *HandoffSummary
	Launch                     *LaunchSummary
	LaunchControl              *LaunchControlSummary
	Acknowledgment             *AcknowledgmentSummary
	FollowThrough              *FollowThroughSummary
	Resolution                 *ResolutionSummary
	ActiveBranch               *ActiveBranchSummary
	LocalRunFinalization       *LocalRunFinalizationSummary
	LocalResume                *LocalResumeAuthoritySummary
	ActionAuthority            *OperatorActionAuthoritySet
	OperatorDecision           *OperatorDecisionSummary
	OperatorExecutionPlan      *OperatorExecutionPlan
	LatestOperatorStepReceipt  *OperatorStepReceiptSummary
	RecentOperatorStepReceipts []OperatorStepReceiptSummary
	HandoffContinuity          *HandoffContinuitySummary
	Recovery                   *RecoverySummary
	RecentProofs               []ProofItem
	RecentConversation         []ConversationItem
	LatestCanonicalResponse    string
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

type BriefSummary struct {
	ID               string
	Objective        string
	NormalizedAction string
	Constraints      []string
	DoneCriteria     []string
}

type RunSummary struct {
	ID                 string
	WorkerKind         string
	Status             string
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
	ReceiptID          string
	TaskID             string
	ActionHandle       string
	ExecutionDomain    string
	CommandSurfaceKind string
	ExecutionAttempted bool
	ResultClass        string
	Summary            string
	Reason             string
	RunID              string
	CheckpointID       string
	BriefID            string
	HandoffID          string
	LaunchAttemptID    string
	LaunchID           string
	CreatedAt          time.Time
	CompletedAt        time.Time
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
	Mode           HostMode
	State          HostState
	Label          string
	Note           string
	InputLive      bool
	ExitCode       *int
	Width          int
	Height         int
	LastOutputAt   time.Time
	StateChangedAt time.Time
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

type KnownShellSessionClass string

const (
	KnownShellSessionClassAttachable         KnownShellSessionClass = "attachable"
	KnownShellSessionClassActiveUnattachable KnownShellSessionClass = "active_unattachable"
	KnownShellSessionClassStale              KnownShellSessionClass = "stale"
	KnownShellSessionClassEnded              KnownShellSessionClass = "ended"
)

type KnownShellSession struct {
	SessionID        string
	TaskID           string
	WorkerPreference WorkerPreference
	ResolvedWorker   WorkerPreference
	WorkerSessionID  string
	AttachCapability WorkerAttachCapability
	HostMode         HostMode
	HostState        HostState
	SessionClass     KnownShellSessionClass
	StartedAt        time.Time
	LastUpdatedAt    time.Time
	Active           bool
	Note             string
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
	Footer     string
	Overlay    *OverlayView
	Layout     shellLayout
}

type HeaderView struct {
	Title      string
	TaskLabel  string
	Phase      string
	Worker     string
	Repo       string
	Continuity string
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
