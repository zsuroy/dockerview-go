package server

import (
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
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zsuroy/dockerview-go/internal/audit"
	"github.com/zsuroy/dockerview-go/internal/filejail"
	"github.com/zsuroy/dockerview-go/internal/files"
)

type filesPathRequest struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

func decodeFilesBody(w http.ResponseWriter, r *http.Request) (filesPathRequest, bool) {
	var req filesPathRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeFilesError(w, http.StatusBadRequest, "invalid_body", "request body must be JSON {id,path}")
		return req, false
	}
	return req, true
}

// jailErrorStatus maps path-jail failures to HTTP responses.
func jailErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, filejail.ErrEscape):
		return http.StatusBadRequest, "path_escapes"
	case errors.Is(err, filejail.ErrInvalidPath):
		return http.StatusBadRequest, "invalid_path"
	case errors.Is(err, filejail.ErrRootAsFile):
		return http.StatusBadRequest, "invalid_target"
	case errors.Is(err, filejail.ErrNotFound):
		return http.StatusNotFound, "not_found"
	default:
		return http.StatusBadRequest, "invalid_path"
	}
}

// linkStaysInJail applies the lexical symlink rule for backends that cannot
// resolve links on a host FS (the docker production backend): an existing
// symlink reported by Stat must point inside the jail, absolute or relative.
func linkStaysInJail(j *filejail.Jail, entryDir string, info files.FileInfo) error {
	if !info.IsSymlink {
		return nil
	}
	target := info.LinkTarget
	if !path.IsAbs(target) {
		target = path.Join(entryDir, target)
	}
	if _, err := j.Resolve(target); err != nil {
		return fmt.Errorf("symlink %q -> %q escapes jail: %w", info.AbsPath, info.LinkTarget, err)
	}
	return nil
}

func (s *Server) filesReady(w http.ResponseWriter) (files.Copier, bool) {
	c := s.fileCopier()
	if c == nil {
		writeFilesError(w, http.StatusServiceUnavailable, "files_unavailable", "file transfer backend not configured")
		return nil, false
	}
	return c, true
}

// resolveContainerPath runs the full lexical+symlink jail check.
func (s *Server) resolveContainerPath(c files.Copier, id, raw string, fileTarget bool) (string, error) {
	j, err := s.newJail()
	if err != nil {
		return "", err
	}
	ensureJailInContainer(c, id, j.Root())
	rootFS := c.RootFS(id)
	if fileTarget {
		return j.ResolveFile(raw, rootFS)
	}
	return j.ResolveReal(raw, rootFS)
}

// missingParentDirs lists the directories on abs's ancestor chain — the
// jail root itself included — that do not yet exist in the container. The
// confirm handler creates exactly these when the operator opts in.
func (s *Server) missingParentDirs(c files.Copier, ctx context.Context, cid, abs string) ([]string, error) {
	jailRoot := s.getFilesSettings().cfg.JailRoot
	var missing []string
	acc := jailRoot
	// The jail root itself must exist before anything below it can be
	// created: docker's CopyToContainer requires the extraction target to
	// pre-exist, so a missing root is the first (and only) gateable gap.
	if info, serr := c.Stat(ctx, cid, jailRoot); serr != nil {
		if !errors.Is(serr, files.ErrNotFound) {
			return nil, serr
		}
		missing = append(missing, jailRoot)
	} else if !info.IsDir {
		return nil, fmt.Errorf("ancestor %s exists and is not a directory", jailRoot)
	}
	rel, err := filepath.Rel(filepath.ToSlash(jailRoot), filepath.ToSlash(abs))
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil, fmt.Errorf("path %q is outside jail %q", abs, jailRoot)
	}
	for _, seg := range strings.Split(path.Dir(rel), "/") {
		if seg == "." {
			continue
		}
		acc = path.Join(acc, seg)
		info, serr := c.Stat(ctx, cid, acc)
		if serr == nil {
			if !info.IsDir {
				return nil, fmt.Errorf("ancestor %s exists and is not a directory", acc)
			}
			continue
		}
		if errors.Is(serr, files.ErrNotFound) {
			missing = append(missing, acc)
			continue
		}
		return nil, serr
	}
	return missing, nil
}

