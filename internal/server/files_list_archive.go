package server

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strconv"

	"github.com/zsuroy/dockerview-go/internal/audit"
	"github.com/zsuroy/dockerview-go/internal/filejail"
	"github.com/zsuroy/dockerview-go/internal/files"
)

// ---- List ----------------------------------------------------------------

// handleFilesList lists immediate children of a jail path (root by
// default). Admin-only: even the file NAMES inside the jail are not shown
// to guests.
func (s *Server) handleFilesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeFilesError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
		return
	}
	ra, authed := s.filesAuth(w, r, false)
	if !authed {
		s.recordFileEvent(r, ra, audit.ActionFileList, "", "", audit.ResultDenied, http.StatusUnauthorized, "invalid or missing token", nil)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeFilesError(w, http.StatusBadRequest, "missing_id", "container id is required")
		s.recordFileEvent(r, ra, audit.ActionFileList, "", "", audit.ResultFailure, http.StatusBadRequest, "missing id", nil)
		return
	}
	raw := r.URL.Query().Get("path")
	if raw == "" {
		raw = "."
	}
	c, ready := s.filesReady(w)
	if !ready {
		return
	}
	j, err := s.newJail()
	if err != nil {
		writeFilesError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	ensureJailInContainer(c, id, j.Root())
	abs, err := j.ResolveReal(raw, c.RootFS(id))
	if err != nil {
		status, code := jailErrorStatus(err)
		writeFilesError(w, status, code, err.Error())
		s.recordFileEvent(r, ra, audit.ActionFileList, id, raw, audit.ResultDenied, status, err.Error(), nil)
		return
	}
	entries, err := c.ReadDir(r.Context(), id, abs)
	if err != nil {
		switch {
		case errors.Is(err, files.ErrNotDirectory):
			writeFilesError(w, http.StatusBadRequest, "not_directory", "path is not a directory")
			s.recordFileEvent(r, ra, audit.ActionFileList, id, abs, audit.ResultFailure, http.StatusBadRequest, "not a directory", nil)
			return
		case errors.Is(err, files.ErrNotFound):
			writeFilesError(w, http.StatusNotFound, "not_found", "directory does not exist")
			s.recordFileEvent(r, ra, audit.ActionFileList, id, abs, audit.ResultFailure, http.StatusNotFound, "not found", nil)
			return
		default:
			writeFilesError(w, http.StatusBadGateway, "read_failed", err.Error())
			return
		}
	}
	if entries == nil {
		entries = []files.DirEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": abs, "entries": entries})
	s.recordFileEvent(r, ra, audit.ActionFileList, id, abs, audit.ResultSuccess, http.StatusOK, "",
		map[string]any{"entries": len(entries)})
}

// ---- Archive -------------------------------------------------------------

// symlinkEscapes reports whether a symlink at absDir/entry points outside
// the jail (lexical, one hop; every hop in a chain is checked by walk).
func symlinkEscapes(jail *filejail.Jail, entryAbs, target string) bool {
	candidate := target
	if !path.IsAbs(target) {
		candidate = path.Join(path.Dir(entryAbs), target)
	}
	_, err := jail.Resolve(candidate)
	return err != nil
}

type archiveStats struct {
	entries int
	bytes   int64
}

// archiveWalkStats counts regular-file payload bytes without reading them.
func archiveWalkStats(ctx context.Context, jail *filejail.Jail, c files.Copier, cid, abs string) (archiveStats, error) {
	var st archiveStats
	err := c.Walk(ctx, cid, abs, func(rel string, info files.FileInfo) error {
		if info.IsDir {
			st.entries++
			return nil
		}
		if info.IsSymlink {
			if symlinkEscapes(jail, info.AbsPath, info.LinkTarget) {
				return errArchiveEscape
			}
			st.entries++
			return nil
		}
		st.entries++
		st.bytes += info.Size
		return nil
	})
	return st, err
}

var errArchiveEscape = errors.New("archive path escapes jail via symlink")

func (s *Server) archivePreview(w http.ResponseWriter, r *http.Request, ra resolvedActor, body filesPathRequest) {
	if body.ID == "" {
		writeFilesError(w, http.StatusBadRequest, "missing_id", "container id is required")
		return
	}
	c, ready := s.filesReady(w)
	if !ready {
		return
	}
	st := s.getFilesSettings()
	jail, err := s.newJail()
	if err != nil {
		writeFilesError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	ensureJailInContainer(c, body.ID, jail.Root())
	abs, err := jail.ResolveReal(body.Path, c.RootFS(body.ID))
	if err != nil {
		status, code := jailErrorStatus(err)
		writeFilesError(w, status, code, err.Error())
		s.recordFileEvent(r, ra, audit.ActionFileArchive, body.ID, body.Path, audit.ResultDenied, status, err.Error(), nil)
		return
	}
	if abs == jail.Root() {
		writeFilesError(w, http.StatusBadRequest, "root_not_allowed", "archive requires a subdirectory of the jail root")
		s.recordFileEvent(r, ra, audit.ActionFileArchive, body.ID, abs, audit.ResultDenied, http.StatusBadRequest, "root not allowed", nil)
		return
	}
	info, err := c.Stat(r.Context(), body.ID, abs)
	if err != nil {
		if errors.Is(err, files.ErrNotFound) {
			writeFilesError(w, http.StatusNotFound, "not_found", "directory does not exist")
			return
		}
		writeFilesError(w, http.StatusBadGateway, "stat_failed", err.Error())
		return
	}
	if !info.IsDir {
		writeFilesError(w, http.StatusBadRequest, "not_directory", "archive target must be a directory")
		s.recordFileEvent(r, ra, audit.ActionFileArchive, body.ID, abs, audit.ResultDenied, http.StatusBadRequest, "archive target is not a directory", nil)
		return
	}
	stats, err := archiveWalkStats(r.Context(), jail, c, body.ID, abs)
	if err != nil {
		writeFilesError(w, http.StatusBadRequest, "path_escapes", err.Error())
		s.recordFileEvent(r, ra, audit.ActionFileArchive, body.ID, abs, audit.ResultDenied, http.StatusBadRequest, err.Error(), nil)
		return
	}
	capped := stats.bytes > st.cfg.MaxArchiveBytes
	if capped {
		writeFilesError(w, http.StatusRequestEntityTooLarge, "too_large",
			"archive payload "+strconv.FormatInt(stats.bytes, 10)+" exceeds limit "+strconv.FormatInt(st.cfg.MaxArchiveBytes, 10))
		s.recordFileEvent(r, ra, audit.ActionFileArchive, body.ID, abs, audit.ResultDenied, http.StatusRequestEntityTooLarge, "size limit", map[string]any{"bytes": stats.bytes})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path": abs, "entries": stats.entries, "bytes": stats.bytes,
		"max_archive_bytes": st.cfg.MaxArchiveBytes, "name": path.Base(abs) + ".tar",
	})
	s.recordFileEvent(r, ra, audit.ActionFileArchive, body.ID, abs, audit.ResultSuccess, http.StatusOK, "preview",
		map[string]any{"bytes": stats.bytes, "entries": stats.entries})
}

func (s *Server) handleFilesArchivePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeFilesError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	ra, authed := s.filesAuth(w, r, false)
	if !authed {
		s.recordFileEvent(r, ra, audit.ActionFileArchive, "", "", audit.ResultDenied, http.StatusUnauthorized, "invalid or missing token", nil)
		return
	}
	var req filesPathRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeFilesError(w, http.StatusBadRequest, "invalid_body", "request body must be JSON {id,path}")
		return
	}
	s.archivePreview(w, r, ra, req)
}

