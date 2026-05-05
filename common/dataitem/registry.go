package dataitem

import (
	"sort"
	"sync"
)

var (
	mu        sync.RWMutex
	detectors []CompositeItemDetector
)

// Register 注册组合数据项检测器。
func Register(d CompositeItemDetector) {
	mu.Lock()
	defer mu.Unlock()
	detectors = append(detectors, d)
}

// GetAll 按优先级排序返回所有检测器，优先级高的在前。
func GetAll() []CompositeItemDetector {
	mu.RLock()
	defer mu.RUnlock()

	result := make([]CompositeItemDetector, len(detectors))
	copy(result, detectors)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Priority() > result[j].Priority()
	})
	return result
}
