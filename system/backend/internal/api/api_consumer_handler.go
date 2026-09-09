package api

import (
	"net/http"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

type APIConsumerHandler struct {
	service *service.APIConsumerService
}

func NewAPIConsumerHandler(service *service.APIConsumerService) *APIConsumerHandler {
	return &APIConsumerHandler{service: service}
}

// Create godoc
// @Summary 创建 API 消费方 | Create API consumer
// @Tags API 消费方 | API Consumers
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body models.CreateAPIConsumerRequest true "API 消费方 | API consumer"
// @Success 201 {object} models.APIConsumer
// @Failure 400 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.api_consumer.create"]
// @Router /tenant/api-consumers [post]
func (h *APIConsumerHandler) Create(c *gin.Context) {
	var req models.CreateAPIConsumerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		commonapi.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}
	principalID, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	consumer, err := h.service.Create(&req, tenantID, principalID)
	if err != nil {
		commonapi.RespondError(c, commonapi.MapErrorToHTTPStatus(err), err.Error())
		return
	}
	commonapi.RespondCreated(c, consumer)
}

// List godoc
// @Summary 获取 API 消费方列表 | List API consumers
// @Tags API 消费方 | API Consumers
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.api_consumer.read"]
// @Router /tenant/api-consumers [get]
func (h *APIConsumerHandler) List(c *gin.Context) {
	_, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	consumers, err := h.service.List(tenantID)
	if err != nil {
		commonapi.RespondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	commonapi.RespondSuccess(c, gin.H{"data": consumers, "total": len(consumers)})
}

// Get godoc
// @Summary 获取 API 消费方 | Get API consumer
// @Tags API 消费方 | API Consumers
// @Produce json
// @Security BearerAuth
// @Param id path int true "API 消费方 ID | API consumer ID"
// @Success 200 {object} models.APIConsumer
// @Failure 404 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.api_consumer.read"]
// @Router /tenant/api-consumers/{id} [get]
func (h *APIConsumerHandler) Get(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}
	_, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	consumer, err := h.service.Get(id, tenantID)
	if err != nil {
		commonapi.RespondError(c, http.StatusNotFound, "API consumer not found")
		return
	}
	commonapi.RespondSuccess(c, consumer)
}

// Update godoc
// @Summary 更新 API 消费方 | Update API consumer
// @Tags API 消费方 | API Consumers
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "API 消费方 ID | API consumer ID"
// @Param request body models.UpdateAPIConsumerRequest true "更新内容 | Update"
// @Success 200 {object} models.APIConsumer
// @Failure 400 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.api_consumer.update"]
// @Router /tenant/api-consumers/{id} [put]
func (h *APIConsumerHandler) Update(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}
	var req models.UpdateAPIConsumerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		commonapi.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}
	_, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	consumer, err := h.service.Update(id, tenantID, &req)
	commonapi.RespondOrError(c, consumer, err)
}

// Delete godoc
// @Summary 删除 API 消费方 | Delete API consumer
// @Tags API 消费方 | API Consumers
// @Produce json
// @Security BearerAuth
// @Param id path int true "API 消费方 ID | API consumer ID"
// @Success 204
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.api_consumer.delete"]
// @Router /tenant/api-consumers/{id} [delete]
func (h *APIConsumerHandler) Delete(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}
	_, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	if err := h.service.Delete(id, tenantID); err != nil {
		commonapi.RespondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}

// CreateCredential godoc
// @Summary 创建 API 消费凭据 | Create API consumer credential
// @Tags API 消费方 | API Consumers
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "API 消费方 ID | API consumer ID"
// @Param request body models.CreateAPIConsumerCredentialRequest true "凭据配置 | Credential"
// @Success 201 {object} models.APIConsumerCredential
// @Failure 400 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.api_consumer_credential.create"]
// @Router /tenant/api-consumers/{id}/credentials [post]
func (h *APIConsumerHandler) CreateCredential(c *gin.Context) {
	consumerID, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}
	var req models.CreateAPIConsumerCredentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		commonapi.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}
	principalID, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	credential, err := h.service.CreateCredential(consumerID, tenantID, principalID, &req)
	if err != nil {
		commonapi.RespondError(c, commonapi.MapErrorToHTTPStatus(err), err.Error())
		return
	}
	commonapi.RespondCreated(c, credential)
}

// ListCredentials godoc
// @Summary 获取 API 消费凭据列表 | List API consumer credentials
// @Tags API 消费方 | API Consumers
// @Produce json
// @Security BearerAuth
// @Param id path int true "API 消费方 ID | API consumer ID"
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.api_consumer_credential.read"]
// @Router /tenant/api-consumers/{id}/credentials [get]
func (h *APIConsumerHandler) ListCredentials(c *gin.Context) {
	consumerID, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}
	_, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	credentials, err := h.service.ListCredentials(consumerID, tenantID)
	if err != nil {
		commonapi.RespondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	commonapi.RespondSuccess(c, gin.H{"data": credentials, "total": len(credentials)})
}

// RevokeCredential godoc
// @Summary 撤销 API 消费凭据 | Revoke API consumer credential
// @Tags API 消费方 | API Consumers
// @Produce json
// @Security BearerAuth
// @Param id path int true "API 消费方 ID | API consumer ID"
// @Param credential_id path int true "凭据 ID | Credential ID"
// @Success 204
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.api_consumer_credential.revoke"]
// @Router /tenant/api-consumers/{id}/credentials/{credential_id} [delete]
func (h *APIConsumerHandler) RevokeCredential(c *gin.Context) {
	consumerID, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}
	credentialID, err := commonapi.BindIDParam(c, "credential_id")
	if err != nil {
		return
	}
	principalID, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	if err := h.service.RevokeCredential(consumerID, credentialID, tenantID, principalID); err != nil {
		commonapi.RespondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}
