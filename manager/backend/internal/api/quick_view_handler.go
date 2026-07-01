package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	commonSpatial "github.com/addp/common/spatial"
	manageri18n "github.com/addp/manager/i18n"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/preview"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	geomGeoJSON "github.com/twpayne/go-geom/encoding/geojson"
)

func quickViewError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrQuickViewRecordNotFound):
		managerError(c, http.StatusNotFound, manageri18n.MsgQuickViewRecordNotFound)
	case errors.Is(err, service.ErrQuickViewInvalidPreferredMode):
		managerError(c, http.StatusBadRequest, manageri18n.MsgQuickViewInvalidMode)
	case errors.Is(err, service.ErrQuickViewGeometryColumnNotFound):
		managerError(c, http.StatusBadRequest, manageri18n.MsgQuickViewGeometryMissing)
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}

func quickViewLocatorError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, preview.ErrEngineAccessDenied), errors.Is(err, service.ErrEngineAccessDenied):
		accessDeniedToEngine(c)
	case errors.Is(err, preview.ErrPreviewRequiresScannedMeta):
		managerError(c, http.StatusNotFound, manageri18n.MsgMetaScanRequired)
	default:
		quickViewError(c, err)
	}
}

type QuickViewHandler struct {
	service         *service.QuickViewService
	previewResolver *preview.PreviewResolver
	mvtService      *service.UnifiedMVTService
}

func NewQuickViewHandler(service *service.QuickViewService, previewResolver *preview.PreviewResolver, mvtService *service.UnifiedMVTService, _ *redis.Client) *QuickViewHandler {
	return &QuickViewHandler{service: service, previewResolver: previewResolver, mvtService: mvtService}
}

type UpdatePreferredModeRequest struct {
	Locator       string `json:"locator" binding:"required"`
	PreferredMode string `json:"preferred_mode" binding:"required,oneof=basic_preview map_quick_view"`
}

type UpdateQuickViewStateRequest struct {
	Locator   string               `json:"locator" binding:"required"`
	ViewState commonModels.JSONMap `json:"view_state" binding:"required"`
}

// GetQuickViewCapabilityByLocator 获取 locator 快显能力
// @Summary 获取 locator 快显能力 | Get locator quick view capability
// @Description 以 Resource Locator 为数据项身份返回快显能力状态。快显判断基于 ADDP engine、datatype、format 和 spatial capabilities，不以数据库 schema/table 为主身份。 | Return quick view capability by Resource Locator. Capability is based on ADDP engine, datatype, format, and spatial capabilities rather than database schema/table identity.
// @Tags Manager
// @Produce json
// @Param locator query string true "资源定位符URI | Resource locator URI"
// @Success 200 {object} service.QuickViewCapability "快显能力状态 | Quick view capability state"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Failure 404 {object} map[string]interface{} "资源不存在 | Resource not found"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /quick-view/capability [get]
// @Security BearerAuth
func (h *QuickViewHandler) GetQuickViewCapabilityByLocator(c *gin.Context) {
	locator := strings.TrimSpace(c.Query("locator"))
	if locator == "" {
		missingLocator(c)
		return
	}
	capability, err := h.quickViewCapabilityForLocator(c.Request.Context(), tenantIDFromContext(c), locator)
	if err != nil {
		quickViewLocatorError(c, err)
		return
	}
	c.JSON(http.StatusOK, capability)
}

