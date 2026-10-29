package preview

import (
	"testing"

	"github.com/addp/manager/internal/models"
)

func TestWholeContainerBasicIndexIsUnknownNotEmpty(t *testing.T) {
	req := &PreviewRequest{ScopePath: "exports/public_test.gdb", ScannedDepth: "basic", Attributes: map[string]interface{}{
		"item":      map[string]interface{}{"layout": "whole", "data_type": "container", "format": "filegdb"},
		"storage":   map[string]interface{}{"total_size": int64(24909470)},
		"type_info": map[string]interface{}{"container": map[string]interface{}{"child_count": 0, "children": []interface{}{}}},
	}}
	object := &models.ObjectPreview{}
	if !applyWholeContainerPreview(req, object) || object.Content == nil || object.Content.Kind != models.ObjectPreviewKindContainer {
		t.Fatalf("whole basic container must have an index placeholder: %#v", object)
	}
	value := object.Content.JSON.(map[string]interface{})
	summary := value["summary"].(map[string]interface{})
	if summary["index_available"] != false || summary["child_count"] != nil || object.Path != req.ScopePath || object.SizeBytes != 24909470 {
		t.Fatalf("unknown index must not claim an empty container: %#v / %#v", value, object)
	}
	req.ScannedDepth = "deep"
	object = &models.ObjectPreview{}
	applyWholeContainerPreview(req, object)
	if object.Content.JSON.(map[string]interface{})["summary"].(map[string]interface{})["index_available"] != true {
		t.Fatal("deep observed empty index lost")
	}
	delete(req.Attributes, "type_info")
	object = &models.ObjectPreview{}
	applyWholeContainerPreview(req, object)
	if object.Content.JSON.(map[string]interface{})["summary"].(map[string]interface{})["index_available"] != false {
		t.Fatal("deep flag alone fabricated an empty index")
	}
	for _, mutate := range []func(*PreviewRequest){
		func(r *PreviewRequest) { r.ChildName = "new_feature_class" },
		func(r *PreviewRequest) { r.RefPath = "outside.gdb" },
		func(r *PreviewRequest) { r.NestedChildPath = "nested" },
	} {
		copy := *req
		mutate(&copy)
		if applyWholeContainerPreview(&copy, &models.ObjectPreview{}) {
			t.Fatal("a child/content request must not be satisfied by parent metadata")
		}
	}
}
