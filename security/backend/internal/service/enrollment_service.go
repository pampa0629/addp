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
	requiredProtectionOwners                 = []string{"develop", "manager", "service", "transfer"}
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
	enrollment := models.ProtectionEnrollment{
		ID: uuid.NewString(), TenantID: tenantID,
		TargetOwner: target.OwnerModule, TargetType: target.ResourceType,
		TargetIdentity: target.ResourceIdentity, TargetEngineID: locator.EngineID,
		TargetItemType: itemType, TargetFullName: fullName,
		State: models.EnrollmentStateActivating, Version: 1, CreatedBy: userID,
		CreatedAt: now, UpdatedAt: now,
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&enrollment).Error; err != nil {
			if repository.IsConflict(err) {
				return commonapi.ErrConflict
			}
			return err
		}
		for _, owner := range requiredProtectionOwners {
			projection := dataprotection.Projection{
				SchemaVersion: dataprotection.ProjectionSchemaV1,
				ProjectionID:  uuid.NewString(), Revision: revisionString(1),
				ConsumerOwner: owner, State: dataprotection.ProjectionStateEnrolling,
				Target: target, Rules: []dataprotection.Rule{},
				ValidFrom: now, ExpiresAt: now.Add(24 * time.Hour),
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
				ChangeID: uuid.NewString(), TenantID: tenantID, EnrollmentID: enrollment.ID,
				ConsumerOwner: owner, Operation: dataprotection.ChangeOperationUpsert,
				ProjectionID: projection.ProjectionID, Revision: projection.Revision,
				TargetOwner: target.OwnerModule, TargetType: target.ResourceType,
				TargetIdentity:    target.ResourceIdentity,
				ProjectionPayload: &payloadText, CreatedAt: now,
			}
			if err := tx.Create(&change).Error; err != nil {
				return err
			}
			record := models.ProtectionProjectionRecord{
				ID: projection.ProjectionID, TenantID: tenantID, EnrollmentID: enrollment.ID,
				ConsumerOwner: owner, Revision: projection.Revision, State: projection.State,
				ProjectionPayload: payloadText, PublishedSequence: change.Sequence,
				CreatedAt: now, UpdatedAt: now,
			}
			if err := tx.Create(&record).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, tenantID, enrollment.ID)
}

