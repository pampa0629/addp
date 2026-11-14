package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/config"
)

func TestLocalNavigationFixturePreparedReadOnly(t *testing.T) {
	source, err := os.ReadFile("../../../../console/frontend/e2e/online/execution-navigation-local-support.mjs")
	if err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile(`export const navigationSQL = '([^']+)'`).FindSubmatch(source)
	if len(matches) != 2 {
		t.Fatal("navigation fixture must declare its one fixed SQL")
	}
	sql := string(matches[1])
	if sql != "SELECT 42 AS fixture_value FROM pg_catalog.pg_sleep(20)" {
		t.Fatal("fixed approved fixture SQL changed")
	}
	policy := defaultExecutionPolicyModel("platform", nil)
	if policy.QueryConcurrency != 20 || policy.QueryPerEngineConcurrency != 5 {
		t.Fatal("navigation fixture cannot assume a modified default queue policy")
	}
	classified, err := AnalyzeQuery("sql", sql)
	if err != nil || classified.Effect != string(SQLExecutionEffectRead) || classified.RequiresConfirmation {
		t.Fatalf("navigation fixture must remain an ordinary read: %+v %v", classified, err)
	}
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/system/runtime/engine-descriptors/13" {
			t.Errorf("preflight must not request credentials or business data: %s", r.URL.Path)
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(commonModels.EngineRuntimeDescriptor{ID: 13, EngineType: "postgresql"})
	}))
	defer system.Close()
	service := NewSQLEngineService(&config.Config{}, commonClient.NewSystemServiceClient(system.URL, staticServiceTokenSource("addp_at_develop"), system.Client()), nil)
	analysis, err := service.AnalyzePreparedQuery(t.Context(), 7, 13, "sql", sql, "", nil, nil, true)
	if err != nil || analysis == nil || queryAnalysisHasErrors(analysis) {
		t.Fatalf("fixed navigation SQL rejected by the actual Provider preflight: %+v %v", analysis, err)
	}
}
