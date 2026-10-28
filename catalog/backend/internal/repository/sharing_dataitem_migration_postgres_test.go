package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Called by the registered Catalog migration PostgreSQL owner test.
func exerciseSharingDataItemMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	var now time.Time
	if err := db.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
		t.Fatal(err)
	}
	entry := models.Entry{ID: uuid.New(), TenantID: 7, EntryType: models.EntryTypeDataItem, EntryStatus: models.EntryStatusActive,
		GovernanceStatus: models.GovernanceStatusDiscovered, Visibility: models.VisibilityInventory, Version: 1}
	established := true
	entry.BusinessResponsibilityEstablished = &established
	if err := db.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	bound := factstest.Binding(plugin.TabularItemPath(12, "schema", "public", "migration_"+entry.ID.String()))
	source := models.SourceBinding{ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID, SourceModule: models.SourceModuleMeta,
		SourceType: models.SourceTypeDataItem, SourceIdentity: bound.ItemFingerprint, SourceVersion: "00000000000000000001", IsCurrent: true,
		SourceStatus: models.SourceStatusActive, BoundAt: now, ObservedAt: now, ObservedSnapshot: commonModels.JSONMap{}}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	owner := models.Responsibility{ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID, Role: models.ResponsibilityRoleBusinessOwner,
		SubjectType: "user", SubjectID: 40, Status: models.ResponsibilityStatusActive, VerifiedAt: now, ObservedSnapshot: commonModels.JSONMap{}}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	path, _ := json.Marshal(bound.NativeTarget)
	decision := models.SharingDecision{ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID, EntryVersion: 2,
		SourceBindingID: source.ID, SourceVersion: source.SourceVersion, ResponsibilityID: owner.ID, EngineID: 12, CatalogPath: path,
		ItemFingerprint: bound.ItemFingerprint, BoundaryHash: bound.BoundaryHash, ConfirmedBy: 40, ConfirmerMembershipID: 41, AuthorizationVersion: 1,
		RecipientType: "user", RecipientID: 40, Action: "read", SelfBeneficiary: true, Reason: "Disposable native confirmation", ExpiryMode: "until_revoked", CreatedAt: now}
	if err := db.Create(&decision).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.FulfillmentCheck{RequestID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID, RequestBinding: json.RawMessage(`{"legacy":"test"}`), CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	audit := models.AuditEvent{ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID, EventType: "test.binding.migration", ActorType: "user", ActorID: "40", Details: commonModels.JSONMap{}, CreatedAt: now}
	if err := db.Create(&audit).Error; err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		var value string
		if err := db.Raw(`SELECT jsonb_build_object('entries',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM catalog.entries e),
 'sources',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM catalog.source_bindings s),
 'owners',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM catalog.responsibilities r),
 'audits',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM catalog.audit_events a))::text`).Scan(&value).Error; err != nil {
			t.Fatal(err)
		}
		return value
	}
	// Simulate the previous installation with no owner retirement checkpoint.
	beforeRetirement := snapshot()
	if err := db.Exec("DELETE FROM catalog.startup_schema_revision").Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if snapshot() != beforeRetirement {
		t.Fatal("authorization retirement changed Catalog resources or audit")
	}
	for _, table := range []string{"catalog.sharing_decisions", "catalog.fulfillment_checks"} {
		var n int64
		if err := db.Table(table).Count(&n).Error; err != nil || n != 0 {
			t.Fatalf("retirement left %s: %d %v", table, n, err)
		}
	}
	decision.ID = uuid.New()
	if err := db.Create(&decision).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var surviving int64
	if err := db.Model(&models.SharingDecision{}).Where("id=?", decision.ID).Count(&surviving).Error; err != nil || surviving != 1 {
		t.Fatalf("repeat startup deleted new approval: %d %v", surviving, err)
	}
	// Reconstruct the previous native-target schema, preserving actual history.
	if err := db.Exec(`ALTER TABLE catalog.sharing_decisions DROP COLUMN item_fingerprint CASCADE, DROP COLUMN boundary_hash CASCADE`).Error; err != nil {
		t.Fatal(err)
	}
	before := snapshot()
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if snapshot() != before {
		t.Fatal("binding migration changed Catalog entries, sources, responsibilities or generic audit")
	}
	for _, table := range []string{"catalog.sharing_decisions", "catalog.fulfillment_checks"} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("old %s retained: %d %v", table, count, err)
		}
	}
}
