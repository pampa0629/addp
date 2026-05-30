package pluginmanifest

import (
	"encoding/json"
	"fmt"
	"sort"
)

func ValidateTopLevelFields(raw []byte, allowedFields ...string) error {
	allowed := make(map[string]struct{}, len(allowedFields))
	for _, field := range allowedFields {
		allowed[field] = struct{}{}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	unknown := make([]string, 0)
	for key := range fields {
		if _, ok := allowed[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf("unsupported plugin config fields: %v", unknown)
}
