package api

import (
	"net/http"

	commonapi "github.com/addp/common/api"
	"github.com/addp/security/internal/models"
	"github.com/addp/security/internal/service"
	"github.com/gin-gonic/gin"
)

type FindingReviewRequest = models.FindingReviewRequest
type FindingReviewResponse = models.FindingReviewResponse
type AssessmentRevisionRequest = models.AssessmentRevisionRequest
type CreateManualAssessmentRequest = models.CreateManualAssessmentRequest
type RevokeAssessmentRequest = models.RevokeAssessmentRequest
type AssessmentComponentListResponse = models.AssessmentComponentListResponse
type ResourceSecurityAssessmentResponse = models.ResourceSecurityAssessmentResponse
type ResourceSecurityAssessmentListResponse = models.ResourceSecurityAssessmentListResponse

type AssessmentHandler struct{ assessments *service.AssessmentService }

func NewAssessmentHandler(assessments *service.AssessmentService) *AssessmentHandler {
	return &AssessmentHandler{assessments: assessments}
}

// @Summary 可人工指定组件列表 | List components available for manual assessment
// @Description 使用 Security 服务身份实时读取该纳管资源的 Meta 当前字段事实，只返回尚未形成正式 Assessment 且不含业务值的可选组件；已经确认、调整或撤销过的组件均不再列出，也不接受自由文本字段路径 | Read the enrolled resource's current Meta field facts with the Security service identity and return only value-free selectable components that have never formed a formal Assessment; components that were confirmed, adjusted, or revoked are omitted, and free-form component paths are not accepted
// @Tags Security Assessment
// @Produce json
// @Param id path string true "Enrollment ID"
// @Success 200 {object} AssessmentComponentListResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.enrollment.read","security.assessment.read"]
// @Router /protection-enrollments/{id}/components [get]
// @Security BearerAuth
func (h *AssessmentHandler) ListComponents(c *gin.Context) {
	result, err := h.assessments.ListComponents(c.Request.Context(), getTenantID(c), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 复核敏感发现 | Review sensitive finding
// @Description 对不可变 Finding 进行一次确认、调整或驳回；确认和调整形成正式 Assessment revision | Confirm, adjust, or reject an immutable Finding once; confirm and adjust create a formal Assessment revision
// @Tags Sensitive Finding
// @Accept json
// @Produce json
// @Param id path string true "Finding ID"
// @Param request body FindingReviewRequest true "复核请求 | Review request"
// @Success 201 {object} FindingReviewResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.finding.update"]
// @Router /findings/{id}/reviews [post]
// @Security BearerAuth
func (h *AssessmentHandler) ReviewFinding(c *gin.Context) {
	var request models.FindingReviewRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondError(c, commonapi.ErrBadRequest)
		return
	}
	result, err := h.assessments.ReviewFinding(c.Request.Context(), getTenantID(c), getUserID(c), c.Param("id"), request)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// @Summary 正式安全评估列表 | List formal security assessments
// @Description 分页返回当前租户的正式资源安全评估及当前修订 | Return formal resource security assessments and their current revisions
// @Tags Security Assessment
// @Produce json
// @Param enrollment_id query string false "纳管 ID | Enrollment ID"
// @Param page query int false "页码 | Page number"
// @Param page_size query int false "每页数量，最大 100 | Page size, maximum 100"
// @Success 200 {object} ResourceSecurityAssessmentListResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.assessment.read"]
// @Router /assessments [get]
// @Security BearerAuth
func (h *AssessmentHandler) List(c *gin.Context) {
	page, pageSize := commonapi.ParsePagination(c)
	result, err := h.assessments.List(c.Request.Context(), getTenantID(c), c.Query("enrollment_id"), int64(page), int64(pageSize))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 人工指定敏感组件 | Create manual security assessment
// @Description 从服务端校验过的 Meta 当前组件创建来源为 manual 的正式敏感评估，并通过唯一编译器发布保护投影；浏览器不能提交组件结构或自由文本路径 | Create a formal sensitive assessment with manual source from a server-validated current Meta component and publish protection projections through the only compiler; clients cannot submit component structures or free-form paths
// @Tags Security Assessment
// @Accept json
// @Produce json
// @Param request body CreateManualAssessmentRequest true "人工指定请求 | Manual assessment request"
// @Success 201 {object} ResourceSecurityAssessmentResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.assessment.create"]
// @Router /assessments [post]
// @Security BearerAuth
func (h *AssessmentHandler) CreateManual(c *gin.Context) {
	var request models.CreateManualAssessmentRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondError(c, commonapi.ErrBadRequest)
		return
	}
	result, err := h.assessments.CreateManual(c.Request.Context(), getTenantID(c), getUserID(c), request)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// @Summary 正式安全评估详情 | Get formal security assessment
// @Description 返回当前修订和不可变修订历史 | Return the current revision and immutable revision history
// @Tags Security Assessment
// @Produce json
// @Param id path string true "Assessment ID"
// @Success 200 {object} ResourceSecurityAssessmentResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.assessment.read"]
// @Router /assessments/{id} [get]
// @Security BearerAuth
func (h *AssessmentHandler) Get(c *gin.Context) {
	result, err := h.assessments.Get(c.Request.Context(), getTenantID(c), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 新增安全评估修订 | Create security assessment revision
// @Description 使用资源版本并发控制，在同一 Assessment 聚合追加不可变正式修订并重新编译保护投影 | Append an immutable formal revision with resource-version concurrency control and recompile the protection projection
// @Tags Security Assessment
// @Accept json
// @Produce json
// @Param id path string true "Assessment ID"
// @Param request body AssessmentRevisionRequest true "修订请求 | Revision request"
// @Success 201 {object} ResourceSecurityAssessmentResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.assessment.update"]
// @Router /assessments/{id}/revisions [post]
// @Security BearerAuth
func (h *AssessmentHandler) Revise(c *gin.Context) {
	var request models.AssessmentRevisionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondError(c, commonapi.ErrBadRequest)
		return
	}
	result, err := h.assessments.Revise(c.Request.Context(), getTenantID(c), getUserID(c), c.Param("id"), request)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// @Summary 撤销正式安全评估 | Revoke formal security assessment
// @Description 携带资源版本追加 not_sensitive 不可变修订并通过唯一编译器移除当前字段规则；不删除 Finding、复核或历史评估 | Append an immutable not_sensitive revision with resource-version concurrency and remove the current field rule through the only compiler; findings, reviews, and assessment history are retained
// @Tags Security Assessment
// @Accept json
// @Produce json
// @Param id path string true "Assessment ID"
// @Param request body RevokeAssessmentRequest true "撤销请求 | Revocation request"
// @Success 200 {object} ResourceSecurityAssessmentResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.assessment.update"]
// @Router /assessments/{id} [delete]
// @Security BearerAuth
func (h *AssessmentHandler) Revoke(c *gin.Context) {
	var request models.RevokeAssessmentRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondError(c, commonapi.ErrBadRequest)
		return
	}
	result, err := h.assessments.Revoke(c.Request.Context(), getTenantID(c), getUserID(c), c.Param("id"), request)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
