package handlers

// Step 08 comparison: the scheduled hashtag delivery through the existing path, the task
// workspace without prediction, and the workspace with shadow prediction, on the same fixed
// cases. The invariants always run. Timings run only with DOKJA_MEASURE=1, since they only
// mean something on a quiet machine:
//
//	DOKJA_MEASURE=1 go test ./internal/handlers -run TestStep08 -v

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"read_books/internal/core"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

type comparisonMode string

const (
	modeExisting  comparisonMode = "existing"
	modeWorkspace comparisonMode = "workspace"
	modeShadow    comparisonMode = "shadow"
)

var comparisonModes = []comparisonMode{modeExisting, modeWorkspace, modeShadow}

// comparisonCase is one fixed input. Every mode sees exactly the same fakes.
type comparisonCase struct {
	name        string
	ocrErr      error
	hits        []any
	knowledgeEr error
	memoryErr   error
	memoryDelay time.Duration
	deliveryErr error
	// reason is the task's terminal or degraded reason; the existing path only logs it.
	reason string
}

func labelledHit(relevant bool) []any {
	return []any{map[string]any{"title": "#Humor", "text": "words of the earlier meme", "source_ref": earlierMemeURL,
		"score": 0.83, "relevant": relevant}}
}

func comparisonCases() []comparisonCase {
	return []comparisonCase{
		{name: "relevant example", hits: labelledHit(true), reason: ""},
		{name: "below threshold", hits: labelledHit(false), reason: core.ReasonAbstained},
		{name: "no examples", hits: []any{}, reason: core.ReasonAbstained},
		{name: "unreadable media", ocrErr: errors.New("ocr unavailable"), reason: "media_text_unavailable"},
		{name: "knowledge down", knowledgeEr: errors.New("knowledge unavailable"), reason: "knowledge_unavailable"},
		{name: "memory down", hits: labelledHit(true), memoryErr: errors.New("memory unavailable"), reason: "prediction_unavailable"},
		{name: "memory slow", hits: labelledHit(true), memoryDelay: 150 * time.Millisecond, reason: "prediction_budget_missed"},
		{name: "delivery fails", hits: labelledHit(true), deliveryErr: errors.New("discord down"), reason: ""},
	}
}

// meter counts calls and response bytes per capability.
type meter struct {
	mu    sync.Mutex
	calls map[string]int
	bytes map[string]int
}

func newMeter() *meter { return &meter{calls: map[string]int{}, bytes: map[string]int{}} }

func (m *meter) note(capability string, response map[string]any) {
	body, _ := json.Marshal(response)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls[capability]++
	if response != nil {
		m.bytes[capability] += len(body)
	}
}

func (m *meter) summary() (string, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.calls))
	total := 0
	for name, count := range m.calls {
		names = append(names, fmt.Sprintf("%s=%d", name, count))
		total += m.bytes[name]
	}
	sort.Strings(names)
	return strings.Join(names, " "), total
}

type meteredReader struct {
	meter *meter
	err   error
}

func (r *meteredReader) ReadText(context.Context, string) (map[string]any, error) {
	if r.err != nil {
		r.meter.note("ocr", nil)
		return nil, r.err
	}
	response := map[string]any{"text": testMemeText}
	r.meter.note("ocr", response)
	return response, nil
}

type meteredKnowledge struct {
	meter *meter
	hits  []any
	err   error
}

func (k *meteredKnowledge) Dispatch(_ context.Context, eventType string, _ map[string]any) (map[string]any, error) {
	if k.err != nil {
		k.meter.note(eventType, nil)
		return nil, k.err
	}
	response := map[string]any{"threshold": 0.6, "degraded": false, "hits": k.hits}
	k.meter.note(eventType, response)
	return response, nil
}

type meteredMemory struct {
	meter *meter
	err   error
	delay time.Duration
	// recalled is set when memory.recall was asked; delivered reads it to see the order.
	mu       sync.Mutex
	recalled bool
}

