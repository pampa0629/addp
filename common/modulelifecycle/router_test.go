package modulelifecycle

import (
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/addp/common/httpmetrics"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
)

func TestSharedHTTPRouterIncludesEarlyRoutesAndPartialPanic(t *testing.T) {
	previousWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = io.Discard
	t.Cleanup(func() { gin.DefaultErrorWriter = previousWriter })
	registry := prometheus.NewRegistry()
	registry.MustRegister(httpmetrics.Process())
	failures := func() float64 {
		families, err := registry.Gather()
		if err != nil {
			t.Fatal(err)
		}
		for _, family := range families {
			if family.GetName() == "addp_http_server_failures_total" {
				return family.Metric[0].Counter.GetValue()
			}
		}
		t.Fatal("missing failure counter")
		return 0
	}
	before := failures()
	router := NewRouter()
	router.GET("/swagger/*any", func(c *gin.Context) { c.Status(200) })
	router.GET("/partial", func(c *gin.Context) { c.String(200, "safe partial"); panic("private panic") })
	for _, path := range []string{"/swagger/index.html", "/partial", "/unknown-private"} {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
	}
	if failures() != before+1 {
		t.Fatal("recovered partial response was counted as success")
	}
}

func TestProductionGoRoutersUseSingleInstrumentationFactory(t *testing.T) {
	root := filepath.Clean("../..")
	paths, err := filepath.Glob(filepath.Join(root, "*", "backend", "internal", "api", "router.go"))
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, filepath.Join(root, "gateway", "internal", "router", "router.go"), filepath.Join(root, "engines", "duckdb", "internal", "api", "router.go"))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		if strings.Contains(text, "gin.Logger()") || strings.Contains(text, "logging.LoggingMiddleware()") || strings.Contains(text, "requestLogger()") || strings.Contains(text, "requestid.RequestIDMiddleware()") || strings.Contains(text, "requestidmiddleware.RequestIDMiddleware()") || strings.Contains(text, "gin.Default()") || strings.Contains(text, "gin.New()") || !strings.Contains(text, "modulelifecycle.NewRouter(") {
			t.Fatalf("%s bypasses the sole HTTP instrumentation factory", path)
		}
	}
}
