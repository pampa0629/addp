package scanruntime

import (
	"context"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaquery"
)

func (s *DatabaseRuntime) describeTableFacts(
	ctx context.Context,
	resource *commonModels.Engine,
	scanCatalog databaseScanCatalog,
	schemaName string,
	tableName string,
) (*plugin.CatalogFacts, error) {
	path := plugin.TabularItemPath(resource.ID, scanCatalog.namespaceTerm, schemaName, tableName)
	item, err := scanCatalog.factsProvider.DescribeCatalogFacts(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), path, plugin.CatalogFactsOptions{IncludeSpatialFacts: true})
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
	if described.SizeBytes == nil {
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
	described.Fields = normalizeDatabaseFields(described.Fields)
	if len(described.PrimaryKey) == 0 {
		described.PrimaryKey = primaryKeyFieldNames(described.Fields)
	}
	return described
}

func normalizeDatabaseFields(input []datatype.FieldInfo) []datatype.FieldInfo {
	fields := make([]datatype.FieldInfo, 0, len(input))
	for _, field := range input {
		nativeType := field.NativeType
		if nativeType == "" {
			nativeType = string(field.Type)
		}
		field.NativeType = nativeType
		field.Type = datatype.FieldType(metaquery.StandardizeFieldType(nativeType, string(field.Type)))
		fields = append(fields, field)
	}
	return fields
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
