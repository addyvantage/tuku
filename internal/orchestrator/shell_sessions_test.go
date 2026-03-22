package orchestrator

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/shellsession"
	"tuku/internal/response/canonical"
	"tuku/internal/storage/sqlite"
)

type recordingShellSessionRegistry struct {
	upserts []ShellSessionRecord
	list    []ShellSessionRecord
}

func (r *recordingShellSessionRegistry) Upsert(record ShellSessionRecord) error {
	r.upserts = append(r.upserts, record)
	found := false
	for i := range r.list {
		if r.list[i].TaskID == record.TaskID && r.list[i].SessionID == record.SessionID {
			r.list[i] = record
			found = true
			break
		}
	}
	if !found {
		r.list = append(r.list, record)
	}
	return nil
}

func (r *recordingShellSessionRegistry) ListByTask(_ common.TaskID) ([]ShellSessionRecord, error) {
	return append([]ShellSessionRecord{}, r.list...), nil
}

func TestMemoryShellSessionRegistryUpsertAndListByTask(t *testing.T) {
	registry := NewMemoryShellSessionRegistry()
	now := time.Unix(1710000000, 0).UTC()

	if err := registry.Upsert(ShellSessionRecord{TaskID: "tsk_1", SessionID: "shs_ended", StartedAt: now, LastUpdatedAt: now, Active: false}); err != nil {
		t.Fatalf("upsert ended session: %v", err)
	}
	if err := registry.Upsert(ShellSessionRecord{TaskID: "tsk_1", SessionID: "shs_live", StartedAt: now, LastUpdatedAt: now.Add(time.Second), Active: true}); err != nil {
		t.Fatalf("upsert live session: %v", err)
	}

	sessions, err := registry.ListByTask("tsk_1")
	if err != nil {
		t.Fatalf("list shell sessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected two shell sessions, got %d", len(sessions))
	}
}

func TestNewCoordinatorRequiresExplicitShellSessionRegistry(t *testing.T) {
	store := newTestStore(t)
	_, err := NewCoordinator(Dependencies{
		Store:          store,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    canonical.NewSimpleSynthesizer(),
		AnchorProvider: defaultAnchor(),
	})
	if err == nil {
		t.Fatal("expected missing shell session registry to fail coordinator construction")
	}
}

func TestReportShellSessionUsesInjectedRegistry(t *testing.T) {
	store := newTestStore(t)
	registry := &recordingShellSessionRegistry{}
	coord, err := NewCoordinator(Dependencies{
		Store:          store,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    canonical.NewSimpleSynthesizer(),
		AnchorProvider: defaultAnchor(),
		ShellSessions:  registry,
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:    string(taskID),
		SessionID: "shs_1",
		HostMode:  "claude-pty",
		HostState: "live",
		StartedAt: time.Unix(1710000000, 0).UTC(),
		Active:    true,
	}); err != nil {
		t.Fatalf("report shell session: %v", err)
	}
	if len(registry.upserts) != 1 || registry.upserts[0].SessionID != "shs_1" {
		t.Fatalf("expected injected registry to receive upsert, got %+v", registry.upserts)
	}
}

