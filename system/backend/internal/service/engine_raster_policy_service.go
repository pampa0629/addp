package service

import (
	"context"
	"encoding/json"
	"fmt"
	commonapi "github.com/addp/common/api"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"gorm.io/gorm"
	"io"
	"net/http"
	"strconv"
	"time"
)

type EngineRasterPolicyService struct {
	repo *repository.EngineRasterPolicyRepository
}

func NewEngineRasterPolicyService(repo *repository.EngineRasterPolicyRepository) *EngineRasterPolicyService {
	return &EngineRasterPolicyService{repo: repo}
}

type RasterEngineSummary struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

func (s *EngineRasterPolicyService) Engines(ctx context.Context) ([]RasterEngineSummary, error) {
	engines, err := s.repo.Engines(ctx)
	if err != nil {
		return nil, err
	}
	values := make([]RasterEngineSummary, 0, len(engines))
	for _, e := range engines {
		values = append(values, RasterEngineSummary{e.ID, e.Name})
	}
	return values, nil
}
func validateRasterPolicy(p models.EngineRasterPolicy) error {
	if p.Running < 1 || p.Running > 64 || p.Waiting < 0 || p.Waiting > 1024 || p.CacheMiB < 16 || p.CacheMiB > 65536 || p.DefaultTenantRunning < 1 || p.DefaultTenantRunning > p.Running || p.DefaultTenantWaiting < 0 || p.DefaultTenantWaiting > p.Waiting {
		return fmt.Errorf("%w: invalid raster resource limits", commonapi.ErrBadRequest)
	}
	return nil
}
func rasterView(p models.EngineRasterPolicy, q *models.EngineRasterQuota) *models.RasterPolicyView {
	v := &models.RasterPolicyView{Policy: p, Quota: q, EffectiveRunning: p.DefaultTenantRunning, EffectiveWaiting: p.DefaultTenantWaiting}
	if q != nil {
		if q.Running != nil {
			v.EffectiveRunning = min(*q.Running, p.Running)
		}
		if q.Waiting != nil {
			v.EffectiveWaiting = min(*q.Waiting, p.Waiting)
		}
	}
	return v
}
func (s *EngineRasterPolicyService) Get(ctx context.Context, id, tenant uint) (*models.RasterPolicyView, error) {
	var view *models.RasterPolicyView
	err := s.repo.WithPolicy(ctx, id, tenant, func(tx *gorm.DB, p *models.EngineRasterPolicy, q *models.EngineRasterQuota) error {
		if err := validateRasterPolicy(*p); err != nil {
			return err
		}
		view = rasterView(*p, q)
		return nil
	})
	return view, err
}
func (s *EngineRasterPolicyService) SavePlatform(ctx context.Context, id uint, candidate models.EngineRasterPolicy, audit iam.AuditMetadata) (*models.RasterPolicyView, error) {
	if candidate.Version < 1 {
		return nil, commonapi.ErrBadRequest
	}
	if err := validateRasterPolicy(candidate); err != nil {
		return nil, err
	}
	var view *models.RasterPolicyView
	err := s.repo.WithPolicy(ctx, id, 0, func(tx *gorm.DB, p *models.EngineRasterPolicy, q *models.EngineRasterQuota) error {
		before := *p
		expected := candidate.Version
		candidate.EngineID = id
		candidate.Version = p.Version
		if err := repository.SaveRasterPolicy(tx, &candidate, expected); err != nil {
			return err
		}
		if err := writeRasterAudit(ctx, tx, audit, id, "engine_raster_policy.updated", before, candidate); err != nil {
			return err
		}
		view = rasterView(candidate, nil)
		return nil
	})
	return view, err
}
func (s *EngineRasterPolicyService) SaveTenant(ctx context.Context, id, tenant uint, candidate models.EngineRasterQuota, audit iam.AuditMetadata) (*models.RasterPolicyView, error) {
	if tenant < 1 || candidate.Version < 1 {
		return nil, commonapi.ErrBadRequest
	}
	var view *models.RasterPolicyView
	err := s.repo.WithPolicy(ctx, id, tenant, func(tx *gorm.DB, p *models.EngineRasterPolicy, q *models.EngineRasterQuota) error {
		if (candidate.Running != nil && (*candidate.Running < 1 || *candidate.Running > p.Running)) || (candidate.Waiting != nil && (*candidate.Waiting < 0 || *candidate.Waiting > p.Waiting)) {
			return commonapi.ErrBadRequest
		}
		before := *q
		expected := candidate.Version
		candidate.EngineID = id
		candidate.TenantID = tenant
		candidate.Version = q.Version
		if err := repository.SaveRasterQuota(tx, &candidate, expected); err != nil {
			return err
		}
		if err := writeRasterAudit(ctx, tx, audit, id, "engine_raster_quota.updated", before, candidate); err != nil {
			return err
		}
		view = rasterView(*p, &candidate)
		return nil
	})
	return view, err
}
func writeRasterAudit(ctx context.Context, tx *gorm.DB, audit iam.AuditMetadata, id uint, event string, before, after interface{}) error {
	return iam.NewAuditWriter(iam.NewRepository(tx)).Write(ctx, iam.AuditEvent{Metadata: audit, EventName: event, Result: iam.AuditResultSucceeded, RiskLevel: iam.AuditRiskMedium, ModuleName: "system", EntityType: "engine_raster_policy", EntityID: strconv.Itoa(int(id)), Details: map[string]interface{}{"before": before, "after": after}})
}

type RuntimeRasterPolicy struct {
	Policy models.EngineRasterPolicy  `json:"policy"`
	Quotas []models.EngineRasterQuota `json:"quotas"`
}

func (s *EngineRasterPolicyService) Resolve(ctx context.Context, connection models.ConnectionInfo) (*RuntimeRasterPolicy, error) {
	engines, err := s.repo.Engines(ctx)
	if err != nil {
		return nil, err
	}
	key, err := buildConnectionIdentityKey("geopython_workflow", connection)
	if err != nil {
		return nil, commonapi.ErrBadRequest
	}
	for _, e := range engines {
		identity, identityErr := buildConnectionIdentityKey(e.EngineType, e.ConnectionInfo)
		if identityErr != nil {
			return nil, identityErr
		}
		if identity == key {
			var result RuntimeRasterPolicy
			err := s.repo.WithPolicy(ctx, e.ID, 0, func(tx *gorm.DB, p *models.EngineRasterPolicy, _ *models.EngineRasterQuota) error {
				if err := validateRasterPolicy(*p); err != nil {
					return err
				}
				result.Policy = *p
				result.Quotas = []models.EngineRasterQuota{}
				return tx.Where("engine_id = ?", e.ID).Order("tenant_id").Find(&result.Quotas).Error
			})
			return &result, err
		}
	}
	return nil, gorm.ErrRecordNotFound
}
func (s *EngineRasterPolicyService) RuntimeHealth(ctx context.Context, id uint) map[string]interface{} {
	engines, err := s.repo.Engines(ctx)
	if err != nil {
		return nil
	}
	for _, e := range engines {
		if e.ID != id {
			continue
		}
		base, err := engineplugin.RuntimeBaseURL(engineplugin.ConnectionInfo(e.ConnectionInfo))
		if err != nil {
			return nil
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
		if err != nil {
			return nil
		}
		client := &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		response, err := client.Do(request)
		if err != nil {
			return nil
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return nil
		}
		var body struct {
			RasterResources map[string]interface{} `json:"raster_resources"`
		}
		if json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&body) != nil {
			return nil
		}
		return body.RasterResources
	}
	return nil
}
