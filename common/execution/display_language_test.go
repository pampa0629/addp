package execution

import (
	"encoding/json"
	"github.com/addp/common/models"
	"testing"
)

func TestDisplayLanguageFreezesWithoutMutatingTemplateAndSurvivesJSON(t *testing.T) {
	for _, lang := range []string{"zh-cn", "en"} {
		template := models.JSONMap{"timeout": 30}
		item := &TaskExecution{TenantID: 7, ExecutionID: "root", ExecutionConfig: template}
		if err := FreezeDisplayLanguage(item, lang); err != nil {
			t.Fatal(err)
		}
		if _, exists := template[DisplayLanguageKey]; exists {
			t.Fatal("task template mutated")
		}
		data, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		var reloaded TaskExecution
		if err := json.Unmarshal(data, &reloaded); err != nil {
			t.Fatal(err)
		}
		if got, err := DisplayLanguage(&reloaded); err != nil || got != lang {
			t.Fatalf("reloaded %q %v", got, err)
		}
		if err := FreezeDisplayLanguage(&reloaded, lang); err != nil {
			t.Fatal(err)
		}
		other := "en"
		if lang == other {
			other = "zh-cn"
		}
		if err := FreezeDisplayLanguage(&reloaded, other); err == nil {
			t.Fatal("frozen value overwritten")
		}
	}
}
func TestDisplayLanguageRequiresValidPersistedFact(t *testing.T) {
	for _, value := range []interface{}{nil, 1, "", "en-US", "EN", "fr"} {
		item := &TaskExecution{ExecutionConfig: models.JSONMap{DisplayLanguageKey: value}}
		if _, err := DisplayLanguage(item); err == nil {
			t.Fatalf("accepted %#v", value)
		}
		if err := FreezeDisplayLanguage(item, "en"); err == nil {
			t.Fatal("invalid fact repaired implicitly")
		}
	}
	if _, err := DisplayLanguage(&TaskExecution{}); err == nil {
		t.Fatal("missing snapshot defaulted")
	}
	if err := FreezeDisplayLanguage(nil, "en"); err == nil {
		t.Fatal("nil execution accepted")
	}
}
func TestDisplayLanguageInheritanceRequiresSameTenantAndParentEdge(t *testing.T) {
	parent := &TaskExecution{TenantID: 7, ExecutionID: "parent"}
	if err := FreezeDisplayLanguage(parent, "en"); err != nil {
		t.Fatal(err)
	}
	id := "parent"
	child := &TaskExecution{TenantID: 7, ExecutionID: "child", ParentExecutionID: &id}
	if err := InheritDisplayLanguage(child, parent); err != nil {
		t.Fatal(err)
	}
	if got, _ := DisplayLanguage(child); got != "en" {
		t.Fatal(got)
	}
	for _, invalid := range []*TaskExecution{nil, {TenantID: 8, ParentExecutionID: &id}, {TenantID: 7}, {TenantID: 7, ExecutionID: "parent", ParentExecutionID: &id}} {
		if err := InheritDisplayLanguage(invalid, parent); err == nil {
			t.Fatal("invalid parent edge accepted")
		}
	}
	wrong := "another-parent"
	child.ParentExecutionID = &wrong
	if err := InheritDisplayLanguage(child, parent); err == nil {
		t.Fatal("wrong parent accepted")
	}
	child.ParentExecutionID = &id
	child.ExecutionConfig[DisplayLanguageKey] = "zh-cn"
	if err := InheritDisplayLanguage(child, parent); err == nil {
		t.Fatal("child language overrides parent")
	}
	parent.ExecutionConfig = nil
	child.ExecutionConfig = nil
	if err := InheritDisplayLanguage(child, parent); err == nil {
		t.Fatal("missing parent language defaulted")
	}
}
