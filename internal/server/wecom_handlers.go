package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/zsuroy/dockerview-go/internal/audit"
	"github.com/zsuroy/dockerview-go/internal/wecom"
)

// maxInjectBody caps the inject request body.
const maxInjectBody = 1 << 20

// wecomAdmin authorizes an inject. It is deliberately stricter than
// checkAuthEx.
//
// checkAuthEx treats an unset server token as "everyone is an admin", which is
// fine for read-only routes such as /data. Inject feeds a message into the
// long connection, so when no token is configured we cannot tell an admin from
// a guest and refuse outright.
func (s *Server) wecomAdmin(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if s.token == "" {
		http.Error(w, "wecom inject is disabled: no admin token is configured on this instance",
			http.StatusForbidden)
		return false
	}
	if !s.matchToken(extractToken(r)) {
		actor, kind, source, ip, ua := audit.ActorFromRequest(r, "")
		s.aud().Record(r.Context(), audit.Event{
			Time:       time.Now().UTC(),
			Actor:      actor,
			ActorKind:  kind,
			Source:     source,
			Action:     "wecom_inject",
			Result:     audit.ResultDenied,
			StatusCode: http.StatusUnauthorized,
			Detail:     "invalid or missing admin token on wecom inject",
			ClientIP:   ip,
			UserAgent:  ua,
		})
		http.Error(w, "Unauthorized: an admin token is required to inject", http.StatusUnauthorized)
		return false
	}
	return true
}

// injectAllowed reports whether this request could inject, without writing an
// error. The console uses it to decide whether to enable the button; the
// handler still checks independently.
func (s *Server) injectAllowed(r *http.Request) bool {
	if s.token == "" {
		return false
	}
	return s.matchToken(extractToken(r))
}

// handleWeComState serves GET /api/wecom/state.
//
// Read-only and open to guests, mirroring /data and /api/networks/topology.
// It never returns the Secret: only whether one is configured, plus a masked
// BotID.
func (s *Server) handleWeComState(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	allowed := s.injectAllowed(r)

	s.mu.RLock()
	b := s.wecomBridge
	s.mu.RUnlock()

	if b == nil {
		writeJSON(w, http.StatusOK, wecom.Snapshot{
			Enabled:       false,
			Mode:          string(wecom.ModeOff),
			State:         string(wecom.StateDisconnected),
			StateReason:   "wecom 未启用（config.yaml 的 wecom.enabled）",
			WSURL:         "wss://openws.work.weixin.qq.com",
			ReplyMode:     string(wecom.DefaultReplyMode),
			InjectAllowed: allowed,
			Recent:        []wecom.Entry{},
		})
		return
	}

	writeJSON(w, http.StatusOK, b.Snapshot(allowed))
}

// handleWeComInject serves POST /api/wecom/inject.
//
// Body: {"text": "...", "user_id": "optional", "chat_id": "optional"}
//
// Admin only. The injected text goes through the same handler a real WeCom
// message hits — this is the whole point of the mock/drill path.
func (s *Server) handleWeComInject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.wecomAdmin(w, r) {
		return
	}

	s.mu.RLock()
	b := s.wecomBridge
	s.mu.RUnlock()
	if b == nil {
		http.Error(w, "wecom is not enabled", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		Text   string `json:"text"`
		UserID string `json:"user_id"`
		ChatID string `json:"chat_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxInjectBody)).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	body.Text = strings.TrimSpace(body.Text)
	if body.Text == "" {
		http.Error(w, "Missing text", http.StatusBadRequest)
		return
	}
	if len(body.Text) > wecom.MaxQuestionChars {
		http.Error(w, "Text too long", http.StatusBadRequest)
		return
	}

	entry, err := b.Inject(body.Text, body.UserID, body.ChatID)
	switch {
	case errors.Is(err, wecom.ErrEmptyText):
		http.Error(w, "Missing text", http.StatusBadRequest)
		return
	case errors.Is(err, wecom.ErrTooLong):
		http.Error(w, "Text too long", http.StatusBadRequest)
		return
	case errors.Is(err, wecom.ErrDisabled):
		http.Error(w, "wecom is not enabled", http.StatusServiceUnavailable)
		return
	case err != nil:
		log.Printf("[WARN] wecom inject: %v", err)
		http.Error(w, "Inject failed", http.StatusInternalServerError)
		return
	}

	// Audit the inject itself: it is an admin action that puts words in the
	// bot's mouth.
	actor, kind, source, ip, ua := audit.ActorFromRequest(r, extractToken(r))
	s.aud().Record(r.Context(), audit.Event{
		Time:       time.Now().UTC(),
		Actor:      actor,
		ActorKind:  kind,
		Source:     source,
		Action:     "wecom_inject",
		Result:     audit.ResultSuccess,
		StatusCode: http.StatusAccepted,
		Detail:     "injected " + truncateForAudit(body.Text, audit.MaxCmdChars),
		ClientIP:   ip,
		UserAgent:  ua,
	})

	// The reply is produced asynchronously by the bridge. The console polls
	// /api/wecom/state, so Accepted is the honest status here.
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":  "accepted",
		"inbound": entry,
	})
}

// handleWeComWelcome serves POST /api/wecom/welcome.
//
// Admin only. Fires a synthetic enter_chat so the welcome path can be drilled
// without a real WeCom connection.
func (s *Server) handleWeComWelcome(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.wecomAdmin(w, r) {
		return
	}

	s.mu.RLock()
	b := s.wecomBridge
	s.mu.RUnlock()
	if b == nil {
		http.Error(w, "wecom is not enabled", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		UserID string `json:"user_id"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, maxInjectBody)).Decode(&body)

	if err := b.InjectEnterChat(body.UserID); err != nil {
		log.Printf("[WARN] wecom welcome drill: %v", err)
		http.Error(w, "Welcome drill failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

// truncateForAudit bounds a string recorded in an audit row.
func truncateForAudit(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// WeComStatusLine renders the read-only TUI row. It reads the bridge live on
// every call, so the terminal row tracks reconnects and new traffic. Returns
// "" when the integration is not installed.
func (s *Server) WeComStatusLine() string {
	s.mu.RLock()
	b := s.wecomBridge
	s.mu.RUnlock()
	if b == nil {
		return ""
	}
	// injectAllowed is irrelevant to a read-only row; the TUI never injects.
	return wecom.StatusLine(b.Snapshot(false))
}
