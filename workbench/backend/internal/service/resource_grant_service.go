package service

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/addp/workbench/internal/models"
	"github.com/addp/workbench/internal/repository"
	"github.com/google/uuid"
)

type resourceGrantRepository interface {
	FulfillAssetGrant(models.ResourceAccessRule) (*models.ResourceAccessRule, error)
	RevokeAssetGrant(models.ResourceAccessRule, time.Time) (*models.ResourceAccessRule, error)
}

type ResourceGrantService struct{ repository resourceGrantRepository }

func NewResourceGrantService(repository resourceGrantRepository) *ResourceGrantService {
	return &ResourceGrantService{repository: repository}
}

func (s *ResourceGrantService) FulfillAssetGrant(tenantID int64, sourceIdentity string, request models.AssetResourceGrantRequest) (*models.AssetResourceGrantResponse, error) {
	rule, err := normalizeAssetResourceGrant(tenantID, sourceIdentity, request, true)
	if err != nil {
		return nil, err
	}
	result, err := s.repository.FulfillAssetGrant(rule)
	if errors.Is(err, repository.ErrResourceGrantConflict) {
		return nil, ErrResourceGrantConflict
	}
	if err != nil {
		return nil, err
	}
	return assetResourceGrantResponse(*result), nil
}

func (s *ResourceGrantService) RevokeAssetGrant(tenantID int64, sourceIdentity string, request models.AssetResourceGrantRequest) (*models.AssetResourceGrantResponse, error) {
	rule, err := normalizeAssetResourceGrant(tenantID, sourceIdentity, request, false)
	if err != nil {
		return nil, err
	}
	result, err := s.repository.RevokeAssetGrant(rule, time.Now().UTC())
	if errors.Is(err, repository.ErrResourceGrantConflict) {
		return nil, ErrResourceGrantConflict
	}
	if err != nil {
		return nil, err
	}
	return assetResourceGrantResponse(*result), nil
}

func normalizeAssetResourceGrant(tenantID int64, sourceIdentity string, request models.AssetResourceGrantRequest, requireFutureExpiry bool) (models.ResourceAccessRule, error) {
	canonicalSourceID, err := parseCanonicalPositiveID(sourceIdentity)
	if err != nil {
		return models.ResourceAccessRule{}, ErrInvalidResourceGrant
	}
	resourceID, err := uuid.Parse(strings.TrimSpace(request.ResourceID))
	if err != nil || resourceID == uuid.Nil || resourceID.String() != request.ResourceID {
		return models.ResourceAccessRule{}, ErrInvalidResourceGrant
	}
	subjectID, err := parseCanonicalPositiveID(request.SubjectID)
	if err != nil || tenantID <= 0 || request.ResourceType != models.ResourceTypeDataApplication ||
		request.SubjectType != models.ResourceAccessSubjectUser || request.Permission != models.DataApplicationExecutePermission {
		return models.ResourceAccessRule{}, ErrInvalidResourceGrant
	}
	if request.ExpiresAt != nil {
		expiresAt := request.ExpiresAt.UTC()
		if requireFutureExpiry && !expiresAt.After(time.Now().UTC()) {
			return models.ResourceAccessRule{}, ErrInvalidResourceGrant
		}
		request.ExpiresAt = &expiresAt
	}
	return models.ResourceAccessRule{
		TenantID: tenantID, ResourceType: request.ResourceType, ResourceID: resourceID.String(),
		SubjectType: request.SubjectType, SubjectID: subjectID, Permission: request.Permission,
		Effect: models.ResourceAccessEffectAllow, SourceModule: models.ResourceAccessSourceAsset,
		SourceIdentity: strconv.FormatInt(canonicalSourceID, 10), ExpiresAt: request.ExpiresAt,
	}, nil
}

func parseCanonicalPositiveID(value string) (int64, error) {
	trimmed := strings.TrimSpace(value)
	parsed, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || parsed <= 0 || strconv.FormatInt(parsed, 10) != trimmed {
		return 0, ErrInvalidResourceGrant
	}
	return parsed, nil
}

func assetResourceGrantResponse(rule models.ResourceAccessRule) *models.AssetResourceGrantResponse {
	status := models.ResourceGrantFulfillmentStatusActive
	if rule.RevokedAt != nil {
		status = models.ResourceGrantFulfillmentStatusRevoked
	}
	return &models.AssetResourceGrantResponse{
		ID: rule.ID, SourceIdentity: rule.SourceIdentity, Status: status,
		ExpiresAt: rule.ExpiresAt, RevokedAt: rule.RevokedAt,
	}
}
