package service

import (
	"context"
	"fmt"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
)

// requirePostgreSQLCatalogTable verifies a schema/table target against the
// System catalog control plane and returns the canonical table entry.
func requirePostgreSQLCatalogTable(ctx context.Context, client *commonClient.SystemServiceClient, tenantID, engineID int64, schemaName, tableName string) (*commonClient.EngineCatalogEntry, error) {
	if client == nil || tenantID <= 0 || engineID <= 0 {
		return nil, fmt.Errorf("%w: a PostgreSQL catalog target is required", commonAPI.ErrBadRequest)
	}
	bound := client.WithTenantID(uint(tenantID))
	rootEntries, err := bound.ListEngineCatalogChildren(ctx, uint(engineID), commonClient.EngineCatalogListChildrenRequest{
		Path:    commonClient.EngineCatalogPath{Version: "catalog.path/v1", EngineID: uint(engineID), Segments: []commonClient.EngineCatalogSegment{}},
		Options: commonClient.EngineCatalogListOptions{Limit: 100},
	})
	if err != nil {
		return nil, fmt.Errorf("validate PostgreSQL catalog root: %w", err)
	}
	var root *commonClient.EngineCatalogEntry
	for i := range rootEntries {
		if rootEntries[i].Role == "branch" && len(rootEntries[i].Path.Segments) == 1 {
			root = &rootEntries[i]
			break
		}
	}
	if root == nil {
		return nil, fmt.Errorf("%w: PostgreSQL catalog root is unavailable", commonAPI.ErrBadRequest)
	}
	namespaces, err := bound.ListEngineCatalogChildren(ctx, uint(engineID), commonClient.EngineCatalogListChildrenRequest{Path: root.Path, Options: commonClient.EngineCatalogListOptions{Limit: 1000}})
	if err != nil {
		return nil, fmt.Errorf("validate PostgreSQL schema: %w", err)
	}
	var namespace *commonClient.EngineCatalogEntry
	for i := range namespaces {
		if namespaces[i].Name == schemaName && namespaces[i].Role == "branch" {
			namespace = &namespaces[i]
			break
		}
	}
	if namespace == nil {
		return nil, fmt.Errorf("%w: schema %q was not found in the selected PostgreSQL engine", commonAPI.ErrBadRequest, schemaName)
	}
	tables, err := bound.ListEngineCatalogChildren(ctx, uint(engineID), commonClient.EngineCatalogListChildrenRequest{Path: namespace.Path, Options: commonClient.EngineCatalogListOptions{Limit: 1000}})
	if err != nil {
		return nil, fmt.Errorf("validate PostgreSQL table: %w", err)
	}
	for i := range tables {
		table := &tables[i]
		if table.Name == tableName && table.Role == "leaf" {
			return table, nil
		}
	}
	return nil, fmt.Errorf("%w: table %q was not found in schema %q", commonAPI.ErrBadRequest, tableName, schemaName)
}

func requirePostgreSQLCatalogColumn(ctx context.Context, client *commonClient.SystemServiceClient, tenantID, engineID int64, table *commonClient.EngineCatalogEntry, columnName string) error {
	if client == nil || tenantID <= 0 || engineID <= 0 || table == nil {
		return fmt.Errorf("%w: a PostgreSQL catalog table is required", commonAPI.ErrBadRequest)
	}
	facts, err := client.WithTenantID(uint(tenantID)).DescribeEngineCatalogFacts(ctx, uint(engineID), commonClient.EngineCatalogDescribeFactsRequest{Path: table.Path})
	if err != nil {
		return fmt.Errorf("validate PostgreSQL column: %w", err)
	}
	if facts == nil || facts.Table == nil {
		return fmt.Errorf("%w: table %q does not expose tabular field facts", commonAPI.ErrBadRequest, table.Name)
	}
	for _, field := range facts.Table.Fields {
		if field.Name == columnName {
			return nil
		}
	}
	return fmt.Errorf("%w: column %q was not found in table %q", commonAPI.ErrBadRequest, columnName, table.Name)
}
