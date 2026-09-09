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
	db            *gorm.DB
	now           func() time.Time
	resolveActors AccessActorResolver
}

type AccessActorResolver func(context.Context, int64, []int64) (map[int64]string, error)

var ErrProtectionAccessRequestExpired = errors.New("protection access request expired")

func NewAccessRequestService(db *gorm.DB, resolveActors AccessActorResolver) *AccessRequestService {
	return &AccessRequestService{db: db, now: time.Now, resolveActors: resolveActors}
}

func (s *AccessRequestService) BackfillActorSnapshots(ctx context.Context) error {
	var rows []models.ProtectionAccessRequest
	if err := s.db.WithContext(ctx).
		Where("subject_display_name = '' OR (decided_by IS NOT NULL AND decided_by_display_name = '')").
		Order("tenant_id ASC, created_at ASC, id ASC").
		Find(&rows).Error; err != nil {
		return err
	}
	rowsByTenant := make(map[int64][]models.ProtectionAccessRequest)
	for _, row := range rows {
		rowsByTenant[row.TenantID] = append(rowsByTenant[row.TenantID], row)
	}
	for tenantID, tenantRows := range rowsByTenant {
		ids := make([]int64, 0, len(tenantRows)*2)
		seen := make(map[int64]struct{}, len(tenantRows)*2)
		for _, row := range tenantRows {
			if row.SubjectType != "user" {
				return errors.New("protection access request contains an unsupported actor type")
			}
			subjectID, err := strconv.ParseInt(row.SubjectID, 10, 64)
			if err != nil || subjectID <= 0 {
				return errors.New("protection access request contains an invalid actor ID")
			}
			if _, ok := seen[subjectID]; !ok {
				seen[subjectID] = struct{}{}
				ids = append(ids, subjectID)
			}
			if row.DecidedBy != nil {
				if _, ok := seen[*row.DecidedBy]; !ok {
					seen[*row.DecidedBy] = struct{}{}
					ids = append(ids, *row.DecidedBy)
				}
			}
		}
		displayNames := make(map[int64]string, len(ids))
		for start := 0; start < len(ids); start += 200 {
			end := min(start+200, len(ids))
			resolved, err := s.resolveActorDisplayNames(ctx, tenantID, ids[start:end])
			if err != nil {
				return err
			}
			for id, displayName := range resolved {
				displayNames[id] = displayName
			}
		}
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for _, row := range tenantRows {
				subjectID, _ := strconv.ParseInt(row.SubjectID, 10, 64)
				updates := map[string]any{"subject_display_name": displayNames[subjectID]}
				if row.DecidedBy != nil {
					updates["decided_by_display_name"] = displayNames[*row.DecidedBy]
				}
				if err := tx.Model(&models.ProtectionAccessRequest{}).
					Where("tenant_id = ? AND id = ?", tenantID, row.ID).
					Updates(updates).Error; err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
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
		var latestRequest models.ProtectionAccessRequest
		if err := s.db.WithContext(ctx).
			Where("tenant_id = ? AND assessment_id = ? AND consumer_owner = ? AND action = ? AND subject_type = ? AND subject_id = ?", tenantID, assessment.ID, owner, action, "user", userIDString(userID)).
			Order("created_at DESC, id DESC").
			First(&latestRequest).Error; err == nil {
			latestRequest = effectiveAccessRequest(latestRequest, now)
			target.AccessRequest = &models.ProtectionAccessRequestSummary{
				ID: latestRequest.ID, State: latestRequest.State, RequestedExpiresAt: latestRequest.RequestedExpiresAt,
			}
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
	actors, err := s.resolveActorDisplayNames(ctx, tenantID, []int64{userID})
	if err != nil {
		return nil, err
	}
	var response *models.ProtectionAccessRequestResponse
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		assessment, current, enrollment, _, err := policyDependencies(tx, tenantID, request.AssessmentID)
		if err != nil {
			return err
		}
		binding := "tenant_id = ? AND assessment_id = ? AND consumer_owner = ? AND action = ? AND subject_type = ? AND subject_id = ?"
		bindingValues := []any{tenantID, assessment.ID, request.ConsumerOwner, request.Action, "user", userIDString(userID)}
		if err := tx.Model(&models.ProtectionAccessRequest{}).
			Where(binding+" AND state = ? AND requested_expires_at <= ?", append(bindingValues, models.ProtectionAccessRequestStatePending, now)...).
			Updates(map[string]any{
				"state": models.ProtectionAccessRequestStateExpired, "version": gorm.Expr("version + 1"), "updated_at": now,
			}).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&models.ProtectionAccessRequest{}).Where(binding+" AND state = ?", append(bindingValues, models.ProtectionAccessRequestStatePending)...).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return commonapi.ErrConflict
		}
		row := models.ProtectionAccessRequest{
			ID: uuid.NewString(), TenantID: tenantID, AssessmentID: assessment.ID, AssessmentRevision: current.Revision,
			ConsumerOwner: request.ConsumerOwner, Action: request.Action, SubjectType: "user", SubjectID: userIDString(userID), SubjectDisplayName: actors[userID],
			RequestedExpiresAt: request.RequestedExpiresAt, Rationale: request.Rationale,
			State: models.ProtectionAccessRequestStatePending, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&row).Error; err != nil {
			return policyDBError(err)
		}
		response = accessRequestResponse(row, current.Component, enrollment.ID, enrollment.TargetFullName, "")
		return nil
	})
	return response, err
}

