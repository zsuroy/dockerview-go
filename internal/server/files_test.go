package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/zsuroy/dockerview-go/internal/audit"
	"github.com/zsuroy/dockerview-go/internal/config"
	"github.com/zsuroy/dockerview-go/internal/files"
)

const testJailRoot = "/tmp/dockerview-files"

func newFilesTestServer(t *testing.T, token string, maxFile int64, guestDL bool) (*Server, *files.MockCopier) {
	t.Helper()
	stage := t.TempDir()
	mockRoot := t.TempDir()
	mc, err := files.NewMockCopier(mockRoot)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(nil, token, "vtest", "c", "d")
	srv.SetFilesConfig(config.FilesConfig{
		JailRoot:           testJailRoot,
		MaxFileBytes:       maxFile,
		MaxArchiveBytes:    maxFile,
		AllowGuestDownload: guestDL,
	}, stage)
	srv.SetFileCopier(mc)
	rec, err := audit.Open(audit.Config{DBPath: ":memory:", RetentionDays: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rec.Close() })
	srv.SetAuditer(rec)
	return srv, mc
}

func (s *Server) stageDirForTest() string { return s.getFilesSettings().stageDir }

func filesJSON(t *testing.T, h http.Handler, method, url, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != nil {
		b, _ := json.Marshal(body)
		r = httptest.NewRequest(method, url, bytes.NewReader(b))
	} else {
		r = httptest.NewRequest(method, url, nil)
	}
	if token != "" {
		r.Header.Set("X-Auth-Token", token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func uploadMultipart(t *testing.T, h http.Handler, token, id, p string, content []byte, overwrite bool) *httptest.ResponseRecorder {
	t.Helper()
	return uploadMultipartOpts(t, h, token, id, p, content, uploadOpts{overwrite: overwrite})
}

type uploadOpts struct {
	overwrite bool
	mkdir     bool
	noFields  []string
}

// uploadMultipartOpts builds the multipart confirm request with optional
// mkdir opt-in; fields in noFields are omitted entirely.
func uploadMultipartOpts(t *testing.T, h http.Handler, token, id, p string, content []byte, o uploadOpts) *httptest.ResponseRecorder {
	t.Helper()
	buf := &bytes.Buffer{}
	mw := multipart.NewWriter(buf)
	if !slices.Contains(o.noFields, "id") {
		_ = mw.WriteField("id", id)
	}
	if !slices.Contains(o.noFields, "path") {
		_ = mw.WriteField("path", p)
	}
	if o.overwrite {
		_ = mw.WriteField("overwrite", "true")
	}
	if o.mkdir {
		_ = mw.WriteField("mkdir", "true")
	}
	part, err := mw.CreateFormFile("file", "upload.bin")
	if err != nil {
		t.Fatal(err)
	}
	part.Write(content)
	mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/api/files/in", buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		r.Header.Set("X-Auth-Token", token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestFilesCopyInPreviewWritesNothing(t *testing.T) {
	srv, mc := newFilesTestServer(t, "secret", 8<<20, false)
	if err := mc.SeedFile("c1", testJailRoot+"/exists.txt", []byte("old")); err != nil {
		t.Fatal(err)
	}
	stageFilesBefore, _ := os.ReadDir(srv.stageDirForTest())

	w := filesJSON(t, srv.Handler(), http.MethodPost, "/api/files/in/preview", "secret",
		map[string]string{"id": "c1", "path": "new.txt"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["path"] != testJailRoot+"/new.txt" || resp["exists"] != false {
		t.Fatalf("bad preview: %v", resp)
	}
	// No staging artifacts, no container FS mutation.
	if entries, _ := os.ReadDir(srv.stageDirForTest()); len(entries) != len(stageFilesBefore) {
		t.Fatal("preview wrote staging files")
	}
	if _, err := mc.Stat(t.Context(), "c1", testJailRoot+"/new.txt"); !errors.Is(err, files.ErrNotFound) {
		t.Fatalf("preview wrote into container FS: %v", err)
	}

	// Existing target reports overwrite_required.
	w2 := filesJSON(t, srv.Handler(), http.MethodPost, "/api/files/in/preview", "secret",
		map[string]string{"id": "c1", "path": "exists.txt"})
	json.Unmarshal(w2.Body.Bytes(), &resp)
	if resp["exists"] != true || resp["overwrite_required"] != true {
		t.Fatalf("overwrite preview: %v", resp)
	}
}
func TestFilesCopyInMkdirGate(t *testing.T) {
	srv, mc := newFilesTestServer(t, "secret", 8<<20, false)
	payload := bytes.Repeat([]byte{0, 1, 2, 255, 10, 127}, 100)
	// New target under a missing directory: the first attempt is gated on
	// the explicit mkdir opt-in (mirrors the overwrite gate).
	w := uploadMultipartOpts(t, srv.Handler(), "secret", "c1", "sub/blob.bin", payload, uploadOpts{})
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "mkdir_required") {
		t.Fatalf("want mkdir_required, got %s", w.Body.String())
	}
	w = uploadMultipartOpts(t, srv.Handler(), "secret", "c1", "sub/blob.bin", payload, uploadOpts{mkdir: true})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	sum := sha256.Sum256(payload)
	if resp["sha256"] != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha mismatch: %v", resp)
	}
	if int(resp["bytes"].(float64)) != len(payload) {
		t.Fatalf("bytes: %v", resp)
	}
	// Staging must be clean afterwards.
	if entries, _ := os.ReadDir(srv.stageDirForTest()); len(entries) != 0 {
		t.Fatalf("staging leftovers: %v", entries)
	}
	// Container FS actually received the bytes under the created dir.
	fi, err := mc.Stat(t.Context(), "c1", testJailRoot+"/sub/blob.bin")
	if err != nil || fi.Size != int64(len(payload)) {
		t.Fatalf("container stat: %+v %v", fi, err)
	}
	if entries, _ := mc.ReadDir(t.Context(), "c1", testJailRoot+"/sub"); len(entries) == 0 {
		t.Fatal("created directory not visible in listing")
	}
	// Preview reports the missing chain before any write.
	pw := filesJSON(t, srv.Handler(), http.MethodPost, "/api/files/in/preview", "secret",
		map[string]string{"id": "c1", "path": "a/b/c.txt"})
	var prev map[string]any
	json.Unmarshal(pw.Body.Bytes(), &prev)
	if prev["mkdir_required"] != true {
		t.Fatalf("preview missing_dirs flag: %v", prev)
	}
	want := []any{testJailRoot + "/a", testJailRoot + "/a/b"}
	if got, ok := prev["missing_dirs"].([]any); !ok || len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("missing_dirs = %v, want %v", prev["missing_dirs"], want)
	}
}

// An existing non-directory ancestor blocks the upload even with mkdir=true.
func TestFilesCopyInMkdirAncestorIsFile(t *testing.T) {
	srv, mc := newFilesTestServer(t, "secret", 8<<20, false)
	if err := mc.SeedFile("c1", testJailRoot+"/blocked", []byte("file")); err != nil {
		t.Fatal(err)
	}
	w := uploadMultipartOpts(t, srv.Handler(), "secret", "c1", "blocked/x.bin", []byte("x"), uploadOpts{mkdir: true})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "not a directory") {
		t.Fatalf("want ancestor error, got %s", w.Body.String())
	}
}

// A container without the jail root itself must be gated the same way as
// missing intermediates: the root is reported (preview) and required
// (confirm). The HTTP path cannot show this with the mock backend — mock
// resolveContainerPath pre-creates the jail dir — so the gate helper is
// exercised directly, mirroring what the docker backend (RootFS "" → no
// pre-create) feeds it in production.
func TestFilesCopyInJailRootMissing(t *testing.T) {
	srv, mc := newFilesTestServer(t, "secret", 8<<20, false)
	if _, err := mc.Stat(t.Context(), "c1", testJailRoot); !errors.Is(err, files.ErrNotFound) {
		t.Fatalf("precondition: jail root should be missing, got %v", err)
	}
	missing, err := srv.missingParentDirs(mc, t.Context(), "c1", testJailRoot+"/logs/app.log")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{testJailRoot, testJailRoot + "/logs"}
	if len(missing) != 2 || missing[0] != want[0] || missing[1] != want[1] {
		t.Fatalf("missing = %v, want %v", missing, want)
	}

	// Root present but a file: hard rejection, same as any bad ancestor.
	if err := mc.SeedFile("c1", testJailRoot, []byte("not a dir")); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.missingParentDirs(mc, t.Context(), "c1", testJailRoot+"/x"); err == nil ||
		!strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("root-as-file err = %v", err)
	}
}
func TestFilesCopyInTooLarge(t *testing.T) {
	srv, mc := newFilesTestServer(t, "secret", 64, false)
	before, _ := os.ReadDir(mc.RootFS("c1"))
	w := uploadMultipart(t, srv.Handler(), "secret", "c1", "big.bin", bytes.Repeat([]byte{'x'}, 100), false)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d want 413 body=%s", w.Code, w.Body.String())
	}
	after, _ := os.ReadDir(mc.RootFS("c1"))
	if len(after) != len(before) {
		t.Fatal("oversized upload left container FS artifacts")
	}
	if entries, _ := os.ReadDir(srv.stageDirForTest()); len(entries) != 0 {
		t.Fatal("oversized upload left staging artifacts")
	}
}

