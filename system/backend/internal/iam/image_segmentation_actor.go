package iam

import (
	"context"

	"gorm.io/gorm"
)

// CheckImageSegmentationActor shares the current-user and role checks used by
// execution authorization. The owner must establish the live execution edge
// and check both data resources within the same transaction.
func CheckImageSegmentationActor(ctx context.Context, db *gorm.DB, tenant, principalID, membershipID, version int64) error {
	return CheckCurrentExecutionActorPermissions(ctx, db, tenant, principalID, membershipID, version, []string{"manager.derived_artifact.create", "manager.data_item.read", "manager.data_item.create"})
}
