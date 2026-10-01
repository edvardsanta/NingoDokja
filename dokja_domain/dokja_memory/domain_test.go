package memory

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeService records what it was sent and answers each event type from canned results.
type fakeService struct {
	answers map[string]map[string]any
	err     error
	sent    []sentEvent
}

type sentEvent struct {
	eventType string
	payload   map[string]any
}

func (f *fakeService) Dispatch(_ context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	f.sent = append(f.sent, sentEvent{eventType, payload})
	if f.err != nil {
		return nil, f.err
	}
	return f.answers[eventType], nil
}

func handle(d *Domain, action string, payload map[string]any) (map[string]any, error) {
	return d.Handle(context.Background(), Request{Action: action, Event: Event{Payload: payload}})
}

func TestHandleForwardsEachActionAsItsServiceEventWithACleanPayload(t *testing.T) {
	cases := []struct {
		action    string
		eventType string
		payload   map[string]any
		want      map[string]any
	}{
		{ActionRecordExperience, "memory.record",
			map[string]any{"ref": " m1 ", "action": "tag.suggest", "context": " some text ", "predicted_p": 0.7, "baseline_p": 0.6, "stray": "dropped"},
			map[string]any{"ref": "m1", "action": "tag.suggest", "context": "some text", "predicted_p": 0.7, "baseline_p": 0.6}},
		{ActionRecordExperience, "memory.record",
			map[string]any{"ref": "m1", "action": "tag.suggest", "context": "text"},
			map[string]any{"ref": "m1", "action": "tag.suggest", "context": "text"}},
		{ActionRecordExperience, "memory.record",
			map[string]any{"ref": "m1", "action": "tag.suggest", "context": "text", "detail": " #Label "},
			map[string]any{"ref": "m1", "action": "tag.suggest", "context": "text", "detail": "#Label"}},
		{ActionResolveExperience, "memory.resolve",
			map[string]any{"ref": "m1", "outcome": "accepted"},
			map[string]any{"ref": "m1", "outcome": "accepted"}},
		{ActionGetExperience, "memory.get", map[string]any{"ref": " m1 ", "stray": "dropped"}, map[string]any{"ref": "m1"}},
		{ActionRecallExperience, "memory.recall",
			map[string]any{"context": "text", "action": "tag.suggest", "k": 5},
			map[string]any{"context": "text", "action": "tag.suggest", "k": 5}},
		{ActionRecallExperience, "memory.recall",
			map[string]any{"context": "text"},
			map[string]any{"context": "text", "k": defaultRecallK}},
		{ActionForgetExperience, "memory.forget", map[string]any{"ref": "m1"}, map[string]any{"ref": "m1"}},
		{ActionInspectMemory, "memory.status", nil, map[string]any{}},
		{ActionReindexMemory, "memory.reindex", map[string]any{"limit": float64(40)}, map[string]any{"limit": 40}},
		{ActionReindexMemory, "memory.reindex", nil, map[string]any{}},
	}
	for _, tc := range cases {
		service := &fakeService{answers: map[string]map[string]any{tc.eventType: {"ok": true}}}
		answer, err := handle(New(service), tc.action, tc.payload)
		if err != nil {
			t.Fatalf("%s: %v", tc.action, err)
		}
		if answer["ok"] != true || len(service.sent) != 1 || service.sent[0].eventType != tc.eventType {
			t.Fatalf("%s: sent %+v, answered %v", tc.action, service.sent, answer)
		}
		got := service.sent[0].payload
		if len(got) != len(tc.want) {
			t.Fatalf("%s: forwarded %v, want %v", tc.action, got, tc.want)
		}
		for key, want := range tc.want {
			if got[key] != want {
				t.Fatalf("%s: forwarded %s=%v, want %v", tc.action, key, got[key], want)
			}
		}
	}
}

