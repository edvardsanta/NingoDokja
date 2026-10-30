package clients

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func square(side int) []string {
	rows := make([]string, side)
	for i := range rows {
		rows[i] = strings.Repeat("01", side)[:side]
	}
	return rows
}

func linkServer(t *testing.T, handler http.HandlerFunc) (*InterfaceLinkClient, *[]string) {
	t.Helper()
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return NewInterfaceLinkClient(server.URL + "/status"), &seen
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func TestInterfaceLinkClientDerivesTheRoutesFromTheStatusAddress(t *testing.T) {
	client, seen := linkServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"state": "waiting_for_link", "matrix": square(25)})
	})

	state, matrix, err := client.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Reset(context.Background()); err != nil {
		t.Fatal(err)
	}

	if state != "waiting_for_link" || len(matrix) != 25 {
		t.Fatalf("unexpected reply %q %d", state, len(matrix))
	}
	want := []string{"GET /link", "POST /link/start", "POST /link/reset"}
	if strings.Join(*seen, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected requests %v", *seen)
	}
}

func TestInterfaceLinkClientAcceptsAReplyWithoutACode(t *testing.T) {
	client, _ := linkServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"state": "connected"})
	})

	state, matrix, err := client.Status(context.Background())
	if err != nil || state != "connected" || matrix != nil {
		t.Fatalf("unexpected reply %q %v %v", state, matrix, err)
	}
}

func TestInterfaceLinkClientRefusesWhatIsNotAGridOfModules(t *testing.T) {
	bad := map[string][]string{
		"too small":    square(5),
		"too large":    square(200),
		"not square":   append(square(25)[:24], "0101"),
		"markup":       append(square(25)[:24], "<svg onload=x>"+strings.Repeat("0", 11)),
		"other digits": append(square(25)[:24], strings.Repeat("2", 25)),
	}
	for name, rows := range bad {
		t.Run(name, func(t *testing.T) {
			client, _ := linkServer(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, map[string]any{"state": "waiting_for_link", "matrix": rows})
			})
			if _, _, err := client.Status(context.Background()); err == nil {
				t.Fatal("a reply that is not a grid of modules must be refused")
			}
		})
	}
}

func TestInterfaceLinkClientReportsFailuresWithoutTheBody(t *testing.T) {
	client, _ := linkServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"EACCES /auth/default/creds.json"}`))
	})

	_, err := client.Reset(context.Background())
	if err == nil || strings.Contains(err.Error(), "creds.json") {
		t.Fatalf("expected an error that does not echo the body, got %v", err)
	}
}

func TestInterfaceLinkClientIsOffWithoutAnAddress(t *testing.T) {
	for _, endpoint := range []string{"", "dokja-whatsapp:8094/status"} {
		if NewInterfaceLinkClient(endpoint) != nil {
			t.Fatalf("%q must not produce a client", endpoint)
		}
	}
	var missing *InterfaceLinkClient
	if _, _, err := missing.Status(context.Background()); err == nil {
		t.Fatal("a nil client must report it is not configured")
	}
}

func TestInterfaceGroupsKeepsOnlyWellFormedGroupsWithBoundedNames(t *testing.T) {
	client, seen := linkServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"state": "connected", "groups": []map[string]any{
			{"id": "120363000000000001@g.us", "name": "  ningo  ", "members": 1},
			{"id": "5511900000000@s.whatsapp.net", "name": "a person", "members": 2},
			{"id": "120363000000000002@g.us", "name": strings.Repeat("x", 150), "members": -3},
		}})
	})
	groups, err := client.Groups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 || groups[0].ID != "120363000000000001@g.us" || groups[0].Name != "ningo" || groups[0].Members != 1 {
		t.Fatalf("unexpected groups %#v", groups)
	}
	if len([]rune(groups[1].Name)) != 100 || groups[1].Members != 0 {
		t.Fatalf("a name must be bounded and a count never negative: %#v", groups[1])
	}
	if (*seen)[0] != "GET /link/groups" {
		t.Fatal(*seen)
	}
}

func TestInterfaceGroupsFailsWhileTheAccountIsNotConnected(t *testing.T) {
	client, _ := linkServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	})
	if _, err := client.Groups(context.Background()); err == nil {
		t.Fatal("a refusal must be an error")
	}
}
