package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/zsuroy/dockerview-go/internal/audit"
	"github.com/zsuroy/dockerview-go/internal/backup"
	"github.com/zsuroy/dockerview-go/internal/config"
	"github.com/zsuroy/dockerview-go/internal/docker"
	"github.com/zsuroy/dockerview-go/internal/files"
	"github.com/zsuroy/dockerview-go/internal/server"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "dockerview: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	fset := flag.NewFlagSet("dockerview", flag.ContinueOnError)
	fset.SetOutput(os.Stderr)
	updateTolatest := fset.Bool("update", false, "update to the latest")
	showVersion := fset.Bool("version", false, "Show version")
	showHelp := fset.Bool("help", false, "Show help")
	configInit := fset.Bool("config-init", false, "Write a commented config.yaml sample into ConfigRoot and exit (never overwrites)")
	enableServer := fset.Bool("server", false, "Enable HTTP server (overrides config.yaml; yaml 'server: true' also enables it)")
	serverPort := fset.Int("port", 0, "HTTP port override (default from config.yaml or 8080)")
	serverToken := fset.String("token", "", "Admin token override (also: DOCKERVIEW_TOKEN, token_file)")
	auditDB := fset.String("audit-db", "", "Audit SQLite path override; empty value disables audit (default: $DataRoot/db/audit.db)")
	auditRetention := fset.Int("audit-retention-days", 0, "Audit retention days override (default 90)")
	auditDisable := fset.Bool("audit-disable", false, "Disable audit logging entirely")
	noDocker := fset.Bool("no-docker", false, "Skip Docker client connection (test/stub mode)")
	fixturePath := fset.String("fixture", "", "JSON fixture for -no-docker backup/files mock mode")
	backupDir := fset.String("backup-dir", "", "Backup snapshot dir override (default: $DataRoot/backups)")
	backupMax := fset.Int("backup-max", 0, "Backup retention count override (default 10)")
	if err := fset.Parse(os.Args[1:]); err != nil {
		return err
	}

	SetColor()

	if *updateTolatest {
		doUpdate()
		return nil
	}
	if *showVersion {
		fmt.Printf("DockerView-Go %s\n", Version)
		fmt.Printf("Commit: %s\n", Commit)
		fmt.Printf("Built: %s\n", Date)
		return nil
	}
	if *showHelp {
		printHelp()
		return nil
	}

	// Resolve ConfigRoot -> config.yaml -> DataRoot (CLI > env > yaml > default).
	cli := config.CLI{
		ServerSet: visited(fset, "server"), Server: *enableServer,
		PortSet: visited(fset, "port"), Port: *serverPort,
		TokenSet: visited(fset, "token"), Token: *serverToken,
		AuditDBSet: visited(fset, "audit-db"), AuditDB: *auditDB,
		AuditRetentionSet: visited(fset, "audit-retention-days"), AuditRetention: *auditRetention,
		AuditDisableSet: visited(fset, "audit-disable"), AuditDisable: *auditDisable,
		NoDockerSet: visited(fset, "no-docker"), NoDocker: *noDocker,
		FixtureSet: visited(fset, "fixture"), Fixture: *fixturePath,
		BackupDirSet: visited(fset, "backup-dir"), BackupDir: *backupDir,
		BackupMaxSet: visited(fset, "backup-max"), BackupMax: *backupMax,
	}
	var cfg *config.Config
	var err error
	if *configInit {
		cfg, err = config.ResolveForInit(os.Getenv)
	} else {
		cfg, err = config.Resolve(cli)
	}
	if err != nil {
		return err
	}

	if *configInit {
		if cfg.ConfigExisted || !cfg.SampleWritten {
			fmt.Printf("[INFO] config.yaml already exists at %s (not overwritten)\n", cfg.ConfigFile)
			return nil
		}
		fmt.Printf("[INFO] Wrote commented sample to %s\n", cfg.ConfigFile)
		fmt.Printf("[INFO] Edit it (set server: true when ready), then just run: dockerview\n")
		return nil
	}

	printConfigBanner(cfg)

	client, err := docker.NewClientMaybeSkipped(cfg.NoDocker)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	if client != nil {
		defer client.Close()
	} else {
		fmt.Printf("[INFO] Running without Docker client (-no-docker); container endpoints are unavailable, backup runs in fixture/stub mode\n")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var srv *server.Server
	if cfg.Server {
		token := cfg.Token
		if token == "" {
			bytes := make([]byte, 16)
			if _, err := rand.Read(bytes); err == nil {
				token = hex.EncodeToString(bytes)
			} else {
				token = "dockerview-secure-default"
			}
		}

		fmt.Printf("[INFO] Web dashboard is running at http://localhost:%d/?token=%s\n", cfg.Port, token)
		fmt.Printf("[INFO] Security token: %s\n", token)

		srv = server.NewServer(client, token, Version, Commit, Date)
		// Files feature: container jail root + host staging + limits.
		srv.SetFilesConfig(cfg.Files, cfg.FilesDir)
		var transferCopier files.Copier
		if client != nil {
			transferCopier = files.NewDockerCopier(client, cfg.Files.JailRoot)
		} else {
			mockRoot := filepath.Join(cfg.DataRoot, "mock-container-fs")
			mc, mcErr := files.NewMockCopier(mockRoot)
			if mcErr != nil {
				return fmt.Errorf("file transfer mock: %w", mcErr)
			}
			log.Printf("[INFO] File transfer backend: mock (root=%s, jail=%s)", mockRoot, cfg.Files.JailRoot)
			transferCopier = mc
		}
		srv.SetFileCopier(transferCopier)

		// Configure audit recorder.
		auditCfg := audit.DefaultConfig()
		auditCfg.DBPath = cfg.AuditDBPath
		auditCfg.RetentionDays = cfg.AuditRetentionDays
		var auditer audit.Recorder
		if !cfg.AuditEnabled || cfg.AuditDBPath == "" {
			auditer = audit.NewNoop(auditCfg)
			log.Printf("[WARN] Audit storage disabled")
		} else {
			var err error
			auditer, err = audit.Open(auditCfg)
			if err != nil {
				log.Printf("[WARN] Audit storage unavailable (falling back to noop): %v", err)
				auditer = audit.NewNoop(auditCfg)
			} else {
				log.Printf("[INFO] Audit DB: %s (retention=%dd)", auditCfg.DBPath, auditCfg.RetentionDays)
			}
		}
		srv.SetAuditer(auditer)
		defer func() {
			if err := auditer.Close(); err != nil {
				log.Printf("[WARN] audit close: %v", err)
			}
		}()

		// Backup snapshot manager: docker-backed in production, fixture/mock
		// under -no-docker so acceptance can run offline (BACKUP_DESIGN §7).
		var provider backup.Provider
		switch {
		case client != nil:
			provider = docker.NewBackupProvider(client)
		case cfg.FixturePath != "":
			fxp, fxErr := backup.NewFixtureProvider(cfg.FixturePath)
			if fxErr != nil {
				return fxErr
			}
			provider = fxp
		default:
			provider = backup.EmptyProvider{}
		}
		bmgr, bmErr := backup.NewManager(backup.Config{
			Dir:         cfg.BackupDir,
			MaxArchives: cfg.BackupMax,
			Provider:    provider,
			Runtime: backup.RuntimeConfig{
				ServerPort:         cfg.Port,
				TokenMode:          token != "",
				AuditEnabled:       cfg.AuditEnabled && cfg.AuditDBPath != "",
				AuditRetentionDays: cfg.AuditRetentionDays,
				Version:            Version,
				Commit:             Commit,
				BuildDate:          Date,
			},
		})
		if bmErr != nil {
			log.Printf("[WARN] Backup snapshots unavailable: %v", bmErr)
		} else {
			srv.SetBackupManager(bmgr)
			log.Printf("[INFO] Backup snapshots: dir=%s max=%d include_images_default=false", cfg.BackupDir, cfg.BackupMax)
		}

		go func() {
			if err := srv.Start(ctx, cfg.Port); err != nil {
				fmt.Fprintf(os.Stderr, "HTTP server error: %v\n", err)
			}
		}()
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		cancel()
	}()

	// In server mode without a TTY, run headless (no TUI)
	if cfg.Server && !isTTY() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				if client == nil {
					continue // -no-docker: nothing to poll; backup stays available
				}
				containers, err := docker.GetContainerStats(ctx, client)
				if err == nil && srv != nil {
					srv.UpdateData(containers)
				}
			}
		}
	}

	m := &model{dockerClient: client}

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if client == nil {
					continue // -no-docker stub mode
				}
				containers, err := docker.GetContainerStats(ctx, client)
				m.mu.Lock()
				m.containers = containers
				m.err = err
				m.mu.Unlock()

				if srv != nil && err == nil {
					srv.UpdateData(containers)
				}
			}
		}
	}()

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}

