package service

import (
	"testing"

	"github.com/addp/common/format"
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

func TestBuildContainerPreviewFromExcelAttributes(t *testing.T) {
	preview := buildContainerPreviewFromAttributes(map[string]interface{}{
		"item": map[string]interface{}{
			"format": "excel",
		},
		"type_info": map[string]interface{}{
			"container": map[string]interface{}{
				"children": []interface{}{
					map[string]interface{}{
						"name":         "Cities",
						"kind":         "sheet",
						"row_count":    int64(7),
						"column_count": int64(2),
						"has_header":   true,
					},
				},
			},
		},
		"format_info": map[string]interface{}{
			"excel": map[string]interface{}{
				"default_sheet": "Cities",
				"sheet_count":   int64(1),
			},
		},
	}, 1024)

	if preview == nil {
		t.Fatal("buildContainerPreviewFromAttributes() returned nil")
	}
	if preview["format"] != "excel" || preview["default_child"] != "Cities" || preview["active_child"] != "Cities" {
		t.Fatalf("container header = %#v, want excel/Cities", preview)
	}
	children, ok := preview["children"].([]map[string]interface{})
	if !ok || len(children) != 1 {
		t.Fatalf("children = %#v, want one child", preview["children"])
	}
	if children[0]["name"] != "Cities" || children[0]["kind"] != "sheet" {
		t.Fatalf("child summary = %#v, want Cities sheet", children[0])
	}
	if _, ok := preview["sheets"]; ok {
		t.Fatalf("container preview should not carry legacy sheets: %#v", preview)
	}
	if _, ok := preview["default_sheet"]; ok {
		t.Fatalf("container preview should not carry legacy default_sheet: %#v", preview)
	}
	summary, ok := preview["summary"].(map[string]interface{})
	if !ok || summary["size_bytes"] != int64(1024) {
		t.Fatalf("summary = %#v, want size_bytes 1024", preview["summary"])
	}
}

func TestBuildContainerPreviewFromSQLiteAttributes(t *testing.T) {
	preview := buildContainerPreviewFromAttributes(map[string]interface{}{
		"item": map[string]interface{}{
			"format": "sqlite",
		},
		"type_info": map[string]interface{}{
			"container": map[string]interface{}{
				"child_count": int64(1),
				"children": []interface{}{
					map[string]interface{}{
						"name":      "Cities",
						"table":     "city_table",
						"kind":      "table",
						"data_type": "table",
						"row_count": int64(7),
					},
				},
			},
		},
		"format_info": map[string]interface{}{
			"sqlite": map[string]interface{}{
				"table_count":      int64(1),
				"sampled_children": int64(1),
			},
		},
	}, 2048)

	if preview == nil {
		t.Fatal("buildContainerPreviewFromAttributes() returned nil")
	}
	if preview["format"] != "sqlite" || preview["default_child"] != "Cities" {
		t.Fatalf("container header = %#v, want sqlite/Cities", preview)
	}
	children, ok := preview["children"].([]map[string]interface{})
	if !ok || len(children) != 1 {
		t.Fatalf("children = %#v, want one child", preview["children"])
	}
	if children[0]["table"] != "city_table" || children[0]["row_count"] != int64(7) {
		t.Fatalf("child = %#v, want city_table row_count 7", children[0])
	}
	if _, ok := preview["tables"]; ok {
		t.Fatalf("container preview should not carry legacy tables: %#v", preview)
	}
	if _, ok := preview["default_table"]; ok {
		t.Fatalf("container preview should not carry legacy default_table: %#v", preview)
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
