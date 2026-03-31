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
}

type primaryWorkerLauncherModel struct {
	width     int
	height    int
	selected  int
	options   []primaryWorkerOption
	selection primaryWorkerSelectionContext
	cancelled bool
}

func runPrimaryWorkerLauncher(ctx context.Context, selection primaryWorkerSelectionContext) (tukushell.WorkerPreference, error) {
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
	return launcher.options[launcher.selected].Preference, nil
}

func newPrimaryWorkerLauncherModel(selection primaryWorkerSelectionContext) primaryWorkerLauncherModel {
	selected := 0
	if selection.Remembered == tukushell.WorkerPreferenceClaude {
		selected = 1
	} else if selection.Remembered == tukushell.WorkerPreferenceAuto && selection.Recommendation.Worker == provider.WorkerClaude {
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
		options = append(options, primaryWorkerOption{
			Preference:  preference,
			Title:       "Launch with " + provider.Label(candidate.Worker),
			Summary:     strings.TrimSpace(candidate.Summary),
			Recommended: candidate.Worker == selection.Recommendation.Worker,
		})
	}
	if len(options) == 0 {
		options = []primaryWorkerOption{
			{Preference: tukushell.WorkerPreferenceCodex, Title: "Launch with Codex"},
			{Preference: tukushell.WorkerPreferenceClaude, Title: "Launch with Claude"},
		}
	}
	return primaryWorkerLauncherModel{
		selected:  selected,
		options:   options,
		selection: selection,
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
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF"))
	recommended := lipgloss.NewStyle().Foreground(lipgloss.Color("#86EFAC")).Bold(true)
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render("↑↓ move • Enter select • Esc cancel")

	lines := []string{"", kicker, title, subtitle, ""}
	if m.selection.Recommendation.Worker != "" && m.selection.Recommendation.Worker != provider.WorkerUnknown {
		lines = append(lines, recommended.Render("Recommended: "+provider.Label(m.selection.Recommendation.Worker)+" ("+nonEmpty(strings.TrimSpace(m.selection.Recommendation.Confidence), "medium")+")"))
		if reason := strings.TrimSpace(m.selection.Recommendation.Reason); reason != "" {
			lines = append(lines, muted.Render("  "+reason))
		}
		lines = append(lines, "")
	}
	for idx, item := range m.options {
		row := "  " + item.Title
		if item.Recommended {
			row += " [recommended]"
		}
		if idx == m.selected {
			lines = append(lines, selected.Render("› "+strings.TrimSpace(row)))
		} else {
			lines = append(lines, option.Render(row))
		}
		if summary := strings.TrimSpace(item.Summary); summary != "" {
			lines = append(lines, muted.Render("    "+summary))
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
