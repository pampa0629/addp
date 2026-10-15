package engineaccess

import (
	"context"
	"reflect"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
	"gorm.io/gorm"
)

// PrepareExecutionSourceRead runs only after IAM qualifies the actual source.
// Remote owner facts are read without IAM locks. The returned verifier is bound
// to that exact identity and immutable execution scope, and performs only SQL.
func (s *Service) PrepareExecutionSourceRead(ctx context.Context, auth *iam.ExecutionAuthorization) (iam.ExecutionSourceReadVerifier, error) {
	if auth == nil || auth.Audience != "manager" || auth.SourceType != "user" || auth.SourceReadScope == nil {
		return nil, commonapi.ErrForbidden
	}
	request := sourceReadRequest{TenantID: auth.TenantID, Source: userProvenance{PrincipalID: auth.ActorPrincipalID,
		MembershipID: auth.TenantMembershipID, AuthorizationVersion: auth.IssuedAuthorizationVersion}, Targets: auth.SourceReadScope.ReadSet.Paths}
	scope := auth.SourceReadScope.Clone()
	paths, _, err := encodeSourceReadTargets(request.Targets)
	if err != nil || len(paths) == 0 || len(paths) != len(request.Targets) || len(paths) > 200 {
		return nil, commonapi.ErrForbidden
	}
	request.Bindings, err = s.resolveReadBindings(ctx, auth.TenantID, paths)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, db *gorm.DB, current *iam.ExecutionAuthorization) (time.Time, error) {
		if db == nil || current == nil || current.Audience != "manager" || current.SourceType != "user" ||
			current.TenantID != request.TenantID || current.ActorPrincipalID != request.Source.PrincipalID ||
			current.TenantMembershipID != request.Source.MembershipID || current.IssuedAuthorizationVersion != request.Source.AuthorizationVersion ||
			!reflect.DeepEqual(current.SourceReadScope, scope) {
			return time.Time{}, commonapi.ErrForbidden
		}
		return verifyExecutionSourceRead(ctx, db, request)
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
