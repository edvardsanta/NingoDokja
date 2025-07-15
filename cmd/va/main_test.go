package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestPrintIfValid(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		wantIn string
	}{
		{"partial present", `{"partial":"hello"}`, "hello"},
		{"final text present", `{"text":"world"}`, "world"},
		{"empty partial", `{"partial":""}`, ""},
		{"empty text", `{"text":""}`, ""},
		{"unknown text", `{"text":"<UNK>"}`, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Capture stdout
			var sb strings.Builder
			old := stdOut
			stdOut = &sb
			defer func() { stdOut = old }()

			printIfValid(tc.input)

			if tc.wantIn != "" && !strings.Contains(sb.String(), tc.wantIn) {
				t.Errorf("expected output to contain %q, got %q", tc.wantIn, sb.String())
			}
		})
	}
}

var stdOut = &strings.Builder{}

func init() {
	fmtPrintln = func(a ...interface{}) (n int, err error) {
		return stdOut.WriteString(strings.TrimSpace(fmt.Sprint(a...)) + "\n")
	}
}

var fmtPrintln = fmt.Println
