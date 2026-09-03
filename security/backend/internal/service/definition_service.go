package service

import (
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataprotection"
	commonexecution "github.com/addp/common/execution"
	"github.com/addp/security/internal/models"
	"github.com/addp/security/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DefinitionService struct {
	db              *gorm.DB
	classifications *repository.Repository[models.SecurityClassification]
	grades          *repository.Repository[models.SecurityGrade]
	types           *repository.Repository[models.SensitiveDataType]
	detectors       *repository.Repository[models.Detector]
	baselines       *repository.Repository[models.ProtectionBaseline]
	now             func() time.Time
}

func NewDefinitionService(db *gorm.DB) *DefinitionService {
	return &DefinitionService{
		db:              db,
		classifications: repository.New[models.SecurityClassification](db),
		grades:          repository.New[models.SecurityGrade](db),
		types:           repository.New[models.SensitiveDataType](db),
		detectors:       repository.New[models.Detector](db),
		baselines:       repository.New[models.ProtectionBaseline](db),
		now:             time.Now,
	}
}

func (s *DefinitionService) ListClassifications(tenantID int64) ([]models.SecurityClassification, error) {
	return s.classifications.List(tenantID)
}
func (s *DefinitionService) GetClassification(id, tenantID int64) (*models.SecurityClassification, error) {
	return s.classifications.Get(id, tenantID)
}
func (s *DefinitionService) CreateClassification(req models.DefinitionRequest, tenantID, userID int64) (*models.SecurityClassification, error) {
	if req.ParentID != nil {
		if _, err := s.classifications.Get(*req.ParentID, tenantID); err != nil {
			return nil, err
		}
	}
	row := &models.SecurityClassification{TenantID: tenantID, Code: strings.TrimSpace(req.Code), Name: strings.TrimSpace(req.Name), Description: strings.TrimSpace(req.Description), ParentID: req.ParentID, SortOrder: req.SortOrder, CreatedBy: userID}
	if row.Code == "" || row.Name == "" {
		return nil, commonapi.ErrBadRequest
	}
	if err := s.classifications.Create(row); err != nil {
		return nil, err
	}
	return row, nil
}
func (s *DefinitionService) UpdateClassification(id, tenantID, userID int64, req models.DefinitionRequest) (*models.SecurityClassification, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, commonapi.ErrBadRequest
	}
	if req.ParentID != nil {
		if err := s.validateClassificationParent(id, *req.ParentID, tenantID); err != nil {
			return nil, err
		}
	}
	if err := s.classifications.Update(id, tenantID, req.Version, map[string]interface{}{"name": strings.TrimSpace(req.Name), "description": strings.TrimSpace(req.Description), "parent_id": req.ParentID, "sort_order": req.SortOrder, "updated_by": userID}); err != nil {
		return nil, err
	}
	return s.classifications.Get(id, tenantID)
}
func (s *DefinitionService) DeleteClassification(id, tenantID int64) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		definitions := s.withDB(tx)
		if err := rejectReferences(definitions.classifications, tenantID, "parent_id = ?", id); err != nil {
			return err
		}
		if err := rejectReferences(definitions.types, tenantID, "security_classification_id = ?", id); err != nil {
			return err
		}
		if err := rejectReferences(repository.New[models.ResourceSecurityAssessmentRevision](tx), tenantID, "security_classification_id = ?", id); err != nil {
			return err
		}
		return definitions.classifications.Delete(id, tenantID)
	})
}

func (s *DefinitionService) validateClassificationParent(id, parentID, tenantID int64) error {
	visited := map[int64]struct{}{id: {}}
	currentID := parentID
	for {
		if _, exists := visited[currentID]; exists {
			return commonapi.ErrBadRequest
		}
		visited[currentID] = struct{}{}
		current, err := s.classifications.Get(currentID, tenantID)
		if err != nil {
			return err
		}
		if current.ParentID == nil {
			return nil
		}
		currentID = *current.ParentID
	}
}

