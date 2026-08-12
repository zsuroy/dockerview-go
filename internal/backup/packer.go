package backup

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"sort"
	"strings"
)

// Fixed in-archive paths (BACKUP_DESIGN §3). verify asserts these names.
const (
	PathManifest   = "manifest.json"
	PathContainers = "containers.json"
	PathRuntime    = "config/runtime.json"
	PathReadme     = "README.txt"
	PathSummaries  = "summaries/"
	PathImages     = "images/"
)

// containerArchive is one entry of containers.json (BACKUP_DESIGN §3.2).
type containerArchive struct {
	ID     string            `json:"id"`
	FullID string            `json:"full_id"`
	Name   string            `json:"name"`
	Image  string            `json:"image"`
	Status string            `json:"status"`
	Labels map[string]string `json:"labels"`
	CPU    string            `json:"cpu,omitempty"`
	Memory string            `json:"memory,omitempty"`
	Ports  []Port            `json:"ports,omitempty"`
}

// runtimeJSON is config/runtime.json (BACKUP_DESIGN §3.4). It intentionally
// has no field capable of carrying a token value.
type runtimeJSON struct {
	Server  runtimeServer     `json:"server"`
	Auth    runtimeAuth       `json:"auth"`
	Audit   runtimeAudit      `json:"audit"`
	Backup  runtimeBackup     `json:"backup"`
	Version DockerviewVersion `json:"version"`
}

type runtimeServer struct {
	Enabled bool `json:"enabled"`
	Port    int  `json:"port"`
}

type runtimeAuth struct {
	TokenMode bool `json:"token_mode"`
}

type runtimeAudit struct {
	Enabled       bool `json:"enabled"`
	RetentionDays int  `json:"retention_days"`
}

type runtimeBackup struct {
	Dir           string `json:"dir"`
	MaxArchives   int    `json:"max_archives"`
	IncludeImages bool   `json:"include_images"`
}

// BuildContainersJSON renders containers.json from snapshots.
func BuildContainersJSON(snaps []ContainerSnapshot) ([]byte, error) {
	out := make([]containerArchive, 0, len(snaps))
	for _, c := range snaps {
		labels := c.Labels
		if labels == nil {
			labels = map[string]string{}
		}
		out = append(out, containerArchive{
			ID: c.ID, FullID: c.FullID, Name: c.Name, Image: c.Image,
			Status: c.Status, Labels: labels, CPU: c.CPU, Memory: c.Memory,
			Ports: c.Ports,
		})
	}
	return marshalIndent(out)
}

// BuildSummary renders one summaries/<file>.json document. Env values are
// redacted here — raw env never reaches the archive.
func BuildSummary(c ContainerSnapshot) ([]byte, error) {
	env, redacted := RedactEnv(c.Env)
	s := ContainerSummary{
		ID:              c.ID,
		Name:            c.Name,
		Image:           c.Image,
		Ports:           c.Ports,
		Mounts:          c.Mounts,
		RestartPolicy:   c.RestartPolicy,
		Networks:        c.Networks,
		Env:             env,
		EnvRedactedKeys: redacted,
	}
	return marshalIndent(s)
}

// SummaryFileName returns the deterministic summaries/ entry name for c.
// Both the id and the name are sanitized: a hostile fixture id like
// "../../evil" must not be able to create traversal entries inside the
// portable archive (zip-slip on extraction elsewhere).
func SummaryFileName(c ContainerSnapshot) string {
	id := c.ID
	if id == "" && c.FullID != "" {
		id = c.FullID
	}
	if len(id) > 12 {
		id = id[:12]
	}
	id = sanitizeSegment(id)
	if id == "" {
		id = "c"
	}
	name := sanitizeSegment(c.Name)
	if name == "" {
		name = "noname"
	}
	if len(name) > 40 {
		name = name[:40]
	}
	return PathSummaries + id + "-" + name + ".json"
}

// BuildRuntimeJSON renders config/runtime.json (non-sensitive parameters).
func BuildRuntimeJSON(rt RuntimeConfig, dir string, maxArchives int, includeImages bool) ([]byte, error) {
	doc := runtimeJSON{
		Server:  runtimeServer{Enabled: true, Port: rt.ServerPort},
		Auth:    runtimeAuth{TokenMode: rt.TokenMode},
		Audit:   runtimeAudit{Enabled: rt.AuditEnabled, RetentionDays: rt.AuditRetentionDays},
		Backup:  runtimeBackup{Dir: dir, MaxArchives: maxArchives, IncludeImages: includeImages},
		Version: DockerviewVersion{Version: rt.Version, Commit: rt.Commit, BuildDate: rt.BuildDate},
	}
	return marshalIndent(doc)
}

