package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/security/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AssessmentService struct {
	db       *gorm.DB
	factsFor TenantSecurityFactsReader
	now      func() time.Time
}

func NewAssessmentService(db *gorm.DB, factsFor TenantSecurityFactsReader) *AssessmentService {
	return &AssessmentService{db: db, factsFor: factsFor, now: time.Now}
}

func (s *AssessmentService) ReviewFinding(ctx context.Context, tenantID, reviewerID int64, findingID string, request models.FindingReviewRequest) (*models.FindingReviewResponse, error) {
	request.Decision = strings.ToLower(strings.TrimSpace(request.Decision))
	request.Rationale = strings.TrimSpace(request.Rationale)
	if tenantID <= 0 || reviewerID <= 0 || uuid.Validate(findingID) != nil || request.Rationale == "" || len(request.Rationale) > 2000 {
		return nil, commonapi.ErrBadRequest
	}
	switch request.Decision {
	case models.FindingReviewDecisionConfirm, models.FindingReviewDecisionReject:
		if request.SensitiveDataTypeID != nil || request.SecurityGradeID != nil {
			return nil, commonapi.ErrBadRequest
		}
	case models.FindingReviewDecisionAdjust:
		if request.SensitiveDataTypeID == nil || *request.SensitiveDataTypeID <= 0 || request.SecurityGradeID == nil || *request.SecurityGradeID <= 0 {
			return nil, commonapi.ErrBadRequest
		}
	default:
		return nil, commonapi.ErrBadRequest
	}

	var response *models.FindingReviewResponse
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var finding models.SensitiveFinding
		if err := tx.Where("tenant_id = ? AND id = ?", tenantID, findingID).First(&finding).Error; err != nil {
			return assessmentDBError(err)
		}
		var enrollment models.ProtectionEnrollment
		if err := tx.Where("tenant_id = ? AND id = ? AND state IN ?", tenantID, finding.EnrollmentID, []string{models.EnrollmentStateEnrolling, models.EnrollmentStateActive}).First(&enrollment).Error; err != nil {
			return assessmentDBError(err)
		}
		if finding.SourceSnapshotHash != enrollment.LatestSourceSnapshotHash || finding.DiscoveryExecutionID != enrollment.LatestDiscoveryExecutionID {
			return commonapi.ErrConflict
		}
		now := s.now().UTC()
		review := models.SensitiveFindingReview{
			ID: uuid.NewString(), TenantID: tenantID, FindingID: finding.ID,
			Decision: request.Decision, Rationale: request.Rationale,
			ReviewedBy: reviewerID, CreatedAt: now,
		}
		if request.Decision == models.FindingReviewDecisionReject {
			if err := tx.Create(&review).Error; err != nil {
				return assessmentDBError(err)
			}
			if err := compileProtectionProjections(tx, enrollment, enrollmentSnapshotHash(enrollment, finding.SourceSnapshotHash), now, []string{"manager", "develop", "service", "transfer"}); err != nil {
				return err
			}
			response = &models.FindingReviewResponse{Review: review}
			return nil
		}

		dataTypeID := finding.SensitiveDataTypeID
		var dataType models.SensitiveDataType
		if request.Decision == models.FindingReviewDecisionAdjust {
			dataTypeID = *request.SensitiveDataTypeID
		}
		if err := tx.Where("tenant_id = ? AND id = ?", tenantID, dataTypeID).First(&dataType).Error; err != nil {
			return assessmentDBError(err)
		}
		gradeID := dataType.DefaultSecurityGradeID
		if request.Decision == models.FindingReviewDecisionAdjust {
			gradeID = *request.SecurityGradeID
		}
		if err := ensureAssessmentGrade(tx, tenantID, gradeID); err != nil {
			return err
		}
		review.SensitiveDataTypeID = &dataTypeID
		review.SecurityGradeID = &gradeID
		if err := tx.Create(&review).Error; err != nil {
			return assessmentDBError(err)
		}

		assessment, revisionNumber, err := lockOrCreateAssessment(tx, tenantID, reviewerID, finding, now)
		if err != nil {
			return err
		}
		revision := models.ResourceSecurityAssessmentRevision{
			ID: uuid.NewString(), TenantID: tenantID, AssessmentID: assessment.ID, Revision: revisionNumber,
			SourceKind: models.AssessmentRevisionSourceFinding, Conclusion: models.AssessmentConclusionSensitive,
			SourceFindingID: &finding.ID, SourceReviewID: &review.ID,
			SensitiveDataTypeID: dataType.ID, SecurityClassificationID: dataType.SecurityClassificationID,
			SecurityGradeID: gradeID, SourceSnapshotHash: finding.SourceSnapshotHash,
			Component: finding.Component, Rationale: request.Rationale,
			CreatedBy: reviewerID, CreatedAt: now,
		}
		if err := tx.Create(&revision).Error; err != nil {
			return assessmentDBError(err)
		}
		if err := compileProtectionProjections(tx, enrollment, enrollmentSnapshotHash(enrollment, finding.SourceSnapshotHash), now, []string{"manager", "develop", "service", "transfer"}); err != nil {
			return err
		}
		built, err := buildAssessmentResponse(tx, *assessment, true)
		if err != nil {
			return err
		}
		response = &models.FindingReviewResponse{Review: review, Assessment: built}
		return nil
	})
	return response, err
}

