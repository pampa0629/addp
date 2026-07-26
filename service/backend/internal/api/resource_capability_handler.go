package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	"github.com/addp/common/dbbridge"
	pgmapper "github.com/addp/common/format/mappers/postgresql"
	commonJSON "github.com/addp/common/jsonmap"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/models"
	"github.com/addp/common/sqldialect"
	servicei18n "github.com/addp/service/i18n"
	serviceModels "github.com/addp/service/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type graphNodeShapeOption struct {
	Name       string               `json:"name"`
	Kind       string               `json:"kind,omitempty"`
	Labels     []string             `json:"labels,omitempty"`
	Count      *int64               `json:"count,omitempty"`
	Properties []datatype.FieldInfo `json:"properties,omitempty"`
}

func authTokenFromContext(c *gin.Context) string {
	authToken := c.GetString("jwt_token")
	if authToken != "" {
		return authToken
	}
	if header := c.GetHeader("Authorization"); len(header) > 7 && header[:7] == "Bearer " {
		return header[7:]
	}
	return ""
}

// ResourceCapabilityHandler 处理 Service 模块仍需自有计算的资源辅助能力。
// 资源选择、资源树和表级空间元数据统一走 Meta resource-tree / item API。
type ResourceCapabilityHandler struct {
	systemClient *commonClient.SystemClient
	metaBaseURL  string
}

// NewResourceCapabilityHandler 创建新的资源能力处理器。
func NewResourceCapabilityHandler(systemClient *commonClient.SystemClient, metaBaseURL string) *ResourceCapabilityHandler {
	return &ResourceCapabilityHandler{
		systemClient: systemClient,
		metaBaseURL:  metaBaseURL,
	}
}

// GetGraphNodeShapes 获取 graph item 的节点形状列表。
// @Summary 获取图节点形状 | Get graph node shapes
// @Tags 资源能力 | Resource Capabilities
// @Produce json
// @Param engine_id query int true "引擎ID | Engine ID"
// @Param database query string false "数据库名 | Database name"
// @Success 200 {array} graphNodeShapeOption
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.definition.read"]
// @Router /graphs/node-shapes [get]
// @Security BearerAuth
func (h *ResourceCapabilityHandler) GetGraphNodeShapes(c *gin.Context) {
	engineIDStr := c.Query("engine_id")
	if engineIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing engine_id parameter"})
		return
	}

	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid engine_id"})
		return
	}

	database := c.Query("database")
	if database == "" {
		database = "neo4j"
	}

	metaClient := commonClient.NewMetaClient(h.metaBaseURL, authTokenFromContext(c))
	item, err := metaClient.GetItemByCatalogPath(uint(engineID), fmt.Sprintf("%s.graph", database))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get graph item: " + err.Error()})
		return
	}

	graphInfo := graphInfoFromMetaAttributes(item.Attributes)
	if graphInfo == nil {
		c.JSON(http.StatusOK, []graphNodeShapeOption{})
		return
	}

	shapes := make([]graphNodeShapeOption, 0, len(graphInfo.NodeShapes))
	for _, shape := range graphInfo.NodeShapes {
		shapes = append(shapes, graphNodeShapeOption{
			Name:       shape.Name,
			Kind:       shape.Kind,
			Labels:     append([]string(nil), shape.Labels...),
			Count:      shape.Count,
			Properties: append([]datatype.FieldInfo(nil), shape.Properties...),
		})
	}

	c.JSON(http.StatusOK, shapes)
}

func graphInfoFromMetaAttributes(attrs map[string]interface{}) *datatype.GraphInfo {
	return datatype.GraphInfoFromPayload(commonJSON.Section(attrs, "type_info.graph"))
}

// GetSQLOutputContract 检测 SQL 查询结果的标准输出契约。
// @Summary 获取 SQL 输出契约 | Get SQL output contract
// @Tags 资源能力 | Resource Capabilities
// @Accept json
// @Produce json
// @Param request body serviceModels.SQLQueryOutputContractRequest true "SQL 输出契约请求 | SQL output contract request"
// @Success 200 {object} serviceModels.QueryServiceOutputContract "输出契约 | Output contract"
// @Failure 400 {object} map[string]string "请求错误 | Bad request"
// @Failure 500 {object} map[string]string "检测失败 | Detection failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.definition.read"]
// @Router /sql/output-contract [post]
// @Security BearerAuth
func (h *ResourceCapabilityHandler) GetSQLOutputContract(c *gin.Context) {
	var req serviceModels.SQLQueryOutputContractRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.TWithDetail(c, commoni18n.MsgInvalidParams, err.Error())})
		return
	}

	// 1. 获取引擎信息
	engine, err := h.systemClient.GetEngine(req.EngineID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.TWithDetail(c, servicei18n.MsgSQLOutputContractFailed, err.Error())})
		return
	}

	// 2. 转换为 common models 并创建连接池
	commonEngine := &models.Engine{
		ID:             engine.ID,
		EngineType:     engine.EngineType,
		ConnectionInfo: models.ConnectionInfo(engine.ConnectionInfo),
	}

	db, err := dbbridge.GetOrCreatePool(commonEngine, dbbridge.DefaultPoolConfig())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.TWithDetail(c, servicei18n.MsgSQLOutputContractFailed, err.Error())})
		return
	}

	contract, err := h.detectSQLOutputContract(c.Request.Context(), db, engine.EngineType, req.SQL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.TWithDetail(c, servicei18n.MsgSQLOutputContractFailed, err.Error())})
		return
	}
	c.JSON(http.StatusOK, contract)
}

