package planner

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/addp/common/format"
	"github.com/addp/common/resourcetree"
	"github.com/google/uuid"
)

// This private source can only live inside a Develop export session's prefix.
func ValidateWorkflowResultExport(spec TableExportTaskSpec, tenant uint) error {
	r := spec.WorkflowResult
	if r.Validate() != nil || spec.Runtime.Boundary != "bounded" || spec.Load.Mode != "snapshot" || spec.Load.ChangeDetection != nil || spec.FileGDBSource != nil || spec.Source.DataType != "table" || spec.Source.Representation != "encoded" || spec.Source.Format != format.FormatJSON || spec.Source.Query != nil || len(spec.Source.ProtectionChoices) != 0 || len(spec.Source.Options) != 0 || len(spec.Source.Policy) != 0 || spec.Target.DataType != "table" || spec.Target.Representation != "encoded" || spec.Target.Format != format.FormatCSV || spec.Target.Locator != "" || spec.Target.OverridePolicy != "" || len(spec.Target.Options) != 0 || len(spec.Target.Policy) != 1 || spec.Target.Policy["apply_mode"] != "replace" {
		return fmt.Errorf("invalid workflow result export shape")
	}
	parent, err := ParseInfraLocatorURI(spec.Target.ParentLocator)
	if err != nil || parent.Kind != "minio" || len(parent.Path) != 5 || parent.Type != resourcetree.TypePrefix || parent.Path[1] != "export" || parent.Path[2] != "develop" {
		return fmt.Errorf("invalid workflow export prefix")
	}
	if tenant != 0 && parent.Path[0] != fmt.Sprintf("tenant_%d", tenant) {
		return fmt.Errorf("workflow export tenant mismatch")
	}
	if !strings.HasPrefix(parent.Path[0], "tenant_") {
		return fmt.Errorf("invalid workflow export tenant")
	}
	if _, err := uuid.Parse(parent.Path[4]); err != nil {
		return fmt.Errorf("invalid workflow export session prefix")
	}
	expected, err := parent.Child("workflow-result.json", resourcetree.TypeObject)
	actual, e := ParseInfraLocatorURI(spec.Source.Locator)
	if err != nil || e != nil || !reflect.DeepEqual(actual, expected) {
		return fmt.Errorf("workflow input is outside export session")
	}
	if len(spec.Transforms) != 1 || spec.Transforms[0].Type != "field_mapping" || spec.Transforms[0].Mode != "project" || len(spec.Transforms[0].Fields) != len(r.Columns) {
		return fmt.Errorf("workflow export requires identity projection")
	}
	for i, f := range spec.Transforms[0].Fields {
		if f.Source != r.Columns[i] || f.Target != r.Columns[i] || f.TargetType != "unknown" {
			return fmt.Errorf("workflow export changed columns")
		}
	}
	return nil
}
