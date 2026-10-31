package knowledge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	// ActionIntakeKnowledge keeps what the owner's chosen sources published. It is not a
	// service event: the orchestrator reads Feeds and hands the items to SelectIntake.
	ActionIntakeKnowledge = "intake-knowledge"

	// MaxIntakeAge is how far back a run looks. Knowledge keeps no record of what the owner
	// deleted, so anything still in a feed would come back on every run: the window bounds
	// that to a fresh item, and it stops the first run from swallowing a source's backlog.
	MaxIntakeAge = 72 * time.Hour
	// MaxIntakeDocuments bounds one run; the newest come first and the rest wait for the next.
	MaxIntakeDocuments = 20

	intakeKind      = "article"
	maxReadingIDLen = 120
)

// IntakeSelection is what one run takes from Feeds, and why it left the rest.
type IntakeSelection struct {
	Documents   []map[string]any
	WithoutBody int // an excerpt is not the text: fetching the page every run is not an option
	OutOfWindow int // older than MaxIntakeAge, or with no date to tell
	OverLimit   int
}

// ParseIntakeSources reads the comma-separated list of Feeds plugins chosen for intake.
func ParseIntakeSources(raw string) []string {
	seen := map[string]bool{}
	sources := []string{}
	for _, part := range strings.Split(raw, ",") {
		source := strings.TrimSpace(part)
		if source != "" && !seen[source] {
			seen[source] = true
			sources = append(sources, source)
		}
	}
	return sources
}

// ReadingSourceID is the identity of a reading in Knowledge. The desktop's "Save to the
// Library" computes the same one, so saving an item intake already kept updates that document
// instead of adding a copy. Change both together.
func ReadingSourceID(plugin, id string) (string, error) {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	// The hash is over JavaScript's JSON.stringify, which leaves &, < and > alone.
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode([]string{strings.TrimSpace(plugin), readingID(id)}); err != nil {
		return "", err
	}
	sum := sha256.Sum256(bytes.TrimSuffix(encoded.Bytes(), []byte("\n")))
	return "reading:" + hex.EncodeToString(sum[:]), nil
}

// readingID cuts an id where the desktop cuts it.
func readingID(id string) string {
	id = strings.TrimSpace(id)
	if runes := []rune(id); len(runes) > maxReadingIDLen {
		return string(runes[:maxReadingIDLen-1]) + "…"
	}
	return id
}

type intakeCandidate struct {
	published time.Time
	id        string
	payload   map[string]any
}

// SelectIntake turns a feeds.items reply into knowledge.ingest payloads: only the chosen
// sources, only items that carry their full text, only recent ones.
func SelectIntake(items any, sources []string, now time.Time) IntakeSelection {
	chosen := map[string]bool{}
	for _, source := range sources {
		chosen[source] = true
	}
	list, _ := items.([]any)
	var selection IntakeSelection
	candidates := make([]intakeCandidate, 0, len(list))
	for _, raw := range list {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		plugin := intakeText(item, "plugin")
		if !chosen[plugin] {
			continue
		}
		id, title, body := intakeText(item, "id"), intakeText(item, "title"), intakeText(item, "body")
		if id == "" || title == "" || body == "" {
			selection.WithoutBody++
			continue
		}
		published, err := time.Parse(time.RFC3339, intakeText(item, "published"))
		if err != nil || now.Sub(published) > MaxIntakeAge {
			selection.OutOfWindow++
			continue
		}
		sourceID, err := ReadingSourceID(plugin, id)
		if err != nil {
			selection.WithoutBody++
			continue
		}
		payload := map[string]any{"title": title, "body": body, "kind": intakeKind, "source_id": sourceID}
		if address := intakeText(item, "url"); address != "" {
			payload["source_ref"] = address
		}
		candidates = append(candidates, intakeCandidate{published: published, id: sourceID, payload: payload})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if !candidates[i].published.Equal(candidates[j].published) {
			return candidates[i].published.After(candidates[j].published)
		}
		return candidates[i].id < candidates[j].id
	})
	for n, candidate := range candidates {
		if n == MaxIntakeDocuments {
			selection.OverLimit = len(candidates) - n
			break
		}
		selection.Documents = append(selection.Documents, candidate.payload)
	}
	return selection
}

func intakeText(item map[string]any, field string) string {
	value, _ := item[field].(string)
	return strings.TrimSpace(value)
}

// IntakeOutcome counts what Knowledge did with a run. Stored includes unchanged documents.
type IntakeOutcome struct {
	Stored  int
	Changed int
	Failed  int
}

// StoreIntake ingests each document on its own, so one refusal does not cost the others. Every
// document has a stable identity, so one that fails is simply tried again by the next run. The
// returned error is the first failure.
func (d *Domain) StoreIntake(ctx context.Context, documents []map[string]any) (IntakeOutcome, error) {
	var outcome IntakeOutcome
	var first error
	for _, payload := range documents {
		if err := ctx.Err(); err != nil {
			outcome.Failed += len(documents) - outcome.Stored - outcome.Failed
			if first == nil {
				first = err
			}
			break
		}
		result, err := d.Handle(ctx, Request{
			Action: ActionIngestKnowledge,
			Event:  Event{Type: eventTypes[ActionIngestKnowledge], Payload: payload},
		})
		if err != nil {
			outcome.Failed++
			if first == nil {
				first = fmt.Errorf("intake: %w", err)
			}
			continue
		}
		outcome.Stored++
		if changed, _ := result["changed"].(bool); changed {
			outcome.Changed++
		}
	}
	return outcome, first
}
