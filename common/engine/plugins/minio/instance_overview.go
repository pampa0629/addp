package minio

import (
	"context"

	"github.com/addp/common/engine/plugin"
	miniogo "github.com/minio/minio-go/v7"
)

func (p *MinIOPlugin) ReadInstanceOverview(ctx context.Context, info plugin.ConnectionInfo) (*plugin.InstanceOverview, error) {
	client, err := p.createClient(info)
	if err != nil {
		return nil, err
	}
	metric := plugin.InstanceOverviewMetric{Key: "account_buckets", Scope: "account", Unit: "count", Semantics: "gauge", Status: "unavailable"}
	buckets, err := client.ListBuckets(ctx)
	if err != nil {
		if miniogo.ToErrorResponse(err).Code != "AccessDenied" {
			return nil, err
		}
		metric.Status = "permission_denied"
	} else {
		metric.SetValue(uint64(len(buckets)))
	}
	return &plugin.InstanceOverview{Metrics: []plugin.InstanceOverviewMetric{metric}}, nil
}
