package format

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/resource"
)

func providerTestDescribe(context.Context, io.Reader, *ParseOptions) (*TableInfo, error) {
	rowCount := int64(1)
	return &TableInfo{RowCount: &rowCount}, nil
}

func providerTestSample(context.Context, io.Reader, int64, int64, *ParseOptions) ([]map[string]interface{}, error) {
	return []map[string]interface{}{{"id": 1}}, nil
}

type providerTestPlugin struct {
	TableProvider
}

func (p providerTestPlugin) Descriptor() FormatDescriptor {
	return FormatDescriptor{
		ID:       "provider-test-plugin",
		Format:   p.Format(),
		DataType: FormatDataTypeTable,
		Layouts:  []string{FormatLayoutSingle},
	}
}

func (p providerTestPlugin) DescribeFormat(context.Context, io.Reader, *ParseOptions) (map[string]interface{}, error) {
	return map[string]interface{}{"kind": "test"}, nil
}

func TestRegisterFormatPlugin(t *testing.T) {
	registry := NewProviderRegistry()
	formatType := FormatType("plugin_test")
	plugin := providerTestPlugin{
		TableProvider: NewTableProvider(formatType, providerTestDescribe, providerTestSample),
	}

	if err := registry.RegisterFormatPlugin(plugin); err != nil {
		t.Fatalf("RegisterFormatPlugin() error = %v", err)
	}
	if got, err := registry.GetFormatPlugin(formatType); err != nil || got.Format() != formatType {
		t.Fatalf("GetFormatPlugin() = %#v, %v; want plugin_test", got, err)
	}
	if descriptor, ok := GetFormatDescriptor(formatType); !ok || descriptor.ID != "provider-test-plugin" {
		t.Fatalf("GetFormatDescriptor() = %#v, %v; want provider-test-plugin", descriptor, ok)
	}
	if capability, ok := GetFormatCapability(formatType); !ok || capability.DataType != FormatDataTypeTable {
		t.Fatalf("GetFormatCapability() = %#v, %v; want table capability", capability, ok)
	}
	if _, err := registry.GetFormatInfoProvider(formatType); err != nil {
		t.Fatalf("GetFormatInfoProvider() error = %v", err)
	}
	if _, err := registry.GetTableProvider(formatType); err != nil {
		t.Fatalf("GetTableProvider() error = %v", err)
	}
	if _, err := registry.GetTableInfoProvider(formatType); err != nil {
		t.Fatalf("GetTableInfoProvider() error = %v", err)
	}
	if _, err := registry.GetTableSampleProvider(formatType); err != nil {
		t.Fatalf("GetTableSampleProvider() error = %v", err)
	}
}

func TestRegisterTableProvider(t *testing.T) {
	registry := NewProviderRegistry()
	provider := NewTableProvider(FormatType("provider_test"), providerTestDescribe, providerTestSample)

	if err := registry.RegisterTableProvider(provider); err != nil {
		t.Fatalf("RegisterTableProvider() error = %v", err)
	}

	got, err := registry.GetTableProvider(FormatType("provider_test"))
	if err != nil {
		t.Fatalf("GetTableProvider() error = %v", err)
	}
	if got.Format() != FormatType("provider_test") {
		t.Fatalf("provider format = %q, want provider_test", got.Format())
	}

	infoProvider, err := registry.GetTableInfoProvider(FormatType("provider_test"))
	if err != nil {
		t.Fatalf("GetTableInfoProvider() error = %v", err)
	}
	if infoProvider.Format() != FormatType("provider_test") {
		t.Fatalf("table info provider format = %q, want provider_test", infoProvider.Format())
	}

	sampleProvider, err := registry.GetTableSampleProvider(FormatType("provider_test"))
	if err != nil {
		t.Fatalf("GetTableSampleProvider() error = %v", err)
	}
	if sampleProvider.Format() != FormatType("provider_test") {
		t.Fatalf("table sample provider format = %q, want provider_test", sampleProvider.Format())
	}
}