func TestFilesCopyInLimitsReadFromConfig(t *testing.T) {
	// Quota must come from config (yaml-driven, testable).
	srv, _ := newFilesTestServer(t, "secret", 16, false)
	w := uploadMultipart(t, srv.Handler(), "secret", "c1", "ok.bin", []byte("0123456789abcdef"), false)
	if w.Code != http.StatusOK {
		t.Fatalf("exact-limit upload: %d %s", w.Code, w.Body.String())
	}
	w2 := uploadMultipart(t, srv.Handler(), "secret", "c1", "one.bin", []byte("0123456789abcdef0"), false)
	if w2.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("one byte over: %d", w2.Code)
	}
}

func TestFilesCopyInOverwriteGate(t *testing.T) {
	srv, _ := newFilesTestServer(t, "secret", 8<<20, false)
	if w := uploadMultipart(t, srv.Handler(), "secret", "c1", "f.txt", []byte("one"), false); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	// Default deny even when caller forgets the flag.
	w := uploadMultipart(t, srv.Handler(), "secret", "c1", "f.txt", []byte("two"), false)
	if w.Code != http.StatusConflict {
		t.Fatalf("missing overwrite: status=%d body=%s", w.Code, w.Body.String())
	}
	w2 := uploadMultipart(t, srv.Handler(), "secret", "c1", "f.txt", []byte("two"), true)
	if w2.Code != http.StatusOK {
		t.Fatalf("with overwrite: %d %s", w2.Code, w2.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w2.Body.Bytes(), &resp)
	if resp["overwritten"] != true {
		t.Fatalf("overwritten flag: %v", resp)
	}
}

func TestFilesCopyInEscapeRejected(t *testing.T) {
	srv, mc := newFilesTestServer(t, "secret", 8<<20, false)
	// Canary outside the jail.
	if err := mc.SeedFile("c1", "/etc/secret-canary", []byte("TOPSECRET")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"../../etc/evil", "/etc/passwd", "sub/../../../etc/evil", `C:\Windows\win.ini`} {
		w := uploadMultipart(t, srv.Handler(), "secret", "c1", p, []byte("x"), false)
		if w.Code < 400 {
			t.Errorf("path %q accepted: %d", p, w.Code)
		}
	}
	if _, err := mc.Stat(t.Context(), "c1", "/etc/evil"); !errors.Is(err, files.ErrNotFound) {
		t.Fatal("escape write landed outside jail")
	}
}

func TestFilesCopyInAuthRequired(t *testing.T) {
	srv, _ := newFilesTestServer(t, "secret", 8<<20, false)
	w := uploadMultipart(t, srv.Handler(), "", "c1", "a.txt", []byte("x"), false)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("guest write: %d", w.Code)
	}
	w2 := uploadMultipart(t, srv.Handler(), "wrong", "c1", "a.txt", []byte("x"), false)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", w2.Code)
	}
	// Preview/list-class endpoints also require admin.
	w3 := filesJSON(t, srv.Handler(), http.MethodPost, "/api/files/in/preview", "",
		map[string]string{"id": "c1", "path": "a.txt"})
	if w3.Code != http.StatusUnauthorized {
		t.Fatalf("guest preview: %d", w3.Code)
	}
}

