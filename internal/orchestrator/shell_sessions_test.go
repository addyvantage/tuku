package orchestrator

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
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
		TaskID:                string(taskID),
		SessionID:             "shs_durable",
		WorkerPreference:      "auto",
		ResolvedWorker:        "codex",
		WorkerSessionID:       "wks_durable",
		WorkerSessionIDSource: shellsession.WorkerSessionIDSourceAuthoritative,
		AttachCapability:      shellsession.AttachCapabilityAttachable,
		HostMode:              "codex-pty",
		HostState:             "live",
		StartedAt:             startedAt,
		Active:                true,
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
	if result.Sessions[0].WorkerSessionIDSource != shellsession.WorkerSessionIDSourceAuthoritative {
		t.Fatalf("expected durable worker-session source authoritative, got %+v", result.Sessions[0])
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
		{TaskID: "tsk_1", SessionID: "shs_attachable", WorkerSessionID: "wks_1", WorkerSessionIDSource: shellsession.WorkerSessionIDSourceAuthoritative, AttachCapability: shellsession.AttachCapabilityAttachable, LastUpdatedAt: now.Add(2 * time.Second), Active: true},
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
		TaskID:                "tsk_1",
		SessionID:             "shs_attachable",
		WorkerSessionID:       "wks_attachable",
		WorkerSessionIDSource: shellsession.WorkerSessionIDSourceAuthoritative,
		AttachCapability:      shellsession.AttachCapabilityAttachable,
		LastUpdatedAt:         now,
		Active:                true,
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

func TestClassifyShellSessionRejectsHeuristicWorkerSessionIDForAttach(t *testing.T) {
	now := time.Unix(1710000600, 0).UTC()
	view := classifyShellSession(ShellSessionRecord{
		TaskID:                "tsk_1",
		SessionID:             "shs_heuristic",
		WorkerSessionID:       "wks_heuristic",
		WorkerSessionIDSource: shellsession.WorkerSessionIDSourceHeuristic,
		AttachCapability:      shellsession.AttachCapabilityAttachable,
		LastUpdatedAt:         now,
		Active:                true,
	}, now, time.Minute)
	if view.SessionClass != ShellSessionClassActiveUnattachable {
		t.Fatalf("expected active_unattachable session class for heuristic worker session id, got %s", view.SessionClass)
	}
	if !strings.Contains(view.SessionClassReason, "heuristically") {
		t.Fatalf("expected heuristic class reason, got %q", view.SessionClassReason)
	}
}

func TestMemoryShellSessionRegistryTranscriptRetentionIsBounded(t *testing.T) {
	registry := NewMemoryShellSessionRegistry()
	taskID := common.TaskID("tsk_transcript")
	sessionID := "shs_transcript"
	base := time.Unix(1710000000, 0).UTC()

	chunks := make([]shellsession.TranscriptChunk, 0, 5)
	for i := 0; i < 5; i++ {
		chunks = append(chunks, shellsession.TranscriptChunk{
			TaskID:     taskID,
			SessionID:  sessionID,
			Source:     shellsession.TranscriptSourceWorkerOutput,
			Content:    fmt.Sprintf("line %d", i+1),
			CreatedAt:  base.Add(time.Duration(i) * time.Second),
			SequenceNo: int64(i + 1),
		})
	}

	summary, err := registry.AppendTranscript(taskID, sessionID, chunks, 3)
	if err != nil {
		t.Fatalf("append transcript: %v", err)
	}
	if summary.RetentionLimit != 3 || summary.RetainedChunks != 3 || summary.DroppedChunks != 2 || summary.LastSequenceNo != 5 {
		t.Fatalf("unexpected transcript summary after bounded append: %+v", summary)
	}

	listed, err := registry.ListTranscript(taskID, sessionID, 10)
	if err != nil {
		t.Fatalf("list transcript: %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("expected 3 retained transcript chunks, got %d", len(listed))
	}
	if listed[0].Content != "line 3" || listed[1].Content != "line 4" || listed[2].Content != "line 5" {
		t.Fatalf("unexpected retained transcript content ordering: %+v", listed)
	}

	summaryCheck, err := registry.TranscriptSummary(taskID, sessionID, 3)
	if err != nil {
		t.Fatalf("transcript summary: %v", err)
	}
	if summaryCheck.RetainedChunks != 3 || summaryCheck.DroppedChunks != 2 || summaryCheck.LastSequenceNo != 5 {
		t.Fatalf("unexpected transcript summary from read path: %+v", summaryCheck)
	}
}

func TestDurableTranscriptEvidencePersistsAndSurfacesAcrossReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tuku-shell-transcript-evidence.db")
	store, err := sqlite.NewStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	now := time.Unix(1710000000, 0).UTC()
	coord, err := NewCoordinator(Dependencies{
		Store:                  store,
		IntentCompiler:         NewIntentStubCompiler(),
		BriefBuilder:           NewBriefBuilderV1(nil, nil),
		WorkerAdapter:          newFakeAdapterSuccess(),
		Synthesizer:            canonical.NewSimpleSynthesizer(),
		AnchorProvider:         defaultAnchor(),
		ShellSessions:          store.ShellSessions(),
		ShellSessionStaleAfter: 24 * time.Hour,
		Clock:                  func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:                string(taskID),
		SessionID:             "shs_transcript_durable",
		WorkerPreference:      "auto",
		ResolvedWorker:        "codex",
		WorkerSessionID:       "wks_transcript_durable",
		WorkerSessionIDSource: shellsession.WorkerSessionIDSourceAuthoritative,
		AttachCapability:      shellsession.AttachCapabilityAttachable,
		HostMode:              "transcript",
		HostState:             "transcript-only",
		StartedAt:             now,
		Active:                true,
	}); err != nil {
		t.Fatalf("report shell session: %v", err)
	}

	chunks := make([]RecordShellTranscriptChunk, 0, 205)
	for i := 0; i < 205; i++ {
		chunks = append(chunks, RecordShellTranscriptChunk{
			Source:    shellsession.TranscriptSourceWorkerOutput,
			Content:   fmt.Sprintf("durable line %03d", i+1),
			CreatedAt: now.Add(time.Duration(i) * time.Second),
		})
	}
	appendOut, err := coord.RecordShellTranscript(context.Background(), RecordShellTranscriptRequest{
		TaskID:    string(taskID),
		SessionID: "shs_transcript_durable",
		Chunks:    chunks,
	})
	if err != nil {
		t.Fatalf("record shell transcript: %v", err)
	}
	if appendOut.Summary.RetentionLimit != shellsession.DefaultTranscriptRetentionChunks ||
		appendOut.Summary.RetainedChunks != shellsession.DefaultTranscriptRetentionChunks ||
		appendOut.Summary.DroppedChunks != 5 ||
		appendOut.Summary.LastSequenceNo != 205 {
		t.Fatalf("unexpected transcript append summary: %+v", appendOut.Summary)
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
		Store:                  reopened,
		IntentCompiler:         NewIntentStubCompiler(),
		BriefBuilder:           NewBriefBuilderV1(nil, nil),
		WorkerAdapter:          newFakeAdapterSuccess(),
		Synthesizer:            canonical.NewSimpleSynthesizer(),
		AnchorProvider:         defaultAnchor(),
		ShellSessions:          reopened.ShellSessions(),
		ShellSessionStaleAfter: 24 * time.Hour,
		Clock:                  func() time.Time { return now.Add(10 * time.Minute) },
	})
	if err != nil {
		t.Fatalf("new reopened coordinator: %v", err)
	}

	status, err := coord2.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.LatestShellTranscriptState != shellsession.TranscriptStateTranscriptOnlyPartial {
		t.Fatalf("expected transcript-only partial status summary, got %s", status.LatestShellTranscriptState)
	}
	if status.LatestShellTranscriptRetainedChunks != shellsession.DefaultTranscriptRetentionChunks || status.LatestShellTranscriptDroppedChunks != 5 {
		t.Fatalf("unexpected transcript summary in status: retained=%d dropped=%d", status.LatestShellTranscriptRetainedChunks, status.LatestShellTranscriptDroppedChunks)
	}

	inspect, err := coord2.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if len(inspect.RecentShellTranscript) != 40 {
		t.Fatalf("expected bounded inspect transcript view of 40 chunks, got %d", len(inspect.RecentShellTranscript))
	}
	if inspect.RecentShellTranscript[0].SequenceNo != 166 || inspect.RecentShellTranscript[len(inspect.RecentShellTranscript)-1].SequenceNo != 205 {
		t.Fatalf("unexpected inspect transcript sequence range: first=%d last=%d", inspect.RecentShellTranscript[0].SequenceNo, inspect.RecentShellTranscript[len(inspect.RecentShellTranscript)-1].SequenceNo)
	}

	snapshot, err := coord2.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if len(snapshot.RecentShellTranscript) != 40 {
		t.Fatalf("expected bounded shell snapshot transcript view of 40 chunks, got %d", len(snapshot.RecentShellTranscript))
	}
	if snapshot.RecentShellTranscript[0].SequenceNo != 166 || snapshot.RecentShellTranscript[len(snapshot.RecentShellTranscript)-1].SequenceNo != 205 {
		t.Fatalf("unexpected shell snapshot transcript sequence range: first=%d last=%d", snapshot.RecentShellTranscript[0].SequenceNo, snapshot.RecentShellTranscript[len(snapshot.RecentShellTranscript)-1].SequenceNo)
	}
}

