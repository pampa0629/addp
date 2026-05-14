package dataitem

import commondataitem "github.com/addp/common/dataitem"

func MatchBuiltinSingleResourceRule(formatName string) (FormatRule, bool) {
	return commondataitem.MatchBuiltinSingleResourceRule(formatName)
}

func BuiltinSingleResourceRules() []FormatRule {
	return commondataitem.BuiltinSingleResourceRules()
}

func ValidateFormatRule(rule FormatRule) error {
	return commondataitem.ValidateFormatRule(rule)
}
