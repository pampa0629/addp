//go:build !proj

package proj

import "fmt"

type Transformer struct{}

func NewTransformer(_, _ string) (*Transformer, error) {
	return nil, fmt.Errorf("proj executor is not enabled")
}

func (t *Transformer) Close() {}

func (t *Transformer) TransformFlatCoords(_ []float64, _ int) ([]float64, error) {
	return nil, fmt.Errorf("proj executor is not enabled")
}
