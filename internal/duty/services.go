// Package duty implements the on-call "duty agent": Genkit tools that wrap
// existing dockerview data (container snapshot, logs, audit) plus a human
// confirmation latch for write operations.
//
// The package never imports the Docker client directly. Tools depend on the
// ToolServices interface, which the HTTP server implements over its existing
// snapshot and handlers. This keeps the tool layer unit-testable without a
// Docker daemon or a network connection.
package duty

import "context"

// ContainerBrief is the read-only container summary returned by listContainers.
// It carries only fields already present in docker.ContainerInfo.
type ContainerBrief struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	HealthScore  int    `json:"health_score"`
	HealthStatus string `json:"health_status"`
	CPU          string `json:"cpu"`
	Memory       string `json:"memory"`
}

// LogsResult is the tailLogs tool output.
type LogsResult struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Tail      int    `json:"tail"`
	Lines     string `json:"lines"`
	Truncated bool   `json:"truncated"`
}

// AuditResult is the recentAudit tool output.
type AuditResult struct {
	Total int64       `json:"total"`
	Items []AuditItem `json:"items"`
}

// AuditItem is a trimmed audit row for tool/LLM consumption.
type AuditItem struct {
	Time          string `json:"time"`
	Actor         string `json:"actor"`
	Source        string `json:"source"`
	Action        string `json:"action"`
	ContainerID   string `json:"container_id"`
	ContainerName string `json:"container_name"`
	Result        string `json:"result"`
	StatusCode    int    `json:"status_code"`
	Detail        string `json:"detail"`
	RequestID     string `json:"request_id"`
}

// PreviewResult is the previewRestart/previewStop output. It never executes
// anything; it only describes the container that would be affected.
type PreviewResult struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	HealthScore int    `json:"health_score"`
	Op          string `json:"op"`
	Impact      string `json:"impact"`
}

// ExecResult mirrors docker.ExecResult for the optional runExecTemplate tool.
type ExecResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

// ToolServices is the surface the duty tools need from the host. The HTTP
// server implements it; tests use a fake. Every method is read-only except
// ExecTemplate, which runs a whitelisted diagnostic command.
type ToolServices interface {
	// ListContainers returns the latest in-memory container snapshot.
	ListContainers(ctx context.Context) ([]ContainerBrief, error)
	// TailLogs returns up to tailLines of container logs, optionally filtered.
	TailLogs(ctx context.Context, id string, tailLines int, grep, level string) (LogsResult, error)
	// RecentAudit returns recent audit rows, optionally filtered by container.
	RecentAudit(ctx context.Context, container string, limit int) (AuditResult, error)
	// FindContainer resolves a 12-char short id or name prefix to a container.
	FindContainer(ctx context.Context, idOrName string) (ContainerBrief, bool, error)
	// ExecTemplate runs one of the whitelisted diagnostic templates.
	ExecTemplate(ctx context.Context, id, template string) (ExecResult, error)
}

// MaxLogTail is the hard cap on log lines a tool call may return.
const MaxLogTail = 200

// MaxAuditItems is the hard cap on audit rows a tool call may return.
const MaxAuditItems = 50

// ExecTemplates is the whitelist of commands runExecTemplate may execute.
// The keys are the only values the model may pass; the commands match the
// quick templates in the web ExecModal. The model can never supply a raw
// command string.
var ExecTemplates = map[string][]string{
	"dir_list":       {"ls", "-la"},
	"env_vars":       {"env"},
	"disk_usage":     {"df", "-h"},
	"current_user":   {"whoami"},
	"network_config": {"sh", "-c", "ip a || ifconfig"},
	"process_list":   {"sh", "-c", "ps aux || ps"},
}

// ExecTemplateKeys returns the whitelisted template keys. The order is
// non-deterministic; callers that need a stable list should sort the result.
func ExecTemplateKeys() []string {
	keys := make([]string, 0, len(ExecTemplates))
	for k := range ExecTemplates {
		keys = append(keys, k)
	}
	return keys
}
