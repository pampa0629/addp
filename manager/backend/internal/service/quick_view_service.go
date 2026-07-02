package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/rastermosaic"
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
	defaultDirectTIFFMaxBytes  = 50 * 1024 * 1024
	defaultDirectTIFFMaxPixels = 64 * 1000 * 1000
)

const (
	QuickViewStatusUnavailable = "unavailable"
	QuickViewStatusAvailable   = "available"
	QuickViewStatusGenerating  = "generating"
	QuickViewStatusFailed      = "failed"

	QuickViewSourceKindVector        = "vector"
	QuickViewSourceKindRaster        = "raster"
	QuickViewSourceKindRasterMosaic  = "raster_mosaic"
	QuickViewSourceKindModel3D       = "model_3d"
	QuickViewSourceKindGaussianSplat = "gaussian_splat"
	QuickViewSourceKindPointCloud    = "point_cloud"

	QuickViewActionSwitchQuickView             = "switch_quick_view"
	QuickViewActionBackToBasicPreview          = "back_to_basic_preview"
	QuickViewActionGenerateTileCache           = "generate_vector_tile_cache"
	QuickViewActionGenerateRasterCOG           = "generate_raster_cog"
	QuickViewActionGenerateVectorMaterialized  = "generate_vector_materialized_view"
	QuickViewActionGenerateModel3DGLB          = "generate_model_3d_glb"
	QuickViewActionGenerateGaussianSplatKSplat = "generate_gaussian_splat_ksplat"
	QuickViewActionGeneratePointCloudCOPC      = "generate_point_cloud_copc"

	QuickViewRenderSourceCachedTile          = "cached_tile"
	QuickViewRenderSourceClientCOG           = "client_cog_render"
	QuickViewRenderSourceDirectGeoJSON       = "direct_geojson"
	QuickViewRenderSourceDirectTIFF          = "direct_tiff_client"
	QuickViewRenderSourceRasterMosaic        = "raster_mosaic_tile"
	QuickViewRenderSourceRealtimeTile        = "realtime_tile"
	QuickViewRenderSourceModel3DGLB          = "model_3d_glb"
	QuickViewRenderSourceGaussianSplatKSplat = "gaussian_splat_ksplat"
	QuickViewRenderSourcePointCloudCOPC      = "point_cloud_copc"

	RealtimeTilePerformanceReady3857Target = "ready_3857_target"
	RealtimeTilePerformanceSource3857Index = "source_3857_indexed"
	RealtimeTilePerformanceSource3857      = "source_3857_unindexed"
	RealtimeTilePerformanceSourceTransform = "source_transform_path"

	RealtimeTileRecommendationVectorMaterializedView = "vector_materialized_view_generation"
	RealtimeTileRecommendationTileCacheGeneration    = "vector_tile_cache_generation"

	RealtimeTileTimeoutRetrySuppressTile = "suppress_tile"
	RealtimeTileTimeoutRetryTTL          = "ttl"

	RealtimeTileTargetKindSourceTable                            = "source_table"
	VectorMaterializedViewTargetKindExternal3857MaterializedView = "external_3857_materialized_view"
	VectorMaterializedViewStatusNotRequired                      = "not_required"

	RasterUnavailableReasonRequiresCOGGeneration = "requires_cog_generation"
	RasterUnavailableReasonRequiresManagedCOG    = "requires_managed_cog"
	RasterUnavailableReasonMissingSpatialExtent  = "missing_spatial_extent"
	RasterUnavailableReasonMissingCRS            = "missing_crs"
	RasterCRSInferenceGeographicExtent           = "geographic_extent_without_declared_crs"
	RasterUnavailableReasonClientBudgetExceeded  = "client_render_budget_exceeded"
)

type QuickViewService struct {
	repo              *repository.PreviewStateRepository
	tileCacheRepo     *repository.TileCacheRepository
	optimizationRepo  *repository.VectorMaterializedViewRepository
	rasterCOGRepo     *repository.RasterCOGRepository
	model3DRepo       *repository.Model3DGLBRepository
	gaussianSplatRepo *repository.GaussianSplatKSplatRepository
	pointCloudRepo    *repository.PointCloudCOPCRepository
	metaClient        *commonClient.MetaClient
	options           QuickViewCapabilityOptions
	spatialLoader     func(ctx context.Context, tenantID, engineID uint, schema, table string) (*SpatialMetadataResult, error)
}

func NewQuickViewService(
	db *gorm.DB,
	metaClient *commonClient.MetaClient,
) *QuickViewService {
	return &QuickViewService{
		repo:              repository.NewPreviewStateRepository(db),
		tileCacheRepo:     repository.NewTileCacheRepository(db),
		optimizationRepo:  repository.NewVectorMaterializedViewRepository(db),
		rasterCOGRepo:     repository.NewRasterCOGRepository(db),
		model3DRepo:       repository.NewModel3DGLBRepository(db),
		gaussianSplatRepo: repository.NewGaussianSplatKSplatRepository(db),
		pointCloudRepo:    repository.NewPointCloudCOPCRepository(db),
		metaClient:        metaClient,
	}
}

func (s *QuickViewService) Repository() *repository.PreviewStateRepository {
	if s == nil {
		return nil
	}
	return s.repo
}

type QuickViewCapabilityOptions struct {
	DirectGeoJSONMaxRows  int
	RealtimeTileTimeoutMS int
	DirectTIFFMaxBytes    int64
	DirectTIFFMaxPixels   int64
}

