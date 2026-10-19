package api

import (
	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"io"
)

func bindEngineDeployment(c *gin.Context, dst interface{}) error {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return commonapi.ErrBadRequest
	}
	return service.DecodeDeploymentJSON(raw, dst)
}
