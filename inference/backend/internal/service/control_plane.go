package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	commonsecurity "github.com/addp/common/security"
	"github.com/addp/inference/internal/models"
	"github.com/addp/inference/internal/repository"
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

const (
	AdapterOpenAICompatible    = "openai_compatible"
	AdapterDashScopeMultimodal = "dashscope_multimodal"

	ChatMaxOutputTokensParameterMaxTokens           = "max_tokens"
	ChatMaxOutputTokensParameterMaxCompletionTokens = "max_completion_tokens"
	ChatTemperatureModeConfigurable                 = "configurable"
	ChatTemperatureModeDefaultOnly                  = "default_only"
)

var profileCodePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

type Actor struct {
	ContextType string
	TenantID    uint
	PrincipalID uint
}

type CredentialStatus struct {
	Configured bool   `json:"configured"`
	Version    uint64 `json:"version"`
}
type PageResult[T any] struct {
	Data       []T   `json:"data"`
	Total      int64 `json:"total"`
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	TotalPages int   `json:"total_pages"`
}
type ProviderView struct {
	models.ProviderConnection
	AllowedTenantIDs []uint           `json:"allowed_tenant_ids"`
	Credential       CredentialStatus `json:"credential"`
}
type ProviderInput struct {
	Name             string `json:"name" binding:"required"`
	ScopeType        string `json:"scope_type" binding:"required"`
	AdapterType      string `json:"adapter_type" binding:"required"`
	Endpoint         string `json:"endpoint" binding:"required"`
	AllowAllTenants  bool   `json:"allow_all_tenants"`
	AllowedTenantIDs []uint `json:"allowed_tenant_ids"`
	Status           string `json:"status"`
}
type DeploymentInput struct {
	ProviderConnectionID         string   `json:"provider_connection_id" binding:"required"`
	Name                         string   `json:"name" binding:"required"`
	UpstreamModel                string   `json:"upstream_model" binding:"required"`
	Operations                   []string `json:"operations" binding:"required"`
	Modalities                   []string `json:"modalities" binding:"required"`
	Dimension                    int      `json:"dimension"`
	ChatMaxOutputTokensParameter string   `json:"chat_max_output_tokens_parameter"`
	ChatTemperatureMode          string   `json:"chat_temperature_mode"`
	Status                       string   `json:"status"`
}
type ProfileInput struct {
	Name              string `json:"name" binding:"required"`
	Code              string `json:"code" binding:"required"`
	ScopeType         string `json:"scope_type" binding:"required"`
	ModelDeploymentID string `json:"model_deployment_id" binding:"required"`
	Status            string `json:"status"`
}

type ControlPlane struct {
	store         *repository.Store
	encryptionKey []byte
}

func NewControlPlane(store *repository.Store, encryptionKey []byte) *ControlPlane {
	return &ControlPlane{store: store, encryptionKey: append([]byte(nil), encryptionKey...)}
}

