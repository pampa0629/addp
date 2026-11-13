package engineaccess

import (
	"context"
	"strings"

	commonapi "github.com/addp/common/api"
	auth "github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/iam"
)

// CheckManagerPGRead consumes evidence from the authenticated, narrowly
// qualified Manager Runtime. It deliberately has no Meta reader dependency.
func (s *Service) CheckManagerPGRead(ctx context.Context, tenantID int64, request client.ManagerPGReadCheckRequest, preview bool) (*SourceReadCheck, error) {
	if tenantID <= 0 || len(request.Sources) == 0 {
		return nil, commonapi.ErrBadRequest
	}
	if !strings.HasPrefix(request.UserCredential, "addp_at_") && !strings.HasPrefix(request.UserCredential, "addp_dat_") {
		return nil, commonapi.ErrUnauthorized
	}
	if request.Validate() != nil {
		return nil, commonapi.ErrBadRequest
	}
	if s == nil || s.repository == nil || s.repository.db == nil {
		return nil, errSourceReadRules
	}
	paths := make([]plugin.EngineCatalogPath, len(request.Sources))
	for i, binding := range request.Sources {
		paths[i] = binding.NativeTarget
	}
	qualify := qualifyManagerProfileResultRead
	if preview {
		qualify = qualifyManagerPreviewRead
	}
	resolve := func(service *iam.AuthContextService) (*auth.AuthContext, error) {
		if preview && strings.HasPrefix(request.UserCredential, "addp_dat_") {
			return service.ResolveDelegatedAccessToken(ctx, request.UserCredential)
		}
		return service.ResolveUserAccessToken(ctx, request.UserCredential)
	}
	result, err := s.repository.observeSourceRules(ctx, request.UserCredential, auth.SourceAccessRead, paths, request.Sources, tenantID, resolve, qualify, &sourceReadEngineVersion{ID: paths[0].EngineID, PG: true})
	if err != nil {
		return nil, err
	}
	if result == nil || !result.Covered {
		return nil, commonapi.ErrForbidden
	}
	return &SourceReadCheck{ObservedAt: result.ObservedAt}, nil
}