func (s *AssessmentService) ListComponents(ctx context.Context, tenantID int64, enrollmentID string) (*models.AssessmentComponentListResponse, error) {
	enrollmentID = strings.TrimSpace(enrollmentID)
	if tenantID <= 0 || uuid.Validate(enrollmentID) != nil {
		return nil, commonapi.ErrBadRequest
	}
	if s.factsFor == nil {
		return nil, errors.New("security facts reader is unavailable")
	}
	var enrollment models.ProtectionEnrollment
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND id = ? AND state IN ?", tenantID, enrollmentID, []string{models.EnrollmentStateEnrolling, models.EnrollmentStateActive}).First(&enrollment).Error; err != nil {
		return nil, assessmentDBError(err)
	}
	facts, err := s.factsFor(uint(tenantID)).GetDataItemSecurityFacts(ctx, enrollment.TargetIdentity)
	if err != nil {
		return nil, err
	}
	if facts == nil || facts.Validate() != nil || facts.ItemFingerprint != enrollment.TargetIdentity {
		return nil, errors.New("Meta security facts are invalid")
	}

	var assessedComponentKeys []string
	if err := s.db.WithContext(ctx).Model(&models.ResourceSecurityAssessment{}).
		Where("tenant_id = ? AND enrollment_id = ?", tenantID, enrollmentID).
		Pluck("component_key", &assessedComponentKeys).Error; err != nil {
		return nil, err
	}
	assessedComponents := make(map[string]struct{}, len(assessedComponentKeys))
	for _, componentKey := range assessedComponentKeys {
		assessedComponents[componentKey] = struct{}{}
	}

	options := make([]models.AssessmentComponentOption, 0, len(facts.Fields))
	for _, field := range facts.Fields {
		if field.Type == datatype.FieldTypeJSON || field.Type == datatype.FieldTypeArray {
			continue
		}
		path := normalizedFieldPath(field)
		if len(path) == 0 {
			continue
		}
		componentKey := strings.Join(path, ".")
		if _, exists := assessedComponents[componentKey]; exists {
			continue
		}
		component, err := componentFromFields(facts.Fields, componentKey)
		if err != nil {
			return nil, err
		}
		fingerprint, err := dataprotection.ComponentSchemaFingerprint(facts.Fields, component)
		if err != nil {
			return nil, err
		}
		component.SchemaFingerprint = fingerprint
		options = append(options, models.AssessmentComponentOption{Component: component})
	}
	sort.Slice(options, func(i, j int) bool { return options[i].Component.Key < options[j].Component.Key })
	return &models.AssessmentComponentListResponse{Data: options, SourceSnapshotHash: facts.SourceSnapshotHash}, nil
}

