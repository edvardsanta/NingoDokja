package core

import (
	"context"
	"encoding/json"
	"path/filepath"
	"read_books/internal/sourceaccess"
	"strings"
	"testing"
)

type sourceAuthorityCapture struct {
	calls     int
	principal sourceaccess.Principal
	context   map[string]any
}

func (h *sourceAuthorityCapture) Domain() Domain { return DomainKnowledge }
func (h *sourceAuthorityCapture) Handle(ctx context.Context, e Event, _ WorkflowStep) (map[string]any, error) {
	h.calls++
	h.principal, _ = sourceaccess.Current(ctx)
	h.context = e.Context
	return map[string]any{"ok": true}, nil
}
func TestSourceSupervisionAuthenticatesBeforeDispatchAndRedacts(t *testing.T) {
	token := strings.Repeat("s", 40)
	for _, tc := range []struct {
		name, configured, supplied string
		source                     EventSource
		allowed                    bool
	}{
		{"valid", token, token, SourceDesktop, true}, {"wrong", token, "wrong", SourceDesktop, false}, {"missing", token, "", SourceDesktop, false}, {"unset", "", token, SourceDesktop, false}, {"placeholder", "CHANGE_ME_" + token, "CHANGE_ME_" + token, SourceDesktop, false}, {"other interface", token, token, SourceDiscord, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &sourceAuthorityCapture{}
			s := DefaultService(h).WithSourceSupervisionToken(tc.configured)
			result, err := s.ProcessWithResult(context.Background(), Event{Source: tc.source, Type: "knowledge.application.supervision.status", User: EventUser{ID: "claimed-owner"}, Context: map[string]any{"is_owner": true, "source_supervision_token": tc.supplied}})
			if (err == nil) != tc.allowed {
				t.Fatalf("unexpected admission: %v", err)
			}
			if !tc.allowed {
				if h.calls != 0 {
					t.Fatal("unauthorized dispatch")
				}
				return
			}
			if h.principal.Actor != "desktop:operator" || h.context["source_supervision_token"] != nil {
				t.Fatalf("lost authority or retained secret: %+v", h.principal)
			}
			raw, _ := json.Marshal(result)
			if strings.Contains(string(raw), token) {
				t.Fatal("verbose result leaked credential")
			}
		})
	}
}
func TestSourceSupervisionCredentialIsRemovedFromOtherEvents(t *testing.T) {
	h := &sourceAuthorityCapture{}
	s := DefaultService(h)
	_, err := s.ProcessWithResult(context.Background(), Event{Source: SourceDesktop, Type: "knowledge.list", Context: map[string]any{"source_supervision_token": "private-input"}})
	if err != nil || h.context["source_supervision_token"] != nil || h.principal.Actor != "" {
		t.Fatalf("credential escaped or granted authority on another route: %v", err)
	}
}

func TestSourceControlsRemainReachableWhenKnowledgeIsOff(t *testing.T) {
	controls, err := NewControls(filepath.Join(t.TempDir(), "controls.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := controls.SetService("knowledge", false); err != nil {
		t.Fatal(err)
	}
	capture := &sourceAuthorityCapture{}
	token := strings.Repeat("s", 40)
	service := DefaultService(capture).WithControls(controls).WithSourceSupervisionToken(token)
	for _, action := range []string{"status", "control"} {
		if _, err := service.ProcessWithResult(context.Background(), Event{Source: SourceDesktop, Type: "knowledge.application.supervision." + action, Context: map[string]any{"source_supervision_token": token}}); err != nil {
			t.Fatal(err)
		}
	}
	if capture.calls != 2 {
		t.Fatal("status/cancellation hidden by service switch")
	}
	if _, err := service.ProcessWithResult(context.Background(), Event{Source: SourceDesktop, Type: "knowledge.application.supervision.start", Context: map[string]any{"source_supervision_token": token}}); err != nil {
		t.Fatal(err)
	}
	if capture.calls != 2 {
		t.Fatal("disabled source service admitted a new run")
	}
}
