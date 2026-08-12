package preview

import (
	"bytes"
	"context"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/engine/plugins/oracle"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	commonSpatial "github.com/addp/common/spatial"
	"github.com/addp/manager/internal/models"
	"github.com/gogama/flatgeobuf/flatgeobuf"
	"github.com/gogama/flatgeobuf/flatgeobuf/flat"
)

func TestIntegrationOracleFlatGeobufQuickView(t *testing.T) {
	if os.Getenv("ADDP_ORACLE_MANAGER_FLATGEOBUF_INTEGRATION") != "1" {
		t.Skip("set ADDP_ORACLE_MANAGER_FLATGEOBUF_INTEGRATION=1 to run Oracle Manager FlatGeobuf integration test")
	}

	portText := oracleManagerIntegrationEnv("ADDP_TEST_ORACLE_PORT", "ORACLE_PORT", "15210")
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("invalid Oracle integration port %q: %v", portText, err)
	}
	enginePlugin, err := plugin.Get("oracle")
	if err != nil {
		t.Fatalf("get Oracle engine plugin: %v", err)
	}
	if _, ok := enginePlugin.(plugin.TableReadSessionProvider); !ok {
		t.Fatalf("Oracle engine plugin %T does not implement TableReadSessionProvider", enginePlugin)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	req := &PreviewRequest{
		Engine: &models.Engine{
			ID:           92001,
			Name:         "business-oracle",
			EngineType:   "oracle",
			EngineOrigin: "general",
			ConnectionInfo: commonModels.ConnectionInfo{
				"host":         oracleManagerIntegrationEnv("ADDP_TEST_ORACLE_HOST", "", "127.0.0.1"),
				"port":         port,
				"service_name": oracleManagerIntegrationEnv("ADDP_TEST_ORACLE_SERVICE_NAME", "ORACLE_SERVICE_NAME", "FREEPDB1"),
				"user":         oracleManagerIntegrationEnv("ADDP_TEST_ORACLE_USER", "ORACLE_APP_USER", "business"),
				"password":     oracleManagerIntegrationEnv("ADDP_TEST_ORACLE_PASSWORD", "ORACLE_APP_PASSWORD", "business_oracle_password"),
			},
		},
		EnginePlugin: enginePlugin,
		ItemType:     "table",
		ProviderPath: plugin.TabularItemPath(
			92001,
			plugin.CatalogTermSchema,
			"BUSINESS",
			"CUSTOMER_LOCATIONS",
		),
	}

	result, err := openDatabaseFlatGeobufFeatureReader(ctx, req, "SHAPE", 10)
	if err != nil {
		t.Fatalf("open Oracle FlatGeobuf feature reader: %v", err)
	}
	defer func() {
		if err := result.Close(context.Background()); err != nil {
			t.Errorf("close Oracle FlatGeobuf feature reader: %v", err)
		}
	}()
	if result.Options.SRID != 4326 || result.Options.GeometryType != "Point" {
		t.Fatalf("Oracle FlatGeobuf options = %#v, want EPSG:4326 Point", result.Options)
	}
	if result.Options.DefaultEncoding != string(format.GeometryEncodingEWKB) {
		t.Fatalf("Oracle FlatGeobuf encoding = %q, want ewkb", result.Options.DefaultEncoding)
	}

	var output bytes.Buffer
	if err := commonSpatial.WriteFlatGeobuf(ctx, &output, result.Reader, result.Options); err != nil {
		t.Fatalf("write Oracle FlatGeobuf quick view: %v", err)
	}
	if output.Len() == 0 {
		t.Fatal("Oracle FlatGeobuf quick view output is empty")
	}

	reader := flatgeobuf.NewFileReader(bytes.NewReader(output.Bytes()))
	defer reader.Close()
	header, err := reader.Header()
	if err != nil {
		t.Fatalf("read Oracle FlatGeobuf header: %v", err)
	}
	if header.GeometryType() != flat.GeometryTypePoint {
		t.Fatalf("Oracle FlatGeobuf geometry type = %s, want Point", header.GeometryType())
	}
	if crs := header.Crs(nil); crs == nil || crs.Code() != 4326 || string(crs.Org()) != "EPSG" {
		t.Fatalf("Oracle FlatGeobuf CRS = %#v, want EPSG:4326", crs)
	}
	features, err := reader.DataRem()
	if err != nil && err != io.EOF {
		t.Fatalf("read Oracle FlatGeobuf features: %v", err)
	}
	if len(features) != 2 {
		t.Fatalf("Oracle FlatGeobuf feature count = %d, want 2", len(features))
	}
	for index := range features {
		var geometry flat.Geometry
		if features[index].Geometry(&geometry) == nil || geometry.Type() != flat.GeometryTypePoint || geometry.XyLength() != 2 {
			t.Fatalf("Oracle FlatGeobuf feature[%d] geometry is not a 2D Point", index)
		}
	}
}

func oracleManagerIntegrationEnv(primary, secondary, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(primary)); value != "" {
		return value
	}
	if secondary != "" {
		if value := strings.TrimSpace(os.Getenv(secondary)); value != "" {
			return value
		}
	}
	return fallback
}
