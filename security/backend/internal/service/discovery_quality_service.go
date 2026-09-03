package service

import (
	"context"
	"sort"
	"strings"

	commonapi "github.com/addp/common/api"
	"github.com/addp/security/internal/models"
)

type currentDiscoveryQualityObservation struct {
	FindingID           string
	DetectorCode        string
	DetectorVersion     string
	SensitiveDataTypeID int64
	ReviewDecision      string
}

type reviewedDiscoveryQualityObservation struct {
	EnrollmentID        string
	ComponentKey        string
	DetectorCode        string
	DetectorVersion     string
	SensitiveDataTypeID int64
	Decision            string
}

type discoveryQualityKey struct {
	sensitiveDataTypeID int64
	detectorVersion     string
}

type reviewedComponentKey struct {
	enrollmentID    string
	componentKey    string
	detectorVersion string
}

func (s *DiscoveryService) GetQualitySummary(ctx context.Context, tenantID int64, sensitiveDataTypeID *int64) (*models.SensitiveDiscoveryQualitySummary, error) {
	if tenantID <= 0 || (sensitiveDataTypeID != nil && *sensitiveDataTypeID <= 0) {
		return nil, commonapi.ErrBadRequest
	}

	qualityByCapability := map[discoveryQualityKey]*models.SensitiveDiscoveryCapabilityQuality{}
	currentQuery := s.db.WithContext(ctx).
		Table("security.sensitive_findings AS f").
		Select("f.id AS finding_id, f.detector_code, f.detector_version, f.sensitive_data_type_id, COALESCE(r.decision, '') AS review_decision").
		Joins("JOIN security.protection_enrollments AS e ON e.tenant_id = f.tenant_id AND e.id = f.enrollment_id AND e.latest_discovery_execution_id = f.discovery_execution_id AND e.latest_source_snapshot_hash = f.source_snapshot_hash").
		Joins("LEFT JOIN security.sensitive_finding_reviews AS r ON r.tenant_id = f.tenant_id AND r.finding_id = f.id").
		Where("f.tenant_id = ? AND e.state <> ?", tenantID, models.EnrollmentStateReleased)
	if sensitiveDataTypeID != nil {
		currentQuery = currentQuery.Where("f.sensitive_data_type_id = ?", *sensitiveDataTypeID)
	}
	var current []currentDiscoveryQualityObservation
	if err := currentQuery.Scan(&current).Error; err != nil {
		return nil, err
	}
	for _, observation := range current {
		quality := ensureDiscoveryCapabilityQuality(qualityByCapability, observation.DetectorCode, observation.DetectorVersion, observation.SensitiveDataTypeID)
		quality.CurrentFindingCount++
		if strings.TrimSpace(observation.ReviewDecision) == "" {
			quality.AwaitingReviewCount++
		}
	}

	var reviewed []reviewedDiscoveryQualityObservation
	if err := s.db.WithContext(ctx).
		Table("security.sensitive_finding_reviews AS r").
		Select("f.enrollment_id, f.component_key, f.detector_code, f.detector_version, f.sensitive_data_type_id, r.decision").
		Joins("JOIN security.sensitive_findings AS f ON f.tenant_id = r.tenant_id AND f.id = r.finding_id").
		Where("r.tenant_id = ?", tenantID).
		Order("r.created_at DESC, r.id DESC").
		Scan(&reviewed).Error; err != nil {
		return nil, err
	}
	seenReviewedComponents := make(map[reviewedComponentKey]struct{}, len(reviewed))
	for _, observation := range reviewed {
		componentKey := reviewedComponentKey{
			enrollmentID: observation.EnrollmentID, componentKey: observation.ComponentKey, detectorVersion: observation.DetectorVersion,
		}
		if _, exists := seenReviewedComponents[componentKey]; exists {
			continue
		}
		seenReviewedComponents[componentKey] = struct{}{}
		if sensitiveDataTypeID != nil && observation.SensitiveDataTypeID != *sensitiveDataTypeID {
			continue
		}
		quality := ensureDiscoveryCapabilityQuality(qualityByCapability, observation.DetectorCode, observation.DetectorVersion, observation.SensitiveDataTypeID)
		switch observation.Decision {
		case models.FindingReviewDecisionConfirm:
			quality.ConfirmedCount++
		case models.FindingReviewDecisionAdjust:
			quality.AdjustedCount++
		case models.FindingReviewDecisionReject:
			quality.RejectedCount++
		}
	}

	result := &models.SensitiveDiscoveryQualitySummary{
		SensitiveDataTypeID: sensitiveDataTypeID,
		Capabilities:        make([]models.SensitiveDiscoveryCapabilityQuality, 0, len(qualityByCapability)),
	}
	for _, quality := range qualityByCapability {
		finalizeDiscoveryQualityMetrics(&quality.SensitiveDiscoveryQualityMetrics)
		result.CurrentFindingCount += quality.CurrentFindingCount
		result.AwaitingReviewCount += quality.AwaitingReviewCount
		result.ConfirmedCount += quality.ConfirmedCount
		result.AdjustedCount += quality.AdjustedCount
		result.RejectedCount += quality.RejectedCount
		result.Capabilities = append(result.Capabilities, *quality)
	}
	result.ReviewedSampleCount = result.ConfirmedCount + result.AdjustedCount + result.RejectedCount
	result.SensitiveConfirmationRate = sensitiveConfirmationRate(result.ConfirmedCount, result.AdjustedCount, result.ReviewedSampleCount)
	sort.Slice(result.Capabilities, func(i, j int) bool {
		if result.Capabilities[i].SensitiveDataTypeID != result.Capabilities[j].SensitiveDataTypeID {
			return result.Capabilities[i].SensitiveDataTypeID < result.Capabilities[j].SensitiveDataTypeID
		}
		return result.Capabilities[i].CapabilityKey < result.Capabilities[j].CapabilityKey
	})

	manualQuery := s.db.WithContext(ctx).
		Table("security.resource_security_assessments AS a").
		Select("r.conclusion, COUNT(*) AS count").
		Joins("JOIN security.resource_security_assessment_revisions AS r ON r.tenant_id = a.tenant_id AND r.assessment_id = a.id AND r.revision = a.current_revision").
		Where("a.tenant_id = ? AND r.source_kind = ?", tenantID, models.AssessmentRevisionSourceManual)
	if sensitiveDataTypeID != nil {
		manualQuery = manualQuery.Where("r.sensitive_data_type_id = ?", *sensitiveDataTypeID)
	}
	type manualAssessmentCount struct {
		Conclusion string
		Count      int64
	}
	var manualCounts []manualAssessmentCount
	if err := manualQuery.Group("r.conclusion").Scan(&manualCounts).Error; err != nil {
		return nil, err
	}
	for _, count := range manualCounts {
		switch count.Conclusion {
		case models.AssessmentConclusionSensitive:
			result.ActiveManualAssessmentCount = count.Count
		case models.AssessmentConclusionNotSensitive:
			result.RevokedManualAssessmentCount = count.Count
		}
	}
	return result, nil
}

