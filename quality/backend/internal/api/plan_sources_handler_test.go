package api

import (
	"context"
	"encoding/json"
	commonClient "github.com/addp/common/client"
	"github.com/addp/quality/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPlanElementCandidatesUseTenantServiceProjection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	standardServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/standard/elements" || r.URL.Query().Get("keyword") != "gender" {
			t.Fatalf("unexpected Standard request: %s", r.URL.String())
		}
		if r.URL.Query().Get("page") != "1" || r.URL.Query().Get("page_size") != "100" {
			t.Fatalf("unexpected pagination: %s", r.URL.RawQuery)
		}
		if r.Header.Get("Authorization") != "Bearer tenant-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":12,"code":"gender","current_revision":{"id":1201,"revision_no":3,"status":"published","name":"Gender","data_type":"string","compiled_quality_rules":{"schema_version":"addp.quality.rules/v1","rules":[{"rule_key":"00000000-0000-4000-8000-000000000001","type":"not_null","enabled":true,"severity":"error","message":"required","params":{}}]}}}],"total":1,"page":1,"page_size":100,"total_pages":1}`))
	}))
	defer standardServer.Close()
	standardClient := commonClient.NewStandardClient(standardServer.URL, commonClient.ServiceTokenProviderFunc(func(_ context.Context, tenantID uint) (string, error) {
		if tenantID != 7 {
			t.Fatalf("tenant ID = %d, want 7", tenantID)
		}
		return "tenant-token", nil
	}), standardServer.Client())
	handler := NewRuleHandler(service.NewRuleService(nil, standardClient))
	router := gin.New()
	router.GET("/rules/element-candidates", withIssueHandlerAuth(7, 11), handler.ListElementCandidates)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/rules/element-candidates?keyword=gender&page=0&page_size=999", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	var body qualityElementCandidateListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Total != 1 || body.Page != 1 || body.PageSize != 100 || len(body.Data) != 1 || body.Data[0].ID != 12 || len(body.Data[0].QualityRules.EnabledRules()) != 1 {
		t.Fatalf("candidate response = %#v", body)
	}

	emptyResponse := httptest.NewRecorder()
	router.ServeHTTP(emptyResponse, httptest.NewRequest(http.MethodGet, "/rules/element-candidates", nil))
	if emptyResponse.Code != http.StatusBadRequest {
		t.Fatalf("empty keyword status = %d, want %d, body=%s", emptyResponse.Code, http.StatusBadRequest, emptyResponse.Body.String())
	}
}