func (s *QuickViewService) SetCapabilityOptions(options QuickViewCapabilityOptions) {
	if options.DirectGeoJSONMaxRows > 0 {
		s.options.DirectGeoJSONMaxRows = options.DirectGeoJSONMaxRows
	}
	if options.RealtimeTileTimeoutMS > 0 {
		s.options.RealtimeTileTimeoutMS = options.RealtimeTileTimeoutMS
	}
	if options.DirectTIFFMaxBytes > 0 {
		s.options.DirectTIFFMaxBytes = options.DirectTIFFMaxBytes
	}
	if options.DirectTIFFMaxPixels > 0 {
		s.options.DirectTIFFMaxPixels = options.DirectTIFFMaxPixels
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
	Raster             *RasterQuickViewSource
	RasterMosaic       *RasterMosaicQuickViewSource
	Model3D            *Model3DGLBSource
	GaussianSplat      *GaussianSplatKSplatSource
	PointCloud         *PointCloudCOPCSource
	DirectGeoJSON      bool
	GeoJSONURL         string
	CanTile            bool
	RealtimeTileTarget *RealtimeTileTarget
}

type RasterQuickViewSource struct {
	Format              string
	Profile             string
	SizeBytes           int64
	Width               int64
	Height              int64
	BandCount           int64
	SourceSRID          int
	SourceCRS           string
	Extent              []float64
	ExtentSRID          int
	CRSInferred         bool
	CRSInference        string
	PreviewURL          string
	IsTiled             interface{}
	HasOverviews        interface{}
	IsCloudOptimized    interface{}
	COGCheckLevel       string
	NoData              *float64
	SampleMin           *float64
	SampleMax           *float64
	DisplayMin          *float64
	DisplayMax          *float64
	DisplayRangeMethod  string
	RecommendedAction   string
	UnavailableReason   string
	ClientMaxBytes      int64
	ClientMaxPixels     int64
	ClientReadMode      string
	ClientRenderLibrary string
}

type RasterMosaicQuickViewSource struct {
	Format         string
	ManifestRef    string
	IndexRef       string
	OverviewRef    string
	LeafCount      int64
	SourceCount    int64
	OverviewWidth  int64
	OverviewHeight int64
	SourceSRID     int
	SourceCRS      string
	Extent         []float64
	ExtentSRID     int
}

type Model3DGLBSource struct {
	Format          string
	Layout          string
	SourceSizeBytes int64
	PreviewURL      string
}

type GaussianSplatKSplatSource struct {
	Format                   string
	Layout                   string
	Representation           string
	SceneCenter              []float64
	SplatCount               int64
	SourceSizeBytes          int64
	PreviewURL               string
	HasOpacity               *bool
	HasScale                 *bool
	HasRotation              *bool
	HasSphericalHarmonics    *bool
	SHDegree                 *int
	Bounds3D                 *datatype.Bounds3D
	SampledBounds3D          *datatype.Bounds3D
	SampledBoundsMethod      string
	SampledBoundsSampleCount *int64
}

type PointCloudCOPCSource struct {
	Format          string
	Layout          string
	PointCloudKind  string
	PointCount      int64
	SourceSizeBytes int64
	PreviewURL      string
	Bounds3D        *datatype.Bounds3D
}

type RealtimeTileTarget struct {
	Schema                       string
	Table                        string
	GeomColumn                   string
	SRID                         int
	VectorMaterializedViewTarget bool
	TargetKind                   string
	PerformanceMode              string
	OptimizationRecommended      bool
	OptimizationRecommendation   string
}

type QuickViewCapability struct {
	TenantID             uint                        `json:"tenant_id"`
	ItemFingerprint      string                      `json:"item_fingerprint,omitempty"`
	Locator              string                      `json:"locator,omitempty"`
	SourceKind           string                      `json:"source_kind,omitempty"`
	SourceEngineID       uint                        `json:"source_engine_id,omitempty"`
	SourceSchema         string                      `json:"source_schema,omitempty"`
	SourceTable          string                      `json:"source_table,omitempty"`
	CanUseQuickView      bool                        `json:"can_use_quick_view"`
	CanGenerateTileCache bool                        `json:"can_generate_vector_tile_cache"`
	PreferredMode        string                      `json:"preferred_mode"`
	ViewState            commonModels.JSONMap        `json:"view_state,omitempty"`
	RecommendedMode      string                      `json:"recommended_mode"`
	ActiveMode           string                      `json:"active_mode"`
	AvailableActions     []string                    `json:"available_actions"`
	DefaultTileCacheID   *uint                       `json:"default_vector_tile_cache_id,omitempty"`
	Status               string                      `json:"status"`
	UnavailableReason    string                      `json:"unavailable_reason,omitempty"`
	RenderSource         string                      `json:"render_source,omitempty"`
	QuickView            QuickViewRenderInfo         `json:"quick_view"`
	RenderFacts          *QuickViewRenderFacts       `json:"render_facts,omitempty"`
	Raster               *QuickViewRasterInfo        `json:"raster,omitempty"`
	RasterMosaic         *QuickViewRasterMosaicInfo  `json:"raster_mosaic,omitempty"`
	Model3D              *QuickViewModel3DInfo       `json:"model_3d,omitempty"`
	GaussianSplat        *QuickViewGaussianSplatInfo `json:"gaussian_splat,omitempty"`
	PointCloud           *QuickViewPointCloudInfo    `json:"point_cloud,omitempty"`
	Optimization         *VectorMaterializedViewInfo `json:"optimization,omitempty"`
	RealtimeTile         *QuickViewRealtimeTileInfo  `json:"realtime_tile,omitempty"`
	TileCacheGeneration  TileCacheGeneration         `json:"vector_tile_cache_generation"`
	LastCheckedAt        *time.Time                  `json:"last_checked_at,omitempty"`
}

type VectorMaterializedViewInfo struct {
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
	PreviewURL      string    `json:"preview_url,omitempty"`
	Extent          []float64 `json:"extent,omitempty"`
	ExtentSRID      int       `json:"extent_srid,omitempty"`
	MinZoom         int       `json:"min_zoom,omitempty"`
	MaxZoom         int       `json:"max_zoom,omitempty"`
	GeometryColumn  string    `json:"geometry_column,omitempty"`
	SourceSRID      int       `json:"source_srid,omitempty"`
	RecordCount     int64     `json:"record_count,omitempty"`
}

type QuickViewRasterInfo struct {
	Format              string      `json:"format,omitempty"`
	Profile             string      `json:"profile,omitempty"`
	Width               int64       `json:"width,omitempty"`
	Height              int64       `json:"height,omitempty"`
	BandCount           int64       `json:"band_count,omitempty"`
	SizeBytes           int64       `json:"size_bytes,omitempty"`
	SourceSRID          int         `json:"source_srid,omitempty"`
	SourceCRS           string      `json:"source_crs,omitempty"`
	Extent              []float64   `json:"extent,omitempty"`
	ExtentSRID          int         `json:"extent_srid,omitempty"`
	CRSInferred         bool        `json:"crs_inferred,omitempty"`
	CRSInference        string      `json:"crs_inference,omitempty"`
	IsTiled             interface{} `json:"is_tiled,omitempty"`
	HasOverviews        interface{} `json:"has_overviews,omitempty"`
	IsCloudOptimized    interface{} `json:"is_cloud_optimized,omitempty"`
	COGCheckLevel       string      `json:"cog_check_level,omitempty"`
	NoData              *float64    `json:"nodata,omitempty"`
	SampleMin           *float64    `json:"sample_min,omitempty"`
	SampleMax           *float64    `json:"sample_max,omitempty"`
	DisplayMin          *float64    `json:"display_min,omitempty"`
	DisplayMax          *float64    `json:"display_max,omitempty"`
	DisplayRangeMethod  string      `json:"display_range_method,omitempty"`
	RecommendedAction   string      `json:"recommended_action,omitempty"`
	UnavailableReason   string      `json:"unavailable_reason,omitempty"`
	ClientMaxBytes      int64       `json:"client_max_bytes,omitempty"`
	ClientMaxPixels     int64       `json:"client_max_pixels,omitempty"`
	ClientReadMode      string      `json:"client_read_mode,omitempty"`
	ClientRenderLibrary string      `json:"client_render_library,omitempty"`
}

type QuickViewRasterMosaicInfo struct {
	Format         string    `json:"format,omitempty"`
	ManifestRef    string    `json:"manifest_ref,omitempty"`
	IndexRef       string    `json:"index_ref,omitempty"`
	OverviewRef    string    `json:"overview_ref,omitempty"`
	LeafCount      int64     `json:"leaf_count,omitempty"`
	SourceCount    int64     `json:"source_count,omitempty"`
	OverviewWidth  int64     `json:"overview_width,omitempty"`
	OverviewHeight int64     `json:"overview_height,omitempty"`
	SourceSRID     int       `json:"source_srid,omitempty"`
	SourceCRS      string    `json:"source_crs,omitempty"`
	Extent         []float64 `json:"extent,omitempty"`
	ExtentSRID     int       `json:"extent_srid,omitempty"`
}

type QuickViewModel3DInfo struct {
	Format          string  `json:"format,omitempty"`
	Layout          string  `json:"layout,omitempty"`
	ResultID        *uint   `json:"result_id,omitempty"`
	TaskID          *uint   `json:"task_id,omitempty"`
	LastExecutionID *string `json:"last_execution_id,omitempty"`
	FileName        string  `json:"file_name,omitempty"`
	SizeBytes       int64   `json:"size_bytes,omitempty"`
	PreviewURL      string  `json:"preview_url,omitempty"`
}

type QuickViewGaussianSplatInfo struct {
	Format                   string             `json:"format,omitempty"`
	Layout                   string             `json:"layout,omitempty"`
	Representation           string             `json:"representation,omitempty"`
	SceneCenter              []float64          `json:"scene_center,omitempty"`
	ProgressiveOrder         string             `json:"progressive_order,omitempty"`
	ResultID                 *uint              `json:"result_id,omitempty"`
	TaskID                   *uint              `json:"task_id,omitempty"`
	LastExecutionID          *string            `json:"last_execution_id,omitempty"`
	FileName                 string             `json:"file_name,omitempty"`
	SplatCount               int64              `json:"splat_count,omitempty"`
	SizeBytes                int64              `json:"size_bytes,omitempty"`
	PreviewURL               string             `json:"preview_url,omitempty"`
	HasOpacity               *bool              `json:"has_opacity,omitempty"`
	HasScale                 *bool              `json:"has_scale,omitempty"`
	HasRotation              *bool              `json:"has_rotation,omitempty"`
	HasSphericalHarmonics    *bool              `json:"has_spherical_harmonics,omitempty"`
	SHDegree                 *int               `json:"sh_degree,omitempty"`
	Bounds3D                 *datatype.Bounds3D `json:"bounds_3d,omitempty"`
	SampledBounds3D          *datatype.Bounds3D `json:"sampled_bounds_3d,omitempty"`
	SampledBoundsMethod      string             `json:"sampled_bounds_method,omitempty"`
	SampledBoundsSampleCount *int64             `json:"sampled_bounds_sample_count,omitempty"`
	UnavailableReason        string             `json:"unavailable_reason,omitempty"`
	RecommendedAction        string             `json:"recommended_action,omitempty"`
}

type QuickViewPointCloudInfo struct {
	Format            string             `json:"format,omitempty"`
	Layout            string             `json:"layout,omitempty"`
	PointCloudKind    string             `json:"point_cloud_kind,omitempty"`
	ResultID          *uint              `json:"result_id,omitempty"`
	TaskID            *uint              `json:"task_id,omitempty"`
	LastExecutionID   *string            `json:"last_execution_id,omitempty"`
	FileName          string             `json:"file_name,omitempty"`
	PointCount        int64              `json:"point_count,omitempty"`
	SizeBytes         int64              `json:"size_bytes,omitempty"`
	PreviewURL        string             `json:"preview_url,omitempty"`
	Bounds3D          *datatype.Bounds3D `json:"bounds_3d,omitempty"`
	UnavailableReason string             `json:"unavailable_reason,omitempty"`
	RecommendedAction string             `json:"recommended_action,omitempty"`
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

func (s *QuickViewService) GetPreference(ctx context.Context, identity QuickViewIdentity) (*models.PreviewState, error) {
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
	return &models.PreviewState{
		TenantID:        identity.TenantID,
		ItemFingerprint: identity.ItemFingerprint,
		Locator:         identity.Locator,
		PreferredMode:   models.PreviewModeBasicPreview,
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
	preferredMode := models.PreviewModeBasicPreview
	existing, err := s.repo.GetByIdentity(identity.TenantID, identity.ItemFingerprint, identity.Locator)
	if err != nil && !errors.Is(err, commonapi.ErrNotFound) && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if existing != nil && strings.TrimSpace(existing.PreferredMode) != "" {
		preferredMode = existing.PreferredMode
	}
	viewState := commonModels.JSONMap{}
	if existing != nil && existing.ViewState != nil {
		viewState = existing.ViewState
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
	optimizationInfo, err := s.vectorMaterializedViewInfo(ctx, identity, engineID, schema, table, spatialMeta)
	if err != nil {
		return nil, err
	}

	initialStatus := QuickViewStatusUnavailable
	initialReason := "tile cache result is not ready"
	capability := &QuickViewCapability{
		TenantID:          identity.TenantID,
		ItemFingerprint:   identity.ItemFingerprint,
		Locator:           identity.Locator,
		SourceKind:        QuickViewSourceKindVector,
		SourceEngineID:    engineID,
		SourceSchema:      strings.TrimSpace(schema),
		SourceTable:       strings.TrimSpace(table),
		PreferredMode:     preferredMode,
		ViewState:         viewState,
		RecommendedMode:   models.PreviewModeBasicPreview,
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
		capability.RecommendedMode = models.PreviewModeMapQuickView
		capability.DefaultTileCacheID = &readyTileCache.ID
		capability.QuickView = renderInfoFromTileCache(engineID, schema, table, readyTileCache, spatialMeta)
	} else if directGeoJSONAvailable(s.directGeoJSONMaxRows(), spatialMeta) {
		capability.CanUseQuickView = true
		capability.Status = QuickViewStatusAvailable
		capability.UnavailableReason = ""
		capability.RenderSource = QuickViewRenderSourceDirectGeoJSON
		capability.RecommendedMode = models.PreviewModeMapQuickView
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
	if capability.ActiveMode == models.PreviewModeMapQuickView && !capability.CanUseQuickView {
		capability.ActiveMode = models.PreviewModeBasicPreview
	}
	capability.RenderFacts = renderFactsFromSpatialMeta(spatialMeta)
	applyOptimizationRenderFacts(capability.RenderFacts, optimizationInfo)
	applyAvailableActions(capability)

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
	preferredMode := models.PreviewModeBasicPreview
	hasExistingPreference := false
	existing, err := s.repo.GetByIdentity(identity.TenantID, identity.ItemFingerprint, identity.Locator)
	if err != nil && !errors.Is(err, commonapi.ErrNotFound) && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if existing != nil && strings.TrimSpace(existing.PreferredMode) != "" {
		preferredMode = existing.PreferredMode
		hasExistingPreference = true
	}
	viewState := commonModels.JSONMap{}
	if existing != nil && existing.ViewState != nil {
		viewState = existing.ViewState
	}
	if (source.Raster != nil || source.RasterMosaic != nil || source.Model3D != nil || source.GaussianSplat != nil || source.PointCloud != nil) &&
		!hasExistingPreference &&
		!isSourceKSplat(source.GaussianSplat) &&
		!isSourceModel3DDirectPreview(source.Model3D) &&
		!isSourcePointCloudDirectPreview(source.PointCloud) {
		preferredMode = models.PreviewModeMapQuickView
	}

	readyTileCache, err := s.defaultReadyTileCache(ctx, identity)
	if err != nil {
		return nil, err
	}
	optimizationInfo, err := s.vectorMaterializedViewInfo(ctx, identity, source.EngineID, source.Schema, source.Table, source.SpatialMeta)
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
		SourceKind:        quickViewSourceKind(source),
		SourceEngineID:    source.EngineID,
		SourceSchema:      strings.TrimSpace(source.Schema),
		SourceTable:       strings.TrimSpace(source.Table),
		PreferredMode:     preferredMode,
		ViewState:         viewState,
		RecommendedMode:   models.PreviewModeBasicPreview,
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

	if source.RasterMosaic != nil {
		s.applyRasterMosaicCapability(capability, source.RasterMosaic)
	} else if source.Raster != nil {
		if err := s.applyRasterCapability(ctx, capability, identity, source.Raster, source.EngineID); err != nil {
			return nil, err
		}
	} else if source.GaussianSplat != nil {
		if err := s.applyGaussianSplatCapability(ctx, capability, identity, source.GaussianSplat, source.EngineID); err != nil {
			return nil, err
		}
	} else if source.PointCloud != nil {
		if err := s.applyPointCloudCapability(ctx, capability, identity, source.PointCloud, source.EngineID); err != nil {
			return nil, err
		}
	} else if source.Model3D != nil {
		if err := s.applyModel3DCapability(ctx, capability, identity, source.Model3D, source.EngineID); err != nil {
			return nil, err
		}
	} else if readyTileCache != nil {
		capability.CanUseQuickView = true
		capability.Status = QuickViewStatusAvailable
		capability.UnavailableReason = ""
		capability.RenderSource = QuickViewRenderSourceCachedTile
		capability.RecommendedMode = models.PreviewModeMapQuickView
		capability.DefaultTileCacheID = &readyTileCache.ID
		capability.QuickView = renderInfoFromTileCache(source.EngineID, source.Schema, source.Table, readyTileCache, source.SpatialMeta)
	} else if source.DirectGeoJSON && directGeoJSONAvailable(s.directGeoJSONMaxRows(), source.SpatialMeta) {
		capability.CanUseQuickView = true
		capability.Status = QuickViewStatusAvailable
		capability.UnavailableReason = ""
		capability.RenderSource = QuickViewRenderSourceDirectGeoJSON
		capability.RecommendedMode = models.PreviewModeMapQuickView
		capability.QuickView = renderInfoFromLocatorGeoJSON(source.SpatialMeta, source.GeoJSONURL)
		capability.RealtimeTile = s.realtimeTileInfoFromTarget(source.RealtimeTileTarget)
	} else if source.RealtimeTileTarget != nil {
		capability.CanUseQuickView = true
		capability.Status = QuickViewStatusAvailable
		capability.UnavailableReason = ""
		capability.RenderSource = QuickViewRenderSourceRealtimeTile
		capability.RecommendedMode = models.PreviewModeMapQuickView
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
	if source.Raster != nil || source.RasterMosaic != nil || source.Model3D != nil || source.GaussianSplat != nil || source.PointCloud != nil {
		capability.CanGenerateTileCache = false
		capability.TileCacheGeneration = TileCacheGeneration{
			Available: false,
			Reason:    "this quick view source does not use vector tile cache generation",
		}
	}
	if capability.ActiveMode == models.PreviewModeMapQuickView && !capability.CanUseQuickView {
		capability.ActiveMode = models.PreviewModeBasicPreview
	}
	capability.RenderFacts = renderFactsFromSpatialMeta(source.SpatialMeta)
	applyOptimizationRenderFacts(capability.RenderFacts, optimizationInfo)
	applyAvailableActions(capability)

	return capability, nil
}

func applyAvailableActions(capability *QuickViewCapability) {
	if capability == nil {
		return
	}
	actions := make([]string, 0, 4)
	add := func(action string) {
		if strings.TrimSpace(action) == "" {
			return
		}
		for _, existing := range actions {
			if existing == action {
				return
			}
		}
		actions = append(actions, action)
	}

	if capability.CanUseQuickView {
		if capability.ActiveMode == models.PreviewModeMapQuickView {
			add(QuickViewActionBackToBasicPreview)
		} else {
			add(QuickViewActionSwitchQuickView)
		}
	}
	if capability.CanGenerateTileCache {
		add(QuickViewActionGenerateTileCache)
	}
	if shouldRecommendVectorMaterializedView(capability) {
		add(QuickViewActionGenerateVectorMaterialized)
	}
	if shouldRecommendRasterCOG(capability) {
		add(QuickViewActionGenerateRasterCOG)
	}
	if capability.SourceKind == QuickViewSourceKindModel3D && !capability.CanUseQuickView && capability.UnavailableReason == "requires_glb_generation" {
		add(QuickViewActionGenerateModel3DGLB)
	}
	if capability.SourceKind == QuickViewSourceKindGaussianSplat && !capability.CanUseQuickView &&
		capability.GaussianSplat != nil &&
		capability.GaussianSplat.RecommendedAction == commonExecution.TaskTypeGaussianSplatKSplatGeneration {
		add(QuickViewActionGenerateGaussianSplatKSplat)
	}
	if capability.SourceKind == QuickViewSourceKindPointCloud && !capability.CanUseQuickView &&
		capability.PointCloud != nil &&
		capability.PointCloud.RecommendedAction == commonExecution.TaskTypePointCloudCOPCGeneration {
		add(QuickViewActionGeneratePointCloudCOPC)
	}
	capability.AvailableActions = actions
}

func shouldRecommendVectorMaterializedView(capability *QuickViewCapability) bool {
	if capability == nil || capability.RenderSource != QuickViewRenderSourceRealtimeTile {
		return false
	}
	if capability.Optimization != nil && capability.Optimization.Available {
		return false
	}
	if capability.RenderFacts != nil && capability.RenderFacts.SourceSRID == 3857 {
		return false
	}
	if capability.Optimization != nil && capability.Optimization.Status == models.VectorMaterializedViewStatusStale {
		return true
	}
	return capability.RealtimeTile != nil &&
		(capability.RealtimeTile.OptimizationRecommended ||
			capability.RealtimeTile.PerformanceMode == RealtimeTilePerformanceSourceTransform ||
			capability.RealtimeTile.TimeoutRecommendation == RealtimeTileRecommendationVectorMaterializedView)
}

func shouldRecommendRasterCOG(capability *QuickViewCapability) bool {
	if capability == nil || capability.Raster == nil {
		return false
	}
	action := strings.TrimSpace(capability.Raster.RecommendedAction)
	if capability.CanUseQuickView {
		return capability.RenderSource == QuickViewRenderSourceDirectTIFF && action == "create_cog"
	}
	switch capability.UnavailableReason {
	case RasterUnavailableReasonRequiresCOGGeneration, RasterUnavailableReasonRequiresManagedCOG, RasterUnavailableReasonClientBudgetExceeded:
		return true
	}
	return action == "create_cog" || action == "create_managed_cog"
}

func quickViewSourceKind(source QuickViewSource) string {
	switch {
	case source.RasterMosaic != nil:
		return QuickViewSourceKindRasterMosaic
	case source.Raster != nil:
		return QuickViewSourceKindRaster
	case source.Model3D != nil:
		return QuickViewSourceKindModel3D
	case source.GaussianSplat != nil:
		return QuickViewSourceKindGaussianSplat
	case source.PointCloud != nil:
		return QuickViewSourceKindPointCloud
	case source.SpatialMeta != nil || source.CanTile || source.DirectGeoJSON || source.RealtimeTileTarget != nil:
		return QuickViewSourceKindVector
	default:
		return ""
	}
}

func (s *QuickViewService) applyRasterCapability(ctx context.Context, capability *QuickViewCapability, identity QuickViewIdentity, raster *RasterQuickViewSource, engineID uint) error {
	if capability == nil || raster == nil {
		return nil
	}
	applyRasterCRSInference(raster)
	rasterInfo := rasterInfoFromSource(raster)
	capability.Raster = rasterInfo
	capability.Optimization = nil
	capability.RealtimeTile = nil
	capability.DefaultTileCacheID = nil
	capability.CanGenerateTileCache = false
	capability.TileCacheGeneration = TileCacheGeneration{
		Available: false,
		Reason:    "raster quick view does not use tile cache generation in phase 1",
	}

	readyRasterCOG, err := s.readyRasterCOG(ctx, identity, raster, engineID)
	if err != nil {
		return err
	}
	if readyRasterCOG != nil {
		capability.CanUseQuickView = true
		capability.Status = QuickViewStatusAvailable
		capability.UnavailableReason = ""
		capability.RenderSource = QuickViewRenderSourceClientCOG
		capability.RecommendedMode = models.PreviewModeMapQuickView
		capability.QuickView = renderInfoFromRasterCOG(readyRasterCOG, raster)
		capability.Raster = rasterInfoFromRasterCOG(readyRasterCOG, raster)
		return nil
	}

	reason := rasterUnavailableReason(raster, s.directTIFFMaxBytes(), s.directTIFFMaxPixels())
	if reason == "" {
		capability.CanUseQuickView = true
		capability.Status = QuickViewStatusAvailable
		capability.UnavailableReason = ""
		capability.RenderSource = QuickViewRenderSourceDirectTIFF
		capability.RecommendedMode = models.PreviewModeMapQuickView
		capability.QuickView = renderInfoFromRaster(raster)
		capability.Raster.UnavailableReason = ""
		capability.Raster.RecommendedAction = rasterDirectTIFFOptionalAction(raster)
		return nil
	}

	capability.CanUseQuickView = false
	capability.Status = QuickViewStatusUnavailable
	capability.UnavailableReason = reason
	capability.RenderSource = ""
	capability.RecommendedMode = models.PreviewModeBasicPreview
	capability.QuickView = QuickViewRenderInfo{}
	capability.Raster.UnavailableReason = reason
	if action := rasterRecommendedActionForReason(reason); action != "" {
		capability.Raster.RecommendedAction = action
		return nil
	}
	if action := rasterUnavailableOptionalAction(reason, raster); action != "" {
		capability.Raster.RecommendedAction = action
	}
	return nil
}

func (s *QuickViewService) applyRasterMosaicCapability(capability *QuickViewCapability, mosaic *RasterMosaicQuickViewSource) {
	if capability == nil || mosaic == nil {
		return
	}
	mosaicInfo := rasterMosaicInfoFromSource(mosaic)
	capability.RasterMosaic = mosaicInfo
	capability.Raster = nil
	capability.Optimization = nil
	capability.RealtimeTile = nil
	capability.DefaultTileCacheID = nil
	capability.CanGenerateTileCache = false
	capability.TileCacheGeneration = TileCacheGeneration{
		Available: false,
		Reason:    "raster mosaic quick view uses backend image tiles",
	}
	if strings.TrimSpace(mosaic.OverviewRef) == "" {
		capability.CanUseQuickView = false
		capability.Status = QuickViewStatusUnavailable
		capability.UnavailableReason = "raster mosaic overview COG is unavailable"
		capability.RenderSource = ""
		capability.RecommendedMode = models.PreviewModeBasicPreview
		capability.QuickView = QuickViewRenderInfo{}
		return
	}
	capability.CanUseQuickView = true
	capability.Status = QuickViewStatusAvailable
	capability.UnavailableReason = ""
	capability.RenderSource = QuickViewRenderSourceRasterMosaic
	capability.RecommendedMode = models.PreviewModeMapQuickView
	capability.QuickView = renderInfoFromRasterMosaic(mosaic)
}

func (s *QuickViewService) applyModel3DCapability(ctx context.Context, capability *QuickViewCapability, identity QuickViewIdentity, model3D *Model3DGLBSource, engineID uint) error {
	if capability == nil || model3D == nil {
		return nil
	}
	capability.Model3D = model3DInfoFromSource(model3D)
	capability.Raster = nil
	capability.RasterMosaic = nil
	capability.Optimization = nil
	capability.RealtimeTile = nil
	capability.DefaultTileCacheID = nil
	capability.CanGenerateTileCache = false
	capability.TileCacheGeneration = TileCacheGeneration{
		Available: false,
		Reason:    "model 3d GLB does not use vector tile cache generation",
	}

	if isSourceModel3DDirectPreview(model3D) {
		reason := "source_format_direct_preview"
		capability.CanUseQuickView = false
		capability.Status = QuickViewStatusUnavailable
		capability.UnavailableReason = reason
		capability.RenderSource = ""
		capability.RecommendedMode = models.PreviewModeBasicPreview
		capability.QuickView = QuickViewRenderInfo{}
		if capability.Model3D != nil {
			capability.Model3D.PreviewURL = strings.TrimSpace(model3D.PreviewURL)
		}
		return nil
	}

	readyGLB, err := s.readyModel3DGLB(ctx, identity, engineID)
	if err != nil {
		return err
	}
	if readyGLB != nil {
		capability.CanUseQuickView = true
		capability.Status = QuickViewStatusAvailable
		capability.UnavailableReason = ""
		capability.RenderSource = QuickViewRenderSourceModel3DGLB
		capability.RecommendedMode = models.PreviewModeMapQuickView
		capability.QuickView = renderInfoFromModel3DGLB(readyGLB)
		capability.Model3D = model3DInfoFromQuickView(readyGLB, model3D)
		return nil
	}

	capability.CanUseQuickView = false
	capability.Status = QuickViewStatusUnavailable
	capability.UnavailableReason = "requires_glb_generation"
	capability.RenderSource = ""
	capability.RecommendedMode = models.PreviewModeBasicPreview
	capability.QuickView = QuickViewRenderInfo{}
	return nil
}

func (s *QuickViewService) applyGaussianSplatCapability(ctx context.Context, capability *QuickViewCapability, identity QuickViewIdentity, gaussian *GaussianSplatKSplatSource, engineID uint) error {
	if capability == nil || gaussian == nil {
		return nil
	}
	capability.GaussianSplat = gaussianSplatInfoFromSource(gaussian)
	capability.Raster = nil
	capability.RasterMosaic = nil
	capability.Model3D = nil
	capability.Optimization = nil
	capability.RealtimeTile = nil
	capability.DefaultTileCacheID = nil
	capability.CanUseQuickView = false
	capability.Status = QuickViewStatusUnavailable
	capability.UnavailableReason = "requires_ksplat_generation"
	capability.RenderSource = ""
	capability.RecommendedMode = models.PreviewModeBasicPreview
	capability.QuickView = QuickViewRenderInfo{}
	capability.CanGenerateTileCache = false
	capability.TileCacheGeneration = TileCacheGeneration{
		Available: false,
		Reason:    "gaussian splat KSplat does not use vector tile cache generation",
	}

	if isSourceKSplat(gaussian) {
		reason := "source_format_direct_preview"
		capability.UnavailableReason = reason
		if capability.GaussianSplat != nil {
			capability.GaussianSplat.UnavailableReason = reason
			capability.GaussianSplat.RecommendedAction = ""
		}
		return nil
	}

	readyKSplat, err := s.readyGaussianSplatKSplat(ctx, identity, engineID)
	if err != nil {
		return err
	}
	if readyKSplat != nil {
		capability.CanUseQuickView = true
		capability.Status = QuickViewStatusAvailable
		capability.UnavailableReason = ""
		capability.RenderSource = QuickViewRenderSourceGaussianSplatKSplat
		capability.RecommendedMode = models.PreviewModeMapQuickView
		capability.QuickView = renderInfoFromGaussianSplatKSplat(readyKSplat)
		capability.GaussianSplat = gaussianSplatInfoFromQuickView(readyKSplat, gaussian)
		return nil
	}

	reason := "requires_ksplat_generation"
	capability.UnavailableReason = reason
	if capability.GaussianSplat != nil {
		capability.GaussianSplat.UnavailableReason = reason
		capability.GaussianSplat.RecommendedAction = commonExecution.TaskTypeGaussianSplatKSplatGeneration
	}
	return nil
}

func (s *QuickViewService) applyPointCloudCapability(ctx context.Context, capability *QuickViewCapability, identity QuickViewIdentity, pointCloud *PointCloudCOPCSource, engineID uint) error {
	if capability == nil || pointCloud == nil {
		return nil
	}
	capability.PointCloud = pointCloudInfoFromSource(pointCloud)
	capability.Raster = nil
	capability.RasterMosaic = nil
	capability.Model3D = nil
	capability.GaussianSplat = nil
	capability.Optimization = nil
	capability.RealtimeTile = nil
	capability.DefaultTileCacheID = nil
	capability.CanUseQuickView = false
	capability.Status = QuickViewStatusUnavailable
	capability.UnavailableReason = "requires_copc_generation"
	capability.RenderSource = ""
	capability.RecommendedMode = models.PreviewModeBasicPreview
	capability.QuickView = QuickViewRenderInfo{}
	capability.CanGenerateTileCache = false
	capability.TileCacheGeneration = TileCacheGeneration{
		Available: false,
		Reason:    "point cloud COPC does not use vector tile cache generation",
	}

	if isSourcePointCloudDirectPreview(pointCloud) {
		capability.CanUseQuickView = true
		capability.Status = QuickViewStatusAvailable
		capability.UnavailableReason = ""
		capability.RenderSource = QuickViewRenderSourcePointCloudCOPC
		capability.RecommendedMode = models.PreviewModeMapQuickView
		capability.QuickView = renderInfoFromPointCloudSource(pointCloud)
		if capability.PointCloud != nil {
			capability.PointCloud.PreviewURL = strings.TrimSpace(pointCloud.PreviewURL)
			capability.PointCloud.UnavailableReason = ""
			capability.PointCloud.RecommendedAction = ""
		}
		return nil
	}

	readyCOPC, err := s.readyPointCloudCOPC(ctx, identity, engineID)
	if err != nil {
		return err
	}
	if readyCOPC != nil {
		capability.CanUseQuickView = true
		capability.Status = QuickViewStatusAvailable
		capability.UnavailableReason = ""
		capability.RenderSource = QuickViewRenderSourcePointCloudCOPC
		capability.RecommendedMode = models.PreviewModeMapQuickView
		capability.QuickView = renderInfoFromPointCloudCOPC(readyCOPC)
		capability.PointCloud = pointCloudInfoFromQuickView(readyCOPC, pointCloud)
		return nil
	}

	reason := "requires_copc_generation"
	capability.UnavailableReason = reason
	if capability.PointCloud != nil {
		capability.PointCloud.UnavailableReason = reason
		capability.PointCloud.RecommendedAction = commonExecution.TaskTypePointCloudCOPCGeneration
	}
	return nil
}

func isSourceKSplat(gaussian *GaussianSplatKSplatSource) bool {
	return gaussian != nil && strings.EqualFold(strings.TrimSpace(gaussian.Format), string(format.FormatKSplat))
}

func isSourcePointCloudDirectPreview(pointCloud *PointCloudCOPCSource) bool {
	return pointCloud != nil && strings.EqualFold(strings.TrimSpace(pointCloud.Format), string(format.FormatCOPC)) &&
		strings.TrimSpace(pointCloud.PreviewURL) != ""
}

func isSourceModel3DDirectPreview(model3D *Model3DGLBSource) bool {
	if model3D == nil {
		return false
	}
	itemFormat := strings.ToLower(strings.TrimSpace(model3D.Format))
	itemLayout := strings.ToLower(strings.TrimSpace(model3D.Layout))
	switch itemFormat {
	case string(format.FormatGLB), string(format.FormatPLY):
		return itemLayout == "" || itemLayout == string(format.LayoutSingle)
	case string(format.Format3DTiles):
		return itemLayout == "" || itemLayout == string(format.LayoutWhole)
	default:
		return false
	}
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
	if preferredMode != models.PreviewModeBasicPreview && preferredMode != models.PreviewModeMapQuickView {
		return fmt.Errorf("%w: %s", ErrQuickViewInvalidPreferredMode, preferredMode)
	}
	if identity.TenantID == 0 {
		identity.TenantID = 1
	}
	identity.Locator = strings.TrimSpace(identity.Locator)
	if strings.TrimSpace(identity.ItemFingerprint) == "" {
		return fmt.Errorf("%w: item identity is missing", ErrQuickViewInvalidPreferredMode)
	}
	if preferredMode == models.PreviewModeMapQuickView {
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

func (s *QuickViewService) UpdateViewStateByIdentity(
	_ context.Context,
	identity QuickViewIdentity,
	viewState commonModels.JSONMap,
) error {
	if identity.TenantID == 0 {
		identity.TenantID = 1
	}
	identity.Locator = strings.TrimSpace(identity.Locator)
	if strings.TrimSpace(identity.ItemFingerprint) == "" {
		return fmt.Errorf("%w: item identity is missing", ErrQuickViewInvalidPreferredMode)
	}
	if viewState == nil {
		viewState = commonModels.JSONMap{}
	}
	return s.repo.UpdateViewState(identity.TenantID, identity.ItemFingerprint, identity.Locator, normalizePreviewViewStatePayload(viewState))
}

func normalizePreviewViewStatePayload(viewState commonModels.JSONMap) commonModels.JSONMap {
	if viewState == nil {
		return commonModels.JSONMap{}
	}
	normalized := commonModels.JSONMap{}
	for _, mode := range []string{"basic_preview", "quick_view"} {
		modeState := jsonObjectMap(viewState[mode])
		if len(modeState) == 0 {
			continue
		}
		normalizedMode := commonModels.JSONMap{}
		for _, renderer := range []string{"map", "scene_3d"} {
			rendererState := jsonObjectMap(modeState[renderer])
			if len(rendererState) > 0 {
				normalizedMode[renderer] = rendererState
			}
		}
		if len(normalizedMode) > 0 {
			normalized[mode] = normalizedMode
		}
	}
	return normalized
}

func jsonObjectMap(value interface{}) commonModels.JSONMap {
	switch typed := value.(type) {
	case commonModels.JSONMap:
		return typed.Clone()
	case map[string]interface{}:
		out := commonModels.JSONMap{}
		for key, val := range typed {
			out[key] = val
		}
		return out
	default:
		return commonModels.JSONMap{}
	}
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

func (s *QuickViewService) vectorMaterializedViewInfo(ctx context.Context, identity QuickViewIdentity, engineID uint, schema, table string, spatialMeta *SpatialMetadataResult) (*VectorMaterializedViewInfo, error) {
	if s.optimizationRepo == nil || strings.TrimSpace(identity.ItemFingerprint) == "" {
		return &VectorMaterializedViewInfo{
			Available: false,
			Reason:    "vector materialized view result is not ready",
		}, nil
	}
	var result *models.VectorMaterializedView
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
		return &VectorMaterializedViewInfo{
			Available: false,
			Reason:    "vector materialized view result is not ready",
		}, nil
	}
	info := &VectorMaterializedViewInfo{
		Available:            result.Status == models.VectorMaterializedViewStatusReady,
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
	if result.Status != models.VectorMaterializedViewStatusReady {
		info.Reason = "vector materialized view result is not ready"
	}
	if !vectorMaterializedViewSourceFactsMatch(result, identity, engineID, schema, table, spatialMeta) {
		if result.Status == models.VectorMaterializedViewStatusReady {
			if err := s.optimizationRepo.MarkResultStale(ctx, result.ID, result.TenantID, models.VectorMaterializedViewStaleReasonSourceFactsChanged); err != nil {
				return nil, err
			}
		}
		info.Available = false
		info.Status = models.VectorMaterializedViewStatusStale
		info.Reason = models.VectorMaterializedViewStaleReasonSourceFactsChanged
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

func (s *QuickViewService) readyRasterCOG(ctx context.Context, identity QuickViewIdentity, raster *RasterQuickViewSource, engineID uint) (*models.RasterCOG, error) {
	if s.rasterCOGRepo == nil || strings.TrimSpace(identity.ItemFingerprint) == "" {
		return nil, nil
	}
	result, err := s.rasterCOGRepo.GetLatestReadyByFingerprint(ctx, identity.TenantID, identity.ItemFingerprint)
	if err != nil || result == nil {
		return nil, err
	}
	if rasterCOGFactsMatch(result, identity, raster, engineID) {
		return result, nil
	}
	if err := s.rasterCOGRepo.MarkStale(ctx, result.ID, result.TenantID, "raster COG source facts changed"); err != nil {
		return nil, err
	}
	return nil, nil
}

func (s *QuickViewService) readyModel3DGLB(ctx context.Context, identity QuickViewIdentity, engineID uint) (*models.Model3DGLB, error) {
	if s.model3DRepo == nil || strings.TrimSpace(identity.ItemFingerprint) == "" {
		return nil, nil
	}
	result, err := s.model3DRepo.GetLatestReadyByFingerprint(ctx, identity.TenantID, identity.ItemFingerprint)
	if err != nil || result == nil {
		return nil, err
	}
	if model3DGLBFactsMatch(result, identity, engineID) {
		return result, nil
	}
	return nil, nil
}

func (s *QuickViewService) readyGaussianSplatKSplat(ctx context.Context, identity QuickViewIdentity, engineID uint) (*models.GaussianSplatKSplat, error) {
	if s.gaussianSplatRepo == nil || strings.TrimSpace(identity.ItemFingerprint) == "" {
		return nil, nil
	}
	result, err := s.gaussianSplatRepo.GetLatestReadyByFingerprint(ctx, identity.TenantID, identity.ItemFingerprint)
	if err != nil || result == nil {
		return nil, err
	}
	if gaussianSplatKSplatFactsMatch(result, identity, engineID) {
		return result, nil
	}
	return nil, nil
}

func (s *QuickViewService) readyPointCloudCOPC(ctx context.Context, identity QuickViewIdentity, engineID uint) (*models.PointCloudCOPC, error) {
	if s.pointCloudRepo == nil || strings.TrimSpace(identity.ItemFingerprint) == "" {
		return nil, nil
	}
	result, err := s.pointCloudRepo.GetLatestReadyByFingerprint(ctx, identity.TenantID, identity.ItemFingerprint)
	if err != nil || result == nil {
		return nil, err
	}
	if pointCloudCOPCFactsMatch(result, identity, engineID) {
		return result, nil
	}
	return nil, nil
}

func model3DGLBFactsMatch(result *models.Model3DGLB, identity QuickViewIdentity, engineID uint) bool {
	if result == nil || result.Status != models.Model3DGLBStatusReady {
		return false
	}
	if engineID > 0 && result.SourceEngineID > 0 && result.SourceEngineID != engineID {
		return false
	}
	if locator := strings.TrimSpace(identity.Locator); locator != "" && strings.TrimSpace(result.Locator) != "" && result.Locator != locator {
		return false
	}
	return isModel3DGLBTaskSourceFormat(result.SourceFormat)
}

func gaussianSplatKSplatFactsMatch(result *models.GaussianSplatKSplat, identity QuickViewIdentity, engineID uint) bool {
	if result == nil || result.Status != models.GaussianSplatKSplatStatusReady {
		return false
	}
	if engineID > 0 && result.SourceEngineID > 0 && result.SourceEngineID != engineID {
		return false
	}
	if locator := strings.TrimSpace(identity.Locator); locator != "" && strings.TrimSpace(result.Locator) != "" && result.Locator != locator {
		return false
	}
	return isGaussianSplatKSplatTaskSourceFormat(result.SourceFormat)
}

func pointCloudCOPCFactsMatch(result *models.PointCloudCOPC, identity QuickViewIdentity, engineID uint) bool {
	if result == nil || result.Status != models.PointCloudCOPCStatusReady {
		return false
	}
	if engineID > 0 && result.SourceEngineID > 0 && result.SourceEngineID != engineID {
		return false
	}
	if locator := strings.TrimSpace(identity.Locator); locator != "" && strings.TrimSpace(result.Locator) != "" && result.Locator != locator {
		return false
	}
	return isPointCloudCOPCTaskSourceFormat(result.SourceFormat)
}

func rasterCOGFactsMatch(result *models.RasterCOG, identity QuickViewIdentity, raster *RasterQuickViewSource, engineID uint) bool {
	if result == nil || result.Status != models.RasterCOGStatusReady {
		return false
	}
	if engineID > 0 && result.SourceEngineID > 0 && result.SourceEngineID != engineID {
		return false
	}
	if locator := strings.TrimSpace(identity.Locator); locator != "" && strings.TrimSpace(result.Locator) != "" && result.Locator != locator {
		return false
	}
	if raster == nil {
		return true
	}
	if raster.Width > 0 && result.Width > 0 && raster.Width != result.Width {
		return false
	}
	if raster.Height > 0 && result.Height > 0 && raster.Height != result.Height {
		return false
	}
	if raster.SourceSRID > 0 && result.SourceSRID > 0 && raster.SourceSRID != result.SourceSRID {
		return false
	}
	return true
}

func vectorMaterializedViewSourceFactsMatch(result *models.VectorMaterializedView, identity QuickViewIdentity, engineID uint, schema, table string, spatialMeta *SpatialMetadataResult) bool {
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

func vectorMaterializedViewCurrentFactsMatch(result *models.VectorMaterializedView, engineID uint, schema, table, geometryColumn string, sourceSRID int) bool {
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

func (s *QuickViewService) directTIFFMaxBytes() int64 {
	if s.options.DirectTIFFMaxBytes > 0 {
		return s.options.DirectTIFFMaxBytes
	}
	return defaultDirectTIFFMaxBytes
}

func (s *QuickViewService) directTIFFMaxPixels() int64 {
	if s.options.DirectTIFFMaxPixels > 0 {
		return s.options.DirectTIFFMaxPixels
	}
	return defaultDirectTIFFMaxPixels
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
		if target.VectorMaterializedViewTarget {
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
		info.TimeoutRecommendation = RealtimeTileRecommendationVectorMaterializedView
		info.TimeoutRetryPolicy = RealtimeTileTimeoutRetrySuppressTile
		if info.OptimizationRecommendation == "" {
			info.OptimizationRecommended = true
			info.OptimizationRecommendation = RealtimeTileRecommendationVectorMaterializedView
		}
	}
	return info
}

func optimizationInfoFromRealtimeTarget(info *VectorMaterializedViewInfo, target *RealtimeTileTarget) *VectorMaterializedViewInfo {
	if info != nil && info.Available {
		return info
	}
	if target == nil || target.SRID != spatial.SRIDWebMercator {
		return info
	}
	if !target.VectorMaterializedViewTarget && target.TargetKind == RealtimeTileTargetKindSourceTable {
		reason := "source geometry is already 3857; vector materialized view task is not required"
		if target.PerformanceMode == RealtimeTilePerformanceSource3857Index {
			reason = "source 3857 geometry with GiST index is already optimized"
		}
		return &VectorMaterializedViewInfo{
			Available:            false,
			Status:               VectorMaterializedViewStatusNotRequired,
			TargetKind:           target.TargetKind,
			TargetSchema:         target.Schema,
			TargetTable:          target.Table,
			TargetGeometryColumn: target.GeomColumn,
			TargetSRID:           spatial.SRIDWebMercator,
			Reason:               reason,
		}
	}
	if !target.VectorMaterializedViewTarget {
		return info
	}
	return &VectorMaterializedViewInfo{
		Available:            true,
		Status:               models.VectorMaterializedViewStatusReady,
		TargetKind:           VectorMaterializedViewTargetKindExternal3857MaterializedView,
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

func renderInfoFromRaster(raster *RasterQuickViewSource) QuickViewRenderInfo {
	info := QuickViewRenderInfo{
		RenderSource: QuickViewRenderSourceDirectTIFF,
		PreviewURL:   strings.TrimSpace(raster.PreviewURL),
		SourceSRID:   raster.SourceSRID,
	}
	if len(raster.Extent) == 4 {
		info.Extent = append([]float64(nil), raster.Extent...)
		info.ExtentSRID = raster.ExtentSRID
	}
	return info
}

func renderInfoFromRasterCOG(result *models.RasterCOG, raster *RasterQuickViewSource) QuickViewRenderInfo {
	info := QuickViewRenderInfo{
		RenderSource: QuickViewRenderSourceClientCOG,
		PreviewURL:   rasterCOGContentURL(result),
		SourceSRID:   result.SourceSRID,
	}
	if info.SourceSRID <= 0 && raster != nil {
		info.SourceSRID = raster.SourceSRID
	}
	extent, extentSRID := rasterCOGExtentForQuickView(result, raster)
	if len(extent) == 4 {
		info.Extent = extent
		info.ExtentSRID = extentSRID
	}
	return info
}

func renderInfoFromRasterMosaic(mosaic *RasterMosaicQuickViewSource) QuickViewRenderInfo {
	info := QuickViewRenderInfo{
		RenderSource: QuickViewRenderSourceRasterMosaic,
		TileFormat:   RasterMosaicTileFormatPNG,
		SourceSRID:   mosaic.SourceSRID,
		MinZoom:      0,
		MaxZoom:      18,
	}
	if len(mosaic.Extent) == 4 {
		info.Extent = append([]float64(nil), mosaic.Extent...)
		info.ExtentSRID = mosaic.ExtentSRID
	}
	return info
}

func renderInfoFromModel3DGLB(result *models.Model3DGLB) QuickViewRenderInfo {
	id := uint(0)
	if result != nil {
		id = result.ID
	}
	return QuickViewRenderInfo{
		RenderSource: QuickViewRenderSourceModel3DGLB,
		PreviewURL:   model3DGLBContentURL(id),
	}
}

func renderInfoFromGaussianSplatKSplat(result *models.GaussianSplatKSplat) QuickViewRenderInfo {
	id := uint(0)
	if result != nil {
		id = result.ID
	}
	return QuickViewRenderInfo{
		RenderSource: QuickViewRenderSourceGaussianSplatKSplat,
		PreviewURL:   gaussianSplatKSplatContentURL(id),
	}
}

func renderInfoFromPointCloudCOPC(result *models.PointCloudCOPC) QuickViewRenderInfo {
	id := uint(0)
	if result != nil {
		id = result.ID
	}
	return QuickViewRenderInfo{
		RenderSource: QuickViewRenderSourcePointCloudCOPC,
		PreviewURL:   pointCloudCOPCContentURL(id),
	}
}

func renderInfoFromPointCloudSource(source *PointCloudCOPCSource) QuickViewRenderInfo {
	info := QuickViewRenderInfo{RenderSource: QuickViewRenderSourcePointCloudCOPC}
	if source != nil {
		info.PreviewURL = strings.TrimSpace(source.PreviewURL)
	}
	return info
}

func rasterInfoFromSource(raster *RasterQuickViewSource) *QuickViewRasterInfo {
	if raster == nil {
		return nil
	}
	info := &QuickViewRasterInfo{
		Format:              strings.TrimSpace(raster.Format),
		Profile:             strings.TrimSpace(raster.Profile),
		Width:               raster.Width,
		Height:              raster.Height,
		BandCount:           raster.BandCount,
		SizeBytes:           raster.SizeBytes,
		SourceSRID:          raster.SourceSRID,
		SourceCRS:           strings.TrimSpace(raster.SourceCRS),
		ExtentSRID:          raster.ExtentSRID,
		CRSInferred:         raster.CRSInferred,
		CRSInference:        strings.TrimSpace(raster.CRSInference),
		IsTiled:             raster.IsTiled,
		HasOverviews:        raster.HasOverviews,
		IsCloudOptimized:    raster.IsCloudOptimized,
		COGCheckLevel:       strings.TrimSpace(raster.COGCheckLevel),
		NoData:              cloneFloat64Ptr(raster.NoData),
		SampleMin:           cloneFloat64Ptr(raster.SampleMin),
		SampleMax:           cloneFloat64Ptr(raster.SampleMax),
		DisplayMin:          cloneFloat64Ptr(raster.DisplayMin),
		DisplayMax:          cloneFloat64Ptr(raster.DisplayMax),
		DisplayRangeMethod:  strings.TrimSpace(raster.DisplayRangeMethod),
		RecommendedAction:   strings.TrimSpace(raster.RecommendedAction),
		UnavailableReason:   strings.TrimSpace(raster.UnavailableReason),
		ClientMaxBytes:      raster.ClientMaxBytes,
		ClientMaxPixels:     raster.ClientMaxPixels,
		ClientReadMode:      strings.TrimSpace(raster.ClientReadMode),
		ClientRenderLibrary: strings.TrimSpace(raster.ClientRenderLibrary),
	}
	if len(raster.Extent) == 4 {
		info.Extent = append([]float64(nil), raster.Extent...)
	}
	return info
}

func applyRasterCRSInference(raster *RasterQuickViewSource) {
	if raster == nil {
		return
	}
	if raster.SourceSRID > 0 || strings.TrimSpace(raster.SourceCRS) != "" {
		return
	}
	if !rasterExtentLooksGeographic(raster.Extent) {
		return
	}
	raster.SourceSRID = spatial.SRIDWGS84
	if raster.ExtentSRID <= 0 {
		raster.ExtentSRID = spatial.SRIDWGS84
	}
	raster.CRSInferred = true
	raster.CRSInference = RasterCRSInferenceGeographicExtent
}

func rasterExtentLooksGeographic(extent []float64) bool {
	if len(extent) != 4 {
		return false
	}
	minX, minY, maxX, maxY := extent[0], extent[1], extent[2], extent[3]
	for _, value := range []float64{minX, minY, maxX, maxY} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return minX >= -180 && minX <= 180 &&
		maxX >= -180 && maxX <= 180 &&
		minY >= -90 && minY <= 90 &&
		maxY >= -90 && maxY <= 90 &&
		maxX > minX &&
		maxY > minY
}

func rasterMosaicInfoFromSource(mosaic *RasterMosaicQuickViewSource) *QuickViewRasterMosaicInfo {
	if mosaic == nil {
		return nil
	}
	info := &QuickViewRasterMosaicInfo{
		Format:         strings.TrimSpace(mosaic.Format),
		ManifestRef:    strings.TrimSpace(mosaic.ManifestRef),
		IndexRef:       strings.TrimSpace(mosaic.IndexRef),
		OverviewRef:    strings.TrimSpace(mosaic.OverviewRef),
		LeafCount:      mosaic.LeafCount,
		SourceCount:    mosaic.SourceCount,
		OverviewWidth:  mosaic.OverviewWidth,
		OverviewHeight: mosaic.OverviewHeight,
		SourceSRID:     mosaic.SourceSRID,
		SourceCRS:      strings.TrimSpace(mosaic.SourceCRS),
		ExtentSRID:     mosaic.ExtentSRID,
	}
	if len(mosaic.Extent) == 4 {
		info.Extent = append([]float64(nil), mosaic.Extent...)
	}
	return info
}

func model3DInfoFromSource(model3D *Model3DGLBSource) *QuickViewModel3DInfo {
	if model3D == nil {
		return nil
	}
	return &QuickViewModel3DInfo{
		Format:     strings.TrimSpace(model3D.Format),
		Layout:     strings.TrimSpace(model3D.Layout),
		SizeBytes:  model3D.SourceSizeBytes,
		PreviewURL: strings.TrimSpace(model3D.PreviewURL),
	}
}

func gaussianSplatInfoFromSource(gaussian *GaussianSplatKSplatSource) *QuickViewGaussianSplatInfo {
	if gaussian == nil {
		return nil
	}
	return &QuickViewGaussianSplatInfo{
		Format:                   strings.TrimSpace(gaussian.Format),
		Layout:                   strings.TrimSpace(gaussian.Layout),
		Representation:           strings.TrimSpace(gaussian.Representation),
		SceneCenter:              cloneFloat64Slice(gaussian.SceneCenter),
		SplatCount:               gaussian.SplatCount,
		SizeBytes:                gaussian.SourceSizeBytes,
		PreviewURL:               strings.TrimSpace(gaussian.PreviewURL),
		HasOpacity:               cloneBoolPtr(gaussian.HasOpacity),
		HasScale:                 cloneBoolPtr(gaussian.HasScale),
		HasRotation:              cloneBoolPtr(gaussian.HasRotation),
		HasSphericalHarmonics:    cloneBoolPtr(gaussian.HasSphericalHarmonics),
		SHDegree:                 cloneIntPtr(gaussian.SHDegree),
		Bounds3D:                 datatype.NormalizeBounds3D(gaussian.Bounds3D),
		SampledBounds3D:          datatype.NormalizeBounds3D(gaussian.SampledBounds3D),
		SampledBoundsMethod:      strings.TrimSpace(gaussian.SampledBoundsMethod),
		SampledBoundsSampleCount: cloneInt64Ptr(gaussian.SampledBoundsSampleCount),
	}
}

func pointCloudInfoFromSource(pointCloud *PointCloudCOPCSource) *QuickViewPointCloudInfo {
	if pointCloud == nil {
		return nil
	}
	return &QuickViewPointCloudInfo{
		Format:         strings.TrimSpace(pointCloud.Format),
		Layout:         strings.TrimSpace(pointCloud.Layout),
		PointCloudKind: strings.TrimSpace(pointCloud.PointCloudKind),
		PointCount:     pointCloud.PointCount,
		SizeBytes:      pointCloud.SourceSizeBytes,
		PreviewURL:     strings.TrimSpace(pointCloud.PreviewURL),
		Bounds3D:       datatype.NormalizeBounds3D(pointCloud.Bounds3D),
	}
}

func gaussianSplatInfoFromQuickView(result *models.GaussianSplatKSplat, source *GaussianSplatKSplatSource) *QuickViewGaussianSplatInfo {
	info := gaussianSplatInfoFromSource(source)
	if info == nil {
		info = &QuickViewGaussianSplatInfo{Format: string(format.FormatKSplat)}
	}
	if result == nil {
		return info
	}
	info.Format = strings.TrimSpace(result.SourceFormat)
	if info.Format == "" {
		info.Format = string(format.FormatKSplat)
	}
	resultID := result.ID
	info.ResultID = &resultID
	info.TaskID = result.TaskID
	info.LastExecutionID = result.LastExecutionID
	info.FileName = strings.TrimSpace(result.FileName)
	if result.SizeBytes > 0 {
		info.SizeBytes = result.SizeBytes
	}
	info.ProgressiveOrder = gaussianSplatProgressiveOrder(result.Metadata)
	info.PreviewURL = gaussianSplatKSplatContentURL(result.ID)
	info.UnavailableReason = ""
	info.RecommendedAction = ""
	return info
}

func pointCloudInfoFromQuickView(result *models.PointCloudCOPC, source *PointCloudCOPCSource) *QuickViewPointCloudInfo {
	info := pointCloudInfoFromSource(source)
	if info == nil {
		info = &QuickViewPointCloudInfo{Format: string(format.FormatCOPC)}
	}
	if result == nil {
		return info
	}
	info.Format = strings.TrimSpace(result.SourceFormat)
	if info.Format == "" {
		info.Format = string(format.FormatCOPC)
	}
	resultID := result.ID
	info.ResultID = &resultID
	info.TaskID = result.TaskID
	info.LastExecutionID = result.LastExecutionID
	info.FileName = strings.TrimSpace(result.FileName)
	if result.SizeBytes > 0 {
		info.SizeBytes = result.SizeBytes
	}
	info.PreviewURL = pointCloudCOPCContentURL(result.ID)
	info.UnavailableReason = ""
	info.RecommendedAction = ""
	return info
}

func gaussianSplatProgressiveOrder(metadata commonModels.JSONMap) string {
	facts := commonJSON.Section(metadata, "ksplat_facts")
	if len(facts) == 0 {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(commonJSON.InterfaceString(facts["converter"])), "copy") {
		return "source_order"
	}
	if strings.TrimSpace(commonJSON.InterfaceString(facts["scene_center_source"])) != "" {
		return "center_first"
	}
	return ""
}

func model3DInfoFromQuickView(result *models.Model3DGLB, source *Model3DGLBSource) *QuickViewModel3DInfo {
	info := model3DInfoFromSource(source)
	if info == nil {
		info = &QuickViewModel3DInfo{Format: string(format.FormatOSGB)}
	}
	if result == nil {
		return info
	}
	info.Format = strings.TrimSpace(result.SourceFormat)
	if info.Format == "" {
		info.Format = string(format.FormatOSGB)
	}
	resultID := result.ID
	info.ResultID = &resultID
	info.TaskID = result.TaskID
	info.LastExecutionID = result.LastExecutionID
	info.FileName = strings.TrimSpace(result.FileName)
	if result.SizeBytes > 0 {
		info.SizeBytes = result.SizeBytes
	}
	info.PreviewURL = model3DGLBContentURL(result.ID)
	return info
}

func rasterInfoFromRasterCOG(result *models.RasterCOG, raster *RasterQuickViewSource) *QuickViewRasterInfo {
	info := rasterInfoFromSource(raster)
	if info == nil {
		info = &QuickViewRasterInfo{Format: "tiff"}
	}
	info.Profile = "cog"
	if result.Width > 0 {
		info.Width = result.Width
	}
	if result.Height > 0 {
		info.Height = result.Height
	}
	if result.BandCount > 0 {
		info.BandCount = result.BandCount
	}
	if result.SizeBytes > 0 {
		info.SizeBytes = result.SizeBytes
	}
	if result.SourceSRID > 0 {
		info.SourceSRID = result.SourceSRID
	}
	if strings.TrimSpace(result.SourceCRS) != "" {
		info.SourceCRS = strings.TrimSpace(result.SourceCRS)
	}
	if extent, extentSRID := rasterCOGExtentForQuickView(result, raster); len(extent) == 4 {
		info.Extent = extent
		info.ExtentSRID = extentSRID
	}
	info.IsCloudOptimized = true
	info.ClientReadMode = "range"
	info.ClientRenderLibrary = "geotiff.js"
	info.UnavailableReason = ""
	info.RecommendedAction = ""
	return info
}

func rasterCOGExtent(result *models.RasterCOG) ([]float64, int) {
	if result == nil || len(result.Extent) == 0 {
		return nil, 0
	}
	var extent []float64
	if err := json.Unmarshal(result.Extent, &extent); err != nil || len(extent) != 4 {
		return nil, 0
	}
	extentSRID := 0
	if result.ExtentSRID != nil {
		extentSRID = *result.ExtentSRID
	}
	return extent, extentSRID
}

func rasterCOGExtentForQuickView(result *models.RasterCOG, raster *RasterQuickViewSource) ([]float64, int) {
	extent, extentSRID := rasterCOGExtent(result)
	if len(extent) == 4 {
		if extentSRID <= 0 && raster != nil {
			extentSRID = raster.ExtentSRID
		}
		return extent, extentSRID
	}
	if raster != nil && len(raster.Extent) == 4 {
		return append([]float64(nil), raster.Extent...), raster.ExtentSRID
	}
	return nil, 0
}

func rasterCOGContentURL(result *models.RasterCOG) string {
	if result == nil || result.ID == 0 {
		return ""
	}
	return fmt.Sprintf("/api/v1/manager/raster_cog/%d/content", result.ID)
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

func rasterUnavailableReason(raster *RasterQuickViewSource, maxBytes, maxPixels int64) string {
	if raster == nil {
		return "raster quick view facts are unavailable"
	}
	if len(raster.Extent) != 4 {
		return RasterUnavailableReasonMissingSpatialExtent
	}
	if raster.SourceSRID <= 0 && strings.TrimSpace(raster.SourceCRS) == "" {
		return RasterUnavailableReasonMissingCRS
	}
	if strings.TrimSpace(raster.PreviewURL) == "" {
		return "raster preview URL is unavailable"
	}
	if maxBytes > 0 && raster.SizeBytes > maxBytes {
		return rasterCOGRequirementReason(raster)
	}
	if maxPixels > 0 {
		pixels := raster.Width * raster.Height
		if raster.Width > 0 && raster.Height > 0 && pixels > maxPixels {
			return rasterCOGRequirementReason(raster)
		}
	}
	return ""
}

func rasterCOGRequirementReason(raster *RasterQuickViewSource) string {
	if raster != nil && strings.EqualFold(strings.TrimSpace(raster.Profile), "cog") {
		return RasterUnavailableReasonRequiresManagedCOG
	}
	return RasterUnavailableReasonRequiresCOGGeneration
}

func rasterDirectTIFFOptionalAction(raster *RasterQuickViewSource) string {
	if raster == nil || strings.EqualFold(strings.TrimSpace(raster.Profile), "cog") {
		return ""
	}
	return "create_cog"
}

func rasterUnavailableOptionalAction(reason string, raster *RasterQuickViewSource) string {
	switch strings.TrimSpace(reason) {
	case RasterUnavailableReasonMissingSpatialExtent, RasterUnavailableReasonMissingCRS:
		return rasterDirectTIFFOptionalAction(raster)
	default:
		return ""
	}
}

func rasterRecommendedActionForReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case RasterUnavailableReasonRequiresCOGGeneration, RasterUnavailableReasonClientBudgetExceeded:
		return "create_cog"
	case RasterUnavailableReasonRequiresManagedCOG:
		return "create_managed_cog"
	default:
		return ""
	}
}

func (s *QuickViewService) RasterMosaicSourceForLocator(ctx context.Context, tenantID *uint, locatorURI string) (QuickViewSource, bool, error) {
	_ = ctx
	if s == nil || s.metaClient == nil {
		return QuickViewSource{}, false, nil
	}
	loc, err := resourcetree.ParseURI(strings.TrimSpace(locatorURI))
	if err != nil {
		return QuickViewSource{}, false, err
	}
	if loc.ItemID == nil || *loc.ItemID == 0 {
		return QuickViewSource{}, false, nil
	}
	if tenantID != nil {
		s.metaClient.SetTenantID(tenantID)
	}
	item, err := s.metaClient.GetItemByID(*loc.ItemID)
	if err != nil {
		return QuickViewSource{}, false, err
	}
	if item == nil {
		return QuickViewSource{}, false, nil
	}
	if tenantID != nil && item.TenantID != 0 && item.TenantID != *tenantID {
		return QuickViewSource{}, false, ErrEngineAccessDenied
	}
	if item.EngineID != loc.EngineID {
		return QuickViewSource{}, false, fmt.Errorf("locator engine_id does not match raster mosaic item")
	}
	mosaic := RasterMosaicQuickViewSourceFromAttributes(item.Attributes)
	if mosaic == nil {
		return QuickViewSource{}, false, nil
	}
	storageTenantID := item.TenantID
	if storageTenantID == 0 && tenantID != nil {
		storageTenantID = *tenantID
	}
	if storageTenantID == 0 {
		storageTenantID = 1
	}
	return QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        storageTenantID,
			Locator:         strings.TrimSpace(locatorURI),
			ItemFingerprint: commonModels.GenerateItemFingerprint(item.EngineID, item.FullName),
		},
		EngineID:      item.EngineID,
		RasterMosaic:  mosaic,
		DirectGeoJSON: false,
		CanTile:       false,
	}, true, nil
}

func RasterMosaicQuickViewSourceFromAttributes(attrs map[string]interface{}) *RasterMosaicQuickViewSource {
	if len(attrs) == 0 {
		return nil
	}
	if !strings.EqualFold(commonJSON.String(attrs, "item", "data_type"), string(datatype.Media)) ||
		!strings.EqualFold(commonJSON.String(attrs, "item", "format"), string(format.FormatRasterMosaic)) ||
		!strings.EqualFold(commonJSON.String(attrs, "item", "layout"), string(format.LayoutWhole)) {
		return nil
	}
	info := commonJSON.Section(attrs, "format_info.raster_mosaic")
	spatialInfo := commonJSON.Section(attrs, "capabilities.spatial")
	if info == nil {
		return nil
	}
	mosaic := &RasterMosaicQuickViewSource{
		Format:         string(format.FormatRasterMosaic),
		ManifestRef:    strings.TrimSpace(commonJSON.InterfaceString(info["manifest_ref"])),
		IndexRef:       strings.TrimSpace(commonJSON.InterfaceString(info["index_ref"])),
		OverviewRef:    strings.TrimSpace(commonJSON.InterfaceString(info["overview_ref"])),
		LeafCount:      commonJSON.InterfaceInt64(info["leaf_count"]),
		SourceCount:    commonJSON.InterfaceInt64(info["source_count"]),
		OverviewWidth:  commonJSON.InterfaceInt64(info["overview_width"]),
		OverviewHeight: commonJSON.InterfaceInt64(info["overview_height"]),
		SourceSRID:     int(commonJSON.InterfaceInt64(spatialInfo["srid"])),
		SourceCRS:      strings.TrimSpace(commonJSON.InterfaceString(spatialInfo["crs_ref"])),
		Extent:         commonJSON.InterfaceFloat64Slice(spatialInfo["extent"]),
		ExtentSRID:     int(commonJSON.InterfaceInt64(spatialInfo["extent_srid"])),
	}
	if mosaic.ManifestRef == "" {
		mosaic.ManifestRef = rastermosaic.ManifestFileName
	}
	if mosaic.IndexRef == "" {
		mosaic.IndexRef = rastermosaic.SourceIndexRef
	}
	if mosaic.ExtentSRID == 0 {
		mosaic.ExtentSRID = mosaic.SourceSRID
	}
	return mosaic
}

func Model3DGLBSourceFromAttributes(attrs map[string]interface{}) *Model3DGLBSource {
	if len(attrs) == 0 {
		return nil
	}
	if !strings.EqualFold(commonJSON.String(attrs, "item", "data_type"), string(datatype.Model3D)) {
		return nil
	}
	itemFormat := strings.ToLower(strings.TrimSpace(commonJSON.String(attrs, "item", "format")))
	itemLayout := strings.ToLower(strings.TrimSpace(commonJSON.String(attrs, "item", "layout")))
	if !isModel3DQuickViewSourceFormat(itemFormat, itemLayout) {
		return nil
	}
	storageInfo := commonJSON.Section(attrs, "storage")
	return &Model3DGLBSource{
		Format:          itemFormat,
		Layout:          itemLayout,
		SourceSizeBytes: commonJSON.InterfaceInt64(storageInfo["total_size"]),
	}
}

func GaussianSplatKSplatSourceFromAttributes(attrs map[string]interface{}) *GaussianSplatKSplatSource {
	if len(attrs) == 0 {
		return nil
	}
	if !strings.EqualFold(commonJSON.String(attrs, "item", "data_type"), string(datatype.GaussianSplat)) {
		return nil
	}
	itemFormat := strings.ToLower(strings.TrimSpace(commonJSON.String(attrs, "item", "format")))
	if itemFormat != string(format.FormatPLY) &&
		itemFormat != string(format.FormatSplat) &&
		itemFormat != string(format.FormatKSplat) {
		return nil
	}
	itemLayout := strings.ToLower(strings.TrimSpace(commonJSON.String(attrs, "item", "layout")))
	typeInfo := commonJSON.Section(attrs, "type_info.gaussian_splat")
	storageInfo := commonJSON.Section(attrs, "storage")
	sourceSizeBytes := commonJSON.InterfaceInt64(storageInfo["total_size"])
	if sourceSizeBytes <= 0 {
		sourceSizeBytes = commonJSON.InterfaceInt64(typeInfo["size_bytes"])
	}
	source := &GaussianSplatKSplatSource{
		Format:                   itemFormat,
		Layout:                   itemLayout,
		Representation:           strings.TrimSpace(commonJSON.InterfaceString(typeInfo["representation"])),
		SceneCenter:              validSceneCenter(commonJSON.InterfaceFloat64Slice(commonJSON.Value(attrs, "format_info."+itemFormat, "scene_center"))),
		SplatCount:               commonJSON.InterfaceInt64(typeInfo["splat_count"]),
		SourceSizeBytes:          sourceSizeBytes,
		HasOpacity:               optionalBoolPtr(typeInfo, "has_opacity"),
		HasScale:                 optionalBoolPtr(typeInfo, "has_scale"),
		HasRotation:              optionalBoolPtr(typeInfo, "has_rotation"),
		HasSphericalHarmonics:    optionalBoolPtr(typeInfo, "has_spherical_harmonics"),
		SHDegree:                 optionalIntPtr(typeInfo, "sh_degree"),
		Bounds3D:                 bounds3DFromPayload(commonJSON.Section(typeInfo, "bounds_3d")),
		SampledBounds3D:          bounds3DFromPayload(commonJSON.Section(typeInfo, "sampled_bounds_3d")),
		SampledBoundsMethod:      strings.TrimSpace(commonJSON.InterfaceString(typeInfo["sampled_bounds_method"])),
		SampledBoundsSampleCount: optionalInt64Ptr(typeInfo, "sampled_bounds_sample_count"),
	}
	if source.Representation == "" {
		source.Representation = datatype.GaussianSplatRepresentation3DGS
	}
	return source
}

func PointCloudCOPCSourceFromAttributes(attrs map[string]interface{}) *PointCloudCOPCSource {
	if len(attrs) == 0 {
		return nil
	}
	if !strings.EqualFold(commonJSON.String(attrs, "item", "data_type"), string(datatype.PointCloud)) {
		return nil
	}
	itemFormat := strings.ToLower(strings.TrimSpace(commonJSON.String(attrs, "item", "format")))
	if itemFormat != string(format.FormatLAS) &&
		itemFormat != string(format.FormatLAZ) &&
		itemFormat != string(format.FormatE57) &&
		itemFormat != string(format.FormatCOPC) {
		return nil
	}
	itemLayout := strings.ToLower(strings.TrimSpace(commonJSON.String(attrs, "item", "layout")))
	typeInfo := commonJSON.Section(attrs, "type_info.point_cloud")
	storageInfo := commonJSON.Section(attrs, "storage")
	sourceSizeBytes := commonJSON.InterfaceInt64(storageInfo["total_size"])
	if sourceSizeBytes <= 0 {
		sourceSizeBytes = commonJSON.InterfaceInt64(typeInfo["size_bytes"])
	}
	return &PointCloudCOPCSource{
		Format:          itemFormat,
		Layout:          itemLayout,
		PointCloudKind:  strings.TrimSpace(commonJSON.InterfaceString(typeInfo["point_cloud_kind"])),
		PointCount:      commonJSON.InterfaceInt64(typeInfo["point_count"]),
		SourceSizeBytes: sourceSizeBytes,
		Bounds3D:        bounds3DFromPayload(commonJSON.Section(typeInfo, "bounds_3d")),
	}
}

func optionalBoolPtr(values map[string]interface{}, key string) *bool {
	if values == nil {
		return nil
	}
	if _, ok := values[key]; !ok {
		return nil
	}
	value := commonJSON.InterfaceBool(values[key])
	return &value
}

func optionalIntPtr(values map[string]interface{}, key string) *int {
	if values == nil {
		return nil
	}
	if _, ok := values[key]; !ok {
		return nil
	}
	value := int(commonJSON.InterfaceInt64(values[key]))
	return &value
}

func optionalInt64Ptr(values map[string]interface{}, key string) *int64 {
	if values == nil {
		return nil
	}
	if _, ok := values[key]; !ok {
		return nil
	}
	value := commonJSON.InterfaceInt64(values[key])
	return &value
}

func bounds3DFromPayload(payload map[string]interface{}) *datatype.Bounds3D {
	if len(payload) == 0 {
		return nil
	}
	var bounds datatype.Bounds3D
	if err := commonJSON.DecodeStruct(payload, &bounds); err != nil {
		return nil
	}
	return datatype.NormalizeBounds3D(&bounds)
}

func validSceneCenter(values []float64) []float64 {
	if len(values) < 3 {
		return nil
	}
	center := values[:3]
	for _, value := range center {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil
		}
	}
	return append([]float64(nil), center...)
}

func cloneFloat64Slice(values []float64) []float64 {
	if len(values) == 0 {
		return nil
	}
	return append([]float64(nil), values...)
}

func cloneBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInt64Ptr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func isModel3DQuickViewSourceFormat(itemFormat, itemLayout string) bool {
	switch itemFormat {
	case string(format.FormatGLB), string(format.FormatPLY):
		return itemLayout == "" || itemLayout == string(format.LayoutSingle)
	case string(format.Format3DTiles):
		return itemLayout == "" || itemLayout == string(format.LayoutWhole)
	case string(format.FormatOSGB):
		return itemLayout == "" || itemLayout == string(format.LayoutSingle)
	case string(format.FormatGLTF):
		return itemLayout == string(format.LayoutMulti)
	case string(format.FormatFBX):
		return itemLayout == "" || itemLayout == string(format.LayoutSingle)
	case string(format.FormatOBJ):
		return itemLayout == "" || itemLayout == string(format.LayoutSingle)
	case string(format.FormatSTL):
		return itemLayout == "" || itemLayout == string(format.LayoutSingle)
	case string(format.FormatIFC):
		return itemLayout == "" || itemLayout == string(format.LayoutSingle)
	default:
		return false
	}
}

func RasterQuickViewSourceFromAttributes(attrs map[string]interface{}, locator string, engineID uint) *RasterQuickViewSource {
	if len(attrs) == 0 {
		return nil
	}
	if !strings.EqualFold(commonJSON.String(attrs, "item", "data_type"), "media") ||
		!strings.EqualFold(commonJSON.String(attrs, "item", "format"), "tiff") {
		return nil
	}
	tiffInfo := commonJSON.Section(attrs, "format_info.tiff")
	mediaInfo := commonJSON.Section(attrs, "type_info.media")
	spatialInfo := commonJSON.Section(attrs, "capabilities.spatial")
	if tiffInfo == nil && mediaInfo == nil && spatialInfo == nil {
		return nil
	}
	raster := &RasterQuickViewSource{
		Format:              "tiff",
		Profile:             strings.TrimSpace(commonJSON.InterfaceString(tiffInfo["profile"])),
		SizeBytes:           rasterSizeBytesFromAttributes(attrs, mediaInfo),
		Width:               commonJSON.InterfaceInt64(mediaInfo["width"]),
		Height:              commonJSON.InterfaceInt64(mediaInfo["height"]),
		BandCount:           commonJSON.InterfaceInt64(mediaInfo["band_count"]),
		SourceSRID:          int(commonJSON.InterfaceInt64(spatialInfo["srid"])),
		SourceCRS:           strings.TrimSpace(commonJSON.InterfaceString(spatialInfo["crs_ref"])),
		Extent:              commonJSON.InterfaceFloat64Slice(spatialInfo["extent"]),
		ExtentSRID:          int(commonJSON.InterfaceInt64(spatialInfo["extent_srid"])),
		IsTiled:             tiffInfo["is_tiled"],
		HasOverviews:        tiffInfo["has_overviews"],
		IsCloudOptimized:    tiffInfo["is_cloud_optimized"],
		COGCheckLevel:       strings.TrimSpace(commonJSON.InterfaceString(tiffInfo["cog_check_level"])),
		NoData:              interfaceFloat64Ptr(tiffInfo["nodata"]),
		SampleMin:           interfaceFloat64Ptr(tiffInfo["sample_min"]),
		SampleMax:           interfaceFloat64Ptr(tiffInfo["sample_max"]),
		DisplayMin:          interfaceFloat64Ptr(tiffInfo["display_min"]),
		DisplayMax:          interfaceFloat64Ptr(tiffInfo["display_max"]),
		DisplayRangeMethod:  strings.TrimSpace(commonJSON.InterfaceString(tiffInfo["display_range_method"])),
		ClientReadMode:      "full_file",
		ClientRenderLibrary: "geotiff.js",
	}
	if raster.ExtentSRID == 0 {
		raster.ExtentSRID = raster.SourceSRID
	}
	if raster.PreviewURL == "" && strings.TrimSpace(locator) != "" {
		raster.PreviewURL = rasterStorageStreamURLFromLocator(locator, engineID)
	}
	if raster.PreviewURL == "" && engineID > 0 {
		if storageRef := storageRefFromRasterAttributes(attrs); storageRef != "" {
			raster.PreviewURL = rasterStorageStreamURL(engineID, storageRef)
		}
	}
	return raster
}

func cloneFloat64Ptr(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func interfaceFloat64Ptr(value interface{}) *float64 {
	var parsed float64
	switch typed := value.(type) {
	case nil:
		return nil
	case float64:
		parsed = typed
	case float32:
		parsed = float64(typed)
	case int:
		parsed = float64(typed)
	case int8:
		parsed = float64(typed)
	case int16:
		parsed = float64(typed)
	case int32:
		parsed = float64(typed)
	case int64:
		parsed = float64(typed)
	case uint:
		parsed = float64(typed)
	case uint8:
		parsed = float64(typed)
	case uint16:
		parsed = float64(typed)
	case uint32:
		parsed = float64(typed)
	case uint64:
		parsed = float64(typed)
	case string:
		value, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return nil
		}
		parsed = value
	default:
		return nil
	}
	if math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return nil
	}
	return &parsed
}

func rasterSizeBytesFromAttributes(attrs map[string]interface{}, mediaInfo map[string]interface{}) int64 {
	for _, value := range []int64{
		commonJSON.Int64(attrs, "storage", "total_size"),
		commonJSON.InterfaceInt64(mediaInfo["size_bytes"]),
		commonJSON.Int64(attrs, "item", "size_bytes"),
	} {
		if value > 0 {
			return value
		}
	}
	return 0
}

func storageRefFromRasterAttributes(attrs map[string]interface{}) string {
	for _, key := range []string{"storage_ref", "physical_path"} {
		if value := strings.TrimSpace(commonJSON.String(attrs, "storage", key)); value != "" {
			return value
		}
	}
	return ""
}

func rasterStorageStreamURLFromLocator(locator string, fallbackEngineID uint) string {
	parsed, err := resourcetree.ParseURI(strings.TrimSpace(locator))
	if err != nil || parsed == nil {
		return ""
	}
	if parsed.Type != resourcetree.TypeFile && parsed.Type != resourcetree.TypeObject {
		return ""
	}
	engineID := parsed.EngineID
	if engineID == 0 {
		engineID = fallbackEngineID
	}
	if engineID == 0 {
		return ""
	}
	return rasterStorageStreamURL(engineID, rasterStorageRefFromLocator(parsed))
}

func rasterStorageStreamURL(engineID uint, storageRef string) string {
	storageRef = strings.TrimSpace(storageRef)
	if engineID == 0 || storageRef == "" {
		return ""
	}
	values := url.Values{}
	values.Set("engine_id", fmt.Sprintf("%d", engineID))
	values.Set("storage_ref", storageRef)
	return "/api/v1/manager/storage-stream?" + values.Encode()
}

func rasterStorageRefFromLocator(loc *resourcetree.ResourceLocator) string {
	if loc == nil {
		return ""
	}
	switch loc.Type {
	case resourcetree.TypeFile, resourcetree.TypeObject:
		return strings.Join(loc.Path, "/")
	default:
		return ""
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

func applyOptimizationRenderFacts(facts *QuickViewRenderFacts, optimization *VectorMaterializedViewInfo) {
	if facts == nil || optimization == nil || !optimization.Available {
		return
	}
	if len(facts.RenderExtent) == 0 && len(optimization.RenderExtent) == 4 {
		facts.RenderExtent = append([]float64(nil), optimization.RenderExtent...)
		facts.RenderExtentSRID = optimization.RenderExtentSRID
		facts.RenderExtentSource = "vector_materialized_view_generation"
	}
	if facts.ZoomRecommendation == nil {
		facts.ZoomRecommendation = &ZoomRecommendation{
			MinZoom: 3,
			MaxZoom: 12,
			Status:  "estimated",
			Reason:  "ready vector materialized view target",
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
	return "/manager/spatial-quick-view/vector-tile-cache?" + values.Encode()
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
