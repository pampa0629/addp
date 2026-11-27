package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/addp/common/client"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/protection"
)

func TestTaskCreationUsesOnlyTenantEngineDescriptors(t *testing.T) {
	const prefix = "/api/v1/system/runtime/engine-descriptors/"
	for _, tc := range []struct {
		name       string
		cdc        bool
		sourceType string
		targetType string
		offline    bool
		denied     bool
		wantError  string
		wantReads  []string
	}{
		{name: "query", sourceType: "postgresql", wantReads: []string{"1"}},
		{name: "query offline", sourceType: "postgresql", offline: true, wantError: "unavailable", wantReads: []string{"1"}},
		{name: "query descriptor denied", denied: true, wantError: "resolve query source engine", wantReads: []string{"1"}},
		{name: "CDC", cdc: true, sourceType: "mysql", targetType: "postgresql", wantReads: []string{"12", "20"}},
		{name: "CDC source unsupported", cdc: true, sourceType: "mongodb", targetType: "postgresql", wantError: "does not support source engine", wantReads: []string{"12", "20"}},
		{name: "CDC target unsupported", cdc: true, sourceType: "mysql", targetType: "mongodb", wantError: "does not declare atomic monotonic", wantReads: []string{"12", "20"}},
		{name: "CDC target descriptor denied", cdc: true, sourceType: "mysql", denied: true, wantError: "resolve database CDC target engine", wantReads: []string{"12", "20"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var paths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer tenant-7" {
					t.Error("expected tenant 7 service-token GET")
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
				d := commonmodels.EngineRuntimeDescriptor{EngineType: tc.sourceType, LifecycleState: "active", ConnectionStatus: "online", Version: 3}
				switch r.URL.Path {
				case prefix + "1":
					d.ID = 1
				case prefix + "12":
					d.ID = 12
				case prefix + "20":
					d.ID, d.EngineType = 20, tc.targetType
				default:
					t.Errorf("unexpected System route %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				if tc.denied && (!tc.cdc || d.ID == 20) {
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
				if tc.offline {
					d.ConnectionStatus = "offline"
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(d)
			}))
			defer server.Close()
			db := newTransferTaskServiceTestDB(t)
			svc := NewTaskService(db, nil, nil)
			tokens := recommendationTokenSource{}
			svc.SetEngineResolver(planner.NewSystemEngineResolver(client.NewSystemClient(server.URL, tokens)))
			svc.SetEngineDescriptorLookup(protection.EngineDescriptorLookup(client.NewSystemServiceClient(server.URL, tokens, server.Client())))
			config := validPostgreSQLCDCTaskConfig()
			if !tc.cdc {
				config = validOverridableTargetTransferTaskConfig()
				config["source"].(map[string]interface{})["query"] = map[string]interface{}{"language": "sql", "statement": "SELECT id FROM public.source_table"}
			}
			_, err := svc.CreateTask(context.Background(), &models.CreateTaskRequest{Name: tc.name, TaskType: "sync", Config: config}, 7, 9)
			var wantCount int64 = 1
			if tc.wantError != "" {
				wantCount = 0
				if !errors.Is(err, ErrInvalidTaskConfig) || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("expected %q rejection, got %v", tc.wantError, err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			var count int64
			if err := db.Model(&models.TransferTask{}).Count(&count).Error; err != nil || count != wantCount {
				t.Fatalf("persisted tasks = %d, expected %d, err = %v", count, wantCount, err)
			}
			var wantPaths []string
			for _, id := range tc.wantReads {
				wantPaths = append(wantPaths, prefix+id)
			}
			if !slices.Equal(paths, wantPaths) {
				t.Fatalf("System requests = %v, expected %v", paths, wantPaths)
			}
		})
	}
}
