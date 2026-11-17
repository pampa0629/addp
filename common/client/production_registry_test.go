package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/production"
	"github.com/google/uuid"
)

func TestProductionRegistryBindsEachOwnerToItsOriginalMachineReceiptRoute(t *testing.T) {
	executionID, resultID := uuid.NewString(), uuid.NewString()
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.RawQuery != "" || request.Header.Get("Authorization") != "Bearer addp_at_system" || request.Header.Get("X-Tenant-ID") != "" {
			t.Error("receipt factory escaped its original Tenant service transport")
		}
		requested = append(requested, request.URL.Path)
		switch request.URL.Path {
		case "/api/v1/transfer/runtime/executions/" + executionID + "/production-receipt":
			json.NewEncoder(w).Encode(TransferProductionReceipt{SchemaVersion: TransferProductionReceiptSchema, TenantID: 7, TargetEngineVersion: 2, ExecutionID: executionID, Attempt: 1, Target: plugin.TabularItemPath(11, "schema", "public", "result"), Preparation: plugin.TableTargetCreated, CreatedAndCommitted: true, CommittedAt: time.Now().UTC()})
		case "/api/v1/develop/runtime/productions/" + executionID + "/" + resultID + "/receipt":
			target := plugin.FileItemPath(11, "result.gpkg")
			json.NewEncoder(w).Encode(production.Receipt{Reference: production.Reference{Owner: "develop", ExecutionID: executionID, ResultID: resultID}, TenantID: 7, TargetEngineVersion: 2, Attempt: 1, OperationID: resultID, CommittedAt: time.Now().UTC(), Primary: target, Objects: []production.CommittedObject{{Target: target, Created: true}}, Sources: production.SourceEvidence{Kind: production.SourcesUnknown}})
		default:
			t.Errorf("unexpected owner receipt route %s", request.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	registry, err := NewProductionRegistry(NewTransferClient(server.URL, staticTenantToken("addp_at_system")), NewDevelopClient(server.URL, staticTenantToken("addp_at_system"), nil))
	if err != nil {
		t.Fatal(err)
	}
	var modules []string
	for _, owner := range registry.Owners() {
		modules = append(modules, owner.Module)
		ref := production.Reference{Owner: owner.Module, ExecutionID: executionID, ResultID: resultID}
		if owner.Module == "transfer" {
			ref.ResultID = executionID
		}
		receipt, err := registry.Read(t.Context(), 7, ref)
		if err != nil || receipt.Reference != ref || receipt.TenantID != 7 {
			t.Fatalf("%s receipt=%+v error=%v", owner.Module, receipt, err)
		}
	}
	if !slices.Equal(modules, []string{"develop", "transfer"}) || len(requested) != len(modules) {
		t.Fatal("add each new owner's fixed transport regression", modules, requested)
	}
	// The inspection copy must not change live route ownership.
	owners := registry.Owners()
	owners[0].Module = "foreign"
	if _, ok := registry.Resolve("develop", "addp-develop"); !ok {
		t.Fatal("inspection changed runtime owner")
	}
}
