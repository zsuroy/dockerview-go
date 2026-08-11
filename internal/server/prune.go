package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zsuroy/dockerview-go/internal/docker"
)

const maxAuditEvents = 200

// AuditEvent records one prune action (including denied attempts).
type AuditEvent struct {
	ID             string `json:"id"`
	Time           string `json:"time"`
	Actor          string `json:"actor"`
	ActorIP        string `json:"actor_ip"`
	Action         string `json:"action"`
	Status         string `json:"status"` // success | partial | denied | failed
	ImagesDeleted  int    `json:"images_deleted"`
	VolumesDeleted int    `json:"volumes_deleted"`
	ImagesFailed   int    `json:"images_failed"`
	VolumesFailed  int    `json:"volumes_failed"`
	Skipped        int    `json:"skipped"`
	ReclaimedBytes int64  `json:"reclaimed_bytes"`
	Detail         string `json:"detail,omitempty"`
}

// auditLog is a bounded, append-only ring buffer of audit events.
type auditLog struct {
	mu     sync.Mutex
	events []AuditEvent
	seq    uint64
}

func newAuditLog() *auditLog { return &auditLog{events: make([]AuditEvent, 0, maxAuditEvents)} }

// Record appends an event, evicting the oldest when full. It also emits a single
// structured line to stdout so the host journal captures the audit trail.
func (a *auditLog) Record(e AuditEvent) AuditEvent {
	a.mu.Lock()
	a.seq++
	if e.ID == "" {
		e.ID = generateEventID(a.seq, e.Time)
	}
	a.events = append(a.events, e)
	if len(a.events) > maxAuditEvents {
		a.events = a.events[len(a.events)-maxAuditEvents:]
	}
	a.mu.Unlock()
	return e
}

// Events returns a copy of the recorded events (newest last).
func (a *auditLog) Events() []AuditEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]AuditEvent, len(a.events))
	copy(out, a.events)
	return out
}

func generateEventID(seq uint64, ts string) string {
	h := sha256.Sum256([]byte(ts + ":" + itoa(seq)))
	return hex.EncodeToString(h[:])[:16]
}

func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// tokenPrefix returns a short non-reversible identifier for the presented token
// so audit entries can distinguish actors without logging the secret.
func tokenPrefix(token string) string {
	h := sha256.Sum256([]byte("dockerview-audit|" + token))
	return hex.EncodeToString(h[:])[:8]
}

// actorFromRequest returns a stable, non-secret actor label for audit.
func (s *Server) actorFromRequest(r *http.Request) string {
	// When the server has no token configured, auth is disabled and every
	// request is effectively admin (backward compatible). Label accordingly.
	if s.token == "" {
		return "admin(no-token-configured)"
	}
	presented := presentedToken(r)
	if presented != "" && secureEqual(presented, s.token) {
		return "admin:" + tokenPrefix(presented)
	}
	if presented == "" {
		return "guest"
	}
	return "invalid-token:" + tokenPrefix(presented)
}

func presentedToken(r *http.Request) string {
	if t := r.URL.Query().Get("token"); t != "" {
		return t
	}
	if t := r.Header.Get("X-Auth-Token"); t != "" {
		return t
	}
	if a := r.Header.Get("Authorization"); strings.HasPrefix(a, "Bearer ") {
		return strings.TrimPrefix(a, "Bearer ")
	}
	return ""
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx > 0 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr // unix socket or already a bare host
	}
	return host
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	// Prune responses include candidate details, fingerprints and audit data;
	// never let intermediate proxies or the browser cache them.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writePruneError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": message})
}

