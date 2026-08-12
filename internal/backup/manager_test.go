package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeProvider is the test double for Provider.
type fakeProvider struct {
	snaps   []ContainerSnapshot
	images  []ImageInfo
	saver   ImageSaver
	snapErr error
	imgErr  error
}

func (f *fakeProvider) Snapshot(context.Context) ([]ContainerSnapshot, error) {
	if f.snapErr != nil {
		return nil, f.snapErr
	}
	return f.snaps, nil
}

func (f *fakeProvider) Images(context.Context) ([]ImageInfo, error) {
	if f.imgErr != nil {
		return nil, f.imgErr
	}
	return f.images, nil
}

func (f *fakeProvider) Saver() ImageSaver {
	if f.saver == nil {
		return &MockImageSaver{Cfg: MockSaverConfig{BytesPerImage: 2048}}
	}
	return f.saver
}

func newTestManager(t *testing.T, p Provider) *Manager {
	t.Helper()
	dir := t.TempDir()
	m, err := NewManager(Config{
		Dir:         dir,
		MaxArchives: 10,
		Provider:    p,
		Runtime:     RuntimeConfig{ServerPort: 8080, TokenMode: true, Version: "vtest"},
		Hostname:    "test-host",
		Now:         func() time.Time { return time.Date(2026, 8, 12, 8, 30, 15, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func TestPreview_NoDiskWrites(t *testing.T) {
	m := newTestManager(t, &fakeProvider{snaps: sampleSnapshots()})
	before := listDir(t, m.Dir())
	rep, err := m.Preview(context.Background(), Options{IncludeImages: false, IncludeStopped: true})
	if err != nil {
		t.Fatal(err)
	}
	after := listDir(t, m.Dir())
	if before != after {
		t.Fatalf("preview must not write to %s: before=%q after=%q", m.Dir(), before, after)
	}
	if rep.Containers != 2 {
		t.Fatalf("containers = %d, want 2", rep.Containers)
	}
}

func TestPreview_DefaultOptions(t *testing.T) {
	m := newTestManager(t, &fakeProvider{snaps: sampleSnapshots()})
	rep, err := m.Preview(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Options.IncludeImages {
		t.Fatal("include_images must default to false")
	}
	if rep.Images != nil {
		t.Fatalf("images must be nil when include_images=false, got %v", rep.Images)
	}
	if rep.EstimatedBytes <= 0 {
		t.Fatalf("estimated bytes must be positive: %d", rep.EstimatedBytes)
	}
	joined := strings.Join(rep.WouldInclude, ",")
	for _, want := range []string{PathManifest, PathContainers, PathReadme, PathRuntime} {
		if !strings.Contains(joined, want) {
			t.Fatalf("would_include missing %q: %v", want, rep.WouldInclude)
		}
	}
	if !strings.Contains(joined, PathSummaries) {
		t.Fatalf("would_include missing summaries entries: %v", rep.WouldInclude)
	}
}

func TestPreview_IncludeImages(t *testing.T) {
	p := &fakeProvider{
		snaps:  sampleSnapshots(),
		images: []ImageInfo{{Ref: "nginx:1.27", SizeBytes: 187_000_000}, {Ref: "postgres:16", SizeBytes: 432_000_000}, {Ref: "unused:1", SizeBytes: 5}},
	}
	m := newTestManager(t, p)
	rep, err := m.Preview(context.Background(), Options{IncludeImages: true, IncludeStopped: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Images) != 2 {
		t.Fatalf("only container-referenced images expected: %v", rep.Images)
	}
	refs := []string{rep.Images[0].Ref, rep.Images[1].Ref}
	if refs[0] != "nginx:1.27" || refs[1] != "postgres:16" {
		t.Fatalf("sorted refs wrong: %v", refs)
	}
	if rep.Images[0].SizeBytes != 187_000_000 {
		t.Fatalf("size join failed: %v", rep.Images[0])
	}
	if len(rep.Warnings) == 0 || !strings.Contains(rep.Warnings[0], "include_images=true") {
		t.Fatalf("size warning missing: %v", rep.Warnings)
	}
	if rep.EstimatedBytes < 600_000_000 {
		t.Fatalf("estimate must include image sizes: %d", rep.EstimatedBytes)
	}
}

func TestPreview_SnapshotError(t *testing.T) {
	m := newTestManager(t, &fakeProvider{snapErr: errors.New("daemon gone")})
	if _, err := m.Preview(context.Background(), Options{}); err == nil {
		t.Fatal("want error when snapshot fails")
	}
}

func TestValidArchiveName(t *testing.T) {
	valid := []string{
		"dockerview-backup-20260812T083015Z-a1b2c3.zip",
		"dockerview-backup-19990101T000000Z-000000.zip",
	}
	invalid := []string{
		"", "a.zip",
		"../dockerview-backup-20260812T083015Z-a1b2c3.zip",
		"/etc/passwd",
		"dockerview-backup-20260812T083015Z-a1b2c3.zip/../x",
		"dockerview-backup-20260812T083015Z-A1B2C3.zip", // upper hex rejected
		"dockerview-backup-20260812T08301Z-a1b2c3.zip",  // short time
		"dockerview-backup-20260812T083015Z-a1b2c3.tar.gz",
		"xdockerview-backup-20260812T083015Z-a1b2c3.zip",
	}
	for _, n := range valid {
		if !ValidArchiveName(n) {
			t.Fatalf("%q must be valid", n)
		}
	}
	for _, n := range invalid {
		if ValidArchiveName(n) {
			t.Fatalf("%q must be invalid", n)
		}
	}
}

func TestNewManager_CleansOrphans(t *testing.T) {
	dir := t.TempDir()
	orphan1 := filepath.Join(dir, ".tmp-abc.zip.part")
	orphan2 := filepath.Join(dir, "dockerview-backup-20260812T083015Z-ffffff.zip.part")
	keep := filepath.Join(dir, "dockerview-backup-20260812T083015Z-ffffff.zip")
	for _, f := range []string{orphan1, orphan2, keep} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NewManager(Config{Dir: dir, Provider: EmptyProvider{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphan1); !os.IsNotExist(err) {
		t.Fatal(".tmp orphan must be removed")
	}
	if _, err := os.Stat(orphan2); !os.IsNotExist(err) {
		t.Fatal(".part orphan must be removed")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("final archive must survive orphan cleanup")
	}
}

func TestNewManager_Defaults(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(Config{Dir: dir, Provider: EmptyProvider{}})
	if err != nil {
		t.Fatal(err)
	}
	if m.MaxArchives() != DefaultMaxArchives {
		t.Fatalf("default max archives = %d", m.MaxArchives())
	}
	if _, err := NewManager(Config{Dir: dir}); err == nil {
		t.Fatal("missing provider must be rejected")
	}
}

func TestContainerImageRefs_DedupSorted(t *testing.T) {
	snaps := []ContainerSnapshot{
		{Image: "b:1"}, {Image: "a:1"}, {Image: "b:1"}, {Image: ""},
	}
	got := containerImageRefs(snaps)
	if len(got) != 2 || got[0] != "a:1" || got[1] != "b:1" {
		t.Fatalf("refs = %v", got)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KiB", 5 << 20: "5.0 MiB"}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Fatalf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func listDir(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return strings.Join(names, ",")
}
