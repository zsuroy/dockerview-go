package backup

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCreate_Default_NoImagesDir(t *testing.T) {
	m := newTestManager(t, &fakeProvider{snaps: sampleSnapshots()})
	rep, err := m.Create(context.Background(), CreateRequest{Note: "交接：重装前快照", IncludeStopped: true})
	if err != nil {
		t.Fatal(err)
	}
	if !ValidArchiveName(rep.Name) {
		t.Fatalf("bad archive name %q", rep.Name)
	}
	if rep.Options.IncludeImages {
		t.Fatal("include_images must default to false")
	}
	if rep.Containers != 2 || rep.Images != 0 {
		t.Fatalf("report counts wrong: %+v", rep)
	}
	if rep.Note != "交接：重装前快照" {
		t.Fatalf("note lost: %q", rep.Note)
	}

	names, contents := unzipAll(t, filepath.Join(m.Dir(), rep.Name))
	for _, want := range []string{PathManifest, PathContainers, PathReadme, PathRuntime} {
		if _, ok := contents[want]; !ok {
			t.Fatalf("archive missing %q; has %v", want, names)
		}
	}
	for _, n := range names {
		if strings.HasPrefix(n, PathImages) {
			t.Fatalf("default archive must contain no images/ entries, found %q", n)
		}
		if strings.HasPrefix(n, PathSummaries) == false &&
			!map[string]bool{PathManifest: true, PathContainers: true, PathReadme: true, PathRuntime: true}[n] {
			t.Fatalf("unexpected entry %q", n)
		}
	}
	summaries := 0
	for _, n := range names {
		if strings.HasPrefix(n, PathSummaries) {
			summaries++
		}
	}
	if summaries != 2 {
		t.Fatalf("want 2 summaries, got %d", summaries)
	}

	var man Manifest
	if err := jsonUnmarshal(contents[PathManifest], &man); err != nil {
		t.Fatalf("manifest invalid: %v", err)
	}
	assertManifestBasics(t, &man, 2, 0)
	// manifest.files mirrors the zip contents except manifest.json itself
	// (a manifest cannot hash itself; zip CRC covers it — DESIGN §3.1)
	if len(man.Files) != len(names)-1 {
		t.Fatalf("manifest files %d vs zip entries %d (minus manifest)", len(man.Files), len(names))
	}
	for _, fe := range man.Files {
		data, ok := contents[fe.Path]
		if !ok {
			t.Fatalf("manifest references missing entry %q", fe.Path)
		}
		if int64(len(data)) != fe.Size {
			t.Fatalf("size mismatch for %s: manifest %d vs actual %d", fe.Path, fe.Size, len(data))
		}
	}
	// no secret may survive into the package
	for name, data := range contents {
		if strings.Contains(string(data), "hunter2") || strings.Contains(string(data), "=secret") {
			t.Fatalf("secret leaked into %s", name)
		}
	}
}

