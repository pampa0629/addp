package api

import (
	"fmt"
	"strconv"
	"strings"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

func iamTenantUserActor(c *gin.Context) (uint, uint, error) {
	principalID, tenantID, principalType, err := iamTenantActor(c)
	if err != nil {
		return 0, 0, err
	}
	if principalType != "user" {
		return 0, 0, commonapi.ErrForbidden
	}
	return principalID, tenantID, nil
}

func iamServiceOwnsModule(c *gin.Context, moduleName string) error {
	authContext, exists := middleware.IAMAuthContextFromGin(c)
	if !exists {
		return commonapi.ErrUnauthorized
	}
	moduleName = strings.TrimSpace(moduleName)
	if authContext.Principal.Type != "service_principal" ||
		authContext.Client.ClientID == nil || *authContext.Client.ClientID != "addp-"+moduleName {
		return commonapi.ErrForbidden
	}
	return nil
}

func iamTenantActor(c *gin.Context) (uint, uint, string, error) {
	authContext, exists := middleware.IAMAuthContextFromGin(c)
	if !exists {
		return 0, 0, "", commonapi.ErrUnauthorized
	}
	if (authContext.Principal.Type != "user" && authContext.Principal.Type != "service_principal") || authContext.Context.Type != "tenant" ||
		authContext.Context.TenantID == nil || authContext.Context.TenantMembershipID == nil {
		return 0, 0, "", commonapi.ErrForbidden
	}
	principalID, err := parseIAMActorID(authContext.Principal.ID)
	if err != nil {
		return 0, 0, "", fmt.Errorf("invalid IAM principal projection: %w", err)
	}
	tenantID, err := parseIAMActorID(*authContext.Context.TenantID)
	if err != nil {
		return 0, 0, "", fmt.Errorf("invalid IAM tenant projection: %w", err)
	}
	return principalID, tenantID, authContext.Principal.Type, nil
}

func iamPlatformUserActor(c *gin.Context) (int64, error) {
	authContext, exists := middleware.IAMAuthContextFromGin(c)
	if !exists {
		return 0, commonapi.ErrUnauthorized
	}
	if authContext.Principal.Type != "user" || authContext.Context.Type != "platform" ||
		authContext.Context.TenantID != nil || authContext.Context.TenantMembershipID != nil {
		return 0, commonapi.ErrForbidden
	}
	principalID, err := strconv.ParseInt(authContext.Principal.ID, 10, 64)
	if err != nil || principalID <= 0 || strconv.FormatInt(principalID, 10) != authContext.Principal.ID {
		return 0, fmt.Errorf("invalid IAM principal projection: invalid canonical IAM ID")
	}
	return principalID, nil
}

func parseIAMActorID(value string) (uint, error) {
	parsed, err := strconv.ParseUint(value, 10, strconv.IntSize)
	if err != nil || parsed == 0 || strconv.FormatUint(parsed, 10) != value {
		return 0, fmt.Errorf("invalid canonical IAM ID")
	}
	return uint(parsed), nil
}
