package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/addp/common/events"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/extractor"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanadapter"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanresolver"
	"github.com/addp/meta/internal/scanruntime"
	"github.com/addp/meta/internal/search"
	"gorm.io/gorm"
)

// ScanService 统一扫描服务
type ScanService struct {
	db                   *gorm.DB
	repo                 *metaRepo.ScanRepository // 数据访问层
	runtimes             *scanruntime.Runtimes
	catalogDispatcher    *scanadapter.CatalogDispatcher // catalog 扫描分发主链路
	scopeResolver        *scanresolver.Resolver         // 扫描入口 scope 解析器
	metadataQueryService *MetadataQueryService          // 元数据查询服务（独立）
	engineService        *EngineService
	config               *config.Config
	log                  *slog.Logger
	indexer              *search.Indexer
	indexerService       *IndexerService              // 索引服务（独立）
	spatialService       *SpatialMetadataService      // 空间元数据服务（独立）
	scanEventPublisher   *events.ScanEventPublisher   // 扫描事件发布器
	metadataExtractor    *extractor.MetadataExtractor // 元数据提取器
	dedupService         *ScanDedupService            // 扫描去重服务（可选）
}

func NewScanService(db *gorm.DB, engineService *EngineService) *ScanService {
	if engineService == nil {
		engineService = NewEngineService(db, "", "", nil) // nil Redis client for fallback
	}

	repo := metaRepo.NewScanRepository(db)
	log := logger.With("component", "scan_service")

	// 创建独立的服务（消除循环依赖）
	indexerService := NewIndexerService(nil, log)         // indexer 稍后通过 SetIndexer 注入
	spatialService := NewSpatialMetadataService(nil, log) // config 稍后通过 SetConfig 注入
	metadataExtractor := extractor.NewMetadataExtractor(db)
	runtimes := scanruntime.NewRuntimes(db, log, repo, spatialService, indexerService)

	s := &ScanService{
		db:                db,
		repo:              repo,
		runtimes:          runtimes,
		engineService:     engineService,
		log:               log,
		metadataExtractor: metadataExtractor,
		indexerService:    indexerService,
		spatialService:    spatialService,
		scopeResolver:     scanresolver.New(db),
	}

	s.catalogDispatcher = scanadapter.NewCatalogDispatcher(
		db,
		repo,
		log,
		s.runtimes.Database,
		s.runtimes.BranchLeaf,
		s.runtimes.ContentCatalogScanner,
	)

	// 创建 MetadataQueryService（提供元数据查询接口）
	s.metadataQueryService = NewMetadataQueryService(db, spatialService, engineService, log)

	return s
}

// SetIndexer 注入搜索索引器
func (s *ScanService) SetIndexer(indexer *search.Indexer) {
	s.indexer = indexer
	// 同时注入到独立服务
	if s.indexerService != nil {
		s.indexerService.indexer = indexer
	}
}

// SetConfig 注入配置
func (s *ScanService) SetConfig(cfg *config.Config) {
	s.config = cfg
	// 同时注入到空间元数据服务
	if s.spatialService != nil {
		s.spatialService.config = cfg
	}
}

// SetScanEventPublisher 注入扫描事件发布器
func (s *ScanService) SetScanEventPublisher(publisher *events.ScanEventPublisher) {
	s.scanEventPublisher = publisher
}

// SetDedupService 注入扫描去重服务
func (s *ScanService) SetDedupService(dedupService *ScanDedupService) {
	s.dedupService = dedupService
	if s.catalogDispatcher != nil {
		s.catalogDispatcher.SetLocker(dedupService)
	}

	// 启动时清理所有残留的扫描锁（防止上次服务异常退出时的锁未清理）
	if dedupService != nil {
		s.cleanupStaleScanLocks()
	}
}

func (s *ScanService) ScanEngineWithOptions(opts scanflow.Options) (*models.ScanResponse, error) {
	startTime := time.Now()
	tenantID := opts.TenantID
	token := opts.Token
	reporter := opts.Reporter

	engineID, err := s.ensureScopeResolver().ResolveEngineID(tenantID, opts)
	if err != nil {
		return nil, err
	}

	// 获取资源
	if reporter != nil {
		reporter.Message("正在加载资源连接信息")
	}
	resource, err := s.engineService.GetResourceByID(engineID, tenantID, token)
	if err != nil {
		return nil, err
	}
	effectiveTenantID := tenantIDForResource(resource, tenantID)

	opts.EngineID = engineID
	scope, err := s.ResolveScanScope(effectiveTenantID, opts)
	if err != nil {
		return nil, err
	}

	startFields := append(connectionLogFields(resource),
		"mode", "manual",
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
		resp, refreshErr := s.refreshItem(context.Background(), engineID, effectiveTenantID, opts.ItemID, token, scope.Force)
		if refreshErr != nil {
			if reporter != nil {
				reporter.Message(fmt.Sprintf("扫描失败: %v", refreshErr))
			}
			return nil, refreshErr
		}
		if reporter != nil {
			reporter.Message("扫描完成")
		}
		return resp, nil
	}

	result, err := s.catalogDispatcher.Dispatch(scanflow.DispatchRequest{
		Resource:     resource,
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
