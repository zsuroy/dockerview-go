package backup

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultDir is where snapshot archives live (BACKUP_DESIGN §1).
const DefaultDir = "data/backups"

// ArchiveTempSuffix marks in-flight archives; they never match the whitelist
// and are removed on failure/startup (atomicity guarantee, BACKUP_DESIGN §6).
const ArchiveTempSuffix = ".part"

// archiveNameRe is the strict whitelist for archive file names. Download and
// delete only ever accept names matching it — the first defense against path
// traversal (BACKUP_SECURITY §5).
var archiveNameRe = regexp.MustCompile(`^dockerview-backup-\d{8}T\d{6}Z-[0-9a-f]{6}\.zip$`)

// ValidArchiveName reports whether name is a well-formed archive file name.
func ValidArchiveName(name string) bool {
	return archiveNameRe.MatchString(name)
}

// Config wires a Manager. Dir is created on demand; Now/Hostname are
// injectable for tests.
type Config struct {
	Dir         string
	MaxArchives int
	Provider    Provider
	Runtime     RuntimeConfig
	Hostname    string
	Now         func() time.Time
}

// Manager owns the backup directory and all preview/create/list/download/
// delete operations (BACKUP_DESIGN §6: single-flight create, atomic writes,
// count-based retention).
type Manager struct {
	dir         string
	maxArchives int
	provider    Provider
	runtime     RuntimeConfig
	hostname    string
	now         func() time.Time

	createMu sync.Mutex
	creating bool

	filesMu sync.RWMutex

	// activeCancel cancels the in-flight create, if any. Used by Abort so a
	// graceful shutdown does not wait for the full create timeout.
	activeMu     sync.Mutex
	activeCancel context.CancelFunc
}

// NewManager validates the config, creates the backup directory and removes
// orphaned temp artifacts from previously interrupted runs.
func NewManager(cfg Config) (*Manager, error) {
	dir := cfg.Dir
	if dir == "" {
		dir = DefaultDir
	}
	// Canonicalize: download/delete validate `filepath.Join(dir, name)` against
	// a `dir + separator` prefix, which only holds for a cleaned dir. Trailing
	// slashes (shell tab completion!) or dot segments would otherwise break
	// every download/delete with 400 while create/list keep working.
	dir = filepath.Clean(dir)
	if cfg.MaxArchives <= 0 {
		cfg.MaxArchives = DefaultMaxArchives
	}
	if cfg.Provider == nil {
		return nil, fmt.Errorf("backup: provider is required")
	}
	now := cfg.Now
	if now == nil {
		now = nowUTC
	}
	hostname := cfg.Hostname
	if hostname == "" {
		if h, err := os.Hostname(); err == nil && h != "" {
			hostname = h
		} else {
			hostname = "unknown"
		}
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("backup: mkdir %s: %w", dir, err)
	}
	m := &Manager{
		dir:         dir,
		maxArchives: cfg.MaxArchives,
		provider:    cfg.Provider,
		runtime:     cfg.Runtime,
		hostname:    hostname,
		now:         now,
	}
	m.cleanOrphans()
	return m, nil
}

// Dir returns the backup directory.
func (m *Manager) Dir() string { return m.dir }

// MaxArchives returns the retention limit.
func (m *Manager) MaxArchives() int { return m.maxArchives }

// Abort cancels the in-flight create, if any. Called during graceful shutdown
// so the server does not block on a long image export. The aborted create
// cleans up its temp file via the normal failure path.
func (m *Manager) Abort() {
	m.activeMu.Lock()
	c := m.activeCancel
	m.activeMu.Unlock()
	if c != nil {
		c()
	}
}

// cleanOrphans removes leftover .part temp files from interrupted creates.
func (m *Manager) cleanOrphans() {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, ".tmp-") || strings.HasSuffix(n, ".part") {
			_ = os.RemoveAll(filepath.Join(m.dir, n))
		}
	}
}

