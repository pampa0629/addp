package preview

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/objectcontent"
	"github.com/xuri/excelize/v2"
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

func TestFileTablePreviewProviderResolveFormatUnknownMetaFormat(t *testing.T) {
	provider := &FileTablePreviewProvider{}
	req := &PreviewRequest{
		Table: "fallback.bin",
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"format": "yml",
			},
		},
	}

	got := provider.resolveFormat(req)
	if got != format.FormatUnknown {
		t.Fatalf("resolveFormat() = %q, want %q", got, format.FormatUnknown)
	}
}

func TestPreviewHintUsesContainerMaterialForContainerDataType(t *testing.T) {
	t.Parallel()

	hint := inferPreviewHint(previewHintInput{
		Name:     "sample.sqlite",
		Format:   format.FormatSQLite,
		DataType: string(datatype.Container),
	})

	if hint.Material != "container" {
		t.Fatalf("Material = %q, want container", hint.Material)
	}
	if hint.Renderer != "container" {
		t.Fatalf("Renderer = %q, want container", hint.Renderer)
	}
	if !hint.Previewable {
		t.Fatalf("Previewable = false, want true")
	}
}

func TestPreviewHintSemanticMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		input           previewHintInput
		wantMaterial    string
		wantRenderer    string
		wantPreviewable bool
		wantTextLike    bool
	}{
		{
			name: "known_pdf_document",
			input: previewHintInput{
				Name:     "report.pdf",
				Format:   format.FormatPDF,
				DataType: string(datatype.Document),
			},
			wantMaterial:    "raw_binary",
			wantRenderer:    "pdf",
			wantPreviewable: true,
		},
		{
			name: "unknown_document_is_unsupported",
			input: previewHintInput{
				Name:     "book.epub",
				Format:   format.FormatUnknown,
				DataType: string(datatype.Document),
			},
			wantMaterial:    "unsupported",
			wantRenderer:    "unsupported",
			wantPreviewable: false,
		},
		{
			name: "unknown_media_is_unsupported",
			input: previewHintInput{
				Name:     "clip.bin",
				Format:   format.FormatUnknown,
				DataType: string(datatype.Media),
			},
			wantMaterial:    "unsupported",
			wantRenderer:    "unsupported",
			wantPreviewable: false,
		},
		{
			name: "known_video",
			input: previewHintInput{
				Name:     "clip.mp4",
				Format:   format.FormatMP4,
				DataType: string(datatype.Media),
			},
			wantMaterial:    "raw_binary",
			wantRenderer:    "video",
			wantPreviewable: true,
		},
		{
			name: "text_peek",
			input: previewHintInput{
				Name: "README",
				Peek: []byte("hello\nworld\n"),
			},
			wantMaterial:    "text",
			wantRenderer:    "text",
			wantPreviewable: true,
			wantTextLike:    true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			hint := inferPreviewHint(tt.input)
			if hint.Material != tt.wantMaterial {
				t.Fatalf("Material = %q, want %q", hint.Material, tt.wantMaterial)
			}
			if hint.Renderer != tt.wantRenderer {
				t.Fatalf("Renderer = %q, want %q", hint.Renderer, tt.wantRenderer)
			}
			if hint.Previewable != tt.wantPreviewable {
				t.Fatalf("Previewable = %v, want %v", hint.Previewable, tt.wantPreviewable)
			}
			if hint.TextLike != tt.wantTextLike {
				t.Fatalf("TextLike = %v, want %v", hint.TextLike, tt.wantTextLike)
			}
		})
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
	if got != format.FormatGeoJSON {
		t.Fatalf("resolveFormat() = %q, want %q", got, format.FormatGeoJSON)
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

	if got := provider.buildParseOptions(format.FormatTSV).Delimiter; got != 0 {
		t.Fatalf("delimiter = %q, want zero value so format plugin owns delimiter semantics", got)
	}
}

func TestFileTablePreviewProviderBuildParseOptionsUsesExcelChildSheet(t *testing.T) {
	provider := &FileTablePreviewProvider{}

	opts := provider.buildParseOptions(format.FormatExcel, &PreviewRequest{ChildName: " Cities "})
	if opts.SheetName != "Cities" {
		t.Fatalf("SheetName = %q, want Cities", opts.SheetName)
	}
}

func TestFileTablePreviewProviderBuildParseOptionsUsesSQLiteChildTable(t *testing.T) {
	provider := &FileTablePreviewProvider{}

	opts := provider.buildParseOptions(format.FormatSQLite, &PreviewRequest{
		ChildName: "Cities",
		Attributes: map[string]interface{}{
			"type_info": map[string]interface{}{
				"container": map[string]interface{}{
					"children": []interface{}{
						map[string]interface{}{
							"name":  "Cities",
							"table": "city_table",
						},
					},
				},
			},
		},
	})

	if opts.ExtraParams == nil {
		t.Fatal("ExtraParams is nil, want sqlite table option")
	}
	if got := opts.ExtraParams[format.ChildTableParam]; got != "city_table" {
		t.Fatalf("child table option = %#v, want city_table", got)
	}
}

func TestFileTablePreviewProviderBuildParseOptionsUsesGeoPackageChildTable(t *testing.T) {
	provider := &FileTablePreviewProvider{}

	opts := provider.buildParseOptions(format.FormatGeoPackage, &PreviewRequest{
		ChildName: "Road Layer",
		Attributes: map[string]interface{}{
			"type_info": map[string]interface{}{
				"container": map[string]interface{}{
					"children": []interface{}{
						map[string]interface{}{
							"name":       "Road Layer",
							"table":      "roads",
							"child_kind": "layer",
						},
					},
				},
			},
		},
	})

	if opts.ExtraParams == nil {
		t.Fatal("ExtraParams is nil, want geopackage table option")
	}
	if got := opts.ExtraParams[format.ChildTableParam]; got != "roads" {
		t.Fatalf("child table option = %#v, want roads", got)
	}
}

func TestFileTablePreviewProviderContentContextUsesFileCatalogReader(t *testing.T) {
	previous, previousErr := plugin.Get("nfs")
	enginePlugin := &recordingContentPlugin{engineType: "nfs"}
	plugin.Register(enginePlugin)
	defer func() {
		if previousErr == nil {
			plugin.Register(previous)
			return
		}
		plugin.Unregister(enginePlugin.Type())
	}()

	provider := &FileTablePreviewProvider{}
	req := &PreviewRequest{
		Engine:       &models.Engine{EngineType: enginePlugin.Type(), ID: 7},
		ItemType:     "file",
		Schema:       "gis-data",
		Table:        "sample.csv",
		PhysicalPath: "/gis-data/sample.csv",
		ProviderPath: plugin.FileItemPath(7, "gis-data/sample.csv"),
	}

	contentCtx, err := provider.contentContextForPreview(req)
	if err != nil {
		t.Fatalf("contentContextForPreview() error = %v", err)
	}
	if _, ok := contentCtx.reader.(*fileCatalogContentReader); !ok {
		t.Fatalf("reader = %T, want *fileCatalogContentReader", contentCtx.reader)
	}
	if contentCtx.path != "gis-data/sample.csv" {
		t.Fatalf("path = %q, want gis-data/sample.csv", contentCtx.path)
	}

	rc, err := contentCtx.reader.Open(context.Background(), contentio.NewRef(contentCtx.path, contentio.RoleMain))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	rc.Close()
	if got := enginePlugin.openedPath.StringPath(); got != "gis-data/sample.csv" {
		t.Fatalf("opened path = %q, want gis-data/sample.csv", got)
	}

	rangeReader, ok := contentCtx.reader.(contentio.RangeReader)
	if !ok {
		t.Fatalf("reader = %T, want contentio.RangeReader", contentCtx.reader)
	}
	rc, err = rangeReader.OpenRange(context.Background(), contentio.NewRef(contentCtx.path, contentio.RoleMain), 10, 20)
	if err != nil {
		t.Fatalf("OpenRange() error = %v", err)
	}
	rc.Close()
	if got := enginePlugin.rangeOpenedPath.StringPath(); got != "gis-data/sample.csv" {
		t.Fatalf("range opened path = %q, want gis-data/sample.csv", got)
	}
	if enginePlugin.rangeOptions.Offset != 10 || enginePlugin.rangeOptions.Length != 20 {
		t.Fatalf("range options = %+v, want offset 10 length 20", enginePlugin.rangeOptions)
	}
}

func TestObjectCatalogContentReaderStripsBucketPrefixFromRefPath(t *testing.T) {
	t.Parallel()

	enginePlugin := &recordingContentPlugin{engineType: "minio-preview-ref"}
	reader := newObjectCatalogContentReader(enginePlugin, nil, nil, 9, "addp")

	rc, err := reader.Open(context.Background(), contentio.NewRef("addp/gis/规划用地.dbf", contentio.RoleAuxiliary))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	rc.Close()
	if got := enginePlugin.openedPath.StringPath(); got != "addp/gis/规划用地.dbf" {
		t.Fatalf("opened path = %q, want catalog path with bucket plus object key", got)
	}
	segments := plugin.CatalogPathWithoutRoot(enginePlugin.openedPath).Segments
	if len(segments) != 3 {
		t.Fatalf("segments = %#v, want bucket + prefix + object", enginePlugin.openedPath.Segments)
	}
	objectSegment := segments[2]
	if objectSegment.Name != "规划用地.dbf" {
		t.Fatalf("object segment = %#v, want key without duplicated bucket", objectSegment)
	}
}

