package chat

import (
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

const VoiceFollowupWindow = 30 * time.Second
const VoiceHandoffWindow = 12 * time.Second

// VoiceDecision is conversational eligibility, independent of account authority.
type VoiceDecision struct {
	Reason      string `json:"reason"`
	Respond     bool   `json:"respond"`
	RequesterID string `json:"-"`
	Detailed    bool   `json:"detailed"`
}

type voiceRoom struct {
	listenerID   string
	engaged      map[string]time.Time
	handoffUntil time.Time
	lastSeen     time.Time
}
type VoicePolicy struct {
	mu    sync.Mutex
	rooms map[string]*voiceRoom
	now   func() time.Time
}

func NewVoicePolicy() *VoicePolicy {
	return &VoicePolicy{rooms: make(map[string]*voiceRoom), now: time.Now}
}

var addressedVoice = regexp.MustCompile(`(^|[^\pL\pN])(ningo|dokja)([^\pL\pN]|$)`)
var voiceWords = regexp.MustCompile(`[\pL\pN]+`)
var normalizeVoiceAccents = strings.NewReplacer("á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a", "é", "e", "ê", "e", "è", "e", "í", "i", "ì", "i", "ó", "o", "ô", "o", "õ", "o", "ö", "o", "ú", "u", "ü", "u", "ç", "c")

func normalizedVoice(text string) string {
	return strings.Join(voiceWords.FindAllString(normalizeVoiceAccents.Replace(strings.ToLower(text)), -1), " ")
}

// Backchannels remain context, but do not take the floor or extend engagement.
func VoiceBackchannel(text string) bool {
	if name := normalizedVoice(text); name == "ningo" || name == "dokja" {
		return false
	}
	text = addressedVoice.ReplaceAllString(normalizeVoiceAccents.Replace(strings.ToLower(text)), " ")
	switch normalizedVoice(text) {
	case "", "ok", "okay", "aham", "uhum", "hum", "hmm", "sim", "certo", "beleza", "entendi", "ta", "tá bom", "ta bom", "legal", "valeu", "obrigado", "obrigada", "thanks", "thank you", "yes", "yeah", "right", "sure", "got it", "kkk", "kkkk", "haha":
		return true
	}
	return false
}
func voiceFollowup(text string) bool {
	if strings.Contains(text, "?") {
		return true
	}
	normalized := normalizedVoice(text)
	for _, prefix := range []string{"e ", "mas ", "entao ", "por que ", "porque ", "como ", "qual ", "quais ", "isso ", "essa ", "esse ", "o que ", "but ", "and ", "why ", "how ", "what ", "that "} {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	return false
}
func voiceDetailed(text string) bool {
	normalized := normalizedVoice(text)
	for _, phrase := range []string{"explica melhor", "explique melhor", "mais detalhes", "em detalhes", "passo a passo", "explicacao completa", "explain more", "in detail", "step by step", "go deeper"} {
		if strings.Contains(normalized, phrase) {
			return true
		}
	}
	return false
}

// Decide opens an exchange on direct address, keeps that speaker engaged, and
// permits a brief, explicit question/continuation from another room participant.
func (p *VoicePolicy) roomLocked(scope string, now time.Time) *voiceRoom {
	room := p.rooms[scope]
	if room == nil {
		if len(p.rooms) >= 256 {
			oldest := ""
			var age time.Time
			for key, value := range p.rooms {
				if oldest == "" || value.lastSeen.Before(age) {
					oldest, age = key, value.lastSeen
				}
			}
			delete(p.rooms, oldest)
		}
		room = &voiceRoom{engaged: make(map[string]time.Time)}
		p.rooms[scope] = room
	}
	return room
}

// OpenListener treats an explicit voice-start command as addressing the assistant.
// Repeated turn metadata cannot keep extending the same listener's initial window.
func (p *VoicePolicy) OpenListener(scope, listenerID, starter string) bool {
	if listenerID == "" || starter == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	room := p.roomLocked(scope, now)
	if room.listenerID == listenerID {
		return false
	}
	room.listenerID = listenerID
	room.lastSeen = now
	room.engaged = map[string]time.Time{starter: now.Add(VoiceFollowupWindow)}
	room.handoffUntil = now.Add(VoiceHandoffWindow)
	return true
}

func (p *VoicePolicy) Decide(scope string, turns []VoiceTurn) VoiceDecision {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	room := p.roomLocked(scope, now)
	room.lastSeen = now
	for id, until := range room.engaged {
		if !now.Before(until) {
			delete(room.engaged, id)
		}
	}
	decision := VoiceDecision{Reason: "passive"}
	for _, turn := range turns {
		text := strings.TrimSpace(turn.Text)
		if VoiceBackchannel(text) {
			if !decision.Respond {
				decision.Reason = "backchannel"
			}
			continue
		}
		addressed := addressedVoice.MatchString(normalizeVoiceAccents.Replace(strings.ToLower(text)))

		if !addressed && !now.Before(room.engaged[turn.UserID]) && !(now.Before(room.handoffUntil) && voiceFollowup(text)) {
			continue
		}
		decision.Respond = true
		decision.Reason = "followup"
		if addressed {
			decision.Reason = "addressed"
		}
		decision.RequesterID = turn.UserID
		decision.Detailed = voiceDetailed(text)
		if len(room.engaged) < 64 || !room.engaged[turn.UserID].IsZero() {
			room.engaged[turn.UserID] = now.Add(VoiceFollowupWindow)
		}
		room.handoffUntil = now.Add(VoiceHandoffWindow)
	}
	return decision
}

// CompleteReply keeps the exchange open after speech, including a longer
// explanation. Estimate playback duration so queued follow-ups need no new name.
func (p *VoicePolicy) CompleteReply(scope string, decision VoiceDecision, reply string) {
	if decision.RequesterID == "" || strings.TrimSpace(reply) == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	room := p.rooms[scope]
	if room == nil {
		return
	}
	duration := time.Duration(len(strings.Fields(reply))) * 400 * time.Millisecond
	if duration > 2*time.Minute {
		duration = 2 * time.Minute
	}
	now := p.now()
	room.engaged[decision.RequesterID] = now.Add(duration + VoiceFollowupWindow)
	room.handoffUntil = now.Add(duration + VoiceHandoffWindow)
}

// ConciseSpokenReply enforces the spoken default without trimming an explicitly
// requested explanation. Keep decimals inside a sentence and avoid mid-word cuts.
func ConciseSpokenReply(reply string, detailed bool) string {
	reply = SpokenReply(reply)
	if detailed || reply == "" {
		return reply
	}
	runes := []rune(strings.TrimSpace(reply))
	for i, r := range runes {
		if (r == '.' || r == '!' || r == '?') && (i+1 == len(runes) || unicode.IsSpace(runes[i+1])) {
			reply = string(runes[:i+1])
			break
		}
	}
	words := strings.Fields(reply)
	if len(words) > 45 {
		return strings.Join(words[:45], " ") + "."
	}
	return reply
}

// Passive listening has a finite checkpoint and does not invoke summarization.
func BoundedVoiceHistory(messages []ContextMessage) []ContextMessage {
	start := len(messages)
	bytes := 0
	for start > 0 && len(messages)-start < 127 {
		size := len(messages[start-1].Content)
		if bytes+size > 128*1024 {
			break
		}
		bytes += size
		start--
	}
	return append([]ContextMessage(nil), messages[start:]...)
}