func TestRegisterFormatInfoProvider(t *testing.T) {
	registry := NewProviderRegistry()
	provider := NewFormatInfoProvider(
		FormatType("format_info_test"),
		func(context.Context, io.Reader, *ParseOptions) (map[string]interface{}, error) {
			return map[string]interface{}{"delimiter": ","}, nil
		},
	)

	if err := registry.RegisterFormatInfoProvider(provider); err != nil {
		t.Fatalf("RegisterFormatInfoProvider() error = %v", err)
	}

	got, err := registry.GetFormatInfoProvider(FormatType("format_info_test"))
	if err != nil {
		t.Fatalf("GetFormatInfoProvider() error = %v", err)
	}
	if got.Format() != FormatType("format_info_test") {
		t.Fatalf("format info provider format = %q, want format_info_test", got.Format())
	}
}

func TestRegisterTableInfoProvider(t *testing.T) {
	registry := NewProviderRegistry()
	provider := NewTableProvider(FormatType("table_info_only"), providerTestDescribe, nil)

	if err := registry.RegisterTableInfoProvider(provider); err != nil {
		t.Fatalf("RegisterTableInfoProvider() error = %v", err)
	}

	if _, err := registry.GetTableInfoProvider(FormatType("table_info_only")); err != nil {
		t.Fatalf("GetTableInfoProvider() error = %v", err)
	}
	if _, err := registry.GetTableSampleProvider(FormatType("table_info_only")); err == nil {
		t.Fatalf("GetTableSampleProvider() error = nil, want missing provider")
	}
}

func TestRegisterTableSampleProvider(t *testing.T) {
	registry := NewProviderRegistry()
	provider := NewTableProvider(FormatType("table_sample_only"), nil, providerTestSample)

	if err := registry.RegisterTableSampleProvider(provider); err != nil {
		t.Fatalf("RegisterTableSampleProvider() error = %v", err)
	}

	if _, err := registry.GetTableSampleProvider(FormatType("table_sample_only")); err != nil {
		t.Fatalf("GetTableSampleProvider() error = %v", err)
	}
	if _, err := registry.GetTableInfoProvider(FormatType("table_sample_only")); err == nil {
		t.Fatalf("GetTableInfoProvider() error = nil, want missing provider")
	}
}

type providerTestTableReaderProvider struct {
	formatType FormatType
}

func (p providerTestTableReaderProvider) Format() FormatType {
	return p.formatType
}

func (p providerTestTableReaderProvider) Capabilities() FormatCapability {
	return FormatCapability{Format: p.formatType, DataType: FormatDataTypeTable}
}

func (p providerTestTableReaderProvider) OpenTableReader(context.Context, io.Reader, *ParseOptions) (TableReader, error) {
	return nil, nil
}

type providerTestComponentTableWriterProvider struct {
	formatType FormatType
}

func (p providerTestComponentTableWriterProvider) Format() FormatType {
	return p.formatType
}

func (p providerTestComponentTableWriterProvider) Capabilities() FormatCapability {
	return FormatCapability{Format: p.formatType, DataType: FormatDataTypeTable}
}

func (p providerTestComponentTableWriterProvider) ComponentSpecs() []resource.ComponentSpec {
	return []resource.ComponentSpec{{Extension: ".main", Role: "main", Required: true}}
}

func (p providerTestComponentTableWriterProvider) OpenComponentTableWriter(context.Context, resource.ComponentWriter, resource.ResourceRef, *TableInfo, *WriteOptions) (TableWriter, error) {
	return nil, nil
}

func TestRegisterTableReaderProvider(t *testing.T) {
	registry := NewProviderRegistry()
	formatType := FormatType("table_reader_only")

	if err := registry.RegisterTableReaderProvider(providerTestTableReaderProvider{formatType: formatType}); err != nil {
		t.Fatalf("RegisterTableReaderProvider() error = %v", err)
	}
	if got, err := registry.GetTableReaderProvider(formatType); err != nil || got.Format() != formatType {
		t.Fatalf("GetTableReaderProvider() = %#v, %v; want table_reader_only", got, err)
	}
}

func TestRegisterComponentTableWriterProvider(t *testing.T) {
	registry := NewProviderRegistry()
	formatType := FormatType("component_table_writer_only")

	if err := registry.RegisterComponentTableWriterProvider(providerTestComponentTableWriterProvider{formatType: formatType}); err != nil {
		t.Fatalf("RegisterComponentTableWriterProvider() error = %v", err)
	}
	if got, err := registry.GetComponentTableWriterProvider(formatType); err != nil || got.Format() != formatType {
		t.Fatalf("GetComponentTableWriterProvider() = %#v, %v; want component_table_writer_only", got, err)
	}
}

