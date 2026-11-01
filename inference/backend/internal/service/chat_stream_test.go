package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commoninference "github.com/addp/common/inference"
	"github.com/addp/inference/internal/models"
)

func streamFixture(t *testing.T, endpoint string) (*Runtime, commoninference.ChatRequest) {
	t.Helper()
	ctx := context.Background()
	control := NewControlPlane(newTestStore(t), testEncryptionKey)
	actor := Actor{ContextType: models.ScopeTenant, TenantID: 7, PrincipalID: 12}
	p, err := control.CreateProvider(ctx, actor, ProviderInput{Name: "stream", ScopeType: models.ScopeTenant, AdapterType: AdapterOpenAICompatible, Endpoint: endpoint})
	if err != nil {
		t.Fatal(err)
	}
	d, err := control.CreateDeployment(ctx, actor, DeploymentInput{ProviderConnectionID: p.ID, Name: "stream", UpstreamModel: "model", Operations: []string{"chat"}, Modalities: []string{"text"}})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := control.CreateProfile(ctx, actor, ProfileInput{Name: "stream", Code: "stream", ScopeType: models.ScopeTenant, ModelDeploymentID: d.ID})
	if err != nil {
		t.Fatal(err)
	}
	return NewRuntime(control.store, testEncryptionKey), commoninference.ChatRequest{SchemaVersion: commoninference.SchemaVersion, TenantID: 7, ModelProfileID: profile.ID, Stream: true, Messages: []commoninference.Message{{Role: "user", Content: "hello"}}}
}

func TestChatStreamDeliversBeforeUpstreamFinishes(t *testing.T) {
	release, observed := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request["stream"] != true || request["tools"] != nil {
			t.Error("invalid text-only upstream request")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"你🙂\",\"reasoning_content\":\"PRIVATE\"}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"好\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	runtime, req := streamFixture(t, server.URL)
	stream, err := runtime.OpenChatStream(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var events []commoninference.ChatStreamEvent
	done := make(chan error, 1)
	go func() {
		done <- stream.Consume(context.Background(), func(event commoninference.ChatStreamEvent) error {
			events = append(events, event)
			if event.Type == "text_delta" && event.Delta == "你🙂" {
				close(observed)
			}
			return nil
		})
	}()
	select {
	case <-observed:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("stream buffered until completion")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(events)
	if strings.Contains(string(encoded), "PRIVATE") || events[len(events)-1].Type != "completed" {
		t.Fatal("unsafe or incomplete stream")
	}
	for i, event := range events {
		if event.Sequence != i+1 {
			t.Fatal("sequence gap")
		}
	}
}

func TestChatStreamRejectsIncompleteMalformedAndToolResponses(t *testing.T) {
	for _, payload := range []string{
		"data: [DONE]\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"cut\"},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n",
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"id\":\"private\"}]}}]}\n\n",
		"data: not-json\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"\xff\"}}]}\n\n",
		strings.Repeat("data: x\n", 70000) + "\n",
	} {
		t.Run(fmt.Sprintf("size-%d", len(payload)), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, payload)
			}))
			defer server.Close()
			runtime, req := streamFixture(t, server.URL)
			stream, err := runtime.OpenChatStream(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			err = stream.Consume(context.Background(), func(event commoninference.ChatStreamEvent) error {
				if event.Type == "completed" {
					t.Error("incomplete response completed")
				}
				return nil
			})
			if !errors.Is(err, ErrUpstreamFailed) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestChatStreamCancellationAndReadDeadlineCloseUpstream(t *testing.T) {
	for _, cancel := range []bool{true, false} {
		t.Run(fmt.Sprintf("cancel=%v", cancel), func(t *testing.T) {
			closed := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\",\"tool_calls\":null}}]}\n\n")
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				close(closed)
			}))
			defer server.Close()
			runtime, req := streamFixture(t, server.URL)
			runtime.client.Timeout = 100 * time.Millisecond
			ctx, stop := context.WithCancel(context.Background())
			defer stop()
			stream, err := runtime.OpenChatStream(ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			text := ""
			err = stream.Consume(ctx, func(event commoninference.ChatStreamEvent) error {
				if event.Type == "text_delta" {
					text += event.Delta
					if cancel {
						stop()
					}
				}
				if event.Type == "completed" {
					t.Error("interrupted stream completed")
				}
				return nil
			})
			want := ErrTimeout
			if cancel {
				want = context.Canceled
			}
			if !errors.Is(err, want) || text != "partial" {
				t.Fatalf("text=%q error=%v", text, err)
			}
			select {
			case <-closed:
			case <-time.After(2 * time.Second):
				t.Fatal("upstream not released")
			}
		})
	}
}

func TestChatStreamRejectsStructuredModeAndWrongTenantBeforeRequest(t *testing.T) {
	runtime, req := streamFixture(t, "http://127.0.0.1:1")
	req.Tools = []commoninference.ToolDefinition{{Name: "query", Parameters: json.RawMessage(`{"type":"object"}`)}}
	if _, err := runtime.OpenChatStream(context.Background(), req); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal(err)
	}
	req.Tools = nil
	req.TenantID = 8
	if _, err := runtime.OpenChatStream(context.Background(), req); err == nil {
		t.Fatal("cross tenant request admitted")
	}
}
