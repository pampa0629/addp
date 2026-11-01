package federatedquery

import (
	"sort"
	"strings"
	"unicode"

	"github.com/addp/common/engine/plugin"
)

// ReferencedSourceEngineIDs discovers candidate source scopes before admission.
// It is not a native read-set proof: the runtime must still validate its complete
// plan against exactly the frozen/authorized mounts. Strings and comments are
// ignored; quoted physical identifiers retain their original names.
func ReferencedSourceEngineIDs(query string, candidates []plugin.FederatedQuerySource, supported []string) []uint {
	referenced := map[string]bool{}
	for _, parts := range QualifiedReferences(query) {
		referenced[strings.ToLower(parts[0])] = true
	}
	types := map[string]bool{}
	for _, engineType := range supported {
		types[engineType] = true
	}
	aliases := map[string]uint{}
	ids := map[uint]bool{}
	for _, candidate := range candidates {
		alias := strings.ToLower(SanitizeIdentifier(candidate.Name))
		if candidate.ID == 0 || candidate.LifecycleState != "active" || !types[candidate.EngineType] || !referenced[alias] {
			continue
		}
		if previous, exists := aliases[alias]; exists && previous != candidate.ID {
			return nil
		}
		aliases[alias], ids[candidate.ID] = candidate.ID, true
	}
	result := make([]uint, 0, len(ids))
	for id := range ids {
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

// QualifiedReferences returns lexical multipart names, never a native SQL
// authorization proof. Both federation providers consume this same discovery.
func QualifiedReferences(query string) [][]string {
	tokens := sourceTokens(query)
	var result [][]string
	for i := 0; i < len(tokens); {
		if tokens[i] == "" || tokens[i] == "." {
			i++
			continue
		}
		parts := []string{tokens[i]}
		end := i + 1
		for end+1 < len(tokens) && tokens[end] == "." && tokens[end+1] != "" && tokens[end+1] != "." {
			parts = append(parts, tokens[end+1])
			end += 2
		}
		if len(parts) > 1 {
			result = append(result, parts)
		}
		i = end
	}
	return result
}

func sourceTokens(query string) []string {
	runes := []rune(query)
	var result []string
	for i := 0; i < len(runes); {
		switch {
		case i+1 < len(runes) && runes[i] == '-' && runes[i+1] == '-':
			i += 2
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
		case i+1 < len(runes) && runes[i] == '/' && runes[i+1] == '*':
			i += 2
			depth := 1
			for i < len(runes) && depth > 0 {
				if i+1 < len(runes) && runes[i] == '/' && runes[i+1] == '*' {
					depth++
					i += 2
				} else if i+1 < len(runes) && runes[i] == '*' && runes[i+1] == '/' {
					depth--
					i += 2
				} else {
					i++
				}
			}
			if depth != 0 {
				return nil
			}
		case runes[i] == '\'' || runes[i] == '"' || runes[i] == '`':
			quote := runes[i]
			i++
			var token strings.Builder
			closed := false
			for i < len(runes) {
				if runes[i] == quote {
					i++
					if i < len(runes) && runes[i] == quote {
						token.WriteRune(quote)
						i++
						continue
					}
					closed = true
					break
				}
				if quote == '\'' && runes[i] == '\\' && i+1 < len(runes) {
					i += 2
					continue
				}
				token.WriteRune(runes[i])
				i++
			}
			if !closed {
				return nil
			}
			if quote != '\'' {
				result = append(result, token.String())
			} else {
				result = append(result, "")
			}
		case runes[i] == '.':
			result = append(result, ".")
			i++
		case runes[i] == '_' || unicode.IsLetter(runes[i]):
			start := i
			i++
			for i < len(runes) && (runes[i] == '_' || unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i])) {
				i++
			}
			result = append(result, string(runes[start:i]))
		default:
			// Keep punctuation as a boundary, so identifiers cannot join across it.
			if !unicode.IsSpace(runes[i]) {
				result = append(result, "")
			}
			i++
		}
	}
	return result
}