// Preview computes the packing plan WITHOUT touching the disk
// (BACKUP_DESIGN §5: zero artifacts in data/backups).
func (m *Manager) Preview(ctx context.Context, opts Options) (*PreviewReport, error) {
	snaps, err := m.provider.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("backup snapshot: %w", err)
	}
	snaps = filterByRunning(snaps, opts.IncludeStopped)

	rep := &PreviewReport{
		Containers:   len(snaps),
		Options:      opts,
		Warnings:     []string{},
		WouldInclude: []string{PathManifest, PathContainers, PathReadme, PathRuntime},
	}
	if len(snaps) > 0 {
		rep.WouldInclude = append(rep.WouldInclude, PathSummaries+"*")
	}

	// Metadata size estimate: containers.json + summaries + runtime + readme.
	est := int64(0)
	if data, err := BuildContainersJSON(snaps); err == nil {
		est += int64(len(data))
	}
	for _, c := range snaps {
		if data, err := BuildSummary(c); err == nil {
			est += int64(len(data))
		}
	}
	if data, err := BuildRuntimeJSON(m.runtime, m.dir, m.maxArchives, opts.IncludeImages); err == nil {
		est += int64(len(data))
	}
	est += int64(len(BuildReadme(opts.IncludeImages))) + 512 // manifest headroom

	if opts.IncludeImages {
		refs := containerImageRefs(snaps)
		images, err := m.provider.Images(ctx)
		if err != nil {
			return nil, fmt.Errorf("backup images: %w", err)
		}
		sizes := make(map[string]int64, len(images))
		ids := make(map[string]string, len(images))
		for _, im := range images {
			sizes[im.Ref] = im.SizeBytes
			ids[im.Ref] = im.ID
		}
		rep.Images = make([]ImageInfo, 0, len(refs))
		imgTotal := int64(0)
		for _, ref := range refs {
			sz := sizes[ref]
			rep.Images = append(rep.Images, ImageInfo{Ref: ref, ID: ids[ref], SizeBytes: sz})
			imgTotal += sz
		}
		est += imgTotal
		rep.Warnings = append(rep.Warnings, fmt.Sprintf(
			"include_images=true: %d image(s) will be exported; estimated total ~%s — check disk space",
			len(refs), humanBytes(est)))
	} else {
		rep.Images = nil
	}

	rep.EstimatedBytes = est
	return rep, nil
}

// filterByRunning keeps only running containers when includeStopped is false.
// When true, all containers (running + stopped/exited) pass through.
func filterByRunning(snaps []ContainerSnapshot, includeStopped bool) []ContainerSnapshot {
	if includeStopped {
		return snaps
	}
	out := snaps[:0:0]
	for _, s := range snaps {
		if isRunningStatus(s.Status) {
			out = append(out, s)
		}
	}
	return out
}

// isRunningStatus reports whether a Docker status string indicates a running
// container. Docker uses "Up ..." for running and "Exited ..."/"Created"/
// "Paused"/"Dead"/"Restarting" for non-running states.
func isRunningStatus(status string) bool {
	return strings.HasPrefix(status, "Up") || strings.HasPrefix(status, "running")
}

// containerImageRefs returns the sorted, de-duplicated image refs referenced
// by the snapshots — the export scope when include_images is true.
func containerImageRefs(snaps []ContainerSnapshot) []string {
	set := map[string]bool{}
	for _, c := range snaps {
		if c.Image != "" {
			set[c.Image] = true
		}
	}
	refs := make([]string, 0, len(set))
	for r := range set {
		refs = append(refs, r)
	}
	sort.Strings(refs)
	return refs
}

// newName builds dockerview-backup-<UTC>-<6hex>.zip from the manager clock.
func (m *Manager) newName() (string, string, error) {
	ts := m.now().UTC().Format("20060102T150405Z")
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", "", fmt.Errorf("backup: shortid: %w", err)
	}
	short := hex.EncodeToString(b[:])
	id := "b-" + strings.ToLower(ts) + "-" + short
	return ArchivePrefix + ts + "-" + short + ArchiveSuffix, id, nil
}

