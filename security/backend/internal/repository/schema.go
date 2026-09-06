package repository

import (
	"fmt"

	"github.com/addp/security/internal/models"
	"gorm.io/gorm"
)

func Migrate(db *gorm.DB) error {
	if db.Dialector.Name() == "sqlite" {
		if err := migrateSQLite(db); err != nil {
			return err
		}
		return migrateProtectionProjectionSchemaV2(db)
	}
	if db.Dialector.Name() == "postgres" {
		if err := db.Exec("CREATE SCHEMA IF NOT EXISTS security").Error; err != nil {
			return fmt.Errorf("create security schema: %w", err)
		}
		if db.Migrator().HasTable(&models.ProtectionExemption{}) && !db.Migrator().HasColumn(&models.ProtectionExemption{}, "subject_type") {
			if err := db.Exec("DROP TABLE IF EXISTS security.protection_exemption_revisions, security.protection_exemptions CASCADE").Error; err != nil {
				return fmt.Errorf("remove legacy tenant-wide protection exemptions: %w", err)
			}
		}
	}
	if err := db.AutoMigrate(
		&models.SecurityClassification{},
		&models.SecurityGrade{},
		&models.SensitiveDataType{},
		&models.Detector{},
		&models.ProtectionBaseline{},
		&models.ProtectionEnrollment{},
		&models.ProtectionProjectionRecord{},
		&models.ProtectionProjectionChange{},
		&models.ProtectionProjectionAcknowledgement{},
		&models.SensitiveFinding{},
		&models.SensitiveFindingReview{},
		&models.ResourceSecurityAssessment{},
		&models.ResourceSecurityAssessmentRevision{},
		&models.ProtectionPolicy{},
		&models.ProtectionPolicyRevision{},
		&models.ProtectionExemption{},
		&models.ProtectionExemptionRevision{},
		&models.ProtectionAccessRequest{},
	); err != nil {
		return fmt.Errorf("migrate security schema: %w", err)
	}
	if db.Dialector.Name() == "postgres" {
		statements := []string{
			`ALTER TABLE security.sensitive_data_types DROP COLUMN IF EXISTS protection_threshold`,
			`DROP INDEX IF EXISTS security.uq_security_live_enrollment_target`,
			`ALTER TABLE security.protection_enrollments DROP COLUMN IF EXISTS target_component`,
			`CREATE UNIQUE INDEX uq_security_live_enrollment_target ON security.protection_enrollments (tenant_id, target_owner, target_type, target_identity) WHERE state <> 'released'`,
			`CREATE INDEX IF NOT EXISTS idx_security_projection_changes_feed ON security.protection_projection_changes (tenant_id, consumer_owner, sequence)`,
			`CREATE INDEX IF NOT EXISTS idx_security_findings_enrollment ON security.sensitive_findings (tenant_id, enrollment_id, created_at DESC)`,
			`DROP INDEX IF EXISTS security.uq_security_finding_observation`,
			`CREATE UNIQUE INDEX uq_security_finding_observation ON security.sensitive_findings (tenant_id, enrollment_id, discovery_execution_id, component_key, detector_version, source_snapshot_hash)`,
			`CREATE INDEX IF NOT EXISTS idx_security_assessments_enrollment ON security.resource_security_assessments (tenant_id, enrollment_id, updated_at DESC)`,
			`ALTER TABLE security.resource_security_assessment_revisions ALTER COLUMN source_finding_id DROP NOT NULL`,
			`ALTER TABLE security.resource_security_assessment_revisions ALTER COLUMN source_review_id DROP NOT NULL`,
			`CREATE INDEX IF NOT EXISTS idx_security_policies_assessment ON security.protection_policies (tenant_id, assessment_id, updated_at DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_security_exemptions_assessment ON security.protection_exemptions (tenant_id, assessment_id, updated_at DESC)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS uq_security_pending_access_request ON security.protection_access_requests (tenant_id, assessment_id, consumer_owner, action, subject_type, subject_id) WHERE state = 'pending'`,
			`CREATE INDEX IF NOT EXISTS idx_security_access_requests_review_queue ON security.protection_access_requests (tenant_id, state, created_at DESC)`,
		}
		for _, statement := range statements {
			if err := db.Exec(statement).Error; err != nil {
				return fmt.Errorf("migrate security projection constraint: %w", err)
			}
		}
	}
	return migrateProtectionProjectionSchemaV2(db)
}

