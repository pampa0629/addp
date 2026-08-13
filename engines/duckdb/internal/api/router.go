package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/addp/common/buildinfo"
	"github.com/addp/common/engine/plugin"
	authmiddleware "github.com/addp/common/middleware/auth"
	"github.com/addp/engines/duckdb/internal/runtime"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type RouterConfig struct {
	SystemURL        string
	AllowedCallerIDs map[string]struct{}
}

func NewRouter(cfg RouterConfig, executor *runtime.Executor) (*gin.Engine, error) {
	authn, err := authmiddleware.NewMiddleware(authmiddleware.MiddlewareConfig{SystemURL: cfg.SystemURL})
	if err != nil {
		return nil, err
	}
	router := gin.New()
	router.Use(gin.Recovery())
	router.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, buildinfo.Health("duckdb")) })
	queries := router.Group("/api/v1/queries", authn)
	queries.POST("", func(c *gin.Context) {
		authContext, ok := authmiddleware.AuthContextFromGin(c)
		if !ok || authContext.Principal.Type != "service_principal" || authContext.Client.ClientID == nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "service principal is required"})
			return
		}
		if _, allowed := cfg.AllowedCallerIDs[*authContext.Client.ClientID]; !allowed {
			c.JSON(http.StatusForbidden, gin.H{"error": "caller is not allowed to invoke DuckDB runtime"})
			return
		}
		tenantIDValue, ok := authmiddleware.TenantIDFromGin(c)
		if !ok {
			c.JSON(http.StatusForbidden, gin.H{"error": "tenant runtime context is required"})
			return
		}
		var body plugin.FederatedQueryRequest
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()
		decoder.UseNumber()
		if err := decoder.Decode(&body); err != nil || uuid.Validate(body.ExecutionID) != nil ||
			!canonicalID(body.ExecutionAuthorizationID) || len(body.SourceEngineIDs) == 0 || strings.TrimSpace(body.Query) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid federated query request"})
			return
		}
		body.Language = strings.ToLower(strings.TrimSpace(body.Language))
		result, err := executor.Execute(c.Request.Context(), uint(tenantIDValue), body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"columns": result.Columns, "rows": result.Rows})
	})
	return router, nil
}

func canonicalID(value string) bool {
	parsed, err := strconv.ParseUint(value, 10, 64)
	return err == nil && parsed > 0 && strconv.FormatUint(parsed, 10) == value
}
