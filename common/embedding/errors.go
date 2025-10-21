package embedding

import "errors"

var (
	// ErrServiceUnavailable 表示远程推理服务暂不可用
	ErrServiceUnavailable = errors.New("embedding service unavailable")
	// ErrEmptyInput 表示请求内容为空
	ErrEmptyInput = errors.New("embedding inputs cannot be empty")
)
