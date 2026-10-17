package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataprotection"
	commonexecution "github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/security/internal/models"
	"github.com/addp/security/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrProjectionCursorConflict              = errors.New("protection projection cursor conflict")
	ErrNoSupportedFindingsReleaseUnavailable = errors.New("no-supported-findings release unavailable")
	ErrDiscoveryExecutionInProgress          = errors.New("protection discovery execution in progress")
	ErrLiveEnrollmentAlreadyExists           = errors.New("live protection enrollment already exists")
)

type EnrollmentService struct {
	db  *gorm.DB
	now func() time.Time
}

func NewEnrollmentService(db *gorm.DB) *EnrollmentService {
	return &EnrollmentService{db: db, now: time.Now}
}

func (s *EnrollmentService) Create(ctx context.Context, tenantID, userID int64, request models.CreateProtectionEnrollmentRequest) (*models.ProtectionEnrollmentResponse, error) {
	locator, err := resourcetree.ParseURI(strings.TrimSpace(request.Locator))
	if tenantID <= 0 || userID <= 0 || err != nil || locator.EngineID == 0 || locator.ItemID == nil || *locator.ItemID == 0 || locator.NodeID != nil {
		return nil, commonapi.ErrBadRequest
	}
	fullName := strings.TrimSpace(locator.FullName())
	itemType := strings.TrimSpace(string(locator.Type))
	if fullName == "" || itemType == "" || resourcetree.IsRootResourceType(locator.Type) {
		return nil, commonapi.ErrBadRequest
	}
	target := dataprotection.ResourceReference{
		OwnerModule: "meta", ResourceType: "data_item",
		ResourceIdentity: commonmodels.GenerateItemFingerprint(locator.EngineID, fullName),
	}
	now := s.now().UTC()
	deadline, err := protectionValidityDeadline(request.ValidityMode, request.ExpiresAt, now, true)
	if err != nil {
		return nil, err
	}
	var enrollment models.ProtectionEnrollment
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var createErr error
		enrollment, createErr = createProtectionEnrollment(tx, tenantID, userID, target, models.ProtectionTargetSnapshot{
			EngineID: locator.EngineID, ItemType: itemType, FullName: fullName,
		}, now, deadline)
		return createErr
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, tenantID, enrollment.ID)
}

func (s *EnrollmentService) ReEnroll(ctx context.Context, tenantID, userID int64, id string, request models.ReEnrollProtectionEnrollmentRequest) (*models.ReEnrollProtectionEnrollmentResponse, error) {
	if tenantID <= 0 || userID <= 0 || uuid.Validate(id) != nil || request.Version <= 0 {
		return nil, commonapi.ErrBadRequest
	}
	now := s.now().UTC()
	var created models.ProtectionEnrollment
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var source models.ProtectionEnrollment
		query := tx
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.Where("tenant_id = ? AND id = ?", tenantID, id).First(&source).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return commonapi.ErrNotFound
			}
			return err
		}
		if source.Version != request.Version {
			return repository.ErrVersionConflict
		}
		if source.State != models.EnrollmentStateReleased {
			return commonapi.ErrConflict
		}
		var liveCount int64
		if err := tx.Model(&models.ProtectionEnrollment{}).
			Where("tenant_id = ? AND target_owner = ? AND target_type = ? AND target_identity = ? AND state <> ?", tenantID, source.TargetOwner, source.TargetType, source.TargetIdentity, models.EnrollmentStateReleased).
			Count(&liveCount).Error; err != nil {
			return err
		}
		if liveCount != 0 {
			return ErrLiveEnrollmentAlreadyExists
		}
		var createErr error
		created, createErr = createProtectionEnrollment(tx, tenantID, userID, source.Target(), source.TargetSnapshot(), now, nil)
		if errors.Is(createErr, commonapi.ErrConflict) {
			return ErrLiveEnrollmentAlreadyExists
		}
		if createErr != nil {
			return createErr
		}
		update := tx.Model(&source).Where("tenant_id = ? AND id = ? AND version = ?", tenantID, source.ID, request.Version).Updates(map[string]any{
			"version": gorm.Expr("version + 1"), "updated_at": now,
		})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return repository.ErrVersionConflict
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	enrollment, err := s.Get(ctx, tenantID, created.ID)
	if err != nil {
		return nil, err
	}
	return &models.ReEnrollProtectionEnrollmentResponse{
		SourceEnrollmentVersion: request.Version + 1,
		Enrollment:              *enrollment,
	}, nil
}

