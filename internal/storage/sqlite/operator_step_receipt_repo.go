package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"tuku/internal/domain/common"
	"tuku/internal/domain/operatorstep"
)

type operatorStepReceiptRepo struct{ q queryable }

func ensureOperatorStepReceiptSchema(q queryable) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS operator_step_receipts (
	receipt_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	action_handle TEXT NOT NULL,
	result_class TEXT NOT NULL,
	created_at TEXT NOT NULL,
	record_json TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_operator_step_receipts_task_created
	ON operator_step_receipts(task_id, created_at DESC, receipt_id DESC);
`
	if _, err := q.Exec(ddl); err != nil {
		return fmt.Errorf("ensure operator step receipts table: %w", err)
	}
	return nil
}

func (r *operatorStepReceiptRepo) Create(record operatorstep.Receipt) error {
	if err := ensureOperatorStepReceiptSchema(r.q); err != nil {
		return err
	}
	recordJSON, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = r.q.Exec(`
INSERT INTO operator_step_receipts(
	receipt_id, task_id, action_handle, result_class, created_at, record_json
) VALUES(?,?,?,?,?,?)
`,
		record.ReceiptID,
		string(record.TaskID),
		record.ActionHandle,
		string(record.ResultClass),
		record.CreatedAt.Format(sqliteTimestampLayout),
		string(recordJSON),
	)
	if err != nil {
		return fmt.Errorf("insert operator step receipt: %w", err)
	}
	return nil
}

func (r *operatorStepReceiptRepo) LatestByTask(taskID common.TaskID) (operatorstep.Receipt, error) {
	if err := ensureOperatorStepReceiptSchema(r.q); err != nil {
		return operatorstep.Receipt{}, err
	}
	row := r.q.QueryRow(`
SELECT record_json
FROM operator_step_receipts
WHERE task_id = ?
ORDER BY created_at DESC, receipt_id DESC
LIMIT 1
`, string(taskID))
	return scanOperatorStepReceipt(row)
}

func (r *operatorStepReceiptRepo) ListByTask(taskID common.TaskID, limit int) ([]operatorstep.Receipt, error) {
	if err := ensureOperatorStepReceiptSchema(r.q); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 10
	}
	rows, err := r.q.Query(`
SELECT record_json
FROM operator_step_receipts
WHERE task_id = ?
ORDER BY created_at DESC, receipt_id DESC
LIMIT ?
`, string(taskID), limit)
	if err != nil {
		return nil, fmt.Errorf("query operator step receipts: %w", err)
	}
	defer rows.Close()

	out := make([]operatorstep.Receipt, 0, limit)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var record operatorstep.Receipt
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate operator step receipts: %w", err)
	}
	return out, nil
}

func scanOperatorStepReceipt(row *sql.Row) (operatorstep.Receipt, error) {
	var raw string
	if err := row.Scan(&raw); err != nil {
		return operatorstep.Receipt{}, err
	}
	var record operatorstep.Receipt
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return operatorstep.Receipt{}, err
	}
	return record, nil
}
