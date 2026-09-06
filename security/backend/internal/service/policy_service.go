package service

import (
	"context"
	"errors"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataprotection"
	"github.com/addp/security/internal/models"
	"github.com/addp/security/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PolicyService struct {
	db  *gorm.DB
	now func() time.Time
}

func NewPolicyService(db *gorm.DB) *PolicyService {
	return &PolicyService{db: db, now: time.Now}
}

func (s *PolicyService) Create(ctx context.Context, tenantID, userID int64, request models.CreateProtectionPolicyRequest) (*models.ProtectionPolicyResponse, error) {
	request.AssessmentID = strings.TrimSpace(request.AssessmentID)
	request.ConsumerOwner = strings.TrimSpace(request.ConsumerOwner)
	request.Action = strings.TrimSpace(request.Action)
	request.Effect = strings.TrimSpace(request.Effect)
	request.Rationale = strings.TrimSpace(request.Rationale)
	if tenantID <= 0 || userID <= 0 || uuid.Validate(request.AssessmentID) != nil || request.ConsumerOwner != managerProtectionOwner || request.Action != managerPreviewAction || !validPolicyEffect(request.Effect) || !validPolicyRationale(request.Rationale) {
		return nil, commonapi.ErrBadRequest
	}

	var result *models.ProtectionPolicyResponse
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		assessment, current, enrollment, baseline, err := policyDependencies(tx, tenantID, request.AssessmentID)
		if err != nil {
			return err
		}
		if protectionEffectRank(request.Effect) < protectionEffectRank(baseline.Effect) {
			return commonapi.ErrBadRequest
		}
		now := s.now().UTC()
		policy := models.ProtectionPolicy{
			ID: uuid.NewString(), TenantID: tenantID, AssessmentID: assessment.ID,
			ConsumerOwner: request.ConsumerOwner, Action: request.Action,
			State: models.ProtectionPolicyStateActive, Version: 1, CurrentRevision: 1,
			CreatedBy: userID, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&policy).Error; err != nil {
			return policyDBError(err)
		}
		revision := models.ProtectionPolicyRevision{
			ID: uuid.NewString(), TenantID: tenantID, PolicyID: policy.ID, Revision: 1,
			State: models.ProtectionPolicyStateActive, Effect: request.Effect,
			Rationale: request.Rationale, CreatedBy: userID, CreatedAt: now,
		}
		if err := tx.Create(&revision).Error; err != nil {
			return policyDBError(err)
		}
		if err := compileProtectionProjections(tx, enrollment, enrollmentSnapshotHash(enrollment, current.SourceSnapshotHash), now, []string{"manager"}); err != nil {
			return err
		}
		result = &models.ProtectionPolicyResponse{ProtectionPolicy: policy, Current: revision, History: []models.ProtectionPolicyRevision{revision}}
		return nil
	})
	return result, err
}

func (s *PolicyService) Update(ctx context.Context, tenantID, userID int64, policyID string, request models.UpdateProtectionPolicyRequest) (*models.ProtectionPolicyResponse, error) {
	request.Effect = strings.TrimSpace(request.Effect)
	request.Rationale = strings.TrimSpace(request.Rationale)
	if tenantID <= 0 || userID <= 0 || uuid.Validate(policyID) != nil || request.Version <= 0 || !validPolicyEffect(request.Effect) || !validPolicyRationale(request.Rationale) {
		return nil, commonapi.ErrBadRequest
	}
	return s.appendRevision(ctx, tenantID, userID, policyID, request.Version, models.ProtectionPolicyStateActive, request.Effect, request.Rationale)
}

