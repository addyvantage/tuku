package shell

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	shellHostTickInterval       = 350 * time.Millisecond
	shellWorkingTickInterval    = 1 * time.Second
	shellSnapshotTickInterval   = 15 * time.Second
	shellRegistryTickInterval   = shellSessionHeartbeatInterval
	shellTranscriptTickInterval = shellTranscriptFlushInterval
	shellExitConfirmWindow      = 2 * time.Second
	shellOverlayVisibleItems    = 6
	shellTailViewportBlocks     = 2
)

type shellOverlayKind int

const (
	shellOverlayNone shellOverlayKind = iota
	shellOverlayCommands
	shellOverlayModel
)

type shellFeedKind int

const (
	shellFeedIntro shellFeedKind = iota
	shellFeedUser
	shellFeedWorker
	shellFeedTuku
	shellFeedWarning
	shellFeedError
	shellFeedWorking
)

type shellFeedEntry struct {
	Key          string
	Kind         shellFeedKind
	Title        string
	Body         []string
	Preformatted bool
}

type shellCommand struct {
	Name        string
	Description string
	Group       string
}

var shellCommands = []shellCommand{
	{Name: "/next", Description: "Run the current primary control-plane step", Group: "Actions"},
	{Name: "/run", Description: "Run guidance or the current local-run action", Group: "Actions"},
	{Name: "/continue", Description: "Continue-recovery guidance when relevant", Group: "Actions"},
	{Name: "/checkpoint", Description: "Checkpoint guidance and resumability context", Group: "Actions"},
	{Name: "/handoff", Description: "Handoff and launch continuity guidance", Group: "Actions"},
	{Name: "/status", Description: "Compact task, worker, and continuity summary", Group: "Context"},
	{Name: "/inspect", Description: "Deeper operator and continuity detail", Group: "Context"},
	{Name: "/sessions", Description: "Durable shell sessions and transcript posture", Group: "Context"},
	{Name: "/model", Description: "Preview runtime model options without switching workers", Group: "Context"},
	{Name: "/help", Description: "Keyboard guidance and shell conventions", Group: "Shell"},
	{Name: "/clear", Description: "Clear local shell notes and command output", Group: "Shell"},
}

var shellModelOptions = []shellCommand{
	{Name: "Worker Default", Description: "Keep the active worker and runtime unchanged", Group: "Current"},
	{Name: "GPT-5.4", Description: "Preview a premium Codex runtime profile", Group: "Preview"},
	{Name: "GPT-5.4 Mini", Description: "Preview a faster lower-latency Codex profile", Group: "Preview"},
	{Name: "Claude", Description: "Preview a Claude-oriented runtime profile", Group: "Preview"},
}

type shellHostTickMsg struct{}
type shellWorkingTickMsg struct{}
type shellSnapshotTickMsg struct{}
type shellRegistryTickMsg struct{}
type shellTranscriptTickMsg struct{}
type shellExitConfirmTimeoutMsg struct {
	nonce int
}

type shellSnapshotLoadedMsg struct {
	snapshot Snapshot
	sessions []KnownShellSession
	err      error
}

type shellMessageSentMsg struct {
	prompt   string
	snapshot Snapshot
	sessions []KnownShellSession
	err      error
}

type shellPrimaryActionDoneMsg struct {
	result primaryActionExecutionResult
}

type shellSurfaceLayout struct {
	padding        int
	headerHeight   int
	footerHeight   int
	composerHeight int
	overlayHeight  int
	viewportHeight int
	contentWidth   int
	composerWidth  int
	feedWidth      int
}

type shellComposerState struct {
	Label       string
	Status      string
	Hint        string
	Placeholder string
	Tone        string
	SendMode    string
}

type shellKeyMap struct {
	Send      key.Binding
	Commands  key.Binding
	Scroll    key.Binding
	Dismiss   key.Binding
	Interrupt key.Binding
	Help      key.Binding
	Quit      key.Binding
	Move      key.Binding
	Select    key.Binding
}

func newShellKeyMap() shellKeyMap {
	return shellKeyMap{
		Send: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("Enter", "send"),
		),
		Commands: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "commands"),
		),
		Scroll: key.NewBinding(
			key.WithKeys("pgup", "pgdown", "ctrl+u", "ctrl+d"),
			key.WithHelp("PgUp/PgDn", "history"),
		),
		Dismiss: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("Esc", "dismiss"),
		),
		Interrupt: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("Esc", "interrupt"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("Ctrl-C", "exit"),
		),
		Move: key.NewBinding(
			key.WithKeys("up", "down", "tab", "shift+tab"),
			key.WithHelp("↑/↓", "move"),
		),
		Select: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("Enter", "select"),
		),
	}
}

func (k shellKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Send, k.Commands, k.Scroll, k.Dismiss, k.Interrupt, k.Help, k.Quit}
}

func (k shellKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Send, k.Select, k.Commands},
		{k.Scroll, k.Move},
		{k.Dismiss, k.Interrupt, k.Help, k.Quit},
	}
}

type shellStyles struct {
	root            lipgloss.Style
	headerKicker    lipgloss.Style
	headerTitle     lipgloss.Style
	headerMeta      lipgloss.Style
	headerRule      lipgloss.Style
	chip            lipgloss.Style
	chipAccent      lipgloss.Style
	chipPositive    lipgloss.Style
	chipCaution     lipgloss.Style
	chipMuted       lipgloss.Style
	feedTitle       lipgloss.Style
	feedBody        lipgloss.Style
	feedUserTitle   lipgloss.Style
	feedUserStrip   lipgloss.Style
	feedWorkerTitle lipgloss.Style
	feedWorkerMeta  lipgloss.Style
	feedActionVerb  lipgloss.Style
	feedPath        lipgloss.Style
	feedNoteTitle   lipgloss.Style
	feedWarnTitle   lipgloss.Style
	feedErrorTitle  lipgloss.Style
	composerBox     lipgloss.Style
	composerFocus   lipgloss.Style
	composerLabel   lipgloss.Style
	composerHint    lipgloss.Style
	composerPrompt  lipgloss.Style
	footer          lipgloss.Style
	footerMuted     lipgloss.Style
	menuBox         lipgloss.Style
	menuTitle       lipgloss.Style
	menuSection     lipgloss.Style
	menuItem        lipgloss.Style
	menuSelected    lipgloss.Style
	menuSelectedKey lipgloss.Style
	menuDesc        lipgloss.Style
}

type shellModel struct {
	ctx context.Context
	app *App

	width  int
	height int

	snapshot Snapshot
	ui       UIState
	host     WorkerHost

	viewport viewport.Model
	composer textinput.Model
	spinner  spinner.Model
	help     help.Model
	keys     shellKeyMap

	overlayKind                 shellOverlayKind
	menuSelected                int
	localEntries                []shellFeedEntry
	lastContent                 string
	lastHost                    HostStatus
	lastDigest                  uint64
	didInitialFit               bool
	followLatest                bool
	spinnerRunning              bool
	scrollToEntry               string
	exitConfirmUntil            time.Time
	exitConfirmNonce            int
	archivedHostLines           []string
	startupConversationBaseline int
	lastViewportWidth           int
	lastViewportHeight          int
}

func newShellModel(ctx context.Context, app *App, snapshot Snapshot, ui UIState, host WorkerHost) *shellModel {
	composer := textinput.New()
	composer.Prompt = ""
	composer.CharLimit = 4000
	composer.PromptStyle = lipgloss.NewStyle()
	composer.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F3F4F6"))
	composer.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	composer.Focus()
	composer.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#F9FAFB"))

	spin := spinner.New()
	spin.Spinner = spinner.Spinner{
		Frames: spinner.Dot.Frames,
		FPS:    200 * time.Millisecond,
	}
	spin.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#F9FAFB"))

	helpModel := help.New()
	helpModel.Styles.ShortKey = lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB"))
	helpModel.Styles.ShortDesc = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	helpModel.Styles.ShortSeparator = lipgloss.NewStyle().Foreground(lipgloss.Color("#4B5563"))
	helpModel.Styles.Ellipsis = lipgloss.NewStyle().Foreground(lipgloss.Color("#4B5563"))
	helpModel.Styles.FullKey = lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB"))
	helpModel.Styles.FullDesc = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	helpModel.Styles.FullSeparator = lipgloss.NewStyle().Foreground(lipgloss.Color("#4B5563"))

	vp := viewport.New(80, 20)
	vp.MouseWheelEnabled = true

	return &shellModel{
		ctx:                         ctx,
		app:                         app,
		snapshot:                    snapshot,
		ui:                          ui,
		host:                        host,
		viewport:                    vp,
		composer:                    composer,
		spinner:                     spin,
		help:                        helpModel,
		keys:                        newShellKeyMap(),
		lastHost:                    host.Status(),
		followLatest:                true,
		startupConversationBaseline: len(visibleConversationItems(snapshot)),
	}
}

