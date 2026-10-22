package repository

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/security/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const legacyProjectionSchemaV1 = "addp.protection_projection/v1"

type legacyProjectionDecisionV1 struct {
	Effect             string                      `json:"effect"`
	Algorithm          string                      `json:"algorithm,omitempty"`
	Parameters         map[string]any              `json:"parameters,omitempty"`
	InvalidValueEffect string                      `json:"invalid_value_effect,omitempty"`
	ValidUntil         *time.Time                  `json:"valid_until,omitempty"`
	Fallback           *legacyProjectionDecisionV1 `json:"fallback,omitempty"`
}

type legacyProjectionRuleV1 struct {
	Action    string                     `json:"action"`
	Component dataprotection.Component   `json:"component"`
	Decision  legacyProjectionDecisionV1 `json:"decision"`
}

type legacyProjectionV1 struct {
	SchemaVersion      string                           `json:"schema_version"`
	ProjectionID       string                           `json:"projection_id"`
	Revision           string                           `json:"revision"`
	ConsumerOwner      string                           `json:"consumer_owner"`
	State              string                           `json:"state"`
	Target             dataprotection.ResourceReference `json:"target"`
	SourceSnapshotHash string                           `json:"source_snapshot_hash"`
	Rules              []legacyProjectionRuleV1         `json:"rules"`
	ValidFrom          time.Time                        `json:"valid_from"`
	ExpiresAt          time.Time                        `json:"expires_at"`
}

