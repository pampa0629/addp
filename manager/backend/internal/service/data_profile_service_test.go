package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/common/dataprofile"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/manager/internal/models"
)

func TestDataProfileServiceGetCurrentMarksStoredResultStale(t *testing.T) {
	profile := &dataprofile.Profile{SchemaVersion: dataprofile.SchemaVersionV1, Mode: dataprofile.ModeSample}
	profiles := &dataProfileServiceTestProfileStore{
		state:   &models.DataProfile{ID: 12, SourceVersion: "old-version"},
		profile: profile,
	}
	completedAt := time.Date(2026, 7, 27, 2, 0, 0, 0, time.UTC)
	executions := &dataProfileServiceTestExecutionStore{byID: &commonExecution.TaskExecution{
		ExecutionID: "execution-12", Status: commonExecution.ExecutionStatusSuccess, CompletedAt: &completedAt,
	}}
	profiles.state.LastExecutionID = "execution-12"
	sampler := &dataProfileServiceTestSampler{target: &DataProfileTarget{
		Locator: "addp://engine/1/item/a", ItemFingerprint: "fingerprint", SourceVersion: "new-version",
	}}
	profileService := NewDataProfileService(profiles, executions, sampler)

	response, err := profileService.GetCurrent(context.Background(), 7, DataProfileCurrentRequest{Locator: sampler.target.Locator})
	if err != nil {
		t.Fatalf("GetCurrent() error = %v", err)
	}
	if response.Profile != profile || !response.Stale || response.StaleReason != "source_changed" {
		t.Fatalf("response = %#v", response)
	}
	if response.StoredSourceVersion != "old-version" || response.SourceVersion != "new-version" {
		t.Fatalf("source versions = stored:%q current:%q", response.StoredSourceVersion, response.SourceVersion)
	}
	if response.ProfileExecution == nil || response.ProfileExecution.ExecutionID != "execution-12" {
		t.Fatalf("profile execution = %#v", response.ProfileExecution)
	}
}

func TestDataProfileServiceRejectsUnsupportedMode(t *testing.T) {
	profileService := NewDataProfileService(
		&dataProfileServiceTestProfileStore{},
		&dataProfileServiceTestExecutionStore{},
		&dataProfileServiceTestSampler{},
	)
	_, err := profileService.CreateExecution(context.Background(), 7, 9, DataProfileExecutionRequest{
		Locator: "addp://engine/1/item/a",
		Mode:    "full",
	})
	if !errors.Is(err, ErrDataProfileInvalidRequest) {
		t.Fatalf("CreateExecution() error = %v, want ErrDataProfileInvalidRequest", err)
	}
}

func TestDataProfileServiceFailedRefreshDoesNotReplaceSuccessfulResult(t *testing.T) {
	profiles := &dataProfileServiceTestProfileStore{}
	executions := &dataProfileServiceTestExecutionStore{}
	sampler := &dataProfileServiceTestSampler{sampleErr: errors.New("source read failed")}
	profileService := NewDataProfileService(profiles, executions, sampler)
	target := &DataProfileTarget{ItemFingerprint: "fingerprint", SourceVersion: "version"}
	execution := &commonExecution.TaskExecution{TenantID: 7, ExecutionID: "execution-1"}

	profileService.runExecution(target, "config", execution)

	if profiles.replaceCalls != 0 {
		t.Fatalf("ReplaceCurrent calls = %d, want 0", profiles.replaceCalls)
	}
	if executions.failedCode != "sample_failed" {
		t.Fatalf("failed code = %q, want sample_failed", executions.failedCode)
	}
}

type dataProfileServiceTestProfileStore struct {
	state        *models.DataProfile
	profile      *dataprofile.Profile
	replaceCalls int
}

func (s *dataProfileServiceTestProfileStore) GetCurrent(context.Context, uint, string, string, string) (*models.DataProfile, *dataprofile.Profile, error) {
	return s.state, s.profile, nil
}

func (s *dataProfileServiceTestProfileStore) ReplaceCurrent(context.Context, *models.DataProfile, dataprofile.Profile) error {
	s.replaceCalls++
	return nil
}

type dataProfileServiceTestExecutionStore struct {
	failedCode string
	byID       *commonExecution.TaskExecution
}

func (s *dataProfileServiceTestExecutionStore) CreateOrReuseActive(context.Context, string, *commonExecution.TaskExecution) (*commonExecution.TaskExecution, bool, error) {
	return nil, false, nil
}
func (s *dataProfileServiceTestExecutionStore) GetActive(context.Context, int, string) (*commonExecution.TaskExecution, error) {
	return nil, nil
}
func (s *dataProfileServiceTestExecutionStore) GetLatest(context.Context, int, string) (*commonExecution.TaskExecution, error) {
	return nil, nil
}
func (s *dataProfileServiceTestExecutionStore) GetByExecutionID(context.Context, int, string) (*commonExecution.TaskExecution, error) {
	return s.byID, nil
}
func (s *dataProfileServiceTestExecutionStore) Start(context.Context, int, string, time.Time) error {
	return nil
}
func (s *dataProfileServiceTestExecutionStore) Complete(context.Context, int, string, time.Time, int64, map[string]interface{}) error {
	return nil
}
func (s *dataProfileServiceTestExecutionStore) Fail(_ context.Context, _ int, _ string, _ time.Time, code string, _ string) error {
	s.failedCode = code
	return nil
}
func (s *dataProfileServiceTestExecutionStore) Timeout(_ context.Context, _ int, _ string, _ time.Time, code string, _ string) error {
	s.failedCode = code
	return nil
}

type dataProfileServiceTestSampler struct {
	target    *DataProfileTarget
	sampleErr error
}

func (s *dataProfileServiceTestSampler) ResolveTarget(context.Context, uint, string, DataProfileSelection) (*DataProfileTarget, error) {
	return s.target, nil
}
func (s *dataProfileServiceTestSampler) Sample(context.Context, *DataProfileTarget, DataProfileBudget) (*DataProfileSample, error) {
	return nil, s.sampleErr
}
