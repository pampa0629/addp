package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	commonExecution "github.com/addp/common/execution"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/common/resourcetree"
	metaErrors "github.com/addp/meta/internal/errors"
	"github.com/addp/meta/internal/models"
	"github.com/gin-gonic/gin"
)

var (
	_ = resourcetree.TreeNode{}
	_ = models.ResourceTreeAncestorsResponse{}
	_ = models.ResourceTreeRefreshResponse{}
	_ = models.ResourceTreeSearchResponse{}
)

// GetResourceTree 获取标准资源树
// @Summary 获取标准资源树 | Get resource tree
// @Description 获取指定引擎的标准资源树视图，返回 common/resourcetree.TreeNode | Get standard resource tree for an engine
// @Tags Meta Resource Tree
// @Produce json
// @Param engine_id path int true "存储引擎ID | Engine ID"
// @Param expand_depth query int false "展开深度，-1 表示全部展开 | Expand depth, -1 for full tree"
// @Success 200 {object} resourcetree.TreeNode "资源树 | Resource tree"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Failure 404 {object} map[string]interface{} "资源不存在 | Resource not found"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /resource-tree/{engine_id} [get]
// @Security BearerAuth
func (h *Handler) GetResourceTree(c *gin.Context) {
	engineID, ok := parseUintPath(c, "engine_id")
	if !ok {
		return
	}
	tenantID, err := h.effectiveTenantIDForEngine(c, engineID)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}
	tree, err := h.resourceTreeService.GetTree(c.Request.Context(), tenantID, engineID, parseExpandDepth(c))
	if err != nil {
		h.handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, tree)
}

// GetResourceTreeNode 获取标准资源树节点
// @Summary 获取标准资源树节点 | Get resource tree node
// @Description 按 locator 获取资源树节点及直接子级，返回 common/resourcetree.TreeNode | Get resource tree node and direct children by locator
// @Tags Meta Resource Tree
// @Produce json
// @Param engine_id path int true "存储引擎ID | Engine ID"
// @Param locator query string true "ResourceLocator URI"
// @Success 200 {object} resourcetree.TreeNode "资源树节点 | Resource tree node"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Failure 404 {object} map[string]interface{} "资源不存在 | Resource not found"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /resource-tree/{engine_id}/node [get]
// @Security BearerAuth
func (h *Handler) GetResourceTreeNode(c *gin.Context) {
	engineID, ok := parseUintPath(c, "engine_id")
	if !ok {
		return
	}
	locator := c.Query("locator")
	if locator == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing locator parameter"})
		return
	}
	tenantID, err := h.effectiveTenantIDForEngine(c, engineID)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}
	node, err := h.resourceTreeService.GetNode(c.Request.Context(), tenantID, engineID, locator)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, node)
}

// GetResourceTreeAncestors 获取标准资源树祖先链
// @Summary 获取标准资源树祖先链 | Get resource tree ancestors
// @Description 按 locator 获取 root 到目标资源的标准资源树祖先链，并重写为当前 Meta 事实 locator | Get resource tree ancestor chain by locator and rewrite target locator from current Meta facts
// @Tags Meta Resource Tree
// @Produce json
// @Param engine_id path int true "存储引擎ID | Engine ID"
// @Param locator query string true "ResourceLocator URI"
// @Success 200 {object} models.ResourceTreeAncestorsResponse "资源树祖先链 | Resource tree ancestors"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Failure 404 {object} map[string]interface{} "资源不存在 | Resource not found"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /resource-tree/{engine_id}/ancestors [get]
// @Security BearerAuth
func (h *Handler) GetResourceTreeAncestors(c *gin.Context) {
	engineID, ok := parseUintPath(c, "engine_id")
	if !ok {
		return
	}
	locator := c.Query("locator")
	if locator == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing locator parameter"})
		return
	}
	tenantID, err := h.effectiveTenantIDForEngine(c, engineID)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}
	result, err := h.resourceTreeService.GetAncestors(c.Request.Context(), tenantID, engineID, locator)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// SearchResourceTree 搜索标准资源树
