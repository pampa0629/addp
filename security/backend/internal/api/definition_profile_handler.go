package api

import (
	"net/http"

	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/security/internal/models"
	"github.com/addp/security/internal/service"
	"github.com/gin-gonic/gin"
)

type DefinitionProfileHandler struct{ svc *service.DefinitionService }

func NewDefinitionProfileHandler(svc *service.DefinitionService) *DefinitionProfileHandler {
	return &DefinitionProfileHandler{svc: svc}
}

// @Summary 推荐定义方案列表 | List recommended definition profiles
// @Description 返回平台随版本提供的只读分类与等级模板；读取不会修改租户数据 | Return read-only classification and grade templates installed with the platform; listing does not mutate tenant data
// @Tags Security Definition Profile
// @Produce json
// @Success 200 {array} models.DefinitionProfile
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.classification.read", "security.grade.read"]
// @Router /definition-profiles [get]
// @Security BearerAuth
func (h *DefinitionProfileHandler) List(c *gin.Context) {
	c.JSON(http.StatusOK, h.svc.ListDefinitionProfiles())
}

// @Summary 应用推荐定义方案 | Apply recommended definition profile
// @Description 在单个事务中按稳定编码补齐缺失的分类与等级，不覆盖已有同编码定义 | Transactionally add missing classifications and grades by stable code without overwriting existing definitions
// @Tags Security Definition Profile
// @Accept json
// @Produce json
// @Param request body models.DefinitionProfileApplicationRequest true "推荐定义方案 | Recommended definition profile"
// @Success 200 {object} models.DefinitionProfileApplication
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.classification.create", "security.grade.create"]
// @Router /definition-profile-applications [post]
// @Security BearerAuth
func (h *DefinitionProfileHandler) Apply(c *gin.Context) {
	var request models.DefinitionProfileApplicationRequest
	if c.ShouldBindJSON(&request) != nil {
		respondError(c, errBadRequest)
		return
	}
	result, err := h.svc.ApplyDefinitionProfile(request.ProfileKey, commoni18n.GetLang(c), getTenantID(c), getUserID(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
