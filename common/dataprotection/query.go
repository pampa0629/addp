package dataprotection

import (
	"errors"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
)

// PreparedTableProtection binds transformation metadata to the same prepared rules as Apply.
// DerivedFields contains top-level value components and no policy or protected values.
type PreparedTableProtection struct {
	Apply         func(*plugin.QueryResult) error
	DerivedFields []string
}

// ProtectQueryResultSource applies already schema-validated rules to every
// value output from one QueryOutputSource. It mutates the result in place and
// never includes protected values in errors.
func ProtectQueryResultSource(result *plugin.QueryResult, source plugin.QueryOutputSource, action string, rules []Rule, subject SubjectReference) error {
	if result == nil {
		return errors.New("query protection result is required")
	}
	mapped, err := queryOutputRules(source, action, rules)
	if err != nil {
		return err
	}
	for _, mappedRule := range mapped {
		mappedRule.Decision = mappedRule.EffectiveDecision(subject, time.Now().UTC())
		mappedRule.Authorizations = nil
		for _, row := range result.Rows {
			if err := ProtectDocument(row, action, []Rule{mappedRule}, subject); err != nil {
				return err
			}
		}
		if mappedRule.Decision.Effect == EffectSuppress && len(mappedRule.Component.Path) == 1 {
			result.Columns = removeQueryColumn(result.Columns, mappedRule.Component.Path[0].Name)
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

// QueryOutputDerivedFields uses the same exact path mapping as result protection.
func QueryOutputDerivedFields(source plugin.QueryOutputSource, action string, rules []Rule, subject SubjectReference, now time.Time) []string {
	mapped, err := queryOutputRules(source, action, rules)
	if err != nil {
		// Apply rejects this output before any value can be written. No successful
		// value transformation can be described, and the execution gate stays in Apply.
		return nil
	}
	fields := []string{}
	seen := map[string]bool{}
	for _, rule := range mapped {
		if len(rule.Component.Path) == 0 || rule.EffectiveDecision(subject, now).Effect == EffectAllow {
			continue
		}
		name := rule.Component.Path[0].Name
		if !seen[name] {
			fields = append(fields, name)
			seen[name] = true
		}
	}
	return fields
}

func queryOutputRules(source plugin.QueryOutputSource, action string, rules []Rule) ([]Rule, error) {
	if source.OpaqueOutput {
		return nil, ErrDenied
	}
	result := []Rule{}
	for _, rule := range rules {
		if rule.Action != action {
			continue
		}
		mapped := []Rule{}
		if source.IdentityOutput {
			mapped = append(mapped, rule)
		}
		for _, binding := range source.Bindings {
			if !sameQueryPath(binding.SourcePath, queryComponentNames(rule.Component.Path)) {
				continue
			}
			if binding.Transformation != plugin.QueryOutputTransformationDirect {
				return nil, ErrDenied
			}
			mappedRule := rule
			mappedRule.Component.Path = queryOutputPath(binding.OutputPath)
			mappedRule.Component.Key = strings.Join(binding.OutputPath, ".")
			mapped = append(mapped, mappedRule)
		}
		seen := map[string]bool{}
		for _, mappedRule := range mapped {
			key := strings.Join(queryComponentNames(mappedRule.Component.Path), "\x00")
			if !seen[key] {
				result = append(result, mappedRule)
				seen[key] = true
			}
		}
	}
	return result, nil
}
