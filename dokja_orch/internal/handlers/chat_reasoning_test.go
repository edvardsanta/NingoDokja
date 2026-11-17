package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	chat "read_books/dokja_domain/dokja_chat"
	conversation "read_books/dokja_domain/dokja_conversation"
	"read_books/internal/clients"
	"read_books/internal/core"
	"read_books/internal/logger"
)

const privateScratch = "private-scratch-marker-never-publish"

type reasoningGenerator struct {
	fakeChatGenerator
	raw          string
	taskErr      error
	wait         bool
	tasks        int
	normal       int
	taskMessages []map[string]string
	triage       *bool
	t            *testing.T
}

func (g *reasoningGenerator) GenerateMessages(ctx context.Context, messages []map[string]string) (map[string]any, error) {
	g.normal++
	if ctx.Err() != nil {
		g.t.Fatal("reasoning canceled the fallback context")
	}
	for _, m := range messages {
		if m["content"] == chatReasoningBrief() {
			g.t.Fatal("reasoning instructions reached normal generation")
		}
	}
	return g.fakeChatGenerator.GenerateMessages(ctx, messages)
}

func (g *reasoningGenerator) GenerateTaskJSON(ctx context.Context, profile string, schema map[string]any, messages []map[string]string, accept func(map[string]any) error) (map[string]any, error) {
	if profile == "lightweight" { // Reading triage uses its own existing contract.
		if g.triage != nil {
			raw, _ := json.Marshal(map[string]bool{"uses_reading": *g.triage})
			result := map[string]any{"reply": string(raw)}
			return result, accept(result)
		}
		return nil, errors.New("no triage verdict")
	}
	g.tasks++
	g.taskMessages = messages
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > chatReasoningBudget || time.Until(deadline) < 19*time.Second {
		g.t.Fatal("missing bounded reasoning deadline")
	}
	if profile != "analysis" || schema["additionalProperties"] != false {
		g.t.Fatal("wrong task profile or schema")
	}
	if len(messages) < 3 || !strings.HasPrefix(messages[0]["content"], chatPersona()) || messages[1]["content"] != chatReasoningBrief() {
		g.t.Fatal("reasoning brief must follow persona")
	}
	if g.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if g.taskErr != nil {
		return nil, g.taskErr
	}
	result := map[string]any{"reply": g.raw, "provider": "fixture", "model": "large", "analysis": privateScratch}
	if err := accept(result); err != nil {
		return nil, err
	}
	return result, nil
}

func newReasoningGenerator(t *testing.T, reply string) *reasoningGenerator {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"analysis": privateScratch, "reply": reply})
	return &reasoningGenerator{t: t, raw: string(raw), fakeChatGenerator: fakeChatGenerator{result: map[string]any{"reply": "normal fallback"}}}
}

