package continuous

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/repository"
)

func TestTargetMetadataScannerSubmitsParentCatalogOnce(t *testing.T) {
	var payload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/meta/scan/run/manual" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":11,"tenant_id":7,"execution_id":"meta-run-1","module":"meta","task_type":"scan","status":"pending","trigger_type":"manual"}`))
	}))
	defer server.Close()

	store := &fakeInitialMetadataScanStore{owned: true}
	scanner := &TargetMetadataScanner{
		Store: store,
		Client: commonClient.NewMetaClient(server.URL, commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
			return "test-token", nil
		})),
		ClaimTTL: 2 * time.Minute,
	}
	claim := continuousRunnerClaim()
	claim.Task.TenantID = 7
	claim.Task.AutoScanMetadata = true
	if err := scanner.ScanPreparedTarget(context.Background(), claim, metadataScanTestPlan(20, "public", "orders_cdc")); err != nil {
		t.Fatal(err)
	}
	if payload["engine_id"] != float64(20) || payload["scan_depth"] != "deep" || payload["source"] != "transfer" {
		t.Fatalf("payload = %#v", payload)
	}
	paths, _ := payload["catalog_paths"].([]interface{})
	if len(paths) != 1 || paths[0] != "public" {
		t.Fatalf("catalog paths = %#v", paths)
	}
	if store.completedStatus != models.InitialMetadataScanSuccess || store.metaExecutionID != "meta-run-1" {
		t.Fatalf("completion status=%q execution=%q", store.completedStatus, store.metaExecutionID)
	}
}

func TestTargetMetadataScannerRecordsMetaSubmissionFailureWithoutBlockingDataPlane(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	store := &fakeInitialMetadataScanStore{owned: true}
	scanner := &TargetMetadataScanner{
		Store: store,
		Client: commonClient.NewMetaClient(server.URL, commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
			return "test-token", nil
		})),
		ClaimTTL: 2 * time.Minute,
	}
	claim := continuousRunnerClaim()
	claim.Task.TenantID = 7
	claim.Task.AutoScanMetadata = true
	if err := scanner.ScanPreparedTarget(context.Background(), claim, metadataScanTestPlan(3, "business", "farmland_cdc")); err != nil {
		t.Fatalf("Meta submission failure must not block data plane: %v", err)
	}
	if store.completedStatus != models.InitialMetadataScanFailed || store.errorMessage == "" {
		t.Fatalf("completion status=%q error=%q", store.completedStatus, store.errorMessage)
	}
}

type fakeInitialMetadataScanStore struct {
	owned           bool
	completedStatus models.InitialMetadataScanStatus
	metaExecutionID string
	errorMessage    string
}

func (s *fakeInitialMetadataScanStore) ClaimInitialMetadataScan(
	_ context.Context, claim repository.RuntimeLeaseClaim, _ time.Time, _ time.Duration,
) (*models.TransferTask, bool, error) {
	task := claim.Task
	task.InitialMetadataScanStatus = models.InitialMetadataScanRunning
	task.InitialMetadataScanClaimToken = "claim-token"
	task.InitialMetadataScanAttempt = 1
	return &task, s.owned, nil
}

func (s *fakeInitialMetadataScanStore) CompleteInitialMetadataScan(
	_ context.Context,
	_ repository.RuntimeLeaseClaim,
	_ string,
	status models.InitialMetadataScanStatus,
	metaExecutionID, errorMessage string,
	_ time.Time,
) (*models.TransferTask, bool, error) {
	s.completedStatus = status
	s.metaExecutionID = metaExecutionID
	s.errorMessage = errorMessage
	return &models.TransferTask{InitialMetadataScanStatus: status}, true, nil
}

func metadataScanTestPlan(engineID uint, parent, table string) *planner.ContinuousPlan {
	path := engineplugin.EngineCatalogRootPath((&postgresql.PostgreSQLPlugin{}).EngineCatalogModel(), engineID)
	path.Segments = append(path.Segments,
		engineplugin.EngineCatalogSegment{Name: parent},
		engineplugin.EngineCatalogSegment{Name: table},
	)
	return &planner.ContinuousPlan{Target: planner.ContinuousTargetPlan{EngineID: engineID, Path: path}}
}