func (s *DefinitionService) ListGrades(tenantID int64) ([]models.SecurityGrade, error) {
	return s.grades.List(tenantID)
}
func (s *DefinitionService) GetGrade(id, tenantID int64) (*models.SecurityGrade, error) {
	return s.grades.Get(id, tenantID)
}
func (s *DefinitionService) CreateGrade(req models.DefinitionRequest, tenantID, userID int64) (*models.SecurityGrade, error) {
	row := &models.SecurityGrade{TenantID: tenantID, Code: strings.TrimSpace(req.Code), Name: strings.TrimSpace(req.Name), Description: strings.TrimSpace(req.Description), RiskOrder: req.RiskOrder, CreatedBy: userID}
	if row.Code == "" || row.Name == "" || row.RiskOrder <= 0 {
		return nil, commonapi.ErrBadRequest
	}
	if err := s.grades.Create(row); err != nil {
		return nil, err
	}
	return row, nil
}
func (s *DefinitionService) UpdateGrade(id, tenantID, userID int64, req models.DefinitionRequest) (*models.SecurityGrade, error) {
	if strings.TrimSpace(req.Name) == "" || req.RiskOrder <= 0 {
		return nil, commonapi.ErrBadRequest
	}
	if err := s.grades.Update(id, tenantID, req.Version, map[string]interface{}{"name": strings.TrimSpace(req.Name), "description": strings.TrimSpace(req.Description), "risk_order": req.RiskOrder, "updated_by": userID}); err != nil {
		return nil, err
	}
	return s.grades.Get(id, tenantID)
}
func (s *DefinitionService) DeleteGrade(id, tenantID int64) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		definitions := s.withDB(tx)
		if err := rejectReferences(definitions.types, tenantID, "default_security_grade_id = ?", id); err != nil {
			return err
		}
		if err := rejectReferences(definitions.baselines, tenantID, "security_grade_id = ?", id); err != nil {
			return err
		}
		if err := rejectReferences(repository.New[models.SensitiveFindingReview](tx), tenantID, "security_grade_id = ?", id); err != nil {
			return err
		}
		if err := rejectReferences(repository.New[models.ResourceSecurityAssessmentRevision](tx), tenantID, "security_grade_id = ?", id); err != nil {
			return err
		}
		return definitions.grades.Delete(id, tenantID)
	})
}

func (s *DefinitionService) ListTypes(tenantID int64) ([]models.SensitiveDataType, error) {
	return s.types.List(tenantID)
}
func (s *DefinitionService) GetType(id, tenantID int64) (*models.SensitiveDataType, error) {
	return s.types.Get(id, tenantID)
}
func (s *DefinitionService) CreateType(req models.SensitiveDataTypeRequest, tenantID, userID int64) (*models.SensitiveDataType, error) {
	if err := s.validateTypeRefs(req.SecurityClassificationID, req.DefaultSecurityGradeID, tenantID); err != nil {
		return nil, err
	}
	row := &models.SensitiveDataType{TenantID: tenantID, Code: strings.TrimSpace(req.Code), Name: strings.TrimSpace(req.Name), Description: strings.TrimSpace(req.Description), SecurityClassificationID: req.SecurityClassificationID, DefaultSecurityGradeID: req.DefaultSecurityGradeID, CreatedBy: userID}
	if row.Code == "" || row.Name == "" {
		return nil, commonapi.ErrBadRequest
	}
	if err := s.types.Create(row); err != nil {
		return nil, err
	}
	return row, nil
}
func (s *DefinitionService) UpdateType(id, tenantID, userID int64, req models.SensitiveDataTypeRequest) (*models.SensitiveDataType, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, commonapi.ErrBadRequest
	}
	var updated *models.SensitiveDataType
	err := s.db.Transaction(func(tx *gorm.DB) error {
		definitions := s.withDB(tx)
		if err := definitions.validateTypeRefs(req.SecurityClassificationID, req.DefaultSecurityGradeID, tenantID); err != nil {
			return err
		}
		current, err := definitions.types.Get(id, tenantID)
		if err != nil {
			return err
		}
		if err := definitions.types.Update(id, tenantID, req.Version, map[string]interface{}{"name": strings.TrimSpace(req.Name), "description": strings.TrimSpace(req.Description), "security_classification_id": req.SecurityClassificationID, "default_security_grade_id": req.DefaultSecurityGradeID, "updated_by": userID}); err != nil {
			return err
		}
		updated, err = definitions.types.Get(id, tenantID)
		if err != nil {
			return err
		}
		if current.DefaultSecurityGradeID != updated.DefaultSecurityGradeID {
			return recompileCandidateTypeImpact(tx, tenantID, id, s.now().UTC())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}
func (s *DefinitionService) DeleteType(id, tenantID int64) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		definitions := s.withDB(tx)
		if err := rejectReferences(definitions.baselines, tenantID, "sensitive_data_type_id = ?", id); err != nil {
			return err
		}
		if err := rejectReferences(definitions.detectors, tenantID, "sensitive_data_type_id = ?", id); err != nil {
			return err
		}
		if err := rejectReferences(repository.New[models.SensitiveFinding](tx), tenantID, "sensitive_data_type_id = ?", id); err != nil {
			return err
		}
		if err := rejectReferences(repository.New[models.SensitiveFindingReview](tx), tenantID, "sensitive_data_type_id = ?", id); err != nil {
			return err
		}
		if err := rejectReferences(repository.New[models.ResourceSecurityAssessmentRevision](tx), tenantID, "sensitive_data_type_id = ?", id); err != nil {
			return err
		}
		return definitions.types.Delete(id, tenantID)
	})
}

