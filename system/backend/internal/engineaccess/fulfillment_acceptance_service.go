package engineaccess

import (
	"context"
	"encoding/json"
	"errors"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrFulfillmentCapability = errors.New("fulfillment capability unavailable")

type FulfillmentBasisReader interface {
	ReadFulfillmentBasis(context.Context, uint, uuid.UUID, shared.SharingFulfillmentBinding) (*shared.SharingFulfillmentBasis, error)
}

func (s *Service) WithFulfillmentBasisReader(reader FulfillmentBasisReader) *Service {
	s.fulfillmentBasis = reader
	return s
}

func (s *Service) AcceptFulfillment(ctx context.Context, actor FulfillmentRuntimeActor, id uuid.UUID, binding shared.SharingFulfillmentBinding) (*shared.SharingFulfillmentResolution, error) {
	request, err := recoveryRequest(actor, id, binding)
	if err != nil {
		return nil, err
	}
	// Authenticate before exposing a historical binding conflict or capability.
	if err := s.withFulfillmentRuntime(ctx, actor, func(_ *Repository, check func() error) error { return check() }); err != nil {
		return nil, recoveryError(err)
	}
	// Historical recovery works without the new credential and without current
	// owner/human eligibility. No network call ever holds System DB locks.
	row, err := s.repository.readFulfillment(ctx, request)
	if err == nil {
		err = s.withFulfillmentRuntime(ctx, actor, func(_ *Repository, check func() error) error { return check() })
		if err != nil {
			return nil, recoveryError(err)
		}
		return recoveryResolution(row, binding), nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, recoveryError(err)
	}
	if s.fulfillmentBasis == nil {
		return nil, ErrFulfillmentCapability
	}
	basis, err := s.fulfillmentBasis.ReadFulfillmentBasis(ctx, uint(actor.TenantID), id, binding)
	if err != nil {
		return nil, ErrFulfillmentCapability
	}
	if basis == nil || basis.RequestID != id || basis.TenantID != actor.TenantID || basis.Binding.Validate() != nil {
		return nil, commonapi.ErrForbidden
	}
	want, _ := json.Marshal(binding)
	actual, _ := json.Marshal(basis.Binding)
	confirmation := userProvenance{basis.Confirmation.PrincipalID, basis.Confirmation.MembershipID, basis.Confirmation.AuthorizationVersion}
	if !equalJSON(want, actual) || !confirmation.valid() {
		return nil, commonapi.ErrForbidden
	}
	ids := []int64{binding.Operator.PrincipalID, confirmation.PrincipalID}
	if binding.RecipientType == "user" {
		ids = append(ids, binding.RecipientID)
	}
	var result *shared.SharingFulfillmentResolution
	err = s.withFulfillmentRuntime(ctx, actor, func(tx *Repository, check func() error) error {
		// A concurrent accept/close may have committed during the owner lookup.
		// Its immutable result is recovery, not a new human authorization check.
		path, encoded, encodeErr := request.encode()
		if encodeErr != nil {
			return encodeErr
		}
		historical, historyErr := tx.findFulfillment(ctx, request, path, encoded)
		if historyErr == nil {
			result = recoveryResolution(historical, binding)
			return check()
		}
		if !errors.Is(historyErr, gorm.ErrRecordNotFound) {
			return historyErr
		}
		now, err := tx.wallClock(ctx)
		if err != nil {
			return err
		}
		rows, err := tx.identity().ListEffectiveRoleAssignmentPermissions(ctx, request.Operator.PrincipalID,
			iam.PrincipalTypeUser, iam.ContextTypeTenant, &request.TenantID, &request.Operator.MembershipID, now)
		if err != nil {
			return err
		}
		newAcceptance := false
		row, err := tx.settleFulfillment(ctx, request, true, confirmation, func(_ *Repository) error {
			newAcceptance = true
			now, err := tx.wallClock(ctx)
			if err != nil {
				return err
			}
			if !hasCurrentTenantPermission(rows, request.TenantID, "system.engine_access_fulfillment.create", now) {
				return commonapi.ErrForbidden
			}
			return check()
		})
		if errors.Is(err, errFulfillmentClosed) {
			return commonapi.ErrConflict
		}
		if errors.Is(err, errApprovalRequirementVersion) || errors.Is(err, errApprovalRequirementUnavailable) {
			return commonapi.ErrConflict
		}
		if err != nil {
			return err
		}
		if err := check(); err != nil {
			return err
		}
		now, err = tx.wallClock(ctx)
		if err != nil {
			return err
		}
		if newAcceptance && !hasCurrentTenantPermission(rows, request.TenantID, "system.engine_access_fulfillment.create", now) {
			return commonapi.ErrForbidden
		}
		result = recoveryResolution(row, binding)
		return nil
	}, ids...)
	if err != nil {
		return nil, recoveryError(err)
	}
	return result, nil
}
