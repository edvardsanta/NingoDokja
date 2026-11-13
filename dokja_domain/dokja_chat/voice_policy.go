package chat

import (
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

// VoiceKnowledgeLookupBudget bounds optional retrieval on the spoken response path.
const VoiceKnowledgeLookupBudget = 100 * time.Millisecond

const VoiceFollowupWindow = 30 * time.Second
const VoiceHandoffWindow = 12 * time.Second
const VoiceBriefMaxSentences = 1
const VoiceBriefMaxWords = 25
const VoiceDefaultMaxSentences = 2
const VoiceDefaultMaxWords = 45

// VoiceDecision is conversational eligibility, independent of account authority.
type VoiceDecision struct {
	Reason      string `json:"reason"`
	Respond     bool   `json:"respond"`
	RequesterID string `json:"-"`
	Detailed    bool   `json:"detailed"`
	Brief       bool   `json:"brief"`
}

type voiceRoom struct {
	listenerID      string
	listenerStarter string
	engaged         map[string]time.Time
	handoffUntil    time.Time
	lastSeen        time.Time
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
var addressedVoiceVariant = regexp.MustCompile(`^(?:e ai |oi |ola |hey )?(lingo|ningu|unigo)([^\pL\pN]|$)`)
var voiceName = regexp.MustCompile(`(^|[^\pL\pN])(ningo|dokja|lingo|ningu|unigo)([^\pL\pN]|$)`)
var voiceWords = regexp.MustCompile(`[\pL\pN]+`)
var normalizeVoiceAccents = strings.NewReplacer("á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a", "é", "e", "ê", "e", "è", "e", "í", "i", "ì", "i", "ó", "o", "ô", "o", "õ", "o", "ö", "o", "ú", "u", "ü", "u", "ç", "c")

func normalizedVoice(text string) string {
	return strings.Join(voiceWords.FindAllString(normalizeVoiceAccents.Replace(strings.ToLower(text)), -1), " ")
}

// VoiceAddressed accepts likely ASR variants only at the start of an utterance.
func VoiceAddressed(text string) bool {
	normalized := normalizeVoiceAccents.Replace(strings.ToLower(text))
	return addressedVoice.MatchString(normalized) || addressedVoiceVariant.MatchString(normalizedVoice(text))
}

// Backchannels remain context, but do not take the floor or extend engagement.
func VoiceBackchannel(text string) bool {
	if name := normalizedVoice(text); name == "ningo" || name == "dokja" || name == "lingo" || name == "ningu" || name == "unigo" {
		return false
	}
	text = voiceName.ReplaceAllString(normalizeVoiceAccents.Replace(strings.ToLower(text)), " ")
	switch normalizedVoice(text) {
	case "", "ok", "okay", "aham", "uhum", "hum", "hmm", "sim", "certo", "beleza", "entendi", "ta", "tá bom", "ta bom", "legal", "valeu", "obrigado", "obrigada", "e ai", "eai", "thanks", "thank you", "yes", "yeah", "right", "sure", "got it", "kkk", "kkkk", "haha":
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

func voiceBrief(text string) bool {
	if strings.Contains(text, "?") || voiceDetailed(text) {
		return false
	}
	normalized := normalizedVoice(text)
	if len(strings.Fields(normalized)) > 16 {
		return false
	}
	for _, phrase := range []string{"explica", "explique", "analisa", "analyse", "resume", "resuma", "argumenta", "argumente", "defende", "defenda", "desenvolve", "desenvolva", "por que", "porque", "como ", "explain", "analyze", "summarize", "argument", "observation"} {
		if strings.Contains(normalized, phrase) {
			return false
		}
	}
	return true
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
// The starter stays engaged for the listener lifetime; other speakers use the
// ordinary addressing and follow-up windows. Repeated metadata cannot replace it.
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
	room.listenerStarter = starter
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
		if turn.AudioKind == VoiceAudioLaughter {
			if !decision.Respond {
				decision.Reason = "laughter"
			}
			continue
		}
		if VoiceBackchannel(text) {
			if !decision.Respond {
				decision.Reason = "backchannel"
			}
			continue
		}
		addressed := VoiceAddressed(text)

		listenerStarter := room.listenerID != "" && room.listenerStarter == turn.UserID
		if !listenerStarter && !addressed && !now.Before(room.engaged[turn.UserID]) && !(now.Before(room.handoffUntil) && voiceFollowup(text)) {
			continue
		}
		decision.Respond = true
		decision.Reason = "followup"
		if addressed {
			decision.Reason = "addressed"
		}
		decision.RequesterID = turn.UserID
		decision.Detailed = voiceDetailed(text)
		decision.Brief = voiceBrief(text)
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

func VoiceSentenceLimit(decision VoiceDecision) int {
	if decision.Brief {
		return VoiceBriefMaxSentences
	}
	return VoiceDefaultMaxSentences
}

// ConciseSpokenReply enforces the spoken default without trimming an explicitly
// requested explanation. Keep decimals inside a sentence and avoid mid-word cuts.
func ConciseSpokenReply(reply string, decision VoiceDecision) string {
	reply = SpokenReply(reply)
	if decision.Detailed || reply == "" {
		return reply
	}
	maxWords := VoiceDefaultMaxWords
	if decision.Brief {
		maxWords = VoiceBriefMaxWords
	}
	runes := []rune(strings.TrimSpace(reply))
	sentences := 0
	for i, r := range runes {
		if (r == '.' || r == '!' || r == '?') && (i+1 == len(runes) || unicode.IsSpace(runes[i+1])) {
			sentences++
			if sentences == VoiceSentenceLimit(decision) {
				reply = string(runes[:i+1])
				break
			}
		}
	}
	words := strings.Fields(reply)
	if len(words) > maxWords {
		return strings.Join(words[:maxWords], " ") + "."
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
