package wecom

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	aibot "github.com/go-sphere/wecom-aibot-go-sdk/aibot"
	"github.com/zsuroy/dockerview-go/internal/duty"
)

// ---------- test doubles ----------

type askCall struct {
	Question  string
	Actor     string
	ActorKind string
	Source    string
}

type fakeAsker struct {
	mu    sync.Mutex
	calls []askCall
	res   *duty.AskResult
	err   error
	block chan struct{}
}

func (f *fakeAsker) Ask(_ context.Context, question, actor, actorKind, source string) (*duty.AskResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, askCall{question, actor, actorKind, source})
	block := f.block
	res, err := f.res, f.err
	f.mu.Unlock()

	if block != nil {
		<-block
	}
	if err != nil {
		return nil, err
	}
	if res == nil {
		return &duty.AskResult{Answer: "ok"}, nil
	}
	return res, nil
}

func (f *fakeAsker) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeAsker) lastCall() (askCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return askCall{}, false
	}
	return f.calls[len(f.calls)-1], true
}

// countingListener wraps a listener and tallies accepted connections, so a test
// can prove that nothing dialed.
type countingListener struct {
	net.Listener
	accepted int64
}

func (l *countingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		atomic.AddInt64(&l.accepted, 1)
	}
	return c, err
}

func (l *countingListener) count() int64 { return atomic.LoadInt64(&l.accepted) }

// dialCounter stands up a local endpoint and reports how many TCP connections
// reached it.
func dialCounter(t *testing.T) (url string, c *countingListener) {
	t.Helper()
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	cl := &countingListener{Listener: inner}

	srv := &httptest.Server{
		Listener: cl,
		Config:   &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})},
	}
	srv.Start()
	t.Cleanup(srv.Close)

	return "ws://" + strings.TrimPrefix(srv.URL, "http://"), cl
}

func mockConfig(t *testing.T) Config {
	t.Helper()
	t.Setenv("WECOM_BOT_ID", "")
	t.Setenv("WECOM_BOT_SECRET", "")
	cfg := DefaultConfig()
	cfg.Enabled = true
	return cfg
}

func newMockBridge(t *testing.T, ask Asker) *Bridge {
	t.Helper()
	b, err := New(mockConfig(t), ask, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = b.Stop() })
	if err := b.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return b
}

func waitIdle(t *testing.T, b *Bridge) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b.WaitIdle(ctx)
}

func outboundTexts(b *Bridge) []string {
	var out []string
	for _, e := range b.Transcript().Entries() {
		if e.Kind == KindOutbound {
			out = append(out, e.Text)
		}
	}
	return out
}

// ---------- transport mode ----------