func createProtectionEnrollment(tx *gorm.DB, tenantID, userID int64, target dataprotection.ResourceReference, snapshot models.ProtectionTargetSnapshot, now time.Time, deadline *time.Time) (models.ProtectionEnrollment, error) {
	if err := dataprotection.VerifyTargetEngine(target, snapshot.EngineID, snapshot.FullName); err != nil {
		return models.ProtectionEnrollment{}, err
	}
	enrollment := models.ProtectionEnrollment{
		ProtectionExpiresAt: deadline,
		ID:                  uuid.NewString(), TenantID: tenantID,
		TargetOwner: target.OwnerModule, TargetType: target.ResourceType,
		TargetIdentity: target.ResourceIdentity, TargetEngineID: snapshot.EngineID,
		TargetItemType: snapshot.ItemType, TargetFullName: snapshot.FullName,
		State: models.EnrollmentStateActivating, Version: 1, CreatedBy: userID,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.Create(&enrollment).Error; err != nil {
		if repository.IsConflict(err) {
			return models.ProtectionEnrollment{}, commonapi.ErrConflict
		}
		return models.ProtectionEnrollment{}, err
	}
	for _, owner := range allRequiredProtectionOwners() {
		if err := installEnrollmentOwnerProjection(tx, enrollment, owner, now); err != nil {
			return models.ProtectionEnrollment{}, err
		}
	}
	return enrollment, nil
}

func (s *EnrollmentService) List(ctx context.Context, tenantID int64, scope string, page, pageSize int64, filter models.ProtectionEnrollmentFilter) (*models.ProtectionEnrollmentListResponse, error) {
	if tenantID <= 0 || page <= 0 || pageSize <= 0 || pageSize > 100 {
		return nil, commonapi.ErrBadRequest
	}
	if scope != models.EnrollmentListScopeCurrent && scope != models.EnrollmentListScopeReleased && scope != models.EnrollmentListScopeAll {
		return nil, commonapi.ErrBadRequest
	}
	filter.ResourceName = strings.TrimSpace(filter.ResourceName)
	if len([]rune(filter.ResourceName)) > 200 {
		return nil, commonapi.ErrBadRequest
	}
	if filter.State != "" && filter.State != models.EnrollmentStateActivating && filter.State != models.EnrollmentStateEnrolling && filter.State != models.EnrollmentStateActive && filter.State != models.EnrollmentStateReleasing && filter.State != models.EnrollmentStateReleased {
		return nil, commonapi.ErrBadRequest
	}
	query := applyEnrollmentListScope(s.db.WithContext(ctx).Model(&models.ProtectionEnrollment{}).Where("tenant_id = ?", tenantID), scope)
	if filter.ResourceName != "" {
		literal := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(filter.ResourceName))
		query = query.Where("LOWER(target_full_name) LIKE ? ESCAPE '!'", "%"+literal+"%")
	}
	if filter.EngineID > 0 {
		query = query.Where("target_engine_id = ?", filter.EngineID)
	}
	if filter.State != "" {
		query = query.Where("state = ?", filter.State)
	}
	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, err
	}
	var enrollments []models.ProtectionEnrollment
	order := "created_at DESC, id ASC"
	if scope == models.EnrollmentListScopeReleased {
		order = "released_at DESC, id ASC"
	}
	if err := query.Session(&gorm.Session{}).Order(order).Offset(int((page - 1) * pageSize)).Limit(int(pageSize)).Find(&enrollments).Error; err != nil {
		return nil, err
	}
	data, err := s.buildResponses(ctx, enrollments)
	if err != nil {
		return nil, err
	}
	totalPages := int((total + pageSize - 1) / pageSize)
	return &models.ProtectionEnrollmentListResponse{Data: data, Total: total, Page: int(page), PageSize: int(pageSize), TotalPages: totalPages}, nil
}

func applyEnrollmentListScope(query *gorm.DB, scope string) *gorm.DB {
	switch scope {
	case models.EnrollmentListScopeCurrent:
		return query.Where("state IN ?", []string{
			models.EnrollmentStateActivating,
			models.EnrollmentStateEnrolling,
			models.EnrollmentStateActive,
			models.EnrollmentStateReleasing,
		})
	case models.EnrollmentListScopeReleased:
		return query.Where("state = ?", models.EnrollmentStateReleased)
	default:
		return query
	}
}

func (s *EnrollmentService) Get(ctx context.Context, tenantID int64, id string) (*models.ProtectionEnrollmentResponse, error) {
	if tenantID <= 0 || uuid.Validate(id) != nil {
		return nil, commonapi.ErrBadRequest
	}
	var enrollment models.ProtectionEnrollment
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id).First(&enrollment).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, commonapi.ErrNotFound
		}
		return nil, err
	}
	responses, err := s.buildResponses(ctx, []models.ProtectionEnrollment{enrollment})
	if err != nil {
		return nil, err
	}
	return &responses[0], nil
}

