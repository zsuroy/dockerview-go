// Package files implements confined container file transfer: a Copier
// abstraction with a docker-API production backend and a tempdir mock used
// by all tests and `make files-verify` (no real docker required).
package files

import (
	"context"
	"errors"
	"io"
	"os"
)

// ErrNotFound is returned when the container path does not exist.
var ErrNotFound = errors.New("files: path not found")

// ErrIsDirectory is returned when a file target turns out to be a directory.
var ErrIsDirectory = errors.New("files: path is a directory")

// ErrNotDirectory is returned by ReadDir/archive on a non-directory.
var ErrNotDirectory = errors.New("files: path is not a directory")

// FileInfo describes a container path.
type FileInfo struct {
	Name       string
	AbsPath    string
	Size       int64
	Mode       os.FileMode
	IsDir      bool
	IsSymlink  bool
	LinkTarget string
}

// DirEntry is one readdir row.
type DirEntry struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	IsDir     bool   `json:"is_dir"`
	IsSymlink bool   `json:"is_symlink"`
}

// WalkFunc receives paths relative to the walk root. linkTarget is set for
// symlinks; implementations must not follow out-of-jail links.
type WalkFunc func(relPath string, info FileInfo) error

// Copier is the storage backend for /api/files/*. The docker backend speaks
// tar streams over the docker API; the mock backend maps container IDs to
// temp directories. Jail checks happen in the caller BEFORE every method;
// implementations must additionally refuse anything suspicious they see
// in tar headers (defense in depth).
type Copier interface {
	// Stat lstat's the path (symlinks reported, not followed).
	Stat(ctx context.Context, container, absPath string) (FileInfo, error)
	// Put writes a regular file at absPath from r (exactly size bytes
	// expected). Caller guarantees the path passed jail checks.
	Put(ctx context.Context, container, absPath string, r io.Reader, size int64, mode os.FileMode) (int64, error)
	// Get streams a regular file's content to w.
	Get(ctx context.Context, container, absPath string, w io.Writer) (int64, error)
	// ReadDir lists immediate children (no recursion).
	ReadDir(ctx context.Context, container, absPath string) ([]DirEntry, error)
	// Walk walks absPath depth-first, relPath anchored at absPath.
	Walk(ctx context.Context, container, absPath string, fn WalkFunc) error
	// RootFS returns the host directory standing in for a container's
	// filesystem ("" for backends where no host mapping exists).
	RootFS(container string) string
	// Backend reports "docker" or "mock" for logs/audit.
	Backend() string
}
