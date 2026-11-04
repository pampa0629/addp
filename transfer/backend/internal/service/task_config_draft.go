package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/addp/common/datatype"
	"github.com/addp/common/query"
	"github.com/addp/common/resourcetree"
)

// TaskConfigDraftRequest contains business choices, never caller-supplied schema
// or executable queries. Public transport wiring must resolve facts first.
type TaskConfigDraftRequest struct {
	Name          string                  `json:"name"`
	Description   *string                 `json:"description,omitempty"`
	BatchSize     *int                    `json:"batch_size,omitempty"`
	SourceLocator string                  `json:"source_locator"`
	Target        TaskConfigDraftTarget   `json:"target"`
	RowGrain      TaskConfigDraftRowGrain `json:"row_grain"`
	Fields        []TaskConfigDraftField  `json:"fields"`
}
type TaskConfigDraftTarget struct {
	ParentLocator string `json:"parent_locator"`
	Name          string `json:"name"`
	ApplyMode     string `json:"apply_mode"`
}
type TaskConfigDraftRowGrain struct {
	Mode string   `json:"mode"`
	Path []string `json:"path,omitempty"`
}
type TaskConfigDraftField struct {
	Path       []string           `json:"path"`
	Target     string             `json:"target"`
	TargetType datatype.FieldType `json:"target_type,omitempty"`
	Nullable   *bool              `json:"nullable,omitempty"`
	Precision  *int               `json:"precision,omitempty"`
	Scale      *int               `json:"scale,omitempty"`
}

// taskConfigDraftFacts is private, non-wire input from authorized owner reads.
// The compiler cannot read connections, rows, task repositories or approvals.
type taskConfigDraftFacts struct {
	sourceLocator       string
	targetParentLocator string
	sourceEngineType    string
	targetEngineType    string
	source              *datatype.TableInfo
	mql                 query.ResourceQueryReference
}

type taskConfigDraftError struct{ Code, Path string }

func (e *taskConfigDraftError) DraftDiagnostic() (string, string) { return e.Code, e.Path }

func (e *taskConfigDraftError) Error() string { return e.Code + ":" + e.Path }
func draftError(code, path string) error      { return &taskConfigDraftError{code, path} }

