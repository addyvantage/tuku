package shellsession

import (
	"time"

	"tuku/internal/domain/common"
)

type AttachCapability string

const (
	AttachCapabilityNone       AttachCapability = "none"
	AttachCapabilityAttachable AttachCapability = "attachable"
)

// Record captures compact daemon-owned shell session metadata only.
type Record struct {
	TaskID           common.TaskID
	SessionID        string
	WorkerPreference string
	ResolvedWorker   string
	WorkerSessionID  string
	AttachCapability AttachCapability
	HostMode         string
	HostState        string
	StartedAt        time.Time
	LastUpdatedAt    time.Time
	Active           bool
	Note             string
}