func TestHandleRejectsInvalidRequestsBeforeCallingTheService(t *testing.T) {
	long := strings.Repeat("x", maxContextRunes+1)
	cases := []struct {
		action  string
		payload map[string]any
		field   string
	}{
		{ActionRecordExperience, map[string]any{"action": "a", "context": "c"}, "ref"},
		{ActionRecordExperience, map[string]any{"ref": "m", "context": "c"}, "action"},
		{ActionRecordExperience, map[string]any{"ref": "m", "action": "Bad Name", "context": "c"}, "action"},
		{ActionRecordExperience, map[string]any{"ref": "m", "action": "a"}, "context"},
		{ActionRecordExperience, map[string]any{"ref": "m", "action": "a", "context": long}, "context"},
		{ActionRecordExperience, map[string]any{"ref": "bad\x00ref", "action": "a", "context": "c"}, "ref"},
		{ActionRecordExperience, map[string]any{"ref": strings.Repeat("r", maxRefLen+1), "action": "a", "context": "c"}, "ref"},
		{ActionRecordExperience, map[string]any{"ref": "m", "action": "a", "context": "c", "predicted_p": 0.5}, "go together"},
		{ActionRecordExperience, map[string]any{"ref": "m", "action": "a", "context": "c", "baseline_p": 0.5}, "go together"},
		{ActionRecordExperience, map[string]any{"ref": "m", "action": "a", "context": "c", "predicted_p": 1.5, "baseline_p": 0.5}, "between 0 and 1"},
		{ActionRecordExperience, map[string]any{"ref": "m", "action": "a", "context": "c", "predicted_p": 0.5, "baseline_p": -0.1}, "between 0 and 1"},
		{ActionRecordExperience, map[string]any{"ref": "m", "action": "a", "context": "c", "detail": strings.Repeat("d", maxDetailRunes+1)}, "detail"},
		{ActionRecordExperience, map[string]any{"ref": "m", "action": "a", "context": "c", "detail": "bad\x00detail"}, "detail"},
		{ActionResolveExperience, map[string]any{"ref": "m", "outcome": "maybe"}, "outcome"},
		{ActionResolveExperience, map[string]any{"outcome": "accepted"}, "ref"},
		{ActionResolveExperience, map[string]any{"ref": "m"}, "outcome or observed"},
		{ActionResolveExperience, map[string]any{"ref": "m", "outcome": "accepted", "observed": "x"}, "not both"},
		{ActionResolveExperience, map[string]any{"ref": "m", "observed": strings.Repeat("o", maxDetailRunes+1)}, "observed"},
		{ActionGetExperience, nil, "ref"},
		{ActionRecallExperience, map[string]any{"context": "c", "k": 0}, "k must"},
		{ActionRecallExperience, map[string]any{"context": "c", "k": maxRecallK + 1}, "k must"},
		{ActionRecallExperience, map[string]any{"context": "c", "action": "Bad Name"}, "action"},
		{ActionRecallExperience, map[string]any{"action": "a"}, "context"},
		{ActionForgetExperience, nil, "ref"},
		{ActionReindexMemory, map[string]any{"limit": 0}, "limit"},
		{ActionReindexMemory, map[string]any{"limit": maxReindexBatch + 1}, "limit"},
		{ActionPredictExperience, map[string]any{"context": "c"}, "action"},
		{ActionPredictExperience, map[string]any{"action": "a"}, "context"},
		{ActionScoreExperience, map[string]any{"action": "Bad Name"}, "action"},
	}
	for _, tc := range cases {
		service := &fakeService{}
		_, err := handle(New(service), tc.action, tc.payload)
		if err == nil || !strings.Contains(err.Error(), tc.field) {
			t.Fatalf("%s %v: expected an error naming %q, got %v", tc.action, tc.payload, tc.field, err)
		}
		if len(service.sent) != 0 {
			t.Fatalf("%s: the service must not be called for an invalid request", tc.action)
		}
	}
}

func TestPredictAsksTheServiceForNeighboursAndAnswersWithoutQuotingTheContext(t *testing.T) {
	service := &fakeService{answers: map[string]map[string]any{"memory.recall": {
		"neighbors": []any{
			map[string]any{"ref": "e1", "action": "tag.suggest", "outcome": "accepted", "similarity": 0.9},
			map[string]any{"ref": "e2", "action": "tag.suggest", "outcome": "accepted", "similarity": 0.8},
			map[string]any{"ref": "e3", "action": "tag.suggest", "outcome": "replaced", "similarity": 0.7},
		},
		"outcomes": map[string]any{"accepted": float64(8), "replaced": float64(4)},
	}}}

	answer, err := handle(New(service), ActionPredictExperience, map[string]any{"context": "private words", "action": "tag.suggest"})
	if err != nil {
		t.Fatal(err)
	}

	if len(service.sent) != 1 || service.sent[0].eventType != "memory.recall" {
		t.Fatalf("sent %+v", service.sent)
	}
	if service.sent[0].payload["action"] != "tag.suggest" || service.sent[0].payload["k"] != predictNeighbors {
		t.Fatalf("recall payload %v", service.sent[0].payload)
	}
	near(t, "baseline_p", answer["baseline_p"].(float64), 9.0/14.0)
	near(t, "predicted_p", answer["predicted_p"].(float64), (1.7+2*9.0/14.0)/4.4)
	if answer["insufficient"] != false || answer["support"] != 3 || answer["resolved"] != 12 || answer["degraded"] != false {
		t.Fatalf("answer %v", answer)
	}
	for key, value := range answer {
		if s, ok := value.(string); ok && strings.Contains(s, "private") {
			t.Fatalf("the answer must not carry the context (%s=%q)", key, s)
		}
	}
}

func TestPredictWithoutSimilarityFallsBackToTheBaselineAndReportsDegraded(t *testing.T) {
	service := &fakeService{answers: map[string]map[string]any{"memory.recall": {
		"neighbors": []any{},
		"outcomes":  map[string]any{"accepted": float64(6), "replaced": float64(6)},
		"degraded":  true,
		"reason":    "embedding server unreachable",
	}}}

	answer, err := handle(New(service), ActionPredictExperience, map[string]any{"context": "text", "action": "tag.suggest"})
	if err != nil {
		t.Fatal(err)
	}
	if answer["degraded"] != true || answer["insufficient"] != true || answer["predicted_p"] != answer["baseline_p"] {
		t.Fatalf("answer %v", answer)
	}
}

