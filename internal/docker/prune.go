// Package docker prune support: list/dry-run/confirm cleanup of dangling images
// and unused (dangling) volumes with a preview-before-delete workflow.
//
// The destructive path is intentionally conservative: candidates are re-fetched
// at confirm time, a dry-run fingerprint must match, and each removal uses
// non-force options so the Docker daemon refuses anything that became in use.
package docker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
)

// PruneClient is the narrow subset of the Docker SDK used by the prune feature.
// The concrete *client.Client satisfies it; tests inject a fake.
type PruneClient interface {
	DiskUsage(ctx context.Context, options types.DiskUsageOptions) (types.DiskUsage, error)
	ImageRemove(ctx context.Context, imageID string, options image.RemoveOptions) ([]image.DeleteResponse, error)
	VolumeRemove(ctx context.Context, volumeID string, force bool) error
	ImagesPrune(ctx context.Context, pruneFilters filters.Args) (image.PruneReport, error)
}

// Candidate reasons.
const (
	ReasonDanglingImage = "dangling"
	ReasonUnusedImage   = "unused"
	ReasonUnusedVolume  = "unused"
)

// ConfirmLiteral is the explicit confirmation string required in a confirm body.
const ConfirmLiteral = "PRUNE"

// Errors returned by the prune layer.
var (
	ErrConfirmationRequired = errors.New("confirmation required: body must include confirm=\"PRUNE\"")
	ErrFingerprintRequired  = errors.New("fingerprint required: run a dry-run first")
	ErrFingerprintMismatch  = errors.New("fingerprint mismatch: candidate set changed; re-run dry-run")
	ErrNoPruner             = errors.New("prune backend not available")
	ErrConfirmInProgress    = errors.New("a prune operation is already in progress")
)

// Timeouts for Docker daemon calls. Listing/dry-run are read-only and bounded;
// the deletion phase runs on a detached context so a client disconnect cannot
// interrupt already-started removals (which would make the audit inaccurate).
const (
	listTimeout          = 30 * time.Second
	deleteTimeout        = 2 * time.Minute
	danglingPruneTimeout = 30 * time.Second
)

// ImageCandidate is a dangling image offered for cleanup.
type ImageCandidate struct {
	ID          string   `json:"id"`
	ShortID     string   `json:"short_id"`
	Tags        []string `json:"tags"`
	RepoDigests []string `json:"repo_digests,omitempty"`
	Size        int64    `json:"size"`
	SharedSize  int64    `json:"shared_size"`
	Created     int64    `json:"created"`
	Containers  int64    `json:"containers"`
	Reason      string   `json:"reason"`
}

// VolumeCandidate is an unused (dangling) volume offered for cleanup.
type VolumeCandidate struct {
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	Mountpoint string `json:"mountpoint"`
	Size       int64  `json:"size"` // -1 when the driver does not report size
	RefCount   int64  `json:"ref_count"`
	CreatedAt  string `json:"created_at,omitempty"`
	Reason     string `json:"reason"`
}

// Candidates is the result of listing / scoping a dry-run.
type Candidates struct {
	Images       []ImageCandidate  `json:"images"`
	Volumes      []VolumeCandidate `json:"volumes"`
	ImagesCount  int               `json:"images_count"`
	VolumesCount int               `json:"volumes_count"`
	ImagesSize   int64             `json:"images_size"`
	VolumesSize  int64             `json:"volumes_size"`
	TotalSize    int64             `json:"total_size"`
	GeneratedAt  time.Time         `json:"generated_at"`
	Fingerprint  string            `json:"fingerprint"`
}

// Selection scopes a dry-run/confirm to a subset of candidates.
// Empty slices mean "all current candidates".
type Selection struct {
	Images  []string `json:"images,omitempty"`
	Volumes []string `json:"volumes,omitempty"`
}

// DryRunReport is the preview returned by a dry-run (never mutates state).
type DryRunReport struct {
	DryRun      bool        `json:"dry_run"`
	Candidates  *Candidates `json:"candidates"`
	WillDelete  ScopeCount  `json:"will_delete"`
	Warnings    []string    `json:"warnings"`
	GeneratedAt time.Time   `json:"generated_at"`
}

// ScopeCount summarizes a scoped candidate set.
type ScopeCount struct {
	Images                int   `json:"images"`
	Volumes               int   `json:"volumes"`
	EstimatedReclaimBytes int64 `json:"estimated_reclaim_bytes"`
}

// ConfirmRequest is the body of a confirm (delete) request.
type ConfirmRequest struct {
	Confirm     string   `json:"confirm"`
	Fingerprint string   `json:"fingerprint"`
	Images      []string `json:"images,omitempty"`
	Volumes     []string `json:"volumes,omitempty"`
}

