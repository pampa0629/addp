package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonclient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
)

type runtimeTestTokenSource struct {
	tenantID uint
}

func (s *runtimeTestTokenSource) Token(_ context.Context, tenantID uint) (string, error) {
	s.tenantID = tenantID
	return "addp_at_runtime-test", nil
}

func (s *runtimeTestTokenSource) PlatformToken(context.Context) (string, error) {
	return "addp_at_runtime-platform-test", nil
}

func TestExecutorRejectsExecutionAuthorizationForAnotherAudience(t *testing.T) {
	t.Parallel()

	var requestedPath string
	var requestedAuthorization string
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		requestedAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(commonclient.ExecutionEngineAccess{
			AuthorizationID: "19",
			ExecutionID:     "45b4db3c-cd45-493c-9d0d-02538ab94f81",
			Audience:        "workflow",
			EngineID:        "7",
			Effects:         []string{"read"},
			ExpiresAt:       time.Now().UTC().Add(time.Minute),
			Engine: &commonmodels.Engine{
				ID:         7,
				Name:       "business-postgresql",
				EngineType: "postgresql",
			},
		})
	}))
	defer systemServer.Close()

	tokens := &runtimeTestTokenSource{}
	systemClient := commonclient.NewSystemServiceClient(systemServer.URL, tokens, systemServer.Client())
	metaClient := commonclient.NewMetaClient("http://meta.invalid", tokens)
	executor := NewExecutor(systemClient, metaClient, 100, "128MB", 1, time.Second, "")
	_, err := executor.Execute(context.Background(), 3, plugin.FederatedQueryRequest{
		ExecutionID:              "45b4db3c-cd45-493c-9d0d-02538ab94f81",
		ExecutionAuthorizationID: "19",
		SourceEngineIDs:          []uint{7},
		Language:                 "sql",
		Query:                    "SELECT * FROM business_postgresql.public.orders",
		Options:                  plugin.QueryOptions{ReadOnly: true},
		ObjectTables:             map[string]map[string]string{},
	})
	if err == nil || !strings.Contains(err.Error(), "not supported by DuckDB runtime") {
		t.Fatalf("Execute() error = %v, want audience rejection", err)
	}
	if tokens.tenantID != 3 {
		t.Fatalf("service token tenant = %d, want 3", tokens.tenantID)
	}
	if requestedPath != "/api/v1/system/execution-authorizations/19/engine-accesses" {
		t.Fatalf("System path = %q", requestedPath)
	}
	if requestedAuthorization != "Bearer addp_at_runtime-test" {
		t.Fatalf("Authorization = %q", requestedAuthorization)
	}
}

func TestExecutorRejectsUnsafeSQLBeforeConsumingAuthorization(t *testing.T) {
	t.Parallel()

	calls := 0
	systemServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls++
	}))
	defer systemServer.Close()
	tokens := &runtimeTestTokenSource{}
	executor := NewExecutor(
		commonclient.NewSystemServiceClient(systemServer.URL, tokens, systemServer.Client()),
		commonclient.NewMetaClient("http://meta.invalid", tokens),
		100, "128MB", 1, time.Second, "",
	)
	_, err := executor.Execute(context.Background(), 3, plugin.FederatedQueryRequest{
		ExecutionID:              "45b4db3c-cd45-493c-9d0d-02538ab94f81",
		ExecutionAuthorizationID: "19",
		SourceEngineIDs:          []uint{7},
		Language:                 "sql",
		Query:                    "DELETE FROM business_postgresql.public.orders",
		Options:                  plugin.QueryOptions{ReadOnly: true},
	})
	if err == nil {
		t.Fatal("unsafe SQL must be rejected")
	}
	if calls != 0 {
		t.Fatalf("System authorization calls = %d, want 0", calls)
	}
}

func TestRuntimeQueryDescribeDoesNotWrapOrPaginateSQL(t *testing.T) {
	t.Parallel()

	query := runtimeQuery("SELECT id FROM business.orders LIMIT 10;", plugin.QueryOptions{
		Describe: true, Limit: 1, Offset: 20,
	}, 100)
	if query != "DESCRIBE SELECT id FROM business.orders LIMIT 10" {
		t.Fatalf("runtimeQuery() = %q", query)
	}
}

func TestExecutorMapsSpatialQueryOptionToFederatedSession(t *testing.T) {
	t.Parallel()

	executor := &Executor{maxMemory: "128MB", threads: 2}
	options := executor.sessionOptions(plugin.QueryOptions{Spatial: true})
	if options.MemoryLimit != "128MB" || options.Threads != 2 || !options.LoadSpatial {
		t.Fatalf("session options = %#v", options)
	}
}

func TestNormalizeQueryArgsPreservesJSONIntegers(t *testing.T) {
	t.Parallel()

	args, err := normalizeQueryArgs([]interface{}{json.Number("9007199254740993"), json.Number("1.25"), "value"})
	if err != nil {
		t.Fatalf("normalizeQueryArgs() error = %v", err)
	}
	if args[0] != int64(9007199254740993) || args[1] != 1.25 || args[2] != "value" {
		t.Fatalf("args = %#v", args)
	}
}
