package engineaccess

import (
	"context"
	"errors"
	"reflect"
	"sort"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/system/internal/iam"
)

var errInitialProductionEvidence = errors.Join(commonapi.ErrConflict, errors.New("initial production evidence is incomplete or ineligible"))

// All fields are private owner evidence, never a credential or a grant command.
// Preparation does not prove that creation and the successful write belonged to
// the same production. The committed logical-result fact, not survival of the
// original physical table until issuance, is the eligibility basis.
type preparedInitialProduction struct {
	production     *preparedTransferProduction
	sources        sourceReadRequest
	engineVersions map[int64]int64
}

func initialProductionSources(production *preparedTransferProduction) (sourceReadRequest, error) {
	if production == nil || production.receipt.Validate() != nil || production.binding.Validate() != nil || production.engineVersion <= 0 {
		return sourceReadRequest{}, errInitialProductionEvidence
	}
	r := production.receipt
	if !r.CreatedAndCommitted || r.Preparation != plugin.TableTargetCreated || r.Attempt != 1 || r.RetryOfExecutionID != nil || r.UserOrigin == nil ||
		r.SourceReadSet == nil || execution.ValidateProductionReadSet(r.SourceReadSet) != nil || !reflect.DeepEqual(r.Target, production.binding.NativeTarget) ||
		len(r.Target.Segments) != 3 || !reflect.DeepEqual(r.Target, plugin.TabularItemPath(r.Target.EngineID, plugin.EngineCatalogTermSchema, r.Target.Segments[1].Name, r.Target.Segments[2].Name)) {
		return sourceReadRequest{}, errInitialProductionEvidence
	}
	paths := r.SourceReadSet.Clone().Paths
	for _, path := range paths {
		if reflect.DeepEqual(path, r.Target) {
			return sourceReadRequest{}, errInitialProductionEvidence
		}
	}
	return sourceReadRequest{TenantID: r.TenantID, Source: userProvenance{r.UserOrigin.PrincipalID,
		r.UserOrigin.TenantMembershipID, r.UserOrigin.AuthorizationVersion}, Targets: paths}, nil
}

// Source facts use the same System-owned clients as target preparation. Cross-
// owner IO happens without any IAM or target transaction. Unsupported engines
// are not silently interpreted as unrestricted sources.
func (s *Service) prepareInitialTransferProduction(ctx context.Context, tenantID int64, executionID string) (*preparedInitialProduction, error) {
	production, err := s.prepareTransferProduction(ctx, tenantID, executionID)
	if err != nil {
		return nil, err
	}
	sources, err := initialProductionSources(production)
	if err != nil {
		return nil, err
	}
	versions := map[int64]int64{int64(production.receipt.Target.EngineID): production.engineVersion}
	for _, path := range sources.Targets {
		id := int64(path.EngineID)
		if _, exists := versions[id]; exists {
			continue
		}
		engine, err := s.repository.productionEngine(ctx, tenantID, id)
		if err != nil {
			return nil, err
		}
		versions[id] = engine.Version
	}
	bindings, readErr := s.resolveReadBindings(ctx, tenantID, sources.Targets)
	prepared := &preparedInitialProduction{production: production, sources: sources, engineVersions: versions}
	// Recheck every source and the target, including when Meta failed.
	if err := s.repository.checkProductionEngines(ctx, tenantID, versions); err != nil {
		return nil, err
	}
	if readErr != nil {
		return nil, readErr
	}
	prepared.sources.Bindings = bindings
	if _, _, err := prepared.sources.encode(); err != nil {
		return nil, err
	}
	return prepared, nil
}

func (r *Repository) checkProductionEngines(ctx context.Context, tenantID int64, versions map[int64]int64) error {
	ids := make([]int64, 0, len(versions))
	for id := range versions {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		engine, err := r.productionEngine(ctx, tenantID, id)
		if err != nil {
			return err
		}
		if engine.Version != versions[id] {
			return errProductionTargetChanged
		}
	}
	return nil
}

// This read-only observation is deliberately not an eligibility boolean or
// access verdict. It cannot freeze expiry or be consumed by insertSourceGrant.
type initialProductionObservation struct {
	sourceRules      *sourceReadRules
	executionIAM     bool
	targetHasHistory bool
}