// UpdatePreferredModeByLocator 更新 locator 预览模式偏好
// @Summary 更新 locator 预览模式偏好 | Update locator preferred preview mode
// @Description 以 Resource Locator 为数据项身份更新显示偏好：basic_preview 或 map_quick_view | Update preferred display mode by Resource Locator: basic_preview or map_quick_view
// @Tags Manager
// @Accept json
// @Produce json
// @Param body body UpdatePreferredModeRequest true "显示模式配置 | Display mode configuration"
// @Success 200 {object} map[string]interface{} "更新成功 | Updated successfully"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "资源不存在 | Resource not found"
// @Router /quick-view/preferred-mode [patch]
// @Security BearerAuth
func (h *QuickViewHandler) UpdatePreferredModeByLocator(c *gin.Context) {
	var req UpdatePreferredModeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		managerErrorWithDetail(c, http.StatusBadRequest, manageri18n.MsgInvalidRequestBody, err.Error())
		return
	}
	locator := strings.TrimSpace(req.Locator)
	if locator == "" {
		missingLocator(c)
		return
	}
	tenantID := tenantIDFromContext(c)
	capability, err := h.quickViewCapabilityForLocator(c.Request.Context(), tenantID, locator)
	if err != nil {
		quickViewLocatorError(c, err)
		return
	}
	identity := service.QuickViewIdentity{
		TenantID:        capability.TenantID,
		Locator:         capability.Locator,
		ItemFingerprint: capability.ItemFingerprint,
	}
	if err := h.service.UpdatePreferredModeByIdentity(c.Request.Context(), identity, req.PreferredMode, func(ctx context.Context) (*service.QuickViewCapability, error) {
		return capability, nil
	}); err != nil {
		quickViewLocatorError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":        "Preferred mode updated successfully",
		"preferred_mode": req.PreferredMode,
	})
}

// UpdateViewStateByLocator 更新 locator 快显视角状态
// @Summary 更新 locator 快显视角状态 | Update locator quick view state
// @Description 以 Resource Locator 为数据项身份更新快显/预览交互状态。view_state 是统一 JSON 字段，顶层按 basic_preview / quick_view 区分显示模式，模式内按 map / scene_3d 区分渲染域。 | Update quick view interaction state by Resource Locator. view_state is a unified JSON field grouped by display mode basic_preview / quick_view, then by render domain map / scene_3d.
// @Tags Manager
// @Accept json
// @Produce json
// @Param body body UpdateQuickViewStateRequest true "快显视角状态 | Quick view state"
// @Success 200 {object} map[string]interface{} "更新成功 | Updated successfully"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "资源不存在 | Resource not found"
// @Router /quick-view/view-state [patch]
// @Security BearerAuth
func (h *QuickViewHandler) UpdateViewStateByLocator(c *gin.Context) {
	var req UpdateQuickViewStateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		managerErrorWithDetail(c, http.StatusBadRequest, manageri18n.MsgInvalidRequestBody, err.Error())
		return
	}
	locator := strings.TrimSpace(req.Locator)
	if locator == "" {
		missingLocator(c)
		return
	}
	tenantID := tenantIDFromContext(c)
	capability, err := h.quickViewCapabilityForLocator(c.Request.Context(), tenantID, locator)
	if err != nil {
		quickViewLocatorError(c, err)
		return
	}
	identity := service.QuickViewIdentity{
		TenantID:        capability.TenantID,
		Locator:         capability.Locator,
		ItemFingerprint: capability.ItemFingerprint,
	}
	if err := h.service.UpdateViewStateByIdentity(c.Request.Context(), identity, req.ViewState); err != nil {
		quickViewLocatorError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":    "Quick view state updated successfully",
		"view_state": req.ViewState,
	})
}

