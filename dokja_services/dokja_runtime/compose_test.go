package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	zmq "github.com/pebbe/zmq4"
	domain "read_books/dokja_domain/dokja_runtime"
)

func TestComposeTargetsOneServiceAndNeverRemovesData(t *testing.T) {
	var calls [][]string
	c := &Compose{Run: func(_ context.Context, args ...string) ([]byte, error) { calls = append(calls, args); return nil, nil }}
	m, _ := domain.Lookup("memory")
	if err := c.Start(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"up", "-d", "--no-deps", "--no-recreate", "--build", "dokja-memory"}, {"stop", "--timeout", "20", "dokja-memory"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatal(calls)
	}
	if err := c.Start(context.Background(), domain.Module{Name: "memory", Service: "other"}); err == nil {
		t.Fatal("unknown service must be rejected")
	}
	if len(calls) != 2 {
		t.Fatal("invalid service reached engine")
	}
}
func TestRecreateReplacesOneContainerWithoutRemovingData(t *testing.T) {
	var calls [][]string
	c := &Compose{Run: func(_ context.Context, args ...string) ([]byte, error) { calls = append(calls, args); return nil, nil }}
	m, _ := domain.Lookup("discord")
	if err := c.Recreate(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, [][]string{{"up", "-d", "--no-deps", "--force-recreate", "--build", "dokja-discord"}}) {
		t.Fatal(calls)
	}
	if c.Recreate(context.Background(), domain.Module{Name: "discord", Service: "other"}) == nil || len(calls) != 1 {
		t.Fatal("unknown service must be rejected before the engine")
	}
}

