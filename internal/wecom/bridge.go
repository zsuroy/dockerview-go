package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	aibot "github.com/go-sphere/wecom-aibot-go-sdk/aibot"
	"github.com/zsuroy/dockerview-go/internal/duty"
)

// Asker is the DUTY brain, reached through the same call the HTTP handler
// makes. internal/wecom never re-implements the agent.
type Asker interface {
	Ask(ctx context.Context, question, actor, actorKind, source string) (*duty.AskResult, error)
}

// Errors returned by the bridge.
var (
	// ErrDisabled means the integration is switched off.
	ErrDisabled = errors.New("wecom: integration is disabled")
	// ErrEmptyText means an inject carried no text.
	ErrEmptyText = errors.New("wecom: empty text")
	// ErrTooLong means an inject exceeded MaxQuestionChars.
	ErrTooLong = errors.New("wecom: text too long")
)

// MaxQuestionChars mirrors the HTTP handler's 4000-char question cap.
const MaxQuestionChars = 4000

// injectReqPrefix marks frames produced by Inject. The SDK builds req_ids as
// "<cmd>_<unixnano>_<rand>"; WeCom's own server-issued req_ids never start
// with "inject", so the prefix tells the handler where a frame came from
// without any shared mutable state.
const injectReqPrefix = "inject"

// WebhookNotifier is an optional outbound-only notification sink (the group
// robot webhook). It is never a message source.
type WebhookNotifier interface {
	NotifyProposal(ctx context.Context, p *duty.PreviewResult, question string)
}

// sdkLogger adapts the SDK's Logger interface to the standard library so the
// long connection logs in the same format as the rest of dockerview.
type sdkLogger struct{}

func (sdkLogger) Debug(format string, v ...interface{}) {}
func (sdkLogger) Info(format string, v ...interface{})  { log.Printf("[INFO] aibot: "+format, v...) }
func (sdkLogger) Warn(format string, v ...interface{})  { log.Printf("[WARN] aibot: "+format, v...) }
func (sdkLogger) Error(format string, v ...interface{}) { log.Printf("[ERROR] aibot: "+format, v...) }

// Bridge owns the SDK client and routes inbound messages through DUTY.
type Bridge struct {
	cfg    Config
	mode   Mode
	secret string
	ask    Asker
	tr     *Transcript

	// client is the SDK type. It is constructed in every mode; Connect is only
	// called in live mode.
	client *aibot.WSClient

	hook WebhookNotifier

	wg      sync.WaitGroup
	mu      sync.Mutex
	started bool
}

// New builds the bridge. The SDK client object is always created — that is
// what makes the mock path share the production dispatch code — but Connect is
// only called from Start, and only in live mode.
func New(cfg Config, ask Asker, hook WebhookNotifier) (*Bridge, error) {
	mode, err := cfg.ResolveMode()
	if err != nil {
		return nil, err
	}
	secret, err := cfg.ResolveSecret()
	if err != nil {
		return nil, err
	}

	b := &Bridge{
		cfg:    cfg,
		mode:   mode,
		secret: secret,
		ask:    ask,
		tr:     NewTranscript(cfg.maxTranscript()),
		hook:   hook,
	}

	// WSURL is only set when explicitly configured. Left empty, the SDK keeps
	// its built-in default (DefaultWSClientOptions.WSURL).
	opts := aibot.WSClientOptions{
		BotID:  cfg.ResolveBotID(),
		Secret: secret,
		Logger: sdkLogger{},
	}
	if u := cfg.ResolveWSURL(); u != "" {
		opts.WSURL = u
	}

	// NewWSClient performs no network I/O: it assigns fields, builds the API
	// client and the connection manager, stores credentials, and binds the
	// internal event callbacks. Dialing happens only in Connect.
	b.client = aibot.NewWSClient(opts)
	b.registerHandlers()

	switch mode {
	case ModeOff:
		b.tr.SetState(StateDisconnected, "未启用（wecom.enabled = false）")
	case ModeMock:
		b.tr.SetState(StateMock, "未配置凭证，使用内存 transport，不拨公网")
	case ModeLive:
		b.tr.SetState(StateDisconnected, "已配置凭证，尚未连接")
	}
	return b, nil
}

