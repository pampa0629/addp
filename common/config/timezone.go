package config

import "os"

// GetTimezone 获取应用使用的时区
// 读取标准 TZ 环境变量，Docker/Linux 原生支持
// 默认返回 Asia/Shanghai
func GetTimezone() string {
	if tz := os.Getenv("TZ"); tz != "" {
		return tz
	}
	return "Asia/Shanghai"
}
