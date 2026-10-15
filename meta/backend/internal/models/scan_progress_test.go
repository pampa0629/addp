package models

import (
	"encoding/json"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"strings"
	"testing"
)

func TestScanProgressProjectionOnlyExposesTypedObservations(t *testing.T) {
	metadata := commonModels.JSONMap{"scan_progress": map[string]interface{}{
		"phase": "processing", "operation": "hashing", "files_discovered": 200,
		"items_planned": 2, "items_total_known": true, "items_completed": 1, "items_succeeded": 1,
		"current_path": "3d/模型.obj", "current_bytes_read": 1024, "current_bytes_total": 4096,
		"password": "DO_NOT_EXPOSE", "error": "DO_NOT_EXPOSE"}}
	response := NewScanExecutionResponse(&commonExecution.TaskExecution{Metadata: metadata})
	if response.ScanProgress == nil || response.ScanProgress.Percent() != 50 {
		t.Fatalf("progress = %+v", response.ScanProgress)
	}
	data, _ := json.Marshal(response)
	if strings.Contains(string(data), "DO_NOT_EXPOSE") {
		t.Fatal("arbitrary metadata escaped the scan projection")
	}
	for _, path := range []string{"/private/mount/file", "addp://engine/14/path/file?token=DO_NOT_EXPOSE", "https://host/file?password=DO_NOT_EXPOSE"} {
		metadata["scan_progress"].(map[string]interface{})["current_path"] = path
		if p := ObserveScanProgress(metadata); p == nil || p.CurrentPath != "" {
			t.Fatalf("unsafe path projected: %+v", p)
		}
	}
	metadata["scan_progress"].(map[string]interface{})["items_completed"] = 100
	if ObserveScanProgress(metadata) != nil {
		t.Fatal("inconsistent completed counters projected")
	}
}

func TestAttributeRangeProgressProjectionRejectsNegativeCounters(t *testing.T) {
	raw := map[string]interface{}{"phase": "processing", "operation": "enriching", "current_range_bytes_read": 458752, "current_ranges_read": 8192}
	metadata := commonModels.JSONMap{"scan_progress": raw}
	p := ObserveScanProgress(metadata)
	if p == nil || p.CurrentRangeBytesRead != 458752 || p.CurrentRangesRead != 8192 {
		t.Fatalf("attribute range observations missing: %+v", p)
	}
	for _, key := range []string{"current_range_bytes_read", "current_ranges_read"} {
		previous := raw[key]
		raw[key] = -1
		if ObserveScanProgress(metadata) != nil {
			t.Fatalf("negative %s projected", key)
		}
		raw[key] = previous
	}
}
