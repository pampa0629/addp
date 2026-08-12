package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	_ "github.com/addp/common/engine/plugins/builtin/general"
	commonModels "github.com/addp/common/models"
	"github.com/gogama/flatgeobuf/flatgeobuf"
)

func TestOracleTableFlatGeobufSourceIntegration(t *testing.T) {
	if os.Getenv("ADDP_ORACLE_MANAGER_FLATGEOBUF_INTEGRATION") != "1" {
		t.Skip("set ADDP_ORACLE_MANAGER_FLATGEOBUF_INTEGRATION=1 to run Oracle table FlatGeobuf source integration test")
	}

	portText := oracleTableFlatGeobufEnv("ADDP_TEST_ORACLE_PORT", "ORACLE_PORT", "15210")
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("invalid Oracle integration port %q: %v", portText, err)
	}
	host := oracleTableFlatGeobufEnv("ADDP_TEST_ORACLE_HOST", "", "127.0.0.1")
	serviceName := oracleTableFlatGeobufEnv("ADDP_TEST_ORACLE_SERVICE_NAME", "ORACLE_SERVICE_NAME", "FREEPDB1")
	user := oracleTableFlatGeobufEnv("ADDP_TEST_ORACLE_USER", "ORACLE_APP_USER", "business")
	password := oracleTableFlatGeobufEnv("ADDP_TEST_ORACLE_PASSWORD", "ORACLE_APP_PASSWORD", "business_oracle_password")
	tenantID := uint(7)
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":22,"tenant_id":7,"name":"Business Oracle","engine_type":"oracle","engine_origin":"general","connection_info":{"host":%q,"port":%d,"service_name":%q,"user":%q,"password":%q},"lifecycle_state":"active"}`, host, port, serviceName, user, password)
	}))
	defer systemServer.Close()

	store := &recordingTemporaryFlatGeobufStore{exists: true}
	executor := &ManagerVectorTileCacheWorkflowExecutor{
		systemClient: newTestSystemClient(systemServer.URL),
		objectStore:  store, minioEndpoint: "http://minio:9000", minioAccessKey: "ak", minioSecretKey: "sk", defaultBucket: "manager",
	}
	identity := tileCacheTaskTargetIdentity{
		EngineID: 22, SourceKind: "table", Schema: "BUSINESS", Table: "CUSTOMER_LOCATIONS",
		FullName: "BUSINESS.CUSTOMER_LOCATIONS",
	}
	uri, _, facts, cleanup, err := executor.prepareDatabaseTableFlatGeobufSource(context.Background(), tenantID, "oracle-exec-1", identity, commonModels.JSONMap{
		"geometry_column": "SHAPE",
	})
	if err != nil {
		t.Fatal(err)
	}
	if uri != "/vsis3/manager/tenant_7/executions/oracle-exec-1/source.fgb" || facts["access_method"] != "temporary_flatgeobuf" || facts["engine_type"] != "oracle" {
		t.Fatalf("Oracle source uri=%q facts=%#v", uri, facts)
	}
	extent, ok := floatSliceFromConfig(facts["extent"])
	if !ok || extent[0] != 116.397 || extent[1] != 31.230 || extent[2] != 121.474 || extent[3] != 39.908 || intFromTileCacheConfig(facts["extent_srid"], 0) != 4326 {
		t.Fatalf("Oracle source extent facts=%#v", facts)
	}
	reader := flatgeobuf.NewFileReader(bytes.NewReader(store.data))
	defer reader.Close()
	if _, err := reader.Header(); err != nil {
		t.Fatalf("read Oracle temporary FlatGeobuf header: %v", err)
	}
	features, err := reader.DataRem()
	if err != nil && err != io.EOF {
		t.Fatalf("read Oracle temporary FlatGeobuf features: %v", err)
	}
	if len(features) != 2 {
		t.Fatalf("Oracle temporary FlatGeobuf feature count = %d, want 2", len(features))
	}
	if err := cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !store.removed {
		t.Fatal("Oracle temporary FlatGeobuf object was not removed")
	}
}

func oracleTableFlatGeobufEnv(primary, secondary, fallback string) string {
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
