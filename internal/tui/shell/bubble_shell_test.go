package shell

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type testSnapshotSource struct {
	snapshot Snapshot
	next     []Snapshot
	err      error
}

func (s *testSnapshotSource) Load(taskID string) (Snapshot, error) {
	if s.err != nil {
		return Snapshot{}, s.err
	}
	if len(s.next) > 0 {
		next := s.next[0]
		s.next = s.next[1:]
		s.snapshot = next
		return next, nil
	}
	return s.snapshot, nil
}

type testTaskMessageSender struct {
	sent []string
	err  error
}

func (s *testTaskMessageSender) Send(_ string, message string) error {
	if s.err != nil {
		return s.err
	}
	s.sent = append(s.sent, message)
	return nil
}

type testRegistrySource struct {
	sessions []KnownShellSession
	err      error
}

func (s *testRegistrySource) List(taskID string) ([]KnownShellSession, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]KnownShellSession{}, s.sessions...), nil
}

type testPrimaryActionExecutor struct {
	outcome PrimaryActionExecutionOutcome
	err     error
	calls   int
}

func (e *testPrimaryActionExecutor) Execute(_ string, snapshot Snapshot) (PrimaryActionExecutionOutcome, error) {
	e.calls++
	if e.err != nil {
		return PrimaryActionExecutionOutcome{}, e.err
	}
	outcome := e.outcome
	if strings.TrimSpace(outcome.Receipt.ActionHandle) == "" && snapshot.OperatorExecutionPlan != nil && snapshot.OperatorExecutionPlan.PrimaryStep != nil {
		outcome.Receipt.ActionHandle = snapshot.OperatorExecutionPlan.PrimaryStep.Action
	}
	if strings.TrimSpace(outcome.Receipt.ResultClass) == "" {
		outcome.Receipt.ResultClass = "SUCCEEDED"
	}
	if strings.TrimSpace(outcome.Receipt.Summary) == "" {
		outcome.Receipt.Summary = "executed primary step"
	}
	return outcome, nil
}

func TestRenderPreviewShowsHeaderFeedComposerAndFooter(t *testing.T) {
	host := &stubHost{
		canInput: true,
		worker:   "codex",
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
		lines:    []string{"worker> ready"},
	}
	rendered := RenderPreview(Snapshot{
		TaskID: "tsk_preview",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
		Repo: RepoAnchor{
			RepoRoot: "/Users/kagaya/Desktop/Tuku",
		},
	}, UIState{Session: newSessionState(time.Unix(1710000000, 0).UTC())}, host, 120, 32)

	if !strings.Contains(rendered, "TUKU") {
		t.Fatalf("expected TUKU header, got %q", rendered)
	}
	if !strings.Contains(rendered, "Live worker") {
		t.Fatalf("expected stateful composer label, got %q", rendered)
	}
	if !strings.Contains(rendered, "/ commands") {
		t.Fatalf("expected footer command hint, got %q", rendered)
	}
	if !strings.Contains(rendered, "worker> ready") {
		t.Fatalf("expected worker stream in feed, got %q", rendered)
	}
	if strings.Contains(rendered, "@detached") {
		t.Fatalf("expected cleaner repo wording, got %q", rendered)
	}
	if !strings.Contains(rendered, "workspace Tuku") {
		t.Fatalf("expected workspace wording in shell chrome, got %q", rendered)
	}
	if !strings.Contains(rendered, "Ask the worker to inspect, explain, or change something") {
		t.Fatalf("expected guided in-field placeholder, got %q", rendered)
	}
}

