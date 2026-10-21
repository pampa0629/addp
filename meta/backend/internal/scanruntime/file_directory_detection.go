package scanruntime

import (
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/scanflow"
)

// FTP is the only admitted model-structure route in this slice. Other
// engines and known-item refresh keep their existing depth and scope semantics.
func ftpModelStructureReader(resource *commonModels.Engine, reader plugin.ContentReadableProvider, depth string) plugin.ContentReadableProvider {
	if resource != nil && resource.EngineType == "ftp" && (depth == scanflow.ScanDepthBasic || depth == scanflow.ScanDepthDeep) {
		return reader
	}
	return nil
}
