package falkor

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

var parameterName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
var graphKeyPattern = regexp.MustCompile(`^ontology:t[1-9][0-9]*:[a-z][a-z0-9_]{0,63}:r[1-9][0-9]*:g[0-9a-f]{32}$`)

// Encode a deliberately small Cypher literal subset, never fmt.Sprint on data.
// Keys are validated identifiers, strings use JSON double-quote escaping,
// and integer values never pass through float64.
func parameters(params map[string]any) (string, error) {
	if len(params) == 0 {
		return "", nil
	}
	if len(params) > 16 {
		return "", ErrInvalid
	}
	keys := sortedKeys(params)
	var out strings.Builder
	out.WriteString("CYPHER ")
	for _, key := range keys {
		if !parameterName.MatchString(key) {
			return "", ErrInvalid
		}
		value, err := literal(params[key], 0)
		if err != nil {
			return "", err
		}
		out.WriteString(key + "=" + value + " ")
		if out.Len() > 2<<20 {
			return "", ErrInvalid
		}
	}
	return out.String(), nil
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func literal(value any, depth int) (string, error) {
	if depth > 4 {
		return "", ErrInvalid
	}
	switch v := value.(type) {
	case string:
		if len(v) > 8192 || !utf8.ValidString(v) {
			return "", ErrInvalid
		}
		encoded, _ := json.Marshal(v)
		return string(encoded), nil
	case bool:
		return strconv.FormatBool(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case []any:
		if len(v) > 4096 {
			return "", ErrInvalid
		}
		items := make([]string, 0, len(v))
		size := 0
		for _, item := range v {
			encoded, err := literal(item, depth+1)
			if err != nil {
				return "", err
			}
			size += len(encoded) + 1
			if size > 2<<20 {
				return "", ErrInvalid
			}
			items = append(items, encoded)
		}
		return "[" + strings.Join(items, ",") + "]", nil
	case map[string]any:
		if len(v) > 16 {
			return "", ErrInvalid
		}
		items := make([]string, 0, len(v))
		for _, key := range sortedKeys(v) {
			if !parameterName.MatchString(key) {
				return "", ErrInvalid
			}
			encoded, err := literal(v[key], depth+1)
			if err != nil {
				return "", err
			}
			items = append(items, key+":"+encoded)
		}
		result := "{" + strings.Join(items, ",") + "}"
		if len(result) > 2<<20 {
			return "", ErrInvalid
		}
		return result, nil
	default:
		return "", ErrInvalid
	}
}