// humanBytes formats byte counts for warnings/previews (1024-based).
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// truncateNote bounds operator notes (rune-safe).
func truncateNote(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > NoteMaxChars {
		r = r[:NoteMaxChars]
	}
	return string(r)
}

// Create builds one snapshot package atomically (BACKUP_DESIGN §6):
// everything is streamed into <name>.zip.part inside the backup dir, then
// os.Rename promotes it. Any failure removes the temp file, so
// data/backups never contains a half-written official archive.
// Concurrent creates are rejected with ErrCreateInProgress (HTTP 409).
func (m *Manager) Create(ctx context.Context, req CreateRequest) (*CreateReport, error) {
	started := time.Now()

	m.createMu.Lock()
	if m.creating {
		m.createMu.Unlock()
		return nil, ErrCreateInProgress
	}
	m.creating = true
	m.createMu.Unlock()
	defer func() {
		m.createMu.Lock()
		m.creating = false
		m.createMu.Unlock()
	}()

	// Register a cancel so Abort (graceful shutdown) can interrupt a long
	// image export instead of blocking up to the full create timeout.
	ctx, cancel := context.WithCancel(ctx)
	m.activeMu.Lock()
	m.activeCancel = cancel
	m.activeMu.Unlock()
	defer func() {
		m.activeMu.Lock()
		m.activeCancel = nil
		m.activeMu.Unlock()
		cancel()
	}()

	note := truncateNote(req.Note)
	opts := Options{IncludeImages: req.IncludeImages, IncludeStopped: req.IncludeStopped}

	snaps, err := m.provider.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("backup snapshot: %w", err)
	}
	snaps = filterByRunning(snaps, opts.IncludeStopped)

	name, id, err := m.newName()
	if err != nil {
		return nil, err
	}
	tmpPath := filepath.Join(m.dir, name+ArchiveTempSuffix)
	finalPath := filepath.Join(m.dir, name)

	success := false
	defer func() {
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("backup: temp file: %w", err)
	}
	fileClosed := false
	defer func() {
		if !fileClosed {
			_ = f.Close()
		}
	}()

	zw := zip.NewWriter(f)
	entries := make([]FileEntry, 0, 8+len(snaps))
	warnings := []string{}

	put := func(path string, data []byte) error {
		fe, err := zipPut(zw, path, data)
		if err != nil {
			return err
		}
		entries = append(entries, fe)
		return nil
	}

	// 1) containers.json
	containersJSON, err := BuildContainersJSON(snaps)
	if err != nil {
		return nil, err
	}
	if err := put(PathContainers, containersJSON); err != nil {
		return nil, err
	}

	// 2) config/runtime.json
	runtimeJSON, err := BuildRuntimeJSON(m.runtime, m.dir, m.maxArchives, opts.IncludeImages)
	if err != nil {
		return nil, err
	}
	if err := put(PathRuntime, runtimeJSON); err != nil {
		return nil, err
	}

	// 3) summaries/<id>-<name>.json (redacted env)
	for _, c := range snaps {
		data, err := BuildSummary(c)
		if err != nil {
			return nil, err
		}
		if err := put(SummaryFileName(c), data); err != nil {
			return nil, err
		}
	}

	// 4) README.txt
	if err := put(PathReadme, BuildReadme(opts.IncludeImages)); err != nil {
		return nil, err
	}

	// 5) images/ (only when include_images=true)
	imagesCount := 0
	if opts.IncludeImages {
		refs := containerImageRefs(snaps)
		fileNames := UniqueImageFileNames(refs)
		saver := m.provider.Saver()
		var failures []string
		for _, ref := range refs {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
			path := fileNames[ref]
			w, err := zw.Create(path)
			if err != nil {
				failures = append(failures, fmt.Sprintf("%s: zip entry: %v", ref, err))
				continue
			}
			hw := newHashWriter(w, MaxImageBytes)
			if _, err := saver.SaveImage(ctx, ref, hw); err != nil {
				failures = append(failures, fmt.Sprintf("%s: %v", ref, err))
				continue
			}
			entries = append(entries, FileEntry{Path: path, Size: hw.Bytes(), SHA256: hw.Sum()})
			imagesCount++
		}
		if len(failures) > 0 {
			// Aggregate and abort the whole create — no half-baked archive.
			return nil, fmt.Errorf("%w: %s", ErrImageExport, strings.Join(failures, "; "))
		}
	}

	// 6) manifest.json last (needs the complete files[] list)
	total := int64(0)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	for _, e := range entries {
		total += e.Size
	}
	manifest := Manifest{
		SchemaVersion: SchemaVersion,
		ID:            id,
		CreatedAt:     m.now().UTC().Format(time.RFC3339),
		Dockerview: DockerviewVersion{
			Version: m.runtime.Version, Commit: m.runtime.Commit, BuildDate: m.runtime.BuildDate,
		},
		Hostname: m.hostname,
		Host:     HostInfo{OS: runtime.GOOS, Arch: runtime.GOARCH, GoVersion: runtime.Version()},
		Format:   FormatZip,
		Options:  opts,
		Operator: note,
		Metrics:  MetricsSkipped, MetricsReason: MetricsReason,
		ContainersCount: len(snaps),
		ImagesCount:     imagesCount,
		Files:           entries,
		TotalBytes:      total,
		Warnings:        warnings,
	}
	manifestJSON, err := marshalIndent(manifest)
	if err != nil {
		return nil, err
	}
	if err := put(PathManifest, manifestJSON); err != nil {
		return nil, err
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("backup: zip close: %w", err)
	}
	if err := f.Sync(); err != nil {
		return nil, fmt.Errorf("backup: sync: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("backup: close: %w", err)
	}
	fileClosed = true

	// Promote atomically, then apply retention (both under the write lock).
	m.filesMu.Lock()
	err = os.Rename(tmpPath, finalPath)
	var removed []string
	if err == nil {
		removed = m.applyRetentionLocked()
	}
	m.filesMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("backup: rename: %w", err)
	}
	success = true
	for _, r := range removed {
		warnings = append(warnings, "retention: removed old archive "+r)
	}

	fi, statErr := os.Stat(finalPath)
	size := int64(0)
	if statErr == nil {
		size = fi.Size()
	}

	return &CreateReport{
		Name:       name,
		Path:       finalPath,
		SizeBytes:  size,
		Containers: len(snaps),
		Images:     imagesCount,
		Options:    opts,
		Note:       note,
		DurationMs: time.Since(started).Milliseconds(),
		Warnings:   warnings,
	}, nil
}

