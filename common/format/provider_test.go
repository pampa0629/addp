package format

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
)

func providerTestDescribe(context.Context, io.Reader, *ParseOptions) (*TableDescribeResult, error) {
	rowCount := int64(1)
	return &TableDescribeResult{Table: &datatype.TableInfo{RowCount: &rowCount}}, nil
}

func providerTestSample(context.Context, io.Reader, int64, int64, *ParseOptions) ([]map[string]interface{}, error) {
	return []map[string]interface{}{{"id": 1}}, nil
}

type providerTestPlugin struct {
	formatType FormatType
}

func (p providerTestPlugin) Format() FormatType {
	return p.formatType
}

func (p providerTestPlugin) Descriptor() FormatDescriptor {
	return FormatDescriptor{
		ID:       "provider-test-plugin",
		Format:   p.formatType,
		DataType: datatype.Table,
		Layouts:  []string{LayoutSingle},
	}
}

func (p providerTestPlugin) DescribeFormat(context.Context, io.Reader, *ParseOptions) (map[string]interface{}, error) {
	return map[string]interface{}{"kind": "test"}, nil
}

func (p providerTestPlugin) DescribeTable(ctx context.Context, input io.Reader, options *ParseOptions) (*TableDescribeResult, error) {
	return providerTestDescribe(ctx, input, options)
}

func (p providerTestPlugin) SampleTable(ctx context.Context, input io.Reader, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error) {
	return providerTestSample(ctx, input, offset, limit, options)
}

func (p providerTestPlugin) SupportsAccessIndex() bool {
	return true
}

func TestRegisterFormatPluginRegistersDescriptorAndDynamicCapabilities(t *testing.T) {
	registry := NewProviderRegistry()
	formatType := FormatType("plugin_test")

	if err := registry.RegisterFormatPlugin(providerTestPlugin{formatType: formatType}); err != nil {
		t.Fatalf("RegisterFormatPlugin() error = %v", err)
	}
	if got, err := registry.GetFormatPlugin(formatType); err != nil || got.Format() != formatType {
		t.Fatalf("GetFormatPlugin() = %#v, %v; want plugin_test", got, err)
	}
	if descriptor, ok := GetFormatDescriptor(formatType); !ok || descriptor.ID != "provider-test-plugin" {
		t.Fatalf("GetFormatDescriptor() = %#v, %v; want provider-test-plugin", descriptor, ok)
	}
	if _, err := registry.GetFormatInfoProvider(formatType); err != nil {
		t.Fatalf("GetFormatInfoProvider() error = %v", err)
	}
	if _, err := registry.GetTableInfoProvider(formatType); err != nil {
		t.Fatalf("GetTableInfoProvider() error = %v", err)
	}
	if _, err := registry.GetTableSampleReader(formatType); err != nil {
		t.Fatalf("GetTableSampleReader() error = %v", err)
	}
	if provider, err := registry.GetTableInfoProvider(formatType); err != nil || !provider.(AccessIndexProvider).SupportsAccessIndex() {
		t.Fatalf("AccessIndexProvider = %#v, %v; want supported", provider, err)
	}
}

type providerTestIdentityPlugin struct {
	formatType FormatType
}

func (p providerTestIdentityPlugin) Format() FormatType {
	return p.formatType
}

func TestRegisterFormatPluginAllowsIdentityOnlyPlugin(t *testing.T) {
	registry := NewProviderRegistry()
	formatType := FormatType("identity_only")

	if err := registry.RegisterFormatPlugin(providerTestIdentityPlugin{formatType: formatType}); err != nil {
		t.Fatalf("RegisterFormatPlugin() error = %v", err)
	}
	if _, ok := GetFormatDescriptor(formatType); ok {
		t.Fatal("identity-only plugin should not register descriptor")
	}
	if _, err := registry.GetTableInfoProvider(formatType); err == nil {
		t.Fatal("GetTableInfoProvider() error = nil, want missing dynamic capability")
	}
}

func TestRegisterFormatPluginRejectsDescriptorFormatMismatch(t *testing.T) {
	registry := NewProviderRegistry()
	err := registry.RegisterFormatPlugin(providerTestMismatchedDescriptorPlugin{})
	if err == nil {
		t.Fatal("RegisterFormatPlugin() error = nil, want mismatch error")
	}
}

type providerTestMismatchedDescriptorPlugin struct{}

func (providerTestMismatchedDescriptorPlugin) Format() FormatType {
	return FormatType("plugin_format")
}

func (providerTestMismatchedDescriptorPlugin) Descriptor() FormatDescriptor {
	return FormatDescriptor{
		ID:       "mismatched",
		Format:   FormatType("descriptor_format"),
		DataType: datatype.Table,
	}
}

type providerTestTableReaderProvider struct {
	formatType FormatType
}

func (p providerTestTableReaderProvider) Format() FormatType {
	return p.formatType
}

func (p providerTestTableReaderProvider) OpenTableReader(context.Context, io.Reader, *ParseOptions) (TableReader, error) {
	return nil, nil
}

type providerTestMultiTableReaderProvider struct {
	formatType FormatType
	specs      []RelatedRefSpec
}

func (p providerTestMultiTableReaderProvider) Format() FormatType {
	return p.formatType
}

func (p providerTestMultiTableReaderProvider) RelatedRefSpecs() []RelatedRefSpec {
	if p.specs != nil {
		return p.specs
	}
	return []RelatedRefSpec{{Extension: ".main", Role: "main", Required: true, Primary: true}}
}

