package plugin

import "context"

// InstanceOverviewProvider reads native operational facts using only the registered credentials.
// It is independent of connection health probes and does not enumerate business content.
type InstanceOverviewProvider interface {
	ReadInstanceOverview(context.Context, ConnectionInfo) (*InstanceOverview, error)
}

type InstanceOverview struct {
	Metrics []InstanceOverviewMetric `json:"metrics"`
}

// Value is exact, optional, and encoded as a decimal string at the HTTP boundary.
type InstanceOverviewMetric struct {
	Key       string  `json:"key"`
	Scope     string  `json:"scope"`
	ScopeName string  `json:"scope_name,omitempty"`
	Unit      string  `json:"unit"`
	Semantics string  `json:"semantics"`
	Status    string  `json:"status"`
	Value     *uint64 `json:"value,omitempty,string" swaggertype:"string"`
}

func (m *InstanceOverviewMetric) SetValue(value uint64) {
	m.Value = &value
	m.Status = "available"
}
