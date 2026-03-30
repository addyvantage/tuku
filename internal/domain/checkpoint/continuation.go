package checkpoint

import (
	"time"

	"tuku/internal/domain/common"
)

// ContinuationRecord is v1.5-ready scaffolding for cross-worker handoff continuity.
type ContinuationRecord struct {
	ContinuationID    string              `json:"continuation_id"`
	TaskID            common.TaskID       `json:"task_id"`
	FromWorker        string              `json:"from_worker"`
	ToWorker          string              `json:"to_worker"`
	HandoffReason     string              `json:"handoff_reason"`
	SourceCheckpointID common.CheckpointID `json:"source_checkpoint_id"`
	HandoffPackRef    string              `json:"handoff_pack_ref"`
	Status            string              `json:"status"`
	CreatedAt         time.Time           `json:"created_at"`
}
