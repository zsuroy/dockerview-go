package server

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/client"
	"github.com/zsuroy/dockerview-go/internal/docker"
	"github.com/zsuroy/dockerview-go/internal/version"
)

//go:embed all:web
var webContent embed.FS

// Server implements an HTTP server that forwards Docker container data.
type Server struct {
	mu             sync.RWMutex
	clients        map[chan []docker.ContainerInfo]bool
	currentData    []docker.ContainerInfo
	dashboard      []byte
	dockerClient   *client.Client
	pruner         *docker.Pruner
	audit          *auditLog
	token          string
	currentVersion string
	commit         string
	buildDate      string
	upgradeMu      sync.Mutex
	upgradeRunning bool
}

// NewServer creates a new Server instance.
func NewServer(cli *client.Client, token string, currentVersion, commit, buildDate string) *Server {
	data, _ := webContent.ReadFile("web/index.html")
	var pruner *docker.Pruner
	if cli != nil {
		pruner = docker.NewPruner(cli)
	}
	return &Server{
		clients:        make(map[chan []docker.ContainerInfo]bool),
		dashboard:      data,
		dockerClient:   cli,
		pruner:         pruner,
		audit:          newAuditLog(),
		token:          token,
		currentVersion: currentVersion,
		commit:         commit,
		buildDate:      buildDate,
	}
}

// secureEqual compares two strings in constant time. It returns false early when
// lengths differ (the length of the configured token is not secret).
func secureEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// checkAuth checks if the request is authenticated.
func (s *Server) checkAuth(w http.ResponseWriter, r *http.Request) bool {
	if s.token == "" {
		return true // No security configured
	}

	// 1. Check query param
	token := r.URL.Query().Get("token")
	if secureEqual(token, s.token) {
		return true
	}

	// 2. Check header X-Auth-Token
	if secureEqual(r.Header.Get("X-Auth-Token"), s.token) {
		return true
	}

	// 3. Check Authorization Bearer header
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		if secureEqual(strings.TrimPrefix(authHeader, "Bearer "), s.token) {
			return true
		}
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte("Unauthorized: Invalid or missing security token"))
	return false
}

// UpdateData updates the current container data and broadcasts it to all connected clients.
func (s *Server) UpdateData(data []docker.ContainerInfo) {
	s.mu.Lock()
	s.currentData = data
	// Copy clients to a slice to avoid holding the lock during send
	var clients []chan []docker.ContainerInfo
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.Unlock()

	for _, clientChan := range clients {
		select {
		case clientChan <- data:
		default:
			// Client slow, skip update for this client
		}
	}
}