func TestStatusHandlesDockerAndPodmanWithoutExposingPrivateFields(t *testing.T) {
	for _, podman := range []bool{false, true} {
		var calls [][]string
		c := &Compose{Run: func(_ context.Context, args ...string) ([]byte, error) {
			calls = append(calls, args)
			if podman && len(args) == 3 {
				return nil, errors.New("--all unsupported")
			}
			return []byte("aaaaaaaaaaaa\nbbbbbbbbbbbb"), nil
		}, Inspect: func(context.Context, ...string) ([]byte, error) {
			return []byte(`[{
"Id":"aaaaaaaaaaaa","Config":{"Labels":{"com.docker.compose.service":"dokja-memory"},"Env":["private=drop"]},"State":{"Status":"running","Running":true}},
{"Id":"bbbbbbbbbbbb","Config":{"Labels":{"io.podman.compose.service":"dokja-embedding-init"}},"State":{"Status":"exited","ExitCode":0}}]`), nil
		}}
		result, err := c.Status(context.Background())
		if err != nil || !result["dokja-memory"].Running || !result["dokja-embedding-init"].Completed {
			t.Fatal(result, err)
		}
		expected := 1
		if podman {
			expected = 2
		}
		if len(calls) != expected {
			t.Fatal(calls)
		}
	}
}
func TestInvalidContainerIDsNeverReachInspect(t *testing.T) {
	c := &Compose{Run: func(context.Context, ...string) ([]byte, error) { return []byte("--other-option"), nil }, Inspect: func(context.Context, ...string) ([]byte, error) {
		t.Fatal("invalid ID reached engine")
		return nil, nil
	}}
	if _, err := c.Status(context.Background()); err == nil {
		t.Fatal("invalid ID accepted")
	}
}
func TestInitializerMustCompleteAndReadinessTimesOut(t *testing.T) {
	m, _ := domain.Lookup("embedding_init")
	state := "running"
	exit := 0
	c := &Compose{Poll: time.Millisecond, Run: func(context.Context, ...string) ([]byte, error) { return []byte("aaaaaaaaaaaa"), nil }, Inspect: func(context.Context, ...string) ([]byte, error) {
		return []byte(fmt.Sprintf(`[{"Config":{"Labels":{"com.docker.compose.service":"dokja-embedding-init"}},"State":{"Status":%q,"Running":%t,"ExitCode":%d}}]`, state, state == "running", exit)), nil
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := c.Ready(ctx, m); err == nil {
		t.Fatal("running initializer must not count as completed")
	}
	state = "exited"
	if err := c.Ready(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	exit = 1
	if err := c.Ready(context.Background(), m); err == nil {
		t.Fatal("failed initializer must not count as ready")
	}
}
func TestProbeRequiresApplicationAcknowledgement(t *testing.T) {
	socket, err := zmq.NewSocket(zmq.REP)
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	socket.SetLinger(0)
	socket.SetRcvtimeo(time.Second)
	if err := socket.Bind("tcp://127.0.0.1:*"); err != nil {
		t.Fatal(err)
	}
	endpoint, _ := socket.GetLastEndpoint()
	u, _ := url.Parse(endpoint)
	port, _ := strconv.Atoi(u.Port())
	seen := make(chan map[string]any, 1)
	go func() {
		raw, _ := socket.RecvBytes(0)
		var event map[string]any
		json.Unmarshal(raw, &event)
		seen <- event
		socket.SendBytes([]byte(`{"status":"ok","result":{}}`), 0)
	}()
	if err := probe(context.Background(), domain.Module{Name: "memory", Port: port}); err != nil {
		t.Fatal(err)
	}
	if e := <-seen; e["type"] != "memory.status" || e["source"] != "desktop" {
		t.Fatal(e)
	}
}
func TestHTTPProbeDoesNotFollowRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.com/", http.StatusFound)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	if err := probe(context.Background(), domain.Module{Name: "voice", Port: port}); err == nil {
		t.Fatal("redirect must not count as ready")
	}
}
func TestCommandOutputIsBounded(t *testing.T) {
	b := &boundedBuffer{}
	n, err := fmt.Fprint(b, strings.Repeat("x", 3*1024*1024))
	if err != nil || n != 3*1024*1024 || b.Len() != 2*1024*1024 || !b.overflow {
		t.Fatal(n, err, b.Len())
	}
}

func TestConfiguredLocalSocketIsPreservedForPodman(t *testing.T) {
	env := engineEnvironment([]string{"PATH=/bin", "DOCKER_HOST=unix:///run/example.sock"})
	if !reflect.DeepEqual(env, []string{"PATH=/bin", "DOCKER_HOST=unix:///run/example.sock", "CONTAINER_HOST=unix:///run/example.sock"}) {
		t.Fatal(env)
	}
	original := []string{"DOCKER_HOST=unix:///run/example.sock", "CONTAINER_HOST=unix:///run/explicit.sock"}
	if !reflect.DeepEqual(engineEnvironment(original), original) {
		t.Fatal("explicit engine target changed")
	}
	remote := []string{"DOCKER_HOST=tcp://host.example:1234"}
	if !reflect.DeepEqual(engineEnvironment(remote), remote) {
		t.Fatal("remote socket must not be guessed")
	}
}
func TestWhatsAppUsesItsPrivateStatusListenerForReadiness(t *testing.T) {
	var calls [][]string
	c := &Compose{Wait: time.Second, Run: func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, args)
		if args[0] == "ps" {
			return []byte("aaaaaaaaaaaa"), nil
		}
		return nil, nil
	}, Inspect: func(context.Context, ...string) ([]byte, error) {
		return []byte(`[{"Config":{"Labels":{"com.docker.compose.service":"dokja-whatsapp"}},"State":{"Status":"running","Running":true}}]`), nil
	}}
	m, _ := domain.Lookup("whatsapp")
	if err := c.Ready(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	want := []string{"exec", "-T", "dokja-whatsapp", "node", "--input-type=module", "-e", whatsappHealth}
	if !reflect.DeepEqual(calls[len(calls)-1], want) {
		t.Fatal(calls)
	}
}