func (m *shellModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		textinput.Blink,
		shellHostTickCmd(),
		shellWorkingTickCmd(),
		shellSnapshotTickCmd(m.app.refreshEvery()),
		shellRegistryTickCmd(),
		shellTranscriptTickCmd(),
	}
	if cmd := m.ensureSpinnerTick(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

func (m *shellModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		return m, nil
	case tea.KeyMsg:
		return m.updateKey(msg)
	case shellHostTickMsg:
		cmd := shellHostTickCmd()
		m.pollHost()
		return m, cmd
	case spinner.TickMsg:
		if !m.spinnerActive() {
			m.spinnerRunning = false
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		m.spinnerRunning = true
		if m.shouldRefreshTransientViewport() {
			m.syncViewport(false)
		}
		return m, cmd
	case shellWorkingTickMsg:
		cmds := []tea.Cmd{shellWorkingTickCmd()}
		if cmd := m.ensureSpinnerTick(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if (m.ui.WorkerPromptPending || m.ui.PrimaryActionInFlight != nil) && m.shouldRefreshTransientViewport() {
			m.syncViewport(false)
		}
		return m, tea.Batch(cmds...)
	case shellSnapshotTickMsg:
		cmds := []tea.Cmd{shellSnapshotTickCmd(m.app.refreshEvery())}
		if !(m.host.Status().State == HostStateLive && m.host.Status().InputLive && !m.ui.WorkerPromptPending) {
			cmds = append(cmds, shellLoadSnapshotCmd(m.app.Source, m.app.TaskID, m.app.RegistrySource))
		}
		return m, tea.Batch(cmds...)
	case shellRegistryTickMsg:
		reportShellSession(m.app.RegistrySink, m.app.TaskID, &m.ui.Session, m.host.Status(), true, &m.ui)
		return m, shellRegistryTickCmd()
	case shellTranscriptTickMsg:
		flushTranscriptEvidence(m.app.TaskID, m.ui.Session.SessionID, m.host, m.app.TranscriptSink, &m.ui)
		return m, shellTranscriptTickCmd()
	case shellExitConfirmTimeoutMsg:
		if msg.nonce == m.exitConfirmNonce && !m.exitConfirmUntil.IsZero() && time.Now().UTC().After(m.exitConfirmUntil) {
			m.exitConfirmUntil = time.Time{}
		}
		return m, nil
	case shellSnapshotLoadedMsg:
		if msg.err != nil {
			m.pushError("Refresh failed", msg.err.Error())
			m.ui.LastError = msg.err.Error()
		} else {
			m.snapshot = msg.snapshot
			m.host.UpdateSnapshot(msg.snapshot)
			m.ui.LastRefresh = time.Now().UTC()
			m.ui.Session.KnownSessions = msg.sessions
			m.ui.LastError = ""
		}
		m.syncViewport(false)
		return m, nil
	case shellMessageSentMsg:
		if msg.err != nil {
			m.pushError("Send failed", msg.err.Error())
			m.ui.LastError = msg.err.Error()
			m.syncViewport(true)
			return m, nil
		}
		m.removeLocalUserPrompt(msg.prompt)
		m.snapshot = msg.snapshot
		m.host.UpdateSnapshot(msg.snapshot)
		m.ui.Session.KnownSessions = msg.sessions
		m.ui.LastRefresh = time.Now().UTC()
		m.ui.LastError = ""
		m.pushNote("Tuku", []string{
			"Prompt sent through Tuku canonical continuity.",
			latestCanonicalLine(m.snapshot),
		}, false)
		m.syncViewport(true)
		return m, nil
	case shellPrimaryActionDoneMsg:
		if err := completePrimaryOperatorStepExecution(m.app.Source, m.app.TaskID, m.host, m.app.RegistrySource, &m.snapshot, &m.ui, msg.result); err != nil {
			m.pushError("Primary step failed", err.Error())
			m.ui.LastError = err.Error()
		} else {
			m.ui.LastError = ""
			if result := m.ui.LastPrimaryActionResult; result != nil {
				lines := []string{"result " + result.Summary}
				for _, delta := range result.Deltas {
					lines = append(lines, "delta "+delta)
				}
				if next := strings.TrimSpace(result.NextStep); next != "" && next != "none" {
					lines = append(lines, "next "+next)
				}
				m.pushNote("Tuku", lines, false)
			}
		}
		m.syncViewport(true)
		return m, nil
	}

	return m, nil
}

func (m *shellModel) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		now := time.Now().UTC()
		if !m.exitConfirmUntil.IsZero() && now.Before(m.exitConfirmUntil) {
			return m, tea.Quit
		}
		m.exitConfirmNonce++
		m.exitConfirmUntil = now.Add(shellExitConfirmWindow)
		return m, tea.Tick(shellExitConfirmWindow, func(time.Time) tea.Msg {
			return shellExitConfirmTimeoutMsg{nonce: m.exitConfirmNonce}
		})
	case "q":
		if m.overlayKind == shellOverlayNone && strings.TrimSpace(m.composer.Value()) == "" {
			return m, tea.Quit
		}
	case "?":
		if strings.TrimSpace(m.composer.Value()) == "" {
			m.help.ShowAll = !m.help.ShowAll
			m.reflowLayout(false)
			return m, nil
		}
	case "esc":
		if m.overlayKind != shellOverlayNone {
			m.setOverlayKind(shellOverlayNone, false)
			return m, nil
		}
		if m.ui.WorkerPromptPending && m.requestWorkerInterrupt() {
			m.syncViewport(true)
			return m, nil
		}
		return m, nil
	case "pgdown":
		m.viewport.ViewDown()
		m.syncFollowLatest()
		return m, nil
	case "pgup":
		m.viewport.ViewUp()
		m.followLatest = m.viewport.AtBottom()
		return m, nil
	case "ctrl+d":
		m.viewport.HalfViewDown()
		m.syncFollowLatest()
		return m, nil
	case "ctrl+u":
		m.viewport.HalfViewUp()
		m.followLatest = m.viewport.AtBottom()
		return m, nil
	}

	if m.overlayKind != shellOverlayNone {
		return m.updateOverlayKey(msg)
	}

	if msg.String() == "enter" {
		return m.submitComposer()
	}

	var cmd tea.Cmd
	m.composer, cmd = m.composer.Update(msg)
	m.syncOverlayFromComposer()
	return m, cmd
}

func (m *shellModel) updateOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.filteredOverlayItems()
	switch msg.String() {
	case "up", "shift+tab":
		if len(items) > 0 {
			m.menuSelected--
			if m.menuSelected < 0 {
				m.menuSelected = len(items) - 1
			}
		}
		return m, nil
	case "down", "tab":
		if len(items) > 0 {
			m.menuSelected = (m.menuSelected + 1) % len(items)
		}
		return m, nil
	case "enter":
		if len(items) == 0 {
			return m, nil
		}
		if m.overlayKind == shellOverlayCommands {
			return m.executeSlashCommand(items[m.menuSelected].Name)
		}
		return m.selectModelOption(items[m.menuSelected].Name)
	default:
		if m.overlayKind == shellOverlayCommands {
			var cmd tea.Cmd
			m.composer, cmd = m.composer.Update(msg)
			m.syncOverlayFromComposer()
			return m, cmd
		}
	}
	return m, nil
}

func (m *shellModel) submitComposer() (tea.Model, tea.Cmd) {
	raw := strings.TrimSpace(m.composer.Value())
	if raw == "" {
		return m, nil
	}
	if strings.HasPrefix(raw, "/") {
		if m.overlayKind == shellOverlayNone {
			m.setOverlayKind(shellOverlayCommands, false)
		}
		items := m.filteredOverlayItems()
		if len(items) == 0 {
			return m, nil
		}
		return m.executeSlashCommand(items[m.menuSelected].Name)
	}

	prompt := raw
	state := m.composerState()
	switch state.SendMode {
	case "blocked":
		m.pushWarning("Input paused", []string{state.Hint})
		m.syncViewport(true)
		return m, nil
	case "worker":
		m.composer.SetValue("")
		m.setOverlayKind(shellOverlayNone, false)
		if m.host.WriteInput([]byte(prompt + "\n")) {
			m.ui.LastWorkerPrompt = prompt
			m.ui.LastWorkerPromptAt = time.Now().UTC()
			m.ui.WorkerPromptPending = true
			m.ui.WorkerResponseStarted = false
			m.ui.WorkerInterruptRequested = false
			m.pushUserPrompt(prompt)
			m.syncViewport(true)
			return m, nil
		}
		m.pushError("Input unavailable", unavailableInputMessage(m.host.Status()))
		m.syncViewport(true)
		return m, nil
	case "canonical":
		m.composer.SetValue("")
		m.setOverlayKind(shellOverlayNone, false)
		m.pushUserPrompt(prompt)
		m.syncViewport(true)
		return m, shellSendPromptCmd(m.app.MessageSender, m.app.Source, m.app.TaskID, prompt, m.app.RegistrySource)
	case "scratch":
		m.composer.SetValue("")
		m.setOverlayKind(shellOverlayNone, false)
		if m.host.WriteInput([]byte(prompt + "\n")) {
			m.pushUserPrompt(prompt)
			m.syncViewport(true)
			return m, nil
		}
		m.pushError("Input unavailable", unavailableInputMessage(m.host.Status()))
		m.syncViewport(true)
		return m, nil
	}

	m.pushError("Input unavailable", unavailableInputMessage(m.host.Status()))
	m.syncViewport(true)
	return m, nil
}

func (m *shellModel) executeSlashCommand(name string) (tea.Model, tea.Cmd) {
	m.setOverlayKind(shellOverlayNone, false)
	m.composer.SetValue("")
	switch strings.TrimSpace(strings.ToLower(name)) {
	case "/help":
		m.pushNote("Help", shellHelpLines(), false)
	case "/status":
		m.pushNote("Status", shellStatusLines(m.snapshot, m.ui, m.host), false)
	case "/inspect":
		m.pushNote("Inspect", shellInspectLines(m.snapshot, m.ui, m.host), false)
	case "/sessions":
		m.pushNote("Sessions", shellSessionsLines(m.ui.Session, m.snapshot), false)
	case "/clear":
		m.localEntries = nil
	case "/model":
		m.setOverlayKind(shellOverlayModel, false)
		m.menuSelected = 0
	case "/next":
		if err := executablePrimaryStepExists(m.snapshot, m.app.ActionExecutor); err != nil {
			m.pushWarning("Next", []string{err.Error(), shellCommandHintLine(m.snapshot)})
			break
		}
		cmd := shellExecutePrimaryActionCmd(m.app.ActionExecutor, m.app.TaskID, m.snapshot, &m.ui)
		m.syncViewport(true)
		return m, cmd
	case "/run":
		return m.executeGuidedPrimaryCommand("run", []string{"RUN", "START_LOCAL_RUN"})
	case "/continue":
		return m.executeGuidedPrimaryCommand("continue", []string{"CONTINUE"})
	case "/checkpoint":
		return m.executeGuidedPrimaryCommand("checkpoint", []string{"CHECKPOINT"})
	case "/handoff":
		return m.executeGuidedPrimaryCommand("handoff", []string{"HANDOFF", "LAUNCH"})
	default:
		m.pushWarning("Commands", []string{"Unknown command " + name})
	}
	m.syncViewport(true)
	return m, nil
}

func (m *shellModel) executeGuidedPrimaryCommand(noun string, actionTerms []string) (tea.Model, tea.Cmd) {
	step := currentPrimaryStep(m.snapshot)
	if step != nil {
		for _, term := range actionTerms {
			if strings.Contains(strings.ToUpper(step.Action), term) {
				if err := executablePrimaryStepExists(m.snapshot, m.app.ActionExecutor); err == nil {
					cmd := shellExecutePrimaryActionCmd(m.app.ActionExecutor, m.app.TaskID, m.snapshot, &m.ui)
					m.syncViewport(true)
					return m, cmd
				}
				break
			}
		}
	}
	lines := []string{
		fmt.Sprintf("No direct %s action is exposed as the current primary step.", noun),
		shellCommandHintLine(m.snapshot),
		"Use /next when Tuku exposes a direct operator step through the shell.",
	}
	m.pushWarning(strings.Title(noun), lines)
	m.syncViewport(true)
	return m, nil
}

func (m *shellModel) selectModelOption(name string) (tea.Model, tea.Cmd) {
	m.setOverlayKind(shellOverlayNone, false)
	lines := []string{
		fmt.Sprintf("Previewed %s.", name),
		fmt.Sprintf("Current worker remains %s.", effectiveWorkerLabel(m.snapshot, m.host)),
		"This shell does not expose an authoritative runtime model switch yet.",
		"Use this preview to inspect options only; no worker/runtime change was performed.",
	}
	m.pushNote("Model", lines, false)
	m.syncViewport(true)
	return m, nil
}

