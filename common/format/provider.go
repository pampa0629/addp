package format

import (
	"context"
	"io"

	"github.com/addp/common/contentio"
)

// Provider 是格式层能力实现的基础接口。
//
// Provider 不接 engine id，不创建资源读取器，也不返回上层模块专用 DTO。
// 调用方应先基于 engine capability 构造读取抽象或内容流，再交给 Provider 解码。
type Provider interface {
	Format() FormatType
	Capabilities() FormatCapability
}

// FormatPlugin 表示一个格式实现的稳定身份入口。
//
// Plugin 只声明格式身份、descriptor 和 capability，不负责识别某个资源、
// 不决定 data item 边界，也不替代具体 InfoProvider / ContentReader。
type FormatPlugin interface {
	Format() FormatType
	Descriptor() FormatDescriptor
	Capabilities() FormatCapability
}

// FormatInfoProvider 表示格式能够提供自身私有元数据。
//
// 结果应写入 attributes.format_info.<format>，不得混入 type_info 或上层模块 DTO。
type FormatInfoProvider interface {
	Provider
	DescribeFormat(ctx context.Context, input io.Reader, options *ParseOptions) (map[string]interface{}, error)
}

// TableInfoProvider 表示格式能够从外部提供的资源流中提取 table 类型信息。
//
// 类型信息是 meta 层写入 type_info.table 的主来源，不应夹带 Manager 展示 DTO。
type TableInfoProvider interface {
	Provider
	DescribeTable(ctx context.Context, input io.Reader, options *ParseOptions) (*TableInfo, error)
}

// ContentReader 是内容读取能力的标记接口。
//
// Reader 命名用于区分“读取内容数据”和“提供元数据”的 provider。
type ContentReader interface {
	Provider
}

// TableSampleReader 表示格式能够从外部提供的资源流中读取 table 样本数据。
//
// 样本数据是面向 Manager 内容查看、Transfer 探查等消费场景的数据能力，
// 和 TableInfoProvider 并列注册，避免把“类型信息”和“内容数据”绑死。
// offset / limit 是逻辑数据行窗口，不是字节偏移。input 默认从资源起点开始；
// 如果调用方基于 content_index 传入局部流，必须通过 ParseOptions.TableSample
// 提供定位上下文。
type TableSampleReader interface {
	ContentReader
	SampleTable(ctx context.Context, input io.Reader, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error)
}

// TableSampleProvider 是旧命名兼容别名，新代码应使用 TableSampleReader。
type TableSampleProvider = TableSampleReader

// TableReaderProvider 表示格式能够从外部提供的资源流中打开连续 table 行读取会话。
//
// 它面向 Transfer 等全量读取场景；与 TableSampleReader 的逻辑窗口读取不同，
// TableReader 持有一次输入流的读取状态，调用方循环 ReadRows 直到返回空结果。
type TableReaderProvider interface {
	ContentReader
	OpenTableReader(ctx context.Context, input io.Reader, options *ParseOptions) (TableReader, error)
}

type TableReader interface {
	Schema() *TableInfo
	ReadRows(ctx context.Context, limit int) ([]map[string]interface{}, error)
	Close(ctx context.Context) error
}

// MultiTableReaderProvider 表示多 ref 格式能够打开连续 table 行读取会话。
//
// 它面向 Transfer 等全量读取场景；与 MultiTableProvider 的 SampleMultiTable
// 不同，TableReader 持有一次ref 读取状态，调用方循环 ReadRows 直到返回空结果。
type MultiTableReaderProvider interface {
	Provider
	RelatedRefSpecs() []contentio.RelatedRefSpec
	OpenMultiTableReader(ctx context.Context, reader contentio.Reader, refs []contentio.Ref, options *ParseOptions) (TableReader, error)
}

// TableProvider 是兼容旧调用方的组合接口。
//
// 新代码优先按 TableInfoProvider / TableSampleReader 分别表达调用意图；
// 已有 plugin 同时实现两者时仍可注册为 TableProvider。
type TableProvider interface {
	TableInfoProvider
	TableSampleReader
}

// TableWriterProvider 表示格式能够把 table 数据写出为该格式编码。
//
// Provider 是可注册的无状态能力入口；TableWriter 是一次输出会话的状态对象。
// 调用方负责基于 engine/resource 打开 io.Writer，再交给格式 writer 编码。
type TableWriterProvider interface {
	Provider
	OpenTableWriter(ctx context.Context, output io.Writer, schema *TableInfo, options *WriteOptions) (TableWriter, error)
}

