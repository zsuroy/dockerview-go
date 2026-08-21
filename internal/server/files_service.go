package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/zsuroy/dockerview-go/internal/audit"
	"github.com/zsuroy/dockerview-go/internal/filejail"
	"github.com/zsuroy/dockerview-go/internal/files"
)

// errTransferTooLarge signals the configured byte cap was exceeded.
var errTransferTooLarge = errors.New("transfer exceeds configured limit")

// tryFilesLock acquires the single-flight lock for one transfer key.
// Returns false when an identical transfer is in flight (caller 409s).
func (s *Server) tryFilesLock(key string) bool {
	s.filesOpMu.Lock()
	defer s.filesOpMu.Unlock()
	if s.filesInflight == nil {
		s.filesInflight = map[string]bool{}
	}
	if s.filesInflight[key] {
		return false
	}
	s.filesInflight[key] = true
	return true
}

func (s *Server) releaseFilesLock(key string) {
	s.filesOpMu.Lock()
	delete(s.filesInflight, key)
	s.filesOpMu.Unlock()
}

// SetFileCopier installs the container transfer backend (docker or mock).
func (s *Server) SetFileCopier(c files.Copier) {
	s.mu.Lock()
	s.copier = c
	s.mu.Unlock()
}

func (s *Server) fileCopier() files.Copier {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.copier
}

func writeFilesError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "message": msg})
}

// filesAuth is the files-feature auth gate. guestOK allows anonymous
// requests (downloads only, and only when configured); a present but
// wrong token is always 401.
func (s *Server) filesAuth(w http.ResponseWriter, r *http.Request, guestOK bool) (resolvedActor, bool) {
	if s.token == "" {
		return resolvedActor{}, true
	}
	tok := extractToken(r)
	if tok != "" && s.matchToken(tok) {
		return resolvedActor{token: tok}, true
	}
	if tok == "" && guestOK && s.getFilesSettings().cfg.AllowGuestDownload {
		return resolvedActor{token: ""}, true
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":   "unauthorized",
		"message": "Invalid or missing security token",
	})
	return resolvedActor{token: ""}, false
}

func (s *Server) recordFileEvent(r *http.Request, ra resolvedActor, action, cid, cpath, result string, status int, detail string, payload map[string]any) {
	actorV, kindV, sourceV, ipV, uaV := audit.ActorFromRequest(r, ra.token)
	if payload == nil {
		payload = map[string]any{}
	}
	payload["path"] = cpath
	s.aud().Record(r.Context(), audit.Event{
		Time:          time.Now(),
		Actor:         actorV,
		ActorKind:     kindV,
		Source:        sourceV,
		Action:        action,
		ContainerID:   cid,
		ContainerName: s.lookupName(cid),
		Result:        result,
		StatusCode:    status,
		Detail:        detail,
		ClientIP:      ipV,
		UserAgent:     uaV,
		RequestID:     r.Header.Get("X-Request-Id"),
		Payload:       payload,
	})
}

// newJail builds the path jail from current settings.
func (s *Server) newJail() (*filejail.Jail, error) {
	return filejail.New(s.getFilesSettings().cfg.JailRoot)
}

// ensureJailInContainer makes sure the mock backend has the jail directory
// inside its container stand-in (docker cp creates target parents; the mock
// needs the dir to exist for realpath checks).
func ensureJailInContainer(c files.Copier, cid, jailRoot string) {
	rootFS := c.RootFS(cid)
	if rootFS == "" {
		return
	}
	_ = os.MkdirAll(filepath.Join(rootFS, filepath.Clean("/"+jailRoot)), 0o750)
}

// stageFile creates a 0600 temp file inside the host staging dir
// ($DataRoot/files) — never /tmp, never the repo cwd.
func (s *Server) stageFile(pattern string) (*os.File, string, error) {
	dir := s.getFilesSettings().stageDir
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, "", err
	}
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, "", err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		os.Remove(f.Name())
		return nil, "", err
	}
	return f, f.Name(), nil
}

// limitedCopy copies at most max bytes; a stream longer than max yields
// errTransferTooLarge (one extra byte is read to detect overflow without
// buffering the whole payload).
func limitedCopy(dst io.Writer, src io.Reader, max int64) (int64, error) {
	n, err := io.Copy(dst, io.LimitReader(src, max+1))
	if err != nil {
		return n, err
	}
	if n > max {
		return n, errTransferTooLarge
	}
	return n, nil
}

var asciiNameSanitizer = strings.NewReplacer(
	"\\", "_", "/", "_", "\"", "_", "\x00", "_", " ", "_",
)

// sanitizeDownloadName produces a basename safe for Content-Disposition:
// no path separators, no control chars; an ASCII form plus the retained
// unicode form for the RFC5987 filename*.
func sanitizeDownloadName(p string) (ascii, utf8name string) {
	base := filepath.Base(filepath.ToSlash(p))
	var sb strings.Builder
	for _, r := range base {
		switch {
		case r == unicode.ReplacementChar || unicode.IsControl(r):
			sb.WriteByte('_')
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			sb.WriteRune(r)
		case r == '.' || r == '-' || r == '_':
			sb.WriteRune(r)
		default:
			sb.WriteByte('_')
		}
	}
	clean := strings.Trim(sb.String(), ".")
	if clean == "" || strings.Trim(clean, "_.-") == "" {
		clean = "dockerview-file"
	}
	ascii = asciiNameSanitizer.Replace(clean)
	if ascii == "" {
		ascii = "dockerview-file"
	}
	return ascii, clean
}

// setContentDisposition sets a header-injection-safe filename.
func setContentDisposition(w http.ResponseWriter, name string) {
	ascii, utf8name := sanitizeDownloadName(name)
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename=%q; filename*=UTF-8''%s`,
			ascii, url.PathEscape(utf8name)))
}