// Mode returns the resolved transport mode.
func (b *Bridge) Mode() Mode { return b.mode }

// State returns the current connection state.
func (b *Bridge) State() State { return b.tr.State() }

// Transcript exposes the in-memory conversation log.
func (b *Bridge) Transcript() *Transcript { return b.tr }

// registerHandlers wires the SDK callbacks. Heartbeat and reconnect stay
// inside the SDK; these handlers only mirror state into the transcript.
func (b *Bridge) registerHandlers() {
	b.client.OnConnected(func() {
		b.tr.SetState(StateConnecting, "WebSocket 已建立，等待认证")
	})
	b.client.OnAuthenticated(func() {
		b.tr.SetState(StateConnected, "")
		b.tr.AddEvent("认证成功，长连接就绪")
	})
	b.client.OnDisconnected(func(reason string) {
		b.tr.SetState(StateDisconnected, reason)
		b.tr.AddEvent("连接断开：" + reason)
	})
	b.client.OnReconnecting(func(attempt int) {
		b.tr.SetState(StateConnecting, fmt.Sprintf("重连第 %d 次", attempt))
	})
	b.client.OnError(func(err error) {
		b.tr.SetState(StateError, err.Error())
		b.tr.AddEvent("连接错误：" + err.Error())
	})

	b.client.OnMessageText(b.handleText)
	b.client.OnEventEnterChat(b.handleEnterChat)
	b.client.OnEventTemplateCardEvent(b.handleTemplateCardEvent)
}

// Start connects in live mode. In mock mode it does nothing on purpose: there
// is no socket to open, and the emitter drives the same handlers.
func (b *Bridge) Start(_ context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.started {
		return nil
	}
	b.started = true

	switch b.mode {
	case ModeOff:
		log.Printf("[INFO] wecom: disabled")
		return nil
	case ModeMock:
		log.Printf("[INFO] wecom: mock mode — no credentials, in-memory transport, no dial")
		return nil
	default:
		b.tr.SetState(StateConnecting, "正在连接 openws")
		log.Printf("[INFO] wecom: connecting (ws_url=%s bot_id=%s)", b.wsURLForLog(), MaskBotID(b.cfg.ResolveBotID()))
		b.client.Connect()
		return nil
	}
}

// Stop closes the connection. Safe to call when never started.
func (b *Bridge) Stop() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.started {
		return nil
	}
	b.started = false
	if b.mode == ModeLive {
		b.client.Disconnect()
	}
	// Let in-flight Ask calls finish so their replies still land in the
	// transcript before the process exits.
	done := make(chan struct{})
	go func() { b.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}
	return nil
}

