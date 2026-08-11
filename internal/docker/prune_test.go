package docker

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// fpFromDryRun runs a dry-run with the selection and returns its fingerprint.
func fpFromDryRun(t *testing.T, p *Pruner, sel Selection) string {
	t.Helper()
	rep, err := p.DryRun(context.Background(), sel)
	if err != nil {
		t.Fatalf("dry-run setup failed: %v", err)
	}
	return rep.Candidates.Fingerprint
}

// ---- candidates ----

func TestCandidates_EmptyDiskUsage(t *testing.T) {
	f := newFakeClient()
	p := NewPruner(f)
	c, err := p.Candidates(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.ImagesCount != 0 || c.VolumesCount != 0 || c.TotalSize != 0 {
		t.Errorf("expected empty candidates, got %+v", c)
	}
	if c.Fingerprint == "" {
		t.Error("fingerprint should be non-empty even for empty set")
	}
	if c.GeneratedAt.IsZero() {
		t.Error("GeneratedAt should be set")
	}
}

func TestCandidates_DanglingImageIncluded(t *testing.T) {
	f := newFakeClient()
	f.images = []*imageSummary{{id: "sha256:abcdef0123456789", size: 1000, containers: 0}}
	c, err := NewPruner(f).Candidates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c.ImagesCount != 1 {
		t.Fatalf("expected 1 image, got %d", c.ImagesCount)
	}
	if c.Images[0].Reason != ReasonDanglingImage {
		t.Errorf("reason = %q", c.Images[0].Reason)
	}
	if c.Images[0].ShortID != "abcdef012345" {
		t.Errorf("short id = %q", c.Images[0].ShortID)
	}
	if c.ImagesSize != 1000 {
		t.Errorf("images size = %d", c.ImagesSize)
	}
}

func TestCandidates_TaggedImageExcluded(t *testing.T) {
	f := newFakeClient()
	f.images = []*imageSummary{
		{id: "a", repoTags: []string{"img:latest"}, containers: 0},
		{id: "b", repoTags: nil, containers: 1},
		{id: "c", repoTags: nil, containers: -1},
	}
	c, _ := NewPruner(f).Candidates(context.Background())
	if c.ImagesCount != 1 {
		t.Errorf("expected 1 unused image (tagged, no containers), got %d: %+v", c.ImagesCount, c.Images)
	}
	if c.Images[0].ID != "a" {
		t.Errorf("expected image 'a', got %s", c.Images[0].ID)
	}
	if c.Images[0].Reason != ReasonUnusedImage {
		t.Errorf("reason = %q, want %q", c.Images[0].Reason, ReasonUnusedImage)
	}
}

func TestCandidates_VolumeRefCountRules(t *testing.T) {
	f := newFakeClient()
	f.volumes = []*volumeUsage{
		{name: "unused", size: 200, refCount: 0},
		{name: "inuse", size: 500, refCount: 2},
		{name: "unknown", size: 999, refCount: -1},
	}
	c, _ := NewPruner(f).Candidates(context.Background())
	if c.VolumesCount != 1 || c.Volumes[0].Name != "unused" {
		t.Fatalf("expected only 'unused' volume, got %+v", c.Volumes)
	}
	if c.Volumes[0].Reason != ReasonUnusedVolume {
		t.Errorf("reason = %q", c.Volumes[0].Reason)
	}
	if c.VolumesSize != 200 {
		t.Errorf("volumes size = %d", c.VolumesSize)
	}
}

func TestCandidates_NegativeVolumeSizeContributesZero(t *testing.T) {
	f := newFakeClient()
	f.volumes = []*volumeUsage{{name: "v", size: -1, refCount: 0}}
	c, _ := NewPruner(f).Candidates(context.Background())
	if c.VolumesCount != 1 {
		t.Fatalf("expected 1 volume, got %d", c.VolumesCount)
	}
	if c.VolumesSize != 0 || c.TotalSize != 0 {
		t.Errorf("negative size should contribute 0, got vol=%d total=%d", c.VolumesSize, c.TotalSize)
	}
}

func TestCandidates_MixedAndSorted(t *testing.T) {
	f := newFakeClient()
	f.images = []*imageSummary{
		{id: "sha256:bbb", size: 100, containers: 0},
		{id: "sha256:aaa", size: 200, containers: 0},
	}
	f.volumes = []*volumeUsage{
		{name: "vol-z", size: 50, refCount: 0},
		{name: "vol-a", size: 60, refCount: 0},
	}
	c, _ := NewPruner(f).Candidates(context.Background())
	if c.ImagesCount != 2 || c.VolumesCount != 2 {
		t.Fatalf("counts wrong: %+v", c)
	}
	if c.Images[0].ID != "sha256:aaa" || c.Volumes[0].Name != "vol-a" {
		t.Errorf("not sorted: img[0]=%s vol[0]=%s", c.Images[0].ID, c.Volumes[0].Name)
	}
	if c.TotalSize != 100+200+50+60 {
		t.Errorf("total = %d", c.TotalSize)
	}
}

func TestCandidates_DiskUsageError(t *testing.T) {
	f := newFakeClient()
	f.diskUsageErr = errors.New("daemon down")
	_, err := NewPruner(f).Candidates(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCandidates_NilPruner(t *testing.T) {
	var p *Pruner
	if _, err := p.Candidates(context.Background()); err != ErrNoPruner {
		t.Fatalf("expected ErrNoPruner, got %v", err)
	}
}

func TestCandidates_FieldsPopulated(t *testing.T) {
	f := newFakeClient()
	f.images = []*imageSummary{{id: "sha256:deadbeef", size: 10, sharedSize: 2, containers: 0, created: 123}}
	f.volumes = []*volumeUsage{{name: "n", driver: "local", mountpoint: "/m", size: 20, refCount: 0, createdAt: "2026-01-01T00:00:00Z"}}
	c, _ := NewPruner(f).Candidates(context.Background())
	im := c.Images[0]
	if im.Size != 10 || im.SharedSize != 2 || im.Created != 123 || im.Containers != 0 {
		t.Errorf("image fields not populated: %+v", im)
	}
	vo := c.Volumes[0]
	if vo.Driver != "local" || vo.Mountpoint != "/m" || vo.CreatedAt != "2026-01-01T00:00:00Z" {
		t.Errorf("volume fields not populated: %+v", vo)
	}
}

// ---- dry-run ----

func TestDryRun_PurityNoMutation(t *testing.T) {
	f := newFakeClient()
	f.images = []*imageSummary{{id: "sha256:aaaa1111", size: 100, containers: 0}}
	f.volumes = []*volumeUsage{{name: "vol1", size: 200, refCount: 0}}
	p := NewPruner(f)

	rep, err := p.DryRun(context.Background(), Selection{})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.DryRun {
		t.Error("DryRun flag should be true")
	}
	if f.imageRemoveCnt != 0 || f.volumeRemoveCnt != 0 {
		t.Fatalf("dry-run mutated state: imageRemoves=%d volumeRemoves=%d", f.imageRemoveCnt, f.volumeRemoveCnt)
	}
	if f.diskUsageCalls != 1 {
		t.Errorf("expected 1 DiskUsage call, got %d", f.diskUsageCalls)
	}
}

func TestDryRun_EmptySelectionMeansAll(t *testing.T) {
	f := newFakeClient()
	f.images = []*imageSummary{
		{id: "sha256:aa", size: 100, containers: 0},
		{id: "sha256:bb", size: 200, containers: 0},
	}
	f.volumes = []*volumeUsage{{name: "v", size: 50, refCount: 0}}
	rep, _ := NewPruner(f).DryRun(context.Background(), Selection{})
	if rep.WillDelete.Images != 2 || rep.WillDelete.Volumes != 1 {
		t.Errorf("will_delete = %+v", rep.WillDelete)
	}
	if rep.WillDelete.EstimatedReclaimBytes != 350 {
		t.Errorf("reclaim = %d", rep.WillDelete.EstimatedReclaimBytes)
	}
}

func TestDryRun_Subset(t *testing.T) {
	f := newFakeClient()
	f.images = []*imageSummary{
		{id: "sha256:aa", size: 100, containers: 0},
		{id: "sha256:bb", size: 200, containers: 0},
	}
	rep, _ := NewPruner(f).DryRun(context.Background(), Selection{Images: []string{"sha256:aa"}})
	if rep.WillDelete.Images != 1 || rep.Candidates.Images[0].ID != "sha256:aa" {
		t.Errorf("subset failed: %+v", rep.Candidates.Images)
	}
	if rep.Candidates.Fingerprint == "" {
		t.Error("scoped fingerprint should be set")
	}
}

func TestDryRun_UnknownIDDropped(t *testing.T) {
	f := newFakeClient()
	f.images = []*imageSummary{{id: "sha256:aa", size: 100, containers: 0}}
	rep, _ := NewPruner(f).DryRun(context.Background(), Selection{Images: []string{"sha256:doesnotexist"}})
	if rep.WillDelete.Images != 0 {
		t.Errorf("unknown id should be dropped, got %d images", rep.WillDelete.Images)
	}
}

func TestDryRun_ShortIDAccepted(t *testing.T) {
	f := newFakeClient()
	f.images = []*imageSummary{{id: "sha256:abcdef0123456789", size: 100, containers: 0}}
	rep, err := NewPruner(f).DryRun(context.Background(), Selection{Images: []string{"abcdef012345"}})
	if err != nil {
		t.Fatal(err)
	}
	if rep.WillDelete.Images != 1 {
		t.Errorf("short-id selection should match, got %d", rep.WillDelete.Images)
	}
}

func TestDryRun_GeneratedAtSet(t *testing.T) {
	f := newFakeClient()
	rep, _ := NewPruner(f).DryRun(context.Background(), Selection{})
	if rep.GeneratedAt.IsZero() {
		t.Error("GeneratedAt should be set")
	}
}

// ---- confirm ----

func TestConfirm_RequiresConfirmLiteral(t *testing.T) {
	f := newFakeClient()
	p := NewPruner(f)
	_, err := p.Confirm(context.Background(), ConfirmRequest{Confirm: "yes", Fingerprint: "x"})
	if err != ErrConfirmationRequired {
		t.Fatalf("expected ErrConfirmationRequired, got %v", err)
	}
	if f.imageRemoveCnt != 0 || f.volumeRemoveCnt != 0 {
		t.Fatal("no removals should have happened")
	}
}

func TestConfirm_RequiresFingerprint(t *testing.T) {
	p := NewPruner(newFakeClient())
	_, err := p.Confirm(context.Background(), ConfirmRequest{Confirm: ConfirmLiteral, Fingerprint: "  "})
	if err != ErrFingerprintRequired {
		t.Fatalf("expected ErrFingerprintRequired, got %v", err)
	}
}

func TestConfirm_FingerprintMismatchDeletesNothing(t *testing.T) {
	f := newFakeClient()
	f.images = []*imageSummary{{id: "sha256:aa", size: 10, containers: 0}}
	p := NewPruner(f)
	rep, err := p.Confirm(context.Background(), ConfirmRequest{Confirm: ConfirmLiteral, Fingerprint: "deadbeefdeadbeef"})
	if err != ErrFingerprintMismatch {
		t.Fatalf("expected mismatch, got %v", err)
	}
	if rep.FingerprintMatched {
		t.Error("FingerprintMatched should be false")
	}
	if f.imageRemoveCnt != 0 || f.volumeRemoveCnt != 0 {
		t.Fatal("nothing should be removed on mismatch")
	}
}

func TestConfirm_DeletesAllCandidates(t *testing.T) {
	f := newFakeClient()
	f.images = []*imageSummary{
		{id: "sha256:aa", size: 100, containers: 0},
		{id: "sha256:bb", size: 200, containers: 0},
	}
	f.volumes = []*volumeUsage{{name: "v1", size: 50, refCount: 0}}
	p := NewPruner(f)
	fp := fpFromDryRun(t, p, Selection{})

	rep, err := p.Confirm(context.Background(), ConfirmRequest{Confirm: ConfirmLiteral, Fingerprint: fp})
	if err != nil {
		t.Fatalf("confirm failed: %v", err)
	}
	if !rep.FingerprintMatched || !rep.Confirmed {
		t.Errorf("expected matched+confirmed, got %+v", rep)
	}
	if rep.Summary.Deleted != 3 {
		t.Errorf("expected 3 deleted, got %d", rep.Summary.Deleted)
	}
	if rep.Summary.Failed != 0 || rep.Summary.Skipped != 0 {
		t.Errorf("expected no failed/skipped, got %+v", rep.Summary)
	}
	if rep.Summary.ReclaimedBytes != 350 {
		t.Errorf("reclaimed = %d", rep.Summary.ReclaimedBytes)
	}
	if f.imageRemoveCnt != 2 || f.volumeRemoveCnt != 1 {
		t.Errorf("remove counts: img=%d vol=%d", f.imageRemoveCnt, f.volumeRemoveCnt)
	}
}

func TestConfirm_SubsetOnly(t *testing.T) {
	f := newFakeClient()
	f.images = []*imageSummary{
		{id: "sha256:aa", size: 100, containers: 0},
		{id: "sha256:bb", size: 200, containers: 0},
	}
	p := NewPruner(f)
	fp := fpFromDryRun(t, p, Selection{Images: []string{"sha256:aa"}})

	rep, err := p.Confirm(context.Background(), ConfirmRequest{Confirm: ConfirmLiteral, Fingerprint: fp, Images: []string{"sha256:aa"}})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Summary.Deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", rep.Summary.Deleted)
	}
	if f.removedImages[0] != "sha256:aa" {
		t.Errorf("wrong image removed: %v", f.removedImages)
	}
}

func TestConfirm_UnknownIDDropped(t *testing.T) {
	f := newFakeClient()
	f.images = []*imageSummary{{id: "sha256:aa", size: 100, containers: 0}}
	p := NewPruner(f)
	fp := fpFromDryRun(t, p, Selection{})
	// Request an unknown id alongside the real one; scope drops it.
	rep, err := p.Confirm(context.Background(), ConfirmRequest{
		Confirm: ConfirmLiteral, Fingerprint: fp,
		Images: []string{"sha256:aa", "sha256:ghost"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Summary.Deleted != 1 {
		t.Errorf("unknown id should be dropped, deleted=%d", rep.Summary.Deleted)
	}
}

func TestConfirm_ImageFailurePartial(t *testing.T) {
	f := newFakeClient()
	f.images = []*imageSummary{
		{id: "sha256:good", size: 100, containers: 0},
		{id: "sha256:bad", size: 200, containers: 0},
	}
	f.imageRemoveErr["sha256:bad"] = errors.New("daemon explode")
	p := NewPruner(f)
	fp := fpFromDryRun(t, p, Selection{})

	rep, err := p.Confirm(context.Background(), ConfirmRequest{Confirm: ConfirmLiteral, Fingerprint: fp})
	if err != nil {
		t.Fatalf("confirm should not return hard error on partial failure: %v", err)
	}
	if rep.Summary.Deleted != 1 || rep.Summary.Failed != 1 {
		t.Errorf("expected 1 deleted 1 failed, got %+v", rep.Summary)
	}
	if len(rep.Warnings) == 0 {
		t.Error("expected a warning on partial failure")
	}
	// Other item still deleted.
	if f.imageRemoveCnt != 2 {
		t.Errorf("both removes should have been attempted, got %d", f.imageRemoveCnt)
	}
}

func TestConfirm_VolumeFailurePartial(t *testing.T) {
	f := newFakeClient()
	f.volumes = []*volumeUsage{
		{name: "good", size: 100, refCount: 0},
		{name: "bad", size: 200, refCount: 0},
	}
	f.volumeRemoveErr["bad"] = errors.New("nfs stale")
	p := NewPruner(f)
	fp := fpFromDryRun(t, p, Selection{})

	rep, err := p.Confirm(context.Background(), ConfirmRequest{Confirm: ConfirmLiteral, Fingerprint: fp})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Summary.Deleted != 1 || rep.Summary.Failed != 1 {
		t.Errorf("expected 1/1, got %+v", rep.Summary)
	}
}

func TestConfirm_NonForceOptionsUsed(t *testing.T) {
	f := newFakeClient()
	f.images = []*imageSummary{{id: "sha256:aa", size: 1, containers: 0}}
	p := NewPruner(f)
	fp := fpFromDryRun(t, p, Selection{})
	if _, err := p.Confirm(context.Background(), ConfirmRequest{Confirm: ConfirmLiteral, Fingerprint: fp}); err != nil {
		t.Fatal(err)
	}
	opts, ok := f.imageRemoveOpts["sha256:aa"]
	if !ok {
		t.Fatal("ImageRemove not called")
	}
	if opts.Force != false {
		t.Error("ImageRemove must use Force=false (non-force)")
	}
	if opts.PruneChildren != true {
		t.Error("ImageRemove should use PruneChildren=true")
	}
}

func TestConfirm_InUseIsSkippedNotFailed(t *testing.T) {
	f := newFakeClient()
	f.images = []*imageSummary{{id: "sha256:aa", size: 100, containers: 0}}
	f.volumes = []*volumeUsage{{name: "v", size: 50, refCount: 0}}
	f.imageRemoveErr["sha256:aa"] = errors.New("conflict: unable to remove image is being used by container")
	f.volumeRemoveErr["v"] = errors.New("volume is in use by a container")
	p := NewPruner(f)
	fp := fpFromDryRun(t, p, Selection{})

	rep, err := p.Confirm(context.Background(), ConfirmRequest{Confirm: ConfirmLiteral, Fingerprint: fp})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Summary.Skipped != 2 {
		t.Errorf("expected 2 skipped, got %+v", rep.Summary)
	}
	for _, it := range rep.Items {
		if it.Status != "skipped" {
			t.Errorf("expected skipped, got %+v", it)
		}
	}
}

func TestConfirm_ReclaimedNegativeSizeZero(t *testing.T) {
	f := newFakeClient()
	f.volumes = []*volumeUsage{{name: "v", size: -1, refCount: 0}}
	p := NewPruner(f)
	fp := fpFromDryRun(t, p, Selection{})
	rep, err := p.Confirm(context.Background(), ConfirmRequest{Confirm: ConfirmLiteral, Fingerprint: fp})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Summary.ReclaimedBytes != 0 {
		t.Errorf("negative size should reclaim 0, got %d", rep.Summary.ReclaimedBytes)
	}
}

func TestConfirm_DiskUsageErrorAtConfirm(t *testing.T) {
	f := newFakeClient()
	p := NewPruner(f)
	// First Candidates succeeds (empty), then set error before confirm re-fetch.
	fp := fpFromDryRun(t, p, Selection{})
	f.diskUsageErr = errors.New("daemon vanished")
	_, err := p.Confirm(context.Background(), ConfirmRequest{Confirm: ConfirmLiteral, Fingerprint: fp})
	if err == nil {
		t.Fatal("expected error when DiskUsage fails at confirm")
	}
}

func TestConfirm_TimestampsSet(t *testing.T) {
	f := newFakeClient()
	p := NewPruner(f)
	fp := fpFromDryRun(t, p, Selection{})
	rep, err := p.Confirm(context.Background(), ConfirmRequest{Confirm: ConfirmLiteral, Fingerprint: fp})
	if err != nil {
		t.Fatal(err)
	}
	if rep.StartedAt.IsZero() || rep.FinishedAt.IsZero() {
		t.Error("timestamps must be set")
	}
	if rep.FinishedAt.Before(rep.StartedAt) {
		t.Error("finished before started")
	}
}

func TestConfirm_SingleFlight(t *testing.T) {
	f := newFakeClient()
	f.images = []*imageSummary{{id: "sha256:aa", size: 1, containers: 0}}
	p := NewPruner(f)
	fp := fpFromDryRun(t, p, Selection{})

	release := make(chan struct{})
	entered := make(chan struct{})
	f.removeBlock = release
	f.removeEntered = entered

	var wg sync.WaitGroup
	var firstErr, secondErr error

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, firstErr = p.Confirm(context.Background(), ConfirmRequest{Confirm: ConfirmLiteral, Fingerprint: fp})
	}()
	<-entered // wait until the first confirm is inside the (busy) deletion
	wg.Add(1)
	secondDone := make(chan struct{})
	go func() {
		defer wg.Done()
		_, secondErr = p.Confirm(context.Background(), ConfirmRequest{Confirm: ConfirmLiteral, Fingerprint: fp})
		close(secondDone)
	}()
	<-secondDone // second returns immediately with ErrConfirmInProgress
	close(release)
	wg.Wait()
	if firstErr != nil {
		t.Errorf("first confirm: %v", firstErr)
	}
	if secondErr != ErrConfirmInProgress {
		t.Errorf("second confirm should be rejected as in-progress, got %v", secondErr)
	}
}

func TestConfirm_TOCTOUCandidateSetChanged(t *testing.T) {
	f := newFakeClient()
	f.images = []*imageSummary{{id: "sha256:aa", size: 100, containers: 0}}
	p := NewPruner(f)
	fp := fpFromDryRun(t, p, Selection{})

	// Simulate the image becoming in-use before confirm re-fetches.
	f.images[0].containers = 2

	_, err := p.Confirm(context.Background(), ConfirmRequest{Confirm: ConfirmLiteral, Fingerprint: fp})
	if err != ErrFingerprintMismatch {
		t.Fatalf("expected fingerprint mismatch on TOCTOU, got %v", err)
	}
	if f.imageRemoveCnt != 0 {
		t.Fatal("in-use image must not be removed")
	}
}

func TestConfirm_NilPruner(t *testing.T) {
	var p *Pruner
	if _, err := p.Confirm(context.Background(), ConfirmRequest{Confirm: ConfirmLiteral, Fingerprint: "x"}); err != ErrNoPruner {
		t.Fatalf("expected ErrNoPruner, got %v", err)
	}
}

func TestConfirm_TrimsConfirmLiteral(t *testing.T) {
	f := newFakeClient()
	p := NewPruner(f)
	// " PRUNE " with surrounding whitespace must still be accepted.
	if _, err := p.Confirm(context.Background(), ConfirmRequest{Confirm: " PRUNE ", Fingerprint: "x"}); err == ErrConfirmationRequired {
		t.Fatal("confirm literal with surrounding whitespace should be accepted after trim")
	}
}

// ---- extra ----

func TestConfirm_AlreadyRemovedIsSkipped(t *testing.T) {
	f := newFakeClient()
	f.images = []*imageSummary{{id: "sha256:gone", size: 100, containers: 0}}
	f.volumes = []*volumeUsage{{name: "gonevol", size: 50, refCount: 0}}
	f.imageRemoveErr["sha256:gone"] = errors.New("no such image: sha256:gone")
	f.volumeRemoveErr["gonevol"] = errors.New("no such volume: gonevol")
	p := NewPruner(f)
	fp := fpFromDryRun(t, p, Selection{})
	rep, err := p.Confirm(context.Background(), ConfirmRequest{Confirm: ConfirmLiteral, Fingerprint: fp})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Summary.Deleted != 0 || rep.Summary.Failed != 0 || rep.Summary.Skipped != 2 {
		t.Fatalf("expected 0 deleted 0 failed 2 skipped, got %+v", rep.Summary)
	}
	for _, it := range rep.Items {
		if it.Status != "skipped" {
			t.Errorf("expected skipped, got %+v", it)
		}
	}
}

func TestConfirm_InUseStillSkipped(t *testing.T) {
	f := newFakeClient()
	f.images = []*imageSummary{{id: "sha256:used", size: 100, containers: 0}}
	f.imageRemoveErr["sha256:used"] = errors.New("conflict: unable to delete image is being used by container")
	p := NewPruner(f)
	fp := fpFromDryRun(t, p, Selection{})
	rep, err := p.Confirm(context.Background(), ConfirmRequest{Confirm: ConfirmLiteral, Fingerprint: fp})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Summary.Skipped != 1 || rep.Summary.Failed != 0 {
		t.Fatalf("expected 1 skipped 0 failed, got %+v", rep.Summary)
	}
}

func TestIsNotFoundErr(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("no such image"), true},
		{errors.New("No such volume: x"), true},
		{errors.New("image not found"), true},
		{errors.New("some other error"), false},
		{errors.New("conflict: is being used"), false},
	}
	for _, c := range cases {
		if got := isNotFoundErr(c.err); got != c.want {
			t.Errorf("isNotFoundErr(%q)=%v want %v", c.err, got, c.want)
		}
	}
}

// ---- short id / fingerprint / sumSizes unit tests ----

func TestShortImageID(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"sha256:abcdef0123456789abcdef0123456789", "abcdef012345"},
		{"abcdef0123456789", "abcdef012345"},
		{"short", "short"},
		{"", ""},
	}
	for _, c := range cases {
		if got := shortImageID(c.in); got != c.want {
			t.Errorf("shortImageID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFingerprint(t *testing.T) {
	a := fingerprint([]string{"img1", "img2"}, []string{"vol1", "vol2"})
	if len(a) != 16 {
		t.Fatalf("fingerprint length = %d, want 16", len(a))
	}
	// Stable
	if b := fingerprint([]string{"img1", "img2"}, []string{"vol1", "vol2"}); a != b {
		t.Errorf("fingerprint not stable: %q != %q", a, b)
	}
	// Order independent
	if c := fingerprint([]string{"img2", "img1"}, []string{"vol2", "vol1"}); a != c {
		t.Errorf("fingerprint not order-independent: %q != %q", a, c)
	}
	// Different sets differ
	if d := fingerprint([]string{"img1"}, []string{"vol1", "vol2"}); a == d {
		t.Errorf("fingerprint should differ for different image sets")
	}
	if e := fingerprint([]string{"img1", "img2"}, []string{"vol1"}); a == e {
		t.Errorf("fingerprint should differ for different volume sets")
	}
	// Empty set is stable and non-empty
	emptyA := fingerprint(nil, nil)
	emptyB := fingerprint([]string{}, []string{})
	if emptyA != emptyB || emptyA == "" {
		t.Errorf("empty fingerprint should be stable and non-empty: %q %q", emptyA, emptyB)
	}
}

func TestSumSizes(t *testing.T) {
	imgs := []ImageCandidate{{Size: 100}, {Size: -1}, {Size: 50}}
	vols := []VolumeCandidate{{Size: 200}, {Size: -1}, {Size: 25}}
	imgSize, volSize, total := sumSizes(imgs, vols)
	if imgSize != 150 || volSize != 225 || total != 375 {
		t.Errorf("sumSizes = (%d,%d,%d), want (150,225,375)", imgSize, volSize, total)
	}
}
