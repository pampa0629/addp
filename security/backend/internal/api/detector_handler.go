package api

import (
	"net/http"

	"github.com/addp/security/internal/models"
	"github.com/addp/security/internal/service"
	"github.com/gin-gonic/gin"
)

type DetectorHandler struct{ svc *service.DefinitionService }

func NewDetectorHandler(svc *service.DefinitionService) *DetectorHandler {
	return &DetectorHandler{svc: svc}
}

// @Summary 平台内置识别能力 | List built-in detector capabilities
// @Description 返回当前平台版本内置的只读可信识别能力及其目标、证据来源、适用范围、实现方法、隐私边界和已知局限，不返回可执行代码 | Return trusted read-only detector capabilities built into this platform version, including target, evidence source, scope, implementation method, privacy boundary, and known limitations, without executable code
// @Tags Detector
// @Produce json
// @Success 200 {array} models.DetectorCapability
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.detector.read"]
// @Router /detector-capabilities [get]
// @Security BearerAuth
func (h *DetectorHandler) ListCapabilities(c *gin.Context) {
	c.JSON(http.StatusOK, h.svc.ListDetectorCapabilities())
}

// @Summary 检测器绑定列表 | List detector bindings
// @Tags Detector
// @Produce json
// @Success 200 {array} models.Detector
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.detector.read"]
// @Router /detectors [get]
// @Security BearerAuth
func (h *DetectorHandler) List(c *gin.Context) {
	rows, err := h.svc.ListDetectors(getTenantID(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, rows)
}

// @Summary 检测器绑定详情 | Get detector binding
// @Tags Detector
// @Produce json
// @Param id path int true "检测器绑定 ID | Detector binding ID"
// @Success 200 {object} models.Detector
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.detector.read"]
// @Router /detectors/{id} [get]
// @Security BearerAuth
func (h *DetectorHandler) Get(c *gin.Context) {
	id, ok := resourceID(c)
	if !ok {
		return
	}
	row, err := h.svc.GetDetector(id, getTenantID(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

// @Summary 创建检测器绑定 | Create detector binding
// @Description 将一个平台内置识别能力绑定到当前租户的敏感数据类型，并为已有受保护资源安排重新发现 | Bind a built-in capability to a tenant sensitive data type and queue rediscovery for existing protected resources
// @Tags Detector
// @Accept json
// @Produce json
// @Param request body models.DetectorRequest true "检测器绑定 | Detector binding"
// @Success 201 {object} models.Detector
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.detector.create"]
// @Router /detectors [post]
// @Security BearerAuth
func (h *DetectorHandler) Create(c *gin.Context) {
	var request models.DetectorRequest
	if c.ShouldBindJSON(&request) != nil {
		respondError(c, errBadRequest)
		return
	}
	row, err := h.svc.CreateDetector(request, getTenantID(c), getUserID(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, row)
}

// @Summary 更新检测器绑定 | Update detector binding
// @Tags Detector
// @Accept json
// @Produce json
// @Param id path int true "检测器绑定 ID | Detector binding ID"
// @Param request body models.DetectorRequest true "检测器绑定 | Detector binding"
// @Success 200 {object} models.Detector
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.detector.update"]
// @Router /detectors/{id} [put]
// @Security BearerAuth
func (h *DetectorHandler) Update(c *gin.Context) {
	id, ok := resourceID(c)
	if !ok {
		return
	}
	var request models.DetectorRequest
	if c.ShouldBindJSON(&request) != nil {
		respondError(c, errBadRequest)
		return
	}
	row, err := h.svc.UpdateDetector(id, getTenantID(c), getUserID(c), request)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

// @Summary 删除检测器绑定 | Delete detector binding
// @Tags Detector
// @Accept json
// @Produce json
// @Param id path int true "检测器绑定 ID | Detector binding ID"
// @Param request body models.DeleteDetectorRequest true "资源版本 | Resource version"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.detector.delete"]
// @Router /detectors/{id} [delete]
// @Security BearerAuth
func (h *DetectorHandler) Delete(c *gin.Context) {
	id, ok := resourceID(c)
	if !ok {
		return
	}
	var request models.DeleteDetectorRequest
	if c.ShouldBindJSON(&request) != nil {
		respondError(c, errBadRequest)
		return
	}
	if err := h.svc.DeleteDetector(id, getTenantID(c), getUserID(c), request.Version); err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}
