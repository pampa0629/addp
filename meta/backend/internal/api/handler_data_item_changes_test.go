package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	metaauthorization "github.com/addp/meta/internal/authorization"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/service"
)

func TestDataItemChangesRouteRequiresCatalogServiceAndTenantPermission(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	if err := db.Exec(`CREATE TABLE meta.data_item_changes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		item_id INTEGER NOT NULL,
		source_identity TEXT NOT NULL,
		operation TEXT NOT NULL,
		snapshot JSON NOT NULL,
		observed_at DATETIME NOT NULL
	)`).Error; err != nil {
		t.Fatalf("create data_item_changes: %v", err)
	}
	for _, tenantID := range []uint{7, 8} {
		if err := db.Exec(`INSERT INTO meta.data_item_changes
			(tenant_id, item_id, source_identity, operation, snapshot, observed_at)
			VALUES (?, ?, ?, 'upsert', '{"name":"orders"}', ?)`,
			tenantID, tenantID, fmt.Sprintf("fingerprint-tenant-%d", tenantID), time.Now().UTC()).Error; err != nil {
			t.Fatalf("insert change: %v", err)
		}
	}

	systemServer := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer catalog-token": {
			ClientID: "addp-catalog", Permissions: []string{metaauthorization.PermissionMetaCatalogRead},
		},
		"Bearer wrong-client-token": {
			ClientID: "addp-asset", Permissions: []string{metaauthorization.PermissionMetaCatalogRead},
		},
		"Bearer no-permission-token": {
			ClientID: "addp-catalog", Permissions: []string{metaauthorization.PermissionMetaLineageRead},
		},
	})
	defer systemServer.Close()

	engineService := service.NewEngineService(db, nil)
	scanService := service.NewScanService(db, engineService)
	cfg := &config.Config{}
	cfg.SystemServiceURL = systemServer.URL
	router := SetupRouter(cfg, db, engineService, scanService, nil, nil, nil, nil, modulelifecycle.NewStandalone("meta"))

	success := performDataItemChangesRequest(router, "catalog-token", "")
	if success.Code != http.StatusOK {
		t.Fatalf("success status = %d, want 200; body = %s", success.Code, success.Body.String())
	}
	var result models.DataItemChangesResponse
	if err := json.Unmarshal(success.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result.Changes) != 1 || result.Changes[0].SourceIdentity != "fingerprint-tenant-7" {
		t.Fatalf("tenant-scoped changes = %#v", result.Changes)
	}

	for _, testCase := range []struct {
		name       string
		token      string
		cursor     string
		wantStatus int
	}{
		{name: "wrong service client", token: "wrong-client-token", wantStatus: http.StatusForbidden},
		{name: "missing permission", token: "no-permission-token", wantStatus: http.StatusForbidden},
		{name: "missing token", wantStatus: http.StatusUnauthorized},
		{name: "invalid cursor", token: "catalog-token", cursor: "!", wantStatus: http.StatusBadRequest},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := performDataItemChangesRequest(router, testCase.token, testCase.cursor)
			if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, testCase.wantStatus, response.Body.String())
			}
		})
	}
}

func performDataItemChangesRequest(handler http.Handler, token, cursor string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/meta/data-items/changes?after_cursor="+cursor, nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
