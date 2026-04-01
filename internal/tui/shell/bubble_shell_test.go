package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

func TestShellModelWindowResizePreservesFeedStateWithoutClearingScreen(t *testing.T) {
	host := &stubHost{
		worker: "codex",
		status: HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
		lines:  []string{"worker> ready"},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_resize"}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 120
	model.height = 32
	model.resize()
	before := model.renderFeedContent(100)

	updated, cmd := model.Update(tea.WindowSizeMsg{Width: 96, Height: 24})
	model = updated.(*shellModel)
	if cmd != nil {
		if got := fmt.Sprintf("%T", cmd()); got == "tea.clearScreenMsg" {
			t.Fatalf("did not expect resize to clear the normal screen")
		}
	}
	if len(model.localEntries) != 0 {
		t.Fatalf("expected resize to avoid appending feed entries, got %+v", model.localEntries)
	}
	after := model.renderFeedContent(100)
	if before != after {
		t.Fatalf("expected resize to preserve feed content, before=%q after=%q", before, after)
	}
}

func TestShellModelRepeatedResizeDoesNotAppendDuplicateShellContent(t *testing.T) {
	host := &stubHost{
		worker: "codex",
		status: HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
		lines:  []string{"worker> ready"},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_resize_repeat"}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 120
	model.height = 32
	model.resize()
	initialContent := model.lastContent

	for _, size := range []tea.WindowSizeMsg{
		{Width: 110, Height: 28},
		{Width: 104, Height: 26},
		{Width: 118, Height: 30},
	} {
		updated, _ := model.Update(size)
		model = updated.(*shellModel)
	}

	if len(model.localEntries) != 0 {
		t.Fatalf("expected repeated resize events to avoid duplicating local entries, got %+v", model.localEntries)
	}
	if strings.Count(model.lastContent, "worker> ready") != 1 {
		t.Fatalf("expected rendered feed to contain one worker stream instance, got %q", model.lastContent)
	}
	if strings.Count(model.View(), "[ready]") > 1 {
		t.Fatalf("expected shell chrome to render once after resize, got %q", model.View())
	}
	if initialContent == "" || model.lastContent == "" {
		t.Fatalf("expected resize flow to maintain rendered content, initial=%q current=%q", initialContent, model.lastContent)
	}
}

func TestShellModelViewFitsActualWindowAfterResize(t *testing.T) {
	host := &stubHost{
		worker: "codex",
		status: HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
		lines:  []string{"worker> ready"},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_runtime_fit"}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 52
	model.height = 12
	model.resize()

	assertRenderedViewFitsWindow(t, model.View(), model.width, model.height)

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 44, Height: 9})
	model = updated.(*shellModel)
	assertRenderedViewFitsWindow(t, model.View(), model.width, model.height)
}

func TestShellModelWideLayoutWidthStaysRootDerivedAcrossShellStates(t *testing.T) {
	host := &stubHost{
		canInterrupt: true,
		worker:       "codex",
		status:       HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_width_authority"}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 132
	model.height = 28
	model.resize()

	idle := model.layout()
	if idle.contentWidth < 120 {
		t.Fatalf("expected wide shells to keep a wide root-derived content area, got %+v", idle)
	}

	model.ui.WorkerPromptPending = true
	model.ui.LastWorkerPromptAt = time.Now().UTC().Add(-3 * time.Second)
	working := model.layout()
	if working.contentWidth != idle.contentWidth || working.feedWidth != idle.feedWidth {
		t.Fatalf("expected working state not to collapse root width, idle=%+v working=%+v", idle, working)
	}

	model.composer.SetValue("/")
	model.syncOverlayFromComposer()
	overlay := model.layout()
	if overlay.contentWidth != idle.contentWidth || overlay.feedWidth != idle.feedWidth {
		t.Fatalf("expected overlay state not to feed width back into root layout, idle=%+v overlay=%+v", idle, overlay)
	}
}

func TestShellModelResizeDoesNotReappendIntroContent(t *testing.T) {
	host := &stubHost{
		worker: "codex",
		status: HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_intro"}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 80
	model.height = 18
	model.resize()

	for _, size := range []tea.WindowSizeMsg{
		{Width: 72, Height: 16},
		{Width: 68, Height: 14},
		{Width: 90, Height: 22},
	} {
		updated, _ := model.Update(size)
		model = updated.(*shellModel)
	}

	rendered := model.renderFeedContent(80)
	if strings.Count(rendered, "Connected to codex") != 1 {
		t.Fatalf("expected intro content to remain singular after resize, got %q", rendered)
	}
}

func TestShellModelComposerStaysPackedBelowLatestTranscriptContent(t *testing.T) {
	host := &stubHost{
		worker:   "codex",
		canInput: true,
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_layout"}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 90
	model.height = 24
	model.resize()

	model.pushUserPrompt("hi")
	model.appendLocalEntry(shellFeedEntry{
		Key:          "worker-reply",
		Kind:         shellFeedWorker,
		Title:        "Worker",
		Body:         []string{"worker says hi"},
		Preformatted: true,
	})
	model.syncViewport(true)

	lines := splitLines(model.View())
	headerLine := findLineContaining(lines, "TUKU shell")
	promptLine := findLineContaining(lines, "› hi")
	workerLine := findLastLineContaining(lines, "worker says hi")
	composerLine := findLineContaining(lines, "Live worker")

	if headerLine != 0 {
		t.Fatalf("expected header to stay top-anchored, got %d", headerLine)
	}
	if promptLine == -1 || workerLine == -1 || composerLine == -1 {
		t.Fatalf("expected prompt, worker reply, and composer in view, got %q", model.View())
	}
	if !(promptLine < workerLine && workerLine < composerLine) {
		t.Fatalf("expected chronological prompt -> worker -> composer order, got prompt=%d worker=%d composer=%d view=%q", promptLine, workerLine, composerLine, model.View())
	}
	if composerLine-workerLine > 3 {
		t.Fatalf("expected composer to sit directly below the latest transcript content, got worker=%d composer=%d", workerLine, composerLine)
	}
	if len(lines) >= model.height {
		t.Fatalf("expected extra terminal height to remain below the packed shell content, got %d lines for height %d", len(lines), model.height)
	}
}

func TestShellModelTallResizeKeepsBlankSpaceBelowComposerNotAboveIt(t *testing.T) {
	host := &stubHost{
		worker:   "codex",
		canInput: true,
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_resize_layout"}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 90
	model.height = 18
	model.resize()
	model.pushUserPrompt("hi")
	model.appendLocalEntry(shellFeedEntry{
		Key:          "worker-reply",
		Kind:         shellFeedWorker,
		Title:        "Worker",
		Body:         []string{"reply ready"},
		Preformatted: true,
	})
	model.syncViewport(true)

	shortLines := splitLines(model.View())
	shortComposer := findLineContaining(shortLines, "Live worker")

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	model = updated.(*shellModel)
	tallLines := splitLines(model.View())
	tallHeader := findLineContaining(tallLines, "TUKU shell")
	tallWorker := findLastLineContaining(tallLines, "reply ready")
	tallComposer := findLineContaining(tallLines, "Live worker")

	if tallHeader != 0 {
		t.Fatalf("expected header to remain pinned to the top after resize, got %d", tallHeader)
	}
	if tallComposer-shortComposer > 4 {
		t.Fatalf("expected taller terminals to add empty space below the composer instead of pushing it downward, got short=%d tall=%d", shortComposer, tallComposer)
	}
	if tallWorker == -1 || tallComposer == -1 {
		t.Fatalf("expected worker reply and composer in resized view, got %q", model.View())
	}
	if tallComposer-tallWorker > 3 {
		t.Fatalf("expected composer to remain packed below the latest transcript after resize, got worker=%d composer=%d", tallWorker, tallComposer)
	}
	if len(tallLines) >= model.height {
		t.Fatalf("expected resized shell content to stay top-packed with blank space below, got %d lines for height %d", len(tallLines), model.height)
	}
}

func TestShellModelPgUpAndPgDnRestoreScrollableHistory(t *testing.T) {
	host := &stubHost{
		worker: "transcript",
		status: HostStatus{Mode: HostModeTranscript, State: HostStateTranscriptOnly, Label: "transcript", InputLive: false},
	}
	conversation := make([]ConversationItem, 0, 24)
	for i := 0; i < 12; i++ {
		conversation = append(conversation,
			ConversationItem{Role: "user", Body: fmt.Sprintf("user prompt %02d", i)},
			ConversationItem{Role: "worker", Body: strings.Repeat(fmt.Sprintf("worker reply %02d ", i), 8)},
		)
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{
		TaskID:             "tsk_scroll",
		RecentConversation: conversation,
	}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.startupConversationBaseline = 0
	model.width = 84
	model.height = 14
	model.resize()

	bottomOffset := model.viewport.YOffset
	updated, _ := model.updateKey(tea.KeyMsg{Type: tea.KeyPgUp})
	model = updated.(*shellModel)
	if model.viewport.YOffset >= bottomOffset {
		t.Fatalf("expected PgUp to scroll into prior turns, got before=%d after=%d", bottomOffset, model.viewport.YOffset)
	}
	if model.followLatest {
		t.Fatal("expected manual upward scroll to disable follow-latest mode")
	}

	upOffset := model.viewport.YOffset
	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyPgDown})
	model = updated.(*shellModel)
	if model.viewport.YOffset <= upOffset {
		t.Fatalf("expected PgDn to move back toward recent turns, got before=%d after=%d", upOffset, model.viewport.YOffset)
	}
}

func TestShellModelLiveWorkerHistoryScrollbackReachesTopOfLongOutput(t *testing.T) {
	history := make([]string, 0, 220)
	for i := 0; i < 220; i++ {
		history = append(history, fmt.Sprintf("worker line %03d", i))
	}
	host := &stubHost{
		worker:       "codex",
		canInput:     true,
		historyLines: history,
		status:       HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true, RenderVersion: 1},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_history_scroll"}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 86
	model.height = 14
	model.resize()

	if !strings.Contains(model.viewport.View(), "worker line 219") {
		t.Fatalf("expected newest worker output at tail, got %q", model.viewport.View())
	}

	for i := 0; i < 80 && model.viewport.YOffset > 0; i++ {
		updated, _ := model.updateKey(tea.KeyMsg{Type: tea.KeyPgUp})
		model = updated.(*shellModel)
	}

	if model.viewport.YOffset != 0 {
		t.Fatalf("expected scrollback to reach the top of the feed, got offset=%d", model.viewport.YOffset)
	}
	updated, _ := model.updateKey(tea.KeyMsg{Type: tea.KeyPgDown})
	model = updated.(*shellModel)
	if !strings.Contains(model.viewport.View(), "worker line 000") {
		t.Fatalf("expected earliest worker output to become visible after scrolling through history, got %q", model.viewport.View())
	}
	if model.followLatest {
		t.Fatal("expected manual scrollback to disable follow-latest mode")
	}
}

func TestShellModelHomeAndEndJumpBetweenTopAndLatest(t *testing.T) {
	history := make([]string, 0, 180)
	for i := 0; i < 180; i++ {
		history = append(history, fmt.Sprintf("jump line %03d", i))
	}
	host := &stubHost{
		worker:       "codex",
		canInput:     true,
		historyLines: history,
		status:       HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true, RenderVersion: 1},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_home_end"}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 86
	model.height = 14
	model.resize()

	updated, _ := model.updateKey(tea.KeyMsg{Type: tea.KeyHome})
	model = updated.(*shellModel)
	if model.viewport.YOffset != 0 {
		t.Fatalf("expected Home to jump to the top of transcript history, got offset=%d", model.viewport.YOffset)
	}
	if model.followLatest {
		t.Fatal("expected Home to leave live-follow mode")
	}
	if !strings.Contains(model.viewport.View(), "Connected to codex") {
		t.Fatalf("expected Home to reveal the top of the transcript feed, got %q", model.viewport.View())
	}

	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyEnd})
	model = updated.(*shellModel)
	if !model.viewport.AtBottom() || !model.followLatest {
		t.Fatalf("expected End to restore latest/follow mode, offset=%d follow=%v", model.viewport.YOffset, model.followLatest)
	}
	if !strings.Contains(model.viewport.View(), "jump line 179") {
		t.Fatalf("expected End to reveal latest transcript content, got %q", model.viewport.View())
	}
}

func TestShellModelArrowKeysScrollLineByLineOnlyWhenComposerIdle(t *testing.T) {
	history := make([]string, 0, 120)
	for i := 0; i < 120; i++ {
		history = append(history, fmt.Sprintf("line scroll %03d", i))
	}
	host := &stubHost{
		worker:       "codex",
		canInput:     true,
		historyLines: history,
		status:       HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true, RenderVersion: 1},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_line_scroll"}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 80
	model.height = 12
	model.resize()

	startOffset := model.viewport.YOffset
	updated, _ := model.updateKey(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(*shellModel)
	if model.viewport.YOffset >= startOffset {
		t.Fatalf("expected Up to scroll one line upward when composer is idle, before=%d after=%d", startOffset, model.viewport.YOffset)
	}

	lineUpOffset := model.viewport.YOffset
	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(*shellModel)
	if model.viewport.YOffset <= lineUpOffset {
		t.Fatalf("expected Down to scroll one line back toward the latest output, before=%d after=%d", lineUpOffset, model.viewport.YOffset)
	}

	model.composer.SetValue("draft message")
	idleOffset := model.viewport.YOffset
	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(*shellModel)
	if model.viewport.YOffset != idleOffset {
		t.Fatalf("expected Up to preserve transcript position while editing composer text, before=%d after=%d", idleOffset, model.viewport.YOffset)
	}
}

func TestShellModelMouseWheelScrollsTranscriptWithoutTouchingComposer(t *testing.T) {
	history := make([]string, 0, 140)
	for i := 0; i < 140; i++ {
		history = append(history, fmt.Sprintf("wheel line %03d", i))
	}
	host := &stubHost{
		worker:       "codex",
		canInput:     true,
		historyLines: history,
		status:       HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true, RenderVersion: 1},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_mouse_scroll"}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 84
	model.height = 14
	model.resize()
	model.composer.SetValue("keep this draft")

	feedY := model.layout().headerHeight + 1
	startOffset := model.viewport.YOffset
	updated, _ := model.Update(tea.MouseMsg{
		X:      4,
		Y:      feedY,
		Type:   tea.MouseWheelUp,
		Button: tea.MouseButtonWheelUp,
		Action: tea.MouseActionPress,
	})
	model = updated.(*shellModel)
	if model.viewport.YOffset >= startOffset {
		t.Fatalf("expected mouse wheel up to scroll transcript upward, before=%d after=%d", startOffset, model.viewport.YOffset)
	}
	if model.followLatest {
		t.Fatal("expected mouse wheel inspection to leave live-follow mode")
	}
	if model.composer.Value() != "keep this draft" {
		t.Fatalf("expected mouse wheel scrolling not to disturb composer text, got %q", model.composer.Value())
	}

	upOffset := model.viewport.YOffset
	updated, _ = model.Update(tea.MouseMsg{
		X:      4,
		Y:      feedY,
		Type:   tea.MouseWheelDown,
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
	})
	model = updated.(*shellModel)
	if model.viewport.YOffset <= upOffset {
		t.Fatalf("expected mouse wheel down to scroll back toward latest output, before=%d after=%d", upOffset, model.viewport.YOffset)
	}
}

func TestShellModelMouseWheelOutsideViewportDoesNotScrollTranscript(t *testing.T) {
	history := make([]string, 0, 140)
	for i := 0; i < 140; i++ {
		history = append(history, fmt.Sprintf("outside line %03d", i))
	}
	host := &stubHost{
		worker:       "codex",
		canInput:     true,
		historyLines: history,
		status:       HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true, RenderVersion: 1},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_mouse_ignore"}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 84
	model.height = 14
	model.resize()

	startOffset := model.viewport.YOffset
	outsideY := model.layout().headerHeight + model.viewport.Height + 2
	updated, _ := model.Update(tea.MouseMsg{
		X:      4,
		Y:      outsideY,
		Type:   tea.MouseWheelUp,
		Button: tea.MouseButtonWheelUp,
		Action: tea.MouseActionPress,
	})
	model = updated.(*shellModel)
	if model.viewport.YOffset != startOffset {
		t.Fatalf("expected mouse wheel outside the transcript viewport to leave scroll position unchanged, before=%d after=%d", startOffset, model.viewport.YOffset)
	}
}

func TestShellModelResizeWhileScrolledUpKeepsHistoricalContentVisible(t *testing.T) {
	history := make([]string, 0, 180)
	for i := 0; i < 180; i++ {
		history = append(history, fmt.Sprintf("history line %03d", i))
	}
	host := &stubHost{
		worker:       "codex",
		canInput:     true,
		historyLines: history,
		status:       HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true, RenderVersion: 1},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_scroll_resize"}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 96
	model.height = 18
	model.resize()

	for i := 0; i < 40 && model.viewport.YOffset > 0; i++ {
		updated, _ := model.updateKey(tea.KeyMsg{Type: tea.KeyPgUp})
		model = updated.(*shellModel)
	}
	if model.followLatest || model.viewport.AtBottom() {
		t.Fatalf("expected manual scroll to move away from the live tail, offset=%d", model.viewport.YOffset)
	}

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 72, Height: 8})
	model = updated.(*shellModel)
	if strings.TrimSpace(model.viewport.View()) == "" {
		t.Fatalf("expected shrunken viewport to keep transcript visible, got %q", model.viewport.View())
	}
	if model.viewport.AtBottom() {
		t.Fatalf("expected shrink while scrolled up to avoid snapping back to the tail, got offset=%d", model.viewport.YOffset)
	}

	updated, _ = model.Update(tea.WindowSizeMsg{Width: 96, Height: 20})
	model = updated.(*shellModel)
	if strings.TrimSpace(model.viewport.View()) == "" {
		t.Fatalf("expected restored viewport to keep transcript visible, got %q", model.viewport.View())
	}
	if model.viewport.AtBottom() {
		t.Fatalf("expected restore while manually scrolled up to avoid snapping back to the tail, got offset=%d", model.viewport.YOffset)
	}
}

func TestShellModelShrinkAndRestoreKeepsTranscriptVisible(t *testing.T) {
	host := &stubHost{
		worker:   "codex",
		canInput: true,
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_resize_recover"}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 104
	model.height = 16
	model.resize()
	model.pushUserPrompt("recover the layout")
	model.appendLocalEntry(shellFeedEntry{
		Key:          "resize-recover-worker",
		Kind:         shellFeedWorker,
		Title:        "Worker",
		Body:         []string{"Most recent worker entry survives resize recovery."},
		Preformatted: true,
	})
	model.syncViewport(true)

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 42, Height: 9})
	model = updated.(*shellModel)
	assertRenderedViewFitsWindow(t, model.View(), model.width, model.height)
	if strings.TrimSpace(model.viewport.View()) == "" {
		t.Fatalf("expected narrow resize to keep transcript visible, got %q", model.viewport.View())
	}

	updated, _ = model.Update(tea.WindowSizeMsg{Width: 104, Height: 16})
	model = updated.(*shellModel)
	assertRenderedViewFitsWindow(t, model.View(), model.width, model.height)
	if strings.TrimSpace(model.viewport.View()) == "" {
		t.Fatalf("expected restored resize to recover transcript visibility, got %q", model.viewport.View())
	}
	if !strings.Contains(model.renderFeedContent(model.layout().contentWidth), "Most recent worker entry survives resize recovery.") {
		t.Fatalf("expected restored shell to retain the latest transcript content, got %q", model.renderFeedContent(model.layout().contentWidth))
	}
}

