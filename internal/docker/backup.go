package docker

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"

	"github.com/zsuroy/dockerview-go/internal/backup"
)

// BackupClient is the narrow subset of the Docker SDK used by the backup
// snapshot feature. *client.Client satisfies it; tests inject a fake.
type BackupClient interface {
	ContainerList(ctx context.Context, options container.ListOptions) ([]container.Summary, error)
	ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error)
	ImageList(ctx context.Context, options image.ListOptions) ([]image.Summary, error)
}

// ImageSaveClient is the one extra SDK call the production saver needs.
type ImageSaveClient interface {
	ImageSave(ctx context.Context, imageIDs []string, saveOpts ...client.ImageSaveOption) (io.ReadCloser, error)
}

// DockerProvider implements backup.Provider against a live daemon.
type DockerProvider struct {
	cli   BackupClient
	saver backup.ImageSaver
}

// NewBackupProvider wires the daemon-backed provider (production path).
func NewBackupProvider(cli *client.Client) *DockerProvider {
	return &DockerProvider{cli: cli, saver: NewDockerImageSaver(cli)}
}

// NewBackupProviderFromInterfaces is the injectable constructor used by tests.
func NewBackupProviderFromInterfaces(cli BackupClient, saver backup.ImageSaver) *DockerProvider {
	return &DockerProvider{cli: cli, saver: saver}
}

// Snapshot implements backup.Provider: list all containers, enrich each with
// inspect data (mounts, restart policy, networks, env). Inspect failures
// degrade gracefully to the list-level metadata.
func (p *DockerProvider) Snapshot(ctx context.Context) ([]backup.ContainerSnapshot, error) {
	list, err := p.cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("backup: container list: %w", err)
	}

	snaps := make([]backup.ContainerSnapshot, 0, len(list))
	for _, c := range list {
		status := string(c.State)
		if c.Status != "" {
			status = c.Status
		}
		snap := backup.ContainerSnapshot{
			FullID: c.ID,
			ID:     truncateID(c.ID, 12),
			Name:   extractContainerName(c.Names),
			Image:  c.Image,
			Status: status,
			Labels: c.Labels,
			Ports:  mapPorts(c.Ports),
		}

		inspect, err := p.cli.ContainerInspect(ctx, c.ID)
		if err == nil {
			enrichFromInspect(&snap, inspect)
		}
		snaps = append(snaps, snap)
	}
	return snaps, nil
}

// Images implements backup.Provider: one entry per tag (plus untagged IDs)
// so the manager can join container refs against sizes.
func (p *DockerProvider) Images(ctx context.Context) ([]backup.ImageInfo, error) {
	list, err := p.cli.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("backup: image list: %w", err)
	}
	out := make([]backup.ImageInfo, 0, len(list))
	for _, im := range list {
		if len(im.RepoTags) == 0 {
			out = append(out, backup.ImageInfo{Ref: im.ID, ID: im.ID, SizeBytes: im.Size})
			continue
		}
		for _, tag := range im.RepoTags {
			out = append(out, backup.ImageInfo{Ref: tag, ID: im.ID, SizeBytes: im.Size})
		}
	}
	return out, nil
}

// Saver implements backup.Provider.
func (p *DockerProvider) Saver() backup.ImageSaver { return p.saver }

func mapPorts(ports []container.Port) []backup.Port {
	out := make([]backup.Port, 0, len(ports))
	for _, p := range ports {
		out = append(out, backup.Port{
			IP:          p.IP,
			PrivatePort: p.PrivatePort,
			PublicPort:  p.PublicPort,
			Type:        p.Type,
		})
	}
	return out
}

func enrichFromInspect(snap *backup.ContainerSnapshot, inspect container.InspectResponse) {
	if inspect.Config != nil {
		snap.Env = inspect.Config.Env
		if snap.Image == "" {
			snap.Image = inspect.Config.Image
		}
		if len(snap.Labels) == 0 {
			snap.Labels = inspect.Config.Labels
		}
	}
	mounts := make([]backup.Mount, 0, len(inspect.Mounts))
	for _, mp := range inspect.Mounts {
		mounts = append(mounts, backup.Mount{
			Type:        string(mp.Type),
			Source:      mp.Source,
			Destination: mp.Destination,
			Mode:        mp.Mode,
		})
	}
	snap.Mounts = mounts
	if inspect.HostConfig != nil {
		snap.RestartPolicy = backup.RestartPolicy{
			Name:              string(inspect.HostConfig.RestartPolicy.Name),
			MaximumRetryCount: inspect.HostConfig.RestartPolicy.MaximumRetryCount,
		}
	}
	if inspect.NetworkSettings != nil && len(inspect.NetworkSettings.Networks) > 0 {
		networks := make([]string, 0, len(inspect.NetworkSettings.Networks))
		for name := range inspect.NetworkSettings.Networks {
			networks = append(networks, name)
		}
		sort.Strings(networks)
		snap.Networks = networks
	}
}

// DockerImageSaver is the production ImageSaver: it streams `docker save`
// output (SDK ImageSave) into the archive writer, enforcing MaxImageBytes.
type DockerImageSaver struct {
	cli      ImageSaveClient
	maxBytes int64 // 0 → backup.MaxImageBytes
}

// NewDockerImageSaver creates the production saver.
func NewDockerImageSaver(cli ImageSaveClient) *DockerImageSaver {
	return &DockerImageSaver{cli: cli}
}

// SaveImage implements backup.ImageSaver.
func (s *DockerImageSaver) SaveImage(ctx context.Context, ref string, w io.Writer) (int64, error) {
	rc, err := s.cli.ImageSave(ctx, []string{ref})
	if err != nil {
		return 0, fmt.Errorf("docker save %s: %w", ref, err)
	}
	defer rc.Close()

	maxBytes := s.maxBytes
	if maxBytes <= 0 {
		maxBytes = backup.MaxImageBytes
	}
	// Read one byte beyond the cap so exact-size streams still complete while
	// oversized streams are detected.
	n, err := io.Copy(w, io.LimitReader(rc, maxBytes+1))
	if err != nil {
		return n, fmt.Errorf("docker save %s: %w", ref, err)
	}
	if n > maxBytes {
		return n, fmt.Errorf("%w: %s (%d bytes)", backup.ErrImageTooLarge, ref, n)
	}
	return n, nil
}