func (s *ControlPlane) ListProviders(ctx context.Context, actor Actor, page, pageSize int) (*PageResult[ProviderView], error) {
	if err := validateActor(actor); err != nil {
		return nil, err
	}
	values, err := s.store.ListProviders(ctx, actor.ContextType, actor.TenantID)
	if err != nil {
		return nil, err
	}
	result := make([]ProviderView, 0, len(values))
	for i := range values {
		visible, err := s.providerVisible(ctx, actor, &values[i])
		if err != nil {
			return nil, err
		}
		if !visible {
			continue
		}
		view, err := s.providerView(ctx, &values[i])
		if err != nil {
			return nil, err
		}
		result = append(result, *view)
	}
	return paginate(result, page, pageSize), nil
}
func (s *ControlPlane) GetProvider(ctx context.Context, actor Actor, id string) (*ProviderView, error) {
	value, err := s.store.GetProvider(ctx, id, false)
	if repository.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	visible, err := s.providerVisible(ctx, actor, value)
	if err != nil {
		return nil, err
	}
	if !visible {
		return nil, ErrNotFound
	}
	return s.providerView(ctx, value)
}
func (s *ControlPlane) CreateProvider(ctx context.Context, actor Actor, input ProviderInput) (*ProviderView, error) {
	value, grants, err := normalizeProvider(actor, input)
	if err != nil {
		return nil, err
	}
	value.ID, value.CreatedBy, value.UpdatedBy = uuid.NewString(), actor.PrincipalID, actor.PrincipalID
	grantRows := grantRows(value.ID, grants)
	if err := s.store.CreateProvider(ctx, value, grantRows); err != nil {
		return nil, err
	}
	return s.providerView(ctx, value)
}
func (s *ControlPlane) UpdateProvider(ctx context.Context, actor Actor, id string, input ProviderInput) (*ProviderView, error) {
	current, err := s.store.GetProvider(ctx, id, false)
	if repository.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !canManageProvider(actor, current) {
		return nil, ErrNotFound
	}
	next, grants, err := normalizeProvider(actor, input)
	if err != nil {
		return nil, err
	}
	current.Name, current.AdapterType, current.Endpoint, current.AllowAllTenants, current.Status, current.UpdatedBy = next.Name, next.AdapterType, next.Endpoint, next.AllowAllTenants, next.Status, actor.PrincipalID
	if err := s.store.SaveProvider(ctx, current, grantRows(current.ID, grants)); err != nil {
		return nil, err
	}
	return s.providerView(ctx, current)
}
func (s *ControlPlane) DeleteProvider(ctx context.Context, actor Actor, id string) error {
	current, err := s.store.GetProvider(ctx, id, false)
	if repository.IsNotFound(err) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !canManageProvider(actor, current) {
		return ErrNotFound
	}
	count, err := s.store.CountDeploymentsForProvider(ctx, id)
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrResourceInUse
	}
	return s.store.DeleteProvider(ctx, id)
}
func (s *ControlPlane) SetCredential(ctx context.Context, actor Actor, id, credential string) (*CredentialStatus, error) {
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return nil, fmt.Errorf("%w: credential is required", ErrInvalidRequest)
	}
	provider, err := s.store.GetProvider(ctx, id, false)
	if repository.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !canManageProvider(actor, provider) {
		return nil, ErrNotFound
	}
	ciphertext, err := commonsecurity.Encrypt(credential, s.encryptionKey)
	if err != nil {
		return nil, err
	}
	updated, err := s.store.RotateCredential(ctx, id, ciphertext, "rotate", actor.PrincipalID)
	if err != nil {
		return nil, err
	}
	return &CredentialStatus{Configured: true, Version: updated.CredentialVersion}, nil
}
func (s *ControlPlane) DeleteCredential(ctx context.Context, actor Actor, id string) (*CredentialStatus, error) {
	provider, err := s.store.GetProvider(ctx, id, false)
	if repository.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !canManageProvider(actor, provider) {
		return nil, ErrNotFound
	}
	updated, err := s.store.RotateCredential(ctx, id, "", "delete", actor.PrincipalID)
	if err != nil {
		return nil, err
	}
	return &CredentialStatus{Configured: false, Version: updated.CredentialVersion}, nil
}

