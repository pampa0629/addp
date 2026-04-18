package detector

import (
	"context"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
)

// CompositeItemDetector 复合数据项检测器
// 当一个目录被识别为某种复合数据类型时，整个目录作为一个 MetaItem，内部文件不再单独扫描
type CompositeItemDetector interface {
	// Detect 根据目录内容判断是否匹配（不需要读文件内容）
	Detect(ctx context.Context, files []plugin.FileEntry, subdirs []plugin.DirEntry) bool

	// ExtractItemInfo 匹配后提取元信息（需要读内容时通过 plugin 读取）
	ExtractItemInfo(ctx context.Context, fsPlugin plugin.FileSystemPlugin,
		connInfo plugin.ConnectionInfo, dirPath string,
		files []plugin.FileEntry) (*CompositeItemInfo, error)

	// Priority 优先级，越大越先检测
	Priority() int

	// ItemType 对应 MetaItem.item_type
	ItemType() string
}

// CompositeItemInfo 复合数据项信息
type CompositeItemInfo struct {
	Fields     []format.FieldInfo             // 列定义，复用现有 FieldInfo
	Attributes map[string]interface{}         // 其他扩展属性
}
