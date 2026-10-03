package engineaccess

import (
	"context"
	"errors"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Issuance history references the immutable accepted binding. It is NOT a
// current access decision, a token, or a copy of Catalog's business approval.
type fulfillmentGrant struct {
	RequestID uuid.UUID `gorm:"type:uuid;primaryKey"`
	GrantedAt time.Time
}

func (fulfillmentGrant) TableName() string { return "system.engine_access_grants" }

func (r *Repository) findFulfillmentGrant(ctx context.Context, id uuid.UUID) (*fulfillmentGrant, error) {
	var grant fulfillmentGrant
	if err := r.db.WithContext(ctx).Where("request_id = ?", id).Take(&grant).Error; err != nil {
		return nil, err
	}
	return &grant, nil
}

func (r *Repository) readFulfillmentGrant(ctx context.Context, request fulfillmentRequest) (*fulfillmentGrant, error) {
	path, binding, err := request.encode()
	if err != nil {
		return nil, err
	}
	var result *fulfillmentGrant
	err = r.readCommittedFulfillmentHistory(ctx, func(tx *Repository) error {
		row, err := tx.findFulfillment(ctx, request, path, binding)
		if err != nil {
			return err
		}
		if row.Outcome != "accepted" {
			return gorm.ErrRecordNotFound
		}
		result, err = tx.findFulfillmentGrant(ctx, request.RequestID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Pure history lookup: no issuance, closure, renewal or current access Allow.
// Read first, without holding IAM locks or occupying a second connection;
// expose neither history nor binding errors until current caller checks pass.
func (s *Service) resolveAcceptedGrant(ctx context.Context, actor FulfillmentRuntimeActor, id uuid.UUID,
	binding shared.SharingFulfillmentBinding,
) (*fulfillmentGrant, error) {
	request, err := recoveryRequest(actor, id, binding)
	if err != nil {
		return nil, err
	}
	prior, readErr := s.repository.readFulfillmentGrant(ctx, request)
	err = s.withFulfillmentRuntime(ctx, actor, func(_ *Repository, check func() error) error {
		if readErr != nil && !errors.Is(readErr, gorm.ErrRecordNotFound) {
			return readErr
		}
		return check()
	})
	if err != nil {
		return nil, grantError(err)
	}
	return prior, nil
}

func grantWindow(row *fulfillmentOutcome, now time.Time) error {
	if row.Outcome != "accepted" || row.Deadline == nil {
		return errFulfillmentClosed
	}
	if now.Before(row.RecordedAt) || !now.Before(*row.Deadline) {
		return errFulfillmentExpired
	}
	return nil
}

// writeAcceptedGrant is internal only: no HTTP route or execution consumer.
// The original receipt supplies trusted provenance and exact parameters. New
// issuance checks current handler and recipient eligibility; historical retry
// checks only the current machine caller and the complete original binding.
func (s *Service) writeAcceptedGrant(ctx context.Context, actor FulfillmentRuntimeActor, id uuid.UUID,
	binding shared.SharingFulfillmentBinding,
) (*fulfillmentGrant, error) {
	request, err := recoveryRequest(actor, id, binding)
	if err != nil {
		return nil, err
	}
	path, encoded, err := request.encode()
	if err != nil {
		return nil, err
	}
	result, err := s.resolveAcceptedGrant(ctx, actor, id, binding)
	if err != nil {
		return nil, err
	}
	if result != nil {
		return result, nil
	}
	ids := []int64{request.Operator.PrincipalID}
	if request.RecipientType == "user" {
		ids = append(ids, request.RecipientID)
	}
	err = s.withFulfillmentRuntime(ctx, actor, func(tx *Repository, runtimeCheck func() error) error {
		row, err := tx.findFulfillment(ctx, request, path, encoded)
		if err != nil {
			return err
		}
		if row.Outcome != "accepted" {
			return errFulfillmentClosed
		}
		// A concurrent issuer may have committed while IAM locks were acquired.
		if prior, err := tx.findFulfillmentGrant(ctx, id); err == nil {
			result = prior
			return runtimeCheck()
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		// The runtime wrapper already locked the union of all principals, in
		// sorted order, before any membership/tenant lock. These are re-reads.
		principals, err := tx.identity().LockUserAuthorizationPrincipals(ctx, ids...)
		if err != nil {
			return err
		}
		operator, err := tx.lockUserProvenance(ctx, request.TenantID, request.Operator)
		if err != nil {
			return err
		}
		recipient, err := tx.lockFulfillmentRecipient(ctx, request, principals)
		if err != nil {
			return err
		}
		now, err := tx.wallClock(ctx)
		if err != nil {
			return err
		}
		permissions, err := tx.identity().ListEffectiveRoleAssignmentPermissions(ctx, request.Operator.PrincipalID,
			iam.PrincipalTypeUser, iam.ContextTypeTenant, &request.TenantID, &request.Operator.MembershipID, now)
		if err != nil {
			return err
		}
		scope, err := tx.lockManagementScope(ctx, request.TenantID, int64(request.Path.EngineID), request.Operator.MembershipID)
		if err != nil {
			return err
		}
		if err := tx.db.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "engine_access_request|"+id.String()).Error; err != nil {
			return err
		}
		if err := tx.lockFulfillmentTarget(ctx, request.TenantID, path); err != nil {
			return err
		}
		if prior, err := tx.findFulfillmentGrant(ctx, id); err == nil {
			result = prior
			return runtimeCheck()
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		check := func() error {
			if err := runtimeCheck(); err != nil {
				return err
			}
			now, err := tx.wallClock(ctx)
			if err != nil {
				return err
			}
			if err := grantWindow(row, now); err != nil {
				return err
			}
			if err := operator.check(now); err != nil {
				return err
			}
			if err := recipient.check(now); err != nil {
				return err
			}
			if err := scope.check(now); err != nil {
				return err
			}
			if !hasCurrentTenantPermission(permissions, request.TenantID, "system.engine_access_fulfillment.create", now) {
				return commonapi.ErrForbidden
			}
			return nil
		}
		if err := check(); err != nil {
			return err
		}
		grant := &fulfillmentGrant{RequestID: id}
		// The insert trigger sets database wall time and checks the window again.
		if err := tx.db.WithContext(ctx).Clauses(clause.Returning{}).Create(grant).Error; err != nil {
			return err
		}
		principalType, contextType := iam.PrincipalTypeServicePrincipal, iam.ContextTypeTenant
		if err := iam.NewAuditWriter(tx.identity()).Write(ctx, iam.AuditEvent{
			Metadata: iam.AuditMetadata{PrincipalID: &request.CallerPrincipalID, PrincipalType: &principalType,
				ContextType: &contextType, TenantID: &request.TenantID},
			EventName: "system.engine_access_grant.issued", Result: iam.AuditResultSucceeded,
			RiskLevel: iam.AuditRiskHigh, ModuleName: "system", EntityType: "engine_access_grant", EntityID: id.String(),
			Details: map[string]any{"request_id": id, "engine_id": request.Path.EngineID,
				"decision_id": request.DecisionID, "operator": request.Operator},
		}); err != nil {
			return err
		}
		// Audit and lock waiting cannot carry expired eligibility into commit.
		if err := check(); err != nil {
			return err
		}
		result = grant
		return nil
	}, ids...)
	if err != nil {
		return nil, grantError(err)
	}
	return result, nil
}

func grantError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == "engine_access_grant_acceptance_window" {
		err = errFulfillmentExpired
	}
	if errors.Is(err, errFulfillmentClosed) || errors.Is(err, errFulfillmentExpired) {
		return errors.Join(commonapi.ErrConflict, err)
	}
	return recoveryError(err)
}
