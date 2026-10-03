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

// Observe the provider-owned schemas after target preparation and before writing.
func (e *TableTransferExecutor) observeFieldLineage(ctx context.Context, plan TableTransferPlan, reader TableBatchSource, read, written *datatype.TableInfo, derived []string) *TableFieldLineage {
	if plan.Target.Kind != TableEndpointNative || e.TargetCatalogFacts == nil {
		return nil
	}
	query, isQuery := reader.(*queryTableBatchSource)
	if isQuery {
		if query.lineage == nil || len(query.lineage.Sources) != 1 || query.lineage.Sources[0].OpaqueOutput {
			return nil
		}
	} else if plan.Source.Kind != TableEndpointNative || strings.TrimSpace(plan.Source.Query) != "" || e.SourceCatalogFacts == nil {
		return nil
	}
	target, err := e.TargetCatalogFacts.DescribeEngineCatalogFacts(ctx, plan.Target.ConnInfo, plan.Target.Path, plugin.EngineCatalogFactsOptions{})
	if err != nil || target == nil || target.Table == nil {
		return nil
	}
	if isQuery {
		return buildQueryTableFieldLineage(query.lineage, target.Table.Fields, read, written, plan.Transforms, derived)
	}
	source, err := e.SourceCatalogFacts.DescribeEngineCatalogFacts(ctx, plan.Source.ConnInfo, plan.Source.Path, plugin.EngineCatalogFactsOptions{})
	if err != nil || source == nil || source.Table == nil {
		return nil
	}
	return buildTableFieldLineage(source.Table.Fields, target.Table.Fields, read, written, plan.Transforms, derived...)
}

func buildQueryTableFieldLineage(lineage *plugin.QueryOutputLineage, targetFields []datatype.FieldInfo, read, written *datatype.TableInfo, plans []TableTransformPlan, derived []string) *TableFieldLineage {
	if lineage == nil || len(lineage.Sources) != 1 || read == nil {
		return nil
	}
	source := lineage.Sources[0]
	if source.OpaqueOutput {
		return nil
	}
	origins := map[string]execution.LineageFieldMapping{}
	for _, field := range read.Fields {
		var candidates []plugin.QueryOutputBinding
		if source.IdentityOutput {
			candidates = append(candidates, plugin.QueryOutputBinding{SourcePath: []string{field.Name}, Transformation: "direct"})
		}
		for _, binding := range source.Bindings {
			if len(binding.OutputPath) == 1 && binding.OutputPath[0] == field.Name {
				candidates = append(candidates, binding)
			}
		}
		if len(candidates) != 1 {
			return nil
		}
		binding := candidates[0]
		name := ""
		for _, original := range source.Fields {
			path := original.Path
			if len(path) == 0 {
				path = []string{original.Name}
			}
			if sameFieldPath(path, binding.SourcePath) {
				if name != "" {
					return nil
				}
				name = original.Name
			}
		}
		if name == "" {
			return nil
		}
		transformation := binding.Transformation
		for _, protected := range derived {
			if protected == field.Name {
				transformation = "derived"
			}
		}
		if _, exists := origins[field.Name]; exists {
			return nil
		}
		origins[field.Name] = execution.LineageFieldMapping{SourceField: name, Transformation: transformation}
	}
	return composeTableFieldLineage(source.Fields, targetFields, origins, written, plans)
}

func sameFieldPath(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func buildTableFieldLineage(sourceFields, targetFields []datatype.FieldInfo, read, written *datatype.TableInfo, plans []TableTransformPlan, derived ...string) *TableFieldLineage {
	if read == nil {
		return nil
	}
	source, err := execution.NewLineageSchemaSnapshot(sourceFields)
	if err != nil {
		return nil
	}
	origins := map[string]execution.LineageFieldMapping{}
	for _, field := range read.Fields {
		if !source.HasField(field.Name) {
			return nil
		}
		transformation := "direct"
		for _, protected := range derived {
			if protected == field.Name {
				transformation = "derived"
			}
		}
		origins[field.Name] = execution.LineageFieldMapping{SourceField: field.Name, Transformation: transformation}
	}
	return composeTableFieldLineage(sourceFields, targetFields, origins, written, plans)
}

func composeTableFieldLineage(sourceFields, targetFields []datatype.FieldInfo, origins map[string]execution.LineageFieldMapping, written *datatype.TableInfo, plans []TableTransformPlan) *TableFieldLineage {
	source, err := execution.NewLineageSchemaSnapshot(sourceFields)
	if err != nil {
		return nil
	}
	target, err := execution.NewLineageSchemaSnapshot(targetFields)
	if err != nil || written == nil || len(written.Fields) == 0 {
		return nil
	}
	for _, origin := range origins {
		if !source.HasField(origin.SourceField) {
			return nil
		}
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
