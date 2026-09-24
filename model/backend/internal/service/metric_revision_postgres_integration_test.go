package service

import (
	"context"
	"encoding/json"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
	queryplan "github.com/addp/common/query/plan"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPostgresMetricRevisionLifecycleIsIndependentAndImmutable(t *testing.T) {
	for _, engineType := range []string{"postgresql", "mysql", "tidb"} {
		t.Run(engineType, func(t *testing.T) { testMetricRevisionLifecycle(t, engineType) })
	}
}

// Metadata ownership always uses PostgreSQL; the metric source dialect varies.
func testMetricRevisionLifecycle(t *testing.T, engineType string) {
	provider, err := plugin.Get(engineType)
	if err != nil {
		t.Fatal(err)
	}
	catalog := provider.(plugin.EngineCatalogModelProvider).EngineCatalogModel()
	branch, ok := plugin.EngineCatalogFirstBusinessBranch(catalog)
	if !ok {
		t.Fatal("metric fixture requires a namespace")
	}
	namespaceType := branch.Term

	tx, tenant := beginModelAggregatePostgresTransaction(t)
	ctx := context.Background()
	contract, bindings := metricGoldenContract()
	if err := tx.Create(&models.DWLayer{TenantID: tenant, LayerCode: "metric_test", LayerName: "Metric test", Version: 1}).Error; err != nil {
		t.Fatal(err)
	}
	tables := map[int64]int64{}
	fields := map[int64]int64{}
	for _, entry := range []struct {
		old    int64
		source metricPlanSource
		kind   string
	}{{1, bindings.Fact, "fact"}, {2, bindings.Relations[10].Target, "dimension"}, {3, bindings.Relations[11].Target, "dimension"}} {
		table := models.LogicalTable{TenantID: tenant, Name: entry.source.Metadata.Name, Code: entry.source.Metadata.Name, TableType: entry.kind, Layer: "metric_test", Status: "approved", Version: 7, CreatedBy: 1, Materialization: models.JSONB{"target_parent_locator": "addp://engine/2/path/model?type=" + namespaceType, "target_name": entry.source.Metadata.Name}}
		if err := tx.Create(&table).Error; err != nil {
			t.Fatal(err)
		}
		tables[entry.old] = table.ID
		for old, field := range entry.source.Fields {
			field.ID = 0
			field.TableID = table.ID
			field.Name = field.ColumnName
			if err := tx.Create(&field).Error; err != nil {
				t.Fatal(err)
			}
			if err := tx.Model(&field).Update("nullable", false).Error; err != nil {
				t.Fatal(err)
			}
			fields[old] = field.ID
		}
	}
	rels := map[int64]int64{}
	for _, old := range []int64{10, 11} {
		r := bindings.Relations[old]
		target := int64(2)
		if old == 11 {
			target = 3
		}
		relation := models.TableRelation{TenantID: tenant, SourceTable: tables[1], SourceField: fields[r.SourceField], TargetTable: tables[target], TargetField: fields[r.TargetField], RelationType: "fk"}
		if err := tx.Create(&relation).Error; err != nil {
			t.Fatal(err)
		}
		rels[old] = relation.ID
	}
	contract.Subject.FieldID = fields[1]
	contract.SubjectRelationID = rels[10]
	contract.SubjectLabel = &models.MetricFieldReference{FieldID: fields[7], RelationID: rels[10]}
	contract.Distinct.FieldID = fields[2]
	contract.Time.FieldID = fields[6]
	contract.Time.RelationID = rels[11]
	contract.Filters[0].Field.FieldID = fields[3]
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/system/runtime/engine-descriptors/2" {
			provider, lookupErr := plugin.Get(engineType)
			if lookupErr != nil {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 2, "engine_type": engineType})
				return
			}
			caps := provider.Capabilities()
			if caps.Compute != nil && caps.Compute.Query != nil {
				caps.Compute.Query.Analytical = &plugin.AnalyticalCapability{Supported: true, PlanVersions: []string{queryplan.SchemaVersion}, SemanticProfiles: []string{queryplan.SemanticProfile}}
			}
			raw, _ := json.Marshal(caps)
			value := commonmodels.JSONString(raw)
			_ = json.NewEncoder(w).Encode(commonmodels.EngineRuntimeDescriptor{ID: 2, EngineType: engineType, Capabilities: &value})
			return
		}
		if r.URL.Path == "/api/v1/meta/items/by-catalog-path" {
			full := r.URL.Query().Get("catalog_path")
			var source metricPlanSource
			for _, candidate := range []metricPlanSource{bindings.Fact, bindings.Relations[10].Target, bindings.Relations[11].Target} {
				if strings.HasSuffix(full, "."+candidate.Metadata.Name) {
					source = candidate
				}
			}
			var physical []datatype.FieldInfo
			for _, field := range source.Fields {
				f := datatype.FieldInfo{Name: field.ColumnName, Type: datatype.FieldType(field.DataType)}
				switch field.DataType {
				case "string":
					f.NativeType = "text"
					if namespaceType == "database" {
						f.NativeType = "varchar(200)"
						f.Size = 200
					}
				case "bool":
					f.NativeType = "boolean"
					if namespaceType == "database" {
						f.NativeType = "tinyint(1)"
					}
				case "date":
					f.NativeType = "date"
				}
				physical = append(physical, f)
			}
			now := time.Now()
			_ = json.NewEncoder(w).Encode(commonmodels.MetaItem{ID: 1, TenantID: uint(tenant), EngineID: 2, FullName: full, ScannedAt: &now, Attributes: map[string]interface{}{"type_info": map[string]interface{}{"table": datatype.TableInfo{Fields: physical}}}})
			return
		}
		_ = json.NewEncoder(w).Encode(commonclient.PublishedMetricDefinitionRevision{ID: 9, TenantID: tenant, RevisionID: 19, RevisionNo: 1, Name: "Leader count", Status: "published", LifecycleState: "active"})
	}))
	defer server.Close()
	svc := NewMetricImplementationService(repository.NewMetricImplementationRepository(tx), repository.NewLogicalTableRepository(tx))
	svc.SetStandardClient(newElementRevisionSnapshotClient(server))
	svc.SetSystemClient(commonclient.NewSystemServiceClient(server.URL, materializationTestTokens{}, nil))
	svc.SetMetaClient(commonclient.NewMetaClient(server.URL, materializationTestTokens{}))
	item, err := svc.Create(context.Background(), tenant, 1, &models.CreateMetricImplementationRequest{FactTableID: tables[1], MetricDefinitionID: 9, Name: "Leader count"})
	if err != nil {
		t.Fatal(err)
	}
	contract.IncludeDetails = true
	req := &models.SaveMetricImplementationRevisionRequest{Version: item.Version, MetricDefinitionRevisionID: 19, Contract: contract}
	nativeEngineType := engineType
	engineType = "duckdb"
	if _, err := svc.SaveDraft(ctx, item.ID, tenant, 1, req); err == nil {
		t.Fatal("unsupported provider accepted a draft")
	}
	engineType = nativeEngineType
	unchanged, err := svc.Get(item.ID, tenant)
	if err != nil || unchanged.Version != item.Version || len(unchanged.Revisions) != 0 {
		t.Fatalf("unsupported provider changed draft: %#v %v", unchanged, err)
	}
	item, err = svc.SaveDraft(ctx, item.ID, tenant, 1, req)
	if err != nil {
		t.Fatal(err)
	}
	if item.Revisions[0].DependencySnapshot["execution_plan"] == nil {
		t.Fatalf("missing execution package: %#v", item.Revisions[0].DependencySnapshot)
	}
	draftID := item.Revisions[0].ID
	if _, err := svc.SaveDraft(ctx, item.ID, tenant, 1, req); err == nil {
		t.Fatal("stale update accepted")
	}
	item, err = svc.ChangeRevisionState(ctx, item.ID, draftID, tenant, 1, item.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := svc.PublishedPlan(ctx, item.ID, draftID, tenant, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublishedPlan(ctx, item.ID, draftID, tenant+1, nil, ""); err == nil {
		t.Fatal("cross-tenant plan accepted")
	}
	details, err := svc.PublishedPlan(ctx, item.ID, draftID, tenant, nil, "details")
	if err != nil {
		t.Fatal(err)
	}
	if details.ResultKind != "details" || details.DependencyHash != plan.DependencyHash || details.ExecutionPlan.PackageHash == plan.ExecutionPlan.PackageHash || len(details.ExecutionPlan.Plan.Output.StableKey) != 3 {
		t.Fatalf("invalid detail publication: %+v", details)
	}
	if _, err := svc.PublishedPlan(ctx, item.ID, draftID, tenant, nil, "unknown"); err == nil {
		t.Fatal("unknown result kind accepted")
	}
	if len(plan.ParameterPresentation) != 4 || plan.ParameterPresentation["end_date"].Labels["zh-cn"] != "结束日期" {
		t.Fatalf("missing frozen presentation: %#v", plan.ParameterPresentation)
	}
	if err := tx.Model(&models.LogicalField{}).Where("id = ?", fields[1]).Update("name", "new subject label").Error; err != nil {
		t.Fatal(err)
	}
	// Descriptive changes do not invalidate a compiled dependency.
	if err := tx.Model(&models.LogicalField{}).Where("id = ?", fields[6]).Updates(map[string]interface{}{"name": "renamed label", "description": "new help"}).Error; err != nil {
		t.Fatal(err)
	}
	after, err := svc.PublishedPlan(ctx, item.ID, draftID, tenant, nil, "")
	if err != nil || after.DependencyHash != plan.DependencyHash || !reflect.DeepEqual(after.ParameterPresentation, plan.ParameterPresentation) {
		t.Fatalf("display change invalidated plan: %v", err)
	}
	req.Version = item.Version
	req.Contract.Filters[0].Value = false
	item, err = svc.SaveDraft(ctx, item.ID, tenant, 1, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(item.Revisions) != 2 || item.Revisions[0].RevisionNo != 2 || !item.Revisions[1].Contract.Filters[0].Value {
		t.Fatalf("published content mutated: %#v", item.Revisions)
	}
	after, err = svc.PublishedPlan(ctx, item.ID, draftID, tenant, nil, "")
	if err != nil || after.ExecutionPlan.PackageHash != plan.ExecutionPlan.PackageHash {
		t.Fatalf("draft changed publication: %v", err)
	}
	if err := svc.Delete(item.ID, tenant, 1, item.Version); err == nil {
		t.Fatal("published history deleted")
	}
	var fact models.LogicalTable
	if err := tx.First(&fact, tables[1]).Error; err != nil {
		t.Fatal(err)
	}
	if fact.Version != 7 || fact.Status != "approved" {
		t.Fatalf("fact changed: %#v", fact)
	}
	if err := tx.Model(&models.LogicalField{}).Where("id = ?", fields[6]).Update("column_name", "changed_date").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublishedPlan(ctx, item.ID, draftID, tenant, nil, ""); err == nil {
		t.Fatal("changed physical column accepted")
	}
	if err := tx.Model(&models.LogicalField{}).Where("id = ?", fields[6]).Update("column_name", "event_date").Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Model(&models.LogicalField{}).Where("id = ?", fields[7]).Update("column_name", "changed_nickname").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublishedPlan(ctx, item.ID, draftID, tenant, nil, ""); err == nil {
		t.Fatal("changed display-name dependency accepted")
	}
	if err := tx.Model(&models.LogicalField{}).Where("id = ?", fields[7]).Update("column_name", "nickname").Error; err != nil {
		t.Fatal(err)
	}
	item, err = svc.ChangeRevisionState(ctx, item.ID, draftID, tenant, 1, item.Version, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublishedPlan(ctx, item.ID, draftID, tenant, nil, ""); err == nil {
		t.Fatal("withdrawn revision executed")
	}
}

func metricReferenceTestClient(t *testing.T, tenant int64) *commonclient.StandardClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(commonclient.PublishedMetricDefinitionRevision{ID: 9, TenantID: tenant, RevisionID: 19, RevisionNo: 1, Name: "Metric", Status: "published", LifecycleState: "active"})
	}))
	t.Cleanup(server.Close)
	return newElementRevisionSnapshotClient(server)
}
