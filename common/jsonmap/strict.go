package jsonmap

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

// DecodeObjectStrict preserves JSON numbers and rejects ambiguous duplicate
// members, invalid UTF-8, multiple values and unbounded nesting. Typed callers
// still own their domain fields and closed-schema validation.
func DecodeObjectStrict(raw []byte, maxBytes, maxDepth int) (map[string]any, error) {
	invalid := errors.New("invalid bounded JSON object")
	if maxBytes <= 0 || maxDepth <= 0 || len(raw) == 0 || len(raw) > maxBytes || !utf8.Valid(raw) {
		return nil, invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := uniqueJSONValue(decoder, 0, maxDepth); err != nil {
		return nil, invalid
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, invalid
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object map[string]any
	if decoder.Decode(&object) != nil || object == nil {
		return nil, invalid
	}
	return object, nil
}

func uniqueJSONValue(decoder *json.Decoder, depth, maxDepth int) error {
	if depth > maxDepth {
		return errors.New("JSON nesting exceeds boundary")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	if delim != '{' && delim != '[' {
		return errors.New("unexpected JSON delimiter")
	}
	seen := make(map[string]bool)
	for decoder.More() {
		if delim == '{' {
			key, err := decoder.Token()
			name, ok := key.(string)
			if err != nil || !ok || seen[name] {
				return errors.New("invalid or duplicate JSON member")
			}
			seen[name] = true
		}
		if err := uniqueJSONValue(decoder, depth+1, maxDepth); err != nil {
			return err
		}
	}
	end, err := decoder.Token()
	if err != nil || (delim == '{' && end != json.Delim('}')) || (delim == '[' && end != json.Delim(']')) {
		return errors.New("unclosed JSON value")
	}
	return nil
}
