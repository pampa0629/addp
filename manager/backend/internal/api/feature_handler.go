package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/instanceprovider"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/logger"
	commonAuth "github.com/addp/common/middleware/auth"
	commonModels "github.com/addp/common/models"
	manageri18n "github.com/addp/manager/i18n"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/ewkb"
	"github.com/twpayne/go-geom/encoding/geojson"
)

// FeatureHandler 处理要素相关的请求（用于地图与表格关联）
type FeatureHandler struct {
	systemClient     *commonClient.SystemClient
	metadataRepo     *repository.MetadataRepository
	quickViewService *service.QuickViewService
}

// NewFeatureHandler 创建要素处理器
func NewFeatureHandler(systemClient *commonClient.SystemClient, metadataRepo *repository.MetadataRepository, quickViewService *service.QuickViewService) *FeatureHandler {
	return &FeatureHandler{
		systemClient:     systemClient,
		metadataRepo:     metadataRepo,
		quickViewService: quickViewService,
	}
}

// GetFeatureCentroid 获取要素的几何中心点（用于表格行定位到地图）
// GET /api/v1/manager/engines/:id/spatial/features/:feature_id/centroid?schema=xxx&table=xxx&geom=geometry_column
// @Summary 获取要素几何中心点 | Get feature centroid
// @Description 获取指定要素的源坐标几何中心点和 CRS 元数据，后端不做 CRS transform | Get the source-CRS centroid and CRS metadata without backend CRS transform
// @Tags Manager
// @Produce json
// @Param id path int true "存储引擎ID | Engine ID"
// @Param feature_id path string true "要素ID | Feature ID"
// @Param schema query string true "Schema | Schema"
// @Param table query string true "数据项名称 | Item name"
// @Param geom query string false "几何字段名；不传则从 Meta 空间元数据读取 | Geometry column; resolved from Meta spatial metadata when omitted"
// @Param primary_key query string false "主键字段名；不传则从 Meta 空间元数据读取 | Primary key column; resolved from Meta spatial metadata when omitted"
// @Success 200 {object} map[string]interface{} "中心点坐标 | Centroid coordinates"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "要素不存在 | Feature not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.content.read"]
// @Router /engines/{id}/spatial/features/{feature_id}/centroid [get]
// @Security BearerAuth
func (h *FeatureHandler) GetFeatureCentroid(c *gin.Context) {
	// 1. 解析路径参数
	engineIDStr := c.Param("id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		managerError(c, http.StatusBadRequest, manageri18n.MsgInvalidEngineIDParam)
		return
	}

	featureIDStr := c.Param("feature_id")
	// 注意：featureID 可能不是数字（如 UUID），所以不转换类型

	// 2. 解析查询参数
	schema := c.Query("schema")
	if schema == "" {
		managerError(c, http.StatusBadRequest, manageri18n.MsgSchemaRequired)
		return
	}

	table := c.Query("table")
	if table == "" {
		managerError(c, http.StatusBadRequest, manageri18n.MsgTableRequired)
		return
	}

	geomCol := strings.TrimSpace(c.Query("geom"))
	primaryKey := strings.TrimSpace(c.Query("primary_key"))
	var spatialMeta *service.SpatialMetadataResult
	if geomCol == "" || primaryKey == "" {
		spatialMeta, err = spatialMetadataFromMeta(c, h.quickViewService, uint(engineID), schema, table)
		if err != nil {
			logger.L().Warn("无法从 Meta 获取要素定位空间元数据", "error", err, "engine_id", engineID, "schema", schema, "table", table)
			managerError(c, http.StatusBadRequest, manageri18n.MsgQuickViewGeometryMissing)
			return
		}
		if geomCol == "" {
			geomCol = strings.TrimSpace(spatialMeta.GeomColumn)
		}
		if primaryKey == "" {
			primaryKey = strings.TrimSpace(spatialMeta.PrimaryKey)
		}
	}
	if geomCol == "" || primaryKey == "" {
		managerError(c, http.StatusBadRequest, manageri18n.MsgQuickViewGeometryMissing)
		return
	}

	// 3. 获取引擎信息
	if h.systemClient == nil {
		managerError(c, http.StatusInternalServerError, manageri18n.MsgSystemClientUnavailable)
		return
	}

	engine, err := h.systemClient.GetEngineForTenant(c.Request.Context(), commonAuth.GetTenantID(c), uint(engineID))
	if err != nil {
		managerError(c, http.StatusNotFound, manageri18n.MsgEngineNotFound)
		return
	}

	if !featureEngineAccessible(c, engine.TenantID) {
		managerError(c, http.StatusForbidden, manageri18n.MsgEngineAccessDenied)
		return
	}
	feature, err := readSpatialFeature(c, engine, plugin.ConnectionInfo(engine.ConnectionInfo), schema, table, geomCol, primaryKey, featureIDStr)
	if err != nil {
		logger.L().Warn("读取要素中心点失败", "error", err, "engine_id", engineID, "schema", schema, "table", table)
		managerError(c, http.StatusInternalServerError, manageri18n.MsgQueryFailed)
		return
	}
	if feature == nil {
		managerError(c, http.StatusNotFound, manageri18n.MsgFeatureNotFound)
		return
	}
	x, y, ok, err := spatialFeatureCentroid(feature.CentroidEWKB)
	if err != nil || !ok {
		managerError(c, http.StatusNotFound, manageri18n.MsgFeatureInvalidGeometry)
		return
	}

	sourceCRS, sourceCRSDefinition := spatialFeatureCRS(feature, spatialMeta)
	response := spatialPreviewContract(geomCol, feature.SRID, sourceCRS, sourceCRSDefinition)
	response["centroid"] = gin.H{
		"x": x,
		"y": y,
	}
	c.JSON(http.StatusOK, response)
}