func ensureDiscoveryCapabilityQuality(items map[discoveryQualityKey]*models.SensitiveDiscoveryCapabilityQuality, detectorCode, detectorVersion string, sensitiveDataTypeID int64) *models.SensitiveDiscoveryCapabilityQuality {
	key := discoveryQualityKey{sensitiveDataTypeID: sensitiveDataTypeID, detectorVersion: detectorVersion}
	if item, exists := items[key]; exists {
		return item
	}
	item := &models.SensitiveDiscoveryCapabilityQuality{
		DetectorCode: detectorCode, CapabilityKey: detectorVersion, SensitiveDataTypeID: sensitiveDataTypeID,
	}
	items[key] = item
	return item
}

func finalizeDiscoveryQualityMetrics(metrics *models.SensitiveDiscoveryQualityMetrics) {
	metrics.ReviewedSampleCount = metrics.ConfirmedCount + metrics.AdjustedCount + metrics.RejectedCount
	metrics.SensitiveConfirmationRate = sensitiveConfirmationRate(metrics.ConfirmedCount, metrics.AdjustedCount, metrics.ReviewedSampleCount)
}

func sensitiveConfirmationRate(confirmed, adjusted, reviewed int64) *float64 {
	if reviewed == 0 {
		return nil
	}
	value := float64(confirmed+adjusted) / float64(reviewed)
	return &value
}
