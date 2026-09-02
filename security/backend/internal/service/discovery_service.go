package service

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	commonexecution "github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/security/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	managerPreviewAction     = "preview"
	managerProfileAction     = "profile"
	managerSearchIndexAction = "search_index"
	developQueryAction       = "query"
	serviceExecuteAction     = "service_execute"
	transferExportAction     = "export"
)

type SecurityFactsReader interface {
	GetDataItemSecurityFacts(context.Context, string) (*dataprotection.DataItemSecurityFacts, error)
	GetDataItemSecuritySample(context.Context, string) (*dataprotection.DataItemSecuritySample, error)
}

type TenantSecurityFactsReader func(uint) SecurityFactsReader

type DiscoveryService struct {
	db         *gorm.DB
	factsFor   TenantSecurityFactsReader
	executions *commonexecution.TaskExecutionRepository
	now        func() time.Time
}

func NewDiscoveryService(db *gorm.DB, factsFor TenantSecurityFactsReader) *DiscoveryService {
	return &DiscoveryService{db: db, factsFor: factsFor, executions: commonexecution.NewTaskExecutionRepository(db), now: time.Now}
}

func (s *DiscoveryService) ClaimNext(ctx context.Context, workerID string, now time.Time, leaseDuration time.Duration) (*commonexecution.TaskExecution, *commonexecution.Lease, error) {
	var item *commonexecution.TaskExecution
	var lease *commonexecution.Lease
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		item, lease, err = commonexecution.ClaimNext(ctx, tx, commonexecution.ClaimOptions{
			Module: commonexecution.ModuleSecurity, TaskType: commonexecution.TaskTypeSensitiveDataDiscovery,
			Source: commonexecution.ModuleSecurity, WorkerID: workerID, Now: now, LeaseDuration: leaseDuration,
		})
		return err
	})
	return item, lease, err
}

func (s *DiscoveryService) Renew(ctx context.Context, lease commonexecution.Lease, expiresAt time.Time) error {
	return commonexecution.RenewLease(ctx, s.db, lease, expiresAt)
}

func (s *DiscoveryService) AttemptIsTerminal(ctx context.Context, lease commonexecution.Lease) (bool, error) {
	return commonexecution.AttemptIsTerminal(ctx, s.db, lease)
}

// RecoverExpired returns abandoned discovery attempts to the bounded queue and
// fails them closed once their configured attempt limit is exhausted.
func (s *DiscoveryService) RecoverExpired(ctx context.Context, now time.Time, limit int) (int, error) {
	recovered := 0
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		items, err := commonexecution.FindExpiredForUpdate(ctx, tx, commonexecution.ExpiredOptions{
			Module: commonexecution.ModuleSecurity, TaskType: commonexecution.TaskTypeSensitiveDataDiscovery,
			Source: commonexecution.ModuleSecurity, Now: now, Limit: limit,
		})
		if err != nil {
			return err
		}
		for _, item := range items {
			lease, err := commonexecution.LeaseFromExecution(item)
			if err != nil {
				return err
			}
			if item.Attempt < item.MaxAttempts {
				if err := commonexecution.RetryExpired(ctx, tx, lease, now, "worker lease expired; retry pending"); err != nil {
					return err
				}
			} else {
				fields := map[string]interface{}{
					"progress":      100,
					"error_details": commonmodels.JSONMap{"error_code": "worker_lease_expired"},
				}
				if item.StartedAt != nil {
					fields["execution_time_ms"] = now.UTC().Sub(item.StartedAt.UTC()).Milliseconds()
				}
				if err := commonexecution.FailExpired(ctx, tx, lease, now, fields); err != nil {
					return err
				}
			}
			recovered++
		}
		return nil
	})
	return recovered, err
}

