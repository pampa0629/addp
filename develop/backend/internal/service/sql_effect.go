package service

import (
	"fmt"
	"strings"
	"unicode"
)

type SQLExecutionEffect string

const (
	SQLExecutionEffectRead           SQLExecutionEffect = "read"
	SQLExecutionEffectWrite          SQLExecutionEffect = "write"
	SQLExecutionEffectDDL            SQLExecutionEffect = "ddl"
	SQLExecutionEffectExternalEffect SQLExecutionEffect = "external_effect"
)

type sqlEffectToken struct {
	word  string
	depth int
}

var sqlEffectKeywords = map[string]SQLExecutionEffect{
	"SELECT":   SQLExecutionEffectRead,
	"SHOW":     SQLExecutionEffectRead,
	"DESC":     SQLExecutionEffectRead,
	"DESCRIBE": SQLExecutionEffectRead,
	"INSERT":   SQLExecutionEffectWrite,
	"UPDATE":   SQLExecutionEffectWrite,
	"DELETE":   SQLExecutionEffectWrite,
	"MERGE":    SQLExecutionEffectWrite,
	"CREATE":   SQLExecutionEffectDDL,
	"ALTER":    SQLExecutionEffectDDL,
	"DROP":     SQLExecutionEffectDDL,
	"TRUNCATE": SQLExecutionEffectDDL,
	"COMMENT":  SQLExecutionEffectDDL,
	"GRANT":    SQLExecutionEffectDDL,
	"REVOKE":   SQLExecutionEffectDDL,
	"VACUUM":   SQLExecutionEffectDDL,
	"ANALYZE":  SQLExecutionEffectDDL,
	"REINDEX":  SQLExecutionEffectDDL,
	"CLUSTER":  SQLExecutionEffectDDL,
	"CALL":     SQLExecutionEffectExternalEffect,
	"DO":       SQLExecutionEffectExternalEffect,
	"COPY":     SQLExecutionEffectExternalEffect,
	"LOAD":     SQLExecutionEffectExternalEffect,
	"ATTACH":   SQLExecutionEffectExternalEffect,
	"INSTALL":  SQLExecutionEffectExternalEffect,
}

// ClassifySQLExecutionEffect classifies one SQL statement into the fixed ADDP
// execution-effect vocabulary. It is intentionally strict: unsupported syntax,
// multiple statements, and malformed lexical structures are rejected.
func ClassifySQLExecutionEffect(sql string) (SQLExecutionEffect, error) {
	tokens, err := tokenizeSQLForEffect(sql)
	if err != nil {
		return "", err
	}
	if len(tokens) == 0 {
		return "", fmt.Errorf("SQL 语句不能为空")
	}

	statementIndex, err := primarySQLStatementIndex(tokens)
	if err != nil {
		return "", err
	}
	statement := tokens[statementIndex].word
	if statement == "EXPLAIN" {
		statementIndex, err = explainedSQLStatementIndex(tokens, statementIndex+1)
		if err != nil {
			return "", err
		}
		statement = tokens[statementIndex].word
	}

	effect, exists := sqlEffectKeywords[statement]
	if !exists {
		return "", fmt.Errorf("不支持或无法可靠判定 SQL 效果: %s", statement)
	}
	if statement == "SELECT" {
		return classifySelectEffect(tokens[statementIndex:]), nil
	}
	return effect, nil
}

func primarySQLStatementIndex(tokens []sqlEffectToken) (int, error) {
	first := firstTopLevelToken(tokens, 0)
	if first < 0 {
		return -1, fmt.Errorf("SQL 语句缺少可执行主语句")
	}
	if tokens[first].word != "WITH" {
		return first, nil
	}
	for index := first + 1; index < len(tokens); index++ {
		if tokens[index].depth != 0 {
			continue
		}
		if _, exists := sqlEffectKeywords[tokens[index].word]; exists || tokens[index].word == "EXPLAIN" {
			return index, nil
		}
	}
	return -1, fmt.Errorf("WITH 语句缺少可执行主语句")
}

func explainedSQLStatementIndex(tokens []sqlEffectToken, start int) (int, error) {
	for index := start; index < len(tokens); index++ {
		if tokens[index].depth != 0 {
			continue
		}
		switch tokens[index].word {
		case "SELECT", "SHOW", "DESC", "DESCRIBE", "INSERT", "UPDATE", "DELETE", "MERGE",
			"CREATE", "ALTER", "DROP", "TRUNCATE", "COMMENT", "GRANT", "REVOKE", "VACUUM",
			"REINDEX", "CLUSTER", "CALL", "DO", "COPY", "LOAD", "ATTACH", "INSTALL":
			return index, nil
		}
		if tokens[index].word == "WITH" {
			inner, err := primarySQLStatementIndex(tokens[index:])
			if err != nil {
				return -1, err
			}
			return index + inner, nil
		}
	}
	return -1, fmt.Errorf("EXPLAIN 缺少可判定的目标语句")
}

