package metaitem

import (
	"bufio"
	"context"
	"io"
	"path"
	"sort"
	"strconv"
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
	initialResolved, err := resolveCommonItems(input.DirPath, files)
	if err != nil {
		return nil, err
	}
	result := &DetectionResult{
		Items:  []*DetectedItem{},
		Claims: ResourceClaimSet{},
	}
	if initialResolved == nil {
		return result, nil
	}
	if initialResolved.Exclusive {
		appendResolvedCommonItems(ctx, input, initialResolved, result)
		return result, nil
	}

	for _, item := range resolveOBJResourceItems(ctx, input, files) {
		appendResolvedDetection(input, result, item)
	}
	for _, item := range resolveGLTFManifestItems(ctx, input, files) {
		appendResolvedDetection(input, result, item)
	}

	resolved, err := resolveCommonItems(input.DirPath, filterManifestAndClaimedStorageFiles(files, result.Claims))
	if err != nil {
		return nil, err
	}
	appendResolvedCommonItems(ctx, input, resolved, result)
	return result, nil
}

func appendResolvedDetection(input DirectoryResolveInput, result *DetectionResult, item dataitem.ResolvedItem) {
	if result == nil {
		return
	}
	detected := detectedItemFromResolvedItem(input.DirPath, item)
	result.Items = append(result.Items, detected)
	for _, path := range item.ClaimPaths {
		if path != "" {
			result.Claims[path] = true
		}
	}
	for _, path := range detected.RefFilePaths() {
		result.Claims[path] = true
	}
}

func resolveCommonItems(dirPath string, files []StorageFileRef) (*dataitem.ResolveResult, error) {
	return dataitem.ResolveItems(dataitem.ResolveInput{
		ScopeKind:  dataitem.ScopeKindDirectory,
		ScopePath:  dirPath,
		Candidates: fileEntriesToCandidates(files),
		Options: dataitem.ResolveOptions{
			AllowWholeScope: true,
		},
	})
}

