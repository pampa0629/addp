package service

import (
	"sort"
	"time"

	"github.com/addp/security/internal/models"
	"gorm.io/gorm"
)

type baselineBinding struct {
	SensitiveDataTypeID int64
	SecurityGradeID     int64
}

// recompileBaselineImpact resolves only Security-owned Finding and current
// Assessment dependencies. It does not call Meta, Catalog, or an engine.
func recompileBaselineImpact(tx *gorm.DB, tenantID int64, bindings []baselineBinding, now time.Time) error {
	enrollmentIDs := make(map[string]struct{})
	seenBindings := make(map[baselineBinding]struct{})
	for _, binding := range bindings {
		if binding.SensitiveDataTypeID <= 0 || binding.SecurityGradeID <= 0 {
			continue
		}
		if _, exists := seenBindings[binding]; exists {
			continue
		}
		seenBindings[binding] = struct{}{}

		var provisionalIDs []string
		if err := tx.Model(&models.ProtectionEnrollment{}).
			Distinct("security.protection_enrollments.id").
			Joins("JOIN security.sensitive_findings AS finding ON finding.tenant_id = security.protection_enrollments.tenant_id AND finding.enrollment_id = security.protection_enrollments.id AND finding.discovery_execution_id = security.protection_enrollments.latest_discovery_execution_id").
			Joins("JOIN security.sensitive_data_types AS data_type ON data_type.tenant_id = finding.tenant_id AND data_type.id = finding.sensitive_data_type_id").
			Joins("LEFT JOIN security.sensitive_finding_reviews AS review ON review.tenant_id = finding.tenant_id AND review.finding_id = finding.id").
			Where("security.protection_enrollments.tenant_id = ? AND security.protection_enrollments.state IN ? AND finding.sensitive_data_type_id = ? AND data_type.default_security_grade_id = ? AND review.id IS NULL", tenantID, liveEnrollmentStates(), binding.SensitiveDataTypeID, binding.SecurityGradeID).
			Pluck("security.protection_enrollments.id", &provisionalIDs).Error; err != nil {
			return err
		}
		for _, id := range provisionalIDs {
			enrollmentIDs[id] = struct{}{}
		}

		var formalIDs []string
		if err := tx.Model(&models.ProtectionEnrollment{}).
			Distinct("security.protection_enrollments.id").
			Joins("JOIN security.resource_security_assessments AS assessment ON assessment.tenant_id = security.protection_enrollments.tenant_id AND assessment.enrollment_id = security.protection_enrollments.id").
			Joins("JOIN security.resource_security_assessment_revisions AS revision ON revision.tenant_id = assessment.tenant_id AND revision.assessment_id = assessment.id AND revision.revision = assessment.current_revision").
			Where("security.protection_enrollments.tenant_id = ? AND security.protection_enrollments.state IN ? AND revision.conclusion = ? AND revision.sensitive_data_type_id = ? AND revision.security_grade_id = ?", tenantID, liveEnrollmentStates(), models.AssessmentConclusionSensitive, binding.SensitiveDataTypeID, binding.SecurityGradeID).
			Pluck("security.protection_enrollments.id", &formalIDs).Error; err != nil {
			return err
		}
		for _, id := range formalIDs {
			enrollmentIDs[id] = struct{}{}
		}
	}
	return compileDefinitionImpact(tx, tenantID, enrollmentIDs, now)
}

func recompileCandidateTypeImpact(tx *gorm.DB, tenantID, sensitiveDataTypeID int64, now time.Time) error {
	var ids []string
	if err := tx.Model(&models.ProtectionEnrollment{}).
		Distinct("security.protection_enrollments.id").
		Joins("JOIN security.sensitive_findings AS finding ON finding.tenant_id = security.protection_enrollments.tenant_id AND finding.enrollment_id = security.protection_enrollments.id AND finding.discovery_execution_id = security.protection_enrollments.latest_discovery_execution_id").
		Joins("LEFT JOIN security.sensitive_finding_reviews AS review ON review.tenant_id = finding.tenant_id AND review.finding_id = finding.id").
		Where("security.protection_enrollments.tenant_id = ? AND security.protection_enrollments.state IN ? AND finding.sensitive_data_type_id = ? AND review.id IS NULL", tenantID, liveEnrollmentStates(), sensitiveDataTypeID).
		Pluck("security.protection_enrollments.id", &ids).Error; err != nil {
		return err
	}
	enrollmentIDs := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		enrollmentIDs[id] = struct{}{}
	}
	return compileDefinitionImpact(tx, tenantID, enrollmentIDs, now)
}

func compileDefinitionImpact(tx *gorm.DB, tenantID int64, enrollmentIDs map[string]struct{}, now time.Time) error {
	ids := make([]string, 0, len(enrollmentIDs))
	for id := range enrollmentIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		var enrollment models.ProtectionEnrollment
		if err := tx.Where("tenant_id = ? AND id = ? AND state IN ?", tenantID, id, liveEnrollmentStates()).First(&enrollment).Error; err != nil {
			return err
		}
		if err := compileProtectionProjections(tx, enrollment, enrollment.LatestSourceSnapshotHash, now, allRequiredProtectionOwners()); err != nil {
			return err
		}
	}
	return nil
}

func liveEnrollmentStates() []string {
	return []string{models.EnrollmentStateEnrolling, models.EnrollmentStateActive}
}
