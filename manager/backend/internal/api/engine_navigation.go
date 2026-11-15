package api

import (
	"github.com/addp/common/authorization"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
	"github.com/gin-gonic/gin"
)

// Engine visibility observes current sources independently of node Grant admission.
func (h *ExplorerHandler) filterVisibleEngines(c *gin.Context, engines []*models.Engine) ([]*models.Engine, error) {
	targets := []plugin.EngineCatalogPath{}
	pending := map[uint]bool{}
	for _, engine := range engines {
		model, err := dbbridge.EngineCatalogModel(engine.EngineType)
		if err != nil {
			continue
		}
		targets = append(targets, plugin.EngineCatalogRootPath(model, engine.ID))
	}
	for offset := 0; offset < len(targets); offset += 200 {
		end := offset + 200
		if end > len(targets) {
			end = len(targets)
		}
		query := authorization.SourceNavigationCheckRequest{Targets: targets[offset:end]}
		if h.navigationChecker == nil {
			return nil, commonclient.ErrMetadataQueryUnavailable
		}
		result, err := h.navigationChecker.CheckManagerNavigation(c.Request.Context(), bearerToken(c), query)
		if err != nil {
			return nil, err
		}
		if !result.MatchesNavigation(query) {
			return nil, commonclient.ErrMetadataQueryUnavailable
		}
		for _, target := range result.Targets {
			pending[target.Target.EngineID] = target.Visible
		}
	}
	result := make([]*models.Engine, 0, len(engines))
	for _, engine := range engines {
		if pending[engine.ID] {
			result = append(result, engine)
		}
	}
	return result, nil
}
