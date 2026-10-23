package protection

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/executor"
	"github.com/addp/transfer/internal/testpg"
	_ "github.com/lib/pq"
)

func TestIntegrationPostgresBoundedExportMasksBeforeTargetWrite(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}

	ctx := context.Background()
	connInfo := testpg.ConnInfoFromEnv(t)
	pg := &postgresql.PostgreSQLPlugin{}
	dsn, err := pg.BuildDSN(connInfo)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	testpg.DropSchemasWithPrefixes(t, ctx, db, "transfer_security_test_")
	schema := fmt.Sprintf("transfer_security_test_%d", time.Now().UnixNano())
	testpg.CreateSchema(t, ctx, db, schema)
	sourceTable := "persons_source"
	targetTable := "persons_export"
	queryTargetTable := "persons_query_export"
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE "%s"."%s" (id integer, phone text);
		INSERT INTO "%s"."%s" (id, phone) VALUES (1, '13661384499');
	`, schema, sourceTable, schema, sourceTable)); err != nil {
		t.Fatalf("prepare PostgreSQL source: %v", err)
	}

	model := plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema)
	sourcePath := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermSchema, schema, plugin.EngineCatalogTermTable, plugin.EngineCatalogKindTable, sourceTable)
	targetPath := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermSchema, schema, plugin.EngineCatalogTermTable, plugin.EngineCatalogKindTable, targetTable)
	queryTargetPath := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermSchema, schema, plugin.EngineCatalogTermTable, plugin.EngineCatalogKindTable, queryTargetTable)
	fields := []datatype.FieldInfo{
		{Name: "id", Path: []string{"id"}, Type: datatype.FieldTypeInt, Nullable: true},
		{Name: "phone", Path: []string{"phone"}, Type: datatype.FieldTypeString, Nullable: true},
	}
	store := transferProjectionStore(t)
	installActiveTransferProjection(t, store, model, sourcePath, fields)
	gate := NewGate(store, fakeEngineGetter{engine: &commonmodels.Engine{ID: 11, EngineType: "postgresql"}}.descriptor)
	protector, err := gate.PrepareBoundedTableProtection(ctx, 7, map[string]interface{}{
		"source": map[string]interface{}{"locator": fmt.Sprintf("addp://engine/11/path/%s/%s?type=table", schema, sourceTable)},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	tableExecutor, err := executor.NewTableTransferExecutor("postgresql", "postgresql", "", "")
	if err != nil {
		t.Fatal(err)
	}
	tableExecutor.SourceProtector = protector
	metrics, err := tableExecutor.Execute(ctx, executor.TableTransferPlan{
		Source:    executor.TableSourcePlan{Kind: executor.TableEndpointNative, ConnInfo: connInfo, Path: sourcePath, TableInfo: &datatype.TableInfo{Name: sourceTable, Fields: fields}},
		Target:    executor.TableTargetPlan{Kind: executor.TableEndpointNative, ConnInfo: connInfo, Path: targetPath},
		BatchSize: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if metrics.RecordsRead != 1 || metrics.RecordsWritten != 1 {
		t.Fatalf("metrics = %#v", metrics)
	}
	if metrics.FieldLineage == nil || metrics.FieldLineage.Sources["source"].Validate() != nil || metrics.FieldLineage.Target.Validate() != nil || len(metrics.FieldLineage.Mappings) != 2 {
		t.Fatalf("native export field evidence = %#v", metrics.FieldLineage)
	}
	for _, mapping := range metrics.FieldLineage.Mappings {
		want := "direct"
		if mapping.SourceField == "phone" {
			want = "derived"
		}
		if mapping.SourceField != mapping.TargetField || mapping.Transformation != want {
			t.Fatalf("masked mapping = %#v, want %s", mapping, want)
		}
	}
	var phone string
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT phone FROM "%s"."%s" WHERE id = 1`, schema, targetTable)).Scan(&phone); err != nil {
		t.Fatal(err)
	}
	if phone != "136****4499" {
		t.Fatalf("exported phone = %q, want masked value", phone)
	}

	t.Run("role_choices_native_and_query_real_output", func(t *testing.T) {
		roleStore := transferProjectionStore(t)
		choice := installTransferRoleOption(t, roleStore, model, sourcePath, fields)
		roleGate := NewGate(roleStore, fakeEngineGetter{engine: &commonmodels.Engine{ID: 11, EngineType: "postgresql"}}.descriptor)
		roles := []string{"custom.analytics"}
		resolve := func(context.Context) (*dataprotection.QueryRoleSelection, error) {
			return &dataprotection.QueryRoleSelection{Choices: []dataprotection.FieldRoleChoice{choice}, RoleKeys: roles, ExpiresAt: time.Now().Add(time.Minute)}, nil
		}
		config := map[string]any{"runtime": map[string]any{"boundary": "bounded"}, "load": map[string]any{"mode": "snapshot"}, "source": map[string]any{"locator": fmt.Sprintf("addp://engine/11/path/%s/%s?type=table", schema, sourceTable), "data_type": "table", "representation": "native", "protection_choices": []dataprotection.FieldRoleChoice{choice}}}
		for _, querySource := range []bool{false, true} {
			roles = []string{"custom.analytics"}
			protected, err := roleGate.PrepareBoundedTableProtection(ctx, 7, config, resolve)
			if err != nil {
				t.Fatal(err)
			}
			engine, err := executor.NewTableTransferExecutor("postgresql", "postgresql", "", "")
			if err != nil {
				t.Fatal(err)
			}
			engine.SourceProtector = protected
			name := fmt.Sprintf("persons_role_%t", querySource)
			target := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermSchema, schema, plugin.EngineCatalogTermTable, plugin.EngineCatalogKindTable, name)
			source := executor.TableSourcePlan{Kind: executor.TableEndpointNative, ConnInfo: connInfo, Path: sourcePath, TableInfo: &datatype.TableInfo{Name: sourceTable, Fields: fields}}
			if querySource {
				source.Kind = executor.TableEndpointQuery
				source.Query = fmt.Sprintf(`SELECT id,phone FROM "%s"."%s"`, schema, sourceTable)
				source.RuntimeQuery = &plugin.QueryRequest{EngineID: 11, Language: "sql", Query: source.Query, TargetPath: &sourcePath, Options: plugin.QueryOptions{ReadOnly: true}}
				source.ExpectedQueryReadSet, _ = plugin.NewQueryReadSet(sourcePath)
			}
			plan := executor.TableTransferPlan{Source: source, Target: executor.TableTargetPlan{Kind: executor.TableEndpointNative, ConnInfo: connInfo, Path: target}, BatchSize: 1}
			metrics, err := engine.Execute(ctx, plan)
			if err != nil {
				t.Fatal(err)
			}
			if metrics.RecordsWritten != 1 {
				t.Fatal("selected output not written")
			}
			var actual string
			if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT phone FROM "%s"."%s"`, schema, name)).Scan(&actual); err != nil || len(actual) != 64 {
				t.Fatal("real target did not receive selected SM3", err)
			}
			roles = []string{"custom.reader"}
			plan.Target.DeleteBeforeWrite = true
			metrics, err = engine.Execute(ctx, plan)
			if err == nil || metrics != nil && (metrics.RecordsRead != 0 || metrics.RecordsWritten != 0) {
				t.Fatal("revoked role crossed target boundary", err)
			}
			var retained string
			if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT phone FROM "%s"."%s"`, schema, name)).Scan(&retained); err != nil || retained != actual {
				t.Fatal("rejected role altered target", err)
			}
		}
	})

	t.Run("native_live_schema_drift_before_target_mutation", func(t *testing.T) {
		var originalOID uint32
		if err := db.QueryRowContext(ctx, `SELECT $1::regclass::oid`, schema+"."+targetTable).Scan(&originalOID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE "%s"."%s" ADD COLUMN unrelated_probe integer`, schema, sourceTable)); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := db.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE "%s"."%s" DROP COLUMN unrelated_probe`, schema, sourceTable)); err != nil {
				t.Error(err)
			}
		})
		// The source plan still contains the original Meta snapshot. The live
		// field is deliberately unrelated to the protected phone component.
		metrics, err := tableExecutor.Execute(ctx, executor.TableTransferPlan{
			Source: executor.TableSourcePlan{Kind: executor.TableEndpointNative, ConnInfo: connInfo, Path: sourcePath, TableInfo: &datatype.TableInfo{Name: sourceTable, Fields: fields}},
			Target: executor.TableTargetPlan{Kind: executor.TableEndpointNative, ConnInfo: connInfo, Path: targetPath, DeleteBeforeWrite: true}, BatchSize: 10,
		})
		if !errors.Is(err, executor.ErrProtectionPreparation) || !errors.Is(err, dataprotection.ErrSchemaMismatch) {
			t.Errorf("live native schema mismatch must fail during protection preparation: %v", err)
		}
		if errors.Is(err, executor.ErrTargetMayHaveChanged) || metrics != nil && (metrics.RecordsRead != 0 || metrics.RecordsWritten != 0) {
			t.Error("schema mismatch crossed the read/write or target mutation boundary")
		}
		var actualOID uint32
		if err := db.QueryRowContext(ctx, `SELECT $1::regclass::oid`, schema+"."+targetTable).Scan(&actualOID); err != nil {
			t.Fatal(err)
		}
		if actualOID != originalOID {
			t.Error("schema mismatch replaced the existing target table")
		}
		var count int
		if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM "%s"."%s"`, schema, targetTable)).Scan(&count); err != nil || count != 1 {
			t.Fatalf("schema mismatch changed target row count: count=%d error=%v", count, err)
		}
		var actual string
		if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT phone FROM "%s"."%s" WHERE id=1`, schema, targetTable)).Scan(&actual); err != nil || actual != "136****4499" {
			t.Fatalf("schema mismatch changed the existing protected value: error=%v", err)
		}
	})

	queryExecutor, err := executor.NewTableTransferExecutor("postgresql", "postgresql", "", "")
	if err != nil {
		t.Fatal(err)
	}
	queryExecutor.SourceProtector = protector
	queryFields := []datatype.FieldInfo{
		{Name: "id", Path: []string{"id"}, Type: datatype.FieldTypeInt, Nullable: true},
		{Name: "contact_phone", Path: []string{"contact_phone"}, Type: datatype.FieldTypeString, Nullable: true},
	}
	query := fmt.Sprintf(`SELECT id, phone AS contact_phone FROM "%s"."%s" WHERE id = :p1`, schema, sourceTable)
	expectedQueryReadSet, err := plugin.NewQueryReadSet(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	metrics, err = queryExecutor.Execute(ctx, executor.TableTransferPlan{
		Source: executor.TableSourcePlan{
			Kind: executor.TableEndpointQuery, ConnInfo: connInfo,
			RuntimeQuery:         &plugin.QueryRequest{EngineID: 11, Language: "sql", Query: query, TargetPath: &sourcePath, Options: plugin.QueryOptions{ReadOnly: true, Parameters: map[string]interface{}{"p1": 1}}},
			ExpectedQueryReadSet: expectedQueryReadSet,
			QueryInputs:          []executor.TableQueryInput{{Port: "source", Path: sourcePath}},
			TableInfo:            &datatype.TableInfo{Name: sourceTable, Fields: queryFields},
		},
		Target:    executor.TableTargetPlan{Kind: executor.TableEndpointNative, ConnInfo: connInfo, Path: queryTargetPath},
		BatchSize: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if metrics.RecordsRead != 1 || metrics.RecordsWritten != 1 {
		t.Fatalf("query metrics = %#v", metrics)
	}
	if metrics.FieldLineage == nil || len(metrics.FieldLineage.Mappings) != 2 {
		t.Fatalf("query field lineage missing: %+v", metrics.FieldLineage)
	}
	for _, mapping := range metrics.FieldLineage.Mappings {
		if mapping.TargetField == "contact_phone" && (mapping.SourceField != "phone" || mapping.Transformation != "derived") {
			t.Fatalf("masked query alias lost provenance: %+v", mapping)
		}
	}
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT contact_phone FROM "%s"."%s" WHERE id = 1`, schema, queryTargetTable)).Scan(&phone); err != nil {
		t.Fatal(err)
	}
	if phone != "136****4499" {
		t.Fatalf("query-exported phone = %q, want masked value", phone)
	}
	for _, probe := range []struct {
		name      string
		statement string
		refusal   error
	}{
		{"literal_control", fmt.Sprintf(`SELECT id, phone AS contact_phone FROM "%s"."%s" WHERE id='1'`, schema, sourceTable), nil},
		{"distinct_refusal", fmt.Sprintf(`SELECT DISTINCT id, phone AS contact_phone FROM "%s"."%s"`, schema, sourceTable), plugin.ErrQueryOutputTransformUnsupported},
		{"computed_refusal", fmt.Sprintf(`SELECT id, CAST(phone AS text) AS contact_phone FROM "%s"."%s"`, schema, sourceTable), dataprotection.ErrComputedOutputUnsupported},
	} {
		t.Run(probe.name, func(t *testing.T) {
			// These use the real PostgreSQL provider and the same protected source.
			// A string literal is supported here; DISTINCT has direct lineage but
			// its output rewrite is deliberately unsupported. CAST has derived lineage.
			request := plugin.QueryRequest{EngineID: 11, Language: "sql", Query: probe.statement, TargetPath: &sourcePath, Options: plugin.QueryOptions{ReadOnly: true}}
			prepared, err := pg.PrepareQuery(ctx, connInfo, request)
			if err != nil {
				t.Fatal(err)
			}
			lineage, err := prepared.OutputLineage(ctx)
			if err != nil || len(lineage.Sources) != 1 {
				t.Fatalf("query lineage = %#v, err=%v", lineage, err)
			}
			found := false
			for _, binding := range lineage.Sources[0].Bindings {
				if len(binding.OutputPath) == 1 && binding.OutputPath[0] == "contact_phone" {
					want := plugin.QueryOutputTransformationDirect
					if probe.name == "computed_refusal" {
						want = plugin.QueryOutputTransformationDerived
					}
					if binding.Transformation != want {
						t.Fatalf("output lineage = %#v, want %s", binding, want)
					}
					found = true
				}
			}
			if !found {
				t.Fatal("protected output has no actual field binding")
			}
			path := queryTargetPath
			if probe.refusal == nil {
				path = plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermSchema, schema, plugin.EngineCatalogTermTable, plugin.EngineCatalogKindTable, "literal_control_export")
			}
			export, err := executor.NewTableTransferExecutor("postgresql", "postgresql", "", "")
			if err != nil {
				t.Fatal(err)
			}
			export.SourceProtector = protector
			metrics, err := export.Execute(ctx, executor.TableTransferPlan{
				Source: executor.TableSourcePlan{Kind: executor.TableEndpointQuery, ConnInfo: connInfo, RuntimeQuery: &request,
					ExpectedQueryReadSet: expectedQueryReadSet, QueryInputs: []executor.TableQueryInput{{Port: "source", Path: sourcePath}},
					TableInfo: &datatype.TableInfo{Name: sourceTable, Fields: queryFields}},
				Target: executor.TableTargetPlan{Kind: executor.TableEndpointNative, ConnInfo: connInfo, Path: path, DeleteBeforeWrite: true}, BatchSize: 10,
			})
			if probe.refusal == nil {
				if err != nil || metrics == nil || metrics.RecordsRead != 1 || metrics.RecordsWritten != 1 {
					t.Fatalf("PostgreSQL literal predicate must transfer protected output: metrics=%+v, err=%v", metrics, err)
				}
			} else if !errors.Is(err, probe.refusal) || !errors.Is(err, executor.ErrProtectionPreparation) ||
				errors.Is(err, executor.ErrTargetMayHaveChanged) || metrics != nil && (metrics.RecordsRead != 0 || metrics.RecordsWritten != 0) {
				t.Fatalf("query must refuse before target mutation: metrics=%+v, err=%v", metrics, err)
			}
			target := queryTargetTable
			if probe.refusal == nil {
				target = "literal_control_export"
			}
			var count int
			if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM "%s"."%s"`, schema, target)).Scan(&count); err != nil || count != 1 {
				t.Fatalf("physical target row count=%d, err=%v", count, err)
			}
			if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT contact_phone FROM "%s"."%s" WHERE id=1`, schema, target)).Scan(&phone); err != nil || phone != "136****4499" {
				t.Fatalf("target must contain only the protected control value: err=%v", err)
			}
		})
	}

	for _, querySource := range []bool{false, true} {
		t.Run(fmt.Sprintf("expired_query_%t", querySource), func(t *testing.T) {
			expiredStore := transferProjectionStore(t)
			installActiveTransferProjectionForComponentAt(t, expiredStore, model, sourcePath, fields, dataprotection.Component{
				Key: "phone", Path: []dataprotection.PathSegment{{Name: "phone", Container: "scalar"}}, ValueType: string(datatype.FieldTypeString),
			}, time.Now().UTC().Add(-2*time.Hour))
			bound, err := NewGate(expiredStore, fakeEngineGetter{engine: &commonmodels.Engine{ID: 11, EngineType: "postgresql"}}.descriptor).PrepareBoundedTableProtection(ctx, 7, map[string]interface{}{
				"source": map[string]interface{}{"locator": fmt.Sprintf("addp://engine/11/path/%s/%s?type=table", schema, sourceTable)},
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			export, err := executor.NewTableTransferExecutor("postgresql", "postgresql", "", "")
			if err != nil {
				t.Fatal(err)
			}
			export.SourceProtector = bound
			plan := executor.TableTransferPlan{
				Source: executor.TableSourcePlan{Kind: executor.TableEndpointNative, ConnInfo: connInfo, Path: sourcePath, TableInfo: &datatype.TableInfo{Name: sourceTable, Fields: fields}},
				Target: executor.TableTargetPlan{Kind: executor.TableEndpointNative, ConnInfo: connInfo, Path: targetPath}, BatchSize: 10,
			}
			if querySource {
				plan.Source = executor.TableSourcePlan{
					Kind: executor.TableEndpointQuery, ConnInfo: connInfo,
					RuntimeQuery:         &plugin.QueryRequest{EngineID: 11, Language: "sql", Query: query, TargetPath: &sourcePath, Options: plugin.QueryOptions{ReadOnly: true, Parameters: map[string]interface{}{"p1": 1}}},
					ExpectedQueryReadSet: expectedQueryReadSet, QueryInputs: []executor.TableQueryInput{{Port: "source", Path: sourcePath}},
					TableInfo: &datatype.TableInfo{Name: sourceTable, Fields: queryFields},
				}
				plan.Target.Path = queryTargetPath
			}
			metrics, err := export.Execute(ctx, plan)
			if !errors.Is(err, dataprotection.ErrDenied) || !errors.Is(err, executor.ErrProtectionPreparation) || metrics != nil && metrics.RecordsWritten != 0 {
				t.Fatalf("expired export must deny with zero writes: error=%v, metrics=%+v", err, metrics)
			}
			target := targetTable
			column := "phone"
			if querySource {
				target, column = queryTargetTable, "contact_phone"
			}
			var count int
			if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM "%s"."%s"`, schema, target)).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("expired export changed physical target row count: %d", count)
			}
			if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT "%s" FROM "%s"."%s" WHERE id = 1`, column, schema, target)).Scan(&phone); err != nil || phone != "136****4499" {
				t.Fatalf("expired export changed previously protected target data: error=%v", err)
			}
		})
	}
}
