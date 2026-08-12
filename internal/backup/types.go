// Package backup implements the on-call backup snapshot feature: it packs a
// point-in-time archive (zip) of container metadata — and optionally exported
// images — into data/backups/ so operators can carry the "scene" away before
// a host rebuild or migration.
//
// Design invariants (see docs/BACKUP_DESIGN.md):
//   - zip format, fixed in-archive layout (manifest.json, containers.json,
//     config/runtime.json, summaries/, README.txt, optional images/)
//   - include_images defaults to false; image export goes through ImageSaver
//     (production Docker SDK, mock for offline verification)
//   - preview never touches disk; create is atomic (temp file + rename)
//   - single-flight create (409), count-based retention, strict archive-name
//     whitelist for download/delete (no path traversal)
//   - secrets never enter the package (env redaction, no token in runtime.json)
package backup

import (
	"context"
	"errors"
	"io"
	"time"
)

// Archive layout and naming constants (asserted by scripts/backup_verify.sh).
const (
	// ArchivePrefix/ArchiveSuffix define the on-disk archive naming scheme:
	// dockerview-backup-<UTC:20060102T150405Z>-<6 hex>.zip
	ArchivePrefix = "dockerview-backup-"
	ArchiveSuffix = ".zip"

	SchemaVersion = 1
	FormatZip     = "zip"

	// MetricsSkipped marks that no persistent metrics store exists in this
	// version; we must NOT fabricate history (see BACKUP_DESIGN §3.6).
	MetricsSkipped = "skipped"
	MetricsReason  = "no persistent metrics storage in this version"

	// NoteMaxChars caps operator notes written into manifest.operator.
	NoteMaxChars = 500

	// DefaultMaxArchives is the default count-based retention limit.
	DefaultMaxArchives = 10

	// MaxImageBytes bounds a single exported image; exceeding it fails the
	// whole create (no half-baked archive is left behind).
	MaxImageBytes int64 = 8 << 30 // 8 GiB
)

// Sentinel errors surfaced to the HTTP layer.
var (
	ErrCreateInProgress = errors.New("a backup create is already in progress")
	ErrInvalidName      = errors.New("invalid backup archive name")
	ErrNotFound         = errors.New("backup archive not found")
	ErrImageExport      = errors.New("image export failed")
	ErrImageTooLarge    = errors.New("image exceeds maximum export size")
)

// Port mirrors docker.PortMapping with stable JSON names for the archive.
type Port struct {
	IP          string `json:"ip,omitempty"`
	PrivatePort uint16 `json:"private_port"`
	PublicPort  uint16 `json:"public_port,omitempty"`
	Type        string `json:"type"`
}