func TestObjectCatalogContentReaderOpenRangeStripsBucketPrefixFromRefPath(t *testing.T) {
	t.Parallel()

	enginePlugin := &recordingContentPlugin{engineType: "minio-preview-ref-range"}
	reader := newObjectCatalogContentReader(enginePlugin, nil, nil, 9, "addp")

	rc, err := reader.OpenRange(context.Background(), contentio.NewRef("addp/gis/规划用地.shx", contentio.RoleAuxiliary), 100, 8)
	if err != nil {
		t.Fatalf("OpenRange() error = %v", err)
	}
	rc.Close()
	if got := enginePlugin.rangeOpenedPath.StringPath(); got != "addp/gis/规划用地.shx" {
		t.Fatalf("range opened path = %q, want catalog path with bucket plus object key", got)
	}
	segments := plugin.CatalogPathWithoutRoot(enginePlugin.rangeOpenedPath).Segments
	if len(segments) != 3 {
		t.Fatalf("segments = %#v, want bucket + prefix + object", enginePlugin.rangeOpenedPath.Segments)
	}
	objectSegment := segments[2]
	if objectSegment.Name != "规划用地.shx" {
		t.Fatalf("object segment = %#v, want key without duplicated bucket", objectSegment)
	}
}

func TestFileTablePreviewProviderPreviewTSVWithRegisteredProvider(t *testing.T) {
	t.Parallel()

	provider := &FileTablePreviewProvider{}
	req := &PreviewRequest{
		Page:     1,
		PageSize: 1,
		Table:    "manager/test.tsv",
		Attributes: map[string]interface{}{
			"type_info": map[string]interface{}{
				"table": map[string]interface{}{
					"row_count": int64(2),
					"fields": []interface{}{
						map[string]interface{}{"name": "name", "type": string(datatype.FieldTypeString), "nullable": true},
						map[string]interface{}{"name": "age", "type": string(datatype.FieldTypeInt), "nullable": true},
					},
				},
			},
		},
	}
	tableProvider, err := format.GetTableSampleReader(format.FormatTSV)
	if err != nil {
		t.Fatalf("GetTableSampleReader(tsv) failed: %v", err)
	}

	preview, err := provider.previewStreamable(
		context.Background(),
		staticContentReader{content: []byte("name\tage\nAlice\t25\nBob\t30\n")},
		"manager",
		"manager/test.tsv",
		format.FormatTSV,
		nil,
		tableProvider,
		provider.buildParseOptions(format.FormatTSV),
		req,
	)
	if err != nil {
		t.Fatalf("previewStreamable() error = %v", err)
	}
	if preview.Rows[0]["name"] != "Alice" {
		t.Fatalf("rows = %#v, want Alice", preview.Rows)
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
		staticContentReader{content: []byte("mock")},
		"manager",
		"manager/test.parquet",
		format.FormatParquet,
		tableProvider,
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
						map[string]interface{}{"name": "name", "type": string(datatype.FieldTypeString), "nullable": true},
					},
				},
			},
		},
	}

	preview, err := provider.previewStreamable(
		context.Background(),
		staticContentReader{content: []byte("mock")},
		"manager",
		"manager/test.csv",
		format.FormatCSV,
		tableProvider,
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

func TestFileTablePreviewProviderDoesNotUseContainerChildAttributesAsTableInfo(t *testing.T) {
	t.Parallel()

	provider := &FileTablePreviewProvider{}
	tableProvider := &recordingTableProvider{}
	req := &PreviewRequest{
		Page:      1,
		PageSize:  2,
		Table:     "manager/sample.db",
		ChildName: "Cities",
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"format": "sqlite",
			},
			"type_info": map[string]interface{}{
				"container": map[string]interface{}{
					"children": []interface{}{
						map[string]interface{}{
							"name":       "Cities",
							"table":      "city_table",
							"child_kind": "table",
							"row_count":  int64(7),
							"has_header": true,
						},
					},
				},
			},
		},
	}

	preview, err := provider.previewStreamable(
		context.Background(),
		staticContentReader{content: []byte("mock")},
		"manager",
		"manager/sample.db",
		format.FormatSQLite,
		tableProvider,
		tableProvider,
		provider.buildParseOptions(format.FormatSQLite, req),
		req,
	)
	if err != nil {
		t.Fatalf("previewStreamable() error = %v", err)
	}
	if tableProvider.describeCalls != 1 {
		t.Fatalf("DescribeTable calls = %d, want 1 because container child attributes are only an index", tableProvider.describeCalls)
	}
	if tableProvider.sampleOptions == nil || tableProvider.sampleOptions.ExtraParams[format.ChildTableParam] != "city_table" {
		t.Fatalf("sqlite sample table option = %#v, want city_table", tableProvider.sampleOptions)
	}
	if preview.Total != 3 {
		t.Fatalf("Total = %d, want 3 from DescribeTable", preview.Total)
	}
	if len(preview.Columns) != 1 || preview.Columns[0] != "name" {
		t.Fatalf("Columns = %#v, want described columns", preview.Columns)
	}
}

func TestFileTablePreviewProviderDoesNotUseGeoPackageChildAttributesAsTableInfo(t *testing.T) {
	t.Parallel()

	provider := &FileTablePreviewProvider{}
	tableProvider := &recordingTableProvider{}
	req := &PreviewRequest{
		Page:      1,
		PageSize:  2,
		Table:     "manager/sample.gpkg",
		ChildName: "Road Layer",
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"format": "geopackage",
			},
			"type_info": map[string]interface{}{
				"container": map[string]interface{}{
					"children": []interface{}{
						map[string]interface{}{
							"name":       "Road Layer",
							"table":      "roads",
							"child_kind": "layer",
							"row_count":  int64(99),
							"columns": []interface{}{
								map[string]interface{}{"name": "stale_name", "type": "string"},
							},
						},
					},
				},
			},
		},
	}

	preview, err := provider.previewStreamable(
		context.Background(),
		staticContentReader{content: []byte("mock")},
		"manager",
		"manager/sample.gpkg",
		format.FormatGeoPackage,
		tableProvider,
		tableProvider,
		provider.buildParseOptions(format.FormatGeoPackage, req),
		req,
	)
	if err != nil {
		t.Fatalf("previewStreamable() error = %v", err)
	}
	if tableProvider.describeCalls != 1 {
		t.Fatalf("DescribeTable calls = %d, want 1 because container child attributes are only an index", tableProvider.describeCalls)
	}
	if tableProvider.sampleOptions == nil || tableProvider.sampleOptions.ExtraParams[format.ChildTableParam] != "roads" {
		t.Fatalf("geopackage sample table option = %#v, want roads", tableProvider.sampleOptions)
	}
	if preview.Total != 3 {
		t.Fatalf("Total = %d, want 3 from DescribeTable", preview.Total)
	}
	if len(preview.Columns) != 1 || preview.Columns[0] != "name" {
		t.Fatalf("Columns = %#v, want described columns", preview.Columns)
	}
}

func TestFileTablePreviewProviderResolvesZIPEntryBeforeTablePreview(t *testing.T) {
	previous, previousErr := plugin.Get("nfs")
	enginePlugin := &recordingContentPlugin{engineType: "nfs"}
	plugin.Register(enginePlugin)
	defer func() {
		if previousErr == nil {
			plugin.Register(previous)
			return
		}
		plugin.Unregister(enginePlugin.Type())
	}()

	enginePlugin.content = zipBytesForFilePreviewTest(t, map[string]string{
		"data/cities.csv": "id,name\n1,Hangzhou\n2,Shanghai\n",
	})
	provider := &FileTablePreviewProvider{}
	req := &PreviewRequest{
		Engine:       &models.Engine{EngineType: enginePlugin.Type(), ID: 7},
		ItemType:     "file",
		Schema:       "datasets",
		Table:        "outer.zip",
		PhysicalPath: "/datasets/outer.zip",
		ChildName:    "data/cities.csv",
		Page:         1,
		PageSize:     10,
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"format":    string(format.FormatZIP),
				"data_type": "container",
			},
			"type_info": map[string]interface{}{
				"container": map[string]interface{}{
					"children": []interface{}{
						map[string]interface{}{
							"name":       "data/cities.csv",
							"child_kind": "file",
							"data_type":  "table",
							"format":     string(format.FormatCSV),
							"path":       "data/cities.csv",
						},
					},
				},
			},
		},
	}

	preview, err := provider.Preview(context.Background(), req)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if !reflect.DeepEqual(preview.Columns, []string{"id", "name"}) {
		t.Fatalf("Columns = %#v, want id/name", preview.Columns)
	}
	if len(preview.Rows) != 2 || preview.Rows[0]["name"] != "Hangzhou" {
		t.Fatalf("Rows = %#v, want cities rows", preview.Rows)
	}
}

func TestContainerChildPreviewProviderResolvesZIPTextEntry(t *testing.T) {
	previous, previousErr := plugin.Get("nfs")
	enginePlugin := &recordingContentPlugin{engineType: "nfs"}
	plugin.Register(enginePlugin)
	defer func() {
		if previousErr == nil {
			plugin.Register(previous)
			return
		}
		plugin.Unregister(enginePlugin.Type())
	}()

	enginePlugin.content = zipBytesForFilePreviewTest(t, map[string]string{
		"docs/readme.txt": "hello nested document",
	})
	provider := NewContainerChildPreviewProvider(objectcontent.NewObjectContentRegistry())
	objectcontent.LoadObjectContentPlugins(provider.(*ContainerChildPreviewProvider).content, "../../plugins")
	req := &PreviewRequest{
		Engine:       &models.Engine{EngineType: enginePlugin.Type(), ID: 7},
		ItemType:     "file",
		Schema:       "datasets",
		Table:        "outer.zip",
		PhysicalPath: "/datasets/outer.zip",
		ChildName:    "docs/readme.txt",
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"format":    string(format.FormatZIP),
				"data_type": "container",
			},
			"type_info": map[string]interface{}{
				"container": map[string]interface{}{
					"children": []interface{}{
						map[string]interface{}{
							"name":       "docs/readme.txt",
							"child_kind": "file",
							"data_type":  "document",
							"format":     string(format.FormatText),
							"path":       "docs/readme.txt",
						},
					},
				},
			},
		},
	}

	preview, err := provider.Preview(context.Background(), req)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if preview.Mode != PreviewModeObject || preview.Object == nil || preview.Object.Content == nil {
		t.Fatalf("preview = %#v, want object content", preview)
	}
	if preview.Object.Content.Kind != models.ObjectPreviewKindText || preview.Object.Content.Text != "hello nested document" {
		t.Fatalf("content = %#v, want text entry", preview.Object.Content)
	}
}

