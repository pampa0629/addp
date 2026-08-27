package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/addp/asset/internal/models"
	commonClient "github.com/addp/common/client"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	grantTargetModuleWorkbench         = "workbench"
	grantTargetResourceDataApplication = "data_application"
	grantFulfillmentPollInterval       = 2 * time.Second
)

type grantCatalogResolver interface {
	ResolveReferences(context.Context, uint, []uuid.UUID) ([]commonClient.CatalogReferenceResolution, error)
}

type grantOwnerClient interface {
	FulfillAssetGrant(context.Context, uint, int64, commonClient.WorkbenchAssetResourceGrantRequest) (*commonClient.WorkbenchAssetResourceGrantResponse, error)
	RevokeAssetGrant(context.Context, uint, int64, commonClient.WorkbenchAssetResourceGrantRequest) (*commonClient.WorkbenchAssetResourceGrantResponse, error)
}

type catalogGrantResolver struct{ client *commonClient.CatalogClient }

func (resolver catalogGrantResolver) ResolveReferences(ctx context.Context, tenantID uint, ids []uuid.UUID) ([]commonClient.CatalogReferenceResolution, error) {
	return resolver.client.WithTenantID(tenantID).ResolveReferences(ctx, ids)
}

type workbenchGrantOwnerClient struct {
	client *commonClient.WorkbenchResourceGrantClient
}

func (owner workbenchGrantOwnerClient) FulfillAssetGrant(ctx context.Context, tenantID uint, sourceIdentity int64, request commonClient.WorkbenchAssetResourceGrantRequest) (*commonClient.WorkbenchAssetResourceGrantResponse, error) {
	return owner.client.WithTenantID(tenantID).FulfillAssetGrant(ctx, sourceIdentity, request)
}

func (owner workbenchGrantOwnerClient) RevokeAssetGrant(ctx context.Context, tenantID uint, sourceIdentity int64, request commonClient.WorkbenchAssetResourceGrantRequest) (*commonClient.WorkbenchAssetResourceGrantResponse, error) {
	return owner.client.WithTenantID(tenantID).RevokeAssetGrant(ctx, sourceIdentity, request)
}

type GrantFulfillmentService struct {
	db      *gorm.DB
	catalog grantCatalogResolver
	owner   grantOwnerClient
	now     func() time.Time
}

