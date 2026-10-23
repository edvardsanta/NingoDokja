package cli

import (
	"testing"
)

func TestChatBindingClientUsesRevisionAndValidatesReceipt(t *testing.T) {
	for _, mode := range []string{"ok", "conflict", "wrong_model", "wrong_revision"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			c := fakeDaemon(t, "/checkout", func(event map[string]any) map[string]any {
				calls++
				reply := map[string]any{"version": 1, "project_id": RuntimeProjectID("/checkout"), "status": "ok"}
				rev := float64(4)
				model := "fixture/model"
				if event["type"] == "runtime.chat.configure" {
					p := event["payload"].(map[string]any)
					if p["configuration_revision"] != float64(4) || p["provider"] != "codex" || p["model"] != model {
						t.Error(p)
					}
					rev++
					if mode == "conflict" {
						reply["status"] = "error"
						reply["error"] = "reload before saving"
						return reply
					}
					if mode == "wrong_model" {
						model = "other"
					}
					if mode == "wrong_revision" {
						rev++
					}
				}
				reply["result"] = map[string]any{"domain": "runtime", "result": map[string]any{"configuration_revision": rev, "provider": "codex", "model": model}}
				return reply
			})
			binding, err := c.ChatBinding(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.ConfigureChat(t.Context(), binding)
			if (err == nil) != (mode == "ok") || calls != 2 {
				t.Fatal(err, calls)
			}
		})
	}
}
func TestChatBindingClientRejectsOldControllerAndInvalidRevision(t *testing.T) {
	calls := 0
	c := fakeDaemon(t, "/checkout", func(map[string]any) map[string]any {
		calls++
		return map[string]any{"version": 1, "project_id": RuntimeProjectID("/checkout"), "status": "ok", "result": map[string]any{"domain": "runtime", "result": map[string]any{}}}
	})
	if _, err := c.ChatBinding(t.Context()); err == nil {
		t.Fatal("accepted old controller")
	}
	if _, err := c.ConfigureChat(t.Context(), ChatBinding{Revision: 0, Provider: "codex", Model: "fixture"}); err == nil || calls != 1 {
		t.Fatal(err, calls)
	}
}
