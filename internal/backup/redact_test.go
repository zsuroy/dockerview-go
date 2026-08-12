package backup

import (
	"strings"
	"testing"
)

func TestRedactEnv_MasksSecrets(t *testing.T) {
	in := []string{
		"DB_PASSWORD=hunter2",
		"API_TOKEN=abc123",
		"AWS_SECRET_KEY=AKIA",
		"APP_MODE=production",
	}
	out, redacted := RedactEnv(in)
	want := map[string]bool{"DB_PASSWORD": true, "API_TOKEN": true, "AWS_SECRET_KEY": true}
	if len(redacted) != 3 {
		t.Fatalf("redacted keys = %v, want 3 entries", redacted)
	}
	for _, k := range redacted {
		if !want[k] {
			t.Fatalf("unexpected redacted key %q", k)
		}
	}
	// sorted order
	if !(redacted[0] == "API_TOKEN" && redacted[1] == "AWS_SECRET_KEY" && redacted[2] == "DB_PASSWORD") {
		t.Fatalf("redacted keys not sorted: %v", redacted)
	}
	for _, e := range out {
		k, v, _ := strings.Cut(e, "=")
		if want[k] && v != MaskedValue {
			t.Fatalf("secret %q not masked: %q", k, e)
		}
		if k == "APP_MODE" && v != "production" {
			t.Fatalf("non-secret env lost: %q", e)
		}
	}
}

func TestRedactEnv_KeepsNormalVars(t *testing.T) {
	in := []string{"PATH=/usr/bin", "TZ=UTC", "LANG=C.UTF-8", "APP_MODE=staging"}
	out, redacted := RedactEnv(in)
	if len(redacted) != 0 {
		t.Fatalf("nothing should be redacted, got %v", redacted)
	}
	if strings.Join(out, ";") != strings.Join(in, ";") {
		t.Fatalf("non-secret env must pass through unchanged: %v", out)
	}
}

func TestRedactEnv_CaseInsensitive(t *testing.T) {
	for _, k := range []string{"db_password", "Api_Token", "mySecret", "PRIVATE_KEY", "connection_string"} {
		if !IsSecretKey(k) {
			t.Fatalf("key %q must be treated as secret", k)
		}
	}
	out, redacted := RedactEnv([]string{"db_password=p@ss"})
	if len(redacted) != 1 || out[0] != "db_password="+MaskedValue {
		t.Fatalf("case-insensitive masking failed: %v / %v", out, redacted)
	}
}

func TestRedactEnv_EmptyValueAndNoEquals(t *testing.T) {
	out, redacted := RedactEnv([]string{"SOME_TOKEN=", "NOEQUALS", "A_KEY=1"})
	if len(redacted) != 2 {
		t.Fatalf("want SOME_TOKEN and A_KEY redacted, got %v", redacted)
	}
	if out[0] != "SOME_TOKEN="+MaskedValue {
		t.Fatalf("empty secret value must still be masked: %q", out[0])
	}
	if out[1] != "NOEQUALS" {
		t.Fatalf("entry without '=' must pass through: %q", out[1])
	}
}

func TestRedactEnv_EmptyInput(t *testing.T) {
	out, redacted := RedactEnv(nil)
	if out == nil || len(out) != 0 {
		t.Fatalf("want empty non-nil slice, got %#v", out)
	}
	if redacted == nil || len(redacted) != 0 {
		t.Fatalf("want empty non-nil redacted slice, got %#v", redacted)
	}
}

func TestIsSecretKey_Negative(t *testing.T) {
	for _, k := range []string{"PATH", "TZ", "HOME", "APP_MODE", "LOG_LEVEL"} {
		if IsSecretKey(k) {
			t.Fatalf("key %q must NOT be treated as secret", k)
		}
	}
}
