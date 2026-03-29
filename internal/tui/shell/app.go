package shell

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

type App struct {
	TaskID            string
	Source            SnapshotSource
	MessageSender     TaskMessageSender
	ActionExecutor    PrimaryActionExecutor
	LifecycleSink     LifecycleSink
	RegistrySink      SessionRegistrySink
	RegistrySource    SessionRegistrySource
	TranscriptSink    TranscriptSink
	WorkerPreference  WorkerPreference
	ReattachSessionID string
	Host              WorkerHost
	FallbackHost      WorkerHost
	Input             io.Reader
	Output            io.Writer
	RefreshInterval   time.Duration
}

type primaryActionExecutionResult struct {
	outcome    PrimaryActionExecutionOutcome
	step       OperatorExecutionStep
	before     Snapshot
	err        error
	finishedAt time.Time
}

func NewApp(taskID string, source SnapshotSource) *App {
	return &App{
		TaskID:           taskID,
		Source:           source,
		WorkerPreference: WorkerPreferenceAuto,
		FallbackHost:     NewTranscriptHost(),
		Input:            os.Stdin,
		Output:           os.Stdout,
		RefreshInterval:  5 * time.Second,
	}
}

func (a *App) Run(ctx context.Context) error {
	if a.Source == nil {
		return fmt.Errorf("shell snapshot source is required")
	}
	if a.Input == nil {
		a.Input = os.Stdin
	}
	if a.Output == nil {
		a.Output = os.Stdout
	}
	snapshot, err := a.Source.Load(a.TaskID)
	if err != nil {
		return err
	}

	ui := initialUIState(time.Now().UTC(), a.WorkerPreference)
	addSessionEvent(&ui.Session, ui.LastRefresh, SessionEventShellStarted, fmt.Sprintf("Shell session %s started.", shortTaskID(ui.Session.SessionID)))
	if prior := capturePriorPersistedShellOutcome(snapshot); prior != "" {
		ui.Session.PriorPersistedSummary = prior
		addSessionEvent(&ui.Session, ui.LastRefresh, SessionEventPriorPersistedProof, "Previous persisted shell outcome: "+prior)
	}

	stdinFile, ok := a.Input.(*os.File)
	if !ok {
		return fmt.Errorf("shell input must be an *os.File")
	}
	stdoutFile, ok := a.Output.(*os.File)
	if !ok {
		return fmt.Errorf("shell output must be an *os.File")
	}

	restore, err := enterTerminalMode(stdinFile, stdoutFile)
	if err != nil {
		return err
	}
	defer restore()

	preferredHost := a.Host
	requestedPreference := a.WorkerPreference
	resolvedWorker := resolveWorkerPreference(requestedPreference, snapshot)
	if isScratchIntakeSnapshot(snapshot) {
		resolvedWorker = WorkerPreferenceAuto
	}
	if err := loadKnownShellSessions(a.RegistrySource, a.TaskID, &ui.Session); err != nil {
		ui.LastError = "shell session registry read failed: " + err.Error()
	}
	reattachTarget, shouldReattach, err := resolveReattachTarget(strings.TrimSpace(a.ReattachSessionID), ui.Session.KnownSessions)
	if err != nil {
		recordLifecycle(ctx, a.LifecycleSink, a.TaskID, ui.Session.SessionID, PersistedLifecycleReattachFailed, HostStatus{
			Mode:  HostModeTranscript,
			State: HostStateTranscriptOnly,
			Note:  err.Error(),
		}, &ui)
		return err
	}
	if shouldReattach {
		if preferred := sessionPreferredWorker(reattachTarget); preferred != WorkerPreferenceAuto {
			requestedPreference = preferred
		}
		ui.Session.WorkerSessionID = reattachTarget.WorkerSessionID
		ui.Session.WorkerSessionIDSource = reattachTarget.WorkerSessionIDSource
		ui.Session.AttachCapability = WorkerAttachCapabilityAttachable
		addSessionEvent(&ui.Session, ui.LastRefresh, SessionEventHostStartupAttempted, fmt.Sprintf("Requested reattach to prior worker session %s via shell session %s.", truncateWithEllipsis(reattachTarget.WorkerSessionID, 20), shortTaskID(reattachTarget.SessionID)))
		recordLifecycle(ctx, a.LifecycleSink, a.TaskID, ui.Session.SessionID, PersistedLifecycleReattachRequested, HostStatus{
			Mode:                  reattachTarget.HostMode,
			State:                 reattachTarget.HostState,
			Note:                  fmt.Sprintf("reattach requested using session %s", reattachTarget.SessionID),
			WorkerSessionID:       reattachTarget.WorkerSessionID,
			WorkerSessionIDSource: reattachTarget.WorkerSessionIDSource,
		}, &ui)
	}
	if preferredHost != nil {
		if hostPreference := workerPreferenceFromHost(preferredHost); hostPreference != WorkerPreferenceAuto {
			resolvedWorker = hostPreference
		}
	}
	if preferredHost == nil {
		preferredHost, resolvedWorker, err = selectPreferredHost(requestedPreference, snapshot)
		if err != nil {
			return err
		}
	}
	if shouldReattach && !configureHostResumeSession(preferredHost, reattachTarget.WorkerSessionID) {
		err := fmt.Errorf("shell session %s is attachable but %s host does not support reattach in this runtime", reattachTarget.SessionID, workerPreferenceLabel(resolvedWorker))
		recordLifecycle(ctx, a.LifecycleSink, a.TaskID, ui.Session.SessionID, PersistedLifecycleReattachFailed, HostStatus{
			Mode:                  reattachTarget.HostMode,
			State:                 reattachTarget.HostState,
			Note:                  err.Error(),
			WorkerSessionID:       reattachTarget.WorkerSessionID,
			WorkerSessionIDSource: reattachTarget.WorkerSessionIDSource,
		}, &ui)
		return err
	}
	ui.Session.ResolvedWorker = resolvedWorker
	reportShellSession(a.RegistrySink, a.TaskID, &ui.Session, preferredHost.Status(), true, &ui)

	host, hostErr := startPreferredHost(ctx, preferredHost, a.FallbackHost, snapshot)
	startupLabel := workerPreferenceLabel(resolvedWorker)
	if preferredHost != nil {
		if label := strings.TrimSpace(preferredHost.WorkerLabel()); label != "" {
			startupLabel = label
		}
	}
	addSessionEvent(&ui.Session, ui.LastRefresh, SessionEventHostStartupAttempted, fmt.Sprintf("Attempted %s host startup.", startupLabel))
	if hostErr != "" {
		ui.LastError = hostErr
	}
	defer reportShellSession(a.RegistrySink, a.TaskID, &ui.Session, host.Status(), false, &ui)
	defer func() {
		_ = host.Stop()
	}()

	lastWidth, lastHeight := terminalSize()
	applyHostResize(host, lastWidth, lastHeight, ui)
	initialStatus := host.Status()
	reportShellSession(a.RegistrySink, a.TaskID, &ui.Session, initialStatus, true, &ui)
	captureHostLifecycle(ctx, a.LifecycleSink, a.TaskID, ui.Session.SessionID, &ui, HostStatus{}, initialStatus)

	keyCh := make(chan byte, 16)
	go readKeys(stdinFile, keyCh)
	primaryActionDoneCh := make(chan primaryActionExecutionResult, 1)

	frameTicker := time.NewTicker(100 * time.Millisecond)
	defer frameTicker.Stop()

	tickerInterval := a.RefreshInterval
	if tickerInterval <= 0 {
		tickerInterval = 5 * time.Second
	}
	snapshotTicker := time.NewTicker(tickerInterval)
	defer snapshotTicker.Stop()
	registryTicker := time.NewTicker(shellSessionHeartbeatInterval)
	defer registryTicker.Stop()
	transcriptTicker := time.NewTicker(shellTranscriptFlushInterval)
	defer transcriptTicker.Stop()
	defer flushTranscriptEvidence(a.TaskID, ui.Session.SessionID, host, a.TranscriptSink, &ui)

	ui.ObservedAt = time.Now().UTC()
	if err := renderShell(stdoutFile, snapshot, ui, host); err != nil {
		return err
	}

	lastHostStatus := host.Status()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case key, ok := <-keyCh:
			if !ok {
				return nil
			}
			action := routeKey(&ui, host, key)
			switch action {
			case actionQuit:
				return nil
			case actionRefresh:
				if loadErr := reloadShellSnapshot(a.Source, a.TaskID, host, a.RegistrySource, &snapshot, &ui, true); loadErr != nil {
					ui.LastError = loadErr.Error()
				} else {
					ui.LastError = ""
				}
			case actionStageScratchAdoption:
				if err := stagePendingTaskMessageFromLocalScratch(&ui, snapshot); err != nil {
					ui.LastError = err.Error()
				} else {
					ui.LastError = ""
				}
			case actionEnterPendingTaskMessageEdit:
				if err := enterPendingTaskMessageEditMode(&ui); err != nil {
					ui.LastError = err.Error()
				} else {
					ui.LastError = ""
				}
			case actionSavePendingTaskMessageEdit:
				if err := savePendingTaskMessageEditMode(&ui); err != nil {
					ui.LastError = err.Error()
				} else {
					ui.LastError = ""
				}
			case actionCancelPendingTaskMessageEdit:
				if err := cancelPendingTaskMessageEditMode(&ui); err != nil {
					ui.LastError = err.Error()
				} else {
					ui.LastError = ""
				}
			case actionSendPendingTaskMessage:
				if err := sendPendingTaskMessage(a.MessageSender, a.TaskID, &ui); err != nil {
					ui.LastError = err.Error()
					break
				}
				next, loadErr := a.Source.Load(a.TaskID)
				if loadErr != nil {
					ui.LastRefresh = time.Now().UTC()
					ui.LastError = "task message sent, but shell refresh failed: " + loadErr.Error()
					break
				}
				snapshot = next
				host.UpdateSnapshot(snapshot)
				ui.LastRefresh = time.Now().UTC()
				ui.LastError = ""
				if err := loadKnownShellSessions(a.RegistrySource, a.TaskID, &ui.Session); err != nil {
					ui.LastError = "shell session registry read failed: " + err.Error()
				}
			case actionClearPendingTaskMessage:
				clearPendingTaskMessage(&ui)
			case actionExecutePrimaryOperatorStep:
				if err := startPrimaryOperatorStepExecution(a.ActionExecutor, a.TaskID, snapshot, &ui, primaryActionDoneCh); err != nil {
					ui.LastError = err.Error()
				} else {
					ui.LastError = ""
				}
			}
		case result := <-primaryActionDoneCh:
			if err := completePrimaryOperatorStepExecution(a.Source, a.TaskID, host, a.RegistrySource, &snapshot, &ui, result); err != nil {
				ui.LastError = err.Error()
			} else {
				ui.LastError = ""
			}
		case <-snapshotTicker.C:
			if loadErr := reloadShellSnapshot(a.Source, a.TaskID, host, a.RegistrySource, &snapshot, &ui, false); loadErr != nil {
				ui.LastError = loadErr.Error()
				continue
			}
			ui.LastError = ""
		case <-registryTicker.C:
			reportShellSession(a.RegistrySink, a.TaskID, &ui.Session, host.Status(), true, &ui)
		case <-transcriptTicker.C:
			flushTranscriptEvidence(a.TaskID, ui.Session.SessionID, host, a.TranscriptSink, &ui)
		case <-frameTicker.C:
		}

		width, height := terminalSize()
		if width != lastWidth || height != lastHeight {
			if applyHostResize(host, width, height, ui) {
				addSessionEvent(&ui.Session, time.Now().UTC(), SessionEventResizeApplied, fmt.Sprintf("Resized live worker pane to %dx%d.", host.Status().Width, host.Status().Height))
			}
			lastWidth = width
			lastHeight = height
		}

		currentStatus := host.Status()
		captureHostLifecycle(ctx, a.LifecycleSink, a.TaskID, ui.Session.SessionID, &ui, lastHostStatus, currentStatus)
		if hostStatusChanged(lastHostStatus, currentStatus) {
			reportShellSession(a.RegistrySink, a.TaskID, &ui.Session, currentStatus, true, &ui)
		}
		if nextHost, note, changed := transitionExitedHost(ctx, host, a.FallbackHost, snapshot); changed {
			flushTranscriptEvidence(a.TaskID, ui.Session.SessionID, host, a.TranscriptSink, &ui)
			host = nextHost
			applyHostResize(host, lastWidth, lastHeight, ui)
			if note != "" {
				ui.LastError = note
			}
			captureHostLifecycle(ctx, a.LifecycleSink, a.TaskID, ui.Session.SessionID, &ui, currentStatus, host.Status())
			reportShellSession(a.RegistrySink, a.TaskID, &ui.Session, host.Status(), true, &ui)
		}
		lastHostStatus = host.Status()

		ui.ObservedAt = time.Now().UTC()
		if err := renderShell(stdoutFile, snapshot, ui, host); err != nil {
			return err
		}
	}
}

