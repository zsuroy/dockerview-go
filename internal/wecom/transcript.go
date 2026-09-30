package wecom

import (
	"sync"
	"time"
)

// State is the connection state shown in the web console and the TUI.
//
// The brief names three of these (disconnected / mock / connected). Connecting
// and error are added because without them "cannot connect" and "is connecting"
// look identical, which makes the failure state impossible to read.
type State string

const (
	// StateDisconnected means the integration is off, or live mode is down.
	StateDisconnected State = "disconnected"
	// StateConnecting means Connect was called and authentication is pending.
	StateConnecting State = "connecting"
	// StateConnected means the SDK reported a successful authentication.
	StateConnected State = "connected"
	// StateMock means no credentials; in-memory transport, no socket.
	StateMock State = "mock"
	// StateError means authentication failed or reconnect attempts ran out.
	StateError State = "error"
)

// Entry kinds recorded in the transcript.
const (
	KindInbound  = "inbound"
	KindOutbound = "outbound"
	KindEvent    = "event"
)

// Entry sources.
const (
	SourceWeCom  = "wecom"
	SourceInject = "inject"
	SourceSystem = "system"
)

// Outbound channels.
const (
	ChannelStream   = "stream"
	ChannelMarkdown = "markdown"
	ChannelWelcome  = "welcome"
	ChannelCard     = "card"
)

// MaxTextInEntry bounds one transcript line so a huge answer cannot bloat the
// state payload. The full answer is already stored on the duty ticket.
const MaxTextInEntry = 2000

// Entry is one line in the conversation log.
type Entry struct {
	Time    time.Time `json:"time"`
	Kind    string    `json:"kind"`
	Source  string    `json:"source"`
	Channel string    `json:"channel,omitempty"`
	User    string    `json:"user,omitempty"`
	Text    string    `json:"text"`
}

// Counts summarizes the transcript.
type Counts struct {
	Inbound  int `json:"inbound"`
	Outbound int `json:"outbound"`
	Event    int `json:"event"`
}

// Snapshot is the read-only view the web console and TUI consume.
type Snapshot struct {
	Enabled       bool      `json:"enabled"`
	Mode          string    `json:"mode"`
	State         string    `json:"state"`
	StateReason   string    `json:"state_reason,omitempty"`
	WSURL         string    `json:"ws_url"`
	BotIDMasked   string    `json:"bot_id_masked,omitempty"`
	HasSecret     bool      `json:"has_secret"`
	ReplyMode     string    `json:"reply_mode"`
	InjectAllowed bool      `json:"inject_allowed"`
	LastActivity  time.Time `json:"last_activity,omitempty"`
	Counts        Counts    `json:"counts"`
	Recent        []Entry   `json:"recent"`
	// GroupWebhookEnabled reports the optional outbound-only notification path.
	GroupWebhookEnabled bool `json:"group_webhook_enabled"`
}

// Transcript is a fixed-capacity in-memory log of inbound messages, outbound
// replies, and events, plus the current connection state. It is deliberately
// not persisted: duty.db already holds every question and answer.
type Transcript struct {
	mu     sync.RWMutex
	cap    int
	ring   []Entry
	state  State
	reason string
	last   time.Time
}

// NewTranscript creates a transcript with the given capacity (min 1).
func NewTranscript(capacity int) *Transcript {
	if capacity < 1 {
		capacity = DefaultMaxTranscript
	}
	return &Transcript{
		cap:   capacity,
		ring:  make([]Entry, 0, capacity),
		state: StateDisconnected,
	}
}

// Add appends an entry, dropping the oldest when at capacity. It stamps the
// time when the caller left it zero and returns the stored entry, so callers
// see exactly what was recorded.
func (t *Transcript) Add(e Entry) Entry {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	if len(e.Text) > MaxTextInEntry {
		e.Text = e.Text[:MaxTextInEntry] + "…"
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.ring) == t.cap {
		copy(t.ring, t.ring[1:])
		t.ring[len(t.ring)-1] = e
	} else {
		t.ring = append(t.ring, e)
	}
	t.last = e.Time
	return e
}

// AddInbound records a message arriving from WeCom or from an inject.
func (t *Transcript) AddInbound(source, user, text string) Entry {
	return t.Add(Entry{Kind: KindInbound, Source: source, User: user, Text: text})
}

// AddOutbound records a reply leaving the process.
func (t *Transcript) AddOutbound(channel, text string) Entry {
	return t.Add(Entry{Kind: KindOutbound, Source: SourceSystem, Channel: channel, Text: text})
}

// AddEvent records a non-message event (card click, connection change).
func (t *Transcript) AddEvent(text string) Entry {
	return t.Add(Entry{Kind: KindEvent, Source: SourceSystem, Text: text})
}

// SetState updates the connection state and its human-readable reason.
func (t *Transcript) SetState(s State, reason string) {
	t.mu.Lock()
	t.state = s
	t.reason = reason
	t.last = time.Now()
	t.mu.Unlock()
}

// State returns the current connection state.
func (t *Transcript) State() State {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.state
}

// Entries returns a copy of the transcript, oldest first.
func (t *Transcript) Entries() []Entry {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]Entry, len(t.ring))
	copy(out, t.ring)
	return out
}

// countsLocked tallies entry kinds. Caller holds at least a read lock.
func (t *Transcript) countsLocked() Counts {
	var c Counts
	for _, e := range t.ring {
		switch e.Kind {
		case KindInbound:
			c.Inbound++
		case KindOutbound:
			c.Outbound++
		case KindEvent:
			c.Event++
		}
	}
	return c
}
