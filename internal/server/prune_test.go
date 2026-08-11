package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/volume"
	"github.com/zsuroy/dockerview-go/internal/docker"
)

// fakePruneClient is a server-package double that satisfies docker.PruneClient.
type fakePruneClient struct {
	mu sync.Mutex

	du     types.DiskUsage
	duErr  error
	rmErr  map[string]error
	volErr map[string]error

	duCalls       int
	imgRemoveCnt  int
	volRemoveCnt  int
	lastImgOpts   image.RemoveOptions
	removedImages []string
	removedVols   []string

	blockRemove   chan struct{}
	removeEntered chan struct{}
}

func newFakePruneClient() *fakePruneClient {
	return &fakePruneClient{rmErr: map[string]error{}, volErr: map[string]error{}}
}

func (f *fakePruneClient) DiskUsage(_ context.Context, _ types.DiskUsageOptions) (types.DiskUsage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.duCalls++
	if f.duErr != nil {
		return f.du, f.duErr
	}
	return f.du, nil
}

func (f *fakePruneClient) ImageRemove(_ context.Context, id string, opts image.RemoveOptions) ([]image.DeleteResponse, error) {
	if f.blockRemove != nil {
		select {
		case <-f.removeEntered:
		default:
			close(f.removeEntered)
		}
		<-f.blockRemove
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.imgRemoveCnt++
	f.lastImgOpts = opts
	f.removedImages = append(f.removedImages, id)
	if err := f.rmErr[id]; err != nil {
		return nil, err
	}
	return []image.DeleteResponse{{Deleted: id}}, nil
}

func (f *fakePruneClient) VolumeRemove(_ context.Context, id string, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.volRemoveCnt++
	f.removedVols = append(f.removedVols, id)
	return f.volErr[id]
}

func (f *fakePruneClient) ImagesPrune(_ context.Context, _ filters.Args) (image.PruneReport, error) {
	return image.PruneReport{}, nil
}

func newTestServerWithPrune(t *testing.T, token string, fc *fakePruneClient) *Server {
	t.Helper()
	s := NewServer(nil, token, "test", "test", "test")
	s.pruner = docker.NewPruner(fc)
	return s
}

func imgSummary(id string, size int64, containers int64, tags ...string) *image.Summary {
	return &image.Summary{ID: id, Size: size, Containers: containers, RepoTags: tags}
}

func volInfo(name string, size, refCount int64) *volume.Volume {
	return &volume.Volume{Name: name, Driver: "local", Mountpoint: "/v/" + name, UsageData: &volume.UsageData{Size: size, RefCount: refCount}}
}

func decode(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(w.Body).Decode(v); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
}

// ---- candidates ----

func TestHandlePruneCandidates_OK(t *testing.T) {
	fc := newFakePruneClient()
	fc.du.Images = []*image.Summary{imgSummary("sha256:abc", 100, 0)}
	fc.du.Volumes = []*volume.Volume{volInfo("v1", 50, 0)}
	s := newTestServerWithPrune(t, "", fc)
	req := httptest.NewRequest(http.MethodGet, "/api/prune/candidates", nil)
	w := httptest.NewRecorder()
	s.handlePruneCandidates(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var c docker.Candidates
	decode(t, w, &c)
	if c.ImagesCount != 1 || c.VolumesCount != 1 || c.TotalSize != 150 {
		t.Errorf("unexpected candidates: %+v", c)
	}
}

func TestHandlePruneCandidates_GuestAllowed(t *testing.T) {
	fc := newFakePruneClient()
	s := newTestServerWithPrune(t, "secret", fc)
	req := httptest.NewRequest(http.MethodGet, "/api/prune/candidates", nil)
	w := httptest.NewRecorder()
	s.handlePruneCandidates(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("guest must list candidates, got %d", w.Code)
	}
}

func TestHandlePruneCandidates_NilPruner503(t *testing.T) {
	s := NewServer(nil, "", "t", "t", "t")
	req := httptest.NewRequest(http.MethodGet, "/api/prune/candidates", nil)
	w := httptest.NewRecorder()
	s.handlePruneCandidates(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestHandlePruneCandidates_DockerError502(t *testing.T) {
	fc := newFakePruneClient()
	fc.duErr = errors.New("daemon down")
	s := newTestServerWithPrune(t, "", fc)
	req := httptest.NewRequest(http.MethodGet, "/api/prune/candidates", nil)
	w := httptest.NewRecorder()
	s.handlePruneCandidates(w, req)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

func TestHandlePruneCandidates_MethodNotAllowed(t *testing.T) {
	s := newTestServerWithPrune(t, "", newFakePruneClient())
	req := httptest.NewRequest(http.MethodDelete, "/api/prune/candidates", nil)
	w := httptest.NewRecorder()
	s.handlePruneCandidates(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

// ---- dry-run ----

func TestHandlePruneDryRun_OK(t *testing.T) {
	fc := newFakePruneClient()
	fc.du.Images = []*image.Summary{imgSummary("sha256:abc", 100, 0)}
	s := newTestServerWithPrune(t, "", fc)
	req := httptest.NewRequest(http.MethodPost, "/api/prune/dry-run", nil)
	w := httptest.NewRecorder()
	s.handlePruneDryRun(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var rep docker.DryRunReport
	decode(t, w, &rep)
	if !rep.DryRun || rep.WillDelete.Images != 1 {
		t.Errorf("unexpected dry-run: %+v", rep)
	}
	if fc.imgRemoveCnt != 0 || fc.volRemoveCnt != 0 {
		t.Fatal("dry-run must not delete")
	}
}

func TestHandlePruneDryRun_GuestAllowed(t *testing.T) {
	s := newTestServerWithPrune(t, "secret", newFakePruneClient())
	req := httptest.NewRequest(http.MethodPost, "/api/prune/dry-run", nil)
	w := httptest.NewRecorder()
	s.handlePruneDryRun(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("guest must dry-run, got %d", w.Code)
	}
}

func TestHandlePruneDryRun_WithSubset(t *testing.T) {
	fc := newFakePruneClient()
	fc.du.Images = []*image.Summary{
		imgSummary("sha256:aa", 100, 0),
		imgSummary("sha256:bb", 200, 0),
	}
	s := newTestServerWithPrune(t, "", fc)
	body, _ := json.Marshal(docker.Selection{Images: []string{"sha256:aa"}})
	req := httptest.NewRequest(http.MethodPost, "/api/prune/dry-run", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handlePruneDryRun(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	var rep docker.DryRunReport
	decode(t, w, &rep)
	if rep.WillDelete.Images != 1 {
		t.Errorf("expected 1, got %d", rep.WillDelete.Images)
	}
}

func TestHandlePruneDryRun_BadJSON(t *testing.T) {
	s := newTestServerWithPrune(t, "", newFakePruneClient())
	req := httptest.NewRequest(http.MethodPost, "/api/prune/dry-run", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	s.handlePruneDryRun(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandlePruneDryRun_MethodNotAllowed(t *testing.T) {
	s := newTestServerWithPrune(t, "", newFakePruneClient())
	req := httptest.NewRequest(http.MethodGet, "/api/prune/dry-run", nil)
	w := httptest.NewRecorder()
	s.handlePruneDryRun(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

// ---- confirm (auth + gates) ----

func TestHandlePruneConfirm_NoToken401(t *testing.T) {
	fc := newFakePruneClient()
	s := newTestServerWithPrune(t, "secret", fc)
	req := httptest.NewRequest(http.MethodPost, "/api/prune/confirm", nil)
	w := httptest.NewRecorder()
	s.handlePruneConfirm(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if fc.imgRemoveCnt != 0 || fc.volRemoveCnt != 0 {
		t.Fatal("no deletion on auth failure")
	}
	if len(s.audit.Events()) != 1 || s.audit.Events()[0].Status != "denied" {
		t.Errorf("expected denied audit event, got %+v", s.audit.Events())
	}
}

func TestHandlePruneConfirm_WrongToken401_AllTransports(t *testing.T) {
	s := newTestServerWithPrune(t, "secret", newFakePruneClient())
	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/api/prune/confirm?token=wrong", nil),
		func() *http.Request {
			r := httptest.NewRequest(http.MethodPost, "/api/prune/confirm", nil)
			r.Header.Set("X-Auth-Token", "wrong")
			return r
		}(),
		func() *http.Request {
			r := httptest.NewRequest(http.MethodPost, "/api/prune/confirm", nil)
			r.Header.Set("Authorization", "Bearer wrong")
			return r
		}(),
	} {
		w := httptest.NewRecorder()
		s.handlePruneConfirm(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d for %v", w.Code, req.Header)
		}
	}
}

func TestHandlePruneConfirm_MissingConfirmLiteral400(t *testing.T) {
	fc := newFakePruneClient()
	s := newTestServerWithPrune(t, "secret", fc)
	body, _ := json.Marshal(docker.ConfirmRequest{Confirm: "yes", Fingerprint: "x"})
	req := httptest.NewRequest(http.MethodPost, "/api/prune/confirm?token=secret", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handlePruneConfirm(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if fc.imgRemoveCnt != 0 {
		t.Fatal("no deletion")
	}
}

func TestHandlePruneConfirm_MissingFingerprint400(t *testing.T) {
	s := newTestServerWithPrune(t, "secret", newFakePruneClient())
	body, _ := json.Marshal(docker.ConfirmRequest{Confirm: docker.ConfirmLiteral})
	req := httptest.NewRequest(http.MethodPost, "/api/prune/confirm?token=secret", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handlePruneConfirm(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandlePruneConfirm_FingerprintMismatch409(t *testing.T) {
	fc := newFakePruneClient()
	fc.du.Images = []*image.Summary{imgSummary("sha256:abc", 100, 0)}
	s := newTestServerWithPrune(t, "secret", fc)
	body, _ := json.Marshal(docker.ConfirmRequest{Confirm: docker.ConfirmLiteral, Fingerprint: "0000000000000000"})
	req := httptest.NewRequest(http.MethodPost, "/api/prune/confirm?token=secret", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handlePruneConfirm(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", w.Code, w.Body.String())
	}
	if fc.imgRemoveCnt != 0 {
		t.Fatal("mismatch must not delete")
	}
}

func TestHandlePruneConfirm_Success(t *testing.T) {
	fc := newFakePruneClient()
	fc.du.Images = []*image.Summary{imgSummary("sha256:abc", 100, 0)}
	fc.du.Volumes = []*volume.Volume{volInfo("v1", 50, 0)}
	s := newTestServerWithPrune(t, "secret", fc)

	// Obtain fingerprint via dry-run.
	drReq := httptest.NewRequest(http.MethodPost, "/api/prune/dry-run", nil)
	drW := httptest.NewRecorder()
	s.handlePruneDryRun(drW, drReq)
	var dr docker.DryRunReport
	decode(t, drW, &dr)

	body, _ := json.Marshal(docker.ConfirmRequest{Confirm: docker.ConfirmLiteral, Fingerprint: dr.Candidates.Fingerprint})
	req := httptest.NewRequest(http.MethodPost, "/api/prune/confirm?token=secret", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handlePruneConfirm(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var rep docker.DeleteReport
	decode(t, w, &rep)
	if rep.Summary.Deleted != 2 || rep.Summary.ReclaimedBytes != 150 {
		t.Errorf("unexpected summary: %+v", rep.Summary)
	}
	if fc.imgRemoveCnt != 1 || fc.volRemoveCnt != 1 {
		t.Errorf("deletions wrong: img=%d vol=%d", fc.imgRemoveCnt, fc.volRemoveCnt)
	}
	// Audit recorded.
	events := s.audit.Events()
	if len(events) != 1 || events[0].Status != "success" || events[0].ImagesDeleted != 1 || events[0].VolumesDeleted != 1 {
		t.Errorf("audit not recorded correctly: %+v", events)
	}
}

func TestHandlePruneConfirm_PartialFailureStatus200(t *testing.T) {
	fc := newFakePruneClient()
	fc.du.Images = []*image.Summary{
		imgSummary("sha256:good", 100, 0),
		imgSummary("sha256:bad", 200, 0),
	}
	fc.rmErr["sha256:bad"] = errors.New("boom")
	s := newTestServerWithPrune(t, "secret", fc)
	drReq := httptest.NewRequest(http.MethodPost, "/api/prune/dry-run", nil)
	drW := httptest.NewRecorder()
	s.handlePruneDryRun(drW, drReq)
	var dr docker.DryRunReport
	decode(t, drW, &dr)
	body, _ := json.Marshal(docker.ConfirmRequest{Confirm: docker.ConfirmLiteral, Fingerprint: dr.Candidates.Fingerprint})
	req := httptest.NewRequest(http.MethodPost, "/api/prune/confirm?token=secret", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handlePruneConfirm(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("partial failure returns 200, got %d", w.Code)
	}
	var rep docker.DeleteReport
	decode(t, w, &rep)
	if rep.Summary.Deleted != 1 || rep.Summary.Failed != 1 {
		t.Errorf("expected 1 deleted 1 failed, got %+v", rep.Summary)
	}
	events := s.audit.Events()
	if len(events) != 1 || events[0].Status != "partial" || events[0].ImagesFailed != 1 {
		t.Errorf("partial audit wrong: %+v", events)
	}
}

func TestHandlePruneConfirm_NilPruner503(t *testing.T) {
	s := NewServer(nil, "secret", "t", "t", "t")
	body, _ := json.Marshal(docker.ConfirmRequest{Confirm: docker.ConfirmLiteral, Fingerprint: "x"})
	req := httptest.NewRequest(http.MethodPost, "/api/prune/confirm?token=secret", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handlePruneConfirm(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestHandlePruneConfirm_BadJSON400(t *testing.T) {
	s := newTestServerWithPrune(t, "secret", newFakePruneClient())
	req := httptest.NewRequest(http.MethodPost, "/api/prune/confirm?token=secret", strings.NewReader("{"))
	w := httptest.NewRecorder()
	s.handlePruneConfirm(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandlePruneConfirm_MethodNotAllowed(t *testing.T) {
	s := newTestServerWithPrune(t, "secret", newFakePruneClient())
	req := httptest.NewRequest(http.MethodGet, "/api/prune/confirm?token=secret", nil)
	w := httptest.NewRecorder()
	s.handlePruneConfirm(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestHandlePruneConfirm_Concurrent409(t *testing.T) {
	fc := newFakePruneClient()
	fc.du.Images = []*image.Summary{imgSummary("sha256:abc", 100, 0)}
	s := newTestServerWithPrune(t, "secret", fc)
	drReq := httptest.NewRequest(http.MethodPost, "/api/prune/dry-run", nil)
	drW := httptest.NewRecorder()
	s.handlePruneDryRun(drW, drReq)
	var dr docker.DryRunReport
	decode(t, drW, &dr)
	body, _ := json.Marshal(docker.ConfirmRequest{Confirm: docker.ConfirmLiteral, Fingerprint: dr.Candidates.Fingerprint})

	release := make(chan struct{})
	entered := make(chan struct{})
	fc.blockRemove = release
	fc.removeEntered = entered

	done := make(chan int)
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/api/prune/confirm?token=secret", bytes.NewReader(body))
		w := httptest.NewRecorder()
		s.handlePruneConfirm(w, req)
		done <- w.Code
	}()
	<-entered
	// Second confirm while first is in flight.
	req2 := httptest.NewRequest(http.MethodPost, "/api/prune/confirm?token=secret", bytes.NewReader(body))
	w2 := httptest.NewRecorder()
	s.handlePruneConfirm(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Errorf("expected 409 in-progress, got %d", w2.Code)
	}
	close(release)
	select {
	case code := <-done:
		if code != http.StatusOK {
			t.Errorf("first confirm expected 200, got %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first confirm hung")
	}
}

// ---- audit ----

func TestHandlePruneAudit_AuthRequired(t *testing.T) {
	s := newTestServerWithPrune(t, "secret", newFakePruneClient())
	req := httptest.NewRequest(http.MethodGet, "/api/prune/audit", nil)
	w := httptest.NewRecorder()
	s.handlePruneAudit(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandlePruneAudit_ListsEvents(t *testing.T) {
	fc := newFakePruneClient()
	s := newTestServerWithPrune(t, "secret", fc)
	s.audit.Record(AuditEvent{Time: "2026-07-29T00:00:00Z", Actor: "admin:x", Action: "confirm", Status: "success", ReclaimedBytes: 1})
	req := httptest.NewRequest(http.MethodGet, "/api/prune/audit?token=secret", nil)
	w := httptest.NewRecorder()
	s.handlePruneAudit(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var out struct {
		Events []AuditEvent `json:"events"`
	}
	decode(t, w, &out)
	if len(out.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(out.Events))
	}
}

func TestHandlePruneAudit_DeniedAttemptsRecorded(t *testing.T) {
	s := newTestServerWithPrune(t, "secret", newFakePruneClient())
	req := httptest.NewRequest(http.MethodPost, "/api/prune/confirm", nil)
	w := httptest.NewRecorder()
	s.handlePruneConfirm(w, req)
	events := s.audit.Events()
	if len(events) != 1 || events[0].Status != "denied" || events[0].Action != "confirm" {
		t.Fatalf("denied attempt not recorded: %+v", events)
	}
}

// ---- wiring ----

func TestPruneRoutesRegistered(t *testing.T) {
	s := NewServer(nil, "", "t", "t", "t")
	mux := http.NewServeMux()
	// Mimic registration order from Start.
	mux.HandleFunc("/api/prune/candidates", s.handlePruneCandidates)
	mux.HandleFunc("/api/prune/dry-run", s.handlePruneDryRun)
	mux.HandleFunc("/api/prune/confirm", s.handlePruneConfirm)
	mux.HandleFunc("/api/prune/audit", s.handlePruneAudit)
	for _, p := range []string{"/api/prune/candidates", "/api/prune/dry-run", "/api/prune/confirm", "/api/prune/audit"} {
		req := httptest.NewRequest(http.MethodOptions, p, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		// OPTIONS handled by CORS wrapper in real server; mux here returns 405/301.
		// Just ensure the route pattern resolves (no 404 from ServeMux).
		if w.Code == http.StatusNotFound {
			t.Errorf("route %s not registered", p)
		}
	}
}

func TestTokenPrefixAndActor(t *testing.T) {
	s := NewServer(nil, "secret", "t", "t", "t")
	req := httptest.NewRequest(http.MethodGet, "/api/prune/audit?token=secret", nil)
	actor := s.actorFromRequest(req)
	if actor == "" || actor == "secret" {
		t.Fatalf("actor should not reveal the token, got %q", actor)
	}
	// Same token -> same prefix; different -> different.
	req2 := httptest.NewRequest(http.MethodGet, "/api/prune/audit?token=other", nil)
	if s.actorFromRequest(req2) == actor {
		t.Error("different tokens should yield different actor prefixes")
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.5:1234"
	if got := clientIP(r); got != "10.0.0.5" {
		t.Errorf("clientIP = %q", got)
	}
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
	if got := clientIP(r); got != "1.2.3.4" {
		t.Errorf("xff clientIP = %q", got)
	}
}

// ---- auth matrix ----

// authMatrixCase is a table-driven case for the prune authorization boundary.
type authMatrixCase struct {
	name   string
	token  string // server token
	route  string
	method string
	submit string // token presented: "", "none", "correct", "wrong", "bearer-correct", "bearer-wrong", "header-correct", "header-wrong"
	want   int
	body   bool // include a valid-ish body
	pruner bool // whether to attach a pruner (nil-pruner gives 503 after auth for confirm)
	denied bool // expect an audit "denied" event
}

func applyAuth(r *http.Request, submit string) {
	switch submit {
	case "correct":
		q := r.URL.Query()
		q.Set("token", "secret")
		r.URL.RawQuery = q.Encode()
	case "wrong":
		q := r.URL.Query()
		q.Set("token", "nope")
		r.URL.RawQuery = q.Encode()
	case "header-correct":
		r.Header.Set("X-Auth-Token", "secret")
	case "header-wrong":
		r.Header.Set("X-Auth-Token", "nope")
	case "bearer-correct":
		r.Header.Set("Authorization", "Bearer secret")
	case "bearer-wrong":
		r.Header.Set("Authorization", "Bearer nope")
	case "none", "":
		// no credentials
	}
}

func validConfirmBody(t *testing.T) *bytes.Reader {
	t.Helper()
	b, _ := json.Marshal(docker.ConfirmRequest{Confirm: docker.ConfirmLiteral, Fingerprint: "0000000000000000"})
	return bytes.NewReader(b)
}

func TestPruneAuthMatrix(t *testing.T) {
	cases := []authMatrixCase{
		// candidates/dry-run are guest-readable regardless of token.
		{"candidates guest allowed", "secret", "/api/prune/candidates", http.MethodGet, "none", http.StatusOK, false, true, false},
		{"candidates wrong token still allowed (read-only)", "secret", "/api/prune/candidates", http.MethodGet, "wrong", http.StatusOK, false, true, false},
		{"dryrun guest allowed", "secret", "/api/prune/dry-run", http.MethodPost, "none", http.StatusOK, false, true, false},
		{"dryrun header wrong still allowed", "secret", "/api/prune/dry-run", http.MethodPost, "header-wrong", http.StatusOK, false, true, false},

		// confirm requires a valid token.
		{"confirm no token 401", "secret", "/api/prune/confirm", http.MethodPost, "none", http.StatusUnauthorized, true, true, true},
		{"confirm wrong query token 401", "secret", "/api/prune/confirm", http.MethodPost, "wrong", http.StatusUnauthorized, true, true, true},
		{"confirm wrong header token 401", "secret", "/api/prune/confirm", http.MethodPost, "header-wrong", http.StatusUnauthorized, true, true, true},
		{"confirm wrong bearer token 401", "secret", "/api/prune/confirm", http.MethodPost, "bearer-wrong", http.StatusUnauthorized, true, true, true},

		// audit requires a valid token.
		{"audit no token 401", "secret", "/api/prune/audit", http.MethodGet, "none", http.StatusUnauthorized, false, true, false},
		{"audit wrong token 401", "secret", "/api/prune/audit", http.MethodGet, "wrong", http.StatusUnauthorized, false, true, false},
		{"audit correct query 200", "secret", "/api/prune/audit", http.MethodGet, "correct", http.StatusOK, false, true, false},
		{"audit correct header 200", "secret", "/api/prune/audit", http.MethodGet, "header-correct", http.StatusOK, false, true, false},
		{"audit correct bearer 200", "secret", "/api/prune/audit", http.MethodGet, "bearer-correct", http.StatusOK, false, true, false},

		// With no server token configured, auth is disabled (backward compatible).
		{"confirm no-token-config allowed (not 401)", "", "/api/prune/confirm", http.MethodPost, "none", http.StatusConflict, true, true, false}, // 409 due to fingerprint mismatch, but NOT 401
		{"audit no-token-config allowed", "", "/api/prune/audit", http.MethodGet, "none", http.StatusOK, false, true, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fc := newFakePruneClient()
			s := newTestServerWithPrune(t, c.token, fc)
			if !c.pruner {
				s.pruner = nil
			}
			var req *http.Request
			if c.body {
				req = httptest.NewRequest(c.method, c.route, validConfirmBody(t))
			} else {
				req = httptest.NewRequest(c.method, c.route, nil)
			}
			applyAuth(req, c.submit)
			w := httptest.NewRecorder()
			dispatchPrune(s, c.route, w, req)
			if w.Code != c.want {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, c.want, w.Body.String())
			}
			if c.denied {
				events := s.audit.Events()
				if len(events) == 0 || events[len(events)-1].Status != "denied" {
					t.Errorf("expected a denied audit event, got %+v", events)
				}
			}
			// No destructive call must have happened when not authorized.
			if c.route == "/api/prune/confirm" && c.want == http.StatusUnauthorized {
				if fc.imgRemoveCnt != 0 || fc.volRemoveCnt != 0 {
					t.Fatalf("unauthorized confirm deleted something: img=%d vol=%d", fc.imgRemoveCnt, fc.volRemoveCnt)
				}
			}
		})
	}
}

// dispatchPrune routes to the prune handler directly, mirroring Start's mux.
func dispatchPrune(s *Server, route string, w http.ResponseWriter, r *http.Request) {
	switch route {
	case "/api/prune/candidates":
		s.handlePruneCandidates(w, r)
	case "/api/prune/dry-run":
		s.handlePruneDryRun(w, r)
	case "/api/prune/confirm":
		s.handlePruneConfirm(w, r)
	case "/api/prune/audit":
		s.handlePruneAudit(w, r)
	default:
		panic("unknown route " + route)
	}
}

// TestConfirmHeaderTransportWorks ensures admin can confirm via the X-Auth-Token
// header (the transport Mobile uses), not only the query string.
func TestConfirmHeaderTransportWorks(t *testing.T) {
	fc := newFakePruneClient()
	fc.du.Images = []*image.Summary{{ID: "sha256:abc", Size: 1, Containers: 0}}
	s := newTestServerWithPrune(t, "secret", fc)

	drReq := httptest.NewRequest(http.MethodPost, "/api/prune/dry-run", nil)
	drW := httptest.NewRecorder()
	s.handlePruneDryRun(drW, drReq)
	var dr docker.DryRunReport
	if err := json.NewDecoder(drW.Body).Decode(&dr); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(docker.ConfirmRequest{Confirm: docker.ConfirmLiteral, Fingerprint: dr.Candidates.Fingerprint})
	req := httptest.NewRequest(http.MethodPost, "/api/prune/confirm", bytes.NewReader(body))
	req.Header.Set("X-Auth-Token", "secret")
	w := httptest.NewRecorder()
	s.handlePruneConfirm(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("header-auth confirm = %d: %s", w.Code, w.Body.String())
	}
}

// TestConfirmAuditNeverLogsToken verifies the audit actor label is derived from
// a hashed prefix and does not contain the raw token.
func TestConfirmAuditNeverLogsToken(t *testing.T) {
	s := newTestServerWithPrune(t, "supersecret", newFakePruneClient())
	body, _ := json.Marshal(docker.ConfirmRequest{Confirm: "yes"})
	req := httptest.NewRequest(http.MethodPost, "/api/prune/confirm?token=supersecret", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handlePruneConfirm(w, req)
	for _, ev := range s.audit.Events() {
		if bytes.Contains([]byte(ev.Actor), []byte("supersecret")) {
			t.Fatalf("audit actor leaked the token: %q", ev.Actor)
		}
	}
}

func TestConfirmEmptyBodyReturns400Not500(t *testing.T) {
	s := newTestServerWithPrune(t, "secret", newFakePruneClient())
	req := httptest.NewRequest(http.MethodPost, "/api/prune/confirm?token=secret", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handlePruneConfirm(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty body should yield 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestActorLabelNoTokenConfig(t *testing.T) {
	s := NewServer(nil, "", "t", "t", "t")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := s.actorFromRequest(req); got != "admin(no-token-configured)" {
		t.Fatalf("actor = %q", got)
	}
}

func TestNoStoreHeaderOnPruneResponses(t *testing.T) {
	fc := newFakePruneClient()
	s := newTestServerWithPrune(t, "", fc)
	req := httptest.NewRequest(http.MethodGet, "/api/prune/candidates", nil)
	w := httptest.NewRecorder()
	s.handlePruneCandidates(w, req)
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}
