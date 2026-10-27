// Package chat owns context and presentation decisions; inference, rendering and transport remain integrations.
package chat

import (
	"regexp"
	"strings"
)

const MaxFormulas = 3
const MaxFormulaBytes = 1024
const MaxImageBytes = 256 << 10

type Formula struct{ LaTeX, Text string }
type Presentation struct {
	Text   string
	Images []Formula
}
type Attachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	DataB64     string `json:"data_b64"`
}

var mathBlock = regexp.MustCompile("(?ms)^```math\\r?\\n(.*?)^```[ \\t]*(?:\\r?\\n|$)")

// PlanMath keeps a readable fallback for every valid formula, whether rendering succeeds or not.
// Source-code fences such as latex/python are deliberately left intact.
func PlanMath(reply string) Presentation {
	plan := Presentation{}
	plan.Text = mathBlock.ReplaceAllStringFunc(reply, func(block string) string {
		match := mathBlock.FindStringSubmatch(block)
		var formula Formula
		force := false
		for _, line := range strings.Split(strings.TrimSpace(match[1]), "\n") {
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "latex: ") && formula.LaTeX == "":
				formula.LaTeX = strings.TrimSpace(strings.TrimPrefix(line, "latex: "))
			case strings.HasPrefix(line, "text: ") && formula.Text == "":
				formula.Text = strings.TrimSpace(strings.TrimPrefix(line, "text: "))
			case line == "image: true":
				force = true
			default:
				return block
			}
		}
		if formula.LaTeX == "" || formula.Text == "" || len(formula.Text) > 2048 {
			return block
		}
		if len(formula.LaTeX) <= MaxFormulaBytes && (needsImage(formula.LaTeX) || force) && len(plan.Images) < MaxFormulas {
			plan.Images = append(plan.Images, formula)
		}
		return formula.Text + "\n"
	})
	return plan
}

func needsImage(expression string) bool {
	if len(expression) > 80 {
		return true
	}
	if strings.Count(expression, "\\frac")+strings.Count(expression, "\\sqrt") > 1 {
		return true
	}
	return strings.Contains(expression, "\\int") || strings.Contains(expression, "\\sum")
}
