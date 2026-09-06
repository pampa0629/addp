package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/security/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestProjectionSchemaV2MigrationPreservesFeedAndDropsTenantWideAllow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS security").Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	legacy := legacyProjectionV1{
		SchemaVersion: legacyProjectionSchemaV1,
		ProjectionID:  "11111111-1111-1111-1111-111111111111",
		Revision:      "00000000000000000003", ConsumerOwner: "manager", State: dataprotection.ProjectionStateActive,
		Target:             dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "sha256:target"},
		SourceSnapshotHash: "sha256:snapshot",
		Rules: []legacyProjectionRuleV1{{
			Action: "preview",
			Component: dataprotection.Component{
				Key: "phone", Path: []dataprotection.PathSegment{{Name: "phone", Container: "scalar"}},
				ValueType: "string", SchemaFingerprint: "sha256:schema",
			},
			Decision: legacyProjectionDecisionV1{
				Effect: dataprotection.EffectAllow, ValidUntil: timePointer(now.Add(time.Hour)),
				Fallback: &legacyProjectionDecisionV1{
					Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV1,
					Parameters: map[string]any{
						"prefix_runes": 3, "suffix_runes": 4, "replacement": "****",
						"exact_runes": 11, "character_class": "ascii_digit",
					},
					InvalidValueEffect: dataprotection.EffectSuppress,
				},
			},
		}},
		ValidFrom: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour),
	}
	payload, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	record := models.ProtectionProjectionRecord{
		ID: legacy.ProjectionID, TenantID: 7, EnrollmentID: "22222222-2222-2222-2222-222222222222",
		ConsumerOwner: "manager", Revision: legacy.Revision, State: legacy.State,
		ProjectionPayload: string(payload), PublishedSequence: 41, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	payloadText := string(payload)
	change := models.ProtectionProjectionChange{
		Sequence: 41, ChangeID: "33333333-3333-3333-3333-333333333333", TenantID: 7,
		EnrollmentID: record.EnrollmentID, ConsumerOwner: "manager", Operation: dataprotection.ChangeOperationUpsert,
		ProjectionID: record.ID, Revision: record.Revision, TargetOwner: "meta", TargetType: "data_item",
		TargetIdentity: "sha256:target", ProjectionPayload: &payloadText, CreatedAt: now,
	}
	if err := db.Create(&change).Error; err != nil {
		t.Fatal(err)
	}

	if err := migrateProtectionProjectionSchemaV2(db); err != nil {
		t.Fatal(err)
	}
	var migratedRecord models.ProtectionProjectionRecord
	if err := db.First(&migratedRecord, "id = ?", record.ID).Error; err != nil {
		t.Fatal(err)
	}
	var projection dataprotection.Projection
	if err := json.Unmarshal([]byte(migratedRecord.ProjectionPayload), &projection); err != nil {
		t.Fatal(err)
	}
	if err := projection.Validate(time.Time{}); err != nil {
		t.Fatal(err)
	}
	if projection.SchemaVersion != dataprotection.ProjectionSchemaV2 || projection.Rules[0].Decision.Effect != dataprotection.EffectMask || len(projection.Rules[0].Authorizations) != 0 {
		t.Fatalf("unexpected migrated projection: %#v", projection)
	}
	var migratedChange models.ProtectionProjectionChange
	if err := db.First(&migratedChange, "change_id = ?", change.ChangeID).Error; err != nil {
		t.Fatal(err)
	}
	if migratedChange.Sequence != 41 || migratedChange.ProjectionPayload == nil {
		t.Fatalf("feed identity changed: %#v", migratedChange)
	}
	var changeProjection dataprotection.Projection
	if err := json.Unmarshal([]byte(*migratedChange.ProjectionPayload), &changeProjection); err != nil {
		t.Fatal(err)
	}
	if err := changeProjection.Validate(time.Time{}); err != nil {
		t.Fatal(err)
	}
}

func timePointer(value time.Time) *time.Time { return &value }
