package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
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

type AccessRequestService struct {
	db  *gorm.DB
	now func() time.Time
}

func NewAccessRequestService(db *gorm.DB) *AccessRequestService {
	return &AccessRequestService{db: db, now: time.Now}
}

func (s *AccessRequestService) Targets(ctx context.Context, tenantID, userID int64, targetIdentity, owner, action string) (*models.ProtectionAccessTargetListResponse, error) {
	targetIdentity, owner, action = strings.TrimSpace(targetIdentity), strings.TrimSpace(owner), strings.TrimSpace(action)
	if tenantID <= 0 || userID <= 0 || targetIdentity == "" || owner != managerProtectionOwner || action != managerPreviewAction {
		return nil, commonapi.ErrBadRequest
	}
	var enrollment models.ProtectionEnrollment
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND target_identity = ? AND state <> ?", tenantID, targetIdentity, models.EnrollmentStateReleased).First(&enrollment).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return &models.ProtectionAccessTargetListResponse{Data: []models.ProtectionAccessTarget{}}, nil
	} else if err != nil {
		return nil, err
	}
	var projectionRecord models.ProtectionProjectionRecord
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND enrollment_id = ? AND consumer_owner = ?", tenantID, enrollment.ID, owner).First(&projectionRecord).Error; err != nil {
		return nil, err
	}
	var projection dataprotection.Projection
	if err := json.Unmarshal([]byte(projectionRecord.ProjectionPayload), &projection); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	if err := projection.Validate(now); err != nil {
		return nil, err
	}
	var assessments []models.ResourceSecurityAssessment
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND enrollment_id = ?", tenantID, enrollment.ID).Find(&assessments).Error; err != nil {
		return nil, err
	}
	assessmentByComponent := make(map[string]models.ResourceSecurityAssessment, len(assessments))
	for _, assessment := range assessments {
		assessmentByComponent[assessment.ComponentKey] = assessment
	}
	result := make([]models.ProtectionAccessTarget, 0, len(projection.Rules))
	for _, rule := range projection.Rules {
		if rule.Action != action {
			continue
		}
		target := models.ProtectionAccessTarget{Component: rule.Component, UnavailableReason: "formal_assessment_required"}
		assessment, exists := assessmentByComponent[rule.Component.Key]
		if !exists {
			result = append(result, target)
			continue
		}
		var revision models.ResourceSecurityAssessmentRevision
		if err := s.db.WithContext(ctx).Where("tenant_id = ? AND assessment_id = ? AND revision = ? AND conclusion = ?", tenantID, assessment.ID, assessment.CurrentRevision, models.AssessmentConclusionSensitive).First(&revision).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			result = append(result, target)
			continue
		} else if err != nil {
			return nil, err
		}
		target.AssessmentID, target.AssessmentRevision, target.Requestable, target.UnavailableReason = assessment.ID, revision.Revision, true, ""
		var pending models.ProtectionAccessRequest
		if err := s.db.WithContext(ctx).Where("tenant_id = ? AND assessment_id = ? AND consumer_owner = ? AND action = ? AND subject_type = ? AND subject_id = ? AND state = ?", tenantID, assessment.ID, owner, action, "user", userIDString(userID), models.ProtectionAccessRequestStatePending).First(&pending).Error; err == nil {
			target.PendingRequestID = pending.ID
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		var exemption models.ProtectionExemption
		if err := s.db.WithContext(ctx).Where("tenant_id = ? AND assessment_id = ? AND consumer_owner = ? AND action = ? AND subject_type = ? AND subject_id = ? AND state = ?", tenantID, assessment.ID, owner, action, "user", userIDString(userID), models.ProtectionExemptionStateActive).First(&exemption).Error; err == nil {
			var current models.ProtectionExemptionRevision
			if err := s.db.WithContext(ctx).Where("tenant_id = ? AND exemption_id = ? AND revision = ?", tenantID, exemption.ID, exemption.CurrentRevision).First(&current).Error; err != nil {
				return nil, err
			}
			if current.AssessmentRevision == revision.Revision && now.Before(current.ExpiresAt) {
				target.ActiveExemptionID, target.AuthorizedUntil = exemption.ID, &current.ExpiresAt
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		result = append(result, target)
	}
	return &models.ProtectionAccessTargetListResponse{Data: result}, nil
}

func (s *AccessRequestService) Create(ctx context.Context, tenantID, userID int64, request models.CreateProtectionAccessRequest) (*models.ProtectionAccessRequestResponse, error) {
	request.AssessmentID = strings.TrimSpace(request.AssessmentID)
	request.ConsumerOwner = strings.TrimSpace(request.ConsumerOwner)
	request.Action = strings.TrimSpace(request.Action)
	request.Rationale = strings.TrimSpace(request.Rationale)
	now := s.now().UTC()
	request.RequestedExpiresAt = request.RequestedExpiresAt.UTC()
	if tenantID <= 0 || userID <= 0 || uuid.Validate(request.AssessmentID) != nil || request.ConsumerOwner != managerProtectionOwner || request.Action != managerPreviewAction || !validExemptionDeadline(now, request.RequestedExpiresAt) || !validPolicyRationale(request.Rationale) {
		return nil, commonapi.ErrBadRequest
	}
	var response *models.ProtectionAccessRequestResponse
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		assessment, current, enrollment, _, err := policyDependencies(tx, tenantID, request.AssessmentID)
		if err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&models.ProtectionAccessRequest{}).Where("tenant_id = ? AND assessment_id = ? AND consumer_owner = ? AND action = ? AND subject_type = ? AND subject_id = ? AND state = ?", tenantID, assessment.ID, request.ConsumerOwner, request.Action, "user", userIDString(userID), models.ProtectionAccessRequestStatePending).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return commonapi.ErrConflict
		}
		row := models.ProtectionAccessRequest{
			ID: uuid.NewString(), TenantID: tenantID, AssessmentID: assessment.ID, AssessmentRevision: current.Revision,
			ConsumerOwner: request.ConsumerOwner, Action: request.Action, SubjectType: "user", SubjectID: userIDString(userID),
			RequestedExpiresAt: request.RequestedExpiresAt, Rationale: request.Rationale,
			State: models.ProtectionAccessRequestStatePending, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&row).Error; err != nil {
			return policyDBError(err)
		}
		response = accessRequestResponse(row, current.Component, enrollment.TargetFullName, "")
		return nil
	})
	return response, err
}

