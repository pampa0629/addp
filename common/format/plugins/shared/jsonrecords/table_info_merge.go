package jsonrecords

import "github.com/addp/common/datatype"

// MergeTableInfo combines schemas observed in successive nonempty record batches.
// Missing fields remain nullable; a null-only field can acquire its actual type.
func MergeTableInfo(current, next *datatype.TableInfo) *datatype.TableInfo {
	if current == nil {
		return next.Clone()
	}
	if next == nil {
		return current.Clone()
	}
	merged := current.Clone()
	nextFields := next.Clone().Fields
	byName := make(map[string]datatype.FieldInfo, len(nextFields))
	for _, field := range nextFields {
		byName[field.Name] = field
	}
	for i := range merged.Fields {
		field := &merged.Fields[i]
		observed, present := byName[field.Name]
		field.Nullable = field.Nullable || !present || observed.Nullable
		if present && field.Type == datatype.FieldTypeUnknown {
			field.Type = observed.Type
		}
		delete(byName, field.Name)
	}
	for _, field := range nextFields {
		if _, added := byName[field.Name]; added {
			field.Nullable = true // Previous batches did not contain this field.
			merged.Fields = append(merged.Fields, field)
			delete(byName, field.Name)
		}
	}
	return merged
}
