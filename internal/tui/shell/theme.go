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
	workspaceFocus   lipgloss.Style
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
	keycap      lipgloss.Style
	keyLabel    lipgloss.Style

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
			Background(lipgloss.Color("#070C14")).
			Foreground(lipgloss.Color("#E6EDF8")),

		headerKicker: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8EC7FF")).
			Bold(true),
		headerTitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F4F8FF")).
			Bold(true),
		headerSubtext: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8EA0BA")),
		headerRule: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#1A273A")),

		chipBase: lipgloss.NewStyle().
			Padding(0, 1),
		chipNeutral: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#D0DBEA")).
			Background(lipgloss.Color("#1A2A3F")),
		chipAccent: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FEE5D2")).
			Background(lipgloss.Color("#5E3A26")),
		chipPositive: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#D9F5E6")).
			Background(lipgloss.Color("#1D503B")),
		chipCaution: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F8E6CC")).
			Background(lipgloss.Color("#5E4B2A")),
		chipDanger: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FCDADA")).
			Background(lipgloss.Color("#5D2D2D")),
		chipMuted: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#AABACF")).
			Background(lipgloss.Color("#172336")),

		workspaceBase: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#293B53")).
			Background(lipgloss.Color("#0C141F")).
			Padding(0, 1),
		workspaceFocused: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#4E83BF")).
			Background(lipgloss.Color("#0E1725")).
			Padding(0, 1),
		workspaceTitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#EDF4FF")).
			Bold(true),
		workspaceFocus: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8EC7FF")).
			Bold(true),
		workspaceSub: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#89A0BE")),
		workspaceBody: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#DCE6F4")),

		railBase: lipgloss.NewStyle().
			Background(lipgloss.Color("#0A111B")).
			Padding(0, 0),
		railTitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#B0C3DB")).
			Bold(true),
		railHint: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7D93AE")),
		railCard: lipgloss.NewStyle().
			Background(lipgloss.Color("#101C2C")).
			Padding(0, 1).
			MarginBottom(1),
		railCardTitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#C8D8EE")).
			Bold(true),
		railCardBody: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#D6E2F2")),

		activityBase: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#263A52")).
			Background(lipgloss.Color("#0C1420")).
			Padding(0, 1),
		activityTitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#B5C9E2")).
			Bold(true),
		activityBody: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#D2DEEE")),

		dockBase: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#2F4560")).
			Background(lipgloss.Color("#0F1826")).
			Padding(0, 1),
		dockTitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#EAF2FF")).
			Bold(true),
		dockStatus: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A4BAD4")),
		dockPrompt: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F4F8FF")).
			Bold(true),
		dockPlaceholder: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A7BCD6")),
		dockHint: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8FA4BF")),
		dockReadOnly: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#D8C9B1")),

		footerText: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#93A6C0")),
		footerMuted: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7287A4")),
		footerRule: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#192A3E")),
		keycap: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#EAF3FF")).
			Background(lipgloss.Color("#27405D")).
			Bold(true).
			Padding(0, 1),
		keyLabel: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8FA5C0")),

		overlayBase: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#5F7CA1")).
			Background(lipgloss.Color("#0D1726")).
			Padding(1, 2),
		overlayTitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ECF3FF")).
			Bold(true),
		overlayText: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#D8E4F4")),
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

func (t shellTheme) keyHint(key string, label string) string {
	key = strings.TrimSpace(key)
	label = strings.TrimSpace(label)
	if key == "" && label == "" {
		return ""
	}
	keyToken := t.keycap.Render(strings.ToUpper(nonEmpty(key, "•")))
	if label == "" {
		return keyToken
	}
	return keyToken + " " + t.keyLabel.Render(label)
}
