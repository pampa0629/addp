package protection

import (
	"context"
	"database/sql"
	"errors"
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
	installActiveFlatServiceProjection(t, store, 91, models.GenerateItemFingerprint(91, schemaName+"."+tableName), fields)
	protect, end, err := NewGate(store).BeginPreparedQuery(ctx, 7, enginePlugin, prepared, nil)
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
}, engineID uint, identity string, fields []datatype.FieldInfo) dataprotection.Projection {
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
		TargetEngineID: engineID,
		SchemaVersion:  dataprotection.ProjectionSchemaV5, ProjectionID: "projection-service-postgres", Revision: "00000000000000000001",
		ConsumerOwner: "service", State: dataprotection.ProjectionStateActive,
		Target:             dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: identity},
		SourceSnapshotHash: snapshot,
		Rules: []dataprotection.Rule{{Action: "service_execute", Component: component, Decision: dataprotection.Decision{
			Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV2,
			Parameters:         map[string]interface{}{"prefix_runes": 3, "suffix_runes": 4, "mask_rune": "*"},
			InvalidValueEffect: dataprotection.EffectSuppress,
		}}}, ValidFrom: now.Add(-time.Minute), ExpiresAt: dataprotection.ProjectionDeadline(now.Add(time.Hour)),
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
	return projection
}

func TestServiceRoleChoiceAgainstPostgres(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("SERVICE_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("SERVICE_POSTGRES_TEST_DSN is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	table := fmt.Sprintf("persons_role_%d", time.Now().UnixNano())
	if _, err := db.Exec(`CREATE SCHEMA IF NOT EXISTS service_security_it`); err != nil {
		t.Fatal(err)
	}
	qualified := `"service_security_it"."` + table + `"`
	if _, err := db.Exec(`CREATE TABLE ` + qualified + ` (id bigint PRIMARY KEY, phone text)`); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DROP TABLE ` + qualified)
	if _, err := db.Exec(`INSERT INTO ` + qualified + ` VALUES (1,'13661384499'),(2,NULL)`); err != nil {
		t.Fatal(err)
	}
	engine, err := plugin.Get("postgresql")
	if err != nil {
		t.Fatal(err)
	}
	provider := engine.(plugin.QueryRuntimeProvider)
	prepare := func() plugin.PreparedQuery {
		q, err := provider.PrepareQuery(t.Context(), postgresServiceTestConnectionInfo(t, dsn), plugin.QueryRequest{EngineID: 91, Language: "sql", Query: `SELECT id,phone AS phone_alias,phone AS phone_copy FROM ` + qualified + ` ORDER BY id LIMIT 3`, Options: plugin.QueryOptions{EngineID: 91, EngineType: "postgresql", ReadOnly: true, Limit: 3}})
		if err != nil {
			t.Fatal(err)
		}
		return q
	}
	lineage, err := prepare().OutputLineage(t.Context())
	if err != nil || len(lineage.Sources) != 1 {
		t.Fatal(err)
	}
	store := serviceProjectionStore(t)
	p := installActiveFlatServiceProjection(t, store, 91, models.GenerateItemFingerprint(91, "service_security_it."+table), lineage.Sources[0].Fields)
	p.Revision = "00000000000000000002"
	p.Rules[0].RoleOptions = &dataprotection.RoleMaskingOptions{PolicyID: "c4d8478d-5a76-49de-a073-0c107c34b0fd", PolicyRevision: 1, AssessmentRevision: 1, Options: []dataprotection.RoleMaskingOption{{ID: "c5024432-a0a1-498c-b320-5cb4547515a3", Name: "Full mask", RoleKeys: []string{"custom.analytics"}, Decision: dataprotection.Decision{Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV2, Parameters: map[string]any{"prefix_runes": 0, "suffix_runes": 0, "mask_rune": "*"}, InvalidValueEffect: dataprotection.EffectSuppress}}}}
	apply := func(previous, next, change string) {
		if err := p.Seal(); err != nil {
			t.Fatal(err)
		}
		if err := store.ApplyBatch(t.Context(), 7, previous, &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, Changes: []dataprotection.ProjectionChange{{ChangeID: change, Operation: dataprotection.ChangeOperationUpsert, Projection: &p}}, NextCursor: next}, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	apply("cursor-service-postgres", "role-2", "role-upsert")
	gate := NewGate(store)
	selection := &dataprotection.QueryRoleSelection{Choices: []dataprotection.FieldRoleChoice{{Target: p.Target, ComponentKey: "phone", Selection: dataprotection.RoleOptionSelection{PolicyID: p.Rules[0].RoleOptions.PolicyID, PolicyRevision: 1, AssessmentRevision: 1, OptionID: p.Rules[0].RoleOptions.Options[0].ID}}}, RoleKeys: []string{"custom.analytics"}, ExpiresAt: time.Now().Add(time.Minute)}
	for _, selected := range []bool{false, true} {
		q := prepare()
		var choice *dataprotection.QueryRoleSelection
		if selected {
			choice = selection
		}
		protect, end, err := gate.BeginPreparedQuery(t.Context(), 7, engine, q, choice)
		if err != nil {
			t.Fatal(err)
		}
		result, err := q.Execute(t.Context())
		if err == nil {
			err = protect(result)
		}
		end()
		if err != nil {
			t.Fatal(err)
		}
		want := "136****4499"
		if selected {
			want = "***********"
		}
		if len(result.Rows) != 2 || result.Rows[0]["phone_alias"] != want || result.Rows[0]["phone_copy"] != want || result.Rows[1]["phone_alias"] != nil {
			t.Fatal("actual PostgreSQL alias/copy masking differs")
		}
	}
	q := prepare()
	protect, end, err := gate.BeginPreparedQuery(t.Context(), 7, engine, q, selection)
	if err != nil {
		t.Fatal(err)
	}
	result, err := q.Execute(t.Context())
	if err != nil {
		end()
		t.Fatal(err)
	}
	p.Revision = "00000000000000000003"
	p.Rules[0].RoleOptions = nil
	apply("role-2", "role-3", "role-revoke")
	if err := protect(result); !errors.Is(err, dataprotection.ErrRoleOptionUnavailable) {
		end()
		t.Fatalf("revoked prepared delivery: %v", err)
	}
	end()
	if gate.HasActiveExecutionsForTenant(7) {
		t.Fatal("request remained active")
	}
	var phone string
	if err := db.QueryRow(`SELECT phone FROM ` + qualified + ` WHERE id=1`).Scan(&phone); err != nil || phone != "13661384499" {
		t.Fatal("source changed", err)
	}
}
