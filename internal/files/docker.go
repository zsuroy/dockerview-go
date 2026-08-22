package files

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// DockerCopier transfers files through the docker API using tar streams
// (the same wire format as `docker cp`). No SSH, no host-path access beyond
// the configured staging dir owned by the caller.
type DockerCopier struct {
	cli      *client.Client
	jailRoot string
}

// NewDockerCopier wraps a connected docker client. jailRoot is the
// in-container whitelist root: tar entries are laid out relative to it so
// docker creates missing intermediate directories on extract.
func NewDockerCopier(cli *client.Client, jailRoot string) *DockerCopier {
	return &DockerCopier{cli: cli, jailRoot: path.Clean(jailRoot)}
}

func (d *DockerCopier) Backend() string { return "docker" }

// RootFS is empty: the production backend cannot stat container paths on the
// host, so jail realpath checks degrade to lexical + tar-header inspection.
func (d *DockerCopier) RootFS(string) string { return "" }

func (d *DockerCopier) Stat(ctx context.Context, cid, absPath string) (FileInfo, error) {
	st, err := d.cli.ContainerStatPath(ctx, cid, absPath)
	if err != nil {
		if client.IsErrNotFound(err) {
			return FileInfo{}, ErrNotFound
		}
		return FileInfo{}, err
	}
	return FileInfo{
		Name:       path.Base(absPath),
		AbsPath:    path.Clean(absPath),
		Size:       st.Size,
		Mode:       st.Mode,
		IsDir:      st.Mode.IsDir(),
		IsSymlink:  st.LinkTarget != "",
		LinkTarget: st.LinkTarget,
	}, nil
}

// tarFile builds a tar stream for one file. When jailRoot is set and the
// target is below it, the whole directory chain from the filesystem root
// down to the file's directory is emitted — the jail root itself included —
// because CopyToContainer extracts relative to "/" and recreates every
// missing ancestor (e.g. containers without /tmp/dockerview-files).
func tarFile(jailRoot, absPath string, r io.Reader, size int64, mode os.FileMode) (io.Reader, error) {
	buf := &bytes.Buffer{}
	tw := tar.NewWriter(buf)
	rel := ""
	if jailRoot != "" {
		if r, err := filepath.Rel(filepath.ToSlash(jailRoot), filepath.ToSlash(absPath)); err == nil && !strings.HasPrefix(r, "..") {
			rel = r
		}
	}
	name := path.Base(absPath)
	if rel != "" {
		// File entry carries the full chain too: extraction happens at "/"
		// and docker lays entries out verbatim.
		name = path.Join(strings.Trim(filepath.ToSlash(jailRoot), "/"), rel)
		fullDir := path.Dir(name)
		var acc string
		for _, seg := range strings.Split(fullDir, "/") {
			acc += seg + "/"
			if err := tw.WriteHeader(&tar.Header{
				Name:     acc,
				Typeflag: tar.TypeDir,
				Mode:     0o750,
			}); err != nil {
				return nil, err
			}
		}
	}
	hdr := &tar.Header{
		Name: name,
		Mode: int64(mode.Perm()),
		Size: size,
	}
	if hdr.Mode == 0 {
		hdr.Mode = 0o600
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return nil, err
	}
	if _, err := io.CopyN(tw, r, size); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf, nil
}

func (d *DockerCopier) Put(ctx context.Context, cid, absPath string, r io.Reader, size int64, mode os.FileMode) (int64, error) {
	tr, err := tarFile(d.jailRoot, absPath, r, size, mode)
	if err != nil {
		return 0, err
	}
	// Extract relative to "/": the tar already carries the full directory
	// chain (jail root included), so the daemon recreates any missing
	// ancestor — including a container that lacks /tmp/dockerview-files.
	dst := "/"
	if d.jailRoot == "" {
		dst = path.Dir(absPath)
	}
	if err := d.cli.CopyToContainer(ctx, cid, dst, tr, container.CopyToContainerOptions{}); err != nil {
		return 0, err
	}
	return size, nil
}

func (d *DockerCopier) Get(ctx context.Context, cid, absPath string, w io.Writer) (int64, error) {
	stream, _, err := d.cli.CopyFromContainer(ctx, cid, absPath)
	if err != nil {
		if client.IsErrNotFound(err) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	defer stream.Close()
	tr := tar.NewReader(stream)
	hdr, err := tr.Next()
	if err != nil {
		return 0, fmt.Errorf("files: empty tar from container: %w", err)
	}
	if hdr.Typeflag == tar.TypeSymlink || strings.Contains(hdr.Name, "..") {
		return 0, fmt.Errorf("files: refusing suspicious tar entry %q", hdr.Name)
	}
	if hdr.FileInfo().IsDir() {
		return 0, ErrIsDirectory
	}
	return io.Copy(w, tr)
}

func (d *DockerCopier) ReadDir(ctx context.Context, cid, absPath string) ([]DirEntry, error) {
	stream, _, err := d.cli.CopyFromContainer(ctx, cid, absPath)
	if err != nil {
		if client.IsErrNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	defer stream.Close()
	prefix := path.Base(path.Clean(absPath))
	var out []DirEntry
	seen := map[string]bool{}
	tr := tar.NewReader(stream)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(hdr.Name, "/")
		parts := strings.SplitN(name, "/", 2)
		if len(parts) < 2 || parts[0] != prefix || parts[1] == "" {
			continue // the root dir entry itself
		}
		child := strings.SplitN(parts[1], "/", 2)[0]
		if seen[child] {
			continue
		}
		seen[child] = true
		out = append(out, DirEntry{
			Name:      child,
			Size:      hdr.Size,
			IsDir:     hdr.Typeflag == tar.TypeDir,
			IsSymlink: hdr.Typeflag == tar.TypeSymlink,
		})
	}
	return out, nil
}

func (d *DockerCopier) Walk(ctx context.Context, cid, absPath string, fn WalkFunc) error {
	stream, _, err := d.cli.CopyFromContainer(ctx, cid, absPath)
	if err != nil {
		if client.IsErrNotFound(err) {
			return ErrNotFound
		}
		return err
	}
	defer stream.Close()
	prefix := path.Base(path.Clean(absPath))
	tr := tar.NewReader(stream)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if strings.Contains(hdr.Name, "..") {
			return fmt.Errorf("files: refusing suspicious tar entry %q", hdr.Name)
		}
		name := strings.TrimSuffix(hdr.Name, "/")
		rel := strings.TrimPrefix(name, prefix+"/")
		if rel == "" || rel == name {
			continue
		}
		info := FileInfo{
			Name:       path.Base(rel),
			AbsPath:    path.Join(absPath, filepath.ToSlash(rel)),
			Size:       hdr.Size,
			Mode:       hdr.FileInfo().Mode(),
			IsDir:      hdr.Typeflag == tar.TypeDir,
			IsSymlink:  hdr.Typeflag == tar.TypeSymlink,
			LinkTarget: hdr.Linkname,
		}
		if err := fn(rel, info); err != nil {
			return err
		}
	}
}
