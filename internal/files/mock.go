package files

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"syscall"
)

// MockCopier maps container IDs onto host temp directories. It is the
// backend used by -no-docker, unit tests and make files-verify — nothing in
// the verification path requires a real docker daemon.
type MockCopier struct {
	root string // host dir containing one sub-directory per container
}

// NewMockCopier creates (or opens) a mock backend rooted at root.
// Container "c" maps to <root>/c/.
func NewMockCopier(root string) (*MockCopier, error) {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, err
	}
	return &MockCopier{root: root}, nil
}

// EnsureContainer creates the container root and returns its host path.
func (m *MockCopier) EnsureContainer(id string) (string, error) {
	dir := filepath.Join(m.root, sanitizeContainer(id))
	return dir, os.MkdirAll(dir, 0o750)
}

func (m *MockCopier) hostPath(container, absPath string) (string, error) {
	base, err := m.EnsureContainer(container)
	if err != nil {
		return "", err
	}
	// absPath is absolute container path; map under base.
	return filepath.Join(base, filepath.Clean("/"+absPath)), nil
}

func sanitizeContainer(id string) string {
	// Container IDs come from docker; keep this defensive and simple.
	out := make([]byte, 0, len(id))
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "unknown"
	}
	return string(out)
}

// RootFS returns the container's host stand-in directory (jail realpath),
// creating it so jail realpath resolution always has a base.
func (m *MockCopier) RootFS(container string) string {
	dir := filepath.Join(m.root, sanitizeContainer(container))
	_ = os.MkdirAll(dir, 0o750)
	return dir
}

// Backend identifies the backend in logs/audit.
func (m *MockCopier) Backend() string { return "mock" }

// SeedFile writes test fixture content into a mock container.
func (m *MockCopier) SeedFile(container, absPath string, content []byte) error {
	hp, err := m.hostPath(container, absPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(hp), 0o750); err != nil {
		return err
	}
	return os.WriteFile(hp, content, 0o600)
}

// SeedDir creates a directory in a mock container.
func (m *MockCopier) SeedDir(container, absPath string, perm os.FileMode) error {
	hp, err := m.hostPath(container, absPath)
	if err != nil {
		return err
	}
	return os.MkdirAll(hp, perm)
}

// SeedSymlink creates a symlink in a mock container (security tests).
func (m *MockCopier) SeedSymlink(container, absPath, target string) error {
	hp, err := m.hostPath(container, absPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(hp), 0o750); err != nil {
		return err
	}
	return os.Symlink(target, hp)
}

func (m *MockCopier) Stat(_ context.Context, container, absPath string) (FileInfo, error) {
	hp, err := m.hostPath(container, absPath)
	if err != nil {
		return FileInfo{}, err
	}
	fi, err := os.Lstat(hp)
	if err != nil {
		// A path component below the container root is a non-directory:
		// report ENOTDIR uniformly as "not found" so callers (mkdir gate)
		// can distinguish a missing leaf from a broken ancestor. The raw
		// ENOTDIR error is preserved for paths whose leaf exists check.
		if os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR) {
			return FileInfo{}, ErrNotFound
		}
		return FileInfo{}, err
	}
	info := FileInfo{
		Name:    filepath.Base(absPath),
		AbsPath: filepath.Clean(absPath),
		Size:    fi.Size(),
		Mode:    fi.Mode(),
		IsDir:   fi.IsDir(),
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		info.IsSymlink = true
		if target, rerr := os.Readlink(hp); rerr == nil {
			info.LinkTarget = target
		}
	}
	return info, nil
}

func (m *MockCopier) Put(_ context.Context, container, absPath string, r io.Reader, size int64, mode os.FileMode) (int64, error) {
	hp, err := m.hostPath(container, absPath)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(hp), 0o750); err != nil {
		return 0, err
	}
	if mode == 0 {
		mode = 0o600
	}
	tmp, err := os.CreateTemp(filepath.Dir(hp), ".put-*")
	if err != nil {
		return 0, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	n, err := io.Copy(tmp, io.LimitReader(r, size+1))
	if err != nil {
		tmp.Close()
		return 0, err
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	if n > size {
		return 0, ErrTooLarge{n}
	}
	if err := os.Chmod(tmpName, mode.Perm()); err != nil {
		return 0, err
	}
	if err := os.Rename(tmpName, hp); err != nil {
		return 0, err
	}
	return n, nil
}

func (m *MockCopier) Get(_ context.Context, container, absPath string, w io.Writer) (int64, error) {
	hp, err := m.hostPath(container, absPath)
	if err != nil {
		return 0, err
	}
	f, err := os.Open(hp)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if fi.IsDir() {
		return 0, ErrIsDirectory
	}
	return io.Copy(w, f)
}

func (m *MockCopier) ReadDir(_ context.Context, container, absPath string) ([]DirEntry, error) {
	hp, err := m.hostPath(container, absPath)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(hp)
	if err != nil {
		// Distinguish "exists but not a directory" (400) from "missing" (404).
		if fi, serr := os.Lstat(hp); serr == nil && !fi.IsDir() {
			return nil, ErrNotDirectory
		}
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	out := make([]DirEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, DirEntry{
			Name:      e.Name(),
			Size:      info.Size(),
			IsDir:     e.IsDir(),
			IsSymlink: e.Type()&os.ModeSymlink != 0,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *MockCopier) Walk(_ context.Context, container, absPath string, fn WalkFunc) error {
	root, err := m.hostPath(container, absPath)
	if err != nil {
		return err
	}
	return filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return ErrNotFound
			}
			return err
		}
		if path == root {
			if fi.IsDir() {
				return nil
			}
			return fn(".", FileInfo{Name: filepath.Base(absPath), AbsPath: absPath, Size: fi.Size(), Mode: fi.Mode(), IsDir: false})
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		info := FileInfo{
			Name:    filepath.Base(path),
			AbsPath: filepath.Join(absPath, rel),
			Size:    fi.Size(),
			Mode:    fi.Mode(),
			IsDir:   fi.IsDir(),
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			info.IsSymlink = true
			if t, lerr := os.Readlink(path); lerr == nil {
				info.LinkTarget = t
			}
		}
		return fn(rel, info)
	})
}

// ErrTooLarge is returned by Put when the stream exceeds the declared size.
type ErrTooLarge struct{ N int64 }

func (e ErrTooLarge) Error() string { return "files: payload exceeded declared size" }
