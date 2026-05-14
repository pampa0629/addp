package format

import "testing"

type testTypeMapper struct{}

func (m testTypeMapper) Name() string { return "test" }

func (m testTypeMapper) ToCommon(nativeType string) FieldType {
	if nativeType == "native_text" {
		return FieldTypeString
	}
	return FieldTypeUnknown
}

func (m testTypeMapper) FromCommon(commonType FieldType) (string, int, int) {
	return "", 0, 0
}

func TestTypeMapperRegistryInferCommonFieldType(t *testing.T) {
	registry := &TypeMapperRegistry{mappers: map[string]TypeMapper{}}
	registry.Register(testTypeMapper{})

	if got := registry.InferCommonFieldType("native_text"); got != FieldTypeString {
		t.Fatalf("InferCommonFieldType() = %q, want %q", got, FieldTypeString)
	}
	if got := registry.InferCommonFieldType("unknown"); got != FieldTypeUnknown {
		t.Fatalf("InferCommonFieldType() = %q, want %q", got, FieldTypeUnknown)
	}
}