func (s *AccessRequestService) ListMine(ctx context.Context, tenantID, userID, page, pageSize int64) (*models.ProtectionAccessRequestListResponse, error) {
	if userID <= 0 {
		return nil, commonapi.ErrBadRequest
	}
	return s.list(ctx, tenantID, page, pageSize, s.now().UTC(), "subject_type = ? AND subject_id = ?", "user", userIDString(userID))
}

func (s *AccessRequestService) ListReviewQueue(ctx context.Context, tenantID, reviewerID int64, filter models.ProtectionAccessRequestReviewFilter, page, pageSize int64) (*models.ProtectionAccessRequestListResponse, error) {
	filter.Scope = strings.TrimSpace(filter.Scope)
	filter.State = strings.TrimSpace(filter.State)
	filter.RequesterSearch = strings.TrimSpace(filter.RequesterSearch)
	filter.ResourceSearch = strings.TrimSpace(filter.ResourceSearch)
	if reviewerID <= 0 || (filter.Scope != models.ProtectionAccessRequestReviewScopePending && filter.Scope != models.ProtectionAccessRequestReviewScopeHistory) ||
		len(filter.RequesterSearch) > 255 || len(filter.ResourceSearch) > 255 ||
		(filter.CreatedFrom != nil && filter.CreatedTo != nil && filter.CreatedFrom.After(*filter.CreatedTo)) {
		return nil, commonapi.ErrBadRequest
	}
	if filter.State != "" && (filter.Scope != models.ProtectionAccessRequestReviewScopeHistory ||
		(filter.State != models.ProtectionAccessRequestStateApproved && filter.State != models.ProtectionAccessRequestStateRejected && filter.State != models.ProtectionAccessRequestStateExpired)) {
		return nil, commonapi.ErrBadRequest
	}
	now := s.now().UTC()
	base := s.db.WithContext(ctx).Model(&models.ProtectionAccessRequest{}).Where("tenant_id = ?", tenantID)
	if filter.Scope == models.ProtectionAccessRequestReviewScopePending {
		base = base.Where("state = ? AND requested_expires_at > ?", models.ProtectionAccessRequestStatePending, now)
	} else {
		switch filter.State {
		case models.ProtectionAccessRequestStateApproved, models.ProtectionAccessRequestStateRejected:
			base = base.Where("state = ?", filter.State)
		case models.ProtectionAccessRequestStateExpired:
			base = base.Where("(state = ? OR (state = ? AND requested_expires_at <= ?))", models.ProtectionAccessRequestStateExpired, models.ProtectionAccessRequestStatePending, now)
		default:
			base = base.Where("(state <> ? OR requested_expires_at <= ?)", models.ProtectionAccessRequestStatePending, now)
		}
	}
	if filter.RequesterSearch != "" {
		pattern := accessRequestContainsPattern(filter.RequesterSearch)
		base = base.Where("(LOWER(subject_display_name) LIKE ? ESCAPE '!' OR subject_id LIKE ? ESCAPE '!')", pattern, pattern)
	}
	if filter.ResourceSearch != "" {
		pattern := accessRequestContainsPattern(filter.ResourceSearch)
		assessmentIDs := s.db.WithContext(ctx).
			Table("security.resource_security_assessments AS assessment").
			Select("assessment.id").
			Joins("JOIN security.protection_enrollments AS enrollment ON enrollment.id = assessment.enrollment_id AND enrollment.tenant_id = assessment.tenant_id").
			Where("assessment.tenant_id = ?", tenantID).
			Where("(LOWER(enrollment.target_full_name) LIKE ? ESCAPE '!' OR LOWER(assessment.component_key) LIKE ? ESCAPE '!')", pattern, pattern)
		base = base.Where("assessment_id IN (?)", assessmentIDs)
	}
	if filter.CreatedFrom != nil {
		base = base.Where("created_at >= ?", filter.CreatedFrom.UTC())
	}
	if filter.CreatedTo != nil {
		base = base.Where("created_at <= ?", filter.CreatedTo.UTC())
	}
	result, err := s.listQuery(ctx, tenantID, page, pageSize, now, base)
	if err != nil {
		return nil, err
	}
	for index := range result.Data {
		row := &result.Data[index]
		if filter.Scope == models.ProtectionAccessRequestReviewScopePending && row.SubjectType == "user" && row.SubjectID == userIDString(reviewerID) {
			row.CanDecide = false
			row.DecisionUnavailableReason = models.ProtectionAccessRequestDecisionUnavailableSelfApproval
		} else if filter.Scope == models.ProtectionAccessRequestReviewScopePending {
			row.CanDecide = true
		}
	}
	return result, nil
}