func TestFilesCopyOutRoundTripAndHeaders(t *testing.T) {
	srv, mc := newFilesTestServer(t, "secret", 8<<20, false)
	payload := []byte("binary\x00payload\n排障")
	if err := mc.SeedFile("c1", testJailRoot+"/sub/data.bin", payload); err != nil {
		t.Fatal(err)
	}
	// Preview: size + sha256 without writing anything.
	w := filesJSON(t, srv.Handler(), http.MethodPost, "/api/files/out/preview", "secret",
		map[string]string{"id": "c1", "path": "sub/data.bin"})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var prev map[string]any
	json.Unmarshal(w.Body.Bytes(), &prev)
	sum := sha256.Sum256(payload)
	if prev["sha256"] != hex.EncodeToString(sum[:]) {
		t.Fatalf("preview sha: %v", prev)
	}
	if entries, _ := os.ReadDir(srv.stageDirForTest()); len(entries) != 0 {
		t.Fatal("out preview staged a file")
	}
	// Download.
	r := httptest.NewRequest(http.MethodGet, "/api/files/out?id=c1&path=sub/data.bin", nil)
	r.Header.Set("X-Auth-Token", "secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, r)
	if rec.Code != 200 {
		t.Fatalf("download: %d %s", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), payload) {
		t.Fatal("download bytes differ")
	}
	if rec.Header().Get("X-Dockerview-Sha256") != hex.EncodeToString(sum[:]) {
		t.Fatalf("download sha header: %q", rec.Header().Get("X-Dockerview-Sha256"))
	}
	cd := rec.Header().Get("Content-Disposition")
	if !strings.Contains(cd, `filename="data.bin"`) {
		t.Fatalf("content-disposition: %q", cd)
	}
	if strings.Count(strings.SplitN(cd, ";", 2)[0], "/") != 0 && strings.Contains(cd, "/") {
		t.Fatalf("path separator leaked into disposition: %q", cd)
	}
	// Staging cleaned after download.
	if entries, _ := os.ReadDir(srv.stageDirForTest()); len(entries) != 0 {
		t.Fatal("download left staging artifact")
	}
}

func TestFilesCopyOutMissing404(t *testing.T) {
	srv, _ := newFilesTestServer(t, "secret", 8<<20, false)
	w := filesJSON(t, srv.Handler(), http.MethodPost, "/api/files/out/preview", "secret",
		map[string]string{"id": "c1", "path": "nope.txt"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing preview: %d", w.Code)
	}
	r := httptest.NewRequest(http.MethodGet, "/api/files/out?id=c1&path=nope.txt", nil)
	r.Header.Set("X-Auth-Token", "secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing download: %d", rec.Code)
	}
}

func TestFilesOutSymlinkToDirectoryIs400(t *testing.T) {
	srv, mc := newFilesTestServer(t, "secret", 8<<20, false)
	if err := mc.SeedDir("c1", testJailRoot+"/realdir", 0o750); err != nil {
		t.Fatal(err)
	}
	if err := mc.SeedSymlink("c1", testJailRoot+"/dirlink", "realdir"); err != nil {
		t.Fatal(err)
	}
	w := filesJSON(t, srv.Handler(), http.MethodPost, "/api/files/out/preview", "secret",
		map[string]string{"id": "c1", "path": "dirlink"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("symlink-to-dir preview: %d %s", w.Code, w.Body.String())
	}
	r := httptest.NewRequest(http.MethodGet, "/api/files/out?id=c1&path=dirlink", nil)
	r.Header.Set("X-Auth-Token", "secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("symlink-to-dir download: %d", rec.Code)
	}
}

func TestFilesGuestDownloadDefaultDeniedAndOptIn(t *testing.T) {
	srv, mc := newFilesTestServer(t, "secret", 8<<20, false)
	mc.SeedFile("c1", testJailRoot+"/g.txt", []byte("guest"))
	r := httptest.NewRequest(http.MethodGet, "/api/files/out?id=c1&path=g.txt", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("guest download default: %d", rec.Code)
	}

	srv2, mc2 := newFilesTestServer(t, "secret", 8<<20, true)
	mc2.SeedFile("c1", testJailRoot+"/g.txt", []byte("guest"))
	r2 := httptest.NewRequest(http.MethodGet, "/api/files/out?id=c1&path=g.txt", nil)
	rec2 := httptest.NewRecorder()
	srv2.Handler().ServeHTTP(rec2, r2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("guest download opt-in: %d %s", rec2.Code, rec2.Body.String())
	}
}

func TestFilesContentDispositionSanitized(t *testing.T) {
	ascii, utf8name := sanitizeDownloadName("/etc/../sub/我的 文件.txt")
	if strings.ContainsAny(ascii, "/\\") {
		t.Fatalf("ascii name leaks separators: %q", ascii)
	}
	if !strings.Contains(utf8name, "文件") || strings.ContainsAny(utf8name, "/\\") {
		t.Fatalf("utf8 name: %q", utf8name)
	}
	a2, _ := sanitizeDownloadName("../../")
	if a2 != "dockerview-file" {
		t.Fatalf("empty-ish name fallback: %q", a2)
	}
	// Invalid UTF-8 must degrade to underscore, never to RuneError bytes.
	a3, _ := sanitizeDownloadName("bad\xffname.bin")
	if strings.ContainsRune(a3, '�') {
		t.Fatalf("replacement rune leaked into name: %q", a3)
	}
}

func TestFilesConfigEndpoint(t *testing.T) {
	srv, _ := newFilesTestServer(t, "secret", 4096, true)
	r := httptest.NewRequest(http.MethodGet, "/api/files/config", nil)
	r.Header.Set("X-Auth-Token", "secret")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["jail_root"] != testJailRoot {
		t.Fatalf("jail_root=%v", resp["jail_root"])
	}
	if resp["max_file_bytes"].(float64) != 4096 {
		t.Fatalf("max_file_bytes=%v", resp["max_file_bytes"])
	}
	if resp["guest_download"] != true || resp["backend_configured"] != true {
		t.Fatalf("flags wrong: %v", resp)
	}
}

func TestFilesConfigGuestWrongTokenIs401(t *testing.T) {
	// Even with guest downloads enabled, a present-but-wrong token is a
	// hard 401 (never silently downgraded to guest).
	srv, mc := newFilesTestServer(t, "secret", 8<<20, true)
	mc.SeedFile("c1", testJailRoot+"/g.txt", []byte("g"))
	r := httptest.NewRequest(http.MethodGet, "/api/files/out?id=c1&path=g.txt", nil)
	r.Header.Set("X-Auth-Token", "wrong")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token guest-opt-in: %d", w.Code)
	}
	r2 := httptest.NewRequest(http.MethodPost, "/api/files/config", nil)
	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, r2)
	if w2.Code != http.StatusMethodNotAllowed {
		t.Fatalf("config POST: %d want 405", w2.Code)
	}
}

