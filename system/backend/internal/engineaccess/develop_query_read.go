package engineaccess

import (
	"context"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/system/internal/iam"
	"gorm.io/gorm"
)

// CheckDevelopQueryRead accepts facts only from the fixed Develop machine
// route. System resolves Meta itself, outside the final IAM/rule transaction.
func (s *Service) CheckDevelopQueryRead(ctx context.Context, tenantID int64, request client.DevelopQueryReadRequest) (*client.DevelopQueryReadObservation, error) {
	service, err := iam.NewExecutionAuthorizationService(iam.NewRepository(s.repository.db))
	if err != nil {
		return nil, err
	}
	return service.ObserveDevelopQueryRead(ctx, tenantID, request, func(ctx context.Context, original *iam.ExecutionAuthorization) (iam.DevelopQueryReadVerifier, error) {
		read := sourceReadRequest{Action: "read", TenantID: tenantID,
			Source:  userProvenance{PrincipalID: original.ActorPrincipalID, MembershipID: original.TenantMembershipID, AuthorizationVersion: original.IssuedAuthorizationVersion},
			Targets: request.ReadSet.Clone().Paths}
		if len(read.Targets) > 0 {
			var err error
			read.Bindings, err = s.resolveReadBindings(ctx, tenantID, read.Targets)
			if err != nil {
				return nil, err
			}
		}
		return func(ctx context.Context, db *gorm.DB, current *iam.ExecutionAuthorization) (*client.DevelopQueryReadObservation, error) {
			if current.ActorPrincipalID != read.Source.PrincipalID || current.TenantMembershipID != read.Source.MembershipID || current.IssuedAuthorizationVersion != read.Source.AuthorizationVersion || current.TenantID != read.TenantID {
				return nil, commonapi.ErrForbidden
			}
			repository := NewRepository(db)
			if len(read.Targets) == 0 {
				// Only a proven empty set can reach this branch; a nil/unknown set
				// was rejected at the fixed operation's boundary.
				now, err := repository.wallClock(ctx)
				return &client.DevelopQueryReadObservation{ObservedAt: now}, err
			}
			paths, batch, err := read.encode()
			if err != nil {
				return nil, err
			}
			observation, err := repository.queryCurrentSourceRules(ctx, read, paths, batch)
			if err != nil {
				return nil, err
			}
			if observation == nil || !observation.Covered {
				return nil, commonapi.ErrForbidden
			}
			result := &client.DevelopQueryReadObservation{ObservedAt: observation.ObservedAt}
			if observation.ExpiresAt != nil {
				result.ExpiresAt = *observation.ExpiresAt
			}
			return result, nil
		}, nil
	})
}
