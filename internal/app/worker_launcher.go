package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tuku/internal/domain/provider"
	tukushell "tuku/internal/tui/shell"
)

var errPrimaryWorkerSelectionCancelled = errors.New("primary worker selection cancelled")

func clearPrimaryLauncherSurface(out io.Writer) error {
	if out == nil {
		return nil
	}
	_, err := io.WriteString(out, "\x1b[2J\x1b[H")
	return err
}

type primaryWorkerPreferenceFile struct {
	LastWorker string `json:"last_worker,omitempty"`
}

type primaryWorkerOption struct {
	Preference  tukushell.WorkerPreference
	Title       string
	Summary     string
	Recommended bool
	Prereq      tukushell.WorkerPrerequisite
}

type primaryWorkerLauncherMode string

const (
	primaryWorkerLauncherModeSelect primaryWorkerLauncherMode = "select"
	primaryWorkerLauncherModeSetup  primaryWorkerLauncherMode = "setup"
)

type primaryWorkerSetupActionKind string

const (
	primaryWorkerSetupActionInstall primaryWorkerSetupActionKind = "install"
	primaryWorkerSetupActionLogin   primaryWorkerSetupActionKind = "login"
)

type primaryWorkerSetupAction struct {
	Preference tukushell.WorkerPreference
	Kind       primaryWorkerSetupActionKind
}

type primaryWorkerSetupOption struct {
	Action  primaryWorkerSetupAction
	Title   string
	Summary string
}

type primaryWorkerLauncherModel struct {
	width         int
	height        int
	selected      int
	setupSelected int
	options       []primaryWorkerOption
	selection     primaryWorkerSelectionContext
	mode          primaryWorkerLauncherMode
	setup         *primaryWorkerOption
	action        *primaryWorkerSetupAction
	cancelled     bool
}

func runPrimaryWorkerLauncher(ctx context.Context, selection primaryWorkerSelectionContext) (tukushell.WorkerPreference, error) {
	for {
		selection = enrichPrimaryWorkerSelectionContext(selection)
		model := newPrimaryWorkerLauncherModel(selection)
		finalModel, err := tea.NewProgram(
			model,
			tea.WithContext(ctx),
		).Run()
		if err != nil {
			return "", err
		}
		launcher, ok := finalModel.(primaryWorkerLauncherModel)
		if !ok {
			return "", errors.New("worker launcher did not return final state")
		}
		if launcher.cancelled {
			return "", errPrimaryWorkerSelectionCancelled
		}
		if launcher.action == nil {
			return launcher.options[launcher.selected].Preference, nil
		}
		if err := clearPrimaryLauncherSurface(os.Stdout); err != nil {
			return "", err
		}
		notice, actionErr := runPrimaryWorkerSetupAction(ctx, *launcher.action)
		selection.Notice = strings.TrimSpace(notice)
		selection.Preferred = launcher.action.Preference
		updated := detectPrimaryWorkerPrereq(launcher.action.Preference)
		if selection.Prerequisites == nil {
			selection.Prerequisites = map[tukushell.WorkerPreference]tukushell.WorkerPrerequisite{}
		}
		selection.Prerequisites[launcher.action.Preference] = updated
		if actionErr != nil {
			if selection.Notice == "" {
				selection.Notice = actionErr.Error()
			}
			continue
		}
		if updated.Ready {
			return launcher.action.Preference, nil
		}
		if selection.Notice == "" {
			selection.Notice = updated.Detail
		}
	}
}

