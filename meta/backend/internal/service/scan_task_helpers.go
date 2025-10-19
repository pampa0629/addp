package service

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

func normalizeStorageType(resourceType string) string {
	normalized := strings.ToLower(strings.TrimSpace(resourceType))
	if normalized == "" {
		return "unknown"
	}
	normalized = strings.ReplaceAll(normalized, " ", "_")
	return normalized
}

func normalizeTriggerLabel(triggerType string) string {
	switch triggerType {
	case triggerTypeManual:
		return "手动"
	case triggerTypeScheduled:
		return "定时"
	case triggerTypeSystem:
		return "系统"
	default:
		fallback := strings.TrimSpace(triggerType)
		if fallback == "" {
			return "未知"
		}
		return fallback
	}
}

func buildRunName(storageType, triggerType string, runID uint) string {
	trigger := normalizeTriggerLabel(triggerType)
	return fmt.Sprintf("%s-%s-%06d", normalizeStorageType(storageType), trigger, runID)
}

func setRunName(db *gorm.DB, run *models.ScanTaskRun, storageType, triggerType string, log *slog.Logger) {
	if run == nil {
		return
	}

	name := buildRunName(storageType, triggerType, run.ID)
	update := map[string]interface{}{
		"name":       name,
		"updated_at": time.Now(),
	}
	if err := db.Model(&models.ScanTaskRun{}).Where("id = ?", run.ID).Updates(update).Error; err != nil {
		if log != nil {
			log.Warn("更新运行名称失败", "run_id", run.ID, "error", err)
		}
		return
	}
	run.Name = name
}
