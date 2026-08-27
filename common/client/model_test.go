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

func TestModelClientResolvesMaterializationReadContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/model/materialization-read-contexts" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"schema_version":"model.materialization-read-context/v1","items":[{"logical_table_id":3,"batch_id":"batch-1","engine_id":9,"staging_locator":"addp://engine/9/schema/public/table/staging","columns":[{"name":"id","data_type":"bigint","nullable":false}],"schema_fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`)
	}))
	defer server.Close()

	result, err := newModelTestClient(server).ResolveMaterializationReadContext(context.Background(), ResolveMaterializationReadContextRequest{
		ParentExecutionID: "parent-1", ReaderExecutionID: "reader-1", ReaderAttempt: 2,
		ReaderLeaseToken: "1cf3c4f4-b567-4bc6-a20a-0bbeb8928e21", LogicalTableIDs: []int64{3},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Items[0].EngineID != 9 || result.Items[0].Columns[0].Name != "id" {
		t.Fatalf("result = %#v", result)
	}
}

func TestModelClientGetsMaterializationGroup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/model/materialization-groups/12" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":12,"code":"outdoor_core","name":"Outdoor Core","version":3,"members":[{"logical_table_id":5,"position":0},{"logical_table_id":8,"position":1}]}`)
	}))
	defer server.Close()

	group, err := newModelTestClient(server).GetMaterializationGroup(context.Background(), 12)
	if err != nil {
		t.Fatal(err)
	}
	if group.Version != 3 || len(group.Members) != 2 || group.Members[1].LogicalTableID != 8 {
		t.Fatalf("group = %#v", group)
	}
}

func TestModelClientRejectsInvalidMaterializationGroup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":12,"code":"outdoor_core","name":"Outdoor Core","version":3,"members":[{"logical_table_id":5,"position":1}]}`)
	}))
	defer server.Close()

	if _, err := newModelTestClient(server).GetMaterializationGroup(context.Background(), 12); err == nil || !strings.Contains(err.Error(), "non-contiguous") {
		t.Fatalf("error = %v", err)
	}
}

func TestModelClientRejectsCrossEngineMaterializationReadContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"schema_version":"model.materialization-read-context/v1","items":[{"logical_table_id":3,"batch_id":"batch-1","engine_id":9,"staging_locator":"one","columns":[{"name":"id","data_type":"bigint"}],"schema_fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},{"logical_table_id":4,"batch_id":"batch-2","engine_id":10,"staging_locator":"two","columns":[{"name":"id","data_type":"bigint"}],"schema_fingerprint":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]}`)
	}))
	defer server.Close()

	_, err := newModelTestClient(server).ResolveMaterializationReadContext(context.Background(), ResolveMaterializationReadContextRequest{
		ParentExecutionID: "parent-1", ReaderExecutionID: "reader-1", ReaderAttempt: 1,
		ReaderLeaseToken: "1cf3c4f4-b567-4bc6-a20a-0bbeb8928e21", LogicalTableIDs: []int64{3, 4},
	})
	if err == nil || !strings.Contains(err.Error(), "cross-engine") {
		t.Fatalf("error = %v", err)
	}
}
