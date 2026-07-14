package capture

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestConnectClientLifecycle(t *testing.T) {
	var config map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); !ok || user != "control" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "PUT /connectors/cdc/config":
			_ = json.NewDecoder(r.Body).Decode(&config)
			w.WriteHeader(http.StatusCreated)
		case "GET /connectors/cdc/status":
			_, _ = w.Write([]byte(`{"name":"cdc","connector":{"state":"RUNNING","worker_id":"worker-1"},"tasks":[{"state":"RUNNING"}]}`))
		case "PUT /connectors/cdc/pause", "PUT /connectors/cdc/resume":
			w.WriteHeader(http.StatusAccepted)
		case "DELETE /connectors/cdc":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := NewConnectClient(server.URL, "control", "secret", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := client.PutConfig(ctx, "cdc", map[string]string{"connector.class": "postgres"}); err != nil {
		t.Fatal(err)
	}
	if config["connector.class"] != "postgres" {
		t.Fatalf("config = %#v", config)
	}
	status, err := client.Status(ctx, "cdc")
	if err != nil || status.ConnectorState != "RUNNING" || len(status.TaskStates) != 1 {
		t.Fatalf("status = %#v, err = %v", status, err)
	}
	if err := client.Pause(ctx, "cdc"); err != nil {
		t.Fatal(err)
	}
	if err := client.Resume(ctx, "cdc"); err != nil {
		t.Fatal(err)
	}
	if err := client.Delete(ctx, "cdc"); err != nil {
		t.Fatal(err)
	}
}

func TestConnectClientDeleteIsIdempotent(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	client, _ := NewConnectClient(server.URL, "", "", time.Second)
	if err := client.Delete(context.Background(), "missing"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}