func (m *meteredMemory) Dispatch(ctx context.Context, eventType string, _ map[string]any) (map[string]any, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	m.mu.Lock()
	if eventType == "memory.recall" {
		m.recalled = true
	}
	m.mu.Unlock()
	if m.err != nil {
		m.meter.note(eventType, nil)
		return nil, m.err
	}
	response := defaultAnswers()[eventType]
	m.meter.note(eventType, response)
	return response, nil
}

type comparisonDeliverer struct {
	memory          *meteredMemory
	err             error
	contents        []string
	predictedBefore bool
}

func (d *comparisonDeliverer) Deliver(_ context.Context, _, content, _ string) error {
	d.memory.mu.Lock()
	d.predictedBefore = d.memory.recalled
	d.memory.mu.Unlock()
	if d.err != nil {
		return d.err
	}
	d.contents = append(d.contents, content)
	return nil
}

func (d *comparisonDeliverer) DeliverAttachment(ctx context.Context, channelID, content string, _ []byte, _, url string) error {
	return d.Deliver(ctx, channelID, content, url)
}

// tracedTask keeps the attempts the task produced, to read their trace.
type tracedTask struct {
	inner    *HashtagTask
	attempts []HashtagAttempt
}

func (t *tracedTask) Suggest(ctx context.Context, event core.Event, url string) HashtagAttempt {
	attempt := t.inner.Suggest(ctx, event, url)
	t.attempts = append(t.attempts, attempt)
	return attempt
}

type comparisonRun struct {
	delivered       string
	err             error
	calls           string
	responseBytes   int
	recorded        int
	predictedBefore bool
	task            *core.TaskResult
	traceBytes      int
	elapsed         time.Duration
}

// runComparison composes the handler for one mode the way cmd/va/main.go does and runs one
// scheduled delivery. background makes experience notes asynchronous, as in production.
func runComparison(tc comparisonCase, mode comparisonMode, background bool, budget time.Duration) comparisonRun {
	m := newMeter()
	media := &meteredReader{meter: m, err: tc.ocrErr}
	knowledge := &meteredKnowledge{meter: m, hits: tc.hits, err: tc.knowledgeEr}
	memory := &meteredMemory{meter: m, err: tc.memoryErr, delay: tc.memoryDelay}
	deliverer := &comparisonDeliverer{memory: memory, err: tc.deliveryErr}

	workflow := NewMemeHashtagWorkflow(media, knowledge)
	experience := NewHashtagExperience(memory, nil)
	var pending sync.WaitGroup
	experience.spawn = func(work func()) {
		if !background {
			work()
			return
		}
		pending.Add(1)
		go func() { defer pending.Done(); work() }()
	}
	fetcher := &fakeMemeFetcher{result: map[string]any{"memes": []any{map[string]any{"title": "meme", "url": testMemeURL}}}}
	handler := NewSystemDomainHandler(nil, nil, fetcher, deliverer, "discord-channel")
	var traced *tracedTask
	switch mode {
	case modeExisting:
		handler = handler.WithHashtagSuggestions(workflow).WithHashtagExperience(experience)
	default:
		task := NewHashtagTask(workflow, memory, func() bool { return mode == modeShadow })
		task.budget = budget
		traced = &tracedTask{inner: task}
		handler = handler.WithHashtagTask(traced, experience)
	}

	started := time.Now()
	err := scheduledRun(handler)
	run := comparisonRun{err: err, elapsed: time.Since(started), predictedBefore: deliverer.predictedBefore}
	pending.Wait()
	if len(deliverer.contents) > 0 {
		run.delivered = deliverer.contents[0]
	}
	run.calls, run.responseBytes = m.summary()
	m.mu.Lock()
	run.recorded = m.calls["memory.record"]
	m.mu.Unlock()
	if traced != nil && len(traced.attempts) > 0 {
		task := traced.attempts[0].Task
		run.task = &task
		body, _ := json.Marshal(task)
		run.traceBytes = len(body)
	}
	return run
}