func TestCreate_EmptyProvider(t *testing.T) {
	m := newTestManager(t, EmptyProvider{})
	rep, err := m.Create(context.Background(), CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	_, contents := unzipAll(t, filepath.Join(m.Dir(), rep.Name))
	var man Manifest
	if err := jsonUnmarshal(contents[PathManifest], &man); err != nil {
		t.Fatal(err)
	}
	if man.ContainersCount != 0 || man.ImagesCount != 0 {
		t.Fatalf("empty provider must yield zero counts: %+v", man)
	}
}

func TestCreate_IncludeImagesMock(t *testing.T) {
	p := &fakeProvider{
		snaps:  sampleSnapshots(),
		images: []ImageInfo{{Ref: "nginx:1.27", SizeBytes: 100}, {Ref: "postgres:16", SizeBytes: 200}},
		saver:  &MockImageSaver{Cfg: MockSaverConfig{BytesPerImage: 2048}},
	}
	m := newTestManager(t, p)
	rep, err := m.Create(context.Background(), CreateRequest{IncludeImages: true, IncludeStopped: true, Note: "with images"})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Options.IncludeImages {
		t.Fatal("report must echo include_images=true")
	}
	if rep.Images != 2 {
		t.Fatalf("images exported = %d, want 2", rep.Images)
	}

	names, contents := unzipAll(t, filepath.Join(m.Dir(), rep.Name))
	var imgEntries []string
	for _, n := range names {
		if strings.HasPrefix(n, PathImages) && strings.HasSuffix(n, ".tar") {
			imgEntries = append(imgEntries, n)
		}
	}
	if len(imgEntries) != 2 {
		t.Fatalf("want 2 images/*.tar entries, got %v", imgEntries)
	}
	// mock saver payload must be identifiable
	for _, n := range imgEntries {
		if !strings.Contains(string(contents[n]), "MOCK-IMAGE-EXPORT ref=") {
			t.Fatalf("image tar %s lacks mock marker", n)
		}
	}

	var man Manifest
	if err := jsonUnmarshal(contents[PathManifest], &man); err != nil {
		t.Fatal(err)
	}
	assertManifestBasics(t, &man, 2, 2)
	if !man.Options.IncludeImages {
		t.Fatal("manifest.options.include_images must be true")
	}
	// every images/ entry must be tracked in manifest.files with a sha256
	tracked := map[string]FileEntry{}
	for _, fe := range man.Files {
		tracked[fe.Path] = fe
	}
	for _, n := range imgEntries {
		fe, ok := tracked[n]
		if !ok {
			t.Fatalf("manifest.files missing %q", n)
		}
		if fe.SHA256 == "" || fe.Size <= 0 {
			t.Fatalf("manifest entry incomplete for %q: %+v", n, fe)
		}
	}
}

func TestCreate_AtomicFailureLeavesNothing(t *testing.T) {
	p := &fakeProvider{
		snaps: sampleSnapshots(),
		saver: &MockImageSaver{Cfg: MockSaverConfig{BytesPerImage: 1024, FailRefs: []string{"nginx:1.27"}}},
	}
	m := newTestManager(t, p)
	_, err := m.Create(context.Background(), CreateRequest{IncludeImages: true})
	if !errors.Is(err, ErrImageExport) {
		t.Fatalf("want ErrImageExport, got %v", err)
	}
	if !strings.Contains(err.Error(), "nginx:1.27") {
		t.Fatalf("aggregated error must name the failing ref: %v", err)
	}
	entries, _ := os.ReadDir(m.Dir())
	for _, e := range entries {
		t.Fatalf("no file may remain after a failed create, found %q", e.Name())
	}
}

func TestCreate_OversizeImageFails(t *testing.T) {
	p := &fakeProvider{
		snaps: sampleSnapshots(),
		saver: &MockImageSaver{Cfg: MockSaverConfig{BytesPerImage: 1024, OversizeRef: "postgres:16"}},
	}
	m := newTestManager(t, p)
	_, err := m.Create(context.Background(), CreateRequest{IncludeImages: true, IncludeStopped: true})
	if !errors.Is(err, ErrImageExport) && !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("want export/too-large error, got %v", err)
	}
	entries, _ := os.ReadDir(m.Dir())
	if len(entries) != 0 {
		t.Fatalf("failed create must not leave artifacts: %v", entries)
	}
}

func TestCreate_NoteTruncatedTo500(t *testing.T) {
	m := newTestManager(t, EmptyProvider{})
	long := strings.Repeat("值", NoteMaxChars+100)
	rep, err := m.Create(context.Background(), CreateRequest{Note: long})
	if err != nil {
		t.Fatal(err)
	}
	if got := len([]rune(rep.Note)); got != NoteMaxChars {
		t.Fatalf("note length = %d, want %d", got, NoteMaxChars)
	}
}

func TestCreate_ConcurrencySingleFlight(t *testing.T) {
	gate := make(chan struct{})
	blocking := &gatedSaver{gate: gate, entered: make(chan struct{}),
		inner: &MockImageSaver{Cfg: MockSaverConfig{BytesPerImage: 512}}}
	p := &fakeProvider{snaps: sampleSnapshots(), saver: blocking}
	m := newTestManager(t, p)

	first := make(chan error, 1)
	go func() {
		_, err := m.Create(context.Background(), CreateRequest{IncludeImages: true})
		first <- err
	}()

	// Wait until the first create is inside the saver, then fire the second.
	<-blocking.entered
	_, err := m.Create(context.Background(), CreateRequest{})
	if !errors.Is(err, ErrCreateInProgress) {
		t.Fatalf("second concurrent create must fail with ErrCreateInProgress, got %v", err)
	}
	close(gate)
	if err := <-first; err != nil {
		t.Fatalf("first create failed: %v", err)
	}
	// After completion a new create works again.
	if _, err := m.Create(context.Background(), CreateRequest{}); err != nil {
		t.Fatalf("create after completion must work: %v", err)
	}
}

// gatedSaver blocks its first SaveImage until gate is closed, signalling via
// entered; later calls pass straight through.
type gatedSaver struct {
	gate    chan struct{}
	entered chan struct{}
	inner   ImageSaver
	mu      sync.Mutex
	first   bool
}

