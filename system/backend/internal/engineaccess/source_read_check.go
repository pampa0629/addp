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
	"github.com/addp/system/internal/models"
)

// Reference to Manager's existing Permission, not a new System Permission.
const ManagerPreviewReadPermission = "manager.data_item.read"

type SourceReadCheckRequest struct {
	Targets        []engineplugin.EngineCatalogPath  `json:"targets"`
	ExpectedSource *client.ManagerFileExpectedSource `json:"expected_source,omitempty"`
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
	return qualifyOrdinarySourceUser(current, ManagerPreviewReadPermission)
}

func qualifyOrdinarySourceUser(current commonauth.AuthContext, permission string) error {
	if current.Principal.Type != "user" || current.Context.Type != "tenant" || !commonauth.HasContextPermissions(current, permission) || len(current.Client.Audiences) != 1 {
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

// ObserveCurrentSourceRead consumes real User or fixed Manager Tool Bearer. Function/client
// conditions and complete source rules share one read-only committed snapshot.
// Security and the actual immutable query remain the Manager owner's gates.
func (s *Service) ObserveCurrentSourceRead(ctx context.Context, credential string, request SourceReadCheckRequest) (*SourceReadCheck, error) {
	return s.checkSourceRead(ctx, credential, request, true, nil)
}

func qualifyManagerProfileResultRead(current commonauth.AuthContext) error {
	if current.Token.Type != "first_party_access_token" && current.Token.Type != "oauth_access_token" {
		return commonapi.ErrForbidden
	}
	return qualifyManagerPreviewRead(current)
}

func (s *Service) checkSourceRead(ctx context.Context, credential string, request SourceReadCheckRequest, preview bool, qualify func(commonauth.AuthContext) error) (*SourceReadCheck, error) {
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
	if qualify == nil {
		qualify = qualifyManagerProfileResultRead
		if preview && request.ExpectedSource == nil {
			qualify = qualifyManagerPreviewRead
		}
	}
	entry, ok := iam.AuthenticatedRequestIdentity(ctx, credential)
	if !ok {
		return nil, commonapi.ErrUnauthorized
	}
	// Preserve the credential kinds accepted by the selected IAM resolver.
	// A profile or expected-source check never consumes a preview delegate.
	if entry.Token.Type != "first_party_access_token" && entry.Token.Type != "oauth_access_token" &&
		!(preview && request.ExpectedSource == nil && entry.Token.Type == "delegated_access_token") {
		return nil, commonapi.ErrUnauthorized
	}
	if err := qualify(entry); err != nil {
		return nil, err
	}
	if entry.Context.Type != "tenant" || entry.Context.TenantID == nil {
		return nil, commonapi.ErrForbidden
	}
	tenantID, err := strconv.ParseInt(*entry.Context.TenantID, 10, 64)
	if err != nil || tenantID <= 0 || strconv.FormatInt(tenantID, 10) != *entry.Context.TenantID {
		return nil, commonapi.ErrForbidden
	}
	bindings, err := s.resolveReadBindings(ctx, tenantID, paths)
	if err != nil {
		return nil, err
	}
	if request.ExpectedSource != nil && (len(bindings) != 1 || !reflect.DeepEqual(bindings[0].FileComposition, &request.ExpectedSource.FileComposition)) {
		return nil, commonapi.ErrForbidden
	}
	result, err := s.repository.observeSourceRules(ctx, credential, commonauth.SourceAccessRead, paths, bindings, tenantID, resolve, qualify, expectedVersion)
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

// CheckDevelopWorkflowResultRead rechecks all actual sources as the current human.
func (s *Service) CheckDevelopWorkflowResultRead(ctx context.Context, credential string, request SourceReadCheckRequest) (*SourceReadCheck, error) {
	return s.checkSourceRead(ctx, credential, request, false, qualifyDevelopWorkflowResultRead)
}

func qualifyDevelopWorkflowResultRead(current commonauth.AuthContext) error {
	if err := qualifyOrdinarySourceUser(current, "develop.task.read"); err != nil {
		return err
	}
	if !commonauth.HasContextPermissions(current, "develop.data_read.execute") {
		return commonapi.ErrForbidden
	}
	return nil
}

// CheckTransferFieldRecommendationRead covers only the native table being scanned.
func (s *Service) CheckTransferFieldRecommendationRead(ctx context.Context, credential string, request SourceReadCheckRequest) (*SourceReadCheck, error) {
	if request.ExpectedSource != nil || len(request.Targets) != 1 {
		return nil, commonapi.ErrBadRequest
	}
	segments := request.Targets[0].Segments
	if len(segments) != 3 || segments[2].Term != engineplugin.EngineCatalogTermTable {
		return nil, commonapi.ErrBadRequest
	}
	return s.checkSourceRead(ctx, credential, request, false, qualifyTransferFieldUser)
}

func qualifyTransferFieldUser(current commonauth.AuthContext) error {
	return qualifyOrdinarySourceUser(current, "transfer.task.create")
}

// AuthorizeTransferFieldConnection returns only the encrypted row observed in the
// same snapshot as the user's current source Grant. The handler decrypts on success.
func (s *Service) AuthorizeTransferFieldConnection(ctx context.Context, tenantID int64, request client.TransferFieldConnectionRequest) (*models.Engine, error) {
	path := request.Target
	if request.EngineVersion <= 0 || len(path.Segments) != 3 || path.Segments[2].Term != engineplugin.EngineCatalogTermTable {
		return nil, commonapi.ErrBadRequest
	}
	paths, _, err := encodeSourceReadTargets([]engineplugin.EngineCatalogPath{path})
	if err != nil {
		return nil, commonapi.ErrBadRequest
	}
	resolve := func(auth *iam.AuthContextService) (*commonauth.AuthContext, error) {
		return auth.ResolveUserAccessToken(ctx, request.UserCredential)
	}
	userTenant, err := s.repository.resolveSourceCredentialTenant(ctx, resolve, qualifyTransferFieldUser)
	if err != nil {
		return nil, err
	}
	if userTenant != tenantID {
		return nil, commonapi.ErrForbidden
	}
	bindings, err := s.resolveReadBindings(ctx, tenantID, paths)
	if err != nil {
		return nil, err
	}
	var table *models.Engine
	// resolveReadBindings has proved this sole, canonical native target with Meta.
	if len(bindings) != 1 || bindings[0].Scope != nil || bindings[0].FileComposition != nil || bindings[0].MetadataTarget != nil {
		return nil, commonapi.ErrForbidden
	}
	batch := []sourceRuleTarget{{Binding: bindings[0], Action: commonauth.SourceAccessRead}}
	observation, err := s.repository.observePreparedSourceRules(ctx, commonauth.SourceAccessRead, paths, bindings, batch, tenantID, resolve, qualifyTransferFieldUser, &sourceReadEngineVersion{ID: path.EngineID, Version: request.EngineVersion, transferTable: func(engine *models.Engine) { table = engine }})
	if err != nil {
		return nil, err
	}
	if table == nil || !observation.Covered {
		return nil, commonapi.ErrForbidden
	}
	return table, nil
}
