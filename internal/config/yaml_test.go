package config

import (
	"strings"
	"testing"
)

func TestParseYAMLBasic(t *testing.T) {
	in := `
# top comment
server: true
port: 9090  # trailing comment
data_root: "/srv/data"
empty: ""
files:
  jail_root: /tmp/jail
  max_file_bytes: 1024
`
	m, err := parseYAML(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ k, want string }{
		{"server", "true"},
		{"port", "9090"},
		{"data_root", "/srv/data"},
		{"empty", ""},
	}
	for _, c := range cases {
		if got, ok := m.get(c.k); !ok || got != c.want {
			t.Errorf("get(%q) = %q,%v want %q", c.k, got, ok, c.want)
		}
	}
	if got, ok := m.getTable("files", "jail_root"); !ok || got != "/tmp/jail" {
		t.Errorf("files.jail_root = %q,%v", got, ok)
	}
	if got, ok := m.getTable("files", "max_file_bytes"); !ok || got != "1024" {
		t.Errorf("files.max_file_bytes = %q,%v", got, ok)
	}
}

func TestParseYAMLRejects(t *testing.T) {
	bad := []string{
		"port 9090\n",            // not key:value
		"port: 9090\nport: 80\n", // duplicate
		"files:\n  port: 1\n  port: 2\n",
		"  port: 9090\n",      // indented without parent
		"port:\t9090\n",       // tab
		"files:\n   bad: 1\n", // 3-space indent
		"weird key: 1\n",      // malformed key
	}
	for i, in := range bad {
		if _, err := parseYAML(strings.NewReader(in)); err == nil {
			t.Errorf("case %d (%q): expected parse error", i, in)
		}
	}
}

func TestStripComment(t *testing.T) {
	if v, ok := stripComment(`a: "x # not comment"`); !ok || !strings.Contains(v, "# not comment") {
		t.Errorf("quoted # stripped incorrectly: %q", v)
	}
	if v, ok := stripComment("a: x  # c"); !ok || strings.Contains(v, "#") {
		t.Errorf("trailing comment not stripped: %q", v)
	}
	if _, ok := stripComment("# full line"); ok {
		t.Error("full-line comment should be dropped")
	}
}

func TestSampleIsValidYAML(t *testing.T) {
	m, err := parseYAML(strings.NewReader(SampleYAML))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateYAML(m); err != nil {
		t.Fatal(err)
	}
	if v, _ := m.get("server"); v != "false" {
		t.Errorf("sample server = %q, want false (safe default)", v)
	}
}
