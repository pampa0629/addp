package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/addp/common/datatype"
	commonmodels "github.com/addp/common/models"
)

type fieldRecommendationEngineGetterStub struct {
	tenantID uint
	engineID uint
	err      error
}

type allowFieldRecommendationProtectionGate struct{}

func (allowFieldRecommendationProtectionGate) RequireLocator(context.Context, uint, string) error {
	return nil
}

func (s *fieldRecommendationEngineGetterStub) GetEngineForTenant(_ context.Context, tenantID, engineID uint) (*commonmodels.Engine, error) {
	s.tenantID = tenantID
	s.engineID = engineID
	return nil, s.err
}

func TestDecimalValueShapePreservesExactRequiredDigits(t *testing.T) {
	tests := []struct {
		value         interface{}
		integerDigits int
		scale         int
	}{
		{"43854577.120000", 8, 2},
		{"0.00120", 0, 4},
		{"-12.3400", 2, 2},
		{"1.2e3", 4, 0},
		{"1.2e-3", 0, 4},
		{"0", 1, 0},
	}
	for _, test := range tests {
		integerDigits, scale, err := decimalValueShape(test.value)
		if err != nil {
			t.Fatalf("decimalValueShape(%v): %v", test.value, err)
		}
		if integerDigits != test.integerDigits || scale != test.scale {
			t.Fatalf("decimalValueShape(%v) = (%d,%d), want (%d,%d)", test.value, integerDigits, scale, test.integerDigits, test.scale)
		}
	}
}

func TestDecimalRecommendationCombinesColumnWideBounds(t *testing.T) {
	accumulator := decimalRecommendationAccumulator{}
	for _, value := range []interface{}{"99999", "0.123456", nil} {
		if err := accumulator.Add(value); err != nil {
			t.Fatal(err)
		}
	}
	precision, scale := accumulator.Recommendation()
	if precision != 11 || scale != 6 || accumulator.NonNullCount != 2 {
		t.Fatalf("recommendation = decimal(%d,%d), non-null=%d", precision, scale, accumulator.NonNullCount)
	}
}

func TestRecommendationFieldsPreserveQuotedIdentifierCase(t *testing.T) {
	fields, err := normalizedRecommendationFields([]string{"Amount", "amount", "Amount"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Amount", "amount"}; !reflect.DeepEqual(fields, want) {
		t.Fatalf("normalized fields = %#v, want %#v", fields, want)
	}

	actual := []datatype.FieldInfo{
		{Name: "Amount", Type: datatype.FieldTypeDecimal},
		{Name: "amount", Type: datatype.FieldTypeString},
	}
	if err := validateDecimalRecommendationFields(actual, []string{"Amount"}); err != nil {
		t.Fatalf("exact decimal field should be accepted: %v", err)
	}
	if err := validateDecimalRecommendationFields(actual, []string{"amount"}); err == nil {
		t.Fatal("case-distinct non-decimal field should be rejected")
	}
}

func TestFieldRecommendationReadsSourceEngineInCurrentTenant(t *testing.T) {
	getter := &fieldRecommendationEngineGetterStub{err: errors.New("stop after tenant binding")}
	service := NewFieldDefinitionRecommendationService(getter, allowFieldRecommendationProtectionGate{})
	_, err := service.Recommend(context.Background(), 7, FieldDefinitionRecommendationRequest{
		SourceLocator:    "addp://engine/8/path/public/amounts?type=table&item_id=60",
		SourceFields:     []string{"amount"},
		TargetEngineType: "mysql",
	})
	if err == nil || getter.tenantID != 7 || getter.engineID != 8 {
		t.Fatalf("tenant/engine binding = (%d,%d), err=%v", getter.tenantID, getter.engineID, err)
	}
}
