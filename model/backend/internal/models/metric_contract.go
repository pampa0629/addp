package models

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
type MetricContract struct {
	Operation         string                `json:"operation" binding:"required,oneof=count_distinct directional_overlap"`
	Subject           MetricFieldReference  `json:"subject"`
	SubjectRelationID int64                 `json:"subject_relation_id" binding:"required,gt=0"`
	Distinct          MetricFieldReference  `json:"distinct"`
	Time              MetricFieldReference  `json:"time"`
	Filters           []MetricBooleanFilter `json:"filters"`
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
	Input *MetricQueryInput `json:"input"`
}
