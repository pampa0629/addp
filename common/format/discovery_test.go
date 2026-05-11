package format

import (
	"context"
	"io"
	"testing"

	"github.com/addp/common/resource"
)

func TestListFormatCapabilityViewsIncludesMarkdown(t *testing.T) {
	views := ListFormatCapabilityViews()
	if len(views) == 0 {
		t.Fatal("expected builtin format capability views")
	}

	for _, view := range views {
		if view.Format != FormatMarkdown {
			continue
		}
		if view.PluginID != "builtin-markdown" {
			t.Fatalf("PluginID = %q, want builtin-markdown", view.PluginID)
		}
		if view.DataType != FormatDataTypeDocument {
			t.Fatalf("DataType = %q, want %q", view.DataType, FormatDataTypeDocument)
		}
		if view.Preview.FrontendRenderer != "markdown" {
			t.Fatalf("FrontendRenderer = %q, want markdown", view.Preview.FrontendRenderer)
		}
		return
	}
	t.Fatal("markdown capability view not found")
}

func TestGetFormatCapabilityView(t *testing.T) {
	view, ok := GetFormatCapabilityView(FormatShapefile)
	if !ok {
		t.Fatal("expected shapefile capability view")
	}
	if !view.Spatial {
		t.Fatal("shapefile capability view should declare spatial")
	}
	if !view.Providers.ComponentTable {
		t.Fatal("shapefile capability view should declare component table provider")
	}
}

func TestListFormatConflictDiagnosticsIsAvailable(t *testing.T) {
	diagnostics := ListFormatConflictDiagnostics()
	if diagnostics == nil {
		t.Fatal("ListFormatConflictDiagnostics should return an empty slice or diagnostics, not nil")
	}
}

func TestFormatCapabilityViewSeparatesDeclaredProvidersAndImplementations(t *testing.T) {
	formatType := FormatType("discovery_declared_document")
	if err := RegisterFormatDescriptor(FormatDescriptor{
		ID:       "discovery-declared-document",
		Format:   formatType,
		DataType: FormatDataTypeDocument,
		Layouts:  []string{FormatLayoutSingle},
		Identification: FormatIdentification{
			Extensions: []string{".ddoc"},
			MimeTypes:  []string{"application/x-discovery-declared-document"},
		},
		Providers: FormatProviderDescriptor{
			Document: true,
			Metadata: true,
		},
		Preview: FormatPreviewDescriptor{
			Kind:             "text",
			PreviewMaterials: []string{"text"},
			FrontendRenderer: "text",
		},
	}); err != nil {
		t.Fatalf("RegisterFormatDescriptor() error = %v", err)
	}

	view, ok := GetFormatCapabilityView(formatType)
	if !ok {
		t.Fatal("expected capability view")
	}
	if !view.Providers.Document || !view.Providers.Metadata {
		t.Fatalf("declared providers = %#v, want document and metadata", view.Providers)
	}
	if view.Implementations.DocumentProvider || view.Implementations.MetadataExtractor {
		t.Fatalf("implementations = %#v, want none before providers are registered", view.Implementations)
	}
}

func TestFormatCapabilityViewReportsRegisteredImplementations(t *testing.T) {
	formatType := FormatType("discovery_implemented_document")
	mimeType := "application/x-discovery-implemented-document"
	if err := RegisterFormatDescriptor(FormatDescriptor{
		ID:       "discovery-implemented-document",
		Format:   formatType,
		DataType: FormatDataTypeDocument,
		Layouts:  []string{FormatLayoutSingle},
		Identification: FormatIdentification{
			Extensions: []string{".idoc"},
			MimeTypes:  []string{mimeType},
		},
		Providers: FormatProviderDescriptor{
			Document: true,
			Metadata: true,
		},
		Preview: FormatPreviewDescriptor{
			Kind:             "text",
			PreviewMaterials: []string{"text"},
			FrontendRenderer: "text",
		},
	}); err != nil {
		t.Fatalf("RegisterFormatDescriptor() error = %v", err)
	}
	if err := RegisterDocumentProvider(NewDocumentProvider(formatType, nil, nil)); err != nil {
		t.Fatalf("RegisterDocumentProvider() error = %v", err)
	}
	if err := RegisterExtractor(discoveryTestExtractor{mimeType: mimeType}); err != nil {
		t.Fatalf("RegisterExtractor() error = %v", err)
	}

	view, ok := GetFormatCapabilityView(formatType)
	if !ok {
		t.Fatal("expected capability view")
	}
	if !view.Implementations.DocumentProvider {
		t.Fatalf("implementations = %#v, want document provider", view.Implementations)
	}
	if !view.Implementations.MetadataExtractor {
		t.Fatalf("implementations = %#v, want metadata extractor", view.Implementations)
	}
}