func (s *EnrollmentService) Release(ctx context.Context, tenantID, actorID int64, id string, request models.ReleaseProtectionEnrollmentRequest) (*models.ProtectionEnrollmentResponse, error) {
	basis := strings.TrimSpace(request.Basis)
	reason := strings.TrimSpace(request.Reason)
	if tenantID <= 0 || actorID <= 0 || uuid.Validate(id) != nil || request.Version <= 0 || reason == "" ||
		(basis != models.ReleaseBasisManual && basis != models.ReleaseBasisNoSupportedFindings) {
		return nil, commonapi.ErrBadRequest
	}
	now := s.now().UTC()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var enrollment models.ProtectionEnrollment
		query := tx
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.Where("tenant_id = ? AND id = ?", tenantID, id).First(&enrollment).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return commonapi.ErrNotFound
			}
			return err
		}
		if enrollment.Version != request.Version {
			return repository.ErrVersionConflict
		}
		if enrollment.State == models.EnrollmentStateReleasing || enrollment.State == models.EnrollmentStateReleased {
			return commonapi.ErrConflict
		}
		releaseSourceSnapshotHash := ""
		if basis == models.ReleaseBasisNoSupportedFindings {
			if err := validateNoSupportedFindingsRelease(tx, enrollment); err != nil {
				return err
			}
			releaseSourceSnapshotHash = enrollment.LatestSourceSnapshotHash
		}
		if err := tx.Model(&enrollment).Updates(map[string]any{
			"state": models.EnrollmentStateReleasing, "release_reason": reason,
			"release_basis": basis, "release_requested_by": actorID, "release_requested_at": now,
			"release_source_snapshot_hash": releaseSourceSnapshotHash,
			"version":                      gorm.Expr("version + 1"), "updated_at": now,
		}).Error; err != nil {
			return err
		}
		var projections []models.ProtectionProjectionRecord
		if err := tx.Where("tenant_id = ? AND enrollment_id = ?", tenantID, id).Order("consumer_owner ASC").Find(&projections).Error; err != nil {
			return err
		}
		for _, projection := range projections {
			revision, err := nextRevision(projection.Revision)
			if err != nil {
				return err
			}
			change := models.ProtectionProjectionChange{
				ChangeID: uuid.NewString(), TenantID: tenantID, EnrollmentID: id,
				ConsumerOwner: projection.ConsumerOwner, Operation: dataprotection.ChangeOperationRelease,
				ProjectionID: projection.ID, Revision: revision,
				TargetOwner: enrollment.TargetOwner, TargetType: enrollment.TargetType,
				TargetIdentity: enrollment.TargetIdentity,
				CreatedAt:      now,
			}
			if err := tx.Create(&change).Error; err != nil {
				return err
			}
			if err := tx.Model(&projection).Updates(map[string]any{"revision": revision, "release_sequence": change.Sequence, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, tenantID, id)
}

func validateNoSupportedFindingsRelease(tx *gorm.DB, enrollment models.ProtectionEnrollment) error {
	if enrollment.LastDiscoveredAt == nil || strings.TrimSpace(enrollment.LatestDiscoveryExecutionID) == "" {
		return ErrNoSupportedFindingsReleaseUnavailable
	}
	var findingCount int64
	if err := tx.Model(&models.SensitiveFinding{}).
		Where("tenant_id = ? AND enrollment_id = ? AND discovery_execution_id = ?", enrollment.TenantID, enrollment.ID, enrollment.LatestDiscoveryExecutionID).
		Count(&findingCount).Error; err != nil {
		return err
	}
	if findingCount != 0 {
		return ErrNoSupportedFindingsReleaseUnavailable
	}
	var activeDiscoveryCount int64
	if err := tx.Model(&commonexecution.TaskExecution{}).
		Where("tenant_id = ? AND module = ? AND task_type = ? AND source = ? AND source_task_id = ? AND status IN ?", enrollment.TenantID, commonexecution.ModuleSecurity, commonexecution.TaskTypeSensitiveDataDiscovery, commonexecution.ModuleSecurity, enrollment.ID, []string{commonexecution.ExecutionStatusPending, commonexecution.ExecutionStatusRunning}).
		Count(&activeDiscoveryCount).Error; err != nil {
		return err
	}
	if activeDiscoveryCount != 0 {
		return ErrNoSupportedFindingsReleaseUnavailable
	}
	return nil
}

func (s *EnrollmentService) CreateDiscoveryExecution(ctx context.Context, tenantID, userID int64, id string, request models.CreateProtectionDiscoveryExecutionRequest) (*models.ProtectionDiscoveryExecutionResponse, error) {
	if tenantID <= 0 || userID <= 0 || uuid.Validate(id) != nil || request.Version <= 0 {
		return nil, commonapi.ErrBadRequest
	}
	var response *models.ProtectionDiscoveryExecutionResponse
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var enrollment models.ProtectionEnrollment
		query := tx
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.Where("tenant_id = ? AND id = ? AND state IN ?", tenantID, id, []string{models.EnrollmentStateEnrolling, models.EnrollmentStateActive}).First(&enrollment).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return commonapi.ErrNotFound
			}
			return err
		}
		if enrollment.Version != request.Version {
			return repository.ErrVersionConflict
		}
		var active int64
		if err := tx.Model(&commonexecution.TaskExecution{}).
			Where("tenant_id = ? AND module = ? AND task_type = ? AND source = ? AND source_task_id = ? AND status IN ?", tenantID, commonexecution.ModuleSecurity, commonexecution.TaskTypeSensitiveDataDiscovery, commonexecution.ModuleSecurity, enrollment.ID, []string{commonexecution.ExecutionStatusPending, commonexecution.ExecutionStatusRunning}).
			Count(&active).Error; err != nil {
			return err
		}
		if active != 0 {
			return ErrDiscoveryExecutionInProgress
		}
		now := s.now().UTC()
		execution := newDiscoveryExecution(enrollment, int(userID), commonexecution.TriggerTypeManual, now)
		if err := tx.Create(&execution).Error; err != nil {
			return err
		}
		update := tx.Model(&enrollment).Where("version = ?", request.Version).Updates(map[string]interface{}{
			"version": gorm.Expr("version + 1"), "updated_at": now,
		})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return repository.ErrVersionConflict
		}
		response = &models.ProtectionDiscoveryExecutionResponse{
			ExecutionID: execution.ExecutionID, EnrollmentID: enrollment.ID,
			EnrollmentVersion: request.Version + 1, Status: execution.Status, CreatedAt: execution.CreatedAt,
		}
		return nil
	})
	return response, err
}

