package service

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/models"
)

type storageModelCheck func(context.Context, string, commonClient.ManagerModel3DExpectedSource) error

func (f storageModelCheck) CheckManagerModel3DRead(ctx context.Context, token string, expected commonClient.ManagerModel3DExpectedSource) error {
	return f(ctx, token, expected)
}

type storageModelIO struct {
	*downloadTestFilePlugin
	stats, opens int
	reportedSize *int64
}

func (p *storageModelIO) DescribeEngineCatalogFacts(ctx context.Context, info plugin.ConnectionInfo, path plugin.EngineCatalogPath, options plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	p.stats++
	if info["identity"] != "same-version" {
		return nil, errors.New("connection changed after source decision")
	}
	facts, err := p.downloadTestFilePlugin.DescribeEngineCatalogFacts(ctx, info, path, options)
	if facts != nil && facts.Storage != nil && p.reportedSize != nil {
		facts.Storage.SizeBytes = p.reportedSize
	}
	return facts, err
}
func (p *storageModelIO) OpenContent(ctx context.Context, info plugin.ConnectionInfo, path plugin.EngineCatalogPath, options plugin.ReadOptions) (io.ReadCloser, error) {
	p.opens++
	if info["identity"] != "same-version" {
		return nil, errors.New("connection changed after source decision")
	}
	return p.downloadTestFilePlugin.OpenContent(ctx, info, path, options)
}

func storageModelFixture(t *testing.T, object bool, requestedKind ...string) (*MetadataService, *StorageDataItem, *storageModelIO, *uint, *models.DownloadPlan) {
	t.Helper()
	kind, engineType, name := "file", "nfs", "models/ exact %.ifc"
	if object {
		kind, engineType, name = "object", "minio", "bucket/"+name
	}
	if len(requestedKind) > 0 {
		engineType = requestedKind[0]
	}
	previous, previousErr := plugin.Get(engineType)
	p := &storageModelIO{downloadTestFilePlugin: newDownloadTestFilePlugin(engineType, map[string]string{name: "original IFC bytes"})}
	p.objectCatalog = object
	plugin.Register(p)
	t.Cleanup(func() {
		if previousErr == nil {
			plugin.Register(previous)
		} else {
			plugin.Unregister(engineType)
		}
	})
	tenant := uint(11)
	attrs := commonModels.JSONMap{"item": map[string]interface{}{"layout": "single", "data_type": "model_3d", "format": "ifc", "refs": []interface{}{map[string]interface{}{"path": name, "role": "model", "primary": true, "required": true}}}, "storage": map[string]interface{}{"physical_path": name}}
	item := &commonModels.MetaItem{ID: 1, TenantID: tenant, EngineID: 9, ItemType: kind, Name: " exact %.ifc", FullName: name, Fingerprint: commonModels.GenerateItemFingerprint(9, name), Attributes: attrs}
	resource := &models.Engine{ID: 9, Version: 7, EngineType: engineType, TenantID: &tenant, LifecycleState: "active", ConnectionStatus: "online", ConnectionInfo: models.ConnectionInfo{"identity": "same-version"}}
	target := &StorageDataItem{EngineID: 9, EngineType: engineType, StorageRef: name, ItemFingerprint: item.Fingerprint, Descriptor: dataitem.ItemDescriptor{Layout: format.LayoutSingle, DataType: datatype.Model3D, Format: "ifc"}, engine: resource, item: item, locator: resourcetree.LocatorFromFullName(9, engineType, kind, name, &item.ID).ToURI()}
	plan := &models.DownloadPlan{Kind: models.DownloadKindStream, Refs: []models.DownloadRef{{StorageRef: name, Role: "model", Required: true, Primary: true}}}
	return &MetadataService{}, target, p, &tenant, plan
}

