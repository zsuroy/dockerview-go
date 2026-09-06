// Package config centralizes dockerview startup configuration: XDG config
// resolution, the config.yaml loader, layered precedence
// (CLI > environment > yaml > built-in defaults), DataRoot derivation and
// migration of the legacy cwd-relative data/ directory.
//
// See docs/DATA_LAYOUT.md for the on-disk layout this package guarantees.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Names of environment variables understood by the resolver.
const (
	EnvConfigDir = "DOCKERVIEW_CONFIG_DIR"
	EnvDataDir   = "DOCKERVIEW_DATA_DIR"

	EnvToken          = "DOCKERVIEW_TOKEN"
	EnvPort           = "DOCKERVIEW_PORT"
	EnvServer         = "DOCKERVIEW_SERVER"
	EnvAuditDB        = "DOCKERVIEW_AUDIT_DB"
	EnvAuditRetention = "DOCKERVIEW_AUDIT_RETENTION_DAYS"
	EnvAuditDisable   = "DOCKERVIEW_AUDIT_DISABLE"
	EnvNoDocker       = "DOCKERVIEW_NO_DOCKER"
	EnvFixture        = "DOCKERVIEW_FIXTURE"
	EnvBackupDir      = "DOCKERVIEW_BACKUP_DIR"
	EnvBackupMax      = "DOCKERVIEW_BACKUP_MAX"

	EnvAgentEnabled    = "DOCKERVIEW_AGENT_ENABLED"
	EnvAgentProvider   = "DOCKERVIEW_AGENT_PROVIDER"
	EnvAgentBaseURL    = "DOCKERVIEW_AGENT_BASE_URL"
	EnvAgentModel      = "DOCKERVIEW_AGENT_MODEL"
	EnvAgentAPIKey     = "DOCKERVIEW_AGENT_API_KEY"
	EnvAgentAPIKeyFile = "DOCKERVIEW_AGENT_API_KEY_FILE"
)

// ConfigFileName is the single human-edited config file under ConfigRoot.
const ConfigFileName = "config.yaml"

// Defaults (also rendered into the generated sample).
const (
	DefaultPort            = 8080
	DefaultAuditRetention  = 90
	DefaultBackupMax       = 10
	DefaultMaxFileBytes    = int64(8 << 20) // 8 MiB
	DefaultMaxArchiveBytes = int64(8 << 20) // 8 MiB
	DefaultJailRoot        = "/tmp/dockerview-files"
	DefaultAuditDBName     = "audit.db"
)

// Layer records where an effective value came from.
type Layer string

const (
	LayerCLI     Layer = "cli"
	LayerEnv     Layer = "env"
	LayerYAML    Layer = "yaml"
	LayerDefault Layer = "default"
)

// AgentConfig groups the duty agent (Genkit/OpenAI-compatible) settings.
type AgentConfig struct {
	Enabled    bool
	Provider   string
	BaseURL    string
	Model      string
	APIKey     string
	APIKeyFile string
}

// FilesConfig groups the container file-transfer settings.
type FilesConfig struct {
	// JailRoot is the in-container whitelist root. in/out/list/archive are
	// only ever allowed inside this tree. It is a CONTAINER path and must
	// never be confused with the host-side DataRoot/files staging dir.
	JailRoot string
	// MaxFileBytes caps a single transferred file (copy in and copy out).
	MaxFileBytes int64
	// MaxArchiveBytes caps the total payload of a copy-out tar archive.
	MaxArchiveBytes int64
	// AllowGuestDownload lets unauthenticated guests download (default
	// false; see SECURITY.md for the risk if enabled).
	AllowGuestDownload bool
}

