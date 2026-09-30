package wecom

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	aibot "github.com/go-sphere/wecom-aibot-go-sdk/aibot"
	"github.com/zsuroy/dockerview-go/internal/duty"
)

// fakeNotifier records webhook calls so a test can assert the optional
// notification path fires only for a proposed write.
type fakeNotifier struct {
	mu    sync.Mutex
	calls []*duty.PreviewResult
}

func (f *fakeNotifier) NotifyProposal(_ context.Context, p *duty.PreviewResult, _ string) {
	f.mu.Lock()
	f.calls = append(f.calls, p)
	f.mu.Unlock()
}

func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func proposalResult() *duty.AskResult {
	return &duty.AskResult{
		Answer:   "api 最近有 7 条 ERROR，看起来确实该重启。",
		TicketID: 1284,
		ProposedWrite: &duty.PreviewResult{
			ID:          "8f3c1a92d4e7",
			Name:        "api",
			Status:      "running",
			HealthScore: 42,
			Op:          "restart",
			Impact:      "短暂中断",
		},
	}
}

func cardEntries(b *Bridge) []Entry {
	var out []Entry
	for _, e := range b.Transcript().Entries() {
		if e.Kind == KindOutbound && e.Channel == ChannelCard {
			out = append(out, e)
		}
	}
	return out
}

func eventTexts(b *Bridge) []string {
	var out []string
	for _, e := range b.Transcript().Entries() {
		if e.Kind == KindEvent {
			out = append(out, e.Text)
		}
	}
	return out
}

// ---------- the latch ----------

func TestProposedWriteNamesContainerAndAction(t *testing.T) {
	b := newMockBridge(t, &fakeAsker{res: proposalResult()})

	if _, err := b.Inject("帮我把 api 重启一下", "alice", ""); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	waitIdle(t, b)

	replies := outboundTexts(b)
	if len(replies) == 0 {
		t.Fatal("no reply recorded")
	}
	// The stream reply is the first outbound entry on this path.
	var reply string
	for _, e := range b.Transcript().Entries() {
		if e.Kind == KindOutbound && e.Channel == ChannelStream {
			reply = e.Text
			break
		}
	}
	if reply == "" {
		t.Fatal("no stream reply recorded")
	}
	for _, want := range []string{"restart", "api", "8f3c1a92d4e7"} {
		if !strings.Contains(reply, want) {
			t.Errorf("reply %q does not name %q", reply, want)
		}
	}
}

func TestProposedWritePointsAtTheWebConsole(t *testing.T) {
	b := newMockBridge(t, &fakeAsker{res: proposalResult()})
	if _, err := b.Inject("重启 api", "alice", ""); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	waitIdle(t, b)

	var reply string
	for _, e := range b.Transcript().Entries() {
		if e.Kind == KindOutbound && e.Channel == ChannelStream {
			reply = e.Text
			break
		}
	}
	if !strings.Contains(reply, "Web") {
		t.Fatalf("reply %q must tell the operator to confirm in the web console", reply)
	}
	if !strings.Contains(reply, "管理员") {
		t.Fatalf("reply %q must say the confirmation needs an admin", reply)
	}
	if !strings.Contains(reply, "不会替你执行") {
		t.Fatalf("reply %q must say the bot does not execute", reply)
	}
}

func TestProposedWriteSendsATemplateCard(t *testing.T) {
	b := newMockBridge(t, &fakeAsker{res: proposalResult()})
	if _, err := b.Inject("重启 api", "alice", ""); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	waitIdle(t, b)

	cards := cardEntries(b)
	if len(cards) != 1 {
		t.Fatalf("got %d card entries, want 1", len(cards))
	}
	if !strings.Contains(cards[0].Text, "restart") || !strings.Contains(cards[0].Text, "api") {
		t.Fatalf("card entry = %q, want it to name the op and container", cards[0].Text)
	}
}