// ---- Copy in: preview ----------------------------------------------------

func (s *Server) handleFilesInPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeFilesError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	ra, authed := s.filesAuth(w, r, false)
	if !authed {
		s.recordFileEvent(r, ra, audit.ActionFileIn, "", "", audit.ResultDenied, http.StatusUnauthorized, "invalid or missing token", nil)
		return
	}
	req, ok := decodeFilesBody(w, r)
	if !ok {
		s.recordFileEvent(r, ra, audit.ActionFileIn, "", "", audit.ResultFailure, http.StatusBadRequest, "invalid body", nil)
		return
	}
	if req.ID == "" {
		writeFilesError(w, http.StatusBadRequest, "missing_id", "container id is required")
		return
	}
	c, ready := s.filesReady(w)
	if !ready {
		s.recordFileEvent(r, ra, audit.ActionFileIn, req.ID, req.Path, audit.ResultFailure, http.StatusServiceUnavailable, "copier unavailable", nil)
		return
	}
	abs, err := s.resolveContainerPath(c, req.ID, req.Path, true)
	if err != nil {
		status, code := jailErrorStatus(err)
		writeFilesError(w, status, code, err.Error())
		s.recordFileEvent(r, ra, audit.ActionFileIn, req.ID, req.Path, audit.ResultDenied, status, err.Error(), nil)
		return
	}
	// Missing intermediate directories are created by confirm (mock
	// MkdirAll; docker tar emits the dir entries) — but only after the
	// operator explicitly opts in, mirroring the overwrite gate.
	exists, isDir := false, false
	var existingSize int64
	if info, serr := c.Stat(r.Context(), req.ID, abs); serr == nil {
		exists = true
		isDir = info.IsDir
		existingSize = info.Size
	} else if !errors.Is(serr, files.ErrNotFound) {
		writeFilesError(w, http.StatusInternalServerError, "stat_failed", serr.Error())
		return
	}
	if isDir {
		writeFilesError(w, http.StatusBadRequest, "target_is_directory", "destination exists and is a directory")
		s.recordFileEvent(r, ra, audit.ActionFileIn, req.ID, abs, audit.ResultDenied, http.StatusBadRequest, "target is directory", nil)
		return
	}
	st := s.getFilesSettings()
	resp := map[string]any{
		"path":               abs,
		"exists":             exists,
		"overwrite_required": exists,
		"size_existing":      existingSize,
		"max_file_bytes":     st.cfg.MaxFileBytes,
	}
	if missing, merr := s.missingParentDirs(c, r.Context(), req.ID, abs); merr == nil && len(missing) > 0 {
		resp["missing_dirs"] = missing
		resp["mkdir_required"] = true
	}
	writeJSON(w, http.StatusOK, resp)
	s.recordFileEvent(r, ra, audit.ActionFileIn, req.ID, abs, audit.ResultSuccess, http.StatusOK, "preview", map[string]any{"exists": exists})
}

// ---- Copy in: confirm (multipart) ---------------------------------------