func TestShellModelScrolledViewportDoesNotSnapBackOnWorkingTick(t *testing.T) {
	now := time.Now().UTC()
	host := &stubHost{
		worker: "codex",
		lines:  []string{"Ran rg --files", "internal/tui/shell/bubble_shell.go", "Checked worker output"},
		status: HostStatus{
			Mode:         HostModeCodexPTY,
			State:        HostStateLive,
			Label:        "codex live",
			InputLive:    true,
			LastOutputAt: now,
		},
	}
	conversation := make([]ConversationItem, 0, 20)
	for i := 0; i < 10; i++ {
		conversation = append(conversation,
			ConversationItem{Role: "user", Body: fmt.Sprintf("prompt %02d", i)},
			ConversationItem{Role: "worker", Body: strings.Repeat(fmt.Sprintf("reply %02d ", i), 8)},
		)
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{
		TaskID:             "tsk_tick",
		RecentConversation: conversation,
	}, UIState{
		Session:               newSessionState(now),
		WorkerPromptPending:   true,
		WorkerResponseStarted: true,
		LastWorkerPromptAt:    now.Add(-5 * time.Second),
	}, host)
	model.startupConversationBaseline = 0
	model.width = 84
	model.height = 14
	model.resize()

	updated, _ := model.updateKey(tea.KeyMsg{Type: tea.KeyPgUp})
	model = updated.(*shellModel)
	scrolledOffset := model.viewport.YOffset

	updatedModel, _ := model.Update(shellWorkingTickMsg{})
	model = updatedModel.(*shellModel)
	if model.viewport.YOffset != scrolledOffset {
		t.Fatalf("expected working tick to preserve manual scroll position, got before=%d after=%d", scrolledOffset, model.viewport.YOffset)
	}
	if model.followLatest {
		t.Fatal("expected working tick to keep follow-latest disabled while user is scrolled up")
	}
}

