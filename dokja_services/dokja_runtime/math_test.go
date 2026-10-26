package runtime

import (
	"bytes"
	"context"
	"image/png"
	"strings"
	"testing"
)

func TestRenderMathProducesReadablePNG(t *testing.T) {
	data, err := RenderMath(t.Context(), `y=\frac{s\pm\sqrt{-2p-z-\frac{2q}{s}}}{2}`)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() < 40 || img.Bounds().Dy() < 40 {
		t.Fatal("formula image is empty")
	}
	r, g, b, a := img.At(0, 0).RGBA()
	if r != 65535 || g != 65535 || b != 65535 || a != 65535 {
		t.Fatal("formula card must have an opaque white background")
	}
	dark := false
	for y := 16; y < img.Bounds().Dy()-16 && !dark; y++ {
		for x := 16; x < img.Bounds().Dx()-16; x++ {
			r, _, _, _ := img.At(x, y).RGBA()
			if r < 32768 {
				dark = true
				break
			}
		}
	}
	if !dark {
		t.Fatal("formula has no visible symbols")
	}
}

func TestRenderMathRejectsCommandsAndResourceAbuse(t *testing.T) {
	for _, expression := range []string{
		`\input{anything}`, `\write18{anything}`, `\includegraphics{file}`,
		strings.Repeat("{", 17) + "x" + strings.Repeat("}", 17),
		strings.Repeat("x", 1025), strings.Repeat("x+", 450), "$x$", "x\ny", `\unknown{x}`,
	} {
		if data, err := RenderMath(t.Context(), expression); err == nil || len(data) != 0 {
			t.Fatalf("accepted invalid expression of length %d", len(expression))
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RenderMath(ctx, "x"); err == nil {
		t.Fatal("canceled render succeeded")
	}
}
