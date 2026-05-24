package format

import (
	"context"
	"errors"
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
	TableInfoProvider
	TableSampleReader
}

func (p providerTestPlugin) Format() FormatType {
	return p.formatType
}

func (p providerTestPlugin) Capabilities() FormatCapability {
	return FormatCapability{Format: p.formatType, DataType: FormatDataTypeTable, Layouts: []string{FormatLayoutSingle}}
}

func (p providerTestPlugin) Descriptor() FormatDescriptor {
	return FormatDescriptor{
		ID:       "provider-test-plugin",
		Format:   p.formatType,
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
		formatType:        formatType,
		TableInfoProvider: NewTableInfoProvider(formatType, providerTestDescribe),
		TableSampleReader: NewTableSampleReader(formatType, providerTestSample),
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
	if _, err := registry.GetTableInfoProvider(formatType); err != nil {
		t.Fatalf("GetTableInfoProvider() error = %v", err)
	}
	if _, err := registry.GetTableSampleReader(formatType); err != nil {
		t.Fatalf("GetTableSampleReader() error = %v", err)
	}
}

func TestRegisterTableInfoAndSampleProvider(t *testing.T) {
	registry := NewProviderRegistry()
	infoProvider := NewTableInfoProvider(FormatType("provider_test"), providerTestDescribe)
	sampleReader := NewTableSampleReader(FormatType("provider_test"), providerTestSample)

	if err := registry.RegisterTableInfoProvider(infoProvider); err != nil {
		t.Fatalf("RegisterTableInfoProvider() error = %v", err)
	}
	if err := registry.RegisterTableSampleReader(sampleReader); err != nil {
		t.Fatalf("RegisterTableSampleReader() error = %v", err)
	}

	infoProvider, err := registry.GetTableInfoProvider(FormatType("provider_test"))
	if err != nil {
		t.Fatalf("GetTableInfoProvider() error = %v", err)
	}
	if infoProvider.Format() != FormatType("provider_test") {
		t.Fatalf("table info provider format = %q, want provider_test", infoProvider.Format())
	}

	sampleProvider, err := registry.GetTableSampleReader(FormatType("provider_test"))
	if err != nil {
		t.Fatalf("GetTableSampleReader() error = %v", err)
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
	provider := NewTableInfoProvider(FormatType("table_info_only"), providerTestDescribe)

	if err := registry.RegisterTableInfoProvider(provider); err != nil {
		t.Fatalf("RegisterTableInfoProvider() error = %v", err)
	}

	if _, err := registry.GetTableInfoProvider(FormatType("table_info_only")); err != nil {
		t.Fatalf("GetTableInfoProvider() error = %v", err)
	}
	if _, err := registry.GetTableSampleReader(FormatType("table_info_only")); err == nil {
		t.Fatalf("GetTableSampleReader() error = nil, want missing provider")
	}
}

func TestRegisterTableSampleReader(t *testing.T) {
	registry := NewProviderRegistry()
	provider := NewTableSampleReader(FormatType("table_sample_only"), providerTestSample)

	if err := registry.RegisterTableSampleReader(provider); err != nil {
		t.Fatalf("RegisterTableSampleReader() error = %v", err)
	}

	if _, err := registry.GetTableSampleReader(FormatType("table_sample_only")); err != nil {
		t.Fatalf("GetTableSampleReader() error = %v", err)
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

type providerTestMultiTableReaderProvider struct {
	formatType FormatType
	specs      []RelatedRefSpec
}

func (p providerTestMultiTableReaderProvider) Format() FormatType {
	return p.formatType
}

func (p providerTestMultiTableReaderProvider) Capabilities() FormatCapability {
	return FormatCapability{Format: p.formatType, DataType: FormatDataTypeTable}
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

func (p providerTestMultiTableWriterProvider) Capabilities() FormatCapability {
	return FormatCapability{Format: p.formatType, DataType: FormatDataTypeTable}
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

func TestRegisterMultiTableReaderProvider(t *testing.T) {
	registry := NewProviderRegistry()
	formatType := FormatType("multi_table_reader_only")

	if err := registry.RegisterMultiTableReaderProvider(providerTestMultiTableReaderProvider{formatType: formatType}); err != nil {
		t.Fatalf("RegisterMultiTableReaderProvider() error = %v", err)
	}
	if got, err := registry.GetMultiTableReaderProvider(formatType); err != nil || got.Format() != formatType {
		t.Fatalf("GetMultiTableReaderProvider() = %#v, %v; want multi_table_reader_only", got, err)
	}
}

func TestRegisterMultiTableReaderProviderRejectsInvalidRelatedRefSpecs(t *testing.T) {
	registry := NewProviderRegistry()
	formatType := FormatType("multi_table_reader_invalid_refs")

	err := registry.RegisterMultiTableReaderProvider(providerTestMultiTableReaderProvider{
		formatType: formatType,
		specs: []RelatedRefSpec{
			{Extension: ".main", Role: "main", Required: true},
		},
	})
	if err == nil {
		t.Fatal("RegisterMultiTableReaderProvider() succeeded with invalid related ref specs")
	}
}

func TestRegisterMultiTableWriterProvider(t *testing.T) {
	registry := NewProviderRegistry()
	formatType := FormatType("multi_table_writer_only")

	if err := registry.RegisterMultiTableWriterProvider(providerTestMultiTableWriterProvider{formatType: formatType}); err != nil {
		t.Fatalf("RegisterMultiTableWriterProvider() error = %v", err)
	}
	if got, err := registry.GetMultiTableWriterProvider(formatType); err != nil || got.Format() != formatType {
		t.Fatalf("GetMultiTableWriterProvider() = %#v, %v; want multi_table_writer_only", got, err)
	}
}

func TestRegisterMultiTableWriterProviderRejectsInvalidRelatedRefSpecs(t *testing.T) {
	registry := NewProviderRegistry()
	formatType := FormatType("multi_table_writer_invalid_refs")

	err := registry.RegisterMultiTableWriterProvider(providerTestMultiTableWriterProvider{
		formatType: formatType,
		specs: []RelatedRefSpec{
			{Extension: ".main", Role: "main", Required: true},
		},
	})
	if err == nil {
		t.Fatal("RegisterMultiTableWriterProvider() succeeded with invalid related ref specs")
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
	if err := registry.RegisterTableInfoProvider(NewTableInfoProvider(FormatType("zeta"), providerTestDescribe)); err != nil {
		t.Fatalf("RegisterTableInfoProvider(zeta) error = %v", err)
	}
	if err := registry.RegisterTableInfoProvider(NewTableInfoProvider(FormatType("alpha"), providerTestDescribe)); err != nil {
		t.Fatalf("RegisterTableInfoProvider(alpha) error = %v", err)
	}

	got := registry.ListTableInfoProviderFormats()
	want := []FormatType{"alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListTableInfoProviderFormats() = %#v, want %#v", got, want)
	}
}

func TestListTableSampleReaderFormatsSorted(t *testing.T) {
	registry := NewProviderRegistry()
	if err := registry.RegisterTableSampleReader(NewTableSampleReader(FormatType("zeta"), providerTestSample)); err != nil {
		t.Fatalf("RegisterTableSampleReader(zeta) error = %v", err)
	}
	if err := registry.RegisterTableSampleReader(NewTableSampleReader(FormatType("alpha"), providerTestSample)); err != nil {
		t.Fatalf("RegisterTableSampleReader(alpha) error = %v", err)
	}

	got := registry.ListTableSampleReaderFormats()
	want := []FormatType{"alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListTableSampleReaderFormats() = %#v, want %#v", got, want)
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

func TestListMultiTableReaderProviderFormatsSorted(t *testing.T) {
	registry := NewProviderRegistry()
	if err := registry.RegisterMultiTableReaderProvider(providerTestMultiTableReaderProvider{formatType: FormatType("zeta")}); err != nil {
		t.Fatalf("RegisterMultiTableReaderProvider(zeta) error = %v", err)
	}
	if err := registry.RegisterMultiTableReaderProvider(providerTestMultiTableReaderProvider{formatType: FormatType("alpha")}); err != nil {
		t.Fatalf("RegisterMultiTableReaderProvider(alpha) error = %v", err)
	}

	got := registry.ListMultiTableReaderProviderFormats()
	want := []FormatType{"alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListMultiTableReaderProviderFormats() = %#v, want %#v", got, want)
	}
}

func TestListMultiTableWriterProviderFormatsSorted(t *testing.T) {
	registry := NewProviderRegistry()
	if err := registry.RegisterMultiTableWriterProvider(providerTestMultiTableWriterProvider{formatType: FormatType("zeta")}); err != nil {
		t.Fatalf("RegisterMultiTableWriterProvider(zeta) error = %v", err)
	}
	if err := registry.RegisterMultiTableWriterProvider(providerTestMultiTableWriterProvider{formatType: FormatType("alpha")}); err != nil {
		t.Fatalf("RegisterMultiTableWriterProvider(alpha) error = %v", err)
	}

	got := registry.ListMultiTableWriterProviderFormats()
	want := []FormatType{"alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListMultiTableWriterProviderFormats() = %#v, want %#v", got, want)
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
					Name:      "Sheet1",
					ChildKind: "sheet",
					DataType:  FormatDataTypeTable,
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
		func(_ context.Context, parent contentio.Reader, parentRef contentio.Ref, child ContainerChildInfo, _ *ParseOptions) (*ContainerChildResource, error) {
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

func TestNewTableInfoProviderHandlesMissingImplementation(t *testing.T) {
	provider := NewTableInfoProvider(FormatType("empty"), nil)

	if _, err := provider.DescribeTable(context.Background(), strings.NewReader(""), nil); err == nil {
		t.Fatalf("DescribeTable() error = nil, want error")
	}
}

func TestNewTableSampleReaderHandlesMissingImplementation(t *testing.T) {
	reader := NewTableSampleReader(FormatType("empty"), nil)

	if _, err := reader.SampleTable(context.Background(), strings.NewReader(""), 0, 1, nil); err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("SampleTable() error = %v, want implementation error", err)
	}
}

func TestRegisterDocumentInfoProviderAndTextReader(t *testing.T) {
	registry := NewProviderRegistry()
	infoProvider := NewDocumentInfoProvider(
		FormatType("doc_test"),
		func(context.Context, io.Reader, *ParseOptions) (*datatype.DocumentInfo, error) {
			return &datatype.DocumentInfo{Encoding: "utf-8"}, nil
		},
	)
	textReader := NewDocumentTextReader(
		FormatType("doc_test"),
		func(context.Context, io.Reader, int64, *ParseOptions) (string, bool, error) {
			return "hello", false, nil
		},
	)

	if err := registry.RegisterDocumentInfoProvider(infoProvider); err != nil {
		t.Fatalf("RegisterDocumentInfoProvider() error = %v", err)
	}
	if err := registry.RegisterDocumentTextReader(textReader); err != nil {
		t.Fatalf("RegisterDocumentTextReader() error = %v", err)
	}

	got, err := registry.GetDocumentInfoProvider(FormatType("doc_test"))
	if err != nil {
		t.Fatalf("GetDocumentInfoProvider() error = %v", err)
	}
	if got.Format() != FormatType("doc_test") {
		t.Fatalf("document info provider format = %q, want doc_test", got.Format())
	}
	reader, err := registry.GetDocumentTextReader(FormatType("doc_test"))
	if err != nil {
		t.Fatalf("GetDocumentTextReader() error = %v", err)
	}
	if reader.Format() != FormatType("doc_test") {
		t.Fatalf("document text reader format = %q, want doc_test", reader.Format())
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

	if _, _, err := reader.ReadDocumentText(context.Background(), strings.NewReader(""), 1, nil); err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("ReadDocumentText() error = %v, want implementation error", err)
	}
}

func TestRegisterMediaInfoProvider(t *testing.T) {
	registry := NewProviderRegistry()
	provider := NewMediaInfoProvider(
		FormatType("media_test"),
		func(context.Context, io.Reader, *ParseOptions) (*MediaDescribeResult, error) {
			return &MediaDescribeResult{Media: &datatype.MediaInfo{Kind: datatype.MediaKindImage}}, nil
		},
	)

	if err := registry.RegisterMediaInfoProvider(provider); err != nil {
		t.Fatalf("RegisterMediaInfoProvider() error = %v", err)
	}

	got, err := registry.GetMediaInfoProvider(FormatType("media_test"))
	if err != nil {
		t.Fatalf("GetMediaInfoProvider() error = %v", err)
	}
	if got.Format() != FormatType("media_test") {
		t.Fatalf("provider format = %q, want media_test", got.Format())
	}
}

func TestListMediaInfoProviderFormatsSorted(t *testing.T) {
	registry := NewProviderRegistry()
	provider := NewMediaInfoProvider(FormatType("zeta"), nil)
	if err := registry.RegisterMediaInfoProvider(provider); err != nil {
		t.Fatalf("RegisterMediaInfoProvider(zeta) error = %v", err)
	}
	provider = NewMediaInfoProvider(FormatType("alpha"), nil)
	if err := registry.RegisterMediaInfoProvider(provider); err != nil {
		t.Fatalf("RegisterMediaInfoProvider(alpha) error = %v", err)
	}

	got := registry.ListMediaInfoProviderFormats()
	want := []FormatType{"alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListMediaInfoProviderFormats() = %#v, want %#v", got, want)
	}
}

func TestNewMediaInfoProviderHandlesMissingImplementation(t *testing.T) {
	provider := NewMediaInfoProvider(FormatType("empty_media"), nil)

	if _, err := provider.DescribeMedia(context.Background(), strings.NewReader(""), nil); err == nil {
		t.Fatalf("DescribeMedia() error = nil, want error")
	}
}