// capWriter hard-fails the moment cumulative bytes exceed max — no half
// archive is ever produced.
type capWriter struct {
	w   io.Writer
	n   int64
	max int64
}

func (c *capWriter) Write(p []byte) (int, error) {
	if c.n+int64(len(p)) > c.max {
		return 0, errTransferTooLarge
	}
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

func (s *Server) handleFilesArchive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeFilesError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
		return
	}
	ra, authed := s.filesAuth(w, r, true)
	if !authed {
		s.recordFileEvent(r, ra, audit.ActionFileArchive, "", "", audit.ResultDenied, http.StatusUnauthorized, "invalid or missing token", nil)
		return
	}
	id := r.URL.Query().Get("id")
	raw := r.URL.Query().Get("path")
	if id == "" || raw == "" {
		writeFilesError(w, http.StatusBadRequest, "missing_params", "id and path are required")
		s.recordFileEvent(r, ra, audit.ActionFileArchive, id, raw, audit.ResultFailure, http.StatusBadRequest, "missing id/path", nil)
		return
	}
	c, ready := s.filesReady(w)
	if !ready {
		return
	}
	st := s.getFilesSettings()
	jail, err := s.newJail()
	if err != nil {
		writeFilesError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	ensureJailInContainer(c, id, jail.Root())
	abs, err := jail.ResolveReal(raw, c.RootFS(id))
	if err != nil {
		status, code := jailErrorStatus(err)
		writeFilesError(w, status, code, err.Error())
		s.recordFileEvent(r, ra, audit.ActionFileArchive, id, raw, audit.ResultDenied, status, err.Error(), nil)
		return
	}
	if abs == jail.Root() {
		writeFilesError(w, http.StatusBadRequest, "root_not_allowed", "archive requires a subdirectory of the jail root")
		return
	}
	// GET must not silently produce an empty tar for a file target.
	if info, serr := c.Stat(r.Context(), id, abs); serr != nil {
		if errors.Is(serr, files.ErrNotFound) {
			writeFilesError(w, http.StatusNotFound, "not_found", "directory does not exist")
			return
		}
		writeFilesError(w, http.StatusBadGateway, "stat_failed", serr.Error())
		return
	} else if !info.IsDir {
		writeFilesError(w, http.StatusBadRequest, "not_directory", "archive target must be a directory")
		s.recordFileEvent(r, ra, audit.ActionFileArchive, id, abs, audit.ResultDenied, http.StatusBadRequest, "archive target is not a directory", nil)
		return
	}

	tmp, tmpName, err := s.stageFile(".dv-tar-*")
	if err != nil {
		writeFilesError(w, http.StatusInternalServerError, "stage_failed", err.Error())
		return
	}
	defer os.Remove(tmpName)

	// Deterministic tar: entries sorted (walk order is lexical), no symlink
	// entries (in-jail links are dereferenced to regular file content, so a
	// tar extract can never recreate an out-of-jail link).
	tw := tar.NewWriter(&capWriter{w: tmp, max: st.cfg.MaxArchiveBytes + 1<<20 /*tar header slack*/})
	prefix := path.Base(abs)
	var regularBytes int64
	walkErr := c.Walk(r.Context(), id, abs, func(rel string, info files.FileInfo) error {
		if rel == "." {
			if info.IsDir {
				return tw.WriteHeader(&tar.Header{Name: prefix + "/", Typeflag: tar.TypeDir, Mode: 0o750})
			}
			return nil
		}
		name := prefix + "/" + path.Clean(rel)
		if info.IsDir {
			return tw.WriteHeader(&tar.Header{Name: name + "/", Typeflag: tar.TypeDir, Mode: 0o750})
		}
		if info.IsSymlink {
			if symlinkEscapes(jail, info.AbsPath, info.LinkTarget) {
				return errArchiveEscape
			}
			// Dereference in-jail links into plain regular-file content.
			info, err = followLink(c, r.Context(), id, info)
			if err != nil {
				return err
			}
		}
		if regularBytes+info.Size > st.cfg.MaxArchiveBytes {
			return errTransferTooLarge
		}
		hdr := &tar.Header{Name: name, Mode: 0o600, Size: info.Size}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		pr, pw := io.Pipe()
		go func() { _, gerr := c.Get(r.Context(), id, info.AbsPath, pw); pw.CloseWithError(gerr) }()
		n, err := io.CopyN(tw, pr, info.Size)
		pr.Close()
		if err != nil || n != info.Size {
			if err == nil {
				err = fmt.Errorf("archive member %q truncated (%d/%d bytes)", rel, n, info.Size)
			}
			return err
		}
		regularBytes += n
		return nil
	})
	if walkErr != nil {
		tmp.Close()
		switch {
		case errors.Is(walkErr, errArchiveEscape):
			writeFilesError(w, http.StatusBadRequest, "path_escapes", walkErr.Error())
			s.recordFileEvent(r, ra, audit.ActionFileArchive, id, abs, audit.ResultDenied, http.StatusBadRequest, "symlink escape", nil)
		case errors.Is(walkErr, errTransferTooLarge):
			writeFilesError(w, http.StatusRequestEntityTooLarge, "too_large", "archive payload exceeds configured limit")
			s.recordFileEvent(r, ra, audit.ActionFileArchive, id, abs, audit.ResultDenied, http.StatusRequestEntityTooLarge, "size limit", map[string]any{"bytes": regularBytes})
		case errors.Is(walkErr, files.ErrNotFound):
			writeFilesError(w, http.StatusNotFound, "not_found", "directory does not exist")
		default:
			writeFilesError(w, http.StatusBadGateway, "archive_failed", walkErr.Error())
		}
		return
	}
	if err := tw.Close(); err != nil {
		writeFilesError(w, http.StatusInternalServerError, "archive_failed", err.Error())
		return
	}
	tmp.Close()

	// Hash the finished tar.
	sum, size, err := hashFile(tmpName)
	if err != nil {
		writeFilesError(w, http.StatusInternalServerError, "stage_failed", err.Error())
		return
	}
	f, err := os.Open(tmpName)
	if err != nil {
		writeFilesError(w, http.StatusInternalServerError, "stage_failed", err.Error())
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("X-Dockerview-Sha256", sum)
	setContentDisposition(w, path.Base(abs)+".tar")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	if _, err := io.Copy(w, f); err != nil {
		return
	}
	s.recordFileEvent(r, ra, audit.ActionFileArchive, id, abs, audit.ResultSuccess, http.StatusOK, "",
		map[string]any{"bytes": regularBytes, "sha256": sum})
}

func followLink(c files.Copier, ctx context.Context, cid string, info files.FileInfo) (files.FileInfo, error) {
	target := info.LinkTarget
	if !path.IsAbs(target) {
		target = path.Join(path.Dir(info.AbsPath), target)
	}
	return c.Stat(ctx, cid, target)
}

func hashFile(name string) (string, int64, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
