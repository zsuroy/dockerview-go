package audit

import (
	"strings"
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"24h", 24 * time.Hour, true},
		{"1h30m", 90 * time.Minute, true},
		{"7d", 7 * 24 * time.Hour, true},
		{"2w", 14 * 24 * time.Hour, true},
		{"0d", 0, false},
		{"-1d", 0, false},
		{"abc", 0, false},
		{"", 0, false},
		{"1.5d", 36 * time.Hour, true},
	}
	for _, c := range cases {
		got, ok := ParseDuration(c.in)
		if ok != c.ok {
			t.Errorf("ParseDuration(%q) ok=%v want %v (got=%v)", c.in, ok, c.ok, got)
			continue
		}
		if c.ok && got != c.want {
			t.Errorf("ParseDuration(%q)=%v want %v", c.in, got, c.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("abc", 10); got != "abc" {
		t.Fatalf("short truncate: %q", got)
	}
	if got := truncate("abcdef", 3); got != "abc" {
		t.Fatalf("hard truncate: %q", got)
	}
}

func TestMarshalPayloadOmitStdout(t *testing.T) {
	p := map[string]any{"stdout": strings.Repeat("x", 5000), "cmd": "echo hi"}
	out := marshalPayload(p)
	if strings.Contains(out, strings.Repeat("x", 100)) {
		t.Fatal("stdout should be replaced by omission marker")
	}
	if !strings.Contains(out, `"cmd"`) {
		t.Fatalf("cmd should be preserved: %s", out)
	}
	if len(out) > MaxPayloadBytes+100 {
		t.Fatalf("payload too large: %d", len(out))
	}
}

func TestNormalizeContainerID(t *testing.T) {
	cases := map[string]string{
		"docker://sha256:abc123def456": "abc123def456",
		"sha256:abcdef":                "abcdef",
		"abc123":                       "abc123",
	}
	for in, want := range cases {
		if got := normalizeContainerID(in); got != want {
			t.Errorf("normalize(%q)=%q want %q", in, got, want)
		}
	}
}
