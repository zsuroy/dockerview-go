package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// SampleYAML is the commented example written on first run and by
// `-config-init`. It intentionally contains NO token: secrets come from
// -token, DOCKERVIEW_TOKEN, or a token_file referenced (but never created)
// here. Keep it in sync with validateYAML's allowlist.
const SampleYAML = `# dockerview configuration.
# Precedence for every setting: command-line flag > DOCKERVIEW_* env >
# this file > built-in defaults. Edit, then just run: dockerview
#
# This file was generated on first launch; it is never overwritten.

# Start the web dashboard when running bare ` + "`dockerview`" + ` (no -server flag).
# Flip to true once the box is configured; operators then only type ` + "`dockerview`" + `.
server: false

# HTTP listen port.
port: 8080

# Audit log (SQLite). The DB always lives under DataRoot: data/db/audit.db.
audit_enabled: true
audit_retention_days: 90

# Backup snapshot retention count (archives themselves stay in
# data/backups; this tool never deletes old snapshots silently here).
backup_max: 10

# Override the data tree location. MUST be absolute.
# Leave empty for the default: the data/ folder next to this file.
# DOCKERVIEW_DATA_DIR beats this; tests set DOCKERVIEW_CONFIG_DIR to a temp dir.
data_root: ""

# File holding the admin token (one line). Never write the token itself
# into this file. Example:
#   echo 'my-secret' > /etc/dockerview/token && chmod 600 /etc/dockerview/token
# token_file: /etc/dockerview/token

files:
  # In-container whitelist root. Copy in/out/list/archive are confined to
  # this tree INSIDE containers. This is a CONTAINER path — it is NOT the
  # host-side staging directory (data/files on the host).
  jail_root: /tmp/dockerview-files
  # Per-transfer size caps (bytes). Default 8 MiB each.
  max_file_bytes: 8388608
  max_archive_bytes: 8388608
  # Default false: guests (no token) cannot download. Read SECURITY.md
  # before enabling on a multi-user intranet.
  allow_guest_download: false

# Duty assistant (Genkit + OpenAI-compatible model).
# When enabled, a "Duty" tab appears in the web UI for natural-language
# questions about containers, logs, and audit history.
# The API key is NEVER stored in yaml. Use OPENAI_API_KEY or
# DOCKERVIEW_AGENT_API_KEY, or point agent.api_key_file at a 0600 file.
# Without a key the agent runs in fake/drill mode (scripted responses, no network).
agent:
  enabled: false
  # provider: openai-compatible
  # base_url: https://api.openai.com/v1
  # model: gpt-4o-mini
  # api_key_file: /etc/dockerview/agent_key

# WeCom (企业微信) smart robot over the official long connection.
# When enabled, a "企微" tab appears in the web UI with the connection state,
# recent inbound messages and replies, and an admin-only inject box.
#
# The Secret is NEVER written here. Put it in the environment:
#   export WECOM_BOT_SECRET=...
# or in a 0600 file and point secret_file at it:
#   printf '%s' "$SECRET" > /etc/dockerview/wecom_secret && chmod 600 /etc/dockerview/wecom_secret
#
# Without WECOM_BOT_ID and a Secret the integration runs in mock mode: it
# builds the SDK client but never dials, and inbound frames are injected
# in memory. That is the default here, and it is what acceptance exercises.
wecom:
  enabled: false
  # bot_id: ww0123456789abcdef        # from the WeCom admin console
  # secret_file: /etc/dockerview/wecom_secret
  # ws_url: wss://openws.work.weixin.qq.com   # default; override for testing
  # reply_mode: stream                # stream (default) | markdown
  # welcome: ""                       # overrides the built-in enter_chat greeting
  #
  # Optional group-robot webhook. A different transport (plain HTTP JSON),
  # outbound only, never the primary path, off by default.
  group_webhook_enabled: false
  # group_webhook_url_file: /etc/dockerview/wecom_group_webhook
`

// WriteSample creates config.yaml in configDir from the commented sample,
// never overwriting an existing file.
func WriteSample(configDir string) error {
	if err := os.MkdirAll(configDir, 0o750); err != nil {
		return err
	}
	p := filepath.Join(configDir, ConfigFileName)
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(SampleYAML); err != nil {
		return fmt.Errorf("write sample: %w", err)
	}
	return nil
}

// validTopKeys / validSubKeys is the strict schema allowlist used both for
// sample generation and unknown-key rejection.
var validTopKeys = map[string]bool{
	"server": true, "port": true,
	"audit_enabled": true, "audit_retention_days": true,
	"backup_max": true, "data_root": true, "token_file": true,
	"files": true, "agent": true, "wecom": true,
	// flat agent_* form is still accepted (pre-group configs).
	"agent_enabled": true, "agent_provider": true, "agent_base_url": true,
	"agent_model": true, "agent_api_key_file": true,
}

var validSubKeys = map[string]map[string]bool{
	"files": {
		"jail_root": true, "max_file_bytes": true,
		"max_archive_bytes": true, "allow_guest_download": true,
	},
	"agent": {
		"enabled": true, "provider": true, "base_url": true,
		"model": true, "api_key_file": true,
	},
	// Note the absences: `secret` and `bot_secret` are deliberately not valid
	// keys. The long-connection Secret comes from WECOM_BOT_SECRET or from the
	// file named by secret_file, and validateYAML rejects a config that tries
	// to put the credential itself in yaml.
	"wecom": {
		"enabled": true, "bot_id": true, "secret_file": true,
		"ws_url": true, "reply_mode": true, "welcome": true,
		"group_webhook_enabled": true, "group_webhook_url_file": true,
	},
}

func validateYAML(m yamlMap) error {
	for k := range m.values {
		if !validTopKeys[k] {
			return fmt.Errorf("unknown config key %q (allowed: server, port, audit_enabled, audit_retention_days, backup_max, data_root, token_file, files, agent/agent_*)", k)
		}
	}
	for t, subs := range m.tables {
		if !validTopKeys[t] {
			return fmt.Errorf("unknown config section [%q]", t)
		}
		allow := validSubKeys[t]
		for k := range subs {
			if allow == nil || !allow[k] {
				return fmt.Errorf("unknown config key %q.%q", t, k)
			}
		}
	}
	return nil
}
