package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	aibot "github.com/go-sphere/wecom-aibot-go-sdk/aibot"
	"github.com/zsuroy/dockerview-go/internal/duty"
)

// actorKindWeCom labels audit actors that arrived over the long connection.
const actorKindWeCom = "wecom"

// proposeNoticeTemplate is the sentence every proposed write carries. The
// brief requires the reply to name the container and the action and to point
// at the web console, so it is assembled from the actual PreviewResult rather
// than being a generic warning.
const proposeNoticeTemplate = "⚠️ 我不会替你执行。待确认：%s %s（%s）。" +
	"请到 dockerview Web 用管理员确认，确认后才会真正执行。"

// proposeCardTitle is the template-card headline.
const proposeCardTitle = "需要人工确认"

// handleText is the OnMessageText handler. It serves both real WeCom callbacks
// and console injects — there is no "injected" branch, because the handler
// cannot tell them apart and should not.
func (b *Bridge) handleText(frame *aibot.WsFrame) {
	var msg aibot.TextMessage
	if err := aibot.ParseMessageBody(frame, &msg); err != nil {
		log.Printf("[WARN] wecom: parse text message: %v", err)
		b.tr.AddEvent("收到无法解析的文本帧")
		return
	}

	text := strings.TrimSpace(msg.Text.Content)
	if text == "" {
		return
	}

	source := SourceWeCom
	if strings.HasPrefix(aibot.GetReqID(frame), injectReqPrefix) {
		source = SourceInject
	}
	user := strings.TrimSpace(msg.From.UserID)

	b.tr.AddInbound(source, user, text)
	b.dispatch(frame, text, user, source)
}

// dispatch runs the Ask call off the SDK read loop. handleMessage runs
// synchronously inside that loop and SendReply blocks on an ack, so doing the
// work inline would stall heartbeats and every other conversation.
func (b *Bridge) dispatch(frame *aibot.WsFrame, question, user, source string) {
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		b.answer(frame, question, user, source)
	}()
}

// answer calls the shared DUTY brain and delivers the reply.
func (b *Bridge) answer(frame *aibot.WsFrame, question, user, source string) {
	if b.ask == nil {
		b.replyText(frame, user, ChannelStream, "DUTY 未启用，无法回答。请在 config.yaml 打开 agent.enabled。")
		return
	}

	actor := actorKindWeCom + ":" + user
	if user == "" {
		actor = actorKindWeCom + ":anonymous"
	}

	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(b.cfg.askTimeoutSeconds())*time.Second)
	defer cancel()

	res, err := b.ask.Ask(ctx, question, actor, actorKindWeCom, source)
	if err != nil {
		log.Printf("[WARN] wecom: duty ask failed: %v", err)
		if errors.Is(err, duty.ErrDisabled) {
			b.replyText(frame, user, ChannelStream, "DUTY 未启用，无法回答。请在 config.yaml 打开 agent.enabled 后重试。")
			return
		}
		b.replyText(frame, user, ChannelStream, "DUTY 出错了，没能回答这个问题。可以到 Web 值班台看审计记录，服务端 data/dockerview.log 有这次的报错。")
		return
	}
	b.deliver(frame, user, res)
}

// deliver turns an AskResult into outbound frames. A proposed write adds the
// human-confirmation notice and a template card — and nothing else. No docker
// call happens anywhere on this path: the only way to execute is an admin
// confirming in the browser via POST /api/duty/confirm.
func (b *Bridge) deliver(frame *aibot.WsFrame, user string, res *duty.AskResult) {
	text := strings.TrimSpace(res.Answer)
	if text == "" {
		text = "(DUTY 返回了空答复)"
	}

	if res.ProposedWrite != nil {
		p := res.ProposedWrite
		text = fmt.Sprintf(proposeNoticeTemplate, p.Op, p.Name, p.ID) + "\n\n" + text
	}

	b.replyText(frame, user, ChannelStream, text)

	if res.ProposedWrite != nil {
		b.replyCard(frame, res.ProposedWrite)
		if b.hook != nil {
			b.hook.NotifyProposal(context.Background(), res.ProposedWrite, "")
		}
	}
}

// replyCard sends the "confirm in the web console" template card. The card
// carries no action that could execute anything: clicking it produces a
// template_card_event, which handleTemplateCardEvent only records.
func (b *Bridge) replyCard(frame *aibot.WsFrame, p *duty.PreviewResult) {
	b.tr.AddOutbound(ChannelCard, fmt.Sprintf("%s：%s %s（%s）", proposeCardTitle, p.Op, p.Name, p.ID))

	if !b.client.IsConnected() {
		return
	}
	if _, err := b.client.ReplyTemplateCard(frame, proposeCard(p), nil); err != nil {
		log.Printf("[WARN] wecom: reply template card: %v", err)
	}
}

