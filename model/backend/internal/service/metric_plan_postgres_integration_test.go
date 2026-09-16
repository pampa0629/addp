package service

import (
	"database/sql"
	"fmt"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/common/query"
	"github.com/addp/model/internal/models"
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
	bindings := metricPlanBindings{Fact: metricPlanSource{Metadata: metricTableMetadata{Name: "metric_golden_facts"}, Fields: map[int64]models.LogicalField{1: field(1, "person_id", "string", false), 2: field(2, "event_id", "string", false), 3: field(3, "leader", "bool", false)}}, Relations: map[int64]metricPlanRelation{
		10: {SourceField: 1, TargetField: 4, Target: metricPlanSource{Metadata: metricTableMetadata{Name: "metric_golden_people"}, Fields: map[int64]models.LogicalField{4: field(4, "person_id", "string", true)}}},
		11: {SourceField: 2, TargetField: 5, Target: metricPlanSource{Metadata: metricTableMetadata{Name: "metric_golden_events"}, Fields: map[int64]models.LogicalField{5: field(5, "event_id", "string", true), 6: field(6, "event_date", "date", false)}}},
	}}
	return models.MetricContract{Operation: "count_distinct", Subject: models.MetricFieldReference{FieldID: 1}, SubjectRelationID: 10, Distinct: models.MetricFieldReference{FieldID: 2}, Time: models.MetricFieldReference{FieldID: 6, RelationID: 11}, Filters: []models.MetricBooleanFilter{{Field: models.MetricFieldReference{FieldID: 3}, Value: true}}}, bindings
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
			if _, err := buildMetricPlan(contract, bindings); err == nil {
				t.Fatal("invalid contract accepted")
			}
		})
	}
}

func TestDirectionalOverlapRejectsUndeclaredRoleFiltersAndParameters(t *testing.T) {
	c, b := metricGoldenContract()
	c.Operation = "directional_overlap"
	if _, err := buildMetricPlan(c, b); err == nil {
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

func TestPostgresMetricPlanUsesSharedPreparedQuery(t *testing.T) {
	dsn := os.Getenv("ADDP_TEST_MODEL_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires Model PostgreSQL gate")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	dialect := query.ForDialect(query.DialectPostgreSQL)
	schema := fmt.Sprintf("metric_plan_%d", time.Now().UnixNano())
	if _, err = db.Exec("CREATE SCHEMA " + dialect.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP SCHEMA " + dialect.QuoteIdentifier(schema) + " CASCADE")
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
	conn := plugin.ConnectionInfo{"host": parsed.Hostname(), "port": port, "database": strings.TrimPrefix(parsed.Path, "/"), "user": parsed.User.Username(), "password": password, "sslmode": "disable"}
	runMetricGolden(t, &postgresql.PostgreSQLPlugin{}, conn, db, schema, dialect)
}
