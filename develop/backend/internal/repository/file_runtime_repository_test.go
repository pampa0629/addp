package repository

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	commonclient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonexecution "github.com/addp/common/execution"
	"github.com/addp/develop/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Called by the same queue fixture in SQLite T1 and the standard PostgreSQL T2.
func runPrivateRuntimeRecordCase(t *testing.T, db *gorm.DB, repo *FileExecutionRepository, item *commonexecution.TaskExecution, lease commonexecution.Lease, facts *commonclient.DevelopFileExecutionFacts) {
	t.Helper()
	ready, issued, readErr := repo.ReadForDispatch(t.Context(), 7, item.ExecutionID)
	if readErr != nil || ready.ScopeDigest != facts.ScopeDigest || issued.ScopeDigest != facts.ScopeDigest {
		t.Fatal("original dispatch binding unavailable", ready, issued, readErr)
	}
	id := uuid.NewString()
	directory := t.TempDir()
	registration := func(runtimeID string, manifest plugin.WorkflowPrivateFileManifest) PrivateFileRuntimeRecord {
		reads := make([]PrivateFileSourceRead, 0, len(facts.Intent.Sources))
		for _, source := range facts.Intent.Sources {
			set, err := plugin.NewQueryReadSet(source.Target)
			if err != nil {
				t.Fatal(err)
			}
			reads = append(reads, PrivateFileSourceRead{EngineVersion: 2, NodeID: source.NodeID, ReadSet: set})
		}
		return PrivateFileRuntimeRecord{RuntimeExecutionID: runtimeID, Manifest: manifest, OwnerInstanceID: "fixture-instance", InputDirectory: directory, SourceReads: reads}
	}
	authorizationID, _ := strconv.ParseInt(facts.AuthorizationID, 10, 64)
	manifest := plugin.WorkflowPrivateFileManifest{SchemaVersion: plugin.WorkflowPrivateFilesAPIV2, TenantID: 7, ExecutionID: item.ExecutionID, Attempt: 1, AuthorizationID: authorizationID, ScopeDigest: facts.ScopeDigest, Deadline: item.StartedAt.Add(time.Minute), WorkflowJSON: `{"tasks":[],"wide":9007199254740993}`, Inputs: []plugin.WorkflowPrivateFile{{NodeID: "source", Format: "geopackage", Size: 123, SHA256: strings.Repeat("a", 64)}}, Outputs: []string{}}
	manifest.Inputs = nil
	for _, source := range facts.Intent.Sources {
		manifest.Inputs = append(manifest.Inputs, plugin.WorkflowPrivateFile{NodeID: source.NodeID, Format: "geopackage", Size: 123, SHA256: strings.Repeat("a", 64)})
	}
	if err := repo.RecordPrivateInputDirectory(t.Context(), lease, "fixture-instance", directory); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.ReadForDispatch(t.Context(), 7, item.ExecutionID); err == nil {
		t.Fatal("registered input was dispatchable again")
	}
	inputRecord, err := repo.ReadPrivateInputDirectory(t.Context(), 7, item.ExecutionID)
	if err != nil || inputRecord.InputDirectory != directory || inputRecord.OwnerInstanceID != "fixture-instance" || inputRecord.ScopeDigest != facts.ScopeDigest {
		t.Fatal(inputRecord, err)
	}
	if err := repo.RecordPrivateInputDirectory(t.Context(), lease, "other", t.TempDir()); err == nil {
		t.Fatal("input directory replaced")
	}
	for _, fault := range []string{"tenant", "execution", "attempt", "scope", "authorization", "lease"} {
		bad := manifest
		badLease := lease
		switch fault {
		case "tenant":
			bad.TenantID = 8
		case "execution":
			bad.ExecutionID = uuid.NewString()
		case "attempt":
			bad.Attempt = 2
		case "scope":
			bad.ScopeDigest = strings.Repeat("f", 64)
		case "authorization":
			bad.AuthorizationID = 102
		case "lease":
			badLease.Token = uuid.NewString()
		}
		if err := repo.RecordPrivateRuntime(t.Context(), badLease, registration(id, bad)); err == nil {
			t.Fatal("wrong binding recorded", fault)
		}
		if record, err := repo.ReadPrivateRuntime(t.Context(), 7, item.ExecutionID); record != nil || err == nil {
			t.Fatal("rejected registration left private record", fault, record, err)
		}
	}
	for _, fault := range []string{"missing", "wrong_node", "wrong_target", "missing_version", "changed_version"} {
		bad := registration(id, manifest)
		switch fault {
		case "missing":
			bad.SourceReads = nil
		case "wrong_node":
			bad.SourceReads[0].NodeID = "other"
		case "missing_version":
			bad.SourceReads[0].EngineVersion = 0
		case "changed_version":
			bad.SourceReads[0].EngineVersion = 3
		case "wrong_target":
			bad.SourceReads[0].ReadSet, _ = plugin.NewQueryReadSet(plugin.FileItemPath(999, "other/source.gpkg"))
		}
		if err := repo.RecordPrivateRuntime(t.Context(), lease, bad); err == nil {
			t.Fatal("unbound source proof accepted", fault)
		}
	}
	// A domain write followed by a failed lease fence must not survive rollback.
	injected := false
	const hook = "private_runtime_fence_loss"
	if err := db.Callback().Update().After("gorm:update").Register(hook, func(tx *gorm.DB) {
		if injected || (tx.Statement.Schema == nil || tx.Statement.Schema.Table != (&models.FileExecution{}).TableName()) {
			return
		}
		injected = true
		changed := tx.Session(&gorm.Session{NewDB: true}).Exec("UPDATE common.task_executions SET lease_expires_at = ? WHERE execution_id = ?", time.Now().Add(-time.Minute), item.ExecutionID)
		if changed.Error != nil || changed.RowsAffected != 1 {
			t.Errorf("lease loss injection failed: %d %v", changed.RowsAffected, changed.Error)
		}
	}); err != nil {
		t.Fatal(err)
	}
	err = repo.RecordPrivateRuntime(t.Context(), lease, registration(id, manifest))
	_ = db.Callback().Update().Remove(hook)
	if err == nil || !injected {
		t.Fatal("late record passed final lease fence", err, injected)
	}
	if record, err := repo.ReadPrivateRuntime(t.Context(), 7, item.ExecutionID); record != nil || err == nil {
		t.Fatal("late private record survived rollback")
	}
	if err = repo.RecordPrivateRuntime(t.Context(), lease, registration(id, manifest)); err != nil {
		t.Fatal(err)
	}
	record, err := NewFileExecutionRepository(db).ReadPrivateRuntime(t.Context(), 7, item.ExecutionID)
	if err != nil || record == nil {
		t.Fatal(record, err)
	}
	if len(record.SourceReads) != len(facts.Intent.Sources) || record.SourceReads[0].NodeID != facts.Intent.Sources[0].NodeID {
		t.Fatal("fresh Owner lost actual source proofs")
	}
	want, _ := json.Marshal(manifest)
	actual, _ := json.Marshal(record.Manifest)
	if err != nil || record.RuntimeExecutionID != id || record.OwnerInstanceID != "fixture-instance" || record.InputDirectory != directory || !bytes.Equal(want, actual) {
		t.Fatal("fresh Owner did not read original manifest", record, err)
	}
	if err := repo.RecordPrivateRuntime(t.Context(), lease, registration(uuid.NewString(), manifest)); err == nil {
		t.Fatal("registered UUID was replaced")
	}
	if record, err := repo.ReadPrivateRuntime(t.Context(), 8, item.ExecutionID); record != nil || err == nil {
		t.Fatal("private runtime crossed tenant")
	}
	public, err := repo.Read(t.Context(), 7, item.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	projection, _ := json.Marshal(public)
	if bytes.Contains(projection, []byte(id)) || bytes.Contains(projection, []byte("runtime_manifest")) || bytes.Contains(projection, []byte(directory)) {
		t.Fatal("private journal leaked into System projection")
	}
}
