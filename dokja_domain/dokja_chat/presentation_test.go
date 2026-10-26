package chat

import (
	"strings"
	"testing"
)

func TestMathPlanKeepsFallbackAndRendersOnlyComplexOrRequestedImages(t *testing.T) {
	for _, tc := range []struct {
		expression, extra string
		images            int
	}{
		{`x^2+1`, "", 0},
		{`x^2+1`, "image: true\n", 1},
		{`x=\frac{-b\pm\sqrt{b^2-4ac}}{2a}`, "", 1},
	} {
		block := "```math\nlatex: " + tc.expression + "\ntext: readable formula\n" + tc.extra + "```\n"
		plan := PlanMath("Before\n" + block + "After")
		if plan.Text != "Before\nreadable formula\nAfter" || len(plan.Images) != tc.images {
			t.Fatalf("unexpected presentation: %#v", plan)
		}
	}
}

func TestMathPlanPreservesRawSourceAndBoundsImages(t *testing.T) {
	source := "```latex\n\\frac{a}{b}\n```\n"
	if plan := PlanMath(source); plan.Text != source || len(plan.Images) != 0 {
		t.Fatal("LaTeX requested as source was interpreted as a render request")
	}
	block := "```math\nlatex: x\ntext: fallback\nimage: true\n```\n"
	plan := PlanMath(strings.Repeat(block, 6))
	if len(plan.Images) != MaxFormulas || strings.Count(plan.Text, "fallback") != 6 {
		t.Fatal("image cap discarded a text fallback")
	}
}

func TestMathPlanRequiresFallbackAndBoundsExpressions(t *testing.T) {
	invalid := "```math\nlatex: x\n```\n"
	if plan := PlanMath(invalid); plan.Text != invalid || len(plan.Images) != 0 {
		t.Fatal("formula without a fallback must remain intact")
	}
	block := "```math\nlatex: " + strings.Repeat("x", MaxFormulaBytes+1) + "\ntext: fallback\nimage: true\n```\n"
	if plan := PlanMath(block); plan.Text != "fallback\n" || len(plan.Images) != 0 {
		t.Fatal("oversized expression did not fall back to text")
	}
}
