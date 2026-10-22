package book

import (
	"context"
	"testing"
)

type processingStub struct {
	capability, action string
	input              map[string]any
}

func (p *processingStub) Invoke(_ context.Context, capability, action string, input map[string]any) (map[string]any, error) {
	p.capability, p.action, p.input = capability, action, input
	return map[string]any{"status": "ok"}, nil
}
func TestBookFacadeUsesDocumentPluginAndPreservesInput(t *testing.T) {
	p := &processingStub{}
	d := New(NewReadingService(p))
	for _, tc := range []struct{ action, pluginAction string }{{ActionSummarizeBook, "summarize"}, {ActionClassifyBookResource, "classify"}} {
		_, err := d.Handle(context.Background(), Request{Action: tc.action, Event: Event{ID: "event", Payload: map[string]any{"content": "the text"}}})
		if err != nil || p.capability != "document.process.v1" || p.action != tc.pluginAction || p.input["content"] != "the text" || p.input["event_id"] != "event" {
			t.Fatal(p, err)
		}
	}
}