func TestListTableProviderFormatsSorted(t *testing.T) {
	registry := NewProviderRegistry()
	if err := registry.RegisterTableProvider(NewTableProvider(FormatType("zeta"), providerTestDescribe, providerTestSample)); err != nil {
		t.Fatalf("RegisterTableProvider(zeta) error = %v", err)
	}
	if err := registry.RegisterTableProvider(NewTableProvider(FormatType("alpha"), providerTestDescribe, providerTestSample)); err != nil {
		t.Fatalf("RegisterTableProvider(alpha) error = %v", err)
	}

	got := registry.ListTableProviderFormats()
	want := []FormatType{"alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListTableProviderFormats() = %#v, want %#v", got, want)
	}
}

func TestListFormatInfoProviderFormatsSorted(t *testing.T) {
	registry := NewProviderRegistry()
	if err := registry.RegisterFormatInfoProvider(NewFormatInfoProvider(FormatType("zeta"), nil)); err != nil {
		t.Fatalf("RegisterFormatInfoProvider(zeta) error = %v", err)
	}
	if err := registry.RegisterFormatInfoProvider(NewFormatInfoProvider(FormatType("alpha"), nil)); err != nil {
		t.Fatalf("RegisterFormatInfoProvider(alpha) error = %v", err)
	}

	got := registry.ListFormatInfoProviderFormats()
	want := []FormatType{"alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListFormatInfoProviderFormats() = %#v, want %#v", got, want)
	}
}

func TestListTableInfoProviderFormatsSorted(t *testing.T) {
	registry := NewProviderRegistry()
	if err := registry.RegisterTableInfoProvider(NewTableProvider(FormatType("zeta"), providerTestDescribe, nil)); err != nil {
		t.Fatalf("RegisterTableInfoProvider(zeta) error = %v", err)
	}
	if err := registry.RegisterTableInfoProvider(NewTableProvider(FormatType("alpha"), providerTestDescribe, nil)); err != nil {
		t.Fatalf("RegisterTableInfoProvider(alpha) error = %v", err)
	}

	got := registry.ListTableInfoProviderFormats()
	want := []FormatType{"alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListTableInfoProviderFormats() = %#v, want %#v", got, want)
	}
}

func TestListTableSampleProviderFormatsSorted(t *testing.T) {
	registry := NewProviderRegistry()
	if err := registry.RegisterTableSampleProvider(NewTableProvider(FormatType("zeta"), nil, providerTestSample)); err != nil {
		t.Fatalf("RegisterTableSampleProvider(zeta) error = %v", err)
	}
	if err := registry.RegisterTableSampleProvider(NewTableProvider(FormatType("alpha"), nil, providerTestSample)); err != nil {
		t.Fatalf("RegisterTableSampleProvider(alpha) error = %v", err)
	}

	got := registry.ListTableSampleProviderFormats()
	want := []FormatType{"alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListTableSampleProviderFormats() = %#v, want %#v", got, want)
	}
}

func TestListTableReaderProviderFormatsSorted(t *testing.T) {
	registry := NewProviderRegistry()
	if err := registry.RegisterTableReaderProvider(providerTestTableReaderProvider{formatType: FormatType("zeta")}); err != nil {
		t.Fatalf("RegisterTableReaderProvider(zeta) error = %v", err)
	}
	if err := registry.RegisterTableReaderProvider(providerTestTableReaderProvider{formatType: FormatType("alpha")}); err != nil {
		t.Fatalf("RegisterTableReaderProvider(alpha) error = %v", err)
	}

	got := registry.ListTableReaderProviderFormats()
	want := []FormatType{"alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListTableReaderProviderFormats() = %#v, want %#v", got, want)
	}
}

