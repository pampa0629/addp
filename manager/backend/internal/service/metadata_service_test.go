package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/models"
)

func setupExplorerService(t *testing.T) (*ExplorerService, func()) {
	t.Helper()

	capabilities, err := plugin.MarshalEngineCapabilities(plugin.NewTabularCapabilities("postgresql", plugin.CatalogTermSchema, plugin.TabularCapabilityOptions{}))
	if err != nil {
		t.Fatalf("failed to marshal capabilities: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/system/engines":
			tenantID := r.URL.Query().Get("tenant_id")
			switch tenantID {
			case "":
				fmt.Fprintf(w, `[
					{"id":1,"name":"tenant-one-db","engine_type":"postgresql","connection_info":{},"tenant_id":1,"is_active":true,"capabilities":%q},
					{"id":2,"name":"tenant-two-db","engine_type":"postgresql","connection_info":{},"tenant_id":2,"is_active":true,"capabilities":%q},
					{"id":3,"name":"global-db","engine_type":"postgresql","connection_info":{},"is_active":true,"capabilities":%q}
				]`, capabilities, capabilities, capabilities)
			case "1":
				fmt.Fprintf(w, `[{"id":1,"name":"tenant-one-db","engine_type":"postgresql","connection_info":{},"tenant_id":1,"is_active":true,"capabilities":%q}]`, capabilities)
			case "2":
				fmt.Fprintf(w, `[{"id":2,"name":"tenant-two-db","engine_type":"postgresql","connection_info":{},"tenant_id":2,"is_active":true,"capabilities":%q}]`, capabilities)
			default:
				fmt.Fprint(w, `[]`)
			}
		case "/api/v1/system/engines/1":
			fmt.Fprintf(w, `{"id":1,"name":"tenant-one-db","engine_type":"postgresql","connection_info":{},"tenant_id":1,"is_active":true,"capabilities":%q}`, capabilities)
		case "/api/v1/system/engines/2":
			fmt.Fprintf(w, `{"id":2,"name":"tenant-two-db","engine_type":"postgresql","connection_info":{},"tenant_id":2,"is_active":true,"capabilities":%q}`, capabilities)
		default:
			http.NotFound(w, r)
		}
	}))

	return NewExplorerService(client.NewSystemClient(server.URL, "test-token"), nil, nil), server.Close
}

func TestExplorerEngineListTenantFiltering(t *testing.T) {
	service, cleanup := setupExplorerService(t)
	defer cleanup()

	// 无租户上下文（超级管理员）应看到所有激活资源（含租户为空）
	resourcesAll, err := service.GetEngineList(nil)
	if err != nil {
		t.Fatalf("GetEngineList(nil) returned error: %v", err)
	}
	if got, want := len(resourcesAll), 3; got != want {
		t.Fatalf("GetEngineList(nil) length = %d, want %d", got, want)
	}

	tenantOne := uint(1)
	resourcesTenantOne, err := service.GetEngineList(&tenantOne)
	if err != nil {
		t.Fatalf("GetEngineList(tenant=1) returned error: %v", err)
	}
	if got, want := len(resourcesTenantOne), 1; got != want {
		t.Fatalf("GetEngineList(tenant=1) length = %d, want %d", got, want)
	}
	if resourcesTenantOne[0].Name != "tenant-one-db" {
		t.Fatalf("GetEngineList(tenant=1)[0] = %s, want tenant-one-db", resourcesTenantOne[0].Name)
	}

	tenantTwo := uint(2)
	resourcesTenantTwo, err := service.GetEngineList(&tenantTwo)
	if err != nil {
		t.Fatalf("GetEngineList(tenant=2) returned error: %v", err)
	}
	if got, want := len(resourcesTenantTwo), 1; got != want {
		t.Fatalf("GetEngineList(tenant=2) length = %d, want %d", got, want)
	}
	if resourcesTenantTwo[0].Name != "tenant-two-db" {
		t.Fatalf("GetEngineList(tenant=2)[0] = %s, want tenant-two-db", resourcesTenantTwo[0].Name)
	}

	tenantThree := uint(3)
	resourcesTenantThree, err := service.GetEngineList(&tenantThree)
	if err != nil {
		t.Fatalf("GetEngineList(tenant=3) returned error: %v", err)
	}
	if got, want := len(resourcesTenantThree), 0; got != want {
		t.Fatalf("GetEngineList(tenant=3) length = %d, want %d", got, want)
	}
}

func TestGetTreeDeniedForOtherTenant(t *testing.T) {
	service, cleanup := setupExplorerService(t)
	defer cleanup()

	tenantOne := uint(1)
	_, err := service.GetTree(t.Context(), &tenantOne, 2, 1)
	if !errors.Is(err, ErrEngineAccessDenied) {
		t.Fatalf("GetTree should deny cross-tenant access, got err=%v", err)
	}
}

func TestDecodeMetaScanTaskRunDirect(t *testing.T) {
	t.Parallel()

	body, err := json.Marshal(models.MetaScanTaskRun{ID: 12, EngineID: 34, Status: "running"})
	if err != nil {
		t.Fatalf("marshal direct run: %v", err)
	}

	run, err := decodeMetaScanTaskRun(body)
	if err != nil {
		t.Fatalf("decodeMetaScanTaskRun() error = %v", err)
	}
	if run.ID != 12 || run.EngineID != 34 || run.Status != "running" {
		t.Fatalf("run = %#v", run)
	}
}

func TestDecodeMetaScanTaskRunWrapped(t *testing.T) {
	t.Parallel()

	body, err := json.Marshal(map[string]models.MetaScanTaskRun{
		"data": {ID: 56, EngineID: 78, Status: "success"},
	})
	if err != nil {
		t.Fatalf("marshal wrapped run: %v", err)
	}

	run, err := decodeMetaScanTaskRun(body)
	if err != nil {
		t.Fatalf("decodeMetaScanTaskRun() error = %v", err)
	}
	if run.ID != 56 || run.EngineID != 78 || run.Status != "success" {
		t.Fatalf("run = %#v", run)
	}
}