func TestReadShellTranscriptSupportsDeterministicSequencePagination(t *testing.T) {
	store := newTestStore(t)
	now := time.Unix(1710000000, 0).UTC()
	coord, err := NewCoordinator(Dependencies{
		Store:                  store,
		IntentCompiler:         NewIntentStubCompiler(),
		BriefBuilder:           NewBriefBuilderV1(nil, nil),
		WorkerAdapter:          newFakeAdapterSuccess(),
		Synthesizer:            canonical.NewSimpleSynthesizer(),
		AnchorProvider:         defaultAnchor(),
		ShellSessions:          NewMemoryShellSessionRegistry(),
		ShellSessionStaleAfter: 24 * time.Hour,
		Clock:                  func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:                string(taskID),
		SessionID:             "shs_read_page",
		WorkerPreference:      "auto",
		ResolvedWorker:        "codex",
		WorkerSessionID:       "wks_read_page",
		WorkerSessionIDSource: shellsession.WorkerSessionIDSourceAuthoritative,
		AttachCapability:      shellsession.AttachCapabilityAttachable,
		HostMode:              "transcript",
		HostState:             "transcript-only",
		StartedAt:             now,
		Active:                true,
	}); err != nil {
		t.Fatalf("report shell session: %v", err)
	}

	chunks := make([]RecordShellTranscriptChunk, 0, 210)
	for i := 0; i < 200; i++ {
		chunks = append(chunks, RecordShellTranscriptChunk{
			Source:    shellsession.TranscriptSourceWorkerOutput,
			Content:   fmt.Sprintf("worker line %03d", i+1),
			CreatedAt: now.Add(time.Duration(i) * time.Second),
		})
	}
	for i := 0; i < 10; i++ {
		chunks = append(chunks, RecordShellTranscriptChunk{
			Source:    shellsession.TranscriptSourceFallback,
			Content:   fmt.Sprintf("fallback note %02d", i+1),
			CreatedAt: now.Add(time.Duration(200+i) * time.Second),
		})
	}
	if _, err := coord.RecordShellTranscript(context.Background(), RecordShellTranscriptRequest{
		TaskID:    string(taskID),
		SessionID: "shs_read_page",
		Chunks:    chunks,
	}); err != nil {
		t.Fatalf("record transcript: %v", err)
	}

	first, err := coord.ReadShellTranscript(context.Background(), ReadShellTranscriptRequest{
		TaskID:    string(taskID),
		SessionID: "shs_read_page",
		Limit:     40,
	})
	if err != nil {
		t.Fatalf("read first transcript page: %v", err)
	}
	if !first.Bounded || !first.Partial || !first.TranscriptOnly {
		t.Fatalf("expected bounded transcript-only partial truth, got %+v", first)
	}
	if first.TranscriptState != shellsession.TranscriptStateTranscriptOnlyPartial {
		t.Fatalf("expected transcript-only partial state, got %s", first.TranscriptState)
	}
	if first.RetentionLimit != shellsession.DefaultTranscriptRetentionChunks || first.RetainedChunkCount != shellsession.DefaultTranscriptRetentionChunks || first.DroppedChunkCount != 10 {
		t.Fatalf("unexpected transcript summary %+v", first)
	}
	if len(first.Chunks) != 40 || first.PageOldestSequence != 171 || first.PageNewestSequence != 210 || !first.HasMoreOlder {
		t.Fatalf("unexpected first transcript page %+v", first)
	}
	if first.NextBeforeSequence == nil || *first.NextBeforeSequence != 171 {
		t.Fatalf("expected next-before sequence 171, got %+v", first.NextBeforeSequence)
	}

	second, err := coord.ReadShellTranscript(context.Background(), ReadShellTranscriptRequest{
		TaskID:         string(taskID),
		SessionID:      "shs_read_page",
		Limit:          40,
		BeforeSequence: *first.NextBeforeSequence,
	})
	if err != nil {
		t.Fatalf("read second transcript page: %v", err)
	}
	if len(second.Chunks) != 40 || second.PageOldestSequence != 131 || second.PageNewestSequence != 170 || !second.HasMoreOlder {
		t.Fatalf("unexpected second transcript page %+v", second)
	}
}