// GetQuickViewGeoJSONByLocator 获取统一 GeoJSON 快显数据
// @Summary 获取统一 GeoJSON 快显数据 | Get unified quick-view GeoJSON
// @Description 以 Resource Locator 为身份返回标准 GeoJSON FeatureCollection。内部按引擎和格式分派，但响应必须是 GeoJSON。 | Return a standard GeoJSON FeatureCollection by Resource Locator. The backend dispatches by engine and format internally, but the response is always GeoJSON.
// @Tags Manager
// @Produce json
// @Param locator query string true "资源定位符URI | Resource locator URI"
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认1000，最大2000 | Page size, default 1000, max 2000"
// @Param geometry_column query string false "几何列名 | Geometry column"
// @Success 200 {object} map[string]interface{} "GeoJSON FeatureCollection"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Failure 404 {object} map[string]interface{} "资源不存在 | Resource not found"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /quick-view/geojson [get]
// @Security BearerAuth
func (h *QuickViewHandler) GetQuickViewGeoJSONByLocator(c *gin.Context) {
	locator := strings.TrimSpace(c.Query("locator"))
	if locator == "" {
		missingLocator(c)
		return
	}
	page := positiveIntQuery(c, "page", 1, 1, 0)
	pageSize := positiveIntQuery(c, "page_size", 1000, 1, 2000)
	result, err := h.previewResolver.PreviewFromURIWithSelection(c.Request.Context(), locator, page, pageSize, "", "", "", plugin.GraphSampleFilter{}, tenantIDFromContext(c))
	if err != nil {
		quickViewLocatorError(c, err)
		return
	}
	tablePreview, _ := result.Data.(*models.TablePreview)
	if tablePreview == nil {
		managerError(c, http.StatusBadRequest, manageri18n.MsgQuickViewGeometryMissing)
		return
	}
	geojson, err := quickViewFeatureCollection(result, tablePreview, strings.TrimSpace(c.Query("geometry_column")))
	if err != nil {
		quickViewLocatorError(c, err)
		return
	}
	c.JSON(http.StatusOK, geojson)
}

// GetQuickViewTileByLocator 获取统一 MVT 快显瓦片
// @Summary 获取统一 MVT 快显瓦片 | Get unified quick-view MVT tile
// @Description 以 Resource Locator 为身份返回 MVT 瓦片。实时 MVT 由 PostGIS 空间 item 提供，其他空间 item 通过瓦片缓存结果提供快显。 | Return an MVT tile by Resource Locator. Realtime MVT is provided by PostGIS spatial items; other spatial items use tile cache results for quick view.
// @Tags Manager
// @Produce application/vnd.mapbox-vector-tile
// @Param locator query string true "资源定位符URI | Resource locator URI"
// @Param z path int true "缩放级别 | Zoom"
// @Param x path int true "瓦片X坐标 | Tile X"
// @Param y path int true "瓦片Y坐标 | Tile Y"
// @Param geometry_column query string false "几何列名 | Geometry column"
// @Param cols query string false "返回列，逗号分隔 | Return columns, comma-separated"
// @Success 200 "MVT瓦片数据 | MVT tile data"
// @Header 200 {string} X-ADDP-Render-Source "渲染来源：cached_tile 或 realtime_tile | Render source: cached_tile or realtime_tile"
// @Header 200 {string} X-ADDP-Tile-Cache "运行时缓存状态：HIT 或 MISS | Runtime cache status: HIT or MISS"
// @Header 200 {string} X-ADDP-Tile-Cache-ID "命中的瓦片缓存结果 ID | Matched tile cache result ID"
// @Header 200 {string} X-ADDP-Tile-Status "瓦片语义状态：ok、empty、timeout 或 degraded | Tile semantic status: ok, empty, timeout, or degraded"
// @Header 200 {string} X-ADDP-Tile-Performance-Mode "动态瓦片性能模式：ready_3857_target、source_3857_indexed、source_3857_unindexed 或 source_transform_path | Realtime tile performance mode"
// @Header 200 {string} X-ADDP-Tile-Timeout-Budget-MS "动态 MVT 单瓦片超时预算，单位毫秒 | Realtime MVT per-tile timeout budget in milliseconds"
// @Header 200 {string} X-ADDP-Tile-Recommendation "超时或降级时的推荐动作：vector_quick_view_target_generation 或 vector_tile_cache_generation | Recommended action when timeout or degraded"
// @Header 200 {string} X-ADDP-Tile-Retry-Policy "超时或降级后的重试策略：suppress_tile 或 ttl | Retry policy after timeout or degraded"
// @Header 200 {string} X-Generation-Time "动态生成耗时 | Dynamic generation duration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 401 {object} map[string]interface{} "未授权 | Unauthorized"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /quick-view/tiles/{z}/{x}/{y}.mvt [get]
// @Security BearerAuth
func (h *QuickViewHandler) GetQuickViewTileByLocator(c *gin.Context) {
	if h.mvtService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "MVT service is not initialized"})
		return
	}
	locator := strings.TrimSpace(c.Query("locator"))
	if locator == "" {
		missingLocator(c)
		return
	}
	source, err := h.quickViewSourceForLocator(c.Request.Context(), tenantIDFromContext(c), locator)
	if err != nil {
		quickViewLocatorError(c, err)
		return
	}
	if !source.CanTile || source.EngineID == 0 || source.Schema == "" || source.Table == "" || source.SpatialMeta == nil {
		managerError(c, http.StatusBadRequest, manageri18n.MsgQuickViewGeometryMissing)
		return
	}
	z := positivePathInt(c, "z", 0, 22)
	x := positivePathInt(c, "x", 0, 0)
	y := positivePathInt(c, "y", 0, 0)
	if c.IsAborted() {
		return
	}
	tenantID := tenantIDFromContext(c)
	if tenantID == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id is required for MVT tile access"})
		return
	}
	geomCol := strings.TrimSpace(c.Query("geometry_column"))
	if geomCol == "" {
		geomCol = source.SpatialMeta.GeomColumn
	}
	tileSource := source
	spatialMeta := *source.SpatialMeta
	spatialMeta.GeomColumn = geomCol
	tileSource.SpatialMeta = &spatialMeta
	tileTarget, err := h.resolveRealtimeTileTarget(c.Request.Context(), tenantID, tileSource)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	cols := csvQuery(c.Query("cols"))
	response, err := h.mvtService.GetTile(
		c.Request.Context(),
		tenantID,
		source.EngineID,
		source.Schema,
		source.Table,
		geomCol,
		cols,
		z, x, y,
		source.SpatialMeta.SRID,
		tileTarget,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	applyTileResponseHeaders(c, response)
	c.Data(http.StatusOK, "application/vnd.mapbox-vector-tile", response.Data)
}

