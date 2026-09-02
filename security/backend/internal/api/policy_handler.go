package api

import (
	"net/http"

	commonapi "github.com/addp/common/api"
	"github.com/addp/security/internal/models"
	"github.com/addp/security/internal/service"
	"github.com/gin-gonic/gin"
)

type CreateProtectionPolicyRequest = models.CreateProtectionPolicyRequest
type UpdateProtectionPolicyRequest = models.UpdateProtectionPolicyRequest
type RevokeProtectionPolicyRequest = models.RevokeProtectionPolicyRequest
type ProtectionPolicyResponse = models.ProtectionPolicyResponse
type ProtectionPolicyListResponse = models.ProtectionPolicyListResponse

type PolicyHandler struct{ policies *service.PolicyService }

func NewPolicyHandler(policies *service.PolicyService) *PolicyHandler {
	return &PolicyHandler{policies: policies}
}

// @Summary 保护策略列表 | List protection policies
// @Description 分页返回当前租户针对正式 Assessment 的显式收紧策略及当前不可变修订 | Return explicit tightening policies and current immutable revisions for formal Assessments in the current tenant
// @Tags Protection Policy
// @Produce json
// @Param page query int false "页码 | Page number"
// @Param page_size query int false "每页数量，最大 100 | Page size, maximum 100"
// @Success 200 {object} ProtectionPolicyListResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.policy.read"]
// @Router /protection-policies [get]
// @Security BearerAuth
func (h *PolicyHandler) List(c *gin.Context) {
	page, pageSize := commonapi.ParsePagination(c)
	result, err := h.policies.List(c.Request.Context(), getTenantID(c), int64(page), int64(pageSize))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 创建保护策略 | Create protection policy
// @Description 为正式 Assessment 的 Manager preview 创建只能收紧当前保护基线的策略，并在同一事务重新编译投影 | Create a policy that can only tighten the current baseline for a formal Assessment's Manager preview and recompile the projection in the same transaction
// @Tags Protection Policy
// @Accept json
// @Produce json
// @Param request body CreateProtectionPolicyRequest true "创建请求 | Create request"
// @Success 201 {object} ProtectionPolicyResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.policy.create"]
// @Router /protection-policies [post]
// @Security BearerAuth
func (h *PolicyHandler) Create(c *gin.Context) {
	var request models.CreateProtectionPolicyRequest
	if c.ShouldBindJSON(&request) != nil {
		respondError(c, commonapi.ErrBadRequest)
		return
	}
	result, err := h.policies.Create(c.Request.Context(), getTenantID(c), getUserID(c), request)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// @Summary 保护策略详情 | Get protection policy
// @Description 返回当前状态、当前修订和不可变修订历史 | Return current state, current revision, and immutable revision history
// @Tags Protection Policy
// @Produce json
// @Param id path string true "策略 ID | Policy ID"
// @Success 200 {object} ProtectionPolicyResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.policy.read"]
// @Router /protection-policies/{id} [get]
// @Security BearerAuth
func (h *PolicyHandler) Get(c *gin.Context) {
	result, err := h.policies.Get(c.Request.Context(), getTenantID(c), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 更新保护策略 | Update protection policy
// @Description 使用资源版本追加 active 不可变修订；新效果只能等于或严于当前保护基线 | Append an active immutable revision using resource-version concurrency control; the new effect must be at least as strict as the current baseline
// @Tags Protection Policy
// @Accept json
// @Produce json
// @Param id path string true "策略 ID | Policy ID"
// @Param request body UpdateProtectionPolicyRequest true "更新请求 | Update request"
// @Success 200 {object} ProtectionPolicyResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.policy.update"]
// @Router /protection-policies/{id} [put]
// @Security BearerAuth
func (h *PolicyHandler) Update(c *gin.Context) {
	var request models.UpdateProtectionPolicyRequest
	if c.ShouldBindJSON(&request) != nil {
		respondError(c, commonapi.ErrBadRequest)
		return
	}
	result, err := h.policies.Update(c.Request.Context(), getTenantID(c), getUserID(c), c.Param("id"), request)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 撤销保护策略 | Revoke protection policy
// @Description 使用资源版本追加 revoked 不可变修订并回落到 Assessment 加 ProtectionBaseline；不解除纳管或放行明文 | Append a revoked immutable revision using resource-version concurrency control and fall back to Assessment plus ProtectionBaseline; enrollment and plaintext protection remain in force
// @Tags Protection Policy
// @Accept json
// @Produce json
// @Param id path string true "策略 ID | Policy ID"
// @Param request body RevokeProtectionPolicyRequest true "撤销请求 | Revoke request"
// @Success 200 {object} ProtectionPolicyResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.policy.delete"]
// @Router /protection-policies/{id} [delete]
// @Security BearerAuth
func (h *PolicyHandler) Revoke(c *gin.Context) {
	var request models.RevokeProtectionPolicyRequest
	if c.ShouldBindJSON(&request) != nil {
		respondError(c, commonapi.ErrBadRequest)
		return
	}
	result, err := h.policies.Revoke(c.Request.Context(), getTenantID(c), getUserID(c), c.Param("id"), request)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