func (s *DiscoveryService) Execute(ctx context.Context, item *commonexecution.TaskExecution, lease commonexecution.Lease) error {
	started := s.now().UTC()
	if item == nil || item.SourceTaskID == nil || item.TenantID <= 0 || s.factsFor == nil {
		return s.completeFailure(ctx, lease, started, "invalid_discovery_execution")
	}
	enrollmentID := strings.TrimSpace(*item.SourceTaskID)
	var enrollment models.ProtectionEnrollment
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND id = ? AND state IN ?", item.TenantID, enrollmentID, []string{models.EnrollmentStateEnrolling, models.EnrollmentStateActive}).First(&enrollment).Error; err != nil {
		return s.completeFailure(ctx, lease, started, "enrollment_not_discoverable")
	}
	facts, err := s.factsFor(uint(item.TenantID)).GetDataItemSecurityFacts(ctx, enrollment.TargetIdentity)
	if err != nil {
		return s.completeFailure(ctx, lease, started, "owner_facts_unavailable")
	}
	if err := facts.Validate(); err != nil || facts.ItemFingerprint != enrollment.TargetIdentity {
		return s.completeFailure(ctx, lease, started, "owner_facts_invalid")
	}
	findings := []models.SensitiveFinding{}
	sourceSnapshotHash := facts.SourceSnapshotHash
	observedAt := facts.ObservedAt
	if len(facts.Fields) > 0 {
		findings, err = s.detectPhoneFindings(enrollment, *facts)
	} else {
		var sample *dataprotection.DataItemSecuritySample
		sample, err = s.factsFor(uint(item.TenantID)).GetDataItemSecuritySample(ctx, enrollment.TargetIdentity)
		if err == nil && sample != nil {
			err = sample.Validate()
		}
		if err == nil && (sample.ItemFingerprint != enrollment.TargetIdentity || sample.ItemType != facts.ItemType) {
			err = errors.New("DataItem security sample identity mismatch")
		}
		if err == nil {
			findings, err = s.detectDocumentPhoneFindings(enrollment, *sample)
			sourceSnapshotHash = sample.SourceSnapshotHash
			observedAt = sample.ObservedAt
		}
	}
	if err != nil {
		return s.completeFailure(ctx, lease, started, "detector_failed")
	}
	now := s.now().UTC()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for index := range findings {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&findings[index]).Error; err != nil {
				return err
			}
		}
		if err := compileProtectionProjections(tx, enrollment, sourceSnapshotHash, now, []string{"manager", "develop", "service", "transfer"}); err != nil {
			return err
		}
		if err := tx.Model(&models.ProtectionEnrollment{}).Where("tenant_id = ? AND id = ?", enrollment.TenantID, enrollment.ID).Updates(map[string]interface{}{
			"latest_source_snapshot_hash": sourceSnapshotHash,
			"last_discovered_at":          now,
			"version":                     gorm.Expr("version + 1"),
			"updated_at":                  now,
		}).Error; err != nil {
			return err
		}
		duration := now.Sub(started).Milliseconds()
		count := int64(len(findings))
		return commonexecution.CompleteWithLease(ctx, tx, lease, commonexecution.ExecutionStatusSuccess, now, map[string]interface{}{
			"progress": 100, "execution_time_ms": duration, "rows_affected": count,
			"metadata": commonmodels.JSONMap{"finding_count": len(findings), "source_snapshot_hash": sourceSnapshotHash, "observed_at": observedAt},
		})
	})
	if err != nil {
		return s.completeFailure(ctx, lease, started, "persistence_or_compile_failed")
	}
	return nil
}

func (s *DiscoveryService) detectDocumentPhoneFindings(enrollment models.ProtectionEnrollment, sample dataprotection.DataItemSecuritySample) ([]models.SensitiveFinding, error) {
	matchCount := countExactASCIIDigitRuns(sample.Text, 11)
	if matchCount == 0 {
		return nil, nil
	}
	var dataType models.SensitiveDataType
	if err := s.db.Where("tenant_id = ? AND code = ?", enrollment.TenantID, "phone").First(&dataType).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return []models.SensitiveFinding{{
		ID: uuid.NewString(), TenantID: enrollment.TenantID, EnrollmentID: enrollment.ID,
		ComponentKey: dataprotection.DocumentTextComponentKey, SensitiveDataTypeID: dataType.ID,
		DetectorCode: "phone_document_text", DetectorVersion: models.FindingDetectorPhoneDocumentV1,
		Confidence: 1, SourceSnapshotHash: sample.SourceSnapshotHash, ObservedAt: sample.ObservedAt,
		Component: dataprotection.DocumentTextComponent(),
		Evidence: commonmodels.JSONMap{
			"schema_version": models.FindingEvidenceSchemaV1, "matched_rule": "exact_ascii_digit_run",
			"component_key": dataprotection.DocumentTextComponentKey, "match_count": matchCount,
		},
		CreatedAt: s.now().UTC(),
	}}, nil
}

