package workflowaccess

import (
	"testing"

	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
)

func TestResolveNFSAndObjectStorePlan(t *testing.T) {
	sourceEngine := &commonModels.Engine{
		ID:         3,
		EngineType: "nfs",
		ConnectionInfo: commonModels.ConnectionInfo{
			"server": "10.0.0.8", "export_path": "/business/nfs/data", "nfs_version": "4",
		},
	}
	targetEngine := &commonModels.Engine{
		ID:         4,
		EngineType: "minio",
		ConnectionInfo: commonModels.ConnectionInfo{
			"endpoint": "http://localhost:19000", "access_key": "key", "secret_key": "secret",
		},
	}
	sourceLocator := &resourcetree.ResourceLocator{EngineID: 3, Type: resourcetree.TypeFile, Path: []string{"pointcloud", "sample.las"}}
	targetParent := &resourcetree.ResourceLocator{EngineID: 4, Type: resourcetree.TypePrefix, Path: []string{"research", "pointcloud"}}
	source, err := ResolveSource(ResourceSpec{Engine: sourceEngine, Locator: sourceLocator, Kind: KindFile, Format: "las"})
	if err != nil {
		t.Fatal(err)
	}
	target, targetLocator, err := ResolveTarget(ResourceSpec{
		Engine: targetEngine, Locator: targetParent, Kind: KindFile, Format: "copc", Name: "result.copc.laz", WriteMode: WriteModeCreate,
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := New(source, target)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Source.Access.Path != "/business/nfs/data/pointcloud/sample.las" {
		t.Fatalf("source path = %q", plan.Source.Access.Path)
	}
	if plan.Source.Access.Server != "10.0.0.8" || plan.Source.Access.ExportPath != "/business/nfs/data" || plan.Source.Access.NFSVersion != "4" {
		t.Fatalf("source NFS access = %#v", plan.Source.Access)
	}
	runtimePlan := plan.JSONMap()
	sourceRuntime := runtimePlan["source"].(commonModels.JSONMap)
	sourceAccess := sourceRuntime["access"].(commonModels.JSONMap)
	if sourceAccess["server"] != "10.0.0.8" || sourceAccess["export_path"] != "/business/nfs/data" || sourceAccess["nfs_version"] != "4" {
		t.Fatalf("runtime source access = %#v", sourceAccess)
	}
	if plan.Target.Access.Bucket != "research" || plan.Target.Access.Object != "pointcloud/result.copc.laz" {
		t.Fatalf("target access = %#v", plan.Target.Access)
	}
	if targetLocator.Type != resourcetree.TypeObject || targetLocator.ToURI() == "" {
		t.Fatalf("target locator = %#v", targetLocator)
	}
	audit := plan.AuditJSONMap()
	targetAudit := audit["target"].(commonModels.JSONMap)
	accessAudit := targetAudit["access"].(commonModels.JSONMap)
	if _, ok := accessAudit["secret_key"]; ok {
		t.Fatalf("audit access leaked secret: %#v", accessAudit)
	}
}

func TestPlanRejectsInvalidWriteMode(t *testing.T) {
	_, err := New(
		Source{Kind: KindFile, Format: "las", Access: Access{Method: MethodMountedPath, Path: "/tmp/a.las"}},
		Target{Kind: KindFile, Format: "copc", Name: "a.copc.laz", WriteMode: "append", Access: Access{Method: MethodMountedPath, Path: "/tmp/a.copc.laz"}},
	)
	if err == nil {
		t.Fatal("expected invalid write_mode error")
	}
}

func TestSourcePlanSupportsReadOnlyDirectOperator(t *testing.T) {
	plan, err := NewSourcePlan(Source{Kind: KindFile, Format: "dwg", Access: Access{Method: MethodMountedPath, Path: "/data/a.dwg"}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.JSONMap()["source"] == nil {
		t.Fatal("source plan JSON is missing source")
	}
}
