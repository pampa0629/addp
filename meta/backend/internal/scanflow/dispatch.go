package scanflow

import (
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
)

type DispatchMode string

const (
	DispatchManual DispatchMode = "manual"
	DispatchAuto   DispatchMode = "auto"
)

type DispatchRequest struct {
	Resource     *commonModels.Engine
	EnginePlugin plugin.EnginePlugin
	TenantID     uint
	CatalogPaths []string
	RefGroups    []models.ScanRefGroup
	ScanDepth    string
	Force        bool
	ScanLogID    uint
	Reporter     ProgressReporter
	Mode         DispatchMode
}

type DispatchResult struct {
	CatalogNodes int
	Items        int
	Fields       int
	Extraction   ExtractionCounts
}
