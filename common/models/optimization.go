package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
)

// OptimizationConfig MVT 瓦片优化配置 (v3.0: 仅保留 Extent 优化和属性优化)
type OptimizationConfig struct {
	Version string `json:"version"` // 配置版本

	// 属性优化
	AttributePruning AttributePruningConfig `json:"attribute_pruning"`

	// 瓦片大小阈值
	TileSizeThresholds TileSizeThresholdsConfig `json:"tile_size_thresholds"`

	// Extent 优化（分层策略 + 动态减半）
	ExtentOptimization ExtentOptimizationConfig `json:"extent_optimization"`
}

// AttributePruningConfig 属性优化配置
type AttributePruningConfig struct {
	Enabled       bool `json:"enabled"`        // 是否启用
	ZoomThreshold int  `json:"zoom_threshold"` // zoom 分界线，<= threshold 只返回主键
}

// TileSizeThresholdsConfig 瓦片大小阈值配置 (v3.0: 仅保留最大大小限制)
type TileSizeThresholdsConfig struct {
	MaxSizeMB float64 `json:"max_size_mb"` // 瓦片大小限制（MB）- 用户可配置，默认 5MB
}

// ExtentOptimizationConfig Extent 优化配置 (v3.0: 分层策略 + 动态减半)
type ExtentOptimizationConfig struct {
	MaxZoomExtent int `json:"max_zoom_extent"` // Max zoom 层的 Extent（固定 2048）
	BaseExtent    int `json:"base_extent"`     // 其他层的基础 Extent（固定 1024）
	MinExtent     int `json:"min_extent"`      // 动态减半的最小 Extent（固定 256）
}

// DefaultOptimizationConfig 返回默认优化配置 (v3.0)
func DefaultOptimizationConfig() OptimizationConfig {
	return OptimizationConfig{
		Version: "3.0",
		AttributePruning: AttributePruningConfig{
			Enabled:       true,
			ZoomThreshold: 8,
		},
		TileSizeThresholds: TileSizeThresholdsConfig{
			MaxSizeMB: 5.0,
		},
		ExtentOptimization: ExtentOptimizationConfig{
			MaxZoomExtent: 2048,
			BaseExtent:    1024,
			MinExtent:     256,
		},
	}
}

// GetExtentForZoom 根据 zoom 级别计算初始 Extent (v3.0: 分层策略)
// - Max zoom: 2048
// - 其他: 1024
func (e *ExtentOptimizationConfig) GetExtentForZoom(z int, maxZoom int) int {
	if z == maxZoom {
		return e.MaxZoomExtent
	}
	return e.BaseExtent
}

// Scan 实现 sql.Scanner 接口，用于从数据库读取 JSONB
func (c *OptimizationConfig) Scan(value interface{}) error {
	if value == nil {
		*c = DefaultOptimizationConfig()
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}

	if err := json.Unmarshal(bytes, c); err != nil {
		return err
	}

	return nil
}

// Value 实现 driver.Valuer 接口，用于写入数据库 JSONB
func (c OptimizationConfig) Value() (driver.Value, error) {
	bytes, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	return string(bytes), nil
}
