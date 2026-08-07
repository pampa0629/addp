package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

type BaseMapProviderResponse struct {
	Version                uint64 `json:"version"`
	ScopeType              string `json:"scope_type"`
	TenantID               *uint  `json:"tenant_id,omitempty"`
	Provider               string `json:"provider"`
	Enabled                bool   `json:"enabled"`
	SortOrder              int    `json:"sort_order"`
	AMapKeyConfigured      bool   `json:"amap_key_configured"`
	AMapSecurityConfigured bool   `json:"amap_security_js_code_configured"`
	TDTKeyConfigured       bool   `json:"tdt_key_configured"`
}

type UpdateBaseMapProviderInput struct {
	Version            uint64 `json:"version"`
	Provider           string `json:"provider" binding:"required"`
	Enabled            bool   `json:"enabled"`
	SortOrder          int    `json:"sort_order"`
	AMapKey            string `json:"amap_key"`
	AMapSecurityJsCode string `json:"amap_security_js_code"`
	TDTKey             string `json:"tdt_key"`
}

type BaseMapProviderService struct {
	repo *repository.BaseMapProviderRepository
}

func NewBaseMapProviderService(repo *repository.BaseMapProviderRepository) *BaseMapProviderService {
	return &BaseMapProviderService{repo: repo}
}

func (s *BaseMapProviderService) List(ctx context.Context, scope string, tenantID *uint) ([]BaseMapProviderResponse, error) {
	values, err := s.repo.List(ctx, scope, tenantID)
	if err != nil {
		return nil, err
	}
	result := make([]BaseMapProviderResponse, 0, len(values))
	for _, value := range values {
		result = append(result, baseMapProviderResponse(&value))
	}
	return result, nil
}

func (s *BaseMapProviderService) Update(ctx context.Context, scope string, tenantID *uint, input UpdateBaseMapProviderInput, updatedBy uint) (BaseMapProviderResponse, error) {
	input.Provider = strings.TrimSpace(input.Provider)
	if input.Provider != models.MapProviderOSM && input.Provider != models.MapProviderAMap && input.Provider != models.MapProviderTDT {
		return BaseMapProviderResponse{}, fmt.Errorf("unsupported map provider %q", input.Provider)
	}
	if scope != models.MapScopePlatform && scope != models.MapScopeTenant {
		return BaseMapProviderResponse{}, fmt.Errorf("invalid map scope")
	}
	if scope == models.MapScopePlatform {
		tenantID = nil
	}
	if existing, err := s.repo.List(ctx, scope, tenantID); err != nil {
		return BaseMapProviderResponse{}, err
	} else {
		for _, item := range existing {
			if item.Provider == input.Provider {
				if input.AMapKey == "" {
					input.AMapKey = item.AMapKey
				}
				if input.AMapSecurityJsCode == "" {
					input.AMapSecurityJsCode = item.AMapSecurityJsCode
				}
				if input.TDTKey == "" {
					input.TDTKey = item.TDTKey
				}
				break
			}
		}
	}
	value := &models.BaseMapProvider{ScopeType: scope, TenantID: tenantID, Provider: input.Provider, Enabled: input.Enabled, SortOrder: input.SortOrder, AMapKey: strings.TrimSpace(input.AMapKey), AMapSecurityJsCode: strings.TrimSpace(input.AMapSecurityJsCode), TDTKey: strings.TrimSpace(input.TDTKey), UpdatedBy: updatedBy}
	if err := validateBaseMapProvider(value); err != nil {
		return BaseMapProviderResponse{}, err
	}
	if err := s.repo.Save(ctx, value, input.Version); err != nil {
		return BaseMapProviderResponse{}, err
	}
	return baseMapProviderResponse(value), nil
}

func (s *BaseMapProviderService) ResolvePublic(ctx context.Context, tenantID uint) (map[string]interface{}, error) {
	values, err := s.repo.GetEffective(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	result := map[string]interface{}{"amap_key": "", "amap_security_js_code": "", "tdt_key": "", "providers": values}
	for _, value := range values {
		switch value.Provider {
		case models.MapProviderAMap:
			result["amap_key"], result["amap_security_js_code"] = value.AMapKey, value.AMapSecurityJsCode
		case models.MapProviderTDT:
			result["tdt_key"] = value.TDTKey
		}
	}
	return result, nil
}

func validateBaseMapProvider(value *models.BaseMapProvider) error {
	if value.SortOrder < 0 {
		return fmt.Errorf("sort_order must not be negative")
	}
	if value.Provider == models.MapProviderAMap && value.Enabled && value.AMapKey == "" {
		return fmt.Errorf("AMap key is required when provider is enabled")
	}
	if value.Provider == models.MapProviderTDT && value.Enabled && value.TDTKey == "" {
		return fmt.Errorf("Tianditu key is required when provider is enabled")
	}
	return nil
}

func baseMapProviderResponse(value *models.BaseMapProvider) BaseMapProviderResponse {
	return BaseMapProviderResponse{Version: value.Version, ScopeType: value.ScopeType, TenantID: value.TenantID, Provider: value.Provider, Enabled: value.Enabled, SortOrder: value.SortOrder, AMapKeyConfigured: value.AMapKey != "", AMapSecurityConfigured: value.AMapSecurityJsCode != "", TDTKeyConfigured: value.TDTKey != ""}
}

func sortBaseMapProviders(values []models.BaseMapProvider) {
	sort.Slice(values, func(i, j int) bool { return values[i].SortOrder < values[j].SortOrder })
}
