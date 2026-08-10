package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/instanceprovider"
	engineplugin "github.com/addp/common/engine/plugin"
	supermapworkflow "github.com/addp/common/engine/plugins/supermap_workflow"
	engineselection "github.com/addp/common/engine/selection"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// EngineService obtains same-tenant engine facts from System with addp-meta's
// Service Access Token. It never accepts or forwards a user's credential.
type EngineService struct {
	db             *gorm.DB
	systemClient   *commonClient.SystemServiceClient
	cacheMu        sync.RWMutex
	engineCache    map[engineCacheKey]*engineCacheEntry
	cacheTTL       time.Duration
	log            *slog.Logger
	rootReconciler *CatalogRootReconciler
}

func NewEngineService(db *gorm.DB, systemClient *commonClient.SystemServiceClient) *EngineService {
	service := &EngineService{
		db:           db,
		systemClient: systemClient,
		engineCache:  make(map[engineCacheKey]*engineCacheEntry),
		cacheTTL:     5 * time.Minute,
		log:          logger.With("component", "engine_service"),
	}
	service.rootReconciler = NewCatalogRootReconciler(db)
	return service
}

// GetEnginesByTenant returns active storage engines visible to the exact
// Tenant Context embedded in addp-meta's Service Access Token.
func (s *EngineService) GetEnginesByTenant(tenantID uint) ([]*commonModels.Engine, error) {
	if tenantID == 0 {
		return nil, errors.New("tenant ID is required to list engines")
	}
	if s == nil || s.systemClient == nil {
		return nil, errors.New("System service client is not configured")
	}
	resources, err := s.systemClient.WithTenantID(tenantID).ListEngines(context.Background())
	if err != nil {
		return nil, fmt.Errorf("list tenant engines from System: %w", err)
	}
	engines := make([]*commonModels.Engine, 0, len(resources))
	for i := range resources {
		resource := resources[i]
		if !resource.IsUsable() || resource.TenantID == nil || *resource.TenantID != tenantID ||
			!engineselection.HasStorageCapability(&resource) {
			continue
		}
		resourceCopy := resource
		engines = append(engines, &resourceCopy)
	}
	s.log.Info("从 System 获取租户引擎列表成功", "tenant_id", tenantID, "count", len(engines))
	return engines, nil
}

// GetResourceByID returns decrypted connection details through System's
// Service Principal branch after enforcing the execution Tenant Context.
func (s *EngineService) GetResourceByID(engineID, tenantID uint) (*commonModels.Engine, error) {
	if engineID == 0 || tenantID == 0 {
		return nil, errors.New("engine ID and tenant ID are required")
	}
	if s == nil {
		return nil, errors.New("engine service is not configured")
	}
	key := engineCacheKey{tenantID: tenantID, engineID: engineID}
	s.cacheMu.RLock()
	entry := s.engineCache[key]
	s.cacheMu.RUnlock()
	if entry != nil && entry.resource != nil && time.Now().Before(entry.expiresAt) {
		resourceCopy := *entry.resource
		s.log.Info("引擎连接信息命中缓存", append(connectionLogFields(&resourceCopy),
			"tenant_id", tenantID, "source", "cache",
			"expires_in_seconds", int(time.Until(entry.expiresAt).Seconds()),
		)...)
		return &resourceCopy, nil
	}
	if s.systemClient == nil {
		return nil, errors.New("System service client is not configured")
	}

	resource, err := s.systemClient.WithTenantID(tenantID).GetEngine(context.Background(), engineID)
	if err != nil {
		return nil, fmt.Errorf("get tenant engine from System: %w", err)
	}
	if resource.TenantID == nil || *resource.TenantID != tenantID {
		return nil, errors.New("engine not found in current tenant")
	}
	if s.rootReconciler != nil {
		s.rootReconciler.Reconcile(resource)
	}
	s.cacheEngine(tenantID, resource)
	s.log.Info("通过 Service Access Token 获取引擎连接信息成功", append(connectionLogFields(resource),
		"tenant_id", tenantID, "source", "system_service_api",
	)...)
	return resource, nil
}

// GetEngine implements the engine lookup used by resource-tree and CAD paths.
func (s *EngineService) GetEngine(engineID, tenantID uint) (*commonModels.Engine, error) {
	return s.GetResourceByID(engineID, tenantID)
}

// ResolveScanPlugin returns the instance-scoped provider used for one scan.
// General PostgreSQL remains the catalog owner; an enabled SuperMap SDX+ for PostgreSQL
// workspace replaces its sdx table facts with the bound SuperMap SDK provider.
func (s *EngineService) ResolveScanPlugin(ctx context.Context, resource *commonModels.Engine, tenantID uint) (engineplugin.EnginePlugin, error) {
	if resource == nil {
		return nil, errors.New("engine resource is required")
	}
	if s == nil || s.systemClient == nil {
		return engineplugin.Get(resource.EngineType)
	}
	return instanceprovider.Resolve(ctx, s.systemClient.WithTenantID(tenantID), resource, supermapworkflow.RequiredTableReadOperators()...)
}

// GetEnginesWithStats returns tenant engines with Meta scan statistics.
func (s *EngineService) GetEnginesWithStats(tenantID uint) ([]*models.ResourceWithStats, error) {
	engines, err := s.GetEnginesByTenant(tenantID)
	if err != nil {
		return nil, err
	}
	if len(engines) == 0 {
		return []*models.ResourceWithStats{}, nil
	}
	stats, err := loadEngineScanStats(s.db, engines)
	if err != nil {
		return nil, err
	}
	result := make([]*models.ResourceWithStats, 0, len(engines))
	for _, resource := range engines {
		result = append(result, buildResourceWithStats(resource, stats))
	}
	return result, nil
}