// WaitIdle blocks until in-flight handler work finishes. Used by tests, which
// would otherwise race the goroutine that dispatch starts.
func (b *Bridge) WaitIdle(ctx context.Context) {
	done := make(chan struct{})
	go func() { b.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// wsURLForLog reports the effective endpoint without leaking credentials.
func (b *Bridge) wsURLForLog() string {
	if u := b.cfg.ResolveWSURL(); u != "" {
		return u
	}
	return aibot.DefaultWSClientOptions.WSURL
}

// SDKClient returns the underlying SDK client. Exposed for tests and for the
// console's diagnostics; callers must not call Connect on it directly.
func (b *Bridge) SDKClient() *aibot.WSClient { return b.client }

// Inject feeds a synthetic inbound text frame through the SDK's emitter, so it
// lands on exactly the same handler a real WeCom callback would hit.
//
// EmitMessageText invokes the registered handler synchronously on this
// goroutine; the handler then hands the Ask call to a worker goroutine.
func (b *Bridge) Inject(text, userID, chatID string) (Entry, error) {
	if !b.cfg.Enabled {
		return Entry{}, ErrDisabled
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return Entry{}, ErrEmptyText
	}
	if len(text) > MaxQuestionChars {
		return Entry{}, ErrTooLong
	}
	if strings.TrimSpace(userID) == "" {
		userID = "inject-user"
	}
	if strings.TrimSpace(chatID) == "" {
		chatID = userID
	}

	body, err := json.Marshal(map[string]any{
		"msgid":    aibot.GenerateReqId("msg"),
		"aibotid":  b.cfg.ResolveBotID(),
		"chatid":   chatID,
		"chattype": "single",
		"from":     map[string]string{"userid": userID},
		"msgtype":  string(aibot.MessageTypeText),
		"text":     map[string]string{"content": text},
	})
	if err != nil {
		return Entry{}, fmt.Errorf("wecom: marshal inject body: %w", err)
	}

	frame := &aibot.WsFrame{
		Cmd:     aibot.WsCmd.CALLBACK,
		Headers: aibot.WsFrameHeaders{ReqID: aibot.GenerateReqId(injectReqPrefix)},
		Body:    json.RawMessage(body),
	}

	b.client.EmitMessageText(frame)

	// The handler already recorded the inbound entry; return the newest one so
	// callers can correlate.
	entries := b.tr.Entries()
	if n := len(entries); n > 0 {
		for i := n - 1; i >= 0; i-- {
			if entries[i].Kind == KindInbound {
				return entries[i], nil
			}
		}
	}
	return Entry{}, nil
}

// InjectEnterChat feeds a synthetic enter_chat event. Used by tests and by the
// console's "welcome" drill, so the welcome path is exercisable without a real
// WeCom connection.
func (b *Bridge) InjectEnterChat(userID string) error {
	if !b.cfg.Enabled {
		return ErrDisabled
	}
	if strings.TrimSpace(userID) == "" {
		userID = "inject-user"
	}
	body, err := json.Marshal(map[string]any{
		"msgid":       aibot.GenerateReqId("msg"),
		"create_time": time.Now().Unix(),
		"aibotid":     b.cfg.ResolveBotID(),
		"from":        map[string]string{"userid": userID},
		"msgtype":     "event",
		"event":       map[string]string{"eventtype": string(aibot.EventTypeEnterChat)},
	})
	if err != nil {
		return fmt.Errorf("wecom: marshal enter_chat body: %w", err)
	}
	b.client.EmitEventEnterChat(&aibot.WsFrame{
		Cmd:     aibot.WsCmd.EVENT_CALLBACK,
		Headers: aibot.WsFrameHeaders{ReqID: aibot.GenerateReqId(injectReqPrefix)},
		Body:    json.RawMessage(body),
	})
	return nil
}

// Snapshot returns the console/TUI view. injectAllowed is decided by the
// caller (the HTTP layer knows the token); each handler re-checks it anyway.
func (b *Bridge) Snapshot(injectAllowed bool) Snapshot {
	entries := b.tr.Entries()

	b.mu.Lock()
	mode := b.mode
	b.mu.Unlock()

	b.tr.mu.RLock()
	state := b.tr.state
	reason := b.tr.reason
	last := b.tr.last
	counts := b.tr.countsLocked()
	b.tr.mu.RUnlock()

	webhookOn := false
	if b.cfg.GroupWebhookEnabled {
		if u, err := b.cfg.ResolveGroupWebhookURL(); err == nil && u != "" {
			webhookOn = true
		}
	}

	return Snapshot{
		Enabled:             b.cfg.Enabled,
		Mode:                string(mode),
		State:               string(state),
		StateReason:         reason,
		WSURL:               b.wsURLForLog(),
		BotIDMasked:         MaskBotID(b.cfg.ResolveBotID()),
		HasSecret:           b.secret != "",
		ReplyMode:           string(b.cfg.resolvedReplyMode()),
		InjectAllowed:       injectAllowed,
		LastActivity:        last,
		Counts:              counts,
		Recent:              entries,
		GroupWebhookEnabled: webhookOn,
	}
}
