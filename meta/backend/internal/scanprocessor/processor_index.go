package scanprocessor

import (
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

func (p Processor) indexDeepAsset(input *input, item *models.MetaItem, extraction documentExtractionResult, isDeepScan bool) scanflow.ExtractionCounts {
	counts := extraction.Counts
	if !isDeepScan || p.indexer == nil {
		return counts
	}
	indexed := p.indexer.IndexCatalogAsset(input.Resource, input.TenantID, input.EngineID, catalogResource(input), input.IndexRelativePath, input.FullName, item, extraction.Text)
	if extraction.Text != "" {
		if indexed {
			counts.Indexed++
		} else {
			counts.IndexFailed++
		}
	}
	return counts
}

func catalogResource(input *input) metacatalog.StorageResource {
	return metacatalog.StorageResource{
		RootName:     input.IndexRootName,
		Path:         input.IndexPath,
		FullPath:     input.FullName,
		NodeType:     input.ItemType,
		Format:       input.Detected.Format,
		SizeBytes:    input.SizeBytes,
		ObjectCount:  1,
		LastModified: input.DataUpdatedAt,
		CatalogPath:  input.CatalogPathFor(input.PhysicalPath),
	}
}