func (m *shellModel) pollHost() {
	current := m.host.Status()
	if nextHost, note, changed := transitionExitedHost(m.ctx, m.host, m.app.FallbackHost, m.snapshot); changed {
		flushTranscriptEvidence(m.app.TaskID, m.ui.Session.SessionID, m.host, m.app.TranscriptSink, &m.ui)
		m.host = nextHost
		m.resizeHost()
		if note != "" {
			m.pushWarning("Worker", []string{note})
			m.ui.LastError = note
		}
		current = m.host.Status()
	}

	captureHostLifecycle(m.ctx, m.app.LifecycleSink, m.app.TaskID, m.ui.Session.SessionID, &m.ui, m.lastHost, current)
	if hostStatusChanged(m.lastHost, current) {
		reportShellSession(m.app.RegistrySink, m.app.TaskID, &m.ui.Session, current, true, &m.ui)
		if current.State != HostStateLive || current.Mode == HostModeTranscript {
			m.archivedHostLines = nil
		}
	}
	if m.ui.WorkerPromptPending {
		if current.State != HostStateLive {
			m.clearWorkerTurnState()
		} else {
			if !current.LastOutputAt.IsZero() && (m.ui.LastWorkerPromptAt.IsZero() || !current.LastOutputAt.Before(m.ui.LastWorkerPromptAt)) {
				m.ui.WorkerResponseStarted = true
			}
			if active, note := m.workerTurnStillActive(current); !active {
				m.clearWorkerTurnState()
				if note != "" {
					m.pushWarning("Worker state", []string{
						note,
						"Use /status or /inspect if the live worker still appears busy.",
					})
				}
			}
		}
	}
	if shellHostChanged(m.lastHost, current) || hostStatusChanged(m.lastHost, current) {
		m.lastDigest = current.RenderVersion
		m.syncViewport(false)
	}
	m.lastHost = current
}

func (m *shellModel) syncOverlayFromComposer() {
	previous := m.overlayKind
	value := strings.TrimSpace(m.composer.Value())
	if strings.HasPrefix(value, "/") {
		if m.overlayKind != shellOverlayCommands {
			m.overlayKind = shellOverlayCommands
			m.menuSelected = 0
		}
		items := m.filteredOverlayItems()
		if len(items) == 0 {
			m.menuSelected = 0
			return
		}
		if m.menuSelected >= len(items) {
			m.menuSelected = len(items) - 1
		}
		return
	}
	if m.overlayKind == shellOverlayCommands {
		m.overlayKind = shellOverlayNone
		m.menuSelected = 0
	}
	if previous != m.overlayKind {
		m.reflowLayout(false)
	}
}

func (m *shellModel) filteredOverlayItems() []shellCommand {
	if m.overlayKind == shellOverlayModel {
		return append([]shellCommand{}, shellModelOptions...)
	}
	filter := strings.TrimSpace(strings.TrimPrefix(m.composer.Value(), "/"))
	if filter == "" {
		return append([]shellCommand{}, shellCommands...)
	}
	filter = strings.ToLower(filter)
	out := make([]shellCommand, 0, len(shellCommands))
	for _, cmd := range shellCommands {
		name := strings.TrimPrefix(strings.ToLower(cmd.Name), "/")
		desc := strings.ToLower(cmd.Description)
		if strings.Contains(name, filter) || strings.Contains(desc, filter) {
			out = append(out, cmd)
		}
	}
	return out
}

func (m *shellModel) View() string {
	if m.width <= 0 || m.height <= 0 {
		return "loading shell..."
	}

	layout := m.layout()
	styles := newShellStyles()

	header := m.renderHeader(styles, layout)
	feed := m.renderViewport(layout)
	composer := m.renderComposer(styles, layout)
	footer := m.renderFooter(styles, layout)

	sections := []string{header, feed, composer}
	if m.overlayKind != shellOverlayNone {
		sections = append(sections, m.renderOverlay(styles, layout))
	}
	sections = append(sections, footer)
	return styles.root.Render(lipgloss.JoinVertical(lipgloss.Left, sections...))
}

func (m *shellModel) resize() {
	m.reflowLayout(false)
}

func (m *shellModel) reflowLayout(forceBottom bool) {
	layout := m.layout()
	m.viewport.Width = layout.feedWidth
	m.composer.Width = layout.composerWidth
	m.resizeHost()
	m.syncViewport(forceBottom)
}

func (m *shellModel) setOverlayKind(kind shellOverlayKind, forceBottom bool) {
	if m.overlayKind == kind {
		return
	}
	m.overlayKind = kind
	m.reflowLayout(forceBottom)
}

func (m *shellModel) resizeHost() {
	if m.host == nil {
		return
	}
	layout := m.layout()
	m.host.Resize(max(10, layout.composerWidth), max(1, layout.viewportHeight-2))
}

func (m *shellModel) layout() shellSurfaceLayout {
	width := m.width
	if width <= 0 {
		width = 60
	}
	height := m.height
	if height <= 0 {
		height = 16
	}
	padding := 2
	if width < 88 {
		padding = 1
	}
	headerHeight := 3
	if width < 88 || height < 18 {
		headerHeight = 2
	}
	composerHeight := 5
	if height < 20 {
		composerHeight = 4
	}
	contentWidth := width - (padding * 2)
	if contentWidth > 1 {
		contentWidth--
	}
	if contentWidth < 40 {
		contentWidth = max(1, width-2)
		padding = 0
	}
	layout := shellSurfaceLayout{
		padding:      padding,
		headerHeight: headerHeight,
		contentWidth: contentWidth,
		feedWidth:    max(10, contentWidth),
		composerWidth: max(10,
			contentWidth-6),
	}
	layout.composerHeight = max(composerHeight, m.composerBlockHeight(layout))
	layout.footerHeight = m.footerBlockHeight(layout)
	if m.overlayKind != shellOverlayNone {
		styles := newShellStyles()
		layout.overlayHeight = len(splitLines(m.renderOverlay(styles, layout)))
	}

	viewportHeight := height - layout.headerHeight - layout.footerHeight - layout.composerHeight - layout.overlayHeight
	if viewportHeight < 1 {
		viewportHeight = 1
	}
	layout.viewportHeight = viewportHeight
	return layout
}

func (m *shellModel) syncViewport(forceBottom bool) {
	layout := m.layout()
	feed := m.renderedFeed(layout.contentWidth)
	content := strings.Join(feed.blocks, "\n\n")
	totalLines := shellFeedLineCount(feed.blocks)
	desiredHeight := layout.viewportHeight
	if totalLines > 0 && totalLines < desiredHeight {
		desiredHeight = totalLines
	}
	if desiredHeight < 1 {
		desiredHeight = 1
	}
	layoutChanged := m.lastViewportWidth != layout.feedWidth || m.lastViewportHeight != desiredHeight
	if content == m.lastContent && !forceBottom && m.didInitialFit && m.scrollToEntry == "" && !layoutChanged {
		return
	}
	oldOffset := m.viewport.YOffset
	m.viewport.Width = layout.feedWidth
	scrollToEntry := m.scrollToEntry != "" && (forceBottom || m.followLatest)
	m.viewport.SetContent(content)
	m.viewport.Height = desiredHeight
	maxOffset := max(0, m.viewport.TotalLineCount()-m.viewport.Height)
	switch {
	case !m.didInitialFit:
		m.viewport.YOffset = min(shellInitialViewportOffset(feed.blocks, m.viewport.Height), maxOffset)
		m.didInitialFit = true
	case scrollToEntry:
		offset, ok := feed.entryOffsets[m.scrollToEntry]
		if !ok {
			m.viewport.GotoBottom()
		} else {
			m.viewport.YOffset = min(offset, maxOffset)
		}
	case forceBottom || m.followLatest:
		m.viewport.GotoBottom()
	default:
		if oldOffset > maxOffset {
			oldOffset = maxOffset
		}
		m.viewport.YOffset = oldOffset
	}
	m.scrollToEntry = ""
	m.lastContent = content
	m.lastViewportWidth = layout.feedWidth
	m.lastViewportHeight = desiredHeight
	m.syncFollowLatest()
}

func (m shellModel) renderViewport(layout shellSurfaceLayout) string {
	return indentBlock(m.viewport.View(), layout.padding)
}

func (m shellModel) renderFeedContent(width int) string {
	return strings.Join(m.renderFeedBlocks(width), "\n\n")
}

func (m shellModel) renderFeedBlocks(width int) []string {
	return m.renderedFeed(width).blocks
}

type renderedShellFeed struct {
	blocks       []string
	entryOffsets map[string]int
}

func (m shellModel) renderedFeed(width int) renderedShellFeed {
	entries := m.feedEntries(width)
	if len(entries) == 0 {
		return renderedShellFeed{}
	}
	out := renderedShellFeed{
		blocks:       make([]string, 0, len(entries)),
		entryOffsets: make(map[string]int, len(entries)),
	}
	lineOffset := 0
	for idx, entry := range entries {
		block := m.renderFeedEntry(entry, width)
		out.blocks = append(out.blocks, block)
		if entry.Key != "" {
			out.entryOffsets[entry.Key] = lineOffset
		}
		lineOffset += len(splitLines(block))
		if idx < len(entries)-1 {
			lineOffset++
		}
	}
	return out
}

func (m shellModel) renderFeedEntry(entry shellFeedEntry, width int) string {
	if entry.Kind == shellFeedWorking {
		return renderWorkingEntry(newShellStyles(), entry, width, m.spinner.View())
	}
	return renderFeedEntry(entry, width)
}

func shellFeedLineCount(blocks []string) int {
	if len(blocks) == 0 {
		return 0
	}
	total := 0
	for i, block := range blocks {
		total += len(splitLines(block))
		if i < len(blocks)-1 {
			total++
		}
	}
	return total
}

func shellTailViewportLineCount(blocks []string) int {
	if len(blocks) == 0 {
		return 0
	}
	start := max(0, len(blocks)-shellTailViewportBlocks)
	total := 0
	for i := start; i < len(blocks); i++ {
		total += len(splitLines(blocks[i]))
		if i < len(blocks)-1 {
			total++
		}
	}
	return total
}

func (m shellModel) feedEntries(width int) []shellFeedEntry {
	entries := make([]shellFeedEntry, 0, 16)
	hostMode := m.host.Status().Mode

	if hostMode == HostModeTranscript {
		lines := curateWorkerLines(shellHistoryLines(m.host, max(10, width-4)), shellPromptBodies(entries))
		if !hasMeaningfulWorkerLines(lines) {
			entries = append(entries, shellIntroEntry(m.snapshot, m.host))
		} else {
			entries = append(entries, shellFeedEntry{
				Key:          "transcript",
				Kind:         shellFeedWorker,
				Title:        shellWorkerStreamTitle(m.snapshot, m.host),
				Body:         lines,
				Preformatted: true,
			})
		}
		entries = append(entries, m.localEntries...)
	} else {
		conversation := shellConversationEntries(m.snapshot)
		if m.startupConversationBaseline > 0 && len(conversation) >= m.startupConversationBaseline {
			conversation = conversation[m.startupConversationBaseline:]
		}
		entries = append(entries, conversation...)
		if len(entries) == 0 {
			entries = append(entries, shellIntroEntry(m.snapshot, m.host))
		}
		entries = append(entries, m.localEntries...)
		lines := trimCommittedHostLines(curateWorkerLines(shellHistoryLines(m.host, max(10, width-4)), shellPromptBodies(entries)), m.archivedHostLines)
		if len(lines) > 0 && hasMeaningfulWorkerLines(lines) {
			entries = append(entries, shellFeedEntry{
				Key:          "host-stream",
				Kind:         shellFeedWorker,
				Title:        shellWorkerStreamTitle(m.snapshot, m.host),
				Body:         lines,
				Preformatted: true,
			})
		}
		if working := m.workingStateEntry(); working != nil {
			entries = append(entries, *working)
		}
	}

	if shellState := m.shellStateEntry(); shellState != nil {
		entries = append([]shellFeedEntry{*shellState}, entries...)
	}

	return coalesceFeedEntries(entries)
}

