package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/engine/plugins/builtin/general"
	commonmodels "github.com/addp/common/models"
)

type fieldRecommendationEngineGetterStub struct {
	tenantIDs []uint
	engineIDs []uint
	engines   map[uint]*commonmodels.Engine
	errors    map[uint]error
}

func availableFieldRecommendationEngine(t *testing.T, id uint, engineType string) *commonmodels.Engine {
	t.Helper()
	plug, err := plugin.Get(engineType)
	if err != nil {
		t.Fatal(err)
	}
	capabilitiesJSON, err := plugin.MarshalEngineCapabilities(plug.Capabilities())
	if err != nil {
		t.Fatal(err)
	}
	capabilities := commonmodels.JSONString(capabilitiesJSON)
	return &commonmodels.Engine{
		ID:               id,
		EngineType:       engineType,
		LifecycleState:   commonmodels.EngineLifecycleActive,
		ConnectionStatus: commonmodels.EngineConnectionOnline,
		Capabilities:     &capabilities,
	}
}

func TestFieldRecommendationTargetUsesDeclaredDecimalLimits(t *testing.T) {
	for _, engineType := range []string{"mysql", "oceanbase"} {
		t.Run(engineType, func(t *testing.T) {
			plug, err := plugin.Get(engineType)
			if err != nil {
				t.Fatal(err)
			}
			limits := plug.Capabilities().Limits.TableWrite.Decimal
			if !fitsDecimalFieldLimits(65, 30, limits) {
				t.Fatalf("%s limits should accept decimal(65,30)", engineType)
			}
			if fitsDecimalFieldLimits(66, 30, limits) || fitsDecimalFieldLimits(65, 31, limits) {
				t.Fatalf("%s limits should reject values beyond decimal(65,30)", engineType)
			}
		})
	}
}

func TestFieldRecommendationRejectsTargetWithoutDecimalLimits(t *testing.T) {
	getter := &fieldRecommendationEngineGetterStub{engines: map[uint]*commonmodels.Engine{
		9: availableFieldRecommendationEngine(t, 9, "postgresql"),
	}}
	service := NewFieldDefinitionRecommendationService(getter, allowFieldRecommendationProtectionGate{})
	_, err := service.Recommend(context.Background(), 7, FieldDefinitionRecommendationRequest{
		SourceLocator:  "addp://engine/8/path/public/amounts?type=table&item_id=60",
		SourceFields:   []string{"amount"},
		TargetEngineID: 9,
	})
	if !errors.Is(err, ErrFieldRecommendationUnsupported) || !reflect.DeepEqual(getter.engineIDs, []uint{9}) {
		t.Fatalf("Recommend() error = %v, engine lookups = %v", err, getter.engineIDs)
	}
}

type allowFieldRecommendationProtectionGate struct{}

func (allowFieldRecommendationProtectionGate) RequireLocator(context.Context, uint, string) error {
	return nil
}

func (s *fieldRecommendationEngineGetterStub) GetEngineForTenant(_ context.Context, tenantID, engineID uint) (*commonmodels.Engine, error) {
	s.tenantIDs = append(s.tenantIDs, tenantID)
	s.engineIDs = append(s.engineIDs, engineID)
	if err := s.errors[engineID]; err != nil {
		return nil, err
	}
	return s.engines[engineID], nil
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
	getter := &fieldRecommendationEngineGetterStub{
		engines: map[uint]*commonmodels.Engine{
			9: availableFieldRecommendationEngine(t, 9, "oceanbase"),
		},
		errors: map[uint]error{8: errors.New("stop after tenant binding")},
	}
	service := NewFieldDefinitionRecommendationService(getter, allowFieldRecommendationProtectionGate{})
	_, err := service.Recommend(context.Background(), 7, FieldDefinitionRecommendationRequest{
		SourceLocator:  "addp://engine/8/path/public/amounts?type=table&item_id=60",
		SourceFields:   []string{"amount"},
		TargetEngineID: 9,
	})
	if err == nil || !reflect.DeepEqual(getter.tenantIDs, []uint{7, 7}) || !reflect.DeepEqual(getter.engineIDs, []uint{9, 8}) {
		t.Fatalf("tenant/engine bindings = (%v,%v), err=%v", getter.tenantIDs, getter.engineIDs, err)
	}
}
