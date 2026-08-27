package service

import (
	"errors"
	"testing"
	"time"

	"github.com/addp/workbench/internal/models"
)

type memoryResourceGrantRepository struct {
	fulfilled *models.ResourceAccessRule
	revoked   *models.ResourceAccessRule
}

func (r *memoryResourceGrantRepository) FulfillAssetGrant(rule models.ResourceAccessRule) (*models.ResourceAccessRule, error) {
	rule.ID = "8cf79572-d2a4-49dc-9f9f-b53dc74d77a4"
	r.fulfilled = &rule
	return &rule, nil
}

func (r *memoryResourceGrantRepository) RevokeAssetGrant(rule models.ResourceAccessRule, revokedAt time.Time) (*models.ResourceAccessRule, error) {
	rule.ID = "8cf79572-d2a4-49dc-9f9f-b53dc74d77a4"
	rule.RevokedAt = &revokedAt
	r.revoked = &rule
	return &rule, nil
}

func TestResourceGrantServiceAllowsRevocationAfterExpiry(t *testing.T) {
	repository := &memoryResourceGrantRepository{}
	service := NewResourceGrantService(repository)
	expiredAt := time.Now().UTC().Add(-time.Hour)
	request := models.AssetResourceGrantRequest{
		ResourceType: models.ResourceTypeDataApplication,
		ResourceID:   "1714dcf7-f34e-4996-a8dc-3b88998ebe55",
		SubjectType:  models.ResourceAccessSubjectUser,
		SubjectID:    "91",
		Permission:   models.DataApplicationExecutePermission,
		ExpiresAt:    &expiredAt,
	}

	response, err := service.RevokeAssetGrant(7, "73", request)
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != models.ResourceGrantFulfillmentStatusRevoked || repository.revoked == nil || repository.revoked.SubjectID != 91 {
		t.Fatalf("response=%#v revoked=%#v", response, repository.revoked)
	}
	if _, err := service.FulfillAssetGrant(7, "73", request); !errors.Is(err, ErrInvalidResourceGrant) {
		t.Fatalf("expired fulfillment error = %v", err)
	}
}
