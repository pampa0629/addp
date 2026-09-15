package models

type MetricSourceRequest struct {
	ImplementationID int64 `json:"implementation_id" binding:"required,gt=0"`
	RevisionID       int64 `json:"revision_id" binding:"required,gt=0"`
}
type MetricSourceSnapshot struct {
	ImplementationID           int64  `json:"implementation_id"`
	RevisionID                 int64  `json:"revision_id"`
	MetricDefinitionID         int64  `json:"metric_definition_id"`
	MetricDefinitionRevisionID int64  `json:"metric_definition_revision_id"`
	DependencyHash             string `json:"dependency_hash"`
}

// RebindMetricSourceRequest replaces an existing SQL publication atomically.
type RebindMetricSourceRequest struct {
	MetricSource   *MetricSourceRequest `json:"metric_source" binding:"required"`
	ServiceVersion string               `json:"service_version" binding:"required"`
}