func TestRenderPreviewReadOnlyTranscriptUsesIntentionalStateWording(t *testing.T) {
	host := &stubHost{
		worker: "transcript",
		status: HostStatus{Mode: HostModeTranscript, State: HostStateTranscriptOnly, Label: "transcript", InputLive: false},
	}
	rendered := RenderPreview(Snapshot{
		TaskID: "tsk_transcript",
		Repo: RepoAnchor{
			RepoRoot: "/Users/kagaya/Desktop/Tuku",
			Branch:   "main",
		},
	}, UIState{Session: newSessionState(time.Unix(1710000000, 0).UTC())}, host, 100, 24)

	if !strings.Contains(rendered, "READ-ONLY SHELL") {
		t.Fatalf("expected read-only shell note, got %q", rendered)
	}
	if !strings.Contains(rendered, "bounded transcript evidence") {
		t.Fatalf("expected transcript constraint wording, got %q", rendered)
	}
	if strings.Contains(strings.ToLower(rendered), "detached") {
		t.Fatalf("expected cleaner branch wording, got %q", rendered)
	}
}

func TestShellModelOpensSlashMenuAndEscapeDismisses(t *testing.T) {
	host := &stubHost{
		status: HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 120
	model.height = 32
	model.resize()

	_, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if model.overlayKind != shellOverlayCommands {
		t.Fatalf("expected command overlay, got %v", model.overlayKind)
	}

	_, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyEsc})
	if model.overlayKind != shellOverlayNone {
		t.Fatalf("expected command overlay dismissed, got %v", model.overlayKind)
	}
}

func TestShellModelCtrlCRequiresConfirmationAndResets(t *testing.T) {
	host := &stubHost{
		status: HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 120
	model.height = 32
	model.resize()

	_, cmd := model.updateKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected first ctrl-c to arm exit confirmation")
	}
	if !model.exitConfirmActive() {
		t.Fatal("expected exit confirmation to become active")
	}
	if state := model.composerState(); !strings.Contains(state.Hint, "Press Ctrl-C again to exit.") {
		t.Fatalf("expected temporary exit hint, got %+v", state)
	}

	msg := cmd()
	if _, ok := msg.(shellExitConfirmTimeoutMsg); !ok {
		t.Fatalf("expected timeout message, got %#v", msg)
	}
	model.exitConfirmUntil = time.Now().UTC().Add(-time.Second)
	updated, _ := model.Update(msg)
	model = updated.(*shellModel)
	if model.exitConfirmActive() {
		t.Fatal("expected exit confirmation to reset after timeout")
	}
}

func TestShellModelSecondCtrlCQuitsImmediately(t *testing.T) {
	host := &stubHost{
		status: HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{}, UIState{Session: newSessionState(time.Now().UTC())}, host)

	_, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	_, cmd := model.updateKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected second ctrl-c to quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected quit message from second ctrl-c, got %#v", cmd())
	}
}

