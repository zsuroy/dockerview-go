package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// resolveWithYAML drops a config.yaml into a temp dir and resolves it with the
// given environment. It follows the same ResolveWithEnv convention as the rest
// of this package's tests, so nothing depends on the ambient environment.
func resolveWithYAML(t *testing.T, body string, env map[string]string) *Config {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ConfigFileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	m := map[string]string{EnvConfigDir: dir}
	for k, v := range env {
		m[k] = v
	}
	cfg, err := ResolveWithEnv(CLI{}, envMap(m))
	if err != nil {
		t.Fatalf("ResolveWithEnv: %v", err)
	}
	return cfg
}

func TestWeComDefaultsAreOff(t *testing.T) {
	cfg := resolveWithYAML(t, "port: 8080\n", nil)

	if cfg.WeCom.Enabled {
		t.Fatal("wecom must default to disabled")
	}
	if cfg.WeCom.BotID != "" {
		t.Fatalf("bot id = %q, want empty", cfg.WeCom.BotID)
	}
	if cfg.WeCom.ReplyMode != "stream" {
		t.Fatalf("reply mode = %q, want the stream default", cfg.WeCom.ReplyMode)
	}
	if cfg.WeCom.GroupWebhookEnabled {
		t.Fatal("the group webhook must default to disabled")
	}
	if cfg.Sources["wecom_enabled"] != LayerDefault {
		t.Fatalf("source = %q, want default", cfg.Sources["wecom_enabled"])
	}
}

func TestWeComReadsTheYAMLTable(t *testing.T) {
	cfg := resolveWithYAML(t, `port: 8080
wecom:
  enabled: true
  bot_id: ww-from-yaml
  reply_mode: markdown
  welcome: "你好"
  ws_url: ws://127.0.0.1:9999/ws
`, nil)

	if !cfg.WeCom.Enabled {
		t.Fatal("wecom.enabled: true was not honored")
	}
	if cfg.WeCom.BotID != "ww-from-yaml" {
		t.Fatalf("bot id = %q", cfg.WeCom.BotID)
	}
	if cfg.WeCom.ReplyMode != "markdown" {
		t.Fatalf("reply mode = %q", cfg.WeCom.ReplyMode)
	}
	if cfg.WeCom.Welcome != "你好" {
		t.Fatalf("welcome = %q", cfg.WeCom.Welcome)
	}
	if cfg.WeCom.WSURL != "ws://127.0.0.1:9999/ws" {
		t.Fatalf("ws url = %q", cfg.WeCom.WSURL)
	}
	if cfg.Sources["wecom_bot_id"] != LayerYAML {
		t.Fatalf("source = %q, want yaml", cfg.Sources["wecom_bot_id"])
	}
}

func TestWeComEnvBeatsYAML(t *testing.T) {
	cfg := resolveWithYAML(t, `port: 8080
wecom:
  enabled: false
  bot_id: ww-from-yaml
  reply_mode: stream
`, map[string]string{
		EnvWeComEnabled:   "true",
		EnvWeComBotID:     "ww-from-env",
		EnvWeComReplyMode: "markdown",
	})

	if !cfg.WeCom.Enabled {
		t.Fatal("env should have enabled wecom")
	}
	if cfg.WeCom.BotID != "ww-from-env" {
		t.Fatalf("bot id = %q, want the env value", cfg.WeCom.BotID)
	}
	if cfg.WeCom.ReplyMode != "markdown" {
		t.Fatalf("reply mode = %q, want the env value", cfg.WeCom.ReplyMode)
	}
	if cfg.Sources["wecom_bot_id"] != LayerEnv {
		t.Fatalf("source = %q, want env", cfg.Sources["wecom_bot_id"])
	}
}

func TestWeComSecretIsNeverReadFromYAML(t *testing.T) {
	// The schema does not have a `secret` or `bot_secret` key under wecom, so
	// a config that tries to put the credential in yaml is rejected outright
	// rather than silently ignored. That is the point: an operator who writes
	// it should find out immediately, not discover months later that the value
	// sat in a world-readable file doing nothing.
	for _, body := range []string{
		"port: 8080\nwecom:\n  enabled: true\n  secret: super-secret-value\n",
		"port: 8080\nwecom:\n  enabled: true\n  bot_secret: another-secret\n",
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, ConfigFileName), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := ResolveWithEnv(CLI{}, envMap(map[string]string{EnvConfigDir: dir}))
		if err == nil {
			t.Fatalf("config with an inline secret was accepted (Secret=%q); it must be rejected", cfg.WeCom.Secret)
		}
		if !strings.Contains(err.Error(), "wecom") {
			t.Fatalf("error = %v, want it to name the offending wecom key", err)
		}
	}
}

func TestWeComSecretComesFromEnv(t *testing.T) {
	cfg := resolveWithYAML(t, "port: 8080\nwecom:\n  enabled: true\n  bot_id: ww-x\n",
		map[string]string{EnvWeComSecret: "env-secret"})

	if cfg.WeCom.Secret != "env-secret" {
		t.Fatalf("Secret = %q, want the env value", cfg.WeCom.Secret)
	}
	if cfg.Sources["wecom_secret"] != LayerEnv {
		t.Fatalf("source = %q, want env", cfg.Sources["wecom_secret"])
	}
}

func TestWeComSecretFileIsMadeAbsolute(t *testing.T) {
	cfg := resolveWithYAML(t, "port: 8080\nwecom:\n  enabled: true\n  secret_file: relative/secret\n", nil)

	if cfg.WeCom.SecretFile == "" {
		t.Fatal("secret_file was dropped")
	}
	if !filepath.IsAbs(cfg.WeCom.SecretFile) {
		t.Fatalf("secret_file = %q, want an absolute path", cfg.WeCom.SecretFile)
	}
}

func TestWeComRejectsABadBool(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ConfigFileName),
		[]byte("port: 8080\nwecom:\n  enabled: maybe\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveWithEnv(CLI{}, envMap(map[string]string{EnvConfigDir: dir})); err == nil {
		t.Fatal("expected an error for wecom.enabled: maybe")
	}
}

func TestWeComGroupWebhookSwitches(t *testing.T) {
	cfg := resolveWithYAML(t, `port: 8080
wecom:
  enabled: true
  group_webhook_enabled: true
  group_webhook_url_file: /etc/dockerview/hook
`, nil)
	if !cfg.WeCom.GroupWebhookEnabled {
		t.Fatal("group_webhook_enabled was not honored")
	}
	if cfg.WeCom.GroupWebhookURLFile != "/etc/dockerview/hook" {
		t.Fatalf("group webhook file = %q", cfg.WeCom.GroupWebhookURLFile)
	}
}

func TestSampleYAMLNeverCarriesASecret(t *testing.T) {
	// The generated sample is the first thing an operator reads. It must show
	// the shape of the config without ever containing a credential.
	lower := strings.ToLower(SampleYAML)
	if strings.Contains(lower, "secret:") {
		t.Error("SampleYAML must not contain a `secret:` key")
	}
	if !strings.Contains(lower, "wecom:") {
		t.Error("SampleYAML should document the wecom table")
	}
	if !strings.Contains(SampleYAML, "WECOM_BOT_SECRET") {
		t.Error("SampleYAML should name the environment variable for the secret")
	}
	if !strings.Contains(lower, "secret_file") {
		t.Error("SampleYAML should point at secret_file as the file-based option")
	}
}
