package service

import (
	"fmt"
	"strings"
)

// maskPostgreSQLNamedParameters keeps byte offsets stable while replacing
// Develop's :name parameters with PostgreSQL literals for AST parsing only.
func maskPostgreSQLNamedParameters(query string) (string, error) {
	masked := []byte(query)
	for index := 0; index < len(masked); {
		switch {
		case masked[index] == '-' && index+1 < len(masked) && masked[index+1] == '-':
			index += 2
			for index < len(masked) && masked[index] != '\n' && masked[index] != '\r' {
				index++
			}
		case masked[index] == '/' && index+1 < len(masked) && masked[index+1] == '*':
			end, err := skipRelationBlockComment(masked, index)
			if err != nil {
				return "", err
			}
			index = end
		case masked[index] == '\'':
			end, err := skipRelationQuoted(masked, index, '\'')
			if err != nil {
				return "", err
			}
			index = end
		case masked[index] == '"':
			end, err := skipRelationQuoted(masked, index, '"')
			if err != nil {
				return "", err
			}
			index = end
		case masked[index] == '$':
			delimiter, ok := relationDollarQuoteDelimiter(masked[index:])
			if !ok {
				index++
				continue
			}
			closing := strings.Index(string(masked[index+len(delimiter):]), delimiter)
			if closing < 0 {
				return "", fmt.Errorf("SQL dollar quote 未闭合")
			}
			index += len(delimiter) + closing + len(delimiter)
		case masked[index] == ':' &&
			(index == 0 || masked[index-1] != ':') &&
			index+1 < len(masked) &&
			masked[index+1] != ':' &&
			isRelationIdentifierStart(masked[index+1]):
			end := index + 2
			for end < len(masked) && isRelationIdentifierPart(masked[end]) {
				end++
			}
			masked[index] = '0'
			for fill := index + 1; fill < end; fill++ {
				masked[fill] = ' '
			}
			index = end
		default:
			index++
		}
	}
	return string(masked), nil
}

func skipRelationQuoted(query []byte, start int, quote byte) (int, error) {
	for index := start + 1; index < len(query); index++ {
		if quote == '\'' && query[index] == '\\' && index+1 < len(query) {
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
	return 0, fmt.Errorf("SQL 引号未闭合")
}

func skipRelationBlockComment(query []byte, start int) (int, error) {
	depth := 1
	for index := start + 2; index < len(query); {
		if index+1 < len(query) && query[index] == '/' && query[index+1] == '*' {
			depth++
			index += 2
			continue
		}
		if index+1 < len(query) && query[index] == '*' && query[index+1] == '/' {
			depth--
			index += 2
			if depth == 0 {
				return index, nil
			}
			continue
		}
		index++
	}
	return 0, fmt.Errorf("SQL 块注释未闭合")
}

func relationDollarQuoteDelimiter(query []byte) (string, bool) {
	if len(query) < 2 || query[0] != '$' {
		return "", false
	}
	for index := 1; index < len(query); index++ {
		if query[index] == '$' {
			return string(query[:index+1]), true
		}
		if !(query[index] == '_' || query[index] >= 'a' && query[index] <= 'z' || query[index] >= 'A' && query[index] <= 'Z' || index > 1 && query[index] >= '0' && query[index] <= '9') {
			return "", false
		}
	}
	return "", false
}

func isRelationIdentifierStart(value byte) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func isRelationIdentifierPart(value byte) bool {
	return isRelationIdentifierStart(value) || value >= '0' && value <= '9'
}
