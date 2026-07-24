package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/service/internal/config"
	"github.com/addp/service/internal/models"
	serviceInternal "github.com/addp/service/internal/service"
	"github.com/gin-gonic/gin"
)

type protocolTileServiceLookup struct {
	services map[string]*models.TileService
}

func (s *protocolTileServiceLookup) GetServiceModelByName(serviceName string) (*models.TileService, error) {
	tileService, ok := s.services[serviceName]
	if !ok {
		return nil, errors.New("service not found")
	}
	return tileService, nil
}

type protocolStaticTileReader struct{}

func (protocolStaticTileReader) GetStaticTile(
	context.Context,
	uint,
	*models.TileServiceLayer,
	int,
	int,
	int,
	string,
) (*serviceInternal.StaticTile, error) {
	return &serviceInternal.StaticTile{
		Data:        []byte("tile"),
		ContentType: "application/vnd.mapbox-vector-tile",
	}, nil
}

func TestProtocolRoutesEnforcePrivateServiceTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	systemServer := newProtocolAuthSystemServer(t)
	defer systemServer.Close()

	lookup := &protocolTileServiceLookup{services: map[string]*models.TileService{
		"public":  protocolTileService("public", 7, true),
		"private": protocolTileService("private", 7, false),
	}}
	tileEndpointHandler := NewTileEndpointHandler(lookup, protocolStaticTileReader{}, nil, nil)
	wmtsHandler := NewWMTSHandler(lookup)
	ogcTilesHandler := NewOGCTilesHandler(lookup, tileEndpointHandler)
	cfg := &config.Config{}
	cfg.SystemServiceURL = systemServer.URL
	router := SetupRouter(
		cfg,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		tileEndpointHandler,
		wmtsHandler,
		ogcTilesHandler,
		nil,
		nil,
		nil,
		nil,
	)

	tests := []struct {
		name       string
		path       string
		token      string
		wantStatus int
	}{
		{name: "public xyz without token", path: "/tiles/public/layer/0/0/0.mvt", wantStatus: http.StatusOK},
		{name: "private xyz without token", path: "/tiles/private/layer/0/0/0.mvt", wantStatus: http.StatusUnauthorized},
		{name: "private xyz same tenant", path: "/tiles/private/layer/0/0/0.mvt", token: "tenant-7", wantStatus: http.StatusOK},
		{name: "private xyz other tenant", path: "/tiles/private/layer/0/0/0.mvt", token: "tenant-8", wantStatus: http.StatusForbidden},
		{name: "private wmts same tenant", path: "/wmts/private", token: "tenant-7", wantStatus: http.StatusOK},
		{name: "private wmts other tenant", path: "/wmts/private", token: "tenant-8", wantStatus: http.StatusForbidden},
		{name: "public ogc tiles without token", path: "/ogc/tiles/public", wantStatus: http.StatusOK},
		{name: "private ogc tiles without token", path: "/ogc/tiles/private", wantStatus: http.StatusUnauthorized},
		{name: "private ogc tiles same tenant", path: "/ogc/tiles/private", token: "tenant-7", wantStatus: http.StatusOK},
		{name: "private ogc tiles other tenant", path: "/ogc/tiles/private", token: "tenant-8", wantStatus: http.StatusForbidden},
		{name: "private conformance requires auth", path: "/ogc/tiles/private/conformance", wantStatus: http.StatusUnauthorized},
		{name: "private matrix sets enforce tenant", path: "/ogc/tiles/private/tileMatrixSets", token: "tenant-8", wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.token != "" {
				request.Header.Set("Authorization", "Bearer "+tt.token)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, tt.wantStatus, response.Body.String())
			}
		})
	}
}

func protocolTileService(name string, tenantID uint, publicAccess bool) *models.TileService {
	return &models.TileService{
		ID:           tenantID,
		TenantID:     tenantID,
		ServiceName:  name,
		Title:        name,
		PublicAccess: publicAccess,
		Layers: []models.TileServiceLayer{
			{ID: 1, LayerName: "layer", LayerType: "static", Enabled: true},
		},
	}
}

func newProtocolAuthSystemServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var tenantID uint
		switch r.Header.Get("Authorization") {
		case "Bearer tenant-7":
			tenantID = 7
		case "Bearer tenant-8":
			tenantID = 8
		default:
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"subject_type": "user",
			"user_id":      1,
			"username":     "test-user",
			"tenant_id":    tenantID,
			"auth_type":    "user_access_token",
		})
	}))
}
