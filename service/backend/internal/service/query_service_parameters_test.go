package service

import (
	"encoding/json"
	"errors"
	commonquery "github.com/addp/common/query"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/service/internal/models"
)

func TestValidateQueryServiceNamedParametersRequiresExactSQLContract(t *testing.T) {
	t.Parallel()

	definitions := []models.QueryServiceNamedParameter{
		{Name: "person_id_a", Type: datatype.FieldTypeString, Required: true},
		{Name: "threshold", Type: datatype.FieldTypeDouble, Default: 0.5},
	}
	got, err := validateQueryServiceNamedParameters(
		"sql",
		"SELECT :person_id_a AS person_id, :threshold AS threshold",
		definitions,
	)
	if err != nil {
		t.Fatalf("validateQueryServiceNamedParameters() error = %v", err)
	}
	if !reflect.DeepEqual(got[1].Default, 0.5) {
		t.Fatalf("normalized default = %#v", got[1].Default)
	}

	for name, candidate := range map[string]struct {
		query       string
		definitions []models.QueryServiceNamedParameter
	}{
		"undeclared reference": {"SELECT :missing", definitions},
		"unused definition":    {"SELECT :person_id_a", definitions},
		"table mode":           {"", definitions[:1]},
	} {
		t.Run(name, func(t *testing.T) {
			configType := "sql"
			if name == "table mode" {
				configType = "table"
			}
			if _, err := validateQueryServiceNamedParameters(configType, candidate.query, candidate.definitions); err == nil {
				t.Fatal("validation error = nil")
			}
		})
	}
}

func TestBindQueryServiceNamedParametersUsesDriverArguments(t *testing.T) {
	t.Parallel()

	service := &models.QueryService{
		ConfigType: "sql",
		SqlQuery:   "SELECT * FROM outdoor.person_metrics WHERE person_id IN (:person_id_a, :person_id_b) AND score >= :threshold",
		NamedParameters: []models.QueryServiceNamedParameter{
			{Name: "person_id_a", Type: datatype.FieldTypeString, Required: true},
			{Name: "person_id_b", Type: datatype.FieldTypeString, Required: true},
			{Name: "threshold", Type: datatype.FieldTypeDouble, Default: 0.25},
		},
	}
	bound, args, resolved, err := bindQueryServiceNamedParameters(service, "postgresql", service.SqlQuery, map[string]interface{}{
		"person_id_a": "a' OR 1=1 --",
		"person_id_b": "b",
	})
	if err != nil {
		t.Fatalf("bindQueryServiceNamedParameters() error = %v", err)
	}
	if strings.Contains(bound, "OR 1=1") || bound != "SELECT * FROM outdoor.person_metrics WHERE person_id IN ($1, $2) AND score >= $3" {
		t.Fatalf("bound SQL = %s", bound)
	}
	if !reflect.DeepEqual(args, []interface{}{"a' OR 1=1 --", "b", 0.25}) {
		t.Fatalf("args = %#v", args)
	}
	if !reflect.DeepEqual(resolved, map[string]interface{}{"person_id_a": "a' OR 1=1 --", "person_id_b": "b", "threshold": 0.25}) {
		t.Fatalf("resolved = %#v", resolved)
	}

	_, _, _, err = bindQueryServiceNamedParameters(service, "postgresql", service.SqlQuery, map[string]interface{}{"person_id_a": "a"})
	if !errors.Is(err, ErrInvalidStructuredQuery) || !strings.Contains(err.Error(), "person_id_b") {
		t.Fatalf("missing parameter error = %v", err)
	}
	_, _, _, err = bindQueryServiceNamedParameters(service, "postgresql", service.SqlQuery, map[string]interface{}{
		"person_id_a": "a", "person_id_b": "b", "extra": true,
	})
	if !errors.Is(err, ErrInvalidStructuredQuery) || !strings.Contains(err.Error(), "extra") {
		t.Fatalf("extra parameter error = %v", err)
	}
}

func TestQueryServiceUpdatesOnlyOptionalDefaults(t *testing.T) {
	q := &models.QueryService{ConfigType: "sql", SqlQuery: "SELECT :minimum,:required,:label", NamedParameters: []models.QueryServiceNamedParameter{
		{Name: "minimum", Type: datatype.FieldTypeInt, Default: float64(1), Description: "unchanged", Options: []commonquery.ParameterOption{{Value: float64(1), Labels: map[string]string{"zh-cn": "一", "en": "One"}}, {Value: float64(2), Labels: map[string]string{"zh-cn": "二", "en": "Two"}}}},
		{Name: "required", Type: datatype.FieldTypeString, Required: true},
		{Name: "label", Type: datatype.FieldTypeString, Default: "kept"},
	}}
	before, _ := json.Marshal(q)
	updated, err := queryServiceUpdatedParameterDefaults(q, map[string]interface{}{"minimum": float64(2)})
	if err != nil || updated[0].Default != float64(2) || updated[2].Default != "kept" || updated[0].Description != q.NamedParameters[0].Description || !reflect.DeepEqual(updated[0].Options, q.NamedParameters[0].Options) {
		t.Fatalf("updated declarations: %+v %v", updated, err)
	}
	for name, values := range map[string]map[string]interface{}{"empty": {}, "required": {"required": "x"}, "unknown": {"missing": 2}, "null": {"minimum": nil}, "type": {"minimum": "2"}, "bool": {"minimum": true}, "outside options": {"minimum": 3}, "partial invalid": {"label": "new", "minimum": 3}} {
		t.Run(name, func(t *testing.T) {
			if _, err := queryServiceUpdatedParameterDefaults(q, values); !errors.Is(err, ErrInvalidNamedParameterDefaults) {
				t.Fatalf("invalid defaults accepted: %v", err)
			}
		})
	}
	after, _ := json.Marshal(q)
	if string(before) != string(after) {
		t.Fatal("default validation mutated original declarations")
	}
	for _, kind := range []string{"table", "analytical"} {
		copy := *q
		copy.ConfigType = kind
		if _, err := queryServiceUpdatedParameterDefaults(&copy, map[string]interface{}{"minimum": 2}); !errors.Is(err, ErrInvalidNamedParameterDefaults) {
			t.Fatalf("non-SQL defaults accepted: %s", kind)
		}
	}
	if onlyDeactivatesQueryService(&models.UpdateQueryServiceRequest{Status: func() *string { v := "inactive"; return &v }(), NamedParameterDefaults: map[string]interface{}{"minimum": 2}}) {
		t.Fatal("default edit bypassed role checks as a pure stop")
	}
}
