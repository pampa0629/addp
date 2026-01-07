package models

// 系统用户常量定义
// 用于标识非真实用户的系统级操作
const (
	// SystemUserIDInternal 系统内部用户ID
	SystemUserIDInternal uint = 0

	// SystemUserNameInternal 系统内部调用（如 /internal/ 路径的API）
	SystemUserNameInternal = "SYSTEM_INTERNAL"

	// SystemUserNameScheduler 系统定时任务
	SystemUserNameScheduler = "SYSTEM_SCHEDULER"

	// SystemUserNameAnonymous 匿名用户（如登录接口）
	SystemUserNameAnonymous = "SYSTEM_ANONYMOUS"

	// SystemUserNameUnknown 未知来源
	SystemUserNameUnknown = "SYSTEM_UNKNOWN"
)

// IsSystemUser 判断是否为系统用户
func IsSystemUser(userID uint) bool {
	return userID == SystemUserIDInternal
}

// GetSystemUserName 根据操作路径推断系统用户类型
func GetSystemUserName(action string) string {
	switch {
	case contains(action, "/internal/"):
		return SystemUserNameInternal
	case contains(action, "/api/auth/login"):
		return SystemUserNameAnonymous
	case contains(action, "cron"):
		return SystemUserNameScheduler
	default:
		return SystemUserNameUnknown
	}
}

// contains 辅助函数：检查字符串是否包含子串
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
