package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/zsuroy/dockerview-go/internal/audit"
	"github.com/zsuroy/dockerview-go/internal/backup"
)

// Backup endpoint timeouts. Create runs on a detached context so a client
// disconnect cannot interrupt a started archive (same rationale as prune's
// delete phase); temp-file cleanup keeps the directory consistent either way.
const (
	backupPreviewTimeout = 30 * time.Second
	backupCreateTimeout  = 10 * time.Minute
)

// SetBackupManager installs the backup manager. When nil, all backup
// endpoints answer 503 backup_unavailable.
func (s *Server) SetBackupManager(m *backup.Manager) {
	s.mu.Lock()
	s.backupMgr = m
	s.mu.Unlock()
}

func (s *Server) backupManager() *backup.Manager {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.backupMgr
}

func writeBackupError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": message})
}

// backupAuditor bundles the repeated audit bookkeeping for backup handlers.
type backupAuditor struct {
	s       *Server
	started time.Time
	action  string
	actor   string
	kind    string
	source  string
	ip      string
	ua      string
	reqID   string
}

func (s *Server) newBackupAuditor(r *http.Request, action string, ra resolvedActor, authed bool) *backupAuditor {
	actorV, kindV, sourceV, ipV, uaV := audit.ActorFromRequest(r, "")
	if authed {
		actorV, kindV, sourceV, ipV, uaV = ra.auditActor(r)
	}
	return &backupAuditor{
		s:       s,
		started: time.Now(),
		action:  action,
		actor:   actorV,
		kind:    kindV,
		source:  sourceV,
		ip:      ipV,
		ua:      uaV,
		reqID:   r.Header.Get("X-Request-Id"),
	}
}

func (b *backupAuditor) record(ctx context.Context, result string, status int, detail string, payload map[string]any) {
	b.s.aud().Record(ctx, audit.Event{
		Time:       b.started,
		Actor:      b.actor,
		ActorKind:  b.kind,
		Source:     b.source,
		Action:     b.action,
		Result:     result,
		StatusCode: status,
		DurationMs: time.Since(b.started).Milliseconds(),
		Detail:     detail,
		ClientIP:   b.ip,
		UserAgent:  b.ua,
		RequestID:  b.reqID,
		Payload:    payload,
	})
}

// truncatePayloadNote keeps operator notes bounded in audit payloads.
func truncatePayloadNote(payload map[string]any) {
	if note, ok := payload["note"].(string); ok && len(note) > 200 {
		payload["note"] = note[:200] + "…"
	}
}

// handleBackupPreview returns the packing plan. Admin-only (the plan includes
// infra details) and guaranteed zero-disk: nothing lands in data/backups.
func (s *Server) handleBackupPreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method != http.MethodPost {
		writeBackupError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	ra, authed := s.checkAuthEx(w, r)
	aud := s.newBackupAuditor(r, audit.ActionBackupPreview, ra, authed)
	if !authed {
		aud.record(r.Context(), audit.ResultDenied, http.StatusUnauthorized, "invalid or missing token", nil)
		return
	}
	mgr := s.backupManager()
	if mgr == nil {
		writeBackupError(w, http.StatusServiceUnavailable, "backup_unavailable", "backup backend not configured")
		aud.record(r.Context(), audit.ResultFailure, http.StatusServiceUnavailable, "backup backend not configured", nil)
		return
	}
	var opts backup.Options
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&opts); err != nil && !errors.Is(err, io.EOF) {
		writeBackupError(w, http.StatusBadRequest, "invalid_body", "request body must be JSON with optional include_images bool")
		aud.record(r.Context(), audit.ResultFailure, http.StatusBadRequest, "invalid body", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), backupPreviewTimeout)
	defer cancel()
	rep, err := mgr.Preview(ctx, opts)
	if err != nil {
		writeBackupError(w, http.StatusBadGateway, "provider_error", err.Error())
		aud.record(r.Context(), audit.ResultFailure, http.StatusBadGateway, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, rep)
	aud.record(r.Context(), audit.ResultSuccess, http.StatusOK, "",
		map[string]any{"include_images": opts.IncludeImages, "include_stopped": opts.IncludeStopped, "containers": rep.Containers})
}

