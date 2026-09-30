package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zsuroy/dockerview-go/internal/audit"
	"github.com/zsuroy/dockerview-go/internal/duty"
	"github.com/zsuroy/dockerview-go/internal/wecom"
)

// scriptedAsker stands in for the DUTY brain. The long-connection tests use it
// so they can assert exactly what the bridge asked, without a model.
type scriptedAsker struct {
	mu    sync.Mutex
	calls []string
	res   *duty.AskResult
	err   error
}

func (a *scriptedAsker) Ask(_ context.Context, question, actor, actorKind, source string) (*duty.AskResult, error) {
	a.mu.Lock()
	a.calls = append(a.calls, question)
	res, err := a.res, a.err
	a.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if res == nil {
		return &duty.AskResult{Answer: "目前没有不健康的容器。"}, nil
	}
	return res, nil
}

func (a *scriptedAsker) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.calls)
}

// newWeComTestServer builds a server with a real *wecom.Bridge in mock mode.
// Mock mode is the point: no credentials, no socket, and the inject path still
// exercises the full handler.
func newWeComTestServer(t *testing.T, token string, ask wecom.Asker) (*Server, *wecom.Bridge) {
	t.Helper()
	t.Setenv("WECOM_BOT_ID", "")
	t.Setenv("WECOM_BOT_SECRET", "")

	cfg := wecom.DefaultConfig()
	cfg.Enabled = true

	b, err := wecom.New(cfg, ask, nil)
	if err != nil {
		t.Fatalf("wecom.New: %v", err)
	}
	if err := b.Start(context.Background()); err != nil {
		t.Fatalf("bridge.Start: %v", err)
	}
	t.Cleanup(func() { _ = b.Stop() })

	s := NewServer(nil, token, "v1", "abc", "now")
	s.SetWeComBridge(b)
	return s, b
}

func wecomReq(t *testing.T, s *Server, method, target, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r = httptest.NewRequest(method, target, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	if token != "" {
		r.Header.Set("X-Auth-Token", token)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

// recordingAuditer captures audit events so a test can assert the inject path
// is logged, including denials.
type recordingAuditer struct {
	mu     sync.Mutex
	events []audit.Event
}

func (r *recordingAuditer) Record(_ context.Context, e audit.Event) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
}

func (r *recordingAuditer) List(context.Context, audit.Query) (audit.Page, error) {
	return audit.Page{}, nil
}

func (r *recordingAuditer) Export(context.Context, audit.Query, string) ([]byte, string, error) {
	return nil, "", nil
}

func (r *recordingAuditer) Stats(context.Context) (audit.Stats, error) { return audit.Stats{}, nil }
func (r *recordingAuditer) Close() error                               { return nil }

func (r *recordingAuditer) hasAction(action string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e.Action == action {
			return true
		}
	}
	return false
}

func (r *recordingAuditer) actions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.events))
	for _, e := range r.events {
		out = append(out, e.Action+"("+e.Result+")")
	}
	return out
}

// ---------- state ----------

func TestWeComStateIsReadableWithoutAToken(t *testing.T) {
	s, _ := newWeComTestServer(t, "admin-token", &scriptedAsker{})

	w := wecomReq(t, s, http.MethodGet, "/api/wecom/state", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; the state view is read-only and guest-readable", w.Code)
	}

	var snap wecom.Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !snap.Enabled {
		t.Fatal("snapshot should report the integration as enabled")
	}
	if snap.Mode != string(wecom.ModeMock) {
		t.Fatalf("mode = %q, want mock with no credentials", snap.Mode)
	}
	if snap.State != string(wecom.StateMock) {
		t.Fatalf("state = %q, want mock", snap.State)
	}
	if snap.InjectAllowed {
		t.Fatal("inject_allowed must be false for an anonymous request")
	}
	if snap.HasSecret {
		t.Fatal("snapshot must not claim a secret is configured")
	}
}

func TestWeComStateReportsInjectAllowedForAdmins(t *testing.T) {
	s, _ := newWeComTestServer(t, "admin-token", &scriptedAsker{})

	w := wecomReq(t, s, http.MethodGet, "/api/wecom/state", "admin-token", nil)
	var snap wecom.Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if !snap.InjectAllowed {
		t.Fatal("inject_allowed should be true for the admin token")
	}
}

