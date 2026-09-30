// Package wecom connects dockerview's DUTY assistant to a WeCom (企业微信)
// smart robot over the official WebSocket long connection.
//
// Official references. Page titles are verbatim:
//
//   - 智能机器人长连接 — https://developer.work.weixin.qq.com/document/path/101463
//     Title: "智能机器人长连接 - 文档 - 企业微信开发者中心" (page last updated 2026/05/25).
//     Source for: endpoint wss://openws.work.weixin.qq.com, the aibot_subscribe
//     handshake, the one-connection-per-robot rule, the 30s heartbeat advice,
//     the 5s welcome-reply deadline, the 10-minute stream window, and the
//     30 msg/min + 1000 msg/hour per-conversation ceiling.
//
//   - 概述 — https://developer.work.weixin.qq.com/document/path/101785
//     Title: "概述 - 文档 - 企业微信开发者中心" (page last updated 2026/08/15).
//     Source for: the three-part shape (消息服务 / Agent / CLI-MCP) and the fact
//     that Bot ID and Secret come from the admin console.
//
//   - SDK README — https://github.com/go-sphere/wecom-aibot-go-sdk
//     Title: "wecom-aibot-go-sdk" (master @ 5258cea, tag v1.0.5).
//     Source for: NewWSClient / Connect / OnMessageText / ReplyStream /
//     ReplyWelcome / SendMarkdown, the injectable WSURL and WsDialer, and the
//     built-in heartbeat + exponential-backoff reconnect.
//
// Transport is github.com/go-sphere/wecom-aibot-go-sdk. This package does not
// implement framing, heartbeat, or reconnect — the SDK owns all of that.
//
// The brain is duty.Agent.Ask, reached through the Asker interface. This
// package never performs a container write: the only path to docker.ContainerOp
// stays POST /api/duty/confirm, gated on an admin token.
package wecom

import (
	"fmt"
	"os"
	"strings"
)

// Mode is the transport mode resolved from configuration.
type Mode string

const (
	// ModeOff means the integration is disabled; nothing is constructed.
	ModeOff Mode = "off"
	// ModeMock means credentials are absent. The SDK client object is built
	// but Connect is never called, so no socket is opened. Inbound frames are
	// fed through the SDK's own emitter instead.
	ModeMock Mode = "mock"
	// ModeLive means both BotID and Secret are present; Connect is called.
	ModeLive Mode = "live"
)

// ReplyMode selects how an answer leaves the process.
type ReplyMode string

const (
	// ReplyStream uses WSClient.ReplyStream with finish=true (one frame).
	ReplyStream ReplyMode = "stream"
	// ReplyMarkdown uses WSClient.SendMarkdown (proactive push channel).
	ReplyMarkdown ReplyMode = "markdown"
)

// DefaultReplyMode is the single-shot stream reply.
const DefaultReplyMode = ReplyStream

// DefaultMaxTranscript bounds the in-memory conversation log. The durable
// record lives in duty.db; this is only the console's "what just happened".
const DefaultMaxTranscript = 200

// DefaultAskTimeout bounds one Ask call. The HTTP handler uses 60s; the long
// connection uses the same budget so behaviour does not drift between paths.
const DefaultAskTimeoutSeconds = 60

// Config controls the WeCom smart-robot long connection.
type Config struct {
	// Enabled turns the whole integration on. Off by default.
	Enabled bool
	// BotID is the robot identifier from the WeCom admin console.
	BotID string
	// Secret is the long-connection key. It never comes from yaml; the config
	// layer fills it from WECOM_BOT_SECRET or leaves it empty for SecretFile.
	Secret string
	// SecretFile is a path to a 0600 file holding the Secret on one line.
	SecretFile string
	// WSURL overrides the endpoint. Empty means the SDK default,
	// wss://openws.work.weixin.qq.com. Tests point this at a local server.
	WSURL string
	// ReplyMode is "stream" (default) or "markdown".
	ReplyMode string
	// Welcome is the text sent on enter_chat. Entered users must be answered
	// within 5 seconds (101463), so this is a fixed local string — it never
	// goes through Ask.
	Welcome string
	// MaxTranscript is the ring-buffer size for the console.
	MaxTranscript int
	// AskTimeoutSeconds bounds one Ask call.
	AskTimeoutSeconds int

	// GroupWebhookEnabled turns on the optional group-robot webhook. That is a
	// different transport (plain HTTP JSON) and it is outbound-only; it never
	// receives. Disabled by default and never the primary path.
	GroupWebhookEnabled bool
	// GroupWebhookURL is the group robot's webhook endpoint.
	GroupWebhookURL string
	// GroupWebhookURLFile is a path to a file holding the webhook URL.
	GroupWebhookURLFile string
}

