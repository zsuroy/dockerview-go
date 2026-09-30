package wecom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/zsuroy/dockerview-go/internal/duty"
)

// GroupWebhook posts notifications to a WeCom group robot.
//
// This is the secondary path the brief asks for, and it is deliberately not
// the main one. A group robot webhook is a different transport: plain HTTP
// JSON, outbound only, no long connection, no subscription, no inbound
// messages at all. It cannot receive a question and it cannot execute
// anything — it only tells a room that a write is waiting for a human.
//
// It is off unless wecom.group_webhook_enabled is set and a URL is configured.
type GroupWebhook struct {
	url    string
	client *http.Client
}

// GroupWebhookTimeout bounds one webhook POST. The notification is advisory:
// on failure we log and move on rather than retrying into a queue.
const GroupWebhookTimeout = 5 * time.Second

// NewGroupWebhook builds the notifier, or returns nil when it is not enabled.
func NewGroupWebhook(cfg Config) (*GroupWebhook, error) {
	if !cfg.GroupWebhookEnabled {
		return nil, nil
	}
	u, err := cfg.ResolveGroupWebhookURL()
	if err != nil {
		return nil, err
	}
	if u == "" {
		return nil, nil
	}
	return &GroupWebhook{
		url:    u,
		client: &http.Client{Timeout: GroupWebhookTimeout},
	}, nil
}

// Enabled reports whether a webhook is configured.
func (g *GroupWebhook) Enabled() bool { return g != nil && g.url != "" }

// NotifyProposal posts a markdown notice about a proposed write.
func (g *GroupWebhook) NotifyProposal(ctx context.Context, p *duty.PreviewResult, question string) {
	if !g.Enabled() || p == nil {
		return
	}

	var b bytes.Buffer
	fmt.Fprintf(&b, "**dockerview 值班提醒**\n")
	fmt.Fprintf(&b, "有人提议执行写操作，需要到 Web 用管理员确认：\n")
	fmt.Fprintf(&b, "> 动作：%s\n> 容器：%s\n> ID：%s\n", p.Op, p.Name, p.ID)
	if question != "" {
		fmt.Fprintf(&b, "> 原话：%s\n", question)
	}
	fmt.Fprintf(&b, "机器人不会执行启停。")

	payload, err := json.Marshal(map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]string{"content": b.String()},
	})
	if err != nil {
		log.Printf("[WARN] wecom: marshal group webhook payload: %v", err)
		return
	}

	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, GroupWebhookTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.url, bytes.NewReader(payload))
	if err != nil {
		log.Printf("[WARN] wecom: build group webhook request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		log.Printf("[WARN] wecom: group webhook post: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("[WARN] wecom: group webhook returned %d", resp.StatusCode)
	}
}
