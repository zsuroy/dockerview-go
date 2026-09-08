package server

import (
	"net/http"

	"github.com/zsuroy/dockerview-go/internal/netview"
)

// SetTopologyProvider injects the read-only topology provider
// (daemon-backed in production, fixture-backed under -no-docker). When not
// set, GET /api/networks/topology answers 503 instead of faking emptiness.
func (s *Server) SetTopologyProvider(p netview.Provider) {
	s.mu.Lock()
	s.topology = p
	s.mu.Unlock()
}

// handleNetworkTopology serves GET /api/networks/topology. It is read-only
// and open to guests, mirroring /data and GET /api/prune/candidates: there
// is deliberately no checkAuth call. Network delete/connect/disconnect have
// no corresponding route anywhere in this server.
func (s *Server) handleNetworkTopology(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTopologyError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
		return
	}
	s.mu.RLock()
	provider := s.topology
	s.mu.RUnlock()
	if provider == nil {
		writeTopologyError(w, http.StatusServiceUnavailable,
			"topology_unavailable",
			"topology backend not available (no Docker client, no -network-fixture)")
		return
	}
	topo, err := provider.Topology(r.Context())
	if err != nil || topo == nil {
		writeTopologyError(w, http.StatusInternalServerError,
			"topology_error", "failed to build network topology: "+errText(err))
		return
	}
	writeJSON(w, http.StatusOK, topo)
}

func errText(err error) string {
	if err == nil {
		return "no topology returned"
	}
	return err.Error()
}

func writeTopologyError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": message})
}
