package orchestrator

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/shellsession"
)

const DefaultShellSessionStaleAfter = 90 * time.Second
const defaultShellTranscriptReviewHistoryPreview = 3

type ShellSessionClass string

const (
	ShellSessionClassAttachable         ShellSessionClass = "attachable"
	ShellSessionClassActiveUnattachable ShellSessionClass = "active_unattachable"
	ShellSessionClassStale              ShellSessionClass = "stale"
	ShellSessionClassEnded              ShellSessionClass = "ended"
)

type ShellSessionRecord = shellsession.Record

type ShellSessionView struct {
	TaskID                           common.TaskID
	SessionID                        string
	WorkerPreference                 string
	ResolvedWorker                   string
	WorkerSessionID                  string
	WorkerSessionIDSource            shellsession.WorkerSessionIDSource
	AttachCapability                 shellsession.AttachCapability
	HostMode                         string
	HostState                        string
	StartedAt                        time.Time
	LastUpdatedAt                    time.Time
	Active                           bool
	Note                             string
	SessionClass                     ShellSessionClass
	SessionClassReason               string
	ReattachGuidance                 string
	OperatorSummary                  string
	TranscriptState                  shellsession.TranscriptState
	TranscriptRetainedChunks         int
	TranscriptDroppedChunks          int
	TranscriptRetentionLimit         int
	TranscriptOldestSequence         int64
	TranscriptNewestSequence         int64
	TranscriptLastChunkAt            time.Time
	TranscriptReviewID               common.EventID
	TranscriptReviewSource           shellsession.TranscriptSource
	TranscriptReviewedUpTo           int64
	TranscriptReviewSummary          string
	TranscriptReviewAt               time.Time
	TranscriptReviewStale            bool
	TranscriptReviewNewer            int
	TranscriptReviewClosureState     shellsession.TranscriptReviewClosureState
	TranscriptReviewOldestUnreviewed int64
	TranscriptRecentReviews          []ShellTranscriptReviewSummary
	LatestEventID                    common.EventID
	LatestEventKind                  string
	LatestEventAt                    time.Time
	LatestEventNote                  string
	LatestEventHostState             string
	LatestEventInputLive             bool
	LatestEventExitCode              *int
	LatestEventSessionMode           string
}

type ShellSessionRegistry interface {
	Upsert(record ShellSessionRecord) error
	ListByTask(taskID common.TaskID) ([]ShellSessionRecord, error)
}

type MemoryShellSessionRegistry struct {
	mu                         sync.Mutex
	byTask                     map[common.TaskID]map[string]ShellSessionRecord
	eventsByTask               map[common.TaskID][]shellsession.Event
	transcriptsBySession       map[string][]shellsession.TranscriptChunk
	transcriptSummaryBySession map[string]shellsession.TranscriptSummary
	transcriptReviewsBySession map[string][]shellsession.TranscriptReview
	transcriptReviewGapAcks    map[string][]shellsession.TranscriptReviewGapAcknowledgment
}

func NewMemoryShellSessionRegistry() *MemoryShellSessionRegistry {
	return &MemoryShellSessionRegistry{
		byTask:                     make(map[common.TaskID]map[string]ShellSessionRecord),
		eventsByTask:               make(map[common.TaskID][]shellsession.Event),
		transcriptsBySession:       make(map[string][]shellsession.TranscriptChunk),
		transcriptSummaryBySession: make(map[string]shellsession.TranscriptSummary),
		transcriptReviewsBySession: make(map[string][]shellsession.TranscriptReview),
		transcriptReviewGapAcks:    make(map[string][]shellsession.TranscriptReviewGapAcknowledgment),
	}
}

func (r *MemoryShellSessionRegistry) Upsert(record ShellSessionRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.byTask[record.TaskID] == nil {
		r.byTask[record.TaskID] = make(map[string]ShellSessionRecord)
	}
	existing, ok := r.byTask[record.TaskID][record.SessionID]
	if ok {
		if !existing.StartedAt.IsZero() {
			record.StartedAt = existing.StartedAt
		} else if record.StartedAt.IsZero() {
			record.StartedAt = existing.StartedAt
		}
		if strings.TrimSpace(record.WorkerPreference) == "" {
			record.WorkerPreference = existing.WorkerPreference
		}
		if strings.TrimSpace(record.ResolvedWorker) == "" {
			record.ResolvedWorker = existing.ResolvedWorker
		}
		if strings.TrimSpace(record.WorkerSessionID) == "" {
			record.WorkerSessionID = existing.WorkerSessionID
		}
		if record.WorkerSessionIDSource == shellsession.WorkerSessionIDSourceNone || record.WorkerSessionIDSource == "" {
			record.WorkerSessionIDSource = existing.WorkerSessionIDSource
		}
		if record.AttachCapability == "" {
			record.AttachCapability = existing.AttachCapability
		}
	}
	if record.StartedAt.IsZero() {
		record.StartedAt = record.LastUpdatedAt
	}
	if record.AttachCapability == "" {
		record.AttachCapability = shellsession.AttachCapabilityNone
	}
	record.WorkerSessionIDSource = normalizeWorkerSessionIDSource(record.WorkerSessionIDSource, record.WorkerSessionID)
	r.byTask[record.TaskID][record.SessionID] = record
	return nil
}