// BuildReadme renders README.txt — plain-language unpacking instructions.
func BuildReadme(includeImages bool) []byte {
	var b strings.Builder
	b.WriteString("DockerView 备份快照包 (on-call backup snapshot)\n")
	b.WriteString("====================================================\n")
	b.WriteString("这个 zip 是 DockerView 自动生成的容器现场快照。\n\n")
	b.WriteString("如何查看:\n")
	b.WriteString("  unzip <本文件名>.zip -d out/\n")
	b.WriteString("  先看 manifest.json（清单、选项、文件校验和）；\n")
	b.WriteString("  containers.json 是容器列表；summaries/ 下每个容器一份摘要\n")
	b.WriteString("  （ports / mounts / restart policy / networks / 脱敏 env）。\n\n")
	if includeImages {
		b.WriteString("本包包含镜像导出：images/ 目录下的 tar 可在目标机用\n")
		b.WriteString("  docker load -i images/<file>.tar 导入。\n\n")
	} else {
		b.WriteString("本包不包含镜像层（options.include_images=false）。\n\n")
	}
	b.WriteString("安全说明：包内不含明文密钥——敏感 env 值已掩码为 ***MASKED***，\n")
	b.WriteString("也不含 volume 数据与审计日志。详见 docs/BACKUP_USER_GUIDE.md。\n")
	return []byte(b.String())
}

// SanitizeImageRef converts an image ref into a safe images/ file base name:
// registry.example.com:5000/app:v1 -> registry.example.com_5000_app_v1
func SanitizeImageRef(ref string) string {
	s := sanitizeSegment(ref)
	if s == "" {
		s = "image"
	}
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

// sanitizeSegment keeps only [A-Za-z0-9._-], mapping everything else to '_'.
func sanitizeSegment(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// UniqueImageFileNames assigns collision-free images/<base>.tar names to refs
// (sorted input stays sorted). A suffixed candidate is re-checked against the
// taken set, so a ref whose own sanitized base equals another ref's suffixed
// name (e.g. {"a/b", "a:b", "a_b-01"}) still gets a unique entry.
func UniqueImageFileNames(refs []string) map[string]string {
	out := make(map[string]string, len(refs))
	taken := map[string]bool{}
	for _, ref := range refs {
		base := SanitizeImageRef(ref)
		name := base + ".tar"
		for i := 1; taken[PathImages+name]; i++ {
			name = fmt.Sprintf("%s-%02d.tar", base, i)
		}
		taken[PathImages+name] = true
		out[ref] = PathImages + name
	}
	return out
}

func marshalIndent(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

// hashWriter computes sha256 and counts bytes passing through.
type hashWriter struct {
	h    hash.Hash
	n    int64
	dst  io.Writer
	err  error
	cap  int64 // optional byte cap; 0 = unlimited
	full bool
}

func newHashWriter(dst io.Writer, cap int64) *hashWriter {
	return &hashWriter{h: sha256.New(), dst: dst, cap: cap}
}

func (w *hashWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	if w.cap > 0 && w.n+int64(len(p)) > w.cap {
		// Consume only the acceptable prefix, then trip the limit — the
		// accounting (n, hash, dst) stays consistent with what was written.
		allow := w.cap - w.n
		if allow > 0 {
			_, _ = w.h.Write(p[:allow])
			if w.dst != nil {
				if _, err := w.dst.Write(p[:allow]); err != nil {
					w.err = err
					w.n += allow
					return int(allow), err
				}
			}
			w.n += allow
		}
		w.full = true
		w.err = ErrImageTooLarge
		return int(allow), ErrImageTooLarge
	}
	w.n += int64(len(p))
	_, _ = w.h.Write(p)
	if w.dst != nil {
		n, err := w.dst.Write(p)
		if err != nil {
			w.err = err
			return n, err
		}
	}
	return len(p), nil
}

func (w *hashWriter) Sum() string  { return hex.EncodeToString(w.h.Sum(nil)) }
func (w *hashWriter) Bytes() int64 { return w.n }

// zipPut writes one entry into the zip and returns its FileEntry record.
func zipPut(zw *zip.Writer, path string, data []byte) (FileEntry, error) {
	h := sha256.Sum256(data)
	w, err := zw.Create(path)
	if err != nil {
		return FileEntry{}, fmt.Errorf("zip create %s: %w", path, err)
	}
	if _, err := w.Write(data); err != nil {
		return FileEntry{}, fmt.Errorf("zip write %s: %w", path, err)
	}
	return FileEntry{Path: path, Size: int64(len(data)), SHA256: hex.EncodeToString(h[:])}, nil
}

// sortedKeys returns sorted keys of a path->content map (deterministic order).
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
