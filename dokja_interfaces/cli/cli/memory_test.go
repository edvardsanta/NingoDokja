package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pebbe/zmq4"
)

func TestBuildMemoryRecordPayload(t *testing.T) {
	payload, err := buildMemoryRecordPayload(" m1 ", " tag.suggest ", " #Label ", []string{"some", " words "})
	if err != nil || payload["ref"] != "m1" || payload["action"] != "tag.suggest" || payload["context"] != "some  words" || payload["detail"] != "#Label" {
		t.Fatalf("unexpected payload %#v (%v)", payload, err)
	}
	if bare, _ := buildMemoryRecordPayload("m1", "a", " ", []string{"x"}); bare["detail"] != nil {
		t.Fatalf("an empty detail is left out: %#v", bare)
	}
	for name, run := range map[string]func() error{
		"no ref":     func() error { _, err := buildMemoryRecordPayload(" ", "a", "", []string{"x"}); return err },
		"no action":  func() error { _, err := buildMemoryRecordPayload("m", " ", "", []string{"x"}); return err },
		"no context": func() error { _, err := buildMemoryRecordPayload("m", "a", "", []string{" "}); return err },
	} {
		if err := run(); err == nil {
			t.Fatalf("%s: expected a validation error", name)
		}
	}
}

func TestBuildMemoryRecallAndPredictPayloads(t *testing.T) {
	recall, err := buildMemoryRecallPayload([]string{"central", "bank"}, " tag.suggest ", 5)
	if err != nil || recall["context"] != "central bank" || recall["action"] != "tag.suggest" || recall["k"] != 5 {
		t.Fatalf("unexpected recall payload %#v (%v)", recall, err)
	}
	if open, _ := buildMemoryRecallPayload([]string{"x"}, "", 10); open["action"] != nil {
		t.Fatalf("no action means every action: %#v", open)
	}
	for _, k := range []int{0, 51} {
		if _, err := buildMemoryRecallPayload([]string{"x"}, "", k); err == nil {
			t.Fatalf("expected k=%d to be rejected", k)
		}
	}
	if _, err := buildMemoryRecallPayload(nil, "", 5); err == nil {
		t.Fatal("expected an empty context to be rejected")
	}

	predict, err := buildMemoryPredictPayload([]string{"some", "words"}, "tag.suggest")
	if err != nil || predict["context"] != "some words" || predict["action"] != "tag.suggest" {
		t.Fatalf("unexpected predict payload %#v (%v)", predict, err)
	}
	if _, err := buildMemoryPredictPayload([]string{"x"}, " "); err == nil {
		t.Fatal("a prediction is about one action")
	}
	if _, err := buildMemoryPredictPayload(nil, "a"); err == nil {
		t.Fatal("expected an empty context to be rejected")
	}
}

func TestBuildMemoryResolvePayload(t *testing.T) {
	stated, err := buildMemoryResolvePayload(" m1 ", " accepted ", "")
	if err != nil || stated["ref"] != "m1" || stated["outcome"] != "accepted" || stated["observed"] != nil {
		t.Fatalf("unexpected payload %#v (%v)", stated, err)
	}
	observed, err := buildMemoryResolvePayload("m1", "", " #Label ")
	if err != nil || observed["observed"] != "#Label" || observed["outcome"] != nil {
		t.Fatalf("unexpected payload %#v (%v)", observed, err)
	}
	for name, run := range map[string]func() error{
		"no ref":  func() error { _, err := buildMemoryResolvePayload(" ", "accepted", ""); return err },
		"both":    func() error { _, err := buildMemoryResolvePayload("m", "accepted", "#x"); return err },
		"neither": func() error { _, err := buildMemoryResolvePayload("m", " ", " "); return err },
	} {
		if err := run(); err == nil {
			t.Fatalf("%s: expected a validation error", name)
		}
	}
}

