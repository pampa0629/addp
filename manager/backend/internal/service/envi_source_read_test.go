package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"path"
	"testing"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/envi"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/models"
)

func TestENVIRawPairRequiresCurrentCompleteGrantAndHeaderProof(t *testing.T) {
	for _, object := range []bool{false, true} {
		for _, scenario := range []string{"allow", "denied", "changed header", "changed data size", "partial download", "outside ref", "incomplete proof"} {
			t.Run(map[bool]string{false: "nfs", true: "minio"}[object]+"/"+scenario, func(t *testing.T) {
				svc, target, provider, tenant, _ := storageModelFixture(t, object)
				main := "imagery/ 原名 %.img"
				header := "imagery/ 原名 %.HDR"
				if object {
					main = "bucket/" + main
					header = "bucket/" + header
				}
				rawHeader := "ENVI\nsamples = 4\nlines = 1\nbands = 1\nheader offset = 0\nfile type = ENVI Standard\ndata type = 1\ninterleave = bsq\nbyte order = 0\n"
				digest := sha256.Sum256([]byte(rawHeader))
				provider.files = map[string]string{main: "\x01\x02\x03\x04", header: rawHeader, "imagery/secret": "never read"}
				refs := []interface{}{map[string]interface{}{"path": main, "role": "raster", "required": true, "primary": true}, map[string]interface{}{"path": header, "role": "header", "required": true, "primary": false}}
				target.item.Name = path.Base(main)
				target.item.FullName = main
				target.item.Fingerprint = commonModels.GenerateItemFingerprint(target.EngineID, main)
				target.item.Attributes = map[string]interface{}{"item": map[string]interface{}{"layout": "multi", "data_type": "media", "format": "envi", "refs": refs}, "storage": map[string]interface{}{"physical_path": main}, "format_info": map[string]interface{}{"envi": map[string]interface{}{"profile": envi.Profile, "composition_complete": true, "header_path": header, "header_sha256": hex.EncodeToString(digest[:])}}}
				target.StorageRef = main
				target.ItemFingerprint = target.item.Fingerprint
				target.Descriptor = dataitem.ItemDescriptor{Layout: format.LayoutMulti, DataType: datatype.Media, Format: "envi"}
				target.locator = resourcetree.LocatorFromFullName(target.EngineID, target.EngineType, target.item.ItemType, main, &target.item.ID).ToURI()
				plan := &models.DownloadPlan{Refs: []models.DownloadRef{{StorageRef: main, Role: "raster", Required: true, Primary: true}, {StorageRef: header, Role: "header", Required: true}}}
				calls := 0
				svc.SetFileSourceReadChecker(storageModelCheck(func(_ context.Context, token string, expected commonClient.ManagerFileExpectedSource) error {
					calls++
					if token != "addp_at_current_user" || expected.FileComposition.Format != "envi" || len(expected.FileComposition.Refs) != 2 {
						t.Fatal("incomplete ENVI authorization request")
					}
					if scenario == "denied" {
						return commonClient.ErrManagerPreviewReadDenied
					}
					return nil
				}))
				if scenario == "incomplete proof" {
					target.item.Attributes["format_info"].(map[string]interface{})["envi"].(map[string]interface{})["composition_complete"] = false
					target.item.Attributes["item"].(map[string]interface{})["refs"] = refs[:1]
				}
				if scenario == "partial download" {
					plan.Refs = plan.Refs[:1]
				}
				read, err := svc.prepareStorageRead(t.Context(), target, tenant, "addp_at_current_user", true, plan)
				if scenario == "partial download" || scenario == "incomplete proof" {
					if err == nil {
						t.Fatal("partial file license became complete ENVI read")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "changed header" {
					provider.files[header] += "description = {changed}\n"
				}
				if scenario == "changed data size" {
					provider.files[main] = "short"
				}
				request := main
				if scenario == "outside ref" {
					request = "imagery/secret"
				}
				stream, _, _, _, err := read.stream(t.Context(), request, "")
				if scenario != "allow" {
					if stream != nil {
						stream.Close()
					}
					if err == nil {
						t.Fatal("unproven ENVI content returned")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				bytes, err := io.ReadAll(stream)
				stream.Close()
				if err != nil || string(bytes) != provider.files[main] || calls < 4 {
					t.Fatal("original ENVI bytes or current permission checkpoints missing", err, calls)
				}
				stream, _, _, _, err = read.stream(t.Context(), header, "bytes=0-3")
				if err != nil {
					t.Fatal(err)
				}
				bytes, err = io.ReadAll(stream)
				stream.Close()
				if err != nil || string(bytes) != "ENVI" {
					t.Fatal("header Range changed original bytes", err)
				}
			})
		}
	}
}
