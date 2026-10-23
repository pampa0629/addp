package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/addp/common/modulelifecycle"
	"github.com/addp/service/internal/config"
	"github.com/addp/service/internal/repository"
	serviceInternal "github.com/addp/service/internal/service"
	"github.com/addp/service/internal/service/data"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type graphExecutionDenyGate struct {
	tenants []uint
}

func (g *graphExecutionDenyGate) BeginUnresolvedRead(_ context.Context, tenantID uint) (func(), error) {
	g.tenants = append(g.tenants, tenantID)
	return nil, errors.New("graph execution protection denied")
}

func TestGraphQueryDTOExecutesThroughCanonicalRouteWithAccessBoundaries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	connection.SetMaxOpenConns(1)
	for _, statement := range []string{
		`ATTACH DATABASE ':memory:' AS service`,
		`CREATE TABLE service.graph_query_services (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, service_name TEXT NOT NULL, title TEXT NOT NULL, config_type TEXT NOT NULL, data_config JSON, public_access BOOLEAN, status TEXT)`,
		`INSERT INTO service.graph_query_services VALUES (1, 7, 'private_graph', 'Private', 'shape', CAST('{}' AS BLOB), false, 'active'), (2, 7, 'public_graph', 'Public', 'shape', CAST('{}' AS BLOB), true, 'active'), (3, 7, 'inactive_graph', 'Inactive', 'shape', CAST('{}' AS BLOB), false, 'inactive')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	systemServer := newProtocolAuthSystemServer(t)
	t.Cleanup(systemServer.Close)
	repo := repository.NewGraphQueryServiceRepository(db)
	definitions := serviceInternal.NewGraphQueryServiceService(repo, "https://public.example.test")
	cfg := &config.Config{}
	cfg.SystemServiceURL = systemServer.URL

	for _, tt := range []struct {
		name        string
		id          uint
		token       string
		withoutGate bool
		wantStatus  int
		wantError   string
		wantGate    bool
	}{
		{name: "private anonymous", id: 1, wantStatus: 401, wantError: "requires authentication"},
		{name: "private same tenant reaches protection", id: 1, token: "tenant-7", wantStatus: 500, wantError: "graph execution protection denied", wantGate: true},
		{name: "private cross tenant", id: 1, token: "tenant-8", wantStatus: 404, wantError: "not found"},
		{name: "public anonymous reaches protection", id: 2, wantStatus: 500, wantError: "graph execution protection denied", wantGate: true},
		{name: "public same tenant reaches protection", id: 2, token: "tenant-7", wantStatus: 500, wantError: "graph execution protection denied", wantGate: true},
		{name: "public cross tenant", id: 2, token: "tenant-8", wantStatus: 404, wantError: "not found"},
		{name: "invalid bearer", id: 2, token: "invalid", wantStatus: 401},
		{name: "inactive", id: 3, token: "tenant-7", wantStatus: 400, wantError: "is inactive"},
		{name: "missing protection remains closed", id: 1, token: "tenant-7", withoutGate: true, wantStatus: 500, wantError: "保护门禁未配置"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dto, err := definitions.GetService(tt.id, 7)
			if err != nil {
				t.Fatal(err)
			}
			endpoint, err := url.Parse(dto.Endpoints["execute"])
			if err != nil {
				t.Fatal(err)
			}
			if endpoint.Scheme != "https" || endpoint.Host != "public.example.test" || endpoint.Path != "/api/gquery/"+dto.ServiceName {
				t.Fatalf("DTO execution endpoint = %q", dto.Endpoints["execute"])
			}
			gate := &graphExecutionDenyGate{}
			executor := data.NewGraphQueryExecutor(repo, nil)
			if !tt.withoutGate {
				executor.SetProtectionGate(gate)
			}
			handler := NewGraphQueryHandler(definitions, executor)
			router := SetupRouter(cfg, db,
				nil, nil, nil, nil,
				nil, nil, nil, nil,
				nil, nil, nil, handler,
				nil, nil, nil, modulelifecycle.NewStandalone("service"))
			request := httptest.NewRequest(http.MethodPost, endpoint.String(), strings.NewReader(`{"parameters":{"city":"北京"},"page":2,"page_size":5}`))
			request.Header.Set("Content-Type", "application/json")
			if tt.token != "" {
				request.Header.Set("Authorization", "Bearer "+tt.token)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tt.wantStatus || !strings.Contains(response.Body.String(), tt.wantError) {
				t.Fatalf("DTO endpoint status/body = %d %s, want %d containing %q", response.Code, response.Body.String(), tt.wantStatus, tt.wantError)
			}
			if tt.wantGate {
				if len(gate.tenants) != 1 || gate.tenants[0] != 7 {
					t.Fatalf("protection tenants = %v, want [7]", gate.tenants)
				}
			} else if len(gate.tenants) != 0 {
				t.Fatalf("rejected request reached protection: %v", gate.tenants)
			}
			for _, path := range []string{"/api/v1/gquery/" + dto.ServiceName, "/api/v1/service/gquery/" + dto.ServiceName} {
				legacy := httptest.NewRecorder()
				router.ServeHTTP(legacy, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`)))
				if legacy.Code != http.StatusNotFound {
					t.Fatalf("unexpected execution alias %s: %d", path, legacy.Code)
				}
			}
		})
	}
}
