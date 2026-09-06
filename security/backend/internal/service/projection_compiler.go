package service

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/security/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type protectionCandidate struct {
	AssessmentID             string
	AssessmentRevision       int64
	SensitiveDataTypeID      int64
	SecurityClassificationID int64
	SecurityGradeID          int64
	Component                dataprotection.Component
	Formal                   bool
}

// compileProtectionProjections is the only Security path that turns provisional
// Findings or formal Assessment revisions into executable owner changes.
func compileProtectionProjections(tx *gorm.DB, enrollment models.ProtectionEnrollment, sourceSnapshotHash string, now time.Time, consumerOwners []string) error {
	var findings []models.SensitiveFinding
	if err := tx.Where("tenant_id = ? AND enrollment_id = ? AND discovery_execution_id = ?", enrollment.TenantID, enrollment.ID, enrollment.LatestDiscoveryExecutionID).Order("component_key ASC, created_at ASC").Find(&findings).Error; err != nil {
		return err
	}
	candidates := make(map[string]protectionCandidate, len(findings))
	for _, finding := range findings {
		candidate, include, err := resolveProtectionCandidate(tx, enrollment, finding)
		if err != nil {
			return err
		}
		if !include {
			continue
		}
		current, exists := candidates[finding.ComponentKey]
		if !exists || candidate.Formal && !current.Formal {
			candidates[finding.ComponentKey] = candidate
		}
	}
	var assessments []models.ResourceSecurityAssessment
	if err := tx.Where("tenant_id = ? AND enrollment_id = ?", enrollment.TenantID, enrollment.ID).Find(&assessments).Error; err != nil {
		return err
	}
	if len(assessments) > 0 {
		assessmentByID := make(map[string]models.ResourceSecurityAssessment, len(assessments))
		assessmentIDs := make([]string, 0, len(assessments))
		for _, assessment := range assessments {
			assessmentByID[assessment.ID] = assessment
			assessmentIDs = append(assessmentIDs, assessment.ID)
		}
		var revisions []models.ResourceSecurityAssessmentRevision
		if err := tx.Where("tenant_id = ? AND assessment_id IN ?", enrollment.TenantID, assessmentIDs).Find(&revisions).Error; err != nil {
			return err
		}
		for _, revision := range revisions {
			assessment, exists := assessmentByID[revision.AssessmentID]
			if !exists || assessment.CurrentRevision != revision.Revision || revision.Conclusion != models.AssessmentConclusionSensitive {
				continue
			}
			candidates[assessment.ComponentKey] = protectionCandidate{
				AssessmentID: assessment.ID, AssessmentRevision: revision.Revision, SensitiveDataTypeID: revision.SensitiveDataTypeID,
				SecurityClassificationID: revision.SecurityClassificationID, SecurityGradeID: revision.SecurityGradeID,
				Component: revision.Component, Formal: true,
			}
		}
	}

	managerRules := make([]dataprotection.Rule, 0, len(candidates)*2)
	developRules := make([]dataprotection.Rule, 0, len(candidates))
	serviceRules := make([]dataprotection.Rule, 0, len(candidates))
	transferRules := make([]dataprotection.Rule, 0, len(candidates))
	componentKeys := make([]string, 0, len(candidates))
	for componentKey := range candidates {
		componentKeys = append(componentKeys, componentKey)
	}
	sort.Strings(componentKeys)
	for _, componentKey := range componentKeys {
		candidate := candidates[componentKey]
		var baseline models.ProtectionBaseline
		if err := tx.Where("tenant_id = ? AND sensitive_data_type_id = ? AND security_grade_id = ? AND enabled = ?", enrollment.TenantID, candidate.SensitiveDataTypeID, candidate.SecurityGradeID, true).First(&baseline).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return err
		}
		baselineDecision, err := protectionDecisionFromBaseline(baseline)
		if err != nil {
			return err
		}
		managerDecision := baselineDecision
		if candidate.AssessmentID != "" && candidate.Component.Key != dataprotection.DocumentTextComponentKey {
			managerDecision, err = applyManagerPolicy(tx, enrollment.TenantID, candidate.AssessmentID, baselineDecision)
			if err != nil {
				return err
			}
		}
		if candidate.Component.Key == dataprotection.DocumentTextComponentKey {
			searchDecision, err := managerSearchIndexDecision(managerDecision)
			if err != nil {
				return err
			}
			managerRules = append(managerRules, dataprotection.Rule{Action: managerSearchIndexAction, Component: candidate.Component, Decision: searchDecision})
			continue
		}
		profileDecision, err := managerProfileDecision(managerDecision)
		if err != nil {
			return err
		}
		managerAuthorizations, err := protectionAuthorizations(tx, enrollment.TenantID, candidate.AssessmentID, candidate.AssessmentRevision, managerProtectionOwner, managerPreviewAction, now)
		if err != nil {
			return err
		}
		managerRules = append(managerRules,
			dataprotection.Rule{Action: managerPreviewAction, Component: candidate.Component, Decision: managerDecision, Authorizations: managerAuthorizations},
			dataprotection.Rule{Action: managerProfileAction, Component: candidate.Component, Decision: profileDecision},
		)
		developRules = append(developRules, dataprotection.Rule{Action: developQueryAction, Component: candidate.Component, Decision: baselineDecision})
		serviceRules = append(serviceRules, dataprotection.Rule{Action: serviceExecuteAction, Component: candidate.Component, Decision: baselineDecision})
		transferRules = append(transferRules, dataprotection.Rule{Action: transferExportAction, Component: candidate.Component, Decision: baselineDecision})
	}
	for _, consumerOwner := range consumerOwners {
		var rules []dataprotection.Rule
		switch consumerOwner {
		case managerProtectionOwner:
			rules = managerRules
		case developProtectionOwner:
			rules = developRules
		case serviceProtectionOwner:
			rules = serviceRules
		case transferProtectionOwner:
			rules = transferRules
		default:
			return errors.New("unsupported field-level protection consumer owner")
		}
		if err := publishProtectionProjection(tx, enrollment, consumerOwner, sourceSnapshotHash, rules, now); err != nil {
			return err
		}
	}
	return nil
}

