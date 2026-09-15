package service

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/mysql"
)

// The root Model MySQL gate exclusively creates and removes this test database.
func TestIntegrationMySQLMetricSemantics(t *testing.T) {
	if os.Getenv("ADDP_MYSQL_INTEGRATION") != "1" {
		t.Skip("requires the Model MySQL gate")
	}
	if os.Getenv("ADDP_TEST_MYSQL_PASSWORD") == "" {
		t.Fatal("ADDP_TEST_MYSQL_PASSWORD is required")
	}
	setting := func(name, fallback string) string {
		if v := os.Getenv(name); v != "" {
			return v
		}
		return fallback
	}
	provider := &mysql.MySQLPlugin{}
	conn := plugin.ConnectionInfo{"host": setting("ADDP_TEST_MYSQL_HOST", "127.0.0.1"), "port": setting("ADDP_TEST_MYSQL_PORT", "3306"), "user": setting("ADDP_TEST_MYSQL_USER", "root"), "password": os.Getenv("ADDP_TEST_MYSQL_PASSWORD"), "database": "mysql"}
	dsn, err := provider.BuildDSN(conn)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	dialect := metricTestDialect(t, "mysql")
	database := fmt.Sprintf("addp_model_mysql_it_%d", time.Now().UnixNano())
	if _, err := db.Exec("CREATE DATABASE " + dialect.QuoteIdentifier(database) + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec("DROP DATABASE " + dialect.QuoteIdentifier(database)); err != nil {
			t.Errorf("clean test database: %v", err)
		}
	})
	conn["database"] = database
	qualify := func(query string) string {
		return strings.ReplaceAll(query, "SCHEMA", dialect.QuoteIdentifier(database))
	}
	for _, statement := range []string{
		`CREATE TABLE SCHEMA.metric_golden_people(person_id varchar(200))`,
		`CREATE TABLE SCHEMA.metric_golden_events(event_id varchar(200),event_date date)`,
		`CREATE TABLE SCHEMA.metric_golden_facts(person_id varchar(200),event_id varchar(200),leader bool)`,
		`INSERT INTO SCHEMA.metric_golden_people VALUES ('A'),('B'),('C'),('a'),('A '),('quoted''person')`,
		`INSERT INTO SCHEMA.metric_golden_events VALUES ('e1','2026-01-01'),('e2','2026-01-02'),('e3','2026-01-03'),('e4','2026-02-01'),('e5','2026-03-01'),('e6','2026-01-06'),('old','2025-12-31'),('new','2027-01-01')`,
		`INSERT INTO SCHEMA.metric_golden_facts VALUES ('A','e1',true),('A','e1',true),('A','e2',true),('A','e6',true),('A','e4',true),('A','e5',false),('B','e1',false),('B','e3',true),('B','e4',false),('A','old',true),('A','new',true)`,
	} {
		if _, err := db.Exec(qualify(statement)); err != nil {
			t.Fatal(err)
		}
	}
	contract, bindings := metricGoldenContract()
	bindings.Fact.Schema = database
	for id, rel := range bindings.Relations {
		rel.Target.Schema = database
		bindings.Relations[id] = rel
	}
	query := func(t *testing.T, operation, subject, comparison, grain, directions, start, end string) (*plugin.QueryResult, error) {
		t.Helper()
		c := contract
		c.Operation = operation
		parameters := map[string]interface{}{"subject_id": subject, "grain": grain, "start_date": start, "end_date": end}
		if operation == "directional_overlap" {
			c.Filters = nil
			parameters["comparison_id"] = comparison
			parameters["directions"] = directions
		}
		compiled, err := compileMetricSQL(c, bindings, dialect)
		if err != nil {
			t.Fatal(err)
		}
		order := " ORDER BY bucket"
		if operation == "directional_overlap" {
			order += ",direction"
		}
		prepared, err := provider.PrepareQuery(context.Background(), conn, plugin.QueryRequest{EngineID: 2, Language: "sql", Query: compiled + order, Options: plugin.QueryOptions{EngineID: 2, EngineType: "mysql", ReadOnly: true, Parameters: parameters, Limit: 240}})
		if err != nil {
			return nil, err
		}
		readSet, err := prepared.ReadSet(context.Background())
		if err != nil || len(readSet.Paths) != 3 {
			t.Fatalf("read set=%#v err=%v", readSet, err)
		}
		lineage, err := prepared.OutputLineage(context.Background())
		if err != nil || len(lineage.Sources) != 3 {
			t.Fatalf("lineage=%#v err=%v", lineage, err)
		}
		return prepared.Execute(context.Background())
	}
	for _, c := range []struct {
		name, op, a, b, grain, directions, start, end string
		want                                          []float64
	}{
		{"count-months", "count_distinct", "A", "", "month", "", "2026-01-01", "2026-04-01", []float64{3, 1, 0}},
		{"count-total", "count_distinct", "A", "", "total", "", "2026-01-01", "2027-01-01", []float64{4}},
		{"empty", "count_distinct", "C", "", "month", "", "2026-01-01", "2026-03-01", []float64{0, 0}},
		{"missing", "count_distinct", "missing", "", "total", "", "2026-01-01", "2027-01-01", nil},
		{"case-sensitive", "count_distinct", "a", "", "total", "", "2026-01-01", "2027-01-01", []float64{0}},
		{"trailing-space", "count_distinct", "A ", "", "total", "", "2026-01-01", "2027-01-01", []float64{0}},
		{"quoted", "count_distinct", "quoted'person", "", "total", "", "2026-01-01", "2027-01-01", []float64{0}},
		{"partial-month", "count_distinct", "A", "", "month", "", "2026-01-02", "2026-02-01", []float64{2}},
		{"exclusive-end", "count_distinct", "A", "", "total", "", "2026-01-01", "2026-01-02", []float64{1}},
		{"overlap-total", "directional_overlap", "A", "B", "total", "both", "2026-01-01", "2027-01-01", []float64{2.0 / 5, 2.0 / 3}},
		{"overlap-months", "directional_overlap", "A", "B", "month", "both", "2026-01-01", "2026-04-01", []float64{1.0 / 3, 0.5, 1, 1, 0, 0}},
		{"overlap-forward", "directional_overlap", "A", "B", "total", "forward", "2026-01-01", "2027-01-01", []float64{2.0 / 5}},
		{"self", "directional_overlap", "A", "A", "total", "both", "2026-01-01", "2027-01-01", []float64{1, 1}},
		{"zero-denominator", "directional_overlap", "C", "A", "total", "both", "2026-01-01", "2027-01-01", []float64{0, 0}},
		{"missing-comparison", "directional_overlap", "A", "missing", "total", "both", "2026-01-01", "2027-01-01", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			result, err := query(t, c.op, c.a, c.b, c.grain, c.directions, c.start, c.end)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Rows) != len(c.want) {
				t.Fatalf("rows=%v want=%v", result.Rows, c.want)
			}
			for i, row := range result.Rows {
				value, err := strconv.ParseFloat(fmt.Sprint(row["value"]), 64)
				if err != nil || math.Abs(value-c.want[i]) > 1e-12 {
					t.Fatalf("row=%v want=%v err=%v", row, c.want[i], err)
				}
			}
		})
	}
	for _, operation := range []string{"count_distinct", "directional_overlap"} {
		t.Run(operation+"-broken-dimension", func(t *testing.T) {
			_, err := db.Exec(qualify(`INSERT INTO SCHEMA.metric_golden_events VALUES ('e1','2026-01-01')`))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = db.Exec(qualify(`DELETE FROM SCHEMA.metric_golden_events WHERE event_id='e1' LIMIT 1`)) })
			if _, err := query(t, operation, "A", "B", "total", "both", "2026-01-01", "2027-01-01"); err == nil {
				t.Fatal("broken relationship silently returned numbers")
			}
		})
	}
}
