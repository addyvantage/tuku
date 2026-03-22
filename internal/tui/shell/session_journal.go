package shell

import (
	"fmt"
	"strings"
	"time"
)

const maxSessionJournalEntries = 40

func newSessionState(now time.Time) SessionState {
	return SessionState{
		SessionID:        fmt.Sprintf("shs_%d", now.UTC().UnixNano()),
		StartedAt:        now.UTC(),
		AttachCapability: WorkerAttachCapabilityNone,
		Journal:          make([]SessionEvent, 0, 8),
	}
}

func newWorkerSessionID(now time.Time) string {
	return fmt.Sprintf("wks_%d", now.UTC().UnixNano())
}

func addSessionEvent(state *SessionState, now time.Time, typ SessionEventType, summary string) {
	if state == nil {
		return
	}
	state.Journal = append(state.Journal, SessionEvent{
		Type:      typ,
		Summary:   strings.TrimSpace(summary),
		CreatedAt: now.UTC(),
	})
	if len(state.Journal) > maxSessionJournalEntries {
		state.Journal = state.Journal[len(state.Journal)-maxSessionJournalEntries:]
	}
}

func recentSessionEvents(state SessionState, limit int) []SessionEvent {
	if limit <= 0 || limit >= len(state.Journal) {
		return append([]SessionEvent{}, state.Journal...)
	}
	return append([]SessionEvent{}, state.Journal[len(state.Journal)-limit:]...)
}

func capturePriorPersistedShellOutcome(snapshot Snapshot) string {
	var latest *ProofItem
	for i := 0; i < len(snapshot.RecentProofs); i++ {
		switch snapshot.RecentProofs[i].Type {
		case "SHELL_HOST_STARTED", "SHELL_HOST_EXITED", "SHELL_FALLBACK_ACTIVATED":
			evt := snapshot.RecentProofs[i]
			if latest == nil || evt.Timestamp.After(latest.Timestamp) {
				latest = &evt
			}
		}
	}
	if latest == nil {
		return ""
	}
	return latest.Summary
}
