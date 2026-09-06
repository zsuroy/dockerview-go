package duty

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Ticket is one duty-agent inquiry: the question, a summary of tool calls,
// the conclusion, and whether a write was confirmed.
type Ticket struct {
	ID               int64       `json:"id"`
	Time             time.Time   `json:"time"`
	Actor            string      `json:"actor"`
	ActorKind        string      `json:"actor_kind"`
	Source           string      `json:"source"`
	Question         string      `json:"question"`
	ToolSummary      []ToolTrace `json:"tool_summary"`
	Conclusion       string      `json:"conclusion"`
	WriteConfirmed   bool        `json:"write_confirmed"`
	WriteAction      string      `json:"write_action,omitempty"`
	RelatedContainer string      `json:"related_container,omitempty"`
	RequestID        string      `json:"request_id,omitempty"`
}

// ToolTrace is one tool call recorded on a ticket.
type ToolTrace struct {
	Tool          string `json:"tool"`
	Input         string `json:"input"`
	OutputExcerpt string `json:"output_excerpt"`
	Evidence      string `json:"evidence,omitempty"`
}

// Store persists duty tickets in SQLite. It opens its own connection to the
// same database file as the audit recorder; WAL mode allows concurrent
// readers and one writer across connections.
type Store struct {
	db *sql.DB
}

// OpenStore opens (creating the schema if needed) the duty_tickets table in
// the given SQLite database path. Use ":memory:" for tests.
func OpenStore(path string) (*Store, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, fmt.Errorf("duty: mkdir db dir: %w", err)
		}
	}
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("duty: open db: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("duty: migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Insert persists a ticket and assigns its ID.
func (s *Store) Insert(ctx context.Context, t *Ticket) error {
	if t.Time.IsZero() {
		t.Time = time.Now().UTC()
	}
	summary, _ := json.Marshal(t.ToolSummary)
	res, err := s.db.ExecContext(ctx, `INSERT INTO duty_tickets
		(time, actor, actor_kind, source, question, tool_summary, conclusion,
		 write_confirmed, write_action, related_container, request_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Time.UTC().Format(time.RFC3339Nano), t.Actor, t.ActorKind, t.Source,
		t.Question, string(summary), t.Conclusion,
		t.WriteConfirmed, t.WriteAction, t.RelatedContainer, t.RequestID)
	if err != nil {
		return fmt.Errorf("duty: insert ticket: %w", err)
	}
	t.ID, _ = res.LastInsertId()
	return nil
}

// ConfirmWrite marks a ticket as having a confirmed write operation.
func (s *Store) ConfirmWrite(ctx context.Context, id int64, action, container, requestID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE duty_tickets SET write_confirmed=1, write_action=?, related_container=?, request_id=? WHERE id=?`,
		action, container, requestID, id)
	if err != nil {
		return fmt.Errorf("duty: confirm write: %w", err)
	}
	return nil
}

// TicketFilter narrows a List query.
type TicketFilter struct {
	Container string
	Limit     int
	Offset    int
}

// List returns tickets, newest first.
func (s *Store) List(ctx context.Context, f TicketFilter) ([]Ticket, int64, error) {
	where := ""
	var args []any
	if f.Container != "" {
		where = " WHERE related_container LIKE ? OR question LIKE ?"
		q := "%" + f.Container + "%"
		args = append(args, q, q)
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM duty_tickets`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("duty: count: %w", err)
	}
	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, time, actor, actor_kind, source, question, tool_summary,
		conclusion, write_confirmed, write_action, related_container, request_id
		FROM duty_tickets`+where+` ORDER BY time DESC, id DESC LIMIT ? OFFSET ?`,
		append(args, limit, f.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("duty: list: %w", err)
	}
	defer rows.Close()
	var out []Ticket
	for rows.Next() {
		var t Ticket
		var ts string
		var writeConfirmed int
		var timeStr string
		if err := rows.Scan(&t.ID, &timeStr, &t.Actor, &t.ActorKind, &t.Source,
			&t.Question, &ts, &t.Conclusion, &writeConfirmed, &t.WriteAction,
			&t.RelatedContainer, &t.RequestID); err != nil {
			return nil, 0, err
		}
		t.Time, _ = time.Parse(time.RFC3339Nano, timeStr)
		t.WriteConfirmed = writeConfirmed != 0
		_ = json.Unmarshal([]byte(ts), &t.ToolSummary)
		out = append(out, t)
	}
	return out, total, rows.Err()
}

// Get returns a single ticket by ID.
func (s *Store) Get(ctx context.Context, id int64) (*Ticket, error) {
	var t Ticket
	var ts, timeStr string
	var writeConfirmed int
	err := s.db.QueryRowContext(ctx, `SELECT id, time, actor, actor_kind, source, question, tool_summary,
		conclusion, write_confirmed, write_action, related_container, request_id
		FROM duty_tickets WHERE id=?`, id).Scan(
		&t.ID, &timeStr, &t.Actor, &t.ActorKind, &t.Source,
		&t.Question, &ts, &t.Conclusion, &writeConfirmed, &t.WriteAction,
		&t.RelatedContainer, &t.RequestID)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("ticket %d not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("duty: get: %w", err)
	}
	t.Time, _ = time.Parse(time.RFC3339Nano, timeStr)
	t.WriteConfirmed = writeConfirmed != 0
	_ = json.Unmarshal([]byte(ts), &t.ToolSummary)
	return &t, nil
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS duty_tickets (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  time              TEXT    NOT NULL,
  actor             TEXT    NOT NULL DEFAULT '',
  actor_kind        TEXT    NOT NULL DEFAULT '',
  source            TEXT    NOT NULL DEFAULT '',
  question          TEXT    NOT NULL,
  tool_summary      TEXT    NOT NULL DEFAULT '',
  conclusion        TEXT    NOT NULL DEFAULT '',
  write_confirmed   INTEGER NOT NULL DEFAULT 0,
  write_action      TEXT    NOT NULL DEFAULT '',
  related_container TEXT    NOT NULL DEFAULT '',
  request_id        TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_duty_time ON duty_tickets(time DESC);
`