func newPrimaryWorkerLauncherModel(selection primaryWorkerSelectionContext) primaryWorkerLauncherModel {
	selected := 0
	preferred := selection.Preferred
	if preferred == "" || preferred == tukushell.WorkerPreferenceAuto {
		preferred = selection.Remembered
	}
	if preferred == tukushell.WorkerPreferenceClaude {
		selected = 1
	} else if preferred == tukushell.WorkerPreferenceAuto && selection.Recommendation.Worker == provider.WorkerClaude {
		selected = 1
	}
	candidates := selection.Recommendation.Candidates
	if len(candidates) == 0 {
		candidates = provider.Registry()
	}
	options := make([]primaryWorkerOption, 0, len(candidates))
	for _, candidate := range candidates {
		preference := tukushell.WorkerPreferenceCodex
		switch candidate.Worker {
		case provider.WorkerClaude:
			preference = tukushell.WorkerPreferenceClaude
		case provider.WorkerCodex:
			preference = tukushell.WorkerPreferenceCodex
		default:
			continue
		}
		prereq := selection.Prerequisites[preference]
		options = append(options, primaryWorkerOption{
			Preference:  preference,
			Title:       "Launch with " + provider.Label(candidate.Worker),
			Summary:     strings.TrimSpace(candidate.Summary),
			Recommended: candidate.Worker == selection.Recommendation.Worker,
			Prereq:      prereq,
		})
	}
	if len(options) == 0 {
		options = []primaryWorkerOption{
			{
				Preference: tukushell.WorkerPreferenceCodex,
				Title:      "Launch with Codex",
				Prereq:     selection.Prerequisites[tukushell.WorkerPreferenceCodex],
			},
			{
				Preference: tukushell.WorkerPreferenceClaude,
				Title:      "Launch with Claude",
				Prereq:     selection.Prerequisites[tukushell.WorkerPreferenceClaude],
			},
		}
	}
	if preferred == tukushell.WorkerPreferenceClaude {
		for idx, option := range options {
			if option.Preference == tukushell.WorkerPreferenceClaude {
				selected = idx
				break
			}
		}
	} else if preferred == tukushell.WorkerPreferenceCodex {
		for idx, option := range options {
			if option.Preference == tukushell.WorkerPreferenceCodex {
				selected = idx
				break
			}
		}
	}
	return primaryWorkerLauncherModel{
		selected:  selected,
		options:   options,
		selection: selection,
		mode:      primaryWorkerLauncherModeSelect,
	}
}

func (m primaryWorkerLauncherModel) Init() tea.Cmd {
	return nil
}

func (m primaryWorkerLauncherModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.cancelled = true
			return m, tea.Quit
		case "up", "shift+tab":
			if m.mode == primaryWorkerLauncherModeSetup {
				if m.setupSelected > 0 {
					m.setupSelected--
				}
				return m, nil
			}
			if m.selected > 0 {
				m.selected--
			}
			return m, nil
		case "down", "tab":
			if m.mode == primaryWorkerLauncherModeSetup {
				if m.setupSelected < len(m.setupOptions())-1 {
					m.setupSelected++
				}
				return m, nil
			}
			if m.selected < len(m.options)-1 {
				m.selected++
			}
			return m, nil
		case "esc":
			if m.mode == primaryWorkerLauncherModeSetup {
				m.mode = primaryWorkerLauncherModeSelect
				m.setup = nil
				m.setupSelected = 0
				return m, nil
			}
			m.cancelled = true
			return m, tea.Quit
		case "enter":
			if m.mode == primaryWorkerLauncherModeSetup {
				options := m.setupOptions()
				if len(options) == 0 {
					m.mode = primaryWorkerLauncherModeSelect
					m.setup = nil
					m.setupSelected = 0
					return m, nil
				}
				choice := options[m.setupSelected]
				if choice.Action.Kind == "" {
					m.mode = primaryWorkerLauncherModeSelect
					m.setup = nil
					m.setupSelected = 0
					return m, nil
				}
				action := choice.Action
				m.action = &action
				return m, tea.Quit
			}
			selected := m.options[m.selected]
			if selected.Prereq.Ready {
				return m, tea.Quit
			}
			m.mode = primaryWorkerLauncherModeSetup
			m.setup = &selected
			m.setupSelected = 0
			return m, nil
		}
	}
	return m, nil
}

func (m primaryWorkerLauncherModel) View() string {
	if m.mode == primaryWorkerLauncherModeSetup {
		return m.setupView()
	}
	return m.selectionView()
}

