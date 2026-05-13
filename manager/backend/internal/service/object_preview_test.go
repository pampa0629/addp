package service

import (
	"testing"

	"github.com/addp/common/format"
	"github.com/addp/manager/internal/models"
)

func TestInferContentType(t *testing.T) {
	testcases := []struct {
		name        string
		objectPath  string
		contentType string
		expect      string
	}{
		{
			name:        "keep_explicit_type",
			objectPath:  "bucket/report.pdf",
			contentType: "application/pdf",
			expect:      "application/pdf",
		},
		{
			name:        "docx_with_generic_type",
			objectPath:  "bucket/docs/关于底座.docx",
			contentType: "application/octet-stream",
			expect:      "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		},
		{
			name:        "docx_uppercase_extension_generic_type",
			objectPath:  "bucket/docs/Manual.DOCX",
			contentType: "APPLICATION/OCTET-STREAM",
			expect:      "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		},
		{
			name:        "docx_non_mime_token_fallback",
			objectPath:  "bucket/docs/公共技术底座部署手册.docx",
			contentType: "docx",
			expect:      "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		},
		{
			name:        "wps_extension_generic_type",
			objectPath:  "bucket/docs/示例文档.wps",
			contentType: "application/octet-stream",
			expect:      "application/vnd.ms-works",
		},
		{
			name:        "unknown_extension_keeps_generic_type",
			objectPath:  "bucket/blob/data.bin",
			contentType: "application/octet-stream",
			expect:      "application/octet-stream",
		},
		{
			name:        "markdown_extension_generic_type",
			objectPath:  "bucket/docs/README.md",
			contentType: "application/octet-stream",
			expect:      "text/markdown",
		},
		{
			name:        "binary_octet_stream_treated_as_generic",
			objectPath:  "bucket/slides/demo.pptx",
			contentType: "binary/octet-stream",
			expect:      "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		},
	}

	for _, tc := range testcases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := inferContentType(tc.objectPath, tc.contentType)
			if got != tc.expect {
				t.Fatalf("inferContentType(%q, %q) = %q, want %q", tc.objectPath, tc.contentType, got, tc.expect)
			}
		})
	}
}

func TestObjectContentMatcherGenericContentType(t *testing.T) {
	t.Parallel()
	matcher := newObjectContentMatcher(
		[]string{"docx"},
		[]string{".docx"},
		[]string{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "wordprocessingml"},
	)
	req := &ObjectContentRequest{
		Extension:   ".docx",
		ContentType: "docx",
	}
	if !matcher.matches(req) {
		t.Fatalf("expected matcher to accept generic DOCX content type")
	}
}

func TestObjectContentMatcherWPS(t *testing.T) {
	t.Parallel()
	matcher := newObjectContentMatcher(
		[]string{"wps"},
		[]string{".wps"},
		[]string{"application/vnd.ms-works", "application/wps-office.doc", "application/x-wps", "application/kswps"},
	)
	req := &ObjectContentRequest{
		Extension:   ".wps",
		ContentType: "application/wps-office.doc",
	}
	if !matcher.matches(req) {
		t.Fatalf("expected matcher to accept WPS content type")
	}
}

func TestBuiltinContentMatcherUsesFormatDescriptorDefaults(t *testing.T) {
	t.Parallel()
	wps, err := builtinContentFactories["wps"](ObjectContentPluginConfig{Name: "wps"})
	if err != nil {
		t.Fatalf("build wps handler: %v", err)
	}
	if !wps.Matches(&ObjectContentRequest{Extension: ".wps", ContentType: "application/kswps"}) {
		t.Fatalf("expected WPS handler to match descriptor MIME")
	}

	excel, err := builtinContentFactories["excel"](ObjectContentPluginConfig{Name: "excel"})
	if err != nil {
		t.Fatalf("build excel handler: %v", err)
	}
	if !excel.Matches(&ObjectContentRequest{Extension: ".xlsm", ContentType: "application/vnd.ms-excel.sheet.macroenabled.12"}) {
		t.Fatalf("expected Excel handler to match descriptor extension and MIME")
	}
}

func TestLoadObjectContentPluginsUsesDescriptorDefaults(t *testing.T) {
	registry := NewObjectContentRegistry()
	LoadObjectContentPlugins(registry, "../../plugins/content")

	tests := []struct {
		name string
		req  ObjectContentRequest
		want string
	}{
		{
			name: "wps",
			req:  ObjectContentRequest{Extension: ".wps", ContentType: "application/kswps"},
			want: "builtin:content-wps",
		},
		{
			name: "markdown",
			req:  ObjectContentRequest{Extension: ".md", ContentType: "text/markdown"},
			want: "builtin:content-markdown",
		},
		{
			name: "excel",
			req:  ObjectContentRequest{Extension: ".xlsm", ContentType: "application/vnd.ms-excel.sheet.macroenabled.12"},
			want: "builtin:content-excel",
		},
		{
			name: "image",
			req:  ObjectContentRequest{Extension: ".png", ContentType: "image/png"},
			want: "builtin:content-image",
		},
		{
			name: "parquet",
			req:  ObjectContentRequest{Extension: ".parquet", ContentType: "application/vnd.apache.parquet"},
			want: "builtin:content-parquet",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			handler := registry.Resolve(&tt.req)
			if handler == nil {
				t.Fatalf("expected handler %s, got nil", tt.want)
			}
			if handler.Name() != tt.want {
				t.Fatalf("handler = %q, want %q", handler.Name(), tt.want)
			}
		})
	}
}

