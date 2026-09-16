package service

import (
	"sort"
	"strings"
	"time"

	commonquery "github.com/addp/common/query"
	"github.com/addp/model/internal/models"
)

// metricPlanSource is resolved from Model-owned field and relation identities.
// It is deliberately not an HTTP input: callers cannot choose physical SQL.
type metricPlanSource struct {
	Metadata metricTableMetadata
	Fields   map[int64]models.LogicalField
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
