package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonapi "github.com/addp/common/api"
	"github.com/gin-gonic/gin"
)

type strictJSONRequestForTest struct {
	Name string `json:"name"`
}

func TestBindOptionalJSONStrictAcceptsEmptyBody(t *testing.T) {
	c := testContextWithBody("")

	var req strictJSONRequestForTest
	if err := commonapi.BindOptionalJSONStrict(c, &req); err != nil {
		t.Fatalf("BindOptionalJSONStrict() error = %v, want nil", err)
	}
}

func TestBindOptionalJSONStrictRejectsUnknownField(t *testing.T) {
	c := testContextWithBody(`{"name":"ok","legacy":true}`)

	var req strictJSONRequestForTest
	err := commonapi.BindOptionalJSONStrict(c, &req)
	if err == nil {
		t.Fatal("BindOptionalJSONStrict() error = nil, want unknown field error")
	}
	if !commonapi.IsUnknownJSONFieldError(err) {
		t.Fatalf("IsUnknownJSONFieldError(%v) = false, want true", err)
	}
}

func TestBindOptionalJSONStrictRejectsInvalidJSON(t *testing.T) {
	c := testContextWithBody(`{"name":`)

	var req strictJSONRequestForTest
	if err := commonapi.BindOptionalJSONStrict(c, &req); err == nil {
		t.Fatal("BindOptionalJSONStrict() error = nil, want invalid JSON error")
	}
}

func testContextWithBody(body string) *gin.Context {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c
}
