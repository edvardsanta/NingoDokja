//go:build unix

package feeds

import (
	"context"
	"flag"
	"strings"
	"testing"
	"time"
)

var livePDFURL = flag.String("pdf-url", "", "opt-in public PDF URL for a live reader check")

func TestLiveWebPDF(t *testing.T) {
	if *livePDFURL == "" {
		t.Skip("supply -args -pdf-url to opt in to a network read")
	}
	ctx, cancel := context.WithTimeout(t.Context(), webTimeout)
	defer cancel()
	client := newWebReader().client()
	defer client.CloseIdleConnections()
	result, err := fetchWeb(ctx, client, *livePDFURL, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	passages := result["passages"].([]webPassage)
	if len(passages) == 0 || !strings.HasPrefix(passages[0].Label, "Page ") {
		t.Fatal("PDF has no original-page evidence")
	}
	t.Logf("PDF read: passages=%d first_page=%s partial=%v", len(passages), passages[0].Label, result["partial"])
}
