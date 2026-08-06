package queryparams

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"
)

const MQLParameterKey = "$param"

type SQLPlaceholderStyle string

const (
	SQLPlaceholderQuestion SQLPlaceholderStyle = "question"
	SQLPlaceholderDollar   SQLPlaceholderStyle = "dollar"
)

func References(language, query string) ([]string, error) {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "sql":
		return SQLReferences(query)
	case "mql":
		value, err := decodeMQL(query)
		if err != nil {
			return nil, err
		}
		return mqlReferences(value)
	case "cypher":
		return CypherReferences(query)
	default:
		return nil, fmt.Errorf("unsupported parameterized query language: %s", language)
	}
}

func ValidName(name string) bool {
	return validName(name)
}

func SQLReferences(query string) ([]string, error) {
	names := make([]string, 0)
	_, err := scanTextParameters(query, ':', true, false, func(name string) (string, error) {
		names = appendUnique(names, name)
		return ":" + name, nil
	})
	return names, err
}

func BindSQL(query string, parameters map[string]interface{}, style SQLPlaceholderStyle) (string, []interface{}, error) {
	args := make([]interface{}, 0)
	used := map[string]struct{}{}
	bound, err := scanTextParameters(query, ':', true, false, func(name string) (string, error) {
		value, ok := parameters[name]
		if !ok {
			return "", fmt.Errorf("query parameter %q is not provided", name)
		}
		used[name] = struct{}{}
		args = append(args, value)
		switch style {
		case SQLPlaceholderDollar:
			return fmt.Sprintf("$%d", len(args)), nil
		case SQLPlaceholderQuestion:
			return "?", nil
		default:
			return "", fmt.Errorf("unsupported SQL placeholder style: %s", style)
		}
	})
	if err != nil {
		return "", nil, err
	}
	if err := rejectUnused(parameters, used); err != nil {
		return "", nil, err
	}
	return bound, args, nil
}

func CypherReferences(query string) ([]string, error) {
	names := make([]string, 0)
	_, err := scanTextParameters(query, '$', false, true, func(name string) (string, error) {
		names = appendUnique(names, name)
		return "$" + name, nil
	})
	return names, err
}

func ValidateCypher(query string, parameters map[string]interface{}) error {
	names, err := CypherReferences(query)
	if err != nil {
		return err
	}
	return validateExactNames(names, parameters)
}

func BindMQL(query string, parameters map[string]interface{}) (string, error) {
	value, err := decodeMQL(query)
	if err != nil {
		return "", err
	}
	used := map[string]struct{}{}
	bound, err := bindMQLValue(value, parameters, used)
	if err != nil {
		return "", err
	}
	if err := rejectUnused(parameters, used); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(bound)
	if err != nil {
		return "", fmt.Errorf("encode bound MQL: %w", err)
	}
	return string(encoded), nil
}

func ValidateDefinitions(references []string, definitions map[string]interface{}) error {
	return validateExactNames(references, definitions)
}

func scanTextParameters(
	query string,
	prefix byte,
	skipDoublePrefix bool,
	slashLineComments bool,
	replace func(string) (string, error),
) (string, error) {
	var output strings.Builder
	output.Grow(len(query))
	for index := 0; index < len(query); {
		if tag := dollarQuoteTag(query[index:]); tag != "" {
			end := strings.Index(query[index+len(tag):], tag)
			if end < 0 {
				return "", fmt.Errorf("unterminated dollar-quoted string")
			}
			length := len(tag) + end + len(tag)
			output.WriteString(query[index : index+length])
			index += length
			continue
		}
		switch query[index] {
		case '\'', '"', '`':
			end, err := quotedEnd(query, index, query[index])
			if err != nil {
				return "", err
			}
			output.WriteString(query[index:end])
			index = end
			continue
		case '-':
			if index+1 < len(query) && query[index+1] == '-' {
				end := lineEnd(query, index+2)
				output.WriteString(query[index:end])
				index = end
				continue
			}
		case '/':
			if slashLineComments && index+1 < len(query) && query[index+1] == '/' {
				end := lineEnd(query, index+2)
				output.WriteString(query[index:end])
				index = end
				continue
			}
			if index+1 < len(query) && query[index+1] == '*' {
				end := strings.Index(query[index+2:], "*/")
				if end < 0 {
					return "", fmt.Errorf("unterminated block comment")
				}
				end += index + 4
				output.WriteString(query[index:end])
				index = end
				continue
			}
		}

		if query[index] == prefix {
			if skipDoublePrefix && index+1 < len(query) && query[index+1] == prefix {
				output.WriteString(query[index : index+2])
				index += 2
				continue
			}
			if index+1 < len(query) && isIdentifierStart(rune(query[index+1])) {
				end := index + 2
				for end < len(query) && isIdentifierPart(rune(query[end])) {
					end++
				}
				replacement, err := replace(query[index+1 : end])
				if err != nil {
					return "", err
				}
				output.WriteString(replacement)
				index = end
				continue
			}
		}

		output.WriteByte(query[index])
		index++
	}
	return output.String(), nil
}

