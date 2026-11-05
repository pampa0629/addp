package plugin

import (
	"context"

	"github.com/addp/common/datatype"
)

// CatalogTablePageReader owns the fixed direct projection of a resolved table.
// Arbitrary SQL uses the query runtime and does not inherit source column facts.
type CatalogTablePageReader interface {
	ReadCatalogTablePage(context.Context, ConnectionInfo, EngineCatalogPath, CatalogTablePageOptions) (*BatchData, error)
}

type CatalogTablePageOptions struct {
	Limit  int
	Offset int64
	// Predicate is compiled by the server from a validated table scope.
	// Values must be bound through Args.
	Predicate string
	Args      []interface{}
	// CheckSchema runs on the live schema used to bind the page, before reading rows.
	CheckSchema func([]datatype.FieldInfo) error
}
