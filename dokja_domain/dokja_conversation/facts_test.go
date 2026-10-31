package conversation

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type memoryFacts struct{ held []Fact }

func (m *memoryFacts) AddFact(_ context.Context, fact Fact) error {
	m.held = append(m.held, fact)
	return nil
}

func (m *memoryFacts) PersonFacts(_ context.Context, person string) ([]Fact, error) {
	found := []Fact{}
	for _, fact := range m.held {
		if fact.Person == person {
			found = append(found, fact)
		}
	}
	return found, nil
}

func (m *memoryFacts) AllFacts(context.Context) ([]Fact, error) { return m.held, nil }

func (m *memoryFacts) DeleteFact(_ context.Context, id string) (bool, error) {
	for index, fact := range m.held {
		if fact.ID == id {
			m.held = append(m.held[:index], m.held[index+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func TestAFactIsKeptForWhoeverAskedAndNobodyElse(t *testing.T) {
	ctx := context.Background()
	facts := NewFacts(&memoryFacts{})
	if _, err := facts.Remember(ctx, PersonKey("Ana"), "whatsapp:group", "  likes   tea "); err != nil {
		t.Fatal(err)
	}
	held, err := facts.Recall(ctx, PersonKey("Ana"), "whatsapp:group")
	if err != nil || len(held) != 1 || held[0].Content != "likes tea" {
		t.Fatal(held, err)
	}
	if other, _ := facts.Recall(ctx, OwnerPerson, PlacePrivate); len(other) != 0 {
		t.Fatal("a fact must not reach another person", other)
	}
}

func TestAPrivateFactNeverSurfacesInASharedPlace(t *testing.T) {
	ctx := context.Background()
	facts := NewFacts(&memoryFacts{})
	facts.Remember(ctx, OwnerPerson, PlacePrivate, "private thing")
	facts.Remember(ctx, OwnerPerson, "whatsapp:one", "said in one")
	facts.Remember(ctx, OwnerPerson, "whatsapp:two", "said in two")
	shown, _ := facts.Recall(ctx, OwnerPerson, "whatsapp:one")
	if len(shown) != 1 || shown[0].Content != "said in one" {
		t.Fatal(shown)
	}
	if all, _ := facts.Recall(ctx, OwnerPerson, PlacePrivate); len(all) != 3 {
		t.Fatal("a private place shows everything of the person", all)
	}
}

func TestRememberRefusesWhatItCannotKeep(t *testing.T) {
	ctx := context.Background()
	facts := NewFacts(&memoryFacts{})
	for _, content := range []string{"", "   ", strings.Repeat("x", MaxFactRunes+1)} {
		if _, err := facts.Remember(ctx, OwnerPerson, PlacePrivate, content); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
	for range MaxFactsPerPerson {
		if _, err := facts.Remember(ctx, OwnerPerson, PlacePrivate, "x"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := facts.Remember(ctx, OwnerPerson, PlacePrivate, "one more"); !errors.Is(err, ErrTooManyFacts) {
		t.Fatal(err)
	}
}

func TestDeleteRemovesAFactForGood(t *testing.T) {
	ctx := context.Background()
	facts := NewFacts(&memoryFacts{})
	kept, _ := facts.Remember(ctx, OwnerPerson, PlacePrivate, "x")
	if err := facts.Delete(ctx, kept.ID); err != nil {
		t.Fatal(err)
	}
	if err := facts.Delete(ctx, kept.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := facts.Delete(ctx, "not an id"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

func TestContextFactsKeepsTheLatestWithinBounds(t *testing.T) {
	many := []Fact{}
	for index := range MaxContextFacts + 5 {
		many = append(many, Fact{Content: string(rune('a' + index%26))})
	}
	kept := ContextFacts(many)
	if len(kept) != MaxContextFacts || kept[len(kept)-1] != many[len(many)-1].Content {
		t.Fatal(kept)
	}
	big := []Fact{{Content: "old"}, {Content: strings.Repeat("x", MaxContextFactRunes)}, {Content: "new"}}
	if kept := ContextFacts(big); len(kept) != 1 || kept[0] != "new" {
		t.Fatal(len(kept))
	}
}