func (m shellModel) renderHeader(styles shellStyles, layout shellSurfaceLayout) string {
	leftTitle := styles.headerKicker.Render("TUKU") + " " + styles.headerTitle.Render("shell")
	rightParts := make([]string, 0, 1)
	if task := shellCompactID(m.snapshot.TaskID); task != "" && task != "no-task" {
		rightParts = append(rightParts, "task "+task)
	}
	chips := []string{
		shellToneChip(styles, effectiveWorkerLabel(m.snapshot, m.host), workerTone(m.host)),
		shellToneChip(styles, shellWorkerStateLabel(m.snapshot, m.ui, m.host), workerStatusTone(m.host.Status())),
		shellToneChip(styles, continuityLabel(m.snapshot), continuityTone(m.snapshot)),
	}
	for _, part := range shellRepoChips(m.snapshot.Repo) {
		chips = append(chips, shellToneChip(styles, part, "muted"))
	}

	line1 := joinLeftRight(leftTitle, styles.headerMeta.Render(strings.Join(rightParts, "  ·  ")), layout.contentWidth)

	meta := strings.Join(chips, "  ")
	line2 := styles.headerMeta.Render(ansiTruncate(meta, layout.contentWidth))
	if layout.headerHeight <= 2 {
		return indentBlock(strings.Join([]string{line1, line2}, "\n"), layout.padding)
	}
	rule := styles.headerRule.Render(strings.Repeat("─", layout.contentWidth))
	return indentBlock(strings.Join([]string{line1, line2, rule}, "\n"), layout.padding)
}

func (m shellModel) renderComposer(styles shellStyles, layout shellSurfaceLayout) string {
	state := m.composerState()
	m.composer.Placeholder = state.Placeholder
	bodyWidth := layout.composerWidth
	m.composer.Width = bodyWidth

	box := styles.composerBox
	if m.overlayKind != shellOverlayNone || state.SendMode == "worker" || state.SendMode == "scratch" {
		box = styles.composerFocus
	}

	header := joinLeftRight(
		styles.composerLabel.Render(state.Label),
		shellToneChip(styles, state.Status, state.Tone),
		max(10, layout.contentWidth-4),
	)

	composerView := m.composer.View()
	line := styles.composerPrompt.Render("›") + " " + ansiPadToWidth(composerView, bodyWidth)
	contentLines := []string{header, line}
	for _, hint := range m.composerHintLines(layout) {
		contentLines = append(contentLines, styles.composerHint.Render(hint))
	}
	content := strings.Join(contentLines, "\n")
	return indentBlock(box.Width(layout.contentWidth).Render(content), layout.padding)
}

func (m shellModel) renderFooter(styles shellStyles, layout shellSurfaceLayout) string {
	state := m.composerState()
	leftParts := []string{
		"session " + shellCompactID(m.ui.Session.SessionID),
	}
	if next := operatorActionLabel(m.snapshot); next != "" && next != "none" {
		leftParts = append(leftParts, "next "+next)
	}
	if refresh := m.ui.LastRefresh; !refresh.IsZero() {
		leftParts = append(leftParts, "refreshed "+refresh.Local().Format("15:04"))
	}
	right := shellFooterHint(state)
	if !m.followLatest && m.viewport.TotalLineCount() > m.viewport.Height {
		right = "PgDn to return to latest"
	}
	if m.exitConfirmActive() {
		right = "Press Ctrl-C again to exit"
	}
	line := joinLeftRight(
		styles.footer.Render(strings.Join(leftParts, "  ·  ")),
		styles.footerMuted.Render(right),
		layout.contentWidth,
	)
	helpView := m.renderHelpView(layout.contentWidth)
	if strings.TrimSpace(helpView) == "" {
		return indentBlock(line, layout.padding)
	}
	return indentBlock(strings.Join([]string{line, helpView}, "\n"), layout.padding)
}

func (m shellModel) composerHintLines(layout shellSurfaceLayout) []string {
	return nil
}

func (m shellModel) composerBlockHeight(layout shellSurfaceLayout) int {
	baseLines := 2 + len(m.composerHintLines(layout))
	frameHeight := newShellStyles().composerBox.GetVerticalFrameSize()
	return baseLines + frameHeight
}

func (m shellModel) footerBlockHeight(layout shellSurfaceLayout) int {
	height := 1
	if helpView := m.renderHelpView(layout.contentWidth); strings.TrimSpace(helpView) != "" {
		height += len(splitLines(helpView))
	}
	return height
}

func (m shellModel) renderHelpView(width int) string {
	if width <= 0 {
		return ""
	}
	helpModel := m.help
	helpModel.Width = width
	return helpModel.View(m.helpKeyMap())
}

func (m shellModel) renderOverlay(styles shellStyles, layout shellSurfaceLayout) string {
	items := m.filteredOverlayItems()
	title := "Commands"
	subtitle := "Enter select • Esc dismiss"
	if m.overlayKind == shellOverlayModel {
		title = "Model preview"
		subtitle = "Preview only • worker unchanged"
	}
	lines := []string{styles.menuTitle.Render(title), styles.menuDesc.Render(subtitle)}
	maxOverlayLines := min(28, max(18, m.height-6))
	if len(items) == 0 {
		lines = append(lines, styles.menuDesc.Render("No matches"))
	} else {
		start, end := overlayWindow(len(items), m.menuSelected, shellOverlayVisibleItems)
		lastGroup := ""
		for idx := start; idx < end; idx++ {
			item := items[idx]
			if item.Group != "" && item.Group != lastGroup {
				lines = append(lines, styles.menuSection.Render(strings.ToUpper(item.Group)))
				lastGroup = item.Group
			}
			prefix := "  "
			rowStyle := styles.menuItem
			if idx == m.menuSelected {
				prefix = styles.menuSelectedKey.Render("› ")
				rowStyle = styles.menuSelected
			}
			name := ansiTruncate(item.Name, 14)
			descWidth := max(10, min(42, layout.contentWidth-24))
			desc := styles.menuDesc.Render(ansiTruncate(item.Description, descWidth))
			row := prefix + rowStyle.Render(ansiPadToWidth(name, 14)) + "  " + desc
			lines = append(lines, row)
		}
		if len(items) > 0 {
			selected := max(0, min(m.menuSelected, len(items)-1)) + 1
			lines = append(lines, styles.menuDesc.Render(fmt.Sprintf("%d of %d", selected, len(items))))
		}
	}
	_ = maxOverlayLines
	return styles.menuBox.Width(min(66, max(36, layout.contentWidth-10))).Render(strings.Join(lines, "\n"))
}

func newShellStyles() shellStyles {
	return shellStyles{
		root:          lipgloss.NewStyle().Foreground(lipgloss.Color("#E5E7EB")),
		headerKicker:  lipgloss.NewStyle().Foreground(lipgloss.Color("#F9FAFB")).Bold(true),
		headerTitle:   lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF")),
		headerMeta:    lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")),
		headerRule:    lipgloss.NewStyle().Foreground(lipgloss.Color("#374151")),
		chip:          lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB")),
		chipAccent:    lipgloss.NewStyle().Foreground(lipgloss.Color("#F9FAFB")),
		chipPositive:  lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB")),
		chipCaution:   lipgloss.NewStyle().Foreground(lipgloss.Color("#E5E7EB")),
		chipMuted:     lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF")),
		feedTitle:     lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB")).Bold(true),
		feedBody:      lipgloss.NewStyle().Foreground(lipgloss.Color("#E5E7EB")),
		feedUserTitle: lipgloss.NewStyle().Foreground(lipgloss.Color("#F9FAFB")).Bold(true),
		feedUserStrip: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F3F4F6")).
			Background(lipgloss.Color("#1F2937")).
			Padding(0, 1),
		feedWorkerTitle: lipgloss.NewStyle().Foreground(lipgloss.Color("#E5E7EB")).Bold(true),
		feedWorkerMeta:  lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF")),
		feedActionVerb:  lipgloss.NewStyle().Foreground(lipgloss.Color("#F3F4F6")).Bold(true),
		feedPath:        lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB")).Underline(true),
		feedNoteTitle:   lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB")).Bold(true),
		feedWarnTitle:   lipgloss.NewStyle().Foreground(lipgloss.Color("#E5E7EB")).Bold(true),
		feedErrorTitle:  lipgloss.NewStyle().Foreground(lipgloss.Color("#F3A8A8")).Bold(true),
		composerBox: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#374151")).
			Padding(0, 1),
		composerFocus: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#9CA3AF")).
			Padding(0, 1),
		composerLabel:  lipgloss.NewStyle().Foreground(lipgloss.Color("#F9FAFB")).Bold(true),
		composerHint:   lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF")),
		composerPrompt: lipgloss.NewStyle().Foreground(lipgloss.Color("#F9FAFB")).Bold(true),
		footer:         lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF")),
		footerMuted:    lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")),
		menuBox: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#4B5563")).
			Padding(0, 1),
		menuTitle:       lipgloss.NewStyle().Foreground(lipgloss.Color("#F9FAFB")).Bold(true),
		menuSection:     lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Bold(true),
		menuItem:        lipgloss.NewStyle().Foreground(lipgloss.Color("#E5E7EB")),
		menuSelected:    lipgloss.NewStyle().Foreground(lipgloss.Color("#F9FAFB")).Bold(true),
		menuSelectedKey: lipgloss.NewStyle().Foreground(lipgloss.Color("#F9FAFB")).Bold(true),
		menuDesc:        lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF")),
	}
}

func shellToneChip(styles shellStyles, label string, tone string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return ""
	}
	render := func(style lipgloss.Style) string {
		return style.Render("[" + label + "]")
	}
	switch tone {
	case "accent":
		return render(styles.chipAccent)
	case "positive":
		return render(styles.chipPositive)
	case "caution", "danger":
		return render(styles.chipCaution)
	case "muted":
		return render(styles.chipMuted)
	default:
		return render(styles.chip)
	}
}

func shellConversationEntries(snapshot Snapshot) []shellFeedEntry {
	items := visibleConversationItems(snapshot)
	out := make([]shellFeedEntry, 0, len(items))
	for idx, msg := range items {
		body := strings.TrimSpace(msg.Body)
		if body == "" {
			continue
		}
		entry := shellFeedEntry{
			Key:   fmt.Sprintf("conversation-%d-%s", idx, msg.Role),
			Title: "Tuku",
			Body:  strings.Split(body, "\n"),
		}
		switch strings.TrimSpace(msg.Role) {
		case "user":
			entry.Kind = shellFeedUser
			entry.Title = "You"
		case "worker":
			entry.Kind = shellFeedWorker
			title := humanizeConstant(snapshotWorkerLabel(snapshot))
			if title == "" || strings.EqualFold(title, "none") {
				title = "Worker"
			}
			entry.Title = title
		default:
			continue
		}
		out = append(out, entry)
	}
	return out
}

