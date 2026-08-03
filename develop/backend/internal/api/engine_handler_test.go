package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	commonModels "github.com/addp/common/models"
	"github.com/gin-gonic/gin"
)

type staticDevelopServiceTokens string

func (t staticDevelopServiceTokens) Token(context.Context, uint) (string, error) {
	return string(t), nil
}

func (t staticDevelopServiceTokens) PlatformToken(context.Context) (string, error) {
	return string(t), nil
}

func TestListEnginesReturnsOnlySystemQueryEngines(t *testing.T) {
	capabilitiesJSON, err := dbbridge.GenerateCapabilities("postgresql")
	if err != nil {
		t.Fatalf("GenerateCapabilities(postgresql): %v", err)
	}
	capabilities := commonModels.JSONString(capabilitiesJSON)

	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/runtime/engine-descriptors" {
			t.Fatalf("unexpected system path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer addp_at_service" {
			t.Fatalf("Authorization = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []commonModels.EngineRuntimeDescriptor{{
				ID:             12,
				Name:           "pg-main",
				EngineType:     "postgresql",
				LifecycleState: commonModels.EngineLifecycleActive,
				Capabilities:   &capabilities,
			}},
			"total": 1, "page": 1, "page_size": 100,
		})
	}))
	defer systemServer.Close()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewEngineHandler(commonClient.NewSystemServiceClient(
		systemServer.URL, staticDevelopServiceTokens("addp_at_service"), nil,
	))
	router.GET("/engines", func(c *gin.Context) {
		setTenantAuthContextForTest(c, 7, 1)
		handler.ListEngines(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/engines", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var engines []commonModels.EngineRuntimeDescriptor
	if err := json.Unmarshal(resp.Body.Bytes(), &engines); err != nil {
		t.Fatalf("decode engines: %v; body=%s", err, resp.Body.String())
	}
	if len(engines) != 1 {
		t.Fatalf("engines len = %d, want 1; body=%s", len(engines), resp.Body.String())
	}
	if engines[0].ID == 0 || engines[0].EngineType == "duckdb" {
		t.Fatalf("ListEngines must not append DuckDB pseudo engine: %#v", engines[0])
	}
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(resp.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, exists := raw[0]["connection_info"]; exists {
		t.Fatalf("Develop engine descriptor leaked connection_info: %s", resp.Body.String())
	}
}
