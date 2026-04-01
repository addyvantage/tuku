package app

import (
	"context"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"tuku/internal/domain/provider"
	"tuku/internal/ipc"
	tukushell "tuku/internal/tui/shell"
)

func TestPrimaryWorkerLauncherDefaultsToCodexOnFirstRun(t *testing.T) {
	model := newPrimaryWorkerLauncherModel(primaryWorkerSelectionContext{Remembered: tukushell.WorkerPreferenceAuto})
	if got := model.options[model.selected].Preference; got != tukushell.WorkerPreferenceCodex {
		t.Fatalf("expected first-run default to codex, got %q", got)
	}
}

func TestPrimaryWorkerLauncherUsesRememberedWorkerAsDefault(t *testing.T) {
	model := newPrimaryWorkerLauncherModel(primaryWorkerSelectionContext{Remembered: tukushell.WorkerPreferenceClaude})
	if got := model.options[model.selected].Preference; got != tukushell.WorkerPreferenceClaude {
		t.Fatalf("expected remembered claude selection, got %q", got)
	}
}

func TestPrimaryWorkerLauncherViewIsCompactTerminalSurface(t *testing.T) {
	model := newPrimaryWorkerLauncherModel(primaryWorkerSelectionContext{
		Remembered: tukushell.WorkerPreferenceAuto,
		Recommendation: provider.Recommendation{
			Worker:     provider.WorkerCodex,
			Confidence: "high",
			Reason:     "execution-ready brief favors direct implementation",
		},
		Prerequisites: map[tukushell.WorkerPreference]tukushell.WorkerPrerequisite{
			tukushell.WorkerPreferenceCodex:  {State: tukushell.WorkerPrerequisiteReady, Ready: true},
			tukushell.WorkerPreferenceClaude: {State: tukushell.WorkerPrerequisiteMissingBinary},
		},
	})
	model.width = 80
	model.height = 24

	rendered := stripANSIEscapeCodes(model.View())
	if strings.Contains(rendered, "╭") || strings.Contains(rendered, "╰") {
		t.Fatalf("expected launcher to avoid modal card borders, got %q", rendered)
	}
	if !strings.Contains(rendered, "Choose a worker for this session") || !strings.Contains(rendered, "› Launch with Codex [recommended]") {
		t.Fatalf("expected compact launcher copy and selected row, got %q", rendered)
	}
	if !strings.Contains(rendered, "Recommendation") || !strings.Contains(rendered, "Codex (high confidence)") {
		t.Fatalf("expected recommendation callout in launcher view, got %q", rendered)
	}
	if !strings.Contains(rendered, "Workers") || !strings.Contains(rendered, "↑↓ move • Enter select • Esc cancel") {
		t.Fatalf("expected structured sections and controls in launcher view, got %q", rendered)
	}
	if !strings.Contains(rendered, "Ready: installed and signed in") {
		t.Fatalf("expected worker readiness status in launcher view, got %q", rendered)
	}
}

func TestPrimaryWorkerLauncherViewUsesWideTerminalWidthBeforeWrapping(t *testing.T) {
	model := newPrimaryWorkerLauncherModel(primaryWorkerSelectionContext{
		Remembered: tukushell.WorkerPreferenceAuto,
		Recommendation: provider.Recommendation{
			Worker:     provider.WorkerCodex,
			Confidence: "high",
			Reason:     "execution-ready brief favors direct implementation",
		},
		Prerequisites: map[tukushell.WorkerPreference]tukushell.WorkerPrerequisite{
			tukushell.WorkerPreferenceCodex: {State: tukushell.WorkerPrerequisiteReady, Ready: true},
		},
	})
	model.width = 120
	model.options[0].Summary = "Best for implementation-heavy work when the brief is already narrowed for direct execution."

	rendered := stripANSIEscapeCodes(model.View())
	if !strings.Contains(rendered, "    Best for implementation-heavy work when the brief is already narrowed for direct execution.") {
		t.Fatalf("expected wide picker to keep the codex summary on one line, got %q", rendered)
	}
	if longestRenderedLine(rendered) < 80 {
		t.Fatalf("expected picker to use more of the available terminal width, longest line width=%d", longestRenderedLine(rendered))
	}
}