// List returns archives newest-first, reading each manifest for metadata.
// Unreadable archives are reported with corrupt:true instead of dropped.
func (m *Manager) List() (*ListReport, error) {
	m.filesMu.RLock()
	defer m.filesMu.RUnlock()

	names := m.archiveNamesLocked()
	items := make([]ListItem, 0, len(names))
	for i := len(names) - 1; i >= 0; i-- {
		name := names[i]
		full := filepath.Join(m.dir, name)
		fi, err := os.Stat(full)
		if err != nil {
			continue
		}
		it := ListItem{Name: name, SizeBytes: fi.Size()}
		if man, err := readManifest(full); err != nil {
			it.Corrupt = true
		} else {
			it.CreatedAt = man.CreatedAt
			it.Note = man.Operator
			it.IncludeImages = man.Options.IncludeImages
			it.Containers = man.ContainersCount
		}
		items = append(items, it)
	}
	return &ListReport{Backups: items, Dir: m.dir, MaxArchives: m.maxArchives}, nil
}

// OpenPath resolves a download request to an on-disk archive path. The name
// must pass the whitelist regex AND stay inside the backup dir (path
// traversal defense, BACKUP_SECURITY §5).
func (m *Manager) OpenPath(name string) (string, error) {
	if !ValidArchiveName(name) {
		return "", ErrInvalidName
	}
	full := filepath.Join(m.dir, filepath.Base(name))
	if !strings.HasPrefix(full, m.dir+string(os.PathSeparator)) {
		return "", ErrInvalidName
	}
	m.filesMu.RLock()
	defer m.filesMu.RUnlock()
	fi, err := os.Stat(full)
	if err != nil || fi.IsDir() {
		return "", ErrNotFound
	}
	return full, nil
}