// Handler returns the HTTP handler for the server, including CORS wrapping and
// all registered routes. It is used by Start and is also useful for tests.
func (s *Server) Handler() http.Handler {
	webFS, _ := fs.Sub(webContent, "web")
	fileServer := http.FileServer(http.FS(webFS))

	mux := http.NewServeMux()
	mux.HandleFunc("/stream", s.handleStream)
	mux.HandleFunc("/data", s.handleData)
	mux.HandleFunc("/dashboard", s.handleDashboard)
	mux.HandleFunc("/api/container/op", s.handleContainerOp)
	mux.HandleFunc("/api/container/logs", s.handleContainerLogs)
	mux.HandleFunc("/api/container/exec", s.handleContainerExec)
	mux.HandleFunc("/api/version", s.handleVersion)
	mux.HandleFunc("/api/upgrade", s.handleUpgrade)
	mux.HandleFunc("/api/prune/candidates", s.handlePruneCandidates)
	mux.HandleFunc("/api/prune/dry-run", s.handlePruneDryRun)
	mux.HandleFunc("/api/prune/confirm", s.handlePruneConfirm)
	mux.HandleFunc("/api/prune/audit", s.handlePruneAudit)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Try to serve static file; if not found, fall back to index.html (SPA)
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" || path == "dashboard" {
			s.handleDashboard(w, r)
			return
		}
		// Check if file exists in embedded FS
		if f, err := webFS.(fs.ReadFileFS).Open(path); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// SPA fallback
		s.handleDashboard(w, r)
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Auth-Token, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// Start starts the HTTP server on the specified port.
func (s *Server) Start(ctx context.Context, port int) error {
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: s.Handler(),
	}

	errChan := make(chan error, 1)
	go func() {
		errChan <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		return server.Shutdown(context.Background())
	case err := <-errChan:
		return err
	}
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if s.dashboard == nil {
		http.Error(w, "Dashboard not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Write(s.dashboard)
}

func (s *Server) handleData(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	data := s.currentData
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if data == nil {
		data = []docker.ContainerInfo{}
	}
	json.NewEncoder(w).Encode(data)
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	clientChan := make(chan []docker.ContainerInfo, 5)
	s.mu.Lock()
	s.clients[clientChan] = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, clientChan)
		s.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Send initial data if available
	s.mu.RLock()
	initialData := s.currentData
	s.mu.RUnlock()
	if initialData != nil {
		if err := sendSSE(w, initialData); err != nil {
			return
		}
		flusher.Flush()
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case data := <-clientChan:
			if err := sendSSE(w, data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func sendSSE(w http.ResponseWriter, data []docker.ContainerInfo) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", jsonData)
	return err
}

func (s *Server) handleContainerOp(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	op := r.URL.Query().Get("op")
	if id == "" || op == "" {
		http.Error(w, "Missing 'id' or 'op' parameter", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	cli := s.dockerClient
	s.mu.RUnlock()

	if cli == nil {
		http.Error(w, "Docker client not available", http.StatusServiceUnavailable)
		return
	}

	err := docker.ContainerOp(r.Context(), cli, id, op)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to perform operation: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success", "op": op})
}

func (s *Server) handleContainerLogs(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	tail := r.URL.Query().Get("tail")
	grep := r.URL.Query().Get("grep")
	level := r.URL.Query().Get("level")

	if id == "" {
		http.Error(w, "Missing 'id' parameter", http.StatusBadRequest)
		return
	}

	// Validate and normalize tail parameter
	validTails := map[string]bool{"100": true, "500": true, "1000": true, "5000": true}
	if tail == "" {
		tail = "100"
	} else if !validTails[tail] {
		tail = "100"
	}

	s.mu.RLock()
	cli := s.dockerClient
	s.mu.RUnlock()

	if cli == nil {
		http.Error(w, "Docker client not available", http.StatusServiceUnavailable)
		return
	}

	reader, err := docker.GetContainerLogs(r.Context(), cli, id, tail)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get logs: %v", err), http.StatusInternalServerError)
		return
	}
	defer reader.Close()

	// Read all logs into memory for filtering
	logsBytes, err := io.ReadAll(reader)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read logs: %v", err), http.StatusInternalServerError)
		return
	}

	// Apply filters
	filtered := filterLogs(string(logsBytes), grep, level)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(filtered))
}

// filterLogs applies grep (keyword) and level filters to log content.
func filterLogs(logs string, grep string, level string) string {
	if grep == "" && level == "" {
		return logs
	}

	lines := strings.Split(logs, "\n")
	var filtered []string

	// Build level keyword map for matching
	levelKeywords := map[string][]string{
		"ERROR": {"ERROR", "error", "Error", "ERR", "err", "FATAL", "fatal"},
		"WARN":  {"WARN", "warn", "WARN", "Warning", "warning"},
		"INFO":  {"INFO", "info", "Info"},
		"DEBUG": {"DEBUG", "debug", "Debug"},
	}

	keywords := levelKeywords[strings.ToUpper(level)]

	for _, line := range lines {
		// Level filter
		if level != "" && keywords != nil {
			matched := false
			for _, kw := range keywords {
				if strings.Contains(line, kw) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		// Grep filter (case-insensitive)
		if grep != "" {
			if !strings.Contains(strings.ToLower(line), strings.ToLower(grep)) {
				continue
			}
		}

		filtered = append(filtered, line)
	}

	return strings.Join(filtered, "\n")
}

// handleVersion returns version info including latest available version.
// GET /api/version - no auth required
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	info := version.GetInfo(r.Context(), s.currentVersion, s.commit, s.buildDate)
	json.NewEncoder(w).Encode(info)
}

// handleUpgrade performs the upgrade and streams progress via SSE.
// GET /api/upgrade?token=...  (SSE streaming, EventSource compatible)
func (s *Server) handleUpgrade(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}

	// Allow GET for SSE (EventSource only supports GET)
	if r.Method != http.MethodGet {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Prevent concurrent upgrades
	s.upgradeMu.Lock()
	if s.upgradeRunning {
		s.upgradeMu.Unlock()
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": "Upgrade already in progress"})
		return
	}
	s.upgradeRunning = true
	s.upgradeMu.Unlock()

	defer func() {
		s.upgradeMu.Lock()
		s.upgradeRunning = false
		s.upgradeMu.Unlock()
	}()

	flusher, ok := w.(http.Flusher)
	if !ok {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)

	// clientGone is closed when the SSE client disconnects.
	clientGone := make(chan struct{})
	go func() {
		<-r.Context().Done()
		close(clientGone)
	}()

	var writeMu sync.Mutex
	sendUpgradeEvent := func(status, message string) {
		select {
		case <-clientGone:
			return // Client disconnected, stop sending
		default:
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		data, _ := json.Marshal(map[string]string{"status": status, "message": message})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	method := version.DetectInstallMethod()
	// Use a detached context with timeout for the upgrade so that
	// client disconnection does not interrupt a binary replacement.
	upgradeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	version.DoUpgrade(upgradeCtx, method, sendUpgradeEvent)
}

func (s *Server) handleContainerExec(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing 'id' parameter", http.StatusBadRequest)
		return
	}

	var reqBody struct {
		Cmd interface{} `json:"cmd"`
	}

	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var cmd []string
	switch v := reqBody.Cmd.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			http.Error(w, "Empty command", http.StatusBadRequest)
			return
		}
		cmd = []string{"sh", "-c", v}
	case []interface{}:
		for _, item := range v {
			if str, ok := item.(string); ok {
				if strings.TrimSpace(str) != "" {
					cmd = append(cmd, str)
				}
			}
		}
	default:
		http.Error(w, "Invalid 'cmd' parameter (must be string or array of strings)", http.StatusBadRequest)
		return
	}

	if len(cmd) == 0 {
		http.Error(w, "Empty command", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	cli := s.dockerClient
	s.mu.RUnlock()

	if cli == nil {
		http.Error(w, "Docker client not available", http.StatusServiceUnavailable)
		return
	}

	result, err := docker.ContainerExec(r.Context(), cli, id, cmd)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to execute command: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)
}
