package runtime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/http/httptest"
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

func TestRenderMathPrefersTheConfiguredMathService(t *testing.T) {
	want := testPNG(t)
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/math/render" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var body struct {
			Expression string `json:"expression"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Expression == "" {
			t.Errorf("bad request body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"data_b64": base64.StdEncoding.EncodeToString(want), "content_type": "image/png"})
	}))
	defer service.Close()
	t.Setenv("DOKJA_MATH_ENDPOINT", service.URL)

	data, err := RenderMath(t.Context(), `\rho_\beta = e^{-\beta \hat{H}}`)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, want) {
		t.Fatal("the service render was not used")
	}
}

func TestRenderMathFallsBackToTheLocalRenderer(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer service.Close()
	t.Setenv("DOKJA_MATH_ENDPOINT", service.URL)

	// A flat formula the built-in renderer accepts: the failure above must not
	// change behavior for what it can do.
	data, err := RenderMath(t.Context(), `y=\frac{a+b}{2}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
}

func TestRenderMathFallsBackAndStillRejects(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}))
	defer service.Close()
	t.Setenv("DOKJA_MATH_ENDPOINT", service.URL)

	// Unrenderable everywhere: the local renderer must have the last word.
	if data, err := RenderMath(t.Context(), `\unknown{x}`); err == nil || len(data) != 0 {
		t.Fatal("an invalid expression was rendered through the fallback")
	}
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 24, 24))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
