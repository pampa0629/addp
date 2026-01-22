package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
)

// OptimizationConfig MVT 瓦片优化配置
type OptimizationConfig struct {
	Version string `json:"version"` // 配置版本

	// 属性优化
	AttributePruning AttributePruningConfig `json:"attribute_pruning"`

	// 瓦片大小阈值
	TileSizeThresholds TileSizeThresholdsConfig `json:"tile_size_thresholds"`

	// Extent 优化
	ExtentOptimization ExtentOptimizationConfig `json:"extent_optimization"`

	// 对象采样
	Sampling SamplingConfig `json:"sampling"`

	// 几何简化
	Simplification SimplificationConfig `json:"simplification"`
}

// AttributePruningConfig 属性优化配置
type AttributePruningConfig struct {
	Enabled       bool `json:"enabled"`        // 是否启用
	ZoomThreshold int  `json:"zoom_threshold"` // zoom 分界线，<= threshold 只返回主键
}

// TileSizeThresholdsConfig 瓦片大小阈值配置
type TileSizeThresholdsConfig struct {
	NoOptimizationMB  float64 `json:"no_optimization_mb"`   // 小于此值不优化（MB）
	StopOptimizationMB float64 `json:"stop_optimization_mb"` // 达到此值停止优化（MB）
}

// ExtentOptimizationConfig Extent 优化配置
type ExtentOptimizationConfig struct {
	BlurLevel int `json:"blur_level"` // 模糊度: 1(清晰,4096), 2(适中,2048), 4(模糊,1024)
}

// SamplingConfig 对象采样配置
type SamplingConfig struct {
	PolygonLine PolygonLineSamplingConfig `json:"polygon_line"` // 面/线要素采样
	Point       PointSamplingConfig       `json:"point"`        // 点要素采样
}

// PolygonLineSamplingConfig 面/线要素采样配置
type PolygonLineSamplingConfig struct {
	CumulativeSizeRatio   float64 `json:"cumulative_size_ratio"`    // 面积/长度占比（0.5-1.0）
	MaxFeatureCountRatio  float64 `json:"max_feature_count_ratio"`  // 对象数量占比（0.3-1.0）
}

// PointSamplingConfig 点要素采样配置
type PointSamplingConfig struct {
	SampleRatio float64 `json:"sample_ratio"` // 点保留比例（0.3-1.0）
}

// SimplificationConfig 几何简化配置
type SimplificationConfig struct {
	ToleranceMultiplier float64 `json:"tolerance_multiplier"` // 容错度倍数: 2, 4, 8
	Algorithm           string  `json:"algorithm"`            // 算法: visvalingam | douglas_peucker
}

// DefaultOptimizationConfig 返回默认优化配置
func DefaultOptimizationConfig() OptimizationConfig {
	return OptimizationConfig{
		Version: "2.0",
		AttributePruning: AttributePruningConfig{
			Enabled:       true,
			ZoomThreshold: 8,
		},
		TileSizeThresholds: TileSizeThresholdsConfig{
			NoOptimizationMB:  2.0,
			StopOptimizationMB: 5.0,
		},
		ExtentOptimization: ExtentOptimizationConfig{
			BlurLevel: 2, // 默认适中
		},
		Sampling: SamplingConfig{
			PolygonLine: PolygonLineSamplingConfig{
				CumulativeSizeRatio:  0.8,
				MaxFeatureCountRatio: 0.6,
			},
			Point: PointSamplingConfig{
				SampleRatio: 0.6,
			},
		},
		Simplification: SimplificationConfig{
			ToleranceMultiplier: 4.0,
			Algorithm:           "visvalingam",
		},
	}
}

// GetExtent 根据 blur_level 计算 extent
func (c *OptimizationConfig) GetExtent() int {
	baseExtent := 4096
	if c.ExtentOptimization.BlurLevel <= 0 {
		return baseExtent
	}
	return baseExtent / c.ExtentOptimization.BlurLevel
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
