package cli

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const fixtureBridgeToken = "fixture-bridge-credential"

func streamServer(t *testing.T, handler http.HandlerFunc) *BridgeStream {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	stream, ok := NewBridgeStream(server.URL, fixtureBridgeToken, server.Client())
	if !ok {
		t.Fatal("a local bridge with a credential is usable")
	}
	return stream
}

func TestTheStreamSaysWhereTheSendStandsOnceEachThenGivesTheAnswer(t *testing.T) {
	var seen *http.Request
	var body string
	stream := streamServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		_, _ = io.WriteString(w, `{"progress":"queued"}`+"\n"+`{"progress":"queued"}`+"\n"+`{"progress":"dispatched"}`+"\n"+
			`{"preview":"Half"}`+"\n"+`{"status":"ok","result":{"result":{"reply":"Whole"}}}`+"\n")
	})
	var states []string
	response, err := stream.Chat(context.Background(), Event{EventID: "e-1", Source: "cli", Type: "message.created"}, func(state string) { states = append(states, state) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(states, ",") != "queued,dispatched" {
		t.Fatal("each state is told once, as it changes", states)
	}
	if reply, err := extractChatReply(response.Result); err != nil || reply != "Whole" {
		t.Fatal(reply, err)
	}
	if seen.URL.Path != "/orchestrator/chat/stream" || seen.Header.Get("Authorization") != "Bearer "+fixtureBridgeToken || !strings.Contains(body, `"source":"cli"`) {
		t.Fatal(seen.URL.Path, seen.Header, body)
	}
}

func TestOnlyAStreamThatNeverReachedTheOrchestratorIsMissing(t *testing.T) {
	notFound := streamServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	if _, err := notFound.Chat(context.Background(), Event{}, nil); !errors.Is(err, ErrBridgeMissing) {
		t.Fatal("a bridge with no chat stream is missing", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := "http://" + listener.Addr().String()
	listener.Close()
	refused, _ := NewBridgeStream(closed, fixtureBridgeToken, nil)
	if _, err := refused.Chat(context.Background(), Event{}, nil); !errors.Is(err, ErrBridgeMissing) {
		t.Fatal("a bridge that is not listening is missing", err)
	}

	// Past here the send may have reached the orchestrator, so none of these may be sent again.
	for name, handler := range map[string]http.HandlerFunc{
		"refused credential": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) },
		"bridge failure":     func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) },
		"orchestrator error": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"status":"error","error":"no"}`+"\n")
		},
		"cut off": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"progress":"dispatched"}`+"\n")
		},
	} {
		_, err := streamServer(t, handler).Chat(context.Background(), Event{}, nil)
		if err == nil || errors.Is(err, ErrBridgeMissing) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestTheCredentialIsNeverSentOffThisMachine(t *testing.T) {
	// A private address is not this machine either; it is built here so no address is written out.
	private := (&url.URL{Scheme: "http", Host: net.JoinHostPort("10.0.0.5", "8091")}).String()
	for _, endpoint := range []string{"http://bridge.example:8091", private, "ftp://localhost:8091", "", "not a url"} {
		if _, ok := NewBridgeStream(endpoint, fixtureBridgeToken, nil); ok {
			t.Fatalf("%q must not be used", endpoint)
		}
	}
	if _, ok := NewBridgeStream("http://localhost:8091", "  ", nil); ok {
		t.Fatal("no credential, no bridge")
	}
	loopback6 := (&url.URL{Scheme: "http", Host: net.JoinHostPort("::1", "8091")}).String()
	for _, endpoint := range []string{"http://localhost:8091", "http://127.0.0.1:8091", loopback6} {
		if _, ok := NewBridgeStream(endpoint, fixtureBridgeToken, nil); !ok {
			t.Fatalf("%q is this machine", endpoint)
		}
	}
}

func archivedSession() chatSessionOptions {
	id := conversationID("e")
	return chatSessionOptions{target: chatTarget{reference: id}, conversationID: &id}
}

func TestAnArchivedCLISendSaysWhereItStandsOnStderrAndNeverUsesTheSocket(t *testing.T) {
	stream := streamServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"progress":"queued"}`+"\n"+`{"progress":"dispatched"}`+"\n"+`{"status":"ok","result":{"result":{"reply":"Answer"}}}`+"\n")
	})
	app := NewApp()
	var stderr strings.Builder
	app.stderr = &stderr
	app.bridge = func() (*BridgeStream, bool) { return stream, true }
	app.newRequester = func(string) (*Requester, error) {
		t.Fatal("a streamed send is not sent again over the socket")
		return nil, nil
	}
	if err := app.chatOnce(context.Background(), "why?", archivedSession()); err != nil {
		t.Fatal(err)
	}
	if stderr.String() != "(waiting behind an earlier reply)\n(being answered)\n" {
		t.Fatalf("%q", stderr.String())
	}
}

func TestAnArchivedCLISendUsesTheSocketOnlyWhenTheStreamWasNeverReached(t *testing.T) {
	missing := streamServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	app := NewApp()
	app.bridge = func() (*BridgeStream, bool) { return missing, true }
	asked := false
	app.newRequester = func(string) (*Requester, error) {
		asked = true
		return nil, errors.New("fixture socket")
	}
	if err := app.chatOnce(context.Background(), "why?", archivedSession()); err == nil || !asked {
		t.Fatal("a bridge without a stream falls back to the socket", err)
	}

	refused := streamServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	app.bridge = func() (*BridgeStream, bool) { return refused, true }
	asked = false
	if err := app.chatOnce(context.Background(), "why?", archivedSession()); err == nil || asked {
		t.Fatal("a send that may have reached the orchestrator is never sent again", err)
	}
}

func TestASessionChatNeverUsesTheStream(t *testing.T) {
	app := NewApp()
	app.bridge = func() (*BridgeStream, bool) {
		t.Fatal("only archived sends, which carry a send id, are streamed")
		return nil, false
	}
	app.newRequester = func(string) (*Requester, error) { return nil, errors.New("fixture socket") }
	if err := app.chatOnce(context.Background(), "hi", chatSessionOptions{sessionKey: "k"}); err == nil {
		t.Fatal("the socket answered nothing")
	}
}