func (s *AccessRequestService) ListMine(ctx context.Context, tenantID, userID, page, pageSize int64) (*models.ProtectionAccessRequestListResponse, error) {
	if userID <= 0 {
		return nil, commonapi.ErrBadRequest
	}
	return s.list(ctx, tenantID, page, pageSize, "subject_type = ? AND subject_id = ?", "user", userIDString(userID))
}

func (s *AccessRequestService) ListReviewQueue(ctx context.Context, tenantID, reviewerID, page, pageSize int64) (*models.ProtectionAccessRequestListResponse, error) {
	if reviewerID <= 0 {
		return nil, commonapi.ErrBadRequest
	}
	return s.list(
		ctx, tenantID, page, pageSize,
		"state = ? AND NOT (subject_type = ? AND subject_id = ?)",
		models.ProtectionAccessRequestStatePending, "user", userIDString(reviewerID),
	)
}

func (s *AccessRequestService) list(ctx context.Context, tenantID, page, pageSize int64, condition string, values ...any) (*models.ProtectionAccessRequestListResponse, error) {
	if tenantID <= 0 || page <= 0 || pageSize <= 0 || pageSize > 100 {
		return nil, commonapi.ErrBadRequest
	}
	base := s.db.WithContext(ctx).Model(&models.ProtectionAccessRequest{}).Where("tenant_id = ?", tenantID).Where(condition, values...)
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, err
	}
	var rows []models.ProtectionAccessRequest
	if err := base.Order("created_at DESC, id ASC").Offset(int((page - 1) * pageSize)).Limit(int(pageSize)).Find(&rows).Error; err != nil {
		return nil, err
	}
	data := make([]models.ProtectionAccessRequestResponse, 0, len(rows))
	for _, row := range rows {
		built, err := s.loadResponse(s.db.WithContext(ctx), row)
		if err != nil {
			return nil, err
		}
		data = append(data, *built)
	}
	return &models.ProtectionAccessRequestListResponse{Data: data, Total: total, Page: int(page), PageSize: int(pageSize), TotalPages: int((total + pageSize - 1) / pageSize)}, nil
}

