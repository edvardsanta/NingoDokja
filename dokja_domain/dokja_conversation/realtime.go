package conversation

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// RealtimeMessage preserves transcription evidence independently of semantic projections.
// Empty associations mean unresolved, never an implicit assignment to the latest topic.
type RealtimeMessage struct {
	CaptureSequence int       `json:"capture_sequence"`
	MessageID       string    `json:"message_id"`
	SessionID       string    `json:"session_id"`
	SpeakerID       string    `json:"speaker_id"`
	SpeakerName     string    `json:"speaker_name,omitempty"`
	TimestampStart  time.Time `json:"timestamp_start"`
	TimestampEnd    time.Time `json:"timestamp_end"`
	Text            string    `json:"text"`
	Status          string    `json:"transcription_status"`
	Revision        int       `json:"revision"`
	ThreadIDs       []string  `json:"thread_ids"`
	ReplyTo         string    `json:"reply_to,omitempty"`
}
type RealtimeSession struct {
	channelWide     bool
	startedBy       string
	SessionID       string            `json:"session_id"`
	GuildID         string            `json:"guild_id"`
	ChannelID       string            `json:"channel_id"`
	OriginChannelID string            `json:"origin_channel_id"`
	Mode            string            `json:"mode"`
	Active          bool              `json:"active"`
	Consenting      []string          `json:"consenting_participants"`
	Messages        []RealtimeMessage `json:"messages"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

// ConversationContextEngine's first projection is a bounded, ephemeral observation
// journal. Semantic assignments are deliberately unresolved until supported by evidence.
// Channel observers admit speakers resolved by the authorized Discord adapter.
type ConversationContextEngine struct {
	mu       sync.Mutex
	sessions map[string]RealtimeSession
	now      func() time.Time
}

func NewConversationContextEngine() *ConversationContextEngine {
	return &ConversationContextEngine{sessions: map[string]RealtimeSession{}, now: time.Now}
}

var ErrRealtime = errors.New("invalid or unauthorized realtime conversation operation")

func bounded(v string, n int) bool { return v != "" && len(v) <= n && utf8.ValidString(v) }
func (e *ConversationContextEngine) prune() {
	for id, s := range e.sessions {
		if e.now().Sub(s.UpdatedAt) > 15*time.Minute {
			delete(e.sessions, id)
		}
	}
}
func (e *ConversationContextEngine) Start(id, guild, channel, origin, speaker string) error {
	return e.start(id, guild, channel, origin, speaker, false)
}

// StartChannel starts a silent observer for the whole voice channel.
// The legacy Start/Consent API remains available to existing callers.
func (e *ConversationContextEngine) StartChannel(id, guild, channel, origin, speaker string) error {
	return e.start(id, guild, channel, origin, speaker, true)
}

func (e *ConversationContextEngine) start(id, guild, channel, origin, speaker string, channelWide bool) error {
	if !bounded(id, 128) || !bounded(guild, 100) || !bounded(channel, 100) || !bounded(origin, 100) || !bounded(speaker, 100) {
		return ErrRealtime
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.prune()
	if prior, ok := e.sessions[id]; ok {
		if prior.GuildID == guild && prior.ChannelID == channel && prior.OriginChannelID == origin && prior.startedBy == speaker && prior.channelWide == channelWide && prior.Active {
			return nil
		}
		return ErrRealtime
	}
	if len(e.sessions) >= 32 {
		return errors.New("realtime session capacity reached")
	}
	e.sessions[id] = RealtimeSession{channelWide: channelWide, startedBy: speaker, SessionID: id, GuildID: guild, ChannelID: channel, OriginChannelID: origin, Mode: "observer", Active: true, Consenting: []string{speaker}, Messages: []RealtimeMessage{}, UpdatedAt: e.now()}
	return nil
}
func allowed(s RealtimeSession, speaker string) bool {
	for _, id := range s.Consenting {
		if id == speaker {
			return true
		}
	}
	return false
}
func (e *ConversationContextEngine) Consent(id, guild, origin, speaker string, join bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.prune()
	s, ok := e.sessions[id]
	if !ok || !s.Active || s.channelWide || s.GuildID != guild || s.OriginChannelID != origin || !bounded(speaker, 100) {
		return ErrRealtime
	}
	if join {
		if !allowed(s, speaker) {
			if len(s.Consenting) >= 16 {
				return ErrRealtime
			}
			s.Consenting = append(s.Consenting, speaker)
		}
	} else {
		members := []string{}
		for _, v := range s.Consenting {
			if v != speaker {
				members = append(members, v)
			}
		}
		s.Consenting = members
		messages := []RealtimeMessage{}
		for _, m := range s.Messages {
			if m.SpeakerID != speaker {
				messages = append(messages, m)
			}
		}
		s.Messages = messages
	}
	s.UpdatedAt = e.now()
	e.sessions[id] = s
	return nil
}
func (e *ConversationContextEngine) Observe(guild, origin string, m RealtimeMessage) error {
	if m.CaptureSequence < 0 || m.CaptureSequence > 1000000000 || !bounded(m.MessageID, 128) || !bounded(m.Text, 8000) || !bounded(m.SpeakerID, 100) || m.Status != "final" || m.Revision < 1 || m.TimestampStart.IsZero() || m.TimestampEnd.Before(m.TimestampStart) || m.TimestampEnd.Sub(m.TimestampStart) > 30*time.Second {
		return ErrRealtime
	}
	// A platform display name is optional, bounded presentation text, never identity.
	if !utf8.ValidString(m.SpeakerName) {
		return ErrRealtime
	}
	name := []rune(strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, m.SpeakerName)))
	m.SpeakerName = string(name[:min(len(name), 100)])
	e.mu.Lock()
	defer e.mu.Unlock()
	e.prune()
	s, ok := e.sessions[m.SessionID]
	if !ok || !s.Active || s.GuildID != guild || s.OriginChannelID != origin || (!s.channelWide && !allowed(s, m.SpeakerID)) {
		return ErrRealtime
	}
	if !allowed(s, m.SpeakerID) {
		if len(s.Consenting) >= 16 {
			return ErrRealtime
		}
		s.Consenting = append(s.Consenting, m.SpeakerID)
	}
	// Ingest never trusts semantic metadata supplied by the interface.
	m.ThreadIDs = []string{}
	m.ReplyTo = ""
	for i, old := range s.Messages {
		if old.MessageID == m.MessageID {
			if old.SpeakerID != m.SpeakerID || old.CaptureSequence != m.CaptureSequence {
				return ErrRealtime
			}
			if m.Revision == old.Revision {
				if old.Text == m.Text && old.TimestampStart.Equal(m.TimestampStart) && old.TimestampEnd.Equal(m.TimestampEnd) {
					return nil
				}
				return ErrRealtime
			}
			if m.Revision != old.Revision+1 {
				return ErrRealtime
			}
			s.Messages[i] = m
			s.UpdatedAt = e.now()
			e.sessions[m.SessionID] = s
			return nil
		}
	}
	if m.Revision != 1 {
		return ErrRealtime
	}
	s.Messages = append(s.Messages, m)
	sort.SliceStable(s.Messages, func(i, j int) bool { return s.Messages[i].CaptureSequence < s.Messages[j].CaptureSequence })
	// Transcript count and byte budgets are independent of prompt/LLM budgets.
	total := 0
	for _, v := range s.Messages {
		total += len(v.Text)
	}
	for len(s.Messages) > 512 || total > 256000 {
		total -= len(s.Messages[0].Text)
		s.Messages = s.Messages[1:]
	}
	s.UpdatedAt = e.now()
	e.sessions[m.SessionID] = s
	return nil
}
func (e *ConversationContextEngine) Stop(id, guild, origin, speaker string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, ok := e.sessions[id]
	if !ok || s.GuildID != guild || s.OriginChannelID != origin || !bounded(speaker, 100) || (!s.channelWide && !allowed(s, speaker)) {
		return ErrRealtime
	}
	// End means delete in this ephemeral MVP. No implicit long-term retention.
	delete(e.sessions, id)
	return nil
}
func (e *ConversationContextEngine) Snapshot() []RealtimeSession {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.prune()
	out := []RealtimeSession{}
	ordered := make([]RealtimeSession, 0, len(e.sessions))
	for _, s := range e.sessions {
		ordered = append(ordered, s)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].UpdatedAt.After(ordered[j].UpdatedAt) })
	budget := 256000
	remaining := 512
	for _, s := range ordered {
		s.Consenting = append([]string{}, s.Consenting...)
		s.Messages = append([]RealtimeMessage{}, s.Messages...)
		for i := range s.Messages {
			s.Messages[i].ThreadIDs = append([]string{}, s.Messages[i].ThreadIDs...)
		}
		visible := []RealtimeMessage{}
		for i := len(s.Messages) - 1; i >= 0; i-- {
			m := s.Messages[i]
			if remaining == 0 || len(m.Text) > budget {
				break
			}
			budget -= len(m.Text)
			remaining--
			visible = append(visible, m)
		}
		for i, j := 0, len(visible)-1; i < j; i, j = i+1, j-1 {
			visible[i], visible[j] = visible[j], visible[i]
		}
		s.Messages = visible
		out = append(out, s)
	}
	return out
}
