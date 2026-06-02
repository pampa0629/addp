package service

import "github.com/addp/common/engine/plugin"

type catalogScanStrategy string

const (
	catalogScanTabular      catalogScanStrategy = "tabular"
	catalogScanBranchLeaves catalogScanStrategy = "branch_leaves"
	catalogScanObject       catalogScanStrategy = "object_catalog"
	catalogScanFile         catalogScanStrategy = "file_catalog"
)

type catalogScanPlan struct {
	strategy   catalogScanStrategy
	model      plugin.CatalogModelSpec
	branchTerm string
	leafTerm   string
}

func catalogScanPlanForPlugin(p plugin.EnginePlugin) (catalogScanPlan, bool) {
	model := catalogModelForPlugin(p)
	if model == nil {
		return catalogScanPlan{}, false
	}

	leafTerm := plugin.CatalogLeafTerm(*model)
	plan := catalogScanPlan{
		model:    *model,
		leafTerm: leafTerm,
	}
	switch leafTerm {
	case plugin.CatalogTermTable:
		plan.strategy = catalogScanTabular
		// Tabular first branch is the schema/database namespace.
		plan.branchTerm = namespaceTermForPlugin(p)
	case plugin.CatalogTermCollection, plugin.CatalogTermGraph:
		plan.strategy = catalogScanBranchLeaves
		plan.branchTerm = firstBusinessBranchTermForPlugin(p)
	case plugin.CatalogTermObject:
		plan.strategy = catalogScanObject
	case plugin.CatalogTermFile:
		plan.strategy = catalogScanFile
	default:
		return catalogScanPlan{}, false
	}
	return plan, true
}
