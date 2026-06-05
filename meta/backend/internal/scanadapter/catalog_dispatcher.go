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
	ScanNamespace(ctx context.Context, resource *commonModels.Engine, tenantID, engineID uint, namespaceName string, scanDepth string, force bool) (int, int, int, error)
}

type branchScanner interface {
	ScanBranch(ctx context.Context, enginePlugin plugin.EnginePlugin, resource *commonModels.Engine, tenantID uint, branchName string, scanDepth string, force bool) (int, int, int, error)
}

type scanLocker interface {
	GenerateNamespaceLockKey(tenantID, engineID uint, namespaceName string) string
	GenerateBranchLockKey(tenantID, engineID uint, branchName string) string
	TryAcquireLock(ctx context.Context, taskKey string, ttl time.Duration) (bool, error)
	ClearTask(ctx context.Context, taskKey string) error
}

type CatalogDispatcher struct {
	db             *gorm.DB
	repo           *metaRepo.ScanRepository
	log            *slog.Logger
	namespaceScan  namespaceScanner
	branchScan     branchScanner
	contentScanner *ContentCatalogScanner
	locker         scanLocker
}

func NewCatalogDispatcher(
	db *gorm.DB,
	repo *metaRepo.ScanRepository,
	log *slog.Logger,
	namespaceScan namespaceScanner,
	branchScan branchScanner,
	contentScanner *ContentCatalogScanner,
) *CatalogDispatcher {
	return &CatalogDispatcher{
		db:             db,
		repo:           repo,
		log:            log,
		namespaceScan:  namespaceScan,
		branchScan:     branchScan,
		contentScanner: contentScanner,
	}
}

func (d *CatalogDispatcher) SetLocker(locker scanLocker) {
	d.locker = locker
}

func (d *CatalogDispatcher) Dispatch(req scanflow.DispatchRequest) (scanflow.DispatchResult, error) {
	if req.Resource == nil {
		return scanflow.DispatchResult{}, fmt.Errorf("scan resource is nil")
	}
	enginePlugin, err := plugin.Get(req.Resource.EngineType)
	if err != nil {
		return scanflow.DispatchResult{}, fmt.Errorf("unsupported engine type: %s", req.Resource.EngineType)
	}

	plan, ok := scanflow.CatalogScanPlanForPlugin(enginePlugin)
	if !ok {
		return scanflow.DispatchResult{}, fmt.Errorf("plugin does not expose a supported catalog scan strategy")
	}

	switch plan.Strategy {
	case scanflow.CatalogScanTabular:
		return d.dispatchTabularScan(context.Background(), enginePlugin, plan, req)
	case scanflow.CatalogScanBranchLeaves:
		return d.dispatchBranchLeafScan(context.Background(), enginePlugin, req)
	case scanflow.CatalogScanObject:
		return d.dispatchObjectCatalogScan(req)
	case scanflow.CatalogScanFile:
		return d.dispatchFileCatalogScan(req)
	default:
		return scanflow.DispatchResult{}, fmt.Errorf("plugin does not support catalog scan strategy %s", plan.Strategy)
	}
}

func (d *CatalogDispatcher) dispatchObjectCatalogScan(req scanflow.DispatchRequest) (scanflow.DispatchResult, error) {
	if d.contentScanner == nil {
		return scanflow.DispatchResult{}, fmt.Errorf("content catalog scanner is nil")
	}
	result, err := d.contentScanner.ScanObjectCatalog(req)
	if err == nil {
		d.finalizeCatalogRootAfterScan(req.Resource, req.TenantID, result.Items, req.ScanDepth)
	}
	return result, err
}

func (d *CatalogDispatcher) dispatchFileCatalogScan(req scanflow.DispatchRequest) (scanflow.DispatchResult, error) {
	if d.contentScanner == nil {
		return scanflow.DispatchResult{}, fmt.Errorf("content catalog scanner is nil")
	}
	if req.Mode == scanflow.DispatchAuto && len(req.CatalogPaths) == 0 {
		req.CatalogPaths = []string{""}
		d.log.Info("文件 catalog 资源从结构 root 开始扫描")
	}
	result, err := d.contentScanner.ScanFileCatalog(req)
	if err == nil {
		d.finalizeCatalogRootAfterScan(req.Resource, req.TenantID, result.Items, req.ScanDepth)
	}
	return result, err
}
