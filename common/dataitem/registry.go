package dataitem

import (
	"fmt"
	"sort"
	"sync"
)

var (
	mu        sync.RWMutex
	detectors []CompositeItemDetector
)

// Register 注册组合数据项检测器。
func Register(d CompositeItemDetector) {
	if provider, ok := d.(FormatRulesProvider); ok {
		for _, rule := range provider.Rules() {
			if err := ValidateFormatRule(rule); err != nil {
				panic(fmt.Sprintf("invalid dataitem detector rule: %v", err))
			}
		}
	}
	if provider, ok := d.(FormatRuleProvider); ok {
		if err := ValidateFormatRule(provider.Rule()); err != nil {
			panic(fmt.Sprintf("invalid dataitem detector rule: %v", err))
		}
	}
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
