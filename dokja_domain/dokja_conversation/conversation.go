// Package conversation is the archive of the owner's conversations with Ningo: what was said, what
// Ningo answered and how each send ended. It holds the rules only, which interfaces may read it,
// what a turn may become, what is bounded and which turns may become context for the next reply.
// Storage implements Store and decides nothing.
//
// The archive is the transcript and nothing more. A turn kept here is not knowledge and does not
// resolve an experience; the model's context is a bounded selection from it, never the whole of it.
package conversation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Scope is who a conversation belongs to. The owner's local interfaces share one scope, so a
// conversation begun on the desktop can be read and continued from the CLI or the TUI.
type Scope string

const OwnerScope Scope = "owner"

// ownerInterfaces are the sources that speak for the owner. A channel on Discord or WhatsApp is not
// the owner, and its history belongs to its own platform.
var ownerInterfaces = map[string]bool{"desktop": true, "cli": true, "tui": true}

// ScopeFor answers the scope a source may use, and false when it may use none.
func ScopeFor(source string) (Scope, bool) {
	if ownerInterfaces[source] {
		return OwnerScope, true
	}
	return "", false
}

type Kind string

const (
	KindGeneral Kind = "general"
	// A reading discussion is about one reading and keeps its reference.
	KindReading Kind = "reading"
)

type TurnState string

const (
	// Sent and not answered yet. A displayed send is not proof the reply was written.
	TurnPending   TurnState = "pending"
	TurnCompleted TurnState = "completed"
	TurnFailed    TurnState = "failed"
	// The orchestrator stopped while the turn was pending; nothing knows whether a reply was written.
	TurnInterrupted TurnState = "interrupted"
)

type Conversation struct {
	ID        string
	Kind      Kind
	ReadingID string
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
	// Revision grows with every change, so a rename or a delete names the version it saw.
	Revision uint64
	Turns    int
}

// Provenance is which provider and model answered. Selected is what the owner chose; Observed is
// what the runtime reported it used, which may be empty when it reported nothing.
type Provenance struct {
	SelectedProvider string
	SelectedModel    string
	ObservedProvider string
	ObservedModel    string
}

// Turn is one send and its outcome: the owner's message and, once there is one, Ningo's reply.
type Turn struct {
	ID             string
	ConversationID string
	// Seq orders turns within a conversation; the store assigns it.
	Seq int
	// SendID is the interface's own name for the send, kept the same when it retries, so a retry
	// returns the turn it already made instead of making another.
	SendID  string
	Content string
	Reply   string
	State   TurnState
	Reason  string
	// Attempts counts the times the turn was sent: 1, and one more for each retry.
	// A completion/failure carries this snapshot unchanged; it may settle only
	// that attempt, never a later retry of the same turn.
	Attempts   int
	Provenance Provenance
	CreatedAt  time.Time
	FinishedAt time.Time
}

// Bounds. A conversation's text is the owner's, and a reply is bounded like the chat reply itself.
const (
	MaxTitleRunes   = 120
	MaxContentRunes = 12000
	MaxReplyRunes   = 24000
	MaxReasonRunes  = 300
	MaxReadingID    = 512
	MaxListLimit    = 50
	MaxTurnLimit    = 100
	maxCursor       = 128
)

var (
	ErrNotFound = errors.New("conversation not found")
	// ErrConflict: the version named is not the current one, or the turn already ended.
	ErrConflict = errors.New("conversation changed; read it again")
	ErrInvalid  = errors.New("invalid conversation request")
)

var (
	opaqueID  = regexp.MustCompile(`^[0-9a-f]{32}$`)
	sendID    = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
	cursorFmt = regexp.MustCompile(`^[0-9]{1,20}\.[0-9a-f]{32}$`)
	modelFmt  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
	providers = map[string]bool{"": true, "codex": true, "claude_code": true, "ollama": true}
)

