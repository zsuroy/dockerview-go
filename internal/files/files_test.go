package files

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMockCopierRoundtrip(t *testing.T) {
	c, err := NewMockCopier(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Binary fixture: every byte value, twice.
	payload := make([]byte, 512)
	for i := range payload {
		payload[i] = byte(i)
	}
	payload = append(payload, payload...)

	n, err := c.Put(ctx, "c1", "/tmp/jail/sub/blob.bin", bytes.NewReader(payload), int64(len(payload)), 0o600)
	if err != nil || n != int64(len(payload)) {
		t.Fatalf("Put: n=%d err=%v", n, err)
	}

	// Get it back and demand byte-identical roundtrip.
	var got bytes.Buffer
	n, err = c.Get(ctx, "c1", "/tmp/jail/sub/blob.bin", &got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), payload) {
		t.Fatal("roundtrip bytes differ")
	}
	sum := sha256.Sum256(payload)
	wantHash := hex.EncodeToString(sum[:])
	gotSum := sha256.Sum256(got.Bytes())
	if hex.EncodeToString(gotSum[:]) != wantHash {
		t.Fatal("sha256 mismatch")
	}

	// Stat.
	fi, err := c.Stat(ctx, "c1", "/tmp/jail/sub/blob.bin")
	if err != nil || fi.Size != int64(len(payload)) || fi.IsDir {
		t.Fatalf("Stat: %+v err=%v", fi, err)
	}
	if _, err := c.Stat(ctx, "c1", "/tmp/jail/missing"); err != ErrNotFound {
		t.Fatalf("missing stat err = %v", err)
	}

	// ReadDir: immediate children only, sorted, symlinks flagged.
	if err := c.SeedSymlink("c1", "/tmp/jail/link", "sub"); err != nil {
		t.Fatal(err)
	}
	entries, err := c.ReadDir(ctx, "c1", "/tmp/jail")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	var sawLink bool
	for _, e := range entries {
		names = append(names, e.Name)
		if e.Name == "link" {
			sawLink = e.IsSymlink
		}
	}
	if !sawLink || len(entries) != 2 {
		t.Fatalf("readdir entries = %+v", entries)
	}
	if names[0] != "link" || names[1] != "sub" {
		t.Fatalf("unexpected order/names: %v", names)
	}
}

func TestMockCopierPutRejectsOversize(t *testing.T) {
	c, _ := NewMockCopier(t.TempDir())
	ctx := context.Background()
	if _, err := c.Put(ctx, "c", "/tmp/jail/f", strings.NewReader("abcd"), 2, 0o600); err == nil {
		t.Fatal("oversized Put accepted")
	}
	if _, err := c.Stat(ctx, "c", "/tmp/jail/f"); err != ErrNotFound {
		t.Fatal("half-written file left behind")
	}
}

func TestMockCopierWalk(t *testing.T) {
	c, _ := NewMockCopier(t.TempDir())
	ctx := context.Background()
	if err := c.SeedFile("c", "/tmp/jail/a.txt", []byte("a")); err != nil {
		t.Fatal(err)
	}
	if err := c.SeedFile("c", "/tmp/jail/sub/b.txt", []byte("bb")); err != nil {
		t.Fatal(err)
	}
	if err := c.SeedSymlink("c", "/tmp/jail/evil", "/etc/passwd"); err != nil {
		t.Fatal(err)
	}
	var rels []string
	var link string
	err := c.Walk(ctx, "c", "/tmp/jail", func(rel string, fi FileInfo) error {
		rels = append(rels, rel)
		if fi.IsSymlink {
			link = fi.LinkTarget
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(rels, ",")
	if !strings.Contains(joined, "a.txt") || !strings.Contains(joined, filepath.ToSlash(filepath.Join("sub", "b.txt"))) {
		t.Fatalf("walk rels = %v", rels)
	}
	if link != "/etc/passwd" {
		t.Fatalf("walk did not report symlink target: %q", link)
	}
}

func TestTarFileShape(t *testing.T) {
	payload := []byte("hello")
	// Without a jail root: basename entry only.
	tr, err := tarFile("", "/tmp/jail/app.conf", bytes.NewReader(payload), int64(len(payload)), 0o640)
	if err != nil {
		t.Fatal(err)
	}
	r := tar.NewReader(tr)
	hdr, err := r.Next()
	if err != nil {
		t.Fatal(err)
	}
	if hdr.Name != "app.conf" {
		t.Errorf("tar header name = %q, want basename only", hdr.Name)
	}
	if hdr.Size != 5 || hdr.Mode != 0o640 {
		t.Errorf("tar header size=%d mode=%o", hdr.Size, hdr.Mode)
	}
	body, _ := io.ReadAll(r)
	if string(body) != "hello" {
		t.Errorf("tar body = %q", body)
	}

	// With jail root: the FULL directory chain from the filesystem root is
	// emitted (jail root itself included) so CopyToContainer at "/" recreates
	// every missing ancestor — even in containers without the jail dir.
	tr2, err := tarFile("/tmp/jail", "/tmp/jail/sub/deep/app.conf",
		bytes.NewReader(payload), int64(len(payload)), 0o640)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	r2 := tar.NewReader(tr2)
	for {
		h, err := r2.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, h.Name)
	}
	want := []string{"tmp/", "tmp/jail/", "tmp/jail/sub/", "tmp/jail/sub/deep/", "tmp/jail/sub/deep/app.conf"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("tar entries = %v, want %v", names, want)
	}

	// Target directly under the jail root: exactly one dir entry (the root).
	tr3, err := tarFile("/tmp/jail", "/tmp/jail/app.conf",
		bytes.NewReader(payload), int64(len(payload)), 0o640)
	if err != nil {
		t.Fatal(err)
	}
	var names3 []string
	r3 := tar.NewReader(tr3)
	for {
		h, err := r3.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names3 = append(names3, h.Name)
	}
	want3 := []string{"tmp/", "tmp/jail/", "tmp/jail/app.conf"}
	if strings.Join(names3, ",") != strings.Join(want3, ",") {
		t.Fatalf("shallow tar entries = %v, want %v", names3, want3)
	}
}

func TestMockRootFS(t *testing.T) {
	root := t.TempDir()
	c, _ := NewMockCopier(root)
	rfs := c.RootFS("web-1")
	if !strings.HasPrefix(filepath.Clean(rfs), filepath.Clean(root)) {
		t.Fatalf("RootFS outside mock root: %s", rfs)
	}
	// The stand-in directory exists and is a sibling layout of the real one.
	if fi, err := os.Stat(rfs); err != nil || !fi.IsDir() {
		t.Fatalf("container FS stand-in missing: %v", err)
	}
}