// visited reports whether a flag was explicitly passed on the command line.
func visited(fset *flag.FlagSet, name string) bool {
	set := false
	fset.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

func printConfigBanner(cfg *config.Config) {
	if cfg.SampleWritten {
		fmt.Printf("[INFO] First launch: wrote commented config to %s\n", cfg.ConfigFile)
	}
	fmt.Printf("[INFO] Config root: %s (%s)\n", cfg.ConfigDir, cfg.ConfigFile)
	fmt.Printf("[INFO] Data root:   %s\n", cfg.DataRoot)
	fmt.Printf("[INFO] Host file staging:   %s\n", cfg.FilesDir)
	fmt.Printf("[INFO] Container jail root: %s (file limit %d bytes, archive limit %d bytes)\n",
		cfg.Files.JailRoot, cfg.Files.MaxFileBytes, cfg.Files.MaxArchiveBytes)
	for _, note := range cfg.Migrations {
		fmt.Printf("[INFO] %s\n", note)
	}
}

func isTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func printHelp() {
	fmt.Printf("DockerView %s - A beautiful terminal-based Docker container monitoring tool\n\n", Version)
	fmt.Println("USAGE:")
	fmt.Println("  dockerview [OPTIONS]")
	fmt.Println()
	fmt.Println("CONFIGURATION (recommended):")
	fmt.Println("  All settings live in one directory, ConfigRoot:")
	fmt.Println("    DOCKERVIEW_CONFIG_DIR > $XDG_CONFIG_HOME/dockerview > ~/.config/dockerview")
	fmt.Println("  First launch writes a commented config.yaml there (never overwritten).")
	fmt.Println("  Data lives next to it under ConfigRoot/data (db/, backups/, files/).")
	fmt.Println("  Set 'server: true' in config.yaml, then just run: dockerview")
	fmt.Println("  Generate only the sample and exit: dockerview -config-init")
	fmt.Println("  Precedence: CLI flag > DOCKERVIEW_* env > config.yaml > built-in default.")
	fmt.Println("  Environment (all optional):")
	fmt.Println("    DOCKERVIEW_CONFIG_DIR  ConfigRoot location (tests/verify set this)")
	fmt.Println("    DOCKERVIEW_DATA_DIR    DataRoot override (absolute)")
	fmt.Println("    DOCKERVIEW_TOKEN       admin token (or token_file in yaml)")
	fmt.Println("    DOCKERVIEW_SERVER / DOCKERVIEW_PORT  web enable/port overrides")
	fmt.Println("  Docs: docs/DATA_LAYOUT.md, docs/USER_GUIDE.md")
	fmt.Println()
	fmt.Println("OPTIONS (all optional; kept as overrides of config.yaml):")
	fmt.Println("  -update")
	fmt.Println("        Update to the latest")
	fmt.Println("  -config-init")
	fmt.Println("        Write a commented config.yaml sample into ConfigRoot and exit")
	fmt.Println("  -server")
	fmt.Println("        Enable HTTP server for real-time data")
	fmt.Println("  -port int")
	fmt.Println("        Port for HTTP server (default from config.yaml or 8080)")
	fmt.Println("  -token string")
	fmt.Println("        Admin token (also DOCKERVIEW_TOKEN, or token_file in config.yaml;")
	fmt.Println("        auto-generated if none is given)")
	fmt.Println("  -audit-db path")
	fmt.Println("        SQLite path for the audit log (default $DataRoot/db/audit.db; empty disables)")
	fmt.Println("  -audit-retention-days int")
	fmt.Println("        Audit retention window; older rows pruned hourly (default 90, 0 disables)")
	fmt.Println("  -audit-disable")
	fmt.Println("        Disable audit storage; /api/audit endpoints return empty")
	fmt.Println("  -no-docker")
	fmt.Println("        Skip Docker client connection (test/stub mode)")
	fmt.Println("  -fixture path")
	fmt.Println("        JSON fixture with container/image metadata for -no-docker backup verification")
	fmt.Println("  -backup-dir path")
	fmt.Println("        Directory for backup snapshot archives (default $DataRoot/backups)")
	fmt.Println("  -backup-max int")
	fmt.Println("        Maximum number of backup archives to retain (default 10)")
	fmt.Println("  -help")
	fmt.Println("        Show this help message")
	fmt.Println("  -version")
	fmt.Println("        Show version information")
	fmt.Println()
	fmt.Println("EXAMPLES:")
	fmt.Println("  dockerview                       # after setting server:true, that's all")
	fmt.Println("  dockerview -config-init")
	fmt.Println("  DOCKERVIEW_CONFIG_DIR=/etc/dockerview dockerview -port 9000  # CLI wins")
	fmt.Println("  dockerview -version")
	fmt.Println()
	fmt.Println("CONTROLS:")
	fmt.Println("  ↑/↓       Select container")
	fmt.Println("  Enter     Show actions")
	fmt.Println("  s         Start container")
	fmt.Println("  x         Stop container")
	fmt.Println("  r         Restart container")
	fmt.Println("  l         View logs")
	fmt.Println("  e         Execute command")
	fmt.Println("  q/Esc     Back / Exit")
	fmt.Println("  Ctrl+C    Exit application")
	fmt.Println()
	fmt.Println("DOCKER SOCKET:")
	fmt.Println("  DockerView automatically detects Docker sockets.")
	fmt.Println("  You can also specify via DOCKER_HOST environment variable:")
	fmt.Println("  DOCKER_HOST=unix:///path/to/docker.sock dockerview")
	fmt.Println()
	fmt.Println("For more information, visit: https://github.com/zsuroy/dockerview-go")
}
