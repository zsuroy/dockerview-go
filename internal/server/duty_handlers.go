package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zsuroy/dockerview-go/internal/audit"
	"github.com/zsuroy/dockerview-go/internal/docker"
	"github.com/zsuroy/dockerview-go/internal/duty"
)

// handleDutyAsk handles POST /api/duty/ask
func (s *Server) handleDutyAsk(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.dutyAgent == nil {
		http.Error(w, "Duty agent is disabled", http.StatusServiceUnavailable)
		return
	}

	// Auth: same token model as other endpoints. No token = anonymous (read-only).
	ra, authed := s.checkAuthEx(w, r)
	actor, kind, source, ip, ua := audit.ActorFromRequest(r, "")
	if authed {
		actor, kind, source, ip, ua = ra.auditActor(r)
	}

	var body struct {
		Question string `json:"question"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	body.Question = strings.TrimSpace(body.Question)
	if body.Question == "" {
		http.Error(w, "Missing question", http.StatusBadRequest)
		return
	}
	if len(body.Question) > 4000 {
		http.Error(w, "Question too long (max 4000 chars)", http.StatusBadRequest)
		return
	}

	// Use a timeout so a slow model doesn't hold the connection forever.
	ctx, cancel := contextWithTimeout(r, 60*time.Second)
	defer cancel()

	res, err := s.dutyAgent.Ask(ctx, body.Question, actor, kind, source)
	if err != nil {
		log.Printf("[WARN] duty ask error: %v", err)
		http.Error(w, "Duty agent error", http.StatusInternalServerError)
		return
	}

	// If the agent proposed a write, record a "proposed" audit event (not
	// an execution — the actual write goes through /api/container/op after
	// human confirmation).
	if res.ProposedWrite != nil {
		s.aud().Record(r.Context(), audit.Event{
			Time:          time.Now().UTC(),
			Actor:         actor,
			ActorKind:     kind,
			Source:        source,
			Action:        "duty_propose_" + res.ProposedWrite.Op,
			ContainerID:   res.ProposedWrite.ID,
			ContainerName: res.ProposedWrite.Name,
			Result:        audit.ResultSuccess,
			StatusCode:    http.StatusOK,
			Detail:        "duty agent proposed " + res.ProposedWrite.Op + "; awaiting human confirm",
			ClientIP:      ip,
			UserAgent:     ua,
			RequestID:     "duty_" + uuid.NewString()[:8],
		})
	}

	writeJSON(w, http.StatusOK, res)
}

// handleDutyTickets handles GET /api/duty/tickets
func (s *Server) handleDutyTickets(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.checkAuth(w, r) {
		return
	}
	if s.dutyAgent == nil || s.dutyAgent.Store() == nil {
		http.Error(w, "Duty agent is disabled", http.StatusServiceUnavailable)
		return
	}
	store := s.dutyAgent.Store()
	container := r.URL.Query().Get("container")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	tickets, total, err := store.List(r.Context(), duty.TicketFilter{
		Container: container,
		Limit:     limit,
		Offset:    offset,
	})
	if err != nil {
		log.Printf("[WARN] duty tickets list: %v", err)
		http.Error(w, "Failed to list tickets", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total":   total,
		"count":   len(tickets),
		"tickets": tickets,
	})
}

// handleDutyConfirm handles POST /api/duty/confirm
// Body: {ticket_id, op, id, confirm: true}
// This is the human-gated write path: it calls the existing docker.ContainerOp
// after verifying the admin token and deduplicating concurrent confirms.
func (s *Server) handleDutyConfirm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	started := time.Now()
	actorV, kindV, sourceV, ipV, uaV := audit.ActorFromRequest(r, "")
	ra, authed := s.checkAuthEx(w, r)
	if authed {
		actorV, kindV, sourceV, ipV, uaV = ra.auditActor(r)
	}
	if !authed {
		s.aud().Record(r.Context(), audit.Event{
			Time: time.Now().UTC(), Actor: actorV, ActorKind: kindV, Source: sourceV,
			Action: "duty_confirm", Result: audit.ResultDenied, StatusCode: http.StatusUnauthorized,
			Detail: "invalid or missing token on duty confirm", ClientIP: ipV, UserAgent: uaV,
		})
		return
	}

	var body struct {
		TicketID int64  `json:"ticket_id"`
		Op       string `json:"op"`
		ID       string `json:"id"`
		Confirm  bool   `json:"confirm"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if !body.Confirm {
		http.Error(w, "Confirmation required", http.StatusBadRequest)
		return
	}
	if body.Op != "start" && body.Op != "stop" && body.Op != "restart" {
		http.Error(w, "Invalid op", http.StatusBadRequest)
		return
	}
	if body.ID == "" {
		http.Error(w, "Missing container id", http.StatusBadRequest)
		return
	}
	if len(body.ID) > 128 {
		http.Error(w, "Container id too long", http.StatusBadRequest)
		return
	}

	// Deduplicate concurrent writes to the same container (409 strategy).
	s.dutyWriteMu.Lock()
	if s.dutyWrites[body.ID] {
		s.dutyWriteMu.Unlock()
		http.Error(w, "A write operation is already in progress for this container", http.StatusConflict)
		return
	}
	s.dutyWrites[body.ID] = true
	s.dutyWriteMu.Unlock()
	defer func() {
		s.dutyWriteMu.Lock()
		delete(s.dutyWrites, body.ID)
		s.dutyWriteMu.Unlock()
	}()

	s.mu.RLock()
	cli := s.dockerClient
	s.mu.RUnlock()
	if cli == nil {
		http.Error(w, "Docker client not available", http.StatusServiceUnavailable)
		return
	}

	correlationID := "duty_" + uuid.NewString()[:8]
	name := s.lookupName(body.ID)

	// Execute the write via the existing docker.ContainerOp.
	err := docker.ContainerOp(r.Context(), cli, body.ID, body.Op)
	if err != nil {
		log.Printf("[WARN] duty confirm %s on %s failed: %v", body.Op, body.ID, err)
		http.Error(w, "Failed to perform operation", http.StatusInternalServerError)
		s.aud().Record(r.Context(), audit.Event{
			Time: started, Actor: actorV, ActorKind: kindV, Source: sourceV,
			Action: body.Op, ContainerID: body.ID, ContainerName: name,
			Result: audit.ResultFailure, StatusCode: http.StatusInternalServerError,
			DurationMs: time.Since(started).Milliseconds(), Detail: "duty-confirmed " + body.Op + " failed",
			ClientIP: ipV, UserAgent: uaV, RequestID: correlationID,
		})
		return
	}

	// Update the duty ticket.
	if s.dutyAgent != nil && s.dutyAgent.Store() != nil && body.TicketID > 0 {
		_ = s.dutyAgent.Store().ConfirmWrite(r.Context(), body.TicketID, body.Op, body.ID, correlationID)
	}

	s.aud().Record(r.Context(), audit.Event{
		Time: started, Actor: actorV, ActorKind: kindV, Source: sourceV,
		Action: body.Op, ContainerID: body.ID, ContainerName: name,
		Result: audit.ResultSuccess, StatusCode: http.StatusOK,
		DurationMs: time.Since(started).Milliseconds(),
		Detail:     "duty-confirmed " + body.Op,
		ClientIP:   ipV, UserAgent: uaV, RequestID: correlationID,
	})

	writeJSON(w, http.StatusOK, map[string]string{
		"status":     "success",
		"op":         body.Op,
		"request_id": correlationID,
	})
}

// handleDutyConfig handles GET /api/duty/config (no auth; does not expose keys)
func (s *Server) handleDutyConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.dutyAgent == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":  true,
		"mode":     s.dutyAgent.Mode(),
		"model":    s.dutyAgent.ModelName(),
		"base_url": s.dutyAgent.BaseURL(),
		"has_key":  s.dutyAgent.Mode() == "live",
	})
}
