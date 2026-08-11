package audit

import (
	"database/sql"
	"fmt"
	"time"
)

// schemaVersion is the current schema revision. Bump when adding migrations.
const schemaVersion = 1

const schemaSQL = `
CREATE TABLE IF NOT EXISTS audit_events (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  time            TEXT    NOT NULL,
  actor           TEXT    NOT NULL,
  actor_kind      TEXT    NOT NULL,
  source          TEXT    NOT NULL,
  action          TEXT    NOT NULL,
  container_id    TEXT    NOT NULL DEFAULT '',
  container_name  TEXT    NOT NULL DEFAULT '',
  result          TEXT    NOT NULL,
  status_code     INTEGER NOT NULL DEFAULT 0,
  duration_ms     INTEGER NOT NULL DEFAULT 0,
  detail          TEXT    NOT NULL DEFAULT '',
  request_id      TEXT    NOT NULL DEFAULT '',
  client_ip       TEXT    NOT NULL DEFAULT '',
  user_agent      TEXT    NOT NULL DEFAULT '',
  payload         TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_audit_time        ON audit_events(time DESC);
CREATE INDEX IF NOT EXISTS idx_audit_action      ON audit_events(action);
CREATE INDEX IF NOT EXISTS idx_audit_container   ON audit_events(container_id);
CREATE INDEX IF NOT EXISTS idx_audit_result      ON audit_events(result);
CREATE INDEX IF NOT EXISTS idx_audit_actor       ON audit_events(actor);
CREATE INDEX IF NOT EXISTS idx_audit_filter      ON audit_events(action, result, container_id, time DESC);
CREATE TABLE IF NOT EXISTS schema_migrations (
  version    INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
);
`

type sqlDB struct{ db *sql.DB }

func openSQLite(path string) (sqlexec, error) {
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL: %w", err)
	}
	return &sqlDB{db: db}, nil
}

func (s *sqlDB) Exec(query string, args ...any) (int64, int64, error) {
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return 0, 0, err
	}
	aff, _ := res.RowsAffected()
	id, _ := res.LastInsertId()
	return aff, id, nil
}

func (s *sqlDB) Query(query string, args ...any) ([][]any, []string, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}
	var out [][]any
	for rows.Next() {
		ptrs := make([]any, len(cols))
		vals := make([]any, len(cols))
		for i := range ptrs {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, nil, err
		}
		// Convert []byte to string for convenience.
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = string(b)
			}
		}
		out = append(out, vals)
	}
	return out, cols, rows.Err()
}

func (s *sqlDB) Close() error { return s.db.Close() }

func (r *sqliteRecorder) migrate() error {
	if _, _, err := r.db.Exec(schemaSQL); err != nil {
		return err
	}
	rows, _, err := r.db.Query(`SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return err
	}
	applied := map[int]bool{}
	for _, row := range rows {
		if len(row) > 0 {
			if v, ok := row[0].(int64); ok {
				applied[int(v)] = true
			}
		}
	}
	for v := 1; v <= schemaVersion; v++ {
		if applied[v] {
			continue
		}
		if _, _, err := r.db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`,
			v, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return nil
}
