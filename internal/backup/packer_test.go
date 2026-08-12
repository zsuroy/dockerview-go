package backup

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func sampleSnapshots() []ContainerSnapshot {
	return []ContainerSnapshot{
		{
			ID: "abc123def456", FullID: "abc123def456789",
			Name: "web-1", Image: "nginx:1.27", Status: "running",
			Labels: map[string]string{"app": "web"},
			CPU:    "1.2%", Memory: "120.0 MB",
			Ports:         []Port{{IP: "0.0.0.0", PrivatePort: 80, PublicPort: 8080, Type: "tcp"}},
			Mounts:        []Mount{{Type: "bind", Source: "/data/web", Destination: "/usr/share/nginx/html", Mode: "ro"}},
			RestartPolicy: RestartPolicy{Name: "unless-stopped"},
			Networks:      []string{"bridge"},
			Env:           []string{"PATH=/usr/local/sbin:/usr/local/bin", "DB_PASSWORD=hunter2"},
		},
		{
			ID: "bbb222ccc333", FullID: "bbb222ccc333ddd",
			Name: "db/primary", Image: "postgres:16", Status: "exited (0)",
			Labels: nil,
			Env:    []string{"POSTGRES_PASSWORD=secret"},
		},
	}
}

func TestBuildContainersJSON_MinimumFields(t *testing.T) {
	data, err := BuildContainersJSON(sampleSnapshots())
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("containers.json must be valid JSON: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 containers, got %d", len(got))
	}
	for _, c := range got {
		for _, field := range []string{"id", "name", "image", "status", "labels"} {
			if _, ok := c[field]; !ok {
				t.Fatalf("containers.json entry missing %q: %v", field, c)
			}
		}
	}
	if got[1]["labels"].(map[string]any) == nil {
		t.Fatalf("nil labels must serialize as {}")
	}
	// env must NOT leak into containers.json
	if strings.Contains(string(data), "hunter2") || strings.Contains(string(data), "POSTGRES_PASSWORD") {
		t.Fatalf("containers.json must not contain env values")
	}
}

func TestBuildSummary_RedactsEnv(t *testing.T) {
	data, err := BuildSummary(sampleSnapshots()[0])
	if err != nil {
		t.Fatal(err)
	}
	var s ContainerSummary
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range s.Env {
		if strings.HasPrefix(e, "DB_PASSWORD=") {
			found = true
			if e != "DB_PASSWORD="+MaskedValue {
				t.Fatalf("secret env not masked: %q", e)
			}
		}
		if strings.Contains(e, "hunter2") {
			t.Fatalf("raw secret leaked into summary: %q", e)
		}
	}
	if !found {
		t.Fatalf("DB_PASSWORD entry missing after redaction: %v", s.Env)
	}
	if len(s.EnvRedactedKeys) != 1 || s.EnvRedactedKeys[0] != "DB_PASSWORD" {
		t.Fatalf("env_redacted_keys wrong: %v", s.EnvRedactedKeys)
	}
	if s.RestartPolicy.Name != "unless-stopped" || len(s.Mounts) != 1 || len(s.Networks) != 1 {
		t.Fatalf("summary lost fields: %+v", s)
	}
}

func TestSummaryFileName(t *testing.T) {
	cases := []struct {
		snap ContainerSnapshot
		want string
	}{
		{ContainerSnapshot{ID: "abc123def456", Name: "web-1"}, "summaries/abc123def456-web-1.json"},
		{ContainerSnapshot{ID: "abc", Name: "db/primary: x"}, "summaries/abc-db_primary__x.json"},
		{ContainerSnapshot{FullID: "0123456789abcdef", Name: ""}, "summaries/0123456789ab-noname.json"},
		{ContainerSnapshot{}, "summaries/c-noname.json"},
		// hostile ids must be sanitized — '/' becomes '_', so no traversal
		// entries can be created inside the portable archive (zip-slip guard)
		{ContainerSnapshot{ID: "../../evil", Name: "x"}, "summaries/.._.._evil-x.json"},
		{ContainerSnapshot{ID: "a/b", Name: "y"}, "summaries/a_b-y.json"},
	}
	for _, tc := range cases {
		got := SummaryFileName(tc.snap)
		if got != tc.want {
			t.Fatalf("SummaryFileName(%+v) = %q, want %q", tc.snap, got, tc.want)
		}
		// beyond summaries/, no path separator may survive
		if strings.Contains(strings.TrimPrefix(got, PathSummaries), "/") {
			t.Fatalf("path separator leaked into summary file name %q", got)
		}
	}
	// long names are truncated to 40 chars
	long := SummaryFileName(ContainerSnapshot{ID: "x", Name: strings.Repeat("a", 80)})
	base := strings.TrimSuffix(strings.TrimPrefix(long, "summaries/x-"), ".json")
	if len(base) != 40 {
		t.Fatalf("name must truncate to 40 chars, got %d (%q)", len(base), base)
	}
}

