package scanflow

import "github.com/addp/common/engine/plugin"

type CatalogScanStrategy string

const (
	CatalogScanTabular      CatalogScanStrategy = "tabular"
	CatalogScanDirectLeaves CatalogScanStrategy = "direct_leaves"
	CatalogScanBranchLeaves CatalogScanStrategy = "branch_leaves"
	CatalogScanObject       CatalogScanStrategy = "object_catalog"
	CatalogScanFile         CatalogScanStrategy = "file_catalog"
)

type CatalogScanPlan struct {
	Strategy   CatalogScanStrategy
	Model      plugin.CatalogModelSpec
	BranchTerm string
	LeafTerm   string
}

func CatalogScanPlanForPlugin(p plugin.EnginePlugin) (CatalogScanPlan, bool) {
	model := CatalogModelForPlugin(p)
	if model == nil {
		return CatalogScanPlan{}, false
	}

	leafTerm := plugin.CatalogLeafTerm(*model)
	plan := CatalogScanPlan{
		Model:    *model,
		LeafTerm: leafTerm,
	}
	if len(model.Levels) == 1 && model.Levels[0].Role == plugin.CatalogRoleLeaf {
		plan.Strategy = CatalogScanDirectLeaves
		return plan, true
	}
	switch leafTerm {
	case plugin.CatalogTermTable:
		plan.Strategy = CatalogScanTabular
		plan.BranchTerm = NamespaceTermForPlugin(p)
	case plugin.CatalogTermCollection, plugin.CatalogTermGraph:
		plan.Strategy = CatalogScanBranchLeaves
		plan.BranchTerm = FirstBusinessBranchTermForPlugin(p)
	case plugin.CatalogTermObject:
		plan.Strategy = CatalogScanObject
	case plugin.CatalogTermFile:
		plan.Strategy = CatalogScanFile
	default:
		return CatalogScanPlan{}, false
	}
	return plan, true
}