func managerSearchIndexDecision(baseline dataprotection.Decision) (dataprotection.Decision, error) {
	switch baseline.Effect {
	case dataprotection.EffectMask:
		baseline.Algorithm = dataprotection.AlgorithmPhoneOccurrencesV1
		return baseline, nil
	case dataprotection.EffectSuppress, dataprotection.EffectDeny:
		return baseline, nil
	default:
		return dataprotection.Decision{}, errors.New("manager baseline decision cannot be compiled for content search")
	}
}

func managerProfileDecision(preview dataprotection.Decision) (dataprotection.Decision, error) {
	switch preview.Effect {
	case dataprotection.EffectMask, dataprotection.EffectSuppress:
		return dataprotection.Decision{Effect: dataprotection.EffectSuppress, InvalidValueEffect: dataprotection.EffectSuppress}, nil
	case dataprotection.EffectDeny:
		return dataprotection.Decision{Effect: dataprotection.EffectDeny, InvalidValueEffect: dataprotection.EffectDeny}, nil
	default:
		return dataprotection.Decision{}, errors.New("manager preview decision cannot be compiled for data profiling")
	}
}

func resolveProtectionCandidate(tx *gorm.DB, enrollment models.ProtectionEnrollment, finding models.SensitiveFinding) (protectionCandidate, bool, error) {
	var assessment models.ResourceSecurityAssessment
	var assessmentRevision models.ResourceSecurityAssessmentRevision
	var assessmentPointer *models.ResourceSecurityAssessment
	var revisionPointer *models.ResourceSecurityAssessmentRevision
	err := tx.Where("tenant_id = ? AND enrollment_id = ? AND component_key = ?", enrollment.TenantID, enrollment.ID, finding.ComponentKey).First(&assessment).Error
	if err == nil {
		if err := tx.Where("tenant_id = ? AND assessment_id = ? AND revision = ?", enrollment.TenantID, assessment.ID, assessment.CurrentRevision).First(&assessmentRevision).Error; err != nil {
			return protectionCandidate{}, false, err
		}
		assessmentPointer = &assessment
		revisionPointer = &assessmentRevision
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return protectionCandidate{}, false, err
	}

	var review models.SensitiveFindingReview
	var reviewPointer *models.SensitiveFindingReview
	if err := tx.Where("tenant_id = ? AND finding_id = ?", enrollment.TenantID, finding.ID).First(&review).Error; err == nil {
		reviewPointer = &review
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return protectionCandidate{}, false, err
	}
	if reviewPointer != nil || (revisionPointer != nil && revisionPointer.Component.SchemaFingerprint == finding.Component.SchemaFingerprint) {
		candidate, include, _, _ := resolveProtectionCandidateFromFacts(finding, assessmentPointer, revisionPointer, reviewPointer, nil, nil)
		return candidate, include, nil
	}
	var dataType models.SensitiveDataType
	if err := tx.Where("tenant_id = ? AND id = ?", enrollment.TenantID, finding.SensitiveDataTypeID).First(&dataType).Error; err != nil {
		return protectionCandidate{}, false, err
	}
	var detector models.Detector
	if err := tx.Where(
		"tenant_id = ? AND capability_key = ? AND sensitive_data_type_id = ? AND enabled = ?",
		enrollment.TenantID, finding.DetectorVersion, finding.SensitiveDataTypeID, true,
	).First(&detector).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		candidate, include, _, _ := resolveProtectionCandidateFromFacts(finding, assessmentPointer, revisionPointer, reviewPointer, &dataType, nil)
		return candidate, include, nil
	} else if err != nil {
		return protectionCandidate{}, false, err
	}
	candidate, include, _, _ := resolveProtectionCandidateFromFacts(finding, assessmentPointer, revisionPointer, reviewPointer, &dataType, &detector)
	return candidate, include, nil
}

