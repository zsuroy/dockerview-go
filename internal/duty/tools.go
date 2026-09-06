package duty

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Input structs define the JSON schema the model sees.
// Field tags carry jsonschema descriptions so Genkit can build the tool schema.

// ListContainersInput is the input for listContainers.
type ListContainersInput struct {
	Name string `json:"name,omitempty" jsonschema:"description=Optional case-insensitive substring filter on container name"`
}

// TailLogsInput is the input for tailLogs.
type TailLogsInput struct {
	ID    string `json:"id" jsonschema:"description=Container short id (12 chars) or name"`
	Tail  int    `json:"tail,omitempty" jsonschema:"description=Number of log lines to return (max 200)"`
	Grep  string `json:"grep,omitempty" jsonschema:"description=Case-insensitive substring filter"`
	Level string `json:"level,omitempty" jsonschema:"description=Log level filter: ERROR, WARN, INFO, DEBUG"`
}

// RecentAuditInput is the input for recentAudit.
type RecentAuditInput struct {
	Container string `json:"container,omitempty" jsonschema:"description=Container short id or name to filter by"`
	Limit     int    `json:"limit,omitempty" jsonschema:"description=Max rows to return (max 50)"`
}

// PreviewInput is the input for previewRestart / previewStop.
type PreviewInput struct {
	ID string `json:"id" jsonschema:"description=Container short id (12 chars) or name"`
}

// ExecTemplateInput is the input for runExecTemplate.
type ExecTemplateInput struct {
	ID       string `json:"id" jsonschema:"description=Container short id (12 chars) or name"`
	Template string `json:"template" jsonschema:"description=One of: dir_list, env_vars, disk_usage, current_user, network_config, process_list"`
}

// Tools bundles the read-only/preview tool functions. Each method is a plain
// function over ToolServices; the Genkit DefineTool wrappers live in agent.go.
type Tools struct {
	svc ToolServices
}

// NewTools constructs a Tools bound to the given services.
func NewTools(svc ToolServices) *Tools {
	return &Tools{svc: svc}
}

// ListContainers returns monitored containers, optionally filtered by name.
func (t *Tools) ListContainers(ctx context.Context, in ListContainersInput) ([]ContainerBrief, error) {
	all, err := t.svc.ListContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	if in.Name == "" {
		return all, nil
	}
	q := strings.ToLower(in.Name)
	var out []ContainerBrief
	for _, c := range all {
		if strings.Contains(strings.ToLower(c.Name), q) || strings.HasPrefix(strings.ToLower(c.ID), q) {
			out = append(out, c)
		}
	}
	return out, nil
}

// TailLogs returns the last log lines for a container, capped at MaxLogTail.
func (t *Tools) TailLogs(ctx context.Context, in TailLogsInput) (LogsResult, error) {
	if in.ID == "" {
		return LogsResult{}, fmt.Errorf("id is required")
	}
	tail := in.Tail
	if tail <= 0 || tail > MaxLogTail {
		tail = MaxLogTail
	}
	res, err := t.svc.TailLogs(ctx, in.ID, tail, in.Grep, in.Level)
	if err != nil {
		return LogsResult{}, fmt.Errorf("tail logs: %w", err)
	}
	return res, nil
}

// RecentAudit returns recent audit rows, optionally filtered by container.
func (t *Tools) RecentAudit(ctx context.Context, in RecentAuditInput) (AuditResult, error) {
	limit := in.Limit
	if limit <= 0 || limit > MaxAuditItems {
		limit = 20
	}
	res, err := t.svc.RecentAudit(ctx, in.Container, limit)
	if err != nil {
		return AuditResult{}, fmt.Errorf("recent audit: %w", err)
	}
	return res, nil
}

// PreviewRestart returns the container that a restart would affect, without
// executing anything.
func (t *Tools) PreviewRestart(ctx context.Context, in PreviewInput) (PreviewResult, error) {
	return t.preview(ctx, in.ID, "restart")
}

// PreviewStop returns the container that a stop would affect, without
// executing anything.
func (t *Tools) PreviewStop(ctx context.Context, in PreviewInput) (PreviewResult, error) {
	return t.preview(ctx, in.ID, "stop")
}

func (t *Tools) preview(ctx context.Context, idOrName, op string) (PreviewResult, error) {
	if idOrName == "" {
		return PreviewResult{}, fmt.Errorf("id is required")
	}
	c, ok, err := t.svc.FindContainer(ctx, idOrName)
	if err != nil {
		return PreviewResult{}, err
	}
	if !ok {
		return PreviewResult{}, fmt.Errorf("no container matches %q", idOrName)
	}
	return PreviewResult{
		ID:          c.ID,
		Name:        c.Name,
		Status:      c.Status,
		HealthScore: c.HealthScore,
		Op:          op,
		Impact:      fmt.Sprintf("Would %s container %q (%s, health %d). No action is taken until a human confirms in the web UI.", op, c.Name, c.Status, c.HealthScore),
	}, nil
}

// RunExecTemplate runs one of the whitelisted diagnostic templates. The
// template key must be in ExecTemplates; arbitrary commands are rejected.
func (t *Tools) RunExecTemplate(ctx context.Context, in ExecTemplateInput) (ExecResult, error) {
	if in.ID == "" {
		return ExecResult{}, fmt.Errorf("id is required")
	}
	if _, ok := ExecTemplates[in.Template]; !ok {
		keys := make([]string, 0, len(ExecTemplates))
		for k := range ExecTemplates {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return ExecResult{}, fmt.Errorf("unknown template %q; allowed: %s", in.Template, strings.Join(keys, ", "))
	}
	return t.svc.ExecTemplate(ctx, in.ID, in.Template)
}
