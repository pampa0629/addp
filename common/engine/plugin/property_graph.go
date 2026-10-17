package plugin

import (
	"context"
	"errors"
	"fmt"
)

// PropertyGraphProvider supplies native operations for trusted domain services.
// User-entered Cypher must use PreparedGraphQueryProvider instead.
type PropertyGraphProvider interface {
	EnginePlugin
	ExecuteGraphQueryAtPath(context.Context, ConnectionInfo, EngineCatalogPath, string, QueryOptions) (*GraphQueryResult, error)
	GraphDialect() PropertyGraphDialect
	SyncGraphSearchIndexes(context.Context, ConnectionInfo, EngineCatalogPath, GraphSearchSchema) ([]GraphSearchIndex, error)
	GraphSearchQuery(GraphSearchIndex, string) (string, map[string]interface{}, error)
	EnsureGraphUniqueConstraint(context.Context, ConnectionInfo, EngineCatalogPath, GraphConstraint) error
	ListGraphConstraints(context.Context, ConnectionInfo, EngineCatalogPath) ([]GraphConstraint, error)
}

type PropertyGraphDialect struct {
	IDTemplate                string
	DegreeTemplate            string
	GDS                       bool
	Spatial                   bool
	AllShortestPaths          bool
	ExcludedNodeLabels        []string
	ExcludedRelationshipTypes []string
}

func (d PropertyGraphDialect) ID(variable string) string { return fmt.Sprintf(d.IDTemplate, variable) }

type GraphSearchIndex struct {
	Name       string
	Labels     []string
	Properties []string
}

type GraphSearchSchema struct {
	OwnerPrefix string
	Managed     bool
	Indexes     []GraphSearchIndex
}

type GraphConstraint struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Label      string   `json:"label"`
	Properties []string `json:"properties"`
	Status     string   `json:"status"`
}

var ErrGraphSearchIndexConflict = errors.New("external graph search index conflicts with ontology")
var ErrGraphSearchLimit = errors.New("graph search exceeds match budget")
var ErrGraphTraversalLimit = errors.New("graph path exceeds traversal budget")
