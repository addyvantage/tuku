package shell

import (
	"strings"
	"testing"
	"time"
)

type stubSessionRegistrySink struct {
	reports []SessionRegistryReport
	err     error
}

func (s *stubSessionRegistrySink) Report(report SessionRegistryReport) error {
	if s.err != nil {
		return s.err
	}
	s.reports = append(s.reports, report)
	return nil
}

type stubSessionRegistrySource struct {
	sessions []KnownShellSession
	err      error
}

func (s *stubSessionRegistrySource) List(_ string) ([]KnownShellSession, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]KnownShellSession{}, s.sessions...), nil
}

func TestReportShellSessionRegistersOnStartup(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	sink := &stubSessionRegistrySink{}
	session := newSessionState(now)
	session.WorkerPreference = WorkerPreferenceAuto
	session.ResolvedWorker = WorkerPreferenceClaude
	ui := UIState{Session: session}

	reportShellSession(sink, "tsk_1", &ui.Session, HostStatus{Mode: HostModeClaudePTY, State: HostStateStarting}, true, &ui)

	if len(sink.reports) != 1 {
		t.Fatalf("expected one session registry report, got %d", len(sink.reports))
	}
	if sink.reports[0].SessionID != session.SessionID || sink.reports[0].ResolvedWorker != WorkerPreferenceClaude {
		t.Fatalf("unexpected session startup report: %+v", sink.reports[0])
	}
}

func TestReportShellSessionUpdatesOnHostTransitions(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	sink := &stubSessionRegistrySink{}
	session := newSessionState(now)
	session.WorkerPreference = WorkerPreferenceCodex
	session.ResolvedWorker = WorkerPreferenceCodex
	ui := UIState{Session: session}

	reportShellSession(sink, "tsk_2", &ui.Session, HostStatus{Mode: HostModeCodexPTY, State: HostStateLive}, true, &ui)
	reportShellSession(sink, "tsk_2", &ui.Session, HostStatus{Mode: HostModeTranscript, State: HostStateFallback, Note: "fallback active"}, true, &ui)

	if len(sink.reports) != 2 {
		t.Fatalf("expected two session registry reports, got %d", len(sink.reports))
	}
	if sink.reports[1].HostState != HostStateFallback || sink.reports[1].HostMode != HostModeTranscript {
		t.Fatalf("expected fallback registry report, got %+v", sink.reports[1])
	}
}

func TestReportShellSessionMarksEndedOnShellExit(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	sink := &stubSessionRegistrySink{}
	session := newSessionState(now)
	ui := UIState{Session: session}

	reportShellSession(sink, "tsk_3", &ui.Session, HostStatus{Mode: HostModeCodexPTY, State: HostStateLive}, false, &ui)

	if len(sink.reports) != 1 {
		t.Fatalf("expected one session registry report, got %d", len(sink.reports))
	}
	if sink.reports[0].Active {
		t.Fatal("expected shell session to be marked ended")
	}
	if sink.reports[0].Note != "shell session ended" {
		t.Fatalf("expected ended note, got %q", sink.reports[0].Note)
	}
}

func TestLoadKnownShellSessionsReadsKnownSessions(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	source := &stubSessionRegistrySource{sessions: []KnownShellSession{{SessionID: "shs_1", SessionClass: KnownShellSessionClassAttachable, Active: true, LastUpdatedAt: now}}}
	session := newSessionState(now)

	if err := loadKnownShellSessions(source, "tsk_4", &session); err != nil {
		t.Fatalf("load known shell sessions: %v", err)
	}
	if len(session.KnownSessions) != 1 || session.KnownSessions[0].SessionID != "shs_1" {
		t.Fatalf("unexpected known sessions: %+v", session.KnownSessions)
	}
}

func TestSessionRegistrySummaryShowsAnotherKnownSession(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	session := newSessionState(now)
	session.KnownSessions = []KnownShellSession{
		{SessionID: session.SessionID, SessionClass: KnownShellSessionClassAttachable, Active: true, LastUpdatedAt: now},
		{SessionID: "shs_other", SessionClass: KnownShellSessionClassAttachable, Active: true, ResolvedWorker: WorkerPreferenceClaude, HostState: HostStateLive, LastUpdatedAt: now.Add(time.Second)},
	}

	summary := sessionRegistrySummary(session)
	if !strings.Contains(summary, "attachable session known") {
		t.Fatalf("expected another-session summary, got %q", summary)
	}
}

