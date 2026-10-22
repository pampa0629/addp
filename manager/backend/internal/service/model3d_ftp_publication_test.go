package service

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func ftpModelPublicationFixture() *plugin.OperatorInvokeResult {
	return &plugin.OperatorInvokeResult{Status: "success", ExecutionID: "runtime-execution", Result: map[string]interface{}{"result": map[string]interface{}{
		"glb_ref": "model3d/execution/model.glb", "glb_uri": "s3://manager/model3d/execution/model.glb", "size_bytes": float64(512),
		"publish": map[string]interface{}{"method": "object_store", "bucket": "manager", "object_name": "model3d/execution/model.glb", "object_uri": "s3://manager/model3d/execution/model.glb", "content_type": "model/gltf-binary", "uploaded_files": float64(1), "uploaded_bytes": float64(512)},
	}}}
}

func TestFTPModelPublicationRequiresMatchingPhysicalObjectAndReceipt(t *testing.T) {
	if err := validateFileModel3DGLBPublication(ftpModelPublicationFixture(), 512, "manager", "model3d/execution/model.glb"); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"nil", "empty-response", "missing-status", "failure", "no-execution", "contradictory-error", "contradictory-code", "empty-physical", "short-physical", "missing-publish", "wrong-ref", "wrong-uri", "wrong-size", "fractional-size", "wrong-bucket", "old-object", "wrong-publish-uri", "mounted-path", "wrong-content-type", "wrong-count", "fractional-count", "wrong-bytes", "missing-bytes"} {
		t.Run(fault, func(t *testing.T) {
			result := ftpModelPublicationFixture()
			size := int64(512)
			facts := result.Result["result"].(map[string]interface{})
			publish := facts["publish"].(map[string]interface{})
			switch fault {
			case "nil":
				result = nil
			case "empty-response":
				result = &plugin.OperatorInvokeResult{}
			case "missing-status":
				result.Status = ""
			case "failure":
				result.Status = "failed"
			case "no-execution":
				result.ExecutionID = ""
			case "contradictory-error":
				result.Error = "write failed"
			case "contradictory-code":
				result.ErrorCode = "OUTPUT_NOT_FOUND"
			case "empty-physical":
				size = 0
			case "short-physical":
				size = 511
			case "missing-publish":
				delete(facts, "publish")
			case "wrong-ref":
				facts["glb_ref"] = "model3d/old.glb"
			case "wrong-uri":
				facts["glb_uri"] = "s3://another/model3d/execution/model.glb"
			case "wrong-size":
				facts["size_bytes"] = float64(511)
			case "fractional-size":
				facts["size_bytes"] = float64(512.5)
			case "wrong-bucket":
				publish["bucket"] = "another"
			case "old-object":
				publish["object_name"] = "model3d/old.glb"
			case "wrong-publish-uri":
				publish["object_uri"] = "s3://manager/model3d/old.glb"
			case "mounted-path":
				publish["method"] = "mounted_path"
			case "wrong-content-type":
				publish["content_type"] = "application/octet-stream"
			case "wrong-count":
				publish["uploaded_files"] = float64(2)
			case "fractional-count":
				publish["uploaded_files"] = float64(1.5)
			case "wrong-bytes":
				publish["uploaded_bytes"] = float64(511)
			case "missing-bytes":
				delete(publish, "uploaded_bytes")
			}
			if err := validateFileModel3DGLBPublication(result, size, "manager", "model3d/execution/model.glb"); err == nil {
				t.Fatal("unproven target publication accepted")
			}
		})
	}
}

func TestModel3DReportedCountsMustBeExactIntegers(t *testing.T) {
	for _, value := range []any{int(512), int64(512), json.Number("512"), float64(512)} {
		if !model3DReportedCountMatches(value, 512) {
			t.Fatalf("valid numeric receipt rejected: %T", value)
		}
	}
	for _, value := range []any{nil, "512", true, float64(512.5), math.NaN(), math.Inf(1), json.Number("512.0"), json.Number("513")} {
		if model3DReportedCountMatches(value, 512) {
			t.Fatalf("malformed numeric receipt accepted: %T", value)
		}
	}
	if model3DReportedCountMatches(float64(9007199254740992), 9007199254740993) {
		t.Fatal("rounded numeric receipt accepted")
	}
	if !model3DReportedCountMatches(json.Number("9007199254740993"), 9007199254740993) {
		t.Fatal("exact large receipt rejected")
	}
}
