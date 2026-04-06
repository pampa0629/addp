package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/service"
	"github.com/gin-gonic/gin"
)

type OntologyHandler struct {
	svc                 *service.OntologyService
	neo4jSvc            *service.Neo4jService
	modelImportSvc      *service.ModelImportService
	schemaInferenceSvc  *service.SchemaInferenceService
}

func NewOntologyHandler(svc *service.OntologyService, neo4jSvc *service.Neo4jService, modelImportSvc *service.ModelImportService, schemaInferenceSvc *service.SchemaInferenceService) *OntologyHandler {
	return &OntologyHandler{svc: svc, neo4jSvc: neo4jSvc, modelImportSvc: modelImportSvc, schemaInferenceSvc: schemaInferenceSvc}
}

// List godoc
// @Summary      本体列表
// @Description  获取当前租户的所有本体模型
// @Tags         本体管理
// @Produce      json
// @Security     BearerAuth
// @Success      200 {array}  models.Ontology
// @Failure      500 {object} models.ErrorResponse
// @Router       /ontologies [get]
func (h *OntologyHandler) List(c *gin.Context) {
	tenantID := getTenantID(c)
	ontologies, err := h.svc.List(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ontologies)
}

// Get godoc
// @Summary      本体详情
// @Description  获取本体详情，含实体类型和关系类型
// @Tags         本体管理
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "本体 ID"
// @Success      200 {object} models.OntologyDetail
// @Failure      404 {object} models.ErrorResponse
// @Router       /ontologies/{id} [get]
func (h *OntologyHandler) Get(c *gin.Context) {
	id := parseUintParam(c, "id")
	tenantID := getTenantID(c)
	ontology, err := h.svc.GetDetail(id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ontology not found"})
		return
	}
	c.JSON(http.StatusOK, ontology)
}

// Create godoc
// @Summary      创建本体
// @Description  创建新的本体模型
// @Tags         本体管理
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body models.CreateOntologyRequest true "创建本体请求"
// @Success      201 {object} models.Ontology
// @Failure      400 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Router       /ontologies [post]
func (h *OntologyHandler) Create(c *gin.Context) {
	var req models.CreateOntologyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tenantID := getTenantID(c)
	ontology, err := h.svc.Create(tenantID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, ontology)
}

// Update godoc
// @Summary      更新本体
// @Description  更新本体模型的名称、描述或状态
// @Tags         本体管理
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id      path int                          true "本体 ID"
// @Param        request body models.UpdateOntologyRequest true "更新本体请求"
// @Success      200 {object} models.Ontology
// @Failure      400 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Router       /ontologies/{id} [put]
func (h *OntologyHandler) Update(c *gin.Context) {
	id := parseUintParam(c, "id")
	tenantID := getTenantID(c)
	var req models.UpdateOntologyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ontology, err := h.svc.Update(id, tenantID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ontology)
}