// proposeCard builds the confirmation card for a proposed write.
func proposeCard(p *duty.PreviewResult) aibot.TemplateCard {
	return aibot.TemplateCard{
		CardType:  string(aibot.TemplateCardTypeTextNotice),
		MainTitle: &aibot.TemplateCardMainTitle{Title: proposeCardTitle, Desc: "dockerview 不会替你做启停"},
		HorizontalContent: []aibot.TemplateCardHorizontalContent{
			{KeyName: "动作", Value: p.Op},
			{KeyName: "容器", Value: p.Name},
			{KeyName: "ID", Value: p.ID},
		},
		SubTitleText: "请到 dockerview Web 用管理员确认，确认后才会执行。",
	}
}

// replyText records the reply and, when a socket exists, sends it.
//
// Reply mode:
//   - stream   → ReplyStream with finish=true. One frame, because Ask returns a
//     complete answer and 101463 caps a conversation at 30 messages/minute.
//   - markdown → SendMarkdown, the proactive-push channel.
//
// In mock mode IsConnected is false, so the reply is only recorded. That is
// what lets the whole console flow be verified with no credentials.
func (b *Bridge) replyText(frame *aibot.WsFrame, user, channel, text string) {
	if !b.client.IsConnected() {
		// Mock mode: record only. Nothing leaves the process.
		b.tr.AddOutbound(channel, text)
		return
	}

	if b.cfg.resolvedReplyMode() == ReplyMarkdown {
		target := chatTarget(frame, user)
		if target == "" {
			log.Printf("[WARN] wecom: markdown reply has no chat target")
			b.tr.AddOutbound(ChannelMarkdown, text)
			return
		}
		if _, err := b.client.SendMarkdown(target, text); err != nil {
			log.Printf("[WARN] wecom: send markdown: %v", err)
		}
		b.tr.AddOutbound(ChannelMarkdown, text)
		return
	}

	streamID := aibot.GenerateReqId("stream")
	if _, err := b.client.ReplyStream(frame, streamID, text, true, nil, nil); err != nil {
		log.Printf("[WARN] wecom: reply stream: %v", err)
	}
	b.tr.AddOutbound(ChannelStream, text)
}

// chatTarget picks the conversation id for a proactive push. Per 101463 the
// chatid field carries the userid in single chats, so fall back to the sender.
func chatTarget(frame *aibot.WsFrame, user string) string {
	if frame != nil && len(frame.Body) > 0 {
		var probe struct {
			ChatID string `json:"chatid"`
		}
		if err := json.Unmarshal(frame.Body, &probe); err == nil && strings.TrimSpace(probe.ChatID) != "" {
			return probe.ChatID
		}
	}
	return user
}

// handleEnterChat answers the enter_chat event with the welcome text. The
// platform allows 5 seconds (101463), so this is a fixed local string and
// never goes through Ask.
func (b *Bridge) handleEnterChat(frame *aibot.WsFrame) {
	b.tr.AddEvent("收到 enter_chat 事件")

	welcome := b.cfg.welcomeText()
	b.tr.AddOutbound(ChannelWelcome, welcome)

	if !b.client.IsConnected() {
		return
	}
	// CreateTextReplyBody is the SDK helper for aibot_respond_welcome_msg.
	body := aibot.CreateTextReplyBody(welcome)
	if _, err := b.client.ReplyWelcome(frame, body); err != nil {
		log.Printf("[WARN] wecom: reply welcome: %v", err)
	}
}

// handleTemplateCardEvent records a card click. It deliberately does not call
// confirm, /api/container/op, or docker.ContainerOp: clicking a card in WeCom
// is not a way to start or stop a container.
func (b *Bridge) handleTemplateCardEvent(frame *aibot.WsFrame) {
	var ev aibot.EventMessage
	if err := aibot.ParseMessageBody(frame, &ev); err != nil {
		log.Printf("[WARN] wecom: parse card event: %v", err)
		b.tr.AddEvent("收到无法解析的卡片事件")
		return
	}

	var data aibot.TemplateCardEventData
	if len(ev.Event) > 0 {
		if err := json.Unmarshal(ev.Event, &data); err != nil {
			log.Printf("[WARN] wecom: parse card event data: %v", err)
		}
	}

	summary := "收到卡片点击事件"
	if data.EventKey != "" {
		summary += " event_key=" + data.EventKey
	}
	if data.TaskID != "" {
		summary += " task_id=" + data.TaskID
	}
	summary += "（只记录，不执行）"
	b.tr.AddEvent(summary)

	if !b.client.IsConnected() {
		return
	}

	// Update the card to say the click was recorded and the write still needs a
	// human in the browser. This is the only reaction to a card event.
	card := aibot.TemplateCard{
		CardType:     string(aibot.TemplateCardTypeTextNotice),
		MainTitle:    &aibot.TemplateCardMainTitle{Title: "已记录，仍未执行", Desc: "请到 dockerview Web 用管理员确认"},
		SubTitleText: "企微里点卡片不会启停容器。",
	}
	if _, err := b.client.UpdateTemplateCard(frame, card, nil); err != nil {
		log.Printf("[WARN] wecom: update template card: %v", err)
	}
}