// Store keeps conversations and turns. Every call names the scope, and a row of another scope is
// as absent as a row that does not exist.
type Store interface {
	Create(ctx context.Context, scope Scope, c Conversation) (Conversation, error)
	Get(ctx context.Context, scope Scope, id string) (Conversation, error)
	// List gives the most recently changed first; next is empty on the last page.
	List(ctx context.Context, scope Scope, cursor string, limit int) (items []Conversation, next string, err error)
	Rename(ctx context.Context, scope Scope, id string, revision uint64, title string, at time.Time) (Conversation, error)
	Delete(ctx context.Context, scope Scope, id string, revision uint64) error
	// BeginTurn records a pending turn, or returns the one already recorded for its send ID with
	// replayed true. The same send ID with other text is ErrConflict.
	BeginTurn(ctx context.Context, scope Scope, t Turn) (turn Turn, replayed bool, err error)
	// RetryTurn makes a failed or interrupted turn pending again, the same turn in the same place,
	// one attempt more. Only the conversation's latest turn may be retried; anything else, or a turn
	// in any other state, is ErrConflict.
	RetryTurn(ctx context.Context, scope Scope, turnID string, at time.Time) (Turn, error)
	// FinishTurn ends the pending turn only if its stored Attempts matches t.Attempts.
	// A stale attempt or already ended turn answers ErrConflict without mutation.
	FinishTurn(ctx context.Context, scope Scope, t Turn) (Turn, error)
	// Turns gives up to limit turns before seq (0: the latest), oldest first.
	Turns(ctx context.Context, scope Scope, conversationID string, beforeSeq int, limit int) ([]Turn, error)
	// InterruptPending marks every turn still pending since before the instant as interrupted.
	InterruptPending(ctx context.Context, before time.Time) (int, error)
}

// Archive applies the rules over a Store.
type Archive struct {
	store Store
	now   func() time.Time
	newID func() string
}

func New(store Store) *Archive {
	return &Archive{store: store, now: func() time.Time { return time.Now().UTC() }, newID: randomID}
}

func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("conversation id: %v", err))
	}
	return hex.EncodeToString(b)
}

func (a *Archive) Start(ctx context.Context, scope Scope, kind Kind, readingID, title string) (Conversation, error) {
	if scope != OwnerScope || (kind != KindGeneral && kind != KindReading) {
		return Conversation{}, ErrInvalid
	}
	if kind == KindReading && (readingID == "" || len(readingID) > MaxReadingID) || kind == KindGeneral && readingID != "" {
		return Conversation{}, ErrInvalid
	}
	title, ok := cleanTitle(title)
	if !ok {
		return Conversation{}, ErrInvalid
	}
	at := a.now()
	return a.store.Create(ctx, scope, Conversation{ID: a.newID(), Kind: kind, ReadingID: readingID, Title: title, CreatedAt: at, UpdatedAt: at, Revision: 1})
}

func (a *Archive) Get(ctx context.Context, scope Scope, id string) (Conversation, error) {
	if scope != OwnerScope || !opaqueID.MatchString(id) {
		return Conversation{}, ErrNotFound
	}
	return a.store.Get(ctx, scope, id)
}

func (a *Archive) List(ctx context.Context, scope Scope, cursor string, limit int) ([]Conversation, string, error) {
	if scope != OwnerScope || cursor != "" && (len(cursor) > maxCursor || !cursorFmt.MatchString(cursor)) {
		return nil, "", ErrInvalid
	}
	return a.store.List(ctx, scope, cursor, clamp(limit, MaxListLimit))
}

func (a *Archive) Rename(ctx context.Context, scope Scope, id string, revision uint64, title string) (Conversation, error) {
	title, ok := cleanTitle(title)
	if scope != OwnerScope || !opaqueID.MatchString(id) || revision == 0 || !ok {
		return Conversation{}, ErrInvalid
	}
	return a.store.Rename(ctx, scope, id, revision, title, a.now())
}

// Delete removes the conversation and its turns. Experiences recorded from it are their own
// records and stay.
func (a *Archive) Delete(ctx context.Context, scope Scope, id string, revision uint64) error {
	if scope != OwnerScope || !opaqueID.MatchString(id) || revision == 0 {
		return ErrInvalid
	}
	return a.store.Delete(ctx, scope, id, revision)
}

// Send records the owner's message as a pending turn. When the same send ID was recorded before,
// it returns that turn with replayed true and the caller must not ask the model again: a completed
// turn is its answer, and a pending one is still on its way.
func (a *Archive) Send(ctx context.Context, scope Scope, conversationID, send, content string, selected Provenance) (Turn, bool, error) {
	content = strings.TrimSpace(content)
	if scope != OwnerScope || !opaqueID.MatchString(conversationID) || !sendID.MatchString(send) || content == "" || utf8.RuneCountInString(content) > MaxContentRunes || !validProvenance(selected.SelectedProvider, selected.SelectedModel) {
		return Turn{}, false, ErrInvalid
	}
	return a.store.BeginTurn(ctx, scope, Turn{
		ID: a.newID(), ConversationID: conversationID, SendID: send, Content: content, State: TurnPending, Attempts: 1,
		Provenance: Provenance{SelectedProvider: selected.SelectedProvider, SelectedModel: selected.SelectedModel}, CreatedAt: a.now(),
	})
}

