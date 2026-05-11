package service

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/addp/common/format"
	"github.com/addp/common/resource"
)

func TestFileTablePreviewProviderResolveFormatUsesMetaFormat(t *testing.T) {
	provider := &FileTablePreviewProvider{}
	req := &PreviewRequest{
		Table: "fallback.bin",
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"format": "xlsx",
			},
		},
	}

	got := provider.resolveFormat(req)
	if got != format.FormatExcel {
		t.Fatalf("resolveFormat() = %q, want %q", got, format.FormatExcel)
	}
}

func TestFileTablePreviewProviderResolveFormatUsesContentType(t *testing.T) {
	provider := &FileTablePreviewProvider{}
	req := &PreviewRequest{
		Table: "fallback.bin",
		Attributes: map[string]interface{}{
			"storage": map[string]interface{}{
				"content_type": "application/geo+json",
			},
		},
	}

	got := provider.resolveFormat(req)
	if got != format.FormatJSON {
		t.Fatalf("resolveFormat() = %q, want %q", got, format.FormatJSON)
	}
}

func TestFileTablePreviewProviderResolveFormatDoesNotFallbackToFilename(t *testing.T) {
	provider := &FileTablePreviewProvider{}
	req := &PreviewRequest{
		Table: "roads.geojson",
	}

	got := provider.resolveFormat(req)
	if got != format.FormatUnknown {
		t.Fatalf("resolveFormat() = %q, want %q", got, format.FormatUnknown)
	}
}

func TestFileTablePreviewProviderBuildParseOptionsUsesResolvedFormat(t *testing.T) {
	provider := &FileTablePreviewProvider{}

	if got := provider.buildParseOptions(format.FormatTSV).Delimiter; got != '\t' {
		t.Fatalf("TSV delimiter = %q, want tab", got)
	}
	if got := provider.buildParseOptions(format.FormatCSV).Delimiter; got != ',' {
		t.Fatalf("CSV delimiter = %q, want comma", got)
	}
}

func TestFileTablePreviewProviderPreviewStreamableReturnsTableModeAndFirstPage(t *testing.T) {
	t.Parallel()

	provider := &FileTablePreviewProvider{}
	tableProvider := &recordingTableProvider{}
	req := &PreviewRequest{
		Page:     1,
		PageSize: 2,
		Table:    "manager/test.parquet",
	}

	preview, err := provider.previewStreamable(
		context.Background(),
		staticResourceReader{content: []byte("mock")},
		"manager",
		"manager/test.parquet",
		format.FormatParquet,
		tableProvider,
		nil,
		req,
	)
	if err != nil {
		t.Fatalf("previewStreamable() error = %v", err)
	}
	if preview.Mode != PreviewModeTable {
		t.Fatalf("Mode = %q, want %q", preview.Mode, PreviewModeTable)
	}
	if tableProvider.sampleOffset != 0 {
		t.Fatalf("sample offset = %d, want 0 for first page", tableProvider.sampleOffset)
	}
	if preview.Page != 1 {
		t.Fatalf("Page = %d, want 1", preview.Page)
	}
	if len(preview.Rows) != 1 || preview.Rows[0]["name"] != "first" {
		t.Fatalf("Rows = %#v, want first row", preview.Rows)
	}
}

func TestFileTablePreviewProviderUsesAttributesTableInfo(t *testing.T) {
	t.Parallel()

	provider := &FileTablePreviewProvider{}
	tableProvider := &recordingTableProvider{}
	req := &PreviewRequest{
		Page:     2,
		PageSize: 2,
		Table:    "manager/test.csv",
		Attributes: map[string]interface{}{
			"type_info": map[string]interface{}{
				"table": map[string]interface{}{
					"row_count": int64(5),
					"fields": []interface{}{
						map[string]interface{}{"name": "name", "type": string(format.FieldTypeString), "nullable": true},
					},
				},
			},
		},
	}

	preview, err := provider.previewStreamable(
		context.Background(),
		staticResourceReader{content: []byte("mock")},
		"manager",
		"manager/test.csv",
		format.FormatCSV,
		tableProvider,
		nil,
		req,
	)
	if err != nil {
		t.Fatalf("previewStreamable() error = %v", err)
	}
	if tableProvider.describeCalls != 0 {
		t.Fatalf("DescribeTable calls = %d, want 0", tableProvider.describeCalls)
	}
	if preview.Total != 5 {
		t.Fatalf("Total = %d, want 5", preview.Total)
	}
	if tableProvider.sampleOffset != 2 {
		t.Fatalf("sample offset = %d, want second page offset 2", tableProvider.sampleOffset)
	}
}