func TestPrimaryWorkerLauncherViewWrapsDetailLinesWithStableIndent(t *testing.T) {
	model := newPrimaryWorkerLauncherModel(primaryWorkerSelectionContext{
		Remembered: tukushell.WorkerPreferenceAuto,
		Recommendation: provider.Recommendation{
			Worker: provider.WorkerCodex,
		},
		Prerequisites: map[tukushell.WorkerPreference]tukushell.WorkerPrerequisite{
			tukushell.WorkerPreferenceCodex: {State: tukushell.WorkerPrerequisiteReady, Ready: true},
		},
	})
	model.width = 62
	model.options[0].Summary = "A longer explanation that wraps neatly under the option title without drifting awkwardly to the left edge."

	lines := strings.Split(stripANSIEscapeCodes(model.View()), "\n")
	headerIndex := -1
	for idx, line := range lines {
		if strings.Contains(line, "› Launch with Codex [recommended]") {
			headerIndex = idx
			break
		}
	}
	if headerIndex == -1 {
		t.Fatalf("expected selected codex option header in rendered picker, got %q", strings.Join(lines, "\n"))
	}
	detailLines := 0
	for idx := headerIndex + 1; idx < len(lines); idx++ {
		line := lines[idx]
		if strings.TrimSpace(line) == "" {
			break
		}
		if strings.Contains(line, "Launch with Claude") || strings.Contains(line, "↑↓ move") {
			break
		}
		if !strings.HasPrefix(line, "    ") {
			t.Fatalf("expected wrapped detail line to keep a four-space block indent, got %q", line)
		}
		detailLines++
	}
	if detailLines < 2 {
		t.Fatalf("expected summary and status detail lines under the selected option, got %d in %q", detailLines, strings.Join(lines, "\n"))
	}
}

func TestPrimaryWorkerLauncherViewStaysReadableInNarrowTerminal(t *testing.T) {
	model := newPrimaryWorkerLauncherModel(primaryWorkerSelectionContext{
		Remembered: tukushell.WorkerPreferenceAuto,
		Recommendation: provider.Recommendation{
			Worker:     provider.WorkerCodex,
			Confidence: "high",
			Reason:     "implementation-heavy brief already narrowed for direct execution",
		},
		Prerequisites: map[tukushell.WorkerPreference]tukushell.WorkerPrerequisite{
			tukushell.WorkerPreferenceCodex:  {State: tukushell.WorkerPrerequisiteReady, Ready: true},
			tukushell.WorkerPreferenceClaude: {State: tukushell.WorkerPrerequisiteUnauthenticated},
		},
	})
	model.width = 48

	rendered := stripANSIEscapeCodes(model.View())
	for _, line := range strings.Split(rendered, "\n") {
		if renderedWidth(line) > model.width {
			t.Fatalf("expected narrow picker lines to stay within the terminal width %d, got %q (%d)", model.width, line, renderedWidth(line))
		}
	}
	if !strings.Contains(rendered, "Setup needed: installed, sign-in") || !strings.Contains(rendered, "required") {
		t.Fatalf("expected narrow picker to keep prerequisite status visible, got %q", rendered)
	}
}