// DeleteItemResult is the per-item outcome of a deletion.
type DeleteItemResult struct {
	Type           string `json:"type"` // "image" | "volume"
	ID             string `json:"id,omitempty"`
	Name           string `json:"name,omitempty"`
	Status         string `json:"status"` // "deleted" | "failed" | "skipped"
	Error          string `json:"error,omitempty"`
	ReclaimedBytes int64  `json:"reclaimed_bytes"`
}

// DeleteSummary aggregates per-item results.
type DeleteSummary struct {
	Deleted        int   `json:"deleted"`
	Failed         int   `json:"failed"`
	Skipped        int   `json:"skipped"`
	ReclaimedBytes int64 `json:"reclaimed_bytes"`
}

// DeleteReport is the result of a confirmed deletion.
type DeleteReport struct {
	DryRun             bool               `json:"dry_run"`
	Confirmed          bool               `json:"confirmed"`
	FingerprintMatched bool               `json:"fingerprint_matched"`
	Items              []DeleteItemResult `json:"items"`
	Summary            DeleteSummary      `json:"summary"`
	Warnings           []string           `json:"warnings"`
	StartedAt          time.Time          `json:"started_at"`
	FinishedAt         time.Time          `json:"finished_at"`
}

// Pruner implements the prune workflow against a PruneClient.
type Pruner struct {
	cli PruneClient

	mu   sync.Mutex
	busy bool
}

// defaultRemoveOptions are the non-force options used for every deletion:
// Force=false lets the daemon refuse in-use items (the last safety backstop),
// and PruneChildren=true reclaims orphaned parent layers.
func defaultRemoveOptions() image.RemoveOptions {
	return image.RemoveOptions{Force: false, PruneChildren: true}
}

// NewPruner creates a Pruner backed by the given client.
func NewPruner(cli PruneClient) *Pruner {
	return &Pruner{cli: cli}
}

// Client returns the underlying PruneClient (used by callers that need to
// verify the concrete daemon connection).
func (p *Pruner) Client() PruneClient {
	return p.cli
}

// shortImageID returns a stable 12-hex-char display id, stripping the "sha256:"
// prefix when present.
func shortImageID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// fingerprint computes a 16-hex-char state token over a sorted candidate set.
// It guards against TOCTOU between dry-run and confirm; it is not a secret.
func fingerprint(imageIDs, volumeNames []string) string {
	imgs := append([]string(nil), imageIDs...)
	sort.Strings(imgs)
	vols := append([]string(nil), volumeNames...)
	sort.Strings(vols)
	canonical := "images:" + strings.Join(imgs, ",") + ";volumes:" + strings.Join(vols, ",")
	h := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(h[:])[:16]
}

// sumSizes totals candidate sizes, treating negative values as 0.
func sumSizes(images []ImageCandidate, volumes []VolumeCandidate) (imgSize, volSize, total int64) {
	for _, im := range images {
		if im.Size > 0 {
			imgSize += im.Size
		}
	}
	for _, v := range volumes {
		if v.Size > 0 {
			volSize += v.Size
		}
	}
	return imgSize, volSize, imgSize + volSize
}

// Candidates lists dangling images and unused volumes using a single DiskUsage
// call. Read-only; never mutates daemon state.
func (p *Pruner) Candidates(ctx context.Context) (*Candidates, error) {
	if p == nil || p.cli == nil {
		return nil, ErrNoPruner
	}
	// Bound the read so a hung daemon cannot pin a handler forever.
	listCtx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()

	// Small delay to handle Docker daemon eventual consistency after
	// mutations (image/volume removal). Without this, a follow-up
	// DiskUsage call can return stale or partially-reconciled state.
	time.Sleep(300 * time.Millisecond)

	du, err := p.cli.DiskUsage(listCtx, types.DiskUsageOptions{
		Types: []types.DiskUsageObject{types.ImageObject, types.VolumeObject},
	})
	if err != nil {
		return nil, err
	}
	return candidatesFromDiskUsage(du), nil
}

