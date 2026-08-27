package service

import (
	"context"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	"github.com/google/uuid"
)

func TestPersonalCatalogMarksAreUserScopedAndAtomicallyReplaced(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, _ := createEditableCatalogEntry(t, db, 7)
	entries := NewEntryService(db, nil, nil)
	personal := NewPersonalCatalogService(db, entries)
	personal.now = func() time.Time { return time.Date(2026, 8, 26, 13, 0, 0, 0, time.UTC) }
	access := EntryAccess{Inventory: true}

	marks, err := personal.ReplaceMarks(context.Background(), 7, 40, access, entry.ID, EntryMarks{Favorite: true, Following: true})
	if err != nil || !marks.Favorite || !marks.Following {
		t.Fatalf("ReplaceMarks() = %#v, %v", marks, err)
	}
	other, err := personal.GetMarks(context.Background(), 7, 41, access, entry.ID)
	if err != nil || other.Favorite || other.Following {
		t.Fatalf("other user marks = %#v, %v", other, err)
	}
	marks, err = personal.ReplaceMarks(context.Background(), 7, 40, access, entry.ID, EntryMarks{Following: true})
	if err != nil || marks.Favorite || !marks.Following {
		t.Fatalf("second ReplaceMarks() = %#v, %v", marks, err)
	}
	var stored []models.EntryMark
	if err := db.Where("tenant_id = ? AND user_id = ? AND catalog_entry_id = ?", 7, 40, entry.ID).Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].MarkType != models.EntryMarkTypeFollowing {
		t.Fatalf("stored marks = %#v", stored)
	}
}

func TestPersonalCatalogListsRelationsWithCatalogVisibility(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, _ := createEditableCatalogEntry(t, db, 7)
	entries := NewEntryService(db, nil, nil)
	personal := NewPersonalCatalogService(db, entries)
	if _, err := personal.ReplaceMarks(context.Background(), 7, 40, EntryAccess{Inventory: true}, entry.ID, EntryMarks{Favorite: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Responsibility{
		ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID, Role: models.ResponsibilityRoleDataSteward,
		SubjectType: models.ResponsibilitySubjectUser, SubjectID: 40, Status: models.ResponsibilityStatusActive,
		ObservedSnapshot: map[string]interface{}{}, VerifiedAt: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	for _, relation := range []string{PersonalRelationResponsible, PersonalRelationFavorite} {
		result, err := personal.List(context.Background(), 7, 40, EntryAccess{Inventory: true}, relation, 1, 20)
		if err != nil || result.Total != 1 || len(result.Data) != 1 || result.Data[0].ID != entry.ID {
			t.Fatalf("List(%s) = %#v, %v", relation, result, err)
		}
	}
	hidden, err := personal.List(context.Background(), 7, 40, EntryAccess{}, PersonalRelationFavorite, 1, 20)
	if err != nil || hidden.Total != 0 || len(hidden.Data) != 0 {
		t.Fatalf("hidden personal entries = %#v, %v", hidden, err)
	}
}

func TestPersonalCatalogRejectsInvalidRelationAndInvisibleEntry(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, _ := createEditableCatalogEntry(t, db, 7)
	personal := NewPersonalCatalogService(db, NewEntryService(db, nil, nil))
	if _, err := personal.List(context.Background(), 7, 40, EntryAccess{Inventory: true}, "recent", 1, 20); err != ErrInvalidPersonalRelation {
		t.Fatalf("invalid relation error = %v", err)
	}
	if _, err := personal.GetMarks(context.Background(), 7, 40, EntryAccess{}, entry.ID); err != ErrEntryNotFound {
		t.Fatalf("invisible entry error = %v", err)
	}
}
