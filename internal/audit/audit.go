// Package audit provides an operation audit recorder for dockerview-go.
//
// Write operations (start/stop/restart/exec) are persisted to SQLite (WAL mode)
// with structured metadata. The recorder is best-effort: a failure to persist
// must not block the underlying Docker operation.
package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// Action values recorded by the audit log.
const (
	ActionStart   = "start"
	ActionStop    = "stop"
	ActionRestart = "restart"
	ActionExec    = "exec"
	ActionUpgrade = "upgrade" // reserved, not emitted in v1
	ActionOp      = "op"      // coarse action for auth failures where op is unknown
	ActionAudit   = "audit"   // reads/exports of the audit endpoint itself
	ActionLogs    = "logs"    // reserved for future log-fetch auditing

	ActionBackupPreview  = "backup_preview"
	ActionBackupCreate   = "backup_create"
	ActionBackupList     = "backup_list"
	ActionBackupDownload = "backup_download"
	ActionBackupDelete   = "backup_delete"
)

// Result values.
const (
	ResultSuccess = "success"
	ResultFailure = "failure"
	ResultDenied  = "denied"
)

// Source values classifying the calling client.
const (
	SourceWeb     = "web"
	SourceMobile  = "mobile"
	SourceAPI     = "api"
	SourceCLI     = "cli"
	SourceUnknown = "unknown"
)

// ActorKind categorises the actor identity.
const (
	ActorKindToken = "token"
	ActorKindAnon  = "anonymous"
	ActorKindCLI   = "cli"
)

// Capping limits (bytes / chars).
const (
	MaxDetailChars    = 512
	MaxPayloadBytes   = 4096
	MaxUserAgentChars = 256
	MaxCmdChars       = 512
	ExportCap         = 10000
)

// Event is a single audit row to be persisted.
type Event struct {
	Time          time.Time
	Actor         string
	ActorKind     string
	Source        string
	Action        string
	ContainerID   string
	ContainerName string
	Result        string
	StatusCode    int
	DurationMs    int64
	Detail        string
	RequestID     string
	ClientIP      string
	UserAgent     string
	Payload       map[string]any
}

// Query is the filter+pagination input for List.
type Query struct {
	Since         time.Time
	Until         time.Time
	ContainerID   string
	ContainerName string
	Actions       []string
	Results       []string
	Actor         string
	Limit         int
	Offset        int
	SortTimeDesc  bool
}

// Page is a paginated list response.
type Page struct {
	Total   int64          `json:"total"`
	Count   int            `json:"count"`
	Offset  int            `json:"offset"`
	Limit   int            `json:"limit"`
	Filters map[string]any `json:"filters"`
	Items   []Item         `json:"items"`
}

// Item is the JSON-friendly representation of an Event.
type Item struct {
	ID            int64          `json:"id"`
	Time          string         `json:"time"`
	Actor         string         `json:"actor"`
	ActorKind     string         `json:"actor_kind"`
	Source        string         `json:"source"`
	Action        string         `json:"action"`
	ContainerID   string         `json:"container_id"`
	ContainerName string         `json:"container_name"`
	Result        string         `json:"result"`
	StatusCode    int            `json:"status_code"`
	DurationMs    int64          `json:"duration_ms"`
	Detail        string         `json:"detail"`
	RequestID     string         `json:"request_id"`
	ClientIP      string         `json:"client_ip"`
	UserAgent     string         `json:"user_agent"`
	Payload       map[string]any `json:"payload"`
}

// Stats are summary counters for the dashboard tile.
type Stats struct {
	Total         int64  `json:"total"`
	Last24h       int64  `json:"last_24h"`
	Failures24h   int64  `json:"failures_24h"`
	Denied24h     int64  `json:"denied_24h"`
	RetentionDays int    `json:"retention_days"`
	DropCount     int64  `json:"drop_count"`
	DBPath        string `json:"db_path"`
}

