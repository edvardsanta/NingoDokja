package feeds

import "testing"

func TestPDFLimitConfiguration(t *testing.T) {
	for _, c := range []struct {
		value string
		want  int64
	}{{"", 25 << 20}, {"invalid", 25 << 20}, {"0", 25 << 20}, {"101", 25 << 20}, {"2", 2 << 20}, {"100", 100 << 20}} {
		t.Setenv("DOKJA_PDF_MAX_MIB", c.value)
		if got := pdfLimitBytes(); got != c.want {
			t.Fatalf("%q: got %d want %d", c.value, got, c.want)
		}
	}
}