func TestObjectContentTableFormatUsesExplicitFormat(t *testing.T) {
	if got := objectContentTableFormat(&ObjectContentRequest{Format: "orc", Extension: ".bin", ContentType: "application/octet-stream"}); got != format.FormatORC {
		t.Fatalf("objectContentTableFormat() = %q, want %q", got, format.FormatORC)
	}
}

func TestObjectContentRegistryDoesNotResolveCSV(t *testing.T) {
	registry := NewObjectContentRegistry()
	LoadObjectContentPlugins(registry, "../../plugins/content")

	for _, req := range []ObjectContentRequest{
		{Format: "csv"},
		{Extension: ".csv"},
		{ContentType: "text/csv"},
		{ContentType: "text/csv; charset=utf-8"},
	} {
		req := req
		if handler := registry.Resolve(&req); handler != nil {
			t.Fatalf("CSV object content request resolved to %q, want nil", handler.Name())
		}
	}
}

func TestObjectContentMatcherShapefileAliases(t *testing.T) {
	t.Parallel()
	matcher := newObjectContentMatcher(
		[]string{"shapefile", "shp"},
		[]string{".shp"},
		defaultShapefileContentTypes(),
	)
	testcases := []string{
		"application/x-esri-shapefile",
		"application/x-shapefile",
		"application/octet-stream",
		"binary/octet-stream",
		"shp",
	}
	for _, contentType := range testcases {
		contentType := contentType
		t.Run(contentType, func(t *testing.T) {
			t.Parallel()
			req := &ObjectContentRequest{
				Extension:   ".shp",
				ContentType: contentType,
			}
			if !matcher.matches(req) {
				t.Fatalf("expected matcher to accept shapefile content type %q", contentType)
			}
		})
	}
}

func TestObjectContentMatcherPrefersStandardFormat(t *testing.T) {
	t.Parallel()
	matcher := newObjectContentMatcher(
		[]string{"pdf"},
		[]string{".pdf"},
		[]string{"application/pdf"},
	)
	req := &ObjectContentRequest{
		Format:      "pdf",
		Extension:   ".bin",
		ContentType: "application/octet-stream",
	}
	if !matcher.matches(req) {
		t.Fatalf("expected matcher to accept standard format even when extension and content type are generic")
	}
}

func TestObjectContentMatcherIgnoresUnknownFormat(t *testing.T) {
	t.Parallel()
	matcher := newObjectContentMatcher(
		[]string{"pdf"},
		[]string{".pdf"},
		[]string{"application/pdf"},
	)
	req := &ObjectContentRequest{
		Format:      "unknown",
		Extension:   ".pdf",
		ContentType: "application/pdf",
	}
	if !matcher.matches(req) {
		t.Fatalf("expected matcher to fall back to extension and content type when format is unknown")
	}
}

