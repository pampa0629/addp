package execution

import (
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
)

func ManagerFileSourceEngineType(engineType string) bool {
	return engineType == "nfs" || engineType == "minio" || engineType == "ftp"
}

// ManagerFileSourceComposition is the current exact-file read admission. The
// model conversion purpose keeps its narrower, independent task contract.
func ManagerFileSourceComposition(engineType string, composition *authorizationfacts.FileCompositionBoundary) bool {
	if composition == nil || composition.DataType() == "" {
		return false
	}
	if engineType != "nfs" && engineType != "minio" && (engineType != "ftp" || composition.DataType() != "model_3d") {
		return false
	}
	if (composition.Format == "ifc" || composition.Format == "stl" || composition.Format == "glb" || composition.Format == "fbx") && len(composition.Refs) != 1 {
		return false
	}
	primary, err := composition.PrimaryTarget()
	if err != nil {
		return false
	}
	term := primary.Segments[len(primary.Segments)-1].Term
	return (engineType == "minio" && term == plugin.EngineCatalogTermObject) || ((engineType == "ftp" || engineType == "nfs") && term == plugin.EngineCatalogTermFile)
}
