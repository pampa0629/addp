package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/addp/common/models"
)

func TestSystemServiceClientRefreshesRejectedContextTokenOnce(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	tokenRequests := map[string]int{}
	apiRequests := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/oauth/token" {
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			contextKey := "tenant:" + r.Form.Get("tenant_id")
			if r.Form.Get("context_type") == "platform" {
				contextKey = "platform"
			}
			mu.Lock()
			tokenRequests[contextKey]++
			sequence := tokenRequests[contextKey]
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "addp_at_" + strings.ReplaceAll(contextKey, ":", "_") + "_" + strconv.Itoa(sequence),
				"token_type":   "Bearer", "expires_in": 300, "scope": "addp.api",
			})
			return
		}

		mu.Lock()
		apiRequests[r.URL.Path]++
		mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/system/engines/12":
			if r.Header.Get("Authorization") == "Bearer addp_at_tenant_7_1" {
				http.Error(w, "authorization version changed", http.StatusUnauthorized)
				return
			}
			if r.Header.Get("Authorization") != "Bearer addp_at_tenant_7_2" {
				t.Fatalf("tenant Authorization = %q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(models.Engine{ID: 12})
		case "/api/v1/system/engines/13":
			http.Error(w, "forbidden", http.StatusForbidden)
		case "/api/v1/system/runtime/modules":
			if r.Header.Get("Authorization") == "Bearer addp_at_platform_1" {
				http.Error(w, "authorization version changed", http.StatusUnauthorized)
				return
			}
			if r.Header.Get("Authorization") != "Bearer addp_at_platform_2" {
				t.Fatalf("platform Authorization = %q", r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source, err := NewOAuthServiceTokenSource(server.URL, "addp-meta", testServiceClientSecret, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client := NewSystemServiceClient(server.URL, source, server.Client())
	engine, err := client.WithTenantID(7).GetEngine(context.Background(), 12)
	if err != nil || engine.ID != 12 {
		t.Fatalf("GetEngine() engine=%#v error=%v", engine, err)
	}
	if err := client.RegisterModule(context.Background(), &ModuleRegistrationRequest{ModuleName: "meta"}); err != nil {
		t.Fatalf("RegisterModule() error = %v", err)
	}
	if _, err := client.WithTenantID(7).GetEngine(context.Background(), 13); err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("forbidden GetEngine() error = %v", err)
	}
	if tokenRequests["tenant:7"] != 2 || tokenRequests["platform"] != 2 ||
		apiRequests["/api/v1/system/engines/12"] != 2 || apiRequests["/api/v1/system/runtime/modules"] != 2 ||
		apiRequests["/api/v1/system/engines/13"] != 1 {
		t.Fatalf("token requests=%v API requests=%v", tokenRequests, apiRequests)
	}
}

func TestSystemServiceClientUsesTenantAndPlatformBearerWithoutLegacyHeaders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/oauth/token" {
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			suffix := r.Form.Get("tenant_id")
			if r.Form.Get("context_type") == "platform" {
				suffix = "platform"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "addp_at_" + suffix, "token_type": "bearer", "expires_in": 300, "scope": "addp.api",
			})
			return
		}
		if r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != "" {
			t.Fatal("System service request sent legacy authentication headers")
		}
		switch r.URL.Path {
		case "/api/v1/system/engines/12":
			if r.Header.Get("Authorization") != "Bearer addp_at_7" {
				t.Fatalf("tenant Authorization = %q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(models.Engine{ID: 12})
		case "/api/v1/system/runtime/modules":
			if r.Header.Get("Authorization") != "Bearer addp_at_platform" {
				t.Fatalf("platform Authorization = %q", r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source, err := NewOAuthServiceTokenSource(server.URL, "addp-meta", testServiceClientSecret, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client := NewSystemServiceClient(server.URL, source, server.Client())
	if _, err := client.WithTenantID(7).GetEngine(context.Background(), 12); err != nil {
		t.Fatalf("GetEngine() error = %v", err)
	}
	if err := client.RegisterModule(context.Background(), &ModuleRegistrationRequest{ModuleName: "meta"}); err != nil {
		t.Fatalf("RegisterModule() error = %v", err)
	}
}

func TestSystemServiceClientListsEveryEngine(t *testing.T) {
	t.Parallel()

	engineRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/system/oauth/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "addp_at_tenant_7", "token_type": "Bearer", "expires_in": 300, "scope": "addp.api",
			})
		case "/api/v1/system/engines":
			if r.Header.Get("Authorization") != "Bearer addp_at_tenant_7" {
				t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
			}
			if r.URL.RawQuery != "" {
				t.Fatalf("unexpected engine list query: %s", r.URL.RawQuery)
			}
			engineRequests++
			data := make([]models.Engine, 0, 205)
			for index := 0; index < 205; index++ {
				data = append(data, models.Engine{ID: uint(index + 1)})
			}
			_ = json.NewEncoder(w).Encode(data)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source, err := NewOAuthServiceTokenSource(server.URL, "addp-meta", testServiceClientSecret, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client := NewSystemServiceClient(server.URL, source, server.Client())
	engines, err := client.WithTenantID(7).ListEngines(context.Background())
	if err != nil {
		t.Fatalf("ListEngines() error = %v", err)
	}
	if len(engines) != 205 || engines[0].ID != 1 || engines[204].ID != 205 {
		t.Fatalf("engines length=%d first=%d last=%d", len(engines), engines[0].ID, engines[len(engines)-1].ID)
	}
	if engineRequests != 1 {
		t.Fatalf("engine requests = %d, want 1", engineRequests)
	}
}

func TestSystemServiceClientReadsRuntimeDescriptorsWithoutLegacyHeaders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/system/oauth/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "addp_at_tenant_7", "token_type": "Bearer", "expires_in": 300, "scope": "addp.api",
			})
		case "/api/v1/system/runtime/engine-descriptors/12":
			if r.Header.Get("Authorization") != "Bearer addp_at_tenant_7" {
				t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
			}
			if r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != "" {
				t.Fatal("descriptor request sent legacy authentication headers")
			}
			_ = json.NewEncoder(w).Encode(models.EngineRuntimeDescriptor{
				ID:              12,
				RuntimeEndpoint: &models.EngineRuntimeEndpoint{Protocol: "http", Host: "workflow", Port: 8099},
			})
		case "/api/v1/system/runtime/engine-descriptors":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data":  []models.EngineRuntimeDescriptor{{ID: 12}},
				"total": 1, "page": 1, "page_size": 100,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source, err := NewOAuthServiceTokenSource(server.URL, "addp-develop", testServiceClientSecret, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client := NewSystemServiceClient(server.URL, source, server.Client()).WithTenantID(7)
	descriptor, err := client.GetEngineRuntimeDescriptor(context.Background(), 12)
	if err != nil || descriptor.RuntimeEndpoint == nil || descriptor.RuntimeEndpoint.Port != 8099 {
		t.Fatalf("GetEngineRuntimeDescriptor() descriptor=%#v error=%v", descriptor, err)
	}
	descriptors, err := client.ListEngineRuntimeDescriptors(context.Background())
	if err != nil || len(descriptors) != 1 || descriptors[0].ID != 12 {
		t.Fatalf("ListEngineRuntimeDescriptors() descriptors=%#v error=%v", descriptors, err)
	}
}
