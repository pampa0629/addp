package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestRebindSourcePreservesCanonicalIdentityAndHistory(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	createSourceEntry(t, db, 7, "old-fingerprint", "00000000000000000001", now, "orders", "id")
	if err := applyMetaDataItemChange(db, 7, commonClient.MetaDataItemChange{
		Operation: "missing", SourceIdentity: "old-fingerprint", SourceVersion: "00000000000000000002",
		ObservedAt: now.Add(time.Minute), Snapshot: map[string]interface{}{"name": "orders"},
	}); err != nil {
		t.Fatalf("mark old source missing: %v", err)
	}
	createSourceEntry(t, db, 7, "new-fingerprint", "00000000000000000003", now.Add(2*time.Minute), "orders_v2", "id", "amount")

	var target, temporary models.Entry
	if err := db.Joins("JOIN catalog.source_bindings source ON source.catalog_entry_id = entries.id").
		Where("source.source_identity = ?", "old-fingerprint").First(&target).Error; err != nil {
		t.Fatalf("find target entry: %v", err)
	}
	if err := db.Joins("JOIN catalog.source_bindings source ON source.catalog_entry_id = entries.id").
		Where("source.source_identity = ?", "new-fingerprint").First(&temporary).Error; err != nil {
		t.Fatalf("find temporary entry: %v", err)
	}
	var originalIDComponent models.Component
	if err := db.Where("catalog_entry_id = ? AND component_key = ?", target.ID, "id").First(&originalIDComponent).Error; err != nil {
		t.Fatalf("find original component: %v", err)
	}

	result, err := NewEntryService(db, nil, nil).RebindSource(context.Background(), 7, target.ID, RebindSourceInput{
		TargetVersion: target.Version, TemporaryEntryID: temporary.ID, TemporaryEntryVersion: temporary.Version,
		NewSourceIdentity: "new-fingerprint", Reason: "physical table renamed", Evidence: "change request CR-42",
	}, UpdateEntryActor{Type: "user", ID: "11"})
	if err != nil {
		t.Fatalf("rebind source: %v", err)
	}
	if result.ID != target.ID || result.Version != target.Version+1 || result.Source == nil || result.Source.SourceIdentity != "new-fingerprint" {
		t.Fatalf("result = %#v", result)
	}
	if result.Source.ReplacedBindingID == nil {
		t.Fatal("replacement binding does not retain previous binding reference")
	}

	if err := db.First(&temporary, "id = ?", temporary.ID).Error; err != nil {
		t.Fatalf("reload temporary entry: %v", err)
	}
	if temporary.EntryStatus != models.EntryStatusMerged || temporary.MergedIntoEntryID == nil || *temporary.MergedIntoEntryID != target.ID || temporary.Version != 2 {
		t.Fatalf("temporary entry = %#v", temporary)
	}
	var temporaryCurrentBindings int64
	if err := db.Model(&models.SourceBinding{}).Where("catalog_entry_id = ? AND is_current = ?", temporary.ID, true).Count(&temporaryCurrentBindings).Error; err != nil {
		t.Fatal(err)
	}
	if temporaryCurrentBindings != 0 {
		t.Fatalf("temporary current bindings = %d", temporaryCurrentBindings)
	}

	var reboundIDComponent models.Component
	if err := db.Where("catalog_entry_id = ? AND component_key = ?", target.ID, "id").First(&reboundIDComponent).Error; err != nil {
		t.Fatalf("find rebound component: %v", err)
	}
	if reboundIDComponent.ID != originalIDComponent.ID || reboundIDComponent.ComponentStatus != models.SourceStatusActive {
		t.Fatalf("rebound component = %#v", reboundIDComponent)
	}
	var amount models.Component
	if err := db.Where("catalog_entry_id = ? AND component_key = ?", target.ID, "amount").First(&amount).Error; err != nil {
		t.Fatalf("new component was not moved: %v", err)
	}

	history, err := NewEntryService(db, nil, nil).History(context.Background(), 7, EntryAccess{Inventory: true}, target.ID)
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if len(history.SourceBindings) != 2 {
		t.Fatalf("source binding history = %d, want 2", len(history.SourceBindings))
	}
	foundRebindAudit := false
	for _, event := range history.AuditEvents {
		if event.EventType == "catalog.source.rebound" {
			foundRebindAudit = event.Details["reason"] == "physical table renamed"
		}
	}
	if !foundRebindAudit {
		t.Fatalf("rebind audit missing: %#v", history.AuditEvents)
	}
}

func TestRebindSourceRejectsTemporaryEntryWithHumanWorkAtomically(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	now := time.Now().UTC()
	createSourceEntry(t, db, 9, "old", "00000000000000000001", now, "old")
	if err := applyMetaDataItemChange(db, 9, commonClient.MetaDataItemChange{
		Operation: "missing", SourceIdentity: "old", SourceVersion: "00000000000000000002", ObservedAt: now,
		Snapshot: map[string]interface{}{"name": "old"},
	}); err != nil {
		t.Fatal(err)
	}
	createSourceEntry(t, db, 9, "new", "00000000000000000003", now, "new")
	var targetBinding, temporaryBinding models.SourceBinding
	if err := db.Where("tenant_id = ? AND source_identity = ? AND is_current = ?", 9, "old", true).First(&targetBinding).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("tenant_id = ? AND source_identity = ? AND is_current = ?", 9, "new", true).First(&temporaryBinding).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.AuditEvent{
		ID: uuid.New(), TenantID: 9, CatalogEntryID: temporaryBinding.CatalogEntryID,
		EventType: "catalog.entry.updated", ActorType: "user", ActorID: "3", Details: commonModels.JSONMap{}, CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	var target, temporary models.Entry
	if err := db.First(&target, "id = ?", targetBinding.CatalogEntryID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&temporary, "id = ?", temporaryBinding.CatalogEntryID).Error; err != nil {
		t.Fatal(err)
	}

	_, err := NewEntryService(db, nil, nil).RebindSource(context.Background(), 9, target.ID, RebindSourceInput{
		TargetVersion: target.Version, TemporaryEntryID: temporary.ID, TemporaryEntryVersion: temporary.Version,
		NewSourceIdentity: "new", Reason: "rename", Evidence: "ticket",
	}, UpdateEntryActor{Type: "user", ID: "5"})
	if !errors.Is(err, ErrSourceRebindConflict) {
		t.Fatalf("error = %v, want source rebind conflict", err)
	}
	if err := db.First(&temporaryBinding, "id = ?", temporaryBinding.ID).Error; err != nil {
		t.Fatal(err)
	}
	if temporaryBinding.CatalogEntryID != temporary.ID || !temporaryBinding.IsCurrent {
		t.Fatalf("temporary binding changed after rejected rebind: %#v", temporaryBinding)
	}
}

func createSourceEntry(t *testing.T, db *gorm.DB, tenantID int64, identity, version string, observedAt time.Time, name string, fields ...string) {
	t.Helper()
	payloadFields := make([]interface{}, 0, len(fields))
	for index, field := range fields {
		payloadFields = append(payloadFields, map[string]interface{}{"name": field, "type": "string", "ordinal_position": index + 1})
	}
	if err := applyMetaDataItemChange(db, tenantID, commonClient.MetaDataItemChange{
		Operation: "upsert", SourceIdentity: identity, SourceVersion: version, ObservedAt: observedAt,
		Snapshot: map[string]interface{}{"name": name, "fields": payloadFields},
	}); err != nil {
		t.Fatalf("create source entry %s: %v", identity, err)
	}
}