func visibleConversationItems(snapshot Snapshot) []ConversationItem {
	out := make([]ConversationItem, 0, len(snapshot.RecentConversation))
	for _, msg := range snapshot.RecentConversation {
		switch strings.TrimSpace(msg.Role) {
		case "user", "worker":
			if strings.TrimSpace(msg.Body) == "" {
				continue
			}
			out = append(out, msg)
		}
	}
	return out
}

func shellIntroEntry(snapshot Snapshot, host WorkerHost) shellFeedEntry {
	lines := []string{
		fmt.Sprintf("Connected to %s for %s.", effectiveWorkerLabel(snapshot, host), shellRepoSummary(snapshot.Repo)),
		"Ask for a change, a reading, or the next bounded step.",
		"Type / for commands. PgUp and PgDn scroll history without leaving the composer.",
	}
	if state := shellWorkerStateLabel(snapshot, UIState{}, host); state != "" {
		lines = append(lines, "Current state: "+state+".")
	}
	return shellFeedEntry{
		Key:   "intro",
		Kind:  shellFeedIntro,
		Title: "Ready",
		Body:  lines,
	}
}

func shellWorkerStreamTitle(snapshot Snapshot, host WorkerHost) string {
	if host == nil {
		return "Worker"
	}
	status := host.Status()
	if status.State == HostStateTranscriptOnly || status.State == HostStateFallback {
		return "Worker transcript"
	}
	if status.State == HostStateLive {
		return "Live worker"
	}
	if status.State == HostStateStarting {
		return "Worker starting"
	}
	title := strings.TrimSpace(host.Title())
	if title == "" {
		return "Worker"
	}
	return strings.TrimPrefix(title, "worker pane | ")
}

func shellHelpLines() []string {
	return []string{
		"Type in the bottom composer and press Enter to submit.",
		"Type / to open the command palette immediately.",
		"PgUp, PgDn, Ctrl+U, and Ctrl+D scroll history without leaving the composer.",
		"Esc closes overlays and menus cleanly.",
		"Press Ctrl-C twice to exit the shell.",
		"/next executes Tuku's current primary operator step when a direct path is exposed.",
		"Status and inspect stay on demand so the default view remains shell-first.",
	}
}

func shellStatusLines(snapshot Snapshot, ui UIState, host WorkerHost) []string {
	return []string{
		"worker " + effectiveWorkerLabel(snapshot, host),
		"state " + shellWorkerStateLabel(snapshot, ui, host),
		"phase " + nonEmpty(snapshot.Phase, "unknown"),
		"continuity " + continuityLabel(snapshot),
		"next " + operatorActionLabel(snapshot),
		"readiness " + operatorReadinessLine(snapshot),
		"decision " + operatorDecisionHeadline(snapshot),
		"command " + operatorExecutionCommand(snapshot),
		"transcript review " + transcriptReviewStatusLine(ui.Session),
		"registry " + sessionRegistrySummary(ui.Session),
		latestCanonicalLine(snapshot),
	}
}

func shellInspectLines(snapshot Snapshot, ui UIState, host WorkerHost) []string {
	lines := []string{
		"intent " + intentDigestLine(snapshot),
		"brief " + briefDigestLine(snapshot),
		"decision " + operatorDecisionHeadline(snapshot),
		"guidance " + operatorDecisionGuidance(snapshot),
		"integrity " + operatorDecisionIntegrity(snapshot),
		"authority " + operatorAuthorityLine(snapshot),
		"launch " + launchControlLine(snapshot),
		"handoff " + handoffLine(snapshot),
		"checkpoint " + checkpointLine(snapshot),
		"branch " + activeBranchLine(snapshot),
		"local run " + localRunFinalizationLine(snapshot),
		"local resume " + localResumeLine(snapshot),
		"incident triage " + latestIncidentTriageLine(snapshot),
		"incident follow-up " + incidentFollowUpLine(snapshot),
		"incident closure " + continuityIncidentClosureLine(snapshot),
		"incident task risk " + continuityIncidentTaskRiskLine(snapshot),
		"host " + hostStatusLine(snapshot, ui, host),
	}
	if ui.LastPrimaryActionResult != nil {
		lines = append(lines, "result "+operatorActionResultHeadline(ui))
	}
	return lines
}

func shellSessionsLines(session SessionState, snapshot Snapshot) []string {
	if len(session.KnownSessions) == 0 && len(snapshot.ShellSessions) == 0 {
		return []string{"No durable shell sessions are recorded yet for this task."}
	}
	lines := []string{sessionRegistrySummary(session)}
	source := session.KnownSessions
	if len(source) == 0 {
		source = snapshot.ShellSessions
	}
	for _, known := range source {
		summary := strings.TrimSpace(known.OperatorSummary)
		if summary == "" {
			summary = humanizeConstant(string(known.SessionClass))
		}
		lines = append(lines, fmt.Sprintf("%s  %s  %s", shellSessionListID(known.SessionID), humanizeConstant(string(known.AttachCapability)), summary))
	}
	return lines
}

func shellCommandHintLine(snapshot Snapshot) string {
	command := strings.TrimSpace(operatorExecutionCommand(snapshot))
	if command == "" || command == "n/a" {
		return "No direct command hint is available for the current task state."
	}
	return "Command hint: " + command
}

func (m shellModel) composerState() shellComposerState {
	applyExitCue := func(state shellComposerState) shellComposerState {
		if m.exitConfirmActive() {
			state.Hint = "Press Ctrl-C again to exit."
		}
		return state
	}
	if m.overlayKind == shellOverlayCommands {
		return applyExitCue(shellComposerState{
			Label:       "Command filter",
			Status:      "commands",
			Hint:        "Use ↑ ↓ and Enter to pick a command. Esc closes the palette.",
			Placeholder: "/help, /status, /next…",
			Tone:        "accent",
			SendMode:    "blocked",
		})
	}
	if m.overlayKind == shellOverlayModel {
		return applyExitCue(shellComposerState{
			Label:       "Model preview",
			Status:      "model preview",
			Hint:        "Preview only. Runtime and worker remain unchanged.",
			Placeholder: "Preview runtime options…",
			Tone:        "muted",
			SendMode:    "blocked",
		})
	}
	if m.ui.PrimaryActionInFlight != nil {
		return applyExitCue(shellComposerState{
			Label:       "Tuku step",
			Status:      "tuku running",
			Hint:        "Tuku is executing the current primary step. History stays scrollable.",
			Placeholder: "Tuku is executing the current step…",
			Tone:        "caution",
			SendMode:    "blocked",
		})
	}
	if m.ui.WorkerPromptPending {
		assessment := assessWorkerTurn(m.host.Status(), m.ui, m.hostCanInterrupt(), m.workerTurnAuthoritative(), time.Now().UTC())
		hint := assessment.Hint
		if m.hostCanInterrupt() && !strings.Contains(strings.ToLower(hint), "esc") {
			hint = strings.TrimSpace(hint + " Esc sends an interrupt to the live worker.")
		}
		return applyExitCue(shellComposerState{
			Label:       "Live worker",
			Status:      assessment.StatusLabel,
			Hint:        hint,
			Placeholder: "Waiting for the live worker reply…",
			Tone:        assessment.Tone,
			SendMode:    "blocked",
		})
	}
	if isScratchIntakeSnapshot(m.snapshot) {
		return applyExitCue(shellComposerState{
			Label:       "Local scratch",
			Status:      "local scratch",
			Hint:        "Enter saves a local scratch note on this machine. Type / for commands.",
			Placeholder: "Write a local scratch note…",
			Tone:        "muted",
			SendMode:    "scratch",
		})
	}
	if m.host != nil && m.host.CanAcceptInput() {
		return applyExitCue(shellComposerState{
			Label:       "Live worker",
			Status:      "live worker",
			Hint:        "Enter sends directly to the live worker. Type / for commands.",
			Placeholder: "Ask the worker to inspect, explain, or change something…",
			Tone:        "positive",
			SendMode:    "worker",
		})
	}
	if strings.TrimSpace(m.snapshot.TaskID) != "" && m.app.MessageSender != nil {
		return applyExitCue(shellComposerState{
			Label:       "Tuku message",
			Status:      "send via tuku",
			Hint:        "Enter sends through Tuku canonical continuity while the worker is not directly writable.",
			Placeholder: "Send a canonical operator message through Tuku…",
			Tone:        "accent",
			SendMode:    "canonical",
		})
	}
	return applyExitCue(shellComposerState{
		Label:       "Read-only shell",
		Status:      "read-only",
		Hint:        "Input is not available right now. Use / for commands or review history.",
		Placeholder: "Input unavailable in this shell state…",
		Tone:        "muted",
		SendMode:    "blocked",
	})
}

func shellCompactID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if len(id) <= 14 {
		return id
	}
	return id[:8] + "…" + id[len(id)-4:]
}

func shellSessionListID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if len(id) <= 20 {
		return id
	}
	return id[:10] + "…" + id[len(id)-6:]
}

func shellRepoSummary(anchor RepoAnchor) string {
	if strings.TrimSpace(anchor.RepoRoot) == "" {
		return "this machine"
	}
	name := filepath.Base(anchor.RepoRoot)
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = anchor.RepoRoot
	}
	branch := strings.TrimSpace(anchor.Branch)
	prefix := "workspace " + name
	if anchor.WorkingTreeDirty {
		if branch != "" {
			return fmt.Sprintf("%s on %s (dirty)", prefix, branch)
		}
		return prefix + " (dirty)"
	}
	if branch != "" {
		return fmt.Sprintf("%s on %s", prefix, branch)
	}
	return prefix
}

func shellRepoChips(anchor RepoAnchor) []string {
	if strings.TrimSpace(anchor.RepoRoot) == "" {
		return []string{"no repo"}
	}
	name := filepath.Base(anchor.RepoRoot)
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = anchor.RepoRoot
	}
	parts := []string{"workspace " + name}
	if branch := strings.TrimSpace(anchor.Branch); branch != "" {
		parts = append(parts, "branch "+branch)
	}
	if anchor.WorkingTreeDirty {
		parts = append(parts, "dirty")
	}
	return parts
}

