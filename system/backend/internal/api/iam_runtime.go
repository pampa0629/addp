package api

import (
	"fmt"
	"time"

	systemauthorization "github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/iam"
	iamoauth "github.com/addp/system/internal/iam/oauth"
	"github.com/addp/system/internal/middleware"
	"github.com/addp/system/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// IAMRuntime is the single production composition root for System IAM.
type IAMRuntime struct {
	Repository                          *iam.Repository
	TokenFamilyService                  *iam.TokenFamilyService
	IdentityService                     *iam.IdentityService
	MFAService                          *iam.MFAService
	MFASessionService                   *iam.MFASessionService
	TenantMembershipService             *iam.TenantMembershipService
	OrganizationService                 *iam.OrganizationService
	TenantInvitationService             *iam.TenantInvitationService
	TenantRoleService                   *iam.TenantRoleService
	PlatformTenantService               *iam.PlatformTenantService
	PlatformUserService                 *iam.PlatformUserService
	AuditQueryService                   *iam.AuditQueryService
	PrivilegedIdentityChangeService     *iam.PrivilegedIdentityChangeService
	SecurityPolicyService               *iam.SecurityPolicyService
	ContextSelectionService             *iam.ContextSelectionService
	BrowserLoginService                 *iam.BrowserLoginService
	AuthContextService                  *iam.AuthContextService
	ContextOptionsService               *iam.ContextOptionsService
	ContextSwitchService                *iam.ContextSwitchService
	LogoutService                       *iam.LogoutService
	UserSelfService                     *iam.UserSelfService
	DelegationService                   *iam.DelegationService
	ExecutionAuthorizationService       *iam.ExecutionAuthorizationService
	NotebookSessionAuthorizationService *iam.NotebookSessionAuthorizationService
	TaskAuthorizationSubjectService     *iam.TaskAuthorizationSubjectService
	CatalogReferenceService             *iam.CatalogReferenceService
	OAuthProvider                       *iamoauth.Provider
	ConsentBridge                       *iamoauth.ConsentBridge

	AuthHandler                         *IAMAuthHandler
	MFASessionHandler                   *IAMMFASessionHandler
	OAuthHandler                        *IAMOAuthHandler
	DelegationHandler                   *IAMDelegationHandler
	ExecutionAuthorizationHandler       *IAMExecutionAuthorizationHandler
	NotebookSessionAuthorizationHandler *IAMNotebookSessionAuthorizationHandler
	TaskAuthorizationSubjectHandler     *IAMTaskAuthorizationSubjectHandler
	CatalogReferenceHandler             *IAMCatalogReferenceHandler
	UserSelfHandler                     *IAMUserSelfHandler
	PlatformTenantHandler               *IAMPlatformTenantHandler
	PlatformUserHandler                 *IAMPlatformUserHandler
	TenantMembershipHandler             *IAMTenantMembershipHandler
	OrganizationHandler                 *IAMOrganizationHandler
	TenantInvitationHandler             *IAMTenantInvitationHandler
	TenantRoleHandler                   *IAMTenantRoleHandler
	InternalAuditHandler                *IAMInternalAuditHandler
	AuditHandler                        *IAMAuditHandler
	PrivilegedIdentityChangeHandler     *IAMPrivilegedIdentityChangeHandler
	SecurityPolicyHandler               *IAMSecurityPolicyHandler
	SecurityPolicy                      iam.SecurityPolicy

	Authentication       gin.HandlerFunc
	FirstPartyCredential gin.HandlerFunc
	UserAccessCredential gin.HandlerFunc
	BusinessCredential   gin.HandlerFunc
	ServiceCredential    gin.HandlerFunc
	OAuthFailureAudit    gin.HandlerFunc
}

func NewIAMRuntime(db *gorm.DB, cfg *config.Config, securityPolicy iam.SecurityPolicy) (*IAMRuntime, error) {
	if db == nil || cfg == nil {
		return nil, fmt.Errorf("IAM Runtime 数据库和配置不能为空")
	}
	if err := iam.ValidateSecurityPolicy(securityPolicy); err != nil {
		return nil, fmt.Errorf("IAM 安全策略无效: %w", err)
	}
	accessTokenTTL := time.Duration(securityPolicy.AccessTokenTTLMinutes) * time.Minute
	refreshTokenTTL := time.Duration(securityPolicy.RefreshTokenTTLDays) * 24 * time.Hour
	authorizationCodeTTL := time.Duration(securityPolicy.OAuthAuthorizationCodeTTLMinutes) * time.Minute
	deviceCodeTTL := time.Duration(securityPolicy.OAuthDeviceCodeTTLMinutes) * time.Minute
	devicePollingInterval := time.Duration(securityPolicy.OAuthDevicePollIntervalSeconds) * time.Second
	delegatedAccessTokenTTL := time.Duration(securityPolicy.DelegatedAccessTokenTTLMinutes) * time.Minute
	resourceTicketTTL := time.Duration(securityPolicy.ResourceAccessTicketTTLMinutes) * time.Minute
	invitationTTL := time.Duration(securityPolicy.TenantInvitationTTLHours) * time.Hour
	enrollmentTicketTTL := time.Duration(securityPolicy.EnrollmentTicketTTLMinutes) * time.Minute

	repository := iam.NewRepository(db)
	securityPolicyService := iam.NewSecurityPolicyService(repository)
	tokenFamilyService, err := iam.NewTokenFamilyService(repository, iam.BrowserSessionConfig{
		AccessTokenTTL:          accessTokenTTL,
		RefreshTokenFamilyTTL:   refreshTokenTTL,
		ResourceAccessTicketTTL: resourceTicketTTL,
		ResourceTicketOwners:    models.BrowserResourceAccessOwners,
	}, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Token Family Service: %w", err)
	}
	identityService := iam.NewIdentityService(repository, nil)
	mfaCipher, err := iam.NewMFACredentialCipher(cfg.IAMMFAEncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM MFA Credential Cipher: %w", err)
	}
	mfaService, err := iam.NewMFAService(repository, mfaCipher, iam.MFAServiceConfig{}, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM MFA Service: %w", err)
	}
	mfaSessionService, err := iam.NewMFASessionService(
		repository, mfaCipher, tokenFamilyService, iam.MFAServiceConfig{}, nil, nil,
	)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM MFA Session Service: %w", err)
	}
	tenantInvitationService, err := iam.NewTenantInvitationService(
		repository, identityService, tokenFamilyService, iam.TenantInvitationServiceConfig{
			InvitationTTL: invitationTTL, EnrollmentTicketTTL: enrollmentTicketTTL,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Tenant Invitation Service: %w", err)
	}
	tenantMembershipService := iam.NewTenantMembershipService(repository, nil)
	organizationService := iam.NewOrganizationService(repository, nil)
	tenantRoleService := iam.NewTenantRoleService(repository, nil)
	platformTenantService := iam.NewPlatformTenantService(repository, nil)
	platformUserService := iam.NewPlatformUserService(repository, identityService, nil)
	auditQueryService := iam.NewAuditQueryService(repository)
	privilegedIdentityChangeService := iam.NewPrivilegedIdentityChangeService(repository, nil)
	contextSelectionService, err := iam.NewContextSelectionService(repository, tokenFamilyService)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Context Selection Service: %w", err)
	}
	browserLoginService, err := iam.NewBrowserLoginService(identityService, mfaService, contextSelectionService)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Browser Login Service: %w", err)
	}
	authContextService, err := iam.NewAuthContextService(repository)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM AuthContext Service: %w", err)
	}
	contextOptionsService, err := iam.NewContextOptionsService(repository)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Context Options Service: %w", err)
	}
	contextSwitchService, err := iam.NewContextSwitchService(repository, tokenFamilyService)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Context Switch Service: %w", err)
	}
	logoutService, err := iam.NewLogoutService(repository, tokenFamilyService)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Logout Service: %w", err)
	}
	userSelfService, err := iam.NewUserSelfService(repository, identityService)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM User Self Service: %w", err)
	}
	delegationService, err := iam.NewDelegationService(
		repository,
		systemauthorization.ToolAuthorizationCatalog{},
		iam.DelegationServiceConfig{AccessTokenTTL: delegatedAccessTokenTTL},
	)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Delegation Service: %w", err)
	}
	executionAuthorizationService, err := iam.NewExecutionAuthorizationService(repository)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Execution Authorization Service: %w", err)
	}
	notebookSessionAuthorizationService, err := iam.NewNotebookSessionAuthorizationService(repository)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Notebook Session Authorization Service: %w", err)
	}
	taskAuthorizationSubjectService, err := iam.NewTaskAuthorizationSubjectService(repository)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Task Authorization Subject Service: %w", err)
	}
	catalogReferenceService, err := iam.NewCatalogReferenceService(repository)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Catalog Reference Service: %w", err)
	}

	providerConfig := iamoauth.ProviderConfig{
		AccessTokenLifespan:        accessTokenTTL,
		RefreshTokenLifespan:       refreshTokenTTL,
		AuthorizeCodeLifespan:      authorizationCodeTTL,
		DeviceCodeLifespan:         deviceCodeTTL,
		DevicePollingInterval:      devicePollingInterval,
		DeviceVerificationURL:      cfg.ConsoleURL + "/oauth/device",
		TokenEndpointURL:           cfg.PublicAPIURL + "/api/v1/system/oauth/token",
		SendDebugMessagesToClients: false,
	}
	strategyConfig := iamoauth.StrategyConfig{
		AccessTokenLifespan:    accessTokenTTL,
		RefreshTokenLifespan:   refreshTokenTTL,
		AuthorizeCodeLifespan:  authorizationCodeTTL,
		DeviceCodeLifespan:     deviceCodeTTL,
		UserCodePepper:         append([]byte(nil), cfg.OAuthUserCodePepper...),
		PreviousUserCodePepper: append([]byte(nil), cfg.OAuthPreviousUserCodePepper...),
	}
	oauthProvider, err := iamoauth.NewProvider(db, providerConfig, strategyConfig)
	if err != nil {
		return nil, fmt.Errorf("装配 Fosite OAuth Provider: %w", err)
	}
	consentBridge, err := iamoauth.NewConsentBridge(oauthProvider, repository, authorizationCodeTTL)
	if err != nil {
		return nil, fmt.Errorf("装配 OAuth Consent Bridge: %w", err)
	}

	secureCookies := cfg.Env == "production"
	authHandler, err := NewIAMAuthHandler(
		browserLoginService,
		contextSelectionService,
		authContextService,
		contextOptionsService,
		contextSwitchService,
		tokenFamilyService,
		logoutService,
		IAMAuthHandlerConfig{
			SecureCookies:        secureCookies,
			ResourceTicketOwners: models.BrowserResourceAccessOwners,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Auth Handler: %w", err)
	}
	mfaSessionHandler, err := NewIAMMFASessionHandler(mfaSessionService, authHandler)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM MFA Session Handler: %w", err)
	}
	tenantInvitationHandler, err := NewIAMTenantInvitationHandler(
		tenantInvitationService, authContextService, authHandler, cfg.ConsoleURL,
	)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Tenant Invitation Handler: %w", err)
	}
	internalAuditHandler, err := NewIAMInternalAuditHandler(iam.NewAuditWriter(repository))
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Internal Audit Handler: %w", err)
	}
	oauthHandler, err := NewIAMOAuthHandler(oauthProvider, consentBridge)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM OAuth Handler: %w", err)
	}
	delegationHandler, err := NewIAMDelegationHandler(delegationService, nil)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Delegation Handler: %w", err)
	}
	catalogReferenceHandler, err := NewIAMCatalogReferenceHandler(catalogReferenceService)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Catalog Reference Handler: %w", err)
	}
	userSelfHandler, err := NewIAMUserSelfHandler(
		userSelfService,
		secureCookies,
		models.BrowserResourceAccessOwners,
	)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM User Self Handler: %w", err)
	}
	platformTenantHandler, err := NewIAMPlatformTenantHandler(platformTenantService)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Platform Tenant Handler: %w", err)
	}
	platformUserHandler, err := NewIAMPlatformUserHandler(platformUserService)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Platform User Handler: %w", err)
	}
	tenantMembershipHandler, err := NewIAMTenantMembershipHandler(tenantMembershipService)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Tenant Membership Handler: %w", err)
	}
	organizationHandler, err := NewIAMOrganizationHandler(organizationService)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Organization Handler: %w", err)
	}
	tenantRoleHandler, err := NewIAMTenantRoleHandler(tenantRoleService)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Tenant Role Handler: %w", err)
	}
	auditHandler, err := NewIAMAuditHandler(auditQueryService)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Audit Handler: %w", err)
	}
	privilegedIdentityChangeHandler, err := NewIAMPrivilegedIdentityChangeHandler(privilegedIdentityChangeService)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Privileged Identity Change Handler: %w", err)
	}
	securityPolicyHandler, err := NewIAMSecurityPolicyHandler(securityPolicyService)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Security Policy Handler: %w", err)
	}

	authentication, err := middleware.NewIAMAuthenticationMiddleware(authContextService)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Authentication Middleware: %w", err)
	}
	firstPartyCredential, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeFirstPartyAccess)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM first-party Credential Guard: %w", err)
	}
	userAccessCredential, err := middleware.NewIAMCredentialGuard(
		middleware.IAMTokenTypeFirstPartyAccess,
		middleware.IAMTokenTypeOAuthAccess,
	)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM User Access Credential Guard: %w", err)
	}
	serviceCredential, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeServiceAccess)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Service Access Credential Guard: %w", err)
	}
	businessCredential, err := middleware.NewIAMCredentialGuard(
		middleware.IAMTokenTypeFirstPartyAccess,
		middleware.IAMTokenTypeOAuthAccess,
		middleware.IAMTokenTypeDelegatedAccess,
		middleware.IAMTokenTypeServiceAccess,
	)
	if err != nil {
		return nil, fmt.Errorf("装配 IAM Business Credential Guard: %w", err)
	}
	oauthFailureAudit, err := middleware.NewIAMOAuthFailureAuditMiddleware(iam.NewAuditWriter(repository))
	if err != nil {
		return nil, fmt.Errorf("装配 IAM OAuth Failure Audit Middleware: %w", err)
	}

	return &IAMRuntime{
		Repository:                          repository,
		TokenFamilyService:                  tokenFamilyService,
		IdentityService:                     identityService,
		MFAService:                          mfaService,
		MFASessionService:                   mfaSessionService,
		TenantMembershipService:             tenantMembershipService,
		OrganizationService:                 organizationService,
		TenantInvitationService:             tenantInvitationService,
		TenantRoleService:                   tenantRoleService,
		PlatformTenantService:               platformTenantService,
		PlatformUserService:                 platformUserService,
		AuditQueryService:                   auditQueryService,
		PrivilegedIdentityChangeService:     privilegedIdentityChangeService,
		SecurityPolicyService:               securityPolicyService,
		SecurityPolicy:                      securityPolicy,
		ContextSelectionService:             contextSelectionService,
		BrowserLoginService:                 browserLoginService,
		AuthContextService:                  authContextService,
		ContextOptionsService:               contextOptionsService,
		ContextSwitchService:                contextSwitchService,
		LogoutService:                       logoutService,
		UserSelfService:                     userSelfService,
		DelegationService:                   delegationService,
		ExecutionAuthorizationService:       executionAuthorizationService,
		NotebookSessionAuthorizationService: notebookSessionAuthorizationService,
		TaskAuthorizationSubjectService:     taskAuthorizationSubjectService,
		CatalogReferenceService:             catalogReferenceService,
		OAuthProvider:                       oauthProvider,
		ConsentBridge:                       consentBridge,
		AuthHandler:                         authHandler,
		MFASessionHandler:                   mfaSessionHandler,
		OAuthHandler:                        oauthHandler,
		DelegationHandler:                   delegationHandler,
		CatalogReferenceHandler:             catalogReferenceHandler,
		UserSelfHandler:                     userSelfHandler,
		PlatformTenantHandler:               platformTenantHandler,
		PlatformUserHandler:                 platformUserHandler,
		TenantMembershipHandler:             tenantMembershipHandler,
		OrganizationHandler:                 organizationHandler,
		TenantInvitationHandler:             tenantInvitationHandler,
		TenantRoleHandler:                   tenantRoleHandler,
		InternalAuditHandler:                internalAuditHandler,
		AuditHandler:                        auditHandler,
		PrivilegedIdentityChangeHandler:     privilegedIdentityChangeHandler,
		SecurityPolicyHandler:               securityPolicyHandler,
		Authentication:                      authentication,
		FirstPartyCredential:                firstPartyCredential,
		UserAccessCredential:                userAccessCredential,
		BusinessCredential:                  businessCredential,
		ServiceCredential:                   serviceCredential,
		OAuthFailureAudit:                   oauthFailureAudit,
	}, nil
}
