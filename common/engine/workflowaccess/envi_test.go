package workflowaccess

import (
	"encoding/json"
	"strings"
	"testing"
)

func enviSource(object bool) Source {
	main := Access{Method: MethodMountedPath, Path: "/data/ 原名 %.img", Server: "nfs", ExportPath: "/data", NFSVersion: "4"}
	header := main
	header.Path = "/data/ 原名 %.HDR"
	if object {
		main = Access{Method: MethodObjectStore, Endpoint: "storage:9000", Bucket: "data", Object: "imagery/ 原名 %.img", AccessKey: "exact-access-secret", SecretKey: "exact-password-secret"}
		header = main
		header.Object = "imagery/ 原名 %.HDR"
	}
	return Source{Kind: KindFile, Format: "envi", Access: main, RelatedFiles: []RelatedFile{{Role: "header", Access: header}}}
}

func TestENVIPlanPreservesExactCompanionAndRedactsEveryCredential(t *testing.T) {
	for _, object := range []bool{false, true} {
		plan, err := NewSourcePlan(enviSource(object))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(plan.JSONMap())
		if err != nil {
			t.Fatal(err)
		}
		var decoded SourcePlan
		if err := json.Unmarshal(raw, &decoded); err != nil || decoded.Validate() != nil || len(decoded.Source.RelatedFiles) != 1 {
			t.Fatalf("lost companion: %s %v", raw, err)
		}
		combined, err := New(plan.Source, Target{Kind: KindFile, Format: "tiff", Name: "result.tif", WriteMode: WriteModeCreate, Access: Access{Method: MethodMountedPath, Path: "/out/result.tif"}})
		if err != nil {
			t.Fatal(err)
		}
		audit, _ := json.Marshal(combined.AuditJSONMap())
		if strings.Contains(string(audit), "exact-access-secret") || strings.Contains(string(audit), "exact-password-secret") {
			t.Fatal("audit leaked companion credentials")
		}
	}
}

func TestENVIPlanRejectsBroaderAndContradictorySources(t *testing.T) {
	for name, mutate := range map[string]func(*Source){
		"single file":         func(s *Source) { s.RelatedFiles = nil },
		"directory":           func(s *Source) { s.Kind = KindDirectory },
		"foreign format":      func(s *Source) { s.Format = "img" },
		"role":                func(s *Source) { s.RelatedFiles[0].Role = "texture" },
		"another parent":      func(s *Source) { s.RelatedFiles[0].Access.Path = "/another/data.hdr" },
		"another connection":  func(s *Source) { s.RelatedFiles[0].Access.Server = "another" },
		"entrypoint override": func(s *Source) { s.Entrypoint = "different.img" },
		"unexplained file":    func(s *Source) { s.RelatedFiles = append(s.RelatedFiles, s.RelatedFiles[0]) },
		"traversal":           func(s *Source) { s.RelatedFiles[0].Access.Path = "/data/../data/file.hdr" },
	} {
		t.Run(name, func(t *testing.T) {
			source := enviSource(false)
			mutate(&source)
			if _, err := NewSourcePlan(source); err == nil {
				t.Fatal("unsupported composition admitted")
			}
		})
	}
	for _, name := range []string{"../imagery/file.hdr", "different/file.hdr", "imagery/data.img"} {
		source := enviSource(true)
		source.RelatedFiles[0].Access.Object = name
		if _, err := NewSourcePlan(source); err == nil {
			t.Fatal("foreign object admitted", name)
		}
	}
}
