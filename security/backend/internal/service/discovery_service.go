package service

import (
	"context"
	"encoding/hex"
	"encoding/json"
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

type configuredDetector struct {
	Binding    models.Detector
	DataType   models.SensitiveDataType
	Capability models.DetectorCapability
}

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
	if item == nil || item.SourceTaskID == nil || item.TenantID <= 0 || strings.TrimSpace(item.ExecutionID) == "" || s.factsFor == nil {
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
	detectors, err := s.enabledDetectors(ctx, int64(item.TenantID), facts.ItemType)
	if err != nil {
		return s.completeFailure(ctx, lease, started, "detector_configuration_invalid")
	}
	findings := []models.SensitiveFinding{}
	sourceSnapshotHash := facts.SourceSnapshotHash
	observedAt := facts.ObservedAt
	if len(facts.Fields) > 0 {
		for _, detector := range detectors {
			if detector.Capability.TargetKind != detectorTargetFieldMetadata {
				continue
			}
			var detected []models.SensitiveFinding
			detected, err = s.detectFieldFindings(enrollment, item.ExecutionID, detector, *facts)
			if err != nil {
				break
			}
			findings = append(findings, detected...)
		}
	} else {
		documentDetectors := make([]configuredDetector, 0, len(detectors))
		for _, detector := range detectors {
			if detector.Capability.TargetKind == detectorTargetDocumentText {
				documentDetectors = append(documentDetectors, detector)
			}
		}
		if len(documentDetectors) > 0 {
			var sample *dataprotection.DataItemSecuritySample
			sample, err = s.factsFor(uint(item.TenantID)).GetDataItemSecuritySample(ctx, enrollment.TargetIdentity)
			if err == nil && sample != nil {
				err = sample.Validate()
			}
			if err == nil && (sample.ItemFingerprint != enrollment.TargetIdentity || sample.ItemType != facts.ItemType) {
				err = errors.New("DataItem security sample identity mismatch")
			}
			if err == nil {
				for _, detector := range documentDetectors {
					var detected []models.SensitiveFinding
					detected, err = s.detectDocumentFindings(enrollment, item.ExecutionID, detector, *sample)
					if err != nil {
						break
					}
					findings = append(findings, detected...)
				}
				sourceSnapshotHash = sample.SourceSnapshotHash
				observedAt = sample.ObservedAt
			}
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
		enrollment.LatestSourceSnapshotHash = sourceSnapshotHash
		enrollment.LatestDiscoveryExecutionID = item.ExecutionID
		if err := compileProtectionProjections(tx, enrollment, sourceSnapshotHash, now, []string{"manager", "develop", "service", "transfer"}); err != nil {
			return err
		}
		if err := tx.Model(&models.ProtectionEnrollment{}).Where("tenant_id = ? AND id = ?", enrollment.TenantID, enrollment.ID).Updates(map[string]interface{}{
			"latest_source_snapshot_hash":   sourceSnapshotHash,
			"latest_discovery_execution_id": item.ExecutionID,
			"last_discovered_at":            now,
			"version":                       gorm.Expr("version + 1"),
			"updated_at":                    now,
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

func (s *DiscoveryService) enabledDetectors(ctx context.Context, tenantID int64, itemType string) ([]configuredDetector, error) {
	var bindings []models.Detector
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND enabled = ?", tenantID, true).Order("id ASC").Find(&bindings).Error; err != nil {
		return nil, err
	}
	if len(bindings) == 0 {
		return []configuredDetector{}, nil
	}
	typeIDs := make([]int64, 0, len(bindings))
	for _, binding := range bindings {
		typeIDs = append(typeIDs, binding.SensitiveDataTypeID)
	}
	var dataTypes []models.SensitiveDataType
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND id IN ?", tenantID, typeIDs).Find(&dataTypes).Error; err != nil {
		return nil, err
	}
	typesByID := make(map[int64]models.SensitiveDataType, len(dataTypes))
	for _, dataType := range dataTypes {
		typesByID[dataType.ID] = dataType
	}
	result := make([]configuredDetector, 0, len(bindings))
	for _, binding := range bindings {
		capability, ok := detectorCapability(binding.CapabilityKey)
		dataType, typeExists := typesByID[binding.SensitiveDataTypeID]
		if !ok || !typeExists {
			return nil, errors.New("enabled detector binding is invalid")
		}
		if capabilitySupportsItemType(capability, itemType) {
			result = append(result, configuredDetector{Binding: binding, DataType: dataType, Capability: capability})
		}
	}
	return result, nil
}

func (s *DiscoveryService) detectDocumentFindings(enrollment models.ProtectionEnrollment, executionID string, detector configuredDetector, sample dataprotection.DataItemSecuritySample) ([]models.SensitiveFinding, error) {
	if detector.Capability.Key != models.FindingDetectorPhoneDocumentV1 {
		return nil, errors.New("document detector capability is not implemented")
	}
	matchCount := countExactASCIIDigitRuns(sample.Text, 11)
	if matchCount == 0 {
		return nil, nil
	}
	return []models.SensitiveFinding{{
		ID: uuid.NewString(), TenantID: enrollment.TenantID, EnrollmentID: enrollment.ID,
		DiscoveryExecutionID: executionID,
		ComponentKey:         dataprotection.DocumentTextComponentKey, SensitiveDataTypeID: detector.DataType.ID,
		DetectorCode: detector.Capability.Code, DetectorVersion: detector.Capability.Key,
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

func (s *DiscoveryService) detectFieldFindings(enrollment models.ProtectionEnrollment, executionID string, detector configuredDetector, facts dataprotection.DataItemSecurityFacts) ([]models.SensitiveFinding, error) {
	aliases, implemented := fieldMetadataAliases(detector.Capability.Key)
	if !implemented {
		return nil, errors.New("field detector capability is not implemented")
	}
	result := make([]models.SensitiveFinding, 0)
	for _, field := range facts.Fields {
		path := normalizedFieldPath(field)
		semanticTerminal := fieldSemanticTerminal(field)
		normalizedTerminal := normalizeFieldSemanticName(semanticTerminal)
		if field.Type != datatype.FieldTypeString || len(path) == 0 {
			continue
		}
		if _, matches := aliases[normalizedTerminal]; !matches {
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
			DiscoveryExecutionID: executionID,
			ComponentKey:         componentKey, SensitiveDataTypeID: detector.DataType.ID,
			DetectorCode: detector.Capability.Code, DetectorVersion: detector.Capability.Key,
			Confidence: 1, SourceSnapshotHash: facts.SourceSnapshotHash, ObservedAt: facts.ObservedAt,
			Component: component,
			Evidence: commonmodels.JSONMap{
				"schema_version": models.FindingEvidenceSchemaV1, "matched_rule": "terminal_field_name",
				"component_key": componentKey, "field_type": string(field.Type),
				"semantic_terminal":   semanticTerminal,
				"normalized_terminal": normalizedTerminal,
				"matched_alias":       normalizedTerminal,
			},
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

func fieldSemanticTerminal(field datatype.FieldInfo) string {
	path := normalizedFieldPath(field)
	if len(path) == 0 {
		return ""
	}
	terminal := path[len(path)-1]
	if len(field.Path) > 0 {
		return terminal
	}
	segments := strings.Split(terminal, "__")
	if len(segments) == 1 {
		return terminal
	}
	for _, segment := range segments {
		if strings.TrimSpace(segment) == "" {
			return terminal
		}
	}
	return segments[len(segments)-1]
}

func normalizeFieldSemanticName(value string) string {
	return strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(value)))
}

type FindingListFilter struct {
	EnrollmentID         string
	SourceSnapshotHash   string
	DiscoveryExecutionID string
	SnapshotScope        string
	ReviewState          string
	SensitiveDataTypeID  *int64
	DetectorVersion      string
}

func (s *DiscoveryService) ListFindings(ctx context.Context, tenantID int64, filter FindingListFilter, page, pageSize int64) (*models.SensitiveFindingListResponse, error) {
	filter.EnrollmentID = strings.TrimSpace(filter.EnrollmentID)
	filter.SourceSnapshotHash = strings.TrimSpace(filter.SourceSnapshotHash)
	filter.DiscoveryExecutionID = strings.TrimSpace(filter.DiscoveryExecutionID)
	filter.SnapshotScope = strings.TrimSpace(filter.SnapshotScope)
	filter.ReviewState = strings.TrimSpace(filter.ReviewState)
	filter.DetectorVersion = strings.TrimSpace(filter.DetectorVersion)
	if filter.SnapshotScope == "" {
		filter.SnapshotScope = models.FindingSnapshotScopeAll
	}
	if filter.ReviewState == "" {
		filter.ReviewState = models.FindingReviewStateAll
	}
	if tenantID <= 0 || page <= 0 || pageSize <= 0 || pageSize > 100 ||
		(filter.EnrollmentID != "" && uuid.Validate(filter.EnrollmentID) != nil) ||
		(filter.SourceSnapshotHash != "" && !validSourceSnapshotHash(filter.SourceSnapshotHash)) ||
		(filter.DiscoveryExecutionID != "" && uuid.Validate(filter.DiscoveryExecutionID) != nil) ||
		(filter.SnapshotScope != models.FindingSnapshotScopeAll && filter.SnapshotScope != models.FindingSnapshotScopeCurrent) ||
		(filter.ReviewState != models.FindingReviewStateAll && filter.ReviewState != models.FindingReviewStatePending && filter.ReviewState != models.FindingReviewStateReviewed) ||
		(filter.SensitiveDataTypeID != nil && *filter.SensitiveDataTypeID <= 0) || len(filter.DetectorVersion) > 100 {
		return nil, commonapi.ErrBadRequest
	}
	query := s.db.WithContext(ctx).Table(models.SensitiveFinding{}.TableName()+" AS finding").Where("finding.tenant_id = ?", tenantID)
	if filter.SnapshotScope == models.FindingSnapshotScopeCurrent {
		query = query.Joins("JOIN "+models.ProtectionEnrollment{}.TableName()+" AS enrollment ON enrollment.tenant_id = finding.tenant_id AND enrollment.id = finding.enrollment_id AND enrollment.state <> ? AND enrollment.latest_source_snapshot_hash = finding.source_snapshot_hash AND enrollment.latest_discovery_execution_id = finding.discovery_execution_id", models.EnrollmentStateReleased)
	}
	if filter.ReviewState != models.FindingReviewStateAll {
		query = query.Joins("LEFT JOIN " + models.SensitiveFindingReview{}.TableName() + " AS finding_review ON finding_review.tenant_id = finding.tenant_id AND finding_review.finding_id = finding.id")
		if filter.ReviewState == models.FindingReviewStatePending {
			query = query.Where("finding_review.id IS NULL")
		} else {
			query = query.Where("finding_review.id IS NOT NULL")
		}
	}
	if filter.EnrollmentID != "" {
		query = query.Where("finding.enrollment_id = ?", filter.EnrollmentID)
	}
	if filter.SourceSnapshotHash != "" {
		query = query.Where("finding.source_snapshot_hash = ?", filter.SourceSnapshotHash)
	}
	if filter.DiscoveryExecutionID != "" {
		query = query.Where("finding.discovery_execution_id = ?", filter.DiscoveryExecutionID)
	}
	if filter.SensitiveDataTypeID != nil {
		query = query.Where("finding.sensitive_data_type_id = ?", *filter.SensitiveDataTypeID)
	}
	if filter.DetectorVersion != "" {
		query = query.Where("finding.detector_version = ?", filter.DetectorVersion)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	var rows []models.SensitiveFinding
	if err := query.Select("finding.*").Order("finding.created_at DESC, finding.id ASC").Offset(int((page - 1) * pageSize)).Limit(int(pageSize)).Find(&rows).Error; err != nil {
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
	enrollmentIDs := make([]string, 0, len(findings))
	seenEnrollments := make(map[string]struct{}, len(findings))
	for _, finding := range findings {
		ids = append(ids, finding.ID)
		if _, exists := seenEnrollments[finding.EnrollmentID]; !exists {
			seenEnrollments[finding.EnrollmentID] = struct{}{}
			enrollmentIDs = append(enrollmentIDs, finding.EnrollmentID)
		}
	}
	var reviews []models.SensitiveFindingReview
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND finding_id IN ?", tenantID, ids).Find(&reviews).Error; err != nil {
		return nil, err
	}
	reviewByFinding := make(map[string]models.SensitiveFindingReview, len(reviews))
	for _, review := range reviews {
		reviewByFinding[review.FindingID] = review
	}
	var enrollments []models.ProtectionEnrollment
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND id IN ?", tenantID, enrollmentIDs).Find(&enrollments).Error; err != nil {
		return nil, err
	}
	targetByEnrollment := make(map[string]models.ProtectionTargetSnapshot, len(enrollments))
	for _, enrollment := range enrollments {
		targetByEnrollment[enrollment.ID] = enrollment.TargetSnapshot()
	}
	explanations, err := s.buildFindingExplanations(ctx, tenantID, findings, reviewByFinding)
	if err != nil {
		return nil, err
	}
	for _, finding := range findings {
		response := models.SensitiveFindingResponse{SensitiveFinding: finding, TargetSnapshot: targetByEnrollment[finding.EnrollmentID], Explanation: explanations[finding.ID]}
		if review, ok := reviewByFinding[finding.ID]; ok {
			reviewCopy := review
			response.Review = &reviewCopy
		}
		responses = append(responses, response)
	}
	return responses, nil
}

type findingBaselineKey struct {
	typeID  int64
	gradeID int64
}

func (s *DiscoveryService) buildFindingExplanations(ctx context.Context, tenantID int64, findings []models.SensitiveFinding, reviews map[string]models.SensitiveFindingReview) (map[string]models.SensitiveFindingExplanation, error) {
	result := make(map[string]models.SensitiveFindingExplanation, len(findings))
	if len(findings) == 0 {
		return result, nil
	}

	enrollmentIDs := make([]string, 0, len(findings))
	capabilityKeys := make([]string, 0, len(findings))
	seenEnrollments := make(map[string]struct{}, len(findings))
	seenCapabilities := make(map[string]struct{}, len(findings))
	for _, finding := range findings {
		if _, exists := seenEnrollments[finding.EnrollmentID]; !exists {
			seenEnrollments[finding.EnrollmentID] = struct{}{}
			enrollmentIDs = append(enrollmentIDs, finding.EnrollmentID)
		}
		if _, exists := seenCapabilities[finding.DetectorVersion]; !exists {
			seenCapabilities[finding.DetectorVersion] = struct{}{}
			capabilityKeys = append(capabilityKeys, finding.DetectorVersion)
		}
	}

	db := s.db.WithContext(ctx)
	var bindings []models.Detector
	if err := db.Where("tenant_id = ? AND capability_key IN ?", tenantID, capabilityKeys).Find(&bindings).Error; err != nil {
		return nil, err
	}
	bindingByCapability := make(map[string]models.Detector, len(bindings))
	for _, binding := range bindings {
		bindingByCapability[binding.CapabilityKey] = binding
	}

	var dataTypes []models.SensitiveDataType
	if err := db.Where("tenant_id = ?", tenantID).Find(&dataTypes).Error; err != nil {
		return nil, err
	}
	typeByID := make(map[int64]models.SensitiveDataType, len(dataTypes))
	for _, dataType := range dataTypes {
		typeByID[dataType.ID] = dataType
	}

	var assessments []models.ResourceSecurityAssessment
	if err := db.Where("tenant_id = ? AND enrollment_id IN ?", tenantID, enrollmentIDs).Find(&assessments).Error; err != nil {
		return nil, err
	}
	assessmentByComponent := make(map[string]models.ResourceSecurityAssessment, len(assessments))
	assessmentIDs := make([]string, 0, len(assessments))
	for _, assessment := range assessments {
		assessmentByComponent[findingComponentMapKey(assessment.EnrollmentID, assessment.ComponentKey)] = assessment
		assessmentIDs = append(assessmentIDs, assessment.ID)
	}
	currentRevisionByAssessment := make(map[string]models.ResourceSecurityAssessmentRevision, len(assessments))
	if len(assessmentIDs) > 0 {
		var revisions []models.ResourceSecurityAssessmentRevision
		if err := db.Where("tenant_id = ? AND assessment_id IN ?", tenantID, assessmentIDs).Find(&revisions).Error; err != nil {
			return nil, err
		}
		assessmentByID := make(map[string]models.ResourceSecurityAssessment, len(assessments))
		for _, assessment := range assessments {
			assessmentByID[assessment.ID] = assessment
		}
		for _, revision := range revisions {
			if assessment, exists := assessmentByID[revision.AssessmentID]; exists && revision.Revision == assessment.CurrentRevision {
				currentRevisionByAssessment[revision.AssessmentID] = revision
			}
		}
	}

	var baselines []models.ProtectionBaseline
	if err := db.Where("tenant_id = ? AND enabled = ?", tenantID, true).Find(&baselines).Error; err != nil {
		return nil, err
	}
	baselineByTarget := make(map[findingBaselineKey]models.ProtectionBaseline, len(baselines))
	for _, baseline := range baselines {
		baselineByTarget[findingBaselineKey{typeID: baseline.SensitiveDataTypeID, gradeID: baseline.SecurityGradeID}] = baseline
	}

	var enrollments []models.ProtectionEnrollment
	if err := db.Where("tenant_id = ? AND id IN ?", tenantID, enrollmentIDs).Find(&enrollments).Error; err != nil {
		return nil, err
	}
	enrollmentByID := make(map[string]models.ProtectionEnrollment, len(enrollments))
	for _, enrollment := range enrollments {
		enrollmentByID[enrollment.ID] = enrollment
	}

	var projections []models.ProtectionProjectionRecord
	if err := db.Where("tenant_id = ? AND enrollment_id IN ?", tenantID, enrollmentIDs).Find(&projections).Error; err != nil {
		return nil, err
	}
	projectionByEnrollmentOwner := make(map[string]models.ProtectionProjectionRecord, len(projections))
	projectionRulesByID := make(map[string][]dataprotection.Rule, len(projections))
	for _, record := range projections {
		projectionByEnrollmentOwner[findingComponentMapKey(record.EnrollmentID, record.ConsumerOwner)] = record
		var projection dataprotection.Projection
		if err := json.Unmarshal([]byte(record.ProjectionPayload), &projection); err != nil {
			return nil, fmt.Errorf("decode protection projection %s: %w", record.ID, err)
		}
		projectionRulesByID[record.ID] = projection.Rules
	}

	var acknowledgements []models.ProtectionProjectionAcknowledgement
	if err := db.Where("tenant_id = ?", tenantID).Find(&acknowledgements).Error; err != nil {
		return nil, err
	}
	ackByOwner := make(map[string]models.ProtectionProjectionAcknowledgement, len(acknowledgements))
	for _, acknowledgement := range acknowledgements {
		ackByOwner[acknowledgement.ConsumerOwner] = acknowledgement
	}

	for _, finding := range findings {
		explanation := models.SensitiveFindingExplanation{Outlets: []models.FindingOutletProtection{}}
		if capability, exists := detectorCapability(finding.DetectorVersion); exists {
			capabilityCopy := capability
			explanation.Capability = &capabilityCopy
		}
		binding, bindingExists := bindingByCapability[finding.DetectorVersion]
		if bindingExists && binding.SensitiveDataTypeID == finding.SensitiveDataTypeID {
			threshold := binding.ConfidenceThreshold
			explanation.AutomaticAdoptionThreshold = &threshold
			explanation.MeetsAutomaticThreshold = finding.Confidence >= threshold
		} else {
			bindingExists = false
		}

		var assessmentPointer *models.ResourceSecurityAssessment
		var revisionPointer *models.ResourceSecurityAssessmentRevision
		if assessment, exists := assessmentByComponent[findingComponentMapKey(finding.EnrollmentID, finding.ComponentKey)]; exists {
			assessmentCopy := assessment
			assessmentPointer = &assessmentCopy
			if revision, revisionExists := currentRevisionByAssessment[assessment.ID]; revisionExists {
				revisionCopy := revision
				revisionPointer = &revisionCopy
			}
		}
		var reviewPointer *models.SensitiveFindingReview
		if review, exists := reviews[finding.ID]; exists {
			reviewCopy := review
			reviewPointer = &reviewCopy
		}
		var dataTypePointer *models.SensitiveDataType
		if dataType, exists := typeByID[finding.SensitiveDataTypeID]; exists {
			dataTypeCopy := dataType
			dataTypePointer = &dataTypeCopy
		}
		var detectorPointer *models.Detector
		if bindingExists {
			bindingCopy := binding
			detectorPointer = &bindingCopy
		}
		candidate, included, decisionState, governanceSource := resolveProtectionCandidateFromFacts(
			finding, assessmentPointer, revisionPointer, reviewPointer, dataTypePointer, detectorPointer,
		)
		explanation.DecisionState = decisionState
		explanation.GovernanceSource = governanceSource
		if included && candidate.SensitiveDataTypeID > 0 {
			typeID := candidate.SensitiveDataTypeID
			explanation.EffectiveSensitiveDataTypeID = &typeID
		}
		if included && candidate.SecurityClassificationID > 0 {
			classificationID := candidate.SecurityClassificationID
			explanation.EffectiveSecurityClassificationID = &classificationID
		}
		if included && candidate.SecurityGradeID > 0 {
			gradeID := candidate.SecurityGradeID
			explanation.EffectiveSecurityGradeID = &gradeID
		}
		explanation.AssessmentID = candidate.AssessmentID

		if included && candidate.SensitiveDataTypeID > 0 && candidate.SecurityGradeID > 0 {
			if baseline, exists := baselineByTarget[findingBaselineKey{typeID: candidate.SensitiveDataTypeID, gradeID: candidate.SecurityGradeID}]; exists {
				explanation.Baseline = &models.FindingProtectionBaseline{
					ID: baseline.ID, Version: baseline.Version, Effect: baseline.Effect, Algorithm: baseline.Algorithm,
					KeepPrefix: baseline.KeepPrefix, KeepSuffix: baseline.KeepSuffix, InvalidValueEffect: baseline.InvalidValueEffect,
				}
			} else if decisionState == models.FindingDecisionAutomatic || decisionState == models.FindingDecisionFormal {
				explanation.DecisionState = models.FindingDecisionBaselineMissing
			}
		}

		enrollment, enrollmentExists := enrollmentByID[finding.EnrollmentID]
		if !enrollmentExists {
			return nil, fmt.Errorf("finding enrollment %s is missing", finding.EnrollmentID)
		}
		for _, owner := range requiredProtectionOwners {
			record, exists := projectionByEnrollmentOwner[findingComponentMapKey(finding.EnrollmentID, owner)]
			if !exists {
				return nil, fmt.Errorf("finding projection for %s/%s is missing", finding.EnrollmentID, owner)
			}
			requiredSequence := record.PublishedSequence
			if (enrollment.State == models.EnrollmentStateReleasing || enrollment.State == models.EnrollmentStateReleased) && record.ReleaseSequence != nil {
				requiredSequence = *record.ReleaseSequence
			}
			acknowledgement, acknowledged := ackByOwner[owner]
			outlet := models.FindingOutletProtection{
				ConsumerOwner: owner, ProjectionState: record.State,
				Acknowledged: acknowledged && acknowledgement.Sequence >= requiredSequence,
				Rules:        []models.FindingOutletProtectionRule{},
			}
			for _, rule := range projectionRulesByID[record.ID] {
				if rule.Component.Key != finding.ComponentKey {
					continue
				}
				decision := rule.Decision.Effective(time.Now().UTC())
				outlet.Rules = append(outlet.Rules, models.FindingOutletProtectionRule{
					Action: rule.Action, Effect: decision.Effect, Algorithm: decision.Algorithm,
				})
			}
			explanation.Outlets = append(explanation.Outlets, outlet)
		}
		result[finding.ID] = explanation
	}
	return result, nil
}

func findingComponentMapKey(first, second string) string {
	return first + "\x00" + second
}

func validSourceSnapshotHash(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil && len(decoded) == 32
}