func migrateSQLite(db *gorm.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS security.security_classifications (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, code TEXT NOT NULL, name TEXT NOT NULL, description TEXT, parent_id INTEGER, sort_order INTEGER NOT NULL DEFAULT 0, version INTEGER NOT NULL DEFAULT 1, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME, UNIQUE (tenant_id, code))`,
		`CREATE TABLE IF NOT EXISTS security.security_grades (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, code TEXT NOT NULL, name TEXT NOT NULL, description TEXT, risk_order INTEGER NOT NULL, version INTEGER NOT NULL DEFAULT 1, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME, UNIQUE (tenant_id, code))`,
		`CREATE TABLE IF NOT EXISTS security.sensitive_data_types (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, code TEXT NOT NULL, name TEXT NOT NULL, description TEXT, security_classification_id INTEGER NOT NULL, default_security_grade_id INTEGER NOT NULL, version INTEGER NOT NULL DEFAULT 1, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME, UNIQUE (tenant_id, code))`,
		`CREATE TABLE IF NOT EXISTS security.detectors (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, capability_key TEXT NOT NULL, sensitive_data_type_id INTEGER NOT NULL, confidence_threshold REAL NOT NULL DEFAULT 0.9, enabled NUMERIC NOT NULL DEFAULT 1, version INTEGER NOT NULL DEFAULT 1, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME, UNIQUE (tenant_id, capability_key))`,
		`CREATE TABLE IF NOT EXISTS security.protection_baselines (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, sensitive_data_type_id INTEGER NOT NULL, security_grade_id INTEGER NOT NULL, effect TEXT NOT NULL, algorithm TEXT, keep_prefix INTEGER NOT NULL DEFAULT 0, keep_suffix INTEGER NOT NULL DEFAULT 0, invalid_value_effect TEXT NOT NULL DEFAULT 'suppress', enabled NUMERIC NOT NULL DEFAULT 1, version INTEGER NOT NULL DEFAULT 1, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME, UNIQUE (tenant_id, sensitive_data_type_id, security_grade_id))`,
		`CREATE TABLE IF NOT EXISTS security.protection_enrollments (id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, target_owner TEXT NOT NULL, target_type TEXT NOT NULL, target_identity TEXT NOT NULL, target_engine_id INTEGER NOT NULL, target_item_type TEXT NOT NULL, target_full_name TEXT NOT NULL, state TEXT NOT NULL, version INTEGER NOT NULL DEFAULT 1, release_reason TEXT NOT NULL DEFAULT '', release_basis TEXT NOT NULL DEFAULT '', release_requested_by INTEGER, release_requested_at DATETIME, release_source_snapshot_hash TEXT NOT NULL DEFAULT '', latest_source_snapshot_hash TEXT NOT NULL DEFAULT '', latest_discovery_execution_id TEXT NOT NULL DEFAULT '', last_discovered_at DATETIME, created_by INTEGER NOT NULL, released_at DATETIME, created_at DATETIME, updated_at DATETIME)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS security.uq_security_live_enrollment_target ON protection_enrollments (tenant_id, target_owner, target_type, target_identity) WHERE state <> 'released'`,
		`CREATE TABLE IF NOT EXISTS security.protection_projections (id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, enrollment_id TEXT NOT NULL, consumer_owner TEXT NOT NULL, revision TEXT NOT NULL, state TEXT NOT NULL, projection_payload TEXT NOT NULL, published_sequence INTEGER NOT NULL, release_sequence INTEGER, created_at DATETIME, updated_at DATETIME, UNIQUE (tenant_id, enrollment_id, consumer_owner))`,
		`CREATE TABLE IF NOT EXISTS security.protection_projection_changes (sequence INTEGER PRIMARY KEY AUTOINCREMENT, change_id TEXT NOT NULL UNIQUE, tenant_id INTEGER NOT NULL, enrollment_id TEXT NOT NULL, consumer_owner TEXT NOT NULL, operation TEXT NOT NULL, projection_id TEXT NOT NULL, revision TEXT NOT NULL, target_owner TEXT NOT NULL, target_type TEXT NOT NULL, target_identity TEXT NOT NULL, target_component TEXT NOT NULL DEFAULT '', projection_payload TEXT, created_at DATETIME)`,
		`CREATE INDEX IF NOT EXISTS security.idx_security_projection_changes_feed ON protection_projection_changes (tenant_id, consumer_owner, sequence)`,
		`CREATE TABLE IF NOT EXISTS security.protection_projection_acknowledgements (tenant_id INTEGER NOT NULL, consumer_owner TEXT NOT NULL, sequence INTEGER NOT NULL, applied_cursor TEXT NOT NULL, updated_at DATETIME, PRIMARY KEY (tenant_id, consumer_owner))`,
		`CREATE TABLE IF NOT EXISTS security.sensitive_findings (id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, enrollment_id TEXT NOT NULL, discovery_execution_id TEXT NOT NULL DEFAULT '', component_key TEXT NOT NULL, sensitive_data_type_id INTEGER NOT NULL, detector_code TEXT NOT NULL, detector_version TEXT NOT NULL, confidence REAL NOT NULL, evidence TEXT NOT NULL, component TEXT NOT NULL, source_snapshot_hash TEXT NOT NULL, observed_at DATETIME NOT NULL, created_at DATETIME NOT NULL, UNIQUE (tenant_id, enrollment_id, discovery_execution_id, component_key, detector_version, source_snapshot_hash))`,
		`CREATE INDEX IF NOT EXISTS security.idx_security_findings_enrollment ON sensitive_findings (tenant_id, enrollment_id, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS security.sensitive_finding_reviews (id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, finding_id TEXT NOT NULL, decision TEXT NOT NULL, sensitive_data_type_id INTEGER, security_grade_id INTEGER, rationale TEXT NOT NULL, reviewed_by INTEGER NOT NULL, created_at DATETIME NOT NULL, UNIQUE (tenant_id, finding_id))`,
		`CREATE TABLE IF NOT EXISTS security.resource_security_assessments (id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, enrollment_id TEXT NOT NULL, component_key TEXT NOT NULL, version INTEGER NOT NULL DEFAULT 1, current_revision INTEGER NOT NULL, created_by INTEGER NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL, UNIQUE (tenant_id, enrollment_id, component_key))`,
		`CREATE INDEX IF NOT EXISTS security.idx_security_assessments_enrollment ON resource_security_assessments (tenant_id, enrollment_id, updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS security.resource_security_assessment_revisions (id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, assessment_id TEXT NOT NULL, revision INTEGER NOT NULL, source_kind TEXT NOT NULL DEFAULT 'finding', conclusion TEXT NOT NULL DEFAULT 'sensitive', source_finding_id TEXT, source_review_id TEXT, sensitive_data_type_id INTEGER NOT NULL, security_classification_id INTEGER NOT NULL, security_grade_id INTEGER NOT NULL, source_snapshot_hash TEXT NOT NULL, component TEXT NOT NULL, rationale TEXT NOT NULL, created_by INTEGER NOT NULL, created_at DATETIME NOT NULL, UNIQUE (assessment_id, revision))`,
		`CREATE TABLE IF NOT EXISTS security.protection_policies (id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, assessment_id TEXT NOT NULL, consumer_owner TEXT NOT NULL, action TEXT NOT NULL, state TEXT NOT NULL, version INTEGER NOT NULL DEFAULT 1, current_revision INTEGER NOT NULL, created_by INTEGER NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL, UNIQUE (tenant_id, assessment_id, consumer_owner, action))`,
		`CREATE INDEX IF NOT EXISTS security.idx_security_policies_assessment ON protection_policies (tenant_id, assessment_id, updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS security.protection_policy_revisions (id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, policy_id TEXT NOT NULL, revision INTEGER NOT NULL, state TEXT NOT NULL, effect TEXT NOT NULL, rationale TEXT NOT NULL, created_by INTEGER NOT NULL, created_at DATETIME NOT NULL, UNIQUE (policy_id, revision))`,
		`CREATE TABLE IF NOT EXISTS security.protection_exemptions (id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, assessment_id TEXT NOT NULL, consumer_owner TEXT NOT NULL, action TEXT NOT NULL, subject_type TEXT NOT NULL, subject_id TEXT NOT NULL, state TEXT NOT NULL, version INTEGER NOT NULL DEFAULT 1, current_revision INTEGER NOT NULL, created_by INTEGER NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL, UNIQUE (tenant_id, assessment_id, consumer_owner, action, subject_type, subject_id))`,
		`CREATE INDEX IF NOT EXISTS security.idx_security_exemptions_assessment ON protection_exemptions (tenant_id, assessment_id, updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS security.protection_exemption_revisions (id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, exemption_id TEXT NOT NULL, revision INTEGER NOT NULL, assessment_revision INTEGER NOT NULL, source_request_id TEXT NOT NULL, state TEXT NOT NULL, expires_at DATETIME NOT NULL, rationale TEXT NOT NULL, created_by INTEGER NOT NULL, created_at DATETIME NOT NULL, UNIQUE (exemption_id, revision))`,
		`CREATE TABLE IF NOT EXISTS security.protection_access_requests (id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, assessment_id TEXT NOT NULL, assessment_revision INTEGER NOT NULL, consumer_owner TEXT NOT NULL, action TEXT NOT NULL, subject_type TEXT NOT NULL, subject_id TEXT NOT NULL, requested_expires_at DATETIME NOT NULL, rationale TEXT NOT NULL, state TEXT NOT NULL, version INTEGER NOT NULL DEFAULT 1, decided_by INTEGER, decided_at DATETIME, decision_rationale TEXT NOT NULL DEFAULT '', created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS security.uq_security_pending_access_request ON protection_access_requests (tenant_id, assessment_id, consumer_owner, action, subject_type, subject_id) WHERE state = 'pending'`,
		`CREATE INDEX IF NOT EXISTS security.idx_security_access_requests_review_queue ON protection_access_requests (tenant_id, state, created_at DESC)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("migrate security sqlite schema: %w", err)
		}
	}
	return nil
}
