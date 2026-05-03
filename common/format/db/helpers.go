package db

import (
	"fmt"

	"github.com/addp/common/engine/plugin"
)

func dbParserCatalogPath(modelProvider plugin.CatalogModelProvider, schema, table string) plugin.CatalogPath {
	namespaceTerm := plugin.CatalogTermDatabase
	if modelProvider != nil {
		model := modelProvider.CatalogModel()
		if len(model.Levels) > 0 && model.Levels[0].Term != "" {
			namespaceTerm = model.Levels[0].Term
		}
	}
	return plugin.CatalogPath{
		Version: plugin.CatalogPathVersion,
		Segments: []plugin.CatalogSegment{
			{Term: namespaceTerm, Kind: plugin.CatalogKindNamespace, Name: schema},
			{Term: plugin.CatalogTermTable, Kind: plugin.CatalogKindTable, Name: table},
		},
	}
}

func dbParserInt64Stat(stats map[string]interface{}, key string) (int64, bool) {
	if stats == nil {
		return 0, false
	}
	switch value := stats[key].(type) {
	case int64:
		return value, true
	case int:
		return int64(value), true
	case int32:
		return int64(value), true
	case int16:
		return int64(value), true
	case int8:
		return int64(value), true
	case uint:
		return int64(value), true
	case uint64:
		return int64(value), true
	case uint32:
		return int64(value), true
	case float64:
		return int64(value), true
	case float32:
		return int64(value), true
	case []byte:
		var parsed int64
		if _, err := fmt.Sscan(string(value), &parsed); err == nil {
			return parsed, true
		}
	case string:
		var parsed int64
		if _, err := fmt.Sscan(value, &parsed); err == nil {
			return parsed, true
		}
	}
	return 0, false
}
