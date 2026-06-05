package scantask

import (
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

func TaskParameters(scanDepth string, force bool) models.JSONMap {
	return models.JSONMap{
		"scan_depth": scanDepth,
		"force":      force,
	}
}

func AutomaticTaskParameters() models.JSONMap {
	return EngineTaskParameters(scanflow.ScanDepthDeep)
}

func EngineTaskParameters(scanDepth string) models.JSONMap {
	if scanDepth == "" {
		scanDepth = scanflow.ScanDepthDeep
	}
	return models.JSONMap{
		"scan_depth": scanDepth,
		"force":      false,
	}
}
