package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zsuroy/dockerview-go/internal/audit"
	"github.com/zsuroy/dockerview-go/internal/backup"
)

const drillToken = "super-secret-drill-token-XYZ"

// TestBackup_ArchiveNeverContainsToken is the security red line: create with
// the live server token configured, then scan the downloaded archive (both
// raw bytes and every unzipped file) for the token value.
func TestBackup_ArchiveNeverContainsToken(t *testing.T) {
	s, _ := newBackupTestServer(t, drillToken, backupFixtureProvider())
	w := doJSON(t, s, "POST", "/api/backup/create", `{"note":"token-scan"}`, drillToken)
	if w.Code != http.StatusOK {
		t.Fatalf("create = %d body=%s", w.Code, w.Body.String())
	}
	var rep backup.CreateReport
	_ = json.Unmarshal(w.Body.Bytes(), &rep)

	req := httptest.NewRequest("GET", "/api/backup/download?name="+rep.Name, nil)
	req.Header.Set("X-Auth-Token", drillToken)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("download = %d", rec.Code)
	}
	raw := rec.Body.Bytes()
	if bytes.Contains(raw, []byte(drillToken)) {
		t.Fatal("archive raw bytes contain the server token")
	}

	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	for _, zf := range zr.File {
		rc, err := zf.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(drillToken)) {
			t.Fatalf("token leaked into %s", zf.Name)
		}
		lower := strings.ToLower(string(data))
		for _, pat := range []string{strings.ToLower(drillToken)} {
			if strings.Contains(lower, pat) {
				t.Fatalf("token (case-insensitive) leaked into %s", zf.Name)
			}
		}
	}
}

// TestBackup_AuditPayloadHasNoToken ensures audit rows about backup ops do
// not embed the raw token in their detail/payload.
func TestBackup_AuditPayloadHasNoToken(t *testing.T) {
	s, _ := newBackupTestServer(t, drillToken, backupFixtureProvider())
	_ = doJSON(t, s, "POST", "/api/backup/create", `{}`, drillToken)
	rec := s.aud()
	page, err := rec.List(t.Context(), audit.Query{Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total == 0 {
		t.Fatal("expected audit rows for the backup create")
	}
	for _, it := range page.Items {
		if strings.Contains(it.Detail, drillToken) {
			t.Fatalf("audit detail leaks token: %s", it.Detail)
		}
	}
}

func TestBackup_DownloadDispositionIsSanitized(t *testing.T) {
	s, _ := newBackupTestServer(t, drillToken, backupFixtureProvider())
	w := doJSON(t, s, "POST", "/api/backup/create", `{}`, drillToken)
	var rep backup.CreateReport
	_ = json.Unmarshal(w.Body.Bytes(), &rep)

	req := httptest.NewRequest("GET", "/api/backup/download?name="+rep.Name, nil)
	req.Header.Set("X-Auth-Token", drillToken)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	cd := rec.Header().Get("Content-Disposition")
	if strings.Contains(cd, "..") || strings.Contains(cd, "\n") || strings.Contains(cd, "\r") {
		t.Fatalf("content-disposition not sanitized: %q", cd)
	}
	if !strings.Contains(cd, `filename="`+rep.Name+`"`) {
		t.Fatalf("content-disposition must carry the exact archive name: %q", cd)
	}
}
