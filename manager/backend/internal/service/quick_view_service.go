package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/common/spatial"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"gorm.io/gorm"
)

var (
	ErrQuickViewRecordNotFound         = errors.New("quick view record not found")
	ErrQuickViewInvalidPreferredMode   = errors.New("quick view invalid preferred mode")
	ErrQuickViewGeometryColumnNotFound = errors.New("quick view geometry column not found")
)

const (
	QuickViewStatusUnavailable = "unavailable"
	QuickViewStatusAvailable   = "available"
	QuickViewStatusGenerating  = "generating"
	QuickViewStatusFailed      = "failed"

	QuickViewRenderSourceCachedTile    = "cached_tile"
	QuickViewRenderSourceDirectGeoJSON = "direct_geojson"
	QuickViewRenderSourceRealtimeTile  = "realtime_tile"

	RealtimeTilePerformanceReady3857Target = "ready_3857_target"
	RealtimeTilePerformanceSource3857Index = "source_3857_indexed"
	RealtimeTilePerformanceSource3857      = "source_3857_unindexed"
	RealtimeTilePerformanceSourceTransform = "source_transform_path"

	RealtimeTileRecommendationQuickViewOptimization = "quick_view_optimization"
	RealtimeTileRecommendationTileCacheGeneration   = "tile_cache_generation"

	RealtimeTileTimeoutRetrySuppressTile = "suppress_tile"
	RealtimeTileTimeoutRetryTTL          = "ttl"

	RealtimeTileTargetKindSourceTable                           = "source_table"
	QuickViewOptimizationTargetKindExternal3857MaterializedView = "external_3857_materialized_view"
	QuickViewOptimizationStatusNotRequired                      = "not_required"
)

type QuickViewService struct {
	repo             *repository.QuickViewRepository
	tileCacheRepo    *repository.TileCacheRepository
	optimizationRepo *repository.QuickViewOptimizationRepository
	metaClient       *commonClient.MetaClient
	options          QuickViewCapabilityOptions
	spatialLoader    func(ctx context.Context, tenantID, engineID uint, schema, table string) (*SpatialMetadataResult, error)
}

func NewQuickViewService(
	db *gorm.DB,
	metaClient *commonClient.MetaClient,
) *QuickViewService {
	return &QuickViewService{
		repo:             repository.NewQuickViewRepository(db),
		tileCacheRepo:    repository.NewTileCacheRepository(db),
		optimizationRepo: repository.NewQuickViewOptimizationRepository(db),
		metaClient:       metaClient,
	}
}

func (s *QuickViewService) Repository() *repository.QuickViewRepository {
	if s == nil {
		return nil
	}
	return s.repo
}

type QuickViewCapabilityOptions struct {
	DirectGeoJSONMaxRows  int
	RealtimeTileTimeoutMS int
}

func (s *QuickViewService) SetCapabilityOptions(options QuickViewCapabilityOptions) {
	if options.DirectGeoJSONMaxRows > 0 {
		s.options.DirectGeoJSONMaxRows = options.DirectGeoJSONMaxRows
	}
	if options.RealtimeTileTimeoutMS > 0 {
		s.options.RealtimeTileTimeoutMS = options.RealtimeTileTimeoutMS
	}
}

func (s *QuickViewService) SetSpatialMetadataLoader(loader func(ctx context.Context, tenantID, engineID uint, schema, table string) (*SpatialMetadataResult, error)) {
	s.spatialLoader = loader
}

type QuickViewIdentity struct {
	TenantID        uint
	ItemFingerprint string
	Locator         string
}

type SpatialMetadataResult struct {
	GeomColumn          string
	GeometryColumns     []string
	SRID                int
	SourceCRS           string
	SourceCRSDefinition *datatype.CRSDefinition
	ExtentSRID          int
	PrimaryKey          string
	Extent              []float64
	RenderExtent        []float64
	RenderExtentSRID    int
	RenderExtentSource  string
	RecordCount         int64
}

type QuickViewSource struct {
	Identity           QuickViewIdentity
	EngineID           uint
	Schema             string
	Table              string
	SpatialMeta        *SpatialMetadataResult
	DirectGeoJSON      bool
	GeoJSONURL         string
	CanTile            bool
	RealtimeTileTarget *RealtimeTileTarget
}

type RealtimeTileTarget struct {
	Schema                      string
	Table                       string
	GeomColumn                  string
	SRID                        int
	QuickViewOptimizationTarget bool
	TargetKind                  string
	PerformanceMode             string
	OptimizationRecommended     bool
	OptimizationRecommendation  string
}

type QuickViewCapability struct {
	TenantID             uint                       `json:"tenant_id"`
	ItemFingerprint      string                     `json:"item_fingerprint,omitempty"`
	Locator              string                     `json:"locator,omitempty"`
	SourceEngineID       uint                       `json:"source_engine_id,omitempty"`
	SourceSchema         string                     `json:"source_schema,omitempty"`
	SourceTable          string                     `json:"source_table,omitempty"`
	CanUseQuickView      bool                       `json:"can_use_quick_view"`
	CanGenerateTileCache bool                       `json:"can_generate_tile_cache"`
	PreferredMode        string                     `json:"preferred_mode"`
	RecommendedMode      string                     `json:"recommended_mode"`
	ActiveMode           string                     `json:"active_mode"`
	DefaultTileCacheID   *uint                      `json:"default_tile_cache_id,omitempty"`
	Status               string                     `json:"status"`
	UnavailableReason    string                     `json:"unavailable_reason,omitempty"`
	RenderSource         string                     `json:"render_source,omitempty"`
	QuickView            QuickViewRenderInfo        `json:"quick_view"`
	RenderFacts          *QuickViewRenderFacts      `json:"render_facts,omitempty"`
	Optimization         *QuickViewOptimizationInfo `json:"optimization,omitempty"`
	RealtimeTile         *QuickViewRealtimeTileInfo `json:"realtime_tile,omitempty"`
	TileCacheGeneration  TileCacheGeneration        `json:"tile_cache_generation"`
	LastCheckedAt        *time.Time                 `json:"last_checked_at,omitempty"`
}