func TestContainerChildPreviewProviderDetectsUnknownZIPTextEntryForPreviewOnly(t *testing.T) {
	previous, previousErr := plugin.Get("nfs")
	enginePlugin := &recordingContentPlugin{engineType: "nfs"}
	plugin.Register(enginePlugin)
	defer func() {
		if previousErr == nil {
			plugin.Register(previous)
			return
		}
		plugin.Unregister(enginePlugin.Type())
	}()

	enginePlugin.content = zipBytesForFilePreviewTest(t, map[string]string{
		"config/docker-compose.yml": "services:\n  app:\n    image: alpine\n",
	})
	provider := NewContainerChildPreviewProvider(objectcontent.NewObjectContentRegistry())
	objectcontent.LoadObjectContentPlugins(provider.(*ContainerChildPreviewProvider).content, "../../plugins")
	req := &PreviewRequest{
		Engine:       &models.Engine{EngineType: enginePlugin.Type(), ID: 7},
		ItemType:     "file",
		Schema:       "datasets",
		Table:        "outer.zip",
		PhysicalPath: "/datasets/outer.zip",
		ChildName:    "config/docker-compose.yml",
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"format":    string(format.FormatZIP),
				"data_type": "container",
			},
			"type_info": map[string]interface{}{
				"container": map[string]interface{}{
					"children": []interface{}{
						map[string]interface{}{
							"name":       "config/docker-compose.yml",
							"child_kind": "file",
							"data_type":  "unknown",
							"path":       "config/docker-compose.yml",
						},
					},
				},
			},
		},
	}

	preview, err := provider.Preview(context.Background(), req)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if preview.Mode != PreviewModeObject || preview.Object == nil || preview.Object.Content == nil {
		t.Fatalf("preview = %#v, want object content", preview)
	}
	if preview.Object.Content.Kind != models.ObjectPreviewKindText {
		t.Fatalf("content = %#v, want text entry", preview.Object.Content)
	}
	if preview.Object.Content.Text != "services:\n  app:\n    image: alpine\n" {
		t.Fatalf("text = %q", preview.Object.Content.Text)
	}
	itemAttrs := preview.Object.Attributes["item"].(map[string]interface{})
	if itemAttrs["format"] == string(format.FormatText) {
		t.Fatalf("child attributes were promoted to text: %#v", itemAttrs)
	}
}

func TestContainerChildPreviewProviderResolvesZIPChinesePDFEntry(t *testing.T) {
	previous, previousErr := plugin.Get("nfs")
	enginePlugin := &recordingContentPlugin{engineType: "nfs"}
	plugin.Register(enginePlugin)
	defer func() {
		if previousErr == nil {
			plugin.Register(previous)
			return
		}
		plugin.Unregister(enginePlugin.Type())
	}()

	childName := "曾志明-测绘正高职称证书.pdf"
	enginePlugin.content = zipBytesRawForFilePreviewTest(t, map[string][]byte{
		childName: []byte("%PDF-1.4\n"),
	})
	provider := NewContainerChildPreviewProvider(objectcontent.NewObjectContentRegistry())
	objectcontent.LoadObjectContentPlugins(provider.(*ContainerChildPreviewProvider).content, "../../plugins")
	req := &PreviewRequest{
		Engine:       &models.Engine{EngineType: enginePlugin.Type(), ID: 7},
		ItemType:     "file",
		Schema:       "datasets",
		Table:        "outer.zip",
		PhysicalPath: "/datasets/outer.zip",
		ChildName:    childName,
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"format":    string(format.FormatZIP),
				"data_type": "container",
			},
			"type_info": map[string]interface{}{
				"container": map[string]interface{}{
					"children": []interface{}{
						map[string]interface{}{
							"name":         childName,
							"child_kind":   "file",
							"data_type":    "document",
							"format":       string(format.FormatPDF),
							"path":         childName,
							"content_type": "application/pdf",
						},
					},
				},
			},
		},
	}

	preview, err := provider.Preview(context.Background(), req)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if preview.Mode != PreviewModeObject || preview.Object == nil || preview.Object.Content == nil {
		t.Fatalf("preview = %#v, want object content", preview)
	}
	if preview.Object.Content.Kind != models.ObjectPreviewKindPDF {
		t.Fatalf("content = %#v, want PDF entry", preview.Object.Content)
	}
}

func TestContainerChildPreviewProviderPreviewsZIPMultiTableChildRefs(t *testing.T) {
	previousEngine, previousEngineErr := plugin.Get("nfs")
	enginePlugin := &recordingContentPlugin{engineType: "nfs"}
	plugin.Register(enginePlugin)
	defer func() {
		if previousEngineErr == nil {
			plugin.Register(previousEngine)
			return
		}
		plugin.Unregister(enginePlugin.Type())
	}()

	enginePlugin.content = zipBytesRawForFilePreviewTest(t, map[string][]byte{
		"roads.shp": []byte("shape"),
		"roads.shx": []byte("index"),
		"roads.dbf": []byte("attrs"),
	})
	refProvider := &recordingMultiTableProvider{}
	registerPreviewTestFormatPlugin(t, refProvider)
	provider := NewContainerChildPreviewProvider(objectcontent.NewObjectContentRegistry())
	req := &PreviewRequest{
		Engine:       &models.Engine{EngineType: enginePlugin.Type(), ID: 7},
		ItemType:     "file",
		Schema:       "datasets",
		Table:        "outer.zip",
		PhysicalPath: "/datasets/outer.zip",
		ChildName:    "roads.shp",
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"format":    string(format.FormatZIP),
				"data_type": "container",
			},
			"type_info": map[string]interface{}{
				"container": map[string]interface{}{
					"children": []interface{}{
						map[string]interface{}{
							"name":       "roads.shp",
							"child_kind": "multi",
							"data_type":  "table",
							"format":     string(format.FormatShapefile),
							"layout":     "multi",
							"path":       "roads.shp",
							"refs": []interface{}{
								map[string]interface{}{"path": "roads.shp", "role": "main", "required": true, "primary": true, "extension": ".shp"},
								map[string]interface{}{"path": "roads.shx", "role": "index", "required": true, "extension": ".shx"},
								map[string]interface{}{"path": "roads.dbf", "role": "attributes", "required": true, "extension": ".dbf"},
							},
						},
					},
				},
			},
		},
	}

	preview, err := provider.Preview(context.Background(), req)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if preview.Mode != PreviewModeTable {
		t.Fatalf("mode = %q, want table preview: %#v", preview.Mode, preview.Object)
	}
	if len(preview.Columns) == 0 || len(preview.Rows) == 0 {
		t.Fatalf("preview columns/rows = %#v/%#v, want table data", preview.Columns, preview.Rows)
	}
	if refProvider.describeCalls != 1 {
		t.Fatalf("DescribeMultiTable calls = %d, want 1", refProvider.describeCalls)
	}
}

func TestContainerChildPreviewProviderPreviewsExcelSheet(t *testing.T) {
	previous, previousErr := plugin.Get("nfs")
	enginePlugin := &recordingContentPlugin{engineType: "nfs", content: excelWorkbookBytesForPreviewTest(t)}
	plugin.Register(enginePlugin)
	defer func() {
		if previousErr == nil {
			plugin.Register(previous)
			return
		}
		plugin.Unregister(enginePlugin.Type())
	}()

	provider := NewContainerChildPreviewProvider(objectcontent.NewObjectContentRegistry())
	req := &PreviewRequest{
		Engine:       &models.Engine{EngineType: enginePlugin.Type(), ID: 7},
		ItemType:     "file",
		Schema:       "datasets",
		Table:        "cities.xlsx",
		PhysicalPath: "/datasets/cities.xlsx",
		ChildName:    "Cities",
		Page:         1,
		PageSize:     10,
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"format":    string(format.FormatExcel),
				"data_type": "container",
			},
			"type_info": map[string]interface{}{
				"container": map[string]interface{}{
					"children": []interface{}{
						map[string]interface{}{
							"name":         "Cities",
							"child_kind":   "sheet",
							"data_type":    "table",
							"row_count":    int64(2),
							"column_count": int64(2),
							"has_header":   true,
						},
					},
				},
			},
		},
	}

	preview, err := provider.Preview(context.Background(), req)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if preview.Mode != PreviewModeTable {
		t.Fatalf("Mode = %q, want %q", preview.Mode, PreviewModeTable)
	}
	if !reflect.DeepEqual(preview.Columns, []string{"id", "name"}) {
		t.Fatalf("Columns = %#v, want id/name", preview.Columns)
	}
	if len(preview.Rows) != 2 || preview.Rows[0]["name"] != "Hangzhou" {
		t.Fatalf("Rows = %#v, want Excel sheet rows", preview.Rows)
	}
}