func countExactASCIIDigitRuns(text string, exact int) int {
	if exact <= 0 {
		return 0
	}
	runes := []rune(text)
	count := 0
	for index := 0; index < len(runes); {
		if runes[index] < '0' || runes[index] > '9' {
			index++
			continue
		}
		end := index
		for end < len(runes) && runes[end] >= '0' && runes[end] <= '9' {
			end++
		}
		if end-index == exact {
			count++
		}
		index = end
	}
	return count
}

func (s *DiscoveryService) completeFailure(ctx context.Context, lease commonexecution.Lease, started time.Time, code string) error {
	now := s.now().UTC()
	err := commonexecution.CompleteWithLease(ctx, s.db, lease, commonexecution.ExecutionStatusFailed, now, map[string]interface{}{
		"progress": 100, "execution_time_ms": now.Sub(started).Milliseconds(),
		"error_details": commonmodels.JSONMap{"error_code": code},
	})
	if err != nil {
		return err
	}
	return fmt.Errorf("security discovery failed: %s", code)
}

func (s *DiscoveryService) detectPhoneFindings(enrollment models.ProtectionEnrollment, facts dataprotection.DataItemSecurityFacts) ([]models.SensitiveFinding, error) {
	var dataType models.SensitiveDataType
	if err := s.db.Where("tenant_id = ? AND code = ?", enrollment.TenantID, "phone").First(&dataType).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	result := make([]models.SensitiveFinding, 0)
	for _, field := range facts.Fields {
		path := normalizedFieldPath(field)
		if field.Type != datatype.FieldTypeString || len(path) == 0 || !isPhoneFieldName(path[len(path)-1]) {
			continue
		}
		componentKey := strings.Join(path, ".")
		component, err := componentFromFields(facts.Fields, componentKey)
		if err != nil {
			return nil, err
		}
		fingerprint, err := dataprotection.ComponentSchemaFingerprint(facts.Fields, component)
		if err != nil {
			return nil, err
		}
		component.SchemaFingerprint = fingerprint
		result = append(result, models.SensitiveFinding{
			ID: uuid.NewString(), TenantID: enrollment.TenantID, EnrollmentID: enrollment.ID,
			ComponentKey: componentKey, SensitiveDataTypeID: dataType.ID,
			DetectorCode: "phone_metadata", DetectorVersion: models.FindingDetectorPhoneMetadataV1,
			Confidence: 1, SourceSnapshotHash: facts.SourceSnapshotHash, ObservedAt: facts.ObservedAt,
			Component: component,
			Evidence:  commonmodels.JSONMap{"schema_version": models.FindingEvidenceSchemaV1, "matched_rule": "terminal_field_name", "component_key": componentKey, "field_type": string(field.Type)},
			CreatedAt: s.now().UTC(),
		})
	}
	return result, nil
}

func componentFromFields(fields []datatype.FieldInfo, componentKey string) (dataprotection.Component, error) {
	parts := strings.Split(componentKey, ".")
	byPath := make(map[string]datatype.FieldInfo, len(fields))
	for _, field := range fields {
		path := normalizedFieldPath(field)
		byPath[strings.Join(path, ".")] = field
	}
	segments := make([]dataprotection.PathSegment, 0, len(parts))
	for index := range parts {
		path := strings.Join(parts[:index+1], ".")
		field, ok := byPath[path]
		if !ok {
			return dataprotection.Component{}, fmt.Errorf("component path %s is missing", path)
		}
		container := "scalar"
		if index < len(parts)-1 {
			switch field.Type {
			case datatype.FieldTypeJSON:
				container = "object"
			case datatype.FieldTypeArray:
				container = "array"
			default:
				return dataprotection.Component{}, fmt.Errorf("component parent %s is not a container", path)
			}
		}
		segments = append(segments, dataprotection.PathSegment{Name: parts[index], Container: container})
	}
	terminal := byPath[componentKey]
	return dataprotection.Component{Key: componentKey, Path: segments, ValueType: string(terminal.Type)}, nil
}

