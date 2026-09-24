package service

import (
	"context"
	"encoding/json"
	"errors"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/service/internal/models"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

type analyticalExecutionGate struct {
	entered, protected bool
	deny               bool
}

func (g *analyticalExecutionGate) BeginPreparedQuery(ctx context.Context, tenant uint, p plugin.EnginePlugin, q plugin.PreparedQuery) (func(*plugin.QueryResult) error, func(), error) {
	g.entered = true
	if _, err := q.ReadSet(ctx); err != nil {
		return nil, nil, err
	}
	if _, err := q.OutputLineage(ctx); err != nil {
		return nil, nil, err
	}
	if g.deny {
		return nil, nil, errors.New("test protection rejection")
	}
	return func(r *plugin.QueryResult) error { g.protected = true; return nil }, func() {}, nil
}
func (g *analyticalExecutionGate) BeginUnresolvedRead(context.Context, uint) (func(), error) {
	return nil, errors.New("unexpected alternate execution route")
}

func TestAnalyticalQueryExecutionAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("SERVICE_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires Service PostgreSQL gate")
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	password, _ := parsed.User.Password()
	testAnalyticalQueryExecution(t, "postgresql", commonmodels.ConnectionInfo{"host": parsed.Hostname(), "port": port, "database": strings.TrimPrefix(parsed.Path, "/"), "user": parsed.User.Username(), "password": password, "sslmode": "disable"})
}

// Model and System HTTP responses are controlled fixtures; the executor and database are real.
func testAnalyticalQueryExecution(t *testing.T, engineType string, connection commonmodels.ConnectionInfo) {
	t.Helper()
	frozen, descriptor := metricServiceFixture(t, engineType)
	engine := descriptor.AsEngine()
	engine.ConnectionInfo = connection
	t.Cleanup(func() { _ = plugin.ClosePool(engine.ID) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/plan") {
			_ = json.NewEncoder(w).Encode(frozen)
		} else {
			_ = json.NewEncoder(w).Encode(engine)
		}
	}))
	defer server.Close()
	tokens := commonclient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "addp_at_test", nil })
	executor := NewQueryExecutorService(commonclient.NewSystemClient(server.URL, tokens), nil, []byte(strings.Repeat("k", 32)))
	executor.SetModelClient(commonclient.NewModelClient(server.URL, tokens, server.Client()))
	gate := &analyticalExecutionGate{}
	executor.SetProtectionGate(gate)
	publisher := NewQueryServiceService(nil, commonclient.NewSystemClient(server.URL, tokens), nil, "")
	publisher.SetModelClient(commonclient.NewModelClient(server.URL, tokens, server.Client()))
	binding, err := publisher.resolveMetricSource(t.Context(), &models.CreateQueryServiceRequest{ConfigType: "analytical", MetricSource: &models.MetricSourceRequest{ImplementationID: frozen.ImplementationID, RevisionID: frozen.RevisionID}}, 7)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &models.QueryServiceDependencySnapshot{CapturedAt: time.Now(), MetricSource: binding}
	snapshot.DependencyHash = queryServiceDependencyHash(snapshot)
	service := &models.QueryService{ID: 17, TenantID: 7, ConfigType: "analytical", DataConfig: models.JSONB{models.QueryServiceSourceSnapshotKey: queryServiceSnapshotPayload(snapshot)}}
	request := &models.QueryExecutionRequest{Parameters: map[string]interface{}{"subject_id": "A", "start_date": "2026-01-01", "end_date": "2027-01-01", "grain": "total"}, Select: []string{"value"}, Page: models.QueryPageRequest{Limit: 1}}
	result, err := executor.ExecuteQuery(context.Background(), service, request)
	if err != nil {
		t.Fatal(err)
	}
	if !gate.entered || !gate.protected || len(result.Data) != 1 || len(result.Data[0]) != 1 || result.Data[0]["value"] != int64(1) {
		t.Fatalf("execution skipped protection or leaked hidden fields: %#v", result)
	}
	request.Filter = &models.QueryFilter{Field: "bucket", Op: "eq", Value: "2026-01-01"}
	filtered, err := executor.ExecuteQuery(context.Background(), service, request)
	if err != nil || len(filtered.Data) != 1 || filtered.Data[0]["value"] != int64(1) {
		t.Fatalf("published period filter failed: %+v %v", filtered, err)
	}
	request.Filter.Value = "2026-02-01"
	filtered, err = executor.ExecuteQuery(context.Background(), service, request)
	if err != nil || len(filtered.Data) != 0 {
		t.Fatalf("period filter did not restrict result: %+v %v", filtered, err)
	}
	gate.deny = true
	gate.protected = false
	if _, err = executor.ExecuteQuery(context.Background(), service, request); err == nil || gate.protected {
		t.Fatal("denied publication executed")
	}
}
