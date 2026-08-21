// Package filejail is the path jail for the container-side whitelist root.
// Every container path used by the file-transfer feature is normalized and
// confined here BEFORE any filesystem or docker IO happens. The package has
// no HTTP dependencies and is unit-tested in isolation (see
// docs/files-path-matrix.html — MatrixCases is the single source shared by
// tests, drills and the matrix document).
package filejail

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Limits enforced on the raw input.
const (
	MaxPathLen    = 4096
	MaxSegmentLen = 255
)

var (
	// ErrInvalidPath: NUL, backslash, Windows drive/UNC, overlong, empty.
	ErrInvalidPath = errors.New("filejail: invalid path")
	// ErrEscape: normalization or symlink resolution lands outside root.
	ErrEscape = errors.New("filejail: path escapes jail root")
	// ErrNotFound: an existing component was required but is missing.
	ErrNotFound = errors.New("filejail: path not found")
	// ErrRootAsFile: root itself given where a file target is required.
	ErrRootAsFile = errors.New("filejail: jail root is not a valid file target")
)

var driveRe = regexp.MustCompile(`^[A-Za-z]:`)

// Jail is rooted at an absolute POSIX path inside the container.
type Jail struct {
	root string
}

// New creates a jail. root must be absolute.
func New(root string) (*Jail, error) {
	if root == "" || !strings.HasPrefix(root, "/") {
		return nil, fmt.Errorf("%w: jail root must be an absolute POSIX path: %q", ErrInvalidPath, root)
	}
	return &Jail{root: filepath.Clean(root)}, nil
}

// Root returns the lexical (configured) root.
func (j *Jail) Root() string { return j.root }

// Normalize validates raw user input and returns a cleaned absolute path,
// WITHOUT jail containment or symlink checks. It rejects the characters
// and shapes that must never reach a container API.
func (j *Jail) Normalize(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("%w: empty path", ErrInvalidPath)
	}
	if len(raw) > MaxPathLen {
		return "", fmt.Errorf("%w: longer than %d bytes", ErrInvalidPath, MaxPathLen)
	}
	if strings.ContainsRune(raw, 0) {
		return "", fmt.Errorf("%w: NUL byte", ErrInvalidPath)
	}
	if strings.ContainsRune(raw, '\\') {
		return "", fmt.Errorf("%w: backslashes are not allowed (Windows/UNC paths rejected)", ErrInvalidPath)
	}
	if driveRe.MatchString(raw) {
		return "", fmt.Errorf("%w: Windows drive prefix", ErrInvalidPath)
	}
	for _, seg := range strings.Split(raw, "/") {
		if len(seg) > MaxSegmentLen {
			return "", fmt.Errorf("%w: path segment longer than %d bytes", ErrInvalidPath, MaxSegmentLen)
		}
	}
	if strings.HasPrefix(raw, "/") {
		return filepath.Clean(raw), nil
	}
	return filepath.Clean(filepath.Join(j.root, raw)), nil
}

// Resolve confines raw to the jail root (lexical only — symlinks are NOT
// followed, so this is suitable for path display and validation of
// not-yet-existing destinations).
func (j *Jail) Resolve(raw string) (string, error) {
	p, err := j.Normalize(raw)
	if err != nil {
		return "", err
	}
	if !j.inside(p, j.root) {
		return "", fmt.Errorf("%w: %q -> %q", ErrEscape, raw, p)
	}
	return p, nil
}

// ResolveReal confines raw AND walks symlinks with CONTAINER path
// semantics: rootFS is the host directory that stands in for the container
// filesystem, and an absolute symlink target such as "/etc/passwd" is
// interpreted inside that namespace (rootFS/etc/passwd), not on the host.
// Any hop (including the not-yet-created tail, which is appended
// lexically) must stay inside the root. rootFS == "" selects lexical-only
// resolution, used by the docker backend where stat happens inside the
// container via tar streams.
func (j *Jail) ResolveReal(raw, rootFS string) (string, error) {
	p, err := j.Resolve(raw)
	if err != nil {
		return "", err
	}
	if rootFS == "" {
		return p, nil
	}
	// Jail root must exist inside the container FS.
	if _, err := os.Stat(filepath.Join(rootFS, j.root)); err != nil {
		return "", fmt.Errorf("%w: jail root missing in container FS: %v", ErrNotFound, err)
	}
	visited := map[string]int{}
	cur := "/"
	for _, seg := range strings.Split(strings.TrimPrefix(p, "/"), "/") {
		if seg == "" {
			continue
		}
		cand := filepath.Join(cur, seg)
		host := filepath.Join(rootFS, filepath.Clean("/"+cand))
		cur = cand
		if fi, lerr := os.Lstat(host); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
			target, rerr := os.Readlink(host)
			if rerr != nil {
				return "", fmt.Errorf("%w: readlink %q: %v", ErrEscape, raw, rerr)
			}
			if strings.HasPrefix(target, "/") {
				cur = filepath.Clean(target)
			} else {
				// Relative to the directory containing the link.
				cur = filepath.Clean(filepath.Join(filepath.Dir(cand), target))
			}
			key := cur + "\x00" + cand
			visited[key]++
			if visited[key] > 1 || len(visited) > 64 {
				return "", fmt.Errorf("%w: symlink loop at %q", ErrEscape, raw)
			}
			// Symlinks are the ONLY component that can redirect the walk;
			// every redirect must land back inside the jail immediately.
			if !j.inside(cur, j.root) {
				return "", fmt.Errorf("%w: symlink resolution escapes jail: %q -> %q", ErrEscape, raw, cur)
			}
		}
	}
	if !j.inside(cur, j.root) {
		return "", fmt.Errorf("%w: symlink resolution escapes jail: %q -> %q", ErrEscape, raw, cur)
	}
	return p, nil
}

// ResolveFile is ResolveReal plus the in/out rule that the target may not
// be the jail root itself.
func (j *Jail) ResolveFile(raw, rootFS string) (string, error) {
	p, err := j.ResolveReal(raw, rootFS)
	if err != nil {
		return "", err
	}
	if p == j.root {
		return "", ErrRootAsFile
	}
	return p, nil
}

func (j *Jail) inside(p, root string) bool {
	if p == root {
		return true
	}
	return strings.HasPrefix(p, root+string(filepath.Separator))
}
