package api

import (
	"net/http/httptest"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/gin-gonic/gin"
)

func TestParseOptionalRFC3339Query(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest("GET", "/?created_from=2026-09-08T18%3A30%3A00%2B08%3A00", nil)

	parsed, err := parseOptionalRFC3339Query(context, "created_from")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, time.September, 8, 10, 30, 0, 0, time.UTC)
	if parsed == nil || !parsed.Equal(want) || parsed.Location() != time.UTC {
		t.Fatalf("parsed time = %#v, want %s UTC", parsed, want)
	}
}

func TestParseOptionalRFC3339QueryRejectsInvalidOrDuplicateValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, target := range []string{
		"/?created_from=not-a-time",
		"/?created_from=2026-09-08T10%3A30%3A00Z&created_from=2026-09-08T11%3A30%3A00Z",
	} {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest("GET", target, nil)
		if _, err := parseOptionalRFC3339Query(context, "created_from"); err != commonapi.ErrBadRequest {
			t.Fatalf("target %q error = %v, want ErrBadRequest", target, err)
		}
	}
}
