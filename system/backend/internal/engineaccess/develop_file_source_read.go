package engineaccess

import (
	"context"
	"reflect"
	"slices"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PrepareDevelopFileRead never promotes metadata_target to content access.
// Meta must independently prove the complete single-file GeoPackage boundary.
func (s *Service) PrepareDevelopFileRead(ctx context.Context, auth *iam.ExecutionAuthorization, intent execution.DevelopFileIntent) (*iam.DevelopFileReadPreparation, error) {
	if s == nil || s.repository == nil || auth == nil || auth.Audience != execution.AudienceDevelop || auth.SourceType != "user" || auth.TenantID <= 0 || intent.Validate() != nil {
		return nil, commonapi.ErrForbidden
	}
	intent = *intent.Clone()
	paths := make([]plugin.EngineCatalogPath, len(intent.Sources))
	for i, source := range intent.Sources {
		paths[i] = source.Target
	}
	set, err := plugin.NewQueryReadSet(paths...)
	if err != nil {
		return nil, commonapi.ErrForbidden
	}
	bindings, err := s.resolveReadBindings(ctx, auth.TenantID, set.Paths)
	if err != nil {
		return nil, err
	}
	refs := make([]execution.DevelopFileSourceReference, len(bindings))
	versions := make(map[uint]int64)
	for i, binding := range bindings {
		if !developSingleGeoPackage(binding, set.Paths[i]) {
			return nil, ErrAuthorizationBoundaryChanged
		}
		engine, err := s.repository.engine(ctx, auth.TenantID, int64(set.Paths[i].EngineID), false)
		if err != nil || !developNFSVersion(engine, auth.TenantID, 0) {
			return nil, commonapi.ErrForbidden
		}
		if previous, ok := versions[engine.ID]; ok && previous != engine.Version {
			return nil, ErrAuthorizationBoundaryChanged
		}
		versions[engine.ID] = engine.Version
		refs[i] = execution.DevelopFileSourceReference{Target: set.Paths[i], ItemFingerprint: binding.ItemFingerprint, BoundaryHash: binding.BoundaryHash, EngineVersion: strconv.FormatInt(engine.Version, 10)}
	}
	targetVersion := ""
	if intent.Target != nil {
		engine, err := s.repository.engine(ctx, auth.TenantID, int64(intent.Target.Target.EngineID), false)
		if err != nil || !developNFSVersion(engine, auth.TenantID, 0) {
			return nil, commonapi.ErrForbidden
		}
		if previous, ok := versions[engine.ID]; ok && previous != engine.Version {
			return nil, ErrAuthorizationBoundaryChanged
		}
		versions[engine.ID] = engine.Version
		targetVersion = strconv.FormatInt(engine.Version, 10)
	}
	scope, err := execution.NewDevelopBoundFileScope(intent, refs, targetVersion)
	if err != nil || (auth.DevelopFileScope != nil && !reflect.DeepEqual(auth.DevelopFileScope, scope)) {
		return nil, ErrAuthorizationBoundaryChanged
	}
	identity := *auth
	identity.DevelopFileScope = scope.Clone()
	request := sourceReadRequest{Action: "read", TenantID: auth.TenantID, Source: userProvenance{PrincipalID: auth.ActorPrincipalID, MembershipID: auth.TenantMembershipID, AuthorizationVersion: auth.IssuedAuthorizationVersion}, Targets: set.Paths, Bindings: bindings}
	verify := func(ctx context.Context, db *gorm.DB, current *iam.ExecutionAuthorization, selected *plugin.EngineCatalogPath) (*iam.DevelopFileReadObservation, error) {
		if db == nil || current == nil || current.Audience != identity.Audience || current.SourceType != identity.SourceType || current.TenantID != identity.TenantID || current.ActorPrincipalID != identity.ActorPrincipalID || current.TenantMembershipID != identity.TenantMembershipID || current.IssuedAuthorizationVersion != identity.IssuedAuthorizationVersion || !reflect.DeepEqual(current.DevelopFileScope, identity.DevelopFileScope) {
			return nil, commonapi.ErrForbidden
		}
		var selectedEngine *models.Engine
		if selected != nil {
			found := false
			for _, p := range set.Paths {
				if reflect.DeepEqual(p, *selected) {
					found = true
					break
				}
			}
			if !found || s.executionEngineSnapshot == nil {
				return nil, commonapi.ErrForbidden
			}
		}
		ids := make([]uint, 0, len(versions))
		for id := range versions {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			var engine models.Engine
			if err := db.WithContext(ctx).Table("system.engines").Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND tenant_id=?", id, identity.TenantID).Take(&engine).Error; err != nil || !developNFSVersion(&engine, identity.TenantID, versions[id]) {
				return nil, commonapi.ErrForbidden
			}
			if selected != nil && id == selected.EngineID {
				selectedEngine = &engine
			}
		}
		observation := &iam.DevelopFileReadObservation{}
		// Reuse the existing bounded SQL batch size without imposing a new
		// workflow source-count policy. Every batch uses this same IAM transaction.
		for start := 0; start < len(request.Targets); start += 200 {
			end := min(start+200, len(request.Targets))
			part := request
			part.Targets = request.Targets[start:end]
			part.Bindings = request.Bindings[start:end]
			paths, batch, err := part.encode()
			if err != nil {
				return nil, err
			}
			result, err := NewRepository(db).queryCurrentSourceRules(ctx, part, paths, batch)
			if err != nil {
				return nil, err
			}
			if result == nil || !result.Covered || result.ObservedAt.IsZero() {
				return nil, commonapi.ErrForbidden
			}
			if result.ObservedAt.After(observation.ObservedAt) {
				observation.ObservedAt = result.ObservedAt
			}
			observation.ExpiresAt = earliestDevelopReadExpiry(observation.ExpiresAt, result.ExpiresAt)
		}
		if observation.ExpiresAt != nil && !observation.ExpiresAt.After(observation.ObservedAt) {
			return nil, commonapi.ErrForbidden
		}
		if selectedEngine != nil {
			decoded, err := s.executionEngineSnapshot(selectedEngine)
			if err != nil || !developNFSVersion(decoded, identity.TenantID, selectedEngine.Version) || decoded.ID != selectedEngine.ID || len(decoded.ConnectionInfo) == 0 {
				return nil, commonapi.ErrForbidden
			}
			observation.Engine = decoded
		}
		return observation, nil
	}
	prepared := &iam.DevelopFileReadPreparation{Scope: *scope.Clone(), Verify: verify}
	if scope.Target != nil {
		target := *scope.Target
		prepared.TargetSnapshot = func(ctx context.Context, db *gorm.DB) (*models.Engine, error) {
			return s.DevelopFileTargetEngine(ctx, db, identity.TenantID, target)
		}
	}
	return prepared, nil
}

