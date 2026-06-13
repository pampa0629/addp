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
)

type QuickViewService struct {
	repo          *repository.QuickViewRepository
	tileCacheRepo *repository.TileCacheRepository
	metaClient    *commonClient.MetaClient
	options       QuickViewCapabilityOptions
	spatialLoader func(ctx context.Context, tenantID, engineID uint, schema, table string) (*SpatialMetadataResult, error)
}

func NewQuickViewService(
	db *gorm.DB,
	metaClient *commonClient.MetaClient,
) *QuickViewService {
	return &QuickViewService{
		repo:          repository.NewQuickViewRepository(db),
		tileCacheRepo: repository.NewTileCacheRepository(db),
		metaClient:    metaClient,
	}
}

type QuickViewCapabilityOptions struct {
	DirectGeoJSONMaxRows int
}

func (s *QuickViewService) SetCapabilityOptions(options QuickViewCapabilityOptions) {
	if options.DirectGeoJSONMaxRows > 0 {
		s.options.DirectGeoJSONMaxRows = options.DirectGeoJSONMaxRows
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
	GeomColumn      string
	GeometryColumns []string
	SRID            int
	ExtentSRID      int
	PrimaryKey      string
	Extent          []float64
	RecordCount     int64
}

type QuickViewSource struct {
	Identity      QuickViewIdentity
	EngineID      uint
	Schema        string
	Table         string
	SpatialMeta   *SpatialMetadataResult
	DirectGeoJSON bool
	GeoJSONURL    string
	CanTile       bool
}

type QuickViewCapability struct {
	TenantID             uint                `json:"tenant_id"`
	ItemFingerprint      string              `json:"item_fingerprint,omitempty"`
	Locator              string              `json:"locator,omitempty"`
	CanUseQuickView      bool                `json:"can_use_quick_view"`
	CanGenerateTileCache bool                `json:"can_generate_tile_cache"`
	PreferredMode        string              `json:"preferred_mode"`
	RecommendedMode      string              `json:"recommended_mode"`
	ActiveMode           string              `json:"active_mode"`
	DefaultTileCacheID   *uint               `json:"default_tile_cache_id,omitempty"`
	Status               string              `json:"status"`
	UnavailableReason    string              `json:"unavailable_reason,omitempty"`
	RenderSource         string              `json:"render_source,omitempty"`
	QuickView            QuickViewRenderInfo `json:"quick_view"`
	TileCacheGeneration  TileCacheGeneration `json:"tile_cache_generation"`
	LastCheckedAt        *time.Time          `json:"last_checked_at,omitempty"`
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
	RecordCount     int64     `json:"record_count,omitempty"`
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

	initialStatus := QuickViewStatusUnavailable
	initialReason := "tile cache result is not ready"
	capability := &QuickViewCapability{
		TenantID:          identity.TenantID,
		ItemFingerprint:   identity.ItemFingerprint,
		Locator:           identity.Locator,
		PreferredMode:     preferredMode,
		RecommendedMode:   "table_geojson",
		ActiveMode:        preferredMode,
		Status:            initialStatus,
		UnavailableReason: initialReason,
		LastCheckedAt:     &now,
		TileCacheGeneration: TileCacheGeneration{
			Available: strings.TrimSpace(identity.ItemFingerprint) != "" || strings.TrimSpace(identity.Locator) != "",
			CreateURL: tileCacheCreateURL(engineID, schema, table),
		},
	}

	if readyTileCache != nil {
		capability.CanUseQuickView = true
		capability.Status = QuickViewStatusAvailable
		capability.UnavailableReason = ""
		capability.RenderSource = QuickViewRenderSourceCachedTile
		capability.RecommendedMode = "quick_view"
		capability.DefaultTileCacheID = &readyTileCache.ID
		capability.QuickView = renderInfoFromTileCache(engineID, schema, table, readyTileCache)
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
	} else if !spatialMetaComplete(spatialMeta) {
		capability.TileCacheGeneration.Available = false
		capability.TileCacheGeneration.Reason = "spatial metadata is incomplete"
		capability.UnavailableReason = capability.TileCacheGeneration.Reason
	} else {
		capability.CanUseQuickView = true
		capability.Status = QuickViewStatusAvailable
		capability.UnavailableReason = ""
		capability.RenderSource = QuickViewRenderSourceRealtimeTile
		capability.RecommendedMode = "quick_view"
		capability.QuickView = renderInfoFromRealtimeTile(engineID, schema, table, spatialMeta)
		capability.CanGenerateTileCache = true
	}

	if capability.CanUseQuickView {
		capability.CanGenerateTileCache = true
		capability.TileCacheGeneration.Available = true
	}
	if !capability.TileCacheGeneration.Available {
		capability.CanGenerateTileCache = false
	}
	if capability.ActiveMode == "quick_view" && !capability.CanUseQuickView {
		capability.ActiveMode = "table_geojson"
	}

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

	initialStatus := QuickViewStatusUnavailable
	initialReason := "tile cache result is not ready"
	capability := &QuickViewCapability{
		TenantID:          identity.TenantID,
		ItemFingerprint:   identity.ItemFingerprint,
		Locator:           identity.Locator,
		PreferredMode:     preferredMode,
		RecommendedMode:   "table_geojson",
		ActiveMode:        preferredMode,
		Status:            initialStatus,
		UnavailableReason: initialReason,
		LastCheckedAt:     &now,
		TileCacheGeneration: TileCacheGeneration{
			Available: source.CanTile,
			CreateURL: tileCacheCreateURL(source.EngineID, source.Schema, source.Table),
		},
	}

	if readyTileCache != nil {
		capability.CanUseQuickView = true
		capability.Status = QuickViewStatusAvailable
		capability.UnavailableReason = ""
		capability.RenderSource = QuickViewRenderSourceCachedTile
		capability.RecommendedMode = "quick_view"
		capability.DefaultTileCacheID = &readyTileCache.ID
		capability.QuickView = renderInfoFromTileCache(source.EngineID, source.Schema, source.Table, readyTileCache)
	} else if source.DirectGeoJSON && directGeoJSONAvailable(s.directGeoJSONMaxRows(), source.SpatialMeta) {
		capability.CanUseQuickView = true
		capability.Status = QuickViewStatusAvailable
		capability.UnavailableReason = ""
		capability.RenderSource = QuickViewRenderSourceDirectGeoJSON
		capability.RecommendedMode = "quick_view"
		capability.QuickView = renderInfoFromLocatorGeoJSON(source.SpatialMeta, source.GeoJSONURL)
	} else if !spatialMetaComplete(source.SpatialMeta) {
		capability.TileCacheGeneration.Available = false
		capability.TileCacheGeneration.Reason = "spatial metadata is incomplete"
		capability.UnavailableReason = capability.TileCacheGeneration.Reason
	} else if source.CanTile {
		capability.CanUseQuickView = true
		capability.Status = QuickViewStatusAvailable
		capability.UnavailableReason = ""
		capability.RenderSource = QuickViewRenderSourceRealtimeTile
		capability.RecommendedMode = "quick_view"
		capability.QuickView = renderInfoFromRealtimeTile(source.EngineID, source.Schema, source.Table, source.SpatialMeta)
		capability.CanGenerateTileCache = true
	}

	if capability.CanUseQuickView && source.CanTile {
		capability.CanGenerateTileCache = true
		capability.TileCacheGeneration.Available = true
	}
	if !capability.TileCacheGeneration.Available {
		capability.CanGenerateTileCache = false
	}
	if capability.ActiveMode == "quick_view" && !capability.CanUseQuickView {
		capability.ActiveMode = "table_geojson"
	}

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

func (s *QuickViewService) directGeoJSONMaxRows() int64 {
	if s.options.DirectGeoJSONMaxRows > 0 {
		return int64(s.options.DirectGeoJSONMaxRows)
	}
	return 2000
}

func spatialMetaComplete(meta *SpatialMetadataResult) bool {
	return meta != nil &&
		strings.TrimSpace(meta.GeomColumn) != "" &&
		meta.SRID > 0 &&
		len(meta.Extent) == 4
}

func spatialMetaDirectGeoJSONReady(meta *SpatialMetadataResult) bool {
	return meta != nil &&
		strings.TrimSpace(meta.GeomColumn) != "" &&
		meta.SRID > 0
}

func directGeoJSONAvailable(maxRows int64, meta *SpatialMetadataResult) bool {
	return spatialMetaDirectGeoJSONReady(meta) && meta.RecordCount > 0 && meta.RecordCount <= maxRows
}

func renderInfoFromTileCache(engineID uint, schema, table string, tileCache *models.TileCache) QuickViewRenderInfo {
	info := QuickViewRenderInfo{
		RenderSource:    QuickViewRenderSourceCachedTile,
		TileFormat:      tileCache.TileFormat,
		TileURLTemplate: tileURLTemplate(engineID, schema, table),
	}
	if extent, extentSRID, ok := TileCacheExtent(tileCache); ok {
		info.Extent = extent
		info.ExtentSRID = extentSRID
	}
	if minZoom, maxZoom, ok := TileCacheZoomRange(tileCache); ok {
		info.MinZoom = minZoom
		info.MaxZoom = maxZoom
	}
	return info
}

func renderInfoFromSpatialMeta(engineID uint, schema, table string, meta *SpatialMetadataResult) QuickViewRenderInfo {
	pageSize := meta.RecordCount
	if pageSize < 1 {
		pageSize = 1
	}
	minZoom, maxZoom := quickViewZoomRange(meta)
	return QuickViewRenderInfo{
		RenderSource:   QuickViewRenderSourceDirectGeoJSON,
		GeoJSONURL:     geoJSONURL(engineID, schema, table, meta.GeomColumn, pageSize),
		Extent:         meta.Extent,
		ExtentSRID:     meta.ExtentSRID,
		MinZoom:        minZoom,
		MaxZoom:        maxZoom,
		GeometryColumn: meta.GeomColumn,
		RecordCount:    meta.RecordCount,
	}
}

func renderInfoFromLocatorGeoJSON(meta *SpatialMetadataResult, geoJSONURL string) QuickViewRenderInfo {
	minZoom, maxZoom := quickViewZoomRange(meta)
	return QuickViewRenderInfo{
		RenderSource:   QuickViewRenderSourceDirectGeoJSON,
		GeoJSONURL:     geoJSONURL,
		Extent:         meta.Extent,
		ExtentSRID:     meta.ExtentSRID,
		MinZoom:        minZoom,
		MaxZoom:        maxZoom,
		GeometryColumn: meta.GeomColumn,
		RecordCount:    meta.RecordCount,
	}
}

func renderInfoFromRealtimeTile(engineID uint, schema, table string, meta *SpatialMetadataResult) QuickViewRenderInfo {
	minZoom, maxZoom := quickViewZoomRange(meta)
	return QuickViewRenderInfo{
		RenderSource:    QuickViewRenderSourceRealtimeTile,
		TileFormat:      "mvt",
		TileURLTemplate: tileURLTemplate(engineID, schema, table),
		Extent:          meta.Extent,
		ExtentSRID:      meta.ExtentSRID,
		MinZoom:         minZoom,
		MaxZoom:         maxZoom,
		GeometryColumn:  meta.GeomColumn,
		RecordCount:     meta.RecordCount,
	}
}

func quickViewZoomRange(meta *SpatialMetadataResult) (int, int) {
	minZoom := 3
	if meta != nil && len(meta.Extent) == 4 {
		minZoom = spatial.CalculateMinZoomFromExtent(meta.Extent, meta.ExtentSRID) - 2
		if minZoom < 3 {
			minZoom = 3
		}
	}
	return minZoom, 18
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

func tileCacheCreateURL(engineID uint, schema, table string) string {
	values := url.Values{}
	values.Set("tab", "tasks")
	values.Set("create", "1")
	if engineID > 0 {
		values.Set("engine_id", fmt.Sprintf("%d", engineID))
	}
	if strings.TrimSpace(schema) != "" {
		values.Set("schema", strings.TrimSpace(schema))
	}
	if strings.TrimSpace(table) != "" {
		values.Set("table", strings.TrimSpace(table))
	}
	if itemFingerprint := spatialItemFingerprint(engineID, schema, table); itemFingerprint != "" {
		values.Set("item_fingerprint", itemFingerprint)
	}
	return "/manager/tile-cache?" + values.Encode()
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
