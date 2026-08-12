package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// Fixture is the JSON document behind FixtureProvider: container metadata,
// image inventory and mock-saver behaviour. See testdata/backup_fixture.json.
type Fixture struct {
	Containers []ContainerSnapshot `json:"containers"`
	Images     []ImageInfo         `json:"images"`
	Saver      MockSaverConfig     `json:"saver"`
}

// MockSaverConfig steers MockImageSaver for offline drills.
type MockSaverConfig struct {
	// BytesPerImage is the size of the deterministic tar each save writes.
	BytesPerImage int64 `json:"bytes_per_image"`
	// FailRefs lists image refs whose export must fail (drill D17).
	FailRefs []string `json:"fail_refs"`
	// OversizeRef simulates exceeding MaxImageBytes without writing gigabytes.
	OversizeRef string `json:"oversize_ref"`
}

// FixtureProvider serves container/image metadata from a JSON file, so the
// whole backup path runs without a Docker daemon (offline acceptance).
type FixtureProvider struct {
	fx Fixture
}

// NewFixtureProvider loads and validates a fixture file.
func NewFixtureProvider(path string) (*FixtureProvider, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("backup fixture: %w", err)
	}
	var fx Fixture
	if err := json.Unmarshal(data, &fx); err != nil {
		return nil, fmt.Errorf("backup fixture %s: invalid JSON: %w", path, err)
	}
	if fx.Saver.BytesPerImage <= 0 {
		fx.Saver.BytesPerImage = 4096 // a few KB per image, enough to be a real file
	}
	return &FixtureProvider{fx: fx}, nil
}

// Snapshot implements Provider.
func (p *FixtureProvider) Snapshot(_ context.Context) ([]ContainerSnapshot, error) {
	out := make([]ContainerSnapshot, len(p.fx.Containers))
	copy(out, p.fx.Containers)
	return out, nil
}

// Images implements Provider.
func (p *FixtureProvider) Images(_ context.Context) ([]ImageInfo, error) {
	out := make([]ImageInfo, len(p.fx.Images))
	copy(out, p.fx.Images)
	return out, nil
}

// Saver implements Provider.
func (p *FixtureProvider) Saver() ImageSaver {
	return &MockImageSaver{Cfg: p.fx.Saver}
}

// EmptyProvider backs -no-docker mode without a fixture: zero containers,
// zero images. It still produces a valid (empty) snapshot package.
type EmptyProvider struct{}

// Snapshot implements Provider.
func (EmptyProvider) Snapshot(_ context.Context) ([]ContainerSnapshot, error) {
	return []ContainerSnapshot{}, nil
}

// Images implements Provider.
func (EmptyProvider) Images(_ context.Context) ([]ImageInfo, error) {
	return []ImageInfo{}, nil
}

// Saver implements Provider.
func (EmptyProvider) Saver() ImageSaver {
	return &MockImageSaver{Cfg: MockSaverConfig{BytesPerImage: 4096}}
}

// MockMaxImageBytes caps the fixture-driven mock export so a hostile or
// typo'd fixture ("bytes_per_image": 10000000000) cannot OOM the process.
const MockMaxImageBytes = 1 << 30 // 1 GiB

// mockPayloadChunk is the streaming padding unit (keeps memory flat).
const mockPayloadChunk = 64 << 10

// MockImageSaver is the replaceable ImageSaver used by tests and offline
// verification. It streams a small deterministic tar (a few KB by default)
// per image and can be told to fail or simulate an oversize export for
// specific refs.
type MockImageSaver struct {
	Cfg MockSaverConfig
}

// SaveImage implements ImageSaver.
func (s *MockImageSaver) SaveImage(_ context.Context, ref string, w io.Writer) (int64, error) {
	for _, f := range s.Cfg.FailRefs {
		if f == ref {
			return 0, fmt.Errorf("mock image export failed for %s", ref)
		}
	}
	if s.Cfg.OversizeRef != "" && s.Cfg.OversizeRef == ref {
		return 0, ErrImageTooLarge
	}

	size := s.Cfg.BytesPerImage
	if size <= 0 {
		size = 4096
	}
	if size > MockMaxImageBytes {
		return 0, fmt.Errorf("mock fixture bytes_per_image=%d exceeds cap %d", size, MockMaxImageBytes)
	}

	marker := []byte("MOCK-IMAGE-EXPORT ref=" + ref + "\n")
	total := size
	if int64(len(marker)) > total {
		total = int64(len(marker))
	}

	// Stream the tar: header + marker + repeated padding, chunked so large
	// fixture sizes never materialize fully in memory.
	tw := tar.NewWriter(w)
	hdr := &tar.Header{
		Name:    SanitizeImageRef(ref) + ".txt",
		Mode:    0o600,
		Size:    total,
		ModTime: time.Unix(0, 0),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return 0, err
	}
	written := int64(0)
	n, err := tw.Write(marker)
	written += int64(n)
	if err != nil {
		return written, err
	}
	chunk := bytes.Repeat([]byte("A"), mockPayloadChunk)
	for written < total {
		p := chunk
		if rem := total - written; rem < int64(len(chunk)) {
			p = chunk[:rem]
		}
		n, err := tw.Write(p)
		written += int64(n)
		if err != nil {
			return written, err
		}
	}
	if err := tw.Close(); err != nil {
		return written, err
	}
	return written, nil
}