// handleBackupCreate builds one snapshot package atomically. Admin-only.
func (s *Server) handleBackupCreate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method != http.MethodPost {
		writeBackupError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	ra, authed := s.checkAuthEx(w, r)
	aud := s.newBackupAuditor(r, audit.ActionBackupCreate, ra, authed)
	if !authed {
		aud.record(r.Context(), audit.ResultDenied, http.StatusUnauthorized, "invalid or missing token", nil)
		return
	}
	mgr := s.backupManager()
	if mgr == nil {
		writeBackupError(w, http.StatusServiceUnavailable, "backup_unavailable", "backup backend not configured")
		aud.record(r.Context(), audit.ResultFailure, http.StatusServiceUnavailable, "backup backend not configured", nil)
		return
	}
	var req backup.CreateRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeBackupError(w, http.StatusBadRequest, "invalid_body", "request body must be JSON")
		aud.record(r.Context(), audit.ResultFailure, http.StatusBadRequest, "invalid body", nil)
		return
	}
	payload := map[string]any{"include_images": req.IncludeImages, "include_stopped": req.IncludeStopped, "note": req.Note}
	truncatePayloadNote(payload)

	// Detached context: a client disconnect must not interrupt a started
	// archive; failures still clean the temp file (BACKUP_DESIGN §6).
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), backupCreateTimeout)
	defer cancel()

	rep, err := mgr.Create(ctx, req)
	switch {
	case errors.Is(err, backup.ErrCreateInProgress):
		writeBackupError(w, http.StatusConflict, "create_in_progress", err.Error())
		aud.record(r.Context(), audit.ResultDenied, http.StatusConflict, "another create in progress", payload)
		return
	case errors.Is(err, backup.ErrImageExport):
		writeBackupError(w, http.StatusBadGateway, "image_export_failed", err.Error())
		aud.record(r.Context(), audit.ResultFailure, http.StatusBadGateway, err.Error(), payload)
		return
	case err != nil:
		writeBackupError(w, http.StatusBadGateway, "backup_error", err.Error())
		aud.record(r.Context(), audit.ResultFailure, http.StatusBadGateway, err.Error(), payload)
		return
	}

	writeJSON(w, http.StatusOK, rep)
	payload["name"] = rep.Name
	payload["size_bytes"] = rep.SizeBytes
	aud.record(r.Context(), audit.ResultSuccess, http.StatusOK, "", payload)
}

// handleBackupList returns archive history. Requires a valid token (guests
// cannot browse the archive inventory — BACKUP_DESIGN §8).
func (s *Server) handleBackupList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method != http.MethodGet {
		writeBackupError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
		return
	}
	ra, authed := s.checkAuthEx(w, r)
	aud := s.newBackupAuditor(r, audit.ActionBackupList, ra, authed)
	if !authed {
		aud.record(r.Context(), audit.ResultDenied, http.StatusUnauthorized, "invalid or missing token", nil)
		return
	}
	mgr := s.backupManager()
	if mgr == nil {
		writeBackupError(w, http.StatusServiceUnavailable, "backup_unavailable", "backup backend not configured")
		aud.record(r.Context(), audit.ResultFailure, http.StatusServiceUnavailable, "backup backend not configured", nil)
		return
	}
	rep, err := mgr.List()
	if err != nil {
		writeBackupError(w, http.StatusInternalServerError, "list_error", err.Error())
		aud.record(r.Context(), audit.ResultFailure, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, rep)
	aud.record(r.Context(), audit.ResultSuccess, http.StatusOK, "",
		map[string]any{"count": len(rep.Backups)})
}

