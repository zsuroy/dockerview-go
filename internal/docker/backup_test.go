package docker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"

	"github.com/zsuroy/dockerview-go/internal/backup"
)

// fakeBackupClient satisfies BackupClient + ImageSaveClient for offline tests.
type fakeBackupClient struct {
	containers []container.Summary
	inspects   map[string]container.InspectResponse
	inspectErr map[string]bool
	images     []image.Summary
	listErr    error
	imgListErr error
	saveBodies map[string][]byte
	saveErr    map[string]bool
}

func (f *fakeBackupClient) ContainerList(context.Context, container.ListOptions) ([]container.Summary, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.containers, nil
}

func (f *fakeBackupClient) ContainerInspect(_ context.Context, id string) (container.InspectResponse, error) {
	if f.inspectErr[id] {
		return container.InspectResponse{}, errors.New("inspect failed")
	}
	return f.inspects[id], nil
}

func (f *fakeBackupClient) ImageList(context.Context, image.ListOptions) ([]image.Summary, error) {
	if f.imgListErr != nil {
		return nil, f.imgListErr
	}
	return f.images, nil
}

func (f *fakeBackupClient) ImageSave(_ context.Context, ids []string, _ ...client.ImageSaveOption) (io.ReadCloser, error) {
	if len(ids) == 0 {
		return nil, errors.New("no image id")
	}
	ref := ids[0]
	if f.saveErr[ref] {
		return nil, errors.New("save exploded")
	}
	body, ok := f.saveBodies[ref]
	if !ok {
		return nil, errors.New("no such image")
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

func dockerFixture() *fakeBackupClient {
	return &fakeBackupClient{
		containers: []container.Summary{
			{
				ID: "abc123def456789", Names: []string{"/web-1"}, Image: "nginx:1.27",
				State: container.StateRunning, Status: "Up 2 hours",
				Labels: map[string]string{"app": "web"},
				Ports:  []container.Port{{IP: "0.0.0.0", PrivatePort: 80, PublicPort: 8080, Type: "tcp"}},
			},
		},
		inspects: map[string]container.InspectResponse{
			"abc123def456789": {
				ContainerJSONBase: &container.ContainerJSONBase{
					ID: "abc123def456789", Name: "/web-1",
					HostConfig: &container.HostConfig{
						RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyMode("unless-stopped"), MaximumRetryCount: 0},
					},
				},
				Config: &container.Config{
					Image:  "nginx:1.27",
					Env:    []string{"PATH=/usr/bin", "DB_PASSWORD=hunter2"},
					Labels: map[string]string{"app": "web"},
				},
				Mounts: []container.MountPoint{
					{Type: mount.TypeBind, Source: "/data/web", Destination: "/usr/share/nginx/html", Mode: "ro"},
				},
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{"bridge": {}, "internal": {}},
				},
			},
		},
		images: []image.Summary{
			{ID: "sha256:aaa", RepoTags: []string{"nginx:1.27", "nginx:latest"}, Size: 187},
			{ID: "sha256:bbb", RepoTags: nil, Size: 99},
		},
	}
}

func TestDockerProvider_Snapshot(t *testing.T) {
	p := NewBackupProviderFromInterfaces(dockerFixture(), nil)
	snaps, err := p.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 1 {
		t.Fatalf("want 1 snapshot, got %d", len(snaps))
	}
	s := snaps[0]
	if s.ID != "abc123def456" || s.FullID != "abc123def456789" || s.Name != "web-1" {
		t.Fatalf("identity wrong: %+v", s)
	}
	if s.Image != "nginx:1.27" || s.Status != "Up 2 hours" {
		t.Fatalf("image/status wrong: %+v", s)
	}
	if len(s.Ports) != 1 || s.Ports[0].PublicPort != 8080 {
		t.Fatalf("ports wrong: %+v", s.Ports)
	}
	if len(s.Mounts) != 1 || s.Mounts[0].Source != "/data/web" || s.Mounts[0].Destination != "/usr/share/nginx/html" {
		t.Fatalf("mounts wrong: %+v", s.Mounts)
	}
	if s.RestartPolicy.Name != "unless-stopped" {
		t.Fatalf("restart policy wrong: %+v", s.RestartPolicy)
	}
	if strings.Join(s.Networks, ",") != "bridge,internal" {
		t.Fatalf("networks must be sorted: %v", s.Networks)
	}
	// Env is captured raw here; redaction is a pack-time concern.
	if len(s.Env) != 2 {
		t.Fatalf("env missing: %v", s.Env)
	}
}