func TestShellModelStatusCommandAppendsStructuredFeedBlock(t *testing.T) {
	host := &stubHost{
		worker: "codex",
		status: HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{
		TaskID: "tsk_status",
		Phase:  "BRIEF_READY",
		Status: "ACTIVE",
	}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 120
	model.height = 32
	model.resize()
	model.composer.SetValue("/status")
	model.syncOverlayFromComposer()

	_, _ = model.executeSlashCommand("/status")
	if len(model.localEntries) == 0 {
		t.Fatal("expected local status entry")
	}
	last := model.localEntries[len(model.localEntries)-1]
	if last.Title != "Status" {
		t.Fatalf("expected status title, got %+v", last)
	}
	joined := strings.Join(last.Body, "\n")
	if !strings.Contains(joined, "worker codex") || !strings.Contains(joined, "phase BRIEF_READY") {
		t.Fatalf("expected structured status summary, got %q", joined)
	}
}

func TestShellModelSuppressesSystemNarrationInDefaultFeed(t *testing.T) {
	host := &stubHost{
		worker: "codex",
		status: HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{
		TaskID: "tsk_clean",
		RecentConversation: []ConversationItem{
			{Role: "system", Body: "Tuku state updated. Giant control-plane narration."},
			{Role: "worker", Body: "Worker reply."},
		},
	}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.startupConversationBaseline = 0
	rendered := model.renderFeedContent(100)
	if strings.Contains(rendered, "Tuku state updated.") {
		t.Fatalf("expected system narration to stay out of the default feed, got %q", rendered)
	}
	if !strings.Contains(rendered, "Worker reply.") {
		t.Fatalf("expected worker reply to remain visible, got %q", rendered)
	}
}

func TestShellModelSuppressesPersistedConversationOnStartup(t *testing.T) {
	host := &stubHost{
		worker: "codex",
		status: HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{
		TaskID: "tsk_history",
		RecentConversation: []ConversationItem{
			{Role: "user", Body: "help me build the TUI properly"},
			{Role: "worker", Body: "I can help with that."},
		},
	}, UIState{Session: newSessionState(time.Now().UTC())}, host)

	rendered := model.renderFeedContent(100)
	if strings.Contains(rendered, "help me build the TUI properly") || strings.Contains(rendered, "I can help with that.") {
		t.Fatalf("expected persisted startup conversation to stay hidden on first attach, got %q", rendered)
	}
	if !strings.Contains(rendered, "Connected to codex") {
		t.Fatalf("expected clean intro entry on first attach, got %q", rendered)
	}
}

func TestShellModelShowsConversationAddedAfterStartupBaseline(t *testing.T) {
	host := &stubHost{
		worker: "codex",
		status: HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{
		TaskID: "tsk_history",
		RecentConversation: []ConversationItem{
			{Role: "user", Body: "older message"},
			{Role: "worker", Body: "older reply"},
		},
	}, UIState{Session: newSessionState(time.Now().UTC())}, host)

	model.snapshot.RecentConversation = append(model.snapshot.RecentConversation,
		ConversationItem{Role: "user", Body: "new message"},
		ConversationItem{Role: "worker", Body: "new reply"},
	)

	rendered := model.renderFeedContent(100)
	if strings.Contains(rendered, "older message") || strings.Contains(rendered, "older reply") {
		t.Fatalf("expected startup baseline conversation to remain hidden, got %q", rendered)
	}
	if !strings.Contains(rendered, "new message") || !strings.Contains(rendered, "new reply") {
		t.Fatalf("expected new conversation after startup baseline to appear, got %q", rendered)
	}
}

func TestShellModelSubmitToLiveHostWritesInputAndShowsPrompt(t *testing.T) {
	host := &stubHost{
		canInput: true,
		worker:   "codex",
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{TaskID: "tsk_live"}, Snapshot{TaskID: "tsk_live"}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 120
	model.height = 32
	model.resize()
	model.composer.SetValue("Fix the parser")

	_, cmd := model.submitComposer()
	if cmd != nil {
		t.Fatalf("expected live worker submit to avoid async command, got %#v", cmd)
	}
	if len(host.writes) != 1 || string(host.writes[0]) != "Fix the parser\n" {
		t.Fatalf("expected prompt written to live worker, got %#v", host.writes)
	}
	if !model.ui.WorkerPromptPending {
		t.Fatal("expected worker prompt pending state")
	}
	if len(model.localEntries) == 0 || model.localEntries[len(model.localEntries)-1].Kind != shellFeedUser {
		t.Fatalf("expected visible user prompt in feed, got %+v", model.localEntries)
	}
}

func TestShellModelCommitsWorkerReplyBeforeNextUserTurn(t *testing.T) {
	host := &stubHost{
		canInput: true,
		worker:   "codex",
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{TaskID: "tsk_live"}, Snapshot{TaskID: "tsk_live"}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 120
	model.height = 32
	model.resize()
	model.composer.SetValue("Hey")

	_, _ = model.submitComposer()
	host.lines = []string{"First reply."}
	host.canInput = true
	host.status.LastOutputAt = time.Now().UTC().Add(time.Second)
	model.pollHost()

	model.composer.SetValue("What's up?")
	_, _ = model.submitComposer()

	if len(model.localEntries) < 3 {
		t.Fatalf("expected ordered local turns, got %+v", model.localEntries)
	}
	if model.localEntries[0].Kind != shellFeedUser || model.localEntries[0].Body[0] != "Hey" {
		t.Fatalf("expected first user turn first, got %+v", model.localEntries)
	}
	if model.localEntries[1].Kind != shellFeedWorker || !strings.Contains(strings.Join(model.localEntries[1].Body, "\n"), "First reply.") {
		t.Fatalf("expected worker reply before next user turn, got %+v", model.localEntries)
	}
	if model.localEntries[2].Kind != shellFeedUser || model.localEntries[2].Body[0] != "What's up?" {
		t.Fatalf("expected second user turn after worker reply, got %+v", model.localEntries)
	}
}

func TestShellModelCuratesDuplicateWorkerPromptEcho(t *testing.T) {
	host := &stubHost{
		canInput: true,
		worker:   "codex",
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
		lines: []string{
			"tuku> Fix the parser",
			"I'm checking the parser now.",
		},
	}
	model := newShellModel(context.Background(), &App{TaskID: "tsk_live"}, Snapshot{TaskID: "tsk_live"}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 120
	model.height = 32
	model.resize()
	model.pushUserPrompt("Fix the parser")

	rendered := model.renderFeedContent(100)
	if strings.Contains(rendered, "tuku> Fix the parser") {
		t.Fatalf("expected duplicate worker echo to be suppressed, got %q", rendered)
	}
	if !strings.Contains(rendered, "I'm checking the parser now.") {
		t.Fatalf("expected real worker output to remain, got %q", rendered)
	}
}

func TestShellModelWorkerRunningStateStaysConsistent(t *testing.T) {
	host := &stubHost{
		canInput: false,
		worker:   "codex",
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{TaskID: "tsk_live", MessageSender: &testTaskMessageSender{}}, Snapshot{TaskID: "tsk_live"}, UIState{
		Session:             newSessionState(time.Now().UTC()),
		WorkerPromptPending: true,
	}, host)

	state := model.composerState()
	if state.SendMode != "blocked" || state.Status != "worker running" {
		t.Fatalf("expected blocked worker-running state, got %+v", state)
	}
	if strings.Contains(strings.ToLower(state.Hint), "sends through tuku") {
		t.Fatalf("expected no fallback send hint during worker-running state, got %q", state.Hint)
	}
}

func TestShellModelCanonicalSendStateUsesDedicatedDockCopy(t *testing.T) {
	host := &stubHost{
		canInput: false,
		worker:   "transcript",
		status:   HostStatus{Mode: HostModeTranscript, State: HostStateTranscriptOnly, Label: "transcript", InputLive: false},
	}
	model := newShellModel(context.Background(), &App{TaskID: "tsk_send", MessageSender: &testTaskMessageSender{}}, Snapshot{
		TaskID: "tsk_send",
		Repo: RepoAnchor{
			RepoRoot: "/Users/kagaya/Desktop/Tuku",
		},
	}, UIState{Session: newSessionState(time.Now().UTC())}, host)

	state := model.composerState()
	if state.Label != "Tuku message" || state.SendMode != "canonical" {
		t.Fatalf("expected canonical-send dock labeling, got %+v", state)
	}
	if !strings.Contains(shellFooterHint(state), "Enter send via Tuku") {
		t.Fatalf("expected dedicated canonical footer hint, got %q", shellFooterHint(state))
	}
}

func TestShellModelModelSelectionIsExplicitlyPreviewOnly(t *testing.T) {
	host := &stubHost{
		worker: "codex",
		status: HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_model"}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 120
	model.height = 32
	model.resize()

	_, _ = model.selectModelOption("GPT-5.4")
	if len(model.localEntries) == 0 {
		t.Fatal("expected local model note")
	}
	last := model.localEntries[len(model.localEntries)-1]
	joined := strings.Join(last.Body, "\n")
	if !strings.Contains(joined, "Previewed GPT-5.4.") {
		t.Fatalf("expected preview wording, got %q", joined)
	}
	if !strings.Contains(joined, "does not expose an authoritative runtime model switch") {
		t.Fatalf("expected truthful non-switch wording, got %q", joined)
	}
}

func TestShellModelCommandPaletteRendersSections(t *testing.T) {
	host := &stubHost{
		status: HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 120
	model.height = 32
	model.resize()
	model.composer.SetValue("/")
	model.syncOverlayFromComposer()

	rendered := model.renderOverlay(newShellStyles(), model.layout())
	if !strings.Contains(rendered, "ACTIONS") || !strings.Contains(rendered, "CONTEXT") {
		t.Fatalf("expected grouped command palette sections, got %q", rendered)
	}
	if !strings.Contains(rendered, "Commands") {
		t.Fatalf("expected compact command palette title, got %q", rendered)
	}
	if strings.Contains(rendered, "/clear") {
		t.Fatalf("expected compact palette window instead of full inventory dump, got %q", rendered)
	}
}

func TestShellModelInitialViewportStartsOnEntryBoundary(t *testing.T) {
	host := &stubHost{
		worker: "codex",
		status: HostStatus{Mode: HostModeTranscript, State: HostStateTranscriptOnly, Label: "transcript", InputLive: false},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{
		TaskID: "tsk_initial",
		RecentConversation: []ConversationItem{
			{Role: "worker", Body: strings.Repeat("This is a long prior line that should wrap cleanly. ", 12)},
			{Role: "worker", Body: "Most recent worker entry."},
		},
	}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 80
	model.height = 16
	model.resize()

	topLine := strings.TrimSpace(strings.Split(model.viewport.View(), "\n")[0])
	if topLine == "" || strings.HasPrefix(topLine, "prior line") {
		t.Fatalf("expected viewport to start on a deliberate entry boundary, got %q", topLine)
	}
}

func TestShellModelCommandResultScrollsToEntryStart(t *testing.T) {
	host := &stubHost{
		worker: "transcript",
		status: HostStatus{Mode: HostModeTranscript, State: HostStateTranscriptOnly, Label: "transcript", InputLive: false},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{
		TaskID: "tsk_command",
		RecentConversation: []ConversationItem{
			{Role: "worker", Body: strings.Repeat("Older transcript block that should stay above the fold. ", 20)},
		},
	}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 84
	model.height = 16
	model.resize()

	_, _ = model.executeSlashCommand("/inspect")

	topLine := firstNonEmptyViewportLine(model.viewport.View())
	if !strings.Contains(topLine, "INSPECT") {
		t.Fatalf("expected viewport to align to the start of the new command entry, got %q", topLine)
	}
}

func TestShellModelBackToBackCommandsSettleOnLatestEntryBoundary(t *testing.T) {
	host := &stubHost{
		worker: "transcript",
		status: HostStatus{Mode: HostModeTranscript, State: HostStateTranscriptOnly, Label: "transcript", InputLive: false},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{
		TaskID: "tsk_commands",
		RecentConversation: []ConversationItem{
			{Role: "worker", Body: strings.Repeat("Older transcript block that should not force the viewport to the tail. ", 18)},
		},
	}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 84
	model.height = 16
	model.resize()

	_, _ = model.executeSlashCommand("/status")
	_, _ = model.executeSlashCommand("/sessions")

	topLine := firstNonEmptyViewportLine(model.viewport.View())
	if !strings.Contains(topLine, "SESSIONS") {
		t.Fatalf("expected viewport to settle on the latest entry boundary, got %q", topLine)
	}
}

func TestShellModelSubmitWithoutLiveInputUsesMessageSenderAndRefreshesSnapshot(t *testing.T) {
	sender := &testTaskMessageSender{}
	source := &testSnapshotSource{
		next: []Snapshot{{
			TaskID: "tsk_send",
			RecentConversation: []ConversationItem{
				{Role: "user", Body: "Plan the next slice"},
				{Role: "worker", Body: "Bounded plan ready."},
			},
		}},
	}
	registry := &testRegistrySource{
		sessions: []KnownShellSession{{SessionID: "shs_old", TaskID: "tsk_send"}},
	}
	host := &stubHost{
		canInput: false,
		status:   HostStatus{Mode: HostModeTranscript, State: HostStateTranscriptOnly, Label: "transcript", InputLive: false},
	}
	model := newShellModel(context.Background(), &App{
		TaskID:         "tsk_send",
		Source:         source,
		MessageSender:  sender,
		RegistrySource: registry,
	}, Snapshot{TaskID: "tsk_send"}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 120
	model.height = 32
	model.resize()
	model.composer.SetValue("Plan the next slice")

	_, cmd := model.submitComposer()
	if cmd == nil {
		t.Fatal("expected async send command")
	}
	msg := cmd()
	updatedModel, _ := model.Update(msg)
	model = updatedModel.(*shellModel)

	if len(sender.sent) != 1 || sender.sent[0] != "Plan the next slice" {
		t.Fatalf("expected sender to receive prompt, got %#v", sender.sent)
	}
	if len(model.snapshot.RecentConversation) != 2 {
		t.Fatalf("expected refreshed snapshot conversation, got %+v", model.snapshot.RecentConversation)
	}
	if len(model.ui.Session.KnownSessions) != 1 || model.ui.Session.KnownSessions[0].SessionID != "shs_old" {
		t.Fatalf("expected refreshed durable sessions, got %+v", model.ui.Session.KnownSessions)
	}
}

func TestShellModelWorkerPromptPendingWaitsForLiveInputToReturn(t *testing.T) {
	now := time.Now().UTC()
	host := &stubHost{
		canInput: false,
		worker:   "codex",
		status: HostStatus{
			Mode:         HostModeCodexPTY,
			State:        HostStateLive,
			Label:        "codex live",
			InputLive:    true,
			LastOutputAt: now.Add(time.Second),
		},
	}
	model := newShellModel(context.Background(), &App{
		TaskID:        "tsk_live",
		MessageSender: &testTaskMessageSender{},
	}, Snapshot{TaskID: "tsk_live"}, UIState{
		Session:             newSessionState(now),
		WorkerPromptPending: true,
		LastWorkerPromptAt:  now,
	}, host)

	model.pollHost()
	if !model.ui.WorkerPromptPending {
		t.Fatal("expected worker prompt to remain pending until live input is writable again")
	}
	if state := model.composerState(); state.Status != "worker running" || state.SendMode != "blocked" {
		t.Fatalf("expected worker-running dock state while host is still recovering, got %+v", state)
	}

	host.canInput = true
	model.pollHost()
	if model.ui.WorkerPromptPending {
		t.Fatal("expected worker prompt pending to clear once live input returns")
	}
	if state := model.composerState(); state.SendMode != "worker" || state.Status != "live worker" {
		t.Fatalf("expected dock to return directly to live-worker state, got %+v", state)
	}
}

func TestShellModelNextCommandExecutesPrimaryAction(t *testing.T) {
	executor := &testPrimaryActionExecutor{
		outcome: PrimaryActionExecutionOutcome{
			Receipt: OperatorStepReceiptSummary{
				ReceiptID:    "orec_1",
				ActionHandle: "START_LOCAL_RUN",
				ResultClass:  "SUCCEEDED",
				Summary:      "started local run run_1",
			},
		},
	}
	source := &testSnapshotSource{
		next: []Snapshot{{
			TaskID: "tsk_next",
			OperatorExecutionPlan: &OperatorExecutionPlan{
				PrimaryStep: &OperatorExecutionStep{
					Action:         "FINALIZE_CONTINUE_RECOVERY",
					Status:         "REQUIRED_NEXT",
					CommandSurface: "DEDICATED",
					CommandHint:    "tuku recovery continue --task tsk_next",
				},
			},
		}},
	}
	host := &stubHost{
		status: HostStatus{Mode: HostModeTranscript, State: HostStateTranscriptOnly, Label: "transcript", InputLive: false},
	}
	model := newShellModel(context.Background(), &App{
		TaskID:         "tsk_next",
		Source:         source,
		ActionExecutor: executor,
	}, Snapshot{
		TaskID: "tsk_next",
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "START_LOCAL_RUN",
				Status:         "REQUIRED_NEXT",
				CommandSurface: "DEDICATED",
				CommandHint:    "tuku run --task tsk_next",
			},
		},
	}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 120
	model.height = 32
	model.resize()

	_, cmd := model.executeSlashCommand("/next")
	if cmd == nil {
		t.Fatal("expected async primary action command")
	}
	msg := cmd()
	updatedModel, _ := model.Update(msg)
	model = updatedModel.(*shellModel)

	if executor.calls != 1 {
		t.Fatalf("expected one primary action execution, got %d", executor.calls)
	}
	if model.ui.LastPrimaryActionResult == nil || model.ui.LastPrimaryActionResult.Outcome != "SUCCESS" {
		t.Fatalf("expected successful primary action result, got %+v", model.ui.LastPrimaryActionResult)
	}
}

func TestExecutePrimaryOperatorStepRefreshesSnapshotAfterSuccess(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	executor := &testPrimaryActionExecutor{
		outcome: PrimaryActionExecutionOutcome{
			Receipt: OperatorStepReceiptSummary{
				ReceiptID:    "orec_123",
				ActionHandle: "START_LOCAL_RUN",
				ResultClass:  "SUCCEEDED",
				Summary:      "started local run run_123",
				CreatedAt:    now,
			},
		},
	}
	source := &testSnapshotSource{
		next: []Snapshot{{
			TaskID: "tsk_1",
			Phase:  "BRIEF_READY",
			OperatorExecutionPlan: &OperatorExecutionPlan{
				PrimaryStep: &OperatorExecutionStep{
					Action:         "FINALIZE_CONTINUE_RECOVERY",
					CommandSurface: "DEDICATED",
					CommandHint:    "tuku recovery continue --task tsk_1",
				},
			},
		}},
	}
	host := &stubHost{}
	snapshot := Snapshot{
		TaskID: "tsk_1",
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "START_LOCAL_RUN",
				CommandSurface: "DEDICATED",
				CommandHint:    "tuku run --task tsk_1",
			},
		},
	}
	ui := UIState{Session: newSessionState(now)}

	if err := executePrimaryOperatorStep(executor, source, "tsk_1", host, nil, &snapshot, &ui); err != nil {
		t.Fatalf("execute primary operator step: %v", err)
	}
	if snapshot.OperatorExecutionPlan.PrimaryStep.Action != "FINALIZE_CONTINUE_RECOVERY" {
		raw, _ := json.Marshal(snapshot.OperatorExecutionPlan)
		t.Fatalf("expected refreshed snapshot after execution, got %s", raw)
	}
	if ui.LastPrimaryActionResult == nil || ui.LastPrimaryActionResult.Outcome != "SUCCESS" {
		t.Fatalf("expected success result summary, got %+v", ui.LastPrimaryActionResult)
	}
}

func TestSendPendingTaskMessageClearsDraft(t *testing.T) {
	sender := &testTaskMessageSender{}
	ui := UIState{
		Session:            newSessionState(time.Now().UTC()),
		PendingTaskMessage: "Adopt the scratch notes",
	}
	if err := sendPendingTaskMessage(sender, "tsk_pending", &ui); err != nil {
		t.Fatalf("send pending task message: %v", err)
	}
	if len(sender.sent) != 1 || sender.sent[0] != "Adopt the scratch notes" {
		t.Fatalf("expected sent draft, got %#v", sender.sent)
	}
	if ui.PendingTaskMessage != "" {
		t.Fatalf("expected pending task message cleared, got %q", ui.PendingTaskMessage)
	}
}

func TestShellSessionsLinesUseMoreDistinctSessionIDs(t *testing.T) {
	lines := shellSessionsLines(SessionState{
		KnownSessions: []KnownShellSession{
			{
				SessionID:        "shs_1774902598960245000",
				AttachCapability: WorkerAttachCapabilityAttachable,
				OperatorSummary:  "first session",
			},
			{
				SessionID:        "shs_1774902598960249000",
				AttachCapability: WorkerAttachCapabilityAttachable,
				OperatorSummary:  "second session",
			},
		},
	}, Snapshot{})

	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "shs_177490…245000") || !strings.Contains(joined, "shs_177490…249000") {
		t.Fatalf("expected more distinguishing session ids, got %q", joined)
	}
}

func firstNonEmptyViewportLine(view string) string {
	for _, line := range strings.Split(view, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}