// Delete removes one archive after the same whitelist validation.
func (m *Manager) Delete(name string) error {
	if !ValidArchiveName(name) {
		return ErrInvalidName
	}
	full := filepath.Join(m.dir, filepath.Base(name))
	if !strings.HasPrefix(full, m.dir+string(os.PathSeparator)) {
		return ErrInvalidName
	}
	m.filesMu.Lock()
	defer m.filesMu.Unlock()
	if _, err := os.Stat(full); err != nil {
		return ErrNotFound
	}
	return os.Remove(full)
}

// archiveNamesLocked returns official archive names, ascending (file name
// embeds the UTC timestamp, so lexicographic order == chronological order).
func (m *Manager) archiveNamesLocked() []string {
	dirEntries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(dirEntries))
	for _, e := range dirEntries {
		if e.IsDir() {
			continue
		}
		if ValidArchiveName(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// applyRetentionLocked enforces the count limit, deleting oldest first.
// Image-heavy archives are NOT exempt. Call with filesMu held for writing.
//
// Ordering uses file modification time (sub-second precision) rather than the
// embedded name timestamp, which only has second precision: several archives
// created within one second would otherwise tie and the random hex suffix
// could cause a NEWER archive to be pruned before an older one.
func (m *Manager) applyRetentionLocked() []string {
	names := m.archiveNamesByMtimeLocked()
	var removed []string
	for len(names) > m.maxArchives {
		oldest := names[0]
		names = names[1:]
		if err := os.Remove(filepath.Join(m.dir, oldest)); err == nil {
			removed = append(removed, oldest)
		}
	}
	return removed
}

// archiveNamesByMtimeLocked returns official archive names sorted by file
// modification time ascending (oldest first), name as a stable tie-break.
func (m *Manager) archiveNamesByMtimeLocked() []string {
	type entry struct {
		name    string
		modTime time.Time
	}
	var entries []entry
	for _, name := range m.archiveNamesLocked() {
		fi, err := os.Stat(filepath.Join(m.dir, name))
		if err != nil {
			continue
		}
		entries = append(entries, entry{name: name, modTime: fi.ModTime()})
	}
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].modTime.Equal(entries[j].modTime) {
			return entries[i].modTime.Before(entries[j].modTime)
		}
		return entries[i].name < entries[j].name
	})
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.name)
	}
	return names
}

// manifestReadCap bounds the manifest.json entry read during List. A planted
// archive could otherwise zip-bomb the process (small highly-compressed entry
// expanding to gigabytes → OOM on GET /api/backup/list). Real manifests are
// tens of KB even with thousands of files; 4 MiB is far above any sane value.
const manifestReadCap = 4 << 20

// readManifest extracts and decodes manifest.json from an archive.
func readManifest(path string) (*Manifest, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		if zf.Name != PathManifest {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		data, err := io.ReadAll(io.LimitReader(rc, manifestReadCap+1))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > manifestReadCap {
			return nil, fmt.Errorf("manifest.json exceeds %d bytes", manifestReadCap)
		}
		var man Manifest
		if err := json.Unmarshal(data, &man); err != nil {
			return nil, err
		}
		return &man, nil
	}
	return nil, fmt.Errorf("manifest.json not found in %s", path)
}
