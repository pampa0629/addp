package models

import (
	"github.com/addp/common/engine/plugin"
	"time"
)

type EngineInstanceOverviewResponse struct {
	EngineID  uint                            `json:"engine_id,string" swaggertype:"string"`
	Version   int64                           `json:"version"`
	QueriedAt time.Time                       `json:"queried_at"`
	Metrics   []plugin.InstanceOverviewMetric `json:"metrics"`
}
