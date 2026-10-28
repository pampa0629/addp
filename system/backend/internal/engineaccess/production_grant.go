package engineaccess

import (
	"context"
	"errors"
	"fmt"
	"sort"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ProductionRuntimeActor struct {
	Actor
	ClientID string
}

func (s *Service) withProductionRuntime(ctx context.Context, actor ProductionRuntimeActor, operation func(*Repository, func() error) error, humans ...int64) error {
	if actor.ClientID != "addp-transfer" {
		return commonapi.ErrForbidden
	}
	return s.withSourceRuntime(ctx, actor.Actor, actor.ClientID, authorization.PermissionSystemEngineAccessInitialGrantExecute, operation, humans...)
}

func productionExecutionID(id string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed == uuid.Nil || parsed.String() != id {
		return uuid.Nil, commonapi.ErrBadRequest
	}
	return parsed, nil
}

func (r *Repository) productionResult(ctx context.Context, row *initialProductionOutcome) (*shared.InitialProductionResult, error) {
	result := &shared.InitialProductionResult{SchemaVersion: shared.InitialProductionResultSchema, TenantID: row.TenantID,
		ProducerOwner: row.ProducerOwner, ExecutionID: row.ExecutionID, Outcome: row.Outcome, RecordedAt: row.RecordedAt}
	if row.Outcome == "issued" {
		g, err := r.initialProductionGrantHistory(ctx, row)
		if err != nil {
			return nil, err
		}
		result.Grant = &shared.InitialProductionGrant{RequestID: g.RequestID, GrantedAt: g.GrantedAt, ExpiryMode: g.ExpiryMode, ExpiresAt: g.ExpiresAt}
	} else if row.DenialCode != nil {
		result.DenialCode = *row.DenialCode
	}
	if err := result.Validate(); err != nil {
		return nil, err
	}
	return result, nil
}

// History recovery only requalifies the current producer runtime; it must not
// call Meta, recalculate expiry or require the historical human to remain active.
func (s *Service) ResolveInitialTransferGrant(ctx context.Context, actor ProductionRuntimeActor, id string) (*shared.InitialProductionLookup, error) {
	executionID, err := productionExecutionID(id)
	if err != nil {
		return nil, err
	}
	result := &shared.InitialProductionLookup{}
	err = s.withProductionRuntime(ctx, actor, func(tx *Repository, check func() error) error {
		row, err := tx.findInitialProductionOutcome(ctx, actor.TenantID, executionID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return check()
		}
		if err != nil {
			return err
		}
		result.Result, err = tx.productionResult(ctx, row)
		if err != nil {
			return err
		}
		result.Found = true
		return check()
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) IssueInitialTransferGrant(ctx context.Context, actor ProductionRuntimeActor, id string) (*shared.InitialProductionResult, error) {
	executionID, err := productionExecutionID(id)
	if err != nil {
		return nil, err
	}
	prior, err := s.ResolveInitialTransferGrant(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	if prior.Found {
		return prior.Result, nil
	}
	prepared, err := s.prepareInitialTransferProduction(ctx, actor.TenantID, id)
	if err != nil {
		// Only explicit owner evidence is a permanent refusal. Unknown replies,
		// dependency failures and missing Meta facts remain pending, never Allow.
		code := ""
		if errors.Is(err, errInitialProductionEvidence) {
			code = "production_ineligible"
		}
		if errors.Is(err, errProductionTargetChanged) {
			code = "engine_changed"
		}
		if code == "" {
			return nil, err
		}
		return s.recordInitialProductionDenial(ctx, actor, executionID, code)
	}
	var result *shared.InitialProductionResult
	err = s.withProductionRuntime(ctx, actor, func(tx *Repository, runtimeCheck func() error) error {
		// The wrapper locked the union of runtime/human principals first, so
		// role, membership and organization mutations cannot cross issuance.
		if _, err := tx.lockUserProvenance(ctx, actor.TenantID, prepared.sources.Source); err != nil {
			return err
		}
		engineIDs := make([]int64, 0, len(prepared.engineVersions))
		for id := range prepared.engineVersions {
			engineIDs = append(engineIDs, id)
		}
		sort.Slice(engineIDs, func(i, j int) bool { return engineIDs[i] < engineIDs[j] })
		for _, id := range engineIDs {
			var row struct{ ID int64 }
			if err := tx.db.WithContext(ctx).Table("system.engines").Select("id").Where("tenant_id=? AND id=?", actor.TenantID, id).
				Clauses(clause.Locking{Strength: "SHARE"}).Take(&row).Error; err != nil {
				return err
			}
		}
		if err := tx.lockInitialProduction(ctx, actor.TenantID, executionID); err != nil {
			return err
		}
		if row, err := tx.findInitialProductionOutcome(ctx, actor.TenantID, executionID); err == nil {
			result, err = tx.productionResult(ctx, row)
			if err != nil {
				return err
			}
			return runtimeCheck()
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.lockProductionTargets(ctx, prepared); err != nil {
			return err
		}
		observation, err := tx.queryInitialProduction(ctx, prepared)
		if err != nil {
			if errors.Is(err, errProductionTargetChanged) {
				result, err = tx.denyInitialProduction(ctx, actor, executionID, "engine_changed")
				if err != nil {
					return err
				}
				return runtimeCheck()
			}
			return err
		}
		code := ""
		if !observation.executionIAM {
			code = "execution_permission_denied"
		}
		if !observation.sourceRules.Covered {
			code = "source_access_denied"
		}
		if observation.targetHasHistory {
			code = "target_history_exists"
		}
		if code != "" {
			result, err = tx.denyInitialProduction(ctx, actor, executionID, code)
			if err != nil {
				return err
			}
			return runtimeCheck()
		}
		r := prepared.production.receipt
		binding := prepared.production.binding
		path, err := shared.EncodeSharingTarget(r.Target)
		if err != nil {
			return err
		}
		requestID := productionGrantCommand(actor.TenantID, executionID)
		mode := shared.SharingExpiryUntilRevoked
		if observation.sourceRules.ExpiresAt != nil {
			mode = shared.SharingExpiryAtTime
		}
		owner := productionOwner
		grant := &sourceGrant{RequestID: requestID, ApprovalMode: productionApprovalMode, TenantID: actor.TenantID, EngineID: int64(r.Target.EngineID),
			CatalogPath: path, ItemFingerprint: binding.ItemFingerprint, BoundaryHash: binding.BoundaryHash, RecipientType: "user", RecipientID: prepared.sources.Source.PrincipalID,
			Action: "read", ExpiryMode: mode, ExpiresAt: observation.sourceRules.ExpiresAt,
			OperatorPrincipalID: prepared.sources.Source.PrincipalID, OperatorMembershipID: prepared.sources.Source.MembershipID, OperatorAuthorizationVersion: prepared.sources.Source.AuthorizationVersion,
			ProducerOwner: &owner, ProductionExecutionID: &executionID, IssuerPrincipalID: &actor.PrincipalID, IssuerMembershipID: &actor.MembershipID}
		outcome, err := tx.insertInitialProductionOutcome(ctx, actor.Actor, executionID, &requestID, "")
		if err != nil {
			return err
		}
		if err := tx.insertSourceGrant(ctx, grant); err != nil {
			return err
		}
		if err := tx.auditInitialProduction(ctx, actor, executionID, "issued", ""); err != nil {
			return err
		}
		// Audit and pool/lock waits cannot carry naturally expired sources or
		// credentials across commit. Never re-freeze a different expiry here.
		final, err := tx.queryInitialProduction(ctx, prepared)
		if err != nil {
			return err
		}
		if !final.executionIAM || !final.sourceRules.Covered || !shared.EqualSharingExpiry(mode, grant.ExpiresAt, mode, final.sourceRules.ExpiresAt) {
			return errInitialProductionEvidence
		}
		if err := tx.checkProductionEngines(ctx, actor.TenantID, prepared.engineVersions); err != nil {
			return err
		}
		if err := runtimeCheck(); err != nil {
			return err
		}
		result, err = tx.productionResult(ctx, outcome)
		return err
	}, prepared.sources.Source.PrincipalID)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Repository) lockInitialProduction(ctx context.Context, tenantID int64, id uuid.UUID) error {
	return r.db.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", fmt.Sprintf("initial_production|%d|transfer|%s", tenantID, id)).Error
}

func (r *Repository) lockProductionTargets(ctx context.Context, prepared *preparedInitialProduction) error {
	type entry struct {
		path plugin.EngineCatalogPath
		key  string
	}
	var entries []entry
	for _, path := range append(prepared.sources.Targets[:len(prepared.sources.Targets):len(prepared.sources.Targets)], prepared.production.receipt.Target) {
		fingerprint, err := authorizationfacts.NativeCandidateFingerprint(path)
		if err != nil {
			return err
		}
		entries = append(entries, entry{path: path, key: fmt.Sprintf("%020d|%s", path.EngineID, fingerprint)})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].key < entries[j].key })
	for _, e := range entries {
		path, err := shared.EncodeSharingTarget(e.path)
		if err != nil {
			return err
		}
		if err := r.lockFulfillmentTarget(ctx, prepared.sources.TenantID, path); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) recordInitialProductionDenial(ctx context.Context, actor ProductionRuntimeActor, id uuid.UUID, code string) (*shared.InitialProductionResult, error) {
	var result *shared.InitialProductionResult
	err := s.withProductionRuntime(ctx, actor, func(tx *Repository, check func() error) error {
		if err := tx.lockInitialProduction(ctx, actor.TenantID, id); err != nil {
			return err
		}
		row, err := tx.findInitialProductionOutcome(ctx, actor.TenantID, id)
		if err == nil {
			result, err = tx.productionResult(ctx, row)
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			result, err = tx.denyInitialProduction(ctx, actor, id, code)
		}
		if err != nil {
			return err
		}
		return check()
	})
	return result, err
}

func (r *Repository) denyInitialProduction(ctx context.Context, actor ProductionRuntimeActor, id uuid.UUID, code string) (*shared.InitialProductionResult, error) {
	if !shared.ValidInitialProductionDenial(code) {
		return nil, errInitialProductionEvidence
	}
	row, err := r.insertInitialProductionOutcome(ctx, actor.Actor, id, nil, code)
	if err != nil {
		return nil, err
	}
	if err := r.auditInitialProduction(ctx, actor, id, "denied", code); err != nil {
		return nil, err
	}
	return r.productionResult(ctx, row)
}

func (r *Repository) auditInitialProduction(ctx context.Context, actor ProductionRuntimeActor, id uuid.UUID, outcome, code string) error {
	p, c := iam.PrincipalTypeServicePrincipal, iam.ContextTypeTenant
	return iam.NewAuditWriter(r.identity()).Write(ctx, iam.AuditEvent{
		Metadata:  iam.AuditMetadata{PrincipalID: &actor.PrincipalID, PrincipalType: &p, TenantID: &actor.TenantID, ContextType: &c},
		EventName: "system.engine_access_initial_grant." + outcome, Result: iam.AuditResultSucceeded, RiskLevel: iam.AuditRiskHigh, ModuleName: "system",
		EntityType: "engine_access_initial_grant", EntityID: id.String(), Details: map[string]any{"producer_owner": productionOwner, "execution_id": id, "outcome": outcome, "denial_code": code}})
}
