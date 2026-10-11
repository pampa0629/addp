package executor

import (
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

// TransformTableDefinition reuses the runtime's metadata transform; it does
// not open a reader/writer or transform rows. Bindings keep source facts apart
// from target annotations so a type annotation cannot invent input evidence.
func TransformTableDefinition(info *datatype.TableInfo, spatial *datatype.SpatialInfo, plans []TableTransformPlan) (plugin.TableWriteDefinition, error) {
	bindings := map[string]plugin.TableWriteFieldDefinition{}
	if info == nil {
		info = &datatype.TableInfo{}
	}
	for _, field := range info.Fields {
		original := field
		bindings[field.Name] = plugin.TableWriteFieldDefinition{Target: field, Source: &original}
	}
	next := info.Clone()
	nextSpatial := spatial.Clone()
	for _, plan := range plans {
		if strings.ToLower(strings.TrimSpace(plan.Type)) != "field_mapping" || plan.FieldMapping == nil {
			return plugin.TableWriteDefinition{}, fmt.Errorf("transform definition not checked")
		}
		transforms, err := buildTableTransforms([]TableTransformPlan{plan}, nil)
		if err != nil {
			return plugin.TableWriteDefinition{}, err
		}
		mapped, mappedSpatial, err := applyTableInfoTransforms(next, nextSpatial, transforms)
		if err != nil {
			return plugin.TableWriteDefinition{}, err
		}
		origins := map[string]plugin.TableWriteFieldDefinition{}
		if plan.FieldMapping.Mode == FieldMappingModePassthrough {
			for key, value := range bindings {
				origins[key] = value
			}
		}
		for _, mapping := range plan.FieldMapping.Fields {
			binding, found := bindings[strings.TrimSpace(mapping.Source)]
			if strings.TrimSpace(mapping.Source) == "" || !found {
				binding = plugin.TableWriteFieldDefinition{Constant: &plugin.TableWriteConstant{Value: mapping.Default}}
			} else if mapping.Default != nil && (binding.Constant == nil || binding.Constant.Value == nil) {
				binding.Constant = &plugin.TableWriteConstant{Value: mapping.Default}
			}
			origins[strings.TrimSpace(mapping.Target)] = binding
		}
		bindings, next, nextSpatial = origins, mapped, mappedSpatial
	}
	definition := plugin.TableWriteDefinition{SpatialInfo: nextSpatial}
	for _, field := range next.Fields {
		binding := bindings[field.Name]
		binding.Target = field
		definition.Fields = append(definition.Fields, binding)
	}
	return definition, nil
}
