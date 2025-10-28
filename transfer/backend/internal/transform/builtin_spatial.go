package transform

import "github.com/addp/transfer/pkg/pipeline"

func init() {
	MustRegisterTransform("spatial", pipeline.NewSpatialTransform, pipeline.SpatialTransformCapability)
}