func (s *AccessRequestService) Decide(ctx context.Context, tenantID, reviewerID int64, requestID string, request models.DecideProtectionAccessRequest) (*models.ProtectionAccessRequestResponse, error) {
	requestID, request.Decision, request.Rationale = strings.TrimSpace(requestID), strings.TrimSpace(request.Decision), strings.TrimSpace(request.Rationale)
	now := s.now().UTC()
	request.ExpiresAt = request.ExpiresAt.UTC()
	if tenantID <= 0 || reviewerID <= 0 || uuid.Validate(requestID) != nil || request.Version <= 0 || (request.Decision != "approve" && request.Decision != "reject") || !validPolicyRationale(request.Rationale) {
		return nil, commonapi.ErrBadRequest
	}
	var response *models.ProtectionAccessRequestResponse
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row models.ProtectionAccessRequest
		query := tx
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.Where("tenant_id = ? AND id = ?", tenantID, requestID).First(&row).Error; err != nil {
			return policyDBError(err)
		}
		if row.Version != request.Version {
			return repository.ErrVersionConflict
		}
		if row.State != models.ProtectionAccessRequestStatePending || row.SubjectID == userIDString(reviewerID) {
			return commonapi.ErrConflict
		}
		assessment, current, enrollment, _, err := policyDependencies(tx, tenantID, row.AssessmentID)
		if err != nil {
			return err
		}
		if current.Revision != row.AssessmentRevision {
			return commonapi.ErrConflict
		}
		state, exemptionID := models.ProtectionAccessRequestStateRejected, ""
		if request.Decision == "approve" {
			if !validExemptionDeadline(now, request.ExpiresAt) || request.ExpiresAt.After(row.RequestedExpiresAt) {
				return commonapi.ErrBadRequest
			}
			exemptionID, err = approveSubjectExemption(tx, row, reviewerID, request.ExpiresAt, request.Rationale, now)
			if err != nil {
				return err
			}
			state = models.ProtectionAccessRequestStateApproved
		}
		update := tx.Model(&row).Where("version = ?", request.Version).Updates(map[string]any{
			"state": state, "version": gorm.Expr("version + 1"), "decided_by": reviewerID, "decided_at": now,
			"decision_rationale": request.Rationale, "updated_at": now,
		})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return repository.ErrVersionConflict
		}
		row.State, row.Version, row.DecidedBy, row.DecidedAt, row.DecisionRationale, row.UpdatedAt = state, row.Version+1, &reviewerID, &now, request.Rationale, now
		if state == models.ProtectionAccessRequestStateApproved {
			if err := compileProtectionProjections(tx, enrollment, enrollmentSnapshotHash(enrollment, current.SourceSnapshotHash), now, []string{managerProtectionOwner}); err != nil {
				return err
			}
		}
		_ = assessment
		response = accessRequestResponse(row, current.Component, enrollment.TargetFullName, exemptionID)
		return nil
	})
	return response, err
}

