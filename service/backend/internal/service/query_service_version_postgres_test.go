package service

import (
	"context"
	"encoding/json"
	"errors"
	commonclient "github.com/addp/common/client"
	commonmodels "github.com/addp/common/models"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestQueryServiceVersionAgainstPostgres(t *testing.T) {
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
	for _, sql := range []string{"DROP SCHEMA IF EXISTS service CASCADE", "CREATE SCHEMA service"} {
		if err := tx.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.AutoMigrate(tx); err != nil {
		t.Fatal(err)
	}
	item := consumerDescriptorTestService()
	item.ID = 0
	item.Status = "inactive"
	if err := tx.Create(item).Error; err != nil {
		t.Fatal(err)
	}
	// Reconstruct the old schema, then exercise the actual production migration.
	if err := tx.Exec("ALTER TABLE service.query_services DROP COLUMN version CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.AutoMigrate(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.First(item, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if item.Version != 1 {
		t.Fatalf("migrated version = %d", item.Version)
	}
	repo := repository.NewQueryServiceRepository(tx)
	svc := NewQueryServiceService(repo, nil, nil, "")
	ctx := context.Background()
	active, inactive := "active", "inactive"
	dto, err := svc.UpdateService(ctx, item.ID, item.TenantID, &models.UpdateQueryServiceRequest{Version: 1, Status: &active})
	if err != nil || dto.Version != 2 || dto.Status != active {
		t.Fatalf("activate: %#v %v", dto, err)
	}
	if dto.PublicAccess != item.PublicAccess {
		t.Fatal("activation changed access")
	}
	unchanged, _ := repo.GetByID(item.ID)
	changeCount := func() int64 {
		var count int64
		if err := tx.Model(&models.CatalogResourceChangeRow{}).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		return count
	}
	beforeChanges := changeCount()
	for name, action := range map[string]func() error{
		"stale edit": func() error {
			_, e := svc.UpdateService(ctx, item.ID, item.TenantID, &models.UpdateQueryServiceRequest{Version: 1, Status: &inactive})
			return e
		},
		"stale delete":  func() error { return svc.DeleteService(ctx, item.ID, item.TenantID, 1) },
		"stale refresh": func() error { _, e := svc.RefreshSourceSnapshot(ctx, item.ID, item.TenantID, 1); return e },
	} {
		if err := action(); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("%s: %v", name, err)
		}
		after, _ := repo.GetByID(item.ID)
		if !reflect.DeepEqual(after, unchanged) || changeCount() != beforeChanges {
			t.Fatalf("%s had side effects", name)
		}
	}
	for _, version := range []int64{1, 2} {
		if _, e := svc.UpdateService(ctx, item.ID, item.TenantID+1, &models.UpdateQueryServiceRequest{Version: version, Status: &inactive}); !errors.Is(e, commonapi.ErrNotFound) {
			t.Fatalf("tenant update: %v", e)
		}
		if e := svc.DeleteService(ctx, item.ID, item.TenantID+1, version); !errors.Is(e, commonapi.ErrNotFound) {
			t.Fatalf("tenant delete: %v", e)
		}
		if _, e := svc.RefreshSourceSnapshot(ctx, item.ID, item.TenantID+1, version); !errors.Is(e, commonapi.ErrNotFound) {
			t.Fatalf("tenant refresh: %v", e)
		}
	}
	dto, err = svc.UpdateService(ctx, item.ID, item.TenantID, &models.UpdateQueryServiceRequest{Version: 2, Status: &inactive})
	if err != nil || dto.Version != 3 || dto.Status != inactive {
		t.Fatalf("deactivate: %#v %v", dto, err)
	}
	// A callback validation error must roll back the version and all changed fields.
	_, err = repo.UpdateVersioned(ctx, item.ID, item.TenantID, 3, func(row *models.QueryService) error {
		row.Title = "must not persist"
		return ErrInvalidConsumerContract
	})
	if !errors.Is(err, ErrInvalidConsumerContract) {
		t.Fatal(err)
	}
	after, _ := repo.GetByID(item.ID)
	if after.Version != 3 || after.Title != item.Title {
		t.Fatal("failed write persisted")
	}
	if err := repository.AutoMigrate(tx); err != nil {
		t.Fatal(err)
	}
	after, _ = repo.GetByID(item.ID)
	if after.Version != 3 {
		t.Fatal("migration reset existing version")
	}
	if e := svc.DeleteService(ctx, item.ID, item.TenantID, 3); e != nil {
		t.Fatal(e)
	}
	if _, e := repo.GetByID(item.ID); !errors.Is(e, commonapi.ErrNotFound) {
		t.Fatalf("delete did not remove row: %v", e)
	}
	// Snapshot refresh uses the same version boundary, including races while Meta is read.
	metaItem := &commonmodels.MetaItem{ID: 33, Name: "items", Fingerprint: "items-33", Attributes: map[string]interface{}{
		"type_info": map[string]interface{}{"table": map[string]interface{}{
			"fields":      []interface{}{map[string]interface{}{"name": "id", "type": "bigint", "native_type": "int8", "nullable": false, "primary_key": true}},
			"primary_key": []interface{}{"id"},
		}},
	}}
	snapshot, err := buildTableDependencySnapshot(metaItem, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	table := consumerDescriptorTestService()
	table.ID = 0
	table.ServiceName = "refresh-version"
	table.ConfigType = "table"
	table.SqlQuery = ""
	table.Status = "inactive"
	table.DataConfig = models.JSONB{"stable_key": []string{"id"}, models.QueryServiceSourceSnapshotKey: queryServiceSnapshotPayload(snapshot)}
	if e := repo.Create(table); e != nil {
		t.Fatal(e)
	}
	reads := 0
	race := false
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		reads++
		if race {
			_, err = svc.UpdateService(ctx, table.ID, table.TenantID, &models.UpdateQueryServiceRequest{Version: 2, Title: func() *string { v := "Concurrent edit"; return &v }()})
			if err != nil {
				t.Errorf("racing edit: %v", err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(metaItem)
	}))
	defer owner.Close()
	svc.metaClient = commonclient.NewMetaClient(owner.URL, commonclient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "test-token", nil }))
	refreshed, e := svc.RefreshSourceSnapshot(ctx, table.ID, table.TenantID, 1)
	if e != nil || refreshed.Version != 2 {
		t.Fatalf("snapshot refresh: %#v %v", refreshed, e)
	}
	race = true
	if _, e = svc.RefreshSourceSnapshot(ctx, table.ID, table.TenantID, 2); !errors.Is(e, commonapi.ErrConflict) {
		t.Fatalf("snapshot race accepted: %v", e)
	}
	saved, _ := repo.GetByID(table.ID)
	if saved.Version != 3 || saved.Title != "Concurrent edit" || reads != 2 {
		t.Fatal("snapshot refresh overwrote the concurrent edit")
	}

}