func (h *ResourceCapabilityHandler) detectSQLOutputContract(
	ctx context.Context,
	db *gorm.DB,
	engineType string,
	query string,
) (*serviceModels.QueryServiceOutputContract, error) {
	// 仅支持 PostgreSQL（PostGIS）
	if engineType != "postgresql" {
		return nil, fmt.Errorf("SQL output contract detection only supports PostgreSQL")
	}

	query = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(query), ";"))
	testSQL := fmt.Sprintf("SELECT * FROM (%s) AS subquery LIMIT 1", query)
	rows, err := db.WithContext(ctx).Raw(testSQL).Rows()
	if err != nil {
		return nil, fmt.Errorf("failed to execute SQL: %w", err)
	}
	defer rows.Close()

	// 获取列类型
	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("failed to get column types: %w", err)
	}

	mapper := &pgmapper.TypeMapper{}
	tableInfo := &datatype.TableInfo{Kind: "query", Fields: make([]datatype.FieldInfo, 0, len(columnTypes))}
	var geometryColumn string
	for index, colType := range columnTypes {
		nativeType := strings.ToLower(strings.TrimSpace(colType.DatabaseTypeName()))
		fieldType := mapper.ToCommon(nativeType)
		if nativeType == "geography" {
			fieldType = datatype.FieldTypeGeometry
		}
		nullable, _ := colType.Nullable()
		length, _ := colType.Length()
		precision, scale, _ := colType.DecimalSize()
		field := datatype.FieldInfo{
			Name:            colType.Name(),
			Type:            fieldType,
			NativeType:      nativeType,
			Nullable:        nullable,
			OrdinalPosition: index + 1,
			Size:            int(length),
			Precision:       int(precision),
			Scale:           int(scale),
		}
		tableInfo.Fields = append(tableInfo.Fields, field)
		if geometryColumn == "" && datatype.IsSpatialFieldType(fieldType) {
			geometryColumn = colType.Name()
		}
	}

	if geometryColumn == "" {
		return &serviceModels.QueryServiceOutputContract{Table: tableInfo}, nil
	}

	quotedGeometryColumn := sqldialect.ForEngine(engineType).QuoteIdentifier(geometryColumn)
	metaSQL := fmt.Sprintf(`
		SELECT
			ST_SRID(%s) AS srid,
			ST_GeometryType(%s) AS geom_type
		FROM (%s) AS subquery
		WHERE %s IS NOT NULL
		LIMIT 1
	`, quotedGeometryColumn, quotedGeometryColumn, query, quotedGeometryColumn)

	var sridValue sql.NullInt64
	var geometryTypeValue sql.NullString
	if err := db.WithContext(ctx).Raw(metaSQL).Row().Scan(&sridValue, &geometryTypeValue); err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("detect geometry metadata failed: %w", err)
	}
	geometryType := "Geometry"
	if geometryTypeValue.Valid {
		geometryType = strings.TrimPrefix(geometryTypeValue.String, "ST_")
	}

	extentSQL := fmt.Sprintf(`
		SELECT
			ST_XMin(extent) AS min_x,
			ST_YMin(extent) AS min_y,
			ST_XMax(extent) AS max_x,
			ST_YMax(extent) AS max_y
		FROM (
			SELECT ST_Extent(%s) AS extent
			FROM (%s) AS subquery
		) AS extent_query
	`, quotedGeometryColumn, query)

	var minX, minY, maxX, maxY *float64
	err = db.WithContext(ctx).Raw(extentSQL).Row().Scan(&minX, &minY, &maxX, &maxY)

	var extent *datatype.BoundingBox
	if err == nil && minX != nil && minY != nil && maxX != nil && maxY != nil {
		bbox := datatype.NewBoundingBox(*minX, *minY, *maxX, *maxY)
		extent = &bbox
	}

	column := datatype.GeometryColumnInfo{Name: geometryColumn, GeometryType: geometryType}
	spatialInfo := &datatype.SpatialInfo{
		GeometryColumns:       []datatype.GeometryColumnInfo{column},
		PrimaryGeometryColumn: geometryColumn,
		Extent:                extent,
	}
	if sridValue.Valid && sridValue.Int64 > 0 {
		srid := int(sridValue.Int64)
		crsRef := datatype.EPSGCRSRef(srid)
		spatialInfo.GeometryColumns[0].SRID = &srid
		spatialInfo.GeometryColumns[0].CRSRef = crsRef
		var wkt, proj4 sql.NullString
		if err := db.WithContext(ctx).Raw(`SELECT srtext, proj4text FROM spatial_ref_sys WHERE srid = ?`, srid).Row().Scan(&wkt, &proj4); err == nil {
			definition := strings.TrimSpace(wkt.String)
			encoding := datatype.CRSDefinitionEncodingWKT
			if definition == "" {
				definition = strings.TrimSpace(proj4.String)
				encoding = datatype.CRSDefinitionEncodingProj4
			}
			if definition != "" {
				spatialInfo.CRSDefinitions = []datatype.CRSDefinition{{
					ID:                 crsRef,
					DefinitionEncoding: encoding,
					Definition:         definition,
					Source:             datatype.CRSDefinitionSourcePostGISSpatialRefSys,
				}}
			}
		}
	}

	return &serviceModels.QueryServiceOutputContract{Table: tableInfo, Spatial: spatialInfo}, nil
}

// HealthCheck 健康检查
func (h *ResourceCapabilityHandler) HealthCheck(c *gin.Context) {
	// 检查 System 和 Meta 服务的连通性
	systemOK := h.systemClient != nil
	metaOK := h.metaBaseURL != ""

	status := "healthy"
	if !systemOK || !metaOK {
		status = "degraded"
	}

	c.JSON(http.StatusOK, gin.H{
		"status": status,
		"checks": gin.H{
			"system": systemOK,
			"meta":   metaOK,
		},
		"message": fmt.Sprintf("Resource capability handler status: %s", status),
	})
}
