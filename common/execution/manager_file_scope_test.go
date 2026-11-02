package execution

import (
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
)

func TestENVIReadAdmissionDoesNotEnableModelConversionOrOtherEngines(t *testing.T) {
	for _, kind := range []string{"nfs", "minio", "ftp", "s3", "localfs"} {
		main, header := plugin.FileItemPath(14, "imagery/grid.bin"), plugin.FileItemPath(14, "imagery/grid.hdr")
		if kind == "minio" {
			main = plugin.ObjectItemPath(14, "bucket", "imagery/grid.bin")
			header = plugin.ObjectItemPath(14, "bucket", "imagery/grid.hdr")
		}
		pair, err := authorizationfacts.NewFileCompositionBoundary("envi", []authorizationfacts.FileCompositionRef{{Target: main, Role: "raster", Required: true, Primary: true}, {Target: header, Role: "header", Required: true}})
		if err != nil {
			t.Fatal(err)
		}
		if got := ManagerFileSourceComposition(kind, pair); got != (kind == "nfs" || kind == "minio") {
			t.Fatalf("unexpected ENVI engine admission %s: %v", kind, got)
		}
		if ManagerModel3DSourceComposition(kind, pair) || ManagerModel3DFileFormat("envi") || ManagerModel3DConversionFormat("envi") {
			t.Fatal("ENVI escaped into model conversion")
		}
	}
	if ManagerModel3DFileFormat("skp") || ManagerModel3DConversionFormat("skp") {
		t.Fatal("SKP exact composition admission was reopened")
	}
}