// buildTaskConfigDraft compiles a bounded snapshot from resolved facts. It is
// deliberately private until the owner-read adapter enforces visibility.
func buildTaskConfigDraft(r TaskConfigDraftRequest, facts taskConfigDraftFacts) (map[string]interface{}, error) {
	if strings.TrimSpace(r.Name) == "" || utf8.RuneCountInString(r.Name) > 200 || (r.Description != nil && utf8.RuneCountInString(*r.Description) > 4000) || (r.BatchSize != nil && (*r.BatchSize < 1 || *r.BatchSize > 10000)) {
		return nil, draftError("draft_input_invalid", "/name")
	}
	if r.SourceLocator != facts.sourceLocator || r.Target.ParentLocator != facts.targetParentLocator || facts.source == nil || len(facts.source.Fields) == 0 {
		return nil, draftError("draft_facts_missing", "/source_locator")
	}
	source, err := resourcetree.ParseURI(r.SourceLocator)
	if err != nil || source.EngineID == 0 || source.Type != resourcetree.TypeCollection || facts.sourceEngineType != "mongodb" || facts.targetEngineType != "postgresql" {
		return nil, draftError("draft_scope_unsupported", "/source_locator")
	}
	parent, err := resourcetree.ParseURI(r.Target.ParentLocator)
	if err != nil || parent.EngineID == 0 || parent.Type != resourcetree.TypeSchema || len(parent.Path) != 1 || strings.TrimSpace(r.Target.Name) == "" || strings.ContainsRune(r.Target.Name, 0) || utf8.RuneCountInString(r.Target.Name) > 200 || (r.Target.ApplyMode != "replace" && r.Target.ApplyMode != "append") {
		return nil, draftError("draft_target_invalid", "/target")
	}
	if len(facts.mql.Parts) != 1 || facts.mql.Parts[0] == "" || strings.ContainsRune(facts.mql.Parts[0], 0) {
		return nil, draftError("draft_query_reference_missing", "/source_locator")
	}
	if len(r.Fields) < 1 || len(r.Fields) > 128 {
		return nil, draftError("draft_fields_invalid", "/fields")
	}
	unwind := ""
	switch r.RowGrain.Mode {
	case "document":
		if len(r.RowGrain.Path) != 0 {
			return nil, draftError("draft_row_grain_invalid", "/row_grain")
		}
	case "array_element":
		f, e := draftSourceField(facts.source.Fields, r.RowGrain.Path, "")
		if e != nil || f.Type != datatype.FieldTypeArray {
			return nil, draftError("draft_row_grain_invalid", "/row_grain/path")
		}
		unwind = strings.Join(r.RowGrain.Path, ".")
	default:
		return nil, draftError("draft_row_grain_invalid", "/row_grain")
	}
	project := map[string]interface{}{"_id": 0}
	mappings := make([]interface{}, 0, len(r.Fields))
	targets := map[string]bool{}
	for i, choice := range r.Fields {
		path := fmt.Sprintf("/fields/%d", i)
		f, e := draftSourceField(facts.source.Fields, choice.Path, unwind)
		if e != nil {
			return nil, draftError("draft_field_unresolved", path+"/path")
		}
		if choice.Target == "" || strings.ContainsRune(choice.Target, 0) || targets[choice.Target] {
			return nil, draftError("draft_target_field_invalid", path+"/target")
		}
		targets[choice.Target] = true
		typ := choice.TargetType
		if typ == "" {
			typ = f.Type
			if strings.Join(choice.Path, ".") == unwind && f.Type == datatype.FieldTypeArray {
				if f.Element != nil {
					typ = f.Element.Type
				} else {
					typ = f.ElementType
				}
			}
			if typ == datatype.FieldTypeArray {
				typ = datatype.FieldTypeJSON
			}
		}
		if !datatype.IsKnownFieldType(typ) || typ == datatype.FieldTypeUnknown || typ == datatype.FieldTypeMixed {
			return nil, draftError("draft_field_type_required", path+"/target_type")
		}
		nullable := f.Nullable
		if choice.Nullable != nil {
			nullable = *choice.Nullable
		}
		alias := fmt.Sprintf("field_%d", i)
		project[alias] = "$" + strings.Join(choice.Path, ".")
		field := map[string]interface{}{"source": alias, "target": choice.Target, "target_type": string(typ), "nullable": nullable}
		if choice.Precision != nil {
			if *choice.Precision < 1 {
				return nil, draftError("draft_precision_invalid", path+"/precision")
			}
			field["precision"] = *choice.Precision
		}
		if choice.Scale != nil {
			if *choice.Scale < 0 || (choice.Precision != nil && *choice.Scale > *choice.Precision) {
				return nil, draftError("draft_scale_invalid", path+"/scale")
			}
			field["scale"] = *choice.Scale
		}
		mappings = append(mappings, field)
	}
	pipeline := []interface{}{}
	if unwind != "" {
		pipeline = append(pipeline, map[string]interface{}{"$unwind": "$" + unwind})
	}
	pipeline = append(pipeline, map[string]interface{}{"$project": project})
	statement, err := json.Marshal(map[string]interface{}{"aggregate": facts.mql.Parts[0], "pipeline": pipeline})
	if err != nil || len(statement) > 32768 {
		return nil, draftError("draft_query_too_large", "/fields")
	}
	args := map[string]interface{}{"name": r.Name, "config": map[string]interface{}{
		"runtime": map[string]interface{}{"boundary": "bounded"}, "load": map[string]interface{}{"mode": "snapshot"},
		"source":     map[string]interface{}{"locator": r.SourceLocator, "data_type": "table", "representation": "native", "query": map[string]interface{}{"language": "mql", "statement": string(statement)}},
		"target":     map[string]interface{}{"parent_locator": r.Target.ParentLocator, "name": r.Target.Name, "data_type": "table", "representation": "native", "policy": map[string]interface{}{"apply_mode": r.Target.ApplyMode}},
		"transforms": []interface{}{map[string]interface{}{"type": "field_mapping", "version": "v1", "mode": "project", "fields": mappings}},
	}}
	if r.Description != nil {
		args["description"] = *r.Description
	}
	if r.BatchSize != nil {
		args["batch_size"] = *r.BatchSize
	}
	return args, nil
}

func draftSourceField(fields []datatype.FieldInfo, path []string, unwind string) (*datatype.FieldInfo, error) {
	if len(path) == 0 {
		return nil, fmt.Errorf("empty path")
	}
	for i, part := range path {
		if part == "" || strings.ContainsAny(part, ".$\x00") {
			return nil, fmt.Errorf("ambiguous path")
		}
		var found *datatype.FieldInfo
		for j := range fields {
			if fields[j].Name == part {
				if found != nil {
					return nil, fmt.Errorf("duplicate field")
				}
				found = &fields[j]
			}
		}
		if found == nil {
			return nil, fmt.Errorf("unobserved field")
		}
		if i == len(path)-1 {
			return found, nil
		}
		fields = found.Fields
		if found.Type == datatype.FieldTypeArray {
			if strings.Join(path[:i+1], ".") != unwind || found.Element == nil {
				return nil, fmt.Errorf("array traversal")
			}
			fields = found.Element.Fields
		}
	}
	return nil, fmt.Errorf("unresolved field")
}
