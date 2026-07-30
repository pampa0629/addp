package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type fakeExecutionAuthorizationService struct {
	issueInput         iam.IssueExecutionAuthorizationInput
	issueFromExecution iam.IssueExecutionAuthorizationFromExecutionInput
	consumeInput       iam.AuthorizeExecutionEngineAccessInput
}

func (service *fakeExecutionAuthorizationService) IssueFromExecution(
	_ context.Context,
	input iam.IssueExecutionAuthorizationFromExecutionInput,
) (*iam.IssuedExecutionAuthorization, error) {
	service.issueFromExecution = input
	return &iam.IssuedExecutionAuthorization{
		ID: 92, ExecutionID: input.ExecutionID, Audience: input.Audience,
		EngineIDs: []int64{12}, Effects: []string{"read"}, ExpiresAt: time.Now().UTC().Add(time.Minute),
		ActorPrincipalID: 7, TenantID: input.TenantID, TenantMembershipID: 8, IssuedAuthorizationVersion: 3,
	}, nil
}

func (service *fakeExecutionAuthorizationService) Issue(
	_ context.Context,
	input iam.IssueExecutionAuthorizationInput,
) (*iam.IssuedExecutionAuthorization, error) {
	service.issueInput = input
	return &iam.IssuedExecutionAuthorization{
		ID: 91, ExecutionID: input.ExecutionID, Audience: input.Audience,
		EngineIDs: []int64{12}, Effects: []string{"read"}, ExpiresAt: time.Now().UTC().Add(time.Minute),
		ActorPrincipalID: 7, TenantID: 5, TenantMembershipID: 8, IssuedAuthorizationVersion: 3,
	}, nil
}

func (service *fakeExecutionAuthorizationService) AuthorizeEngineAccess(
	_ context.Context,
	input iam.AuthorizeExecutionEngineAccessInput,
) (*iam.AuthorizedExecutionEngineAccess, error) {
	service.consumeInput = input
	return &iam.AuthorizedExecutionEngineAccess{
		AuthorizationID: input.AuthorizationID, ExecutionID: input.ExecutionID,
		Audience: "develop", EngineID: input.EngineID, Effects: []string{"read"},
		TenantID: input.TenantID, ExpiresAt: time.Now().UTC().Add(time.Minute),
	}, nil
}

type fakeExecutionEngineResolver struct{}

func (fakeExecutionEngineResolver) GetForExecution(id, tenantID uint) (*models.Engine, error) {
	return &models.Engine{ID: id, TenantID: &tenantID, ConnectionInfo: models.ConnectionInfo{"password": "plain"}}, nil
}

func TestIAMExecutionAuthorizationHandlerUsesUserAndServiceActors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	executionID := "9a21ab1a-2900-42a5-ae91-821339b3fcdd"
	fakeService := &fakeExecutionAuthorizationService{}
	handler, err := NewIAMExecutionAuthorizationHandler(fakeService, fakeExecutionEngineResolver{})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.POST("/api/v1/system/auth/execution-authorizations", handler.Issue)
	router.POST("/api/v1/system/execution-authorizations/:id/engine-accesses", func(c *gin.Context) {
		if err := sharedauth.SetAuthContextForGin(c, testIAMServiceActorContext("tenant", "addp-develop")); err != nil {
			t.Fatal(err)
		}
		c.Next()
	}, handler.AuthorizeEngineAccess)

	issue := performIAMJSONRequest(t, router, http.MethodPost, "/api/v1/system/auth/execution-authorizations", map[string]any{
		"audience": "develop", "execution_id": executionID, "engine_ids": []string{"12"},
		"effects": []string{"read"}, "expires_in": 600,
	}, map[string]string{"Authorization": "Bearer addp_at_user"})
	if issue.Code != http.StatusCreated || fakeService.issueInput.SourceAccessToken != "addp_at_user" ||
		fakeService.issueInput.ExecutionID != uuid.MustParse(executionID) {
		t.Fatalf("issue status=%d input=%#v body=%s", issue.Code, fakeService.issueInput, issue.Body.String())
	}

	consume := performIAMJSONRequest(t, router, http.MethodPost, "/api/v1/system/execution-authorizations/91/engine-accesses", map[string]any{
		"execution_id": executionID, "engine_id": "12", "required_effects": []string{"read"},
	}, nil)
	if consume.Code != http.StatusOK || fakeService.consumeInput.ServiceClientID != "addp-develop" ||
		fakeService.consumeInput.AuthorizationID != 91 || fakeService.consumeInput.EngineID != 12 {
		t.Fatalf("consume status=%d input=%#v body=%s", consume.Code, fakeService.consumeInput, consume.Body.String())
	}
	var response IAMExecutionEngineAccessResponse
	if err := json.Unmarshal(consume.Body.Bytes(), &response); err != nil || response.Engine == nil ||
		response.Engine.ConnectionInfo["password"] != "plain" {
		t.Fatalf("consume response=%#v error=%v", response, err)
	}
}