func (s *ControlPlane) ListDeployments(ctx context.Context, actor Actor, page, pageSize int) (*PageResult[models.ModelDeployment], error) {
	if err := validateActor(actor); err != nil {
		return nil, err
	}
	values, err := s.store.ListDeployments(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]models.ModelDeployment, 0, len(values))
	for _, value := range values {
		provider, err := s.store.GetProvider(ctx, value.ProviderConnectionID, false)
		if err != nil {
			return nil, err
		}
		visible, err := s.providerVisible(ctx, actor, provider)
		if err != nil {
			return nil, err
		}
		if visible {
			result = append(result, value)
		}
	}
	return paginate(result, page, pageSize), nil
}
func (s *ControlPlane) CreateDeployment(ctx context.Context, actor Actor, input DeploymentInput) (*models.ModelDeployment, error) {
	value, err := s.normalizeDeployment(ctx, actor, input)
	if err != nil {
		return nil, err
	}
	value.ID, value.CreatedBy, value.UpdatedBy = uuid.NewString(), actor.PrincipalID, actor.PrincipalID
	if err := s.store.CreateDeployment(ctx, value); err != nil {
		return nil, err
	}
	return value, nil
}
func (s *ControlPlane) GetDeployment(ctx context.Context, actor Actor, id string) (*models.ModelDeployment, error) {
	if err := validateActor(actor); err != nil {
		return nil, err
	}
	value, err := s.store.GetDeployment(ctx, id)
	if repository.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	provider, err := s.store.GetProvider(ctx, value.ProviderConnectionID, false)
	if err != nil {
		return nil, err
	}
	visible, err := s.providerVisible(ctx, actor, provider)
	if err != nil {
		return nil, err
	}
	if !visible {
		return nil, ErrNotFound
	}
	return value, nil
}
func (s *ControlPlane) UpdateDeployment(ctx context.Context, actor Actor, id string, input DeploymentInput) (*models.ModelDeployment, error) {
	current, err := s.store.GetDeployment(ctx, id)
	if repository.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	provider, err := s.store.GetProvider(ctx, current.ProviderConnectionID, false)
	if err != nil || !canManageProvider(actor, provider) {
		return nil, ErrNotFound
	}
	next, err := s.normalizeDeployment(ctx, actor, input)
	if err != nil {
		return nil, err
	}
	if next.ProviderConnectionID != current.ProviderConnectionID {
		return nil, fmt.Errorf("%w: provider_connection_id is immutable", ErrInvalidRequest)
	}
	current.Name, current.UpstreamModel, current.Operations, current.Modalities, current.Dimension, current.ChatMaxOutputTokensParameter, current.ChatTemperatureMode, current.Status, current.UpdatedBy = next.Name, next.UpstreamModel, next.Operations, next.Modalities, next.Dimension, next.ChatMaxOutputTokensParameter, next.ChatTemperatureMode, next.Status, actor.PrincipalID
	if err := s.store.SaveDeployment(ctx, current); err != nil {
		return nil, err
	}
	return current, nil
}
func (s *ControlPlane) DeleteDeployment(ctx context.Context, actor Actor, id string) error {
	current, err := s.store.GetDeployment(ctx, id)
	if repository.IsNotFound(err) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	provider, err := s.store.GetProvider(ctx, current.ProviderConnectionID, false)
	if err != nil || !canManageProvider(actor, provider) {
		return ErrNotFound
	}
	count, err := s.store.CountProfilesForDeployment(ctx, id)
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrResourceInUse
	}
	return s.store.DeleteDeployment(ctx, id)
}

func (s *ControlPlane) ListProfiles(ctx context.Context, actor Actor, page, pageSize int) (*PageResult[models.ModelProfile], error) {
	if err := validateActor(actor); err != nil {
		return nil, err
	}
	values, err := s.store.ListProfiles(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]models.ModelProfile, 0, len(values))
	for _, value := range values {
		visible, err := s.profileVisible(ctx, actor, &value)
		if err != nil {
			return nil, err
		}
		if visible {
			result = append(result, value)
		}
	}
	return paginate(result, page, pageSize), nil
}
func (s *ControlPlane) CreateProfile(ctx context.Context, actor Actor, input ProfileInput) (*models.ModelProfile, error) {
	value, err := s.normalizeProfile(ctx, actor, input)
	if err != nil {
		return nil, err
	}
	value.ID, value.Version, value.CreatedBy, value.UpdatedBy = uuid.NewString(), 1, actor.PrincipalID, actor.PrincipalID
	if err := s.store.CreateProfile(ctx, value); err != nil {
		return nil, err
	}
	return value, nil
}
func (s *ControlPlane) GetProfile(ctx context.Context, actor Actor, id string) (*models.ModelProfile, error) {
	if err := validateActor(actor); err != nil {
		return nil, err
	}
	value, err := s.store.GetProfile(ctx, id)
	if repository.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	visible, err := s.profileVisible(ctx, actor, value)
	if err != nil {
		return nil, err
	}
	if !visible {
		return nil, ErrNotFound
	}
	return value, nil
}
func (s *ControlPlane) UpdateProfile(ctx context.Context, actor Actor, id string, input ProfileInput) (*models.ModelProfile, error) {
	current, err := s.store.GetProfile(ctx, id)
	if repository.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !profileManageable(actor, current) {
		return nil, ErrNotFound
	}
	next, err := s.normalizeProfile(ctx, actor, input)
	if err != nil {
		return nil, err
	}
	if next.ScopeType != current.ScopeType || tenantValue(next.TenantID) != tenantValue(current.TenantID) || next.Code != current.Code {
		return nil, fmt.Errorf("%w: profile scope and code are immutable", ErrInvalidRequest)
	}
	current.Name, current.ModelDeploymentID, current.Status, current.UpdatedBy, current.Version = next.Name, next.ModelDeploymentID, next.Status, actor.PrincipalID, current.Version+1
	if err := s.store.SaveProfile(ctx, current); err != nil {
		return nil, err
	}
	return current, nil
}

