package service

import "strings"

// Meilisearch preserves the field array order in _formatted. Pair the actual
// highlighted definition with its indexed facts, without interpreting the query.
func matchedSearchFields(raw, formatted interface{}) []SearchFieldMatch {
	fields, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	highlighted, ok := formatted.([]interface{})
	if !ok || len(fields) != len(highlighted) {
		return nil
	}
	var matches []SearchFieldMatch
	for index, entry := range fields {
		field, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		formattedField, ok := highlighted[index].(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := field["name"].(string)
		dataType, _ := field["data_type"].(string)
		comment, _ := field["comment"].(string)
		if name == "" {
			continue
		}
		highlights := make(map[string]string)
		for _, key := range []string{"name", "data_type", "comment"} {
			original, _ := field[key].(string)
			value, ok := formattedField[key].(string)
			if ok && value != original && strings.Contains(value, "<mark>") && strings.Contains(value, "</mark>") {
				highlights[key] = value
			}
		}
		if len(highlights) != 0 {
			matches = append(matches, SearchFieldMatch{Name: name, DataType: dataType, Comment: comment, Highlights: highlights})
		}
	}
	return matches
}
