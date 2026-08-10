package query

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"unicode"
)

type Effect string

const (
	Read           Effect = "read"
	Write          Effect = "write"
	DDL            Effect = "ddl"
	ExternalEffect Effect = "external_effect"
)

// Analysis describes the safety-relevant facts that can be derived from one
// SQL statement. It intentionally reports uncertainty instead of widening a
// permission decision when the statement cannot be understood reliably.
type Analysis struct {
	Effect                   Effect   `json:"effect"`
	Statement                string   `json:"statement"`
	ClassificationConfidence string   `json:"classification_confidence"`
	TargetObjects            []string `json:"target_objects,omitempty"`
	Warnings                 []string `json:"warnings,omitempty"`
	Fingerprint              string   `json:"fingerprint"`
	RequiresConfirmation     bool     `json:"requires_confirmation"`
}

type token struct {
	word  string
	depth int
}

var keywords = map[string]Effect{
	"SELECT": Read, "SHOW": Read, "DESC": Read, "DESCRIBE": Read,
	"INSERT": Write, "UPDATE": Write, "DELETE": Write, "MERGE": Write,
	"CREATE": DDL, "ALTER": DDL, "DROP": DDL, "TRUNCATE": DDL, "COMMENT": DDL,
	"GRANT": DDL, "REVOKE": DDL, "VACUUM": DDL, "ANALYZE": DDL, "REINDEX": DDL, "CLUSTER": DDL,
	"CALL": ExternalEffect, "DO": ExternalEffect, "COPY": ExternalEffect, "LOAD": ExternalEffect,
	"ATTACH": ExternalEffect, "INSTALL": ExternalEffect,
}

// Classify returns the effect of exactly one SQL statement. Unsupported or
// malformed syntax is rejected instead of being treated as read-only.
func Classify(sql string) (Effect, error) {
	analysis, err := Analyze(sql)
	if err != nil {
		return "", err
	}
	return analysis.Effect, nil
}

// Analyze classifies exactly one SQL statement and extracts conservative
// execution-safety facts for preflight and audit displays.
func Analyze(sql string) (Analysis, error) {
	tokens, err := tokenize(sql)
	if err != nil {
		return Analysis{}, err
	}
	if len(tokens) == 0 {
		return Analysis{}, fmt.Errorf("SQL 语句不能为空")
	}

	statementIndex, err := primaryStatementIndex(tokens)
	if err != nil {
		return Analysis{}, err
	}
	statement := tokens[statementIndex].word
	if statement == "EXPLAIN" {
		statementIndex, err = explainedStatementIndex(tokens, statementIndex+1)
		if err != nil {
			return Analysis{}, err
		}
		statement = tokens[statementIndex].word
	}

	effect, exists := keywords[statement]
	if !exists {
		return Analysis{}, fmt.Errorf("不支持或无法可靠判定 SQL 效果: %s", statement)
	}
	if statement == "SELECT" {
		effect = classifySelect(tokens[statementIndex:])
	}

	analysis := Analysis{
		Effect:                   effect,
		Statement:                statement,
		ClassificationConfidence: "high",
		TargetObjects:            extractTargetObjects(tokens, statementIndex, statement),
		Fingerprint:              fingerprint(sql),
	}
	if len(analysis.TargetObjects) == 0 {
		analysis.ClassificationConfidence = "unknown"
		analysis.Warnings = append(analysis.Warnings, "target_unknown")
	}
	if (statement == "DELETE" || statement == "UPDATE") && !hasTopLevelWord(tokens[statementIndex:], "WHERE") {
		analysis.Warnings = append(analysis.Warnings, "missing_where")
		analysis.RequiresConfirmation = true
	}
	if effect == Write || effect == DDL || effect == ExternalEffect {
		analysis.RequiresConfirmation = true
	}
	return analysis, nil
}

func RequireReadOnly(sql string) error {
	effect, err := Classify(sql)
	if err != nil {
		return err
	}
	if effect != Read {
		return fmt.Errorf("只读查询不允许 SQL 效果 %s", effect)
	}
	return nil
}

func primaryStatementIndex(tokens []token) (int, error) {
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
		if _, exists := keywords[tokens[index].word]; exists || tokens[index].word == "EXPLAIN" {
			return index, nil
		}
	}
	return -1, fmt.Errorf("WITH 语句缺少可执行主语句")
}

