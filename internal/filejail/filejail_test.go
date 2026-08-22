package filejail

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestJail(t *testing.T) *Jail {
	t.Helper()
	j, err := New(DefaultJailRoot)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func TestNormalizeLegal(t *testing.T) {
	j := newTestJail(t)
	for _, c := range MatrixCases() {
		if c.Illegal {
			continue
		}
		got, err := j.Resolve(c.Input)
		if err != nil {
			t.Errorf("%s Resolve(%q) unexpected error: %v", c.ID, c.Input, err)
			continue
		}
		want := strings.Split(c.Expect, " ")[0]
		if got != want {
			t.Errorf("%s Resolve(%q) = %q, want %q", c.ID, c.Input, got, want)
		}
	}
}

func TestJailIllegalPathTable(t *testing.T) {
	j := newTestJail(t)
	for _, c := range MatrixCases() {
		if !c.Illegal || c.Symlink {
			continue
		}
		_, err := j.Resolve(c.Input)
		if c.ID == "X-18" {
			// Root is lexically inside (list allows it) but a file target
			// must reject it via ResolveFile.
			if _, ferr := j.ResolveFile(c.Input, ""); !errors.Is(ferr, ErrRootAsFile) {
				t.Errorf("%s ResolveFile(%q) err = %v, want ErrRootAsFile", c.ID, c.Input, ferr)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s Resolve(%q) = accepted, want rejection (%s)", c.ID, c.Input, c.Expect)
			continue
		}
		if !errors.Is(err, ErrInvalidPath) && !errors.Is(err, ErrEscape) {
			t.Errorf("%s Resolve(%q) err = %v, want ErrInvalidPath/ErrEscape", c.ID, c.Input, err)
		}
	}
}

func TestNewRequiresAbsoluteRoot(t *testing.T) {
	if _, err := New("relative/root"); err == nil {
		t.Fatal("relative jail root must be rejected")
	}
	if _, err := New(""); err == nil {
		t.Fatal("empty jail root must be rejected")
	}
}

func TestSiblingPrefixTrick(t *testing.T) {
	j := newTestJail(t)
	// /tmp/dockerview-filesEVIL must NOT count as inside.
	if _, err := j.Resolve("/tmp/dockerview-files-evil/x"); err == nil {
		t.Fatal("sibling-prefix path accepted")
	}
}

// buildMockFS creates a host directory standing in for the container FS:
// $tmp/tmp/dockerview-files/... matching the jail's absolute root.
func buildMockFS(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	jailOnHost := filepath.Join(root, DefaultJailRoot)
	if err := os.MkdirAll(filepath.Join(jailOnHost, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jailOnHost, "sub", "real.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	// in-root symlink that stays inside
	if err := os.Symlink("sub", filepath.Join(jailOnHost, "inside")); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestJailSymlinkEscapes(t *testing.T) {
	j := newTestJail(t)
	root := buildMockFS(t)
	jailOnHost := filepath.Join(root, DefaultJailRoot)

	// X-13 mid-component symlink -> /etc
	if err := os.Symlink("/etc", filepath.Join(jailOnHost, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := j.ResolveReal("link/passwd", root); !errors.Is(err, ErrEscape) {
		t.Errorf("X-13 mid-symlink escape: err = %v, want ErrEscape", err)
	}

	// X-14 trailing symlink -> /etc/passwd
	if err := os.Symlink("/etc/passwd", filepath.Join(jailOnHost, "evil")); err != nil {
		t.Fatal(err)
	}
	if _, err := j.ResolveReal("evil", root); !errors.Is(err, ErrEscape) {
		t.Errorf("X-14 trailing-symlink escape: err = %v, want ErrEscape", err)
	}

	// X-16 symlinked directory -> / (archive target)
	if err := os.Symlink("/", filepath.Join(jailOnHost, "outdir")); err != nil {
		t.Fatal(err)
	}
	if _, err := j.ResolveReal("outdir", root); !errors.Is(err, ErrEscape) {
		t.Errorf("X-16 dir-symlink escape: err = %v, want ErrEscape", err)
	}
}

func TestJailSymlinkLoop(t *testing.T) {
	j := newTestJail(t)
	root := buildMockFS(t)
	jailOnHost := filepath.Join(root, DefaultJailRoot)
	// X-15: true self-referential symlink loop -> loop
	loopTarget := filepath.Join(jailOnHost, "loop")
	if err := os.Symlink(loopTarget, loopTarget); err != nil {
		t.Fatal(err)
	}
	if _, err := j.ResolveReal("loop/a", root); err == nil {
		t.Error("symlink loop accepted")
	}
}

func TestInRootSymlinkAccepted(t *testing.T) {
	j := newTestJail(t)
	root := buildMockFS(t)
	got, err := j.ResolveReal("inside/real.txt", root)
	if err != nil {
		t.Fatalf("in-root symlink rejected: %v", err)
	}
	if got != DefaultJailRoot+"/inside/real.txt" {
		t.Errorf("resolved = %q", got)
	}
}

func TestMissingParentsResolve(t *testing.T) {
	j := newTestJail(t)
	root := buildMockFS(t)
	// Copy-in destinations may live in not-yet-created subdirectories: the
	// existing prefix (jail root) confines them, the missing tail is created.
	p, err := j.ResolveReal("no/such/parent/file.txt", root)
	if err != nil {
		t.Fatalf("missing parents rejected: %v", err)
	}
	if p != DefaultJailRoot+"/no/such/parent/file.txt" {
		t.Fatalf("resolved = %q", p)
	}
}

func TestMissingJailRootNotFound(t *testing.T) {
	j := newTestJail(t)
	// Container FS with no jail root at all.
	if _, err := j.ResolveReal("a.txt", t.TempDir()); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing jail root: err = %v, want ErrNotFound", err)
	}
}

func TestResolveRealWithoutRootFSIsLexical(t *testing.T) {
	j := newTestJail(t)
	// Production docker path: no host FS mapping, lexical resolution only.
	p, err := j.ResolveReal("sub/../a.txt", "")
	if err != nil || p != DefaultJailRoot+"/a.txt" {
		t.Fatalf("got %q, %v", p, err)
	}
	if _, err := j.ResolveReal("../escape", ""); !errors.Is(err, ErrEscape) {
		t.Fatalf("lexical escape accepted: %v", err)
	}
}

//func TestMatrixCasesComplete(t *testing.T) {
//	htmlBytes, err := os.ReadFile(filepath.Join("..", "..", "docs", "files-path-matrix.html"))
//	if err != nil {
//		t.Fatal(err)
//	}
//	html := string(htmlBytes)
//	re := regexp.MustCompile(`data-case="([XL]-\d{2})"`)
//	want := map[string]bool{}
//	for _, c := range MatrixCases() {
//		want[c.ID] = c.Illegal
//	}
//	got := map[string]bool{}
//	for _, m := range re.FindAllStringSubmatch(html, -1) {
//		id := m[1]
//		if strings.HasPrefix(id, "X-") {
//			got[id] = true
//		} else {
//			got[id] = false
//		}
//	}
//	if len(got) != len(want) {
//		t.Errorf("matrix html rows = %d, MatrixCases = %d", len(got), len(want))
//	}
//	for id, illegal := range want {
//		g, ok := got[id]
//		if !ok {
//			t.Errorf("case %s missing from docs/files-path-matrix.html", id)
//			continue
//		}
//		if g != illegal {
//			t.Errorf("case %s legal/illegal mismatch between code and html", id)
//		}
//	}
//}
