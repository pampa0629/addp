package service

import (
	"context"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/security/internal/models"
	"github.com/addp/security/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const maxProtectionExemptionDuration = 30 * 24 * time.Hour

type ExemptionService struct {
	db  *gorm.DB
	now func() time.Time
}

func NewExemptionService(db *gorm.DB) *ExemptionService {
	return &ExemptionService{db: db, now: time.Now}
}

func (s *ExemptionService) Revoke(ctx context.Context, tenantID, userID int64, exemptionID string, request models.RevokeProtectionExemptionRequest) (*models.ProtectionExemptionResponse, error) {
	request.Rationale = strings.TrimSpace(request.Rationale)
	now := s.now().UTC()
	if tenantID <= 0 || userID <= 0 || uuid.Validate(exemptionID) != nil || request.Version <= 0 || !validPolicyRationale(request.Rationale) {
		return nil, commonapi.ErrBadRequest
	}
	return s.appendRevocation(ctx, tenantID, userID, exemptionID, request.Version, request.Rationale, now)
}

func (s *ExemptionService) appendRevocation(ctx context.Context, tenantID, userID int64, exemptionID string, version int64, rationale string, now time.Time) (*models.ProtectionExemptionResponse, error) {
	var result *models.ProtectionExemptionResponse
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var exemption models.ProtectionExemption
		query := tx
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.Where("tenant_id = ? AND id = ?", tenantID, exemptionID).First(&exemption).Error; err != nil {
			return policyDBError(err)
		}
		if exemption.Version != version {
			return repository.ErrVersionConflict
		}
		var previous models.ProtectionExemptionRevision
		if err := tx.Where("tenant_id = ? AND exemption_id = ? AND revision = ?", tenantID, exemption.ID, exemption.CurrentRevision).First(&previous).Error; err != nil {
			return err
		}
		if exemption.State == models.ProtectionExemptionStateRevoked || !now.Before(previous.ExpiresAt) {
			return commonapi.ErrConflict
		}
		assessment, current, enrollment, _, err := policyDependencies(tx, tenantID, exemption.AssessmentID)
		if err != nil {
			return err
		}
		revisionNumber := exemption.CurrentRevision + 1
		revision := models.ProtectionExemptionRevision{
			ID: uuid.NewString(), TenantID: tenantID, ExemptionID: exemption.ID, Revision: revisionNumber,
			AssessmentRevision: current.Revision, SourceRequestID: previous.SourceRequestID,
			State: models.ProtectionExemptionStateRevoked, ExpiresAt: previous.ExpiresAt, Rationale: rationale, CreatedBy: userID, CreatedAt: now,
		}
		if err := tx.Create(&revision).Error; err != nil {
			return policyDBError(err)
		}
		update := tx.Model(&exemption).Where("version = ?", version).Updates(map[string]interface{}{
			"state": models.ProtectionExemptionStateRevoked, "version": gorm.Expr("version + 1"), "current_revision": revisionNumber, "updated_at": now,
		})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return repository.ErrVersionConflict
		}
		exemption.State = models.ProtectionExemptionStateRevoked
		exemption.Version++
		exemption.CurrentRevision = revisionNumber
		exemption.UpdatedAt = now
		if err := compileProtectionProjections(tx, enrollment, enrollmentSnapshotHash(enrollment, current.SourceSnapshotHash), now, []string{exemption.ConsumerOwner}); err != nil {
			return err
		}
		_ = assessment
		var history []models.ProtectionExemptionRevision
		if err := tx.Where("tenant_id = ? AND exemption_id = ?", tenantID, exemption.ID).Order("revision DESC").Find(&history).Error; err != nil {
			return err
		}
		result = buildExemptionResponse(exemption, revision, history, current.Revision, now)
		return nil
	})
	return result, err
}

