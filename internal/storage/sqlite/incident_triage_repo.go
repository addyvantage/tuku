package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"tuku/internal/domain/common"
	"tuku/internal/domain/incidenttriage"
)

type incidentTriageRepo struct{ q queryable }

func ensureIncidentTriageSchema(q queryable) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS continuity_incident_triage_receipts (
	receipt_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	anchor_transition_receipt_id TEXT NOT NULL,
	posture TEXT NOT NULL,
	created_at TEXT NOT NULL,
	record_json TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_continuity_incident_triage_receipts_task_created
	ON continuity_incident_triage_receipts(task_id, created_at DESC, receipt_id DESC);
CREATE INDEX IF NOT EXISTS idx_continuity_incident_triage_receipts_task_anchor_created
	ON continuity_incident_triage_receipts(task_id, anchor_transition_receipt_id, created_at DESC, receipt_id DESC);
CREATE INDEX IF NOT EXISTS idx_continuity_incident_triage_receipts_task_posture_created
	ON continuity_incident_triage_receipts(task_id, posture, created_at DESC, receipt_id DESC);
`
	if _, err := q.Exec(ddl); err != nil {
		return fmt.Errorf("ensure continuity incident triage receipts table: %w", err)
	}
	return nil
}

func (r *incidentTriageRepo) Create(record incidenttriage.Receipt) error {
	if err := ensureIncidentTriageSchema(r.q); err != nil {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = r.q.Exec(`
INSERT INTO continuity_incident_triage_receipts(
	receipt_id, task_id, anchor_transition_receipt_id, posture, created_at, record_json
) VALUES(?,?,?,?,?,?)
`,
		string(record.ReceiptID),
		string(record.TaskID),
		string(record.AnchorTransitionReceiptID),
		string(record.Posture),
		record.CreatedAt.UTC().Format(sqliteTimestampLayout),
		string(raw),
	)
	if err != nil {
		return fmt.Errorf("insert continuity incident triage receipt: %w", err)
	}
	return nil
}

func (r *incidentTriageRepo) LatestByTask(taskID common.TaskID) (incidenttriage.Receipt, error) {
	if err := ensureIncidentTriageSchema(r.q); err != nil {
		return incidenttriage.Receipt{}, err
	}
	row := r.q.QueryRow(`
SELECT record_json
FROM continuity_incident_triage_receipts
WHERE task_id = ?
ORDER BY created_at DESC, receipt_id DESC
LIMIT 1
`, string(taskID))
	return scanIncidentTriageReceipt(row)
}

func (r *incidentTriageRepo) GetByTaskReceipt(taskID common.TaskID, receiptID common.EventID) (incidenttriage.Receipt, error) {
	if err := ensureIncidentTriageSchema(r.q); err != nil {
		return incidenttriage.Receipt{}, err
	}
	row := r.q.QueryRow(`
SELECT record_json
FROM continuity_incident_triage_receipts
WHERE task_id = ? AND receipt_id = ?
LIMIT 1
`, string(taskID), string(receiptID))
	return scanIncidentTriageReceipt(row)
}

func (r *incidentTriageRepo) LatestByTaskAnchor(taskID common.TaskID, anchorTransitionReceiptID common.EventID) (incidenttriage.Receipt, error) {
	if err := ensureIncidentTriageSchema(r.q); err != nil {
		return incidenttriage.Receipt{}, err
	}
	row := r.q.QueryRow(`
SELECT record_json
FROM continuity_incident_triage_receipts
WHERE task_id = ? AND anchor_transition_receipt_id = ?
ORDER BY created_at DESC, receipt_id DESC
LIMIT 1
`, string(taskID), string(anchorTransitionReceiptID))
	return scanIncidentTriageReceipt(row)
}

func (r *incidentTriageRepo) ListByTask(taskID common.TaskID, limit int) ([]incidenttriage.Receipt, error) {
	return r.ListByTaskFiltered(taskID, incidenttriage.ReceiptListFilter{
		Limit: limit,
	})
}

func (r *incidentTriageRepo) ListByTaskFiltered(taskID common.TaskID, filter incidenttriage.ReceiptListFilter) ([]incidenttriage.Receipt, error) {
	if err := ensureIncidentTriageSchema(r.q); err != nil {
		return nil, err
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 10
	}
	query := `
SELECT record_json
FROM continuity_incident_triage_receipts
WHERE task_id = ?`
	args := []any{string(taskID)}
	if filter.AnchorTransitionReceiptID != "" {
		query += ` AND anchor_transition_receipt_id = ?`
		args = append(args, string(filter.AnchorTransitionReceiptID))
	}
	if filter.Posture != "" {
		query += ` AND posture = ?`
		args = append(args, string(filter.Posture))
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
		return nil, fmt.Errorf("query continuity incident triage receipts: %w", err)
	}
	defer rows.Close()

	out := make([]incidenttriage.Receipt, 0, limit)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var record incidenttriage.Receipt
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate continuity incident triage receipts: %w", err)
	}
	return out, nil
}

func scanIncidentTriageReceipt(row *sql.Row) (incidenttriage.Receipt, error) {
	var raw string
	if err := row.Scan(&raw); err != nil {
		return incidenttriage.Receipt{}, err
	}
	var record incidenttriage.Receipt
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return incidenttriage.Receipt{}, err
	}
	return record, nil
}