func TestFilesContentDispositionUnicode(t *testing.T) {
	srv, mc := newFilesTestServer(t, "secret", 8<<20, false)
	mc.SeedFile("c1", testJailRoot+"/日志.txt", []byte("zh"))
	r := httptest.NewRequest(http.MethodGet, "/api/files/out?id=c1&path="+url.QueryEscape("日志.txt"), nil)
	r.Header.Set("X-Auth-Token", "secret")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("unicode download: %d %s", w.Code, w.Body.String())
	}
	cd := w.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "filename*=UTF-8''") || !strings.Contains(cd, "%E6%97%A5") {
		t.Fatalf("RFC5987 encoding missing: %q", cd)
	}
}

func TestFilesUnknownRouteJSON404(t *testing.T) {
	srv, _ := newFilesTestServer(t, "secret", 8<<20, false)
	r := httptest.NewRequest(http.MethodGet, "/api/files/bogus/route", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("content-type=%q", w.Header().Get("Content-Type"))
	}
}

func TestFilesNoCopierMeans503(t *testing.T) {
	srv := NewServer(nil, "secret", "v", "c", "d")
	srv.SetFilesConfig(config.FilesConfig{JailRoot: testJailRoot, MaxFileBytes: 100}, t.TempDir())
	// SetAuditer noop by default when nil
	w := filesJSON(t, srv.Handler(), http.MethodPost, "/api/files/in/preview", "secret",
		map[string]string{"id": "c1", "path": "a.txt"})
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d %s", w.Code, w.Body.String())
	}
}
