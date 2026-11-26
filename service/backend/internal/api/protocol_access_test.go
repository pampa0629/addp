package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	commonAuth "github.com/addp/common/authorization"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/service/internal/config"
	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/repository"
	serviceInternal "github.com/addp/service/internal/service"
	"github.com/gin-gonic/gin"
)

type protocolTileServiceLookup struct {
	services map[string]*models.TileService
}

func (s *protocolTileServiceLookup) GetServiceForConsumption(serviceName string) (*models.TileService, error) {
	tileService, ok := s.services[serviceName]
	if !ok {
		return nil, commonapi.ErrNotFound
	}
	return tileService, nil
}

type protocolStaticTileReader struct {
	reads [][3]int
}

func (r *protocolStaticTileReader) GetStaticTile(_ context.Context, _ uint, _ *models.TileServiceLayer, z, x, y int, _ string) (*serviceInternal.StaticTile, error) {
	r.reads = append(r.reads, [3]int{z, x, y})
	return &serviceInternal.StaticTile{
		Data:        []byte("tile"),
		ContentType: "application/vnd.mapbox-vector-tile",
	}, nil
}

func TestProtocolRoutesEnforcePrivateServiceTenant(t *testing.T) {
	lookup := &protocolTileServiceLookup{services: map[string]*models.TileService{
		"public": protocolTileService("public", 7, true), "private": protocolTileService("private", 7, false),
	}}
	router := tileProtocolRouter(t, lookup, &protocolStaticTileReader{})

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

func tileProtocolRouter(t *testing.T, lookup tileServiceLookup, reader staticTileReader) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	systemServer := newProtocolAuthSystemServer(t)
	t.Cleanup(systemServer.Close)
	tileEndpointHandler := NewTileEndpointHandler(lookup, reader, nil, nil)
	wmtsHandler := NewWMTSHandler(lookup)
	ogcTilesHandler := NewOGCTilesHandler(lookup, tileEndpointHandler)
	cfg := &config.Config{}
	cfg.SystemServiceURL = systemServer.URL
	return SetupRouter(
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
		nil,
		nil,
		nil,
		modulelifecycle.NewStandalone("service"),
	)
}

