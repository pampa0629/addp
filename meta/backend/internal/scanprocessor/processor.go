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

type Input struct {
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

type DocumentExtractionResult struct {
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

func (p Processor) Process(ctx context.Context, input Input) (Result, error) {
	if input.Resource == nil {
		return Result{}, fmt.Errorf("resource is nil")
	}
	if input.ParentNode == nil {
		return Result{}, fmt.Errorf("parent node is nil")
	}
	if input.Detected == nil {
		return Result{}, fmt.Errorf("detected item is nil")
	}
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
	isDeepScan := strings.EqualFold(input.ScanDepth, "deep")
	if isDeepScan {
		if input.Detected.Layout == format.LayoutMulti && input.Detected.DataType == datatype.Table {
			ClearStaleKnownMultiTableAccessIndex(attrs, input.Detected)
			enriched, _, err := metaitem.EnrichKnownMultiTableItem(ctx, input.ContentReader, input.ConnInfo, input.EngineID, input.CatalogPathFor, input.Detected)
			if err != nil {
				if input.StrictDeepEnrich {
					return Result{}, err
				}
				if p.log != nil {
					p.log.Warn("提取 multi table 深度属性失败，保留基础属性", "path", input.PhysicalPath, "format", input.Detected.Format, "error", err)
				}
			}
			if enriched != nil {
				input.Detected = enriched
				if len(enriched.Attributes) > 0 {
					attrs = metaattr.JSONMap(enriched.Attributes)
				}
			}
		}
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
				return Result{}, err
			}
			if p.log != nil {
				p.log.Warn("提取资源深度属性失败，保留基础属性", "path", input.PhysicalPath, "format", input.Detected.Format, "error", err)
			}
		}
		if enriched != nil {
			input.Detected = enriched
		}
	} else {
		metaitem.ApplyContainerSummary(attrs, input.Detected)
	}

	extraction := DocumentExtractionResult{}
	if isDeepScan && input.ContentReader != nil {
		if contentHash, err := ComputeContentSHA256(ctx, input.ContentReader, input.ConnInfo, input.CatalogPathFor(input.PhysicalPath)); err != nil {
			if input.StrictDeepEnrich {
				return Result{}, err
			}
			if p.log != nil {
				p.log.Warn("计算内容指纹失败", "path", input.PhysicalPath, "error", err)
			}
		} else {
			SetStorageContentHash(attrs, contentHash)
		}
		extraction = ExtractCatalogDocumentText(ctx, attrs, input.ContentReader, input.ConnInfo, input.EngineID, metacatalog.StorageResource{
			RootName:     input.IndexRootName,
			Path:         input.IndexPath,
			FullPath:     input.FullName,
			NodeType:     input.ItemType,
			Format:       input.Detected.Format,
			SizeBytes:    input.SizeBytes,
			ObjectCount:  1,
			LastModified: input.DataUpdatedAt,
			CatalogPath:  input.CatalogPathFor(input.PhysicalPath),
		}, input.Detected)
	}

	rowCount := itemRowCountFromMetaAttributes(attrs)
	var item *models.MetaItem
	var err error
	if input.ExistingItemID > 0 {
		item, err = p.repo.UpdateItemByIDWithDepth(
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
	} else {
		item, err = p.repo.UpsertItemWithDepth(
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
	if err != nil {
		return Result{}, err
	}

	counts := extraction.Counts
	if isDeepScan && p.indexer != nil {
		indexed := p.indexer.IndexCatalogAsset(input.Resource, input.TenantID, input.EngineID, metacatalog.StorageResource{
			RootName:     input.IndexRootName,
			Path:         input.IndexPath,
			FullPath:     input.FullName,
			NodeType:     input.ItemType,
			Format:       input.Detected.Format,
			SizeBytes:    input.SizeBytes,
			ObjectCount:  1,
			LastModified: input.DataUpdatedAt,
			CatalogPath:  input.CatalogPathFor(input.PhysicalPath),
		}, input.IndexRelativePath, input.FullName, item, extraction.Text)
		if extraction.Text != "" {
			if indexed {
				counts.Indexed++
			} else {
				counts.IndexFailed++
			}
		}
	}

	return Result{Item: item, Fields: len(input.Detected.Fields), Extraction: counts}, nil
}

func ClearStaleKnownMultiTableAccessIndex(attrs map[string]interface{}, item *metaitem.DetectedItem) {
	if attrs == nil || item == nil {
		return
	}
	if item.Layout != format.LayoutMulti || item.DataType != datatype.Table {
		return
	}
	metaattr.RemoveAccessIndexTable(attrs)
	metaattr.RemoveAccessIndexTable(item.Attributes)
}