func (p providerTestMultiTableReaderProvider) OpenMultiTableReader(context.Context, contentio.Reader, []RelatedRef, *ParseOptions) (TableReader, error) {
	return nil, nil
}

type providerTestMultiTableWriterProvider struct {
	formatType FormatType
	specs      []RelatedRefSpec
}

func (p providerTestMultiTableWriterProvider) Format() FormatType {
	return p.formatType
}

func (p providerTestMultiTableWriterProvider) RelatedRefSpecs() []RelatedRefSpec {
	if p.specs != nil {
		return p.specs
	}
	return []RelatedRefSpec{{Extension: ".main", Role: "main", Required: true, Primary: true}}
}

func (p providerTestMultiTableWriterProvider) OpenMultiTableWriter(context.Context, contentio.Writer, []RelatedRef, *datatype.TableInfo, *WriteOptions) (TableWriter, error) {
	return nil, nil
}

type providerTestMultiPlugin struct {
	formatType FormatType
	providerTestMultiTableReaderProvider
	providerTestMultiTableWriterProvider
}

func newProviderTestMultiPlugin(formatType FormatType, specs []RelatedRefSpec) providerTestMultiPlugin {
	return providerTestMultiPlugin{
		formatType: formatType,
		providerTestMultiTableReaderProvider: providerTestMultiTableReaderProvider{
			formatType: formatType,
			specs:      specs,
		},
		providerTestMultiTableWriterProvider: providerTestMultiTableWriterProvider{
			formatType: formatType,
			specs:      specs,
		},
	}
}

func (p providerTestMultiPlugin) Format() FormatType {
	return p.formatType
}

func (p providerTestMultiPlugin) RelatedRefSpecs() []RelatedRefSpec {
	return p.providerTestMultiTableReaderProvider.RelatedRefSpecs()
}

func TestRegisterFormatPluginValidatesRelatedRefSpecs(t *testing.T) {
	registry := NewProviderRegistry()
	err := registry.RegisterFormatPlugin(newProviderTestMultiPlugin(FormatType("multi_table_invalid_refs"), []RelatedRefSpec{
		{Extension: ".main", Role: "main", Required: true},
	}))
	if err == nil {
		t.Fatal("RegisterFormatPlugin() succeeded with invalid related ref specs")
	}
}

func TestRegisterFormatPluginProvidesMultiReaderAndWriter(t *testing.T) {
	registry := NewProviderRegistry()
	formatType := FormatType("multi_table")
	if err := registry.RegisterFormatPlugin(newProviderTestMultiPlugin(formatType, nil)); err != nil {
		t.Fatalf("RegisterFormatPlugin() error = %v", err)
	}
	if got, err := registry.GetMultiTableReaderProvider(formatType); err != nil || got.Format() != formatType {
		t.Fatalf("GetMultiTableReaderProvider() = %#v, %v; want multi_table", got, err)
	}
	if got, err := registry.GetMultiTableWriterProvider(formatType); err != nil || got.Format() != formatType {
		t.Fatalf("GetMultiTableWriterProvider() = %#v, %v; want multi_table", got, err)
	}
}

func TestListFormatPluginFormatsSorted(t *testing.T) {
	registry := NewProviderRegistry()
	for _, formatType := range []FormatType{"zeta", "alpha"} {
		if err := registry.RegisterFormatPlugin(providerTestIdentityPlugin{formatType: formatType}); err != nil {
			t.Fatalf("RegisterFormatPlugin(%s) error = %v", formatType, err)
		}
	}

	got := registry.ListFormatPluginFormats()
	want := []FormatType{"alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListFormatPluginFormats() = %#v, want %#v", got, want)
	}
}

func TestNewTableInfoProviderHandlesMissingImplementation(t *testing.T) {
	provider := NewTableInfoProvider(FormatType("empty"), nil)

	if _, err := provider.DescribeTable(context.Background(), strings.NewReader(""), nil); err == nil {
		t.Fatalf("DescribeTable() error = nil, want error")
	}
}

func TestNewTableSampleReaderHandlesMissingImplementation(t *testing.T) {
	reader := NewTableSampleReader(FormatType("empty"), nil)

	if _, err := reader.SampleTable(context.Background(), strings.NewReader(""), 0, 1, nil); err == nil {
		t.Fatalf("SampleTable() error = nil, want implementation error")
	}
}

func TestNewDocumentInfoProviderHandlesMissingImplementation(t *testing.T) {
	provider := NewDocumentInfoProvider(FormatType("empty_doc"), nil)

	if _, err := provider.DescribeDocument(context.Background(), strings.NewReader(""), nil); err == nil {
		t.Fatalf("DescribeDocument() error = nil, want error")
	}
}

func TestNewDocumentTextReaderHandlesMissingImplementation(t *testing.T) {
	reader := NewDocumentTextReader(FormatType("empty_doc"), nil)

	if _, _, err := reader.ReadDocumentText(context.Background(), strings.NewReader(""), 1, nil); err == nil {
		t.Fatalf("ReadDocumentText() error = nil, want implementation error")
	}
}

func TestNewMediaInfoProviderHandlesMissingImplementation(t *testing.T) {
	provider := NewMediaInfoProvider(FormatType("empty_media"), nil)

	if _, err := provider.DescribeMedia(context.Background(), strings.NewReader(""), nil); err == nil {
		t.Fatalf("DescribeMedia() error = nil, want error")
	}
}
