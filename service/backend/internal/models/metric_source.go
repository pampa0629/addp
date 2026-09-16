package models

import "github.com/addp/common/client"

type MetricSourceRequest struct {
	ImplementationID int64 `json:"implementation_id" binding:"required,gt=0"`
	RevisionID       int64 `json:"revision_id" binding:"required,gt=0"`
}
type MetricSourceSnapshot = client.ModelMetricPlan

// RebindMetricSourceRequest replaces an existing metric publication atomically.
type RebindMetricSourceRequest struct {
	MetricSource *MetricSourceRequest `json:"metric_source" binding:"required"`
	Version      int64                `json:"version" binding:"required,gt=0"`
}