func TestContainerChildPreviewProviderPreviewsSQLiteTable(t *testing.T) {
	previous, previousErr := plugin.Get("nfs")
	enginePlugin := &recordingContentPlugin{engineType: "nfs", content: sqliteDatabaseBytesForPreviewTest(t)}
	plugin.Register(enginePlugin)
	defer func() {
		if previousErr == nil {
			plugin.Register(previous)
			return
		}
		plugin.Unregister(enginePlugin.Type())
	}()

	provider := NewContainerChildPreviewProvider(objectcontent.NewObjectContentRegistry())
	req := &PreviewRequest{
		Engine:       &models.Engine{EngineType: enginePlugin.Type(), ID: 7},
		ItemType:     "file",
		Schema:       "datasets",
		Table:        "cities.sqlite",
		PhysicalPath: "/datasets/cities.sqlite",
		ChildName:    "Cities",
		Page:         1,
		PageSize:     10,
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"format":    string(format.FormatSQLite),
				"data_type": "container",
			},
			"type_info": map[string]interface{}{
				"container": map[string]interface{}{
					"children": []interface{}{
						map[string]interface{}{
							"name":         "Cities",
							"table":        "cities",
							"child_kind":   "table",
							"data_type":    "table",
							"row_count":    int64(2),
							"column_count": int64(2),
						},
					},
				},
			},
		},
	}

	preview, err := provider.Preview(context.Background(), req)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if preview.Mode != PreviewModeTable {
		t.Fatalf("Mode = %q, want %q", preview.Mode, PreviewModeTable)
	}
	if !reflect.DeepEqual(preview.Columns, []string{"id", "name"}) {
		t.Fatalf("Columns = %#v, want id/name", preview.Columns)
	}
	if len(preview.Rows) != 2 || preview.Rows[1]["name"] != "Shanghai" {
		t.Fatalf("Rows = %#v, want SQLite table rows", preview.Rows)
	}
}

func TestContainerChildPreviewProviderPreviewsGeoPackageLayerSpatialInfo(t *testing.T) {
	previous, previousErr := plugin.Get("nfs")
	enginePlugin := &recordingContentPlugin{engineType: "nfs", content: geoPackageBytesForPreviewTest(t)}
	plugin.Register(enginePlugin)
	defer func() {
		if previousErr == nil {
			plugin.Register(previous)
			return
		}
		plugin.Unregister(enginePlugin.Type())
	}()

	provider := NewContainerChildPreviewProvider(objectcontent.NewObjectContentRegistry())
	req := &PreviewRequest{
		Engine:       &models.Engine{EngineType: enginePlugin.Type(), ID: 7},
		ItemType:     "file",
		Schema:       "datasets",
		Table:        "roads.gpkg",
		PhysicalPath: "/datasets/roads.gpkg",
		ChildName:    "Road Layer",
		Page:         1,
		PageSize:     10,
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"format":    string(format.FormatGeoPackage),
				"data_type": "container",
			},
			"type_info": map[string]interface{}{
				"container": map[string]interface{}{
					"children": []interface{}{
						map[string]interface{}{
							"name":         "Road Layer",
							"table":        "roads",
							"child_kind":   "layer",
							"data_type":    "table",
							"row_count":    int64(1),
							"column_count": int64(3),
						},
					},
				},
			},
		},
	}

	preview, err := provider.Preview(context.Background(), req)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if preview.Mode != PreviewModeTable {
		t.Fatalf("Mode = %q, want %q", preview.Mode, PreviewModeTable)
	}
	if !reflect.DeepEqual(preview.Columns, []string{"id", "geom", "name"}) {
		t.Fatalf("Columns = %#v, want id/geom/name", preview.Columns)
	}
	if len(preview.GeometryColumns) != 1 || preview.GeometryColumns[0] != "geom" || preview.SRID != 4326 {
		t.Fatalf("spatial = columns %#v srid %d, want geom EPSG:4326", preview.GeometryColumns, preview.SRID)
	}
}

func TestFileTablePreviewProviderMultiPreviewKeepsNFSFullStorageRef(t *testing.T) {
	previousEngine, previousEngineErr := plugin.Get("nfs")
	enginePlugin := &recordingContentPlugin{engineType: "nfs"}
	plugin.Register(enginePlugin)
	defer func() {
		if previousEngineErr == nil {
			plugin.Register(previousEngine)
			return
		}
		plugin.Unregister(enginePlugin.Type())
	}()

	refProvider := &recordingMultiTableProvider{}
	registerPreviewTestFormatPlugin(t, refProvider)

	provider := &FileTablePreviewProvider{}
	req := &PreviewRequest{
		Engine:       &models.Engine{EngineType: enginePlugin.Type(), ID: 26},
		ItemType:     "file",
		Schema:       "shp",
		Table:        "farmland.shp",
		PhysicalPath: "shp/farmland.shp",
		Page:         1,
		PageSize:     20,
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"format":    string(format.FormatShapefile),
				"data_type": "table",
				"layout":    "multi",
				"refs": []interface{}{
					map[string]interface{}{"path": "shp/farmland.shp", "role": "main", "required": true, "primary": true},
					map[string]interface{}{"path": "shp/farmland.shx", "role": "index", "required": true},
					map[string]interface{}{"path": "shp/farmland.dbf", "role": "attributes", "required": true},
				},
			},
		},
	}

	preview, err := provider.Preview(context.Background(), req)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if preview.Object == nil || preview.Object.Download == nil {
		t.Fatalf("preview object/download = %#v", preview.Object)
	}
	if preview.Object.Path != "shp/farmland.shp" || preview.Object.StorageRef != "shp/farmland.shp" {
		t.Fatalf("object path/storage_ref = %q/%q, want full NFS path", preview.Object.Path, preview.Object.StorageRef)
	}
	if got := preview.Object.Download.URL; !strings.Contains(got, "/downloads/file?locator=") || !strings.Contains(got, "path%2Fshp%2Ffarmland.shp") || !strings.Contains(got, "type%3Dfile") {
		t.Fatalf("download url = %q, want file locator with full NFS path", got)
	}
	if preview.Object.Download.FileName != "farmland.shapefile.zip" {
		t.Fatalf("download filename = %q, want farmland.shapefile.zip", preview.Object.Download.FileName)
	}
}

func TestFileTablePreviewProviderMultiPreviewKeepsObjectStorageRef(t *testing.T) {
	engineType := "preview_test_object_bundle"
	enginePlugin := &recordingContentPlugin{engineType: engineType}
	plugin.Register(enginePlugin)
	defer plugin.Unregister(engineType)

	refProvider := &recordingMultiTableProvider{}
	registerPreviewTestFormatPlugin(t, refProvider)

	provider := &FileTablePreviewProvider{}
	req := &PreviewRequest{
		Engine:       &models.Engine{EngineType: enginePlugin.Type(), ID: 9},
		ItemType:     "object",
		Schema:       "gischain",
		Table:        "data/farmland.shp",
		PhysicalPath: "gischain/data/farmland.shp",
		Page:         1,
		PageSize:     20,
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"format":    string(format.FormatShapefile),
				"data_type": "table",
				"layout":    "multi",
				"refs": []interface{}{
					map[string]interface{}{"path": "gischain/data/farmland.shp", "role": "main", "required": true, "primary": true},
					map[string]interface{}{"path": "gischain/data/farmland.shx", "role": "index", "required": true},
					map[string]interface{}{"path": "gischain/data/farmland.dbf", "role": "attributes", "required": true},
				},
			},
		},
	}

	preview, err := provider.Preview(context.Background(), req)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if preview.Object == nil || preview.Object.Download == nil {
		t.Fatalf("preview object/download = %#v", preview.Object)
	}
	if preview.Object.Path != "data/farmland.shp" || preview.Object.StorageRef != "gischain/data/farmland.shp" {
		t.Fatalf("object path/storage_ref = %q/%q, want object storage ref with bucket", preview.Object.Path, preview.Object.StorageRef)
	}
	if got := preview.Object.Download.URL; !strings.Contains(got, "/downloads/file?locator=") || !strings.Contains(got, "path%2Fgischain%2Fdata%2Ffarmland.shp") || !strings.Contains(got, "type%3Dobject") {
		t.Fatalf("download url = %q, want object locator with bucket path", got)
	}
	if preview.Object.Download.FileName != "farmland.shapefile.zip" {
		t.Fatalf("download filename = %q, want farmland.shapefile.zip", preview.Object.Download.FileName)
	}
}

func TestContainerChildPreviewProviderResolvesNestedZIPEntry(t *testing.T) {
	previous, previousErr := plugin.Get("nfs")
	enginePlugin := &recordingContentPlugin{engineType: "nfs"}
	plugin.Register(enginePlugin)
	defer func() {
		if previousErr == nil {
			plugin.Register(previous)
			return
		}
		plugin.Unregister(enginePlugin.Type())
	}()

	inner := zipBytesForFilePreviewTest(t, map[string]string{
		"data/cities.csv": "id,name\n1,Hangzhou\n",
	})
	enginePlugin.content = zipBytesRawForFilePreviewTest(t, map[string][]byte{
		"inner.zip": inner,
	})
	provider := NewContainerChildPreviewProvider(objectcontent.NewObjectContentRegistry())
	objectcontent.LoadObjectContentPlugins(provider.(*ContainerChildPreviewProvider).content, "../../plugins")
	req := &PreviewRequest{
		Engine:       &models.Engine{EngineType: enginePlugin.Type(), ID: 7},
		ItemType:     "file",
		Schema:       "datasets",
		Table:        "outer.zip",
		PhysicalPath: "/datasets/outer.zip",
		ChildName:    "inner.zip",
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"format":    string(format.FormatZIP),
				"data_type": "container",
			},
			"type_info": map[string]interface{}{
				"container": map[string]interface{}{
					"children": []interface{}{
						map[string]interface{}{
							"name":       "inner.zip",
							"child_kind": "file",
							"data_type":  "container",
							"format":     string(format.FormatZIP),
							"path":       "inner.zip",
						},
					},
				},
			},
		},
	}

	preview, err := provider.Preview(context.Background(), req)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if preview.Mode != PreviewModeObject || preview.Object == nil || preview.Object.Content == nil {
		t.Fatalf("preview = %#v, want object container content", preview)
	}
	content := preview.Object.Content
	if content.Kind != models.ObjectPreviewKindContainer {
		t.Fatalf("content kind = %q, want container", content.Kind)
	}
	if content.FrontendRenderer != models.ObjectPreviewKindContainer {
		t.Fatalf("frontend renderer = %q, want container", content.FrontendRenderer)
	}
	container, ok := content.JSON.(map[string]interface{})
	if !ok {
		t.Fatalf("container JSON = %#v", content.JSON)
	}
	children, ok := container["children"].([]map[string]interface{})
	if !ok || len(children) != 1 || children[0]["name"] != "data/cities.csv" {
		t.Fatalf("nested children = %#v", container["children"])
	}
}

