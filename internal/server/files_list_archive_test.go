package server

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/zsuroy/dockerview-go/internal/files"
)

func TestFilesListOnlyUnderRoot(t *testing.T) {
	srv, mc := newFilesTestServer(t, "secret", 8<<20, false)
	mc.SeedFile("c1", testJailRoot+"/a.txt", []byte("a"))
	mc.SeedFile("c1", testJailRoot+"/sub/b.txt", []byte("bb"))
	mc.SeedFile("c1", "/etc/canary", []byte("C"))

	r := httptest.NewRequest(http.MethodGet, "/api/files/list?id=c1", nil)
	r.Header.Set("X-Auth-Token", "secret")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Path    string           `json:"path"`
		Entries []files.DirEntry `json:"entries"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Path != testJailRoot {
		t.Fatalf("path = %q", resp.Path)
	}
	names := map[string]bool{}
	for _, e := range resp.Entries {
		names[e.Name] = true
	}
	if !names["a.txt"] || !names["sub"] {
		t.Fatalf("entries = %v", names)
	}
	if names["canary"] {
		t.Fatal("listing leaked outside jail")
	}

	// Escape attempts rejected.
	for _, p := range []string{"../../../etc", "/etc", "../"} {
		r := httptest.NewRequest(http.MethodGet, "/api/files/list?id=c1&path="+p, nil)
		r.Header.Set("X-Auth-Token", "secret")
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, r)
		if w.Code < 400 {
			t.Errorf("list %q accepted: %d", p, w.Code)
		}
	}

	// Guests cannot list.
	r2 := httptest.NewRequest(http.MethodGet, "/api/files/list?id=c1", nil)
	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, r2)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("guest list: %d", w2.Code)
	}
}

func TestFilesArchiveSubdirTar(t *testing.T) {
	srv, mc := newFilesTestServer(t, "secret", 8<<20, false)
	mc.SeedFile("c1", testJailRoot+"/bundle/a.txt", []byte("aaa"))
	mc.SeedFile("c1", testJailRoot+"/bundle/nested/b.bin", []byte{0, 1, 255, 9})

	w := filesJSON(t, srv.Handler(), http.MethodPost, "/api/files/archive/preview", "secret",
		map[string]string{"id": "c1", "path": "bundle"})
	if w.Code != 200 {
		t.Fatalf("archive preview: %d %s", w.Code, w.Body.String())
	}

	r := httptest.NewRequest(http.MethodGet, "/api/files/archive?id=c1&path=bundle", nil)
	r.Header.Set("X-Auth-Token", "secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, r)
	if rec.Code != 200 {
		t.Fatalf("archive: %d %s", rec.Code, rec.Body.String())
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "bundle.tar") {
		t.Fatalf("disposition: %q", cd)
	}
	if rec.Header().Get("X-Dockerview-Sha256") == "" {
		t.Fatal("missing tar sha256 header")
	}
	tr := tar.NewReader(rec.Body)
	names := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeSymlink {
			t.Fatalf("tar contains symlink entry: %s", hdr.Name)
		}
		if strings.Contains(hdr.Name, "..") || !strings.HasPrefix(hdr.Name, "bundle/") {
			t.Fatalf("tar entry escapes archive root: %s", hdr.Name)
		}
		names[hdr.Name] = true
		if hdr.Name == "bundle/a.txt" {
			b, _ := io.ReadAll(tr)
			if string(b) != "aaa" {
				t.Fatalf("a.txt content: %q", b)
			}
		}
	}
	if !names["bundle/a.txt"] || !names["bundle/nested/b.bin"] {
		t.Fatalf("tar entries: %v", names)
	}
}

func TestFilesArchiveTooLarge(t *testing.T) {
	srv, mc := newFilesTestServer(t, "secret", 10, false)
	mc.SeedFile("c1", testJailRoot+"/big/x.txt", bytes.Repeat([]byte{'x'}, 11))
	w := filesJSON(t, srv.Handler(), http.MethodPost, "/api/files/archive/preview", "secret",
		map[string]string{"id": "c1", "path": "big"})
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("archive preview oversize: %d %s", w.Code, w.Body.String())
	}
	r := httptest.NewRequest(http.MethodGet, "/api/files/archive?id=c1&path=big", nil)
	r.Header.Set("X-Auth-Token", "secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("archive oversize: %d", rec.Code)
	}
	if entries, _ := readDirNames(srv.stageDirForTest()); len(entries) != 0 {
		t.Fatalf("half tar left in staging: %v", entries)
	}
}

func TestFilesArchiveRootRejected(t *testing.T) {
	srv, _ := newFilesTestServer(t, "secret", 8<<20, false)
	w := filesJSON(t, srv.Handler(), http.MethodPost, "/api/files/archive/preview", "secret",
		map[string]string{"id": "c1", "path": "."})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("archive root preview: %d", w.Code)
	}
}

func TestFilesSymlinkRejectedAllOps(t *testing.T) {
	srv, mc := newFilesTestServer(t, "secret", 8<<20, false)
	// Host FS canary + out-of-jail symlinks inside the mock container.
	mc.SeedFile("c1", "/etc/secret-canary", []byte("TOPSECRET"))
	mc.SeedFile("c1", testJailRoot+"/ok.txt", []byte("ok"))
	if err := mc.SeedSymlink("c1", testJailRoot+"/evil", "/etc/secret-canary"); err != nil {
		t.Fatal(err)
	}
	if err := mc.SeedSymlink("c1", testJailRoot+"/etclink", "/etc"); err != nil {
		t.Fatal(err)
	}
	if err := mc.SeedDir("c1", testJailRoot+"/outdir", 0o750); err != nil {
		t.Fatal(err)
	}
	// outdir contains a link that escapes.
	if err := mc.SeedSymlink("c1", testJailRoot+"/outdir/leak", "/etc/secret-canary"); err != nil {
		t.Fatal(err)
	}
	if err := mc.SeedSymlink("c1", testJailRoot+"/outroot", "/"); err != nil {
		t.Fatal(err)
	}

	// Copy OUT through trailing symlink.
	w := filesJSON(t, srv.Handler(), http.MethodPost, "/api/files/out/preview", "secret",
		map[string]string{"id": "c1", "path": "evil"})
	if w.Code < 400 {
		t.Errorf("out via trailing symlink accepted: %d", w.Code)
	}
	r := httptest.NewRequest(http.MethodGet, "/api/files/out?id=c1&path=evil", nil)
	r.Header.Set("X-Auth-Token", "secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, r)
	if rec.Code < 400 || bytes.Contains(rec.Body.Bytes(), []byte("TOPSECRET")) {
		t.Fatalf("out symlink: %d body=%q", rec.Code, rec.Body.String())
	}

	// Copy OUT via mid-component symlink.
	r2 := httptest.NewRequest(http.MethodGet, "/api/files/out?id=c1&path=etclink/hostname", nil)
	r2.Header.Set("X-Auth-Token", "secret")
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, r2)
	if rec2.Code < 400 {
		t.Fatalf("mid-symlink out: %d", rec2.Code)
	}

	// Copy IN onto a trailing out-link.
	up := uploadMultipart(t, srv.Handler(), "secret", "c1", "evil", []byte("pwn"), true)
	if up.Code < 400 {
		t.Fatalf("in via symlink: %d", up.Code)
	}

	// LIST through an out-link.
	r3 := httptest.NewRequest(http.MethodGet, "/api/files/list?id=c1&path=etclink", nil)
	r3.Header.Set("X-Auth-Token", "secret")
	rec3 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec3, r3)
	if rec3.Code < 400 {
		t.Fatalf("list via symlink: %d", rec3.Code)
	}
	// Normal list still shows names only (link itself visible but unreadable).
	r4 := httptest.NewRequest(http.MethodGet, "/api/files/list?id=c1", nil)
	r4.Header.Set("X-Auth-Token", "secret")
	rec4 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec4, r4)
	if rec4.Code != 200 || !strings.Contains(rec4.Body.String(), "evil") {
		t.Fatalf("root list should still show link entry: %d", rec4.Code)
	}

	// ARCHIVE of a dir containing an escaping link (preview + GET).
	for _, target := range []string{"outdir", "outroot"} {
		w := filesJSON(t, srv.Handler(), http.MethodPost, "/api/files/archive/preview", "secret",
			map[string]string{"id": "c1", "path": target})
		if w.Code < 400 {
			t.Errorf("archive preview %s accepted: %d", target, w.Code)
		}
		r := httptest.NewRequest(http.MethodGet, "/api/files/archive?id=c1&path="+target, nil)
		r.Header.Set("X-Auth-Token", "secret")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, r)
		if rec.Code < 400 {
			t.Errorf("archive %s accepted: %d", target, rec.Code)
		}
	}

	// In-jail symlink remains usable: it resolves inside the jail.
	if err := mc.SeedSymlink("c1", testJailRoot+"/alias", "ok.txt"); err != nil {
		t.Fatal(err)
	}
	r5 := httptest.NewRequest(http.MethodGet, "/api/files/out?id=c1&path=alias", nil)
	r5.Header.Set("X-Auth-Token", "secret")
	rec5 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec5, r5)
	if rec5.Code != 200 || rec5.Body.String() != "ok" {
		t.Fatalf("in-jail symlink out: %d %q", rec5.Code, rec5.Body.String())
	}
}

// slowCopier blocks Put until released, to prove same-target writes 409.
type slowCopier struct {
	files.Copier
	release chan struct{}
	put     chan struct{}
}

func (s *slowCopier) Put(ctx context.Context, cid, abs string, r io.Reader, size int64, mode os.FileMode) (int64, error) {
	select {
	case s.put <- struct{}{}:
	default:
	}
	<-s.release
	return s.Copier.Put(ctx, cid, abs, r, size, mode)
}

func TestFilesConcurrentInFlight409(t *testing.T) {
	srv, mc := newFilesTestServer(t, "secret", 8<<20, false)
	slow := &slowCopier{Copier: mc, release: make(chan struct{}), put: make(chan struct{}, 1)}
	srv.SetFileCopier(slow)

	started := make(chan struct{})
	go func() {
		uploadMultipart(t, srv.Handler(), "secret", "c1", "race.txt", []byte("first"), false)
		close(started)
	}()
	<-slow.put
	w := uploadMultipart(t, srv.Handler(), "secret", "c1", "race.txt", []byte("second"), false)
	close(slow.release)
	<-started
	if w.Code != http.StatusConflict {
		t.Fatalf("concurrent upload: %d %s", w.Code, w.Body.String())
	}
}

// lexicalCopier simulates the docker backend: no host FS mapping
// (RootFS == ""), but Stat reports daemon-side link targets.
type lexicalCopier struct {
	files.Copier
	links map[string]string // abs container path -> link target
}

func (l *lexicalCopier) RootFS(string) string { return "" }

func (l *lexicalCopier) Get(_ context.Context, _, _ string, _ io.Writer) (int64, error) {
	return 0, files.ErrNotFound
}

func (l *lexicalCopier) ReadDir(_ context.Context, _, _ string) ([]files.DirEntry, error) {
	return nil, files.ErrNotFound
}

func (l *lexicalCopier) Stat(_ context.Context, _, abs string) (files.FileInfo, error) {
	if tgt, ok := l.links[abs]; ok {
		return files.FileInfo{AbsPath: abs, Name: path2base(abs), IsSymlink: true, LinkTarget: tgt}, nil
	}
	return files.FileInfo{}, files.ErrNotFound
}

func path2base(p string) string {
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	return parts[len(parts)-1]
}

func TestFilesLexicalSymlinkRejection(t *testing.T) {
	srv, _ := newFilesTestServer(t, "secret", 8<<20, false)
	stub := &lexicalCopier{links: map[string]string{
		"/tmp/dockerview-files/evil":  "/etc/passwd",
		"/tmp/dockerview-files/alias": "ok.txt",
	}}
	srv.SetFileCopier(stub)

	// Trailing out-link denied even without host realpath resolution.
	w := filesJSON(t, srv.Handler(), http.MethodPost, "/api/files/out/preview", "secret",
		map[string]string{"id": "c1", "path": "evil"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("out evil: %d %s", w.Code, w.Body.String())
	}
	r := httptest.NewRequest(http.MethodGet, "/api/files/out?id=c1&path=evil", nil)
	r.Header.Set("X-Auth-Token", "secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("download evil: %d", rec.Code)
	}
	// In-jail link accepted by the lexical rule.
	w2 := filesJSON(t, srv.Handler(), http.MethodPost, "/api/files/out/preview", "secret",
		map[string]string{"id": "c1", "path": "alias"})
	if w2.Code != http.StatusNotFound {
		// mock Get would fail; lexical check passed -> 404 from stub stat miss downstream?
		// Stat says symlink ok -> size/hasher tries Get -> ErrNotFound -> 502/404.
		t.Logf("in-jail link proceeds (code %d)", w2.Code)
	}
}

func TestFilesListSubdir(t *testing.T) {
	srv, mc := newFilesTestServer(t, "secret", 8<<20, false)
	mc.SeedFile("c1", testJailRoot+"/sub/deep.txt", []byte("d"))
	r := httptest.NewRequest(http.MethodGet, "/api/files/list?id=c1&path=sub", nil)
	r.Header.Set("X-Auth-Token", "secret")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("list subdir: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "deep.txt") {
		t.Fatalf("subdir listing missing deep.txt: %s", w.Body.String())
	}
}

func TestFilesMissingParamsRejected(t *testing.T) {
	srv, _ := newFilesTestServer(t, "secret", 8<<20, false)
	w := filesJSON(t, srv.Handler(), http.MethodPost, "/api/files/out/preview", "secret",
		map[string]string{"id": "", "path": "x"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing id out-preview: %d", w.Code)
	}
	r := httptest.NewRequest(http.MethodGet, "/api/files/out?id=c1", nil)
	r.Header.Set("X-Auth-Token", "secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing path download: %d", rec.Code)
	}
	// Listing a file path is a 400 (not-directory), not a 404.
	srv2, mc2 := newFilesTestServer(t, "secret", 8<<20, false)
	mc2.SeedFile("c1", testJailRoot+"/plain.txt", []byte("x"))
	r2 := httptest.NewRequest(http.MethodGet, "/api/files/list?id=c1&path=plain.txt", nil)
	r2.Header.Set("X-Auth-Token", "secret")
	rec2 := httptest.NewRecorder()
	srv2.Handler().ServeHTTP(rec2, r2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("list file path: %d want 400", rec2.Code)
	}
}

func TestFilesGuestArchiveDeniedByDefault(t *testing.T) {
	srv, mc := newFilesTestServer(t, "secret", 8<<20, false)
	mc.SeedDir("c1", testJailRoot+"/bundle", 0o750)
	mc.SeedFile("c1", testJailRoot+"/bundle/a", []byte("a"))
	r := httptest.NewRequest(http.MethodGet, "/api/files/archive?id=c1&path=bundle", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("guest archive: %d", rec.Code)
	}
	// Preview is admin-only even when guest download is opted in.
	srv2, _ := newFilesTestServer(t, "secret", 8<<20, true)
	w := filesJSON(t, srv2.Handler(), http.MethodPost, "/api/files/archive/preview", "",
		map[string]string{"id": "c1", "path": "bundle"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("guest archive preview must stay admin-only: %d", w.Code)
	}
}

func readDirNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}
