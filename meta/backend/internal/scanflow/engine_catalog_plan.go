package scanflow

import "github.com/addp/common/engine/plugin"

type EngineCatalogScanStrategy string

const (
	EngineCatalogScanTabular      EngineCatalogScanStrategy = "tabular"
	EngineCatalogScanDirectLeaves EngineCatalogScanStrategy = "direct_leaves"
	EngineCatalogScanBranchLeaves EngineCatalogScanStrategy = "branch_leaves"
	EngineCatalogScanObject       EngineCatalogScanStrategy = "object_catalog"
	EngineCatalogScanFile         EngineCatalogScanStrategy = "file_catalog"
)

type EngineCatalogScanPlan struct {
	Strategy   EngineCatalogScanStrategy
	Model      plugin.EngineCatalogModelSpec
	BranchTerm string
	LeafTerm   string
}

func EngineCatalogScanPlanForPlugin(p plugin.EnginePlugin) (EngineCatalogScanPlan, bool) {
	model := EngineCatalogModelForPlugin(p)
	if model == nil {
		return EngineCatalogScanPlan{}, false
	}

	leafTerm := plugin.EngineCatalogLeafTerm(*model)
	plan := EngineCatalogScanPlan{
		Model:    *model,
		LeafTerm: leafTerm,
	}
	if len(model.Levels) == 1 && model.Levels[0].Role == plugin.EngineCatalogRoleLeaf {
		plan.Strategy = EngineCatalogScanDirectLeaves
		return plan, true
	}
	switch leafTerm {
	case plugin.EngineCatalogTermTable:
		plan.Strategy = EngineCatalogScanTabular
		plan.BranchTerm = NamespaceTermForPlugin(p)
	case plugin.EngineCatalogTermCollection, plugin.EngineCatalogTermGraph:
		plan.Strategy = EngineCatalogScanBranchLeaves
		plan.BranchTerm = FirstBusinessBranchTermForPlugin(p)
	case plugin.EngineCatalogTermObject:
		plan.Strategy = EngineCatalogScanObject
	case plugin.EngineCatalogTermFile:
		plan.Strategy = EngineCatalogScanFile
	default:
		return EngineCatalogScanPlan{}, false
	}
	return plan, true
}
