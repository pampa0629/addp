package xml

import (
	"context"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestPluginDescriptorAndTextReader(t *testing.T) {
	plugin := NewPlugin()
	descriptor := plugin.Descriptor()
	if descriptor.Format != format.FormatXML || descriptor.DataType != datatype.Document || !format.HasLayout(descriptor.Layouts, format.LayoutSingle) {
		t.Fatalf("descriptor = %#v, want single XML document", descriptor)
	}
	if len(descriptor.Identification.Extensions) != 1 || descriptor.Identification.Extensions[0] != ".xml" {
		t.Fatalf("Extensions = %#v, want .xml", descriptor.Identification.Extensions)
	}
	if len(descriptor.Identification.MimeTypes) != 2 {
		t.Fatalf("MimeTypes = %#v, want application/xml and text/xml", descriptor.Identification.MimeTypes)
	}
	got, truncated, err := plugin.ReadDocumentText(context.Background(), strings.NewReader("<root>value</root>"), 1024, nil)
	if err != nil {
		t.Fatalf("ReadDocumentText() error = %v", err)
	}
	if truncated || got != "<root>value</root>" {
		t.Fatalf("ReadDocumentText() = %q, truncated=%v", got, truncated)
	}
}
