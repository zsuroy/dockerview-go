package server

import (
	"encoding/json"
	"net/http"

	"github.com/zsuroy/dockerview-go/internal/config"
)

// handleFilesConfig reports the resolved file-transfer settings so the UI
// can show the real jail root/quotas instead of hardcoded assumptions.
func (s *Server) handleFilesConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeFilesError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
		return
	}
	st := s.getFilesSettings()
	writeJSON(w, http.StatusOK, map[string]any{
		"jail_root":          st.cfg.JailRoot,
		"max_file_bytes":     st.cfg.MaxFileBytes,
		"max_archive_bytes":  st.cfg.MaxArchiveBytes,
		"guest_download":     st.cfg.AllowGuestDownload,
		"backend_configured": s.fileCopier() != nil,
	})
}

// handleFilesNotFound turns unknown /api/files/* routes into a JSON 404
// instead of falling through to the SPA dashboard HTML.
func (s *Server) handleFilesNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":   "not_found",
		"message": "unknown files API route",
	})
}

// filesSettings holds the resolved file-transfer settings on the server.
// Routes are wired in later phases; this setter keeps the config plumbing
// independent of the transfer implementation.
type filesSettings struct {
	cfg      config.FilesConfig
	stageDir string
}

// SetFilesConfig installs the in-container jail config and the host-side
// staging directory ($DataRoot/files).
func (s *Server) SetFilesConfig(fc config.FilesConfig, stageDir string) {
	s.mu.Lock()
	s.files = filesSettings{cfg: fc, stageDir: stageDir}
	s.mu.Unlock()
}

func (s *Server) getFilesSettings() filesSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.files
}
