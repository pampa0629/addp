package repository

import (
	"fmt"

	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
)

type defaultMeasurementCategory struct {
	name, code, description string
	sortOrder               int
}

type defaultMeasurementUnit struct {
	categoryCode, name, symbol, description string
	sortOrder                               int
}

// 默认目录由 Standard 拥有，不依赖部署脚本或固定租户 ID。
var defaultMeasurementCategories = []defaultMeasurementCategory{
	{"计数", "count", "表示数量、个数、次数等无量纲单位", 1},
	{"货币", "currency", "表示金额、价格等货币值", 2},
	{"质量", "mass", "表示物体质量", 3},
	{"长度", "length", "表示距离、长度", 4},
	{"面积", "area", "表示面积大小", 5},
	{"体积", "volume", "表示体积或容积", 6},
	{"时间", "time", "表示时间长度", 7},
	{"温度", "temperature", "表示温度", 8},
	{"百分比", "percentage", "表示比率、百分比", 9},
	{"存储", "storage", "表示数据存储大小", 10},
}

var defaultMeasurementUnits = []defaultMeasurementUnit{
	{"count", "个", "个", "计量个体数量", 1},
	{"count", "件", "件", "计量件数", 2},
	{"count", "次", "次", "计量操作次数", 3},
	{"count", "条", "条", "计量记录条数", 4},
	{"count", "批", "批", "计量批次", 5},
	{"count", "人", "人", "计量人数", 6},
	{"count", "户", "户", "计量户数", 7},
	{"currency", "元", "¥", "人民币元", 1},
	{"currency", "万元", "万¥", "人民币万元", 2},
	{"currency", "亿元", "亿¥", "人民币亿元", 3},
	{"currency", "美元", "$", "美元", 4},
	{"mass", "克", "g", "克", 1},
	{"mass", "千克", "kg", "千克", 2},
	{"mass", "吨", "t", "吨", 3},
	{"length", "毫米", "mm", "毫米", 1},
	{"length", "厘米", "cm", "厘米", 2},
	{"length", "米", "m", "米", 3},
	{"length", "千米", "km", "千米", 4},
	{"area", "平方米", "m²", "平方米", 1},
	{"area", "亩", "亩", "亩", 2},
	{"area", "公顷", "ha", "公顷", 3},
	{"time", "秒", "s", "秒", 1},
	{"time", "分钟", "min", "分钟", 2},
	{"time", "小时", "h", "小时", 3},
	{"time", "天", "d", "天", 4},
	{"time", "月", "mo", "月", 5},
	{"time", "年", "yr", "年", 6},
	{"percentage", "百分比", "%", "百分比", 1},
	{"storage", "MB", "MB", "兆字节", 1},
	{"storage", "GB", "GB", "吉字节", 2},
	{"storage", "TB", "TB", "太字节", 3},
}

// EnsureDefaults 原子补齐当前租户的默认目录，保留既有身份及自定义内容。
func (r *MeasurementCategoryRepository) EnsureDefaults(tenantID int64) error {
	if tenantID <= 0 {
		return fmt.Errorf("measurement defaults require a tenant")
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		// 多实例和同租户并发首次读取共用事务锁；失败时整体回滚，下次请求可重试。
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", fmt.Sprintf("standard.unit-defaults:%d", tenantID)).Error; err != nil {
				return err
			}
		}
		var categories []models.MeasurementCategory
		if err := tx.Where("tenant_id = ?", tenantID).Find(&categories).Error; err != nil {
			return err
		}
		byCode := make(map[string]int64, len(categories))
		for _, category := range categories {
			byCode[category.Code] = category.ID
		}
		for _, item := range defaultMeasurementCategories {
			if _, exists := byCode[item.code]; exists {
				continue
			}
			category := models.MeasurementCategory{TenantID: tenantID, Name: item.name, Code: item.code, Description: item.description, SortOrder: item.sortOrder, IsSystem: true}
			if err := tx.Create(&category).Error; err != nil {
				return err
			}
			byCode[item.code] = category.ID
		}
		var units []models.Unit
		if err := tx.Where("tenant_id = ?", tenantID).Find(&units).Error; err != nil {
			return err
		}
		type unitKey struct {
			categoryID   int64
			name, symbol string
		}
		existing := make(map[unitKey]bool, len(units))
		for _, unit := range units {
			existing[unitKey{unit.CategoryID, unit.Name, unit.Symbol}] = true
		}
		var missing []models.Unit
		for _, item := range defaultMeasurementUnits {
			categoryID := byCode[item.categoryCode]
			if existing[unitKey{categoryID, item.name, item.symbol}] {
				continue
			}
			missing = append(missing, models.Unit{TenantID: tenantID, CategoryID: categoryID, Name: item.name, Symbol: item.symbol, Description: item.description, SortOrder: item.sortOrder, IsSystem: true})
		}
		if len(missing) > 0 {
			return tx.Create(&missing).Error
		}
		return nil
	})
}
