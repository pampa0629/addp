package modulelifecycle

import (
	"github.com/addp/common/httpmetrics"
	"github.com/gin-gonic/gin"
)

// NewRouter installs the sole HTTP collector before any route or rejection
// middleware. Optional logger middleware precedes the shared Gin recovery.
func NewRouter(handlers ...gin.HandlerFunc) *gin.Engine {
	router := gin.New()
	router.Use(httpmetrics.Process().Middleware())
	router.Use(handlers...)
	router.Use(httpmetrics.Recovery())
	return router
}
