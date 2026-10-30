package ingress

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/gofiber/fiber/v2"
	"net/http/httptest"
	communications "read_books/dokja_domain/dokja_communications"
	"read_books/internal/core"
	"read_books/internal/handlers"
	"strings"
	"testing"
)

type bridgeDocumentReader struct{ calls int }

func (r *bridgeDocumentReader) Extract(context.Context, string, string) (map[string]any, error) {
	r.calls++
	return map[string]any{"documents": []any{map[string]any{"title": "Memo", "text": "Evidence from the supplied document.", "source_ref": "", "key": ""}}}, nil
}
func TestWhatsAppDocumentIngressAuthorizesBeforeRequestingOrReadingBytes(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		name := "denied"
		if allowed {
			name = "allowed"
		}
		t.Run(name, func(t *testing.T) {
			reader := &bridgeDocumentReader{}
			generator := &identityGenerator{}
			gate := handlers.NewCommunicationsDomainHandler(nil, "").WithInboundPolicy(communications.InboundPolicy{WhatsAppSelfChat: allowed})
			chat := handlers.NewChatDomainHandler(generator).WithIncomingContent(handlers.NewIncomingContentWorkflow(reader, nil, func() bool { return true }))
			service := core.DefaultService(gate, handlers.NewModerationDomainHandler(nil), chat)
			app := fiber.New()
			app.Post("/orchestrator", (&HTTPBridgeIngress{service: service, token: "fixture-credential"}).handle)
			send := func(encoded string) BridgeResponse {
				payload := map[string]any{"content": "", "userId": "owner", "channelId": "self", "messageId": "m", "chatKind": "direct", "isSelfChat": true, "mentionsBot": false, "senderNumbers": []string{}, "entryKind": "message", "locale": "en", "document": map[string]any{"filename": "memo.pdf", "content_type": "application/pdf", "size": 3, "data_b64": encoded}}
				body, _ := json.Marshal(map[string]any{"event_type": "message.created", "interface": "whatsapp", "payload": payload})
				req := httptest.NewRequest("POST", "/orchestrator", strings.NewReader(string(body)))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer fixture-credential")
				res, err := app.Test(req)
				if err != nil {
					t.Fatal(err)
				}
				defer res.Body.Close()
				var answer BridgeResponse
				if err := json.NewDecoder(res.Body).Decode(&answer); err != nil {
					t.Fatal(err)
				}
				if res.StatusCode != 200 {
					t.Fatalf("bridge failed: %d %+v", res.StatusCode, answer)
				}
				return answer
			}
			metadata := send("")
			if metadata.DocumentRequired != allowed || reader.calls != 0 || generator.calls != 0 {
				t.Fatal("metadata bypassed authorization or caused extraction")
			}
			result := send(base64.StdEncoding.EncodeToString([]byte("pdf")))
			if allowed {
				if result.Reply != "model reply" || reader.calls != 1 || generator.calls != 1 {
					t.Fatalf("summary failed: %+v", result)
				}
				if !strings.Contains(generator.messages[0]["content"], "English") || !strings.Contains(generator.messages[1]["content"], "supplied document") {
					t.Fatal("summary context missing")
				}
			} else if result.Reply != "" || reader.calls != 0 || generator.calls != 0 {
				t.Fatal("denied bytes caused processing")
			}
		})
	}
}

type bridgePostReader struct{ calls int }

func (r *bridgePostReader) Dispatch(_ context.Context, event string, payload map[string]any) (map[string]any, error) {
	r.calls++
	if event == "feeds.status" {
		return map[string]any{"providers": []any{map[string]any{"id": "reader", "enabled": true, "provides": []any{"social.resolve.v1"}}}}, nil
	}
	if payload["action"] == "read" {
		return map[string]any{"handled": true, "post": map[string]any{"url": "https://example.com/post", "author": "Example Author", "caption": "Original caption", "media": []any{map[string]any{"kind": "video", "url": "http://localhost/media/opaque.mp4"}}}}, nil
	}
	return map[string]any{"content_type": "video/mp4", "data_b64": base64.StdEncoding.EncodeToString([]byte("video"))}, nil
}
func TestWhatsAppPublicationRoutesThroughDigestAndReturnsMediaAfterAuthorization(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		name := "denied"
		if allowed {
			name = "allowed"
		}
		t.Run(name, func(t *testing.T) {
			posts := &bridgePostReader{}
			generator := &identityGenerator{}
			gate := handlers.NewCommunicationsDomainHandler(nil, "").WithInboundPolicy(communications.InboundPolicy{WhatsAppSelfChat: allowed})
			chat := handlers.NewChatDomainHandler(generator).WithIncomingContent(handlers.NewIncomingContentWorkflow(nil, posts, func() bool { return true }))
			service := core.DefaultService(gate, handlers.NewModerationDomainHandler(nil), chat)
			app := fiber.New()
			app.Post("/orchestrator", (&HTTPBridgeIngress{service: service, token: "fixture-credential"}).handle)
			body := `{"event_type":"message.created","interface":"whatsapp","payload":{"content":"https://example.com/post","locale":"pt-BR","userId":"owner","channelId":"self","chatKind":"direct","isSelfChat":true,"mentionsBot":false,"senderNumbers":[],"entryKind":"message"}}`
			req := httptest.NewRequest("POST", "/orchestrator", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer fixture-credential")
			response, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			var reply BridgeResponse
			if err := json.NewDecoder(response.Body).Decode(&reply); err != nil {
				t.Fatal(err)
			}
			if allowed {
				if len(reply.Attachments) != 1 || reply.Attachments[0].ContentType != "video/mp4" || !strings.Contains(reply.Reply, "Original caption") || posts.calls != 3 {
					t.Fatalf("publication delivery failed: %+v", reply)
				}
			} else if reply.Reply != "" || len(reply.Attachments) != 0 || posts.calls != 0 {
				t.Fatal("denied input read or delivered media")
			}
			if generator.calls != 0 {
				t.Fatal("post delivery unexpectedly invoked a model")
			}
		})
	}
}
