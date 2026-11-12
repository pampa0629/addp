package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/engine/plugin"
	commonauth "github.com/addp/common/middleware/auth"
	"github.com/addp/common/production"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type productionReceiptFixture struct {
	receipt production.Receipt
	err     error
	calls   int
}

func (f *productionReceiptFixture) ReadCommittedProduction(_ context.Context, tenant uint, ref production.Reference) (*production.Receipt, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if tenant != uint(f.receipt.TenantID) || ref != f.receipt.Reference {
		return nil, gorm.ErrRecordNotFound
	}
	return &f.receipt, nil
}

func TestDevelopProductionReceiptRequiresExactTenantService(t *testing.T) {
	auth := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer system":        {ClientID: "addp-system", Permissions: []string{"develop.production_receipt.read"}},
		"Bearer meta":          {ClientID: "addp-meta", Permissions: []string{"develop.production_receipt.read"}},
		"Bearer other":         {ClientID: "addp-transfer", Permissions: []string{"develop.production_receipt.read"}},
		"Bearer no-permission": {ClientID: "addp-system", Permissions: []string{"develop.file_execution.read"}},
	})
	defer auth.Close()
	target := plugin.FileItemPath(11, "result.gpkg")
	reader := &productionReceiptFixture{receipt: production.Receipt{Reference: production.Reference{Owner: "develop", ExecutionID: uuid.NewString(), ResultID: uuid.NewString()}, TenantID: 7, TargetEngineVersion: 2, Attempt: 1, OperationID: uuid.NewString(), CommittedAt: time.Now().UTC(), Primary: target, Objects: []production.CommittedObject{{Target: target, Created: true}}, Sources: production.SourceEvidence{Kind: production.SourcesUnknown}}}
	reader.receipt.OperationID = reader.receipt.Reference.ResultID
	router := gin.New()
	api := router.Group("/api/v1/develop", commonauth.MustNewMiddleware(commonauth.MiddlewareConfig{SystemURL: auth.URL}), commonauth.MustNewContextGuard("tenant"))
	registerProductionReceiptRoutes(api, reader)
	path := "/api/v1/develop/runtime/productions/" + reader.receipt.Reference.ExecutionID + "/" + reader.receipt.Reference.ResultID + "/receipt"
	for _, tc := range []struct {
		token, path, body string
		status, calls     int
	}{
		{"", path, "", 401, 0}, {"other", path, "", 403, 0}, {"no-permission", path, "", 403, 0},
		{"system", path + "?tenant_id=8", "", 400, 0}, {"system", path, `{}`, 400, 0},
		{"system", strings.Replace(path, reader.receipt.Reference.ResultID, "not-a-uuid", 1), "", 400, 0},
		{"system", path, "", 200, 1}, {"meta", path, "", 200, 2},
		{"system", strings.Replace(path, reader.receipt.Reference.ResultID, uuid.NewString(), 1), "", 404, 3},
	} {
		response := performDevelopCatalogRequest(router, http.MethodGet, tc.path, tc.token, tc.body)
		if response.Code != tc.status || reader.calls != tc.calls {
			t.Fatal(tc, response.Code, reader.calls, response.Body.String())
		}
		if tc.status == 200 {
			var result production.Receipt
			if response.Header().Get("Cache-Control") != "no-store" || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Validate() != nil || result.Reference != reader.receipt.Reference {
				t.Fatal("invalid receipt response", response.Body.String())
			}
		}
	}
	reader.err = errors.New("private database message")
	response := performDevelopCatalogRequest(router, http.MethodGet, path, "system", "")
	if response.Code != 500 || strings.Contains(response.Body.String(), reader.err.Error()) {
		t.Fatal("private error escaped", response.Body.String())
	}
	userAuth := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{"Bearer user": {"develop.production_receipt.read"}})
	defer userAuth.Close()
	userRouter := gin.New()
	group := userRouter.Group("/api/v1/develop", commonauth.MustNewMiddleware(commonauth.MiddlewareConfig{SystemURL: userAuth.URL}), commonauth.MustNewContextGuard("tenant"))
	registerProductionReceiptRoutes(group, reader)
	if got := performDevelopCatalogRequest(userRouter, http.MethodGet, path, "user", ""); got.Code != 403 {
		t.Fatal("User read machine evidence", got.Code)
	}
}