func (g *gatedSaver) SaveImage(ctx context.Context, ref string, w io.Writer) (int64, error) {
	g.mu.Lock()
	isFirst := !g.first
	g.first = true
	g.mu.Unlock()
	if isFirst {
		close(g.entered)
		<-g.gate
	}
	return g.inner.SaveImage(ctx, ref, w)
}

func TestRetention_CountLimit(t *testing.T) {
	dir := t.TempDir()
	clock := time.Date(2026, 8, 12, 8, 0, 0, 0, time.UTC)
	m, err := NewManager(Config{
		Dir: dir, MaxArchives: 3, Provider: EmptyProvider{},
		Now: func() time.Time { clock = clock.Add(time.Minute); return clock },
	})
	if err != nil {
		t.Fatal(err)
	}
	var all []string
	for i := 0; i < 5; i++ {
		rep, err := m.Create(context.Background(), CreateRequest{Note: fmt.Sprintf("n%d", i)})
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, rep.Name)
	}
	got := listDir(t, dir)
	remaining := strings.Split(got, ",")
	if len(remaining) != 3 {
		t.Fatalf("retention must keep 3 archives, got %v", remaining)
	}
	// the two OLDEST must be gone
	for _, old := range all[:2] {
		if strings.Contains(got, old) {
			t.Fatalf("oldest archive %s survived retention", old)
		}
	}
	for _, fresh := range all[2:] {
		if !strings.Contains(got, fresh) {
			t.Fatalf("recent archive %s missing", fresh)
		}
	}
}

func TestRetention_SameSecondKeepsNewest(t *testing.T) {
	// Several archives created within the SAME second: name timestamps tie, so
	// retention must fall back to file mtime and keep the truly newest.
	dir := t.TempDir()
	fixed := time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC)
	m, err := NewManager(Config{
		Dir: dir, MaxArchives: 2, Provider: EmptyProvider{},
		Now: func() time.Time { return fixed }, // identical timestamp every create
	})
	if err != nil {
		t.Fatal(err)
	}
	var created []string
	for i := 0; i < 4; i++ {
		rep, err := m.Create(context.Background(), CreateRequest{Note: fmt.Sprintf("s%d", i)})
		if err != nil {
			t.Fatal(err)
		}
		created = append(created, rep.Name)
		time.Sleep(15 * time.Millisecond) // ensure distinct mtimes
	}
	got := listDir(t, dir)
	remaining := strings.Split(got, ",")
	if len(remaining) != 2 {
		t.Fatalf("retention must keep 2 archives, got %v", remaining)
	}
	// the last two created must survive
	for _, fresh := range created[2:] {
		if !strings.Contains(got, fresh) {
			t.Fatalf("newest archive %s was wrongly pruned; dir=%v", fresh, remaining)
		}
	}
}

func TestList_NewestFirstAndCorrupt(t *testing.T) {
	m := newTestManager(t, &fakeProvider{snaps: sampleSnapshots()})
	rep1, err := m.Create(context.Background(), CreateRequest{Note: "first", IncludeStopped: true})
	if err != nil {
		t.Fatal(err)
	}
	// a corrupt file with a valid archive name
	corruptName := "dockerview-backup-20260812T093015Z-abcdef.zip"
	if err := os.WriteFile(filepath.Join(m.Dir(), corruptName), []byte("not a zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	lr, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(lr.Backups) != 2 {
		t.Fatalf("want 2 entries, got %d", len(lr.Backups))
	}
	if lr.Backups[0].Name != corruptName || !lr.Backups[0].Corrupt {
		t.Fatalf("newest (corrupt) entry wrong: %+v", lr.Backups[0])
	}
	if lr.Backups[1].Name != rep1.Name || lr.Backups[1].Corrupt {
		t.Fatalf("second entry wrong: %+v", lr.Backups[1])
	}
	if lr.Backups[1].Note != "first" || lr.Backups[1].Containers != 2 {
		t.Fatalf("manifest metadata not surfaced: %+v", lr.Backups[1])
	}
	if lr.MaxArchives != 10 || lr.Dir != m.Dir() {
		t.Fatalf("list report meta wrong: %+v", lr)
	}
}

func TestOpenPath_AndDelete(t *testing.T) {
	m := newTestManager(t, &fakeProvider{snaps: sampleSnapshots()})
	rep, err := m.Create(context.Background(), CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	full, err := m.OpenPath(rep.Name)
	if err != nil {
		t.Fatalf("OpenPath: %v", err)
	}
	if filepath.Dir(full) != m.Dir() {
		t.Fatalf("resolved outside backup dir: %s", full)
	}

	// traversal attempts must all fail with ErrInvalidName
	for _, evil := range []string{
		"../secret.zip",
		rep.Name + "/../x",
		"/etc/passwd",
		"..%2f..%2fetc/passwd",
		"dockerview-backup-20260812T083015Z-zzzzzz.zip", // bad hex
	} {
		if _, err := m.OpenPath(evil); !errors.Is(err, ErrInvalidName) {
			t.Fatalf("OpenPath(%q) must be ErrInvalidName, got %v", evil, err)
		}
		if err := m.Delete(evil); !errors.Is(err, ErrInvalidName) {
			t.Fatalf("Delete(%q) must be ErrInvalidName, got %v", evil, err)
		}
	}

	if err := m.Delete(rep.Name); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := m.OpenPath(rep.Name); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted archive must 404, got %v", err)
	}
	if err := m.Delete(rep.Name); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double delete must 404, got %v", err)
	}
}