func TestMockModeNeverDialsConfiguredEndpoint(t *testing.T) {
	url, counter := dialCounter(t)

	cfg := mockConfig(t)
	cfg.WSURL = url // point the SDK at a local endpoint we can observe

	b, err := New(cfg, &fakeAsker{}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if b.Mode() != ModeMock {
		t.Fatalf("mode = %q, want mock", b.Mode())
	}
	if err := b.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer b.Stop()

	// Give a stray dial every chance to happen before asserting it did not.
	time.Sleep(300 * time.Millisecond)
	if n := counter.count(); n != 0 {
		t.Fatalf("mock mode opened %d connection(s); it must not dial at all", n)
	}
}

func TestLiveModeDialsConfiguredEndpoint(t *testing.T) {
	url, counter := dialCounter(t)

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.BotID = "ww-test-bot"
	cfg.Secret = "test-secret"
	cfg.WSURL = url

	b, err := New(cfg, &fakeAsker{}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if b.Mode() != ModeLive {
		t.Fatalf("mode = %q, want live", b.Mode())
	}
	if err := b.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer b.Stop()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if counter.count() > 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("live mode never dialed the configured endpoint")
}

func TestDisabledBridgeReportsDisconnected(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false

	b, err := New(cfg, &fakeAsker{}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if b.Mode() != ModeOff {
		t.Fatalf("mode = %q, want off", b.Mode())
	}
	if got := b.State(); got != StateDisconnected {
		t.Fatalf("state = %q, want disconnected", got)
	}

	snap := b.Snapshot(false)
	if snap.Enabled {
		t.Fatal("snapshot should report enabled=false")
	}
	if snap.State != string(StateDisconnected) {
		t.Fatalf("snapshot state = %q", snap.State)
	}
}

func TestMockModeReportsMockState(t *testing.T) {
	b := newMockBridge(t, &fakeAsker{})
	if got := b.State(); got != StateMock {
		t.Fatalf("state = %q, want mock", got)
	}
	snap := b.Snapshot(true)
	if snap.Mode != string(ModeMock) {
		t.Fatalf("snapshot mode = %q", snap.Mode)
	}
	if snap.HasSecret {
		t.Fatal("snapshot must not claim a secret is configured")
	}
	if snap.StateReason == "" {
		t.Fatal("mock state should explain itself")
	}
	if !strings.Contains(snap.StateReason, "内存") {
		t.Fatalf("mock reason = %q, want it to mention the in-memory transport", snap.StateReason)
	}
}

func TestBridgeConstructsSDKClientInEveryMode(t *testing.T) {
	// A compile-time-plus-runtime proof that the transport really is the SDK
	// type, not a lookalike.
	for _, enabled := range []bool{false, true} {
		cfg := mockConfig(t)
		cfg.Enabled = enabled
		b, err := New(cfg, &fakeAsker{}, nil)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		var c *aibot.WSClient = b.SDKClient()
		if c == nil {
			t.Fatal("SDKClient returned nil; the SDK client must always be constructed")
		}
		if c.IsConnected() {
			t.Fatal("a freshly built client must not report itself connected")
		}
	}
}

func TestDefaultWSURLMatchesOfficialEndpoint(t *testing.T) {
	if got := aibot.DefaultWSClientOptions.WSURL; got != "wss://openws.work.weixin.qq.com" {
		t.Fatalf("SDK default endpoint = %q; docs 101463 specifies wss://openws.work.weixin.qq.com", got)
	}
}

// ---------- inbound -> Ask -> reply ----------

func TestInjectReachesTheSharedHandler(t *testing.T) {
	ask := &fakeAsker{res: &duty.AskResult{Answer: "api 不健康，健康分 42。", TicketID: 7}}
	b := newMockBridge(t, ask)

	if _, err := b.Inject("哪些容器不健康？", "alice", ""); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	waitIdle(t, b)

	if ask.callCount() != 1 {
		t.Fatalf("Ask called %d times, want 1", ask.callCount())
	}
	call, _ := ask.lastCall()
	if call.Question != "哪些容器不健康？" {
		t.Fatalf("question = %q", call.Question)
	}
	if call.Actor != "wecom:alice" {
		t.Fatalf("actor = %q, want wecom:alice", call.Actor)
	}
	if call.ActorKind != actorKindWeCom {
		t.Fatalf("actor kind = %q", call.ActorKind)
	}
	if call.Source != SourceInject {
		t.Fatalf("source = %q, want inject", call.Source)
	}

	replies := outboundTexts(b)
	if len(replies) != 1 {
		t.Fatalf("got %d outbound entries, want 1: %v", len(replies), replies)
	}
	if !strings.Contains(replies[0], "健康分 42") {
		t.Fatalf("reply = %q, want the Ask answer", replies[0])
	}
}

func TestInjectRecordsInboundWithItsSource(t *testing.T) {
	b := newMockBridge(t, &fakeAsker{})
	if _, err := b.Inject("hello", "bob", ""); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	waitIdle(t, b)

	var found bool
	for _, e := range b.Transcript().Entries() {
		if e.Kind == KindInbound {
			found = true
			if e.Source != SourceInject {
				t.Fatalf("inbound source = %q, want inject", e.Source)
			}
			if e.User != "bob" {
				t.Fatalf("inbound user = %q", e.User)
			}
		}
	}
	if !found {
		t.Fatal("no inbound entry recorded")
	}
}

func TestRealWeComFrameIsLabelledWeCom(t *testing.T) {
	ask := &fakeAsker{}
	b := newMockBridge(t, ask)

	// Emit a frame whose req_id looks like one the WeCom server would issue.
	frame := textFrame(t, "server_req_1", "u-123", "api 日志里有什么？")
	b.SDKClient().EmitMessageText(frame)
	waitIdle(t, b)

	var inbound Entry
	for _, e := range b.Transcript().Entries() {
		if e.Kind == KindInbound {
			inbound = e
		}
	}
	if inbound.Source != SourceWeCom {
		t.Fatalf("source = %q, want wecom for a server-issued frame", inbound.Source)
	}
	if ask.callCount() != 1 {
		t.Fatalf("Ask called %d times, want 1", ask.callCount())
	}
}

func TestInjectRejectsEmptyAndOversizedText(t *testing.T) {
	b := newMockBridge(t, &fakeAsker{})

	if _, err := b.Inject("   ", "u", ""); !errors.Is(err, ErrEmptyText) {
		t.Fatalf("err = %v, want ErrEmptyText", err)
	}
	if _, err := b.Inject(strings.Repeat("x", MaxQuestionChars+1), "u", ""); !errors.Is(err, ErrTooLong) {
		t.Fatalf("err = %v, want ErrTooLong", err)
	}
}

func TestInjectRefusedWhenDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false
	b, err := New(cfg, &fakeAsker{}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := b.Inject("hi", "u", ""); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
}

func TestAskErrorStillProducesAReadableReply(t *testing.T) {
	ask := &fakeAsker{err: errors.New("model exploded")}
	b := newMockBridge(t, ask)

	if _, err := b.Inject("哪些不健康", "u", ""); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	waitIdle(t, b)

	replies := outboundTexts(b)
	if len(replies) != 1 {
		t.Fatalf("got %d replies, want 1", len(replies))
	}
	if !strings.Contains(replies[0], "出错了") {
		t.Fatalf("reply = %q, want an honest error message", replies[0])
	}
}

func TestAskDisabledStillReplies(t *testing.T) {
	b := newMockBridge(t, nil) // no Asker at all
	if _, err := b.Inject("hi", "u", ""); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	waitIdle(t, b)

	replies := outboundTexts(b)
	if len(replies) != 1 || !strings.Contains(replies[0], "DUTY 未启用") {
		t.Fatalf("replies = %v, want a DUTY-disabled notice", replies)
	}
}

// textFrame builds an inbound text frame with a caller-chosen req_id.
func textFrame(t *testing.T, reqID, user, text string) *aibot.WsFrame {
	t.Helper()
	body := []byte(`{"msgid":"m1","aibotid":"bot","chatid":"c1","chattype":"single","from":{"userid":"` +
		user + `"},"msgtype":"text","text":{"content":"` + text + `"}}`)
	return &aibot.WsFrame{
		Cmd:     aibot.WsCmd.CALLBACK,
		Headers: aibot.WsFrameHeaders{ReqID: reqID},
		Body:    body,
	}
}
