package shell

import (
	"context"
	"fmt"
	"strings"
)

type WorkerPreference string

const (
	WorkerPreferenceAuto   WorkerPreference = "auto"
	WorkerPreferenceCodex  WorkerPreference = "codex"
	WorkerPreferenceClaude WorkerPreference = "claude"
)

func ParseWorkerPreference(raw string) (WorkerPreference, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(WorkerPreferenceAuto):
		return WorkerPreferenceAuto, nil
	case string(WorkerPreferenceCodex):
		return WorkerPreferenceCodex, nil
	case string(WorkerPreferenceClaude):
		return WorkerPreferenceClaude, nil
	default:
		return "", fmt.Errorf("unsupported worker %q (expected auto, codex, or claude)", raw)
	}
}

func resolveWorkerPreference(preference WorkerPreference, snapshot Snapshot) WorkerPreference {
	switch preference {
	case WorkerPreferenceCodex, WorkerPreferenceClaude:
		return preference
	}
	if strings.EqualFold(snapshot.RunWorkerKind(), string(WorkerPreferenceClaude)) {
		return WorkerPreferenceClaude
	}
	if strings.EqualFold(snapshot.HandoffTargetWorker(), string(WorkerPreferenceClaude)) {
		return WorkerPreferenceClaude
	}
	return WorkerPreferenceCodex
}

func selectPreferredHost(preference WorkerPreference, snapshot Snapshot) (WorkerHost, WorkerPreference, error) {
	resolved := resolveWorkerPreference(preference, snapshot)
	switch resolved {
	case WorkerPreferenceCodex:
		return NewDefaultCodexPTYHost(), resolved, nil
	case WorkerPreferenceClaude:
		return NewDefaultClaudePTYHost(), resolved, nil
	default:
		return nil, "", fmt.Errorf("unsupported resolved worker preference %q", resolved)
	}
}

func workerPreferenceLabel(preference WorkerPreference) string {
	switch preference {
	case WorkerPreferenceClaude:
		return "Claude"
	default:
		return "Codex"
	}
}

func workerPreferenceFromHost(host WorkerHost) WorkerPreference {
	if host == nil {
		return WorkerPreferenceAuto
	}
	switch host.Status().Mode {
	case HostModeClaudePTY:
		return WorkerPreferenceClaude
	case HostModeCodexPTY:
		return WorkerPreferenceCodex
	default:
		return WorkerPreferenceAuto
	}
}

func startPreferredHost(ctx context.Context, preferred WorkerHost, fallback WorkerHost, snapshot Snapshot) (WorkerHost, string) {
	if fallback == nil {
		fallback = NewTranscriptHost()
	}
	markTranscriptOnly(fallback, "")
	fallback.UpdateSnapshot(snapshot)
	if err := fallback.Start(ctx, snapshot); err != nil {
		return fallback, err.Error()
	}
	if preferred == nil {
		return fallback, ""
	}
	preferred.UpdateSnapshot(snapshot)
	if err := preferred.Start(ctx, snapshot); err != nil {
		note := fmt.Sprintf("%s PTY host unavailable, using transcript fallback: %v", workerPreferenceLabel(workerPreferenceFromHost(preferred)), err)
		markFallback(fallback, note)
		return fallback, note
	}
	return preferred, ""
}

func transitionExitedHost(ctx context.Context, current WorkerHost, fallback WorkerHost, snapshot Snapshot) (WorkerHost, string, bool) {
	if current == nil {
		return fallback, "", false
	}
	status := current.Status()
	if status.State != HostStateExited && status.State != HostStateFailed {
		return current, "", false
	}
	if fallback == nil {
		fallback = NewTranscriptHost()
	}
	fallback.UpdateSnapshot(snapshot)
	if err := fallback.Start(ctx, snapshot); err != nil {
		return current, fmt.Sprintf("worker host exited and transcript fallback failed to start: %v", err), false
	}
	note := fallbackNote(status)
	markFallback(fallback, note)
	return fallback, note, true
}

func fallbackNote(status HostStatus) string {
	base := strings.TrimSpace(status.Note)
	switch status.State {
	case HostStateFailed:
		if base != "" {
			return "live worker failed; switched to transcript fallback: " + base
		}
		return "live worker failed; switched to transcript fallback"
	case HostStateExited:
		if status.ExitCode != nil {
			if base != "" {
				return fmt.Sprintf("live worker exited with code %d; switched to transcript fallback: %s", *status.ExitCode, base)
			}
			return fmt.Sprintf("live worker exited with code %d; switched to transcript fallback", *status.ExitCode)
		}
		if base != "" {
			return "live worker exited; switched to transcript fallback: " + base
		}
		return "live worker exited; switched to transcript fallback"
	default:
		return "switched to transcript fallback"
	}
}

func markFallback(host WorkerHost, note string) {
	if transcript, ok := host.(*TranscriptHost); ok {
		transcript.markFallback(note)
	}
}

func markTranscriptOnly(host WorkerHost, note string) {
	if transcript, ok := host.(*TranscriptHost); ok {
		transcript.markTranscriptOnly(note)
	}
}
