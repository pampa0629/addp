package plugin

import (
	"context"

	"github.com/addp/common/datatype"
)

// TableResultProvider owns the frozen source query and atomic existing-table write.
// Owners authorize ReadSet before consuming the plan; they never submit a second SQL.
type TableResultProvider interface {
	EnginePlugin
	PrepareTableResult(context.Context, ConnectionInfo, TableResultRequest) (PreparedTableResult, error)
}

type TableResultRequest struct {
	Query     QueryRequest
	Target    EngineCatalogPath
	WriteMode string
}

type PreparedTableResult interface {
	ReadSet(context.Context) (*QueryReadSet, error)
	Execute(context.Context) (*TableResult, error)
}

// TableResult is returned only after commit. Field structures were observed under
// the transaction's schema locks. It contains neither resource locators nor policy.
type TableResult struct {
	RowsAffected         int64
	Sources              []TableResultSource
	TargetFields         []datatype.FieldInfo
	FieldLineageComplete bool
	FieldMappings        []TableResultFieldMapping
}

type TableResultSource struct {
	Path   EngineCatalogPath
	Fields []datatype.FieldInfo
}

type TableResultFieldMapping struct {
	SourcePath     EngineCatalogPath // empty for generated values
	SourceField    string
	TargetField    string
	Transformation string
}
