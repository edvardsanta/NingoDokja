package clients

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serveStatus(t *testing.T, status int, body string) *InterfaceStatusClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return NewInterfaceStatusClient(server.URL + "/status")
}

func TestInterfaceStatusClientReadsLevelStateAndDetail(t *testing.T) {
	client := serveStatus(t, http.StatusOK, `{"status":"ok","state":"connected","detail":"bot and delivery"}`)

	level, state, detail, err := client.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if level != "ok" || state != "connected" || detail != "bot and delivery" {
		t.Fatalf("unexpected reply %q %q %q", level, state, detail)
	}
}

func TestInterfaceStatusClientCutsAnUnexpectedReplyDown(t *testing.T) {
	long := strings.Repeat("x", 500)
	client := serveStatus(t, http.StatusOK,
		`{"status":"fine","state":"Not A Name","detail":"`+long+`\u0000\u001b[31m"}`)

	level, state, detail, err := client.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if level != "error" {
		t.Fatalf("an unknown level must count as error, got %q", level)
	}
	if state != "unknown" {
		t.Fatalf("a state outside the name pattern must be unknown, got %q", state)
	}
	if len(detail) != interfaceDetailMax || strings.ContainsAny(detail, "\x00\x1b") {
		t.Fatalf("detail must be short and printable, got %d chars", len(detail))
	}
}

func TestInterfaceStatusClientRefusesErrorsAndNonJSON(t *testing.T) {
	if _, _, _, err := serveStatus(t, http.StatusInternalServerError, `{}`).Status(context.Background()); err == nil {
		t.Fatal("a failing status must be an error")
	}
	if _, _, _, err := serveStatus(t, http.StatusOK, `not json`).Status(context.Background()); err == nil {
		t.Fatal("a reply that is not JSON must be an error")
	}
}

func TestInterfaceStatusClientIsOffWithoutAnAddress(t *testing.T) {
	for _, endpoint := range []string{"", "  ", "dokja-whatsapp:8094/status", "ftp://x"} {
		if NewInterfaceStatusClient(endpoint) != nil {
			t.Fatalf("%q must not produce a client", endpoint)
		}
	}
	var missing *InterfaceStatusClient
	if _, _, _, err := missing.Status(context.Background()); err == nil {
		t.Fatal("a nil client must report it is not configured")
	}
}