func (m primaryWorkerLauncherModel) selectionView() string {
	layout := primaryWorkerSurfaceLayoutForWidth(m.width)
	kickerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F9FAFB")).Bold(true)
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F3F4F6")).Bold(true)
	subtitleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF"))
	sectionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Bold(true)
	optionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB"))
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F9FAFB")).Bold(true)
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF"))
	recommended := lipgloss.NewStyle().Foreground(lipgloss.Color("#86EFAC")).Bold(true)
	info := lipgloss.NewStyle().Foreground(lipgloss.Color("#FDE68A")).Bold(true)
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))

	lines := []string{
		"",
		kickerStyle.Render("TUKU"),
		titleStyle.Render("Choose a worker for this session"),
		subtitleStyle.Render("Pick one to open the shell."),
	}
	if notice := strings.TrimSpace(m.selection.Notice); notice != "" {
		lines = append(lines, "")
		lines = append(lines, renderPrimaryWorkerParagraph(info, notice, layout.contentWidth, "")...)
	}
	if m.selection.Recommendation.Worker != "" && m.selection.Recommendation.Worker != provider.WorkerUnknown {
		lines = append(lines, "")
		lines = append(lines, sectionStyle.Render("Recommendation"))
		lines = append(lines, recommended.Render(provider.Label(m.selection.Recommendation.Worker)+" ("+nonEmpty(strings.TrimSpace(m.selection.Recommendation.Confidence), "medium")+" confidence)"))
		if reason := strings.TrimSpace(m.selection.Recommendation.Reason); reason != "" {
			lines = append(lines, renderPrimaryWorkerParagraph(muted, "Why: "+reason, layout.contentWidth, "  ")...)
		}
	}
	lines = append(lines, "", sectionStyle.Render("Workers"))
	for idx, item := range m.options {
		if idx > 0 {
			lines = append(lines, "")
		}
		row := item.Title
		if item.Recommended {
			row += " [recommended]"
		}
		if idx == m.selected {
			lines = append(lines, selectedStyle.Render("› "+strings.TrimSpace(row)))
		} else {
			lines = append(lines, optionStyle.Render("  "+strings.TrimSpace(row)))
		}
		if summary := strings.TrimSpace(item.Summary); summary != "" {
			lines = append(lines, renderPrimaryWorkerParagraph(muted, summary, layout.contentWidth, "    ")...)
		}
		if statusLine, statusStyle := renderPrimaryWorkerPrereq(item.Prereq); statusLine != "" {
			lines = append(lines, renderPrimaryWorkerParagraph(statusStyle, statusLine, layout.contentWidth, "    ")...)
		}
	}
	lines = append(lines, "", hintStyle.Render("↑↓ move • Enter select • Esc cancel"))
	return primaryWorkerSurfaceFrame(layout, strings.Join(lines, "\n"))
}

func (m primaryWorkerLauncherModel) setupView() string {
	layout := primaryWorkerSurfaceLayoutForWidth(m.width)
	kicker := lipgloss.NewStyle().Foreground(lipgloss.Color("#F9FAFB")).Bold(true)
	title := lipgloss.NewStyle().Foreground(lipgloss.Color("#F3F4F6")).Bold(true)
	section := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Bold(true)
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF"))
	selected := lipgloss.NewStyle().Foreground(lipgloss.Color("#F9FAFB")).Bold(true)
	option := lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB"))
	info := lipgloss.NewStyle().Foreground(lipgloss.Color("#FDE68A")).Bold(true)
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))

	setup := m.activeSetupOption()
	if setup == nil {
		return m.selectionView()
	}

	lines := []string{
		"",
		kicker.Render("TUKU"),
		title.Render(setup.Prereq.WorkerLabel + " needs a quick setup"),
	}
	if summary := strings.TrimSpace(setup.Prereq.Summary); summary != "" {
		lines = append(lines, renderPrimaryWorkerParagraph(muted, summary, layout.contentWidth, "")...)
	}
	if detail := strings.TrimSpace(setup.Prereq.Detail); detail != "" {
		lines = append(lines, "")
		lines = append(lines, renderPrimaryWorkerParagraph(muted, detail, layout.contentWidth, "")...)
	}
	if notice := strings.TrimSpace(m.selection.Notice); notice != "" {
		lines = append(lines, "")
		lines = append(lines, renderPrimaryWorkerParagraph(info, notice, layout.contentWidth, "")...)
	}
	if len(setup.Prereq.InstallCommand) > 0 && setup.Prereq.State == tukushell.WorkerPrerequisiteMissingBinary {
		lines = append(lines, "")
		lines = append(lines, section.Render("Setup"))
		lines = append(lines, renderPrimaryWorkerParagraph(muted, "Install command: "+strings.Join(setup.Prereq.InstallCommand, " "), layout.contentWidth, "  ")...)
	}
	if len(setup.Prereq.LoginCommand) > 0 && setup.Prereq.State == tukushell.WorkerPrerequisiteUnauthenticated {
		lines = append(lines, "")
		lines = append(lines, section.Render("Setup"))
		lines = append(lines, renderPrimaryWorkerParagraph(muted, "Login command: "+strings.Join(setup.Prereq.LoginCommand, " "), layout.contentWidth, "  ")...)
	}
	lines = append(lines, "", section.Render("Actions"))
	for idx, action := range m.setupOptions() {
		if idx > 0 {
			lines = append(lines, "")
		}
		row := action.Title
		if idx == m.setupSelected {
			lines = append(lines, selected.Render("› "+row))
		} else {
			lines = append(lines, option.Render("  "+row))
		}
		if summary := strings.TrimSpace(action.Summary); summary != "" {
			lines = append(lines, renderPrimaryWorkerParagraph(muted, summary, layout.contentWidth, "    ")...)
		}
	}
	lines = append(lines, "", hint.Render("↑↓ move • Enter select • Esc back"))
	return primaryWorkerSurfaceFrame(layout, strings.Join(lines, "\n"))
}