func NewGrantFulfillmentService(db *gorm.DB, catalog *commonClient.CatalogClient, owner *commonClient.WorkbenchResourceGrantClient) *GrantFulfillmentService {
	return &GrantFulfillmentService{
		db: db, catalog: catalogGrantResolver{client: catalog}, owner: workbenchGrantOwnerClient{client: owner},
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (s *GrantFulfillmentService) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(grantFulfillmentPollInterval)
		defer ticker.Stop()
		for {
			if _, err := s.ReconcileOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("Asset grant fulfillment failed: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// ReconcileOnce claims and fulfills at most one authorization so it is easy to
// exercise deterministically in tests and safe for multiple Asset replicas.
func (s *GrantFulfillmentService) ReconcileOnce(ctx context.Context) (bool, error) {
	authorization, err := s.claimOne(ctx)
	if err != nil || authorization == nil {
		return false, err
	}
	if err := s.ensureTarget(ctx, authorization); err != nil {
		s.recordFailure(authorization, err)
		return true, err
	}
	request, err := commonClient.NewWorkbenchDataApplicationGrantRequest(
		authorization.TargetResourceID, authorization.UserID, authorization.ExpiresAt,
	)
	if err != nil {
		s.recordFailure(authorization, err)
		return true, err
	}

	if authorization.Status == models.AuthorizationStatusPending {
		_, err = s.owner.FulfillAssetGrant(ctx, uint(authorization.TenantID), authorization.ID, request)
		if err == nil {
			now := s.now()
			err = s.db.WithContext(ctx).Model(&models.Authorization{}).
				Where("id = ? AND status = ?", authorization.ID, models.AuthorizationStatusPending).
				Updates(map[string]any{
					"status": models.AuthorizationStatusEffective, "fulfilled_at": now,
					"fulfillment_last_error": "", "next_attempt_at": nil,
				}).Error
		}
	} else {
		_, err = s.owner.RevokeAssetGrant(ctx, uint(authorization.TenantID), authorization.ID, request)
		if err == nil {
			now := s.now()
			err = s.db.WithContext(ctx).Model(&models.Authorization{}).
				Where("id = ? AND status = ?", authorization.ID, models.AuthorizationStatusRevocationPending).
				Updates(map[string]any{
					"status": models.AuthorizationStatusRevoked, "revoked_at": now,
					"fulfillment_last_error": "", "next_attempt_at": nil,
				}).Error
		}
	}
	if err != nil {
		s.recordFailure(authorization, err)
		return true, err
	}
	return true, nil
}

func (s *GrantFulfillmentService) claimOne(ctx context.Context) (*models.Authorization, error) {
	now := s.now()
	var claimed models.Authorization
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where(`((status IN ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?))
			       OR (status = ? AND expires_at IS NOT NULL AND expires_at <= ?))`,
				[]string{models.AuthorizationStatusPending, models.AuthorizationStatusRevocationPending}, now,
				models.AuthorizationStatusEffective, now).
			Order("updated_at ASC, id ASC").Limit(1).Find(&claimed)
		if query.Error != nil {
			return query.Error
		}
		if claimed.Status == models.AuthorizationStatusEffective {
			claimed.Status = models.AuthorizationStatusRevocationPending
		}
		claimed.FulfillmentAttempt++
		nextAttempt := now.Add(grantRetryDelay(claimed.FulfillmentAttempt))
		claimed.NextAttemptAt = &nextAttempt
		return tx.Model(&models.Authorization{}).Where("id = ?", claimed.ID).Updates(map[string]any{
			"status": claimed.Status, "fulfillment_attempt": claimed.FulfillmentAttempt, "next_attempt_at": nextAttempt,
		}).Error
	})
	if err != nil || claimed.ID == 0 {
		return nil, err
	}
	return &claimed, nil
}

func (s *GrantFulfillmentService) ensureTarget(ctx context.Context, authorization *models.Authorization) error {
	if authorization.TargetModule != "" || authorization.TargetResourceType != "" || authorization.TargetResourceID != "" {
		if authorization.TargetModule != grantTargetModuleWorkbench || authorization.TargetResourceType != grantTargetResourceDataApplication {
			return errors.New("authorization target is not a Workbench data application")
		}
		resourceID, err := uuid.Parse(authorization.TargetResourceID)
		if err != nil || resourceID == uuid.Nil || resourceID.String() != authorization.TargetResourceID {
			return errors.New("authorization target has an invalid resource ID")
		}
		return nil
	}

	var component models.AssetComponent
	var count int64
	base := func() *gorm.DB {
		return s.db.WithContext(ctx).Table("asset.asset_components AS component").
			Joins("JOIN asset.assets asset ON asset.id = component.asset_id AND asset.tenant_id = component.tenant_id").
			Joins("JOIN asset.type_definitions type ON type.id = asset.type_id").
			Where("component.tenant_id = ? AND component.asset_id = ? AND component.role = 'primary' AND asset.status = 'published' AND type.code = 'application'",
				authorization.TenantID, authorization.AssetID)
	}
	if err := base().Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return errors.New("application Asset has no unique published primary component")
	}
	if err := base().Select("component.*").First(&component).Error; err != nil {
		return fmt.Errorf("load application Asset primary component: %w", err)
	}
	resolutions, err := s.catalog.ResolveReferences(ctx, uint(authorization.TenantID), []uuid.UUID{component.CatalogEntryID})
	if err != nil {
		return fmt.Errorf("resolve application CatalogEntry: %w", err)
	}
	if len(resolutions) != 1 {
		return errors.New("application CatalogEntry resolution count mismatch")
	}
	resolution := resolutions[0]
	resourceID, parseErr := uuid.Parse(resolution.SourceIdentity)
	if parseErr != nil || resourceID == uuid.Nil || resourceID.String() != resolution.SourceIdentity || resolution.ID != component.CatalogEntryID ||
		!resolution.Found || !resolution.Publishable || resolution.EntryType != grantTargetResourceDataApplication ||
		resolution.SourceModule != grantTargetModuleWorkbench || resolution.SourceType != grantTargetResourceDataApplication {
		return errors.New("application CatalogEntry no longer resolves to a published Workbench data application")
	}
	updates := map[string]any{
		"target_module": grantTargetModuleWorkbench, "target_resource_type": grantTargetResourceDataApplication,
		"target_resource_id": resourceID.String(),
	}
	if err := s.db.WithContext(ctx).Model(&models.Authorization{}).Where("id = ?", authorization.ID).Updates(updates).Error; err != nil {
		return err
	}
	authorization.TargetModule = grantTargetModuleWorkbench
	authorization.TargetResourceType = grantTargetResourceDataApplication
	authorization.TargetResourceID = resourceID.String()
	return nil
}

func (s *GrantFulfillmentService) recordFailure(authorization *models.Authorization, failure error) {
	if failure == nil || authorization == nil {
		return
	}
	message := failure.Error()
	if len(message) > 2000 {
		message = message[:2000]
	}
	_ = s.db.Model(&models.Authorization{}).Where("id = ?", authorization.ID).
		Update("fulfillment_last_error", message).Error
}

func grantRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := 5 * time.Second
	for index := 1; index < attempt && delay < 5*time.Minute; index++ {
		delay *= 2
	}
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}