func (s *ExemptionService) List(ctx context.Context, tenantID int64, enrollmentID string, page, pageSize int64) (*models.ProtectionExemptionListResponse, error) {
	enrollmentID = strings.TrimSpace(enrollmentID)
	if tenantID <= 0 || page <= 0 || pageSize <= 0 || pageSize > 100 || (enrollmentID != "" && uuid.Validate(enrollmentID) != nil) {
		return nil, commonapi.ErrBadRequest
	}
	base := s.db.WithContext(ctx).Model(&models.ProtectionExemption{}).Where("protection_exemptions.tenant_id = ?", tenantID)
	if enrollmentID != "" {
		base = base.Joins("JOIN security.resource_security_assessments ON resource_security_assessments.id = protection_exemptions.assessment_id AND resource_security_assessments.tenant_id = protection_exemptions.tenant_id").Where("resource_security_assessments.enrollment_id = ?", enrollmentID)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, err
	}
	var rows []models.ProtectionExemption
	if err := base.Order("protection_exemptions.updated_at DESC, protection_exemptions.id ASC").Offset(int((page - 1) * pageSize)).Limit(int(pageSize)).Find(&rows).Error; err != nil {
		return nil, err
	}
	now := s.now().UTC()
	data := make([]models.ProtectionExemptionResponse, 0, len(rows))
	for _, row := range rows {
		built, err := loadExemptionResponse(s.db.WithContext(ctx), row, false, now)
		if err != nil {
			return nil, err
		}
		data = append(data, *built)
	}
	return &models.ProtectionExemptionListResponse{Data: data, Total: total, Page: int(page), PageSize: int(pageSize), TotalPages: int((total + pageSize - 1) / pageSize)}, nil
}

func (s *ExemptionService) Get(ctx context.Context, tenantID int64, exemptionID string) (*models.ProtectionExemptionResponse, error) {
	if tenantID <= 0 || uuid.Validate(exemptionID) != nil {
		return nil, commonapi.ErrBadRequest
	}
	var exemption models.ProtectionExemption
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, exemptionID).First(&exemption).Error; err != nil {
		return nil, policyDBError(err)
	}
	return loadExemptionResponse(s.db.WithContext(ctx), exemption, true, s.now().UTC())
}

func loadExemptionResponse(db *gorm.DB, exemption models.ProtectionExemption, history bool, now time.Time) (*models.ProtectionExemptionResponse, error) {
	var current models.ProtectionExemptionRevision
	if err := db.Where("tenant_id = ? AND exemption_id = ? AND revision = ?", exemption.TenantID, exemption.ID, exemption.CurrentRevision).First(&current).Error; err != nil {
		return nil, err
	}
	var revisions []models.ProtectionExemptionRevision
	if history {
		if err := db.Where("tenant_id = ? AND exemption_id = ?", exemption.TenantID, exemption.ID).Order("revision DESC").Find(&revisions).Error; err != nil {
			return nil, err
		}
	}
	var assessment models.ResourceSecurityAssessment
	if err := db.Where("tenant_id = ? AND id = ?", exemption.TenantID, exemption.AssessmentID).First(&assessment).Error; err != nil {
		return nil, err
	}
	return buildExemptionResponse(exemption, current, revisions, assessment.CurrentRevision, now), nil
}

func buildExemptionResponse(exemption models.ProtectionExemption, current models.ProtectionExemptionRevision, history []models.ProtectionExemptionRevision, assessmentRevision int64, now time.Time) *models.ProtectionExemptionResponse {
	effectiveState := exemption.State
	if effectiveState == models.ProtectionExemptionStateActive {
		switch {
		case current.AssessmentRevision != assessmentRevision:
			effectiveState = models.ProtectionExemptionStateSuperseded
		case !now.Before(current.ExpiresAt):
			effectiveState = models.ProtectionExemptionStateExpired
		}
	}
	return &models.ProtectionExemptionResponse{ProtectionExemption: exemption, EffectiveState: effectiveState, Current: current, History: history}
}

func validExemptionDeadline(now, expiresAt time.Time) bool {
	return !expiresAt.IsZero() && expiresAt.After(now) && !expiresAt.After(now.Add(maxProtectionExemptionDuration))
}
