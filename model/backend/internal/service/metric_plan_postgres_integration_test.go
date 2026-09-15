package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/common/query"
	"github.com/addp/model/internal/models"
	"gorm.io/gorm"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func metricGoldenContract() (models.MetricContract, metricPlanBindings) {
	field := func(id int64, name, typ string, pk bool) models.LogicalField {
		return models.LogicalField{ID: id, ColumnName: name, DataType: typ, IsPK: pk, Nullable: false}
	}
	bindings := metricPlanBindings{Fact: metricPlanSource{Schema: "model", Table: "metric_golden_facts", Fields: map[int64]models.LogicalField{1: field(1, "person_id", "string", false), 2: field(2, "event_id", "string", false), 3: field(3, "leader", "bool", false)}}, Relations: map[int64]metricPlanRelation{
		10: {SourceField: 1, TargetField: 4, Target: metricPlanSource{Schema: "model", Table: "metric_golden_people", Fields: map[int64]models.LogicalField{4: field(4, "person_id", "string", true)}}},
		11: {SourceField: 2, TargetField: 5, Target: metricPlanSource{Schema: "model", Table: "metric_golden_events", Fields: map[int64]models.LogicalField{5: field(5, "event_id", "string", true), 6: field(6, "event_date", "date", false)}}},
	}}
	return models.MetricContract{Operation: "count_distinct", Subject: models.MetricFieldReference{FieldID: 1}, SubjectRelationID: 10, Distinct: models.MetricFieldReference{FieldID: 2}, Time: models.MetricFieldReference{FieldID: 6, RelationID: 11}, Filters: []models.MetricBooleanFilter{{Field: models.MetricFieldReference{FieldID: 3}, Value: true}}}, bindings
}

