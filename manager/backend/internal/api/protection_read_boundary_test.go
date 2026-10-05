package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	managerprotection "github.com/addp/manager/internal/protection"
	"github.com/gin-gonic/gin"
)

type readBoundaryTestStore struct{ err error }

func (s *readBoundaryTestStore) EnsureCurrent(ctx context.Context, _ int64) error {
	if s.err != nil {
		return s.err
	}
	return ctx.Err()
}

type boundaryResponseWriter struct {
	*httptest.ResponseRecorder
	t        *testing.T
	boundary *managerprotection.ReadBoundary
	wrote    bool
}

func (w *boundaryResponseWriter) Write(body []byte) (int, error) {
	w.wrote = true
	if !w.boundary.HasActiveExecutionsForTenant(7) {
		w.t.Error("read ended before HTTP serialization")
	}
	return w.ResponseRecorder.Write(body)
}

func TestProtectionReadBoundaryCoversResponseAndFailure(t *testing.T) {
	for _, mode := range []string{"json", "stream", "failed", "canceled", "refresh-failed", "missing-tenant"} {
		t.Run(mode, func(t *testing.T) {
			store := &readBoundaryTestStore{}
			if mode == "refresh-failed" {
				store.err = errors.New("checkpoint failed")
			}
			boundary := managerprotection.NewReadBoundary(store)
			router := gin.New()
			router.Use(func(c *gin.Context) {
				if mode != "missing-tenant" {
					setTenantAuthContextForTest(c, 7, 9)
				}
			}, protectionReadBoundary(boundary))
			handled := false
			router.GET("/read", func(c *gin.Context) {
				handled = true
				if !boundary.HasActiveExecutionsForTenant(7) {
					t.Error("handler was not registered")
				}
				switch mode {
				case "json":
					c.JSON(http.StatusOK, gin.H{"value": "protected"})
				case "stream":
					c.Data(http.StatusOK, "application/octet-stream", []byte("protected"))
				case "failed":
					c.AbortWithStatus(http.StatusBadGateway)
				}
			})
			request := httptest.NewRequest(http.MethodGet, "/read", nil)
			if mode == "canceled" {
				ctx, cancel := context.WithCancel(request.Context())
				cancel()
				request = request.WithContext(ctx)
			}
			writer := &boundaryResponseWriter{ResponseRecorder: httptest.NewRecorder(), t: t, boundary: boundary}
			// Failure responses intentionally occur after a failed registration.
			if mode == "refresh-failed" || mode == "canceled" || mode == "missing-tenant" {
				router.ServeHTTP(writer.ResponseRecorder, request)
				if handled || writer.Code != http.StatusForbidden {
					t.Fatalf("failed boundary reached handler: %d", writer.Code)
				}
			} else {
				router.ServeHTTP(writer, request)
				if !handled {
					t.Fatal("handler not reached")
				}
				if mode != "failed" && !writer.wrote {
					t.Fatal("response not serialized")
				}
			}
			if boundary.HasActiveExecutionsForTenant(7) {
				t.Fatal("request retained after completion")
			}
		})
	}
}
