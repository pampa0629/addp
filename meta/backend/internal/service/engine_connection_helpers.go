package service

import (
	"strings"

	commonModels "github.com/addp/common/models"
)

func containsMaskedSensitive(info commonModels.ConnectionInfo) bool {
	if info == nil {
		return false
	}

	for k, v := range info {
		lowerKey := strings.ToLower(k)
		if !(strings.Contains(lowerKey, "password") ||
			strings.Contains(lowerKey, "secret") ||
			strings.Contains(lowerKey, "token") ||
			strings.Contains(lowerKey, "key")) {
			continue
		}

		strVal, ok := v.(string)
		if !ok || strVal == "" {
			continue
		}
		if strVal == "******" || strVal == "****" || strings.Contains(strVal, "*") {
			return true
		}
	}

	return false
}
