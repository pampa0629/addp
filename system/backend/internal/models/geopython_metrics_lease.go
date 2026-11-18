package models

import commonmodels "github.com/addp/common/models"

type GeoPythonMetricsLease struct {
	Singleton bool `gorm:"primaryKey;default:true"`
	commonmodels.GeoPythonMetricsIdentity
}

func (GeoPythonMetricsLease) TableName() string { return "geopython_metrics_lease" }