func TestContainerChildPreviewProviderPreviewsNestedZIPEntryByNestedChildPath(t *testing.T) {
	previous, previousErr := plugin.Get("nfs")
	enginePlugin := &recordingContentPlugin{engineType: "nfs"}
	plugin.Register(enginePlugin)
	defer func() {
		if previousErr == nil {
			plugin.Register(previous)
			return
		}
		plugin.Unregister(enginePlugin.Type())
	}()

	inner := zipBytesForFilePreviewTest(t, map[string]string{
		"data/cities.csv": "id,name\n1,Hangzhou\n2,Shanghai\n",
	})
	enginePlugin.content = zipBytesRawForFilePreviewTest(t, map[string][]byte{
		"inner.zip": inner,
	})
	provider := NewContainerChildPreviewProvider(objectcontent.NewObjectContentRegistry())
	objectcontent.LoadObjectContentPlugins(provider.(*ContainerChildPreviewProvider).content, "../../plugins")
	req := &PreviewRequest{
		Engine:          &models.Engine{EngineType: enginePlugin.Type(), ID: 7},
		ItemType:        "file",
		Schema:          "datasets",
		Table:           "outer.zip",
		PhysicalPath:    "/datasets/outer.zip",
		ChildName:       "inner.zip",
		NestedChildPath: "data/cities.csv",
		Page:            1,
		PageSize:        10,
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"format":    string(format.FormatZIP),
				"data_type": "container",
			},
			"type_info": map[string]interface{}{
				"container": map[string]interface{}{
					"children": []interface{}{
						map[string]interface{}{
							"name":       "inner.zip",
							"child_kind": "file",
							"data_type":  "container",
							"format":     string(format.FormatZIP),
							"path":       "inner.zip",
						},
					},
				},
			},
		},
	}

	preview, err := provider.Preview(context.Background(), req)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if preview.Mode != PreviewModeTable {
		t.Fatalf("Mode = %q, want %q", preview.Mode, PreviewModeTable)
	}
	if !reflect.DeepEqual(preview.Columns, []string{"id", "name"}) {
		t.Fatalf("Columns = %#v, want id/name", preview.Columns)
	}
	if len(preview.Rows) != 2 || preview.Rows[0]["name"] != "Hangzhou" {
		t.Fatalf("Rows = %#v, want cities rows", preview.Rows)
	}
}

func TestContainerChildPreviewProviderPreviewsDeepNestedZIPEntryByNestedChildPath(t *testing.T) {
	previous, previousErr := plugin.Get("nfs")
	enginePlugin := &recordingContentPlugin{engineType: "nfs"}
	plugin.Register(enginePlugin)
	defer func() {
		if previousErr == nil {
			plugin.Register(previous)
			return
		}
		plugin.Unregister(enginePlugin.Type())
	}()

	middle := zipBytesForFilePreviewTest(t, map[string]string{
		"data/cities.csv": "id,name\n1,Hangzhou\n2,Shanghai\n",
	})
	inner := zipBytesRawForFilePreviewTest(t, map[string][]byte{
		"middle.zip": middle,
	})
	enginePlugin.content = zipBytesRawForFilePreviewTest(t, map[string][]byte{
		"inner.zip": inner,
	})
	provider := NewContainerChildPreviewProvider(objectcontent.NewObjectContentRegistry())
	objectcontent.LoadObjectContentPlugins(provider.(*ContainerChildPreviewProvider).content, "../../plugins")
	req := &PreviewRequest{
		Engine:          &models.Engine{EngineType: enginePlugin.Type(), ID: 7},
		ItemType:        "file",
		Schema:          "datasets",
		Table:           "outer.zip",
		PhysicalPath:    "/datasets/outer.zip",
		ChildName:       "inner.zip",
		NestedChildPath: "middle.zip/data/cities.csv",
		Page:            1,
		PageSize:        10,
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"format":    string(format.FormatZIP),
				"data_type": "container",
			},
			"type_info": map[string]interface{}{
				"container": map[string]interface{}{
					"children": []interface{}{
						map[string]interface{}{
							"name":       "inner.zip",
							"child_kind": "file",
							"data_type":  "container",
							"format":     string(format.FormatZIP),
							"path":       "inner.zip",
						},
					},
				},
			},
		},
	}

	preview, err := provider.Preview(context.Background(), req)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if preview.Mode != PreviewModeTable {
		t.Fatalf("Mode = %q, want %q", preview.Mode, PreviewModeTable)
	}
	if !reflect.DeepEqual(preview.Columns, []string{"id", "name"}) {
		t.Fatalf("Columns = %#v, want id/name", preview.Columns)
	}
	if len(preview.Rows) != 2 || preview.Rows[1]["name"] != "Shanghai" {
		t.Fatalf("Rows = %#v, want cities rows", preview.Rows)
	}
}

func TestContainerChildPreviewProviderPreviewsNestedZIPMultiTableChildRefs(t *testing.T) {
	previousEngine, previousEngineErr := plugin.Get("nfs")
	enginePlugin := &recordingContentPlugin{engineType: "nfs"}
	plugin.Register(enginePlugin)
	defer func() {
		if previousEngineErr == nil {
			plugin.Register(previousEngine)
			return
		}
		plugin.Unregister(enginePlugin.Type())
	}()

	inner := zipBytesRawForFilePreviewTest(t, map[string][]byte{
		"roads.shp": []byte("shape"),
		"roads.shx": []byte("index"),
		"roads.dbf": []byte("attrs"),
	})
	enginePlugin.content = zipBytesRawForFilePreviewTest(t, map[string][]byte{
		"inner.zip": inner,
	})

	refProvider := &recordingMultiTableProvider{}
	registerPreviewTestFormatPlugin(t, refProvider)

	provider := NewContainerChildPreviewProvider(objectcontent.NewObjectContentRegistry())
	req := &PreviewRequest{
		Engine:          &models.Engine{EngineType: enginePlugin.Type(), ID: 7},
		ItemType:        "file",
		Schema:          "datasets",
		Table:           "outer.zip",
		PhysicalPath:    "/datasets/outer.zip",
		ChildName:       "inner.zip",
		NestedChildPath: "roads.shp",
		Page:            1,
		PageSize:        10,
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"format":    string(format.FormatZIP),
				"data_type": "container",
			},
			"type_info": map[string]interface{}{
				"container": map[string]interface{}{
					"children": []interface{}{
						map[string]interface{}{
							"name":       "inner.zip",
							"child_kind": "file",
							"data_type":  "container",
							"format":     string(format.FormatZIP),
							"path":       "inner.zip",
						},
					},
				},
			},
		},
	}

	preview, err := provider.Preview(context.Background(), req)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if preview.Mode != PreviewModeTable {
		t.Fatalf("Mode = %q, want %q", preview.Mode, PreviewModeTable)
	}
	if len(refProvider.lastRefs) != 3 {
		t.Fatalf("refs = %#v, want shapefile shp/shx/dbf refs", refProvider.lastRefs)
	}
}