func TestFileTablePreviewProviderPreviewShapefileReturnsTableModeAndFirstPage(t *testing.T) {
	t.Parallel()

	provider := &FileTablePreviewProvider{}
	componentProvider := &recordingComponentTableProvider{}
	req := &PreviewRequest{
		Page:     1,
		PageSize: 2,
		Table:    "gis/roads.shp",
	}

	preview, err := provider.previewShapefile(
		context.Background(),
		emptyComponentReader{},
		"bucket",
		componentProvider,
		nil,
		req,
	)
	if err != nil {
		t.Fatalf("previewShapefile() error = %v", err)
	}
	if preview.Mode != PreviewModeTable {
		t.Fatalf("Mode = %q, want %q", preview.Mode, PreviewModeTable)
	}
	if componentProvider.sampleOffset != 0 {
		t.Fatalf("sample offset = %d, want 0 for first page", componentProvider.sampleOffset)
	}
	if preview.Page != 1 {
		t.Fatalf("Page = %d, want 1", preview.Page)
	}
}

type staticResourceReader struct {
	content []byte
}

func (r staticResourceReader) Open(context.Context, resource.ResourceRef) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(r.content)), nil
}

func (r staticResourceReader) Stat(context.Context, resource.ResourceRef) (*resource.ResourceMetadata, error) {
	return nil, nil
}

func (r staticResourceReader) List(context.Context, resource.ResourceRef) ([]resource.ResourceRef, error) {
	return nil, nil
}

type recordingTableProvider struct {
	sampleOffset  int64
	describeCalls int
}

func (p *recordingTableProvider) Format() format.FormatType {
	return format.FormatParquet
}

func (p *recordingTableProvider) Capabilities() format.FormatCapability {
	return format.FormatCapability{}
}

func (p *recordingTableProvider) DescribeTable(context.Context, io.Reader, *format.ParseOptions) (*format.TableInfo, error) {
	p.describeCalls++
	rowCount := int64(1)
	return &format.TableInfo{
		Fields:   []format.FieldInfo{{Name: "name", Type: format.FieldTypeString}},
		RowCount: &rowCount,
	}, nil
}

func (p *recordingTableProvider) SampleTable(_ context.Context, _ io.Reader, offset, _ int64, _ *format.ParseOptions) ([]map[string]interface{}, error) {
	p.sampleOffset = offset
	return []map[string]interface{}{{"name": "first"}}, nil
}

type recordingComponentTableProvider struct {
	recordingTableProvider
	sampleOffset int64
}

func (p *recordingComponentTableProvider) Format() format.FormatType {
	return format.FormatShapefile
}

func (p *recordingComponentTableProvider) DescribeTableComponents(context.Context, resource.ComponentReader, *format.ParseOptions) (*format.TableInfo, error) {
	rowCount := int64(1)
	return &format.TableInfo{
		Fields:   []format.FieldInfo{{Name: "name", Type: format.FieldTypeString}},
		RowCount: &rowCount,
	}, nil
}

func (p *recordingComponentTableProvider) SampleTableComponents(_ context.Context, _ resource.ComponentReader, offset, _ int64, _ *format.ParseOptions) ([]map[string]interface{}, error) {
	p.sampleOffset = offset
	return []map[string]interface{}{{"name": "first"}}, nil
}

type emptyComponentReader struct{}

func (emptyComponentReader) Components() []resource.ComponentRef {
	return nil
}

func (emptyComponentReader) OpenComponent(context.Context, resource.ComponentRef) (io.ReadCloser, error) {
	return nil, resource.ErrResourceNotFound
}

func (emptyComponentReader) OpenComponentRole(context.Context, string) (io.ReadCloser, error) {
	return nil, resource.ErrResourceNotFound
}

var _ format.TableProvider = (*recordingTableProvider)(nil)
var _ format.ComponentTableProvider = (*recordingComponentTableProvider)(nil)
var _ resource.ResourceReader = staticResourceReader{}
var _ resource.ComponentReader = emptyComponentReader{}
