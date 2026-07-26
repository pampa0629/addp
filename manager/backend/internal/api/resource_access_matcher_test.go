package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestManagerBrowserResourceRequestMatcher(t *testing.T) {
	gin.SetMode(gin.TestMode)
	contentPaths := []string{
		"/api/v1/manager/storage-stream",
		"/api/v1/manager/downloads/file",
		"/api/v1/manager/storage-assets/12/path/to/file.png",
		"/api/v1/manager/quick-view/flatgeobuf",
		"/api/v1/manager/quick-view/geojson",
	}
	for _, path := range contentPaths {
		if !isManagerContentResourceRequest(newMatcherContext(path)) ||
			isManagerDerivedResourceRequest(newMatcherContext(path)) {
			t.Errorf("content resource path classified incorrectly: %s", path)
		}
	}

	derivedPaths := []string{
		"/api/v1/manager/exports/7/file",
		"/api/v1/manager/model3d_tiles/8/assets/tiles/0.b3dm",
		"/api/v1/manager/raster_mosaic/tiles/2/3/4",
		"/api/v1/manager/raster_cog/1/content",
		"/api/v1/manager/model_3d_glb/1/content",
		"/api/v1/manager/gaussian_splat_ksplat/1/content",
		"/api/v1/manager/point_cloud_copc/1/content",
		"/api/v1/manager/cad-previews/1/manifest",
		"/api/v1/manager/cad-previews/1/tiles/2/3/4",
		"/api/v1/manager/quick-view/tiles/2/3/4.mvt",
	}
	for _, path := range derivedPaths {
		if !isManagerDerivedResourceRequest(newMatcherContext(path)) ||
			isManagerContentResourceRequest(newMatcherContext(path)) {
			t.Errorf("derived resource path classified incorrectly: %s", path)
		}
	}

	disallowed := []string{
		"/api/v1/manager/search",
		"/api/v1/manager/preview",
		"/api/v1/manager/exports/7",
		"/api/v1/manager/raster_cog/1",
		"/api/v1/manager/gaussian_splat_ksplat/1/inspect",
		"/api/v1/manager/cad-preview-tasks/1",
		"/api/v1/manager/quick-view/capability",
	}
	for _, path := range disallowed {
		if isManagerContentResourceRequest(newMatcherContext(path)) ||
			isManagerDerivedResourceRequest(newMatcherContext(path)) {
			t.Errorf("ordinary API path accepted: %s", path)
		}
	}
}

func newMatcherContext(path string) *gin.Context {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodGet, path, nil)
	return context
}
