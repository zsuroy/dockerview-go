package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zsuroy/dockerview-go/internal/audit"
	"github.com/zsuroy/dockerview-go/internal/backup"
)

// srvProvider is the server-test implementation of backup.Provider.
type srvProvider struct {
	snaps     []backup.ContainerSnapshot
	images    []backup.ImageInfo
	saver     backup.ImageSaver
	blockSnap chan struct{} // when non-nil, Snapshot blocks until closed
}

func (p *srvProvider) Snapshot(ctx context.Context) ([]backup.ContainerSnapshot, error) {
	if p.blockSnap != nil {
		select {
		case <-p.blockSnap:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return p.snaps, nil
}

func (p *srvProvider) Images(context.Context) ([]backup.ImageInfo, error) { return p.images, nil }
func (p *srvProvider) Saver() backup.ImageSaver {
	if p.saver == nil {
		return &backup.MockImageSaver{Cfg: backup.MockSaverConfig{BytesPerImage: 2048}}
	}
	return p.saver
}

func backupFixtureProvider() *srvProvider {
	return &srvProvider{
		snaps: []backup.ContainerSnapshot{
			{
				ID: "a1b2c3d4e5f6", FullID: "a1b2c3d4e5f6full",
				Name: "web-frontend", Image: "registry.internal/ops/web:2.4.1",
				Status: "Up 3 days",
				Labels: map[string]string{"app": "web"},
				Ports:  []backup.Port{{IP: "0.0.0.0", PrivatePort: 80, PublicPort: 8080, Type: "tcp"}},
				Env:    []string{"TZ=Asia/Shanghai", "DB_PASSWORD=do-not-leak-1"},
			},
			{
				ID: "b2c3d4e5f6a7", FullID: "b2c3d4e5f6a7full",
				Name: "db-postgres", Image: "registry.internal/ops/postgres:16.3",
				Status: "Up 3 days",
				Env:    []string{"POSTGRES_PASSWORD=do-not-leak-4"},
			},
		},
		images: []backup.ImageInfo{
			{Ref: "registry.internal/ops/web:2.4.1", ID: "sha256:1111", SizeBytes: 187_000_000},
			{Ref: "registry.internal/ops/postgres:16.3", ID: "sha256:2222", SizeBytes: 432_000_000},
		},
	}
}

func RuntimeConfigShim() backup.RuntimeConfig {
	return backup.RuntimeConfig{
		ServerPort: 8080, TokenMode: true, AuditEnabled: false,
		Version: "vtest", Commit: "abc", BuildDate: "now",
	}
}

// newBackupTestServer wires a Server with token auth + a temp-dir manager.
func newBackupTestServer(t *testing.T, token string, p backup.Provider) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	mgr, err := backup.NewManager(backup.Config{
		Dir: dir, Provider: p,
		Runtime: RuntimeConfigShim(),
	})
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(nil, token, "vtest", "abc", "now")
	s.SetBackupManager(mgr)
	rec, err := audit.Open(audit.Config{DBPath: ":memory:", RetentionDays: 0})
	if err != nil {
		t.Fatal(err)
	}
	s.SetAuditer(rec)
	t.Cleanup(func() { _ = rec.Close() })
	return s, dir
}

func doJSON(t *testing.T, s *Server, method, target, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	var rd *strings.Reader
	if body != "" {
		rd = strings.NewReader(body)
	} else {
		rd = strings.NewReader("")
	}
	req := httptest.NewRequest(method, target, rd)
	if token != "" {
		req.Header.Set("X-Auth-Token", token)
	}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	return w
}

func TestBackupPreview_AuthMatrix(t *testing.T) {
	s, dir := newBackupTestServer(t, "secret", backupFixtureProvider())
	cases := []struct {
		name  string
		token string
		want  int
	}{
		{"no token", "", http.StatusUnauthorized},
		{"wrong token", "nope", http.StatusUnauthorized},
		{"valid token", "secret", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doJSON(t, s, "POST", "/api/backup/preview", `{"include_images":false}`, tc.token)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, tc.want, w.Body.String())
			}
		})
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("preview+auth failures must not create files: %v", entries)
	}
}