// GetFeatureGeometry 获取要素的完整几何（用于地图高亮显示）
// GET /api/v1/manager/engines/:id/spatial/features/:feature_id/geometry?schema=xxx&table=xxx&geom=geometry_column
// @Summary 获取要素完整几何 | Get feature geometry
// @Description 获取指定要素的源坐标完整几何数据和 CRS 元数据，后端不做 CRS transform | Get source-CRS feature geometry and CRS metadata without backend CRS transform
// @Tags Manager
// @Produce json
// @Param id path int true "存储引擎ID | Engine ID"
// @Param feature_id path string true "要素ID | Feature ID"
// @Param schema query string true "Schema | Schema"
// @Param table query string true "数据项名称 | Item name"
// @Param geom query string false "几何字段名；不传则从 Meta 空间元数据读取 | Geometry column; resolved from Meta spatial metadata when omitted"
// @Param primary_key query string false "主键字段名；不传则从 Meta 空间元数据读取 | Primary key column; resolved from Meta spatial metadata when omitted"
// @Success 200 {object} map[string]interface{} "几何数据及边界框 | Geometry data and bounding box"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "要素不存在 | Feature not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.content.read"]
// @Router /engines/{id}/spatial/features/{feature_id}/geometry [get]
// @Security BearerAuth
func (h *FeatureHandler) GetFeatureGeometry(c *gin.Context) {
	// 1. 解析路径参数
	engineIDStr := c.Param("id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		managerError(c, http.StatusBadRequest, manageri18n.MsgInvalidEngineIDParam)
		return
	}

	featureIDStr := c.Param("feature_id")

	// 2. 解析查询参数
	schema := c.Query("schema")
	if schema == "" {
		managerError(c, http.StatusBadRequest, manageri18n.MsgSchemaRequired)
		return
	}

	table := c.Query("table")
	if table == "" {
		managerError(c, http.StatusBadRequest, manageri18n.MsgTableRequired)
		return
	}

	geomCol := strings.TrimSpace(c.Query("geom"))
	primaryKey := strings.TrimSpace(c.Query("primary_key"))
	var spatialMeta *service.SpatialMetadataResult
	if geomCol == "" || primaryKey == "" {
		spatialMeta, err = spatialMetadataFromMeta(c, h.quickViewService, uint(engineID), schema, table)
		if err != nil {
			logger.L().Warn("无法从 Meta 获取要素几何空间元数据", "error", err, "engine_id", engineID, "schema", schema, "table", table)
			managerError(c, http.StatusBadRequest, manageri18n.MsgQuickViewGeometryMissing)
			return
		}
		if geomCol == "" {
			geomCol = strings.TrimSpace(spatialMeta.GeomColumn)
		}
		if primaryKey == "" {
			primaryKey = strings.TrimSpace(spatialMeta.PrimaryKey)
		}
	}
	if geomCol == "" || primaryKey == "" {
		managerError(c, http.StatusBadRequest, manageri18n.MsgQuickViewGeometryMissing)
		return
	}

	// 3. 获取引擎信息
	if h.systemClient == nil {
		managerError(c, http.StatusInternalServerError, manageri18n.MsgSystemClientUnavailable)
		return
	}

	engine, err := h.systemClient.GetEngineForTenant(c.Request.Context(), commonAuth.GetTenantID(c), uint(engineID))
	if err != nil {
		managerError(c, http.StatusNotFound, manageri18n.MsgEngineNotFound)
		return
	}

	if !featureEngineAccessible(c, engine.TenantID) {
		managerError(c, http.StatusForbidden, manageri18n.MsgEngineAccessDenied)
		return
	}
	feature, err := readSpatialFeature(c, engine, plugin.ConnectionInfo(engine.ConnectionInfo), schema, table, geomCol, primaryKey, featureIDStr)
	if err != nil {
		logger.L().Warn("读取要素几何失败", "error", err, "engine_id", engineID, "schema", schema, "table", table)
		managerError(c, http.StatusInternalServerError, manageri18n.MsgQueryFailed)
		return
	}
	if feature == nil {
		managerError(c, http.StatusNotFound, manageri18n.MsgFeatureNotFound)
		return
	}
	geometry, err := ewkb.Unmarshal(feature.GeometryEWKB)
	if err != nil || geometry == nil || geometry.Bounds().IsEmpty() {
		managerError(c, http.StatusNotFound, manageri18n.MsgFeatureInvalidGeometry)
		return
	}
	encodedGeoJSON, err := geojson.Marshal(geometry)
	if err != nil {
		managerError(c, http.StatusNotFound, manageri18n.MsgFeatureInvalidGeometry)
		return
	}
	x, y, ok, err := spatialFeatureCentroid(feature.CentroidEWKB)
	if err != nil || !ok {
		managerError(c, http.StatusNotFound, manageri18n.MsgFeatureInvalidGeometry)
		return
	}
	bounds := geometry.Bounds()
	sourceCRS, sourceCRSDefinition := spatialFeatureCRS(feature, spatialMeta)
	response := spatialPreviewContract(geomCol, feature.SRID, sourceCRS, sourceCRSDefinition)
	response["geojson"] = string(encodedGeoJSON)
	response["centroid"] = gin.H{
		"x": x,
		"y": y,
	}
	response["extent"] = []float64{bounds.Min(0), bounds.Min(1), bounds.Max(0), bounds.Max(1)}
	response["extent_srid"] = feature.SRID
	c.JSON(http.StatusOK, response)
}

