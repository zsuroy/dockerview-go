package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFixture(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "containers.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadContainerFixtureReadsTheRepoFixture(t *testing.T) {
	got, err := loadContainerFixture("testdata/containers.json")
	if err != nil {
		t.Fatalf("loadContainerFixture: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d containers, want 3", len(got))
	}

	byName := map[string]int{}
	for _, c := range got {
		byName[c.Name] = c.HealthScore
	}
	if byName["api"] != 42 {
		t.Errorf("api health score = %d, want 42 — the E2E relies on api being the unhealthy one", byName["api"])
	}
	if byName["web"] != 91 {
		t.Errorf("web health score = %d, want 91", byName["web"])
	}
	if _, ok := byName["worker"]; !ok {
		t.Error("the fixture should include a stopped container")
	}
}

func TestLoadContainerFixtureParsesPorts(t *testing.T) {
	got, err := loadContainerFixture("testdata/containers.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(got[0].Ports) == 0 {
		t.Fatal("ports did not parse; the fixture must use docker.PortMapping's json tags")
	}
	if got[0].Ports[0].PrivatePort != 8080 {
		t.Fatalf("private port = %d, want 8080", got[0].Ports[0].PrivatePort)
	}
	if got[0].Ports[0].Type != "tcp" {
		t.Fatalf("port type = %q, want tcp", got[0].Ports[0].Type)
	}
}

func TestLoadContainerFixtureRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"not json":     `{`,
		"empty array":  `[]`,
		"missing name": `[{"ID":"abc","Status":"running"}]`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := loadContainerFixture(writeFixture(t, body)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestLoadContainerFixtureMissingFile(t *testing.T) {
	if _, err := loadContainerFixture(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}
