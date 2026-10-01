package repository

import (
	"errors"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Invoked by the existing PostgreSQL migration gate inside its owning transaction.
func assertBusinessResponsibilityEvidenceMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	var rows []models.Entry
	for _, tc := range []struct {
		name        string
		status      string
		role        string
		audit       commonModels.JSONMap
		otherTenant bool
		want        bool
	}{
		{name: "current owner", role: models.ResponsibilityRoleBusinessOwner, want: true},
		{name: "curated", status: models.GovernanceStatusCurated, want: true},
		{name: "certified", status: models.GovernanceStatusCertified, want: true},
		{name: "deprecated", status: models.GovernanceStatusDeprecated, want: true},
		{name: "withdrawn curation", audit: commonModels.JSONMap{"previous_governance_status": "curated", "governance_status": "discovered"}, want: true},
		{name: "explicit old owner", audit: commonModels.JSONMap{"responsibilities": []any{map[string]any{"role": "business_owner", "subject_type": "user", "subject_id": "40"}}}, want: true},
		{name: "count only", audit: commonModels.JSONMap{"responsibility_count": 3}},
		{name: "no history"},
		{name: "department only", role: models.ResponsibilityRoleAccountableDepartment},
		{name: "technical owner only", role: models.ResponsibilityRoleTechnicalOwner},
		{name: "malformed audit", audit: commonModels.JSONMap{"responsibilities": map[string]any{"role": "business_owner"}}},
		{name: "invalid owner ID", audit: commonModels.JSONMap{"responsibilities": []any{map[string]any{"role": "business_owner", "subject_type": "user", "subject_id": "0"}}}},
		{name: "other tenant audit", otherTenant: true, audit: commonModels.JSONMap{"governance_status": "curated"}},
		{name: "other tenant owner", otherTenant: true, role: models.ResponsibilityRoleBusinessOwner},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Microsecond)
			entry := models.Entry{ID: uuid.New(), TenantID: 77, EntryType: models.EntryTypeDataItem, EntryStatus: models.EntryStatusActive,
				GovernanceStatus: models.GovernanceStatusDiscovered, Visibility: models.VisibilityInventory, Version: 9,
				CreatedAt: now, UpdatedAt: now}
			if tc.status != "" {
				entry.GovernanceStatus = tc.status
			}
			if tc.status == models.GovernanceStatusCertified {
				entry.Visibility = models.VisibilityTenant
			}
			if err := db.Create(&entry).Error; err != nil {
				t.Fatal(err)
			}
			rows = append(rows, entry)
			evidenceTenant := entry.TenantID
			if tc.otherTenant {
				evidenceTenant++
			}
			if tc.role != "" {
				subjectType := models.ResponsibilitySubjectUser
				if tc.role == models.ResponsibilityRoleAccountableDepartment {
					subjectType = models.ResponsibilitySubjectDepartment
				}
				if err := db.Create(&models.Responsibility{ID: uuid.New(), TenantID: evidenceTenant, CatalogEntryID: entry.ID,
					Role: tc.role, SubjectType: subjectType, SubjectID: 40, Status: models.ResponsibilityStatusNeedsTransfer,
					ObservedSnapshot: commonModels.JSONMap{}, VerifiedAt: entry.CreatedAt}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if tc.audit != nil {
				if err := db.Create(&models.AuditEvent{ID: uuid.New(), TenantID: evidenceTenant, CatalogEntryID: entry.ID,
					EventType: "catalog.entry.updated", ActorType: "user", ActorID: "99", Details: tc.audit}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := Migrate(db); err != nil {
				t.Fatal(err)
			}
			var migrated models.Entry
			if err := db.First(&migrated, "id = ?", entry.ID).Error; err != nil {
				t.Fatal(err)
			}
			if tc.want && (migrated.BusinessResponsibilityEstablished == nil || !*migrated.BusinessResponsibilityEstablished) ||
				!tc.want && migrated.BusinessResponsibilityEstablished != nil {
				t.Fatalf("wrong legacy evidence: %#v", migrated)
			}
			if migrated.Version != entry.Version || !migrated.UpdatedAt.Equal(entry.UpdatedAt) {
				t.Fatal("historical backfill changed business version or timestamp")
			}
		})
	}
	var auditCount, projectionCount int64
	fresh := models.Entry{ID: uuid.New(), TenantID: 77, EntryType: models.EntryTypeDataItem, EntryStatus: models.EntryStatusActive,
		GovernanceStatus: models.GovernanceStatusDiscovered, Visibility: models.VisibilityInventory, Version: 1,
		BusinessResponsibilityEstablished: evidenceBool(false)}
	if err := db.Create(&fresh).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.AuditEvent{}).Count(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.ProjectionTask{}).Count(&projectionCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var unchangedFresh models.Entry
	if err := db.First(&unchangedFresh, "id = ?", fresh.ID).Error; err != nil {
		t.Fatal(err)
	}
	if unchangedFresh.BusinessResponsibilityEstablished == nil || *unchangedFresh.BusinessResponsibilityEstablished {
		t.Fatal("migration changed fresh negative evidence")
	}
	for _, original := range rows {
		var reloaded models.Entry
		if err := db.First(&reloaded, "id = ?", original.ID).Error; err != nil {
			t.Fatal(err)
		}
		if reloaded.Version != original.Version || !reloaded.UpdatedAt.Equal(original.UpdatedAt) {
			t.Fatal("repeated migration changed business version or timestamp")
		}
	}
	var afterAudit, afterProjection int64
	if err := db.Model(&models.AuditEvent{}).Count(&afterAudit).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.ProjectionTask{}).Count(&afterProjection).Error; err != nil {
		t.Fatal(err)
	}
	if auditCount != afterAudit || projectionCount != afterProjection {
		t.Fatal("migration created audit or projection facts")
	}
	// Exercise the actual pre-column upgrade, not only backfill with an already
	// migrated shape. Restore this disposable fixture after checking the result.
	if err := db.SavePoint("pre_evidence_column").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP TRIGGER preserve_business_responsibility_establishment ON catalog.entries").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE catalog.entries DROP COLUMN business_responsibility_established").Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("upgrade from old schema: %v", err)
	}
	for index, want := range map[int]bool{0: true, 7: false} {
		var upgraded models.Entry
		// The fixture deliberately drops/re-adds a column in this connection.
		// Select only the verified field so the prepared result shape is stable.
		if err := db.Select("business_responsibility_established").First(&upgraded, "id = ?", rows[index].ID).Error; err != nil {
			t.Fatal(err)
		}
		if want && (upgraded.BusinessResponsibilityEstablished == nil || !*upgraded.BusinessResponsibilityEstablished) ||
			!want && upgraded.BusinessResponsibilityEstablished != nil {
			t.Fatalf("old schema upgrade evidence: %#v", upgraded)
		}
	}
	if err := db.RollbackTo("pre_evidence_column").Error; err != nil {
		t.Fatal(err)
	}
	// Database enforcement rejects both erasing positive evidence and guessing
	// false from unknown. Savepoints let this gate check each rejected write.
	for _, change := range []struct {
		from *bool
		to   *bool
	}{
		{evidenceBool(true), evidenceBool(false)}, {evidenceBool(true), nil},
		{nil, evidenceBool(false)}, {evidenceBool(false), nil},
	} {
		entry := rows[0]
		entry.ID, entry.BusinessResponsibilityEstablished = uuid.New(), change.from
		if err := db.Create(&entry).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.SavePoint("evidence_reset").Error; err != nil {
			t.Fatal(err)
		}
		err := db.Model(&models.Entry{}).Where("id = ?", entry.ID).UpdateColumn("business_responsibility_established", change.to).Error
		var sqlError interface{ SQLState() string }
		if !errors.As(err, &sqlError) || sqlError.SQLState() != "23514" {
			t.Fatalf("reset not rejected by constraint: %v", err)
		}
		if err := db.RollbackTo("evidence_reset").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&models.Entry{}).Where("id = ?", entry.ID).UpdateColumn("business_responsibility_established", true).Error; err != nil {
			t.Fatalf("positive establishment rejected: %v", err)
		}
	}
}

func evidenceBool(value bool) *bool { return &value }
