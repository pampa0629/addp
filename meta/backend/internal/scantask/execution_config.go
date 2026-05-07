package scantask

import (
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
)

type ExecutionConfig struct {
	EngineID    uint
	StorageType string
	Namespaces  []string
	ObjectPaths []string
	ScanDepth   string
	Token       string
}

func ManualExecutionConfig(engineID uint, storageType string, namespaces, objectPaths []string, scanDepth, token string) commonModels.JSONMap {
	return commonModels.JSONMap{
		"engine_id":    engineID,
		"storage_type": storageType,
		"namespaces":   namespaces,
		"object_paths": objectPaths,
		"scan_depth":   scanDepth,
		"token":        token,
	}
}

func TaskExecutionConfig(engineID uint, storageType string, params models.JSONMap, defaultScanDepth string) commonModels.JSONMap {
	return TargetExecutionConfig(
		engineID,
		storageType,
		JSONMapStringSlice(params, "namespaces"),
		JSONMapStringSlice(params, "object_paths"),
		JSONMapString(params, "scan_depth", defaultScanDepth),
	)
}

func TargetExecutionConfig(engineID uint, storageType string, namespaces, objectPaths []string, scanDepth string) commonModels.JSONMap {
	return commonModels.JSONMap{
		"engine_id":    engineID,
		"storage_type": storageType,
		"namespaces":   namespaces,
		"object_paths": objectPaths,
		"scan_depth":   scanDepth,
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
	parsed.Token, _ = config["token"].(string)
	parsed.Namespaces = StringSliceFromInterface(config["namespaces"])
	parsed.ObjectPaths = StringSliceFromInterface(config["object_paths"])
	return parsed
}

func TaskParameters(namespaces, objectPaths []string, scanDepth string) models.JSONMap {
	return models.JSONMap{
		"namespaces":   namespaces,
		"object_paths": objectPaths,
		"scan_depth":   scanDepth,
	}
}
