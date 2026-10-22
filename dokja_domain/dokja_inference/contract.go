// Package inference owns bounded inference semantics, not provider transport or
// cognitive scheduling. It neither dispatches actions nor writes task state.
package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
)

const Version = "inference.v1"
const MaxRequestBytes = 32768
const MaxReplyBytes = 16384

type Ref struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Source  string `json:"source"`
}
type Evidence struct {
	Ref
	Facts    map[string]bool `json:"facts"`
	Requires []string        `json:"requires"`
	Question string          `json:"question"`
}
type Experience struct {
	ID         string `json:"id"`
	Action     string `json:"action"`
	Outcome    string `json:"observed_outcome"`
	Simulation bool   `json:"simulation"`
}
type Action struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}
type Request struct {
	Contract       string       `json:"contract"`
	TaskID         string       `json:"task_id"`
	Revision       int          `json:"revision"`
	Scope          string       `json:"scope"`
	Goal           string       `json:"goal"`
	Evidence       []Evidence   `json:"evidence"`
	Experiences    []Experience `json:"experiences"`
	Actions        []Action     `json:"actions"`
	RemainingCalls int          `json:"remaining_calls"`
}
type Proposal struct {
	Contract    string   `json:"contract"`
	Revision    int      `json:"revision"`
	Status      string   `json:"status"`
	Action      string   `json:"action"`
	Missing     string   `json:"missing"`
	Target      string   `json:"target"`
	Probability float64  `json:"probability"`
	Evidence    []string `json:"evidence_ids"`
	Experiences []string `json:"experience_ids"`
	Summary     string   `json:"summary"`
}
type Attempt struct {
	RequestedModel            string
	Failure                   string `json:",omitempty"`
	Provider                  string
	Model                     string
	Outcome                   string
	ElapsedMS                 int64
	InputTokens, OutputTokens *int64
}
type Result struct {
	Proposal Proposal
	Attempts []Attempt
}
type Provider interface {
	Infer(context.Context, Request) (Result, error)
}

func ValidateRequest(r Request) error {
	b, err := json.Marshal(r)
	if err != nil || len(b) > MaxRequestBytes || r.Contract != Version || r.TaskID == "" || r.Scope == "" || r.Revision < 1 || r.Goal == "" || r.RemainingCalls < 1 || r.RemainingCalls > 32 || len(r.Evidence) > 32 || len(r.Experiences) > 16 || len(r.Actions) == 0 || len(r.Actions) > 8 {
		return errors.New("invalid inference request")
	}
	ids := map[string]bool{}
	for _, e := range r.Evidence {
		if e.ID == "" || e.Version == "" || e.Source == "" || ids[e.ID] {
			return errors.New("invalid evidence reference")
		}
		ids[e.ID] = true
	}
	ids = map[string]bool{}
	for _, e := range r.Experiences {
		if e.ID == "" || ids[e.ID] || !e.Simulation || (e.Outcome != "success" && e.Outcome != "failed") {
			return errors.New("invalid simulation experience")
		}
		ids[e.ID] = true
	}
	ids = map[string]bool{}
	for _, a := range r.Actions {
		if a.ID == "" || a.Description == "" || ids[a.ID] {
			return errors.New("invalid action catalog")
		}
		ids[a.ID] = true
	}
	return nil
}
func Decode(r Request, body []byte) (Proposal, error) {
	var p Proposal
	if err := ValidateRequest(r); err != nil {
		return p, err
	}
	if len(body) > MaxReplyBytes {
		return p, errors.New("inference reply too large")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		return p, errors.New("invalid inference JSON")
	}
	for _, key := range []string{"contract", "revision", "status", "action", "missing", "target", "probability", "evidence_ids", "experience_ids", "summary"} {
		if raw, ok := fields[key]; !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return p, errors.New("missing inference field")
		}
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return p, errors.New("invalid inference JSON")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return p, errors.New("trailing inference data")
	}
	return p, Validate(r, p)
}
func Validate(r Request, p Proposal) error {
	if err := ValidateRequest(r); err != nil {
		return err
	}
	if p.Contract != Version || p.Revision != r.Revision {
		return errors.New("stale or incompatible inference")
	}
	if math.IsNaN(p.Probability) || math.IsInf(p.Probability, 0) || p.Probability < 0 || p.Probability > 1 || len(p.Summary) == 0 || len(p.Summary) > 1200 || len(p.Evidence) > 32 || len(p.Experiences) > 16 {
		return errors.New("invalid inference bounds")
	}
	known := map[string]bool{}
	missing := map[string]bool{}
	for _, e := range r.Evidence {
		known[e.ID] = true
		for _, id := range e.Requires {
			missing[id] = true
		}
		if e.Question != "" {
			missing[e.Question] = true
		}
	}
	used := map[string]bool{}
	for _, id := range p.Evidence {
		if !known[id] || used[id] {
			return errors.New("unknown or repeated evidence citation")
		}
		used[id] = true
	}
	episodes := map[string]bool{}
	for _, e := range r.Experiences {
		episodes[e.ID] = true
	}
	used = map[string]bool{}
	for _, id := range p.Experiences {
		if !episodes[id] || used[id] {
			return errors.New("unknown or repeated experience citation")
		}
		used[id] = true
	}
	switch p.Status {
	case "need_evidence":
		if p.Action != "" || !missing[p.Missing] || known[p.Missing] || len(p.Evidence) == 0 {
			return errors.New("unjustified evidence request")
		}
	case "propose":
		allowed := false
		for _, a := range r.Actions {
			if a.ID == p.Action {
				allowed = true
			}
		}
		if !allowed || p.Missing != "" || p.Target != "simulated_sum_success" || len(p.Evidence) == 0 {
			return errors.New("invalid action proposal")
		}
	case "abstain", "clarify":
		if p.Action != "" || p.Missing != "" {
			return errors.New("terminal inference cannot request effects")
		}
	default:
		return errors.New("unsupported inference status")
	}
	return nil
}

// Schema returns a fresh provider-neutral structured output schema. Validation
// above remains authoritative even when a provider accepts this schema.
func Schema() map[string]any {
	props := map[string]any{}
	for _, key := range []string{"contract", "status", "action", "missing", "target", "summary"} {
		props[key] = map[string]any{"type": "string"}
	}
	props["contract"] = map[string]any{"type": "string", "enum": []string{Version}}
	props["status"] = map[string]any{"type": "string", "enum": []string{"need_evidence", "propose", "abstain", "clarify"}}
	props["revision"] = map[string]any{"type": "integer"}
	props["probability"] = map[string]any{"type": "number"}
	for _, key := range []string{"evidence_ids", "experience_ids"} {
		props[key] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	}
	return map[string]any{"type": "object", "additionalProperties": false, "properties": props, "required": []string{"contract", "revision", "status", "action", "missing", "target", "probability", "evidence_ids", "experience_ids", "summary"}}
}