func TestProjectionSchemaV5MigrationPreservesFeedAndDropsTenantWideAllow(t *testing.T) {
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
		Target:             dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: commonmodels.GenerateItemFingerprint(1, "business.people")},
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
					Effect: dataprotection.EffectMask, Algorithm: legacyKeepPrefixSuffixAlgorithmV1,
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
	persistProjectionEnrollment(t, db, record.EnrollmentID, record.TenantID, legacy.Target, "business.people")
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	payloadText := string(payload)
	change := models.ProtectionProjectionChange{
		Sequence: 41, ChangeID: "33333333-3333-3333-3333-333333333333", TenantID: 7,
		EnrollmentID: record.EnrollmentID, ConsumerOwner: "manager", Operation: dataprotection.ChangeOperationUpsert,
		ProjectionID: record.ID, Revision: record.Revision, TargetOwner: "meta", TargetType: "data_item",
		TargetIdentity: legacy.Target.ResourceIdentity, ProjectionPayload: &payloadText, CreatedAt: now,
	}
	if err := db.Create(&change).Error; err != nil {
		t.Fatal(err)
	}

	if err := migrateProtectionProjectionSchemaV5(db); err != nil {
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
	if projection.SchemaVersion != dataprotection.ProjectionSchemaV5 || projection.Rules[0].Decision.Effect != dataprotection.EffectMask || len(projection.Rules[0].Authorizations) != 0 {
		t.Fatalf("unexpected migrated projection: %#v", projection)
	}
	if projection.Rules[0].Decision.Algorithm != dataprotection.AlgorithmKeepPrefixSuffixV2 || projection.Rules[0].Decision.Parameters["mask_rune"] != "*" || len(projection.Rules[0].Decision.Parameters) != 3 {
		t.Fatalf("legacy mask algorithm was not migrated: %#v", projection.Rules[0].Decision)
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

func TestProjectionSchemaV5MigrationFailsClosedForNonCurrentPayloads(t *testing.T) {
	for name, testCase := range map[string]struct {
		payload  string
		expected string
	}{
		"missing schema": {payload: `{}`, expected: "schema_version is required"},
		"unknown schema": {payload: `{"schema_version":"addp.protection_projection/v9"}`, expected: "unsupported protection projection schema"},
	} {
		t.Run(name, func(t *testing.T) {
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
			record := models.ProtectionProjectionRecord{
				ID: "77777777-7777-7777-7777-777777777777", TenantID: 7,
				EnrollmentID: "88888888-8888-8888-8888-888888888888", ConsumerOwner: "manager",
				Revision: "00000000000000000001", State: dataprotection.ProjectionStateActive,
				ProjectionPayload: testCase.payload, PublishedSequence: 1,
			}
			if err := db.Create(&record).Error; err != nil {
				t.Fatal(err)
			}
			err = migrateProtectionProjectionSchemaV5(db)
			if err == nil || !strings.Contains(err.Error(), testCase.expected) {
				t.Fatalf("migrateProtectionProjectionSchemaV5() error=%v, want %q", err, testCase.expected)
			}
		})
	}
}

func TestKeepPrefixSuffixV2MigrationRewritesBaselineAndCurrentFeed(t *testing.T) {
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
	now := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	baseline := models.ProtectionBaseline{
		TenantID: 7, SensitiveDataTypeID: 11, SecurityGradeID: 13, Effect: dataprotection.EffectMask,
		Algorithm: legacyKeepPrefixSuffixAlgorithmV1, Parameters: map[string]any{"prefix_runes": 3, "suffix_runes": 4, "mask_rune": "*"}, AllowedAlgorithms: []string{dataprotection.AlgorithmKeepPrefixSuffixV2},
		InvalidValueEffect: dataprotection.EffectSuppress, Enabled: true, Version: 1, CreatedBy: 41,
	}
	if err := db.Create(&baseline).Error; err != nil {
		t.Fatal(err)
	}
	projection := dataprotection.Projection{
		TargetEngineID: 1,
		SchemaVersion:  dataprotection.ProjectionSchemaV5, ProjectionID: "44444444-4444-4444-4444-444444444444", Revision: "00000000000000000007",
		ConsumerOwner: "manager", State: dataprotection.ProjectionStateActive,
		Target:             dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "sha256:item"},
		SourceSnapshotHash: "sha256:snapshot",
		Rules: []dataprotection.Rule{{Action: "preview", Component: dataprotection.Component{
			Key: "email", Path: []dataprotection.PathSegment{{Name: "email", Container: "scalar"}}, ValueType: "string", SchemaFingerprint: "sha256:schema",
		}, Decision: dataprotection.Decision{
			Effect: dataprotection.EffectMask, Algorithm: legacyKeepPrefixSuffixAlgorithmV1, InvalidValueEffect: dataprotection.EffectSuppress,
			Parameters: map[string]any{"prefix_runes": 3, "suffix_runes": 4, "replacement": "****", "exact_runes": 11, "character_class": "ascii_digit"},
		}}}, ValidFrom: now.Add(-time.Hour), ExpiresAt: dataprotection.ProjectionDeadline(now.Add(time.Hour)),
	}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	record := models.ProtectionProjectionRecord{
		ID: projection.ProjectionID, TenantID: 7, EnrollmentID: "55555555-5555-5555-5555-555555555555", ConsumerOwner: "manager",
		Revision: projection.Revision, State: projection.State, ProjectionPayload: string(payload), PublishedSequence: 9, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	payloadText := string(payload)
	change := models.ProtectionProjectionChange{
		Sequence: 9, ChangeID: "66666666-6666-6666-6666-666666666666", TenantID: 7, EnrollmentID: record.EnrollmentID,
		ConsumerOwner: "manager", Operation: dataprotection.ChangeOperationUpsert, ProjectionID: record.ID, Revision: record.Revision,
		TargetOwner: "meta", TargetType: "data_item", TargetIdentity: "sha256:item", ProjectionPayload: &payloadText, CreatedAt: now,
	}
	if err := db.Create(&change).Error; err != nil {
		t.Fatal(err)
	}

	if err := migrateKeepPrefixSuffixAlgorithmV2(db); err != nil {
		t.Fatal(err)
	}
	var migratedBaseline models.ProtectionBaseline
	if err := db.First(&migratedBaseline, baseline.ID).Error; err != nil {
		t.Fatal(err)
	}
	if migratedBaseline.Algorithm != dataprotection.AlgorithmKeepPrefixSuffixV2 {
		t.Fatalf("migrated baseline algorithm = %q", migratedBaseline.Algorithm)
	}
	for _, payload := range migratedSecurityProjectionPayloads(t, db, record.ID, change.ChangeID) {
		var migrated dataprotection.Projection
		if err := json.Unmarshal([]byte(payload), &migrated); err != nil {
			t.Fatal(err)
		}
		if err := migrated.Validate(time.Time{}); err != nil {
			t.Fatal(err)
		}
		decision := migrated.Rules[0].Decision
		if decision.Algorithm != dataprotection.AlgorithmKeepPrefixSuffixV2 || decision.Parameters["mask_rune"] != "*" || len(decision.Parameters) != 3 {
			t.Fatalf("migrated decision = %#v", decision)
		}
	}
}

func migratedSecurityProjectionPayloads(t *testing.T, db *gorm.DB, recordID, changeID string) []string {
	t.Helper()
	var record models.ProtectionProjectionRecord
	if err := db.First(&record, "id = ?", recordID).Error; err != nil {
		t.Fatal(err)
	}
	var change models.ProtectionProjectionChange
	if err := db.First(&change, "change_id = ?", changeID).Error; err != nil {
		t.Fatal(err)
	}
	if change.ProjectionPayload == nil {
		t.Fatal("migrated change payload is nil")
	}
	return []string{record.ProjectionPayload, *change.ProjectionPayload}
}

func TestExistingEnrollmentValidityMigrationPreservesDeadline(t *testing.T) {
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
	assertExistingEnrollmentValidityMigration(t, db)
}

func assertExistingEnrollmentValidityMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	deadline := now.Add(time.Hour)
	row := models.ProtectionEnrollment{ID: uuid.NewString(), TenantID: 7, TargetOwner: "meta", TargetType: "data_item", TargetIdentity: commonmodels.GenerateItemFingerprint(1, "business.people"), TargetEngineID: 1, TargetFullName: "business.people", State: "active", Version: 3, CreatedBy: 11, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	ownerDeadlines := map[string]time.Time{}
	for index, owner := range []string{"manager", "develop", "service", "transfer"} {
		ownerDeadline := deadline.Add(time.Duration(index) * time.Minute)
		ownerDeadlines[owner] = ownerDeadline
		projection := dataprotection.Projection{
			TargetEngineID: 1, SchemaVersion: "addp.protection_projection/v2", ProjectionID: uuid.NewString(), Revision: "00000000000000000003", ConsumerOwner: owner, State: "enrolling", Target: row.Target(), Rules: []dataprotection.Rule{}, ValidFrom: now, ExpiresAt: &ownerDeadline}
		if err := projection.Seal(); err != nil {
			t.Fatal(err)
		}
		payload, _ := json.Marshal(projection)
		record := models.ProtectionProjectionRecord{ID: projection.ProjectionID, TenantID: 7, EnrollmentID: row.ID, ConsumerOwner: owner, Revision: projection.Revision, State: "enrolling", ProjectionPayload: string(payload), PublishedSequence: 1, CreatedAt: now, UpdatedAt: now}
		if err := db.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, column := range []string{"protection_expires_at", "validity_updated_by", "validity_updated_at"} {
		if err := db.Exec("ALTER TABLE security.protection_enrollments DROP COLUMN " + column).Error; err != nil {
			t.Fatal(err)
		}
	}
	for repeat := 0; repeat < 2; repeat++ {
		if err := Migrate(db); err != nil {
			t.Fatal(err)
		}
		var migrated models.ProtectionEnrollment
		if err := db.First(&migrated, "id = ?", row.ID).Error; err != nil {
			t.Fatal(err)
		}
		if migrated.ProtectionExpiresAt == nil || !migrated.ProtectionExpiresAt.Equal(deadline) || migrated.Version != row.Version {
			t.Fatal("migration silently renewed an existing resource")
		}
		var records []models.ProtectionProjectionRecord
		if err := db.Where("enrollment_id = ?", row.ID).Find(&records).Error; err != nil {
			t.Fatal(err)
		}
		if len(records) != len(ownerDeadlines) {
			t.Fatal("migration changed the Owner projection set")
		}
		for _, record := range records {
			var projection dataprotection.Projection
			if err := json.Unmarshal([]byte(record.ProjectionPayload), &projection); err != nil {
				t.Fatal(err)
			}
			if err := projection.Validate(time.Time{}); err != nil {
				t.Fatal(err)
			}
			if projection.SchemaVersion != dataprotection.ProjectionSchemaV5 || projection.ExpiresAt == nil || !projection.ExpiresAt.Equal(ownerDeadlines[record.ConsumerOwner]) || projection.Revision != "00000000000000000003" || record.PublishedSequence != 1 {
				t.Fatal("migration changed an Owner deadline, revision or feed position")
			}
		}
	}
}

func persistProjectionEnrollment(t *testing.T, db *gorm.DB, id string, tenantID int64, target dataprotection.ResourceReference, fullName string) {
	t.Helper()
	row := models.ProtectionEnrollment{ID: id, TenantID: tenantID, TargetOwner: target.OwnerModule, TargetType: target.ResourceType, TargetIdentity: target.ResourceIdentity, TargetEngineID: 1, TargetFullName: fullName, State: "active", Version: 1, CreatedBy: 11}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
}

func TestProjectionV4MigrationRejectsMissingOrInconsistentEnginePreimage(t *testing.T) {
	for _, kind := range []string{"missing", "wrong_engine", "wrong_name"} {
		t.Run(kind, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			pool, _ := db.DB()
			defer pool.Close()
			if err := db.Exec("ATTACH DATABASE ':memory:' AS security").Error; err != nil {
				t.Fatal(err)
			}
			if err := Migrate(db); err != nil {
				t.Fatal(err)
			}
			target := dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: commonmodels.GenerateItemFingerprint(1, "business.people")}
			now := time.Now().UTC()
			projection := dataprotection.Projection{SchemaVersion: "addp.protection_projection/v3", ProjectionID: uuid.NewString(), Revision: "00000000000000000001", ConsumerOwner: "develop", State: dataprotection.ProjectionStateEnrolling, Target: target, ValidFrom: now}
			_ = projection.Seal()
			payload, _ := json.Marshal(projection)
			enrollmentID := uuid.NewString()
			if kind != "missing" {
				persistProjectionEnrollment(t, db, enrollmentID, 7, target, "business.people")
				updates := map[string]interface{}{"target_engine_id": 2}
				if kind == "wrong_name" {
					updates = map[string]interface{}{"target_full_name": "business.other"}
				}
				if err := db.Model(&models.ProtectionEnrollment{}).Where("id = ?", enrollmentID).Updates(updates).Error; err != nil {
					t.Fatal(err)
				}
			}
			record := models.ProtectionProjectionRecord{ID: projection.ProjectionID, TenantID: 7, EnrollmentID: enrollmentID, ConsumerOwner: "develop", Revision: projection.Revision, State: projection.State, ProjectionPayload: string(payload), PublishedSequence: 1}
			if err := db.Create(&record).Error; err != nil {
				t.Fatal(err)
			}
			if err := migrateProtectionProjectionSchemaV5(db); err == nil {
				t.Fatal("untrusted engine preimage was migrated")
			}
			var persisted models.ProtectionProjectionRecord
			if err := db.First(&persisted, "id = ?", record.ID).Error; err != nil {
				t.Fatal(err)
			}
			if persisted.ProjectionPayload != string(payload) {
				t.Fatal("failed migration changed payload")
			}
		})
	}
}
