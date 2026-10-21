package iam

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	"github.com/google/uuid"
)

type EmergencySessionService struct{ repository *Repository }

func NewEmergencySessionService(r *Repository) *EmergencySessionService {
	return &EmergencySessionService{repository: r}
}

func emergencyActor(ctx context.Context, r *Repository, token, permission string, stepUp bool) (*SessionCredentialAuthSnapshot, *commonauth.AuthContext, error) {
	resolver, err := NewAuthContextService(r)
	if err != nil {
		return nil, nil, err
	}
	auth, err := resolver.ResolveFirstPartyAccessToken(ctx, token)
	if err != nil {
		return nil, nil, err
	}
	if auth.Principal.Type != "user" || auth.Client.ClientID == nil || *auth.Client.ClientID != "addp-web" ||
		auth.Token.Type != "first_party_access_token" || !commonauth.HasContextPermissions(*auth, emergencyPermissionPrefix+permission) {
		return nil, nil, commonapi.ErrForbidden
	}
	snapshot, err := resolveFirstPartyAccessTokenSnapshot(ctx, r, token)
	if err != nil {
		return nil, nil, err
	}
	now, err := r.emergencyTime(ctx)
	if err != nil {
		return nil, nil, err
	}
	if auth.Authentication.AssuranceLevel != "aal2" && auth.Authentication.AssuranceLevel != "aal3" {
		return nil, nil, ErrStepUpRequired
	}
	if stepUp && (auth.Authentication.StepUpExpiresAt == nil || !auth.Authentication.StepUpExpiresAt.After(now)) {
		return nil, nil, ErrStepUpRequired
	}
	return snapshot, auth, nil
}

func emergencyTenantActor(s *SessionCredentialAuthSnapshot) bool {
	return s != nil && s.FamilyContextType == ContextTypeTenant && s.TenantID != nil && s.TenantMembershipID != nil
}

