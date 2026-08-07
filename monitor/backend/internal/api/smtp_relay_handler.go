package api

import (
	"errors"
	"net/http"

	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	monitori18n "github.com/addp/monitor/i18n"
	"github.com/addp/monitor/internal/repository"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
)

type SMTPRelayHandler struct{ service *service.SMTPRelayService }

func NewSMTPRelayHandler(value *service.SMTPRelayService) *SMTPRelayHandler {
	return &SMTPRelayHandler{service: value}
}

// Get godoc
// @Summary 获取 SMTP Relay | Get SMTP relay
// @Tags 配置管理 | Configuration Management
// @Produce json
// @Security BearerAuth
// @Success 200 {object} service.SMTPRelayResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.configuration.read"]
// @Router /settings/smtp-relay [get]
func (h *SMTPRelayHandler) Get(c *gin.Context) {
	value, err := h.service.Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, monitori18n.MsgSMTPRelayLoadFailed)})
		return
	}
	c.JSON(http.StatusOK, value)
}

// Update godoc
// @Summary 更新 SMTP Relay | Update SMTP relay
// @Tags 配置管理 | Configuration Management
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body service.UpdateSMTPRelayInput true "SMTP Relay"
// @Success 200 {object} service.SMTPRelayResponse
// @Failure 409 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.configuration.update"]
// @Router /settings/smtp-relay [put]
func (h *SMTPRelayHandler) Update(c *gin.Context) {
	var input service.UpdateSMTPRelayInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, monitori18n.MsgSMTPRelayInvalid)})
		return
	}
	principalID, ok := commonAuth.PrincipalIDFromGin(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, monitori18n.MsgConfigurationAuthentication)})
		return
	}
	value, err := h.service.Update(c.Request.Context(), input, uint(principalID))
	if errors.Is(err, repository.ErrSMTPRelayVersionConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, monitori18n.MsgConfigurationConflict)})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, value)
}

type smtpRelayCredentialRequest struct {
	Credential string `json:"credential" binding:"required"`
}

// SetCredential godoc
// @Summary 设置 SMTP 凭据 | Set SMTP credential
// @Tags 配置管理 | Configuration Management
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body api.smtpRelayCredentialRequest true "SMTP credential"
// @Success 200 {object} service.SMTPRelayCredentialStatus
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.configuration.update"]
// @Router /settings/smtp-relay/credential [put]
func (h *SMTPRelayHandler) SetCredential(c *gin.Context) {
	var input smtpRelayCredentialRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, monitori18n.MsgSMTPRelayCredentialRequired)})
		return
	}
	principalID, ok := commonAuth.PrincipalIDFromGin(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, monitori18n.MsgConfigurationAuthentication)})
		return
	}
	value, err := h.service.SetCredential(c.Request.Context(), input.Credential, uint(principalID))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, value)
}

// DeleteCredential godoc
// @Summary 删除 SMTP 凭据 | Delete SMTP credential
// @Tags 配置管理 | Configuration Management
// @Produce json
// @Security BearerAuth
// @Success 200 {object} service.SMTPRelayCredentialStatus
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.configuration.update"]
// @Router /settings/smtp-relay/credential [delete]
func (h *SMTPRelayHandler) DeleteCredential(c *gin.Context) {
	principalID, ok := commonAuth.PrincipalIDFromGin(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, monitori18n.MsgConfigurationAuthentication)})
		return
	}
	value, err := h.service.DeleteCredential(c.Request.Context(), uint(principalID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, value)
}
