package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	commonclient "github.com/addp/common/client"
	"github.com/addp/service/internal/models"
)

func TestExecutionQueryPreviewMatchesExecutionCompiler(t *testing.T) {
	for _, engineType := range []string{"postgresql", "mysql", "metric_test_extension"} {
		t.Run(engineType, func(t *testing.T) {
			frozen, engine := metricServiceFixture(t, engineType)
			withdrawn := false
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/plan") {
					if withdrawn {
						http.Error(w, "withdrawn", http.StatusConflict)
						return
					}
					_ = json.NewEncoder(w).Encode(frozen)
				} else {
					_ = json.NewEncoder(w).Encode(engine)
				}
			}))
			defer server.Close()
			tokens := commonclient.ServiceTokenProviderFunc(func(_ context.Context, tenant uint) (string, error) {
				if tenant != 7 {
					t.Errorf("wrong tenant: %d", tenant)
				}
				return "addp_at_test", nil
			})
			executor := NewQueryExecutorService(commonclient.NewSystemClient(server.URL, tokens), nil, []byte(strings.Repeat("k", 32)))
			executor.SetModelClient(commonclient.NewModelClient(server.URL, tokens, server.Client()))
			snapshot := &models.QueryServiceDependencySnapshot{CapturedAt: time.Now(), MetricSource: &frozen}
			snapshot.DependencyHash = queryServiceDependencyHash(snapshot)
			item := &models.QueryService{ID: 71, TenantID: 7, Version: 9, ConfigType: "analytical", Status: "inactive", MaxFeatures: 5,
				Protocols:  models.JSONB{"rest_api": map[string]interface{}{"enabled": true, "formats": []interface{}{"json"}}},
				DataConfig: models.JSONB{models.QueryServiceSourceSnapshotKey: queryServiceSnapshotPayload(snapshot)}}
			request := &models.QueryExecutionRequest{Parameters: map[string]interface{}{"subject_id": "A' sensitive", "start_date": "2026-01-01", "end_date": "2027-01-01", "grain": "total"},
				Select: []string{"value"}, Page: models.QueryPageRequest{Limit: 1}, Filter: &models.QueryFilter{Field: "bucket", Op: "eq", Value: "2026-01-01"}, Format: "json"}
			result, err := executor.PreviewExecutionQuery(context.Background(), item, request)
			if err != nil {
				t.Fatal(err)
			}
			// This succeeds with no protection gate and no query runtime connection.
			_, actual, err := compileAnalyticalQuery(item, request, queryProtocolREST, engine.AsEngine(), executor.tokenCodec)
			if err != nil {
				t.Fatal(err)
			}
			if result.Query != actual.Query || result.Language != actual.Language || result.Version != 9 || result.RevisionID != frozen.RevisionID || result.EngineID != engine.ID {
				t.Fatalf("preview diverged: %+v", result)
			}
			if strings.Contains(result.Query, "sensitive") || len(result.Parameters) != len(actual.Options.Parameters) {
				t.Fatal("parameter binding lost")
			}
			names := map[string]bool{}
			for _, parameter := range result.Parameters {
				if names[parameter.Name] || parameter.Type == "" {
					t.Fatal("bad parameter metadata")
				}
				names[parameter.Name] = true
				if _, ok := actual.Options.Parameters[parameter.Name]; !ok {
					t.Fatal("invented binding")
				}
				if parameter.Name == "subject_id" && (parameter.Value == nil || *parameter.Value != "A' sensitive") {
					t.Fatal("value changed")
				}
			}
			encoded, _ := json.Marshal(result)
			if strings.Contains(string(encoded), "connection_info") || strings.Contains(string(encoded), "password") {
				t.Fatal("connection disclosure")
			}
			originalQuery := result.Query
			request.Select = []string{"subject_id", "bucket", "value"}
			result, err = executor.PreviewExecutionQuery(context.Background(), item, request)
			if err != nil || result.Query == originalQuery {
				t.Fatalf("selection ignored: %v", err)
			}
			before := calls
			request.Parameters["sql"] = "select secret"
			if _, err = executor.PreviewExecutionQuery(context.Background(), item, request); err == nil || calls != before {
				t.Fatal("untrusted parameter reached dependency owner")
			}
			delete(request.Parameters, "sql")
			request.Filter.Field = "private_source_column"
			if _, err = executor.PreviewExecutionQuery(context.Background(), item, request); !errors.Is(err, ErrInvalidStructuredQuery) {
				t.Fatalf("private column accepted: %v", err)
			}
			request.Filter.Field = "bucket"
			withdrawn = true
			if _, err = executor.PreviewExecutionQuery(context.Background(), item, request); err == nil {
				t.Fatal("withdrawn revision accepted")
			}
			if !reflect.DeepEqual(item.MetricPlan().ExecutionPlan.Plan, frozen.ExecutionPlan.Plan) {
				t.Fatal("preview mutated frozen plan")
			}
		})
	}
}
