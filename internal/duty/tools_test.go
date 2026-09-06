package duty

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeServices is an in-memory ToolServices for unit tests.
type fakeServices struct {
	containers []ContainerBrief
	logs       map[string]string // id -> log text
	audit      []AuditItem
	execCalls  int
}

func (f *fakeServices) ListContainers(ctx context.Context) ([]ContainerBrief, error) {
	return f.containers, nil
}

func (f *fakeServices) TailLogs(ctx context.Context, id string, tail int, grep, level string) (LogsResult, error) {
	text, ok := f.logs[id]
	if !ok {
		// resolve by name
		for _, c := range f.containers {
			if c.Name == id {
				text, ok = f.logs[c.ID]
			}
		}
	}
	if !ok {
		return LogsResult{}, errors.New("no logs for " + id)
	}
	lines := strings.Split(text, "\n")
	if len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	res := LogsResult{ID: id, Tail: tail, Lines: strings.Join(lines, "\n")}
	if grep != "" {
		var filtered []string
		for _, ln := range lines {
			if strings.Contains(strings.ToLower(ln), strings.ToLower(grep)) {
				filtered = append(filtered, ln)
			}
		}
		res.Lines = strings.Join(filtered, "\n")
	}
	return res, nil
}

func (f *fakeServices) RecentAudit(ctx context.Context, container string, limit int) (AuditResult, error) {
	var items []AuditItem
	for _, it := range f.audit {
		if container != "" && !strings.Contains(it.ContainerID, container) && !strings.Contains(strings.ToLower(it.ContainerName), strings.ToLower(container)) {
			continue
		}
		items = append(items, it)
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return AuditResult{Total: int64(len(items)), Items: items}, nil
}

func (f *fakeServices) FindContainer(ctx context.Context, idOrName string) (ContainerBrief, bool, error) {
	for _, c := range f.containers {
		if c.ID == idOrName || c.Name == idOrName || strings.HasPrefix(c.ID, idOrName) {
			return c, true, nil
		}
	}
	return ContainerBrief{}, false, nil
}

func (f *fakeServices) ExecTemplate(ctx context.Context, id, template string) (ExecResult, error) {
	f.execCalls++
	cmd, ok := ExecTemplates[template]
	if !ok {
		return ExecResult{}, errors.New("unknown template")
	}
	return ExecResult{ExitCode: 0, Stdout: "ran " + strings.Join(cmd, " ") + " in " + id}, nil
}

func fixtureServices() *fakeServices {
	return &fakeServices{
		containers: []ContainerBrief{
			{ID: "abc123456789", Name: "api", Status: "running", HealthScore: 42, HealthStatus: "dangerous", CPU: "12.3%", Memory: "210 MB"},
			{ID: "def987654321", Name: "web", Status: "running", HealthScore: 91, HealthStatus: "healthy", CPU: "4.1%", Memory: "88 MB"},
		},
		logs: map[string]string{
			"abc123456789": "INFO starting\nERROR upstream 502 from payments\nWARN retrying\nERROR connection refused\nINFO ok",
			"def987654321": "INFO healthy\nGET / 200",
		},
		audit: []AuditItem{
			{Time: "2026-08-27T10:00:00Z", Actor: "tok_abc", Source: "web", Action: "restart", ContainerID: "abc123456789", ContainerName: "api", Result: "success", StatusCode: 200, RequestID: "req_1"},
			{Time: "2026-08-27T09:00:00Z", Actor: "tok_def", Source: "web", Action: "stop", ContainerID: "def987654321", ContainerName: "web", Result: "success", StatusCode: 200, RequestID: "req_2"},
		},
	}
}

func TestListContainers(t *testing.T) {
	tools := NewTools(fixtureServices())
	got, err := tools.ListContainers(context.Background(), ListContainersInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 containers, got %d", len(got))
	}
	got, err = tools.ListContainers(context.Background(), ListContainersInput{Name: "api"})
	if err != nil || len(got) != 1 || got[0].Name != "api" {
		t.Fatalf("name filter failed: %+v err=%v", got, err)
	}
}

func TestTailLogsCapsAt200(t *testing.T) {
	f := fixtureServices()
	// build 500 lines
	var sb strings.Builder
	for i := 0; i < 500; i++ {
		sb.WriteString("line\n")
	}
	f.logs["abc123456789"] = sb.String()
	tools := NewTools(f)
	res, err := tools.TailLogs(context.Background(), TailLogsInput{ID: "abc123456789", Tail: 9999})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(strings.Split(res.Lines, "\n")); n != MaxLogTail {
		t.Fatalf("want %d lines after cap, got %d", MaxLogTail, n)
	}
}

func TestTailLogsGrepFindsError(t *testing.T) {
	tools := NewTools(fixtureServices())
	res, err := tools.TailLogs(context.Background(), TailLogsInput{ID: "abc123456789", Grep: "ERROR"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Lines, "ERROR upstream 502") {
		t.Fatalf("expected fixture ERROR line, got %q", res.Lines)
	}
}

func TestRecentAuditFiltersByContainer(t *testing.T) {
	tools := NewTools(fixtureServices())
	res, err := tools.RecentAudit(context.Background(), RecentAuditInput{Container: "api"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 || res.Items[0].Action != "restart" {
		t.Fatalf("want 1 restart for api, got %+v", res)
	}
}

func TestPreviewDoesNotWrite(t *testing.T) {
	f := fixtureServices()
	tools := NewTools(f)
	res, err := tools.PreviewRestart(context.Background(), PreviewInput{ID: "api"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Op != "restart" || res.Name != "api" {
		t.Fatalf("bad preview: %+v", res)
	}
	if f.execCalls != 0 {
		t.Fatal("preview must not execute anything")
	}
	// unknown container
	if _, err := tools.PreviewStop(context.Background(), PreviewInput{ID: "nope"}); err == nil {
		t.Fatal("expected error for unknown container")
	}
}

func TestRunExecTemplateRejectsUnknown(t *testing.T) {
	tools := NewTools(fixtureServices())
	if _, err := tools.RunExecTemplate(context.Background(), ExecTemplateInput{ID: "api", Template: "rm -rf /"}); err == nil {
		t.Fatal("unknown template must be rejected")
	}
	res, err := tools.RunExecTemplate(context.Background(), ExecTemplateInput{ID: "api", Template: "dir_list"})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("template run failed: %+v err=%v", res, err)
	}
}
