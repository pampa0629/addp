package models

import commonmodels "github.com/addp/common/models"

type RuntimeMetricsLease struct {
	commonmodels.RuntimeMetricsIdentity
}

func (RuntimeMetricsLease) TableName() string { return "runtime_metrics_leases" }
