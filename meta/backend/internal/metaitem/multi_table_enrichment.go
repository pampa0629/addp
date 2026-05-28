package metaitem

import (
	"context"
	"io"
	"sort"

	"github.com/addp/common/contentio"
	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metaattr"
)

func resolveCatalogPath(engineID uint, path string, catalogPathFor func(path string) plugin.CatalogPath) plugin.CatalogPath {
	if catalogPathFor != nil {
		return catalogPathFor(path)
	}
	return plugin.FileItemPath(engineID, path)
}

type commonDataItemResolver struct{}

func (d *commonDataItemResolver) Priority() int {
	return 100
}

func (d *commonDataItemResolver) ResolveItems(ctx context.Context, input DirectoryResolveInput) (*DetectionResult, error) {
	files := input.Files
	if len(input.RecursiveFiles) > 0 {
		files = input.RecursiveFiles
	}
	resolved, err := dataitem.ResolveItems(dataitem.ResolveInput{
		ScopeKind:  dataitem.ScopeKindDirectory,
		ScopePath:  input.DirPath,
		Candidates: fileEntriesToCandidates(files),
	})
	if err != nil {
		return nil, err
	}
	result := &DetectionResult{
		Items:  []*DetectedItem{},
		Claims: ResourceClaimSet{},
	}
	if resolved == nil {
		return result, nil
	}
	for _, item := range resolved.Items {
		if item.Layout != dataitem.LayoutMulti {
			continue
		}
		detected := detectedItemFromResolvedItem(input.DirPath, item)
		enrichRefTableInfo(ctx, input.ContentReader, input.ConnInfo, input.EngineID, input.CatalogPathFor, item, detected)
		result.Items = append(result.Items, detected)
		for _, path := range detected.RefFilePaths() {
			result.Claims[path] = true
		}
	}
	return result, nil
}

func fileEntriesToCandidates(files []plugin.FileEntry) []dataitem.Candidate {
	candidates := make([]dataitem.Candidate, 0, len(files))
	for _, file := range files {
		size := file.Size
		candidates = append(candidates, dataitem.Candidate{
			Path:        file.Path,
			Name:        file.Name,
			ContentType: file.ContentType,
			SizeBytes:   &size,
		})
	}
	return candidates
}

func detectedItemFromResolvedItem(physicalPath string, item dataitem.ResolvedItem) *DetectedItem {
	sort.Slice(item.RefList, func(i, j int) bool {
		return item.RefList[i].Path < item.RefList[j].Path
	})

	return &DetectedItem{
		ResolvedItem: item,
		PhysicalPath: physicalPath,
	}
}

func enrichRefTableInfo(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	catalogPathFor func(path string) plugin.CatalogPath,
	item dataitem.ResolvedItem,
	detected *DetectedItem,
) {
	if contentReader == nil || detected == nil || item.Format == "" {
		return
	}
	formatType := format.NormalizeFormat(item.Format)
	if formatType == format.FormatUnknown {
		return
	}
	refProvider, err := format.GetMultiTableInfoProvider(formatType)
	if err != nil {
		return
	}
	reader := newMetaRefReader(contentReader, connInfo, engineID, catalogPathFor)
	tableInfo, err := refProvider.DescribeMultiTable(ctx, reader, item.RelatedRefs(), nil)
	if err != nil {
		return
	}
	if tableInfo != nil && tableInfo.Table != nil {
		detected.Fields = append([]datatype.FieldInfo(nil), tableInfo.Table.Fields...)
	}
	upsertRefTableInfo(detected, tableInfo)
}

type metaRefReader struct {
	contentReader  plugin.ContentReadableProvider
	connInfo       plugin.ConnectionInfo
	engineID       uint
	catalogPathFor func(path string) plugin.CatalogPath
}

func newMetaRefReader(contentReader plugin.ContentReadableProvider, connInfo plugin.ConnectionInfo, engineID uint, catalogPathFor func(path string) plugin.CatalogPath) *metaRefReader {
	return &metaRefReader{
		contentReader:  contentReader,
		connInfo:       connInfo,
		engineID:       engineID,
		catalogPathFor: catalogPathFor,
	}
}

func (r *metaRefReader) Open(ctx context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	return r.contentReader.OpenContent(ctx, r.connInfo, resolveCatalogPath(r.engineID, ref.Path, r.catalogPathFor), plugin.ReadOptions{})
}

func (r *metaRefReader) Stat(context.Context, contentio.Ref) (*contentio.Stat, error) {
	return nil, contentio.ErrContentNotFound
}

func upsertRefTableInfo(item *DetectedItem, tableInfo *format.TableDescribeResult) {
	if item == nil || tableInfo == nil {
		return
	}
	if item.Attributes == nil {
		item.Attributes = map[string]interface{}{}
	}
	metaattr.UpsertNested(item.Attributes, "type_info", "table", datatype.TableInfoAttributes(tableInfo.Table))
	if len(tableInfo.FormatInfo) > 0 {
		metaattr.UpsertNested(item.Attributes, "format_info", item.Format, tableInfo.FormatInfo)
	}
	if spatialAttrs := metaattr.SpatialInfoAttributes(tableInfo.Spatial); len(spatialAttrs) > 0 {
		metaattr.UpsertNested(item.Attributes, "capabilities", "spatial", spatialAttrs)
	}
	if tableInfo.ContentIndex != nil {
		metaattr.UpsertNested(item.Attributes, "content_index", "table", commonJSON.MapFromStruct(tableInfo.ContentIndex))
	}
}

func EnrichKnownMultiTableItem(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	catalogPathFor func(path string) plugin.CatalogPath,
	item *DetectedItem,
) (*DetectedItem, bool, error) {
	if contentReader == nil || item == nil || item.Layout != dataitem.LayoutMulti || item.Format == "" || len(item.RefList) == 0 {
		return item, false, nil
	}
	formatType := format.NormalizeFormat(item.Format)
	if formatType == format.FormatUnknown {
		return item, false, nil
	}
	refProvider, err := format.GetMultiTableInfoProvider(formatType)
	if err != nil {
		return item, false, nil
	}
	reader := newMetaRefReader(contentReader, connInfo, engineID, catalogPathFor)
	tableInfo, err := refProvider.DescribeMultiTable(ctx, reader, item.RelatedRefs(), nil)
	if err != nil {
		return item, false, err
	}
	if tableInfo.Table != nil {
		item.Fields = append([]datatype.FieldInfo(nil), tableInfo.Table.Fields...)
	}
	upsertRefTableInfo(item, tableInfo)
	return item, true, nil
}
