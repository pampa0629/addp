package service

import (
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	commonClient "github.com/addp/common/client"
	"github.com/google/uuid"
)

func TestUnchangedResponsibilityReplacementPreservesIdentity(t *testing.T) {
	current := []models.Responsibility{
		{Role: "business_owner", SubjectType: "user", SubjectID: 8, Status: "active"},
		{Role: "data_steward", SubjectType: "user", SubjectID: 9, Status: "active"},
	}
	inputs := []ResponsibilityInput{{Role: "data_steward", SubjectType: "user", SubjectID: 9}, {Role: "business_owner", SubjectType: "user", SubjectID: 8}}
	if !sameActiveResponsibilities(current, inputs) {
		t.Fatal("reordering changed identity")
	}
	inputs[0].SubjectID++
	if sameActiveResponsibilities(current, inputs) {
		t.Fatal("changed subject treated as prose-only")
	}
	inputs[0].SubjectID--
	current[0].Status = "needs_transfer"
	if sameActiveResponsibilities(current, inputs) {
		t.Fatal("restoration of invalid responsibility skipped")
	}
}

func TestUnchangedResponsibilityReplacementKeepsRowsAndEstablishesHistory(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, _ := createEditableCatalogEntry(t, db, 7)
	row := models.Responsibility{ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID,
		Role: models.ResponsibilityRoleBusinessOwner, SubjectType: models.ResponsibilitySubjectUser,
		SubjectID: 8, Status: models.ResponsibilityStatusActive}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	inputs := []ResponsibilityInput{{Role: row.Role, SubjectType: row.SubjectType, SubjectID: row.SubjectID}}
	references := &validatedEntryReferences{system: map[string]commonClient.SystemCatalogReferenceResolution{
		"user:8": {Name: "Current business owner"},
	}}
	if err := replaceResponsibilities(db, 7, entry.ID, inputs, references, time.Now()); err != nil {
		t.Fatal(err)
	}
	var current []models.Responsibility
	if err := db.Where("catalog_entry_id = ?", entry.ID).Find(&current).Error; err != nil || len(current) != 1 || current[0].ID != row.ID {
		t.Fatalf("unchanged responsibility replaced: %+v err=%v", current, err)
	}
	if current[0].ObservedSnapshot["name"] != "Current business owner" || current[0].VerifiedAt.IsZero() {
		t.Fatalf("current owner facts were not refreshed: %+v", current[0])
	}
	assertBusinessResponsibilityEvidence(t, db, entry.ID, boolEvidence(true))
}