// handleBackupDownload streams one archive. Requires a valid token; the name
// must pass the whitelist regex and stay inside the backup dir (no `..`).
func (s *Server) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method != http.MethodGet {
		writeBackupError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
		return
	}
	ra, authed := s.checkAuthEx(w, r)
	aud := s.newBackupAuditor(r, audit.ActionBackupDownload, ra, authed)
	if !authed {
		aud.record(r.Context(), audit.ResultDenied, http.StatusUnauthorized, "invalid or missing token", nil)
		return
	}
	mgr := s.backupManager()
	if mgr == nil {
		writeBackupError(w, http.StatusServiceUnavailable, "backup_unavailable", "backup backend not configured")
		aud.record(r.Context(), audit.ResultFailure, http.StatusServiceUnavailable, "backup backend not configured", nil)
		return
	}
	name := r.URL.Query().Get("name")
	full, err := mgr.OpenPath(name)
	switch {
	case errors.Is(err, backup.ErrInvalidName):
		writeBackupError(w, http.StatusBadRequest, "invalid_name", "archive name fails the whitelist check")
		aud.record(r.Context(), audit.ResultDenied, http.StatusBadRequest, "invalid name: "+sanitizeAuditDetail(name), nil)
		return
	case errors.Is(err, backup.ErrNotFound):
		writeBackupError(w, http.StatusNotFound, "not_found", "archive does not exist")
		aud.record(r.Context(), audit.ResultFailure, http.StatusNotFound, "not found: "+sanitizeAuditDetail(name), nil)
		return
	case err != nil:
		writeBackupError(w, http.StatusInternalServerError, "open_error", err.Error())
		aud.record(r.Context(), audit.ResultFailure, http.StatusInternalServerError, err.Error(), nil)
		return
	}

	f, err := os.Open(full)
	if err != nil {
		writeBackupError(w, http.StatusNotFound, "not_found", "archive does not exist")
		aud.record(r.Context(), audit.ResultFailure, http.StatusNotFound, err.Error(), nil)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		writeBackupError(w, http.StatusInternalServerError, "stat_error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Content-Length", itoa64(fi.Size()))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	n, copyErr := io.Copy(w, f)
	result, status := audit.ResultSuccess, http.StatusOK
	if copyErr != nil {
		result, status = audit.ResultFailure, http.StatusInternalServerError
	}
	// Stream finished (or client left): record with a detached context so the
	// audit row survives a disconnect.
	aud.record(context.Background(), result, status, "",
		map[string]any{"name": name, "size_bytes": n})
}

// handleBackupDelete removes one archive. Admin-only.
func (s *Server) handleBackupDelete(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method != http.MethodPost {
		writeBackupError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	ra, authed := s.checkAuthEx(w, r)
	aud := s.newBackupAuditor(r, audit.ActionBackupDelete, ra, authed)
	if !authed {
		aud.record(r.Context(), audit.ResultDenied, http.StatusUnauthorized, "invalid or missing token", nil)
		return
	}
	mgr := s.backupManager()
	if mgr == nil {
		writeBackupError(w, http.StatusServiceUnavailable, "backup_unavailable", "backup backend not configured")
		aud.record(r.Context(), audit.ResultFailure, http.StatusServiceUnavailable, "backup backend not configured", nil)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeBackupError(w, http.StatusBadRequest, "invalid_body", "request body must be JSON with name")
		aud.record(r.Context(), audit.ResultFailure, http.StatusBadRequest, "invalid body", nil)
		return
	}
	payload := map[string]any{"name": body.Name}
	err := mgr.Delete(body.Name)
	switch {
	case errors.Is(err, backup.ErrInvalidName):
		writeBackupError(w, http.StatusBadRequest, "invalid_name", "archive name fails the whitelist check")
		aud.record(r.Context(), audit.ResultDenied, http.StatusBadRequest, "invalid name", payload)
		return
	case errors.Is(err, backup.ErrNotFound):
		writeBackupError(w, http.StatusNotFound, "not_found", "archive does not exist")
		aud.record(r.Context(), audit.ResultFailure, http.StatusNotFound, "not found", payload)
		return
	case err != nil:
		writeBackupError(w, http.StatusInternalServerError, "delete_error", err.Error())
		aud.record(r.Context(), audit.ResultFailure, http.StatusInternalServerError, err.Error(), payload)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": body.Name})
	aud.record(r.Context(), audit.ResultSuccess, http.StatusOK, "", payload)
}

// sanitizeAuditDetail keeps untrusted input short and single-line in audit.
func sanitizeAuditDetail(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}

// itoa64 renders a size without pulling strconv everywhere.
func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