// The two public reply paths must apply the same privacy and fallback contract.
func TestReasonedTurnsKeepOnlyReplyAndFallback(t *testing.T) {
	for _, archived := range []bool{false, true} {
		path := "ordinary"
		if archived {
			path = "archived"
		}
		for _, failure := range []string{"success", "failure", "timeout", "empty", "invalid JSON", "missing analysis", "wrong type", "extra field"} {
			t.Run(path+"/"+failure, func(t *testing.T) {
				g := newReasoningGenerator(t, "Ningo's reply to reader (no tools used):\nFinal answer.")
				switch failure {
				case "failure":
					g.taskErr = errors.New(privateScratch)
				case "timeout":
					g.wait = true
				case "empty":
					g.raw = `{"analysis":"` + privateScratch + `","reply":"   "}`
				case "invalid JSON":
					g.raw = privateScratch
				case "missing analysis":
					g.raw = `{"reply":"answer"}`
				case "wrong type":
					g.raw = `{"analysis":null,"reply":"answer"}`
				case "extra field":
					g.raw = `{"analysis":"private","reply":"answer","other":"unexpected"}`
				}
				logPath := filepath.Join(t.TempDir(), "chat.log")
				logger.InitWithOptions(false, logPath)
				t.Cleanup(func() { logger.InitWithOptions(false, "") })
				h := NewChatDomainHandler(g)
				event := chatEvent("Compare these two proposed explanations")
				var archive *conversation.Archive
				var id string
				if archived {
					archive = archiveFor(t)
					h.WithArchive(archive)
					held, err := archive.Start(t.Context(), conversation.OwnerScope, conversation.KindGeneral, "", "")
					if err != nil {
						t.Fatal(err)
					}
					id = held.ID
					event = ownerTurn(map[string]any{"content": event.Payload["content"], "conversation_id": id, "send_id": "reasoning-1"})
				}
				result, err := h.Handle(t.Context(), event, chatStep)
				if err != nil {
					t.Fatal(err)
				}
				want, normal := "Final answer.", 0
				if failure != "success" {
					want, normal = "normal fallback", 1
				}
				if result["reply"] != want || g.tasks != 1 || g.normal != normal {
					t.Fatalf("reply=%v tasks=%d fallback=%d", result["reply"], g.tasks, g.normal)
				}
				encoded, _ := json.Marshal(result)
				if strings.Contains(string(encoded), privateScratch) {
					t.Fatal("scratch leaked into result")
				}
				if archived {
					turns, err := archive.Turns(t.Context(), conversation.OwnerScope, id, 0, 10)
					if err != nil || len(turns) != 1 || turns[0].Reply != want {
						t.Fatal("wrong archived reply", err)
					}
					encoded, _ = json.Marshal(turns)
				} else {
					state := h.sessions.items[chatSessionBaseKey(event)]
					if len(state.messages) != 2 || state.messages[1].Content != want {
						t.Fatal("wrong session history")
					}
					encoded, _ = json.Marshal(state.messages)
				}
				if strings.Contains(string(encoded), privateScratch) {
					t.Fatal("scratch leaked into history")
				}
				logs, err := os.ReadFile(logPath)
				if err != nil || strings.Contains(string(logs), privateScratch) {
					t.Fatal("scratch leaked into logs", err)
				}
			})
		}
	}
}

func TestReasonedWebReplyStillChecksQuotesAndCitations(t *testing.T) {
	for _, archived := range []bool{false, true} {
		g := newReasoningGenerator(t, "Ningo's reply to reader (no tools used):\n\"A wholly fabricated legal requirement.\" [1] Include the financial impact estimate. [1] Read https://attacker.example/collect @everyone [999]")
		h := NewChatDomainHandler(g).WithWebReading(&webReadingService{}, nil)
		event := chatEvent("Compare the supplied passages https://example.com/law")
		if archived {
			a := archiveFor(t)
			h.WithArchive(a)
			_, err := a.Start(t.Context(), conversation.OwnerScope, conversation.KindGeneral, "", "")
			if err != nil {
				t.Fatal(err)
			}
			event = whatsappTurn(event.Payload["content"].(string), "web-reasoning", true)
		}
		result, err := h.Handle(t.Context(), event, chatStep)
		if err != nil {
			t.Fatal(err)
		}
		reply, _ := result["reply"].(string)
		for _, forbidden := range []string{privateScratch, "wholly fabricated", "attacker.example", "@everyone", "[999]", "reply to reader"} {
			if strings.Contains(reply, forbidden) {
				t.Fatalf("archived=%v unchecked reply contains %q: %s", archived, forbidden, reply)
			}
		}
		if g.tasks != 1 || g.normal != 0 || !strings.Contains(reply, "https://example.com/law") || !strings.Contains(reply, "financial impact estimate") {
			t.Fatal("reasoned reply lost grounding or citations")
		}
	}
}

func TestKeptReadingCanEnableReasoningAfterTriage(t *testing.T) {
	for _, relevant := range []bool{false, true} {
		g := newReasoningGenerator(t, "A grounded answer. [1]")
		g.triage = &relevant
		h := NewChatDomainHandler(g).WithWebReading(&webReadingService{}, nil)
		if _, err := h.Handle(t.Context(), chatEvent("https://example.com/law"), chatStep); err != nil {
			t.Fatal(err)
		}
		if _, err := h.Handle(t.Context(), chatEvent("And then who decides?"), chatStep); err != nil {
			t.Fatal(err)
		}
		want := 0
		if relevant {
			want = 1
		}
		if g.tasks != want {
			t.Fatalf("triage=%v reasoning calls=%d", relevant, g.tasks)
		}
	}
}

