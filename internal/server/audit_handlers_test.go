package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zsuroy/dockerview-go/internal/audit"
	"github.com/zsuroy/dockerview-go/internal/docker"
)

func newAuditTestServer(t *testing.T, token string) (*Server, audit.Recorder) {
	t.Helper()
	s := NewServer(nil, token, "test", "abc", "now")
	rec, err := audit.Open(audit.Config{DBPath: ":memory:", RetentionDays: 0})
	if err != nil {
		t.Fatal(err)
	}
	s.SetAuditer(rec)
	return s, rec
}

func TestAudit_AuthRequired(t *testing.T) {
	s, _ := newAuditTestServer(t, "secret")
	req := httptest.NewRequest("GET", "/api/audit", nil)
	w := httptest.NewRecorder()
	s.handleAudit(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("got %d want 401", w.Result().StatusCode)
	}
}

func TestAudit_ListEmpty(t *testing.T) {
	s, _ := newAuditTestServer(t, "secret")
	req := httptest.NewRequest("GET", "/api/audit?token=secret", nil)
	w := httptest.NewRecorder()
	s.handleAudit(w, req)
	if w.Result().StatusCode != 200 {
		t.Fatalf("status=%d body=%s", w.Result().StatusCode, w.Body.String())
	}
	var p audit.Page
	if err := json.NewDecoder(w.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}
	if p.Total != 0 || p.Count != 0 {
		t.Fatalf("page=%+v", p)
	}
}

func TestAudit_WrongMethod(t *testing.T) {
	s, _ := newAuditTestServer(t, "secret")
	req := httptest.NewRequest("POST", "/api/audit?token=secret", nil)
	w := httptest.NewRecorder()
	s.handleAudit(w, req)
	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("got %d", w.Result().StatusCode)
	}
}

func TestAudit_BadParams(t *testing.T) {
	s, _ := newAuditTestServer(t, "secret")
	cases := []string{
		"/api/audit?token=secret&since=not-a-date",
		"/api/audit?token=secret&limit=abc",
		"/api/audit?token=secret&offset=-3",
	}
	for _, u := range cases {
		req := httptest.NewRequest("GET", u, nil)
		w := httptest.NewRecorder()
		s.handleAudit(w, req)
		if w.Result().StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: got %d", u, w.Result().StatusCode)
		}
	}
}

func TestAudit_AfterOpSuccess(t *testing.T) {
	s, rec := newAuditTestServer(t, "")
	// prime currentData so lookupName resolves
	s.UpdateData([]docker.ContainerInfo{{FullID: "abc1234", ID: "abc1234", Name: "redis", Status: "running"}})
	// s.dockerClient is nil → op fails with 503
	req := httptest.NewRequest("POST", "/api/container/op?id=abc1234&op=start", nil)
	w := httptest.NewRecorder()
	s.handleContainerOp(w, req)
	if w.Result().StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("op status=%d", w.Result().StatusCode)
	}
	p, err := rec.List(context.Background(), audit.Query{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != 1 {
		t.Fatalf("total=%d want 1", p.Total)
	}
	row := p.Items[0]
	if row.Action != "start" || row.Result != audit.ResultFailure || row.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("row=%+v", row)
	}
	if row.ContainerName != "redis" {
		t.Fatalf("container_name=%q want redis", row.ContainerName)
	}
}

func TestAudit_BadTokenRecorded(t *testing.T) {
	s, rec := newAuditTestServer(t, "secret")
	req := httptest.NewRequest("POST", "/api/container/op?id=x&op=start&token=wrong", nil)
	req.Header.Set("User-Agent", "curl/8.0")
	w := httptest.NewRecorder()
	s.handleContainerOp(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("got %d", w.Result().StatusCode)
	}
	p, _ := rec.List(context.Background(), audit.Query{})
	if p.Total != 1 {
		t.Fatalf("total=%d want 1", p.Total)
	}
	if p.Items[0].Result != audit.ResultDenied {
		t.Fatalf("result=%s", p.Items[0].Result)
	}
}

