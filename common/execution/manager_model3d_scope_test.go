package execution

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
)

func model3DTestComposition(t *testing.T) *authorizationfacts.FileCompositionBoundary {
	t.Helper()
	result, err := authorizationfacts.NewFileCompositionBoundary("3ds", []authorizationfacts.FileCompositionRef{
		{Target: plugin.FileItemPath(33, "models/scene.3DS"), Role: "model", Required: true, Primary: true},
		{Target: plugin.FileItemPath(33, "models/TEST.PNG"), Role: "texture", Required: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func model3DTestRaw(t *testing.T, options string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"config_version":        ManagerModel3DConfigVersion,
		"owner_task":            map[string]string{"id": "10", "version": "7"},
		"source_engine_version": "5", "source_composition": model3DTestComposition(t),
		"source":  map[string]any{"source_engine_id": 33, "format": "3ds", "item_locator": "models/scene.3DS"},
		"result":  map[string]string{"storage_ref": "tenant_3/model3d/executions/uuid/model.glb"},
		"options": json.RawMessage(options),
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestManagerModel3DScopeBindsWholeRawConfigAndExactVersions(t *testing.T) {
	composition := model3DTestComposition(t)
	raw := model3DTestRaw(t, `{"precision":9007199254740993}`)
	scope, err := NewManagerModel3DReadScope(raw, "10", "7", "5", composition)
	if err != nil || scope.Validate() != nil {
		t.Fatal(err)
	}
	var indented strings.Builder
	indented.WriteString(" \n")
	indented.Write(raw)
	indented.WriteString(" \n")
	formatted, err := NewManagerModel3DReadScope(json.RawMessage(indented.String()), "10", "7", "5", composition)
	if err != nil || !reflect.DeepEqual(scope, formatted) {
		t.Fatal("whitespace changed scope", err)
	}
	for _, changed := range []json.RawMessage{
		model3DTestRaw(t, `{"precision":9007199254740992}`),
		json.RawMessage(strings.Replace(string(raw), "model.glb", "other.glb", 1)),
		json.RawMessage(strings.Replace(string(raw), `"item_locator":"models/scene.3DS"`, `"item_locator":"models/changed.3DS"`, 1)),
	} {
		other, err := NewManagerModel3DReadScope(changed, "10", "7", "5", composition)
		if err != nil || other.ConfigDigest == scope.ConfigDigest {
			t.Fatal("conversion change was not bound", err)
		}
	}
	composition.Refs[0].Target.Segments[2].Name = "changed"
	clone := scope.Clone()
	clone.FileComposition.Refs[0].Target.Segments[2].Name = "changed"
	if scope.Validate() != nil || reflect.DeepEqual(clone, scope) {
		t.Fatal("scope aliases mutable composition")
	}
}

func TestManagerModel3DScopeRejectsMismatchedAndAmbiguousFrozenWrapper(t *testing.T) {
	raw := model3DTestRaw(t, `{}`)
	for _, bad := range []string{
		"", `null`, `[]`, `{} {}`, string(raw) + `{}`,
		strings.Replace(string(raw), ManagerModel3DConfigVersion, "other", 1),
		strings.Replace(string(raw), `"id":"10"`, `"id":"11"`, 1),
		strings.Replace(string(raw), `"version":"7"`, `"version":"8"`, 1),
		strings.Replace(string(raw), `"source_engine_version":"5"`, `"source_engine_version":"6"`, 1),
		strings.Replace(string(raw), `"options":{}`, `"options":null`, 1),
		strings.Replace(string(raw), `"options":{}`, `"options":{"x":1,"x":2}`, 1),
		strings.Replace(string(raw), `"owner_task":`, `"owner_task":{},"owner_task":`, 1),
		strings.Repeat(" ", 512<<10) + string(raw),
	} {
		if scope, err := NewManagerModel3DReadScope(json.RawMessage(bad), "10", "7", "5", model3DTestComposition(t)); err == nil || scope != nil {
			t.Fatal("invalid frozen wrapper accepted")
		}
	}
	for _, ids := range [][3]string{{"010", "7", "5"}, {"10", "0", "5"}, {"10", "7", "-1"}} {
		if _, err := NewManagerModel3DReadScope(raw, ids[0], ids[1], ids[2], model3DTestComposition(t)); err == nil {
			t.Fatal("invalid identity accepted")
		}
	}
	changed := model3DTestComposition(t)
	changed.Refs[0].Role = "different"
	if _, err := NewManagerModel3DReadScope(raw, "10", "7", "5", changed); err == nil {
		t.Fatal("different composition accepted")
	}
}

func TestManagerModel3DScopeStrictWireAndUnsupportedVariants(t *testing.T) {
	scope, err := NewManagerModel3DReadScope(model3DTestRaw(t, `{}`), "10", "7", "5", model3DTestComposition(t))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(scope)
	var decoded ManagerModel3DReadScope
	if err := json.Unmarshal(raw, &decoded); err != nil || !reflect.DeepEqual(scope, &decoded) {
		t.Fatal(err)
	}
	for _, bad := range []string{
		`{}`, string(raw) + `{}`,
		strings.Replace(string(raw), `"owner_task_id":"10"`, `"owner_task_id":"10","allow":true`, 1),
		strings.Replace(string(raw), `"owner_task_id":"10"`, `"owner_task_id":"10","owner_task_id":"10"`, 1),
		strings.Replace(string(raw), `"engine_version":"5"`, `"engine_version":"05"`, 1),
		strings.Replace(string(raw), `"format":"3ds"`, `"format":"fbx"`, 1),
	} {
		if json.Unmarshal([]byte(bad), &decoded) == nil {
			t.Fatal("invalid scope wire accepted")
		}
	}
	for _, mutate := range []func(*ManagerModel3DReadScope){
		func(s *ManagerModel3DReadScope) { s.ConfigDigest = strings.Repeat("A", 64) },
		func(s *ManagerModel3DReadScope) { s.FileComposition.Refs = nil },
		func(s *ManagerModel3DReadScope) { s.OwnerTaskVersion = "0" },
	} {
		bad := scope.Clone()
		mutate(bad)
		if bad.Validate() == nil {
			t.Fatal("invalid scope accepted")
		}
	}
}

func TestManagerModel3DScopeRejectsReplacedFTPConfigVersion(t *testing.T) {
	raw := model3DTestRaw(t, `{}`)
	raw = json.RawMessage(strings.Replace(string(raw), ManagerModel3DConfigVersion, "model3d-glb-ftp-config/v1", 1))
	if scope, err := NewManagerModel3DReadScope(raw, "10", "7", "5", model3DTestComposition(t)); err == nil || scope != nil {
		t.Fatal("legacy frozen configuration accepted")
	}
}

func TestManagerModel3DObjectAdmissionPreservesCatalogAndFormat(t *testing.T) {
	for _, format := range []string{"ifc", "3ds", "dae", "obj"} {
		for _, kind := range []string{"ftp", "nfs", "minio", "s3"} {
			for _, term := range []string{"file", "object"} {
				target := plugin.FileItemPath(33, "models/model."+format)
				if term == "object" {
					target = plugin.ObjectItemPath(33, "models", "model."+format)
				}
				composition, err := authorizationfacts.NewFileCompositionBoundary(format, []authorizationfacts.FileCompositionRef{{Target: target, Role: "model", Required: true, Primary: true}})
				if err != nil {
					t.Fatal(err)
				}
				want := ((kind == "ftp" || kind == "nfs") && term == "file") || (kind == "minio" && term == "object")
				if ManagerModel3DSourceComposition(kind, composition) != want {
					t.Fatalf("wrong admission %s/%s/%s", kind, term, format)
				}
				scope := &ManagerModel3DReadScope{ConfigDigest: strings.Repeat("a", 64), OwnerTaskID: "1", OwnerTaskVersion: "1", EngineVersion: "1", FileComposition: *composition}
				if scope.Validate() != nil {
					t.Fatalf("wrong scope shape %s/%s", term, format)
				}
			}
		}
	}
}
