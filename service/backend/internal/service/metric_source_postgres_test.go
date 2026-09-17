package service

import (
	"context"
	"encoding/json"
	"errors"
	commonquery "github.com/addp/common/query"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	commonapi "github.com/addp/common/api"
	commonclient "github.com/addp/common/client"
	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestMetricSourcePublicationAgainstPostgres(t *testing.T) {
	for _, migrated := range []bool{false, true} {
		name := "existing_sql"
		if migrated {
			name = "migrated_metric"
		}
		t.Run(name, func(t *testing.T) { testMetricSourcePublicationAgainstPostgres(t, migrated) })
	}
}

func testMetricSourcePublicationAgainstPostgres(t *testing.T, migrated bool) {
	dsn := os.Getenv("SERVICE_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("SERVICE_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	for _, statement := range []string{"DROP SCHEMA IF EXISTS service CASCADE", "CREATE SCHEMA service"} {
		if err := tx.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.AutoMigrate(&models.QueryService{}); err != nil {
		t.Fatal(err)
	}
	item := consumerDescriptorTestService()
	item.ID = 0
	item.Protocols = models.JSONB{"rest_api": map[string]interface{}{"enabled": true, "formats": []string{"json"}}}
	if migrated {
		item.DataConfig[models.QueryServiceSourceSnapshotKey].(map[string]interface{})["metric_source"] = map[string]interface{}{"implementation_id": 3, "revision_id": 3, "sql": "SELECT 1"}
	}
	if err := tx.Create(item).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.AutoMigrate(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.First(item, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	originalVersion := item.Version
	if originalVersion <= 0 {
		t.Fatal("saved definition has no concurrency version")
	}
	if migrated && (item.Status != "inactive" || QueryServiceVersion(item) != "") {
		t.Fatal("migrated service should be inactive without a consumer version")
	}
	plan, engine := metricServiceFixture(t, "postgresql")
	plan.ParameterPresentation = map[string]commonquery.ParameterPresentation{}
	for _, p := range plan.ExecutionPlan.Plan.Parameters {
		plan.ParameterPresentation[p.Name] = commonquery.ParameterPresentation{Labels: map[string]string{"zh-cn": p.Name, "en": p.Name}, Descriptions: map[string]string{"zh-cn": "说明", "en": "Help"}}
	}
	calls := 0
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/plan") {
			calls++
			_ = json.NewEncoder(w).Encode(plan)
			return
		}
		_ = json.NewEncoder(w).Encode(engine)
	}))
	defer owner.Close()
	tokens := commonclient.ServiceTokenProviderFunc(func(_ context.Context, tenant uint) (string, error) {
		if tenant != 7 {
			t.Errorf("unexpected tenant %d", tenant)
		}
		return "addp_at_test", nil
	})
	repo := repository.NewQueryServiceRepository(tx)
	svc := NewQueryServiceService(repo, commonclient.NewSystemClient(owner.URL, tokens), nil, "")
	modelOwner := commonclient.NewModelClient(owner.URL, tokens, owner.Client())
	svc.SetModelClient(modelOwner)
	list, _, err := svc.ListServices(7, 0, 100, models.QueryServiceListFilter{})
	if err != nil || len(list) != 1 || list[0].Version != originalVersion {
		t.Fatalf("management list lost concurrency version: %v", err)
	}
	req := &models.RebindMetricSourceRequest{MetricSource: &models.MetricSourceRequest{ImplementationID: 3, RevisionID: 7}, Version: originalVersion}
	if _, err := svc.RebindMetricSource(context.Background(), item.ID, 8, req); !errors.Is(err, commonapi.ErrNotFound) || calls != 0 {
		t.Fatalf("cross-tenant request: calls=%d error=%v", calls, err)
	}
	// An unrelated saved edit must invalidate management concurrency even when
	// the consumer contract is unchanged or absent after migration.
	if err := tx.Model(&models.QueryService{}).Where("id = ?", item.ID).Updates(map[string]interface{}{"title": "Edited before rebind", "version": gorm.Expr("version + 1")}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RebindMetricSource(context.Background(), item.ID, 7, req); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("concurrent definition edit accepted: %v", err)
	}
	list, _, err = svc.ListServices(7, 0, 100, models.QueryServiceListFilter{})
	if err != nil || len(list) != 1 {
		t.Fatalf("reload management definition: %v", err)
	}
	req.Version = list[0].Version
	dto, err := svc.RebindMetricSource(context.Background(), item.ID, 7, req)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetByID(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dto.Version != stored.Version {
		t.Fatal("returned definition version differs from persisted state")
	}
	if dto.ID != item.ID || dto.Version != originalVersion+2 || stored.ServiceName != item.ServiceName || stored.PublicAccess != item.PublicAccess || stored.MaxFeatures != item.MaxFeatures {
		t.Fatalf("identity/access not preserved: %#v", dto)
	}
	if migrated && stored.Status != "inactive" {
		t.Fatal("rebind automatically activated migrated service")
	}
	if stored.SqlQuery != "" || len(stored.NamedParameters) != 0 || !reflect.DeepEqual(stored.GetStableKey(), plan.ExecutionPlan.Plan.Output.StableKey) || stored.SourceSnapshot().MetricSource.RevisionID != 7 {
		t.Fatalf("incomplete replacement: %#v", stored)
	}
	if !reflect.DeepEqual(stored.GetNamedParameters()[3].Options, plan.ParameterLabels["grain"]) {
		t.Fatal("parameter options did not survive publication storage")
	}
	if !reflect.DeepEqual(stored.GetNamedParameters()[0].Presentation, dto.NamedParameters[0].Presentation) || dto.NamedParameters[0].Presentation == nil {
		t.Fatal("presentation was not persisted and projected")
	}
	if !reflect.DeepEqual(consumerNamedParameters(stored.GetNamedParameters())[0].Presentation, dto.NamedParameters[0].Presentation) {
		t.Fatal("consumer lost presentation")
	}
	if formats := consumerRESTFormats(stored, false); !reflect.DeepEqual(formats, []string{"json"}) {
		t.Fatalf("REST formats widened: %v", formats)
	}
	executor := &QueryExecutorService{}
	executor.SetModelClient(modelOwner)
	input := map[string]interface{}{"subject_id": "A", "start_date": "2026-01-01", "end_date": "2027-01-01", "grain": "total"}
	if err := executor.validateMetricSource(context.Background(), stored, input); err != nil {
		t.Fatal(err)
	}
	plan.ExecutionPlan.Plan.Output.StableKey = []string{"bucket"}
	if err := executor.validateMetricSource(context.Background(), stored, input); err == nil {
		t.Fatal("signature drift accepted")
	}
	plan.ExecutionPlan.Plan.Output.StableKey = []string{"subject_id", "bucket"}
	if _, err := svc.RebindMetricSource(context.Background(), item.ID, 7, req); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("stale publication: %v", err)
	}

	// Metadata-only rebind changes the optimistic publication version too.
	oldVersion := dto.Version
	req.Version = oldVersion
	oldContractVersion := QueryServiceVersion(stored)
	plan.ParameterPresentation["grain"].Descriptions["en"] = "Changed description"
	if err := executor.validateMetricSource(context.Background(), stored, input); err == nil {
		t.Fatal("presentation drift accepted")
	}
	dto, err = svc.RebindMetricSource(context.Background(), item.ID, 7, req)
	if err != nil || dto.Version != oldVersion+1 {
		t.Fatalf("metadata rebind version: %v", err)
	}
	updated, err := repo.GetByID(item.ID)
	if err != nil || QueryServiceVersion(updated) == oldContractVersion {
		t.Fatal("presentation did not change consumer version")
	}
	if _, err := svc.RebindMetricSource(context.Background(), item.ID, 7, req); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("stale metadata rebind: %v", err)
	}
	// A failed validation inside the transaction must not replace any part of the publication.
	req.Version = dto.Version
	plan.ExecutionPlan.Plan.Output.StableKey = []string{"missing"}
	if _, err := svc.RebindMetricSource(context.Background(), item.ID, 7, req); err == nil {
		t.Fatal("invalid output key published")
	}
	after, err := repo.GetByID(item.ID)
	if err != nil || QueryServiceVersion(after) != dto.ServiceVersion || after.SqlQuery != stored.SqlQuery {
		t.Fatalf("failed replacement was not rolled back: %v", err)
	}
}
