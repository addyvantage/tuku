package bubble

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type DispatchResult struct {
	CanonicalResponse string
	Phase             string
	RunID             string
	RunStatus         string
}

type Config struct {
	Title    string
	TaskID   string
	RepoRoot string
	Worker   string
	Send     func(ctx context.Context, prompt string) (DispatchResult, error)
}

type sendFinishedMsg struct {
	result  DispatchResult
	err     error
	elapsed time.Duration
}

type model struct {
	ctx context.Context
	cfg Config

	width  int
	height int

	input    textinput.Model
	activity viewport.Model

	lines      []string
	sending    bool
	statusText string
}

var (
	bgStyle = lipgloss.NewStyle().Background(lipgloss.Color("#0B0E14")).Foreground(lipgloss.Color("#D9E1EE"))

	headerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F5A97F")).
			Bold(true)

	subtleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8993A4"))

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#2C3442")).
			Padding(1, 2)

	sidePanelStyle = panelStyle.Copy().Width(34)
)

func Run(ctx context.Context, cfg Config) error {
	if strings.TrimSpace(cfg.Title) == "" {
		cfg.Title = "Tuku"
	}
	if strings.TrimSpace(cfg.Worker) == "" {
		cfg.Worker = "auto"
	}
	if cfg.Send == nil {
		return fmt.Errorf("bubble ui send callback is required")
	}

	ti := textinput.New()
	ti.Placeholder = "Describe what you want to build..."
	ti.Prompt = "> "
	ti.CharLimit = 1200
	ti.Focus()

	vp := viewport.New(80, 20)

	m := model{
		ctx:        ctx,
		cfg:        cfg,
		input:      ti,
		activity:   vp,
		statusText: "ready",
		lines: []string{
			"Welcome to Tuku UI.",
			"Type your prompt and press Enter. Ctrl+C exits.",
		},
	}
	m.syncActivity()

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "ctrl+l":
			m.lines = nil
			m.syncActivity()
			return m, nil
		case "enter":
			if m.sending {
				return m, nil
			}
			prompt := strings.TrimSpace(m.input.Value())
			if prompt == "" {
				return m, nil
			}
			m.sending = true
			m.statusText = "dispatching..."
			m.lines = append(m.lines, "prompt> "+prompt)
			m.input.SetValue("")
			m.syncActivity()
			return m, sendCmd(m.ctx, m.cfg.Send, prompt)
		}
	case sendFinishedMsg:
		m.sending = false
		if msg.err != nil {
			m.lines = append(m.lines, "error: "+msg.err.Error())
			m.statusText = "last run failed"
			m.syncActivity()
			return m, nil
		}

		m.lines = append(m.lines, fmt.Sprintf("run ok in %s | run=%s status=%s phase=%s", msg.elapsed.Truncate(time.Millisecond), nonEmpty(msg.result.RunID, "n/a"), nonEmpty(msg.result.RunStatus, "unknown"), nonEmpty(msg.result.Phase, "unknown")))
		if summary := strings.TrimSpace(msg.result.CanonicalResponse); summary != "" {
			m.lines = append(m.lines, "tuku> "+summary)
		}
		m.statusText = "ready"
		m.syncActivity()
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func sendCmd(ctx context.Context, fn func(context.Context, string) (DispatchResult, error), prompt string) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		res, err := fn(ctx, prompt)
		return sendFinishedMsg{
			result:  res,
			err:     err,
			elapsed: time.Since(start),
		}
	}
}

func (m *model) resize() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	mainWidth := m.width - 38
	if mainWidth < 40 {
		mainWidth = m.width
	}
	activityHeight := m.height - 11
	if activityHeight < 8 {
		activityHeight = 8
	}
	m.activity.Width = mainWidth - 6
	m.activity.Height = activityHeight
}

func (m *model) syncActivity() {
	m.activity.SetContent(strings.Join(m.lines, "\n"))
	m.activity.GotoBottom()
}

func (m model) View() string {
	mainWidth := m.width - 38
	if mainWidth < 40 {
		mainWidth = m.width
	}
	if mainWidth < 50 {
		mainWidth = 50
	}

	title := headerStyle.Render(fmt.Sprintf("%s  ·  task %s", m.cfg.Title, shortID(m.cfg.TaskID)))
	status := subtleStyle.Render(fmt.Sprintf("worker %s  |  %s", nonEmpty(m.cfg.Worker, "auto"), m.statusText))
	hero := panelStyle.Copy().Width(mainWidth).Render(title + "\n" + status + "\n" + subtleStyle.Render("repo "+nonEmpty(m.cfg.RepoRoot, "n/a")))

	activity := panelStyle.Copy().Width(mainWidth).Render("Activity\n\n" + m.activity.View())
	promptLabel := subtleStyle.Render("Prompt")
	prompt := panelStyle.Copy().Width(mainWidth).Render(promptLabel + "\n" + m.input.View())

	right := sidePanelStyle.Render(
		headerStyle.Render("Quick Keys") + "\n\n" +
			"enter  send prompt\n" +
			"ctrl+l clear activity\n" +
			"q      quit\n\n" +
			headerStyle.Render("Flow") + "\n\n" +
			"1) task.message\n" +
			"2) task.run start\n" +
			"3) canonical response",
	)

	left := lipgloss.JoinVertical(lipgloss.Left, hero, activity, prompt)
	if m.width < 110 {
		return bgStyle.Render(left)
	}
	return bgStyle.Render(lipgloss.JoinHorizontal(lipgloss.Top, left, right))
}

func shortID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
