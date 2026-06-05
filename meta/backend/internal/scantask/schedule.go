package scantask

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	commonModels "github.com/addp/common/models"
	commonScheduler "github.com/addp/common/scheduler"
)

func BuildCronExpressionFromPolicy(builder *commonScheduler.ExpressionBuilder, policy *commonModels.ScanConfig) (string, error) {
	if policy == nil {
		return "", errors.New("扫描配置为空")
	}

	switch policy.ScheduleType {
	case "manual":
		return "", nil
	case "cron":
		if policy.CronExpression == "" {
			return "", errors.New("Cron 类型必须提供 cron_expression")
		}
		if builder != nil {
			if err := builder.Validate(policy.CronExpression); err != nil {
				return "", fmt.Errorf("无效的 Cron 表达式: %w", err)
			}
		}
		return policy.CronExpression, nil
	case "daily":
		hour, minute := ParseScheduleTime(policy.ScheduleTime)
		return fmt.Sprintf("%d %d * * *", minute, hour), nil
	case "weekly":
		hour, minute := ParseScheduleTime(policy.ScheduleTime)
		days := "0"
		if len(policy.ScheduleValue) > 0 {
			dayStrs := make([]string, len(policy.ScheduleValue))
			for i, day := range policy.ScheduleValue {
				dayStrs[i] = strconv.Itoa(day)
			}
			days = strings.Join(dayStrs, ",")
		}
		return fmt.Sprintf("%d %d * * %s", minute, hour, days), nil
	case "monthly":
		hour, minute := ParseScheduleTime(policy.ScheduleTime)
		dates := "1"
		if len(policy.ScheduleValue) > 0 {
			dateStrs := make([]string, len(policy.ScheduleValue))
			for i, date := range policy.ScheduleValue {
				dateStrs[i] = strconv.Itoa(date)
			}
			dates = strings.Join(dateStrs, ",")
		}
		return fmt.Sprintf("%d %d %s * *", minute, hour, dates), nil
	default:
		return "", fmt.Errorf("不支持的调度类型: %s", policy.ScheduleType)
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
