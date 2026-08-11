package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zsuroy/dockerview-go/internal/audit"
)

// parseTime parses an RFC3339 timestamp; it accepts the range shorthand "24h",
// "7d", "2w" etc. relative to now (d/w suffixes supported via audit.ParseDuration).
func parseTime(s string, now time.Time) (time.Time, bool, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false, nil
	}
	if d, ok := audit.ParseDuration(s); ok {
		return now.Add(-d), true, nil
	}
	// Allow plain date (YYYY-MM-DD) as a convenience.
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, true, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true, nil
	}
	return time.Time{}, false, fmt.Errorf("invalid time %q (want RFC3339, YYYY-MM-DD, or duration like 24h/7d/2w)", s)
}

func splitCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	http.Error(w, msg, status)
}

func (s *Server) buildQuery(r *http.Request) (audit.Query, error) {
	q := audit.Query{SortTimeDesc: true}
	now := time.Now().UTC()
	if v := r.URL.Query().Get("since"); v != "" {
		t, ok, err := parseTime(v, now)
		if err != nil {
			return q, err
		}
		if ok {
			q.Since = t
		}
	}
	if v := r.URL.Query().Get("until"); v != "" {
		t, ok, err := parseTime(v, now)
		if err != nil {
			return q, err
		}
		if ok {
			q.Until = t
		}
	}
	q.ContainerID = r.URL.Query().Get("container_id")
	q.ContainerName = r.URL.Query().Get("container_name")
	q.Actions = splitCSV(r.URL.Query().Get("action"))
	q.Results = splitCSV(r.URL.Query().Get("result"))
	q.Actor = r.URL.Query().Get("actor")
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return q, fmt.Errorf("invalid limit %q", v)
		}
		if n > 200 {
			n = 200
		}
		q.Limit = n
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return q, fmt.Errorf("invalid offset %q", v)
		}
		q.Offset = n
	}
	if v := r.URL.Query().Get("sort"); v == "time_asc" {
		q.SortTimeDesc = false
	}
	return q, nil
}

// handleAudit serves GET /api/audit
func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if _, ok := s.checkAuthEx(w, r); !ok {
		actorV, kindV, sourceV, ipV, uaV := audit.ActorFromRequest(r, "")
		s.aud().Record(r.Context(), audit.Event{
			Action:     audit.ActionAudit,
			Result:     audit.ResultDenied,
			StatusCode: http.StatusUnauthorized,
			Detail:     "invalid or missing token on /api/audit",
			Actor:      actorV, ActorKind: kindV, Source: sourceV,
			ClientIP: ipV, UserAgent: uaV, RequestID: r.Header.Get("X-Request-Id"),
		})
		return
	}
	q, err := s.buildQuery(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	page, err := s.aud().List(r.Context(), q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "audit list: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// handleAuditExport serves GET /api/audit/export?format=json|md
func (s *Server) handleAuditExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if _, ok := s.checkAuthEx(w, r); !ok {
		actorV, kindV, sourceV, ipV, uaV := audit.ActorFromRequest(r, "")
		s.aud().Record(r.Context(), audit.Event{
			Action: audit.ActionAudit, Result: audit.ResultDenied, StatusCode: http.StatusUnauthorized,
			Detail: "invalid or missing token on /api/audit/export",
			Actor:  actorV, ActorKind: kindV, Source: sourceV, ClientIP: ipV, UserAgent: uaV,
			RequestID: r.Header.Get("X-Request-Id"),
		})
		return
	}
	q, err := s.buildQuery(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}
	body, name, err := s.aud().Export(r.Context(), q, format)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ct := "application/json; charset=utf-8"
	if format == "md" || format == "markdown" {
		ct = "text/markdown; charset=utf-8"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// handleAuditStats serves GET /api/audit/stats
func (s *Server) handleAuditStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if _, ok := s.checkAuthEx(w, r); !ok {
		actorV, kindV, sourceV, ipV, uaV := audit.ActorFromRequest(r, "")
		s.aud().Record(r.Context(), audit.Event{
			Action: audit.ActionAudit, Result: audit.ResultDenied, StatusCode: http.StatusUnauthorized,
			Detail: "invalid or missing token on /api/audit/stats",
			Actor:  actorV, ActorKind: kindV, Source: sourceV, ClientIP: ipV, UserAgent: uaV,
			RequestID: r.Header.Get("X-Request-Id"),
		})
		return
	}
	st, err := s.aud().Stats(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "audit stats: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}
