package format_test

import (
	"context"
	"io"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	. "github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
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
		if !containsStringForDiscoveryTest(view.ContentReaders, string(ContentReaderDocumentText)) {
			t.Fatalf("ContentReaders = %#v, want document text reader", view.ContentReaders)
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
	if !view.Providers.MultiTable {
		t.Fatal("shapefile capability view should declare multi table provider")
	}
	if view.Providers.ContentIndex {
		t.Fatal("shapefile capability view should not declare content index provider; .shx is native format indexing")
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
			DocumentInfo: true,
			FormatInfo:   true,
		},
		ContentReaders: []string{string(ContentReaderDocumentText)},
	}); err != nil {
		t.Fatalf("RegisterFormatDescriptor() error = %v", err)
	}

	view, ok := GetFormatCapabilityView(formatType)
	if !ok {
		t.Fatal("expected capability view")
	}
	if !view.Providers.DocumentInfo || !view.Providers.FormatInfo {
		t.Fatalf("declared providers = %#v, want document info and format info", view.Providers)
	}
	if view.Implementations.DocumentInfoProvider || view.Implementations.DocumentTextReader {
		t.Fatalf("implementations = %#v, want none before providers are registered", view.Implementations)
	}
}

func TestFormatCapabilityViewReportsContentIndexProviderCapability(t *testing.T) {
	view, ok := GetFormatCapabilityView(FormatCSV)
	if !ok {
		t.Fatal("expected csv capability view")
	}
	if !view.Providers.ContentIndex {
		t.Fatalf("providers = %#v, want content_index", view.Providers)
	}
}

func TestFormatCapabilityViewReportsTableWriterProvider(t *testing.T) {
	view, ok := GetFormatCapabilityView(FormatCSV)
	if !ok {
		t.Fatal("expected csv capability view")
	}
	if !view.Transfer.Write {
		t.Fatalf("transfer = %#v, want write declared", view.Transfer)
	}
	if !view.Implementations.TableWriterProvider {
		t.Fatalf("implementations = %#v, want table writer provider", view.Implementations)
	}
}

func TestFormatCapabilityViewReportsRegisteredImplementations(t *testing.T) {
	formatType := FormatType("discovery_implemented_document")
	if err := RegisterFormatDescriptor(FormatDescriptor{
		ID:       "discovery-implemented-document",
		Format:   formatType,
		DataType: FormatDataTypeDocument,
		Layouts:  []string{FormatLayoutSingle},
		Identification: FormatIdentification{
			Extensions: []string{".idoc"},
			MimeTypes:  []string{"application/x-discovery-implemented-document"},
		},
		Providers: FormatProviderDescriptor{
			DocumentInfo: true,
			FormatInfo:   true,
		},
		ContentReaders: []string{string(ContentReaderDocumentText)},
	}); err != nil {
		t.Fatalf("RegisterFormatDescriptor() error = %v", err)
	}
	if err := RegisterDocumentInfoProvider(NewDocumentInfoProvider(formatType, nil)); err != nil {
		t.Fatalf("RegisterDocumentInfoProvider() error = %v", err)
	}
	if err := RegisterDocumentTextReader(NewDocumentTextReader(formatType, nil)); err != nil {
		t.Fatalf("RegisterDocumentTextReader() error = %v", err)
	}
	if err := RegisterFormatPlugin(discoveryDocumentPlugin{formatType: formatType}); err != nil {
		t.Fatalf("RegisterFormatPlugin() error = %v", err)
	}

	view, ok := GetFormatCapabilityView(formatType)
	if !ok {
		t.Fatal("expected capability view")
	}
	if !view.Implementations.DocumentInfoProvider || !view.Implementations.DocumentTextReader {
		t.Fatalf("implementations = %#v, want document info/text providers", view.Implementations)
	}
	if !view.Implementations.FormatPlugin {
		t.Fatalf("implementations = %#v, want format plugin", view.Implementations)
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
			TableInfo:   true,
			TableSample: true,
			Table:       true,
			ScopeTable:  true,
		},
		ContentReaders: []string{string(ContentReaderTableSample), string(ContentReaderScopeTableSample)},
	}); err != nil {
		t.Fatalf("RegisterFormatDescriptor() error = %v", err)
	}
	provider := discoveryScopeTableProvider{formatType: formatType}
	if err := RegisterTableInfoProvider(provider); err != nil {
		t.Fatalf("RegisterTableInfoProvider() error = %v", err)
	}
	if err := RegisterTableSampleReader(provider); err != nil {
		t.Fatalf("RegisterTableSampleReader() error = %v", err)
	}
	if err := RegisterScopeTableInfoProvider(provider); err != nil {
		t.Fatalf("RegisterScopeTableInfoProvider() error = %v", err)
	}
	if err := RegisterScopeTableSampleReader(provider); err != nil {
		t.Fatalf("RegisterScopeTableSampleReader() error = %v", err)
	}
	if err := RegisterScopeTableReaderProvider(provider); err != nil {
		t.Fatalf("RegisterScopeTableReaderProvider() error = %v", err)
	}

	view, ok := GetFormatCapabilityView(formatType)
	if !ok {
		t.Fatal("expected capability view")
	}
	if !view.Implementations.TableInfoProvider || !view.Implementations.TableSampleReader {
		t.Fatalf("implementations = %#v, want table info and sample providers", view.Implementations)
	}
	if !view.Implementations.ScopeTableInfoProvider || !view.Implementations.ScopeTableSampleReader || !view.Implementations.ScopeTableReader {
		t.Fatalf("implementations = %#v, want scope table info, sample, and reader providers", view.Implementations)
	}
	if view.Implementations.MultiTableInfoProvider || view.Implementations.MultiTableSampleReader {
		t.Fatalf("implementations = %#v, did not expect multi table providers", view.Implementations)
	}
}

