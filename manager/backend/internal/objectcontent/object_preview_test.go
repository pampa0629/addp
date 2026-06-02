package objectcontent

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
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
			name:        "avif_extension_generic_type",
			objectPath:  "bucket/images/photo.avif",
			contentType: "application/octet-stream",
			expect:      "image/avif",
		},
		{
			name:        "heic_extension_generic_type",
			objectPath:  "bucket/images/photo.heic",
			contentType: "application/octet-stream",
			expect:      "image/heic",
		},
		{
			name:        "avro_extension_generic_type",
			objectPath:  "bucket/data/events.avro",
			contentType: "application/octet-stream",
			expect:      "application/avro",
		},
		{
			name:        "orc_extension_generic_type",
			objectPath:  "bucket/data/events.orc",
			contentType: "application/octet-stream",
			expect:      "application/orc",
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
			got := InferContentType(tc.objectPath, tc.contentType)
			if got != tc.expect {
				t.Fatalf("InferContentType(%q, %q) = %q, want %q", tc.objectPath, tc.contentType, got, tc.expect)
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

func TestNormalizeFormatsKeepsOnlyCanonicalKnownFormats(t *testing.T) {
	t.Parallel()

	got := normalizeFormats([]string{"pdf", ".csv", "application/json", "yml", "unknown"})
	want := []string{"pdf", "csv", "json"}
	if len(got) != len(want) {
		t.Fatalf("normalizeFormats() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("normalizeFormats()[%d] = %q, want %q; got %#v", i, got[i], want[i], got)
		}
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
	wps, err := buildBuiltinContentHandler(ObjectContentPluginConfig{Name: "wps", Builtin: "wps"})
	if err != nil {
		t.Fatalf("build wps handler: %v", err)
	}
	if !wps.Matches(&ObjectContentRequest{Extension: ".wps", ContentType: "application/kswps"}) {
		t.Fatalf("expected WPS handler to match descriptor MIME")
	}

	container, err := buildBuiltinContentHandler(ObjectContentPluginConfig{Name: "container", Builtin: "container"})
	if err != nil {
		t.Fatalf("build container handler: %v", err)
	}
	for _, req := range []*ObjectContentRequest{
		{Extension: ".xlsm", ContentType: "application/vnd.ms-excel.sheet.macroenabled.12"},
		{Extension: ".gpkg", ContentType: "application/geopackage+sqlite3"},
		{Extension: ".sqlite", ContentType: "application/x-sqlite3"},
	} {
		if !container.Matches(req) {
			t.Fatalf("expected container handler to match descriptor request: %#v", req)
		}
	}

	image, err := buildBuiltinContentHandler(ObjectContentPluginConfig{Name: "image", Builtin: "image"})
	if err != nil {
		t.Fatalf("build image handler: %v", err)
	}
	if !image.Matches(&ObjectContentRequest{Extension: ".avif", ContentType: "image/avif"}) {
		t.Fatalf("expected image handler to match media descriptor extension and MIME")
	}
	if !image.Matches(&ObjectContentRequest{Format: "jpg", Extension: ".jpg", ContentType: "image/jpeg"}) {
		t.Fatalf("expected image handler to normalize jpg format alias to jpeg")
	}

	audio, err := buildBuiltinContentHandler(ObjectContentPluginConfig{Name: "audio", Builtin: "audio"})
	if err != nil {
		t.Fatalf("build audio handler: %v", err)
	}
	if !audio.Matches(&ObjectContentRequest{Extension: ".flac", ContentType: "audio/flac"}) {
		t.Fatalf("expected audio handler to match media descriptor extension and MIME")
	}
}

func TestLoadObjectContentPluginsUsesDescriptorDefaults(t *testing.T) {
	registry := NewObjectContentRegistry()
	LoadObjectContentPlugins(registry, "../../plugins")

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
			want: "builtin:content-container",
		},
		{
			name: "image",
			req:  ObjectContentRequest{Extension: ".png", ContentType: "image/png"},
			want: "builtin:content-image",
		},
		{
			name: "video",
			req:  ObjectContentRequest{Extension: ".mp4", ContentType: "video/mp4"},
			want: "builtin:content-video",
		},
		{
			name: "audio",
			req:  ObjectContentRequest{Extension: ".mp3", ContentType: "audio/mpeg"},
			want: "builtin:content-audio",
		},
		{
			name: "parquet",
			req:  ObjectContentRequest{Extension: ".parquet", ContentType: "application/vnd.apache.parquet"},
			want: "builtin:content-table",
		},
		{
			name: "csv",
			req:  ObjectContentRequest{Extension: ".csv", ContentType: "text/csv"},
			want: "builtin:content-table",
		},
		{
			name: "spatial_json_uses_json_handler",
			req:  ObjectContentRequest{Format: "json", Extension: ".geojson", ContentType: "application/geo+json"},
			want: "builtin:content-json",
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

func TestLoadObjectContentPluginsRegistersBuiltinDefaultsWithoutFiles(t *testing.T) {
	registry := NewObjectContentRegistry()
	LoadObjectContentPlugins(registry, "")

	tests := []struct {
		name string
		req  ObjectContentRequest
		want string
	}{
		{
			name: "pdf",
			req:  ObjectContentRequest{Format: "pdf"},
			want: "builtin:content-pdf",
		},
		{
			name: "docx",
			req:  ObjectContentRequest{Extension: ".docx", ContentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
			want: "builtin:content-docx",
		},
		{
			name: "json",
			req:  ObjectContentRequest{Extension: ".json", ContentType: "application/json"},
			want: "builtin:content-json",
		},
		{
			name: "video",
			req:  ObjectContentRequest{Extension: ".mp4", ContentType: "video/mp4"},
			want: "builtin:content-video",
		},
		{
			name: "audio",
			req:  ObjectContentRequest{Extension: ".wav", ContentType: "audio/wav"},
			want: "builtin:content-audio",
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

func TestFallbackBuiltinContentPluginsCoverMediaDescriptorKinds(t *testing.T) {
	plugins := fallbackBuiltinContentPlugins()
	seen := make(map[string]bool, len(plugins))
	for _, plugin := range plugins {
		seen[plugin.Builtin] = true
	}
	for _, kind := range []string{
		models.ObjectPreviewKindImage,
		models.ObjectPreviewKindVideo,
		models.ObjectPreviewKindAudio,
	} {
		if !seen[kind] {
			t.Fatalf("fallback builtin content plugins missing media kind %q: %#v", kind, plugins)
		}
	}
}

func TestObjectContentRegistryReplacesHandlerWithSameName(t *testing.T) {
	registry := NewObjectContentRegistry()
	low, err := buildBuiltinContentHandler(ObjectContentPluginConfig{Name: "builtin:content-json", Builtin: "json"})
	if err != nil {
		t.Fatalf("build low priority handler: %v", err)
	}
	priority := 99
	high, err := buildBuiltinContentHandler(ObjectContentPluginConfig{Name: "builtin:content-json", Builtin: "json", Priority: &priority})
	if err != nil {
		t.Fatalf("build high priority handler: %v", err)
	}

	registry.Register(low)
	registry.Register(high)

	handler := registry.Resolve(&ObjectContentRequest{Format: "json"})
	if handler == nil {
		t.Fatal("expected json handler")
	}
	if handler.Priority() != priority {
		t.Fatalf("priority = %d, want %d", handler.Priority(), priority)
	}
	if len(registry.handlers) != 1 {
		t.Fatalf("handler count = %d, want 1", len(registry.handlers))
	}
}

func TestLoadObjectContentPluginsUsesFallbackDefaultsAndContentConfigOverrides(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "content.json")
	config := []byte(`{"content_plugins":[{"name":"builtin:content-image","type":"builtin","builtin":"image","max_bytes":4}]}`)
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("write content config: %v", err)
	}

	registry := NewObjectContentRegistry()
	LoadObjectContentPlugins(registry, dir)

	if handler := registry.Resolve(&ObjectContentRequest{Format: "pdf"}); handler == nil || handler.Name() != "builtin:content-pdf" {
		t.Fatalf("pdf handler = %#v, want fallback builtin:content-pdf", handler)
	}

	handler := registry.Resolve(&ObjectContentRequest{Extension: ".png", ContentType: "image/png", Size: 8})
	if handler == nil {
		t.Fatal("expected image handler")
	}
	content, truncated, err := handler.Handle(
		context.Background(),
		&ObjectContentRequest{Name: "photo.png", Extension: ".png", ContentType: "image/png", Size: 8},
		func(limit int64) ([]byte, bool, error) {
			if limit != 4 {
				t.Fatalf("limit = %d, want content config override 4", limit)
			}
			return []byte{0x89, 0x50, 0x4E, 0x47}, false, nil
		},
	)
	if err != nil {
		t.Fatalf("handle image: %v", err)
	}
	if truncated || content.Kind != "image" {
		t.Fatalf("content = %#v truncated=%v, want image without truncation", content, truncated)
	}
}

func TestLoadObjectContentPluginsRejectsLegacyDefaultContentPluginsField(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "content.json")
	config := []byte(`{"default_content_plugins":[{"name":"builtin:content-json","type":"builtin","builtin":"json"}]}`)
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("write content config: %v", err)
	}

	registry := NewObjectContentRegistry()
	LoadObjectContentPlugins(registry, dir)

	if handler := registry.Resolve(&ObjectContentRequest{Format: "json"}); handler != nil {
		t.Fatalf("legacy default_content_plugins config should not load fallback or requested handler, got %q", handler.Name())
	}
}

func TestLoadObjectContentPluginsRejectsProvidersField(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "content.json")
	config := []byte(`{"providers":[]}`)
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("write content config: %v", err)
	}

	registry := NewObjectContentRegistry()
	LoadObjectContentPlugins(registry, dir)

	if handler := registry.Resolve(&ObjectContentRequest{Format: "json"}); handler != nil {
		t.Fatalf("content config with providers should not load fallback handlers, got %q", handler.Name())
	}
}

func TestVideoContentHandlerReturnsURLMaterial(t *testing.T) {
	t.Parallel()
	handler, err := buildBuiltinContentHandler(ObjectContentPluginConfig{Name: "video", Builtin: "video"})
	if err != nil {
		t.Fatalf("build video handler: %v", err)
	}
	content, truncated, err := handler.Handle(
		context.Background(),
		&ObjectContentRequest{
			Name:        "clip.mp4",
			Extension:   ".mp4",
			ContentType: "video/mp4",
			PreviewURL:  "/api/v1/manager/storage-stream?engine_id=1&storage_ref=bucket/clip.mp4",
		},
		func(limit int64) ([]byte, bool, error) {
			t.Fatalf("video handler must not read video bytes")
			return nil, false, nil
		},
	)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if truncated {
		t.Fatalf("truncated = true, want false")
	}
	if content.Kind != "video" || content.PreviewMaterial != "url" || content.FrontendRenderer != "video" || content.URL == "" {
		t.Fatalf("content = %#v, want URL material with video renderer", content)
	}
}

func TestAudioContentHandlerReturnsURLMaterial(t *testing.T) {
	t.Parallel()
	handler, err := buildBuiltinContentHandler(ObjectContentPluginConfig{Name: "audio", Builtin: "audio"})
	if err != nil {
		t.Fatalf("build audio handler: %v", err)
	}
	req := &ObjectContentRequest{
		Name:        "song.flac",
		Extension:   ".flac",
		ContentType: "audio/flac",
		PreviewURL:  "/api/v1/manager/storage-stream?engine_id=1&storage_ref=bucket/song.flac",
	}
	if !handler.Matches(req) {
		t.Fatalf("expected audio handler to match FLAC descriptor")
	}
	content, truncated, err := handler.Handle(
		context.Background(),
		req,
		func(limit int64) ([]byte, bool, error) {
			t.Fatalf("audio handler must not read audio bytes")
			return nil, false, nil
		},
	)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if truncated {
		t.Fatalf("truncated = true, want false")
	}
	if content.Kind != "audio" || content.PreviewMaterial != "url" || content.FrontendRenderer != "audio" || content.URL == "" {
		t.Fatalf("content = %#v, want URL material with audio renderer", content)
	}
}

func TestJSONContentHandlerReturnsMapMaterialForSpatialJSON(t *testing.T) {
	t.Parallel()
	handler, err := buildBuiltinContentHandler(ObjectContentPluginConfig{Name: "json", Builtin: "json"})
	if err != nil {
		t.Fatalf("build json handler: %v", err)
	}
	payload := []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"Point","coordinates":[1,2]},"properties":{"name":"A"}}]}`)

	content, truncated, err := handler.Handle(
		context.Background(),
		&ObjectContentRequest{Name: "roads.geojson", Format: "json", ContentType: "application/geo+json", Size: int64(len(payload))},
		func(limit int64) ([]byte, bool, error) {
			return payload, false, nil
		},
	)
	if err != nil {
		t.Fatalf("handle spatial json: %v", err)
	}
	if truncated {
		t.Fatalf("unexpected truncation")
	}
	if content.Kind != "json" {
		t.Fatalf("Kind = %q, want json", content.Kind)
	}
	if content.PreviewMaterial != "geojson" || content.FrontendRenderer != "map" {
		t.Fatalf("content = %#v, want geojson material with map renderer", content)
	}
	if content.GeoJSON == nil {
		t.Fatalf("expected GeoJSON preview payload")
	}
}

func TestJSONContentHandlerKeepsPlainJSONRenderer(t *testing.T) {
	t.Parallel()
	handler, err := buildBuiltinContentHandler(ObjectContentPluginConfig{Name: "json", Builtin: "json"})
	if err != nil {
		t.Fatalf("build json handler: %v", err)
	}
	payload := []byte(`{"name":"ADDP","kind":"config"}`)

	content, truncated, err := handler.Handle(
		context.Background(),
		&ObjectContentRequest{Name: "config.json", Format: "json", ContentType: "application/json", Size: int64(len(payload))},
		func(limit int64) ([]byte, bool, error) {
			return payload, false, nil
		},
	)
	if err != nil {
		t.Fatalf("handle plain json: %v", err)
	}
	if truncated {
		t.Fatalf("unexpected truncation")
	}
	if content.Kind != "json" || content.PreviewMaterial != "json" || content.FrontendRenderer != "json" {
		t.Fatalf("content = %#v, want json material with json renderer", content)
	}
	if content.GeoJSON != nil {
		t.Fatalf("plain json must not return GeoJSON payload: %#v", content.GeoJSON)
	}
}

func TestImageContentHandlerReturnsURLMaterial(t *testing.T) {
	t.Parallel()
	handler, err := buildBuiltinContentHandler(ObjectContentPluginConfig{Name: "image", Builtin: "image"})
	if err != nil {
		t.Fatalf("build image handler: %v", err)
	}
	content, truncated, err := handler.Handle(
		context.Background(),
		&ObjectContentRequest{
			Name:        "photo.avif",
			Extension:   ".avif",
			ContentType: "image/avif",
			Size:        1024,
			PreviewURL:  "/api/v1/manager/storage-stream?engine_id=1&storage_ref=bucket/photo.avif",
		},
		func(limit int64) ([]byte, bool, error) {
			t.Fatalf("image handler must not read image bytes")
			return nil, false, nil
		},
	)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if truncated {
		t.Fatalf("truncated = true, want false")
	}
	if content == nil {
		t.Fatal("content is nil")
	}
	if content.URL == "" || content.PreviewMaterial != "url" || content.FrontendRenderer != "image" {
		t.Fatalf("content = %#v, want URL material with image renderer", content)
	}
	if content.Encoding == "base64" {
		t.Fatalf("image handler should not return base64 image data: %#v", content)
	}
}

func TestImageContentHandlerDoesNotTrustAttributeURL(t *testing.T) {
	t.Parallel()
	handler, err := buildBuiltinContentHandler(ObjectContentPluginConfig{Name: "image", Builtin: "image"})
	if err != nil {
		t.Fatalf("build image handler: %v", err)
	}
	data := []byte{0xFF, 0xD8, 0xFF}
	content, _, err := handler.Handle(
		context.Background(),
		&ObjectContentRequest{
			Name:        "photo.jpg",
			Extension:   ".jpg",
			ContentType: "image/jpeg",
			Attributes: map[string]interface{}{
				"preview_url": "http://minio.internal/addp/image/photo.jpg",
				"url":         "http://example.invalid/photo.jpg",
			},
		},
		func(limit int64) ([]byte, bool, error) {
			return data, false, nil
		},
	)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if content == nil {
		t.Fatal("content is nil")
	}
	if content.URL != "" || content.PreviewMaterial == "url" {
		t.Fatalf("image handler trusted attribute URL: %#v", content)
	}
	if content.Encoding != "base64" || content.Data != base64.StdEncoding.EncodeToString(data) {
		t.Fatalf("image handler did not return fetched image bytes: %#v", content)
	}
}

func TestImageContentHandlerReturnsRawBinaryMaterialWithoutURL(t *testing.T) {
	t.Parallel()
	handler, err := buildBuiltinContentHandler(ObjectContentPluginConfig{Name: "image", Builtin: "image"})
	if err != nil {
		t.Fatalf("build image handler: %v", err)
	}
	data := []byte{0x89, 0x50, 0x4E, 0x47}
	content, truncated, err := handler.Handle(
		context.Background(),
		&ObjectContentRequest{Name: "photo.png", Extension: ".png", ContentType: "image/png", Size: int64(len(data))},
		func(limit int64) ([]byte, bool, error) {
			if limit <= 0 {
				t.Fatalf("expected positive image read limit")
			}
			return data, false, nil
		},
	)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if truncated {
		t.Fatalf("truncated = true, want false")
	}
	if content.Kind != "image" || content.PreviewMaterial != "raw_binary" || content.FrontendRenderer != "image" {
		t.Fatalf("content = %#v, want raw image material with image renderer", content)
	}
	if content.Encoding != "base64" || content.Data != base64.StdEncoding.EncodeToString(data) {
		t.Fatalf("unexpected encoded image data: %#v", content)
	}
}

func TestObjectContentTableFormatUsesExplicitFormat(t *testing.T) {
	if got := objectContentTableFormat(&ObjectContentRequest{Format: "orc", Extension: ".bin", ContentType: "application/octet-stream"}); got != format.FormatORC {
		t.Fatalf("objectContentTableFormat() = %q, want %q", got, format.FormatORC)
	}
}

func TestObjectContentTableFormatKeepsUnknownUnknown(t *testing.T) {
	if got := objectContentTableFormat(&ObjectContentRequest{Name: "blob.bin", Extension: ".bin", ContentType: "application/octet-stream"}); got != format.FormatUnknown {
		t.Fatalf("objectContentTableFormat() = %q, want %q", got, format.FormatUnknown)
	}
	if got := objectContentTableFormat(nil); got != format.FormatUnknown {
		t.Fatalf("objectContentTableFormat(nil) = %q, want %q", got, format.FormatUnknown)
	}
}

func TestBuildContainerPreviewFromExcelAttributes(t *testing.T) {
	preview := buildContainerPreviewFromMetaAttributes(map[string]interface{}{
		"item": map[string]interface{}{
			"format": "excel",
		},
		"type_info": map[string]interface{}{
			"container": map[string]interface{}{
				"children": []interface{}{
					map[string]interface{}{
						"name":         "Cities",
						"child_kind":   "sheet",
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
		t.Fatal("buildContainerPreviewFromMetaAttributes() returned nil")
	}
	if preview["format"] != "excel" || preview["default_child"] != "Cities" || preview["active_child"] != "Cities" {
		t.Fatalf("container header = %#v, want excel/Cities", preview)
	}
	children, ok := preview["children"].([]map[string]interface{})
	if !ok || len(children) != 1 {
		t.Fatalf("children = %#v, want one child", preview["children"])
	}
	if children[0]["name"] != "Cities" || children[0]["child_kind"] != "sheet" {
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

func TestBuildContainerPreviewFromMetaAttributesNormalizesParentFormat(t *testing.T) {
	preview := buildContainerPreviewFromMetaAttributes(map[string]interface{}{
		"item": map[string]interface{}{
			"format": ".zip",
		},
		"type_info": map[string]interface{}{
			"container": map[string]interface{}{
				"children": []interface{}{
					map[string]interface{}{
						"name":       "readme.txt",
						"child_kind": "file",
						"data_type":  "document",
						"format":     "text",
					},
				},
			},
		},
		"format_info": map[string]interface{}{
			"zip": map[string]interface{}{
				"default_child": "readme.txt",
			},
		},
	}, 0)

	if preview == nil {
		t.Fatal("buildContainerPreviewFromMetaAttributes() returned nil")
	}
	if preview["format"] != string(format.FormatZIP) {
		t.Fatalf("preview format = %#v, want zip", preview["format"])
	}
	if preview["default_child"] != "readme.txt" {
		t.Fatalf("default_child = %#v, want readme.txt from canonical format_info.zip", preview["default_child"])
	}
}

func TestBuildContainerPreviewFromMetaAttributesDropsUnknownParentFormat(t *testing.T) {
	preview := buildContainerPreviewFromMetaAttributes(map[string]interface{}{
		"item": map[string]interface{}{
			"format": "yml",
		},
		"type_info": map[string]interface{}{
			"container": map[string]interface{}{
				"children": []interface{}{
					map[string]interface{}{
						"name":       "readme.txt",
						"child_kind": "file",
						"data_type":  "document",
						"format":     "text",
					},
				},
			},
		},
	}, 0)

	if preview == nil {
		t.Fatal("buildContainerPreviewFromMetaAttributes() returned nil")
	}
	if preview["format"] != "" {
		t.Fatalf("preview format = %#v, want empty for unknown parent format", preview["format"])
	}
}

func TestBuildContainerPreviewFromSQLiteAttributes(t *testing.T) {
	preview := buildContainerPreviewFromMetaAttributes(map[string]interface{}{
		"item": map[string]interface{}{
			"format": "sqlite",
		},
		"type_info": map[string]interface{}{
			"container": map[string]interface{}{
				"child_count": int64(1),
				"children": []interface{}{
					map[string]interface{}{
						"name":       "Cities",
						"table":      "city_table",
						"child_kind": "table",
						"data_type":  "table",
						"row_count":  int64(7),
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
		t.Fatal("buildContainerPreviewFromMetaAttributes() returned nil")
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

func TestResolveContainerChildrenForPreviewGroupsShapefileRefs(t *testing.T) {
	info := &datatype.ContainerInfo{
		ChildCount:   5,
		DefaultChild: "roads.shp",
		Children: []datatype.ContainerChildInfo{
			{Name: "roads.shp", ChildKind: "file", DataType: "table", Native: map[string]interface{}{"path": "roads.shp", "format": "shapefile", "uncompressed_size": int64(10)}},
			{Name: "roads.shx", ChildKind: "file", DataType: "table", Native: map[string]interface{}{"path": "roads.shx", "format": "shapefile", "uncompressed_size": int64(10)}},
			{Name: "roads.dbf", ChildKind: "file", DataType: "table", Native: map[string]interface{}{"path": "roads.dbf", "format": "shapefile", "uncompressed_size": int64(10)}},
			{Name: "roads.prj", ChildKind: "file", DataType: "table", Native: map[string]interface{}{"path": "roads.prj", "format": "shapefile", "uncompressed_size": int64(10)}},
			{Name: "readme.md", ChildKind: "file", DataType: "document", Native: map[string]interface{}{"path": "readme.md", "format": "markdown"}},
		},
	}

	resolved, _ := resolveContainerChildrenForPreview(info)
	if resolved == nil || len(resolved.Children) != 2 {
		t.Fatalf("children = %#v, want shapefile child + markdown child", resolved)
	}
	child := resolved.Children[0]
	if child.ChildKind != "multi" || child.Format != string(format.FormatShapefile) || len(child.Refs) != 4 {
		t.Fatalf("first child = %#v, want multi shapefile with refs", child)
	}
}

func TestContainerChildPreviewMapKeepsNormalizedRefs(t *testing.T) {
	child := containerChildPreviewMap(datatype.ContainerChildInfo{
		Name:      "roads.shp",
		ChildKind: "file",
		DataType:  "table",
		Format:    string(format.FormatShapefile),
		Refs: []datatype.ContainerChildRef{
			{Path: "roads.shp", Role: "main", Required: true, Primary: true},
			{Path: "roads.dbf", Role: "attributes", Required: true},
		},
		Native: map[string]interface{}{
			"refs": []interface{}{
				map[string]interface{}{"path": "raw-should-not-win.dbf"},
			},
		},
	}, 0)

	refs, ok := child["refs"].([]map[string]interface{})
	if !ok || len(refs) != 2 {
		t.Fatalf("refs = %#v, want normalized descriptors", child["refs"])
	}
	if refs[0]["path"] != "roads.shp" || refs[0]["label"] == "" {
		t.Fatalf("first ref = %#v, want described main ref", refs[0])
	}
	if _, ok := child["ref_paths"]; ok {
		t.Fatalf("preview child should not carry ref_paths: %#v", child)
	}
}

func TestBuildContainerPreviewFromMetaAttributesKeepsResolvedMultiChild(t *testing.T) {
	preview := buildContainerPreviewFromMetaAttributes(map[string]interface{}{
		"format": "zip",
		"type_info": map[string]interface{}{
			"container": map[string]interface{}{
				"children": []interface{}{
					map[string]interface{}{
						"name":       "roads/roads.shp",
						"child_kind": "multi",
						"data_type":  "table",
						"format":     "shapefile",
						"layout":     "multi",
						"path":       "roads/roads.shp",
						"ref_paths":  map[string]interface{}{"main": "roads/roads.shp"},
						"refs": []interface{}{
							map[string]interface{}{"role": "main", "path": "roads/roads.shp", "required": true, "primary": true, "extension": ".shp"},
							map[string]interface{}{"role": "index", "path": "roads/roads.shx", "required": true, "extension": ".shx"},
							map[string]interface{}{"role": "attributes", "path": "roads/roads.dbf", "required": true, "extension": ".dbf"},
						},
					},
				},
			},
		},
	}, 0)

	children, ok := preview["children"].([]map[string]interface{})
	if !ok || len(children) != 1 {
		t.Fatalf("children = %#v, want one resolved child", preview["children"])
	}
	child := children[0]
	if child["layout"] != "multi" || child["child_kind"] != "multi" || child["format"] != "shapefile" {
		t.Fatalf("child = %#v, want resolved multi shapefile", child)
	}
	if refs, ok := child["refs"].([]map[string]interface{}); !ok || len(refs) != 3 {
		t.Fatalf("refs = %#v, want normalized descriptors", child["refs"])
	}
	if _, ok := child["ref_paths"]; ok {
		t.Fatalf("preview child should not carry ref_paths: %#v", child)
	}
}

func TestObjectContentRegistryResolvesCSVWithTableHandler(t *testing.T) {
	registry := NewObjectContentRegistry()
	LoadObjectContentPlugins(registry, "../../plugins")

	for _, tt := range []struct {
		name string
		req  ObjectContentRequest
	}{
		{name: "format", req: ObjectContentRequest{Format: "csv"}},
		{name: "extension", req: ObjectContentRequest{Extension: ".csv"}},
		{name: "content_type", req: ObjectContentRequest{ContentType: "text/csv"}},
		{name: "content_type_with_charset", req: ObjectContentRequest{ContentType: "text/csv; charset=utf-8"}},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			handler := registry.Resolve(&tt.req)
			if handler == nil {
				t.Fatalf("CSV object content request did not resolve")
			}
			if handler.Name() != "builtin:content-table" {
				t.Fatalf("CSV object content request resolved to %q, want builtin:content-table", handler.Name())
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

func TestObjectContentMatcherIgnoresUnqualifiedFormat(t *testing.T) {
	t.Parallel()
	matcher := newObjectContentMatcher(
		[]string{"pdf"},
		[]string{".pdf"},
		[]string{"application/pdf"},
	)
	req := &ObjectContentRequest{
		Format:      "yml",
		Extension:   ".pdf",
		ContentType: "application/pdf",
	}
	if !matcher.matches(req) {
		t.Fatalf("expected matcher to ignore unqualified format and fall back to extension/content type")
	}
}

func TestTextContentHandlerUsesExplicitTextMatcher(t *testing.T) {
	t.Parallel()
	handler, err := buildBuiltinContentHandler(ObjectContentPluginConfig{Name: "text", Builtin: "text"})
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
	handler, err := buildBuiltinContentHandler(ObjectContentPluginConfig{Name: "text", Builtin: "text"})
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

func TestUnsupportedContentHandlerTreatsUnknownTextAsBinary(t *testing.T) {
	t.Parallel()
	handler, err := buildBuiltinContentHandler(ObjectContentPluginConfig{Name: "unsupported", Builtin: "unsupported"})
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
	if content.Kind != "unsupported" {
		t.Fatalf("expected unsupported fallback for unknown content, got %q", content.Kind)
	}
	if content.PreviewMaterial != "unsupported" {
		t.Fatalf("PreviewMaterial = %q, want unsupported", content.PreviewMaterial)
	}
	if content.FrontendRenderer != "unsupported" {
		t.Fatalf("FrontendRenderer = %q, want unsupported", content.FrontendRenderer)
	}
	if content.Text == "hello\nworld\n" {
		t.Fatalf("unknown content must not be promoted to text preview")
	}
}

func TestUnsupportedContentHandlerKeepsBinaryUnsupported(t *testing.T) {
	t.Parallel()
	handler, err := buildBuiltinContentHandler(ObjectContentPluginConfig{Name: "unsupported", Builtin: "unsupported"})
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
	if content.PreviewMaterial != "unsupported" {
		t.Fatalf("PreviewMaterial = %q, want unsupported", content.PreviewMaterial)
	}
	if content.FrontendRenderer != "unsupported" {
		t.Fatalf("FrontendRenderer = %q, want unsupported", content.FrontendRenderer)
	}
	if content.Text != "" {
		t.Fatalf("Text = %q, want empty because unsupported is a preview state", content.Text)
	}
}

func TestUnsupportedContentHandlerDoesNotReportProbeTruncationAsPreviewTruncation(t *testing.T) {
	t.Parallel()
	handler, err := buildBuiltinContentHandler(ObjectContentPluginConfig{Name: "unsupported", Builtin: "unsupported"})
	if err != nil {
		t.Fatalf("build unsupported handler: %v", err)
	}

	content, truncated, err := handler.Handle(
		nil,
		&ObjectContentRequest{Name: "book.epub", Extension: ".epub", ContentType: "application/epub+zip", Size: 1024},
		func(limit int64) ([]byte, bool, error) {
			return []byte("probe"), true, nil
		},
	)
	if err != nil {
		t.Fatalf("handle binary fallback: %v", err)
	}
	if truncated || content.Truncated {
		t.Fatalf("truncated = %v content.Truncated = %v, want false because no original content is previewed", truncated, content.Truncated)
	}
	if content.Metadata["probe_truncated"] != true {
		t.Fatalf("metadata = %#v, want probe_truncated", content.Metadata)
	}
	if content.Metadata["binary_probe"] != true {
		t.Fatalf("metadata = %#v, want binary_probe", content.Metadata)
	}
}

func TestRawDocumentContentHandlerReturnsURLMaterialWhenAvailable(t *testing.T) {
	t.Parallel()
	handler, err := buildBuiltinContentHandler(ObjectContentPluginConfig{Name: "pdf", Builtin: "pdf"})
	if err != nil {
		t.Fatalf("build pdf handler: %v", err)
	}

	content, truncated, err := handler.Handle(
		nil,
		&ObjectContentRequest{
			Name:       "report.pdf",
			Format:     "pdf",
			Size:       1024,
			PreviewURL: "/api/v1/manager/storage-stream?engine_id=1&storage_ref=bucket/report.pdf",
		},
		func(limit int64) ([]byte, bool, error) {
			t.Fatalf("document URL preview should not read bytes")
			return nil, false, nil
		},
	)
	if err != nil {
		t.Fatalf("handle pdf content: %v", err)
	}
	if truncated {
		t.Fatalf("unexpected truncation")
	}
	if content.Kind != "pdf" {
		t.Fatalf("Kind = %q, want pdf", content.Kind)
	}
	if content.URL == "" || content.PreviewMaterial != "url" {
		t.Fatalf("content = %#v, want URL material", content)
	}
	if content.FrontendRenderer != "pdf" {
		t.Fatalf("FrontendRenderer = %q, want pdf", content.FrontendRenderer)
	}
	if content.Encoding == "base64" || content.Data != "" {
		t.Fatalf("URL preview should not return base64 data: %#v", content)
	}
}

func TestPreviewMetadataIncludesDisplayFactsFromAttributes(t *testing.T) {
	t.Parallel()
	metadata := buildPreviewMetadata(&ObjectContentRequest{
		Name: "report.pdf",
		Attributes: map[string]interface{}{
			"type_info": map[string]interface{}{
				"media": map[string]interface{}{
					"width":       1920,
					"height":      1080,
					"duration_ms": 4500,
					"encoding":    "h264",
					"color_space": "yuv420p",
				},
				"document": map[string]interface{}{
					"page_count": 12,
					"title":      "Annual Report",
				},
			},
			"format_info": map[string]interface{}{
				"pdf": map[string]interface{}{
					"author":  "ADDP",
					"creator": "manager",
				},
			},
		},
	}, 0)

	if metadata["width"] != 1920 || metadata["height"] != 1080 || metadata["duration_ms"] != 4500 {
		t.Fatalf("media metadata = %#v", metadata)
	}
	if metadata["page_count"] != 12 || metadata["title"] != "Annual Report" {
		t.Fatalf("document metadata = %#v", metadata)
	}
	if metadata["author"] != "ADDP" || metadata["creator"] != "manager" {
		t.Fatalf("pdf metadata = %#v", metadata)
	}
}

func TestRawDocumentContentHandlerDeclaresRawBinaryMaterialAndRendererWithoutURL(t *testing.T) {
	t.Parallel()
	handler, err := buildBuiltinContentHandler(ObjectContentPluginConfig{Name: "wps", Builtin: "wps"})
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

func TestDecoratePreviewContentIgnoresFormatContentReaderAsPreviewMaterial(t *testing.T) {
	t.Parallel()

	content := DecoratePreviewContent(&models.ObjectPreviewContent{
		Kind: models.ObjectPreviewKindUnsupported,
		Metadata: map[string]interface{}{
			"preview_material": "raw_content",
		},
	})

	if content.PreviewMaterial != models.PreviewMaterialUnsupported {
		t.Fatalf("PreviewMaterial = %q, want unsupported for invalid format content reader value", content.PreviewMaterial)
	}
	if got := content.Metadata["preview_material"]; got != "raw_content" {
		t.Fatalf("metadata preview_material = %#v, want original raw_content", got)
	}
}

func TestDecoratePreviewContentClearsTruncatedWhenNoPreviewMaterial(t *testing.T) {
	t.Parallel()

	content := DecoratePreviewContent(&models.ObjectPreviewContent{
		Kind:      models.ObjectPreviewKindJSON,
		Truncated: true,
	})

	if content.Truncated {
		t.Fatalf("Truncated = true, want false when no preview material exists")
	}
}

func TestDecoratePreviewContentKeepsTruncatedWithText(t *testing.T) {
	t.Parallel()

	content := DecoratePreviewContent(&models.ObjectPreviewContent{
		Kind:      models.ObjectPreviewKindText,
		Text:      "partial",
		Truncated: true,
	})

	if !content.Truncated {
		t.Fatalf("Truncated = false, want true when preview text exists")
	}
}

func TestDecoratePreviewContentSemanticMatrix(t *testing.T) {
	t.Parallel()

	testcases := []struct {
		name              string
		content           models.ObjectPreviewContent
		wantMaterial      string
		wantRenderer      string
		wantMetadata      string
		wantNoTopMaterial bool
	}{
		{
			name: "text",
			content: models.ObjectPreviewContent{
				Kind: models.ObjectPreviewKindText,
				Text: "hello",
			},
			wantMaterial: models.PreviewMaterialText,
			wantRenderer: models.ObjectPreviewKindText,
			wantMetadata: models.PreviewMaterialText,
		},
		{
			name: "markdown",
			content: models.ObjectPreviewContent{
				Kind: models.ObjectPreviewKindMarkdown,
				Text: "# title",
			},
			wantMaterial: models.PreviewMaterialMarkdown,
			wantRenderer: models.ObjectPreviewKindMarkdown,
			wantMetadata: models.PreviewMaterialMarkdown,
		},
		{
			name: "json",
			content: models.ObjectPreviewContent{
				Kind: models.ObjectPreviewKindJSON,
				JSON: map[string]interface{}{"name": "ADDP"},
			},
			wantMaterial: models.PreviewMaterialJSON,
			wantRenderer: models.ObjectPreviewKindJSON,
			wantMetadata: models.PreviewMaterialJSON,
		},
		{
			name: "geojson_map_renderer",
			content: models.ObjectPreviewContent{
				Kind:             models.ObjectPreviewKindJSON,
				PreviewMaterial:  models.PreviewMaterialGeoJSON,
				FrontendRenderer: "map",
				GeoJSON:          map[string]interface{}{"type": "FeatureCollection"},
			},
			wantMaterial: models.PreviewMaterialGeoJSON,
			wantRenderer: "map",
			wantMetadata: models.PreviewMaterialGeoJSON,
		},
		{
			name: "table",
			content: models.ObjectPreviewContent{
				Kind: models.ObjectPreviewKindTable,
				JSON: map[string]interface{}{
					"columns": []map[string]interface{}{{"name": "id", "type": "int"}},
					"rows":    []map[string]interface{}{{"id": 1}},
				},
			},
			wantMaterial: models.PreviewMaterialTable,
			wantRenderer: models.ObjectPreviewKindTable,
			wantMetadata: models.PreviewMaterialTable,
		},
		{
			name: "container",
			content: models.ObjectPreviewContent{
				Kind: models.ObjectPreviewKindContainer,
				JSON: map[string]interface{}{
					"format":   "zip",
					"children": []map[string]interface{}{{"key": "a.csv", "name": "a.csv"}},
				},
			},
			wantMaterial: models.PreviewMaterialContainer,
			wantRenderer: models.ObjectPreviewKindContainer,
			wantMetadata: models.PreviewMaterialContainer,
		},
		{
			name: "raw_binary",
			content: models.ObjectPreviewContent{
				Kind:     models.ObjectPreviewKindImage,
				Data:     "aGVsbG8=",
				Encoding: "base64",
			},
			wantMaterial: models.PreviewMaterialRawBinary,
			wantRenderer: models.ObjectPreviewKindImage,
			wantMetadata: models.PreviewMaterialRawBinary,
		},
		{
			name: "url",
			content: models.ObjectPreviewContent{
				Kind: models.ObjectPreviewKindPDF,
				URL:  "/api/v1/manager/storage-stream?engine_id=1&storage_ref=bucket/report.pdf",
			},
			wantMaterial: models.PreviewMaterialURL,
			wantRenderer: models.ObjectPreviewKindPDF,
			wantMetadata: models.PreviewMaterialURL,
		},
		{
			name: "unsupported",
			content: models.ObjectPreviewContent{
				Kind: models.ObjectPreviewKindUnsupported,
			},
			wantMaterial: models.PreviewMaterialUnsupported,
			wantRenderer: models.ObjectPreviewKindUnsupported,
			wantMetadata: models.PreviewMaterialUnsupported,
		},
		{
			name: "invalid_material_from_metadata",
			content: models.ObjectPreviewContent{
				Kind: models.ObjectPreviewKindUnsupported,
				Metadata: map[string]interface{}{
					"preview_material": "raw_content",
				},
			},
			wantMaterial: models.PreviewMaterialUnsupported,
			wantRenderer: models.ObjectPreviewKindUnsupported,
			wantMetadata: "raw_content",
		},
	}

	for _, tc := range testcases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			content := DecoratePreviewContent(&tc.content)
			if content.PreviewMaterial != tc.wantMaterial {
				t.Fatalf("PreviewMaterial = %q, want %q", content.PreviewMaterial, tc.wantMaterial)
			}
			if tc.wantNoTopMaterial && content.PreviewMaterial != "" {
				t.Fatalf("PreviewMaterial = %q, want empty", content.PreviewMaterial)
			}
			if content.FrontendRenderer != tc.wantRenderer {
				t.Fatalf("FrontendRenderer = %q, want %q", content.FrontendRenderer, tc.wantRenderer)
			}
			if tc.wantMetadata != "" && content.Metadata["preview_material"] != tc.wantMetadata {
				t.Fatalf("metadata preview_material = %#v, want %q", content.Metadata["preview_material"], tc.wantMetadata)
			}
		})
	}
}

func TestMarkdownHandlerDeclaresMarkdownMaterialAndRenderer(t *testing.T) {
	t.Parallel()
	handler, err := buildBuiltinContentHandler(ObjectContentPluginConfig{Name: "markdown", Builtin: "markdown"})
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
	handler, err := buildBuiltinContentHandler(ObjectContentPluginConfig{Name: "markdown", Builtin: "markdown", MaxBytes: &maxBytes})
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