func TestPostgresMetricCompiledMonthlyCountGolden(t *testing.T) {
	tx, _ := beginModelAggregatePostgresTransaction(t)
	for _, sql := range []string{
		`CREATE TABLE model.metric_golden_people(person_id text PRIMARY KEY)`,
		`CREATE TABLE model.metric_golden_events(event_id text PRIMARY KEY,event_date date NOT NULL)`,
		`CREATE TABLE model.metric_golden_facts(person_id text NOT NULL,event_id text NOT NULL,leader bool NOT NULL)`,
		`INSERT INTO model.metric_golden_people VALUES ('A'),('B'),('C'),('quoted''person')`,
		`INSERT INTO model.metric_golden_events VALUES ('e1','2026-01-01'),('e2','2026-01-02'),('e3','2026-01-03'),('e6','2026-01-06'),('e4','2026-02-01'),('e5','2026-03-01'),('previous','2025-12-31'),('following','2027-01-01')`,
		`INSERT INTO model.metric_golden_facts VALUES ('A','e1',true),('A','e2',true),('A','e6',true),('A','e4',true),('A','e5',false),('B','e1',false),('B','e3',true),('B','e4',false),('A','e1',true),('A','previous',true),('A','following',true)`,
	} {
		if err := tx.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	contract, bindings := metricGoldenContract()
	sql, err := compileMetricSQL(contract, bindings)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, subject, start, end, grain string
		want                             []int64
	}{
		{"monthly", "A", "2026-01-01", "2027-01-01", "month", []int64{3, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
		{"annual", "A", "2026-01-01", "2027-01-01", "total", []int64{4}},
		{"empty-person", "C", "2026-01-01", "2026-03-01", "month", []int64{0, 0}},
		{"missing-person", "missing", "2026-01-01", "2027-01-01", "total", nil},
		{"partial-month", "A", "2026-01-02", "2026-02-01", "month", []int64{2}},
		{"upper-exclusive", "A", "2026-01-01", "2026-01-02", "total", []int64{1}},
		{"quoted-parameter", "quoted'person", "2026-01-01", "2026-02-01", "total", []int64{0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			input := models.MetricQueryInput{SubjectID: c.subject, StartDate: c.start, EndDate: c.end, Grain: c.grain}
			if err := validateMetricQueryInput(input); err != nil {
				t.Fatal(err)
			}
			bound, args, err := query.BindSQL(sql+" ORDER BY bucket", map[string]interface{}{"subject_id": c.subject, "start_date": c.start, "end_date": c.end, "grain": c.grain}, query.SQLPlaceholderDollar)
			if err != nil {
				t.Fatal(err)
			}
			var rows []struct {
				SubjectID string
				Bucket    time.Time
				Value     int64
			}
			if err := tx.Raw(bound, args...).Scan(&rows).Error; err != nil {
				t.Fatal(err)
			}
			if len(rows) != len(c.want) {
				t.Fatalf("rows=%v want=%v", rows, c.want)
			}
			for i, row := range rows {
				if row.SubjectID != c.subject || row.Value != c.want[i] {
					t.Fatalf("row=%v want=%d", row, c.want[i])
				}
			}
		})
	}
}

func TestMetricCompilerRejectsAmbiguousDimensionsAndUnknownFields(t *testing.T) {
	for _, name := range []string{"composite-key", "nullable-key", "unknown-date", "type-mismatch", "duplicate-filter"} {
		t.Run(name, func(t *testing.T) {
			contract, bindings := metricGoldenContract()
			switch name {
			case "composite-key":
				bindings.Relations[11].Target.Fields[6] = models.LogicalField{ID: 6, IsPK: true, ColumnName: "event_date", DataType: "date"}
			case "nullable-key":
				field := bindings.Relations[10].Target.Fields[4]
				field.Nullable = true
				bindings.Relations[10].Target.Fields[4] = field
			case "unknown-date":
				contract.Time.FieldID = 999
			case "type-mismatch":
				field := bindings.Fact.Fields[2]
				field.DataType = "int"
				bindings.Fact.Fields[2] = field
			case "duplicate-filter":
				contract.Filters = append(contract.Filters, contract.Filters[0])
			}
			if _, err := compileMetricSQL(contract, bindings); err == nil {
				t.Fatal("invalid contract accepted")
			}
		})
	}
}

func TestPostgresMetricCountRejectsBrokenDimensionData(t *testing.T) {
	tx, _ := beginModelAggregatePostgresTransaction(t)
	for _, sql := range []string{
		`CREATE TABLE model.metric_golden_people(person_id text)`,
		`CREATE TABLE model.metric_golden_events(event_id text,event_date date)`,
		`CREATE TABLE model.metric_golden_facts(person_id text,event_id text,leader bool)`,
		`INSERT INTO model.metric_golden_people VALUES ('A')`,
		`INSERT INTO model.metric_golden_events VALUES ('e1','2026-01-01')`,
		`INSERT INTO model.metric_golden_facts VALUES ('A','e1',true)`,
	} {
		if err := tx.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	contract, bindings := metricGoldenContract()
	sql, err := compileMetricSQL(contract, bindings)
	if err != nil {
		t.Fatal(err)
	}
	bound, args, err := query.BindSQL(sql, map[string]interface{}{"subject_id": "A", "start_date": "2026-01-01", "end_date": "2027-01-01", "grain": "total"}, query.SQLPlaceholderDollar)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutation := range map[string]string{"missing": "DELETE FROM model.metric_golden_events", "null-date": "UPDATE model.metric_golden_events SET event_date=NULL", "null-identity": "UPDATE model.metric_golden_facts SET event_id=NULL", "duplicate-person": "INSERT INTO model.metric_golden_people VALUES ('A')", "duplicate": "INSERT INTO model.metric_golden_events VALUES ('e1','2026-02-01')"} {
		t.Run(name, func(t *testing.T) {
			tx.Transaction(func(inner *gorm.DB) error {
				if err := inner.Exec(mutation).Error; err != nil {
					t.Fatal(err)
				}
				var rows []struct{ Value int64 }
				err := inner.Raw(bound, args...).Scan(&rows).Error
				if err == nil {
					t.Error("broken dimension silently produced a number")
					return errors.New("rollback fixture")
				}
				return err
			})
		})
	}
}

func TestPostgresMetricPlanUsesSharedPreparedQuery(t *testing.T) {
	dsn := os.Getenv("ADDP_TEST_MODEL_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADDP_TEST_MODEL_POSTGRES_DSN is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	schema := fmt.Sprintf("metric_plan_%d", time.Now().UnixNano())
	if _, err := db.Exec("CREATE SCHEMA " + quoteMetricIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP SCHEMA " + quoteMetricIdentifier(schema) + " CASCADE")
	for _, statement := range []string{
		`CREATE TABLE SCHEMA.metric_golden_people(person_id text PRIMARY KEY)`,
		`CREATE TABLE SCHEMA.metric_golden_events(event_id text PRIMARY KEY,event_date date NOT NULL)`,
		`CREATE TABLE SCHEMA.metric_golden_facts(person_id text,event_id text,leader bool)`,
		`INSERT INTO SCHEMA.metric_golden_people VALUES ('A')`,
		`INSERT INTO SCHEMA.metric_golden_events VALUES ('e1','2026-01-01')`,
		`INSERT INTO SCHEMA.metric_golden_facts VALUES ('A','e1',true)`,
	} {
		if _, err := db.Exec(strings.ReplaceAll(statement, "SCHEMA", quoteMetricIdentifier(schema))); err != nil {
			t.Fatal(err)
		}
	}
	contract, bindings := metricGoldenContract()
	bindings.Fact.Schema = schema
	for id, rel := range bindings.Relations {
		rel.Target.Schema = schema
		bindings.Relations[id] = rel
	}
	for _, operation := range []string{"count_distinct", "directional_overlap"} {
		t.Run(operation, func(t *testing.T) {
			contract.Operation = operation
			input := map[string]interface{}{"subject_id": "A", "start_date": "2026-01-01", "end_date": "2026-03-01", "grain": "month"}
			wantRows := 2
			if operation == "directional_overlap" {
				contract.Filters = nil
				input["comparison_id"] = "A"
				input["directions"] = "both"
				wantRows = 4
			}
			compiled, err := compileMetricSQL(contract, bindings)
			if err != nil {
				t.Fatal(err)
			}
			bound, args, err := query.BindSQL(compiled, input, query.SQLPlaceholderDollar)
			if err != nil {
				t.Fatal(err)
			}
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
			provider := &postgresql.PostgreSQLPlugin{}
			prepared, err := provider.PrepareQuery(context.Background(), plugin.ConnectionInfo{"host": parsed.Hostname(), "port": port, "database": strings.TrimPrefix(parsed.Path, "/"), "user": parsed.User.Username(), "password": password, "sslmode": "disable"}, plugin.QueryRequest{EngineID: 2, Language: "sql", Query: bound, Options: plugin.QueryOptions{EngineID: 2, EngineType: "postgresql", ReadOnly: true, Limit: 120, Args: args}})
			if err != nil {
				t.Fatal(err)
			}
			readSet, err := prepared.ReadSet(context.Background())
			if err != nil || len(readSet.Paths) != 3 {
				t.Fatalf("metric read set=%#v error=%v", readSet, err)
			}
			lineage, err := prepared.OutputLineage(context.Background())
			if err != nil || len(lineage.Sources) != 3 {
				t.Fatalf("metric lineage=%#v error=%v", lineage, err)
			}
			result, err := prepared.Execute(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Rows) != wantRows {
				t.Fatalf("metric result=%#v", result)
			}
		})
	}
}

func TestPostgresDirectionalOverlapGolden(t *testing.T) {
	tx, _ := beginModelAggregatePostgresTransaction(t)
	for _, statement := range []string{
		`CREATE TABLE model.metric_golden_people(person_id text PRIMARY KEY)`,
		`CREATE TABLE model.metric_golden_events(event_id text PRIMARY KEY,event_date date NOT NULL)`,
		`CREATE TABLE model.metric_golden_facts(person_id text,event_id text,leader bool)`,
		`INSERT INTO model.metric_golden_people VALUES ('A'),('B'),('C'),('quoted''person')`,
		`INSERT INTO model.metric_golden_events VALUES ('e1','2026-01-01'),('e2','2026-01-02'),('e3','2026-01-03'),('e6','2026-01-06'),('e4','2026-02-01'),('e5','2026-03-01'),('old','2025-12-31'),('new','2027-01-01')`,
		`INSERT INTO model.metric_golden_facts VALUES ('A','e1',true),('A','e1',true),('A','e2',true),('A','e6',true),('A','e4',true),('A','e5',false),('B','e1',false),('B','e3',true),('B','e4',false),('A','old',true),('A','new',true)`,
	} {
		if err := tx.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	contract, bindings := metricGoldenContract()
	contract.Operation = "directional_overlap"
	contract.Filters = nil
	compiled, err := compileMetricSQL(contract, bindings)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, a, b, grain, directions, start, end string
		want                                      []float64
	}{
		{"forward-full-denominator", "A", "B", "total", "forward", "2026-01-01", "2027-01-01", []float64{2.0 / 5}},
		{"two-directions", "A", "B", "total", "both", "2026-01-01", "2027-01-01", []float64{2.0 / 5, 2.0 / 3}},
		{"monthly-not-average", "A", "B", "month", "both", "2026-01-01", "2026-04-01", []float64{1.0 / 3, 0.5, 1, 1, 0, 0}},
		{"same-person", "A", "A", "total", "both", "2026-01-01", "2027-01-01", []float64{1, 1}},
		{"same-empty", "C", "C", "total", "both", "2026-01-01", "2027-01-01", []float64{0, 0}},
		{"zero-denominator", "C", "A", "total", "both", "2026-01-01", "2027-01-01", []float64{0, 0}},
		{"missing-subject", "missing", "A", "total", "both", "2026-01-01", "2027-01-01", nil},
		{"missing-comparison", "A", "missing", "total", "both", "2026-01-01", "2027-01-01", nil},
		{"partial-range", "A", "B", "total", "both", "2026-01-02", "2026-02-01", []float64{0, 0}},
		{"quoted-identity", "quoted'person", "A", "total", "forward", "2026-01-01", "2027-01-01", []float64{0}},
	} {
		t.Run(c.name, func(t *testing.T) {
			input := models.MetricQueryInput{SubjectID: c.a, ComparisonID: c.b, Grain: c.grain, Directions: c.directions, StartDate: c.start, EndDate: c.end}
			if err := validateMetricOperationInput(contract.Operation, input); err != nil {
				t.Fatal(err)
			}
			bound, args, err := query.BindSQL(compiled+" ORDER BY bucket,direction", map[string]interface{}{"subject_id": c.a, "comparison_id": c.b, "start_date": c.start, "end_date": c.end, "grain": c.grain, "directions": c.directions}, query.SQLPlaceholderDollar)
			if err != nil {
				t.Fatal(err)
			}
			var rows []struct {
				Direction, SubjectID, ComparisonID         string
				Value                                      float64
				SubjectCount, ComparisonCount, SharedCount int64
			}
			if err := tx.Raw(bound, args...).Scan(&rows).Error; err != nil {
				t.Fatal(err)
			}
			if len(rows) != len(c.want) {
				t.Fatalf("rows=%v want=%v", rows, c.want)
			}
			for i, r := range rows {
				if math.Abs(r.Value-c.want[i]) > 1e-12 {
					t.Fatalf("row=%+v want=%v", r, c.want)
				}
				if r.SharedCount > r.SubjectCount || r.SharedCount > r.ComparisonCount {
					t.Fatalf("bad counts %+v", r)
				}
				if r.Direction == "forward" && (r.SubjectID != c.a || r.ComparisonID != c.b) || r.Direction == "reverse" && (r.SubjectID != c.b || r.ComparisonID != c.a) {
					t.Fatalf("swapped roles %+v", r)
				}
			}
		})
	}
}

func TestDirectionalOverlapRejectsUndeclaredRoleFiltersAndParameters(t *testing.T) {
	c, b := metricGoldenContract()
	c.Operation = "directional_overlap"
	if _, err := compileMetricSQL(c, b); err == nil {
		t.Fatal("leader-specific filter accepted for overlap")
	}
	input := models.MetricQueryInput{SubjectID: "A", StartDate: "2026-01-01", EndDate: "2027-01-01", Grain: "total"}
	if validateMetricOperationInput("directional_overlap", input) == nil {
		t.Fatal("missing comparison accepted")
	}
	input.ComparisonID = "B"
	input.Directions = "both"
	if validateMetricOperationInput("count_distinct", input) == nil {
		t.Fatal("extra roles accepted by count")
	}
	input.Directions = "arbitrary"
	if validateMetricOperationInput("directional_overlap", input) == nil {
		t.Fatal("unknown direction accepted")
	}
}