func TestReadShellTranscriptSupportsSourceFiltering(t *testing.T) {
	store := newTestStore(t)
	now := time.Unix(1710000000, 0).UTC()
	coord, err := NewCoordinator(Dependencies{
		Store:                  store,
		IntentCompiler:         NewIntentStubCompiler(),
		BriefBuilder:           NewBriefBuilderV1(nil, nil),
		WorkerAdapter:          newFakeAdapterSuccess(),
		Synthesizer:            canonical.NewSimpleSynthesizer(),
		AnchorProvider:         defaultAnchor(),
		ShellSessions:          NewMemoryShellSessionRegistry(),
		ShellSessionStaleAfter: 24 * time.Hour,
		Clock:                  func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:    string(taskID),
		SessionID: "shs_source_filter",
		HostMode:  "transcript",
		HostState: "fallback",
		StartedAt: now,
		Active:    true,
	}); err != nil {
		t.Fatalf("report shell session: %v", err)
	}
	if _, err := coord.RecordShellTranscript(context.Background(), RecordShellTranscriptRequest{
		TaskID:    string(taskID),
		SessionID: "shs_source_filter",
		Chunks: []RecordShellTranscriptChunk{
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "worker line one", CreatedAt: now.Add(1 * time.Second)},
			{Source: shellsession.TranscriptSourceSystemNote, Content: "system note one", CreatedAt: now.Add(2 * time.Second)},
			{Source: shellsession.TranscriptSourceFallback, Content: "fallback note one", CreatedAt: now.Add(3 * time.Second)},
		},
	}); err != nil {
		t.Fatalf("record transcript: %v", err)
	}

	filtered, err := coord.ReadShellTranscript(context.Background(), ReadShellTranscriptRequest{
		TaskID:    string(taskID),
		SessionID: "shs_source_filter",
		Limit:     10,
		Source:    string(shellsession.TranscriptSourceFallback),
	})
	if err != nil {
		t.Fatalf("read filtered transcript: %v", err)
	}
	if filtered.RequestedSource != shellsession.TranscriptSourceFallback {
		t.Fatalf("expected fallback source filter, got %s", filtered.RequestedSource)
	}
	if len(filtered.Chunks) != 1 || filtered.Chunks[0].Source != shellsession.TranscriptSourceFallback {
		t.Fatalf("unexpected filtered chunk payload %+v", filtered.Chunks)
	}
	if filtered.TranscriptState != shellsession.TranscriptStateTranscriptOnlyAvailable {
		t.Fatalf("expected transcript-only available state, got %s", filtered.TranscriptState)
	}
}

