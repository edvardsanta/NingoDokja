package inference

import (
	"encoding/json"
	"strings"
	"testing"
)

func request() Request {
	return Request{Contract: Version, TaskID: "task", Revision: 3, Scope: "owner", Goal: "simulate", RemainingCalls: 2, Evidence: []Evidence{{Ref: Ref{"rule", "1", "fixture:rule"}, Question: "sample"}}, Actions: []Action{{"fast", "parse"}}, Experiences: []Experience{{"old", "fast", "failed", true}}}
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