func explainedStatementIndex(tokens []token, start int) (int, error) {
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
			inner, err := primaryStatementIndex(tokens[index:])
			if err != nil {
				return -1, err
			}
			return index + inner, nil
		}
	}
	return -1, fmt.Errorf("EXPLAIN 缺少可判定的目标语句")
}

func classifySelect(tokens []token) Effect {
	for index := 1; index < len(tokens); index++ {
		if tokens[index].depth != 0 {
			continue
		}
		switch tokens[index].word {
		case "INTO":
			return ExternalEffect
		case "FOR":
			if next := nextTopLevelWord(tokens, index+1); next == "UPDATE" || next == "SHARE" {
				return Write
			}
		case "LOCK":
			if next := nextTopLevelWord(tokens, index+1); next == "IN" {
				return Write
			}
		}
	}
	return Read
}

func hasTopLevelWord(tokens []token, word string) bool {
	for _, item := range tokens {
		if item.depth == 0 && item.word == word {
			return true
		}
	}
	return false
}

func extractTargetObjects(tokens []token, statementIndex int, statement string) []string {
	markers := map[string][]string{
		"SELECT":   {"FROM", "JOIN", "INTO", "UPDATE"},
		"INSERT":   {"INTO"},
		"UPDATE":   {"UPDATE"},
		"DELETE":   {"FROM"},
		"MERGE":    {"INTO", "USING"},
		"CREATE":   {"TABLE", "VIEW", "INDEX", "DATABASE", "SCHEMA"},
		"ALTER":    {"TABLE", "VIEW", "INDEX", "DATABASE", "SCHEMA"},
		"DROP":     {"TABLE", "VIEW", "INDEX", "DATABASE", "SCHEMA"},
		"TRUNCATE": {"TABLE"},
	}
	allowed := make(map[string]struct{}, len(markers[statement]))
	for _, marker := range markers[statement] {
		allowed[marker] = struct{}{}
	}
	seen := make(map[string]struct{})
	var targets []string
	for index := statementIndex + 1; index < len(tokens); index++ {
		if tokens[index].depth != 0 {
			continue
		}
		if _, ok := allowed[tokens[index].word]; !ok {
			continue
		}
		candidateIndex := index + 1
		for candidateIndex < len(tokens) && tokens[candidateIndex].depth == 0 {
			switch tokens[candidateIndex].word {
			case "ONLY", "IF", "NOT", "EXISTS", "CONCURRENTLY":
				candidateIndex++
				continue
			}
			break
		}
		if candidateIndex >= len(tokens) || tokens[candidateIndex].depth != 0 {
			continue
		}
		candidate := tokens[candidateIndex].word
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		targets = append(targets, candidate)
	}
	return targets
}

func fingerprint(sql string) string {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(sql)), " ")
	digest := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("%x", digest[:])
}

func firstTopLevelToken(tokens []token, start int) int {
	for index := start; index < len(tokens); index++ {
		if tokens[index].depth == 0 {
			return index
		}
	}
	return -1
}

func nextTopLevelWord(tokens []token, start int) string {
	if index := firstTopLevelToken(tokens, start); index >= 0 {
		return tokens[index].word
	}
	return ""
}

func tokenize(sql string) ([]token, error) {
	var tokens []token
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
			next, err := skipBlockComment(sql, index)
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
			next, err := skipQuoted(sql, index, current)
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
			if delimiter, ok := dollarQuoteDelimiter(sql[index:]); ok {
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
		if isWordStart(current) {
			start := index
			index++
			for index < len(sql) && isWordPart(sql[index]) {
				index++
			}
			tokens = append(tokens, token{word: strings.ToUpper(sql[start:index]), depth: depth})
			continue
		}
		index++
	}
	if depth != 0 {
		return nil, fmt.Errorf("SQL 括号未闭合")
	}
	return tokens, nil
}

func skipBlockComment(sql string, start int) (int, error) {
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

func skipQuoted(sql string, start int, quote byte) (int, error) {
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

func dollarQuoteDelimiter(sql string) (string, bool) {
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

func isWordStart(value byte) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func isWordPart(value byte) bool {
	return isWordStart(value) || value >= '0' && value <= '9' || value == '$'
}
