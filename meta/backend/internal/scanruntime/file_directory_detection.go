package scanruntime

import (
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/scanflow"
)

// FTP, NFS and MinIO share the admitted model-structure route. Other
// engines and known-item refresh keep their existing depth and scope semantics.
func modelStructureReader(resource *commonModels.Engine, reader plugin.ContentReadableProvider, depth string) plugin.ContentReadableProvider {
	if resource != nil && (resource.EngineType == "ftp" || resource.EngineType == "nfs" || resource.EngineType == "minio") && (depth == scanflow.ScanDepthBasic || depth == scanflow.ScanDepthDeep) {
		return reader
	}
	return nil
}
