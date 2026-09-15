package service

import (
	"fmt"
	"sort"
	"strings"
	"time"

	commonquery "github.com/addp/common/query"
	"github.com/addp/model/internal/models"
)

// metricPlanSource is resolved from Model-owned field and relation identities.
// It is deliberately not an HTTP input: callers cannot choose physical SQL.
type metricPlanSource struct {
	Schema string
	Table  string
	Fields map[int64]models.LogicalField
}

type metricPlanRelation struct {
	SourceField int64
	TargetField int64
	Target      metricPlanSource
}

type metricPlanBindings struct {
	Fact      metricPlanSource
	Relations map[int64]metricPlanRelation
}

func validateMetricQueryInput(input models.MetricQueryInput) error {
	start, err := time.Parse("2006-01-02", input.StartDate)
	if err != nil {
		return invalidRequest()
	}
	end, err := time.Parse("2006-01-02", input.EndDate)
	if err != nil || !end.After(start) || strings.TrimSpace(input.SubjectID) == "" || len(input.SubjectID) > 200 || !commonquery.ParameterOptionAllows(metricParameterOptions("grain"), input.Grain) {
		return invalidRequest()
	}
	months := (end.Year()-start.Year())*12 + int(end.Month()-start.Month())
	if end.Day() > 1 {
		months++
	}
	if months > 120 {
		return invalidRequest()
	}
	return nil
}

func validateMetricOperationInput(operation string, input models.MetricQueryInput) error {
	if err := validateMetricQueryInput(input); err != nil {
		return err
	}
	switch operation {
	case "count_distinct":
		if input.ComparisonID != "" || input.Directions != "" {
			return invalidRequest()
		}
	case "directional_overlap":
		if strings.TrimSpace(input.ComparisonID) == "" || len(input.ComparisonID) > 200 || !commonquery.ParameterOptionAllows(metricParameterOptions("directions"), input.Directions) {
			return invalidRequest()
		}
	default:
		return invalidRequest()
	}
	return nil
}