func TestShellModelFollowLatestKeepsNewestWorkerOutputVisibleAtBottom(t *testing.T) {
	now := time.Now().UTC()
	host := &stubHost{
		worker: "codex",
		lines:  []string{"Explored repo state", "Read internal/tui/shell/bubble_shell.go"},
		status: HostStatus{
			Mode:         HostModeCodexPTY,
			State:        HostStateLive,
			Label:        "codex live",
			InputLive:    true,
			LastOutputAt: now,
		},
	}
	conversation := make([]ConversationItem, 0, 18)
	for i := 0; i < 9; i++ {
		conversation = append(conversation,
			ConversationItem{Role: "user", Body: fmt.Sprintf("prompt %02d", i)},
			ConversationItem{Role: "worker", Body: strings.Repeat(fmt.Sprintf("reply %02d ", i), 7)},
		)
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{
		TaskID:             "tsk_follow",
		RecentConversation: conversation,
	}, UIState{
		Session:               newSessionState(now),
		WorkerPromptPending:   true,
		WorkerResponseStarted: true,
		LastWorkerPromptAt:    now.Add(-5 * time.Second),
	}, host)
	model.startupConversationBaseline = 0
	model.width = 84
	model.height = 14
	model.resize()

	host.lines = append(host.lines, "Ran go test ./internal/tui/shell")
	host.status.LastOutputAt = now.Add(time.Second)
	model.pollHost()

	if !model.followLatest || !model.viewport.AtBottom() {
		t.Fatalf("expected follow-latest mode to keep the viewport pinned to the tail, offset=%d", model.viewport.YOffset)
	}
	if !strings.Contains(model.viewport.View(), "Ran go test ./internal/tui/shell") {
		t.Fatalf("expected newest worker output to stay visible at the bottom, got %q", model.viewport.View())
	}
}

