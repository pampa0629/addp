package preview

import (
	"encoding/json"
	"fmt"
)

// BigintJSONText preserves the public exact-text contract, including protected output.
func BigintJSONText(value interface{}) (string, error) {
	switch value := value.(type) {
	case string:
		// Includes protection output; do not reinterpret masked text as a number.
		return value, nil
	case json.Number:
		if _, err := value.Int64(); err != nil {
			return "", fmt.Errorf("bigint JSON number must be an exact int64")
		}
		return value.String(), nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprint(value), nil
	default:
		return "", fmt.Errorf("bigint requires an integer or exact text, got %T", value)
	}
}