func TestReadShellTranscriptRejectsUnknownSession(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	_, err := coord.ReadShellTranscript(context.Background(), ReadShellTranscriptRequest{
		TaskID:    string(taskID),
		SessionID: "shs_missing",
		Limit:     10,
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected missing session error, got %v", err)
	}
}

func TestReadShellTranscriptRejectsUnknownTask(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())

	_, err := coord.ReadShellTranscript(context.Background(), ReadShellTranscriptRequest{
		TaskID:    "tsk_unknown",
		SessionID: "shs_any",
		Limit:     10,
	})
	if err == nil {
		t.Fatal("expected unknown task error")
	}
}

func TestRecordShellTranscriptReviewPersistsProofAndSurfacesAcrossReadModels(t *testing.T) {
	store := newTestStore(t)
	now := time.Unix(1710000000, 0).UTC()
	coord, err := NewCoordinator(Dependencies{
		Store:                  store,
		IntentCompiler:         NewIntentStubCompiler(),
		BriefBuilder:           NewBriefBuilderV1(nil, nil),
		WorkerAdapter:          newFakeAdapterSuccess(),
		Synthesizer:            canonical.NewSimpleSynthesizer(),
		AnchorProvider:         defaultAnchor(),
		ShellSessions:          NewMemoryShellSessionRegistry(),
		ShellSessionStaleAfter: 24 * time.Hour,
		Clock:                  func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:    string(taskID),
		SessionID: "shs_review",
		HostMode:  "transcript",
		HostState: "transcript-only",
		StartedAt: now,
		Active:    true,
	}); err != nil {
		t.Fatalf("report shell session: %v", err)
	}
	chunks := make([]RecordShellTranscriptChunk, 0, 6)
	for i := 0; i < 6; i++ {
		chunks = append(chunks, RecordShellTranscriptChunk{
			Source:    shellsession.TranscriptSourceWorkerOutput,
			Content:   fmt.Sprintf("line %d", i+1),
			CreatedAt: now.Add(time.Duration(i) * time.Second),
		})
	}
	if _, err := coord.RecordShellTranscript(context.Background(), RecordShellTranscriptRequest{
		TaskID:    string(taskID),
		SessionID: "shs_review",
		Chunks:    chunks,
	}); err != nil {
		t.Fatalf("record shell transcript: %v", err)
	}

	review, err := coord.RecordShellTranscriptReview(context.Background(), RecordShellTranscriptReviewRequest{
		TaskID:          string(taskID),
		SessionID:       "shs_review",
		ReviewedUpToSeq: 4,
		Summary:         "reviewed initial bounded segment",
	})
	if err != nil {
		t.Fatalf("record transcript review: %v", err)
	}
	if review.LatestReview.ReviewID == "" || review.LatestReview.ReviewedUpToSequence != 4 {
		t.Fatalf("unexpected transcript review result: %+v", review)
	}
	if !review.HasUnreadNewerEvidence {
		t.Fatalf("expected unread newer evidence after review boundary, got %+v", review)
	}
	if review.Closure.State == shellsession.TranscriptReviewClosureNone || review.Closure.OldestUnreviewedSequence != 5 {
		t.Fatalf("expected transcript review closure metadata, got %+v", review.Closure)
	}

	readOut, err := coord.ReadShellTranscript(context.Background(), ReadShellTranscriptRequest{
		TaskID:    string(taskID),
		SessionID: "shs_review",
		Limit:     6,
	})
	if err != nil {
		t.Fatalf("read shell transcript: %v", err)
	}
	if readOut.LatestReview == nil || readOut.LatestReview.ReviewedUpToSequence != 4 {
		t.Fatalf("expected latest review in transcript read result, got %+v", readOut.LatestReview)
	}
	if !readOut.PageCrossesReview || !readOut.PageHasUnreviewed || readOut.PageFullyReviewed {
		t.Fatalf("expected review boundary coverage metadata in read result, got %+v", readOut)
	}
	if readOut.Closure.State == shellsession.TranscriptReviewClosureNone || readOut.Closure.OldestUnreviewedSequence != 5 {
		t.Fatalf("expected closure metadata in transcript read result, got %+v", readOut.Closure)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.LatestShellTranscriptReviewedUpTo != 4 || status.LatestShellTranscriptReviewID == "" || !status.LatestShellTranscriptReviewStale {
		t.Fatalf("expected status review metadata, got %+v", status)
	}

	inspect, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if len(inspect.ShellSessions) == 0 || inspect.ShellSessions[0].TranscriptReviewedUpTo != 4 {
		t.Fatalf("expected inspect shell session review metadata, got %+v", inspect.ShellSessions)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if len(snapshot.ShellSessions) == 0 || snapshot.ShellSessions[0].TranscriptReviewedUpTo != 4 {
		t.Fatalf("expected shell snapshot review metadata, got %+v", snapshot.ShellSessions)
	}

	events, err := store.Proofs().ListByTask(taskID, 200)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	foundReviewProof := false
	for _, evt := range events {
		if evt.Type == proof.EventTranscriptEvidenceReviewed {
			foundReviewProof = true
			break
		}
	}
	if !foundReviewProof {
		t.Fatalf("expected transcript review proof event, got %+v", events)
	}
}

func TestRecordShellTranscriptReviewRejectsOutOfRangeSequence(t *testing.T) {
	store := newTestStore(t)
	now := time.Unix(1710000000, 0).UTC()
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:    string(taskID),
		SessionID: "shs_review_validate",
		HostMode:  "transcript",
		HostState: "transcript-only",
		StartedAt: now,
		Active:    true,
	}); err != nil {
		t.Fatalf("report shell session: %v", err)
	}
	if _, err := coord.RecordShellTranscript(context.Background(), RecordShellTranscriptRequest{
		TaskID:    string(taskID),
		SessionID: "shs_review_validate",
		Chunks: []RecordShellTranscriptChunk{
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "line 1", CreatedAt: now},
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "line 2", CreatedAt: now.Add(time.Second)},
		},
	}); err != nil {
		t.Fatalf("record shell transcript: %v", err)
	}
	_, err := coord.RecordShellTranscriptReview(context.Background(), RecordShellTranscriptReviewRequest{
		TaskID:          string(taskID),
		SessionID:       "shs_review_validate",
		ReviewedUpToSeq: 9,
	})
	if err == nil || !strings.Contains(err.Error(), "outside retained transcript window") {
		t.Fatalf("expected out-of-range review boundary error, got %v", err)
	}
}

