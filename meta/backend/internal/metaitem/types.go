package metaitem

import (
	"context"
	"time"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
)

// CompositeItemInfo 是 Meta resolver 提取出的 data item 元信息。
type CompositeItemInfo struct {
	Fields             []datatype.FieldInfo
	Document           *datatype.DocumentInfo
	Media              *datatype.MediaInfo
	Container          *datatype.ContainerInfo
	CAD                *datatype.CADInfo
	Model3D            *datatype.Model3DInfo
	PointCloud         *datatype.PointCloudInfo
	GaussianSplat      *datatype.GaussianSplatInfo
	Attributes         map[string]interface{}
	Layout             format.Layout
	DataType           datatype.DataType
	Format             string
	PrimaryContentPath string
	ScopePath          string
	RefFiles           []string
	SizeBytes          *int64
}

// ResourceClaimSet 记录 Meta resolver 已认领的源资源路径。
type ResourceClaimSet map[string]bool

// StorageFileRef 是 Meta item resolver 使用的扫描期文件资源引用。
// 它不是 engine catalog 主模型；catalog 结构应由 plugin.CatalogEntry 表达。
type StorageFileRef struct {
	Name        string
	Path        string
	CatalogPath plugin.CatalogPath
	Size        int64
	ModifiedAt  time.Time
	ContentType string
}

// StorageDirectoryRef 是 Meta item resolver 使用的扫描期目录资源引用。
// 它不是 engine catalog 主模型；catalog 结构应由 plugin.CatalogEntry 表达。
type StorageDirectoryRef struct {
	Name        string
	Path        string
	CatalogPath plugin.CatalogPath
}

// DetectionResult 是 Meta 统一识别入口在一个扫描范围内产出的 item 集合。
type DetectionResult struct {
	Items     []*DetectedItem
	Claims    ResourceClaimSet
	Exclusive bool
}

// ResolveOptions 控制扫描期 item 识别结果的范围。
type ResolveOptions struct {
	IncludeSingleResources bool
}

// DetectedItem 是 Meta 识别后的标准化 data item 计划。
type DetectedItem struct {
	dataitem.ResolvedItem
	PhysicalPath  string
	Fields        []datatype.FieldInfo
	Document      *datatype.DocumentInfo
	Media         *datatype.MediaInfo
	Container     *datatype.ContainerInfo
	CAD           *datatype.CADInfo
	Model3D       *datatype.Model3DInfo
	PointCloud    *datatype.PointCloudInfo
	GaussianSplat *datatype.GaussianSplatInfo
	Attributes    map[string]interface{}
}

func (item *DetectedItem) Size() int64 {
	if item == nil || item.SizeBytes == nil {
		return 0
	}
	return *item.SizeBytes
}

func (item *DetectedItem) RefFilePaths() []string {
	if item == nil || len(item.RefList) == 0 {
		return nil
	}
	paths := make([]string, 0, len(item.RefList))
	for _, ref := range item.RefList {
		if ref.Path != "" {
			paths = append(paths, ref.Path)
		}
	}
	return paths
}

func DetectedItemFromCompositeInfo(info *CompositeItemInfo, physicalPath string, fallbackSize int64) *DetectedItem {
	if info == nil {
		return nil
	}
	sizeBytes := fallbackSize
	if info.SizeBytes != nil {
		sizeBytes = *info.SizeBytes
	}
	return &DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:             info.Layout,
			DataType:           info.DataType,
			Format:             info.Format,
			PrimaryContentPath: info.PrimaryContentPath,
			ScopePath:          info.ScopePath,
			SizeBytes:          &sizeBytes,
			RefList:            ItemRefsFromPaths(info.RefFiles),
		},
		PhysicalPath:  physicalPath,
		Fields:        info.Fields,
		Document:      info.Document.Clone(),
		Media:         info.Media.Clone(),
		Container:     info.Container.Clone(),
		CAD:           info.CAD.Clone(),
		Model3D:       info.Model3D.Clone(),
		PointCloud:    info.PointCloud.Clone(),
		GaussianSplat: info.GaussianSplat.Clone(),
		Attributes:    info.Attributes,
	}
}

func ItemRefsFromPaths(paths []string) []dataitem.ItemRef {
	if len(paths) == 0 {
		return nil
	}
	refs := make([]dataitem.ItemRef, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		refs = append(refs, dataitem.ItemRef{Path: path})
	}
	return refs
}

// DirectoryResolveInput 是 Meta 扫描范围 item 识别的输入。
type DirectoryResolveInput struct {
	ContentReader  plugin.ContentReadableProvider
	ConnInfo       plugin.ConnectionInfo
	EngineID       uint
	CatalogPathFor func(path string) plugin.CatalogPath
	DirPath        string
	Files          []StorageFileRef
	Subdirs        []StorageDirectoryRef
	Options        ResolveOptions
	// RecursiveFiles/RecursiveSubdirs 由扫描入口在需要识别 whole scope 时提供。
	// resolver 只消费观察资源，不自行遍历存储引擎。
	RecursiveFiles   []StorageFileRef
	RecursiveSubdirs []StorageDirectoryRef
}

// ItemResolver 是 Meta item 识别器的最小公共接口。
type ItemResolver interface {
	Priority() int
}

// ScopeItemResolver 从一个扫描范围内识别 0..N 个 Meta data item。
type ScopeItemResolver interface {
	ItemResolver
	ResolveItems(ctx context.Context, input DirectoryResolveInput) (*DetectionResult, error)
}

// FormatRuleProvider 声明 resolver 背后的格式规则。
type FormatRuleProvider interface {
	Rule() dataitem.FormatRule
}

// FormatRulesProvider 声明 resolver 背后的多条格式规则。
type FormatRulesProvider interface {
	Rules() []dataitem.FormatRule
}
