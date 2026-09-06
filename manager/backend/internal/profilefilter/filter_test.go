package profilefilter

import (
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	commonquery "github.com/addp/common/query"
	"github.com/addp/manager/internal/dataprofile"
)

func TestNormalizeAndCompileConditionScope(t *testing.T) {
	scope, err := Normalize(dataprofile.DataScope{
		Kind:  dataprofile.DataScopeKindCondition,
		Logic: dataprofile.DataScopeLogicAnd,
		Conditions: []dataprofile.DataScopeCondition{
			{Field: "name", Operator: "contains", Value: "A_%"},
			{Field: "amount", Operator: "between", Values: []interface{}{10.0, 20.0}},
		},
	}, []datatype.FieldInfo{
		{Name: "name", Type: datatype.FieldTypeString},
		{Name: "amount", Type: datatype.FieldTypeDouble},
	})
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	whereClause, args, err := SQL(scope, commonquery.ForDialect(commonquery.DialectPostgreSQL), "")
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(whereClause, `"amount" BETWEEN $1 AND $2`) || !strings.Contains(whereClause, `"name" LIKE $3 ESCAPE '!'`) {
		t.Fatalf("where clause = %q", whereClause)
	}
	if !reflect.DeepEqual(args, []interface{}{10.0, 20.0, "%A!_!%%"}) {
		t.Fatalf("args = %#v", args)
	}
}

func TestNormalizeRejectsOperatorForCanonicalType(t *testing.T) {
	_, err := Normalize(dataprofile.DataScope{
		Kind:  dataprofile.DataScopeKindCondition,
		Logic: dataprofile.DataScopeLogicAnd,
		Conditions: []dataprofile.DataScopeCondition{
			{Field: "created_at", Operator: "contains", Value: "2026"},
		},
	}, []datatype.FieldInfo{{Name: "created_at", Type: datatype.FieldTypeTimestamp}})
	if err == nil {
		t.Fatal("Normalize() succeeded for a text operator on a timestamp field")
	}
}

func TestNormalizeProducesStableConditionOrder(t *testing.T) {
	fields := []datatype.FieldInfo{
		{Name: "status", Type: datatype.FieldTypeString},
		{Name: "amount", Type: datatype.FieldTypeInt},
	}
	left, err := Normalize(dataprofile.DataScope{
		Kind: dataprofile.DataScopeKindCondition, Logic: dataprofile.DataScopeLogicAnd,
		Conditions: []dataprofile.DataScopeCondition{
			{Field: "status", Operator: "eq", Value: "active"},
			{Field: "amount", Operator: "gte", Value: 10.0},
		},
	}, fields)
	if err != nil {
		t.Fatal(err)
	}
	right, err := Normalize(dataprofile.DataScope{
		Kind: dataprofile.DataScopeKindCondition, Logic: dataprofile.DataScopeLogicAnd,
		Conditions: []dataprofile.DataScopeCondition{
			{Field: "amount", Operator: "gte", Value: 10.0},
			{Field: "status", Operator: "eq", Value: "active"},
		},
	}, fields)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(left, right) {
		t.Fatalf("normalized scopes differ: %#v != %#v", left, right)
	}
}
