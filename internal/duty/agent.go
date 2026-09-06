// agent.go wires the Genkit runtime: Init, DefineTool, and Generate.
//
// Official Genkit Go API references (v1.12.0, go.mod-locked):
//   - Get started: https://genkit.dev/docs/go/get-started/
//   - Tool calling: https://genkit.dev/docs/go/tool-calling/
//   - OpenAI plugin: https://genkit.dev/docs/go/integrations/openai/
//   - OpenAI-compatible plugin: https://genkit.dev/docs/go/integrations/openai-compatible/
//
// The default model is OpenAI-compatible (not Gemini). BaseURL and model
// name are configurable. When no API key is present, a scripted fake model
// is registered via genkit.DefineModelAction so the full tool loop can be
// verified without network access.
package duty

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
	"github.com/firebase/genkit/go/plugins/compat_oai"
	"github.com/openai/openai-go/option"
)

// Agent owns the Genkit runtime, the tool wrappers, and the ticket store.
type Agent struct {
	g      *genkit.Genkit
	tools  *Tools
	cfg    Config
	store  *Store
	traces *traceRecorder

	listContainers *ai.ToolAction[ListContainersInput, []ContainerBrief]
	tailLogs       *ai.ToolAction[TailLogsInput, LogsResult]
	recentAudit    *ai.ToolAction[RecentAuditInput, AuditResult]
	previewRestart *ai.ToolAction[PreviewInput, PreviewResult]
	previewStop    *ai.ToolAction[PreviewInput, PreviewResult]
	runExecTmpl    *ai.ToolAction[ExecTemplateInput, ExecResult]

	modelName string
	mode      string
}

// NewAgent initializes Genkit, registers the model (OpenAI-compatible or
// fake), and defines all tools. The caller must call Close when done.
func NewAgent(ctx context.Context, cfg Config, svc ToolServices, store *Store) (*Agent, error) {
	key, err := cfg.ResolveKey()
	if err != nil {
		return nil, err
	}

	a := &Agent{
		tools:  NewTools(svc),
		cfg:    cfg,
		store:  store,
		traces: newTraceRecorder(),
	}

	if key != "" {
		// OpenAI-compatible plugin: BaseURL and model name are configurable.
		// The plugin implements ResolveAction so any "dockerview/<model>"
		// reference is resolved dynamically without explicit registration.
		// https://genkit.dev/docs/go/integrations/openai-compatible/
		compat := &compat_oai.OpenAICompatible{
			Provider: "dockerview",
			APIKey:   key,
			BaseURL:  cfg.BaseURL,
			Opts:     []option.RequestOption{},
		}
		a.modelName = "dockerview/" + cfg.Model
		a.mode = "live"
		a.g = genkit.Init(ctx,
			genkit.WithPlugins(compat),
			genkit.WithDefaultModel(a.modelName),
		)
	} else {
		// No key: register a scripted fake model for drill/eval mode.
		a.modelName = "dockerview/fake"
		a.mode = "fake"
		a.g = genkit.Init(ctx, genkit.WithDefaultModel(a.modelName))
		registerFakeModel(a.g)
	}

	a.defineTools()
	log.Printf("[INFO] duty agent initialized: mode=%s model=%s base_url=%s", a.mode, a.modelName, cfg.BaseURL)
	return a, nil
}

// Close releases Genkit resources.
func (a *Agent) Close() error { return nil }

// Mode returns "live" or "fake".
func (a *Agent) Mode() string { return a.mode }

// ModelName returns the active model reference.
func (a *Agent) ModelName() string { return a.modelName }

// BaseURL returns the configured endpoint.
func (a *Agent) BaseURL() string { return a.cfg.BaseURL }

// Store returns the duty ticket store (may be nil).
func (a *Agent) Store() *Store { return a.store }

func (a *Agent) defineTools() {
	// genkit.DefineTool: https://genkit.dev/docs/go/tool-calling/
	a.listContainers = genkit.DefineTool(a.g, "listContainers",
		"List monitored Docker containers with id, name, status, and health score.",
		func(ctx *ai.ToolContext, in ListContainersInput) ([]ContainerBrief, error) {
			out, err := a.tools.ListContainers(ctx, in)
			a.traces.record("listContainers", in, summarizeContainers(out))
			return out, err
		})

	a.tailLogs = genkit.DefineTool(a.g, "tailLogs",
		"Return the last log lines (max 200) for a container. Use to find errors or 502s.",
		func(ctx *ai.ToolContext, in TailLogsInput) (LogsResult, error) {
			out, err := a.tools.TailLogs(ctx, in)
			a.traces.record("tailLogs", in, truncate(out.Lines, 500))
			return out, err
		})

	a.recentAudit = genkit.DefineTool(a.g, "recentAudit",
		"Return recent audit events (who started/stopped/restarted what), optionally filtered by container.",
		func(ctx *ai.ToolContext, in RecentAuditInput) (AuditResult, error) {
			out, err := a.tools.RecentAudit(ctx, in)
			a.traces.record("recentAudit", in, summarizeAudit(out))
			return out, err
		})

	a.previewRestart = genkit.DefineTool(a.g, "previewRestart",
		"Preview which container would be restarted. Does NOT restart anything; a human must confirm in the web UI.",
		func(ctx *ai.ToolContext, in PreviewInput) (PreviewResult, error) {
			out, err := a.tools.PreviewRestart(ctx, in)
			a.traces.record("previewRestart", in, out.Impact)
			return out, err
		})

	a.previewStop = genkit.DefineTool(a.g, "previewStop",
		"Preview which container would be stopped. Does NOT stop anything; a human must confirm in the web UI.",
		func(ctx *ai.ToolContext, in PreviewInput) (PreviewResult, error) {
			out, err := a.tools.PreviewStop(ctx, in)
			a.traces.record("previewStop", in, out.Impact)
			return out, err
		})

	a.runExecTmpl = genkit.DefineTool(a.g, "runExecTemplate",
		"Run a whitelisted diagnostic command in a container (dir_list, env_vars, disk_usage, etc.). No arbitrary shell.",
		func(ctx *ai.ToolContext, in ExecTemplateInput) (ExecResult, error) {
			out, err := a.tools.RunExecTemplate(ctx, in)
			a.traces.record("runExecTemplate", in, truncate(out.Stdout, 300))
			return out, err
		})
}