func applyTileResponseHeaders(c *gin.Context, response *service.TileResponse) {
	c.Header("Content-Type", "application/vnd.mapbox-vector-tile")
	c.Header("Access-Control-Expose-Headers", "X-ADDP-Render-Source, X-ADDP-Tile-Cache, X-ADDP-Tile-Cache-ID, X-ADDP-Tile-Status, X-ADDP-Tile-Performance-Mode, X-ADDP-Tile-Timeout-Budget-MS, X-ADDP-Tile-Recommendation, X-ADDP-Tile-Retry-Policy, Retry-After, X-Generation-Time, Content-Length")
	renderSource := strings.TrimSpace(response.RenderSource)
	if renderSource == "" {
		renderSource = service.QuickViewRenderSourceRealtimeTile
	}
	c.Header("X-ADDP-Render-Source", renderSource)
	tileStatus := strings.TrimSpace(response.Status)
	if tileStatus == "" {
		tileStatus = service.TileStatusOK
		if len(response.Data) == 0 {
			tileStatus = service.TileStatusEmpty
		}
	}
	c.Header("X-ADDP-Tile-Status", tileStatus)
	if response.PerformanceMode != "" {
		c.Header("X-ADDP-Tile-Performance-Mode", response.PerformanceMode)
	}
	if response.TimeoutBudget > 0 {
		c.Header("X-ADDP-Tile-Timeout-Budget-MS", strconv.FormatInt(response.TimeoutBudget.Milliseconds(), 10))
	}
	if isDegradedTileStatus(tileStatus) {
		if response.TimeoutRecommendation != "" {
			c.Header("X-ADDP-Tile-Recommendation", response.TimeoutRecommendation)
		}
		if response.TimeoutRetryPolicy != "" {
			c.Header("X-ADDP-Tile-Retry-Policy", response.TimeoutRetryPolicy)
			if response.TimeoutRetryPolicy == service.RealtimeTileTimeoutRetryTTL {
				retryAfter := int64(60)
				if response.TimeoutRetryAfter > 0 {
					retryAfter = int64(response.TimeoutRetryAfter.Seconds())
				}
				c.Header("Retry-After", strconv.FormatInt(retryAfter, 10))
			}
		}
	}
	if response.TileCacheID != nil {
		c.Header("X-ADDP-Tile-Cache-ID", strconv.FormatUint(uint64(*response.TileCacheID), 10))
	}
	if response.FromCache {
		c.Header("Content-Encoding", "gzip")
		c.Header("Cache-Control", "public, max-age=86400")
		c.Header("X-ADDP-Tile-Cache", "HIT")
	} else {
		c.Header("Cache-Control", "public, max-age=60")
		c.Header("X-ADDP-Tile-Cache", "MISS")
		c.Header("X-Generation-Time", response.Duration.String())
	}
}

func isDegradedTileStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case service.TileStatusTimeout, service.TileStatusDegraded:
		return true
	default:
		return false
	}
}

func (h *QuickViewHandler) quickViewCapabilityForLocator(ctx context.Context, tenantID *uint, locator string) (*service.QuickViewCapability, error) {
	source, err := h.quickViewSourceForLocator(ctx, tenantID, locator)
	if err != nil {
		return nil, err
	}
	h.attachRenderableExtent(ctx, tenantID, &source)
	h.attachRealtimeTileTarget(ctx, tenantID, &source)
	capability, err := h.service.BuildCapabilityFromSource(ctx, source)
	if err != nil {
		return nil, err
	}
	applyLocatorQuickViewURLs(capability)
	return capability, nil
}

func (h *QuickViewHandler) attachRenderableExtent(ctx context.Context, tenantID *uint, source *service.QuickViewSource) {
	if h == nil || source == nil || source.SpatialMeta == nil {
		return
	}
	meta := source.SpatialMeta
	if len(meta.RenderExtent) == 4 && meta.RenderExtentSRID > 0 {
		return
	}
	if len(meta.Extent) != 4 {
		return
	}
	extentSRID := meta.ExtentSRID
	if extentSRID == 0 {
		extentSRID = meta.SRID
	}
	if extentSRID == commonSpatial.SRIDWGS84 {
		meta.RenderExtent = append([]float64(nil), meta.Extent...)
		meta.RenderExtentSRID = commonSpatial.SRIDWGS84
		meta.RenderExtentSource = "source_extent"
		return
	}
	if extentSRID <= 0 || source.EngineID == 0 || h.mvtService == nil {
		return
	}
	renderExtent, err := h.mvtService.TransformExtentWGS84(ctx, tenantID, source.EngineID, meta.Extent, extentSRID)
	if err != nil {
		return
	}
	meta.RenderExtent = renderExtent
	meta.RenderExtentSRID = commonSpatial.SRIDWGS84
	meta.RenderExtentSource = "source_extent_transformed"
}

func (h *QuickViewHandler) attachRealtimeTileTarget(ctx context.Context, tenantID *uint, source *service.QuickViewSource) {
	target, err := h.resolveRealtimeTileTarget(ctx, tenantID, *source)
	if err != nil || target == nil {
		return
	}
	source.RealtimeTileTarget = target
}

func (h *QuickViewHandler) resolveRealtimeTileTarget(ctx context.Context, tenantID *uint, source service.QuickViewSource) (*service.RealtimeTileTarget, error) {
	if h == nil || h.mvtService == nil || !source.CanTile || source.SpatialMeta == nil {
		return nil, nil
	}
	if source.EngineID == 0 || strings.TrimSpace(source.Schema) == "" || strings.TrimSpace(source.Table) == "" {
		return nil, nil
	}
	geomCol := strings.TrimSpace(source.SpatialMeta.GeomColumn)
	if geomCol == "" {
		return nil, nil
	}
	return h.mvtService.ResolveRealtimeTileTarget(ctx, tenantID, source.EngineID, source.Schema, source.Table, geomCol, source.SpatialMeta.SRID)
}