func (r *Repository) observeInitialProduction(ctx context.Context, prepared *preparedInitialProduction) (*initialProductionObservation, error) {
	if r == nil || r.db == nil || prepared == nil {
		return nil, errInitialProductionEvidence
	}
	var result *initialProductionObservation
	err := r.readCommitted(ctx, func(tx *Repository) error {
		var err error
		result, err = tx.queryInitialProduction(ctx, prepared)
		return err
	})
	return result, err
}

// The same predicates serve the read-only diagnostic and the locked signer;
// caller owns transaction/qualification, not this query helper.
func (r *Repository) queryInitialProduction(ctx context.Context, prepared *preparedInitialProduction) (*initialProductionObservation, error) {
	if r == nil || r.db == nil || prepared == nil {
		return nil, errInitialProductionEvidence
	}
	request, err := initialProductionSources(prepared.production)
	if err != nil || !reflect.DeepEqual(request.Source, prepared.sources.Source) || request.TenantID != prepared.sources.TenantID ||
		!reflect.DeepEqual(request.Targets, prepared.sources.Targets) {
		return nil, errInitialProductionEvidence
	}
	paths, batch, err := prepared.sources.encode()
	if err != nil || len(prepared.engineVersions) == 0 {
		return nil, errInitialProductionEvidence
	}
	expectedEngines := map[int64]bool{int64(prepared.production.receipt.Target.EngineID): true}
	for _, path := range paths {
		expectedEngines[int64(path.EngineID)] = true
	}
	if len(expectedEngines) != len(prepared.engineVersions) {
		return nil, errInitialProductionEvidence
	}
	for id := range expectedEngines {
		if prepared.engineVersions[id] <= 0 {
			return nil, errInitialProductionEvidence
		}
	}
	var result *initialProductionObservation
	err = func(tx *Repository) error {
		if err := tx.checkProductionEngines(ctx, request.TenantID, prepared.engineVersions); err != nil {
			return err
		}
		rules, err := tx.queryCurrentSourceRules(ctx, prepared.sources, paths, batch)
		if err != nil {
			return err
		}
		permissions, err := tx.identity().ListEffectiveRoleAssignmentPermissions(ctx, request.Source.PrincipalID,
			iam.PrincipalTypeUser, iam.ContextTypeTenant, &request.TenantID, &request.Source.MembershipID, rules.ObservedAt)
		if err != nil {
			return err
		}
		path, err := authorization.EncodeSharingTarget(prepared.production.receipt.Target)
		if err != nil {
			return err
		}
		var history bool
		// Any historical rule/approval blocks inferring a fresh logical target.
		// Revocations and releases never erase their parent facts.
		if err := tx.db.WithContext(ctx).Raw(productionTargetHistorySQL,
			request.TenantID, int64(prepared.production.receipt.Target.EngineID), prepared.production.binding.ItemFingerprint, string(path)).Scan(&history).Error; err != nil {
			return err
		}
		result = &initialProductionObservation{sourceRules: rules, targetHasHistory: history,
			executionIAM: hasCurrentTenantPermission(permissions, request.TenantID, "system.execution_authorization.create", rules.ObservedAt) &&
				hasCurrentTenantPermission(permissions, request.TenantID, iam.TaskAuthorizationExecutePermission("transfer", "sync"), rules.ObservedAt)}
		return nil
	}(r)
	if err != nil {
		return nil, err
	}
	return result, nil
}

const productionTargetHistorySQL = `WITH target AS (
 SELECT ?::bigint AS tenant_id, ?::bigint AS engine_id, ?::text AS fingerprint, ?::jsonb AS path
) SELECT EXISTS (
 SELECT 1 FROM target t JOIN system.engine_access_direct_permissions r
 ON r.tenant_id=t.tenant_id AND r.engine_id=t.engine_id AND (r.item_fingerprint=t.fingerprint OR r.catalog_path=t.path)
 UNION ALL SELECT 1 FROM target t JOIN system.engine_access_fulfillment_outcomes o
 ON o.tenant_id=t.tenant_id AND o.engine_id=t.engine_id AND (o.binding->>'item_fingerprint'=t.fingerprint OR o.catalog_path=t.path)
 UNION ALL SELECT 1 FROM target t JOIN system.engine_access_grants g
 ON g.tenant_id=t.tenant_id AND g.engine_id=t.engine_id AND (g.item_fingerprint=t.fingerprint OR g.catalog_path=t.path)
 UNION ALL SELECT 1 FROM target t JOIN system.engine_access_denies d
 ON d.tenant_id=t.tenant_id AND d.engine_id=t.engine_id AND (d.item_fingerprint=t.fingerprint OR d.catalog_path=t.path))`
