package iam

import (
	"context"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
)

// ResolveCurrentSubjectRoles consumes only the caller's live browser credential.
// This is not an identity resolver and cannot enumerate another principal.
func (s *AuthContextService) ResolveCurrentSubjectRoles(ctx context.Context, bearer string) (*authorization.CurrentSubjectRoles, error) {
	if s == nil || s.repository == nil {
		return nil, commonapi.ErrUnauthorized
	}
	var result *authorization.CurrentSubjectRoles
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		snapshot, err := resolveAccessTokenSnapshot(ctx, tx, bearer)
		if err != nil {
			return err
		}
		if snapshot.PrincipalType != PrincipalTypeUser || snapshot.FamilyContextType != ContextTypeTenant || snapshot.TenantID == nil || snapshot.TenantMembershipID == nil {
			return commonapi.ErrForbidden
		}
		_, membership, now, err := lockAndValidateExecutionActor(ctx, tx, snapshot.FamilyPrincipalID, *snapshot.TenantMembershipID, *snapshot.TenantID, snapshot.FamilyAuthorizationVersion)
		if err != nil {
			return commonapi.ErrUnauthorized
		}
		// Recheck the credential after waiting for identity locks, then again after
		// the role read. Never use a transaction-start clock or a stale Family.
		snapshot, err = resolveAccessTokenSnapshot(ctx, tx, bearer)
		if err != nil {
			return err
		}
		now = snapshot.DatabaseTime.UTC()
		rows, err := tx.listEffectiveSubjectRoles(ctx, snapshot.FamilyPrincipalID, *snapshot.TenantID, now)
		if err != nil {
			return err
		}
		keys := make([]string, 0, len(rows))
		expires := snapshot.CredentialExpiresAt.UTC()
		if snapshot.FamilyExpiresAt.Before(expires) {
			expires = snapshot.FamilyExpiresAt.UTC()
		}
		if membership.ExpiresAt != nil && membership.ExpiresAt.Before(expires) {
			expires = membership.ExpiresAt.UTC()
		}
		for _, row := range rows {
			if len(keys) == 0 || keys[len(keys)-1] != row.RoleKey {
				keys = append(keys, row.RoleKey)
			}
			if row.ValidUntil != nil && row.ValidUntil.Before(expires) {
				expires = row.ValidUntil.UTC()
			}
		}
		snapshot, err = resolveAccessTokenSnapshot(ctx, tx, bearer)
		if err != nil {
			return err
		}
		now = snapshot.DatabaseTime.UTC()
		if !expires.After(now) {
			return commonapi.ErrUnauthorized
		}
		result = &authorization.CurrentSubjectRoles{TenantID: formatIAMID(*snapshot.TenantID), ActorPrincipalID: formatIAMID(snapshot.FamilyPrincipalID), TenantMembershipID: formatIAMID(*snapshot.TenantMembershipID), AuthorizationVersion: formatIAMID(snapshot.FamilyAuthorizationVersion), RoleKeys: keys, ObservedAt: now, ExpiresAt: expires}
		return nil
	})
	return result, err
}
