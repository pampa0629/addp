package format

import (
	"testing"

	"github.com/addp/common/datatype"
)

type testTypeMapper struct{}

func (m testTypeMapper) Name() string { return "test" }

func (m testTypeMapper) ToCommon(nativeType string) datatype.FieldType {
	if nativeType == "native_text" {
		return datatype.FieldTypeString
	}
	return datatype.FieldTypeUnknown
}

func (m testTypeMapper) FromCommon(commonType datatype.FieldType) (string, int, int) {
	return "", 0, 0
}

func TestTypeMapperRegistryInferCommonFieldType(t *testing.T) {
	registry := &TypeMapperRegistry{mappers: map[string]TypeMapper{}}
	registry.Register(testTypeMapper{})

	if got := registry.InferCommonFieldType("native_text"); got != datatype.FieldTypeString {
		t.Fatalf("InferCommonFieldType() = %q, want %q", got, datatype.FieldTypeString)
	}
	if got := registry.InferCommonFieldType("unknown"); got != datatype.FieldTypeUnknown {
		t.Fatalf("InferCommonFieldType() = %q, want %q", got, datatype.FieldTypeUnknown)
	}
}