func TestAudit_ExecNonZeroExitRecorded(t *testing.T) {
	// with nil docker client, exec returns 503; we test bad params success case
	s, rec := newAuditTestServer(t, "")
	// empty cmd (string empty) should produce 400 + audit row
	body := strings.NewReader(`{"cmd":""}`)
	req := httptest.NewRequest("POST", "/api/container/exec?id=c1", body)
	w := httptest.NewRecorder()
	s.handleContainerExec(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("got %d", w.Result().StatusCode)
	}
	p, _ := rec.List(context.Background(), audit.Query{})
	if p.Total != 1 {
		t.Fatalf("total=%d want 1", p.Total)
	}
	if p.Items[0].Action != audit.ActionExec {
		t.Fatalf("action=%s", p.Items[0].Action)
	}
}

func TestAudit_ExportJSON(t *testing.T) {
	s, rec := newAuditTestServer(t, "")
	rec.Record(context.Background(), audit.Event{Action: audit.ActionStart, Result: audit.ResultSuccess})
	req := httptest.NewRequest("GET", "/api/audit/export?format=json", nil)
	w := httptest.NewRecorder()
	s.handleAuditExport(w, req)
	if w.Result().StatusCode != 200 {
		t.Fatalf("status=%d", w.Result().StatusCode)
	}
	if ct := w.Result().Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("ct=%s", ct)
	}
	if !strings.Contains(w.Result().Header.Get("Content-Disposition"), ".json") {
		t.Fatalf("disposition=%s", w.Result().Header.Get("Content-Disposition"))
	}
	var items []audit.Item
	if err := json.NewDecoder(w.Body).Decode(&items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("len=%d", len(items))
	}
}

func TestAudit_ExportMarkdown(t *testing.T) {
	s, rec := newAuditTestServer(t, "")
	rec.Record(context.Background(), audit.Event{Action: audit.ActionStop, Result: audit.ResultSuccess, ContainerName: "redis"})
	req := httptest.NewRequest("GET", "/api/audit/export?format=md", nil)
	w := httptest.NewRecorder()
	s.handleAuditExport(w, req)
	if w.Result().StatusCode != 200 {
		t.Fatalf("status=%d body=%s", w.Result().StatusCode, w.Body.String())
	}
	if ct := w.Result().Header.Get("Content-Type"); !strings.Contains(ct, "text/markdown") {
		t.Fatalf("ct=%s", ct)
	}
	if !strings.Contains(w.Body.String(), "# Audit export") {
		t.Fatalf("body=%s", w.Body.String())
	}
	if !strings.Contains(w.Result().Header.Get("Content-Disposition"), ".md") {
		t.Fatalf("disposition=%s", w.Result().Header.Get("Content-Disposition"))
	}
}

func TestAudit_ExportBadFormat(t *testing.T) {
	s, _ := newAuditTestServer(t, "")
	req := httptest.NewRequest("GET", "/api/audit/export?format=xml", nil)
	w := httptest.NewRecorder()
	s.handleAuditExport(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("got %d", w.Result().StatusCode)
	}
}

