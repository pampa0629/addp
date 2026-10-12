package mongodb

import (
	"sort"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

var _ plugin.QueryDefinitionChecker = (*MongoDBPlugin)(nil)

// CheckQueryDefinition deliberately does not use the receiver: even a nil
// provider can check a saved definition without opening a runtime connection.
func (*MongoDBPlugin) CheckQueryDefinition(definition plugin.QueryDefinition) plugin.QueryDefinitionReport {
	report := plugin.QueryDefinitionReport{SchemaCoverage: plugin.QuerySchemaCoverageUnknown, Checks: []plugin.QueryDefinitionCheck{}}
	add := func(status plugin.DefinitionCheckStatus, code, field string) {
		report.Checks = append(report.Checks, plugin.QueryDefinitionCheck{Status: status, Code: code, Field: field})
	}
	request := definition.Request
	if !request.Options.ReadOnly || strings.ToLower(strings.TrimSpace(request.Language)) != "mql" {
		add(plugin.DefinitionUnknown, "query_definition_not_checked", "")
		return report
	}
	// Reuse the runtime's pure parameter binding and command parser. Supplying no
	// connection prevents any configured namespace from replacing the saved leaf.
	plan, err := prepareMongoQueryReadPlan(nil, request)
	if err != nil {
		add(plugin.DefinitionUnsupported, "query_definition_invalid", "")
		return report
	}
	path := plugin.EngineCatalogPathWithoutRoot(definition.SourcePath)
	if request.TargetPath == nil || request.EngineID == 0 || definition.SourcePath.EngineID != request.EngineID ||
		request.TargetPath.EngineID != request.EngineID || len(path.Segments) != 2 ||
		path.Segments[0].Term != "database" || path.Segments[1].Term != "collection" ||
		path.Segments[0].Name != plan.database || path.Segments[1].Name != plan.collection {
		add(plugin.DefinitionUnsupported, "query_snapshot_binding_mismatch", "")
		return report
	}
	selected := plugin.EngineCatalogPathWithoutRoot(*request.TargetPath)
	if len(selected.Segments) != 2 || selected.Segments[0] != path.Segments[0] || selected.Segments[1] != path.Segments[1] {
		add(plugin.DefinitionUnsupported, "query_snapshot_binding_mismatch", "")
		return report
	}
	pipeline, ok := mongoAggregatePipeline(plan.document["pipeline"])
	if plan.command != "aggregate" || !ok || len(pipeline) != 1 {
		add(plugin.DefinitionUnknown, "query_definition_not_checked", "")
		return report
	}
	stage, ok := mongoStageDocument(pipeline[0])
	if !ok || len(stage) != 1 {
		add(plugin.DefinitionUnknown, "query_definition_not_checked", "")
		return report
	}
	project, ok := mongoStageDocument(stage["$project"])
	if !ok || len(project) == 0 {
		add(plugin.DefinitionUnknown, "query_definition_not_checked", "")
		return report
	}
	// More complex expressions are intentionally outside this snapshot contract,
	// including $ifNull supported by runtime lineage. No second MQL parser.
	keys := sortedMongoDefinitionKeys(project)
	for i, name := range keys {
		expression := project[name]
		if len(splitMongoFieldPath(name)) == 0 {
			add(plugin.DefinitionUnsupported, "query_definition_invalid", name)
			return report
		}
		for _, other := range keys[:i] {
			if mongoPathHasPrefix(splitMongoFieldPath(name), splitMongoFieldPath(other)) || mongoPathHasPrefix(splitMongoFieldPath(other), splitMongoFieldPath(name)) {
				add(plugin.DefinitionUnsupported, "query_definition_invalid", name)
				return report
			}
		}
		if mongoProjectionIncludes(expression) || mongoProjectionExcludes(expression) {
			continue
		}
		if reference, ok := expression.(string); !ok || len(mongoFieldReferencePath(reference)) == 0 {
			add(plugin.DefinitionUnknown, "query_definition_not_checked", name)
			return report
		}
	}
	fields := []datatype.FieldInfo{}
	if definition.Source != nil {
		fields = definition.Source.Clone().Fields
	}
	// A placeholder means unobserved, never absent or a known null. It allows
	// the shared projection analyser to keep aliases without inventing type facts.
	ensureReference := func(sourcePath []string) {
		for _, field := range fields {
			path := field.Path
			if len(path) == 0 {
				path = splitMongoFieldPath(field.Name)
			}
			if mongoPathHasPrefix(path, sourcePath) {
				return
			}
		}
		name := strings.Join(sourcePath, ".")
		fields = append(fields, datatype.FieldInfo{Name: name, Path: append([]string(nil), sourcePath...), Type: datatype.FieldTypeUnknown, Nullable: true})
		add(plugin.DefinitionUnknown, "query_source_field_not_observed", name)
	}
	for _, name := range sortedMongoDefinitionKeys(project) {
		expression := project[name]
		if mongoProjectionExcludes(expression) {
			continue
		}
		sourcePath, _ := mongoTransparentProjectionSource(name, expression)
		ensureReference(sourcePath)
	}
	if _, explicitID := project["_id"]; !explicitID {
		ensureReference([]string{"_id"})
	}
	bindings, err := mongoAggregateOutputBindings(plan.document["pipeline"], fields)
	if err != nil {
		add(plugin.DefinitionUnsupported, "query_definition_invalid", "")
		return report
	}
	output := &datatype.TableInfo{}
	// Query read sessions return top-level record keys. Nested projections retain
	// an object at their top-level key; they do not silently flatten into columns.
	for _, binding := range bindings {
		name := binding.OutputPath[0]
		field := datatype.FieldInfo{Name: name, Path: []string{name}, Type: datatype.FieldTypeJSON, Nullable: true}
		if len(binding.OutputPath) == 1 {
			for _, source := range fields {
				path := source.Path
				if len(path) == 0 {
					path = splitMongoFieldPath(source.Name)
				}
				if mongoOutputPathKey(path) == mongoOutputPathKey(binding.SourcePath) {
					field = source
					field.Name, field.Path = name, []string{name}
					field.PrimaryKey, field.Generated = false, false
					field.DefaultExpression, field.GenerationExpression = "", ""
					break
				}
			}
		}
		if existing := output.GetField(name); existing != nil {
			if len(binding.OutputPath) == 1 {
				*existing = field
			}
		} else {
			output.Fields = append(output.Fields, field)
		}
	}
	report.Output = output
	if definition.Source != nil && definition.SchemaCoverage == plugin.QuerySchemaCoverageSampled {
		report.SchemaCoverage = plugin.QuerySchemaCoverageSampled
	}
	add(plugin.DefinitionSupported, "query_projection_definition_checked", "")
	add(plugin.DefinitionUnknown, "query_output_schema_not_complete", "")
	return report
}

func sortedMongoDefinitionKeys(document map[string]interface{}) []string {
	keys := make([]string, 0, len(document))
	for key := range document {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
