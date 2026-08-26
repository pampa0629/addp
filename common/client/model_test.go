package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestModelClientRejectsInvalidStandardReferenceGuardResponse(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{name: "resource type", response: `{"resource_type":"element","resource_id":42,"state":"frozen","reference_count":0}`},
		{name: "resource id", response: `{"resource_type":"domain","resource_id":43,"state":"frozen","reference_count":0}`},
		{name: "state", response: `{"resource_type":"domain","resource_id":42,"state":"open","reference_count":0}`},
		{name: "negative reference count", response: `{"resource_type":"domain","resource_id":42,"state":"frozen","reference_count":-1}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, test.response)
			}))
			defer server.Close()

			_, err := newModelTestClient(server).SetStandardReferenceGuard(context.Background(), "domain", 42, StandardReferenceGuardFrozen)
			if err == nil || !strings.Contains(err.Error(), "invalid standard reference guard response") {
				t.Fatalf("SetStandardReferenceGuard() error = %v, want invalid response", err)
			}
		})
	}
}

func TestModelClientResolvesMaterializationWriteContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/model/materialization-write-contexts/resolve" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"batch_id":"batch-1","engine_id":9,"staging_locator":"addp://engine/9/schema/public/table/staging","write_columns":["id","value"]}`)
	}))
	defer server.Close()

	context, err := newModelTestClient(server).ResolveMaterializationWriteContext(context.Background(), MaterializationWriteContextRequest{
		ParentExecutionID: "parent-1", LogicalTableID: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if context.EngineID != 9 || len(context.WriteColumns) != 2 || context.WriteColumns[1] != "value" {
		t.Fatalf("context = %#v", context)
	}
}

func TestModelClientRejectsIncompleteMaterializationWriteContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"batch_id":"batch-1","engine_id":9,"staging_locator":"x","write_columns":[]}`)
	}))
	defer server.Close()

	_, err := newModelTestClient(server).ResolveMaterializationWriteContext(context.Background(), MaterializationWriteContextRequest{
		ParentExecutionID: "parent-1", LogicalTableID: 3,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid materialization write context") {
		t.Fatalf("error = %v", err)
	}
}
