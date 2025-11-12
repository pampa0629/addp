package models

// DataSource 数据源配置
type DataSource struct {
	ID         string           `yaml:"id" json:"id"`
	Name       string           `yaml:"name" json:"name"`
	Type       string           `yaml:"type" json:"type"`
	Connection ConnectionConfig `yaml:"connection" json:"connection"`
	TileConfig TileConfig       `yaml:"tile_config" json:"tile_config"`
	Properties []Property       `yaml:"properties" json:"properties"`
	Filters    []string         `yaml:"filters" json:"filters"`
}

// ConnectionConfig 数据库连接配置
type ConnectionConfig struct {
	Schema         string `yaml:"schema" json:"schema"`
	Table          string `yaml:"table" json:"table"`
	GeometryColumn string `yaml:"geometry_column" json:"geometry_column"`
	SRID           int    `yaml:"srid" json:"srid"`
}

// TileConfig 瓦片配置
type TileConfig struct {
    MinZoom        int     `yaml:"min_zoom" json:"min_zoom"`
    MaxZoom        int     `yaml:"max_zoom" json:"max_zoom"`
    Buffer         int     `yaml:"buffer" json:"buffer"`
    Extent         int     `yaml:"extent" json:"extent"`
    Simplification float64 `yaml:"simplification" json:"simplification"`
    MinPxLine      float64 `yaml:"min_px_line" json:"min_px_line"`
    MinPxPoly      float64 `yaml:"min_px_poly" json:"min_px_poly"`
    PixelRules     []PixelRule `yaml:"pixel_rules" json:"pixel_rules"`
}

// PixelRule 瓦片像素阈值规则（按缩放区间）
type PixelRule struct {
    MinZoom    int     `yaml:"min_zoom" json:"min_zoom"`
    MaxZoom    int     `yaml:"max_zoom" json:"max_zoom"`
    MinPxLine  float64 `yaml:"min_px_line" json:"min_px_line"`
    MinPxPoly  float64 `yaml:"min_px_poly" json:"min_px_poly"`
}

// Property 属性字段定义
type Property struct {
	Name string `yaml:"name" json:"name"`
	Type string `yaml:"type" json:"type"`
}

// DataSourcesConfig 数据源配置文件根结构
type DataSourcesConfig struct {
	DataSources []DataSource `yaml:"datasources" json:"datasources"`
}
