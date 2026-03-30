package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/transition"
)

type transitionReceiptRepo struct{ q queryable }

func ensureTransitionReceiptSchema(q queryable) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS continuity_transition_receipts (
	receipt_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	transition_kind TEXT NOT NULL,
	created_at TEXT NOT NULL,
	record_json TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_continuity_transition_receipts_task_created
	ON continuity_transition_receipts(task_id, created_at DESC, receipt_id DESC);
CREATE INDEX IF NOT EXISTS idx_continuity_transition_receipts_task_kind_created
	ON continuity_transition_receipts(task_id, transition_kind, created_at DESC, receipt_id DESC);
`
	if _, err := q.Exec(ddl); err != nil {
		return fmt.Errorf("ensure continuity transition receipts table: %w", err)
	}
	return nil
}

func (r *transitionReceiptRepo) Create(record transition.Receipt) error {
	if err := ensureTransitionReceiptSchema(r.q); err != nil {
		return err
	}
	recordJSON, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = r.q.Exec(`
INSERT INTO continuity_transition_receipts(
	receipt_id, task_id, transition_kind, created_at, record_json
) VALUES(?,?,?,?,?)
`,
		string(record.ReceiptID),
		string(record.TaskID),
		string(record.TransitionKind),
		record.CreatedAt.Format(sqliteTimestampLayout),
		string(recordJSON),
	)
	if err != nil {
		return fmt.Errorf("insert continuity transition receipt: %w", err)
	}
	return nil
}

func (r *transitionReceiptRepo) LatestByTask(taskID common.TaskID) (transition.Receipt, error) {
	if err := ensureTransitionReceiptSchema(r.q); err != nil {
		return transition.Receipt{}, err
	}
	row := r.q.QueryRow(`
SELECT record_json
FROM continuity_transition_receipts
WHERE task_id = ?
ORDER BY created_at DESC, receipt_id DESC
LIMIT 1
`, string(taskID))
	return scanTransitionReceipt(row)
}

func (r *transitionReceiptRepo) GetByTaskReceipt(taskID common.TaskID, receiptID common.EventID) (transition.Receipt, error) {
	if err := ensureTransitionReceiptSchema(r.q); err != nil {
		return transition.Receipt{}, err
	}
	row := r.q.QueryRow(`
SELECT record_json
FROM continuity_transition_receipts
WHERE task_id = ? AND receipt_id = ?
LIMIT 1
`, string(taskID), string(receiptID))
	return scanTransitionReceipt(row)
}

func (r *transitionReceiptRepo) ListByTask(taskID common.TaskID, limit int) ([]transition.Receipt, error) {
	return r.ListByTaskFiltered(taskID, transition.ReceiptListFilter{
		Limit: limit,
	})
}

func (r *transitionReceiptRepo) ListByTaskFiltered(taskID common.TaskID, filter transition.ReceiptListFilter) ([]transition.Receipt, error) {
	if err := ensureTransitionReceiptSchema(r.q); err != nil {
		return nil, err
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 10
	}
	query := `
SELECT record_json
FROM continuity_transition_receipts
WHERE task_id = ?`
	args := []any{string(taskID)}
	if filter.TransitionKind != "" {
		query += ` AND transition_kind = ?`
		args = append(args, string(filter.TransitionKind))
	}
	if handoffID := strings.TrimSpace(filter.HandoffID); handoffID != "" {
		query += ` AND json_extract(record_json, '$.handoff_id') = ?`
		args = append(args, handoffID)
	}
	if filter.BeforeReceiptID != "" && !filter.BeforeCreatedAt.IsZero() {
		anchor := filter.BeforeCreatedAt.UTC().Format(sqliteTimestampLayout)
		query += ` AND (created_at < ? OR (created_at = ? AND receipt_id < ?))`
		args = append(args, anchor, anchor, string(filter.BeforeReceiptID))
	}
	query += `
ORDER BY created_at DESC, receipt_id DESC
LIMIT ?`
	args = append(args, limit)

	rows, err := r.q.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query continuity transition receipts: %w", err)
	}
	defer rows.Close()

	out := make([]transition.Receipt, 0, limit)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var record transition.Receipt
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate continuity transition receipts: %w", err)
	}
	return out, nil
}

func (r *transitionReceiptRepo) ListByTaskAfter(taskID common.TaskID, afterReceiptID common.EventID, afterCreatedAt time.Time, limit int) ([]transition.Receipt, error) {
	if err := ensureTransitionReceiptSchema(r.q); err != nil {
		return nil, err
	}
	if afterReceiptID == "" || afterCreatedAt.IsZero() {
		return nil, fmt.Errorf("after receipt id and after created at are required")
	}
	if limit <= 0 {
		limit = 10
	}
	rows, err := r.q.Query(`
SELECT record_json
FROM continuity_transition_receipts
WHERE task_id = ?
  AND (created_at > ? OR (created_at = ? AND receipt_id > ?))
ORDER BY created_at ASC, receipt_id ASC
LIMIT ?
`, string(taskID), afterCreatedAt.UTC().Format(sqliteTimestampLayout), afterCreatedAt.UTC().Format(sqliteTimestampLayout), string(afterReceiptID), limit)
	if err != nil {
		return nil, fmt.Errorf("query continuity transition receipts after anchor: %w", err)
	}
	defer rows.Close()

	out := make([]transition.Receipt, 0, limit)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var record transition.Receipt
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate continuity transition receipts after anchor: %w", err)
	}
	return out, nil
}

func scanTransitionReceipt(row *sql.Row) (transition.Receipt, error) {
	var raw string
	if err := row.Scan(&raw); err != nil {
		return transition.Receipt{}, err
	}
	var record transition.Receipt
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return transition.Receipt{}, err
	}
	return record, nil
}