func TestRecordShellTranscriptReviewRejectsMissingSourceSequence(t *testing.T) {
	store := newTestStore(t)
	now := time.Unix(1710000000, 0).UTC()
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:    string(taskID),
		SessionID: "shs_review_source",
		HostMode:  "transcript",
		HostState: "fallback",
		StartedAt: now,
		Active:    true,
	}); err != nil {
		t.Fatalf("report shell session: %v", err)
	}
	if _, err := coord.RecordShellTranscript(context.Background(), RecordShellTranscriptRequest{
		TaskID:    string(taskID),
		SessionID: "shs_review_source",
		Chunks: []RecordShellTranscriptChunk{
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "worker 1", CreatedAt: now.Add(1 * time.Second)},
			{Source: shellsession.TranscriptSourceSystemNote, Content: "system 2", CreatedAt: now.Add(2 * time.Second)},
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "worker 3", CreatedAt: now.Add(3 * time.Second)},
		},
	}); err != nil {
		t.Fatalf("record shell transcript: %v", err)
	}
	_, err := coord.RecordShellTranscriptReview(context.Background(), RecordShellTranscriptReviewRequest{
		TaskID:          string(taskID),
		SessionID:       "shs_review_source",
		ReviewedUpToSeq: 3,
		Source:          string(shellsession.TranscriptSourceSystemNote),
	})
	if err == nil || (!strings.Contains(err.Error(), "not present in retained transcript evidence") && !strings.Contains(err.Error(), "outside retained transcript window")) {
		t.Fatalf("expected source-scoped sequence validation error, got %v", err)
	}
}