func TestFileTablePreviewProviderRestoresSpatialInfoFromMetaAttributes(t *testing.T) {
	t.Parallel()

	provider := &FileTablePreviewProvider{}
	tableProvider := &recordingTableProvider{}
	req := &PreviewRequest{
		Page:     1,
		PageSize: 2,
		Table:    "manager/roads.json",
		Attributes: map[string]interface{}{
			"type_info": map[string]interface{}{
				"table": map[string]interface{}{
					"row_count": int64(1),
					"fields": []interface{}{
						map[string]interface{}{"name": "geometry", "type": string(datatype.FieldTypeGeometry), "nullable": false},
						map[string]interface{}{"name": "name", "type": string(datatype.FieldTypeString), "nullable": true},
					},
				},
			},
			"capabilities": map[string]interface{}{
				"spatial": map[string]interface{}{
					"primary_geometry_column": "geometry",
					"geometry_columns": []interface{}{
						map[string]interface{}{"name": "geometry", "geometry_type": "Point", "srid": int64(4326), "crs_ref": "EPSG:4326"},
					},
					"crs_definitions": []interface{}{
						map[string]interface{}{
							"id":                  "EPSG:4326",
							"definition_encoding": datatype.CRSDefinitionEncodingProj4,
							"definition":          "+proj=longlat +datum=WGS84 +no_defs +type=crs",
							"source":              "test",
						},
					},
					"extent": []interface{}{1.0, 2.0, 3.0, 4.0},
				},
			},
		},
	}

	preview, err := provider.previewStreamable(
		context.Background(),
		staticContentReader{content: []byte("mock")},
		"manager",
		"manager/roads.json",
		format.FormatJSON,
		tableProvider,
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
	if preview.SRID != 4326 {
		t.Fatalf("SRID = %d, want 4326", preview.SRID)
	}
	if len(preview.GeometryColumns) != 1 || preview.GeometryColumns[0] != "geometry" {
		t.Fatalf("GeometryColumns = %#v", preview.GeometryColumns)
	}
	if preview.GeometryColumn != "geometry" || preview.SourceCRS != "EPSG:4326" || preview.SourceCRSDefinition == nil {
		t.Fatalf("CRS contract = column:%q source:%q definition:%#v", preview.GeometryColumn, preview.SourceCRS, preview.SourceCRSDefinition)
	}
	if preview.TransformStatus != "not_transformed" || preview.PreviewHint != "direct_renderable" {
		t.Fatalf("transform contract = %q/%q", preview.TransformStatus, preview.PreviewHint)
	}
}

func TestTablePreviewSpatialCRSContractAcceptsCustomCRSWithoutSRID(t *testing.T) {
	t.Parallel()

	definition := &datatype.CRSDefinition{
		ID:                 "ADDP:CRS:test",
		DefinitionEncoding: datatype.CRSDefinitionEncodingESRIWKT,
		Definition:         `PROJCS["CGCS2000_3_Degree_GK_CM_120E"]`,
		Source:             datatype.CRSDefinitionSourceSidecarPRJ,
	}
	contract := tablePreviewSpatialCRSContract([]string{"geometry"}, 0, definition.ID, definition)

	if contract.GeometryColumn != "geometry" || contract.SourceCRS != definition.ID || contract.SourceCRSDefinition != definition {
		t.Fatalf("contract = %#v, want custom CRS contract", contract)
	}
	if contract.TransformStatus != "not_transformed" || contract.PreviewHint != "frontend_transform_required" {
		t.Fatalf("transform contract = %#v, want frontend transform required", contract)
	}
}

func zipBytesForFilePreviewTest(t *testing.T, files map[string]string) []byte {
	t.Helper()
	raw := make(map[string][]byte, len(files))
	for name, body := range files {
		raw[name] = []byte(body)
	}
	return zipBytesRawForFilePreviewTest(t, raw)
}

func zipBytesRawForFilePreviewTest(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, body := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := entry.Write(body); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func TestFileTablePreviewProviderPreviewShapefileReturnsTableModeAndFirstPage(t *testing.T) {
	t.Parallel()

	provider := &FileTablePreviewProvider{}
	refProvider := &recordingMultiTableProvider{}
	req := &PreviewRequest{
		Page:     1,
		PageSize: 2,
		Table:    "gis/roads.shp",
	}

	preview, err := provider.previewRefs(
		context.Background(),
		emptyRefReader{},
		nil,
		"gis/roads.shp",
		"bucket",
		format.FormatShapefile,
		refProvider,
		refProvider,
		nil,
		req,
	)
	if err != nil {
		t.Fatalf("previewRefs() error = %v", err)
	}
	if preview.Mode != PreviewModeTable {
		t.Fatalf("Mode = %q, want %q", preview.Mode, PreviewModeTable)
	}
	if refProvider.sampleOffset != 0 {
		t.Fatalf("sample offset = %d, want 0 for first page", refProvider.sampleOffset)
	}
	if preview.Page != 1 {
		t.Fatalf("Page = %d, want 1", preview.Page)
	}
}

func TestFileTablePreviewProviderPreviewRefsUsesAttributesTableInfo(t *testing.T) {
	provider := &FileTablePreviewProvider{}
	refProvider := &recordingMultiTableProvider{}
	req := &PreviewRequest{
		Page:     2,
		PageSize: 3,
		Table:    "gis/roads.shp",
		Attributes: map[string]interface{}{
			"type_info": map[string]interface{}{
				"table": map[string]interface{}{
					"row_count": float64(9),
					"fields": []interface{}{
						map[string]interface{}{"name": "name", "type": "string", "nullable": true},
					},
				},
			},
		},
	}

	preview, err := provider.previewRefs(
		context.Background(),
		emptyRefReader{},
		nil,
		"gis/roads.shp",
		"bucket",
		format.FormatShapefile,
		refProvider,
		refProvider,
		nil,
		req,
	)
	if err != nil {
		t.Fatalf("previewRefs() error = %v", err)
	}
	if refProvider.describeCalls != 0 {
		t.Fatalf("DescribeMultiTable calls = %d, want 0 when attributes have table info", refProvider.describeCalls)
	}
	if refProvider.sampleOffset != 3 {
		t.Fatalf("sample offset = %d, want 3", refProvider.sampleOffset)
	}
	if preview.Total != 9 {
		t.Fatalf("Total = %d, want 9", preview.Total)
	}
}

func TestFileTablePreviewProviderPreviewRefsRestoresProjectionDefinition(t *testing.T) {
	provider := &FileTablePreviewProvider{}
	refProvider := &recordingMultiTableProvider{}
	prj := `PROJCS["WGS_1984_UTM_Zone_50N",GEOGCS["GCS_WGS_1984",DATUM["D_WGS_1984",SPHEROID["WGS_1984",6378137.0,298.257223563]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Transverse_Mercator"],PARAMETER["False_Easting",500000.0],PARAMETER["False_Northing",0.0],PARAMETER["Central_Meridian",117.0],PARAMETER["Scale_Factor",0.9996],PARAMETER["Latitude_Of_Origin",0.0],UNIT["Meter",1.0]]`
	req := &PreviewRequest{
		Page:     1,
		PageSize: 2,
		Table:    "gis/roads.shp",
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"refs": []interface{}{
					map[string]interface{}{"path": "gis/roads.dbf", "role": "attributes", "required": true, "extension": ".dbf"},
					map[string]interface{}{"path": "gis/roads.prj", "role": "projection", "extension": ".prj"},
					map[string]interface{}{"path": "gis/roads.shp", "role": "main", "required": true, "primary": true, "extension": ".shp"},
					map[string]interface{}{"path": "gis/roads.shx", "role": "index", "required": true, "extension": ".shx"},
				},
			},
			"type_info": map[string]interface{}{
				"table": map[string]interface{}{
					"row_count": float64(1),
					"fields": []interface{}{
						map[string]interface{}{"name": "geometry", "type": string(datatype.FieldTypeGeometry), "nullable": false},
					},
				},
			},
			"capabilities": map[string]interface{}{
				"spatial": map[string]interface{}{
					"primary_geometry_column": "geometry",
					"geometry_columns": []interface{}{
						map[string]interface{}{"name": "geometry", "geometry_type": "LineString", "srid": int64(32650), "crs_ref": "EPSG:32650"},
					},
				},
			},
		},
	}

	preview, err := provider.previewRefs(
		context.Background(),
		staticContentReader{content: []byte(prj)},
		refsForPreview("gis/roads.shp", format.FormatShapefile, req.Attributes),
		"gis/roads.shp",
		"bucket",
		format.FormatShapefile,
		refProvider,
		refProvider,
		nil,
		req,
	)
	if err != nil {
		t.Fatalf("previewRefs() error = %v", err)
	}
	if preview.SourceCRS != "EPSG:32650" {
		t.Fatalf("SourceCRS = %q, want EPSG:32650", preview.SourceCRS)
	}
	if preview.SourceCRSDefinition == nil || preview.SourceCRSDefinition.Definition != prj || preview.SourceCRSDefinition.Source != datatype.CRSDefinitionSourceSidecarPRJ {
		t.Fatalf("SourceCRSDefinition = %#v, want sidecar PRJ definition", preview.SourceCRSDefinition)
	}
	if preview.TransformStatus != "not_transformed" || preview.PreviewHint != "frontend_transform_required" {
		t.Fatalf("transform contract = %q/%q, want not_transformed/frontend_transform_required", preview.TransformStatus, preview.PreviewHint)
	}
}

func TestRefReaderForPreviewUsesMetaRefFiles(t *testing.T) {
	refs := refsForPreview("bucket/roads/roads.shp", format.FormatShapefile, map[string]interface{}{
		"item": map[string]interface{}{
			"refs": []interface{}{
				map[string]interface{}{"path": "bucket/roads/roads.dbf", "role": "attributes", "required": true},
				map[string]interface{}{"path": "bucket/roads/roads.prj", "role": "projection"},
				map[string]interface{}{"path": "bucket/roads/roads.shp", "role": "main", "required": true, "primary": true},
				map[string]interface{}{"path": "bucket/roads/roads.shx", "role": "index", "required": true},
			},
		},
	})

	got := make(map[string]format.RelatedRef, len(refs))
	for _, ref := range refs {
		got[ref.Ref.Role] = ref
	}

	for _, role := range []string{"main", "index", "attributes", "projection"} {
		if _, ok := got[role]; !ok {
			t.Fatalf("missing ref role %q in %#v", role, refs)
		}
	}
	if !got["main"].Required || !got["index"].Required || !got["attributes"].Required {
		t.Fatalf("required flags = main:%v index:%v attributes:%v", got["main"].Required, got["index"].Required, got["attributes"].Required)
	}
	if got["projection"].Required {
		t.Fatalf("projection ref should be optional")
	}
	if got["main"].Ref.Path != "bucket/roads/roads.shp" {
		t.Fatalf("main path = %q", got["main"].Ref.Path)
	}
}

func TestRefReaderForPreviewDoesNotInventMissingOptionalShapefileRefs(t *testing.T) {
	refs := refsForPreview("bucket/roads/roads.shp", format.FormatShapefile, map[string]interface{}{
		"item": map[string]interface{}{
			"refs": []interface{}{
				map[string]interface{}{"path": "bucket/roads/roads.shp", "role": "main", "required": true, "primary": true},
				map[string]interface{}{"path": "bucket/roads/roads.shx", "role": "index", "required": true},
				map[string]interface{}{"path": "bucket/roads/roads.dbf", "role": "attributes", "required": true},
				map[string]interface{}{"path": "bucket/roads/roads.cpg", "role": "encoding"},
			},
		},
	})

	paths := make([]string, 0, len(refs))
	for _, ref := range refs {
		paths = append(paths, ref.Ref.Path)
	}
	for _, want := range []string{
		"bucket/roads/roads.shp",
		"bucket/roads/roads.shx",
		"bucket/roads/roads.dbf",
		"bucket/roads/roads.cpg",
	} {
		if !containsString(paths, want) {
			t.Fatalf("preview refs = %#v, want actual ref %s", paths, want)
		}
	}
	for _, unexpected := range []string{
		"bucket/roads/roads.prj",
		"bucket/roads/roads.qpj",
		"bucket/roads/roads.sbn",
		"bucket/roads/roads.sbx",
	} {
		if containsString(paths, unexpected) {
			t.Fatalf("preview refs = %#v, must not invent optional ref %s", paths, unexpected)
		}
	}
}

