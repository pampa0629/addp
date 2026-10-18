package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/client"
	commonauth "github.com/addp/common/middleware/auth"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

func TestItemRefreshPreservesSafeDependencyFailures(t *testing.T) {
	for _, test := range []struct{ upstream, want int }{{400, 400}, {401, 503}, {403, 403}, {404, 404}, {409, 409}, {500, 503}, {503, 503}, {504, 504}} {
		t.Run(http.StatusText(test.upstream), func(t *testing.T) {
			tokens := client.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "machine", nil })
			system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"id":9,"tenant_id":1,"engine_type":"nfs","lifecycle_state":"active","connection_status":"online","connection_info":{}}`))
			}))
			defer system.Close()
			calls := 0
			meta := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(test.upstream)
				_, _ = w.Write([]byte(`{"error":"private-secret"}`))
			}))
			defer meta.Close()
			svc := service.NewMetadataService(nil, client.NewSystemClient(system.URL, tokens), nil, nil, nil)
			svc.SetMetaScanUserClient(client.NewMetaScanUserClient(meta.URL))
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			setTenantAuthContextForTest(c, 1, 7)
			c.Params = gin.Params{{Key: "id", Value: "9"}}
			c.Request = httptest.NewRequest(http.MethodPost, "/refresh", strings.NewReader(`{"item_id":8}`))
			c.Request.Header.Set("Authorization", "Bearer user")
			NewMetadataHandler(svc).RefreshItem(c)
			if response.Code != test.want || calls != 1 || strings.Contains(response.Body.String(), "private-secret") {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, calls, response.Body.String())
			}
		})
	}
}

func TestItemRefreshRequiresUserAndBothPermissionsBeforeReadingBody(t *testing.T) {
	for _, name := range []string{"allowed", "no-update", "no-scan", "machine"} {
		t.Run(name, func(t *testing.T) {
			permissions := []string{"manager.data_item.update", "meta.scan_task.execute"}
			if name == "no-update" {
				permissions = permissions[1:]
			}
			if name == "no-scan" {
				permissions = permissions[:1]
			}
			identity := authtest.NewTenantUserAuthContext("1", "7", permissions)
			if name == "machine" {
				identity.Principal.Type = "service_principal"
			}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				if err := commonauth.SetAuthContextForGin(c, identity); err != nil {
					t.Fatal(err)
				}
				c.Next()
			})
			router.POST("/engines/:id/items/refresh", commonauth.MustNewPermissionGuard("manager.data_item.update", "meta.scan_task.execute"), NewMetadataHandler(nil).RefreshItem)
			probe := &uploadReadProbe{}
			request := httptest.NewRequest(http.MethodPost, "/engines/9/items/refresh", probe)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if name == "allowed" {
				if !probe.read || response.Code != 400 {
					t.Fatal("allowed user did not reach body validation")
				}
				return
			}
			if response.Code != 403 || probe.read {
				t.Fatalf("status=%d body-read=%t", response.Code, probe.read)
			}
		})
	}
}

func TestItemRefreshForwardsCurrentUserWithoutServiceFallback(t *testing.T) {
	tokenSource := client.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "machine-token", nil })
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":9,"tenant_id":1,"engine_type":"nfs","lifecycle_state":"active","connection_status":"online","connection_info":{}}`))
	}))
	defer system.Close()
	calls := 0
	meta := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer current-user" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"manual scan requires a user"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","items_scanned":1}`))
	}))
	defer meta.Close()
	svc := service.NewMetadataService(nil, client.NewSystemClient(system.URL, tokenSource), client.NewMetaClient(meta.URL, tokenSource), nil, nil)
	svc.SetMetaScanUserClient(client.NewMetaScanUserClient(meta.URL))
	handler := NewMetadataHandler(svc)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "id", Value: "9"}}
	setTenantAuthContextForTest(c, 1, 7)
	c.Request = httptest.NewRequest(http.MethodPost, "/engines/9/items/refresh", strings.NewReader(`{"item_id":8,"encoding":"gbk"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Authorization", "Bearer current-user")
	handler.RefreshItem(c)
	if recorder.Code != http.StatusOK || calls != 1 {
		t.Fatalf("refresh status=%d calls=%d body=%s", recorder.Code, calls, recorder.Body.String())
	}
}

func TestItemEncodingRefreshRejectsInvalidEncodingWithStableLocalizedContract(t *testing.T) {
	tokenSource := client.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "token", nil })
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 9, "tenant_id": 1, "engine_type": "nfs", "lifecycle_state": "active", "connection_status": "online", "connection_info": map[string]interface{}{}})
	}))
	defer system.Close()
	meta := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/meta/items/8/refresh" {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"upstream-localized-message","error_code":"scan_delimited_options_invalid"}`))
	}))
	defer meta.Close()
	svc := service.NewMetadataService(nil, client.NewSystemClient(system.URL, tokenSource), client.NewMetaClient(meta.URL, tokenSource), nil, nil)
	svc.SetMetaScanUserClient(client.NewMetaScanUserClient(meta.URL))
	handler := NewMetadataHandler(svc)
	for _, lang := range []string{"zh-cn", "en"} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Params = gin.Params{{Key: "id", Value: "9"}}
		setTenantAuthContextForTest(c, 1, 7)
		c.Set("addp_lang", lang)
		c.Request = httptest.NewRequest(http.MethodPost, "/engines/9/items/refresh", strings.NewReader(`{"item_id":8,"encoding":"bad"}`))
		c.Request.Header.Set("Authorization", "Bearer current-user")
		c.Request.Header.Set("Content-Type", "application/json")
		handler.RefreshItem(c)
		var body map[string]string
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if recorder.Code != 400 || body["error_code"] != "scan_delimited_options_invalid" || body["error"] == "" || strings.Contains(body["error"], "upstream-localized-message") {
			t.Fatalf("response=%d %#v", recorder.Code, body)
		}
		if lang == "zh-cn" && !strings.Contains(body["error"], "编码") {
			t.Fatal(body)
		}
		if lang == "en" && !strings.Contains(body["error"], "Encoding") {
			t.Fatal(body)
		}
	}
}
