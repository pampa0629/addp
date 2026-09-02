package protection

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/common/models"
)

func TestServiceExecuteProtectionAgainstPostgres(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("SERVICE_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("SERVICE_POSTGRES_TEST_DSN is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	schemaName := "service_security_it"
	tableName := fmt.Sprintf("persons_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS "%s"`, schemaName)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE "%s"."%s" (id bigint NOT NULL, phone text)`, schemaName, tableName)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf(`DROP TABLE IF EXISTS "%s"."%s"`, schemaName, tableName))
	}()
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO "%s"."%s" (id, phone) VALUES (1, '13661384499')`, schemaName, tableName)); err != nil {
		t.Fatal(err)
	}

	enginePlugin, err := plugin.Get("postgresql")
	if err != nil {
		t.Fatal(err)
	}
	provider := enginePlugin.(plugin.QueryRuntimeProvider)
	query := fmt.Sprintf(`
		SELECT addp_source.id, addp_source.phone
		FROM (SELECT id, phone FROM "%s"."%s") AS addp_source
		ORDER BY addp_source.id ASC LIMIT 2`, schemaName, tableName)
	prepared, err := provider.PrepareQuery(ctx, postgresServiceTestConnectionInfo(t, dsn), plugin.QueryRequest{
		EngineID: 91, Language: "sql", Query: query,
		Options: plugin.QueryOptions{EngineID: 91, EngineType: "postgresql", ReadOnly: true, Limit: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	lineage, err := prepared.OutputLineage(ctx)
	if err != nil || len(lineage.Sources) != 1 {
		t.Fatalf("output lineage = %#v, err=%v", lineage, err)
	}
	fields := lineage.Sources[0].Fields
	store := serviceProjectionStore(t)
	installActiveFlatServiceProjection(t, store, models.GenerateItemFingerprint(91, schemaName+"."+tableName), fields)
	protect, end, err := NewGate(store).BeginPreparedQuery(ctx, 7, enginePlugin, prepared)
	if err != nil {
		t.Fatal(err)
	}
	defer end()
	result, err := prepared.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := protect(result); err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.Rows[0]["phone"] != "136****4499" {
		t.Fatalf("protected result = %#v", result)
	}
}

func postgresServiceTestConnectionInfo(t *testing.T, dsn string) plugin.ConnectionInfo {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	port := 5432
	if parsed.Port() != "" {
		port, err = strconv.Atoi(parsed.Port())
		if err != nil {
			t.Fatal(err)
		}
	}
	password, _ := parsed.User.Password()
	sslmode := parsed.Query().Get("sslmode")
	if sslmode == "" {
		sslmode = "disable"
	}
	return plugin.ConnectionInfo{
		"host": parsed.Hostname(), "port": port, "database": strings.TrimPrefix(parsed.Path, "/"),
		"user": parsed.User.Username(), "password": password, "sslmode": sslmode,
	}
}

func installActiveFlatServiceProjection(t *testing.T, store interface {
	ApplyBatch(context.Context, int64, string, *dataprotection.ProjectionChangesResponse, time.Time) error
}, identity string, fields []datatype.FieldInfo) {
	t.Helper()
	now := time.Now().UTC()
	component := dataprotection.Component{
		Key: "phone", Path: []dataprotection.PathSegment{{Name: "phone", Container: "scalar"}},
		ValueType: string(datatype.FieldTypeString),
	}
	var err error
	component.SchemaFingerprint, err = dataprotection.ComponentSchemaFingerprint(fields, component)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := dataprotection.TableSchemaSnapshotHash(fields)
	if err != nil {
		t.Fatal(err)
	}
	projection := dataprotection.Projection{
		SchemaVersion: dataprotection.ProjectionSchemaV1, ProjectionID: "projection-service-postgres", Revision: "00000000000000000001",
		ConsumerOwner: "service", State: dataprotection.ProjectionStateActive,
		Target:             dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: identity},
		SourceSnapshotHash: snapshot,
		Rules: []dataprotection.Rule{{Action: "service_execute", Component: component, Decision: dataprotection.Decision{
			Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV1,
			Parameters:         map[string]interface{}{"prefix_runes": 3, "suffix_runes": 4, "replacement": "****", "exact_runes": 11, "character_class": "ascii_digit"},
			InvalidValueEffect: dataprotection.EffectSuppress,
		}}}, ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyBatch(t.Context(), 7, "", &dataprotection.ProjectionChangesResponse{
		SchemaVersion: dataprotection.ProjectionChangesSchemaV1,
		Changes:       []dataprotection.ProjectionChange{{ChangeID: "change-service-postgres", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}},
		NextCursor:    "cursor-service-postgres",
	}, now); err != nil {
		t.Fatal(err)
	}
}
