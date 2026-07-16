package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
)

func TestEmailErrorMapsOperationalDomainErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "invalid delivery", err: service.ErrEmailDeliveryInvalid, want: http.StatusBadRequest},
		{name: "missing delivery", err: service.ErrEmailDeliveryNotFound, want: http.StatusNotFound},
		{name: "not retryable", err: service.ErrEmailDeliveryNotRetryable, want: http.StatusConflict},
		{name: "SMTP unavailable", err: service.ErrEmailSenderUnavailable, want: http.StatusServiceUnavailable},
		{name: "SMTP failure", err: service.ErrEmailTestFailed, want: http.StatusBadGateway},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			emailError(context, test.err)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d, body=%s", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}
}