// Config is the fully resolved startup configuration.
type Config struct {
	// ConfigRoot and the yaml file inside it.
	ConfigDir     string
	ConfigFile    string
	ConfigExisted bool
	SampleWritten bool

	// DataRoot and its guaranteed sub-directories (all host paths).
	DataRoot   string
	DBDir      string // $DataRoot/db
	BackupsDir string // $DataRoot/backups
	FilesDir   string // $DataRoot/files — host-side transfer staging

	Server    bool
	Port      int
	Token     string
	TokenFile string

	AuditEnabled       bool
	AuditDBPath        string // absolute; empty when audit is disabled
	AuditDBDisabled    bool   // explicit -audit-db "" / audit_enabled:false
	AuditRetentionDays int

	BackupDir string
	BackupMax int

	NoDocker    bool
	FixturePath string

	// Agent config for the duty assistant (OpenAI-compatible).
	Agent AgentConfig

	Files FilesConfig

	// Sources records the winning layer per setting (used by tests and the
	// startup banner).
	Sources map[string]Layer

	// Migrations performed from the legacy cwd-relative layout.
	Migrations []string
}

// CLI carries flag values plus which flags were explicitly passed. The
// booleans matter: flag defaults must NOT be mistaken for operator intent.
type CLI struct {
	ServerSet bool
	Server    bool

	PortSet bool
	Port    int

	TokenSet bool
	Token    string

	AuditDBSet bool
	AuditDB    string

	AuditRetentionSet bool
	AuditRetention    int

	AuditDisableSet bool
	AuditDisable    bool

	NoDockerSet bool
	NoDocker    bool

	FixtureSet bool
	Fixture    string

	BackupDirSet bool
	BackupDir    string

	BackupMaxSet bool
	BackupMax    int
}

// Resolve loads configuration using os.Getenv.
func Resolve(cli CLI) (*Config, error) {
	return ResolveWithEnv(cli, os.Getenv)
}

// ResolveWithEnv is Resolve with an injectable environment lookup (tests).
func ResolveWithEnv(cli CLI, getenv func(string) string) (*Config, error) {
	return resolve(cli, getenv, false)
}

// ResolveForInit is used by `-config-init`: it writes the sample and lays
// out ConfigRoot but must NOT touch data (no migration, no DataRoot
// creation) — generating a sample must have no side effects on a
// production data tree.
func ResolveForInit(getenv func(string) string) (*Config, error) {
	return resolve(CLI{}, getenv, true)
}

