package server

import (
	"context"
	"io"
	"strconv"
	"strings"

	"github.com/zsuroy/dockerview-go/internal/audit"
	"github.com/zsuroy/dockerview-go/internal/docker"
	"github.com/zsuroy/dockerview-go/internal/duty"
)

// dutyServices implements duty.ToolServices over the server's existing
// snapshot, Docker client, and audit recorder. It is the only place the
// duty package touches Docker, and it reuses the same functions as the
// existing HTTP handlers.
type dutyServices struct {
	s *Server
}

// NewDutyServices creates a ToolServices implementation backed by the server.
func NewDutyServices(s *Server) duty.ToolServices {
	return &dutyServices{s: s}
}

func (d *dutyServices) ListContainers(ctx context.Context) ([]duty.ContainerBrief, error) {
	d.s.mu.RLock()
	data := d.s.currentData
	d.s.mu.RUnlock()

	out := make([]duty.ContainerBrief, 0, len(data))
	for _, c := range data {
		out = append(out, duty.ContainerBrief{
			ID:           c.ID,
			Name:         c.Name,
			Status:       c.Status,
			HealthScore:  c.HealthScore,
			HealthStatus: string(c.HealthStatus),
			CPU:          c.CPU,
			Memory:       c.Memory,
		})
	}
	return out, nil
}

func (d *dutyServices) TailLogs(ctx context.Context, id string, tailLines int, grep, level string) (duty.LogsResult, error) {
	d.s.mu.RLock()
	cli := d.s.dockerClient
	data := d.s.currentData
	d.s.mu.RUnlock()

	if cli == nil {
		// No Docker client (e.g. -no-docker drill mode): return empty logs
		// so the agent can still answer list/audit questions in fake mode.
		return duty.LogsResult{ID: id, Name: id, Lines: ""}, nil
	}

	// Resolve id to full container id using the snapshot.
	fullID := resolveContainerID(data, id)
	if fullID == "" {
		return duty.LogsResult{}, errDutyContainerNotFound(id)
	}
	name := lookupNameFromData(data, fullID)

	tail := "200"
	if tailLines > 0 && tailLines <= 200 {
		tail = strings.TrimSpace(strconv.Itoa(tailLines))
	}

	reader, err := docker.GetContainerLogs(ctx, cli, fullID, tail)
	if err != nil {
		return duty.LogsResult{}, err
	}
	defer reader.Close()

	logsBytes, err := io.ReadAll(reader)
	if err != nil {
		return duty.LogsResult{}, err
	}
	filtered := filterLogs(string(logsBytes), grep, level)

	shortID := fullID
	if len(shortID) > 12 {
		shortID = shortID[:12]
	}
	return duty.LogsResult{
		ID:    shortID,
		Name:  name,
		Tail:  tailLines,
		Lines: filtered,
	}, nil
}

func (d *dutyServices) RecentAudit(ctx context.Context, container string, limit int) (duty.AuditResult, error) {
	q := audit.Query{
		Limit:        limit,
		SortTimeDesc: true,
	}
	if container != "" {
		q.ContainerID = container
	}
	page, err := d.s.aud().List(ctx, q)
	if err != nil {
		return duty.AuditResult{}, err
	}
	items := make([]duty.AuditItem, 0, len(page.Items))
	for _, it := range page.Items {
		items = append(items, duty.AuditItem{
			Time:          it.Time,
			Actor:         it.Actor,
			Source:        it.Source,
			Action:        it.Action,
			ContainerID:   it.ContainerID,
			ContainerName: it.ContainerName,
			Result:        it.Result,
			StatusCode:    it.StatusCode,
			Detail:        it.Detail,
			RequestID:     it.RequestID,
		})
	}
	return duty.AuditResult{Total: page.Total, Items: items}, nil
}

func (d *dutyServices) FindContainer(ctx context.Context, idOrName string) (duty.ContainerBrief, bool, error) {
	d.s.mu.RLock()
	data := d.s.currentData
	d.s.mu.RUnlock()

	for _, c := range data {
		if c.ID == idOrName || c.Name == idOrName ||
			strings.HasPrefix(c.FullID, idOrName) || strings.HasPrefix(c.ID, idOrName) {
			return duty.ContainerBrief{
				ID:           c.ID,
				Name:         c.Name,
				Status:       c.Status,
				HealthScore:  c.HealthScore,
				HealthStatus: string(c.HealthStatus),
				CPU:          c.CPU,
				Memory:       c.Memory,
			}, true, nil
		}
	}
	return duty.ContainerBrief{}, false, nil
}

func (d *dutyServices) ExecTemplate(ctx context.Context, id, template string) (duty.ExecResult, error) {
	d.s.mu.RLock()
	cli := d.s.dockerClient
	data := d.s.currentData
	d.s.mu.RUnlock()

	if cli == nil {
		return duty.ExecResult{}, errDutyDockerUnavailable
	}
	cmd, ok := duty.ExecTemplates[template]
	if !ok {
		return duty.ExecResult{}, errDutyUnknownTemplate(template)
	}
	fullID := resolveContainerID(data, id)
	if fullID == "" {
		return duty.ExecResult{}, errDutyContainerNotFound(id)
	}
	res, err := docker.ContainerExec(ctx, cli, fullID, cmd)
	if err != nil {
		return duty.ExecResult{}, err
	}
	return duty.ExecResult{
		ExitCode: res.ExitCode,
		Stdout:   res.Stdout,
		Stderr:   res.Stderr,
	}, nil
}
