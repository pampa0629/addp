package plugin

import "github.com/addp/common/datatype"

// EngineCatalogFactsTableInfo returns table facts for a table-shaped catalog entry.
func EngineCatalogFactsTableInfo(facts *EngineCatalogFacts) *datatype.TableInfo {
	if facts == nil {
		return nil
	}
	if facts.Table != nil {
		return facts.Table.Clone()
	}
	return nil
}

// EngineCatalogEntryTableInfo returns the table-shaped summary allowed on EngineCatalogEntry.
// Field-level and key-level details belong to EngineCatalogFacts, not listing entries.
func EngineCatalogEntryTableInfo(facts *EngineCatalogFacts) *datatype.TableInfo {
	if facts == nil {
		return nil
	}
	return EngineCatalogEntryTableSummary(facts.Table)
}

// EngineCatalogEntryTableSummary projects full table facts to the list-level summary.
func EngineCatalogEntryTableSummary(table *datatype.TableInfo) *datatype.TableInfo {
	if table == nil {
		return nil
	}
	summary := table.Clone()
	summary.Fields = nil
	summary.PrimaryKey = nil
	return summary
}

// EngineCatalogEntryStorageInfo returns the storage summary allowed on EngineCatalogEntry.
func EngineCatalogEntryStorageInfo(facts *EngineCatalogFacts) *EngineCatalogStorageFacts {
	if facts == nil {
		return nil
	}
	return EngineCatalogEntryStorageSummary(facts.Storage)
}

// EngineCatalogEntryStorageSummary projects full storage facts to the list-level summary.
func EngineCatalogEntryStorageSummary(storage *EngineCatalogStorageFacts) *EngineCatalogStorageFacts {
	if storage == nil {
		return nil
	}
	summary := *storage
	summary.Name = ""
	summary.Extension = ""
	if storage.SizeBytes != nil {
		sizeBytes := *storage.SizeBytes
		summary.SizeBytes = &sizeBytes
	}
	return &summary
}

// EngineCatalogFactsGraphInfo returns graph facts for a graph-shaped catalog entry.
func EngineCatalogFactsGraphInfo(facts *EngineCatalogFacts) *datatype.GraphInfo {
	if facts == nil {
		return nil
	}
	if facts.Graph != nil {
		return facts.Graph.Clone()
	}
	return nil
}

// EngineCatalogFactsSpatialInfo returns spatial facts for a catalog entry.
func EngineCatalogFactsSpatialInfo(facts *EngineCatalogFacts) *datatype.SpatialInfo {
	if facts == nil {
		return nil
	}
	if facts.Spatial != nil {
		return facts.Spatial.Clone()
	}
	return nil
}
