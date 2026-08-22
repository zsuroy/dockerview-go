package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// MigrateLegacy moves the old cwd-relative data tree (./data/dockerview.db,
// ./data/backups) into the resolved DataRoot. It is deliberately loud:
// when both a legacy artifact and its target exist, or a cross-device move
// fails, it returns an error instead of silently running with two data
// directories. It is a no-op when no legacy tree exists (fresh installs).
func MigrateLegacy(cwd string, cfg *Config) ([]string, error) {
	var notes []string
	legacyRoot := filepath.Join(cwd, "data")
	if fi, err := os.Stat(legacyRoot); err != nil || !fi.IsDir() {
		return nil, nil // nothing to migrate
	}
	if filepath.Clean(legacyRoot) == filepath.Clean(cfg.DataRoot) {
		return nil, nil // already IS the data root (exotic override)
	}

	// --- audit database (+ sqlite -wal/-shm sidecars) ----------------------
	legacyDB := filepath.Join(legacyRoot, "dockerview.db")
	targetDB := filepath.Join(cfg.DBDir, DefaultAuditDBName)
	if _, err := os.Stat(legacyDB); err == nil {
		if _, terr := os.Stat(targetDB); terr == nil {
			return nil, fmt.Errorf(
				"data migration: both legacy %s and target %s exist; move one aside manually and restart",
				legacyDB, targetDB)
		}
		if err := moveFile(legacyDB, targetDB); err != nil {
			return nil, fmt.Errorf("data migration: %w", err)
		}
		notes = append(notes, fmt.Sprintf("migrated %s -> %s", legacyDB, targetDB))
		for _, suffix := range []string{"-wal", "-shm"} {
			from := legacyDB + suffix
			to := targetDB + suffix
			if _, err := os.Stat(from); err == nil {
				if err := moveFile(from, to); err != nil {
					return nil, fmt.Errorf("data migration sidecar: %w", err)
				}
				notes = append(notes, fmt.Sprintf("migrated %s -> %s", from, to))
			}
		}
	}

	// --- backups directory --------------------------------------------------
	legacyBackups := filepath.Join(legacyRoot, "backups")
	if fi, err := os.Stat(legacyBackups); err == nil && fi.IsDir() {
		switch entries, _ := os.ReadDir(legacyBackups); {
		case len(entries) == 0:
			// empty legacy dir: drop it quietly, ensure target exists.
			_ = os.Remove(legacyBackups)
		default:
			if tfi, terr := os.Stat(cfg.BackupsDir); terr == nil && tfi.IsDir() {
				tEntries, _ := os.ReadDir(cfg.BackupsDir)
				if len(tEntries) != 0 {
					return nil, fmt.Errorf(
						"data migration: both legacy %s and target %s are non-empty; merge or move one aside manually and restart",
						legacyBackups, cfg.BackupsDir)
				}
				_ = os.RemoveAll(cfg.BackupsDir) // target exists but empty
			}
			if err := os.Rename(legacyBackups, cfg.BackupsDir); err != nil {
				return nil, fmt.Errorf("data migration: move %s -> %s: %w",
					legacyBackups, cfg.BackupsDir, err)
			}
			notes = append(notes, fmt.Sprintf("migrated %s -> %s", legacyBackups, cfg.BackupsDir))
		}
	}

	// Remove the legacy root only when it is now empty; other tooling may
	// keep files there, and that must not break startup.
	if entries, err := os.ReadDir(legacyRoot); err == nil && len(entries) == 0 {
		if err := os.Remove(legacyRoot); err == nil {
			notes = append(notes, fmt.Sprintf("removed empty legacy dir %s", legacyRoot))
		}
	}
	return notes, nil
}

// moveFile renames, falling back to copy+remove across filesystems.
func moveFile(from, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o750); err != nil {
		return err
	}
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	in, err := os.Open(from)
	if err != nil {
		return fmt.Errorf("open %s: %w", from, err)
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fi.Mode().Perm())
	if err != nil {
		return fmt.Errorf("create %s: %w", to, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(to)
		return fmt.Errorf("copy %s -> %s: %w", from, to, err)
	}
	if err := out.Close(); err != nil {
		os.Remove(to)
		return err
	}
	if err := os.Remove(from); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove source %s after copy: %w", from, err)
	}
	return nil
}
