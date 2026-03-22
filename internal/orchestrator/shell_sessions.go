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

type ShellSessionClass string

const (
	ShellSessionClassAttachable         ShellSessionClass = "attachable"
	ShellSessionClassActiveUnattachable ShellSessionClass = "active_unattachable"
	ShellSessionClassStale              ShellSessionClass = "stale"
	ShellSessionClassEnded              ShellSessionClass = "ended"
)

type ShellSessionRecord = shellsession.Record

type ShellSessionView struct {
	TaskID           common.TaskID
	SessionID        string
	WorkerPreference string
	ResolvedWorker   string
	WorkerSessionID  string
	AttachCapability shellsession.AttachCapability
	HostMode         string
	HostState        string
	StartedAt        time.Time
	LastUpdatedAt    time.Time
	Active           bool
	Note             string
	SessionClass     ShellSessionClass
}

type ShellSessionRegistry interface {
	Upsert(record ShellSessionRecord) error
	ListByTask(taskID common.TaskID) ([]ShellSessionRecord, error)
}

type MemoryShellSessionRegistry struct {
	mu     sync.Mutex
	byTask map[common.TaskID]map[string]ShellSessionRecord
}

func NewMemoryShellSessionRegistry() *MemoryShellSessionRegistry {
	return &MemoryShellSessionRegistry{byTask: make(map[common.TaskID]map[string]ShellSessionRecord)}
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

type ReportShellSessionRequest struct {
	TaskID           string
	SessionID        string
	WorkerPreference string
	ResolvedWorker   string
	WorkerSessionID  string
	AttachCapability shellsession.AttachCapability
	HostMode         string
	HostState        string
	StartedAt        time.Time
	Active           bool
	Note             string
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
		TaskID:           record.TaskID,
		SessionID:        record.SessionID,
		WorkerPreference: record.WorkerPreference,
		ResolvedWorker:   record.ResolvedWorker,
		WorkerSessionID:  record.WorkerSessionID,
		AttachCapability: record.AttachCapability,
		HostMode:         record.HostMode,
		HostState:        record.HostState,
		StartedAt:        record.StartedAt,
		LastUpdatedAt:    record.LastUpdatedAt,
		Active:           record.Active,
		Note:             record.Note,
		SessionClass:     ShellSessionClassActiveUnattachable,
	}
	if !record.Active {
		view.SessionClass = ShellSessionClassEnded
		return view
	}
	if staleAfter > 0 && !record.LastUpdatedAt.IsZero() && now.Sub(record.LastUpdatedAt) > staleAfter {
		view.SessionClass = ShellSessionClassStale
		return view
	}
	if isAttachableRecord(record) {
		view.SessionClass = ShellSessionClassAttachable
	}
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
		TaskID:           taskID,
		SessionID:        sessionID,
		WorkerPreference: strings.TrimSpace(req.WorkerPreference),
		ResolvedWorker:   strings.TrimSpace(req.ResolvedWorker),
		WorkerSessionID:  strings.TrimSpace(req.WorkerSessionID),
		AttachCapability: normalizeAttachCapability(req.AttachCapability),
		HostMode:         strings.TrimSpace(req.HostMode),
		HostState:        strings.TrimSpace(req.HostState),
		StartedAt:        req.StartedAt.UTC(),
		LastUpdatedAt:    now,
		Active:           req.Active,
		Note:             strings.TrimSpace(req.Note),
	}
	if err := c.shellSessions.Upsert(record); err != nil {
		return ReportShellSessionResult{}, err
	}
	persisted, err := c.loadShellSessionRecord(taskID, sessionID)
	if err != nil {
		return ReportShellSessionResult{}, err
	}
	return ReportShellSessionResult{
		TaskID:  taskID,
		Session: classifyShellSession(persisted, now, c.shellSessionStaleAfter),
	}, nil
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
	return ListShellSessionsResult{
		TaskID:   id,
		Sessions: classifyShellSessions(records, c.clock(), c.shellSessionStaleAfter),
	}, nil
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

func isAttachableRecord(record ShellSessionRecord) bool {
	return strings.TrimSpace(record.WorkerSessionID) != "" && record.AttachCapability == shellsession.AttachCapabilityAttachable
}
