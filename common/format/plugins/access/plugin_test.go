package access

import (
	"testing"

	"github.com/addp/common/format"
)

func TestDescriptorOwnsMDBWeakIdentification(t *testing.T) {
	plugin := NewPlugin()
	descriptor := plugin.Descriptor()
	if descriptor.Format != format.FormatAccess || len(descriptor.Identification.Extensions) != 1 || descriptor.Identification.Extensions[0] != ".mdb" {
		t.Fatalf("descriptor = %#v, want access .mdb identity", descriptor)
	}
	if _, ok := interface{}(plugin).(format.RuntimeContainerInfoProviderFactory); ok {
		t.Fatal("generic Access plugin must not expose PGeo container inspection")
	}
	if _, ok := interface{}(plugin).(format.RuntimeFormatDetectorFactory); !ok {
		t.Fatal("Access plugin must expose runtime format detection")
	}
}
