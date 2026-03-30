package shell

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type shellTone int

const (
	shellToneNeutral shellTone = iota
	shellToneAccent
	shellTonePositive
	shellToneCaution
	shellToneDanger
	shellToneMuted
)

type shellTheme struct {
	outerPadding int
	panelGap     int

	rootBackground lipgloss.Style

	headerKicker  lipgloss.Style
	headerTitle   lipgloss.Style
	headerSubtext lipgloss.Style
	headerRule    lipgloss.Style

	chipBase     lipgloss.Style
	chipAccent   lipgloss.Style
	chipPositive lipgloss.Style
	chipCaution  lipgloss.Style
	chipDanger   lipgloss.Style
	chipMuted    lipgloss.Style
	chipNeutral  lipgloss.Style

	workspaceBase    lipgloss.Style
	workspaceFocused lipgloss.Style
	workspaceTitle   lipgloss.Style
	workspaceSub     lipgloss.Style
	workspaceBody    lipgloss.Style

	railBase      lipgloss.Style
	railTitle     lipgloss.Style
	railHint      lipgloss.Style
	railCard      lipgloss.Style
	railCardTitle lipgloss.Style
	railCardBody  lipgloss.Style

	activityBase  lipgloss.Style
	activityTitle lipgloss.Style
	activityBody  lipgloss.Style

	dockBase        lipgloss.Style
	dockTitle       lipgloss.Style
	dockStatus      lipgloss.Style
	dockPrompt      lipgloss.Style
	dockPlaceholder lipgloss.Style
	dockHint        lipgloss.Style
	dockReadOnly    lipgloss.Style

	footerText  lipgloss.Style
	footerMuted lipgloss.Style
	footerRule  lipgloss.Style

	overlayBase  lipgloss.Style
	overlayTitle lipgloss.Style
	overlayText  lipgloss.Style
}

func newShellTheme(layout shellLayout) shellTheme {
	outerPadding := max(0, layout.outerPadding)
	panelGap := max(1, layout.panelGap)

	return shellTheme{
		outerPadding: outerPadding,
		panelGap:     panelGap,

		rootBackground: lipgloss.NewStyle().
			Background(lipgloss.Color("#090C12")).
			Foreground(lipgloss.Color("#E8EDF4")),

		headerKicker: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#97AFC9")).
			Bold(true),
		headerTitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F3F7FD")).
			Bold(true),
		headerSubtext: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#93A1B5")),
		headerRule: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#1A2533")),

		chipBase: lipgloss.NewStyle().
			Padding(0, 1),
		chipNeutral: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#C7D2E1")).
			Background(lipgloss.Color("#182231")),
		chipAccent: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFDCC5")).
			Background(lipgloss.Color("#553626")),
		chipPositive: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#D6F3E5")).
			Background(lipgloss.Color("#1D4938")),
		chipCaution: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F5E4CC")).
			Background(lipgloss.Color("#594527")),
		chipDanger: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAD7D7")).
			Background(lipgloss.Color("#5A2D2D")),
		chipMuted: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#AEBBCF")).
			Background(lipgloss.Color("#17202D")),

		workspaceBase: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#293444")).
			Background(lipgloss.Color("#0D121A")).
			Padding(0, 1),
		workspaceFocused: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#57759A")).
			Background(lipgloss.Color("#0D121A")).
			Padding(0, 1),
		workspaceTitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ECF2FA")).
			Bold(true),
		workspaceSub: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8F9EB3")),
		workspaceBody: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E0E7F1")),

		railBase: lipgloss.NewStyle().
			Background(lipgloss.Color("#0B1017")).
			Padding(0, 0),
		railTitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A2B4CB")).
			Bold(true),
		railHint: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7888A0")),
		railCard: lipgloss.NewStyle().
			Background(lipgloss.Color("#101724")).
			Padding(0, 1).
			MarginBottom(1),
		railCardTitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#BFCDE0")).
			Bold(true),
		railCardBody: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#D1DCEB")),

		activityBase: lipgloss.NewStyle().
			Background(lipgloss.Color("#0C1118")).
			Padding(0, 1),
		activityTitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A5B7CF")).
			Bold(true),
		activityBody: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#C8D6E8")),

		dockBase: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#2D3A4D")).
			Background(lipgloss.Color("#0F151E")).
			Padding(0, 1),
		dockTitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E9F0FA")).
			Bold(true),
		dockStatus: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#97ABC4")),
		dockPrompt: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F3F7FD")).
			Bold(true),
		dockPlaceholder: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9FB0C6")),
		dockHint: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8597B0")),
		dockReadOnly: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#D8C5A8")),

		footerText: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8D9CB2")),
		footerMuted: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6F7F98")),
		footerRule: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#192433")),

		overlayBase: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#5A6E89")).
			Background(lipgloss.Color("#0D121A")).
			Padding(1, 2),
		overlayTitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E9F0FA")).
			Bold(true),
		overlayText: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#D6E0EE")),
	}
}

func (t shellTheme) chip(text string, tone shellTone) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	s := t.chipNeutral
	switch tone {
	case shellToneAccent:
		s = t.chipAccent
	case shellTonePositive:
		s = t.chipPositive
	case shellToneCaution:
		s = t.chipCaution
	case shellToneDanger:
		s = t.chipDanger
	case shellToneMuted:
		s = t.chipMuted
	}
	return t.chipBase.Copy().Inherit(s).Render(text)
}

func toneForStatus(value string) shellTone {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return shellToneMuted
	}
	switch {
	case strings.Contains(v, "failed"), strings.Contains(v, "error"), strings.Contains(v, "exited"), strings.Contains(v, "read-only"), strings.Contains(v, "fallback"), strings.Contains(v, "transcript"), strings.Contains(v, "blocked"), strings.Contains(v, "repair"):
		return shellToneCaution
	case strings.Contains(v, "live"), strings.Contains(v, "active"), strings.Contains(v, "attachable"), strings.Contains(v, "clean"), strings.Contains(v, "ready"), strings.Contains(v, "resumable"), strings.Contains(v, "complete"):
		return shellTonePositive
	case strings.Contains(v, "next"), strings.Contains(v, "phase"), strings.Contains(v, "continuity"), strings.Contains(v, "launch"), strings.Contains(v, "decision"):
		return shellToneAccent
	default:
		return shellToneNeutral
	}
}
