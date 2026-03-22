package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/recoveryaction"
)

type recoveryActionRepo struct{ q queryable }

func ensureRecoveryActionSchema(q queryable) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS recovery_actions (
	action_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	kind TEXT NOT NULL,
	run_id TEXT NOT NULL,
	checkpoint_id TEXT NOT NULL,
	handoff_id TEXT NOT NULL,
	launch_attempt_id TEXT NOT NULL,
	summary TEXT NOT NULL,
	notes_json TEXT NOT NULL,
	created_at TEXT NOT NULL,
	record_json TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_recovery_actions_task_created
	ON recovery_actions(task_id, created_at DESC, action_id DESC);
`
	if _, err := q.Exec(ddl); err != nil {
		return fmt.Errorf("ensure recovery actions table: %w", err)
	}
	return nil
}

func (r *recoveryActionRepo) Create(record recoveryaction.Record) error {
	if err := ensureRecoveryActionSchema(r.q); err != nil {
		return err
	}
	notesJSON, err := marshalStringSlice(record.Notes)
	if err != nil {
		return err
	}
	recordJSON, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = r.q.Exec(`
INSERT INTO recovery_actions(
	action_id, task_id, kind, run_id, checkpoint_id, handoff_id, launch_attempt_id,
	summary, notes_json, created_at, record_json
) VALUES(?,?,?,?,?,?,?,?,?,?,?)
`,
		record.ActionID,
		string(record.TaskID),
		string(record.Kind),
		string(record.RunID),
		string(record.CheckpointID),
		record.HandoffID,
		record.LaunchAttemptID,
		record.Summary,
		notesJSON,
		record.CreatedAt.Format(sqliteTimestampLayout),
		string(recordJSON),
	)
	if err != nil {
		return fmt.Errorf("insert recovery action: %w", err)
	}
	return nil
}

func (r *recoveryActionRepo) LatestByTask(taskID common.TaskID) (recoveryaction.Record, error) {
	if err := ensureRecoveryActionSchema(r.q); err != nil {
		return recoveryaction.Record{}, err
	}
	row := r.q.QueryRow(`
SELECT record_json
FROM recovery_actions
WHERE task_id = ?
ORDER BY created_at DESC, action_id DESC
LIMIT 1
`, string(taskID))
	return scanRecoveryAction(row)
}

func (r *recoveryActionRepo) ListByTask(taskID common.TaskID, limit int) ([]recoveryaction.Record, error) {
	if err := ensureRecoveryActionSchema(r.q); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.q.Query(`
SELECT record_json
FROM recovery_actions
WHERE task_id = ?
ORDER BY created_at DESC, action_id DESC
LIMIT ?
`, string(taskID), limit)
	if err != nil {
		return nil, fmt.Errorf("query recovery actions: %w", err)
	}
	defer rows.Close()

	var out []recoveryaction.Record
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var record recoveryaction.Record
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recovery actions: %w", err)
	}
	return out, nil
}

func scanRecoveryAction(row *sql.Row) (recoveryaction.Record, error) {
	var raw string
	if err := row.Scan(&raw); err != nil {
		return recoveryaction.Record{}, err
	}
	var record recoveryaction.Record
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return recoveryaction.Record{}, err
	}
	if record.CreatedAt.IsZero() {
		// Backstop older records if schema is reused in tests.
		record.CreatedAt = time.Now().UTC()
	}
	return record, nil
}