func TestProposeCardShapeIsAConfirmationNotice(t *testing.T) {
	card := proposeCard(proposalResult().ProposedWrite)
	if card.CardType != string(aibot.TemplateCardTypeTextNotice) {
		t.Fatalf("card type = %q", card.CardType)
	}
	if card.MainTitle == nil || card.MainTitle.Title != proposeCardTitle {
		t.Fatalf("card title = %+v", card.MainTitle)
	}
	if !strings.Contains(card.SubTitleText, "Web") {
		t.Fatalf("card subtitle = %q, want a pointer at the web console", card.SubTitleText)
	}
	var sawOp, sawContainer bool
	for _, h := range card.HorizontalContent {
		if h.Value == "restart" {
			sawOp = true
		}
		if h.Value == "8f3c1a92d4e7" {
			sawContainer = true
		}
	}
	if !sawOp || !sawContainer {
		t.Fatalf("card rows = %+v, want the op and the container id", card.HorizontalContent)
	}
}

func TestPlainAnswerSendsNoCard(t *testing.T) {
	b := newMockBridge(t, &fakeAsker{res: &duty.AskResult{Answer: "一切正常。"}})
	if _, err := b.Inject("哪些不健康？", "alice", ""); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	waitIdle(t, b)

	if got := cardEntries(b); len(got) != 0 {
		t.Fatalf("got %d card entries for a read-only answer, want 0", len(got))
	}
}

func TestProposalNotifiesTheOptionalWebhook(t *testing.T) {
	hook := &fakeNotifier{}
	cfg := mockConfig(t)
	b, err := New(cfg, &fakeAsker{res: proposalResult()}, hook)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := b.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer b.Stop()

	if _, err := b.Inject("重启 api", "alice", ""); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	waitIdle(t, b)

	if hook.count() != 1 {
		t.Fatalf("webhook notified %d times, want 1", hook.count())
	}
}

func TestReadOnlyAnswerDoesNotNotifyTheWebhook(t *testing.T) {
	hook := &fakeNotifier{}
	cfg := mockConfig(t)
	b, err := New(cfg, &fakeAsker{res: &duty.AskResult{Answer: "都挺好。"}}, hook)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := b.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer b.Stop()

	if _, err := b.Inject("哪些不健康？", "alice", ""); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	waitIdle(t, b)

	if hook.count() != 0 {
		t.Fatalf("webhook notified %d times for a read-only answer, want 0", hook.count())
	}
}

// ---------- card clicks ----------

func cardEventFrame(t *testing.T, eventKey string) *aibot.WsFrame {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"msgid":   "card-msg-1",
		"aibotid": "bot",
		"from":    map[string]string{"userid": "alice"},
		"msgtype": "event",
		"event": map[string]any{
			"eventtype": string(aibot.EventTypeTemplateCardEvent),
			"event_key": eventKey,
			"task_id":   "task-1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &aibot.WsFrame{
		Cmd:     aibot.WsCmd.EVENT_CALLBACK,
		Headers: aibot.WsFrameHeaders{ReqID: "server_card_req_1"},
		Body:    body,
	}
}

func TestCardClickIsRecordedNotExecuted(t *testing.T) {
	b := newMockBridge(t, &fakeAsker{})

	b.SDKClient().EmitEventTemplateCardEvent(cardEventFrame(t, "confirm_restart"))
	waitIdle(t, b)

	var found bool
	for _, txt := range eventTexts(b) {
		if strings.Contains(txt, "卡片点击") {
			found = true
			if !strings.Contains(txt, "只记录") {
				t.Errorf("event text = %q, want it to say the click is recorded only", txt)
			}
			if !strings.Contains(txt, "confirm_restart") {
				t.Errorf("event text = %q, want the event_key preserved", txt)
			}
		}
	}
	if !found {
		t.Fatal("card click produced no event entry")
	}
}

