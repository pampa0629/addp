//go:build !proj

package spatial

import "context"

type projExecutor struct{}

func (projExecutor) Name() string {
	return "proj"
}

func (projExecutor) CanTransform(sourceCRS, targetCRS CRS) bool {
	return false
}

func (projExecutor) TransformGeoJSON(_ context.Context, _ interface{}, _, _ CRS) (interface{}, error) {
	return nil, ErrPROJUnavailable
}
