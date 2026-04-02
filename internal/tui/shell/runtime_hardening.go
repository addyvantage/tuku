package shell

import (
	"fmt"
	"strings"
	"time"
)

const (
	shellFeedFallbackLineLimit  = 400
	shellWorkerSettleGrace      = 12 * time.Second
	shellWorkerAwaitSignalGrace = 60 * time.Second
	shellWorkerSilentNotice     = 20 * time.Second
	shellWorkerStaleNotice      = 90 * time.Second
)

type shellHistoryProvider interface {
	HistoryLines(width int) []string
}

type shellRawHistoryProvider interface {
	RawHistoryLines() []string
}

type workerTurnAssessment struct {
	StatusLabel string
	Hint        string
	Tone        string
	WorkingLine string
	Active      bool
	Stale       bool
	ClearNote   string
}

func shellHistoryLines(host WorkerHost, width int) []string {
	if host == nil {
		return nil
	}
	width = max(10, width)
	if history, ok := host.(shellHistoryProvider); ok {
		return history.HistoryLines(width)
	}
	return host.Lines(shellFeedFallbackLineLimit, width)
}

func shellRenderableHistoryLines(host WorkerHost, width int) []string {
	if host == nil {
		return nil
	}
	if raw, ok := host.(shellRawHistoryProvider); ok {
		if lines := raw.RawHistoryLines(); len(lines) > 0 {
			return lines
		}
	}
	return shellHistoryLines(host, width)
}

