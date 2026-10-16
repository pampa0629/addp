package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/system/internal/models"
)

type apiVersionProbeFixture struct {
	*postgresql.PostgreSQLPlugin
	version string
	failure error
	calls   int
}

func (p *apiVersionProbeFixture) Type() string { return "test_api_version_probe" }
func (p *apiVersionProbeFixture) ProbeConnection(context.Context, plugin.ConnectionInfo) (plugin.ConnectionProbeResult, error) {
	p.calls++
	return plugin.ConnectionProbeResult{ServerVersion: p.version}, p.failure
}
func (p *apiVersionProbeFixture) TestConnection(context.Context, plugin.ConnectionInfo) error {
	panic("duplicate connection probe")
}

func TestSavedAndTemporaryConnectionVersionObservations(t *testing.T) {
	fixture := &apiVersionProbeFixture{PostgreSQLPlugin: &postgresql.PostgreSQLPlugin{}, version: "NativeDB 4.5"}
	plugin.Register(fixture)
	t.Cleanup(func() { plugin.Unregister(fixture.Type()) })
	tenant := uint(3)
	router := newEngineListTestRouter(t, models.Engine{TenantID: &tenant, Name: t.Name(), EngineType: fixture.Type(), EngineOrigin: "general", LifecycleState: models.EngineLifecycleActive, ConnectionInfo: models.ConnectionInfo{"host": "localhost", "port": 5432, "database": "test", "user": "user"}})
	call := func(body string) map[string]any {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/engines/1/test", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("probe status=%d body=%s", rec.Code, rec.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	read := func() models.Engine {
		t.Helper()
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/engines", nil))
		var rows []models.Engine
		if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil || len(rows) != 1 {
			t.Fatalf("read engines: %s err=%v", rec.Body.String(), err)
		}
		return rows[0]
	}
	baseline := read()
	out := call("")
	if out["success"] != true || out["probe"].(map[string]any)["server_version"] != fixture.version {
		t.Fatalf("native probe response: %+v", out)
	}
	saved := read()
	if saved.ServerVersion != fixture.version || saved.ServerVersionCheckedAt == nil || saved.Version != baseline.Version || !saved.UpdatedAt.Equal(baseline.UpdatedAt) {
		t.Fatalf("saved observation: %+v", saved)
	}
	fixture.version = "temporary server"
	out = call(`{"connection_info":{"host":"temporary","port":5432,"database":"test","user":"user"},"server_version":"request spoof"}`)
	if out["success"] != true {
		t.Fatal(out)
	}
	latest := read()
	if latest.ServerVersion != saved.ServerVersion || !latest.ServerVersionCheckedAt.Equal(*saved.ServerVersionCheckedAt) {
		t.Fatal("temporary config changed saved version")
	}
	fixture.failure = errors.New("unavailable")
	out = call("")
	latest = read()
	if out["success"] != false || latest.ConnectionStatus != models.EngineConnectionOffline || latest.ServerVersion != saved.ServerVersion || !latest.ServerVersionCheckedAt.Equal(*saved.ServerVersionCheckedAt) {
		t.Fatal("failed probe overwrote successful version", out)
	}
	if fixture.calls != 3 {
		t.Fatal("expected one native query per request", fixture.calls)
	}
}

func TestInstanceVersionIsAbsentFromAdministrativeRequests(t *testing.T) {
	for _, request := range []any{&models.EngineCreateRequest{}, &models.EngineUpdateRequest{}, &models.EngineRestoreRequest{}} {
		if err := json.Unmarshal([]byte(`{"server_version":"spoof","server_version_checked_at":"2026-10-16T00:00:00Z"}`), request); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "server_version") {
			t.Fatalf("administrative request accepts native facts: %s", data)
		}
	}
}

// The workflow protocol reports zero operators explicitly; native-version probes
// must not invent a workflow operator count.
func TestConnectionProbeResponsePreservesExplicitZeroOperatorCount(t *testing.T) {
	zero := 0
	for _, probe := range []*models.EngineConnectionProbe{
		{RuntimeProtocol: plugin.WorkflowRuntimeAPIAddpV1, OperatorsCount: &zero},
		{ServerVersion: "NativeDB 4.5"},
	} {
		data, err := json.Marshal(probe)
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		count, present := decoded["operators_count"]
		if probe.RuntimeProtocol != "" && (!present || count != float64(0)) {
			t.Fatalf("zero workflow count omitted: %s", data)
		}
		if probe.ServerVersion != "" && present {
			t.Fatalf("native probe invented workflow facts: %s", data)
		}
	}
}
