package api

import (
	"net/http"

	commonapi "github.com/addp/common/api"
	"github.com/addp/security/internal/models"
	"github.com/addp/security/internal/service"
	"github.com/gin-gonic/gin"
)

type CreateProtectionAccessRequest = models.CreateProtectionAccessRequest
type DecideProtectionAccessRequest = models.DecideProtectionAccessRequest
type ProtectionAccessRequestResponse = models.ProtectionAccessRequestResponse
type ProtectionAccessRequestListResponse = models.ProtectionAccessRequestListResponse
type ProtectionAccessTargetListResponse = models.ProtectionAccessTargetListResponse

type AccessRequestHandler struct{ requests *service.AccessRequestService }

func NewAccessRequestHandler(requests *service.AccessRequestService) *AccessRequestHandler {
	return &AccessRequestHandler{requests: requests}
}

// @Summary 可申请原值访问的字段 | List plaintext access request targets
// @Description 按当前用户和数据出口返回正式敏感字段及其申请或授权状态，不返回业务值 | Return formal sensitive fields and the current user's request or grant state for one outlet without business values
// @Tags Protection Access Request
// @Produce json
// @Param target_identity query string true "DataItem 指纹 | DataItem fingerprint"
// @Param consumer_owner query string true "消费 Owner | Consumer owner"
// @Param action query string true "出口动作 | Outlet action"
// @Success 200 {object} ProtectionAccessTargetListResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.protection_access_request.read"]
// @Router /protection-access-request-targets [get]
// @Security BearerAuth
func (h *AccessRequestHandler) Targets(c *gin.Context) {
	result, err := h.requests.Targets(c.Request.Context(), getTenantID(c), getUserID(c), c.Query("target_identity"), c.Query("consumer_owner"), c.Query("action"))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 发起原值访问申请 | Create plaintext access request
// @Description 使用当前可信用户主体为一个正式敏感字段申请 Manager 预览原值；申请本身不改变保护 | Request Manager preview plaintext access for one formal sensitive field using the trusted current user; creating a request does not change protection
// @Tags Protection Access Request
// @Accept json
// @Produce json
// @Param request body CreateProtectionAccessRequest true "申请 | Request"
// @Success 201 {object} ProtectionAccessRequestResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.protection_access_request.create"]
// @Router /protection-access-requests [post]
// @Security BearerAuth
func (h *AccessRequestHandler) Create(c *gin.Context) {
	var request models.CreateProtectionAccessRequest
	if c.ShouldBindJSON(&request) != nil {
		respondError(c, commonapi.ErrBadRequest)
		return
	}
	result, err := h.requests.Create(c.Request.Context(), getTenantID(c), getUserID(c), request)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// @Summary 我的原值访问申请 | List my plaintext access requests
// @Description 只返回当前可信用户自己的申请 | Return only requests owned by the trusted current user
// @Tags Protection Access Request
// @Produce json
// @Param page query int false "页码 | Page number"
// @Param page_size query int false "每页数量 | Page size"
// @Success 200 {object} ProtectionAccessRequestListResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.protection_access_request.read"]
// @Router /protection-access-requests [get]
// @Security BearerAuth
func (h *AccessRequestHandler) ListMine(c *gin.Context) {
	page, pageSize := commonapi.ParsePagination(c)
	result, err := h.requests.ListMine(c.Request.Context(), getTenantID(c), getUserID(c), int64(page), int64(pageSize))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 原值访问待审批申请 | List plaintext access review queue
// @Description 返回当前租户等待审批且申请人不是当前用户的申请，申请人不能审批自己的申请 | Return pending requests in the current tenant except requests made by the current user because self-approval is forbidden
// @Tags Protection Access Request
// @Produce json
// @Param page query int false "页码 | Page number"
// @Param page_size query int false "每页数量 | Page size"
// @Success 200 {object} ProtectionAccessRequestListResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.protection_access_request.update"]
// @Router /protection-access-requests/review-queue [get]
// @Security BearerAuth
func (h *AccessRequestHandler) ReviewQueue(c *gin.Context) {
	page, pageSize := commonapi.ParsePagination(c)
	result, err := h.requests.ListReviewQueue(c.Request.Context(), getTenantID(c), getUserID(c), int64(page), int64(pageSize))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 审批原值访问申请 | Decide plaintext access request
// @Description 申请人不能审批自己的申请；批准后原子生成按用户临时授权和新投影 | The requester cannot decide their own request; approval atomically creates a subject-scoped temporary grant and projection
// @Tags Protection Access Request
// @Accept json
// @Produce json
// @Param id path string true "申请 ID | Request ID"
// @Param request body DecideProtectionAccessRequest true "审批 | Decision"
// @Success 200 {object} ProtectionAccessRequestResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.protection_access_request.update"]
// @Router /protection-access-requests/{id}/decisions [post]
// @Security BearerAuth
func (h *AccessRequestHandler) Decide(c *gin.Context) {
	var request models.DecideProtectionAccessRequest
	if c.ShouldBindJSON(&request) != nil {
		respondError(c, commonapi.ErrBadRequest)
		return
	}
	result, err := h.requests.Decide(c.Request.Context(), getTenantID(c), getUserID(c), c.Param("id"), request)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