func (s *Server) handleFilesIn(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeFilesError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	ra, authed := s.filesAuth(w, r, false)
	if !authed {
		s.recordFileEvent(r, ra, audit.ActionFileIn, "", "", audit.ResultDenied, http.StatusUnauthorized, "invalid or missing token", nil)
		return
	}
	st := s.getFilesSettings()
	c, ready := s.filesReady(w)
	if !ready {
		return
	}

	// Stream the multipart body: small fields are read inline; the file part
	// goes straight to a 0600 staging temp under $DataRoot/files (never the
	// repo cwd or the system temp). Path/quota gates run before the bytes
	// enter the container, and every rejection deletes the staging file.
	r.Body = http.MaxBytesReader(w, r.Body, st.cfg.MaxFileBytes+(1<<20))
	mr, err := r.MultipartReader()
	if err != nil {
		writeFilesError(w, http.StatusBadRequest, "invalid_multipart", err.Error())
		return
	}
	var id, targetPath string
	var stagedName string
	var n int64
	var sumHex string
	overwrite := false
	mkdir := false
	sawFile := false
	defer func() { _ = os.Remove(stagedName) }()
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			writeFilesError(w, http.StatusBadRequest, "invalid_multipart", err.Error())
			return
		}
		switch part.FormName() {
		case "id":
			b, _ := io.ReadAll(io.LimitReader(part, 4096))
			id = string(b)
		case "path":
			b, _ := io.ReadAll(io.LimitReader(part, 4096))
			targetPath = string(b)
		case "overwrite":
			b, _ := io.ReadAll(io.LimitReader(part, 16))
			overwrite = string(b) == "true" || string(b) == "1"
		case "mkdir":
			b, _ := io.ReadAll(io.LimitReader(part, 16))
			mkdir = string(b) == "true" || string(b) == "1"
		case "file":
			sawFile = true
			tmp, tmpName, terr := s.stageFile(".dv-in-*")
			if terr != nil {
				writeFilesError(w, http.StatusInternalServerError, "stage_failed", terr.Error())
				return
			}
			hasher := sha256.New()
			n, err = limitedCopy(io.MultiWriter(tmp, hasher), part, st.cfg.MaxFileBytes)
			tmp.Close()
			part.Close()
			stagedName = tmpName
			if err != nil {
				_ = os.Remove(stagedName)
				stagedName = ""
				if errors.Is(err, errTransferTooLarge) {
					writeFilesError(w, http.StatusRequestEntityTooLarge, "too_large",
						fmt.Sprintf("file exceeds %d byte limit", st.cfg.MaxFileBytes))
					s.recordFileEvent(r, ra, audit.ActionFileIn, id, targetPath, audit.ResultDenied, http.StatusRequestEntityTooLarge, "size limit exceeded", map[string]any{"bytes": n})
					return
				}
				writeFilesError(w, http.StatusBadRequest, "read_failed", err.Error())
				return
			}
			sumHex = hex.EncodeToString(hasher.Sum(nil))
		default:
			part.Close()
		}
	}
	if id == "" || targetPath == "" || !sawFile {
		writeFilesError(w, http.StatusBadRequest, "missing_fields", "id, path and file are required")
		s.recordFileEvent(r, ra, audit.ActionFileIn, id, targetPath, audit.ResultFailure, http.StatusBadRequest, "missing fields", nil)
		return
	}

	abs, err := s.resolveContainerPath(c, id, targetPath, true)
	if err != nil {
		status, code := jailErrorStatus(err)
		writeFilesError(w, status, code, err.Error())
		s.recordFileEvent(r, ra, audit.ActionFileIn, id, targetPath, audit.ResultDenied, status, err.Error(), nil)
		return
	}

	lockKey := "in:" + id + ":" + abs
	if !s.tryFilesLock(lockKey) {
		writeFilesError(w, http.StatusConflict, "in_flight", "an identical transfer is already running")
		s.recordFileEvent(r, ra, audit.ActionFileIn, id, abs, audit.ResultDenied, http.StatusConflict, "concurrent transfer", nil)
		return
	}
	defer s.releaseFilesLock(lockKey)

	// Destination state re-checked under the lock (TOCTU gate).
	info, statErr := c.Stat(r.Context(), id, abs)
	switch {
	case statErr == nil && info.IsDir:
		writeFilesError(w, http.StatusBadRequest, "target_is_directory", "destination exists and is a directory")
		s.recordFileEvent(r, ra, audit.ActionFileIn, id, abs, audit.ResultDenied, http.StatusBadRequest, "target is directory", nil)
		return
	case statErr == nil && !overwrite:
		writeFilesError(w, http.StatusConflict, "overwrite_required", "destination exists; repeat with overwrite=true after preview")
		s.recordFileEvent(r, ra, audit.ActionFileIn, id, abs, audit.ResultDenied, http.StatusConflict, "overwrite gate", map[string]any{"exists": true})
		return
	case statErr != nil && !errors.Is(statErr, files.ErrNotFound):
		writeFilesError(w, http.StatusInternalServerError, "stat_failed", statErr.Error())
		return
	}
	overwritten := statErr == nil
	if !overwritten {
		// Target is new: missing parents need an explicit opt-in, and any
		// existing ancestor must be a real in-jail directory (not a file,
		// not an escaping symlink) before we create children below it.
		missing, merr := s.missingParentDirs(c, r.Context(), id, abs)
		if merr != nil {
			writeFilesError(w, http.StatusBadRequest, "path_escapes", merr.Error())
			s.recordFileEvent(r, ra, audit.ActionFileIn, id, abs, audit.ResultDenied, http.StatusBadRequest, merr.Error(), nil)
			return
		}
		if len(missing) > 0 && !mkdir {
			writeFilesError(w, http.StatusConflict, "mkdir_required",
				"destination directory does not exist; repeat with mkdir=true after preview")
			s.recordFileEvent(r, ra, audit.ActionFileIn, id, abs, audit.ResultDenied, http.StatusConflict, "mkdir gate", map[string]any{"missing": missing})
			return
		}
	}
	if overwritten {
		if j, jerr := s.newJail(); jerr == nil {
			if lerr := linkStaysInJail(j, path.Dir(abs), info); lerr != nil {
				writeFilesError(w, http.StatusBadRequest, "path_escapes", lerr.Error())
				s.recordFileEvent(r, ra, audit.ActionFileIn, id, abs, audit.ResultDenied, http.StatusBadRequest, lerr.Error(), nil)
				return
			}
		}
	}

	staged, err := os.Open(stagedName)
	if err != nil {
		writeFilesError(w, http.StatusInternalServerError, "stage_failed", err.Error())
		return
	}
	defer staged.Close()
	if _, err := c.Put(r.Context(), id, abs, staged, n, 0o600); err != nil {
		writeFilesError(w, http.StatusBadGateway, "copy_failed", err.Error())
		s.recordFileEvent(r, ra, audit.ActionFileIn, id, abs, audit.ResultFailure, http.StatusBadGateway, err.Error(), map[string]any{"bytes": n})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path": abs, "bytes": n, "sha256": sumHex, "overwritten": overwritten,
	})
	s.recordFileEvent(r, ra, audit.ActionFileIn, id, abs, audit.ResultSuccess, http.StatusOK, "",
		map[string]any{"bytes": n, "sha256": sumHex, "overwrite": overwrite, "overwritten": overwritten})
}