func (h *QuickViewHandler) quickViewSourceForLocator(ctx context.Context, tenantID *uint, locator string) (service.QuickViewSource, error) {
	if h.service != nil {
		if source, ok, err := h.service.RasterMosaicSourceForLocator(ctx, tenantID, locator); err != nil || ok {
			return source, err
		}
	}
	if h.previewResolver == nil {
		return service.QuickViewSource{}, errors.New("preview resolver not initialized")
	}
	result, err := h.previewResolver.PreviewFromURIWithSelection(ctx, locator, 1, 1, "", "", "", plugin.GraphSampleFilter{}, tenantID)
	if err != nil {
		return service.QuickViewSource{}, err
	}
	tablePreview, _ := result.Data.(*models.TablePreview)
	source := quickViewSourceFromPreview(locator, tenantID, result, tablePreview)
	return source, nil
}

func quickViewSourceFromPreview(locator string, tenantID *uint, result *preview.PreviewResult, tablePreview *models.TablePreview) service.QuickViewSource {
	storageTenantID := uint(0)
	if tenantID != nil {
		storageTenantID = *tenantID
	}
	source := service.QuickViewSource{
		Identity: service.QuickViewIdentity{
			TenantID: storageTenantID,
			Locator:  locator,
		},
		DirectGeoJSON: true,
		GeoJSONURL:    locatorQuickViewGeoJSONURL(locator, tablePreview),
	}
	if tablePreview == nil {
		return source
	}
	source.EngineID = tablePreview.EngineID
	source.Schema = tablePreview.Schema
	source.Table = tablePreview.Table
	if result != nil && result.Metadata != nil {
		source.Identity.Locator = result.Metadata.Locator
		source.Identity.ItemFingerprint = strings.TrimSpace(result.Metadata.ItemFingerprint)
		source.GeoJSONURL = locatorQuickViewGeoJSONURL(source.Identity.Locator, tablePreview)
	}
	if tablePreview.Object != nil {
		gaussianSplat := service.GaussianSplatQuickViewSourceFromAttributes(tablePreview.Object.Attributes)
		if gaussianSplat != nil {
			source.EngineID = tablePreview.Object.EngineID
			source.GaussianSplat = gaussianSplat
			source.DirectGeoJSON = false
			source.GeoJSONURL = ""
			source.CanTile = false
			return source
		}
		model3D := service.Model3DQuickViewSourceFromAttributes(tablePreview.Object.Attributes)
		if model3D != nil {
			source.EngineID = tablePreview.Object.EngineID
			source.Model3D = model3D
			source.DirectGeoJSON = false
			source.GeoJSONURL = ""
			source.CanTile = false
			return source
		}
		raster := service.RasterQuickViewSourceFromAttributes(tablePreview.Object.Attributes, source.Identity.Locator, tablePreview.Object.EngineID)
		if raster != nil {
			source.EngineID = tablePreview.Object.EngineID
			source.Raster = raster
			source.DirectGeoJSON = false
			source.GeoJSONURL = ""
			source.CanTile = false
			return source
		}
	}
	source.CanTile = strings.EqualFold(strings.TrimSpace(tablePreview.EngineType), "postgresql") &&
		strings.TrimSpace(tablePreview.Schema) != "" &&
		strings.TrimSpace(tablePreview.Table) != ""
	geometryColumn := strings.TrimSpace(tablePreview.GeometryColumn)
	if geometryColumn == "" && len(tablePreview.GeometryColumns) > 0 {
		geometryColumn = strings.TrimSpace(tablePreview.GeometryColumns[0])
	}
	sourceSRID := tablePreview.SourceSRID
	if sourceSRID == 0 {
		sourceSRID = tablePreview.SRID
	}
	extentSRID := tablePreview.SRID
	if extentSRID == 0 {
		extentSRID = sourceSRID
	}
	source.SpatialMeta = &service.SpatialMetadataResult{
		GeomColumn:          geometryColumn,
		GeometryColumns:     tablePreview.GeometryColumns,
		SRID:                sourceSRID,
		SourceCRS:           tablePreview.SourceCRS,
		SourceCRSDefinition: tablePreview.SourceCRSDefinition,
		ExtentSRID:          extentSRID,
		Extent:              tablePreview.Extent,
		RecordCount:         int64(tablePreview.Total),
	}
	return source
}

