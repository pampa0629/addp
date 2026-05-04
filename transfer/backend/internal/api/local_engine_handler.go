package api

import (
	"errors"
	"net/http"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/service"
	"github.com/gin-gonic/gin"
)

// LocalEngineHandler 提供本地引擎管理 API
type LocalEngineHandler struct {
	service *service.LocalEngineService
}

// NewLocalEngineHandler 构造函数
func NewLocalEngineHandler(service *service.LocalEngineService) *LocalEngineHandler {
	return &LocalEngineHandler{service: service}
}

// LocalEngineRequest 创建或更新请求体
type LocalEngineRequest struct {
	Name           string         `json:"name" binding:"required"`
	EngineType     string         `json:"engine_type" binding:"required"`
	Description    string         `json:"description"`
	IsActive       bool           `json:"is_active"`
	ConnectionInfo models.JSONMap `json:"connection_info" binding:"required"`
}

// LocalEngineUpdateRequest 更新请求
type LocalEngineUpdateRequest struct {
	Name           *string         `json:"name"`
	Description    *string         `json:"description"`
	IsActive       *bool           `json:"is_active"`
	ConnectionInfo *models.JSONMap `json:"connection_info"`
}

// mergeConnectionInfo 合并连接信息，保留敏感字段旧值（当请求中未提供或为空时）
func mergeConnectionInfo(existing, incoming models.JSONMap) models.JSONMap {
	// 如果没有传入新的连接信息，直接返回旧值副本
	if incoming == nil {
		return cloneJSONMap(existing)
	}

	merged := cloneJSONMap(existing)
	if merged == nil {
		merged = make(models.JSONMap)
	}

	// 写入请求中的字段，跳过前端的临时标记
	for k, v := range incoming {
		if k == "_has_password" || k == "_has_secret_key" {
			continue
		}
		merged[k] = v
	}

	preserveSensitiveField(merged, existing, incoming, "password")
	preserveSensitiveField(merged, existing, incoming, "secret_key")

	return merged
}

func preserveSensitiveField(target, existing, incoming models.JSONMap, key string) {
	if incoming == nil {
		return
	}
	incomingVal, provided := incoming[key]
	if !provided {
		if existing != nil {
			if oldVal, ok := existing[key]; ok {
				target[key] = oldVal
			} else {
				delete(target, key)
			}
		} else {
			delete(target, key)
		}
		return
	}

	switch val := incomingVal.(type) {
	case string:
		if val == "" {
			if existing != nil {
				if oldVal, ok := existing[key]; ok {
					target[key] = oldVal
				} else {
					delete(target, key)
				}
			} else {
				delete(target, key)
			}
		}
	case nil:
		if existing != nil {
			if oldVal, ok := existing[key]; ok {
				target[key] = oldVal
			} else {
				delete(target, key)
			}
		} else {
			delete(target, key)
		}
	}
}

func cloneJSONMap(source models.JSONMap) models.JSONMap {
	if source == nil {
		return nil
	}
	cloned := make(models.JSONMap, len(source))
	for k, v := range source {
		cloned[k] = v
	}
	return cloned
}

// List 返回当前租户的资源列表
// @Summary 获取本地引擎列表 | List local engines
// @Tags 本地引擎 | Local Engines
// @Produce json
// @Param engine_type query string false "引擎类型 | Engine type"
// @Success 200 {array} models.LocalEngine
// @Failure 500 {object} map[string]string
// @Router /local-engines [get]
// @Security BearerAuth
func (h *LocalEngineHandler) List(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	engineType := c.Query("engine_type")

	engines, err := h.service.List(tenantID, engineType)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, engines)
}

// ListSystemEngines 返回 System 模块的存储引擎
// @Summary 获取 System 引擎列表 | List system engines
// @Tags 本地引擎 | Local Engines
// @Produce json
// @Param engine_type query string false "引擎类型 | Engine type"
// @Success 200 {array} map[string]interface{}
// @Failure 502 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Router /system-engines [get]
// @Security BearerAuth
func (h *LocalEngineHandler) ListSystemEngines(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	engineType := c.Query("engine_type")

	engines, err := h.service.ListSystemEngines(engineType, tenantID)
	if err != nil {
		if errors.Is(err, service.ErrSystemIntegrationDisabled) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "system integration not available"})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, engines)
}

// Create 新建资源
// @Summary 创建本地引擎 | Create local engine
// @Tags 本地引擎 | Local Engines
// @Accept json
// @Produce json
// @Param request body LocalEngineRequest true "本地引擎请求 | Local engine request"
// @Success 201 {object} models.LocalEngine
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /local-engines [post]
// @Security BearerAuth
func (h *LocalEngineHandler) Create(c *gin.Context) {
	var req LocalEngineRequest
	if !commonAPI.BindJSON(c, &req) {
		return
	}

	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	resource := &models.LocalEngine{
		TenantID:       tenantID,
		Name:           req.Name,
		EngineType:     req.EngineType,
		Description:    req.Description,
		IsActive:       req.IsActive,
		ConnectionInfo: req.ConnectionInfo,
		CreatedBy:      &userID,
	}

	if err := h.service.Create(resource); err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	c.JSON(http.StatusCreated, resource)
}

