package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	commonapi "github.com/addp/common/api"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	commonmodels "github.com/addp/common/models"
	commonquery "github.com/addp/common/query"
	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestMetricSourcePublicationAgainstPostgres(t *testing.T) {
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
	if err := repository.AutoMigrate(tx); err != nil {
		t.Fatal(err)
	}
	item := consumerDescriptorTestService()
	item.ID = 0
	item.Protocols = models.JSONB{"rest_api": map[string]interface{}{"enabled": true, "formats": []string{"json"}}}
	if err := tx.Create(item).Error; err != nil {
		t.Fatal(err)
	}
	originalVersion := QueryServiceVersion(item)
	plan := commonclient.ModelMetricPlan{
		ImplementationID: 3, RevisionID: 7, MetricDefinitionID: 9, MetricDefinitionRevisionID: 19,
		EngineID: 2, DependencyHash: strings.Repeat("a", 64),
		SQL:       "SELECT :subject_id AS subject_id,:comparison_id AS comparison_id,CAST(:start_date AS date) AS bucket,1::numeric AS value,:directions AS direction WHERE :grain = 'total' AND CAST(:end_date AS date)>CAST(:start_date AS date)",
		StableKey: []string{"direction", "bucket"},
		Fields:    []datatype.FieldInfo{{Name: "subject_id", Type: datatype.FieldTypeString}, {Name: "comparison_id", Type: datatype.FieldTypeString}, {Name: "bucket", Type: datatype.FieldTypeDate}, {Name: "value", Type: datatype.FieldTypeDecimal}, {Name: "direction", Type: datatype.FieldTypeString}},
	}
	for _, name := range []string{"subject_id", "comparison_id", "start_date", "end_date", "grain", "directions"} {
		fieldType := datatype.FieldTypeString
		if strings.HasSuffix(name, "date") {
			fieldType = datatype.FieldTypeDate
		}
		plan.Parameters = append(plan.Parameters, commonclient.ModelMetricParameter{Name: name, Type: fieldType, Required: true})
	}
	plan.Parameters[4].Options = []commonquery.ParameterOption{{Value: "total", Labels: map[string]string{"zh-cn": "全期", "en": "Total"}}}
	enginePlugin := &postgresql.PostgreSQLPlugin{}
	if _, err := plugin.Get(enginePlugin.Type()); err != nil {
		plugin.Register(enginePlugin)
		t.Cleanup(func() { plugin.Unregister(enginePlugin.Type()) })
	}
	capabilities := querySampleCapabilities(t, enginePlugin.Capabilities())
	calls := 0
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/plan") {
			calls++
			_ = json.NewEncoder(w).Encode(plan)
			return
		}
		_ = json.NewEncoder(w).Encode(commonmodels.EngineRuntimeDescriptor{ID: 2, EngineType: "postgresql", LifecycleState: commonmodels.EngineLifecycleActive, ConnectionStatus: commonmodels.EngineConnectionOnline, Capabilities: capabilities})
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
	req := &models.RebindMetricSourceRequest{MetricSource: &models.MetricSourceRequest{ImplementationID: 3, RevisionID: 7}, ServiceVersion: originalVersion}
	if _, err := svc.RebindMetricSource(context.Background(), item.ID, 8, req); !errors.Is(err, commonapi.ErrNotFound) || calls != 0 {
		t.Fatalf("cross-tenant request: calls=%d error=%v", calls, err)
	}
	dto, err := svc.RebindMetricSource(context.Background(), item.ID, 7, req)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetByID(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dto.ID != item.ID || dto.ServiceVersion == originalVersion || stored.ServiceName != item.ServiceName || stored.PublicAccess != item.PublicAccess || stored.MaxFeatures != item.MaxFeatures {
		t.Fatalf("identity/access not preserved: %#v", dto)
	}
	if stored.SqlQuery != plan.SQL || len(stored.NamedParameters) != 6 || !reflect.DeepEqual(stored.GetStableKey(), plan.StableKey) || stored.SourceSnapshot().MetricSource.RevisionID != 7 {
		t.Fatalf("incomplete replacement: %#v", stored)
	}
	if !reflect.DeepEqual(stored.NamedParameters[4].Options, plan.Parameters[4].Options) {
		t.Fatal("parameter options did not survive publication storage")
	}
	if formats := consumerRESTFormats(stored, false); !reflect.DeepEqual(formats, []string{"json"}) {
		t.Fatalf("REST formats widened: %v", formats)
	}
	executor := &QueryExecutorService{}
	executor.SetModelClient(modelOwner)
	input := map[string]interface{}{"subject_id": "A", "comparison_id": "B", "start_date": "2026-01-01", "end_date": "2027-01-01", "grain": "total", "directions": "both"}
	if err := executor.validateMetricSource(context.Background(), stored, input); err != nil {
		t.Fatal(err)
	}
	plan.StableKey = []string{"subject_id", "bucket"}
	if err := executor.validateMetricSource(context.Background(), stored, input); err == nil {
		t.Fatal("signature drift accepted")
	}
	plan.StableKey = []string{"direction", "bucket"}
	if _, err := svc.RebindMetricSource(context.Background(), item.ID, 7, req); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("stale publication: %v", err)
	}

	// Metadata-only rebind changes the optimistic publication version too.
	oldVersion := dto.ServiceVersion
	req.ServiceVersion = oldVersion
	plan.Parameters[4].Options[0].Labels["en"] = "Entire period"
	dto, err = svc.RebindMetricSource(context.Background(), item.ID, 7, req)
	if err != nil || dto.ServiceVersion == oldVersion {
		t.Fatalf("metadata rebind version: %v", err)
	}
	if _, err := svc.RebindMetricSource(context.Background(), item.ID, 7, req); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("stale metadata rebind: %v", err)
	}
	// A failed validation inside the transaction must not replace any part of the publication.
	req.ServiceVersion = dto.ServiceVersion
	plan.StableKey = []string{"missing"}
	if _, err := svc.RebindMetricSource(context.Background(), item.ID, 7, req); err == nil {
		t.Fatal("invalid output key published")
	}
	after, err := repo.GetByID(item.ID)
	if err != nil || QueryServiceVersion(after) != dto.ServiceVersion || after.SqlQuery != stored.SqlQuery {
		t.Fatalf("failed replacement was not rolled back: %v", err)
	}
}
