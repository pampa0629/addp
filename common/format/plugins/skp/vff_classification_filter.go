package skp

// Fixed syntax facts only. Names are not resolved against XSD types, and no
// SketchUp UI state or complete resource closure is inferred from these rules.
import (
	"context"
	"crypto/sha256"
	"fmt"
	"unicode/utf8"
)

type vffSKCFilterLimits struct{ bytes, tokens, rules, attributes int }
type vffSKCFilterBudget struct {
	limits                           vffSKCFilterLimits
	bytes, tokens, rules, attributes int
}
type vffSKCFilterRule struct {
	typeName            string // Empty means global attribute blacklist.
	attributesSpecified bool
	attributes          []string
	begin, end          int
	sha256              [32]byte
}
type vffSKCFilterFact struct {
	vffSKCMemberFact
	rules []vffSKCFilterRule
}
type vffSKCFilterToken struct {
	value      string
	begin, end int
}

func readVFFSKCFilter(ctx context.Context, member string, raw []byte, budget *vffSKCFilterBudget) (*vffSKCFilterFact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if budget == nil || budget.limits.bytes <= 0 || budget.limits.tokens <= 0 || budget.limits.rules <= 0 || budget.limits.attributes <= 0 {
		return nil, fmt.Errorf("SKC filters require explicit cumulative budgets")
	}
	if len(raw) > budget.limits.bytes-budget.bytes || !utf8.Valid(raw) {
		return nil, fmt.Errorf("invalid SKC filter encoding or cumulative byte budget")
	}
	budget.bytes += len(raw)
	var tokens []vffSKCFilterToken
	lineStart := true
	for offset := 0; offset < len(raw); {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ch := raw[offset]
		if ch == '\r' || ch == '\n' {
			lineStart = true
			offset++
			continue
		}
		if ch == ' ' || ch == '\t' {
			offset++
			continue
		}
		if lineStart && ch == '/' && offset+1 < len(raw) && raw[offset+1] == '/' {
			for offset < len(raw) && raw[offset] != '\r' && raw[offset] != '\n' {
				if offset%1024 == 0 {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
				}
				offset++
			}
			continue
		}
		lineStart = false
		begin := offset
		if ch == '{' || ch == '}' {
			offset++
		} else if vffSKCFilterNameByte(ch, true) {
			offset++
			for offset < len(raw) && vffSKCFilterNameByte(raw[offset], false) {
				if offset%1024 == 0 {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
				}
				offset++
			}
		} else {
			return nil, fmt.Errorf("unsupported SKC filter token")
		}
		if budget.tokens >= budget.limits.tokens {
			return nil, fmt.Errorf("SKC cumulative filter token budget exceeded")
		}
		budget.tokens++
		tokens = append(tokens, vffSKCFilterToken{string(raw[begin:offset]), begin, offset})
	}
	result := &vffSKCFilterFact{vffSKCMemberFact: vffSKCMemberFact{member, sha256.Sum256(raw)}}
	seen := map[string]bool{}
	for offset := 0; offset < len(tokens); {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if budget.rules >= budget.limits.rules {
			return nil, fmt.Errorf("SKC cumulative filter rule budget exceeded")
		}
		budget.rules++
		token := tokens[offset]
		rule := vffSKCFilterRule{begin: token.begin, end: token.end}
		if token.value == "}" {
			return nil, fmt.Errorf("unmatched SKC filter bracket")
		}
		if token.value != "{" {
			rule.typeName = token.value
			offset++
		}
		if seen[rule.typeName] {
			return nil, fmt.Errorf("duplicate SKC type rule or global blacklist")
		}
		seen[rule.typeName] = true
		if offset < len(tokens) && tokens[offset].value == "{" {
			rule.attributesSpecified = true
			offset++
			attributes := map[string]bool{}
			for offset < len(tokens) && tokens[offset].value != "}" {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				value := tokens[offset].value
				if value == "{" || attributes[value] {
					return nil, fmt.Errorf("nested SKC filter block or duplicate attribute")
				}
				if budget.attributes >= budget.limits.attributes {
					return nil, fmt.Errorf("SKC cumulative filter attribute budget exceeded")
				}
				budget.attributes++
				attributes[value] = true
				rule.attributes = append(rule.attributes, value)
				offset++
			}
			if offset == len(tokens) {
				return nil, fmt.Errorf("truncated SKC filter attribute block")
			}
			rule.end = tokens[offset].end
			offset++
		}
		rule.sha256 = sha256.Sum256(raw[rule.begin:rule.end])
		result.rules = append(result.rules, rule)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func vffSKCFilterNameByte(b byte, first bool) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b == '_' || !first && (b >= '0' && b <= '9' || b == '-' || b == '.')
}
