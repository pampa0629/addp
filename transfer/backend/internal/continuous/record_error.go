package continuous

import "fmt"

const (
	recordErrorCategoryDecode          = "record_decode"
	recordErrorCategoryFieldValidation = "field_validation"
	recordErrorCategoryKeyValidation   = "key_validation"
	recordErrorCategoryTypeConversion  = "type_conversion"
)

// RecordDataError 仅表示业务 Kafka record 的确定性数据错误。
// Message 是允许进入 DLQ 的稳定安全摘要；Cause 只用于运行日志和诊断。
type RecordDataError struct {
	Code     string
	Category string
	Message  string
	Cause    error
}

func (e *RecordDataError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *RecordDataError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func newRecordDataError(code, category, message string, cause error) error {
	return &RecordDataError{Code: code, Category: category, Message: message, Cause: cause}
}