func TestDurableShellSessionRegistryPersistsAcrossStoreReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tuku-shell-sessions.db")
	store, err := sqlite.NewStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	coord, err := NewCoordinator(Dependencies{
		Store:          store,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    canonical.NewSimpleSynthesizer(),
		AnchorProvider: defaultAnchor(),
		ShellSessions:  store.ShellSessions(),
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	taskID := setupTaskWithBrief(t, coord)
	startedAt := time.Unix(1710000000, 0).UTC()
	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:           string(taskID),
		SessionID:        "shs_durable",
		WorkerPreference: "auto",
		ResolvedWorker:   "codex",
		WorkerSessionID:  "wks_durable",
		AttachCapability: shellsession.AttachCapabilityAttachable,
		HostMode:         "codex-pty",
		HostState:        "live",
		StartedAt:        startedAt,
		Active:           true,
	}); err != nil {
		t.Fatalf("report durable shell session: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite store: %v", err)
	}

	reopened, err := sqlite.NewStore(dbPath)
	if err != nil {
		t.Fatalf("reopen sqlite store: %v", err)
	}
	defer reopened.Close()
	coord2, err := NewCoordinator(Dependencies{
		Store:          reopened,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    canonical.NewSimpleSynthesizer(),
		AnchorProvider: defaultAnchor(),
		ShellSessions:  reopened.ShellSessions(),
	})
	if err != nil {
		t.Fatalf("new reopened coordinator: %v", err)
	}

	result, err := coord2.ListShellSessions(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("list durable shell sessions after reopen: %v", err)
	}
	if len(result.Sessions) != 1 {
		t.Fatalf("expected one durable shell session, got %d", len(result.Sessions))
	}
	if result.Sessions[0].SessionID != "shs_durable" {
		t.Fatalf("unexpected durable shell session %+v", result.Sessions[0])
	}
	if result.Sessions[0].WorkerSessionID != "wks_durable" || result.Sessions[0].AttachCapability != shellsession.AttachCapabilityAttachable {
		t.Fatalf("expected durable worker-session anchor, got %+v", result.Sessions[0])
	}
	if !result.Sessions[0].StartedAt.Equal(startedAt) {
		t.Fatalf("expected durable started_at %v, got %v", startedAt, result.Sessions[0].StartedAt)
	}
}

func TestClassifyShellSessionMarksStale(t *testing.T) {
	now := time.Unix(1710000600, 0).UTC()
	view := classifyShellSession(ShellSessionRecord{
		TaskID:        "tsk_1",
		SessionID:     "shs_stale",
		LastUpdatedAt: now.Add(-2 * time.Minute),
		Active:        true,
	}, now, time.Minute)
	if view.SessionClass != ShellSessionClassStale {
		t.Fatalf("expected stale session class, got %s", view.SessionClass)
	}
}

func TestClassifyShellSessionsOrdersAttachableActiveUnattachableStaleEnded(t *testing.T) {
	now := time.Unix(1710000600, 0).UTC()
	views := classifyShellSessions([]ShellSessionRecord{
		{TaskID: "tsk_1", SessionID: "shs_attachable", WorkerSessionID: "wks_1", AttachCapability: shellsession.AttachCapabilityAttachable, LastUpdatedAt: now.Add(2 * time.Second), Active: true},
		{TaskID: "tsk_1", SessionID: "shs_ended", LastUpdatedAt: now.Add(time.Second), Active: false},
		{TaskID: "tsk_1", SessionID: "shs_stale", LastUpdatedAt: now.Add(-2 * time.Minute), Active: true},
		{TaskID: "tsk_1", SessionID: "shs_active", LastUpdatedAt: now, Active: true},
	}, now, time.Minute)
	if len(views) != 4 {
		t.Fatalf("expected four classified sessions, got %d", len(views))
	}
	if views[0].SessionClass != ShellSessionClassAttachable || views[1].SessionClass != ShellSessionClassActiveUnattachable || views[2].SessionClass != ShellSessionClassStale || views[3].SessionClass != ShellSessionClassEnded {
		t.Fatalf("unexpected session ordering: %+v", views)
	}
}

func TestClassifyShellSessionMarksAttachable(t *testing.T) {
	now := time.Unix(1710000600, 0).UTC()
	view := classifyShellSession(ShellSessionRecord{
		TaskID:           "tsk_1",
		SessionID:        "shs_attachable",
		WorkerSessionID:  "wks_attachable",
		AttachCapability: shellsession.AttachCapabilityAttachable,
		LastUpdatedAt:    now,
		Active:           true,
	}, now, time.Minute)
	if view.SessionClass != ShellSessionClassAttachable {
		t.Fatalf("expected attachable session class, got %s", view.SessionClass)
	}
}

