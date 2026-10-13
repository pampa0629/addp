package writetest

import (
	"database/sql/driver"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

type jsonConstantHook struct{ calls *int }

func (v jsonConstantHook) MarshalJSON() ([]byte, error) {
	*v.calls++
	return []byte(`{"callback":true}`), nil
}

type textConstantHook struct{ calls *int }

func (v textConstantHook) MarshalText() ([]byte, error) {
	*v.calls++
	return []byte("callback"), nil
}

type driverConstantHook string

func (driverConstantHook) Value() (driver.Value, error) {
	panic("pure definition check must not invoke Valuer")
}

type pointerJSONConstantHook string

func (*pointerJSONConstantHook) MarshalJSON() ([]byte, error) {
	panic("pure check must not invoke addressable element callback")
}

type pointerTextConstantHook string

func (*pointerTextConstantHook) MarshalText() ([]byte, error) {
	panic("pure check must not invoke addressable text callback")
}

type pointerDriverConstantHook string

func (*pointerDriverConstantHook) Value() (driver.Value, error) {
	panic("pure check must not invoke pointer Valuer")
}

// PureDefinitionConstants verifies the no-callback boundary at actual Providers.
func PureDefinitionConstants(t *testing.T, checker plugin.TableWriteDefinitionChecker) {
	t.Helper()
	calls := 0
	cycle := map[string]interface{}{}
	cycle["self"] = cycle
	deep := interface{}(true)
	for i := 0; i < 66; i++ {
		deep = []interface{}{deep}
	}
	values := map[string]interface{}{
		"JSON callback":        jsonConstantHook{&calls},
		"nested JSON callback": map[string]interface{}{"child": []interface{}{jsonConstantHook{&calls}}},
		"text callback":        textConstantHook{&calls},
		"map key callback":     map[textConstantHook]interface{}{{&calls}: true},
		"driver callback":      driverConstantHook("driver"),
		"nested driver":        []interface{}{driverConstantHook("driver")},
		"pointer":              &jsonConstantHook{&calls},
		"addressable element":  []pointerJSONConstantHook{"callback"},
		"addressable text":     []pointerTextConstantHook{"callback"},
		"pointer driver":       []pointerDriverConstantHook{"callback"},
		"opaque struct":        struct{ Value bool }{true},
		"cycle":                cycle,
		"depth limit":          deep,
		"node limit":           make([]interface{}, 4097),
	}
	for name, value := range values {
		t.Run(name, func(t *testing.T) {
			calls = 0
			definition := plugin.TableWriteDefinition{
				Fields: []plugin.TableWriteFieldDefinition{{
					Target:   datatype.FieldInfo{Name: "payload", Type: datatype.FieldTypeJSON, Nullable: true},
					Source:   &datatype.FieldInfo{Type: datatype.FieldTypeJSON},
					Constant: &plugin.TableWriteConstant{Value: value},
				}, {Target: datatype.FieldInfo{Name: "other", Type: datatype.FieldTypeArray, ElementType: datatype.FieldTypeJSON}}},
				Existing: &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "payload", Type: datatype.FieldTypeString, NativeType: "text"}}},
			}
			report := checker.CheckTableWriteDefinition(definition)
			if calls != 0 {
				t.Fatalf("pure check invoked %d callbacks", calls)
			}
			foundConstant, foundSource, foundExisting, foundOther := false, false, false, false
			for _, check := range report.Checks {
				if check.Field == "other" {
					foundOther = check.Status == plugin.DefinitionUnsupported
					continue
				}
				switch check.Check {
				case "constant_encoding":
					foundConstant = check.Status == plugin.DefinitionUnknown && check.Code == "constant_encoding_not_checked"
				case "source_assignment":
					foundSource = check.Status == plugin.DefinitionSupported
				case "existing_target":
					foundExisting = check.Status == plugin.DefinitionUnsupported && check.Code == "existing_column_incompatible"
				}
			}
			if !foundConstant || !foundSource || !foundExisting || !foundOther || report.UnsupportedError() == nil {
				t.Fatalf("unknown constant suppressed other checks: %+v", report)
			}
		})
	}
}
