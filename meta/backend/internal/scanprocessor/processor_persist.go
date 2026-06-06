package scanprocessor

import "github.com/addp/meta/internal/models"

func (p Processor) persistItem(input *input, attrs models.JSONMap) (*models.MetaItem, error) {
	rowCount := itemRowCountFromMetaAttributes(attrs)
	if input.ExistingItemID > 0 {
		return p.repo.UpdateItemByIDWithDepth(
			input.TenantID,
			input.ExistingItemID,
			input.EngineID,
			input.ParentNode,
			input.ItemType,
			input.ItemName,
			input.FullName,
			attrs,
			rowCount,
			&input.SizeBytes,
			input.DataUpdatedAt,
			input.ScanDepth,
		)
	}
	return p.repo.UpsertItemWithDepth(
		input.TenantID,
		input.EngineID,
		input.ParentNode,
		input.ItemType,
		input.ItemName,
		input.FullName,
		attrs,
		rowCount,
		&input.SizeBytes,
		input.DataUpdatedAt,
		input.ScanDepth,
	)
}