func initialUIState(now time.Time, preference WorkerPreference) UIState {
	ui := UIState{
		ShowInspector: false,
		ShowProof:     false,
		Focus:         FocusWorker,
		LastRefresh:   now,
		ObservedAt:    now,
		Session:       newSessionState(now),
	}
	ui.Session.WorkerPreference = preference
	return ui
}

type keyAction int

const (
	actionNone keyAction = iota
	actionQuit
	actionRefresh
	actionStageScratchAdoption
	actionEnterPendingTaskMessageEdit
	actionSavePendingTaskMessageEdit
	actionCancelPendingTaskMessageEdit
	actionSendPendingTaskMessage
	actionClearPendingTaskMessage
	actionExecutePrimaryOperatorStep
)

func handleShellKey(ui *UIState, key byte) keyAction {
	switch key {
	case 'q', 'Q', 3:
		return actionQuit
	case 'i', 'I':
		ui.ShowInspector = !ui.ShowInspector
		if !ui.ShowInspector && ui.Focus == FocusInspector {
			ui.Focus = FocusWorker
		}
	case 'p', 'P':
		ui.ShowProof = !ui.ShowProof
		if !ui.ShowProof && ui.Focus == FocusActivity {
			ui.Focus = FocusWorker
		}
	case 'r', 'R':
		return actionRefresh
	case 'a', 'A':
		return actionStageScratchAdoption
	case 'e', 'E':
		return actionEnterPendingTaskMessageEdit
	case 'm', 'M':
		return actionSendPendingTaskMessage
	case 'n', 'N':
		return actionExecutePrimaryOperatorStep
	case 'x', 'X':
		return actionClearPendingTaskMessage
	case 'h', 'H':
		ui.ShowHelp = !ui.ShowHelp
		if ui.ShowHelp {
			ui.ShowStatus = false
		}
	case 's', 'S':
		ui.ShowStatus = !ui.ShowStatus
		if ui.ShowStatus {
			ui.ShowHelp = false
		}
	case '\t':
		ui.Focus = nextFocus(*ui)
	}
	return actionNone
}