func TestRecordShellTranscriptReviewRejectsUnknownSession(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	_, err := coord.RecordShellTranscriptReview(context.Background(), RecordShellTranscriptReviewRequest{
		TaskID:          string(taskID),
		SessionID:       "shs_missing",
		ReviewedUpToSeq: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected unknown session error, got %v", err)
	}
}

func TestRecordShellTranscriptReviewRejectsUnknownTask(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())

	_, err := coord.RecordShellTranscriptReview(context.Background(), RecordShellTranscriptReviewRequest{
		TaskID:          "tsk_unknown",
		SessionID:       "shs_any",
		ReviewedUpToSeq: 1,
	})
	if err == nil {
		t.Fatal("expected unknown task error")
	}
}

func TestReadShellTranscriptReviewHistoryComputesLatestAndStaleDelta(t *testing.T) {
	store := newTestStore(t)
	now := time.Unix(1710000000, 0).UTC()
	coord, err := NewCoordinator(Dependencies{
		Store:                  store,
		IntentCompiler:         NewIntentStubCompiler(),
		BriefBuilder:           NewBriefBuilderV1(nil, nil),
		WorkerAdapter:          newFakeAdapterSuccess(),
		Synthesizer:            canonical.NewSimpleSynthesizer(),
		AnchorProvider:         defaultAnchor(),
		ShellSessions:          NewMemoryShellSessionRegistry(),
		ShellSessionStaleAfter: 24 * time.Hour,
		Clock:                  func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:    string(taskID),
		SessionID: "shs_review_history",
		HostMode:  "transcript",
		HostState: "transcript-only",
		StartedAt: now,
		Active:    true,
	}); err != nil {
		t.Fatalf("report shell session: %v", err)
	}
	if _, err := coord.RecordShellTranscript(context.Background(), RecordShellTranscriptRequest{
		TaskID:    string(taskID),
		SessionID: "shs_review_history",
		Chunks: []RecordShellTranscriptChunk{
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "w1", CreatedAt: now.Add(1 * time.Second)},
			{Source: shellsession.TranscriptSourceSystemNote, Content: "s2", CreatedAt: now.Add(2 * time.Second)},
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "w3", CreatedAt: now.Add(3 * time.Second)},
			{Source: shellsession.TranscriptSourceFallback, Content: "f4", CreatedAt: now.Add(4 * time.Second)},
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "w5", CreatedAt: now.Add(5 * time.Second)},
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "w6", CreatedAt: now.Add(6 * time.Second)},
			{Source: shellsession.TranscriptSourceSystemNote, Content: "s7", CreatedAt: now.Add(7 * time.Second)},
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "w8", CreatedAt: now.Add(8 * time.Second)},
		},
	}); err != nil {
		t.Fatalf("record shell transcript: %v", err)
	}
	if _, err := coord.RecordShellTranscriptReview(context.Background(), RecordShellTranscriptReviewRequest{
		TaskID:          string(taskID),
		SessionID:       "shs_review_history",
		ReviewedUpToSeq: 4,
		Summary:         "reviewed all retained evidence to seq 4",
	}); err != nil {
		t.Fatalf("record global transcript review: %v", err)
	}
	now = now.Add(time.Second)
	if _, err := coord.RecordShellTranscriptReview(context.Background(), RecordShellTranscriptReviewRequest{
		TaskID:          string(taskID),
		SessionID:       "shs_review_history",
		ReviewedUpToSeq: 8,
		Source:          string(shellsession.TranscriptSourceWorkerOutput),
		Summary:         "reviewed worker output to seq 8",
	}); err != nil {
		t.Fatalf("record source-scoped transcript review: %v", err)
	}
	if _, err := coord.RecordShellTranscript(context.Background(), RecordShellTranscriptRequest{
		TaskID:    string(taskID),
		SessionID: "shs_review_history",
		Chunks: []RecordShellTranscriptChunk{
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "w9", CreatedAt: now.Add(2 * time.Second)},
			{Source: shellsession.TranscriptSourceWorkerOutput, Content: "w10", CreatedAt: now.Add(3 * time.Second)},
		},
	}); err != nil {
		t.Fatalf("append post-review transcript evidence: %v", err)
	}

	history, err := coord.ReadShellTranscriptReviewHistory(context.Background(), ReadShellTranscriptReviewHistoryRequest{
		TaskID:    string(taskID),
		SessionID: "shs_review_history",
		Source:    string(shellsession.TranscriptSourceWorkerOutput),
		Limit:     5,
	})
	if err != nil {
		t.Fatalf("read transcript review history: %v", err)
	}
	if history.Closure.State != shellsession.TranscriptReviewClosureSourceScopedStale {
		t.Fatalf("expected source-scoped stale closure, got %+v", history.Closure)
	}
	if !history.Closure.HasUnreadNewerEvidence || history.Closure.OldestUnreviewedSequence != 9 || history.Closure.NewestRetainedSequence != 10 {
		t.Fatalf("expected stale-delta range 9-10, got %+v", history.Closure)
	}
	if len(history.Reviews) != 1 || history.Reviews[0].ReviewedUpToSequence != 8 {
		t.Fatalf("expected source-scoped history entry, got %+v", history.Reviews)
	}

	anyScope, err := coord.ReadShellTranscriptReviewHistory(context.Background(), ReadShellTranscriptReviewHistoryRequest{
		TaskID:    string(taskID),
		SessionID: "shs_review_history",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("read any-scope transcript review history: %v", err)
	}
	if anyScope.LatestReview == nil || anyScope.LatestReview.ReviewedUpToSequence != 8 || anyScope.LatestReview.SourceFilter != shellsession.TranscriptSourceWorkerOutput {
		t.Fatalf("expected latest any-scope review marker to be source-scoped seq 8, got %+v", anyScope.LatestReview)
	}
	if len(anyScope.Reviews) != 2 {
		t.Fatalf("expected two review history entries, got %+v", anyScope.Reviews)
	}
	if anyScope.Reviews[0].ClosureState == shellsession.TranscriptReviewClosureNone || anyScope.Reviews[0].OldestUnreviewedSequence == 0 {
		t.Fatalf("expected closure state and oldest-unreviewed metadata in review history, got %+v", anyScope.Reviews[0])
	}
}

