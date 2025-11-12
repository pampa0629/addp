package service

import (
    "fmt"
    "time"
)

// GenError 表示瓦片生成时的错误，包含耗时信息
type GenError struct {
    Err     error
    Elapsed time.Duration
}

func (e *GenError) Error() string {
    if e == nil { return "" }
    return fmt.Sprintf("%v (elapsed=%s)", e.Err, e.Elapsed)
}

func (e *GenError) Unwrap() error { return e.Err }

