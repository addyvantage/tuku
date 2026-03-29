package orchestrator

import (
	"strings"

	"tuku/internal/domain/common"
	"tuku/internal/domain/shellsession"
)

type shellSessionEventStore interface {
	AppendEvent(event shellsession.Event) error
	ListEvents(taskID common.TaskID, sessionID string, limit int) ([]shellsession.Event, error)
}

func (c *Coordinator) appendShellSessionEvent(event shellsession.Event) error {
	recorder, ok := c.shellSessions.(shellSessionEventStore)
	if !ok {
		return nil
	}
	if event.EventID == "" {
		event.EventID = common.EventID(c.idGenerator("sev"))
	}
	event.SessionID = strings.TrimSpace(event.SessionID)
	event.HostMode = strings.TrimSpace(event.HostMode)
	event.HostState = strings.TrimSpace(event.HostState)
	event.WorkerSessionID = strings.TrimSpace(event.WorkerSessionID)
	event.WorkerSessionIDSource = normalizeWorkerSessionIDSource(event.WorkerSessionIDSource, event.WorkerSessionID)
	event.Note = strings.TrimSpace(event.Note)
	if event.AttachCapability == "" {
		event.AttachCapability = shellsession.AttachCapabilityNone
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = c.clock()
	}
	return recorder.AppendEvent(event)
}

func (c *Coordinator) listShellSessionEvents(taskID common.TaskID, sessionID string, limit int) ([]shellsession.Event, error) {
	recorder, ok := c.shellSessions.(shellSessionEventStore)
	if !ok {
		return nil, nil
	}
	return recorder.ListEvents(taskID, strings.TrimSpace(sessionID), limit)
}