func (s *AssessmentService) CreateManual(ctx context.Context, tenantID, reviewerID int64, request models.CreateManualAssessmentRequest) (*models.ResourceSecurityAssessmentResponse, error) {
	request.EnrollmentID = strings.TrimSpace(request.EnrollmentID)
	request.ComponentKey = strings.TrimSpace(request.ComponentKey)
	request.Rationale = strings.TrimSpace(request.Rationale)
	if tenantID <= 0 || reviewerID <= 0 || uuid.Validate(request.EnrollmentID) != nil || request.EnrollmentVersion <= 0 || request.ComponentKey == "" || request.SensitiveDataTypeID <= 0 || request.SecurityGradeID <= 0 || request.Rationale == "" || len(request.Rationale) > 2000 || s.factsFor == nil {
		return nil, commonapi.ErrBadRequest
	}
	var enrollment models.ProtectionEnrollment
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND id = ? AND state IN ?", tenantID, request.EnrollmentID, []string{models.EnrollmentStateEnrolling, models.EnrollmentStateActive}).First(&enrollment).Error; err != nil {
		return nil, assessmentDBError(err)
	}
	if enrollment.Version != request.EnrollmentVersion {
		return nil, fmt.Errorf("%w: enrollment version is %d", commonapi.ErrConflict, enrollment.Version)
	}
	facts, err := s.factsFor(uint(tenantID)).GetDataItemSecurityFacts(ctx, enrollment.TargetIdentity)
	if err != nil {
		return nil, err
	}
	if facts == nil || facts.Validate() != nil || facts.ItemFingerprint != enrollment.TargetIdentity || facts.SourceSnapshotHash == "" || facts.SourceSnapshotHash != enrollment.LatestSourceSnapshotHash {
		return nil, commonapi.ErrConflict
	}
	component, err := componentFromFields(facts.Fields, request.ComponentKey)
	if err != nil || component.ValueType == string(datatype.FieldTypeJSON) || component.ValueType == string(datatype.FieldTypeArray) {
		return nil, commonapi.ErrBadRequest
	}
	component.SchemaFingerprint, err = dataprotection.ComponentSchemaFingerprint(facts.Fields, component)
	if err != nil {
		return nil, commonapi.ErrBadRequest
	}

	var response *models.ResourceSecurityAssessmentResponse
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.Where("tenant_id = ? AND id = ? AND state IN ?", tenantID, request.EnrollmentID, []string{models.EnrollmentStateEnrolling, models.EnrollmentStateActive}).First(&enrollment).Error; err != nil {
			return assessmentDBError(err)
		}
		if enrollment.Version != request.EnrollmentVersion || enrollment.LatestSourceSnapshotHash != facts.SourceSnapshotHash {
			return commonapi.ErrConflict
		}
		var count int64
		if err := tx.Model(&models.ResourceSecurityAssessment{}).Where("tenant_id = ? AND enrollment_id = ? AND component_key = ?", tenantID, enrollment.ID, request.ComponentKey).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return commonapi.ErrConflict
		}
		var dataType models.SensitiveDataType
		if err := tx.Where("tenant_id = ? AND id = ?", tenantID, request.SensitiveDataTypeID).First(&dataType).Error; err != nil {
			return assessmentDBError(err)
		}
		if err := ensureAssessmentGrade(tx, tenantID, request.SecurityGradeID); err != nil {
			return err
		}
		now := s.now().UTC()
		assessment := models.ResourceSecurityAssessment{
			ID: uuid.NewString(), TenantID: tenantID, EnrollmentID: enrollment.ID, ComponentKey: component.Key,
			Version: 1, CurrentRevision: 1, CreatedBy: reviewerID, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&assessment).Error; err != nil {
			return assessmentDBError(err)
		}
		revision := models.ResourceSecurityAssessmentRevision{
			ID: uuid.NewString(), TenantID: tenantID, AssessmentID: assessment.ID, Revision: 1,
			SourceKind: models.AssessmentRevisionSourceManual, Conclusion: models.AssessmentConclusionSensitive,
			SensitiveDataTypeID: dataType.ID, SecurityClassificationID: dataType.SecurityClassificationID,
			SecurityGradeID: request.SecurityGradeID, SourceSnapshotHash: facts.SourceSnapshotHash,
			Component: component, Rationale: request.Rationale, CreatedBy: reviewerID, CreatedAt: now,
		}
		if err := tx.Create(&revision).Error; err != nil {
			return assessmentDBError(err)
		}
		if err := compileProtectionProjections(tx, enrollment, enrollment.LatestSourceSnapshotHash, now, []string{"manager", "develop", "service", "transfer"}); err != nil {
			return err
		}
		response, err = buildAssessmentResponse(tx, assessment, true)
		return err
	})
	return response, err
}