func TestClassifyShellSessionMarksActiveUnattachable(t *testing.T) {
	now := time.Unix(1710000600, 0).UTC()
	view := classifyShellSession(ShellSessionRecord{
		TaskID:           "tsk_1",
		SessionID:        "shs_active_unattachable",
		WorkerSessionID:  "wks_present_but_unattachable",
		AttachCapability: shellsession.AttachCapabilityNone,
		LastUpdatedAt:    now,
		Active:           true,
	}, now, time.Minute)
	if view.SessionClass != ShellSessionClassActiveUnattachable {
		t.Fatalf("expected active_unattachable session class, got %s", view.SessionClass)
	}
}

func TestDurableShellSessionClassificationIsDerivedOnRead(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tuku-shell-derived.db")
	store, err := sqlite.NewStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	reportedAt := time.Unix(1710000000, 0).UTC()
	now := reportedAt
	coord, err := NewCoordinator(Dependencies{
		Store:                  store,
		IntentCompiler:         NewIntentStubCompiler(),
		BriefBuilder:           NewBriefBuilderV1(nil, nil),
		WorkerAdapter:          newFakeAdapterSuccess(),
		Synthesizer:            canonical.NewSimpleSynthesizer(),
		AnchorProvider:         defaultAnchor(),
		ShellSessions:          store.ShellSessions(),
		ShellSessionStaleAfter: time.Minute,
		Clock:                  func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	taskID := setupTaskWithBrief(t, coord)
	now = reportedAt.Add(90 * time.Second)
	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:           string(taskID),
		SessionID:        "shs_active",
		WorkerPreference: "auto",
		ResolvedWorker:   "codex",
		WorkerSessionID:  "wks_active",
		AttachCapability: shellsession.AttachCapabilityAttachable,
		HostMode:         "codex-pty",
		HostState:        "live",
		StartedAt:        reportedAt.Add(30 * time.Second),
		Active:           true,
	}); err != nil {
		t.Fatalf("report active shell session: %v", err)
	}
	now = reportedAt
	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:           string(taskID),
		SessionID:        "shs_active_then_stale",
		WorkerPreference: "auto",
		ResolvedWorker:   "codex",
		WorkerSessionID:  "wks_stale",
		AttachCapability: shellsession.AttachCapabilityAttachable,
		HostMode:         "codex-pty",
		HostState:        "live",
		StartedAt:        reportedAt,
		Active:           true,
	}); err != nil {
		t.Fatalf("report stale-bound shell session: %v", err)
	}
	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:           string(taskID),
		SessionID:        "shs_ended",
		WorkerPreference: "auto",
		ResolvedWorker:   "claude",
		WorkerSessionID:  "wks_ended",
		AttachCapability: shellsession.AttachCapabilityNone,
		HostMode:         "transcript",
		HostState:        "fallback",
		StartedAt:        reportedAt,
		Active:           false,
		Note:             "shell session ended",
	}); err != nil {
		t.Fatalf("report ended shell session: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite store: %v", err)
	}

	reopened, err := sqlite.NewStore(dbPath)
	if err != nil {
		t.Fatalf("reopen sqlite store: %v", err)
	}
	defer reopened.Close()
	readAt := reportedAt.Add(2 * time.Minute)
	coord2, err := NewCoordinator(Dependencies{
		Store:                  reopened,
		IntentCompiler:         NewIntentStubCompiler(),
		BriefBuilder:           NewBriefBuilderV1(nil, nil),
		WorkerAdapter:          newFakeAdapterSuccess(),
		Synthesizer:            canonical.NewSimpleSynthesizer(),
		AnchorProvider:         defaultAnchor(),
		ShellSessions:          reopened.ShellSessions(),
		ShellSessionStaleAfter: time.Minute,
		Clock:                  func() time.Time { return readAt },
	})
	if err != nil {
		t.Fatalf("new reopened coordinator: %v", err)
	}

	result, err := coord2.ListShellSessions(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("list derived shell sessions: %v", err)
	}
	if len(result.Sessions) != 3 {
		t.Fatalf("expected three durable shell sessions, got %d", len(result.Sessions))
	}
	if result.Sessions[0].SessionID != "shs_active" || result.Sessions[0].SessionClass != ShellSessionClassAttachable {
		t.Fatalf("expected active session first, got %+v", result.Sessions[0])
	}
	if result.Sessions[1].SessionID != "shs_active_then_stale" || result.Sessions[1].SessionClass != ShellSessionClassStale {
		t.Fatalf("expected stale session second, got %+v", result.Sessions[1])
	}
	if result.Sessions[2].SessionID != "shs_ended" || result.Sessions[2].SessionClass != ShellSessionClassEnded {
		t.Fatalf("expected ended session third, got %+v", result.Sessions[2])
	}
}