func TestCommunicationsReadsItsUnpublishedHealthInsideTheContainer(t *testing.T) {
	var calls [][]string
	c := &Compose{Wait: time.Second, Run: func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, args)
		if args[0] == "ps" {
			return []byte("aaaaaaaaaaaa"), nil
		}
		return nil, nil
	}, Inspect: func(context.Context, ...string) ([]byte, error) {
		return []byte(`[{"Config":{"Labels":{"com.docker.compose.service":"dokja-communications"}},"State":{"Status":"running","Running":true}}]`), nil
	}}
	m, _ := domain.Lookup("communications")
	if err := c.Ready(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	want := []string{"exec", "-T", "dokja-communications", "python", "-c", communicationsHealth}
	if !reflect.DeepEqual(calls[len(calls)-1], want) {
		t.Fatal(calls)
	}
}

func TestProfileGatedModulesAreVisibleToEveryComposeCommand(t *testing.T) {
	got := composeArgs("/stack", []string{"up", "-d", "dokja-communications"})
	want := []string{"compose", "-f", "/stack/docker-compose.lite.yml", "--profile", "communications", "--profile", "whatsapp", "up", "-d", "dokja-communications"}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}

func TestDiscordAndSchedulerReadiness(t *testing.T) {
	for _, tc := range []struct {
		name, service string
		want          []string
	}{
		{"discord", "dokja-discord", []string{"exec", "-T", "dokja-discord", "node", "--input-type=module", "-e", discordHealth}},
		{"scheduler", "dokja-scheduler", []string{"ps", "--all", "--quiet"}},
	} {
		var calls [][]string
		c := &Compose{Wait: time.Second, Run: func(_ context.Context, args ...string) ([]byte, error) {
			calls = append(calls, args)
			if args[0] == "ps" {
				return []byte("aaaaaaaaaaaa"), nil
			}
			return nil, nil
		}, Inspect: func(context.Context, ...string) ([]byte, error) {
			return []byte(`[{"Config":{"Labels":{"com.docker.compose.service":"` + tc.service + `"}},"State":{"Status":"running","Running":true}}]`), nil
		}}
		m, _ := domain.Lookup(tc.name)
		if err := c.Ready(context.Background(), m); err != nil {
			t.Fatal(tc.name, err)
		}
		if !reflect.DeepEqual(calls[len(calls)-1], tc.want) {
			t.Fatal(tc.name, calls)
		}
	}
}

func TestKnownRemoteEngineTargetsAreRejected(t *testing.T) {
	for _, env := range [][]string{{"DOCKER_HOST=tcp://host.example:1234"}, {"CONTAINER_HOST=ssh://host.example"}, {"DOCKER_CONTEXT=production"}} {
		if localEngine(env) == nil {
			t.Fatal("remote/custom target accepted")
		}
	}
	if err := localEngine([]string{"DOCKER_HOST=unix:///run/example.sock"}); err != nil {
		t.Fatal(err)
	}
}

func TestLogsTailOnlySelectedModuleWithoutUnboundedOutput(t *testing.T) {
	var calls [][]string
	c := &Compose{Run: func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, args)
		return []byte("2026-10-21T12:00:00Z api ready\x1b[31mred\x1b[0m\x07"), nil
	}, output: map[string]*tailBuffer{}}
	for _, name := range []string{"orchestrator", "embeddings", "embedding_init", "memory"} {
		c.output[nameService(name)] = &tailBuffer{}
		c.output[nameService(name)].Write([]byte("build output for " + name))
	}
	module, _ := domain.Lookup("memory")
	logs, err := c.Logs(context.Background(), []domain.Module{module})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || !reflect.DeepEqual(calls[0], []string{"logs", "--no-color", "--timestamps", "--tail", "40", "dokja-memory"}) {
		t.Fatal(calls)
	}
	for _, want := range []string{"build output for memory", "api readyred"} {
		if !strings.Contains(logs, want) {
			t.Fatalf("missing %q in logs %q", want, logs)
		}
	}
	if strings.Contains(logs, "\x1b") || strings.ContainsRune(logs, '\x07') || len(logs) > 24*1024 {
		t.Fatal("log output must be clean and bounded")
	}
}
func nameService(name string) string {
	module, _ := domain.Lookup(name)
	return module.Service
}
