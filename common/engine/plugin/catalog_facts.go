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