func TestTextContentHandlerUsesExplicitTextMatcher(t *testing.T) {
	t.Parallel()
	handler, err := builtinContentFactories["text"](ObjectContentPluginConfig{Name: "text"})
	if err != nil {
		t.Fatalf("build text handler: %v", err)
	}

	if !handler.Matches(&ObjectContentRequest{Format: "text", Extension: ".bin", ContentType: "application/octet-stream"}) {
		t.Fatalf("expected text handler to match explicit text format")
	}
	if !handler.Matches(&ObjectContentRequest{Extension: ".bin", ContentType: "text/plain"}) {
		t.Fatalf("expected text handler to match text content type")
	}
	if handler.Matches(&ObjectContentRequest{Format: "unknown", Extension: ".bin", ContentType: "application/octet-stream"}) {
		t.Fatalf("text handler must not be the catch-all fallback for unknown binary files")
	}
}

func TestTextContentHandlerUsesDocumentTextReader(t *testing.T) {
	t.Parallel()
	handler, err := builtinContentFactories["text"](ObjectContentPluginConfig{Name: "text"})
	if err != nil {
		t.Fatalf("build text handler: %v", err)
	}

	content, truncated, err := handler.Handle(
		nil,
		&ObjectContentRequest{Name: "note.txt", Format: "text", Size: 8},
		func(limit int64) ([]byte, bool, error) {
			return []byte("\ufeffhello"), false, nil
		},
	)
	if err != nil {
		t.Fatalf("handle text content: %v", err)
	}
	if truncated {
		t.Fatalf("unexpected truncation")
	}
	if content.Kind != "text" {
		t.Fatalf("Kind = %q, want text", content.Kind)
	}
	if content.Text != "hello" {
		t.Fatalf("Text = %q, want hello", content.Text)
	}
}

func TestUnsupportedContentHandlerProbesText(t *testing.T) {
	t.Parallel()
	handler, err := builtinContentFactories["unsupported"](ObjectContentPluginConfig{Name: "unsupported"})
	if err != nil {
		t.Fatalf("build unsupported handler: %v", err)
	}

	content, truncated, err := handler.Handle(
		nil,
		&ObjectContentRequest{Name: "README", Size: 12},
		func(limit int64) ([]byte, bool, error) {
			return []byte("hello\nworld\n"), false, nil
		},
	)
	if err != nil {
		t.Fatalf("handle text fallback: %v", err)
	}
	if truncated {
		t.Fatalf("unexpected truncation")
	}
	if content.Kind != "text" {
		t.Fatalf("expected text fallback for UTF-8 content, got %q", content.Kind)
	}
	if content.PreviewMaterial != "text" {
		t.Fatalf("PreviewMaterial = %q, want text", content.PreviewMaterial)
	}
	if content.Text != "hello\nworld\n" {
		t.Fatalf("unexpected text content: %q", content.Text)
	}
}

func TestUnsupportedContentHandlerKeepsBinaryUnsupported(t *testing.T) {
	t.Parallel()
	handler, err := builtinContentFactories["unsupported"](ObjectContentPluginConfig{Name: "unsupported"})
	if err != nil {
		t.Fatalf("build unsupported handler: %v", err)
	}

	content, truncated, err := handler.Handle(
		nil,
		&ObjectContentRequest{Name: "blob.bin", Extension: ".bin", ContentType: "application/octet-stream", Size: 4},
		func(limit int64) ([]byte, bool, error) {
			return []byte{0x00, 0x01, 0x02, 0x03}, false, nil
		},
	)
	if err != nil {
		t.Fatalf("handle binary fallback: %v", err)
	}
	if truncated {
		t.Fatalf("unexpected truncation")
	}
	if content.Kind != "unsupported" {
		t.Fatalf("expected unsupported fallback, got %q", content.Kind)
	}
	if content.PreviewMaterial != "raw_binary" {
		t.Fatalf("PreviewMaterial = %q, want raw_binary", content.PreviewMaterial)
	}
	if content.Text == "" {
		t.Fatalf("expected unsupported preview message")
	}
}