func TestFormatCapabilityViewReportsMultiTableReaderProvider(t *testing.T) {
	view, ok := GetFormatCapabilityView(FormatShapefile)
	if !ok {
		t.Fatal("expected shapefile capability view")
	}
	if !view.Implementations.MultiTableReader {
		t.Fatalf("implementations = %#v, want multi table reader provider", view.Implementations)
	}
}

func TestFormatCapabilityViewReportsMediaInfoProvider(t *testing.T) {
	formatType := FormatType("discovery_media")
	if err := RegisterFormatDescriptor(FormatDescriptor{
		ID:       "discovery-media",
		Format:   formatType,
		DataType: FormatDataTypeMedia,
		Layouts:  []string{FormatLayoutSingle},
		Providers: FormatProviderDescriptor{
			MediaInfo: true,
		},
		ContentReaders: []string{string(ContentReaderRawContent)},
	}); err != nil {
		t.Fatalf("RegisterFormatDescriptor() error = %v", err)
	}
	if err := RegisterMediaInfoProvider(NewMediaInfoProvider(formatType, nil)); err != nil {
		t.Fatalf("RegisterMediaInfoProvider() error = %v", err)
	}

	view, ok := GetFormatCapabilityView(formatType)
	if !ok {
		t.Fatal("expected capability view")
	}
	if !view.Implementations.MediaInfoProvider {
		t.Fatalf("implementations = %#v, want media info provider", view.Implementations)
	}
}

type discoveryDocumentPlugin struct {
	formatType FormatType
}

func (p discoveryDocumentPlugin) Format() FormatType {
	return p.formatType
}

func (p discoveryDocumentPlugin) Descriptor() FormatDescriptor {
	return FormatDescriptor{
		ID:       "discovery-document-plugin",
		Format:   p.formatType,
		DataType: FormatDataTypeDocument,
		Layouts:  []string{FormatLayoutSingle},
	}
}

func (p discoveryDocumentPlugin) Capabilities() FormatCapability {
	capability, _ := GetFormatCapability(p.formatType)
	return capability
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

func (p discoveryScopeTableProvider) DescribeTable(context.Context, io.Reader, *ParseOptions) (*TableDescribeResult, error) {
	return &TableDescribeResult{Table: &datatype.TableInfo{}}, nil
}

func (p discoveryScopeTableProvider) SampleTable(context.Context, io.Reader, int64, int64, *ParseOptions) ([]map[string]interface{}, error) {
	return nil, nil
}

func (p discoveryScopeTableProvider) DescribeTableScope(context.Context, contentio.Reader, contentio.Ref, *ParseOptions) (*TableDescribeResult, error) {
	return &TableDescribeResult{Table: &datatype.TableInfo{}}, nil
}

func (p discoveryScopeTableProvider) SampleTableScope(context.Context, contentio.Reader, contentio.Ref, int64, int64, *ParseOptions) ([]map[string]interface{}, error) {
	return nil, nil
}

func (p discoveryScopeTableProvider) OpenTableScopeReader(context.Context, contentio.Reader, contentio.Ref, *ParseOptions) (TableReader, error) {
	return nil, nil
}

func containsStringForDiscoveryTest(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
