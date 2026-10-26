package resourcequery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPromtoolUsesExistingCenterAndPrivateRuleDirectory(t *testing.T) {
	work := t.TempDir()
	if err := os.Mkdir(filepath.Join(work, "query-tests"), 0755); err != nil {
		t.Fatal(err)
	}
	// Exercise the actual fixture invocation and file lifecycle without Docker.
	docker := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$PROMTOOL_ARGS\"\ncat \"$METRICS_T2_WORK/query-tests/lifecycle-query-tests.yml\" > \"$PROMTOOL_INPUT\"\n"
	if err := os.WriteFile(filepath.Join(work, "docker"), []byte(docker), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", work+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("METRICS_T2_WORK", work)
	t.Setenv("METRICS_T2_PROJECT", "owned-fixture")
	t.Setenv("METRICS_T2_COMPOSE", "/owned/compose.yml")
	t.Setenv("PROMTOOL_ARGS", filepath.Join(work, "args"))
	t.Setenv("PROMTOOL_INPUT", filepath.Join(work, "payload"))
	runPromtoolCases(t, "lifecycle", []any{map[string]any{"name": "owned"}}, false)
	data, err := os.ReadFile(filepath.Join(work, "args"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"compose", "--env-file", "/dev/null", "-p", "owned-fixture", "-f", "/owned/compose.yml", "exec", "-T", "prometheus", "/bin/promtool", "test", "rules", "/query-tests/lifecycle-query-tests.yml"}
	if !reflect.DeepEqual(strings.Split(strings.TrimSpace(string(data)), "\n"), want) {
		t.Fatalf("unexpected promtool lifecycle: %s", data)
	}
	payload, err := os.ReadFile(filepath.Join(work, "payload"))
	if err != nil {
		t.Fatal(err)
	}
	var input struct {
		Tests []struct{ Name string } `json:"tests"`
	}
	if err := json.Unmarshal(payload, &input); err != nil || len(input.Tests) != 1 || input.Tests[0].Name != "owned" {
		t.Fatalf("fixture input not available to existing center: %s, %v", payload, err)
	}
	if _, err := os.Stat(filepath.Join(work, "query-tests/lifecycle-query-tests.yml")); !os.IsNotExist(err) {
		t.Fatal("rule fixture was not removed", err)
	}
}
