package shellsession

import (
	"time"

	"tuku/internal/domain/common"
)

type EventKind string

const (
	EventKindHostStarted       EventKind = "host_started"
	EventKindHostExited        EventKind = "host_exited"
	EventKindFallbackActivated EventKind = "fallback_activated"
	EventKindReattachRequested EventKind = "reattach_requested"
	EventKindReattachFailed    EventKind = "reattach_failed"
	EventKindSessionReported   EventKind = "session_reported"
)

// Event captures durable shell/session lifecycle evidence.
type Event struct {
	EventID               common.EventID
	TaskID                common.TaskID
	SessionID             string
	Kind                  EventKind
	HostMode              string
	HostState             string
	WorkerSessionID       string
	WorkerSessionIDSource WorkerSessionIDSource
	AttachCapability      AttachCapability
	Active                bool
	InputLive             bool
	ExitCode              *int
	PaneWidth             int
	PaneHeight            int
	Note                  string
	CreatedAt             time.Time
}
