package api

import (
	"net/http"
	"strings"
	"time"

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

// @Summary 原值访问审批工作区 | List plaintext access review workspace
// @Description 按 scope 分页返回未过期待审批申请或审批记录，并支持按处理结果、当前授权状态、申请人、资源或字段及申请时间筛选；本人申请可见但不能自审 | Return paginated unexpired pending requests or review history by scope, with optional outcome, current grant state, requester, resource or field, and request-time filters; self-submitted requests remain visible but cannot be self-approved
// @Tags Protection Access Request
// @Produce json
// @Param scope query string true "视图 pending|history | View pending|history" Enums(pending,history)
// @Param state query string false "审批记录处理结果 approved|rejected|expired，仅 history 可用 | Review history outcome approved|rejected|expired, history only" Enums(approved,rejected,expired)
// @Param authorization_state query string false "当前授权状态 active|expired|revoked|superseded，仅 history 可用 | Current grant state active|expired|revoked|superseded, history only" Enums(active,expired,revoked,superseded)
// @Param requester_search query string false "申请人显示名或用户 ID | Requester display name or user ID"
// @Param resource_search query string false "资源全名或字段路径 | Resource full name or field path"
// @Param created_from query string false "申请时间起点（RFC3339，含边界） | Request creation start time (RFC3339, inclusive)"
// @Param created_to query string false "申请时间终点（RFC3339，含边界） | Request creation end time (RFC3339, inclusive)"
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
	createdFrom, err := parseOptionalRFC3339Query(c, "created_from")
	if err != nil {
		respondError(c, commonapi.ErrBadRequest)
		return
	}
	createdTo, err := parseOptionalRFC3339Query(c, "created_to")
	if err != nil {
		respondError(c, commonapi.ErrBadRequest)
		return
	}
	filter := models.ProtectionAccessRequestReviewFilter{
		Scope:              c.Query("scope"),
		State:              c.Query("state"),
		AuthorizationState: c.Query("authorization_state"),
		RequesterSearch:    c.Query("requester_search"),
		ResourceSearch:     c.Query("resource_search"),
		CreatedFrom:        createdFrom,
		CreatedTo:          createdTo,
	}
	result, err := h.requests.ListReviewQueue(c.Request.Context(), getTenantID(c), getUserID(c), filter, int64(page), int64(pageSize))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 原值访问申请审批详情 | Get plaintext access request review detail
// @Description 按当前租户返回一条申请的最新状态、审计身份与当前审批人的可审批性，供版本冲突后显式重新加载 | Return the latest request state, audit identities, and decision availability for the current reviewer within the tenant, for explicit reload after a version conflict
// @Tags Protection Access Request
// @Produce json
// @Param id path string true "申请 ID | Request ID"
// @Success 200 {object} ProtectionAccessRequestResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.protection_access_request.update"]
// @Router /protection-access-requests/{id} [get]
// @Security BearerAuth
func (h *AccessRequestHandler) GetForReview(c *gin.Context) {
	result, err := h.requests.GetForReview(c.Request.Context(), getTenantID(c), getUserID(c), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func parseOptionalRFC3339Query(c *gin.Context, key string) (*time.Time, error) {
	values := c.Request.URL.Query()[key]
	if len(values) > 1 {
		return nil, commonapi.ErrBadRequest
	}
	if len(values) == 0 || strings.TrimSpace(values[0]) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(values[0]))
	if err != nil {
		return nil, commonapi.ErrBadRequest
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

// @Summary 审批原值访问申请 | Decide plaintext access request
// @Description 申请人不能审批自己的申请，超过申请截止时间后也不能审批；批准后原子生成按用户临时授权和新投影；版本冲突返回 409 + resource_version_conflict，申请过期返回 409 + protection_access_request_expired | The requester cannot decide their own request, and an expired request cannot be decided; approval atomically creates a subject-scoped temporary grant and projection; version conflicts return 409 + resource_version_conflict, and expiration returns 409 + protection_access_request_expired
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
