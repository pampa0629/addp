package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
)

func TestWebhookErrorMapsOperationalDomainErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "invalid delivery", err: service.ErrWebhookDeliveryInvalid, want: http.StatusBadRequest},
		{name: "missing delivery", err: service.ErrWebhookDeliveryNotFound, want: http.StatusNotFound},
		{name: "not retryable", err: service.ErrWebhookDeliveryNotRetryable, want: http.StatusConflict},
		{name: "test receiver failure", err: service.ErrWebhookTestFailed, want: http.StatusBadGateway},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			webhookError(context, test.err)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d, body=%s", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}
}