func TestBinaryBase64HandlerDeclaresRawBinaryMaterialAndRenderer(t *testing.T) {
	t.Parallel()
	handler, err := builtinContentFactories["wps"](ObjectContentPluginConfig{Name: "wps"})
	if err != nil {
		t.Fatalf("build wps handler: %v", err)
	}

	content, truncated, err := handler.Handle(
		nil,
		&ObjectContentRequest{Name: "demo.wps", Format: "wps", Size: 4},
		func(limit int64) ([]byte, bool, error) {
			return []byte{0x50, 0x4b, 0x03, 0x04}, false, nil
		},
	)
	if err != nil {
		t.Fatalf("handle wps content: %v", err)
	}
	if truncated {
		t.Fatalf("unexpected truncation")
	}
	if content.Kind != "wps" {
		t.Fatalf("Kind = %q, want wps", content.Kind)
	}
	if content.PreviewMaterial != "raw_binary" {
		t.Fatalf("PreviewMaterial = %q, want raw_binary", content.PreviewMaterial)
	}
	if content.FrontendRenderer != "wps" {
		t.Fatalf("FrontendRenderer = %q, want wps", content.FrontendRenderer)
	}
	if content.Encoding != "base64" || content.Data == "" {
		t.Fatalf("expected base64 data, encoding=%q data=%q", content.Encoding, content.Data)
	}
}

func TestMarkdownHandlerDeclaresMarkdownMaterialAndRenderer(t *testing.T) {
	t.Parallel()
	handler, err := builtinContentFactories["markdown"](ObjectContentPluginConfig{Name: "markdown"})
	if err != nil {
		t.Fatalf("build markdown handler: %v", err)
	}

	content, _, err := handler.Handle(
		nil,
		&ObjectContentRequest{Name: "README.md", Format: "markdown", Size: 8},
		func(limit int64) ([]byte, bool, error) {
			return []byte("# Title\n"), false, nil
		},
	)
	if err != nil {
		t.Fatalf("handle markdown content: %v", err)
	}
	if content.Kind != "markdown" {
		t.Fatalf("Kind = %q, want markdown", content.Kind)
	}
	if content.PreviewMaterial != "markdown" {
		t.Fatalf("PreviewMaterial = %q, want markdown", content.PreviewMaterial)
	}
	if content.FrontendRenderer != "markdown" {
		t.Fatalf("FrontendRenderer = %q, want markdown", content.FrontendRenderer)
	}
}

func TestMarkdownContentHandlerUsesDocumentTextReader(t *testing.T) {
	t.Parallel()
	maxBytes := int64(6)
	handler, err := builtinContentFactories["markdown"](ObjectContentPluginConfig{Name: "markdown", MaxBytes: &maxBytes})
	if err != nil {
		t.Fatalf("build markdown handler: %v", err)
	}

	content, truncated, err := handler.Handle(
		nil,
		&ObjectContentRequest{Name: "README.md", Format: "markdown", Size: 8},
		func(limit int64) ([]byte, bool, error) {
			return []byte("\ufeff# Ti"), true, nil
		},
	)
	if err != nil {
		t.Fatalf("handle markdown content: %v", err)
	}
	if !truncated || !content.Truncated {
		t.Fatalf("expected truncated content, truncated=%v content=%#v", truncated, content)
	}
	if content.Kind != "markdown" {
		t.Fatalf("Kind = %q, want markdown", content.Kind)
	}
	if content.Text != "# T" {
		t.Fatalf("Text = %q, want # T", content.Text)
	}
}

func TestShouldBuildShapefileTablePreview(t *testing.T) {
	t.Parallel()
	preview := &models.TablePreview{
		Object: &models.ObjectPreview{
			Attributes: models.JSONMap{
				"storage": map[string]interface{}{"file_type": "shp"},
			},
			Content: &models.ObjectPreviewContent{
				Kind: "text",
			},
		},
	}
	if !shouldBuildShapefileTablePreview(preview) {
		t.Fatalf("expected shapefile detection by file_type")
	}

	preview = &models.TablePreview{
		Object: &models.ObjectPreview{
			Attributes: models.JSONMap{},
			Content: &models.ObjectPreviewContent{
				Kind: "shapefile",
			},
		},
	}
	if !shouldBuildShapefileTablePreview(preview) {
		t.Fatalf("expected shapefile detection by content kind")
	}
}