func TestMemoryIsASwitchableServiceAndItsAnswerUnwrapsInBothModes(t *testing.T) {
	if _, err := buildServicePayload("memory", false); err != nil {
		t.Fatalf("memory must be a known service: %v", err)
	}
	for name, envelope := range map[string]any{
		"verbose": map[string]any{"result": map[string]any{"memory": map[string]any{"found": true}}},
		"compact": map[string]any{"workflow": "memory", "domain": "memory", "result": map[string]any{"found": true}},
	} {
		answer, skipped, err := domainResult(envelope, "memory")
		if err != nil || skipped != "" || answer["found"] != true {
			t.Fatalf("%s: unexpected %#v %q %v", name, answer, skipped, err)
		}
	}
}

func TestFormatRecallShowsNeighboursAndWhatTheCountsAre(t *testing.T) {
	out := FormatRecall(map[string]any{
		"outcomes": map[string]any{"replaced": 4.0, "accepted": 6.0},
		"neighbors": []any{
			map[string]any{"ref": "hashtag:aa", "action": "hashtag.suggest", "outcome": "accepted", "similarity": 0.91},
			map[string]any{"ref": "hashtag:bb", "action": "hashtag.suggest", "outcome": "replaced", "similarity": 0.62},
		},
	})
	for _, want := range []string{"outcomes: accepted 6, replaced 4", " 1. [0.91] accepted", " 2. [0.62] replaced", "hashtag:bb"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in:\n%s", want, out)
		}
	}

	degraded := FormatRecall(map[string]any{"degraded": true, "reason": "embedding server unreachable", "outcomes": map[string]any{}, "neighbors": []any{}})
	for _, want := range []string{"! degraded: embedding server unreachable", "no outcomes recorded yet", "no similar experience"} {
		if !strings.Contains(degraded, want) {
			t.Fatalf("expected %q in:\n%s", want, degraded)
		}
	}
}

func TestFormatPredictionNeverPassesTheBaselineOffAsMore(t *testing.T) {
	strong := FormatPrediction(map[string]any{
		"action": "hashtag.suggest", "predicted_p": 0.72, "baseline_p": 0.6, "support": 5.0, "resolved": 40.0,
		"evidence": []any{map[string]any{"ref": "hashtag:aa", "outcome": "accepted", "similarity": 0.9}},
	})
	for _, want := range []string{"hashtag.suggest is accepted: 0.72 (baseline 0.60)", "5 similar experiences, 40 resolved", "[0.90] accepted"} {
		if !strings.Contains(strong, want) {
			t.Fatalf("expected %q in:\n%s", want, strong)
		}
	}
	if strings.Contains(strong, "!") {
		t.Fatalf("a well supported prediction has no warning:\n%s", strong)
	}

	weak := FormatPrediction(map[string]any{
		"action": "hashtag.suggest", "predicted_p": 0.5, "baseline_p": 0.5, "support": 0.0, "resolved": 3.0,
		"insufficient": true, "reason": "too few resolved experiences of this action", "degraded": true,
	})
	for _, want := range []string{"! not enough evidence (too few resolved experiences of this action): this is just the baseline", "! degraded"} {
		if !strings.Contains(weak, want) {
			t.Fatalf("expected %q in:\n%s", want, weak)
		}
	}
}

func TestFormatScoreSaysWhenThereIsNothingOrNotEnoughToJudge(t *testing.T) {
	if out := FormatScore(map[string]any{"scored": 0.0, "unscored": 4.0}); !strings.Contains(out, "nothing scored yet") || !strings.Contains(out, "unscored: 4") {
		t.Fatalf("unexpected rendering:\n%s", out)
	}

	few := FormatScore(map[string]any{
		"scored": 5.0, "unscored": 1.0, "min_scored": 30.0, "enough_data": false,
		"brier_prediction": 0.1, "brier_baseline": 0.25, "skill": 0.6, "beats_baseline": true,
	})
	if !strings.HasPrefix(few, "! only 5 scored; judging needs at least 30") {
		t.Fatalf("the caveat must come first:\n%s", few)
	}

	enough := FormatScore(map[string]any{
		"scored": 40.0, "unscored": 2.0, "min_scored": 30.0, "enough_data": true,
		"brier_prediction": 0.2, "brier_baseline": 0.18, "skill": -0.111, "beats_baseline": false,
	})
	for _, want := range []string{"scored 40, unscored 2", "prediction 0.200, baseline 0.180", "skill -0.111: does not beat the baseline"} {
		if !strings.Contains(enough, want) {
			t.Fatalf("expected %q in:\n%s", want, enough)
		}
	}
	if strings.Contains(enough, "!") {
		t.Fatalf("enough data needs no caveat:\n%s", enough)
	}
}

