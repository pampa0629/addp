package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
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

func TestPPTXPDFExecutionMetadataIncludesManagedArtifactLineage(t *testing.T) {
	task := &models.PPTXPDFTask{
		TenantID:        7,
		ItemID:          77,
		ItemFingerprint: "pptx-fingerprint",
		Locator:         "addp://engine/12/path/addp/doc/slides.pptx?type=object&item_id=77",
	}
	built := &PPTXPDFExecutionResult{
		StorageRef: rastercogref.ObjectStorageRef("manager", "tenant_7/document-preview/pptx-fingerprint/slides.pdf"),
		Metadata:   commonModels.JSONMap{"kept": true},
	}

	metadata, err := pptxPDFExecutionMetadata(task, built, "manager")
	if err != nil {
		t.Fatalf("pptxPDFExecutionMetadata() error = %v", err)
	}
	if metadata["kept"] != true {
		t.Fatalf("existing metadata was not preserved: %#v", metadata)
	}
	payload, err := json.Marshal(metadata["lineage_facts"])
	if err != nil {
		t.Fatalf("marshal lineage facts: %v", err)
	}
	var facts commonExecution.LineageFacts
	if err := json.Unmarshal(payload, &facts); err != nil {
		t.Fatalf("unmarshal lineage facts: %v", err)
	}
	if facts.SchemaVersion != commonExecution.LineageFactsSchemaVersion {
		t.Fatalf("schema_version = %q", facts.SchemaVersion)
	}
	if len(facts.Inputs) != 1 || facts.Inputs[0].Locator != task.Locator || facts.Inputs[0].ItemID == nil || *facts.Inputs[0].ItemID != task.ItemID {
		t.Fatalf("inputs = %#v", facts.Inputs)
	}
	if len(facts.Outputs) != 1 || facts.Outputs[0].Locator != "addp-infra://minio/manager/tenant_7/document-preview/pptx-fingerprint/slides.pdf?type=object" {
		t.Fatalf("outputs = %#v", facts.Outputs)
	}
	if len(facts.Operations) != 1 || facts.Operations[0].Operator != commonExecution.TaskTypePPTXPDFGeneration || facts.Operations[0].Kind != "derive" {
		t.Fatalf("operations = %#v", facts.Operations)
	}
}

type recordingPPTXPDFCleaner struct {
	storageRefs []string
}

func (c *recordingPPTXPDFCleaner) DeleteByStorageRef(_ context.Context, storageRef string) error {
	c.storageRefs = append(c.storageRefs, storageRef)
	return nil
}

func TestPPTXPDFTaskServiceDeletesManagedResultBeforeTask(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	for _, statement := range []string{
		`CREATE TABLE manager.pptx_pdf_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL,
			description TEXT, enabled BOOLEAN, schedule TEXT, next_run_at DATETIME,
			item_fingerprint TEXT NOT NULL, artifact_variant TEXT NOT NULL, source_engine_id INTEGER NOT NULL,
			item_id INTEGER NOT NULL, locator TEXT NOT NULL, source_version TEXT NOT NULL,
			source_size_bytes INTEGER NOT NULL, last_run_at DATETIME, last_execution_id TEXT,
			last_execution_status TEXT, config JSON, created_by INTEGER, created_at DATETIME,
			updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE manager.pptx_pdf (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, item_fingerprint TEXT NOT NULL,
			artifact_variant TEXT NOT NULL, source_version TEXT NOT NULL, source_engine_id INTEGER NOT NULL,
			item_id INTEGER NOT NULL, locator TEXT NOT NULL, task_id INTEGER, last_execution_id TEXT,
			storage_ref TEXT NOT NULL, file_name TEXT NOT NULL, size_bytes INTEGER NOT NULL,
			page_count INTEGER NOT NULL, content_url TEXT, status TEXT NOT NULL, metadata JSON,
			error_message TEXT, created_by INTEGER, created_at DATETIME, updated_at DATETIME,
			deleted_at DATETIME)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create PPTX PDF table: %v", err)
		}
	}
	repo := repository.NewPPTXPDFRepository(db)
	cleaner := &recordingPPTXPDFCleaner{}
	svc := NewPPTXPDFTaskService(repo)
	svc.SetCleaner(cleaner)
	task := &models.PPTXPDFTask{
		TenantID: 7, Name: "slides.pptx", Enabled: true, ItemFingerprint: "pptx-fingerprint",
		ArtifactVariant: models.PPTXPDFArtifactVariant, SourceEngineID: 12, ItemID: 77,
		Locator:       "addp://engine/12/path/doc/slides.pptx?type=object&item_id=77",
		SourceVersion: "version-1", Config: commonModels.JSONMap{},
	}
	if err := repo.CreateTask(context.Background(), task); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	storageRef := rastercogref.ObjectStorageRef("manager", "tenant_7/document-preview/pptx-fingerprint/slides.pdf")
	result := &models.PPTXPDF{
		TenantID: 7, ItemFingerprint: task.ItemFingerprint, ArtifactVariant: models.PPTXPDFArtifactVariant,
		SourceVersion: task.SourceVersion, SourceEngineID: task.SourceEngineID, ItemID: task.ItemID,
		Locator: task.Locator, TaskID: &task.ID, StorageRef: storageRef, FileName: "slides.pdf",
		SizeBytes: 1024, PageCount: 3, Status: models.PPTXPDFStatusReady, Metadata: commonModels.JSONMap{},
	}
	if err := repo.CreateResult(context.Background(), result); err != nil {
		t.Fatalf("CreateResult() error = %v", err)
	}

	if err := svc.DeleteResult(context.Background(), result.ID, task.TenantID); err != nil {
		t.Fatalf("DeleteResult() error = %v", err)
	}
	if len(cleaner.storageRefs) != 1 || cleaner.storageRefs[0] != storageRef {
		t.Fatalf("cleaner storage refs = %#v", cleaner.storageRefs)
	}
	if current, err := repo.GetResult(context.Background(), result.ID, task.TenantID); err != nil || current != nil {
		t.Fatalf("deleted result = %#v, %v", current, err)
	}
	if err := svc.DeleteTask(context.Background(), task.ID, task.TenantID); err != nil {
		t.Fatalf("DeleteTask() error = %v", err)
	}
	if current, err := repo.GetTask(context.Background(), task.ID, task.TenantID); err != nil || current != nil {
		t.Fatalf("deleted task = %#v, %v", current, err)
	}
	if err := svc.DeleteResult(context.Background(), result.ID, task.TenantID); !errors.Is(err, ErrPPTXPDFResultNotFound) {
		t.Fatalf("second DeleteResult() error = %v", err)
	}
	if err := svc.DeleteTask(context.Background(), task.ID, task.TenantID); !errors.Is(err, ErrPPTXPDFTaskNotFound) {
		t.Fatalf("second DeleteTask() error = %v", err)
	}
}