func TestBackupPreview_NoDiskArtifacts(t *testing.T) {
	s, dir := newBackupTestServer(t, "secret", backupFixtureProvider())
	w := doJSON(t, s, "POST", "/api/backup/preview", `{"include_images":true}`, "secret")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var rep backup.PreviewReport
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Containers != 2 || len(rep.Images) != 2 {
		t.Fatalf("preview report wrong: %+v", rep)
	}
	if !rep.Options.IncludeImages {
		t.Fatal("preview must echo include_images=true")
	}
	if len(rep.Warnings) == 0 {
		t.Fatal("include_images preview must warn about size")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("preview must not write to backups dir: %v", entries)
	}
}

func TestBackupCreate_DefaultArchive(t *testing.T) {
	s, dir := newBackupTestServer(t, "secret", backupFixtureProvider())
	w := doJSON(t, s, "POST", "/api/backup/create", `{"note":"drill"}`, "secret")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var rep backup.CreateReport
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if !backup.ValidArchiveName(rep.Name) || rep.Options.IncludeImages {
		t.Fatalf("bad report: %+v", rep)
	}
	full := filepath.Join(dir, rep.Name)
	zr, err := zip.OpenReader(full)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
		if strings.HasPrefix(f.Name, "images/") {
			t.Fatalf("default create must not include images/: %s", f.Name)
		}
	}
	for _, must := range []string{"manifest.json", "containers.json", "README.txt", "config/runtime.json"} {
		if !names[must] {
			t.Fatalf("archive missing %s: %v", must, names)
		}
	}
}

func TestBackupCreate_IncludeImages(t *testing.T) {
	s, dir := newBackupTestServer(t, "secret", backupFixtureProvider())
	w := doJSON(t, s, "POST", "/api/backup/create", `{"include_images":true}`, "secret")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var rep backup.CreateReport
	_ = json.Unmarshal(w.Body.Bytes(), &rep)
	if rep.Images != 2 {
		t.Fatalf("images = %d, want 2: %+v", rep.Images, rep)
	}
	zr, err := zip.OpenReader(filepath.Join(dir, rep.Name))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	imgCount := 0
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "images/") && strings.HasSuffix(f.Name, ".tar") {
			imgCount++
		}
	}
	if imgCount != 2 {
		t.Fatalf("images entries = %d, want 2", imgCount)
	}
}

func TestBackupCreate_ImageExportFailure502(t *testing.T) {
	p := backupFixtureProvider()
	p.saver = &backup.MockImageSaver{Cfg: backup.MockSaverConfig{
		BytesPerImage: 1024,
		FailRefs:      []string{"registry.internal/ops/web:2.4.1"},
	}}
	s, dir := newBackupTestServer(t, "secret", p)
	w := doJSON(t, s, "POST", "/api/backup/create", `{"include_images":true}`, "secret")
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
	}
	if !strings.Contains(w.Body.String(), "image_export_failed") {
		t.Fatalf("error code missing: %s", w.Body.String())
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		t.Fatalf("failed create must leave no artifacts, found %s", e.Name())
	}
}