func TestDurableShellSessionRegistryPreservesSessionIdentityAcrossUpdates(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tuku-shell-identity.db")
	store, err := sqlite.NewStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer store.Close()

	first := time.Unix(1710000000, 0).UTC()
	second := first.Add(10 * time.Minute)
	registry := store.ShellSessions()
	if err := registry.Upsert(ShellSessionRecord{
		TaskID:           "tsk_identity",
		SessionID:        "shs_identity",
		WorkerSessionID:  "wks_identity",
		AttachCapability: shellsession.AttachCapabilityAttachable,
		StartedAt:        first,
		LastUpdatedAt:    first,
		HostState:        "live",
		Active:           true,
	}); err != nil {
		t.Fatalf("upsert first durable shell session state: %v", err)
	}
	if err := registry.Upsert(ShellSessionRecord{
		TaskID:           "tsk_identity",
		SessionID:        "shs_identity",
		AttachCapability: shellsession.AttachCapabilityNone,
		StartedAt:        second,
		LastUpdatedAt:    second,
		HostState:        "fallback",
		Active:           false,
	}); err != nil {
		t.Fatalf("upsert second durable shell session state: %v", err)
	}

	sessions, err := registry.ListByTask("tsk_identity")
	if err != nil {
		t.Fatalf("list durable shell session identity state: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one durable shell session record, got %d", len(sessions))
	}
	if !sessions[0].StartedAt.Equal(first) {
		t.Fatalf("expected original durable started_at to be preserved, got %v want %v", sessions[0].StartedAt, first)
	}
	if sessions[0].WorkerSessionID != "wks_identity" {
		t.Fatalf("expected worker session id to survive update, got %+v", sessions[0])
	}
}

func TestReportShellSessionRegistersAndMarksEnded(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	now := time.Unix(1710000000, 0).UTC()

	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:           string(taskID),
		SessionID:        "shs_1",
		WorkerPreference: "auto",
		ResolvedWorker:   "claude",
		WorkerSessionID:  "wks_1",
		AttachCapability: shellsession.AttachCapabilityAttachable,
		HostMode:         "claude-pty",
		HostState:        "live",
		StartedAt:        now,
		Active:           true,
	}); err != nil {
		t.Fatalf("report shell session live: %v", err)
	}
	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:           string(taskID),
		SessionID:        "shs_1",
		WorkerPreference: "auto",
		ResolvedWorker:   "claude",
		WorkerSessionID:  "wks_1",
		AttachCapability: shellsession.AttachCapabilityNone,
		HostMode:         "transcript",
		HostState:        "fallback",
		StartedAt:        now,
		Active:           false,
		Note:             "shell session ended",
	}); err != nil {
		t.Fatalf("report shell session ended: %v", err)
	}

	result, err := coord.ListShellSessions(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("list shell sessions: %v", err)
	}
	if len(result.Sessions) != 1 {
		t.Fatalf("expected one shell session record, got %d", len(result.Sessions))
	}
	if result.Sessions[0].SessionID != "shs_1" || result.Sessions[0].SessionClass != ShellSessionClassEnded {
		t.Fatalf("expected ended session record, got %+v", result.Sessions[0])
	}
	if !result.Sessions[0].StartedAt.Equal(now) {
		t.Fatalf("expected stable started_at, got %v want %v", result.Sessions[0].StartedAt, now)
	}
}