func (s *ControlPlane) providerView(ctx context.Context, value *models.ProviderConnection) (*ProviderView, error) {
	grants, err := s.store.ProviderGrants(ctx, value.ID)
	if err != nil {
		return nil, err
	}
	copyValue := *value
	copyValue.CredentialCiphertext = ""
	return &ProviderView{ProviderConnection: copyValue, AllowedTenantIDs: grants, Credential: CredentialStatus{Configured: value.CredentialCiphertext != "", Version: value.CredentialVersion}}, nil
}
func normalizeProvider(actor Actor, input ProviderInput) (*models.ProviderConnection, []uint, error) {
	if err := validateActor(actor); err != nil {
		return nil, nil, err
	}
	status, err := normalizeStatus(input.Status)
	if err != nil {
		return nil, nil, err
	}
	input.Name, input.AdapterType, input.Endpoint, input.Status = strings.TrimSpace(input.Name), strings.TrimSpace(input.AdapterType), strings.TrimRight(strings.TrimSpace(input.Endpoint), "/"), status
	if input.Name == "" || (input.AdapterType != AdapterOpenAICompatible && input.AdapterType != AdapterDashScopeMultimodal) || !validEndpoint(input.Endpoint) {
		return nil, nil, fmt.Errorf("%w: invalid provider fields", ErrInvalidRequest)
	}
	if input.ScopeType != actor.ContextType || (input.ScopeType != models.ScopePlatform && input.ScopeType != models.ScopeTenant) {
		return nil, nil, ErrForbidden
	}
	var tenantID *uint
	if input.ScopeType == models.ScopeTenant {
		value := actor.TenantID
		tenantID = &value
		input.AllowAllTenants = false
		input.AllowedTenantIDs = nil
	}
	grants, err := normalizeUintSet(input.AllowedTenantIDs)
	if err != nil {
		return nil, nil, err
	}
	if input.AllowAllTenants && len(grants) > 0 {
		return nil, nil, fmt.Errorf("%w: allow_all_tenants and allowlist are mutually exclusive", ErrInvalidRequest)
	}
	return &models.ProviderConnection{Name: input.Name, ScopeType: input.ScopeType, TenantID: tenantID, AdapterType: input.AdapterType, Endpoint: input.Endpoint, AllowAllTenants: input.AllowAllTenants, Status: input.Status}, grants, nil
}
func (s *ControlPlane) normalizeDeployment(ctx context.Context, actor Actor, input DeploymentInput) (*models.ModelDeployment, error) {
	provider, err := s.store.GetProvider(ctx, input.ProviderConnectionID, false)
	if repository.IsNotFound(err) || (err == nil && !canManageProvider(actor, provider)) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	status, err := normalizeStatus(input.Status)
	if err != nil {
		return nil, err
	}
	input.Name, input.UpstreamModel, input.ChatMaxOutputTokensParameter, input.ChatTemperatureMode, input.Status = strings.TrimSpace(input.Name), strings.TrimSpace(input.UpstreamModel), strings.TrimSpace(input.ChatMaxOutputTokensParameter), strings.TrimSpace(input.ChatTemperatureMode), status
	if input.ChatMaxOutputTokensParameter == "" {
		input.ChatMaxOutputTokensParameter = ChatMaxOutputTokensParameterMaxTokens
	}
	if input.ChatTemperatureMode == "" {
		input.ChatTemperatureMode = ChatTemperatureModeConfigurable
	}
	operations, err := normalizeEnumSet(input.Operations, map[string]bool{"chat": true, "embedding": true, "rerank": true})
	if err != nil {
		return nil, err
	}
	modalities, err := normalizeEnumSet(input.Modalities, map[string]bool{"text": true, "image": true})
	if err != nil {
		return nil, err
	}
	if input.Name == "" || input.UpstreamModel == "" || len(operations) == 0 || len(modalities) == 0 || input.Dimension < 0 {
		return nil, fmt.Errorf("%w: invalid deployment fields", ErrInvalidRequest)
	}
	if input.ChatMaxOutputTokensParameter != ChatMaxOutputTokensParameterMaxTokens && input.ChatMaxOutputTokensParameter != ChatMaxOutputTokensParameterMaxCompletionTokens {
		return nil, fmt.Errorf("%w: invalid chat max output tokens parameter", ErrInvalidRequest)
	}
	if input.ChatTemperatureMode != ChatTemperatureModeConfigurable && input.ChatTemperatureMode != ChatTemperatureModeDefaultOnly {
		return nil, fmt.Errorf("%w: invalid chat temperature mode", ErrInvalidRequest)
	}
	if !contains(operations, "embedding") && input.Dimension != 0 {
		return nil, fmt.Errorf("%w: dimension requires embedding operation", ErrInvalidRequest)
	}
	return &models.ModelDeployment{ProviderConnectionID: provider.ID, Name: input.Name, UpstreamModel: input.UpstreamModel, Operations: mustJSON(operations), Modalities: mustJSON(modalities), Dimension: input.Dimension, ChatMaxOutputTokensParameter: input.ChatMaxOutputTokensParameter, ChatTemperatureMode: input.ChatTemperatureMode, Status: input.Status}, nil
}
func (s *ControlPlane) normalizeProfile(ctx context.Context, actor Actor, input ProfileInput) (*models.ModelProfile, error) {
	status, err := normalizeStatus(input.Status)
	if err != nil {
		return nil, err
	}
	input.Name, input.Code, input.Status = strings.TrimSpace(input.Name), strings.TrimSpace(input.Code), status
	if input.Name == "" || !profileCodePattern.MatchString(input.Code) || input.ScopeType != actor.ContextType {
		return nil, fmt.Errorf("%w: invalid profile fields", ErrInvalidRequest)
	}
	deployment, err := s.store.GetDeployment(ctx, input.ModelDeploymentID)
	if repository.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	provider, err := s.store.GetProvider(ctx, deployment.ProviderConnectionID, false)
	if err != nil {
		return nil, err
	}
	var tenantID *uint
	if input.ScopeType == models.ScopePlatform {
		if provider.ScopeType != models.ScopePlatform {
			return nil, ErrForbidden
		}
	} else {
		allowed, err := providerAllowedForTenant(ctx, s.store, provider, actor.TenantID)
		if err != nil {
			return nil, err
		}
		if actor.TenantID == 0 || !allowed {
			return nil, ErrForbidden
		}
		value := actor.TenantID
		tenantID = &value
	}
	return &models.ModelProfile{Name: input.Name, Code: input.Code, ScopeType: input.ScopeType, TenantID: tenantID, ModelDeploymentID: deployment.ID, Status: input.Status}, nil
}

