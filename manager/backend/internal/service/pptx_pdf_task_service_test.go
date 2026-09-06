package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
)

type fakePPTXPDFMetaClient struct {
	item *commonModels.MetaItem
	err  error
}

func (f fakePPTXPDFMetaClient) GetItemByIDForTenant(_, _ uint) (*commonModels.MetaItem, error) {
	return f.item, f.err
}

func TestNormalizePPTXPDFTaskBuildsStableManagedTarget(t *testing.T) {
	updatedAt := time.Date(2026, time.April, 3, 12, 0, 0, 0, time.UTC)
	size := int64(374_334_451)
	task := &models.PPTXPDFTask{
		TenantID: 7,
		Locator:  "addp://engine/12/path/addp/doc/slides.pptx?type=object&item_id=77",
	}
	meta := fakePPTXPDFMetaClient{item: &commonModels.MetaItem{
		ID: 77, TenantID: 7, EngineID: 12, ItemType: "object", Name: "slides.pptx",
		FullName: "addp/doc/slides.pptx", ObjectSizeBytes: &size, DataUpdatedAt: &updatedAt,
	}}

	if err := resolvePPTXPDFTaskSource(meta, task, "manager"); err != nil {
		t.Fatalf("resolvePPTXPDFTaskSource() error = %v", err)
	}
	if task.Name != "slides.pptx" || task.ItemID != 77 || task.SourceEngineID != 12 {
		t.Fatalf("normalized identity = name %q, item_id %d", task.Name, task.ItemID)
	}
	expectedFingerprint := commonModels.GenerateItemFingerprint(12, "addp/doc/slides.pptx")
	if task.ItemFingerprint != expectedFingerprint || task.SourceVersion == "" || task.SourceSizeBytes != size {
		t.Fatalf("server identity = fingerprint %q, version %q, size %d", task.ItemFingerprint, task.SourceVersion, task.SourceSizeBytes)
	}
	if task.ArtifactVariant != models.PPTXPDFArtifactVariant {
		t.Fatalf("artifact variant = %q", task.ArtifactVariant)
	}
	result, ok := asJSONMap(task.Config["result"])
	if !ok {
		t.Fatalf("result config = %#v", task.Config["result"])
	}
	storageRef := stringFromConfig(result["storage_ref"])
	if !strings.Contains(storageRef, "tenant_7/document-preview/"+expectedFingerprint+"/slides.pdf") {
		t.Fatalf("storage_ref = %q", storageRef)
	}
	options, ok := asJSONMap(task.Config["options"])
	if !ok || options["strip_embedded_media"] != true {
		t.Fatalf("options = %#v", task.Config["options"])
	}
}

func TestNormalizePPTXPDFTaskRejectsWrongSourceIdentity(t *testing.T) {
	tests := []struct {
		name    string
		locator string
		itemID  uint
		want    string
	}{
		{name: "engine", locator: "addp://engine/13/path/doc/slides.pptx?type=file&item_id=77", want: "engine_id"},
		{name: "format", locator: "addp://engine/12/path/doc/slides.docx?type=file&item_id=77", want: ".pptx"},
		{name: "resource type", locator: "addp://engine/12/path/doc/slides.pptx?type=directory&item_id=77", want: "file or object"},
		{name: "item", locator: "addp://engine/12/path/doc/slides.pptx?type=file&item_id=77", itemID: 78, want: "item_id"},
		{name: "unscanned", locator: "addp://engine/12/path/doc/slides.pptx?type=file", want: "scanned item"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			task := &models.PPTXPDFTask{SourceEngineID: 12, ItemID: tc.itemID, Locator: tc.locator}
			err := normalizePPTXPDFTask(task)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestResolvePPTXPDFTaskSourceRejectsUnverifiedMetaIdentity(t *testing.T) {
	task := &models.PPTXPDFTask{TenantID: 7, Locator: "addp://engine/12/path/doc/slides.pptx?type=file&item_id=77"}
	meta := fakePPTXPDFMetaClient{item: &commonModels.MetaItem{ID: 77, TenantID: 7, EngineID: 12, ItemType: "file", Name: "other.pptx", FullName: "doc/other.pptx"}}
	if err := resolvePPTXPDFTaskSource(meta, task, "manager"); err == nil || !errors.Is(err, ErrInvalidPPTXPDFSource) || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("error = %v", err)
	}

	task = &models.PPTXPDFTask{TenantID: 7, Locator: "addp://engine/12/path/doc/slides.pptx?type=file&item_id=77"}
	if err := resolvePPTXPDFTaskSource(fakePPTXPDFMetaClient{err: errors.New("meta unavailable")}, task, "manager"); err == nil || !strings.Contains(err.Error(), "meta unavailable") {
		t.Fatalf("error = %v", err)
	}
}
