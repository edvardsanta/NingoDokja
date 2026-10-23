package cognitive

import (
	"context"
	"errors"
	inference "read_books/dokja_domain/dokja_inference"
	"testing"
	"time"
)

type inferenceFunc func(context.Context, inference.Request) (inference.Result, error)

func (f inferenceFunc) Infer(c context.Context, r inference.Request) (inference.Result, error) {
	return f(c, r)
}
func validReply(r inference.Request) inference.Result {
	p := inference.Proposal{Contract: inference.Version, Revision: r.Revision, Status: "propose", Action: "fast", Target: "simulated_sum_success", Probability: .7, Evidence: []string{}, Experiences: []string{}, Summary: "Use current evidence"}
	hasSample := false
	for _, e := range r.Evidence {
		p.Evidence = append(p.Evidence, e.ID)
		if e.ID == "sample" {
			hasSample = true
		}
	}
	if !hasSample {
		p.Status = "need_evidence"
		p.Action = ""
		p.Target = ""
		p.Missing = "sample"
		p.Probability = 0
	}
	if hasSample && len(r.Experiences) > 0 {
		p.Action = "normalize"
		p.Experiences = []string{r.Experiences[0].ID}
	}
	return inference.Result{Proposal: p, Attempts: []inference.Attempt{{Provider: "fixture", Model: "fixture-v1", Outcome: "accepted"}}}
}
func TestModelProposalsRemainUnderDokjaControl(t *testing.T) {
	var revisions []int
	provider := inferenceFunc(func(_ context.Context, r inference.Request) (inference.Result, error) {
		revisions = append(revisions, r.Revision)
		return validReply(r), nil
	})
	c := config("iterative", "first")
	c.Inference = provider
	m := NewMemory(Fixture())
	first := Run(context.Background(), m, c)
	if first.Outcome != "completed" || first.Observed != "failed" || len(first.Inferences) != 2 || len(revisions) != 2 || revisions[1] <= revisions[0] {
		t.Fatal(first, revisions)
	}
	c.Attempt = "second"
	second := Run(context.Background(), m, c)
	if second.Observed != "success" || second.Action != "normalize" || len(m.Episodes) != 2 || second.Inferences[1].Proposal.Experiences[0] != "first" {
		t.Fatal(second)
	}
	c.Attempt = "no-permission"
	c.Authorized = false
	blocked := Run(context.Background(), m, c)
	if blocked.Outcome != "authorization_required" || len(m.Episodes) != 2 {
		t.Fatal(blocked)
	}
}
func TestInvalidOrUnavailableInferenceNeverFallsBack(t *testing.T) {
	for _, kind := range []string{"stale", "action", "citation", "budget", "error", "late"} {
		t.Run(kind, func(t *testing.T) {
			c := config("iterative", kind)
			c.Deadline = 100 * time.Millisecond
			m := NewMemory(Fixture())
			c.Inference = inferenceFunc(func(ctx context.Context, r inference.Request) (inference.Result, error) {
				answer := validReply(r)
				switch kind {
				case "stale":
					answer.Proposal.Revision--
				case "action":
					answer.Proposal.Status = "propose"
					answer.Proposal.Action = "shell"
					answer.Proposal.Missing = ""
				case "citation":
					answer.Proposal.Evidence = []string{"foreign"}
				case "budget":
					answer.Attempts = make([]inference.Attempt, r.RemainingCalls+1)
				case "error":
					return answer, errors.New("unavailable")
				case "late":
					<-ctx.Done()
				}
				return answer, nil
			})
			got := Run(context.Background(), m, c)
			if got.Outcome != "inference_rejected" && got.Outcome != "inference_failed" && got.Outcome != "budget_exhausted" {
				t.Fatal(got)
			}
			if len(m.Episodes) != 0 || got.Observed != "" || got.Prediction.Model == "synthetic-v1" {
				t.Fatal("escaped rejection", got)
			}
		})
	}
}
func TestModelCannotOverrideContraryEvidence(t *testing.T) {
	n := Fixture()
	n[3].Facts["sample_verified"] = false
	m := NewMemory(n)
	c := config("iterative", "contrary")
	c.Inference = inferenceFunc(func(_ context.Context, r inference.Request) (inference.Result, error) { return validReply(r), nil })
	got := Run(context.Background(), m, c)
	if got.Outcome != "abstain" || got.Observed != "" || len(m.Episodes) != 0 {
		t.Fatal(got)
	}
}
func TestProviderReceivesSnapshotCopy(t *testing.T) {
	m := NewMemory(Fixture())
	c := config("iterative", "copy")
	c.Inference = inferenceFunc(func(_ context.Context, r inference.Request) (inference.Result, error) {
		reply := validReply(r)
		r.Evidence[0].Facts = map[string]bool{"sandbox_allowed": false}
		r.Actions[0].ID = "shell"
		return reply, nil
	})
	got := Run(context.Background(), m, c)
	if got.Outcome != "completed" {
		t.Fatal(got)
	}
}

func TestObservedDiagnosticsAreReusableFacts(t *testing.T) {
	for _, tc := range []struct {
		action              string
		tokens              []string
		want                int
		outcome, diagnostic string
	}{
		{"fast", []string{"１２", "3"}, 15, "failed", "parse_error_full_width_digits"},
		{"normalize", []string{"１２", "3"}, 15, "success", "sum_verified"},
		{"fast", []string{"abc"}, 15, "failed", "parse_error"},
		{"normalize", []string{"abc"}, 15, "failed", "parse_error"},
		{"fast", []string{"12", "3"}, 16, "failed", "sum_mismatch"},
		{"shell", []string{"12"}, 12, "unknown", ""},
	} {
		outcome, diagnostic := ObserveSimulation(tc.action, tc.tokens, tc.want)
		if outcome != tc.outcome || diagnostic != tc.diagnostic {
			t.Fatal(tc, outcome, diagnostic)
		}
	}
	m := NewMemory(Fixture())
	c := config("iterative", "diagnostic-first")
	c.Inference = inferenceFunc(func(_ context.Context, r inference.Request) (inference.Result, error) { return validReply(r), nil })
	first := Run(context.Background(), m, c)
	if first.Diagnostic != "parse_error_full_width_digits" || len(m.Episodes) != 1 || m.Episodes[0].Diagnostic != first.Diagnostic {
		t.Fatal(first, m.Episodes)
	}
	c.Attempt = "diagnostic-second"
	seen := false
	c.Inference = inferenceFunc(func(_ context.Context, r inference.Request) (inference.Result, error) {
		if len(r.Experiences) != 1 || r.Experiences[0].Diagnostic != first.Diagnostic {
			t.Fatal(r)
		}
		seen = true
		return validReply(r), nil
	})
	second := Run(context.Background(), m, c)
	if !seen || second.Diagnostic != "sum_verified" {
		t.Fatal(second)
	}
	bad := m.Episodes[0]
	bad.ID = "invalid"
	bad.Diagnostic = "sum_verified"
	if m.Record(context.Background(), bad) == nil {
		t.Fatal("accepted contradictory observed diagnostic")
	}
}
