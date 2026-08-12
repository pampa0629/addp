package api

import (
	"net/http"
	"strconv"

	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/standard/i18n"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/service"
	"github.com/gin-gonic/gin"
)

type DomainHandler struct {
	svc *service.DomainService
}

func NewDomainHandler(svc *service.DomainService) *DomainHandler {
	return &DomainHandler{svc: svc}
}

// ListDomains GET /api/model/domains
// @Summary 获取业务域列表 | List business domains
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.domain.read"]
// @Router /domains [get]
// @Security BearerAuth
func (h *DomainHandler) ListDomains(c *gin.Context) {
	tenantID := getTenantID(c)
	tree, err := h.svc.ListDomainsAsTree(tenantID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, tree)
}

// CreateDomain POST /api/model/domains
// @Summary 创建业务域 | Create business domain
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.domain.create"]
// @Router /domains [post]
// @Security BearerAuth
func (h *DomainHandler) CreateDomain(c *gin.Context) {
	var req models.CreateDomainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	domain, err := h.svc.CreateDomain(&req, tenantID, userID)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusCreated, domain)
}

// GetDomain GET /api/model/domains/:id
// @Summary 获取业务域详情 | Get business domain detail
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.domain.read"]
// @Router /domains/{id} [get]
// @Security BearerAuth
func (h *DomainHandler) GetDomain(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}

	tenantID := getTenantID(c)
	domain, err := h.svc.GetDomain(id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, sysi18n.MsgDomainNotFound)})
		return
	}
	c.JSON(http.StatusOK, domain)
}

// UpdateDomain PUT /api/model/domains/:id
// @Summary 更新业务域 | Update business domain
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.domain.update"]
// @Router /domains/{id} [put]
// @Security BearerAuth
func (h *DomainHandler) UpdateDomain(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}

	var req models.UpdateDomainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	domain, err := h.svc.UpdateDomain(id, tenantID, userID, &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, domain)
}

// DeleteDomain DELETE /api/model/domains/:id
// @Summary 删除业务域 | Delete business domain
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 409 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.domain.delete"]
// @Router /domains/{id} [delete]
// @Security BearerAuth
func (h *DomainHandler) DeleteDomain(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}

	tenantID := getTenantID(c)
	if err := h.svc.DeleteDomain(id, tenantID); err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgDeleteSuccess)})
}