func (s *EnrollmentService) ListChanges(ctx context.Context, tenantID int64, consumerOwner, afterCursor string, limit int) (*dataprotection.ProjectionChangesResponse, error) {
	if tenantID <= 0 || !isRequiredOwner(consumerOwner) || limit <= 0 || limit > 500 {
		return nil, commonapi.ErrBadRequest
	}
	afterSequence, err := decodeProjectionCursor(afterCursor)
	if err != nil {
		return nil, ErrProjectionCursorConflict
	}
	if afterSequence > 0 {
		var count int64
		if err := s.db.WithContext(ctx).Model(&models.ProtectionProjectionChange{}).Where("tenant_id = ? AND consumer_owner = ? AND sequence = ?", tenantID, consumerOwner, afterSequence).Count(&count).Error; err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, ErrProjectionCursorConflict
		}
	}
	var rows []models.ProtectionProjectionChange
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND consumer_owner = ? AND sequence > ?", tenantID, consumerOwner, afterSequence).Order("sequence ASC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return nil, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	changes := make([]dataprotection.ProjectionChange, 0, len(rows))
	nextSequence := afterSequence
	for _, row := range rows {
		change := dataprotection.ProjectionChange{ChangeID: row.ChangeID, Operation: row.Operation}
		if row.Operation == dataprotection.ChangeOperationUpsert {
			if row.ProjectionPayload == nil || json.Unmarshal([]byte(*row.ProjectionPayload), &change.Projection) != nil || change.Projection == nil {
				return nil, errors.New("stored protection projection change is invalid")
			}
		} else {
			change.Release = &dataprotection.ProjectionRelease{
				ProjectionID: row.ProjectionID, Revision: row.Revision,
				Target: dataprotection.ResourceReference{OwnerModule: row.TargetOwner, ResourceType: row.TargetType, ResourceIdentity: row.TargetIdentity},
			}
		}
		changes = append(changes, change)
		nextSequence = row.Sequence
	}
	nextCursor := afterCursor
	if nextSequence > 0 {
		nextCursor = encodeProjectionCursor(nextSequence)
	}
	return &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, Changes: changes, NextCursor: nextCursor, HasMore: hasMore}, nil
}

