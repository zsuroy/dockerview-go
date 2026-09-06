package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestConfigDirPrecedence(t *testing.T) {
	// DOCKERVIEW_CONFIG_DIR beats XDG_CONFIG_HOME.
	cfg, err := ResolveWithEnv(CLI{}, envMap(map[string]string{
		EnvConfigDir:      "/tmp/cfg-explicit",
		"XDG_CONFIG_HOME": "/tmp/xdg",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConfigDir != "/tmp/cfg-explicit" {
		t.Fatalf("ConfigDir = %q, want /tmp/cfg-explicit", cfg.ConfigDir)
	}
	if cfg.Sources["config_dir"] != LayerEnv {
		t.Fatalf("config_dir source = %q", cfg.Sources["config_dir"])
	}

	// XDG fallback.
	cfg2, err := ResolveWithEnv(CLI{}, envMap(map[string]string{
		"XDG_CONFIG_HOME": filepath.Join(t.TempDir(), "xdg"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.Base(cfg2.ConfigDir); got != "dockerview" {
		t.Fatalf("ConfigDir = %q", cfg2.ConfigDir)
	}

	// HOME fallback (~/.config/dockerview).
	home := t.TempDir()
	cfg3, err := ResolveWithEnv(CLI{}, envMap(map[string]string{"HOME": home}))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".config", "dockerview")
	if cfg3.ConfigDir != want {
		t.Fatalf("ConfigDir = %q, want %q", cfg3.ConfigDir, want)
	}
}

func TestDefaultDataRootAndSubdirs(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := ResolveWithEnv(CLI{}, envMap(map[string]string{EnvConfigDir: filepath.Join(tmp, "dv")}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataRoot != filepath.Join(cfg.ConfigDir, "data") {
		t.Fatalf("DataRoot = %q, want under ConfigDir", cfg.DataRoot)
	}
	for _, d := range []string{cfg.DBDir, cfg.BackupsDir, cfg.FilesDir} {
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			t.Fatalf("expected dir %s to exist: %v", d, err)
		}
		if !strings.HasPrefix(d, cfg.DataRoot+string(os.PathSeparator)) {
			t.Fatalf("%s not under DataRoot %s", d, cfg.DataRoot)
		}
	}
	// sqlite default lives in data/db, never next to config.yaml.
	if filepath.Dir(cfg.AuditDBPath) != cfg.DBDir {
		t.Fatalf("audit db %q not in db dir %q", cfg.AuditDBPath, cfg.DBDir)
	}
	if filepath.Dir(cfg.AuditDBPath) == cfg.ConfigDir {
		t.Fatalf("audit db must not sit next to config.yaml")
	}
}

func TestDataRootOverrides(t *testing.T) {
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, "cfg")
	alt := filepath.Join(tmp, "elsewhere")

	// DOCKERVIEW_DATA_DIR beats yaml data_root.
	writeYAML(t, cfgDir, "data_root: "+filepath.Join(tmp, "from-yaml")+"\nport: 9090\n")
	cfg, err := ResolveWithEnv(CLI{}, envMap(map[string]string{
		EnvConfigDir: cfgDir,
		EnvDataDir:   alt,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataRoot != alt {
		t.Fatalf("DataRoot = %q, want %q", cfg.DataRoot, alt)
	}
	if cfg.Sources["data_root"] != LayerEnv {
		t.Fatalf("data_root source = %q", cfg.Sources["data_root"])
	}

	// yaml data_root alone wins over default.
	cfg2, err := ResolveWithEnv(CLI{}, envMap(map[string]string{EnvConfigDir: cfgDir}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.DataRoot != filepath.Join(tmp, "from-yaml") {
		t.Fatalf("yaml data_root not honored: %q", cfg2.DataRoot)
	}
}

func TestRelativeDataRootRejected(t *testing.T) {
	cfgDir := filepath.Join(t.TempDir(), "cfg")
	writeYAML(t, cfgDir, "data_root: relative/path\n")
	if _, err := ResolveWithEnv(CLI{}, envMap(map[string]string{EnvConfigDir: cfgDir})); err == nil {
		t.Fatal("expected error for relative data_root")
	}
}

func TestYAMLDrivesServerAndPort(t *testing.T) {
	cfgDir := filepath.Join(t.TempDir(), "cfg")
	writeYAML(t, cfgDir, "server: true\nport: 9090\n")
	cfg, err := ResolveWithEnv(CLI{}, envMap(map[string]string{EnvConfigDir: cfgDir}))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Server || cfg.Port != 9090 {
		t.Fatalf("server=%v port=%d", cfg.Server, cfg.Port)
	}
	if cfg.Sources["server"] != LayerYAML || cfg.Sources["port"] != LayerYAML {
		t.Fatalf("sources: %v", cfg.Sources)
	}
}

func TestCLIPortBeatsYAML(t *testing.T) {
	// The task's canonical example: yaml says 9090, -port 8080 -> 8080.
	cfgDir := filepath.Join(t.TempDir(), "cfg")
	writeYAML(t, cfgDir, "port: 9090\n")
	cfg, err := ResolveWithEnv(CLI{PortSet: true, Port: 8080},
		envMap(map[string]string{EnvConfigDir: cfgDir, EnvPort: "8081"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 8080 {
		t.Fatalf("port = %d, want 8080 (CLI beats env and yaml)", cfg.Port)
	}
	if cfg.Sources["port"] != LayerCLI {
		t.Fatalf("port source = %q", cfg.Sources["port"])
	}

	// env beats yaml when CLI absent.
	cfg2, err := ResolveWithEnv(CLI{},
		envMap(map[string]string{EnvConfigDir: cfgDir, EnvPort: "8081"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Port != 8081 || cfg2.Sources["port"] != LayerEnv {
		t.Fatalf("port=%d src=%s, want 8081/env", cfg2.Port, cfg2.Sources["port"])
	}
}

func TestEnvBeatsYAMLAndDefault(t *testing.T) {
	cfgDir := filepath.Join(t.TempDir(), "cfg")
	writeYAML(t, cfgDir, "backup_max: 3\n")
	cfg, err := ResolveWithEnv(CLI{},
		envMap(map[string]string{EnvConfigDir: cfgDir, EnvBackupMax: "7"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BackupMax != 7 || cfg.Sources["backup_max"] != LayerEnv {
		t.Fatalf("backup_max=%d src=%s", cfg.BackupMax, cfg.Sources["backup_max"])
	}
}

func TestEnvLayerDrivesServerAndPort(t *testing.T) {
	cfgDir := filepath.Join(t.TempDir(), "cfg")
	writeYAML(t, cfgDir, "server: false\nport: 9090\n")
	cfg, err := ResolveWithEnv(CLI{}, envMap(map[string]string{
		EnvConfigDir:        cfgDir,
		"DOCKERVIEW_SERVER": "true",
		"DOCKERVIEW_PORT":   "9091",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Server {
		t.Fatal("env DOCKERVIEW_SERVER did not enable web")
	}
	if cfg.Port != 9091 || cfg.Sources["port"] != LayerEnv {
		t.Fatalf("port=%d src=%s", cfg.Port, cfg.Sources["port"])
	}
}

func TestSampleGeneratedAndNotOverwritten(t *testing.T) {
	cfgDir := filepath.Join(t.TempDir(), "cfg")
	cfg, err := ResolveWithEnv(CLI{}, envMap(map[string]string{EnvConfigDir: cfgDir}))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SampleWritten || cfg.ConfigExisted {
		t.Fatalf("first run must write the sample: written=%v existed=%v",
			cfg.SampleWritten, cfg.ConfigExisted)
	}
	b, err := os.ReadFile(cfg.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "jail_root") {
		t.Fatal("sample missing files.jail_root")
	}
	// Secret hygiene: every line mentioning a token must be a comment.
	for n, line := range strings.Split(string(b), "\n") {
		trim := strings.TrimSpace(line)
		if strings.Contains(trim, "token") && !strings.HasPrefix(trim, "#") {
			t.Fatalf("sample line %d leaks a token setting: %q", n+1, line)
		}
	}

	// Customized yaml must survive a second resolve (no overwrite).
	writeYAML(t, cfgDir, "server: true\nport: 9091\n")
	cfg2, err := ResolveWithEnv(CLI{}, envMap(map[string]string{EnvConfigDir: cfgDir}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Port != 9091 || cfg2.SampleWritten {
		t.Fatalf("sample regeneration clobbered config: port=%d written=%v", cfg2.Port, cfg2.SampleWritten)
	}
}

func TestTokenFileResolution(t *testing.T) {
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, "cfg")
	tokenFile := filepath.Join(tmp, "token.txt")
	if err := os.WriteFile(tokenFile, []byte("file-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeYAML(t, cfgDir, "token_file: "+tokenFile+"\n")
	cfg, err := ResolveWithEnv(CLI{}, envMap(map[string]string{EnvConfigDir: cfgDir}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "file-token" {
		t.Fatalf("token = %q", cfg.Token)
	}

	// CLI token beats token_file.
	cfg2, err := ResolveWithEnv(CLI{TokenSet: true, Token: "cli-token"},
		envMap(map[string]string{EnvConfigDir: cfgDir}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Token != "cli-token" {
		t.Fatalf("token = %q", cfg2.Token)
	}

	// env token beats token_file.
	cfg3, err := ResolveWithEnv(CLI{},
		envMap(map[string]string{EnvConfigDir: cfgDir, EnvToken: "env-token"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg3.Token != "env-token" {
		t.Fatalf("token = %q", cfg3.Token)
	}

	// Relative token_file resolves under ConfigDir (relocatable config tree).
	cfgDir3 := filepath.Join(tmp, "cfg3")
	writeYAML(t, cfgDir3, "token_file: secret.txt\n")
	if err := os.WriteFile(filepath.Join(cfgDir3, "secret.txt"), []byte("rel-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgRel, err := ResolveWithEnv(CLI{}, envMap(map[string]string{EnvConfigDir: cfgDir3}))
	if err != nil {
		t.Fatal(err)
	}
	if cfgRel.Token != "rel-token" {
		t.Fatalf("relative token_file: token=%q", cfgRel.Token)
	}

	// Unreadable token_file is a hard error.
	cfgDir2 := filepath.Join(tmp, "cfg2")
	writeYAML(t, cfgDir2, "token_file: "+filepath.Join(tmp, "missing")+"\n")
	if _, err := ResolveWithEnv(CLI{}, envMap(map[string]string{EnvConfigDir: cfgDir2})); err == nil {
		t.Fatal("expected error for missing token_file")
	}
}

func TestUnknownYAMLKeyRejected(t *testing.T) {
	cfgDir := filepath.Join(t.TempDir(), "cfg")
	writeYAML(t, cfgDir, "prt: 9000\n")
	if _, err := ResolveWithEnv(CLI{}, envMap(map[string]string{EnvConfigDir: cfgDir})); err == nil ||
		!strings.Contains(err.Error(), "unknown") {
		t.Fatalf("expected unknown-key error, got %v", err)
	}
	cfgDir2 := filepath.Join(t.TempDir(), "cfg2")
	writeYAML(t, cfgDir2, "files:\n  max_bytes: 1\n")
	if _, err := ResolveWithEnv(CLI{}, envMap(map[string]string{EnvConfigDir: cfgDir2})); err == nil {
		t.Fatal("expected unknown sub-key error")
	}
}

func TestFilesDefaultsFromYAML(t *testing.T) {
	cfgDir := filepath.Join(t.TempDir(), "cfg")
	writeYAML(t, cfgDir, "files:\n  jail_root: /srv/dv\n  max_file_bytes: 1024\n  max_archive_bytes: 2048\n  allow_guest_download: true\n")
	cfg, err := ResolveWithEnv(CLI{}, envMap(map[string]string{EnvConfigDir: cfgDir}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Files.JailRoot != "/srv/dv" || cfg.Files.MaxFileBytes != 1024 ||
		cfg.Files.MaxArchiveBytes != 2048 || !cfg.Files.AllowGuestDownload {
		t.Fatalf("files cfg = %+v", cfg.Files)
	}
	// defaults when absent
	cfg2, err := ResolveWithEnv(CLI{}, envMap(map[string]string{EnvConfigDir: t.TempDir()}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Files.JailRoot != DefaultJailRoot || cfg2.Files.MaxFileBytes != DefaultMaxFileBytes {
		t.Fatalf("defaults wrong: %+v", cfg2.Files)
	}
}

func TestAgentGroupFromYAML(t *testing.T) {
	cfgDir := filepath.Join(t.TempDir(), "cfg")
	writeYAML(t, cfgDir, "agent:\n  enabled: true\n  base_url: http://localhost:11434/v1\n  model: llama3.2\n")
	cfg, err := ResolveWithEnv(CLI{}, envMap(map[string]string{EnvConfigDir: cfgDir}))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Agent.Enabled {
		t.Fatalf("agent.enabled should be true: %+v", cfg.Agent)
	}
	if cfg.Agent.BaseURL != "http://localhost:11434/v1" || cfg.Agent.Model != "llama3.2" ||
		cfg.Agent.Provider != "openai-compatible" {
		t.Fatalf("agent cfg = %+v", cfg.Agent)
	}
	if cfg.Sources["agent_enabled"] != LayerYAML || cfg.Sources["agent_model"] != LayerYAML {
		t.Fatalf("sources wrong: %+v", cfg.Sources)
	}
	// Defaults when absent.
	cfg2, err := ResolveWithEnv(CLI{}, envMap(map[string]string{EnvConfigDir: t.TempDir()}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Agent.Enabled || cfg2.Agent.Model != "gpt-4o-mini" || cfg2.Agent.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("agent defaults wrong: %+v", cfg2.Agent)
	}
}

func TestAgentFlatFormStillAccepted(t *testing.T) {
	// Configs written before the agent: group existed must keep working.
	cfgDir := filepath.Join(t.TempDir(), "cfg")
	writeYAML(t, cfgDir, "agent_enabled: true\nagent_model: gpt-4o-mini\n")
	cfg, err := ResolveWithEnv(CLI{}, envMap(map[string]string{EnvConfigDir: cfgDir}))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Agent.Enabled || cfg.Agent.Model != "gpt-4o-mini" {
		t.Fatalf("flat form not honored: %+v", cfg.Agent)
	}
	if cfg.Sources["agent_enabled"] != LayerYAML {
		t.Fatalf("agent_enabled source = %q", cfg.Sources["agent_enabled"])
	}
}

func TestAgentEnvBeatsYAMLGroup(t *testing.T) {
	cfgDir := filepath.Join(t.TempDir(), "cfg")
	writeYAML(t, cfgDir, "agent:\n  enabled: false\n  model: yaml-model\n")
	cfg, err := ResolveWithEnv(CLI{}, envMap(map[string]string{
		EnvConfigDir:    cfgDir,
		EnvAgentEnabled: "true",
		EnvAgentModel:   "env-model",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Agent.Enabled || cfg.Agent.Model != "env-model" {
		t.Fatalf("env should win: %+v", cfg.Agent)
	}
	if cfg.Sources["agent_enabled"] != LayerEnv || cfg.Sources["agent_model"] != LayerEnv {
		t.Fatalf("sources wrong: %+v", cfg.Sources)
	}
}

func TestAgentKeyFileFromGroupIsAbs(t *testing.T) {
	cfgDir := filepath.Join(t.TempDir(), "cfg")
	writeYAML(t, cfgDir, "agent:\n  enabled: true\n  api_key_file: relative-key\n")
	cfg, err := ResolveWithEnv(CLI{}, envMap(map[string]string{EnvConfigDir: cfgDir}))
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(cfg.Agent.APIKeyFile) {
		t.Fatalf("api_key_file should be absolute, got %q", cfg.Agent.APIKeyFile)
	}
}

func TestAgentUnknownSubKeyRejected(t *testing.T) {
	cfgDir := filepath.Join(t.TempDir(), "cfg")
	writeYAML(t, cfgDir, "agent:\n  api_key: sekrit\n")
	if _, err := ResolveWithEnv(CLI{}, envMap(map[string]string{EnvConfigDir: cfgDir})); err == nil ||
		!strings.Contains(err.Error(), "unknown") {
		t.Fatalf("expected unknown sub-key error, got %v", err)
	}
}

func TestAuditDisablePaths(t *testing.T) {
	cfgDir := filepath.Join(t.TempDir(), "cfg")
	writeYAML(t, cfgDir, "audit_enabled: false\n")
	cfg, err := ResolveWithEnv(CLI{}, envMap(map[string]string{EnvConfigDir: cfgDir}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuditEnabled || cfg.AuditDBPath != "" {
		t.Fatalf("audit should be disabled: %+v", cfg)
	}
	// Legacy `-audit-db ""` disables.
	cfg2, err := ResolveWithEnv(CLI{AuditDBSet: true, AuditDB: ""},
		envMap(map[string]string{EnvConfigDir: t.TempDir()}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.AuditEnabled {
		t.Fatal("empty -audit-db should disable audit")
	}
}

func TestLegacyMigration(t *testing.T) {
	// Simulate the old layout: cwd/data/dockerview.db (+wal) and backups.
	cwd := t.TempDir()
	old := filepath.Join(cwd, "data")
	if err := os.MkdirAll(filepath.Join(old, "backups"), 0o750); err != nil {
		t.Fatal(err)
	}
	dbBytes := []byte("pretend-sqlite")
	if err := os.WriteFile(filepath.Join(old, "dockerview.db"), dbBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "dockerview.db-wal"), []byte("wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "backups", "dockerview-backup-old.zip"), []byte("zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustChdir(t, cwd)

	cfg, err := ResolveWithEnv(CLI{}, envMap(map[string]string{
		EnvConfigDir: filepath.Join(cwd, ".config", "dockerview"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(cfg.AuditDBPath); err != nil || string(got) != string(dbBytes) {
		t.Fatalf("db not migrated: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.DBDir, "audit.db-wal")); err != nil {
		t.Fatalf("wal sidecar not migrated: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.BackupsDir, "dockerview-backup-old.zip")); err != nil {
		t.Fatalf("backups not migrated: %v", err)
	}
	if _, err := os.Stat(filepath.Join(old, "dockerview.db")); !os.IsNotExist(err) {
		t.Fatalf("legacy db should be gone: %v", err)
	}
	if len(cfg.Migrations) == 0 {
		t.Fatal("migration notes empty")
	}
}

func TestMigrationConflictIsFatal(t *testing.T) {
	cwd := t.TempDir()
	old := filepath.Join(cwd, "data")
	if err := os.MkdirAll(filepath.Join(old, "backups"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "dockerview.db"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustChdir(t, cwd)
	cfgDir := filepath.Join(cwd, "cfg")
	// Pre-create the target db so both exist.
	if err := os.MkdirAll(filepath.Join(cfgDir, "data", "db"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "data", "db", "audit.db"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveWithEnv(CLI{}, envMap(map[string]string{EnvConfigDir: cfgDir})); err == nil {
		t.Fatal("expected fatal migration conflict error")
	}
}

func TestJailRootMustDifferFromHostStaging(t *testing.T) {
	cfgDir := filepath.Join(t.TempDir(), "cfg")
	jail := filepath.Join(cfgDir, "data", "files")
	writeYAML(t, cfgDir, "files:\n  jail_root: "+jail+"\n")
	if _, err := ResolveWithEnv(CLI{}, envMap(map[string]string{EnvConfigDir: cfgDir})); err == nil {
		t.Fatal("expected error: host staging and jail root must differ")
	}
}

func TestResolveForInitHasNoDataSideEffects(t *testing.T) {
	cfgDir := filepath.Join(t.TempDir(), "cfg")
	cfg, err := ResolveForInit(envMap(map[string]string{EnvConfigDir: cfgDir}))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SampleWritten {
		t.Fatal("init should write the sample")
	}
	if _, err := os.Stat(filepath.Join(cfgDir, "data")); !os.IsNotExist(err) {
		t.Fatalf("-config-init must not create DataRoot: %v", err)
	}
	if len(cfg.Migrations) != 0 {
		t.Fatalf("-config-init must not migrate: %v", cfg.Migrations)
	}
}

func writeYAML(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ConfigFileName), []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
}

func mustChdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}
