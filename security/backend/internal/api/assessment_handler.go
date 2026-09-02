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
type ResourceSecurityAssessmentResponse = models.ResourceSecurityAssessmentResponse
type ResourceSecurityAssessmentListResponse = models.ResourceSecurityAssessmentListResponse

type AssessmentHandler struct{ assessments *service.AssessmentService }

func NewAssessmentHandler(assessments *service.AssessmentService) *AssessmentHandler {
	return &AssessmentHandler{assessments: assessments}
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
	result, err := h.assessments.List(c.Request.Context(), getTenantID(c), int64(page), int64(pageSize))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
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
