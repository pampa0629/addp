package execution

import (
	"encoding/json"
	"errors"
	"reflect"

	"github.com/addp/common/engine/plugin"
)

// ValidateProductionReadSet is the bounded pure-value contract for the first
// PG production slice. Nil means unavailable, never unbounded permission or a
// proven source-free generation. The owner may not normalize or fill old proof.
func ValidateProductionReadSet(set *plugin.QueryReadSet) error {
	if set == nil {
		return nil
	}
	if len(set.Paths) == 0 || len(set.Paths) > 200 {
		return errors.New("invalid production source read set")
	}
	normalized, err := plugin.NewQueryReadSet(set.Paths...)
	if err != nil || !reflect.DeepEqual(normalized, set) {
		return errors.New("noncanonical production source read set")
	}
	for _, path := range set.Paths {
		if len(path.Segments) != 3 || int64(path.EngineID) <= 0 || !reflect.DeepEqual(path, plugin.TabularItemPath(path.EngineID, plugin.EngineCatalogTermSchema, path.Segments[1].Name, path.Segments[2].Name)) {
			return errors.New("unsupported production source read set")
		}
	}
	if encoded, err := json.Marshal(set); err != nil || len(encoded) > 128<<10 {
		return errors.New("production source read set exceeds contract size")
	}
	return nil
}
