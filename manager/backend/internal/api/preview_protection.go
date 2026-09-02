package api

import (
	"github.com/addp/common/dataprotection"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/preview"
	managerprotection "github.com/addp/manager/internal/protection"
)

// applyPreviewProtection is the Manager response-boundary executor. Providers
// continue returning native decoded rows; protected rows are transformed only
// immediately before serialization.
func applyPreviewProtection(result *preview.PreviewResult, rules []dataprotection.Rule) error {
	if len(rules) == 0 {
		return nil
	}
	if result == nil {
		return managerprotection.ErrRequired
	}
	table, ok := result.Data.(*models.TablePreview)
	if !ok || table == nil {
		return managerprotection.ErrRequired
	}
	return managerprotection.ProtectRows(table.Rows, managerprotection.ActionPreview, rules)
}
