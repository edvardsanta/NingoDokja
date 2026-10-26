package clients

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestAudioSourcesAreTheEnabledProvidersOfTheCapabilityInIdOrder(t *testing.T) {
	status := `{"status":"ok","result":{"providers":[
		{"id":"30-pages","enabled":true,"provides":["audio.resolve.v1"]},
		{"id":"10-catalogue","enabled":true,"provides":["audio.resolve.v1"]},
		{"id":"20-off","enabled":false,"provides":["audio.resolve.v1"]},
		{"id":"ocr","enabled":true,"provides":["media.perceive.v1"]}
	]}}`
	sources := NewFeedsAudioSources(feedsClient(&fakeFeedsRequester{response: status}), "audio.resolve.v1")
	ids, err := sources.AudioProviders(context.Background())
	if err != nil || !reflect.DeepEqual(ids, []string{"10-catalogue", "30-pages"}) {
		t.Fatal(ids, err)
	}
}

func TestAudioSourcesNameThePluginTheyInvoke(t *testing.T) {
	requester := &fakeFeedsRequester{response: `{"status":"ok","result":{"handled":true,"tracks":[]}}`}
	sources := NewFeedsAudioSources(feedsClient(requester), "audio.resolve.v1")
	result, err := sources.InvokeAudio(context.Background(), "20-music", "search", map[string]any{"query": "monalisa"})
	if err != nil || result["handled"] != true {
		t.Fatal(result, err)
	}
	var sent struct {
		Type    string         `json:"type"`
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal([]byte(requester.request), &sent); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"capability": "audio.resolve.v1", "action": "search", "plugin": "20-music", "input": map[string]any{"query": "monalisa"}}
	if sent.Type != "feeds.invoke" || !reflect.DeepEqual(sent.Payload, want) {
		t.Fatal(requester.request)
	}
}
