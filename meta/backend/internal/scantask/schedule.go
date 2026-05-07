package scantask

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	commonModels "github.com/addp/common/models"
	commonScheduler "github.com/addp/common/scheduler"
)

func BuildCronExpressionFromScanConfig(builder *commonScheduler.ExpressionBuilder, config *commonModels.ScanConfig) (string, error) {
	if config == nil {
		return "", errors.New("扫描配置为空")
	}

	switch config.ScheduleType {
	case "manual":
		return "", nil
	case "cron":
		if config.CronExpression == "" {
			return "", errors.New("Cron 类型必须提供 cron_expression")
		}
		if builder != nil {
			if err := builder.Validate(config.CronExpression); err != nil {
				return "", fmt.Errorf("无效的 Cron 表达式: %w", err)
			}
		}
		return config.CronExpression, nil
	case "daily":
		hour, minute := ParseScheduleTime(config.ScheduleTime)
		return fmt.Sprintf("%d %d * * *", minute, hour), nil
	case "weekly":
		hour, minute := ParseScheduleTime(config.ScheduleTime)
		days := "0"
		if len(config.ScheduleValue) > 0 {
			dayStrs := make([]string, len(config.ScheduleValue))
			for i, day := range config.ScheduleValue {
				dayStrs[i] = strconv.Itoa(day)
			}
			days = strings.Join(dayStrs, ",")
		}
		return fmt.Sprintf("%d %d * * %s", minute, hour, days), nil
	case "monthly":
		hour, minute := ParseScheduleTime(config.ScheduleTime)
		dates := "1"
		if len(config.ScheduleValue) > 0 {
			dateStrs := make([]string, len(config.ScheduleValue))
			for i, date := range config.ScheduleValue {
				dateStrs[i] = strconv.Itoa(date)
			}
			dates = strings.Join(dateStrs, ",")
		}
		return fmt.Sprintf("%d %d %s * *", minute, hour, dates), nil
	default:
		return "", fmt.Errorf("不支持的调度类型: %s", config.ScheduleType)
	}
}

func ParseScheduleTime(scheduleTime string) (hour, minute int) {
	if scheduleTime == "" {
		return 0, 0
	}
	parts := strings.Split(scheduleTime, ":")
	if len(parts) == 2 {
		if h, err := strconv.Atoi(parts[0]); err == nil {
			hour = h
		}
		if m, err := strconv.Atoi(parts[1]); err == nil {
			minute = m
		}
	}
	return hour, minute
}
