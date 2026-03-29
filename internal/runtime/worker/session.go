package worker

import "tuku/internal/domain/common"

type Session struct {
	RunID  common.RunID  `json:"run_id"`
	TaskID common.TaskID `json:"task_id"`
	Worker string        `json:"worker"`
	Status string        `json:"status"`
}
