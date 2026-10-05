package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	commonapi "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

type HostNodeHandler struct{ service *service.HostNodeService }

func hostNodeError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	key := "system.host_node.failed"
	code := "host_node_failed"
	switch {
	case errors.Is(err, commonapi.ErrBadRequest):
		status = 400
		key = "system.host_node.invalid"
		code = "host_node_invalid"
	case errors.Is(err, commonapi.ErrNotFound):
		status = 404
		key = "system.host_node.not_found"
		code = "host_node_not_found"
	case errors.Is(err, commonapi.ErrConflict):
		status = 409
		key = "system.host_node.version_conflict"
		code = "resource_version_conflict"
	case errors.Is(err, commonapi.ErrUnauthorized), errors.Is(err, commonapi.ErrForbidden):
		respondIAMError(c, err)
		return
	}
	c.JSON(status, gin.H{"error": commoni18n.T(c, key), "error_code": code})
}
func bindHostNode(c *gin.Context, dst interface{}) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 65536)
	if err := commonapi.BindOptionalJSONStrict(c, dst); err != nil {
		return commonapi.ErrBadRequest
	}
	return nil
}

// List godoc
// @Summary 查询节点台账 | List managed host nodes
// @Description 平台 User 节点台账；不执行主机操作或资源采集 | Platform user node inventory without host operations or resource collection
// @Tags 节点台账 | Host Nodes
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页数量 | Page size" default(20)
// @Param search query string false "名称或地址片段 | Name or address fragment"
// @Success 200 {object} models.HostNodePage
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["platform.host_node.read"]
// @Router /platform/host_nodes [get]
func (h *HostNodeHandler) List(c *gin.Context) {
	if _, err := iamPlatformUserActor(c); err != nil {
		hostNodeError(c, err)
		return
	}
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		hostNodeError(c, commonapi.ErrBadRequest)
		return
	}
	for key := range query {
		if key != "page" && key != "page_size" && key != "search" {
			hostNodeError(c, commonapi.ErrBadRequest)
			return
		}
		if len(query[key]) != 1 {
			hostNodeError(c, commonapi.ErrBadRequest)
			return
		}
	}
	page, size := 1, 20
	if values, exists := query["page"]; exists {
		raw := values[0]
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			hostNodeError(c, commonapi.ErrBadRequest)
			return
		}
		page = parsed
	}
	if values, exists := query["page_size"]; exists {
		raw := values[0]
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			hostNodeError(c, commonapi.ErrBadRequest)
			return
		}
		size = parsed
	}
	result, err := h.service.List(c.Request.Context(), page, size, query.Get("search"))
	if err != nil {
		hostNodeError(c, err)
		return
	}
	c.JSON(200, result)
}

// Get godoc
// @Summary 查看节点台账 | Get managed host node
// @Description 平台 User 节点台账；不执行主机操作或资源采集 | Platform user node inventory without host operations or resource collection
// @Tags 节点台账 | Host Nodes
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param node_id path string true "节点 UUID | Node UUID"
// @Success 200 {object} models.HostNode
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["platform.host_node.read"]
// @Router /platform/host_nodes/{node_id} [get]
func (h *HostNodeHandler) Get(c *gin.Context) {
	if _, err := iamPlatformUserActor(c); err != nil {
		hostNodeError(c, err)
		return
	}
	node, err := h.service.Get(c.Request.Context(), c.Param("node_id"))
	if err != nil {
		hostNodeError(c, err)
		return
	}
	c.JSON(200, node)
}

// Create godoc
// @Summary 创建纳管节点 | Create managed host node
// @Description 平台 User 节点台账；不执行主机操作或资源采集 | Platform user node inventory without host operations or resource collection
// @Tags 节点台账 | Host Nodes
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body models.HostNodeInput true "节点台账与允许集合 | Node inventory and allowed bindings"
// @Success 201 {object} models.HostNode
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["platform.host_node.create"]
// @Router /platform/host_nodes [post]
func (h *HostNodeHandler) Create(c *gin.Context) {
	if _, err := iamPlatformUserActor(c); err != nil {
		hostNodeError(c, err)
		return
	}
	var input models.HostNodeInput
	if err := bindHostNode(c, &input); err != nil {
		hostNodeError(c, err)
		return
	}
	node, err := h.service.Create(c.Request.Context(), input, iamAuditMetadataWithStatus(c, 201))
	if err != nil {
		hostNodeError(c, err)
		return
	}
	c.JSON(201, node)
}

// Update godoc
// @Summary 更新节点台账 | Update managed host node
// @Description 平台 User 节点台账；不执行主机操作或资源采集 | Platform user node inventory without host operations or resource collection
// @Tags 节点台账 | Host Nodes
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param node_id path string true "节点 UUID | Node UUID"
// @Param request body models.HostNodeUpdateRequest true "完整替换及节点版本 | Full replacement and node version"
// @Success 200 {object} models.HostNode
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["platform.host_node.update"]
// @Router /platform/host_nodes/{node_id} [put]
func (h *HostNodeHandler) Update(c *gin.Context) {
	if _, err := iamPlatformUserActor(c); err != nil {
		hostNodeError(c, err)
		return
	}
	var input models.HostNodeUpdateRequest
	if err := bindHostNode(c, &input); err != nil {
		hostNodeError(c, err)
		return
	}
	node, err := h.service.Update(c.Request.Context(), c.Param("node_id"), input, iamAuditMetadataWithStatus(c, 200))
	if err != nil {
		hostNodeError(c, err)
		return
	}
	c.JSON(200, node)
}
