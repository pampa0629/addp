package service

import (
	"encoding/json"
	"errors"
	"math"

	"github.com/addp/common/engine/plugin"
)

// A successful HTTP call is not a publication receipt. Match the real product
// response to this execution's exact target and the independent object stat.
func validateFileModel3DGLBPublication(result *plugin.OperatorInvokeResult, actualSize int64, bucket, object string) error {
	reject := errors.New("file model GLB publication does not match the execution target")
	if result == nil || result.Status != "success" || result.ExecutionID == "" || result.Error != "" || result.ErrorCode != "" || actualSize <= 0 || bucket == "" || object == "" {
		return reject
	}
	facts := operatorInvokeJSONFacts(result)
	publish, ok := facts["publish"].(map[string]interface{})
	uri := "s3://" + bucket + "/" + object
	if !ok || facts["glb_ref"] != object || facts["glb_uri"] != uri || !model3DReportedCountMatches(facts["size_bytes"], actualSize) ||
		publish["method"] != "object_store" || publish["bucket"] != bucket || publish["object_name"] != object || publish["object_uri"] != uri ||
		publish["content_type"] != "model/gltf-binary" || !model3DReportedCountMatches(publish["uploaded_files"], 1) || !model3DReportedCountMatches(publish["uploaded_bytes"], actualSize) {
		return reject
	}
	return nil
}

func model3DReportedCountMatches(raw any, expected int64) bool {
	if expected < 0 {
		return false
	}
	switch value := raw.(type) {
	case int:
		return int64(value) == expected
	case int64:
		return value == expected
	case json.Number:
		parsed, err := value.Int64()
		return err == nil && parsed == expected
	case float64:
		// The HTTP Runtime provider decodes JSON numbers as float64. Reject values
		// outside the exact-integer range and fractions instead of rounding them.
		return expected <= 1<<53 && !math.IsNaN(value) && !math.IsInf(value, 0) && math.Trunc(value) == value && value == float64(expected)
	default:
		return false
	}
}
