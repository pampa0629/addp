package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
)

type swaggerContractDocument struct {
	Paths map[string]map[string]swaggerContractOperation `json:"paths"`
}

type swaggerContractOperation struct {
	Parameters []struct {
		In string `json:"in"`
	} `json:"parameters"`
	Responses map[string]json.RawMessage `json:"responses"`
	AuthMode  string                     `json:"x-addp-auth-mode"`
}

func TestSwaggerDeclaresCommonModelErrorResponses(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path")
	}
	content, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "..", "docs", "swagger.json"))
	if err != nil {
		t.Fatalf("read swagger: %v", err)
	}
	var document swaggerContractDocument
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatalf("decode swagger: %v", err)
	}

	var missing []string
	for path, methods := range document.Paths {
		for method, operation := range methods {
			if operation.AuthMode == "permission" {
				for _, status := range []string{"401", "403"} {
					if _, ok := operation.Responses[status]; !ok {
						missing = append(missing, method+" "+path+" missing "+status)
					}
				}
			}
			hasPathParameter := false
			hasRequestParameter := false
			for _, parameter := range operation.Parameters {
				switch parameter.In {
				case "path":
					hasPathParameter = true
					hasRequestParameter = true
				case "query", "body":
					hasRequestParameter = true
				}
			}
			if hasRequestParameter {
				if _, ok := operation.Responses["400"]; !ok {
					missing = append(missing, method+" "+path+" missing 400")
				}
			}
			if hasPathParameter {
				if _, ok := operation.Responses["404"]; !ok {
					missing = append(missing, method+" "+path+" missing 404")
				}
			}
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		for _, item := range missing {
			t.Error(item)
		}
	}
}
