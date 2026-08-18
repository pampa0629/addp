package scanprocessor

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
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
	repo               *metaRepo.ScanRepository
	indexer            AssetIndexer
	log                *slog.Logger
	cadInspector       metaenrich.CADInspector
	formatDetector     metaenrich.RuntimeFormatDetector
	containerInspector metaenrich.ContainerInspector
}

func (p Processor) WithCADInspector(inspector metaenrich.CADInspector) Processor {
	p.cadInspector = inspector
	return p
}

func (p Processor) WithContainerInspector(inspector metaenrich.ContainerInspector) Processor {
	p.containerInspector = inspector
	if detector, ok := inspector.(metaenrich.RuntimeFormatDetector); ok {
		p.formatDetector = detector
	}
	return p
}

func (p Processor) WithFormatDetector(detector metaenrich.RuntimeFormatDetector) Processor {
	p.formatDetector = detector
	return p
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