func (s *EnrollmentService) List(ctx context.Context, tenantID int64, scope string, page, pageSize int64) (*models.ProtectionEnrollmentListResponse, error) {
	if tenantID <= 0 || page <= 0 || pageSize <= 0 || pageSize > 100 {
		return nil, commonapi.ErrBadRequest
	}
	if scope != models.EnrollmentListScopeCurrent && scope != models.EnrollmentListScopeReleased && scope != models.EnrollmentListScopeAll {
		return nil, commonapi.ErrBadRequest
	}
	var total int64
	if err := applyEnrollmentListScope(s.db.WithContext(ctx).Model(&models.ProtectionEnrollment{}).Where("tenant_id = ?", tenantID), scope).Count(&total).Error; err != nil {
		return nil, err
	}
	var enrollments []models.ProtectionEnrollment
	if err := applyEnrollmentListScope(s.db.WithContext(ctx).Where("tenant_id = ?", tenantID), scope).Order("created_at DESC, id ASC").Offset(int((page - 1) * pageSize)).Limit(int(pageSize)).Find(&enrollments).Error; err != nil {
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
	if enrollment.LastDiscoveredAt == nil || strings.TrimSpace(enrollment.LatestSourceSnapshotHash) == "" {
		return ErrNoSupportedFindingsReleaseUnavailable
	}
	var findingCount int64
	if err := tx.Model(&models.SensitiveFinding{}).
		Where("tenant_id = ? AND enrollment_id = ? AND source_snapshot_hash = ?", enrollment.TenantID, enrollment.ID, enrollment.LatestSourceSnapshotHash).
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
			return commonapi.ErrConflict
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
		response = &models.ProtectionDiscoveryExecutionResponse{ExecutionID: execution.ExecutionID, EnrollmentID: enrollment.ID, Status: execution.Status, CreatedAt: execution.CreatedAt}
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
	if err := tx.Where("tenant_id = ? AND state IN ?", tenantID, []string{models.EnrollmentStateActivating, models.EnrollmentStateReleasing}).Find(&enrollments).Error; err != nil {
		return err
	}
	for _, enrollment := range enrollments {
		var projections []models.ProtectionProjectionRecord
		if err := tx.Where("tenant_id = ? AND enrollment_id = ?", tenantID, enrollment.ID).Find(&projections).Error; err != nil {
			return err
		}
		covered := len(projections) == len(requiredProtectionOwners)
		for _, projection := range projections {
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
		} else {
			values["state"] = models.EnrollmentStateReleased
			values["released_at"] = now
		}
		if err := tx.Model(&enrollment).Updates(values).Error; err != nil {
			return err
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
		Joins("JOIN security.protection_enrollments ON security.protection_enrollments.tenant_id = security.sensitive_findings.tenant_id AND security.protection_enrollments.id = security.sensitive_findings.enrollment_id AND security.protection_enrollments.latest_source_snapshot_hash = security.sensitive_findings.source_snapshot_hash").
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
	for _, enrollment := range enrollments {
		progress, err := buildOwnerProgress(enrollment, projectionsByEnrollment[enrollment.ID], ackByOwner)
		if err != nil {
			return nil, err
		}
		discoverySummary := models.ProtectionDiscoverySummary{Status: models.DiscoverySummaryStatusNotCompleted}
		if enrollment.LastDiscoveredAt != nil && strings.TrimSpace(enrollment.LatestSourceSnapshotHash) != "" {
			discoverySummary.Status = models.DiscoverySummaryStatusCompleted
			counts := findingCountByEnrollment[enrollment.ID]
			discoverySummary.FindingCount = counts.FindingCount
			discoverySummary.ReviewedCount = counts.ReviewedCount
			discoverySummary.PendingReviewCount = counts.FindingCount - counts.ReviewedCount
		}
		responses = append(responses, models.ProtectionEnrollmentResponse{
			ID: enrollment.ID, Target: enrollment.Target(), TargetSnapshot: enrollment.TargetSnapshot(), State: enrollment.State,
			Version: enrollment.Version, ReleaseReason: enrollment.ReleaseReason, ReleaseBasis: enrollment.ReleaseBasis,
			ReleaseRequestedBy: enrollment.ReleaseRequestedBy, ReleaseRequestedAt: enrollment.ReleaseRequestedAt,
			ReleaseSourceSnapshotHash: enrollment.ReleaseSourceSnapshotHash,
			LatestSourceSnapshotHash:  enrollment.LatestSourceSnapshotHash, LastDiscoveredAt: enrollment.LastDiscoveredAt,
			DiscoverySummary: discoverySummary, OwnerProgress: progress, CreatedBy: enrollment.CreatedBy,
			ReleasedAt: enrollment.ReleasedAt, CreatedAt: enrollment.CreatedAt, UpdatedAt: enrollment.UpdatedAt,
		})
	}
	return responses, nil
}

func buildOwnerProgress(enrollment models.ProtectionEnrollment, projections []models.ProtectionProjectionRecord, ackByOwner map[string]models.ProtectionProjectionAcknowledgement) ([]models.ProtectionOwnerProgress, error) {
	progress := make([]models.ProtectionOwnerProgress, 0, len(projections))
	for _, projection := range projections {
		requiredSequence := projection.PublishedSequence
		if enrollment.State == models.EnrollmentStateReleasing || enrollment.State == models.EnrollmentStateReleased {
			if projection.ReleaseSequence != nil {
				requiredSequence = *projection.ReleaseSequence
			}
		}
		acknowledgement, exists := ackByOwner[projection.ConsumerOwner]
		effects, err := projectionEffects(projection)
		if err != nil {
			return nil, err
		}
		item := models.ProtectionOwnerProgress{
			ConsumerOwner: projection.ConsumerOwner, ProjectionID: projection.ID,
			Revision: projection.Revision, ProjectionState: projection.State, Effects: effects,
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

func projectionEffects(record models.ProtectionProjectionRecord) ([]string, error) {
	if record.State == dataprotection.ProjectionStateEnrolling {
		return []string{dataprotection.EffectDeny}, nil
	}
	var projection dataprotection.Projection
	if err := json.Unmarshal([]byte(record.ProjectionPayload), &projection); err != nil {
		return nil, fmt.Errorf("decode protection projection %s: %w", record.ID, err)
	}
	seen := make(map[string]struct{}, len(projection.Rules))
	for _, rule := range projection.Rules {
		if effect := strings.TrimSpace(rule.Decision.Effect); effect != "" {
			seen[effect] = struct{}{}
		}
	}
	effects := make([]string, 0, len(seen))
	for effect := range seen {
		effects = append(effects, effect)
	}
	sort.Strings(effects)
	return effects, nil
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

func isRequiredOwner(owner string) bool {
	for _, candidate := range requiredProtectionOwners {
		if owner == candidate {
			return true
		}
	}
	return false
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