// Mount describes one container mount (paths only, never content).
type Mount struct {
	Type        string `json:"type"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Mode        string `json:"mode,omitempty"`
}

// RestartPolicy is the container restart policy summary.
type RestartPolicy struct {
	Name              string `json:"name"`
	MaximumRetryCount int    `json:"maximum_retry_count"`
}

// ContainerSnapshot is all backup-relevant metadata for one container.
// Env values are RAW here; redaction happens at pack time (see redact.go).
type ContainerSnapshot struct {
	ID            string            `json:"id"`
	FullID        string            `json:"full_id"`
	Name          string            `json:"name"`
	Image         string            `json:"image"`
	Status        string            `json:"status"`
	Labels        map[string]string `json:"labels"`
	CPU           string            `json:"cpu,omitempty"`
	Memory        string            `json:"memory,omitempty"`
	Ports         []Port            `json:"ports,omitempty"`
	Mounts        []Mount           `json:"mounts,omitempty"`
	RestartPolicy RestartPolicy     `json:"restart_policy"`
	Networks      []string          `json:"networks,omitempty"`
	Env           []string          `json:"env,omitempty"`
}

// ImageInfo describes one image candidate for include_images export.
type ImageInfo struct {
	Ref       string `json:"ref"`
	ID        string `json:"id,omitempty"`
	SizeBytes int64  `json:"size_bytes"`
}

// RuntimeConfig is the NON-SENSITIVE runtime parameter summary written to
// config/runtime.json. It must never contain token values (only token_mode).
type RuntimeConfig struct {
	ServerPort         int    `json:"server_port"`
	TokenMode          bool   `json:"token_mode"`
	AuditEnabled       bool   `json:"audit_enabled"`
	AuditRetentionDays int    `json:"audit_retention_days"`
	Version            string `json:"version"`
	Commit             string `json:"commit"`
	BuildDate          string `json:"build_date"`
}

// Options controls what goes into a snapshot package. include_images MUST
// always serialize (bool) so the manifest records the explicit choice.
// include_stopped defaults to false: only running containers are exported;
// set true to also capture stopped/exited containers.
type Options struct {
	IncludeImages  bool `json:"include_images"`
	IncludeStopped bool `json:"include_stopped"`
}

// CreateRequest is the decoded POST /api/backup/create body.
type CreateRequest struct {
	IncludeImages  bool   `json:"include_images"`
	IncludeStopped bool   `json:"include_stopped"`
	Note           string `json:"note"`
}

// FileEntry is one row of manifest.files[].
type FileEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// DockerviewVersion is the tool version block inside the manifest.
type DockerviewVersion struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
}

// HostInfo identifies the host the snapshot was taken on.
type HostInfo struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	GoVersion string `json:"go_version"`
}

// Manifest is the fixed manifest.json schema (BACKUP_DESIGN §3.1).
type Manifest struct {
	SchemaVersion   int               `json:"schema_version"`
	ID              string            `json:"id"`
	CreatedAt       string            `json:"created_at"`
	Dockerview      DockerviewVersion `json:"dockerview"`
	Hostname        string            `json:"hostname"`
	Host            HostInfo          `json:"host"`
	Format          string            `json:"format"`
	Options         Options           `json:"options"`
	Operator        string            `json:"operator,omitempty"`
	Metrics         string            `json:"metrics"`
	MetricsReason   string            `json:"metrics_reason"`
	ContainersCount int               `json:"containers_count"`
	ImagesCount     int               `json:"images_count"`
	Files           []FileEntry       `json:"files"`
	TotalBytes      int64             `json:"total_bytes"`
	Warnings        []string          `json:"warnings"`
}

// ContainerSummary is one summaries/<id>-<name>.json document.
type ContainerSummary struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	Image           string        `json:"image"`
	Ports           []Port        `json:"ports,omitempty"`
	Mounts          []Mount       `json:"mounts,omitempty"`
	RestartPolicy   RestartPolicy `json:"restart_policy"`
	Networks        []string      `json:"networks,omitempty"`
	Env             []string      `json:"env,omitempty"`
	EnvRedactedKeys []string      `json:"env_redacted_keys,omitempty"`
}

// PreviewReport is returned by POST /api/backup/preview (plan only, no disk).
type PreviewReport struct {
	Containers     int         `json:"containers"`
	Images         []ImageInfo `json:"images"`
	EstimatedBytes int64       `json:"estimated_bytes"`
	Options        Options     `json:"options"`
	Warnings       []string    `json:"warnings"`
	WouldInclude   []string    `json:"would_include"`
}

// CreateReport is returned by POST /api/backup/create on success.
type CreateReport struct {
	Name       string   `json:"name"`
	Path       string   `json:"path"`
	SizeBytes  int64    `json:"size_bytes"`
	Containers int      `json:"containers"`
	Images     int      `json:"images"`
	Options    Options  `json:"options"`
	Note       string   `json:"note,omitempty"`
	DurationMs int64    `json:"duration_ms"`
	Warnings   []string `json:"warnings"`
}

// ListItem is one row of GET /api/backup/list.
type ListItem struct {
	Name          string `json:"name"`
	CreatedAt     string `json:"created_at"`
	SizeBytes     int64  `json:"size_bytes"`
	Note          string `json:"note,omitempty"`
	IncludeImages bool   `json:"include_images"`
	Containers    int    `json:"containers"`
	Corrupt       bool   `json:"corrupt,omitempty"`
}

// ListReport is the GET /api/backup/list response.
type ListReport struct {
	Backups     []ListItem `json:"backups"`
	Dir         string     `json:"dir"`
	MaxArchives int        `json:"max_archives"`
}

// ImageSaver exports one image into w (production: `docker save` via SDK).
// It must be replaceable by a mock for offline verification; implementations
// return the number of bytes written.
type ImageSaver interface {
	SaveImage(ctx context.Context, ref string, w io.Writer) (int64, error)
}

// Provider abstracts where container/image metadata comes from (real daemon,
// JSON fixture, or empty), which is what makes mock acceptance possible.
type Provider interface {
	// Snapshot returns metadata for all containers (running and stopped).
	Snapshot(ctx context.Context) ([]ContainerSnapshot, error)
	// Images returns known images with sizes (used for include_images).
	Images(ctx context.Context) ([]ImageInfo, error)
	// Saver returns the image exporter used when include_images is true.
	Saver() ImageSaver
}

// nowUTC is overridable in tests via Manager.now.
func nowUTC() time.Time { return time.Now().UTC() }
