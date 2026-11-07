package inference

import (
	"encoding/json"
	"strings"
	"testing"
)

func request() Request {
	return Request{Contract: Version, TaskID: "task", Revision: 3, Scope: "owner", Goal: "simulate", RemainingCalls: 2, Evidence: []Evidence{{Ref: Ref{"rule", "1", "fixture:rule"}, Question: "sample"}}, Actions: []Action{{"fast", "parse"}}, Experiences: []Experience{{ID: "old", Action: "fast", Outcome: "failed", Simulation: true}}}
}
func proposal() Proposal {
	return Proposal{Contract: Version, Revision: 3, Status: "need_evidence", Missing: "sample", Evidence: []string{"rule"}, Experiences: []string{}, Summary: "Need the referenced observation"}
}
func TestStrictInferenceContract(t *testing.T) {
	r := request()
	p := proposal()
	b, _ := json.Marshal(p)
	if _, err := Decode(r, b); err != nil {
		t.Fatal(err)
	}
	cases := []func(*Proposal){func(p *Proposal) { p.Revision-- }, func(p *Proposal) { p.Missing = "unrelated-file" }, func(p *Proposal) { p.Evidence = []string{"foreign"} }, func(p *Proposal) { p.Experiences = []string{"foreign"} }, func(p *Proposal) { p.Action = "shell" }, func(p *Proposal) { p.Probability = 1.1 }, func(p *Proposal) { p.Summary = strings.Repeat("x", 1201) }, func(p *Proposal) { p.Status = "record_memory" }}
	for i, change := range cases {
		p = proposal()
		change(&p)
		b, _ = json.Marshal(p)
		if _, err := Decode(r, b); err == nil {
			t.Fatalf("accepted invalid proposal %d", i)
		}
	}
	p = proposal()
	b, _ = json.Marshal(p)
	for _, bad := range [][]byte{append(b, []byte(` {}`)...), []byte(`{"contract":"inference.v1"}`), []byte(strings.Replace(string(b), `"probability":0`, `"probability":null`, 1)), []byte(strings.Replace(string(b), `"summary":`, `"execute":true,"summary":`, 1))} {
		if _, err := Decode(r, bad); err == nil {
			t.Fatal("accepted invalid JSON shape")
		}
	}
}
func TestProposalsDoNotGrantEffects(t *testing.T) {
	r := request()
	p := proposal()
	p.Status = "propose"
	p.Action = "fast"
	p.Missing = ""
	p.Target = "simulated_sum_success"
	p.Probability = .5
	if err := Validate(r, p); err != nil {
		t.Fatal(err)
	}
	p.Action = "delete"
	if err := Validate(r, p); err == nil {
		t.Fatal("unknown action accepted")
	}
	r = request()
	r.RemainingCalls = 0
	if err := ValidateRequest(r); err == nil {
		t.Fatal("budget omitted")
	}
	r = request()
	r.Experiences[0].Simulation = false
	if err := ValidateRequest(r); err == nil {
		t.Fatal("unlabelled simulated memory")
	}
}

func TestGroundedSchemaAndObservedDiagnostics(t *testing.T) {
	r := request()
	r.Evidence = append(r.Evidence, Evidence{Ref: Ref{"sample", "1", "fixture:sample"}, Requires: []string{"rule", "other", "other"}})
	ids := Retrievable(r)
	if len(ids) != 1 || ids[0] != "other" {
		t.Fatal(ids)
	}
	props := SchemaFor(r)["properties"].(map[string]any)
	missing := props["missing"].(map[string]any)["enum"].([]string)
	if len(missing) != 2 || missing[1] != "other" {
		t.Fatal(missing)
	}
	for _, diagnostic := range []string{"parse_error_full_width_digits", "parse_error", "sum_mismatch"} {
		r.Experiences[0].Diagnostic = diagnostic
		if err := ValidateRequest(r); err != nil {
			t.Fatal(err)
		}
	}
	for _, diagnostic := range []string{"sum_verified", "use_normalize", "untrusted advice"} {
		r.Experiences[0].Diagnostic = diagnostic
		if ValidateRequest(r) == nil {
			t.Fatal("accepted invalid diagnostic", diagnostic)
		}
	}
}

