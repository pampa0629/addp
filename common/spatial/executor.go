package spatial

import "context"

type transformExecutor interface {
	Name() string
	CanTransform(sourceCRS, targetCRS CRS) bool
	TransformGeoJSON(ctx context.Context, payload interface{}, sourceCRS, targetCRS CRS) (interface{}, error)
}

func availableTransformExecutors() []transformExecutor {
	return []transformExecutor{
		pureGoExecutor{},
		projExecutor{},
	}
}

func resolveTransformExecutor(sourceCRS, targetCRS CRS) transformExecutor {
	for _, executor := range availableTransformExecutors() {
		if executor.CanTransform(sourceCRS, targetCRS) {
			return executor
		}
	}
	return nil
}
