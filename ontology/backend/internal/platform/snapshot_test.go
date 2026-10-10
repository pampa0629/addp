package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
)

func sourceDefinition(t *testing.T) Context {
	t.Helper()
	var definition Context
	if err := json.Unmarshal(transferDefinition, &definition); err != nil {
		t.Fatal(err)
	}
	return definition
}

func definitionJSON(t *testing.T, definition Context) []byte {
	t.Helper()
	data, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mustCompile(t *testing.T, data []byte) *Snapshot {
	t.Helper()
	var definition Context
	_ = json.Unmarshal(data, &definition)
	snapshot, err := Compile(data, fixtureReview(t, definition))
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

// Synthetic limit/validation definitions need synthetic member bindings. These
// are test evidence only; production provenance is checked independently.
func fixtureReview(t *testing.T, d Context) []byte {
	t.Helper()
	var review Review
	if err := json.Unmarshal(transferReview, &review); err != nil {
		t.Fatal(err)
	}
	review.Bindings = nil
	for _, subject := range ReviewSubjects(d) {
		review.Bindings = append(review.Bindings, SourceBinding{Subject: subject, Sources: []string{"config"}})
	}
	review.Coverage[0].ID = d.Capability
	data, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPlatformSnapshotDeterminismAndIsolation(t *testing.T) {
	snapshot := mustCompile(t, transferDefinition)
	canonical := snapshot.CanonicalJSON()
	hash := sha256.Sum256(canonical)
	if snapshot.Digest() != hex.EncodeToString(hash[:]) {
		t.Fatal("digest does not bind the complete release")
	}
	var stored envelope
	if err := json.Unmarshal(canonical, &stored); err != nil || stored.Contract != ContractVersion ||
		stored.Compiler != CompilerVersion || stored.Definition.Digest != "" {
		t.Fatalf("invalid snapshot envelope: %v", err)
	}
	compact := definitionJSON(t, sourceDefinition(t))
	indented, err := json.MarshalIndent(sourceDefinition(t), "", "    ")
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range [][]byte{compact, indented} {
		other := mustCompile(t, source)
		if other.Digest() != snapshot.Digest() || !bytes.Equal(other.CanonicalJSON(), canonical) {
			t.Fatal("formatting changed the compiled release")
		}
	}
	// Source bytes, returned bytes, maps and every nested slice are independent.
	input := bytes.Clone(transferDefinition)
	other := mustCompile(t, input)
	clear(input)
	clear(canonical)
	context, err := other.Context()
	if err != nil {
		t.Fatal(err)
	}
	context.Concepts[0].Name["en"] = "changed"
	context.Concepts[0].ID = "changed"
	context.Relations[0].To = "changed"
	context.Requirements[0].Tools[0] = "changed.tool"
	context.Operation.InputsRequired[0] = "changed"
	context.Operation.Effects[0] = "changed.effect"
	context.Operation.ExcludedEffects[0] = "changed.excluded"
	fresh, err := other.Context()
	if err != nil || fresh.Concepts[0].ID == "changed" || fresh.Concepts[0].Name["en"] == "changed" ||
		fresh.Relations[0].To == "changed" || fresh.Requirements[0].Tools[0] == "changed.tool" ||
		fresh.Operation.InputsRequired[0] == "changed" || fresh.Operation.Effects[0] == "changed.effect" ||
		fresh.Operation.ExcludedEffects[0] == "changed.excluded" || fresh.Digest != snapshot.Digest() {
		t.Fatalf("mutation escaped release boundary: %v", err)
	}
	changed := sourceDefinition(t)
	changed.Concepts[0].Name["en"] = "Different definition"
	if mustCompile(t, definitionJSON(t, changed)).Digest() == snapshot.Digest() {
		t.Fatal("changed semantics retained the old digest")
	}
	changed = sourceDefinition(t)
	changed.Revision++
	if mustCompile(t, definitionJSON(t, changed)).Digest() == snapshot.Digest() {
		t.Fatal("revision is not bound by the digest")
	}
}

func TestPlatformCompileRejectsInvalidDefinition(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Context)
	}{
		{"schema", func(d *Context) { d.SchemaVersion = "other" }},
		{"tenant_kind", func(d *Context) { d.KnowledgeKind = "native_definition" }},
		{"availability", func(d *Context) { d.Availability = "ready" }},
		{"self_reported_digest", func(d *Context) { d.Digest = strings.Repeat("a", 64) }},
		{"zero_revision", func(d *Context) { d.Revision = 0 }},
		{"revision_overflow", func(d *Context) { d.Revision = math.MaxInt64 + 1 }},
		{"capability", func(d *Context) { d.Capability = "transfer" }},
		{"no_concepts", func(d *Context) { d.Concepts = []Concept{} }},
		{"duplicate_concept", func(d *Context) { d.Concepts = append(d.Concepts, d.Concepts[0]) }},
		{"invalid_concept_id", func(d *Context) { d.Concepts[0].ID = "../source" }},
		{"missing_bilingual_name", func(d *Context) { delete(d.Concepts[0].Name, "en") }},
		{"blank_name", func(d *Context) { d.Concepts[0].Name["en"] = " \n " }},
		{"long_name", func(d *Context) { d.Concepts[0].Name["en"] = strings.Repeat("x", 513) }},
		{"dangling_relation", func(d *Context) { d.Relations[0].To = "absent" }},
		{"invalid_relation_kind", func(d *Context) { d.Relations[0].Kind = "" }},
		{"duplicate_relation", func(d *Context) { d.Relations = append(d.Relations, d.Relations[0]) }},
		{"owner", func(d *Context) { d.Operation.Owner = "" }},
		{"skill", func(d *Context) { d.Operation.Skill = "../transfer" }},
		{"tool_binding", func(d *Context) { d.Operation.Tool = "transfer.task.run" }},
		{"missing_inputs", func(d *Context) { d.Operation.InputsRequired = []string{} }},
		{"duplicate_input", func(d *Context) {
			d.Operation.InputsRequired = append(d.Operation.InputsRequired, d.Operation.InputsRequired[0])
		}},
		{"invalid_effect", func(d *Context) { d.Operation.Effects[0] = "written" }},
		{"duplicate_effect", func(d *Context) { d.Operation.Effects = append(d.Operation.Effects, d.Operation.Effects[0]) }},
		{"duplicate_exclusion", func(d *Context) {
			d.Operation.ExcludedEffects = append(d.Operation.ExcludedEffects, d.Operation.ExcludedEffects[0])
		}},
		{"conflicting_effect", func(d *Context) { d.Operation.ExcludedEffects[0] = d.Operation.Effects[0] }},
		{"requirement_id", func(d *Context) { d.Requirements[0].ID = "" }},
		{"duplicate_requirement", func(d *Context) { d.Requirements = append(d.Requirements, d.Requirements[0]) }},
		{"empty_requirement", func(d *Context) { d.Requirements[0].Tools = []string{} }},
		{"invalid_tool", func(d *Context) { d.Requirements[0].Tools[0] = "search" }},
		{"duplicate_tool", func(d *Context) {
			d.Requirements[0].Tools = append(d.Requirements[0].Tools, d.Requirements[0].Tools[0])
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			definition := sourceDefinition(t)
			tc.mutate(&definition)
			if snapshot, err := Compile(definitionJSON(t, definition), fixtureReview(t, definition)); err == nil || snapshot != nil {
				t.Fatal("invalid definition compiled")
			}
		})
	}
}

func TestPlatformCompileRejectsNonStrictJSON(t *testing.T) {
	source := definitionJSON(t, sourceDefinition(t))
	for name, data := range map[string][]byte{
		"empty":                   nil,
		"null":                    []byte("null"),
		"array":                   []byte("[]"),
		"truncated":               source[:len(source)-1],
		"trailing_object":         append(bytes.Clone(source), []byte(" {}")...),
		"trailing_null":           append(bytes.Clone(source), []byte(" null")...),
		"trailing_garbage":        append(bytes.Clone(source), 'x'),
		"unknown_field":           bytes.Replace(source, []byte(`"revision":`), []byte(`"tenant_id":9,"revision":`), 1),
		"unknown_nested_field":    bytes.Replace(source, []byte(`"owner":`), []byte(`"credentials":"private","owner":`), 1),
		"case_folded_field":       bytes.Replace(source, []byte(`"revision":`), []byte(`"Revision":`), 1),
		"duplicate_field":         bytes.Replace(source, []byte(`"revision":`), []byte(`"revision":9,"revision":`), 1),
		"escaped_duplicate_field": bytes.Replace(source, []byte(`"revision":`), []byte(`"\u0072evision":9,"revision":`), 1),
		"nested_duplicate":        bytes.Replace(source, []byte(`"owner":`), []byte(`"owner":"other","owner":`), 1),
		"missing_digest":          bytes.Replace(source, []byte(`"digest":"",`), nil, 1),
		"null_digest":             bytes.Replace(source, []byte(`"digest":""`), []byte(`"digest":null`), 1),
		"null_relations":          bytes.Replace(source, []byte(`"relations":[`), []byte(`"relations":null,"extra":[`), 1),
		"invalid_utf8":            bytes.Replace(source, []byte("Transfer task definition"), []byte{0xff}, 1),
		"size_limit":              bytes.Repeat([]byte(" "), maxSourceBytes+1),
		"depth_limit":             []byte(strings.Repeat("[", maxJSONDepth+2) + "0" + strings.Repeat("]", maxJSONDepth+2)),
	} {
		t.Run(name, func(t *testing.T) {
			if snapshot, err := Compile(data, transferReview); err == nil || snapshot != nil {
				t.Fatal("non-strict source compiled")
			}
		})
	}
}

func TestPlatformSnapshotRestore(t *testing.T) {
	snapshot := mustCompile(t, transferDefinition)
	data := snapshot.CanonicalJSON()
	restored, err := Restore(data, snapshot.Digest())
	if err != nil || restored.Digest() != snapshot.Digest() || !bytes.Equal(restored.CanonicalJSON(), data) {
		t.Fatalf("valid snapshot failed restoration: %v", err)
	}
	if _, err := Restore(data, strings.Repeat("0", 64)); err == nil {
		t.Fatal("wrong digest accepted")
	}
	for name, invalid := range map[string][]byte{
		"whitespace":           append([]byte(" "), data...),
		"trailer":              append(bytes.Clone(data), []byte(" {}")...),
		"unsupported_contract": bytes.Replace(data, []byte(ContractVersion), []byte("other"), 1),
		"unsupported_compiler": bytes.Replace(data, []byte(CompilerVersion), []byte("other"), 1),
		"digest_in_definition": bytes.Replace(data, []byte(`"digest":""`), []byte(`"digest":"self_reported"`), 1),
		"modified_content":     bytes.Replace(data, []byte("Transfer task definition"), []byte("Modified task definition"), 1),
		"unknown_field":        bytes.Replace(data, []byte(`"compiler":`), []byte(`"extra":0,"compiler":`), 1),
		"duplicate_field":      bytes.Replace(data, []byte(`"compiler":`), []byte(`"compiler":"other","compiler":`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Restore(invalid, snapshot.Digest()); err == nil {
				t.Fatal("corrupt snapshot restored")
			}
		})
	}
	// A newly computed hash must not bypass contract or canonical checks.
	for _, invalid := range [][]byte{
		append([]byte(" "), data...),
		bytes.Replace(data, []byte(CompilerVersion), []byte("other"), 1),
		bytes.Replace(data, []byte(`"kind":"reads_from"`), []byte(`"kind":""`), 1),
	} {
		hash := sha256.Sum256(invalid)
		if _, err := Restore(invalid, hex.EncodeToString(hash[:])); err == nil {
			t.Fatal("new hash bypassed snapshot validation")
		}
	}
}

func TestPlatformDefinitionLimits(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit int
		set   func(*Context, int)
	}{
		{"concepts", 32, func(d *Context, n int) {
			d.Concepts = nil
			d.Relations = []Relation{}
			for i := 0; i < n; i++ {
				d.Concepts = append(d.Concepts, Concept{fmt.Sprintf("c_%d", i), map[string]string{"en": "Class", "zh-cn": "类"}})
			}
		}},
		{"relations", 64, func(d *Context, n int) {
			d.Relations = nil
			for i := 0; i < n; i++ {
				d.Relations = append(d.Relations, Relation{"source", fmt.Sprintf("r_%d", i), "target"})
			}
		}},
		{"requirements", 16, func(d *Context, n int) {
			d.Requirements = nil
			for i := 0; i < n; i++ {
				d.Requirements = append(d.Requirements, Requirement{fmt.Sprintf("q_%d", i), []string{"engine.list"}})
			}
		}},
		{"inputs", 32, func(d *Context, n int) { d.Operation.InputsRequired = ids(n, "input_") }},
		{"effects", 32, func(d *Context, n int) { d.Operation.Effects = ids(n, "effect.e_") }},
		{"exclusions", 32, func(d *Context, n int) { d.Operation.ExcludedEffects = ids(n, "excluded.e_") }},
		{"tools", 32, func(d *Context, n int) { d.Requirements[0].Tools = ids(n, "engine.t_") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			definition := sourceDefinition(t)
			tc.set(&definition, tc.limit)
			mustCompile(t, definitionJSON(t, definition))
			tc.set(&definition, tc.limit+1)
			if _, err := Compile(definitionJSON(t, definition), fixtureReview(t, definition)); err == nil {
				t.Fatal("member limit bypassed")
			}
		})
	}
	oversize := sourceDefinition(t)
	oversize.Concepts = nil
	oversize.Relations = []Relation{}
	for i := 0; i < 32; i++ {
		oversize.Concepts = append(oversize.Concepts, Concept{fmt.Sprintf("c_%d", i), map[string]string{
			"en": strings.Repeat("x", 512), "zh-cn": strings.Repeat("x", 512),
		}})
	}
	if _, err := Compile(definitionJSON(t, oversize), fixtureReview(t, oversize)); err == nil {
		t.Fatal("context byte limit bypassed")
	}
}

func ids(n int, prefix string) []string {
	values := make([]string, n)
	for i := range values {
		values[i] = fmt.Sprintf("%s%d", prefix, i)
	}
	return values
}
