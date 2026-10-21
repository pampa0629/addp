package execution

import (
	"fmt"

	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/models"
)

// DisplayLanguageKey belongs to an execution snapshot, never a task definition.
const DisplayLanguageKey = "display_language"

// DisplayLanguage reads a persisted fact strictly; it never supplies a default.
func DisplayLanguage(item *TaskExecution) (string, error) {
	if item == nil {
		return "", fmt.Errorf("execution display language is missing")
	}
	lang, ok := item.ExecutionConfig[DisplayLanguageKey].(string)
	if !ok || !commoni18n.IsSupportedLanguage(lang) {
		return "", fmt.Errorf("execution display language is invalid")
	}
	return lang, nil
}

// FreezeDisplayLanguage freezes the fact without modifying an aliased template map.
func FreezeDisplayLanguage(item *TaskExecution, lang string) error {
	if item == nil || !commoni18n.IsSupportedLanguage(lang) {
		return fmt.Errorf("canonical execution display language is required")
	}
	if _, exists := item.ExecutionConfig[DisplayLanguageKey]; exists {
		existing, err := DisplayLanguage(item)
		if err != nil {
			return err
		}
		if existing != lang {
			return fmt.Errorf("execution display language is immutable")
		}
		return nil
	}
	config := make(models.JSONMap, len(item.ExecutionConfig)+1)
	for key, value := range item.ExecutionConfig {
		config[key] = value
	}
	config[DisplayLanguageKey] = lang
	item.ExecutionConfig = config
	return nil
}

// InheritDisplayLanguage requires the actual same-tenant parent edge. Owners load
// and authorize that parent; this helper performs no database or remote access.
func InheritDisplayLanguage(child, parent *TaskExecution) error {
	if child == nil || parent == nil || child.TenantID <= 0 || child.TenantID != parent.TenantID || parent.ExecutionID == "" || child.ParentExecutionID == nil || *child.ParentExecutionID != parent.ExecutionID || child.ExecutionID == parent.ExecutionID {
		return fmt.Errorf("execution language parent does not match child")
	}
	lang, err := DisplayLanguage(parent)
	if err != nil {
		return err
	}
	return FreezeDisplayLanguage(child, lang)
}