// MultiTableWriterProvider 表示格式能够把 table 数据写出为多 ref 资源。
//
// 典型场景是 Shapefile 这类天然由 .shp/.shx/.dbf 等相关 ref 共同构成的格式。
// target 表示目标主资源，provider 基于它和 RelatedRefSpecs 派生ref 路径；
// output 负责把ref 写入具体 engine/resource。
type MultiTableWriterProvider interface {
	Provider
	RelatedRefSpecs() []contentio.RelatedRefSpec
	OpenMultiTableWriter(ctx context.Context, writer contentio.Writer, refs []contentio.Ref, target contentio.Ref, schema *TableInfo, options *WriteOptions) (TableWriter, error)
}

type TableWriter interface {
	WriteRows(ctx context.Context, rows []map[string]interface{}) error
	Close(ctx context.Context) error
}

type DocumentInfo struct {
	Format    FormatType
	Title     string
	Language  string
	Encoding  string
	SizeBytes *int64
}

type MediaInfo struct {
	Format       FormatType
	MediaType    string
	MIMEType     string
	Width        int
	Height       int
	DurationMS   *int64
	Encoding     string
	ColorSpace   string
	SizeBytes    *int64
	SpatialAttrs map[string]interface{}
}

// ContainerInfoProvider 表示格式能够提供容器内部对象信息。
//
// 结果应由 Meta 映射到 type_info.container 和 format_info.<format>；
// provider 不决定 child 是否成为独立 data item，也不返回上层展示 DTO。
type ContainerInfoProvider interface {
	Provider
	DescribeContainer(ctx context.Context, input io.Reader, options *ParseOptions) (*ContainerInfo, error)
}

type DocumentInfoProvider interface {
	Provider
	DescribeDocument(ctx context.Context, input io.Reader, options *ParseOptions) (*DocumentInfo, error)
}

type DocumentTextReader interface {
	ContentReader
	ReadDocumentText(ctx context.Context, input io.Reader, limit int64, options *ParseOptions) (string, bool, error)
}

// DocumentProvider 是兼容期组合接口，等价于同时具备 DocumentInfoProvider 和 DocumentTextReader。
type DocumentProvider interface {
	DocumentInfoProvider
	DocumentTextReader
}

type MediaInfoProvider interface {
	Provider
	DescribeMedia(ctx context.Context, input io.Reader, options *ParseOptions) (*MediaInfo, error)
}

// MediaProvider 是旧命名兼容别名，新代码应使用 MediaInfoProvider。
type MediaProvider = MediaInfoProvider

type MultiTableProvider interface {
	TableProvider
	DescribeMultiTable(ctx context.Context, reader contentio.Reader, refs []contentio.Ref, options *ParseOptions) (*TableInfo, error)
	SampleMultiTable(ctx context.Context, reader contentio.Reader, refs []contentio.Ref, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error)
}

// RelatedRefSpecProvider 表示格式能够声明多 ref 资源的 ref 规格。
//
// 该接口只描述 ref 角色和必需性，调用方仍负责 data item 边界识别、
// 资源路径发现与 contentio.Reader 构造。
type RelatedRefSpecProvider interface {
	Provider
	RelatedRefSpecs() []contentio.RelatedRefSpec
}

type RefDescriptor struct {
	Key       string     `json:"key,omitempty"`
	Path      string     `json:"path"`
	Role      string     `json:"role,omitempty"`
	Label     string     `json:"label,omitempty"`
	Required  bool       `json:"required,omitempty"`
	Primary   bool       `json:"primary,omitempty"`
	DataType  string     `json:"data_type,omitempty"`
	Format    FormatType `json:"format,omitempty"`
	Extension string     `json:"extension,omitempty"`
}

// RefDescriptorProvider 表示格式能够解释 multi item 的 refs。
//
// 该接口只提供用户可理解的 ref 描述，不参与 data item 边界识别；
// Meta、Manager、前端不得硬编码某个格式的 ref 语义。
type RefDescriptorProvider interface {
	Provider
	DescribeRefs(refs []contentio.Ref) []RefDescriptor
}

type ScopeTableProvider interface {
	TableProvider
	DescribeTableScope(ctx context.Context, reader contentio.Reader, scope contentio.Ref, options *ParseOptions) (*TableInfo, error)
	SampleTableScope(ctx context.Context, reader contentio.Reader, scope contentio.Ref, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error)
}
