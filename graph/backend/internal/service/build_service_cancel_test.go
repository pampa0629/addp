package service

import (
	"errors"
	"testing"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCancelTaskWithoutOwnedRuntimeDoesNotChangePersistentState(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.BuildTask{}); err != nil {
		t.Fatalf("migrate build task: %v", err)
	}
	task := models.BuildTask{TenantID: 7, GraphID: 42, Name: "running", Status: models.BuildStatusRunning, ExecutionID: "execution-1"}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create build task: %v", err)
	}
	svc := &BuildService{
		buildRepo: repository.NewBuildRepository(db),
		cancels:   make(map[string]*activeBuildRun),
	}

	err = svc.CancelTask(task.ID, task.GraphID, task.TenantID)
	if !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("CancelTask error = %v, want conflict", err)
	}
	if !errors.Is(err, ErrBuildRuntimeNotOwned) {
		t.Fatalf("CancelTask error = %v, want ErrBuildRuntimeNotOwned", err)
	}
	var stored models.BuildTask
	if err := db.First(&stored, task.ID).Error; err != nil {
		t.Fatalf("reload build task: %v", err)
	}
	if stored.Status != models.BuildStatusRunning || stored.CompletedAt != nil {
		t.Fatalf("task changed without runtime ownership: status=%s completed_at=%v", stored.Status, stored.CompletedAt)
	}
}

func TestResetCancelledMaterialRestoresPendingState(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.BuildMaterial{}); err != nil {
		t.Fatalf("migrate build material: %v", err)
	}
	material := models.BuildMaterial{
		TaskID: 5, TenantID: 1, GraphID: 1, Type: "document", Status: models.BuildMaterialStatusProcessing,
		ProcessedChunks: 1, TotalChunks: 19, ErrorMessage: "context canceled",
	}
	if err := db.Create(&material).Error; err != nil {
		t.Fatalf("create build material: %v", err)
	}
	svc := &BuildService{buildRepo: repository.NewBuildRepository(db)}

	if err := svc.resetCancelledMaterial(&material); err != nil {
		t.Fatalf("resetCancelledMaterial: %v", err)
	}
	var stored models.BuildMaterial
	if err := db.First(&stored, material.ID).Error; err != nil {
		t.Fatalf("reload build material: %v", err)
	}
	if stored.Status != models.BuildMaterialStatusPending || stored.ProcessedChunks != 0 || stored.TotalChunks != 0 ||
		stored.ErrorMessage != "" || stored.ProcessedAt != nil {
		t.Fatalf("cancelled material was not reset: %#v", stored)
	}
}
