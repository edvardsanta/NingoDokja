//go:build unix

package feeds

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func newClassifier(t *testing.T, root, id string, enabled bool) {
	t.Helper()
	newPlugin(t, root, id, v1(map[string]any{
		"id": id, "enabled": enabled, "provides": []string{CapabilityDocumentClassify}, "formats": nil,
	}))
}

func classifyPayload(texts ...string) map[string]any {
	documents := make([]any, len(texts))
	for i, text := range texts {
		documents[i] = map[string]any{"title": "Part", "text": text, "key": "k"}
	}
	return map[string]any{"format": ".PDF", "documents": documents}
}

func proposalsOutput(proposals ...map[string]any) []byte {
	body, _ := json.Marshal(map[string]any{"classifications": proposals})
	return body
}

func TestClassifyHandsTheDocumentsToTheClassifierAndReturnsItsProposals(t *testing.T) {
	root := t.TempDir()
	newClassifier(t, root, "kinds", true)
	newExtractor(t, root, "reader", true, ".pdf")
	runner := &inputRunner{reply: func(Plugin) ([]byte, error) {
		return proposalsOutput(
			map[string]any{"form": "article", "content_kind": "news", "topics": []string{" elections "},
				"entities": []map[string]string{{"name": "City Council", "type": "organization"}}, "date_anchor": "2026-10-19"},
			map[string]any{"form": "book", "uncertain_content_kind": true},
		), nil
	}}
	service := extractService(root, runner)

	long := strings.Repeat("é", maxClassifySample+50)
	result, err := service.Dispatch(context.Background(), "feeds.classify", classifyPayload("first", long))
	if err != nil {
		t.Fatal(err)
	}
	proposals := result["classifications"].([]Proposal)
	if result["plugin"] != "kinds" || result["version"] != "0.1.0" || result["count"] != 2 ||
		proposals[0].ContentKind != "news" || proposals[0].Topics[0] != "elections" ||
		proposals[0].Entities[0].Type != "organization" || proposals[0].DateAnchor != "2026-10-19" ||
		proposals[1].Form != "book" || !proposals[1].UncertainContentKind {
		t.Fatalf("unexpected result %#v", result)
	}
	if runner.ran("reader") {
		t.Fatal("classification must not run the extractor")
	}
	var request classifyRequest
	if err := json.Unmarshal(runner.inputs["kinds"], &request); err != nil {
		t.Fatal(err)
	}
	if request.Operation != CapabilityDocumentClassify || request.Format != ".pdf" || len(request.Documents) != 2 ||
		request.Documents[0].Text != "first" || utf8.RuneCountInString(request.Documents[1].Text) != maxClassifySample {
		t.Fatalf("the classifier must get the operation, the format and a bounded sample of each text: %#v", request.Documents[0])
	}
}

func TestClassifyRefusesBadRequestsWithoutRunningAPlugin(t *testing.T) {
	root := t.TempDir()
	newClassifier(t, root, "kinds", true)
	runner := &inputRunner{reply: func(Plugin) ([]byte, error) { return proposalsOutput(map[string]any{}), nil }}
	service := extractService(root, runner)

	cases := map[string]map[string]any{
		"no documents":   {"format": ".pdf"},
		"empty text":     classifyPayload("  "),
		"bad format":     {"format": "pdf", "documents": []any{map[string]any{"text": "x"}}},
		"too many":       {"documents": make([]any, maxDocuments+1)},
		"not a document": {"documents": []any{"text"}},
	}
	for name, payload := range cases {
		if reason := reasonOf(t, func() error { _, err := service.Dispatch(context.Background(), "feeds.classify", payload); return err }()); reason != ReasonInvalid {
			t.Errorf("%s: reason %q, want %q", name, reason, ReasonInvalid)
		}
	}
	if runner.ran("kinds") {
		t.Fatal("a bad request must not run the classifier")
	}
}

func TestClassifyNeedsOneEnabledClassifier(t *testing.T) {
	cases := map[string]struct {
		plugins func(t *testing.T, root string)
		reason  string
	}{
		"none":     {func(t *testing.T, root string) { newExtractor(t, root, "reader", true, ".pdf") }, ReasonUnavailable},
		"disabled": {func(t *testing.T, root string) { newClassifier(t, root, "kinds", false) }, ReasonUnavailable},
		"two": {func(t *testing.T, root string) {
			newClassifier(t, root, "a", true)
			newClassifier(t, root, "b", true)
		}, ReasonAmbiguous},
	}
	for name, tc := range cases {
		root := t.TempDir()
		tc.plugins(t, root)
		runner := &inputRunner{reply: func(Plugin) ([]byte, error) { return proposalsOutput(map[string]any{}), nil }}
		_, err := extractService(root, runner).Dispatch(context.Background(), "feeds.classify", classifyPayload("x"))
		if reason := reasonOf(t, err); reason != tc.reason {
			t.Errorf("%s: reason %q, want %q (%v)", name, reason, tc.reason, err)
		}
		if len(runner.inputs) != 0 {
			t.Errorf("%s: nothing may run without a single bound classifier", name)
		}
	}
}

