package knowledge

import (
	"strings"
	"testing"
)

func pinnedSupervisionRequest() ApplicationRequest {
	return ApplicationRequest{Contract: ApplicationContract, Goal: "Apply the version-one constraints", Sources: []ApplicationSelection{{SourceID: "guide", TextVersion: "sha256:" + strings.Repeat("a", 64)}}}
}
func TestSupervisionRequiresExplicitPinnedCandidates(t *testing.T) {
	request := pinnedSupervisionRequest()
	if _, err := ValidateSupervisedApplication(request); err != nil {
		t.Fatal(err)
	}
	command := SourceControl{Action: "redirect", Reason: "The previous plan uses the wrong version", Application: &request}
	if err := ValidateSourceControl(command); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*SourceControl){
		func(c *SourceControl) { c.Reason = " " }, func(c *SourceControl) { c.Reason = strings.Repeat("x", 1001) }, func(c *SourceControl) { c.Application = nil }, func(c *SourceControl) { c.Action = "execute" }, func(c *SourceControl) { c.Action = "resume" },
	} {
		candidate := command
		mutate(&candidate)
		if ValidateSourceControl(candidate) == nil {
			t.Fatalf("accepted invalid correction: %+v", candidate)
		}
	}
	request.Sources[0].TextVersion = ""
	if _, err := ValidateSupervisedApplication(request); err == nil {
		t.Fatal("accepted unpinned supervised source")
	}
	request = pinnedSupervisionRequest()
	request.Goal = ""
	if _, err := ValidateSupervisedApplication(request); err == nil {
		t.Fatal("bypassed existing Knowledge request validation")
	}
	for _, action := range []string{"pause", "resume", "cancel"} {
		if err := ValidateSourceControl(SourceControl{Action: action}); err != nil {
			t.Fatal(err)
		}
	}
}
