package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func queryAudit(t *testing.T, srv *Server, action string) []map[string]any {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/api/audit?action="+action+"&limit=200", nil)
	r.Header.Set("X-Auth-Token", "secret")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("audit query %s: %d %s", action, w.Code, w.Body.String())
	}
	var resp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("audit decode: %v body=%s", err, w.Body.String())
	}
	return resp.Items
}

func TestFilesAuditSuccessAndDenied(t *testing.T) {
	srv, mc := newFilesTestServer(t, "secret", 8<<20, false)
	mc.SeedFile("c1", testJailRoot+"/old.txt", []byte("old"))

	// One successful copy in.
	ok := uploadMultipart(t, srv.Handler(), "secret", "c1", "ok.txt", []byte("audit-me"), false)
	if ok.Code != 200 {
		t.Fatal(ok.Code, ok.Body.String())
	}
	// One denied: overwrite without the flag.
	denied := uploadMultipart(t, srv.Handler(), "secret", "c1", "old.txt", []byte("x"), false)
	if denied.Code != http.StatusConflict {
		t.Fatalf("gate: %d", denied.Code)
	}
	// One denied: guest.
	guest := uploadMultipart(t, srv.Handler(), "", "c1", "g.txt", []byte("x"), false)
	if guest.Code != http.StatusUnauthorized {
		t.Fatalf("guest: %d", guest.Code)
	}
	// One successful list and one denied archive (escape).
	lr := httptest.NewRequest(http.MethodGet, "/api/files/list?id=c1", nil)
	lr.Header.Set("X-Auth-Token", "secret")
	lw := httptest.NewRecorder()
	srv.Handler().ServeHTTP(lw, lr)
	if lw.Code != 200 {
		t.Fatalf("list: %d", lw.Code)
	}
	aw := filesJSON(t, srv.Handler(), http.MethodPost, "/api/files/archive/preview", "secret",
		map[string]string{"id": "c1", "path": "../../etc"})
	if aw.Code >= 400 {
		// expected denial, status itself is asserted elsewhere
	} else {
		t.Fatalf("escape archive should fail: %d", aw.Code)
	}

	check := func(action string, wantSuccess, wantDenied int) {
		items := queryAudit(t, srv, action)
		var success, denied int
		for _, it := range items {
			result, _ := it["result"].(string)
			if result == "success" {
				success++
			}
			if result == "denied" {
				denied++
				// Authenticated-but-rejected rows carry the attempted path;
				// pre-auth rows cannot know it and must not be fabricated.
				if cid, _ := it["container_id"].(string); cid != "" {
					if payload, _ := it["payload"].(map[string]any); payload != nil {
						if payload["path"] == nil || payload["path"] == "" {
							t.Errorf("denied %s row for %s missing path payload: %v", action, cid, payload)
						}
					}
				}
			}
		}
		if success < wantSuccess {
			t.Errorf("action %s success rows = %d, want >= %d", action, success, wantSuccess)
		}
		if denied < wantDenied {
			t.Errorf("action %s denied rows = %d, want >= %d", action, denied, wantDenied)
		}
	}
	check("file_in", 1, 2)
	check("file_list", 1, 0)
	check("file_archive", 0, 1)

	// A successful out roundtrip records actor + bytes + sha.
	mc.SeedFile("c1", testJailRoot+"/dl.txt", []byte("download"))
	dr := httptest.NewRequest(http.MethodGet, "/api/files/out?id=c1&path=dl.txt", nil)
	dr.Header.Set("X-Auth-Token", "secret")
	dw := httptest.NewRecorder()
	srv.Handler().ServeHTTP(dw, dr)
	if dw.Code != 200 {
		t.Fatalf("download: %d", dw.Code)
	}
	outItems := queryAudit(t, srv, "file_out")
	var sawBytes bool
	for _, it := range outItems {
		if it["result"] == "success" {
			if p, _ := it["payload"].(map[string]any); p != nil {
				if n, _ := p["bytes"].(float64); n > 0 && p["sha256"] != "" {
					sawBytes = true
				}
			}
		}
	}
	if !sawBytes {
		t.Fatal("no file_out success row with bytes+sha256 payload")
	}
}