func TestPrimaryWorkerLauncherEnterOnMissingWorkerShowsSetupPrompt(t *testing.T) {
	model := newPrimaryWorkerLauncherModel(primaryWorkerSelectionContext{
		Remembered: tukushell.WorkerPreferenceCodex,
		Prerequisites: map[tukushell.WorkerPreference]tukushell.WorkerPrerequisite{
			tukushell.WorkerPreferenceCodex: {
				Preference:     tukushell.WorkerPreferenceCodex,
				WorkerLabel:    "Codex",
				State:          tukushell.WorkerPrerequisiteMissingBinary,
				Summary:        "Codex is not installed on this machine yet.",
				Detail:         "Tuku can install Codex for you.",
				InstallCommand: []string{"npm", "install", "-g", "@openai/codex"},
			},
			tukushell.WorkerPreferenceClaude: {State: tukushell.WorkerPrerequisiteReady, Ready: true},
		},
	})
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	launcher := updated.(primaryWorkerLauncherModel)
	if launcher.mode != primaryWorkerLauncherModeSetup {
		t.Fatalf("expected setup mode after selecting missing worker, got %s", launcher.mode)
	}
	rendered := launcher.View()
	if !strings.Contains(rendered, "Codex needs a quick setup") {
		t.Fatalf("expected setup prompt title, got %q", rendered)
	}
	if !strings.Contains(rendered, "Install Codex now") {
		t.Fatalf("expected install action in setup prompt, got %q", rendered)
	}
}

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSIEscapeCodes(value string) string {
	return ansiEscapePattern.ReplaceAllString(value, "")
}

func longestRenderedLine(value string) int {
	maxWidth := 0
	for _, line := range strings.Split(value, "\n") {
		width := renderedWidth(line)
		if width > maxWidth {
			maxWidth = width
		}
	}
	return maxWidth
}

func renderedWidth(value string) int {
	return len([]rune(value))
}

func TestRunPrimaryWorkerSetupActionUsesInstallCommand(t *testing.T) {
	origDetect := detectPrimaryWorkerPrereq
	origCommand := workerSetupCommandContext
	defer func() {
		detectPrimaryWorkerPrereq = origDetect
		workerSetupCommandContext = origCommand
	}()

	detectPrimaryWorkerPrereq = func(preference tukushell.WorkerPreference) tukushell.WorkerPrerequisite {
		return tukushell.WorkerPrerequisite{
			Preference:     preference,
			WorkerLabel:    "Codex",
			InstallCommand: []string{"npm", "install", "-g", "@openai/codex"},
		}
	}

	var captured []string
	workerSetupCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		captured = append([]string{name}, args...)
		return exec.CommandContext(ctx, "sh", "-c", "exit 0")
	}

	notice, err := runPrimaryWorkerSetupAction(context.Background(), primaryWorkerSetupAction{
		Preference: tukushell.WorkerPreferenceCodex,
		Kind:       primaryWorkerSetupActionInstall,
	})
	if err != nil {
		t.Fatalf("run setup action: %v", err)
	}
	if strings.Join(captured, " ") != "npm install -g @openai/codex" {
		t.Fatalf("expected npm install command, got %q", strings.Join(captured, " "))
	}
	if !strings.Contains(notice, "installed") {
		t.Fatalf("expected install completion notice, got %q", notice)
	}
}

func TestPrimaryWorkerPreferenceRoundTrip(t *testing.T) {
	t.Setenv("TUKU_DATA_DIR", t.TempDir())

	if err := savePrimaryWorkerPreference(tukushell.WorkerPreferenceClaude); err != nil {
		t.Fatalf("save primary worker preference: %v", err)
	}
	got, err := loadPrimaryWorkerPreference()
	if err != nil {
		t.Fatalf("load primary worker preference: %v", err)
	}
	if got != tukushell.WorkerPreferenceClaude {
		t.Fatalf("expected remembered claude preference, got %q", got)
	}
}