func TestShellModelWorkerOutputDoesNotYankViewportWhenUserScrolledUp(t *testing.T) {
	now := time.Now().UTC()
	host := &stubHost{
		worker: "codex",
		lines:  []string{"Explored repo state", "Read internal/tui/shell/bubble_shell.go"},
		status: HostStatus{
			Mode:         HostModeCodexPTY,
			State:        HostStateLive,
			Label:        "codex live",
			InputLive:    true,
			LastOutputAt: now,
		},
	}
	conversation := make([]ConversationItem, 0, 18)
	for i := 0; i < 9; i++ {
		conversation = append(conversation,
			ConversationItem{Role: "user", Body: fmt.Sprintf("prompt %02d", i)},
			ConversationItem{Role: "worker", Body: strings.Repeat(fmt.Sprintf("reply %02d ", i), 7)},
		)
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{
		TaskID:             "tsk_no_yank",
		RecentConversation: conversation,
	}, UIState{
		Session:               newSessionState(now),
		WorkerPromptPending:   true,
		WorkerResponseStarted: true,
		LastWorkerPromptAt:    now.Add(-5 * time.Second),
	}, host)
	model.startupConversationBaseline = 0
	model.width = 84
	model.height = 14
	model.resize()

	updated, _ := model.updateKey(tea.KeyMsg{Type: tea.KeyPgUp})
	model = updated.(*shellModel)
	scrolledOffset := model.viewport.YOffset

	host.lines = append(host.lines, "Waited on worker response", "internal/tui/shell/app.go")
	host.status.LastOutputAt = now.Add(time.Second)
	model.pollHost()

	if model.viewport.YOffset != scrolledOffset {
		t.Fatalf("expected worker output to preserve manual scroll position, got before=%d after=%d", scrolledOffset, model.viewport.YOffset)
	}
	if model.followLatest {
		t.Fatal("expected manual scroll mode to remain active after new worker output")
	}
}

