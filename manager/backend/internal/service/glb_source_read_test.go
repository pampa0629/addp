package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"io"
	"path"
	"testing"
)

func sourceReadGLB(doc string) []byte {
	raw := []byte(doc)
	for len(raw)%4 != 0 {
		raw = append(raw, ' ')
	}
	out := []byte{'g', 'l', 'T', 'F'}
	out = binary.LittleEndian.AppendUint32(out, 2)
	out = binary.LittleEndian.AppendUint32(out, uint32(20+len(raw)))
	out = binary.LittleEndian.AppendUint32(out, uint32(len(raw)))
	out = binary.LittleEndian.AppendUint32(out, 0x4e4f534a)
	return append(out, raw...)
}
func TestGLBRawReadValidatesSameWholeBytesBeforeRangeOrDownload(t *testing.T) {
	for _, kind := range []string{"ftp", "nfs", "minio"} {
		for _, mode := range []string{"full", "range", "download", "invalid content", "external URI", "revoked after read", "incomplete basic", "extra constituent", "bad range", "oversize facts"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				svc, target, p, tenant, plan := storageModelFixture(t, kind == "minio", kind)

				name := path.Dir(target.StorageRef) + "/ exact %.GLB"
				target.StorageRef = name
				target.Descriptor.Format = "glb"
				target.item.FullName = name
				target.item.Name = path.Base(name)
				target.item.Fingerprint = commonModels.GenerateItemFingerprint(target.EngineID, name)
				target.ItemFingerprint = target.item.Fingerprint
				target.locator = resourcetree.LocatorFromFullName(target.EngineID, target.EngineType, target.item.ItemType, name, &target.item.ID).ToURI()
				attrs := target.item.Attributes
				attrs["storage"].(map[string]interface{})["physical_path"] = name
				attrs["item"].(map[string]interface{})["format"] = "glb"
				refs := attrs["item"].(map[string]interface{})["refs"].([]interface{})
				refs[0].(map[string]interface{})["path"] = name
				attrs["format_info"] = map[string]interface{}{"glb": map[string]interface{}{"composition_complete": mode != "incomplete basic"}}
				if mode == "extra constituent" {
					attrs["item"].(map[string]interface{})["refs"] = append(refs, map[string]interface{}{"path": path.Dir(name) + "/image.png", "role": "texture", "required": true})
				}
				plan.Refs[0].StorageRef = name
				raw := sourceReadGLB(`{"asset":{"version":"2.0"}}`)
				if mode == "external URI" {
					raw = sourceReadGLB(`{"asset":{"version":"2.0"},"images":[{"uri":"https://example.invalid/secret"}]}`)
				}
				if mode == "invalid content" {
					raw = []byte("invalid GLB but complete old metadata")
				}
				p.files[name] = string(raw)
				if mode == "oversize facts" {
					size := int64(64<<20) + 1
					p.reportedSize = &size
				}
				checks := 0
				svc.SetModelSourceReadChecker(storageModelCheck(func(context.Context, string, commonClient.ManagerModel3DExpectedSource) error {
					checks++
					if mode == "revoked after read" && checks == 3 {
						return commonClient.ErrManagerPreviewReadDenied
					}
					return nil
				}))
				var reader io.ReadCloser
				var err error
				var size int64
				var cr string
				if mode == "download" {
					reader, err = svc.OpenStorageDownloadPlan(t.Context(), target, plan, tenant, "addp_at_current_user", true)
				} else {
					header := ""
					if mode == "range" {
						header = "bytes=4-7"
					}
					if mode == "bad range" {
						header = "bytes=9999-"
					}
					reader, size, cr, _, err = svc.StreamStorageContent(t.Context(), target, header, tenant, "addp_at_current_user", true)
				}
				if mode != "full" && mode != "range" && mode != "download" {
					want := commonClient.ErrManagerPreviewReadDenied
					if mode == "bad range" {
						want = ErrInvalidRange
					}
					if mode == "oversize facts" && p.opens != 0 {
						t.Fatal("over-budget source opened")
					}
					if !errors.Is(err, want) || reader != nil {
						t.Fatalf("rejected read returned bytes: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				got, e := io.ReadAll(reader)
				reader.Close()
				if e != nil {
					t.Fatal(e)
				}
				want := raw
				if mode == "range" {
					want = raw[4:8]
					if size != 4 || cr == "" {
						t.Fatal("invalid range headers")
					}
				}
				if !bytes.Equal(got, want) || p.opens != 1 || checks != 3 {
					t.Fatalf("not the same validated bytes: checks=%d opens=%d", checks, p.opens)
				}
				if managedModel3DFileSource(kind, "glb") || isModel3DGLBTaskSourceFormat("glb") {
					t.Fatal("direct GLB acquired conversion")
				}
			})
		}
	}
}