func TestListComponentTableWriterProviderFormatsSorted(t *testing.T) {
	registry := NewProviderRegistry()
	if err := registry.RegisterComponentTableWriterProvider(providerTestComponentTableWriterProvider{formatType: FormatType("zeta")}); err != nil {
		t.Fatalf("RegisterComponentTableWriterProvider(zeta) error = %v", err)
	}
	if err := registry.RegisterComponentTableWriterProvider(providerTestComponentTableWriterProvider{formatType: FormatType("alpha")}); err != nil {
		t.Fatalf("RegisterComponentTableWriterProvider(alpha) error = %v", err)
	}

	got := registry.ListComponentTableWriterProviderFormats()
	want := []FormatType{"alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListComponentTableWriterProviderFormats() = %#v, want %#v", got, want)
	}
}

func TestRegisterContainerInfoProvider(t *testing.T) {
	registry := NewProviderRegistry()
	provider := NewContainerInfoProvider(
		FormatType("container_test"),
		func(context.Context, io.Reader, *ParseOptions) (*ContainerInfo, error) {
			return &ContainerInfo{
				Format:       FormatType("container_test"),
				ChildCount:   1,
				DefaultChild: "Sheet1",
				Children: []ContainerChildInfo{{
					Name:     "Sheet1",
					Kind:     "sheet",
					DataType: FormatDataTypeTable,
				}},
			}, nil
		},
	)

	if err := registry.RegisterContainerInfoProvider(provider); err != nil {
		t.Fatalf("RegisterContainerInfoProvider() error = %v", err)
	}

	got, err := registry.GetContainerInfoProvider(FormatType("container_test"))
	if err != nil {
		t.Fatalf("GetContainerInfoProvider() error = %v", err)
	}
	if got.Format() != FormatType("container_test") {
		t.Fatalf("container info provider format = %q, want container_test", got.Format())
	}
}

func TestListContainerInfoProviderFormatsSorted(t *testing.T) {
	registry := NewProviderRegistry()
	if err := registry.RegisterContainerInfoProvider(NewContainerInfoProvider(FormatType("zeta"), nil)); err != nil {
		t.Fatalf("RegisterContainerInfoProvider(zeta) error = %v", err)
	}
	if err := registry.RegisterContainerInfoProvider(NewContainerInfoProvider(FormatType("alpha"), nil)); err != nil {
		t.Fatalf("RegisterContainerInfoProvider(alpha) error = %v", err)
	}

	got := registry.ListContainerInfoProviderFormats()
	want := []FormatType{"alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListContainerInfoProviderFormats() = %#v, want %#v", got, want)
	}
}

func TestRegisterContainerChildResolver(t *testing.T) {
	registry := NewProviderRegistry()
	resolver := NewContainerChildResolver(
		FormatType("container_child_test"),
		func(_ context.Context, parent resource.ResourceReader, parentRef resource.ResourceRef, child ContainerChildInfo, _ *ParseOptions) (*ContainerChildResource, error) {
			return NativeContainerChildResource(parent, parentRef, FormatType("container_child_test"), child, ChildTableParseOptions(child.Name, nil)), nil
		},
	)

	if err := registry.RegisterContainerChildResolver(resolver); err != nil {
		t.Fatalf("RegisterContainerChildResolver() error = %v", err)
	}

	got, err := registry.GetContainerChildResolver(FormatType("container_child_test"))
	if err != nil {
		t.Fatalf("GetContainerChildResolver() error = %v", err)
	}
	if got.Format() != FormatType("container_child_test") {
		t.Fatalf("container child resolver format = %q, want container_child_test", got.Format())
	}
}

func TestListContainerChildResolverFormatsSorted(t *testing.T) {
	registry := NewProviderRegistry()
	if err := registry.RegisterContainerChildResolver(NewContainerChildResolver(FormatType("zeta"), nil)); err != nil {
		t.Fatalf("RegisterContainerChildResolver(zeta) error = %v", err)
	}
	if err := registry.RegisterContainerChildResolver(NewContainerChildResolver(FormatType("alpha"), nil)); err != nil {
		t.Fatalf("RegisterContainerChildResolver(alpha) error = %v", err)
	}

	got := registry.ListContainerChildResolverFormats()
	want := []FormatType{"alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListContainerChildResolverFormats() = %#v, want %#v", got, want)
	}
}

func TestNewTableProviderHandlesMissingImplementation(t *testing.T) {
	provider := NewTableProvider(FormatType("empty"), nil, nil)

	if _, err := provider.DescribeTable(context.Background(), strings.NewReader(""), nil); err == nil {
		t.Fatalf("DescribeTable() error = nil, want error")
	}
	if _, err := provider.SampleTable(context.Background(), strings.NewReader(""), 0, 1, nil); err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("SampleTable() error = %v, want implementation error", err)
	}
}