func (s *AssessmentService) Revise(ctx context.Context, tenantID, reviewerID int64, assessmentID string, request models.AssessmentRevisionRequest) (*models.ResourceSecurityAssessmentResponse, error) {
	request.Rationale = strings.TrimSpace(request.Rationale)
	if tenantID <= 0 || reviewerID <= 0 || uuid.Validate(assessmentID) != nil || request.Version <= 0 || request.SensitiveDataTypeID <= 0 || request.SecurityGradeID <= 0 || request.Rationale == "" || len(request.Rationale) > 2000 {
		return nil, commonapi.ErrBadRequest
	}
	var response *models.ResourceSecurityAssessmentResponse
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var assessment models.ResourceSecurityAssessment
		query := tx
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.Where("tenant_id = ? AND id = ?", tenantID, assessmentID).First(&assessment).Error; err != nil {
			return assessmentDBError(err)
		}
		if assessment.Version != request.Version {
			return fmt.Errorf("%w: assessment version is %d", commonapi.ErrConflict, assessment.Version)
		}
		var current models.ResourceSecurityAssessmentRevision
		if err := tx.Where("tenant_id = ? AND assessment_id = ? AND revision = ?", tenantID, assessment.ID, assessment.CurrentRevision).First(&current).Error; err != nil {
			return assessmentDBError(err)
		}
		var dataType models.SensitiveDataType
		if err := tx.Where("tenant_id = ? AND id = ?", tenantID, request.SensitiveDataTypeID).First(&dataType).Error; err != nil {
			return assessmentDBError(err)
		}
		if err := ensureAssessmentGrade(tx, tenantID, request.SecurityGradeID); err != nil {
			return err
		}
		now := s.now().UTC()
		revisionNumber := assessment.CurrentRevision + 1
		revision := models.ResourceSecurityAssessmentRevision{
			ID: uuid.NewString(), TenantID: tenantID, AssessmentID: assessment.ID, Revision: revisionNumber,
			SourceKind: current.SourceKind, Conclusion: models.AssessmentConclusionSensitive,
			SourceFindingID: current.SourceFindingID, SourceReviewID: current.SourceReviewID,
			SensitiveDataTypeID: dataType.ID, SecurityClassificationID: dataType.SecurityClassificationID,
			SecurityGradeID: request.SecurityGradeID, SourceSnapshotHash: current.SourceSnapshotHash,
			Component: current.Component, Rationale: request.Rationale,
			CreatedBy: reviewerID, CreatedAt: now,
		}
		if err := tx.Create(&revision).Error; err != nil {
			return assessmentDBError(err)
		}
		update := tx.Model(&assessment).Where("version = ?", request.Version).Updates(map[string]interface{}{
			"version": gorm.Expr("version + 1"), "current_revision": revisionNumber, "updated_at": now,
		})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return commonapi.ErrConflict
		}
		assessment.Version++
		assessment.CurrentRevision = revisionNumber
		assessment.UpdatedAt = now
		var enrollment models.ProtectionEnrollment
		if err := tx.Where("tenant_id = ? AND id = ? AND state IN ?", tenantID, assessment.EnrollmentID, []string{models.EnrollmentStateEnrolling, models.EnrollmentStateActive}).First(&enrollment).Error; err != nil {
			return assessmentDBError(err)
		}
		if err := compileProtectionProjections(tx, enrollment, enrollmentSnapshotHash(enrollment, current.SourceSnapshotHash), now, []string{"manager", "develop", "service", "transfer"}); err != nil {
			return err
		}
		var err error
		response, err = buildAssessmentResponse(tx, assessment, true)
		return err
	})
	return response, err
}

