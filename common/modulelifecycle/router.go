package modulelifecycle

import (
	"github.com/addp/common/httpmetrics"
	"github.com/addp/common/httptrace"
	"github.com/addp/common/middleware/logging"
	"github.com/addp/common/middleware/requestid"
	"github.com/gin-gonic/gin"
)

// NewRouter installs the sole HTTP collector before any route or rejection
// middleware. Request IDs, bounded JSON logging and recovery have one owner.
func NewRouter(handlers ...gin.HandlerFunc) *gin.Engine {
	router := gin.New()
	router.Use(httpmetrics.Process().Middleware())
	router.Use(httptrace.Middleware())
	router.Use(requestid.RequestIDMiddleware())
	router.Use(logging.LoggingMiddleware())
	router.Use(handlers...)
	router.Use(httpmetrics.Recovery())
	return router
}
