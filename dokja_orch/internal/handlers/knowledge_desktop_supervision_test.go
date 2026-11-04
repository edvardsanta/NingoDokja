package handlers

import (
	"context"
	"encoding/json"
	application "read_books/dokja_domain/dokja_memory/application"
	"read_books/internal/core"
	"strings"
	"testing"
	"time"
)

func desktopSourceService(t *testing.T) (*core.Service, *DesktopSourceApplications) {
	t.Helper()
	journal := newSourceJournalStore()
	journal.settled = make(chan application.Episode, 128)
	handler := NewKnowledgeDomainHandler(&steeringSources{})
	life, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	host := NewDesktopSourceApplications(life, handler, journal, func() bool { return true })
	handler.WithSourceApplications(host)
	return core.DefaultService(handler).WithSourceSupervisionToken(strings.Repeat("s", 40)), host
}
func sourceDesktopRequest(t *testing.T, service *core.Service, kind string, payload map[string]any) (map[string]any, error) {
	t.Helper()
	event := core.Event{Source: core.SourceDesktop, Type: "knowledge.application.supervision." + kind, Payload: payload, Context: map[string]any{"source_supervision_token": strings.Repeat("s", 40)}}
	// Match the actual transport's JSON map representation.
	raw, _ := json.Marshal(event)
	var decoded core.Event
	_ = json.Unmarshal(raw, &decoded)
	result, err := service.ProcessWithResult(context.Background(), decoded)
	if err != nil {
		return nil, err
	}
	return result.Result["knowledge"].(map[string]any), nil
}
func TestDesktopSourceHostStartReplayAndProcessFence(t *testing.T) {
	service, host := desktopSourceService(t)
	caps, err := sourceDesktopRequest(t, service, "status", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(mustSteeringRequest(t))
	var request map[string]any
	_ = json.Unmarshal(raw, &request)
	payload := map[string]any{"operation_ref": "start:one", "host_ref": caps["host_ref"], "application": request}
	first, err := sourceDesktopRequest(t, service, "start", payload)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := sourceDesktopRequest(t, service, "start", payload)
	if err != nil || first["application_ref"] != replay["application_ref"] || len(host.applications) != 1 {
		t.Fatalf("start replay duplicated native application: %v", err)
	}
	request["goal"] = "changed command"
	if _, err := sourceDesktopRequest(t, service, "start", payload); err == nil {
		t.Fatal("changed start operation did not conflict")
	}
	payload["host_ref"] = "previous-process"
	if _, err := sourceDesktopRequest(t, service, "start", payload); err == nil {
		t.Fatal("old process command resurrected after restart")
	}
	caps, err = sourceDesktopRequest(t, service, "status", map[string]any{})
	if err != nil || len(caps["applications"].([]any)) != 1 {
		t.Fatal("lost start cannot be found by its operation reference")
	}
}
func TestDesktopSourceHostRejectsForeignAndUnknownControls(t *testing.T) {
	service, host := desktopSourceService(t)
	host.applications["private:application"] = desktopApplication{owner: "foreign:operator", created: time.Now()}
	for _, kind := range []string{"status", "control"} {
		if _, err := sourceDesktopRequest(t, service, kind, map[string]any{"application_ref": "private:application"}); err == nil {
			t.Fatal("foreign application exposed")
		}
	}
	if _, err := host.Handle(context.Background(), core.Event{Type: "knowledge.application.supervision.status"}); err == nil {
		t.Fatal("direct host call trusted event claims without native authority")
	}
}
func TestDesktopSourceHostDistinctColonKeysAndQuota(t *testing.T) {
	service, host := desktopSourceService(t)
	caps, err := sourceDesktopRequest(t, service, "status", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(mustSteeringRequest(t))
	var request map[string]any
	_ = json.Unmarshal(raw, &request)
	for i, op := range []string{"start", "start:second"} {
		if _, err := sourceDesktopRequest(t, service, "start", map[string]any{"operation_ref": op, "host_ref": caps["host_ref"], "application": request}); err != nil {
			t.Fatalf("distinct key %d conflicted: %v", i, err)
		}
	}
	host.mu.Lock()
	for len(host.applications) < 16 {
		host.applications["reserved:"+string(rune('a'+len(host.applications)))] = desktopApplication{owner: "desktop:operator"}
	}
	host.mu.Unlock()
	if _, err := sourceDesktopRequest(t, service, "start", map[string]any{"operation_ref": "beyond:quota", "host_ref": caps["host_ref"], "application": request}); err == nil {
		t.Fatal("unbounded application admission")
	}
}