// Recorder is the interface used by the HTTP server and TUI to log events.
type Recorder interface {
	Record(ctx context.Context, e Event)
	List(ctx context.Context, q Query) (Page, error)
	Export(ctx context.Context, q Query, format string) ([]byte, string, error)
	Stats(ctx context.Context) (Stats, error)
	Close() error
}

// Config controls recorder behaviour.
type Config struct {
	DBPath        string
	RetentionDays int
}

// DefaultConfig returns the defaults (path=data/dockerview.db, retention=90).
func DefaultConfig() Config {
	return Config{
		DBPath:        filepath.Join("data", "dockerview.db"),
		RetentionDays: 90,
	}
}

// Open opens a SQLite-backed recorder, creating/migrating the schema as
// required. A best-effort failure here returns a noop recorder (with an
// associated error logged) so callers can keep serving Docker ops.
func Open(cfg Config) (Recorder, error) {
	if cfg.DBPath == "" {
		return &noopRecorder{cfg: cfg}, nil
	}
	if cfg.DBPath != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o750); err != nil {
			return nil, fmt.Errorf("audit: mkdir db dir: %w", err)
		}
	}
	r := &sqliteRecorder{cfg: cfg, dropCount: new(atomic.Int64), retentionDays: cfg.RetentionDays}
	if cfg.RetentionDays > 0 {
		r.stopCh = make(chan struct{})
	}
	var err error
	r.db, err = openSQLite(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("audit: open db: %w", err)
	}
	if err := r.migrate(); err != nil {
		r.db.Close()
		return nil, fmt.Errorf("audit: migrate: %w", err)
	}
	if cfg.DBPath != ":memory:" {
		// Best-effort 0600 on the created file.
		_ = os.Chmod(cfg.DBPath, 0o600)
	}
	// busy_timeout is already set via the DSN pragma; re-apply defensively so
	// a transient lock during prune/INSERT doesn't immediately fail.
	if _, _, err := r.db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		log.Printf("audit: set busy_timeout: %v", err)
	}
	if cfg.RetentionDays > 0 {
		go r.pruneLoop()
	}
	return r, nil
}

// NewNoop returns a recorder that discards everything. Used when audit is
// disabled or storage fails to initialize.
func NewNoop(cfg Config) Recorder { return &noopRecorder{cfg: cfg} }

// ---- actor helpers ----

// HashActor returns the truncated SHA-256 pseudonym used for a token.
func HashActor(token string) string {
	sum := sha256.Sum256([]byte("dockerview-audit-v1:" + token))
	return "tok_" + hex.EncodeToString(sum[:6])
}

// DeriveSource returns the source category from a User-Agent string.
func DeriveSource(ua string) string {
	s := strings.ToLower(ua)
	switch {
	case strings.Contains(s, "dockerviewmobile") || strings.Contains(s, "expo"):
		return SourceMobile
	case strings.Contains(s, "mozilla/"):
		return SourceWeb
	case strings.Contains(s, "curl/") || strings.Contains(s, "wget/") || strings.Contains(s, "go-http-client"):
		return SourceAPI
	case ua == "":
		return SourceUnknown
	default:
		return SourceAPI
	}
}

// ClientIP extracts the client IP from the request, honouring a single
// X-Forwarded-For hop if the header is present.
func ClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// first hop only
		if i := strings.Index(xff, ","); i >= 0 {
			xff = strings.TrimSpace(xff[:i])
		}
		if xff != "" {
			return xff
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ActorFromRequest resolves the actor metadata from a request. tokenMatched
// is the already-validated token (or "" when anonymous).
func ActorFromRequest(r *http.Request, tokenMatched string) (actor, kind, source, ip, ua string) {
	ua = r.Header.Get("User-Agent")
	if len(ua) > MaxUserAgentChars {
		ua = ua[:MaxUserAgentChars]
	}
	source = DeriveSource(ua)
	ip = ClientIP(r)
	switch {
	case tokenMatched == "":
		kind = ActorKindAnon
		actor = "anonymous"
	default:
		kind = ActorKindToken
		actor = HashActor(tokenMatched)
	}
	return
}

// CLIEvent builds an Event originating from the local TUI.
func CLIEvent(action, containerID, containerName, result, detail string, payload map[string]any) Event {
	return Event{
		Time:          time.Now().UTC(),
		Actor:         "cli",
		ActorKind:     ActorKindCLI,
		Source:        SourceCLI,
		Action:        action,
		ContainerID:   containerID,
		ContainerName: containerName,
		Result:        result,
		RequestID:     uuid.NewString(),
		Payload:       payload,
		Detail:        truncate(detail, MaxDetailChars),
	}
}

// ---- truncation helpers ----

func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n]
}