// ---- the whole command, against a real socket ---------------------------------------------

// fakeOrchestrator answers one request on a loopback ZeroMQ socket and reports what it received.
func fakeOrchestrator(t *testing.T, reply string) (endpoint string, received <-chan string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	endpoint = fmt.Sprintf("tcp://127.0.0.1:%d", listener.Addr().(*net.TCPAddr).Port)
	listener.Close()

	socket, err := zmq4.NewSocket(zmq4.REP)
	if err != nil {
		t.Fatal(err)
	}
	if err := socket.Bind(endpoint); err != nil {
		t.Fatal(err)
	}
	socket.SetRcvtimeo(5 * time.Second)
	got := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		raw, err := socket.Recv(0)
		if err != nil {
			got <- ""
			return
		}
		got <- raw
		socket.Send(reply, 0)
	}()
	t.Cleanup(func() { <-done; socket.Close() })
	return endpoint, got
}

func captureStdout(t *testing.T, run func()) string {
	t.Helper()
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	defer func() { os.Stdout = original }()

	run()
	writer.Close()
	out, _ := io.ReadAll(reader)
	return string(out)
}

func TestPredictCommandAsksTheOrchestratorAndPrintsTheEstimate(t *testing.T) {
	endpoint, received := fakeOrchestrator(t, `{"status":"ok","result":{"event_id":"e1","workflow":"memory","domain":"memory","result":{`+
		`"action":"hashtag.suggest","predicted_p":0.64,"baseline_p":0.6,"support":4,"resolved":25,"insufficient":false,"degraded":false,"evidence":[]}}}`)

	var runErr error
	out := captureStdout(t, func() {
		runErr = NewApp().Run(context.Background(), []string{
			"dokja-cli", "--request-endpoint", endpoint, "memory", "predict", "--action", "hashtag.suggest", "some", "words",
		})
	})
	if runErr != nil {
		t.Fatalf("unexpected error: %v", runErr)
	}

	var event struct {
		Type    string         `json:"type"`
		Source  string         `json:"source"`
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal([]byte(<-received), &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "memory.predict" || event.Source != "cli" || event.Payload["action"] != "hashtag.suggest" || event.Payload["context"] != "some words" {
		t.Fatalf("the orchestrator received %+v", event)
	}
	if !strings.Contains(out, "hashtag.suggest is accepted: 0.64 (baseline 0.60)") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestMemoryCommandsReportASwitchedOffServiceAndBadInputWithoutAskingAnyone(t *testing.T) {
	endpoint, _ := fakeOrchestrator(t, `{"status":"ok","result":{"event_id":"e1","result":{"skipped":true,"reason":"service memory is disabled"}}}`)
	err := NewApp().Run(context.Background(), []string{"dokja-cli", "--request-endpoint", endpoint, "memory", "status"})
	if err == nil || !strings.Contains(err.Error(), "experience memory is off: service memory is disabled") {
		t.Fatalf("expected the skip reason, got %v", err)
	}

	app := NewApp()
	app.newRequester = func(string) (*Requester, error) {
		t.Fatal("a request with invalid input must never be sent")
		return nil, nil
	}
	for name, args := range map[string][]string{
		"predict without an action": {"memory", "predict", "words"},
		"record without an action":  {"memory", "record", "m1", "words"},
		"resolve without a verdict": {"memory", "resolve", "m1"},
		"resolve with two verdicts": {"memory", "resolve", "m1", "--outcome", "accepted", "--observed", "#x"},
		"recall with a bad k":       {"memory", "recall", "-k", "0", "words"},
		"show without a ref":        {"memory", "show"},
	} {
		if err := app.Run(context.Background(), append([]string{"dokja-cli"}, args...)); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}