func TestClassifyRefusesOutputOutsideTheContract(t *testing.T) {
	cases := map[string]struct {
		output string
		reason string
	}{
		"not json":            {`nope`, ReasonFailed},
		"two objects":         {`{"classifications":[{}]}{}`, ReasonFailed},
		"unknown field":       {`{"classifications":[{"genre":"x"}]}`, ReasonFailed},
		"no list":             {`{}`, ReasonFailed},
		"both":                {`{"classifications":[{}],"error":"x"}`, ReasonFailed},
		"wrong count":         {`{"classifications":[{},{}]}`, ReasonFailed},
		"label not a label":   {`{"classifications":[{"form":"Book!"}]}`, ReasonFailed},
		"bad date":            {`{"classifications":[{"date_anchor":"19/10/2026"}]}`, ReasonFailed},
		"empty topic":         {`{"classifications":[{"topics":[" "]}]}`, ReasonFailed},
		"nameless entity":     {`{"classifications":[{"entities":[{"type":"person"}]}]}`, ReasonFailed},
		"too many topics":     {`{"classifications":[{"topics":[` + strings.TrimSuffix(strings.Repeat(`"t",`, maxTopics+1), ",") + `]}]}`, ReasonFailed},
		"declines":            {`{"error":"no idea"}`, ReasonUnreadable},
		"declines with blank": {`{"error":""}`, ReasonUnreadable},
	}
	for name, tc := range cases {
		root := t.TempDir()
		newClassifier(t, root, "kinds", true)
		output := tc.output
		runner := &inputRunner{reply: func(Plugin) ([]byte, error) { return []byte(output), nil }}
		_, err := extractService(root, runner).Dispatch(context.Background(), "feeds.classify", classifyPayload("x"))
		if reason := reasonOf(t, err); reason != tc.reason {
			t.Errorf("%s: reason %q, want %q (%v)", name, reason, tc.reason, err)
		}
	}
}

func TestClassifyReportsATimeoutAndKeepsItsStatus(t *testing.T) {
	root := t.TempDir()
	newClassifier(t, root, "kinds", true)
	runner := &inputRunner{reply: func(Plugin) ([]byte, error) { return nil, ErrTimeout }}
	service := extractService(root, runner)

	_, err := service.Dispatch(context.Background(), "feeds.classify", classifyPayload("x"))
	if reason := reasonOf(t, err); reason != ReasonTimeout {
		t.Fatalf("reason %q, want timeout", reason)
	}
	status, _ := service.Dispatch(context.Background(), "feeds.status", nil)
	listed := status["extractors"].([]map[string]any)
	if len(listed) != 1 || listed[0]["id"] != "kinds" || listed[0]["last_error"] == "" ||
		listed[0]["last_run"] != testNow.UTC().Format(time.RFC3339) {
		t.Fatalf("a classifier is listed apart from feeds with its last run: %#v", listed)
	}
	if status["enabled"] != 0 {
		t.Fatalf("a classifier is not a feed source: %#v", status)
	}
}

func TestAClassifierManifestTakesNoFormats(t *testing.T) {
	root := t.TempDir()
	newClassifier(t, root, "kinds", true)
	newPlugin(t, root, "formatted", v1(map[string]any{"id": "formatted", "provides": []string{CapabilityDocumentClassify}}))
	found, err := Discover(root, testLimits())
	if err != nil {
		t.Fatal(err)
	}
	byDir := map[string]Found{}
	for _, f := range found {
		byDir[filepath.Base(f.Dir)] = f
	}
	if p := byDir["kinds"].Plugin; p == nil || !p.provides(CapabilityDocumentClassify) || p.Collects() {
		t.Fatalf("a classifier without formats loads: %#v", byDir["kinds"])
	}
	if f := byDir["formatted"]; f.Plugin != nil || f.Err == nil || !strings.Contains(f.Err.Error(), "formats apply only") {
		t.Fatalf("formats belong to extractors: %#v", f)
	}
}