// @Summary 搜索标准资源树 | Search resource tree
// @Description 在指定引擎的标准资源树中按关键词搜索节点 | Search nodes in a standard resource tree by keyword
// @Tags Meta Resource Tree
// @Produce json
// @Param engine_id path int true "存储引擎ID | Engine ID"
// @Param q query string true "搜索关键词，至少 2 个字符 | Search keyword, at least 2 characters"
// @Param node_types query string false "节点类型过滤，逗号分隔 | Node type filter, comma-separated"
// @Param limit query int false "返回数量限制，默认 50，最大 100 | Result limit, default 50, max 100"
// @Success 200 {object} models.ResourceTreeSearchResponse "搜索结果 | Search results"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Failure 404 {object} map[string]interface{} "资源不存在 | Resource not found"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /resource-tree/{engine_id}/search [get]
// @Security BearerAuth
func (h *Handler) SearchResourceTree(c *gin.Context) {
	engineID, ok := parseUintPath(c, "engine_id")
	if !ok {
		return
	}
	keyword := strings.TrimSpace(c.Query("q"))
	if len([]rune(keyword)) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "search keyword must be at least 2 characters"})
		return
	}
	tenantID, err := h.effectiveTenantIDForEngine(c, engineID)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}
	result, err := h.resourceTreeService.Search(c.Request.Context(), tenantID, engineID, keyword, parseNodeTypes(c), parseSearchLimit(c))
	if err != nil {
		h.handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// RefreshResourceTreeNode 刷新标准资源树节点
// @Summary 刷新标准资源树节点 | Refresh resource tree node
// @Description 按 locator 提交一次后台深度扫描，刷新 Meta 资源树事实 | Submit a background deep scan by locator to refresh Meta resource tree facts
// @Tags Meta Resource Tree
// @Produce json
// @Param engine_id path int true "存储引擎ID | Engine ID"
// @Param locator query string true "ResourceLocator URI"
// @Success 202 {object} map[string]interface{} "已提交的刷新运行 | Submitted refresh run"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 401 {object} map[string]interface{} "未授权 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Failure 404 {object} map[string]interface{} "资源不存在 | Resource not found"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /resource-tree/{engine_id}/refresh [post]
// @Security BearerAuth
func (h *Handler) RefreshResourceTreeNode(c *gin.Context) {
	if h.executionService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "execution service not available"})
		return
	}
	engineID, ok := parseUintPath(c, "engine_id")
	if !ok {
		return
	}
	locatorURI := c.Query("locator")
	if locatorURI == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing locator parameter"})
		return
	}
	loc, err := resourcetree.ParseURI(locatorURI)
	if err != nil {
		h.handleServiceError(c, fmt.Errorf("%w: %v", metaErrors.ErrInvalidResourceLocator, err))
		return
	}
	if loc.EngineID != engineID {
		h.handleServiceError(c, fmt.Errorf("%w: locator engine_id %d does not match requested engine_id %d", metaErrors.ErrInvalidResourceLocator, loc.EngineID, engineID))
		return
	}
	if (loc.ItemID == nil || *loc.ItemID == 0) && (loc.NodeID == nil || *loc.NodeID == 0) {
		h.handleServiceError(c, fmt.Errorf("%w: resource tree refresh requires locator node_id or item_id", metaErrors.ErrInvalidResourceLocator))
		return
	}

	token, hasBearerToken := extractBearerToken(c)
	if !hasBearerToken && strings.TrimSpace(c.GetHeader("X-Internal-API-Key")) == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization token"})
		return
	}

	run, err := h.executionService.CreateManualRun(c.Request.Context(), commonAuth.GetTenantID(c), commonAuth.GetUserID(c), token, refreshResourceTreeScanRequest(loc))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"data": models.ResourceTreeRefreshResponse{
			Locator: locatorURI,
			Run:     run,
		},
	})
}

func refreshResourceTreeScanRequest(loc *resourcetree.ResourceLocator) *models.ScanRequest {
	req := &models.ScanRequest{
		EngineID:    loc.EngineID,
		ScanDepth:   "deep",
		Force:       true,
		TriggerType: commonExecution.TriggerTypeManual,
		Source:      commonExecution.ModuleMeta,
	}
	if loc.ItemID != nil && *loc.ItemID > 0 {
		req.ItemID = *loc.ItemID
		return req
	}
	if loc.NodeID != nil && *loc.NodeID > 0 {
		req.NodeID = *loc.NodeID
	}
	return req
}

func parseUintPath(c *gin.Context, key string) (uint, bool) {
	raw := c.Param(key)
	value, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + key})
		return 0, false
	}
	return uint(value), true
}

func parseExpandDepth(c *gin.Context) int {
	raw := c.Query("expand_depth")
	if raw == "" {
		return 1
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 1
	}
	return value
}

func parseNodeTypes(c *gin.Context) []string {
	raw := c.Query("node_types")
	if raw == "" {
		return nil
	}
	return strings.Split(raw, ",")
}

func parseSearchLimit(c *gin.Context) int {
	raw := c.Query("limit")
	if raw == "" {
		return 50
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 50
	}
	return value
}
