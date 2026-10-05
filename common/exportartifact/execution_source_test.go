package exportartifact

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/client"
	"github.com/addp/common/format"
	"github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type transferSourceCheck struct {
	check    func(*client.CreateTransferExecutionRequest)
	mismatch bool
}

func (s transferSourceCheck) CreateExecution(_ context.Context, request *client.CreateTransferExecutionRequest) (*client.CreateTransferExecutionResponse, error) {
	if s.check != nil {
		s.check(request)
	}
	id := request.ExportSession.ExecutionID
	if s.mismatch {
		id = uuid.NewString()
	}
	return &client.CreateTransferExecutionResponse{ExecutionID: id, Status: StatusPending}, nil
}
func (transferSourceCheck) GetExecution(string, uint) (*client.TransferExecutionResponse, error) {
	return &client.TransferExecutionResponse{Status: StatusPending}, nil
}

func sourceStore(t *testing.T) (*gorm.DB, *GormStore) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = EnsureStore(db, "export_sessions"); err != nil {
		t.Fatal(err)
	}
	return db, NewGormStore(db, "export_sessions")
}

func TestExportSourceIsBoundBeforeDispatchForBothOwners(t *testing.T) {
	for _, owner := range []string{"manager", "develop"} {
		t.Run(owner, func(t *testing.T) {
			_, store := sourceStore(t)
			var service *Service
			transfer := transferSourceCheck{check: func(request *client.CreateTransferExecutionRequest) {
				if request.ExportSession == nil || request.ExportSession.Validate() != nil {
					t.Fatal("unbound transfer request")
				}
				digest, err := client.ExportRequestDigest(request)
				if err != nil {
					t.Fatal(err)
				}
				facts, err := service.ResolveExecutionSource(t.Context(), request.ExportSession.SessionID, 7, client.ExportExecutionSourceRequest{ExecutionID: request.ExportSession.ExecutionID, RequestDigest: digest})
				if err != nil || facts.UserID != 9 || facts.TenantID != 7 {
					t.Fatalf("pre-dispatch facts=%#v err=%v", facts, err)
				}
			}}
			service = NewService(transfer, store, testMinIOClient(t), "manager", owner, "/exports")
			result, err := service.Create(t.Context(), CreateRequest{TenantID: 7, UserID: 9, SourceRef: "source", Format: format.FormatCSV, FileName: "orders"})
			if err != nil || result.TransferExecutionID == "" {
				t.Fatalf("create=%#v err=%v", result, err)
			}
		})
	}
}

func TestExportSourceRejectsWrongBindingAndExpiredOrFinalSession(t *testing.T) {
	for _, check := range []string{"valid", "tenant", "id", "uuid", "digest", "failed", "running", "success", "expired", "no_user", "missing_digest"} {
		t.Run(check, func(t *testing.T) {
			_, store := sourceStore(t)
			session := &Session{TenantID: 7, UserID: 9, TransferExecutionID: uuid.NewString(), ExecutionRequestDigest: strings.Repeat("a", 64), Status: StatusPending, CreatedAt: time.Now()}
			request := client.ExportExecutionSourceRequest{ExecutionID: session.TransferExecutionID, RequestDigest: session.ExecutionRequestDigest}
			tenant := uint(7)
			switch check {
			case "tenant":
				tenant = 8
			case "uuid":
				request.ExecutionID = uuid.NewString()
			case "digest":
				request.RequestDigest = strings.Repeat("b", 64)
			case "failed", "running", "success":
				session.Status = check
			case "expired":
				session.CreatedAt = time.Now().Add(-7 * time.Hour)
			case "no_user":
				session.UserID = 0
			case "missing_digest":
				session.ExecutionRequestDigest = ""
			}
			if err := store.Create(t.Context(), session); err != nil {
				t.Fatal(err)
			}
			id := session.ID
			if check == "id" {
				id++
			}
			service := NewService(nil, store, nil, "manager", "manager", "/exports")
			facts, err := service.ResolveExecutionSource(t.Context(), id, tenant, request)
			if check == "valid" {
				if err != nil || facts.UserID != 9 {
					t.Fatalf("valid facts=%#v err=%v", facts, err)
				}
			} else if err != ErrSessionNotFound || facts != nil {
				t.Fatalf("untrusted source accepted: facts=%#v err=%v", facts, err)
			}
		})
	}
}