// Retry sends a failed or interrupted turn again, on the owner's explicit request only: the same
// turn, so the owner's message is never shown twice. Nothing retries by itself, after a restart or
// otherwise. Only the latest turn may be retried, so a late reply never lands mid-transcript.
func (a *Archive) Retry(ctx context.Context, scope Scope, turnID string) (Turn, error) {
	if scope != OwnerScope || !opaqueID.MatchString(turnID) {
		return Turn{}, ErrInvalid
	}
	return a.store.RetryTurn(ctx, scope, turnID, a.now())
}

// Complete records Ningo's reply and the model the runtime says it used.
func (a *Archive) Complete(ctx context.Context, scope Scope, turn Turn, reply string, observedProvider, observedModel string) (Turn, error) {
	reply = strings.TrimSpace(reply)
	if scope != OwnerScope || !opaqueID.MatchString(turn.ID) || reply == "" || !validProvenance(observedProvider, observedModel) {
		return Turn{}, ErrInvalid
	}
	turn.Reply, turn.State, turn.Reason = truncate(reply, MaxReplyRunes), TurnCompleted, ""
	turn.Provenance.ObservedProvider, turn.Provenance.ObservedModel = observedProvider, observedModel
	turn.FinishedAt = a.now()
	return a.store.FinishTurn(ctx, scope, turn)
}

// Fail records that the turn got no reply, with a plain reason for the owner.
func (a *Archive) Fail(ctx context.Context, scope Scope, turn Turn, reason string) (Turn, error) {
	if scope != OwnerScope || !opaqueID.MatchString(turn.ID) {
		return Turn{}, ErrInvalid
	}
	turn.Reply, turn.State, turn.Reason = "", TurnFailed, truncate(strings.TrimSpace(reason), MaxReasonRunes)
	turn.FinishedAt = a.now()
	return a.store.FinishTurn(ctx, scope, turn)
}

func (a *Archive) Turns(ctx context.Context, scope Scope, conversationID string, beforeSeq, limit int) ([]Turn, error) {
	if scope != OwnerScope || !opaqueID.MatchString(conversationID) || beforeSeq < 0 {
		return nil, ErrInvalid
	}
	return a.store.Turns(ctx, scope, conversationID, beforeSeq, clamp(limit, MaxTurnLimit))
}

// Recover marks the turns a stopped orchestrator left pending as interrupted. It runs once at start,
// for turns begun before it; nothing is sent again.
func (a *Archive) Recover(ctx context.Context, startedAt time.Time) (int, error) {
	return a.store.InterruptPending(ctx, startedAt)
}

// ContextTurns picks the turns that may become context for the next reply: completed ones only,
// the newest first until either budget is spent, given back in their order. A failed, pending or
// interrupted turn never becomes context.
func ContextTurns(turns []Turn, maxTurns, maxRunes int) []Turn {
	picked := []Turn{}
	used := 0
	for i := len(turns) - 1; i >= 0 && len(picked) < maxTurns; i-- {
		t := turns[i]
		if t.State != TurnCompleted {
			continue
		}
		size := utf8.RuneCountInString(t.Content) + utf8.RuneCountInString(t.Reply)
		if used+size > maxRunes {
			break
		}
		used += size
		picked = append(picked, t)
	}
	for i, j := 0, len(picked)-1; i < j; i, j = i+1, j-1 {
		picked[i], picked[j] = picked[j], picked[i]
	}
	return picked
}

func cleanTitle(title string) (string, bool) {
	title = strings.Join(strings.Fields(title), " ")
	return title, utf8.RuneCountInString(title) <= MaxTitleRunes
}

func validProvenance(provider, model string) bool {
	return providers[provider] && (model == "" || modelFmt.MatchString(model)) && (provider == "") == (model == "")
}

func clamp(limit, max int) int {
	if limit <= 0 || limit > max {
		return max
	}
	return limit
}

func truncate(text string, max int) string {
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	return string([]rune(text)[:max])
}
