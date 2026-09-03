package objectcontent

import (
	"context"
	"testing"

	"github.com/addp/manager/internal/models"
)

func TestCADContentRequiresSourceURL(t *testing.T) {
	handler := buildCADContentHandler(ObjectContentPluginConfig{Name: "cad", Builtin: models.ObjectPreviewKindCAD})
	content, _, err := handler.Handle(context.Background(), &ObjectContentRequest{
		Name: "drawing.dwg", Format: "dwg",
		Attributes: map[string]interface{}{"item": map[string]interface{}{"data_type": "cad", "format": "dwg"}},
	}, nil)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if content.Kind != models.ObjectPreviewKindCAD || content.PreviewMaterial != models.PreviewMaterialUnsupported {
		t.Fatalf("content = %#v", content)
	}
	if content.FrontendRenderer != models.ObjectPreviewKindUnsupported {
		t.Fatalf("frontend renderer = %q, want unsupported", content.FrontendRenderer)
	}
	if content.Metadata["preview_reason"] != "preview_url_missing" {
		t.Fatalf("preview reason = %#v", content.Metadata["preview_reason"])
	}
}

func TestCADContentUsesSourceStorageStreamURL(t *testing.T) {
	handler := buildCADContentHandler(ObjectContentPluginConfig{Name: "cad", Builtin: models.ObjectPreviewKindCAD})
	content, _, err := handler.Handle(context.Background(), &ObjectContentRequest{
		Name: "drawing.dwg", Format: "dwg", PreviewURL: "/api/v1/manager/storage-stream?engine_id=9&storage_ref=drawings%2Fdrawing.dwg",
	}, nil)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if content.PreviewMaterial != models.PreviewMaterialURL || content.FrontendRenderer != models.ObjectPreviewKindCAD {
		t.Fatalf("content = %#v", content)
	}
	if content.URL != "/api/v1/manager/storage-stream?engine_id=9&storage_ref=drawings%2Fdrawing.dwg" {
		t.Fatalf("url = %q", content.URL)
	}
}

func TestCADContentAcceptsDXF(t *testing.T) {
	handler := buildCADContentHandler(ObjectContentPluginConfig{Name: "cad", Builtin: models.ObjectPreviewKindCAD})
	content, _, err := handler.Handle(context.Background(), &ObjectContentRequest{Name: "drawing.dxf", Format: "dxf"}, nil)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if content.Kind != models.ObjectPreviewKindCAD || content.Metadata["source_format"] != "dxf" {
		t.Fatalf("content = %#v", content)
	}
}