// ---- Copy out: preview ---------------------------------------------------

func (s *Server) handleFilesOutPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeFilesError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	ra, authed := s.filesAuth(w, r, false)
	if !authed {
		s.recordFileEvent(r, ra, audit.ActionFileOut, "", "", audit.ResultDenied, http.StatusUnauthorized, "invalid or missing token", nil)
		return
	}
	req, ok := decodeFilesBody(w, r)
	if !ok {
		s.recordFileEvent(r, ra, audit.ActionFileOut, "", "", audit.ResultFailure, http.StatusBadRequest, "invalid body", nil)
		return
	}
	if req.ID == "" || req.Path == "" {
		writeFilesError(w, http.StatusBadRequest, "missing_params", "id and path are required")
		s.recordFileEvent(r, ra, audit.ActionFileOut, req.ID, req.Path, audit.ResultFailure, http.StatusBadRequest, "missing id/path", nil)
		return
	}
	c, ready := s.filesReady(w)
	if !ready {
		return
	}
	abs, err := s.resolveContainerPath(c, req.ID, req.Path, true)
	if err != nil {
		status, code := jailErrorStatus(err)
		writeFilesError(w, status, code, err.Error())
		s.recordFileEvent(r, ra, audit.ActionFileOut, req.ID, req.Path, audit.ResultDenied, status, err.Error(), nil)
		return
	}
	info, err := c.Stat(r.Context(), req.ID, abs)
	if err != nil {
		if errors.Is(err, files.ErrNotFound) {
			writeFilesError(w, http.StatusNotFound, "not_found", "file does not exist")
			s.recordFileEvent(r, ra, audit.ActionFileOut, req.ID, abs, audit.ResultFailure, http.StatusNotFound, "not found", nil)
			return
		}
		writeFilesError(w, http.StatusInternalServerError, "stat_failed", err.Error())
		return
	}
	if info.IsDir {
		writeFilesError(w, http.StatusBadRequest, "is_directory", "path is a directory; use /api/files/archive")
		s.recordFileEvent(r, ra, audit.ActionFileOut, req.ID, abs, audit.ResultDenied, http.StatusBadRequest, "target is directory", nil)
		return
	}
	if j, jerr := s.newJail(); jerr == nil {
		if lerr := linkStaysInJail(j, path.Dir(abs), info); lerr != nil {
			writeFilesError(w, http.StatusBadRequest, "path_escapes", lerr.Error())
			s.recordFileEvent(r, ra, audit.ActionFileOut, req.ID, abs, audit.ResultDenied, http.StatusBadRequest, lerr.Error(), nil)
			return
		}
	}
	st := s.getFilesSettings()
	if info.Size > st.cfg.MaxFileBytes {
		writeFilesError(w, http.StatusRequestEntityTooLarge, "too_large",
			fmt.Sprintf("file %d bytes exceeds limit %d", info.Size, st.cfg.MaxFileBytes))
		s.recordFileEvent(r, ra, audit.ActionFileOut, req.ID, abs, audit.ResultDenied, http.StatusRequestEntityTooLarge, "size limit exceeded", map[string]any{"size": info.Size})
		return
	}
	hasher := sha256.New()
	n, err := c.Get(r.Context(), req.ID, abs, hasher)
	if err != nil {
		if errors.Is(err, files.ErrIsDirectory) {
			writeFilesError(w, http.StatusBadRequest, "is_directory", "path resolves to a directory; use /api/files/archive")
			s.recordFileEvent(r, ra, audit.ActionFileOut, req.ID, abs, audit.ResultDenied, http.StatusBadRequest, "resolves to directory", nil)
			return
		}
		writeFilesError(w, http.StatusBadGateway, "read_failed", err.Error())
		return
	}
	sum := hex.EncodeToString(hasher.Sum(nil))
	writeJSON(w, http.StatusOK, map[string]any{
		"path": abs, "size": n, "sha256": sum, "name": path.Base(abs),
	})
	s.recordFileEvent(r, ra, audit.ActionFileOut, req.ID, abs, audit.ResultSuccess, http.StatusOK, "preview",
		map[string]any{"bytes": n, "sha256": sum})
}

