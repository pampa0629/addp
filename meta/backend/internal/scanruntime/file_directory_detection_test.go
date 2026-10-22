package scanruntime

import (
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/nfs"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/scanflow"
	"testing"
)

func TestModelStructureReadIsBasicForAdmittedFileSourcesOnly(t *testing.T) {
	var reader plugin.ContentReadableProvider = &nfs.NFSPlugin{}
	for _, kind := range []string{"ftp", "nfs", "minio", "", "NFS"} {
		for _, depth := range []string{"basic", "deep", "", "none"} {
			got := modelStructureReader(&commonModels.Engine{EngineType: kind}, reader, depth)
			want := (kind == "ftp" || kind == "nfs" || kind == "minio") && (depth == "basic" || depth == "deep")
			if (got != nil) != want {
				t.Fatalf("kind=%q depth=%q reader=%v", kind, depth, got)
			}
		}
	}
	if modelStructureReader(nil, reader, "basic") != nil {
		t.Fatal("missing engine admitted")
	}
	if scanflow.ContentReaderForDepth(reader, "basic") != nil {
		t.Fatal("basic enabled deep summary reads")
	}
}