func (s *AccessRequestService) list(ctx context.Context, tenantID, page, pageSize int64, now time.Time, condition string, values ...any) (*models.ProtectionAccessRequestListResponse, error) {
	base := s.db.WithContext(ctx).Model(&models.ProtectionAccessRequest{}).Where("tenant_id = ?", tenantID).Where(condition, values...)
	return s.listQuery(ctx, tenantID, page, pageSize, now, base)
}

func (s *AccessRequestService) listQuery(ctx context.Context, tenantID, page, pageSize int64, now time.Time, base *gorm.DB) (*models.ProtectionAccessRequestListResponse, error) {
	if tenantID <= 0 || page <= 0 || pageSize <= 0 || pageSize > 100 {
		return nil, commonapi.ErrBadRequest
	}
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
		row = effectiveAccessRequest(row, now)
		built, err := s.loadResponse(s.db.WithContext(ctx), row, now)
		if err != nil {
			return nil, err
		}
		data = append(data, *built)
	}
	return &models.ProtectionAccessRequestListResponse{Data: data, Total: total, Page: int(page), PageSize: int(pageSize), TotalPages: int((total + pageSize - 1) / pageSize)}, nil
}

func accessRequestContainsPattern(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "!", "!!")
	value = strings.ReplaceAll(value, "%", "!%")
	value = strings.ReplaceAll(value, "_", "!_")
	return "%" + value + "%"
}

func effectiveAccessRequest(row models.ProtectionAccessRequest, now time.Time) models.ProtectionAccessRequest {
	if row.State == models.ProtectionAccessRequestStatePending && !now.Before(row.RequestedExpiresAt) {
		row.State = models.ProtectionAccessRequestStateExpired
	}
	return row
}

func (s *AccessRequestService) Decide(ctx context.Context, tenantID, reviewerID int64, requestID string, request models.DecideProtectionAccessRequest) (*models.ProtectionAccessRequestResponse, error) {
	requestID, request.Decision, request.Rationale = strings.TrimSpace(requestID), strings.TrimSpace(request.Decision), strings.TrimSpace(request.Rationale)
	now := s.now().UTC()
	request.ExpiresAt = request.ExpiresAt.UTC()
	if tenantID <= 0 || reviewerID <= 0 || uuid.Validate(requestID) != nil || request.Version <= 0 || (request.Decision != "approve" && request.Decision != "reject") || !validPolicyRationale(request.Rationale) {
		return nil, commonapi.ErrBadRequest
	}
	actors, err := s.resolveActorDisplayNames(ctx, tenantID, []int64{reviewerID})
	if err != nil {
		return nil, err
	}
	var response *models.ProtectionAccessRequestResponse
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		if !now.Before(row.RequestedExpiresAt) {
			return ErrProtectionAccessRequestExpired
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
			"decided_by_display_name": actors[reviewerID], "decision_rationale": request.Rationale, "updated_at": now,
		})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return repository.ErrVersionConflict
		}
		row.State, row.Version, row.DecidedBy, row.DecidedByDisplayName, row.DecidedAt, row.DecisionRationale, row.UpdatedAt = state, row.Version+1, &reviewerID, actors[reviewerID], &now, request.Rationale, now
		if state == models.ProtectionAccessRequestStateApproved {
			if err := compileProtectionProjections(tx, enrollment, enrollmentSnapshotHash(enrollment, current.SourceSnapshotHash), now, []string{managerProtectionOwner}); err != nil {
				return err
			}
		}
		_ = assessment
		response = accessRequestResponse(row, current.Component, enrollment.ID, enrollment.TargetFullName, exemptionID)
		if state == models.ProtectionAccessRequestStateApproved {
			response.AuthorizationState = models.ProtectionExemptionStateActive
			response.AuthorizedUntil = &request.ExpiresAt
		}
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