// ---- Copy out: download --------------------------------------------------

func (s *Server) handleFilesOut(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeFilesError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
		return
	}
	ra, authed := s.filesAuth(w, r, true)
	if !authed {
		s.recordFileEvent(r, ra, audit.ActionFileOut, "", "", audit.ResultDenied, http.StatusUnauthorized, "invalid or missing token", nil)
		return
	}
	id := r.URL.Query().Get("id")
	targetPath := r.URL.Query().Get("path")
	if id == "" || targetPath == "" {
		writeFilesError(w, http.StatusBadRequest, "missing_params", "id and path are required")
		return
	}
	c, ready := s.filesReady(w)
	if !ready {
		return
	}
	abs, err := s.resolveContainerPath(c, id, targetPath, true)
	if err != nil {
		status, code := jailErrorStatus(err)
		writeFilesError(w, status, code, err.Error())
		s.recordFileEvent(r, ra, audit.ActionFileOut, id, targetPath, audit.ResultDenied, status, err.Error(), nil)
		return
	}
	info, err := c.Stat(r.Context(), id, abs)
	if err != nil {
		if errors.Is(err, files.ErrNotFound) {
			writeFilesError(w, http.StatusNotFound, "not_found", "file does not exist")
			s.recordFileEvent(r, ra, audit.ActionFileOut, id, abs, audit.ResultFailure, http.StatusNotFound, "not found", nil)
			return
		}
		writeFilesError(w, http.StatusInternalServerError, "stat_failed", err.Error())
		return
	}
	if info.IsDir {
		writeFilesError(w, http.StatusBadRequest, "is_directory", "path is a directory; use /api/files/archive")
		s.recordFileEvent(r, ra, audit.ActionFileOut, id, abs, audit.ResultDenied, http.StatusBadRequest, "target is directory", nil)
		return
	}
	if j, jerr := s.newJail(); jerr == nil {
		if lerr := linkStaysInJail(j, path.Dir(abs), info); lerr != nil {
			writeFilesError(w, http.StatusBadRequest, "path_escapes", lerr.Error())
			s.recordFileEvent(r, ra, audit.ActionFileOut, id, abs, audit.ResultDenied, http.StatusBadRequest, lerr.Error(), nil)
			return
		}
	}
	st := s.getFilesSettings()
	if info.Size > st.cfg.MaxFileBytes {
		writeFilesError(w, http.StatusRequestEntityTooLarge, "too_large", "file exceeds configured limit")
		s.recordFileEvent(r, ra, audit.ActionFileOut, id, abs, audit.ResultDenied, http.StatusRequestEntityTooLarge, "size limit", map[string]any{"size": info.Size})
		return
	}

	tmp, tmpName, err := s.stageFile(".dv-out-*")
	if err != nil {
		writeFilesError(w, http.StatusInternalServerError, "stage_failed", err.Error())
		return
	}
	defer os.Remove(tmpName)
	hasher := sha256.New()
	pr, pw := io.Pipe()
	go func() {
		_, gerr := c.Get(r.Context(), id, abs, pw)
		pw.CloseWithError(gerr)
	}()
	n, err := limitedCopy(io.MultiWriter(tmp, hasher), pr, st.cfg.MaxFileBytes)
	tmp.Close()
	if err != nil {
		if errors.Is(err, errTransferTooLarge) {
			writeFilesError(w, http.StatusRequestEntityTooLarge, "too_large", "file exceeds configured limit")
			s.recordFileEvent(r, ra, audit.ActionFileOut, id, abs, audit.ResultDenied, http.StatusRequestEntityTooLarge, "size limit", map[string]any{"bytes": n})
			return
		}
		if errors.Is(err, files.ErrIsDirectory) {
			writeFilesError(w, http.StatusBadRequest, "is_directory", "path resolves to a directory; use /api/files/archive")
			s.recordFileEvent(r, ra, audit.ActionFileOut, id, abs, audit.ResultDenied, http.StatusBadRequest, "resolves to directory", nil)
			return
		}
		writeFilesError(w, http.StatusBadGateway, "read_failed", err.Error())
		s.recordFileEvent(r, ra, audit.ActionFileOut, id, abs, audit.ResultFailure, http.StatusBadGateway, err.Error(), nil)
		return
	}
	sum := hex.EncodeToString(hasher.Sum(nil))

	f, err := os.Open(tmpName)
	if err != nil {
		writeFilesError(w, http.StatusInternalServerError, "stage_failed", err.Error())
		return
	}
	defer f.Close()
	fi, _ := f.Stat()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Dockerview-Sha256", sum)
	setContentDisposition(w, path.Base(abs))
	w.Header().Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
	if _, err := io.Copy(w, f); err != nil {
		return
	}
	s.recordFileEvent(r, ra, audit.ActionFileOut, id, abs, audit.ResultSuccess, http.StatusOK, "",
		map[string]any{"bytes": fi.Size(), "sha256": sum})
}
