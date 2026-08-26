package api

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/addp/common/modulelifecycle"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/service"
)

func TestMetaRoutesRejectLegacyInternalAuthenticationWithoutAuthContextLookup(t *testing.T) {
	t.Parallel()

	var authContextRequests atomic.Int32
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authContextRequests.Add(1)
		http.Error(w, "unexpected AuthContext request", http.StatusInternalServerError)
	}))
	defer systemServer.Close()

	db := metatest.OpenMetadataDB(t)
	engineService := service.NewEngineService(db, nil)
	scanService := service.NewScanService(db, engineService)
	cfg := &config.Config{}
	cfg.SystemServiceURL = systemServer.URL
	router := SetupRouter(
		cfg,
		db,
		engineService,
		scanService,
		nil,
		nil,
		nil,
		nil,
		modulelifecycle.NewStandalone("meta"),
	)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/meta/items/51572", nil)
	request.Header.Set("X-Internal-API-Key", "legacy-internal-key")
	request.Header.Set("X-Tenant-ID", "7")
	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, body = %s", response.Code, http.StatusUnauthorized, response.Body.String())
	}
	if got := authContextRequests.Load(); got != 0 {
		t.Fatalf("AuthContext requests = %d, want 0", got)
	}
}