func TestWeComStateWithoutABridge(t *testing.T) {
	s := NewServer(nil, "admin-token", "v1", "abc", "now")

	w := wecomReq(t, s, http.MethodGet, "/api/wecom/state", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var snap wecom.Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Enabled {
		t.Fatal("a server with no bridge must report enabled=false")
	}
	if snap.State != string(wecom.StateDisconnected) {
		t.Fatalf("state = %q, want disconnected", snap.State)
	}
	if snap.Recent == nil {
		t.Fatal("recent must be an empty array, not null, so the console can map over it")
	}
}

func TestWeComStateNeverLeaksTheSecret(t *testing.T) {
	t.Setenv("WECOM_BOT_ID", "ww-secret-bot")
	t.Setenv("WECOM_BOT_SECRET", "super-secret-value")

	cfg := wecom.DefaultConfig()
	cfg.Enabled = true
	b, err := wecom.New(cfg, &scriptedAsker{}, nil)
	if err != nil {
		t.Fatalf("wecom.New: %v", err)
	}
	t.Cleanup(func() { _ = b.Stop() })

	s := NewServer(nil, "admin-token", "v1", "abc", "now")
	s.SetWeComBridge(b)

	for _, tok := range []string{"", "admin-token"} {
		w := wecomReq(t, s, http.MethodGet, "/api/wecom/state", tok, nil)
		body := w.Body.String()
		if strings.Contains(body, "super-secret-value") {
			t.Fatalf("state response leaks the secret (token=%q)", tok)
		}
		if strings.Contains(body, "ww-secret-bot") {
			t.Fatalf("state response leaks the raw bot id (token=%q)", tok)
		}
		var snap wecom.Snapshot
		if err := json.Unmarshal(w.Body.Bytes(), &snap); err != nil {
			t.Fatal(err)
		}
		if !snap.HasSecret {
			t.Fatal("has_secret should be true when credentials are present")
		}
		if snap.BotIDMasked != "ww-s****" {
			t.Fatalf("bot_id_masked = %q, want a masked id", snap.BotIDMasked)
		}
	}
}

func TestWeComStateRejectsNonGET(t *testing.T) {
	s, _ := newWeComTestServer(t, "admin-token", &scriptedAsker{})
	if w := wecomReq(t, s, http.MethodPost, "/api/wecom/state", "admin-token", nil); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", w.Code)
	}
}

// ---------- inject authorization ----------