func TestReadShellTranscriptReviewHistoryNoReviewReturnsClosureNone(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:    string(taskID),
		SessionID: "shs_review_none",
		HostMode:  "transcript",
		HostState: "fallback",
		StartedAt: time.Unix(1710000000, 0).UTC(),
		Active:    true,
	}); err != nil {
		t.Fatalf("report shell session: %v", err)
	}
	out, err := coord.ReadShellTranscriptReviewHistory(context.Background(), ReadShellTranscriptReviewHistoryRequest{
		TaskID:    string(taskID),
		SessionID: "shs_review_none",
		Limit:     5,
	})
	if err != nil {
		t.Fatalf("read transcript review history: %v", err)
	}
	if out.Closure.State != shellsession.TranscriptReviewClosureNone || out.LatestReview != nil || len(out.Reviews) != 0 {
		t.Fatalf("expected empty review history closure, got %+v", out)
	}
}

func TestReadShellTranscriptReviewHistoryRejectsUnknownTaskAndSession(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.ReadShellTranscriptReviewHistory(context.Background(), ReadShellTranscriptReviewHistoryRequest{
		TaskID:    string(taskID),
		SessionID: "shs_missing",
	}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected missing session error, got %v", err)
	}
	if _, err := coord.ReadShellTranscriptReviewHistory(context.Background(), ReadShellTranscriptReviewHistoryRequest{
		TaskID:    "tsk_unknown",
		SessionID: "shs_any",
	}); err == nil {
		t.Fatal("expected unknown task error")
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
		TaskID:                string(taskID),
		SessionID:             "shs_active",
		WorkerPreference:      "auto",
		ResolvedWorker:        "codex",
		WorkerSessionID:       "wks_active",
		WorkerSessionIDSource: shellsession.WorkerSessionIDSourceAuthoritative,
		AttachCapability:      shellsession.AttachCapabilityAttachable,
		HostMode:              "codex-pty",
		HostState:             "live",
		StartedAt:             reportedAt.Add(30 * time.Second),
		Active:                true,
	}); err != nil {
		t.Fatalf("report active shell session: %v", err)
	}
	now = reportedAt
	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:                string(taskID),
		SessionID:             "shs_active_then_stale",
		WorkerPreference:      "auto",
		ResolvedWorker:        "codex",
		WorkerSessionID:       "wks_stale",
		WorkerSessionIDSource: shellsession.WorkerSessionIDSourceAuthoritative,
		AttachCapability:      shellsession.AttachCapabilityAttachable,
		HostMode:              "codex-pty",
		HostState:             "live",
		StartedAt:             reportedAt,
		Active:                true,
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
		TaskID:                "tsk_identity",
		SessionID:             "shs_identity",
		WorkerSessionID:       "wks_identity",
		WorkerSessionIDSource: shellsession.WorkerSessionIDSourceAuthoritative,
		AttachCapability:      shellsession.AttachCapabilityAttachable,
		StartedAt:             first,
		LastUpdatedAt:         first,
		HostState:             "live",
		Active:                true,
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
		TaskID:                string(taskID),
		SessionID:             "shs_1",
		WorkerPreference:      "auto",
		ResolvedWorker:        "claude",
		WorkerSessionID:       "wks_1",
		WorkerSessionIDSource: shellsession.WorkerSessionIDSourceAuthoritative,
		AttachCapability:      shellsession.AttachCapabilityAttachable,
		HostMode:              "claude-pty",
		HostState:             "live",
		StartedAt:             now,
		Active:                true,
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
		TaskID:                string(taskID),
		SessionID:             "shs_started_at",
		WorkerSessionID:       "wks_started_at",
		WorkerSessionIDSource: shellsession.WorkerSessionIDSourceAuthoritative,
		AttachCapability:      shellsession.AttachCapabilityAttachable,
		StartedAt:             first,
		HostMode:              "codex-pty",
		HostState:             "live",
		Active:                true,
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
		TaskID:                string(taskID),
		SessionID:             "shs_no_proof",
		WorkerPreference:      "auto",
		ResolvedWorker:        "claude",
		WorkerSessionID:       "wks_no_proof",
		WorkerSessionIDSource: shellsession.WorkerSessionIDSourceAuthoritative,
		AttachCapability:      shellsession.AttachCapabilityAttachable,
		HostMode:              "claude-pty",
		HostState:             "live",
		StartedAt:             time.Unix(1710000000, 0).UTC(),
		Active:                true,
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