// DefaultConfig returns the built-in defaults.
func DefaultConfig() Config {
	return Config{
		ReplyMode:         string(DefaultReplyMode),
		Welcome:           DefaultWelcome,
		MaxTranscript:     DefaultMaxTranscript,
		AskTimeoutSeconds: DefaultAskTimeoutSeconds,
	}
}

// DefaultWelcome is the enter_chat greeting.
const DefaultWelcome = "我是 dockerview 值班助手。可以问我容器状态、日志和审计记录。" +
	"如果要重启或停止容器，我会告诉你到 Web 值班台用管理员确认——我不会直接动手。"

// ResolveSecret returns the long-connection Secret, or "" when none is
// configured. Precedence: Config.Secret, then SecretFile, then the
// WECOM_BOT_SECRET environment variable. The value is never logged.
func (c Config) ResolveSecret() (string, error) {
	if s := strings.TrimSpace(c.Secret); s != "" {
		return s, nil
	}
	if c.SecretFile != "" {
		b, err := os.ReadFile(c.SecretFile)
		if err != nil {
			return "", fmt.Errorf("wecom: read secret_file: %w", err)
		}
		s := strings.TrimSpace(strings.TrimRight(string(b), "\r\n"))
		if s == "" {
			return "", fmt.Errorf("wecom: secret_file %q is empty", c.SecretFile)
		}
		return s, nil
	}
	return strings.TrimSpace(os.Getenv("WECOM_BOT_SECRET")), nil
}

// ResolveBotID returns the robot id, falling back to WECOM_BOT_ID.
func (c Config) ResolveBotID() string {
	if s := strings.TrimSpace(c.BotID); s != "" {
		return s
	}
	return strings.TrimSpace(os.Getenv("WECOM_BOT_ID"))
}

// ResolveWSURL returns the configured endpoint override, falling back to
// WECOM_WS_URL. An empty result means "use the SDK default".
func (c Config) ResolveWSURL() string {
	if s := strings.TrimSpace(c.WSURL); s != "" {
		return s
	}
	return strings.TrimSpace(os.Getenv("WECOM_WS_URL"))
}

// ResolveGroupWebhookURL returns the optional group-robot webhook endpoint.
func (c Config) ResolveGroupWebhookURL() (string, error) {
	if s := strings.TrimSpace(c.GroupWebhookURL); s != "" {
		return s, nil
	}
	if c.GroupWebhookURLFile != "" {
		b, err := os.ReadFile(c.GroupWebhookURLFile)
		if err != nil {
			return "", fmt.Errorf("wecom: read group_webhook_url_file: %w", err)
		}
		s := strings.TrimSpace(strings.TrimRight(string(b), "\r\n"))
		if s == "" {
			return "", fmt.Errorf("wecom: group_webhook_url_file %q is empty", c.GroupWebhookURLFile)
		}
		return s, nil
	}
	return "", nil
}

// ResolveMode decides the transport mode. Both BotID and Secret must be
// present for live; otherwise the bridge runs in memory.
func (c Config) ResolveMode() (Mode, error) {
	if !c.Enabled {
		return ModeOff, nil
	}
	secret, err := c.ResolveSecret()
	if err != nil {
		return ModeOff, err
	}
	if c.ResolveBotID() == "" || secret == "" {
		return ModeMock, nil
	}
	return ModeLive, nil
}

// resolvedReplyMode normalizes ReplyMode, defaulting to stream.
func (c Config) resolvedReplyMode() ReplyMode {
	switch ReplyMode(strings.ToLower(strings.TrimSpace(c.ReplyMode))) {
	case ReplyMarkdown:
		return ReplyMarkdown
	case "", ReplyStream:
		return ReplyStream
	default:
		return ReplyStream
	}
}

func (c Config) maxTranscript() int {
	if c.MaxTranscript > 0 {
		return c.MaxTranscript
	}
	return DefaultMaxTranscript
}

func (c Config) askTimeoutSeconds() int {
	if c.AskTimeoutSeconds > 0 {
		return c.AskTimeoutSeconds
	}
	return DefaultAskTimeoutSeconds
}

func (c Config) welcomeText() string {
	if strings.TrimSpace(c.Welcome) != "" {
		return c.Welcome
	}
	return DefaultWelcome
}

// MaskBotID renders a BotID for display: first four characters, then stars.
// Returns "" for an empty id so the console can show "未配置".
func MaskBotID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if len(id) <= 4 {
		return strings.Repeat("*", len(id))
	}
	return id[:4] + "****"
}