func readSpatialFeature(c *gin.Context, engine *commonModels.Engine, connInfo plugin.ConnectionInfo, schema, table, geometryField, identityField, identityValue string) (*plugin.SpatialFeatureData, error) {
	if instanceprovider.IsSuperMapSDXPostgreSQLTable(engine, schema, table) {
		return nil, fmt.Errorf("SuperMap SDX+ for PostgreSQL feature endpoint is not supported until a provider feature-read session is implemented")
	}
	if engine == nil {
		return nil, fmt.Errorf("engine is required")
	}
	engineType := engine.EngineType
	plug, err := plugin.Get(engineType)
	if err != nil {
		return nil, err
	}
	provider, ok := plug.(plugin.SpatialFeatureReadProvider)
	if !ok {
		return nil, fmt.Errorf("engine %s does not implement SpatialFeatureReadProvider", engineType)
	}
	modelProvider, ok := plug.(plugin.CatalogModelProvider)
	if !ok {
		return nil, fmt.Errorf("engine %s does not implement CatalogModelProvider", engineType)
	}
	branch, ok := plugin.CatalogFirstBusinessBranch(modelProvider.CatalogModel())
	if !ok || strings.TrimSpace(branch.Term) == "" {
		return nil, fmt.Errorf("engine %s catalog model has no business namespace", engineType)
	}
	return provider.ReadSpatialFeature(c.Request.Context(), connInfo, plugin.TabularItemPath(engine.ID, branch.Term, schema, table), plugin.SpatialFeatureReadOptions{
		GeometryField: geometryField,
		IdentityField: identityField,
		IdentityValue: identityValue,
	})
}

func featureEngineAccessible(c *gin.Context, engineTenantID *uint) bool {
	tenantID := tenantIDFromContext(c)
	return tenantID == nil || engineTenantID == nil || *tenantID == *engineTenantID
}

func spatialFeatureCentroid(encoded []byte) (float64, float64, bool, error) {
	if len(encoded) == 0 {
		return 0, 0, false, nil
	}
	geometry, err := ewkb.Unmarshal(encoded)
	if err != nil {
		return 0, 0, false, err
	}
	point, ok := geometry.(*geom.Point)
	if !ok || point.Empty() || len(point.FlatCoords()) < 2 {
		return 0, 0, false, nil
	}
	return point.FlatCoords()[0], point.FlatCoords()[1], true, nil
}

func spatialFeatureCRS(feature *plugin.SpatialFeatureData, meta *service.SpatialMetadataResult) (string, *datatype.CRSDefinition) {
	if meta != nil && (strings.TrimSpace(meta.SourceCRS) != "" || meta.SourceCRSDefinition != nil) {
		return strings.TrimSpace(meta.SourceCRS), meta.SourceCRSDefinition
	}
	if feature != nil && feature.Spatial != nil {
		crsRef := feature.Spatial.PrimaryCRSRef()
		return crsRef, feature.Spatial.CRSDefinitionByID(crsRef)
	}
	if feature != nil && feature.SRID > 0 {
		return datatype.EPSGCRSRef(feature.SRID), nil
	}
	return "", nil
}
