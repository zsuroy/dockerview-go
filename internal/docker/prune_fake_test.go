package docker

import (
	"context"
	"sync"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
)

// fakePruneClient is a scripted PruneClient for tests.
type fakePruneClient struct {
	mu sync.Mutex

	diskUsageErr error
	images       []*imageSummary
	volumes      []*volumeUsage

	imageRemoveErr  map[string]error
	volumeRemoveErr map[string]error

	// Call counters / captures.
	diskUsageCalls  int
	imageRemoveCnt  int
	volumeRemoveCnt int
	imageRemoveOpts map[string]image.RemoveOptions
	removedImages   []string
	removedVolumes  []string

	// Test hooks for concurrency.
	removeBlock   chan struct{} // if non-nil, ImageRemove blocks receiving from it
	removeEntered chan struct{} // closed once when a blocking ImageRemove is entered
}

type imageSummary struct {
	id         string
	repoTags   []string
	size       int64
	sharedSize int64
	containers int64
	created    int64
}

type volumeUsage struct {
	name       string
	driver     string
	mountpoint string
	size       int64
	refCount   int64
	createdAt  string
}

func newFakeClient() *fakePruneClient {
	return &fakePruneClient{
		imageRemoveErr:  map[string]error{},
		volumeRemoveErr: map[string]error{},
		imageRemoveOpts: map[string]image.RemoveOptions{},
	}
}

func (f *fakePruneClient) DiskUsage(_ context.Context, _ types.DiskUsageOptions) (types.DiskUsage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.diskUsageCalls++
	if f.diskUsageErr != nil {
		return types.DiskUsage{}, f.diskUsageErr
	}
	du := types.DiskUsage{}
	for _, im := range f.images {
		s := &image.Summary{
			ID:         im.id,
			Size:       im.size,
			SharedSize: im.sharedSize,
			Containers: im.containers,
			Created:    im.created,
			RepoTags:   append([]string(nil), im.repoTags...),
		}
		du.Images = append(du.Images, s)
	}
	for _, v := range f.volumes {
		du.Volumes = append(du.Volumes, volSummary(v.name, v.driver, v.mountpoint, v.size, v.refCount, v.createdAt))
	}
	return du, nil
}

func (f *fakePruneClient) ImageRemove(_ context.Context, imageID string, opts image.RemoveOptions) ([]image.DeleteResponse, error) {
	if f.removeBlock != nil {
		// Signal entry exactly once, then block until released.
		if f.removeEntered != nil {
			select {
			case <-f.removeEntered:
			default:
				close(f.removeEntered)
			}
		}
		<-f.removeBlock
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.imageRemoveCnt++
	f.imageRemoveOpts[imageID] = opts
	f.removedImages = append(f.removedImages, imageID)
	if err := f.imageRemoveErr[imageID]; err != nil {
		return nil, err
	}
	return []image.DeleteResponse{{Deleted: imageID}}, nil
}

func (f *fakePruneClient) VolumeRemove(_ context.Context, volumeID string, force bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.volumeRemoveCnt++
	f.removedVolumes = append(f.removedVolumes, volumeID)
	if err := f.volumeRemoveErr[volumeID]; err != nil {
		return err
	}
	_ = force
	return nil
}

func (f *fakePruneClient) ImagesPrune(_ context.Context, _ filters.Args) (image.PruneReport, error) {
	return image.PruneReport{}, nil
}
