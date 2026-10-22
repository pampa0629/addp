package service

import (
	"encoding/json"
	"reflect"

	"github.com/addp/common/execution"
	"github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

// A matching digest only authenticates the task definition, not the separate
// execution plan. Check their effective fields before projecting source facts.
// This is not an approval and performs no source I/O or authorization writes.
func scanExecutionMatchesTask(definition json.RawMessage, config map[string]any, triggerType string) bool {
	task, err := jsonmap.DecodeObjectStrict(definition, execution.MaxExecutionConfigBytes, 64)
	if err != nil || !reflect.DeepEqual(task["engine_id"], config["engine_id"]) {
		return false
	}
	scope, scopeOK := task["scope"].(map[string]any)
	params, paramsOK := task["parameters"].(map[string]any)
	if !scopeOK || !paramsOK {
		return false
	}
	// Defaults must match the task producer, but malformed explicit values
	// cannot fall through its permissive JSONMap conversion during authorization.
	for _, option := range []struct {
		key      string
		fallback any
	}{
		{"scan_depth", scanflow.ScanDepthDeep}, {"force", false}, {"encoding", ""},
	} {
		want, exists := params[option.key]
		if !exists {
			want = option.fallback
		}
		got, exists := config[option.key]
		if !exists {
			got = option.fallback
		}
		if reflect.TypeOf(want) != reflect.TypeOf(option.fallback) || !reflect.DeepEqual(want, got) {
			return false
		}
	}
	header, hasHeader := params["has_header"]
	actualHeader, actualHasHeader := config["has_header"]
	if hasHeader != actualHasHeader || !reflect.DeepEqual(header, actualHeader) {
		return false
	}
	if hasHeader {
		if _, ok := header.(bool); !ok {
			return false
		}
	}
	// Task producers do not emit these ad-hoc/private selectors. Even a zero
	// or empty injection is contradictory provenance, not a fallback to whole.
	for _, key := range []string{"item_id", "targets", "initial_production_target"} {
		if _, exists := config[key]; exists {
			return false
		}
	}
	kind, exists := scope["type"]
	if !exists {
		kind = "engine"
	}
	paths, pathsOK := scanPlanPathSet(scope["catalog_paths"])
	actualPaths, actualPathsOK := scanPlanPathSet(config["catalog_paths"])
	if !pathsOK || !actualPathsOK {
		return false
	}
	refs, refsOK := scanPlanArray(scope["ref_groups"])
	actualRefs, actualRefsOK := scanPlanArray(config["ref_groups"])
	if !refsOK || !actualRefsOK {
		return false
	}
	switch kind {
	case "engine":
		return len(paths) == 0 && len(actualPaths) == 0 && len(refs) == 0 && len(actualRefs) == 0
	case "catalog_path":
		if len(paths) == 0 || len(actualPaths) == 0 || len(refs) != 0 || len(actualRefs) != 0 {
			return false
		}
		// The existing scheduler may subtract independently scheduled paths.
		// Permit only exact declared members, never prefix/ancestor inference.
		if triggerType != models.TriggerTypeScheduled && len(paths) != len(actualPaths) {
			return false
		}
		for path := range actualPaths {
			if !paths[path] {
				return false
			}
		}
		return true
	case "ref_group":
		return len(paths) == 0 && len(actualPaths) == 0 && len(refs) > 0 && reflect.DeepEqual(refs, actualRefs)
	default:
		return false
	}
}

func scanPlanArray(value any) ([]any, bool) {
	if value == nil {
		return []any{}, true
	}
	values, ok := value.([]any)
	return values, ok
}

func scanPlanPathSet(value any) (map[string]bool, bool) {
	values, ok := scanPlanArray(value)
	if !ok {
		return nil, false
	}
	result := make(map[string]bool, len(values))
	for _, value := range values {
		path, ok := value.(string)
		if !ok || path == "" || result[path] {
			return nil, false
		}
		result[path] = true
	}
	return result, true
}