func TestScoreReadsResolvedExperiencesFromTheService(t *testing.T) {
	service := &fakeService{answers: map[string]map[string]any{"memory.resolved": {
		"experiences": []any{
			map[string]any{"ref": "1", "action": "a", "outcome": "accepted", "predicted_p": 0.8, "baseline_p": 0.5},
			map[string]any{"ref": "2", "action": "a", "outcome": "replaced", "predicted_p": 0.3, "baseline_p": 0.5},
			map[string]any{"ref": "3", "action": "a", "outcome": "expired", "predicted_p": nil, "baseline_p": nil},
		},
	}}}

	answer, err := handle(New(service).WithConfig(Config{MinScored: 2}), ActionScoreExperience, map[string]any{"action": "a"})
	if err != nil {
		t.Fatal(err)
	}
	if service.sent[0].eventType != "memory.resolved" || service.sent[0].payload["action"] != "a" {
		t.Fatalf("sent %+v", service.sent)
	}
	if answer["scored"] != 2 || answer["unscored"] != 1 || answer["enough_data"] != true || answer["beats_baseline"] != true {
		t.Fatalf("answer %v", answer)
	}
	near(t, "brier_prediction", answer["brier_prediction"].(float64), (0.04+0.09)/2)
}

func TestHandlePropagatesServiceErrors(t *testing.T) {
	boom := errors.New("service down")
	for _, action := range []string{ActionInspectMemory, ActionPredictExperience, ActionScoreExperience} {
		service := &fakeService{err: boom}
		_, err := handle(New(service), action, map[string]any{"context": "c", "action": "a"})
		if !errors.Is(err, boom) {
			t.Fatalf("%s: expected the service error, got %v", action, err)
		}
	}
}

func TestHandleRejectsUnknownActionsAndMissingWiring(t *testing.T) {
	if _, err := handle(New(&fakeService{}), "nope", nil); err == nil {
		t.Fatal("expected an unsupported action error")
	}
	if _, err := handle(New(nil), ActionInspectMemory, nil); err == nil {
		t.Fatal("expected a not configured error")
	}
	if _, err := (*Domain)(nil).Handle(context.Background(), Request{}); err == nil {
		t.Fatal("expected a nil domain error")
	}
}

func TestResolvingByWhatWasObservedComparesItWithWhatTheBotDid(t *testing.T) {
	cases := []struct {
		observed string
		detail   string
		outcome  string
		matched  bool
	}{
		{"#Label", "#Label", OutcomeAccepted, true},
		{" #label ", "#Label", OutcomeAccepted, true},
		{"#Other", "#Label", OutcomeReplaced, false},
	}
	for _, tc := range cases {
		service := &fakeService{answers: map[string]map[string]any{
			"memory.get":     {"found": true, "detail": tc.detail},
			"memory.resolve": {"found": true, "resolved": true, "outcome": tc.outcome},
		}}

		answer, err := handle(New(service), ActionResolveExperience, map[string]any{"ref": "m1", "observed": tc.observed})
		if err != nil {
			t.Fatalf("%q vs %q: %v", tc.observed, tc.detail, err)
		}

		if len(service.sent) != 2 || service.sent[0].eventType != "memory.get" || service.sent[1].eventType != "memory.resolve" {
			t.Fatalf("expected a get then a resolve, sent %+v", service.sent)
		}
		if service.sent[1].payload["outcome"] != tc.outcome || service.sent[1].payload["ref"] != "m1" {
			t.Fatalf("%q vs %q: resolved with %v, want %s", tc.observed, tc.detail, service.sent[1].payload, tc.outcome)
		}
		if answer["matched"] != tc.matched || answer["resolved"] != true {
			t.Fatalf("answer %v", answer)
		}
	}
}

func TestResolvingByObservationNeedsAnExperienceWithADetail(t *testing.T) {
	missing := &fakeService{answers: map[string]map[string]any{"memory.get": {"found": false}}}
	answer, err := handle(New(missing), ActionResolveExperience, map[string]any{"ref": "nope", "observed": "#Label"})
	if err != nil || answer["found"] != false || answer["resolved"] != false {
		t.Fatalf("an unknown experience is not an error, just not found: %v %v", answer, err)
	}
	if len(missing.sent) != 1 {
		t.Fatalf("nothing may be resolved when the experience is unknown: %+v", missing.sent)
	}

	bare := &fakeService{answers: map[string]map[string]any{"memory.get": {"found": true, "detail": ""}}}
	if _, err := handle(New(bare), ActionResolveExperience, map[string]any{"ref": "m1", "observed": "#Label"}); err == nil ||
		!strings.Contains(err.Error(), "no detail") || len(bare.sent) != 1 {
		t.Fatalf("an experience with no detail cannot be compared: %v %+v", err, bare.sent)
	}
}

func TestResolvingByObservationPropagatesServiceErrors(t *testing.T) {
	boom := errors.New("service down")
	if _, err := handle(New(&fakeService{err: boom}), ActionResolveExperience, map[string]any{"ref": "m1", "observed": "x"}); !errors.Is(err, boom) {
		t.Fatalf("expected the service error, got %v", err)
	}
}