func quotedEnd(query string, start int, quote byte) (int, error) {
	for index := start + 1; index < len(query); index++ {
		if query[index] == '\\' && index+1 < len(query) {
			index++
			continue
		}
		if query[index] != quote {
			continue
		}
		if index+1 < len(query) && query[index+1] == quote {
			index++
			continue
		}
		return index + 1, nil
	}
	return 0, fmt.Errorf("unterminated quoted string or identifier")
}

func dollarQuoteTag(query string) string {
	if len(query) < 2 || query[0] != '$' {
		return ""
	}
	for index := 1; index < len(query); index++ {
		switch {
		case query[index] == '$':
			return query[:index+1]
		case index == 1 && query[index] >= '0' && query[index] <= '9':
			return ""
		case !isIdentifierPart(rune(query[index])):
			return ""
		}
	}
	return ""
}

func lineEnd(query string, start int) int {
	if end := strings.IndexByte(query[start:], '\n'); end >= 0 {
		return start + end + 1
	}
	return len(query)
}

func decodeMQL(query string) (interface{}, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(query))
	decoder.UseNumber()
	var value interface{}
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("invalid MQL JSON: %w", err)
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("invalid MQL JSON: multiple values")
		}
		return nil, fmt.Errorf("invalid MQL JSON: %w", err)
	}
	if _, ok := value.(map[string]interface{}); !ok {
		return nil, fmt.Errorf("MQL query must be a JSON object")
	}
	return value, nil
}

func mqlReferences(value interface{}) ([]string, error) {
	names := make([]string, 0)
	var visit func(interface{}) error
	visit = func(current interface{}) error {
		switch typed := current.(type) {
		case map[string]interface{}:
			if raw, exists := typed[MQLParameterKey]; exists {
				if len(typed) != 1 {
					return fmt.Errorf("MQL parameter node must contain only %s", MQLParameterKey)
				}
				name, ok := raw.(string)
				if !ok || !validName(name) {
					return fmt.Errorf("invalid MQL parameter name")
				}
				names = appendUnique(names, name)
				return nil
			}
			for _, child := range typed {
				if err := visit(child); err != nil {
					return err
				}
			}
		case []interface{}:
			for _, child := range typed {
				if err := visit(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(value); err != nil {
		return nil, err
	}
	return names, nil
}

func bindMQLValue(value interface{}, parameters map[string]interface{}, used map[string]struct{}) (interface{}, error) {
	switch typed := value.(type) {
	case map[string]interface{}:
		if raw, exists := typed[MQLParameterKey]; exists {
			if len(typed) != 1 {
				return nil, fmt.Errorf("MQL parameter node must contain only %s", MQLParameterKey)
			}
			name, ok := raw.(string)
			if !ok || !validName(name) {
				return nil, fmt.Errorf("invalid MQL parameter name")
			}
			value, ok := parameters[name]
			if !ok {
				return nil, fmt.Errorf("query parameter %q is not provided", name)
			}
			used[name] = struct{}{}
			return value, nil
		}
		result := make(map[string]interface{}, len(typed))
		for key, child := range typed {
			bound, err := bindMQLValue(child, parameters, used)
			if err != nil {
				return nil, err
			}
			result[key] = bound
		}
		return result, nil
	case []interface{}:
		result := make([]interface{}, len(typed))
		for index, child := range typed {
			bound, err := bindMQLValue(child, parameters, used)
			if err != nil {
				return nil, err
			}
			result[index] = bound
		}
		return result, nil
	default:
		return value, nil
	}
}

func validateExactNames(names []string, parameters map[string]interface{}) error {
	used := make(map[string]struct{}, len(names))
	for _, name := range names {
		if _, ok := parameters[name]; !ok {
			return fmt.Errorf("query parameter %q is not provided", name)
		}
		used[name] = struct{}{}
	}
	return rejectUnused(parameters, used)
}

func rejectUnused(parameters map[string]interface{}, used map[string]struct{}) error {
	unused := make([]string, 0)
	for name := range parameters {
		if _, ok := used[name]; !ok {
			unused = append(unused, name)
		}
	}
	if len(unused) == 0 {
		return nil
	}
	sort.Strings(unused)
	return fmt.Errorf("unused query parameters: %s", strings.Join(unused, ", "))
}

func appendUnique(names []string, name string) []string {
	for _, existing := range names {
		if existing == name {
			return names
		}
	}
	return append(names, name)
}

func validName(name string) bool {
	if name == "" || !isIdentifierStart(rune(name[0])) {
		return false
	}
	for _, char := range name[1:] {
		if !isIdentifierPart(char) {
			return false
		}
	}
	return true
}

func isIdentifierStart(char rune) bool {
	return char == '_' || unicode.IsLetter(char)
}

func isIdentifierPart(char rune) bool {
	return isIdentifierStart(char) || unicode.IsDigit(char)
}
