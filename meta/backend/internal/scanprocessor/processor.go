package scanprocessor

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/metaenrich"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
)

type AssetIndexer interface {
	IndexCatalogAsset(resource *commonModels.Engine, tenantID, engineID uint, catalogResource metacatalog.StorageResource, relativePath, fullName string, item *models.MetaItem, extractedText string) bool
}

type input struct {
	Resource           *commonModels.Engine
	TenantID           uint
	EngineID           uint
	ParentNode         *models.MetaNode
	ExistingItemID     uint
	ItemType           string
	ItemName           string
	FullName           string
	Attributes         models.JSONMap
	Detected           *metaitem.DetectedItem
	ContentReader      plugin.ContentReadableProvider
	ConnInfo           plugin.ConnectionInfo
	CatalogPath        plugin.CatalogPath
	CatalogPathFor     func(string) plugin.CatalogPath
	PhysicalPath       string
	IndexRootName      string
	IndexPath          string
	IndexRelativePath  string
	SizeBytes          int64
	DataUpdatedAt      *time.Time
	ScanDepth          string
	IncludeAccessIndex bool
	StrictDeepEnrich   bool
}

type Result struct {
	Item       *models.MetaItem
	Fields     int
	Extraction scanflow.ExtractionCounts
}

type documentExtractionResult struct {
	Text   string
	Counts scanflow.ExtractionCounts
}

type Processor struct {
	repo    *metaRepo.ScanRepository
	indexer AssetIndexer
	log     *slog.Logger
}

func New(repo *metaRepo.ScanRepository, indexer AssetIndexer, log *slog.Logger) Processor {
	if isNilAssetIndexer(indexer) {
		indexer = nil
	}
	return Processor{repo: repo, indexer: indexer, log: log}
}

func isNilAssetIndexer(indexer AssetIndexer) bool {
	if indexer == nil {
		return true
	}
	value := reflect.ValueOf(indexer)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (p Processor) Process(ctx context.Context, input input) (Result, error) {
	if err := validateInput(input); err != nil {
		return Result{}, err
	}

	attrs := prepareAttributes(&input)
	isDeepScan := strings.EqualFold(input.ScanDepth, "deep")
	if isDeepScan {
		enrichedAttrs, err := p.enrichDeep(ctx, &input, attrs)
		if err != nil {
			return Result{}, err
		}
		attrs = enrichedAttrs
	} else {
		metaitem.ApplyContainerSummary(attrs, input.Detected)
	}

	extraction, err := p.extractDeepContent(ctx, &input, attrs, isDeepScan)
	if err != nil {
		return Result{}, err
	}

	item, err := p.persistItem(&input, attrs)
	if err != nil {
		return Result{}, err
	}

	counts := p.indexDeepAsset(&input, item, extraction, isDeepScan)

	return Result{Item: item, Fields: len(input.Detected.Fields), Extraction: counts}, nil
}

func validateInput(input input) error {
	if input.Resource == nil {
		return fmt.Errorf("resource is nil")
	}
	if input.ParentNode == nil {
		return fmt.Errorf("parent node is nil")
	}
	if input.Detected == nil {
		return fmt.Errorf("detected item is nil")
	}
	return nil
}

func prepareAttributes(input *input) models.JSONMap {
	attrs := metaattr.JSONMap(input.Attributes)
	if attrs == nil {
		attrs = metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(input.Detected)))
	}
	metaattr.SetStorage(attrs, "physical_path", input.PhysicalPath)
	if input.IndexRootName != "" {
		metaattr.SetStorage(attrs, "bucket", input.IndexRootName)
	}
	dir, name := splitCatalogResourcePath(input.IndexPath)
	if dir != "" {
		metaattr.SetStorage(attrs, "path", dir)
	}
	if name != "" {
		metaattr.SetStorage(attrs, "name", name)
	}
	if input.DataUpdatedAt != nil {
		metaattr.SetStorage(attrs, "last_modified_at", input.DataUpdatedAt)
	}
	if input.CatalogPathFor == nil {
		path := input.CatalogPath
		input.CatalogPathFor = func(string) plugin.CatalogPath {
			return path
		}
	}
	return attrs
}