func TestRegisterDocumentProvider(t *testing.T) {
	registry := NewProviderRegistry()
	provider := NewDocumentProvider(
		FormatType("doc_test"),
		func(context.Context, io.Reader, *ParseOptions) (*DocumentInfo, error) {
			return &DocumentInfo{Format: FormatType("doc_test"), Encoding: "utf-8"}, nil
		},
		func(context.Context, io.Reader, int64, *ParseOptions) (string, bool, error) {
			return "hello", false, nil
		},
	)

	if err := registry.RegisterDocumentProvider(provider); err != nil {
		t.Fatalf("RegisterDocumentProvider() error = %v", err)
	}

	got, err := registry.GetDocumentProvider(FormatType("doc_test"))
	if err != nil {
		t.Fatalf("GetDocumentProvider() error = %v", err)
	}
	if got.Format() != FormatType("doc_test") {
		t.Fatalf("provider format = %q, want doc_test", got.Format())
	}
}

func TestListDocumentProviderFormatsSorted(t *testing.T) {
	registry := NewProviderRegistry()
	provider := NewDocumentProvider(FormatType("zeta"), nil, nil)
	if err := registry.RegisterDocumentProvider(provider); err != nil {
		t.Fatalf("RegisterDocumentProvider(zeta) error = %v", err)
	}
	provider = NewDocumentProvider(FormatType("alpha"), nil, nil)
	if err := registry.RegisterDocumentProvider(provider); err != nil {
		t.Fatalf("RegisterDocumentProvider(alpha) error = %v", err)
	}

	got := registry.ListDocumentProviderFormats()
	want := []FormatType{"alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListDocumentProviderFormats() = %#v, want %#v", got, want)
	}
}

func TestNewDocumentProviderHandlesMissingImplementation(t *testing.T) {
	provider := NewDocumentProvider(FormatType("empty_doc"), nil, nil)

	if _, err := provider.DescribeDocument(context.Background(), strings.NewReader(""), nil); err == nil {
		t.Fatalf("DescribeDocument() error = nil, want error")
	}
	if _, _, err := provider.ReadDocumentText(context.Background(), strings.NewReader(""), 1, nil); err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("ReadDocumentText() error = %v, want implementation error", err)
	}
}

func TestRegisterMediaProvider(t *testing.T) {
	registry := NewProviderRegistry()
	provider := NewMediaProvider(
		FormatType("media_test"),
		func(context.Context, io.Reader, *ParseOptions) (*MediaInfo, error) {
			return &MediaInfo{Format: FormatType("media_test"), MediaType: "image"}, nil
		},
	)

	if err := registry.RegisterMediaProvider(provider); err != nil {
		t.Fatalf("RegisterMediaProvider() error = %v", err)
	}

	got, err := registry.GetMediaProvider(FormatType("media_test"))
	if err != nil {
		t.Fatalf("GetMediaProvider() error = %v", err)
	}
	if got.Format() != FormatType("media_test") {
		t.Fatalf("provider format = %q, want media_test", got.Format())
	}
}

func TestListMediaProviderFormatsSorted(t *testing.T) {
	registry := NewProviderRegistry()
	provider := NewMediaProvider(FormatType("zeta"), nil)
	if err := registry.RegisterMediaProvider(provider); err != nil {
		t.Fatalf("RegisterMediaProvider(zeta) error = %v", err)
	}
	provider = NewMediaProvider(FormatType("alpha"), nil)
	if err := registry.RegisterMediaProvider(provider); err != nil {
		t.Fatalf("RegisterMediaProvider(alpha) error = %v", err)
	}

	got := registry.ListMediaProviderFormats()
	want := []FormatType{"alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListMediaProviderFormats() = %#v, want %#v", got, want)
	}
}

func TestNewMediaProviderHandlesMissingImplementation(t *testing.T) {
	provider := NewMediaProvider(FormatType("empty_media"), nil)

	if _, err := provider.DescribeMedia(context.Background(), strings.NewReader(""), nil); err == nil {
		t.Fatalf("DescribeMedia() error = nil, want error")
	}
}