func (s *AssessmentService) Revoke(ctx context.Context, tenantID, reviewerID int64, assessmentID string, request models.RevokeAssessmentRequest) (*models.ResourceSecurityAssessmentResponse, error) {
	request.Rationale = strings.TrimSpace(request.Rationale)
	if tenantID <= 0 || reviewerID <= 0 || uuid.Validate(assessmentID) != nil || request.Version <= 0 || request.Rationale == "" || len(request.Rationale) > 2000 {
		return nil, commonapi.ErrBadRequest
	}
	var response *models.ResourceSecurityAssessmentResponse
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var assessment models.ResourceSecurityAssessment
		query := tx
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.Where("tenant_id = ? AND id = ?", tenantID, assessmentID).First(&assessment).Error; err != nil {
			return assessmentDBError(err)
		}
		if assessment.Version != request.Version {
			return fmt.Errorf("%w: assessment version is %d", commonapi.ErrConflict, assessment.Version)
		}
		var current models.ResourceSecurityAssessmentRevision
		if err := tx.Where("tenant_id = ? AND assessment_id = ? AND revision = ?", tenantID, assessment.ID, assessment.CurrentRevision).First(&current).Error; err != nil {
			return assessmentDBError(err)
		}
		if current.Conclusion != models.AssessmentConclusionSensitive {
			return commonapi.ErrConflict
		}
		now := s.now().UTC()
		revisionNumber := assessment.CurrentRevision + 1
		revision := models.ResourceSecurityAssessmentRevision{
			ID: uuid.NewString(), TenantID: tenantID, AssessmentID: assessment.ID, Revision: revisionNumber,
			SourceKind: current.SourceKind, Conclusion: models.AssessmentConclusionNotSensitive,
			SourceFindingID: current.SourceFindingID, SourceReviewID: current.SourceReviewID,
			SensitiveDataTypeID: current.SensitiveDataTypeID, SecurityClassificationID: current.SecurityClassificationID,
			SecurityGradeID: current.SecurityGradeID, SourceSnapshotHash: current.SourceSnapshotHash,
			Component: current.Component, Rationale: request.Rationale, CreatedBy: reviewerID, CreatedAt: now,
		}
		if err := tx.Create(&revision).Error; err != nil {
			return assessmentDBError(err)
		}
		update := tx.Model(&assessment).Where("version = ?", request.Version).Updates(map[string]interface{}{
			"version": gorm.Expr("version + 1"), "current_revision": revisionNumber, "updated_at": now,
		})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return commonapi.ErrConflict
		}
		assessment.Version++
		assessment.CurrentRevision = revisionNumber
		assessment.UpdatedAt = now
		var enrollment models.ProtectionEnrollment
		if err := tx.Where("tenant_id = ? AND id = ? AND state IN ?", tenantID, assessment.EnrollmentID, []string{models.EnrollmentStateEnrolling, models.EnrollmentStateActive}).First(&enrollment).Error; err != nil {
			return assessmentDBError(err)
		}
		if err := compileProtectionProjections(tx, enrollment, enrollment.LatestSourceSnapshotHash, now, []string{"manager", "develop", "service", "transfer"}); err != nil {
			return err
		}
		var err error
		response, err = buildAssessmentResponse(tx, assessment, true)
		return err
	})
	return response, err
}

