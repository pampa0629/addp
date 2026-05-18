package preview

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/objectcontent"
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
							"name":  "Road Layer",
							"table": "roads",
							"kind":  "layer",
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
	if len(enginePlugin.openedPath.Segments) != 3 {
		t.Fatalf("segments = %#v, want bucket + prefix + object", enginePlugin.openedPath.Segments)
	}
	objectSegment := enginePlugin.openedPath.Segments[2]
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
	if len(enginePlugin.rangeOpenedPath.Segments) != 3 {
		t.Fatalf("segments = %#v, want bucket + prefix + object", enginePlugin.rangeOpenedPath.Segments)
	}
	objectSegment := enginePlugin.rangeOpenedPath.Segments[2]
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
						map[string]interface{}{"name": "name", "type": string(format.FieldTypeString), "nullable": true},
						map[string]interface{}{"name": "age", "type": string(format.FieldTypeInt), "nullable": true},
					},
				},
			},
		},
	}
	tableProvider, err := format.GetTableProvider(format.FormatTSV)
	if err != nil {
		t.Fatalf("GetTableProvider(tsv) failed: %v", err)
	}

	preview, err := provider.previewStreamable(
		context.Background(),
		staticContentReader{content: []byte("name\tage\nAlice\t25\nBob\t30\n")},
		"manager",
		"manager/test.tsv",
		format.FormatTSV,
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
		staticContentReader{content: []byte("mock")},
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
							"kind":       "table",
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
							"name":      "data/cities.csv",
							"kind":      "file",
							"data_type": "table",
							"format":    string(format.FormatCSV),
							"path":      "data/cities.csv",
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
	objectcontent.LoadObjectContentPlugins(provider.(*ContainerChildPreviewProvider).content, "../../plugins/manifest.json")
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
							"name":      "docs/readme.txt",
							"kind":      "file",
							"data_type": "document",
							"format":    string(format.FormatText),
							"path":      "docs/readme.txt",
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
	objectcontent.LoadObjectContentPlugins(provider.(*ContainerChildPreviewProvider).content, "../../plugins/manifest.json")
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
							"name":      "inner.zip",
							"kind":      "file",
							"data_type": "container",
							"format":    string(format.FormatZIP),
							"path":      "inner.zip",
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
	objectcontent.LoadObjectContentPlugins(provider.(*ContainerChildPreviewProvider).content, "../../plugins/manifest.json")
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
							"name":      "inner.zip",
							"kind":      "file",
							"data_type": "container",
							"format":    string(format.FormatZIP),
							"path":      "inner.zip",
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
	objectcontent.LoadObjectContentPlugins(provider.(*ContainerChildPreviewProvider).content, "../../plugins/manifest.json")
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
							"name":      "inner.zip",
							"kind":      "file",
							"data_type": "container",
							"format":    string(format.FormatZIP),
							"path":      "inner.zip",
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

func TestFileTablePreviewProviderRestoresSpatialInfoFromAttributes(t *testing.T) {
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
						map[string]interface{}{"name": "geometry", "type": string(format.FieldTypeGeometry), "nullable": false},
						map[string]interface{}{"name": "name", "type": string(format.FieldTypeString), "nullable": true},
					},
				},
			},
			"capabilities": map[string]interface{}{
				"spatial": map[string]interface{}{
					"primary_geometry_column": "geometry",
					"geometry_columns": []interface{}{
						map[string]interface{}{"name": "geometry", "geometry_type": "Point", "srid": int64(4326)},
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
		"bucket",
		format.FormatShapefile,
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
		"bucket",
		format.FormatShapefile,
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

	got := make(map[string]contentio.Ref, len(refs))
	for _, ref := range refs {
		got[ref.Role] = ref
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
	if got["main"].Path != "bucket/roads/roads.shp" {
		t.Fatalf("main path = %q", got["main"].Path)
	}
}

func TestRefReaderForPreviewFallsBackToSameBasenameRefs(t *testing.T) {
	refs := refsForPreview("bucket/roads/roads.shp", format.FormatShapefile, nil)
	required := map[string]bool{}
	for _, ref := range refs {
		if ref.Required {
			required[ref.Path] = true
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

type staticContentReader struct {
	content []byte
}

func (r staticContentReader) Open(context.Context, contentio.Ref) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(r.content)), nil
}

func (r staticContentReader) Stat(context.Context, contentio.Ref) (*contentio.Stat, error) {
	return nil, nil
}

func (r staticContentReader) List(context.Context, contentio.Ref) ([]contentio.Ref, error) {
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

func (p *recordingTableProvider) Capabilities() format.FormatCapability {
	return format.FormatCapability{}
}

func (p *recordingTableProvider) DescribeTable(context.Context, io.Reader, *format.ParseOptions) (*format.TableInfo, error) {
	p.describeCalls++
	rowCount := int64(3)
	return &format.TableInfo{
		Fields:   []format.FieldInfo{{Name: "name", Type: format.FieldTypeString}},
		RowCount: &rowCount,
	}, nil
}

func (p *recordingTableProvider) SampleTable(_ context.Context, _ io.Reader, offset, _ int64, opts *format.ParseOptions) ([]map[string]interface{}, error) {
	p.sampleOffset = offset
	p.sampleOptions = opts
	return []map[string]interface{}{{"name": "first"}}, nil
}

type recordingMultiTableProvider struct {
	recordingTableProvider
	sampleOffset  int64
	describeCalls int
}

func (p *recordingMultiTableProvider) Format() format.FormatType {
	return format.FormatShapefile
}

func (p *recordingMultiTableProvider) DescribeMultiTable(context.Context, contentio.Reader, []contentio.Ref, *format.ParseOptions) (*format.TableInfo, error) {
	p.describeCalls++
	rowCount := int64(1)
	return &format.TableInfo{
		Fields:   []format.FieldInfo{{Name: "name", Type: format.FieldTypeString}},
		RowCount: &rowCount,
	}, nil
}

func (p *recordingMultiTableProvider) SampleMultiTable(_ context.Context, _ contentio.Reader, _ []contentio.Ref, offset, _ int64, _ *format.ParseOptions) ([]map[string]interface{}, error) {
	p.sampleOffset = offset
	return []map[string]interface{}{{"name": "first"}}, nil
}

type emptyRefReader struct{}

func (emptyRefReader) Open(context.Context, contentio.Ref) (io.ReadCloser, error) {
	return nil, contentio.ErrContentNotFound
}

func (emptyRefReader) Stat(context.Context, contentio.Ref) (*contentio.Stat, error) {
	return nil, contentio.ErrContentNotFound
}

func (emptyRefReader) List(context.Context, contentio.Ref) ([]contentio.Ref, error) {
	return nil, contentio.ErrContentNotFound
}

var _ format.TableProvider = (*recordingTableProvider)(nil)
var _ format.MultiTableProvider = (*recordingMultiTableProvider)(nil)
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

func TestContentIndexObjectKeyIncludesBucketForObjectCatalog(t *testing.T) {
	req := &PreviewRequest{Engine: &models.Engine{EngineType: "minio"}, ItemType: "object"}

	if got := contentIndexObjectKey(req, "bucket", "dir/sample.csv"); got != "bucket/dir/sample.csv" {
		t.Fatalf("object key = %q, want bucket/dir/sample.csv", got)
	}
	if got := contentIndexObjectKey(req, "bucket", "bucket/dir/sample.csv"); got != "bucket/dir/sample.csv" {
		t.Fatalf("object key = %q, want bucket/dir/sample.csv", got)
	}
}

func TestMapToContainerChildInfoKeepsRefs(t *testing.T) {
	child := mapToContainerChildInfo(map[string]interface{}{
		"name":         "roads.shp",
		"kind":         "multi",
		"data_type":    "table",
		"format":       "shapefile",
		"organization": "multi",
		"refs": []interface{}{
			map[string]interface{}{"role": "main", "path": "roads.shp", "required": true, "primary": true, "extension": ".shp"},
			map[string]interface{}{"role": "index", "path": "roads.shx", "required": true, "extension": ".shx"},
		},
	})
	if child.Format != format.FormatShapefile || child.Organization != "multi" || len(child.Refs) != 2 {
		t.Fatalf("child = %#v, want shapefile multi refs", child)
	}
	if !child.Refs[0].Primary || child.Refs[0].Path != "roads.shp" {
		t.Fatalf("primary ref = %#v, want roads.shp", child.Refs[0])
	}
}
