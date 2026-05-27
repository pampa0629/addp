package format

import (
	"context"
	"io"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
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

// ContentSniffer 表示格式 plugin 可以基于内容前缀自行判断是否认领资源。
//
// 该接口只用于格式身份兜底识别；调用方应传入已截断的 peek bytes，
// 不应让 sniffer 负责打开 engine 资源或决定 data item 边界。
type ContentSniffer interface {
	SniffFormat(peek []byte) bool
}

// FormatInfoProvider 表示格式能够提供自身私有元数据。
//
// 结果是当前格式的裸 format_info 内容；调用编排层负责按格式名写入
// attributes.format_info.<format>，provider 不得混入 type_info 或上层模块 DTO。
type FormatInfoProvider interface {
	Provider
	DescribeFormat(ctx context.Context, input io.Reader, options *ParseOptions) (map[string]interface{}, error)
}

// TableInfoProvider 表示格式能够从外部提供的资源流中提取 table 类型信息。
//
// 类型信息是 meta 层写入 type_info.table 的主来源，不应夹带 Manager 展示 DTO。
type TableInfoProvider interface {
	Provider
	DescribeTable(ctx context.Context, input io.Reader, options *ParseOptions) (*TableDescribeResult, error)
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

// TableReaderProvider 表示格式能够从外部提供的资源流中打开连续 table 行读取会话。
//
// 它面向 Transfer 等全量读取场景；与 TableSampleReader 的逻辑窗口读取不同，
// TableReader 持有一次输入流的读取状态，调用方循环 ReadRows 直到返回空结果。
type TableReaderProvider interface {
	ContentReader
	OpenTableReader(ctx context.Context, input io.Reader, options *ParseOptions) (TableReader, error)
}

type TableReader interface {
	Fields() []datatype.FieldInfo
	ReadRows(ctx context.Context, limit int) ([]map[string]interface{}, error)
	Close(ctx context.Context) error
}

// TableSpatialInfoProvider 表示 table reader 可额外提供读取行对应的空间上下文。
//
// 这是 TableReader 的可选能力，不代表一个独立 reader。调用方通过类型断言使用：
// provider, ok := reader.(TableSpatialInfoProvider)
type TableSpatialInfoProvider interface {
	SpatialInfo() *datatype.SpatialInfo
}

// MultiTableInfoProvider 表示多 ref table 格式能够提取 table 类型信息。
type MultiTableInfoProvider interface {
	Provider
	RelatedRefSpecs() []RelatedRefSpec
	DescribeMultiTable(ctx context.Context, reader contentio.Reader, refs []RelatedRef, options *ParseOptions) (*TableDescribeResult, error)
}

// MultiTableSampleReader 表示多 ref table 格式能够读取 table 样本数据。
type MultiTableSampleReader interface {
	ContentReader
	RelatedRefSpecs() []RelatedRefSpec
	SampleMultiTable(ctx context.Context, reader contentio.Reader, refs []RelatedRef, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error)
}

// MultiTableReaderProvider 表示多 ref 格式能够打开连续 table 行读取会话。
//
// 它面向 Transfer 等全量读取场景；与 MultiTableSampleReader 的 SampleMultiTable
// 不同，TableReader 持有一次 ref 读取状态，调用方循环 ReadRows 直到返回空结果。
type MultiTableReaderProvider interface {
	Provider
	RelatedRefSpecs() []RelatedRefSpec
	OpenMultiTableReader(ctx context.Context, reader contentio.Reader, refs []RelatedRef, options *ParseOptions) (TableReader, error)
}

// ScopeTableInfoProvider 表示 whole scope table 格式能够提取 table 类型信息。
type ScopeTableInfoProvider interface {
	Provider
	DescribeTableScope(ctx context.Context, reader contentio.Reader, scope contentio.Ref, options *ParseOptions) (*TableDescribeResult, error)
}

// ScopeTableSampleReader 表示 whole scope table 格式能够读取 table 样本数据。
type ScopeTableSampleReader interface {
	ContentReader
	SampleTableScope(ctx context.Context, reader contentio.Reader, scope contentio.Ref, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error)
}

// ScopeTableReaderProvider 表示 whole scope table 格式能够打开连续 table 行读取会话。
//
// 它面向 Transfer 等全量读取场景；与 ScopeTableSampleReader 的 SampleTableScope
// 不同，TableReader 持有一次 scope 读取状态，调用方循环 ReadRows 直到返回空结果。
type ScopeTableReaderProvider interface {
	Provider
	OpenTableScopeReader(ctx context.Context, reader contentio.Reader, scope contentio.Ref, options *ParseOptions) (TableReader, error)
}

// TableWriterProvider 表示格式能够把 table 数据写出为该格式编码。
//
// Provider 是可注册的无状态能力入口；TableWriter 是一次输出会话的状态对象。
// 调用方负责打开 io.Writer，再交给格式 writer 编码。
type TableWriterProvider interface {
	Provider
	OpenTableWriter(ctx context.Context, output io.Writer, tableInfo *datatype.TableInfo, options *WriteOptions) (TableWriter, error)
}

// MultiTableWriterProvider 表示格式能够把 table 数据写出为多 ref 资源。
//
// 典型场景是 Shapefile 这类天然由 .shp/.shx/.dbf 等相关 ref 共同构成的格式。
// 调用方负责根据 RelatedRefSpecs 解析或构造 refs；provider 只消费这些 refs，
// 并通过调用方提供的 writer 写入对应 content。主输出语义由 refs 中的 Primary
// 标记表达，不额外传递 target，避免 provider 承担资源发现或 engine 路径判断。
type MultiTableWriterProvider interface {
	Provider
	RelatedRefSpecs() []RelatedRefSpec
	OpenMultiTableWriter(ctx context.Context, writer contentio.Writer, refs []RelatedRef, tableInfo *datatype.TableInfo, options *WriteOptions) (TableWriter, error)
}

type TableWriter interface {
	WriteRows(ctx context.Context, rows []map[string]interface{}) error
	Close(ctx context.Context) error
}

// ContainerInfoProvider 表示格式能够提供容器内部对象信息。
//
// 结果应由 Meta 映射到 type_info.container；容器级和 child 级原生事实
// 通过 datatype.ContainerInfo.Native / ContainerChildInfo.Native 承载。
// provider 不决定 child 是否成为独立 data item，也不返回上层展示 DTO。
type ContainerInfoProvider interface {
	Provider
	DescribeContainer(ctx context.Context, input io.Reader, options *ParseOptions) (*datatype.ContainerInfo, error)
}

type DocumentInfoProvider interface {
	Provider
	DescribeDocument(ctx context.Context, input io.Reader, options *ParseOptions) (*datatype.DocumentInfo, error)
}

type DocumentTextReader interface {
	ContentReader
	ReadDocumentText(ctx context.Context, input io.Reader, limit int64, options *ParseOptions) (string, bool, error)
}

// BinaryContentReader 表示格式层通用二进制内容读取能力。
//
// 该 reader 只消费调用方传入的内容流并按 limit 返回原始字节，不参与格式识别，
// 不把 binary 声明为 format identity，也不返回 Manager 等上层模块的展示 DTO。
// 调用方应仅在资源已经被判定为 unknown 且非文本时，把结果投影为自己的预览对象。
type BinaryContentReader interface {
	ContentReader
	ReadBinaryContent(ctx context.Context, input io.Reader, limit int64, options *ParseOptions) (*BinaryContent, error)
}

type MediaInfoProvider interface {
	Provider
	DescribeMedia(ctx context.Context, input io.Reader, options *ParseOptions) (*MediaDescribeResult, error)
}

// RelatedRefSpecProvider 表示格式能够声明多 ref 资源的 ref 规格。
//
// 该接口只描述 ref 角色和必需性，调用方仍负责 data item 边界识别、
// 资源路径发现与 contentio.Reader 构造。
type RelatedRefSpecProvider interface {
	Provider
	RelatedRefSpecs() []RelatedRefSpec
}

type RefDescriptor struct {
	Key       string            `json:"key,omitempty"`
	Path      string            `json:"path"`
	Role      string            `json:"role,omitempty"`
	Label     string            `json:"label,omitempty"`
	Required  bool              `json:"required,omitempty"`
	Primary   bool              `json:"primary,omitempty"`
	DataType  datatype.DataType `json:"data_type,omitempty"`
	Format    FormatType        `json:"format,omitempty"`
	Extension string            `json:"extension,omitempty"`
}

// RefDescriptorProvider 表示格式能够解释 multi item 的 refs。
//
// 该接口只提供用户可理解的 ref 描述，不参与 data item 边界识别；
// Meta、Manager、前端不得硬编码某个格式的 ref 语义。
type RefDescriptorProvider interface {
	Provider
	DescribeRefs(refs []RelatedRef) []RefDescriptor
}
