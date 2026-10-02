package engineaccess

import (
	"context"

	"github.com/addp/common/authorization"
	systemauthorization "github.com/addp/system/internal/authorization"
)

func (s *Service) GetHandlingScope(ctx context.Context, actor Actor, engineID int64) (*authorization.EngineAccessHandlingScope, error) {
	var result *authorization.EngineAccessHandlingScope
	err := s.withApprovalRequirementScope(ctx, actor, engineID, systemauthorization.PermissionSystemEngineAccessFulfillmentCreate,
		func(tx *Repository, check func() error) error {
			now, err := tx.wallClock(ctx)
			if err != nil {
				return err
			}
			if err := check(); err != nil {
				return err
			}
			result = &authorization.EngineAccessHandlingScope{TenantID: actor.TenantID, EngineID: engineID,
				Operator: authorization.SharingFulfillmentOperator{PrincipalID: actor.PrincipalID, MembershipID: actor.MembershipID,
					AuthorizationVersion: actor.AuthorizationVersion}, VerifiedAt: now}
			return nil
		})
	if err != nil {
		return nil, err
	}
	return result, nil
}
