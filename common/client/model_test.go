package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newModelTestClient(server *httptest.Server) *ModelClient {
	tokens := ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "token", nil })
	return NewModelClient(server.URL, tokens, server.Client()).WithTenantID(7)
}

func TestModelClientListEntitiesPaginatesApprovedEntities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("status") != "approved" || r.URL.Query().Get("page_size") != "100" {
			t.Fatalf("unexpected model list query: %s", r.URL.RawQuery)
		}
		page := r.URL.Query().Get("page")
		w.Header().Set("Content-Type", "application/json")
		if page == "1" {
			fmt.Fprint(w, `{"data":[{"id":1,"code":"first"}],"total":2,"page_size":1}`)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":2,"code":"second"}],"total":2,"page_size":1}`)
	}))
	defer server.Close()

	entities, err := newModelTestClient(server).ListEntities(context.Background())
	if err != nil {
		t.Fatalf("list entities: %v", err)
	}
	if len(entities) != 2 || entities[1].Code != "second" {
		t.Fatalf("unexpected entities: %+v", entities)
	}
}

func TestModelClientGetEntityReturnsAttributeFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/model/entities/1" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"id":1,"code":"customer"}`)
			return
		}
		http.Error(w, "attribute lookup failed", http.StatusInternalServerError)
	}))
	defer server.Close()

	if _, err := newModelTestClient(server).GetEntityWithAttributes(context.Background(), 1); err == nil {
		t.Fatal("expected attribute request failure")
	}
}