func (s *EnrollmentService) Acknowledge(ctx context.Context, tenantID int64, consumerOwner, cursor string) error {
	if tenantID <= 0 || !isRequiredOwner(consumerOwner) {
		return commonapi.ErrBadRequest
	}
	sequence, err := decodeProjectionCursor(cursor)
	if err != nil || sequence <= 0 {
		return ErrProjectionCursorConflict
	}
	now := s.now().UTC()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var change models.ProtectionProjectionChange
		if err := tx.Where("tenant_id = ? AND consumer_owner = ? AND sequence = ?", tenantID, consumerOwner, sequence).First(&change).Error; err != nil {
			return ErrProjectionCursorConflict
		}
		var acknowledgement models.ProtectionProjectionAcknowledgement
		err := tx.Where("tenant_id = ? AND consumer_owner = ?", tenantID, consumerOwner).First(&acknowledgement).Error
		if err == nil && sequence < acknowledgement.Sequence {
			return ErrProjectionCursorConflict
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			acknowledgement = models.ProtectionProjectionAcknowledgement{TenantID: tenantID, ConsumerOwner: consumerOwner, Sequence: sequence, AppliedCursor: cursor, UpdatedAt: now}
			if err := tx.Create(&acknowledgement).Error; err != nil {
				return err
			}
		} else if sequence > acknowledgement.Sequence {
			if err := tx.Model(&acknowledgement).Updates(map[string]any{"sequence": sequence, "applied_cursor": cursor, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		return s.advanceEnrollmentStates(tx, tenantID, now)
	})
}

func (s *EnrollmentService) advanceEnrollmentStates(tx *gorm.DB, tenantID int64, now time.Time) error {
	var enrollments []models.ProtectionEnrollment
	if err := tx.Where("tenant_id = ? AND state IN ?", tenantID, []string{models.EnrollmentStateActivating, models.EnrollmentStateEnrolling, models.EnrollmentStateReleasing}).Find(&enrollments).Error; err != nil {
		return err
	}
	for _, enrollment := range enrollments {
		var projections []models.ProtectionProjectionRecord
		if err := tx.Where("tenant_id = ? AND enrollment_id = ?", tenantID, enrollment.ID).Find(&projections).Error; err != nil {
			return err
		}
		covered := len(projections) == len(requiredProtectionOwnerContracts)
		for _, projection := range projections {
			if enrollment.State == models.EnrollmentStateEnrolling && projection.State != dataprotection.ProjectionStateActive {
				covered = false
				break
			}
			requiredSequence := projection.PublishedSequence
			if enrollment.State == models.EnrollmentStateReleasing {
				if projection.ReleaseSequence == nil {
					covered = false
					break
				}
				requiredSequence = *projection.ReleaseSequence
			}
			var acknowledgement models.ProtectionProjectionAcknowledgement
			if err := tx.Where("tenant_id = ? AND consumer_owner = ? AND sequence >= ?", tenantID, projection.ConsumerOwner, requiredSequence).First(&acknowledgement).Error; err != nil {
				covered = false
				break
			}
		}
		if !covered {
			continue
		}
		wasActivating := enrollment.State == models.EnrollmentStateActivating
		values := map[string]any{"version": gorm.Expr("version + 1"), "updated_at": now}
		if wasActivating {
			values["state"] = models.EnrollmentStateEnrolling
		} else if enrollment.State == models.EnrollmentStateEnrolling {
			values["state"] = models.EnrollmentStateActive
		} else {
			values["state"] = models.EnrollmentStateReleased
			values["released_at"] = now
		}
		update := tx.Model(&models.ProtectionEnrollment{}).Where("tenant_id = ? AND id = ? AND state = ?", tenantID, enrollment.ID, enrollment.State).Updates(values)
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected == 0 {
			continue
		}
		if wasActivating {
			execution := newDiscoveryExecution(enrollment, int(enrollment.CreatedBy), commonexecution.TriggerTypeEvent, now)
			if err := tx.Create(&execution).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *EnrollmentService) buildResponses(ctx context.Context, enrollments []models.ProtectionEnrollment) ([]models.ProtectionEnrollmentResponse, error) {
	responses := make([]models.ProtectionEnrollmentResponse, 0, len(enrollments))
	if len(enrollments) == 0 {
		return responses, nil
	}
	tenantID := enrollments[0].TenantID
	enrollmentIDs := make([]string, 0, len(enrollments))
	for _, enrollment := range enrollments {
		if enrollment.TenantID != tenantID {
			return nil, errors.New("protection enrollment response batch crosses tenants")
		}
		enrollmentIDs = append(enrollmentIDs, enrollment.ID)
	}
	var projections []models.ProtectionProjectionRecord
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND enrollment_id IN ?", tenantID, enrollmentIDs).Order("enrollment_id ASC, consumer_owner ASC").Find(&projections).Error; err != nil {
		return nil, err
	}
	var acknowledgements []models.ProtectionProjectionAcknowledgement
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&acknowledgements).Error; err != nil {
		return nil, err
	}
	ackByOwner := make(map[string]models.ProtectionProjectionAcknowledgement, len(acknowledgements))
	for _, acknowledgement := range acknowledgements {
		ackByOwner[acknowledgement.ConsumerOwner] = acknowledgement
	}
	projectionsByEnrollment := make(map[string][]models.ProtectionProjectionRecord, len(enrollments))
	for _, projection := range projections {
		projectionsByEnrollment[projection.EnrollmentID] = append(projectionsByEnrollment[projection.EnrollmentID], projection)
	}
	type findingCountRow struct {
		EnrollmentID  string
		FindingCount  int64
		ReviewedCount int64
	}
	var findingCounts []findingCountRow
	if err := s.db.WithContext(ctx).Model(&models.SensitiveFinding{}).
		Select("security.sensitive_findings.enrollment_id, COUNT(*) AS finding_count, COUNT(security.sensitive_finding_reviews.id) AS reviewed_count").
		Joins("JOIN security.protection_enrollments ON security.protection_enrollments.tenant_id = security.sensitive_findings.tenant_id AND security.protection_enrollments.id = security.sensitive_findings.enrollment_id AND security.protection_enrollments.latest_discovery_execution_id = security.sensitive_findings.discovery_execution_id").
		Joins("LEFT JOIN security.sensitive_finding_reviews ON security.sensitive_finding_reviews.tenant_id = security.sensitive_findings.tenant_id AND security.sensitive_finding_reviews.finding_id = security.sensitive_findings.id").
		Where("security.sensitive_findings.tenant_id = ? AND security.sensitive_findings.enrollment_id IN ?", tenantID, enrollmentIDs).
		Group("security.sensitive_findings.enrollment_id").
		Scan(&findingCounts).Error; err != nil {
		return nil, err
	}
	findingCountByEnrollment := make(map[string]findingCountRow, len(findingCounts))
	for _, row := range findingCounts {
		findingCountByEnrollment[row.EnrollmentID] = row
	}
	now := s.now().UTC()
	for _, enrollment := range enrollments {
		progress, err := buildOwnerProgress(enrollment, projectionsByEnrollment[enrollment.ID], ackByOwner, now)
		if err != nil {
			return nil, err
		}
		discoverySummary := models.ProtectionDiscoverySummary{Status: models.DiscoverySummaryStatusNotCompleted}
		if enrollment.LastDiscoveredAt != nil && strings.TrimSpace(enrollment.LatestDiscoveryExecutionID) != "" {
			discoverySummary.Status = models.DiscoverySummaryStatusCompleted
			counts := findingCountByEnrollment[enrollment.ID]
			discoverySummary.FindingCount = counts.FindingCount
			discoverySummary.ReviewedCount = counts.ReviewedCount
			discoverySummary.PendingReviewCount = counts.FindingCount - counts.ReviewedCount
		}
		responses = append(responses, models.ProtectionEnrollmentResponse{
			ID: enrollment.ID, Target: enrollment.Target(), TargetSnapshot: enrollment.TargetSnapshot(), State: enrollment.State,
			ValidityMode: enrollment.ValidityMode(), ExpiresAt: enrollment.ProtectionExpiresAt, ValidityUpdatedBy: enrollment.ValidityUpdatedBy, ValidityUpdatedAt: enrollment.ValidityUpdatedAt,
			Version: enrollment.Version, ReleaseReason: enrollment.ReleaseReason, ReleaseBasis: enrollment.ReleaseBasis,
			ReleaseRequestedBy: enrollment.ReleaseRequestedBy, ReleaseRequestedAt: enrollment.ReleaseRequestedAt,
			ReleaseSourceSnapshotHash: enrollment.ReleaseSourceSnapshotHash,
			LatestSourceSnapshotHash:  enrollment.LatestSourceSnapshotHash, LastDiscoveredAt: enrollment.LastDiscoveredAt,
			LatestDiscoveryExecutionID: enrollment.LatestDiscoveryExecutionID,
			DiscoverySummary:           discoverySummary, OwnerProgress: progress, CreatedBy: enrollment.CreatedBy,
			ReleasedAt: enrollment.ReleasedAt, CreatedAt: enrollment.CreatedAt, UpdatedAt: enrollment.UpdatedAt,
		})
	}
	return responses, nil
}

func buildOwnerProgress(enrollment models.ProtectionEnrollment, projections []models.ProtectionProjectionRecord, ackByOwner map[string]models.ProtectionProjectionAcknowledgement, now time.Time) ([]models.ProtectionOwnerProgress, error) {
	progress := make([]models.ProtectionOwnerProgress, 0, len(projections))
	for _, projection := range projections {
		requiredSequence := projection.PublishedSequence
		if enrollment.State == models.EnrollmentStateReleasing || enrollment.State == models.EnrollmentStateReleased {
			if projection.ReleaseSequence != nil {
				requiredSequence = *projection.ReleaseSequence
			}
		}
		acknowledgement, exists := ackByOwner[projection.ConsumerOwner]
		var payload dataprotection.Projection
		if err := json.Unmarshal([]byte(projection.ProjectionPayload), &payload); err != nil {
			return nil, fmt.Errorf("decode protection projection %s: %w", projection.ID, err)
		}
		rules := projectionRuleSummaries(payload, now)
		item := models.ProtectionOwnerProgress{
			ValidFrom: payload.ValidFrom, ExpiresAt: payload.ExpiresAt, Validity: projectionTimeValidity(enrollment.State, payload, now), ExpiringSoon: payload.ExpiresAt != nil && now.Before(*payload.ExpiresAt) && payload.ExpiresAt.Sub(now) <= 7*24*time.Hour,
			ConsumerOwner: projection.ConsumerOwner, ProjectionID: projection.ID,
			Revision: projection.Revision, ProjectionState: projection.State, Rules: rules,
			PublishedCursor: encodeProjectionCursor(requiredSequence),
			Acknowledged:    exists && acknowledgement.Sequence >= requiredSequence,
		}
		if item.Acknowledged {
			acknowledgedAt := acknowledgement.UpdatedAt
			item.AcknowledgedAt = &acknowledgedAt
		}
		progress = append(progress, item)
	}
	return progress, nil
}

func projectionTimeValidity(state string, projection dataprotection.Projection, now time.Time) string {
	if state == models.EnrollmentStateReleased {
		return "released"
	}
	if projection.ExpiresAt != nil && !now.Before(*projection.ExpiresAt) {
		return "expired"
	}
	if now.Before(projection.ValidFrom) {
		return "not_yet_valid"
	}
	return "valid"
}

func projectionRuleSummaries(projection dataprotection.Projection, now time.Time) []models.ProtectionOwnerRuleSummary {
	if projection.State == dataprotection.ProjectionStateEnrolling {
		return []models.ProtectionOwnerRuleSummary{}
	}
	seen := make(map[string]models.ProtectionOwnerRuleSummary, len(projection.Rules))
	for _, rule := range projection.Rules {
		action := strings.TrimSpace(rule.Action)
		effect := strings.TrimSpace(rule.EffectiveDecision(dataprotection.SubjectReference{}, now).Effect)
		if action != "" && effect != "" {
			key := action + "\x00" + effect
			seen[key] = models.ProtectionOwnerRuleSummary{Action: action, Effect: effect}
		}
	}
	rules := make([]models.ProtectionOwnerRuleSummary, 0, len(seen))
	for _, rule := range seen {
		rules = append(rules, rule)
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Action == rules[j].Action {
			return rules[i].Effect < rules[j].Effect
		}
		return rules[i].Action < rules[j].Action
	})
	return rules
}