func (s *EmergencySessionService) Create(ctx context.Context, in CreateEmergencySessionInput) (*EmergencySessionRequest, error) {
	in.Reason, in.IncidentReference = strings.TrimSpace(in.Reason), strings.TrimSpace(in.IncidentReference)
	if s == nil || s.repository == nil || in.ID == uuid.Nil || in.TargetPrincipalID <= 0 || in.TargetFamilyID <= 0 ||
		in.Reason == "" || len(in.Reason) > 1000 || in.IncidentReference == "" || len(in.IncidentReference) > 200 {
		return nil, commonapi.ErrBadRequest
	}
	actor, _, err := emergencyActor(ctx, s.repository, in.AccessToken, "create", false)
	if err != nil {
		return nil, err
	}
	if !emergencyTenantActor(actor) || actor.FamilyPrincipalID == in.TargetPrincipalID {
		return nil, commonapi.ErrForbidden
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result *EmergencySessionRequest
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		if _, err := tx.lockPrincipalsInOrder(ctx, actor.FamilyPrincipalID, in.TargetPrincipalID); err != nil {
			return err
		}
		current, _, err := emergencyActor(ctx, tx, in.AccessToken, "create", false)
		if err != nil {
			return err
		}
		if !emergencyTenantActor(current) || current.FamilyPrincipalID != actor.FamilyPrincipalID || *current.TenantID != *actor.TenantID || *current.TenantMembershipID != *actor.TenantMembershipID {
			return commonapi.ErrUnauthorized
		}
		existing, err := tx.emergencyRequest(ctx, in.ID, false)
		if err == nil {
			if existing.TenantID != *current.TenantID || existing.ExecutorPrincipalID != current.FamilyPrincipalID {
				return commonapi.ErrNotFound
			}
			if existing.TargetPrincipalID != in.TargetPrincipalID || existing.TargetFamilyID != in.TargetFamilyID || existing.Reason != in.Reason || existing.IncidentReference != in.IncidentReference {
				return commonapi.ErrConflict
			}
			if err := tx.decorateEmergencyRequest(ctx, existing); err != nil {
				return err
			}
			result = existing
			return nil // Immutable command; no renewed deadline.
		}
		if !errors.Is(err, commonapi.ErrNotFound) {
			return err
		}
		family, err := tx.GetRefreshTokenFamily(ctx, in.TargetFamilyID)
		if err != nil {
			return err
		}
		if family.TenantMembershipID == nil {
			return commonapi.ErrNotFound
		}
		now, err := tx.emergencyTime(ctx)
		if err != nil {
			return err
		}
		row := &EmergencySessionRequest{ID: in.ID, Version: 1, TenantID: *current.TenantID, ExecutorPrincipalID: current.FamilyPrincipalID,
			ExecutorMembershipID: *current.TenantMembershipID, ExecutorAuthorizationVersion: current.PrincipalAuthorizationVersion,
			TargetPrincipalID: in.TargetPrincipalID, TargetMembershipID: *family.TenantMembershipID, TargetFamilyID: family.ID,
			Reason: in.Reason, IncidentReference: in.IncidentReference, Status: "pending", RequestedAt: now, ApprovalExpiresAt: now.Add(EmergencyApprovalTTL)}
		target, err := tx.GetPrincipal(ctx, in.TargetPrincipalID)
		if err != nil {
			return err
		}
		row.TargetAuthorizationVersion = target.AuthorizationVersion
		if err := tx.lockEmergencyBoundaries(ctx, row); err != nil {
			return err
		}
		if err := tx.validateEmergencyTargets(ctx, row, now); err != nil {
			return err
		}
		if _, err := tx.LockRefreshTokenFamily(ctx, current.CredentialFamilyID); err != nil {
			return err
		}
		if _, err := tx.LockAccessToken(ctx, current.CredentialID); err != nil {
			return err
		}
		if family, err = tx.LockRefreshTokenFamily(ctx, family.ID); err != nil {
			return err
		}
		if err := validateEmergencyFamily(row, family, now, true); err != nil {
			return err
		}
		if err := tx.db.WithContext(ctx).Create(row).Error; err != nil {
			return wrapRepositoryError(err)
		}
		if err := writeEmergencyAudit(ctx, tx, emergencyAuditActor(in.Audit, current), "created", row); err != nil {
			return err
		}
		finished, err := tx.emergencyTime(ctx)
		if err != nil {
			return err
		}
		if err := tx.validateEmergencyTargets(ctx, row, finished); err != nil {
			return err
		}
		if err := validateEmergencyFamily(row, family, finished, true); err != nil {
			return err
		}
		if _, _, err = emergencyActor(ctx, tx, in.AccessToken, "create", false); err != nil {
			return err
		}
		if err := tx.decorateEmergencyRequest(ctx, row); err != nil {
			return err
		}
		result = row
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Repository) lockEmergencyBoundaries(ctx context.Context, row *EmergencySessionRequest) error {
	for _, id := range normalizedPrincipalIDs([]int64{row.ExecutorMembershipID, row.TargetMembershipID}) {
		if _, err := r.LockTenantMembershipByID(ctx, id); err != nil {
			return err
		}
	}
	_, err := r.LockTenant(ctx, row.TenantID)
	return err
}

func (r *Repository) validateEmergencyTargets(ctx context.Context, row *EmergencySessionRequest, now time.Time) error {
	for _, binding := range []struct{ id, pid int64 }{{row.ExecutorMembershipID, row.ExecutorPrincipalID}, {row.TargetMembershipID, row.TargetPrincipalID}} {
		m, err := r.GetTenantMembershipByID(ctx, binding.id)
		if err != nil {
			return err
		}
		if m.TenantID != row.TenantID || m.PrincipalID != binding.pid {
			return commonapi.ErrNotFound
		}
		if m.Status != TenantMembershipStatusActive || m.ExpiresAt != nil && !m.ExpiresAt.After(now) {
			return commonapi.ErrForbidden
		}
	}
	tenant, err := r.GetTenant(ctx, row.TenantID)
	if err != nil {
		return err
	}
	if tenant.Status != TenantStatusActive {
		return commonapi.ErrForbidden
	}
	for id, version := range map[int64]int64{row.ExecutorPrincipalID: row.ExecutorAuthorizationVersion, row.TargetPrincipalID: row.TargetAuthorizationVersion} {
		p, err := r.GetPrincipal(ctx, id)
		if err != nil {
			return err
		}
		if p.PrincipalType != PrincipalTypeUser || p.Status != PrincipalStatusActive || p.AuthorizationVersion != version {
			return commonapi.ErrConflict
		}
		platform, err := r.hasEmergencyPlatformRole(ctx, id, "", now)
		if err != nil {
			return err
		}
		if platform {
			return commonapi.ErrForbidden
		}
	}
	return nil
}

func validateEmergencyFamily(row *EmergencySessionRequest, f *RefreshTokenFamily, now time.Time, active bool) error {
	if f.ID != row.TargetFamilyID || f.PrincipalID != row.TargetPrincipalID || f.ContextType != ContextTypeTenant ||
		f.TenantMembershipID == nil || *f.TenantMembershipID != row.TargetMembershipID || f.ClientID != "addp-web" || f.AuthType != "first_party" {
		return commonapi.ErrNotFound
	}
	if active && (f.RevokedAt != nil || !f.ExpiresAt.After(now) || f.IssuedAuthorizationVersion != row.TargetAuthorizationVersion) {
		return commonapi.ErrConflict
	}
	return nil
}

func (r *Repository) emergencyApproverRole(ctx context.Context, id int64, now time.Time) (string, error) {
	for _, role := range []string{"platform.system_administrator", "platform.security_administrator"} {
		ok, err := r.hasEmergencyPlatformRole(ctx, id, role, now)
		if err != nil {
			return "", err
		}
		if ok {
			return role, nil
		}
	}
	return "", commonapi.ErrForbidden
}

func (r *Repository) validateEmergencyApprovals(ctx context.Context, row *EmergencySessionRequest, now time.Time) error {
	for _, a := range row.Approvals {
		p, err := r.GetPrincipal(ctx, a.PrincipalID)
		if err != nil {
			return err
		}
		ok, err := r.hasEmergencyPlatformRole(ctx, a.PrincipalID, a.RoleKey, now)
		if err != nil {
			return err
		}
		if !ok || p.Status != PrincipalStatusActive || p.AuthorizationVersion != a.AuthorizationVersion {
			return commonapi.ErrConflict
		}
	}
	return nil
}

func (s *EmergencySessionService) Mutate(ctx context.Context, in MutateEmergencySessionInput, action string) (*EmergencySessionRequest, error) {
	if s == nil || s.repository == nil || in.ID == uuid.Nil || in.Version <= 0 {
		return nil, commonapi.ErrBadRequest
	}
	if action != "approve" && action != "reject" && action != "revoke" && action != "cancel" && action != "execute" {
		return nil, commonapi.ErrBadRequest
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if action == "execute" {
		if in.ExecutionID == uuid.Nil {
			return nil, commonapi.ErrBadRequest
		}
	} else if in.Reason == "" || len(in.Reason) > 1000 {
		return nil, commonapi.ErrBadRequest
	}
	actor, _, err := emergencyActor(ctx, s.repository, in.AccessToken, action, action != "cancel")
	if err != nil {
		return nil, err
	}
	source, err := s.repository.emergencyRequest(ctx, in.ID, false)
	if err != nil {
		return nil, err
	}
	if err := emergencyRequestScope(source, actor, action == "approve" || action == "reject" || action == "revoke"); err != nil {
		return nil, err
	}
	ids := []int64{actor.FamilyPrincipalID, source.ExecutorPrincipalID, source.TargetPrincipalID}
	for _, a := range source.Approvals {
		ids = append(ids, a.PrincipalID)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result *EmergencySessionRequest
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		if _, err := tx.lockPrincipalsInOrder(ctx, ids...); err != nil {
			return err
		}
		// Boundary locks precede credentials, request and target Family locks.
		if err := tx.lockEmergencyBoundaries(ctx, source); err != nil {
			return err
		}
		if _, err := tx.LockRefreshTokenFamily(ctx, actor.CredentialFamilyID); err != nil {
			return err
		}
		if _, err := tx.LockAccessToken(ctx, actor.CredentialID); err != nil {
			return err
		}
		current, _, err := emergencyActor(ctx, tx, in.AccessToken, action, action != "cancel")
		if err != nil {
			return err
		}
		row, err := tx.emergencyRequest(ctx, in.ID, true)
		if err != nil {
			return err
		}
		if err := emergencyRequestScope(row, current, action == "approve" || action == "reject" || action == "revoke"); err != nil {
			return err
		}
		expected, currentIDs := []int64{}, []int64{}
		for _, a := range source.Approvals {
			expected = append(expected, a.PrincipalID)
		}
		for _, a := range row.Approvals {
			currentIDs = append(currentIDs, a.PrincipalID)
		}
		if err := verifyMutationPrincipalSet(expected, currentIDs, nil); err != nil {
			return err
		}
		// A completed command is immutable history; never replay its writes.
		if action == "execute" && row.Status == "executed" {
			if row.ExecutionID != nil && *row.ExecutionID == in.ExecutionID {
				if err := tx.decorateEmergencyRequest(ctx, row); err != nil {
					return err
				}
				result = row
				return nil
			}
			return commonapi.ErrConflict
		}
		if row.Version != in.Version {
			return commonapi.ErrConflict
		}
		now, err := tx.emergencyTime(ctx)
		if err != nil {
			return err
		}
		updates := map[string]any{}
		if action == "approve" || action == "execute" {
			if err := tx.validateEmergencyTargets(ctx, row, now); err != nil {
				return err
			}
		}
		switch action {
		case "approve", "reject":
			if row.Status != "pending" || !row.ApprovalExpiresAt.After(now) {
				return commonapi.ErrConflict
			}
			role, err := tx.emergencyApproverRole(ctx, current.FamilyPrincipalID, now)
			if err != nil {
				return err
			}
			if current.FamilyPrincipalID == row.ExecutorPrincipalID || current.FamilyPrincipalID == row.TargetPrincipalID {
				return commonapi.ErrForbidden
			}
			if err := tx.validateEmergencyApprovals(ctx, row, now); err != nil {
				return err
			}
			if action == "approve" {
				for _, a := range row.Approvals {
					if a.PrincipalID == current.FamilyPrincipalID || a.RoleKey == role {
						return commonapi.ErrConflict
					}
				}
			}
			if action == "reject" {
				updates = emergencyCloseValues("rejected", current.FamilyPrincipalID, in.Reason)
			} else {
				a := EmergencySessionApproval{RequestID: row.ID, RoleKey: role, PrincipalID: current.FamilyPrincipalID, AuthorizationVersion: current.PrincipalAuthorizationVersion, Reason: in.Reason, ApprovedAt: now}
				if err := tx.db.WithContext(ctx).Create(&a).Error; err != nil {
					return wrapRepositoryError(err)
				}
				row.Approvals = append(row.Approvals, a)
				if len(row.Approvals) == 2 {
					updates["status"] = "approved"
					updates["approved_at"] = now
					updates["execute_before"] = now.Add(EmergencyExecutionTTL)
				}
			}
		case "revoke", "cancel":
			if row.Status != "pending" && row.Status != "approved" {
				return commonapi.ErrConflict
			}
			if action == "revoke" {
				if row.Status != "approved" {
					return commonapi.ErrConflict
				}
				if _, err := tx.emergencyApproverRole(ctx, current.FamilyPrincipalID, now); err != nil {
					return err
				}
			}
			status := "revoked"
			if action == "cancel" {
				status = "cancelled"
			}
			updates = emergencyCloseValues(status, current.FamilyPrincipalID, in.Reason)
		case "execute":
			if row.Status != "approved" || row.ExecuteBefore == nil || !row.ExecuteBefore.After(now) || len(row.Approvals) != 2 {
				return commonapi.ErrConflict
			}
			if err := tx.validateEmergencyApprovals(ctx, row, now); err != nil {
				return err
			}
			family, err := tx.LockRefreshTokenFamily(ctx, row.TargetFamilyID)
			if err != nil {
				return err
			}
			now, err = tx.emergencyTime(ctx)
			if err != nil {
				return err
			}
			if !row.ExecuteBefore.After(now) {
				return commonapi.ErrConflict
			}
			if err := validateEmergencyFamily(row, family, now, false); err != nil {
				return err
			}
			outcome := "revoked"
			if family.RevokedAt != nil {
				outcome = "already_revoked"
			} else if !family.ExpiresAt.After(now) {
				outcome = "already_expired"
			} else {
				if family.IssuedAuthorizationVersion != row.TargetAuthorizationVersion {
					return commonapi.ErrConflict
				}
				if err := tx.RevokeTokenFamily(ctx, family.ID, now, "emergency_session_revocation"); err != nil {
					return err
				}
			}
			updates["status"], updates["execution_id"], updates["executed_at"], updates["outcome"] = "executed", in.ExecutionID, now, outcome
		}
		if err := tx.updateEmergencyRequest(ctx, row, updates); err != nil {
			return err
		}
		updated, err := tx.emergencyRequest(ctx, row.ID, false)
		if err != nil {
			return err
		}
		if err := writeEmergencyAudit(ctx, tx, emergencyAuditActor(in.Audit, current), action, updated); err != nil {
			return err
		}
		// Audit/lock waits may cross an expiry. Such a transaction must roll back.
		finished, err := tx.emergencyTime(ctx)
		if err != nil {
			return err
		}
		if action == "approve" && !row.ApprovalExpiresAt.After(finished) || action == "execute" && !row.ExecuteBefore.After(finished) {
			return commonapi.ErrConflict
		}
		if action == "approve" || action == "execute" {
			if err := tx.validateEmergencyTargets(ctx, row, finished); err != nil {
				return err
			}
			if err := tx.validateEmergencyApprovals(ctx, updated, finished); err != nil {
				return err
			}
		}
		if _, _, err = emergencyActor(ctx, tx, in.AccessToken, action, action != "cancel"); err != nil {
			return err
		}
		if err := tx.decorateEmergencyRequest(ctx, updated); err != nil {
			return err
		}
		finished, err = tx.emergencyTime(ctx)
		if err != nil {
			return err
		}
		if action == "approve" && !row.ApprovalExpiresAt.After(finished) || action == "execute" && !row.ExecuteBefore.After(finished) {
			return commonapi.ErrConflict
		}
		result = updated
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func emergencyCloseValues(status string, actor int64, reason string) map[string]any {
	return map[string]any{"status": status, "closed_by_principal_id": actor, "close_reason": reason}
}

func emergencyRequestScope(row *EmergencySessionRequest, actor *SessionCredentialAuthSnapshot, platform bool) error {
	if platform {
		if actor.FamilyContextType != ContextTypePlatform {
			return commonapi.ErrForbidden
		}
		return nil
	}
	if !emergencyTenantActor(actor) || *actor.TenantID != row.TenantID || actor.FamilyPrincipalID != row.ExecutorPrincipalID {
		return commonapi.ErrNotFound
	}
	return nil
}

func emergencyAuditActor(metadata AuditMetadata, actor *SessionCredentialAuthSnapshot) AuditMetadata {
	pid, kind, scope := actor.FamilyPrincipalID, PrincipalTypeUser, actor.FamilyContextType
	metadata.PrincipalID, metadata.PrincipalType, metadata.ContextType = &pid, &kind, &scope
	metadata.TenantID = actor.TenantID
	return metadata
}

func writeEmergencyAudit(ctx context.Context, r *Repository, metadata AuditMetadata, action string, row *EmergencySessionRequest) error {
	return NewAuditWriter(r).Write(ctx, AuditEvent{Metadata: metadata, EventName: "iam.emergency_session_revocation." + action,
		Result: AuditResultSucceeded, RiskLevel: AuditRiskHigh, ModuleName: "system", EntityType: "emergency_session_request", EntityID: row.ID.String(),
		Details: map[string]any{"tenant_id": strconv.FormatInt(row.TenantID, 10), "executor_principal_id": strconv.FormatInt(row.ExecutorPrincipalID, 10), "target_principal_id": strconv.FormatInt(row.TargetPrincipalID, 10), "target_family_id": strconv.FormatInt(row.TargetFamilyID, 10), "reason": row.Reason, "incident_reference": row.IncidentReference, "version": strconv.FormatInt(row.Version, 10), "status": row.Status, "approvals": row.Approvals, "approval_expires_at": row.ApprovalExpiresAt, "execute_before": row.ExecuteBefore, "execution_id": row.ExecutionID, "outcome": row.Outcome, "close_reason": row.CloseReason}})
}

func (s *EmergencySessionService) Get(ctx context.Context, token string, id uuid.UUID) (*EmergencySessionRequest, error) {
	if s == nil || s.repository == nil || id == uuid.Nil {
		return nil, commonapi.ErrBadRequest
	}
	var result *EmergencySessionRequest
	err := s.repository.ReadOnlyRepeatableReadTransaction(ctx, func(tx *Repository) error {
		actor, _, err := emergencyActor(ctx, tx, token, "read", false)
		if err != nil {
			return err
		}
		row, err := tx.emergencyRequest(ctx, id, false)
		if err != nil {
			return err
		}
		if err := emergencyRequestScope(row, actor, actor.FamilyContextType == ContextTypePlatform); err != nil {
			return err
		}
		if err := tx.decorateEmergencyRequest(ctx, row); err != nil {
			return err
		}
		result = row
		return nil
	})
	return result, err
}

func (s *EmergencySessionService) List(ctx context.Context, token string, page, size int) ([]EmergencySessionRequest, int64, error) {
	if s == nil || s.repository == nil {
		return nil, 0, commonapi.ErrBadRequest
	}
	if err := validateManagementPagination(page, size); err != nil {
		return nil, 0, err
	}
	rows := []EmergencySessionRequest{}
	var count int64
	err := s.repository.ReadOnlyRepeatableReadTransaction(ctx, func(tx *Repository) error {
		actor, _, err := emergencyActor(ctx, tx, token, "read", false)
		if err != nil {
			return err
		}
		q := tx.db.WithContext(ctx).Model(&EmergencySessionRequest{})
		if actor.FamilyContextType != ContextTypePlatform {
			if !emergencyTenantActor(actor) {
				return commonapi.ErrForbidden
			}
			q = q.Where("tenant_id=? AND executor_principal_id=?", *actor.TenantID, actor.FamilyPrincipalID)
		}
		if err := q.Count(&count).Error; err != nil {
			return wrapRepositoryError(err)
		}
		if err := q.Order("requested_at DESC,id").Limit(size).Offset((page - 1) * size).Find(&rows).Error; err != nil {
			return wrapRepositoryError(err)
		}
		for i := range rows {
			row, err := tx.emergencyRequest(ctx, rows[i].ID, false)
			if err != nil {
				return err
			}
			if err := tx.decorateEmergencyRequest(ctx, row); err != nil {
				return err
			}
			rows[i] = *row
		}
		return nil
	})
	return rows, count, err
}
