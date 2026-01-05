package scheduler

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// ScheduleConfig UI 调度配置
type ScheduleConfig struct {
	Type  string      // manual/daily/weekly/monthly/cron
	Time  string      // HH:MM 格式（daily/weekly/monthly 使用）
	Value interface{} // []int (weekly: weekdays, monthly: days)
	Expr  string      // 自定义表达式（cron 类型使用）
}

// ExpressionBuilder Cron 表达式构建器
type ExpressionBuilder struct {
	parser cron.Parser
}

// NewExpressionBuilder 创建表达式构建器
func NewExpressionBuilder() *ExpressionBuilder {
	return &ExpressionBuilder{
		parser: cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
	}
}

// NewExpressionBuilderWithSeconds 创建支持秒级精度的表达式构建器
func NewExpressionBuilderWithSeconds() *ExpressionBuilder {
	return &ExpressionBuilder{
		parser: cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
	}
}

// BuildFromScheduleConfig 从 UI 配置构建 Cron 表达式
func (b *ExpressionBuilder) BuildFromScheduleConfig(config ScheduleConfig) (string, map[string]interface{}, error) {
	// 构建配置 map（用于前端回显）
	scheduleConfig := map[string]interface{}{
		"type":  config.Type,
		"time":  config.Time,
		"value": config.Value,
	}

	switch strings.ToLower(config.Type) {
	case "manual":
		return "", scheduleConfig, nil

	case "daily":
		hour, minute, err := parseTimeOfDay(config.Time)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("%d %d * * *", minute, hour), scheduleConfig, nil

	case "weekly":
		hour, minute, err := parseTimeOfDay(config.Time)
		if err != nil {
			return "", nil, err
		}
		field, err := formatCronList(config.Value, 0, 6, []int{0})
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("%d %d * * %s", minute, hour, field), scheduleConfig, nil

	case "monthly":
		hour, minute, err := parseTimeOfDay(config.Time)
		if err != nil {
			return "", nil, err
		}
		field, err := formatCronList(config.Value, 1, 31, []int{1})
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("%d %d %s * *", minute, hour, field), scheduleConfig, nil

	case "cron":
		// 允许空的 cron 表达式（用于清除调度）
		scheduleConfig["cron_expression"] = config.Expr
		return config.Expr, scheduleConfig, nil

	default:
		return "", nil, fmt.Errorf("不支持的 schedule_type: %s", config.Type)
	}
}

// Validate 验证 Cron 表达式
func (b *ExpressionBuilder) Validate(expr string) error {
	_, err := b.parser.Parse(expr)
	return err
}

// NextRunTime 计算下次执行时间
func (b *ExpressionBuilder) NextRunTime(expr string, from time.Time) (time.Time, error) {
	sched, err := b.parser.Parse(expr)
	if err != nil {
		return time.Time{}, err
	}
	return sched.Next(from), nil
}

// NextRunTimes 计算接下来 N 次执行时间
func (b *ExpressionBuilder) NextRunTimes(expr string, count int) ([]time.Time, error) {
	sched, err := b.parser.Parse(expr)
	if err != nil {
		return nil, err
	}

	times := make([]time.Time, 0, count)
	current := time.Now()
	for i := 0; i < count; i++ {
		next := sched.Next(current)
		times = append(times, next)
		current = next
	}
	return times, nil
}

// parseTimeOfDay 解析时间字符串（HH:MM）
func parseTimeOfDay(value string) (int, int, error) {
	if strings.TrimSpace(value) == "" {
		return 0, 0, nil
	}
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("时间格式非法，应为 HH:MM，当前为: %s", value)
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("小时格式非法: %s", parts[0])
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("分钟格式非法: %s", parts[1])
	}
	return hour, minute, nil
}

// formatCronList 格式化 Cron 列表字段（用于 weekly 和 monthly）
func formatCronList(values interface{}, min, max int, defaults []int) (string, error) {
	var intValues []int

	// 类型转换
	switch v := values.(type) {
	case []int:
		intValues = v
	case []interface{}:
		intValues = make([]int, len(v))
		for i, val := range v {
			switch num := val.(type) {
			case int:
				intValues[i] = num
			case float64:
				intValues[i] = int(num)
			default:
				return "", fmt.Errorf("invalid value type: %T", val)
			}
		}
	case nil:
		intValues = defaults
	default:
		return "", fmt.Errorf("invalid values type: %T", v)
	}

	// 使用默认值
	if len(intValues) == 0 {
		intValues = defaults
	}

	// 验证范围
	for _, val := range intValues {
		if val < min || val > max {
			return "", fmt.Errorf("值 %d 超出范围 [%d, %d]", val, min, max)
		}
	}

	// 格式化为逗号分隔的字符串
	strValues := make([]string, len(intValues))
	for i, val := range intValues {
		strValues[i] = strconv.Itoa(val)
	}
	return strings.Join(strValues, ","), nil
}