func TestResolvePrimaryEntryWorkerPreferenceUsesLauncherForAuto(t *testing.T) {
	origLoad := loadPrimaryWorkerPref
	origSave := savePrimaryWorkerPref
	defer func() {
		loadPrimaryWorkerPref = origLoad
		savePrimaryWorkerPref = origSave
	}()

	loadPrimaryWorkerPref = func() (tukushell.WorkerPreference, error) {
		return tukushell.WorkerPreferenceClaude, nil
	}
	saved := tukushell.WorkerPreferenceAuto
	savePrimaryWorkerPref = func(preference tukushell.WorkerPreference) error {
		saved = preference
		return nil
	}

	var remembered tukushell.WorkerPreference
	var recommended provider.WorkerKind
	app := &CLIApplication{
		chooseWorkerFn: func(_ context.Context, selection primaryWorkerSelectionContext) (tukushell.WorkerPreference, error) {
			remembered = selection.Remembered
			recommended = selection.Recommendation.Worker
			return tukushell.WorkerPreferenceClaude, nil
		},
	}

	got, explicit, launcherUsed, err := resolvePrimaryEntryWorkerPreference(context.Background(), app, "", "auto", true, primaryWorkerSelectionContext{
		Remembered: tukushell.WorkerPreferenceClaude,
		Recommendation: provider.Recommendation{
			Worker: provider.WorkerClaude,
		},
	})
	if err != nil {
		t.Fatalf("resolve primary entry worker preference: %v", err)
	}
	if !explicit {
		t.Fatal("expected launcher-driven worker selection to be explicit")
	}
	if !launcherUsed {
		t.Fatal("expected auto primary entry to mark launcher usage")
	}
	if remembered != tukushell.WorkerPreferenceClaude {
		t.Fatalf("expected remembered worker to seed launcher default, got %q", remembered)
	}
	if recommended != provider.WorkerClaude {
		t.Fatalf("expected recommendation to reach launcher, got %q", recommended)
	}
	if got != tukushell.WorkerPreferenceClaude || saved != tukushell.WorkerPreferenceClaude {
		t.Fatalf("expected claude to be chosen and persisted, got chosen=%q saved=%q", got, saved)
	}
}

func TestRunPrimaryEntryLauncherCancelExitsCleanly(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	origGetwd := getWorkingDir
	origResolveRepo := resolveRepoRootFromDir
	origClear := clearPrimaryLauncherFn
	origLoad := loadPrimaryWorkerPref
	origSave := savePrimaryWorkerPref
	origTimeout := daemonReadyTimeout
	origInterval := daemonRetryInterval
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
		getWorkingDir = origGetwd
		resolveRepoRootFromDir = origResolveRepo
		clearPrimaryLauncherFn = origClear
		loadPrimaryWorkerPref = origLoad
		savePrimaryWorkerPref = origSave
		daemonReadyTimeout = origTimeout
		daemonRetryInterval = origInterval
	}()

	getWorkingDir = func() (string, error) { return "/tmp/repo", nil }
	resolveRepoRootFromDir = func(_ context.Context, dir string) (string, error) { return dir, nil }
	daemonReadyTimeout = 50 * time.Millisecond
	daemonRetryInterval = 0
	loadPrimaryWorkerPref = func() (tukushell.WorkerPreference, error) { return tukushell.WorkerPreferenceAuto, nil }
	savePrimaryWorkerPref = func(preference tukushell.WorkerPreference) error { return nil }

	var calls int
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		calls++
		if calls <= 2 {
			return ipc.Response{}, daemonUnavailableErr()
		}
		switch req.Method {
		case ipc.MethodResolveShellTaskForRepo:
			return mustResolveShellTaskResponse(t, "tsk_launcher_cancel"), nil
		case ipc.MethodTaskShellSnapshot:
			return mustShellSnapshotResponse(t, "tsk_launcher_cancel"), nil
		default:
			t.Fatalf("unexpected ipc method %s", req.Method)
			return ipc.Response{}, nil
		}
	}
	startLocalDaemon = func() (<-chan error, error) {
		ch := make(chan error)
		return ch, nil
	}

	cleared := false
	clearPrimaryLauncherFn = func(_ io.Writer) error {
		cleared = true
		return nil
	}

	called := false
	app := &CLIApplication{
		chooseWorkerFn: func(_ context.Context, _ primaryWorkerSelectionContext) (tukushell.WorkerPreference, error) {
			return "", errPrimaryWorkerSelectionCancelled
		},
		openShellFn: func(_ context.Context, _ string, _ string, _ tukushell.WorkerPreference) error {
			called = true
			return nil
		},
	}
	if err := app.runPrimaryEntry(context.Background(), "/tmp/tukud.sock", nil); err != nil {
		t.Fatalf("expected launcher cancel to exit cleanly, got %v", err)
	}
	if called {
		t.Fatal("shell should not open when launcher is cancelled")
	}
	if cleared {
		t.Fatal("launcher cancel should not clear the terminal surface")
	}
}