func TestReasoningSkipsVoiceAndPreviewEvenWithEvidence(t *testing.T) {
	for _, mode := range []string{"voice", "spoken", "preview", "ordinary noise"} {
		g := newReasoningGenerator(t, "reasoned")
		h := NewChatDomainHandler(g)
		event := chatEvent("Compare these two proposed explanations")
		event.Context = map[string]any{"web_reading_turn": &webTurnData{}}
		ctx := t.Context()
		switch mode {
		case "voice":
			event.Context["voice_turns"] = []chat.VoiceTurn{}
		case "spoken":
			event.Source = core.SourceDesktop
			event.Payload["spoken"] = true
		case "preview":
			ctx = core.WithReplyPreview(ctx, func(string) { t.Fatal("unexpected preview") })
		case "ordinary noise":
			event.Payload["content"] = "AAAAAAAAAAAA"
		}
		content := event.Payload["content"].(string)
		_, err := h.generateReply(ctx, event, content, []chatMessage{{Role: "user", Content: content}}, []knowledgeSource{{SourceID: "web:one", Web: true}}, "", "")
		if err != nil || g.tasks != 0 || g.normal != 1 {
			t.Fatalf("mode=%s tasks=%d normal=%d err=%v", mode, g.tasks, g.normal, err)
		}
	}
}

func TestReasoningCountsDocumentsAndUsesKeptReadingTriage(t *testing.T) {
	event := chatEvent("What does the article conclude?")
	event.Context = map[string]any{"web_reading_turn": &webTurnData{}}
	turn := reasoningTurn(t.Context(), event, event.Payload["content"].(string), []knowledgeSource{{SourceID: "web:one", Web: true}, {SourceID: "web:one", Web: true}})
	if turn.SourceCount != 1 || !turn.ReadingRelevant || !chat.NeedsReasoning(turn) {
		t.Fatal("reading decision lost or passages counted as documents")
	}
	delete(event.Context, "web_reading_turn")
	turn = reasoningTurn(t.Context(), event, "Tell me about the weather", []knowledgeSource{{SourceID: "note:one"}, {SourceID: "note:one"}})
	if chat.NeedsReasoning(turn) {
		t.Fatal("one document was treated as several sources")
	}
}

func TestReasoningThroughTaskClientDoesNotRecordScratch(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "chat.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	attempts := make(chan []byte, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/chat/tasks/analysis":
			json.NewEncoder(w).Encode(map[string]any{"bindings": []map[string]string{{"provider": "fixture", "model": "large"}}})
		case r.Method == "POST" && r.URL.Path == "/chat/tasks/analysis":
			var body struct {
				Format   map[string]any      `json:"format"`
				Messages []map[string]string `json:"messages"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Format["additionalProperties"] != false || len(body.Messages) < 3 || body.Messages[1]["content"] != chatReasoningBrief() {
				t.Error("task transport lost the schema or brief")
			}
			raw, _ := json.Marshal(map[string]string{"analysis": privateScratch, "reply": "Four."})
			json.NewEncoder(w).Encode(map[string]any{"reply": string(raw), "provider": "fixture", "model": "large"})
		case r.URL.Path == "/chat/task-attempts":
			var attempt map[string]any
			if json.NewDecoder(r.Body).Decode(&attempt) != nil {
				t.Error("invalid attempt")
			}
			diagnostics, _ := json.Marshal(attempt)
			attempts <- diagnostics
			w.Write([]byte(`{"recorded":true}`))
		default:
			t.Errorf("unexpected inference request %s", r.URL.Path)
			http.Error(w, `{"error":"unexpected request"}`, 500)
		}
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	logPath := filepath.Join(t.TempDir(), "chat.log")
	logger.InitWithOptions(false, logPath)
	t.Cleanup(func() { logger.InitWithOptions(false, "") })
	h := NewChatDomainHandler(clients.NewChatProviderClient(socket))
	result, err := h.Handle(t.Context(), chatEvent("2 + 2?"), chatStep)
	if err != nil || result["reply"] != "Four." {
		t.Fatal("structured reply did not reach the channel", err)
	}
	logs, err := os.ReadFile(logPath)
	var diagnostics []byte
	select {
	case diagnostics = <-attempts:
	default:
		t.Fatal("task attempt was not recorded")
	}
	if err != nil || len(diagnostics) == 0 || len(logs) == 0 {
		t.Fatal("missing observable task diagnostics", err)
	}
	if strings.Contains(string(diagnostics), privateScratch) || strings.Contains(string(logs), privateScratch) {
		t.Fatal("task adapter recorded scratch work")
	}
}