func TestExportSourceHTTPRequiresTransferClientAndExactPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, owner := range []string{"manager", "develop"} {
		t.Run(owner, func(t *testing.T) {
			_, store := sourceStore(t)
			session := &Session{TenantID: 7, UserID: 9, TransferExecutionID: uuid.NewString(), ExecutionRequestDigest: strings.Repeat("a", 64), Status: StatusPending}
			if err := store.Create(t.Context(), session); err != nil {
				t.Fatal(err)
			}
			permission := owner + ".export_provenance.read"
			authServer := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
				"Bearer transfer":      {ClientID: "addp-transfer", Permissions: []string{permission}},
				"Bearer wrong-client":  {ClientID: "addp-meta", Permissions: []string{permission}},
				"Bearer no-permission": {ClientID: "addp-transfer", Permissions: []string{"system.engine.read"}},
			})
			defer authServer.Close()
			service := NewService(nil, store, nil, "manager", owner, "/exports")
			makeRouter := func(systemURL string) *gin.Engine {
				router := gin.New()
				router.Use(auth.MustNewMiddleware(auth.MiddlewareConfig{SystemURL: systemURL}))
				group := router.Group("/exports", ExecutionSourceGuards(permission)...)
				group.POST("/:id", func(c *gin.Context) { ServeExecutionSource(c, service) })
				return router
			}
			router := makeRouter(authServer.URL)
			body := `{"execution_id":"` + session.TransferExecutionID + `","request_digest":"` + session.ExecutionRequestDigest + `"}`
			for _, check := range []struct {
				token, body string
				status      int
			}{
				{"transfer", body, 200}, {"wrong-client", body, 403}, {"no-permission", body, 403},
				{"transfer", strings.TrimSuffix(body, "}") + `,"user_id":10}`, 400},
				{"transfer", strings.Replace(body, session.ExecutionRequestDigest, strings.Repeat("b", 64), 1), 404},
				{"transfer", body + ` {}`, 400},
			} {
				request := httptest.NewRequest("POST", "/exports/"+strconv.Itoa(int(session.ID)), strings.NewReader(check.body))
				request.Header.Set("Authorization", "Bearer "+check.token)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				if response.Code != check.status {
					t.Fatalf("%s HTTP=%d want=%d", check.token, response.Code, check.status)
				}
				if response.Code == 200 {
					var facts map[string]interface{}
					if err := json.Unmarshal(response.Body.Bytes(), &facts); err != nil {
						t.Fatal(err)
					}
					if len(facts) != 2 || facts["user_id"] != float64(9) || facts["tenant_id"] != float64(7) {
						t.Fatal("unexpected provenance response")
					}
				}
			}
			userServer := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{"Bearer user": {permission}})
			defer userServer.Close()
			request := httptest.NewRequest("POST", "/exports/"+strconv.Itoa(int(session.ID)), strings.NewReader(body))
			request.Header.Set("Authorization", "Bearer user")
			response := httptest.NewRecorder()
			makeRouter(userServer.URL).ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("user HTTP=%d", response.Code)
			}
		})
	}
}

func TestMismatchedTransferUUIDFailsSession(t *testing.T) {
	_, store := sourceStore(t)
	service := NewService(transferSourceCheck{mismatch: true}, store, testMinIOClient(t), "manager", "manager", "/exports")
	if _, err := service.Create(t.Context(), CreateRequest{TenantID: 7, UserID: 9, SourceRef: "source", Format: format.FormatCSV}); err == nil {
		t.Fatal("accepted mismatched execution")
	}
	session, err := store.Get(t.Context(), 1, 7, 9)
	if err != nil || session.Status != StatusFailed || session.TransferExecutionID == "" {
		t.Fatal("failed session lost immutable binding")
	}
}
