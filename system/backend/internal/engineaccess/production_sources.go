package engineaccess

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	productionfacts "github.com/addp/common/production"
	"github.com/addp/system/internal/iam"
)

var errInitialProductionEvidence = errors.Join(commonapi.ErrConflict, errors.New("initial production evidence is incomplete or ineligible"))

// All fields are private owner evidence, never a credential or a grant command.
// Preparation does not prove that creation and the successful write belonged to
// the same production. The committed logical-result fact, not survival of the
// original physical table until issuance, is the eligibility basis.
type preparedInitialProduction struct {
	production     *preparedProduction
	sources        sourceReadRequest
	engineVersions map[int64]int64
	owner          productionfacts.Owner
}

func initialProductionSources(prepared *preparedProduction) (sourceReadRequest, error) {
	if prepared == nil || prepared.receipt.Validate() != nil || prepared.binding.Validate() != nil || prepared.engineVersion <= 0 {
		return sourceReadRequest{}, errInitialProductionEvidence
	}
	r := prepared.receipt
	if r.Origin == nil || r.ProveNewBoundary(prepared.binding) != nil {
		return sourceReadRequest{}, errInitialProductionEvidence
	}
	request := sourceReadRequest{Action: "read", TenantID: r.TenantID, Source: userProvenance{r.Origin.PrincipalID, r.Origin.MembershipID, r.Origin.AuthorizationVersion}}
	switch r.Sources.Kind {
	case productionfacts.SourcesNone:
		return request, nil
	case productionfacts.SourcesPrivateInput:
		if r.Sources.PrivateUpload.UploaderID != r.Origin.PrincipalID {
			return sourceReadRequest{}, errInitialProductionEvidence
		}
		return request, nil
	case productionfacts.SourcesDataItems:
	default:
		return sourceReadRequest{}, errInitialProductionEvidence
	}
	paths := r.Sources.ReadSet.Clone().Paths
	for _, path := range paths {
		if reflect.DeepEqual(path, r.Primary) {
			return sourceReadRequest{}, errInitialProductionEvidence
		}
	}
	request.Targets = paths
	return request, nil
}

// Source facts use the same System-owned clients as target preparation. Cross-
// owner IO happens without any IAM or target transaction. Unsupported engines
// are not silently interpreted as unrestricted sources.
func (s *Service) prepareInitialProduction(ctx context.Context, tenantID int64, ref productionfacts.Reference) (*preparedInitialProduction, error) {
	production, err := s.prepareProduction(ctx, tenantID, ref)
	if err != nil {
		return nil, err
	}
	sources, err := initialProductionSources(production)
	if err != nil {
		return nil, err
	}
	versions := map[int64]int64{int64(production.receipt.Primary.EngineID): production.engineVersion}
	for _, frozen := range production.receipt.Sources.EngineVersions {
		id := frozen.EngineID
		if prior, exists := versions[id]; exists && prior != frozen.Version {
			return nil, errProductionTargetChanged
		}
		engine, err := s.repository.productionEngine(ctx, tenantID, id)
		if err != nil {
			return nil, err
		}
		if engine.Version != frozen.Version {
			return nil, errProductionTargetChanged
		}
		versions[id] = frozen.Version
	}
	owner, _ := s.productionRegistry.Resolve(ref.Owner, "addp-"+ref.Owner)
	prepared := &preparedInitialProduction{production: production, sources: sources, engineVersions: versions, owner: owner}
	if production.receipt.Sources.Kind != productionfacts.SourcesDataItems {
		return prepared, nil
	}
	bindings, readErr := s.resolveReadBindings(ctx, tenantID, sources.Targets)
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

	expectedEngines := map[int64]bool{int64(prepared.production.receipt.Primary.EngineID): true}
	for _, path := range request.Targets {
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
		rules, err := tx.queryProductionSourceRules(ctx, prepared)
		if err != nil {
			return err
		}
		permissions, err := tx.identity().ListEffectiveRoleAssignmentPermissions(ctx, request.Source.PrincipalID,
			iam.PrincipalTypeUser, iam.ContextTypeTenant, &request.TenantID, &request.Source.MembershipID, rules.ObservedAt)
		if err != nil {
			return err
		}
		path, err := authorization.EncodeSharingTarget(prepared.production.receipt.Primary)
		if err != nil {
			return err
		}
		var history bool
		// Any historical rule/approval blocks inferring a fresh logical target.
		// Revocations and releases never erase their parent facts.
		if err := tx.db.WithContext(ctx).Raw(productionTargetHistorySQL,
			request.TenantID, int64(prepared.production.receipt.Primary.EngineID), prepared.production.binding.ItemFingerprint, string(path)).Scan(&history).Error; err != nil {
			return err
		}
		result = &initialProductionObservation{sourceRules: rules, targetHasHistory: history,
			executionIAM: hasCurrentTenantPermission(permissions, request.TenantID, "system.execution_authorization.create", rules.ObservedAt) &&
				hasCurrentTenantPermission(permissions, request.TenantID, prepared.owner.ExecutionPermission, rules.ObservedAt)}
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
 SELECT 1 FROM target t JOIN system.engine_access_fulfillment_outcomes o
 ON o.tenant_id=t.tenant_id AND o.engine_id=t.engine_id AND (o.binding->>'item_fingerprint'=t.fingerprint OR o.catalog_path=t.path)
 UNION ALL SELECT 1 FROM target t JOIN system.engine_access_grants g
 ON g.tenant_id=t.tenant_id AND g.engine_id=t.engine_id AND (g.item_fingerprint=t.fingerprint OR g.catalog_path=t.path)
)`

// The private upload is a proven input outside the DataItem Grant store.
// It still requires the same live human identity at issuance and commit.
func (r *Repository) queryProductionSourceRules(ctx context.Context, prepared *preparedInitialProduction) (*sourceReadRules, error) {
	request := prepared.sources
	if prepared.production.receipt.Sources.Kind == productionfacts.SourcesDataItems {
		paths, batch, err := request.encode()
		if err != nil {
			return nil, errInitialProductionEvidence
		}
		return r.queryCurrentSourceRules(ctx, request, paths, batch)
	}
	var subject struct {
		ObservedAt time.Time
		Covered    bool
	}
	err := r.db.WithContext(ctx).Raw(privateImportSourceSQL, request.TenantID, request.Source.PrincipalID,
		request.Source.MembershipID, request.Source.AuthorizationVersion).Scan(&subject).Error
	return &sourceReadRules{ObservedAt: subject.ObservedAt, Covered: subject.Covered}, err
}

const privateImportSourceSQL = `WITH input AS MATERIALIZED (
 SELECT ?::bigint AS tenant_id, ?::bigint AS principal_id,
        ?::bigint AS membership_id, ?::bigint AS authorization_version,
        clock_timestamp() AS observed_at
)` + sourceReadSubjectCTEs + ` SELECT input.observed_at, EXISTS (SELECT 1 FROM current_source) AS covered FROM input`
