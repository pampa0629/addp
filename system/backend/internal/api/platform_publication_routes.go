package api

import (
	"errors"
	"net/http"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/system/internal/middleware"
	"github.com/addp/system/internal/models"
	"github.com/gin-gonic/gin"
)

func RegisterPlatformPublicationCheckRoute(api *gin.RouterGroup, runtime *IAMRuntime) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.ServiceCredential == nil {
		return errors.New("platform publication check route dependencies are required")
	}
	platform, err := middleware.NewIAMServiceContextGuard("platform")
	if err != nil {
		return err
	}
	publish, err := middleware.NewIAMPermissionGuard(authorization.PlatformDefinitionPublishPermission)
	if err != nil {
		return err
	}
	client, err := middleware.NewIAMClientGuard(authorization.PlatformDefinitionPublisherClient)
	if err != nil {
		return err
	}
	api.POST("/runtime/platform-definition-publication-checks", runtime.Authentication, runtime.ServiceCredential, platform, publish, client, checkPlatformPublication)
	return nil
}

// checkPlatformPublication godoc
// @Summary 核验平台定义发布身份 | Check platform definition publisher
// @Description 固定 Ontology Platform Service 身份核验当前发布权限；仅回显能力修订摘要及机器主体，不签发凭据，不表示发布或激活成功 | Check current publication permission for the fixed Ontology Platform Service; echo only the capability/revision/digest binding and machine identity, without issuing a credential or claiming publication or activation
// @Tags Runtime 发布授权 | Runtime Publication Authorization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body authorization.PlatformPublicationCheck true "能力修订摘要 | Capability revision and digest"
// @Success 200 {object} authorization.PlatformPublicationObservation
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["ontology.platform_definition.publish"]
// @Router /runtime/platform-definition-publication-checks [post]
func checkPlatformPublication(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, authorization.PlatformPublicationCheckByteLimit)
	var binding authorization.PlatformPublicationCheck
	if c.Request.URL.RawQuery != "" || commonapi.BindOptionalJSONStrict(c, &binding) != nil || binding.Validate() != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: commoni18n.T(c, "system.platform_publication.invalid"), ErrorCode: "platform_publication_invalid"})
		return
	}
	actor, ok := middleware.IAMAuthContextFromGin(c)
	if !ok || actor.Client.ClientID == nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: commoni18n.T(c, commoni18n.MsgUnauthorized), ErrorCode: "authentication_required"})
		return
	}
	result := authorization.PlatformPublicationObservation{PlatformPublicationCheck: binding, ContextType: actor.Context.Type, ClientID: *actor.Client.ClientID, PrincipalID: actor.Principal.ID, PrincipalType: actor.Principal.Type, AuthorizationVersion: actor.Authorization.AuthorizationVersion}
	if result.Validate(binding) != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: commoni18n.T(c, "system.auth.internal_error"), ErrorCode: "platform_publication_check_failed"})
		return
	}
	c.JSON(http.StatusOK, result)
}
