package planner

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Use the actual Hosted request builder so mocks cannot accept configuration
// that the production planner rejects before the intended write failure.
func TestHostedWriteFailureTaskContract(t *testing.T) {
	script := filepath.Join("..", "..", "..", "..", "scripts", "test", "transfer-relational-sql-etl-online.py")
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "python3", "-c", `
import importlib.util, json, sys
from pathlib import Path
path = Path(sys.argv[1]).resolve()
spec = importlib.util.spec_from_file_location("hosted_transfer_contract", path)
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)
tasks = []
for engine, engine_id in (("doris", 8), ("clickhouse", 9)):
    for scenario in module.WRITE_FAILURE_CASES:
        source = "addp_online_transfer_failure_source" if scenario == "late_execute_failure" else "addp_online_transfer_invalid_json_source"
        locator = f"addp://engine/1/path/public/{source}?type=table&item_id=17"
        tasks.append(module.write_failure_task(engine + "_" + scenario, locator, source, engine_id, scenario))
print(json.dumps(tasks))
`, script)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("build Hosted synthetic requests: %v\n%s", err, output)
	}
	var tasks []struct {
		Name             string                 `json:"name"`
		AutoScanMetadata bool                   `json:"auto_scan_metadata"`
		Config           map[string]interface{} `json:"config"`
	}
	if err := json.Unmarshal(output, &tasks); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 4 {
		t.Fatalf("Hosted write failure requests = %d, want 4", len(tasks))
	}
	for _, task := range tasks {
		t.Run(task.Name, func(t *testing.T) {
			spec, err := ParseTableExportTaskSpec(task.Config, 1000)
			if err != nil {
				t.Fatalf("actual Hosted request rejected: %v", err)
			}
			if task.AutoScanMetadata || spec.BatchSize != 1001 || spec.Load.Mode != loadModeSnapshot || applyMode(spec.Target.Policy) != applyModeAppend {
				t.Fatalf("unexpected failure task boundary: %#v", spec)
			}
			parent, err := spec.Target.ParentResourceLocator()
			if err != nil {
				t.Fatal(err)
			}
			if parent.LastSegment() != "transfer_fixture" || spec.Target.Name == "" {
				t.Fatalf("invalid fixture target: %#v", spec.Target)
			}
			engineType := "doris"
			if parent.EngineID == 9 {
				engineType = "clickhouse"
			}
			path, err := nativeTableTargetPath(spec.Target, EngineBinding{Type: engineType})
			if err != nil {
				t.Fatal(err)
			}
			if path.StringPath() != "transfer_fixture/"+spec.Target.Name {
				t.Fatalf("target path = %#v", path)
			}
		})
	}
}
