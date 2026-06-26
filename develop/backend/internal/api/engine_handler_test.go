package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	commonModels "github.com/addp/common/models"
	"github.com/gin-gonic/gin"
)

func TestListEnginesReturnsOnlySystemQueryEngines(t *testing.T) {
	capabilitiesJSON, err := dbbridge.GenerateCapabilities("postgresql")
	if err != nil {
		t.Fatalf("GenerateCapabilities(postgresql): %v", err)
	}
	capabilities := commonModels.JSONString(capabilitiesJSON)

	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/internal/engines" {
			t.Fatalf("unexpected system path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("storage_type"); got != "tabular,dynamic_schema,graph" {
			t.Fatalf("storage_type = %q, want tabular,dynamic_schema,graph", got)
		}
		_ = json.NewEncoder(w).Encode([]commonModels.Engine{
			{
				ID:           12,
				Name:         "pg-main",
				EngineType:   "postgresql",
				Capabilities: &capabilities,
			},
		})
	}))
	defer systemServer.Close()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewEngineHandler(commonClient.NewSystemClientWithInternalKey(systemServer.URL, "test-key"))
	router.GET("/engines", func(c *gin.Context) {
		c.Set("tenant_id", uint(7))
		handler.ListEngines(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/engines", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var engines []commonModels.Engine
	if err := json.Unmarshal(resp.Body.Bytes(), &engines); err != nil {
		t.Fatalf("decode engines: %v; body=%s", err, resp.Body.String())
	}
	if len(engines) != 1 {
		t.Fatalf("engines len = %d, want 1; body=%s", len(engines), resp.Body.String())
	}
	if engines[0].ID == 0 || engines[0].EngineType == "duckdb" {
		t.Fatalf("ListEngines must not append DuckDB pseudo engine: %#v", engines[0])
	}
}

func TestListQueryModesExposesDuckDBMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewEngineHandler(nil)
	router.GET("/query-modes", handler.ListQueryModes)

	req := httptest.NewRequest(http.MethodGet, "/query-modes", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var modes []QueryMode
	if err := json.Unmarshal(resp.Body.Bytes(), &modes); err != nil {
		t.Fatalf("decode query modes: %v; body=%s", err, resp.Body.String())
	}
	if len(modes) != 1 || modes[0].Mode != "duckdb" || modes[0].QueryType != "sql" {
		t.Fatalf("query modes = %#v, want duckdb sql mode", modes)
	}
}
