package engineaccess

import (
	"context"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
	"gorm.io/gorm"
)

// VerifyExecutionSourceRead is a System-internal composition dependency. IAM
// has already established and locked the actual User source and owner claim;
// this function cannot be invoked by an HTTP caller with arbitrary actor IDs.
// Use the IAM transaction, not a second pool connection while its locks are held.
func VerifyExecutionSourceRead(ctx context.Context, db *gorm.DB, auth *iam.ExecutionAuthorization) (time.Time, error) {
	if db == nil || auth == nil || auth.Audience != "manager" || auth.SourceType != "user" || auth.SourceReadScope == nil {
		return time.Time{}, commonapi.ErrForbidden
	}
	request := sourceReadRequest{TenantID: auth.TenantID, Source: userProvenance{PrincipalID: auth.ActorPrincipalID,
		MembershipID: auth.TenantMembershipID, AuthorizationVersion: auth.IssuedAuthorizationVersion}, Targets: auth.SourceReadScope.ReadSet.Paths}
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
