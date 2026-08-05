package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	commoninference "github.com/addp/common/inference"
	commonauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	inferencei18n "github.com/addp/inference/i18n"
	"github.com/addp/inference/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
)

type Handler struct {
	control *service.ControlPlane
	runtime *service.Runtime
}

func NewHandler(control *service.ControlPlane, runtime *service.Runtime) *Handler {
	return &Handler{control: control, runtime: runtime}
}

// Capabilities godoc
// @Summary 查询推理能力 | Get inference capabilities
// @Tags Inference
// @Produce json
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "public"
// @Router /capabilities [get]
func (h *Handler) Capabilities(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"schema_version": commoninference.SchemaVersion, "operations": []string{"chat", "embedding", "rerank"}, "modalities": []string{"text", "image"}, "streaming": false})
}

// ListProviders godoc
// @Summary 查询 Provider Connection | List provider connections
// @Tags Inference Provider
// @Produce json
// @Param page query int false "页码 | Page number" default(1)
// @Param page_size query int false "每页数量 | Page size" default(20)
// @Success 200 {object} object{data=[]service.ProviderView,total=int64,page=int,page_size=int,total_pages=int}
// @Failure 401 {object} commoninference.ErrorResponse
// @Failure 403 {object} commoninference.ErrorResponse
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["inference.provider.read"]
// @Router /provider-connections [get]
func (h *Handler) ListProviders(c *gin.Context) {
	page, pageSize := pagination(c)
	values, err := h.control.ListProviders(c.Request.Context(), actor(c), page, pageSize)
	respond(c, values, err)
}

// GetProvider godoc
// @Summary 读取 Provider Connection | Get provider connection
// @Tags Inference Provider
// @Produce json
// @Param id path string true "Provider ID"
// @Success 200 {object} service.ProviderView
// @Failure 404 {object} commoninference.ErrorResponse
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["inference.provider.read"]
// @Router /provider-connections/{id} [get]
func (h *Handler) GetProvider(c *gin.Context) {
	value, err := h.control.GetProvider(c.Request.Context(), actor(c), c.Param("id"))
	respond(c, value, err)
}

// CreateProvider godoc
// @Summary 创建 Provider Connection | Create provider connection
// @Tags Inference Provider
// @Accept json
// @Produce json
// @Param request body service.ProviderInput true "Provider"
// @Success 201 {object} service.ProviderView
// @Failure 400 {object} commoninference.ErrorResponse
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["inference.provider.create"]
// @Router /provider-connections [post]
func (h *Handler) CreateProvider(c *gin.Context) {
	var input service.ProviderInput
	if !bind(c, &input) {
		return
	}
	value, err := h.control.CreateProvider(c.Request.Context(), actor(c), input)
	if err != nil {
		respond(c, nil, err)
		return
	}
	c.JSON(http.StatusCreated, value)
}

// UpdateProvider godoc
// @Summary 更新 Provider Connection | Update provider connection
// @Tags Inference Provider
// @Accept json
// @Produce json
// @Param id path string true "Provider ID"
// @Param request body service.ProviderInput true "Provider"
// @Success 200 {object} service.ProviderView
// @Failure 400 {object} commoninference.ErrorResponse
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["inference.provider.update"]
// @Router /provider-connections/{id} [put]
func (h *Handler) UpdateProvider(c *gin.Context) {
	var input service.ProviderInput
	if !bind(c, &input) {
		return
	}
	value, err := h.control.UpdateProvider(c.Request.Context(), actor(c), c.Param("id"), input)
	respond(c, value, err)
}

