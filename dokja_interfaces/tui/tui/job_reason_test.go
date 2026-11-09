package tui

import (
	"strings"
	"testing"
	"time"
)

func TestAJobRowSaysWhyItWasSkippedAndWhenTheSchedulerHoldsIt(t *testing.T) {
	now := time.Date(2026, 11, 9, 3, 0, 0, 0, time.UTC)
	held := jobRow{Name: "price.intake", Enabled: true, Interval: "15m0s", LastAt: now.Add(-time.Minute), LastOutcome: "skipped", LastReason: "scheduler_off"}
	m := &Model{status: &statusData{UpdatedAt: now, Services: []serviceRow{{Name: "scheduler", Status: "ok", Enabled: false}}}}

	line := m.jobState(held)
	if !strings.Contains(line, tr("job_reason_scheduler_off")) || !strings.Contains(line, tr("job_held_by_scheduler")) {
		t.Fatalf("line %q", line)
	}

	m.status.Services[0].Enabled = true
	line = m.jobState(held)
	if strings.Contains(line, tr("job_held_by_scheduler")) {
		t.Fatalf("a running scheduler holds nothing: %q", line)
	}

	off := jobRow{Name: "meme.dispatch", Enabled: false, LastAt: now.Add(-time.Minute), LastOutcome: "skipped", LastReason: "job_off"}
	m.status.Services[0].Enabled = false
	line = m.jobState(off)
	if !strings.Contains(line, tr("job_reason_job_off")) || strings.Contains(line, tr("job_held_by_scheduler")) {
		t.Fatalf("a job that is off is not held by the scheduler: %q", line)
	}
	if skipReason("made_up") != "" {
		t.Fatal("an unknown code must say nothing")
	}
}
