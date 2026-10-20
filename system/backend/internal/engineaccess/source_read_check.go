package engineaccess

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	"github.com/addp/common/client"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/iam"
)

// Reference to Manager's existing Permission, not a new System Permission.
const ManagerPreviewReadPermission = "manager.data_item.read"

type SourceReadCheckRequest struct {
	Targets        []engineplugin.EngineCatalogPath     `json:"targets"`
	ExpectedSource *client.ManagerModel3DExpectedSource `json:"expected_source,omitempty"`
}

// A point-in-time source check. NOT a reusable Allow, lease or execution token.
type SourceReadCheck struct {
	ObservedAt time.Time `json:"observed_at"`
}

func qualifyManagerPreviewRead(current commonauth.AuthContext) error {
	if current.Principal.Type != "user" || current.Context.Type != "tenant" ||
		!commonauth.HasContextPermissions(current, ManagerPreviewReadPermission) ||
		len(current.Client.Audiences) != 1 {
		return commonapi.ErrForbidden
	}
	if current.Token.Type == "delegated_access_token" {
		if current.Client.Audiences[0] == "manager" && current.Client.ScopeMode == "restricted" &&
			len(current.Client.Scopes) == 1 && current.Client.Scopes[0] == "data.preview" &&
			current.Delegation != nil && current.Client.ClientID != nil &&
			current.Delegation.DelegatedByClientID == *current.Client.ClientID &&
			current.Delegation.AgentRunID != "" && current.Delegation.ToolCallID != "" {
			return nil
		}
		return commonapi.ErrForbidden
	}
	if current.Delegation != nil || current.Client.Audiences[0] != "addp.api" {
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

// CheckManagerPreviewRead consumes real User or fixed Manager Tool Bearer. Function/client
// conditions and complete source rules share one read-only committed snapshot.
// Security and the actual immutable query remain the Manager owner's gates.
func (s *Service) CheckManagerPreviewRead(ctx context.Context, credential string, request SourceReadCheckRequest) (*SourceReadCheck, error) {
	return s.checkSourceRead(ctx, credential, request, true)
}

// Result content uses the current ordinary User, never a preview Tool delegate
// or a historical execution identity. It does not authorize background sampling.
func (s *Service) CheckManagerProfileResultRead(ctx context.Context, credential string, request SourceReadCheckRequest) (*SourceReadCheck, error) {
	return s.checkSourceRead(ctx, credential, request, false)
}

func qualifyManagerProfileResultRead(current commonauth.AuthContext) error {
	if current.Token.Type != "first_party_access_token" && current.Token.Type != "oauth_access_token" {
		return commonapi.ErrForbidden
	}
	return qualifyManagerPreviewRead(current)
}

func (s *Service) checkSourceRead(ctx context.Context, credential string, request SourceReadCheckRequest, preview bool) (*SourceReadCheck, error) {
	if len(request.Targets) == 0 || len(request.Targets) > 200 {
		return nil, commonapi.ErrBadRequest
	}
	var expectedVersion *sourceReadEngineVersion
	if request.ExpectedSource != nil {
		if !preview || len(request.Targets) != 1 || request.ExpectedSource.Validate() != nil {
			return nil, commonapi.ErrBadRequest
		}
		copy := *request.ExpectedSource
		copy.FileComposition = *copy.FileComposition.Clone()
		request.ExpectedSource = &copy
		primary, _ := copy.FileComposition.PrimaryTarget()
		if !reflect.DeepEqual(primary, request.Targets[0]) {
			return nil, commonapi.ErrBadRequest
		}
		version, _ := strconv.ParseInt(copy.EngineVersion, 10, 64)
		expectedVersion = &sourceReadEngineVersion{ID: primary.EngineID, Version: version}
	}
	paths, _, err := encodeSourceReadTargets(request.Targets)
	if err != nil {
		return nil, commonapi.ErrBadRequest
	}
	if s == nil || s.repository == nil || s.repository.db == nil {
		return nil, errSourceReadRules
	}
	resolve := func(auth *iam.AuthContextService) (*commonauth.AuthContext, error) {
		if preview && request.ExpectedSource == nil && strings.HasPrefix(credential, "addp_dat_") {
			return auth.ResolveDelegatedAccessToken(ctx, credential)
		}
		return auth.ResolveUserAccessToken(ctx, credential)
	}
	qualify := qualifyManagerProfileResultRead
	if preview && request.ExpectedSource == nil {
		qualify = qualifyManagerPreviewRead
	}
	var tenantID int64
	// Release the initial credential snapshot before Meta IO. The final
	// snapshot re-resolves the same credential and current source rules.
	err = s.repository.readCommitted(ctx, func(tx *Repository) error {
		auth, err := iam.NewAuthContextService(tx.identity())
		if err != nil {
			return err
		}
		current, err := resolve(auth)
		if err != nil {
			return err
		}
		if current == nil || current.Context.TenantID == nil {
			return commonapi.ErrForbidden
		}
		if err := qualify(*current); err != nil {
			return err
		}
		tenantID, err = strconv.ParseInt(*current.Context.TenantID, 10, 64)
		if err != nil || tenantID <= 0 || strconv.FormatInt(tenantID, 10) != *current.Context.TenantID {
			return commonapi.ErrForbidden
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	bindings, err := s.resolveReadBindings(ctx, tenantID, paths)
	if err != nil {
		return nil, err
	}
	if request.ExpectedSource != nil && (len(bindings) != 1 || !reflect.DeepEqual(bindings[0].FileComposition, &request.ExpectedSource.FileComposition)) {
		return nil, commonapi.ErrForbidden
	}
	result, err := s.repository.observeSourceRules(ctx, credential, paths, bindings, tenantID, resolve, qualify, expectedVersion)
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