func (m shellModel) shellStateEntry() *shellFeedEntry {
	state := m.composerState()
	switch state.SendMode {
	case "canonical":
		entry := shellFeedEntry{
			Key:   "shell-state-canonical",
			Kind:  shellFeedTuku,
			Title: "Tuku send path",
			Body: []string{
				"Direct worker input is paused in this shell state.",
				"Enter still sends a canonical Tuku message while the feed stays readable and scrollable.",
			},
		}
		return &entry
	case "blocked":
		if m.overlayKind != shellOverlayNone || m.ui.PrimaryActionInFlight != nil || m.ui.WorkerPromptPending {
			return nil
		}
		hostStatus := m.host.Status()
		switch hostStatus.State {
		case HostStateTranscriptOnly, HostStateFallback, HostStateExited, HostStateFailed:
			title := "Read-only shell"
			lines := []string{
				"Showing bounded transcript evidence in a constrained shell state.",
				"Use /sessions or /status for continuity context and durable session detail.",
			}
			if hostStatus.State == HostStateFallback {
				title = "Fallback shell"
				lines[0] = "Live worker input is unavailable, so the shell is showing bounded fallback evidence."
			}
			entry := shellFeedEntry{
				Key:   "shell-state-read-only",
				Kind:  shellFeedTuku,
				Title: title,
				Body:  lines,
			}
			return &entry
		}
	}
	return nil
}

func (m shellModel) workingStateEntry() *shellFeedEntry {
	if !m.ui.WorkerPromptPending && m.ui.PrimaryActionInFlight == nil {
		return nil
	}
	startedAt := m.ui.LastWorkerPromptAt
	if inflight := m.ui.PrimaryActionInFlight; inflight != nil && !inflight.StartedAt.IsZero() {
		startedAt = inflight.StartedAt
	}
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	line := renderWorkingLine(time.Since(startedAt), nil, m.hostCanInterrupt() && m.ui.PrimaryActionInFlight == nil, m.ui.WorkerInterruptRequested)
	if m.ui.WorkerPromptPending {
		assessment := assessWorkerTurn(m.host.Status(), m.ui, m.hostCanInterrupt(), m.workerTurnAuthoritative(), time.Now().UTC())
		line = assessment.WorkingLine
	}
	return &shellFeedEntry{
		Key:   "working",
		Kind:  shellFeedWorking,
		Title: "Working",
		Body:  []string{line},
	}
}

func shellWorkerStateLabel(snapshot Snapshot, ui UIState, host WorkerHost) string {
	if ui.PrimaryActionInFlight != nil {
		return "tuku step running"
	}
	if ui.WorkerPromptPending {
		return "worker responding"
	}
	if isScratchIntakeSnapshot(snapshot) {
		return "local scratch"
	}
	return workerStateBadge(host)
}

func shellPromptBodies(entries []shellFeedEntry) []string {
	out := make([]string, 0, 6)
	for _, entry := range entries {
		if entry.Kind != shellFeedUser || len(entry.Body) != 1 {
			continue
		}
		body := strings.TrimSpace(entry.Body[0])
		if body == "" {
			continue
		}
		out = append(out, body)
	}
	if len(out) > 6 {
		out = out[len(out)-6:]
	}
	return out
}

func curateWorkerLines(lines []string, prompts []string) []string {
	if len(lines) == 0 {
		return nil
	}
	seenPrompts := map[string]struct{}{}
	for _, prompt := range prompts {
		seenPrompts[strings.TrimSpace(prompt)] = struct{}{}
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if shellFeedNoiseLine(trimmed) {
			continue
		}
		if strings.HasPrefix(trimmed, "tuku> ") {
			echo := strings.TrimSpace(strings.TrimPrefix(trimmed, "tuku> "))
			if _, ok := seenPrompts[echo]; ok {
				continue
			}
		}
		out = append(out, line)
	}
	return out
}

func hasMeaningfulWorkerLines(lines []string) bool {
	if len(lines) == 0 {
		return false
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || shellFeedNoiseLine(trimmed) {
			continue
		}
		return true
	}
	return false
}

func shellInitialViewportOffset(blocks []string, viewportHeight int) int {
	if len(blocks) == 0 || viewportHeight <= 0 {
		return 0
	}
	lineCounts := make([]int, len(blocks))
	totalLines := 0
	for i, block := range blocks {
		lineCounts[i] = len(splitLines(block))
		totalLines += lineCounts[i]
		if i > 0 {
			totalLines++
		}
	}
	if totalLines <= viewportHeight {
		return 0
	}
	if lineCounts[len(lineCounts)-1] >= viewportHeight {
		return max(0, totalLines-viewportHeight)
	}
	start := len(blocks) - 1
	used := lineCounts[start]
	for start > 0 {
		next := lineCounts[start-1] + 1
		if used+next > viewportHeight {
			break
		}
		start--
		used += next
	}
	offset := 0
	for i := 0; i < start; i++ {
		offset += lineCounts[i]
		if i < len(blocks)-1 {
			offset++
		}
	}
	return offset
}

func shellFooterHint(state shellComposerState) string {
	switch state.SendMode {
	case "worker":
		return "ready"
	case "canonical":
		return "canonical"
	case "scratch":
		return "scratch"
	case "blocked":
		if strings.HasPrefix(state.Status, "worker ") || state.Status == "state may be stale" {
			return state.Status
		}
		return "paused"
	default:
		return "shell"
	}
}

func (m shellModel) helpKeyMap() shellKeyMap {
	keys := m.keys
	state := m.composerState()
	keys.Send.SetEnabled(false)
	keys.Select.SetEnabled(false)
	keys.Dismiss.SetEnabled(false)
	keys.Interrupt.SetEnabled(false)

	switch {
	case m.overlayKind == shellOverlayCommands || m.overlayKind == shellOverlayModel:
		keys.Move.SetEnabled(true)
		keys.Select.SetEnabled(true)
		keys.Dismiss.SetEnabled(true)
	default:
		keys.Move.SetEnabled(false)
		switch state.SendMode {
		case "worker", "canonical", "scratch":
			keys.Send.SetEnabled(true)
		}
		if state.Status == "worker running" && m.hostCanInterrupt() {
			keys.Interrupt.SetEnabled(true)
		}
	}

	return keys
}

func shellFeedNoiseLine(line string) bool {
	switch strings.TrimSpace(line) {
	case "",
		"Input goes directly to the worker.",
		"Live worker input is unavailable in this shell.",
		"Showing bounded transcript evidence in a read-only shell.",
		"No transcript evidence is available yet.":
		return true
	default:
		return false
	}
}

func renderFeedEntry(entry shellFeedEntry, width int) string {
	styles := newShellStyles()
	switch entry.Kind {
	case shellFeedUser:
		return renderUserPromptEntry(styles, entry, width)
	case shellFeedWorking:
		return renderWorkingEntry(styles, entry, width, styles.feedWorkerMeta.Render("•"))
	}
	bodyStyle := styles.feedBody
	borderColor := lipgloss.Color("#374151")
	labelTone := "muted"
	contentWidth := max(8, width-2)
	topPadding := 0
	switch entry.Kind {
	case shellFeedWorker:
		borderColor = lipgloss.Color("#4B5563")
		topPadding = 1
	case shellFeedTuku, shellFeedIntro:
		borderColor = lipgloss.Color("#4B5563")
		topPadding = 1
	case shellFeedWarning:
		borderColor = lipgloss.Color("#6B7280")
		labelTone = "caution"
	case shellFeedError:
		borderColor = lipgloss.Color("#6A3434")
		labelTone = "danger"
	}

	lines := []string{shellToneChip(styles, strings.ToUpper(entry.Title), labelTone)}
	if topPadding > 0 {
		lines = append(lines, "")
	}
	if entry.Kind == shellFeedWorker {
		lines = append(lines, renderWorkerEntryLines(styles, entry.Body, contentWidth)...)
	} else {
		for _, raw := range entry.Body {
			var wrapped []string
			if entry.Preformatted {
				wrapped = wrapOutputLine(raw, contentWidth)
			} else {
				wrapped = wrapText(raw, contentWidth)
			}
			for _, line := range wrapped {
				lines = append(lines, bodyStyle.Render(line))
			}
		}
	}
	return lipgloss.NewStyle().
		BorderLeft(true).
		BorderForeground(borderColor).
		PaddingLeft(1).
		Width(width).
		Render(strings.Join(lines, "\n"))
}

func renderUserPromptEntry(styles shellStyles, entry shellFeedEntry, width int) string {
	contentWidth := max(8, width-4)
	lines := make([]string, 0, len(entry.Body))
	for _, raw := range entry.Body {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		for idx, part := range wrapOutputLine(raw, contentWidth) {
			prefix := "› "
			if idx > 0 {
				prefix = "  "
			}
			lines = append(lines, prefix+part)
		}
	}
	for idx := range lines {
		lines[idx] = ansiPadToWidth(styles.feedUserStrip.Render(lines[idx]), width)
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}

func renderWorkingEntry(styles shellStyles, entry shellFeedEntry, width int, spinnerView string) string {
	if len(entry.Body) == 0 {
		return ""
	}
	line := strings.TrimSpace(entry.Body[0])
	if line == "" {
		return ""
	}
	if strings.TrimSpace(spinnerView) == "" {
		spinnerView = styles.feedWorkerMeta.Render("•")
	}
	rendered := spinnerView + " " + styles.feedWorkerTitle.Render("Working") + styles.feedWorkerMeta.Render(strings.TrimPrefix(line, "Working"))
	return lipgloss.NewStyle().Width(width).Render(rendered)
}

type workerRenderedLineKind int

const (
	workerRenderedBlank workerRenderedLineKind = iota
	workerRenderedParagraph
	workerRenderedHeader
	workerRenderedAction
	workerRenderedDetail
	workerRenderedBullet
)

type workerRenderedLine struct {
	Kind workerRenderedLineKind
	Text string
}

func renderWorkerEntryLines(styles shellStyles, lines []string, width int) []string {
	shaped := shapeWorkerTranscriptLines(lines)
	out := make([]string, 0, len(shaped)+4)
	previousKind := workerRenderedBlank
	for _, line := range shaped {
		if line.Kind == workerRenderedBlank {
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			previousKind = workerRenderedBlank
			continue
		}
		if workerNeedsBreathingRoom(previousKind, line.Kind, len(out)) && out[len(out)-1] != "" {
			out = append(out, "")
		}
		out = append(out, renderWorkerLine(styles, line, width)...)
		previousKind = line.Kind
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return out
}

func shapeWorkerTranscriptLines(lines []string) []workerRenderedLine {
	if len(lines) == 0 {
		return nil
	}
	out := make([]workerRenderedLine, 0, len(lines))
	previousKind := workerRenderedBlank
	for _, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			out = append(out, workerRenderedLine{Kind: workerRenderedBlank})
			previousKind = workerRenderedBlank
			continue
		}
		bulletBody, isBullet := extractBulletText(trimmed)
		switch {
		case (previousKind == workerRenderedAction || previousKind == workerRenderedBullet) && (strings.HasPrefix(raw, "  ") || strings.HasPrefix(raw, "\t")):
			out = append(out, workerRenderedLine{Kind: workerRenderedDetail, Text: trimmed})
			previousKind = workerRenderedDetail
		case isSectionHeaderLine(trimmed):
			out = append(out, workerRenderedLine{Kind: workerRenderedHeader, Text: normalizeSectionHeader(trimmed)})
			previousKind = workerRenderedHeader
		case isActionTranscriptLine(trimmed):
			out = append(out, workerRenderedLine{Kind: workerRenderedAction, Text: trimmed})
			previousKind = workerRenderedAction
		case isBullet:
			out = append(out, workerRenderedLine{Kind: workerRenderedBullet, Text: bulletBody})
			previousKind = workerRenderedBullet
		default:
			out = append(out, workerRenderedLine{Kind: workerRenderedParagraph, Text: trimmed})
			previousKind = workerRenderedParagraph
		}
	}
	return out
}