func TestWeComInjectRefusedWithoutAToken(t *testing.T) {
	s, b := newWeComTestServer(t, "admin-token", &scriptedAsker{})

	w := wecomReq(t, s, http.MethodPost, "/api/wecom/inject", "", map[string]string{"text": "哪些不健康？"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a guest", w.Code)
	}
	if n := len(b.Transcript().Entries()); n != 0 {
		t.Fatalf("a rejected inject wrote %d transcript entries, want 0", n)
	}
}

func TestWeComInjectRefusedWithAWrongToken(t *testing.T) {
	s, _ := newWeComTestServer(t, "admin-token", &scriptedAsker{})

	w := wecomReq(t, s, http.MethodPost, "/api/wecom/inject", "not-the-token", map[string]string{"text": "hi"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestWeComInjectRefusedWhenNoTokenIsConfigured(t *testing.T) {
	// With no server token, checkAuthEx would call every caller an admin. A
	// guest and an admin are indistinguishable, so inject must be refused
	// outright rather than silently open.
	s, b := newWeComTestServer(t, "", &scriptedAsker{})

	w := wecomReq(t, s, http.MethodPost, "/api/wecom/inject", "", map[string]string{"text": "hi"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 when no admin token is configured", w.Code)
	}
	if !strings.Contains(w.Body.String(), "no admin token") {
		t.Fatalf("body = %q, want an explanation", w.Body.String())
	}
	if n := len(b.Transcript().Entries()); n != 0 {
		t.Fatalf("inject wrote %d entries despite being disabled, want 0", n)
	}
}

// ---------- inject behaviour ----------

func TestWeComInjectRunsTheQuestionAndRecordsTheReply(t *testing.T) {
	ask := &scriptedAsker{res: &duty.AskResult{Answer: "api 不健康，健康分 42。"}}
	s, b := newWeComTestServer(t, "admin-token", ask)

	w := wecomReq(t, s, http.MethodPost, "/api/wecom/inject", "admin-token",
		map[string]string{"text": "哪些容器不健康？", "user_id": "alice"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (the reply is produced asynchronously)", w.Code)
	}

	idleCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b.WaitIdle(idleCtx)

	if ask.count() != 1 {
		t.Fatalf("Ask called %d times, want 1", ask.count())
	}

	var sawInbound, sawReply bool
	for _, e := range b.Transcript().Entries() {
		if e.Kind == wecom.KindInbound && e.Source == wecom.SourceInject {
			sawInbound = true
		}
		if e.Kind == wecom.KindOutbound && strings.Contains(e.Text, "健康分 42") {
			sawReply = true
		}
	}
	if !sawInbound {
		t.Fatal("no inbound entry recorded for the inject")
	}
	if !sawReply {
		t.Fatal("no reply recorded for the inject")
	}
}

func TestWeComInjectRejectsEmptyText(t *testing.T) {
	s, _ := newWeComTestServer(t, "admin-token", &scriptedAsker{})

	w := wecomReq(t, s, http.MethodPost, "/api/wecom/inject", "admin-token", map[string]string{"text": "   "})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestWeComInjectRejectsOversizedText(t *testing.T) {
	s, _ := newWeComTestServer(t, "admin-token", &scriptedAsker{})

	w := wecomReq(t, s, http.MethodPost, "/api/wecom/inject", "admin-token",
		map[string]string{"text": strings.Repeat("x", wecom.MaxQuestionChars+1)})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestWeComInjectWithoutABridge(t *testing.T) {
	s := NewServer(nil, "admin-token", "v1", "abc", "now")

	w := wecomReq(t, s, http.MethodPost, "/api/wecom/inject", "admin-token", map[string]string{"text": "hi"})
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 when wecom is not installed", w.Code)
	}
}

func TestWeComInjectWritesAnAuditRow(t *testing.T) {
	rec := &recordingAuditer{}
	s, _ := newWeComTestServer(t, "admin-token", &scriptedAsker{})
	s.SetAuditer(rec)

	wecomReq(t, s, http.MethodPost, "/api/wecom/inject", "admin-token", map[string]string{"text": "哪些不健康？"})

	if !rec.hasAction("wecom_inject") {
		t.Fatalf("no wecom_inject audit row; got %v", rec.actions())
	}
}

func TestWeComInjectDenialIsAudited(t *testing.T) {
	rec := &recordingAuditer{}
	s, _ := newWeComTestServer(t, "admin-token", &scriptedAsker{})
	s.SetAuditer(rec)

	wecomReq(t, s, http.MethodPost, "/api/wecom/inject", "wrong", map[string]string{"text": "hi"})

	if !rec.hasAction("wecom_inject") {
		t.Fatal("a denied inject should still be audited")
	}
}

// ---------- welcome drill ----------

func TestWeComWelcomeDrillRequiresAnAdmin(t *testing.T) {
	s, _ := newWeComTestServer(t, "admin-token", &scriptedAsker{})

	if w := wecomReq(t, s, http.MethodPost, "/api/wecom/welcome", "", map[string]string{"user_id": "a"}); w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a guest", w.Code)
	}
}

func TestWeComWelcomeDrillRecordsTheWelcome(t *testing.T) {
	s, b := newWeComTestServer(t, "admin-token", &scriptedAsker{})

	w := wecomReq(t, s, http.MethodPost, "/api/wecom/welcome", "admin-token", map[string]string{"user_id": "alice"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}

	var found bool
	for _, e := range b.Transcript().Entries() {
		if e.Channel == wecom.ChannelWelcome {
			found = true
		}
	}
	if !found {
		t.Fatal("the welcome drill produced no welcome entry")
	}
}