func TestRunPrimaryShortcutCodexSkipsLauncher(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	origGetwd := getWorkingDir
	origResolveRepo := resolveRepoRootFromDir
	origLoad := loadPrimaryWorkerPref
	origSave := savePrimaryWorkerPref
	origClear := clearPrimaryLauncherFn
	origTimeout := daemonReadyTimeout
	origInterval := daemonRetryInterval
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
		getWorkingDir = origGetwd
		resolveRepoRootFromDir = origResolveRepo
		loadPrimaryWorkerPref = origLoad
		savePrimaryWorkerPref = origSave
		clearPrimaryLauncherFn = origClear
		daemonReadyTimeout = origTimeout
		daemonRetryInterval = origInterval
	}()

	loadPrimaryWorkerPref = func() (tukushell.WorkerPreference, error) { return tukushell.WorkerPreferenceAuto, nil }
	saved := tukushell.WorkerPreferenceAuto
	savePrimaryWorkerPref = func(preference tukushell.WorkerPreference) error {
		saved = preference
		return nil
	}
	cleared := false
	clearPrimaryLauncherFn = func(_ io.Writer) error {
		cleared = true
		return nil
	}
	getWorkingDir = func() (string, error) { return "/tmp/repo", nil }
	resolveRepoRootFromDir = func(_ context.Context, dir string) (string, error) { return dir, nil }
	daemonReadyTimeout = 50 * time.Millisecond
	daemonRetryInterval = 0

	var calls int
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		calls++
		if calls <= 2 {
			return ipc.Response{}, daemonUnavailableErr()
		}
		switch req.Method {
		case ipc.MethodResolveShellTaskForRepo:
			return mustResolveShellTaskResponse(t, "tsk_codex_shortcut"), nil
		case ipc.MethodTaskShellSnapshot:
			return mustShellSnapshotResponse(t, "tsk_codex_shortcut"), nil
		default:
			t.Fatalf("unexpected ipc method %s", req.Method)
			return ipc.Response{}, nil
		}
	}
	startLocalDaemon = func() (<-chan error, error) {
		ch := make(chan error)
		return ch, nil
	}

	app := &CLIApplication{
		chooseWorkerFn: func(_ context.Context, _ primaryWorkerSelectionContext) (tukushell.WorkerPreference, error) {
			t.Fatal("launcher should be skipped for explicit codex shortcut")
			return "", nil
		},
		openShellFn: func(_ context.Context, _ string, _ string, preference tukushell.WorkerPreference) error {
			if preference != tukushell.WorkerPreferenceCodex {
				t.Fatalf("expected codex shortcut to launch codex, got %q", preference)
			}
			return nil
		},
	}
	if err := app.Run(context.Background(), []string{"codex"}); err != nil {
		t.Fatalf("run codex shortcut: %v", err)
	}
	if saved != tukushell.WorkerPreferenceCodex {
		t.Fatalf("expected codex shortcut to persist codex, got %q", saved)
	}
	if cleared {
		t.Fatal("explicit codex shortcut should not clear a launcher surface")
	}
}

