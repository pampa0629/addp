package scanruntime

import (
	"context"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
)

func (s *DatabaseRuntime) describeTableFacts(
	ctx context.Context,
	resource *commonModels.Engine,
	scanCatalog databaseScanCatalog,
	schemaName string,
	tableName string,
) (*plugin.EngineCatalogFacts, error) {
	path := plugin.TabularItemPath(resource.ID, scanCatalog.namespaceTerm, schemaName, tableName)
	item, err := scanCatalog.factsProvider.DescribeEngineCatalogFacts(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), path, plugin.EngineCatalogFactsOptions{
		IncludeIndexes:      true,
		IncludeConstraints:  true,
		IncludePartitioning: true,
		IncludeSpatialFacts: true,
	})
	if err != nil {
		return nil, err
	}
	return item, nil
}

func mergeDatabaseTableInfo(base, described datatype.TableInfo) datatype.TableInfo {
	if described.Name == "" {
		described.Name = base.Name
	}
	if described.Kind == "" {
		described.Kind = base.Kind
	}
	if described.Comment == "" {
		described.Comment = base.Comment
	}
	if described.RowCount == nil {
		described.RowCount = base.RowCount
	}
	if described.EstimatedRowCount == nil {
		described.EstimatedRowCount = base.EstimatedRowCount
	}
	if base.SizeBytes != nil {
		described.SizeBytes = base.SizeBytes
	} else if described.SizeBytes == nil {
		described.SizeBytes = base.SizeBytes
	}
	if described.CreatedAt == nil {
		described.CreatedAt = base.CreatedAt
	}
	if described.UpdatedAt == nil {
		described.UpdatedAt = base.UpdatedAt
	}
	if len(described.Native) == 0 {
		described.Native = base.Native
	}
	if len(described.PrimaryKey) == 0 {
		described.PrimaryKey = primaryKeyFieldNames(described.Fields)
	}
	return described
}

func primaryKeyFieldNames(fields []datatype.FieldInfo) []string {
	if len(fields) == 0 {
		return nil
	}
	names := make([]string, 0)
	for _, field := range fields {
		if field.PrimaryKey && field.Name != "" {
			names = append(names, field.Name)
		}
	}
	return names
}
