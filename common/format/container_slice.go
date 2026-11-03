package format

import (
	"context"
	"errors"
	"fmt"
)

var ErrGridSliceSelection = errors.New("invalid grid slice selection")

var ErrContainerSourceChanged = errors.New("container source version changed")

// GridSliceSelection selects one bounded XY grid with an explicit value mode. It is a temporary view
// of a container variable, not a new data type, DataItem, task or artifact.
type GridSliceSelection struct {
	Variable string  `json:"variable"`
	Indices  []int64 `json:"indices"`
	X        int64   `json:"x"`
	Y        int64   `json:"y"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
}

func (s GridSliceSelection) Validate() error {
	if s.Variable == "" || len(s.Variable) > 1024 || len(s.Indices) > 6 || s.X < 0 || s.Y < 0 || s.Width < 1 || s.Height < 1 || s.Width > 128 || s.Height > 128 || s.Width*s.Height > 4096 {
		return fmt.Errorf("invalid bounded grid slice selection")
	}
	for _, i := range s.Indices {
		if i < 0 {
			return fmt.Errorf("negative grid slice index")
		}
	}
	return nil
}

type GridSliceResult struct {
	SchemaVersion string                 `json:"schema_version"`
	Format        string                 `json:"format"`
	SourceVersion string                 `json:"source_version"`
	Selection     GridSliceSelection     `json:"selection"`
	Variable      map[string]interface{} `json:"variable"`
	Values        [][]string             `json:"values"`
	Valid         [][]bool               `json:"valid"`
	ValueMode     string                 `json:"value_mode"`
	Spatial       map[string]interface{} `json:"spatial"`
}

// The existing container factory remains the only runtime binding path.
// Slicing is an optional content capability of that bound provider.
type RuntimeContainerSliceProviderFactory interface {
	RuntimeContainerInfoProviderFactory
	RequiredContainerSliceOperators() []string
}

type ContainerSliceReader interface {
	BoundContainerInfoProvider
	ReadGridSlice(context.Context, string, GridSliceSelection) (*GridSliceResult, error)
}
