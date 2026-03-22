package shell

import (
	"testing"
	"time"
)

func TestNewSessionStateCreatesStableSessionID(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	state := newSessionState(now)
	if state.SessionID == "" {
		t.Fatal("expected session id")
	}
	if state.StartedAt != now {
		t.Fatalf("expected started-at %v, got %v", now, state.StartedAt)
	}

	sessionID := state.SessionID
	addSessionEvent(&state, now.Add(time.Second), SessionEventShellStarted, "Shell session started.")
	addSessionEvent(&state, now.Add(2*time.Second), SessionEventManualRefresh, "Manual refresh completed.")

	if state.SessionID != sessionID {
		t.Fatalf("expected stable session id %q, got %q", sessionID, state.SessionID)
	}
}

func TestSessionJournalRecordsRecentEntries(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	state := newSessionState(now)
	addSessionEvent(&state, now, SessionEventShellStarted, "one")
	addSessionEvent(&state, now.Add(time.Second), SessionEventHostStartupAttempted, "two")
	addSessionEvent(&state, now.Add(2*time.Second), SessionEventHostLive, "three")

	recent := recentSessionEvents(state, 2)
	if len(recent) != 2 {
		t.Fatalf("expected two recent events, got %d", len(recent))
	}
	if recent[0].Summary != "two" || recent[1].Summary != "three" {
		t.Fatalf("unexpected recent journal slice %#v", recent)
	}
}

func TestCapturePriorPersistedShellOutcomePrefersLatestShellLifecycleProof(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	snapshot := Snapshot{
		RecentProofs: []ProofItem{
			{ID: "evt_1", Type: "CHECKPOINT_CREATED", Summary: "Checkpoint created", Timestamp: now},
			{ID: "evt_2", Type: "SHELL_HOST_EXITED", Summary: "Shell live host ended", Timestamp: now.Add(time.Second)},
			{ID: "evt_3", Type: "SHELL_FALLBACK_ACTIVATED", Summary: "Shell transcript fallback activated", Timestamp: now.Add(2 * time.Second)},
		},
	}

	got := capturePriorPersistedShellOutcome(snapshot)
	if got != "Shell transcript fallback activated" {
		t.Fatalf("expected latest persisted shell outcome, got %q", got)
	}
}
