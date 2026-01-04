package format

// ExtensionInfo 扩展信息接口（标记接口）
// 所有扩展信息类型都必须实现此接口
type ExtensionInfo interface {
	// ExtensionType 返回扩展类型标识
	// 常见值: "spatial", "csv", "shapefile", "image", "video"
	ExtensionType() string
}