func normalizedFieldPath(field datatype.FieldInfo) []string {
	path := append([]string(nil), field.Path...)
	if len(path) == 0 && strings.TrimSpace(field.Name) != "" {
		path = strings.Split(strings.TrimSpace(field.Name), ".")
	}
	for index := range path {
		path[index] = strings.TrimSpace(path[index])
	}
	return path
}

func isPhoneFieldName(value string) bool {
	normalized := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(value)))
	switch normalized {
	case "phone", "mobile", "mobilephone", "phonenumber", "telephone", "手机号", "手机号码":
		return true
	default:
		return false
	}
}

func (s *DiscoveryService) ListFindings(ctx context.Context, tenantID int64, enrollmentID, sourceSnapshotHash string, page, pageSize int64) (*models.SensitiveFindingListResponse, error) {
	enrollmentID = strings.TrimSpace(enrollmentID)
	sourceSnapshotHash = strings.TrimSpace(sourceSnapshotHash)
	if tenantID <= 0 || page <= 0 || pageSize <= 0 || pageSize > 100 ||
		(enrollmentID != "" && uuid.Validate(enrollmentID) != nil) ||
		(sourceSnapshotHash != "" && !validSourceSnapshotHash(sourceSnapshotHash)) {
		return nil, commonapi.ErrBadRequest
	}
	query := s.db.WithContext(ctx).Model(&models.SensitiveFinding{}).Where("tenant_id = ?", tenantID)
	if enrollmentID != "" {
		query = query.Where("enrollment_id = ?", enrollmentID)
	}
	if sourceSnapshotHash != "" {
		query = query.Where("source_snapshot_hash = ?", sourceSnapshotHash)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	var rows []models.SensitiveFinding
	if err := query.Order("created_at DESC, id ASC").Offset(int((page - 1) * pageSize)).Limit(int(pageSize)).Find(&rows).Error; err != nil {
		return nil, err
	}
	data, err := s.attachFindingReviews(ctx, tenantID, rows)
	if err != nil {
		return nil, err
	}
	return &models.SensitiveFindingListResponse{Data: data, Total: total, Page: int(page), PageSize: int(pageSize), TotalPages: int((total + pageSize - 1) / pageSize)}, nil
}

func (s *DiscoveryService) GetFinding(ctx context.Context, tenantID int64, id string) (*models.SensitiveFindingResponse, error) {
	if tenantID <= 0 || uuid.Validate(id) != nil {
		return nil, commonapi.ErrBadRequest
	}
	var finding models.SensitiveFinding
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id).First(&finding).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, commonapi.ErrNotFound
		}
		return nil, err
	}
	data, err := s.attachFindingReviews(ctx, tenantID, []models.SensitiveFinding{finding})
	if err != nil {
		return nil, err
	}
	return &data[0], nil
}

func (s *DiscoveryService) attachFindingReviews(ctx context.Context, tenantID int64, findings []models.SensitiveFinding) ([]models.SensitiveFindingResponse, error) {
	responses := make([]models.SensitiveFindingResponse, 0, len(findings))
	if len(findings) == 0 {
		return responses, nil
	}
	ids := make([]string, 0, len(findings))
	for _, finding := range findings {
		ids = append(ids, finding.ID)
	}
	var reviews []models.SensitiveFindingReview
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND finding_id IN ?", tenantID, ids).Find(&reviews).Error; err != nil {
		return nil, err
	}
	reviewByFinding := make(map[string]models.SensitiveFindingReview, len(reviews))
	for _, review := range reviews {
		reviewByFinding[review.FindingID] = review
	}
	for _, finding := range findings {
		response := models.SensitiveFindingResponse{SensitiveFinding: finding}
		if review, ok := reviewByFinding[finding.ID]; ok {
			reviewCopy := review
			response.Review = &reviewCopy
		}
		responses = append(responses, response)
	}
	return responses, nil
}

func validSourceSnapshotHash(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil && len(decoded) == 32
}
