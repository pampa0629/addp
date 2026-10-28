package service

import (
	"context"
	"errors"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	plugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/resourcetree"
	"github.com/addp/transfer/internal/models"
	"github.com/google/uuid"
)

func (s *ExecutionService) ReadProductionReceipt(ctx context.Context, tenantID uint, id string) (*client.TransferProductionReceipt, error) {
	parsed, err := uuid.Parse(id)
	if tenantID == 0 || err != nil || parsed == uuid.Nil || parsed.String() != id {
		return nil, commonapi.ErrBadRequest
	}
	item, err := s.taskExecutionRepo.GetByExecutionID(ctx, id, int(tenantID))
	if err != nil {
		return nil, err
	}
	if item.Module != execution.ModuleTransfer || item.TaskType != execution.TaskTypeSync || item.ExecutionBoundary != execution.ExecutionBoundaryBounded {
		return nil, commonapi.ErrNotFound
	}
	if raw, exists := item.Metadata[models.ProductionAuthorizationMetadataKey]; exists {
		state, err := models.DecodeProductionAuthorization(raw)
		if err != nil || state.Code == models.ProductionAuthorizationRetired {
			return nil, commonapi.ErrConflict
		}
	}
	record, err := models.DecodeTargetProduction(item.Metadata[models.TargetProductionMetadataKey])
	if err != nil || item.Status != execution.ExecutionStatusSuccess || record.Phase != models.TargetProductionCommitted || record.TenantID != item.TenantID || record.ExecutionID != item.ExecutionID || record.Attempt != item.Attempt || !sameReceiptRetry(record.RetryOfExecutionID, item.RetryOfExecutionID) {
		return nil, commonapi.ErrConflict
	}
	locator, err := resourcetree.ParseURI(record.TargetLocator)
	if err != nil || locator == nil || locator.ItemID != nil || locator.NodeID != nil || locator.ToURI() != record.TargetLocator || locator.Type != resourcetree.TypeTable || len(locator.Path) != 2 {
		return nil, commonapi.ErrConflict
	}
	path, err := resourcetree.EngineCatalogPathFromLocator(plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema), locator)
	if err != nil {
		return nil, commonapi.ErrConflict
	}
	result := &client.TransferProductionReceipt{SchemaVersion: client.TransferProductionReceiptSchema, TenantID: int64(item.TenantID), ExecutionID: item.ExecutionID, Attempt: item.Attempt, RetryOfExecutionID: record.RetryOfExecutionID, Target: path, Preparation: record.Preparation, CommittedAt: *record.CommittedAt}
	result.SourceReadSet = record.SourceReadSet.Clone()
	result.CreatedAndCommitted = record.CreatedAndCommitted
	if item.ActorPrincipalID != nil || item.ActorTenantMembershipID != nil || item.IssuedAuthorizationVersion != nil {
		copy := execution.TaskExecution{TenantID: item.TenantID}
		if execution.CopyActorOrigin(&copy, item) != nil {
			return nil, commonapi.ErrConflict
		}
		result.UserOrigin = &client.TransferProductionUserOrigin{PrincipalID: *copy.ActorPrincipalID, TenantMembershipID: *copy.ActorTenantMembershipID, AuthorizationVersion: *copy.IssuedAuthorizationVersion}
	}
	if err := result.Validate(); err != nil {
		return nil, errors.Join(commonapi.ErrConflict, err)
	}
	return result, nil
}

func sameReceiptRetry(left, right *string) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}