type QuickViewOptimizationInfo struct {
	Available            bool      `json:"available"`
	Status               string    `json:"status,omitempty"`
	ResultID             *uint     `json:"result_id,omitempty"`
	TaskID               *uint     `json:"task_id,omitempty"`
	LastExecutionID      *string   `json:"last_execution_id,omitempty"`
	TargetKind           string    `json:"target_kind,omitempty"`
	TargetSchema         string    `json:"target_schema,omitempty"`
	TargetTable          string    `json:"target_table,omitempty"`
	TargetGeometryColumn string    `json:"target_geometry_column,omitempty"`
	TargetSRID           int       `json:"target_srid,omitempty"`
	RenderExtent         []float64 `json:"render_extent,omitempty"`
	RenderExtentSRID     int       `json:"render_extent_srid,omitempty"`
	Reason               string    `json:"reason,omitempty"`
}

type QuickViewRealtimeTileInfo struct {
	Available                  bool   `json:"available"`
	PerformanceMode            string `json:"performance_mode,omitempty"`
	TimeoutBudgetMS            int    `json:"timeout_budget_ms,omitempty"`
	TimeoutRecommendation      string `json:"timeout_recommendation,omitempty"`
	TimeoutRetryPolicy         string `json:"timeout_retry_policy,omitempty"`
	OptimizationRecommended    bool   `json:"optimization_recommended,omitempty"`
	OptimizationRecommendation string `json:"optimization_recommendation,omitempty"`
}

type QuickViewRenderInfo struct {
	RenderSource    string    `json:"render_source,omitempty"`
	TileFormat      string    `json:"tile_format,omitempty"`
	TileURLTemplate string    `json:"tile_url_template,omitempty"`
	GeoJSONURL      string    `json:"geojson_url,omitempty"`
	Extent          []float64 `json:"extent,omitempty"`
	ExtentSRID      int       `json:"extent_srid,omitempty"`
	MinZoom         int       `json:"min_zoom,omitempty"`
	MaxZoom         int       `json:"max_zoom,omitempty"`
	GeometryColumn  string    `json:"geometry_column,omitempty"`
	SourceSRID      int       `json:"source_srid,omitempty"`
	RecordCount     int64     `json:"record_count,omitempty"`
}

type QuickViewRenderFacts struct {
	SourceSRID         int                 `json:"source_srid,omitempty"`
	SourceExtent       []float64           `json:"source_extent,omitempty"`
	SourceExtentSRID   int                 `json:"source_extent_srid,omitempty"`
	RenderExtent       []float64           `json:"render_extent,omitempty"`
	RenderExtentSRID   int                 `json:"render_extent_srid,omitempty"`
	RenderExtentSource string              `json:"render_extent_source,omitempty"`
	ZoomRecommendation *ZoomRecommendation `json:"zoom_recommendation,omitempty"`
}