func routeKey(ui *UIState, host WorkerHost, key byte) keyAction {
	if ui.PendingTaskMessageEditMode && ui.Focus == FocusWorker {
		return routePendingTaskMessageEditKey(ui, key)
	}
	if host != nil && ui.Focus == FocusWorker {
		if ui.EscapePrefix {
			ui.EscapePrefix = false
			return handleShellKey(ui, key)
		}
		if key == 0x07 {
			ui.EscapePrefix = true
			return actionNone
		}
		if host.CanAcceptInput() && host.WriteInput([]byte{key}) {
			ui.LastError = ""
			return actionNone
		}
		action := handleShellKey(ui, key)
		if action != actionNone {
			return action
		}
		if isPrintableKey(key) {
			ui.LastError = unavailableInputMessage(host.Status())
			return actionNone
		}
	}
	return handleShellKey(ui, key)
}

func routePendingTaskMessageEditKey(ui *UIState, key byte) keyAction {
	if ui.EscapePrefix {
		ui.EscapePrefix = false
		switch key {
		case 's', 'S':
			return actionSavePendingTaskMessageEdit
		case 'c', 'C':
			return actionCancelPendingTaskMessageEdit
		default:
			return handleShellKey(ui, key)
		}
	}
	if key == 0x07 {
		ui.EscapePrefix = true
		return actionNone
	}
	if applyPendingTaskMessageEditInput(ui, key) {
		ui.LastError = ""
	}
	return actionNone
}

