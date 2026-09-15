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

func quoteMetricIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func metricRelationName(source metricPlanSource) string {
	return quoteMetricIdentifier(source.Schema) + "." + quoteMetricIdentifier(source.Table)
}

func compileMetricSQL(contract models.MetricContract, bindings metricPlanBindings) (string, error) {
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
				joins = append(joins, "JOIN "+metricRelationName(source)+" "+alias+" ON f."+quoteMetricIdentifier(left.ColumnName)+" = "+alias+"."+quoteMetricIdentifier(right.ColumnName))
				used[ref.RelationID] = true
			}
		}
		resolved, found := source.Fields[ref.FieldID]
		if !found || resolved.ColumnName == "" || (expectedType != "" && resolved.DataType != expectedType) {
			return "", invalidRequest()
		}
		return alias + "." + quoteMetricIdentifier(resolved.ColumnName), nil
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
				requiredValues += " AND q." + quoteMetricIdentifier(relation.Target.Fields[ref.FieldID].ColumnName) + " IS NOT NULL"
			}
		}
		qualityFailures = append(qualityFailures, "(SELECT COUNT(*) FROM "+metricRelationName(relation.Target)+" q WHERE q."+quoteMetricIdentifier(right.ColumnName)+" = f."+quoteMetricIdentifier(left.ColumnName)+requiredValues+") <> 1")
	}
	for _, ref := range []models.MetricFieldReference{contract.Time, contract.Distinct} {
		if ref.RelationID == 0 {
			qualityFailures = append(qualityFailures, "f."+quoteMetricIdentifier(bindings.Fact.Fields[ref.FieldID].ColumnName)+" IS NULL")
		}
	}

	qualityCheck := "COALESCE((SELECT failure FROM (VALUES (0),(1)) rejected(failure) WHERE EXISTS (SELECT 1 FROM " + metricRelationName(bindings.Fact) + " f WHERE " + strings.Join(qualityConditions, " AND ") + " AND (" + strings.Join(qualityFailures, " OR ") + ")) OR (SELECT COUNT(*) FROM " + metricRelationName(identity.Target) + " p WHERE p." + quoteMetricIdentifier(key.ColumnName) + " = :subject_id) > 1),0)"
	if overlap {
		qualityCheck = "COALESCE((SELECT failure FROM (VALUES (0),(1)) rejected(failure) WHERE EXISTS (SELECT 1 FROM " + metricRelationName(bindings.Fact) + " f WHERE " + strings.Join(qualityConditions, " AND ") + " AND (" + strings.Join(qualityFailures, " OR ") + ")) OR EXISTS (SELECT 1 FROM " + metricRelationName(identity.Target) + " p WHERE p." + quoteMetricIdentifier(key.ColumnName) + " IN (:subject_id, :comparison_id) GROUP BY p." + quoteMetricIdentifier(key.ColumnName) + " HAVING COUNT(*) > 1)),0)"
	}
	// A bounded calendar is part of the deterministic plan. It uses only VALUES
	// and scalar date operations, so it introduces no table-function read source.
	offsets := make([]string, 120)
	for index := range offsets {
		offsets[index] = fmt.Sprintf("(%d)", index)
	}
	bucket := "CASE WHEN :grain = 'month' THEN date_trunc('month', " + date + ")::date ELSE CAST(:start_date AS date) END"
	prefix := "WITH calendar AS (SELECT (date_trunc('month', CAST(:start_date AS date)) + n * INTERVAL '1 month')::date AS bucket FROM (VALUES " + strings.Join(offsets, ",") + ") offsets(n)), " +
		"buckets AS (SELECT bucket FROM calendar WHERE :grain = 'month' AND bucket < CAST(:end_date AS date) UNION ALL SELECT CAST(:start_date AS date) WHERE :grain = 'total'), "
	if overlap {
		sets := "members AS (SELECT DISTINCT " + subject + " AS person_id, " + bucket + " AS bucket, " + distinct + " AS member_id FROM " + metricRelationName(bindings.Fact) + " f " + strings.Join(joins, " ") + " WHERE " + strings.Join(conditions, " AND ") + "), "
		counts := "a_counts AS (SELECT bucket, COUNT(*) AS value FROM members WHERE person_id = :subject_id GROUP BY bucket), b_counts AS (SELECT bucket, COUNT(*) AS value FROM members WHERE person_id = :comparison_id GROUP BY bucket), shared AS (SELECT a.bucket, COUNT(*) AS value FROM members a JOIN members z ON a.bucket=z.bucket AND a.member_id=z.member_id WHERE a.person_id=:subject_id AND z.person_id=:comparison_id GROUP BY a.bucket), "
		stats := "stats AS (SELECT b.bucket, COALESCE(a.value,0) AS a_count, COALESCE(z.value,0) AS b_count, COALESCE(c.value,0) AS shared_count FROM buckets b LEFT JOIN a_counts a ON a.bucket=b.bucket LEFT JOIN b_counts z ON z.bucket=b.bucket LEFT JOIN shared c ON c.bucket=b.bucket), "
		roles := "roles AS (SELECT 'forward'::text AS direction, CAST(:subject_id AS text) AS subject_id, CAST(:comparison_id AS text) AS comparison_id UNION ALL SELECT 'reverse'::text, CAST(:comparison_id AS text), CAST(:subject_id AS text) WHERE :directions='both') "
		denominator := "CASE WHEN r.direction='forward' THEN s.a_count ELSE s.b_count END"
		other := "CASE WHEN r.direction='forward' THEN s.b_count ELSE s.a_count END"
		return prefix + sets + counts + stats + roles + "SELECT r.direction,r.subject_id,r.comparison_id,s.bucket,(CASE WHEN (" + denominator + ")=0 THEN 0::numeric ELSE s.shared_count::numeric/(" + denominator + ") END + " + qualityCheck + ")::numeric AS value,(" + denominator + ")::bigint AS subject_count,(" + other + ")::bigint AS comparison_count,s.shared_count::bigint AS shared_count FROM roles r CROSS JOIN stats s JOIN " + metricRelationName(identity.Target) + " p ON p." + quoteMetricIdentifier(key.ColumnName) + "=r.subject_id JOIN " + metricRelationName(identity.Target) + " other_person ON other_person." + quoteMetricIdentifier(key.ColumnName) + "=r.comparison_id", nil
	}
	return "WITH calendar AS (SELECT (date_trunc('month', CAST(:start_date AS date)) + n * INTERVAL '1 month')::date AS bucket FROM (VALUES " + strings.Join(offsets, ",") + ") offsets(n)), " +
		"buckets AS (SELECT bucket FROM calendar WHERE :grain = 'month' AND bucket < CAST(:end_date AS date) UNION ALL SELECT CAST(:start_date AS date) WHERE :grain = 'total'), " +
		"counts AS (SELECT " + bucket + " AS bucket, COUNT(DISTINCT " + distinct + ") AS value FROM " + metricRelationName(bindings.Fact) + " f " + strings.Join(joins, " ") + " WHERE " + strings.Join(conditions, " AND ") + " GROUP BY 1) " +
		"SELECT p." + quoteMetricIdentifier(key.ColumnName) + " AS subject_id, b.bucket, (COALESCE(c.value, 0) + " + qualityCheck + ")::bigint AS value FROM " + metricRelationName(identity.Target) + " p CROSS JOIN buckets b LEFT JOIN counts c ON c.bucket = b.bucket WHERE p." + quoteMetricIdentifier(key.ColumnName) + " = :subject_id", nil
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