func TestSchemaRequiresGroundedCitation(t *testing.T) {
	r := request()
	props := SchemaFor(r)["properties"].(map[string]any)
	if props["evidence_ids"].(map[string]any)["minItems"] != 1 {
		t.Fatal("citation not required")
	}
	r.RequiredFacts = []string{"sample_verified", "sample_verified"}
	if ValidateRequest(r) == nil {
		t.Fatal("duplicate prerequisites accepted")
	}
	r.RequiredFacts = []string{"sample_verified"}
	if err := ValidateRequest(r); err != nil {
		t.Fatal(err)
	}
}

// Fields that belong to another status are an inconsistent answer: it is refused as it came, never
// repaired by dropping the field the validator does not expect.
func TestFieldsOfAnotherStatusAreRefusedNotRepaired(t *testing.T) {
	for _, status := range []string{"need_evidence", "abstain", "clarify"} {
		// Each status where it is otherwise valid: asking for evidence while the sample is missing,
		// a terminal answer once nothing is left to retrieve.
		r := resolved()
		if status == "need_evidence" {
			r = request()
		}
		p := proposal()
		p.Status = status
		if status != "need_evidence" {
			p.Missing = ""
		}
		if err := Validate(r, p); err != nil {
			t.Fatalf("%s without stray fields is valid: %v", status, err)
		}
		p.Target = "simulated_sum_success"
		if err := Validate(r, p); err == nil {
			t.Fatalf("%s carrying a success target was accepted", status)
		}
		if p.Target != "simulated_sum_success" {
			t.Fatal("validation must not change the proposal")
		}
	}
	p := proposal()
	p.Status, p.Missing, p.Action = "clarify", "", "fast"
	if err := Validate(resolved(), p); err == nil {
		t.Fatal("a clarification naming an action was accepted")
	}
}

// resolved is the fixture once the sample it names has been read: nothing is left to retrieve.
func resolved() Request {
	r := request()
	r.Evidence = append(r.Evidence, Evidence{Ref: Ref{"sample", "1", "fixture:sample"}, Facts: map[string]bool{"sample_ready": true}})
	return r
}

// Retrieve before asking: a question to the person is refused while evidence can still be read,
// and allowed once nothing is left to retrieve. Abstaining is allowed either way.
func TestAClarificationWaitsUntilNothingIsLeftToRetrieve(t *testing.T) {
	ask := proposal()
	ask.Status, ask.Missing = "clarify", ""
	if err := Validate(request(), ask); err == nil {
		t.Fatal("asking while the sample can still be read was accepted")
	}
	fetch := proposal()
	if err := Validate(request(), fetch); err != nil {
		t.Fatal("asking for the evidence instead is the valid move:", err)
	}
	if err := Validate(resolved(), ask); err != nil {
		t.Fatal("asking once nothing is left to retrieve is allowed:", err)
	}
	stop := ask
	stop.Status = "abstain"
	for _, r := range []Request{request(), resolved()} {
		if err := Validate(r, stop); err != nil {
			t.Fatal("abstaining is always open:", err)
		}
	}
}

// A contradiction is the exception: reading more cannot undo it, so asking is allowed at once.
func TestAClarificationAboutConflictingFactsNeedNotWait(t *testing.T) {
	r := request()
	r.Evidence = append(r.Evidence,
		Evidence{Ref: Ref{"format-a", "1", "fixture:a"}, Facts: map[string]bool{"ascii": true}},
		Evidence{Ref: Ref{"format-b", "1", "fixture:b"}, Facts: map[string]bool{"ascii": false}})
	ask := proposal()
	ask.Status, ask.Missing = "clarify", ""
	if err := Validate(r, ask); err != nil {
		t.Fatal("asking about contradicting facts must not wait for retrieval:", err)
	}
}