func TestModelRawReadCurrentCredentialAndRulesBeforeSourceIO(t *testing.T) {
	for _, object := range []bool{false, true} {
		for _, scenario := range []string{"allow", "no current credential", "ticket", "service", "denied", "unavailable", "revoked before open", "engine changed", "unsupported format", "changed composition", "extra download ref"} {
			t.Run(map[bool]string{false: "file", true: "object"}[object]+"/"+scenario, func(t *testing.T) {
				svc, target, provider, tenant, plan := storageModelFixture(t, object)
				calls := 0
				svc.SetModelSourceReadChecker(storageModelCheck(func(_ context.Context, token string, expected commonClient.ManagerModel3DExpectedSource) error {
					calls++
					if token != "addp_at_current_user" || expected.EngineVersion != "7" || expected.FileComposition.Format != "ifc" || len(expected.FileComposition.Refs) != 1 || expected.FileComposition.Refs[0].Target.StringPath() != target.StorageRef {
						t.Fatal("lost exact request identity or expected source")
					}
					if scenario == "denied" || scenario == "engine changed" || (scenario == "revoked before open" && calls == 2) {
						return commonClient.ErrManagerPreviewReadDenied
					}
					if scenario == "unavailable" {
						return commonClient.ErrManagerPreviewReadUnavailable
					}
					return nil
				}))
				token, human := "addp_at_current_user", true
				switch scenario {
				case "no current credential":
					token = ""
				case "ticket":
					token = "addp_rt_ticket"
				case "service":
					human = false
				case "unsupported format":
					target.Descriptor.Format = "skp"
				case "changed composition":
					target.item.Attributes["item"].(map[string]interface{})["refs"] = []interface{}{map[string]interface{}{"path": target.StorageRef, "role": "model", "required": true, "primary": true}, map[string]interface{}{"path": "models/unapproved.png", "role": "texture", "required": true}}
				case "extra download ref":
					plan.Refs = append(plan.Refs, models.DownloadRef{StorageRef: "models/unapproved.png", Role: "texture", Required: true})
				}
				reader, err := svc.OpenStorageDownloadPlan(t.Context(), target, plan, tenant, token, human)
				if scenario == "allow" {
					if err != nil {
						t.Fatal(err)
					}
					defer reader.Close()
					raw, err := io.ReadAll(reader)
					if err != nil || string(raw) != "original IFC bytes" || calls != 2 || provider.stats != 1 || provider.opens != 1 {
						t.Fatalf("raw=%q error=%v checks=%d stats=%d opens=%d", raw, err, calls, provider.stats, provider.opens)
					}
					return
				}
				if err == nil || reader != nil || provider.opens != 0 {
					t.Fatalf("denied path opened source: error=%v stats=%d opens=%d", err, provider.stats, provider.opens)
				}
				expectedStats := 0
				if scenario == "revoked before open" {
					expectedStats = 1
				}
				if provider.stats != expectedStats {
					t.Fatalf("facts read before permission: %d", provider.stats)
				}
			})
		}
	}
}

func TestModelRawReadCannotSubstituteAnotherConstituent(t *testing.T) {
	svc, target, provider, tenant, _ := storageModelFixture(t, false)
	svc.SetModelSourceReadChecker(storageModelCheck(func(context.Context, string, commonClient.ManagerModel3DExpectedSource) error {
		t.Fatal("out-of-composition ref reached System")
		return nil
	}))
	target.StorageRef = "models/neighbor.ifc"
	reader, _, _, _, err := svc.StreamStorageContent(t.Context(), target, "", tenant, "addp_at_current_user", true)
	if err == nil || reader != nil || provider.stats != 0 || provider.opens != 0 {
		t.Fatal("neighbor raw content was opened")
	}
}

func TestModelRawReadRechecksEveryRangeRequest(t *testing.T) {
	svc, target, _, tenant, _ := storageModelFixture(t, false)
	denied := false
	var scopes []commonClient.ManagerModel3DExpectedSource
	svc.SetModelSourceReadChecker(storageModelCheck(func(_ context.Context, _ string, expected commonClient.ManagerModel3DExpectedSource) error {
		scopes = append(scopes, expected)
		if denied {
			return commonClient.ErrManagerPreviewReadDenied
		}
		return nil
	}))
	r, _, _, _, err := svc.StreamStorageContent(t.Context(), target, "bytes=0-3", tenant, "addp_at_current_user", true)
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	denied = true
	r, _, _, _, err = svc.StreamStorageContent(t.Context(), target, "bytes=4-7", tenant, "addp_at_current_user", true)
	if !errors.Is(err, commonClient.ErrManagerPreviewReadDenied) || r != nil || len(scopes) != 3 || !reflect.DeepEqual(scopes[0], scopes[2]) {
		t.Fatal("range request reused a prior Allow")
	}
}
