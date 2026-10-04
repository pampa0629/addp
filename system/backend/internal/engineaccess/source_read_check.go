package engineaccess

import (
	"context"
	"time"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
)

// Reference to Manager's existing Permission, not a new System Permission.
const ManagerPreviewReadPermission = "manager.data_item.read"

type ManagerPreviewReadCheckRequest struct {
	Targets []engineplugin.EngineCatalogPath `json:"targets"`
}

// A point-in-time source check. NOT a reusable Allow, lease or execution token.
type SourceReadCheck struct {
	ObservedAt time.Time `json:"observed_at"`
}

func qualifyManagerPreviewRead(current commonauth.AuthContext) error {
	if current.Principal.Type != "user" || current.Context.Type != "tenant" || current.Delegation != nil ||
		!commonauth.HasContextPermissions(current, ManagerPreviewReadPermission) ||
		len(current.Client.Audiences) != 1 || current.Client.Audiences[0] != "addp.api" {
		return commonapi.ErrForbidden
	}
	switch current.Token.Type {
	case "first_party_access_token":
		if current.Client.ScopeMode == "unrestricted" && len(current.Client.Scopes) == 0 {
			return nil
		}
	case "oauth_access_token":
		if current.Client.ScopeMode == "restricted" {
			for _, scope := range current.Client.Scopes {
				if scope == "addp.api" {
					return nil
				}
			}
		}
	}
	return commonapi.ErrForbidden
}

// CheckManagerPreviewRead consumes only a real User Bearer. Function/client
// conditions and complete source rules share one read-only committed snapshot.
// Security and the actual immutable query remain the Manager owner's gates.
func (s *Service) CheckManagerPreviewRead(ctx context.Context, credential string, request ManagerPreviewReadCheckRequest) (*SourceReadCheck, error) {
	if len(request.Targets) == 0 || len(request.Targets) > 200 {
		return nil, commonapi.ErrBadRequest
	}
	if _, _, err := encodeSourceReadTargets(request.Targets); err != nil {
		return nil, commonapi.ErrBadRequest
	}
	if s == nil || s.repository == nil {
		return nil, errSourceReadRules
	}
	result, err := s.repository.observeCurrentUserSourceRules(ctx, credential, request.Targets, qualifyManagerPreviewRead)
	if err != nil {
		return nil, err
	}
	if result == nil || result.ObservedAt.IsZero() {
		return nil, errSourceReadRules
	}
	if !result.Covered {
		return nil, commonapi.ErrForbidden
	}
	return &SourceReadCheck{ObservedAt: result.ObservedAt}, nil
}
