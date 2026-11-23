package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/common/dbbridge"
	engineplugin "github.com/addp/common/engine/plugin"
	engineselection "github.com/addp/common/engine/selection"
	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/system/i18n"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/middleware"
	"github.com/addp/system/internal/models"
	systemservice "github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type IAMIssueNotebookSessionAuthorizationRequest struct {
	SessionID string `json:"session_id"`
	TaskID    uint   `json:"task_id"`
	ExpiresIn int64  `json:"expires_in"`
}

type IAMNotebookSessionAuthorizationResponse struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	TaskID    uint      `json:"task_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type IAMNotebookEngineCatalogChildrenRequest struct {
	SessionID string                          `json:"session_id"`
	EngineID  uint                            `json:"engine_id"`
	Path      models.EngineCatalogPath        `json:"path"`
	Options   models.EngineCatalogListOptions `json:"options,omitempty"`
}

type IAMRevokeNotebookSessionAuthorizationRequest struct {
	SessionID string `json:"session_id"`
}

type IAMNotebookExecutionEngineAccessRequest struct {
	SessionID   string `json:"session_id"`
	ExecutionID string `json:"execution_id"`
	EngineID    uint   `json:"engine_id"`
	ExpiresIn   int64  `json:"expires_in"`
}

type iamNotebookSessionAuthorizationService interface {
	ReviewNavigation(context.Context, iam.AuthorizeNotebookEngineCatalogInput, bool) (*iam.AuthorizedNotebookEngineCatalog, error)
	Issue(context.Context, iam.IssueNotebookSessionAuthorizationInput) (*iam.IssuedNotebookSessionAuthorization, error)
	Authorize(context.Context, iam.AuthorizeNotebookEngineCatalogInput) (*iam.AuthorizedNotebookEngineCatalog, error)
	DeriveExecutionEngineAccess(context.Context, iam.DeriveNotebookExecutionEngineAccessInput) (*iam.AuthorizedExecutionEngineAccess, error)
	Revoke(context.Context, iam.RevokeNotebookSessionAuthorizationInput) error
}

type notebookEngineCatalogEngineResolver interface {
	GetForExecution(id, tenantID uint) (*models.Engine, error)
	ListRuntimeDescriptors(page, pageSize int, filter systemservice.EngineListFilter, tenantID uint) ([]models.EngineRuntimeDescriptor, int64, error)
}

type notebookEngineCatalogChildrenLister interface {
	ListEngineCatalogChildren(context.Context, *models.Engine, models.EngineCatalogListChildrenRequest) ([]models.EngineCatalogEntry, error)
}

type notebookNavigationObserver interface {
	CheckNotebookTableVisibility(context.Context, *iam.AuthorizedNotebookEngineCatalog, []engineplugin.EngineCatalogPath) ([]bool, error)
	CheckNotebookNavigation(context.Context, *iam.AuthorizedNotebookEngineCatalog, shared.SourceNavigationCheckRequest) (*shared.SourceMetadataCheck, error)
}

type IAMNotebookSessionAuthorizationHandler struct {
	navigation notebookNavigationObserver
	service    iamNotebookSessionAuthorizationService
	engines    notebookEngineCatalogEngineResolver
	catalog    notebookEngineCatalogChildrenLister
}

func NewIAMNotebookSessionAuthorizationHandler(
	authorizationService iamNotebookSessionAuthorizationService,
	engines notebookEngineCatalogEngineResolver,
	catalog notebookEngineCatalogChildrenLister,
	navigation notebookNavigationObserver,
) (*IAMNotebookSessionAuthorizationHandler, error) {
	if authorizationService == nil || engines == nil || catalog == nil || navigation == nil {
		return nil, fmt.Errorf("%w: notebook session authorization handler dependencies are required", commonapi.ErrBadRequest)
	}
	return &IAMNotebookSessionAuthorizationHandler{
		service: authorizationService, engines: engines, catalog: catalog, navigation: navigation,
	}, nil
}

// Issue godoc
// @Summary      签发 Notebook 会话授权 | Issue Notebook session authorization
// @Description  从当前 Tenant User Access Token 派生绑定唯一 Notebook Session 和 Task 的短期会话授权事实 | Derive short-lived session authorization facts bound to one Notebook Session and Task from the current tenant user access token
// @Tags         认证 | Authentication
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body IAMIssueNotebookSessionAuthorizationRequest true "Notebook 会话边界 | Notebook session boundary"
// @Success      201 {object} IAMNotebookSessionAuthorizationResponse
// @Failure      400 {object} IAMErrorResponse
// @Failure      401 {object} IAMErrorResponse
// @Failure      403 {object} IAMErrorResponse
// @Failure      409 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.read","develop.notebook.update"]
// @Router       /auth/notebook-session-authorizations [post]
func (h *IAMNotebookSessionAuthorizationHandler) Issue(c *gin.Context) {
	var request IAMIssueNotebookSessionAuthorizationRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondNotebookEngineCatalogError(c, fmt.Errorf("%w: invalid notebook session authorization request", commonapi.ErrBadRequest))
		return
	}
	sessionID, err := parseCanonicalNotebookEngineCatalogUUID(request.SessionID)
	if err != nil || request.TaskID == 0 || request.ExpiresIn <= 0 || request.ExpiresIn > 3600 {
		respondNotebookEngineCatalogError(c, fmt.Errorf("%w: invalid notebook session authorization request", commonapi.ErrBadRequest))
		return
	}
	sourceAccessToken := iamBearerToken(c.GetHeader("Authorization"))
	if sourceAccessToken == "" {
		respondNotebookEngineCatalogError(c, commonapi.ErrUnauthorized)
		return
	}
	issued, err := h.service.Issue(c.Request.Context(), iam.IssueNotebookSessionAuthorizationInput{
		SourceAccessToken: sourceAccessToken, SessionID: sessionID, TaskID: int64(request.TaskID),
		ExpiresIn: time.Duration(request.ExpiresIn) * time.Second,
		Audit:     iamAuditMetadataWithStatus(c, http.StatusCreated),
	})
	if err != nil {
		respondNotebookEngineCatalogError(c, err)
		return
	}
	c.JSON(http.StatusCreated, IAMNotebookSessionAuthorizationResponse{
		ID: issued.ID.String(), SessionID: issued.SessionID.String(),
		TaskID: uint(issued.TaskID), ExpiresAt: issued.ExpiresAt.UTC(),
	})
}

// ListEngineCatalogChildren godoc
// @Summary      使用 Notebook 会话授权列出实时 Catalog 子节点 | List live Catalog children with Notebook session authorization
// @Description  仅 addp-develop Tenant Service Principal 可消费绑定 Session 的用户派生授权；授权复核与 EngineCatalogProvider.ListChildren 在同一请求完成 | Only the addp-develop tenant service principal may consume the session-bound user-derived authorization; authorization review and EngineCatalogProvider.ListChildren complete in one request
// @Description PostgreSQL 按原用户目录可见性分页，其他引擎沿管理读取权限浏览 | PostgreSQL paginates visible entries for the original user. Other catalogs require management read permission
// @Tags         Notebook 会话授权 | Notebook Session Authorization
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Notebook 会话授权 ID | Notebook session authorization ID"
// @Param        request body IAMNotebookEngineCatalogChildrenRequest true "Session、Engine 与 Catalog 路径 | Session, Engine, and Catalog path"
// @Success      200 {object} models.EngineCatalogListChildrenResponse
// @Failure      400 {object} IAMErrorResponse
// @Failure      403 {object} IAMErrorResponse
// @Failure      404 {object} IAMErrorResponse
// @Failure      422 {object} IAMErrorResponse
// @Failure      502 {object} IAMErrorResponse
// @Failure      503 {object} IAMErrorResponse
// @Failure      504 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.notebook_session_authorization.execute"]
// @Router       /notebook-session-authorizations/{id}/catalog/children [post]
func (h *IAMNotebookSessionAuthorizationHandler) ListEngineCatalogChildren(c *gin.Context) {
	authorizationID, err := parseCanonicalNotebookEngineCatalogUUID(c.Param("id"))
	if err != nil {
		respondNotebookEngineCatalogError(c, fmt.Errorf("%w: invalid notebook session authorization ID", commonapi.ErrBadRequest))
		return
	}
	var request IAMNotebookEngineCatalogChildrenRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondNotebookEngineCatalogError(c, fmt.Errorf("%w: invalid notebook catalog request", commonapi.ErrBadRequest))
		return
	}
	sessionID, err := parseCanonicalNotebookEngineCatalogUUID(request.SessionID)
	if err != nil || validateNotebookEngineCatalogRequest(request) != nil {
		respondNotebookEngineCatalogError(c, fmt.Errorf("%w: invalid notebook catalog request", commonapi.ErrBadRequest))
		return
	}
	principalID, tenantID, principalType, err := iamTenantActor(c)
	if err != nil || principalType != string(iam.PrincipalTypeServicePrincipal) {
		respondNotebookEngineCatalogError(c, iam.ErrNotebookSessionAuthorizationForbidden)
		return
	}
	authContext, exists := middleware.IAMAuthContextFromGin(c)
	if !exists || authContext.Client.ClientID == nil {
		respondNotebookEngineCatalogError(c, commonapi.ErrUnauthorized)
		return
	}
	input := iam.AuthorizeNotebookEngineCatalogInput{
		AuthorizationID: authorizationID, SessionID: sessionID,
		ServicePrincipalID: int64(principalID), ServiceClientID: *authContext.Client.ClientID,
		TenantID: int64(tenantID), Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	}
	authorized, err := h.service.ReviewNavigation(c.Request.Context(), input, false)
	if err != nil {
		respondNotebookEngineCatalogError(c, err)
		return
	}
	engine, err := h.engines.GetForExecution(request.EngineID, uint(authorized.TenantID))
	if err != nil {
		respondNotebookEngineCatalogError(c, err)
		return
	}
	if engine.EngineType != "postgresql" && !authorized.CanBrowseOtherEngines {
		respondNotebookEngineCatalogError(c, iam.ErrNotebookSessionAuthorizationForbidden)
		return
	}
	if err := requireLiveEngineCatalogCapability(engine); err != nil {
		respondNotebookEngineCatalogError(c, err)
		return
	}
	if engine.EngineType == "postgresql" {
		nodes, err := h.postgresCatalogChildren(c.Request.Context(), authorized, engine, request)
		if err != nil {
			respondNotebookEngineCatalogError(c, err)
			return
		}
		if _, err = h.service.ReviewNavigation(c.Request.Context(), input, true); err != nil {
			respondNotebookEngineCatalogError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, models.EngineCatalogListChildrenResponse{Nodes: nodes})
		return
	}
	nodes, err := h.catalog.ListEngineCatalogChildren(c.Request.Context(), engine, models.EngineCatalogListChildrenRequest{
		Path: request.Path, Options: request.Options,
	})
	if err != nil {
		respondEngineCatalogProviderError(c, err)
		return
	}
	if _, err = h.service.Authorize(c.Request.Context(), input); err != nil {
		respondNotebookEngineCatalogError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, models.EngineCatalogListChildrenResponse{Nodes: nodes})
}

func (h *IAMNotebookSessionAuthorizationHandler) postgresCatalogChildren(ctx context.Context, session *iam.AuthorizedNotebookEngineCatalog, engine *models.Engine, req IAMNotebookEngineCatalogChildrenRequest) ([]models.EngineCatalogEntry, error) {
	parent := systemservice.ToPluginEngineCatalogPath(req.Path)
	parent.EngineID = engine.ID
	if len(parent.Segments) == 0 {
		parent = engineplugin.EngineCatalogRootPath(engineplugin.TabularCatalogModel("schema"), engine.ID)
	}
	query := shared.SourceNavigationCheckRequest{Targets: []engineplugin.EngineCatalogPath{parent}}
	permit, err := h.navigation.CheckNotebookNavigation(ctx, session, query)
	if err != nil {
		return nil, err
	}
	if !permit.MatchesNavigation(query) || !permit.Targets[0].Visible {
		return nil, iam.ErrNotebookSessionAuthorizationForbidden
	}
	// PostgreSQL enumerates this level in one call. Offsets apply to the visible sequence.
	nodes, err := h.catalog.ListEngineCatalogChildren(ctx, engine, models.EngineCatalogListChildrenRequest{Path: req.Path})
	if err != nil {
		return nil, err
	}
	visible := make([]models.EngineCatalogEntry, 0)
	skip := req.Options.Offset
	for start := 0; start < len(nodes); start += 200 {
		batch := nodes[start:min(start+200, len(nodes))]
		paths := make([]engineplugin.EngineCatalogPath, len(batch))
		for i, n := range batch {
			paths[i] = systemservice.ToPluginEngineCatalogPath(n.Path)
		}
		allowed := make([]bool, len(batch))
		if len(parent.Segments) == 2 {
			allowed, err = h.navigation.CheckNotebookTableVisibility(ctx, session, paths)
			if err != nil {
				return nil, err
			}
		} else {
			query := shared.SourceNavigationCheckRequest{Targets: paths}
			observation, err := h.navigation.CheckNotebookNavigation(ctx, session, query)
			if err != nil {
				return nil, err
			}
			if !observation.MatchesNavigation(query) {
				return nil, errors.New("invalid Notebook navigation observation")
			}
			for i, t := range observation.Targets {
				allowed[i] = t.Visible
			}
		}
		for i, n := range batch {
			if !allowed[i] {
				continue
			}
			if skip > 0 {
				skip--
				continue
			}
			// Row estimates and branch totals describe content beyond navigation.
			n.Table = nil
			n.LeafCount = nil
			visible = append(visible, n)
			if len(visible) == req.Options.Limit {
				return visible, nil
			}
		}
	}
	return visible, nil
}

// ListEngineDescriptors godoc
// @Summary      使用 Notebook 会话授权列出可访问数据引擎 | List accessible data engines with Notebook session authorization
// @Description  按会话原用户有效来源授权列出 PostgreSQL，其他在线实时目录沿引擎管理读取资格列出 | List PostgreSQL by current session-user source grants and other live catalogs by engine management read permission
// @Tags         Notebook 会话授权 | Notebook Session Authorization
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Notebook 会话授权 ID | Notebook session authorization ID"
// @Param        session_id query string true "Notebook 会话 ID | Notebook session ID"
// @Success      200 {array} models.EngineRuntimeDescriptor
// @Failure      400 {object} IAMErrorResponse
// @Failure      403 {object} IAMErrorResponse
// @Failure      502 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.notebook_session_authorization.execute"]
// @Router       /notebook-session-authorizations/{id}/engine-descriptors [get]
func (h *IAMNotebookSessionAuthorizationHandler) ListEngineDescriptors(c *gin.Context) {
	authorizationID, err := parseCanonicalNotebookEngineCatalogUUID(c.Param("id"))
	if err != nil {
		respondNotebookEngineCatalogError(c, fmt.Errorf("%w: invalid notebook session authorization ID", commonapi.ErrBadRequest))
		return
	}
	sessionID, err := parseCanonicalNotebookEngineCatalogUUID(c.Query("session_id"))
	if err != nil {
		respondNotebookEngineCatalogError(c, fmt.Errorf("%w: invalid notebook session ID", commonapi.ErrBadRequest))
		return
	}
	principalID, tenantID, principalType, err := iamTenantActor(c)
	if err != nil || principalType != string(iam.PrincipalTypeServicePrincipal) {
		respondNotebookEngineCatalogError(c, iam.ErrNotebookSessionAuthorizationForbidden)
		return
	}
	authContext, exists := middleware.IAMAuthContextFromGin(c)
	if !exists || authContext.Client.ClientID == nil {
		respondNotebookEngineCatalogError(c, commonapi.ErrUnauthorized)
		return
	}
	input := iam.AuthorizeNotebookEngineCatalogInput{
		AuthorizationID: authorizationID, SessionID: sessionID,
		ServicePrincipalID: int64(principalID), ServiceClientID: *authContext.Client.ClientID,
		TenantID: int64(tenantID), Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	}
	session, err := h.service.ReviewNavigation(c.Request.Context(), input, false)
	if err != nil {
		respondNotebookEngineCatalogError(c, err)
		return
	}

	const pageSize = 100
	result := make([]models.EngineRuntimeDescriptor, 0, pageSize)
	for page := 1; ; page++ {
		descriptors, total, err := h.engines.ListRuntimeDescriptors(page, pageSize, systemservice.EngineListFilter{
			IncludeBuiltin: true, LifecycleStates: []string{models.EngineLifecycleActive},
		}, tenantID)
		if err != nil {
			respondNotebookEngineCatalogError(c, err)
			return
		}
		visible, err := h.visibleNotebookEngines(c.Request.Context(), session, descriptors)
		if err != nil {
			respondNotebookEngineCatalogError(c, err)
			return
		}
		result = append(result, visible...)
		if int64(page*pageSize) >= total {
			break
		}
	}
	current, err := h.service.ReviewNavigation(c.Request.Context(), input, true)
	if err != nil {
		respondNotebookEngineCatalogError(c, err)
		return
	}
	if !current.CanBrowseOtherEngines {
		result = slices.DeleteFunc(result, func(d models.EngineRuntimeDescriptor) bool { return d.EngineType != "postgresql" })
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
}

func (h *IAMNotebookSessionAuthorizationHandler) visibleNotebookEngines(ctx context.Context, session *iam.AuthorizedNotebookEngineCatalog, descriptors []models.EngineRuntimeDescriptor) ([]models.EngineRuntimeDescriptor, error) {
	var candidates []models.EngineRuntimeDescriptor
	var visible []models.EngineRuntimeDescriptor
	var query shared.SourceNavigationCheckRequest
	for i := range descriptors {
		descriptor := &descriptors[i]
		if !notebookDataEngineDescriptor(descriptor) {
			continue
		}
		if descriptor.EngineType != "postgresql" {
			if session.CanBrowseOtherEngines {
				visible = append(visible, *descriptor)
			}
			continue
		}
		model, err := dbbridge.EngineCatalogModel(descriptor.EngineType)
		if err != nil {
			continue
		}
		root := engineplugin.EngineCatalogRootPath(model, descriptor.ID)
		candidates = append(candidates, *descriptor)
		query.Targets = append(query.Targets, root)
	}
	if len(candidates) == 0 {
		return visible, nil
	}
	observation, err := h.navigation.CheckNotebookNavigation(ctx, session, query)
	if err != nil {
		return nil, err
	}
	if !observation.MatchesNavigation(query) {
		return nil, errors.New("invalid Notebook navigation observation")
	}

	for i, target := range observation.Targets {
		if target.Visible {
			visible = append(visible, candidates[i])
		}
	}
	return visible, nil
}

func notebookDataEngineDescriptor(descriptor *models.EngineRuntimeDescriptor) bool {
	if descriptor == nil || !engineselection.IsAvailableStorageEngine(descriptor.AsEngine()) || descriptor.Capabilities == nil {
		return false
	}
	return hasLiveEngineCatalogCapability(descriptor.AsEngine())
}

// DeriveExecutionEngineAccess godoc
// @Summary      派生 Notebook 单次只读执行访问 | Derive one Notebook read execution access
// @Description  为新的 execution 原子创建只读 Execution Authorization 并返回仅供 addp-develop 受控 Runtime 使用的引擎访问 | Atomically create a read-only Execution Authorization for a new execution and return engine access only to the controlled addp-develop runtime
// @Tags         Notebook 会话授权 | Notebook Session Authorization
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Notebook 会话授权 ID | Notebook session authorization ID"
// @Param        request body IAMNotebookExecutionEngineAccessRequest true "Session、execution 与 Engine 边界 | Session, execution, and Engine boundary"
// @Success      201 {object} IAMExecutionEngineAccessResponse
// @Failure      400 {object} IAMErrorResponse
// @Failure      401 {object} IAMErrorResponse
// @Failure      403 {object} IAMErrorResponse
// @Failure      409 {object} IAMErrorResponse
// @Failure      503 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.notebook_session_authorization.execute"]
// @Router       /notebook-session-authorizations/{id}/execution-engine-accesses [post]
func (h *IAMNotebookSessionAuthorizationHandler) DeriveExecutionEngineAccess(c *gin.Context) {
	authorizationID, err := parseCanonicalNotebookEngineCatalogUUID(c.Param("id"))
	if err != nil {
		respondNotebookEngineCatalogError(c, fmt.Errorf("%w: invalid notebook session authorization ID", commonapi.ErrBadRequest))
		return
	}
	var request IAMNotebookExecutionEngineAccessRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil || request.EngineID == 0 ||
		request.ExpiresIn <= 0 {
		respondNotebookEngineCatalogError(c, fmt.Errorf("%w: invalid notebook execution engine access request", commonapi.ErrBadRequest))
		return
	}
	sessionID, err := parseCanonicalNotebookEngineCatalogUUID(request.SessionID)
	if err != nil {
		respondNotebookEngineCatalogError(c, fmt.Errorf("%w: invalid notebook execution engine access request", commonapi.ErrBadRequest))
		return
	}
	executionID, err := parseCanonicalExecutionUUID(request.ExecutionID)
	if err != nil {
		respondNotebookEngineCatalogError(c, err)
		return
	}
	principalID, tenantID, principalType, err := iamTenantActor(c)
	if err != nil || principalType != string(iam.PrincipalTypeServicePrincipal) {
		respondNotebookEngineCatalogError(c, iam.ErrNotebookSessionAuthorizationForbidden)
		return
	}
	authContext, exists := middleware.IAMAuthContextFromGin(c)
	if !exists || authContext.Client.ClientID == nil {
		respondNotebookEngineCatalogError(c, commonapi.ErrUnauthorized)
		return
	}
	access, err := h.service.DeriveExecutionEngineAccess(c.Request.Context(), iam.DeriveNotebookExecutionEngineAccessInput{
		AuthorizationID: authorizationID, SessionID: sessionID, ExecutionID: executionID,
		EngineID: int64(request.EngineID), ExpiresIn: time.Duration(request.ExpiresIn) * time.Second,
		ServicePrincipalID: int64(principalID), ServiceClientID: *authContext.Client.ClientID,
		TenantID: int64(tenantID), Audit: iamAuditMetadataWithStatus(c, http.StatusCreated),
	})
	if err != nil {
		respondNotebookEngineCatalogError(c, err)
		return
	}
	engine, err := h.engines.GetForExecution(uint(access.EngineID), uint(access.TenantID))
	if err != nil {
		respondNotebookEngineCatalogError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, IAMExecutionEngineAccessResponse{
		AuthorizationID: strconv.FormatInt(access.AuthorizationID, 10),
		ExecutionID:     access.ExecutionID.String(), Audience: access.Audience,
		EngineID: strconv.FormatInt(access.EngineID, 10), Effects: append([]string(nil), access.Effects...),
		ExpiresAt: access.ExpiresAt.UTC(), Engine: engine,
	})
}

// Revoke godoc
// @Summary      撤销 Notebook 会话授权 | Revoke Notebook session authorization
// @Description  addp-develop 关闭 Session 时幂等撤销其会话授权 | Idempotently revoke the session authorization when addp-develop closes the Session
// @Tags         Notebook 会话授权 | Notebook Session Authorization
// @Accept       json
// @Security     BearerAuth
// @Param        id path string true "Notebook 会话授权 ID | Notebook session authorization ID"
// @Param        request body IAMRevokeNotebookSessionAuthorizationRequest true "Notebook Session | Notebook Session"
// @Success      204
// @Failure      400 {object} IAMErrorResponse
// @Failure      403 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.notebook_session_authorization.execute"]
// @Router       /notebook-session-authorizations/{id}/revocations [post]
func (h *IAMNotebookSessionAuthorizationHandler) Revoke(c *gin.Context) {
	authorizationID, err := parseCanonicalNotebookEngineCatalogUUID(c.Param("id"))
	if err != nil {
		respondNotebookEngineCatalogError(c, fmt.Errorf("%w: invalid notebook session authorization ID", commonapi.ErrBadRequest))
		return
	}
	var request IAMRevokeNotebookSessionAuthorizationRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondNotebookEngineCatalogError(c, fmt.Errorf("%w: invalid notebook catalog revocation", commonapi.ErrBadRequest))
		return
	}
	sessionID, err := parseCanonicalNotebookEngineCatalogUUID(request.SessionID)
	if err != nil {
		respondNotebookEngineCatalogError(c, fmt.Errorf("%w: invalid notebook catalog revocation", commonapi.ErrBadRequest))
		return
	}
	principalID, tenantID, principalType, err := iamTenantActor(c)
	if err != nil || principalType != string(iam.PrincipalTypeServicePrincipal) {
		respondNotebookEngineCatalogError(c, iam.ErrNotebookSessionAuthorizationForbidden)
		return
	}
	authContext, exists := middleware.IAMAuthContextFromGin(c)
	if !exists || authContext.Client.ClientID == nil {
		respondNotebookEngineCatalogError(c, commonapi.ErrUnauthorized)
		return
	}
	if err := h.service.Revoke(c.Request.Context(), iam.RevokeNotebookSessionAuthorizationInput{
		AuthorizationID: authorizationID, SessionID: sessionID,
		ServicePrincipalID: int64(principalID), ServiceClientID: *authContext.Client.ClientID,
		TenantID: int64(tenantID), Audit: iamAuditMetadataWithStatus(c, http.StatusNoContent),
	}); err != nil {
		respondNotebookEngineCatalogError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func validateNotebookEngineCatalogRequest(request IAMNotebookEngineCatalogChildrenRequest) error {
	if request.EngineID == 0 || request.Options.Limit <= 0 || request.Options.Limit > 1000 ||
		request.Options.Offset < 0 || request.Options.Recursive {
		return commonapi.ErrBadRequest
	}
	if request.Path.EngineID != 0 && request.Path.EngineID != request.EngineID {
		return commonapi.ErrBadRequest
	}
	if len(request.Path.Segments) > 0 && request.Path.Version != engineplugin.EngineCatalogPathVersion {
		return commonapi.ErrBadRequest
	}
	return nil
}

func parseCanonicalNotebookEngineCatalogUUID(value string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(value)
	if err != nil || parsed == uuid.Nil || parsed.String() != value {
		return uuid.Nil, fmt.Errorf("%w: invalid UUID", commonapi.ErrBadRequest)
	}
	return parsed, nil
}

func respondNotebookEngineCatalogError(c *gin.Context, err error) {
	respondEngineCatalogErrorWithDefault(
		c, err, sysi18n.MsgEngineCatalogControlPlaneFailed, "engine_catalog_control_plane_failed",
	)
}

func respondEngineCatalogProviderError(c *gin.Context, err error) {
	respondEngineCatalogErrorWithDefault(
		c, err, sysi18n.MsgEngineCatalogProviderFailed, "engine_catalog_provider_failed",
	)
}

func respondEngineCatalogErrorWithDefault(c *gin.Context, err error, defaultMessageID, defaultErrorCode string) {
	status := http.StatusBadGateway
	messageID := defaultMessageID
	errorCode := defaultErrorCode
	switch {
	case errors.Is(err, iam.ErrNotebookSessionAuthorizationConflict):
		status = http.StatusConflict
		messageID = sysi18n.MsgNotebookSessionAuthorizationConflict
		errorCode = "notebook_session_authorization_conflict"
	case errors.Is(err, iam.ErrNotebookSessionAuthorizationForbidden):
		status = http.StatusForbidden
		messageID = sysi18n.MsgNotebookSessionAuthorizationForbidden
		errorCode = "notebook_session_authorization_forbidden"
	case errors.Is(err, iam.ErrExecutionAuthorizationConflict):
		status = http.StatusConflict
		messageID = sysi18n.MsgNotebookSessionAuthorizationConflict
		errorCode = "execution_authorization_conflict"
	case errors.Is(err, iam.ErrExecutionAuthorizationPermissionDenied):
		status = http.StatusForbidden
		messageID = commoni18n.MsgForbidden
		errorCode = "execution_access_forbidden"
	case errors.Is(err, iam.ErrExecutionAuthorizationUnavailable):
		status = http.StatusServiceUnavailable
		messageID = sysi18n.MsgEngineCatalogEngineUnavailable
		errorCode = "engine_unavailable"
	case errors.Is(err, commonapi.ErrBadRequest), engineplugin.IsEngineCatalogErrorKind(err, engineplugin.EngineCatalogErrorInvalidPath):
		status = http.StatusBadRequest
		messageID = commoni18n.MsgInvalidParams
		errorCode = "engine_catalog_request_invalid"
	case errors.Is(err, commonapi.ErrUnauthorized):
		status = http.StatusUnauthorized
		messageID = commoni18n.MsgUnauthorized
		errorCode = "authentication_required"
	case errors.Is(err, commonapi.ErrForbidden):
		status = http.StatusForbidden
		messageID = commoni18n.MsgForbidden
		errorCode = "permission_denied"
	case errors.Is(err, systemservice.ErrResourceNotFound), errors.Is(err, systemservice.ErrResourceForbidden):
		status = http.StatusNotFound
		messageID = sysi18n.MsgEngineCatalogEngineNotFound
		errorCode = "engine_not_found"
	case engineplugin.IsEngineCatalogErrorKind(err, engineplugin.EngineCatalogErrorNotFound):
		status = http.StatusNotFound
		messageID = sysi18n.MsgEngineCatalogEntryNotFound
		errorCode = "engine_catalog_entry_not_found"
	case engineplugin.IsEngineCatalogErrorKind(err, engineplugin.EngineCatalogErrorUnsupported):
		status = http.StatusUnprocessableEntity
		messageID = sysi18n.MsgEngineCatalogOperationUnsupported
		errorCode = "engine_catalog_operation_unsupported"
	case engineplugin.IsEngineCatalogErrorKind(err, engineplugin.EngineCatalogErrorUnavailable):
		status = http.StatusServiceUnavailable
		messageID = sysi18n.MsgEngineCatalogEngineUnavailable
		errorCode = "engine_unavailable"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		status = http.StatusGatewayTimeout
		messageID = sysi18n.MsgEngineCatalogTimeout
		errorCode = "engine_catalog_timeout"
	}
	c.JSON(status, IAMErrorResponse{Error: commoni18n.T(c, messageID), ErrorCode: &errorCode})
}