func newDiscoveryExecution(enrollment models.ProtectionEnrollment, actorID int, triggerType string, now time.Time) commonexecution.TaskExecution {
	sourceTaskID := enrollment.ID
	return commonexecution.TaskExecution{
		TenantID: int(enrollment.TenantID), ExecutionID: uuid.NewString(),
		Module: commonexecution.ModuleSecurity, TaskType: commonexecution.TaskTypeSensitiveDataDiscovery,
		Source: commonexecution.ModuleSecurity, SourceTaskID: &sourceTaskID,
		Status: commonexecution.ExecutionStatusPending, Progress: 0,
		ExecutionBoundary: commonexecution.ExecutionBoundaryBounded,
		TriggerType:       triggerType, TriggeredBy: &actorID,
		ExecutionConfig: commonmodels.JSONMap{}, Metadata: commonmodels.JSONMap{},
		MaxAttempts: 3, CreatedAt: now, UpdatedAt: now,
	}
}

func revisionString(value int64) string { return fmt.Sprintf("%020d", value) }

func nextRevision(current string) (string, error) {
	value, err := strconv.ParseInt(current, 10, 64)
	if err != nil || value <= 0 {
		return "", errors.New("invalid protection projection revision")
	}
	return revisionString(value + 1), nil
}

func encodeProjectionCursor(sequence int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte("security.projection:" + strconv.FormatInt(sequence, 10)))
}

