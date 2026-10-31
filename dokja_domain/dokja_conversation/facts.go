package conversation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Facts are what a known person explicitly asked Ningo to keep. A fact belongs to whoever asked and is
// never inferred. It is not a turn of a conversation: deleting a conversation leaves it, and deleting
// a fact leaves the conversation where it was said.
const (
	// OwnerPerson is whose facts the owner's are, on any of the channels the owner is verified on.
	OwnerPerson = "owner"
	// PlacePrivate is a place only the person reads. Any other place is shared, and is named.
	PlacePrivate = "private"

	MaxFactRunes      = 1000
	MaxFactsPerPerson = 200
	// A turn's context holds the latest facts within these bounds.
	MaxContextFacts     = 30
	MaxContextFactRunes = 3000
)

// ErrTooManyFacts refuses a fact before anything is recorded.
var ErrTooManyFacts = errors.New("too many facts are kept for this person")

// PersonKey names a known person other than the owner.
func PersonKey(name string) string { return "person:" + name }

// PersonName gives the name a person's key was made from, and empty for the owner.
func PersonName(person string) string { return strings.TrimPrefix(person, "person:") }

type Fact struct {
	ID        string
	Person    string
	Content   string
	Place     string
	CreatedAt time.Time
}

// FactStore keeps facts, oldest first. It decides nothing.
type FactStore interface {
	AddFact(ctx context.Context, fact Fact) error
	PersonFacts(ctx context.Context, person string) ([]Fact, error)
	AllFacts(ctx context.Context) ([]Fact, error)
	DeleteFact(ctx context.Context, id string) (bool, error)
}

type Facts struct {
	store FactStore
	now   func() time.Time
}

func NewFacts(store FactStore) *Facts {
	return &Facts{store: store, now: func() time.Time { return time.Now().UTC() }}
}

// Remember keeps content for person, as said in place.
func (f *Facts) Remember(ctx context.Context, person, place, content string) (Fact, error) {
	content = strings.Join(strings.Fields(content), " ")
	if person == "" || place == "" || content == "" || utf8.RuneCountInString(content) > MaxFactRunes {
		return Fact{}, ErrInvalid
	}
	held, err := f.store.PersonFacts(ctx, person)
	if err != nil {
		return Fact{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if len(held) >= MaxFactsPerPerson {
		return Fact{}, ErrTooManyFacts
	}
	fact := Fact{ID: randomID(), Person: person, Content: content, Place: place, CreatedAt: f.now()}
	if err := f.store.AddFact(ctx, fact); err != nil {
		return Fact{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return fact, nil
}

// Recall gives person's facts that may appear in place. A private place shows them all. A shared
// place shows only what was said there, so nothing kept in private surfaces in front of others, even
// when the person asks there.
func (f *Facts) Recall(ctx context.Context, person, place string) ([]Fact, error) {
	held, err := f.store.PersonFacts(ctx, person)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if place == PlacePrivate {
		return held, nil
	}
	shown := make([]Fact, 0, len(held))
	for _, fact := range held {
		if fact.Place == place {
			shown = append(shown, fact)
		}
	}
	return shown, nil
}

// ContextFacts selects what a turn's model may see: the latest of what Recall allows, within bounds.
func ContextFacts(facts []Fact) []string {
	kept := []string{}
	runes := 0
	for index := len(facts) - 1; index >= 0 && len(kept) < MaxContextFacts; index-- {
		runes += utf8.RuneCountInString(facts[index].Content)
		if runes > MaxContextFactRunes {
			break
		}
		kept = append(kept, facts[index].Content)
	}
	for left, right := 0, len(kept)-1; left < right; left, right = left+1, right-1 {
		kept[left], kept[right] = kept[right], kept[left]
	}
	return kept
}

// All gives every person's facts, for the owner to see.
func (f *Facts) All(ctx context.Context) ([]Fact, error) {
	held, err := f.store.AllFacts(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return held, nil
}

// Delete removes a fact for good.
func (f *Facts) Delete(ctx context.Context, id string) error {
	if !opaqueID.MatchString(id) {
		return ErrInvalid
	}
	found, err := f.store.DeleteFact(ctx, id)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if !found {
		return ErrNotFound
	}
	return nil
}