func TestDockerProvider_SnapshotInspectFailureDegrades(t *testing.T) {
	fx := dockerFixture()
	fx.inspectErr = map[string]bool{"abc123def456789": true}
	p := NewBackupProviderFromInterfaces(fx, nil)
	snaps, err := p.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 1 {
		t.Fatalf("snapshot must still be returned, got %d", len(snaps))
	}
	if len(snaps[0].Mounts) != 0 {
		t.Fatalf("no mounts expected when inspect fails: %+v", snaps[0])
	}
}

func TestDockerProvider_SnapshotListError(t *testing.T) {
	fx := dockerFixture()
	fx.listErr = errors.New("daemon down")
	p := NewBackupProviderFromInterfaces(fx, nil)
	if _, err := p.Snapshot(context.Background()); err == nil {
		t.Fatal("want list error to propagate")
	}
}

func TestDockerProvider_Images(t *testing.T) {
	p := NewBackupProviderFromInterfaces(dockerFixture(), nil)
	imgs, err := p.Images(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// nginx has 2 tags => 2 entries; untagged image => 1 entry keyed by ID.
	if len(imgs) != 3 {
		t.Fatalf("want 3 image entries, got %v", imgs)
	}
	refs := map[string]int64{}
	for _, im := range imgs {
		refs[im.Ref] = im.SizeBytes
	}
	if refs["nginx:1.27"] != 187 || refs["nginx:latest"] != 187 {
		t.Fatalf("tag join wrong: %v", refs)
	}
	if refs["sha256:bbb"] != 99 {
		t.Fatalf("untagged image must use ID as ref: %v", refs)
	}
}

func TestDockerProvider_ImagesError(t *testing.T) {
	fx := dockerFixture()
	fx.imgListErr = errors.New("nope")
	p := NewBackupProviderFromInterfaces(fx, nil)
	if _, err := p.Images(context.Background()); err == nil {
		t.Fatal("want image list error")
	}
}

func TestDockerImageSaver_CopiesAndCounts(t *testing.T) {
	body := []byte("fake image tar bytes 0123456789")
	fx := &fakeBackupClient{saveBodies: map[string][]byte{"nginx:1.27": body}}
	saver := NewDockerImageSaver(fx)
	var buf bytes.Buffer
	n, err := saver.SaveImage(context.Background(), "nginx:1.27", &buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(body)) || !bytes.Equal(buf.Bytes(), body) {
		t.Fatalf("n=%d bytes=%q", n, buf.String())
	}
}

func TestDockerImageSaver_PropagatesSaveError(t *testing.T) {
	fx := &fakeBackupClient{saveErr: map[string]bool{"bad:1": true}}
	saver := NewDockerImageSaver(fx)
	if _, err := saver.SaveImage(context.Background(), "bad:1", io.Discard); err == nil {
		t.Fatal("want save error")
	}
}

func TestDockerImageSaver_EnforcesMaxBytes(t *testing.T) {
	// saver with a tiny cap must trip ErrImageTooLarge without writing more.
	body := bytes.Repeat([]byte("x"), 100)
	fx := &fakeBackupClient{saveBodies: map[string][]byte{"big:1": body}}
	saver := &DockerImageSaver{cli: fx, maxBytes: 10}
	var buf bytes.Buffer
	_, err := saver.SaveImage(context.Background(), "big:1", &buf)
	if !errors.Is(err, backup.ErrImageTooLarge) {
		t.Fatalf("want ErrImageTooLarge, got %v", err)
	}
	if buf.Len() > 11 {
		t.Fatalf("must not stream far past the cap, wrote %d", buf.Len())
	}
}

func TestNewBackupProvider_SatisfiesInterface(t *testing.T) {
	// Compile-time guarantee that the production wiring implements Provider.
	var _ backup.Provider = (*DockerProvider)(nil)
}
