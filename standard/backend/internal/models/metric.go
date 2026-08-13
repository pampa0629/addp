package models

import "time"

// MetricCategory 指标目录（独立树形结构）
type MetricCategory struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID    int64     `gorm:"not null;index;uniqueIndex:uq_standard_metric_categories_tenant_code" json:"tenant_id"`
	Name        string    `gorm:"size:100;not null" json:"name"`
	Code        string    `gorm:"size:50;not null;uniqueIndex:uq_standard_metric_categories_tenant_code" json:"code"`
	Description string    `gorm:"type:text" json:"description"`
	ParentID    *int64    `gorm:"index" json:"parent_id,omitempty"`
	SortOrder   int       `gorm:"default:0" json:"sort_order"`
	CreatedBy   int64     `gorm:"not null" json:"created_by"`
	UpdatedBy   *int64    `json:"updated_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Version     int64     `gorm:"not null;default:1" json:"version"`
}

func (MetricCategory) TableName() string {
	return "standard.metric_categories"
}

// Metric 指标定义
type Metric struct {
	ID               int64       `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID         int64       `gorm:"not null;index;uniqueIndex:uq_standard_metrics_tenant_code" json:"tenant_id"`
	CategoryID       *int64      `gorm:"index" json:"category_id,omitempty"`
	DomainID         *int64      `gorm:"index" json:"domain_id,omitempty"` // 辅助标注，关联的主业务域
	Name             string      `gorm:"size:200;not null" json:"name"`
	Code             string      `gorm:"size:100;not null;uniqueIndex:uq_standard_metrics_tenant_code" json:"code"`
	Type             string      `gorm:"size:20;not null" json:"type"` // atomic/derived/composite
	Definition       string      `gorm:"type:text" json:"definition"`  // 业务口径描述
	Formula          string      `gorm:"type:text" json:"formula"`     // 复合指标：计算公式描述
	UnitID           *int64      `gorm:"index" json:"unit_id,omitempty"`
	BaseMetricID     *int64      `gorm:"index" json:"base_metric_id,omitempty"` // 派生指标的基准原子指标
	DerivationConfig JSONB       `gorm:"type:jsonb;serializer:json" json:"derivation_config"`
	Status           string      `gorm:"size:20;default:'draft'" json:"status"`
	StewardID        *int64      `json:"steward_id,omitempty"`
	Tags             StringArray `gorm:"type:jsonb;serializer:json" json:"tags"`
	CreatedBy        int64       `gorm:"not null" json:"created_by"`
	UpdatedBy        *int64      `json:"updated_by,omitempty"`
	CreatedAt        time.Time   `json:"created_at"`
	UpdatedAt        time.Time   `json:"updated_at"`
	Version          int64       `gorm:"not null;default:1" json:"version"`
}

func (Metric) TableName() string {
	return "standard.metrics"
}

// MetricElementMapping 指标与数据元关联（原子指标）
type MetricElementMapping struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	MetricID  int64     `gorm:"not null;index;uniqueIndex:uq_standard_metric_element_mappings_metric_element" json:"metric_id"`
	ElementID int64     `gorm:"not null;uniqueIndex:uq_standard_metric_element_mappings_metric_element" json:"element_id"`
	Note      string    `gorm:"type:text" json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

func (MetricElementMapping) TableName() string {
	return "standard.metric_element_mappings"
}

// MetricDependency 复合指标依赖关系
type MetricDependency struct {
	ID           int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	FromMetricID int64     `gorm:"not null;index;uniqueIndex:uq_standard_metric_dependencies_from_to" json:"from_metric_id"`
	ToMetricID   int64     `gorm:"not null;uniqueIndex:uq_standard_metric_dependencies_from_to" json:"to_metric_id"`
	Coefficient  *float64  `json:"coefficient,omitempty"`
	Note         string    `gorm:"type:text" json:"note"`
	CreatedAt    time.Time `json:"created_at"`
}

func (MetricDependency) TableName() string {
	return "standard.metric_dependencies"
}

// CreateMetricCategoryRequest 创建指标目录请求
type CreateMetricCategoryRequest struct {
	Name        string `json:"name" binding:"required"`
	Code        string `json:"code" binding:"required"`
	Description string `json:"description"`
	ParentID    *int64 `json:"parent_id,omitempty"`
	SortOrder   int    `json:"sort_order"`
}

// UpdateMetricCategoryRequest 更新指标目录请求
type UpdateMetricCategoryRequest struct {
	Version     int64  `json:"version" binding:"required"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ParentID    *int64 `json:"parent_id,omitempty"`
	SortOrder   int    `json:"sort_order"`
}

// CreateMetricRequest 创建指标请求
type CreateMetricRequest struct {
	CategoryID       *int64                 `json:"category_id,omitempty"`
	DomainID         *int64                 `json:"domain_id,omitempty"`
	Name             string                 `json:"name" binding:"required"`
	Code             string                 `json:"code" binding:"required"`
	Type             string                 `json:"type" binding:"required"` // atomic/derived/composite
	Definition       string                 `json:"definition"`
	Formula          string                 `json:"formula"`
	UnitID           *int64                 `json:"unit_id,omitempty"`
	BaseMetricID     *int64                 `json:"base_metric_id,omitempty"`
	DerivationConfig map[string]interface{} `json:"derivation_config"`
	StewardID        *int64                 `json:"steward_id,omitempty"`
	Tags             []string               `json:"tags"`
	ElementIDs       []int64                `json:"element_ids"`    // 关联数据元（原子指标）
	DependencyIDs    []int64                `json:"dependency_ids"` // 依赖指标（复合指标）
}

// UpdateMetricRequest 更新指标请求
type UpdateMetricRequest struct {
	Version          int64                  `json:"version" binding:"required"`
	CategoryID       *int64                 `json:"category_id,omitempty"`
	DomainID         *int64                 `json:"domain_id,omitempty"`
	Name             string                 `json:"name"`
	Type             string                 `json:"type"`
	Definition       string                 `json:"definition"`
	Formula          string                 `json:"formula"`
	UnitID           *int64                 `json:"unit_id,omitempty"`
	BaseMetricID     *int64                 `json:"base_metric_id,omitempty"`
	DerivationConfig map[string]interface{} `json:"derivation_config"`
	StewardID        *int64                 `json:"steward_id,omitempty"`
	Tags             []string               `json:"tags"`
	ElementIDs       []int64                `json:"element_ids"`
	DependencyIDs    []int64                `json:"dependency_ids"`
}
