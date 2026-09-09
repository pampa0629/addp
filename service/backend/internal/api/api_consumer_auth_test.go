package api

import (
	"net/http"
	"testing"

	commonclient "github.com/addp/common/client"
	"github.com/gin-gonic/gin"
)

func TestAPIConsumerServiceAccessStatus(t *testing.T) {
	tests := []struct {
		name       string
		context    any
		tenantID   uint
		serviceID  uint
		wantStatus int
		wantAPIKey bool
	}{
		{name: "ordinary request", tenantID: 7, serviceID: 11},
		{
			name: "exact query grant",
			context: &commonclient.APIConsumerCredentialValidationResponse{
				Valid: true, TenantID: 7,
				ServiceGrants: []commonclient.APIConsumerServiceReference{{ServiceType: "query", ServiceID: 11}},
			},
			tenantID: 7, serviceID: 11, wantAPIKey: true,
		},
		{
			name: "wrong tenant",
			context: &commonclient.APIConsumerCredentialValidationResponse{
				Valid: true, TenantID: 8,
				ServiceGrants: []commonclient.APIConsumerServiceReference{{ServiceType: "query", ServiceID: 11}},
			},
			tenantID: 7, serviceID: 11, wantStatus: http.StatusForbidden, wantAPIKey: true,
		},
		{
			name: "different service",
			context: &commonclient.APIConsumerCredentialValidationResponse{
				Valid: true, TenantID: 7,
				ServiceGrants: []commonclient.APIConsumerServiceReference{{ServiceType: "query", ServiceID: 12}},
			},
			tenantID: 7, serviceID: 11, wantStatus: http.StatusForbidden, wantAPIKey: true,
		},
		{
			name: "different service type",
			context: &commonclient.APIConsumerCredentialValidationResponse{
				Valid: true, TenantID: 7,
				ServiceGrants: []commonclient.APIConsumerServiceReference{{ServiceType: "tile", ServiceID: 11}},
			},
			tenantID: 7, serviceID: 11, wantStatus: http.StatusForbidden, wantAPIKey: true,
		},
		{
			name:       "invalid projection",
			context:    &commonclient.APIConsumerCredentialValidationResponse{Valid: false, TenantID: 7},
			tenantID:   7,
			serviceID:  11,
			wantStatus: http.StatusUnauthorized,
			wantAPIKey: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(nil)
			if test.context != nil {
				ctx.Set(apiConsumerContextKey, test.context)
			}
			status, apiConsumerRequest := apiConsumerServiceAccessStatus(ctx, test.tenantID, "query", test.serviceID)
			if status != test.wantStatus || apiConsumerRequest != test.wantAPIKey {
				t.Fatalf("status, apiConsumerRequest = %d, %v; want %d, %v", status, apiConsumerRequest, test.wantStatus, test.wantAPIKey)
			}
		})
	}
}
