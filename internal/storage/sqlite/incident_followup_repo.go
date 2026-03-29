package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"tuku/internal/domain/common"
	"tuku/internal/domain/incidenttriage"
)

type incidentFollowUpRepo struct{ q queryable }

func ensureIncidentFollowUpSchema(q queryable) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS continuity_incident_follow_up_receipts (
	receipt_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	anchor_transition_receipt_id TEXT NOT NULL,
	triage_receipt_id TEXT NOT NULL,
	action_kind TEXT NOT NULL,
	created_at TEXT NOT NULL,
	record_json TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_continuity_incident_follow_up_receipts_task_created
	ON continuity_incident_follow_up_receipts(task_id, created_at DESC, receipt_id DESC);
CREATE INDEX IF NOT EXISTS idx_continuity_incident_follow_up_receipts_task_anchor_created
	ON continuity_incident_follow_up_receipts(task_id, anchor_transition_receipt_id, created_at DESC, receipt_id DESC);
CREATE INDEX IF NOT EXISTS idx_continuity_incident_follow_up_receipts_task_action_created
	ON continuity_incident_follow_up_receipts(task_id, action_kind, created_at DESC, receipt_id DESC);
CREATE INDEX IF NOT EXISTS idx_continuity_incident_follow_up_receipts_task_triage_created
	ON continuity_incident_follow_up_receipts(task_id, triage_receipt_id, created_at DESC, receipt_id DESC);
`
	if _, err := q.Exec(ddl); err != nil {
		return fmt.Errorf("ensure continuity incident follow-up receipts table: %w", err)
	}
	return nil
}

func (r *incidentFollowUpRepo) Create(record incidenttriage.FollowUpReceipt) error {
	if err := ensureIncidentFollowUpSchema(r.q); err != nil {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = r.q.Exec(`
INSERT INTO continuity_incident_follow_up_receipts(
	receipt_id, task_id, anchor_transition_receipt_id, triage_receipt_id, action_kind, created_at, record_json
) VALUES(?,?,?,?,?,?,?)
`,
		string(record.ReceiptID),
		string(record.TaskID),
		string(record.AnchorTransitionReceiptID),
		string(record.TriageReceiptID),
		string(record.ActionKind),
		record.CreatedAt.UTC().Format(sqliteTimestampLayout),
		string(raw),
	)
	if err != nil {
		return fmt.Errorf("insert continuity incident follow-up receipt: %w", err)
	}
	return nil
}

func (r *incidentFollowUpRepo) GetByTaskReceipt(taskID common.TaskID, receiptID common.EventID) (incidenttriage.FollowUpReceipt, error) {
	if err := ensureIncidentFollowUpSchema(r.q); err != nil {
		return incidenttriage.FollowUpReceipt{}, err
	}
	row := r.q.QueryRow(`
SELECT record_json
FROM continuity_incident_follow_up_receipts
WHERE task_id = ? AND receipt_id = ?
LIMIT 1
`, string(taskID), string(receiptID))
	return scanIncidentFollowUpReceipt(row)
}

func (r *incidentFollowUpRepo) LatestByTask(taskID common.TaskID) (incidenttriage.FollowUpReceipt, error) {
	if err := ensureIncidentFollowUpSchema(r.q); err != nil {
		return incidenttriage.FollowUpReceipt{}, err
	}
	row := r.q.QueryRow(`
SELECT record_json
FROM continuity_incident_follow_up_receipts
WHERE task_id = ?
ORDER BY created_at DESC, receipt_id DESC
LIMIT 1
`, string(taskID))
	return scanIncidentFollowUpReceipt(row)
}

func (r *incidentFollowUpRepo) LatestByTaskAnchor(taskID common.TaskID, anchorTransitionReceiptID common.EventID) (incidenttriage.FollowUpReceipt, error) {
	if err := ensureIncidentFollowUpSchema(r.q); err != nil {
		return incidenttriage.FollowUpReceipt{}, err
	}
	row := r.q.QueryRow(`
SELECT record_json
FROM continuity_incident_follow_up_receipts
WHERE task_id = ? AND anchor_transition_receipt_id = ?
ORDER BY created_at DESC, receipt_id DESC
LIMIT 1
`, string(taskID), string(anchorTransitionReceiptID))
	return scanIncidentFollowUpReceipt(row)
}

func (r *incidentFollowUpRepo) ListByTask(taskID common.TaskID, limit int) ([]incidenttriage.FollowUpReceipt, error) {
	return r.ListByTaskFiltered(taskID, incidenttriage.FollowUpReceiptListFilter{Limit: limit})
}

func (r *incidentFollowUpRepo) ListByTaskFiltered(taskID common.TaskID, filter incidenttriage.FollowUpReceiptListFilter) ([]incidenttriage.FollowUpReceipt, error) {
	if err := ensureIncidentFollowUpSchema(r.q); err != nil {
		return nil, err
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 10
	}
	query := `
SELECT record_json
FROM continuity_incident_follow_up_receipts
WHERE task_id = ?`
	args := []any{string(taskID)}
	if filter.AnchorTransitionReceiptID != "" {
		query += ` AND anchor_transition_receipt_id = ?`
		args = append(args, string(filter.AnchorTransitionReceiptID))
	}
	if filter.TriageReceiptID != "" {
		query += ` AND triage_receipt_id = ?`
		args = append(args, string(filter.TriageReceiptID))
	}
	if filter.ActionKind != "" {
		query += ` AND action_kind = ?`
		args = append(args, string(filter.ActionKind))
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
		return nil, fmt.Errorf("query continuity incident follow-up receipts: %w", err)
	}
	defer rows.Close()

	out := make([]incidenttriage.FollowUpReceipt, 0, limit)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var record incidenttriage.FollowUpReceipt
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate continuity incident follow-up receipts: %w", err)
	}
	return out, nil
}

func scanIncidentFollowUpReceipt(row *sql.Row) (incidenttriage.FollowUpReceipt, error) {
	var raw string
	if err := row.Scan(&raw); err != nil {
		return incidenttriage.FollowUpReceipt{}, err
	}
	var record incidenttriage.FollowUpReceipt
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return incidenttriage.FollowUpReceipt{}, err
	}
	return record, nil
}
