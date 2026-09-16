package service

import (
	"context"
	"encoding/json"
	"github.com/addp/quality/internal/models"
	"testing"
)

func TestRuleDefinitionsAreTargetIndependent(t *testing.T) {
	svc := NewRuleService(nil, nil)
	for _, tc := range []struct {
		kind, params string
		valid        bool
	}{
		{"not_null", `{}`, true},
		{"allowed_values", `{"values":["signup","leader"]}`, true},
		{"foreign_key", `{}`, true},
		{"predicate_implication", `{"when":{"operator":"is_true"},"then":{"operator":"eq","value":true}}`, true},
		{"length", `{"constraint":{"min":1,"max":64}}`, true},
		{"row_count", `{"min":1}`, true},
		{"not_null", `{"table":"customers"}`, false},
		{"not_null", `{"column":"id"}`, false},
		{"length", `{"constraint":{"min":64,"max":1}}`, false},
		{"predicate_implication", `{"when":{"column":"a","operator":"is_true"},"then":{"operator":"is_true"}}`, false},
		{"row_count", `{"sql":"select 1"}`, false},
	} {
		t.Run(tc.kind+"/"+tc.params, func(t *testing.T) {
			request := RuleWriteRequest{Code: "test_rule", RuleContent: models.RuleContent{Name: "test", Type: tc.kind, Params: json.RawMessage(tc.params)}}
			err := svc.validate(context.Background(), 7, &request, nil)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%t err=%v", tc.valid, err)
			}
		})
	}
}
