package nvidia

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

type fakeNVIDIA struct {
	requests                                         int
	mu                                               sync.Mutex
	enabled, unavailable, rejectCapture, hideCapture bool
	initializations, statusCalls, captureCalls       int
	badArguments                                     bool
}

// Run the real SDK through HTTP requests without depending on the host's
// loopback networking. Physical Windows networking is checked with --doctor.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func (f *fakeNVIDIA) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var request struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if len(request.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	if f.unavailable {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}
	var result any
	switch request.Method {
	case "initialize":
		f.initializations++
		result = map[string]any{"protocolVersion": "2025-11-25", "serverInfo": map[string]any{"name": "fake-nvidia", "version": "1.0.0"}, "capabilities": map[string]any{"tools": map[string]any{}}}
	case "tools/list":
		tools := []any{map[string]any{"name": statusTool, "inputSchema": map[string]any{"type": "object"}}}
		if !f.hideCapture {
			tools = append(tools, map[string]any{"name": captureTool, "inputSchema": captureSchema()})
		}
		result = map[string]any{"tools": tools}
	case "tools/call":
		switch request.Params.Name {
		case statusTool:
			f.statusCalls++
			if len(request.Params.Arguments) != 0 {
				f.badArguments = true
			}
			result = map[string]any{"content": []any{}, "structuredContent": map[string]any{"instantReplayEnabled": f.enabled}}
		case captureTool:
			f.captureCalls++
			args := request.Params.Arguments
			if len(args) != 2 || args["action"] != "toggle_instant_replay" || args["enable"] != true {
				f.badArguments = true
			}
			if f.rejectCapture {
				result = map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": "overlay_command_failed"}}}
			} else {
				f.enabled = true
				result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "Instant Replay enabled"}}}
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	response, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	w.Header().Set("Content-Length", strconv.Itoa(len(response)))
	_, _ = w.Write(response)
}

func captureSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"action": map[string]any{"type": "string", "enum": []string{"toggle_instant_replay"}},
		"enable": map[string]any{"type": "boolean"},
	}, "required": []string{"action"}}
}

func testClient(t *testing.T, fake *fakeNVIDIA) *Client {
	t.Helper()
	client := NewClient("test", func() (string, error) { return DefaultEndpoint, nil })
	client.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		defer request.Body.Close()
		recorder := httptest.NewRecorder()
		fake.ServeHTTP(recorder, request)
		return recorder.Result(), nil
	})
	t.Cleanup(client.Close)
	return client
}

func TestClientReusesSessionAndSendsExplicitEnable(t *testing.T) {
	fake := &fakeNVIDIA{}
	client := testClient(t, fake)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	enabled, err := client.IsReplayEnabled(ctx)
	if err != nil || enabled {
		t.Fatalf("enabled=%v err=%v", enabled, err)
	}
	if err := client.EnableReplay(ctx); err != nil {
		t.Fatal(err)
	}
	enabled, err = client.IsReplayEnabled(ctx)
	if err != nil || !enabled {
		t.Fatalf("enabled=%v err=%v", enabled, err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.initializations != 1 || fake.captureCalls != 1 || fake.statusCalls != 2 || fake.badArguments {
		t.Fatalf("initializations=%d captures=%d reads=%d badArgs=%v", fake.initializations, fake.captureCalls, fake.statusCalls, fake.badArguments)
	}
}

func TestClientReconnectsOnlyOnNextRequest(t *testing.T) {
	fake := &fakeNVIDIA{enabled: true}
	client := testClient(t, fake)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := client.IsReplayEnabled(ctx); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.unavailable = true
	fake.mu.Unlock()
	if _, err := client.IsReplayEnabled(ctx); err == nil {
		t.Fatal("expected HTTP failure")
	}
	fake.mu.Lock()
	if fake.initializations != 1 {
		t.Fatal("retried initialization within failed call")
	}
	if fake.requests != 4 {
		t.Fatalf("retried failed HTTP request: %d requests", fake.requests)
	}
	fake.unavailable = false
	fake.mu.Unlock()
	if _, err := client.IsReplayEnabled(ctx); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.initializations != 2 {
		t.Fatalf("initializations=%d", fake.initializations)
	}
}

func TestClientRejectsUnavailableOrFailedCapture(t *testing.T) {
	for _, hide := range []bool{false, true} {
		t.Run(map[bool]string{false: "backend rejected", true: "access restricted"}[hide], func(t *testing.T) {
			fake := &fakeNVIDIA{hideCapture: hide, rejectCapture: true}
			client := testClient(t, fake)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if _, err := client.IsReplayEnabled(ctx); err != nil {
				t.Fatal(err)
			}
			if err := client.EnableReplay(ctx); err == nil {
				t.Fatal("expected capture failure")
			}
			fake.mu.Lock()
			defer fake.mu.Unlock()
			if hide && fake.captureCalls != 0 {
				t.Fatal("called hidden tool")
			}
		})
	}
}

func TestReplayStateMustBeExplicitBoolean(t *testing.T) {
	for _, test := range []struct {
		name    string
		content any
		want    bool
		wantErr bool
	}{
		{"on", map[string]any{"instantReplayEnabled": true}, true, false},
		{"off", map[string]any{"instantReplayEnabled": false}, false, false},
		{"missing", map[string]any{}, false, true},
		{"no structured content", nil, false, true},
		{"null", map[string]any{"instantReplayEnabled": nil}, false, true},
		{"string", map[string]any{"instantReplayEnabled": "false"}, false, true},
		{"number", map[string]any{"instantReplayEnabled": 0}, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseReplayEnabled(test.content)
			if got != test.want || (err != nil) != test.wantErr {
				t.Fatalf("got=%v err=%v", got, err)
			}
		})
	}
}

func TestClientHonorsRequestDeadline(t *testing.T) {
	client := NewClient("test", func() (string, error) { return DefaultEndpoint, nil })
	client.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		defer request.Body.Close()
		_, _ = io.Copy(io.Discard, request.Body)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := client.IsReplayEnabled(ctx); err == nil {
		t.Fatal("expected timeout")
	}
	if time.Since(started) > time.Second {
		t.Fatal("deadline did not bound connection")
	}
}

func TestValidateEndpoint(t *testing.T) {
	for _, endpoint := range []string{"http://127.0.0.1:13508/mcp", "http://[::1]:13509/mcp/"} {
		if err := ValidateEndpoint(endpoint); err != nil {
			t.Fatal(err)
		}
	}
	for _, endpoint := range []string{"http://example.com:13508/mcp", "http://127.0.0.1:13508/other", "https://127.0.0.1:13508/mcp", "http://user:pass@127.0.0.1:13508/mcp", "http://127.0.0.1:13508/mcp?token=x", "http://127.0.0.1/mcp"} {
		if err := ValidateEndpoint(endpoint); err == nil {
			t.Errorf("accepted %s", endpoint)
		}
	}
}