func TestSanitizeImageRef(t *testing.T) {
	cases := map[string]string{
		"nginx:1.27":                         "nginx_1.27",
		"registry.example.com:5000/app:v1.2": "registry.example.com_5000_app_v1.2",
		"sha256:abcdef":                      "sha256_abcdef",
		"":                                   "image",
		strings.Repeat("x", 200):             strings.Repeat("x", 120),
	}
	for in, want := range cases {
		if got := SanitizeImageRef(in); got != want {
			t.Fatalf("SanitizeImageRef(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUniqueImageFileNames(t *testing.T) {
	refs := []string{"a/b:v1", "a_b_v1", "c:v2"}
	got := UniqueImageFileNames(refs)
	if got["a/b:v1"] == got["a_b_v1"] {
		t.Fatalf("collision not resolved: %v", got)
	}
	for _, ref := range refs {
		if !strings.HasPrefix(got[ref], PathImages) || !strings.HasSuffix(got[ref], ".tar") {
			t.Fatalf("bad images path for %q: %q", ref, got[ref])
		}
	}
}

// TestUniqueImageFileNames_SuffixCollision guards the case where a suffixed
// candidate collides with another ref's own sanitized base.
func TestUniqueImageFileNames_SuffixCollision(t *testing.T) {
	// "a/b" and "a:b" both sanitize to "a_b"; "a_b-01" is the exact name the
	// disambiguation suffix would generate for the second one.
	refs := []string{"a/b", "a:b", "a_b-01"}
	got := UniqueImageFileNames(refs)
	seen := map[string]string{}
	for _, ref := range refs {
		path := got[ref]
		if prev, dup := seen[path]; dup {
			t.Fatalf("duplicate entry %q for refs %q and %q", path, prev, ref)
		}
		seen[path] = ref
	}
	if len(seen) != 3 {
		t.Fatalf("want 3 distinct paths, got %v", got)
	}
}

func TestBuildRuntimeJSON_NoTokenLeak(t *testing.T) {
	rt := RuntimeConfig{ServerPort: 8080, TokenMode: true, AuditEnabled: true,
		AuditRetentionDays: 90, Version: "v0.9.0", Commit: "abc", BuildDate: "2026-08-12"}
	data, err := BuildRuntimeJSON(rt, "data/backups", 10, false)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	auth := doc["auth"].(map[string]any)
	if auth["token_mode"] != true {
		t.Fatalf("auth.token_mode missing: %v", auth)
	}
	if len(auth) != 1 {
		t.Fatalf("auth block must only contain token_mode, got %v", auth)
	}
	bk := doc["backup"].(map[string]any)
	if bk["dir"] != "data/backups" || bk["max_archives"] != float64(10) || bk["include_images"] != false {
		t.Fatalf("backup block wrong: %v", bk)
	}
}

func TestBuildReadme(t *testing.T) {
	plain := string(BuildReadme(false))
	withImg := string(BuildReadme(true))
	for _, want := range []string{"unzip", "manifest.json", "summaries"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("README.txt missing %q", want)
		}
	}
	if !strings.Contains(withImg, "docker load") {
		t.Fatalf("include_images README must mention docker load")
	}
	if strings.Contains(plain, "docker load") {
		t.Fatalf("default README must not mention docker load")
	}
}

func TestZipPut_RecordsHashAndSize(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	data := []byte("hello dockerview")
	fe, err := zipPut(zw, "manifest.json", data)
	if err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if fe.Size != int64(len(data)) {
		t.Fatalf("size = %d, want %d", fe.Size, len(data))
	}
	sum := sha256.Sum256(data)
	if fe.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha256 mismatch: %s", fe.SHA256)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if zr.File[0].Name != "manifest.json" {
		t.Fatalf("entry name = %q", zr.File[0].Name)
	}
}

func TestHashWriter_CapTripsTooLarge(t *testing.T) {
	w := newHashWriter(nil, 10)
	n, err := w.Write([]byte("01234"))
	if err != nil || n != 5 {
		t.Fatalf("first write: n=%d err=%v", n, err)
	}
	if _, err := w.Write([]byte("56789abcdef")); !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("want ErrImageTooLarge, got %v", err)
	}
	if _, err := w.Write([]byte("x")); !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("writer must stay tripped, got %v", err)
	}
}

func TestSortedKeys(t *testing.T) {
	m := map[string][]byte{"b": nil, "a": nil, "c": nil}
	got := sortedKeys(m)
	if got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("sortedKeys = %v", got)
	}
}
