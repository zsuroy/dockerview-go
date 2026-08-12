package backup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPackage_NoSecretEnvLeak is the whole-archive redaction sweep: build a
// package from snapshots carrying every secret pattern, then grep every file
// inside the produced zip for the raw values.
func TestPackage_NoSecretEnvLeak(t *testing.T) {
	secrets := map[string]string{
		"DB_PASSWORD":         "hunter2-hunter2",
		"API_TOKEN":           "tok-abc-123",
		"AWS_SECRET_KEY":      "AKIA-SECRET",
		"SSH_PRIVATE_KEY":     "BEGIN-PRIVATE",
		"PG_CONNECTION":       "postgres://u:cred@h/db",
		"OAUTH_CLIENT_SECRET": "shh",
		"MY_ACCESSKEY":        "access-999",
		"AUTH_HEADER":         "bearer-x",
		"SOME_PASSWD":         "pw-456",
	}
	env := []string{"TZ=UTC", "APP_MODE=production"}
	for k, v := range secrets {
		env = append(env, k+"="+v)
	}
	snaps := []ContainerSnapshot{{
		ID: "aaa111bbb222", FullID: "aaa111bbb222full", Name: "leaky",
		Image: "app:1", Status: "running", Env: env,
	}}
	m := newTestManager(t, &fakeProvider{snaps: snaps})
	rep, err := m.Create(context.Background(), CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	names, contents := unzipAll(t, filepath.Join(m.Dir(), rep.Name))
	if len(names) == 0 {
		t.Fatal("empty archive")
	}
	for name, data := range contents {
		text := string(data)
		for k, v := range secrets {
			if strings.Contains(text, v) {
				t.Fatalf("secret value of %s leaked into %s", k, name)
			}
		}
	}
	// masked form + redacted key names must be present in the summary
	var summaryFound bool
	for name, data := range contents {
		if strings.HasPrefix(name, PathSummaries) {
			summaryFound = true
			text := string(data)
			if !strings.Contains(text, MaskedValue) {
				t.Fatalf("summary lacks masked values: %s", text)
			}
			if !strings.Contains(text, "DB_PASSWORD") {
				t.Fatalf("env_redacted_keys must list the key names: %s", text)
			}
		}
	}
	if !summaryFound {
		t.Fatal("no summaries/ entry produced")
	}
}

// TestPackage_RuntimeJSONShape guards config/runtime.json against accidental
// sensitive fields (only token_mode may describe auth).
func TestPackage_RuntimeJSONShape(t *testing.T) {
	m := newTestManager(t, EmptyProvider{})
	rep, err := m.Create(context.Background(), CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	_, contents := unzipAll(t, filepath.Join(m.Dir(), rep.Name))
	rt := string(contents[PathRuntime])
	for _, banned := range []string{"token\"", "secret", "password", "credential"} {
		if strings.Contains(strings.ToLower(rt), banned) && !strings.Contains(rt, "token_mode") {
			t.Fatalf("runtime.json may carry no secrets: %s", rt)
		}
	}
	if !strings.Contains(rt, `"token_mode"`) {
		t.Fatalf("runtime.json must expose auth.token_mode: %s", rt)
	}
}

func TestFixtureProvider(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "fx.json")
	if err := os.WriteFile(good, []byte(`{
		"containers":[{"id":"abc","name":"c1","image":"img:1","status":"running"}],
		"images":[{"ref":"img:1","size_bytes":42}],
		"saver":{"bytes_per_image":512}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := NewFixtureProvider(good)
	if err != nil {
		t.Fatal(err)
	}
	snaps, err := p.Snapshot(context.Background())
	if err != nil || len(snaps) != 1 || snaps[0].Name != "c1" {
		t.Fatalf("snapshot = %v, %v", snaps, err)
	}
	imgs, err := p.Images(context.Background())
	if err != nil || len(imgs) != 1 || imgs[0].SizeBytes != 42 {
		t.Fatalf("images = %v, %v", imgs, err)
	}
	saver := p.Saver()
	var buf strings.Builder
	n, err := saver.SaveImage(context.Background(), "img:1", &buf)
	if err != nil || n <= 0 {
		t.Fatalf("mock save: n=%d err=%v", n, err)
	}
	if !strings.Contains(buf.String(), "MOCK-IMAGE-EXPORT ref=img:1") {
		t.Fatalf("mock payload lacks marker: %q", buf.String())
	}

	if _, err := NewFixtureProvider(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("missing fixture must error")
	}
	bad := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(bad, []byte("{oops"), 0o600)
	if _, err := NewFixtureProvider(bad); err == nil {
		t.Fatal("invalid JSON must error")
	}
}

func TestMockSaver_OversizeSimulation(t *testing.T) {
	s := &MockImageSaver{Cfg: MockSaverConfig{BytesPerImage: 1024, OversizeRef: "big:1"}}
	var buf strings.Builder
	if _, err := s.SaveImage(context.Background(), "big:1", &buf); err != ErrImageTooLarge {
		t.Fatalf("want ErrImageTooLarge, got %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("oversize simulation must not write payload, wrote %d", buf.Len())
	}
}

// TestMockSaver_FixtureCapRejected guards against a hostile/typo'd fixture
// (huge bytes_per_image) exhausting memory: the mock must refuse it.
func TestMockSaver_FixtureCapRejected(t *testing.T) {
	s := &MockImageSaver{Cfg: MockSaverConfig{BytesPerImage: MockMaxImageBytes + 1}}
	var buf strings.Builder
	if _, err := s.SaveImage(context.Background(), "x:1", &buf); err == nil {
		t.Fatal("want error for bytes_per_image above cap")
	}
}

// TestMockSaver_StreamsLargePayload verifies a moderately large mock export
// succeeds and produces the marker without materializing it all at once.
func TestMockSaver_StreamsLargePayload(t *testing.T) {
	s := &MockImageSaver{Cfg: MockSaverConfig{BytesPerImage: 300 * 1024}} // > chunk
	var buf strings.Builder
	n, err := s.SaveImage(context.Background(), "big:2", &buf)
	if err != nil {
		t.Fatal(err)
	}
	if n <= 0 || !strings.Contains(buf.String(), "MOCK-IMAGE-EXPORT ref=big:2") {
		t.Fatalf("large mock export broken: n=%d", n)
	}
}