func appendResolvedCommonItems(ctx context.Context, input DirectoryResolveInput, resolved *dataitem.ResolveResult, result *DetectionResult) {
	if resolved == nil || result == nil {
		return
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
		if item.Layout == format.LayoutWhole && item.DataType == datatype.PointCloud {
			attrs, ok := scopePointCloudAttributes(ctx, input, item)
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

func scopePointCloudAttributes(ctx context.Context, input DirectoryResolveInput, item dataitem.ResolvedItem) (map[string]interface{}, bool) {
	if input.ContentReader == nil {
		return nil, false
	}
	formatType := format.NormalizeFormat(item.Format)
	if formatType == format.FormatUnknown {
		return nil, false
	}
	provider, err := format.GetScopePointCloudInfoProvider(formatType)
	if err != nil {
		return nil, false
	}
	reader := contentadapter.NewMappedReader(input.ContentReader, input.ConnInfo, func(ref contentio.Ref) (plugin.CatalogPath, error) {
		return resolveCatalogPath(input.EngineID, ref.Path, input.CatalogPathFor), nil
	}, plugin.ReadOptions{})
	info, err := provider.DescribePointCloudScope(ctx, reader, contentio.NewRef(item.ScopePath, contentio.RoleScope), nil)
	if err != nil || info == nil {
		return nil, false
	}
	if info.PointCloud != nil && info.PointCloud.SizeBytes == nil && item.SizeBytes != nil && *item.SizeBytes > 0 {
		info.PointCloud.SizeBytes = item.SizeBytes
	}
	attrs := map[string]interface{}{}
	if info.PointCloud != nil {
		metaattr.MergeStandardAttributes(attrs, metaattr.PointCloudInfoAttributes(info.PointCloud, info.Spatial))
	}
	if len(info.FormatInfo) > 0 {
		metaattr.MergeStandardAttributes(attrs, metaattr.FormatInfoAttributes(string(formatType), info.FormatInfo))
	}
	if len(attrs) == 0 {
		return nil, false
	}
	return attrs, true
}

const maxOBJResourceManifestBytes = 1 << 20

var mtlTextureRefKeywords = map[string]bool{
	"map_ka":   true,
	"map_kd":   true,
	"map_ks":   true,
	"map_ke":   true,
	"map_ns":   true,
	"map_d":    true,
	"map_bump": true,
	"bump":     true,
	"disp":     true,
	"decal":    true,
	"norm":     true,
	"refl":     true,
}

func resolveOBJResourceItems(ctx context.Context, input DirectoryResolveInput, files []StorageFileRef) []dataitem.ResolvedItem {
	if input.ContentReader == nil || len(files) == 0 {
		return nil
	}
	byPath := map[string]StorageFileRef{}
	for _, file := range files {
		if file.Path != "" {
			byPath[file.Path] = file
		}
	}
	items := []dataitem.ResolvedItem{}
	for _, file := range files {
		if strings.ToLower(path.Ext(file.Path)) != ".obj" {
			continue
		}
		item, ok := resolveSingleOBJResourceItem(ctx, input, file, byPath)
		if ok {
			items = append(items, item)
		}
	}
	return items
}

func resolveSingleOBJResourceItem(ctx context.Context, input DirectoryResolveInput, objFile StorageFileRef, byPath map[string]StorageFileRef) (dataitem.ResolvedItem, bool) {
	reader, err := input.ContentReader.OpenContent(ctx, input.ConnInfo, resolveCatalogPath(input.EngineID, objFile.Path, input.CatalogPathFor), plugin.ReadOptions{Length: maxOBJResourceManifestBytes})
	if err != nil {
		return dataitem.ResolvedItem{}, false
	}
	defer reader.Close()

	materialRefs := scanOBJMaterialLibraryRefs(reader)
	if len(materialRefs) == 0 {
		return dataitem.ResolvedItem{}, false
	}

	refList := []dataitem.ItemRef{{
		Role:      "model",
		Path:      objFile.Path,
		Required:  true,
		Primary:   true,
		Extension: ".obj",
	}}
	claimPaths := []string{objFile.Path}
	totalSize := objFile.Size
	seen := map[string]bool{objFile.Path: true}
	roleCounts := map[string]int{}

	for _, materialRef := range materialRefs {
		materialPath, materialFile, ok := resolveLocalOBJResourcePath(objFile.Path, materialRef, byPath)
		if !ok || seen[materialPath] {
			continue
		}
		refList = append(refList, dataitem.ItemRef{
			Role:      uniqueRefRole("material_library", roleCounts),
			Path:      materialPath,
			Required:  true,
			Extension: strings.ToLower(path.Ext(materialPath)),
		})
		claimPaths = append(claimPaths, materialPath)
		totalSize += materialFile.Size
		seen[materialPath] = true

		for _, textureRef := range scanMTLTextureRefs(ctx, input, materialPath) {
			texturePath, textureFile, ok := resolveLocalOBJResourcePath(materialPath, textureRef, byPath)
			if !ok || seen[texturePath] {
				continue
			}
			refList = append(refList, dataitem.ItemRef{
				Role:      uniqueRefRole("texture", roleCounts),
				Path:      texturePath,
				Required:  false,
				Extension: strings.ToLower(path.Ext(texturePath)),
			})
			claimPaths = append(claimPaths, texturePath)
			totalSize += textureFile.Size
			seen[texturePath] = true
		}
	}
	if len(refList) == 1 {
		return dataitem.ResolvedItem{}, false
	}
	return dataitem.ResolvedItem{
		Name:               objFile.Name,
		FullName:           objFile.Path,
		Layout:             format.LayoutSingle,
		DataType:           datatype.Model3D,
		Format:             string(format.FormatOBJ),
		PrimaryContentPath: objFile.Path,
		RefList:            refList,
		ClaimPaths:         claimPaths,
		SizeBytes:          &totalSize,
		DetectionReason:    "obj_material_library",
	}, true
}

func scanOBJMaterialLibraryRefs(reader io.Reader) []string {
	scanner := bufio.NewScanner(io.LimitReader(reader, maxOBJResourceManifestBytes))
	scanner.Buffer(make([]byte, 16*1024), 1024*1024)
	refs := []string{}
	seen := map[string]bool{}
	for scanner.Scan() {
		for _, ref := range parseOBJResourceStatement(scanner.Text(), "mtllib") {
			if ref != "" && !seen[ref] {
				refs = append(refs, ref)
				seen[ref] = true
			}
		}
	}
	return refs
}

func scanMTLTextureRefs(ctx context.Context, input DirectoryResolveInput, materialPath string) []string {
	reader, err := input.ContentReader.OpenContent(ctx, input.ConnInfo, resolveCatalogPath(input.EngineID, materialPath, input.CatalogPathFor), plugin.ReadOptions{Length: maxOBJResourceManifestBytes})
	if err != nil {
		return nil
	}
	defer reader.Close()

	scanner := bufio.NewScanner(io.LimitReader(reader, maxOBJResourceManifestBytes))
	scanner.Buffer(make([]byte, 16*1024), 1024*1024)
	refs := []string{}
	seen := map[string]bool{}
	for scanner.Scan() {
		line := stripOBJComment(scanner.Text())
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if !mtlTextureRefKeywords[strings.ToLower(fields[0])] {
			continue
		}
		for _, ref := range resourceRefCandidates(line, fields[0], []string{fields[len(fields)-1]}) {
			if ref != "" && !seen[ref] {
				refs = append(refs, ref)
				seen[ref] = true
			}
		}
	}
	return refs
}

func parseOBJResourceStatement(line, keyword string) []string {
	line = stripOBJComment(line)
	fields := strings.Fields(line)
	if len(fields) < 2 || !strings.EqualFold(fields[0], keyword) {
		return nil
	}
	return resourceRefCandidates(line, fields[0], fields[1:])
}

func resourceRefCandidates(line, keyword string, tokens []string) []string {
	remainder := strings.TrimSpace(strings.TrimSpace(line)[len(keyword):])
	candidates := []string{}
	if remainder != "" {
		candidates = append(candidates, remainder)
	}
	candidates = append(candidates, tokens...)
	return uniqueNonEmptyStrings(candidates)
}

func stripOBJComment(line string) string {
	if index := strings.Index(line, "#"); index >= 0 {
		line = line[:index]
	}
	return strings.TrimSpace(line)
}

func resolveLocalOBJResourcePath(basePath, ref string, byPath map[string]StorageFileRef) (string, StorageFileRef, bool) {
	ref = strings.TrimSpace(strings.ReplaceAll(ref, "\\", "/"))
	if ref == "" || strings.HasPrefix(ref, "/") || strings.Contains(ref, "://") {
		return "", StorageFileRef{}, false
	}
	baseDir := path.Dir(basePath)
	if baseDir == "." {
		baseDir = ""
	}
	resourcePath := path.Clean(path.Join(baseDir, ref))
	if !isPathUnderScope(resourcePath, baseDir) {
		return "", StorageFileRef{}, false
	}
	file, ok := byPath[resourcePath]
	return resourcePath, file, ok
}

func isPathUnderScope(candidate, scope string) bool {
	if candidate == "." || strings.HasPrefix(candidate, "../") || candidate == ".." {
		return false
	}
	if scope == "" {
		return !strings.HasPrefix(candidate, "/")
	}
	return candidate == scope || strings.HasPrefix(candidate, strings.TrimRight(scope, "/")+"/")
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		result = append(result, value)
		seen[value] = true
	}
	return result
}

func resolveGLTFManifestItems(ctx context.Context, input DirectoryResolveInput, files []StorageFileRef) []dataitem.ResolvedItem {
	if input.ContentReader == nil || len(files) == 0 {
		return nil
	}
	byPath := map[string]StorageFileRef{}
	for _, file := range files {
		if file.Path != "" {
			byPath[file.Path] = file
		}
	}
	items := []dataitem.ResolvedItem{}
	for _, file := range files {
		if strings.ToLower(path.Ext(file.Path)) != ".gltf" {
			continue
		}
		item, ok := resolveSingleGLTFManifestItem(ctx, input, file, byPath)
		if ok {
			items = append(items, item)
		}
	}
	return items
}

func resolveSingleGLTFManifestItem(ctx context.Context, input DirectoryResolveInput, manifest StorageFileRef, byPath map[string]StorageFileRef) (dataitem.ResolvedItem, bool) {
	reader, err := input.ContentReader.OpenContent(ctx, input.ConnInfo, resolveCatalogPath(input.EngineID, manifest.Path, input.CatalogPathFor), plugin.ReadOptions{Length: format.MaxGLTFManifestBytes + 1})
	if err != nil {
		return dataitem.ResolvedItem{}, false
	}
	defer reader.Close()

	doc, err := format.DecodeGLTFManifest(reader, format.MaxGLTFManifestBytes)
	if err != nil {
		return dataitem.ResolvedItem{}, false
	}
	refList := []dataitem.ItemRef{{
		Role:      "manifest",
		Path:      manifest.Path,
		Required:  true,
		Primary:   true,
		Extension: ".gltf",
	}}
	claimPaths := []string{manifest.Path}
	totalSize := manifest.Size
	seen := map[string]bool{manifest.Path: true}
	roleCounts := map[string]int{}
	manifestDir := path.Dir(manifest.Path)
	if manifestDir == "." {
		manifestDir = ""
	}
	for _, resource := range format.LocalGLTFResourceRefs(doc) {
		resourcePath := path.Clean(path.Join(manifestDir, resource.URI))
		file, ok := byPath[resourcePath]
		if !ok {
			return dataitem.ResolvedItem{}, false
		}
		if seen[resourcePath] {
			continue
		}
		refList = append(refList, dataitem.ItemRef{
			Role:      uniqueRefRole(resource.Role, roleCounts),
			Path:      resourcePath,
			Required:  true,
			Extension: strings.ToLower(path.Ext(resourcePath)),
		})
		claimPaths = append(claimPaths, resourcePath)
		totalSize += file.Size
		seen[resourcePath] = true
	}
	return dataitem.ResolvedItem{
		Name:               manifest.Name,
		FullName:           manifest.Path,
		Layout:             format.LayoutMulti,
		DataType:           datatype.Model3D,
		Format:             string(format.FormatGLTF),
		PrimaryContentPath: manifest.Path,
		RefList:            refList,
		ClaimPaths:         claimPaths,
		SizeBytes:          &totalSize,
		DetectionReason:    "gltf_manifest",
	}, true
}

func uniqueRefRole(base string, counts map[string]int) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "resource"
	}
	count := counts[base]
	counts[base] = count + 1
	if count == 0 {
		return base
	}
	return base + "_" + strconv.Itoa(count)
}

func filterManifestAndClaimedStorageFiles(files []StorageFileRef, claims ResourceClaimSet) []StorageFileRef {
	filtered := make([]StorageFileRef, 0, len(files))
	for _, file := range files {
		if strings.ToLower(path.Ext(file.Path)) == ".gltf" {
			continue
		}
		if file.Path == "" || len(claims) == 0 || !claims[file.Path] {
			filtered = append(filtered, file)
		}
	}
	return filtered
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
