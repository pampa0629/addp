package postgresql

import (
	"context"
	"crypto/rand"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestIntegrationPostgresCatalogPreciseReadOnly(t *testing.T) {
	db, pg, info := openPostgresPrepareIntegration(t, false)
	defer db.Close()
	ctx := t.Context()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	role, schema := "catalog_reader_"+suffix, "catalog_readonly_"+suffix
	password := rand.Text()
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE ROLE "%s" LOGIN PASSWORD '%s'`, role, password)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, statement := range []string{
			fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schema),
			fmt.Sprintf(`DROP OWNED BY "%s"`, role),
			fmt.Sprintf(`DROP ROLE "%s"`, role),
		} {
			if _, err := db.ExecContext(ctx, statement); err != nil {
				t.Errorf("cleanup catalog reader fixture: %v", err)
			}
		}
		var remains bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname=$1)
			OR EXISTS (SELECT 1 FROM pg_namespace WHERE nspname=$2)`, role, schema).Scan(&remains); err != nil || remains {
			t.Errorf("catalog reader cleanup not proven: remains=%v error=%v", remains, err)
		}
	}()
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE SCHEMA "%s";
		CREATE TABLE "%s".source (id bigint PRIMARY KEY, value text);
		CREATE TABLE "%s".other (id bigint, value text);
		GRANT USAGE ON SCHEMA "%s" TO "%s";
		GRANT SELECT ON "%s".source TO "%s"`, schema, schema, schema, schema, role, schema, role)); err != nil {
		t.Fatal(err)
	}
	readerInfo := make(plugin.ConnectionInfo, len(info))
	for key, value := range info {
		readerInfo[key] = value
	}
	readerInfo["user"], readerInfo["password"] = role, password
	dsn, err := pg.BuildDSN(readerInfo)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	readerDB, err := reader.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer readerDB.Close()
	var canRead, canWrite bool
	if err := reader.Raw(`SELECT has_table_privilege(?, 'SELECT'), has_table_privilege(?, 'INSERT,UPDATE,DELETE')`, schema+".source", schema+".source").Row().Scan(&canRead, &canWrite); err != nil || !canRead || canWrite {
		t.Fatalf("reader privileges: read=%v write=%v error=%v", canRead, canWrite, err)
	}
	trace := &preciseCatalogQueryTrace{Interface: logger.Default.LogMode(logger.Silent)}
	exactReader := reader.Session(&gorm.Session{Logger: trace})
	summary, err := pg.getTable(ctx, exactReader, schema, "source")
	if err != nil || summary == nil || summary.Name != "source" || len(summary.Fields) != 0 || len(trace.queries) != 1 || !strings.Contains(trace.queries[0], "t.table_name = 'source'") {
		t.Fatalf("precise source summary=%#v error=%v queries=%v", summary, err, trace.queries)
	}
	path := plugin.TabularItemPath(1, plugin.EngineCatalogTermSchema, schema, "source")
	entry, err := pg.ResolvePath(ctx, readerInfo, path)
	if err != nil || entry == nil || entry.Table == nil || len(entry.Table.Fields) != 0 || len(entry.Table.PrimaryKey) != 0 {
		t.Fatalf("resolve must remain lightweight: entry=%#v error=%v", entry, err)
	}
	fields, err := pg.listColumns(ctx, reader, schema, "source")
	if err != nil || len(fields) != 2 || fields[0].Name != "id" || !fields[0].PrimaryKey || fields[0].Nullable || fields[1].PrimaryKey {
		t.Fatalf("SELECT-only catalog fields lost primary key: fields=%#v error=%v", fields, err)
	}
	facts, err := pg.DescribeEngineCatalogFacts(ctx, readerInfo, path, plugin.EngineCatalogFactsOptions{})
	if err != nil || facts == nil || facts.Table == nil || !reflect.DeepEqual(facts.Table.PrimaryKey, []string{"id"}) {
		t.Fatalf("SELECT-only table facts lost stable key: facts=%#v error=%v", facts, err)
	}
	fields, err = pg.listColumns(ctx, reader, schema, "other")
	if err != nil || len(fields) != 0 {
		t.Fatalf("ungranted table columns must remain hidden: fields=%#v error=%v", fields, err)
	}
	for _, table := range []string{"other", "missing"} {
		deniedPath := plugin.TabularItemPath(1, plugin.EngineCatalogTermSchema, schema, table)
		if _, err := pg.ResolvePath(ctx, readerInfo, deniedPath); !plugin.IsEngineCatalogErrorKind(err, plugin.EngineCatalogErrorNotFound) {
			t.Fatalf("unreadable resolve %s: %v", table, err)
		}
		if _, err := pg.DescribeEngineCatalogFacts(ctx, readerInfo, deniedPath, plugin.EngineCatalogFactsOptions{}); !plugin.IsEngineCatalogErrorKind(err, plugin.EngineCatalogErrorNotFound) {
			t.Fatalf("unreadable facts %s: %v", table, err)
		}
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`REVOKE USAGE ON SCHEMA "%s" FROM "%s"`, schema, role)); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.ResolvePath(ctx, readerInfo, path); !plugin.IsEngineCatalogErrorKind(err, plugin.EngineCatalogErrorNotFound) {
		t.Fatalf("schema without USAGE must be hidden: %v", err)
	}
}

type preciseCatalogQueryTrace struct {
	logger.Interface
	queries []string
}

func (l *preciseCatalogQueryTrace) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	query, _ := fc()
	l.queries = append(l.queries, query)
}