func assessWorkerTurn(status HostStatus, ui UIState, canInterrupt bool, authoritative bool, now time.Time) workerTurnAssessment {
	assessment := workerTurnAssessment{
		StatusLabel: "worker running",
		Tone:        "caution",
		Active:      true,
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if !ui.WorkerPromptPending {
		assessment.Active = false
		return assessment
	}

	promptAt := ui.LastWorkerPromptAt
	if promptAt.IsZero() {
		promptAt = now
	}
	elapsed := elapsedSince(now, promptAt)

	lastOutputAt := workerSignalAfterPrompt(status.LastOutputAt, promptAt)
	lastActivityAt := workerSignalAfterPrompt(status.LastActivityAt, promptAt)
	responseStarted := ui.WorkerResponseStarted || !lastOutputAt.IsZero()

	lastSignalAt := lastActivityAt
	lastSignalLabel := "activity"
	if lastSignalAt.IsZero() || (!lastOutputAt.IsZero() && lastOutputAt.After(lastSignalAt)) {
		lastSignalAt = lastOutputAt
		lastSignalLabel = "output"
	}

	if status.State != HostStateLive {
		assessment.Active = false
		assessment.StatusLabel = "worker state changed"
		assessment.Hint = "The live worker host is no longer active. Tuku is reconciling the shell state."
		assessment.WorkingLine = renderWorkingLine(elapsed, nil, canInterrupt, false)
		return assessment
	}

	switch {
	case !lastSignalAt.IsZero():
		quietFor := elapsedSince(now, lastSignalAt)
		if authoritative {
			assessment.Active = true
		} else {
			assessment.Active = quietFor <= shellWorkerSettleGrace
		}
		switch {
		case quietFor < shellWorkerSilentNotice:
			if responseStarted && lastSignalLabel == "output" {
				assessment.StatusLabel = "worker running"
				assessment.Hint = "The live worker is still responding. New prompts stay paused until the turn settles."
			} else {
				assessment.StatusLabel = "worker running"
				assessment.Hint = "The live worker is active, but no visible reply has landed yet."
			}
		case quietFor < shellWorkerStaleNotice || authoritative:
			assessment.StatusLabel = "worker silent"
			assessment.Hint = fmt.Sprintf("The live worker is quiet right now. Last %s was %s ago.", lastSignalLabel, formatElapsed(quietFor))
		default:
			assessment.StatusLabel = "state may be stale"
			assessment.Tone = "caution"
			assessment.Stale = true
			assessment.Hint = fmt.Sprintf("No new worker %s arrived for %s. Refresh or inspect if the worker still appears busy.", lastSignalLabel, formatElapsed(quietFor))
		}
		if !assessment.Active && !authoritative && !responseStarted {
			assessment.ClearNote = fmt.Sprintf("Cleared the live-worker running state after %s without visible output. Last worker activity was %s ago.", formatElapsed(elapsed), formatElapsed(quietFor))
		}
		fragments := []string{}
		if responseStarted && !lastOutputAt.IsZero() {
			fragments = append(fragments, "last output "+formatElapsed(elapsedSince(now, lastOutputAt))+" ago")
		} else {
			fragments = append(fragments, "last activity "+formatElapsed(elapsedSince(now, lastSignalAt))+" ago")
		}
		if assessment.Stale {
			fragments = append(fragments, "state may be stale")
		}
		assessment.WorkingLine = renderWorkingLine(elapsed, fragments, canInterrupt, ui.WorkerInterruptRequested)
		return assessment
	default:
		waitingFor := elapsed
		clearGrace := shellWorkerAwaitSignalGrace
		if responseStarted {
			clearGrace = shellWorkerSettleGrace
		}
		if authoritative {
			assessment.Active = true
		} else {
			assessment.Active = waitingFor <= clearGrace
		}
		switch {
		case responseStarted && waitingFor < shellWorkerSettleGrace:
			assessment.StatusLabel = "worker running"
			assessment.Hint = "Waiting for the live worker to settle after the latest visible output."
		case !responseStarted && waitingFor < shellWorkerSilentNotice:
			assessment.StatusLabel = "worker running"
			assessment.Hint = "Waiting for the live worker response. No visible worker activity has landed yet."
		case !responseStarted && waitingFor < shellWorkerStaleNotice:
			assessment.StatusLabel = "worker silent"
			assessment.Hint = fmt.Sprintf("No worker output or activity has been observed for %s.", formatElapsed(waitingFor))
		default:
			assessment.StatusLabel = "state may be stale"
			assessment.Stale = true
			if responseStarted {
				assessment.Hint = fmt.Sprintf("The live worker has been silent for %s since the last visible output. Refresh or inspect if it still appears busy.", formatElapsed(waitingFor))
			} else {
				assessment.Hint = fmt.Sprintf("No worker output or activity has been observed for %s. Refresh or inspect if the worker still appears busy.", formatElapsed(waitingFor))
			}
		}
		if !assessment.Active && !authoritative {
			if responseStarted {
				assessment.ClearNote = fmt.Sprintf("Cleared the live-worker running state after %s without any newer worker activity since the last visible output.", formatElapsed(waitingFor))
			} else {
				assessment.ClearNote = fmt.Sprintf("Cleared the live-worker running state after %s without any worker activity.", formatElapsed(waitingFor))
			}
		}
		fragments := []string{"no worker activity " + formatElapsed(waitingFor)}
		if assessment.Stale {
			fragments = append(fragments, "state may be stale")
		}
		assessment.WorkingLine = renderWorkingLine(elapsed, fragments, canInterrupt, ui.WorkerInterruptRequested)
		return assessment
	}
}

func workerSignalAfterPrompt(signalAt time.Time, promptAt time.Time) time.Time {
	if signalAt.IsZero() {
		return time.Time{}
	}
	if !promptAt.IsZero() && signalAt.Before(promptAt) {
		return time.Time{}
	}
	return signalAt
}

func renderWorkingLine(elapsed time.Duration, fragments []string, canInterrupt bool, interruptRequested bool) string {
	parts := []string{formatShellElapsed(elapsed)}
	for _, fragment := range fragments {
		if trimmed := strings.TrimSpace(fragment); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	if interruptRequested {
		parts = append(parts, "interrupt sent")
	} else if canInterrupt {
		parts = append(parts, "Esc to interrupt")
	}
	return "Working (" + strings.Join(parts, " • ") + ")"
}
