package wecom

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveModeDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false
	cfg.BotID = "ww123"
	cfg.Secret = "s3cr3t"

	mode, err := cfg.ResolveMode()
	if err != nil {
		t.Fatalf("ResolveMode: %v", err)
	}
	if mode != ModeOff {
		t.Fatalf("mode = %q, want %q even with credentials present", mode, ModeOff)
	}
}

func TestResolveModeMockWithoutCredentials(t *testing.T) {
	t.Setenv("WECOM_BOT_ID", "")
	t.Setenv("WECOM_BOT_SECRET", "")

	cases := []struct {
		name   string
		botID  string
		secret string
	}{
		{"both empty", "", ""},
		{"bot id only", "ww123", ""},
		{"secret only", "", "s3cr3t"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Enabled = true
			cfg.BotID = tc.botID
			cfg.Secret = tc.secret

			mode, err := cfg.ResolveMode()
			if err != nil {
				t.Fatalf("ResolveMode: %v", err)
			}
			if mode != ModeMock {
				t.Fatalf("mode = %q, want %q", mode, ModeMock)
			}
		})
	}
}

func TestResolveModeLive(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.BotID = "ww123"
	cfg.Secret = "s3cr3t"

	mode, err := cfg.ResolveMode()
	if err != nil {
		t.Fatalf("ResolveMode: %v", err)
	}
	if mode != ModeLive {
		t.Fatalf("mode = %q, want %q", mode, ModeLive)
	}
}

func TestResolveSecretPrecedence(t *testing.T) {
	dir := t.TempDir()
	secretFile := filepath.Join(dir, "secret")
	if err := os.WriteFile(secretFile, []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("explicit secret wins", func(t *testing.T) {
		t.Setenv("WECOM_BOT_SECRET", "from-env")
		cfg := DefaultConfig()
		cfg.Secret = "from-config"
		cfg.SecretFile = secretFile

		got, err := cfg.ResolveSecret()
		if err != nil {
			t.Fatal(err)
		}
		if got != "from-config" {
			t.Fatalf("secret = %q, want from-config", got)
		}
	})

	t.Run("secret file next", func(t *testing.T) {
		t.Setenv("WECOM_BOT_SECRET", "from-env")
		cfg := DefaultConfig()
		cfg.SecretFile = secretFile

		got, err := cfg.ResolveSecret()
		if err != nil {
			t.Fatal(err)
		}
		if got != "from-file" {
			t.Fatalf("secret = %q, want from-file", got)
		}
	})

	t.Run("env last", func(t *testing.T) {
		t.Setenv("WECOM_BOT_SECRET", "from-env")
		cfg := DefaultConfig()

		got, err := cfg.ResolveSecret()
		if err != nil {
			t.Fatal(err)
		}
		if got != "from-env" {
			t.Fatalf("secret = %q, want from-env", got)
		}
	})

	t.Run("nothing configured", func(t *testing.T) {
		t.Setenv("WECOM_BOT_SECRET", "")
		cfg := DefaultConfig()

		got, err := cfg.ResolveSecret()
		if err != nil {
			t.Fatal(err)
		}
		if got != "" {
			t.Fatalf("secret = %q, want empty", got)
		}
	})
}

func TestResolveSecretMissingFileIsAnError(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SecretFile = filepath.Join(t.TempDir(), "nope")
	if _, err := cfg.ResolveSecret(); err == nil {
		t.Fatal("expected an error for a missing secret_file")
	}
}

func TestResolveSecretEmptyFileIsAnError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "secret")
	if err := os.WriteFile(p, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.SecretFile = p
	if _, err := cfg.ResolveSecret(); err == nil {
		t.Fatal("expected an error for an empty secret_file")
	}
}

func TestResolveBotIDFallsBackToEnv(t *testing.T) {
	t.Setenv("WECOM_BOT_ID", "ww-env")
	cfg := DefaultConfig()
	if got := cfg.ResolveBotID(); got != "ww-env" {
		t.Fatalf("ResolveBotID = %q, want ww-env", got)
	}
	cfg.BotID = "ww-config"
	if got := cfg.ResolveBotID(); got != "ww-config" {
		t.Fatalf("ResolveBotID = %q, want config to win", got)
	}
}

func TestResolveWSURLUsesEnvFallback(t *testing.T) {
	t.Setenv("WECOM_WS_URL", "ws://127.0.0.1:9/ws")
	cfg := DefaultConfig()
	if got := cfg.ResolveWSURL(); got != "ws://127.0.0.1:9/ws" {
		t.Fatalf("ResolveWSURL = %q, want the env value", got)
	}
	cfg.WSURL = "ws://example.invalid/ws"
	if got := cfg.ResolveWSURL(); got != "ws://example.invalid/ws" {
		t.Fatalf("ResolveWSURL = %q, want the explicit value to win", got)
	}
}

func TestMaskBotID(t *testing.T) {
	cases := map[string]string{
		"":            "",
		"ww12":        "****",
		"ww12345678":  "ww12****",
		"  ww123456 ": "ww12****",
	}
	for in, want := range cases {
		if got := MaskBotID(in); got != want {
			t.Errorf("MaskBotID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolvedReplyModeDefaultsToStream(t *testing.T) {
	for _, in := range []string{"", "stream", "STREAM", "  stream  ", "bogus"} {
		cfg := DefaultConfig()
		cfg.ReplyMode = in
		if got := cfg.resolvedReplyMode(); got != ReplyStream {
			t.Errorf("reply mode %q resolved to %q, want stream", in, got)
		}
	}
	cfg := DefaultConfig()
	cfg.ReplyMode = "markdown"
	if got := cfg.resolvedReplyMode(); got != ReplyMarkdown {
		t.Errorf("reply mode markdown resolved to %q", got)
	}
}

func TestWelcomeTextDefaultAndOverride(t *testing.T) {
	cfg := DefaultConfig()
	if got := cfg.welcomeText(); got != DefaultWelcome {
		t.Fatalf("welcome = %q, want the default", got)
	}
	cfg.Welcome = "  自定义欢迎语  "
	if got := cfg.welcomeText(); got != "  自定义欢迎语  " {
		t.Fatalf("welcome = %q, want the override", got)
	}
	if !strings.Contains(DefaultWelcome, "Web") {
		t.Fatal("the default welcome should point at the web console")
	}
}

func TestGroupWebhookResolution(t *testing.T) {
	t.Run("disabled yields false", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.GroupWebhookURL = "https://example.invalid/hook"
		got, err := cfg.ResolveGroupWebhookURL()
		if err != nil {
			t.Fatal(err)
		}
		if got == "" {
			t.Fatal("ResolveGroupWebhookURL should still return the configured URL")
		}
		if cfg.GroupWebhookEnabled {
			t.Fatal("group webhook must default to disabled")
		}
	})

	t.Run("file source", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "hook")
		if err := os.WriteFile(p, []byte("https://example.invalid/from-file\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg := DefaultConfig()
		cfg.GroupWebhookURLFile = p
		got, err := cfg.ResolveGroupWebhookURL()
		if err != nil {
			t.Fatal(err)
		}
		if got != "https://example.invalid/from-file" {
			t.Fatalf("webhook url = %q", got)
		}
	})
}