func renderWorkerLine(styles shellStyles, line workerRenderedLine, width int) []string {
	switch line.Kind {
	case workerRenderedHeader:
		return renderWorkerWrappedLine(styles.feedWorkerTitle.Render(line.Text), width)
	case workerRenderedAction:
		verb, rest := splitWorkerAction(line.Text)
		prefix := styles.feedWorkerMeta.Render("• ") + styles.feedActionVerb.Render(verb) + " "
		return wrapStyledWorkerTextWithPrefix(styles, prefix, lipgloss.Width("• "+verb+" "), rest, width)
	case workerRenderedDetail:
		return wrapStyledWorkerText(styles, "└ ", line.Text, width)
	case workerRenderedBullet:
		return wrapStyledWorkerText(styles, "• ", line.Text, width)
	default:
		return wrapStyledWorkerText(styles, "", line.Text, width)
	}
}

func renderWorkerWrappedLine(line string, width int) []string {
	if width <= 0 {
		return []string{line}
	}
	return []string{ansiTruncate(line, width)}
}

func wrapStyledWorkerText(styles shellStyles, prefix string, text string, width int) []string {
	return wrapStyledWorkerTextWithPrefix(styles, styles.feedBody.Render(prefix), lipgloss.Width(prefix), text, width)
}

func wrapStyledWorkerTextWithPrefix(styles shellStyles, styledPrefix string, prefixWidth int, text string, width int) []string {
	if width <= 0 {
		return []string{styledPrefix + styleFileReferences(styles, text)}
	}
	available := max(1, width-prefixWidth)
	parts := wrapText(text, available)
	out := make([]string, 0, len(parts))
	for idx, part := range parts {
		linePrefix := styledPrefix
		if idx > 0 {
			linePrefix = strings.Repeat(" ", prefixWidth)
		}
		out = append(out, linePrefix+styles.feedBody.Render(styleFileReferences(styles, part)))
	}
	return out
}

func workerNeedsBreathingRoom(previous workerRenderedLineKind, current workerRenderedLineKind, renderedCount int) bool {
	if renderedCount == 0 {
		return false
	}
	if previous == workerRenderedBlank || current == workerRenderedBlank {
		return false
	}
	if current == workerRenderedHeader || previous == workerRenderedHeader {
		return true
	}
	if previous == workerRenderedAction && current == workerRenderedAction {
		return true
	}
	if (previous == workerRenderedParagraph && current != workerRenderedDetail) || (current == workerRenderedParagraph && previous != workerRenderedDetail) {
		return true
	}
	return false
}

func splitWorkerAction(line string) (string, string) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return "", ""
	}
	verb := parts[0]
	if len(parts) == 1 {
		return verb, ""
	}
	return verb, strings.TrimSpace(strings.TrimPrefix(line, verb))
}

func isActionTranscriptLine(line string) bool {
	for _, prefix := range []string{
		"Explored ",
		"Read ",
		"Opened ",
		"Ran ",
		"Waited ",
		"Edited ",
		"Updated ",
		"Wrote ",
		"Searched ",
		"Checked ",
		"Inspected ",
	} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func isSectionHeaderLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, "#") {
		return true
	}
	if strings.HasSuffix(trimmed, ":") && runeLen(trimmed) <= 56 && !strings.Contains(trimmed, ".") {
		return true
	}
	return false
}

func normalizeSectionHeader(line string) string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimLeft(trimmed, "#")
	return strings.TrimSpace(trimmed)
}

func extractBulletText(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(trimmed, "- "):
		return strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")), true
	case strings.HasPrefix(trimmed, "* "):
		return strings.TrimSpace(strings.TrimPrefix(trimmed, "* ")), true
	case strings.HasPrefix(trimmed, "• "):
		return strings.TrimSpace(strings.TrimPrefix(trimmed, "• ")), true
	}
	if len(trimmed) >= 3 && trimmed[0] >= '0' && trimmed[0] <= '9' {
		for idx := 1; idx < len(trimmed); idx++ {
			if trimmed[idx] == '.' && idx+1 < len(trimmed) && trimmed[idx+1] == ' ' {
				return strings.TrimSpace(trimmed[idx+2:]), true
			}
			if trimmed[idx] < '0' || trimmed[idx] > '9' {
				break
			}
		}
	}
	return "", false
}

var workerFileRefPattern = regexp.MustCompile(`^[A-Za-z0-9_./-]+(?::\d+(?::\d+)?)?$`)

func styleFileReferences(styles shellStyles, line string) string {
	if strings.TrimSpace(line) == "" {
		return line
	}
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return line
	}
	styled := make([]string, 0, len(parts))
	for _, token := range parts {
		styled = append(styled, styleFileReferenceToken(styles, token))
	}
	return strings.Join(styled, " ")
}

func styleFileReferenceToken(styles shellStyles, token string) string {
	if token == "" {
		return token
	}
	leading, core, trailing := splitTokenPunctuation(token)
	if !looksLikeFileReference(core) {
		return token
	}
	return leading + styles.feedPath.Render(core) + trailing
}

func splitTokenPunctuation(token string) (string, string, string) {
	start := 0
	for start < len(token) && strings.ContainsRune(`"'([{<`, rune(token[start])) {
		start++
	}
	end := len(token)
	for end > start && strings.ContainsRune(`"'.,;)]}>`, rune(token[end-1])) {
		end--
	}
	return token[:start], token[start:end], token[end:]
}

func looksLikeFileReference(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	if strings.HasPrefix(token, "http://") || strings.HasPrefix(token, "https://") {
		return false
	}
	if !workerFileRefPattern.MatchString(token) {
		return false
	}
	base := token
	if idx := strings.Index(base, ":"); idx >= 0 {
		base = base[:idx]
	}
	if strings.EqualFold(base, "go.mod") || strings.EqualFold(base, "go.sum") || strings.EqualFold(base, "README.md") || strings.EqualFold(base, "AGENTS.md") {
		return true
	}
	if strings.Contains(base, "/") {
		return strings.Contains(filepath.Base(base), ".")
	}
	if strings.HasPrefix(base, "./") || strings.HasPrefix(base, "../") || strings.HasPrefix(base, "/") {
		return true
	}
	return strings.Contains(filepath.Base(base), ".")
}

func overlayWindow(total int, selected int, limit int) (int, int) {
	if total <= 0 {
		return 0, 0
	}
	if limit <= 0 || total <= limit {
		return 0, total
	}
	if selected < 0 {
		selected = 0
	}
	if selected >= total {
		selected = total - 1
	}
	start := selected - (limit / 2)
	if start < 0 {
		start = 0
	}
	end := start + limit
	if end > total {
		end = total
		start = end - limit
	}
	return start, end
}

func coalesceFeedEntries(entries []shellFeedEntry) []shellFeedEntry {
	out := make([]shellFeedEntry, 0, len(entries))
	seen := map[string]struct{}{}
	for _, entry := range entries {
		if entry.Key == "" {
			out = append(out, entry)
			continue
		}
		if _, ok := seen[entry.Key]; ok {
			continue
		}
		seen[entry.Key] = struct{}{}
		out = append(out, entry)
	}
	return out
}

func workerTone(host WorkerHost) string {
	if host == nil {
		return "muted"
	}
	switch host.Status().Mode {
	case HostModeCodexPTY, HostModeClaudePTY:
		return "accent"
	default:
		return "muted"
	}
}

func workerStatusTone(status HostStatus) string {
	switch status.State {
	case HostStateLive:
		if status.InputLive {
			return "positive"
		}
		return "caution"
	case HostStateTranscriptOnly, HostStateFallback, HostStateFailed, HostStateExited:
		return "caution"
	default:
		return "muted"
	}
}

func continuityTone(snapshot Snapshot) string {
	v := strings.ToLower(strings.TrimSpace(continuityLabel(snapshot)))
	switch {
	case strings.Contains(v, "ready"), strings.Contains(v, "clean"), strings.Contains(v, "live"):
		return "positive"
	case strings.Contains(v, "fallback"), strings.Contains(v, "repair"), strings.Contains(v, "handoff"), strings.Contains(v, "recover"), strings.Contains(v, "review"), strings.Contains(v, "triaged"):
		return "caution"
	default:
		return "muted"
	}
}

func overlayOver(base string, overlay string, width int, height int) string {
	baseLines := splitLines(base)
	overlayLines := splitLines(overlay)
	lines := boxOverlay(baseLines, width, height, overlayLines)
	return strings.Join(lines, "\n")
}

func overlayNearComposer(base string, overlay string, width int, height int, layout shellSurfaceLayout) string {
	baseLines := splitLines(base)
	overlayLines := splitLines(overlay)
	if len(overlayLines) == 0 {
		return base
	}
	overlayWidth := 0
	for _, line := range overlayLines {
		overlayWidth = max(overlayWidth, lipgloss.Width(line))
	}
	top := max(layout.headerHeight, height-layout.footerHeight-layout.composerHeight-len(overlayLines)-1)
	left := max(layout.padding+2, 2)
	out := append([]string{}, baseLines...)
	for i := 0; i < len(overlayLines) && top+i < len(out); i++ {
		overlayLine := ansiPadToWidth(overlayLines[i], overlayWidth)
		out[top+i] = ansiPadToWidth(strings.Repeat(" ", left)+overlayLine, width)
	}
	return strings.Join(out, "\n")
}

