package client

// DiscoverableAsset 可注册为资产的源模块资源
//
// 各源模块（Meta/Service/Standard/Develop）的内部 assets/discoverable 接口
// 均须返回此格式，作为跨模块资产发现的统一数据契约。
// 具体 HTTP 调用由各消费方（如 asset 模块）自行实现，保持模块间松耦合。
type DiscoverableAsset struct {
	SourceReference string `json:"source_reference"` // 在源模块中的唯一标识
	Name            string `json:"name"`             // 资源名称
	Description     string `json:"description"`      // 描述（可空）
}