// handlePruneCandidates returns dangling images and unused volumes. Read-only;
// no token required (mirrors /data).
func (s *Server) handlePruneCandidates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writePruneError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
		return
	}
	s.mu.RLock()
	pruner := s.pruner
	s.mu.RUnlock()
	if pruner == nil {
		writePruneError(w, http.StatusServiceUnavailable, "pruner_unavailable", "prune backend not available")
		return
	}
	c, err := pruner.Candidates(r.Context())
	if err != nil {
		writePruneError(w, http.StatusBadGateway, "docker_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// handlePruneDryRun returns a preview without deleting anything. Read-only; no
// token required.
func (s *Server) handlePruneDryRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writePruneError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	s.mu.RLock()
	pruner := s.pruner
	s.mu.RUnlock()
	if pruner == nil {
		writePruneError(w, http.StatusServiceUnavailable, "pruner_unavailable", "prune backend not available")
		return
	}
	var sel docker.Selection
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB cap
	if err := json.NewDecoder(r.Body).Decode(&sel); err != nil && !errors.Is(err, io.EOF) {
		writePruneError(w, http.StatusBadRequest, "invalid_body", "request body must be JSON with optional images/volumes arrays")
		return
	}
	rep, err := pruner.DryRun(r.Context(), sel)
	if err != nil {
		writePruneError(w, http.StatusBadGateway, "docker_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// handlePruneConfirm deletes candidates after auth + explicit confirmation.
func (s *Server) handlePruneConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writePruneError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	actor := s.actorFromRequest(r)
	ip := clientIP(r)
	base := AuditEvent{Time: time.Now().UTC().Format(time.RFC3339Nano), Actor: actor, ActorIP: ip, Action: "confirm"}

	if !s.checkAuth(w, r) {
		s.audit.Record(base.withStatus("denied", "missing or invalid token"))
		return
	}

	var req docker.ConfirmRequest
	// An empty body is treated as a zero-value request so the caller gets the
	// clearer confirmation_required error rather than a generic JSON error.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB cap
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		s.audit.Record(base.withStatus("denied", "invalid JSON body"))
		writePruneError(w, http.StatusBadRequest, "invalid_body", "request body must be JSON")
		return
	}

	s.mu.RLock()
	pruner := s.pruner
	s.mu.RUnlock()
	if pruner == nil {
		s.audit.Record(base.withStatus("failed", "pruner unavailable"))
		writePruneError(w, http.StatusServiceUnavailable, "pruner_unavailable", "prune backend not available")
		return
	}

	rep, err := pruner.Confirm(r.Context(), req)
	switch {
	case errors.Is(err, docker.ErrConfirmationRequired):
		s.audit.Record(base.withStatus("denied", "confirmation string missing"))
		writePruneError(w, http.StatusBadRequest, "confirmation_required", docker.ErrConfirmationRequired.Error())
		return
	case errors.Is(err, docker.ErrFingerprintRequired):
		s.audit.Record(base.withStatus("denied", "fingerprint missing"))
		writePruneError(w, http.StatusBadRequest, "fingerprint_required", docker.ErrFingerprintRequired.Error())
		return
	case errors.Is(err, docker.ErrFingerprintMismatch):
		s.audit.Record(base.withStatus("denied", "fingerprint mismatch"))
		writePruneError(w, http.StatusConflict, "fingerprint_mismatch", docker.ErrFingerprintMismatch.Error())
		return
	case errors.Is(err, docker.ErrConfirmInProgress):
		s.audit.Record(base.withStatus("denied", "another prune in progress"))
		writePruneError(w, http.StatusConflict, "in_progress", docker.ErrConfirmInProgress.Error())
		return
	case err != nil:
		s.audit.Record(base.withStatus("failed", err.Error()))
		writePruneError(w, http.StatusBadGateway, "docker_error", err.Error())
		return
	}

	// Count per-type outcomes.
	var imDel, volDel, imFail, volFail, skipped int
	for _, it := range rep.Items {
		switch it.Type {
		case "image":
			switch it.Status {
			case "deleted":
				imDel++
			case "failed":
				imFail++
			case "skipped":
				skipped++
			}
		case "volume":
			switch it.Status {
			case "deleted":
				volDel++
			case "failed":
				volFail++
			case "skipped":
				skipped++
			}
		}
	}
	status := "success"
	if imFail+volFail > 0 {
		status = "partial"
	}
	ev := base
	ev.Status = status
	ev.ImagesDeleted = imDel
	ev.VolumesDeleted = volDel
	ev.ImagesFailed = imFail
	ev.VolumesFailed = volFail
	ev.Skipped = skipped
	ev.ReclaimedBytes = rep.Summary.ReclaimedBytes
	s.audit.Record(ev)

	writeJSON(w, http.StatusOK, rep)
}

func (e AuditEvent) withStatus(status, detail string) AuditEvent {
	e.Status = status
	e.Detail = detail
	return e
}

// handlePruneAudit returns recorded audit events. Token required.
func (s *Server) handlePruneAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writePruneError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
		return
	}
	if !s.checkAuth(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": s.audit.Events()})
}