func summarizeContainers(cs []ContainerBrief) string {
	var b strings.Builder
	for i, c := range cs {
		if i >= 5 {
			fmt.Fprintf(&b, " …(+%d more)", len(cs)-5)
			break
		}
		fmt.Fprintf(&b, "%s/%s(%s,health=%d) ", c.ID, c.Name, c.Status, c.HealthScore)
	}
	return strings.TrimSpace(b.String())
}

func summarizeAudit(r AuditResult) string {
	var b strings.Builder
	for i, it := range r.Items {
		if i >= 5 {
			fmt.Fprintf(&b, " …(+%d more)", len(r.Items)-5)
			break
		}
		fmt.Fprintf(&b, "%s %s %s %s; ", it.Time, it.Action, it.ContainerName, it.Result)
	}
	return strings.TrimSpace(b.String())
}

// AskResult is the outcome of one duty inquiry.
type AskResult struct {
	Answer        string         `json:"answer"`
	ToolTraces    []ToolTrace    `json:"tool_traces"`
	TicketID      int64          `json:"ticket_id"`
	ProposedWrite *PreviewResult `json:"proposed_write,omitempty"`
}

// systemPrompt instructs the model to use tools and cite evidence.
const systemPrompt = `You are a dockerview on-call assistant. You have tools to inspect containers, logs, and audit history.

Rules:
1. ALWAYS call a tool before answering. Never invent CPU, memory, or log content.
2. Cite evidence in your answer: a container id/name, a log line substring, or an audit timestamp.
3. For restart/stop requests, call previewRestart or previewStop, then tell the user a human must confirm in the web UI. You cannot perform the write yourself.
4. Keep answers concise and in the language of the question.
5. If a tool returns an error, report it honestly.`

// Ask runs one inquiry through Genkit Generate, collects tool traces, and
// persists a duty ticket.
func (a *Agent) Ask(ctx context.Context, question, actor, actorKind, source string) (*AskResult, error) {
	a.traces.reset()

	// genkit.Generate: https://genkit.dev/docs/go/tool-calling/
	// WithMaxTurns prevents infinite tool-call loops.
	resp, err := genkit.Generate(ctx, a.g,
		ai.WithSystem(systemPrompt),
		ai.WithPrompt(question),
		ai.WithMaxTurns(8),
		ai.WithTools(
			a.listContainers, a.tailLogs, a.recentAudit,
			a.previewRestart, a.previewStop, a.runExecTmpl,
		),
	)
	if err != nil {
		return nil, fmt.Errorf("duty generate: %w", err)
	}

	answer := ""
	if resp.Message != nil {
		answer = resp.Message.Text()
	}

	traces := a.traces.snapshot()

	// Detect a proposed write from preview tool traces.
	var proposed *PreviewResult
	for _, tr := range traces {
		if tr.Tool != "previewRestart" && tr.Tool != "previewStop" {
			continue
		}
		var in PreviewInput
		if err := json.Unmarshal([]byte(tr.Input), &in); err != nil || in.ID == "" {
			continue
		}
		op := "restart"
		if tr.Tool == "previewStop" {
			op = "stop"
		}
		if pr, err := a.tools.preview(ctx, in.ID, op); err == nil {
			proposed = &pr
		}
	}

	// Build evidence summary for the ticket.
	ticket := &Ticket{
		Actor:       actor,
		ActorKind:   actorKind,
		Source:      source,
		Question:    question,
		Conclusion:  answer,
		ToolSummary: traces,
	}
	if proposed != nil {
		ticket.RelatedContainer = proposed.ID
	}
	if a.store != nil {
		if err := a.store.Insert(ctx, ticket); err != nil {
			log.Printf("[WARN] duty: insert ticket: %v", err)
		}
	}

	return &AskResult{
		Answer:        answer,
		ToolTraces:    traces,
		TicketID:      ticket.ID,
		ProposedWrite: proposed,
	}, nil
}

// traceRecorder collects tool calls during one Generate call.
type traceRecorder struct {
	mu     sync.Mutex
	traces []ToolTrace
}

func newTraceRecorder() *traceRecorder { return &traceRecorder{} }

func (r *traceRecorder) reset() {
	r.mu.Lock()
	r.traces = nil
	r.mu.Unlock()
}

func (r *traceRecorder) record(tool string, input any, output string) {
	in, _ := json.Marshal(input)
	r.mu.Lock()
	r.traces = append(r.traces, ToolTrace{
		Tool:          tool,
		Input:         string(in),
		OutputExcerpt: truncate(output, 500),
	})
	r.mu.Unlock()
}

func (r *traceRecorder) snapshot() []ToolTrace {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ToolTrace, len(r.traces))
	copy(out, r.traces)
	return out
}

// truncate shortens a string for ticket storage.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