func TestRunPrimaryShortcutClaudeSkipsLauncher(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	origGetwd := getWorkingDir
	origResolveRepo := resolveRepoRootFromDir
	origLoad := loadPrimaryWorkerPref
	origSave := savePrimaryWorkerPref
	origClear := clearPrimaryLauncherFn
	origTimeout := daemonReadyTimeout
	origInterval := daemonRetryInterval
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
		getWorkingDir = origGetwd
		resolveRepoRootFromDir = origResolveRepo
		loadPrimaryWorkerPref = origLoad
		savePrimaryWorkerPref = origSave
		clearPrimaryLauncherFn = origClear
		daemonReadyTimeout = origTimeout
		daemonRetryInterval = origInterval
	}()

	loadPrimaryWorkerPref = func() (tukushell.WorkerPreference, error) { return tukushell.WorkerPreferenceAuto, nil }
	saved := tukushell.WorkerPreferenceAuto
	savePrimaryWorkerPref = func(preference tukushell.WorkerPreference) error {
		saved = preference
		return nil
	}
	cleared := false
	clearPrimaryLauncherFn = func(_ io.Writer) error {
		cleared = true
		return nil
	}
	getWorkingDir = func() (string, error) { return "/tmp/repo", nil }
	resolveRepoRootFromDir = func(_ context.Context, dir string) (string, error) { return dir, nil }
	daemonReadyTimeout = 50 * time.Millisecond
	daemonRetryInterval = 0

	var calls int
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		calls++
		if calls <= 2 {
			return ipc.Response{}, daemonUnavailableErr()
		}
		switch req.Method {
		case ipc.MethodResolveShellTaskForRepo:
			return mustResolveShellTaskResponse(t, "tsk_claude_shortcut"), nil
		case ipc.MethodTaskShellSnapshot:
			return mustShellSnapshotResponse(t, "tsk_claude_shortcut"), nil
		default:
			t.Fatalf("unexpected ipc method %s", req.Method)
			return ipc.Response{}, nil
		}
	}
	startLocalDaemon = func() (<-chan error, error) {
		ch := make(chan error)
		return ch, nil
	}

	app := &CLIApplication{
		chooseWorkerFn: func(_ context.Context, _ primaryWorkerSelectionContext) (tukushell.WorkerPreference, error) {
			t.Fatal("launcher should be skipped for explicit claude shortcut")
			return "", nil
		},
		openShellFn: func(_ context.Context, _ string, _ string, preference tukushell.WorkerPreference) error {
			if preference != tukushell.WorkerPreferenceClaude {
				t.Fatalf("expected claude shortcut to launch claude, got %q", preference)
			}
			return nil
		},
	}
	if err := app.Run(context.Background(), []string{"claude"}); err != nil {
		t.Fatalf("run claude shortcut: %v", err)
	}
	if saved != tukushell.WorkerPreferenceClaude {
		t.Fatalf("expected claude shortcut to persist claude, got %q", saved)
	}
	if cleared {
		t.Fatal("explicit claude shortcut should not clear a launcher surface")
	}
}

func TestRunPrimaryChatShortcutClaudeSkipsLauncher(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	origGetwd := getWorkingDir
	origResolveRepo := resolveRepoRootFromDir
	origLoad := loadPrimaryWorkerPref
	origSave := savePrimaryWorkerPref
	origClear := clearPrimaryLauncherFn
	origTimeout := daemonReadyTimeout
	origInterval := daemonRetryInterval
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
		getWorkingDir = origGetwd
		resolveRepoRootFromDir = origResolveRepo
		loadPrimaryWorkerPref = origLoad
		savePrimaryWorkerPref = origSave
		clearPrimaryLauncherFn = origClear
		daemonReadyTimeout = origTimeout
		daemonRetryInterval = origInterval
	}()

	loadPrimaryWorkerPref = func() (tukushell.WorkerPreference, error) { return tukushell.WorkerPreferenceAuto, nil }
	savePrimaryWorkerPref = func(preference tukushell.WorkerPreference) error { return nil }
	cleared := false
	clearPrimaryLauncherFn = func(_ io.Writer) error {
		cleared = true
		return nil
	}
	getWorkingDir = func() (string, error) { return "/tmp/repo", nil }
	resolveRepoRootFromDir = func(_ context.Context, dir string) (string, error) { return dir, nil }
	daemonReadyTimeout = 50 * time.Millisecond
	daemonRetryInterval = 0

	var calls int
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		calls++
		if calls <= 2 {
			return ipc.Response{}, daemonUnavailableErr()
		}
		switch req.Method {
		case ipc.MethodResolveShellTaskForRepo:
			return mustResolveShellTaskResponse(t, "tsk_chat_claude_shortcut"), nil
		case ipc.MethodTaskShellSnapshot:
			return mustShellSnapshotResponse(t, "tsk_chat_claude_shortcut"), nil
		default:
			t.Fatalf("unexpected ipc method %s", req.Method)
			return ipc.Response{}, nil
		}
	}
	startLocalDaemon = func() (<-chan error, error) {
		ch := make(chan error)
		return ch, nil
	}

	app := &CLIApplication{
		chooseWorkerFn: func(_ context.Context, _ primaryWorkerSelectionContext) (tukushell.WorkerPreference, error) {
			t.Fatal("launcher should be skipped for explicit chat claude shortcut")
			return "", nil
		},
		openShellFn: func(_ context.Context, _ string, _ string, preference tukushell.WorkerPreference) error {
			if preference != tukushell.WorkerPreferenceClaude {
				t.Fatalf("expected chat claude shortcut to launch claude, got %q", preference)
			}
			return nil
		},
	}
	if err := app.Run(context.Background(), []string{"chat", "claude"}); err != nil {
		t.Fatalf("run chat claude shortcut: %v", err)
	}
	if cleared {
		t.Fatal("explicit chat claude shortcut should not clear a launcher surface")
	}
}

