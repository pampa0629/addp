package objectcontent

import (
	"context"
	"testing"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/manager/internal/models"
)

func TestCADContentRequiresManagedPreviewArtifact(t *testing.T) {
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
	if content.Metadata["preview_artifact_task_type"] != commonExecution.TaskTypeCADPreviewGeneration {
		t.Fatalf("task type = %#v", content.Metadata["preview_artifact_task_type"])
	}
}

func TestCADContentUsesManifestURLOnlyWhenArtifactReady(t *testing.T) {
	handler := buildCADContentHandler(ObjectContentPluginConfig{Name: "cad", Builtin: models.ObjectPreviewKindCAD})
	content, _, err := handler.Handle(context.Background(), &ObjectContentRequest{
		Name: "drawing.dwg", Format: "dwg", PreviewURL: "/api/v1/manager/cad-previews/9/manifest",
	}, nil)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if content.PreviewMaterial != models.PreviewMaterialURL || content.FrontendRenderer != models.ObjectPreviewKindCAD {
		t.Fatalf("content = %#v", content)
	}
	if content.URL != "/api/v1/manager/cad-previews/9/manifest" {
		t.Fatalf("url = %q", content.URL)
	}
}