func resolve(cli CLI, getenv func(string) string, initOnly bool) (*Config, error) {
	cfg := &Config{Sources: map[string]Layer{}}

	// --- 1. ConfigRoot -----------------------------------------------------
	configDir, src := resolveConfigDir(getenv)
	cfg.ConfigDir = configDir
	cfg.ConfigFile = filepath.Join(configDir, ConfigFileName)
	cfg.Sources["config_dir"] = src
	if err := os.MkdirAll(configDir, 0o750); err != nil {
		return nil, fmt.Errorf("config: mkdir %s: %w", configDir, err)
	}

	// --- 2. yaml (created from the commented sample on first run) ----------
	var ym yamlMap
	if b, err := os.ReadFile(cfg.ConfigFile); err == nil {
		cfg.ConfigExisted = true
		parsed, perr := parseYAML(strings.NewReader(string(b)))
		if perr != nil {
			return nil, fmt.Errorf("config: %w", perr)
		}
		if verr := validateYAML(parsed); verr != nil {
			return nil, fmt.Errorf("config: %w", verr)
		}
		ym = parsed
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("config: read %s: %w", cfg.ConfigFile, err)
	} else {
		if werr := WriteSample(configDir); werr != nil {
			return nil, fmt.Errorf("config: write sample %s: %w", cfg.ConfigFile, werr)
		}
		cfg.SampleWritten = true
	}

	// --- 3. DataRoot: env > yaml > $ConfigDir/data --------------------------
	dataRoot, dSrc := resolveDataRoot(configDir, ym, getenv)
	if !filepath.IsAbs(dataRoot) {
		return nil, fmt.Errorf("config: data_root must be an absolute path, got %q", dataRoot)
	}
	cfg.DataRoot = filepath.Clean(dataRoot)
	cfg.Sources["data_root"] = dSrc
	cfg.DBDir = filepath.Join(cfg.DataRoot, "db")
	cfg.BackupsDir = filepath.Join(cfg.DataRoot, "backups")
	cfg.FilesDir = filepath.Join(cfg.DataRoot, "files")
	if !initOnly {
		for _, d := range []string{cfg.DBDir, cfg.BackupsDir, cfg.FilesDir} {
			if err := os.MkdirAll(d, 0o750); err != nil {
				return nil, fmt.Errorf("config: mkdir %s: %w", d, err)
			}
		}

		// --- 4. Migrate legacy cwd-relative data/ BEFORE anything opens it --
		migrations, merr := MigrateLegacy(cwdOr("."), cfg)
		if merr != nil {
			return nil, merr
		}
		cfg.Migrations = migrations
	}

	// --- 5. Layered scalar resolution ---------------------------------------
	if err := resolveScalars(cfg, cli, getenv, ym); err != nil {
		return nil, err
	}
	if !initOnly {
		if err := cfg.Validate(); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

// resolveConfigDir implements DOCKERVIEW_CONFIG_DIR >
// $XDG_CONFIG_HOME/dockerview > ~/.config/dockerview (Unix) or
// %APPDATA%\dockerview (Windows).
func resolveConfigDir(getenv func(string) string) (string, Layer) {
	if v := strings.TrimSpace(getenv(EnvConfigDir)); v != "" {
		return filepath.Clean(v), LayerEnv
	}
	if v := strings.TrimSpace(getenv("XDG_CONFIG_HOME")); v != "" {
		return filepath.Join(v, "dockerview"), LayerEnv
	}
	if runtime.GOOS == "windows" {
		if app := strings.TrimSpace(getenv("APPDATA")); app != "" {
			return filepath.Join(app, "dockerview"), LayerDefault
		}
	}
	if home := strings.TrimSpace(getenv("HOME")); home != "" {
		return filepath.Join(home, ".config", "dockerview"), LayerDefault
	}
	// Last-resort fallback (no HOME at all): cwd .config — never the repo
	// data/ dir itself.
	return filepath.Join(".", ".config", "dockerview"), LayerDefault
}

func resolveDataRoot(configDir string, ym yamlMap, getenv func(string) string) (string, Layer) {
	if v := strings.TrimSpace(getenv(EnvDataDir)); v != "" {
		return filepath.Clean(v), LayerEnv
	}
	if v, ok := ym.get("data_root"); ok && strings.TrimSpace(v) != "" {
		return filepath.Clean(strings.TrimSpace(v)), LayerYAML
	}
	return filepath.Join(configDir, "data"), LayerDefault
}

func resolveScalars(cfg *Config, cli CLI, getenv func(string) string, ym yamlMap) error {
	// server
	cfg.Server, cfg.Sources["server"] = layeredBool(ym, "server", getenv(EnvServer), cli.ServerSet, cli.Server)

	// port
	if v, src, ok := layeredInt(ym, "port", getenv(EnvPort), cli.PortSet, cli.Port, DefaultPort); ok {
		cfg.Port = v
		cfg.Sources["port"] = src
	} else {
		return fmt.Errorf("config: port must be an integer 1-65535")
	}

	// token: CLI > env > token_file (yaml never stores the secret itself)
	switch {
	case cli.TokenSet && cli.Token != "":
		cfg.Token = cli.Token
		cfg.Sources["token"] = LayerCLI
	case getenv(EnvToken) != "":
		cfg.Token = getenv(EnvToken)
		cfg.Sources["token"] = LayerEnv
	default:
		if tf, ok := ym.get("token_file"); ok && strings.TrimSpace(tf) != "" {
			tokenPath := strings.TrimSpace(tf)
			// Relative token_file paths resolve against ConfigDir so a
			// config directory stays relocatable as a unit.
			if !filepath.IsAbs(tokenPath) {
				tokenPath = filepath.Join(cfg.ConfigDir, tokenPath)
			}
			b, err := os.ReadFile(tokenPath)
			if err != nil {
				return fmt.Errorf("config: token_file %q unreadable: %w", tokenPath, err)
			}
			tok := strings.TrimSpace(strings.TrimRight(string(b), "\r\n"))
			if tok == "" {
				return fmt.Errorf("config: token_file %q is empty", tf)
			}
			cfg.Token = tok
			cfg.TokenFile = strings.TrimSpace(tf)
			cfg.Sources["token"] = LayerYAML
		} else {
			cfg.Sources["token"] = LayerDefault
		}
	}

	// audit enabled? precedence: CLI (-audit-disable, or the legacy
	// `-audit-db ""` switch) > env DOCKERVIEW_AUDIT_DISABLE > yaml
	// audit_enabled > default (enabled).
	cfg.AuditEnabled = true
	switch {
	case cli.AuditDisableSet:
		cfg.AuditEnabled = !cli.AuditDisable
		cfg.Sources["audit_enabled"] = LayerCLI
	case cli.AuditDBSet && cli.AuditDB == "":
		// Back-compat: an explicitly empty -audit-db disabled audit.
		cfg.AuditEnabled = false
		cfg.AuditDBDisabled = true
		cfg.Sources["audit_enabled"] = LayerCLI
	case getenv(EnvAuditDisable) != "":
		cfg.AuditEnabled = !parseEnvBool(getenv(EnvAuditDisable))
		cfg.Sources["audit_enabled"] = LayerEnv
	default:
		if v, ok := ym.get("audit_enabled"); ok {
			b, err := parseBool(v)
			if err != nil {
				return fmt.Errorf("config: audit_enabled: %w", err)
			}
			cfg.AuditEnabled = b
			cfg.Sources["audit_enabled"] = LayerYAML
		} else {
			cfg.Sources["audit_enabled"] = LayerDefault
		}
	}
	// DB path: CLI -audit-db > env DOCKERVIEW_AUDIT_DB > $DataRoot/db/audit.db.
	if cfg.AuditEnabled {
		switch {
		case cli.AuditDBSet && cli.AuditDB != "":
			cfg.AuditDBPath = absPath(cli.AuditDB)
			cfg.Sources["audit_db"] = LayerCLI
		case getenv(EnvAuditDB) != "":
			cfg.AuditDBPath = absPath(getenv(EnvAuditDB))
			cfg.Sources["audit_db"] = LayerEnv
		default:
			cfg.AuditDBPath = filepath.Join(cfg.DBDir, DefaultAuditDBName)
			cfg.Sources["audit_db"] = LayerDefault
		}
	}

	// audit retention
	if v, src, ok := layeredInt(ym, "audit_retention_days", getenv(EnvAuditRetention), cli.AuditRetentionSet, cli.AuditRetention, DefaultAuditRetention); ok {
		cfg.AuditRetentionDays = v
		cfg.Sources["audit_retention_days"] = src
	} else {
		return fmt.Errorf("config: audit_retention_days must be a non-negative integer")
	}

	// backup dir: CLI/env override; default lives under DataRoot.
	switch {
	case cli.BackupDirSet && cli.BackupDir != "":
		cfg.BackupDir = absPath(cli.BackupDir)
		cfg.Sources["backup_dir"] = LayerCLI
	case getenv(EnvBackupDir) != "":
		cfg.BackupDir = absPath(getenv(EnvBackupDir))
		cfg.Sources["backup_dir"] = LayerEnv
	default:
		cfg.BackupDir = cfg.BackupsDir
		cfg.Sources["backup_dir"] = LayerDefault
	}

	if v, src, ok := layeredInt(ym, "backup_max", getenv(EnvBackupMax), cli.BackupMaxSet, cli.BackupMax, DefaultBackupMax); ok {
		cfg.BackupMax = v
		cfg.Sources["backup_max"] = src
	} else {
		return fmt.Errorf("config: backup_max must be a positive integer")
	}

	// no-docker / fixture
	cfg.NoDocker, cfg.Sources["no_docker"] = layeredBool(ym, "", getenv(EnvNoDocker), cli.NoDockerSet, cli.NoDocker)
	if cli.FixtureSet {
		cfg.FixturePath = cli.Fixture
		cfg.Sources["fixture"] = LayerCLI
	} else if v := getenv(EnvFixture); v != "" {
		cfg.FixturePath = v
		cfg.Sources["fixture"] = LayerEnv
	}

	// --- files table --------------------------------------------------------
	fc := FilesConfig{
		JailRoot:        DefaultJailRoot,
		MaxFileBytes:    DefaultMaxFileBytes,
		MaxArchiveBytes: DefaultMaxArchiveBytes,
	}
	if v, ok := ym.getTable("files", "jail_root"); ok && strings.TrimSpace(v) != "" {
		fc.JailRoot = strings.TrimSpace(v)
		cfg.Sources["files_jail_root"] = LayerYAML
	} else {
		cfg.Sources["files_jail_root"] = LayerDefault
	}
	if v, ok := ym.getTable("files", "max_file_bytes"); ok {
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil || n <= 0 {
			return fmt.Errorf("config: files.max_file_bytes must be a positive integer")
		}
		fc.MaxFileBytes = n
		cfg.Sources["files_max_file_bytes"] = LayerYAML
	} else {
		cfg.Sources["files_max_file_bytes"] = LayerDefault
	}
	if v, ok := ym.getTable("files", "max_archive_bytes"); ok {
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil || n <= 0 {
			return fmt.Errorf("config: files.max_archive_bytes must be a positive integer")
		}
		fc.MaxArchiveBytes = n
		cfg.Sources["files_max_archive_bytes"] = LayerYAML
	} else {
		cfg.Sources["files_max_archive_bytes"] = LayerDefault
	}
	if v, ok := ym.getTable("files", "allow_guest_download"); ok {
		b, err := parseBool(v)
		if err != nil {
			return fmt.Errorf("config: files.allow_guest_download: %w", err)
		}
		fc.AllowGuestDownload = b
		cfg.Sources["files_allow_guest_download"] = LayerYAML
	} else {
		cfg.Sources["files_allow_guest_download"] = LayerDefault
	}
	cfg.Files = fc

	// --- agent (duty assistant) --------------------------------------------
	// The `agent:` table mirrors the other groups (files:). env always wins;
	// the old flat agent_* keys are still honored for configs written before
	// the group form existed. API keys are NEVER read from yaml.
	ac := AgentConfig{
		Provider: "openai-compatible",
		BaseURL:  "https://api.openai.com/v1",
		Model:    "gpt-4o-mini",
	}
	if v := strings.TrimSpace(getenv(EnvAgentEnabled)); v != "" {
		ac.Enabled = parseEnvBool(v)
		cfg.Sources["agent_enabled"] = LayerEnv
	} else if v, ok := ym.getTable("agent", "enabled"); ok && strings.TrimSpace(v) != "" {
		b, err := parseBool(strings.TrimSpace(v))
		if err != nil {
			return fmt.Errorf("config: agent.enabled must be true or false, got %q", v)
		}
		ac.Enabled = b
		cfg.Sources["agent_enabled"] = LayerYAML
	} else if v, ok := ym.get("agent_enabled"); ok && strings.TrimSpace(v) != "" {
		b, err := parseBool(strings.TrimSpace(v))
		if err != nil {
			return fmt.Errorf("config: agent_enabled must be true or false, got %q", v)
		}
		ac.Enabled = b
		cfg.Sources["agent_enabled"] = LayerYAML
	} else {
		cfg.Sources["agent_enabled"] = LayerDefault
	}
	agentScalar := func(envKey, tableKey, flatKey, srcKey string, dst *string) {
		if v := strings.TrimSpace(getenv(envKey)); v != "" {
			*dst = v
			cfg.Sources[srcKey] = LayerEnv
		} else if v, ok := ym.getTable("agent", tableKey); ok && strings.TrimSpace(v) != "" {
			*dst = strings.TrimSpace(v)
			cfg.Sources[srcKey] = LayerYAML
		} else if v, ok := ym.get(flatKey); ok && strings.TrimSpace(v) != "" {
			*dst = strings.TrimSpace(v)
			cfg.Sources[srcKey] = LayerYAML
		} else {
			cfg.Sources[srcKey] = LayerDefault
		}
	}
	agentScalar(EnvAgentProvider, "provider", "agent_provider", "agent_provider", &ac.Provider)
	agentScalar(EnvAgentBaseURL, "base_url", "agent_base_url", "agent_base_url", &ac.BaseURL)
	agentScalar(EnvAgentModel, "model", "agent_model", "agent_model", &ac.Model)
	// API key: env only (never from yaml). OPENAI_API_KEY is also checked by duty.Config.ResolveKey.
	ac.APIKey = strings.TrimSpace(getenv(EnvAgentAPIKey))
	if ac.APIKey != "" {
		cfg.Sources["agent_api_key"] = LayerEnv
	}
	agentScalar(EnvAgentAPIKeyFile, "api_key_file", "agent_api_key_file", "agent_api_key_file", &ac.APIKeyFile)
	if ac.APIKeyFile != "" {
		ac.APIKeyFile = absPath(ac.APIKeyFile)
	}
	cfg.Agent = ac
	return nil
}

func absPath(p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

func cwdOr(fallback string) string {
	if d, err := os.Getwd(); err == nil {
		return d
	}
	return fallback
}

// Validate checks cross-field invariants after resolution.
func (c *Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("config: port %d out of range (1-65535)", c.Port)
	}
	if c.AuditRetentionDays < 0 {
		return fmt.Errorf("config: audit_retention_days must be >= 0")
	}
	if c.BackupMax <= 0 {
		return fmt.Errorf("config: backup_max must be > 0")
	}
	if c.Files.MaxFileBytes <= 0 || c.Files.MaxArchiveBytes <= 0 {
		return fmt.Errorf("config: file size limits must be > 0")
	}
	if !strings.HasPrefix(c.Files.JailRoot, "/") {
		return fmt.Errorf("config: files.jail_root must be an absolute POSIX path, got %q", c.Files.JailRoot)
	}
	if filepath.Clean(c.FilesDir) == filepath.Clean(c.Files.JailRoot) {
		return fmt.Errorf("config: host staging dir (%s) must differ from the in-container jail root (%s)",
			c.FilesDir, c.Files.JailRoot)
	}
	return nil
}

// ---- layered scalar helpers ----------------------------------------------

func layeredInt(ym yamlMap, key, envVal string, cliSet bool, cliVal, def int) (int, Layer, bool) {
	if cliSet {
		if cliVal <= 0 && key != "port" && key != "audit_retention_days" {
			return 0, "", false
		}
		return cliVal, LayerCLI, true
	}
	if envVal != "" {
		n, err := strconv.Atoi(strings.TrimSpace(envVal))
		if err != nil || n < 0 {
			return 0, "", false
		}
		return n, LayerEnv, true
	}
	if v, ok := ym.get(key); ok {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || n < 0 {
			return 0, "", false
		}
		return n, LayerYAML, true
	}
	return def, LayerDefault, true
}

func layeredBool(ym yamlMap, key, envVal string, cliSet bool, cliVal bool) (bool, Layer) {
	if cliSet {
		return cliVal, LayerCLI
	}
	if envVal != "" {
		return parseEnvBool(envVal), LayerEnv
	}
	if key != "" {
		if v, ok := ym.get(key); ok {
			if b, err := parseBool(v); err == nil {
				return b, LayerYAML
			}
		}
	}
	return false, LayerDefault
}

func parseEnvBool(v string) bool {
	b, _ := parseBool(v)
	return b
}

func parseBool(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off", "":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean %q", v)
	}
}