// DeleteProvider godoc
// @Summary 删除 Provider Connection | Delete provider connection
// @Tags Inference Provider
// @Param id path string true "Provider ID"
// @Success 204
// @Failure 409 {object} commoninference.ErrorResponse
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["inference.provider.delete"]
// @Router /provider-connections/{id} [delete]
func (h *Handler) DeleteProvider(c *gin.Context) {
	if err := h.control.DeleteProvider(c.Request.Context(), actor(c), c.Param("id")); err != nil {
		respond(c, nil, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// SetCredential godoc
// @Summary 设置或轮换 Provider 凭据 | Set or rotate provider credential
// @Tags Inference Provider
// @Accept json
// @Produce json
// @Param id path string true "Provider ID"
// @Param request body object{credential=string} true "Credential"
// @Success 200 {object} service.CredentialStatus
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["inference.provider_credential.update"]
// @Router /provider-connections/{id}/credential [put]
func (h *Handler) SetCredential(c *gin.Context) {
	var input struct {
		Credential string `json:"credential" binding:"required"`
	}
	if !bind(c, &input) {
		return
	}
	value, err := h.control.SetCredential(c.Request.Context(), actor(c), c.Param("id"), input.Credential)
	respond(c, value, err)
}

// DeleteCredential godoc
// @Summary 删除 Provider 凭据 | Delete provider credential
// @Tags Inference Provider
// @Produce json
// @Param id path string true "Provider ID"
// @Success 200 {object} service.CredentialStatus
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["inference.provider_credential.update"]
// @Router /provider-connections/{id}/credential [delete]
func (h *Handler) DeleteCredential(c *gin.Context) {
	value, err := h.control.DeleteCredential(c.Request.Context(), actor(c), c.Param("id"))
	respond(c, value, err)
}

// ListDeployments godoc
// @Summary 查询 Model Deployment | List model deployments
// @Tags Inference Deployment
// @Produce json
// @Param page query int false "页码 | Page number" default(1)
// @Param page_size query int false "每页数量 | Page size" default(20)
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["inference.deployment.read"]
// @Router /model-deployments [get]
func (h *Handler) ListDeployments(c *gin.Context) {
	page, pageSize := pagination(c)
	values, err := h.control.ListDeployments(c.Request.Context(), actor(c), page, pageSize)
	respond(c, values, err)
}

// GetDeployment godoc
// @Summary 读取 Model Deployment | Get model deployment
// @Tags Inference Deployment
// @Produce json
// @Param id path string true "Deployment ID"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["inference.deployment.read"]
// @Router /model-deployments/{id} [get]
func (h *Handler) GetDeployment(c *gin.Context) {
	value, err := h.control.GetDeployment(c.Request.Context(), actor(c), c.Param("id"))
	respond(c, value, err)
}

// CreateDeployment godoc
// @Summary 创建 Model Deployment | Create model deployment
// @Tags Inference Deployment
// @Accept json
// @Produce json
// @Param request body service.DeploymentInput true "Deployment"
// @Success 201 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["inference.deployment.create"]
// @Router /model-deployments [post]
func (h *Handler) CreateDeployment(c *gin.Context) {
	var input service.DeploymentInput
	if !bind(c, &input) {
		return
	}
	value, err := h.control.CreateDeployment(c.Request.Context(), actor(c), input)
	if err != nil {
		respond(c, nil, err)
		return
	}
	c.JSON(http.StatusCreated, value)
}

// UpdateDeployment godoc
// @Summary 更新 Model Deployment | Update model deployment
// @Tags Inference Deployment
// @Accept json
// @Produce json
// @Param id path string true "Deployment ID"
// @Param request body service.DeploymentInput true "Deployment"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["inference.deployment.update"]
// @Router /model-deployments/{id} [put]
func (h *Handler) UpdateDeployment(c *gin.Context) {
	var input service.DeploymentInput
	if !bind(c, &input) {
		return
	}
	value, err := h.control.UpdateDeployment(c.Request.Context(), actor(c), c.Param("id"), input)
	respond(c, value, err)
}

// DeleteDeployment godoc
// @Summary 删除 Model Deployment | Delete model deployment
// @Tags Inference Deployment
// @Param id path string true "Deployment ID"
// @Success 204
// @Failure 409 {object} commoninference.ErrorResponse
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["inference.deployment.delete"]
// @Router /model-deployments/{id} [delete]
func (h *Handler) DeleteDeployment(c *gin.Context) {
	if err := h.control.DeleteDeployment(c.Request.Context(), actor(c), c.Param("id")); err != nil {
		respond(c, nil, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ProbeDeployment godoc
// @Summary 探测 Model Deployment | Probe model deployment
// @Tags Inference Deployment
// @Produce json
// @Param id path string true "Deployment ID"
// @Success 200 {object} service.ProbeResponse
// @Failure 502 {object} commoninference.ErrorResponse
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["inference.deployment.execute"]
// @Router /model-deployments/{id}/probe [post]
func (h *Handler) ProbeDeployment(c *gin.Context) {
	value, err := h.runtime.Probe(c.Request.Context(), actor(c), c.Param("id"))
	respond(c, value, err)
}

// ListProfiles godoc
// @Summary 查询 Model Profile | List model profiles
// @Tags Inference Profile
// @Produce json
// @Param page query int false "页码 | Page number" default(1)
// @Param page_size query int false "每页数量 | Page size" default(20)
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["inference.profile.read"]
// @Router /model-profiles [get]
func (h *Handler) ListProfiles(c *gin.Context) {
	page, pageSize := pagination(c)
	values, err := h.control.ListProfiles(c.Request.Context(), actor(c), page, pageSize)
	respond(c, values, err)
}

// GetProfile godoc
// @Summary 读取 Model Profile | Get model profile
// @Tags Inference Profile
// @Produce json
// @Param id path string true "Profile ID"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["inference.profile.read"]
// @Router /model-profiles/{id} [get]
func (h *Handler) GetProfile(c *gin.Context) {
	value, err := h.control.GetProfile(c.Request.Context(), actor(c), c.Param("id"))
	respond(c, value, err)
}

// CreateProfile godoc
// @Summary 创建 Model Profile | Create model profile
// @Tags Inference Profile
// @Accept json
// @Produce json
// @Param request body service.ProfileInput true "Profile"
// @Success 201 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["inference.profile.create"]
// @Router /model-profiles [post]
func (h *Handler) CreateProfile(c *gin.Context) {
	var input service.ProfileInput
	if !bind(c, &input) {
		return
	}
	value, err := h.control.CreateProfile(c.Request.Context(), actor(c), input)
	if err != nil {
		respond(c, nil, err)
		return
	}
	c.JSON(http.StatusCreated, value)
}

// UpdateProfile godoc
// @Summary 更新 Model Profile | Update model profile
// @Tags Inference Profile
// @Accept json
// @Produce json
// @Param id path string true "Profile ID"
// @Param request body service.ProfileInput true "Profile"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["inference.profile.update"]
// @Router /model-profiles/{id} [put]
func (h *Handler) UpdateProfile(c *gin.Context) {
	var input service.ProfileInput
	if !bind(c, &input) {
		return
	}
	value, err := h.control.UpdateProfile(c.Request.Context(), actor(c), c.Param("id"), input)
	respond(c, value, err)
}

// Chat godoc
// @Summary 执行对话推理 | Execute chat inference
// @Tags Inference Runtime
// @Accept json
// @Produce json
// @Param request body commoninference.ChatRequest true "Chat request"
// @Success 200 {object} commoninference.ChatResponse
// @Failure 400 {object} commoninference.ErrorResponse
// @Failure 502 {object} commoninference.ErrorResponse
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["inference.runtime.execute"]
// @Router /internal/chat [post]
func (h *Handler) Chat(c *gin.Context) {
	var input commoninference.ChatRequest
	if !bind(c, &input) {
		return
	}
	enforceTenant(c, &input.TenantID)
	if c.IsAborted() {
		return
	}
	value, err := h.runtime.Chat(c.Request.Context(), input)
	respond(c, value, err)
}

// ResolveProfile godoc
// @Summary 解析模型 Profile 执行快照 | Resolve model profile execution snapshot
// @Tags Inference Runtime
// @Accept json
// @Produce json
// @Param request body commoninference.ResolveProfileRequest true "Profile resolution request"
// @Success 200 {object} commoninference.ResolveProfileResponse
// @Failure 400 {object} commoninference.ErrorResponse
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["inference.runtime.execute"]
// @Router /internal/profiles/resolve [post]
func (h *Handler) ResolveProfile(c *gin.Context) {
	var input commoninference.ResolveProfileRequest
	if !bind(c, &input) {
		return
	}
	enforceTenant(c, &input.TenantID)
	if c.IsAborted() {
		return
	}
	value, err := h.runtime.ResolveProfile(c.Request.Context(), input)
	respond(c, value, err)
}

// Embed godoc
// @Summary 执行向量推理 | Execute embedding inference
// @Tags Inference Runtime
// @Accept json
// @Produce json
// @Param request body commoninference.EmbeddingRequest true "Embedding request"
// @Success 200 {object} commoninference.EmbeddingResponse
// @Failure 400 {object} commoninference.ErrorResponse
// @Failure 502 {object} commoninference.ErrorResponse
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["inference.runtime.execute"]
// @Router /internal/embeddings [post]
func (h *Handler) Embed(c *gin.Context) {
	var input commoninference.EmbeddingRequest
	if !bind(c, &input) {
		return
	}
	enforceTenant(c, &input.TenantID)
	if c.IsAborted() {
		return
	}
	value, err := h.runtime.Embed(c.Request.Context(), input)
	respond(c, value, err)
}

// Rerank godoc
// @Summary 执行重排推理 | Execute rerank inference
// @Tags Inference Runtime
// @Accept json
// @Produce json
// @Param request body commoninference.RerankRequest true "Rerank request"
// @Success 200 {object} commoninference.RerankResponse
// @Failure 400 {object} commoninference.ErrorResponse
// @Failure 502 {object} commoninference.ErrorResponse
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["inference.runtime.execute"]
// @Router /internal/rerank [post]
func (h *Handler) Rerank(c *gin.Context) {
	var input commoninference.RerankRequest
	if !bind(c, &input) {
		return
	}
	enforceTenant(c, &input.TenantID)
	if c.IsAborted() {
		return
	}
	value, err := h.runtime.Rerank(c.Request.Context(), input)
	respond(c, value, err)
}

func actor(c *gin.Context) service.Actor {
	contextValue, _ := commonauth.AuthContextFromGin(c)
	tenantID, _ := commonauth.TenantIDFromGin(c)
	return service.Actor{ContextType: contextValue.Context.Type, TenantID: uint(tenantID), PrincipalID: commonauth.GetUserID(c)}
}
func enforceTenant(c *gin.Context, requested *uint) {
	tenantID, ok := commonauth.TenantIDFromGin(c)
	if !ok || tenantID == 0 || *requested != uint(tenantID) {
		c.AbortWithStatusJSON(http.StatusForbidden, commoninference.ErrorResponse{ErrorCode: "inference_scope_forbidden", Error: commoni18n.T(c, inferencei18n.MsgScopeForbidden)})
	}
}
func bind(c *gin.Context, target interface{}) bool {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		invalidRequest(c)
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		invalidRequest(c)
		return false
	}
	if err := binding.Validator.ValidateStruct(target); err != nil {
		invalidRequest(c)
		return false
	}
	return true
}
func invalidRequest(c *gin.Context) {
	c.JSON(http.StatusBadRequest, commoninference.ErrorResponse{ErrorCode: "inference_request_invalid", Error: commoni18n.T(c, inferencei18n.MsgRequestInvalid)})
}
func pagination(c *gin.Context) (int, int) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		page = 1
	}
	pageSize, err := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if err != nil || pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}
func respond(c *gin.Context, value interface{}, err error) {
	if err == nil {
		c.JSON(http.StatusOK, value)
		return
	}
	status, code := mapError(err)
	c.JSON(status, commoninference.ErrorResponse{ErrorCode: code, Error: commoni18n.T(c, errorMessageID(code))})
}
func mapError(err error) (int, string) {
	switch {
	case errors.Is(err, service.ErrInvalidRequest):
		return http.StatusBadRequest, "inference_request_invalid"
	case errors.Is(err, service.ErrForbidden):
		return http.StatusForbidden, "inference_scope_forbidden"
	case errors.Is(err, service.ErrNotFound):
		return http.StatusNotFound, "model_profile_not_found"
	case errors.Is(err, service.ErrResourceInUse):
		return http.StatusConflict, "resource_in_use"
	case errors.Is(err, service.ErrProfileUnavailable):
		return http.StatusConflict, "model_profile_unavailable"
	case errors.Is(err, service.ErrUnsupported):
		return http.StatusUnprocessableEntity, "inference_operation_unsupported"
	case errors.Is(err, service.ErrUpstreamUnavailable):
		return http.StatusServiceUnavailable, "inference_upstream_unavailable"
	case errors.Is(err, service.ErrTimeout):
		return http.StatusGatewayTimeout, "inference_timeout"
	default:
		return http.StatusBadGateway, "inference_upstream_failed"
	}
}
func errorMessageID(code string) string {
	return "inference.error." + strings.TrimPrefix(code, "inference_")
}
