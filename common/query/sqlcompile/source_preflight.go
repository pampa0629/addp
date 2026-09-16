package sqlcompile

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

// ValidateBoundFields compares only the physical facts frozen by compilation.
// Display metadata and unrelated columns are not analytical dependencies.
func ValidateBoundFields(source plugin.SourceBinding, current []datatype.FieldInfo) error {
	byName := make(map[string]datatype.FieldInfo, len(current))
	for _, f := range current {
		byName[f.Name] = f
	}
	for _, binding := range source.Columns {
		want := binding.Field
		got, ok := byName[want.Name]
		if !ok || len(want.Path) != 1 || want.Path[0] != want.Name || got.Type != want.Type || got.NativeType != want.NativeType || got.Nullable != want.Nullable || got.Size != want.Size || got.Precision != want.Precision || got.Scale != want.Scale {
			return plugin.ErrAnalyticalPlanChanged
		}
	}
	return nil
}