func TestSessionRegistrySummaryShowsStaleSession(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	session := newSessionState(now)
	session.KnownSessions = []KnownShellSession{
		{SessionID: session.SessionID, SessionClass: KnownShellSessionClassAttachable, Active: true, LastUpdatedAt: now},
		{SessionID: "shs_stale", SessionClass: KnownShellSessionClassStale, Active: true, ResolvedWorker: WorkerPreferenceClaude, HostState: HostStateLive, LastUpdatedAt: now.Add(-2 * time.Minute)},
	}

	summary := sessionRegistrySummary(session)
	if !strings.Contains(summary, "stale session known") {
		t.Fatalf("expected stale-session summary, got %q", summary)
	}
}

func TestSessionRegistrySummaryShowsPriorEndedSession(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	session := newSessionState(now)
	session.KnownSessions = []KnownShellSession{
		{SessionID: session.SessionID, SessionClass: KnownShellSessionClassAttachable, Active: true, LastUpdatedAt: now},
		{SessionID: "shs_ended", SessionClass: KnownShellSessionClassEnded, Active: false, ResolvedWorker: WorkerPreferenceCodex, HostState: HostStateFallback, LastUpdatedAt: now.Add(-time.Minute)},
	}

	summary := sessionRegistrySummary(session)
	if !strings.Contains(summary, "prior ended session known") {
		t.Fatalf("expected prior-ended summary, got %q", summary)
	}
}

func TestOtherKnownShellSessionsExcludeCurrentSession(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	session := newSessionState(now)
	session.KnownSessions = []KnownShellSession{
		{SessionID: session.SessionID, SessionClass: KnownShellSessionClassAttachable, Active: true, LastUpdatedAt: now},
		{SessionID: "shs_other", SessionClass: KnownShellSessionClassEnded, Active: false, LastUpdatedAt: now.Add(time.Second)},
	}

	others := otherKnownShellSessions(session)
	if len(others) != 1 || others[0].SessionID != "shs_other" {
		t.Fatalf("expected current session to be excluded, got %+v", others)
	}
}

func TestSessionRegistrySummaryShowsActiveUnattachableSession(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	session := newSessionState(now)
	session.KnownSessions = []KnownShellSession{
		{SessionID: session.SessionID, SessionClass: KnownShellSessionClassAttachable, Active: true, LastUpdatedAt: now},
		{SessionID: "shs_unattachable", SessionClass: KnownShellSessionClassActiveUnattachable, Active: true, ResolvedWorker: WorkerPreferenceClaude, HostState: HostStateFallback, LastUpdatedAt: now.Add(-time.Second)},
	}

	summary := sessionRegistrySummary(session)
	if !strings.Contains(summary, "active non-attachable session known") {
		t.Fatalf("expected active-unattachable summary, got %q", summary)
	}
}

func TestRefreshWorkerSessionAnchorRequiresAuthoritativeIDForAttachable(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	session := newSessionState(now)

	refreshWorkerSessionAnchor(&session, HostStatus{
		Mode:                  HostModeCodexPTY,
		State:                 HostStateLive,
		WorkerSessionID:       "wks_heuristic",
		WorkerSessionIDSource: WorkerSessionIDSourceHeuristic,
	})
	if session.AttachCapability != WorkerAttachCapabilityNone {
		t.Fatalf("expected non-authoritative worker session id to remain non-attachable, got %s", session.AttachCapability)
	}

	refreshWorkerSessionAnchor(&session, HostStatus{
		Mode:                  HostModeCodexPTY,
		State:                 HostStateLive,
		WorkerSessionID:       "wks_auth",
		WorkerSessionIDSource: WorkerSessionIDSourceAuthoritative,
	})
	if session.AttachCapability != WorkerAttachCapabilityAttachable {
		t.Fatalf("expected authoritative worker session id to become attachable, got %s", session.AttachCapability)
	}
}
