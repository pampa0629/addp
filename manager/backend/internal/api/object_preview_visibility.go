package api

import (
	"context"
	"errors"

	"github.com/addp/common/authorization"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/preview"
	"github.com/gin-gonic/gin"
)

type objectPreviewVisibilityChecker interface {
	CheckManagerObjectPreview(context.Context, string, []plugin.EngineCatalogPath) (*authorization.SourceObjectPreviewCheck, error)
}

func checkObjectPreviewVisibility(c *gin.Context, req *preview.PreviewResolverRequest, checker objectPreviewVisibilityChecker) (bool, error) {
	if checker == nil {
		return false, commonClient.ErrMetadataQueryUnavailable
	}
	target := plugin.FileItemPath(req.Engine.ID, req.ItemFullName)
	request := authorization.SourceMetadataCheckRequest{Targets: []plugin.EngineCatalogPath{target}}
	result, err := checker.CheckManagerObjectPreview(c.Request.Context(), bearerToken(c), request.Targets)
	if err != nil {
		return false, err
	}
	if !result.Matches(request) {
		return false, commonClient.ErrMetadataQueryUnavailable
	}
	if !result.Targets[0].Visible || result.Targets[0].ItemFingerprint != req.ItemFingerprint {
		return false, plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorNotFound, errors.New("preview target unavailable"))
	}
	return result.Targets[0].ContentReadable, nil
}
