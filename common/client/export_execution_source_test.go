package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/models"
	"github.com/google/uuid"
)

type exportRegistryStub struct {
	modules []models.ModuleRoutingEndpoint
	err     error
}

func (s exportRegistryStub) ListModuleEndpoints(context.Context, string) ([]models.ModuleRoutingEndpoint, error) {
	return s.modules, s.err
}

func TestExportSourceClientUsesOnlyLiveOwnerBackendAndTenantServiceToken(t *testing.T) {
	ref := TransferExportSessionReference{SessionID: 4, ExecutionID: uuid.NewString()}
	digest := strings.Repeat("a", 64)
	called := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		if r.Method != "POST" || r.URL.Path != "/api/v1/manager/runtime/export-sessions/4/execution-source" || r.Header.Get("Authorization") != "Bearer transfer-service" {
			t.Error("unexpected owner request")
		}
		var body ExportExecutionSourceRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ExecutionID != ref.ExecutionID || body.RequestDigest != digest {
			t.Error("lost request binding")
		}
		_ = json.NewEncoder(w).Encode(ExportExecutionSource{TenantID: 7, UserID: 9})
	}))
	defer server.Close()
	registry := exportRegistryStub{modules: []models.ModuleRoutingEndpoint{
		{ModuleName: "meta", EndpointID: "other", URL: "http://127.0.0.1:1", ExpiresAt: time.Now().Add(time.Minute)},
		{ModuleName: "manager", EndpointID: "expired", URL: "http://127.0.0.1:1", ExpiresAt: time.Now().Add(-time.Minute)},
		{ModuleName: "manager", EndpointID: "live", URL: server.URL, ExpiresAt: time.Now().Add(time.Minute)},
	}}

	tokens := ServiceTokenProviderFunc(func(_ context.Context, tenant uint) (string, error) {
		if tenant != 7 {
			t.Error("wrong token tenant")
		}
		return "transfer-service", nil
	})
	client := NewExportExecutionSourceClient(registry, tokens)
	facts, err := client.ResolveExportExecutionSource(t.Context(), "manager", 7, ref, digest)
	if err != nil || facts.UserID != 9 || called != 1 {
		t.Fatalf("facts=%#v calls=%d err=%v", facts, called, err)
	}
	client.registry = exportRegistryStub{}
	if _, err := client.ResolveExportExecutionSource(t.Context(), "manager", 7, ref, digest); err == nil || called != 1 {
		t.Fatal("disabled owner contacted")
	}

	client.registry = exportRegistryStub{err: errors.New("registry unavailable")}
	if _, err := client.ResolveExportExecutionSource(t.Context(), "manager", 7, ref, digest); err == nil || called != 1 {
		t.Fatal("bypassed registry")
	}
}

func TestExportSourceClientRejectsRedirectsAndWrongTenantOrEmptyActor(t *testing.T) {
	for _, kind := range []string{"redirect", "tenant", "actor"} {
		t.Run(kind, func(t *testing.T) {
			redirectCalled := false
			other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirectCalled = true }))
			defer other.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if kind == "redirect" {
					http.Redirect(w, r, other.URL, 307)
					return
				}
				facts := ExportExecutionSource{TenantID: 7, UserID: 9}
				if kind == "tenant" {
					facts.TenantID = 8
				}
				if kind == "actor" {
					facts.UserID = 0
				}
				_ = json.NewEncoder(w).Encode(facts)
			}))
			defer server.Close()
			registry := exportRegistryStub{modules: []models.ModuleRoutingEndpoint{{ModuleName: "develop", URL: server.URL, ExpiresAt: time.Now().Add(time.Minute)}}}
			client := NewExportExecutionSourceClient(registry, staticTenantToken("transfer-service"))
			if _, err := client.ResolveExportExecutionSource(t.Context(), "develop", 7, TransferExportSessionReference{SessionID: 1, ExecutionID: uuid.NewString()}, strings.Repeat("a", 64)); err == nil {
				t.Fatal("accepted untrusted source")
			}
			if redirectCalled {
				t.Fatal("forwarded service credential to redirect")
			}
		})
	}
}

func TestExportRequestDigestFreezesAllExecutionFields(t *testing.T) {
	base := CreateTransferExecutionRequest{Name: "export", BatchSize: 1000, Config: TransferExecutionConfig{
		Runtime: TransferExecutionRuntime{Boundary: "bounded"}, Load: TransferExecutionLoad{Mode: "snapshot"},
		Source: TransferExecutionEndpoint{Locator: "source", Query: &TransferExecutionQuery{Language: "sql", Statement: "SELECT 1", Parameters: map[string]interface{}{"p": "value"}}},
		Target: TransferExecutionEndpoint{Name: "orders.csv", ParentLocator: "infra-target"},
	}}
	want, err := ExportRequestDigest(&base)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*CreateTransferExecutionRequest){
		func(r *CreateTransferExecutionRequest) { r.Name = "other" },
		func(r *CreateTransferExecutionRequest) { r.BatchSize = 10 },
		func(r *CreateTransferExecutionRequest) { r.AutoScanMetadata = true },
		func(r *CreateTransferExecutionRequest) { r.Config.Source.Locator = "other-source" },
		func(r *CreateTransferExecutionRequest) { r.Config.Target.ParentLocator = "other-target" },
		func(r *CreateTransferExecutionRequest) { r.Config.Source.Query.Statement = "SELECT 2" },
		func(r *CreateTransferExecutionRequest) { r.Config.Source.Query.Parameters["p"] = "other-value" },
	} {
		encoded, _ := json.Marshal(base)
		var request CreateTransferExecutionRequest
		_ = json.Unmarshal(encoded, &request)
		change(&request)
		got, err := ExportRequestDigest(&request)
		if err != nil || got == want {
			t.Fatal("execution mutation did not change digest")
		}
	}
}