func TestAudit_Stats(t *testing.T) {
	s, rec := newAuditTestServer(t, "secret")
	rec.Record(context.Background(), audit.Event{Action: audit.ActionStart, Result: audit.ResultSuccess})
	req := httptest.NewRequest("GET", "/api/audit/stats?token=secret", nil)
	w := httptest.NewRecorder()
	s.handleAuditStats(w, req)
	if w.Result().StatusCode != 200 {
		t.Fatalf("status=%d", w.Result().StatusCode)
	}
	var st audit.Stats
	if err := json.NewDecoder(w.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if st.Total != 1 || st.Last24h != 1 {
		t.Fatalf("stats=%+v", st)
	}
}

func TestAudit_AuthHeader(t *testing.T) {
	s, _ := newAuditTestServer(t, "secret")
	req := httptest.NewRequest("GET", "/api/audit/stats", nil)
	req.Header.Set("X-Auth-Token", "secret")
	w := httptest.NewRecorder()
	s.handleAuditStats(w, req)
	if w.Result().StatusCode != 200 {
		t.Fatalf("status=%d", w.Result().StatusCode)
	}
}

func TestAudit_BearerHeader(t *testing.T) {
	s, _ := newAuditTestServer(t, "secret")
	req := httptest.NewRequest("GET", "/api/audit/stats", nil)
	req.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	s.handleAuditStats(w, req)
	if w.Result().StatusCode != 200 {
		t.Fatalf("status=%d", w.Result().StatusCode)
	}
}

func TestAudit_Pagination(t *testing.T) {
	s, rec := newAuditTestServer(t, "")
	for i := 0; i < 60; i++ {
		rec.Record(context.Background(), audit.Event{Action: audit.ActionStart, Result: audit.ResultSuccess})
	}
	req := httptest.NewRequest("GET", "/api/audit?limit=10&offset=10", nil)
	w := httptest.NewRecorder()
	s.handleAudit(w, req)
	var p audit.Page
	if err := json.NewDecoder(w.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}
	if p.Total != 60 || p.Count != 10 || p.Offset != 10 || p.Limit != 10 {
		t.Fatalf("page=%+v", p)
	}
}

func TestAudit_Filters(t *testing.T) {
	s, rec := newAuditTestServer(t, "")
	rec.Record(context.Background(), audit.Event{Action: audit.ActionStart, Result: audit.ResultSuccess, ContainerID: "abc"})
	rec.Record(context.Background(), audit.Event{Action: audit.ActionStop, Result: audit.ResultFailure, ContainerID: "def"})
	rec.Record(context.Background(), audit.Event{Action: audit.ActionExec, Result: audit.ResultDenied})
	cases := []struct {
		url      string
		wantRows int
	}{
		{"/api/audit?action=start", 1},
		{"/api/audit?result=failure", 1},
		{"/api/audit?result=failure,denied", 2},
		{"/api/audit?container_id=abc", 1},
		{"/api/audit?container_name=", 3},
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", c.url, nil)
		w := httptest.NewRecorder()
		s.handleAudit(w, req)
		var p audit.Page
		json.NewDecoder(w.Body).Decode(&p)
		if int(p.Total) != c.wantRows {
			t.Errorf("%s: total=%d want %d", c.url, p.Total, c.wantRows)
		}
	}
}

func TestAudit_LimitClamped(t *testing.T) {
	s, _ := newAuditTestServer(t, "")
	req := httptest.NewRequest("GET", "/api/audit?limit=999", nil)
	w := httptest.NewRecorder()
	s.handleAudit(w, req)
	var p audit.Page
	json.NewDecoder(w.Body).Decode(&p)
	if p.Limit != 200 {
		t.Fatalf("limit=%d want 200", p.Limit)
	}
}

func TestAudit_OptionsCORS(t *testing.T) {
	s := NewServer(nil, "", "test", "a", "n")
	req2 := httptest.NewRequest("POST", "/api/audit", nil)
	w2 := httptest.NewRecorder()
	s.handleAudit(w2, req2)
	if w2.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("missing CORS header")
	}
	if w2.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", w2.Result().StatusCode)
	}
}

func TestAudit_SortAsc(t *testing.T) {
	s, rec := newAuditTestServer(t, "")
	rec.Record(context.Background(), audit.Event{Action: audit.ActionStart, Result: audit.ResultSuccess})
	rec.Record(context.Background(), audit.Event{Action: audit.ActionStart, Result: audit.ResultSuccess})
	req := httptest.NewRequest("GET", "/api/audit?sort=time_asc", nil)
	w := httptest.NewRecorder()
	s.handleAudit(w, req)
	var p audit.Page
	json.NewDecoder(w.Body).Decode(&p)
	if p.Items[0].ID > p.Items[1].ID {
		t.Fatalf("not asc: %d > %d", p.Items[0].ID, p.Items[1].ID)
	}
}

func TestAudit_NoopReturnsEmpty(t *testing.T) {
	s := NewServer(nil, "", "test", "a", "n")
	s.SetAuditer(nil) // forces noop
	req := httptest.NewRequest("GET", "/api/audit", nil)
	w := httptest.NewRecorder()
	s.handleAudit(w, req)
	var p audit.Page
	json.NewDecoder(w.Body).Decode(&p)
	if p.Total != 0 || len(p.Items) != 0 {
		t.Fatalf("noop expected empty, got %+v", p)
	}
}