func nextFocus(ui UIState) FocusPane {
	order := []FocusPane{FocusWorker}
	if ui.ShowInspector {
		order = append(order, FocusInspector)
	}
	if ui.ShowProof {
		order = append(order, FocusActivity)
	}
	for idx, pane := range order {
		if pane == ui.Focus {
			return order[(idx+1)%len(order)]
		}
	}
	return FocusWorker
}

func renderShell(out io.Writer, snapshot Snapshot, ui UIState, host WorkerHost) error {
	width, height := terminalSize()
	vm := BuildViewModel(snapshot, ui, host, width, height)
	_, err := io.WriteString(out, Render(vm, width, height))
	return err
}

func reloadShellSnapshot(source SnapshotSource, taskID string, host WorkerHost, registrySource SessionRegistrySource, snapshot *Snapshot, ui *UIState, manual bool) error {
	next, err := source.Load(taskID)
	if err != nil {
		return err
	}
	if snapshot != nil {
		*snapshot = next
	}
	if host != nil {
		host.UpdateSnapshot(next)
	}
	if ui != nil {
		ui.LastRefresh = time.Now().UTC()
		if manual {
			addSessionEvent(&ui.Session, ui.LastRefresh, SessionEventManualRefresh, "Manual shell refresh completed.")
		}
		if err := loadKnownShellSessions(registrySource, taskID, &ui.Session); err != nil {
			return fmt.Errorf("shell session registry read failed: %w", err)
		}
	}
	return nil
}