func candidatesFromDiskUsage(du types.DiskUsage) *Candidates {
	imgs := make([]ImageCandidate, 0)
	for _, im := range du.Images {
		if im == nil || im.ID == "" {
			continue
		}
		// A negative Containers value means "unknown"; exclude conservatively.
		if im.Containers != 0 {
			continue
		}
		reason := ReasonDanglingImage
		if len(im.RepoTags) > 0 || len(im.RepoDigests) > 0 {
			reason = ReasonUnusedImage
		}
		imgs = append(imgs, ImageCandidate{
			ID:          im.ID,
			ShortID:     shortImageID(im.ID),
			Tags:        append([]string(nil), im.RepoTags...),
			RepoDigests: append([]string(nil), im.RepoDigests...),
			Size:        im.Size,
			SharedSize:  im.SharedSize,
			Created:     im.Created,
			Containers:  im.Containers,
			Reason:      reason,
		})
	}
	vols := make([]VolumeCandidate, 0)
	for _, v := range du.Volumes {
		if v == nil || v.Name == "" {
			continue
		}
		// Unused = reference count explicitly zero. nil/negative = unknown; exclude.
		if v.UsageData == nil || v.UsageData.RefCount != 0 {
			continue
		}
		vols = append(vols, VolumeCandidate{
			Name:       v.Name,
			Driver:     v.Driver,
			Mountpoint: v.Mountpoint,
			Size:       v.UsageData.Size,
			RefCount:   v.UsageData.RefCount,
			CreatedAt:  v.CreatedAt,
			Reason:     ReasonUnusedVolume,
		})
	}
	// Deterministic order (by short id / name) for stable fingerprints & display.
	sort.Slice(imgs, func(i, j int) bool { return imgs[i].ID < imgs[j].ID })
	sort.Slice(vols, func(i, j int) bool { return vols[i].Name < vols[j].Name })

	imgSize, volSize, total := sumSizes(imgs, vols)
	imgIDs := make([]string, len(imgs))
	for i := range imgs {
		imgIDs[i] = imgs[i].ID
	}
	volNames := make([]string, len(vols))
	for i := range vols {
		volNames[i] = vols[i].Name
	}
	return &Candidates{
		Images:       imgs,
		Volumes:      vols,
		ImagesCount:  len(imgs),
		VolumesCount: len(vols),
		ImagesSize:   imgSize,
		VolumesSize:  volSize,
		TotalSize:    total,
		GeneratedAt:  time.Now().UTC(),
		Fingerprint:  fingerprint(imgIDs, volNames),
	}
}

// scope filters candidates to the requested selection. Empty selection means
// "all". Unknown ids are dropped (they do not widen scope or error).
func scope(c *Candidates, sel Selection) (*Candidates, int, int, int64) {
	wantImg := toSet(sel.Images)
	wantVol := toSet(sel.Volumes)
	allImg := len(sel.Images) == 0
	allVol := len(sel.Volumes) == 0

	imgs := make([]ImageCandidate, 0)
	for _, im := range c.Images {
		if allImg || wantImg[im.ID] || wantImg[im.ShortID] {
			imgs = append(imgs, im)
		}
	}
	vols := make([]VolumeCandidate, 0)
	for _, v := range c.Volumes {
		if allVol || wantVol[v.Name] {
			vols = append(vols, v)
		}
	}
	imgIDs := make([]string, len(imgs))
	for i := range imgs {
		imgIDs[i] = imgs[i].ID
	}
	volNames := make([]string, len(vols))
	for i := range vols {
		volNames[i] = vols[i].Name
	}
	imgSize, volSize, total := sumSizes(imgs, vols)
	scoped := &Candidates{
		Images:       imgs,
		Volumes:      vols,
		ImagesCount:  len(imgs),
		VolumesCount: len(vols),
		ImagesSize:   imgSize,
		VolumesSize:  volSize,
		TotalSize:    total,
		GeneratedAt:  c.GeneratedAt,
		Fingerprint:  fingerprint(imgIDs, volNames),
	}
	return scoped, len(imgs), len(vols), total
}

func toSet(items []string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, it := range items {
		m[it] = true
	}
	return m
}

// DryRun returns a preview of what would be deleted for the given selection.
// It is strictly read-only: it never calls ImageRemove/VolumeRemove.
func (p *Pruner) DryRun(ctx context.Context, sel Selection) (*DryRunReport, error) {
	if p == nil || p.cli == nil {
		return nil, ErrNoPruner
	}
	all, err := p.Candidates(ctx)
	if err != nil {
		return nil, err
	}
	scoped, ic, vc, total := scope(all, sel)
	return &DryRunReport{
		DryRun:      true,
		Candidates:  scoped,
		WillDelete:  ScopeCount{Images: ic, Volumes: vc, EstimatedReclaimBytes: total},
		Warnings:    []string{},
		GeneratedAt: time.Now().UTC(),
	}, nil
}

