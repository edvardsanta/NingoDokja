package tui

import (
	"errors"
	"strings"
	"testing"
)

func pendingFeedbackTUI(t *testing.T) (*Model, *fakeClient) {
	m, fake := browseTUI(t)
	fake.responses["memory.resolve"] = map[string]any{"found": true, "resolved": true, "outcome": "accepted"}
	press(m, "m", "l", "down", "down")
	return m, fake
}

func TestExperienceFeedbackNeedsExplicitConfirmation(t *testing.T) {
	for _, key := range []string{"a", "i"} {
		t.Run(key, func(t *testing.T) {
			m, fake := pendingFeedbackTUI(t)
			press(m, key)
			if m.overlay != overlayConfirm || fake.count("memory.resolve") != 0 {
				t.Fatal("expected confirmation before writing")
			}
			if !strings.Contains(m.View(), "hashtag:cccc3333") {
				t.Fatal("confirmation must name the exact ref")
			}
			press(m, "n")
			if fake.count("memory.resolve") != 0 || m.overlay != overlayMemoryList {
				t.Fatal("cancel must leave the outcome alone")
			}
			press(m, key, "y")
			req := fake.last("memory.resolve")
			want := "accepted"
			if key == "i" {
				want = "ignored"
			}
			if req.payload["ref"] != "hashtag:cccc3333" || req.payload["outcome"] != want {
				t.Fatalf("wrong request: %#v", req)
			}
			if fake.count("memory.list") != 2 {
				t.Fatal("refresh after recording")
			}
		})
	}
}

func TestCorrectionFreezesTheRefAndLeavesTheVerdictToTheDomain(t *testing.T) {
	m, fake := pendingFeedbackTUI(t)
	press(m, "c", "enter")
	if m.overlay != overlayMemoryFeedback || fake.count("memory.resolve") != 0 {
		t.Fatal("empty result cannot write")
	}
	typeText(m, "#Other")
	press(m, "enter")
	m.memoryList.Cursor = 0 // even if selection changes, the reviewed experience must win
	if fake.count("memory.resolve") != 0 {
		t.Fatal("must wait for confirmation")
	}
	press(m, "y")
	req := fake.last("memory.resolve")
	if req.payload["ref"] != "hashtag:cccc3333" || req.payload["observed"] != "#Other" || req.payload["outcome"] != nil {
		t.Fatalf("wrong correction: %#v", req.payload)
	}
}

func TestFeedbackDoesNotOverwriteResolvedExperienceOrRunWhileBusy(t *testing.T) {
	m, fake := pendingFeedbackTUI(t)
	m.memoryList.Cursor = 0
	press(m, "a", "c", "i")
	if m.overlay != overlayMemoryList || fake.count("memory.resolve") != 0 {
		t.Fatal("recorded verdict cannot be changed")
	}
	m.memoryList.Cursor = 2
	m.busy = true
	press(m, "a", "c", "i")
	if m.overlay != overlayMemoryList {
		t.Fatal("must not open feedback while busy")
	}
}

func TestFeedbackDistinguishesConflictingVerdictsAndFailures(t *testing.T) {
	for _, scenario := range []string{"conflict", "missing", "timeout", "disabled"} {
		t.Run(scenario, func(t *testing.T) {
			m, fake := pendingFeedbackTUI(t)
			switch scenario {
			case "conflict":
				fake.responses["memory.resolve"] = map[string]any{"found": true, "resolved": false, "outcome": "ignored"}
			case "missing":
				fake.responses["memory.resolve"] = map[string]any{"found": false, "resolved": false}
			case "timeout":
				fake.errs["memory.resolve"] = errors.New("timeout")
			case "disabled":
				fake.responses["memory.resolve"] = map[string]any{"skipped": true, "reason": "paused"}
			}
			press(m, "a", "y")
			if strings.Contains(m.notice, "Feedback salvo") || fake.count("memory.resolve") != 1 {
				t.Fatalf("must not report a saved verdict or retry: %s", m.notice)
			}
		})
	}
}