func marshalPayload(p map[string]any) string {
	if p == nil {
		return ""
	}
	// Pre-trim large string values so the resulting JSON is always valid.
	trimmed := map[string]any{}
	for k, v := range p {
		kl := strings.ToLower(k)
		if strings.Contains(kl, "stdout") || strings.Contains(kl, "stderr") || strings.Contains(kl, "output") {
			trimmed[k] = "[omitted: large output]"
			continue
		}
		if s, ok := v.(string); ok && len(s) > 1024 {
			trimmed[k] = s[:1024] + "…"
			continue
		}
		trimmed[k] = v
	}
	b, err := json.Marshal(trimmed)
	if err != nil {
		return ""
	}
	if len(b) > MaxPayloadBytes {
		// Hard cap: drop everything except a small marker.
		fallback := map[string]any{"_truncated": true}
		b2, _ := json.Marshal(fallback)
		return string(b2)
	}
	return string(b)
}

// normalizeContainerID strips the "docker://" / "sha256:" prefixes that Docker
// sometimes prepends, so 12-char short-id prefix searches match consistently.
func normalizeContainerID(id string) string {
	id = strings.TrimPrefix(id, "docker://")
	id = strings.TrimPrefix(id, "sha256:")
	return id
}

// ---- noop recorder ----

type noopRecorder struct{ cfg Config }

func (n *noopRecorder) Record(context.Context, Event) {}
func (n *noopRecorder) List(_ context.Context, q Query) (Page, error) {
	return Page{Items: []Item{}, Filters: filtersMap(q)}, nil
}
func (n *noopRecorder) Export(_ context.Context, _ Query, format string) ([]byte, string, error) {
	if format == "md" {
		return []byte("# Audit export\n\nAudit storage is disabled.\n"), "audit-disabled.md", nil
	}
	return []byte("[]"), "audit-disabled.json", nil
}
func (n *noopRecorder) Stats(_ context.Context) (Stats, error) {
	return Stats{RetentionDays: n.cfg.RetentionDays, DBPath: n.cfg.DBPath}, nil
}
func (n *noopRecorder) Close() error { return nil }

// ---- sqlite recorder ----

type sqliteRecorder struct {
	cfg           Config
	db            sqlexec
	dropCount     *atomic.Int64
	stopOnce      sync.Once
	stopCh        chan struct{}
	closed        atomic.Bool
	retentionDays int
}

// small interface so tests can substitute a fake.
type sqlexec interface {
	Exec(query string, args ...any) (affected int64, lastID int64, err error)
	Query(query string, args ...any) (rows [][]any, cols []string, err error)
	Close() error
}

func (r *sqliteRecorder) pruneLoop() {
	retentionDays := r.retentionDays
	if retentionDays <= 0 {
		return
	}
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-r.stopCh:
			return
		case <-t.C:
			if r.closed.Load() {
				return
			}
			r.pruneWithDays(retentionDays)
		}
	}
}

func (r *sqliteRecorder) pruneWithDays(retentionDays int) {
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)
	_, _, err := r.db.Exec(`DELETE FROM audit_events WHERE time < ?`,
		cutoff.Format(time.RFC3339Nano))
	if err != nil {
		r.dropCount.Add(1)
		log.Printf("audit: prune failed: %v", err)
	}
}

func (r *sqliteRecorder) prune() {
	r.pruneWithDays(r.cfg.RetentionDays)
}