func locatorQuickViewGeoJSONURL(locator string, tablePreview *models.TablePreview) string {
	values := url.Values{}
	values.Set("locator", strings.TrimSpace(locator))
	values.Set("page", "1")
	pageSize := 1
	if tablePreview != nil && tablePreview.Total > 0 {
		pageSize = tablePreview.Total
	}
	values.Set("page_size", strconv.Itoa(pageSize))
	if tablePreview != nil && strings.TrimSpace(tablePreview.GeometryColumn) != "" {
		values.Set("geometry_column", strings.TrimSpace(tablePreview.GeometryColumn))
	}
	return "/api/v1/manager/quick-view/geojson?" + values.Encode()
}

func locatorQuickViewTileURL(locator string) string {
	values := url.Values{}
	values.Set("locator", strings.TrimSpace(locator))
	return "/api/v1/manager/quick-view/tiles/{z}/{x}/{y}.mvt?" + values.Encode()
}

func locatorRasterMosaicTileURL(locator string) string {
	values := url.Values{}
	values.Set("locator", strings.TrimSpace(locator))
	values.Set("gamma", strconv.FormatFloat(service.DefaultRasterMosaicGamma, 'f', -1, 64))
	return "/api/v1/manager/raster_mosaic/tiles/{z}/{x}/{y}.png?" + values.Encode()
}

func applyLocatorQuickViewURLs(capability *service.QuickViewCapability) {
	if capability == nil || strings.TrimSpace(capability.Locator) == "" {
		return
	}
	switch capability.RenderSource {
	case service.QuickViewRenderSourceDirectGeoJSON:
		if capability.QuickView.GeoJSONURL == "" {
			capability.QuickView.GeoJSONURL = locatorQuickViewGeoJSONURL(capability.Locator, nil)
		}
	case service.QuickViewRenderSourceRealtimeTile, service.QuickViewRenderSourceCachedTile:
		capability.QuickView.TileURLTemplate = locatorQuickViewTileURL(capability.Locator)
	case service.QuickViewRenderSourceRasterMosaic:
		capability.QuickView.TileURLTemplate = locatorRasterMosaicTileURL(capability.Locator)
	}
}

func quickViewFeatureCollection(result *preview.PreviewResult, tablePreview *models.TablePreview, requestedGeometryColumn string) (gin.H, error) {
	geometryColumn := strings.TrimSpace(requestedGeometryColumn)
	if geometryColumn == "" {
		geometryColumn = strings.TrimSpace(tablePreview.GeometryColumn)
	}
	if geometryColumn == "" && len(tablePreview.GeometryColumns) > 0 {
		geometryColumn = strings.TrimSpace(tablePreview.GeometryColumns[0])
	}
	if geometryColumn == "" {
		return nil, service.ErrQuickViewGeometryColumnNotFound
	}

	features := make([]gin.H, 0, len(tablePreview.Rows))
	for _, row := range tablePreview.Rows {
		geometry, ok := quickViewGeoJSONGeometry(row[geometryColumn])
		if !ok {
			continue
		}
		properties := gin.H{}
		for key, value := range row {
			if strings.EqualFold(strings.TrimSpace(key), geometryColumn) {
				continue
			}
			properties[key] = value
		}
		features = append(features, gin.H{
			"type":       "Feature",
			"geometry":   geometry,
			"properties": properties,
		})
	}

	locator := ""
	itemFingerprint := ""
	if result != nil && result.Metadata != nil {
		locator = result.Metadata.Locator
		itemFingerprint = result.Metadata.ItemFingerprint
	}
	sourceSRID := tablePreview.SourceSRID
	if sourceSRID == 0 {
		sourceSRID = tablePreview.SRID
	}
	sourceCRS := strings.TrimSpace(tablePreview.SourceCRS)
	sourceCRSDefinition := tablePreview.SourceCRSDefinition
	metadata := spatialPreviewContract(geometryColumn, sourceSRID, sourceCRS, sourceCRSDefinition)
	metadata["locator"] = locator
	metadata["item_fingerprint"] = itemFingerprint

	return gin.H{
		"type":     "FeatureCollection",
		"features": features,
		"metadata": metadata,
		"pagination": gin.H{
			"page":      tablePreview.Page,
			"page_size": tablePreview.PageSize,
			"total":     tablePreview.Total,
		},
	}, nil
}

