package clients

import (
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"
)

func TestMathClientUsesThePrivateSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inference.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/math/render" {
			t.Error("wrong rendering route")
		}
		var request map[string]string
		json.NewDecoder(r.Body).Decode(&request)
		if request["expression"] != `\frac{x}{2}` {
			t.Error("formula changed in transit")
		}
		json.NewEncoder(w).Encode(map[string]any{"data_b64": []byte("image bytes"), "content_type": "image/png"})
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	data, err := NewChatProviderClient(path).RenderMath(t.Context(), `\frac{x}{2}`)
	if err != nil || string(data) != "image bytes" {
		t.Fatalf("render result: data=%q err=%v", data, err)
	}
}
