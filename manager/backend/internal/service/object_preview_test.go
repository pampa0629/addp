package service

import (
	"testing"

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

func TestShouldBuildShapefileTablePreview(t *testing.T) {
	t.Parallel()
	preview := &models.TablePreview{
		Object: &models.ObjectPreview{
			Attributes: models.JSONMap{"file_type": "shp"},
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
	if srid != 0 {
		t.Fatalf("expected srid 0 by default, got %d", srid)
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
