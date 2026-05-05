package detector

import (
	"github.com/addp/common/dataitem"
)

// Register 注册检测器（通常在 init() 中调用）。
//
// Deprecated: 请改用 common/dataitem.Register。
func Register(d CompositeItemDetector) {
	dataitem.Register(d)
}

// GetAll 按优先级排序返回所有检测器（优先级高的在前）。
//
// Deprecated: 请改用 common/dataitem.GetAll。
func GetAll() []CompositeItemDetector {
	registered := dataitem.GetAll()
	result := make([]CompositeItemDetector, 0, len(registered))
	for _, d := range registered {
		result = append(result, d)
	}
	return result
}
