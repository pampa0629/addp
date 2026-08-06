package service

import "testing"

func TestProviderTemplatesOnlyDescribeSupportedAdapters(t *testing.T) {
	templates := ListProviderTemplates()
	if len(templates) < 8 {
		t.Fatalf("template count = %d, want a useful built-in catalog", len(templates))
	}
	foundMultimodal := false
	for _, template := range templates {
		if template.Code == "" || template.Category == "" || template.AdapterType == "" {
			t.Fatalf("invalid provider template: %+v", template)
		}
		if template.AdapterType != AdapterOpenAICompatible && template.AdapterType != AdapterDashScopeMultimodal {
			t.Fatalf("template %s exposes unsupported adapter %s", template.Code, template.AdapterType)
		}
		if template.Code == "dashscope-multimodal" {
			foundMultimodal = true
			if len(template.SuggestedModels) != 1 || template.SuggestedModels[0].Dimension != 2560 {
				t.Fatalf("unexpected multimodal template: %+v", template)
			}
		}
	}
	if !foundMultimodal {
		t.Fatal("dashscope multimodal template is missing")
	}
}

func TestProviderTemplateListReturnsIndependentSlices(t *testing.T) {
	first := ListProviderTemplates()
	first[0].Code = "changed"
	if len(first[2].SuggestedModels) > 0 {
		first[2].SuggestedModels[0].UpstreamModel = "changed"
		first[2].SuggestedModels[0].Operations[0] = "changed"
	}
	second := ListProviderTemplates()
	if second[0].Code == "changed" || second[2].SuggestedModels[0].UpstreamModel == "changed" || second[2].SuggestedModels[0].Operations[0] == "changed" {
		t.Fatal("provider template catalog leaked mutable shared slices")
	}
}
