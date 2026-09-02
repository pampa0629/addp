package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	standardauthorization "github.com/addp/standard/internal/authorization"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"github.com/addp/standard/internal/service"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMetricProfessionalRelationRouteUsesCurrentUserPermissionAndTenant(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:standard-professional-relation-api?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`ATTACH DATABASE ':memory:' AS standard`,
		`CREATE TABLE standard.metrics (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, name TEXT, code TEXT, type TEXT, status TEXT, base_metric_id INTEGER)`,
		`CREATE TABLE standard.metric_dependencies (id INTEGER PRIMARY KEY, from_metric_id INTEGER NOT NULL, to_metric_id INTEGER NOT NULL, coefficient REAL, note TEXT, created_at DATETIME)`,
		`INSERT INTO standard.metrics VALUES (1, 7, 'Growth', 'growth', 'derived', 'approved', 2), (2, 7, 'Revenue', 'revenue', 'atomic', 'approved', NULL), (3, 8, 'Other tenant', 'other', 'atomic', 'approved', NULL)`,
		`INSERT INTO standard.metric_dependencies VALUES (11, 1, 2, NULL, 'input', CURRENT_TIMESTAMP)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	authServer := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{
		"Bearer metric-reader": {standardauthorization.PermissionStandardMetricRead},
		"Bearer no-permission": {standardauthorization.PermissionStandardDomainRead},
	})
	defer authServer.Close()
	metricService := service.NewMetricService(nil, repository.NewMetricRepository(db), nil, nil)
	router := SetupRouter(db, nil, nil, nil, nil, nil, metricService, nil, nil, nil, nil, nil, authServer.URL, modulelifecycle.NewStandalone("standard"))

	response := performTenantRequest(router, http.MethodGet, "/api/v1/standard/metrics/1/relations", "metric-reader", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	var graph models.ProfessionalRelationsResponse
	if err := json.Unmarshal(response.Body.Bytes(), &graph); err != nil {
		t.Fatal(err)
	}
	if len(graph.Edges) != 2 || graph.Subject.ResourceID != "1" {
		t.Fatalf("graph = %#v", graph)
	}
	forbidden := performTenantRequest(router, http.MethodGet, "/api/v1/standard/metrics/1/relations", "no-permission", "")
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("forbidden status = %d; body=%s", forbidden.Code, forbidden.Body.String())
	}
	crossTenant := performTenantRequest(router, http.MethodGet, "/api/v1/standard/metrics/3/relations", "metric-reader", "")
	if crossTenant.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant status = %d; body=%s", crossTenant.Code, crossTenant.Body.String())
	}
}
