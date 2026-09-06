package duty

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
)

// fakeConfig is the typed config for the fake model (unused but required by
// DefineModelAction's type parameter).
type fakeConfig struct{}

// registerFakeModel registers a scripted model that returns tool requests
// based on the question text. It is used when no API key is configured so
// the full tool loop can be verified without network access.
//
// Uses genkit.DefineModelAction (not the deprecated DefineModel).
// https://genkit.dev/docs/go/tool-calling/
func registerFakeModel(g *genkit.Genkit) {
	genkit.DefineModelAction[fakeConfig](g, "dockerview/fake",
		&ai.ModelOptions{
			Supports: &ai.ModelSupports{Tools: true, Multiturn: true, SystemRole: true},
		},
		func(ctx context.Context, req *ai.ModelRequest, _ fakeConfig, cb ai.ModelStreamCallback) (*ai.ModelResponse, error) {
			// Check if this is a continuation after tool results (messages contain
			// tool response parts). If so, return a final text answer.
			for _, msg := range req.Messages {
				if msg.Role == ai.RoleTool {
					return finalFakeAnswer(req), nil
				}
				for _, p := range msg.Content {
					if p.Kind == ai.PartToolResponse {
						return finalFakeAnswer(req), nil
					}
				}
			}

			// First turn: decide which tool to call based on the latest user message.
			prompt := strings.ToLower(collectUserText(req.Messages))
			toolReqs := decideFakeTools(prompt, req.Tools)
			if len(toolReqs) > 0 {
				var parts []*ai.Part
				for _, tr := range toolReqs {
					parts = append(parts, ai.NewToolRequestPart(tr))
				}
				return &ai.ModelResponse{
					Message: &ai.Message{Role: ai.RoleModel, Content: parts},
				}, nil
			}

			// No tool needed: answer directly (should not happen in normal use).
			return &ai.ModelResponse{
				Message: &ai.Message{
					Role:    ai.RoleModel,
					Content: []*ai.Part{ai.NewTextPart("(fake) no tool was needed")},
				},
			}, nil
		})
}

func decideFakeTools(prompt string, available []*ai.ToolDefinition) []*ai.ToolRequest {
	hasTool := func(name string) bool {
		for _, t := range available {
			if t.Name == name {
				return true
			}
		}
		return false
	}
	mkReq := func(name string, input any) *ai.ToolRequest {
		return &ai.ToolRequest{Name: name, Input: input}
	}

	var reqs []*ai.ToolRequest

	switch {
	case strings.Contains(prompt, "error") || strings.Contains(prompt, "502") || strings.Contains(prompt, "log"):
		if hasTool("listContainers") {
			reqs = append(reqs, mkReq("listContainers", map[string]any{}))
		}
		if hasTool("tailLogs") {
			reqs = append(reqs, mkReq("tailLogs", map[string]any{
				"id":   "api",
				"tail": 100,
				"grep": "ERROR",
			}))
		}
	case strings.Contains(prompt, "restart") || strings.Contains(prompt, "stop"):
		op := "previewRestart"
		if strings.Contains(prompt, "stop") {
			op = "previewStop"
		}
		if hasTool(op) {
			reqs = append(reqs, mkReq(op, map[string]any{"id": "api"}))
		}
	case strings.Contains(prompt, "audit") || strings.Contains(prompt, "who"):
		if hasTool("recentAudit") {
			reqs = append(reqs, mkReq("recentAudit", map[string]any{"limit": 10}))
		}
	default:
		if hasTool("listContainers") {
			reqs = append(reqs, mkReq("listContainers", map[string]any{}))
		}
	}
	return reqs
}

func finalFakeAnswer(req *ai.ModelRequest) *ai.ModelResponse {
	var evidence []string
	for _, msg := range req.Messages {
		for _, p := range msg.Content {
			if p.Kind == ai.PartToolResponse && p.ToolResponse != nil {
				out, _ := json.Marshal(p.ToolResponse.Output)
				evidence = append(evidence, string(out))
			}
		}
	}
	answer := "(fake/drill mode) Based on tool data: " + strings.Join(evidence, " | ")
	return &ai.ModelResponse{
		Message: &ai.Message{
			Role:    ai.RoleModel,
			Content: []*ai.Part{ai.NewTextPart(answer)},
		},
	}
}

// collectUserText returns only the last user message text, so the fake
// model's tool selection is not fooled by the system prompt containing
// words like "log".
func collectUserText(msgs []*ai.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != ai.RoleUser {
			continue
		}
		var b strings.Builder
		for _, p := range msgs[i].Content {
			if p.Text != "" {
				b.WriteString(p.Text)
				b.WriteString(" ")
			}
		}
		return b.String()
	}
	return ""
}
