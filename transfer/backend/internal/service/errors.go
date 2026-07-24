package service

import "errors"

// ErrSystemIntegrationDisabled 表示 Transfer 未配置 System 集成。
var ErrSystemIntegrationDisabled = errors.New("system integration not available")

var ErrCDCStopConfirmationRequired = errors.New("database CDC stop requires irreversible confirmation")
var ErrCDCCaptureControlUnavailable = errors.New("database CDC capture control is not available")
var ErrCDCSchemaChangeBlocked = errors.New("database CDC task is blocked by schema change")
