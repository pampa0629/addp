package api

import (
	"errors"

	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
	redisv9 "github.com/redis/go-redis/v9"
)

// RegisterIAMRoutes registers the complete target IAM HTTP surface. The legacy
// AuthHandler, OAuthHandler and TokenService routes must not be registered with it.
func RegisterIAMRoutes(
	api *gin.RouterGroup,
	runtime *IAMRuntime,
	redisClient redisv9.Scripter,
) error {
	if api == nil || runtime == nil ||
		runtime.AuthHandler == nil || runtime.OAuthHandler == nil ||
		runtime.DelegationHandler == nil || runtime.ExecutionAuthorizationHandler == nil ||
		runtime.NotebookSessionAuthorizationHandler == nil ||
		runtime.TaskAuthorizationSubjectHandler == nil ||
		runtime.UserSelfHandler == nil || runtime.MFASessionHandler == nil ||
		runtime.TenantInvitationHandler == nil ||
		runtime.Authentication == nil || runtime.FirstPartyCredential == nil ||
		runtime.UserAccessCredential == nil || runtime.ServiceCredential == nil || runtime.OAuthFailureAudit == nil {
		return errors.New("IAM 路由依赖不完整")
	}
	tenantContext, err := middleware.NewIAMContextGuard("tenant")
	if err != nil {
		return err
	}
	tenantServiceContext, err := middleware.NewIAMServiceContextGuard("tenant")
	if err != nil {
		return err
	}
	executionAuthorizationIssue, err := middleware.NewIAMPermissionGuard("system.execution_authorization.create")
	if err != nil {
		return err
	}
	executionAuthorizationExecute, err := middleware.NewIAMPermissionGuard("system.execution_authorization.execute")
	if err != nil {
		return err
	}
	notebookEngineCatalogIssue, err := middleware.NewIAMPermissionGuard("system.engine.read")
	if err != nil {
		return err
	}
	notebookEngineCatalogExecute, err := middleware.NewIAMPermissionGuard("system.notebook_session_authorization.execute")
	if err != nil {
		return err
	}
	developClient, err := middleware.NewIAMClientGuard("addp-develop")
	if err != nil {
		return err
	}
	orchestratorExecute, err := middleware.NewIAMPermissionGuard("orchestrator.workflow.execute")
	if err != nil {
		return err
	}

	publicRateLimit := middleware.OAuthPublicRateLimitMiddleware(
		redisClient,
		int64(runtime.SecurityPolicy.OAuthPublicRateLimitPerMinute),
	)
	userRateLimit := middleware.OAuthUserRateLimitMiddleware(
		redisClient,
		int64(runtime.SecurityPolicy.OAuthUserRateLimitPerMinute),
	)
	api.Use(runtime.OAuthFailureAudit)

	api.POST("/login", runtime.AuthHandler.Login)
	api.POST("/auth/mfa-verifications", publicRateLimit, runtime.AuthHandler.VerifyMFA)
	api.POST("/refresh", runtime.AuthHandler.Refresh)
	api.POST("/logout", runtime.AuthHandler.Logout)
	api.POST("/auth/context-selections", runtime.AuthHandler.ConsumeContextSelection)
	api.GET("/auth/context", runtime.AuthHandler.Context)
	api.POST("/tenant/invitations/enrollments", publicRateLimit, runtime.TenantInvitationHandler.Enroll)
	api.POST("/tenant/invitations/registrations", publicRateLimit, runtime.TenantInvitationHandler.Register)
	api.POST("/tenant/invitations/acceptances", publicRateLimit, runtime.TenantInvitationHandler.Accept)

	auth := api.Group("/auth")
	auth.Use(runtime.Authentication)
	{
		auth.GET("/context-options", runtime.FirstPartyCredential, runtime.AuthHandler.ContextOptions)
		auth.POST("/context-switches", runtime.FirstPartyCredential, runtime.AuthHandler.SwitchContext)
		auth.GET("/mfa", runtime.FirstPartyCredential, runtime.MFASessionHandler.Status)
		auth.POST("/mfa/totp-enrollments", runtime.FirstPartyCredential, userRateLimit, runtime.MFASessionHandler.BeginEnrollment)
		auth.POST("/mfa/totp-enrollment-verifications", runtime.FirstPartyCredential, userRateLimit, runtime.MFASessionHandler.CompleteEnrollment)
		auth.POST("/mfa/step-up-challenges", runtime.FirstPartyCredential, userRateLimit, runtime.MFASessionHandler.BeginStepUp)
		auth.POST("/mfa/step-up-verifications", runtime.FirstPartyCredential, userRateLimit, runtime.MFASessionHandler.CompleteStepUp)
		auth.POST("/delegations", runtime.UserAccessCredential, runtime.DelegationHandler.CreateDelegation)
		auth.POST("/execution-authorizations", runtime.UserAccessCredential, tenantContext, executionAuthorizationIssue, runtime.ExecutionAuthorizationHandler.Issue)
		auth.POST("/notebook-session-authorizations", runtime.UserAccessCredential, tenantContext, notebookEngineCatalogIssue, runtime.NotebookSessionAuthorizationHandler.Issue)
		auth.POST("/task-authorization-subjects", runtime.UserAccessCredential, tenantContext, orchestratorExecute, runtime.TaskAuthorizationSubjectHandler.Authorize)
	}

	api.POST("/execution-authorizations/:id/engine-accesses", runtime.Authentication, runtime.ServiceCredential, tenantServiceContext, executionAuthorizationExecute, runtime.ExecutionAuthorizationHandler.AuthorizeEngineAccess)
	api.GET("/notebook-session-authorizations/:id/engine-descriptors", runtime.Authentication, runtime.ServiceCredential, tenantServiceContext, developClient, notebookEngineCatalogExecute, runtime.NotebookSessionAuthorizationHandler.ListEngineDescriptors)
	api.POST("/notebook-session-authorizations/:id/catalog/children", runtime.Authentication, runtime.ServiceCredential, tenantServiceContext, developClient, notebookEngineCatalogExecute, runtime.NotebookSessionAuthorizationHandler.ListEngineCatalogChildren)
	api.POST("/notebook-session-authorizations/:id/execution-engine-accesses", runtime.Authentication, runtime.ServiceCredential, tenantServiceContext, developClient, notebookEngineCatalogExecute, runtime.NotebookSessionAuthorizationHandler.DeriveExecutionEngineAccess)
	api.POST("/notebook-session-authorizations/:id/revocations", runtime.Authentication, runtime.ServiceCredential, tenantServiceContext, developClient, notebookEngineCatalogExecute, runtime.NotebookSessionAuthorizationHandler.Revoke)

	users := api.Group("/users")
	users.Use(runtime.Authentication, runtime.FirstPartyCredential)
	{
		users.GET("/me", runtime.UserSelfHandler.Me)
		users.PUT("/me/password", runtime.UserSelfHandler.ChangePassword)
	}

	oauth := api.Group("/oauth")
	{
		oauth.POST("/authorization_requests", publicRateLimit, runtime.OAuthHandler.CreateAuthorizationRequest)
		oauth.DELETE("/authorization_requests/:request_id", publicRateLimit, runtime.OAuthHandler.CancelAuthorizationRequest)
		oauth.POST("/device/code", publicRateLimit, runtime.OAuthHandler.DeviceCode)
		oauth.POST("/token", publicRateLimit, runtime.OAuthHandler.Token)
		oauth.POST("/revoke", publicRateLimit, runtime.OAuthHandler.Revoke)

		consent := oauth.Group("")
		consent.Use(runtime.Authentication, runtime.FirstPartyCredential)
		{
			consent.GET("/authorization_requests/:request_id", runtime.OAuthHandler.GetAuthorizationRequest)
			consent.POST("/authorizations", userRateLimit, runtime.OAuthHandler.Authorize)
			consent.POST("/device/authorizations", userRateLimit, runtime.OAuthHandler.DecideDeviceAuthorization)
		}
	}
	return nil
}
