package metaitem

import (
	"context"
	"io"
	"sort"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/contentadapter"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/common/rastermosaic"
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
		Options: dataitem.ResolveOptions{
			AllowWholeScope: true,
		},
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
		if item.Layout == format.LayoutSingle && !input.Options.IncludeSingleResources {
			continue
		}
		var itemAttributes map[string]interface{}
		if item.Layout == format.LayoutWhole && item.Format == string(format.FormatRasterMosaic) {
			attrs, ok := rasterMosaicManifestAttributes(ctx, input, item)
			if !ok {
				continue
			}
			itemAttributes = attrs
		}
		if item.Layout == format.LayoutWhole && item.DataType == datatype.Model3D {
			attrs, ok := scopeModel3DAttributes(ctx, input, item)
			if !ok {
				continue
			}
			itemAttributes = attrs
		}
		detected := detectedItemFromResolvedItem(input.DirPath, item)
		if len(itemAttributes) > 0 {
			detected.Attributes = itemAttributes
		}
		if item.Layout == format.LayoutMulti {
			enrichRefTableInfo(ctx, input.ContentReader, input.ConnInfo, input.EngineID, input.CatalogPathFor, item, detected)
		}
		result.Items = append(result.Items, detected)
		if item.Layout == format.LayoutWhole && resolved.Exclusive {
			result.Exclusive = true
		}
		for _, path := range item.ClaimPaths {
			if path != "" {
				result.Claims[path] = true
			}
		}
		for _, path := range detected.RefFilePaths() {
			result.Claims[path] = true
		}
	}
	return result, nil
}

func scopeModel3DAttributes(ctx context.Context, input DirectoryResolveInput, item dataitem.ResolvedItem) (map[string]interface{}, bool) {
	if input.ContentReader == nil {
		return nil, false
	}
	formatType := format.NormalizeFormat(item.Format)
	if formatType == format.FormatUnknown {
		return nil, false
	}
	provider, err := format.GetScopeModel3DInfoProvider(formatType)
	if err != nil {
		return nil, false
	}
	reader := contentadapter.NewMappedReader(input.ContentReader, input.ConnInfo, func(ref contentio.Ref) (plugin.CatalogPath, error) {
		return resolveCatalogPath(input.EngineID, ref.Path, input.CatalogPathFor), nil
	}, plugin.ReadOptions{})
	info, err := provider.DescribeModel3DScope(ctx, reader, contentio.NewRef(item.ScopePath, contentio.RoleScope), nil)
	if err != nil || info == nil {
		return nil, false
	}
	if info.Model3D != nil && info.Model3D.SizeBytes == nil && item.SizeBytes != nil && *item.SizeBytes > 0 {
		info.Model3D.SizeBytes = item.SizeBytes
	}
	attrs := map[string]interface{}{}
	if info.Model3D != nil {
		metaattr.MergeStandardAttributes(attrs, metaattr.Model3DInfoAttributes(info.Model3D, info.Spatial))
	}
	if len(info.FormatInfo) > 0 {
		metaattr.MergeStandardAttributes(attrs, metaattr.FormatInfoAttributes(string(formatType), info.FormatInfo))
	}
	if len(attrs) == 0 {
		return nil, false
	}
	return attrs, true
}

func rasterMosaicManifestAttributes(ctx context.Context, input DirectoryResolveInput, item dataitem.ResolvedItem) (map[string]interface{}, bool) {
	if input.ContentReader == nil {
		return nil, false
	}
	manifestPath := ""
	for _, ref := range item.RefList {
		if ref.Role == "manifest" && ref.Path != "" {
			manifestPath = ref.Path
			break
		}
	}
	if manifestPath == "" {
		return nil, false
	}
	reader, err := input.ContentReader.OpenContent(ctx, input.ConnInfo, resolveCatalogPath(input.EngineID, manifestPath, input.CatalogPathFor), plugin.ReadOptions{Length: 1 << 20})
	if err != nil {
		return nil, false
	}
	defer reader.Close()

	manifest, err := rastermosaic.DecodeManifest(reader, 1<<20)
	if err != nil {
		return nil, false
	}
	manifestRef := strings.TrimPrefix(strings.TrimPrefix(manifestPath, strings.Trim(item.ScopePath, "/")), "/")
	if manifestRef == "" {
		manifestRef = rastermosaic.ManifestFileName
	}
	formatInfo := map[string]interface{}{
		"manifest_ref":            manifestRef,
		"manifest_schema_version": manifest.SchemaVersion,
	}
	if value := strings.TrimSpace(manifest.Refs.Index); value != "" {
		formatInfo["index_ref"] = value
	}
	if value := strings.TrimSpace(manifest.Refs.Overview); value != "" {
		formatInfo["overview_ref"] = value
		formatInfo["overview_profile"] = "cog"
	}
	if manifest.Summary.LeafCount > 0 {
		formatInfo["leaf_count"] = manifest.Summary.LeafCount
	}
	if manifest.Summary.SourceCount > 0 {
		formatInfo["source_count"] = manifest.Summary.SourceCount
	}
	if manifest.Summary.OverviewWidth > 0 {
		formatInfo["overview_width"] = manifest.Summary.OverviewWidth
	}
	if manifest.Summary.OverviewHeight > 0 {
		formatInfo["overview_height"] = manifest.Summary.OverviewHeight
	}
	attrs := map[string]interface{}{
		"format_info": map[string]interface{}{
			string(format.FormatRasterMosaic): formatInfo,
		},
	}
	if spatial := rasterMosaicSpatialAttributes(manifest); len(spatial) > 0 {
		attrs["capabilities"] = map[string]interface{}{"spatial": spatial}
	}
	return attrs, true
}

func rasterMosaicSpatialAttributes(manifest rastermosaic.Manifest) map[string]interface{} {
	spatial := map[string]interface{}{}
	if len(manifest.Summary.Extent) == 4 {
		spatial["extent"] = append([]float64(nil), manifest.Summary.Extent...)
	}
	if crs := strings.TrimSpace(manifest.Summary.SourceCRS); crs != "" {
		spatial["crs"] = crs
	}
	return spatial
}

func interfaceMap(value interface{}) map[string]interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return typed
	default:
		return nil
	}
}

func interfaceString(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
	}
}

func interfaceInt64(value interface{}) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	default:
		return 0
	}
}

func floatSlice(value interface{}) []float64 {
	items, ok := value.([]interface{})
	if !ok {
		return nil
	}
	result := make([]float64, 0, len(items))
	for _, item := range items {
		switch typed := item.(type) {
		case int:
			result = append(result, float64(typed))
		case int64:
			result = append(result, float64(typed))
		case float64:
			result = append(result, typed)
		default:
			return nil
		}
	}
	return result
}

func fileEntriesToCandidates(files []StorageFileRef) []dataitem.Candidate {
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
	metaattr.MergeStandardAttributes(item.Attributes, metaattr.TableDescribeAttributes(metaattr.TableDescribeAttributesInput{
		FormatName:         item.Format,
		Table:              tableInfo.Table,
		FormatInfo:         tableInfo.FormatInfo,
		Spatial:            tableInfo.Spatial,
		AccessIndex:        tableInfo.AccessIndex,
		IncludeAccessIndex: true,
	}))
}

func EnrichKnownMultiTableItem(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	catalogPathFor func(path string) plugin.CatalogPath,
	item *DetectedItem,
) (*DetectedItem, bool, error) {
	if contentReader == nil || item == nil || item.Layout != format.LayoutMulti || item.Format == "" || len(item.RefList) == 0 {
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