func TestRefReaderForPreviewFallsBackToSameBasenameRefs(t *testing.T) {
	refs := refsForPreview("bucket/roads/roads.shp", format.FormatShapefile, nil)
	required := map[string]bool{}
	for _, ref := range refs {
		if ref.Required {
			required[ref.Ref.Path] = true
		}
	}
	want := map[string]bool{
		"bucket/roads/roads.shp": true,
		"bucket/roads/roads.shx": true,
		"bucket/roads/roads.dbf": true,
	}
	if !reflect.DeepEqual(required, want) {
		t.Fatalf("required fallback refs = %#v, want %#v", required, want)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestRefReaderForPreviewFallsBackWhenMetaRefsHaveNoPrimary(t *testing.T) {
	refs := refsForPreview("bucket/roads/roads.shp", format.FormatShapefile, map[string]interface{}{
		"item": map[string]interface{}{
			"refs": []interface{}{
				map[string]interface{}{"path": "bucket/custom/roads.dbf", "role": "attributes", "required": true},
				map[string]interface{}{"path": "bucket/custom/roads.shp", "role": "main", "required": true},
				map[string]interface{}{"path": "bucket/custom/roads.shx", "role": "index", "required": true},
			},
		},
	})

	primary, err := format.PrimaryRelatedRef(refs)
	if err != nil {
		t.Fatalf("fallback refs primary error = %v", err)
	}
	if primary.Ref.Path != "bucket/roads/roads.shp" {
		t.Fatalf("primary fallback path = %q, want bucket/roads/roads.shp", primary.Ref.Path)
	}
}

func TestRefFilePreviewProviderOpensSelectedRelatedRef(t *testing.T) {
	previous, previousErr := plugin.Get("minio-ref-preview-selected")
	enginePlugin := &recordingContentPlugin{
		engineType: "minio-ref-preview-selected",
		content:    []byte("EPSG:4326"),
	}
	plugin.Register(enginePlugin)
	defer func() {
		if previousErr == nil {
			plugin.Register(previous)
			return
		}
		plugin.Unregister(enginePlugin.Type())
	}()

	contentRegistry := objectcontent.NewObjectContentRegistry()
	objectcontent.LoadObjectContentPlugins(contentRegistry, "")
	provider := NewRefFilePreviewProvider(contentRegistry)
	req := &PreviewRequest{
		Engine: &models.Engine{
			ID:             9,
			EngineType:     enginePlugin.Type(),
			ConnectionInfo: models.ConnectionInfo{"bucket": "bucket"},
		},
		ItemType: "object",
		NodeType: "object",
		Schema:   "bucket",
		Table:    "roads/roads.shp",
		RefPath:  "bucket/roads/roads.prj",
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"format":    string(format.FormatShapefile),
				"data_type": "table",
				"layout":    "multi",
				"refs": []interface{}{
					map[string]interface{}{"path": "bucket/roads/roads.shp", "role": "main", "required": true, "primary": true},
					map[string]interface{}{"path": "bucket/roads/roads.shx", "role": "index", "required": true},
					map[string]interface{}{"path": "bucket/roads/roads.dbf", "role": "attributes", "required": true},
					map[string]interface{}{"path": "bucket/roads/roads.prj", "role": "projection"},
				},
			},
		},
	}

	preview, err := provider.Preview(context.Background(), req)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if got := enginePlugin.openedPath.StringPath(); got != "bucket/roads/roads.prj" {
		t.Fatalf("opened path = %q, want selected projection ref", got)
	}
	if preview.Mode != PreviewModeObject || preview.Object == nil || preview.Object.Content == nil {
		t.Fatalf("preview = %#v, want object preview with content", preview)
	}
	if preview.Object.Path != "bucket/roads/roads.prj" || preview.Object.StorageRef != "bucket/roads/roads.prj" {
		t.Fatalf("object path/storage_ref = %q/%q, want selected ref path", preview.Object.Path, preview.Object.StorageRef)
	}
	if preview.Object.Content.Kind != models.ObjectPreviewKindText || preview.Object.Content.Text != "EPSG:4326" {
		t.Fatalf("content = %#v, want text projection content", preview.Object.Content)
	}
	if preview.Object.Content.Metadata["layout"] != "multi" {
		t.Fatalf("content metadata = %#v, want multi layout", preview.Object.Content.Metadata)
	}
	refs, ok := preview.Object.Content.Metadata["refs"].([]map[string]interface{})
	if !ok || len(refs) != 4 {
		t.Fatalf("content refs metadata = %#v, want four ref descriptors", preview.Object.Content.Metadata["refs"])
	}
}

type staticContentReader struct {
	content []byte
}

func (r staticContentReader) Open(context.Context, contentio.Ref) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(r.content)), nil
}

func (r staticContentReader) Stat(context.Context, contentio.Ref) (*contentio.Stat, error) {
	return nil, nil
}

type recordingTableProvider struct {
	sampleOffset  int64
	describeCalls int
	sampleOptions *format.ParseOptions
}

func (p *recordingTableProvider) Format() format.FormatType {
	return format.FormatParquet
}

func (p *recordingTableProvider) DescribeTable(context.Context, io.Reader, *format.ParseOptions) (*format.TableDescribeResult, error) {
	p.describeCalls++
	rowCount := int64(3)
	return format.TableDescribeResultFromTableInfo(&datatype.TableInfo{
		Fields:   []datatype.FieldInfo{{Name: "name", Type: datatype.FieldTypeString}},
		RowCount: &rowCount,
	}), nil
}

func (p *recordingTableProvider) SampleTable(_ context.Context, _ io.Reader, offset, _ int64, opts *format.ParseOptions) ([]map[string]interface{}, error) {
	p.sampleOffset = offset
	p.sampleOptions = opts
	return []map[string]interface{}{{"name": "first"}}, nil
}

type recordingMultiTableProvider struct {
	recordingTableProvider
	formatType    format.FormatType
	sampleOffset  int64
	describeCalls int
	lastRefs      []format.RelatedRef
}

func (p *recordingMultiTableProvider) Format() format.FormatType {
	if p.formatType != "" {
		return p.formatType
	}
	return format.FormatShapefile
}

func (p *recordingMultiTableProvider) RelatedRefSpecs() []format.RelatedRefSpec {
	return []format.RelatedRefSpec{
		{Extension: ".shp", Role: "main", Required: true, Primary: true},
		{Extension: ".shx", Role: "index", Required: true},
		{Extension: ".dbf", Role: "attributes", Required: true},
	}
}

func (p *recordingMultiTableProvider) DescribeMultiTable(_ context.Context, _ contentio.Reader, refs []format.RelatedRef, _ *format.ParseOptions) (*format.TableDescribeResult, error) {
	p.describeCalls++
	p.lastRefs = append([]format.RelatedRef(nil), refs...)
	rowCount := int64(1)
	return format.TableDescribeResultFromTableInfo(&datatype.TableInfo{
		Fields:   []datatype.FieldInfo{{Name: "name", Type: datatype.FieldTypeString}},
		RowCount: &rowCount,
	}), nil
}

func (p *recordingMultiTableProvider) SampleMultiTable(_ context.Context, _ contentio.Reader, _ []format.RelatedRef, offset, _ int64, _ *format.ParseOptions) ([]map[string]interface{}, error) {
	p.sampleOffset = offset
	return []map[string]interface{}{{"name": "first"}}, nil
}

func registerPreviewTestFormatPlugin(t *testing.T, testPlugin format.FormatPlugin) {
	t.Helper()
	previousPlugin, previousErr := format.GetFormatPlugin(testPlugin.Format())
	if err := format.RegisterFormatPlugin(testPlugin); err != nil {
		t.Fatalf("RegisterFormatPlugin(%s) error = %v", testPlugin.Format(), err)
	}
	t.Cleanup(func() {
		if previousErr == nil {
			_ = format.RegisterFormatPlugin(previousPlugin)
		}
	})
}

type emptyRefReader struct{}

func (emptyRefReader) Open(context.Context, contentio.Ref) (io.ReadCloser, error) {
	return nil, contentio.ErrContentNotFound
}

func (emptyRefReader) Stat(context.Context, contentio.Ref) (*contentio.Stat, error) {
	return nil, contentio.ErrContentNotFound
}

var _ format.TableInfoProvider = (*recordingTableProvider)(nil)
var _ format.TableSampleReader = (*recordingTableProvider)(nil)
var _ format.MultiTableInfoProvider = (*recordingMultiTableProvider)(nil)
var _ format.MultiTableSampleReader = (*recordingMultiTableProvider)(nil)
var _ contentio.Reader = staticContentReader{}
var _ contentio.Reader = emptyRefReader{}

type recordingContentPlugin struct {
	engineType      string
	content         []byte
	openedPath      plugin.CatalogPath
	rangeOpenedPath plugin.CatalogPath
	rangeOptions    plugin.ReadOptions
}

func (p *recordingContentPlugin) Type() string         { return p.engineType }
func (p *recordingContentPlugin) DisplayName() string  { return p.engineType }
func (p *recordingContentPlugin) EngineOrigin() string { return "general" }
func (p *recordingContentPlugin) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p *recordingContentPlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error {
	return nil
}
func (p *recordingContentPlugin) DefaultPort() int          { return 0 }
func (p *recordingContentPlugin) RequiredFields() []string  { return nil }
func (p *recordingContentPlugin) SensitiveFields() []string { return nil }
func (p *recordingContentPlugin) Capabilities() plugin.EngineCapabilities {
	switch p.engineType {
	case "nfs":
		return plugin.NewFileCapabilities(p.engineType)
	default:
		return plugin.NewObjectCapabilities(p.engineType)
	}
}
func (p *recordingContentPlugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemantics{}
}
func (p *recordingContentPlugin) OpenContent(_ context.Context, _ plugin.ConnectionInfo, path plugin.CatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	p.openedPath = path
	if len(p.content) > 0 {
		return io.NopCloser(bytes.NewReader(p.content)), nil
	}
	return io.NopCloser(strings.NewReader("name\nAlice\n")), nil
}
func (p *recordingContentPlugin) OpenRange(_ context.Context, _ plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	p.rangeOpenedPath = path
	p.rangeOptions = opts
	return io.NopCloser(strings.NewReader("range")), nil
}

