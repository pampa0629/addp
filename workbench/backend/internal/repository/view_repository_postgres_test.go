package repository

import (
	"errors"
	"os"
	"testing"

	"github.com/addp/workbench/internal/models"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestViewRepositoryAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("WORKBENCH_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set WORKBENCH_POSTGRES_TEST_DSN to addp_test or an isolated disposable database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP SCHEMA IF EXISTS workbench CASCADE").Error; err != nil {
		t.Fatalf("reset workbench schema: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	repository := NewViewRepository(db)

	viewA := postgresView(7, 11, "View A")
	viewB := postgresView(7, 11, "View B")
	otherOwner := postgresView(7, 12, "Other Owner")
	otherTenant := postgresView(8, 11, "Other Tenant")
	for _, view := range []*models.View{viewA, viewB, otherOwner, otherTenant} {
		if err := repository.Create(view); err != nil {
			t.Fatalf("Create(%s) error = %v", view.Name, err)
		}
	}

	items, total, err := repository.List(7, 11, 0, 1)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 2 || len(items) != 1 {
		t.Fatalf("List() total=%d items=%d", total, len(items))
	}
	if _, err := repository.Get(7, 12, viewA.ID); !errors.Is(err, ErrViewNotFound) {
		t.Fatalf("cross-owner Get() error = %v", err)
	}
	if _, err := repository.Get(8, 11, viewA.ID); !errors.Is(err, ErrViewNotFound) {
		t.Fatalf("cross-tenant Get() error = %v", err)
	}

	viewA.Name = "Updated"
	if err := repository.Update(viewA, 1); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	updated, err := repository.Get(7, 11, viewA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Updated" || updated.Version != 2 {
		t.Fatalf("updated view = %#v", updated)
	}
	if err := repository.Update(viewA, 1); !errors.Is(err, ErrViewVersionConflict) {
		t.Fatalf("stale Update() error = %v", err)
	}
	if err := repository.Delete(7, 12, viewA.ID); !errors.Is(err, ErrViewNotFound) {
		t.Fatalf("cross-owner Delete() error = %v", err)
	}
	if err := repository.Delete(7, 11, viewA.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := repository.Get(7, 11, viewA.ID); !errors.Is(err, ErrViewNotFound) {
		t.Fatalf("deleted Get() error = %v", err)
	}
}

func postgresView(tenantID, ownerUserID int64, name string) *models.View {
	return &models.View{
		ID: uuid.NewString(), TenantID: tenantID, OwnerUserID: ownerUserID,
		Name: name, Description: "", ServiceType: "query", ServiceID: 23,
		ContractFingerprint:  "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ParameterDefinitions: datatypes.JSON(`[]`), QueryTemplate: datatypes.JSON(`{"select":["id"],"fixed_filter":null,"parameter_filters":[],"order_by":[],"page_limit":50,"format":"json"}`),
		DefaultParameterValues: datatypes.JSON(`{}`), RendererType: "table", RendererConfig: datatypes.JSON(`{"columns":["id"]}`), Version: 1,
	}
}
