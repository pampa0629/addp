package api

import (
	"fmt"

	"github.com/addp/standard/internal/models"
	"github.com/gin-gonic/gin"
)

func parseOptionalStandardScope(c *gin.Context) (string, error) {
	values := c.Request.URL.Query()["scope_type"]
	if len(values) > 1 {
		return "", fmt.Errorf("duplicate scope_type query parameter")
	}
	if len(values) == 0 {
		return "", nil
	}
	value := values[0]
	switch value {
	case "", models.StandardScopePlatform, models.StandardScopeTenantCommon, models.StandardScopeDomain:
		return value, nil
	default:
		return "", fmt.Errorf("invalid scope_type")
	}
}