func resolveProtectionCandidateFromFacts(
	finding models.SensitiveFinding,
	assessment *models.ResourceSecurityAssessment,
	revision *models.ResourceSecurityAssessmentRevision,
	review *models.SensitiveFindingReview,
	dataType *models.SensitiveDataType,
	detector *models.Detector,
) (protectionCandidate, bool, string, string) {
	if assessment != nil && revision != nil && revision.Component.SchemaFingerprint == finding.Component.SchemaFingerprint {
		if revision.Conclusion == models.AssessmentConclusionNotSensitive {
			return protectionCandidate{}, false, models.FindingDecisionRevoked, models.FindingGovernanceAssessment
		}
		return protectionCandidate{
			AssessmentID: assessment.ID, AssessmentRevision: revision.Revision, SensitiveDataTypeID: revision.SensitiveDataTypeID,
			SecurityClassificationID: revision.SecurityClassificationID, SecurityGradeID: revision.SecurityGradeID,
			Component: finding.Component, Formal: true,
		}, true, models.FindingDecisionFormal, models.FindingGovernanceAssessment
	}
	if review != nil {
		if review.Decision == models.FindingReviewDecisionReject {
			return protectionCandidate{}, false, models.FindingDecisionRejected, ""
		}
		return protectionCandidate{}, false, models.FindingDecisionSuperseded, ""
	}
	if dataType == nil || detector == nil || !detector.Enabled || detector.CapabilityKey != finding.DetectorVersion || detector.SensitiveDataTypeID != finding.SensitiveDataTypeID {
		return protectionCandidate{}, false, models.FindingDecisionDetectorInactive, ""
	}
	if finding.Confidence < detector.ConfidenceThreshold {
		return protectionCandidate{}, false, models.FindingDecisionAwaitingReview, ""
	}
	return protectionCandidate{
		SensitiveDataTypeID: dataType.ID, SecurityClassificationID: dataType.SecurityClassificationID,
		SecurityGradeID: dataType.DefaultSecurityGradeID, Component: finding.Component,
	}, true, models.FindingDecisionAutomatic, models.FindingGovernanceDetectorDefault
}

func protectionDecisionFromBaseline(baseline models.ProtectionBaseline) (dataprotection.Decision, error) {
	switch baseline.Effect {
	case dataprotection.EffectMask:
		if baseline.Algorithm != dataprotection.AlgorithmKeepPrefixSuffixV1 || baseline.KeepPrefix < 0 || baseline.KeepSuffix < 0 || baseline.KeepPrefix+baseline.KeepSuffix >= 11 {
			return dataprotection.Decision{}, errors.New("phone protection baseline is invalid")
		}
		return dataprotection.Decision{
			Effect: baseline.Effect, Algorithm: baseline.Algorithm, InvalidValueEffect: baseline.InvalidValueEffect,
			Parameters: map[string]any{"prefix_runes": baseline.KeepPrefix, "suffix_runes": baseline.KeepSuffix, "replacement": strings.Repeat("*", 11-baseline.KeepPrefix-baseline.KeepSuffix), "exact_runes": 11, "character_class": "ascii_digit"},
		}, nil
	case dataprotection.EffectSuppress:
		return dataprotection.Decision{Effect: dataprotection.EffectSuppress, InvalidValueEffect: dataprotection.EffectSuppress}, nil
	case dataprotection.EffectDeny:
		return dataprotection.Decision{Effect: dataprotection.EffectDeny, InvalidValueEffect: dataprotection.EffectDeny}, nil
	default:
		return dataprotection.Decision{}, errors.New("phone protection baseline is invalid")
	}
}

