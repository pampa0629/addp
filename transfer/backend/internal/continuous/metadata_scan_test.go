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

func TestTargetMetadataScannerSubmitsTableLeafOnce(t *testing.T) {
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
	paths, _ := payload["targets"].([]interface{})
	if len(paths) != 1 || paths[0] != "addp://engine/20/path/public/orders_cdc?type=table" {
		t.Fatalf("targets = %#v", paths)
	}
	if _, exists := payload["catalog_paths"]; exists {
		t.Fatalf("parent catalog scope must not be submitted: %#v", payload)
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

func TestPreparedTargetMetadataScopePreservesCompleteTablePath(t *testing.T) {
	for _, term := range []string{engineplugin.EngineCatalogTermSchema, engineplugin.EngineCatalogTermDatabase} {
		t.Run(term, func(t *testing.T) {
			plan := metadataScanTestPlan(20, "sales.v2", "orders.2026")
			plan.Target.Path.Segments[1].Term = term
			engineID, targets, err := preparedTargetMetadataScope(plan)
			if err != nil || engineID != 20 || len(targets) != 1 || targets[0] != "addp://engine/20/path/sales.v2/orders.2026?type=table" {
				t.Fatalf("scope engine=%d targets=%#v err=%v", engineID, targets, err)
			}
			if len(plan.Target.Path.Segments) != 3 || plan.Target.Path.Segments[2].Name != "orders.2026" {
				t.Fatal("scan scope mutated execution target")
			}
		})
	}
}

func TestTargetMetadataScannerRejectsIncompleteScopeBeforeSubmission(t *testing.T) {
	for _, name := range []string{"nil_plan", "parent_only", "empty_name", "wrong_kind", "engine_mismatch", "no_engine"} {
		t.Run(name, func(t *testing.T) {
			plan := metadataScanTestPlan(20, "public", "orders")
			switch name {
			case "nil_plan":
				plan = nil
			case "parent_only":
				plan.Target.Path.Segments = plan.Target.Path.Segments[:2]
			case "empty_name":
				plan.Target.Path.Segments[2].Name = ""
			case "wrong_kind":
				plan.Target.Path.Segments[2].Kind = engineplugin.EngineCatalogKindNamespace
			case "engine_mismatch":
				plan.Target.Path.EngineID = 21
			case "no_engine":
				plan.Target.EngineID = 0
				plan.Target.Path.EngineID = 0
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(http.StatusCreated) }))
			defer server.Close()
			store := &fakeInitialMetadataScanStore{owned: true}
			scanner := &TargetMetadataScanner{Store: store, Client: commonClient.NewMetaClient(server.URL, commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "test-token", nil }))}
			claim := continuousRunnerClaim()
			claim.Task.AutoScanMetadata = true
			if err := scanner.ScanPreparedTarget(context.Background(), claim, plan); err == nil {
				t.Fatal("invalid scan scope accepted")
			}
			if calls != 0 || store.completedStatus != "" {
				t.Fatalf("invalid scope reached Meta: calls=%d status=%s", calls, store.completedStatus)
			}
		})
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
		engineplugin.EngineCatalogSegment{Term: engineplugin.EngineCatalogTermSchema, Kind: engineplugin.EngineCatalogKindNamespace, Name: parent},
		engineplugin.EngineCatalogSegment{Term: engineplugin.EngineCatalogTermTable, Kind: engineplugin.EngineCatalogKindTable, Name: table},
	)
	return &planner.ContinuousPlan{Target: planner.ContinuousTargetPlan{EngineID: engineID, Path: path}}
}