func quickViewGeoJSONGeometry(value interface{}) (gin.H, bool) {
	switch typed := value.(type) {
	case nil:
		return nil, false
	case map[string]interface{}:
		if quickViewIsGeoJSONGeometry(typed) {
			return typed, true
		}
		if strings.EqualFold(strings.TrimSpace(stringValue(typed["type"])), "Feature") {
			if geometry, ok := typed["geometry"].(map[string]interface{}); ok && quickViewIsGeoJSONGeometry(geometry) {
				return geometry, true
			}
		}
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return nil, false
		}
		var parsed map[string]interface{}
		if strings.HasPrefix(text, "{") {
			if err := json.Unmarshal([]byte(text), &parsed); err == nil {
				return quickViewGeoJSONGeometry(parsed)
			}
		}
		geometry, err := commonSpatial.ParseGeometryText(text)
		if err != nil {
			return nil, false
		}
		encoded, err := geomGeoJSON.Encode(geometry)
		if err != nil || encoded == nil {
			return nil, false
		}
		data, err := json.Marshal(encoded)
		if err != nil {
			return nil, false
		}
		var out gin.H
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, false
		}
		return out, true
	case []byte:
		geometry, err := commonSpatial.ParseGeometryBytes(typed)
		if err != nil {
			return nil, false
		}
		encoded, err := geomGeoJSON.Encode(geometry)
		if err != nil || encoded == nil {
			return nil, false
		}
		data, err := json.Marshal(encoded)
		if err != nil {
			return nil, false
		}
		var out gin.H
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, false
		}
		return out, true
	}
	return nil, false
}

func quickViewIsGeoJSONGeometry(value map[string]interface{}) bool {
	typeName := strings.TrimSpace(stringValue(value["type"]))
	switch typeName {
	case "Point", "MultiPoint", "LineString", "MultiLineString", "Polygon", "MultiPolygon":
		_, ok := value["coordinates"]
		return ok
	case "GeometryCollection":
		_, ok := value["geometries"]
		return ok
	default:
		return false
	}
}

func positiveIntQuery(c *gin.Context, name string, defaultValue, minValue, maxValue int) int {
	value := defaultValue
	if raw := strings.TrimSpace(c.Query(name)); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			value = parsed
		}
	}
	if value < minValue {
		value = minValue
	}
	if maxValue > 0 && value > maxValue {
		value = maxValue
	}
	return value
}

func positivePathInt(c *gin.Context, name string, minValue, maxValue int) int {
	raw := strings.TrimSpace(c.Param(name))
	if name == "y" {
		if raw == "" {
			raw = strings.TrimSpace(c.Param("y.mvt"))
		}
		raw = strings.TrimSuffix(raw, ".mvt")
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < minValue || (maxValue > 0 && parsed > maxValue) {
		managerError(c, http.StatusBadRequest, "invalid "+name)
		c.Abort()
		return 0
	}
	return parsed
}

func csvQuery(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if item := strings.TrimSpace(part); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func stringValue(value interface{}) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