func TestDurableShellSessionEventsPersistAcrossStoreReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tuku-shell-events.db")
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
	if _, err := coord.ReportShellSession(context.Background(), ReportShellSessionRequest{
		TaskID:                string(taskID),
		SessionID:             "shs_event_durable",
		WorkerPreference:      "codex",
		ResolvedWorker:        "codex",
		WorkerSessionID:       "wks_event_durable",
		WorkerSessionIDSource: shellsession.WorkerSessionIDSourceAuthoritative,
		AttachCapability:      shellsession.AttachCapabilityAttachable,
		HostMode:              "codex-pty",
		HostState:             "live",
		StartedAt:             time.Unix(1710000000, 0).UTC(),
		Active:                true,
	}); err != nil {
		t.Fatalf("report shell session: %v", err)
	}
	if _, err := coord.RecordShellLifecycle(context.Background(), RecordShellLifecycleRequest{
		TaskID:    string(taskID),
		SessionID: "shs_event_durable",
		Kind:      ShellLifecycleHostStarted,
		HostMode:  "codex-pty",
		HostState: "live",
		InputLive: true,
	}); err != nil {
		t.Fatalf("record shell lifecycle: %v", err)
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
	events, err := coord2.listShellSessionEvents(taskID, "shs_event_durable", 10)
	if err != nil {
		t.Fatalf("list durable shell events: %v", err)
	}
	if len(events) < 2 {
		t.Fatalf("expected at least two durable shell events, got %d", len(events))
	}
}

func TestMemoryShellSessionRegistryPreservesSessionIdentityAcrossStateChanges(t *testing.T) {
	registry := NewMemoryShellSessionRegistry()
	first := time.Unix(1710000000, 0).UTC()
	second := first.Add(10 * time.Minute)

	if err := registry.Upsert(ShellSessionRecord{
		TaskID:                "tsk_identity",
		SessionID:             "shs_identity",
		WorkerSessionID:       "wks_identity",
		WorkerSessionIDSource: shellsession.WorkerSessionIDSourceAuthoritative,
		AttachCapability:      shellsession.AttachCapabilityAttachable,
		StartedAt:             first,
		LastUpdatedAt:         first,
		HostState:             "live",
		Active:                true,
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
