package engineaccess

import (
	"context"
	"reflect"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	commonexecution "github.com/addp/common/execution"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PrepareExecutionSourceRead runs only after IAM qualifies the actual source.
// Remote owner facts are read without IAM locks. The returned verifier is bound
// to that exact identity and immutable execution scope, and performs only SQL.
func (s *Service) PrepareExecutionSourceRead(ctx context.Context, auth *iam.ExecutionAuthorization) (iam.ExecutionSourceReadVerifier, error) {
	if auth == nil || auth.Audience != "manager" || auth.SourceType != "user" || auth.SourceReadScope == nil {
		return nil, commonapi.ErrForbidden
	}
	targets, err := auth.SourceReadScope.Targets()
	if err != nil {
		return nil, commonapi.ErrForbidden
	}
	request := sourceReadRequest{TenantID: auth.TenantID, Source: userProvenance{PrincipalID: auth.ActorPrincipalID,
		MembershipID: auth.TenantMembershipID, AuthorizationVersion: auth.IssuedAuthorizationVersion}, Targets: targets}
	scope := auth.SourceReadScope.Clone()
	paths, _, err := encodeSourceReadTargets(request.Targets)
	if err != nil || len(paths) == 0 || len(paths) != len(request.Targets) || len(paths) > 200 {
		return nil, commonapi.ErrForbidden
	}
	request.Bindings, err = s.resolveReadBindings(ctx, auth.TenantID, paths)
	if err != nil {
		return nil, err
	}
	modelScope := scope.Model3D()
	if modelScope != nil && (s.executionEngineSnapshot == nil || len(request.Bindings) != 1 || !reflect.DeepEqual(request.Bindings[0].FileComposition, &modelScope.FileComposition)) {
		return nil, commonapi.ErrForbidden
	}
	return func(ctx context.Context, db *gorm.DB, current *iam.ExecutionAuthorization) (*iam.ExecutionSourceReadObservation, error) {
		if db == nil || current == nil || current.Audience != "manager" || current.SourceType != "user" ||
			current.TenantID != request.TenantID || current.ActorPrincipalID != request.Source.PrincipalID ||
			current.TenantMembershipID != request.Source.MembershipID || current.IssuedAuthorizationVersion != request.Source.AuthorizationVersion ||
			!reflect.DeepEqual(current.SourceReadScope, scope) {
			return nil, commonapi.ErrForbidden
		}
		var stored *models.Engine
		if modelScope != nil {
			primary, _ := modelScope.FileComposition.PrimaryTarget()
			version, _ := strconv.ParseInt(modelScope.EngineVersion, 10, 64)
			var engine models.Engine
			if err := db.WithContext(ctx).Table("system.engines").Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND tenant_id=? AND version=? AND engine_type IN ('ftp','nfs','minio') AND lifecycle_state='active' AND deleted_at IS NULL", primary.EngineID, request.TenantID, version).Take(&engine).Error; err != nil {
				return nil, commonapi.ErrForbidden
			}
			if !commonexecution.ManagerModel3DSourceComposition(engine.EngineType, &modelScope.FileComposition) {
				return nil, commonapi.ErrForbidden
			}
			stored = &engine
		}
		observedAt, err := verifyExecutionSourceRead(ctx, db, request)
		if err != nil {
			return nil, err
		}
		observation := &iam.ExecutionSourceReadObservation{ObservedAt: observedAt}
		if stored != nil {
			decoded, err := s.executionEngineSnapshot(stored)
			if err != nil || decoded == nil || decoded.ID != stored.ID || decoded.Version != stored.Version || decoded.EngineType != stored.EngineType || decoded.TenantID == nil || *decoded.TenantID != uint(request.TenantID) || decoded.DeletedAt != nil || decoded.LifecycleState != "active" || len(decoded.ConnectionInfo) == 0 {
				return nil, commonapi.ErrForbidden
			}
			observation.Engine = decoded
		}
		return observation, nil
	}, nil
}

func verifyExecutionSourceRead(ctx context.Context, db *gorm.DB, request sourceReadRequest) (time.Time, error) {
	paths, batch, err := request.encode()
	if err != nil || len(paths) == 0 || len(paths) != len(request.Targets) || len(paths) > 200 {
		return time.Time{}, commonapi.ErrForbidden
	}
	observation, err := NewRepository(db).queryCurrentSourceRules(ctx, request, paths, batch)
	if err != nil {
		return time.Time{}, err
	}
	if observation == nil || observation.ObservedAt.IsZero() || !observation.Covered {
		return time.Time{}, commonapi.ErrForbidden
	}
	return observation.ObservedAt, nil
}
