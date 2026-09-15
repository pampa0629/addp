package service

import (
	q "github.com/addp/common/query"
	"testing"
)

func TestMetricPlanOptionsMatchExecutionInput(t *testing.T) {
	for _, operation := range []string{"count_distinct", "directional_overlap"} {
		parameters, _, _ := metricPlanSignature(operation)
		for _, parameter := range parameters {
			if err := q.ValidateParameterOptions(parameter.Options, func(any) error { return nil }); err != nil {
				t.Fatal(err)
			}
			if parameter.Name == "grain" || parameter.Name == "directions" {
				if len(parameter.Options) != 2 {
					t.Fatalf("missing enum for %s", parameter.Name)
				}
				for _, option := range parameter.Options {
					if option.Labels["zh-cn"] == option.Labels["en"] {
						t.Fatal("unresolved translations")
					}
				}
			}
		}
	}
}