// Update 更新资源
// @Summary 更新本地引擎 | Update local engine
// @Tags 本地引擎 | Local Engines
// @Accept json
// @Produce json
// @Param id path int true "本地引擎ID | Local engine ID"
// @Param request body LocalEngineUpdateRequest true "本地引擎更新请求 | Local engine update request"
// @Success 200 {object} models.LocalEngine
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /local-engines/{id} [put]
// @Security BearerAuth
func (h *LocalEngineHandler) Update(c *gin.Context) {
	id, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	var req LocalEngineUpdateRequest
	if !commonAPI.BindJSON(c, &req) {
		return
	}

	tenantID := c.GetUint("tenant_id")

	resource, err := h.service.Get(id, tenantID)
	if err != nil {
		commonAPI.NotFoundError(c, "resource not found")
		return
	}

	if req.Name != nil {
		resource.Name = *req.Name
	}
	if req.Description != nil {
		resource.Description = *req.Description
	}
	if req.IsActive != nil {
		resource.IsActive = *req.IsActive
	}
	if req.ConnectionInfo != nil {
		resource.ConnectionInfo = mergeConnectionInfo(resource.ConnectionInfo, *req.ConnectionInfo)
	}

	if err := h.service.Update(resource); err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, resource)
}

// Delete 删除资源
// @Summary 删除本地引擎 | Delete local engine
// @Tags 本地引擎 | Local Engines
// @Produce json
// @Param id path int true "本地引擎ID | Local engine ID"
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /local-engines/{id} [delete]
// @Security BearerAuth
func (h *LocalEngineHandler) Delete(c *gin.Context) {
	id, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	tenantID := c.GetUint("tenant_id")

	if err := h.service.Delete(id, tenantID); err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// TestBeforeCreate 创建前测试连接
// @Summary 创建前测试本地引擎连接 | Test local engine before create
// @Tags 本地引擎 | Local Engines
// @Accept json
// @Produce json
// @Param request body LocalEngineRequest true "本地引擎请求 | Local engine request"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Router /local-engines/test-connection [post]
// @Security BearerAuth
func (h *LocalEngineHandler) TestBeforeCreate(c *gin.Context) {
	var req LocalEngineRequest
	if !commonAPI.BindJSON(c, &req) {
		return
	}

	if err := h.service.TestConnectionBeforeCreate(req.EngineType, req.ConnectionInfo); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// TestExisting 测试已有资源
// @Summary 测试已有本地引擎 | Test existing local engine
// @Tags 本地引擎 | Local Engines
// @Produce json
// @Param id path int true "本地引擎ID | Local engine ID"
// @Success 200 {object} map[string]interface{}
// @Router /local-engines/{id}/test [post]
// @Security BearerAuth
func (h *LocalEngineHandler) TestExisting(c *gin.Context) {
	id, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	tenantID := c.GetUint("tenant_id")

	if err := h.service.TestConnection(id, tenantID); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// Sync 推送到 System
// @Summary 同步本地引擎到 System | Sync local engine to System
// @Tags 本地引擎 | Local Engines
// @Produce json
// @Param id path int true "本地引擎ID | Local engine ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string
// @Failure 502 {object} map[string]string
// @Router /local-engines/{id}/sync [post]
// @Security BearerAuth
func (h *LocalEngineHandler) Sync(c *gin.Context) {
	id, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	tenantID := c.GetUint("tenant_id")

	resource, err := h.service.Get(id, tenantID)
	if err != nil {
		commonAPI.NotFoundError(c, "resource not found")
		return
	}

	result, err := h.service.SyncToSystem(resource)
	if err != nil {
		commonAPI.ErrorResponse(c, http.StatusBadGateway, err.Error())
		return
	}

	c.JSON(http.StatusOK, result)
}

// ListTables 列出本地资源下的表（针对文件型或直连数据源）
// @Summary 列出本地引擎表 | List local engine tables
// @Tags 本地引擎 | Local Engines
// @Produce json
// @Param id path int true "本地引擎ID | Local engine ID"
// @Success 200 {array} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /local-engines/{id}/tables [get]
// @Security BearerAuth
func (h *LocalEngineHandler) ListTables(c *gin.Context) {
	id, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	tenantID := c.GetUint("tenant_id")
	tables, err := h.service.ListTables(id, tenantID)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, tables)
}

// ListFields 列出指定表的字段（含类型信息）
// @Summary 列出本地引擎字段 | List local engine fields
// @Tags 本地引擎 | Local Engines
// @Produce json
// @Param id path int true "本地引擎ID | Local engine ID"
// @Param table query string true "表名 | Table name"
// @Success 200 {array} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /local-engines/{id}/fields [get]
// @Security BearerAuth
func (h *LocalEngineHandler) ListFields(c *gin.Context) {
	id, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	table := c.Query("table")
	if table == "" {
		commonAPI.BadRequestError(c, "missing table parameter")
		return
	}

	tenantID := c.GetUint("tenant_id")
	fields, err := h.service.ListFields(id, tenantID, table)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, fields)
}