func (r *MemoryShellSessionRegistry) ListByTask(taskID common.TaskID) ([]ShellSessionRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	taskSessions := r.byTask[taskID]
	if len(taskSessions) == 0 {
		return nil, nil
	}
	out := make([]ShellSessionRecord, 0, len(taskSessions))
	for _, record := range taskSessions {
		out = append(out, record)
	}
	return out, nil
}

func (r *MemoryShellSessionRegistry) AppendEvent(event shellsession.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if event.TaskID == "" {
		return fmt.Errorf("shell session event task id is required")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	r.eventsByTask[event.TaskID] = append(r.eventsByTask[event.TaskID], event)
	return nil
}

func (r *MemoryShellSessionRegistry) ListEvents(taskID common.TaskID, sessionID string, limit int) ([]shellsession.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	events := r.eventsByTask[taskID]
	if len(events) == 0 {
		return nil, nil
	}
	sessionID = strings.TrimSpace(sessionID)
	filtered := make([]shellsession.Event, 0, len(events))
	for _, event := range events {
		if sessionID != "" && event.SessionID != sessionID {
			continue
		}
		filtered = append(filtered, event)
	}
	if len(filtered) == 0 {
		return nil, nil
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return string(filtered[i].EventID) > string(filtered[j].EventID)
		}
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

func (r *MemoryShellSessionRegistry) AppendTranscript(taskID common.TaskID, sessionID string, chunks []shellsession.TranscriptChunk, retention int) (shellsession.TranscriptSummary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if taskID == "" {
		return shellsession.TranscriptSummary{}, fmt.Errorf("task id is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return shellsession.TranscriptSummary{}, fmt.Errorf("session id is required")
	}
	if retention <= 0 {
		retention = shellsession.DefaultTranscriptRetentionChunks
	}
	key := transcriptSessionKey(taskID, sessionID)
	summary := r.transcriptSummaryBySession[key]
	summary.TaskID = taskID
	summary.SessionID = sessionID
	summary.RetentionLimit = retention
	summary.RetainedChunks = len(r.transcriptsBySession[key])

	for _, chunk := range chunks {
		content := strings.TrimSpace(chunk.Content)
		if content == "" {
			continue
		}
		summary.LastSequenceNo++
		createdAt := chunk.CreatedAt.UTC()
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		entry := shellsession.TranscriptChunk{
			ChunkID:    common.EventID(fmt.Sprintf("sst_%d_%d", time.Now().UTC().UnixNano(), summary.LastSequenceNo)),
			TaskID:     taskID,
			SessionID:  sessionID,
			SequenceNo: summary.LastSequenceNo,
			Source:     normalizeTranscriptSourceValue(chunk.Source),
			Content:    chunk.Content,
			CreatedAt:  createdAt,
		}
		r.transcriptsBySession[key] = append(r.transcriptsBySession[key], entry)
		if createdAt.After(summary.LastChunkAt) {
			summary.LastChunkAt = createdAt
		}
	}
	if len(r.transcriptsBySession[key]) > retention {
		dropped := len(r.transcriptsBySession[key]) - retention
		r.transcriptsBySession[key] = r.transcriptsBySession[key][dropped:]
		summary.DroppedChunks += dropped
	}
	hydrateMemoryTranscriptSummary(&summary, r.transcriptsBySession[key], retention)
	r.transcriptSummaryBySession[key] = summary
	return summary, nil
}

func (r *MemoryShellSessionRegistry) ListTranscript(taskID common.TaskID, sessionID string, limit int) ([]shellsession.TranscriptChunk, error) {
	chunks, _, err := r.ListTranscriptPage(taskID, sessionID, 0, limit, "")
	return chunks, err
}

func (r *MemoryShellSessionRegistry) ListTranscriptPage(taskID common.TaskID, sessionID string, beforeSequence int64, limit int, source shellsession.TranscriptSource) ([]shellsession.TranscriptChunk, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	sessionID = strings.TrimSpace(sessionID)
	key := transcriptSessionKey(taskID, sessionID)
	transcript := r.transcriptsBySession[key]
	if len(transcript) == 0 {
		return nil, false, nil
	}
	source = shellsession.TranscriptSource(strings.TrimSpace(string(source)))
	if source != "" && source != shellsession.TranscriptSourceWorkerOutput && source != shellsession.TranscriptSourceSystemNote && source != shellsession.TranscriptSourceFallback {
		return nil, false, fmt.Errorf("unsupported transcript source filter %q", source)
	}
	filtered := make([]shellsession.TranscriptChunk, 0, len(transcript))
	for _, chunk := range transcript {
		if beforeSequence > 0 && chunk.SequenceNo >= beforeSequence {
			continue
		}
		if source != "" && chunk.Source != source {
			continue
		}
		filtered = append(filtered, chunk)
	}
	if len(filtered) == 0 {
		return nil, false, nil
	}
	if limit <= 0 {
		limit = 40
	}
	if len(filtered) <= limit {
		return append([]shellsession.TranscriptChunk{}, filtered...), false, nil
	}
	start := len(filtered) - limit
	return append([]shellsession.TranscriptChunk{}, filtered[start:]...), start > 0, nil
}

func (r *MemoryShellSessionRegistry) TranscriptSummary(taskID common.TaskID, sessionID string, retention int) (shellsession.TranscriptSummary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	sessionID = strings.TrimSpace(sessionID)
	key := transcriptSessionKey(taskID, sessionID)
	summary := r.transcriptSummaryBySession[key]
	summary.TaskID = taskID
	summary.SessionID = sessionID
	if retention <= 0 {
		retention = shellsession.DefaultTranscriptRetentionChunks
	}
	hydrateMemoryTranscriptSummary(&summary, r.transcriptsBySession[key], retention)
	summary.RetentionLimit = retention
	return summary, nil
}

func (r *MemoryShellSessionRegistry) AppendTranscriptReview(review shellsession.TranscriptReview) (shellsession.TranscriptReview, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	review.TaskID = common.TaskID(strings.TrimSpace(string(review.TaskID)))
	review.SessionID = strings.TrimSpace(review.SessionID)
	review.SourceFilter = shellsession.TranscriptSource(strings.TrimSpace(string(review.SourceFilter)))
	review.Summary = strings.TrimSpace(review.Summary)
	if review.TaskID == "" {
		return shellsession.TranscriptReview{}, fmt.Errorf("task id is required")
	}
	if review.SessionID == "" {
		return shellsession.TranscriptReview{}, fmt.Errorf("session id is required")
	}
	if review.ReviewedUpToSequence <= 0 {
		return shellsession.TranscriptReview{}, fmt.Errorf("reviewed_up_to_sequence must be greater than zero")
	}
	if review.ReviewID == "" {
		review.ReviewID = common.EventID(fmt.Sprintf("srev_%d", time.Now().UTC().UnixNano()))
	}
	if review.CreatedAt.IsZero() {
		review.CreatedAt = time.Now().UTC()
	}
	key := transcriptSessionKey(review.TaskID, review.SessionID)
	r.transcriptReviewsBySession[key] = append(r.transcriptReviewsBySession[key], review)
	return review, nil
}

func (r *MemoryShellSessionRegistry) ListTranscriptReviews(taskID common.TaskID, sessionID string, source shellsession.TranscriptSource, limit int) ([]shellsession.TranscriptReview, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	taskID = common.TaskID(strings.TrimSpace(string(taskID)))
	sessionID = strings.TrimSpace(sessionID)
	source = shellsession.TranscriptSource(strings.TrimSpace(string(source)))
	if taskID == "" {
		return nil, fmt.Errorf("task id is required")
	}
	if sessionID == "" {
		return nil, fmt.Errorf("session id is required")
	}
	if limit <= 0 {
		limit = 20
	}

	key := transcriptSessionKey(taskID, sessionID)
	reviews := r.transcriptReviewsBySession[key]
	if len(reviews) == 0 {
		return nil, nil
	}
	filtered := make([]shellsession.TranscriptReview, 0, len(reviews))
	for _, review := range reviews {
		if shellsession.TranscriptSource(strings.TrimSpace(string(review.SourceFilter))) != source {
			continue
		}
		filtered = append(filtered, review)
	}
	if len(filtered) == 0 {
		return nil, nil
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return string(filtered[i].ReviewID) > string(filtered[j].ReviewID)
		}
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return append([]shellsession.TranscriptReview{}, filtered...), nil
}

func (r *MemoryShellSessionRegistry) ListTranscriptReviewsAnyScope(taskID common.TaskID, sessionID string, limit int) ([]shellsession.TranscriptReview, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	taskID = common.TaskID(strings.TrimSpace(string(taskID)))
	sessionID = strings.TrimSpace(sessionID)
	if taskID == "" {
		return nil, fmt.Errorf("task id is required")
	}
	if sessionID == "" {
		return nil, fmt.Errorf("session id is required")
	}
	if limit <= 0 {
		limit = 20
	}

	key := transcriptSessionKey(taskID, sessionID)
	reviews := r.transcriptReviewsBySession[key]
	if len(reviews) == 0 {
		return nil, nil
	}
	filtered := append([]shellsession.TranscriptReview{}, reviews...)
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return string(filtered[i].ReviewID) > string(filtered[j].ReviewID)
		}
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

func (r *MemoryShellSessionRegistry) LatestTranscriptReview(taskID common.TaskID, sessionID string, source shellsession.TranscriptSource) (*shellsession.TranscriptReview, error) {
	reviews, err := r.ListTranscriptReviews(taskID, sessionID, source, 1)
	if err != nil {
		return nil, err
	}
	if len(reviews) == 0 {
		return nil, nil
	}
	review := reviews[0]
	return &review, nil
}

func (r *MemoryShellSessionRegistry) LatestTranscriptReviewAnyScope(taskID common.TaskID, sessionID string) (*shellsession.TranscriptReview, error) {
	reviews, err := r.ListTranscriptReviewsAnyScope(taskID, sessionID, 1)
	if err != nil {
		return nil, err
	}
	if len(reviews) == 0 {
		return nil, nil
	}
	review := reviews[0]
	return &review, nil
}

func (r *MemoryShellSessionRegistry) AppendTranscriptReviewGapAcknowledgment(record shellsession.TranscriptReviewGapAcknowledgment) (shellsession.TranscriptReviewGapAcknowledgment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	record.TaskID = common.TaskID(strings.TrimSpace(string(record.TaskID)))
	record.SessionID = strings.TrimSpace(record.SessionID)
	record.Summary = strings.TrimSpace(record.Summary)
	record.ActionContext = strings.TrimSpace(record.ActionContext)
	record.ReviewState = strings.TrimSpace(record.ReviewState)
	if record.TaskID == "" {
		return shellsession.TranscriptReviewGapAcknowledgment{}, fmt.Errorf("task id is required")
	}
	if record.SessionID == "" {
		return shellsession.TranscriptReviewGapAcknowledgment{}, fmt.Errorf("session id is required")
	}
	if record.Class == "" {
		return shellsession.TranscriptReviewGapAcknowledgment{}, fmt.Errorf("acknowledgment class is required")
	}
	if record.AcknowledgmentID == "" {
		record.AcknowledgmentID = common.EventID(fmt.Sprintf("sack_%d", time.Now().UTC().UnixNano()))
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	key := transcriptSessionKey(record.TaskID, record.SessionID)
	r.transcriptReviewGapAcks[key] = append(r.transcriptReviewGapAcks[key], record)
	return record, nil
}

func (r *MemoryShellSessionRegistry) LatestTranscriptReviewGapAcknowledgment(taskID common.TaskID, sessionID string) (*shellsession.TranscriptReviewGapAcknowledgment, error) {
	records, err := r.ListTranscriptReviewGapAcknowledgments(taskID, sessionID, 1)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}
	record := records[0]
	return &record, nil
}

func (r *MemoryShellSessionRegistry) ListTranscriptReviewGapAcknowledgments(taskID common.TaskID, sessionID string, limit int) ([]shellsession.TranscriptReviewGapAcknowledgment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	taskID = common.TaskID(strings.TrimSpace(string(taskID)))
	sessionID = strings.TrimSpace(sessionID)
	if taskID == "" {
		return nil, fmt.Errorf("task id is required")
	}
	if limit <= 0 {
		limit = 20
	}
	filtered := make([]shellsession.TranscriptReviewGapAcknowledgment, 0, limit)
	if sessionID != "" {
		key := transcriptSessionKey(taskID, sessionID)
		filtered = append(filtered, r.transcriptReviewGapAcks[key]...)
	} else {
		prefix := string(taskID) + "::"
		for key, items := range r.transcriptReviewGapAcks {
			if !strings.HasPrefix(key, prefix) {
				continue
			}
			filtered = append(filtered, items...)
		}
	}
	if len(filtered) == 0 {
		return nil, nil
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return string(filtered[i].AcknowledgmentID) > string(filtered[j].AcknowledgmentID)
		}
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return append([]shellsession.TranscriptReviewGapAcknowledgment{}, filtered...), nil
}

func transcriptSessionKey(taskID common.TaskID, sessionID string) string {
	return string(taskID) + "::" + strings.TrimSpace(sessionID)
}

func hydrateMemoryTranscriptSummary(summary *shellsession.TranscriptSummary, retained []shellsession.TranscriptChunk, retention int) {
	if summary == nil {
		return
	}
	summary.RetentionLimit = retention
	summary.RetainedChunks = len(retained)
	if len(retained) == 0 {
		summary.OldestSequenceNo = 0
		summary.NewestSequenceNo = 0
		summary.SourceCounts = nil
		return
	}
	oldest := retained[0]
	newest := retained[len(retained)-1]
	summary.OldestSequenceNo = oldest.SequenceNo
	summary.OldestChunkAt = oldest.CreatedAt
	summary.NewestSequenceNo = newest.SequenceNo
	summary.NewestChunkAt = newest.CreatedAt
	summary.LastSequenceNo = newest.SequenceNo
	summary.LastChunkAt = newest.CreatedAt

	sourceCounts := map[shellsession.TranscriptSource]int{}
	for _, chunk := range retained {
		source := normalizeTranscriptSourceValue(chunk.Source)
		sourceCounts[source]++
	}
	summary.SourceCounts = summary.SourceCounts[:0]
	for _, source := range []shellsession.TranscriptSource{
		shellsession.TranscriptSourceFallback,
		shellsession.TranscriptSourceSystemNote,
		shellsession.TranscriptSourceWorkerOutput,
	} {
		if sourceCounts[source] <= 0 {
			continue
		}
		summary.SourceCounts = append(summary.SourceCounts, shellsession.TranscriptSourceCount{
			Source: source,
			Chunks: sourceCounts[source],
		})
	}
}

func normalizeTranscriptSourceValue(source shellsession.TranscriptSource) shellsession.TranscriptSource {
	switch source {
	case shellsession.TranscriptSourceWorkerOutput, shellsession.TranscriptSourceSystemNote, shellsession.TranscriptSourceFallback:
		return source
	default:
		return shellsession.TranscriptSourceWorkerOutput
	}
}

type ReportShellSessionRequest struct {
	TaskID                string
	SessionID             string
	WorkerPreference      string
	ResolvedWorker        string
	WorkerSessionID       string
	WorkerSessionIDSource shellsession.WorkerSessionIDSource
	AttachCapability      shellsession.AttachCapability
	HostMode              string
	HostState             string
	StartedAt             time.Time
	Active                bool
	Note                  string
}

type ReportShellSessionResult struct {
	TaskID  common.TaskID
	Session ShellSessionView
}

type ListShellSessionsResult struct {
	TaskID   common.TaskID
	Sessions []ShellSessionView
}

func classifyShellSession(record ShellSessionRecord, now time.Time, staleAfter time.Duration) ShellSessionView {
	view := ShellSessionView{
		TaskID:                record.TaskID,
		SessionID:             record.SessionID,
		WorkerPreference:      record.WorkerPreference,
		ResolvedWorker:        record.ResolvedWorker,
		WorkerSessionID:       record.WorkerSessionID,
		WorkerSessionIDSource: normalizeWorkerSessionIDSource(record.WorkerSessionIDSource, record.WorkerSessionID),
		AttachCapability:      record.AttachCapability,
		HostMode:              record.HostMode,
		HostState:             record.HostState,
		StartedAt:             record.StartedAt,
		LastUpdatedAt:         record.LastUpdatedAt,
		Active:                record.Active,
		Note:                  record.Note,
		SessionClass:          ShellSessionClassActiveUnattachable,
	}
	if !record.Active {
		view.SessionClass = ShellSessionClassEnded
		view.SessionClassReason = "session is ended; no active host continuity remains"
		view.ReattachGuidance = "start a new shell session; ended sessions cannot be reattached"
		view.OperatorSummary = "ended session; reattach unavailable"
		return view
	}
	if staleAfter > 0 && !record.LastUpdatedAt.IsZero() && now.Sub(record.LastUpdatedAt) > staleAfter {
		view.SessionClass = ShellSessionClassStale
		view.SessionClassReason = fmt.Sprintf("session is stale; last update is older than %s", staleAfter.Truncate(time.Second))
		view.ReattachGuidance = "open a new shell session; stale sessions are not trusted as attach targets"
		view.OperatorSummary = "stale session; live continuity is unproven"
		return view
	}
	if isAttachableRecord(record) {
		view.SessionClass = ShellSessionClassAttachable
		view.SessionClassReason = "active PTY session with authoritative worker session id and attach capability"
		view.ReattachGuidance = fmt.Sprintf("reattach with `tuku shell --task %s --reattach %s`", record.TaskID, record.SessionID)
		view.OperatorSummary = fmt.Sprintf("attachable %s session", nonEmptyWorker(record.ResolvedWorker, record.WorkerPreference))
		return view
	}
	view.SessionClassReason = activeUnattachableReason(view)
	view.ReattachGuidance = activeUnattachableGuidance(view)
	view.OperatorSummary = "active but not reattachable"
	return view
}

func classifyShellSessions(records []ShellSessionRecord, now time.Time, staleAfter time.Duration) []ShellSessionView {
	out := make([]ShellSessionView, 0, len(records))
	for _, record := range records {
		out = append(out, classifyShellSession(record, now, staleAfter))
	}
	sort.Slice(out, func(i, j int) bool {
		if rank := shellSessionClassRank(out[i].SessionClass) - shellSessionClassRank(out[j].SessionClass); rank != 0 {
			return rank < 0
		}
		return out[i].LastUpdatedAt.After(out[j].LastUpdatedAt)
	})
	return out
}

func enrichShellSessionViewsWithEvents(views []ShellSessionView, events []shellsession.Event) {
	if len(views) == 0 || len(events) == 0 {
		return
	}
	latestBySession := make(map[string]shellsession.Event, len(views))
	for _, event := range events {
		if strings.TrimSpace(event.SessionID) == "" {
			continue
		}
		if _, exists := latestBySession[event.SessionID]; exists {
			continue
		}
		latestBySession[event.SessionID] = event
	}
	for i := range views {
		event, ok := latestBySession[views[i].SessionID]
		if !ok {
			continue
		}
		views[i].LatestEventID = event.EventID
		views[i].LatestEventKind = string(event.Kind)
		views[i].LatestEventAt = event.CreatedAt
		views[i].LatestEventNote = event.Note
		views[i].LatestEventHostState = event.HostState
		views[i].LatestEventInputLive = event.InputLive
		views[i].LatestEventExitCode = event.ExitCode
		views[i].LatestEventSessionMode = event.HostMode
	}
}

func activeUnattachableReason(view ShellSessionView) string {
	if isTranscriptOnlyMode(view.HostMode, view.HostState) {
		return "session is in transcript or fallback mode; live worker attach is unavailable"
	}
	if strings.TrimSpace(view.WorkerSessionID) == "" {
		return "session has no worker session id"
	}
	switch view.WorkerSessionIDSource {
	case shellsession.WorkerSessionIDSourceHeuristic:
		return "worker session id was heuristically detected from output and is not authoritative"
	case shellsession.WorkerSessionIDSourceUnknown:
		return "worker session id source is unknown; authoritative attach proof is missing"
	case shellsession.WorkerSessionIDSourceNone:
		return "worker session id is missing"
	}
	if view.AttachCapability != shellsession.AttachCapabilityAttachable {
		return "host reported attach capability as none"
	}
	return "session is active but does not satisfy attachability requirements"
}

func activeUnattachableGuidance(view ShellSessionView) string {
	if isTranscriptOnlyMode(view.HostMode, view.HostState) {
		return "review transcript evidence and start a new live shell session if more work is needed"
	}
	if strings.TrimSpace(view.WorkerSessionID) == "" {
		return "reattach requires a worker session id; keep this shell open or start a new session"
	}
	if view.WorkerSessionIDSource != shellsession.WorkerSessionIDSourceAuthoritative {
		return "reattach requires an authoritative worker session id; heuristic or unknown ids are not eligible"
	}
	if view.AttachCapability != shellsession.AttachCapabilityAttachable {
		return "current host/worker runtime does not support reattach for this session"
	}
	return "open a new shell session for continued work"
}

func nonEmptyWorker(primary string, fallback string) string {
	if value := strings.TrimSpace(primary); value != "" {
		return value
	}
	if value := strings.TrimSpace(fallback); value != "" {
		return value
	}
	return "worker"
}

func isTranscriptOnlyMode(mode string, state string) bool {
	mode = strings.TrimSpace(strings.ToLower(mode))
	state = strings.TrimSpace(strings.ToLower(state))
	if mode == "transcript" {
		return true
	}
	return state == "fallback" || state == "transcript-only"
}

func shellSessionClassRank(class ShellSessionClass) int {
	switch class {
	case ShellSessionClassAttachable:
		return 0
	case ShellSessionClassActiveUnattachable:
		return 1
	case ShellSessionClassStale:
		return 2
	default:
		return 3
	}
}

func (c *Coordinator) ReportShellSession(ctx context.Context, req ReportShellSessionRequest) (ReportShellSessionResult, error) {
	_ = ctx
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return ReportShellSessionResult{}, fmt.Errorf("task id is required")
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		return ReportShellSessionResult{}, fmt.Errorf("session id is required")
	}
	if _, err := c.store.Capsules().Get(taskID); err != nil {
		return ReportShellSessionResult{}, err
	}

	now := c.clock()
	record := ShellSessionRecord{
		TaskID:                taskID,
		SessionID:             sessionID,
		WorkerPreference:      strings.TrimSpace(req.WorkerPreference),
		ResolvedWorker:        strings.TrimSpace(req.ResolvedWorker),
		WorkerSessionID:       strings.TrimSpace(req.WorkerSessionID),
		WorkerSessionIDSource: normalizeWorkerSessionIDSource(req.WorkerSessionIDSource, strings.TrimSpace(req.WorkerSessionID)),
		AttachCapability:      normalizeAttachCapability(req.AttachCapability),
		HostMode:              strings.TrimSpace(req.HostMode),
		HostState:             strings.TrimSpace(req.HostState),
		StartedAt:             req.StartedAt.UTC(),
		LastUpdatedAt:         now,
		Active:                req.Active,
		Note:                  strings.TrimSpace(req.Note),
	}
	if err := c.shellSessions.Upsert(record); err != nil {
		return ReportShellSessionResult{}, err
	}
	persisted, err := c.loadShellSessionRecord(taskID, sessionID)
	if err != nil {
		return ReportShellSessionResult{}, err
	}
	if err := c.appendShellSessionEvent(shellsession.Event{
		TaskID:                taskID,
		SessionID:             sessionID,
		Kind:                  shellsession.EventKindSessionReported,
		HostMode:              persisted.HostMode,
		HostState:             persisted.HostState,
		WorkerSessionID:       persisted.WorkerSessionID,
		WorkerSessionIDSource: persisted.WorkerSessionIDSource,
		AttachCapability:      persisted.AttachCapability,
		Active:                persisted.Active,
		InputLive:             persisted.Active,
		Note:                  persisted.Note,
		CreatedAt:             now,
	}); err != nil {
		return ReportShellSessionResult{}, err
	}
	view := classifyShellSession(persisted, now, c.shellSessionStaleAfter)
	if events, err := c.listShellSessionEvents(taskID, sessionID, 1); err == nil {
		enriched := []ShellSessionView{view}
		enrichShellSessionViewsWithEvents(enriched, events)
		view = enriched[0]
	} else {
		return ReportShellSessionResult{}, err
	}
	if summary, err := c.shellTranscriptSummary(taskID, sessionID); err == nil {
		applyTranscriptSummaryToView(&view, summary)
	} else {
		return ReportShellSessionResult{}, err
	}
	review, err := c.latestShellTranscriptReviewAnyScope(taskID, sessionID)
	if err != nil {
		return ReportShellSessionResult{}, err
	}
	reviewScopeNewest, err := c.newestTranscriptSequenceForReviewScope(taskID, sessionID, review, view.TranscriptNewestSequence)
	if err != nil {
		return ReportShellSessionResult{}, err
	}
	applyTranscriptReviewToView(&view, review, reviewScopeNewest)
	reviews, err := c.listShellTranscriptReviewsAnyScope(taskID, sessionID, defaultShellTranscriptReviewHistoryPreview)
	if err != nil {
		return ReportShellSessionResult{}, err
	}
	if err := c.applyTranscriptReviewHistoryToView(taskID, sessionID, &view, reviews); err != nil {
		return ReportShellSessionResult{}, err
	}
	return ReportShellSessionResult{TaskID: taskID, Session: view}, nil
}

func (c *Coordinator) ListShellSessions(ctx context.Context, taskID string) (ListShellSessionsResult, error) {
	_ = ctx
	id := common.TaskID(strings.TrimSpace(taskID))
	if id == "" {
		return ListShellSessionsResult{}, fmt.Errorf("task id is required")
	}
	if _, err := c.store.Capsules().Get(id); err != nil {
		return ListShellSessionsResult{}, err
	}
	records, err := c.shellSessions.ListByTask(id)
	if err != nil {
		return ListShellSessionsResult{}, err
	}
	views, err := c.classifiedShellSessionsFromRecords(id, records)
	if err != nil {
		return ListShellSessionsResult{}, err
	}
	return ListShellSessionsResult{
		TaskID:   id,
		Sessions: views,
	}, nil
}

func (c *Coordinator) classifiedShellSessions(taskID common.TaskID) ([]ShellSessionView, error) {
	records, err := c.shellSessions.ListByTask(taskID)
	if err != nil {
		return nil, err
	}
	return c.classifiedShellSessionsFromRecords(taskID, records)
}

func (c *Coordinator) classifiedShellSessionsFromRecords(taskID common.TaskID, records []ShellSessionRecord) ([]ShellSessionView, error) {
	views := classifyShellSessions(records, c.clock(), c.shellSessionStaleAfter)
	events, err := c.listShellSessionEvents(taskID, "", len(records)*5+20)
	if err != nil {
		return nil, err
	}
	enrichShellSessionViewsWithEvents(views, events)
	for i := range views {
		summary, summaryErr := c.shellTranscriptSummary(taskID, views[i].SessionID)
		if summaryErr != nil {
			return nil, summaryErr
		}
		applyTranscriptSummaryToView(&views[i], summary)
		review, reviewErr := c.latestShellTranscriptReviewAnyScope(taskID, views[i].SessionID)
		if reviewErr != nil {
			return nil, reviewErr
		}
		reviewScopeNewest, newestErr := c.newestTranscriptSequenceForReviewScope(taskID, views[i].SessionID, review, views[i].TranscriptNewestSequence)
		if newestErr != nil {
			return nil, newestErr
		}
		applyTranscriptReviewToView(&views[i], review, reviewScopeNewest)
		reviews, historyErr := c.listShellTranscriptReviewsAnyScope(taskID, views[i].SessionID, defaultShellTranscriptReviewHistoryPreview)
		if historyErr != nil {
			return nil, historyErr
		}
		if err := c.applyTranscriptReviewHistoryToView(taskID, views[i].SessionID, &views[i], reviews); err != nil {
			return nil, err
		}
	}
	return views, nil
}

func (c *Coordinator) loadShellSessionRecord(taskID common.TaskID, sessionID string) (ShellSessionRecord, error) {
	records, err := c.shellSessions.ListByTask(taskID)
	if err != nil {
		return ShellSessionRecord{}, err
	}
	for _, record := range records {
		if record.SessionID == sessionID {
			return record, nil
		}
	}
	return ShellSessionRecord{}, fmt.Errorf("shell session %s not found for task %s after upsert", sessionID, taskID)
}

func normalizeAttachCapability(value shellsession.AttachCapability) shellsession.AttachCapability {
	switch value {
	case shellsession.AttachCapabilityAttachable:
		return value
	default:
		return shellsession.AttachCapabilityNone
	}
}

func normalizeWorkerSessionIDSource(value shellsession.WorkerSessionIDSource, workerSessionID string) shellsession.WorkerSessionIDSource {
	if strings.TrimSpace(workerSessionID) == "" {
		return shellsession.WorkerSessionIDSourceNone
	}
	switch value {
	case shellsession.WorkerSessionIDSourceAuthoritative, shellsession.WorkerSessionIDSourceHeuristic, shellsession.WorkerSessionIDSourceUnknown:
		return value
	default:
		return shellsession.WorkerSessionIDSourceUnknown
	}
}

func isAttachableRecord(record ShellSessionRecord) bool {
	return strings.TrimSpace(record.WorkerSessionID) != "" &&
		normalizeWorkerSessionIDSource(record.WorkerSessionIDSource, record.WorkerSessionID) == shellsession.WorkerSessionIDSourceAuthoritative &&
		record.AttachCapability == shellsession.AttachCapabilityAttachable
}

func applyTranscriptSummaryToView(view *ShellSessionView, summary shellsession.TranscriptSummary) {
	if view == nil {
		return
	}
	view.TranscriptRetainedChunks = summary.RetainedChunks
	view.TranscriptDroppedChunks = summary.DroppedChunks
	view.TranscriptRetentionLimit = summary.RetentionLimit
	view.TranscriptOldestSequence = summary.OldestSequenceNo
	view.TranscriptNewestSequence = summary.NewestSequenceNo
	view.TranscriptLastChunkAt = summary.LastChunkAt
	view.TranscriptState = deriveTranscriptState(*view)
}

func deriveTranscriptState(view ShellSessionView) shellsession.TranscriptState {
	if view.TranscriptRetainedChunks <= 0 {
		return shellsession.TranscriptStateNone
	}
	isTranscriptOnly := isTranscriptOnlyMode(view.HostMode, view.HostState)
	isPartial := view.TranscriptDroppedChunks > 0
	switch {
	case isTranscriptOnly && isPartial:
		return shellsession.TranscriptStateTranscriptOnlyPartial
	case isTranscriptOnly:
		return shellsession.TranscriptStateTranscriptOnlyAvailable
	case isPartial:
		return shellsession.TranscriptStateBoundedPartial
	default:
		return shellsession.TranscriptStateBoundedAvailable
	}
}

func applyTranscriptReviewToView(view *ShellSessionView, review *shellsession.TranscriptReview, newestForScope int64) {
	if view == nil {
		return
	}
	view.TranscriptReviewID = ""
	view.TranscriptReviewSource = ""
	view.TranscriptReviewedUpTo = 0
	view.TranscriptReviewSummary = ""
	view.TranscriptReviewAt = time.Time{}
	view.TranscriptReviewStale = false
	view.TranscriptReviewNewer = 0
	view.TranscriptReviewClosureState = shellsession.TranscriptReviewClosureNone
	view.TranscriptReviewOldestUnreviewed = 0
	if review == nil {
		return
	}
	view.TranscriptReviewID = review.ReviewID
	view.TranscriptReviewSource = review.SourceFilter
	view.TranscriptReviewedUpTo = review.ReviewedUpToSequence
	view.TranscriptReviewSummary = strings.TrimSpace(review.Summary)
	view.TranscriptReviewAt = review.CreatedAt
	if newestForScope == 0 {
		newestForScope = review.NewestRetainedSequence
	}
	if newestForScope > review.ReviewedUpToSequence {
		view.TranscriptReviewStale = true
		view.TranscriptReviewNewer = int(newestForScope - review.ReviewedUpToSequence)
		view.TranscriptReviewOldestUnreviewed = review.ReviewedUpToSequence + 1
	}
	view.TranscriptReviewClosureState = deriveTranscriptReviewClosureState(review.SourceFilter, view.TranscriptReviewStale)
}

func (c *Coordinator) newestTranscriptSequenceForReviewScope(taskID common.TaskID, sessionID string, review *shellsession.TranscriptReview, fallbackNewest int64) (int64, error) {
	if review == nil {
		return fallbackNewest, nil
	}
	if review.SourceFilter == "" {
		if fallbackNewest > 0 {
			return fallbackNewest, nil
		}
		return review.NewestRetainedSequence, nil
	}
	newest, err := c.latestShellTranscriptSequenceForSource(taskID, sessionID, review.SourceFilter)
	if err != nil {
		return 0, err
	}
	if newest > 0 {
		return newest, nil
	}
	if fallbackNewest > 0 {
		return fallbackNewest, nil
	}
	return review.NewestRetainedSequence, nil
}

func (c *Coordinator) applyTranscriptReviewHistoryToView(taskID common.TaskID, sessionID string, view *ShellSessionView, reviews []shellsession.TranscriptReview) error {
	if view == nil {
		return nil
	}
	view.TranscriptRecentReviews = nil
	if len(reviews) == 0 {
		return nil
	}
	latestBySource := map[shellsession.TranscriptSource]int64{}
	view.TranscriptRecentReviews = make([]ShellTranscriptReviewSummary, 0, len(reviews))
	for _, review := range reviews {
		newest := view.TranscriptNewestSequence
		if review.SourceFilter != "" {
			if cached, ok := latestBySource[review.SourceFilter]; ok {
				newest = cached
			} else {
				sourceNewest, err := c.latestShellTranscriptSequenceForSource(taskID, sessionID, review.SourceFilter)
				if err != nil {
					return err
				}
				latestBySource[review.SourceFilter] = sourceNewest
				if sourceNewest > 0 {
					newest = sourceNewest
				}
			}
		}
		view.TranscriptRecentReviews = append(view.TranscriptRecentReviews, shellTranscriptReviewSummary(review, newest))
	}
	return nil
}