func (s *DefinitionService) ListDetectorCapabilities() []models.DetectorCapability {
	return ListDetectorCapabilities()
}

func (s *DefinitionService) ListDetectors(tenantID int64) ([]models.Detector, error) {
	return s.detectors.List(tenantID)
}

func (s *DefinitionService) GetDetector(id, tenantID int64) (*models.Detector, error) {
	return s.detectors.Get(id, tenantID)
}

func (s *DefinitionService) CreateDetector(req models.DetectorRequest, tenantID, userID int64) (*models.Detector, error) {
	capabilityKey := strings.TrimSpace(req.CapabilityKey)
	if tenantID <= 0 || userID <= 0 || req.SensitiveDataTypeID <= 0 || req.ConfidenceThreshold <= 0 || req.ConfidenceThreshold > 1 {
		return nil, commonapi.ErrBadRequest
	}
	if _, ok := detectorCapability(capabilityKey); !ok {
		return nil, commonapi.ErrBadRequest
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	row := &models.Detector{
		TenantID: tenantID, CapabilityKey: capabilityKey, SensitiveDataTypeID: req.SensitiveDataTypeID,
		ConfidenceThreshold: req.ConfidenceThreshold, Enabled: enabled, CreatedBy: userID,
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		definitions := s.withDB(tx)
		if _, err := definitions.types.Get(row.SensitiveDataTypeID, tenantID); err != nil {
			return err
		}
		if err := definitions.detectors.Create(row); err != nil {
			return err
		}
		return queueDetectorImpactDiscovery(tx, tenantID, userID, s.now().UTC())
	})
	if err != nil {
		return nil, err
	}
	return row, nil
}