func excelWorkbookBytesForPreviewTest(t *testing.T) []byte {
	t.Helper()

	workbook := excelize.NewFile()
	defer workbook.Close()
	index, err := workbook.NewSheet("Cities")
	if err != nil {
		t.Fatalf("new sheet: %v", err)
	}
	workbook.SetActiveSheet(index)
	if err := workbook.SetSheetRow("Cities", "A1", &[]interface{}{"id", "name"}); err != nil {
		t.Fatalf("set header: %v", err)
	}
	if err := workbook.SetSheetRow("Cities", "A2", &[]interface{}{1, "Hangzhou"}); err != nil {
		t.Fatalf("set row: %v", err)
	}
	if err := workbook.SetSheetRow("Cities", "A3", &[]interface{}{2, "Shanghai"}); err != nil {
		t.Fatalf("set row: %v", err)
	}
	var buf bytes.Buffer
	if err := workbook.Write(&buf); err != nil {
		t.Fatalf("write workbook: %v", err)
	}
	return buf.Bytes()
}

func sqliteDatabaseBytesForPreviewTest(t *testing.T) []byte {
	t.Helper()

	return sqliteFileBytesForPreviewTest(t, "sqlite-preview-*.db", []string{
		`CREATE TABLE cities (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`,
		`INSERT INTO cities(id, name) VALUES (1, 'Hangzhou'), (2, 'Shanghai')`,
	})
}

func geoPackageBytesForPreviewTest(t *testing.T) []byte {
	t.Helper()

	return sqliteFileBytesForPreviewTest(t, "gpkg-preview-*.gpkg", []string{
		`CREATE TABLE gpkg_contents (table_name TEXT PRIMARY KEY, data_type TEXT NOT NULL, identifier TEXT, srs_id INTEGER, min_x DOUBLE, min_y DOUBLE, max_x DOUBLE, max_y DOUBLE)`,
		`CREATE TABLE gpkg_geometry_columns (table_name TEXT, column_name TEXT, geometry_type_name TEXT, srs_id INTEGER)`,
		`CREATE TABLE roads (id INTEGER PRIMARY KEY, geom BLOB, name TEXT)`,
		`CREATE VIRTUAL TABLE rtree_roads_geom USING rtree(id, minx, maxx, miny, maxy)`,
		`INSERT INTO gpkg_contents(table_name, data_type, identifier, srs_id, min_x, min_y, max_x, max_y) VALUES ('roads', 'features', 'Road Layer', 4326, 120.0, 30.0, 121.0, 31.0)`,
		`INSERT INTO gpkg_geometry_columns(table_name, column_name, geometry_type_name, srs_id) VALUES ('roads', 'geom', 'LINESTRING', 4326)`,
		`INSERT INTO roads(id, geom, name) VALUES (1, X'00', 'Main Road')`,
	})
}

func sqliteFileBytesForPreviewTest(t *testing.T, pattern string, statements []string) []byte {
	t.Helper()

	tmp, err := os.CreateTemp("", pattern)
	if err != nil {
		t.Fatalf("create temp sqlite file: %v", err)
	}
	path := tmp.Name()
	if err := tmp.Close(); err != nil {
		t.Fatalf("close temp sqlite file: %v", err)
	}
	defer os.Remove(path)

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sqlite file: %v", err)
	}
	return data
}

func TestStorageRefForPreviewIncludesBucketForObjectCatalogRef(t *testing.T) {
	req := &PreviewRequest{Engine: &models.Engine{EngineType: "minio"}, ItemType: "object"}

	if got := storageRefForPreview(req, "gischain", "data/farmland.prj"); got != "gischain/data/farmland.prj" {
		t.Fatalf("storage ref = %q, want gischain/data/farmland.prj", got)
	}
	if got := storageRefForPreview(req, "gischain", "gischain/data/farmland.prj"); got != "gischain/data/farmland.prj" {
		t.Fatalf("storage ref = %q, want gischain/data/farmland.prj", got)
	}
}

func TestContainerChildInfoFromMapKeepsRefsAndExplicitNative(t *testing.T) {
	child := objectcontent.ContainerChildInfoFromMap(map[string]interface{}{
		"name":       "roads.shp",
		"child_kind": "multi",
		"data_type":  "table",
		"format":     "shapefile",
		"unknown":    "ignored",
		"native": map[string]interface{}{
			"table":     "roads",
			"columns":   []interface{}{map[string]interface{}{"name": "stale"}},
			"fields":    []interface{}{"stale"},
			"schema":    map[string]interface{}{"fields": []interface{}{"stale"}},
			"type_info": map[string]interface{}{"table": map[string]interface{}{"fields": []interface{}{"stale"}}},
		},
		"refs": []interface{}{
			map[string]interface{}{"role": "main", "path": "roads.shp", "required": true, "primary": true, "extension": ".shp"},
			map[string]interface{}{"role": "index", "path": "roads.shx", "required": true, "extension": ".shx"},
		},
	})
	if child.Format != string(format.FormatShapefile) || len(child.Refs) != 2 {
		t.Fatalf("child = %#v, want shapefile multi refs", child)
	}
	if !child.Refs[0].Primary || child.Refs[0].Path != "roads.shp" {
		t.Fatalf("primary ref = %#v, want roads.shp", child.Refs[0])
	}
	if child.Native["table"] != "roads" || child.Native["unknown"] != nil {
		t.Fatalf("native = %#v, want explicit native only", child.Native)
	}
	for _, key := range []string{"columns", "fields", "schema", "type_info"} {
		if child.Native[key] != nil {
			t.Fatalf("schema-like native key %q should not survive: %#v", key, child.Native)
		}
	}
}

func TestContainerChildInfoFromMapNormalizesFormat(t *testing.T) {
	unknown := objectcontent.ContainerChildInfoFromMap(map[string]interface{}{
		"name":       "config/docker-compose.yml",
		"child_kind": "file",
		"data_type":  "unknown",
		"format":     "yml",
	})
	if unknown.Format != "" {
		t.Fatalf("unknown child format = %q, want empty", unknown.Format)
	}

	csv := objectcontent.ContainerChildInfoFromMap(map[string]interface{}{
		"name":       "data/table.data",
		"child_kind": "file",
		"data_type":  "table",
		"format":     ".csv",
	})
	if csv.Format != string(format.FormatCSV) {
		t.Fatalf("csv child format = %q, want csv", csv.Format)
	}
}

func TestNormalizeObjectContentRequestFormatDropsUnknownLegacyFormat(t *testing.T) {
	if got := normalizeObjectContentRequestFormat("yml"); got != "" {
		t.Fatalf("normalized legacy yml format = %q, want empty", got)
	}
	if got := normalizeObjectContentRequestFormat(".csv"); got != string(format.FormatCSV) {
		t.Fatalf("normalized csv format = %q, want csv", got)
	}
}

func TestTableAccessIndexFromMetaAttributesNormalizesFormat(t *testing.T) {
	index := tableAccessIndexFromMetaAttributes(map[string]interface{}{
		"access_index": map[string]interface{}{
			"table": map[string]interface{}{
				"kind":        datatype.AccessIndexKindSparseRow,
				"data_type":   string(datatype.Table),
				"format":      "yml",
				"unit":        datatype.AccessIndexUnitRow,
				"offset_unit": datatype.AccessIndexOffsetByte,
				"anchors": []interface{}{
					map[string]interface{}{"row": 0, "byte_offset": 0},
				},
			},
		},
	})
	if index == nil {
		t.Fatal("index is nil")
	}
	if index.Format != "" {
		t.Fatalf("index format = %q, want empty for unknown legacy format", index.Format)
	}
}

func TestShouldRecommendAccessIndexRefreshOnlyForSupportedMissingIndex(t *testing.T) {
	req := &PreviewRequest{ItemType: "object", ScannedDepth: "deep", Attributes: map[string]interface{}{}}
	if !shouldRecommendAccessIndexRefresh(req, format.FormatCSV, false) {
		t.Fatal("expected CSV object preview without usable access index to recommend item refresh")
	}
	if shouldRecommendAccessIndexRefresh(&PreviewRequest{ItemType: "object", ScannedDepth: "basic"}, format.FormatCSV, false) {
		t.Fatal("basic item should use the general item refresh recommendation")
	}
	if shouldRecommendAccessIndexRefresh(req, format.FormatParquet, false) {
		t.Fatal("parquet should not recommend access index refresh")
	}
	if shouldRecommendAccessIndexRefresh(req, format.FormatCSV, true) {
		t.Fatal("used access index should not recommend refresh")
	}
	withIndex := &PreviewRequest{
		ItemType:     "object",
		ScannedDepth: "deep",
		Attributes: map[string]interface{}{
			"access_index": map[string]interface{}{
				"table": map[string]interface{}{
					"kind":        datatype.AccessIndexKindSparseRow,
					"unit":        datatype.AccessIndexUnitRow,
					"offset_unit": datatype.AccessIndexOffsetByte,
					"anchors": []interface{}{
						map[string]interface{}{"row": 0, "byte_offset": 0},
					},
				},
			},
		},
	}
	if shouldRecommendAccessIndexRefresh(withIndex, format.FormatCSV, false) {
		t.Fatal("usable access index should not recommend refresh")
	}
}