func (m primaryWorkerLauncherModel) activeSetupOption() *primaryWorkerOption {
	if m.setup != nil {
		return m.setup
	}
	if m.selected < 0 || m.selected >= len(m.options) {
		return nil
	}
	selected := m.options[m.selected]
	return &selected
}

func (m primaryWorkerLauncherModel) setupOptions() []primaryWorkerSetupOption {
	setup := m.activeSetupOption()
	if setup == nil {
		return nil
	}
	switch setup.Prereq.State {
	case tukushell.WorkerPrerequisiteMissingBinary:
		return []primaryWorkerSetupOption{
			{
				Action: primaryWorkerSetupAction{Preference: setup.Preference, Kind: primaryWorkerSetupActionInstall},
				Title:  "Install " + setup.Prereq.WorkerLabel + " now",
				Summary: fmt.Sprintf(
					"Tuku will run `%s` in this terminal.",
					strings.Join(setup.Prereq.InstallCommand, " "),
				),
			},
			{Title: "Back to worker selection", Summary: "Choose a different worker or return later."},
		}
	case tukushell.WorkerPrerequisiteUnauthenticated:
		return []primaryWorkerSetupOption{
			{
				Action: primaryWorkerSetupAction{Preference: setup.Preference, Kind: primaryWorkerSetupActionLogin},
				Title:  "Sign in to " + setup.Prereq.WorkerLabel,
				Summary: fmt.Sprintf(
					"Tuku will run `%s` in this terminal.",
					strings.Join(setup.Prereq.LoginCommand, " "),
				),
			},
			{Title: "Back to worker selection", Summary: "Choose a different worker or return later."},
		}
	default:
		return []primaryWorkerSetupOption{{Title: "Back to worker selection", Summary: "Return to the worker picker."}}
	}
}

func renderPrimaryWorkerPrereq(prereq tukushell.WorkerPrerequisite) (string, lipgloss.Style) {
	ok := lipgloss.NewStyle().Foreground(lipgloss.Color("#86EFAC"))
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color("#FDE68A"))
	alert := lipgloss.NewStyle().Foreground(lipgloss.Color("#FCA5A5"))
	switch prereq.State {
	case tukushell.WorkerPrerequisiteReady:
		return "Ready: installed and signed in", ok
	case tukushell.WorkerPrerequisiteMissingBinary:
		return "Setup needed: not installed yet", alert
	case tukushell.WorkerPrerequisiteUnauthenticated:
		return "Setup needed: installed, sign-in required", warn
	default:
		if strings.TrimSpace(prereq.Summary) == "" {
			return "", warn
		}
		return strings.TrimSpace(prereq.Summary), warn
	}
}

type primaryWorkerSurfaceLayout struct {
	leftPad      int
	contentWidth int
}

func primaryWorkerSurfaceLayoutForWidth(width int) primaryWorkerSurfaceLayout {
	if width <= 0 {
		width = 80
	}
	contentWidth := min(96, width-4)
	if width >= 72 {
		contentWidth = min(96, width-6)
	}
	if contentWidth < 36 {
		contentWidth = max(24, width-2)
	}
	leftPad := max(1, (width-contentWidth)/2)
	return primaryWorkerSurfaceLayout{
		leftPad:      leftPad,
		contentWidth: contentWidth,
	}
}

func primaryWorkerSurfaceFrame(layout primaryWorkerSurfaceLayout, body string) string {
	return lipgloss.NewStyle().PaddingLeft(layout.leftPad).Render(body)
}

func renderPrimaryWorkerParagraph(style lipgloss.Style, text string, width int, indent string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	usableWidth := width - lipgloss.Width(indent)
	if usableWidth < 12 {
		usableWidth = max(8, width)
	}
	wrapped := primaryWorkerWrapText(text, usableWidth)
	lines := make([]string, 0, len(wrapped))
	for _, line := range wrapped {
		lines = append(lines, style.Render(indent+line))
	}
	return lines
}

