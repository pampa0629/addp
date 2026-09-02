package service

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/security/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type protectionCandidate struct {
	AssessmentID        string
	SensitiveDataTypeID int64
	SecurityGradeID     int64
	Component           dataprotection.Component
	Formal              bool
}

// compileProtectionProjections is the only Security path that turns provisional
// Findings or formal Assessment revisions into executable owner changes.
func compileProtectionProjections(tx *gorm.DB, enrollment models.ProtectionEnrollment, sourceSnapshotHash string, now time.Time, consumerOwners []string) error {
	var findings []models.SensitiveFinding
	if err := tx.Where("tenant_id = ? AND enrollment_id = ? AND source_snapshot_hash = ?", enrollment.TenantID, enrollment.ID, sourceSnapshotHash).Order("component_key ASC, created_at ASC").Find(&findings).Error; err != nil {
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

	managerRules := make([]dataprotection.Rule, 0, len(candidates)*2)
	developRules := make([]dataprotection.Rule, 0, len(candidates))
	serviceRules := make([]dataprotection.Rule, 0, len(candidates))
	transferRules := make([]dataprotection.Rule, 0, len(candidates))
	for _, finding := range findings {
		candidate, exists := candidates[finding.ComponentKey]
		if !exists {
			continue
		}
		delete(candidates, finding.ComponentKey)
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
		managerRules = append(managerRules,
			dataprotection.Rule{Action: managerPreviewAction, Component: candidate.Component, Decision: managerDecision},
			dataprotection.Rule{Action: managerProfileAction, Component: candidate.Component, Decision: profileDecision},
		)
		developRules = append(developRules, dataprotection.Rule{Action: developQueryAction, Component: candidate.Component, Decision: baselineDecision})
		serviceRules = append(serviceRules, dataprotection.Rule{Action: serviceExecuteAction, Component: candidate.Component, Decision: baselineDecision})
		transferRules = append(transferRules, dataprotection.Rule{Action: transferExportAction, Component: candidate.Component, Decision: baselineDecision})
	}
	for _, consumerOwner := range consumerOwners {
		var rules []dataprotection.Rule
		switch consumerOwner {
		case "manager":
			rules = managerRules
		case "develop":
			rules = developRules
		case "service":
			rules = serviceRules
		case "transfer":
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
	err := tx.Where("tenant_id = ? AND enrollment_id = ? AND component_key = ?", enrollment.TenantID, enrollment.ID, finding.ComponentKey).First(&assessment).Error
	if err == nil {
		var revision models.ResourceSecurityAssessmentRevision
		if err := tx.Where("tenant_id = ? AND assessment_id = ? AND revision = ?", enrollment.TenantID, assessment.ID, assessment.CurrentRevision).First(&revision).Error; err != nil {
			return protectionCandidate{}, false, err
		}
		if revision.Component.SchemaFingerprint == finding.Component.SchemaFingerprint {
			return protectionCandidate{AssessmentID: assessment.ID, SensitiveDataTypeID: revision.SensitiveDataTypeID, SecurityGradeID: revision.SecurityGradeID, Component: finding.Component, Formal: true}, true, nil
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return protectionCandidate{}, false, err
	}

	var review models.SensitiveFindingReview
	if err := tx.Where("tenant_id = ? AND finding_id = ?", enrollment.TenantID, finding.ID).First(&review).Error; err == nil {
		return protectionCandidate{}, false, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return protectionCandidate{}, false, err
	}
	var dataType models.SensitiveDataType
	if err := tx.Where("tenant_id = ? AND id = ?", enrollment.TenantID, finding.SensitiveDataTypeID).First(&dataType).Error; err != nil {
		return protectionCandidate{}, false, err
	}
	if finding.Confidence < dataType.ProtectionThreshold {
		return protectionCandidate{}, false, nil
	}
	return protectionCandidate{SensitiveDataTypeID: dataType.ID, SecurityGradeID: dataType.DefaultSecurityGradeID, Component: finding.Component}, true, nil
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
	err := tx.Where("tenant_id = ? AND assessment_id = ? AND consumer_owner = ? AND action = ?", tenantID, assessmentID, "manager", managerPreviewAction).First(&policy).Error
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
		SchemaVersion: dataprotection.ProjectionSchemaV1, ProjectionID: record.ID, Revision: revision,
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