func TestShellModelCommandPalettePreservesViewportAndComposerAnchoring(t *testing.T) {
	host := &stubHost{
		canInput: true,
		worker:   "codex",
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	conversation := make([]ConversationItem, 0, 18)
	for i := 0; i < 9; i++ {
		conversation = append(conversation,
			ConversationItem{Role: "user", Body: fmt.Sprintf("prompt %02d", i)},
			ConversationItem{Role: "worker", Body: strings.Repeat(fmt.Sprintf("reply %02d ", i), 7)},
		)
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{
		TaskID:             "tsk_palette",
		RecentConversation: conversation,
	}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.startupConversationBaseline = 0
	model.width = 88
	model.height = 18
	model.resize()

	updated, _ := model.updateKey(tea.KeyMsg{Type: tea.KeyPgUp})
	model = updated.(*shellModel)
	scrolledOffset := model.viewport.YOffset

	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	model = updated.(*shellModel)
	openLines := splitLines(model.View())
	if model.viewport.YOffset != scrolledOffset {
		t.Fatalf("expected command palette open to preserve feed scroll position, got before=%d after=%d", scrolledOffset, model.viewport.YOffset)
	}
	if composerLine := findLineContaining(openLines, "Command filter"); composerLine != model.layout().headerHeight+model.viewport.Height+1 {
		t.Fatalf("expected palette open to keep the composer directly after visible transcript content, got composer=%d viewport=%d", composerLine, model.viewport.Height)
	}

	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(*shellModel)
	closedLines := splitLines(model.View())
	if composerLine := findLineContaining(closedLines, "Live worker"); composerLine != model.layout().headerHeight+model.viewport.Height+1 {
		t.Fatalf("expected palette close to keep the composer directly after visible transcript content, got composer=%d viewport=%d", composerLine, model.viewport.Height)
	}
	if model.viewport.YOffset != scrolledOffset {
		t.Fatalf("expected palette close to preserve feed scroll position, got before=%d after=%d", scrolledOffset, model.viewport.YOffset)
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

func TestShellModelUserPromptRendersAsCleanPromptLine(t *testing.T) {
	host := &stubHost{
		canInput: true,
		worker:   "codex",
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_prompt"}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 100
	model.height = 24
	model.resize()
	model.pushUserPrompt("help")

	rendered := model.renderFeedContent(100)
	if !strings.Contains(rendered, "› help") {
		t.Fatalf("expected clean prompt-line rendering, got %q", rendered)
	}
	if strings.Contains(rendered, "[YOU]") {
		t.Fatalf("expected prompt-line rendering instead of boxed YOU label, got %q", rendered)
	}
	if strings.Count(rendered, "› help") != 1 {
		t.Fatalf("expected prompt to render exactly once, got %q", rendered)
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
	model.ui.LastWorkerPromptAt = time.Now().UTC().Add(-shellWorkerSettleGrace - time.Second)
	host.status.LastOutputAt = time.Now().UTC().Add(-shellWorkerSettleGrace - time.Second)
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

func TestShellModelWorkingStateLineShowsElapsedSecondsAndInterruptHint(t *testing.T) {
	host := &stubHost{
		worker:       "codex",
		canInterrupt: true,
		status:       HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_working"}, UIState{
		Session:             newSessionState(time.Now().UTC()),
		WorkerPromptPending: true,
		LastWorkerPromptAt:  time.Now().UTC().Add(-5 * time.Second),
	}, host)
	model.width = 100
	model.height = 24
	model.resize()

	rendered := model.renderFeedContent(100)
	if !strings.Contains(rendered, "Working (5s") {
		t.Fatalf("expected elapsed working indicator, got %q", rendered)
	}
	if !strings.Contains(rendered, "no worker activity 5s") {
		t.Fatalf("expected working indicator to explain that no worker activity has landed yet, got %q", rendered)
	}
	if !strings.Contains(rendered, "Esc to interrupt") {
		t.Fatalf("expected interrupt hint in working indicator, got %q", rendered)
	}
}

func TestShellModelWorkerSilentStateShowsLastActivityTruth(t *testing.T) {
	now := time.Now().UTC()
	host := &stubHost{
		worker: "codex",
		status: HostStatus{
			Mode:           HostModeCodexPTY,
			State:          HostStateLive,
			Label:          "codex live",
			InputLive:      true,
			LastActivityAt: now.Add(-25 * time.Second),
		},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_silent"}, UIState{
		Session:             newSessionState(now),
		WorkerPromptPending: true,
		LastWorkerPromptAt:  now.Add(-40 * time.Second),
	}, host)

	state := model.composerState()
	if state.Status != "worker silent" {
		t.Fatalf("expected worker-silent status, got %+v", state)
	}
	if !strings.Contains(state.Hint, "Last activity was 25s ago") {
		t.Fatalf("expected last-activity wording, got %q", state.Hint)
	}
}

func TestShellModelStalePendingStateClearsAfterNoWorkerSignalGrace(t *testing.T) {
	now := time.Now().UTC()
	host := &stubHost{
		canInput: false,
		worker:   "codex",
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateLive,
			Label:     "codex live",
			InputLive: true,
		},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_stale"}, UIState{
		Session:             newSessionState(now),
		WorkerPromptPending: true,
		LastWorkerPromptAt:  now.Add(-shellWorkerAwaitSignalGrace - 5*time.Second),
	}, host)
	model.width = 96
	model.height = 18
	model.resize()

	model.pollHost()
	if model.ui.WorkerPromptPending {
		t.Fatal("expected stale pending state to reconcile after prolonged silence")
	}
	if state := model.composerState(); state.SendMode != "blocked" && state.SendMode != "worker" {
		t.Fatalf("expected reconciled shell state, got %+v", state)
	}
	joined := model.renderFeedContent(96)
	if !strings.Contains(joined, "Cleared the live-worker running state") {
		t.Fatalf("expected conservative stale-state note in feed, got %q", joined)
	}
}

func TestShellModelAuthoritativeWorkerTurnStaysPendingThroughSilence(t *testing.T) {
	now := time.Now().UTC()
	host := &authoritativeStubHost{stubHost: &stubHost{
		canInput:   false,
		worker:     "codex",
		turnActive: true,
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateLive,
			Label:     "codex live",
			InputLive: true,
		},
	}}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_authoritative"}, UIState{
		Session:             newSessionState(now),
		WorkerPromptPending: true,
		LastWorkerPromptAt:  now.Add(-shellWorkerStaleNotice - 10*time.Second),
	}, host)

	model.pollHost()
	if !model.ui.WorkerPromptPending {
		t.Fatal("expected authoritative worker turn to remain pending until the host says it settled")
	}
}

func TestFormatShellElapsedAcrossRanges(t *testing.T) {
	cases := []struct {
		elapsed time.Duration
		want    string
	}{
		{elapsed: 5 * time.Second, want: "5s"},
		{elapsed: 59 * time.Second, want: "59s"},
		{elapsed: time.Minute + 2*time.Second, want: "1m 02s"},
		{elapsed: 3*time.Minute + 41*time.Second, want: "3m 41s"},
		{elapsed: time.Hour + 2*time.Minute + 7*time.Second, want: "1h 02m 07s"},
	}
	for _, tc := range cases {
		if got := formatShellElapsed(tc.elapsed); got != tc.want {
			t.Fatalf("elapsed %s: expected %q, got %q", tc.elapsed, tc.want, got)
		}
	}
}

func TestShellModelWorkingTickRefreshesTimerWithoutHostOutput(t *testing.T) {
	host := &stubHost{
		worker: "codex",
		status: HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_tick"}, UIState{
		Session:             newSessionState(time.Now().UTC()),
		WorkerPromptPending: true,
		LastWorkerPromptAt:  time.Now().UTC().Add(-5 * time.Second),
	}, host)
	model.width = 100
	model.height = 24
	model.resize()
	before := model.renderFeedContent(100)

	model.ui.LastWorkerPromptAt = time.Now().UTC().Add(-6 * time.Second)
	updated, _ := model.Update(shellWorkingTickMsg{})
	model = updated.(*shellModel)
	after := model.renderFeedContent(100)

	if before == after {
		t.Fatalf("expected working tick to update the rendered timer, before=%q after=%q", before, after)
	}
	if !strings.Contains(after, "Working (6s") {
		t.Fatalf("expected updated working timer after tick, got %q", after)
	}
}

func TestShellModelWorkingSpinnerTicksIndependentlyOfTranscriptArrival(t *testing.T) {
	host := &stubHost{
		worker: "codex",
		status: HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_spinner"}, UIState{
		Session:             newSessionState(time.Now().UTC()),
		WorkerPromptPending: true,
		LastWorkerPromptAt:  time.Now().UTC().Add(-5 * time.Second),
	}, host)
	model.width = 100
	model.height = 24
	model.resize()

	before := model.renderFeedContent(100)
	updated, cmd := model.Update(model.spinner.Tick())
	model = updated.(*shellModel)
	after := model.renderFeedContent(100)

	if cmd == nil {
		t.Fatal("expected spinner update to schedule the next animation tick")
	}
	if before == after {
		t.Fatalf("expected spinner frame to advance without transcript changes, before=%q after=%q", before, after)
	}
	if !strings.Contains(after, "Working (5s") {
		t.Fatalf("expected spinner tick to preserve the working row content, got %q", after)
	}
}

