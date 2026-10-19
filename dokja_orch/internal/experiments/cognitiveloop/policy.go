package cognitiveloop

import "fmt"

// view is what the domain policy may read. It never writes the workspace.
type view struct {
	knowledge []Evidence
	recalled  []Evidence
	premises  map[string]bool
	rejected  map[string]bool
}

type decision struct {
	step    string
	premise string
	reject  string
	reason  string
}

// check reports whether every premise holds, which one fails, or which ones are unknown.
func check(requires []string, premises map[string]bool) (ok bool, failed string, unknown []string) {
	for _, premise := range requires {
		holds, known := premises[premise]
		switch {
		case !known:
			unknown = append(unknown, premise)
		case !holds:
			return false, premise, nil
		}
	}
	return len(unknown) == 0, "", unknown
}

// propose picks a general method from Knowledge and, when an observed procedure for it
// applies here, that procedure from Experience. Unknown premises are returned so the
// caller can ask for them before settling.
func propose(v view) (*Candidate, []string, string) {
	var method string
	var missing []string
	failed := ""
	for _, e := range v.knowledge {
		if e.Method == "" {
			continue
		}
		_, fails, unknown := check(e.Requires, v.premises)
		if fails != "" {
			failed = fails
			continue
		}
		method, missing = e.Method, unknown
		break
	}
	if method == "" {
		if failed != "" {
			return nil, nil, fmt.Sprintf("no general method applies: %s does not hold", failed)
		}
		return nil, nil, "no general method in knowledge"
	}
	candidate := &Candidate{Method: method, From: "knowledge-only"}
	for _, e := range v.knowledge {
		if e.Method == method {
			candidate.Supports = append(candidate.Supports, e.ID)
		}
	}
	for _, e := range v.recalled {
		if e.Method != method || e.Procedure == "" || v.rejected[e.Procedure] {
			continue
		}
		ok, _, unknown := check(e.Requires, v.premises)
		if ok {
			candidate.Procedure, candidate.From = e.Procedure, "experience"
			candidate.Supports = append(candidate.Supports, e.ID)
			return candidate, missing, ""
		}
		missing = append(missing, unknown...)
	}
	return candidate, missing, ""
}

// decide turns a candidate and its hypothetical prediction into the next step.
func decide(candidate *Candidate, prediction Prediction, premises map[string]bool) decision {
	if prediction.Missing != "" {
		if _, known := premises[prediction.Missing]; !known {
			return decision{step: stepNeedEvidence, premise: prediction.Missing}
		}
	}
	if prediction.Probability < minPredictedSuccess {
		if candidate.From == "experience" {
			return decision{step: stepRevise, reject: candidate.Procedure,
				reason: fmt.Sprintf("predicted success %.2f for %s", prediction.Probability, candidate.Procedure)}
		}
		return decision{step: OutcomeAbstain, reason: fmt.Sprintf("predicted success %.2f is below %.2f", prediction.Probability, minPredictedSuccess)}
	}
	return decision{step: OutcomeReady}
}

// RecordObserved builds the experience of a finished task from an observed outcome. A
// prediction is never an observation: without a ready result and a real outcome nothing
// is recorded, and an unknown outcome stays an unresolved attempt.
func RecordObserved(task Task, result Result, observed string) (Evidence, bool) {
	if result.Outcome != OutcomeReady || result.Candidate == nil || observed == "" || observed == "unknown" {
		return Evidence{}, false
	}
	procedure := result.Candidate.Procedure
	if procedure == "" {
		procedure = result.Candidate.Method
	}
	return Evidence{
		ID: fmt.Sprintf("experience:%s", task.ID), Kind: KindExperience, Scope: task.Scope,
		Method: result.Candidate.Method, Procedure: procedure, Source: fmt.Sprintf("observed:%s", observed),
	}, true
}
