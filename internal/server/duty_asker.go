package server

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/zsuroy/dockerview-go/internal/audit"
	"github.com/zsuroy/dockerview-go/internal/duty"
)

// askRequest carries everything one inquiry needs, including the audit
// enrichment fields. The HTTP handler fills the client fields from the request;
// the long connection has no HTTP request and leaves them empty.
type askRequest struct {
	Question  string
	Actor     string
	ActorKind string
	Source    string
	ClientIP  string
	UserAgent string
	RequestID string
}

// askDuty is the single place a question reaches the duty agent. Both
// POST /api/duty/ask and the WeCom long connection call it, so the two entry
// points cannot drift apart — same actor resolution, same timeout budget, same
// "a proposed write is recorded, never executed" rule.
//
// It deliberately does not re-implement the agent: the body is one call to
// duty.Agent.Ask plus the audit line for a proposal.
func (s *Server) askDuty(ctx context.Context, req askRequest) (*duty.AskResult, error) {
	if s.dutyAgent == nil {
		return nil, duty.ErrDisabled
	}

	res, err := s.dutyAgent.Ask(ctx, req.Question, req.Actor, req.ActorKind, req.Source)
	if err != nil {
		return nil, err
	}

	// A proposed write is recorded, not executed. The actual write goes through
	// /api/duty/confirm after a human confirms with an admin token.
	if res.ProposedWrite != nil {
		requestID := req.RequestID
		if requestID == "" {
			requestID = "duty_" + uuid.NewString()[:8]
		}
		s.aud().Record(ctx, audit.Event{
			Time:          time.Now().UTC(),
			Actor:         req.Actor,
			ActorKind:     req.ActorKind,
			Source:        req.Source,
			Action:        "duty_propose_" + res.ProposedWrite.Op,
			ContainerID:   res.ProposedWrite.ID,
			ContainerName: res.ProposedWrite.Name,
			Result:        audit.ResultSuccess,
			StatusCode:    http.StatusOK,
			Detail:        "duty agent proposed " + res.ProposedWrite.Op + "; awaiting human confirm",
			ClientIP:      req.ClientIP,
			UserAgent:     req.UserAgent,
			RequestID:     requestID,
		})
		log.Printf("[INFO] duty: proposed %s on %s for %s (awaiting web confirm)",
			res.ProposedWrite.Op, res.ProposedWrite.Name, req.Actor)
	}

	return res, nil
}

// dutyAsker adapts the server to wecom.Asker, so the long-connection bridge
// reaches DUTY through exactly the same method the HTTP handler uses.
type dutyAsker struct{ s *Server }

// NewDutyAsker returns the wecom.Asker backed by this server's duty agent.
func NewDutyAsker(s *Server) *dutyAsker { return &dutyAsker{s: s} }

// Ask implements wecom.Asker.
func (a *dutyAsker) Ask(ctx context.Context, question, actor, actorKind, source string) (*duty.AskResult, error) {
	return a.s.askDuty(ctx, askRequest{
		Question:  question,
		Actor:     actor,
		ActorKind: actorKind,
		Source:    source,
	})
}