func (r *sqliteRecorder) Close() error {
	r.closed.Store(true)
	if r.stopCh != nil {
		r.stopOnce.Do(func() { close(r.stopCh) })
	}
	if r.db == nil {
		return nil
	}
	return r.db.Close()
}

func (r *sqliteRecorder) Record(_ context.Context, e Event) {
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	// Keep client-supplied correlation id; fall back to server-generated UUID
	// only when the caller did not provide X-Request-Id.
	if e.RequestID == "" {
		e.RequestID = "req_" + uuid.NewString()[:8]
	}
	if e.Actor == "" {
		e.Actor = "unknown"
	}
	if e.Action == "" {
		e.Action = ActionOp
	}
	if e.Result == "" {
		e.Result = ResultSuccess
	}
	e.Detail = truncate(e.Detail, MaxDetailChars)
	e.UserAgent = truncate(e.UserAgent, MaxUserAgentChars)
	e.ContainerID = normalizeContainerID(e.ContainerID)
	payload := marshalPayload(e.Payload)
	_, _, err := r.db.Exec(
		`INSERT INTO audit_events
      (time, actor, actor_kind, source, action, container_id, container_name,
       result, status_code, duration_ms, detail, request_id, client_ip, user_agent, payload)
     VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Time.UTC().Format(time.RFC3339Nano), e.Actor, e.ActorKind, e.Source,
		e.Action, e.ContainerID, e.ContainerName, e.Result, e.StatusCode,
		e.DurationMs, e.Detail, e.RequestID, e.ClientIP, e.UserAgent, payload,
	)
	if err != nil {
		r.dropCount.Add(1)
		log.Printf("audit: record failed: %v", err)
	}
}

func filtersMap(q Query) map[string]any {
	m := map[string]any{}
	if !q.Since.IsZero() {
		m["since"] = q.Since.UTC().Format(time.RFC3339)
	}
	if !q.Until.IsZero() {
		m["until"] = q.Until.UTC().Format(time.RFC3339)
	}
	if q.ContainerID != "" {
		m["container_id"] = q.ContainerID
	}
	if q.ContainerName != "" {
		m["container_name"] = q.ContainerName
	}
	if len(q.Actions) > 0 {
		m["action"] = strings.Join(q.Actions, ",")
	}
	if len(q.Results) > 0 {
		m["result"] = strings.Join(q.Results, ",")
	}
	return m
}

func (r *sqliteRecorder) List(_ context.Context, q Query) (Page, error) {
	where, args := buildWhere(q)

	limit := q.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	order := "DESC"
	if !q.SortTimeDesc {
		order = "ASC"
	}

	var total int64
	rows, _, err := r.db.Query(`SELECT COUNT(*) FROM audit_events`+where, args...)
	if err == nil && len(rows) > 0 && len(rows[0]) > 0 {
		total, _ = rows[0][0].(int64)
	}
	if err != nil {
		return Page{}, fmt.Errorf("audit: count: %w", err)
	}

	query := `SELECT id, time, actor, actor_kind, source, action, container_id, container_name,
	             result, status_code, duration_ms, detail, request_id, client_ip, user_agent, payload
	          FROM audit_events` + where +
		` ORDER BY time ` + order + `, id ` + order +
		` LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, _, err = r.db.Query(query, args...)
	if err != nil {
		return Page{}, fmt.Errorf("audit: list: %w", err)
	}
	items := make([]Item, 0, len(rows))
	for _, row := range rows {
		items = append(items, itemFromRow(row))
	}
	return Page{
		Total:   total,
		Count:   len(items),
		Offset:  offset,
		Limit:   limit,
		Filters: filtersMap(q),
		Items:   items,
	}, nil
}

func buildWhere(q Query) (string, []any) {
	var conds []string
	var args []any
	if !q.Since.IsZero() {
		conds = append(conds, `time >= ?`)
		args = append(args, q.Since.UTC().Format(time.RFC3339Nano))
	}
	if !q.Until.IsZero() {
		conds = append(conds, `time <= ?`)
		args = append(args, q.Until.UTC().Format(time.RFC3339Nano))
	}
	if q.ContainerID != "" {
		// Prefix match on id (12-char short id works). Both stored IDs and
		// query IDs are normalised to strip "sha256:"/"docker://" prefixes.
		cid := normalizeContainerID(q.ContainerID)
		conds = append(conds, `(container_id = ? OR substr(container_id, 1, ?) = ?)`)
		args = append(args, cid, len(cid), cid)
	}
	if q.ContainerName != "" {
		conds = append(conds, `LOWER(container_name) LIKE ?`)
		args = append(args, "%"+strings.ToLower(q.ContainerName)+"%")
	}
	if len(q.Actions) > 0 {
		ph := strings.TrimRight(strings.Repeat("?,", len(q.Actions)), ",")
		conds = append(conds, `action IN (`+ph+`)`)
		for _, a := range q.Actions {
			args = append(args, a)
		}
	}
	if len(q.Results) > 0 {
		ph := strings.TrimRight(strings.Repeat("?,", len(q.Results)), ",")
		conds = append(conds, `result IN (`+ph+`)`)
		for _, x := range q.Results {
			args = append(args, x)
		}
	}
	if q.Actor != "" {
		conds = append(conds, `actor = ?`)
		args = append(args, q.Actor)
	}
	if len(conds) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func itemFromRow(row []any) Item {
	getStr := func(i int) string {
		if i >= len(row) || row[i] == nil {
			return ""
		}
		return fmt.Sprint(row[i])
	}
	getInt := func(i int) int64 {
		if i >= len(row) || row[i] == nil {
			return 0
		}
		switch v := row[i].(type) {
		case int64:
			return v
		case int:
			return int64(v)
		case float64:
			return int64(v)
		default:
			return 0
		}
	}
	var payload map[string]any
	if raw := getStr(15); raw != "" {
		_ = json.Unmarshal([]byte(raw), &payload)
	}
	return Item{
		ID:            getInt(0),
		Time:          getStr(1),
		Actor:         getStr(2),
		ActorKind:     getStr(3),
		Source:        getStr(4),
		Action:        getStr(5),
		ContainerID:   getStr(6),
		ContainerName: getStr(7),
		Result:        getStr(8),
		StatusCode:    int(getInt(9)),
		DurationMs:    getInt(10),
		Detail:        getStr(11),
		RequestID:     getStr(12),
		ClientIP:      getStr(13),
		UserAgent:     getStr(14),
		Payload:       payload,
	}
}

func (r *sqliteRecorder) Stats(_ context.Context) (Stats, error) {
	var s Stats
	s.RetentionDays = r.cfg.RetentionDays
	s.DBPath = r.cfg.DBPath
	s.DropCount = r.dropCount.Load()

	rows, _, err := r.db.Query(`SELECT COUNT(*) FROM audit_events`)
	if err != nil {
		return s, err
	}
	if len(rows) > 0 && len(rows[0]) > 0 {
		s.Total, _ = rows[0][0].(int64)
	}
	since := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339Nano)
	rows, _, err = r.db.Query(`SELECT COUNT(*) FROM audit_events WHERE time >= ?`, since)
	if err == nil && len(rows) > 0 && len(rows[0]) > 0 {
		s.Last24h, _ = rows[0][0].(int64)
	}
	rows, _, err = r.db.Query(`SELECT COUNT(*) FROM audit_events WHERE time >= ? AND result = ?`, since, ResultFailure)
	if err == nil && len(rows) > 0 && len(rows[0]) > 0 {
		s.Failures24h, _ = rows[0][0].(int64)
	}
	rows, _, err = r.db.Query(`SELECT COUNT(*) FROM audit_events WHERE time >= ? AND result = ?`, since, ResultDenied)
	if err == nil && len(rows) > 0 && len(rows[0]) > 0 {
		s.Denied24h, _ = rows[0][0].(int64)
	}
	return s, nil
}

// Export writes filtered events in either "json" or "md" format.
func (r *sqliteRecorder) Export(ctx context.Context, q Query, format string) ([]byte, string, error) {
	q2 := q
	q2.Limit = ExportCap
	q2.Offset = 0
	page, err := r.List(ctx, q2)
	if err != nil {
		return nil, "", err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	baseName := "audit-" + sanitizeFileTime(q.Since) + "-" + sanitizeFileTime(q.Until)
	switch format {
	case "md", "markdown":
		md := renderMarkdown(page, now, q)
		return []byte(md), baseName + ".md", nil
	case "json", "":
		b, err := json.MarshalIndent(page.Items, "", "  ")
		if err != nil {
			return nil, "", err
		}
		return b, baseName + ".json", nil
	default:
		return nil, "", fmt.Errorf("unknown export format %q", format)
	}
}

func sanitizeFileTime(t time.Time) string {
	if t.IsZero() {
		return "all"
	}
	return t.UTC().Format("20060102T150405Z")
}

func mdEscape(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}

func renderMarkdown(page Page, generated string, q Query) string {
	var b strings.Builder
	b.WriteString("# Audit export\n\n")
	b.WriteString("- Generated at: `" + generated + "`\n")
	b.WriteString(fmt.Sprintf("- Rows: **%d** of %d total\n", page.Count, page.Total))
	if !q.Since.IsZero() {
		b.WriteString("- Since: `" + q.Since.UTC().Format(time.RFC3339) + "`\n")
	}
	if !q.Until.IsZero() {
		b.WriteString("- Until: `" + q.Until.UTC().Format(time.RFC3339) + "`\n")
	}
	if len(q.Actions) > 0 {
		b.WriteString("- Actions: `" + strings.Join(q.Actions, ",") + "`\n")
	}
	if len(q.Results) > 0 {
		b.WriteString("- Results: `" + strings.Join(q.Results, ",") + "`\n")
	}
	b.WriteString("\n| Time (UTC) | Actor | Source | Action | Container | Result | Status | Dur (ms) | Detail |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|\n")
	for _, it := range page.Items {
		name := it.ContainerName
		if it.ContainerID != "" {
			short := it.ContainerID
			if len(short) > 12 {
				short = short[:12]
			}
			if name != "" {
				name = name + " (" + short + ")"
			} else {
				name = short
			}
		}
		b.WriteString(fmt.Sprintf("| %s | `%s` | %s | %s | %s | %s | %d | %d | %s |\n",
			mdEscape(strings.ReplaceAll(it.Time, "T", " ")),
			mdEscape(it.Actor),
			mdEscape(it.Source),
			mdEscape(it.Action),
			mdEscape(name),
			mdEscape(it.Result),
			it.StatusCode,
			it.DurationMs,
			mdEscape(truncate(it.Detail, 120)),
		))
	}
	if page.Count < int(page.Total) {
		b.WriteString(fmt.Sprintf("\n_Export capped at %d rows (%d total match). Tighten filters and re-export._\n", ExportCap, page.Total))
	}
	return b.String()
}

// ErrStorageDisabled is returned (as 503) by audit HTTP handlers when the
// recorder is a noop.
var ErrStorageDisabled = errors.New("audit storage unavailable")

// Ensure parent dir exists for tests that set relative paths; use fs.MkdirAll
// compatibility wrapper.
var _ = fs.FileMode(0)
var _ = os.ErrNotExist

// ParseDuration extends time.ParseDuration with "d"/"w" suffixes for days/weeks,
// used by time-range shorthands such as "since=7d" or "since=2w". Returns
// (d, true) on success, (0, false) on malformed/non-positive input.
func ParseDuration(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	mul := time.Duration(1)
	switch {
	case strings.HasSuffix(s, "w"):
		mul = 7 * 24 * time.Hour
		s = s[:len(s)-1]
	case strings.HasSuffix(s, "d"):
		mul = 24 * time.Hour
		s = s[:len(s)-1]
	}
	if mul != 1 {
		n, err := strconv.ParseFloat(s, 64)
		if err != nil || n <= 0 {
			return 0, false
		}
		return time.Duration(n * float64(mul)), true
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, false
	}
	return d, true
}