func (s *AccessRequestService) loadResponse(db *gorm.DB, row models.ProtectionAccessRequest, now time.Time) (*models.ProtectionAccessRequestResponse, error) {
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
	response := accessRequestResponse(row, revision.Component, enrollment.ID, enrollment.TargetFullName, exemptionID)
	if row.State != models.ProtectionAccessRequestStateApproved {
		return response, nil
	}
	if exemptionID == "" {
		return nil, errors.New("approved protection access request is missing its exemption")
	}
	var exemptionRevisions []models.ProtectionExemptionRevision
	if err := db.Where(
		"tenant_id = ? AND exemption_id = ? AND (revision = ? OR source_request_id = ?)",
		row.TenantID, exemption.ID, exemption.CurrentRevision, row.ID,
	).Order("revision DESC").Find(&exemptionRevisions).Error; err != nil {
		return nil, err
	}
	currentExemptionRevision, grantRevision := findAccessRequestExemptionRevisions(exemptionRevisions, exemption.CurrentRevision, row.ID)
	if currentExemptionRevision == nil || grantRevision == nil {
		return nil, errors.New("approved protection access request is missing its exemption revision")
	}
	response.AuthorizationState = effectiveAccessRequestAuthorizationState(row.ID, exemption, *currentExemptionRevision, assessment.CurrentRevision, now)
	response.AuthorizedUntil = &grantRevision.ExpiresAt
	return response, nil
}

func findAccessRequestExemptionRevisions(revisions []models.ProtectionExemptionRevision, currentRevision int64, requestID string) (*models.ProtectionExemptionRevision, *models.ProtectionExemptionRevision) {
	var current, grant *models.ProtectionExemptionRevision
	for index := range revisions {
		revision := &revisions[index]
		if revision.Revision == currentRevision {
			current = revision
		}
		if grant == nil && revision.SourceRequestID == requestID && revision.State == models.ProtectionExemptionStateActive {
			grant = revision
		}
	}
	return current, grant
}

func effectiveAccessRequestAuthorizationState(requestID string, exemption models.ProtectionExemption, current models.ProtectionExemptionRevision, assessmentRevision int64, now time.Time) string {
	switch {
	case current.SourceRequestID != requestID:
		return models.ProtectionExemptionStateSuperseded
	case current.State == models.ProtectionExemptionStateRevoked || exemption.State == models.ProtectionExemptionStateRevoked:
		return models.ProtectionExemptionStateRevoked
	case current.AssessmentRevision != assessmentRevision:
		return models.ProtectionExemptionStateSuperseded
	case !now.Before(current.ExpiresAt):
		return models.ProtectionExemptionStateExpired
	default:
		return models.ProtectionExemptionStateActive
	}
}

func accessRequestResponse(row models.ProtectionAccessRequest, component dataprotection.Component, enrollmentID, targetFullName, exemptionID string) *models.ProtectionAccessRequestResponse {
	response := &models.ProtectionAccessRequestResponse{
		ProtectionAccessRequest: row,
		Requester:               models.ProtectionAccessActor{Type: row.SubjectType, ID: row.SubjectID, DisplayName: row.SubjectDisplayName},
		Component:               component,
		EnrollmentID:            enrollmentID,
		TargetFullName:          targetFullName,
		ExemptionID:             exemptionID,
	}
	if row.DecidedBy != nil {
		response.Reviewer = &models.ProtectionAccessActor{Type: "user", ID: userIDString(*row.DecidedBy), DisplayName: row.DecidedByDisplayName}
	}
	return response
}

func (s *AccessRequestService) resolveActorDisplayNames(ctx context.Context, tenantID int64, ids []int64) (map[int64]string, error) {
	if s == nil || s.resolveActors == nil || tenantID <= 0 || len(ids) == 0 {
		return nil, errors.New("protection access actor resolver is required")
	}
	resolved, err := s.resolveActors(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if strings.TrimSpace(resolved[id]) == "" {
			return nil, errors.New("protection access actor identity is unavailable")
		}
	}
	return resolved, nil
}

func userIDString(userID int64) string {
	return strconv.FormatInt(userID, 10)
}