// Delete godoc
// @Summary      删除本体
// @Description  删除本体及其所有实体类型和关系类型
// @Tags         本体管理
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "本体 ID"
// @Success      200 {object} models.SuccessResponse
// @Failure      500 {object} models.ErrorResponse
// @Router       /ontologies/{id} [delete]
func (h *OntologyHandler) Delete(c *gin.Context) {
	id := parseUintParam(c, "id")
	tenantID := getTenantID(c)
	if err := h.svc.Delete(id, tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// --- EntityType handlers ---

// ListEntityTypes godoc
// @Summary      实体类型列表
// @Description  获取本体下的所有实体类型
// @Tags         本体管理
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "本体 ID"
// @Success      200 {array}  models.EntityType
// @Failure      500 {object} models.ErrorResponse
// @Router       /ontologies/{id}/entity-types [get]
func (h *OntologyHandler) ListEntityTypes(c *gin.Context) {
	ontologyID := parseUintParam(c, "id")
	tenantID := getTenantID(c)
	types, err := h.svc.ListEntityTypes(ontologyID, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, types)
}

// CreateEntityType godoc
// @Summary      创建实体类型
// @Description  在本体下创建新的实体类型
// @Tags         本体管理
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id      path int                               true "本体 ID"
// @Param        request body models.CreateEntityTypeRequest   true "创建实体类型请求"
// @Success      201 {object} models.EntityType
// @Failure      400 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Router       /ontologies/{id}/entity-types [post]
func (h *OntologyHandler) CreateEntityType(c *gin.Context) {
	ontologyID := parseUintParam(c, "id")
	tenantID := getTenantID(c)
	var req models.CreateEntityTypeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	et, err := h.svc.CreateEntityType(ontologyID, tenantID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, et)
}

func (h *OntologyHandler) UpdateEntityType(c *gin.Context) {
	ontologyID := parseUintParam(c, "id")
	eid := parseUintParam(c, "eid")
	tenantID := getTenantID(c)
	var req models.UpdateEntityTypeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	et, err := h.svc.UpdateEntityType(eid, ontologyID, tenantID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, et)
}

func (h *OntologyHandler) DeleteEntityType(c *gin.Context) {
	ontologyID := parseUintParam(c, "id")
	eid := parseUintParam(c, "eid")
	tenantID := getTenantID(c)
	if err := h.svc.DeleteEntityType(eid, ontologyID, tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// --- RelationType handlers ---

// ListRelationTypes godoc
// @Summary      关系类型列表
// @Description  获取本体下的所有关系类型
// @Tags         本体管理
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "本体 ID"
// @Success      200 {array}  models.RelationType
// @Failure      500 {object} models.ErrorResponse
// @Router       /ontologies/{id}/relation-types [get]
func (h *OntologyHandler) ListRelationTypes(c *gin.Context) {
	ontologyID := parseUintParam(c, "id")
	tenantID := getTenantID(c)
	types, err := h.svc.ListRelationTypes(ontologyID, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, types)
}

// CreateRelationType godoc
// @Summary      创建关系类型
// @Description  在本体下创建新的关系类型
// @Tags         本体管理
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id      path int                                 true "本体 ID"
// @Param        request body models.CreateRelationTypeRequest   true "创建关系类型请求"
// @Success      201 {object} models.RelationType
// @Failure      400 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Router       /ontologies/{id}/relation-types [post]
func (h *OntologyHandler) CreateRelationType(c *gin.Context) {
	ontologyID := parseUintParam(c, "id")
	tenantID := getTenantID(c)
	var req models.CreateRelationTypeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	rt, err := h.svc.CreateRelationType(ontologyID, tenantID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, rt)
}

func (h *OntologyHandler) UpdateRelationType(c *gin.Context) {
	ontologyID := parseUintParam(c, "id")
	rid := parseUintParam(c, "rid")
	tenantID := getTenantID(c)
	var req models.UpdateRelationTypeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	rt, err := h.svc.UpdateRelationType(rid, ontologyID, tenantID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rt)
}

func (h *OntologyHandler) DeleteRelationType(c *gin.Context) {
	ontologyID := parseUintParam(c, "id")
	rid := parseUintParam(c, "rid")
	tenantID := getTenantID(c)
	if err := h.svc.DeleteRelationType(rid, ontologyID, tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// --- Version handlers ---

// ListVersions godoc
// @Summary      版本列表
// @Description  获取本体的所有版本快照
// @Tags         本体管理
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "本体 ID"
// @Success      200 {array}  models.OntologyVersion
// @Failure      500 {object} models.ErrorResponse
// @Router       /ontologies/{id}/versions [get]
func (h *OntologyHandler) ListVersions(c *gin.Context) {
	ontologyID := parseUintParam(c, "id")
	tenantID := getTenantID(c)
	versions, err := h.svc.ListVersions(ontologyID, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, versions)
}

// CreateVersion godoc
// @Summary      创建版本快照
// @Description  为本体创建版本快照
// @Tags         本体管理
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id      path int                                   true "本体 ID"
// @Param        request body models.CreateOntologyVersionRequest   true "创建版本请求"
// @Success      201 {object} models.OntologyVersion
// @Failure      400 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Router       /ontologies/{id}/versions [post]
func (h *OntologyHandler) CreateVersion(c *gin.Context) {
	ontologyID := parseUintParam(c, "id")
	tenantID := getTenantID(c)
	userID := getUserID(c)
	var req models.CreateOntologyVersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	v, err := h.svc.CreateVersion(ontologyID, tenantID, userID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, v)
}

// --- SyncConstraints handler ---

func (h *OntologyHandler) SyncEntityTypeConstraints(c *gin.Context) {
	ontologyID := parseUintParam(c, "id")
	eid := parseUintParam(c, "eid")
	tenantID := getTenantID(c)

	var req models.SyncConstraintsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 获取实体类型及其属性
	et, err := h.svc.GetEntityType(eid, ontologyID, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "entity type not found"})
		return
	}

	props, err := et.ParsedProperties()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse properties"})
		return
	}

	if err := h.neo4jSvc.SyncConstraints(c.Request.Context(), req.GraphID, tenantID, et.Name, props); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "constraints synced"})
}

// --- Import from Model handlers ---

// ImportPreviewFromModel GET /ontologies/import-preview/from-model
func (h *OntologyHandler) ImportPreviewFromModel(c *gin.Context) {
	if h.modelImportSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "model service not configured"})
		return
	}
	tenantID := getTenantID(c)
	preview, err := h.modelImportSvc.GetImportPreview(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, preview)
}

