package knowledge

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SourceControl is a candidate control command, never an authentication grant.
// Redirects carry explicit structured changes; reason text is not executed or
// silently interpreted as a replacement method, source or factual observation.
type SourceControl struct {
	OperationRef     string
	ExpectedRevision uint64
	Action           string
	Reason           string
	Application      *ApplicationRequest
}

// ValidateSupervisedApplication reuses Knowledge's request/selection policy.
// Pinned read eligibility does not establish semantic applicability of a method.
func ValidateSupervisedApplication(request ApplicationRequest) (ApplicationRequest, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return ApplicationRequest{}, fmt.Errorf("invalid supervised application")
	}
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return ApplicationRequest{}, fmt.Errorf("invalid supervised application")
	}
	parsed, err := ParseApplicationRequest(payload)
	if err != nil {
		return ApplicationRequest{}, err
	}
	for _, source := range parsed.Sources {
		if source.TextVersion == "" {
			return ApplicationRequest{}, fmt.Errorf("supervised reads require every source pin")
		}
	}
	return parsed, nil
}

func ValidateSourceControl(command SourceControl) error {
	if len(command.Reason) > 1000 {
		return fmt.Errorf("control reason exceeds budget")
	}
	switch command.Action {
	case "redirect":
		if strings.TrimSpace(command.Reason) == "" || command.Application == nil {
			return fmt.Errorf("redirect requires reasons and an explicit replacement application")
		}
		_, err := ValidateSupervisedApplication(*command.Application)
		return err
	case "pause", "resume", "cancel":
		if command.Application != nil {
			return fmt.Errorf("only redirect may replace the application")
		}
		return nil
	default:
		return fmt.Errorf("unsupported source control")
	}
}
