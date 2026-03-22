package sqlite

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/shellsession"
)

type shellSessionRepo struct{ q queryable }

func ensureShellSessionSchema(db *sql.DB) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS shell_sessions (
	task_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	worker_preference TEXT NOT NULL,
	resolved_worker TEXT NOT NULL,
	worker_session_id TEXT NOT NULL,
	attach_capability TEXT NOT NULL,
	host_mode TEXT NOT NULL,
	host_state TEXT NOT NULL,
	started_at TEXT NOT NULL,
	last_updated_at TEXT NOT NULL,
	active INTEGER NOT NULL,
	note TEXT NOT NULL,
	PRIMARY KEY (task_id, session_id)
);
CREATE INDEX IF NOT EXISTS idx_shell_sessions_task_updated
	ON shell_sessions(task_id, last_updated_at DESC);
`
	if _, err := db.Exec(ddl); err != nil {
		return fmt.Errorf("apply shell session schema: %w", err)
	}
	type colDef struct {
		Name string
		DDL  string
	}
	needed := []colDef{
		{Name: "worker_session_id", DDL: "ALTER TABLE shell_sessions ADD COLUMN worker_session_id TEXT NOT NULL DEFAULT ''"},
		{Name: "attach_capability", DDL: "ALTER TABLE shell_sessions ADD COLUMN attach_capability TEXT NOT NULL DEFAULT 'none'"},
	}
	for _, item := range needed {
		ok, err := hasColumn(db, "shell_sessions", item.Name)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		if _, err := db.Exec(item.DDL); err != nil {
			return fmt.Errorf("add shell_sessions column %s: %w", item.Name, err)
		}
	}
	return nil
}

func (r *shellSessionRepo) Upsert(record shellsession.Record) error {
	record.WorkerPreference = strings.TrimSpace(record.WorkerPreference)
	record.ResolvedWorker = strings.TrimSpace(record.ResolvedWorker)
	record.WorkerSessionID = strings.TrimSpace(record.WorkerSessionID)
	if record.AttachCapability == "" {
		record.AttachCapability = shellsession.AttachCapabilityNone
	}
	record.HostMode = strings.TrimSpace(record.HostMode)
	record.HostState = strings.TrimSpace(record.HostState)
	record.Note = strings.TrimSpace(record.Note)
	if record.StartedAt.IsZero() {
		record.StartedAt = record.LastUpdatedAt
	}

	_, err := r.q.Exec(`
INSERT INTO shell_sessions(
	task_id, session_id, worker_preference, resolved_worker, worker_session_id, attach_capability, host_mode, host_state,
	started_at, last_updated_at, active, note
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(task_id, session_id) DO UPDATE SET
	worker_preference = CASE
		WHEN excluded.worker_preference <> '' THEN excluded.worker_preference
		ELSE shell_sessions.worker_preference
	END,
	resolved_worker = CASE
		WHEN excluded.resolved_worker <> '' THEN excluded.resolved_worker
		ELSE shell_sessions.resolved_worker
	END,
	worker_session_id = CASE
		WHEN excluded.worker_session_id <> '' THEN excluded.worker_session_id
		ELSE shell_sessions.worker_session_id
	END,
	attach_capability = CASE
		WHEN excluded.attach_capability <> '' THEN excluded.attach_capability
		ELSE shell_sessions.attach_capability
	END,
	host_mode = excluded.host_mode,
	host_state = excluded.host_state,
	last_updated_at = excluded.last_updated_at,
	active = excluded.active,
	note = excluded.note
`,
		string(record.TaskID),
		record.SessionID,
		record.WorkerPreference,
		record.ResolvedWorker,
		record.WorkerSessionID,
		string(record.AttachCapability),
		record.HostMode,
		record.HostState,
		record.StartedAt.Format(sqliteTimestampLayout),
		record.LastUpdatedAt.Format(sqliteTimestampLayout),
		boolToInt(record.Active),
		record.Note,
	)
	if err != nil {
		return fmt.Errorf("upsert shell session: %w", err)
	}
	return nil
}

func (r *shellSessionRepo) ListByTask(taskID common.TaskID) ([]shellsession.Record, error) {
	rows, err := r.q.Query(`
SELECT task_id, session_id, worker_preference, resolved_worker, worker_session_id, attach_capability,
	host_mode, host_state, started_at, last_updated_at, active, note
FROM shell_sessions
WHERE task_id = ?
ORDER BY last_updated_at DESC, session_id ASC
`, string(taskID))
	if err != nil {
		return nil, fmt.Errorf("query shell sessions by task: %w", err)
	}
	defer rows.Close()

	var records []shellsession.Record
	for rows.Next() {
		record, err := scanShellSession(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate shell sessions: %w", err)
	}
	return records, nil
}

func scanShellSession(scanner interface {
	Scan(dest ...any) error
}) (shellsession.Record, error) {
	var (
		taskID           string
		sessionID        string
		workerPreference string
		resolvedWorker   string
		workerSessionID  string
		attachCapability string
		hostMode         string
		hostState        string
		startedAt        string
		lastUpdatedAt    string
		active           int
		note             string
	)
	if err := scanner.Scan(
		&taskID,
		&sessionID,
		&workerPreference,
		&resolvedWorker,
		&workerSessionID,
		&attachCapability,
		&hostMode,
		&hostState,
		&startedAt,
		&lastUpdatedAt,
		&active,
		&note,
	); err != nil {
		return shellsession.Record{}, err
	}

	started, err := time.Parse(sqliteTimestampLayout, startedAt)
	if err != nil {
		return shellsession.Record{}, fmt.Errorf("parse shell session started_at: %w", err)
	}
	updated, err := time.Parse(sqliteTimestampLayout, lastUpdatedAt)
	if err != nil {
		return shellsession.Record{}, fmt.Errorf("parse shell session last_updated_at: %w", err)
	}

	return shellsession.Record{
		TaskID:           common.TaskID(taskID),
		SessionID:        sessionID,
		WorkerPreference: workerPreference,
		ResolvedWorker:   resolvedWorker,
		WorkerSessionID:  workerSessionID,
		AttachCapability: shellsession.AttachCapability(attachCapability),
		HostMode:         hostMode,
		HostState:        hostState,
		StartedAt:        started,
		LastUpdatedAt:    updated,
		Active:           active == 1,
		Note:             note,
	}, nil
}
