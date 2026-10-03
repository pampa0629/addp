package engineaccess

import (
	"context"
	"errors"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const FulfillmentReconcilePermission = "system.engine_access_fulfillment.execute"

type FulfillmentRuntimeActor struct {
	Actor
	ClientID string
}

func recoveryRequest(actor FulfillmentRuntimeActor, id uuid.UUID, binding shared.SharingFulfillmentBinding) (fulfillmentRequest, error) {
	if !validActor(actor.Actor) || actor.ClientID != "addp-catalog" || binding.CallerPrincipalID != actor.PrincipalID {
		return fulfillmentRequest{}, commonapi.ErrForbidden
	}
	if id == uuid.Nil || binding.Validate() != nil {
		return fulfillmentRequest{}, commonapi.ErrBadRequest
	}
	return fulfillmentRequest{RequestID: id, TenantID: actor.TenantID, CallerPrincipalID: actor.PrincipalID,
		Operator: userProvenance{binding.Operator.PrincipalID, binding.Operator.MembershipID, binding.Operator.AuthorizationVersion},
		Path:     binding.Path, DecisionID: binding.DecisionID, RequirementVersion: binding.RequirementVersion,
		RecipientType: binding.RecipientType, RecipientID: binding.RecipientID, Action: binding.Action,
		ExpiryMode: binding.ExpiryMode, ExpiresAt: binding.ExpiresAt}, nil
}

// Caller IAM is current; the original human, recipient and business basis are
// deliberately historical. Recovery must work after those identities expire.
func (s *Service) withFulfillmentRuntime(ctx context.Context, actor FulfillmentRuntimeActor, operation func(*Repository, func() error) error, humanPrincipals ...int64) error {
	return s.repository.transaction(ctx, func(tx *Repository) error {
		// Credential rotation locks OAuth Client before Principal. Match that
		// order; never acquire these locks after request/target arbitration.
		var client struct {
			ServicePrincipalID int64
			Status             string
		}
		if err := tx.db.WithContext(ctx).Table("system.oauth_clients").Select("service_principal_id, status").
			Where("client_id = ?", actor.ClientID).Clauses(clause.Locking{Strength: "SHARE"}).Take(&client).Error; err != nil {
			return err
		}
		// Union service and human principals before any membership/tenant lock.
		principals, err := tx.identity().LockUserAuthorizationPrincipals(ctx, append(humanPrincipals, actor.PrincipalID)...)
		if err != nil {
			return err
		}
		member, err := tx.identity().LockTenantRecipientMembership(ctx, actor.TenantID, actor.PrincipalID)
		if err != nil {
			return err
		}
		var tenant iam.Tenant
		if err := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).First(&tenant, actor.TenantID).Error; err != nil {
			return err
		}
		var service struct{ Name, OwnerScope string }
		if err := tx.db.WithContext(ctx).Table("system.service_principals").Where("id = ?", actor.PrincipalID).
			Clauses(clause.Locking{Strength: "SHARE"}).Take(&service).Error; err != nil {
			return err
		}
		now, err := tx.wallClock(ctx)
		if err != nil {
			return err
		}
		rows, err := tx.identity().ListEffectiveRoleAssignmentPermissions(ctx, actor.PrincipalID,
			iam.PrincipalTypeServicePrincipal, iam.ContextTypeTenant, &actor.TenantID, &actor.MembershipID, now)
		if err != nil {
			return err
		}
		check := func() error {
			now, err := tx.wallClock(ctx)
			if err != nil {
				return err
			}
			p := principals[actor.PrincipalID]
			if client.ServicePrincipalID != actor.PrincipalID || client.Status != "active" || service.Name != "addp-catalog" || service.OwnerScope != "platform" ||
				p.PrincipalType != iam.PrincipalTypeServicePrincipal || p.Status != iam.PrincipalStatusActive || p.AuthorizationVersion != actor.AuthorizationVersion ||
				member.ID != actor.MembershipID || member.Status != iam.TenantMembershipStatusActive || (member.ExpiresAt != nil && !member.ExpiresAt.After(now)) ||
				tenant.Status != iam.TenantStatusActive || !hasCurrentTenantPermission(rows, actor.TenantID, FulfillmentReconcilePermission, now) {
				return commonapi.ErrForbidden
			}
			if !actor.TokenExpiresAt.After(now) {
				return commonapi.ErrUnauthorized
			}
			return nil
		}
		if err := check(); err != nil {
			return err
		}
		return operation(tx, check)
	})
}

func recoveryResolution(row *fulfillmentOutcome, binding shared.SharingFulfillmentBinding) *shared.SharingFulfillmentResolution {
	return &shared.SharingFulfillmentResolution{RequestID: row.RequestID, TenantID: row.TenantID, Binding: binding,
		Outcome: row.Outcome, RecordedAt: row.RecordedAt, Deadline: row.Deadline}
}

func recoveryError(err error) error {
	if errors.Is(err, errFulfillmentBinding) {
		return errors.Join(commonapi.ErrConflict, err)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, commonapi.ErrNotFound) {
		return commonapi.ErrForbidden
	}
	return err
}

func (s *Service) ResolveFulfillment(ctx context.Context, actor FulfillmentRuntimeActor, id uuid.UUID, binding shared.SharingFulfillmentBinding) (*shared.SharingFulfillmentLookup, error) {
	request, err := recoveryRequest(actor, id, binding)
	if err != nil {
		return nil, err
	}
	result := &shared.SharingFulfillmentLookup{}
	// Fetch immutable committed history before qualification locks. The
	// result is not exposed until current caller qualification succeeds;
	// never occupy a second pooled connection under a qualification transaction.
	row, readErr := s.repository.readFulfillment(ctx, request)
	err = s.withFulfillmentRuntime(ctx, actor, func(_ *Repository, check func() error) error {
		if readErr != nil && !errors.Is(readErr, gorm.ErrRecordNotFound) {
			return recoveryError(readErr)
		}
		if readErr == nil {
			result.Found, result.Resolution = true, recoveryResolution(row, binding)
		}
		return check()
	})
	if err != nil {
		return nil, recoveryError(err)
	}
	return result, nil
}

func (s *Service) CloseFulfillment(ctx context.Context, actor FulfillmentRuntimeActor, id uuid.UUID, binding shared.SharingFulfillmentBinding) (*shared.SharingFulfillmentResolution, error) {
	request, err := recoveryRequest(actor, id, binding)
	if err != nil {
		return nil, err
	}
	var result *shared.SharingFulfillmentResolution
	err = s.withFulfillmentRuntime(ctx, actor, func(tx *Repository, check func() error) error {
		row, err := tx.settleFulfillment(ctx, request, false, userProvenance{}, nil)
		if err != nil {
			return err
		}
		// Waiting cannot carry a revoked/expired service credential into commit.
		if err := check(); err != nil {
			return err
		}
		result = recoveryResolution(row, binding)
		return nil
	})
	if err != nil {
		return nil, recoveryError(err)
	}
	return result, nil
}