func (s *AssessmentService) List(ctx context.Context, tenantID int64, enrollmentID string, page, pageSize int64) (*models.ResourceSecurityAssessmentListResponse, error) {
	enrollmentID = strings.TrimSpace(enrollmentID)
	if tenantID <= 0 || page <= 0 || pageSize <= 0 || pageSize > 100 || (enrollmentID != "" && uuid.Validate(enrollmentID) != nil) {
		return nil, commonapi.ErrBadRequest
	}
	query := s.db.WithContext(ctx).Model(&models.ResourceSecurityAssessment{}).Where("tenant_id = ?", tenantID)
	if enrollmentID != "" {
		query = query.Where("enrollment_id = ?", enrollmentID)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	var rows []models.ResourceSecurityAssessment
	if err := query.Order("updated_at DESC, id ASC").Offset(int((page - 1) * pageSize)).Limit(int(pageSize)).Find(&rows).Error; err != nil {
		return nil, err
	}
	data := make([]models.ResourceSecurityAssessmentResponse, 0, len(rows))
	for _, row := range rows {
		built, err := buildAssessmentResponse(s.db.WithContext(ctx), row, false)
		if err != nil {
			return nil, err
		}
		data = append(data, *built)
	}
	return &models.ResourceSecurityAssessmentListResponse{Data: data, Total: total, Page: int(page), PageSize: int(pageSize), TotalPages: int((total + pageSize - 1) / pageSize)}, nil
}

func (s *AssessmentService) Get(ctx context.Context, tenantID int64, assessmentID string) (*models.ResourceSecurityAssessmentResponse, error) {
	if tenantID <= 0 || uuid.Validate(assessmentID) != nil {
		return nil, commonapi.ErrBadRequest
	}
	var assessment models.ResourceSecurityAssessment
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, assessmentID).First(&assessment).Error; err != nil {
		return nil, assessmentDBError(err)
	}
	return buildAssessmentResponse(s.db.WithContext(ctx), assessment, true)
}

func lockOrCreateAssessment(tx *gorm.DB, tenantID, reviewerID int64, finding models.SensitiveFinding, now time.Time) (*models.ResourceSecurityAssessment, int64, error) {
	var assessment models.ResourceSecurityAssessment
	query := tx
	if tx.Dialector.Name() == "postgres" {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := query.Where("tenant_id = ? AND enrollment_id = ? AND component_key = ?", tenantID, finding.EnrollmentID, finding.ComponentKey).First(&assessment).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		assessment = models.ResourceSecurityAssessment{
			ID: uuid.NewString(), TenantID: tenantID, EnrollmentID: finding.EnrollmentID,
			ComponentKey: finding.ComponentKey, Version: 1, CurrentRevision: 1,
			CreatedBy: reviewerID, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&assessment).Error; err != nil {
			return nil, 0, assessmentDBError(err)
		}
		return &assessment, 1, nil
	}
	if err != nil {
		return nil, 0, err
	}
	revision := assessment.CurrentRevision + 1
	if err := tx.Model(&assessment).Updates(map[string]interface{}{
		"version": gorm.Expr("version + 1"), "current_revision": revision, "updated_at": now,
	}).Error; err != nil {
		return nil, 0, err
	}
	assessment.Version++
	assessment.CurrentRevision = revision
	assessment.UpdatedAt = now
	return &assessment, revision, nil
}

func ensureAssessmentGrade(tx *gorm.DB, tenantID, gradeID int64) error {
	var count int64
	if err := tx.Model(&models.SecurityGrade{}).Where("tenant_id = ? AND id = ?", tenantID, gradeID).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return commonapi.ErrNotFound
	}
	return nil
}

func buildAssessmentResponse(db *gorm.DB, assessment models.ResourceSecurityAssessment, history bool) (*models.ResourceSecurityAssessmentResponse, error) {
	var current models.ResourceSecurityAssessmentRevision
	if err := db.Where("tenant_id = ? AND assessment_id = ? AND revision = ?", assessment.TenantID, assessment.ID, assessment.CurrentRevision).First(&current).Error; err != nil {
		return nil, err
	}
	response := &models.ResourceSecurityAssessmentResponse{ResourceSecurityAssessment: assessment, Current: current}
	if history {
		if err := db.Where("tenant_id = ? AND assessment_id = ?", assessment.TenantID, assessment.ID).Order("revision DESC").Find(&response.History).Error; err != nil {
			return nil, err
		}
	}
	return response, nil
}

func assessmentDBError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return commonapi.ErrNotFound
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return commonapi.ErrConflict
	}
	return err
}
