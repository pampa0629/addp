package repository

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/develop/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestPrivateFilePublicationOriginalOperation(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		confirmed bool
	}{{"confirmed", true}, {"unknown", false}} {
		t.Run(scenario.name, func(t *testing.T) {
			db, repo, _, _ := filePendingFixture(t)
			runPrivatePublicationCase(t, db, repo, 102, scenario.confirmed)
		})
	}
}

func runPrivatePublicationCase(t *testing.T, db *gorm.DB, repo *FileExecutionRepository, authorizationID int64, confirmed bool) {
	t.Helper()
	ctx := t.Context()
	item, sources := newFilePendingValues()
	target := &execution.DevelopFileTargetIntent{NodeID: "save", Target: plugin.FileItemPath(11, "outputs/result.gpkg"), Mode: "fail"}
	facts, err := repo.CreatePending(ctx, item, sources, target)
	if err != nil {
		t.Fatal(err)
	}
	issued := fileIssuedFixture(t, facts)
	issued.Authorization.ID = strconv.FormatInt(authorizationID, 10)
	if err := repo.BindAuthorization(ctx, 7, item.ExecutionID, issued); err != nil {
		t.Fatal(err)
	}
	facts, err = repo.Read(ctx, 7, item.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	approval := fileAdmissionFixture(facts)
	approval.AuthorizationID = issued.Authorization.ID
	approval.ApprovalID = uuid.NewString()
	if err := repo.ApplyAdmission(ctx, 7, item.ExecutionID, approval); err != nil {
		t.Fatal(err)
	}
	running, lease, err := NewExecutionRepository(db).ClaimNext("workflow", ctx, "publication-fixture", time.Now(), time.Minute, nil)
	if err != nil || lease == nil || running.ExecutionID != item.ExecutionID {
		t.Fatal(running, lease, err)
	}
	id, op, directory := uuid.NewString(), uuid.NewString(), t.TempDir()
	output := plugin.WorkflowPrivateFile{NodeID: target.NodeID, Size: 8192, SHA256: strings.Repeat("a", 64)}
	if err := repo.RegisterPrivatePublication(ctx, *lease, id, op, output); err == nil {
		t.Fatal("operation registered before Runtime")
	}
	if err := repo.RecordPrivateInputDirectory(ctx, *lease, "publication-fixture", directory); err != nil {
		t.Fatal(err)
	}
	manifest := plugin.WorkflowPrivateFileManifest{SchemaVersion: plugin.WorkflowPrivateFilesAPIV1,
		TenantID: 7, ExecutionID: item.ExecutionID, Attempt: lease.Attempt, AuthorizationID: authorizationID, ScopeDigest: issued.ScopeDigest,
		Deadline: running.StartedAt.Add(time.Minute), WorkflowJSON: `{"tasks":[]}`, Outputs: []string{target.NodeID}}
	if err := repo.RecordPrivateRuntime(ctx, *lease, PrivateFileRuntimeRecord{RuntimeExecutionID: id,
		Manifest: manifest, OwnerInstanceID: "publication-fixture", InputDirectory: directory}); err != nil {
		t.Fatal(err)
	}
	otherLease := *lease
	otherLease.Token = uuid.NewString()
	if err := repo.RegisterPrivatePublication(ctx, otherLease, id, op, output); err == nil {
		t.Fatal("wrong lease registered operation")
	}
	if err := repo.RegisterPrivatePublication(ctx, *lease, uuid.NewString(), op, output); err == nil {
		t.Fatal("wrong Runtime registered operation")
	}
	otherOutput := output
	otherOutput.NodeID = "different"
	if err := repo.RegisterPrivatePublication(ctx, *lease, id, op, otherOutput); err == nil {
		t.Fatal("wrong Runtime output registered operation")
	}
	if err := repo.RegisterPrivatePublication(ctx, *lease, id, op, output); err != nil {
		t.Fatal(err)
	}
	if err := repo.RegisterPrivatePublication(ctx, *lease, id, uuid.NewString(), output); err == nil {
		t.Fatal("operation replaced")
	}
	facts, err = repo.Read(ctx, 7, item.ExecutionID)
	expected := plugin.NewFilePublicationRequest{OperationID: op, Target: target.Target, SizeBytes: output.Size, SHA256: output.SHA256}
	if err != nil || facts.Publication == nil || facts.Publication.ApprovalID != approval.ApprovalID ||
		!reflect.DeepEqual(facts.Publication.Request, expected) || facts.Publication.PreparationDigest != "" {
		t.Fatal(facts, err)
	}
	rawProviderState := []byte("sensitive Provider handle and staging path")
	result := plugin.PreparedNewFile{NewFilePublicationRequest: expected, ProviderState: rawProviderState}
	if err := repo.RecordPrivatePublicationPreparation(ctx, otherLease, result, true); err == nil {
		t.Fatal("wrong lease recorded preparation")
	}
	wrong := result
	wrong.Target = plugin.FileItemPath(11, "outputs/other.gpkg")
	if err := repo.RecordPrivatePublicationPreparation(ctx, *lease, wrong, true); err == nil {
		t.Fatal("retargeted receipt accepted")
	}
	if err := repo.RecordPrivatePublicationPreparation(ctx, *lease, result, confirmed); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordPrivatePublicationPreparation(ctx, *lease, result, true); err == nil {
		t.Fatal("preparation overwritten or promoted")
	}
	var row models.FileExecution
	if err := db.Where("execution_id = ?", item.ExecutionID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	record, err := privateFilePublication(&row)
	if err != nil || !record.PreparationObserved || !bytes.Equal(record.ProviderState, rawProviderState) {
		t.Fatal("original private receipt not retained", err)
	}
	facts, err = repo.Read(ctx, 7, item.ExecutionID)
	digest := ""
	if confirmed {
		digest = providerPreparationDigest(rawProviderState)
	}
	if err != nil || facts.Publication == nil || facts.Publication.PreparationDigest != digest {
		t.Fatal(facts, err)
	}
	encoded, err := json.Marshal(facts)
	if err != nil || bytes.Contains(encoded, []byte("provider_state")) || bytes.Contains(encoded, rawProviderState) {
		t.Fatal("private Provider receipt entered projection", err)
	}
}
