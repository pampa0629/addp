package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

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
		`CREATE TABLE standard.metric_definitions (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, category_id INTEGER, scope_type TEXT NOT NULL, owner_domain_id INTEGER, code TEXT NOT NULL, steward_id INTEGER, tags TEXT, draft_revision_id INTEGER, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME, version INTEGER NOT NULL DEFAULT 1, lifecycle_state TEXT NOT NULL DEFAULT 'active')`,
		`CREATE TABLE standard.metric_definition_revisions (id INTEGER PRIMARY KEY, metric_definition_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, status TEXT NOT NULL, metric_type TEXT NOT NULL, name TEXT NOT NULL, definition TEXT NOT NULL, statistical_caliber TEXT NOT NULL, semantic_formula TEXT, unit_id INTEGER, change_summary TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME, submitted_by INTEGER, submitted_at DATETIME, published_by INTEGER, published_at DATETIME, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE standard.metric_definition_revision_dependencies (id INTEGER PRIMARY KEY, metric_definition_revision_id INTEGER NOT NULL, dependency_definition_id INTEGER NOT NULL, dependency_revision_id INTEGER, relation_kind TEXT NOT NULL, coefficient REAL, note TEXT, created_at DATETIME)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []models.MetricDefinition{
		{ID: 1, TenantID: 7, Code: "growth", ScopeType: "tenant_common", Tags: models.StringArray{}, CreatedBy: 1, Version: 1, LifecycleState: "active"},
		{ID: 2, TenantID: 7, Code: "revenue", ScopeType: "tenant_common", Tags: models.StringArray{}, CreatedBy: 1, Version: 1, LifecycleState: "active"},
		{ID: 3, TenantID: 8, Code: "other", ScopeType: "tenant_common", Tags: models.StringArray{}, CreatedBy: 1, Version: 1, LifecycleState: "active"},
	} {
		if err := db.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
	}
	effectiveFrom := time.Now().UTC().Add(-time.Hour)
	for _, revision := range []models.MetricDefinitionRevision{
		{ID: 11, MetricDefinitionID: 1, RevisionNo: 1, Status: models.RevisionStatusPublished, MetricType: models.MetricTypeDerived, Name: "Growth", Definition: "Growth", StatisticalCaliber: "Published", ChangeSummary: "Initial", EffectiveFrom: &effectiveFrom, CreatedBy: 1},
		{ID: 12, MetricDefinitionID: 2, RevisionNo: 1, Status: models.RevisionStatusPublished, MetricType: models.MetricTypeAtomic, Name: "Revenue", Definition: "Revenue", StatisticalCaliber: "Published", ChangeSummary: "Initial", EffectiveFrom: &effectiveFrom, CreatedBy: 1},
		{ID: 13, MetricDefinitionID: 3, RevisionNo: 1, Status: models.RevisionStatusPublished, MetricType: models.MetricTypeAtomic, Name: "Other tenant", Definition: "Other", StatisticalCaliber: "Published", ChangeSummary: "Initial", EffectiveFrom: &effectiveFrom, CreatedBy: 1},
	} {
		if err := db.Create(&revision).Error; err != nil {
			t.Fatal(err)
		}
	}
	dependencyRevisionID := int64(12)
	if err := db.Create(&models.MetricDefinitionRevisionDependency{ID: 21, MetricDefinitionRevisionID: 11, DependencyDefinitionID: 2, DependencyRevisionID: &dependencyRevisionID, RelationKind: models.MetricDependencyBase, Note: "input"}).Error; err != nil {
		t.Fatal(err)
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
	if len(graph.Edges) != 1 || graph.Subject.ResourceID != "1" || graph.Edges[0].RelationKind != "standard.metric.base" {
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