func TestBackupCreate_Conflict409(t *testing.T) {
	p := backupFixtureProvider()
	gate := make(chan struct{})
	p.blockSnap = gate
	s, _ := newBackupTestServer(t, "secret", p)

	var wg sync.WaitGroup
	codes := make(chan int, 2)
	wg.Add(1)
	go func() {
		defer wg.Done()
		w := doJSON(t, s, "POST", "/api/backup/create", `{}`, "secret")
		codes <- w.Code
	}()
	// give the first request time to enter Snapshot (blocked on gate)
	time.Sleep(150 * time.Millisecond)
	w := doJSON(t, s, "POST", "/api/backup/create", `{}`, "secret")
	if w.Code != http.StatusConflict {
		t.Fatalf("second create = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "create_in_progress") {
		t.Fatalf("error code missing: %s", w.Body.String())
	}
	close(gate)
	wg.Wait()
	close(codes)
	for c := range codes {
		if c != http.StatusOK {
			t.Fatalf("first create = %d, want 200", c)
		}
	}
}

func TestBackupCreate_InvalidBody(t *testing.T) {
	s, _ := newBackupTestServer(t, "secret", backupFixtureProvider())
	w := doJSON(t, s, "POST", "/api/backup/create", `{invalid`, "secret")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestBackupList_AuthAndContent(t *testing.T) {
	s, _ := newBackupTestServer(t, "secret", backupFixtureProvider())
	if w := doJSON(t, s, "GET", "/api/backup/list", "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("list no-token = %d, want 401", w.Code)
	}
	if w := doJSON(t, s, "POST", "/api/backup/create", `{"note":"hello"}`, "secret"); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w := doJSON(t, s, "GET", "/api/backup/list", "", "secret")
	if w.Code != http.StatusOK {
		t.Fatalf("list = %d", w.Code)
	}
	var lr backup.ListReport
	if err := json.Unmarshal(w.Body.Bytes(), &lr); err != nil {
		t.Fatal(err)
	}
	if len(lr.Backups) != 1 || lr.Backups[0].Note != "hello" || lr.Backups[0].Containers != 2 {
		t.Fatalf("list report wrong: %+v", lr)
	}
}

func TestBackupDownload_SuccessAndTraversal(t *testing.T) {
	s, _ := newBackupTestServer(t, "secret", backupFixtureProvider())
	w := doJSON(t, s, "POST", "/api/backup/create", `{}`, "secret")
	var rep backup.CreateReport
	_ = json.Unmarshal(w.Body.Bytes(), &rep)

	req := httptest.NewRequest("GET", "/api/backup/download?name="+rep.Name, nil)
	req.Header.Set("X-Auth-Token", "secret")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("download = %d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("content-type = %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, rep.Name) {
		t.Fatalf("content-disposition = %q", cd)
	}
	body := rec.Body.Bytes()
	if len(body) < 4 || body[0] != 'P' || body[1] != 'K' {
		t.Fatalf("not a zip stream (first bytes %v)", body[:4])
	}

	// traversal + invalid names must be rejected with 400
	for _, evil := range []string{
		"../../../etc/passwd",
		"/etc/passwd",
		"..%2F..%2Fetc%2Fpasswd",
		rep.Name + "%00",
		rep.Name[:len(rep.Name)-4] + ".tar", // suffix tampering
	} {
		req := httptest.NewRequest("GET", "/api/backup/download?name="+evil, nil)
		req.Header.Set("X-Auth-Token", "secret")
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("download %q = %d, want 400", evil, rec.Code)
		}
	}

	// valid name that does not exist → 404
	req = httptest.NewRequest("GET", "/api/backup/download?name=dockerview-backup-20200101T000000Z-000000.zip", nil)
	req.Header.Set("X-Auth-Token", "secret")
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing archive = %d, want 404", rec.Code)
	}

	// guest cannot download
	req = httptest.NewRequest("GET", "/api/backup/download?name="+rep.Name, nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("guest download = %d, want 401", rec.Code)
	}
}

func TestBackupDelete_Flow(t *testing.T) {
	s, _ := newBackupTestServer(t, "secret", backupFixtureProvider())
	w := doJSON(t, s, "POST", "/api/backup/create", `{}`, "secret")
	var rep backup.CreateReport
	_ = json.Unmarshal(w.Body.Bytes(), &rep)

	if w := doJSON(t, s, "POST", "/api/backup/delete", `{"name":"`+rep.Name+`"}`, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("delete no-token = %d, want 401", w.Code)
	}
	if w := doJSON(t, s, "POST", "/api/backup/delete", `{"name":"../x"}`, "secret"); w.Code != http.StatusBadRequest {
		t.Fatalf("delete traversal = %d, want 400", w.Code)
	}
	if w := doJSON(t, s, "POST", "/api/backup/delete", `{"name":"`+rep.Name+`"}`, "secret"); w.Code != http.StatusOK {
		t.Fatalf("delete = %d body=%s", w.Code, w.Body.String())
	}
	if w := doJSON(t, s, "POST", "/api/backup/delete", `{"name":"`+rep.Name+`"}`, "secret"); w.Code != http.StatusNotFound {
		t.Fatalf("double delete = %d, want 404", w.Code)
	}
}

