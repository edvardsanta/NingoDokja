package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func code(side int) []string {
	rows := make([]string, side)
	for y := range rows {
		row := make([]byte, side)
		for x := range row {
			row[x] = "01"[(x+y)%2]
		}
		rows[y] = string(row)
	}
	return rows
}

func TestTheCodeIsDrawnInHalfBlocksWithAQuietZone(t *testing.T) {
	out, err := RenderLinkCode(code(21))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != (21+8+1)/2 {
		t.Fatalf("got %d lines", len(lines))
	}
	if !strings.HasPrefix(lines[0], "\x1b[97;40m████") || !strings.HasSuffix(lines[0], "\x1b[0m") {
		t.Fatalf("%q", lines[0])
	}
}

func TestMalformedCodesAreRefused(t *testing.T) {
	bad := code(21)
	bad[3] = strings.Replace(bad[3], "1", "x", 1)
	for _, rows := range [][]string{code(5), bad, append(code(21)[:20], "0")} {
		if _, err := RenderLinkCode(rows); err == nil {
			t.Fatal("expected a refusal")
		}
	}
}

func TestOnlyCommunicationsActionsCarryTheCredential(t *testing.T) {
	env := func(string) string { return "a-shared-credential-of-at-least-32-chars" }
	base := map[string]any{"interface": "cli"}
	if got := CommunicationsContext("services.set", base, env); got["communications_token"] != nil {
		t.Fatal("a non-communications action must not carry the credential")
	}
	got := CommunicationsContext("communications.link.status", base, env)
	if got["communications_token"] != "a-shared-credential-of-at-least-32-chars" || base["communications_token"] != nil {
		t.Fatal("the credential is added to a copy")
	}
}

func TestLinkStopsOnceTheAccountIsLinked(t *testing.T) {
	t.Setenv("DOKJA_COMMUNICATIONS_TOKEN", "a-shared-credential-of-at-least-32-chars")
	endpoint, received := fakeOrchestrator(t, `{"status":"ok","result":{"event_id":"e1","workflow":"system","domain":"system","result":{"interface":"whatsapp","state":"connected"}}}`)
	var runErr error
	out := captureStdout(t, func() {
		runErr = NewApp().Run(context.Background(), []string{"dokja-cli", "--request-endpoint", endpoint, "interfaces", "link", "whatsapp"})
	})
	if runErr != nil || !strings.Contains(out, "whatsapp: linked") {
		t.Fatalf("%v %q", runErr, out)
	}
	var event struct {
		Type    string         `json:"type"`
		Payload map[string]any `json:"payload"`
		Context map[string]any `json:"context"`
	}
	if err := json.Unmarshal([]byte(<-received), &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "communications.link.status" || event.Payload["interface"] != "whatsapp" || event.Context["communications_token"] == nil {
		t.Fatalf("%+v", event)
	}
	if strings.Contains(out, "a-shared-credential") {
		t.Fatal("the credential must never be printed")
	}
}

func TestOnlyLinkableInterfacesAreAccepted(t *testing.T) {
	if err := NewApp().Run(context.Background(), []string{"dokja-cli", "interfaces", "link", "discord"}); err == nil {
		t.Fatal("discord does not link to an account")
	}
}