func classifySelectEffect(tokens []sqlEffectToken) SQLExecutionEffect {
	for index := 1; index < len(tokens); index++ {
		if tokens[index].depth != 0 {
			continue
		}
		switch tokens[index].word {
		case "INTO":
			return SQLExecutionEffectExternalEffect
		case "FOR":
			if next := nextTopLevelWord(tokens, index+1); next == "UPDATE" || next == "SHARE" {
				return SQLExecutionEffectWrite
			}
		case "LOCK":
			if next := nextTopLevelWord(tokens, index+1); next == "IN" {
				return SQLExecutionEffectWrite
			}
		}
	}
	return SQLExecutionEffectRead
}

func firstTopLevelToken(tokens []sqlEffectToken, start int) int {
	for index := start; index < len(tokens); index++ {
		if tokens[index].depth == 0 {
			return index
		}
	}
	return -1
}

func nextTopLevelWord(tokens []sqlEffectToken, start int) string {
	if index := firstTopLevelToken(tokens, start); index >= 0 {
		return tokens[index].word
	}
	return ""
}

func tokenizeSQLForEffect(sql string) ([]sqlEffectToken, error) {
	var tokens []sqlEffectToken
	depth := 0
	terminated := false
	for index := 0; index < len(sql); {
		current := sql[index]
		if unicode.IsSpace(rune(current)) {
			index++
			continue
		}
		if current == '-' && index+1 < len(sql) && sql[index+1] == '-' {
			index += 2
			for index < len(sql) && sql[index] != '\n' && sql[index] != '\r' {
				index++
			}
			continue
		}
		if current == '/' && index+1 < len(sql) && sql[index+1] == '*' {
			next, err := skipSQLBlockComment(sql, index)
			if err != nil {
				return nil, err
			}
			index = next
			continue
		}
		if terminated {
			return nil, fmt.Errorf("一次执行只允许一条 SQL 语句")
		}
		if current == '\'' || current == '"' || current == '`' {
			next, err := skipSQLQuoted(sql, index, current)
			if err != nil {
				return nil, err
			}
			index = next
			continue
		}
		if current == '[' {
			next := strings.IndexByte(sql[index+1:], ']')
			if next < 0 {
				return nil, fmt.Errorf("SQL 方括号标识符未闭合")
			}
			index += next + 2
			continue
		}
		if current == '$' {
			if delimiter, ok := sqlDollarQuoteDelimiter(sql[index:]); ok {
				end := strings.Index(sql[index+len(delimiter):], delimiter)
				if end < 0 {
					return nil, fmt.Errorf("SQL dollar quote 未闭合")
				}
				index += len(delimiter) + end + len(delimiter)
				continue
			}
		}
		switch current {
		case '(':
			depth++
			index++
			continue
		case ')':
			if depth == 0 {
				return nil, fmt.Errorf("SQL 括号不匹配")
			}
			depth--
			index++
			continue
		case ';':
			if depth != 0 {
				return nil, fmt.Errorf("SQL 子表达式中不允许语句分隔符")
			}
			terminated = true
			index++
			continue
		}
		if isSQLWordStart(current) {
			start := index
			index++
			for index < len(sql) && isSQLWordPart(sql[index]) {
				index++
			}
			tokens = append(tokens, sqlEffectToken{word: strings.ToUpper(sql[start:index]), depth: depth})
			continue
		}
		index++
	}
	if depth != 0 {
		return nil, fmt.Errorf("SQL 括号未闭合")
	}
	return tokens, nil
}

func skipSQLBlockComment(sql string, start int) (int, error) {
	depth := 1
	for index := start + 2; index < len(sql); {
		if index+1 < len(sql) && sql[index] == '/' && sql[index+1] == '*' {
			depth++
			index += 2
			continue
		}
		if index+1 < len(sql) && sql[index] == '*' && sql[index+1] == '/' {
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

func skipSQLQuoted(sql string, start int, quote byte) (int, error) {
	for index := start + 1; index < len(sql); index++ {
		if sql[index] == '\\' && quote == '\'' && index+1 < len(sql) {
			index++
			continue
		}
		if sql[index] != quote {
			continue
		}
		if index+1 < len(sql) && sql[index+1] == quote {
			index++
			continue
		}
		return index + 1, nil
	}
	return 0, fmt.Errorf("SQL 引号未闭合")
}

func sqlDollarQuoteDelimiter(sql string) (string, bool) {
	if len(sql) < 2 || sql[0] != '$' {
		return "", false
	}
	for index := 1; index < len(sql); index++ {
		if sql[index] == '$' {
			return sql[:index+1], true
		}
		if !(sql[index] == '_' || sql[index] >= 'a' && sql[index] <= 'z' || sql[index] >= 'A' && sql[index] <= 'Z' || index > 1 && sql[index] >= '0' && sql[index] <= '9') {
			return "", false
		}
	}
	return "", false
}

func isSQLWordStart(value byte) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func isSQLWordPart(value byte) bool {
	return isSQLWordStart(value) || value >= '0' && value <= '9' || value == '$'
}
