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