func TestTilePublicationLifecycle(t *testing.T) {
	db := tileProtocolTestDB(t)
	const serviceID, tenantID = 1, 7
	statements := []string{
		`INSERT INTO service.tile_services (id, tenant_id, service_name, title, public_access, status, protocols) VALUES (1, 7, 'map', 'Map', true, 'active', CAST('{"xyz":{"enabled":true},"ogc_tiles":{"enabled":true}}' AS BLOB))`,
		`INSERT INTO service.tile_service_layers (id, service_id, layer_name, title, layer_type, enabled) VALUES (1, 1, 'layer', 'Layer', 'static', true), (2, 1, 'sibling', 'Sibling', 'static', true)`,
		`INSERT INTO service.tile_service_layers (id, service_id, layer_name, title, layer_type, layer_config, enabled) VALUES (3, 1, 'cached', 'Cached', 'dynamic', CAST('{"cache":{"enabled":true}}' AS BLOB), false)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := serviceInternal.NewTileServiceService(repository.NewTileServiceRepository(db), nil, "")
	reader := &protocolStaticTileReader{}
	router := tileProtocolRouter(t, svc, reader)
	changeService := func(body string) {
		t.Helper()
		var req models.UpdateTileServiceRequest
		if err := json.Unmarshal([]byte(body), &req); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.UpdateService(serviceID, tenantID, &req); err != nil {
			t.Fatal(err)
		}
	}
	setLayerEnabled := func(enabled bool) {
		t.Helper()
		if _, err := svc.UpdateLayer(context.Background(), 1, serviceID, tenantID, &models.UpdateTileLayerRequest{Enabled: &enabled}); err != nil {
			t.Fatal(err)
		}
	}

	const xyz = "/tiles/map/layer/3/2/5.mvt"
	const ogc = "/ogc/tiles/map/tiles/layer/WebMercatorQuad/3/5/2"
	const wmts = "/wmts/map"
	const sets = "/ogc/tiles/map/tiles"
	get := func(path string, want int) string {
		t.Helper()
		w := httptest.NewRecorder()
		before := len(reader.reads)
		router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != want {
			t.Fatalf("%s status = %d, want %d: %s", path, w.Code, want, w.Body)
		}
		if want != http.StatusOK {
			ct := "application/json"
			if path == wmts {
				ct = "application/xml"
			}
			if !strings.HasPrefix(w.Header().Get("Content-Type"), ct) || len(reader.reads) != before {
				t.Fatalf("rejected %s: type %s, storage reads %d", path, w.Header().Get("Content-Type"), len(reader.reads)-before)
			}
		} else if path == xyz || path == ogc {
			if len(reader.reads) != before+1 || reader.reads[before] != [3]int{3, 2, 5} || w.Body.String() != "tile" {
				t.Fatalf("%s returned %q, read %v", path, w.Body.String(), reader.reads[before:])
			}
		}
		return w.Body.String()
	}
	get(xyz, 200)
	get(ogc, 200)
	changeService(`{"status":"inactive"}`)
	for _, path := range []string{xyz, ogc, wmts, sets, "/ogc/tiles/map", "/ogc/tiles/map/conformance", "/ogc/tiles/map/tileMatrixSets", "/ogc/tiles/map/tileMatrixSets/WebMercatorQuad"} {
		get(path, 503)
	}
	changeService(`{"public_access":false}`)
	get(wmts, 401)
	get(ogc, 401)
	changeService(`{"public_access":true}`)
	changeService(`{"status":"active"}`)
	changeService(`{"protocols":{"xyz":{"enabled":false}}}`)
	get(xyz, 404)
	get(wmts, 404)
	get(ogc, 200)
	changeService(`{"protocols":{"xyz":{"enabled":true}}}`)
	changeService(`{"protocols":{"ogc_tiles":{"enabled":false}}}`)
	get(xyz, 200)
	get(wmts, 200)
	get(ogc, 404)
	get(sets, 404)
	changeService(`{"protocols":{"ogc_tiles":{"enabled":true}}}`)
	setLayerEnabled(false)
	get(xyz, 404)
	get(ogc, 404)
	get("/tiles/map/cached/3/2/5.mvt", 404)
	get("/ogc/tiles/map/tiles/cached/WebMercatorQuad/3/5/2", 404)
	get("/tiles/map/sibling/3/2/5.mvt", 200)
	for _, path := range []string{wmts, sets} {
		body := get(path, 200)
		if strings.Contains(body, "/layer/") || !strings.Contains(body, "/sibling/") {
			t.Fatalf("%s discovery: %s", path, body)
		}
	}
	setLayerEnabled(true)
	get(xyz, 200)
	get(ogc, 200)
	for _, path := range []string{wmts, sets} {
		if !strings.Contains(get(path, 200), "/layer/") {
			t.Fatalf("%s missing restored layer", path)
		}
	}
}

func protocolTileService(name string, tenantID uint, publicAccess bool) *models.TileService {
	return &models.TileService{
		ID:           tenantID,
		TenantID:     tenantID,
		ServiceName:  name,
		Title:        name,
		PublicAccess: publicAccess,
		Status:       "active",
		Protocols:    models.JSONB{"xyz": map[string]interface{}{"enabled": true}, "ogc_tiles": map[string]interface{}{"enabled": true}},
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
		_ = json.NewEncoder(w).Encode(protocolAuthContext(tenantID))
	}))
}

func protocolAuthContext(tenantID uint) commonAuth.AuthContext {
	tenantIDText := strconv.FormatUint(uint64(tenantID), 10)
	membershipID := "1"
	clientID := "addp-web"
	issuedAt := time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)
	return commonAuth.AuthContext{
		SchemaVersion: commonAuth.AuthContextSchemaVersion,
		Principal:     commonAuth.AuthPrincipal{Type: "user", ID: "1"},
		Context: commonAuth.AuthSessionContext{
			Type: "tenant", TenantID: &tenantIDText, TenantMembershipID: &membershipID,
		},
		Authentication: commonAuth.AuthenticationFacts{
			Methods: []string{"password"}, AssuranceLevel: "aal1", AuthenticatedAt: issuedAt,
		},
		Client: commonAuth.ClientConstraints{
			ClientID: &clientID, Audiences: []string{"addp.api"}, ScopeMode: "unrestricted", Scopes: []string{},
		},
		Organization: commonAuth.OrganizationContext{
			Departments: []commonAuth.DepartmentMembership{}, ProjectGroups: []commonAuth.ProjectGroupMembership{},
		},
		Authorization: commonAuth.AuthorizationFacts{
			AuthorizationVersion: "1", RoleAssignments: []commonAuth.RoleAssignment{},
		},
		Token: commonAuth.TokenFacts{
			Type: "first_party_access_token", IssuedAt: issuedAt, ExpiresAt: issuedAt.Add(time.Hour),
		},
	}
}
