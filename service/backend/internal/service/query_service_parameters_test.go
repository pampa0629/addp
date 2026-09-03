package service

import (
	"errors"
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
