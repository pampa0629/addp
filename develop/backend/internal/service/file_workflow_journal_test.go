package service

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/addp/common/engine/plugin"
	"io"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Real Owner SQL and Common HTTP consumer, with explicit authority/storage and
// Runtime doubles. This is T1, not a real IAM/NFS or business execution.
func TestFileWorkflowInputsDurableOwnerRecordBeforeRuntimeCreate(t *testing.T) {
	facts, issued, _, base := fileInputsFixture(t, false)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	defer sqlDB.Close()
	for _, schema := range []string{"common", "develop"} {
		if err := db.Exec("ATTACH DATABASE ':memory:' AS " + schema).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.FileExecution{}); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(facts.ConfigJSON))
	decoder.UseNumber()
	var config commonModels.JSONMap
	if err := decoder.Decode(&config); err != nil {
		t.Fatal(err)
	}
	actor, membership, version, trigger := int64(31), int64(41), int64(5), 31
	item := &execution.TaskExecution{ExecutionID: facts.ExecutionID, TenantID: 3, Module: execution.ModuleDevelop, Source: execution.ModuleDevelop, TaskType: execution.TaskTypeWorkflow, TriggerType: execution.TriggerTypeManual, Status: execution.ExecutionStatusPending, ExecutionBoundary: execution.ExecutionBoundaryBounded, MaxAttempts: 1, ActorPrincipalID: &actor, ActorTenantMembershipID: &membership, IssuedAuthorizationVersion: &version, TriggeredBy: &trigger, ExecutionConfig: config}
	repo := repository.NewFileExecutionRepository(db)
	if _, err := repo.CreatePending(t.Context(), item, facts.Intent.Sources, nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.BindAuthorization(t.Context(), 3, item.ExecutionID, issued); err != nil {
		t.Fatal(err)
	}
	if err := repo.ApplyAdmission(t.Context(), 3, item.ExecutionID, client.DevelopFileAdmissionObservation{AuthorizationID: issued.Authorization.ID, ExecutionID: item.ExecutionID, ExpectedAttempt: 1, ScopeDigest: issued.ScopeDigest, State: "ready", ObservedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	_, lease, err := repository.NewExecutionRepository(db).ClaimNext("workflow", t.Context(), "private-input-worker", time.Now(), time.Minute, nil)
	if err != nil || lease == nil {
		t.Fatal(lease, err)
	}
	frozen, err := repo.Read(t.Context(), 3, item.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	registerDirectory := func(ctx context.Context, instanceID, directory string) error {
		return repo.RecordPrivateInputDirectory(ctx, *lease, instanceID, directory)
	}
	provider := fileInputsProvider(fileInputsData())
	open := provider.open
	provider.open = func(ctx context.Context, connection plugin.ConnectionInfo, path plugin.EngineCatalogPath, options plugin.ReadOptions) (io.ReadCloser, error) {
		registered, err := repo.ReadPrivateInputDirectory(ctx, 3, item.ExecutionID)
		if err != nil || registered.OwnerInstanceID != "fixture-instance" || registered.InputDirectory == "" {
			t.Error("source opened before directory registration", registered, err)
		}
		return open(ctx, connection, path, options)
	}
	b, err := prepareFileWorkflowInputs(t.Context(), "fixture-instance", fileInputsAuthority(base), provider, frozen, issued, *lease, registerDirectory)
	if err != nil {
		t.Fatal(err)
	}
	h := newFileInputsHTTP(t, "")
	record := func(ctx context.Context, record repository.PrivateFileRuntimeRecord) error {
		id, manifest := record.RuntimeExecutionID, record.Manifest
		if err := repo.RecordPrivateRuntime(ctx, *lease, record); err != nil {
			return err
		}
		persisted, err := repository.NewFileExecutionRepository(db).ReadPrivateRuntime(ctx, 3, item.ExecutionID)
		if err != nil {
			return err
		}
		expected, _ := json.Marshal(manifest)
		actual, _ := json.Marshal(persisted.Manifest)
		if id != persisted.RuntimeExecutionID || !bytes.Equal(expected, actual) {
			t.Error("journal did not retain original full request")
		}
		if persisted.OwnerInstanceID != record.OwnerInstanceID || persisted.InputDirectory != record.InputDirectory {
			t.Error("directory/instance ownership changed")
		}
		return h.record(ctx, record)
	}
	if err := b.Upload(t.Context(), h.connection, fileInputRuntimeID, runtimeFileInputsCredential, record); err != nil {
		t.Fatal(err)
	}
	if h.creates.Load() != 1 || h.uploads.Load() != 2 {
		t.Fatal("Owner registration did not precede the transfer")
	}
	result, err := b.Cleanup(t.Context())
	if err != nil || !result.LocalRemoved {
		t.Fatal(result, err)
	}
}
