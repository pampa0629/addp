package dataprotection

import (
	"errors"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
)

// ProtectQueryResultSource applies already schema-validated rules to every
// value output from one QueryOutputSource. It mutates the result in place and
// never includes protected values in errors.
func ProtectQueryResultSource(result *plugin.QueryResult, source plugin.QueryOutputSource, action string, rules []Rule, subject SubjectReference) error {
	if result == nil {
		return errors.New("query protection result is required")
	}
	if source.OpaqueOutput {
		return ErrDenied
	}
	for _, rule := range rules {
		if rule.Action != action {
			continue
		}
		mapped := make([]Rule, 0, len(source.Bindings)+1)
		if source.IdentityOutput {
			mapped = append(mapped, rule)
		}
		componentPath := queryComponentNames(rule.Component.Path)
		for _, binding := range source.Bindings {
			if !sameQueryPath(binding.SourcePath, componentPath) {
				continue
			}
			if binding.Transformation != plugin.QueryOutputTransformationDirect {
				return ErrDenied
			}
			mappedRule := rule
			mappedRule.Component.Path = queryOutputPath(binding.OutputPath)
			mappedRule.Component.Key = strings.Join(binding.OutputPath, ".")
			mapped = append(mapped, mappedRule)
		}
		seen := map[string]struct{}{}
		for _, mappedRule := range mapped {
			mappedRule.Decision = mappedRule.EffectiveDecision(subject, time.Now().UTC())
			mappedRule.Authorizations = nil
			key := strings.Join(queryComponentNames(mappedRule.Component.Path), "\x00")
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			for _, row := range result.Rows {
				if err := ProtectDocument(row, action, []Rule{mappedRule}, subject); err != nil {
					return err
				}
			}
			if mappedRule.Decision.Effect == EffectSuppress && len(mappedRule.Component.Path) == 1 {
				result.Columns = removeQueryColumn(result.Columns, mappedRule.Component.Path[0].Name)
			}
		}
	}
	return nil
}

func queryComponentNames(path []PathSegment) []string {
	result := make([]string, len(path))
	for index, segment := range path {
		result[index] = segment.Name
	}
	return result
}

func queryOutputPath(path []string) []PathSegment {
	result := make([]PathSegment, len(path))
	for index, name := range path {
		container := "object"
		if index == len(path)-1 {
			container = "scalar"
		}
		result[index] = PathSegment{Name: name, Container: container}
	}
	return result
}

func sameQueryPath(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func removeQueryColumn(columns []string, target string) []string {
	result := columns[:0]
	for _, column := range columns {
		if column != target {
			result = append(result, column)
		}
	}
	return result
}
