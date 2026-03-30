package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
	Preference tukushell.WorkerPreference
	Title      string
}

type primaryWorkerLauncherModel struct {
	width     int
	height    int
	selected  int
	options   []primaryWorkerOption
	cancelled bool
}

func runPrimaryWorkerLauncher(ctx context.Context, remembered tukushell.WorkerPreference) (tukushell.WorkerPreference, error) {
	model := newPrimaryWorkerLauncherModel(remembered)
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
	return launcher.options[launcher.selected].Preference, nil
}

func newPrimaryWorkerLauncherModel(remembered tukushell.WorkerPreference) primaryWorkerLauncherModel {
	selected := 0
	if remembered == tukushell.WorkerPreferenceClaude {
		selected = 1
	}
	return primaryWorkerLauncherModel{
		selected: selected,
		options: []primaryWorkerOption{
			{Preference: tukushell.WorkerPreferenceCodex, Title: "Launch with Codex"},
			{Preference: tukushell.WorkerPreferenceClaude, Title: "Launch with Claude"},
		},
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
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "up", "shift+tab":
			if m.selected > 0 {
				m.selected--
			}
			return m, nil
		case "down", "tab":
			if m.selected < len(m.options)-1 {
				m.selected++
			}
			return m, nil
		case "enter":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m primaryWorkerLauncherModel) View() string {
	contentWidth := min(58, max(40, m.width-4))
	kicker := lipgloss.NewStyle().Foreground(lipgloss.Color("#F9FAFB")).Bold(true).Render("TUKU")
	title := lipgloss.NewStyle().Foreground(lipgloss.Color("#F3F4F6")).Bold(true).Render("Choose a worker for this session")
	subtitle := lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF")).Render("Pick one to open the shell.")
	option := lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB"))
	selected := lipgloss.NewStyle().Foreground(lipgloss.Color("#F9FAFB")).Bold(true)
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render("↑↓ move • Enter select • Esc cancel")

	lines := []string{"", kicker, title, subtitle, ""}
	for idx, item := range m.options {
		row := "  " + item.Title
		if item.Preference == tukushell.WorkerPreferenceCodex && m.selected != idx {
			row = "  Launch with Codex"
		}
		if idx == m.selected {
			lines = append(lines, selected.Render("› "+strings.TrimSpace(row)))
		} else {
			lines = append(lines, option.Render(row))
		}
	}
	lines = append(lines, "", hint)
	return lipgloss.NewStyle().
		Width(contentWidth).
		Render(strings.Join(lines, "\n"))
}

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