func TestCardClickSendsNoMessageAndNoWrite(t *testing.T) {
	b := newMockBridge(t, &fakeAsker{})

	b.SDKClient().EmitEventTemplateCardEvent(cardEventFrame(t, "confirm_restart"))
	waitIdle(t, b)

	if got := outboundTexts(b); len(got) != 0 {
		t.Fatalf("card click produced %d outbound messages, want 0 in mock mode", len(got))
	}
}

func TestCardClickAfterProposalStillDoesNotExecute(t *testing.T) {
	ask := &fakeAsker{res: proposalResult()}
	b := newMockBridge(t, ask)

	// A question that proposes a write, then a click on the resulting card.
	if _, err := b.Inject("重启 api", "alice", ""); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	waitIdle(t, b)
	before := ask.callCount()

	b.SDKClient().EmitEventTemplateCardEvent(cardEventFrame(t, "confirm_restart"))
	waitIdle(t, b)

	if ask.callCount() != before {
		t.Fatalf("card click triggered %d extra Ask call(s); it must not re-enter the brain",
			ask.callCount()-before)
	}
	// Only the original proposal's entries exist; the click adds an event line.
	var streams, cards int
	for _, e := range b.Transcript().Entries() {
		switch {
		case e.Kind == KindOutbound && e.Channel == ChannelStream:
			streams++
		case e.Kind == KindOutbound && e.Channel == ChannelCard:
			cards++
		}
	}
	if streams != 1 || cards != 1 {
		t.Fatalf("streams=%d cards=%d, want exactly the one proposal exchange", streams, cards)
	}
}

// ---------- structural guarantees ----------

// TestPackageNeverImportsDocker is the structural half of the latch. The
// handler cannot start or stop a container if the package has no way to reach
// docker.ContainerOp in the first place.
func TestPackageNeverImportsDocker(t *testing.T) {
	forbidden := []string{
		"github.com/zsuroy/dockerview-go/internal/docker",
		"github.com/docker/docker/client",
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse package dir: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("no package parsed")
	}

	for _, pkg := range pkgs {
		for path, f := range pkg.Files {
			for _, imp := range f.Imports {
				v := strings.Trim(imp.Path.Value, `"`)
				for _, bad := range forbidden {
					if v == bad {
						t.Errorf("%s imports %s; the long-connection layer must not be able to touch Docker",
							filepath.Base(path), v)
					}
				}
			}
		}
	}
}

// TestPackageNeverCallsConfirmEndpoint is the textual half of the latch: no
// string literal and no selector in this package may name the confirm route,
// the container-op route, or docker.ContainerOp.
//
// It walks the AST rather than grepping raw bytes, because the doc comments in
// this package deliberately explain what the handler must NOT do — prose that
// names the forbidden calls is the point, code that makes them is not.
func TestPackageNeverCallsConfirmEndpoint(t *testing.T) {
	forbidden := []string{
		"/api/duty/confirm",
		"/api/container/op",
		"ContainerOp",
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse package dir: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("no package parsed")
	}

	checked := 0
	for _, pkg := range pkgs {
		for path, f := range pkg.Files {
			ast.Inspect(f, func(n ast.Node) bool {
				switch v := n.(type) {
				case *ast.BasicLit:
					if v.Kind != token.STRING {
						return true
					}
					checked++
					lit := strings.Trim(v.Value, "`\"")
					for _, bad := range forbidden {
						if strings.Contains(lit, bad) {
							t.Errorf("%s:%d string literal references %q; the write path stays in the browser",
								filepath.Base(path), fset.Position(v.Pos()).Line, bad)
						}
					}
				case *ast.SelectorExpr:
					checked++
					for _, bad := range forbidden {
						if v.Sel.Name == bad {
							t.Errorf("%s:%d calls %s; the long-connection layer must not reach Docker",
								filepath.Base(path), fset.Position(v.Pos()).Line, bad)
						}
					}
				}
				return true
			})
		}
	}
	if checked == 0 {
		t.Fatal("walked no literals or selectors; the guard is not actually inspecting anything")
	}
}
