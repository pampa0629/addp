package shapefile

// This file demonstrates how a third-party plugin registers itself with ADDP.
//
// Usage in ADDP Meta module:
//
// In meta/backend/internal/scanner/extractors/init.go, add:
//
//	import (
//	    _ "github.com/example/addp-shapefile-extractor"
//	)
//
// The underscore import will trigger the init() function below,
// which automatically registers the extractor.

import (
	sdk "github.com/addp/meta-extractor-sdk"
)

func init() {
	// Register the shapefile extractor when this package is imported
	// This is the magic that makes third-party plugins work!
	RegisterShapefileExtractor()
}

// RegisterShapefileExtractor registers the shapefile extractor
// This function can also be called explicitly if you prefer manual registration
func RegisterShapefileExtractor() {
	// Note: This assumes ADDP's scanner package exports a Register function
	// that accepts sdk.MetadataExtractor interface
	//
	// In ADDP's code, the Register function would look like:
	//
	// func Register(extractor interface{}) {
	//     if e, ok := extractor.(MetadataExtractor); ok {
	//         defaultRegistry.Register(e)
	//     }
	// }

	// For this example, we'll note that the ADDP codebase needs to provide
	// a bridge function. See the documentation for details.

	// Placeholder - actual registration happens through ADDP's scanner.Register()
	// scanner.Register(&ShapefileExtractor{})
}

// GetExtractor returns a new instance of the shapefile extractor
// This is useful for programmatic registration
func GetExtractor() sdk.MetadataExtractor {
	return &ShapefileExtractor{}
}