func indentBlock(block string, padding int) string {
	if padding <= 0 {
		return block
	}
	prefix := strings.Repeat(" ", padding)
	lines := splitLines(block)
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func shellHostChanged(previous HostStatus, current HostStatus) bool {
	if previous.RenderVersion != current.RenderVersion {
		return true
	}
	if previous.LastActivityAt != current.LastActivityAt {
		return true
	}
	if previous.LastOutputAt != current.LastOutputAt {
		return true
	}
	return false
}

func shellHostDigest(host WorkerHost, width int) uint64 {
	if host == nil {
		return 0
	}
	layoutWidth := max(12, width)
	lines := shellHistoryLines(host, layoutWidth)
	if len(lines) > 80 {
		lines = lines[len(lines)-80:]
	}
	var digest uint64
	for _, line := range lines {
		for _, r := range line {
			digest = digest*1099511628211 + uint64(r)
		}
		digest = digest*1099511628211 + 10
	}
	return digest
}

func currentPrimaryStep(snapshot Snapshot) *OperatorExecutionStep {
	if snapshot.OperatorExecutionPlan == nil {
		return nil
	}
	return snapshot.OperatorExecutionPlan.PrimaryStep
}

func executablePrimaryStepExists(snapshot Snapshot, executor PrimaryActionExecutor) error {
	if executor == nil {
		return fmt.Errorf("no primary action executor is configured for this shell")
	}
	_, err := executablePrimaryStep(snapshot)
	return err
}

func shellSendPromptCmd(sender TaskMessageSender, source SnapshotSource, taskID string, prompt string, registrySource SessionRegistrySource) tea.Cmd {
	return func() tea.Msg {
		if err := sender.Send(taskID, prompt); err != nil {
			return shellMessageSentMsg{prompt: prompt, err: err}
		}
		next, err := source.Load(taskID)
		if err != nil {
			return shellMessageSentMsg{prompt: prompt, err: fmt.Errorf("task message sent, but shell refresh failed: %w", err)}
		}
		var sessions []KnownShellSession
		if registrySource != nil {
			sessions, err = registrySource.List(taskID)
			if err != nil {
				return shellMessageSentMsg{prompt: prompt, err: fmt.Errorf("shell session registry read failed: %w", err)}
			}
		}
		return shellMessageSentMsg{prompt: prompt, snapshot: next, sessions: sessions}
	}
}

func shellLoadSnapshotCmd(source SnapshotSource, taskID string, registrySource SessionRegistrySource) tea.Cmd {
	return func() tea.Msg {
		next, err := source.Load(taskID)
		if err != nil {
			return shellSnapshotLoadedMsg{err: err}
		}
		var sessions []KnownShellSession
		if registrySource != nil {
			sessions, err = registrySource.List(taskID)
			if err != nil {
				return shellSnapshotLoadedMsg{err: fmt.Errorf("shell session registry read failed: %w", err)}
			}
		}
		return shellSnapshotLoadedMsg{snapshot: next, sessions: sessions}
	}
}

func shellExecutePrimaryActionCmd(executor PrimaryActionExecutor, taskID string, snapshot Snapshot, ui *UIState) tea.Cmd {
	step, err := executablePrimaryStep(snapshot)
	if err != nil {
		return func() tea.Msg {
			return shellPrimaryActionDoneMsg{
				result: primaryActionExecutionResult{
					step:       OperatorExecutionStep{Action: "UNKNOWN"},
					before:     snapshot,
					err:        err,
					finishedAt: time.Now().UTC(),
				},
			}
		}
	}
	now := time.Now().UTC()
	if ui != nil {
		ui.PrimaryActionInFlight = &PrimaryActionInFlightSummary{
			Action:    step.Action,
			StartedAt: now,
		}
		addSessionEvent(&ui.Session, now, SessionEventPrimaryOperatorActionStarted, fmt.Sprintf("Executing primary operator step %s through Tuku control-plane IPC.", strings.ToLower(step.Action)))
	}
	before := snapshot
	return func() tea.Msg {
		outcome, execErr := executor.Execute(taskID, snapshot)
		return shellPrimaryActionDoneMsg{
			result: primaryActionExecutionResult{
				outcome:    outcome,
				step:       *step,
				before:     before,
				err:        execErr,
				finishedAt: time.Now().UTC(),
			},
		}
	}
}

func shellHostTickCmd() tea.Cmd {
	return tea.Tick(shellHostTickInterval, func(time.Time) tea.Msg { return shellHostTickMsg{} })
}

func shellSnapshotTickCmd(interval time.Duration) tea.Cmd {
	if interval <= 0 {
		interval = shellSnapshotTickInterval
	}
	return tea.Tick(interval, func(time.Time) tea.Msg { return shellSnapshotTickMsg{} })
}

func shellRegistryTickCmd() tea.Cmd {
	return tea.Tick(shellRegistryTickInterval, func(time.Time) tea.Msg { return shellRegistryTickMsg{} })
}

func shellWorkingTickCmd() tea.Cmd {
	return tea.Tick(shellWorkingTickInterval, func(time.Time) tea.Msg { return shellWorkingTickMsg{} })
}

func shellTranscriptTickCmd() tea.Cmd {
	return tea.Tick(shellTranscriptTickInterval, func(time.Time) tea.Msg { return shellTranscriptTickMsg{} })
}

func (a *App) refreshEvery() time.Duration {
	if a.RefreshInterval > 0 {
		return a.RefreshInterval
	}
	return shellSnapshotTickInterval
}

func (m *shellModel) pushUserPrompt(prompt string) {
	m.appendLocalEntry(shellFeedEntry{
		Key:   fmt.Sprintf("local-user-%d", time.Now().UTC().UnixNano()),
		Kind:  shellFeedUser,
		Title: "You",
		Body:  []string{prompt},
	})
}

func (m *shellModel) pushNote(title string, lines []string, preformatted bool) {
	m.appendLocalEntry(shellFeedEntry{
		Key:          fmt.Sprintf("local-note-%d", time.Now().UTC().UnixNano()),
		Kind:         shellFeedTuku,
		Title:        title,
		Body:         append([]string{}, lines...),
		Preformatted: preformatted,
	})
}

func (m *shellModel) pushWarning(title string, lines []string) {
	m.appendLocalEntry(shellFeedEntry{
		Key:   fmt.Sprintf("local-warning-%d", time.Now().UTC().UnixNano()),
		Kind:  shellFeedWarning,
		Title: title,
		Body:  append([]string{}, lines...),
	})
}

func (m *shellModel) pushError(title string, detail string) {
	m.appendLocalEntry(shellFeedEntry{
		Key:   fmt.Sprintf("local-error-%d", time.Now().UTC().UnixNano()),
		Kind:  shellFeedError,
		Title: title,
		Body:  []string{detail},
	})
}

func (m *shellModel) appendLocalEntry(entry shellFeedEntry) {
	m.localEntries = append(m.localEntries, entry)
	if entry.Key != "" && m.followLatest {
		m.scrollToEntry = entry.Key
	}
}

func (m *shellModel) removeLocalUserPrompt(prompt string) {
	trimmed := strings.TrimSpace(prompt)
	filtered := m.localEntries[:0]
	for _, entry := range m.localEntries {
		if entry.Kind == shellFeedUser && len(entry.Body) == 1 && strings.TrimSpace(entry.Body[0]) == trimmed {
			continue
		}
		filtered = append(filtered, entry)
	}
	m.localEntries = filtered
}

func RenderPreview(snapshot Snapshot, ui UIState, host WorkerHost, width int, height int) string {
	if host == nil {
		host = NewTranscriptHost()
		host.UpdateSnapshot(snapshot)
	}
	model := newShellModel(context.Background(), &App{}, snapshot, ui, host)
	model.width = width
	model.height = height
	model.resize()
	return model.View()
}

func (m *shellModel) exitConfirmActive() bool {
	return !m.exitConfirmUntil.IsZero() && time.Now().UTC().Before(m.exitConfirmUntil)
}

type interruptibleWorkerHost interface {
	CanInterrupt() bool
	Interrupt() bool
}

type authoritativeWorkerTurnHost interface {
	WorkerTurnActive() bool
}

func (m *shellModel) hostCanInterrupt() bool {
	host, ok := m.host.(interruptibleWorkerHost)
	return ok && host.CanInterrupt()
}

func (m *shellModel) requestWorkerInterrupt() bool {
	host, ok := m.host.(interruptibleWorkerHost)
	if !ok || !host.CanInterrupt() {
		return false
	}
	if !host.Interrupt() {
		return false
	}
	m.ui.WorkerInterruptRequested = true
	m.pushWarning("Interrupt", []string{
		"Interrupt signal sent to the live worker.",
		"Tuku will only clear the running state when the host/runtime visibly settles.",
	})
	return true
}

func (m *shellModel) workerTurnStillActive(status HostStatus) (bool, string) {
	if !m.ui.WorkerPromptPending {
		return false, ""
	}
	if host, ok := m.host.(authoritativeWorkerTurnHost); ok {
		return host.WorkerTurnActive(), ""
	}
	assessment := assessWorkerTurn(status, m.ui, m.hostCanInterrupt(), false, time.Now().UTC())
	return assessment.Active, assessment.ClearNote
}

func (m *shellModel) clearWorkerTurnState() {
	if m.ui.WorkerPromptPending {
		m.commitCurrentWorkerStream()
	}
	m.ui.WorkerPromptPending = false
	m.ui.WorkerResponseStarted = false
	m.ui.WorkerInterruptRequested = false
	m.spinnerRunning = false
}

func (m *shellModel) syncFollowLatest() {
	m.followLatest = m.viewport.AtBottom()
}

func (m *shellModel) spinnerActive() bool {
	return m.ui.WorkerPromptPending || m.ui.PrimaryActionInFlight != nil
}

func (m *shellModel) shouldRefreshTransientViewport() bool {
	if m.followLatest || m.viewport.AtBottom() {
		return true
	}
	return m.viewport.TotalLineCount() <= m.viewport.Height
}

func (m *shellModel) workerTurnAuthoritative() bool {
	host, ok := m.host.(authoritativeWorkerTurnHost)
	return ok && host.WorkerTurnActive()
}

func (m *shellModel) ensureSpinnerTick() tea.Cmd {
	if !m.spinnerActive() || m.spinnerRunning {
		return nil
	}
	m.spinnerRunning = true
	return m.spinner.Tick
}

func formatShellElapsed(elapsed time.Duration) string {
	if elapsed < 0 {
		elapsed = 0
	}
	totalSeconds := int(elapsed / time.Second)
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	switch {
	case hours > 0:
		return fmt.Sprintf("%dh %02dm %02ds", hours, minutes, seconds)
	case minutes > 0:
		return fmt.Sprintf("%dm %02ds", minutes, seconds)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}

func (m *shellModel) basePromptEntries() []shellFeedEntry {
	entries := append([]shellFeedEntry{}, shellConversationEntries(m.snapshot)...)
	entries = append(entries, m.localEntries...)
	return entries
}

func trimCommittedHostLines(lines []string, committed []string) []string {
	if len(lines) == 0 {
		return nil
	}
	if len(committed) == 0 {
		return append([]string{}, lines...)
	}
	prefix := 0
	for prefix < len(lines) && prefix < len(committed) && lines[prefix] == committed[prefix] {
		prefix++
	}
	if prefix < len(committed) {
		return append([]string{}, lines...)
	}
	return append([]string{}, lines[prefix:]...)
}

func (m *shellModel) commitCurrentWorkerStream() {
	if m.host == nil || m.host.Status().Mode == HostModeTranscript {
		return
	}
	lines := curateWorkerLines(shellHistoryLines(m.host, max(10, m.layout().contentWidth-4)), shellPromptBodies(m.basePromptEntries()))
	if len(lines) == 0 {
		m.archivedHostLines = nil
		return
	}
	tail := trimCommittedHostLines(lines, m.archivedHostLines)
	if hasMeaningfulWorkerLines(tail) {
		m.appendLocalEntry(shellFeedEntry{
			Key:          fmt.Sprintf("local-worker-%d", time.Now().UTC().UnixNano()),
			Kind:         shellFeedWorker,
			Title:        "Worker",
			Body:         tail,
			Preformatted: true,
		})
	}
	m.archivedHostLines = append([]string{}, lines...)
}
