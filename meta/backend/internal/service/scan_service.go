package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/events"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanadapter"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanresolver"
	"github.com/addp/meta/internal/scanruntime"
	"gorm.io/gorm"
)

// ScanService 统一扫描服务
type ScanService struct {
	db                          *gorm.DB
	repo                        *metaRepo.ScanRepository // 数据访问层
	runtimes                    *scanruntime.Runtimes
	engineCatalogScanDispatcher *scanadapter.EngineCatalogScanDispatcher // catalog 扫描分发主链路
	scopeResolver               *scanresolver.Resolver                   // 扫描入口 scope 解析器
	engineService               *EngineService
	config                      *config.Config
	log                         *slog.Logger
	indexerService              *IndexerService            // 索引服务（独立）
	scanEventPublisher          *events.ScanEventPublisher // 扫描事件发布器
	dedupService                *ScanDedupService          // 扫描去重服务（可选）
}

func NewScanService(db *gorm.DB, engineService *EngineService) *ScanService {
	if engineService == nil {
		engineService = NewEngineService(db, nil)
	}

	repo := metaRepo.NewScanRepository(db)
	log := logger.With("component", "scan_service")

	// 创建独立的服务（消除循环依赖）
	indexerService := NewIndexerService(nil, log) // indexer 稍后通过 SetIndexer 注入
	runtimes := scanruntime.NewRuntimes(db, log, repo, indexerService)
	runtimes.SetCADInspector(NewSuperMapCADInspector(engineService))
	runtimes.SetContainerInspector(NewWorkflowContainerInspector(engineService))

	s := &ScanService{
		db:             db,
		repo:           repo,
		runtimes:       runtimes,
		engineService:  engineService,
		log:            log,
		indexerService: indexerService,
		scopeResolver:  scanresolver.New(db),
	}

	s.engineCatalogScanDispatcher = scanadapter.NewEngineCatalogScanDispatcher(
		db,
		repo,
		log,
		s.runtimes.Database,
		s.runtimes.BranchLeaf,
		s.runtimes.DirectLeaf,
		s.runtimes.EngineCatalogContentScanner,
	)

	return s
}

func (s *ScanService) SetContentIndexClient(client *commonClient.ManagerContentClient) {
	if s.indexerService != nil {
		s.indexerService.contentIndex = client
	}
}

// SetConfig 注入配置
func (s *ScanService) SetConfig(cfg *config.Config) {
	s.config = cfg
}

// SetScanEventPublisher 注入扫描事件发布器
func (s *ScanService) SetScanEventPublisher(publisher *events.ScanEventPublisher) {
	s.scanEventPublisher = publisher
}

// SetDedupService 注入扫描去重服务
func (s *ScanService) SetDedupService(dedupService *ScanDedupService) {
	s.dedupService = dedupService
	if s.engineCatalogScanDispatcher != nil {
		s.engineCatalogScanDispatcher.SetLocker(dedupService)
	}

	// 启动时清理所有残留的扫描锁（防止上次服务异常退出时的锁未清理）
	if dedupService != nil {
		s.cleanupStaleScanLocks()
	}
}

func (s *ScanService) ScanEngineWithOptions(opts scanflow.Options) (*models.ScanResponse, error) {
	startTime := time.Now()
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	tenantID := opts.TenantID
	reporter := opts.Reporter

	engineID, err := s.ensureScopeResolver().ResolveEngineID(tenantID, opts)
	if err != nil {
		return nil, err
	}

	// 获取资源
	if reporter != nil {
		reporter.Message("正在加载资源连接信息")
	}
	resource, err := s.engineService.GetResourceByIDWithContext(ctx, engineID, tenantID)
	if err != nil {
		return nil, err
	}
	effectiveTenantID := tenantIDForResource(resource, tenantID)
	enginePlugin, err := s.engineService.ResolveScanPlugin(ctx, resource, effectiveTenantID)
	if err != nil {
		return nil, err
	}

	opts.EngineID = engineID
	scope, err := s.ResolveScanScope(effectiveTenantID, opts)
	if err != nil {
		return nil, err
	}

	startFields := append(connectionLogFields(resource),
		"scope_mode", string(scope.Mode),
		"scan_depth", scope.ScanDepth,
		"force", scope.Force,
		"source", scope.Source,
	)
	if len(scope.CatalogPaths) > 0 {
		startFields = append(startFields, "target_paths", scope.CatalogPaths)
	}
	if len(scope.RefGroups) > 0 {
		startFields = append(startFields, "ref_groups", len(scope.RefGroups))
	}
	s.log.Info("开始扫描资源", startFields...)

	if scope.Mode == scanflow.ModeItem && opts.ItemID > 0 {
		if reporter != nil {
			reporter.Message("正在刷新数据项元数据")
		}
		result, refreshErr := s.runtimes.ItemRefresh.RefreshKnownItemByIDWithPlugin(ctx, enginePlugin, resource, effectiveTenantID, opts.ItemID)
		if refreshErr != nil {
			if reporter != nil {
				reporter.Message(fmt.Sprintf("扫描失败: %v", refreshErr))
			}
			return nil, refreshErr
		}
		completedAt := time.Now()
		resp := scanflow.NewScanResponse(
			"success",
			"item refreshed",
			scanflow.ScanCounts{Items: 1, Fields: result.Fields, Extraction: result.Extraction},
			startTime,
			completedAt,
		)
		if reporter != nil {
			reporter.Message("扫描完成")
		}
		return resp, nil
	}

	result, err := s.engineCatalogScanDispatcher.Dispatch(scanflow.DispatchRequest{
		Context:      ctx,
		Resource:     resource,
		EnginePlugin: enginePlugin,
		TenantID:     effectiveTenantID,
		CatalogPaths: scope.CatalogPaths,
		RefGroups:    scope.RefGroups,
		ScanDepth:    scope.ScanDepth,
		Force:        scope.Force,
		Reporter:     reporter,
		Mode:         scanflow.DispatchManual,
	})

	if err != nil {
		if reporter != nil {
			reporter.Message(fmt.Sprintf("扫描失败: %v", err))
		}
		return nil, err
	}

	completedAt := time.Now()
	durationMs := completedAt.Sub(startTime).Milliseconds()

	finishFields := append(make([]any, 0, len(startFields)+6), startFields...)
	finishFields = append(finishFields,
		"catalog_nodes_scanned", result.CatalogNodes,
		"items_scanned", result.Items,
		"fields_scanned", result.Fields,
		"duration_ms", durationMs,
	)
	s.log.Info("资源扫描完成", finishFields...)

	if reporter != nil {
		reporter.Message("扫描完成")
	}

	return scanflow.NewScanResponse(
		"success",
		"Scan completed successfully",
		scanflow.ScanCounts{CatalogNodes: result.CatalogNodes, Items: result.Items, Fields: result.Fields, Extraction: result.Extraction},
		startTime,
		completedAt,
	), nil
}

func tenantIDForResource(resource *commonModels.Engine, fallback uint) uint {
	if resource != nil && resource.TenantID != nil && *resource.TenantID > 0 {
		return *resource.TenantID
	}
	return fallback
}
