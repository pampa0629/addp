package plugin

import "github.com/addp/common/datatype"

// QueryDefinitionChecker inspects a fixed request using only a saved schema.
// It must not prepare a query, access catalogs, sample records or perform I/O.
// Its partial output is neither runtime lineage nor an authorization read set.
type QueryDefinitionChecker interface {
	CheckQueryDefinition(QueryDefinition) QueryDefinitionReport
}

type QueryDefinition struct {
	Request QueryRequest
	// SourcePath binds the server-owned snapshot to one catalog leaf.
	SourcePath     EngineCatalogPath
	Source         *datatype.TableInfo
	SchemaCoverage string
}

type QueryDefinitionCheck struct {
	Status DefinitionCheckStatus `json:"status"`
	Code   string                `json:"code"`
	Field  string                `json:"field,omitempty"`
}

type QueryDefinitionReport struct {
	Output         *datatype.TableInfo    `json:"output,omitempty"`
	SchemaCoverage string                 `json:"schema_coverage"`
	Checks         []QueryDefinitionCheck `json:"checks"`
}