func startPrimaryOperatorStepExecution(executor PrimaryActionExecutor, taskID string, snapshot Snapshot, ui *UIState, done chan<- primaryActionExecutionResult) error {
	if strings.TrimSpace(taskID) == "" {
		return fmt.Errorf("primary operator step cannot run because this shell is not attached to a task")
	}
	if executor == nil {
		return fmt.Errorf("primary operator step cannot run because no shell action executor is configured")
	}
	if done == nil {
		return fmt.Errorf("primary operator step cannot run because no completion channel is configured")
	}
	if ui != nil && ui.PrimaryActionInFlight != nil {
		return fmt.Errorf("primary operator step %s is already in progress", operatorActionDisplayName(ui.PrimaryActionInFlight.Action))
	}
	step, err := executablePrimaryStep(snapshot)
	if err != nil {
		return err
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
	go func() {
		outcome, err := executor.Execute(taskID, snapshot)
		done <- primaryActionExecutionResult{
			outcome:    outcome,
			step:       *step,
			before:     before,
			err:        err,
			finishedAt: time.Now().UTC(),
		}
	}()
	return nil
}

func completePrimaryOperatorStepExecution(source SnapshotSource, taskID string, host WorkerHost, registrySource SessionRegistrySource, snapshot *Snapshot, ui *UIState, result primaryActionExecutionResult) error {
	if snapshot == nil {
		return fmt.Errorf("primary operator step cannot finish because shell snapshot state is unavailable")
	}
	if ui != nil {
		ui.PrimaryActionInFlight = nil
	}
	now := time.Now().UTC()
	if result.err != nil {
		if ui != nil {
			ui.LastPrimaryActionResult = failedPrimaryActionResult(result.step, result.err, result.finishedAt)
			addSessionEvent(&ui.Session, now, SessionEventPrimaryOperatorActionFailed, ui.LastPrimaryActionResult.Summary+". "+truncateWithEllipsis(ui.LastPrimaryActionResult.ErrorText, 96))
		}
		return fmt.Errorf("primary operator step %s failed: %w", strings.ToLower(result.step.Action), result.err)
	}
	if err := reloadShellSnapshot(source, taskID, host, registrySource, snapshot, ui, true); err != nil {
		if ui != nil {
			ui.LastPrimaryActionResult = failedPrimaryActionResult(result.step, fmt.Errorf("primary operator step ran, but shell refresh failed: %w", err), result.finishedAt)
			addSessionEvent(&ui.Session, now, SessionEventPrimaryOperatorActionFailed, ui.LastPrimaryActionResult.Summary+". "+truncateWithEllipsis(ui.LastPrimaryActionResult.ErrorText, 96))
		}
		return fmt.Errorf("primary operator step ran, but shell refresh failed: %w", err)
	}
	if ui != nil {
		if receiptIsFailure(result.outcome.Receipt) {
			ui.LastPrimaryActionResult = failedPrimaryActionResultWithReceipt(result.step, result.outcome.Receipt, result.finishedAt)
		} else {
			ui.LastPrimaryActionResult = successfulPrimaryActionResult(result.step, result.before, *snapshot, result.outcome.Receipt, result.finishedAt)
		}
		summary := ui.LastPrimaryActionResult.Summary
		if next := strings.TrimSpace(ui.LastPrimaryActionResult.NextStep); next != "" && next != "none" {
			summary += ". next " + next
		}
		addSessionEvent(&ui.Session, now, SessionEventPrimaryOperatorActionExecuted, summary)
	}
	if receiptIsFailure(result.outcome.Receipt) {
		return fmt.Errorf("primary operator step %s %s: %s", strings.ToLower(result.step.Action), strings.ToLower(nonEmpty(result.outcome.Receipt.ResultClass, "failed")), nonEmpty(result.outcome.Receipt.Reason, result.outcome.Receipt.Summary))
	}
	return nil
}

func executePrimaryOperatorStep(executor PrimaryActionExecutor, source SnapshotSource, taskID string, host WorkerHost, registrySource SessionRegistrySource, snapshot *Snapshot, ui *UIState) error {
	if snapshot == nil {
		return fmt.Errorf("primary operator step cannot run because shell snapshot state is unavailable")
	}
	done := make(chan primaryActionExecutionResult, 1)
	if err := startPrimaryOperatorStepExecution(executor, taskID, *snapshot, ui, done); err != nil {
		return err
	}
	result := <-done
	return completePrimaryOperatorStepExecution(source, taskID, host, registrySource, snapshot, ui, result)
}

func applyHostResize(host WorkerHost, width int, height int, ui UIState) bool {
	if host == nil {
		return false
	}
	layout := computeShellLayout(width, height, ui)
	paneWidth, paneHeight := layout.workerContentSize()
	return host.Resize(paneWidth, paneHeight)
}

func unavailableInputMessage(status HostStatus) string {
	switch status.State {
	case HostStateFallback:
		return "worker session is in transcript fallback mode; live input is unavailable"
	case HostStateTranscriptOnly:
		return "worker session is transcript-only; live input is unavailable"
	case HostStateExited:
		if status.ExitCode != nil {
			return fmt.Sprintf("worker session exited with code %d; live input is unavailable", *status.ExitCode)
		}
		return "worker session exited; live input is unavailable"
	case HostStateFailed:
		return "worker session failed; live input is unavailable"
	case HostStateStarting:
		return "worker session is still starting; try again in a moment"
	default:
		return "worker input is unavailable"
	}
}

func isPrintableKey(key byte) bool {
	return key >= 32 && key < 127
}

func stagePendingTaskMessageFromLocalScratch(ui *UIState, snapshot Snapshot) error {
	if ui == nil {
		return fmt.Errorf("shell ui state is unavailable")
	}
	draft, err := buildLocalScratchAdoptionDraft(snapshot)
	if err != nil {
		return err
	}
	ui.PendingTaskMessage = draft
	ui.PendingTaskMessageSource = "local_scratch_adoption"
	resetPendingTaskMessageEditMode(ui)
	addSessionEvent(&ui.Session, time.Now().UTC(), SessionEventPendingMessageStaged, "Staged pending task message from local scratch intake notes.")
	return nil
}

func sendPendingTaskMessage(sender TaskMessageSender, taskID string, ui *UIState) error {
	if ui == nil {
		return fmt.Errorf("shell ui state is unavailable")
	}
	if strings.TrimSpace(taskID) == "" {
		return fmt.Errorf("pending task message cannot be sent because this shell is not attached to a task")
	}
	if sender == nil {
		return fmt.Errorf("pending task message cannot be sent because no task-message sender is configured")
	}
	message := currentPendingTaskMessage(*ui)
	if isEffectivelyEmptyPendingTaskMessage(message) {
		return fmt.Errorf("no pending task message is staged")
	}
	if err := sender.Send(taskID, message); err != nil {
		return err
	}
	addSessionEvent(&ui.Session, time.Now().UTC(), SessionEventPendingMessageSent, "Sent pending task message to Tuku canonical continuity.")
	ui.PendingTaskMessage = ""
	ui.PendingTaskMessageSource = ""
	resetPendingTaskMessageEditMode(ui)
	return nil
}

func clearPendingTaskMessage(ui *UIState) {
	if ui == nil {
		return
	}
	if isEffectivelyEmptyPendingTaskMessage(ui.PendingTaskMessage) && !ui.PendingTaskMessageEditMode {
		return
	}
	ui.PendingTaskMessage = ""
	ui.PendingTaskMessageSource = ""
	resetPendingTaskMessageEditMode(ui)
	addSessionEvent(&ui.Session, time.Now().UTC(), SessionEventPendingMessageCleared, "Cleared pending task message.")
}

func enterPendingTaskMessageEditMode(ui *UIState) error {
	if ui == nil {
		return fmt.Errorf("shell ui state is unavailable")
	}
	if ui.PendingTaskMessageEditMode {
		return fmt.Errorf("pending task message edit mode is already active")
	}
	if isEffectivelyEmptyPendingTaskMessage(ui.PendingTaskMessage) {
		return fmt.Errorf("no pending task message is staged")
	}
	ui.PendingTaskMessageEditMode = true
	ui.PendingTaskMessageEditOriginal = ui.PendingTaskMessage
	ui.PendingTaskMessageEditBuffer = ui.PendingTaskMessage
	addSessionEvent(&ui.Session, time.Now().UTC(), SessionEventPendingMessageEditStarted, "Pending task message edit mode is active. Draft changes remain shell-local until explicit send.")
	return nil
}

func savePendingTaskMessageEditMode(ui *UIState) error {
	if ui == nil {
		return fmt.Errorf("shell ui state is unavailable")
	}
	if !ui.PendingTaskMessageEditMode {
		return fmt.Errorf("pending task message edit mode is not active")
	}
	ui.PendingTaskMessage = ui.PendingTaskMessageEditBuffer
	resetPendingTaskMessageEditMode(ui)
	addSessionEvent(&ui.Session, time.Now().UTC(), SessionEventPendingMessageEditSaved, "Saved pending task message edits. Draft remains shell-local until explicit send.")
	return nil
}

func cancelPendingTaskMessageEditMode(ui *UIState) error {
	if ui == nil {
		return fmt.Errorf("shell ui state is unavailable")
	}
	if !ui.PendingTaskMessageEditMode {
		return fmt.Errorf("pending task message edit mode is not active")
	}
	ui.PendingTaskMessage = ui.PendingTaskMessageEditOriginal
	resetPendingTaskMessageEditMode(ui)
	addSessionEvent(&ui.Session, time.Now().UTC(), SessionEventPendingMessageEditCanceled, "Canceled pending task message edits and restored the saved draft.")
	return nil
}

func resetPendingTaskMessageEditMode(ui *UIState) {
	if ui == nil {
		return
	}
	ui.PendingTaskMessageEditMode = false
	ui.PendingTaskMessageEditBuffer = ""
	ui.PendingTaskMessageEditOriginal = ""
}

func currentPendingTaskMessage(ui UIState) string {
	if ui.PendingTaskMessageEditMode {
		return ui.PendingTaskMessageEditBuffer
	}
	return ui.PendingTaskMessage
}

func applyPendingTaskMessageEditInput(ui *UIState, key byte) bool {
	if ui == nil || !ui.PendingTaskMessageEditMode {
		return false
	}
	switch key {
	case '\r', '\n':
		ui.PendingTaskMessageEditBuffer += "\n"
		return true
	case 0x7f, 0x08:
		if ui.PendingTaskMessageEditBuffer == "" {
			return true
		}
		_, size := utf8.DecodeLastRuneInString(ui.PendingTaskMessageEditBuffer)
		if size <= 0 {
			return true
		}
		ui.PendingTaskMessageEditBuffer = ui.PendingTaskMessageEditBuffer[:len(ui.PendingTaskMessageEditBuffer)-size]
		return true
	default:
		if key >= 32 {
			ui.PendingTaskMessageEditBuffer += string(key)
			return true
		}
	}
	return false
}

func isEffectivelyEmptyPendingTaskMessage(message string) bool {
	return strings.TrimSpace(message) == ""
}

func buildLocalScratchAdoptionDraft(snapshot Snapshot) (string, error) {
	if !snapshot.HasLocalScratchAdoption() {
		return "", fmt.Errorf("no surfaced local scratch notes are available to stage")
	}
	lines := []string{
		"Explicitly adopt these local scratch intake notes into this repo-backed Tuku task:",
		"",
	}
	for _, note := range snapshot.LocalScratch.Notes {
		body := strings.TrimSpace(note.Body)
		if body == "" {
			continue
		}
		lines = append(lines, "- "+body)
	}
	lines = append(lines,
		"",
		"These notes came from local scratch history for this repo root. I am explicitly adopting them into canonical task continuity now.",
	)
	return strings.Join(lines, "\n"), nil
}

func hostStatusChanged(previous HostStatus, current HostStatus) bool {
	if previous.Mode != current.Mode {
		return true
	}
	if previous.State != current.State {
		return true
	}
	if previous.InputLive != current.InputLive {
		return true
	}
	if previous.Note != current.Note {
		return true
	}
	if previous.WorkerSessionID != current.WorkerSessionID {
		return true
	}
	if previous.WorkerSessionIDSource != current.WorkerSessionIDSource {
		return true
	}
	if previous.Width != current.Width || previous.Height != current.Height {
		return true
	}
	switch {
	case previous.ExitCode == nil && current.ExitCode == nil:
		return false
	case previous.ExitCode == nil || current.ExitCode == nil:
		return true
	default:
		return *previous.ExitCode != *current.ExitCode
	}
}

func captureHostLifecycle(ctx context.Context, sink LifecycleSink, taskID string, sessionID string, ui *UIState, previous HostStatus, current HostStatus) {
	now := time.Now().UTC()
	switch current.State {
	case HostStateLive:
		if previous.State != HostStateLive {
			addSessionEvent(&ui.Session, now, SessionEventHostLive, "Live worker host is active.")
			recordLifecycle(ctx, sink, taskID, sessionID, PersistedLifecycleHostStarted, current, ui)
		}
	case HostStateExited:
		if previous.State != HostStateExited {
			summary := "Live worker host ended."
			if current.ExitCode != nil {
				summary = fmt.Sprintf("Live worker host ended with exit code %d.", *current.ExitCode)
			}
			addSessionEvent(&ui.Session, now, SessionEventHostExited, summary)
			recordLifecycle(ctx, sink, taskID, sessionID, PersistedLifecycleHostExited, current, ui)
		}
	case HostStateFailed:
		if previous.State != HostStateFailed {
			summary := "Live worker host failed."
			if current.Note != "" {
				summary = "Live worker host failed: " + current.Note
			}
			addSessionEvent(&ui.Session, now, SessionEventHostFailed, summary)
			recordLifecycle(ctx, sink, taskID, sessionID, PersistedLifecycleHostExited, current, ui)
		}
	case HostStateFallback:
		if previous.State != HostStateFallback {
			summary := "Transcript fallback is active."
			if current.Note != "" {
				summary = current.Note
			}
			addSessionEvent(&ui.Session, now, SessionEventFallbackActivated, summary)
			recordLifecycle(ctx, sink, taskID, sessionID, PersistedLifecycleFallback, current, ui)
		}
	}
}

func recordLifecycle(ctx context.Context, sink LifecycleSink, taskID string, sessionID string, kind PersistedLifecycleKind, status HostStatus, ui *UIState) {
	if sink == nil {
		return
	}
	if ui != nil {
		if strings.TrimSpace(status.WorkerSessionID) == "" {
			status.WorkerSessionID = strings.TrimSpace(ui.Session.WorkerSessionID)
		}
		status.WorkerSessionIDSource = normalizeWorkerSessionIDSource(status.WorkerSessionIDSource, status.WorkerSessionID)
	}
	if err := sink.Record(taskID, sessionID, kind, status); err != nil {
		ui.LastError = "shell lifecycle proof bridge failed: " + err.Error()
	}
}

func resolveReattachTarget(targetSessionID string, known []KnownShellSession) (KnownShellSession, bool, error) {
	targetSessionID = strings.TrimSpace(targetSessionID)
	if targetSessionID == "" {
		return KnownShellSession{}, false, nil
	}
	for _, session := range known {
		if session.SessionID != targetSessionID {
			continue
		}
		if session.SessionClass == KnownShellSessionClassEnded {
			return KnownShellSession{}, false, fmt.Errorf("shell session %s is ended; reattach is unavailable", targetSessionID)
		}
		if session.SessionClass == KnownShellSessionClassStale {
			return KnownShellSession{}, false, fmt.Errorf("shell session %s is stale (last updated %s); live continuity is not trusted for reattach", targetSessionID, session.LastUpdatedAt.Format(time.RFC3339))
		}
		if strings.TrimSpace(session.WorkerSessionID) == "" {
			return KnownShellSession{}, false, fmt.Errorf("shell session %s has no worker session id; reattach requires an authoritative worker session id", targetSessionID)
		}
		if session.WorkerSessionIDSource != WorkerSessionIDSourceAuthoritative {
			return KnownShellSession{}, false, fmt.Errorf("shell session %s only has a %s worker session id; reattach requires an authoritative id", targetSessionID, nonEmpty(string(session.WorkerSessionIDSource), "unknown"))
		}
		if session.AttachCapability != WorkerAttachCapabilityAttachable {
			return KnownShellSession{}, false, fmt.Errorf("shell session %s reports attach capability %s; host/worker attach is unsupported", targetSessionID, nonEmpty(string(session.AttachCapability), "none"))
		}
		if session.SessionClass != KnownShellSessionClassAttachable {
			reason := strings.TrimSpace(session.SessionClassReason)
			if reason == "" {
				reason = fmt.Sprintf("class=%s state=%s", session.SessionClass, session.HostState)
			}
			return KnownShellSession{}, false, fmt.Errorf("shell session %s is not attachable: %s", targetSessionID, reason)
		}
		return session, true, nil
	}
	return KnownShellSession{}, false, fmt.Errorf("shell session %s was not found in the durable registry for this task", targetSessionID)
}

func sessionPreferredWorker(session KnownShellSession) WorkerPreference {
	if session.ResolvedWorker == WorkerPreferenceCodex || session.ResolvedWorker == WorkerPreferenceClaude {
		return session.ResolvedWorker
	}
	if session.WorkerPreference == WorkerPreferenceCodex || session.WorkerPreference == WorkerPreferenceClaude {
		return session.WorkerPreference
	}
	return WorkerPreferenceAuto
}

func configureHostResumeSession(host WorkerHost, workerSessionID string) bool {
	if host == nil || strings.TrimSpace(workerSessionID) == "" {
		return false
	}
	resumeConfigurable, ok := host.(interface{ SetResumeSessionID(sessionID string) })
	if !ok {
		return false
	}
	resumeConfigurable.SetResumeSessionID(strings.TrimSpace(workerSessionID))
	return true
}

func readKeys(in io.Reader, out chan<- byte) {
	buf := make([]byte, 1)
	for {
		n, err := in.Read(buf)
		if err != nil || n == 0 {
			close(out)
			return
		}
		out <- buf[0]
	}
}

func flushTranscriptEvidence(taskID string, sessionID string, host WorkerHost, sink TranscriptSink, ui *UIState) {
	if sink == nil || host == nil {
		return
	}
	provider, ok := host.(TranscriptProvider)
	if !ok {
		return
	}
	chunks := provider.DrainTranscriptEvidence(80)
	if len(chunks) == 0 {
		return
	}
	if err := sink.Append(taskID, sessionID, chunks); err != nil && ui != nil {
		ui.LastError = "shell transcript evidence append failed: " + err.Error()
	}
}

func enterTerminalMode(stdin *os.File, stdout *os.File) (func(), error) {
	fd := int(stdin.Fd())
	termios, err := ioctlGetTermios(fd)
	if err != nil {
		return nil, fmt.Errorf("read terminal attrs: %w", err)
	}
	raw := *termios
	raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	raw.Oflag &^= unix.OPOST
	raw.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	raw.Cflag &^= unix.CSIZE | unix.PARENB
	raw.Cflag |= unix.CS8
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := ioctlSetTermios(fd, &raw); err != nil {
		return nil, fmt.Errorf("set raw terminal attrs: %w", err)
	}
	if _, err := io.WriteString(stdout, "\x1b[?1049h\x1b[?25l"); err != nil {
		return nil, err
	}
	return func() {
		_ = ioctlSetTermios(fd, termios)
		_, _ = io.WriteString(stdout, "\x1b[?25h\x1b[?1049l")
	}, nil
}

func terminalSize() (int, int) {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 || ws.Row == 0 {
		return 120, 32
	}
	return int(ws.Col), int(ws.Row)
}
