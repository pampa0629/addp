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
	Sources  map[string]*execution.LineageSchemaSnapshot
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
		if query.lineage == nil {
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
		return buildQueryTableFieldLineage(query.lineage, plan.Source.QueryInputs, target.Table.Fields, read, written, plan.Transforms, derived)
	}
	source, err := e.SourceCatalogFacts.DescribeEngineCatalogFacts(ctx, plan.Source.ConnInfo, plan.Source.Path, plugin.EngineCatalogFactsOptions{})
	if err != nil || source == nil || source.Table == nil {
		return nil
	}
	return buildTableFieldLineage(source.Table.Fields, target.Table.Fields, read, written, plan.Transforms, derived...)
}

func buildQueryTableFieldLineage(lineage *plugin.QueryOutputLineage, inputs []TableQueryInput, targetFields []datatype.FieldInfo, read, written *datatype.TableInfo, plans []TableTransformPlan, derived []string) *TableFieldLineage {
	if lineage == nil || len(inputs) == 0 || len(lineage.Sources) != len(inputs) || read == nil {
		return nil
	}
	paths := make([]plugin.EngineCatalogPath, 0, len(inputs))
	for _, input := range inputs {
		paths = append(paths, input.Path)
	}
	readSet, err := plugin.NewQueryReadSet(paths...)
	if err != nil || len(readSet.Paths) != len(inputs) || plugin.ValidateQueryOutputLineage(readSet, lineage) != nil {
		return nil
	}
	sources := map[string]*execution.LineageSchemaSnapshot{}
	ports := make([]string, len(lineage.Sources))
	for index, source := range lineage.Sources {
		if source.OpaqueOutput {
			return nil
		}
		for _, input := range inputs {
			if sameQueryReadPaths([]plugin.EngineCatalogPath{source.Path}, []plugin.EngineCatalogPath{input.Path}) {
				ports[index] = input.Port
			}
		}
		port := ports[index]
		if strings.TrimSpace(port) == "" || sources[port] != nil {
			return nil
		}
		snapshot, err := execution.NewLineageSchemaSnapshot(source.Fields)
		if err != nil {
			return nil
		}
		sources[port] = snapshot
	}
	origins := map[string][]execution.LineageFieldMapping{}
	for _, field := range read.Fields {
		if _, exists := origins[field.Name]; exists {
			return nil
		}
		var candidates []execution.LineageFieldMapping
		for index, source := range lineage.Sources {
			bindings := append([]plugin.QueryOutputBinding(nil), source.Bindings...)
			if source.IdentityOutput {
				bindings = append(bindings, plugin.QueryOutputBinding{SourcePath: []string{field.Name}, OutputPath: []string{field.Name}, Transformation: "direct"})
			}
			for _, binding := range bindings {
				if len(binding.OutputPath) != 1 || binding.OutputPath[0] != field.Name {
					continue
				}
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
				if name == "" || !sources[ports[index]].HasField(name) {
					return nil
				}
				transformation := binding.Transformation
				for _, protected := range derived {
					if protected == field.Name {
						transformation = "derived"
					}
				}
				// Repeated expressions can mention the same physical field more than
				// once. Keep one fact per origin, with derived taking precedence.
				duplicate := false
				for i := range candidates {
					if candidates[i].InputPort == ports[index] && candidates[i].SourceField == name {
						if transformation == "derived" {
							candidates[i].Transformation = transformation
						}
						duplicate = true
						break
					}
				}
				if !duplicate {
					candidates = append(candidates, execution.LineageFieldMapping{InputPort: ports[index], SourceField: name, Transformation: transformation})
				}
			}
		}
		// No exact value binding is not proof of a constant. Fail closed.
		if len(candidates) == 0 {
			return nil
		}
		origins[field.Name] = candidates
	}
	return composeTableFieldLineage(sources, targetFields, origins, written, plans)
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
	origins := map[string][]execution.LineageFieldMapping{}
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
		origins[field.Name] = []execution.LineageFieldMapping{{InputPort: "source", SourceField: field.Name, Transformation: transformation}}
	}
	return composeTableFieldLineage(map[string]*execution.LineageSchemaSnapshot{"source": source}, targetFields, origins, written, plans)
}

func composeTableFieldLineage(sources map[string]*execution.LineageSchemaSnapshot, targetFields []datatype.FieldInfo, origins map[string][]execution.LineageFieldMapping, written *datatype.TableInfo, plans []TableTransformPlan) *TableFieldLineage {
	target, err := execution.NewLineageSchemaSnapshot(targetFields)
	if err != nil || written == nil || len(written.Fields) == 0 {
		return nil
	}
	for _, mappings := range origins {
		for _, origin := range mappings {
			if !sources[origin.InputPort].HasField(origin.SourceField) {
				return nil
			}
		}
	}
	for _, plan := range plans {
		switch strings.TrimSpace(plan.Type) {
		case "", "field_mapping":
			if plan.FieldMapping == nil {
				return nil
			}
			next := map[string][]execution.LineageFieldMapping{}
			if plan.FieldMapping.Mode == FieldMappingModePassthrough {
				for name, mappings := range origins {
					next[name] = mappings
				}
			}
			for _, field := range plan.FieldMapping.Fields {
				mappings, exists := origins[strings.TrimSpace(field.Source)]
				mappings = append([]execution.LineageFieldMapping(nil), mappings...)
				if !exists || strings.TrimSpace(field.Source) == "" {
					mappings = []execution.LineageFieldMapping{{Transformation: "generated"}}
				}
				if field.Default != nil {
					for i := range mappings {
						if mappings[i].SourceField != "" {
							mappings[i].Transformation = "derived"
						}
					}
				}
				next[strings.TrimSpace(field.Target)] = mappings
			}
			origins = next
		default:
			return nil
		}
	}
	result := &TableFieldLineage{Sources: sources, Target: target}
	for _, field := range written.Fields {
		mappings, exists := origins[field.Name]
		if !exists || !target.HasField(field.Name) {
			return nil
		}
		for _, origin := range mappings {
			origin.OutputPort = "target"
			origin.TargetField = field.Name
			if origin.SourceField != "" {
				for _, sourceField := range sources[origin.InputPort].Fields {
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
	}
	sort.Slice(result.Mappings, func(i, j int) bool {
		left, right := result.Mappings[i], result.Mappings[j]
		if left.TargetField != right.TargetField {
			return left.TargetField < right.TargetField
		}
		if left.InputPort != right.InputPort {
			return left.InputPort < right.InputPort
		}
		return left.SourceField < right.SourceField
	})
	return result
}

func sameDecimalShape(source, target datatype.FieldInfo) bool {
	return source.Precision == target.Precision && source.Scale == target.Scale
}
