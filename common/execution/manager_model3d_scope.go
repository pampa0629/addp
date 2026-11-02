package execution

import "github.com/addp/common/dataitem/authorizationfacts"

const ManagerModel3DConfigVersion = "model3d-glb-file-config/v1"

// ManagerModel3DSourceEngineType restricts the admitted source adapters.
func ManagerModel3DSourceEngineType(engineType string) bool {
	return ManagerFileSourceEngineType(engineType)
}

// ManagerModel3DSourceFormat admits exact-composition reads. Conversion has a narrower boundary.
func ManagerModel3DSourceFormat(engineType, format string) bool {
	return ManagerModel3DSourceEngineType(engineType) && ManagerModel3DFileFormat(format)
}

// ManagerModel3DFileFormat is the shared exact-composition format admission.
// Identity, basic completeness and the native file/object shape remain mandatory.
func ManagerModel3DFileFormat(format string) bool {
	return (&authorizationfacts.FileCompositionBoundary{Format: format}).DataType() == "model_3d"
}

// ManagerModel3DConversionFormat excludes directly previewed GLB from execution authorization.
func ManagerModel3DConversionFormat(format string) bool {
	return ManagerModel3DFileFormat(format) && format != "glb"
}

// ManagerModel3DSourceComposition also proves the native catalog shape. Engine
// identity alone must never let a file scope authorize an object or vice versa.
func ManagerModel3DSourceComposition(engineType string, composition *authorizationfacts.FileCompositionBoundary) bool {
	if composition == nil || composition.DataType() != "model_3d" || !ManagerFileSourceComposition(engineType, composition) {
		return false
	}
	return (composition.Format != "glb" && composition.Format != "fbx") || composition.Refs[0].Role == "model"
}
