package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	"github.com/addp/common/jsonmap"
	sharedauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	modulei18n "github.com/addp/system/i18n"
	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type metaCollectionService interface {
	CheckMetaCollection(context.Context, engineaccess.Actor, execution.MetaCollectionAccessRequest) (*execution.MetaCollectionAccessObservation, error)
}
type MetaCollectionHandler struct{ service metaCollectionService }

func RegisterMetaCollectionRoutes(api *gin.RouterGroup, runtime *IAMRuntime, h *MetaCollectionHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.UserAccessCredential == nil || runtime.ServiceCredential == nil || h == nil || h.service == nil {
		return errors.New("Meta collection dependencies required")
	}
	serviceTenant, err := middleware.NewIAMServiceContextGuard("tenant")
	if err != nil {
		return err
	}
	api.POST("/runtime/meta_collection_checks", runtime.Authentication, runtime.ServiceCredential, serviceTenant, sharedauth.MustNewServiceClientGuard("addp-meta"), sharedauth.MustNewPermissionGuard(authorization.PermissionSystemMetaCollectionExecute), h.Check)
	return nil
}

// Check godoc
// @Summary 核验当前 Meta basic 采集资格 | Check current Meta basic collection access
// @Description 固定 addp-meta Tenant Service；System 独立核验当前机器身份、Common 执行与 lease、Meta 计划和任务摘要及取消状态。200 仅为当次观察，不是可缓存凭据；重试和后续读取重新核验 | Restricted to addp-meta Tenant Service. Independently checks current machine identity, canonical Common execution and lease, Meta plan/task digest and cancellation state. HTTP 200 is a current observation, never a cacheable credential; retries and subsequent reads recheck
// @Tags 元数据集中采集 | Centralized Metadata Collection
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body execution.MetaCollectionAccessRequest true "执行和租约 | Execution and lease"
// @Success 200 {object} execution.MetaCollectionAccessObservation
// @Failure 400,401,403,404,500,503 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.meta_collection.execute"]
// @Router /runtime/meta_collection_checks [post]
func (h *MetaCollectionHandler) Check(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var request execution.MetaCollectionAccessRequest
	if bindMetaCollection(c, &request) != nil || request.Validate() != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	ac, ok := sharedauth.AuthContextFromGin(c)
	if !ok || ac.Context.TenantID == nil || ac.Context.TenantMembershipID == nil {
		respondIAMError(c, commonapi.ErrForbidden)
		return
	}
	var actor engineaccess.Actor
	for i, value := range []string{*ac.Context.TenantID, ac.Principal.ID, *ac.Context.TenantMembershipID, ac.Authorization.AuthorizationVersion} {
		parsed, err := parseIAMDecimalID(value)
		if err != nil {
			respondIAMError(c, commonapi.ErrForbidden)
			return
		}
		*[]*int64{&actor.TenantID, &actor.PrincipalID, &actor.MembershipID, &actor.AuthorizationVersion}[i] = parsed
	}
	actor.TokenExpiresAt = ac.Token.ExpiresAt
	row, err := h.service.CheckMetaCollection(c.Request.Context(), actor, request)
	if err != nil {
		respondMetaCollectionError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

func bindMetaCollection(c *gin.Context, dst any) error {
	c.Header("Cache-Control", "no-store")
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		return commonapi.ErrBadRequest
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
	if err != nil {
		return commonapi.ErrBadRequest
	}
	if _, err := jsonmap.DecodeObjectStrict(raw, 8192, 8); err != nil {
		return commonapi.ErrBadRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}
func respondMetaCollectionError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, engineaccess.ErrMetaCollectionFactsUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": commoni18n.T(c, modulei18n.MsgMetaCollectionUnavailable), "error_code": "meta_collection_facts_unavailable"})
	default:
		respondIAMError(c, err)
	}
}
