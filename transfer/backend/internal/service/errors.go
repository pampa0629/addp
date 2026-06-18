package service

import "errors"

// ErrSystemIntegrationDisabled 表示 Transfer 未配置 System 集成。
var ErrSystemIntegrationDisabled = errors.New("system integration not available")
