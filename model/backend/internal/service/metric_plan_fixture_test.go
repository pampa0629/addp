package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query"
	"github.com/addp/common/query/plan"
	"github.com/addp/model/internal/models"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

type metricGoldenProvider interface {
	plugin.EnginePlugin
	plugin.AnalyticalCompilerProvider
	plugin.EngineCatalogFactsProvider
	plugin.EngineCatalogModelProvider
}

func runMetricGolden(t *testing.T, provider metricGoldenProvider, conn plugin.ConnectionInfo, db *sql.DB, database string, dialect query.Dialect) {
	qualify := func(query string) string {
		return strings.ReplaceAll(query, "SCHEMA", dialect.QuoteIdentifier(database))
	}
	for _, statement := range []string{
		`CREATE TABLE SCHEMA.metric_golden_people(person_id varchar(200),nickname varchar(200))`,
		`CREATE TABLE SCHEMA.metric_golden_events(event_id varchar(200),event_date date)`,
		`CREATE TABLE SCHEMA.metric_golden_facts(person_id varchar(200),event_id varchar(200),leader bool)`,
		`INSERT INTO SCHEMA.metric_golden_people VALUES ('A','Alpha'),('B','Beta'),('C',NULL),('a','Lowercase'),('A ','Spaced'),('quoted''person','Quoted')`,
		`INSERT INTO SCHEMA.metric_golden_events VALUES ('e1','2026-01-01'),('e2','2026-01-02'),('e3','2026-01-03'),('e4','2026-02-01'),('e5','2026-03-01'),('e6','2026-01-06'),('old','2025-12-31'),('new','2027-01-01')`,
		`INSERT INTO SCHEMA.metric_golden_facts VALUES ('A','e1',true),('A','e1',true),('A','e2',true),('A','e6',true),('A','e4',true),('A','e5',false),('B','e1',false),('B','e3',true),('B','e4',false),('A','old',true),('A','new',true)`,
	} {
		if _, err := db.Exec(qualify(statement)); err != nil {
			t.Fatal(err)
		}
	}
	contract, bindings := metricGoldenContract()
	contract.SubjectLabel = &models.MetricFieldReference{FieldID: 7, RelationID: 10}
	labels := map[string]interface{}{"A": "Alpha", "B": "Beta", "C": nil, "a": "Lowercase", "A ": "Spaced", "quoted'person": "Quoted"}
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
		request, err := metricGoldenRequest(t, provider, conn, database, c, bindings, parameters)
		if err != nil {
			return nil, err
		}
		prepared, err := provider.PrepareQuery(context.Background(), conn, request)
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
		{"same-empty", "directional_overlap", "C", "C", "total", "both", "2026-01-01", "2027-01-01", []float64{0, 0}},
		{"missing-subject", "directional_overlap", "missing", "A", "total", "both", "2026-01-01", "2027-01-01", nil},
		{"overlap-partial", "directional_overlap", "A", "B", "total", "both", "2026-01-02", "2026-02-01", []float64{0, 0}},
		{"overlap-quoted", "directional_overlap", "quoted'person", "A", "total", "forward", "2026-01-01", "2027-01-01", []float64{0}},
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
				if actual, exists := row["subject_label"]; !exists || actual != labels[fmt.Sprint(row["subject_id"])] {
					t.Fatalf("subject name must follow result identity: %v", row)
				}
				if c.op == "directional_overlap" {
					if actual, exists := row["comparison_label"]; !exists || actual != labels[fmt.Sprint(row["comparison_id"])] {
						t.Fatalf("comparison name must follow result identity: %v", row)
					}
				}
				value, err := strconv.ParseFloat(fmt.Sprint(row["value"]), 64)
				if err != nil || math.Abs(value-c.want[i]) > 1e-12 {
					t.Fatalf("row=%v want=%v err=%v", row, c.want[i], err)
				}
			}
		})
	}
	// Both engines execute the same detail plan and compare complete member
	// sets to the published count, including partial months and excluded rows.
	for _, tc := range []struct {
		subject, grain, start, end string
		want                       int
	}{
		{"A", "total", "2026-01-01", "2027-01-01", 4},
		{"A", "month", "2026-01-01", "2026-04-01", 4},
		{"A", "month", "2026-01-02", "2026-02-01", 2},
		{"A", "total", "2026-01-01", "2026-01-02", 1},
		{"C", "total", "2026-01-01", "2027-01-01", 0},
		{"missing", "total", "2026-01-01", "2027-01-01", 0},
	} {
		t.Run("details/"+tc.subject+"/"+tc.grain+"/"+tc.start+"/"+tc.end, func(t *testing.T) {
			c := contract
			c.IncludeDetails = true
			params := map[string]interface{}{"subject_id": tc.subject, "grain": tc.grain, "start_date": tc.start, "end_date": tc.end}
			req, err := metricGoldenResultRequest(t, provider, conn, database, c, bindings, params, true)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := provider.PrepareQuery(context.Background(), conn, req)
			if err != nil {
				t.Fatal(err)
			}
			rows, err := prepared.Execute(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(rows.Rows) != tc.want {
				t.Fatalf("detail rows=%v want=%d", rows.Rows, tc.want)
			}
			seen := map[string]bool{}
			for _, row := range rows.Rows {
				key := fmt.Sprint(row["bucket"], "/", row["member"])
				if seen[key] || row["subject_id"] != tc.subject || row["subject_label"] != labels[tc.subject] {
					t.Fatalf("invalid detail grain: %v", row)
				}
				seen[key] = true
			}
			summary, err := query(t, "count_distinct", tc.subject, "", tc.grain, "", tc.start, tc.end)
			if err != nil {
				t.Fatal(err)
			}
			var count int
			for _, row := range summary.Rows {
				n, _ := strconv.Atoi(fmt.Sprint(row["value"]))
				count += n
			}
			if count != len(rows.Rows) {
				t.Fatalf("summary=%d details=%d", count, len(rows.Rows))
			}
		})
	}
	t.Run("details/month-filter-and-keyset", func(t *testing.T) {
		c := contract
		c.IncludeDetails = true
		params := map[string]interface{}{"subject_id": "A", "grain": "month", "start_date": "2026-01-02", "end_date": "2026-03-01"}
		request := plan.ResultRequest{Limit: 1, Filter: &plan.ResultFilter{Op: "eq", Field: "bucket", Values: []plan.Literal{{Type: datatype.FieldTypeDate, Text: "2026-01-01"}}}}
		var members []string
		for page := 0; page < 3; page++ {
			req, err := metricGoldenResultRequest(t, provider, conn, database, c, bindings, params, true, request)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := provider.PrepareQuery(context.Background(), conn, req)
			if err != nil {
				t.Fatal(err)
			}
			result, err := prepared.Execute(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Rows) == 0 {
				break
			}
			row := result.Rows[0]
			members = append(members, fmt.Sprint(row["member"]))
			// Stable order defaults to subject_id, bucket, member. Consume one
			// row and keep the provider's extra row only as a has-more signal.
			request.After = []plan.Literal{{Type: datatype.FieldTypeString, Text: "A"}, {Type: datatype.FieldTypeDate, Text: "2026-01-01"}, {Type: datatype.FieldTypeString, Text: fmt.Sprint(row["member"])}}
		}
		if strings.Join(members, ",") != "e2,e6" {
			t.Fatalf("filtered/paged members=%v", members)
		}
	})
	for _, operation := range []string{"count_distinct", "directional_overlap"} {
		for _, mutation := range []struct{ name, apply, restore string }{
			{"duplicate-dimension", `INSERT INTO SCHEMA.metric_golden_events VALUES ('e1','2026-01-01')`, `DELETE FROM SCHEMA.metric_golden_events WHERE event_id='e1'; INSERT INTO SCHEMA.metric_golden_events VALUES ('e1','2026-01-01')`},
			{"missing-dimension", `DELETE FROM SCHEMA.metric_golden_events WHERE event_id='e1'`, `INSERT INTO SCHEMA.metric_golden_events VALUES ('e1','2026-01-01')`},
			{"null-date", `UPDATE SCHEMA.metric_golden_events SET event_date=NULL WHERE event_id='e1'`, `UPDATE SCHEMA.metric_golden_events SET event_date='2026-01-01' WHERE event_id='e1'`},
			{"invalid-outside-range", `UPDATE SCHEMA.metric_golden_events SET event_date=NULL WHERE event_id='old'`, `UPDATE SCHEMA.metric_golden_events SET event_date='2025-12-31' WHERE event_id='old'`},
			{"null-member", `UPDATE SCHEMA.metric_golden_facts SET event_id=NULL WHERE event_id='e1'`, `UPDATE SCHEMA.metric_golden_facts SET event_id='e1' WHERE event_id IS NULL`},
			{"duplicate-person", `INSERT INTO SCHEMA.metric_golden_people VALUES ('A','Another name')`, `DELETE FROM SCHEMA.metric_golden_people WHERE person_id='A'; INSERT INTO SCHEMA.metric_golden_people VALUES ('A','Alpha')`},
		} {
			t.Run(operation+"/"+mutation.name, func(t *testing.T) {
				if _, err := db.Exec(qualify(mutation.apply)); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					for _, statement := range strings.Split(mutation.restore, ";") {
						if _, err := db.Exec(qualify(statement)); err != nil {
							t.Error(err)
						}
					}
				})
				_, err := query(t, operation, "A", "B", "total", "both", "2026-01-01", "2027-01-01")
				var assertion *plugin.AnalyticalAssertionError
				if !errors.As(err, &assertion) {
					t.Fatalf("expected independent quality assertion, got %v", err)
				}
				if operation == "count_distinct" {
					c := contract
					c.IncludeDetails = true
					params := map[string]interface{}{"subject_id": "A", "grain": "month", "start_date": "2026-01-01", "end_date": "2027-01-01"}
					req, err := metricGoldenResultRequest(t, provider, conn, database, c, bindings, params, true, plan.ResultRequest{Limit: 1})
					if err != nil {
						t.Fatal(err)
					}
					prepared, err := provider.PrepareQuery(context.Background(), conn, req)
					if err != nil {
						t.Fatal(err)
					}
					_, err = prepared.Execute(context.Background())
					if !errors.As(err, &assertion) {
						t.Fatalf("detail pagination hid quality assertion: %v", err)
					}
				}
			})
		}
	}
	runGroupedDecimalMetricGolden(t, provider, conn, db, database, dialect)
}