func developSingleGeoPackage(binding authorizationfacts.Binding, target plugin.EngineCatalogPath) bool {
	if binding.Validate() != nil || binding.MetadataTarget != nil || binding.Scope != nil || binding.FileComposition == nil {
		return false
	}
	c := binding.FileComposition
	return c.Format == "geopackage" && c.Layout() == "single" && len(c.Refs) == 1 && c.Refs[0].Role == "container" && c.Refs[0].Required && c.Refs[0].Primary && reflect.DeepEqual(c.Refs[0].Target, target)
}

func developNFSVersion(engine *models.Engine, tenant, version int64) bool {
	return engine != nil && engine.ID > 0 && engine.TenantID != nil && int64(*engine.TenantID) == tenant && engine.EngineType == "nfs" && engine.LifecycleState == "active" && engine.DeletedAt == nil && engine.Version > 0 && (version == 0 || engine.Version == version)
}

// PrepareDevelopFileCreateApprover reuses the current engine-management check
// under the IAM approval transaction. It does not retrieve connection data.
func (s *Service) PrepareDevelopFileCreateApprover(ctx context.Context, db *gorm.DB, tenant, membership int64, target execution.DevelopBoundFileTarget) (func(time.Time) (*time.Time, error), error) {
	version, err := strconv.ParseInt(target.EngineVersion, 10, 64)
	if err != nil || version <= 0 || target.Mode != "fail" {
		return nil, commonapi.ErrForbidden
	}
	scope, err := NewRepository(db).lockManagementScope(ctx, tenant, int64(target.Target.EngineID), membership)
	if err != nil {
		return nil, err
	}
	if !developNFSVersion(scope.engine, tenant, version) {
		return nil, commonapi.ErrForbidden
	}
	return func(now time.Time) (*time.Time, error) {
		if err := scope.check(now); err != nil {
			return nil, err
		}
		if !scope.isAdministrator(now) {
			return &scope.delegation.ExpiresAt, nil
		}
		var latest time.Time
		for _, row := range scope.administratorPermissions {
			if !hasCurrentTenantPermission([]iam.RoleAssignmentPermissionProjection{row}, tenant, authorization.PermissionSystemEngineAccessDelegationCreate, now) {
				continue
			}
			if row.ValidUntil == nil {
				return nil, nil
			}
			if row.ValidUntil.After(latest) {
				latest = *row.ValidUntil
			}
		}
		return &latest, nil
	}, nil
}

func earliestDevelopReadExpiry(a, b *time.Time) *time.Time {
	if b == nil {
		return a
	}
	if a != nil && !b.Before(*a) {
		return a
	}
	copy := b.UTC()
	return &copy
}

// DevelopFileTargetEngine reads only the originally version-bound target.
func (s *Service) DevelopFileTargetEngine(ctx context.Context, db *gorm.DB, tenant int64, target execution.DevelopBoundFileTarget) (*models.Engine, error) {
	var engine models.Engine
	version, err := strconv.ParseInt(target.EngineVersion, 10, 64)
	if err != nil || s.executionEngineSnapshot == nil {
		return nil, commonapi.ErrForbidden
	}
	if err := db.WithContext(ctx).Table("system.engines").Where("id=? AND tenant_id=?", target.Target.EngineID, tenant).
		Clauses(clause.Locking{Strength: "SHARE"}).Take(&engine).Error; err != nil {
		return nil, err
	}
	if !developNFSVersion(&engine, tenant, version) {
		return nil, commonapi.ErrForbidden
	}
	decoded, err := s.executionEngineSnapshot(&engine)
	if err != nil || !developNFSVersion(decoded, tenant, version) || decoded.ID != engine.ID || len(decoded.ConnectionInfo) == 0 {
		return nil, commonapi.ErrForbidden
	}
	return decoded, nil
}