func (p Processor) enrichDeep(ctx context.Context, input *input, attrs models.JSONMap) (models.JSONMap, error) {
	enrichedAttrs, err := p.enrichKnownMultiTable(ctx, input, attrs)
	if err != nil {
		return nil, err
	}
	attrs = enrichedAttrs

	enriched, _, err := metaenrich.EnrichResourceAttributes(ctx, attrs, metaenrich.ResourceAttributesInput{
		ContentReader:      input.ContentReader,
		ConnInfo:           input.ConnInfo,
		EngineID:           input.EngineID,
		Item:               input.Detected,
		PhysicalPath:       input.PhysicalPath,
		SizeBytes:          input.SizeBytes,
		IncludeAccessIndex: input.IncludeAccessIndex,
		CatalogPathFor:     input.CatalogPathFor,
	})
	if err != nil {
		if input.StrictDeepEnrich {
			return nil, err
		}
		p.warn("提取资源深度属性失败，保留基础属性", input, err)
	}
	if enriched != nil {
		input.Detected = enriched
	}
	return attrs, nil
}

func (p Processor) enrichKnownMultiTable(ctx context.Context, input *input, attrs models.JSONMap) (models.JSONMap, error) {
	if input.Detected.Layout != format.LayoutMulti || input.Detected.DataType != datatype.Table {
		return attrs, nil
	}
	clearStaleKnownMultiTableAccessIndex(attrs, input.Detected)
	enriched, _, err := metaitem.EnrichKnownMultiTableItem(ctx, input.ContentReader, input.ConnInfo, input.EngineID, input.CatalogPathFor, input.Detected)
	if err != nil {
		if input.StrictDeepEnrich {
			return nil, err
		}
		p.warn("提取 multi table 深度属性失败，保留基础属性", input, err)
	}
	if enriched == nil {
		return attrs, nil
	}
	input.Detected = enriched
	if len(enriched.Attributes) > 0 {
		return metaattr.JSONMap(enriched.Attributes), nil
	}
	return attrs, nil
}

func (p Processor) extractDeepContent(ctx context.Context, input *input, attrs models.JSONMap, isDeepScan bool) (documentExtractionResult, error) {
	if !isDeepScan || input.ContentReader == nil {
		return documentExtractionResult{}, nil
	}
	if input.Detected.Layout != format.LayoutSingle {
		return documentExtractionResult{}, nil
	}
	if contentHash, err := computeContentSHA256(ctx, input.ContentReader, input.ConnInfo, input.CatalogPathFor(input.PhysicalPath)); err != nil {
		if input.StrictDeepEnrich {
			return documentExtractionResult{}, err
		}
		p.warnPath("计算内容指纹失败", input.PhysicalPath, err)
	} else {
		setStorageContentHash(attrs, contentHash)
	}
	return extractCatalogDocumentText(ctx, attrs, input.ContentReader, input.ConnInfo, input.EngineID, catalogResource(input), input.Detected), nil
}

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

func (p Processor) warn(message string, input *input, err error) {
	if p.log == nil {
		return
	}
	p.log.Warn(message, "path", input.PhysicalPath, "format", input.Detected.Format, "error", err)
}

func (p Processor) warnPath(message, path string, err error) {
	if p.log == nil {
		return
	}
	p.log.Warn(message, "path", path, "error", err)
}

func clearStaleKnownMultiTableAccessIndex(attrs map[string]interface{}, item *metaitem.DetectedItem) {
	if attrs == nil || item == nil {
		return
	}
	if item.Layout != format.LayoutMulti || item.DataType != datatype.Table {
		return
	}
	metaattr.RemoveAccessIndexTable(attrs)
	metaattr.RemoveAccessIndexTable(item.Attributes)
}