type ZoomRecommendation struct {
	MinZoom int    `json:"min_zoom"`
	MaxZoom int    `json:"max_zoom"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
}

type TileCacheGeneration struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	CreateURL string `json:"create_url,omitempty"`
}

func (s *QuickViewService) GetPreference(ctx context.Context, identity QuickViewIdentity) (*models.QuickView, error) {
	if identity.TenantID == 0 {
		identity.TenantID = 1
	}
	identity.Locator = strings.TrimSpace(identity.Locator)
	qv, err := s.repo.GetByIdentity(identity.TenantID, identity.ItemFingerprint, identity.Locator)
	if err == nil {
		return qv, nil
	}
	if !errors.Is(err, commonapi.ErrNotFound) && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return &models.QuickView{
		TenantID:        identity.TenantID,
		ItemFingerprint: identity.ItemFingerprint,
		Locator:         identity.Locator,
		PreferredMode:   "table_geojson",
	}, nil
}

func (s *QuickViewService) BuildCapability(ctx context.Context, identity QuickViewIdentity, engineID uint, schema, table string) (*QuickViewCapability, error) {
	if identity.TenantID == 0 {
		identity.TenantID = 1
	}
	if strings.TrimSpace(identity.Locator) == "" {
		identity.Locator = tableLocator(engineID, schema, table)
	}
	if strings.TrimSpace(identity.ItemFingerprint) == "" {
		identity.ItemFingerprint = spatialItemFingerprint(engineID, schema, table)
	}

	now := time.Now()
	preferredMode := "table_geojson"
	existing, err := s.repo.GetByIdentity(identity.TenantID, identity.ItemFingerprint, identity.Locator)
	if err != nil && !errors.Is(err, commonapi.ErrNotFound) && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if existing != nil && strings.TrimSpace(existing.PreferredMode) != "" {
		preferredMode = existing.PreferredMode
	}

	var spatialMeta *SpatialMetadataResult
	var spatialErr error
	if engineID > 0 && strings.TrimSpace(schema) != "" && strings.TrimSpace(table) != "" {
		spatialMeta, spatialErr = s.GetSpatialMetadataFromMeta(ctx, identity.TenantID, engineID, schema, table)
	}

	readyTileCache, err := s.defaultReadyTileCache(ctx, identity)
	if err != nil {
		return nil, err
	}
	optimizationInfo, err := s.quickViewOptimizationInfo(ctx, identity, engineID, schema, table, spatialMeta)
	if err != nil {
		return nil, err
	}

	initialStatus := QuickViewStatusUnavailable
	initialReason := "tile cache result is not ready"
	capability := &QuickViewCapability{
		TenantID:          identity.TenantID,
		ItemFingerprint:   identity.ItemFingerprint,
		Locator:           identity.Locator,
		SourceEngineID:    engineID,
		SourceSchema:      strings.TrimSpace(schema),
		SourceTable:       strings.TrimSpace(table),
		PreferredMode:     preferredMode,
		RecommendedMode:   "table_geojson",
		ActiveMode:        preferredMode,
		Status:            initialStatus,
		UnavailableReason: initialReason,
		LastCheckedAt:     &now,
		TileCacheGeneration: TileCacheGeneration{
			Available: strings.TrimSpace(identity.ItemFingerprint) != "" || strings.TrimSpace(identity.Locator) != "",
			CreateURL: tileCacheCreateURL(identity, engineID, schema, table, spatialMeta),
		},
	}
	capability.Optimization = optimizationInfo

	if readyTileCache != nil {
		capability.CanUseQuickView = true
		capability.Status = QuickViewStatusAvailable
		capability.UnavailableReason = ""
		capability.RenderSource = QuickViewRenderSourceCachedTile
		capability.RecommendedMode = "quick_view"
		capability.DefaultTileCacheID = &readyTileCache.ID
		capability.QuickView = renderInfoFromTileCache(engineID, schema, table, readyTileCache, spatialMeta)
	} else if directGeoJSONAvailable(s.directGeoJSONMaxRows(), spatialMeta) {
		capability.CanUseQuickView = true
		capability.Status = QuickViewStatusAvailable
		capability.UnavailableReason = ""
		capability.RenderSource = QuickViewRenderSourceDirectGeoJSON
		capability.RecommendedMode = "quick_view"
		capability.QuickView = renderInfoFromSpatialMeta(engineID, schema, table, spatialMeta)
	} else if spatialErr != nil {
		capability.TileCacheGeneration.Available = false
		capability.TileCacheGeneration.Reason = spatialErr.Error()
		capability.UnavailableReason = spatialErr.Error()
	} else if reason := quickViewUnavailableReason(spatialMeta, true, s.directGeoJSONMaxRows()); reason != "" {
		if !spatialMetaTileReady(spatialMeta) {
			capability.TileCacheGeneration.Available = false
			capability.TileCacheGeneration.Reason = tileCacheGenerationUnavailableReason(spatialMeta)
		}
		capability.UnavailableReason = reason
	}

	if capability.CanUseQuickView {
		capability.CanGenerateTileCache = true
		capability.TileCacheGeneration.Available = true
	} else if capability.TileCacheGeneration.Available && spatialMetaTileReady(spatialMeta) {
		capability.CanGenerateTileCache = true
	}
	if !capability.TileCacheGeneration.Available {
		capability.CanGenerateTileCache = false
	}
	if capability.ActiveMode == "quick_view" && !capability.CanUseQuickView {
		capability.ActiveMode = "table_geojson"
	}
	capability.RenderFacts = renderFactsFromSpatialMeta(spatialMeta)
	applyOptimizationRenderFacts(capability.RenderFacts, optimizationInfo)

	return capability, nil
}

func (s *QuickViewService) BuildCapabilityFromSource(ctx context.Context, source QuickViewSource) (*QuickViewCapability, error) {
	identity := source.Identity
	if identity.TenantID == 0 {
		identity.TenantID = 1
	}
	identity.Locator = strings.TrimSpace(identity.Locator)
	if strings.TrimSpace(identity.ItemFingerprint) == "" {
		identity.ItemFingerprint = spatialItemFingerprint(source.EngineID, source.Schema, source.Table)
	}
	if strings.TrimSpace(identity.ItemFingerprint) == "" {
		return nil, fmt.Errorf("%w: item fingerprint is required", ErrQuickViewInvalidPreferredMode)
	}
	if identity.Locator == "" {
		identity.Locator = tableLocator(source.EngineID, source.Schema, source.Table)
	}

	now := time.Now()
	preferredMode := "table_geojson"
	existing, err := s.repo.GetByIdentity(identity.TenantID, identity.ItemFingerprint, identity.Locator)
	if err != nil && !errors.Is(err, commonapi.ErrNotFound) && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if existing != nil && strings.TrimSpace(existing.PreferredMode) != "" {
		preferredMode = existing.PreferredMode
	}

	readyTileCache, err := s.defaultReadyTileCache(ctx, identity)
	if err != nil {
		return nil, err
	}
	optimizationInfo, err := s.quickViewOptimizationInfo(ctx, identity, source.EngineID, source.Schema, source.Table, source.SpatialMeta)
	if err != nil {
		return nil, err
	}
	optimizationInfo = optimizationInfoFromRealtimeTarget(optimizationInfo, source.RealtimeTileTarget)

	initialStatus := QuickViewStatusUnavailable
	initialReason := "tile cache result is not ready"
	capability := &QuickViewCapability{
		TenantID:          identity.TenantID,
		ItemFingerprint:   identity.ItemFingerprint,
		Locator:           identity.Locator,
		SourceEngineID:    source.EngineID,
		SourceSchema:      strings.TrimSpace(source.Schema),
		SourceTable:       strings.TrimSpace(source.Table),
		PreferredMode:     preferredMode,
		RecommendedMode:   "table_geojson",
		ActiveMode:        preferredMode,
		Status:            initialStatus,
		UnavailableReason: initialReason,
		LastCheckedAt:     &now,
		TileCacheGeneration: TileCacheGeneration{
			Available: source.CanTile,
			CreateURL: tileCacheCreateURL(identity, source.EngineID, source.Schema, source.Table, source.SpatialMeta),
		},
	}
	capability.Optimization = optimizationInfo

	if readyTileCache != nil {
		capability.CanUseQuickView = true
		capability.Status = QuickViewStatusAvailable
		capability.UnavailableReason = ""
		capability.RenderSource = QuickViewRenderSourceCachedTile
		capability.RecommendedMode = "quick_view"
		capability.DefaultTileCacheID = &readyTileCache.ID
		capability.QuickView = renderInfoFromTileCache(source.EngineID, source.Schema, source.Table, readyTileCache, source.SpatialMeta)
	} else if source.DirectGeoJSON && directGeoJSONAvailable(s.directGeoJSONMaxRows(), source.SpatialMeta) {
		capability.CanUseQuickView = true
		capability.Status = QuickViewStatusAvailable
		capability.UnavailableReason = ""
		capability.RenderSource = QuickViewRenderSourceDirectGeoJSON
		capability.RecommendedMode = "quick_view"
		capability.QuickView = renderInfoFromLocatorGeoJSON(source.SpatialMeta, source.GeoJSONURL)
		capability.RealtimeTile = s.realtimeTileInfoFromTarget(source.RealtimeTileTarget)
	} else if source.RealtimeTileTarget != nil {
		capability.CanUseQuickView = true
		capability.Status = QuickViewStatusAvailable
		capability.UnavailableReason = ""
		capability.RenderSource = QuickViewRenderSourceRealtimeTile
		capability.RecommendedMode = "quick_view"
		capability.QuickView = renderInfoFromRealtimeTile(source.EngineID, source.Schema, source.Table, source.SpatialMeta)
		capability.RealtimeTile = s.realtimeTileInfoFromTarget(source.RealtimeTileTarget)
		capability.CanGenerateTileCache = true
	} else if reason := quickViewUnavailableReason(source.SpatialMeta, source.DirectGeoJSON, s.directGeoJSONMaxRows()); reason != "" {
		if !spatialMetaTileReady(source.SpatialMeta) {
			capability.TileCacheGeneration.Available = false
			capability.TileCacheGeneration.Reason = tileCacheGenerationUnavailableReason(source.SpatialMeta)
		}
		capability.UnavailableReason = reason
	}

	if capability.CanUseQuickView && source.CanTile {
		capability.CanGenerateTileCache = true
		capability.TileCacheGeneration.Available = true
	} else if source.CanTile && capability.TileCacheGeneration.Available && spatialMetaTileReady(source.SpatialMeta) {
		capability.CanGenerateTileCache = true
	}
	if !capability.TileCacheGeneration.Available {
		capability.CanGenerateTileCache = false
	}
	if capability.ActiveMode == "quick_view" && !capability.CanUseQuickView {
		capability.ActiveMode = "table_geojson"
	}
	capability.RenderFacts = renderFactsFromSpatialMeta(source.SpatialMeta)
	applyOptimizationRenderFacts(capability.RenderFacts, optimizationInfo)

	return capability, nil
}

func (s *QuickViewService) GetDefaultTileCache(
	ctx context.Context,
	tenantID, engineID uint,
	schema, table string,
) (*models.TileCache, error) {
	return s.tileCacheRepo.GetLatestReadyTileCacheByFingerprint(ctx, tenantID, spatialItemFingerprint(engineID, schema, table))
}

func TileCacheExtent(tileCache *models.TileCache) ([]float64, int, bool) {
	if tileCache == nil || len(tileCache.Extent) == 0 {
		return nil, 0, false
	}
	var extent []float64
	if err := json.Unmarshal(tileCache.Extent, &extent); err != nil || len(extent) != 4 {
		return nil, 0, false
	}
	extentSRID := 4326
	if tileCache.ExtentSRID != nil && *tileCache.ExtentSRID > 0 {
		extentSRID = *tileCache.ExtentSRID
	}
	return extent, extentSRID, true
}

func TileCacheZoomRange(tileCache *models.TileCache) (int, int, bool) {
	if tileCache == nil || tileCache.MinZoom == nil || tileCache.MaxZoom == nil {
		return 0, 0, false
	}
	return *tileCache.MinZoom, *tileCache.MaxZoom, true
}

func (s *QuickViewService) UpdatePreferredModeByIdentity(
	ctx context.Context,
	identity QuickViewIdentity,
	preferredMode string,
	capabilityLoader func(context.Context) (*QuickViewCapability, error),
) error {
	if preferredMode != "table_geojson" && preferredMode != "quick_view" {
		return fmt.Errorf("%w: %s", ErrQuickViewInvalidPreferredMode, preferredMode)
	}
	if identity.TenantID == 0 {
		identity.TenantID = 1
	}
	identity.Locator = strings.TrimSpace(identity.Locator)
	if strings.TrimSpace(identity.ItemFingerprint) == "" {
		return fmt.Errorf("%w: item identity is missing", ErrQuickViewInvalidPreferredMode)
	}
	if preferredMode == "quick_view" {
		capability, err := capabilityLoader(ctx)
		if err != nil {
			return err
		}
		if capability == nil || !capability.CanUseQuickView {
			reason := ""
			if capability != nil {
				reason = capability.UnavailableReason
			}
			if reason == "" {
				reason = "quick view is unavailable"
			}
			return fmt.Errorf("%w: %s", ErrQuickViewInvalidPreferredMode, reason)
		}
	}
	return s.repo.UpdatePreferredMode(identity.TenantID, identity.ItemFingerprint, identity.Locator, preferredMode)
}

func (s *QuickViewService) GetSpatialMetadataFromMeta(
	ctx context.Context,
	tenantID, engineID uint,
	schema, table string,
) (*SpatialMetadataResult, error) {
	if s.spatialLoader != nil {
		return s.spatialLoader(ctx, tenantID, engineID, schema, table)
	}
	if s.metaClient == nil {
		return nil, fmt.Errorf("meta client not initialized, cannot query spatial metadata")
	}
	s.metaClient.SetTenantID(&tenantID)
	spatialMeta, err := s.metaClient.GetItemSpatialMetadataByCatalogPath(engineID, fmt.Sprintf("%s.%s", schema, table))
	if err != nil {
		return nil, fmt.Errorf("failed to get spatial metadata from Meta API: %w", err)
	}
	geometryColumns := []string{}
	for _, field := range spatialMeta.Fields {
		if spatial.IsPostGISSpatialType(field.NativeType) || spatial.IsPostGISSpatialType(string(field.Type)) {
			geometryColumns = append(geometryColumns, field.Name)
		}
	}
	if spatialMeta.GeometryColumn != "" && !stringSliceContains(geometryColumns, spatialMeta.GeometryColumn) {
		geometryColumns = append([]string{spatialMeta.GeometryColumn}, geometryColumns...)
	}
	return &SpatialMetadataResult{
		GeomColumn:      spatialMeta.GeometryColumn,
		GeometryColumns: geometryColumns,
		SRID:            spatialMeta.SRID,
		ExtentSRID:      spatialMeta.ExtentSRID,
		Extent:          spatialMeta.Extent,
		PrimaryKey:      spatialMeta.PrimaryKey,
		RecordCount:     spatialMeta.RowCount,
	}, nil
}

func (s *QuickViewService) defaultReadyTileCache(ctx context.Context, identity QuickViewIdentity) (*models.TileCache, error) {
	return s.tileCacheRepo.GetLatestReadyTileCacheByFingerprint(ctx, identity.TenantID, identity.ItemFingerprint)
}

func (s *QuickViewService) quickViewOptimizationInfo(ctx context.Context, identity QuickViewIdentity, engineID uint, schema, table string, spatialMeta *SpatialMetadataResult) (*QuickViewOptimizationInfo, error) {
	if s.optimizationRepo == nil || strings.TrimSpace(identity.ItemFingerprint) == "" {
		return &QuickViewOptimizationInfo{
			Available: false,
			Reason:    "quick view optimization result is not ready",
		}, nil
	}
	var result *models.QuickViewOptimization
	var err error
	geometryColumn := ""
	if spatialMeta != nil {
		geometryColumn = strings.TrimSpace(spatialMeta.GeomColumn)
	}
	if geometryColumn != "" {
		result, err = s.optimizationRepo.GetCurrentResult(ctx, identity.TenantID, identity.ItemFingerprint, geometryColumn, spatial.SRIDWebMercator)
	} else {
		result, err = s.optimizationRepo.GetLatestReadyByFingerprint(ctx, identity.TenantID, identity.ItemFingerprint)
	}
	if err != nil {
		return nil, err
	}
	if result == nil {
		return &QuickViewOptimizationInfo{
			Available: false,
			Reason:    "quick view optimization result is not ready",
		}, nil
	}
	info := &QuickViewOptimizationInfo{
		Available:            result.Status == models.QuickViewOptimizationStatusReady,
		Status:               result.Status,
		ResultID:             &result.ID,
		TaskID:               result.TaskID,
		LastExecutionID:      result.LastExecutionID,
		TargetKind:           result.TargetKind,
		TargetSchema:         result.TargetSchema,
		TargetTable:          result.TargetTable,
		TargetGeometryColumn: result.TargetGeometryColumn,
		TargetSRID:           result.TargetSRID,
	}
	if result.Status != models.QuickViewOptimizationStatusReady {
		info.Reason = "quick view optimization result is not ready"
	}
	if !quickViewOptimizationSourceFactsMatch(result, identity, engineID, schema, table, spatialMeta) {
		if result.Status == models.QuickViewOptimizationStatusReady {
			if err := s.optimizationRepo.MarkResultStale(ctx, result.ID, result.TenantID, models.QuickViewOptimizationStaleReasonSourceFactsChanged); err != nil {
				return nil, err
			}
		}
		info.Available = false
		info.Status = models.QuickViewOptimizationStatusStale
		info.Reason = models.QuickViewOptimizationStaleReasonSourceFactsChanged
	}
	if result.RenderExtentSRID != nil {
		info.RenderExtentSRID = *result.RenderExtentSRID
	}
	if len(result.RenderExtent) > 0 {
		var extent []float64
		if err := json.Unmarshal(result.RenderExtent, &extent); err == nil && len(extent) == 4 {
			info.RenderExtent = extent
		}
	}
	return info, nil
}

func quickViewOptimizationSourceFactsMatch(result *models.QuickViewOptimization, identity QuickViewIdentity, engineID uint, schema, table string, spatialMeta *SpatialMetadataResult) bool {
	if result == nil {
		return false
	}
	if result.SourceEngineID == 0 || result.SourceSchema == "" || result.SourceTable == "" {
		return false
	}
	if engineID == 0 || strings.TrimSpace(schema) == "" || strings.TrimSpace(table) == "" {
		if locator, err := resourcetree.ParseURI(identity.Locator); err == nil && locator != nil {
			engineID = locator.EngineID
			if len(locator.Path) >= 2 {
				schema = locator.Path[len(locator.Path)-2]
				table = locator.Path[len(locator.Path)-1]
			}
		}
	}
	if engineID > 0 && result.SourceEngineID != engineID {
		return false
	}
	if schema != "" && !strings.EqualFold(strings.TrimSpace(result.SourceSchema), strings.TrimSpace(schema)) {
		return false
	}
	if table != "" && !strings.EqualFold(strings.TrimSpace(result.SourceTable), strings.TrimSpace(table)) {
		return false
	}
	if spatialMeta != nil {
		if strings.TrimSpace(spatialMeta.GeomColumn) != "" && !strings.EqualFold(strings.TrimSpace(result.SourceGeometryColumn), strings.TrimSpace(spatialMeta.GeomColumn)) {
			return false
		}
		if spatialMeta.SRID > 0 && result.SourceSRID != spatialMeta.SRID {
			return false
		}
	}
	return result.TargetSRID == spatial.SRIDWebMercator
}

func quickViewOptimizationCurrentFactsMatch(result *models.QuickViewOptimization, engineID uint, schema, table, geometryColumn string, sourceSRID int) bool {
	if result == nil {
		return false
	}
	if result.SourceEngineID != engineID {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(result.SourceSchema), strings.TrimSpace(schema)) {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(result.SourceTable), strings.TrimSpace(table)) {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(result.SourceGeometryColumn), strings.TrimSpace(geometryColumn)) {
		return false
	}
	if sourceSRID > 0 && result.SourceSRID != sourceSRID {
		return false
	}
	return result.TargetSRID == spatial.SRIDWebMercator
}

func (s *QuickViewService) directGeoJSONMaxRows() int64 {
	if s.options.DirectGeoJSONMaxRows > 0 {
		return int64(s.options.DirectGeoJSONMaxRows)
	}
	return 2000
}

func (s *QuickViewService) realtimeTileTimeoutBudgetMS() int {
	if s.options.RealtimeTileTimeoutMS > 0 {
		return s.options.RealtimeTileTimeoutMS
	}
	return 5000
}

func spatialMetaComplete(meta *SpatialMetadataResult) bool {
	return spatialMetaTileReady(meta)
}

func spatialMetaTileReady(meta *SpatialMetadataResult) bool {
	return meta != nil &&
		strings.TrimSpace(meta.GeomColumn) != "" &&
		meta.SRID > 0 &&
		len(meta.Extent) == 4
}

func spatialMetaDirectGeoJSONReady(meta *SpatialMetadataResult) bool {
	return meta != nil &&
		strings.TrimSpace(meta.GeomColumn) != "" &&
		spatialMetaRenderableCRS(meta)
}

func spatialMetaRenderableCRS(meta *SpatialMetadataResult) bool {
	if meta == nil {
		return false
	}
	if meta.SRID > 0 {
		return true
	}
	return strings.TrimSpace(meta.SourceCRS) != "" && meta.SourceCRSDefinition != nil
}

func directGeoJSONAvailable(maxRows int64, meta *SpatialMetadataResult) bool {
	return spatialMetaDirectGeoJSONReady(meta) && meta.RecordCount > 0 && meta.RecordCount <= maxRows
}

func quickViewUnavailableReason(meta *SpatialMetadataResult, directGeoJSON bool, directGeoJSONMaxRows int64) string {
	if meta == nil || strings.TrimSpace(meta.GeomColumn) == "" {
		return "quick view geometry metadata is unavailable"
	}
	if directGeoJSON {
		if !spatialMetaRenderableCRS(meta) {
			return "quick view CRS is not renderable"
		}
		if meta.RecordCount <= 0 {
			return "quick view row count is unavailable"
		}
		if directGeoJSONMaxRows > 0 && meta.RecordCount > directGeoJSONMaxRows {
			return "direct GeoJSON quick view exceeds row limit"
		}
	}
	if !spatialMetaTileReady(meta) {
		return tileCacheGenerationUnavailableReason(meta)
	}
	return ""
}

func tileCacheGenerationUnavailableReason(meta *SpatialMetadataResult) string {
	if meta == nil || strings.TrimSpace(meta.GeomColumn) == "" {
		return "tile generation requires geometry metadata"
	}
	if meta.SRID <= 0 {
		return "tile generation requires numeric SRID"
	}
	if len(meta.Extent) != 4 {
		return "tile generation requires spatial extent"
	}
	return "tile generation is unavailable"
}

func (s *QuickViewService) realtimeTileInfoFromTarget(target *RealtimeTileTarget) *QuickViewRealtimeTileInfo {
	return realtimeTileInfoFromTarget(target, s.realtimeTileTimeoutBudgetMS())
}

func realtimeTileInfoFromTarget(target *RealtimeTileTarget, timeoutBudgetMS int) *QuickViewRealtimeTileInfo {
	if target == nil {
		return nil
	}
	if timeoutBudgetMS <= 0 {
		timeoutBudgetMS = 5000
	}
	mode := target.PerformanceMode
	if mode == "" {
		if target.QuickViewOptimizationTarget {
			mode = RealtimeTilePerformanceReady3857Target
		} else if target.SRID == spatial.SRIDWebMercator {
			mode = RealtimeTilePerformanceSource3857
		} else {
			mode = RealtimeTilePerformanceSourceTransform
		}
	}
	info := &QuickViewRealtimeTileInfo{
		Available:                  true,
		PerformanceMode:            mode,
		TimeoutBudgetMS:            timeoutBudgetMS,
		OptimizationRecommended:    target.OptimizationRecommended,
		OptimizationRecommendation: target.OptimizationRecommendation,
	}
	switch mode {
	case RealtimeTilePerformanceReady3857Target, RealtimeTilePerformanceSource3857Index:
		info.TimeoutRecommendation = RealtimeTileRecommendationTileCacheGeneration
		info.TimeoutRetryPolicy = RealtimeTileTimeoutRetryTTL
	case RealtimeTilePerformanceSource3857:
		info.TimeoutRecommendation = RealtimeTileRecommendationTileCacheGeneration
		info.TimeoutRetryPolicy = RealtimeTileTimeoutRetrySuppressTile
	default:
		info.TimeoutRecommendation = RealtimeTileRecommendationQuickViewOptimization
		info.TimeoutRetryPolicy = RealtimeTileTimeoutRetrySuppressTile
		if info.OptimizationRecommendation == "" {
			info.OptimizationRecommended = true
			info.OptimizationRecommendation = RealtimeTileRecommendationQuickViewOptimization
		}
	}
	return info
}

func optimizationInfoFromRealtimeTarget(info *QuickViewOptimizationInfo, target *RealtimeTileTarget) *QuickViewOptimizationInfo {
	if info != nil && info.Available {
		return info
	}
	if target == nil || target.SRID != spatial.SRIDWebMercator {
		return info
	}
	if !target.QuickViewOptimizationTarget && target.TargetKind == RealtimeTileTargetKindSourceTable {
		reason := "source geometry is already 3857; quick view optimization task is not required"
		if target.PerformanceMode == RealtimeTilePerformanceSource3857Index {
			reason = "source 3857 geometry with GiST index is already optimized"
		}
		return &QuickViewOptimizationInfo{
			Available:            false,
			Status:               QuickViewOptimizationStatusNotRequired,
			TargetKind:           target.TargetKind,
			TargetSchema:         target.Schema,
			TargetTable:          target.Table,
			TargetGeometryColumn: target.GeomColumn,
			TargetSRID:           spatial.SRIDWebMercator,
			Reason:               reason,
		}
	}
	if !target.QuickViewOptimizationTarget {
		return info
	}
	return &QuickViewOptimizationInfo{
		Available:            true,
		Status:               models.QuickViewOptimizationStatusReady,
		TargetKind:           QuickViewOptimizationTargetKindExternal3857MaterializedView,
		TargetSchema:         target.Schema,
		TargetTable:          target.Table,
		TargetGeometryColumn: target.GeomColumn,
		TargetSRID:           spatial.SRIDWebMercator,
		Reason:               "external 3857 materialized view detected",
	}
}

func renderInfoFromTileCache(engineID uint, schema, table string, tileCache *models.TileCache, spatialMeta *SpatialMetadataResult) QuickViewRenderInfo {
	info := QuickViewRenderInfo{
		RenderSource:    QuickViewRenderSourceCachedTile,
		TileFormat:      tileCache.TileFormat,
		TileURLTemplate: tileURLTemplate(engineID, schema, table),
	}
	if extent, extentSRID, ok := TileCacheExtent(tileCache); ok {
		if extentSRID == spatial.SRIDWGS84 {
			info.Extent = extent
			info.ExtentSRID = extentSRID
		}
	}
	if len(info.Extent) != 4 {
		applyRenderableWGS84Extent(&info, spatialMeta)
	}
	if minZoom, maxZoom, ok := TileCacheZoomRange(tileCache); ok {
		info.MinZoom = minZoom
		info.MaxZoom = maxZoom
	}
	if spatialMeta != nil {
		info.SourceSRID = spatialMeta.SRID
	}
	return info
}

func renderInfoFromSpatialMeta(engineID uint, schema, table string, meta *SpatialMetadataResult) QuickViewRenderInfo {
	pageSize := meta.RecordCount
	if pageSize < 1 {
		pageSize = 1
	}
	minZoom, maxZoom := quickViewZoomRange(meta)
	info := QuickViewRenderInfo{
		RenderSource:   QuickViewRenderSourceDirectGeoJSON,
		GeoJSONURL:     geoJSONURL(engineID, schema, table, meta.GeomColumn, pageSize),
		MinZoom:        minZoom,
		MaxZoom:        maxZoom,
		GeometryColumn: meta.GeomColumn,
		SourceSRID:     meta.SRID,
		RecordCount:    meta.RecordCount,
	}
	applyRenderableWGS84Extent(&info, meta)
	return info
}

func renderInfoFromLocatorGeoJSON(meta *SpatialMetadataResult, geoJSONURL string) QuickViewRenderInfo {
	minZoom, maxZoom := quickViewZoomRange(meta)
	info := QuickViewRenderInfo{
		RenderSource:   QuickViewRenderSourceDirectGeoJSON,
		GeoJSONURL:     geoJSONURL,
		MinZoom:        minZoom,
		MaxZoom:        maxZoom,
		GeometryColumn: meta.GeomColumn,
		SourceSRID:     meta.SRID,
		RecordCount:    meta.RecordCount,
	}
	applyRenderableWGS84Extent(&info, meta)
	return info
}

func renderInfoFromRealtimeTile(engineID uint, schema, table string, meta *SpatialMetadataResult) QuickViewRenderInfo {
	minZoom, maxZoom := quickViewZoomRange(meta)
	info := QuickViewRenderInfo{
		RenderSource:    QuickViewRenderSourceRealtimeTile,
		TileFormat:      "mvt",
		TileURLTemplate: tileURLTemplate(engineID, schema, table),
		MinZoom:         minZoom,
		MaxZoom:         maxZoom,
		GeometryColumn:  meta.GeomColumn,
		SourceSRID:      meta.SRID,
		RecordCount:     meta.RecordCount,
	}
	applyRenderableWGS84Extent(&info, meta)
	return info
}

func applyRenderableWGS84Extent(info *QuickViewRenderInfo, meta *SpatialMetadataResult) {
	if info == nil || meta == nil {
		return
	}
	if len(meta.RenderExtent) == 4 && meta.RenderExtentSRID == spatial.SRIDWGS84 {
		info.Extent = append([]float64(nil), meta.RenderExtent...)
		info.ExtentSRID = spatial.SRIDWGS84
		return
	}
	if len(meta.Extent) == 4 && meta.ExtentSRID == spatial.SRIDWGS84 {
		info.Extent = append([]float64(nil), meta.Extent...)
		info.ExtentSRID = spatial.SRIDWGS84
	}
}

func quickViewZoomRange(meta *SpatialMetadataResult) (int, int) {
	minZoom := 3
	maxZoom := 18
	if extent, srid, ok := zoomExtent(meta); ok {
		minZoom = spatial.CalculateMinZoomFromExtent(extent, srid)
		if minZoom < 3 {
			minZoom = 3
		}
	}
	if meta != nil {
		switch {
		case meta.RecordCount >= 1000000:
			maxZoom = 12
		case meta.RecordCount >= 100000:
			maxZoom = 14
		}
	}
	if maxZoom < minZoom {
		maxZoom = minZoom
	}
	return minZoom, maxZoom
}

func isReliableZoomExtentSRID(srid int) bool {
	return srid == spatial.SRIDWGS84 || srid == spatial.SRIDWebMercator
}

func zoomExtent(meta *SpatialMetadataResult) ([]float64, int, bool) {
	if meta == nil {
		return nil, 0, false
	}
	if len(meta.RenderExtent) == 4 && isReliableZoomExtentSRID(meta.RenderExtentSRID) {
		return meta.RenderExtent, meta.RenderExtentSRID, true
	}
	if len(meta.Extent) == 4 && isReliableZoomExtentSRID(meta.ExtentSRID) {
		return meta.Extent, meta.ExtentSRID, true
	}
	return nil, 0, false
}

func renderFactsFromSpatialMeta(meta *SpatialMetadataResult) *QuickViewRenderFacts {
	if meta == nil {
		return nil
	}
	minZoom, maxZoom := quickViewZoomRange(meta)
	status := "manual_required"
	reason := "render extent is unavailable"
	if _, _, ok := zoomExtent(meta); ok {
		status = "estimated"
		reason = "computed from render extent"
	}
	facts := &QuickViewRenderFacts{
		SourceSRID:       meta.SRID,
		SourceExtent:     append([]float64(nil), meta.Extent...),
		SourceExtentSRID: meta.ExtentSRID,
		ZoomRecommendation: &ZoomRecommendation{
			MinZoom: minZoom,
			MaxZoom: maxZoom,
			Status:  status,
			Reason:  reason,
		},
	}
	if len(meta.RenderExtent) == 4 && meta.RenderExtentSRID > 0 {
		facts.RenderExtent = append([]float64(nil), meta.RenderExtent...)
		facts.RenderExtentSRID = meta.RenderExtentSRID
		facts.RenderExtentSource = meta.RenderExtentSource
	} else if len(meta.Extent) == 4 && meta.ExtentSRID == spatial.SRIDWGS84 {
		facts.RenderExtent = append([]float64(nil), meta.Extent...)
		facts.RenderExtentSRID = spatial.SRIDWGS84
		facts.RenderExtentSource = "source_extent"
	}
	return facts
}

func applyOptimizationRenderFacts(facts *QuickViewRenderFacts, optimization *QuickViewOptimizationInfo) {
	if facts == nil || optimization == nil || !optimization.Available {
		return
	}
	if len(facts.RenderExtent) == 0 && len(optimization.RenderExtent) == 4 {
		facts.RenderExtent = append([]float64(nil), optimization.RenderExtent...)
		facts.RenderExtentSRID = optimization.RenderExtentSRID
		facts.RenderExtentSource = "quick_view_optimization"
	}
	if facts.ZoomRecommendation == nil {
		facts.ZoomRecommendation = &ZoomRecommendation{
			MinZoom: 3,
			MaxZoom: 12,
			Status:  "estimated",
			Reason:  "ready quick view optimization target",
		}
	}
}

func tileURLTemplate(engineID uint, schema, table string) string {
	locator := tableLocator(engineID, schema, table)
	if locator == "" {
		return ""
	}
	values := url.Values{}
	values.Set("locator", locator)
	return "/api/v1/manager/quick-view/tiles/{z}/{x}/{y}.mvt?" + values.Encode()
}

func geoJSONURL(engineID uint, schema, table, geomColumn string, pageSize int64) string {
	values := url.Values{}
	if locator := tableLocator(engineID, schema, table); locator != "" {
		values.Set("locator", locator)
	}
	values.Set("page", "1")
	values.Set("page_size", fmt.Sprintf("%d", pageSize))
	if strings.TrimSpace(geomColumn) != "" {
		values.Set("geometry_column", strings.TrimSpace(geomColumn))
	}
	return "/api/v1/manager/quick-view/geojson?" + values.Encode()
}

func tileCacheCreateURL(identity QuickViewIdentity, engineID uint, schema, table string, meta *SpatialMetadataResult) string {
	values := url.Values{}
	values.Set("tab", "tasks")
	values.Set("create", "1")
	locator := strings.TrimSpace(identity.Locator)
	if locator == "" {
		locator = tableLocator(engineID, schema, table)
	}
	if locator != "" {
		values.Set("locator", locator)
		if parsed, err := resourcetree.ParseURI(locator); err == nil && parsed != nil && parsed.ItemID != nil {
			values.Set("item_id", fmt.Sprintf("%d", *parsed.ItemID))
		}
	}
	itemFingerprint := strings.TrimSpace(identity.ItemFingerprint)
	if itemFingerprint == "" {
		itemFingerprint = spatialItemFingerprint(engineID, schema, table)
	}
	if itemFingerprint != "" {
		values.Set("item_fingerprint", itemFingerprint)
	}
	if meta != nil {
		if strings.TrimSpace(meta.GeomColumn) != "" {
			values.Set("geom", strings.TrimSpace(meta.GeomColumn))
		}
		if len(meta.GeometryColumns) > 0 {
			columns := make([]string, 0, len(meta.GeometryColumns))
			for _, column := range meta.GeometryColumns {
				if column = strings.TrimSpace(column); column != "" {
					columns = append(columns, column)
				}
			}
			if len(columns) > 0 {
				values.Set("geometry_columns", strings.Join(columns, ","))
			}
		}
		if meta.SRID > 0 {
			values.Set("source_srid", fmt.Sprintf("%d", meta.SRID))
		}
		extent, extentSRID := tileCacheCreateURLExtent(meta)
		if len(extent) == 4 {
			values.Set("extent", joinFloatParams(extent))
		}
		if extentSRID > 0 {
			values.Set("extent_srid", fmt.Sprintf("%d", extentSRID))
		}
	}
	return "/manager/tile-cache?" + values.Encode()
}

func tileCacheCreateURLExtent(meta *SpatialMetadataResult) ([]float64, int) {
	if meta == nil {
		return nil, 0
	}
	if len(meta.RenderExtent) == 4 && meta.RenderExtentSRID > 0 {
		return meta.RenderExtent, meta.RenderExtentSRID
	}
	if len(meta.Extent) == 4 && meta.ExtentSRID > 0 {
		return meta.Extent, meta.ExtentSRID
	}
	return nil, 0
}

func joinFloatParams(values []float64) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%g", value))
	}
	return strings.Join(parts, ",")
}

func stringSliceContains(items []string, target string) bool {
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}

func tableLocator(engineID uint, schema, table string) string {
	schema = strings.TrimSpace(schema)
	table = strings.TrimSpace(table)
	if engineID == 0 || schema == "" || table == "" {
		return ""
	}
	return (&resourcetree.ResourceLocator{
		EngineID: engineID,
		Path:     []string{schema, table},
		Type:     resourcetree.TypeTable,
	}).ToURI()
}

func spatialItemFingerprint(engineID uint, schema, table string) string {
	schema = strings.TrimSpace(schema)
	table = strings.TrimSpace(table)
	if engineID == 0 || schema == "" || table == "" {
		return ""
	}
	return commonModels.GenerateItemFingerprint(engineID, fmt.Sprintf("%s.%s", schema, table))
}
