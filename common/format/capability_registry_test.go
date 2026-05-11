package format

import (
	"reflect"
	"testing"
)

func TestListFormatCapabilitiesDelegatesToCapabilityRegistry(t *testing.T) {
	capabilities := ListFormatCapabilities()
	if len(capabilities) == 0 {
		t.Fatal("expected built-in format capabilities")
	}

	for i := 1; i < len(capabilities); i++ {
		if capabilities[i-1].Format > capabilities[i].Format {
			t.Fatalf("format capabilities are not sorted: %s before %s", capabilities[i-1].Format, capabilities[i].Format)
		}
	}
}

func TestListTransferFormatsForEngineFamilyDelegate(t *testing.T) {
	got := ListTransferFormatsForEngineFamily(EngineFamilyObject)
	want := []string{"csv", "json", "markdown", "parquet", "shapefile", "text"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListTransferFormatsForEngineFamily(%q) = %#v, want %#v", EngineFamilyObject, got, want)
	}
}