func applyManagerPolicy(tx *gorm.DB, tenantID int64, assessmentID string, baseline dataprotection.Decision) (dataprotection.Decision, error) {
	var policy models.ProtectionPolicy
	err := tx.Where("tenant_id = ? AND assessment_id = ? AND consumer_owner = ? AND action = ?", tenantID, assessmentID, managerProtectionOwner, managerPreviewAction).First(&policy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && policy.State == models.ProtectionPolicyStateRevoked) {
		return baseline, nil
	}
	if err != nil {
		return dataprotection.Decision{}, err
	}
	var revision models.ProtectionPolicyRevision
	if err := tx.Where("tenant_id = ? AND policy_id = ? AND revision = ?", tenantID, policy.ID, policy.CurrentRevision).First(&revision).Error; err != nil {
		return dataprotection.Decision{}, err
	}
	if revision.State != models.ProtectionPolicyStateActive || protectionEffectRank(revision.Effect) < 0 {
		return dataprotection.Decision{}, errors.New("protection policy is invalid")
	}
	if protectionEffectRank(revision.Effect) < protectionEffectRank(baseline.Effect) {
		return baseline, nil
	}
	if revision.Effect == dataprotection.EffectMask {
		return baseline, nil
	}
	return dataprotection.Decision{Effect: revision.Effect, InvalidValueEffect: revision.Effect}, nil
}

func protectionAuthorizations(tx *gorm.DB, tenantID int64, assessmentID string, assessmentRevision int64, consumerOwner, action string, now time.Time) ([]dataprotection.TemporaryAuthorization, error) {
	if assessmentID == "" {
		return nil, nil
	}
	var exemptions []models.ProtectionExemption
	if err := tx.Where("tenant_id = ? AND assessment_id = ? AND consumer_owner = ? AND action = ? AND state = ?", tenantID, assessmentID, consumerOwner, action, models.ProtectionExemptionStateActive).Order("subject_type ASC, subject_id ASC").Find(&exemptions).Error; err != nil {
		return nil, err
	}
	authorizations := make([]dataprotection.TemporaryAuthorization, 0, len(exemptions))
	for _, exemption := range exemptions {
		var revision models.ProtectionExemptionRevision
		if err := tx.Where("tenant_id = ? AND exemption_id = ? AND revision = ?", tenantID, exemption.ID, exemption.CurrentRevision).First(&revision).Error; err != nil {
			return nil, err
		}
		if revision.State != models.ProtectionExemptionStateActive || revision.AssessmentRevision != assessmentRevision || !now.Before(revision.ExpiresAt) {
			continue
		}
		authorizations = append(authorizations, dataprotection.TemporaryAuthorization{
			Subject: dataprotection.SubjectReference{Type: exemption.SubjectType, ID: exemption.SubjectID},
			Effect:  dataprotection.EffectAllow, ValidFrom: now, ValidUntil: revision.ExpiresAt.UTC(),
		})
	}
	return authorizations, nil
}

func protectionEffectRank(effect string) int {
	switch effect {
	case dataprotection.EffectMask:
		return 1
	case dataprotection.EffectSuppress:
		return 2
	case dataprotection.EffectDeny:
		return 3
	default:
		return -1
	}
}

func publishProtectionProjection(tx *gorm.DB, enrollment models.ProtectionEnrollment, consumerOwner, sourceSnapshotHash string, rules []dataprotection.Rule, now time.Time) error {
	var record models.ProtectionProjectionRecord
	query := tx
	if tx.Dialector.Name() == "postgres" {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.Where("tenant_id = ? AND enrollment_id = ? AND consumer_owner = ?", enrollment.TenantID, enrollment.ID, consumerOwner).First(&record).Error; err != nil {
		return err
	}
	revision, err := nextRevision(record.Revision)
	if err != nil {
		return err
	}
	state := dataprotection.ProjectionStateActive
	if len(rules) == 0 {
		state = dataprotection.ProjectionStateEnrolling
		sourceSnapshotHash = ""
	}
	projection := dataprotection.Projection{
		SchemaVersion: dataprotection.ProjectionSchemaV2, ProjectionID: record.ID, Revision: revision,
		ConsumerOwner: consumerOwner, State: state, Target: enrollment.Target(),
		SourceSnapshotHash: sourceSnapshotHash, Rules: rules, ValidFrom: now, ExpiresAt: now.Add(365 * 24 * time.Hour),
	}
	if err := projection.Seal(); err != nil {
		return err
	}
	payload, err := json.Marshal(projection)
	if err != nil {
		return err
	}
	payloadText := string(payload)
	change := models.ProtectionProjectionChange{
		ChangeID: uuid.NewString(), TenantID: enrollment.TenantID, EnrollmentID: enrollment.ID,
		ConsumerOwner: consumerOwner, Operation: dataprotection.ChangeOperationUpsert,
		ProjectionID: record.ID, Revision: revision, TargetOwner: enrollment.TargetOwner,
		TargetType: enrollment.TargetType, TargetIdentity: enrollment.TargetIdentity,
		ProjectionPayload: &payloadText, CreatedAt: now,
	}
	if err := tx.Create(&change).Error; err != nil {
		return err
	}
	return tx.Model(&record).Updates(map[string]interface{}{
		"revision": revision, "state": state,
		"projection_payload": payloadText, "published_sequence": change.Sequence, "updated_at": now,
	}).Error
}
