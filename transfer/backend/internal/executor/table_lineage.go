package executor

import (
	"context"
	"sort"
	"strings"

	"github.com/addp/common/datatype"
	plugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
)

type TableFieldLineage struct {
	Source   *execution.LineageSchemaSnapshot
	Target   *execution.LineageSchemaSnapshot
	Mappings []execution.LineageFieldMapping
}

// Observe the native schemas inside the pipeline, after target preparation and
// before writing. Query-result columns cannot stand in for source fields.
func (e *TableTransferExecutor) observeFieldLineage(ctx context.Context, plan TableTransferPlan, read, written *datatype.TableInfo) *TableFieldLineage {
	if plan.Source.Kind != TableEndpointNative || strings.TrimSpace(plan.Source.Query) != "" || plan.Target.Kind != TableEndpointNative || e.SourceCatalogFacts == nil || e.TargetCatalogFacts == nil {
		return nil
	}
	source, err := e.SourceCatalogFacts.DescribeEngineCatalogFacts(ctx, plan.Source.ConnInfo, plan.Source.Path, plugin.EngineCatalogFactsOptions{})
	if err != nil || source == nil || source.Table == nil {
		return nil
	}
	target, err := e.TargetCatalogFacts.DescribeEngineCatalogFacts(ctx, plan.Target.ConnInfo, plan.Target.Path, plugin.EngineCatalogFactsOptions{})
	if err != nil || target == nil || target.Table == nil {
		return nil
	}
	return buildTableFieldLineage(source.Table.Fields, target.Table.Fields, read, written, plan.Transforms)
}

func buildTableFieldLineage(sourceFields, targetFields []datatype.FieldInfo, read, written *datatype.TableInfo, plans []TableTransformPlan) *TableFieldLineage {
	source, err := execution.NewLineageSchemaSnapshot(sourceFields)
	if err != nil {
		return nil
	}
	target, err := execution.NewLineageSchemaSnapshot(targetFields)
	if err != nil || read == nil || written == nil {
		return nil
	}
	origins := map[string]execution.LineageFieldMapping{}
	for _, field := range read.Fields {
		if !source.HasField(field.Name) {
			return nil
		}
		origins[field.Name] = execution.LineageFieldMapping{SourceField: field.Name, Transformation: "direct"}
	}
	for _, plan := range plans {
		switch strings.TrimSpace(plan.Type) {
		case "", "field_mapping":
			if plan.FieldMapping == nil {
				return nil
			}
			next := map[string]execution.LineageFieldMapping{}
			if plan.FieldMapping.Mode == FieldMappingModePassthrough {
				for name, origin := range origins {
					next[name] = origin
				}
			}
			for _, field := range plan.FieldMapping.Fields {
				origin, exists := origins[strings.TrimSpace(field.Source)]
				if !exists || strings.TrimSpace(field.Source) == "" {
					origin = execution.LineageFieldMapping{Transformation: "generated"}
				}
				if field.Default != nil && origin.SourceField != "" {
					origin.Transformation = "derived"
				}
				next[strings.TrimSpace(field.Target)] = origin
			}
			origins = next

		default:
			return nil
		}
	}
	result := &TableFieldLineage{Source: source, Target: target}
	for _, field := range written.Fields {
		origin, exists := origins[field.Name]
		if !exists || !target.HasField(field.Name) {
			return nil
		}
		origin.OutputPort = "target"
		origin.TargetField = field.Name
		if origin.SourceField != "" {
			origin.InputPort = "source"
			for _, sourceField := range sourceFields {
				if sourceField.Name != origin.SourceField {
					continue
				}
				for _, targetField := range targetFields {
					if targetField.Name == field.Name && (sourceField.Type != targetField.Type || sourceField.ElementType != targetField.ElementType || !sameDecimalShape(sourceField, targetField)) {
						origin.Transformation = "derived"
					}
				}
			}
		}
		result.Mappings = append(result.Mappings, origin)
	}
	sort.Slice(result.Mappings, func(i, j int) bool { return result.Mappings[i].TargetField < result.Mappings[j].TargetField })
	return result
}

func sameDecimalShape(source, target datatype.FieldInfo) bool {
	return source.Precision == target.Precision && source.Scale == target.Scale
}