func approveSubjectExemption(tx *gorm.DB, request models.ProtectionAccessRequest, reviewerID int64, expiresAt time.Time, rationale string, now time.Time) (string, error) {
	var exemption models.ProtectionExemption
	err := tx.Where("tenant_id = ? AND assessment_id = ? AND consumer_owner = ? AND action = ? AND subject_type = ? AND subject_id = ?", request.TenantID, request.AssessmentID, request.ConsumerOwner, request.Action, request.SubjectType, request.SubjectID).First(&exemption).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		exemption = models.ProtectionExemption{
			ID: uuid.NewString(), TenantID: request.TenantID, AssessmentID: request.AssessmentID,
			ConsumerOwner: request.ConsumerOwner, Action: request.Action, SubjectType: request.SubjectType, SubjectID: request.SubjectID,
			State: models.ProtectionExemptionStateActive, Version: 1, CurrentRevision: 1,
			CreatedBy: reviewerID, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&exemption).Error; err != nil {
			return "", policyDBError(err)
		}
	} else if err != nil {
		return "", err
	} else {
		exemption.State = models.ProtectionExemptionStateActive
		exemption.Version++
		exemption.CurrentRevision++
		exemption.UpdatedAt = now
		if err := tx.Model(&models.ProtectionExemption{}).Where("tenant_id = ? AND id = ?", request.TenantID, exemption.ID).Updates(map[string]any{
			"state": exemption.State, "version": exemption.Version, "current_revision": exemption.CurrentRevision, "updated_at": now,
		}).Error; err != nil {
			return "", err
		}
	}
	revision := models.ProtectionExemptionRevision{
		ID: uuid.NewString(), TenantID: request.TenantID, ExemptionID: exemption.ID, Revision: exemption.CurrentRevision,
		AssessmentRevision: request.AssessmentRevision, SourceRequestID: request.ID, State: models.ProtectionExemptionStateActive,
		ExpiresAt: expiresAt, Rationale: rationale, CreatedBy: reviewerID, CreatedAt: now,
	}
	if err := tx.Create(&revision).Error; err != nil {
		return "", policyDBError(err)
	}
	return exemption.ID, nil
}

func (s *AccessRequestService) loadResponse(db *gorm.DB, row models.ProtectionAccessRequest) (*models.ProtectionAccessRequestResponse, error) {
	var revision models.ResourceSecurityAssessmentRevision
	if err := db.Where("tenant_id = ? AND assessment_id = ? AND revision = ?", row.TenantID, row.AssessmentID, row.AssessmentRevision).First(&revision).Error; err != nil {
		return nil, err
	}
	var assessment models.ResourceSecurityAssessment
	if err := db.Where("tenant_id = ? AND id = ?", row.TenantID, row.AssessmentID).First(&assessment).Error; err != nil {
		return nil, err
	}
	var enrollment models.ProtectionEnrollment
	if err := db.Where("tenant_id = ? AND id = ?", row.TenantID, assessment.EnrollmentID).First(&enrollment).Error; err != nil {
		return nil, err
	}
	var exemption models.ProtectionExemption
	exemptionID := ""
	if err := db.Where("tenant_id = ? AND assessment_id = ? AND consumer_owner = ? AND action = ? AND subject_type = ? AND subject_id = ?", row.TenantID, row.AssessmentID, row.ConsumerOwner, row.Action, row.SubjectType, row.SubjectID).First(&exemption).Error; err == nil {
		exemptionID = exemption.ID
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return accessRequestResponse(row, revision.Component, enrollment.TargetFullName, exemptionID), nil
}

func accessRequestResponse(row models.ProtectionAccessRequest, component dataprotection.Component, targetFullName, exemptionID string) *models.ProtectionAccessRequestResponse {
	return &models.ProtectionAccessRequestResponse{
		ProtectionAccessRequest: row,
		Component:               component,
		TargetFullName:          targetFullName,
		ExemptionID:             exemptionID,
	}
}

func userIDString(userID int64) string {
	return strconv.FormatInt(userID, 10)
}
