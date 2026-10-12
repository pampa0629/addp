package planner

import (
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/transfer/internal/executor"
)

// CheckableTableDefinition follows the runtime metadata transform without
// building a plan, resolving a query, or opening a data connection.
func CheckableTableDefinition(spec TableExportTaskSpec, info *datatype.TableInfo, spatial *datatype.SpatialInfo, sourceComplete bool) (plugin.TableWriteDefinition, error) {
	for _, transform := range spec.Transforms {
		if strings.ToLower(strings.TrimSpace(transform.Type)) != "field_mapping" {
			return plugin.TableWriteDefinition{}, fmt.Errorf("transform definition not checked")
		}
	}
	return executor.TransformTableDefinition(info, spatial, buildTableTransforms(spec.Transforms), sourceComplete)
}