func TestShellModelWorkingSpinnerStopsWhenWorkSettles(t *testing.T) {
	host := &stubHost{
		canInput: true,
		worker:   "codex",
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
		lines:    []string{"Ran go test ./internal/tui/shell"},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_spinner_stop"}, UIState{
		Session:               newSessionState(time.Now().UTC()),
		WorkerPromptPending:   true,
		WorkerResponseStarted: true,
		LastWorkerPromptAt:    time.Now().UTC().Add(-5 * time.Second),
	}, host)
	model.width = 100
	model.height = 24
	model.resize()

	model.ui.LastWorkerPromptAt = time.Now().UTC().Add(-shellWorkerSettleGrace - time.Second)
	host.status.LastOutputAt = time.Now().UTC().Add(-shellWorkerSettleGrace - time.Second)
	model.pollHost()
	updated, cmd := model.Update(model.spinner.Tick())
	model = updated.(*shellModel)

	if cmd != nil {
		t.Fatal("expected spinner loop to stop once the worker turn settles")
	}
	if strings.Contains(model.renderFeedContent(100), "Working (") {
		t.Fatalf("expected no active working row after the worker settles, got %q", model.renderFeedContent(100))
	}
}

func TestShellModelWorkingStateTracksLiveOutputAndClearsWhenSettled(t *testing.T) {
	host := &stubHost{
		canInput: true,
		worker:   "codex",
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_working"}, UIState{
		Session:             newSessionState(time.Now().UTC()),
		WorkerPromptPending: true,
		LastWorkerPromptAt:  time.Now().UTC().Add(-3 * time.Second),
	}, host)
	model.width = 100
	model.height = 24
	model.resize()

	if !strings.Contains(model.renderFeedContent(100), "Working (3s") {
		t.Fatalf("expected pending worker indicator before output, got %q", model.renderFeedContent(100))
	}

	host.lines = []string{"Explored README.md", "  Read README.md", "Answer ready."}
	host.status.LastOutputAt = time.Now().UTC()
	model.pollHost()

	rendered := model.renderFeedContent(100)
	if !strings.Contains(rendered, "• Explored README.md") || !strings.Contains(rendered, "└ Read README.md") {
		t.Fatalf("expected committed worker output to be grouped cleanly, got %q", rendered)
	}
	if !strings.Contains(rendered, "Working (") {
		t.Fatalf("expected working indicator to remain while live output is still active, got %q", rendered)
	}

	model.ui.LastWorkerPromptAt = time.Now().UTC().Add(-shellWorkerSettleGrace - time.Second)
	host.status.LastOutputAt = time.Now().UTC().Add(-shellWorkerSettleGrace - time.Second)
	model.pollHost()

	rendered = model.renderFeedContent(100)
	if strings.Contains(rendered, "Working (") {
		t.Fatalf("expected working indicator to clear after the worker stream settles, got %q", rendered)
	}
}

func TestShellModelWorkingRowStaysBelowNewestVisibleWorkerContent(t *testing.T) {
	host := &stubHost{
		worker: "codex",
		status: HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
		lines: []string{
			"Explored internal/tui/shell/bubble_shell.go",
			"  Read internal/tui/shell/bubble_shell.go:1409",
			"Ran go test ./internal/tui/shell",
		},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_position"}, UIState{
		Session:               newSessionState(time.Now().UTC()),
		WorkerPromptPending:   true,
		WorkerResponseStarted: true,
		LastWorkerPromptAt:    time.Now().UTC().Add(-8 * time.Second),
	}, host)
	model.width = 110
	model.height = 28
	model.resize()

	rendered := model.renderFeedContent(110)
	actionIdx := strings.Index(rendered, "• Explored internal/tui/shell/bubble_shell.go")
	workingIdx := strings.Index(rendered, "Working (")
	if actionIdx == -1 || workingIdx == -1 {
		t.Fatalf("expected worker output and working row in feed, got %q", rendered)
	}
	if workingIdx <= actionIdx {
		t.Fatalf("expected working row below the newest worker content, got %q", rendered)
	}
}

func TestShellModelEscInterruptsRunningWorkerWhenSupported(t *testing.T) {
	host := &stubHost{
		canInterrupt: true,
		worker:       "codex",
		status:       HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_interrupt"}, UIState{
		Session:             newSessionState(time.Now().UTC()),
		WorkerPromptPending: true,
		LastWorkerPromptAt:  time.Now().UTC().Add(-4 * time.Second),
	}, host)
	model.width = 100
	model.height = 24
	model.resize()

	updated, _ := model.updateKey(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(*shellModel)

	if host.interrupts != 1 {
		t.Fatalf("expected Esc to interrupt the live worker, got %d interrupts", host.interrupts)
	}
	if !model.ui.WorkerInterruptRequested {
		t.Fatal("expected shell state to record that interrupt was requested")
	}
	rendered := model.renderFeedContent(100)
	if !strings.Contains(rendered, "interrupt sent") {
		t.Fatalf("expected conservative interrupt note in transcript, got %q", rendered)
	}
}

