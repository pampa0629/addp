package scanadapter

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
	"gorm.io/gorm"
)

type namespaceScanner interface {
	ScanNamespace(ctx context.Context, enginePlugin plugin.EnginePlugin, resource *commonModels.Engine, tenantID, engineID uint, namespaceName string, scanDepth string, force bool) (int, int, int, error)
}

type branchScanner interface {
	ScanBranch(ctx context.Context, enginePlugin plugin.EnginePlugin, resource *commonModels.Engine, tenantID uint, branchName string, scanDepth string, force bool) (int, int, int, error)
}

type directLeafScanner interface {
	ScanRoot(ctx context.Context, enginePlugin plugin.EnginePlugin, resource *commonModels.Engine, tenantID uint, scanDepth string, force bool) (int, error)
}

type scanLocker interface {
	GenerateNamespaceLockKey(tenantID, engineID uint, namespaceName string) string
	GenerateBranchLockKey(tenantID, engineID uint, branchName string) string
	TryAcquireLock(ctx context.Context, taskKey string, ttl time.Duration) (bool, error)
	ClearTask(ctx context.Context, taskKey string) error
}

type EngineCatalogScanDispatcher struct {
	db             *gorm.DB
	repo           *metaRepo.ScanRepository
	log            *slog.Logger
	namespaceScan  namespaceScanner
	branchScan     branchScanner
	directLeafScan directLeafScanner
	contentScanner *EngineCatalogContentScanner
	locker         scanLocker
}

func NewEngineCatalogScanDispatcher(
	db *gorm.DB,
	repo *metaRepo.ScanRepository,
	log *slog.Logger,
	namespaceScan namespaceScanner,
	branchScan branchScanner,
	directLeafScan directLeafScanner,
	contentScanner *EngineCatalogContentScanner,
) *EngineCatalogScanDispatcher {
	return &EngineCatalogScanDispatcher{
		db:             db,
		repo:           repo,
		log:            log,
		namespaceScan:  namespaceScan,
		branchScan:     branchScan,
		directLeafScan: directLeafScan,
		contentScanner: contentScanner,
	}
}

func (d *EngineCatalogScanDispatcher) SetLocker(locker scanLocker) {
	d.locker = locker
}

func (d *EngineCatalogScanDispatcher) Dispatch(req scanflow.DispatchRequest) (scanflow.DispatchResult, error) {
	ctx := req.Context
	if ctx == nil {
		ctx = context.Background()
	}
	req.Context = ctx
	if req.Resource == nil {
		return scanflow.DispatchResult{}, fmt.Errorf("scan resource is nil")
	}
	enginePlugin := req.EnginePlugin
	if enginePlugin == nil {
		var err error
		enginePlugin, err = plugin.Get(req.Resource.EngineType)
		if err != nil {
			return scanflow.DispatchResult{}, fmt.Errorf("unsupported engine type: %s", req.Resource.EngineType)
		}
	}

	plan, ok := scanflow.EngineCatalogScanPlanForPlugin(enginePlugin)
	if !ok {
		return scanflow.DispatchResult{}, fmt.Errorf("plugin does not expose a supported catalog scan strategy")
	}

	switch plan.Strategy {
	case scanflow.EngineCatalogScanTabular:
		return d.dispatchTabularScan(ctx, enginePlugin, plan, req)
	case scanflow.EngineCatalogScanDirectLeaves:
		return d.dispatchDirectLeafScan(ctx, enginePlugin, req)
	case scanflow.EngineCatalogScanBranchLeaves:
		return d.dispatchBranchLeafScan(ctx, enginePlugin, req)
	case scanflow.EngineCatalogScanObject:
		return d.dispatchObjectCatalogScan(req)
	case scanflow.EngineCatalogScanFile:
		return d.dispatchFileCatalogScan(req)
	default:
		return scanflow.DispatchResult{}, fmt.Errorf("plugin does not support catalog scan strategy %s", plan.Strategy)
	}
}

func (d *EngineCatalogScanDispatcher) dispatchObjectCatalogScan(req scanflow.DispatchRequest) (scanflow.DispatchResult, error) {
	if d.contentScanner == nil {
		return scanflow.DispatchResult{}, fmt.Errorf("content catalog scanner is nil")
	}
	result, err := d.contentScanner.ScanObjectCatalog(req)
	if err == nil {
		d.finalizeEngineCatalogRootAfterScan(req.Resource, req.TenantID, result.Items, req.ScanDepth)
	}
	return result, err
}

func (d *EngineCatalogScanDispatcher) dispatchFileCatalogScan(req scanflow.DispatchRequest) (scanflow.DispatchResult, error) {
	if d.contentScanner == nil {
		return scanflow.DispatchResult{}, fmt.Errorf("content catalog scanner is nil")
	}
	if req.Mode == scanflow.DispatchAuto && len(req.CatalogPaths) == 0 {
		req.CatalogPaths = []string{""}
		d.log.Info("文件 catalog 资源从结构 root 开始扫描")
	}
	result, err := d.contentScanner.ScanFileCatalog(req)
	if err == nil {
		d.finalizeEngineCatalogRootAfterScan(req.Resource, req.TenantID, result.Items, req.ScanDepth)
	}
	return result, err
}
