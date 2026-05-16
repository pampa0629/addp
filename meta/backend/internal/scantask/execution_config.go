package scantask

import (
	"fmt"
	"strings"

	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
)

const (
	ScanDepthBasic = "basic"
	ScanDepthDeep  = "deep"
)

type ExecutionConfig struct {
	EngineID     uint
	StorageType  string
	CatalogPaths []string
	ScanDepth    string
	Force        bool
	Token        string
}

func ManualExecutionConfig(engineID uint, storageType string, catalogPaths []string, scanDepth string, force bool, token string) commonModels.JSONMap {
	return commonModels.JSONMap{
		"engine_id":     engineID,
		"storage_type":  storageType,
		"catalog_paths": catalogPaths,
		"scan_depth":    scanDepth,
		"force":         force,
		"token":         token,
	}
}

func TaskExecutionConfig(engineID uint, storageType string, params models.JSONMap, defaultScanDepth string) commonModels.JSONMap {
	return TargetExecutionConfig(
		engineID,
		storageType,
		catalogPathsFromParams(params),
		JSONMapString(params, "scan_depth", defaultScanDepth),
		JSONMapBool(params, "force", false),
	)
}

func TargetExecutionConfig(engineID uint, storageType string, catalogPaths []string, scanDepth string, force bool) commonModels.JSONMap {
	return commonModels.JSONMap{
		"engine_id":     engineID,
		"storage_type":  storageType,
		"catalog_paths": catalogPaths,
		"scan_depth":    scanDepth,
		"force":         force,
	}
}

func ParseExecutionConfig(config commonModels.JSONMap) ExecutionConfig {
	var parsed ExecutionConfig
	if config == nil {
		return parsed
	}

	if v, ok := config["engine_id"]; ok {
		switch val := v.(type) {
		case uint:
			parsed.EngineID = val
		case int:
			parsed.EngineID = uint(val)
		case int64:
			parsed.EngineID = uint(val)
		case float64:
			parsed.EngineID = uint(val)
		}
	}
	parsed.StorageType, _ = config["storage_type"].(string)
	parsed.ScanDepth, _ = config["scan_depth"].(string)
	parsed.Force = BoolFromInterface(config["force"])
	parsed.Token, _ = config["token"].(string)
	parsed.CatalogPaths = catalogPathsFromCommonConfig(config)
	return parsed
}

func TaskParameters(catalogPaths []string, scanDepth string, force bool) models.JSONMap {
	return models.JSONMap{
		"catalog_paths": catalogPaths,
		"scan_depth":    scanDepth,
		"force":         force,
	}
}

func catalogPathsFromParams(params models.JSONMap) []string {
	return JSONMapStringSlice(params, "catalog_paths")
}

func catalogPathsFromCommonConfig(config commonModels.JSONMap) []string {
	return StringSliceFromInterface(config["catalog_paths"])
}

func NormalizeScanDepth(scanDepth, defaultDepth string) (string, error) {
	if defaultDepth == "" {
		defaultDepth = ScanDepthBasic
	}
	if scanDepth == "" {
		scanDepth = defaultDepth
	}
	scanDepth = strings.ToLower(scanDepth)
	if scanDepth == "shallow" {
		return "", fmt.Errorf("unsupported scan depth %q: use basic or deep", scanDepth)
	}
	if scanDepth != ScanDepthBasic && scanDepth != ScanDepthDeep {
		return "", fmt.Errorf("unsupported scan depth %q: use basic or deep", scanDepth)
	}
	return scanDepth, nil
}