// --- helpers ---

func unzipAll(t *testing.T, path string) ([]string, map[string][]byte) {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open zip %s: %v", path, err)
	}
	defer zr.Close()
	names := make([]string, 0, len(zr.File))
	contents := map[string][]byte{}
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
		names = append(names, zf.Name)
		contents[zf.Name] = data
	}
	sort.Strings(names)
	return names, contents
}

func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func assertManifestBasics(t *testing.T, man *Manifest, containers, images int) {
	t.Helper()
	if man.SchemaVersion != SchemaVersion {
		t.Fatalf("schema_version = %d", man.SchemaVersion)
	}
	if man.Format != FormatZip {
		t.Fatalf("format = %q", man.Format)
	}
	if man.Options.IncludeImages != (images > 0) {
		t.Fatalf("options.include_images = %v for %d images", man.Options.IncludeImages, images)
	}
	if man.Metrics != MetricsSkipped {
		t.Fatalf("metrics = %q, want skipped", man.Metrics)
	}
	if man.MetricsReason == "" {
		t.Fatal("metrics_reason must explain the skip")
	}
	if man.CreatedAt == "" || man.Hostname == "" || man.ID == "" {
		t.Fatalf("manifest identity fields missing: %+v", man)
	}
	if _, err := time.Parse(time.RFC3339, man.CreatedAt); err != nil {
		t.Fatalf("created_at not RFC3339: %q", man.CreatedAt)
	}
	if man.ContainersCount != containers || man.ImagesCount != images {
		t.Fatalf("counts = %d/%d, want %d/%d", man.ContainersCount, man.ImagesCount, containers, images)
	}
	if man.Files == nil || man.Warnings == nil {
		t.Fatal("files/warnings must be present (non-nil)")
	}
	if man.Host.OS == "" || man.Host.Arch == "" || man.Host.GoVersion == "" {
		t.Fatalf("host info missing: %+v", man.Host)
	}
}

// TestNewManager_NormalizesDir ensures a trailing-slash / non-canonical dir
// (shell tab completion!) does not break the download/delete prefix check.
func TestNewManager_NormalizesDir(t *testing.T) {
	base := t.TempDir()
	m, err := NewManager(Config{
		Dir:      base + "/backups/", // trailing slash
		Provider: EmptyProvider{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(m.Dir(), "/") {
		t.Fatalf("dir must be cleaned, got %q", m.Dir())
	}
	// create then resolve+delete must work despite the trailing slash input
	m2, err := NewManager(Config{Dir: m.Dir(), Provider: &fakeProvider{snaps: sampleSnapshots()}})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := m2.Create(context.Background(), CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m2.OpenPath(rep.Name); err != nil {
		t.Fatalf("OpenPath broken for cleaned dir: %v", err)
	}
	if err := m2.Delete(rep.Name); err != nil {
		t.Fatalf("Delete broken for cleaned dir: %v", err)
	}
}

// TestCreate_AbortInterrupts verifies Abort cancels an in-flight create and
// leaves no official archive behind.
func TestCreate_AbortInterrupts(t *testing.T) {
	gate := make(chan struct{})
	blocking := &gatedSaver{gate: gate, entered: make(chan struct{}),
		inner: &MockImageSaver{Cfg: MockSaverConfig{BytesPerImage: 512}}}
	p := &fakeProvider{snaps: sampleSnapshots(), saver: blocking}
	m := newTestManager(t, p)

	done := make(chan error, 1)
	go func() {
		_, err := m.Create(context.Background(), CreateRequest{IncludeImages: true, IncludeStopped: true})
		done <- err
	}()
	<-blocking.entered
	m.Abort()
	close(gate) // let the saver proceed; the context is already cancelled

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("aborted create must return an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Abort did not interrupt the in-flight create")
	}
	entries, _ := os.ReadDir(m.Dir())
	for _, e := range entries {
		t.Fatalf("aborted create must leave no archive, found %q", e.Name())
	}
}
