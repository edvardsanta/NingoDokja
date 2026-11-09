package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Regression: the price and quiz domains were added to go.mod but not to Dockerfile.dev, whose
// dependency step copies each local module's go.mod by hand, so the dev and lite stacks stopped
// building ("open .../dokja_price/go.mod: no such file or directory").
func TestTheDevImageCopiesEveryLocalModuleTheOrchestratorReplaces(t *testing.T) {
	goMod, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	dockerfile, err := os.ReadFile("../../Dockerfile.dev")
	if err != nil {
		t.Fatal(err)
	}
	replaced := regexp.MustCompile(`(?m)^replace \S+ => \.\./(\S+)$`).FindAllStringSubmatch(string(goMod), -1)
	if len(replaced) == 0 {
		t.Fatal("found no local replacements in go.mod")
	}
	for _, match := range replaced {
		module := match[1]
		if !strings.Contains(string(dockerfile), "COPY "+module+"/go.mod ") {
			t.Errorf("Dockerfile.dev does not copy %s/go.mod before go mod download", module)
		}
	}
}