// Confirm validates the explicit confirmation and fingerprint, re-fetches live
// candidates (TOCTOU guard), and deletes the scoped intersection using non-force
// removal. Partial failures are collected per item and never hidden.
func (p *Pruner) Confirm(ctx context.Context, req ConfirmRequest) (*DeleteReport, error) {
	if p == nil || p.cli == nil {
		return nil, ErrNoPruner
	}
	if strings.TrimSpace(req.Confirm) != ConfirmLiteral {
		return nil, ErrConfirmationRequired
	}
	if strings.TrimSpace(req.Fingerprint) == "" {
		return nil, ErrFingerprintRequired
	}

	// Single-flight: only one deletion at a time.
	p.mu.Lock()
	if p.busy {
		p.mu.Unlock()
		return nil, ErrConfirmInProgress
	}
	p.busy = true
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.busy = false
		p.mu.Unlock()
	}()

	started := time.Now().UTC()
	report := &DeleteReport{
		DryRun:    false,
		Confirmed: true,
		Items:     []DeleteItemResult{},
		StartedAt: started,
	}

	// Re-fetch live candidates so the fingerprint reflects current state.
	// The read is bounded by Candidates' own timeout.
	live, err := p.Candidates(ctx)
	if err != nil {
		return nil, err
	}
	scoped, _, _, _ := scope(live, Selection{Images: req.Images, Volumes: req.Volumes})
	if scoped.Fingerprint != req.Fingerprint {
		report.FingerprintMatched = false
		report.Warnings = append(report.Warnings, ErrFingerprintMismatch.Error())
		report.FinishedAt = time.Now().UTC()
		return report, ErrFingerprintMismatch
	}
	report.FingerprintMatched = true

	// Run the deletion phase on a detached, bounded context so a client
	// disconnect cannot interrupt removals that already started (which would
	// leave the audit under-reporting what was actually deleted).
	delCtx, cancel := context.WithTimeout(context.Background(), deleteTimeout)
	defer cancel()

	opts := defaultRemoveOptions()

	for _, im := range scoped.Images {
		_, err := p.cli.ImageRemove(delCtx, im.ID, opts)
		item := DeleteItemResult{Type: "image", ID: im.ID}
		switch {
		case err == nil:
			item.Status = "deleted"
			if im.Size > 0 {
				item.ReclaimedBytes = im.Size
			}
		case isInUseErr(err):
			item.Status = "skipped"
			item.Error = "image is in use by a container (non-force refused)"
		case isNotFoundErr(err):
			// Another process already removed it; not a failure.
			item.Status = "skipped"
			item.Error = "image no longer exists (already removed)"
		default:
			item.Status = "failed"
			item.Error = err.Error()
		}
		report.Items = append(report.Items, item)
	}

	for _, v := range scoped.Volumes {
		err := p.cli.VolumeRemove(delCtx, v.Name, false)
		item := DeleteItemResult{Type: "volume", Name: v.Name}
		switch {
		case err == nil:
			item.Status = "deleted"
			if v.Size > 0 {
				item.ReclaimedBytes = v.Size
			}
		case isInUseErr(err):
			item.Status = "skipped"
			item.Error = "volume is in use by a container (non-force refused)"
		case isNotFoundErr(err):
			item.Status = "skipped"
			item.Error = "volume no longer exists (already removed)"
		default:
			item.Status = "failed"
			item.Error = err.Error()
		}
		report.Items = append(report.Items, item)
	}

	// After removing unused (tagged) images, Docker may leave behind dangling
	// layers that appear as new image IDs on the next DiskUsage call. Prune
	// them here so the user does not see "ghost" candidates on the next
	// refresh.
	danglingCtx, cancelDangling := context.WithTimeout(context.Background(), danglingPruneTimeout)
	defer cancelDangling()
	f := filters.NewArgs()
	f.Add("until", "1h")
	if _, err := p.cli.ImagesPrune(danglingCtx, f); err == nil {
		report.Warnings = append(report.Warnings, "dangling layers left by the deletions were also removed")
	}

	for _, it := range report.Items {
		switch it.Status {
		case "deleted":
			report.Summary.Deleted++
			report.Summary.ReclaimedBytes += it.ReclaimedBytes
		case "failed":
			report.Summary.Failed++
		case "skipped":
			report.Summary.Skipped++
		}
	}
	if report.Summary.Failed > 0 {
		report.Warnings = append(report.Warnings, "one or more items failed to delete; see items for details")
	}
	report.FinishedAt = time.Now().UTC()
	return report, nil
}

// isInUseErr reports whether err indicates the daemon refused to remove an
// in-use resource (triggering a "skipped" rather than "failed" outcome). It is
// intentionally narrow: a bare "conflict" is not enough, because Docker returns
// 409 conflicts for many reasons that should remain hard failures.
func isInUseErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "is being used"),
		strings.Contains(msg, "in use"),
		strings.Contains(msg, "is using the image"):
		return true
	}
	return false
}

// isNotFoundErr reports whether err indicates a resource had already been
// removed (concurrent deletion between dry-run and confirm). Treated as
// "skipped" rather than "failed" because the end state is correct.
func isNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// Docker returns "No such image" / "no such volume" with a 404; client
	// errors may surface as "not found" depending on transport.
	return strings.Contains(msg, "no such image") ||
		strings.Contains(msg, "no such volume") ||
		strings.Contains(msg, "not found")
}
