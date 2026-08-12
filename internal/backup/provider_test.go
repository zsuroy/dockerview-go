package backup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureFromJSON writes a fixture document to a temp file and loads it.
func fixtureFromJSON(t *testing.T, doc string) (*FixtureProvider, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fx.json")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	return NewFixtureProvider(path)
}

func TestFixtureProvider_DefaultSaverSize(t *testing.T) {
	// bytes_per_image omitted → defaults to a small positive value.
	p, err := fixtureFromJSON(t, `{"containers":[],"images":[],"saver":{}}`)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	n, err := p.Saver().SaveImage(context.Background(), "x:1", &sb)
	if err != nil || n <= 0 {
		t.Fatalf("default saver must write a positive payload: n=%d err=%v", n, err)
	}
}

func TestFixtureProvider_SnapshotIsolation(t *testing.T) {
	// Mutating the returned slice must not corrupt the provider's state.
	p, err := fixtureFromJSON(t, `{"containers":[{"id":"a","name":"c1","image":"i:1","status":"running"}],"images":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := p.Snapshot(context.Background())
	first[0].Name = "mutated"
	second, _ := p.Snapshot(context.Background())
	if second[0].Name != "c1" {
		t.Fatalf("snapshot must return a copy, got mutated %q", second[0].Name)
	}
}

func TestEmptyProvider_AllMethods(t *testing.T) {
	var p Provider = EmptyProvider{}
	snaps, err := p.Snapshot(context.Background())
	if err != nil || len(snaps) != 0 {
		t.Fatalf("empty snapshot = %v, %v", snaps, err)
	}
	imgs, err := p.Images(context.Background())
	if err != nil || len(imgs) != 0 {
		t.Fatalf("empty images = %v, %v", imgs, err)
	}
	if p.Saver() == nil {
		t.Fatal("empty provider must still return a saver")
	}
}

func TestBuildRuntimeJSON_TokenModeFalse(t *testing.T) {
	rt := RuntimeConfig{ServerPort: 9090, TokenMode: false}
	data, err := BuildRuntimeJSON(rt, "d", 5, true)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Server struct {
			Port int `json:"port"`
		} `json:"server"`
		Auth struct {
			TokenMode bool `json:"token_mode"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Server.Port != 9090 || doc.Auth.TokenMode {
		t.Fatalf("runtime json wrong: %+v", doc)
	}
}

func TestTruncateNote_Multibyte(t *testing.T) {
	got := truncateNote(strings.Repeat("备", NoteMaxChars+50))
	if len([]rune(got)) != NoteMaxChars {
		t.Fatalf("note must truncate by runes to %d, got %d", NoteMaxChars, len([]rune(got)))
	}
	// short note untouched and trimmed
	if truncateNote("  hi  ") != "hi" {
		t.Fatalf("note must be trimmed")
	}
}

func TestValidArchiveName_ExactShape(t *testing.T) {
	// generated names from Manager.newName must always validate.
	m := newTestManager(t, EmptyProvider{})
	name, id, err := m.newName()
	if err != nil {
		t.Fatal(err)
	}
	if !ValidArchiveName(name) {
		t.Fatalf("generated name %q must be valid", name)
	}
	if !strings.HasPrefix(id, "b-") {
		t.Fatalf("id %q must start with b-", id)
	}
}
