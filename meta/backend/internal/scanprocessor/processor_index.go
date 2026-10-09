package scanprocessor

import (
	"context"

	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanresource"
)

func (p Processor) indexItemProjections(ctx context.Context, input *input, item *models.MetaItem, extraction documentExtractionResult, isDeepScan bool) scanflow.ExtractionCounts {
	counts := extraction.Counts
	if p.indexer == nil {
		return counts
	}
	p.indexer.IndexTechnicalMetadata(ctx, input.Resource, input.TenantID, input.ParentNode, item)
	if !isDeepScan {
		return counts
	}
	indexed := p.indexer.IndexCatalogContent(ctx, input.Resource, input.TenantID, item, extraction.Text, extraction.Truncated)
	if extraction.Text != "" {
		if indexed {
			counts.Indexed++
		} else {
			counts.IndexFailed++
		}
	}
	return counts
}

func catalogResource(input *input) scanresource.StorageResource {
	return scanresource.StorageResource{
		RootName:          input.IndexRootName,
		Path:              input.IndexPath,
		FullPath:          input.FullName,
		NodeType:          input.ItemType,
		Format:            input.Detected.Format,
		SizeBytes:         input.SizeBytes,
		ObjectCount:       1,
		LastModified:      input.DataUpdatedAt,
		EngineCatalogPath: input.EngineCatalogPathFor(input.PhysicalPath),
	}
}