func (s *PolicyService) Revoke(ctx context.Context, tenantID, userID int64, policyID string, request models.RevokeProtectionPolicyRequest) (*models.ProtectionPolicyResponse, error) {
	request.Rationale = strings.TrimSpace(request.Rationale)
	if tenantID <= 0 || userID <= 0 || uuid.Validate(policyID) != nil || request.Version <= 0 || !validPolicyRationale(request.Rationale) {
		return nil, commonapi.ErrBadRequest
	}
	return s.appendRevision(ctx, tenantID, userID, policyID, request.Version, models.ProtectionPolicyStateRevoked, "", request.Rationale)
}

func (s *PolicyService) appendRevision(ctx context.Context, tenantID, userID int64, policyID string, version int64, state, effect, rationale string) (*models.ProtectionPolicyResponse, error) {
	var result *models.ProtectionPolicyResponse
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var policy models.ProtectionPolicy
		query := tx
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.Where("tenant_id = ? AND id = ?", tenantID, policyID).First(&policy).Error; err != nil {
			return policyDBError(err)
		}
		if policy.Version != version {
			return repository.ErrVersionConflict
		}
		if state == models.ProtectionPolicyStateRevoked && policy.State == models.ProtectionPolicyStateRevoked {
			return commonapi.ErrConflict
		}
		assessment, current, enrollment, baseline, err := policyDependencies(tx, tenantID, policy.AssessmentID)
		if err != nil {
			return err
		}
		if state == models.ProtectionPolicyStateRevoked {
			var previous models.ProtectionPolicyRevision
			if err := tx.Where("tenant_id = ? AND policy_id = ? AND revision = ?", tenantID, policy.ID, policy.CurrentRevision).First(&previous).Error; err != nil {
				return err
			}
			effect = previous.Effect
		} else if protectionEffectRank(effect) < protectionEffectRank(baseline.Effect) {
			return commonapi.ErrBadRequest
		}
		now := s.now().UTC()
		revisionNumber := policy.CurrentRevision + 1
		revision := models.ProtectionPolicyRevision{
			ID: uuid.NewString(), TenantID: tenantID, PolicyID: policy.ID, Revision: revisionNumber,
			State: state, Effect: effect, Rationale: rationale, CreatedBy: userID, CreatedAt: now,
		}
		if err := tx.Create(&revision).Error; err != nil {
			return policyDBError(err)
		}
		update := tx.Model(&policy).Where("version = ?", version).Updates(map[string]interface{}{
			"state": state, "version": gorm.Expr("version + 1"), "current_revision": revisionNumber, "updated_at": now,
		})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return repository.ErrVersionConflict
		}
		policy.State = state
		policy.Version++
		policy.CurrentRevision = revisionNumber
		policy.UpdatedAt = now
		if err := compileProtectionProjections(tx, enrollment, enrollmentSnapshotHash(enrollment, current.SourceSnapshotHash), now, []string{"manager"}); err != nil {
			return err
		}
		_ = assessment
		result, err = buildPolicyResponse(tx, policy, true)
		return err
	})
	return result, err
}

func (s *PolicyService) List(ctx context.Context, tenantID, page, pageSize int64) (*models.ProtectionPolicyListResponse, error) {
	if tenantID <= 0 || page <= 0 || pageSize <= 0 || pageSize > 100 {
		return nil, commonapi.ErrBadRequest
	}
	var total int64
	if err := s.db.WithContext(ctx).Model(&models.ProtectionPolicy{}).Where("tenant_id = ?", tenantID).Count(&total).Error; err != nil {
		return nil, err
	}
	var rows []models.ProtectionPolicy
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Order("updated_at DESC, id ASC").Offset(int((page - 1) * pageSize)).Limit(int(pageSize)).Find(&rows).Error; err != nil {
		return nil, err
	}
	data := make([]models.ProtectionPolicyResponse, 0, len(rows))
	for _, row := range rows {
		built, err := buildPolicyResponse(s.db.WithContext(ctx), row, false)
		if err != nil {
			return nil, err
		}
		data = append(data, *built)
	}
	return &models.ProtectionPolicyListResponse{Data: data, Total: total, Page: int(page), PageSize: int(pageSize), TotalPages: int((total + pageSize - 1) / pageSize)}, nil
}