func TestRunPrimaryEntryLauncherSelectionClearsBeforeShellOpens(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	origGetwd := getWorkingDir
	origResolveRepo := resolveRepoRootFromDir
	origLoad := loadPrimaryWorkerPref
	origSave := savePrimaryWorkerPref
	origClear := clearPrimaryLauncherFn
	origTimeout := daemonReadyTimeout
	origInterval := daemonRetryInterval
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
		getWorkingDir = origGetwd
		resolveRepoRootFromDir = origResolveRepo
		loadPrimaryWorkerPref = origLoad
		savePrimaryWorkerPref = origSave
		clearPrimaryLauncherFn = origClear
		daemonReadyTimeout = origTimeout
		daemonRetryInterval = origInterval
	}()

	loadPrimaryWorkerPref = func() (tukushell.WorkerPreference, error) { return tukushell.WorkerPreferenceAuto, nil }
	savePrimaryWorkerPref = func(preference tukushell.WorkerPreference) error { return nil }
	getWorkingDir = func() (string, error) { return "/tmp/repo", nil }
	resolveRepoRootFromDir = func(_ context.Context, dir string) (string, error) { return dir, nil }
	daemonReadyTimeout = 50 * time.Millisecond
	daemonRetryInterval = 0

	var calls int
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		calls++
		if calls <= 2 {
			return ipc.Response{}, daemonUnavailableErr()
		}
		switch req.Method {
		case ipc.MethodResolveShellTaskForRepo:
			return mustResolveShellTaskResponse(t, "tsk_launcher_clear"), nil
		case ipc.MethodTaskShellSnapshot:
			return mustShellSnapshotResponse(t, "tsk_launcher_clear"), nil
		default:
			t.Fatalf("unexpected ipc method %s", req.Method)
			return ipc.Response{}, nil
		}
	}
	startLocalDaemon = func() (<-chan error, error) {
		ch := make(chan error)
		return ch, nil
	}

	steps := []string{}
	clearPrimaryLauncherFn = func(_ io.Writer) error {
		steps = append(steps, "clear")
		return nil
	}

	app := &CLIApplication{
		chooseWorkerFn: func(_ context.Context, _ primaryWorkerSelectionContext) (tukushell.WorkerPreference, error) {
			return tukushell.WorkerPreferenceCodex, nil
		},
		openShellFn: func(_ context.Context, _ string, _ string, _ tukushell.WorkerPreference) error {
			steps = append(steps, "open")
			return nil
		},
	}
	if err := app.runPrimaryEntry(context.Background(), "/tmp/tukud.sock", nil); err != nil {
		t.Fatalf("run primary entry with launcher: %v", err)
	}
	if len(steps) != 2 || steps[0] != "clear" || steps[1] != "open" {
		t.Fatalf("expected launcher clear before shell open, got %#v", steps)
	}
}