func TestBackup_MethodNotAllowed(t *testing.T) {
	s, _ := newBackupTestServer(t, "secret", backupFixtureProvider())
	cases := []struct{ method, path string }{
		{"GET", "/api/backup/preview"},
		{"PUT", "/api/backup/create"},
		{"DELETE", "/api/backup/list"},
		{"POST", "/api/backup/download"},
		{"GET", "/api/backup/delete"},
		{"PATCH", "/api/backup/preview"},
	}
	for _, tc := range cases {
		w := doJSON(t, s, tc.method, tc.path, "", "secret")
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s = %d, want 405", tc.method, tc.path, w.Code)
		}
	}
}

func TestBackup_ManagerNotConfigured503(t *testing.T) {
	s := NewServer(nil, "secret", "vtest", "abc", "now") // no backup manager
	cases := []struct{ method, path, body string }{
		{"POST", "/api/backup/preview", `{}`},
		{"POST", "/api/backup/create", `{}`},
		{"GET", "/api/backup/list", ""},
		{"GET", "/api/backup/download?name=x", ""},
		{"POST", "/api/backup/delete", `{"name":"x"}`},
	}
	for _, tc := range cases {
		w := doJSON(t, s, tc.method, tc.path, tc.body, "secret")
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s without manager = %d, want 503 (%s)", tc.method, tc.path, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "backup_unavailable") {
			t.Fatalf("%s %s must report backup_unavailable: %s", tc.method, tc.path, w.Body.String())
		}
	}
}

func TestBackup_NoTokenConfiguredAllowsAll(t *testing.T) {
	s, _ := newBackupTestServer(t, "", backupFixtureProvider())
	for _, c := range []struct{ method, path, body string }{
		{"POST", "/api/backup/preview", `{}`},
		{"POST", "/api/backup/create", `{}`},
		{"GET", "/api/backup/list", ""},
	} {
		w := doJSON(t, s, c.method, c.path, c.body, "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s %s without token-mode = %d, want 200 (%s)", c.method, c.path, w.Code, w.Body.String())
		}
	}
}

