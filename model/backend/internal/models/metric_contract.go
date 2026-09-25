package models

import "encoding/json"

// MetricFieldReference identifies a field in the source fact or an explicitly
// declared dimension relation. Zero RelationID denotes the fact itself.
type MetricFieldReference struct {
	FieldID    int64 `json:"field_id" binding:"required,gt=0"`
	RelationID int64 `json:"relation_id" binding:"gte=0"`
}

type MetricBooleanFilter struct {
	Field MetricFieldReference `json:"field"`
	Value bool                 `json:"value"`
}

// MetricContract is the typed executable metric contract. Business names
// and physical identifiers are resolved by Model, never supplied as SQL.
// Count/overlap references are validated by the operation-aware plan builder;
// grouped sums intentionally omit those references entirely.
type MetricContract struct {
	IncludeDetails    bool                  `json:"include_details,omitempty"`
	Operation         string                `json:"operation" binding:"required,oneof=count_distinct directional_overlap sum_decimal_by_group"`
	Subject           MetricFieldReference  `json:"subject" binding:"-"`
	SubjectRelationID int64                 `json:"subject_relation_id"`
	SubjectLabel      *MetricFieldReference `json:"subject_label,omitempty"`
	Distinct          MetricFieldReference  `json:"distinct" binding:"-"`
	Time              MetricFieldReference  `json:"time" binding:"-"`
	Group             *MetricFieldReference `json:"group,omitempty"`
	Measure           *MetricFieldReference `json:"measure,omitempty"`
	Filters           []MetricBooleanFilter `json:"filters"`
}

// Existing published contracts retain their exact JSON representation because
// it contributes to their dependency hash. The new operation has a disjoint
// shape and does not serialize unrelated legacy-operation fields.
func (c MetricContract) MarshalJSON() ([]byte, error) {
	if c.Operation == "sum_decimal_by_group" {
		return json.Marshal(struct {
			Operation string                `json:"operation"`
			Group     *MetricFieldReference `json:"group"`
			Measure   *MetricFieldReference `json:"measure"`
		}{Operation: c.Operation, Group: c.Group, Measure: c.Measure})
	}
	type plain MetricContract
	return json.Marshal(plain(c))
}

type MetricQueryInput struct {
	ComparisonID string `json:"comparison_id,omitempty"`
	Directions   string `json:"directions,omitempty"`
	SubjectID    string `json:"subject_id" binding:"required,max=200"`
	StartDate    string `json:"start_date" binding:"required"`
	EndDate      string `json:"end_date" binding:"required"`
	Grain        string `json:"grain" binding:"required,oneof=month total"`
}

type MetricPlanRequest struct {
	ResultKind string            `json:"result_kind,omitempty" binding:"omitempty,oneof=details"`
	Input      *MetricQueryInput `json:"input"`
}