func TestReportShellSessionReturnsPersistedStartedAt(t *testing.T) {
	store := newTestStore(t)
	coord, err := NewCoordinator(Dependencies{
		Store:          store,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    canonical.NewSimpleSynthesizer(),
		AnchorProvider: defaultAnchor(),
		ShellSessions:  NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	taskID := setupTaskWithBrief(t, coord)
	first := time.Unix(1710000000, 0).UTC()
	second := first.Add(10 * time.Minute)
	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:           string(taskID),
		SessionID:        "shs_started_at",
		WorkerSessionID:  "wks_started_at",
		AttachCapability: shellsession.AttachCapabilityAttachable,
		StartedAt:        first,
		HostMode:         "codex-pty",
		HostState:        "live",
		Active:           true,
	}); err != nil {
		t.Fatalf("report first shell session state: %v", err)
	}
	out, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:           string(taskID),
		SessionID:        "shs_started_at",
		AttachCapability: shellsession.AttachCapabilityNone,
		StartedAt:        second,
		HostMode:         "transcript",
		HostState:        "fallback",
		Active:           false,
	})
	if err != nil {
		t.Fatalf("report second shell session state: %v", err)
	}
	if !out.Session.StartedAt.Equal(first) {
		t.Fatalf("expected persisted started_at %v, got %v", first, out.Session.StartedAt)
	}
	if out.Session.WorkerSessionID != "wks_started_at" {
		t.Fatalf("expected persisted worker session id, got %+v", out.Session)
	}
}

func TestReportShellSessionDoesNotAppendProofEvents(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	before, err := store.Proofs().ListByTask(taskID, 200)
	if err != nil {
		t.Fatalf("list proof events before shell session report: %v", err)
	}
	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:           string(taskID),
		SessionID:        "shs_no_proof",
		WorkerPreference: "auto",
		ResolvedWorker:   "claude",
		WorkerSessionID:  "wks_no_proof",
		AttachCapability: shellsession.AttachCapabilityAttachable,
		HostMode:         "claude-pty",
		HostState:        "live",
		StartedAt:        time.Unix(1710000000, 0).UTC(),
		Active:           true,
	}); err != nil {
		t.Fatalf("report shell session: %v", err)
	}
	after, err := store.Proofs().ListByTask(taskID, 200)
	if err != nil {
		t.Fatalf("list proof events after shell session report: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("expected proof event count to remain unchanged, before=%d after=%d", len(before), len(after))
	}
	for _, event := range after {
		if event.Type == proof.EventType("SHELL_SESSION_REPORTED") {
			t.Fatalf("unexpected shell-session proof event persisted: %+v", event)
		}
	}
}

func TestMemoryShellSessionRegistryPreservesSessionIdentityAcrossStateChanges(t *testing.T) {
	registry := NewMemoryShellSessionRegistry()
	first := time.Unix(1710000000, 0).UTC()
	second := first.Add(10 * time.Minute)

	if err := registry.Upsert(ShellSessionRecord{
		TaskID:           "tsk_identity",
		SessionID:        "shs_identity",
		WorkerSessionID:  "wks_identity",
		AttachCapability: shellsession.AttachCapabilityAttachable,
		StartedAt:        first,
		LastUpdatedAt:    first,
		HostState:        "live",
		Active:           true,
	}); err != nil {
		t.Fatalf("upsert first shell session state: %v", err)
	}
	if err := registry.Upsert(ShellSessionRecord{
		TaskID:           "tsk_identity",
		SessionID:        "shs_identity",
		AttachCapability: shellsession.AttachCapabilityNone,
		StartedAt:        second,
		LastUpdatedAt:    second,
		HostState:        "fallback",
		Active:           false,
	}); err != nil {
		t.Fatalf("upsert second shell session state: %v", err)
	}

	sessions, err := registry.ListByTask("tsk_identity")
	if err != nil {
		t.Fatalf("list shell session identity state: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one shell session record, got %d", len(sessions))
	}
	if !sessions[0].StartedAt.Equal(first) {
		t.Fatalf("expected original started_at to be preserved, got %v want %v", sessions[0].StartedAt, first)
	}
	if sessions[0].WorkerSessionID != "wks_identity" {
		t.Fatalf("expected worker session id to be preserved, got %+v", sessions[0])
	}
}