// taskReason is the machine-readable reason a trace gives: the terminal one, or the first
// degraded step's.
func taskReason(task *core.TaskResult) string {
	if task == nil {
		return ""
	}
	if task.Reason != "" {
		return task.Reason
	}
	for _, step := range task.Steps {
		if step.Degraded {
			return step.Reason
		}
	}
	return ""
}

func TestStep08ModesDeliverTheSameTextOnEveryFixedCase(t *testing.T) {
	for _, tc := range comparisonCases() {
		runs := map[comparisonMode]comparisonRun{}
		for _, mode := range comparisonModes {
			runs[mode] = runComparison(tc, mode, false, 50*time.Millisecond)
		}
		existing := runs[modeExisting]
		for _, mode := range comparisonModes {
			run := runs[mode]
			if run.delivered != existing.delivered || (run.err == nil) != (existing.err == nil) {
				t.Fatalf("%s/%s delivered %q (%v), existing %q (%v)", tc.name, mode, run.delivered, run.err, existing.delivered, existing.err)
			}
			t.Logf("%-17s %-9s delivered=%q calls[%s] bytes=%d recorded=%d predicted_before_delivery=%t reason=%q trace_bytes=%d",
				tc.name, mode, run.delivered, run.calls, run.responseBytes, run.recorded, run.predictedBefore, taskReason(run.task), run.traceBytes)
		}

		workspace, shadow := runs[modeWorkspace], runs[modeShadow]
		// Workspace-only records the attempt unscored; it never asks Memory for a prediction.
		if strings.Contains(workspace.calls, "memory.recall") || strings.Contains(workspace.calls, "memory.predict") {
			t.Fatalf("%s: workspace-only must not predict: %s", tc.name, workspace.calls)
		}
		if got := taskReason(shadow.task); got != tc.reason {
			t.Fatalf("%s: the shadow trace gives %q, want %q", tc.name, got, tc.reason)
		}
		if tc.name == "delivery fails" && (existing.recorded != 1 || shadow.recorded != 0) {
			t.Fatalf("a failed delivery is recorded by the existing path only (ADR 0004 change): existing=%d shadow=%d",
				existing.recorded, shadow.recorded)
		}
		// The existing path predicts in background work after the suggestion, so its order
		// against delivery is not guaranteed; only the shadow path freezes it before.
		if tc.name == "relevant example" && !shadow.predictedBefore {
			t.Fatal("the shadow path must predict before delivering")
		}
	}
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	index := int(p*float64(len(sorted)-1) + 0.5)
	return sorted[index]
}

func TestStep08DeliveryLatencyByMode(t *testing.T) {
	if os.Getenv("DOKJA_MEASURE") != "1" {
		t.Skip("set DOKJA_MEASURE=1 to measure")
	}
	const runs = 50
	measure := func(tc comparisonCase, mode comparisonMode, budget time.Duration, n int) (time.Duration, time.Duration) {
		durations := make([]time.Duration, 0, n)
		for range n {
			durations = append(durations, runComparison(tc, mode, true, budget).elapsed)
		}
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		return percentile(durations, 0.5), percentile(durations, 0.95)
	}
	// Orchestration overhead: every fake answers at once.
	for _, tc := range comparisonCases() {
		if tc.memoryDelay > 0 {
			continue
		}
		for _, mode := range comparisonModes {
			p50, p95 := measure(tc, mode, hashtagPredictionBudget, runs)
			t.Logf("overhead %-17s %-9s n=%d p50=%s p95=%s", tc.name, mode, runs, p50, p95)
		}
	}
	// The shadow cost: Memory answers after a fixed delay, under the production budget.
	for _, delay := range []time.Duration{100 * time.Millisecond, 400 * time.Millisecond, 2 * time.Second} {
		tc := comparisonCase{name: "memory delay", hits: labelledHit(true), memoryDelay: delay}
		for _, mode := range comparisonModes {
			p50, p95 := measure(tc, mode, hashtagPredictionBudget, 5)
			t.Logf("memory delay %-6s %-9s n=5 p50=%s p95=%s", delay, mode, p50, p95)
		}
	}
}