func runGroupedDecimalMetricGolden(t *testing.T, provider metricGoldenProvider, conn plugin.ConnectionInfo, db *sql.DB, database string, dialect query.Dialect) {
	t.Helper()
	table := dialect.QuoteIdentifier(database) + "." + dialect.QuoteIdentifier("metric_golden_areas")
	for _, statement := range []string{
		"CREATE TABLE " + table + " (city varchar(200), area_m2 decimal(38,18))",
		"INSERT INTO " + table + " VALUES ('长沙',1.25),('长沙',2.50),('永州',0.50),('',0.75),(NULL,9),(NULL,NULL)",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	contract, bindings := groupedDecimalFixture()
	request, err := metricGoldenRequest(t, provider, conn, database, contract, bindings, nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := provider.PrepareQuery(t.Context(), conn, request)
	if err != nil {
		t.Fatal(err)
	}
	readSet, err := prepared.ReadSet(t.Context())
	if err != nil || len(readSet.Paths) != 1 {
		t.Fatalf("grouped sum read set=%#v err=%v", readSet, err)
	}
	result, err := prepared.Execute(t.Context())
	if err != nil || result == nil || len(result.Rows) != 3 {
		t.Fatalf("grouped sum result=%#v err=%v", result, err)
	}
	want := map[string]string{"": "0.750000000000000000", "长沙": "3.750000000000000000", "永州": "0.500000000000000000"}
	for _, row := range result.Rows {
		key, ok := row["group_key"].(string)
		if !ok || row["value"] != want[key] {
			t.Fatalf("unexpected grouped decimal row: %#v", row)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("missing grouped decimal rows: %v", want)
	}
	if _, err := db.Exec("INSERT INTO " + table + " VALUES ('长沙',NULL)"); err != nil {
		t.Fatal(err)
	}
	request, err = metricGoldenRequest(t, provider, conn, database, contract, bindings, nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err = provider.PrepareQuery(t.Context(), conn, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.Execute(t.Context()); err == nil {
		t.Fatal("NULL measure did not fail the metric quality assertion")
	} else {
		var assertion *plugin.AnalyticalAssertionError
		if !errors.As(err, &assertion) || assertion.Code != "metric_value_required" {
			t.Fatalf("wrong measure quality error: %v", err)
		}
	}
}

func metricGoldenRequest(t *testing.T, provider metricGoldenProvider, conn plugin.ConnectionInfo, namespace string, contract models.MetricContract, bindings metricPlanBindings, parameters map[string]interface{}) (plugin.QueryRequest, error) {
	t.Helper()
	return metricGoldenResultRequest(t, provider, conn, namespace, contract, bindings, parameters, false)
}

func metricGoldenResultRequest(t *testing.T, provider metricGoldenProvider, conn plugin.ConnectionInfo, namespace string, contract models.MetricContract, bindings metricPlanBindings, parameters map[string]interface{}, details bool, resultRequests ...plan.ResultRequest) (plugin.QueryRequest, error) {
	t.Helper()
	p, err := buildMetricResultPlan(contract, bindings, details)
	if err != nil {
		return plugin.QueryRequest{}, err
	}
	order := []plan.SortKey{{Name: "bucket", Direction: "asc"}}
	if contract.Operation == "sum_decimal_by_group" {
		order = []plan.SortKey{{Name: "group_key", Direction: "asc"}}
	}
	if contract.Operation == "directional_overlap" {
		order = append(order, plan.SortKey{Name: "direction", Direction: "asc"})
	}
	resultRequest := plan.ResultRequest{Limit: 240, OrderBy: order}
	if len(resultRequests) > 0 {
		resultRequest = resultRequests[0]
	}
	result, err := plan.ApplyResultRequest(p, resultRequest)
	if err != nil {
		return plugin.QueryRequest{}, err
	}
	engineID := uint(2)
	branch, ok := plugin.EngineCatalogFirstBusinessBranch(provider.EngineCatalogModel())
	if !ok {
		t.Fatal("metric fixture requires a catalog namespace")
	}
	term := branch.Term
	var sources []plugin.SourceBinding
	for _, node := range p.Nodes {
		if node.Scan == nil {
			continue
		}
		source := bindings.Fact
		if node.Scan.Source == "source_10" {
			source = bindings.Relations[10].Target
		}
		if node.Scan.Source == "source_11" {
			source = bindings.Relations[11].Target
		}
		binding := plugin.SourceBinding{Source: node.Scan.Source, Path: plugin.TabularItemPath(engineID, term, namespace, source.Metadata.Name)}
		facts, err := provider.DescribeEngineCatalogFacts(context.Background(), conn, binding.Path, plugin.EngineCatalogFactsOptions{})
		if err != nil || facts.Table == nil {
			return plugin.QueryRequest{}, fmt.Errorf("source facts: %v", err)
		}
		for _, logical := range node.Scan.Fields {
			var relation, fieldID int64
			fmt.Sscanf(logical.Name, "r%d_f%d", &relation, &fieldID)
			name := source.Fields[fieldID].ColumnName
			for _, f := range facts.Table.Fields {
				if f.Name == name {
					binding.Columns = append(binding.Columns, plugin.ColumnBinding{Column: logical.Name, Field: datatype.FieldInfo{Name: f.Name, Path: []string{f.Name}, Type: f.Type, NativeType: f.NativeType, Nullable: f.Nullable, Size: f.Size, Precision: f.Precision, Scale: f.Scale}})
				}
			}
		}

		sources = append(sources, binding)
	}
	compiler := provider.AnalyticalCompiler()
	compiled, err := compiler.Compile(plugin.CompileRequest{Plan: result.Plan, Sources: sources, Instance: plugin.AnalyticalInstance{EngineID: engineID, Capability: plugin.AnalyticalCapability{Supported: true, PlanVersions: []string{plan.SchemaVersion}, SemanticProfiles: []string{plan.SemanticProfile}}}})
	if err != nil {

		return plugin.QueryRequest{}, err
	}
	values := result.Values
	for _, param := range p.Parameters {
		values[param.Name] = plan.Literal{Type: param.Type, Text: parameters[param.Name].(string)}
	}
	return compiled.QueryRequest(values, 30*time.Second)
}