func compileMetricSQL(contract models.MetricContract, bindings metricPlanBindings, dialect commonquery.AnalyticalDialect) (string, error) {
	quote := dialect.QuoteIdentifier
	relationName := func(source metricPlanSource) string { return dialect.QualifiedTable(source.Schema, source.Table) }
	column := func(alias string, f models.LogicalField) string {
		expr := alias + "." + quote(f.ColumnName)
		if f.DataType == "string" {
			return dialect.ExactText(expr)
		}
		return expr
	}
	overlap := contract.Operation == "directional_overlap"
	if (!overlap && contract.Operation != "count_distinct") || (overlap && len(contract.Filters) != 0) || contract.Subject.RelationID != 0 || contract.Subject.FieldID <= 0 || contract.Distinct.FieldID <= 0 || contract.Time.FieldID <= 0 {
		return "", invalidRequest()
	}
	if bindings.Fact.Schema == "" || bindings.Fact.Table == "" {
		return "", invalidRequest()
	}
	identity, ok := bindings.Relations[contract.SubjectRelationID]
	if !ok || contract.SubjectRelationID <= 0 || identity.SourceField != contract.Subject.FieldID {
		return "", invalidRequest()
	}
	key, ok := identity.Target.Fields[identity.TargetField]
	if !ok || !singleMetricKey(identity.Target, identity.TargetField) || key.Nullable || key.DataType != "string" {
		return "", invalidRequest()
	}

	joins := []string{}
	used := map[int64]bool{}
	field := func(ref models.MetricFieldReference, expectedType string) (string, error) {
		source, alias := bindings.Fact, "f"
		if ref.RelationID != 0 {
			relation, found := bindings.Relations[ref.RelationID]
			if !found {
				return "", invalidRequest()
			}
			left, leftOK := bindings.Fact.Fields[relation.SourceField]
			right, rightOK := relation.Target.Fields[relation.TargetField]
			if !leftOK || !rightOK || !singleMetricKey(relation.Target, relation.TargetField) || right.Nullable || left.DataType != right.DataType {
				return "", invalidRequest()
			}
			source, alias = relation.Target, fmt.Sprintf("d%d", ref.RelationID)
			if !used[ref.RelationID] {
				joins = append(joins, "JOIN "+relationName(source)+" "+alias+" ON "+column("f", left)+" = "+column(alias, right))
				used[ref.RelationID] = true
			}
		}
		resolved, found := source.Fields[ref.FieldID]
		if !found || resolved.ColumnName == "" || (expectedType != "" && resolved.DataType != expectedType) {
			return "", invalidRequest()
		}
		return column(alias, resolved), nil
	}
	subject, err := field(contract.Subject, "string")
	if err != nil {
		return "", err
	}
	distinct, err := field(contract.Distinct, "")
	if err != nil {
		return "", err
	}
	date, err := field(contract.Time, "date")
	if err != nil {
		return "", err
	}
	subjectCondition := subject + " = :subject_id"
	if overlap {
		subjectCondition = subject + " IN (:subject_id, :comparison_id)"
	}
	conditions := []string{subjectCondition, date + " >= CAST(:start_date AS date)", date + " < CAST(:end_date AS date)"}
	qualityConditions := []string{subjectCondition}
	seenFilters := map[models.MetricFieldReference]bool{}
	for _, filter := range contract.Filters {
		if seenFilters[filter.Field] {
			return "", invalidRequest()
		}
		seenFilters[filter.Field] = true
		column, fieldErr := field(filter.Field, "bool")
		if fieldErr != nil {
			return "", fieldErr
		}
		literal := "FALSE"
		if filter.Value {
			literal = "TRUE"
		}
		conditions = append(conditions, column+" = "+literal)
		if filter.Field.RelationID == 0 {
			qualityConditions = append(qualityConditions, column+" = "+literal)
		}
	}
	// Check the declared many-to-one joins in the same query snapshot. A scalar
	// subquery must produce at most one row; two rows deliberately reject a
	// broken dependency instead of silently dropping or multiplying facts.
	qualityFailures := []string{}
	for _, id := range sortedMetricRelationIDs(bindings.Relations) {
		relation := bindings.Relations[id]
		left, right := bindings.Fact.Fields[relation.SourceField], relation.Target.Fields[relation.TargetField]
		requiredValues := ""
		for _, ref := range []models.MetricFieldReference{contract.Time, contract.Distinct} {
			if ref.RelationID == id {
				requiredValues += " AND q." + quote(relation.Target.Fields[ref.FieldID].ColumnName) + " IS NOT NULL"
			}
		}
		qualityFailures = append(qualityFailures, "(SELECT COUNT(*) FROM "+relationName(relation.Target)+" q WHERE "+column("q", right)+" = "+column("f", left)+requiredValues+") <> 1")
	}
	for _, ref := range []models.MetricFieldReference{contract.Time, contract.Distinct} {
		if ref.RelationID == 0 {
			qualityFailures = append(qualityFailures, "f."+quote(bindings.Fact.Fields[ref.FieldID].ColumnName)+" IS NULL")
		}
	}

	qualityCheck := "COALESCE((SELECT failure FROM " + dialect.IntegerRows("failure", 2) + " rejected WHERE EXISTS (SELECT 1 FROM " + relationName(bindings.Fact) + " f WHERE " + strings.Join(qualityConditions, " AND ") + " AND (" + strings.Join(qualityFailures, " OR ") + ")) OR (SELECT COUNT(*) FROM " + relationName(identity.Target) + " p WHERE " + column("p", key) + " = :subject_id) > 1),0)"
	if overlap {
		qualityCheck = "COALESCE((SELECT failure FROM " + dialect.IntegerRows("failure", 2) + " rejected WHERE EXISTS (SELECT 1 FROM " + relationName(bindings.Fact) + " f WHERE " + strings.Join(qualityConditions, " AND ") + " AND (" + strings.Join(qualityFailures, " OR ") + ")) OR EXISTS (SELECT 1 FROM " + relationName(identity.Target) + " p WHERE " + column("p", key) + " IN (:subject_id, :comparison_id) GROUP BY " + column("p", key) + " HAVING COUNT(*) > 1)),0)"
	}
	// A bounded calendar is part of the deterministic plan. It uses only constant rows
	// and scalar date operations, so it introduces no table-function read source.
	bucket := "CASE WHEN :grain = 'month' THEN " + dialect.MonthStart(date) + " ELSE " + dialect.Date(":start_date") + " END"
	prefix := "WITH calendar AS (SELECT " + dialect.AddMonths(dialect.MonthStart(dialect.Date(":start_date")), "n") + " AS bucket FROM " + dialect.IntegerRows("n", 120) + " offsets), " +
		"buckets AS (SELECT bucket FROM calendar WHERE :grain = 'month' AND bucket < CAST(:end_date AS date) UNION ALL SELECT CAST(:start_date AS date) WHERE :grain = 'total'), "
	if overlap {
		sets := "members AS (SELECT DISTINCT " + subject + " AS person_id, " + bucket + " AS bucket, " + distinct + " AS member_id FROM " + relationName(bindings.Fact) + " f " + strings.Join(joins, " ") + " WHERE " + strings.Join(conditions, " AND ") + "), "
		counts := "a_counts AS (SELECT bucket, COUNT(*) AS value FROM members WHERE person_id = :subject_id GROUP BY bucket), b_counts AS (SELECT bucket, COUNT(*) AS value FROM members WHERE person_id = :comparison_id GROUP BY bucket), " + quote("shared") + " AS (SELECT a.bucket, COUNT(*) AS value FROM members a JOIN members z ON a.bucket=z.bucket AND a.member_id=z.member_id WHERE a.person_id=:subject_id AND z.person_id=:comparison_id GROUP BY a.bucket), "
		stats := "stats AS (SELECT b.bucket, COALESCE(a.value,0) AS a_count, COALESCE(z.value,0) AS b_count, COALESCE(c.value,0) AS shared_count FROM buckets b LEFT JOIN a_counts a ON a.bucket=b.bucket LEFT JOIN b_counts z ON z.bucket=b.bucket LEFT JOIN " + quote("shared") + " c ON c.bucket=b.bucket), "
		roles := quote("roles") + " AS (SELECT " + dialect.Text("'forward'") + " AS direction, " + dialect.Text(":subject_id") + " AS subject_id, " + dialect.Text(":comparison_id") + " AS comparison_id UNION ALL SELECT " + dialect.Text("'reverse'") + ", " + dialect.Text(":comparison_id") + ", " + dialect.Text(":subject_id") + " WHERE :directions='both') "
		denominator := "CASE WHEN r.direction='forward' THEN s.a_count ELSE s.b_count END"
		other := "CASE WHEN r.direction='forward' THEN s.b_count ELSE s.a_count END"
		value := dialect.Decimal("CASE WHEN (" + denominator + ")=0 THEN 0 ELSE " + dialect.Decimal("s.shared_count") + "/(" + denominator + ") END + " + qualityCheck)
		return prefix + sets + counts + stats + roles + "SELECT r.direction,r.subject_id,r.comparison_id,s.bucket," + value + " AS value," + dialect.Integer(denominator) + " AS subject_count," + dialect.Integer(other) + " AS comparison_count," + dialect.Integer("s.shared_count") + " AS shared_count FROM " + quote("roles") + " r CROSS JOIN stats s JOIN " + relationName(identity.Target) + " p ON " + column("p", key) + "=r.subject_id JOIN " + relationName(identity.Target) + " other_person ON " + column("other_person", key) + "=r.comparison_id", nil
	}
	return prefix + "counts AS (SELECT " + bucket + " AS bucket, COUNT(DISTINCT " + distinct + ") AS value FROM " + relationName(bindings.Fact) + " f " + strings.Join(joins, " ") + " WHERE " + strings.Join(conditions, " AND ") + " GROUP BY 1) " +
		"SELECT p." + quote(key.ColumnName) + " AS subject_id, b.bucket, " + dialect.Integer("COALESCE(c.value, 0) + "+qualityCheck) + " AS value FROM " + relationName(identity.Target) + " p CROSS JOIN buckets b LEFT JOIN counts c ON c.bucket = b.bucket WHERE " + column("p", key) + " = :subject_id", nil

}

func sortedMetricRelationIDs(relations map[int64]metricPlanRelation) []int64 {
	ids := make([]int64, 0, len(relations))
	for id := range relations {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func singleMetricKey(source metricPlanSource, fieldID int64) bool {
	count := 0
	for _, field := range source.Fields {
		if field.IsPK {
			count++
			if field.ID != fieldID {
				return false
			}
		}
	}
	return count == 1
}