func (s *PolicyService) Get(ctx context.Context, tenantID int64, policyID string) (*models.ProtectionPolicyResponse, error) {
	if tenantID <= 0 || uuid.Validate(policyID) != nil {
		return nil, commonapi.ErrBadRequest
	}
	var policy models.ProtectionPolicy
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, policyID).First(&policy).Error; err != nil {
		return nil, policyDBError(err)
	}
	return buildPolicyResponse(s.db.WithContext(ctx), policy, true)
}

func policyDependencies(tx *gorm.DB, tenantID int64, assessmentID string) (models.ResourceSecurityAssessment, models.ResourceSecurityAssessmentRevision, models.ProtectionEnrollment, models.ProtectionBaseline, error) {
	var assessment models.ResourceSecurityAssessment
	if err := tx.Where("tenant_id = ? AND id = ?", tenantID, assessmentID).First(&assessment).Error; err != nil {
		return assessment, models.ResourceSecurityAssessmentRevision{}, models.ProtectionEnrollment{}, models.ProtectionBaseline{}, policyDBError(err)
	}
	var current models.ResourceSecurityAssessmentRevision
	if err := tx.Where("tenant_id = ? AND assessment_id = ? AND revision = ?", tenantID, assessment.ID, assessment.CurrentRevision).First(&current).Error; err != nil {
		return assessment, current, models.ProtectionEnrollment{}, models.ProtectionBaseline{}, policyDBError(err)
	}
	if current.Conclusion != models.AssessmentConclusionSensitive {
		return assessment, current, models.ProtectionEnrollment{}, models.ProtectionBaseline{}, commonapi.ErrConflict
	}
	var enrollment models.ProtectionEnrollment
	if err := tx.Where("tenant_id = ? AND id = ? AND state IN ?", tenantID, assessment.EnrollmentID, []string{models.EnrollmentStateEnrolling, models.EnrollmentStateActive}).First(&enrollment).Error; err != nil {
		return assessment, current, enrollment, models.ProtectionBaseline{}, policyDBError(err)
	}
	var baseline models.ProtectionBaseline
	if err := tx.Where("tenant_id = ? AND sensitive_data_type_id = ? AND security_grade_id = ? AND enabled = ?", tenantID, current.SensitiveDataTypeID, current.SecurityGradeID, true).First(&baseline).Error; err != nil {
		return assessment, current, enrollment, baseline, policyDBError(err)
	}
	return assessment, current, enrollment, baseline, nil
}

func buildPolicyResponse(db *gorm.DB, policy models.ProtectionPolicy, history bool) (*models.ProtectionPolicyResponse, error) {
	var current models.ProtectionPolicyRevision
	if err := db.Where("tenant_id = ? AND policy_id = ? AND revision = ?", policy.TenantID, policy.ID, policy.CurrentRevision).First(&current).Error; err != nil {
		return nil, err
	}
	result := &models.ProtectionPolicyResponse{ProtectionPolicy: policy, Current: current}
	if history {
		if err := db.Where("tenant_id = ? AND policy_id = ?", policy.TenantID, policy.ID).Order("revision DESC").Find(&result.History).Error; err != nil {
			return nil, err
		}
	}
	return result, nil
}

func validPolicyEffect(effect string) bool {
	return effect == dataprotection.EffectMask || effect == dataprotection.EffectSuppress || effect == dataprotection.EffectDeny
}

func validPolicyRationale(rationale string) bool {
	return rationale != "" && len(rationale) <= 2000
}

func enrollmentSnapshotHash(enrollment models.ProtectionEnrollment, fallback string) string {
	if enrollment.LatestSourceSnapshotHash != "" {
		return enrollment.LatestSourceSnapshotHash
	}
	return fallback
}

func policyDBError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return commonapi.ErrNotFound
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return commonapi.ErrConflict
	}
	return err
}