func decodeProjectionCursor(cursor string) (int64, error) {
	if cursor == "" {
		return 0, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || !strings.HasPrefix(string(payload), "security.projection:") {
		return 0, ErrProjectionCursorConflict
	}
	sequence, err := strconv.ParseInt(strings.TrimPrefix(string(payload), "security.projection:"), 10, 64)
	if err != nil || sequence <= 0 || encodeProjectionCursor(sequence) != cursor {
		return 0, ErrProjectionCursorConflict
	}
	return sequence, nil
}

func protectionValidityDeadline(mode string, deadline *time.Time, now time.Time, creating bool) (*time.Time, error) {
	if creating && mode == "" {
		mode = "permanent"
	}
	switch mode {
	case "permanent":
		if deadline != nil {
			return nil, commonapi.ErrBadRequest
		}
		return nil, nil
	case "until":
		if deadline == nil || !deadline.After(now) || deadline.Year() > 9999 {
			return nil, commonapi.ErrBadRequest
		}
		value := deadline.UTC().Truncate(time.Microsecond)
		if !value.After(now) {
			return nil, commonapi.ErrBadRequest
		}
		return &value, nil
	default:
		return nil, commonapi.ErrBadRequest
	}
}

func (s *EnrollmentService) UpdateValidity(ctx context.Context, tenantID, userID int64, id string, request models.UpdateProtectionValidityRequest) (*models.ProtectionEnrollmentResponse, error) {
	if tenantID <= 0 || userID <= 0 || uuid.Validate(id) != nil || request.Version <= 0 {
		return nil, commonapi.ErrBadRequest
	}
	now := s.now().UTC()
	deadline, err := protectionValidityDeadline(request.ValidityMode, request.ExpiresAt, now, false)
	if err != nil {
		return nil, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := lockProtectionEnrollment(tx, tenantID, id)
		if err != nil {
			return err
		}
		if row.Version != request.Version {
			return repository.ErrVersionConflict
		}
		if row.State == models.EnrollmentStateReleased || row.State == models.EnrollmentStateReleasing {
			return commonapi.ErrConflict
		}
		update := tx.Model(&row).Where("version = ?", request.Version).Updates(map[string]any{"protection_expires_at": deadline, "validity_updated_by": userID, "validity_updated_at": now, "updated_at": now, "version": gorm.Expr("version + 1")})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return repository.ErrVersionConflict
		}
		row.ProtectionExpiresAt = deadline
		// Recompile existing facts through the unique compiler; no discovery is started.
		if row.LatestSourceSnapshotHash != "" {
			return compileProtectionProjections(tx, row, row.LatestSourceSnapshotHash, now, allRequiredProtectionOwners())
		}
		// No source facts yet: retain the initial resource-level deny barrier.
		var records []models.ProtectionProjectionRecord
		if err := tx.Where("tenant_id = ? AND enrollment_id = ?", tenantID, id).Order("consumer_owner ASC").Find(&records).Error; err != nil {
			return err
		}
		if len(records) != len(allRequiredProtectionOwners()) {
			return commonapi.ErrConflict
		}
		for _, record := range records {
			var current dataprotection.Projection
			if err := json.Unmarshal([]byte(record.ProjectionPayload), &current); err != nil {
				return err
			}
			if err := current.Validate(time.Time{}); err != nil {
				return err
			}
			if err := publishProtectionProjection(tx, row, record.ConsumerOwner, current.SourceSnapshotHash, current.Rules, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, tenantID, id)
}

func lockProtectionEnrollment(tx *gorm.DB, tenantID int64, id string) (models.ProtectionEnrollment, error) {
	var row models.ProtectionEnrollment
	query := tx
	if tx.Dialector.Name() == "postgres" {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.Where("tenant_id = ? AND id = ?", tenantID, id).First(&row).Error; err != nil {
		return row, policyDBError(err)
	}
	return row, nil
}

func installEnrollmentOwnerProjection(tx *gorm.DB, enrollment models.ProtectionEnrollment, owner string, now time.Time) error {
	if err := dataprotection.VerifyTargetEngine(enrollment.Target(), enrollment.TargetEngineID, enrollment.TargetFullName); err != nil {
		return err
	}
	validFrom := now
	if enrollment.ProtectionExpiresAt != nil && !now.Before(*enrollment.ProtectionExpiresAt) {
		validFrom = enrollment.CreatedAt
	}
	projection := dataprotection.Projection{
		SchemaVersion: dataprotection.ProjectionSchemaV4,
		ProjectionID:  uuid.NewString(), Revision: revisionString(1),
		ConsumerOwner: owner, State: dataprotection.ProjectionStateEnrolling,
		Target: enrollment.Target(), TargetEngineID: enrollment.TargetEngineID, Rules: []dataprotection.Rule{},
		ValidFrom: validFrom, ExpiresAt: enrollment.ProtectionExpiresAt,
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
		ConsumerOwner: owner, Operation: dataprotection.ChangeOperationUpsert,
		ProjectionID: projection.ProjectionID, Revision: projection.Revision,
		TargetOwner: enrollment.TargetOwner, TargetType: enrollment.TargetType,
		TargetIdentity:    enrollment.TargetIdentity,
		ProjectionPayload: &payloadText, CreatedAt: now,
	}
	if err := tx.Create(&change).Error; err != nil {
		return err
	}
	record := models.ProtectionProjectionRecord{
		ID: projection.ProjectionID, TenantID: enrollment.TenantID, EnrollmentID: enrollment.ID,
		ConsumerOwner: owner, Revision: projection.Revision, State: projection.State,
		ProjectionPayload: payloadText, PublishedSequence: change.Sequence,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.Create(&record).Error; err != nil {
		return err
	}
	return nil
}
