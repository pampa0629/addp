// Package plugins imports third-party metadata extractor plugins.
//
// To add a new third-party plugin:
// 1. Add the plugin import below (with underscore for side-effect only)
// 2. The plugin's init() function will automatically register the extractor
//
// Example:
//   import _ "github.com/example/my-custom-extractor"
package plugins

import (
	"github.com/addp/meta/internal/scanner"
	sdk "github.com/addp/meta-extractor-sdk"

	// Import third-party plugins here
	// Each plugin should provide a RegisterExtractor() function or use init()

	// For local development/testing, import the local plugins
	shapefile "github.com/addp/plugins/shapefile-extractor"
	video "github.com/addp/plugins/video-extractor"
	pdf "github.com/addp/plugins/pdf-extractor"
	image "github.com/addp/plugins/image-extractor"
	csv "github.com/addp/plugins/csv-extractor"
	geojson "github.com/addp/plugins/geojson-extractor"
	sqlite "github.com/addp/plugins/sqlite-extractor"
	office "github.com/addp/plugins/office-extractor"
)

func init() {
	// Manually register plugins that don't use init() auto-registration
	// This is useful for plugins that export a GetExtractor() function

	// Register shapefile extractor
	scanner.RegisterSDKExtractor(shapefile.GetExtractor())

	// Register video extractor
	scanner.RegisterSDKExtractor(video.GetExtractor())

	// Register PDF extractor
	scanner.RegisterSDKExtractor(pdf.GetExtractor())

	// Register image extractor
	scanner.RegisterSDKExtractor(image.GetExtractor())

	// Register CSV extractor
	scanner.RegisterSDKExtractor(csv.GetExtractor())

	// Register GeoJSON extractor
	scanner.RegisterSDKExtractor(geojson.GetExtractor())

	// Register SQLite extractor
	scanner.RegisterSDKExtractor(sqlite.GetExtractor())

	// Register Office extractor
	scanner.RegisterSDKExtractor(office.GetExtractor())

	// Register other third-party extractors here
	// Example:
	// scanner.RegisterSDKExtractor(myplugin.GetExtractor())
}

// Helper function to register SDK extractors programmatically
func RegisterExtractor(extractor sdk.MetadataExtractor) {
	scanner.RegisterSDKExtractor(extractor)
}
