package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	"github.com/google/uuid"
)

type fakeMetaFieldResolver struct {
	fields   []datatype.FieldInfo
	err      error
	tenantID int64
	itemID   int64
}

func (f *fakeMetaFieldResolver) ResolveItemFields(_ context.Context, tenantID, itemID int64) ([]datatype.FieldInfo, error) {
	f.tenantID, f.itemID = tenantID, itemID
	return f.fields, f.err
}

type fakeElementRevisionResolver struct {
	snapshots  map[int64]*commonClient.ElementRevisionBinding
	err        error
	tenantID   int64
	elementIDs []int64
	asOf       time.Time
}

func (f *fakeElementRevisionResolver) ResolveElementRevisionSnapshots(
	_ context.Context,
	tenantID int64,
	elementIDs []int64,
	asOf time.Time,
) (map[int64]*commonClient.ElementRevisionBinding, error) {
	f.tenantID, f.elementIDs, f.asOf = tenantID, append([]int64(nil), elementIDs...), asOf
	return f.snapshots, f.err
}

func TestGetDataDictionaryFederatesCurrentPhysicalFieldsAndPointInTimeStandards(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, idComponent := createEditableCatalogEntry(t, db, 7)
	if err := db.Model(&models.SourceBinding{}).Where("catalog_entry_id = ?", entry.ID).
		Update("observed_snapshot", `{"name":"orders","item_id":21}`).Error; err != nil {
		t.Fatal(err)
	}
	nameComponent := models.Component{
		ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID, ComponentKey: "customer_name",
		DisplayName: "customer_name", DataType: "string", ComponentStatus: models.SourceStatusActive,
		Ordinal: 2, ObservedSnapshot: map[string]any{"name": "customer_name"},
	}
	if err := db.Create(&nameComponent).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	for _, association := range []models.ComponentElementAssociation{
		{ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID, ComponentID: idComponent.ID, ElementID: 50, ObservedVersion: 1, ObservedSnapshot: map[string]any{}, VerifiedAt: now},
		{ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID, ComponentID: nameComponent.ID, ElementID: 60, ObservedVersion: 1, ObservedSnapshot: map[string]any{}, VerifiedAt: now},
	} {
		if err := db.Create(&association).Error; err != nil {
			t.Fatal(err)
		}
	}
	meta := &fakeMetaFieldResolver{fields: []datatype.FieldInfo{
		{Name: "customer_name", Type: datatype.FieldTypeString, Nullable: true, OrdinalPosition: 2},
		{Name: "id", Type: datatype.FieldTypeBigInt, PrimaryKey: true, OrdinalPosition: 1},
		{Name: "created_at", Type: datatype.FieldTypeTimestamp, Nullable: false, OrdinalPosition: 3},
	}}
	standard := &fakeElementRevisionResolver{snapshots: map[int64]*commonClient.ElementRevisionBinding{
		50: {ElementID: 50, RevisionID: 501, RevisionNo: 3, Code: "order_id", Name: "Order ID", DataType: "bigint", ValueDomainKind: "unrestricted", EffectiveFrom: now.Add(-time.Hour)},
		60: nil,
	}}
	dictionary, err := NewEntryService(db, nil, nil).WithDataDictionaryResolvers(meta, standard).
		GetDataDictionary(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, now)
	if err != nil {
		t.Fatalf("GetDataDictionary() error = %v", err)
	}
	if dictionary.SchemaVersion != DataDictionarySchemaVersion || dictionary.EntryID != entry.ID || !dictionary.AsOf.Equal(now) || len(dictionary.Fields) != 3 {
		t.Fatalf("dictionary = %#v", dictionary)
	}
	if dictionary.Fields[0].Physical.Name != "customer_name" || dictionary.Fields[0].ElementID == nil || *dictionary.Fields[0].ElementID != 60 || dictionary.Fields[0].Standard != nil {
		t.Fatalf("historically unresolved field = %#v", dictionary.Fields[0])
	}
	if dictionary.Fields[1].Physical.Name != "id" || dictionary.Fields[1].Standard == nil || dictionary.Fields[1].Standard.RevisionID != 501 {
		t.Fatalf("resolved field = %#v", dictionary.Fields[1])
	}
	if dictionary.Fields[2].ComponentID != nil || dictionary.Fields[2].ElementID != nil || dictionary.Fields[2].Standard != nil {
		t.Fatalf("unmapped live field = %#v", dictionary.Fields[2])
	}
	if meta.tenantID != 7 || meta.itemID != 21 || standard.tenantID != 7 || len(standard.elementIDs) != 2 || standard.elementIDs[0] != 60 || standard.elementIDs[1] != 50 || !standard.asOf.Equal(now) {
		t.Fatalf("resolver calls = meta(%d,%d) standard(%d,%v,%s)", meta.tenantID, meta.itemID, standard.tenantID, standard.elementIDs, standard.asOf)
	}
}

func TestGetDataDictionaryRejectsNonMetaEntry(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, _ := createEditableCatalogEntry(t, db, 7)
	if err := db.Model(&models.SourceBinding{}).Where("catalog_entry_id = ?", entry.ID).
		Updates(map[string]any{"source_module": models.SourceModuleModel, "observed_snapshot": `{"item_id":21}`}).Error; err != nil {
		t.Fatal(err)
	}
	_, err := NewEntryService(db, nil, nil).WithDataDictionaryResolvers(&fakeMetaFieldResolver{}, &fakeElementRevisionResolver{}).
		GetDataDictionary(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, time.Now().UTC())
	if !errors.Is(err, ErrDataDictionaryNotApplicable) {
		t.Fatalf("GetDataDictionary() error = %v", err)
	}
}

func TestGetDataDictionaryReportsDependencyFailure(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, _ := createEditableCatalogEntry(t, db, 7)
	if err := db.Model(&models.SourceBinding{}).Where("catalog_entry_id = ?", entry.ID).
		Update("observed_snapshot", `{"item_id":21}`).Error; err != nil {
		t.Fatal(err)
	}
	meta := &fakeMetaFieldResolver{err: errors.New("Meta unavailable")}
	_, err := NewEntryService(db, nil, nil).WithDataDictionaryResolvers(meta, &fakeElementRevisionResolver{}).
		GetDataDictionary(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, time.Now().UTC())
	if !errors.Is(err, ErrDataDictionaryDependencyUnavailable) {
		t.Fatalf("GetDataDictionary() error = %v", err)
	}
}
