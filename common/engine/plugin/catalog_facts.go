package plugin

import "github.com/addp/common/datatype"

// CatalogFactsTableInfo returns table facts for a table-shaped catalog entry.
func CatalogFactsTableInfo(facts *CatalogFacts) *datatype.TableInfo {
	if facts == nil {
		return nil
	}
	if facts.Table != nil {
		return facts.Table.Clone()
	}
	return nil
}

// CatalogEntryTableInfo returns the table-shaped summary allowed on CatalogEntry.
// Field-level and key-level details belong to CatalogFacts, not listing entries.
func CatalogEntryTableInfo(facts *CatalogFacts) *datatype.TableInfo {
	if facts == nil {
		return nil
	}
	return CatalogEntryTableSummary(facts.Table)
}

// CatalogEntryTableSummary projects full table facts to the list-level summary.
func CatalogEntryTableSummary(table *datatype.TableInfo) *datatype.TableInfo {
	if table == nil {
		return nil
	}
	summary := table.Clone()
	summary.Fields = nil
	summary.PrimaryKey = nil
	return summary
}

// CatalogEntryStorageInfo returns the storage summary allowed on CatalogEntry.
func CatalogEntryStorageInfo(facts *CatalogFacts) *CatalogStorageFacts {
	if facts == nil {
		return nil
	}
	return CatalogEntryStorageSummary(facts.Storage)
}

// CatalogEntryStorageSummary projects full storage facts to the list-level summary.
func CatalogEntryStorageSummary(storage *CatalogStorageFacts) *CatalogStorageFacts {
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

// CatalogFactsGraphInfo returns graph facts for a graph-shaped catalog entry.
func CatalogFactsGraphInfo(facts *CatalogFacts) *datatype.GraphInfo {
	if facts == nil {
		return nil
	}
	if facts.Graph != nil {
		return facts.Graph.Clone()
	}
	return nil
}