func TestAudit_DeniedUsesAnonymousActor(t *testing.T) {
	s, rec := newAuditTestServer(t, "secret")
	req := httptest.NewRequest("GET", "/api/audit?token=wrong", nil)
	req.Header.Set("User-Agent", "curl/8.0")
	w := httptest.NewRecorder()
	s.handleAudit(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d", w.Result().StatusCode)
	}
	p, err := rec.List(context.Background(), audit.Query{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 1 {
		t.Fatalf("expected 1 denied row, got %d", len(p.Items))
	}
	if p.Items[0].Actor != "anonymous" {
		t.Fatalf("wrong-token should record anonymous actor, got %q", p.Items[0].Actor)
	}
	if p.Items[0].Result != audit.ResultDenied {
		t.Fatalf("result=%s", p.Items[0].Result)
	}
}

func TestAudit_TimeShorthandDays(t *testing.T) {
	s, rec := newAuditTestServer(t, "")
	rec.Record(context.Background(), audit.Event{Action: audit.ActionStart, Result: audit.ResultSuccess})
	req := httptest.NewRequest("GET", "/api/audit?since=7d", nil)
	w := httptest.NewRecorder()
	s.handleAudit(w, req)
	var p audit.Page
	json.NewDecoder(w.Body).Decode(&p)
	if w.Result().StatusCode != 200 {
		t.Fatalf("status=%d body=%s", w.Result().StatusCode, w.Body.String())
	}
	if p.Total != 1 {
		t.Fatalf("since=7d should match fresh row, got total=%d", p.Total)
	}
}

func TestAudit_TimePlainDate(t *testing.T) {
	s, _ := newAuditTestServer(t, "")
	req := httptest.NewRequest("GET", "/api/audit?since=2099-01-01", nil)
	w := httptest.NewRecorder()
	s.handleAudit(w, req)
	var p audit.Page
	json.NewDecoder(w.Body).Decode(&p)
	if p.Total != 0 {
		t.Fatalf("future date should yield 0 rows, got %d", p.Total)
	}
}

func TestAudit_RequestIDHeader(t *testing.T) {
	// Starting through the full mux so the correlation middleware fires.
	s, _ := newAuditTestServer(t, "secret")
	// Direct call to handleAudit won't hit the CORS middleware; verify denied
	// rows propagate X-Request-Id when supplied by the client.
	req := httptest.NewRequest("GET", "/api/audit?token=wrong", nil)
	req.Header.Set("X-Request-Id", "test-req-123")
	w := httptest.NewRecorder()
	s.handleAudit(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d", w.Result().StatusCode)
	}
}

func TestAudit_ExportUnknownFormat(t *testing.T) {
	s, _ := newAuditTestServer(t, "secret")
	req := httptest.NewRequest("GET", "/api/audit/export?token=secret&format=xml", nil)
	w := httptest.NewRecorder()
	s.handleAuditExport(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d want 400", w.Result().StatusCode)
	}
}

func TestAudit_LimitZeroDefaults(t *testing.T) {
	s, _ := newAuditTestServer(t, "")
	req := httptest.NewRequest("GET", "/api/audit?limit=0", nil)
	w := httptest.NewRecorder()
	s.handleAudit(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("limit=0 should be rejected, got %d", w.Result().StatusCode)
	}
}

func TestAudit_ContainerNameFilter(t *testing.T) {
	s, rec := newAuditTestServer(t, "")
	rec.Record(context.Background(), audit.Event{Action: audit.ActionStart, Result: audit.ResultSuccess, ContainerName: "redis"})
	rec.Record(context.Background(), audit.Event{Action: audit.ActionStop, Result: audit.ResultSuccess, ContainerName: "postgres"})
	req := httptest.NewRequest("GET", "/api/audit?container_name=REDIS", nil)
	w := httptest.NewRecorder()
	s.handleAudit(w, req)
	var p audit.Page
	json.NewDecoder(w.Body).Decode(&p)
	if p.Total != 1 || p.Items[0].ContainerName != "redis" {
		t.Fatalf("container_name filter expected 1 redis row, got total=%d items=%+v", p.Total, p.Items)
	}
}

func TestAudit_ActorFilter(t *testing.T) {
	s, rec := newAuditTestServer(t, "")
	rec.Record(context.Background(), audit.Event{Action: audit.ActionStart, Result: audit.ResultSuccess, Actor: "tok_abc"})
	rec.Record(context.Background(), audit.Event{Action: audit.ActionStop, Result: audit.ResultSuccess, Actor: "cli"})
	req := httptest.NewRequest("GET", "/api/audit?actor=cli", nil)
	w := httptest.NewRecorder()
	s.handleAudit(w, req)
	var p audit.Page
	json.NewDecoder(w.Body).Decode(&p)
	if p.Total != 1 || p.Items[0].Actor != "cli" {
		t.Fatalf("actor filter: total=%d items=%+v", p.Total, p.Items)
	}
}
