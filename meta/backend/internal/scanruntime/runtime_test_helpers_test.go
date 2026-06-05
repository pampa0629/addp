package scanruntime

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

func openObjectCatalogScanTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return metatest.OpenMetadataDB(t)
}

func strPtr(s string) *string {
	return &s
}

func assertShapefileLogicalItem(t *testing.T, attrs models.JSONMap, wantPaths, unexpectedPaths []string) {
	t.Helper()
	if got := commonJSON.String(attrs, "item", "layout"); got != string(format.LayoutMulti) {
		t.Fatalf("item.layout = %q, want multi", got)
	}
	if got := commonJSON.String(attrs, "item", "format"); got != string(format.FormatShapefile) {
		t.Fatalf("item.format = %q, want shapefile", got)
	}
	refs := commonJSON.InterfaceSlice(commonJSON.Section(attrs, "item")["refs"])
	if len(refs) != len(wantPaths) {
		t.Fatalf("item.refs = %#v, want %d refs", refs, len(wantPaths))
	}
	paths := make([]string, 0, len(refs))
	for _, ref := range refs {
		item := commonJSON.InterfaceMap(ref)
		if path := commonJSON.InterfaceString(item["path"]); path != "" {
			paths = append(paths, path)
		}
	}
	for _, want := range wantPaths {
		if !containsString(paths, want) {
			t.Fatalf("item.refs paths = %#v, want %s", paths, want)
		}
	}
	for _, unexpected := range unexpectedPaths {
		if containsString(paths, unexpected) {
			t.Fatalf("item.refs paths = %#v, must not include non-created ref %s", paths, unexpected)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func pluginRegisterForTest(t *testing.T, enginePlugin plugin.EnginePlugin) {
	t.Helper()
	plugin.Register(enginePlugin)
	t.Cleanup(func() {
		plugin.Unregister(enginePlugin.Type())
	})
}

type staticObjectContentReader struct {
	content string
}

func (r staticObjectContentReader) Type() string         { return "static" }
func (r staticObjectContentReader) DisplayName() string  { return "static" }
func (r staticObjectContentReader) EngineOrigin() string { return "general" }
func (r staticObjectContentReader) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (r staticObjectContentReader) ValidateConnectionInfo(plugin.ConnectionInfo) error { return nil }
func (r staticObjectContentReader) DefaultPort() int                                   { return 0 }
func (r staticObjectContentReader) RequiredFields() []string                           { return nil }
func (r staticObjectContentReader) SensitiveFields() []string                          { return nil }
func (r staticObjectContentReader) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (r staticObjectContentReader) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemantics{}
}
func (r staticObjectContentReader) OpenContent(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(r.content)), nil
}
