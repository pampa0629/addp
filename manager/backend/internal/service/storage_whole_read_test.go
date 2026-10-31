package service

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
)

type storageWholeCheck func(context.Context, string, []plugin.EngineCatalogPath) error

func (f storageWholeCheck) CheckManagerPreviewRead(ctx context.Context, token string, targets []plugin.EngineCatalogPath) error {
	return f(ctx, token, targets)
}

func storageWholeFixture(t *testing.T) (*MetadataService, *StorageDataItem, *storageModelIO, *uint) {
	t.Helper()
	svc, target, provider, tenant, _ := storageModelFixture(t, false)
	root := "models/sample.gdb"
	target.Descriptor.Layout = format.LayoutWhole
	target.Descriptor.DataType = datatype.Container
	target.Descriptor.Format = "filegdb"
	target.StorageRef = root + "/a00000001.gdbtable"
	target.item.FullName = root
	target.ItemFingerprint = commonModels.GenerateItemFingerprint(9, root)
	target.item.Fingerprint = target.ItemFingerprint
	target.item.Attributes = commonModels.JSONMap{"item": map[string]interface{}{"layout": "whole", "data_type": "container", "format": "filegdb"}, "storage": map[string]interface{}{"physical_path": root}}
	target.locator = resourcetree.LocatorFromFullName(9, "nfs", "file", root, &target.item.ID).ToURI()
	provider.files = map[string]string{target.StorageRef: "synthetic FileGDB member", root + "/new.gdbtable": "new member"}
	return svc, target, provider, tenant
}

func TestWholeRawReadCurrentUserAndRulesBeforeSourceIO(t *testing.T) {
	for _, scenario := range []string{"allow", "missing checker", "no credential", "ticket", "service", "denied", "unavailable", "revoked before open", "wrong fingerprint", "wrong locator", "wrong tenant", "wrong layout", "other engine", "neighbor", "traversal"} {
		t.Run(scenario, func(t *testing.T) {
			svc, target, provider, tenant := storageWholeFixture(t)
			calls := 0
			svc.SetWholeSourceReadChecker(storageWholeCheck(func(_ context.Context, token string, targets []plugin.EngineCatalogPath) error {
				calls++
				if token != "addp_at_current_user" || !reflect.DeepEqual(targets, []plugin.EngineCatalogPath{plugin.FileDirectoryPath(9, "models/sample.gdb")}) {
					t.Fatal("lost current User or whole root identity")
				}
				if scenario == "denied" || (scenario == "revoked before open" && calls == 2) {
					return commonClient.ErrManagerPreviewReadDenied
				}
				if scenario == "unavailable" {
					return commonClient.ErrManagerPreviewReadUnavailable
				}
				return nil
			}))
			token, human := "addp_at_current_user", true
			switch scenario {
			case "missing checker":
				svc.SetWholeSourceReadChecker(nil)
			case "no credential":
				token = ""
			case "ticket":
				token = "addp_rt_ticket"
			case "service":
				human = false
			case "wrong fingerprint":
				target.ItemFingerprint = "another item"
			case "wrong locator":
				target.locator = resourcetree.LocatorFromFullName(9, "nfs", "file", "models/other.gdb", &target.item.ID).ToURI()
			case "wrong tenant":
				target.item.TenantID++
			case "wrong layout":
				target.Descriptor.Layout = format.LayoutSingle
			case "other engine":
				target.EngineType = "ftp"
			case "neighbor":
				target.StorageRef = "models/sample.gdb-other/member"
			case "traversal":
				target.StorageRef = "models/sample.gdb/../neighbor"
			}
			reader, _, _, _, err := svc.StreamStorageContent(t.Context(), target, "", tenant, token, human)
			if scenario == "allow" {
				if err != nil {
					t.Fatal(err)
				}
				raw, readErr := io.ReadAll(reader)
				closeErr := reader.Close()
				if readErr != nil || closeErr != nil || string(raw) != "synthetic FileGDB member" || calls != 2 || provider.stats != 1 || provider.opens != 1 {
					t.Fatalf("raw=%q error=%v checks=%d stats=%d opens=%d", raw, readErr, calls, provider.stats, provider.opens)
				}
				return
			}
			expectedStats := 0
			if scenario == "revoked before open" {
				expectedStats = 1
			}
			if err == nil || reader != nil || provider.stats != expectedStats || provider.opens != 0 {
				t.Fatalf("denied request reached source: error=%v checks=%d stats=%d opens=%d", err, calls, provider.stats, provider.opens)
			}
		})
	}
}

func TestWholeRawReadNewMemberAndRangeRecheckSameRoot(t *testing.T) {
	svc, target, provider, tenant := storageWholeFixture(t)
	denied, calls := false, 0
	svc.SetWholeSourceReadChecker(storageWholeCheck(func(_ context.Context, _ string, targets []plugin.EngineCatalogPath) error {
		calls++
		if !reflect.DeepEqual(targets, []plugin.EngineCatalogPath{plugin.FileDirectoryPath(9, "models/sample.gdb")}) {
			t.Fatal("member changed authorization identity")
		}
		if denied {
			return commonClient.ErrManagerPreviewReadDenied
		}
		return nil
	}))
	for _, member := range []string{target.StorageRef, "models/sample.gdb/new.gdbtable"} {
		target.StorageRef = member
		r, _, _, _, err := svc.StreamStorageContent(t.Context(), target, "bytes=0-2", tenant, "addp_at_current_user", true)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.ReadAll(r); err != nil {
			t.Fatal(err)
		}
		r.Close()
	}
	denied = true
	r, _, _, _, err := svc.StreamStorageContent(t.Context(), target, "bytes=3-4", tenant, "addp_at_current_user", true)
	if !errors.Is(err, commonClient.ErrManagerPreviewReadDenied) || r != nil || calls != 5 || provider.stats != 2 || provider.opens != 2 {
		t.Fatalf("Range reused prior Allow: error=%v checks=%d stats=%d opens=%d", err, calls, provider.stats, provider.opens)
	}
}
