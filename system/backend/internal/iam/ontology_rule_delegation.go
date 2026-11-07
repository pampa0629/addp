package iam

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	"gorm.io/gorm/clause"
)

func canEvaluateOntologyRule(rows []RoleAssignmentPermissionProjection, tenantID int64) bool {
	return slices.ContainsFunc(rows, func(row RoleAssignmentPermissionProjection) bool {
		return row.ScopeType == "tenant" && row.TenantID != nil && *row.TenantID == tenantID && row.PermissionKey == "service.data_read.execute"
	}) && slices.ContainsFunc(rows, func(row RoleAssignmentPermissionProjection) bool {
		return row.ScopeType == "tenant" && row.TenantID != nil && *row.TenantID == tenantID && row.PermissionKey == "ontology.semantic.read"
	})
}

// ConsumeOntologyRuleEvaluation resolves the source User again and consumes only
// the matched evaluation. The caller is authenticated separately as addp-service.
func (s *DelegationService) ConsumeOntologyRuleEvaluation(ctx context.Context, tenantID int64, call authorization.OntologyRuleCall, audit AuditMetadata) (*authorization.CurrentSubjectRoles, error) {
	digest := call.Evaluation.Digest()
	if digest == "" || tenantID <= 0 || !validOntologyCallID(call.AgentRunID) || !validOntologyCallID(call.ToolCallID) {
		return nil, commonapi.ErrBadRequest
	}
	var result *authorization.CurrentSubjectRoles
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		var token DelegatedAccessToken
		found := tx.db.WithContext(ctx).Where("agent_run_id = ? AND tool_call_id = ? AND audience = ? AND rule_evaluation_digest = ?", call.AgentRunID, call.ToolCallID, "ontology", digest).Find(&token)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected != 1 || !slices.Equal(token.Scopes, []string{authorization.OntologyRuleEvaluateTool}) {
			return commonapi.ErrForbidden
		}
		snapshot, err := tx.GetDelegatedAccessTokenAuthSnapshot(ctx, token.TokenHash)
		if err != nil {
			return err
		}
		if !matchesOntologyRuleSubject(snapshot, tenantID, call.PrincipalID) {
			return commonapi.ErrForbidden
		}
		if _, err = tx.LockPrincipal(ctx, snapshot.FamilyPrincipalID); err != nil {
			return err
		}
		if _, err = tx.LockRefreshTokenFamily(ctx, snapshot.CredentialFamilyID); err != nil {
			return err
		}
		if _, err = tx.LockAccessToken(ctx, token.SourceAccessTokenID); err != nil {
			return err
		}
		if err = tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&token, token.ID).Error; err != nil {
			return err
		}
		if token.RuleEvaluationConsumedAt != nil {
			return ErrDelegationConflict
		}
		snapshot, err = tx.GetDelegatedAccessTokenAuthSnapshot(ctx, token.TokenHash)
		if err != nil {
			return err
		}
		if !matchesOntologyRuleSubject(snapshot, tenantID, call.PrincipalID) || validateDelegatedAccessTokenSnapshot(snapshot) != nil {
			return commonapi.ErrForbidden
		}
		now := snapshot.DatabaseTime.UTC()
		permissions, err := tx.ListEffectiveRoleAssignmentPermissions(ctx, snapshot.FamilyPrincipalID, snapshot.PrincipalType, snapshot.FamilyContextType, snapshot.TenantID, snapshot.TenantMembershipID, now)
		if err != nil {
			return err
		}
		if !canEvaluateOntologyRule(permissions, tenantID) {
			return commonapi.ErrForbidden
		}
		roles, err := tx.listEffectiveSubjectRoles(ctx, snapshot.FamilyPrincipalID, tenantID, now)
		if err != nil {
			return err
		}
		keys := make([]string, 0, len(roles))
		expires := token.ExpiresAt.UTC()
		if snapshot.TenantMembershipExpiresAt != nil {
			expires = earlierTime(expires, snapshot.TenantMembershipExpiresAt.UTC())
		}
		for _, role := range roles {
			if len(keys) == 0 || keys[len(keys)-1] != role.RoleKey {
				keys = append(keys, role.RoleKey)
			}
			if role.ValidUntil != nil {
				expires = earlierTime(expires, role.ValidUntil.UTC())
			}
		}
		if !expires.After(now) {
			return commonapi.ErrForbidden
		}
		if err = tx.db.WithContext(ctx).Model(&token).UpdateColumns(map[string]any{"rule_evaluation_consumed_at": now, "revoked_at": now}).Error; err != nil {
			return fmt.Errorf("consume ontology fact claim: %w", err)
		}
		contextType := ContextTypeTenant
		audit.ContextType, audit.TenantID = &contextType, snapshot.TenantID
		if err = NewAuditWriter(tx).Write(ctx, AuditEvent{Metadata: audit, EventName: "iam.ontology_rule.consumed",
			Result: AuditResultSucceeded, RiskLevel: AuditRiskMedium, ModuleName: "system", EntityType: "delegated_access_token", EntityID: strconv.FormatInt(token.ID, 10),
			Details: map[string]any{"agent_run_id": call.AgentRunID, "tool_call_id": call.ToolCallID, "source_principal_id": snapshot.FamilyPrincipalID}}); err != nil {
			return fmt.Errorf("audit ontology fact claim: %w", err)
		}
		result = &authorization.CurrentSubjectRoles{TenantID: formatIAMID(tenantID), ActorPrincipalID: call.PrincipalID,
			TenantMembershipID: formatIAMID(*snapshot.TenantMembershipID), AuthorizationVersion: formatIAMID(snapshot.FamilyAuthorizationVersion),
			RoleKeys: keys, ObservedAt: now, ExpiresAt: expires}
		return nil
	})
	return result, err
}

func matchesOntologyRuleSubject(snapshot *SessionCredentialAuthSnapshot, tenantID int64, principal string) bool {
	return snapshot.TenantID != nil && snapshot.TenantMembershipID != nil &&
		*snapshot.TenantID == tenantID && formatIAMID(snapshot.FamilyPrincipalID) == principal
}

func validOntologyCallID(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && len(value) <= maxDelegationBindingLength
}