func validateActor(actor Actor) error {
	if actor.PrincipalID == 0 || (actor.ContextType != models.ScopePlatform && actor.ContextType != models.ScopeTenant) || (actor.ContextType == models.ScopeTenant && actor.TenantID == 0) || (actor.ContextType == models.ScopePlatform && actor.TenantID != 0) {
		return ErrForbidden
	}
	return nil
}
func (s *ControlPlane) providerVisible(ctx context.Context, actor Actor, p *models.ProviderConnection) (bool, error) {
	if actor.ContextType == models.ScopePlatform {
		return p.ScopeType == models.ScopePlatform, nil
	}
	return providerAllowedForTenant(ctx, s.store, p, actor.TenantID)
}
func canManageProvider(actor Actor, p *models.ProviderConnection) bool {
	return actor.ContextType == p.ScopeType && (p.ScopeType == models.ScopePlatform || (p.TenantID != nil && *p.TenantID == actor.TenantID))
}
func (s *ControlPlane) profileVisible(ctx context.Context, actor Actor, p *models.ModelProfile) (bool, error) {
	if actor.ContextType == models.ScopePlatform {
		return p.ScopeType == models.ScopePlatform, nil
	}
	if p.ScopeType != models.ScopePlatform && (p.ScopeType != models.ScopeTenant || tenantValue(p.TenantID) != actor.TenantID) {
		return false, nil
	}
	deployment, err := s.store.GetDeployment(ctx, p.ModelDeploymentID)
	if err != nil {
		return false, err
	}
	provider, err := s.store.GetProvider(ctx, deployment.ProviderConnectionID, false)
	if err != nil {
		return false, err
	}
	return providerAllowedForTenant(ctx, s.store, provider, actor.TenantID)
}
func profileManageable(actor Actor, p *models.ModelProfile) bool {
	return actor.ContextType == p.ScopeType && (p.ScopeType == models.ScopePlatform || tenantValue(p.TenantID) == actor.TenantID)
}
func providerAllowedForTenant(ctx context.Context, store *repository.Store, p *models.ProviderConnection, tenantID uint) (bool, error) {
	if p.ScopeType == models.ScopeTenant {
		return tenantValue(p.TenantID) == tenantID, nil
	}
	if p.AllowAllTenants {
		return true, nil
	}
	grants, err := store.ProviderGrants(ctx, p.ID)
	if err != nil {
		return false, err
	}
	for _, id := range grants {
		if id == tenantID {
			return true, nil
		}
	}
	return false, nil
}
func normalizeStatus(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return models.StatusActive, nil
	}
	if value != models.StatusActive && value != models.StatusDisabled {
		return "", fmt.Errorf("%w: status must be active or disabled", ErrInvalidRequest)
	}
	return value, nil
}
func validEndpoint(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
}
func normalizeUintSet(values []uint) ([]uint, error) {
	seen := map[uint]bool{}
	out := make([]uint, 0, len(values))
	for _, value := range values {
		if value == 0 {
			return nil, fmt.Errorf("%w: tenant id must be positive", ErrInvalidRequest)
		}
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}
func normalizeEnumSet(values []string, allowed map[string]bool) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if !allowed[value] {
			return nil, fmt.Errorf("%w: invalid value %q", ErrInvalidRequest, value)
		}
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out, nil
}
func grantRows(providerID string, ids []uint) []models.ProviderTenantGrant {
	rows := make([]models.ProviderTenantGrant, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, models.ProviderTenantGrant{ProviderConnectionID: providerID, TenantID: id})
	}
	return rows
}
func mustJSON(value interface{}) datatypes.JSON {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return datatypes.JSON(encoded)
}
func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func tenantValue(value *uint) uint {
	if value == nil {
		return 0
	}
	return *value
}
func decodeStrings(value datatypes.JSON) ([]string, error) {
	var result []string
	return result, json.Unmarshal(value, &result)
}

func paginate[T any](values []T, page, pageSize int) *PageResult[T] {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	total := len(values)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	data := append([]T(nil), values[start:end]...)
	return &PageResult[T]{Data: data, Total: int64(total), Page: page, PageSize: pageSize, TotalPages: (total + pageSize - 1) / pageSize}
}