func TestBuildShapefileTableRows(t *testing.T) {
	t.Parallel()
	content := &models.ObjectPreviewContent{
		GeoJSON: map[string]interface{}{
			"type": "FeatureCollection",
			"features": []interface{}{
				map[string]interface{}{
					"type": "Feature",
					"geometry": map[string]interface{}{
						"type":        "Point",
						"coordinates": []interface{}{120.1, 30.2},
					},
					"properties": map[string]interface{}{
						"name": "A",
						"code": 1,
					},
				},
			},
		},
		Metadata: map[string]interface{}{
			"source_srid": 3857,
		},
	}

	cols, rows, geomCols, renderCols, srid, ok := buildShapefileTableRows(content)
	if !ok {
		t.Fatalf("expected buildShapefileTableRows success")
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if len(geomCols) != 1 || geomCols[0] != shapefileGeometryColumn {
		t.Fatalf("unexpected geometry columns: %+v", geomCols)
	}
	if renderCols[shapefileGeometryColumn] != renderGeometryColumnName(shapefileGeometryColumn) {
		t.Fatalf("unexpected render geometry columns: %+v", renderCols)
	}
	if srid != 3857 {
		t.Fatalf("expected srid 3857, got %d", srid)
	}
	if _, exists := rows[0][shapefileGeometryColumn]; !exists {
		t.Fatalf("expected geometry field in row")
	}
	if _, exists := rows[0][renderGeometryColumnName(shapefileGeometryColumn)]; !exists {
		t.Fatalf("expected render geometry field in row")
	}

	hasName := false
	hasGeometry := false
	hasRenderGeometry := false
	for _, c := range cols {
		if c == "name" {
			hasName = true
		}
		if c == shapefileGeometryColumn {
			hasGeometry = true
		}
		if c == renderGeometryColumnName(shapefileGeometryColumn) {
			hasRenderGeometry = true
		}
	}
	if !hasName || !hasGeometry || !hasRenderGeometry {
		t.Fatalf("expected columns include name and geometry, got %+v", cols)
	}
}

func TestApplyShapefileTablePreview(t *testing.T) {
	t.Parallel()

	preview := &models.TablePreview{
		Mode:            PreviewModeObject,
		Columns:         []string{},
		Rows:            []map[string]interface{}{},
		GeometryColumns: []string{},
		Object: &models.ObjectPreview{
			Content: &models.ObjectPreviewContent{
				Kind: "shapefile",
				GeoJSON: map[string]interface{}{
					"type": "FeatureCollection",
					"features": []interface{}{
						map[string]interface{}{
							"type": "Feature",
							"id":   "feature-1",
							"geometry": map[string]interface{}{
								"type":        "Point",
								"coordinates": []interface{}{116.4, 39.9},
							},
							"properties": map[string]interface{}{
								"name": "A",
							},
						},
					},
				},
				Metadata: map[string]interface{}{
					"source_srid":           "32650",
					"preview_feature_count": float64(1),
				},
			},
		},
	}

	applyShapefileTablePreview(preview)

	if preview.Total != 1 || preview.PageSize != 1 {
		t.Fatalf("unexpected total/page_size: %d/%d", preview.Total, preview.PageSize)
	}
	if preview.SRID != 32650 {
		t.Fatalf("expected srid 32650, got %d", preview.SRID)
	}
	renderColumn := renderGeometryColumnName(shapefileGeometryColumn)
	if preview.RenderGeometryColumns[shapefileGeometryColumn] != renderColumn {
		t.Fatalf("unexpected render geometry columns: %+v", preview.RenderGeometryColumns)
	}
	if got := preview.Rows[0][renderColumn]; got == nil {
		t.Fatalf("expected render geometry value")
	}
	if got := preview.Rows[0]["__feature_id"]; got != "feature-1" {
		t.Fatalf("expected feature id, got %#v", got)
	}
}

func TestBuildShapefileTableRowsFallbackWhenNoFeatures(t *testing.T) {
	t.Parallel()
	content := &models.ObjectPreviewContent{
		Text: "Shapefile 文件较大，已跳过全量下载",
	}
	_, rows, _, _, _, ok := buildShapefileTableRows(content)
	if ok {
		t.Fatalf("expected buildShapefileTableRows fallback when no features")
	}
	if len(rows) != 0 {
		t.Fatalf("expected no rows when no features")
	}
}

func TestResolveShapefilePreviewSRID(t *testing.T) {
	t.Parallel()

	content := &models.ObjectPreviewContent{
		Metadata: map[string]interface{}{
			"source_srid": 3857,
		},
	}
	if got := resolveShapefilePreviewSRID(content); got != 3857 {
		t.Fatalf("expected 3857, got %d", got)
	}
}