func TestShellModelEscDoesNotClaimInterruptWhenUnsupported(t *testing.T) {
	host := &stubHost{
		worker: "codex",
		status: HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_no_interrupt"}, UIState{
		Session:             newSessionState(time.Now().UTC()),
		WorkerPromptPending: true,
		LastWorkerPromptAt:  time.Now().UTC().Add(-4 * time.Second),
	}, host)
	model.width = 100
	model.height = 24
	model.resize()

	updated, _ := model.updateKey(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(*shellModel)

	if host.interrupts != 0 {
		t.Fatalf("expected no interrupt call in unsupported state, got %d", host.interrupts)
	}
	if model.ui.WorkerInterruptRequested {
		t.Fatal("expected no interrupt-request state when the host cannot interrupt")
	}
	if strings.Contains(model.renderFeedContent(100), "interrupt sent") {
		t.Fatalf("expected no interrupt note in transcript, got %q", model.renderFeedContent(100))
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
	if !strings.Contains(model.renderHelpView(90), "Enter") || !strings.Contains(model.renderHelpView(90), "send") {
		t.Fatalf("expected canonical help rail to advertise real send behavior, got %q", model.renderHelpView(90))
	}
}

func TestShellModelComposerUsesFocusedTextInputPlaceholderAndTypedValue(t *testing.T) {
	host := &stubHost{
		canInput: true,
		worker:   "codex",
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_input"}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 100
	model.height = 24
	model.resize()

	if !model.composer.Focused() {
		t.Fatal("expected live worker composer to stay focused")
	}
	empty := model.renderComposer(newShellStyles(), model.layout())
	if !strings.Contains(empty, "Ask the worker to inspect, explain, or change something") {
		t.Fatalf("expected placeholder in empty composer, got %q", empty)
	}

	updated, _ := model.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	model = updated.(*shellModel)
	typed := model.renderComposer(newShellStyles(), model.layout())
	if strings.Contains(typed, "Ask the worker to inspect, explain, or change something") {
		t.Fatalf("expected placeholder to disappear after typing, got %q", typed)
	}
	if strings.Count(typed, "› z") != 1 {
		t.Fatalf("expected typed content to render once without ghosting, got %q", typed)
	}
}

func TestShellModelComposerKeepsTypedInputStableAcrossResize(t *testing.T) {
	host := &stubHost{
		canInput: true,
		worker:   "codex",
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_input_resize"}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 100
	model.height = 24
	model.resize()
	model.composer.SetValue("inspect")

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 86, Height: 22})
	model = updated.(*shellModel)
	rendered := model.renderComposer(newShellStyles(), model.layout())
	if strings.Count(rendered, "inspect") != 1 {
		t.Fatalf("expected typed input to survive resize without duplication, got %q", rendered)
	}
}

func TestShellModelHelpRailRendersCompactBindingsAtBottom(t *testing.T) {
	host := &stubHost{
		canInput: true,
		worker:   "codex",
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_help"}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 100
	model.height = 24
	model.resize()

	helpView := model.renderHelpView(100)
	if !strings.Contains(helpView, "/") || !strings.Contains(helpView, "commands") {
		t.Fatalf("expected compact help to expose the command palette binding, got %q", helpView)
	}
	if !strings.Contains(helpView, "PgUp/PgDn") {
		t.Fatalf("expected compact help to expose history scrolling, got %q", helpView)
	}
}

func TestShellModelHelpRailUpdatesWidthAndToggleState(t *testing.T) {
	host := &stubHost{
		canInput: true,
		worker:   "codex",
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_help_width"}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 90
	model.height = 24
	model.resize()

	wide := model.renderHelpView(90)
	narrow := model.renderHelpView(24)
	if wide == narrow {
		t.Fatalf("expected help rail to adapt to width changes, wide=%q narrow=%q", wide, narrow)
	}

	updated, _ := model.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	model = updated.(*shellModel)
	if !model.help.ShowAll {
		t.Fatal("expected ? to expand full help when the composer is empty")
	}
	full := model.renderHelpView(90)
	if !strings.Contains(full, "Ctrl-C") || !strings.Contains(full, "exit") {
		t.Fatalf("expected expanded help to include quit guidance, got %q", full)
	}
}

func TestShellModelHelpRailReflectsRealEscBindingByState(t *testing.T) {
	running := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_help_state"}, UIState{
		Session:             newSessionState(time.Now().UTC()),
		WorkerPromptPending: true,
		LastWorkerPromptAt:  time.Now().UTC().Add(-2 * time.Second),
	}, &stubHost{
		canInterrupt: true,
		worker:       "codex",
		status:       HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	})
	running.width = 100
	running.height = 24
	running.resize()
	if !strings.Contains(strings.ToLower(running.renderHelpView(100)), "interrupt") {
		t.Fatalf("expected running help rail to advertise Esc interrupt, got %q", running.renderHelpView(100))
	}

	idle := newShellModel(context.Background(), &App{}, Snapshot{TaskID: "tsk_help_overlay"}, UIState{Session: newSessionState(time.Now().UTC())}, &stubHost{
		canInput: true,
		worker:   "codex",
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	})
	idle.width = 100
	idle.height = 24
	idle.resize()
	idle.setOverlayKind(shellOverlayCommands, false)
	if !strings.Contains(strings.ToLower(idle.renderHelpView(100)), "dismiss") {
		t.Fatalf("expected overlay help rail to advertise Esc dismiss, got %q", idle.renderHelpView(100))
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

func TestShellModelPaletteRendersBelowComposerArea(t *testing.T) {
	host := &stubHost{
		canInput: true,
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 100
	model.height = 26
	model.resize()
	model.composer.SetValue("/")
	model.syncOverlayFromComposer()

	lines := splitLines(model.View())
	composerIdx := findLineContaining(lines, "Command filter")
	overlayIdx := findLineContaining(lines, "Commands")
	footerIdx := findLineContaining(lines, "session ")

	if composerIdx == -1 || overlayIdx == -1 || footerIdx == -1 {
		t.Fatalf("expected composer, overlay, and footer in rendered shell, got %q", model.View())
	}
	if overlayIdx <= composerIdx {
		t.Fatalf("expected overlay below the composer area, got composer=%d overlay=%d", composerIdx, overlayIdx)
	}
	if overlayIdx >= footerIdx {
		t.Fatalf("expected overlay to stay above the footer, got overlay=%d footer=%d", overlayIdx, footerIdx)
	}
}

func TestShellModelPaletteCounterUsesSelectedCommandIndex(t *testing.T) {
	host := &stubHost{
		status: HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 120
	model.height = 32
	model.resize()
	model.composer.SetValue("/")
	model.syncOverlayFromComposer()

	model.menuSelected = 0
	rendered := model.renderOverlay(newShellStyles(), model.layout())
	if !strings.Contains(rendered, "1 of 11") {
		t.Fatalf("expected /next to show 1 of 11, got %q", rendered)
	}

	model.menuSelected = 1
	rendered = model.renderOverlay(newShellStyles(), model.layout())
	if !strings.Contains(rendered, "2 of 11") {
		t.Fatalf("expected /run to show 2 of 11, got %q", rendered)
	}

	model.menuSelected = len(shellCommands) - 1
	rendered = model.renderOverlay(newShellStyles(), model.layout())
	if !strings.Contains(rendered, "11 of 11") {
		t.Fatalf("expected /clear to show 11 of 11, got %q", rendered)
	}
}

func TestShellModelPaletteCounterUsesFilteredSelectionIndex(t *testing.T) {
	host := &stubHost{
		status: HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 120
	model.height = 32
	model.resize()
	model.composer.SetValue("/r")
	model.syncOverlayFromComposer()

	items := model.filteredOverlayItems()
	if len(items) < 2 {
		t.Fatalf("expected multiple filtered commands, got %+v", items)
	}
	model.menuSelected = 1
	rendered := model.renderOverlay(newShellStyles(), model.layout())
	if !strings.Contains(rendered, fmt.Sprintf("2 of %d", len(items))) {
		t.Fatalf("expected filtered selected index in overlay counter, got %q", rendered)
	}
}

func TestRenderFeedEntryShapesWorkerActionLines(t *testing.T) {
	rendered := renderFeedEntry(shellFeedEntry{
		Kind:         shellFeedWorker,
		Title:        "Worker",
		Body:         []string{"Explored repository", "  Read README.md", "Ran go test ./..."},
		Preformatted: true,
	}, 100)

	if !strings.Contains(rendered, "• Explored repository") {
		t.Fatalf("expected action line to be grouped with bullet, got %q", rendered)
	}
	if !strings.Contains(rendered, "└ Read README.md") {
		t.Fatalf("expected indented detail line to be grouped under action, got %q", rendered)
	}
	if !strings.Contains(rendered, "• Ran go test ./...") {
		t.Fatalf("expected second action line to render as compact bullet, got %q", rendered)
	}
}

func TestRenderFeedEntryWrapsLongStructuredWorkerContentWithinWidth(t *testing.T) {
	rendered := renderFeedEntry(shellFeedEntry{
		Kind:  shellFeedWorker,
		Title: "Worker",
		Body: []string{
			"Explored internal/tui/shell/bubble_shell.go to trace width authority through the shell.",
			"  Read internal/tui/shell/bubble_shell.go:780 to verify resize and viewport clamping.",
			"Plan:",
			"- keep root width authoritative",
			"- preserve wrapped paragraph spacing under resize",
		},
		Preformatted: true,
	}, 52)

	for _, line := range splitLines(rendered) {
		if lipgloss.Width(line) > 52 {
			t.Fatalf("expected structured worker rendering to fit width 52, got %d for %q", lipgloss.Width(line), line)
		}
	}
	if !strings.Contains(rendered, "• Explored internal/tui/shell/bubble_shell.go") {
		t.Fatalf("expected action block to remain structured after wrapping, got %q", rendered)
	}
	if !strings.Contains(rendered, "└ Read internal/tui/shell/bubble_shell.go:780") {
		t.Fatalf("expected detail line to remain grouped after wrapping, got %q", rendered)
	}
	if !strings.Contains(rendered, "Plan") || !strings.Contains(rendered, "• keep root width authoritative") {
		t.Fatalf("expected header and bullet list readability, got %q", rendered)
	}
}

func TestStyleFileReferencesDetectsObviousPathsConservatively(t *testing.T) {
	if !looksLikeFileReference("README.md") {
		t.Fatal("expected README.md to be recognized as a file reference")
	}
	if !looksLikeFileReference("internal/tui/shell/bubble_shell.go:1409") {
		t.Fatal("expected repo-relative line reference to be recognized")
	}
	if looksLikeFileReference("Summarize") {
		t.Fatal("expected plain prose token to remain unstyled")
	}
}

func TestShellModelBackspaceFromSlashClosesPaletteCleanly(t *testing.T) {
	host := &stubHost{
		canInput: true,
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 100
	model.height = 24
	model.resize()

	updated, _ := model.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	model = updated.(*shellModel)
	if model.overlayKind != shellOverlayCommands {
		t.Fatalf("expected slash to open commands, got %v", model.overlayKind)
	}
	if !strings.Contains(model.View(), "Commands") {
		t.Fatalf("expected command palette to render after slash, got %q", model.View())
	}

	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyBackspace})
	model = updated.(*shellModel)
	if model.overlayKind != shellOverlayNone {
		t.Fatalf("expected deleting slash to close the palette, got %v", model.overlayKind)
	}
	if model.composer.Value() != "" {
		t.Fatalf("expected deleting slash to clear the filter input, got %q", model.composer.Value())
	}
	rendered := model.View()
	if strings.Contains(rendered, "Commands") || strings.Contains(rendered, "ACTIONS") {
		t.Fatalf("expected no stale palette content after dismiss, got %q", rendered)
	}
	if !strings.Contains(rendered, "Live worker") {
		t.Fatalf("expected composer to return to normal live-worker state, got %q", rendered)
	}
}

func TestShellModelCommandFilterBackspaceUpdatesPaletteStepByStep(t *testing.T) {
	host := &stubHost{
		canInput: true,
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live", InputLive: true},
	}
	model := newShellModel(context.Background(), &App{}, Snapshot{}, UIState{Session: newSessionState(time.Now().UTC())}, host)
	model.width = 100
	model.height = 24
	model.resize()

	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'/'}},
		{Type: tea.KeyRunes, Runes: []rune{'r'}},
	} {
		updated, _ := model.updateKey(msg)
		model = updated.(*shellModel)
	}
	if model.overlayKind != shellOverlayCommands || model.composer.Value() != "/r" {
		t.Fatalf("expected command filter state after typing /r, overlay=%v value=%q", model.overlayKind, model.composer.Value())
	}

	updated, _ := model.updateKey(tea.KeyMsg{Type: tea.KeyBackspace})
	model = updated.(*shellModel)
	if model.overlayKind != shellOverlayCommands || model.composer.Value() != "/" {
		t.Fatalf("expected backspace to keep command mode active at /, overlay=%v value=%q", model.overlayKind, model.composer.Value())
	}
	if !strings.Contains(model.View(), "Commands") {
		t.Fatalf("expected palette to remain visible while filter is still /, got %q", model.View())
	}

	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyBackspace})
	model = updated.(*shellModel)
	if model.overlayKind != shellOverlayNone || model.composer.Value() != "" {
		t.Fatalf("expected deleting the final slash to exit command mode, overlay=%v value=%q", model.overlayKind, model.composer.Value())
	}
	if strings.Contains(model.View(), "Commands") {
		t.Fatalf("expected palette to disappear after command mode exits, got %q", model.View())
	}

	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	model = updated.(*shellModel)
	if model.overlayKind != shellOverlayNone || model.composer.Value() != "h" {
		t.Fatalf("expected normal typing after dismiss, overlay=%v value=%q", model.overlayKind, model.composer.Value())
	}

	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyBackspace})
	model = updated.(*shellModel)
	if model.composer.Value() != "" {
		t.Fatalf("expected normal text to backspace cleanly before reopening commands, got %q", model.composer.Value())
	}

	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	model = updated.(*shellModel)
	if model.overlayKind != shellOverlayCommands {
		t.Fatalf("expected command mode to reopen cleanly, got %v", model.overlayKind)
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

	topLine := firstNonEmptyViewportLine(model.viewport.View())
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

func TestShellModelBackToBackCommandsKeepLatestCommandVisibleAtBottom(t *testing.T) {
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

	viewport := model.viewport.View()
	if !model.viewport.AtBottom() {
		t.Fatalf("expected explicit slash commands to keep the latest command output visible, offset=%d", model.viewport.YOffset)
	}
	if !strings.Contains(viewport, "SESSIONS") {
		t.Fatalf("expected latest command block to stay visible near the tail, got %q", viewport)
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
	model.ui.LastWorkerPromptAt = time.Now().UTC().Add(-shellWorkerSettleGrace - time.Second)
	host.status.LastOutputAt = time.Now().UTC().Add(-shellWorkerSettleGrace - time.Second)
	model.pollHost()
	if model.ui.WorkerPromptPending {
		t.Fatal("expected worker prompt pending to clear once the host is writable and the response has settled")
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

func assertRenderedViewFitsWindow(t *testing.T, view string, width int, height int) {
	t.Helper()
	lines := splitLines(view)
	if len(lines) > height {
		t.Fatalf("expected rendered view to fit height %d, got %d lines: %q", height, len(lines), view)
	}
	for _, line := range lines {
		if lipgloss.Width(line) > width {
			t.Fatalf("expected rendered line to fit width %d, got %d for %q", width, lipgloss.Width(line), line)
		}
	}
}

func findLineContaining(lines []string, needle string) int {
	for i, line := range lines {
		if strings.Contains(line, needle) {
			return i
		}
	}
	return -1
}

func findLastLineContaining(lines []string, needle string) int {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], needle) {
			return i
		}
	}
	return -1
}