func TestBackup_AuditEventsRecorded(t *testing.T) {
	s, _ := newBackupTestServer(t, "secret", backupFixtureProvider())
	// denied create (no token)
	_ = doJSON(t, s, "POST", "/api/backup/create", `{}`, "")
	// successful create
	_ = doJSON(t, s, "POST", "/api/backup/create", `{"note":"audit-check"}`, "secret")

	rec := s.aud()
	page, err := rec.List(context.Background(), audit.Query{Actions: []string{audit.ActionBackupCreate}, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total < 2 {
		t.Fatalf("want >=2 backup_create events, got %d", page.Total)
	}
	var sawDenied, sawSuccess bool
	for _, it := range page.Items {
		if it.Result == audit.ResultDenied && it.StatusCode == http.StatusUnauthorized {
			sawDenied = true
		}
		if it.Result == audit.ResultSuccess && it.Payload != nil {
			if name, ok := it.Payload["name"]; ok && name != "" {
				sawSuccess = true
			}
		}
	}
	if !sawDenied || !sawSuccess {
		t.Fatalf("audit must record denied+success: denied=%v success=%v", sawDenied, sawSuccess)
	}
}

func TestBackupCreate_DownloadRoundtripBytes(t *testing.T) {
	s, dir := newBackupTestServer(t, "secret", backupFixtureProvider())
	w := doJSON(t, s, "POST", "/api/backup/create", `{}`, "secret")
	var rep backup.CreateReport
	_ = json.Unmarshal(w.Body.Bytes(), &rep)

	req := httptest.NewRequest("GET", "/api/backup/download?name="+rep.Name, nil)
	req.Header.Set("X-Auth-Token", "secret")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	onDisk, err := os.ReadFile(filepath.Join(dir, rep.Name))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rec.Body.Bytes(), onDisk) {
		t.Fatalf("downloaded bytes differ from on-disk archive (%d vs %d)", rec.Body.Len(), len(onDisk))
	}
}

func TestBackupCreate_PreviewConsistency(t *testing.T) {
	s, _ := newBackupTestServer(t, "secret", backupFixtureProvider())
	pw := doJSON(t, s, "POST", "/api/backup/preview", `{"include_images":false}`, "secret")
	var pv backup.PreviewReport
	_ = json.Unmarshal(pw.Body.Bytes(), &pv)
	cw := doJSON(t, s, "POST", "/api/backup/create", `{}`, "secret")
	var cr backup.CreateReport
	_ = json.Unmarshal(cw.Body.Bytes(), &cr)
	if pv.Containers != cr.Containers {
		t.Fatalf("preview containers %d != create %d", pv.Containers, cr.Containers)
	}
	if fmt.Sprint(pv.Options.IncludeImages) != fmt.Sprint(cr.Options.IncludeImages) {
		t.Fatalf("options mismatch preview=%+v create=%+v", pv.Options, cr.Options)
	}
}

func TestBackupPreview_InvalidBody(t *testing.T) {
	s, _ := newBackupTestServer(t, "secret", backupFixtureProvider())
	w := doJSON(t, s, "POST", "/api/backup/preview", `{not-json`, "secret")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestBackupPreview_EmptyProvider(t *testing.T) {
	s, _ := newBackupTestServer(t, "secret", backup.EmptyProvider{})
	w := doJSON(t, s, "POST", "/api/backup/preview", `{}`, "secret")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var rep backup.PreviewReport
	_ = json.Unmarshal(w.Body.Bytes(), &rep)
	if rep.Containers != 0 {
		t.Fatalf("empty provider must preview 0 containers, got %d", rep.Containers)
	}
}

func TestBackupCreate_EmptyBodyDefaultsNoImages(t *testing.T) {
	s, _ := newBackupTestServer(t, "secret", backupFixtureProvider())
	w := doJSON(t, s, "POST", "/api/backup/create", ``, "secret")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var rep backup.CreateReport
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Options.IncludeImages {
		t.Fatal("empty body must default include_images=false")
	}
}

func TestBackupList_EmptyDir(t *testing.T) {
	s, _ := newBackupTestServer(t, "secret", backup.EmptyProvider{})
	w := doJSON(t, s, "GET", "/api/backup/list", "", "secret")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var lr backup.ListReport
	_ = json.Unmarshal(w.Body.Bytes(), &lr)
	if len(lr.Backups) != 0 {
		t.Fatalf("fresh dir must list 0 backups, got %d", len(lr.Backups))
	}
	if lr.MaxArchives != backup.DefaultMaxArchives {
		t.Fatalf("max_archives = %d, want default %d", lr.MaxArchives, backup.DefaultMaxArchives)
	}
}

func TestBackupDelete_EmptyName(t *testing.T) {
	s, _ := newBackupTestServer(t, "secret", backupFixtureProvider())
	w := doJSON(t, s, "POST", "/api/backup/delete", `{}`, "secret")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty name delete = %d, want 400", w.Code)
	}
}

func TestBackupDownload_MissingNameParam(t *testing.T) {
	s, _ := newBackupTestServer(t, "secret", backupFixtureProvider())
	w := doJSON(t, s, "GET", "/api/backup/download", "", "secret")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing name = %d, want 400", w.Code)
	}
}
