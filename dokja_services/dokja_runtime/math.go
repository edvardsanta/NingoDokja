package runtime

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"regexp"
	"strings"
	"sync"

	"github.com/go-latex/latex/drawtex"
	"github.com/go-latex/latex/drawtex/drawimg"
	"github.com/go-latex/latex/mtex"
)

var mathCommand = regexp.MustCompile(`\\([A-Za-z]+)`)
var mathCommands = func() map[string]bool {
	m := map[string]bool{}
	for _, name := range strings.Fields(`
		frac sqrt pm mp left right quad qquad cdot times div sum prod int infty partial
		alpha beta gamma delta epsilon theta lambda mu nu pi rho sigma tau phi chi psi omega
		Gamma Delta Theta Lambda Pi Sigma Phi Psi Omega
		sin cos tan log ln exp lim mathrm mathbf mathit text leq geq neq approx
		in notin subset supset cup cap to rightarrow ldots cdots vert Vert
		overline underline hat vec dot ddot
	`) {
		m[name] = true
	}
	return m
}()
var mathRenderMu sync.Mutex
var errMath = errors.New("unsupported or oversized mathematical expression")

// RenderMath uses a math parser, not a TeX installation or shell. Only bounded expressions enter it.
func RenderMath(ctx context.Context, expression string) (data []byte, err error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if len(expression) == 0 || len(expression) > 1024 || strings.ContainsAny(expression, "$%#&\r\n\x00") {
		return nil, errMath
	}
	depth := 0
	for _, r := range expression {
		if r < 32 {
			return nil, errMath
		}
		if r == '{' {
			depth++
			if depth > 16 {
				return nil, errMath
			}
		}
		if r == '}' {
			depth--
			if depth < 0 {
				return nil, errMath
			}
		}
	}
	if depth != 0 {
		return nil, errMath
	}
	for _, match := range mathCommand.FindAllStringSubmatch(expression, -1) {
		if !mathCommands[match[1]] {
			return nil, errMath
		}
	}
	// The dependency owns font caches; serialize access, including parsing.
	mathRenderMu.Lock()
	defer mathRenderMu.Unlock()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	defer func() {
		if recover() != nil {
			data = nil
			err = errMath
		}
	}()
	var raw bytes.Buffer
	if mtex.Render(boundedMathCanvas{dst: drawimg.NewRenderer(&raw)}, "$"+expression+"$", 20, 120, nil) != nil {
		return nil, errMath
	}
	img, err := png.Decode(bytes.NewReader(raw.Bytes()))
	if err != nil {
		return nil, errMath
	}
	// A white card keeps black mathematical symbols readable on dark Discord themes.
	bounds := img.Bounds()
	card := image.NewRGBA(image.Rect(0, 0, bounds.Dx()+32, bounds.Dy()+32))
	draw.Draw(card, card.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(card, bounds.Add(image.Pt(16, 16)), img, bounds.Min, draw.Over)
	var out bytes.Buffer
	if png.Encode(&out, card) != nil || out.Len() > 256<<10 {
		return nil, errMath
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return out.Bytes(), nil
}

type boundedMathCanvas struct{ dst *drawimg.Renderer }

func (r boundedMathCanvas) Render(w, h, dpi float64, canvas *drawtex.Canvas) error {
	w *= dpi
	h *= dpi
	if math.IsNaN(w) || math.IsInf(w, 0) || math.IsNaN(h) || math.IsInf(h, 0) || w < 1 || h < 1 || w > 2200 || h > 1000 || w*h > 1500000 {
		return errMath
	}
	return r.dst.Render(w/dpi, h/dpi, dpi, canvas)
}