func (s *DefinitionService) UpdateDetector(id, tenantID, userID int64, req models.DetectorRequest) (*models.Detector, error) {
	capabilityKey := strings.TrimSpace(req.CapabilityKey)
	if tenantID <= 0 || userID <= 0 || id <= 0 || req.Version <= 0 || req.SensitiveDataTypeID <= 0 || req.ConfidenceThreshold <= 0 || req.ConfidenceThreshold > 1 || req.Enabled == nil {
		return nil, commonapi.ErrBadRequest
	}
	if _, ok := detectorCapability(capabilityKey); !ok {
		return nil, commonapi.ErrBadRequest
	}
	var updated *models.Detector
	err := s.db.Transaction(func(tx *gorm.DB) error {
		definitions := s.withDB(tx)
		if _, err := definitions.types.Get(req.SensitiveDataTypeID, tenantID); err != nil {
			return err
		}
		current, err := definitions.detectors.Get(id, tenantID)
		if err != nil {
			return err
		}
		if err := definitions.detectors.Update(id, tenantID, req.Version, map[string]interface{}{
			"capability_key": capabilityKey, "sensitive_data_type_id": req.SensitiveDataTypeID,
			"confidence_threshold": req.ConfidenceThreshold, "enabled": *req.Enabled, "updated_by": userID,
		}); err != nil {
			return err
		}
		updated, err = definitions.detectors.Get(id, tenantID)
		if err != nil {
			return err
		}
		if current.CapabilityKey != updated.CapabilityKey || current.SensitiveDataTypeID != updated.SensitiveDataTypeID || current.ConfidenceThreshold != updated.ConfidenceThreshold || current.Enabled != updated.Enabled {
			return queueDetectorImpactDiscovery(tx, tenantID, userID, s.now().UTC())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *DefinitionService) DeleteDetector(id, tenantID, userID, version int64) error {
	if tenantID <= 0 || userID <= 0 || id <= 0 || version <= 0 {
		return commonapi.ErrBadRequest
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Where("id = ? AND tenant_id = ? AND version = ?", id, tenantID, version).Delete(&models.Detector{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			var count int64
			if err := tx.Model(&models.Detector{}).Where("id = ? AND tenant_id = ?", id, tenantID).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				return commonapi.ErrNotFound
			}
			return repository.ErrVersionConflict
		}
		return queueDetectorImpactDiscovery(tx, tenantID, userID, s.now().UTC())
	})
}

func queueDetectorImpactDiscovery(tx *gorm.DB, tenantID, actorID int64, now time.Time) error {
	var enrollments []models.ProtectionEnrollment
	if err := tx.Where("tenant_id = ? AND state IN ?", tenantID, []string{models.EnrollmentStateEnrolling, models.EnrollmentStateActive}).Order("id ASC").Find(&enrollments).Error; err != nil {
		return err
	}
	if len(enrollments) == 0 {
		return nil
	}
	ids := make([]string, 0, len(enrollments))
	for _, enrollment := range enrollments {
		ids = append(ids, enrollment.ID)
	}
	var active []commonexecution.TaskExecution
	activeQuery := tx
	if tx.Dialector.Name() == "postgres" {
		activeQuery = activeQuery.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := activeQuery.Where(
		"tenant_id = ? AND module = ? AND task_type = ? AND source = ? AND source_task_id IN ? AND status IN ?",
		tenantID, commonexecution.ModuleSecurity, commonexecution.TaskTypeSensitiveDataDiscovery, commonexecution.ModuleSecurity,
		ids, []string{commonexecution.ExecutionStatusPending, commonexecution.ExecutionStatusRunning},
	).Find(&active).Error; err != nil {
		return err
	}
	busy := make(map[string]struct{}, len(active))
	for _, execution := range active {
		if execution.SourceTaskID != nil {
			if execution.Status == commonexecution.ExecutionStatusRunning {
				return commonapi.ErrConflict
			}
			busy[*execution.SourceTaskID] = struct{}{}
		}
	}
	for _, enrollment := range enrollments {
		if _, exists := busy[enrollment.ID]; exists {
			continue
		}
		execution := newDiscoveryExecution(enrollment, int(actorID), commonexecution.TriggerTypeEvent, now)
		if err := tx.Create(&execution).Error; err != nil {
			return err
		}
	}
	return nil
}
func (s *DefinitionService) validateTypeRefs(classificationID, gradeID, tenantID int64) error {
	if _, err := s.classifications.Get(classificationID, tenantID); err != nil {
		return err
	}
	_, err := s.grades.Get(gradeID, tenantID)
	return err
}

func (s *DefinitionService) ListBaselines(tenantID int64) ([]models.ProtectionBaseline, error) {
	return s.baselines.List(tenantID)
}
func (s *DefinitionService) GetBaseline(id, tenantID int64) (*models.ProtectionBaseline, error) {
	return s.baselines.Get(id, tenantID)
}
func (s *DefinitionService) CreateBaseline(req models.ProtectionBaselineRequest, tenantID, userID int64) (*models.ProtectionBaseline, error) {
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	row := &models.ProtectionBaseline{TenantID: tenantID, SensitiveDataTypeID: req.SensitiveDataTypeID, SecurityGradeID: req.SecurityGradeID, Effect: req.Effect, Algorithm: req.Algorithm, KeepPrefix: req.KeepPrefix, KeepSuffix: req.KeepSuffix, InvalidValueEffect: req.InvalidValueEffect, Enabled: enabled, CreatedBy: userID}
	if row.InvalidValueEffect == "" {
		row.InvalidValueEffect = "suppress"
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		definitions := s.withDB(tx)
		if err := definitions.validateBaseline(req, tenantID); err != nil {
			return err
		}
		if err := definitions.baselines.Create(row); err != nil {
			return err
		}
		return recompileBaselineImpact(tx, tenantID, []baselineBinding{{SensitiveDataTypeID: row.SensitiveDataTypeID, SecurityGradeID: row.SecurityGradeID}}, s.now().UTC())
	})
	if err != nil {
		return nil, err
	}
	return row, nil
}
func (s *DefinitionService) UpdateBaseline(id, tenantID, userID int64, req models.ProtectionBaselineRequest) (*models.ProtectionBaseline, error) {
	var updated *models.ProtectionBaseline
	err := s.db.Transaction(func(tx *gorm.DB) error {
		definitions := s.withDB(tx)
		if err := definitions.validateBaseline(req, tenantID); err != nil {
			return err
		}
		current, err := definitions.baselines.Get(id, tenantID)
		if err != nil {
			return err
		}
		enabled := current.Enabled
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		invalid := req.InvalidValueEffect
		if invalid == "" {
			invalid = "suppress"
		}
		if err := definitions.baselines.Update(id, tenantID, req.Version, map[string]interface{}{"sensitive_data_type_id": req.SensitiveDataTypeID, "security_grade_id": req.SecurityGradeID, "effect": req.Effect, "algorithm": req.Algorithm, "keep_prefix": req.KeepPrefix, "keep_suffix": req.KeepSuffix, "invalid_value_effect": invalid, "enabled": enabled, "updated_by": userID}); err != nil {
			return err
		}
		updated, err = definitions.baselines.Get(id, tenantID)
		if err != nil {
			return err
		}
		return recompileBaselineImpact(tx, tenantID, []baselineBinding{
			{SensitiveDataTypeID: current.SensitiveDataTypeID, SecurityGradeID: current.SecurityGradeID},
			{SensitiveDataTypeID: updated.SensitiveDataTypeID, SecurityGradeID: updated.SecurityGradeID},
		}, s.now().UTC())
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}
func (s *DefinitionService) DeleteBaseline(id, tenantID int64, version int64) error {
	if version <= 0 {
		return commonapi.ErrBadRequest
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		definitions := s.withDB(tx)
		current, err := definitions.baselines.Get(id, tenantID)
		if err != nil {
			return err
		}
		result := tx.Where("id = ? AND tenant_id = ? AND version = ?", id, tenantID, version).Delete(&models.ProtectionBaseline{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return repository.ErrVersionConflict
		}
		return recompileBaselineImpact(tx, tenantID, []baselineBinding{{SensitiveDataTypeID: current.SensitiveDataTypeID, SecurityGradeID: current.SecurityGradeID}}, s.now().UTC())
	})
}
func (s *DefinitionService) validateBaseline(req models.ProtectionBaselineRequest, tenantID int64) error {
	if _, err := s.types.Get(req.SensitiveDataTypeID, tenantID); err != nil {
		return err
	}
	if _, err := s.grades.Get(req.SecurityGradeID, tenantID); err != nil {
		return err
	}
	if req.KeepPrefix < 0 || req.KeepSuffix < 0 {
		return commonapi.ErrBadRequest
	}
	switch req.Effect {
	case dataprotection.EffectMask:
		if req.Algorithm != dataprotection.AlgorithmKeepPrefixSuffixV1 {
			return commonapi.ErrBadRequest
		}
	case dataprotection.EffectSuppress, dataprotection.EffectDeny:
		if req.Algorithm != "" || req.KeepPrefix != 0 || req.KeepSuffix != 0 {
			return commonapi.ErrBadRequest
		}
	default:
		return commonapi.ErrBadRequest
	}
	invalidEffect := req.InvalidValueEffect
	if invalidEffect == "" {
		invalidEffect = dataprotection.EffectSuppress
	}
	if invalidEffect != dataprotection.EffectSuppress && invalidEffect != dataprotection.EffectDeny {
		return commonapi.ErrBadRequest
	}
	if req.Effect == dataprotection.EffectDeny && invalidEffect != dataprotection.EffectDeny {
		return commonapi.ErrBadRequest
	}
	return nil
}

func rejectReferences[T any](repo *repository.Repository[T], tenantID int64, query string, args ...interface{}) error {
	count, err := repo.CountWhere(tenantID, query, args...)
	if err != nil {
		return err
	}
	if count > 0 {
		return commonapi.ErrConflict
	}
	return nil
}

func (s *DefinitionService) withDB(db *gorm.DB) *DefinitionService {
	definitions := NewDefinitionService(db)
	definitions.now = s.now
	return definitions
}