// ImportFromModel POST /ontologies/:id/import-from-model
func (h *OntologyHandler) ImportFromModel(c *gin.Context) {
	if h.modelImportSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "model service not configured"})
		return
	}
	ontologyID := parseUintParam(c, "id")
	tenantID := getTenantID(c)

	var req service.ImportFromModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Conflict == "" {
		req.Conflict = "skip"
	}

	result, err := h.modelImportSvc.ImportFromModel(ontologyID, tenantID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// --- Infer schema from Neo4j engine (no knowledge graph needed) ---

// ListNeo4jEngines GET /ontologies/neo4j-engines
func (h *OntologyHandler) ListNeo4jEngines(c *gin.Context) {
	if h.schemaInferenceSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "schema inference not available"})
		return
	}
	tenantID := getTenantID(c)
	engines, err := h.schemaInferenceSvc.ListNeo4jEngines(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, engines)
}

// InferSchemaFromEngine GET /ontologies/infer-schema/from-engine?engine_id=X&ontology_id=Y
func (h *OntologyHandler) InferSchemaFromEngine(c *gin.Context) {
	if h.schemaInferenceSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "schema inference not available"})
		return
	}
	tenantID := getTenantID(c)

	var engineID uint
	if _, err := fmt.Sscanf(c.Query("engine_id"), "%d", &engineID); err != nil || engineID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "engine_id required"})
		return
	}

	var ontologyID *uint
	if oidStr := c.Query("ontology_id"); oidStr != "" {
		var oid uint
		if _, err := fmt.Sscanf(oidStr, "%d", &oid); err == nil && oid > 0 {
			ontologyID = &oid
		}
	}

	preview, err := h.schemaInferenceSvc.InferSchemaFromEngine(c.Request.Context(), engineID, tenantID, ontologyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, preview)
}

// ApplyInferredSchemaFromEngine POST /ontologies/:id/infer-schema/from-engine/apply
func (h *OntologyHandler) ApplyInferredSchemaFromEngine(c *gin.Context) {
	if h.schemaInferenceSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "schema inference not available"})
		return
	}
	ontologyID := parseUintParam(c, "id")
	tenantID := getTenantID(c)

	var req service.ApplyInferredSchemaFromEngineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Conflict == "" {
		req.Conflict = "skip"
	}

	result, err := h.schemaInferenceSvc.ApplyInferredSchemaFromEngine(c.Request.Context(), req.EngineID, ontologyID, tenantID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// --- helpers ---

func getTenantID(c *gin.Context) uint {
	if v, exists := c.Get("tenant_id"); exists {
		if id, ok := v.(uint); ok {
			return id
		}
	}
	return 1
}

func getUserID(c *gin.Context) uint {
	if v, exists := c.Get("user_id"); exists {
		if id, ok := v.(uint); ok {
			return id
		}
	}
	return 0
}

func parseUintParam(c *gin.Context, param string) uint {
	id, _ := strconv.ParseUint(c.Param(param), 10, 64)
	return uint(id)
}