func primaryWorkerWrapText(text string, width int) []string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if text == "" {
		return nil
	}
	if width <= 1 {
		return []string{text}
	}
	words := strings.Fields(text)
	lines := make([]string, 0, len(words))
	current := ""
	appendWord := func(word string) {
		if lipgloss.Width(word) <= width {
			current = word
			return
		}
		chunks := primaryWorkerSplitLongWord(word, width)
		if len(chunks) == 0 {
			return
		}
		lines = append(lines, chunks[:len(chunks)-1]...)
		current = chunks[len(chunks)-1]
	}
	for _, word := range words {
		if current == "" {
			appendWord(word)
			continue
		}
		candidate := current + " " + word
		if lipgloss.Width(candidate) <= width {
			current = candidate
			continue
		}
		lines = append(lines, current)
		current = ""
		appendWord(word)
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func primaryWorkerSplitLongWord(word string, width int) []string {
	if word == "" {
		return nil
	}
	if width <= 1 {
		return []string{word}
	}
	runes := []rune(word)
	lines := make([]string, 0, (len(runes)/width)+1)
	for len(runes) > 0 {
		chunkWidth := min(width, len(runes))
		lines = append(lines, string(runes[:chunkWidth]))
		runes = runes[chunkWidth:]
	}
	return lines
}

func enrichPrimaryWorkerSelectionContext(selection primaryWorkerSelectionContext) primaryWorkerSelectionContext {
	if selection.Prerequisites == nil {
		selection.Prerequisites = map[tukushell.WorkerPreference]tukushell.WorkerPrerequisite{}
	}
	for _, preference := range []tukushell.WorkerPreference{tukushell.WorkerPreferenceCodex, tukushell.WorkerPreferenceClaude} {
		selection.Prerequisites[preference] = detectPrimaryWorkerPrereq(preference)
	}
	return selection
}

func runPrimaryWorkerSetupAction(ctx context.Context, action primaryWorkerSetupAction) (string, error) {
	prereq := detectPrimaryWorkerPrereq(action.Preference)
	command := []string{}
	label := strings.TrimSpace(prereq.WorkerLabel)
	if label == "" {
		label = provider.Label(provider.WorkerKind(action.Preference))
	}
	switch action.Kind {
	case primaryWorkerSetupActionInstall:
		command = append([]string{}, prereq.InstallCommand...)
	case primaryWorkerSetupActionLogin:
		command = append([]string{}, prereq.LoginCommand...)
	default:
		return "", fmt.Errorf("unsupported worker setup action %q", action.Kind)
	}
	if len(command) == 0 {
		return "", fmt.Errorf("no setup command is available for %s", label)
	}
	message := fmt.Sprintf("[tuku] %s setup: %s\n", label, strings.Join(command, " "))
	if _, err := io.WriteString(os.Stdout, message); err != nil {
		return "", err
	}
	cmd := workerSetupCommandContext(ctx, command[0], command[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Sprintf("%s setup did not complete yet.", label), err
	}
	switch action.Kind {
	case primaryWorkerSetupActionInstall:
		return fmt.Sprintf("%s installed. Tuku is checking whether sign-in is still needed.", label), nil
	case primaryWorkerSetupActionLogin:
		return fmt.Sprintf("%s sign-in finished. Tuku is verifying the session.", label), nil
	default:
		return "", nil
	}
}

var (
	detectPrimaryWorkerPrereq = tukushell.DetectWorkerPrerequisite
	workerSetupCommandContext = exec.CommandContext
)

func primaryWorkerPreferencePath() (string, error) {
	root, err := defaultDataRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "primary-worker.json"), nil
}

func loadPrimaryWorkerPreference() (tukushell.WorkerPreference, error) {
	path, err := primaryWorkerPreferencePath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return tukushell.WorkerPreferenceAuto, nil
		}
		return "", err
	}
	var prefs primaryWorkerPreferenceFile
	if err := json.Unmarshal(data, &prefs); err != nil {
		return "", err
	}
	preference, err := tukushell.ParseWorkerPreference(prefs.LastWorker)
	if err != nil || preference == tukushell.WorkerPreferenceAuto {
		return tukushell.WorkerPreferenceAuto, nil
	}
	return preference, nil
}

func savePrimaryWorkerPreference(preference tukushell.WorkerPreference) error {
	if preference != tukushell.WorkerPreferenceCodex && preference != tukushell.WorkerPreferenceClaude {
		return nil
	}
	path, err := primaryWorkerPreferencePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload, err := json.Marshal(primaryWorkerPreferenceFile{LastWorker: string(preference)})
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o644)
}
