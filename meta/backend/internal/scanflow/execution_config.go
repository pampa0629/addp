package scanflow

import (
	"strings"

	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
)

type ExecutionConfig struct {
	EngineID     uint
	ItemID       uint
	StorageType  string
	CatalogPaths []string
	RefGroups    []models.ScanRefGroup
	ScanDepth    string
	Force        bool
	Source       string
	Token        string
	PlannedRunAt string
}

func ManualExecutionConfig(engineID uint, itemID uint, storageType string, catalogPaths []string, refGroups []models.ScanRefGroup, scanDepth string, force bool, source string, token string) commonModels.JSONMap {
	config := commonModels.JSONMap{
		"engine_id":     engineID,
		"storage_type":  storageType,
		"catalog_paths": catalogPaths,
		"ref_groups":    refGroups,
		"scan_depth":    scanDepth,
		"force":         force,
		"source":        source,
		"token":         token,
	}
	if itemID > 0 {
		config["item_id"] = itemID
	}
	return config
}

func TaskExecutionConfig(engineID uint, storageType string, scope models.JSONMap, params models.JSONMap, defaultScanDepth string, source string) commonModels.JSONMap {
	targets := TargetsFromScope(scope)
	return TargetExecutionConfig(
		engineID,
		storageType,
		targets.CatalogPaths,
		targets.RefGroups,
		jsonMapString(params, "scan_depth", defaultScanDepth),
		jsonMapBool(params, "force", false),
		source,
		"",
	)
}

func TargetExecutionConfig(engineID uint, storageType string, catalogPaths []string, refGroups []models.ScanRefGroup, scanDepth string, force bool, source string, plannedRunAt string) commonModels.JSONMap {
	config := commonModels.JSONMap{
		"engine_id":     engineID,
		"storage_type":  storageType,
		"catalog_paths": catalogPaths,
		"ref_groups":    refGroups,
		"scan_depth":    scanDepth,
		"force":         force,
	}
	if source != "" {
		config["source"] = source
	}
	if plannedRunAt != "" {
		config["planned_run_at"] = plannedRunAt
	}
	return config
}

func ParseExecutionConfig(config commonModels.JSONMap) ExecutionConfig {
	var parsed ExecutionConfig
	if config == nil {
		return parsed
	}
	parsed.EngineID = uintFromInterface(config["engine_id"])
	parsed.ItemID = uintFromInterface(config["item_id"])
	parsed.StorageType, _ = config["storage_type"].(string)
	parsed.ScanDepth, _ = config["scan_depth"].(string)
	parsed.Force = boolFromInterface(config["force"])
	parsed.Source, _ = config["source"].(string)
	parsed.Token, _ = config["token"].(string)
	parsed.PlannedRunAt, _ = config["planned_run_at"].(string)
	parsed.CatalogPaths = StringSliceFromInterface(config["catalog_paths"])
	parsed.RefGroups = RefGroupsFromMap(models.JSONMap(config))
	return parsed
}

func jsonMapString(m models.JSONMap, key, defaultVal string) string {
	if m == nil {
		return defaultVal
	}
	v, ok := m[key]
	if !ok {
		return defaultVal
	}
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return defaultVal
}

func jsonMapBool(m models.JSONMap, key string, defaultVal bool) bool {
	if m == nil {
		return defaultVal
	}
	v, ok := m[key]
	if !ok {
		return defaultVal
	}
	return boolFromInterfaceWithDefault(v, defaultVal)
}

func boolFromInterface(raw interface{}) bool {
	return boolFromInterfaceWithDefault(raw, false)
}

func boolFromInterfaceWithDefault(raw interface{}, defaultVal bool) bool {
	switch v := raw.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "y":
			return true
		case "false", "0", "no", "n":
			return false
		}
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	}
	return defaultVal
}

func uintFromInterface(raw interface{}) uint {
	switch v := raw.(type) {
	case uint:
		return v
	case int:
		return uint(v)
	case int64:
		return uint(v)
	case float64:
		return uint(v)
	}
	return 0
}