func TestFormatCapabilityViewReportsTableProviderSpecializations(t *testing.T) {
	formatType := FormatType("discovery_scope_table")
	if err := RegisterFormatDescriptor(FormatDescriptor{
		ID:       "discovery-scope-table",
		Format:   formatType,
		DataType: FormatDataTypeTable,
		Layouts:  []string{FormatLayoutSingle, FormatLayoutWhole},
		Providers: FormatProviderDescriptor{
			Table:      true,
			ScopeTable: true,
		},
		Preview: FormatPreviewDescriptor{
			Kind:             "table",
			PreviewMaterials: []string{"table"},
			FrontendRenderer: "table",
		},
	}); err != nil {
		t.Fatalf("RegisterFormatDescriptor() error = %v", err)
	}
	if err := RegisterTableProvider(discoveryScopeTableProvider{formatType: formatType}); err != nil {
		t.Fatalf("RegisterTableProvider() error = %v", err)
	}

	view, ok := GetFormatCapabilityView(formatType)
	if !ok {
		t.Fatal("expected capability view")
	}
	if !view.Implementations.TableProvider || !view.Implementations.ScopeTableProvider {
		t.Fatalf("implementations = %#v, want table and scope table providers", view.Implementations)
	}
	if view.Implementations.ComponentTableProvider {
		t.Fatalf("implementations = %#v, did not expect component table provider", view.Implementations)
	}
}

func TestFormatCapabilityViewReportsMediaProvider(t *testing.T) {
	formatType := FormatType("discovery_media")
	if err := RegisterFormatDescriptor(FormatDescriptor{
		ID:       "discovery-media",
		Format:   formatType,
		DataType: FormatDataTypeMedia,
		Layouts:  []string{FormatLayoutSingle},
		Providers: FormatProviderDescriptor{
			Media: true,
		},
		Preview: FormatPreviewDescriptor{
			Kind:             "image",
			PreviewMaterials: []string{"image"},
			FrontendRenderer: "image",
		},
	}); err != nil {
		t.Fatalf("RegisterFormatDescriptor() error = %v", err)
	}
	if err := RegisterMediaProvider(NewMediaProvider(formatType, nil)); err != nil {
		t.Fatalf("RegisterMediaProvider() error = %v", err)
	}

	view, ok := GetFormatCapabilityView(formatType)
	if !ok {
		t.Fatal("expected capability view")
	}
	if !view.Implementations.MediaProvider {
		t.Fatalf("implementations = %#v, want media provider", view.Implementations)
	}
}

type discoveryTestExtractor struct {
	mimeType string
}

func (e discoveryTestExtractor) SupportedTypes() []string {
	return []string{e.mimeType}
}

func (e discoveryTestExtractor) Extract(context.Context, ExtractInput) (*ExtractedMetadata, error) {
	return &ExtractedMetadata{}, nil
}

func (e discoveryTestExtractor) Priority() int {
	return 1
}

type discoveryScopeTableProvider struct {
	formatType FormatType
}

func (p discoveryScopeTableProvider) Format() FormatType {
	return p.formatType
}

func (p discoveryScopeTableProvider) Capabilities() FormatCapability {
	capability, _ := GetFormatCapability(p.formatType)
	return capability
}

func (p discoveryScopeTableProvider) DescribeTable(context.Context, io.Reader, *ParseOptions) (*TableInfo, error) {
	return &TableInfo{}, nil
}

func (p discoveryScopeTableProvider) SampleTable(context.Context, io.Reader, int64, int64, *ParseOptions) ([]map[string]interface{}, error) {
	return nil, nil
}

func (p discoveryScopeTableProvider) DescribeTableScope(context.Context, resource.ResourceReader, resource.ResourceRef, *ParseOptions) (*TableInfo, error) {
	return &TableInfo{}, nil
}

func (p discoveryScopeTableProvider) SampleTableScope(context.Context, resource.ResourceReader, resource.ResourceRef, int64, int64, *ParseOptions) ([]map[string]interface{}, error) {
	return nil, nil
}
