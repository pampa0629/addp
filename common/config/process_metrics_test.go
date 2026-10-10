package config

import (
	"strings"
	"testing"
)

const selectedProcessDeployment = `{"module_name":"monitor","role":"backend","listen":"127.0.0.1:18100","endpoint":"https://host.docker.internal:18100/metrics","tls_dir":"/tmp/process-certs"}`

func TestProcessMetricsDeploymentSelection(t *testing.T) {
	for _, raw := range []string{"", "[]"} {
		got, err := ProcessMetricsDeploymentFor(raw, "monitor", "backend")
		if err != nil || got != nil {
			t.Fatalf("disabled: %v %v", got, err)
		}
	}
	raw := "[" + selectedProcessDeployment + "]"
	got, err := ProcessMetricsDeploymentFor(raw, "monitor", "backend")
	if err != nil || got == nil || got.Listen != "127.0.0.1:18100" {
		t.Fatalf("selected: %v %v", got, err)
	}
	got, err = ProcessMetricsDeploymentFor(raw, "monitor", "worker")
	if err != nil || got != nil {
		t.Fatal("backend must not configure worker")
	}
}

func TestProcessMetricsDeploymentRejectsAmbiguousOrUnsafeInput(t *testing.T) {
	valid := "[" + selectedProcessDeployment + "]"
	inputs := []string{"null", "{}", valid + "[]", "[" + selectedProcessDeployment + "," + selectedProcessDeployment + "]", strings.Repeat(" ", 256<<10) + valid}
	for _, pair := range [][2]string{{`"role":"backend"`, `"role":"backend","role":"worker"`}, {`"role":"backend"`, `"role":"unknown"`}, {`"listen":"127.0.0.1:18100"`, `"listen":"0.0.0.0:18100"`}, {`"listen":"127.0.0.1:18100"`, `"listen":"localhost:18100"`}, {`"listen":"127.0.0.1:18100"`, `"listen":"127.0.0.1:0"`}, {`"tls_dir":"/tmp/process-certs"`, `"tls_dir":"relative"`}, {`"role":"backend"`, `"role":null`}, {`"role":"backend"`, `"role":"backend","secret":"value"`}, {`https://host.docker.internal:18100/metrics`, `https://host.docker.internal:18100/metrics?token=secret`}} {
		inputs = append(inputs, strings.Replace(valid, pair[0], pair[1], 1))
	}
	for _, raw := range inputs {
		if _, err := ProcessMetricsDeploymentFor(raw, "monitor", "backend"); err == nil {
			t.Fatalf("accepted unsafe input: %.200s", raw)
		}
	}
	// Invalid entries cannot be hidden behind an unrelated module selection.
	if _, err := ProcessMetricsDeploymentFor(`[{}]`, "other", "worker"); err == nil {
		t.Fatal("invalid unrelated entry accepted")
	}
	for _, collision := range []string{
		strings.Replace(strings.Replace(selectedProcessDeployment, `"monitor"`, `"system"`, 1), `"listen":"127.0.0.1:18100"`, `"listen":"127.0.0.1:18101"`, 1),
		strings.Replace(strings.Replace(selectedProcessDeployment, `"monitor"`, `"system"`, 1), `"endpoint":"https://host.docker.internal:18100/metrics"`, `"endpoint":"https://host.docker.internal:18101/metrics"`, 1),
	} {
		if _, err := ProcessMetricsDeploymentFor("["+selectedProcessDeployment+","+collision+"]", "monitor", "backend"); err == nil {
			t.Fatal("duplicate transport accepted")
		}
	}
}
