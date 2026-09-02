package api

import (
	"net/http"
	"strconv"

	commoni18n "github.com/addp/common/middleware/i18n"
	securityi18n "github.com/addp/security/i18n"
	"github.com/addp/security/internal/models"
	"github.com/addp/security/internal/service"
	"github.com/gin-gonic/gin"
)

type DefinitionHandler struct{ svc *service.DefinitionService }

func NewDefinitionHandler(svc *service.DefinitionService) *DefinitionHandler {
	return &DefinitionHandler{svc: svc}
}

func resourceID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, securityi18n.MsgInvalidID)})
		return 0, false
	}
	return id, true
}
func deleted(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, securityi18n.MsgDeleteSuccess)})
}

// @Summary 安全分类列表 | List security classifications
// @Tags Security Classification
// @Success 200 {array} models.SecurityClassification
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.classification.read"]
// @Router /classifications [get]
// @Security BearerAuth
func (h *DefinitionHandler) ListClassifications(c *gin.Context) {
	rows, err := h.svc.ListClassifications(getTenantID(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, rows)
}

// @Summary 安全分类详情 | Get security classification
// @Tags Security Classification
// @Param id path int true "安全分类 ID | Security classification ID"
// @Success 200 {object} models.SecurityClassification
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.classification.read"]
// @Router /classifications/{id} [get]
// @Security BearerAuth
func (h *DefinitionHandler) GetClassification(c *gin.Context) {
	id, ok := resourceID(c)
	if !ok {
		return
	}
	row, err := h.svc.GetClassification(id, getTenantID(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

// @Summary 创建安全分类 | Create security classification
// @Tags Security Classification
// @Param request body models.DefinitionRequest true "安全分类 | Security classification"
// @Success 201 {object} models.SecurityClassification
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.classification.create"]
// @Router /classifications [post]
// @Security BearerAuth
func (h *DefinitionHandler) CreateClassification(c *gin.Context) {
	var req models.DefinitionRequest
	if c.ShouldBindJSON(&req) != nil {
		respondError(c, errBadRequest)
		return
	}
	row, err := h.svc.CreateClassification(req, getTenantID(c), getUserID(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, row)
}

// @Summary 更新安全分类 | Update security classification
// @Tags Security Classification
// @Param id path int true "安全分类 ID | Security classification ID"
// @Param request body models.DefinitionRequest true "安全分类 | Security classification"
// @Success 200 {object} models.SecurityClassification
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.classification.update"]
// @Router /classifications/{id} [put]
// @Security BearerAuth
func (h *DefinitionHandler) UpdateClassification(c *gin.Context) {
	id, ok := resourceID(c)
	if !ok {
		return
	}
	var req models.DefinitionRequest
	if c.ShouldBindJSON(&req) != nil {
		respondError(c, errBadRequest)
		return
	}
	row, err := h.svc.UpdateClassification(id, getTenantID(c), getUserID(c), req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

// @Summary 删除安全分类 | Delete security classification
// @Tags Security Classification
// @Param id path int true "安全分类 ID | Security classification ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.classification.delete"]
// @Router /classifications/{id} [delete]
// @Security BearerAuth
func (h *DefinitionHandler) DeleteClassification(c *gin.Context) {
	id, ok := resourceID(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteClassification(id, getTenantID(c)); err != nil {
		respondError(c, err)
		return
	}
	deleted(c)
}

// @Summary 安全等级列表 | List security grades
// @Tags Security Grade
// @Success 200 {array} models.SecurityGrade
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.grade.read"]
// @Router /grades [get]
// @Security BearerAuth
func (h *DefinitionHandler) ListGrades(c *gin.Context) {
	rows, err := h.svc.ListGrades(getTenantID(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, rows)
}

// @Summary 安全等级详情 | Get security grade
// @Tags Security Grade
// @Param id path int true "安全等级 ID | Security grade ID"
// @Success 200 {object} models.SecurityGrade
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.grade.read"]
// @Router /grades/{id} [get]
// @Security BearerAuth
func (h *DefinitionHandler) GetGrade(c *gin.Context) {
	id, ok := resourceID(c)
	if !ok {
		return
	}
	row, err := h.svc.GetGrade(id, getTenantID(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

// @Summary 创建安全等级 | Create security grade
// @Tags Security Grade
// @Param request body models.DefinitionRequest true "安全等级 | Security grade"
// @Success 201 {object} models.SecurityGrade
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.grade.create"]
// @Router /grades [post]
// @Security BearerAuth
func (h *DefinitionHandler) CreateGrade(c *gin.Context) {
	var req models.DefinitionRequest
	if c.ShouldBindJSON(&req) != nil {
		respondError(c, errBadRequest)
		return
	}
	row, err := h.svc.CreateGrade(req, getTenantID(c), getUserID(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, row)
}

// @Summary 更新安全等级 | Update security grade
// @Tags Security Grade
// @Param id path int true "安全等级 ID | Security grade ID"
// @Param request body models.DefinitionRequest true "安全等级 | Security grade"
// @Success 200 {object} models.SecurityGrade
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.grade.update"]
// @Router /grades/{id} [put]
// @Security BearerAuth
func (h *DefinitionHandler) UpdateGrade(c *gin.Context) {
	id, ok := resourceID(c)
	if !ok {
		return
	}
	var req models.DefinitionRequest
	if c.ShouldBindJSON(&req) != nil {
		respondError(c, errBadRequest)
		return
	}
	row, err := h.svc.UpdateGrade(id, getTenantID(c), getUserID(c), req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

// @Summary 删除安全等级 | Delete security grade
// @Tags Security Grade
// @Param id path int true "安全等级 ID | Security grade ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.grade.delete"]
// @Router /grades/{id} [delete]
// @Security BearerAuth
func (h *DefinitionHandler) DeleteGrade(c *gin.Context) {
	id, ok := resourceID(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteGrade(id, getTenantID(c)); err != nil {
		respondError(c, err)
		return
	}
	deleted(c)
}

// @Summary 敏感数据类型列表 | List sensitive data types
// @Tags Sensitive Data Type
// @Success 200 {array} models.SensitiveDataType
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.sensitive_data_type.read"]
// @Router /sensitive-data-types [get]
// @Security BearerAuth
func (h *DefinitionHandler) ListTypes(c *gin.Context) {
	rows, err := h.svc.ListTypes(getTenantID(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, rows)
}

// @Summary 敏感数据类型详情 | Get sensitive data type
// @Tags Sensitive Data Type
// @Param id path int true "敏感数据类型 ID | Sensitive data type ID"
// @Success 200 {object} models.SensitiveDataType
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.sensitive_data_type.read"]
// @Router /sensitive-data-types/{id} [get]
// @Security BearerAuth
func (h *DefinitionHandler) GetType(c *gin.Context) {
	id, ok := resourceID(c)
	if !ok {
		return
	}
	row, err := h.svc.GetType(id, getTenantID(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

// @Summary 创建敏感数据类型 | Create sensitive data type
// @Tags Sensitive Data Type
// @Param request body models.SensitiveDataTypeRequest true "敏感数据类型 | Sensitive data type"
// @Success 201 {object} models.SensitiveDataType
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.sensitive_data_type.create"]
// @Router /sensitive-data-types [post]
// @Security BearerAuth
func (h *DefinitionHandler) CreateType(c *gin.Context) {
	var req models.SensitiveDataTypeRequest
	if c.ShouldBindJSON(&req) != nil {
		respondError(c, errBadRequest)
		return
	}
	row, err := h.svc.CreateType(req, getTenantID(c), getUserID(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, row)
}

// @Summary 更新敏感数据类型 | Update sensitive data type
// @Tags Sensitive Data Type
// @Param id path int true "敏感数据类型 ID | Sensitive data type ID"
// @Param request body models.SensitiveDataTypeRequest true "敏感数据类型 | Sensitive data type"
// @Success 200 {object} models.SensitiveDataType
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.sensitive_data_type.update"]
// @Router /sensitive-data-types/{id} [put]
// @Security BearerAuth
func (h *DefinitionHandler) UpdateType(c *gin.Context) {
	id, ok := resourceID(c)
	if !ok {
		return
	}
	var req models.SensitiveDataTypeRequest
	if c.ShouldBindJSON(&req) != nil {
		respondError(c, errBadRequest)
		return
	}
	row, err := h.svc.UpdateType(id, getTenantID(c), getUserID(c), req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

// @Summary 删除敏感数据类型 | Delete sensitive data type
// @Tags Sensitive Data Type
// @Param id path int true "敏感数据类型 ID | Sensitive data type ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.sensitive_data_type.delete"]
// @Router /sensitive-data-types/{id} [delete]
// @Security BearerAuth
func (h *DefinitionHandler) DeleteType(c *gin.Context) {
	id, ok := resourceID(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteType(id, getTenantID(c)); err != nil {
		respondError(c, err)
		return
	}
	deleted(c)
}

// @Summary 保护基线列表 | List protection baselines
// @Tags Protection Baseline
// @Success 200 {array} models.ProtectionBaseline
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.protection_baseline.read"]
// @Router /protection-baselines [get]
// @Security BearerAuth
func (h *DefinitionHandler) ListBaselines(c *gin.Context) {
	rows, err := h.svc.ListBaselines(getTenantID(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, rows)
}

// @Summary 保护基线详情 | Get protection baseline
// @Tags Protection Baseline
// @Param id path int true "保护基线 ID | Protection baseline ID"
// @Success 200 {object} models.ProtectionBaseline
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.protection_baseline.read"]
// @Router /protection-baselines/{id} [get]
// @Security BearerAuth
func (h *DefinitionHandler) GetBaseline(c *gin.Context) {
	id, ok := resourceID(c)
	if !ok {
		return
	}
	row, err := h.svc.GetBaseline(id, getTenantID(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

// @Summary 创建保护基线 | Create protection baseline
// @Description 创建后在同一事务精确重编译引用该敏感类型和等级的纳管投影 | After creation, precisely recompile enrolled projections that reference this sensitive type and grade in the same transaction
// @Tags Protection Baseline
// @Param request body models.ProtectionBaselineRequest true "保护基线 | Protection baseline"
// @Success 201 {object} models.ProtectionBaseline
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.protection_baseline.create"]
// @Router /protection-baselines [post]
// @Security BearerAuth
func (h *DefinitionHandler) CreateBaseline(c *gin.Context) {
	var req models.ProtectionBaselineRequest
	if c.ShouldBindJSON(&req) != nil {
		respondError(c, errBadRequest)
		return
	}
	row, err := h.svc.CreateBaseline(req, getTenantID(c), getUserID(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, row)
}

// @Summary 更新保护基线 | Update protection baseline
// @Description 完整更新后在同一事务重编译更新前后绑定范围的并集；每个受影响纳管只编译一次 | After full update, recompile the union of old and new binding scopes in the same transaction; each affected enrollment is compiled once
// @Tags Protection Baseline
// @Param id path int true "保护基线 ID | Protection baseline ID"
// @Param request body models.ProtectionBaselineRequest true "保护基线 | Protection baseline"
// @Success 200 {object} models.ProtectionBaseline
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.protection_baseline.update"]
// @Router /protection-baselines/{id} [put]
// @Security BearerAuth
func (h *DefinitionHandler) UpdateBaseline(c *gin.Context) {
	id, ok := resourceID(c)
	if !ok {
		return
	}
	var req models.ProtectionBaselineRequest
	if c.ShouldBindJSON(&req) != nil {
		respondError(c, errBadRequest)
		return
	}
	row, err := h.svc.UpdateBaseline(id, getTenantID(c), getUserID(c), req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

// @Summary 删除保护基线 | Delete protection baseline
// @Description 使用资源版本删除并在同一事务重编译受影响投影；没有其他有效规则时回到 enrolling 资源级拒绝 | Delete with resource-version concurrency control and recompile affected projections in the same transaction; fall back to the enrolling resource-level deny when no other effective rule remains
// @Tags Protection Baseline
// @Accept json
// @Produce json
// @Param id path int true "保护基线 ID | Protection baseline ID"
// @Param request body models.DeleteProtectionBaselineRequest true "删除版本 | Delete version"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.protection_baseline.delete"]
// @Router /protection-baselines/{id} [delete]
// @Security BearerAuth
func (h *DefinitionHandler) DeleteBaseline(c *gin.Context) {
	id, ok := resourceID(c)
	if !ok {
		return
	}
	var request models.DeleteProtectionBaselineRequest
	if c.ShouldBindJSON(&request) != nil {
		respondError(c, errBadRequest)
		return
	}
	if err := h.svc.DeleteBaseline(id, getTenantID(c), request.Version); err != nil {
		respondError(c, err)
		return
	}
	deleted(c)
}
